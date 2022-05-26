package serviceimpl

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	gerrors "errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	lru "github.com/hashicorp/golang-lru"
	"go.opentelemetry.io/otel/metric/global"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"

	"github.com/getlantern/errors"
	"github.com/getlantern/golog"
	"github.com/getlantern/trace"

	"github.com/getlantern/libmessaging-go/encoding"
	"github.com/getlantern/libmessaging-go/identity"
	"github.com/getlantern/tassis/attachments"
	"github.com/getlantern/tassis/broker"
	"github.com/getlantern/tassis/db"
	"github.com/getlantern/tassis/forwarder"
	"github.com/getlantern/tassis/model"
	"github.com/getlantern/tassis/presence"
	"github.com/getlantern/tassis/presence/mempresence"
	"github.com/getlantern/tassis/service"
)

var (
	log    = golog.LoggerFor("service")
	tracer = trace.NewTracer("service")
	meter  = global.Meter("github.com/getlantern/tassis")
)

type Opts struct {
	// The address at which this service publicly reachable. Used by other tasses to forward messages to us for consumption by our clients. Required.
	PublicAddr string
	// Domain for which this tassis registers short numbers
	ChatNumberDomain string
	// The DB to use for storing user device information and keys
	DB db.DB
	// The Broker to use for transmitting user messages between devices
	Broker broker.Broker
	// The Repository to use for advertising and finding user device presence (defaults to a local in-memory implementation)
	PresenceRepo presence.Repository
	// The Forwarder to use for forwarding user messages to other tasses. This should only be used for background tassis processes.
	Forwarder *forwarder.Forwarder
	// A manager for attachments
	AttachmentsManager attachments.Manager
	// How many publishers to cache (can reduce number connect attempts to broker for performance), defaults to 1
	PublisherCacheSize int
	// How frequently to check if a device's one time preKeys are getting low, defaults to 5 minutes
	CheckPreKeysInterval time.Duration
	// How many one time preKeys to consider low enough to warn the client, defaults to 10
	LowPreKeysLimit int
	// How many one time preKeys to request from the client when they get low, defaults to LowPreKeysLimit * 2
	NumPreKeysToRequest int
	// How many parallel routines to run for forwarding messages to other tasses, defaults to 1
	ForwardingParallelism int
	// If a message has been failing to forward for more than ForwardingTimeout, it is permanently discarded. Defaults to 1 hour.
	ForwardingTimeout time.Duration
	// Minimum interval for retrying messages that failed to forward, defaults to ForwardingTimeout / 60.
	MinForwardingRetryInterval time.Duration
	// Determines how frequently to check for users who have transferred and send over their messages. Set to 0 or less to disable. Should only be used in a background tassis process.
	UserTransferInterval time.Duration
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
	if opts.NumPreKeysToRequest <= 0 {
		opts.NumPreKeysToRequest = opts.LowPreKeysLimit * 2
		log.Debugf("Defaulted NumPreKeysToRequest to: %d", opts.NumPreKeysToRequest)
	}
	if opts.ForwardingParallelism <= 0 {
		opts.ForwardingParallelism = 1
		log.Debugf("Defaulted ForwardingParallelism to: %d", opts.ForwardingParallelism)
	}
	if opts.ForwardingTimeout == 0 {
		opts.ForwardingTimeout = 1 * time.Hour
		log.Debugf("Defaulted ForwardingTimeout to: %v", opts.ForwardingTimeout)
	}
	if opts.MinForwardingRetryInterval <= 0 {
		opts.MinForwardingRetryInterval = opts.ForwardingTimeout / 60
		log.Debugf("Defaulted MinForwardingRetryInterval to: %v", opts.MinForwardingRetryInterval)
	}
	if opts.PresenceRepo == nil {
		opts.PresenceRepo = mempresence.NewRepository()
		log.Debug("Defaulted to local in-memory presence repository")
	}
}

