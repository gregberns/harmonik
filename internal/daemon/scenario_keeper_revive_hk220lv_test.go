//go:build scenario

package daemon_test

// scenario_keeper_revive_hk220lv_test.go — §3 scenario coverage for the
// keeper-revive watcher (hk-220lv), the fix for "a crew keeper watcher can die
// silently and nothing notices for 43 hours".
//
// This is the build-practices scenario test owed by a bug bead filed from an
// observed dogfooding runtime failure (build-practices.md §"scenario test for
// every bug fix"). See ## Test plan at the bottom of this file for why it landed
// in one commit rather than red-first.
//
// WHAT IS REAL HERE. A real daemon boots via daemon.Start against a real project
// directory. It parses a real .harmonik/config.yaml (so the revive_* keys travel
// the production parse path, not a struct literal), reads a real crew registry
// record, reads a real .managed marker through keeper.IsManaged, probes the real
// advisory flock through keeper.LiveKeeperPresent, and writes to a real JSONL
// event log through the real event bus.
//
// WHAT IS SUBSTITUTED, AND WHY. The re-arm seam is a substrate double instead of
// tmux. Not for convenience: an in-process daemon cannot spawn a real keeper
// window, because spawnCrewKeeperWindow resolves the keeper binary with
// os.Executable(), which inside a test binary is the TEST binary — the spawned
// window would run `<pkg>.test keeper --agent …` and never take a flock. The
// tmux half of the contract (a nil re-arm return MUST mean a keeper was really
// spawned; a stale window is killed rather than counted as success) is pinned
// separately by keeperrevive_rearm_hk220lv_test.go against a tmux.Adapter double.
//
// Scenarios:
//
//	A — Revive restores MONITORING, not just an event. Crew "kilo" is registered,
//	    .managed, and has no live keeper. Assert session_keeper_watcher_dead is
//	    followed by session_keeper_watcher_revived in the event log AND that the
//	    keeper flock is genuinely HELD afterward. The flock assertion is the
//	    load-bearing one: an event-only assertion would have passed while the
//	    name-only window match made the sweep a no-op.
//
//	B — A re-arm that restores nothing does not go quiet. When the re-armer
//	    returns success but no keeper ever takes the flock, the sweep must keep
//	    retrying to the configured cap and then escalate to the operator, rather
//	    than declaring victory once and leaving the crew unmonitored.
//
//	C — The operator kill-switch works end to end. With
//	    keeper.timings.revive_scan_interval: 0s in the real config file, the sweep
//	    never runs and never re-arms.
//
// Helper prefix: krsc
//
// Bead ref: hk-220lv.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/keeper"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers (prefix: krsc)
// ─────────────────────────────────────────────────────────────────────────────

const (
	krscCrewName = "kilo"
	krscSession  = "harmonik-abc123-crew-kilo"
	krscHandle   = krscSession + ":agent"
)

// krscSubstrate is a handler.Substrate that also satisfies the daemon's
// (unexported) crewKeeperReArmer seam. When takeFlock is true its re-arm does
// what a real keeper window does that matters here: it acquires the agent's
// exclusive keeper flock and holds it, so keeper.LiveKeeperPresent flips to true
// exactly as it would with a live watcher process.
type krscSubstrate struct {
	projectDir string
	takeFlock  bool

	mu     sync.Mutex
	calls  []string // sessions passed to ReArmCrewKeeperWindow
	locks  []*keeper.Lock
	lockMu sync.Mutex
}

func (s *krscSubstrate) SpawnWindow(_ context.Context, _ handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	return nil, nil //nolint:nilnil // unused by this scenario; the work loop never runs (BrPath empty)
}

func (s *krscSubstrate) ReArmCrewKeeperWindow(_ context.Context, crewName, sessName, _ string) error {
	s.mu.Lock()
	s.calls = append(s.calls, sessName)
	s.mu.Unlock()

	if !s.takeFlock {
		// Simulates a re-arm that reported success but produced no live keeper.
		return nil
	}
	lock, err := keeper.AcquireLock(s.projectDir, crewName)
	if err != nil {
		return err
	}
	s.lockMu.Lock()
	s.locks = append(s.locks, lock)
	s.lockMu.Unlock()
	return nil
}

// krscReArmCount returns how many times the re-arm seam was invoked.
func (s *krscSubstrate) krscReArmCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// krscSessions returns the tmux session names the watcher asked to re-arm.
func (s *krscSubstrate) krscSessions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.calls))
	copy(out, s.calls)
	return out
}

