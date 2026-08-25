package daemon

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/runregistry"
)

func TestRunSupervisor_CompletedRunLeavesNoGoroutineBehind(t *testing.T) {
	registry := runregistry.NewRunRegistry()
	supervisor := newRunSupervisor(registry)
	before := runtime.NumGoroutine()

	supervisor.Start(t.Context(), core.RunID(uuid.MustParse("00000000-0000-0000-0000-000000000001")), &runregistry.RunHandle{},
		func(context.Context) bool { return true }, func(runTerminalResult) {})
	results := supervisor.Wait()

	if len(results) != 1 || !results[0].succeeded {
		t.Fatalf("terminal results = %+v, want one successful run", results)
	}
	if registry.Len() != 0 {
		t.Fatalf("live run registry has %d entries after completion, want 0", registry.Len())
	}
	awaitRunGoroutineCount(t, before)
}

func TestRunSupervisor_CancelledRunLeavesNoGoroutineBehind(t *testing.T) {
	registry := runregistry.NewRunRegistry()
	supervisor := newRunSupervisor(registry)
	handle := &runregistry.RunHandle{}
	started := make(chan struct{})
	before := runtime.NumGoroutine()

	supervisor.Start(t.Context(), core.RunID(uuid.MustParse("00000000-0000-0000-0000-000000000002")), handle, func(ctx context.Context) bool {
		close(started)
		<-ctx.Done()
		return false
	}, func(runTerminalResult) {})
	<-started
	handle.Cancel()
	handle.Cancel()
	results := supervisor.Wait()

	if len(results) != 1 || results[0].succeeded {
		t.Fatalf("terminal results = %+v, want one cancelled unsuccessful run", results)
	}
	if registry.Len() != 0 {
		t.Fatalf("live run registry has %d entries after cancellation, want 0", registry.Len())
	}
	awaitRunGoroutineCount(t, before)
}

func awaitRunGoroutineCount(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() != want && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if got := runtime.NumGoroutine(); got != want {
		t.Fatalf("goroutine count after run = %d, want pre-run count %d", got, want)
	}
}
