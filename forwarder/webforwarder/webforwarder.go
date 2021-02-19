// webforwarder provides an implementation of forwarder by dialing webclient connections
package webforwarder

import (
	"fmt"

	"github.com/getlantern/tassis/forwarder"
	"github.com/getlantern/tassis/service"
	"github.com/getlantern/tassis/webclient"
)

func New(bufferDepth int) *forwarder.Forwarder {
	dial := func(host string) (service.ClientConnection, error) {
		url := fmt.Sprintf("ws://%s/api", host)
		return webclient.Connect(url, bufferDepth)
	}

	return forwarder.New(dial)
}
