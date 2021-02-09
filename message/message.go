package message

import (
	"encoding/binary"

	"github.com/getlantern/msgpack"
	"github.com/google/uuid"
)

const (
	LatestVersion = 1
)

const (
	TypeRegister            = 1
	TypeUnregister          = 2
	TypeRequestPreKeys      = 3
	TypePreKey              = 4
	TypeOutboundUserMessage = 5
	TypeInboundUserMessage  = 6
)

var (
	enc = binary.LittleEndian // typical byte order for most CPU architectures
)

type Version uint8

type Type uint8

// Message is a message encoded as follows:
//
//   +---------+------+----------------+--------------+
//   | Version | Type | Payload Length |    Payload   |
//   +---------+------+----------------+--------------+
//   |    1    |  1   |        4       | <=4294967296 |
//   +---------+------+----------------+--------------+
//
// All multi-byte numberic values are encoded in Little Endian byte order.
//
type Message []byte

// New constructs a new message
func New(msgType Type, payload []byte) Message {
	payloadLength := len(payload)
	msg := make(Message, 6+payloadLength)
	msg[0] = byte(LatestVersion)
	msg[1] = byte(msgType)
	enc.PutUint32(msg[2:], uint32(payloadLength))
	copy(msg[6:], payload)
	return msg
}

func (msg Message) Version() Version {
	return Version(msg[0])
}

func (msg Message) Type() Type {
	return Type(msg[1])
}

func (msg Message) PayloadLength() int {
	return int(enc.Uint32(msg[2:]))
}

func (msg Message) Payload() []byte {
	return msg[6 : 6+msg.PayloadLength()]
}

func newMessagePacked(msgType Type, msg interface{}) (Message, error) {
	payload, err := msgpack.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return New(msgType, payload), nil
}

// Register is a message that a client sends to register a user and device with one or more pre-keys. It is encoded with MessagePack.
type Register struct {
	UserID         string
	DeviceID       uint32
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

func NewRegister(msg *Register) (Message, error) {
	return newMessagePacked(TypeRegister, msg)
}

// Unregister is a message that a client sends to unregister a specific device for a user. It is encoded with MessagePack.
type Unregister struct {
	UserID   string
	DeviceID uint32
}

func (msg Message) Unregister() (*Unregister, error) {
	result := &Unregister{}
	err := msgpack.Unmarshal(msg.Payload(), result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func NewUnregister(msg *Unregister) (Message, error) {
	return newMessagePacked(TypeUnregister, msg)
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

func NewRequestPreKeys(msg *RequestPreKeys) (Message, error) {
	return newMessagePacked(TypeRequestPreKeys, msg)
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

func NewPreKey(msg *PreKey) (Message, error) {
	return newMessagePacked(TypePreKey, msg)
}

// OutboundUserMessage is a message outbound from one user to another encoded as follows:
//
//   +-----+------+-------------+
//   | To  | From | Cipher Text |
//   +-----+------+-------------+
//   | 128 | 128  |   variable  |
//   +-----+------+-------------+
//
// To and From are user IDs (128 bit type 4 UUIDs)
//
type OutboundUserMessage []byte

func (msg Message) OutboundUserMessage() OutboundUserMessage {
	return OutboundUserMessage(msg.Payload())
}

func NewOutboundUserMessage(to uuid.UUID, from uuid.UUID, cipherText []byte) Message {
	msg := make(OutboundUserMessage, 32+len(cipherText))
	copy(msg, to[:])
	copy(msg[16:], from[:])
	copy(msg[32:], cipherText)
	return New(TypeOutboundUserMessage, msg)
}

func (msg OutboundUserMessage) To() uuid.UUID {
	id := uuid.UUID{}
	copy(id[:], msg[:16])
	return id
}

func (msg OutboundUserMessage) From() uuid.UUID {
	id := uuid.UUID{}
	copy(id[:], msg[16:32])
	return id
}

func (msg OutboundUserMessage) CipherText() []byte {
	return msg[32:]
}

func (msg OutboundUserMessage) ToInbound() InboundUserMessage {
	return InboundUserMessage(msg[16:])
}

// InboundUserMessage is a message inbound from another user as follows:
//
//   +------+-------------+
//   | From | Cipher Text |
//   +------+-------------+
//   | 128  |   variable  |
//   +------+-------------+
//
// From is a user ID (128 bit type 4 UUID)
//
type InboundUserMessage []byte

func NewInboundUserMessage(msg InboundUserMessage) Message {
	return New(TypeInboundUserMessage, msg)
}

func (msg Message) InboundUserMessage() InboundUserMessage {
	return InboundUserMessage(msg.Payload())
}

func (msg InboundUserMessage) From() uuid.UUID {
	id := uuid.UUID{}
	copy(id[:], msg[:16])
	return id
}

func (msg InboundUserMessage) CipherText() []byte {
	return msg[16:]
}
