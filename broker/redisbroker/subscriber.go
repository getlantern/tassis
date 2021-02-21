package redisbroker

import (
	"context"
	gerrors "errors"
	"sync"

	"github.com/getlantern/tassis/broker"
	"github.com/go-redis/redis/v8"
)

type subscriberRequest struct {
	sub    *subscriber
	offset string
}

type subscriber struct {
	b           *redisBroker
	stream      string
	messagesOut chan broker.Message
	messagesIn  chan []*message
	closeOnce   sync.Once
	closeCh     chan interface{}
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
		b:           b,
		stream:      stream,
		messagesOut: make(chan broker.Message, 100), // TODO make this tunable
		messagesIn:  make(chan []*message),
		closeCh:     make(chan interface{}),
	}
	go sub.process(startingOffset)
	return sub
}

func (sub *subscriber) process(startingOffset string) {
	defer close(sub.messagesOut)

	offset := startingOffset
	for {
		sub.b.subscriberRequests <- &subscriberRequest{sub, offset}
		msgs := <-sub.messagesIn
		for _, msg := range msgs {
			if offsetLessThan(offset, msg.offset) {
				select {
				case sub.messagesOut <- msg:
					offset = msg.offset
				default:
					// backloged
				}
			}
		}
		select {
		case <-sub.closeCh:
			return
		default:
			// continue
		}
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

type ack struct {
	b       *redisBroker
	stream  string
	offset  string
	errCh   chan error
	ackOnce sync.Once
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
