package service

import (
	"time"

	"github.com/google/uuid"

	"github.com/getlantern/messaging-server/broker/membroker"
	"github.com/getlantern/messaging-server/db/memdb"
	"github.com/getlantern/messaging-server/model"

	"testing"

	"github.com/stretchr/testify/require"
)

const (
	checkPreKeysInterval = 100 * time.Millisecond
)

var (
	messageBuilder model.MessageBuilder
)

func TestServiceInMemory(t *testing.T) {
	service, err := New(&Opts{
		DB:                   memdb.New(),
		Broker:               membroker.New(),
		CheckPreKeysInterval: checkPreKeysInterval,
		LowPreKeysLimit:      3,
		NumPreKeysToRequest:  4,
	})
	require.NoError(t, err)
	doTestService(t, service)
}

func doTestService(t *testing.T, service *Service) {
	userA := uuid.New()
	deviceA1 := uint32(11)
	deviceA2 := uint32(12)

	userB := uuid.New()
	deviceB1 := uint32(21)
	deviceB2 := uint32(22)

	clientA1, err := service.Connect(userA, deviceA1)
	require.NoError(t, err)
	defer clientA1.Close()

	clientA2, err := service.Connect(userA, deviceA2)
	require.NoError(t, err)
	defer clientA2.Close()

	clientB1, err := service.Connect(userB, deviceB1)
	require.NoError(t, err)
	defer clientB1.Close()

	clientB2, err := service.Connect(userB, deviceB2)
	require.NoError(t, err)
	defer clientB2.Close()

	// register builds a registration messaging for registering key material
	register := func(registrationID uint32, identityKey, signedPreKey string, preKeys ...uint8) model.Message {
		reg := &model.Register{
			RegistrationID: registrationID,
			IdentityKey:    []byte(identityKey),
			SignedPreKey:   []byte(signedPreKey),
		}
		for _, preKey := range preKeys {
			reg.PreKeys = append(reg.PreKeys, []byte{preKey})
		}

		msg, err := messageBuilder.NewRegister(reg)
		require.NoError(t, err)
		return msg
	}

	// requestPreKeys builds a message for requesting pre key information for a specific user
	requestPreKeys := func(userID uuid.UUID, knownDeviceIDs ...uint32) model.Message {
		req := &model.RequestPreKeys{
			UserID:         userID.String(),
			KnownDeviceIDs: knownDeviceIDs,
		}

		msg, err := messageBuilder.NewRequestPreKeys(req)
		require.NoError(t, err)
		return msg
	}

	// send sends a message via a given client
	send := func(client *ClientConnection, msg model.Message) {
		client.Out() <- msg
	}

	// receive receives a message via a given client
	receive := func(client *ClientConnection) model.Message {
		return <-client.In()
	}

	// drain drains all pending messages for a client and returns the number of messages drained
	drain := func(client *ClientConnection) int {
		count := 0

		for {
			select {
			case <-client.In():
				count++
			default:
				return count
			}
		}
	}

	// roundTrip sends a message to the server and verifies that a corresponding ACK
	// is received.
	roundTrip := func(t *testing.T, client *ClientConnection, msg model.Message) {
		send(client, msg)
		ack := receive(client)
		require.Equal(t, msg.Sequence(), ack.Sequence())
	}

	// // roundTripForError sends a message to the server and verifies that the correct error
	// // is received
	// roundTripForError := func(t *testing.T, client *ClientConnection, msg model.Message, expectedError error) {
	// 	send(client, msg)
	// 	err := receive(client).Error()
	// 	require.Equal(t, expectedError, err)
	// }

	t.Run("register 4 devices for 2 users", func(t *testing.T) {
		roundTrip(t, clientA1, register(1, "ikA1", "spkA1", 11, 12, 13))
		roundTrip(t, clientA2, register(2, "ikA2", "spkA2", 21, 22, 23))
		roundTrip(t, clientB1, register(3, "ikB1", "spkB1", 31, 32, 33))
		roundTrip(t, clientB2, register(4, "ikB2", "spkB2", 41, 42, 43))
	})

	t.Run("request a preKey, pretending that we already know about one of the user's devices", func(t *testing.T) {
		send(clientB1, requestPreKeys(userA, deviceA2))
		preKey, err := receive(clientB1).PreKey()
		require.NoError(t, err)
		require.Zero(t, drain(clientB1), "should have received only one preKey")

		expectedPreKey := &model.PreKey{
			UserID:         userA.String(),
			DeviceID:       deviceA1,
			RegistrationID: 1,
			IdentityKey:    []byte("ikA1"),
			SignedPreKey:   []byte("spkA1"),
			PreKey:         []byte{13},
		}
		require.EqualValues(t, expectedPreKey, preKey, "should have gotten most recent preKey")
	})

	t.Run("request preKeys for multiple devices", func(t *testing.T) {
		send(clientA1, requestPreKeys(userB))
		preKey1, err := receive(clientA1).PreKey()
		require.NoError(t, err)
		preKey2, err := receive(clientA1).PreKey()
		require.NoError(t, err)
		require.Zero(t, drain(clientA1), "should have received only two preKeys")
		preKeys := map[uint32]*model.PreKey{
			preKey1.DeviceID: preKey1,
			preKey2.DeviceID: preKey2,
		}

		expectedPreKeys := map[uint32]*model.PreKey{
			deviceB1: {
				UserID:         userB.String(),
				DeviceID:       deviceB1,
				RegistrationID: 3,
				IdentityKey:    []byte("ikB1"),
				SignedPreKey:   []byte("spkB1"),
				PreKey:         []byte{33},
			},
			deviceB2: {
				UserID:         userB.String(),
				DeviceID:       deviceB2,
				RegistrationID: 4,
				IdentityKey:    []byte("ikB2"),
				SignedPreKey:   []byte("spkB2"),
				PreKey:         []byte{43},
			},
		}
		require.EqualValues(t, expectedPreKeys, preKeys, "should have gotten correct pre keys for both devices")
	})

	t.Run("make sure that server is requesting pre-keys when necessary", func(t *testing.T) {
		time.Sleep(checkPreKeysInterval * 2)
		require.EqualValues(t, 4, receive(clientA1).PreKeysLow().NumKeysRequested(), "clientA1 should have been notified that prekeys are getting low")
		require.EqualValues(t, 4, receive(clientB1).PreKeysLow().NumKeysRequested(), "clientB1 should have been notified that prekeys are getting low")
		require.EqualValues(t, 4, receive(clientB2).PreKeysLow().NumKeysRequested(), "clientB2 should have been notified that prekeys are getting low")
		roundTrip(t, clientA1, register(1, "ikA1", "spkA1", 14, 15, 16, 17))
		roundTrip(t, clientB1, register(3, "ikB1", "spkB1", 34, 35, 36, 37))
		roundTrip(t, clientB2, register(4, "ikB2", "spkB2", 44, 45, 46, 47))

		time.Sleep(checkPreKeysInterval * 2)
		require.Zero(t, drain(clientA1), "server should have stopped requesting pre keys for clientA1")
		require.Zero(t, drain(clientB1), "server should have stopped requesting pre keys for clientB1")
		require.Zero(t, drain(clientB2), "server should have stopped requesting pre keys for clientB2")
	})

	t.Run("exhaust pre-keys for deviceA2, then make sure we get a response without a preKey when requesting pre keys", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			send(clientB1, requestPreKeys(userA, deviceA1))
			receive(clientB1).PreKey()
		}
		require.Zero(t, drain(clientB1), "shouldn't be getting any more messages on clientB1 after receiving pre keys")

		send(clientB1, requestPreKeys(userA, deviceA1))
		preKey, err := receive(clientB1).PreKey()
		require.NoError(t, err)
		require.Zero(t, drain(clientB1), "should have received only one preKey")
		expectedPreKey := &model.PreKey{
			UserID:         userA.String(),
			DeviceID:       deviceA2,
			RegistrationID: 2,
			IdentityKey:    []byte("ikA2"),
			SignedPreKey:   []byte("spkA2"),
			PreKey:         nil, // should not have pre-key because we ran out
		}
		require.EqualValues(t, expectedPreKey, preKey, "should have gotten correct pre key")
		time.Sleep(checkPreKeysInterval * 2)
		require.EqualValues(t, 4, receive(clientA2).PreKeysLow().NumKeysRequested(), "clientA2 should have been notified that prekeys are getting low")
	})

	t.Run("send and receive message", func(t *testing.T) {
		roundTrip(t, clientA1, messageBuilder.NewUserMessage(userB, deviceB1, []byte("ciphertext")))
		msg := receive(clientB1).UserMessage()
		require.Equal(t, userA, msg.UserID())
		require.Equal(t, deviceA1, msg.DeviceID())
		require.Equal(t, "ciphertext", string(msg.CipherText()))
	})

	t.Run("unregister user and then request pre-keys for now non-existing user", func(t *testing.T) {
		roundTrip(t, clientA1, messageBuilder.NewUnregister())
		_, err := service.db.PreKeysRemaining(userA, deviceA1)
		require.EqualValues(t, model.ErrUnknownDevice, err, "device should be unknown after removal")
		roundTrip(t, clientA2, messageBuilder.NewUnregister())
		send(clientB1, requestPreKeys(userA))
		err = receive(clientB1).Error()
		require.EqualValues(t, model.ErrUnknownUser, err)
	})
}
