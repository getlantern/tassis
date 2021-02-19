package serviceimpl

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	lru "github.com/hashicorp/golang-lru"
	"google.golang.org/protobuf/proto"

	"github.com/getlantern/errors"
	"github.com/getlantern/golog"

	"github.com/getlantern/tassis/broker"
	"github.com/getlantern/tassis/db"
	"github.com/getlantern/tassis/identity"
	"github.com/getlantern/tassis/model"
	"github.com/getlantern/tassis/presence"
	"github.com/getlantern/tassis/presence/mempresence"
	"github.com/getlantern/tassis/service"
)

var (
	log = golog.LoggerFor("service")
)

type Opts struct {
	PublicAddr           string
	DB                   db.DB
	Broker               broker.Broker
	PresenceRepo         presence.Repository
	PublisherCacheSize   int
	CheckPreKeysInterval time.Duration
	LowPreKeysLimit      int
	NumPreKeysToRequest  int
	ForwardingTimeout    time.Duration
}

func (opts *Opts) ApplyDefaults() {
	if opts.PublisherCacheSize <= 0 {
		opts.PublisherCacheSize = 1
		log.Debugf("Defaulted PublisherCacheSize to: %d", opts.PublisherCacheSize)
	}
	if opts.CheckPreKeysInterval == 0 {
		opts.CheckPreKeysInterval = 5 * time.Minute
		log.Debugf("Defaulted CheckPreKeysInterval to: %v", opts.CheckPreKeysInterval)
	}
	if opts.LowPreKeysLimit == 0 {
		opts.LowPreKeysLimit = 10
		log.Debugf("Defaulted LowPreKeysLimit to: %d", opts.LowPreKeysLimit)
	}
	if opts.NumPreKeysToRequest == 0 {
		opts.NumPreKeysToRequest = opts.LowPreKeysLimit * 2
		log.Debugf("Defaulted NumPreKeysToRequest to: %d", opts.NumPreKeysToRequest)
	}
	if opts.ForwardingTimeout == 0 {
		opts.ForwardingTimeout = 1 * time.Hour
		log.Debugf("Defaulted ForwardingTimeout to: %v", opts.ForwardingTimeout)
	}
	if opts.PresenceRepo == nil {
		opts.PresenceRepo = mempresence.NewRepository()
		log.Debug("Defaulted to local in-memory presence repository")
	}
}

type Service struct {
	publicAddr           string
	db                   db.DB
	broker               broker.Broker
	presenceRepo         presence.Repository
	checkPreKeysInterval time.Duration
	lowPreKeysLimit      int
	numPreKeysToRequest  int
	forwardingTimeout    time.Duration
	publisherCache       *lru.Cache
	publisherCacheMx     sync.Mutex
	// forwarders           map[string]*forwarder
}

func New(opts *Opts) (*Service, error) {
	opts.ApplyDefaults()
	if opts.PublicAddr == "" {
		return nil, errors.New("please specify a PublicAddr for this Service")
	}
	publisherCache, err := lru.NewWithEvict(opts.PublisherCacheSize, func(key, value interface{}) {
		value.(broker.Publisher).Close()
	})
	if err != nil {
		return nil, err
	}
	srvc := &Service{
		publicAddr:     strings.ToLower(opts.PublicAddr),
		db:             opts.DB,
		broker:         opts.Broker,
		presenceRepo:   opts.PresenceRepo,
		publisherCache: publisherCache,
		// forwarders:           make(map[string]*forwarder),
		checkPreKeysInterval: opts.CheckPreKeysInterval,
		lowPreKeysLimit:      opts.LowPreKeysLimit,
		numPreKeysToRequest:  opts.NumPreKeysToRequest,
		forwardingTimeout:    opts.ForwardingTimeout,
	}
	// handle messages to federated tasses
	// err = srvc.startHandlingFederatedOutbound()
	// if err != nil {
	// 	return nil, errors.New("unable to start handling federated outbound messages: %v", err)
	// }
	return srvc, nil
}

type clientConnection struct {
	authNonce        []byte
	userID           atomic.Value
	deviceID         atomic.Value
	srvc             *Service
	mb               *model.MessageBuilder
	unackedMessages  map[uint32]func() error
	unackedMessageMx sync.Mutex
	out              chan *model.Message
	in               chan *model.Message
	closeCh          chan interface{}
	closeOnce        sync.Once
}

