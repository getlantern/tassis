package redisbroker

import (
	"context"
	gerrors "errors"
	"time"

	"github.com/go-redis/redis/v8"
)

// processSubscribers reads messages from Redis streams for active subscribers. It works as follows:
//
// 1. subscriber reqisters itselfs as present
// 2. processSubscribers interrogates each subscriber's current read offset to determine the minimum required offset per stream
// 3. processSubscribers then reads from Redis using XREAD, blocking for some small amount of time that's large enough to read a batch of messages but not so long as to block pending subscribers for a significant amount of time
// 4. processSubscribers then sends results to all relevant registered subscribers
func (b *redisBroker) processSubscribers() {
	for {
		b.readStreams()
	}
}

func (b *redisBroker) readStreams() {
	streamsWithOffsets := b.gatherStreamsWithOffsets()
	if len(streamsWithOffsets) == 0 {
		// no streams, sleep and try again
		time.Sleep(250 * time.Millisecond)
		return
	}

	ctx := context.Background()

	streams, err := b.client.XRead(ctx, &redis.XReadArgs{
		Block:   250 * time.Millisecond, // TODO: make this tunable, it should never be so long as to block new subscribers for a substantial amount of time
		Streams: streamsWithOffsets,
		Count:   10000, // TODO: make this tunable
	}).Result()
	if err != nil {
		if !gerrors.Is(err, context.Canceled) && !gerrors.Is(err, redis.Nil) {
			// unexpected error, log and wait a little before reconnecting
			log.Error(err)
			time.Sleep(250 * time.Millisecond)
		}
		return
	}

	log.Debugf("Read %d streams", len(streams))
	b.subscribersMx.RLock()
	for _, stream := range streams {
		subscribersForStream := b.subscribersByStream[stream.Stream]
		for _, sub := range subscribersForStream {
			msgs := make([]*message, 0, len(stream.Messages))
			for _, msg := range stream.Messages {
				msgs = append(msgs, &message{
					b:      b,
					offset: msg.ID,
					sub:    sub,
					data:   []byte(msg.Values["data"].(string)),
				})
			}
			sub.send(msgs)
		}
	}
	b.subscribersMx.RUnlock()
}

func (b *redisBroker) gatherStreamsWithOffsets() []string {
	b.subscribersMx.RLock()
	defer b.subscribersMx.RUnlock()

	streamsWithOffsets := make([]string, 0, len(b.subscribersByStream)*2)
	offsets := make([]string, 0, len(b.subscribersByStream))
	for stream, subscribersForStream := range b.subscribersByStream {
		streamsWithOffsets = append(streamsWithOffsets, stream)
		lowestOffset := emptyOffset
		for _, sub := range subscribersForStream {
			sub.offsetMx.RLock()
			offset := sub.offset
			sub.offsetMx.RUnlock()
			if lowestOffset == emptyOffset || offsetLessThan(offset, lowestOffset) {
				lowestOffset = offset
			}
		}
		offsets = append(offsets, lowestOffset)
	}

	// add offsets to streams for full list of Redis arguments
	streamsWithOffsets = append(streamsWithOffsets, offsets...)
	return streamsWithOffsets
}
