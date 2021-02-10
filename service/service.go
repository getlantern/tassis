package service

import (
	"sync"

	"github.com/google/uuid"

	"github.com/getlantern/errors"
	"github.com/getlantern/golog"
	"github.com/getlantern/messaging-server/broker"
	"github.com/getlantern/messaging-server/db"
	"github.com/getlantern/messaging-server/model"
)

var (
	log = golog.LoggerFor("service")
)

type Opts struct {
	DB     db.DB
	Broker broker.Broker
}

type Service struct {
	db     db.DB
	broker broker.Broker
}

func New(opts *Opts) *Service {
	return &Service{
		db:     opts.DB,
		broker: opts.Broker,
	}
}

type ClientConnection struct {
	userID           uuid.UUID
	deviceID         uint32
	service          *Service
	subscriber       broker.Subscriber
	publisher        broker.Publisher
	unackedMessages  map[model.Sequence]func() error
	unackedMessageMx sync.Mutex
	in               chan model.Message
	out              chan model.Message
	closeCh          chan interface{}
	closeOnce        sync.Once
}

// Connect connects a user to the service, returning a ClientConnection with
// channels for sending messages to the service and receiving messages from it.
// When the client wishes to disconnect, it should close the ClientConnection.
func (service *Service) Connect(userID uuid.UUID, deviceID uint32) (*ClientConnection, error) {
	// TODO: authenticate user

	subscriber, err := service.broker.NewSubscriber(userID.String())
	if err != nil {
		return nil, errors.New("unable to open subscriber: %v", err)
	}
	publisher, err := service.broker.NewPublisher(userID.String())
	if err != nil {
		subscriber.Close()
		return nil, errors.New("unable to open publisher: %v", err)
	}

	conn := &ClientConnection{
		userID:          userID,
		deviceID:        deviceID,
		service:         service,
		subscriber:      subscriber,
		publisher:       publisher,
		unackedMessages: make(map[model.Sequence]func() error),
		in:              make(chan model.Message),
		out:             make(chan model.Message),
		closeCh:         make(chan interface{}),
	}

	go conn.handleInbound()
	go conn.handleOutbound()

	return conn, nil
}

func (conn *ClientConnection) In() <-chan model.Message {
	return conn.in
}

func (conn *ClientConnection) Out() chan<- model.Message {
	return conn.out
}

func (conn *ClientConnection) Close() {
	conn.closeOnce.Do(func() {
		close(conn.closeCh)
	})
}

func (conn *ClientConnection) handleInbound() {
	defer conn.subscriber.Close()

	var currentSequence model.Sequence = 0
	ch := conn.subscriber.Messages()
	for {
		select {
		case <-conn.closeCh:
			return
		case brokerMsg := <-ch:
			msg := model.Message(brokerMsg.Data())
			if msg.Type() == model.TypeUserMessage {
				msg.SetSequence(currentSequence)
				conn.unackedMessageMx.Lock()
				conn.unackedMessages[currentSequence] = brokerMsg.Acker()
				conn.unackedMessageMx.Unlock()
				currentSequence++
			}
			conn.in <- msg
		}
	}
}

func (conn *ClientConnection) handleOutbound() {
	defer conn.Close()
	defer conn.publisher.Close()

	for msg := range conn.out {
		var err error
		switch msg.Type() {
		case model.TypeACK:
			err = conn.handleACK(msg.Sequence())
		case model.TypeRegister:
			var register *model.Register
			register, err = msg.Register()
			if err == nil {
				err = conn.handleRegister(register)
				if err == nil {
					conn.in <- msg.Ack()
				}
			}
		case model.TypeUnregister:
			err = conn.handleUnregister()
			return
		case model.TypeRequestPreKeys:
			err = conn.handleRequestPreKeys(msg)
		case model.TypeUserMessage:
			err = conn.handleUserMessage(msg)
		}
		if err != nil {
			log.Error(err)
			conn.in <- model.NewError(msg.Sequence(), model.TypedError(err))
		}
	}
}

func (conn *ClientConnection) handleACK(sequence model.Sequence) error {
	conn.unackedMessageMx.Lock()
	acker, found := conn.unackedMessages[sequence]
	conn.unackedMessageMx.Unlock()
	if found {
		err := acker()
		if err == nil {
			conn.unackedMessageMx.Lock()
			delete(conn.unackedMessages, sequence)
			conn.unackedMessageMx.Unlock()
		}
		return err
	}
	return errors.New("no ack found for sequence %d", sequence)
}

func (conn *ClientConnection) handleRegister(msg *model.Register) error {
	return conn.service.db.Register(conn.userID, conn.deviceID, msg)
}

func (conn *ClientConnection) handleUnregister() error {
	return conn.service.db.Unregister(conn.userID, conn.deviceID)
}

func (conn *ClientConnection) handleRequestPreKeys(msg model.Message) error {
	request, err := msg.RequestPreKeys()
	if err != nil {
		return err
	}

	// Get as many pre-keys as we can and handle the errors in here
	preKeys, errs := conn.service.db.RequestPreKeys(request)
	for _, preKey := range preKeys {
		outMsg, err := model.NewPreKey(preKey)
		if err != nil {
			errs = append(errs, err)
		} else {
			conn.in <- outMsg
		}
	}
	for _, err := range errs {
		conn.in <- model.NewError(msg.Sequence(), model.TypedError(err))
	}

	return nil
}

func (conn *ClientConnection) handleUserMessage(msg model.Message) error {
	msg.UserMessage().SetToFrom(conn.userID)
	return conn.publisher.Publish(msg)
}
