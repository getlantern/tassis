// memdb implements a memory-based db.DB. This is not well tested and not intended for production.
package memdb

import (
	"bytes"
	"sync"

	"github.com/getlantern/libmessaging-go/identity"
	"github.com/getlantern/tassis/db"
	"github.com/getlantern/tassis/model"
)

func New() db.DB {
	return &memdb{
		identities:            make(map[string]map[string]*model.Register),
		identityToShortNumber: make(map[string]string),
		shortNumberToIdentity: make(map[string]string),
	}
}

type memdb struct {
	identities            map[string]map[string]*model.Register
	identityToShortNumber map[string]string
	shortNumberToIdentity map[string]string
	mx                    sync.Mutex
}

func (d *memdb) Register(identityKey identity.PublicKey, deviceId []byte, registration *model.Register) error {
	identityKeyString := identityKey.String()

	d.mx.Lock()
	defer d.mx.Unlock()

	identity := d.identities[identityKeyString]
	if identity == nil {
		identity = make(map[string]*model.Register)
		d.identities[identityKeyString] = identity
	}
	existing := identity[string(deviceId)]
	if existing != nil && bytes.Equal(existing.SignedPreKey, registration.SignedPreKey) {
		// Add pre-keys
		existing.OneTimePreKeys = append(existing.OneTimePreKeys, registration.OneTimePreKeys...)
	} else {
		identity[string(deviceId)] = registration
	}
	return nil
}

func (d *memdb) Unregister(identityKey identity.PublicKey, deviceId []byte) error {
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

func (d *memdb) RegisterShortNumber(identityKey identity.PublicKey) (string, error) {
	identityKeyString := identityKey.String()
	shortNumber := identityKey.ShortNumber()

	d.mx.Lock()
	defer d.mx.Unlock()
	_existing, found := d.shortNumberToIdentity[shortNumber]
	if found {
		existing, err := identity.PublicKeyFromString(_existing)
		if err != nil {
			return "", err
		}
		if bytes.Equal(existing, identityKey) {
			// already registered
			return shortNumber, nil
		} else {
			// short number already belongs to a different identity key
			return "", model.ErrShortNumberTaken
		}
	}

	// register
	d.identityToShortNumber[identityKeyString] = shortNumber
	d.shortNumberToIdentity[shortNumber] = identityKeyString
	return shortNumber, nil
}

func (d *memdb) LookupIdentityKeyByShortNumber(shortNumber string) (identity.PublicKey, error) {
	d.mx.Lock()
	defer d.mx.Unlock()
	str, found := d.shortNumberToIdentity[shortNumber]
	if !found {
		return nil, model.ErrUnknownShortNumber
	}
	return identity.PublicKeyFromString(str)
}

func (d *memdb) LookupShortNumberByIdentityKey(identityKey identity.PublicKey) (string, error) {
	identityKeyString := identityKey.String()
	d.mx.Lock()
	defer d.mx.Unlock()
	str, found := d.identityToShortNumber[identityKeyString]
	if !found {
		return "", model.ErrUnknownIdentity
	}
	return str, nil
}

func (d *memdb) Close() error {
	return nil
}
