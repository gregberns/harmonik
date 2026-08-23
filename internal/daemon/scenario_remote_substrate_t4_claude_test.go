//go:build scenario

package daemon_test

import (
	"context"
	"encoding/json"
	"os"
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
)

const (
	t4WorkerEnv        = "HARMONIK_T4_WORKER"
	t4WorkerRepoEnv    = "HARMONIK_T4_WORKER_REPO" // optional: pre-provisioned worker repo path
	t4DefaultWorker    = "gb-mbp"
	t4WorkFileName     = "remote-work.txt"
	t4WorkFileContents = "work from the remote worker\n"
)

func t4SSHRunnerFor(host string) tmux.SSHRunner {
	return tmux.SSHRunner{
		Host: host,
		Opts: []string{
			"-o", "BatchMode=yes",
			"-o", "StrictHostKeyChecking=accept-new",
			"-o", "ConnectTimeout=15",
		},
	}
}

func t4RemoteAvailable(ctx context.Context, host string) (bool, string) {
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	runner := t4SSHRunnerFor(host)
	for _, probe := range []struct {
		what string
		argv []string
	}{
		{"ssh reachable", []string{"true"}},
		{"claude present", []string{"claude", "--version"}},
		{"tmux present", []string{"tmux", "-V"}},
	} {
		cmd := runner.Command(cctx, probe.argv[0], probe.argv[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return false, probe.what + " failed: " + strings.TrimSpace(string(out)) + " (" + err.Error() + ")"
		}
	}
	return true, ""
}

type t4Ledger struct {
	mu sync.Mutex

	pending []core.BeadID

	closed       map[core.BeadID]int
	reopened     map[core.BeadID]int
	reopenReason map[core.BeadID]string

	doneCh   chan struct{}
	doneOnce sync.Once
}

func newT4Ledger(bead core.BeadID) *t4Ledger {
	return &t4Ledger{
		pending:      []core.BeadID{bead},
		closed:       make(map[core.BeadID]int),
		reopened:     make(map[core.BeadID]int),
		reopenReason: make(map[core.BeadID]string),
		doneCh:       make(chan struct{}),
	}
}

func t4TaskPrompt(bead core.BeadID) string {
	return "Create a file named " + t4WorkFileName + " in the repository root " +
		"containing exactly the text:\n\n" + t4WorkFileContents + "\n" +
		"Then stage it and make a single git commit whose message includes the " +
		"line `Refs: " + string(bead) + "`. Do nothing else — no other files, no " +
		"further commits."
}

func (l *t4Ledger) record(id core.BeadID) core.BeadRecord {
	return core.BeadRecord{
		BeadID:        id,
		Title:         "T4 remote-substrate Claude-slice proof",
		Description:   t4TaskPrompt(id),
		BeadType:      "task",
		Status:        core.CoarseStatusOpen,
		AuditTrailRef: "t4-audit",
	}
}

func (l *t4Ledger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.pending) == 0 {
		return nil, nil
	}
	id := l.pending[0]
	l.pending = l.pending[1:]
	return []core.BeadRecord{l.record(id)}, nil
}

func (l *t4Ledger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return l.record(id), nil
}

func (l *t4Ledger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	return nil
}

func (l *t4Ledger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, _ bool) error {
	l.mu.Lock()
	l.closed[beadID]++
	l.signalDoneLocked()
	l.mu.Unlock()
	return nil
}

func (l *t4Ledger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, reason string) error {
	l.mu.Lock()
	l.reopened[beadID]++
	l.reopenReason[beadID] = reason
	l.signalDoneLocked()
	l.mu.Unlock()
	return nil
}

func (l *t4Ledger) signalDoneLocked() { l.doneOnce.Do(func() { close(l.doneCh) }) }

func (l *t4Ledger) closedCount(id core.BeadID) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closed[id]
}

func (l *t4Ledger) reopenedCount(id core.BeadID) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reopened[id]
}

func (l *t4Ledger) reopenReasonOf(id core.BeadID) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reopenReason[id]
}

func t4AgentReadyProvenance(t *testing.T, col *stubEventCollector) (string, bool) {
	t.Helper()
	for _, e := range col.allEvents() {
		if e.EventType != string(core.EventTypeAgentReady) {
			continue
		}
		var pl core.AgentReadyPayload
		if err := json.Unmarshal(e.Payload, &pl); err != nil {
			t.Fatalf("t4: decode agent_ready payload: %v\nraw: %s", err, e.Payload)
		}
		return pl.Provenance, true
	}
	return "", false
}

func t4SawAgentInputAcked(col *stubEventCollector) bool {
	for _, e := range col.allEvents() {
		if e.EventType == "agent_input_acked" {
			return true
		}
	}
	return false
}

