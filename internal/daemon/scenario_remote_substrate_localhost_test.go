//go:build scenario

package daemon_test

// scenario_remote_substrate_localhost_test.go — end-to-end scenario coverage for
// the remote-substrate Phase 1 feature (beads B1–B11), driven over a real SSH
// transport to localhost (bead hk-rs-b12-e2e-localhost).
//
// The per-bead unit tests prove the remote-substrate halves in isolation:
//   - codesync_rs_b8_test.go  — argv ORDER of fetch-base / worktree-add /
//     push-branch / box-A-fetch, but with every git call mocked (no real git).
//   - remote_substrate_b11_test.go — IsSSHConnectionFailure / liveness-probe
//     classification, with a RecordingRunner driving exit codes.
//   - workers/*_test.go — registry slot-tracking, health probes, offline events.
//
// What NONE of them exercise (and what hk-rs-b12 demands): the WHOLE remote
// lifecycle stitched together over a REAL ssh transport, proving a commit made
// in a worker's repo clone actually lands on box A's main. This test:
//
//   1. registers ONE worker {Name:"localhost", Transport:"ssh", Host:"localhost",
//      OS:"darwin", RepoPath:<worker clone>, MaxSlots:1, Enabled:true} via the
//      workers.Registry, so beadRunOne's SelectWorker() returns it and the DD1
//      code-sync path activates (workloop.go ~2055);
//   2. drives ONE bead through the production work-loop (ExportedRunWorkLoop)
//      with SSHRunner{Host:"localhost"} as the worker transport:
//        (a) fetch-base on the worker        — real `ssh localhost -- git fetch`
//        (worktree) git worktree add on the worker — real `ssh localhost -- git`
//        (commit)  a stub agent writes a `Refs: <bead>` commit in the worker wt
//        (c) box-A direct-SSH fetch of run/<id> straight from the worker repo —
//            real local `git fetch ssh://localhost<workerDir> run/<id>:refs/...`
//            (NO worker→GitHub push; box A pulls the branch over SSH — hk-7bwx)
//        (merge)   the UNCHANGED one-at-a-time mergeRunBranchToMain;
//   3. asserts the bead reaches a terminal state and inspects box A's main.
//
// Harness lineage: mirrors scenario_multibead_mergeconflict_serial_hktijaj_test.go
// — same in-process ExportedWorkLoopDeps + ExportedRunWorkLoop driver, same
// real-throwaway-git-repos-under-t.TempDir pattern, same FIFO recording ledger,
// same `/bin/sh -c "exit 0"` stub handler (the worktree factory makes the commit,
// not a real claude subprocess), same skipRealDaemonE2EInShort + t.Parallel.
// The ONLY additions are: a second (worker) clone, a workers.Registry wired into
// WorkLoopDepsParams.WorkerRegistry, an SSHRunner-backed worktree factory, and
// an `ssh localhost true` pre-flight guard.
//
// Bead: hk-rs-b12-e2e-localhost. Refs (the merged feature): hk-rs-b6-healthcheck-isda,
// hk-rs-b8-codesync-3fk0, hk-rs-b9-liveness-1m9n, hk-rs-b11-offline-dh57.
//
// ── BOX-A REF GAP this E2E proves CLOSED (read before "fixing" a failure) ──
//
// This test asserts the worker's commit lands on box A's main. Two gaps had to
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
//      (`git fetch ssh://<host><repoPath> run/<id>:refs/heads/run/<id>`). In this
//      test that URL is ssh://localhost<workerDir>; the worker clone has the
//      branch locally because the worktree was created with `worktree add -b`.
//
// The unit tests cannot catch these (they mock every git call, argv-order only).
// This test keeps the strict "commit lands on box A main" assertion — do NOT
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
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workers"
	"github.com/gregberns/harmonik/internal/workspace"
)

