// package web provides a websocket frontend to a tassis service
package web

import (
	"bytes"
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"
	"nhooyr.io/websocket"

	"github.com/getlantern/golog"

	"github.com/getlantern/tassis/model"
	"github.com/getlantern/tassis/service/serviceimpl"
)

var (
	log = golog.LoggerFor("web")

	forceClose = []byte("forceclose") // a special byte sequence that clients can send to force a close (used for testing clients)
)

type Handler interface {
	http.Handler

	// ActiveConnections tells us how many active client connections the Handler has in flight
	ActiveConnections() int
}

type handler struct {
	srvc              *serviceimpl.Service
	activeConnections int64
}

func NewHandler(srvc *serviceimpl.Service) Handler {
	h := &handler{
		srvc: srvc,
	}
	go func() {
		for {
			time.Sleep(10 * time.Second)
			log.Debugf("Active connections: %d", h.ActiveConnections())
		}
	}()
	return h
}

func (h *handler) ActiveConnections() int {
	return int(atomic.LoadInt64(&h.activeConnections))
}

func (h *handler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	if req.RequestURI != "/api" {
		log.Errorf("unknown request URI: %v", req.RequestURI)
		resp.WriteHeader(http.StatusNotFound)
		return
	}

	client, err := h.srvc.Connect()
	if err != nil {
		log.Errorf("unable to connect to service: %v", err)
		resp.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer client.Close()

	conn, err := websocket.Accept(resp, req, nil)
	if err != nil {
		log.Errorf("unable to upgrade to websocket: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "closing normally")

	atomic.AddInt64(&h.activeConnections, 1)
	defer atomic.AddInt64(&h.activeConnections, -1)

	var wg sync.WaitGroup
	wg.Add(2)

	stopCh := make(chan interface{})

	// write to client
	go func() {
		defer wg.Done()

		in := client.In()
		for {
			select {
			case msg := <-in:
				b, err := proto.Marshal(msg)
				if err != nil {
					log.Errorf("error marshalling message: %v", err)
					continue
				}
				ctx, cancel := context.WithTimeout(req.Context(), 10*time.Second) // TODO: make this timeout configurable
				err = conn.Write(ctx, websocket.MessageBinary, b)
				cancel()
				if err != nil {
					log.Debugf("error writing: %v", err)
					return
				}
			case <-stopCh:
				// This was getting verbose
				// log.Debug("client reader closed")
				return
			}
		}
	}()

	// read from client
	go func() {
		defer wg.Done()
		defer close(stopCh)

		out := client.Out()
		for {
			ctx, cancel := context.WithTimeout(req.Context(), 60*time.Second) // TODO: make this timeout configurable
			_, b, err := conn.Read(ctx)
			cancel()
			if err != nil {
				// This was getting verbose
				// if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				// 	log.Debug("websocket closed normally")
				// } else {
				// 	log.Debugf("unexpected error reading: %v", websocket.CloseStatus(err))
				// }
				return
			}
			if bytes.Equal(b, forceClose) {
				// This was getting verbose
				// log.Debug("force closing connection at client's request")
				conn.Close(websocket.StatusNormalClosure, "forced closure")
				return
			}
			msg := &model.Message{}
			err = proto.Unmarshal(b, msg)
			if err != nil {
				log.Errorf("error unmarshalling message: %v", err)
				continue
			}
			out <- msg
		}
	}()

	// ping/pong keepalives
	go func() {
		for {
			time.Sleep(15 * time.Second)
			if err := ping(conn); err != nil {
				// ping failed, connection will have been closed, stop looping
				return
			}
		}
	}()

	wg.Wait()
	conn.Close(websocket.StatusNormalClosure, "")
}

// this pings the client to make sure it's still there and to keep any intermediate routers from timing out
func ping(conn *websocket.Conn) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	return conn.Ping(ctx)
}
