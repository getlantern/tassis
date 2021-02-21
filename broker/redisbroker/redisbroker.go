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
	gerrors "errors"
	"strconv"
	"strings"
	"time"

	"github.com/getlantern/errors"
	"github.com/getlantern/golog"
	"github.com/getlantern/tassis/broker"

	"github.com/go-redis/redis/v8"
)

const (
	emptyOffset = ""
	minOffset   = "0"

	ackScript = `
local offsetKey = KEYS[1]
local newOffset = ARGV[1]

local oldOffset = redis.call("get", offsetKey)
if oldOffset then
	local newOffsetParts = {}
	for str in string.gmatch(newOffset, "[^-]+") do
		table.insert(newOffsetParts, str)
	end
	local oldOffsetParts = {}
	for str in string.gmatch(oldOffset, "[^-]+") do
		table.insert(oldOffsetParts, str)
	end
	if newOffsetParts[1] > oldOffsetParts[1] or (newOffsetParts[1] == oldOffsetParts[1] and newOffsetParts[2] > oldOffsetParts[2]) then
		redis.call("set", offsetKey, newOffset)
		return 1
	end
else
	redis.call("set", offsetKey, newOffset)
	return 1
end

return 0
`
)

var (
	log = golog.LoggerFor("redisdb")
)

type redisBroker struct {
	client             *redis.Client
	subscriberRequests chan *subscriberRequest
	acks               chan *ack
	ackScriptSHA       string
}

// New constructs a new Redis-backed Broker that connects with the given client.
func New(client *redis.Client) (broker.Broker, error) {
	ackScriptSHA, err := client.ScriptLoad(context.Background(), ackScript).Result()
	if err != nil {
		return nil, errors.New("unable to load ackScript: %v", err)
	}

	b := &redisBroker{
		client:             client,
		subscriberRequests: make(chan *subscriberRequest, 10000), // TODO: make this tunable
		acks:               make(chan *ack, 10000),               // TODO: make this tunable
		ackScriptSHA:       ackScriptSHA,
	}
	go b.handleSubscribers()
	go b.handleAcks()
	return b, nil
}

func (b *redisBroker) handleSubscribers() {
	requestsByStream := make(map[string][]*subscriberRequest)
	for {
		if len(requestsByStream) == 0 {
			// wait for a new request
			req := <-b.subscriberRequests
			requestsByStream[req.sub.stream] = append(requestsByStream[req.sub.stream], req)
		}
	coalesce:
		for {
			select {
			case additionalReq := <-b.subscriberRequests:
				requestsByStream[additionalReq.sub.stream] = append(requestsByStream[additionalReq.sub.stream], additionalReq)
			default:
				// no more pending requests
				break coalesce
			}
		}

		streamsWithOffsets := make([]string, 0, len(requestsByStream)*2)
		offsets := make([]string, 0, len(requestsByStream))
		for stream, requestsForStream := range requestsByStream {
			streamsWithOffsets = append(streamsWithOffsets, stream)
			lowestOffset := emptyOffset
			for _, req := range requestsForStream {
				if lowestOffset == emptyOffset || offsetLessThan(req.offset, lowestOffset) {
					lowestOffset = req.offset
				}
			}
			offsets = append(offsets, lowestOffset)
		}

		// add offsets to streams for full list of Redis arguments
		streamsWithOffsets = append(streamsWithOffsets, offsets...)

		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan interface{})
		// cancel blocked xread on added subscription
		go func() {
			select {
			case carryoverRequest := <-b.subscriberRequests:
				cancel()
				b.subscriberRequests <- carryoverRequest
			case <-done:
				return
			}
		}()

		streams, err := b.client.XRead(ctx, &redis.XReadArgs{
			Block:   250 * time.Millisecond, // TODO: make this tunable
			Streams: streamsWithOffsets,
			Count:   10000, // TODO: make this tunable
		}).Result()
		close(done)

		if err != nil {
			if !gerrors.Is(err, context.Canceled) && !gerrors.Is(err, redis.Nil) {
				// unexpected error, log and wait a little before reconnecting
				log.Error(err)
				time.Sleep(2 * time.Second)
			}
			continue
		}

		for _, stream := range streams {
			requestsForStream := requestsByStream[stream.Stream]
			for _, req := range requestsForStream {
				msgs := make([]*message, 0, len(stream.Messages))
				for _, msg := range stream.Messages {
					msgs = append(msgs, &message{
						b:      b,
						offset: msg.ID,
						sub:    req.sub,
						data:   []byte(msg.Values["data"].(string)),
					})
				}
				req.sub.messagesIn <- msgs
			}
		}

		requestsByStream = make(map[string][]*subscriberRequest)
	}
}

func (b *redisBroker) handleAcks() {
	for a := range b.acks {
		acksByStream := map[string][]*ack{
			a.stream: {a},
		}
		// gather additional pending acks
	coalesce:
		for {
			select {
			case aa := <-b.acks:
				acksByStream[aa.stream] = append(acksByStream[aa.stream], aa)
			default:
				break coalesce
			}
		}

		ctx := context.Background()
		p := b.client.Pipeline()
		for stream, acks := range acksByStream {
			highestOffset := minOffset
			for _, a := range acks {
				if offsetLessThan(highestOffset, a.offset) {
					highestOffset = a.offset
				}
			}
			if highestOffset != minOffset {
				offsetKey := offsetName(stream)
				p.EvalSha(ctx,
					b.ackScriptSHA,
					[]string{offsetKey},
					highestOffset)
			}
		}

		_, err := p.Exec(ctx)
		for _, acks := range acksByStream {
			for _, a := range acks {
				a.errCh <- err
			}
		}
	}
}

func (b *redisBroker) NewPublisher(topicName string) (broker.Publisher, error) {
	return &publisher{
		b:      b,
		stream: streamName(topicName),
	}, nil
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

func offsetLessThan(a, b string) bool {
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
