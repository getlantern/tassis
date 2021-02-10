package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	lru "github.com/hashicorp/golang-lru"

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
	DB                   db.DB
	Broker               broker.Broker
	PublisherCacheSize   int
	CheckPreKeysInterval time.Duration
	LowPreKeysLimit      int
	NumPreKeysToRequest  int
}

func (opts *Opts) ApplyDefaults() {
	if opts.PublisherCacheSize <= 0 {
		opts.PublisherCacheSize = 1
	}
	if opts.CheckPreKeysInterval == 0 {
		opts.CheckPreKeysInterval = 5 * time.Minute
	}
	if opts.LowPreKeysLimit == 0 {
		opts.LowPreKeysLimit = 10
	}
	if opts.NumPreKeysToRequest == 0 {
		opts.NumPreKeysToRequest = opts.LowPreKeysLimit * 2
	}
}

type Service struct {
	db                   db.DB
	broker               broker.Broker
	checkPreKeysInterval time.Duration
	lowPreKeysLimit      int
	numPreKeysToRequest  int
	publisherCache       *lru.Cache
	publisherCacheMx     sync.Mutex
}

func New(opts *Opts) (*Service, error) {
	opts.ApplyDefaults()
	publisherCache, err := lru.NewWithEvict(opts.PublisherCacheSize, func(key, value interface{}) {
		value.(broker.Publisher).Close()
	})
	if err != nil {
		return nil, err
	}
	return &Service{
		db:                   opts.DB,
		broker:               opts.Broker,
		publisherCache:       publisherCache,
		checkPreKeysInterval: opts.CheckPreKeysInterval,
		lowPreKeysLimit:      opts.LowPreKeysLimit,
		numPreKeysToRequest:  opts.NumPreKeysToRequest,
	}, err
}

type ClientConnection struct {
	userID           uuid.UUID
	deviceID         uint32
	srvc             *Service
	subscriber       broker.Subscriber
	messageBuilder   *model.MessageBuilder
	unackedMessages  map[model.Sequence]func() error
	unackedMessageMx sync.Mutex
	out              chan model.Message
	in               chan model.Message
	closeCh          chan interface{}
	closeOnce        sync.Once
}

// Connect connects a user to the service, returning a ClientConnection with
// channels for sending messages to the service and receiving messages from it.
// When the client wishes to disconnect, it should close the ClientConnection.
func (srvc *Service) Connect(userID uuid.UUID, deviceID uint32) (*ClientConnection, error) {
	// TODO: instrument number of open clientConnections
	// TODO: make sure web layer authenticates user somehow

	subscriber, err := srvc.broker.NewSubscriber(topicFor(userID, deviceID))
	if err != nil {
		return nil, errors.New("unable to open subscriber: %v", err)
	}

	conn := &ClientConnection{
		userID:          userID,
		deviceID:        deviceID,
		srvc:            srvc,
		subscriber:      subscriber,
		messageBuilder:  &model.MessageBuilder{},
		unackedMessages: make(map[model.Sequence]func() error),
		out:             make(chan model.Message),
		in:              make(chan model.Message),
		closeCh:         make(chan interface{}),
	}

	if conn.srvc.checkPreKeysInterval > 0 {
		// on first connect and periodically thereafter, check if a device is low on pre keys and request more
		go conn.warnPreKeysLowIfNecessary()
	}

	// handle messages outbound from the client and inbound to the client in full duplex
	go conn.handleOutbound()
	go conn.handleInbound()

	return conn, nil
}

func (conn *ClientConnection) Out() chan<- model.Message {
	return conn.out
}

func (conn *ClientConnection) Send(msg model.Message) {
	conn.out <- msg
}

func (conn *ClientConnection) In() <-chan model.Message {
	return conn.in
}

func (conn *ClientConnection) Receive() model.Message {
	return <-conn.in
}

// Drain drains all pending messages for a client and returns the number of messages drained
func (conn *ClientConnection) Drain() int {
	count := 0

	for {
		select {
		case <-conn.in:
			count++
		default:
			return count
		}
	}
}

func (conn *ClientConnection) Close() {
	conn.closeOnce.Do(func() {
		close(conn.closeCh)
	})
}

func (conn *ClientConnection) warnPreKeysLowIfNecessary() {
	for {
		numPreKeys, err := conn.srvc.db.PreKeysRemaining(conn.userID, conn.deviceID)
		if err == nil && numPreKeys < conn.srvc.lowPreKeysLimit {
			conn.send(conn.messageBuilder.NewPreKeysLow(uint16(conn.srvc.numPreKeysToRequest)))
		}
		select {
		case <-time.After(conn.srvc.checkPreKeysInterval):
			// okay
		case <-conn.closeCh:
			// stop
			return
		}
	}
}

