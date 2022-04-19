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
	_ "embed"
	"strconv"
	"strings"

	"github.com/go-redis/redis/v8"

	"github.com/getlantern/errors"
	"github.com/getlantern/golog"
	"github.com/getlantern/tassis/broker"
	"github.com/getlantern/trace"
)

const (
	emptyOffset = ""
	minOffset   = "0"
)

var (
	log    = golog.LoggerFor("redisdb")
	tracer = trace.NewTracer("redisbroker")
)

//go:embed ack.lua
var ackScript []byte

type redisBroker struct {
	client             *redis.Client
	subscriberRequests chan *subscriberRequest
	acks               chan *ack
	ackScriptSHA       string
}

// New constructs a new Redis-backed Broker that connects with the given client.
func New(client *redis.Client) (broker.Broker, error) {
	ackScriptSHA, err := client.ScriptLoad(context.Background(), string(ackScript)).Result()
	if err != nil {
		return nil, errors.New("unable to load ackScript: %v", err)
	}

	b := &redisBroker{
		client:             client,
		subscriberRequests: make(chan *subscriberRequest, 10000), // TODO: make this tunable
		acks:               make(chan *ack, 10000),               // TODO: make this tunable
		ackScriptSHA:       ackScriptSHA,
	}
	go b.processSubscribers()
	go b.processAcks()
	return b, nil
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
	_, span := tracer.Continue("publish")
	defer span.End()

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
