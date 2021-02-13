package model

import (
	"encoding/base32"
	"fmt"
	"sync/atomic"
)

var (
	userIDEncoding = base32.NewEncoding("ybndrfg8ejkmcpqxot1uw2sza345h769").WithPadding(base32.NoPadding)
)

var (
	ErrUnknown = &Error{
		Name: "unknown_error",
	}

	ErrInvalidUserID = &Error{
		Name: "invalid_user_id",
	}

	ErrUnknownUser = &Error{
		Name: "unknown_user",
	}

	ErrUnknownDevice = &Error{
		Name: "unknown_device",
	}
)

func (err *Error) Error() string {
	return fmt.Sprintf("%s|%s", err.Name, err.Description)
}

func (err *Error) WithDescription(description string) *Error {
	return &Error{
		Name:        err.Name,
		Description: description,
	}
}

func (err *Error) Equals(other *Error) bool {
	return err.Name == other.Name && err.Description == other.Description
}

func TypedError(err error) *Error {
	typed, ok := err.(*Error)
	if ok {
		return typed
	}
	return ErrUnknown.WithDescription(err.Error())
}

func UserIDToString(userID []byte) string {
	return userIDEncoding.EncodeToString(userID)
}

func StringToUserID(userID string) ([]byte, error) {
	return userIDEncoding.DecodeString(userID)
}

type MessageBuilder struct {
	seq uint32
}

// AttachNextSequence attaches the next sequence number to the given message
func (mb *MessageBuilder) AttachNextSequence(msg *Message) {
	msg.Sequence = atomic.AddUint32(&mb.seq, 1)
}

func (mb *MessageBuilder) Build(payload isMessage_Payload) *Message {
	return &Message{
		Sequence: atomic.AddUint32(&mb.seq, 1),
		Payload:  payload,
	}
}

func (mb *MessageBuilder) NewAck(orig *Message) *Message {
	return &Message{
		Sequence: orig.Sequence,
		Payload:  &Message_Ack{&Ack{}},
	}
}

func (mb *MessageBuilder) NewError(orig *Message, err *Error) *Message {
	return &Message{
		Sequence: orig.Sequence,
		Payload:  &Message_Error{err},
	}
}
