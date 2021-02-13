// memdb implements a memory-based db.DB. This is not well tested and not intended for production.
package memdb

import (
	"bytes"
	"sync"

	"github.com/getlantern/messaging-server/db"
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

func (d *memdb) Register(userID []byte, deviceID uint32, registration *model.Register) error {
	userIDString := model.UserIDToString(userID)

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
		existing.PreKeys = append(existing.PreKeys, registration.PreKeys...)
	} else {
		user[deviceID] = registration
	}
	return nil
}

func (d *memdb) Unregister(userID []byte, deviceID uint32) error {
	userIDString := model.UserIDToString(userID)

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
	userIDString := model.UserIDToString(request.UserID)

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
			var preKey []byte
			if len(registration.PreKeys) > 0 {
				preKey = registration.PreKeys[len(registration.PreKeys)-1]
				registration.PreKeys = registration.PreKeys[:len(registration.PreKeys)-1]
			}
			result = append(result, &model.PreKey{
				UserID:         request.UserID,
				DeviceID:       deviceID,
				RegistrationID: registration.RegistrationID,
				SignedPreKey:   registration.SignedPreKey,
				PreKey:         preKey,
			})
		}
	}

	return result, nil
}

func (d *memdb) PreKeysRemaining(userID []byte, deviceID uint32) (int, error) {
	userIDString := model.UserIDToString(userID)

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

	return len(device.PreKeys), nil
}
