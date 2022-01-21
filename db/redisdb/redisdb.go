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
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/getlantern/errors"
	"github.com/getlantern/golog"

	"github.com/getlantern/libmessaging-go/encoding"
	"github.com/getlantern/libmessaging-go/identity"
	"github.com/getlantern/tassis/db"
	"github.com/getlantern/tassis/model"
)

var (
	log = golog.LoggerFor("redisdb")
)

const (
	queryTimeout        = 10 * time.Second
	registrationTimeout = 5 * time.Minute

	// The below limits chat number registrations to an average of 2 per second, or a total of about 63 million per year
	minMillisPerChatNumberRegistration = 500

	identityToNumberKey     = "{global}identity->number"
	numberToIdentityKey     = "{global}number->identity"
	numberToShortNumberKey  = "{global}number->shortnumber"
	shortNumberToNumberKey  = "{global}shortnumber->number"
	lastRegistrationTimeKey = "{global}last-registration-time"
	registrationTokensKey   = "{global}registration-tokens"
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

	registerNumberScript = `
local identityToNumberKey = KEYS[1]
local numberToIdentityKey = KEYS[2]
local numberToShortNumberKey = KEYS[3]
local shortNumberToNumberKey = KEYS[4]
local lastRegistrationTimeKey = KEYS[5]
local registrationTokensKey = KEYS[6]

local identityKey = ARGV[1]
local newNumber = ARGV[2]
local newShortNumber = ARGV[3]
local requiredTokens = tonumber(ARGV[4])

local number = redis.call("hget", identityToNumberKey, identityKey)
if number and number ~= nil and number ~= '' then
	-- already registered
	local shortNumber = redis.call("hget", numberToShortNumberKey, number)
	return {number, shortNumber}
end

local existingIdentityKey = redis.call("hget", numberToIdentityKey, newNumber)
if existingIdentityKey then
	-- number already belongs to a different identity key
	return -1
end

-- rate limiting
if requiredTokens > 0 then
	local now = ARGV[5]
	local lastRegistrationTime = redis.call("getset", lastRegistrationTimeKey, now)
	if lastRegistrationTime then
		local delta = now - lastRegistrationTime - requiredTokens
		local availableTokens = redis.call("incrby", registrationTokensKey, delta)
		if availableTokens < 0 then
			-- This client needs to be rate limited, return the number of tokens we're short.
			-- We do this prior to completing the registration. Once the client has slept the
			-- necessary amount of time, it will call us again with rate limiting disabled in
			-- order to complete the registration.
			return -1 * availableTokens
		end
	end
end

-- new registration
redis.call("hset", identityToNumberKey, identityKey, newNumber)
redis.call("hset", numberToIdentityKey, newNumber, identityKey)
redis.call("hset", numberToShortNumberKey, newNumber, newShortNumber)
redis.call("hset", shortNumberToNumberKey, newShortNumber, newNumber)

return {newNumber, newShortNumber}
`
)

// New constructs a new Redis-backed DB that connects with the given client.
func New(client *redis.Client) (db.DB, error) {
	ctx, cancel := defaultContext()
	defer cancel()

	registerScriptSHA, err := client.ScriptLoad(ctx, registerScript).Result()
	if err != nil {
		return nil, errors.New("unable to load registerScript: %v", err)
	}
	unregisterScriptSHA, err := client.ScriptLoad(ctx, unregisterScript).Result()
	if err != nil {
		return nil, errors.New("unable to load unregisterScript: %v", err)
	}
	getPreKeyScriptSHA, err := client.ScriptLoad(ctx, getPreKeyScript).Result()
	if err != nil {
		return nil, errors.New("unable to load getPreKeyScript: %v", err)
	}
	registerNumberScriptSHA, err := client.ScriptLoad(ctx, registerNumberScript).Result()
	if err != nil {
		return nil, errors.New("unable to load registerNumberScript: %v", err)
	}
	return &redisDB{
		client:                  client,
		registerScriptSHA:       registerScriptSHA,
		unregisterScriptSHA:     unregisterScriptSHA,
		getPreKeyScriptSHA:      getPreKeyScriptSHA,
		registerNumberScriptSHA: registerNumberScriptSHA,
	}, nil
}

