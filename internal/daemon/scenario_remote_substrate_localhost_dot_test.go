//go:build scenario

package daemon_test

// scenario_remote_substrate_localhost_dot_test.go — end-to-end scenario coverage
// for the remote-substrate Phase 1 feature driven through the PRODUCTION DOT
// workflow mode (--workflow-mode dot) over a real SSH transport to localhost.
//
// # What this proves
//
// This is THE remote-substrate lifecycle proof: fetch-base → worktree-add on the
// worker → commit → push-branch → box-A-fetch → merge, all over a real ssh
// transport. A "single-mode" twin used to sit beside it in
// scenario_remote_substrate_localhost_test.go; that file's header records why it
// is gone. Production runs in DOT workflow mode, and the DOT path
// (driveDotWorkflow / dispatchDotAgenticNode, dot_cascade.go) owns the
// HEAD-resolution + spawn sites the remote flow has to get right. On a real
// worker a DOT run failed with:
//
//	dot: resolve HEAD before agentic node "implement" at iteration 0:
//	daemon: gitprobe.ResolveWorktreeHEAD: git rev-parse HEAD in
//	"<worker>/.harmonik/worktrees/<run_id>": chdir … no such file
//
// i.e. the DOT workflow ran gitprobe.ResolveWorktreeHEAD LOCALLY on box A against the
// WORKER's worktree path, instead of via the run's SSHRunner. This test drives
// the whole ssh-localhost remote lifecycle in DOT workflow mode, so every
// DOT-specific worktree probe is exercised over ssh.
//
// # Topology (all under t.TempDir())
//
//	origin.git (bare)
//	  ├── boxA   (projectDir; the daemon's repo; pushes main here)
//	  └── worker (the SSH worker's clone; RepoPath in the worker registry)
//
// The MINIMAL DOT graph used here (start → implement → close) avoids a reviewer
// node and a commit_gate shell node: those would require a live claude that the
// /bin/sh stub handler cannot drive. The stub implementer handler commits DURING
// the node, so the implementer node sees HEAD advance past preHeadSHA and returns
// SUCCESS, the cascade follows the unconditional edge to the `close` success
// terminal, and the daemon merges the worker's commit to box A main.
//
// What this exercises in the DOT path:
//   - driveDotWorkflow's resolve-HEAD-before-agentic-node (dot_cascade.go ~357)
//   - dispatchDotAgenticNode's preHeadSHA / postHeadSHA / implementer-phase HEAD
//     reads (dot_cascade.go ~821 / ~1020 / ~1092)
//   - the agentic-node spawn (newPerRunSubstrate runner threading)
//
// Harness lineage: reuses the rsb12* helpers (ledger, git fixture, ssh-available
// probe, run_started worker_name decode) from
// scenario_remote_substrate_localhost_test.go verbatim.
//
// Bead: hk-rs-b12-e2e-localhost.

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workers"
	"github.com/gregberns/harmonik/internal/workspace"
)

// rsb12DotMinimalGraph is a minimal valid DOT workflow: start (non-agentic noop)
// → implement (agentic implementer; commit required) → close (success terminal).
// The eager-commit worktree factory makes the implementer's HEAD advance, so the
// implementer node returns SUCCESS and the cascade reaches `close`. No reviewer
// node (would need a verdict the stub cannot write) and no commit_gate shell node
// (would need `go`/test on the worker) appear, keeping the run hermetic while
// still traversing every DOT-mode worktree probe.
const rsb12DotMinimalGraph = `digraph "remote-substrate-dot-e2e" {
    schema_version="1";
    version="1.0";
    workflow_id="remote-substrate-dot-e2e";
    start_node="start";
    terminal_node_ids="close,close-needs-attention";

    start [
        type="non-agentic",
        handler_ref="noop",
        idempotency_class="idempotent",
        role="entry"
    ];

    implement [
        type="agentic",
        agent_type="implementer",
        handler_ref="claude-implementer",
        idempotency_class="non-idempotent",
        role="produce the change; commit required"
    ];

    close [
        type="non-agentic",
        handler_ref="noop",
        idempotency_class="idempotent",
        role="success terminal"
    ];

    "close-needs-attention" [
        type="non-agentic",
        handler_ref="noop",
        idempotency_class="idempotent",
        role="failure terminal"
    ];

    start -> implement;

    implement -> close [
        condition="outcome.status == 'SUCCESS'"
    ];

    implement -> "close-needs-attention";
}
`

