package main

// captain_launch_hkbcd0_test.go — ES2 (hk-bcd0) coverage for the NATIVE
// full-parity captain launcher: hashed-namespace session name, agent+keeper
// window-nesting argv (via the shared agentlaunch helper), the keeper warn/act
// band, the sentinel/pid file effects, and the D7 idempotent existing-session
// pre-flight. All assertions run through the injected seams — NO real tmux.
//
// Integration-only (NOT covered here, honest per plan review E): tmux actually
// taking effect, /clear injection into a live pane, and the six process-
// choreography invariants (session_id flip on /clear, --resume vs --session-id,
// agent-window-only respawn, no-dup-keeper, sentinel/pid refresh churn). Those
// are runtime sequencing on Linux, not argv shape.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/agentlaunch"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// fakeCaptainOps is the injectable captainTmuxOps test double. It records the
// keeper-window opts it is handed, drives the D7 branch via existsResult, and
// returns a deterministic pane PID — no real tmux server is touched.
type fakeCaptainOps struct {
	existsResult bool  // what SessionExists returns
	existsErr    error // optional SessionExists error
	killErr      error // optional KillSession error
	panePID      int   // what AgentPanePID returns (0 → "could not resolve")
	panePIDErr   error

	existsCalls   int
	killCalls     int
	killedSession string
	keeperOpts    *agentlaunch.KeeperWindowOpts // nil until SpawnKeeperWindow called
	keeperOutcome ltmux.Outcome                 // what SpawnKeeperWindow returns
}

func (f *fakeCaptainOps) SessionExists(_ context.Context, _ string) (bool, error) {
	f.existsCalls++
	return f.existsResult, f.existsErr
}

func (f *fakeCaptainOps) KillSession(_ context.Context, sess string) error {
	f.killCalls++
	f.killedSession = sess
	return f.killErr
}

func (f *fakeCaptainOps) SpawnKeeperWindow(_ context.Context, opts agentlaunch.KeeperWindowOpts) ltmux.Outcome {
	cp := opts
	f.keeperOpts = &cp
	return f.keeperOutcome
}

func (f *fakeCaptainOps) AgentPanePID(_ context.Context, _ string) (int, error) {
	if f.panePID == 0 && f.panePIDErr == nil {
		f.panePID = 4242 // default live pid
	}
	return f.panePID, f.panePIDErr
}

// expectedHashedSession mirrors the launcher's in-process hash derivation so the
// test asserts the SAME session name the launcher computes.
func expectedHashedSession(t *testing.T, project string) string {
	t.Helper()
	realDir, err := filepath.EvalSymlinks(project)
	if err != nil {
		realDir = project
	}
	return lifecycle.TmuxSessionName(lifecycle.ComputeProjectHash(realDir), "captain")
}

func TestCaptainLaunch_DefaultsToHashedNamespace_hkbcd0(t *testing.T) {
	run, captured := captureRunHkly0n()
	ops := &fakeCaptainOps{}
	proj := t.TempDir()

	code := runCaptainLaunchWithOps([]string{"--project", proj}, run, noopKeeperHkly0n, ops)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	argv := argvHkly0n(*captured)
	wantSess := expectedHashedSession(t, proj)
	if got := flagValueHkly0n(argv, "-s"); got != wantSess {
		t.Errorf("tmux session = %q, want hashed namespace %q", got, wantSess)
	}
	// Agent window MUST be named "agent" so the keeper can target <session>:agent.
	if got := flagValueHkly0n(argv, "-n"); got != ltmux.WindowAgent {
		t.Errorf("first window name = %q, want %q", got, ltmux.WindowAgent)
	}
}

func TestCaptainLaunch_ExplicitTmuxOverridesHash_hkbcd0(t *testing.T) {
	run, captured := captureRunHkly0n()
	ops := &fakeCaptainOps{}
	code := runCaptainLaunchWithOps([]string{"--tmux", "cap-explicit", "--project", t.TempDir()}, run, noopKeeperHkly0n, ops)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if got := flagValueHkly0n(argvHkly0n(*captured), "-s"); got != "cap-explicit" {
		t.Errorf("tmux session = %q, want explicit override %q", got, "cap-explicit")
	}
}