type redisDB struct {
	client                  *redis.Client
	registerScriptSHA       string
	unregisterScriptSHA     string
	getPreKeyScriptSHA      string
	registerNumberScriptSHA string
}

func (d *redisDB) Register(identityKey identity.PublicKey, deviceId []byte, registration *model.Register) error {
	deviceKey := deviceKey(identityKey, deviceId)
	idDevicesKey := identityDevicesKey(identityKey)
	oneTimePreKeysKey := oneTimePreKeysKey(deviceKey)

	ctx, cancel := defaultContext()
	defer cancel()

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

	ctx, cancel := defaultContext()
	defer cancel()

	return d.client.EvalSha(ctx,
		d.unregisterScriptSHA,
		[]string{deviceKey, idDevicesKey, oneTimePreKeysKey},
		encoding.HumanFriendlyBase32Encoding.EncodeToString(deviceId)).Err()
}

func (d *redisDB) RequestPreKeys(request *model.RequestPreKeys) ([]*model.PreKey, error) {
	idDevicesKey := identityDevicesKey(request.IdentityKey)

	ctx, cancel := defaultContext()
	defer cancel()

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
			DeviceId:      deviceId,
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

	ctx, cancel := defaultContext()
	defer cancel()

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
	ctx, cancel := defaultContext()
	defer cancel()

	deviceKeys, err := d.client.Keys(ctx, "device:*").Result()
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

func (d *redisDB) RegisterChatNumber(identityKey identity.PublicKey, newNumber string, newShortNumber string) (string, string, error) {
	identityKeyString := identityKey.String()

	ctx, cancel := context.WithTimeout(context.Background(), registrationTimeout)
	defer cancel()

	keys := []string{
		identityToNumberKey,
		numberToIdentityKey,
		numberToShortNumberKey,
		shortNumberToNumberKey,
		lastRegistrationTimeKey,
		registrationTokensKey}

	result, err := d.client.EvalSha(ctx,
		d.registerNumberScriptSHA,
		keys,
		identityKeyString,
		newNumber,
		newShortNumber,
		minMillisPerChatNumberRegistration,
		time.Now().UnixMilli()).Result()
	if err != nil {
		return "", "", err
	}
	r, resultIsInt64 := result.(int64)
	if resultIsInt64 {
		if r == -1 {
			return "", "", model.ErrNumberTaken
		} else {
			sleepFor := time.Duration(r) * time.Millisecond
			log.Debugf("Sleeping for %v to rate limit new chat number registration", sleepFor)
			time.Sleep(sleepFor)

			// after sleeping, we can register again, this time with rate limiting turned off
			result, err = d.client.EvalSha(ctx,
				d.registerNumberScriptSHA,
				keys,
				identityKeyString,
				newNumber,
				newShortNumber,
				0).Result()
			if err != nil {
				return "", "", err
			}
			if result == int64(-1) {
				return "", "", model.ErrNumberTaken
			}
		}
	}

	results := result.([]interface{})
	return results[0].(string), results[1].(string), nil
}

func (d *redisDB) FindChatNumberByShortNumber(shortNumber string) (string, error) {
	ctx, cancel := defaultContext()
	defer cancel()

	number, err := d.client.HGet(ctx, shortNumberToNumberKey, shortNumber).Result()
	if err != nil {
		log.Errorf("error here: %v", err)
		return "", err
	}
	if number == "" {
		return "", model.ErrUnknownShortNumber
	}
	return number, nil
}

func (d *redisDB) FindChatNumberByIdentityKey(identityKey identity.PublicKey) (string, string, error) {
	identityKeyString := identityKey.String()

	ctx, cancel := defaultContext()
	defer cancel()

	number, err := d.client.HGet(ctx, identityToNumberKey, identityKeyString).Result()
	if err != nil {
		return "", "", err
	}
	if number == "" {
		return "", "", model.ErrUnknownIdentity
	}

	shortNumber, err := d.client.HGet(ctx, numberToShortNumberKey, number).Result()
	if err != nil {
		return "", "", err
	}

	return number, shortNumber, nil
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

func defaultContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), queryTimeout)
}
