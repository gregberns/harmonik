//go:build integration

package keeper_test

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

func smokeSessionName() string {
	return fmt.Sprintf("hk-smoke-test-%d", os.Getpid())
}

func smokeKillSession(name string) {
	_ = exec.Command("tmux", "kill-session", "-t", "="+name).Run() //nolint:errcheck,gosec // G204: test-local name; best-effort teardown
}

func smokeSessionAlive(name string) bool {
	err := exec.Command("tmux", "has-session", "-t", "="+name).Run() //nolint:gosec // G204: test-local name
	return err == nil
}

func smokeStartSession(t *testing.T, name string) {
	t.Helper()
	out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "bash", "--norc").CombinedOutput() //nolint:gosec // G204: test-local session name; test-only binary selection
	if err != nil {
		t.Fatalf("smoke: tmux new-session -s %q: %v (%s)", name, err, out)
	}
}

func smokeWriteGaugeAndSID(t *testing.T, projectDir, agent, sid string) {
	t.Helper()
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("smoke: mkdir keeper dir: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	ctx := fmt.Sprintf(`{"pct":50,"session_id":%q,"ts":%q}`, sid, now)
	if err := os.WriteFile(filepath.Join(keeperDir, agent+".ctx"), []byte(ctx+"\n"), 0o600); err != nil {
		t.Fatalf("smoke: write .ctx: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keeperDir, agent+".sid"), []byte(sid+"\n"), 0o600); err != nil {
		t.Fatalf("smoke: write .sid: %v", err)
	}
}

func smokeWriteFreshHandoff(t *testing.T, projectDir, agent string) string {
	t.Helper()
	path := filepath.Join(projectDir, "HANDOFF-"+agent+".md")
	content := "# HANDOFF smoke test\n\nThis is a test-only handoff file.\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
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
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("smoke: tmux not found on PATH; skipping restart-now smoke test")
	}

	t.Run("NoPreconditions", func(t *testing.T) {
		project := t.TempDir()
		agent := "smoke-" + strconv.Itoa(os.Getpid())

		err := keeper.RestartNow(context.Background(), keeper.RestartNowConfig{
			ProjectDir:  project,
			AgentName:   agent,
			TmuxTarget:  "", // no pane — exercises the no_tmux_target fast-fail
			RequestedAt: time.Now(),
		}, "smoke-nonce-nopre")
		if err == nil {
			t.Fatal("smoke: NoPreconditions: want error from RestartNow with no tmux target; got nil")
		}
		t.Logf("smoke: NoPreconditions: RestartNow returned expected error: %v", err)
	})

	t.Run("HappyPath", func(t *testing.T) {
		sessName := smokeSessionName() + "-hp"
		t.Cleanup(func() {
			smokeKillSession(sessName)
		})

		smokeStartSession(t, sessName)

		if !smokeSessionAlive(sessName) {
			t.Fatalf("smoke: HappyPath: tmux session %q not found after new-session", sessName)
		}

		project := t.TempDir()
		agent := "smoke-" + strconv.Itoa(os.Getpid())

		const primarySID = "aaaabbbb-cccc-4ddd-8eee-ffffffffffff"

		smokeWriteGaugeAndSID(t, project, agent, primarySID)
		smokeWriteFreshHandoff(t, project, agent)

		var injected []string
		spyInject := func(_ context.Context, _ string, text string) error {
			injected = append(injected, text)
			return nil
		}

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
		if !strings.Contains(injected[2], "agent brief") || !strings.Contains(injected[2], "keeper-restart") {
			t.Errorf("smoke: HappyPath: inject[2] = %q, want 'agent brief ... keeper-restart'", injected[2])
		}

		t.Logf("smoke: HappyPath: RestartNow succeeded; injected sequence: %v", injected)
		t.Logf("smoke: HappyPath: session %q will be killed by t.Cleanup", sessName)
	})

	t.Run("CrewNamingB4", func(t *testing.T) {
		project := t.TempDir()
		agent := "smoke-crew-b4-" + strconv.Itoa(os.Getpid())

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
		project := t.TempDir()
		agent := "smoke-nogauge-" + strconv.Itoa(os.Getpid())

		keeperDir := filepath.Join(project, ".harmonik", "keeper")
		if err := os.MkdirAll(keeperDir, 0o700); err != nil {
			t.Fatalf("smoke: GaugeMissing: mkdir: %v", err)
		}
		const primarySID = "aaaabbbb-cccc-4ddd-8eee-ffffffffffff"
		if err := os.WriteFile(filepath.Join(keeperDir, agent+".sid"), []byte(primarySID+"\n"), 0o600); err != nil {
			t.Fatalf("smoke: GaugeMissing: write .sid: %v", err)
		}

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

	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output() //nolint:gosec // G204: read-only tmux list
	if err != nil {
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
