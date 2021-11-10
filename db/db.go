package db

import (
	"github.com/getlantern/libmessaging-go/identity"
	"github.com/getlantern/tassis/model"
)

var ()

// DB represents a database that can store user registration information
type DB interface {
	Register(identityKey identity.PublicKey, deviceId []byte, registration *model.Register) error

	Unregister(identityKey identity.PublicKey, deviceId []byte) error

	RequestPreKeys(request *model.RequestPreKeys) ([]*model.PreKey, error)

	PreKeysRemaining(identityKey identity.PublicKey, deviceId []byte) (int, error)

	AllRegisteredDevices() ([]*model.Address, error)

	RegisterChatNumber(identityKey identity.PublicKey, newNumber string, newShortNumber string) (string, string, error)

	FindChatNumberByShortNumber(shortNumber string) (string, error)

	FindChatNumberByIdentityKey(identityKey identity.PublicKey) (string, string, error)

	Close() error
}
