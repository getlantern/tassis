package redisbroker

import (
	"context"
)

func (b *redisBroker) processAcks() {
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
			highestOffset := emptyOffset
			for _, a := range acks {
				if highestOffset == emptyOffset || offsetLessThan(highestOffset, a.offset) {
					highestOffset = a.offset
				}
			}
			if highestOffset != emptyOffset {
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
