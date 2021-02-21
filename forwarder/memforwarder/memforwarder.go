// memforwarder provides an implementation of forwarder using process-local in-memory services
package memforwarder

import (
	"github.com/getlantern/errors"

	"github.com/getlantern/tassis/forwarder"
	"github.com/getlantern/tassis/service"
)

func New(services map[string]service.Service) *forwarder.Forwarder {
	dial := func(host string) (service.ClientConnection, error) {
		srvc, found := services[host]
		if !found {
			return nil, errors.New("unknown host: %v", host)
		}
		var err error
		conn, err := srvc.Connect()
		if err != nil {
			return nil, errors.New("unable to connect to %v: %v", host, err)
		}
		return conn, nil
	}

	return forwarder.New(dial)
}
