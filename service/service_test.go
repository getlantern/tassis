package service

import (
	"github.com/google/uuid"

	"github.com/getlantern/messaging-server/broker/membroker"
	"github.com/getlantern/messaging-server/db/memdb"
	"github.com/getlantern/messaging-server/model"

	"testing"

	"github.com/stretchr/testify/require"
)

var (
	messageBuilder model.MessageBuilder
)

func TestService(t *testing.T) {
	service := New(&Opts{
		DB:     memdb.New(2, 4),
		Broker: membroker.New(),
	})

	userA := uuid.New()
	deviceA1 := uint32(11)
	deviceA2 := uint32(12)

	userB := uuid.New()
	deviceB1 := uint32(21)
	deviceB2 := uint32(22)

	clientA1, err := service.Connect(userA, deviceA1)
	require.NoError(t, err)

	clientA2, err := service.Connect(userA, deviceA2)
	require.NoError(t, err)

	clientB1, err := service.Connect(userB, deviceB1)
	require.NoError(t, err)

	clientB2, err := service.Connect(userB, deviceB2)
	require.NoError(t, err)

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

	requestPreKeys := func(userID uuid.UUID, knownDeviceIDs ...uint32) model.Message {
		req := &model.RequestPreKeys{
			UserID:         userID.String(),
			KnownDeviceIDs: knownDeviceIDs,
		}

		msg, err := messageBuilder.NewRequestPreKeys(req)
		require.NoError(t, err)
		return msg
	}

	send := func(client *ClientConnection, msg model.Message) {
		client.Out() <- msg
	}

	receive := func(client *ClientConnection) model.Message {
		return <-client.In()
	}

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
	roundTrip := func(client *ClientConnection, msg model.Message) {
		send(client, msg)
		ack := receive(client)
		require.Equal(t, msg.Sequence(), ack.Sequence())
	}

	// register 4 devices for 2 users
	roundTrip(clientA1, register(1, "ikA1", "spkA1", 11, 12, 13))
	roundTrip(clientA2, register(2, "ikA2", "spkA2", 21, 22, 23))
	roundTrip(clientB1, register(3, "ikB1", "spkB1", 31, 32, 33))
	roundTrip(clientB2, register(4, "ikB2", "spkB2", 41, 42, 43))

	// request a preKey, pretending that we already know about one of the user's devices
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

	// request preKeys for multiple devices
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

}
