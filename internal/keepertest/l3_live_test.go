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

// TestL3_OneCycleTmuxSmoke is the keeper pre-deploy live smoke: a real tmux
// pane receives the full scripted restart-cycle injection sequence.
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
	nonce := fmt.Sprintf("l3-smoke-%d", os.Getpid())
	if err := keeper.RestartNow(ctx, keeper.RestartNowConfig{
		ProjectDir:  project,
		AgentName:   agent,
		TmuxTarget:  sessName,
		RequestedAt: time.Now(),
	}, nonce); err != nil {
		t.Fatalf("L3: RestartNow (live injection): %v", err)
	}

	pane, err := exec.CommandContext(context.Background(), "tmux", "capture-pane", "-p", "-t", sessName).CombinedOutput() //nolint:gosec // G204: test-local session name
	if err != nil {
		t.Fatalf("L3: capture-pane: %v (%s)", err, pane)
	}
	captured := string(pane)
	if !strings.Contains(captured, nonce) {
		t.Errorf("L3: pane does not show the ACK nonce %q:\n%s", nonce, captured)
	}
	if !strings.Contains(captured, "/clear") {
		t.Errorf("L3: pane does not show the injected /clear:\n%s", captured)
	}
	if !strings.Contains(captured, "agent brief") {
		t.Errorf("L3: pane does not show the injected agent brief:\n%s", captured)
	}
	t.Logf("L3: one-cycle tmux smoke GREEN — ACK/nonce, /clear, brief all landed in pane %s", sessName)
}
