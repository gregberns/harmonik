//go:build scenario

package daemon_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

type rsb12Ledger struct {
	mu sync.Mutex

	pending []core.BeadID

	closed       map[core.BeadID]int
	reopened     map[core.BeadID]int
	reopenReason map[core.BeadID]string

	doneCh   chan struct{}
	doneOnce sync.Once
	onClose  func()
	onReopen func()
}

func newRSB12Ledger(beads []core.BeadID) *rsb12Ledger {
	pending := make([]core.BeadID, len(beads))
	copy(pending, beads)
	return &rsb12Ledger{
		pending:      pending,
		closed:       make(map[core.BeadID]int),
		reopened:     make(map[core.BeadID]int),
		reopenReason: make(map[core.BeadID]string),
		doneCh:       make(chan struct{}),
	}
}

func (l *rsb12Ledger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.pending) == 0 {
		return nil, nil
	}
	id := l.pending[0]
	l.pending = l.pending[1:]
	return []core.BeadRecord{{
		BeadID:        id,
		Title:         "remote-substrate e2e bead",
		BeadType:      "task",
		Status:        core.CoarseStatusOpen,
		AuditTrailRef: "rsb12-audit",
	}}, nil
}

func (l *rsb12Ledger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{
		BeadID:        id,
		Title:         "remote-substrate e2e bead",
		BeadType:      "task",
		Status:        core.CoarseStatusOpen,
		AuditTrailRef: "rsb12-audit",
	}, nil
}

func (l *rsb12Ledger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	return nil
}

func (l *rsb12Ledger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, _ bool) error {
	l.mu.Lock()
	l.closed[beadID]++
	l.signalDoneLocked()
	onClose := l.onClose
	l.mu.Unlock()
	if onClose != nil {
		onClose()
	}
	return nil
}

func (l *rsb12Ledger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, reason string) error {
	l.mu.Lock()
	l.reopened[beadID]++
	l.reopenReason[beadID] = reason
	l.signalDoneLocked()
	onReopen := l.onReopen
	l.mu.Unlock()
	if onReopen != nil {
		onReopen()
	}
	return nil
}

func (l *rsb12Ledger) signalDoneLocked() {
	l.doneOnce.Do(func() { close(l.doneCh) })
}

func (l *rsb12Ledger) closedCount(id core.BeadID) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closed[id]
}

func (l *rsb12Ledger) reopenedCount(id core.BeadID) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reopened[id]
}

func (l *rsb12Ledger) reopenReasonOf(id core.BeadID) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reopenReason[id]
}

func rsb12Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rsb12Git: git %v (dir=%s): %v\n%s", args, dir, err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

func rsb12GitConfig(t *testing.T, dir string) {
	t.Helper()
	rsb12Git(t, dir, "config", "user.email", "daemon@harmonik.local")
	rsb12Git(t, dir, "config", "user.name", "Harmonik Test")
}

func rsb12SSHHost() string {
	if h := os.Getenv("HARMONIK_E2E_SSH_HOST"); h != "" {
		return h
	}
	return "localhost"
}

func rsb12SharedRoot() string {
	return os.Getenv("HARMONIK_E2E_SHARED_ROOT")
}

func rsb12OriginWorkerDirs(t *testing.T) (originDir, workerDir string) {
	t.Helper()
	if root := rsb12SharedRoot(); root != "" {
		originDir = filepath.Join(root, "origin.git")
		workerDir = filepath.Join(root, "worker")
		_ = os.RemoveAll(originDir)
		_ = os.RemoveAll(workerDir)
		//nolint:gosec // G301: 0755 fixture dir on the shared volume
		if err := os.MkdirAll(originDir, 0o755); err != nil {
			t.Fatalf("mkdir shared originDir %s: %v", originDir, err)
		}
		return originDir, workerDir
	}
	originDir = t.TempDir()
	workerDir = t.TempDir()
	return originDir, workerDir
}

func rsb12SSHAvailable(ctx context.Context, host string) (bool, string) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
		host, "true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, strings.TrimSpace(string(out)) + " (" + err.Error() + ")"
	}
	return true, ""
}

func rsb12RequireSSHOrSkip(t *testing.T) {
	t.Helper()
	host := rsb12SSHHost()
	ok, detail := rsb12SSHAvailable(t.Context(), host)
	if ok {
		return
	}
	msg := "remote-substrate e2e requires a working `ssh " + host + " true`; probe: " + detail
	if os.Getenv("HARMONIK_REQUIRE_REMOTE_E2E") == "1" {
		t.Fatalf("%s (HARMONIK_REQUIRE_REMOTE_E2E=1)", msg)
	}
	t.Skipf("%s", msg)
}

