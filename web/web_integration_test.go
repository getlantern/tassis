// +build integrationtest
// +build !smoketest

package web

import (
	"context"

	"github.com/go-redis/redis/v8"

	"github.com/getlantern/tassis/broker"
	"github.com/getlantern/tassis/broker/redisbroker"
	"github.com/getlantern/tassis/db"
	"github.com/getlantern/tassis/db/redisdb"

	"testing"

	"github.com/stretchr/testify/require"
)

func TestWebSocketClientWithRealDatabaseAndBroker(t *testing.T) {
	testWebSocketClient(t, true, true, func(id int) db.DB {
		client := redis.NewClient(&redis.Options{
			Addr:     "localhost:6379",
			Password: "", // no password set
			DB:       id,
		})

		// clear the database
		keys, err := client.Keys(context.Background(), "*").Result()
		require.NoError(t, err)
		if len(keys) > 0 {
			err = client.Del(context.Background(), keys...).Err()
			require.NoError(t, err)
		}

		d, err := redisdb.New(client)
		require.NoError(t, err)
		return d
	}, func(id int) broker.Broker {
		client := redis.NewClient(&redis.Options{
			Addr:     "localhost:6379",
			Password: "", // no password set
			DB:       id,
		})
		b, err := redisbroker.New(client)
		require.NoError(t, err)
		return b
	})
}
