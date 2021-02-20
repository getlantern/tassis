// redisdb provides an implementation of the ../db.DB interface backed by a Redis database. It can run on a cluster.
//
// It uses the following data model:
//
//   device:{<userID>}:<deviceID> - a Map containing the registration information for a given device
//   user->devices:{<userID>}     - a Set of all of a user's registered devices
//   otpk:{<userID>}:<deviceID>   - a List of available one-time pre keys for the given device, used as a queue with LPUSH and LPOP.
//
// The {} braces around userID indicate that the userID is used as the sharding key when running on a Redis cluster.
package redisdb

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-redis/redis/v8"

	"github.com/getlantern/errors"
	"github.com/getlantern/golog"

	"github.com/getlantern/tassis/db"
	"github.com/getlantern/tassis/identity"
	"github.com/getlantern/tassis/model"
)

var (
	log = golog.LoggerFor("redisdb")
)

const (
	registerScript = `
local deviceKey = KEYS[1]
local userDevicesKey = KEYS[2]
local oneTimePreKeysKey = KEYS[3]

local newRegistrationID = ARGV[1]
local newSignedPreKey = ARGV[2]
local deviceID = ARGV[3]

local oldRegistrationID = redis.call("hget", deviceKey, "registrationID")
local oldSignedPreKey = redis.call("hget", deviceKey, "signedPreKey")

redis.call("sadd", userDevicesKey, deviceID)
if newRegistrationID ~= oldRegistrationID or newSignedPreKey ~= oldSignedPreKey then
	redis.call("hset", deviceKey, "registrationID", newRegistrationID)
	redis.call("hset", deviceKey, "signedPreKey", newSignedPreKey)
	redis.call("del", oneTimePreKeysKey)
	return 0
end

return 1
`

	unregisterScript = `
local deviceKey = KEYS[1]
local userDevicesKey = KEYS[2]
local oneTimePreKeysKey = KEYS[3]

local deviceID = ARGV[1]

local remainingDevices = redis.call("scard", userDevicesKey)
if remainingDevices == 1 then
	redis.call("del", userDevicesKey)
else
	redis.call("srem", userDevicesKey, deviceID)
end

redis.call("del", deviceKey, oneTimePreKeysKey)
return 0
`

	getPreKeyScript = `
local deviceKey = KEYS[1]
local oneTimePreKeysKey = KEYS[2]

local registrationID = redis.call("hget", deviceKey, "registrationID")
local signedPreKey = redis.call("hget", deviceKey, "signedPreKey")
local oneTimePreKey = redis.call("lpop", oneTimePreKeysKey)

return {registrationID, signedPreKey, oneTimePreKey}
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

func (d *redisDB) Register(userID identity.UserID, deviceID uint32, registration *model.Register) error {
	deviceKey := deviceKey(userID, deviceID)
	userDevicesKey := userDevicesKey(userID)
	oneTimePreKeysKey := oneTimePreKeysKey(deviceKey)

	ctx := context.Background()
	p := d.client.TxPipeline()
	p.EvalSha(ctx,
		d.registerScriptSHA,
		[]string{deviceKey, userDevicesKey, oneTimePreKeysKey},
		registration.RegistrationID,
		string(registration.SignedPreKey),
		deviceID).Err()
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

func (d *redisDB) Unregister(userID identity.UserID, deviceID uint32) error {
	deviceKey := deviceKey(userID, deviceID)
	userDevicesKey := userDevicesKey(userID)
	oneTimePreKeysKey := oneTimePreKeysKey(deviceKey)

	return d.client.EvalSha(context.Background(),
		d.unregisterScriptSHA,
		[]string{deviceKey, userDevicesKey, oneTimePreKeysKey},
		deviceID).Err()
}

func (d *redisDB) RequestPreKeys(request *model.RequestPreKeys) ([]*model.PreKey, error) {
	userDevicesKey := userDevicesKey(request.UserID)

	ctx := context.Background()
	userDevices, err := d.client.SMembers(ctx, userDevicesKey).Result()
	if err != nil {
		return nil, err
	}

	cmds := make([]*redis.Cmd, 0)
	deviceIDs := make([]string, 0)
	p := d.client.Pipeline()
deviceLoop:
	for _, deviceID := range userDevices {
		for _, knownDeviceID := range request.KnownDeviceIDs {
			if strconv.Itoa(int(knownDeviceID)) == deviceID {
				continue deviceLoop
			}
		}
		deviceKey := deviceKeyFromString(request.UserID, deviceID)
		deviceIDs = append(deviceIDs, deviceID)
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
		_deviceID := deviceIDs[i]
		deviceID, err := strconv.Atoi(_deviceID)
		if err != nil {
			return nil, errors.New("unable to parse deviceID '%v': %v", _deviceID, err)
		}
		_registrationID := out[0].(string)
		registrationID, err := strconv.Atoi(_registrationID)
		if err != nil {
			return nil, errors.New("unable to parse registrationID '%v': %v", _registrationID, err)
		}
		var oneTimePreKey []byte
		if out[2] != nil {
			oneTimePreKey = []byte(out[2].(string))
		}
		preKeys = append(preKeys, &model.PreKey{
			Address: &model.Address{
				UserID:   request.UserID,
				DeviceID: uint32(deviceID),
			},
			RegistrationID: uint32(registrationID),
			SignedPreKey:   []byte(out[1].(string)),
			OneTimePreKey:  oneTimePreKey,
		})
	}

	if len(preKeys) == 0 {
		return nil, model.ErrUnknownUser
	}
	return preKeys, nil
}

func (d *redisDB) PreKeysRemaining(userID identity.UserID, deviceID uint32) (int, error) {
	deviceKey := deviceKey(userID, deviceID)
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
		deviceID, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil, err
		}
		userID, err := identity.UserIDFromString(strings.Trim(parts[1], "{}"))
		if err != nil {
			return nil, err
		}
		result = append(result, &model.Address{UserID: userID, DeviceID: uint32(deviceID)})
	}
	return result, nil
}

func (d *redisDB) Close() error {
	return d.client.Close()
}

func deviceKey(userID identity.UserID, deviceID uint32) string {
	return fmt.Sprintf("device:{%v}:%d", userID.String(), deviceID)
}

func deviceKeyFromString(userID identity.UserID, deviceID string) string {
	return fmt.Sprintf("device:{%v}:%v", userID.String(), deviceID)
}

func userDevicesKey(userID identity.UserID) string {
	return "user->devices:{" + userID.String() + "}"
}

func oneTimePreKeysKey(deviceKey string) string {
	return "otpk:" + deviceKey
}
