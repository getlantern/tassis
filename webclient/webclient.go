// package webclient provides a client that connects to a tassis Service via its websocket front end
package webclient

import (
	"sync"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"

	"github.com/getlantern/golog"
	"github.com/getlantern/tassis/model"
	"github.com/getlantern/tassis/service"
)

var (
	log = golog.LoggerFor("webclient")
)

type websocketService struct {
	url         string
	bufferDepth int
}

// NewService returns an implementation of service.Service that connects via websockets to the given url.
func NewService(url string, bufferDepth int) service.Service {
	return &websocketService{
		url:         url,
		bufferDepth: bufferDepth,
	}
}

func (srvc *websocketService) Connect() (service.ClientConnection, error) {
	return Connect(srvc.url, srvc.bufferDepth)
}

// Connect creates a new ServiceConnection using a websocket to the given url.
// bufferDepth specifies how many messages to buffer in either direction.
func Connect(url string, bufferDepth int) (service.ClientConnection, error) {
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}
	result := &websocketClient{
		conn: conn,
		out:  make(chan *model.Message, bufferDepth),
		in:   make(chan *model.Message, bufferDepth),
	}
	go result.write()
	go result.read()
	return result, nil
}

type websocketClient struct {
	conn      *websocket.Conn
	out       chan *model.Message
	in        chan *model.Message
	closeOnce sync.Once
}

func (client *websocketClient) write() {
	defer client.Close()

	for msg := range client.out {
		b, err := proto.Marshal(msg)
		if err != nil {
			log.Errorf("error marshaling outbound message: %v", err)
			return
		}
		err = client.conn.WriteMessage(websocket.BinaryMessage, b)
		if err != nil {
			log.Errorf("error writing outbound message: %v", err)
		}
	}
}

func (client *websocketClient) read() {
	// TODO: tighten up closing logic
	defer client.Close()
	defer close(client.in)

	for {
		_, b, err := client.conn.ReadMessage()
		if err != nil {
			log.Errorf("error reading inbound message: %v", err)
			return
		}
		msg := &model.Message{}
		err = proto.Unmarshal(b, msg)
		if err != nil {
			log.Errorf("error unmarshaling inbound message: %v", err)
			return
		}
		client.in <- msg
	}
}

func (client *websocketClient) Out() chan<- *model.Message {
	return client.out
}

func (client *websocketClient) Send(msg *model.Message) {
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

func (client *websocketClient) In() <-chan *model.Message {
	return client.in
}

func (client *websocketClient) Receive() *model.Message {
	return <-client.in
}

func (client *websocketClient) Drain() int {
	count := 0

	for {
		select {
		case <-client.in:
			count++
		default:
			return count
		}
	}
}

func (client *websocketClient) Close() {
	client.closeOnce.Do(func() {
		log.Debug("closing websocket conn")
		client.conn.Close()
		log.Debug("closed websocket conn")
	})
}
