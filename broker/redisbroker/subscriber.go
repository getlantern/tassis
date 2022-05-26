package redisbroker

import (
	"context"
	gerrors "errors"
	"sync"
	"sync/atomic"

	"github.com/go-redis/redis/v8"

	"github.com/getlantern/tassis/broker"
)

type subscriber struct {
	b                  *redisBroker
	stream             string
	messagesOut        chan broker.Message
	messagesIn         chan []*message
	processedLastBatch chan interface{}
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
	sub := &subscriber{
		b:                  b,
		stream:             stream,
		messagesOut:        make(chan broker.Message, 10), // TODO make this tunable
		messagesIn:         make(chan []*message),
		processedLastBatch: make(chan interface{}),
		closeCh:            make(chan interface{}),
	}
	atomic.AddInt64(&b.subscriberCount, 1)
	go sub.process(startingOffset)
	return sub
}

func (sub *subscriber) process(startingOffset string) {
	defer atomic.AddInt64(&sub.b.subscriberCount, -1)
	defer close(sub.messagesOut)

	offset := startingOffset
	for {
		sub.b.subscriberRequests <- &subscriberRequest{sub, offset}
		select {
		case <-sub.closeCh:
			return
		case msgs := <-sub.messagesIn:
			for _, msg := range msgs {
				if offsetLessThan(offset, msg.offset) {
					sub.messagesOut <- msg
					offset = msg.offset
				}
			}
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
	sub.messagesIn <- msgs
	<-sub.processedLastBatch
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
