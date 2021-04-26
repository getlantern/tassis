package model

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/getlantern/tassis/util"
)

var (
	ErrUnknown = &Error{
		Name: "unknown_error",
	}

	ErrInvalidIdentityKey = &Error{
		Name: "invalid_identity_key",
	}

	ErrUnknownIdentity = &Error{
		Name: "unknown_identity",
	}

	ErrUnknownDevice = &Error{
		Name: "unknown_device",
	}

	ErrUnauthorized = &Error{
		Name: "unauthorized",
	}

	ErrNonAnonymous = &Error{
		Name: "attempted_anonymous_op_on_authenticated_connection",
	}

	ErrUnableToOpenSubscriber = &Error{
		Name: "unable_to_open_subscriber",
	}

	ErrUnableToACK = &Error{
		Name: "unable_to_ack",
	}
)

func (err *Error) Error() string {
	return fmt.Sprintf("%s:%s", err.Name, err.Description)
}

func (err *Error) WithDescription(description string) *Error {
	return &Error{
		Name:        err.Name,
		Description: description,
	}
}

func (err *Error) WithError(other error) *Error {
	return err.WithDescription(other.Error())
}

func TypedError(err error) *Error {
	typed, ok := err.(*Error)
	if ok {
		return typed
	}
	return ErrUnknown.WithDescription(err.Error())
}

// MarkFailed marks this message as failed (couldn't be forwarded)
func (msg *ForwardedMessage) MarkFailed() {
	now := util.NowUnixMillis()
	msg.LastFailed = now
	if msg.FirstFailed == 0 {
		msg.FirstFailed = now
	}
}

// HasBeenFailingFor indicates how long the message has been failing since it first started failing.
// This returns 0 if the message has never failed before.
func (msg *ForwardedMessage) HasBeenFailingFor() time.Duration {
	if msg.FirstFailed == 0 {
		return 0
	}
	return util.DurationSince(msg.FirstFailed)
}

// DurationSinceLastFailure indicates how long it's been since this message last failed to forward.
func (msg *ForwardedMessage) DurationSinceLastFailure() time.Duration {
	if msg.LastFailed == 0 {
		return 0
	}
	return util.DurationSince(msg.LastFailed)
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
