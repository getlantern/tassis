// +build integrationtest
// +build !smoketest

package web

import (
	"github.com/go-redis/redis/v8"

	"github.com/getlantern/tassis/broker/redisbroker"
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
	b := redisbroker.New(client)
	d, err := redisdb.New(client)
	require.NoError(t, err)
	testWebSocketClient(t, true, d, b)
}
