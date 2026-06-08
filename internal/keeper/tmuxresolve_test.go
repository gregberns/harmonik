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
	expected := keeper.HarmonikSessionName(dir, "orchestrator")
	stub := func(name string) bool { return name == expected }

	got := keeper.ResolveTmuxTarget(dir, "orchestrator", "", stub)
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
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
