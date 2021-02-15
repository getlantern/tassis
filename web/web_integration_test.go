// +build integrationtest

package web

import (
	"github.com/go-redis/redis/v8"

	"github.com/getlantern/messaging-server/broker/membroker"
	"github.com/getlantern/messaging-server/db/redisdb"

	"github.com/stretchr/testify/require"
	"testing"
)

func TestWebSocketClientWithRealDatabaseAndBroker(t *testing.T) {
	b := membroker.New()
	d, err := redisdb.New(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // no password set
		DB:       0,  // use default DB
	})
	require.NoError(t, err)
	testWebSocketClient(t, d, b)
}
