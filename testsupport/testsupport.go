package testsupport

import (
	"time"

	"github.com/getlantern/tassis/db"
	"github.com/getlantern/tassis/identity"
	"github.com/getlantern/tassis/model"
	"github.com/getlantern/tassis/presence"
	"github.com/getlantern/tassis/service"
	"google.golang.org/protobuf/proto"

	"testing"

	"github.com/stretchr/testify/require"
)

const (
	CheckPreKeysInterval                 = 200 * time.Millisecond
	slightlyMoreThanCheckPreKeysInterval = 220 * time.Millisecond
	LowPreKeysLimit                      = 3
	NumPreKeysToRequest                  = 4
	ForwardingTimeout                    = 2000 * time.Millisecond
	MinForwardingRetryInterval           = 200 * time.Millisecond
	UserTransferInterval                 = 100 * time.Millisecond

	server1 = 1
	server2 = 2
)

var (
	mb model.MessageBuilder
)

// TestService runs a comprehensive test of the service API. If testMultiClientMessaging is false, it will omit scenarios
// that involve multiple recipient clients of the same messages. This is primarily used to avoid testing those scenarios on the
// membroker, which can't support them.
func TestService(t *testing.T, testMultiClientMessaging bool, presenceRepo presence.Repository, buildServiceAndDB func(t *testing.T, serverID int) (service.Service, db.DB)) {
	servicesByID := make(map[int]service.Service, 0)
	dbsByID := make(map[int]db.DB, 0)
	s1, db1 := buildServiceAndDB(t, 1)
	s2, db2 := buildServiceAndDB(t, 2)
	servicesByID[1] = s1
	servicesByID[2] = s2
	dbsByID[1] = db1
	dbsByID[2] = db2

	// roundTrip sends a message to the server and verifies that a corresponding ACK
	// is received, ignoring any non-ack messages that arrive first
	roundTrip := func(t *testing.T, client service.ClientConnection, msg *model.Message) {
		client.Send(msg)
		for {
			ack := client.Receive()
			switch ack.GetPayload().(type) {
			case *model.Message_Ack:
				require.Equal(t, msg.Sequence, ack.Sequence)
				return
			default:
				continue
			}
		}
	}

	doLogin := func(t *testing.T, serverID int, userID identity.UserID, deviceID uint32, privateKey identity.PrivateKey) (service.ClientConnection, *model.Message) {
		client, err := servicesByID[serverID].Connect()
		require.NoError(t, err)
		authChallenge := client.Receive().GetAuthChallenge()
		require.Len(t, authChallenge.Nonce, 32)

		login := &model.Login{
			Address: &model.Address{
				UserID:   userID,
				DeviceID: deviceID,
			},
			Nonce: authChallenge.Nonce,
		}

		loginBytes, err := proto.Marshal(login)
		require.NoError(t, err)
		signature, err := privateKey.Sign(loginBytes)
		require.NoError(t, err)
		msg := mb.Build(
			&model.Message_AuthResponse{
				AuthResponse: &model.AuthResponse{
					Login:     loginBytes,
					Signature: signature,
				},
			})
		client.Send(msg)
		return client, msg
	}

	// login logs a client in
	login := func(t *testing.T, serverID int, userID identity.UserID, deviceID uint32, privateKey identity.PrivateKey) service.ClientConnection {
		client, msg := doLogin(t, serverID, userID, deviceID, privateKey)
		ack := client.Receive()
		require.Equal(t, msg.Sequence, ack.Sequence)
		return client
	}

	connectAnonymous := func(serverID int) service.ClientConnection {
		client, err := servicesByID[serverID].Connect()
		require.NoError(t, err)
		client.Receive()
		return client
	}

	// register builds a registration message for registering key material
	register := func(registrationID uint32, signedPreKey string, preKeys ...uint8) *model.Message {
		reg := &model.Register{
			RegistrationID: registrationID,
			SignedPreKey:   []byte(signedPreKey),
		}
		for _, preKey := range preKeys {
			reg.OneTimePreKeys = append(reg.OneTimePreKeys, []byte{preKey})
		}

		return mb.Build(&model.Message_Register{reg})
	}

	// requestPreKeys builds a message for requesting pre key information for a specific user
	requestPreKeys := func(userID []byte, knownDeviceIDs ...uint32) *model.Message {
		req := &model.RequestPreKeys{
			UserID:         userID,
			KnownDeviceIDs: knownDeviceIDs,
		}

		return mb.Build(&model.Message_RequestPreKeys{req})
	}

	userAKeyPair, err := identity.GenerateKeyPair()
	require.NoError(t, err)
	userA := userAKeyPair.Public.UserID()
	deviceA1 := uint32(11)
	deviceA2 := uint32(12)

	userBKeyPair, err := identity.GenerateKeyPair()
	require.NoError(t, err)
	userB := userBKeyPair.Public.UserID()
	deviceB1 := uint32(21)
	deviceB2 := uint32(22)

	clientA1 := login(t, server1, userA, deviceA1, userAKeyPair.Private)
	defer clientA1.Close()
	clientA1Anonymous := connectAnonymous(server1)
	defer clientA1Anonymous.Close()

	clientA2 := login(t, server1, userA, deviceA2, userAKeyPair.Private)
	defer clientA2.Close()
	clientA2Anonymous := connectAnonymous(server1)
	defer clientA2Anonymous.Close()

	clientB1 := login(t, server1, userB, deviceB1, userBKeyPair.Private)
	defer clientB1.Close()
	clientB1Anonymous := connectAnonymous(server1)
	defer clientB1Anonymous.Close()

	clientB2 := login(t, server1, userB, deviceB2, userBKeyPair.Private)
	defer clientB2.Close()
	clientB2Anonymous := connectAnonymous(server1)
	defer clientB2Anonymous.Close()

	t.Run("login failure", func(t *testing.T) {
		keyPair, err := identity.GenerateKeyPair()
		require.NoError(t, err)
		wrongKeyPair, err := identity.GenerateKeyPair()
		require.NoError(t, err)
		user := keyPair.Public.UserID()
		device := uint32(1)
		client, _ := doLogin(t, server1, user, device, wrongKeyPair.Private)
		defer client.Close()
		msg := client.Receive()
		err = msg.GetError()
		require.EqualValues(t, model.ErrUnauthorized.Error(), err.Error())
	})

	t.Run("register 4 devices for 2 users", func(t *testing.T) {
		roundTrip(t, clientA1, register(1, "spkA1", 11, 12, 13))
		roundTrip(t, clientA2, register(2, "spkA2", 21, 22, 23))
		roundTrip(t, clientB1, register(3, "spkB1", 31, 32, 33))
		roundTrip(t, clientB2, register(4, "spkB2", 41, 42, 43))
	})

	t.Run("request a preKey, pretending that we already know about one of the user's devices", func(t *testing.T) {
		clientB1Anonymous.Send(requestPreKeys(userA, deviceA2))
		preKey := clientB1Anonymous.Receive().GetPreKeys().GetPreKeys()[0]
		require.Zero(t, clientB1Anonymous.Drain(), "should have received only one preKey")

		expectedPreKey := &model.PreKey{
			Address: &model.Address{
				UserID:   userA,
				DeviceID: deviceA1,
			},
			RegistrationID: 1,
			SignedPreKey:   []byte("spkA1"),
			OneTimePreKey:  []byte{13},
		}
		require.EqualValues(t, expectedPreKey, preKey, "should have gotten most recent preKey")
	})

	t.Run("request preKeys for multiple devices", func(t *testing.T) {
		clientA2Anonymous.Send(requestPreKeys(userB))
		preKeysList := clientA2Anonymous.Receive().GetPreKeys().GetPreKeys()
		require.Zero(t, clientA2Anonymous.Drain(), "should have received only one preKeys message")
		preKey1 := preKeysList[0]
		preKey2 := preKeysList[1]
		require.Len(t, preKeysList, 2, "should have received 2 preKeys")
		preKeys := map[uint32]*model.PreKey{
			preKey1.Address.DeviceID: preKey1,
			preKey2.Address.DeviceID: preKey2,
		}

		expectedPreKeys := map[uint32]*model.PreKey{
			deviceB1: {
				Address: &model.Address{
					UserID:   userB,
					DeviceID: deviceB1,
				},
				RegistrationID: 3,
				SignedPreKey:   []byte("spkB1"),
				OneTimePreKey:  []byte{33},
			},
			deviceB2: {
				Address: &model.Address{
					UserID:   userB,
					DeviceID: deviceB2,
				},
				RegistrationID: 4,
				SignedPreKey:   []byte("spkB2"),
				OneTimePreKey:  []byte{43},
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
			clientB1Anonymous.Send(requestPreKeys(userA, deviceA1))
			clientB1Anonymous.Receive().GetPreKeys()
		}
		require.Zero(t, clientB1Anonymous.Drain(), "shouldn't be getting any more messages on clientB1 after receiving pre keys")

		clientB1Anonymous.Send(requestPreKeys(userA, deviceA1))
		preKeys := clientB1Anonymous.Receive().GetPreKeys().GetPreKeys()
		require.Len(t, preKeys, 1, "should have received only 1 preKey")
		preKey := preKeys[0]
		require.Zero(t, clientB1Anonymous.Drain(), "should have received only one preKeys response")
		expectedPreKey := &model.PreKey{
			Address: &model.Address{
				UserID:   userA,
				DeviceID: deviceA2,
			},
			RegistrationID: 2,
			SignedPreKey:   []byte("spkA2"),
			OneTimePreKey:  nil, // should not have pre-key because we ran out
		}
		require.EqualValues(t, expectedPreKey, preKey, "should have gotten correct pre key")
		time.Sleep(slightlyMoreThanCheckPreKeysInterval)
		require.EqualValues(t, 4, clientA2.Receive().GetPreKeysLow().GetKeysRequested(), "clientA2 should have been notified that prekeys are getting low")
	})

	t.Run("send and receive message", func(t *testing.T) {
		roundTrip(t, clientA1Anonymous, mb.Build(&model.Message_OutboundMessage{&model.OutboundMessage{
			To: &model.Address{
				UserID:   userB,
				DeviceID: deviceB1,
			},
			UnidentifiedSenderMessage: []byte("ciphertext"),
		}}))
		msg := clientB1.Receive()
		inboundMsg := msg.GetInboundMessage()
		require.Equal(t, "ciphertext", string(inboundMsg))
		clientB1.Send(mb.NewAck(msg))
	})

	if testMultiClientMessaging {
		t.Run("new recipient after prior ack should receive only new message, old client should receive new message as well", func(t *testing.T) {
			roundTrip(t, clientA1Anonymous, mb.Build(&model.Message_OutboundMessage{&model.OutboundMessage{
				To: &model.Address{
					UserID:   userB,
					DeviceID: deviceB1,
				},
				UnidentifiedSenderMessage: []byte("ciphertext2"),
			}}))
			clientB1Extra := login(t, server1, userB, deviceB1, userBKeyPair.Private)
			defer clientB1Extra.Close()
			msg := clientB1Extra.Receive()
			inboundMsg := msg.GetInboundMessage()
			require.Equal(t, "ciphertext2", string(inboundMsg))
			msg = clientB1.Receive()
			inboundMsg = msg.GetInboundMessage()
			require.Equal(t, "ciphertext2", string(inboundMsg))
		})

		t.Run("new recipient without prior ack should receive old message, old client should receive none", func(t *testing.T) {
			clientB1Extra := login(t, server1, userB, deviceB1, userBKeyPair.Private)
			defer clientB1Extra.Close()
			msg := clientB1Extra.Receive()
			inboundMsg := msg.GetInboundMessage()
			require.Equal(t, "ciphertext2", string(inboundMsg))
			clientB1Extra.Send(mb.NewAck(msg))
			require.Zero(t, clientB1.Drain())
		})
	}

	t.Run("unregister user and then request pre-keys for now non-existing user", func(t *testing.T) {
		roundTrip(t, clientA1, mb.Build(&model.Message_Unregister{&model.Unregister{}}))
		_, err := dbsByID[1].PreKeysRemaining(userA, deviceA1)
		require.EqualValues(t, model.ErrUnknownDevice, err, "device should be unknown after removal")
		roundTrip(t, clientA2, mb.Build(&model.Message_Unregister{&model.Unregister{}}))
		clientB1Anonymous.Send(requestPreKeys(userA))
		err = clientB1Anonymous.Receive().GetError()
		require.EqualValues(t, model.ErrUnknownUser.Error(), err.Error())
	})

	t.Run("test forwarding between tasses", func(t *testing.T) {
		userCKeyPair, err := identity.GenerateKeyPair()
		require.NoError(t, err)
		userC := userCKeyPair.Public.UserID()
		deviceC1 := uint32(31)

		clientC1 := login(t, server1, userC, deviceC1, userCKeyPair.Private)
		defer clientC1.Close()

		roundTrip(t, clientC1, register(1, "spkC1", 31, 32, 33))

		clientAnonymous := connectAnonymous(server2)
		defer clientAnonymous.Close()

		deviceC1Addr := &model.Address{
			UserID:   userC,
			DeviceID: deviceC1,
		}

		// temporarily corrupt the presence repo to make sure that retries work and that messages time out
		// if failing for too long.
		origHost, err := presenceRepo.Find(deviceC1Addr)
		require.NoError(t, err)
		require.NoError(t, presenceRepo.Announce(deviceC1Addr, "garbage"))

		roundTrip(t, clientAnonymous, mb.Build(&model.Message_OutboundMessage{&model.OutboundMessage{
			To:                        deviceC1Addr,
			UnidentifiedSenderMessage: []byte("shouldbelost"),
		}}))
		// sleep long enough for first message to get lost
		time.Sleep(ForwardingTimeout * 2)

		roundTrip(t, clientAnonymous, mb.Build(&model.Message_OutboundMessage{&model.OutboundMessage{
			To:                        deviceC1Addr,
			UnidentifiedSenderMessage: []byte("shouldbeforwarded"),
		}}))

		// fix presence and make sure 2nd message is received
		require.NoError(t, presenceRepo.Announce(deviceC1Addr, origHost))
		msg := clientC1.Receive()
		inboundMsg := msg.GetInboundMessage()
		require.Equal(t, "shouldbeforwarded", string(inboundMsg))
		clientC1.Send(mb.NewAck(msg))
	})

	t.Run("test user transfer between tasses", func(t *testing.T) {
		userCKeyPair, err := identity.GenerateKeyPair()
		require.NoError(t, err)
		userC := userCKeyPair.Public.UserID()
		deviceC1 := uint32(31)

		clientC1 := login(t, server1, userC, deviceC1, userCKeyPair.Private)
		defer clientC1.Close()

		roundTrip(t, clientC1, register(1, "spkC1", 31, 32, 33))

		clientAnonymous := connectAnonymous(server2)
		defer clientAnonymous.Close()

		deviceC1Addr := &model.Address{
			UserID:   userC,
			DeviceID: deviceC1,
		}
		roundTrip(t, clientAnonymous, mb.Build(&model.Message_OutboundMessage{&model.OutboundMessage{
			To:                        deviceC1Addr,
			UnidentifiedSenderMessage: []byte("forwarded"),
		}}))

		clientC1AtServer2 := login(t, server2, userC, deviceC1, userCKeyPair.Private)
		defer clientC1AtServer2.Close()

		roundTrip(t, clientC1AtServer2, register(1, "spkC1", 34, 35, 36))
		msg := clientC1AtServer2.Receive()
		inboundMsg := msg.GetInboundMessage()
		require.Equal(t, "forwarded", string(inboundMsg))
		clientC1AtServer2.Send(mb.NewAck(msg))

		// switch back to original server
		roundTrip(t, clientC1, register(1, "spkC1", 37, 38, 39))
		// temporarily corrupt the presence repo to make sure that retries work
		origHost, err := presenceRepo.Find(deviceC1Addr)
		require.NoError(t, err)
		require.NoError(t, presenceRepo.Announce(deviceC1Addr, "garbage"))
		roundTrip(t, clientAnonymous, mb.Build(&model.Message_OutboundMessage{&model.OutboundMessage{
			To:                        deviceC1Addr,
			UnidentifiedSenderMessage: []byte("backhome"),
		}}))
		time.Sleep(ForwardingTimeout / 2)

		// fix presence to make sure message gets delivered
		presenceRepo.Announce(deviceC1Addr, origHost)
		msg = clientC1.Receive()
		inboundMsg = msg.GetInboundMessage()
		require.Equal(t, "backhome", string(inboundMsg))
		clientC1.Send(mb.NewAck(msg))
	})
}
