package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

type intentDurabilityEmitter struct {
	mu    sync.Mutex
	calls int
}

func (e *intentDurabilityEmitter) Emit(context.Context, core.EventType, []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	return nil
}

func (e *intentDurabilityEmitter) EmitWithRunID(context.Context, core.RunID, core.EventType, []byte) error {
	return e.Emit(context.Background(), "", nil)
}

func (e *intentDurabilityEmitter) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

type intentDurabilityLedger struct{}

func (intentDurabilityLedger) LookupStatus(context.Context, core.BeadID) (queue.BeadStatus, error) {
	return queue.BeadStatusOpen, nil
}

func (intentDurabilityLedger) BlocksEdge(context.Context, core.BeadID, core.BeadID) (bool, error) {
	return false, nil
}

func intentDurabilityBlockedProjectDir(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("file"), 0o600); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}
	return path
}

func TestGroupCompletionCommitFailureEmitsNoIntent(t *testing.T) {
	emitter := &intentDurabilityEmitter{}
	store := queuewiring.NewQueueStore()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0197b5e1-0000-7000-8000-000000000101",
		Name:          queue.QueueNameMain,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindStream,
			Status:     queue.GroupStatusActive,
			Items:      []queue.Item{{BeadID: "hk-intent-terminal", Status: queue.ItemStatusPending}},
		}},
	}
	store.SetQueue(q)
	port := reapSeamPort{
		bus:           emitter,
		projectDir:    intentDurabilityBlockedProjectDir(t),
		queueStore:    store,
		runRegistry:   newLocalRunRegistry(),
		maxConcurrent: 1,
	}

	evaluateGroupAdvanceWithOutcome(t.Context(), port, queue.QueueNameMain, q.QueueID, 0, 0, true, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))

	if got := emitter.count(); got != 0 {
		t.Fatalf("emit calls = %d, want 0 after completion commit failure", got)
	}
}

func TestGroupCompletionCleanupFailureEmitsCommittedIntent(t *testing.T) {
	emitter := &intentDurabilityEmitter{}
	store := queuewiring.NewQueueStore()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0197b5e1-0000-7000-8000-000000000102",
		Name:          queue.QueueNameMain,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindStream,
			Status:     queue.GroupStatusActive,
			Items:      []queue.Item{{BeadID: "hk-intent-cleanup", Status: queue.ItemStatusPending}},
		}},
	}
	store.SetQueue(q)
	port := reapSeamPort{
		bus:           emitter,
		projectDir:    t.TempDir(),
		queueStore:    store,
		runRegistry:   newLocalRunRegistry(),
		maxConcurrent: 1,
		completeQueue: func(context.Context, string, *queue.Queue) queue.TerminalResult {
			return queue.TerminalResult{Committed: true, CleanupErr: errors.New("cleanup failed")}
		},
	}

	evaluateGroupAdvanceWithOutcome(t.Context(), port, queue.QueueNameMain, q.QueueID, 0, 0, true, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))

	if got := emitter.count(); got != 1 {
		t.Fatalf("emit calls = %d, want 1 after durable completion with cleanup failure", got)
	}
}

func TestEagerRefillPersistFailureEmitsNoIntent(t *testing.T) {
	emitter := &intentDurabilityEmitter{}
	store := queuewiring.NewQueueStore()
	store.SetQueue(em063FixtureStreamQueueWithBeads())

	root := t.TempDir()
	kerfPath := filepath.Join(root, "kerf")
	writeTestFile(t, kerfPath, "#!/bin/sh\nprintf '[{\"bead_id\":\"hk-eager-intent\"}]\\n'\n")
	if err := os.Chmod(kerfPath, 0o755); err != nil {
		t.Fatalf("chmod kerf fixture: %v", err)
	}

	deps := em063FixtureDeps(t, store)
	deps.env.ProjectDir = intentDurabilityBlockedProjectDir(t)
	deps.ports.Emitter = emitter
	deps.queueSurface = newQueueSurfacePort(nil, intentDurabilityLedger{})
	port := deps.reap(eagerRefillPort{kerfPath: kerfPath})

	eagerRefillEval(t.Context(), port)

	if got := emitter.count(); got != 0 {
		t.Fatalf("emit calls = %d, want 0 after eager-refill persist failure", got)
	}
}

func TestEagerRefillStaleQueueIdentityDoesNotMutateReplacement(t *testing.T) {
	emitter := &intentDurabilityEmitter{}
	store := queuewiring.NewQueueStore()
	original := em063FixtureStreamQueueWithBeads()
	store.SetQueue(original)

	root := t.TempDir()
	readyPath := filepath.Join(root, "ready")
	releasePath := filepath.Join(root, "release")
	kerfPath := filepath.Join(root, "kerf")
	script := "#!/bin/sh\n: > '" + readyPath + "'\nwhile [ ! -e '" + releasePath + "' ]; do sleep 0.01; done\nprintf '[{\"bead_id\":\"hk-stale-intent\"}]\\n'\n"
	writeTestFile(t, kerfPath, script)
	if err := os.Chmod(kerfPath, 0o755); err != nil {
		t.Fatalf("chmod kerf fixture: %v", err)
	}

	deps := em063FixtureDeps(t, store)
	deps.ports.Emitter = emitter
	deps.queueSurface = newQueueSurfacePort(nil, intentDurabilityLedger{})
	port := deps.reap(eagerRefillPort{kerfPath: kerfPath})
	done := make(chan struct{})
	go func() {
		defer close(done)
		eagerRefillEval(t.Context(), port)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("kerf fixture did not reach replacement window")
		}
		time.Sleep(10 * time.Millisecond)
	}
	replacement := em063FixtureStreamQueueWithBeads("hk-replacement")
	store.SetQueue(replacement)
	if err := os.WriteFile(releasePath, nil, 0o600); err != nil {
		t.Fatalf("release kerf fixture: %v", err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("eager refill did not return after queue replacement")
	}

	got := store.Queue()
	if got == nil || got.QueueID != replacement.QueueID {
		t.Fatalf("active queue = %+v, want replacement %s", got, replacement.QueueID)
	}
	if len(got.Groups[0].Items) != 1 || got.Groups[0].Items[0].BeadID != "hk-replacement" {
		t.Fatalf("replacement items = %+v, want only hk-replacement", got.Groups[0].Items)
	}
	if gotCalls := emitter.count(); gotCalls != 0 {
		t.Fatalf("emit calls = %d, want 0 for stale queue identity", gotCalls)
	}
}
