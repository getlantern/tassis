package redisbroker

import (
	"context"
	gerrors "errors"
	"sync"

	"github.com/go-redis/redis/v8"

	"github.com/getlantern/tassis/broker"
	"github.com/getlantern/tassis/telemetry"
	"github.com/getlantern/uuid"
)

type subscriber struct {
	id                 string
	b                  *redisBroker
	stream             string
	offset             string
	messagesOut        chan broker.Message
	messagesIn         chan []*message
	processedLastBatch chan interface{}
	offsetMx           sync.RWMutex
	closeOnce          sync.Once
	closeCh            chan interface{}
}

func (b *redisBroker) NewSubscriber(topicName string) (broker.Subscriber, error) {
	stream := streamName(topicName)
	offset, err := b.client.Get(context.Background(), offsetName(stream)).Result()
	if err != nil {
		if gerrors.Is(err, redis.Nil) {
			offset = minOffset
		} else {
			return nil, err
		}
	}

	return b.newSubscriber(stream, offset), nil
}

func (b *redisBroker) newSubscriber(stream string, startingOffset string) *subscriber {
	id := uuid.New().String()
	sub := &subscriber{
		id:                 id,
		b:                  b,
		stream:             stream,
		offset:             startingOffset,
		messagesOut:        make(chan broker.Message, 10), // TODO make this tunable
		messagesIn:         make(chan []*message),
		processedLastBatch: make(chan interface{}),
		closeCh:            make(chan interface{}),
	}

	b.subscribersMx.Lock()
	subscribersById := b.subscribersByStream[stream]
	if subscribersById == nil {
		subscribersById = make(map[string]*subscriber)
		b.subscribersByStream[stream] = subscribersById
	}
	subscribersById[id] = sub
	b.subscribersMx.Unlock()
	go sub.process()
	return sub
}

func (sub *subscriber) process() {
	defer func() {
		sub.b.subscribersMx.Lock()
		subscribersById := sub.b.subscribersByStream[sub.stream]
		delete(subscribersById, sub.id)
		if len(subscribersById) == 0 {
			delete(sub.b.subscribersByStream, sub.stream)
		}
		sub.b.subscribersMx.Unlock()
	}()
	defer close(sub.messagesOut)

	for {
		select {
		case <-sub.closeCh:
			return
		case msgs := <-sub.messagesIn:
			sub.offsetMx.Lock()
			for _, msg := range msgs {
				if offsetLessThan(sub.offset, msg.offset) {
					sub.messagesOut <- msg
					if telemetry.MessagesReceived != nil {
						telemetry.MessagesReceived.Add(context.Background(), 1)
					}
					sub.offset = msg.offset
				}
			}
			sub.offsetMx.Unlock()
			sub.processedLastBatch <- nil
			select {
			case <-sub.closeCh:
				return
			default:
				// continue
			}
		}
	}
}

func (sub *subscriber) send(msgs []*message) {
	select {
	case <-sub.closeCh:
		// closed
		return
	case sub.messagesIn <- msgs:
		<-sub.processedLastBatch
	}
}

func (sub *subscriber) Messages() <-chan broker.Message {
	return sub.messagesOut
}

func (sub *subscriber) Close() error {
	sub.closeOnce.Do(func() {
		close(sub.closeCh)
	})
	return nil
}

type message struct {
	b      *redisBroker
	sub    *subscriber
	offset string
	data   []byte
}

func (msg *message) Data() []byte {
	return msg.data
}

func (msg *message) Acker() func() error {
	a := &ack{
		b:      msg.b,
		stream: msg.sub.stream,
		offset: msg.offset,
		errCh:  make(chan error),
	}
	return a.ack
}

func (a *ack) ack() error {
	a.b.acks <- a
	return <-a.errCh
}

type ack struct {
	b      *redisBroker
	stream string
	offset string
	errCh  chan error
}

type subscriberRequest struct {
	sub    *subscriber
	offset string
}
