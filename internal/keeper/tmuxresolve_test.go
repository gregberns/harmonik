package keeper_test

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/keeper"
)

// hashDir mirrors the hash formula used by HarmonikSessionName so tests can
// compute expected values without importing lifecycle.
func hashDir(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		resolved = dir
	}
	sum := sha256.Sum256([]byte(resolved))
	return fmt.Sprintf("%x", sum[:6])
}

func TestHarmonikSessionName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	want := "harmonik-" + hashDir(t, dir) + "-orchestrator"
	got := keeper.HarmonikSessionName(dir, "orchestrator")
	if got != want {
		t.Errorf("HarmonikSessionName: got %q, want %q", got, want)
	}
}

func TestHarmonikSessionName_DifferentAgents(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hash := hashDir(t, dir)
	cases := []struct {
		agent string
		want  string
	}{
		{"orchestrator", "harmonik-" + hash + "-orchestrator"},
		{"flywheel", "harmonik-" + hash + "-flywheel"},
		{"named-queues", "harmonik-" + hash + "-named-queues"},
	}
	for _, tc := range cases {
		got := keeper.HarmonikSessionName(dir, tc.agent)
		if got != tc.want {
			t.Errorf("agent=%q: got %q, want %q", tc.agent, got, tc.want)
		}
	}
}

func TestResolveTmuxTarget_ExplicitTakesPrecedence(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	called := false
	stub := func(_ string) bool { called = true; return true }

	got := keeper.ResolveTmuxTarget(dir, "orchestrator", "my:explicit:target", stub)
	if got != "my:explicit:target" {
		t.Errorf("got %q, want %q", got, "my:explicit:target")
	}
	if called {
		t.Error("sessionExistsFn should not be called when explicit is non-empty")
	}
}

func TestResolveTmuxTarget_DerivesFromConventionWhenSessionLive(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	session := keeper.HarmonikSessionName(dir, "orchestrator")
	// Liveness is probed against the bare SESSION name; the resolved target then
	// points at the AGENT window's active pane.
	stub := func(name string) bool { return name == session }

	got := keeper.ResolveTmuxTarget(dir, "orchestrator", "", stub)
	want := session + ":agent"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveTmuxTarget_ReturnsEmptyWhenSessionAbsent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stub := func(_ string) bool { return false }

	got := keeper.ResolveTmuxTarget(dir, "orchestrator", "", stub)
	if got != "" {
		t.Errorf("expected empty string when session absent, got %q", got)
	}
}

func TestResolveTmuxTarget_ReturnsEmptyForEmptyAgent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stub := func(_ string) bool { return true }

	got := keeper.ResolveTmuxTarget(dir, "", "", stub)
	if got != "" {
		t.Errorf("expected empty string for empty agentName, got %q", got)
	}
}

func TestResolveTmuxTarget_ReturnsEmptyForEmptyProjectDir(t *testing.T) {
	t.Parallel()

	stub := func(_ string) bool { return true }

	got := keeper.ResolveTmuxTarget("", "orchestrator", "", stub)
	if got != "" {
		t.Errorf("expected empty string for empty projectDir, got %q", got)
	}
}

// TestResolveTmuxTarget_ExplicitWindowTarget verifies that an explicit
// "session:window" --tmux value (e.g. "harmonik-<hash>-captain:agent") is
// passed through verbatim so the keeper injects/gauges against the named
// window's active pane — never its own sibling "keeper" window.
func TestResolveTmuxTarget_ExplicitWindowTarget(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	called := false
	stub := func(_ string) bool { called = true; return true }

	got := keeper.ResolveTmuxTarget(dir, "captain", "harmonik-abc-captain:agent", stub)
	if got != "harmonik-abc-captain:agent" {
		t.Errorf("got %q, want %q", got, "harmonik-abc-captain:agent")
	}
	if called {
		t.Error("sessionExistsFn should not be called when explicit is non-empty")
	}
	// And the resolved target splits back to (session, window=agent).
	session, window := keeper.SplitTmuxTarget(got)
	if session != "harmonik-abc-captain" || window != "agent" {
		t.Errorf("split(%q) = (%q, %q), want (harmonik-abc-captain, agent)", got, session, window)
	}
}

// TestResolveTmuxTarget_ExplicitSessionNoColonLegacy verifies that an explicit
// bare-session --tmux value (no colon) is preserved as-is — legacy
// session-active-pane behavior, so a half-migrated fleet does not break.
func TestResolveTmuxTarget_ExplicitSessionNoColonLegacy(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	got := keeper.ResolveTmuxTarget(dir, "captain", "harmonik-abc-captain", nil)
	if got != "harmonik-abc-captain" {
		t.Errorf("got %q, want %q (legacy session-active-pane)", got, "harmonik-abc-captain")
	}
	session, window := keeper.SplitTmuxTarget(got)
	if session != "harmonik-abc-captain" || window != "" {
		t.Errorf("split(%q) = (%q, %q), want (harmonik-abc-captain, \"\")", got, session, window)
	}
}

