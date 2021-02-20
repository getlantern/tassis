// package forwarder provides a facility for forwarding messages to another tassis cluster
package forwarder

import (
	"sync"

	"github.com/getlantern/errors"
	"github.com/getlantern/tassis/model"
	"github.com/getlantern/tassis/service"
)

// Dial is a function that connects to a tassis host
type Dial func(host string) (service.ClientConnection, error)

// Forwarder is a forwarder that connects to other tassis clusters using a Connect function
type Forwarder struct {
	dial      func(host string) (service.ClientConnection, error)
	conns     map[string]service.ClientConnection
	callbacks map[uint32]func(err error)
	mb        model.MessageBuilder
	mx        sync.Mutex
}

// New constructs a new Forwarder using the specified dial function
func New(dial Dial) *Forwarder {
	return &Forwarder{
		dial:      dial,
		conns:     make(map[string]service.ClientConnection),
		callbacks: make(map[uint32]func(err error)),
	}
}

// Forward forwards the given message to specified tassisHost. Once it receives an ack or error response,
// it calls finished. If finished receives a nil error, that means the message was forwarded successfully.
func (f *Forwarder) Forward(msg *model.ForwardedMessage, tassisHost string, finished func(error)) {
	// TODO: we shouldn't just trust whatever addresses we get, maybe create a whitelist or some more sophisticated algorithm (blockchain?)
	f.mx.Lock()
	conn, found := f.conns[tassisHost]
	if !found {
		var err error
		// TODO: make sure we have a timeout here
		conn, err = f.dial(tassisHost)
		if err != nil {
			finished(errors.New("unable to connect to %v: %v", tassisHost, err))
			f.mx.Unlock()
			return
		}
		go f.readFrom(conn)
	}
	fullMsg := f.mb.Build(&model.Message_OutboundMessage{msg.GetMessage()})
	f.callbacks[fullMsg.Sequence] = finished
	f.mx.Unlock()
	conn.Send(fullMsg)
}

func (f *Forwarder) readFrom(conn service.ClientConnection) {
	for msg := range conn.In() {
		switch t := msg.GetPayload().(type) {
		case *model.Message_Ack:
			f.triggerCallbackFor(msg, nil)
		case *model.Message_Error:
			f.triggerCallbackFor(msg, t.Error)
		default:
			// unknown response, don't bother
		}
	}
}

func (f *Forwarder) triggerCallbackFor(msg *model.Message, err error) {
	seq := msg.GetSequence()
	f.mx.Lock()
	finished, found := f.callbacks[seq]
	if found {
		delete(f.callbacks, seq)
	}
	f.mx.Unlock()

	if found {
		finished(err)
	}
}
