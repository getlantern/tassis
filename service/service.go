package service

import (
	"github.com/getlantern/tassis/model"
)

// Service is a tassis service that handles key distribution and message transport.
type Service interface {
	// Connect connects to the Service and returns a ClientConnection that can be used to interact with the service.
	Connect() (ClientConnection, error)
}

// ClientConnection represents a client connection to a Service
type ClientConnection interface {
	// Out returns a channel with which clients can send messages to the service
	Out() chan<- *model.Message

	// Send sends the given msg to the Service
	Send(msg *model.Message)

	// In returns a channel which clients can use to receive messages from the service
	In() <-chan *model.Message

	// Receive receives the next msg from the Service
	Receive() *model.Message

	// Drain drains all pending messages from the service
	Drain() int

	// Close closes the client connection
	Close()
}