// krscReleaseLocks drops every flock this double acquired.
func (s *krscSubstrate) krscReleaseLocks() {
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	for _, l := range s.locks {
		_ = l.Release() //nolint:errcheck // test teardown
	}
	s.locks = nil
}

var _ handler.Substrate = (*krscSubstrate)(nil)

// krscProject builds a project dir containing a real config.yaml, a real crew
// registry record, and a real .managed marker for krscCrewName. No keeper flock
// is taken, so the crew starts out exactly as the field case did: registered,
// keeper-managed, and completely unwatched.
func krscProject(t *testing.T, configYAML string) (projectDir, jsonlPath string) {
	t.Helper()

	// Keep the path short: the daemon binds .harmonik/daemon.sock and macOS caps
	// sun_path at 104 bytes.
	projectDir, err := os.MkdirTemp("/tmp", "krsc-")
	if err != nil {
		t.Fatalf("krscProject: MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(projectDir) }) //nolint:errcheck // test teardown

	for _, sub := range []string{".harmonik/events", ".harmonik/keeper", ".harmonik/crew"} {
		if mkErr := os.MkdirAll(filepath.Join(projectDir, sub), 0o755); mkErr != nil {
			t.Fatalf("krscProject: MkdirAll %s: %v", sub, mkErr)
		}
	}

	cfgPath := filepath.Join(projectDir, ".harmonik", "config.yaml")
	if wErr := os.WriteFile(cfgPath, []byte(configYAML), 0o600); wErr != nil {
		t.Fatalf("krscProject: write config.yaml: %v", wErr)
	}

	if wErr := crew.Write(projectDir, crew.Record{
		Name:      krscCrewName,
		SessionID: "11111111-1111-4111-8111-111111111111",
		Queue:     "q-kilo",
		Handle:    krscHandle,
		StartedAt: time.Now(),
	}); wErr != nil {
		t.Fatalf("krscProject: crew.Write: %v", wErr)
	}

	// The .managed opt-in marker: without it the sweep must never revive.
	managed := filepath.Join(projectDir, ".harmonik", "keeper", krscCrewName+".managed")
	if wErr := os.WriteFile(managed, []byte("11111111-1111-4111-8111-111111111111\n"), 0o600); wErr != nil {
		t.Fatalf("krscProject: write .managed: %v", wErr)
	}

	return projectDir, filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
}

// krscIdleBr writes a stub `br` that answers the boot handshake and reports an
// empty backlog forever.
//
// It exists for ONE structural reason, not convenience: with BrPath unset,
// daemon.Start wires everything, launches its background goroutines and RETURNS
// — and its `defer jsonlWriter.Close()` fires on that return. eventbus.Emit
// aborts on a JSONL append error BEFORE consumer dispatch (busimpl.go step 4b),
// so a daemon in that mode can neither write nor deliver any event a background
// watcher emits. Setting BrPath makes Start block on the work loop exactly as it
// does in production, which is the only state in which the event log is a real
// event log. The stub never returns work, so nothing is ever dispatched.
func krscIdleBr(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	script := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"  --version) echo 'br 0.1.45' ;;\n" +
		"  *) echo '[]' ;;\n" +
		"esac\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // G306: must be executable
		t.Fatalf("krscIdleBr: write stub: %v", err)
	}
	return path
}

// krscStartDaemon boots a real daemon against projectDir with sub as its
// substrate, and returns a stop function that cancels and waits for exit.
func krscStartDaemon(t *testing.T, projectDir, jsonlPath string, sub handler.Substrate) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	started := time.Now()
	go func() {
		err := daemon.Start(ctx, daemon.Config{
			ProjectDir:            projectDir,
			JSONLLogPath:          jsonlPath,
			Substrate:             sub,
			BrPath:                krscIdleBr(t),
			NoAutoPull:            true,
			WorkflowModeDefault:   core.WorkflowModeDot,
			SkipWALCheckpoint:     true,
			SkipBrHistoryRotation: true,
			SkipRestartBackoff:    true,
		})
		t.Logf("daemon.Start returned after %s: %v", time.Since(started), err)
		done <- err
	}()
	// A boot FAILURE must surface immediately rather than as a silent "nothing
	// ever happened" 30 s later. A nil return is normal here: with BrPath unset
	// Start wires everything, launches its background goroutines under ctx, and
	// returns without blocking on a work loop.
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("daemon.Start failed during boot: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
	}

	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Error("daemon.Start did not exit within 30s of context cancel")
		}
	}
}

