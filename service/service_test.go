package service

import (
	"github.com/google/uuid"

	"github.com/getlantern/messaging-server/broker/membroker"
	"github.com/getlantern/messaging-server/db/memdb"
	"github.com/getlantern/messaging-server/model"

	"testing"

	"github.com/stretchr/testify/require"
)

func TestService(t *testing.T) {
	service := New(&Opts{
		DB:     memdb.New(2, 4),
		Broker: membroker.New(),
	})

	userA := uuid.New()
	deviceA1 := uint32(11)
	// deviceA2 := uint32(12)

	// userB := uuid.New()
	// deviceB1 := uint32(21)
	// deviceB2 := uint32(22)

	clientA1, err := service.Connect(userA, deviceA1)
	require.NoError(t, err)

	// clientA2, err := service.Connect(userA, deviceA2)
	// require.NoError(t, err)

	// clientB1, err := service.Connect(userB, deviceB1)
	// require.NoError(t, err)

	// clientB2, err := service.Connect(userB, deviceB2)
	// require.NoError(t, err)

	register, err := model.NewRegister(&model.Register{
		RegistrationID: 1,
		IdentityKey:    []byte("identityKeyA1"),
		SignedPreKey:   []byte("signedPreKeyA1"),
		PreKeys:        [][]byte{[]byte{1}, []byte{2}, []byte{3}},
	})
	require.NoError(t, err)

	register.SetSequence(5)
	clientA1.Out() <- register
	ack := <-clientA1.In()
	require.Equal(t, register.Sequence(), ack.Sequence())
}