type Service struct {
	publicAddr                 string
	chatNumberDomain           string
	db                         db.DB
	broker                     broker.Broker
	presenceRepo               presence.Repository
	forwarder                  *forwarder.Forwarder
	attachmentsManager         attachments.Manager
	checkPreKeysInterval       time.Duration
	lowPreKeysLimit            int
	numPreKeysToRequest        int
	forwardingParallelism      int
	forwardingTimeout          time.Duration
	minForwardingRetryInterval time.Duration
	userTransferInterval       time.Duration
	publisherCache             *lru.Cache
	publisherCacheMx           sync.Mutex
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
		publicAddr:                 strings.ToLower(opts.PublicAddr),
		chatNumberDomain:           opts.ChatNumberDomain,
		db:                         opts.DB,
		broker:                     opts.Broker,
		presenceRepo:               opts.PresenceRepo,
		forwarder:                  opts.Forwarder,
		attachmentsManager:         opts.AttachmentsManager,
		publisherCache:             publisherCache,
		checkPreKeysInterval:       opts.CheckPreKeysInterval,
		lowPreKeysLimit:            opts.LowPreKeysLimit,
		numPreKeysToRequest:        opts.NumPreKeysToRequest,
		forwardingParallelism:      opts.ForwardingParallelism,
		forwardingTimeout:          opts.ForwardingTimeout,
		minForwardingRetryInterval: opts.MinForwardingRetryInterval,
		userTransferInterval:       opts.UserTransferInterval,
	}

	if srvc.forwarder != nil {
		log.Debug("will forward messages to other tasses as appropriate")
		err = srvc.startForwarding()
		if err != nil {
			return nil, errors.New("unable to start forwarding outbound messages: %v", err)
		}

		if srvc.userTransferInterval > 0 {
			err = srvc.startUserTransfers()
			if err != nil {
				return nil, errors.New("unable to start handling user transfers: %v", err)
			}
		}
	}

	return srvc, nil
}

type clientConnection struct {
	authNonce        []byte
	identityKey      atomic.Value
	deviceId         atomic.Value
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
	go conn.sendInitMessages()

	return conn, nil
}

func (conn *clientConnection) getIdentityKey() identity.PublicKey {
	identityKey := conn.identityKey.Load()
	if identityKey == nil {
		return nil
	}
	return identity.PublicKey(identityKey.([]byte))
}

func (conn *clientConnection) getDeviceId() []byte {
	deviceId := conn.deviceId.Load()
	if deviceId == nil {
		return nil
	}
	return deviceId.([]byte)
}

func (conn *clientConnection) isAuthenticated() bool {
	return len(conn.getIdentityKey()) != 0
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
		numPreKeys, err := conn.srvc.db.PreKeysRemaining(conn.getIdentityKey(), conn.getDeviceId())
		if err == nil && numPreKeys < conn.srvc.lowPreKeysLimit {
			conn.send(conn.mb.Build(&model.Message_PreKeysLow{PreKeysLow: &model.PreKeysLow{KeysRequested: uint32(conn.srvc.numPreKeysToRequest)}}))
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
	subscriber, err := conn.srvc.broker.NewSubscriber(topicFor(conn.getIdentityKey(), conn.getDeviceId()))
	if err != nil {
		return model.ErrUnableToOpenSubscriber.WithError(err)
	}

	go func() {
		defer func() {
			log.Debug("Closing subscriber")
			subscriber.Close()
			log.Debug("Closed subscriber")
		}()

		ch := subscriber.Messages()
		for {
			select {
			case <-conn.closeCh:
				return
			case brokerMsg := <-ch:
				conn.handleInboundMessage(brokerMsg)
			}
		}
	}()

	return nil
}

func (conn *clientConnection) handleInboundMessage(brokerMsg broker.Message) {
	_, span := tracer.Continue("inbound_message")
	defer span.End()

	msg := conn.mb.Build(&model.Message_InboundMessage{InboundMessage: &model.InboundMessage{UnidentifiedSenderMessage: brokerMsg.Data()}})
	conn.unackedMessageMx.Lock()
	conn.unackedMessages[msg.Sequence] = brokerMsg.Acker()
	conn.unackedMessageMx.Unlock()
	conn.in <- msg
}

func (conn *clientConnection) handleOutbound() {
	defer conn.Close()

	for msg := range conn.out {
		conn.doHandleOutboundMessage(msg)
	}
}

func (conn *clientConnection) doHandleOutboundMessage(msg *model.Message) {
	_, span := tracer.Continue("outbound_message")
	defer span.End()

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
	case *model.Message_RequestUploadAuthorizations:
		if conn.isAuthenticated() {
			err = model.ErrNonAnonymous
		} else {
			conn.handleRequestUploadAuthorizations(msg)
		}
	case *model.Message_FindChatNumberByShortNumber:
		if conn.isAuthenticated() {
			err = model.ErrNonAnonymous
		} else {
			conn.handleFindChatNumberByShortNumber(msg)
		}
	case *model.Message_FindChatNumberByIdentityKey:
		if conn.isAuthenticated() {
			err = model.ErrNonAnonymous
		} else {
			conn.handleFindChatNumberByIdentityKey(msg)
		}
	case *model.Message_OutboundMessage:
		if conn.isAuthenticated() {
			err = model.ErrNonAnonymous
		} else {
			conn.sendOutboundMessage(msg)
		}
	}
	if err != nil {
		span.RecordError(log.Error(err))
		conn.in <- conn.mb.NewError(msg, model.TypedError(err))
	}
}

