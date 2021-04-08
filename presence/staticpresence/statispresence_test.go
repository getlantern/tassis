package staticpresence

import (
	"testing"

	"github.com/getlantern/tassis/model"
	"github.com/stretchr/testify/require"
)

const (
	tassisHost = "mytassishost"
)

func TestPresence(t *testing.T) {
	r := NewRepository(tassisHost)

	addr := &model.Address{
		IdentityKey: []byte("identityKey"),
		DeviceId:    []byte{5},
	}

	host, err := r.Find(addr)
	require.NoError(t, err)
	require.EqualValues(t, tassisHost, host)

	err = r.Announce(addr, "thehost")
	require.NoError(t, err)

	host, err = r.Find(addr)
	require.NoError(t, err)
	require.EqualValues(t, tassisHost, host)
}