// TestScenario_RemoteSubstrate_Localhost_DOT_E2E drives ONE bead through the
// entire remote-substrate Phase 1 lifecycle in PRODUCTION DOT workflow mode
// against a worker reachable over `ssh localhost`, and asserts the commit made in
// the worker's clone lands on box A's main via the unchanged one-at-a-time merge.
//
// The work loop runs in DOT mode (WorkflowModeDefault=dot + a seeded workflow.dot
// holding rsb12DotMinimalGraph), which is what production runs.
//
// Bead: hk-rs-b12-e2e-localhost.
func TestScenario_RemoteSubstrate_Localhost_DOT_E2E(t *testing.T) {
	rsb12RunRemoteDot(t, false)
}

// TestScenario_RemoteSubstrate_Localhost_DOT_ShutdownDrain cancels a real
// worker run after its commit and proves the drain sync releases that commit.
func TestScenario_RemoteSubstrate_Localhost_DOT_ShutdownDrain(t *testing.T) {
	rsb12RunRemoteDot(t, true)
}

func rsb12RunRemoteDot(t *testing.T, shutdownDrain bool) {
	t.Helper()
	skipRealDaemonE2EInShort(t)

	// ── Pre-flight guard: ssh localhost must work (no sshd / no key → skip). ──
	rsb12RequireSSHOrSkip(t)

	bead := core.BeadID("hk-rs-b12-e2e-localhost-dot")
	if shutdownDrain {
		bead = core.BeadID("hk-rs-b12-drain-localhost-dot")
	}
	sshHost := rsb12SSHHost()
	sshRunner := tmux.SSHRunner{Host: sshHost}

	// ── origin (bare) + worker-clone paths. Under the docker drive both live on a
	//    volume mounted at the SAME absolute path in both containers, so the
	//    worker's `git fetch origin <baseSHA>` and box A's `git fetch ssh://worker
	//    <workerDir>` resolve the identical repos across the network. See
	//    rsb12OriginWorkerDirs and test/docker/README.md. ───────────────────────
	originDir, workerDir := rsb12OriginWorkerDirs(t)
	rsb12Git(t, originDir, "init", "--bare", "--initial-branch=main")

	// ── box A (projectDir): the daemon's repo (STAYS daemon-local, not shared). ─
	projectDir := rsb12ShortTempDir(t)
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

	// Seed the minimal DOT workflow as <projectDir>/workflow.dot so the DOT-mode
	// branch (workloop.go ~2329) resolves it instead of the embedded standard-bead
	// graph (which would require a reviewer + commit_gate the stub cannot drive).
	//nolint:gosec // G306: test fixture file
	if err := os.WriteFile(filepath.Join(projectDir, "workflow.dot"), []byte(rsb12DotMinimalGraph), 0o644); err != nil {
		t.Fatalf("WriteFile workflow.dot: %v", err)
	}

	// ── worker clone: the SSH worker's repo (registry RepoPath). ─────────────
	rsb12Git(t, ".", "clone", originDir, workerDir)
	rsb12GitConfig(t, workerDir)

	// ── worker registry: one ssh worker (host from HARMONIK_E2E_SSH_HOST). ────
	cfg := workers.Config{
		Version: 1,
		Workers: []workers.Worker{{
			Name:      sshHost,
			Transport: "ssh",
			Host:      sshHost,
			OS:        runtime.GOOS, // localhost worker == this host; hardcoded "linux" was wrong on darwin (latent trap, c063/c066/c068)
			RepoPath:  workerDir,
			MaxSlots:  1,
			Enabled:   true,
		}},
	}
	reg := workers.NewRegistry(cfg)

	// ── SSHRunner-backed worktree factory (mirrors production remote factory). ─
	// PRODUCTION-FAITHFUL: create the run-branch worktree on the WORKER via ssh and
	// return it WITHOUT committing. The graph captures preHeadSHA AFTER the worktree
	// is created and BEFORE the implementer launches, then requires postHeadSHA to
	// advance — so the commit MUST happen DURING the node, which is what a real
	// claude implementer does. Here the stub handler (below) makes that commit. A
	// factory that commits eagerly is already in the baseline and trips the
	// no-advance guard in dot_cascade_core.go, which is what retired the
	// eager-commit twin of this test.
	worktreeFactory := func(ctx context.Context, _, runID, headSHA string) (string, func(), error) {
		wtCfg := workspace.NoWorktreeRootOverride().WithRunner(sshRunner)
		if err := workspace.CreateWorktree(ctx, workerDir, runID, headSHA, wtCfg); err != nil {
			return "", nil, err
		}
		wtPath := workspace.WorktreePath(workerDir, runID, workspace.NoWorktreeRootOverride())

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

	// ── Stub implementer handler that COMMITS in the worktree during the node. ─
	// The handler is spawned with cmd.Dir = the worker worktree path (handler.go
	// `cmd.Dir = spec.WorkDir`), so the bare `git add`/`git commit` run inside the
	// worker's run/<id> worktree — advancing HEAD past preHeadSHA exactly as a real
	// implementer's commit does. The `Refs:` trailer makes the commit the bead's
	// landed work; the Harmonik-Run-ID trailer keys the daemon's commit-detect.
	marker := ""
	if shutdownDrain {
		marker = filepath.Join(workerDir, "dot-drain-committed")
	}
	handlerScript := rsb12DotImplementerHandlerScript(t, bead)
	if shutdownDrain {
		handlerScript = rsb12DotCommitThenWaitHandlerScript(t, bead, marker)
	}

	trace := &dotShutdownDrainTrace{}
	collector := &stubEventCollector{onEmit: func(eventType core.EventType) {
		if !shutdownDrain {
			return
		}
		switch eventType {
		case core.EventTypeBeadClosed, core.EventTypeRunCompleted, core.EventTypeRunFailed, core.EventTypeOutcomeEmitted:
			trace.record(string(eventType))
		}
	}}
	ledger := newRSB12Ledger([]core.BeadID{bead})
	if shutdownDrain {
		ledger.onClose = func() { trace.record("close") }
		ledger.onReopen = func() { trace.record("reopen") }
	}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{handlerScript},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		MaxConcurrent:       1,
		WorkflowModeDefault: core.WorkflowModeDot, // PRODUCTION DOT path
		AdapterRegistry2:    NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:     worktreeFactory,
		WorkerRegistry:      reg, // activates the DD1 remote code-sync path (B8/B11)
	})

	// 300s ceiling: a safety net, not a budget (several real ssh round-trips).
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Second)
	defer cancel()
	checkpointBeforeDrain := make(chan bool, 1)
	if shutdownDrain {
		go rsb12CancelAfterCommitMarker(ctx, cancel, marker, filepath.Join(projectDir, "remote-work.txt"), checkpointBeforeDrain)
	}

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	if shutdownDrain {
		// Cancellation is the test input. The drain still owes its terminal
		// release result after ctx.Done(), so wait on the ledger independently.
		select {
		case <-ledger.doneCh:
		case <-time.After(30 * time.Second):
			t.Fatalf("timed out waiting for remote shutdown-drain bead %s; events=%v", bead, collector.eventTypes())
		}
	} else {
		select {
		case <-ledger.doneCh:
			cancel()
		case <-ctx.Done():
			t.Fatalf("timed out waiting for DOT-mode bead %s to reach a terminal state; events=%v", bead, collector.eventTypes())
		}
	}

	select {
	case <-loopDone:
	case <-time.After(15 * time.Second):
		t.Error("work loop did not exit within 15s of cancel")
	}

	// ── Diagnostics. ──────────────────────────────────────────────────────────
	closed := ledger.closedCount(bead)
	reopened := ledger.reopenedCount(bead)
	t.Logf("remote-substrate DOT e2e: bead %s closed=%d reopened=%d reopenReason=%q events=%v",
		bead, closed, reopened, ledger.reopenReasonOf(bead), collector.eventTypes())

	// The cascade must have walked the graph — node_dispatch_requested is emitted
	// ONLY by driveDotWorkflow, so it is the positive evidence that the graph
	// driver ran rather than the run reaching a terminal state some other way.
	if !rsb12Contains(collector.eventTypes(), string(core.EventTypeNodeDispatchRequested)) {
		t.Errorf("DOT-mode remote e2e: node_dispatch_requested not emitted — the cascade "+
			"driver did not walk the graph (DOT path not exercised); events=%v", collector.eventTypes())
	}

	if reopened > 0 {
		t.Fatalf("remote DOT bead %s reopened (%d) — a remote-substrate DOT sync/merge/HEAD step failed: %q",
			bead, reopened, ledger.reopenReasonOf(bead))
	}
	if closed != 1 {
		t.Fatalf("remote DOT bead %s closed %d times; want 1 (the full ssh-localhost DOT lifecycle must land + close it)", bead, closed)
	}

	// ── Assert the worker's commit landed on box A's main. ────────────────────
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
	// The emitted run_started event must carry worker_name == the single registered
	// worker. A silent route-to-LOCAL regression (SelectWorker returns nil → rbc==nil
	// → local path) would land the same commit on main yet leave worker_name empty:
	// every merge assertion above would still pass and the routing regression would
	// slip through. This makes the routing observable and fails it loud (hk-mcf1z).
	// TestScenario_RemoteSubstrate_NoWorker_RunStartedWorkerNameEmpty is the negative
	// guard that proves this assertion is load-bearing.
	gotWorker, ok := rsb12RunStartedWorkerName(t, collector)
	if !ok {
		t.Fatalf("no run_started event captured; events=%v", collector.eventTypes())
	}
	if gotWorker != sshHost {
		t.Errorf("run_started.worker_name = %q, want %q — the run was NOT routed to the ssh worker (silent route-to-LOCAL regression)", gotWorker, sshHost)
	}

	if shutdownDrain {
		select {
		case unchanged := <-checkpointBeforeDrain:
			if !unchanged {
				t.Error("local target already contained remote work at the worker commit checkpoint")
			}
		default:
			t.Error("shutdown drain did not observe the worker commit checkpoint")
		}
		workerTip := rsb12Git(t, workerDir, "log", "--all", "--format=%H", "--grep=Refs: "+string(bead), "-1")
		if workerTip == "" {
			t.Fatal("worker commit was not retained for the shutdown-drain sync check")
		}
		rsb12Git(t, projectDir, "merge-base", "--is-ancestor", workerTip, "main")
		if got, want := trace.snapshot(), []string{"close", string(core.EventTypeBeadClosed), string(core.EventTypeRunCompleted)}; !reflect.DeepEqual(got, want) {
			t.Errorf("remote shutdown-drain terminal order = %v, want %v", got, want)
		}
	}

	t.Logf("remote-substrate DOT e2e OK: worker commit synced over ssh localhost and landed on box A main (%s); run_started.worker_name=%q", boxAMainSHA, gotWorker)
}