// Connect connects a user to the service, returning a ClientConnection with
// channels for sending messages to the service and receiving messages from it.
// When the client wishes to disconnect, it should close the ClientConnection.
//
// Until the client sends a Login message, the connection will be unauthenticated and anonymous.
// An unauthenticated connection can be used only for requesting pre-keys and
// sending outbound messages with a sealed sender.
//
// A client my log in by sending a login message signed by their public key.
// Once authenticated, a connection can be used for everything except for sending
// messages or retrieving pre-keys, which are required to be performed anonymously.
func (srvc *Service) Connect() (service.ClientConnection, error) {
	// TODO: instrument number of open clientConnections
	// TODO: rate limit, especially on unauthenticated connections

	authNonce := make([]byte, 32)
	_, err := rand.Read(authNonce)
	if err != nil {
		return nil, err
	}

	conn := &clientConnection{
		authNonce:       authNonce,
		srvc:            srvc,
		mb:              &model.MessageBuilder{},
		unackedMessages: make(map[uint32]func() error),
		out:             make(chan *model.Message),
		in:              make(chan *model.Message),
		closeCh:         make(chan interface{}),
	}

	// handle messages outbound from the client
	go conn.handleOutbound()
	go conn.sendAuthChallenge()

	return conn, nil
}

func (conn *clientConnection) getUserID() []byte {
	userID := conn.userID.Load()
	if userID == nil {
		return nil
	}
	return userID.([]byte)
}

func (conn *clientConnection) getDeviceID() uint32 {
	deviceID := conn.deviceID.Load()
	if deviceID == nil {
		return 0
	}
	return deviceID.(uint32)
}

func (conn *clientConnection) isAuthenticated() bool {
	return len(conn.getUserID()) != 0
}

func (conn *clientConnection) Out() chan<- *model.Message {
	return conn.out
}

func (conn *clientConnection) Send(msg *model.Message) {
	conn.out <- msg
}

func (conn *clientConnection) In() <-chan *model.Message {
	return conn.in
}

func (conn *clientConnection) Receive() *model.Message {
	return <-conn.in
}