func (conn *ClientConnection) handleInbound() {
	defer conn.subscriber.Close()

	ch := conn.subscriber.Messages()
	for {
		select {
		case <-conn.closeCh:
			return
		case brokerMsg := <-ch:
			msg := model.Message(brokerMsg.Data())
			if msg.Type() == model.TypeUserMessage {
				conn.messageBuilder.AttachNextSequence(msg)
				conn.unackedMessageMx.Lock()
				conn.unackedMessages[msg.Sequence()] = brokerMsg.Acker()
				conn.unackedMessageMx.Unlock()
			}
			conn.in <- msg
		}
	}
}

func (conn *ClientConnection) handleOutbound() {
	defer conn.Close()

	for msg := range conn.out {
		var err error
		switch msg.Type() {
		case model.TypeACK:
			conn.handleACK(msg)
		case model.TypeRegister:
			conn.handleRegister(msg)
		case model.TypeUnregister:
			conn.handleUnregister(msg)
		case model.TypeRequestPreKeys:
			conn.handleRequestPreKeys(msg)
		case model.TypeUserMessage:
			conn.handleUserMessage(msg)
		}
		if err != nil {
			log.Error(err)
			conn.in <- conn.messageBuilder.NewError(msg.Sequence(), model.TypedError(err))
		}
	}
}

func (conn *ClientConnection) handleACK(msg model.Message) {
	sequence := msg.Sequence()
	conn.unackedMessageMx.Lock()
	acker, found := conn.unackedMessages[sequence]
	conn.unackedMessageMx.Unlock()

	if !found {
		log.Errorf("no ack found for sequence %d", sequence)
		return
	}

	err := acker()
	if err != nil {
		log.Error(err)
		return
	}
	conn.unackedMessageMx.Lock()
	delete(conn.unackedMessages, sequence)
	conn.unackedMessageMx.Unlock()
}

func (conn *ClientConnection) handleRegister(msg model.Message) {
	register, err := msg.Register()
	if err != nil {
		conn.error(msg, err)
		return
	}

	err = conn.srvc.db.Register(conn.userID, conn.deviceID, register)
	if err != nil {
		conn.error(msg, err)
		return
	}

	conn.ack(msg)
}

func (conn *ClientConnection) handleUnregister(msg model.Message) {
	err := conn.srvc.db.Unregister(conn.userID, conn.deviceID)
	if err != nil {
		conn.error(msg, err)
		return
	}
	conn.ack(msg)
}

func (conn *ClientConnection) handleRequestPreKeys(msg model.Message) {
	request, err := msg.RequestPreKeys()
	if err != nil {
		conn.error(msg, err)
		return
	}

	// Get as many pre-keys as we can and handle the errors in here
	preKeys, err := conn.srvc.db.RequestPreKeys(request)
	if err != nil {
		conn.error(msg, err)
		return
	}

	for _, preKey := range preKeys {
		outMsg, err := conn.messageBuilder.NewPreKey(preKey)
		if err != nil {
			conn.error(msg, err)
			return
		}
		conn.send(outMsg)
	}
}

func (conn *ClientConnection) handleUserMessage(msg model.Message) {
	userMessage := msg.UserMessage()

	// get to address
	toUserID := userMessage.UserID()
	toDeviceID := userMessage.DeviceID()

	// update message with from address
	userMessage.SetUserID(conn.userID)
	userMessage.SetDeviceID(conn.deviceID)

	// publish
	publisher, err := conn.srvc.publisherFor(toUserID, toDeviceID)
	if err != nil {
		conn.error(msg, err)
		return
	}
	err = publisher.Publish(msg)
	if err != nil {
		conn.error(msg, err)
		return
	}
	conn.ack(msg)
}

func (srvc *Service) publisherFor(userID uuid.UUID, deviceID uint32) (broker.Publisher, error) {
	topic := topicFor(userID, deviceID)
	srvc.publisherCacheMx.Lock()
	defer srvc.publisherCacheMx.Unlock()
	_publisher, found := srvc.publisherCache.Get(topic)
	if found {
		return _publisher.(broker.Publisher), nil
	}
	publisher, err := srvc.broker.NewPublisher(topic)
	if err != nil {
		return nil, err
	}
	srvc.publisherCache.Add(topic, publisher)
	return publisher, nil
}

func topicFor(userID uuid.UUID, deviceID uint32) string {
	return fmt.Sprintf("%v|%d", userID.String(), deviceID)
}

func (conn *ClientConnection) ack(msg model.Message) {
	conn.send(conn.messageBuilder.Ack(msg))
}

func (conn *ClientConnection) error(msg model.Message, err error) {
	log.Error(err)
	conn.send(conn.messageBuilder.NewError(msg.Sequence(), model.TypedError(err)))
}

func (conn *ClientConnection) send(msg model.Message) {
	conn.in <- msg
}
