package scenariotest

// concurrent_merge.go — RunConcurrentMerge, a parameterized N-bead concurrent
// dispatch+merge scenario fixture (validation-net VN1, bead hk-944c2).
//
// # Why this exists
//
// A concurrency-only bug (hk-37giq: the per-run event-tap competing-consumer
// starve) hid for ~2 weeks because no scenario test exercised concurrent REAL-
// bead dispatch through the real heartbeat/launch/watchdog path. The narrow unit
// test that shipped with the fix (workloopeventsource_hk37giq_test.go) proves the
// tap fans out, but does NOT prove the end-to-end property: that N beads
// dispatched concurrently all reach merge+close without a launch wedge.
//
// RunConcurrentMerge promotes the determinism recipe from the N=2 multi-queue
// scenario (scenario_concurrent_multiqueue_hkumemp_test.go) into a reusable
// helper parameterized on N and the twin scenario. It boots the FULL daemon
// composition root (via an injected boot func — see the import-boundary note
// below), dispatches N distinct beads from a single wave queue at
// MaxConcurrent=N, and asserts:
//
//   - all N beads reach a terminal run event (run_completed/run_failed) within
//     budget;
//   - the concurrent-runs counter never exceeds the MaxConcurrent cap;
//   - (when ExpectAllComplete) all N reach run_completed + merge + close, with no
//     terminal run_stale / launch_stall_detected wedge;
//   - run lifecycle causality holds (run_started precedes a terminal event).
//
// # Import-boundary note (why boot is injected)
//
// The daemon's test-only seams — daemon.StartForTesting, daemon.WithWorktree
// Factory, daemon.WithMergeMutex, daemon.ExportedProductionWorktreeFactory and
// the emptyCommitWorktreeFactory wrapper — live in *_test.go files in package
// daemon. Go test-only symbols are visible ONLY within that package's own test
// binary, so this package (scenariotest) cannot reference them directly. The
// caller therefore supplies a Boot closure that has already bound
// StartForTesting with the determinism options (WithWorktreeFactory(empty
// CommitWorktreeFactory) + WithMergeMutex). RunConcurrentMerge owns everything
// else: project/git/br setup, queue persistence, the phase-aware twin wrapper,
// the daemon.Config assembly (with the Skip* flags and short AgentReadyTimeout),
// the wait loop, and the assertions.
//
// # The determinism recipe (encapsulated here, from hkumemp)
//
//   - emptyCommitWorktreeFactory (caller-bound, pre-commits an --allow-empty
//     commit) satisfies the no-commit guard (hk-mmh8f) WITHOUT the twin running
//     git, and avoids the concurrent-merge `git status` race (hk-bnm89).
//   - WithMergeMutex (caller-bound) serialises rebase→update-ref→push so
//     concurrent merges to the shared bare-repo origin do not race.
//   - A phase-aware twin wrapper: implementer phase runs the supplied scenario;
//     reviewer phase writes an APPROVE review.json so the review loop terminates
//     successfully (hk-4f5ua) instead of "verdict absent".
//   - Skip flags: SkipWALCheckpoint, SkipRestartBackoff, SkipBrHistoryRotation,
//     plus a short AgentReadyTimeout so the test runs in seconds.
//
// Helper prefix: rcm (run-concurrent-merge). Per implementer-protocol.md
// §Helper-prefix discipline.
//
// Bead: hk-944c2. Refs: hk-37giq, hk-umemp, hk-bnm89, hk-4f5ua.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/queue"
)

