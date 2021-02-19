// package mempresence provides a local in-memory repository of presence information
package mempresence

import (
	"github.com/getlantern/tassis/model"
	"github.com/getlantern/tassis/presence"
)

type repo map[string]string

func NewRepository() presence.Repository {
	return repo(make(map[string]string, 0))
}

func (r repo) Announce(addr *model.Address, tassisHost string) error {
	r[addr.String()] = tassisHost
	return nil
}

func (r repo) Find(addr *model.Address) (string, error) {
	tassisHost, found := r[addr.String()]
	if !found {
		return "", model.ErrUnknownDevice
	}
	return tassisHost, nil
}
