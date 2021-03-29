// package staticpresences provides an implementation of the presence.Repository interface that assumes all users are on the local cluster
package staticpresence

import (
	"github.com/getlantern/tassis/model"
	"github.com/getlantern/tassis/presence"
)

type repo struct {
	tassisHost string
}

func NewRepository(tassisHost string) presence.Repository {
	return &repo{tassisHost: tassisHost}
}

func (r *repo) Announce(addr *model.Address, tassisHost string) error {
	// no-op
	return nil
}

func (r *repo) Find(addr *model.Address) (string, error) {
	return r.tassisHost, nil
}
