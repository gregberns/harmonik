package keepertest_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

// TestL4_RealClaudeClearThenBrief proves the external coordination boundary.
// It starts an actual Claude Code TUI in tmux. It sends one clear, waits for
// the SessionStart hook to report a different session ID, and only then sends
// a resume message. This test uses a real model and is opt-in.
//
//nolint:gosec // Opt-in test runs fixed tmux and Claude argv.
func TestL4_RealClaudeClearThenBrief(t *testing.T) {
	if os.Getenv("KEEPER_LIVE_CLAUDE") != "1" {
		t.Skip("KEEPER_LIVE_CLAUDE=1 required for the real Claude scenario")
	}
	for _, name := range []string{"tmux", "claude"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Fatalf("%s not found on PATH: %v", name, err)
		}
	}

	artifacts := liveClaudeArtifactDir(t)
	hookLog := filepath.Join(artifacts, "session-starts.log")
	agent := "live-claude"
	sidPath := filepath.Join(artifacts, ".harmonik", "keeper", agent+".sid")
	if err := os.MkdirAll(filepath.Dir(sidPath), 0o700); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(artifacts, "session-start-hook.sh")
	settingsPath := filepath.Join(artifacts, "settings.json")
	launcherPath := filepath.Join(artifacts, "launch-claude.sh")
	writeLiveClaudeHook(t, hookPath, hookLog, sidPath)
	writeLiveClaudeSettings(t, settingsPath, hookPath)

	session := fmt.Sprintf("hk-keeper-claude-%d", os.Getpid())
	t.Cleanup(func() { l3KillSession(session) })
	initialSID := fmt.Sprintf("11111111-1111-4111-8111-%012x", time.Now().UnixNano()&0xffffffffffff)
	writeLiveClaudeLauncher(t, launcherPath, settingsPath, initialSID)
	cmd := exec.CommandContext(context.Background(), "tmux", "new-session", "-d", "-s", session, "bash", "--norc")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("start Claude tmux session: %v (%s)", err, out)
	}
	target := liveClaudePaneTarget(t, session)
	if out, err := exec.CommandContext(context.Background(), "tmux", "set-option", "-w", "-t", target, "remain-on-exit", "on").CombinedOutput(); err != nil {
		t.Fatalf("set remain-on-exit: %v (%s)", err, out)
	}
	sendTmuxLine(t, target, launcherPath)

	waitForSessionStarts(t, hookLog, 1, 45*time.Second)
	waitForPaneText(t, target, "❯", 30*time.Second, artifacts, "startup-pane.txt")
	time.Sleep(500 * time.Millisecond)
	nonce := fmt.Sprintf("KEEPER_LIVE_%d", time.Now().UnixNano())
	sendTmuxLine(t, target, "Reply with exactly READY_"+nonce)
	waitForPaneText(t, target, "READY_"+nonce, 90*time.Second, artifacts, "ready-pane.txt")

	err := keeper.DriveRestartAfterReturn(context.Background(), keeper.RestartDriveConfig{
		RestartNowConfig:  keeper.RestartNowConfig{ProjectDir: artifacts, AgentName: agent, TmuxTarget: target},
		PreviousSessionID: startsFirst(t, hookLog), Grace: time.Millisecond, Timeout: 45 * time.Second, Poll: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	starts := waitForSessionStarts(t, hookLog, 2, time.Second)
	if starts[0] == starts[len(starts)-1] {
		t.Fatalf("clear did not change the Claude session ID: %v", starts)
	}

	waitForPaneText(t, target, "agent brief", 30*time.Second, artifacts, "final-pane.txt")
	t.Logf("real Claude clear-to-brief scenario passed; artifacts: %s", artifacts)
}

func liveClaudeArtifactDir(t *testing.T) string {
	t.Helper()
	root := os.Getenv("KEEPER_LIVE_ARTIFACTS")
	if root == "" {
		root = os.TempDir()
	}
	dir, err := os.MkdirTemp(root, "harmonik-keeper-live-claude-")
	if err != nil {
		t.Fatalf("create artifact directory: %v", err)
	}
	return dir
}

func startsFirst(t *testing.T, path string) string {
	ids := readSessionStartIDs(t, path)
	if len(ids) != 1 {
		t.Fatalf("want one initial session, got %v", ids)
	}
	return ids[0]
}

func writeLiveClaudeHook(t *testing.T, hookPath, hookLog, sidPath string) {
	t.Helper()
	body := "#!/bin/sh\n" +
		"payload=$(sed -n '1p')\n" +
		"printf '%s\\n' \"$payload\" >> \"" + hookLog + "\"\n" +
		"printf '%s\\n' \"$payload\" | jq -r '.session_id' > \"" + sidPath + "\"\n"
	if err := os.WriteFile(hookPath, []byte(body), 0o700); err != nil { //nolint:gosec // Hook fixture must be executable.
		t.Fatalf("write SessionStart hook: %v", err)
	}
}

func writeLiveClaudeSettings(t *testing.T, settingsPath, hookPath string) {
	t.Helper()
	settings := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{map[string]any{
				"matcher": "",
				"hooks":   []any{map[string]any{"type": "command", "command": hookPath}},
			}},
		},
	}
	raw, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}

