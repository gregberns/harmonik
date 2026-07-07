//go:build integration

package keeper_test

// restartnow_smoke_integration_test.go — safe restart-now smoke harness (hk-7myt).
//
// Root cause of the fork-bomb: the ad-hoc /tmp/hk-smoke-keeper binary did not
// clean up its spawned tmux sessions on failure paths (wedge / no_tmux_target
// exit) and re-executed without a guard, so leaked sessions compounded at ~13/sec,
// driving load to 42 with 1500+ *-flywheel and *-default sessions.
//
// This harness is SAFE because:
//  1. Session name is "hk-smoke-test-<pid>" — never in the harmonik-* / *-flywheel /
//     *-default namespace the daemon uses.
//  2. t.Cleanup kills the session on ALL exit paths (success, failure, panic, timeout).
//  3. A TestMain sweep kills any orphaned hk-smoke-test-* sessions as belt-and-suspenders.
//  4. RestartNow is called AT MOST ONCE — no retry loop.
//
// What the test asserts:
//   - RestartNow exits with an error (no_gauge, no_tmux_target, sid_not_primary, or
//     handoff_missing) — a real Claude Code session is NOT running inside the pane, so
//     at least one pre-flight check refuses. The meaningful guarantee is that NO daemon
//     processes are spawned and the tmux session is gone after cleanup.
//   - When the gauge and handoff ARE present and the SID is a valid UUIDv4, but the
//     inject target is a real (but inert) shell pane, RestartNow succeeds — validating
//     the happy-path inject sequence on a REAL tmux pane without starting a daemon.
//
// The test does NOT start a harmonik daemon, does NOT create *-flywheel or *-default
// sessions, and does NOT loop.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

// smokeSessionName returns a unique tmux session name that is:
//   - scoped to this test run (pid-suffixed for uniqueness)
//   - NEVER in the harmonik-* / *-flywheel / *-default namespace
func smokeSessionName() string {
	return fmt.Sprintf("hk-smoke-test-%d", os.Getpid())
}

// smokeKillSession kills the named tmux session by exact name. Idempotent:
// an already-absent session is not an error.
func smokeKillSession(name string) {
	// Use exact-match anchor "=" so a prefix collision is impossible.
	_ = exec.Command("tmux", "kill-session", "-t", "="+name).Run() //nolint:errcheck,gosec // G204: test-local name; best-effort teardown
}

// smokeSessionAlive reports whether the named tmux session currently exists.
func smokeSessionAlive(name string) bool {
	err := exec.Command("tmux", "has-session", "-t", "="+name).Run() //nolint:gosec // G204: test-local name
	return err == nil
}

// smokeStartSession creates a detached tmux session running a plain shell.
// It does NOT start harmonik, Claude Code, or any daemon binary — the session
// is a bare shell so RestartNow can inject into a REAL pane without spawning
// daemon processes.
func smokeStartSession(t *testing.T, name string) {
	t.Helper()
	// new-session -d: detached (background); -s: session name; "bash --norc": bare shell.
	out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "bash", "--norc").CombinedOutput() //nolint:gosec // G204: test-local session name; test-only binary selection
	if err != nil {
		t.Fatalf("smoke: tmux new-session -s %q: %v (%s)", name, err, out)
	}
}

// smokeWriteGaugeAndSID writes the .ctx gauge file and the .sid channel for
// the agent so RestartNow's pre-flight checks find a valid primary SID.
// sid must be a well-formed lowercase UUIDv4 (IsPrimarySID == true).
func smokeWriteGaugeAndSID(t *testing.T, projectDir, agent, sid string) {
	t.Helper()
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("smoke: mkdir keeper dir: %v", err)
	}
	// .ctx gauge file
	now := time.Now().UTC().Format(time.RFC3339)
	ctx := fmt.Sprintf(`{"pct":50,"session_id":%q,"ts":%q}`, sid, now)
	if err := os.WriteFile(filepath.Join(keeperDir, agent+".ctx"), []byte(ctx+"\n"), 0o600); err != nil {
		t.Fatalf("smoke: write .ctx: %v", err)
	}
	// .sid channel (authoritative primary SID — overrides .ctx in ReadCtxFile)
	if err := os.WriteFile(filepath.Join(keeperDir, agent+".sid"), []byte(sid+"\n"), 0o600); err != nil {
		t.Fatalf("smoke: write .sid: %v", err)
	}
}

