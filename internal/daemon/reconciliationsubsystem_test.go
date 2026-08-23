package daemon

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

type subpartEmitter struct {
	mu sync.Mutex
	n  map[core.EventType]int
}

func (e *subpartEmitter) Emit(_ context.Context, t core.EventType, _ []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.n == nil {
		e.n = make(map[core.EventType]int)
	}
	e.n[t]++
	return nil
}

func (e *subpartEmitter) count(t core.EventType) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.n[t]
}

func subpartLoadConfig(t *testing.T, yamlContent string) (cfg projectconfig.ProjectConfig, projectRoot string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".harmonik"), 0o750); err != nil {
		t.Fatalf("subpartLoadConfig: MkdirAll: %v", err)
	}
	if yamlContent != "" {
		p := filepath.Join(root, ".harmonik", "config.yaml")
		if err := os.WriteFile(p, []byte(yamlContent), 0o600); err != nil {
			t.Fatalf("subpartLoadConfig: WriteFile: %v", err)
		}
	}
	pc, err := projectconfig.LoadProjectConfig(root)
	if err != nil {
		t.Fatalf("subpartLoadConfig: LoadProjectConfig: %v", err)
	}
	return pc, root
}

// Default state: no subsystems: block → the scheduler is constructed and its
// ticker fires, exactly as before subsystem partitioning existed.
func TestSubsystemPartition_ReconciliationScheduler_DefaultRuns(t *testing.T) {
	t.Parallel()

	pc, root := subpartLoadConfig(t, "schema_version: 1\n")
	emitter := &subpartEmitter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := startReconciliationSchedulerIfEnabled(ctx, pc, ReconciliationSchedulerConfig{
		ProjectDir: root,
		BrPath:     "", // no bead ledger: the tick still emits started/completed
		Interval:   15 * time.Millisecond,
		Emitter:    emitter,
		LogWriter:  io.Discard,
	})
	if !started {
		t.Fatal("startReconciliationSchedulerIfEnabled = false with no subsystems: block; want true (absent config must not disable anything)")
	}

	deadline := time.After(3 * time.Second)
	for emitter.count(core.EventTypeReconciliationStarted) == 0 {
		select {
		case <-deadline:
			t.Fatal("scheduler was reported started but never ticked within 3s")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// Disabled state: the scheduler is ABSENT — never constructed, so no ticker can
// fire no matter how long the test waits.
func TestSubsystemPartition_ReconciliationScheduler_DisabledIsAbsent(t *testing.T) {
	t.Parallel()

	pc, root := subpartLoadConfig(t, `
schema_version: 1
subsystems:
  reconciliation_scheduler:
    enabled: false
`)
	emitter := &subpartEmitter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := startReconciliationSchedulerIfEnabled(ctx, pc, ReconciliationSchedulerConfig{
		ProjectDir: root,
		BrPath:     "",
		Interval:   15 * time.Millisecond,
		Emitter:    emitter,
		LogWriter:  io.Discard,
	})
	if started {
		t.Fatal("startReconciliationSchedulerIfEnabled = true with subsystems.reconciliation_scheduler.enabled: false; the scheduler must be ABSENT, not constructed")
	}

	time.Sleep(300 * time.Millisecond)
	if got := emitter.count(core.EventTypeReconciliationStarted); got != 0 {
		t.Errorf("reconciliation_started emitted %d times after the subsystem was switched off; want 0 (off means absent, not inert)", got)
	}
	if got := emitter.count(core.EventTypeReconciliationCompleted); got != 0 {
		t.Errorf("reconciliation_completed emitted %d times after the subsystem was switched off; want 0", got)
	}
}