func TestCaptainLaunch_ArmsKeeperWindowWithFullBand_hkbcd0(t *testing.T) {
	run, _ := captureRunHkly0n()
	ops := &fakeCaptainOps{}
	proj := t.TempDir()

	code := runCaptainLaunchWithOps([]string{"--project", proj}, run, noopKeeperHkly0n, ops)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if ops.keeperOpts == nil {
		t.Fatal("keeper window was not spawned")
	}
	o := *ops.keeperOpts
	if o.WarnOnly {
		t.Error("captain keeper must NOT be --warn-only; it is force-cut with a full band")
	}
	if o.WarnAbsTokens != keeper.DefaultWarnAbsTokens || o.ActAbsTokens != keeper.DefaultActAbsTokens {
		t.Errorf("keeper band = warn %d / act %d, want defaults %d / %d",
			o.WarnAbsTokens, o.ActAbsTokens, keeper.DefaultWarnAbsTokens, keeper.DefaultActAbsTokens)
	}
	if o.Session != expectedHashedSession(t, proj) {
		t.Errorf("keeper opts.Session = %q, want %q", o.Session, expectedHashedSession(t, proj))
	}
	if o.AgentName != "captain" {
		t.Errorf("keeper opts.AgentName = %q, want %q", o.AgentName, "captain")
	}

	// Cross-check the SHARED argv-builder produces a session:agent inject target
	// and the explicit band flags (not --warn-only).
	keeperArgv := agentlaunch.KeeperWindowArgv(o)
	if got := flagValueHkly0n(keeperArgv, "--tmux"); got != o.Session+":"+ltmux.WindowAgent {
		t.Errorf("keeper --tmux = %q, want %q", got, o.Session+":"+ltmux.WindowAgent)
	}
	if containsHkly0n(keeperArgv, "--warn-only") {
		t.Error("captain keeper argv must not contain --warn-only")
	}
	if flagValueHkly0n(keeperArgv, "--warn-abs-tokens") == "" || flagValueHkly0n(keeperArgv, "--act-abs-tokens") == "" {
		t.Errorf("keeper argv missing explicit band flags: %v", keeperArgv)
	}
}

func TestCaptainLaunch_HonorsCustomBandFlags_hkbcd0(t *testing.T) {
	run, _ := captureRunHkly0n()
	ops := &fakeCaptainOps{}
	code := runCaptainLaunchWithOps([]string{
		"--project", t.TempDir(), "--warn-abs-tokens", "111111", "--act-abs-tokens", "222222",
	}, run, noopKeeperHkly0n, ops)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if ops.keeperOpts == nil {
		t.Fatal("keeper window not spawned")
	}
	if ops.keeperOpts.WarnAbsTokens != 111111 || ops.keeperOpts.ActAbsTokens != 222222 {
		t.Errorf("band = %d/%d, want 111111/222222", ops.keeperOpts.WarnAbsTokens, ops.keeperOpts.ActAbsTokens)
	}
}

func TestCaptainLaunch_WritesSentinelAndPID_hkbcd0(t *testing.T) {
	run, _ := captureRunHkly0n()
	ops := &fakeCaptainOps{panePID: 9876}
	proj := t.TempDir()

	code := runCaptainLaunchWithOps([]string{"--project", proj}, run, noopKeeperHkly0n, ops)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}

	sentinel := filepath.Join(proj, ".harmonik", "cognition", "captain.sentinel")
	b, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("captain.sentinel not written: %v", err)
	}
	if !strings.Contains(string(b), "schema_version=1") {
		t.Errorf("captain.sentinel = %q, want schema_version=1", string(b))
	}

	pidFile := filepath.Join(proj, ".harmonik", "cognition", "captain.pid")
	pb, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("captain.pid not written: %v", err)
	}
	if strings.TrimSpace(string(pb)) != "9876" {
		t.Errorf("captain.pid = %q, want 9876", strings.TrimSpace(string(pb)))
	}
}

