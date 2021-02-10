// memdb implements a memory-based db.DB. This is not well tested and not intended for production.
package memdb

import (
	"bytes"
	"sync"

	"github.com/google/uuid"

	"github.com/getlantern/messaging-server/db"
	"github.com/getlantern/messaging-server/model"
)

func New(lowPreKeyLimit int, numPreKeysToRequest int) db.DB {
	return &memdb{
		lowPreKeyLimit:      lowPreKeyLimit,
		numPreKeysToRequest: numPreKeysToRequest,
		users:               make(map[uuid.UUID]map[uint32]*model.Register),
	}
}

type memdb struct {
	lowPreKeyLimit      int
	numPreKeysToRequest int
	users               map[uuid.UUID]map[uint32]*model.Register
	mx                  sync.Mutex
}

func (d *memdb) Register(userID uuid.UUID, deviceID uint32, registration *model.Register) error {
	d.mx.Lock()
	defer d.mx.Unlock()

	user := d.users[userID]
	if user == nil {
		user = make(map[uint32]*model.Register)
		d.users[userID] = user
	}
	existing := user[deviceID]
	if existing != nil && existing.RegistrationID == registration.RegistrationID && bytes.Equal(existing.IdentityKey, registration.IdentityKey) && bytes.Equal(existing.SignedPreKey, registration.SignedPreKey) {
		// Add pre-keys
		existing.PreKeys = append(existing.PreKeys, registration.PreKeys...)
	} else {
		user[deviceID] = registration
	}
	return nil
}

func (d *memdb) Unregister(userID uuid.UUID, deviceID uint32) error {
	d.mx.Lock()
	defer d.mx.Unlock()

	user := d.users[userID]
	if user != nil {
		delete(user, deviceID)
	}

	return nil
}

func (d *memdb) RequestPreKeys(request *model.RequestPreKeys) ([]*model.PreKey, []error) {
	d.mx.Lock()
	defer d.mx.Unlock()

	userID, err := uuid.Parse(request.UserID)
	if err != nil {
		return nil, []error{model.ErrInvalidUserID}
	}

	user := d.users[userID]
	if user == nil {
		return nil, []error{model.ErrUnknownUser}
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
	errors := make([]error, 0)
	for deviceID, registration := range user {
		if !isKnownDeviceID(deviceID) {
			if len(registration.PreKeys) == 0 {
				errors = append(errors, model.ErrNoPreKeyAvailable)
			} else {
				preKey := registration.PreKeys[len(registration.PreKeys)-1]
				registration.PreKeys = registration.PreKeys[:len(registration.PreKeys)-1]
				if len(registration.PreKeys) < d.lowPreKeyLimit {
					// TODO: send a message to request numPreKeysToRequest
				}
				result = append(result, &model.PreKey{
					UserID:         request.UserID,
					DeviceID:       deviceID,
					RegistrationID: registration.RegistrationID,
					IdentityKey:    registration.IdentityKey,
					SignedPreKey:   registration.SignedPreKey,
					PreKey:         preKey,
				})
			}
		}
	}

	return result, errors
}
