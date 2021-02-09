package message

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMessage(t *testing.T) {
	payload := "supercalifragilisticexpialidocious"
	msg := New(5, []byte(payload))
	require.Equal(t, Version(LatestVersion), msg.Version())
	require.Equal(t, Type(5), msg.Type())
	require.
		Equal(t, len(payload), msg.PayloadLength())
	require.Equal(t, payload, string(msg.Payload()))
}

func TestRegister(t *testing.T) {
	orig := &Register{
		UserID:         "user",
		DeviceID:       3,
		RegistrationID: 2,
		IdentityKey:    []byte{4},
		SignedPreKey:   []byte{5},
		PreKeys:        [][]byte{[]byte{6}, []byte{7}},
	}

	msg, err := NewRegister(orig)
	require.NoError(t, err)
	require.Equal(t, Type(TypeRegister), msg.Type())

	roundTripped, err := msg.Register()
	require.NoError(t, err)
	require.EqualValues(t, orig, roundTripped)
}

func TestUnregister(t *testing.T) {
	orig := &Unregister{
		UserID:   "user",
		DeviceID: 3,
	}

	msg, err := NewUnregister(orig)
	require.NoError(t, err)
	require.Equal(t, Type(TypeUnregister), msg.Type())

	roundTripped, err := msg.Unregister()
	require.NoError(t, err)
	require.EqualValues(t, orig, roundTripped)
}

func TestRequestPreKeys(t *testing.T) {
	orig := &RequestPreKeys{
		UserID:         "user",
		KnownDeviceIDs: []uint32{3, 4},
	}

	msg, err := NewRequestPreKeys(orig)
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

	msg, err := NewPreKey(orig)
	require.NoError(t, err)
	require.Equal(t, Type(TypePreKey), msg.Type())

	roundTripped, err := msg.PreKey()
	require.NoError(t, err)
	require.EqualValues(t, orig, roundTripped)
}

func TestUserMessage(t *testing.T) {
	to := uuid.New()
	from := uuid.New()
	cipherText := "bob"
	msg := NewOutboundUserMessage(to, from, []byte(cipherText))
	require.Equal(t, Type(TypeOutboundUserMessage), msg.Type())

	roundTripped := msg.OutboundUserMessage()
	require.Equal(t, to, roundTripped.To())
	require.Equal(t, from, roundTripped.From())
	require.Equal(t, cipherText, string(roundTripped.CipherText()))

	inboundMessage := NewInboundUserMessage(roundTripped.ToInbound())
	require.Equal(t, Type(TypeInboundUserMessage), inboundMessage.Type())

	inbound := inboundMessage.InboundUserMessage()
	require.Equal(t, from, inbound.From())
	require.Equal(t, cipherText, string(inbound.CipherText()))
}