// ConcurrentMergeConfig parameterizes a RunConcurrentMerge invocation.
type ConcurrentMergeConfig struct {
	// N is the number of distinct beads to dispatch concurrently. MaxConcurrent
	// is set to N so all N may run at once (true concurrent dispatch). Must be
	// >= 1; the regression guard (hk-ukhzu) requires N >= 3.
	N int

	// TwinScenario is the harmonik-twin-claude --scenario name the implementer
	// phase runs (e.g. "single-happy-path", "heartbeat-then-hold"). For a
	// scenario that does not commit on its own, the caller's worktree factory
	// (emptyCommitWorktreeFactory) supplies the HEAD-advancing commit.
	TwinScenario string

	// Boot boots the daemon for testing. The caller binds daemon.StartForTesting
	// with the determinism options already applied:
	//
	//	func(ctx context.Context, cfg daemon.Config) <-chan error {
	//	    done := make(chan error, 1)
	//	    go func() {
	//	        done <- daemon.StartForTesting(ctx, cfg,
	//	            daemon.WithWorktreeFactory(emptyCommitWorktreeFactory),
	//	            daemon.WithMergeMutex(&mergeMu),
	//	        )
	//	    }()
	//	    return done
	//	}
	//
	// RunConcurrentMerge calls Boot once with the assembled Config and the
	// daemon's lifecycle context, then drives the wait loop and cancels via the
	// context it owns.
	Boot func(ctx context.Context, cfg daemon.Config) <-chan error

	// Substrate is an OPTIONAL tmux substrate to wire into daemon.Config.
	// Nil (the default) uses the exec/stdout-watcher path. Pass a fake-adapter-
	// backed *tmuxSubstrate (daemon.NewTmuxSubstrate(fakeAdapter, name)) to
	// engage the perRunSubstrate path so pasteInjectQuitOnCommit launches as a
	// SECOND per-run-tap consumer — required to exercise the hk-37giq competing-
	// consumer race (the exec path has only one tap consumer, waitAgentReady,
	// and structurally cannot reproduce the wedge). See the VN4 test docstring
	// for the altitude caveat on driving this to completion deterministically.
	Substrate handler.Substrate

	// TerminalBudget is how long to wait for all N terminal run events. When
	// zero a default is computed from N (per-run AgentReadyTimeout + merge
	// overhead + headroom).
	TerminalBudget time.Duration

	// AgentReadyTimeout overrides the per-run agent_ready wait. When zero a
	// short default (5s) is used so the fixture runs fast.
	AgentReadyTimeout time.Duration

	// ExpectAllComplete, when true, asserts the STRONG terminal outcome: all N
	// beads reach run_completed (not run_failed), all N beads close in br, and
	// no terminal run_stale / launch_stall_detected event appears. This is the
	// post-fix assertion for the regression guard. When false the fixture only
	// asserts that all N reach SOME terminal event and the cap is honored (used
	// by the reverted-fix demonstration, which expects failure).
	ExpectAllComplete bool

	// BeadPrefix is the br workspace prefix (and bead-id namespace). Defaults to
	// "rcm". Must be a short lowercase token accepted by `br init --prefix`.
	BeadPrefix string
}

// ConcurrentMergeResult reports the observed outcome for caller-side assertions
// beyond the ones RunConcurrentMerge performs internally.
type ConcurrentMergeResult struct {
	// BeadIDs are the N created bead IDs, in creation order.
	BeadIDs []string
	// MaxConcurrent is the peak number of concurrently in-flight runs observed.
	MaxConcurrent int
	// Completed / Failed / Stale are terminal-event counts.
	Completed int
	Failed    int
	Stale     int
	// LaunchStall is the count of launch_stall_detected events.
	LaunchStall int
	// ClosedBeads is the number of beads that reached "closed" in br.
	ClosedBeads int
	// JSONLPath is the events log path (for caller-side custom assertions).
	JSONLPath string
	// ProjectDir is the test project dir.
	ProjectDir string
}

