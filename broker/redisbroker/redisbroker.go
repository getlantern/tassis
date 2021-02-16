package redisbroker

import (
	"context"
	"time"

	"github.com/getlantern/golog"
	"github.com/getlantern/messaging-server/broker"

	"github.com/go-redis/redis/v8"
)

var (
	log = golog.LoggerFor("redisdb")
)

type message redis.XMessage

func (msg *message) Data() []byte {
	return []byte(msg.Values["data"].(string))
}

func (msg *message) Acker() func() error {
	return nil
}

type subscriber struct {
	b        *redisBroker
	stream   string
	messages chan broker.Message
}

func (sub *subscriber) Messages() <-chan broker.Message {
	return sub.messages
}

func (sub *subscriber) Close() error {
	sub.b.subscriberRemoved <- sub
	return nil
}

type publisher struct {
	b      *redisBroker
	stream string
}

func (pub *publisher) Publish(data []byte) error {
	ctx := context.Background()
	return pub.b.client.XAdd(ctx, &redis.XAddArgs{
		Stream: pub.stream,
		Values: map[string]interface{}{
			"data": string(data),
		},
	}).Err()
}

func (pub *publisher) Close() error {
	return nil
}

type redisBroker struct {
	client            *redis.Client
	subscriberAdded   chan *subscriber
	subscriberRemoved chan *subscriber
}

func New(client *redis.Client) broker.Broker {
	b := &redisBroker{
		client:            client,
		subscriberAdded:   make(chan *subscriber, 100),
		subscriberRemoved: make(chan *subscriber, 100),
	}
	go b.handleSubscriptions()
	return b
}

func (b *redisBroker) handleSubscriptions() {
	subscribers := make(map[string]*subscriber)
	for {
		if len(subscribers) == 0 {
			// wait for a new subscriber
			newSubscriber := <-b.subscriberAdded
			b.subscriberAdded <- newSubscriber
		}

		select {
		case newSubscriber := <-b.subscriberAdded:
			oldSubscriber, hasOldSubscription := subscribers[newSubscriber.stream]
			if hasOldSubscription {
				// only one subscriber per stream at a time
				close(oldSubscriber.messages)
			}
			subscribers[newSubscriber.stream] = newSubscriber
		case removedSubscriber := <-b.subscriberRemoved:
			delete(subscribers, removedSubscriber.stream)
			close(removedSubscriber.messages)
		default:
			streamNames := make([]string, 0, 2*len(subscribers))
			streamOffests := make([]string, 0, len(subscribers))
			for _, subscriber := range subscribers {
				streamNames = append(streamNames, subscriber.stream)
				streamOffests = append(streamOffests, "0")
			}
			streamNames = append(streamNames, streamOffests...)
			ctx, cancel := context.WithCancel(context.Background())

			// cancel blocked xread on removed or added subscription
			go func() {
				select {
				case newSubscriber := <-b.subscriberAdded:
					b.subscriberAdded <- newSubscriber
				case removedSubscriber := <-b.subscriberRemoved:
					b.subscriberRemoved <- removedSubscriber
				}
				cancel()
			}()

			streams, err := b.client.XRead(ctx, &redis.XReadArgs{
				Block:   30 * time.Second,
				Streams: streamNames,
			}).Result()
			if err != nil {
				log.Error(err)
				time.Sleep(2 * time.Second)
				continue
			}
			for _, stream := range streams {
				subscriber := subscribers[stream.Stream]
				for _, msg := range stream.Messages {
					mmsg := message(msg)
					subscriber.messages <- &mmsg
				}
			}
		}
	}
}

func (b *redisBroker) NewSubscriber(topicName string) (broker.Subscriber, error) {
	sub := &subscriber{
		b:        b,
		stream:   streamName(topicName),
		messages: make(chan broker.Message, 100),
	}
	b.subscriberAdded <- sub
	return sub, nil
}

func (b *redisBroker) NewPublisher(topicName string) (broker.Publisher, error) {
	return &publisher{
		b:      b,
		stream: streamName(topicName),
	}, nil
}

func streamName(topicName string) string {
	return "topic:" + topicName
}