func rsb12RunStartedWorkerName(t *testing.T, col *stubEventCollector) (string, bool) {
	t.Helper()
	for _, e := range col.allEvents() {
		if e.EventType != string(core.EventTypeRunStarted) {
			continue
		}
		var pl struct {
			WorkerName string `json:"worker_name"`
		}
		if err := json.Unmarshal(e.Payload, &pl); err != nil {
			t.Fatalf("rsb12: decode run_started payload: %v\nraw: %s", err, e.Payload)
		}
		return pl.WorkerName, true
	}
	return "", false
}

// TestScenario_RemoteSubstrate_NoWorker_RunStartedWorkerNameEmpty is the NEGATIVE
// GUARD for the worker_name assertion in the e2e above. With NO worker registered
// the WorkerRegistry is empty, beadRunOne's SelectWorker() returns nil, rbc==nil,
// and the run takes the LOCAL substrate path — so run_started.worker_name MUST be
// empty (NOT "localhost"). This locks the positive assertion as load-bearing: if
// worker_name were ever emitted unconditionally, this test fails.
//
// Unlike the e2e, this runs entirely on box A's local git (the production local
// worktree factory) and needs NO ssh, so it executes even on boxes where
// `ssh localhost` is unavailable and the e2e above skips.
//
// Bead: hk-mcf1z (guarding hk-rs-b12-e2e-localhost).
func TestScenario_RemoteSubstrate_NoWorker_RunStartedWorkerNameEmpty(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	const bead = core.BeadID("hk-rs-noworker-local")

	originDir := t.TempDir()
	rsb12Git(t, originDir, "init", "--bare", "--initial-branch=main")

	projectDir := t.TempDir()
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("mkdir beads-intents: %v", err)
	}
	//nolint:gosec // G301
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("mkdir events: %v", err)
	}
	rsb12Git(t, projectDir, "init", "--initial-branch=main")
	rsb12GitConfig(t, projectDir)
	//nolint:gosec // G306: test fixture file
	if err := os.WriteFile(filepath.Join(projectDir, "README"), []byte("initial\n"), 0o644); err != nil {
		t.Fatalf("WriteFile README: %v", err)
	}
	rsb12Git(t, projectDir, "add", "README")
	rsb12Git(t, projectDir, "commit", "-m", "init")
	rsb12Git(t, projectDir, "remote", "add", "origin", originDir)
	rsb12Git(t, projectDir, "push", "origin", "main")

	worktreeFactory := func(ctx context.Context, _, runID, headSHA string) (string, func(), error) {
		wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}
		relPath := "local-work.txt"
		//nolint:gosec // G306: test fixture file in a throwaway worktree
		if err := os.WriteFile(filepath.Join(wtPath, relPath), []byte("work from the local substrate\n"), 0o644); err != nil {
			cleanup()
			return "", nil, err
		}
		for _, args := range [][]string{
			{"-C", wtPath, "add", relPath},
			{"-C", wtPath, "commit", "-m", "feat: local-substrate work\n\nRefs: " + string(bead), "--trailer", "Harmonik-Run-ID: " + runID},
		} {
			cmd := exec.CommandContext(ctx, "git", args...)
			if out, err := cmd.CombinedOutput(); err != nil {
				cleanup()
				return "", nil, &rsb12CommitError{argv: args, out: string(out), err: err}
			}
		}
		return wtPath, cleanup, nil
	}

	collector := &stubEventCollector{}
	ledger := newRSB12Ledger([]core.BeadID{bead})

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		MaxConcurrent:    1,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:  worktreeFactory,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	select {
	case <-ledger.doneCh:
		cancel()
	case <-ctx.Done():
		t.Fatalf("timed out waiting for bead %s to reach a terminal state; events=%v", bead, collector.eventTypes())
	}

	awaitLoopTeardown(t, loopDone, "localhost work loop")

	gotWorker, ok := rsb12RunStartedWorkerName(t, collector)
	if !ok {
		t.Fatalf("no run_started event captured; events=%v", collector.eventTypes())
	}
	if gotWorker != "" {
		t.Errorf("run_started.worker_name = %q, want empty — a no-worker run must take the LOCAL substrate, not report a worker (hk-mcf1z)", gotWorker)
	}
	t.Logf("remote-substrate no-worker guard OK: local run emitted run_started.worker_name=%q (empty as required)", gotWorker)
}

type rsb12CommitError struct {
	argv []string
	out  string
	err  error
}

func (e *rsb12CommitError) Error() string {
	return "rsb12: git " + strings.Join(e.argv, " ") + ": " + e.err.Error() + "\n" + e.out
}