// RunConcurrentMerge boots a twin-backed daemon at MaxConcurrent=N, dispatches N
// distinct beads from a single wave queue, waits for all N terminal events, and
// asserts the concurrency-merge properties (see ConcurrentMergeConfig). It
// returns the observed result for further caller-side assertions.
//
// The helper is NOT t.Parallel-safe with respect to itself if multiple
// invocations share the HARMONIK_CLAUDE_CONFIG_PATH env (it sets and unsets it
// via t.Cleanup); run invocations serially within a test.
func RunConcurrentMerge(t *testing.T, cfg ConcurrentMergeConfig) ConcurrentMergeResult {
	t.Helper()

	if cfg.N < 1 {
		t.Fatalf("RunConcurrentMerge: N must be >= 1, got %d", cfg.N)
	}
	if cfg.Boot == nil {
		t.Fatal("RunConcurrentMerge: Boot func is required (binds daemon.StartForTesting)")
	}
	if cfg.TwinScenario == "" {
		t.Fatal("RunConcurrentMerge: TwinScenario is required")
	}
	prefix := cfg.BeadPrefix
	if prefix == "" {
		prefix = "rcm"
	}
	agentReadyTimeout := cfg.AgentReadyTimeout
	if agentReadyTimeout == 0 {
		agentReadyTimeout = 5 * time.Second
	}
	terminalBudget := cfg.TerminalBudget
	if terminalBudget == 0 {
		// Per-run ready timeout × N + merge/review overhead + headroom.
		terminalBudget = time.Duration(cfg.N)*agentReadyTimeout + 60*time.Second
	}

	// Locate the twin binary; skip when absent (matches hkumemp / N1).
	twinPath, ok := TwinBinaryPath()
	if !ok {
		t.Skip("RunConcurrentMerge: harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}
	realBrPath := rcmBrPath(t)

	// Project dir + git (with bare origin) + br DB.
	projectDir, jsonlPath := rcmProjectDir(t)
	rcmGitRepo(t, projectDir)
	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := rcmBrWrapperScript(t, realBrPath, dbPath)
	beadIDs := rcmInitBrWithBeads(t, realBrPath, projectDir, brWrapper, prefix, cfg.N)
	t.Logf("RunConcurrentMerge: N=%d beads=%v scenario=%q", cfg.N, beadIDs, cfg.TwinScenario)

	// Single wave queue holding all N beads → dispatched concurrently up to
	// MaxConcurrent=N (queue-model.md §wave semantics: a wave dispatches its
	// whole set concurrently up to the daemon cap).
	beads := make([]core.BeadID, len(beadIDs))
	for i, id := range beadIDs {
		beads[i] = core.BeadID(id)
	}
	q := rcmBuildActiveWaveQueue("main", "00000000-0000-7c00-8000-cc1000000001", cfg.N, beads...)
	if err := queue.Persist(t.Context(), projectDir, q); err != nil {
		t.Fatalf("RunConcurrentMerge: persist wave queue: %v", err)
	}

	// Phase-aware twin wrapper: implementer phase runs the scenario; reviewer
	// phase writes an APPROVE review.json so the review loop terminates.
	twinWrapper := rcmTwinWrapperScript(t, twinPath, cfg.TwinScenario)

	// Redirect EnsureWorktreeTrust to a test-local claude config so this test
	// does not contend with a running harmonik daemon on ~/.claude.json.lock.
	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath); err != nil {
		t.Fatalf("RunConcurrentMerge: Setenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Unsetenv("HARMONIK_CLAUDE_CONFIG_PATH"); err != nil {
			t.Logf("RunConcurrentMerge: Unsetenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
		}
	})

	daemonCfg := daemon.Config{
		ProjectDir:            projectDir,
		JSONLLogPath:          jsonlPath,
		BrPath:                brWrapper,
		HandlerBinary:         twinWrapper,
		NoAutoPull:            true,  // queue-only: no br-ready fallback
		MaxConcurrent:         cfg.N, // global ceiling = N → true concurrent dispatch
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		SkipRestartBackoff:    true,
		AgentReadyTimeout:     agentReadyTimeout,
		LogWriter:             rcmLogWriter{t: t},
		WorkflowModeDefault:   core.WorkflowModeReviewLoop,
	}
	if cfg.Substrate != nil {
		daemonCfg.Substrate = cfg.Substrate
	}

	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

	startDone := cfg.Boot(loopCtx, daemonCfg)

	// Wait for all N terminal run events (run_completed OR run_failed).
	MustCompleteWithin(t, jsonlPath, "", nil, terminalBudget, func() {
		for {
			nDone := rcmEventCount(t, jsonlPath, string(core.EventTypeRunCompleted)) +
				rcmEventCount(t, jsonlPath, string(core.EventTypeRunFailed))
			if nDone >= cfg.N {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	})

	// Cancel the daemon; all runs are terminal (or budget elapsed).
	loopCancel()
	MustCompleteWithin(t, jsonlPath, "", nil, 15*time.Second, func() {
		if err := <-startDone; err != nil {
			t.Errorf("RunConcurrentMerge: daemon returned error after cancel: %v", err)
		}
	})

	res := ConcurrentMergeResult{
		BeadIDs:       beadIDs,
		MaxConcurrent: rcmMaxConcurrentRuns(t, jsonlPath),
		Completed:     rcmEventCount(t, jsonlPath, string(core.EventTypeRunCompleted)),
		Failed:        rcmEventCount(t, jsonlPath, string(core.EventTypeRunFailed)),
		Stale:         rcmEventCount(t, jsonlPath, string(core.EventTypeRunStale)),
		LaunchStall:   rcmEventCount(t, jsonlPath, string(core.EventTypeLaunchStallDetected)),
		JSONLPath:     jsonlPath,
		ProjectDir:    projectDir,
	}
	for _, id := range beadIDs {
		if rcmPollBeadClosed(t, brWrapper, id, 2*time.Second) {
			res.ClosedBeads++
		}
	}

	// ── Assertion: cap honored (always) ───────────────────────────────────────
	if res.MaxConcurrent > cfg.N {
		t.Errorf("RunConcurrentMerge: max concurrent runs = %d, want <= cap %d", res.MaxConcurrent, cfg.N)
	}
	t.Logf("RunConcurrentMerge: maxConcurrent=%d completed=%d failed=%d stale=%d launchStall=%d closed=%d/%d",
		res.MaxConcurrent, res.Completed, res.Failed, res.Stale, res.LaunchStall, res.ClosedBeads, cfg.N)

	// ── Assertion: no implementer_escaped_worktree (sibling-exclusion, hk-77q8e)
	if nEscape := rcmEventCount(t, jsonlPath, string(core.EventTypeImplementerEscapedWorktree)); nEscape > 0 {
		t.Errorf("RunConcurrentMerge: implementer_escaped_worktree emitted %d time(s); want 0", nEscape)
	}

	if cfg.ExpectAllComplete {
		// ── STRONG terminal outcome (post-fix regression guard) ────────────────
		if res.Completed < cfg.N {
			t.Errorf("RunConcurrentMerge: %d/%d runs reached run_completed; want all N "+
				"(a shortfall is the hk-37giq concurrent-dispatch wedge signature)", res.Completed, cfg.N)
		}
		if res.Stale > 0 {
			t.Errorf("RunConcurrentMerge: %d terminal run_stale event(s); want 0 "+
				"(run_stale is the launch-wedge terminal signature)", res.Stale)
		}
		if res.LaunchStall > 0 {
			t.Errorf("RunConcurrentMerge: %d launch_stall_detected event(s); want 0 "+
				"(launch_stall_detected is the per-run-tap starve signature)", res.LaunchStall)
		}
		if res.ClosedBeads < cfg.N {
			t.Errorf("RunConcurrentMerge: %d/%d beads closed in br; want all N", res.ClosedBeads, cfg.N)
		}
		// Event-ordered lifecycle: every run_started is eventually followed by a
		// terminal event (run_completed/run_failed/run_cancelled).
		AssertEventCausality(t, jsonlPath,
			"run_started",
			[]string{"run_completed", "run_failed", "run_cancelled"},
			terminalBudget,
		)
	}

	return res
}

// ─────────────────────────────────────────────────────────────────────────────
// rcm fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// rcmLogWriter adapts *testing.T to io.Writer for daemon log capture.
type rcmLogWriter struct{ t *testing.T }

func (w rcmLogWriter) Write(p []byte) (int, error) {
	w.t.Log("daemon:", strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// rcmProjectDir creates the minimal project directory (.harmonik/{events,
// beads-intents,queues}) and returns it plus the JSONL events log path.
func rcmProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	raw := t.TempDir()
	resolved, err := filepath.EvalSymlinks(raw)
	if err != nil {
		t.Fatalf("rcmProjectDir: EvalSymlinks %q: %v", raw, err)
	}
	projectDir = resolved
	for _, sub := range []string{
		filepath.Join(".harmonik", "events"),
		filepath.Join(".harmonik", "beads-intents"),
		filepath.Join(".harmonik", "queues"),
	} {
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if err := os.MkdirAll(filepath.Join(projectDir, sub), 0o755); err != nil {
			t.Fatalf("rcmProjectDir: mkdir %s: %v", sub, err)
		}
	}
	jsonlPath = filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	return projectDir, jsonlPath
}

// rcmGitRepo initialises a git repo with one commit and a bare-repo origin so
// mergeRunBranchToMain's push step succeeds.
func rcmGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("rcmGitRepo: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik RCM Test")
	readmePath := filepath.Join(dir, "README")
	//nolint:gosec // G306: test-only README
	if err := os.WriteFile(readmePath, []byte("rcm scenario test\n"), 0o644); err != nil {
		t.Fatalf("rcmGitRepo: write README: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")

	raw := t.TempDir()
	originDir, err := filepath.EvalSymlinks(raw)
	if err != nil {
		t.Fatalf("rcmGitRepo: EvalSymlinks originDir: %v", err)
	}
	initBare := exec.CommandContext(t.Context(), "git", "init", "--bare", "--initial-branch=main", originDir)
	if out, bErr := initBare.CombinedOutput(); bErr != nil {
		t.Fatalf("rcmGitRepo: git init --bare: %v\n%s", bErr, out)
	}
	run("remote", "add", "origin", originDir)
	run("push", "origin", "main")
}

// rcmBrPath returns the real `br` binary path, skipping the test when absent.
func rcmBrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("rcm: br required for scenario test (not on PATH)")
	}
	return brPath
}

// rcmBrWrapperScript writes a /bin/sh wrapper invoking realBrPath with
// --db <dbPath> prepended to all args.
func rcmBrWrapperScript(t *testing.T, realBrPath, dbPath string) string {
	t.Helper()
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("rcmBrWrapperScript: EvalSymlinks: %v", err)
	}
	path := filepath.Join(resolved, "br")
	content := "#!/bin/sh\nexec " + realBrPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("rcmBrWrapperScript: WriteFile: %v", err)
	}
	return path
}

