package testsupport

import (
	"crypto/rand"
	"time"

	"github.com/getlantern/messaging-server/db"
	"github.com/getlantern/messaging-server/model"

	"testing"

	"github.com/stretchr/testify/require"
)

const (
	CheckPreKeysInterval                 = 100 * time.Millisecond
	slightlyMoreThanCheckPreKeysInterval = 110 * time.Millisecond
	LowPreKeysLimit                      = 3
	NumPreKeysToRequest                  = 4
)

var (
	messageBuilder model.MessageBuilder
)

type ClientConnectionLike interface {
	Send(msg *model.Message)
	Receive() *model.Message
	Drain() int
	Close()
}

func TestService(t *testing.T, database db.DB, connect func(t *testing.T, userID []byte, deviceID uint32) ClientConnectionLike) {
	userA := make([]byte, 32)
	rand.Read(userA)
	deviceA1 := uint32(11)
	deviceA2 := uint32(12)

	userB := make([]byte, 32)
	rand.Read(userB)
	deviceB1 := uint32(21)
	deviceB2 := uint32(22)

	clientA1 := connect(t, userA, deviceA1)
	defer clientA1.Close()

	clientA2 := connect(t, userA, deviceA2)
	defer clientA2.Close()

	clientB1 := connect(t, userB, deviceB1)
	defer clientB1.Close()

	clientB2 := connect(t, userB, deviceB2)
	defer clientB2.Close()

	// register builds a registration message for registering key material
	register := func(registrationID uint32, signedPreKey string, preKeys ...uint8) *model.Message {
		reg := &model.Register{
			RegistrationID: registrationID,
			SignedPreKey:   []byte(signedPreKey),
		}
		for _, preKey := range preKeys {
			reg.PreKeys = append(reg.PreKeys, []byte{preKey})
		}

		return messageBuilder.Build(&model.Message_Register{reg})
	}

	// requestPreKeys builds a message for requesting pre key information for a specific user
	requestPreKeys := func(userID []byte, knownDeviceIDs ...uint32) *model.Message {
		req := &model.RequestPreKeys{
			UserID:         userID,
			KnownDeviceIDs: knownDeviceIDs,
		}

		return messageBuilder.Build(&model.Message_RequestPreKeys{req})
	}

	// roundTrip sends a message to the server and verifies that a corresponding ACK
	// is received.
	roundTrip := func(t *testing.T, client ClientConnectionLike, msg *model.Message) {
		client.Send(msg)
		ack := client.Receive()
		require.Equal(t, msg.Sequence, ack.Sequence)
	}

	t.Run("register 4 devices for 2 users", func(t *testing.T) {
		roundTrip(t, clientA1, register(1, "spkA1", 11, 12, 13))
		roundTrip(t, clientA2, register(2, "spkA2", 21, 22, 23))
		roundTrip(t, clientB1, register(3, "spkB1", 31, 32, 33))
		roundTrip(t, clientB2, register(4, "spkB2", 41, 42, 43))
	})

	t.Run("request a preKey, pretending that we already know about one of the user's devices", func(t *testing.T) {
		clientB1.Send(requestPreKeys(userA, deviceA2))
		preKey := clientB1.Receive().GetPreKey()
		require.Zero(t, clientB1.Drain(), "should have received only one preKey")

		expectedPreKey := &model.PreKey{
			UserID:         userA,
			DeviceID:       deviceA1,
			RegistrationID: 1,
			SignedPreKey:   []byte("spkA1"),
			PreKey:         []byte{13},
		}
		require.EqualValues(t, expectedPreKey, preKey, "should have gotten most recent preKey")
	})

	t.Run("request preKeys for multiple devices", func(t *testing.T) {
		clientA2.Send(requestPreKeys(userB))
		preKey1 := clientA2.Receive().GetPreKey()
		preKey2 := clientA2.Receive().GetPreKey()
		require.Zero(t, clientA2.Drain(), "should have received only two preKeys")
		preKeys := map[uint32]*model.PreKey{
			preKey1.DeviceID: preKey1,
			preKey2.DeviceID: preKey2,
		}

		expectedPreKeys := map[uint32]*model.PreKey{
			deviceB1: {
				UserID:         userB,
				DeviceID:       deviceB1,
				RegistrationID: 3,
				SignedPreKey:   []byte("spkB1"),
				PreKey:         []byte{33},
			},
			deviceB2: {
				UserID:         userB,
				DeviceID:       deviceB2,
				RegistrationID: 4,
				SignedPreKey:   []byte("spkB2"),
				PreKey:         []byte{43},
			},
		}
		require.EqualValues(t, expectedPreKeys, preKeys, "should have gotten correct pre keys for both devices")
	})

	t.Run("make sure that server is requesting pre-keys when necessary", func(t *testing.T) {
		time.Sleep(slightlyMoreThanCheckPreKeysInterval)
		require.EqualValues(t, 4, clientA1.Receive().GetPreKeysLow().GetKeysRequested(), "clientA1 should have been notified that prekeys are getting low")
		require.EqualValues(t, 4, clientB1.Receive().GetPreKeysLow().GetKeysRequested(), "clientB1 should have been notified that prekeys are getting low")
		require.EqualValues(t, 4, clientB2.Receive().GetPreKeysLow().GetKeysRequested(), "clientB2 should have been notified that prekeys are getting low")
		roundTrip(t, clientA1, register(1, "spkA1", 14, 15, 16, 17))
		roundTrip(t, clientB1, register(3, "spkB1", 34, 35, 36, 37))
		roundTrip(t, clientB2, register(4, "spkB2", 44, 45, 46, 47))

		time.Sleep(slightlyMoreThanCheckPreKeysInterval)
		require.Zero(t, clientA1.Drain(), "server should have stopped requesting pre keys for clientA1")
		require.Zero(t, clientB1.Drain(), "server should have stopped requesting pre keys for clientB1")
		require.Zero(t, clientB2.Drain(), "server should have stopped requesting pre keys for clientB2")
	})

	t.Run("exhaust pre-keys for deviceA2, then make sure we get a response without a preKey when requesting pre keys", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			clientB1.Send(requestPreKeys(userA, deviceA1))
			clientB1.Receive().GetPreKey()
		}
		require.Zero(t, clientB1.Drain(), "shouldn't be getting any more messages on clientB1 after receiving pre keys")

		clientB1.Send(requestPreKeys(userA, deviceA1))
		preKey := clientB1.Receive().GetPreKey()
		require.Zero(t, clientB1.Drain(), "should have received only one preKey")
		expectedPreKey := &model.PreKey{
			UserID:         userA,
			DeviceID:       deviceA2,
			RegistrationID: 2,
			SignedPreKey:   []byte("spkA2"),
			PreKey:         nil, // should not have pre-key because we ran out
		}
		require.EqualValues(t, expectedPreKey, preKey, "should have gotten correct pre key")
		time.Sleep(slightlyMoreThanCheckPreKeysInterval)
		require.EqualValues(t, 4, clientA2.Receive().GetPreKeysLow().GetKeysRequested(), "clientA2 should have been notified that prekeys are getting low")
	})

	t.Run("send and receive message", func(t *testing.T) {
		roundTrip(t, clientA1, messageBuilder.Build(&model.Message_OutboundMessage{&model.OutboundMessage{
			To: &model.Address{
				UserID:   userB,
				DeviceID: deviceB1,
			},
			UnidentifiedSenderMessage: []byte("ciphertext"),
		}}))
		msg := clientB1.Receive().GetInboundMessage()
		require.Equal(t, "ciphertext", string(msg))
	})

	t.Run("unregister user and then request pre-keys for now non-existing user", func(t *testing.T) {
		roundTrip(t, clientA1, messageBuilder.Build(&model.Message_Unregister{&model.Unregister{}}))
		_, err := database.PreKeysRemaining(userA, deviceA1)
		require.EqualValues(t, model.ErrUnknownDevice, err, "device should be unknown after removal")
		roundTrip(t, clientA2, messageBuilder.Build(&model.Message_Unregister{&model.Unregister{}}))
		clientB1.Send(requestPreKeys(userA))
		err = clientB1.Receive().GetError()
		require.EqualValues(t, model.ErrUnknownUser.Error(), err.Error())
	})
}