// ─────────────────────────────────────────────────────────────────────────────
// Recording ledger (single bead; FIFO; per-bead close/reopen counts)
//
// Mirrors multiBeadLedger from the hktijaj scenario, trimmed to a single bead.
// ClaimBead records the runID→beadID map so the SSHRunner-backed worktree factory
// (which only receives runID) can stamp the right `Refs:` trailer.
// ─────────────────────────────────────────────────────────────────────────────

type rsb12Ledger struct {
	mu sync.Mutex

	pending []core.BeadID

	closed       map[core.BeadID]int
	reopened     map[core.BeadID]int
	reopenReason map[core.BeadID]string
	runToBead    map[string]core.BeadID

	doneCh   chan struct{}
	doneOnce sync.Once
}

func newRSB12Ledger(beads []core.BeadID) *rsb12Ledger {
	pending := make([]core.BeadID, len(beads))
	copy(pending, beads)
	return &rsb12Ledger{
		pending:      pending,
		closed:       make(map[core.BeadID]int),
		reopened:     make(map[core.BeadID]int),
		reopenReason: make(map[core.BeadID]string),
		runToBead:    make(map[string]core.BeadID),
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

func (l *rsb12Ledger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, runID core.RunID, _ core.TransitionID, beadID core.BeadID) error {
	l.mu.Lock()
	l.runToBead[runID.String()] = beadID
	l.mu.Unlock()
	return nil
}

func (l *rsb12Ledger) beadForRun(runID string) (core.BeadID, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.runToBead[runID]
	return b, ok
}

func (l *rsb12Ledger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, _ bool) error {
	l.mu.Lock()
	l.closed[beadID]++
	l.signalDoneLocked()
	l.mu.Unlock()
	return nil
}

func (l *rsb12Ledger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, reason string) error {
	l.mu.Lock()
	l.reopened[beadID]++
	l.reopenReason[beadID] = reason
	l.signalDoneLocked()
	l.mu.Unlock()
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

// rsb12SSHAvailable reports whether `ssh localhost true` succeeds within a short
// bound. Returns the combined output on failure so the skip message is actionable
// (no sshd, no key, host-key prompt, etc.).
func rsb12SSHAvailable(ctx context.Context) (bool, string) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// BatchMode=yes: never prompt for a passphrase/password — fail fast instead.
	cmd := exec.CommandContext(cctx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
		"localhost", "true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, strings.TrimSpace(string(out)) + " (" + err.Error() + ")"
	}
	return true, ""
}

// ─────────────────────────────────────────────────────────────────────────────
// Test — full remote lifecycle over ssh localhost
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_RemoteSubstrate_Localhost_E2E drives ONE bead through the entire
// remote-substrate Phase 1 lifecycle against a worker reachable over
// `ssh localhost`, and asserts the commit made in the worker's clone lands on
// box A's main via the unchanged one-at-a-time merge.
//
// Topology (all real, all under t.TempDir()):
//
//	origin.git (bare)
//	  ├── boxA   (projectDir; the daemon's repo; pushes main here)
//	  └── worker (the SSH worker's clone; RepoPath in the worker registry)
//
// The worker registry has exactly one worker {Host:"localhost", Transport:"ssh"};
// SelectWorker() therefore returns it for the single bead, activating the DD1
// code-sync path. The worktree factory reproduces the production REMOTE factory
// (workloop.go ~2123): `git worktree add` on the worker via SSHRunner, then the
// stub agent's `Refs:` commit in the worker worktree. The work loop itself runs
// the REAL fetch-base (step a), preMergeSync push+box-A-fetch (steps b,c), and
// mergeRunBranchToMain — all over SSHRunner{Host:"localhost"} / local git.
//
// Bead: hk-rs-b12-e2e-localhost.
func TestScenario_RemoteSubstrate_Localhost_E2E(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	// ── Pre-flight guard: ssh localhost must work (no sshd / no key → skip). ──
	if ok, detail := rsb12SSHAvailable(t.Context()); !ok {
		t.Skipf("remote-substrate e2e requires a working `ssh localhost true` "+
			"(passwordless sshd on this box); skipping. probe output: %s", detail)
	}

	const bead = core.BeadID("hk-rs-b12-e2e-localhost")
	sshRunner := tmux.SSHRunner{Host: "localhost"}

	// ── origin (bare) ────────────────────────────────────────────────────────
	originDir := t.TempDir()
	rsb12Git(t, originDir, "init", "--bare", "--initial-branch=main")

	// ── box A (projectDir): the daemon's repo. ───────────────────────────────
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
	// Push the base so the worker can fetch it (the work loop resolves the base
	// SHA from box A's main and step (a) fetches it onto the worker from origin).
	rsb12Git(t, projectDir, "push", "origin", "main")

	// ── worker clone: the SSH worker's repo (registry RepoPath). ─────────────
	// A real clone of origin so its default fetch refspec + `git push origin`
	// behave exactly as a production worker's clone does.
	workerDir := t.TempDir()
	// `git clone <origin> <workerDir>` — clone into the pre-created temp dir.
	rsb12Git(t, ".", "clone", originDir, workerDir)
	rsb12GitConfig(t, workerDir)

	// ── worker registry: one ssh/localhost worker. ───────────────────────────
	cfg := workers.Config{
		Version: 1,
		Workers: []workers.Worker{{
			Name:      "localhost",
			Transport: "ssh",
			Host:      "localhost",
			OS:        "darwin",
			RepoPath:  workerDir,
			MaxSlots:  1,
			Enabled:   true,
		}},
	}
	reg := workers.NewRegistry(cfg)

	// ── SSHRunner-backed worktree factory (mirrors production remote factory). ─
	// Creates the run-branch worktree on the WORKER via ssh, then commits a
	// `Refs: <bead>` file in that worktree (the stub agent's "work"). The commit
	// lands in the worker's repo — exactly the remote-placement the feature must
	// then synchronise back to box A.
	worktreeFactory := func(ctx context.Context, _, runID, headSHA string) (string, func(), error) {
		wtCfg := workspace.NoWorktreeRootOverride().WithRunner(sshRunner)
		if err := workspace.CreateWorktree(ctx, workerDir, runID, headSHA, wtCfg); err != nil {
			return "", nil, err
		}
		wtPath := workspace.WorktreePath(workerDir, runID, workspace.NoWorktreeRootOverride())

		// Commit the agent's work in the worker worktree with the run-id trailer
		// (the daemon's commit-detect keys off the Harmonik-Run-ID trailer).
		relPath := "remote-work.txt"
		//nolint:gosec // G306: test fixture file in a throwaway worktree
		if err := os.WriteFile(filepath.Join(wtPath, relPath), []byte("work from the remote worker\n"), 0o644); err != nil {
			return "", nil, err
		}
		for _, args := range [][]string{
			{"-C", wtPath, "add", relPath},
			{"-C", wtPath, "commit", "-m", "feat: remote-substrate e2e work\n\nRefs: " + string(bead), "--trailer", "Harmonik-Run-ID: " + runID},
		} {
			cmd := exec.CommandContext(ctx, "git", args...)
			if out, err := cmd.CombinedOutput(); err != nil {
				return "", nil, &rsb12CommitError{argv: args, out: string(out), err: err}
			}
		}

		cleanup := func() {
			cleanCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			rm := sshRunner.Command(cleanCtx, "git", "-C", workerDir, "worktree", "remove", "--force", "--force", wtPath)
			_ = rm.Run()
			prune := sshRunner.Command(cleanCtx, "git", "-C", workerDir, "worktree", "prune")
			_ = prune.Run()
		}
		return wtPath, cleanup, nil
	}

	collector := &stubEventCollector{}
	ledger := newRSB12Ledger([]core.BeadID{bead})

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		MaxConcurrent:    1,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:  worktreeFactory,
		WorkerRegistry:   reg, // activates the DD1 remote code-sync path (B8/B11)
	})

	// 300s ceiling: a safety net, not a budget. The lifecycle makes several real
	// ssh round-trips (fetch-base, worktree-add, push-branch) plus a local merge;
	// on a loaded box under `go test -race` these can be starved well past a tight
	// bound. The work completes long before 300s once it gets CPU + the network.
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Second)
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

	select {
	case <-loopDone:
	case <-time.After(15 * time.Second):
		t.Error("work loop did not exit within 15s of cancel")
	}

	// ── Diagnostics: the remote lifecycle either CLOSED the bead (commit landed)
	//    or REOPENED it (a sync/merge step failed). Surface the reason either way.
	closed := ledger.closedCount(bead)
	reopened := ledger.reopenedCount(bead)
	t.Logf("remote-substrate e2e: bead %s closed=%d reopened=%d reopenReason=%q events=%v",
		bead, closed, reopened, ledger.reopenReasonOf(bead), collector.eventTypes())

	if reopened > 0 {
		t.Fatalf("remote bead %s reopened (%d) — a remote-substrate sync/merge step failed: %q",
			bead, reopened, ledger.reopenReasonOf(bead))
	}
	if closed != 1 {
		t.Fatalf("remote bead %s closed %d times; want 1 (the full ssh-localhost lifecycle must land + close it)", bead, closed)
	}

	// ── Assert the worker's commit landed on box A's main. ────────────────────
	// box A's local main was fast-forwarded by mergeRunBranchToMain (update-ref +
	// push origin main). Check both the remote-work file content and that box A's
	// main now points at a commit carrying the Refs: trailer.
	rsb12Git(t, projectDir, "checkout", "main")
	workPath := filepath.Join(projectDir, "remote-work.txt")
	if _, err := os.Stat(workPath); err != nil {
		t.Errorf("worker's remote-work.txt missing on box A main: %v — the remote commit did not land", err)
	}
	mainLog := rsb12Git(t, projectDir, "log", "-1", "--format=%B", "main")
	if !strings.Contains(mainLog, "Refs: "+string(bead)) {
		t.Errorf("box A main tip commit message does not carry %q; got:\n%s", "Refs: "+string(bead), mainLog)
	}

	// ── Assert origin's main also advanced (the push origin main step). ───────
	originMainSHA := rsb12Git(t, originDir, "rev-parse", "main")
	boxAMainSHA := rsb12Git(t, projectDir, "rev-parse", "main")
	if originMainSHA != boxAMainSHA {
		t.Errorf("origin/main (%s) != box A main (%s) — the merge push did not reach origin",
			originMainSHA, boxAMainSHA)
	}

	// ── Assert the run was actually ROUTED to the worker (not silently run LOCAL).
	// The emitted run_started event must carry worker_name == "localhost" — the
	// single registered worker. A silent route-to-LOCAL regression (SelectWorker
	// returns nil → rbc==nil → local path, workloop.go ~2055) would land the same
	// commit on main yet leave worker_name empty: the merge assertions above would
	// still pass and the routing regression would slip through. This makes the
	// routing observable and fails it loud (hk-mcf1z). The NoWorker negative guard
	// below proves this assertion is load-bearing.
	gotWorker, ok := rsb12RunStartedWorkerName(t, collector)
	if !ok {
		t.Fatalf("no run_started event captured; events=%v", collector.eventTypes())
	}
	if gotWorker != "localhost" {
		t.Errorf("run_started.worker_name = %q, want %q — the run was NOT routed to the ssh worker (silent route-to-LOCAL regression)", gotWorker, "localhost")
	}

	t.Logf("remote-substrate e2e OK: worker commit synced over ssh localhost and landed on box A main (%s); run_started.worker_name=%q", boxAMainSHA, gotWorker)
}

// rsb12RunStartedWorkerName scans the recorded bus events for the run_started
// event and returns its worker_name field (false when none was captured). The
// emitted payload is workloopRunStartedPayload, which is package-internal to
// daemon; from this external _test package we decode only the load-bearing
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

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
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

	select {
	case <-loopDone:
	case <-time.After(15 * time.Second):
		t.Error("work loop did not exit within 15s of cancel")
	}

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
