package scenariotest

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

	// ClaudeConfigPath overrides the test-local ~/.claude.json the fixture points
	// HARMONIK_CLAUDE_CONFIG_PATH at. Zero value = the fixture mints its own temp
	// path (the normal case: the redirect exists only so the test does not contend
	// with a running daemon on the real config's lock).
	//
	// Set it when the TEST needs to observe or manipulate that file — e.g. the
	// hk-qx065 trust-clobber scenario runs an adversarial writer against it.
	ClaudeConfigPath string
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
// The helper is NOT t.Parallel-safe: it redirects HARMONIK_CLAUDE_CONFIG_PATH,
// which is process-wide, through t.Setenv. Run invocations serially within a
// test; calling it from a parallel test panics rather than leaking.
func RunConcurrentMerge(t *testing.T, cfg ConcurrentMergeConfig) ConcurrentMergeResult {
	t.Helper()

	cfg = cfg.checkedWithDefaults(t)

	twinPath, ok := TwinBinaryPath()
	if !ok {
		t.Skip("RunConcurrentMerge: harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}
	realBrPath := rcmBrPath(t)

	projectDir, jsonlPath := rcmProjectDir(t)
	// Registered before the run starts so a red assertion below prints the
	// reason the daemon gave, while its temp dir still exists.
	ReportRunFailures(t, jsonlPath)
	rcmGitRepo(t, projectDir)
	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := rcmBrWrapperScript(t, realBrPath, dbPath)
	beadIDs := rcmInitBrWithBeads(t, realBrPath, projectDir, brWrapper, cfg.BeadPrefix, cfg.N)
	t.Logf("RunConcurrentMerge: N=%d beads=%v scenario=%q", cfg.N, beadIDs, cfg.TwinScenario)

	beads := make([]core.BeadID, len(beadIDs))
	for i, id := range beadIDs {
		beads[i] = core.BeadID(id)
	}
	q := rcmBuildActiveWaveQueue("main", "00000000-0000-7c00-8000-cc1000000001", cfg.N, beads...)
	if err := queue.Persist(t.Context(), projectDir, q); err != nil {
		t.Fatalf("RunConcurrentMerge: persist wave queue: %v", err)
	}

	twinWrapper := rcmTwinWrapperScript(t, twinPath, cfg.TwinScenario)

	WriteReviewLoopWorkflowDot(t, projectDir)

	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", cfg.ClaudeConfigPath)

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
		AgentReadyTimeout:     cfg.AgentReadyTimeout,
		LogWriter:             rcmLogWriter{t: t},
		// dot, carrying the review gate this fixture has always run under.
		//
		// It ran under review-loop until that mode was retired (EM-015d). Two
		// wrong turns were taken getting here, both recorded so they are not
		// repeated: first it was moved to single on the false rationale that the
		// twin "never writes a reviewer verdict" (rcmTwinWrapperScript below is
		// phase-aware and DOES write an APPROVE review.json); then dot was tried
		// without changing the implementer scenario, and every run failed.
		//
		// The actual requirement is that the implementer twin scenario must land
		// a real per-node commit. dot checks HEAD advance per node, which
		// "single-happy-path" does not satisfy — it leans on the fixture's
		// pre-committed empty commit. "commit-on-cue-startup-delay" git-commits a
		// timestamped sentinel and does satisfy it, which is why the sibling
		// fixtures in scenario_queue_submit_dispatch_hksk00a_test.go have always
		// worked under dot with an otherwise identical reviewer branch. Callers
		// wanting the happy path pass that scenario.
		WorkflowModeDefault: core.WorkflowModeDot,
	}
	if cfg.Substrate != nil {
		daemonCfg.Substrate = cfg.Substrate
	}

	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

	startDone := cfg.Boot(loopCtx, daemonCfg)

	MustCompleteWithin(t, jsonlPath, "", nil, cfg.TerminalBudget, func() {
		for {
			nDone := rcmEventCount(t, jsonlPath, string(core.EventTypeRunCompleted)) +
				rcmEventCount(t, jsonlPath, string(core.EventTypeRunFailed))
			if nDone >= cfg.N {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	})

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

	if res.MaxConcurrent > cfg.N {
		t.Errorf("RunConcurrentMerge: max concurrent runs = %d, want <= cap %d", res.MaxConcurrent, cfg.N)
	}
	t.Logf("RunConcurrentMerge: maxConcurrent=%d completed=%d failed=%d stale=%d launchStall=%d closed=%d/%d",
		res.MaxConcurrent, res.Completed, res.Failed, res.Stale, res.LaunchStall, res.ClosedBeads, cfg.N)

	if cfg.ExpectAllComplete {
		assertAllRunsComplete(t, cfg, res, jsonlPath)
	}

	return res
}

// checkedWithDefaults refuses a config that cannot produce a meaningful run and
// fills the optional fields. Returning a completed copy keeps one name per value:
// the body reads cfg.BeadPrefix rather than carrying a second local that shadows
// the field it defaults.
func (cfg ConcurrentMergeConfig) checkedWithDefaults(t *testing.T) ConcurrentMergeConfig {
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
	if cfg.BeadPrefix == "" {
		cfg.BeadPrefix = "rcm"
	}
	if cfg.AgentReadyTimeout == 0 {
		cfg.AgentReadyTimeout = 5 * time.Second
	}
	if cfg.TerminalBudget == 0 {
		cfg.TerminalBudget = time.Duration(cfg.N)*cfg.AgentReadyTimeout + 60*time.Second
	}
	if cfg.ClaudeConfigPath == "" {
		cfg.ClaudeConfigPath = filepath.Join(t.TempDir(), ".claude.json")
	}
	return cfg
}

// assertAllRunsComplete holds the properties a caller asserts by setting
// ExpectAllComplete. It is separate so the shape of a run stays readable in
// RunConcurrentMerge and so the assertions can be read as one list.
func assertAllRunsComplete(t *testing.T, cfg ConcurrentMergeConfig, res ConcurrentMergeResult, jsonlPath string) {
	t.Helper()
	if res.Completed < cfg.N {
		t.Errorf("RunConcurrentMerge: %d/%d runs reached run_completed; want all N "+
			"(%d run(s) short — the cause is in the failure detail printed below, "+
			"not necessarily a dispatch wedge)", res.Completed, cfg.N, cfg.N-res.Completed)
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
	AssertEventCausality(t, jsonlPath,
		"run_started",
		[]string{"run_completed", "run_failed", "run_cancelled"},
		cfg.TerminalBudget,
	)
}

type rcmLogWriter struct{ t *testing.T }

func (w rcmLogWriter) Write(p []byte) (int, error) {
	w.t.Log("daemon:", strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

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
		if err := os.MkdirAll(filepath.Join(projectDir, sub), core.HarmonikDirMode); err != nil {
			t.Fatalf("rcmProjectDir: mkdir %s: %v", sub, err)
		}
	}
	jsonlPath = filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	return projectDir, jsonlPath
}

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

func rcmBrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("rcm: br required for scenario test (not on PATH)")
	}
	return brPath
}

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

func rcmInitBrWithBeads(t *testing.T, realBrPath, projectDir, brWrapper, prefix string, n int) []string {
	t.Helper()
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