func rsb12CancelAfterCommitMarker(ctx context.Context, cancel context.CancelFunc, marker, localWorkPath string, checkpoint chan<- bool) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(marker); err == nil {
			_, localErr := os.Stat(localWorkPath)
			checkpoint <- os.IsNotExist(localErr)
			cancel()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func rsb12ShortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hk-rs-")
	if err != nil {
		t.Fatalf("MkdirTemp for remote socket path: %v", err)
	}
	t.Cleanup(func() {
		if removeErr := os.RemoveAll(dir); removeErr != nil {
			t.Errorf("remove remote scenario directory %s: %v", dir, removeErr)
		}
	})
	return dir
}

// rsb12Contains reports whether haystack contains needle.
func rsb12Contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle || strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// rsb12DotImplementerHandlerScript writes a /bin/sh handler script that commits a
// sentinel file in its working directory (the worker's run/<id> worktree, set via
// cmd.Dir = spec.WorkDir) so HEAD advances during the DOT implementer node. The
// commit carries the bead's `Refs:` trailer (for the box-A-main assertion and the
// noCommit/subsume guards) and the `Harmonik-Run-ID:` trailer (read from the
// daemon-supplied HARMONIK_RUN_ID env var) for commit-detect keying.
func rsb12DotImplementerHandlerScript(t *testing.T, bead core.BeadID) string {
	return rsb12DotHandlerScript(t, bead, "exit 0\n")
}

func rsb12DotCommitThenWaitHandlerScript(t *testing.T, bead core.BeadID, marker string) string {
	return rsb12DotHandlerScript(t, bead, "touch "+marker+"\nwhile :; do sleep 1; done\n")
}

func rsb12DotHandlerScript(t *testing.T, bead core.BeadID, afterCommit string) string {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "dot-impl-handler.sh")
	script := "#!/bin/sh\n" +
		"set -e\n" +
		"echo \"work from the remote worker (dot) $$\" > remote-work.txt\n" +
		"git add remote-work.txt\n" +
		"git commit -m \"feat: remote-substrate dot e2e work\n\nRefs: " + string(bead) +
		"\" --trailer \"Harmonik-Run-ID: ${HARMONIK_RUN_ID}\" >/dev/null 2>&1\n" +
		afterCommit
	//nolint:gosec // G306: 0755 required to exec the handler script in a test tree.
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("rsb12DotImplementerHandlerScript: write %s: %v", scriptPath, err)
	}
	return scriptPath
}
