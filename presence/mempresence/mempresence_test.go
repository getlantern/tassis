package mempresence

import (
	"testing"

	"github.com/getlantern/tassis/model"
	"github.com/stretchr/testify/require"
)

func TestPresence(t *testing.T) {
	r := NewRepository()

	addr := &model.Address{
		UserID:   []byte("user id"),
		DeviceID: 5,
	}

	_, err := r.Find(addr)
	require.Error(t, err)
	require.EqualValues(t, model.ErrUnknownDevice, err)

	err = r.Announce(addr, "thehost")
	require.NoError(t, err)

	host, err := r.Find(addr)
	require.NoError(t, err)
	require.Equal(t, "thehost", host)
}
