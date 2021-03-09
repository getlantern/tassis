// redisdb provides an implementation of the ../db.DB interface backed by a Redis database. It can run on a cluster.
//
// It uses the following data model:
//
//   device:{<identityKey>}:<deviceId> - a Map containing the registration information for a given device
//   identity->devices:{<identityKey>}     - a Set of all of an identity's registered devices
//   otpk:{<identityKey>}:<deviceId>   - a List of available one-time pre keys for the given device, used as a queue with LPUSH and LPOP.
//
// The {} braces around identityKey indicate that the identityKey is used as the sharding key when running on a Redis cluster.
package redisdb

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-redis/redis/v8"

	"github.com/getlantern/errors"
	"github.com/getlantern/golog"

	"github.com/getlantern/tassis/db"
	"github.com/getlantern/tassis/encoding"
	"github.com/getlantern/tassis/identity"
	"github.com/getlantern/tassis/model"
)

var (
	log = golog.LoggerFor("redisdb")
)

const (
	registerScript = `
local deviceKey = KEYS[1]
local identityDevicesKey = KEYS[2]
local oneTimePreKeysKey = KEYS[3]

local newSignedPreKey = ARGV[1]
local deviceId = ARGV[2]

local oldSignedPreKey = redis.call("hget", deviceKey, "signedPreKey")

redis.call("sadd", identityDevicesKey, deviceId)
if newSignedPreKey ~= oldSignedPreKey then
	redis.call("hset", deviceKey, "signedPreKey", newSignedPreKey)
	redis.call("del", oneTimePreKeysKey)
	return 0
end

return 1
`

	unregisterScript = `
local deviceKey = KEYS[1]
local identityDevicesKey = KEYS[2]
local oneTimePreKeysKey = KEYS[3]

local deviceId = ARGV[1]

local remainingDevices = redis.call("scard", identityDevicesKey)
if remainingDevices == 1 then
	redis.call("del", identityDevicesKey)
else
	redis.call("srem", identityDevicesKey, deviceId)
end

redis.call("del", deviceKey, oneTimePreKeysKey)
return 0
`

	getPreKeyScript = `
local deviceKey = KEYS[1]
local oneTimePreKeysKey = KEYS[2]

local signedPreKey = redis.call("hget", deviceKey, "signedPreKey")
local oneTimePreKey = redis.call("lpop", oneTimePreKeysKey)

return {signedPreKey, oneTimePreKey}
`
)

// New constructs a new Redis-backed DB that connects with the given client.
func New(client *redis.Client) (db.DB, error) {
	registerScriptSHA, err := client.ScriptLoad(context.Background(), registerScript).Result()
	if err != nil {
		return nil, errors.New("unable to load registerScript: %v", err)
	}
	unregisterScriptSHA, err := client.ScriptLoad(context.Background(), unregisterScript).Result()
	if err != nil {
		return nil, errors.New("unable to load unregisterScript: %v", err)
	}
	getPreKeyScriptSHA, err := client.ScriptLoad(context.Background(), getPreKeyScript).Result()
	if err != nil {
		return nil, errors.New("unable to load getPreKeyScript: %v", err)
	}
	return &redisDB{
		client:              client,
		registerScriptSHA:   registerScriptSHA,
		unregisterScriptSHA: unregisterScriptSHA,
		getPreKeyScriptSHA:  getPreKeyScriptSHA,
	}, nil
}

type redisDB struct {
	client              *redis.Client
	registerScriptSHA   string
	unregisterScriptSHA string
	getPreKeyScriptSHA  string
}

func (d *redisDB) Register(identityKey identity.PublicKey, deviceId []byte, registration *model.Register) error {
	deviceKey := deviceKey(identityKey, deviceId)
	idDevicesKey := identityDevicesKey(identityKey)
	oneTimePreKeysKey := oneTimePreKeysKey(deviceKey)

	ctx := context.Background()
	p := d.client.TxPipeline()
	p.EvalSha(ctx,
		d.registerScriptSHA,
		[]string{deviceKey, idDevicesKey, oneTimePreKeysKey},
		string(registration.SignedPreKey),
		encoding.HumanFriendlyBase32Encoding.EncodeToString(deviceId))
	oneTimePreKeys := make([]interface{}, 0, len(registration.OneTimePreKeys))
	for _, oneTimePreKey := range registration.OneTimePreKeys {
		oneTimePreKeys = append(oneTimePreKeys, oneTimePreKey)
	}
	if len(registration.OneTimePreKeys) > 0 {
		p.LPush(ctx, oneTimePreKeysKey, oneTimePreKeys)
	}
	_, err := p.Exec(ctx)
	return err
}

