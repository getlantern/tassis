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
	_ "embed"
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

//go:embed register.lua
var registerScript []byte

//go:embed unregister.lua
var unregisterScript []byte

//go:embed get_pre_key.lua
var getPreKeyScript []byte

//go:embed register_chat_number.lua
var registerChatNumberScript []byte

// New constructs a new Redis-backed DB that connects with the given client.
func New(client *redis.Client) (db.DB, error) {
	ctx, cancel := defaultContext()
	defer cancel()

	registerScriptSHA, err := client.ScriptLoad(ctx, string(registerScript)).Result()
	if err != nil {
		return nil, errors.New("unable to load registerScript: %v", err)
	}
	unregisterScriptSHA, err := client.ScriptLoad(ctx, string(unregisterScript)).Result()
	if err != nil {
		return nil, errors.New("unable to load unregisterScript: %v", err)
	}
	getPreKeyScriptSHA, err := client.ScriptLoad(ctx, string(getPreKeyScript)).Result()
	if err != nil {
		return nil, errors.New("unable to load getPreKeyScript: %v", err)
	}
	registerChatNumberScriptSHA, err := client.ScriptLoad(ctx, string(registerChatNumberScript)).Result()
	if err != nil {
		return nil, errors.New("unable to load registerNumberScript: %v", err)
	}
	return &redisDB{
		client:                      client,
		registerScriptSHA:           registerScriptSHA,
		unregisterScriptSHA:         unregisterScriptSHA,
		getPreKeyScriptSHA:          getPreKeyScriptSHA,
		registerChatNumberScriptSHA: registerChatNumberScriptSHA,
	}, nil
}

type redisDB struct {
	client                      *redis.Client
	registerScriptSHA           string
	unregisterScriptSHA         string
	getPreKeyScriptSHA          string
	registerChatNumberScriptSHA string
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
		d.registerChatNumberScriptSHA,
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
				d.registerChatNumberScriptSHA,
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