func (conn *clientConnection) sendInitMessages() {
	conn.in <- conn.mb.Build(&model.Message_AuthChallenge{AuthChallenge: &model.AuthChallenge{Nonce: conn.authNonce}})
	conn.in <- conn.mb.Build(&model.Message_Configuration{Configuration: &model.Configuration{MaxAttachmentSize: conn.srvc.attachmentsManager.MaxAttachmentSize()}})
}

func (conn *clientConnection) handleAuthResponse(msg *model.Message) {
	_, span := tracer.Continue("auth_response")
	defer span.End()

	// unpack auth response
	authResponse := msg.GetAuthResponse()
	login := &model.Login{}
	err := proto.Unmarshal(authResponse.Login, login)
	if err != nil {
		conn.error(span, msg, err)
		conn.Close()
		return
	}

	// verify nonce
	if subtle.ConstantTimeCompare(conn.authNonce, login.Nonce) != 1 {
		conn.error(span, msg, model.ErrUnauthorized)
		conn.Close()
		return
	}

	// verify signature
	address := login.Address
	identityKey := identity.PublicKey(address.IdentityKey)
	if !identityKey.Verify(authResponse.Login, authResponse.Signature) {
		conn.error(span, msg, model.ErrUnauthorized)
		conn.Close()
		return
	}

	conn.identityKey.Store(address.IdentityKey)
	conn.deviceId.Store(address.DeviceId)

	// get the number
	number, shortNumber, err := conn.getNumber()
	if err != nil {
		conn.error(span, msg, err)
		conn.Close()
	}

	err = conn.startHandlingInbound()
	if err != nil {
		conn.error(span, msg, err)
		return
	}
	if conn.srvc.checkPreKeysInterval > 0 {
		// on first connect and periodically thereafter, check if a device is low on pre keys and request more
		go conn.warnPreKeysLowIfNecessary()
	}

	// Send back a Number with information about our number
	outMsg := &model.Message{
		Sequence: msg.Sequence,
		Payload: &model.Message_ChatNumber{
			ChatNumber: &model.ChatNumber{
				Number:      number,
				ShortNumber: shortNumber,
				Domain:      conn.srvc.chatNumberDomain,
			},
		},
	}
	conn.send(outMsg)
}

