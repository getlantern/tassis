package redisbroker

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/getlantern/errors"
)

// PeriodicallyTrimStreams runs TrimStreams every trimInterval
func PeriodicallyTrimStreams(client *redis.Client, maxLen int, trimInterval time.Duration, batchSize int) {
	for {
		err := TrimStreams(client, maxLen, batchSize)
		if err != nil {
			log.Error(err)
		}
		time.Sleep(trimInterval)
	}
}

// TrimStreams removes acked messages from all streams in the redis database, and furthermore caps them to approximately the
// given maxLength (may remain a little longer after trimming). Capping is processed in batches of the given batchSize.
func TrimStreams(client *redis.Client, maxLen int, batchSize int) error {
	// TODO: actually respect bachSize by using a cursor or something
	ctx := context.Background()
	streams, err := client.Keys(ctx, "topic:*").Result()
	if err != nil {
		return errors.New("error while scanning topics: %v", err)
	}
	if len(streams) == 0 {
		return nil
	}

	offsetKeys := make([]string, 0, len(streams))
	for _, stream := range streams {
		offsetKeys = append(offsetKeys, offsetName(stream))
	}

	offsets, err := client.MGet(ctx, offsetKeys...).Result()
	if err != nil {
		log.Errorf("error while reading offsets, ignoring: %v", err)
	}

	p := client.Pipeline()
	hasOffsetToTrim := false
	for i, stream := range streams {
		offset := offsets[i]
		// _, isError := offset.(error)
		if offset != nil {
			// need to increment the offset by 1 since xtrim minid treats the offset exlusively
			offsetPlusOne := idPlusOne(offset.(string))
			cmd := redis.NewIntCmd(ctx, "xtrim", stream, "minid", offsetPlusOne)
			_ = p.Process(ctx, cmd) // ignoring error because pipeline.Process always returns a nil error
			hasOffsetToTrim = true
		}
	}

	if hasOffsetToTrim {
		_, err = p.Exec(ctx)
		if err != nil {
			log.Errorf("error while trimming streams to offsets, ignoring: %v", err)
		}
	}

	p = client.Pipeline()
	for _, stream := range streams {
		p.XTrim(ctx, stream, int64(maxLen))
	}
	_, err = p.Exec(ctx)
	if err != nil {
		log.Errorf("error while trimming streams to max length, ignoring: %v", err)
	}

	return nil
}

func idPlusOne(id string) string {
	parts := strings.Split(id, "-")
	if len(parts) != 2 {
		return id
	}
	seq, _ := strconv.Atoi(parts[1])
	return fmt.Sprintf("%v-%d", parts[0], seq+1)
}
