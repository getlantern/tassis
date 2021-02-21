package redisbroker

import (
	"context"
	gerrors "errors"
	"time"

	"github.com/go-redis/redis/v8"
)

type subscriberRequestsByStream map[string][]*subscriberRequest

func (reqs subscriberRequestsByStream) add(req *subscriberRequest) {
	reqs[req.sub.stream] = append(reqs[req.sub.stream], req)
}

// processSubscribers reads messages from Redis streams for active subscribers. It works as follows:
//
// 1. subscriber submits request to b.subscriberRequests with the stream name and the offset from which to read
// 2. processSubscribers coalesces multiple pending requests and determines the minimum required offset per stream
// 3. processSubscribers then reads from Redis using XREAD, blocking for some small amount of time that's large enough to read a batch of messages but not so long as to block pending subscribers for a significant amount of time
// 4. processSubscribers then sends results to all subscribers with pending requests
// 5. if any streams didn't yield results, subscribers to those streams will be carried over to the next loop of processSubscribers
// 6. once the subscribers that did receive results have had a chance to forward them to their clients, they submit a new request to b.subscriberRequests in order to be included in an upcoming loop
//
func (b *redisBroker) processSubscribers() {
	requestsByStream := make(subscriberRequestsByStream)
	for {
		if len(requestsByStream) == 0 {
			// wait for a new request
			req := <-b.subscriberRequests
			requestsByStream.add(req)
		}
		b.coalesceAdditionalSubscriberRequests(requestsByStream)

		b.readStreams(requestsByStream)
	}
}

func (b *redisBroker) coalesceAdditionalSubscriberRequests(requestsByStream subscriberRequestsByStream) {
	for {
		select {
		case req := <-b.subscriberRequests:
			requestsByStream.add(req)
		case <-time.After(250 * time.Millisecond): // TODO: make this tunable
			// no more pending requests
			return
		}
	}
}

func (b *redisBroker) readStreams(requestsByStream subscriberRequestsByStream) {
	streamsWithOffsets := b.gatherStreamsWithOffsets(requestsByStream)

	// ctx, cancel := context.WithCancel(context.Background())
	ctx := context.Background()

	// done := make(chan interface{})
	// gotInterruptRequest := make(chan *subscriberRequest, 1)
	// // cancel blocked xread on added subscription
	// go func() {
	// 	select {
	// 	case req := <-b.subscriberRequests:
	// 		gotInterruptRequest <- req
	// 		cancel()
	// 	case <-done:
	// 		return
	// 	}
	// }()

	streams, err := b.client.XRead(ctx, &redis.XReadArgs{
		Block:   250 * time.Millisecond, // TODO: make this tunable, it should never be so long as to block new subscribers for a substantial amount of time
		Streams: streamsWithOffsets,
		Count:   10000, // TODO: make this tunable
	}).Result()
	// close(done)

	// select {
	// case req := <-gotInterruptRequest:
	// 	requestsByStream.add(req)
	// 	return
	// default:
	// 	// keep going
	// }
	// cancel() // cancel context to free resources

	if err != nil {
		if !gerrors.Is(err, context.Canceled) && !gerrors.Is(err, redis.Nil) {
			// unexpected error, log and wait a little before reconnecting
			log.Error(err)
			time.Sleep(2 * time.Second)
		}
		return
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
			req.sub.send(msgs)
		}
		// since we got something for this stream, delete its subscribers
		delete(requestsByStream, stream.Stream)
	}
}

func (b *redisBroker) gatherStreamsWithOffsets(requestsByStream subscriberRequestsByStream) []string {
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
	return streamsWithOffsets
}
