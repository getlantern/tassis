package db

import (
	"github.com/google/uuid"

	"github.com/getlantern/messaging-server/model"
)

// DB represents a database that can store user registration information
type DB interface {
	Register(userID uuid.UUID, deviceID uint32, registration *model.Register) error

	Unregister(userID uuid.UUID, deviceID uint32) error

	RequestPreKeys(request *model.RequestPreKeys) ([]*model.PreKey, error)

	PreKeysRemaining(userID uuid.UUID, deviceID uint32) (int, error)
}
