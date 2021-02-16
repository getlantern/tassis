package redisbroker

import (
	"context"
	"time"

	"github.com/go-redis/redis/v8"
)

// CapStreams caps all streams in the redis database to approximately the given maxLength (may remain a little longer after trimming),
// performing its check every checkInterval duration. Capping happens in batches of the given batchSize.
func CapStreams(client *redis.Client, maxLen int, checkInterval time.Duration, batchSize int) {
	for {
		err := doCapStreams(client, int64(maxLen), batchSize)
		if err != nil {
			log.Error(err)
		}
		time.Sleep(checkInterval)
	}
}

func doCapStreams(client *redis.Client, maxLen int64, batchSize int) error {
	// TODO: also delete stuff older than the latest acknowledgement for a given topic
	streams, err := client.Keys(context.Background(), "topic:*").Result()
	if err != nil {
		return err
	}

	batch := make([]string, 0, batchSize)
	commit := func() error {
		ctx := context.Background()
		p := client.Pipeline()
		for _, stream := range batch {
			p.XTrimApprox(ctx, stream, maxLen)
		}
		_, err := p.Exec(ctx)
		return err
	}

	for _, stream := range streams {
		batch = append(batch, stream)
		if len(batch) == batchSize {
			err := commit()
			if err != nil {
				return err
			}
			batch = make([]string, 0, batchSize)
		}
	}

	if len(batch) > 0 {
		// commit remaining items
		return commit()
	}

	return nil
}