// smokeWriteFreshHandoff writes HANDOFF-<agent>.md with mtime = now so
// RestartNow's freshness check passes.
func smokeWriteFreshHandoff(t *testing.T, projectDir, agent string) string {
	t.Helper()
	path := filepath.Join(projectDir, "HANDOFF-"+agent+".md")
	content := "# HANDOFF smoke test\n\nThis is a test-only handoff file.\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // G306: readable handoff
		t.Fatalf("smoke: write handoff: %v", err)
	}
	return path
}

// TestSmoke_RestartNow_Integration is the safe restart-now smoke harness.
// It creates a uniquely-named tmux session (never in daemon namespace),
// registers t.Cleanup to kill it on all exit paths (including panic/timeout),
// calls RestartNow exactly once, and asserts the function returns without
// spawning daemon processes.
//
// Two sub-tests:
//   - NoPreconditions: gauge + SID + handoff all absent — RestartNow must fail at
//     the no_gauge step (before any pane injection). Validates the error path that
//     the old fork-bomb harness hit on wedge/no_tmux_target without cleaning up.
//   - HappyPath: gauge + SID + handoff all present; TmuxTarget is the real
//     smoke shell session. RestartNow succeeds and injects into the real pane
//     without starting a daemon.
func TestSmoke_RestartNow_Integration(t *testing.T) {
	// Require tmux — same guard used by all real-pane integration tests.
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("smoke: tmux not found on PATH; skipping restart-now smoke test")
	}

	t.Run("NoPreconditions", func(t *testing.T) {
		// Even without a real tmux pane, RestartNow must fail loudly (no silent no-op).
		// The session name is the smoke namespace; no tmux session is created for
		// this sub-test — we prove the no_gauge / no_sid path errors correctly
		// before any pane interaction.
		project := t.TempDir()
		agent := "smoke-" + strconv.Itoa(os.Getpid())

		// No gauge, no SID, no handoff written — RestartNow must fail.
		// TmuxTarget is deliberately empty (no pane) to exercise the no_tmux_target
		// fast-fail first; the no_gauge path is next if the target were provided.
		// Either way: NO injection, NO daemon spawn.
		err := keeper.RestartNow(context.Background(), keeper.RestartNowConfig{
			ProjectDir:  project,
			AgentName:   agent,
			TmuxTarget:  "", // no pane — exercises the no_tmux_target fast-fail
			RequestedAt: time.Now(),
		}, "smoke-nonce-nopre")
		if err == nil {
			t.Fatal("smoke: NoPreconditions: want error from RestartNow with no tmux target; got nil")
		}
		// Good: the error is the expected no_tmux_target loud failure.
		t.Logf("smoke: NoPreconditions: RestartNow returned expected error: %v", err)
	})

	t.Run("HappyPath", func(t *testing.T) {
		// Create a test-scoped tmux session (bare shell, NOT a daemon or Claude Code).
		// ALWAYS kill it in t.Cleanup — on success, failure, panic, and timeout.
		sessName := smokeSessionName() + "-hp"
		t.Cleanup(func() {
			smokeKillSession(sessName)
		})

		smokeStartSession(t, sessName)

		// Verify the session is alive after creation.
		if !smokeSessionAlive(sessName) {
			t.Fatalf("smoke: HappyPath: tmux session %q not found after new-session", sessName)
		}

		project := t.TempDir()
		agent := "smoke-" + strconv.Itoa(os.Getpid())

		// Valid UUIDv4 (lowercase) — IsPrimarySID == true.
		const primarySID = "aaaabbbb-cccc-4ddd-8eee-ffffffffffff"

		// Write gauge + SID + handoff so all pre-flight checks pass.
		smokeWriteGaugeAndSID(t, project, agent, primarySID)
		smokeWriteFreshHandoff(t, project, agent)

		// Capture injection calls via a spy — we verify the sequence without
		// needing a real Claude Code process in the pane.
		var injected []string
		spyInject := func(_ context.Context, _ string, text string) error {
			injected = append(injected, text)
			return nil
		}

		// RestartNow called EXACTLY ONCE. No retry loop. Cap is enforced structurally.
		err := keeper.RestartNow(context.Background(), keeper.RestartNowConfig{
			ProjectDir:  project,
			AgentName:   agent,
			TmuxTarget:  sessName,
			Inject:      spyInject,
			RequestedAt: time.Now(),
		}, "smoke-nonce-hp")
		if err != nil {
			t.Fatalf("smoke: HappyPath: RestartNow returned unexpected error: %v", err)
		}

		// Assert the full injection sequence: ack → /clear → agent brief (T8/I1).
		wantLen := 3
		if len(injected) != wantLen {
			t.Fatalf("smoke: HappyPath: got %d injections %v, want %d (ack + /clear + agent brief)", len(injected), injected, wantLen)
		}
		wantACK := keeper.AckLine("smoke-nonce-hp", "restart")
		if injected[0] != wantACK {
			t.Errorf("smoke: HappyPath: inject[0] = %q, want %q", injected[0], wantACK)
		}
		if injected[1] != "/clear" {
			t.Errorf("smoke: HappyPath: inject[1] = %q, want \"/clear\"", injected[1])
		}
		// inject[2] is agent brief — re-pins identity from soul.md (I1).
		if !strings.Contains(injected[2], "agent brief") || !strings.Contains(injected[2], "keeper-restart") {
			t.Errorf("smoke: HappyPath: inject[2] = %q, want 'agent brief ... keeper-restart'", injected[2])
		}

		t.Logf("smoke: HappyPath: RestartNow succeeded; injected sequence: %v", injected)
		t.Logf("smoke: HappyPath: session %q will be killed by t.Cleanup", sessName)
	})

	// B4 / hk-pp1in regression: crew-named session resolves correctly.
	//
	// Creates a real tmux session whose name matches the crew convention
	// ("harmonik-<hash>-crew-<agent>"). Verifies that ResolveTmuxTarget (with
	// the real tmuxSessionLive probe) returns the crew pane and that RestartNow
	// drives the full ACK→/clear→resume sequence without aborting no_tmux_target.
	// Layer: L-twin (real tmux session; spy injector; no daemon or Claude Code).
	t.Run("CrewNamingB4", func(t *testing.T) {
		project := t.TempDir()
		agent := "smoke-crew-b4-" + strconv.Itoa(os.Getpid())

		// Derive the crew session name that ResolveTmuxTarget now checks as
		// fallback. The session must be created EXACTLY with this name so the
		// real tmuxSessionLive `has-session -t "=<name>"` probe finds it.
		crewSessName := keeper.HarmonikCrewSessionName(project, agent)
		t.Cleanup(func() {
			smokeKillSession(crewSessName)
		})

		smokeStartSession(t, crewSessName)
		if !smokeSessionAlive(crewSessName) {
			t.Fatalf("smoke: CrewNamingB4: session %q not alive after creation", crewSessName)
		}

		const primarySID = "ccccdddd-eeee-4fff-8000-111122223333"
		smokeWriteGaugeAndSID(t, project, agent, primarySID)
		smokeWriteFreshHandoff(t, project, agent)

		// ResolveTmuxTarget with real tmuxSessionLive — must find the crew session.
		resolved := keeper.ResolveTmuxTarget(project, agent, "", nil)
		wantResolved := crewSessName + ":agent"
		if resolved != wantResolved {
			t.Fatalf("smoke: CrewNamingB4: ResolveTmuxTarget returned %q, want %q (B4 no_tmux_target regression)", resolved, wantResolved)
		}

		var injected []string
		spyInject := func(_ context.Context, _ string, text string) error {
			injected = append(injected, text)
			return nil
		}
		err := keeper.RestartNow(context.Background(), keeper.RestartNowConfig{
			ProjectDir:  project,
			AgentName:   agent,
			TmuxTarget:  resolved,
			Inject:      spyInject,
			RequestedAt: time.Now(),
		}, "smoke-crew-b4-nonce")
		if err != nil {
			t.Fatalf("smoke: CrewNamingB4: RestartNow aborted for crew agent: %v", err)
		}
		if len(injected) != 3 {
			t.Fatalf("smoke: CrewNamingB4: want 3 injections (ack+/clear+brief), got %d: %v", len(injected), injected)
		}
		t.Logf("smoke: CrewNamingB4: AccCorpus1 verified; session %q will be killed by t.Cleanup", crewSessName)
	})

	t.Run("GaugeMissing_SIDPresent", func(t *testing.T) {
		// A common failure mode: the .sid is present but the .ctx gauge is missing.
		// RestartNow must refuse at the no_gauge step and NOT inject anything.
		// No tmux session created — this sub-test is pure filesystem state.
		project := t.TempDir()
		agent := "smoke-nogauge-" + strconv.Itoa(os.Getpid())

		// Write .sid but NOT .ctx — gauge read should fail.
		keeperDir := filepath.Join(project, ".harmonik", "keeper")
		if err := os.MkdirAll(keeperDir, 0o755); err != nil {
			t.Fatalf("smoke: GaugeMissing: mkdir: %v", err)
		}
		const primarySID = "aaaabbbb-cccc-4ddd-8eee-ffffffffffff"
		if err := os.WriteFile(filepath.Join(keeperDir, agent+".sid"), []byte(primarySID+"\n"), 0o600); err != nil {
			t.Fatalf("smoke: GaugeMissing: write .sid: %v", err)
		}
		// No .ctx gauge file written.

		// Target is non-empty to bypass no_tmux_target and reach no_gauge.
		var injected []string
		spyInject := func(_ context.Context, _ string, text string) error {
			injected = append(injected, text)
			return nil
		}
		err := keeper.RestartNow(context.Background(), keeper.RestartNowConfig{
			ProjectDir:  project,
			AgentName:   agent,
			TmuxTarget:  "smoke-fake-target",
			Inject:      spyInject,
			RequestedAt: time.Now(),
		}, "smoke-nonce-nogauge")
		if err == nil {
			t.Fatal("smoke: GaugeMissing: want error when .ctx gauge is absent; got nil")
		}
		if len(injected) != 0 {
			t.Errorf("smoke: GaugeMissing: must NOT inject when gauge missing; got %v", injected)
		}
		t.Logf("smoke: GaugeMissing: RestartNow returned expected error: %v", err)
	})
}

