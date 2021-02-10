package web

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/getlantern/golog"

	"github.com/getlantern/messaging-server/service"
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
	pathParts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	if len(pathParts) != 2 {
		resp.WriteHeader(http.StatusBadRequest)
		return
	}

	userID, err := uuid.Parse(pathParts[0])
	if err != nil {
		log.Errorf("unable to parse userID %v: %v", pathParts[0], err)
		resp.WriteHeader(http.StatusBadRequest)
		return
	}

	_deviceID, err := strconv.ParseInt(pathParts[1], 10, 64)
	if err != nil {
		log.Errorf("unable to parse deviceID %v: %v", pathParts[1], err)
		resp.WriteHeader(http.StatusBadRequest)
		return
	}
	deviceID := uint32(_deviceID)

	client, err := h.srvc.Connect(userID, deviceID)
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
				err := conn.WriteMessage(websocket.BinaryMessage, msg)
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
			_, msg, err := conn.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err) {
					log.Debugf("error reading: %v", err)
				}
				return
			}
			out <- msg
		}
	}()

	wg.Wait()
}
