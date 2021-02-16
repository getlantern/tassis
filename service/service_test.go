package service

import (
	"github.com/getlantern/messaging-server/broker/membroker"
	"github.com/getlantern/messaging-server/db/memdb"
	"github.com/getlantern/messaging-server/testsupport"

	"testing"

	"github.com/stretchr/testify/require"
)

func TestServiceInMemory(t *testing.T) {
	database := memdb.New()
	srvc, err := New(&Opts{
		DB:                   database,
		Broker:               membroker.New(),
		CheckPreKeysInterval: testsupport.CheckPreKeysInterval,
		LowPreKeysLimit:      testsupport.LowPreKeysLimit,
		NumPreKeysToRequest:  testsupport.NumPreKeysToRequest,
	})
	require.NoError(t, err)

	testsupport.TestService(t, false, database, func(t *testing.T) testsupport.ClientConnectionLike {
		conn, err := srvc.Connect()
		require.NoError(t, err)
		return conn
	})
}
