package main

// captain_respawn_hkz1rj_test.go — ES3 (hk-z1rj) coverage for the NATIVE-Go
// `harmonik captain respawn` subcommand and the captain.go --respawn-cmd arming.
//
// Re-authors the dead-pane self-heal coverage as ARGV tests (review E). The old
// bash-grep respawn test was retired alongside the bash captain launcher (ES8 /
// hk-877k); these tests assert the Go behavior directly and do NOT depend on any
// shell script existing.
//
// Asserted invariants (hk-opuv / hk-z036):
//   - respawn-window -k targeting the AGENT window "<sess>:agent" (only the agent
//     window, never the whole session; keeper window survives; no dup keeper).
//   - --resume <sid>, NOT --session-id (resume keeps the SAME conversation).
//   - captain.pid is refreshed from the new agent pane PID (display-message read).
//   - captain.go arms the keeper --respawn-cmd with the correct invocation string.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

const respawnTestSID = "11111111-2222-4333-8444-555555555555"

// captureRespawnRun returns a run func that records every *exec.Cmd it is handed
// (in order) and feeds back a fixed pane-pid for the display-message read. The
// first call is respawn-window; the second is display-message.
func captureRespawnRun(panePID string) (run captainRespawnRunFn, captured *[]*exec.Cmd) {
	var cmds []*exec.Cmd
	run = func(cmd *exec.Cmd) ([]byte, error) {
		cmds = append(cmds, cmd)
		// Only the display-message read consumes stdout; return the pid for both
		// (respawn-window's bytes are ignored by the caller).
		return []byte(panePID + "\n"), nil
	}
	return run, &cmds
}

func joinArgv(cmd *exec.Cmd) string { return strings.Join(cmd.Args, " ") }

// TestCaptainRespawn_RespawnsAgentWindowWithResume is the core argv assertion:
// respawn-window -k on the AGENT window, with --resume <sid> (NOT --session-id).
func TestCaptainRespawn_RespawnsAgentWindowWithResume_hkz1rj(t *testing.T) {
	proj := t.TempDir()
	run, captured := captureRespawnRun("9988")
	rc := runCaptainRespawn(
		[]string{
			"--name", "captain", "--tmux", "harmonik-abc123-captain:agent",
			"--session-id", respawnTestSID, "--project", proj,
		},
		run, os.Stdout, os.Stderr,
	)
	if rc != 0 {
		t.Fatalf("runCaptainRespawn rc=%d, want 0", rc)
	}
	if len(*captured) != 2 {
		t.Fatalf("expected 2 tmux commands (respawn-window, display-message), got %d", len(*captured))
	}

	respawn := joinArgv((*captured)[0])
	// respawn-window -k targeting the agent window.
	if !strings.Contains(respawn, "tmux respawn-window -k") {
		t.Errorf("first command is not `tmux respawn-window -k`: %q", respawn)
	}
	if !strings.Contains(respawn, "-t harmonik-abc123-captain:agent") {
		t.Errorf("respawn does not target the agent window `<sess>:agent`: %q", respawn)
	}
	// --resume <sid>, NOT --session-id: forking a new conversation is the bug.
	if !strings.Contains(respawn, "--resume "+respawnTestSID) {
		t.Errorf("respawn must use --resume <sid>: %q", respawn)
	}
	if strings.Contains(respawn, "--session-id") {
		t.Errorf("respawn must NOT use --session-id (would fork a new conversation): %q", respawn)
	}
	if !strings.Contains(respawn, "--remote-control captain") {
		t.Errorf("respawn missing --remote-control captain: %q", respawn)
	}
	if !strings.Contains(respawn, "HARMONIK_AGENT=captain") {
		t.Errorf("respawn missing -e HARMONIK_AGENT=captain: %q", respawn)
	}
}

// TestCaptainRespawn_RefreshesCaptainPid asserts the second command reads the
// pane pid and that captain.pid is rewritten to it.
func TestCaptainRespawn_RefreshesCaptainPid_hkz1rj(t *testing.T) {
	proj := t.TempDir()
	run, captured := captureRespawnRun("778899")
	rc := runCaptainRespawn(
		[]string{
			"--name", "captain", "--tmux", "sess:agent",
			"--session-id", respawnTestSID, "--project", proj,
		},
		run, os.Stdout, os.Stderr,
	)
	if rc != 0 {
		t.Fatalf("rc=%d, want 0", rc)
	}
	pidRead := joinArgv((*captured)[1])
	if !strings.Contains(pidRead, "display-message") || !strings.Contains(pidRead, "#{pane_pid}") {
		t.Errorf("second command is not the pane-pid read: %q", pidRead)
	}
	if !strings.Contains(pidRead, "-t sess:agent") {
		t.Errorf("pane-pid read does not target the agent window: %q", pidRead)
	}
	pidFile := filepath.Join(proj, ".harmonik", "cognition", "captain.pid")
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("captain.pid not written: %v", err)
	}
	if strings.TrimSpace(string(b)) != "778899" {
		t.Errorf("captain.pid = %q, want 778899", strings.TrimSpace(string(b)))
	}
}

