package supervisecmd

import (
	"os"
	"os/signal"
)

var signalNotify = func(c chan<- os.Signal, sig ...os.Signal) {
	signal.Notify(c, sig...)
}
