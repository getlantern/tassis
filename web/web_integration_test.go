// +build integrationtest
// +build !smoketest

package web

import (
	"github.com/go-redis/redis/v8"

	"github.com/getlantern/tassis/broker"
	"github.com/getlantern/tassis/broker/redisbroker"
	"github.com/getlantern/tassis/db"
	"github.com/getlantern/tassis/db/redisdb"

	"testing"

	"github.com/stretchr/testify/require"
)

func TestWebSocketClientWithRealDatabaseAndBroker(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	testWebSocketClient(t, true, func() db.DB {
		d, err := redisdb.New(client)
		require.NoError(t, err)
		return d
	}, func() broker.Broker {
		return redisbroker.New(client)
	})
}
