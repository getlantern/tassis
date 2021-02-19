// +build !smoketest

package web

import (
	"github.com/getlantern/tassis/broker/membroker"
	"github.com/getlantern/tassis/db/memdb"

	"testing"
)

func TestWebSocketClientInMemory(t *testing.T) {
	testWebSocketClient(t, false, memdb.New, membroker.New)
}
