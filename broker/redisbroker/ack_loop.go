package redisbroker

import "context"

const (
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
