package daemon

// crewstart_hkmmlqt_test.go — regression tests for hk-mmlqt.
//
// Bug: crews were spawned as tmux WINDOWS inside the daemon's own session, so
// daemon SIGTERM / supervisor-revive tore down every crew window.
//
// Fix: crews are now spawned via crewSessionSpawner.SpawnCrewSession which
// creates an independent tmux session, decoupled from the daemon's lifecycle.
// The crew-stop path uses crewSessionStopper.StopCrewSession to kill the whole
// independent session rather than just the window.
//
// These tests verify:
//   - SpawnCrewSession is called (not SpawnWindow) when substrate implements crewSessionSpawner.
//   - The independent session name follows the project-qualified form (hk-ohd fleet-portability T2).
//   - The crew registry handle is recorded from the independent session.
//   - StopCrewSession is called (not StopWindowByHandle) when substrate implements crewSessionStopper.
//   - The fallback SpawnWindow path remains intact for substrates that don't implement crewSessionSpawner.
//
// Bead ref: hk-mmlqt.

import (
	"context"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// crewWindowRecordingAdapter — tmux.Adapter + sessionCreator double (hk-rmy1)
// ─────────────────────────────────────────────────────────────────────────────

// crewWindowRecordingAdapter records the NewSessionIn (agent window) and
// NewWindowIn (keeper window) parameters so the slice-C two-window topology can
// be asserted without a real tmux server. It implements tmux.Adapter and the
// daemon-local sessionCreator interface (NewSessionIn).
type crewWindowRecordingAdapter struct {
	newSessionParams tmux.NewWindowIn
	newWindowParams  []tmux.NewWindowIn
}

// NewSessionIn satisfies sessionCreator: records the agent-window/session params.
func (a *crewWindowRecordingAdapter) NewSessionIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.newSessionParams = params
	return tmux.Outcome{Handle: tmux.WindowHandle(params.Session + ":" + params.WindowName), PaneID: "%1"}
}

// NewWindowIn satisfies tmux.Adapter: records each sibling-window creation
// (the keeper window on the slice-C path).
func (a *crewWindowRecordingAdapter) NewWindowIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.newWindowParams = append(a.newWindowParams, params)
	return tmux.Outcome{Handle: tmux.WindowHandle(params.Session + ":" + params.WindowName), PaneID: "%2"}
}

