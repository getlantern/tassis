package model

import (
	"encoding/binary"
	"sync/atomic"

	"github.com/getlantern/msgpack"
	"github.com/google/uuid"
)

const (
	LatestVersion = 1
)

const (
	TypeACK            = 1
	TypeRegister       = 2
	TypeUnregister     = 3
	TypeRequestPreKeys = 4
	TypePreKey         = 5
	TypePreKeysLow     = 6
	TypeUserMessage    = 7
	TypeError          = 8
)

var (
	enc = binary.LittleEndian // typical byte order for most CPU architectures
)

type Version uint8

type Sequence uint32

type Type uint8

// Message is a message encoded as follows:
//
//   +---------+----------+------+----------------+--------------+
//   | Version | Sequence | Type | Payload Length |    Payload   |
//   +---------+----------+------+----------------+--------------+
//   |    1    |     4    |  1   |        4       | <=4294967296 |
//   +---------+----------+------+----------------+--------------+
//
// All multi-byte numeric values are encoded in Little Endian byte order.
//
type Message []byte

func (msg Message) Version() Version {
	return Version(msg[0])
}

func (msg Message) Sequence() Sequence {
	return Sequence(enc.Uint32(msg[1:]))
}

func (msg Message) SetSequence(sequence Sequence) {
	enc.PutUint32(msg[1:], uint32(sequence))
}

func (msg Message) Type() Type {
	return Type(msg[5])
}

func (msg Message) PayloadLength() int {
	return int(enc.Uint32(msg[6:]))
}

func (msg Message) Payload() []byte {
	return msg[10 : 10+msg.PayloadLength()]
}

type MessageBuilder struct {
	seq uint32
}

// NewMessage constructs a new message
func (b *MessageBuilder) NewMessage(msgType Type, payload []byte) Message {
	payloadLength := len(payload)
	msg := make(Message, 10+payloadLength)
	msg[0] = byte(LatestVersion)
	b.AttachNextSequence(msg)
	msg[5] = byte(msgType)
	enc.PutUint32(msg[6:], uint32(payloadLength))
	copy(msg[10:], payload)
	return msg
}

// AttachNextSequence attaches the next sequence number to the given message
func (b *MessageBuilder) AttachNextSequence(msg Message) {
	msg.SetSequence(Sequence(atomic.AddUint32(&b.seq, 1)))
}

func (b *MessageBuilder) newMessagePacked(msgType Type, msg interface{}) (Message, error) {
	payload, err := msgpack.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return b.NewMessage(msgType, payload), nil
}

// Ack is a message that one end or the other sends to acknowledge durable receipt of a message based on its sequence number. It simply uses the sequence number
// field of the Message envelope as the acked sequence number (i.e. acks don't have their own sequence numbers)
func (b *MessageBuilder) Ack(msg Message) Message {
	return b.NewAck(msg.Sequence())
}

func (b *MessageBuilder) NewAck(sequence Sequence) Message {
	msg := b.NewMessage(TypeACK, nil)
	msg.SetSequence(sequence)
	return msg
}

// Register is a message that a client sends to register a user and device with one or more pre-keys. It is encoded with MessagePack.
type Register struct {
	RegistrationID uint32
	IdentityKey    []byte
	SignedPreKey   []byte
	PreKeys        [][]byte
}

func (msg Message) Register() (*Register, error) {
	result := &Register{}
	err := msgpack.Unmarshal(msg.Payload(), result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (b *MessageBuilder) NewRegister(msg *Register) (Message, error) {
	return b.newMessagePacked(TypeRegister, msg)
}

// RequestPreKeys is a message that a client sends to request pre keys for all of a user's device about which it does not yet know. It is encoded with MessagePack.
type RequestPreKeys struct {
	UserID         string
	KnownDeviceIDs []uint32
}

func (msg Message) RequestPreKeys() (*RequestPreKeys, error) {
	result := &RequestPreKeys{}
	err := msgpack.Unmarshal(msg.Payload(), result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (b *MessageBuilder) NewRequestPreKeys(msg *RequestPreKeys) (Message, error) {
	return b.newMessagePacked(TypeRequestPreKeys, msg)
}

// PreKey is a message with pre key information for a specific device. It is encoded with MessagePack.
type PreKey struct {
	UserID         string
	DeviceID       uint32
	RegistrationID uint32
	IdentityKey    []byte
	SignedPreKey   []byte
	PreKey         []byte
}

func (msg Message) PreKey() (*PreKey, error) {
	result := &PreKey{}
	err := msgpack.Unmarshal(msg.Payload(), result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (b *MessageBuilder) NewPreKey(msg *PreKey) (Message, error) {
	return b.newMessagePacked(TypePreKey, msg)
}

// PreKeysLow is a message indicating that more pre-keys are needed
//
//   +--------------------+
//   | Num Keys Requested |
//   +--------------------+
//   |         2          |
//   +--------------------+
//
// All multi-byte numeric values are encoded in Little Endian byte order.
//
type PreKeysLow []byte

func (b *MessageBuilder) NewPreKeysLow(numKeysRequested uint16) Message {
	msg := make(PreKeysLow, 2)
	enc.PutUint16(msg, numKeysRequested)
	return b.NewMessage(TypePreKeysLow, msg)
}

func (msg Message) PreKeysLow() PreKeysLow {
	return PreKeysLow(msg.Payload())
}

func (msg PreKeysLow) NumKeysRequested() uint16 {
	return enc.Uint16(msg)
}

// UserMessage is a message to/from another user, encoded as follows:
//
//   +---------+-------------+
//   | To/From | Cipher Text |
//   +---------+-------------+
//   |   128   |   variable  |
//   +---------+-------------+
//
// To/From is a user ID (128 bit type 4 UUID)
//
type UserMessage []byte

func (b *MessageBuilder) NewUserMessage(toFrom uuid.UUID, cipherText []byte) Message {
	msg := make(UserMessage, 16+len(cipherText))
	msg.SetToFrom(toFrom)
	copy(msg[16:], cipherText)
	return b.NewMessage(TypeUserMessage, msg)
}

func (msg Message) UserMessage() UserMessage {
	return UserMessage(msg.Payload())
}

func (msg UserMessage) ToFrom() uuid.UUID {
	id := uuid.UUID{}
	copy(id[:], msg[:16])
	return id
}

func (msg UserMessage) CipherText() []byte {
	return msg[16:]
}

func (msg UserMessage) SetToFrom(id uuid.UUID) {
	copy(msg, id[:])
}
