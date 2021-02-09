// redisbroker implements the ../broker.Broker interface using Redis streams. It can run on a cluster.
//
// Each Topic gets its own stream at topic:{<topic>}, for example if the topic is "abcde" the stream is at key "topic:{abcde}".
// The {} braces around the topic indicate that the topic is used as the sharding key when running on a Redis cluster.
//
// Streams are append-only logs from which clients may read starting at any offset, where the offset is determined by the ID
// of the message stored in the stream. redisbroker takes care of tracking the highest previously acknowledged offset by
// stream. This is stored in offset:<stream>, for example "offset:topic:{abcde}".
//
// Whenever a new subscriber is created, it will start receiving messages at the highest recorded offset. Whenever a message is
// acked, the highest recorded offset is updated to the acked ID. However, if an ACK is received out of order, it is ignored.
//
// The broker also supports a stream capping function that can be used in a single batch process. This capping function caps
// streams to a certain length to limit storage, deleting the oldest messages if necessary to stay under the cap.
//
// In practice, this scheme provides at least once delivery semantics as long as messages haven't been lost due to hitting the
// cap before they could be delivered.
package redisbroker

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getlantern/golog"
	"github.com/getlantern/tassis/broker"

	"github.com/go-redis/redis/v8"
)

var (
	log = golog.LoggerFor("redisdb")
)

type redisBroker struct {
	client            *redis.Client
	subscriberAdded   chan *subscriber
	subscriberRemoved chan *subscriber
	nextSubscriberID  int64
}

// New constructs a new Redis-backed Broker that connects with the given client.
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
	subscribers := make(map[string]map[int64]*subscriber)
	var streamNames []string
	var streamOffsets []string
	subscriberWasAdded := false
	for {
		if len(subscribers) == 0 {
			// wait for a new subscriber
			newSubscriber := <-b.subscriberAdded
			b.subscriberAdded <- newSubscriber
		}

		select {
		case newSubscriber := <-b.subscriberAdded:
			subscribersForStream, found := subscribers[newSubscriber.stream]
			if !found {
				subscribersForStream = make(map[int64]*subscriber, 1)
				subscribers[newSubscriber.stream] = subscribersForStream
			}
			subscribersForStream[newSubscriber.id] = newSubscriber
			subscriberWasAdded = true
		case removedSubscriber := <-b.subscriberRemoved:
			subscribersForStream, found := subscribers[removedSubscriber.stream]
			if found {
				delete(subscribersForStream, removedSubscriber.id)
				if len(subscribersForStream) == 0 {
					delete(subscribers, removedSubscriber.stream)
				}
			}
			close(removedSubscriber.messages)
		default:
			if subscriberWasAdded {
				streamNames = make([]string, 0, 2*len(subscribers))
				streamOffsets = make([]string, 0, len(subscribers))
				for stream, subscriberForStream := range subscribers {
					streamNames = append(streamNames, stream)
					// find the lowest offset needed by one of the applicable subscribers
					highOffset := ""
					for _, sub := range subscriberForStream {
						if highOffset == "" || idLessThan(sub.highOffset, highOffset) {
							highOffset = sub.highOffset
						}
					}
					streamOffsets = append(streamOffsets, highOffset)
				}
				subscriberWasAdded = false
			}
			streamsWithOffsets := append(streamNames, streamOffsets...)

			ctx, cancel := context.WithCancel(context.Background())

			// cancel blocked xread on added subscription
			go func() {
				select {
				case newSubscriber := <-b.subscriberAdded:
					b.subscriberAdded <- newSubscriber
				}
				cancel()
			}()

			streams, err := b.client.XRead(ctx, &redis.XReadArgs{
				Block:   30 * time.Second,
				Streams: streamsWithOffsets,
				Count:   1000,
			}).Result()
			if err != nil {
				if !errors.Is(err, context.Canceled) && !errors.Is(err, redis.Nil) {
					// unexpected error, log and wait a little before reconnecting
					log.Error(err)
					time.Sleep(2 * time.Second)
				}
				continue
			}
			for idx, stream := range streams {
				subscribersForStream := subscribers[stream.Stream]
				highOffset := ""
				for _, sub := range subscribersForStream {
					for _, msg := range stream.Messages {
						mmsg := &message{
							b:    b,
							id:   msg.ID,
							sub:  sub,
							data: []byte(msg.Values["data"].(string)),
						}
						sub.highOffsetMx.Lock()
						if idLessThan(sub.highOffset, msg.ID) {
							sub.highOffset = msg.ID
							sub.messages <- mmsg
						}
						sub.highOffsetMx.Unlock()
					}

					sub.highOffsetMx.Lock()
					if highOffset == "" || idLessThan(sub.highOffset, highOffset) {
						highOffset = sub.highOffset
					}
					sub.highOffsetMx.Unlock()
				}

				// update offset on stream for next read
				streamOffsets[idx] = highOffset
			}
		}
	}
}

func (b *redisBroker) NewSubscriber(topicName string) (broker.Subscriber, error) {
	stream := streamName(topicName)
	offset, err := b.client.Get(context.Background(), offsetName(stream)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			offset = "0"
		} else {
			return nil, err
		}
	}
	sub := &subscriber{
		id:         atomic.AddInt64(&b.nextSubscriberID, 1),
		b:          b,
		stream:     stream,
		highOffset: offset,
		messages:   make(chan broker.Message, 100),
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

type message struct {
	b    *redisBroker
	sub  *subscriber
	id   string
	data []byte
}

func (msg *message) Data() []byte {
	return msg.data
}

func (msg *message) Acker() func() error {
	a := &acker{
		b:   msg.b,
		sub: msg.sub,
		id:  msg.id,
	}
	return a.ack
}

type acker struct {
	b   *redisBroker
	sub *subscriber
	id  string
}

func (a *acker) ack() error {
	a.sub.highOffsetMx.Lock()
	shouldAck := a.id >= a.sub.highOffset
	a.sub.highOffsetMx.Unlock()
	if shouldAck {
		return a.b.client.Set(context.Background(), offsetName(a.sub.stream), a.id, 0).Err()
	}
	return nil
}

type subscriber struct {
	id           int64
	b            *redisBroker
	stream       string
	messages     chan broker.Message
	highOffset   string
	highOffsetMx sync.Mutex
	closeOnce    sync.Once
}

func (sub *subscriber) Messages() <-chan broker.Message {
	return sub.messages
}

func (sub *subscriber) Close() error {
	sub.closeOnce.Do(func() {
		sub.b.subscriberRemoved <- sub
	})
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

func streamName(topicName string) string {
	return "topic:{" + topicName + "}"
}

func offsetName(stream string) string {
	return "offset:" + stream
}

func idLessThan(a, b string) bool {
	aParts := strings.Split(a, "-")
	bParts := strings.Split(b, "-")
	if aParts[0] < bParts[0] {
		return true
	}
	if len(aParts) == 1 || len(bParts) == 1 {
		return false
	}
	aSeq, _ := strconv.Atoi(aParts[1])
	bSeq, _ := strconv.Atoi(bParts[1])
	return aSeq < bSeq
}