func (a *crewWindowRecordingAdapter) ProbeTmux(_ context.Context) error { return nil }
func (a *crewWindowRecordingAdapter) ListSessions(_ context.Context) ([]string, error) {
	return nil, nil
}
func (a *crewWindowRecordingAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (a *crewWindowRecordingAdapter) KillWindow(_ context.Context, _ tmux.WindowHandle) error {
	return nil
}
func (a *crewWindowRecordingAdapter) WindowPanePID(_ context.Context, _ tmux.WindowHandle) (int, error) {
	return 0, nil
}
func (a *crewWindowRecordingAdapter) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return "", nil
}
func (a *crewWindowRecordingAdapter) KillSession(_ context.Context, _ string) error { return nil }
func (a *crewWindowRecordingAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (a *crewWindowRecordingAdapter) PasteBuffer(_ context.Context, _, _ string) error { return nil }
func (a *crewWindowRecordingAdapter) SendKeysLiteral(_ context.Context, _, _ string) error {
	return nil
}
func (a *crewWindowRecordingAdapter) SendKeysEnter(_ context.Context, _ string) error { return nil }
func (a *crewWindowRecordingAdapter) SendKeysQuit(_ context.Context, _ string) error  { return nil }
func (a *crewWindowRecordingAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

var _ tmux.Adapter = (*crewWindowRecordingAdapter)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// Test doubles
// ─────────────────────────────────────────────────────────────────────────────

// hkmmlqtCrewSessionSubstrate implements handler.Substrate, crewSessionSpawner,
// and crewSessionStopper to exercise the independent-session path.
type hkmmlqtCrewSessionSubstrate struct {
	// SpawnCrewSession records
	spawnSessionCalled bool
	spawnSessionName   string // crewName passed to SpawnCrewSession
	spawnWindowCalled  bool   // true if SpawnWindow was called instead (fallback)
	spawnErr           error

	// StopCrewSession records
	stopSessionCalled bool
	stopSessionName   string
	stopWindowCalled  bool // true if StopWindowByHandle was called instead
}

// SpawnWindow satisfies handler.Substrate. Should NOT be called on the
// independent-session path.
func (f *hkmmlqtCrewSessionSubstrate) SpawnWindow(_ context.Context, _ handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	f.spawnWindowCalled = true
	return &fakeSession{handle: "fallback-window"}, nil
}

// SpawnCrewSession implements crewSessionSpawner.
func (f *hkmmlqtCrewSessionSubstrate) SpawnCrewSession(_ context.Context, crewName string, _ handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	f.spawnSessionCalled = true
	f.spawnSessionName = crewName
	if f.spawnErr != nil {
		return nil, f.spawnErr
	}
	// Fake uses the legacy fallback session name to keep the test simple.
	sessName := "hk-crew-" + crewName
	return &fakeSession{handle: sessName + ":hk-crew-" + crewName}, nil
}

// StopWindowByHandle satisfies crewPaneStopper. Should NOT be called when
// crewSessionStopper is available.
func (f *hkmmlqtCrewSessionSubstrate) StopWindowByHandle(_ context.Context, _ string) error {
	f.stopWindowCalled = true
	return nil
}

// StopCrewSession implements crewSessionStopper.
func (f *hkmmlqtCrewSessionSubstrate) StopCrewSession(_ context.Context, crewName string, _ string) error {
	f.stopSessionCalled = true
	f.stopSessionName = crewName
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestCrewStart_IndependentSession_hkmmlqt verifies that when the substrate
// implements crewSessionSpawner, SpawnCrewSession (not SpawnWindow) is called
// and the registry handle encodes the independent session name.
func TestCrewStart_IndependentSession_hkmmlqt(t *testing.T) {
	sub := &hkmmlqtCrewSessionSubstrate{}
	h, dir := newTestCrewHandler(t, sub, nil)

	result := mustCrewStart(t, h, CrewStartRequest{
		Name:  "chani",
		Queue: "crew-chani",
	})

	if result.SessionID == "" {
		t.Error("crew-start: expected non-empty session_id")
	}

	// crewSessionSpawner.SpawnCrewSession must be called, not SpawnWindow.
	if !sub.spawnSessionCalled {
		t.Error("SpawnCrewSession was not called; substrate's crewSessionSpawner was not used")
	}
	if sub.spawnWindowCalled {
		t.Error("SpawnWindow was called; should use SpawnCrewSession on the independent-session path")
	}
	if sub.spawnSessionName != "chani" {
		t.Errorf("SpawnCrewSession crewName = %q, want %q", sub.spawnSessionName, "chani")
	}

	// The recorded handle must carry the independent session name prefix.
	// The fake substrate uses "hk-crew-<name>" as session name.
	rec, loadErr := crew.Load(dir, "chani")
	if loadErr != nil {
		t.Fatalf("crew.Load: %v", loadErr)
	}
	wantPrefix := "hk-crew-chani:"
	if !strings.HasPrefix(rec.Handle, wantPrefix) {
		t.Errorf("registry handle = %q, want prefix %q (independent session)", rec.Handle, wantPrefix)
	}
}

// TestCrewStop_IndependentSession_hkmmlqt verifies that when the substrate
// implements crewSessionStopper, StopCrewSession (not StopWindowByHandle) is
// called on crew-stop.
func TestCrewStop_IndependentSession_hkmmlqt(t *testing.T) {
	sub := &hkmmlqtCrewSessionSubstrate{}
	h, _ := newTestCrewHandler(t, sub, nil)

	mustCrewStart(t, h, CrewStartRequest{Name: "stilgar", Queue: "crew-stilgar"})
	mustCrewStop(t, h, CrewStopRequest{Name: "stilgar"})

	if !sub.stopSessionCalled {
		t.Error("StopCrewSession was not called; substrate's crewSessionStopper was not used")
	}
	if sub.stopWindowCalled {
		t.Error("StopWindowByHandle was called; should use StopCrewSession on the independent-session path")
	}
	if sub.stopSessionName != "stilgar" {
		t.Errorf("StopCrewSession crewName = %q, want %q", sub.stopSessionName, "stilgar")
	}
}

// TestCrewStart_FallbackToSpawnWindow_hkmmlqt verifies that substrates that do
// NOT implement crewSessionSpawner fall back to SpawnWindow (existing behaviour).
func TestCrewStart_FallbackToSpawnWindow_hkmmlqt(t *testing.T) {
	// fakeSubstrate does not implement crewSessionSpawner.
	sub := &fakeSubstrate{}
	h, _ := newTestCrewHandler(t, sub, nil)

	mustCrewStart(t, h, CrewStartRequest{Name: "duncan", Queue: "crew-duncan"})

	if !sub.spawnCalled {
		t.Error("SpawnWindow was not called; expected fallback to SpawnWindow for non-crewSessionSpawner substrate")
	}
}

// TestCrewStop_FallbackToStopWindowByHandle_hkmmlqt verifies that substrates
// that do NOT implement crewSessionStopper fall back to StopWindowByHandle.
func TestCrewStop_FallbackToStopWindowByHandle_hkmmlqt(t *testing.T) {
	sub := &fakeSubstrate{}
	h, _ := newTestCrewHandler(t, sub, nil)

	mustCrewStart(t, h, CrewStartRequest{Name: "liet", Queue: "crew-liet"})
	mustCrewStop(t, h, CrewStopRequest{Name: "liet"})

	if !sub.stopCalled {
		t.Error("StopWindowByHandle was not called; expected fallback for non-crewSessionStopper substrate")
	}
}

// TestCrewSessionName_hkmmlqt verifies the deterministic session name formula.
// hk-rmy1 (slice C): the name is ALWAYS the project-qualified form
// "harmonik-<hash>-crew-<name>"; the legacy "hk-crew-<name>" no-hash fallback was
// DELETED. With no project hash, crewSessionName returns an error (never a legacy
// name).
func TestCrewSessionName_hkmmlqt(t *testing.T) {
	const testHash core.ProjectHash = "abc123def456"

	// Project-qualified (T2): with project hash → always the harmonik-<hash>- form.
	cases := []struct {
		name string
		want string
	}{
		{"alpha", lifecycle.TmuxSessionName(testHash, "crew-alpha")},
		{"chani-1", lifecycle.TmuxSessionName(testHash, "crew-chani-1")},
	}
	for _, tc := range cases {
		sub := &tmuxSubstrate{projectHash: testHash}
		got, err := sub.crewSessionName(tc.name)
		if err != nil {
			t.Errorf("crewSessionName(%q, hash=%q): unexpected error: %v", tc.name, testHash, err)
			continue
		}
		if got != tc.want {
			t.Errorf("crewSessionName(%q, hash=%q) = %q, want %q", tc.name, testHash, got, tc.want)
		}
		if strings.HasPrefix(got, "hk-crew-") {
			t.Errorf("crewSessionName(%q) = %q: legacy hk-crew- form must never be produced", tc.name, got)
		}
	}

	// No project hash → error, NOT a legacy "hk-crew-<name>" name.
	noHash := &tmuxSubstrate{projectHash: ""}
	got, err := noHash.crewSessionName("alpha")
	if err == nil {
		t.Errorf("crewSessionName with empty hash: want error, got name %q", got)
	}
	if got != "" {
		t.Errorf("crewSessionName with empty hash: want empty name, got %q", got)
	}
}

// TestCrewKeeperWindowArgv_hkrmy1 verifies the keeper-window launch argv: the
// per-crew keeper targets the sibling "agent" window via "--tmux <session>:agent"
// (slice K inject-target contract).
//
// ES5 / hk-lcga (D4): the crew keeper is FORCE-CUT by default — full band, NOT
// --warn-only — so a crew that fills its context gets force-cut + restarted instead
// of nagging forever. Operator-required-config change: the band NUMBERS come from
// the operator's keeper: config (no product default), so the argv carries no band
// flags — they are omitted and the keeper reads .harmonik/config.yaml.
func TestCrewKeeperWindowArgv_hkrmy1(t *testing.T) {
	const (
		keeperBin = "/usr/local/bin/harmonik"
		crewName  = "alpha"
		sessName  = "harmonik-abc123def456-crew-alpha"
		projDir   = "/home/op/proj"
	)
	argv := crewKeeperWindowArgv(keeperBin, crewName, sessName, projDir)
	joined := strings.Join(argv, " ")

	// Must invoke the keeper subcommand for the right crew.
	if argv[0] != keeperBin {
		t.Errorf("argv[0] = %q, want keeper binary %q", argv[0], keeperBin)
	}
	if !containsPair(argv, "keeper", "") || argv[1] != "keeper" {
		t.Errorf("argv = %v, want 'keeper' subcommand", argv)
	}
	if !containsPair(argv, "--agent", crewName) {
		t.Errorf("argv = %v, want --agent %q", argv, crewName)
	}

	// THE load-bearing assertion: --tmux targets the sibling "agent" window.
	wantTarget := sessName + ":agent"
	if !containsPair(argv, "--tmux", wantTarget) {
		t.Errorf("argv = %v, want --tmux %q (slice-K window inject target)", argv, wantTarget)
	}
	if !strings.Contains(joined, ":agent") {
		t.Errorf("keeper argv %q missing the ':agent' window suffix", joined)
	}

	// D4: crew keeper is FORCE-CUT (full band, NOT --warn-only). Operator-required-
	// config change: the band NUMBERS come from the operator's keeper: config, so the
	// argv carries NO product-default band — the abs flags are OMITTED. The keeper
	// reads .harmonik/config.yaml (and refuses to start if a required value is unset).
	if argvHasFlag(argv, "--warn-only") {
		t.Errorf("argv = %v, must NOT carry --warn-only (D4: crew is force-cut)", argv)
	}
	if argvHasFlag(argv, "--warn-abs-tokens") || argvHasFlag(argv, "--act-abs-tokens") {
		t.Errorf("argv = %v, must OMIT the band flags (no product default; keeper reads operator config)", argv)
	}

	// Pinned to the project.
	if !containsPair(argv, "--project", projDir) {
		t.Errorf("argv = %v, want --project %q", argv, projDir)
	}

	// Empty project dir omits the --project flag (best-effort derivation).
	argvNoProj := crewKeeperWindowArgv(keeperBin, crewName, sessName, "")
	if argvHasFlag(argvNoProj, "--project") {
		t.Errorf("argv = %v, want no --project flag when projectDir empty", argvNoProj)
	}
}

// TestSpawnCrewSession_AgentAndKeeperWindows_hkrmy1 verifies SpawnCrewSession
// creates the crew session with the "agent" window (NewSessionIn) AND adds a
// sibling "keeper" window (NewWindowIn) whose command launches the keeper with
// the "--tmux <session>:agent" inject target.
func TestSpawnCrewSession_AgentAndKeeperWindows_hkrmy1(t *testing.T) {
	const testHash core.ProjectHash = "abc123def456"
	adapter := &crewWindowRecordingAdapter{}
	sub := &tmuxSubstrate{adapter: adapter, sessionName: "daemon-default", projectHash: testHash}

	spawn := handler.SubstrateSpawn{
		Cwd:  "/home/op/proj",
		Env:  []string{"HARMONIK_PROJECT=/home/op/proj", "HARMONIK_AGENT=alpha"},
		Argv: []string{"claude", "--remote-control", "alpha"},
	}
	if _, err := sub.SpawnCrewSession(context.Background(), "alpha", spawn); err != nil {
		t.Fatalf("SpawnCrewSession: %v", err)
	}

	wantSession := lifecycle.TmuxSessionName(testHash, "crew-alpha")

	// The session must be created with the "agent" window.
	if adapter.newSessionParams.Session != wantSession {
		t.Errorf("NewSessionIn session = %q, want %q", adapter.newSessionParams.Session, wantSession)
	}
	if adapter.newSessionParams.WindowName != "agent" {
		t.Errorf("crew claude window = %q, want %q", adapter.newSessionParams.WindowName, "agent")
	}

	// A sibling "keeper" window must be added in the SAME session.
	if len(adapter.newWindowParams) != 1 {
		t.Fatalf("NewWindowIn called %d times, want 1 (the keeper window)", len(adapter.newWindowParams))
	}
	kw := adapter.newWindowParams[0]
	if kw.Session != wantSession {
		t.Errorf("keeper window session = %q, want %q (same session as agent)", kw.Session, wantSession)
	}
	if kw.WindowName != "keeper" {
		t.Errorf("keeper window name = %q, want %q", kw.WindowName, "keeper")
	}

	// The keeper window's command must target the agent window via --tmux.
	if !strings.Contains(kw.Command, "keeper") || !strings.Contains(kw.Command, "--agent") {
		t.Errorf("keeper window command = %q, want a 'keeper --agent ...' invocation", kw.Command)
	}
	wantInject := wantSession + ":agent"
	if !strings.Contains(kw.Command, "--tmux") || !strings.Contains(kw.Command, wantInject) {
		t.Errorf("keeper window command = %q, want --tmux %q", kw.Command, wantInject)
	}
	// ES5 / hk-lcga (D4): the crew keeper is force-cut by default (full band, NOT
	// --warn-only). Operator-required-config change: the band numbers come from the
	// operator's keeper: config, so the command carries NO product-default band — the
	// abs flags are OMITTED and the keeper reads .harmonik/config.yaml.
	if strings.Contains(kw.Command, "--warn-only") {
		t.Errorf("keeper window command = %q, must NOT carry --warn-only (D4: force-cut)", kw.Command)
	}
	if strings.Contains(kw.Command, "--warn-abs-tokens") || strings.Contains(kw.Command, "--act-abs-tokens") {
		t.Errorf("keeper window command = %q, must OMIT band flags (no product default; keeper reads operator config)", kw.Command)
	}
}

func argvHasFlag(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// containsPair reports whether ss contains flag immediately followed by val.
// When val is "", it reports whether flag is present at all.
func containsPair(ss []string, flag, val string) bool {
	for i, s := range ss {
		if s == flag {
			if val == "" {
				return true
			}
			return i+1 < len(ss) && ss[i+1] == val
		}
	}
	return false
}
