// package presence provides facilities for announcing and discovering the presence of devices, where presence
// is defined as the hostname of a tassis server through which the device is reachable.
package presence

import (
	"github.com/getlantern/tassis/model"
)

// Repository is a repository for presence information
type Repository interface {
	// Announce announces the presence of an address at a given tassisHost
	Announce(addr *model.Address, tassisHost string) error

	// Find the host at which a given address can be reached
	Find(addr *model.Address) (tassisHost string, err error)
}
