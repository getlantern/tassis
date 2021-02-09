package web

import (
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"

	"github.com/getlantern/golog"

	"github.com/getlantern/tassis/model"
	"github.com/getlantern/tassis/service"
)

var (
	log = golog.LoggerFor("web")

	upgrader = websocket.Upgrader{}
)

type Handler interface {
	http.Handler

	// ActiveConnections tells us how many active client connections the Handler has in flight
	ActiveConnections() int
}

type handler struct {
	srvc              *service.Service
	upgrader          *websocket.Upgrader
	activeConnections int64
}

func NewHandler(srvc *service.Service) Handler {
	return &handler{
		srvc:     srvc,
		upgrader: &websocket.Upgrader{},
	}
}

func (h *handler) ActiveConnections() int {
	return int(atomic.LoadInt64(&h.activeConnections))
}

func (h *handler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	if req.RequestURI != "/api" {
		log.Errorf("Unknown request URI: %v", req.RequestURI)
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

	conn, err := h.upgrader.Upgrade(resp, req, nil)
	if err != nil {
		log.Errorf("unable to upgrade to websocket: %v", err)
		return
	}
	defer conn.Close()

	atomic.AddInt64(&h.activeConnections, 1)
	defer atomic.AddInt64(&h.activeConnections, -1)

	var wg sync.WaitGroup
	wg.Add(2)

	stopCh := make(chan interface{})

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
				err = conn.WriteMessage(websocket.BinaryMessage, b)
				if err != nil {
					log.Debugf("error writing: %v", err)
					return
				}
			case <-stopCh:
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		defer close(stopCh)

		out := client.Out()
		for {
			_, b, err := conn.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err) {
					log.Debugf("error reading: %v", err)
				}
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

	wg.Wait()
}