// rcmInitBrWithBeads initialises a beads workspace and creates n distinct open
// beads, returning their IDs in creation order.
func rcmInitBrWithBeads(t *testing.T, realBrPath, projectDir, brWrapper, prefix string, n int) []string {
	t.Helper()
	//nolint:gosec // G204: br args are test-internal literals; not user input
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", prefix)
	initCmd.Dir = projectDir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("rcmInitBrWithBeads: br init: %v\n%s", err, out)
	}
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		title := fmt.Sprintf("%s concurrent-merge bead %d", prefix, i)
		//nolint:gosec // G204: br args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), brWrapper, "create", title, "--status", "open", "--silent")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("rcmInitBrWithBeads: br create %q: %v\n%s", title, err, out)
		}
		id := strings.TrimSpace(string(out))
		if id == "" {
			t.Fatalf("rcmInitBrWithBeads: br create %q returned empty ID", title)
		}
		ids = append(ids, id)
	}
	return ids
}

// rcmBuildActiveWaveQueue builds an active wave queue (group 0) holding beadIDs
// as pending items, with the given worker count.
func rcmBuildActiveWaveQueue(name, queueID string, workers int, beadIDs ...core.BeadID) *queue.Queue {
	items := make([]queue.Item, len(beadIDs))
	for i, id := range beadIDs {
		items[i] = queue.Item{BeadID: id, Status: queue.ItemStatusPending}
	}
	now := time.Now().UTC()
	started := now
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       queueID,
		Name:          name,
		Workers:       workers,
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items:      items,
				CreatedAt:  now,
				StartedAt:  &started,
			},
		},
	}
}

