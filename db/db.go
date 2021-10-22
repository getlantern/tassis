package db

import (
	"github.com/getlantern/libmessaging-go/identity"
	"github.com/getlantern/tassis/model"
)

// DB represents a database that can store user registration information
type DB interface {
	Register(identityKey identity.PublicKey, deviceId []byte, registration *model.Register) error

	Unregister(identityKey identity.PublicKey, deviceId []byte) error

	RequestPreKeys(request *model.RequestPreKeys) ([]*model.PreKey, error)

	PreKeysRemaining(identityKey identity.PublicKey, deviceId []byte) (int, error)

	AllRegisteredDevices() ([]*model.Address, error)

	Close() error
}
