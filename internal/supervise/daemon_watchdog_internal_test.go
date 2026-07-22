package supervise

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

type errorCloser struct{ err error }

func (c errorCloser) Close() error { return c.err }

func TestDaemonWatchdog_CloseErrorDoesNotMarkDaemonDead(t *testing.T) {
	dw := NewDaemonWatchdog(DaemonWatchdogSpec{
		SocketPath: "unused",
		Command:    []string{"unused"},
	}, slog.Default())
	dw.dialDaemon = func(context.Context) (io.Closer, error) {
		return errorCloser{err: errors.New("close failed")}, nil
	}

	if !dw.isDaemonAlive(context.Background()) {
		t.Fatal("successful dial with local close error marked daemon dead")
	}
}