func (conn *clientConnection) getNumber() (string, string, error) {
	identityKey := conn.getIdentityKey()
	preferredNumber := identityKey.Number()
	iter := 0
	for {
		newNumber := preferredNumber
		newNumberTail := newNumber[12:]
		newShortNumber := newNumber[:12]
		for i := 0; i < iter; i++ {
			// try adding some 5's to the number to find a unique one (5s are ignored when parsing numbers)
			newShortNumber = "5" + newShortNumber
			newNumberTail = "5" + newNumberTail
		}
		newNumber = fmt.Sprintf("%s%s", newShortNumber, newNumberTail)
		lastChance := false
		if len(newShortNumber) >= len(preferredNumber) {
			// we've gotten to the point where the short number isn't any shorter than the whole number, so just
			// use the whole number
			newNumber = preferredNumber
			newShortNumber = preferredNumber
			lastChance = true
		}
		number, shortNumber, err := conn.srvc.db.RegisterChatNumber(identityKey, newNumber, newShortNumber)
		if err == nil {
			return number, shortNumber, nil
		}
		if gerrors.Is(err, model.ErrNumberTaken) && !lastChance {
			// this number is taken, try again with a different version of number
			iter++
			continue
		}
		return "", "", err
	}
}

func (conn *clientConnection) handleACK(msg *model.Message) {
	_, span := tracer.Continue("ack")
	defer span.End()

	sequence := msg.Sequence
	conn.unackedMessageMx.Lock()
	acker, found := conn.unackedMessages[sequence]
	conn.unackedMessageMx.Unlock()

	if !found {
		span.RecordError(log.Errorf("no ack found for sequence %d", sequence))
		return
	}

	err := acker()
	if err != nil {
		span.RecordError(log.Error(err))
		return
	}
	conn.unackedMessageMx.Lock()
	delete(conn.unackedMessages, sequence)
	conn.unackedMessageMx.Unlock()
}

func (conn *clientConnection) handleRegister(msg *model.Message) {
	_, span := tracer.Continue("register")
	defer span.End()

	identityKey, deviceId := conn.getIdentityKey(), conn.getDeviceId()

	register := msg.GetRegister()

	err := conn.srvc.presenceRepo.Announce(&model.Address{IdentityKey: identityKey, DeviceId: deviceId}, conn.srvc.publicAddr)
	if err != nil {
		conn.error(span, msg, err)
		return
	}

	err = conn.srvc.db.Register(identityKey, deviceId, register)
	if err != nil {
		conn.error(span, msg, err)
		return
	}

	conn.ack(msg)
}

func (conn *clientConnection) handleUnregister(msg *model.Message) {
	_, span := tracer.Continue("unregister")
	defer span.End()

	err := conn.srvc.db.Unregister(conn.getIdentityKey(), conn.getDeviceId())
	if err != nil {
		conn.error(span, msg, err)
		return
	}
	conn.ack(msg)
}

func (conn *clientConnection) handleRequestPreKeys(msg *model.Message) {
	_, span := tracer.Continue("request_pre_keys")
	defer span.End()

	request := msg.GetRequestPreKeys()

	// Get as many pre-keys as we can and handle the errors in here
	preKeys, err := conn.srvc.db.RequestPreKeys(request)
	if err != nil {
		conn.error(span, msg, err)
		return
	}

	outMsg := &model.Message{
		Sequence: msg.Sequence,
		Payload:  &model.Message_PreKeys{PreKeys: &model.PreKeys{PreKeys: preKeys}},
	}
	conn.send(outMsg)
}

func (conn *clientConnection) handleRequestUploadAuthorizations(msg *model.Message) {
	_, span := tracer.Continue("request_upload_authorizations")
	defer span.End()

	request := msg.GetRequestUploadAuthorizations()

	// Get as many authorizations as we can
	uploadAuthorizations := &model.UploadAuthorizations{}
	var finalErr error
	for i := int32(0); i < request.NumRequested; i++ {
		auth, err := conn.srvc.attachmentsManager.AuthorizeUpload()
		if err == nil {
			uploadAuthorizations.Authorizations = append(uploadAuthorizations.Authorizations, auth)
		} else {
			finalErr = err
		}
	}
	if len(uploadAuthorizations.Authorizations) == 0 {
		conn.error(span, msg, finalErr)
		return
	}

	outMsg := &model.Message{
		Sequence: msg.Sequence,
		Payload:  &model.Message_UploadAuthorizations{UploadAuthorizations: uploadAuthorizations},
	}
	conn.send(outMsg)
}