// TestScenario_RemoteSubstrate_ClaudeSlice_RemoteWorker_E2E is the T4 (M4-C8)
// proof. It arms only when HARMONIK_T4_WORKER names a reachable worker that has
// `claude` (subscription-authenticated) and `tmux`. It then drives ONE bead
// through the production remote substrate so a REAL Claude process spawns on the
// worker over the `ssh -- tmux/git` path, and asserts:
//
//	(1) agent_ready arrived over the reverse tunnel (real spawn — not a stub);
//	(2) agent_input_acked arrived over the reverse tunnel (input truly acked);
//	(3) the run was routed to the worker (run_started.worker_name == host);
//	(4) the bead closed and the worker's commit landed on box A's main.
//
// With no reachable worker it SKIPS (and reports why); it always compiles.
func TestScenario_RemoteSubstrate_ClaudeSlice_RemoteWorker_E2E(t *testing.T) {
	skipRealDaemonE2EInShort(t)

	worker := strings.TrimSpace(os.Getenv(t4WorkerEnv))
	if worker == "" {
		t.Skipf("T4 Claude-slice remote proof is armed-only: set %s=<host> (e.g. %s=%s) to run "+
			"against a reachable worker with subscription-authed `claude` + `tmux`; skipping.",
			t4WorkerEnv, t4WorkerEnv, t4DefaultWorker)
	}
	if ok, detail := t4RemoteAvailable(t.Context(), worker); !ok {
		t.Skipf("T4 Claude-slice remote proof requires worker %q reachable with claude+tmux; skipping. probe: %s",
			worker, detail)
	}

	const bead = core.BeadID("hk-t4-m4c8-claude-remote")

	originDir := t.TempDir()
	rsb12Git(t, originDir, "init", "--bare", "--initial-branch=main")

	projectDir, err := os.MkdirTemp("/tmp", "t4-")
	if err != nil {
		t.Fatalf("MkdirTemp /tmp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(projectDir) })
	projectDir = cc14EvalSymlinks(t, projectDir)
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

	workerDir := strings.TrimSpace(os.Getenv(t4WorkerRepoEnv))
	if workerDir == "" {
		workerDir = t.TempDir()
		rsb12Git(t, ".", "clone", originDir, workerDir)
		rsb12GitConfig(t, workerDir)
	}

	cfg := workers.Config{
		Version: 1,
		Workers: []workers.Worker{{
			Name:      worker,
			Transport: "ssh",
			Host:      worker,
			OS:        "darwin",
			RepoPath:  workerDir,
			MaxSlots:  1,
			Enabled:   true,
		}},
	}
	reg := workers.NewRegistry(cfg)

	substrate := daemon.NewTmuxSubstrate(tmux.OSAdapter{}, "t4-remote-substrate")

	collector := &stubEventCollector{}
	ledger := newT4Ledger(bead)

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:  ledger,
		Bus:        collector,
		ProjectDir: projectDir,
		// REAL claude — not a stub. Args left to the production launch spec.
		HandlerBinary: "claude",
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
		MaxConcurrent: 1,
		Substrate:     substrate,
		// REAL ClaudeCode adapter so waitAgentReady genuinely gates on the
		// tunnel-delivered agent_ready (NOT the empty registry that bypasses it).
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		// WorktreeFactory nil ⇒ production remote worktree factory: real
		// `git worktree add` on the worker via the dispatch-time SSHRunner.
		WorkerRegistry: reg,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 600*time.Second)
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

	awaitLoopTeardown(t, loopDone, "remote-substrate work loop")

	closed := ledger.closedCount(bead)
	reopened := ledger.reopenedCount(bead)
	t.Logf("T4 Claude-slice: bead %s closed=%d reopened=%d reopenReason=%q events=%v",
		bead, closed, reopened, ledger.reopenReasonOf(bead), collector.eventTypes())

	prov, sawReady := t4AgentReadyProvenance(t, collector)
	if !sawReady {
		t.Fatalf("no agent_ready event — a REAL Claude never became ready over the reverse tunnel; "+
			"events=%v reopenReason=%q", collector.eventTypes(), ledger.reopenReasonOf(bead))
	}
	t.Logf("T4 Claude-slice: agent_ready observed (provenance=%q) over the reverse tunnel", prov)

	if !t4SawAgentInputAcked(collector) {
		t.Fatalf("no agent_input_acked event — the paste-inject SubmitInput was never acknowledged "+
			"over the reverse tunnel; events=%v", collector.eventTypes())
	}
	t.Logf("T4 Claude-slice: agent_input_acked observed over the reverse tunnel")

	gotWorker, ok := rsb12RunStartedWorkerName(t, collector)
	if !ok {
		t.Fatalf("no run_started event captured; events=%v", collector.eventTypes())
	}
	if gotWorker != worker {
		t.Errorf("run_started.worker_name=%q, want %q — the run was NOT routed to the ssh worker", gotWorker, worker)
	}

	if reopened > 0 {
		t.Fatalf("bead %s reopened (%d) — the remote Claude run did not complete cleanly: %q",
			bead, reopened, ledger.reopenReasonOf(bead))
	}
	if closed != 1 {
		t.Fatalf("bead %s closed %d times; want 1 (the full remote Claude lifecycle must land + close it)", bead, closed)
	}

	rsb12Git(t, projectDir, "checkout", "main")
	workPath := filepath.Join(projectDir, t4WorkFileName)
	if _, err := os.Stat(workPath); err != nil {
		t.Errorf("worker's %s missing on box A main: %v — the remote commit did not land", t4WorkFileName, err)
	}
	mainLog := rsb12Git(t, projectDir, "log", "-1", "--format=%B", "main")
	if !strings.Contains(mainLog, "Refs: "+string(bead)) {
		t.Errorf("box A main tip does not carry %q; got:\n%s", "Refs: "+string(bead), mainLog)
	}

	originMainSHA := rsb12Git(t, originDir, "rev-parse", "main")
	boxAMainSHA := rsb12Git(t, projectDir, "rev-parse", "main")
	if originMainSHA != boxAMainSHA {
		t.Errorf("origin/main (%s) != box A main (%s) — the merge push did not reach origin", originMainSHA, boxAMainSHA)
	}

	t.Logf("T4 Claude-slice OK: REAL Claude ran on worker %q, agent_ready+agent_input_acked flowed over the "+
		"reverse tunnel, and the commit landed on box A main (%s)", worker, boxAMainSHA)
}
