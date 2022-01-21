// +build !smoketest

package web

import (
	"github.com/getlantern/tassis/broker"
	"github.com/getlantern/tassis/broker/membroker"
	"github.com/getlantern/tassis/db"
	"github.com/getlantern/tassis/db/memdb"

	"testing"
)

func TestWebSocketClientInMemory(t *testing.T) {
	testWebSocketClient(t, false, false, func(id int) db.DB {
		return memdb.New()
	}, func(id int) broker.Broker {
		return membroker.New()
	})
}
