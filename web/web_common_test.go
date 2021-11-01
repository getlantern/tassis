package web

import (
	"fmt"
	"net"
	"net/http"

	"github.com/getlantern/tassis/broker"
	"github.com/getlantern/tassis/db"
	"github.com/getlantern/tassis/forwarder/webforwarder"
	"github.com/getlantern/tassis/presence/mempresence"
	"github.com/getlantern/tassis/service"
	"github.com/getlantern/tassis/service/serviceimpl"
	"github.com/getlantern/tassis/testsupport"
	"github.com/getlantern/tassis/webclient"

	"testing"

	"github.com/stretchr/testify/require"
)

func testWebSocketClient(t *testing.T, testMultiClientMessaging bool, d func(id int) db.DB, b func(id int) broker.Broker) {
	listeners := make([]net.Listener, 0)
	handlers := make([]Handler, 0)

	defer func() {
		for _, l := range listeners {
			l.Close()
		}
	}()

	presenceRepo := mempresence.NewRepository()
	buildServiceAndDB := func(t *testing.T, serverID int) (service.Service, db.DB) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		listeners = append(listeners, l)

		database := d(serverID)
		addr := l.Addr().String()
		srvc, err := serviceimpl.New(&serviceimpl.Opts{
			PublicAddr:                 addr,
			NumberDomain:               testsupport.NumberDomain,
			DB:                         database,
			Broker:                     b(serverID),
			PresenceRepo:               presenceRepo,
			Forwarder:                  webforwarder.New(100),
			AttachmentsManager:         testsupport.NewNoopAttachmentsManager(),
			CheckPreKeysInterval:       testsupport.CheckPreKeysInterval,
			LowPreKeysLimit:            testsupport.LowPreKeysLimit,
			NumPreKeysToRequest:        testsupport.NumPreKeysToRequest,
			ForwardingTimeout:          testsupport.ForwardingTimeout,
			MinForwardingRetryInterval: testsupport.MinForwardingRetryInterval,
			UserTransferInterval:       testsupport.UserTransferInterval,
		})
		require.NoError(t, err)

		handler := NewHandler(srvc)
		handlers = append(handlers, handler)
		server := &http.Server{
			Handler: handler,
		}

		go server.Serve(l)

		url := fmt.Sprintf("ws://%s/api", l.Addr().String())
		return webclient.NewService(url, 100), database
	}

	testsupport.TestService(t, testMultiClientMessaging, presenceRepo, buildServiceAndDB)
}
