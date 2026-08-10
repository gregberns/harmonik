//go:build scenario

package daemon_test

// scenario_remote_substrate_localhost_test.go — the shared rsb12 fixture for the
// remote-substrate scenarios, plus the no-worker negative guard.
//
// The per-bead unit tests prove the remote-substrate halves in isolation:
//   - internal/transport/codesync/codesync_test.go — argv ORDER of fetch-base /
//     push-branch / box-A-fetch, but with every git call mocked (no real git).
//   - remote_substrate_b11_test.go — IsSSHConnectionFailure / liveness-probe
//     classification, with a RecordingRunner driving exit codes.
//   - workers/*_test.go — registry slot-tracking, health probes, offline events.
//
// None of them stitches the WHOLE remote lifecycle together over a real ssh
// transport. That positive proof lives in
// scenario_remote_substrate_localhost_dot_test.go
// (TestScenario_RemoteSubstrate_Localhost_DOT_E2E). This file holds the fixture
// helpers that test builds on — the ssh pre-flight probe, the git repo helpers,
// the recording ledger, the shared origin/worker directory resolver — and the
// negative guard that keeps its worker_name routing assertion load-bearing.
//
// ── Why the single-mode twin of the DOT e2e is GONE (read before you re-add it) ──
//
// This file used to carry TestScenario_RemoteSubstrate_Localhost_E2E, a
// "single-mode" copy of the DOT e2e. It made its commit inside the worktree
// factory rather than inside the agent. Both halves of that design are now dead:
//
//  1. There is no single-mode driver. Every run walks driveDotWorkflow.
//     WorkflowModeSingle only selects a different GRAPH (the registered
//     no-review-bead.dot), and resolveWorkflow in workloop_runplan.go selects it
//     for exactly two compatibility inputs — a queue item whose mode string is
//     "single", or a bead carrying the exact workflow:single label. A daemon
//     DEFAULT of single is explicitly refused there and falls through to dot, so
//     the retired test could not have reached single mode even if it had asked.
//
//  2. A commit made in the worktree factory is already in the node baseline. The
//     graph reads worktree HEAD after the factory returns and before the agent
//     launches, then requires HEAD to advance. An eager factory commit therefore
//     trips the no-advance guard in dot_cascade_core.go on purpose. The same
//     lesson already retired the eager factory in
//     scenario_multibead_mergeconflict_serial_hktijaj_test.go.
//
// The DOT e2e commits from the agent, which is what a real implementer does, and
// it now carries the run_started.worker_name assertion the retired test owned.
//
// Bead: hk-rs-b12-e2e-localhost. Refs (the merged feature): hk-rs-b6-healthcheck-isda,
// hk-rs-b8-codesync-3fk0, hk-rs-b9-liveness-1m9n, hk-rs-b11-offline-dh57.
//
// ── BOX-A REF GAP the remote e2e proves CLOSED (read before "fixing" a failure) ──
//
// The DOT e2e asserts the worker's commit lands on box A's main. Two gaps had to
// be closed for that to hold; both are now fixed in the feature:
//
//   1. (historical) fetchRunBranchBoxA must fetch into a LOCAL head
//      (`run/<id>:refs/heads/run/<id>`), not a refspec-less fetch — otherwise
//      mergeRunBranchToMain's `git rev-parse refs/heads/run/<id>` finds no ref and
//      silently no-changes. Closed: the fetch carries the explicit refspec.
//
//   2. (hk-7bwx) the branch must reach box A WITHOUT a worker→GitHub round-trip.
//      The worker has no valid GitHub push credential in production, so the old
//      `git push origin run/<id>` on the worker failed and box A's
//      `fetch origin run/<id>` died with `couldn't find remote ref`. Closed: box A
//      now fetches the branch DIRECTLY from the worker repo over SSH
//      (`git fetch ssh://<host><repoPath> run/<id>:refs/heads/run/<id>`). In that
//      test the URL is ssh://localhost<workerDir>; the worker clone has the
//      branch locally because the worktree was created with `worktree add -b`.
//
// The unit tests cannot catch these (they mock every git call, argv-order only).
// The DOT e2e keeps the strict "commit lands on box A main" assertion — do NOT
// relax it to noChange-tolerant.

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

// ─────────────────────────────────────────────────────────────────────────────
// Recording ledger (single bead; FIFO; per-bead close/reopen counts)
//
// Mirrors multiBeadLedger from the hktijaj scenario, trimmed to a single bead.
// ─────────────────────────────────────────────────────────────────────────────

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

// signalDoneLocked closes doneCh on the first terminal transition (single bead).
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

