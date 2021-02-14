// memdb implements a memory-based db.DB. This is not well tested and not intended for production.
package memdb

import (
	"bytes"
	"sync"

	"github.com/getlantern/messaging-server/db"
	"github.com/getlantern/messaging-server/identity"
	"github.com/getlantern/messaging-server/model"
)

func New() db.DB {
	return &memdb{
		users: make(map[string]map[uint32]*model.Register),
	}
}

type memdb struct {
	users map[string]map[uint32]*model.Register
	mx    sync.Mutex
}

func (d *memdb) Register(userID identity.UserID, deviceID uint32, registration *model.Register) error {
	userIDString := userID.String()

	d.mx.Lock()
	defer d.mx.Unlock()

	user := d.users[userIDString]
	if user == nil {
		user = make(map[uint32]*model.Register)
		d.users[userIDString] = user
	}
	existing := user[deviceID]
	if existing != nil && existing.RegistrationID == registration.RegistrationID && bytes.Equal(existing.SignedPreKey, registration.SignedPreKey) {
		// Add pre-keys
		existing.OneTimePreKeys = append(existing.OneTimePreKeys, registration.OneTimePreKeys...)
	} else {
		user[deviceID] = registration
	}
	return nil
}

func (d *memdb) Unregister(userID identity.UserID, deviceID uint32) error {
	userIDString := userID.String()

	d.mx.Lock()
	defer d.mx.Unlock()

	user := d.users[userIDString]
	if user != nil {
		delete(user, deviceID)
		if len(user) == 0 {
			delete(d.users, userIDString)
		}
	}

	return nil
}

func (d *memdb) RequestPreKeys(request *model.RequestPreKeys) ([]*model.PreKey, error) {
	userIDString := identity.UserID(request.UserID).String()

	d.mx.Lock()
	defer d.mx.Unlock()

	user := d.users[userIDString]
	if user == nil {
		return nil, model.ErrUnknownUser
	}

	isKnownDeviceID := func(deviceID uint32) bool {
		for _, candidate := range request.KnownDeviceIDs {
			if candidate == deviceID {
				return true
			}
		}
		return false
	}

	result := make([]*model.PreKey, 0)
	for deviceID, registration := range user {
		if !isKnownDeviceID(deviceID) {
			var oneTimePreKey []byte
			if len(registration.OneTimePreKeys) > 0 {
				oneTimePreKey = registration.OneTimePreKeys[len(registration.OneTimePreKeys)-1]
				registration.OneTimePreKeys = registration.OneTimePreKeys[:len(registration.OneTimePreKeys)-1]
			}
			result = append(result, &model.PreKey{
				Address: &model.Address{
					UserID:   request.UserID,
					DeviceID: deviceID,
				},
				RegistrationID: registration.RegistrationID,
				SignedPreKey:   registration.SignedPreKey,
				OneTimePreKey:  oneTimePreKey,
			})
		}
	}

	return result, nil
}

func (d *memdb) PreKeysRemaining(userID identity.UserID, deviceID uint32) (int, error) {
	userIDString := userID.String()

	d.mx.Lock()
	defer d.mx.Unlock()

	user := d.users[userIDString]
	if user == nil {
		return 0, model.ErrUnknownUser
	}

	device := user[deviceID]
	if device == nil {
		return 0, model.ErrUnknownDevice
	}

	return len(device.OneTimePreKeys), nil
}

func (d *memdb) Close() error {
	return nil
}