func (conn *clientConnection) handleFindChatNumberByShortNumber(msg *model.Message) {
	_, span := tracer.Continue("find_chat_number_by_short_number")
	defer span.End()

	shortNumber := msg.GetFindChatNumberByShortNumber().ShortNumber
	number, err := conn.srvc.db.FindChatNumberByShortNumber(shortNumber)
	if err != nil {
		conn.error(span, msg, err)
		return
	}

	outMsg := &model.Message{
		Sequence: msg.Sequence,
		Payload: &model.Message_ChatNumber{
			ChatNumber: &model.ChatNumber{
				Number:      number,
				ShortNumber: shortNumber,
				Domain:      conn.srvc.chatNumberDomain,
			},
		},
	}
	conn.send(outMsg)
}

func (conn *clientConnection) handleFindChatNumberByIdentityKey(msg *model.Message) {
	_, span := tracer.Continue("find_chat_number_by_identity_key")
	defer span.End()

	identityKey := msg.GetFindChatNumberByIdentityKey().IdentityKey
	number, shortNumber, err := conn.srvc.db.FindChatNumberByIdentityKey(identityKey)
	if err != nil {
		conn.error(span, msg, err)
		return
	}

	outMsg := &model.Message{
		Sequence: msg.Sequence,
		Payload: &model.Message_ChatNumber{
			ChatNumber: &model.ChatNumber{
				Number:      number,
				ShortNumber: shortNumber,
				Domain:      conn.srvc.chatNumberDomain,
			},
		},
	}
	conn.send(outMsg)
}

func (conn *clientConnection) sendOutboundMessage(msg *model.Message) {
	_, span := tracer.Continue("send_outbound_message")
	defer span.End()

	outboundMessage := msg.GetOutboundMessage()

	tassisHost, err := conn.srvc.presenceRepo.Find(outboundMessage.To)
	if err != nil {
		conn.error(span, msg, err)
		return
	}
	tassisHost = strings.ToLower(tassisHost)

	topic := topicFor(outboundMessage.To.IdentityKey, outboundMessage.To.DeviceId)
	data := outboundMessage.GetUnidentifiedSenderMessage()
	if conn.srvc.publicAddr != tassisHost {
		// this is a federated message, write it to a forwarding topic
		topic = forwardingTopic
		data, err = proto.Marshal(&model.ForwardedMessage{
			Message: outboundMessage,
		})
		if err != nil {
			conn.error(span, msg, err)
			return
		}
	}

	// publish
	publisher, err := conn.srvc.publisherFor(topic)
	if err != nil {
		conn.error(span, msg, err)
		return
	}
	err = publisher.Publish(data)
	if err != nil {
		conn.error(span, msg, err)
		return
	}
	conn.ack(msg)
	messagesSent, err := meter.SyncFloat64().Counter("messages_sent")
	if err != nil {
		log.Errorf("Unable to initialize messagesSent counter, will not track number of messages sent: %v", err)
	} else {
		messagesSent.Add(context.Background(), 1)
	}
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

func topicFor(identityKey identity.PublicKey, deviceId []byte) string {
	return fmt.Sprintf("%v:%v", identityKey.String(), encoding.HumanFriendlyBase32Encoding.EncodeToString(deviceId))
}

func topicForAddr(addr *model.Address) string {
	return topicFor(identity.PublicKey(addr.IdentityKey), addr.DeviceId)
}

func (conn *clientConnection) ack(msg *model.Message) {
	conn.send(conn.mb.NewAck(msg))
}

func (conn *clientConnection) error(span oteltrace.Span, msg *model.Message, err error) {
	span.RecordError(log.Error(err))
	conn.send(conn.mb.NewError(msg, model.TypedError(err)))
}

func (conn *clientConnection) send(msg *model.Message) {
	conn.in <- msg
}