// TestCaptainRespawn_BareSessionGetsAgentWindow verifies a bare --tmux session
// name (no ":") is targeted at the agent window.
func TestCaptainRespawn_BareSessionGetsAgentWindow_hkz1rj(t *testing.T) {
	proj := t.TempDir()
	run, captured := captureRespawnRun("1")
	rc := runCaptainRespawn(
		[]string{
			"--name", "captain", "--tmux", "harmonik-xyz-captain",
			"--session-id", respawnTestSID, "--project", proj,
		},
		run, os.Stdout, os.Stderr,
	)
	if rc != 0 {
		t.Fatalf("rc=%d, want 0", rc)
	}
	respawn := joinArgv((*captured)[0])
	if !strings.Contains(respawn, "-t harmonik-xyz-captain:agent") {
		t.Errorf("bare session not retargeted to `:agent`: %q", respawn)
	}
}

// TestCaptainRespawn_RejectsBadFlags covers the guard rails: missing --tmux,
// missing --session-id, and a non-UUIDv4 --session-id all exit 1.
func TestCaptainRespawn_RejectsBadFlags_hkz1rj(t *testing.T) {
	run, _ := captureRespawnRun("1")
	cases := []struct {
		name string
		args []string
	}{
		{"no-tmux", []string{"--name", "captain", "--session-id", respawnTestSID}},
		{"no-session-id", []string{"--name", "captain", "--tmux", "s:agent"}},
		{"bad-session-id", []string{"--name", "captain", "--tmux", "s:agent", "--session-id", "NOT-A-UUID"}},
		{"empty-name", []string{"--name", "", "--tmux", "s:agent", "--session-id", respawnTestSID}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if rc := runCaptainRespawn(c.args, run, os.Stdout, os.Stderr); rc != 1 {
				t.Errorf("rc=%d, want 1 (should reject)", rc)
			}
		})
	}
}

// TestCaptainRespawn_RoutedFromSubcommand verifies `harmonik captain respawn`
// routes to the respawn path (not the launcher). A missing --session-id makes
// the respawn path exit 1; if it had fallen through to the launcher it would have
// minted a session-id and tried to launch tmux instead.
func TestCaptainRespawn_RoutedFromSubcommand_hkz1rj(t *testing.T) {
	// `captain respawn` with no required flags must hit the respawn guard (rc 1),
	// proving the subverb peel-off in runCaptainSubcommand fired.
	if rc := runCaptainRespawnSubcommand([]string{"--name", "captain", "--tmux", "s:agent"}); rc != 1 {
		t.Errorf("respawn subcommand rc=%d, want 1 (missing --session-id guard)", rc)
	}
}

// ── captain.go --respawn-cmd arming ─────────────────────────────────────────

// TestCaptainArmsRespawnCmd verifies captain.go arms the keeper window with a
// `--respawn-cmd` whose payload is the `harmonik captain respawn …` invocation:
// the agent-window target, --resume via --session-id flag (resolved to --resume
// at respawn time), and NO verified-restart wrapper.
func TestCaptainArmsRespawnCmd_hkz1rj(t *testing.T) {
	run, _ := captureRunHkly0n()
	ops := &fakeCaptainOps{existsResult: false, panePID: 4242}
	proj := t.TempDir()
	rc := runCaptainLaunchWithOps(
		[]string{"--project", proj, "--session-id", respawnTestSID},
		run, noopKeeperHkly0n, ops,
	)
	if rc != 0 {
		t.Fatalf("captain launch rc=%d, want 0", rc)
	}
	if ops.keeperOpts == nil {
		t.Fatal("keeper window was never armed")
	}
	rc2 := ops.keeperOpts.RespawnCmd
	if rc2 == "" {
		t.Fatal("captain.go armed an EMPTY --respawn-cmd (the ES3 seam is still unwired)")
	}
	session := expectedHashedSession(t, proj)
	wantAgentTarget := session + ":" + ltmux.WindowAgent
	for _, want := range []string{
		"captain respawn",
		"--name captain",
		"--tmux " + wantAgentTarget,
		"--session-id " + respawnTestSID,
		"--project " + proj,
	} {
		if !strings.Contains(rc2, want) {
			t.Errorf("--respawn-cmd %q missing %q", rc2, want)
		}
	}
	// No verified-restart wrapper: the respawn-cmd must NOT reference a
	// restart-verified script or `keeper restart-now` (review B deletes it).
	if strings.Contains(rc2, "restart-verified") || strings.Contains(rc2, "restart-now") {
		t.Errorf("--respawn-cmd must not reference a verified-restart wrapper: %q", rc2)
	}
}

// TestCaptainRespawnCmdString_Quoting verifies a project path with a space is
// shell-quoted so the keeper's `sh -c` survives it.
func TestCaptainRespawnCmdString_Quoting_hkz1rj(t *testing.T) {
	got := captainRespawnCmdString("/path/to/harmonik", "captain", "sess", respawnTestSID, "/a b/proj")
	if !strings.Contains(got, "'/a b/proj'") {
		t.Errorf("project path with space not shell-quoted: %q", got)
	}
	// Sanity: the agent-window target is "<sess>:agent".
	if !strings.Contains(got, "sess:"+ltmux.WindowAgent) {
		t.Errorf("respawn-cmd target is not the agent window: %q", got)
	}
}
