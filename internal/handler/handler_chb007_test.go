package handler_test

// handler_chb007_test.go — wiring-level tests for CHB-007 enforcement in
// Handler.Launch (specs/claude-hook-bridge.md §4.2 CHB-007).
//
// These tests verify that Handler.Launch itself refuses to start any subprocess
// (exec.CommandContext or substrate path) when forbidden flags or env vars are
// present in the LaunchSpec.  The per-function unit tests for CheckForbiddenFlags
// live in claudehandler_chb006_024_test.go; this file tests the call-site wiring.
//
// Helper prefix: launchCHB007Fixture (per implementer-protocol.md §Helper-prefix
// discipline).

import (
	"context"
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// launchCHB007FixtureHandler constructs a Handler with minimal stub dependencies
// sufficient for CHB-007 wiring tests.  The handler never reaches subprocess-start
// because CheckForbiddenFlags fires first.
func launchCHB007FixtureHandler(t *testing.T) handler.Handler {
	t.Helper()
	pub := &handlercontract.CollectingEmitter{}
	dl := handlercontract.NoopWatcherDeadLetter{}
	reg := handlercontract.NewAdapterRegistry()
	return handler.NewHandler(pub, dl, reg)
}

// TestLaunch_CHB007_ForbiddenFlag_ForkSession verifies that Launch returns an
// ErrStructural-wrapping error when --fork-session is in spec.Args, without
// starting any subprocess.
func TestLaunch_CHB007_ForbiddenFlag_ForkSession(t *testing.T) {
	t.Parallel()
	h := launchCHB007FixtureHandler(t)
	spec := handler.LaunchSpec{
		Binary:  "/usr/bin/true", // must not be executed
		Args:    []string{"--fork-session"},
		WorkDir: t.TempDir(),
	}
	_, _, err := h.Launch(context.Background(), spec)
	if err == nil {
		t.Fatal("Launch with --fork-session: want error, got nil")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error does not wrap ErrStructural: %v", err)
	}
}

// TestLaunch_CHB007_ForbiddenFlag_Bare verifies that Launch returns an
// ErrStructural-wrapping error when --bare is in spec.Args.
func TestLaunch_CHB007_ForbiddenFlag_Bare(t *testing.T) {
	t.Parallel()
	h := launchCHB007FixtureHandler(t)
	spec := handler.LaunchSpec{
		Binary:  "/usr/bin/true",
		Args:    []string{"--bare"},
		WorkDir: t.TempDir(),
	}
	_, _, err := h.Launch(context.Background(), spec)
	if err == nil {
		t.Fatal("Launch with --bare: want error, got nil")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error does not wrap ErrStructural: %v", err)
	}
}

// TestLaunch_CHB007_ForbiddenFlag_NoSessionPersistence verifies that Launch
// returns an ErrStructural-wrapping error when --no-session-persistence is in
// spec.Args.
func TestLaunch_CHB007_ForbiddenFlag_NoSessionPersistence(t *testing.T) {
	t.Parallel()
	h := launchCHB007FixtureHandler(t)
	spec := handler.LaunchSpec{
		Binary:  "/usr/bin/true",
		Args:    []string{"--no-session-persistence"},
		WorkDir: t.TempDir(),
	}
	_, _, err := h.Launch(context.Background(), spec)
	if err == nil {
		t.Fatal("Launch with --no-session-persistence: want error, got nil")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error does not wrap ErrStructural: %v", err)
	}
}

// TestLaunch_CHB007_ForbiddenEnvVar_SkipPromptHistory verifies that Launch
// returns an ErrStructural-wrapping error when CLAUDE_CODE_SKIP_PROMPT_HISTORY
// appears in spec.Env.
func TestLaunch_CHB007_ForbiddenEnvVar_SkipPromptHistory(t *testing.T) {
	t.Parallel()
	h := launchCHB007FixtureHandler(t)
	spec := handler.LaunchSpec{
		Binary:  "/usr/bin/true",
		WorkDir: t.TempDir(),
		Env:     []string{"PATH=/usr/bin", "CLAUDE_CODE_SKIP_PROMPT_HISTORY=1"},
	}
	_, _, err := h.Launch(context.Background(), spec)
	if err == nil {
		t.Fatal("Launch with CLAUDE_CODE_SKIP_PROMPT_HISTORY: want error, got nil")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error does not wrap ErrStructural: %v", err)
	}
}

// TestLaunch_CHB007_SubstratePath_ForbiddenFlag verifies that the substrate
// path (spec.Substrate != nil) also rejects forbidden flags before
// SpawnWindow is called.
func TestLaunch_CHB007_SubstratePath_ForbiddenFlag(t *testing.T) {
	t.Parallel()
	h := launchCHB007FixtureHandler(t)

	// fakeSubstrate from substrate_hkgql2011_test.go is not visible here
	// (same package but different file); use an inline stub.
	stub := &chb007FakeSubstrate{}
	spec := handler.LaunchSpec{
		Binary:    "/usr/bin/true",
		Args:      []string{"--bare"},
		WorkDir:   t.TempDir(),
		Substrate: stub,
	}
	_, _, err := h.Launch(context.Background(), spec)
	if err == nil {
		t.Fatal("Launch via substrate with --bare: want error, got nil")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error does not wrap ErrStructural: %v", err)
	}
	// SpawnWindow must NOT have been called — the guard fires before substrate dispatch.
	if stub.spawnCalled {
		t.Error("SpawnWindow was called despite forbidden flag; guard must fire before substrate dispatch")
	}
}

// chb007FakeSubstrate is a minimal Substrate stub for CHB-007 wiring tests.
// It records whether SpawnWindow was called; the test expects it NOT to be called.
type chb007FakeSubstrate struct {
	spawnCalled bool
}

func (f *chb007FakeSubstrate) SpawnWindow(_ context.Context, _ handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	f.spawnCalled = true
	return nil, nil
}

// Compile-time assertion: chb007FakeSubstrate satisfies handler.Substrate.
var _ handler.Substrate = (*chb007FakeSubstrate)(nil)
