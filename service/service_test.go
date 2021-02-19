package service

import (
	"github.com/getlantern/tassis/broker/membroker"
	"github.com/getlantern/tassis/db/memdb"
	"github.com/getlantern/tassis/testsupport"

	"testing"

	"github.com/stretchr/testify/require"
)

func TestServiceInMemory(t *testing.T) {
	database := memdb.New()
	srvc, err := New(&Opts{
		PublicAddr:           "localhost:0",
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