func TestSplitTmuxTarget(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in          string
		wantSession string
		wantWindow  string
	}{
		{"", "", ""},
		{"foo", "foo", ""},            // legacy: session only, no window
		{"foo:agent", "foo", "agent"}, // session:window
		{"harmonik-abc-captain:agent", "harmonik-abc-captain", "agent"},
		{"foo:agent.1", "foo", "agent.1"}, // session:window.pane round-trips
		{"foo:bar:baz", "foo", "bar:baz"}, // only the FIRST colon splits
		{":agent", "", "agent"},           // empty session, explicit window
	}
	for _, tc := range cases {
		session, window := keeper.SplitTmuxTarget(tc.in)
		if session != tc.wantSession || window != tc.wantWindow {
			t.Errorf("SplitTmuxTarget(%q) = (%q, %q), want (%q, %q)",
				tc.in, session, window, tc.wantSession, tc.wantWindow)
		}
	}
}

// TestHarmonikCrewSessionName verifies the crew-convention name formula.
func TestHarmonikCrewSessionName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	want := "harmonik-" + hashDir(t, dir) + "-crew-admiral"
	got := keeper.HarmonikCrewSessionName(dir, "admiral")
	if got != want {
		t.Errorf("HarmonikCrewSessionName: got %q, want %q", got, want)
	}
}

// TestHarmonikCrewSessionName_DifferentFromBare confirms the crew and bare forms
// differ only in the "crew-" infix — they share the same project hash.
func TestHarmonikCrewSessionName_DifferentFromBare(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hash := hashDir(t, dir)
	bare := keeper.HarmonikSessionName(dir, "admiral")
	crew := keeper.HarmonikCrewSessionName(dir, "admiral")
	wantBare := "harmonik-" + hash + "-admiral"
	wantCrew := "harmonik-" + hash + "-crew-admiral"
	if bare != wantBare {
		t.Errorf("bare: got %q, want %q", bare, wantBare)
	}
	if crew != wantCrew {
		t.Errorf("crew: got %q, want %q", crew, wantCrew)
	}
	if bare == crew {
		t.Errorf("bare and crew session names must differ: both %q", bare)
	}
}

// TestResolveTmuxTarget_CrewNaming_B4 is the RED→GREEN guard for B4 / hk-pp1in:
// restart-now aborted no_tmux_target for crew agents whose tmux session is named
// "harmonik-<hash>-crew-<name>" rather than the bare "harmonik-<hash>-<name>".
//
// The stub returns true ONLY for the crew-prefixed session name — exactly the
// live incident where the admiral ran in "harmonik-<hash>-crew-admiral" but
// ResolveTmuxTarget only checked "harmonik-<hash>-admiral" → empty → abort.
//
// Before the B4 fix: this test was RED (got == "").
// After the fix: GREEN (got == crewSession + ":agent").
func TestResolveTmuxTarget_CrewNaming_B4(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	crewSession := keeper.HarmonikCrewSessionName(dir, "admiral")

	// Only the crew-prefixed session is live; bare convention is absent.
	stub := func(name string) bool { return name == crewSession }

	got := keeper.ResolveTmuxTarget(dir, "admiral", "", stub)
	want := crewSession + ":agent"
	if got != want {
		t.Errorf("B4 crew-naming: got %q, want %q (no_tmux_target would fire)", got, want)
	}
}

// TestResolveTmuxTarget_BareFirstThenCrew verifies priority: when BOTH sessions
// exist, bare convention wins (captain wins over a hypothetical crew-captain).
func TestResolveTmuxTarget_BareFirstThenCrew(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	bareSession := keeper.HarmonikSessionName(dir, "captain")
	crewSession := keeper.HarmonikCrewSessionName(dir, "captain")

	// Both sessions are live — bare must be returned first.
	stub := func(name string) bool { return name == bareSession || name == crewSession }

	got := keeper.ResolveTmuxTarget(dir, "captain", "", stub)
	want := bareSession + ":agent"
	if got != want {
		t.Errorf("bare-first priority: got %q, want %q", got, want)
	}
}

// TestResolveTmuxTarget_CrewOnlyNoBare confirms that a crew agent resolves
// correctly when the bare session is absent (no false-empty from the bare miss).
func TestResolveTmuxTarget_CrewOnlyNoBare(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	crewSession := keeper.HarmonikCrewSessionName(dir, "jamis")

	stub := func(name string) bool { return name == crewSession }

	got := keeper.ResolveTmuxTarget(dir, "jamis", "", stub)
	want := crewSession + ":agent"
	if got != want {
		t.Errorf("crew-only: got %q, want %q", got, want)
	}
}