// krscReadLog returns the JSONL log lines written so far.
func krscReadLog(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: path is under a t-owned temp dir
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("krscReadLog: %v", err)
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// krscCount counts log lines containing needle.
func krscCount(lines []string, needle string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, needle) {
			n++
		}
	}
	return n
}

// krscTypes extracts the distinct "type" values present in the log, so a
// missing-event failure names what DID land instead of just what did not.
func krscTypes(lines []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range lines {
		var env struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(l), &env) != nil || env.Type == "" || seen[env.Type] {
			continue
		}
		seen[env.Type] = true
		out = append(out, env.Type)
	}
	return out
}

// krscFirstIndex returns the index of the first line containing needle, or -1.
func krscFirstIndex(lines []string, needle string) int {
	for i, l := range lines {
		if strings.Contains(l, needle) {
			return i
		}
	}
	return -1
}

// krscWait polls cond every 20 ms up to budget. Returns true if cond held.
func krscWait(budget time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

// krscFastReviveYAML is a real config.yaml turning the sweep's cadence down to
// test speed. The keys travel parseKeeperBlock exactly as they would in prod.
const krscFastReviveYAML = `schema_version: 1
sentinel:
  liveness_no_progress_n: 0
keeper:
  timings:
    revive_scan_interval: 50ms
    revive_grace: 100ms
  budgets:
    revive_max_attempts: 2
`

// ─────────────────────────────────────────────────────────────────────────────
// Scenario A — revive restores MONITORING, not merely an event
// ─────────────────────────────────────────────────────────────────────────────

func TestScenario_KeeperRevive_DeadWatcher_MonitoringRestored_hk220lv(t *testing.T) {
	projectDir, jsonlPath := krscProject(t, krscFastReviveYAML)

	sub := &krscSubstrate{projectDir: projectDir, takeFlock: true}
	t.Cleanup(sub.krscReleaseLocks)

	if keeper.LiveKeeperPresent(projectDir, krscCrewName) {
		t.Fatal("precondition: a keeper flock is already held; the scenario must start with an unwatched crew")
	}

	stop := krscStartDaemon(t, projectDir, jsonlPath, sub)
	defer stop()

	const budget = 30 * time.Second

	// The remediation the operator actually cares about: a live watcher again.
	if !krscWait(budget, func() bool { return keeper.LiveKeeperPresent(projectDir, krscCrewName) }) {
		t.Fatalf("keeper flock still UNHELD after %s — the sweep did not restore monitoring "+
			"(regression: the crew stays unwatched exactly as in the 43h field case, no matter what the "+
			"event log claims). re-arm calls=%d", budget, sub.krscReArmCount())
	}

	// And the durable audit trail, in order: diagnosis then remediation.
	if !krscWait(budget, func() bool {
		return krscCount(krscReadLog(t, jsonlPath), "session_keeper_watcher_revived") > 0
	}) {
		t.Fatalf("no session_keeper_watcher_revived in %s within %s; log holds %d lines: %v",
			jsonlPath, budget, len(krscReadLog(t, jsonlPath)), krscTypes(krscReadLog(t, jsonlPath)))
	}

	lines := krscReadLog(t, jsonlPath)
	deadAt := krscFirstIndex(lines, "session_keeper_watcher_dead")
	revivedAt := krscFirstIndex(lines, "session_keeper_watcher_revived")
	if deadAt < 0 {
		t.Errorf("no session_keeper_watcher_dead emitted (regression: the self-heal fires with no durable "+
			"diagnosis, so a post-mortem cannot tell the keeper ever died). lines=%d", len(lines))
	}
	if deadAt >= 0 && revivedAt >= 0 && deadAt > revivedAt {
		t.Errorf("event order = revived(%d) before dead(%d); want dead first "+
			"(specs/event-model.md §8.16.19 states revived always follows dead for the same agent)",
			revivedAt, deadAt)
	}

	// The watcher must target the crew's own tmux session, derived from the
	// registry Handle with the ":agent" window suffix trimmed.
	for _, got := range sub.krscSessions() {
		if got != krscSession {
			t.Errorf("re-arm session = %q; want %q (regression: re-arming into the wrong tmux target "+
				"spawns a keeper that watches nothing)", got, krscSession)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario B — a re-arm that restores nothing must not go quiet
// ─────────────────────────────────────────────────────────────────────────────

func TestScenario_KeeperRevive_ReArmRestoresNothing_RetriesThenEscalates_hk220lv(t *testing.T) {
	projectDir, jsonlPath := krscProject(t, krscFastReviveYAML)

	// takeFlock=false: the re-arm reports success but no keeper ever appears.
	sub := &krscSubstrate{projectDir: projectDir, takeFlock: false}

	stop := krscStartDaemon(t, projectDir, jsonlPath, sub)
	defer stop()

	const budget = 30 * time.Second
	const wantAttempts = 2 // keeper.budgets.revive_max_attempts in krscFastReviveYAML

	// It must keep trying up to the cap rather than declaring victory once.
	if !krscWait(budget, func() bool { return sub.krscReArmCount() >= wantAttempts }) {
		t.Fatalf("re-arm calls = %d after %s; want %d — a re-arm that did not restore the flock was "+
			"treated as done (regression: report-green-do-nothing, the failure class hk-220lv closes)",
			sub.krscReArmCount(), budget, wantAttempts)
	}

	// And it must escalate to the operator once the cap is hit.
	if !krscWait(budget, func() bool {
		return krscCount(krscReadLog(t, jsonlPath), "keeper-alert") > 0
	}) {
		t.Fatalf("no keeper-alert comms after %s (regression: the crew is permanently unmonitored and "+
			"nobody is told). re-arm calls=%d", budget, sub.krscReArmCount())
	}

	// Then it must STOP: the cap bounds window spawning.
	settle := sub.krscReArmCount()
	time.Sleep(1 * time.Second) // ≥ 20 scan intervals at 50ms
	if got := sub.krscReArmCount(); got != settle || got > wantAttempts {
		t.Errorf("re-arm calls = %d after settling (was %d, cap %d); want the cap to hold "+
			"(regression: an unrevivable crew spawns a tmux window on every scan, forever)",
			got, settle, wantAttempts)
	}

	if got := krscCount(krscReadLog(t, jsonlPath), "keeper-alert"); got != 1 {
		t.Errorf("keeper-alert comms = %d; want exactly 1 (regression: >1 means the escalation repeats "+
			"per scan and the operator learns to mute it)", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario C — the operator kill-switch, through the real config file
// ─────────────────────────────────────────────────────────────────────────────

func TestScenario_KeeperRevive_ExplicitZeroScanInterval_NeverRevives_hk220lv(t *testing.T) {
	const offYAML = `schema_version: 1
sentinel:
  liveness_no_progress_n: 0
keeper:
  timings:
    revive_scan_interval: 0s
    revive_grace: 100ms
`
	projectDir, jsonlPath := krscProject(t, offYAML)

	sub := &krscSubstrate{projectDir: projectDir, takeFlock: true}
	t.Cleanup(sub.krscReleaseLocks)

	stop := krscStartDaemon(t, projectDir, jsonlPath, sub)
	defer stop()

	// Give the sweep far longer than it would need if it were running at all.
	time.Sleep(2 * time.Second)

	if got := sub.krscReArmCount(); got != 0 {
		t.Errorf("re-arm calls = %d; want 0 (regression: `revive_scan_interval: 0s` is the operator's ONLY "+
			"opt-out and it was ignored)", got)
	}
	if keeper.LiveKeeperPresent(projectDir, krscCrewName) {
		t.Error("a keeper flock was taken while the sweep is disabled; want none")
	}
	lines := krscReadLog(t, jsonlPath)
	if got := krscCount(lines, "session_keeper_watcher_revived"); got != 0 {
		t.Errorf("session_keeper_watcher_revived count = %d; want 0 while disabled", got)
	}
}

// ## Test plan
//
// Written in the SAME commit as the fix rather than red-first. build-practices.md
// allows this when bisect value is low and the choice is documented; it is low
// here because the product code is a NEW file (internal/daemon/keeperrevive.go)
// plus a new substrate method — there is no prior revision at which this scenario
// could have been meaningfully red rather than simply un-compilable.
//
// The regression these scenarios lock down was nonetheless observed red before
// the fix landed: with the presence-based re-arm (a window merely NAMED "keeper"
// counted as success) Scenario A fails on the flock assertion while the event log
// shows a revived event, which is exactly the false-green shape under test.
