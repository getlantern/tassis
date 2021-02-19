// memforwarder provides an implementation of forwarder using process-local in-memory services
package memforwarder

import (
	"sync"

	"github.com/getlantern/errors"

	"github.com/getlantern/tassis/forwarder"
	"github.com/getlantern/tassis/model"
	"github.com/getlantern/tassis/service"
)

// New constructs a new forwarder using a map of known services running in-process with each other
func New(services map[string]service.Service) forwarder.Send {

}

type sender struct {
	services map[string]service.Service
	conns    map[string]service.ClientConnection
	mx       sync.Mutex
}

func (s *sender) send(msg *model.ForwardedMessage, onSuccess func(), onError func(err error)) {
	s.mx.Lock()
	conn, found := s.conns[msg.ForwardTo]
	if !found {
		srvc, found := s.services[msg.ForwardTo]
		if !found {
			onError(errors.New("unknown host: %v", msg.ForwardTo))
			s.mx.Unlock()
			return
		}
		var err error
		conn, err = srvc.Connect()
		if err != nil {
			onError(errors.New("unable to connect to %v: %v", msg.ForwardTo, err))
			s.mx.Unlock()
			return
		}
	}
	msg
	conn.Send(msg.GetMessage())
}