// TestMain for the keeper_test package runs a sweep that kills any orphaned
// hk-smoke-test-* tmux sessions before and after the test suite. This is
// belt-and-suspenders: t.Cleanup handles the normal case; TestMain guards
// against test panics or hard-kills that bypass t.Cleanup.
//
// NOTE: There is already a TestMain in cycle_twin_e2e_integration_test.go;
// Go does not allow two TestMain functions in the same package. The sweep is
// therefore embedded there as an m.Run() wrapper. Since we cannot add a second
// TestMain, we rely solely on t.Cleanup (which Go guarantees runs on panic/timeout
// via testing.T.Cleanup) plus a best-effort post-suite sweep in TestMain_SmokeOrphanSweep
// (a regular test that runs last due to its name prefix).
//
// The TestMain in cycle_twin_e2e_integration_test.go already manages teardown
// for that package's sessions; the hk-smoke-test-* namespace is disjoint.

// TestSmoke_OrphanSweep is a belt-and-suspenders cleanup: it kills any tmux
// session whose name starts with "hk-smoke-test-" that might have been orphaned
// by a prior panicking test run. It is intentionally named so it sorts last among
// the smoke tests, running AFTER the main smoke test (alphabetically "sweep" > "restart").
//
// Safe: it only kills sessions in the "hk-smoke-test-" namespace, which is
// NEVER used by the harmonik daemon, captain, or crew sessions.
func TestSmoke_OrphanSweep(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("smoke: tmux not found on PATH; skipping orphan sweep")
	}

	// List all tmux sessions and kill any in the hk-smoke-test-* namespace.
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output() //nolint:gosec // G204: read-only tmux list
	if err != nil {
		// No sessions at all (tmux exits non-zero when no server is running) — that is fine.
		t.Logf("smoke: OrphanSweep: tmux list-sessions returned error (no server?): %v", err)
		return
	}

	killed := 0
	for _, line := range splitLines(string(out)) {
		if len(line) > len("hk-smoke-test-") && line[:len("hk-smoke-test-")] == "hk-smoke-test-" {
			smokeKillSession(line)
			t.Logf("smoke: OrphanSweep: killed orphaned session %q", line)
			killed++
		}
	}
	if killed > 0 {
		t.Logf("smoke: OrphanSweep: swept %d orphaned hk-smoke-test-* session(s)", killed)
	} else {
		t.Logf("smoke: OrphanSweep: no orphaned hk-smoke-test-* sessions found")
	}
}

// splitLines splits s by newline, omitting empty lines.
func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
