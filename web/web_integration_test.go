// +build integrationtest

package web

import (
	"github.com/go-redis/redis/v8"

	"github.com/getlantern/messaging-server/broker/redisbroker"
	"github.com/getlantern/messaging-server/db/redisdb"

	"github.com/stretchr/testify/require"
	"testing"
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
