// +build !smoketest

package web

import (
	"github.com/getlantern/tassis/broker/membroker"
	"github.com/getlantern/tassis/db/memdb"

	"testing"
)

func TestWebSocketClientInMemory(t *testing.T) {
	b := membroker.New()
	d := memdb.New()
	testWebSocketClient(t, false, d, b)
}