//nolint:gosec // Executable test fixture needs owner execute permission.
func writeLiveClaudeLauncher(t *testing.T, path, settingsPath, sessionID string) {
	t.Helper()
	body := "#!/bin/sh\nexec claude --dangerously-skip-permissions --model sonnet --effort low" +
		" --session-id " + sessionID + " --settings \"" + settingsPath + "\"\n"
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write Claude launcher: %v", err)
	}
}

//nolint:gosec // Fixed tmux argv with a generated test session.
func liveClaudePaneTarget(t *testing.T, session string) string {
	t.Helper()
	out, err := exec.CommandContext(context.Background(), "tmux", "list-panes", "-t", "="+session, "-F", "#{pane_id}").CombinedOutput()
	if err != nil {
		t.Fatalf("resolve tmux pane: %v (%s)", err, out)
	}
	target := strings.TrimSpace(string(out))
	if target == "" || strings.Contains(target, "\n") {
		t.Fatalf("expected one tmux pane, got %q", target)
	}
	return target
}

func sendTmuxLine(t *testing.T, target, line string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "tmux", "send-keys", "-t", target, "-l", line)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tmux send text: %v (%s)\n%s", err, out, capturePaneBestEffort(target))
	}
	if out, err := exec.CommandContext(context.Background(), "tmux", "send-keys", "-t", target, "Enter").CombinedOutput(); err != nil {
		t.Fatalf("tmux send Enter: %v (%s)", err, out)
	}
}

//nolint:errcheck // Diagnostic capture is intentionally best effort.
func capturePaneBestEffort(target string) string {
	pane, _ := exec.CommandContext(context.Background(), "tmux", "capture-pane", "-p", "-S", "-200", "-t", target).CombinedOutput()
	return string(pane)
}

func waitForSessionStarts(t *testing.T, path string, want int, timeout time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ids := readSessionStartIDs(t, path)
		if len(ids) >= want {
			return ids
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d SessionStart hooks in %s", want, path)
	return nil
}

func readSessionStartIDs(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // Test-owned artifact path.
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("open SessionStart log: %v", err)
	}
	defer f.Close()
	var ids []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var payload struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &payload); err == nil && payload.SessionID != "" {
			ids = append(ids, payload.SessionID)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan SessionStart log: %v", err)
	}
	return ids
}

//nolint:errcheck // Failure artifacts are best effort.
func waitForPaneText(t *testing.T, target, want string, timeout time.Duration, artifacts, captureName string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var pane []byte
	for time.Now().Before(deadline) {
		var err error
		pane, err = exec.CommandContext(context.Background(), "tmux", "capture-pane", "-p", "-S", "-200", "-t", target).CombinedOutput()
		if err == nil && strings.Contains(string(pane), want) {
			_ = os.WriteFile(filepath.Join(artifacts, captureName), pane, 0o600)
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	_ = os.WriteFile(filepath.Join(artifacts, captureName), pane, 0o600)
	t.Fatalf("timed out waiting for %q in pane; artifacts: %s", want, artifacts)
}
