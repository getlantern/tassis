// memdb implements a memory-based db.DB. This is not well tested and not intended for production.
package memdb

import (
	"crypto/subtle"
	"sync"

	"github.com/getlantern/libmessaging-go/identity"
	"github.com/getlantern/ops"
	"github.com/getlantern/tassis/db"
	"github.com/getlantern/tassis/model"
	"go.opentelemetry.io/otel"
)

var (
	tracer = otel.Tracer("memdb")
)

func New() db.DB {
	return &memdb{
		identities:          make(map[string]map[string]*model.Register),
		identityKeyToNumber: make(map[string]string),
		numberToIdentityKey: make(map[string]string),
		numberToShortNumber: make(map[string]string),
		shortNumberToNumber: make(map[string]string),
	}
}

type memdb struct {
	identities          map[string]map[string]*model.Register
	identityKeyToNumber map[string]string
	numberToIdentityKey map[string]string
	numberToShortNumber map[string]string
	shortNumberToNumber map[string]string
	mx                  sync.Mutex
}

func (d *memdb) Register(identityKey identity.PublicKey, deviceId []byte, registration *model.Register) error {
	op := ops.Begin("memdb.register")
	defer op.End()

	identityKeyString := identityKey.String()

	d.mx.Lock()
	defer d.mx.Unlock()

	identity := d.identities[identityKeyString]
	if identity == nil {
		identity = make(map[string]*model.Register)
		d.identities[identityKeyString] = identity
	}
	existing := identity[string(deviceId)]
	if existing != nil && subtle.ConstantTimeCompare(existing.SignedPreKey, registration.SignedPreKey) == 1 {
		// Add pre-keys
		existing.OneTimePreKeys = append(existing.OneTimePreKeys, registration.OneTimePreKeys...)
	} else {
		identity[string(deviceId)] = registration
	}
	return nil
}

func (d *memdb) Unregister(identityKey identity.PublicKey, deviceId []byte) error {
	op := ops.Begin("memdb.unregister")
	defer op.End()

	identityKeyString := identityKey.String()

	d.mx.Lock()
	defer d.mx.Unlock()

	identity := d.identities[identityKeyString]
	if identity != nil {
		delete(identity, string(deviceId))
		if len(identity) == 0 {
			delete(d.identities, identityKeyString)
		}
	}

	return nil
}

func (d *memdb) RequestPreKeys(request *model.RequestPreKeys) ([]*model.PreKey, error) {
	op := ops.Begin("memdb.request_pre_keys")
	defer op.End()

	identityKeyString := identity.PublicKey(request.IdentityKey).String()

	d.mx.Lock()
	defer d.mx.Unlock()

	identity := d.identities[identityKeyString]
	if identity == nil {
		return nil, model.ErrUnknownIdentity
	}

	isKnownDeviceId := func(deviceId string) bool {
		for _, candidate := range request.KnownDeviceIds {
			if string(candidate) == deviceId {
				return true
			}
		}
		return false
	}

	result := make([]*model.PreKey, 0)
	for deviceId, registration := range identity {
		if !isKnownDeviceId(deviceId) {
			var oneTimePreKey []byte
			if len(registration.OneTimePreKeys) > 0 {
				oneTimePreKey = registration.OneTimePreKeys[len(registration.OneTimePreKeys)-1]
				registration.OneTimePreKeys = registration.OneTimePreKeys[:len(registration.OneTimePreKeys)-1]
			}
			result = append(result, &model.PreKey{
				DeviceId:      []byte(deviceId),
				SignedPreKey:  registration.SignedPreKey,
				OneTimePreKey: oneTimePreKey,
			})
		}
	}

	return result, nil
}

func (d *memdb) PreKeysRemaining(identityKey identity.PublicKey, deviceId []byte) (int, error) {
	op := ops.Begin("memdb.pre_keys_remaining")
	defer op.End()

	identityKeyString := identityKey.String()

	d.mx.Lock()
	defer d.mx.Unlock()

	identity := d.identities[identityKeyString]
	if identity == nil {
		return 0, model.ErrUnknownIdentity
	}

	device := identity[string(deviceId)]
	if device == nil {
		return 0, model.ErrUnknownDevice
	}

	return len(device.OneTimePreKeys), nil
}

func (d *memdb) AllRegisteredDevices() ([]*model.Address, error) {
	op := ops.Begin("memdb.all_registered_devices")
	defer op.End()

	result := make([]*model.Address, 0)
	d.mx.Lock()
	defer d.mx.Unlock()

	for identityKey, devices := range d.identities {
		for deviceId := range devices {
			idKey, err := identity.PublicKeyFromString(identityKey)
			if err != nil {
				return nil, err
			}
			result = append(result, &model.Address{
				IdentityKey: idKey,
				DeviceId:    []byte(deviceId),
			})
		}
	}

	return result, nil
}

func (d *memdb) RegisterChatNumber(identityKey identity.PublicKey, newNumber string, newShortNumber string) (string, string, error) {
	op := ops.Begin("memdb.register_chat_number")
	defer op.End()

	identityKeyString := identityKey.String()

	d.mx.Lock()
	defer d.mx.Unlock()

	number, found := d.identityKeyToNumber[identityKeyString]
	if found {
		// already registered
		return number, d.numberToShortNumber[number], nil
	}

	_, numberTaken := d.numberToIdentityKey[newNumber]
	if numberTaken {
		// number already belongs to a different identity key
		return "", "", model.ErrNumberTaken
	}

	// register
	d.identityKeyToNumber[identityKeyString] = newNumber
	d.numberToIdentityKey[newNumber] = identityKeyString
	d.numberToShortNumber[newNumber] = newShortNumber
	d.shortNumberToNumber[newShortNumber] = newNumber
	return newNumber, newShortNumber, nil
}

func (d *memdb) FindChatNumberByShortNumber(shortNumber string) (string, error) {
	op := ops.Begin("memdb.find_chat_number_by_short_number")
	defer op.End()

	d.mx.Lock()
	defer d.mx.Unlock()
	number, found := d.shortNumberToNumber[shortNumber]
	if !found {
		return "", model.ErrUnknownShortNumber
	}
	return number, nil
}

func (d *memdb) FindChatNumberByIdentityKey(identityKey identity.PublicKey) (string, string, error) {
	op := ops.Begin("memdb.find_chat_number_by_identity_key")
	defer op.End()

	identityKeyString := identityKey.String()
	d.mx.Lock()
	defer d.mx.Unlock()
	number, found := d.identityKeyToNumber[identityKeyString]
	if !found {
		return "", "", model.ErrUnknownIdentity
	}
	shortNumber := d.numberToShortNumber[number]
	return number, shortNumber, nil
}

func (d *memdb) Close() error {
	return nil
}
