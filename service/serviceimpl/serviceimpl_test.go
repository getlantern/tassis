package serviceimpl

import (
	"fmt"

	"github.com/getlantern/tassis/broker/membroker"
	"github.com/getlantern/tassis/db"
	"github.com/getlantern/tassis/db/memdb"
	"github.com/getlantern/tassis/forwarder/memforwarder"
	"github.com/getlantern/tassis/presence/mempresence"
	"github.com/getlantern/tassis/service"
	"github.com/getlantern/tassis/testsupport"

	"testing"

	"github.com/stretchr/testify/require"
)

func TestServiceInMemory(t *testing.T) {
	presenceRepo := mempresence.NewRepository()

	services := make(map[string]service.Service, 0)
	buildServiceAndDB := func(t *testing.T, serverID int) (service.Service, db.DB) {
		database := memdb.New()

		srvc, err := New(&Opts{
			PublicAddr:           fmt.Sprintf("localhost:%d", serverID),
			DB:                   database,
			Broker:               membroker.New(),
			PresenceRepo:         presenceRepo,
			Forwarder:            memforwarder.New(services),
			CheckPreKeysInterval: testsupport.CheckPreKeysInterval,
			LowPreKeysLimit:      testsupport.LowPreKeysLimit,
			NumPreKeysToRequest:  testsupport.NumPreKeysToRequest,
			ForwardingTimeout:    testsupport.ForwardingTimeout,
			UserTransferInterval: testsupport.UserTransferInterval,
		})
		require.NoError(t, err)
		services[srvc.publicAddr] = srvc
		return srvc, database
	}

	testsupport.TestService(t, false, presenceRepo, buildServiceAndDB)
}
