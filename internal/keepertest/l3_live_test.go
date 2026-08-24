package keepertest_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

func skipUnlessKeeperLive(t *testing.T) {
	t.Helper()
	if os.Getenv("KEEPER_LIVE") != "1" {
		t.Skip("KEEPER_LIVE=1 required for L3 live tests (set env var to run)")
	}
}

func l3SessionName() string {
	return fmt.Sprintf("hk-keeper-l3-%d", os.Getpid())
}

func l3KillSession(name string) {
	_ = exec.CommandContext(context.Background(), "tmux", "kill-session", "-t", "="+name).Run() //nolint:errcheck,gosec // G204: test-local name; best-effort teardown
}

// TestL3_OneCycleTmuxSmoke is the keeper pre-deploy live smoke. A real tmux
// pane receives one clear. The driver observes session turnover, resets any
// queued input, and only then submits the resume brief.
func TestL3_OneCycleTmuxSmoke(t *testing.T) {
	skipUnlessKeeperLive(t)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatalf("L3: KEEPER_LIVE=1 but tmux not found on PATH: %v", err)
	}

	sessName := l3SessionName()
	t.Cleanup(func() { l3KillSession(sessName) })
	out, err := exec.CommandContext(context.Background(), "tmux", "new-session", "-d", "-s", sessName, "bash", "--norc").CombinedOutput() //nolint:gosec // G204: test-local session name
	if err != nil {
		t.Fatalf("L3: tmux new-session -s %q: %v (%s)", sessName, err, out)
	}

	project := t.TempDir()
	agent := fmt.Sprintf("keeper-l3-%d", os.Getpid())
	const primarySID = "aaaabbbb-cccc-4ddd-8eee-ffffffffffff" // valid lowercase UUIDv4

	keeperDir := filepath.Join(project, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o750); err != nil {
		t.Fatalf("L3: mkdir keeper dir: %v", err)
	}
	ctxLine := fmt.Sprintf(`{"pct":50,"session_id":%q,"ts":%q}`, primarySID, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(keeperDir, agent+".ctx"), []byte(ctxLine+"\n"), 0o600); err != nil {
		t.Fatalf("L3: write .ctx: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keeperDir, agent+".sid"), []byte(primarySID+"\n"), 0o600); err != nil {
		t.Fatalf("L3: write .sid: %v", err)
	}
	handoff := filepath.Join(project, "HANDOFF-"+agent+".md")
	if err := os.WriteFile(handoff, []byte("# HANDOFF keeper L3 smoke\n\nlive wire canary.\n"), 0o644); err != nil { //nolint:gosec // G306: readable handoff
		t.Fatalf("L3: write handoff: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const newSID = "bbbbcccc-dddd-4eee-8fff-aaaaaaaaaaaa"
	inject := func(ctx context.Context, target, text string) error {
		if err := keeper.InjectText(ctx, target, text); err != nil {
			return err
		}
		if text == "/clear" {
			return os.WriteFile(filepath.Join(keeperDir, agent+".sid"), []byte(newSID+"\n"), 0o600)
		}
		return nil
	}
	if err := keeper.DriveRestartAfterReturn(ctx, keeper.RestartDriveConfig{
		RestartNowConfig: keeper.RestartNowConfig{
			ProjectDir: project, AgentName: agent, TmuxTarget: sessName, Inject: inject,
		},
		PreviousSessionID: primarySID,
		Grace:             time.Millisecond,
		Timeout:           10 * time.Second,
		Poll:              10 * time.Millisecond,
	}); err != nil {
		t.Fatalf("L3: detached restart driver: %v", err)
	}

	pane, err := exec.CommandContext(context.Background(), "tmux", "capture-pane", "-p", "-t", sessName).CombinedOutput() //nolint:gosec // G204: test-local session name
	if err != nil {
		t.Fatalf("L3: capture-pane: %v (%s)", err, pane)
	}
	captured := string(pane)
	if !strings.Contains(captured, "/clear") {
		t.Errorf("L3: pane does not show the injected /clear:\n%s", captured)
	}
	if !strings.Contains(captured, "agent brief") {
		t.Errorf("L3: pane does not show the injected agent brief:\n%s", captured)
	}
	t.Logf("L3: one-cycle tmux smoke GREEN — one /clear, observed turnover, and later brief in pane %s", sessName)
}

//nolint:gosec,errcheck // Opt-in test runs fixed tmux argv with test-owned values.
func TestL3_ExternalTurnoverDuringGraceSendsNoDriverClear(t *testing.T) {
	skipUnlessKeeperLive(t)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatalf("L3: KEEPER_LIVE=1 but tmux not found on PATH: %v", err)
	}

	sessName := l3SessionName() + "-external"
	t.Cleanup(func() { l3KillSession(sessName) })
	if out, err := exec.CommandContext(t.Context(), "tmux", "new-session", "-d", "-s", sessName, "bash", "--norc").CombinedOutput(); err != nil {
		t.Fatalf("L3: tmux new-session: %v (%s)", err, out)
	}

	project := t.TempDir()
	agent := "keeper-l3-external"
	const oldSID = "11111111-1111-4111-8111-111111111111"
	const newSID = "22222222-2222-4222-8222-222222222222"
	keeperDir := filepath.Join(project, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keeperDir, agent+".sid"), []byte(oldSID+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(keeperDir, agent+".sid"), []byte(newSID+"\n"), 0o600)
	}()

	if err := keeper.DriveRestartAfterReturn(t.Context(), keeper.RestartDriveConfig{
		RestartNowConfig: keeper.RestartNowConfig{
			ProjectDir: project, AgentName: agent, TmuxTarget: sessName,
		},
		PreviousSessionID: oldSID,
		Grace:             100 * time.Millisecond,
		Timeout:           10 * time.Second,
		Poll:              10 * time.Millisecond,
	}); err != nil {
		t.Fatal(err)
	}

	pane, err := exec.CommandContext(t.Context(), "tmux", "capture-pane", "-p", "-t", sessName).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	captured := string(pane)
	if strings.Contains(captured, "/clear") {
		t.Fatalf("driver submitted a clear after external turnover:\n%s", captured)
	}
	if !strings.Contains(captured, "agent brief") {
		t.Fatalf("driver did not brief the new session:\n%s", captured)
	}
}
