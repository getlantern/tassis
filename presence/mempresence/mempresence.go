// package mempresence provides a local in-memory repository of presence information
package mempresence

import (
	"sync"

	"github.com/getlantern/tassis/model"
	"github.com/getlantern/tassis/presence"
)

type repo struct {
	m  map[string]string
	mx sync.Mutex
}

func NewRepository() presence.Repository {
	return repo{m: make(map[string]string, 0)}
}

func (r repo) Announce(addr *model.Address, tassisHost string) error {
	r.mx.Lock()
	defer r.mx.Unlock()

	r.m[addr.String()] = tassisHost
	return nil
}

func (r repo) Find(addr *model.Address) (string, error) {
	r.mx.Lock()
	defer r.mx.Unlock()

	tassisHost, found := r.m[addr.String()]
	if !found {
		return "", model.ErrUnknownDevice
	}
	return tassisHost, nil
}
