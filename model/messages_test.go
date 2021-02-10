package model

import (
	"bytes"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var (
	messageBuilder MessageBuilder
)

func TestMessage(t *testing.T) {
	payload := "supercalifragilisticexpialidocious"
	_msg := messageBuilder.NewMessage(5, []byte(payload))
	var buf bytes.Buffer
	buf.Write(_msg)
	msg, err := ReadNextMessage(&buf)
	require.NoError(t, err)
	require.Equal(t, Version(LatestVersion), msg.Version())
	require.Equal(t, Sequence(atomic.LoadUint32(&messageBuilder.seq)), msg.Sequence())
	require.Equal(t, Type(5), msg.Type())
	require.Equal(t, len(payload), msg.PayloadLength())
	require.Equal(t, payload, string(msg.Payload()))
}

func TestAck(t *testing.T) {
	msg := messageBuilder.NewMessage(TypePreKey, nil)
	msg.SetSequence(5)
	require.Equal(t, Sequence(5), messageBuilder.Ack(msg).Sequence())
}

func TestRegister(t *testing.T) {
	orig := &Register{
		RegistrationID: 2,
		IdentityKey:    []byte{4},
		SignedPreKey:   []byte{5},
		PreKeys:        [][]byte{[]byte{6}, []byte{7}},
	}

	msg, err := messageBuilder.NewRegister(orig)
	require.NoError(t, err)
	require.Equal(t, Type(TypeRegister), msg.Type())

	roundTripped, err := msg.Register()
	require.NoError(t, err)
	require.EqualValues(t, orig, roundTripped)
}

func TestRequestPreKeys(t *testing.T) {
	orig := &RequestPreKeys{
		UserID:         "user",
		KnownDeviceIDs: []uint32{3, 4},
	}

	msg, err := messageBuilder.NewRequestPreKeys(orig)
	require.NoError(t, err)
	require.Equal(t, Type(TypeRequestPreKeys), msg.Type())

	roundTripped, err := msg.RequestPreKeys()
	require.NoError(t, err)
	require.EqualValues(t, orig, roundTripped)
}

func TestPreKey(t *testing.T) {
	orig := &PreKey{
		UserID:         "user",
		DeviceID:       3,
		RegistrationID: 2,
		IdentityKey:    []byte{4},
		SignedPreKey:   []byte{5},
		PreKey:         []byte{6},
	}

	msg, err := messageBuilder.NewPreKey(orig)
	require.NoError(t, err)
	require.Equal(t, Type(TypePreKey), msg.Type())

	roundTripped, err := msg.PreKey()
	require.NoError(t, err)
	require.EqualValues(t, orig, roundTripped)
}

func TestPreKeysLow(t *testing.T) {
	msg := messageBuilder.NewPreKeysLow(65)
	require.Equal(t, Type(TypePreKeysLow), msg.Type())

	roundTripped := msg.PreKeysLow()
	require.Equal(t, uint16(65), roundTripped.NumKeysRequested())
}

func TestUserMessage(t *testing.T) {
	toUser := uuid.New()
	toDevice := uint32(1)
	fromUser := uuid.New()
	fromDevice := uint32(2)

	cipherText := "bob"
	msg := messageBuilder.NewUserMessage(toUser, toDevice, []byte(cipherText))
	require.Equal(t, Type(TypeUserMessage), msg.Type())

	roundTripped := msg.UserMessage()
	require.Equal(t, toUser, roundTripped.UserID())
	require.Equal(t, toDevice, roundTripped.DeviceID())
	require.Equal(t, cipherText, string(roundTripped.CipherText()))

	roundTripped.SetUserID(fromUser)
	roundTripped.SetDeviceID(fromDevice)
	require.Equal(t, fromUser, roundTripped.UserID())
	require.Equal(t, fromDevice, roundTripped.DeviceID())
}

func TestError(t *testing.T) {
	orig := &Error{
		Code:        5,
		Description: "something went wrong",
	}
	msg := messageBuilder.NewError(7, orig)
	require.Equal(t, Type(TypeError), msg.Type())
	require.Equal(t, Sequence(7), msg.Sequence())

	roundTripped := msg.Error()
	require.EqualValues(t, orig, roundTripped)

	makeTypedError := func() error {
		return orig
	}
	require.EqualValues(t, orig, TypedError(makeTypedError()))

	makeUntypedError := func() error {
		return errors.New("I'm an error")
	}
	require.EqualValues(t, &Error{ErrCodeUnknownError, makeUntypedError().Error()}, TypedError(makeUntypedError()))
}