// rcmTwinWrapperScript writes a /bin/sh wrapper that is phase-aware: the reviewer
// phase (detected by .harmonik/review-target.md in the worktree) writes an
// APPROVE review.json so the review loop terminates; the implementer phase runs
// the supplied twin scenario with --worktree-path "$PWD". PATH is re-exported so
// the twin's internal git (used by committing scenarios) resolves.
func rcmTwinWrapperScript(t *testing.T, twinPath, scenario string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "twin-rcm-wrapper.sh")
	content := `#!/bin/sh
set -e
export PATH=` + os.Getenv("PATH") + `
if [ -f "$PWD/.harmonik/review-target.md" ]; then
  mkdir -p "$PWD/.harmonik"
  printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"rcm review-loop happy path"}' > "$PWD/.harmonik/review.json"
  exit 0
fi
exec "` + twinPath + `" --scenario ` + scenario + ` --worktree-path "$PWD"
`
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("rcmTwinWrapperScript: WriteFile: %v", err)
	}
	return path
}

// rcmMaxConcurrentRuns returns the peak number of concurrently in-flight runs,
// tracked by run_started (+1) and run_completed/run_failed (-1) in event order.
func rcmMaxConcurrentRuns(t *testing.T, jsonlPath string) int {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("rcmMaxConcurrentRuns: open %s: %v", jsonlPath, err)
	}
	defer func() {
		if cErr := f.Close(); cErr != nil {
			t.Logf("rcmMaxConcurrentRuns: close: %v", cErr)
		}
	}()
	var current, maxSeen int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var env struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(line), &env) != nil {
			continue
		}
		switch env.Type {
		case string(core.EventTypeRunStarted):
			current++
			if current > maxSeen {
				maxSeen = current
			}
		case string(core.EventTypeRunCompleted), string(core.EventTypeRunFailed):
			if current > 0 {
				current--
			}
		}
	}
	return maxSeen
}

// rcmEventCount returns the number of JSONL events matching eventType.
func rcmEventCount(t *testing.T, jsonlPath, eventType string) int {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("rcmEventCount: open %s: %v", jsonlPath, err)
	}
	defer func() {
		if cErr := f.Close(); cErr != nil {
			t.Logf("rcmEventCount: close: %v", cErr)
		}
	}()
	var count int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var env struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(line), &env) != nil {
			continue
		}
		if env.Type == eventType {
			count++
		}
	}
	return count
}

// rcmPollBeadClosed polls `br show <id>` every 10ms for up to budget, returning
// true when the bead reaches "closed".
func rcmPollBeadClosed(t *testing.T, brWrapper, beadID string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		cmd := exec.CommandContext(t.Context(), brWrapper, "show", beadID, "--format", "json")
		out, err := cmd.Output()
		if err == nil {
			var items []struct {
				Status string `json:"status"`
			}
			if json.Unmarshal(out, &items) == nil && len(items) > 0 && items[0].Status == "closed" {
				return true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
