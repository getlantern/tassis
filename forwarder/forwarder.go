// package forwarder provides a facility for forwarding messages to another tassis cluster
package forwarder

import "github.com/getlantern/tassis/model"

// Client can be used to forward messages to another tassis
type Send func(msg *model.ForwardedMessage, onSuccess func(), onError func(err error))