// ─────────────────────────────────────────────────────────────────────────────
// git fixture helpers (scenario-local, prefix rsb12)
// ─────────────────────────────────────────────────────────────────────────────

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

// rsb12SSHHost returns the ssh host the remote-substrate e2e drives against.
// Default "localhost" preserves the original single-box behaviour; the docker
// compose drive (WS2.3) sets HARMONIK_E2E_SSH_HOST=worker so the SAME test runs
// across two containers with the worker reachable by its compose service name.
func rsb12SSHHost() string {
	if h := os.Getenv("HARMONIK_E2E_SSH_HOST"); h != "" {
		return h
	}
	return "localhost"
}

// rsb12SharedRoot returns HARMONIK_E2E_SHARED_ROOT, the directory (mounted at an
// IDENTICAL absolute path in both the daemon and worker containers) under which
// origin.git + the worker clone are rooted so box A and the worker share those
// two repos across the network. Empty (the default) keeps the original
// single-box t.TempDir() behaviour where every path is box-A-local.
func rsb12SharedRoot() string {
	return os.Getenv("HARMONIK_E2E_SHARED_ROOT")
}

// rsb12OriginWorkerDirs returns the origin.git (bare) + worker-clone paths. When
// HARMONIK_E2E_SHARED_ROOT is set they are rooted under it at the shared,
// identical-in-both-containers absolute path (and any stale copy from a prior run
// on a persistent volume is removed first); otherwise origin uses t.TempDir() and
// the caller lets `git clone` create the worker dir under t.TempDir().
func rsb12OriginWorkerDirs(t *testing.T) (originDir, workerDir string) {
	t.Helper()
	if root := rsb12SharedRoot(); root != "" {
		originDir = filepath.Join(root, "origin.git")
		workerDir = filepath.Join(root, "worker")
		// Clean any stale repos left by a prior run (down -v normally wipes the
		// volume, but be defensive so a reused volume can't poison the fixture).
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

// rsb12SSHAvailable reports whether `ssh <host> true` succeeds within a short
// bound. Returns the combined output on failure so the skip message is actionable
// (no sshd, no key, host-key prompt, etc.).
func rsb12SSHAvailable(ctx context.Context, host string) (bool, string) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// BatchMode=yes: never prompt for a passphrase/password — fail fast instead.
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

// rsb12RequireSSHOrSkip gates the remote-substrate e2e on a working
// `ssh <host> true` (host = rsb12SSHHost()). By default (flag unset) it SKIPs
// green so the suite is clean on boxes without a passwordless sshd. With
// HARMONIK_REQUIRE_REMOTE_E2E=1 it FATALs instead — so CI (and the docker drive)
// that intends to exercise the remote path fails loudly rather than silently
// skipping.
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

// rsb12RunStartedWorkerName scans the recorded bus events for the run_started
// event and returns its worker_name field (false when none was captured). The
// emitted payload is core.RunStartedPayload. From this external _test package
// we decode only the load-bearing
// worker_name field. Mirrors the b10 unit test's worker_name decode, but off the
// real emitted bus event rather than a hand-built payload struct.
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

	// ── origin (bare) + box A (projectDir) — same fixture as the e2e, minus the
	//    worker clone and the worker registry. ────────────────────────────────
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

	// ── LOCAL worktree factory (production): worktree on box A, commit the agent's
	//    `Refs:` work in it. No worker registry is wired, so the work loop runs the
	//    LOCAL substrate and the unchanged local merge path. ──────────────────
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
		// WorkerRegistry intentionally omitted (nil) — SelectWorker() returns nil →
		// rbc==nil → LOCAL substrate path; run_started.worker_name must be empty.
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

	// ── The run took the LOCAL path: run_started.worker_name MUST be empty. ────
	gotWorker, ok := rsb12RunStartedWorkerName(t, collector)
	if !ok {
		t.Fatalf("no run_started event captured; events=%v", collector.eventTypes())
	}
	if gotWorker != "" {
		t.Errorf("run_started.worker_name = %q, want empty — a no-worker run must take the LOCAL substrate, not report a worker (hk-mcf1z)", gotWorker)
	}
	t.Logf("remote-substrate no-worker guard OK: local run emitted run_started.worker_name=%q (empty as required)", gotWorker)
}

// rsb12CommitError carries argv + git output for a worktree-factory commit failure
// so the work loop's reopen reason is actionable.
type rsb12CommitError struct {
	argv []string
	out  string
	err  error
}

func (e *rsb12CommitError) Error() string {
	return "rsb12: git " + strings.Join(e.argv, " ") + ": " + e.err.Error() + "\n" + e.out
}
