package web

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"

	"github.com/getlantern/messaging-server/broker"
	"github.com/getlantern/messaging-server/broker/membroker"
	"github.com/getlantern/messaging-server/db"
	"github.com/getlantern/messaging-server/db/memdb"
	"github.com/getlantern/messaging-server/model"
	"github.com/getlantern/messaging-server/service"
	"github.com/getlantern/messaging-server/testsupport"

	"testing"

	"github.com/stretchr/testify/require"
)

func TestWebSocketClientInMemory(t *testing.T) {
	b := membroker.New()
	d := memdb.New()
	testWebSocketClient(t, d, b)
}

func testWebSocketClient(t *testing.T, d db.DB, b broker.Broker) {
	srvc, err := service.New(&service.Opts{
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

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	go server.Serve(l)

	testsupport.TestService(t, d, func(t *testing.T) testsupport.ClientConnectionLike {
		url := fmt.Sprintf("ws://%s/api", l.Addr().String())
		t.Logf("connecting to %v", url)
		conn, _, err := websocket.DefaultDialer.Dial(url, nil)
		require.NoError(t, err)
		result := &websocketClientLike{conn, make(chan *model.Message)}
		go result.read()
		return result
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

type websocketClientLike struct {
	conn *websocket.Conn
	msgs chan *model.Message
}

func (client *websocketClientLike) read() {
	defer close(client.msgs)

	for {
		_, b, err := client.conn.ReadMessage()
		if err != nil {
			log.Error(err)
			return
		}
		msg := &model.Message{}
		err = proto.Unmarshal(b, msg)
		if err != nil {
			log.Error(err)
			return
		}
		client.msgs <- msg
	}
}

func (client *websocketClientLike) Send(msg *model.Message) {
	b, err := proto.Marshal(msg)
	if err != nil {
		log.Error(err)
		return
	}
	err = client.conn.WriteMessage(websocket.BinaryMessage, b)
	if err != nil {
		log.Error(err)
	}
}

func (client *websocketClientLike) Receive() *model.Message {
	return <-client.msgs
}

func (client *websocketClientLike) Drain() int {
	count := 0

	for {
		select {
		case <-client.msgs:
			count++
		default:
			return count
		}
	}
}

func (client *websocketClientLike) Close() {
	client.conn.Close()
}
