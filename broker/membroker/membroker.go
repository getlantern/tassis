// membroker implements a memory-based broker.Broker. This is not well tested and not intended for production.
package membroker

import (
	"sync"
	"time"

	"github.com/getlantern/tassis/broker"
	"github.com/getlantern/trace"
)

var (
	tracer = trace.NewTracer("membroker")
)

func New() broker.Broker {
	return &membroker{
		topics: make(map[string]*topic),
	}
}

type topic struct {
	seq      int
	ch       chan int
	messages map[int][]byte
	mx       sync.Mutex
}

func (t *topic) Publish(msg []byte) error {
	_, span := tracer.Continue("publish")
	defer span.End()

	t.mx.Lock()
	defer t.mx.Unlock()

	seq := t.seq
	t.seq++
	t.messages[seq] = msg
	t.ch <- seq

	return nil
}

func (t *topic) Close() error {
	return nil
}

func (t *topic) ack(seq int) {
	t.mx.Lock()
	defer t.mx.Unlock()
	delete(t.messages, seq)
}

type message struct {
	t    *topic
	seq  int
	data []byte
}

func (msg *message) Data() []byte {
	return msg.data
}

func (msg *message) Acker() func() error {
	t := msg.t
	seq := msg.seq

	return func() error {
		t.ack(seq)
		return nil
	}
}

type subscriber struct {
	t         *topic
	ch        chan broker.Message
	closeCh   chan interface{}
	closeOnce sync.Once
}

func (s *subscriber) process() {
	defer close(s.ch)

	s.t.mx.Lock()
	copyOfExisting := make(map[int][]byte, len(s.t.messages))
	for seq, data := range s.t.messages {
		copyOfExisting[seq] = data
	}
	s.t.mx.Unlock()

	// re-enqueue old messages
	for seq, data := range copyOfExisting {
		s.ch <- &message{
			t:    s.t,
			seq:  seq,
			data: data,
		}
	}

	// forward new messages
	for {
		select {
		case <-s.closeCh:
			return
		case seq := <-s.t.ch:
			s.t.mx.Lock()
			data := s.t.messages[seq]
			s.t.mx.Unlock()
			if data != nil {
				select {
				case s.ch <- &message{
					t:    s.t,
					seq:  seq,
					data: data,
				}:
				// okay
				case <-time.After(15 * time.Second):
					// give us a chance to read from closeCh
				}
			}
		}
	}
}

func (s *subscriber) Messages() <-chan broker.Message {
	return s.ch
}

func (s *subscriber) Close() error {
	s.closeOnce.Do(func() {
		close(s.closeCh)
	})
	return nil
}

type membroker struct {
	topics map[string]*topic
	mx     sync.Mutex
}

func (mb *membroker) NewSubscriber(topicName string) (broker.Subscriber, error) {
	s := &subscriber{
		t:       mb.getOrCreateTopic(topicName),
		ch:      make(chan broker.Message),
		closeCh: make(chan interface{}),
	}
	go s.process()
	return s, nil
}

func (mb *membroker) NewPublisher(topicName string) (broker.Publisher, error) {
	return mb.getOrCreateTopic(topicName), nil
}

func (mb *membroker) getOrCreateTopic(topicName string) *topic {
	mb.mx.Lock()
	defer mb.mx.Unlock()

	t := mb.topics[topicName]
	if t == nil {
		t = &topic{
			ch:       make(chan int, 10000),
			messages: make(map[int][]byte),
		}
		mb.topics[topicName] = t
	}

	return t
}
