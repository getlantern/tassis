// +build smoketest

package web

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/getlantern/tassis/model"
	"github.com/getlantern/tassis/testsupport"
	"github.com/getlantern/tassis/webclient"
)

// TestSmokeTest makes sure the live site is up and running by opening a websocket client, sending a message to itself, authenticating, and receiving that message.
func TestSmokeTest(t *testing.T) {
	client, err := webclient.Connect(os.Getenv("SMOKE_TEST_URL"), 100)
	require.NoError(t, err)

	authChallenge := client.Receive().GetAuthChallenge()
	require.NotEmpty(t, authChallenge)

	// read and ignore config
	client.Receive()

	sendForAck := func(msg *model.Message) {
		client.Send(msg)
		response := client.Receive()
		switch p := response.GetPayload().(type) {
		case *model.Message_Error:
			t.Error(p.Error)
			t.FailNow()
		case *model.Message_Ack:
			require.Equal(t, msg.Sequence, response.Sequence)
		default:
			t.Error("Didn't receive Ack")
			t.FailNow()
		}
	}

	keyPair, err := testsupport.GenerateKeyPair()
	require.NoError(t, err)

	identityKey := keyPair.Public
	deviceId := []byte{2}

	address := &model.Address{
		IdentityKey: identityKey,
		DeviceId:    deviceId,
	}

	var mb model.MessageBuilder

	login := func() {
		login := &model.Login{
			Address: address,
			Nonce:   authChallenge.Nonce,
		}

		loginBytes, err := proto.Marshal(login)
		require.NoError(t, err)
		signature, err := keyPair.Private.Sign(loginBytes)
		require.NoError(t, err)

		msg := mb.Build(
			&model.Message_AuthResponse{
				AuthResponse: &model.AuthResponse{
					Login:     loginBytes,
					Signature: signature,
				},
			})
		sendForAck(msg)
	}

	t.Run("log in and register", func(t *testing.T) {
		login()
		register := mb.Build(
			&model.Message_Register{
				Register: &model.Register{
					SignedPreKey: []byte("spk"),
				},
			})
		for i := 0; i < 100; i++ {
			register.GetRegister().OneTimePreKeys = append(register.GetRegister().OneTimePreKeys, []byte(fmt.Sprintf("otpk%d", i)))
		}
		sendForAck(register)
	})

	// log out and reconnect
	client.Close()
	client, err = webclient.Connect(os.Getenv("SMOKE_TEST_URL"), 100)
	require.NoError(t, err)

	authChallenge = client.Receive().GetAuthChallenge()
	require.NotEmpty(t, authChallenge)

	// ignore config
	client.Receive()

	testMessage := "I'm smoke testing"
	t.Run("send test message", func(t *testing.T) {
		sendForAck(mb.Build(&model.Message_OutboundMessage{
			OutboundMessage: &model.OutboundMessage{
				To:                        address,
				UnidentifiedSenderMessage: []byte(testMessage),
			}}))
	})

	t.Run("log in, receive message and unregister", func(t *testing.T) {
		login := &model.Login{
			Address: address,
			Nonce:   authChallenge.Nonce,
		}

		loginBytes, err := proto.Marshal(login)
		require.NoError(t, err)
		signature, err := keyPair.Private.Sign(loginBytes)
		require.NoError(t, err)

		msg := mb.Build(
			&model.Message_AuthResponse{
				AuthResponse: &model.AuthResponse{
					Login:     loginBytes,
					Signature: signature,
				},
			})
		sendForAck(msg)

		inboundMsg := client.Receive().GetInboundMessage()
		require.Equal(t, testMessage, string(inboundMsg))

		unregister := mb.Build(
			&model.Message_Unregister{
				Unregister: &model.Unregister{},
			})
		sendForAck(unregister)
	})
}
