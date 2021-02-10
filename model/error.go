package model

import (
	"fmt"
)

const (
	ErrCodeUnknownError   = 1
	ErrCodeMarshalError   = 2
	ErrCodeUnmarshalError = 3
)

var (
	ErrInvalidUserID = &Error{
		Code:        100,
		Description: "invalid userID",
	}

	ErrUnknownUser = &Error{
		Code:        101,
		Description: "unknown user",
	}

	ErrNoPreKeyAvailable = &Error{
		Code:        102,
		Description: "no prekey available for device",
	}
)

// Error is a message indicating that there was an error, encoded as follows:
//
//   +------+-------------+
//   | Code | Description |
//   +------+-------------+
//   |  1   |   variable  |
//   +------+-------------+
//
// If the error corresponds to an original message, the sequence on the enclosing Message envelope is
// set to the sequence from the original message.
type Error struct {
	Code        uint8
	Description string
}

func (err *Error) Error() string {
	return fmt.Sprintf("%d|%s", err.Code, err.Description)
}

func TypedError(err error) *Error {
	typed, ok := err.(*Error)
	if ok {
		return typed
	}
	return &Error{ErrCodeUnknownError, err.Error()}
}

func NewError(sequence Sequence, err *Error) Message {
	descriptionBytes := []byte(err.Description)
	payload := make(UserMessage, 1+len(descriptionBytes))
	payload[0] = err.Code
	copy(payload[1:], descriptionBytes)

	msg := NewMessage(TypeError, payload)
	msg.SetSequence(sequence)
	return msg
}

func (msg Message) Error() *Error {
	payload := msg.Payload()
	err := &Error{}
	err.Code = payload[0]
	err.Description = string(payload[1:])
	return err
}
