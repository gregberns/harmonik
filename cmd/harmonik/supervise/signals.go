package supervisecmd

import (
	"os"
	"os/signal"
)

// signalNotify is a package-level var wrapping signal.Notify so tests can
// substitute a no-op without modifying production behaviour.
var signalNotify = func(c chan<- os.Signal, sig ...os.Signal) {
	signal.Notify(c, sig...)
}
