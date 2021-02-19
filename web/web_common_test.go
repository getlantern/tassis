package web

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/getlantern/tassis/broker"
	"github.com/getlantern/tassis/db"
	"github.com/getlantern/tassis/forwarder/memforwarder"
	"github.com/getlantern/tassis/presence/mempresence"
	"github.com/getlantern/tassis/service"
	"github.com/getlantern/tassis/service/serviceimpl"
	"github.com/getlantern/tassis/testsupport"
	"github.com/getlantern/tassis/webclient"

	"testing"

	"github.com/stretchr/testify/require"
)

func testWebSocketClient(t *testing.T, testMultiClientMessaging bool, d func() db.DB, b func() broker.Broker) {
	listeners := make([]net.Listener, 0)
	handlers := make([]Handler, 0)

	defer func() {
		for _, l := range listeners {
			l.Close()
		}
	}()

	defer func() {
		for _, handler := range handlers {
			for i := 0; i < 20; i++ {
				activeConnections := handler.ActiveConnections()
				if activeConnections == 0 {
					return
				}
				time.Sleep(100 * time.Millisecond)
			}
			require.Zero(t, handler.ActiveConnections(), "shouldn't have any active connections after waiting 2 seconds")
		}
	}()

	presenceRepo := mempresence.NewRepository()
	services := make(map[string]service.Service, 0)
	buildServiceAndDB := func(t *testing.T, serverID int) (service.Service, db.DB) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		listeners = append(listeners, l)

		database := d()
		addr := l.Addr().String()
		srvc, err := serviceimpl.New(&serviceimpl.Opts{
			PublicAddr:           addr,
			DB:                   database,
			Broker:               b(),
			PresenceRepo:         presenceRepo,
			Forwarder:            memforwarder.New(services),
			CheckPreKeysInterval: testsupport.CheckPreKeysInterval,
			LowPreKeysLimit:      testsupport.LowPreKeysLimit,
			NumPreKeysToRequest:  testsupport.NumPreKeysToRequest,
		})
		require.NoError(t, err)
		services[addr] = srvc

		handler := NewHandler(srvc)
		handlers = append(handlers, handler)
		server := &http.Server{
			Handler: handler,
		}

		go server.Serve(l)

		url := fmt.Sprintf("ws://%s/api", l.Addr().String())
		return webclient.NewService(url, 100), database
	}

	testsupport.TestService(t, testMultiClientMessaging, buildServiceAndDB)
}