func TestCaptainLaunch_SentinelWrittenEvenWhenPidUnresolved_hkbcd0(t *testing.T) {
	run, _ := captureRunHkly0n()
	ops := &fakeCaptainOps{panePID: 0, panePIDErr: ltmux.ErrNoSession}
	proj := t.TempDir()

	code := runCaptainLaunchWithOps([]string{"--project", proj}, run, noopKeeperHkly0n, ops)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (a missing pid must not block the launch)", code)
	}
	// Sentinel still written (sweep PRIMARY path probes the live session).
	if _, err := os.Stat(filepath.Join(proj, ".harmonik", "cognition", "captain.sentinel")); err != nil {
		t.Errorf("captain.sentinel must be written even when pid is unresolved: %v", err)
	}
	// captain.pid must NOT exist (we could not resolve a pid).
	if _, err := os.Stat(filepath.Join(proj, ".harmonik", "cognition", "captain.pid")); !os.IsNotExist(err) {
		t.Errorf("captain.pid should not exist when pane PID is unresolved (stat err = %v)", err)
	}
}

func TestCaptainLaunch_D7ReapsExistingSession_hkbcd0(t *testing.T) {
	run, captured := captureRunHkly0n()
	ops := &fakeCaptainOps{existsResult: true} // keeper outlived the agent
	proj := t.TempDir()

	code := runCaptainLaunchWithOps([]string{"--project", proj}, run, noopKeeperHkly0n, ops)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (re-launch over a stale session must Just Work, D7)", code)
	}
	if ops.killCalls != 1 {
		t.Fatalf("KillSession called %d times, want exactly 1 (D7 reap)", ops.killCalls)
	}
	wantSess := expectedHashedSession(t, proj)
	if ops.killedSession != wantSess {
		t.Errorf("reaped session = %q, want %q", ops.killedSession, wantSess)
	}
	// After reaping, the agent window must still be (re)launched.
	if *captured == nil {
		t.Error("agent window must be recreated after the D7 reap")
	}
}

func TestCaptainLaunch_D7NoReapWhenSessionAbsent_hkbcd0(t *testing.T) {
	run, _ := captureRunHkly0n()
	ops := &fakeCaptainOps{existsResult: false}
	code := runCaptainLaunchWithOps([]string{"--project", t.TempDir()}, run, noopKeeperHkly0n, ops)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if ops.killCalls != 0 {
		t.Errorf("KillSession called %d times, want 0 when no session exists", ops.killCalls)
	}
}

func TestCaptainLaunch_D7ReapFailureBlocks_hkbcd0(t *testing.T) {
	run, captured := captureRunHkly0n()
	ops := &fakeCaptainOps{existsResult: true, killErr: ltmux.ErrNoSession}
	code := runCaptainLaunchWithOps([]string{"--project", t.TempDir()}, run, noopKeeperHkly0n, ops)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (a failed reap must block before recreating)", code)
	}
	if *captured != nil {
		t.Error("agent window must NOT be launched when the D7 reap fails")
	}
}

func TestCaptainLaunch_NoKeeperSkipsKeeperWindow_hkbcd0(t *testing.T) {
	run, captured := captureRunHkly0n()
	ops := &fakeCaptainOps{}
	code := runCaptainLaunchWithOps([]string{"--no-keeper", "--project", t.TempDir()}, run, noopKeeperHkly0n, ops)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if ops.keeperOpts != nil {
		t.Error("--no-keeper must skip arming the keeper window")
	}
	if *captured == nil {
		t.Error("agent window must still launch under --no-keeper")
	}
}

func TestCaptainLaunch_KeeperWindowFailureDoesNotBlock_hkbcd0(t *testing.T) {
	run, captured := captureRunHkly0n()
	ops := &fakeCaptainOps{keeperOutcome: ltmux.Outcome{Err: ltmux.ErrNoSession}}
	code := runCaptainLaunchWithOps([]string{"--project", t.TempDir()}, run, noopKeeperHkly0n, ops)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (a keeper-window failure must not block the launch)", code)
	}
	if *captured == nil {
		t.Error("agent window must still launch when the keeper window fails")
	}
}
