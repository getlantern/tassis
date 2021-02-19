package web

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/getlantern/tassis/broker"
	"github.com/getlantern/tassis/db"
	"github.com/getlantern/tassis/service"
	"github.com/getlantern/tassis/service/serviceimpl"
	"github.com/getlantern/tassis/testsupport"
	"github.com/getlantern/tassis/webclient"

	"testing"

	"github.com/stretchr/testify/require"
)

func testWebSocketClient(t *testing.T, testMultiClientMessaging bool, d db.DB, b broker.Broker) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()

	srvc, err := serviceimpl.New(&serviceimpl.Opts{
		PublicAddr:           l.Addr().String(),
		DB:                   d,
		Broker:               b,
		CheckPreKeysInterval: testsupport.CheckPreKeysInterval,
		LowPreKeysLimit:      testsupport.LowPreKeysLimit,
		NumPreKeysToRequest:  testsupport.NumPreKeysToRequest,
	})
	require.NoError(t, err)

	handler := NewHandler(srvc)
	server := &http.Server{
		Handler: handler,
	}

	go server.Serve(l)

	url := fmt.Sprintf("ws://%s/api", l.Addr().String())

	testsupport.TestService(t, testMultiClientMessaging, d, func(t *testing.T) service.ClientConnection {
		conn, err := webclient.Connect(url, 100)
		require.NoError(t, err)
		return conn
	})

	for i := 0; i < 20; i++ {
		activeConnections := handler.ActiveConnections()
		if activeConnections == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.Zero(t, handler.ActiveConnections(), "shouldn't have any active connections after waiting 2 seconds")
}