// Drain drains all pending messages for a client and returns the number of messages drained
func (conn *clientConnection) Drain() int {
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

func (conn *clientConnection) Close() {
	conn.closeOnce.Do(func() {
		close(conn.closeCh)
	})
}

func (conn *clientConnection) warnPreKeysLowIfNecessary() {
	for {
		numPreKeys, err := conn.srvc.db.PreKeysRemaining(conn.getUserID(), conn.getDeviceID())
		if err == nil && numPreKeys < conn.srvc.lowPreKeysLimit {
			conn.send(conn.mb.Build(&model.Message_PreKeysLow{&model.PreKeysLow{KeysRequested: uint32(conn.srvc.numPreKeysToRequest)}}))
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

func (conn *clientConnection) startHandlingInbound() error {
	subscriber, err := conn.srvc.broker.NewSubscriber(topicFor(conn.getUserID(), conn.getDeviceID()))
	if err != nil {
		return model.ErrUnableToOpenSubscriber.WithError(err)
	}

	go func() {
		defer subscriber.Close()

		ch := subscriber.Messages()
		for {
			select {
			case <-conn.closeCh:
				return
			case brokerMsg := <-ch:
				msg := conn.mb.Build(&model.Message_InboundMessage{InboundMessage: brokerMsg.Data()})
				conn.unackedMessageMx.Lock()
				conn.unackedMessages[msg.Sequence] = brokerMsg.Acker()
				conn.unackedMessageMx.Unlock()
				conn.in <- msg
			}
		}
	}()

	return nil
}

func (conn *clientConnection) handleOutbound() {
	defer conn.Close()

	for msg := range conn.out {
		var err error
		switch msg.Payload.(type) {
		case *model.Message_Ack:
			conn.handleACK(msg)
		case *model.Message_AuthResponse:
			if conn.isAuthenticated() {
				err = model.ErrNonAnonymous
			} else {
				conn.handleAuthResponse(msg)
			}
		case *model.Message_Register:
			if !conn.isAuthenticated() {
				err = model.ErrUnauthorized
			} else {
				conn.handleRegister(msg)
			}
		case *model.Message_Unregister:
			if !conn.isAuthenticated() {
				err = model.ErrUnauthorized
			} else {
				conn.handleUnregister(msg)
			}
		case *model.Message_RequestPreKeys:
			if conn.isAuthenticated() {
				err = model.ErrNonAnonymous
			} else {
				conn.handleRequestPreKeys(msg)
			}
		case *model.Message_OutboundMessage:
			if conn.isAuthenticated() {
				err = model.ErrNonAnonymous
			} else {
				conn.handleOutboundMessage(msg)
			}
		}
		if err != nil {
			log.Error(err)
			conn.in <- conn.mb.NewError(msg, model.TypedError(err))
		}
	}
}

func (conn *clientConnection) sendAuthChallenge() {
	conn.in <- conn.mb.Build(&model.Message_AuthChallenge{&model.AuthChallenge{Nonce: conn.authNonce}})
}

func (conn *clientConnection) handleAuthResponse(msg *model.Message) {
	// unpack auth response
	authResponse := msg.GetAuthResponse()
	login := &model.Login{}
	err := proto.Unmarshal(authResponse.Login, login)
	if err != nil {
		conn.error(msg, err)
		conn.Close()
		return
	}

	// verify nonce
	if !bytes.Equal(conn.authNonce, login.Nonce) {
		conn.error(msg, model.ErrUnauthorized)
		conn.Close()
		return
	}

	// verify signature
	address := login.Address
	publicKey := identity.UserID(address.UserID).PublicKey()
	if !publicKey.Verify(authResponse.Login, authResponse.Signature) {
		conn.error(msg, model.ErrUnauthorized)
		conn.Close()
		return
	}

	conn.userID.Store(address.UserID)
	conn.deviceID.Store(address.DeviceID)

	err = conn.startHandlingInbound()
	if err != nil {
		conn.error(msg, err)
		conn.Close()
		return
	}
	if conn.srvc.checkPreKeysInterval > 0 {
		// on first connect and periodically thereafter, check if a device is low on pre keys and request more
		go conn.warnPreKeysLowIfNecessary()
	}

	conn.ack(msg)
}

func (conn *clientConnection) handleACK(msg *model.Message) {
	sequence := msg.Sequence
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

func (conn *clientConnection) handleRegister(msg *model.Message) {
	userID, deviceID := conn.getUserID(), conn.getDeviceID()

	register := msg.GetRegister()

	err := conn.srvc.presenceRepo.Announce(&model.Address{UserID: userID, DeviceID: deviceID}, conn.srvc.publicAddr)
	if err != nil {
		conn.error(msg, err)
		return
	}

	err = conn.srvc.db.Register(userID, deviceID, register)
	if err != nil {
		conn.error(msg, err)
		return
	}

	conn.ack(msg)
}

func (conn *clientConnection) handleUnregister(msg *model.Message) {
	err := conn.srvc.db.Unregister(conn.getUserID(), conn.getDeviceID())
	if err != nil {
		conn.error(msg, err)
		return
	}
	conn.ack(msg)
}

func (conn *clientConnection) handleRequestPreKeys(msg *model.Message) {
	request := msg.GetRequestPreKeys()

	// Get as many pre-keys as we can and handle the errors in here
	preKeys, err := conn.srvc.db.RequestPreKeys(request)
	if err != nil {
		conn.error(msg, err)
		return
	}

	for _, preKey := range preKeys {
		outMsg := conn.mb.Build(&model.Message_PreKey{preKey})
		conn.send(outMsg)
	}
}

func (conn *clientConnection) handleOutboundMessage(msg *model.Message) {
	outboundMessage := msg.GetOutboundMessage()

	host, err := conn.srvc.presenceRepo.Find(outboundMessage.To)
	if err != nil {
		conn.error(msg, err)
		return
	}
	host = strings.ToLower(host)

	topic := topicFor(outboundMessage.To.UserID, outboundMessage.To.DeviceID)
	data := outboundMessage.GetUnidentifiedSenderMessage()
	// if conn.srvc.publicAddr != host {
	// 	// this is a federated message, write it to a forwarding topic
	// 	topic = forwardingTopic
	// 	data, err = proto.Marshal(&model.ForwardedMessage{
	// 		ForwardTo: host,
	// 		Message:   outboundMessage,
	// 	})
	// 	if err != nil {
	// 		conn.error(msg, err)
	// 		return
	// 	}
	// }

	// publish
	publisher, err := conn.srvc.publisherFor(topic)
	if err != nil {
		conn.error(msg, err)
		return
	}
	err = publisher.Publish(data)
	if err != nil {
		conn.error(msg, err)
		return
	}
	conn.ack(msg)
}

func (srvc *Service) publisherFor(topic string) (broker.Publisher, error) {
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

func topicFor(userID identity.UserID, deviceID uint32) string {
	return fmt.Sprintf("%v:%d", userID.String(), deviceID)
}

func (conn *clientConnection) ack(msg *model.Message) {
	conn.send(conn.mb.NewAck(msg))
}

func (conn *clientConnection) error(msg *model.Message, err error) {
	log.Error(err)
	conn.send(conn.mb.NewError(msg, model.TypedError(err)))
}

func (conn *clientConnection) send(msg *model.Message) {
	conn.in <- msg
}
