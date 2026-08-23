package daemon_test

import (
	"testing"
	"time"
)

const workLoopTestBudget = 60 * time.Second

const awaitLoopTeardownMargin = 10 * time.Second

func awaitLoopTeardown(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-awaitLoopTeardownDeadline(t):
		t.Fatalf("%s did not exit before the test deadline after context cancellation", what)
	}
}

func awaitLoopTeardownErr(t *testing.T, ch <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-awaitLoopTeardownDeadline(t):
		t.Fatalf("%s did not exit before the test deadline after context cancellation", what)
		return nil
	}
}

func awaitLoopTeardownDeadline(t *testing.T) <-chan time.Time {
	t.Helper()
	deadline, ok := t.Deadline()
	if !ok {
		return nil
	}
	remaining := time.Until(deadline) - awaitLoopTeardownMargin
	if remaining < time.Second {
		remaining = time.Second
	}
	timer := time.NewTimer(remaining)
	t.Cleanup(func() { timer.Stop() })
	return timer.C
}