func (d *redisDB) Unregister(identityKey identity.PublicKey, deviceId []byte) error {
	deviceKey := deviceKey(identityKey, deviceId)
	idDevicesKey := identityDevicesKey(identityKey)
	oneTimePreKeysKey := oneTimePreKeysKey(deviceKey)

	return d.client.EvalSha(context.Background(),
		d.unregisterScriptSHA,
		[]string{deviceKey, idDevicesKey, oneTimePreKeysKey},
		encoding.HumanFriendlyBase32Encoding.EncodeToString(deviceId)).Err()
}

func (d *redisDB) RequestPreKeys(request *model.RequestPreKeys) ([]*model.PreKey, error) {
	idDevicesKey := identityDevicesKey(request.IdentityKey)

	ctx := context.Background()
	identityDevices, err := d.client.SMembers(ctx, idDevicesKey).Result()
	if err != nil {
		return nil, err
	}

	cmds := make([]*redis.Cmd, 0)
	deviceIds := make([]string, 0)
	p := d.client.Pipeline()
deviceLoop:
	for _, deviceId := range identityDevices {
		for _, knownDeviceId := range request.KnownDeviceIds {
			if encoding.HumanFriendlyBase32Encoding.EncodeToString(knownDeviceId) == deviceId {
				continue deviceLoop
			}
		}
		deviceKey := deviceKeyFromString(request.IdentityKey, deviceId)
		deviceIds = append(deviceIds, deviceId)
		oneTimePreKeysKey := oneTimePreKeysKey(deviceKey)
		cmd := p.EvalSha(ctx,
			d.getPreKeyScriptSHA,
			[]string{deviceKey, oneTimePreKeysKey})
		cmds = append(cmds, cmd)
	}

	_, err = p.Exec(ctx)
	if err != nil {
		return nil, err
	}

	preKeys := make([]*model.PreKey, 0, len(cmds))
	for i, cmd := range cmds {
		_out, _ := cmd.Result()
		out := _out.([]interface{})
		_deviceId := deviceIds[i]
		deviceId, err := encoding.HumanFriendlyBase32Encoding.DecodeString(_deviceId)
		if err != nil {
			return nil, errors.New("unable to parse deviceId '%v': %v", _deviceId, err)
		}
		var oneTimePreKey []byte
		if out[1] != nil {
			oneTimePreKey = []byte(out[1].(string))
		}
		preKeys = append(preKeys, &model.PreKey{
			Address: &model.Address{
				IdentityKey: request.IdentityKey,
				DeviceId:    deviceId,
			},
			SignedPreKey:  []byte(out[0].(string)),
			OneTimePreKey: oneTimePreKey,
		})
	}

	if len(preKeys) == 0 {
		return nil, model.ErrUnknownIdentity
	}
	return preKeys, nil
}

func (d *redisDB) PreKeysRemaining(identityKey identity.PublicKey, deviceId []byte) (int, error) {
	deviceKey := deviceKey(identityKey, deviceId)
	oneTimePreKeysKey := oneTimePreKeysKey(deviceKey)

	ctx := context.Background()
	p := d.client.Pipeline()
	deviceExistsCmd := p.Exists(ctx, deviceKey)
	numPreKeysCmd := p.LLen(ctx, oneTimePreKeysKey)
	_, err := p.Exec(ctx)
	if err != nil {
		return 0, err
	}

	deviceExists, _ := deviceExistsCmd.Result()
	if deviceExists == 0 {
		return 0, model.ErrUnknownDevice
	}
	numPreKeys, _ := numPreKeysCmd.Result()
	return int(numPreKeys), nil
}

func (d *redisDB) AllRegisteredDevices() ([]*model.Address, error) {
	deviceKeys, err := d.client.Keys(context.Background(), "device:*").Result()
	if err != nil {
		return nil, err
	}
	result := make([]*model.Address, 0, len(deviceKeys))
	for _, deviceKey := range deviceKeys {
		parts := strings.Split(deviceKey, ":")
		identityKey, err := identity.PublicKeyFromString(strings.Trim(parts[1], "{}"))
		if err != nil {
			return nil, err
		}
		deviceIdBytes, err := encoding.HumanFriendlyBase32Encoding.DecodeString(parts[2])
		if err != nil {
			return nil, err
		}
		result = append(result, &model.Address{IdentityKey: identityKey, DeviceId: deviceIdBytes})
	}
	return result, nil
}

func (d *redisDB) Close() error {
	return d.client.Close()
}

func deviceKey(identityKey identity.PublicKey, deviceId []byte) string {
	return deviceKeyFromString(identityKey, encoding.HumanFriendlyBase32Encoding.EncodeToString(deviceId))
}

func deviceKeyFromString(identityKey identity.PublicKey, deviceId string) string {
	return fmt.Sprintf("device:{%v}:%v", identityKey.String(), deviceId)
}

func identityDevicesKey(identityKey identity.PublicKey) string {
	return "identity->devices:{" + identityKey.String() + "}"
}

func oneTimePreKeysKey(deviceKey string) string {
	return "otpk:" + deviceKey
}
