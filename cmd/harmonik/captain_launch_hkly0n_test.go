package main

// captain_launch_hkly0n_test.go — RED-then-GREEN coverage for the bare
// `harmonik captain` launcher (hk-ly0n). Verifies the subcommand parses flags,
// mints+validates a UUIDv4 session-id, builds the correct tmux/claude argv
// (asserted via the injected run func — NO real tmux), and rejects a
// non-UUIDv4 --session-id. Helpers carry the hkly0n suffix per test-hygiene.

import (
	"io"
	"os/exec"
	"testing"

	"github.com/gregberns/harmonik/internal/keeper"
)

// captureRunHkly0n returns a run func that records the *exec.Cmd it is handed
// and a pointer through which the captured command is read back.
func captureRunHkly0n() (run captainLaunchRunFn, captured **exec.Cmd) {
	var got *exec.Cmd
	return func(cmd *exec.Cmd) error {
		got = cmd
		return nil
	}, &got
}

// noopKeeperHkly0n is a keeper-enable seam that records nothing and reports
// success. The hkly0n launcher tests assert tmux argv, not keeper wiring, so a
// no-op keeper keeps them from touching the real ~/.claude/settings.json.
func noopKeeperHkly0n(_ enableConfig, _, _ io.Writer) int { return 0 }

// argvHkly0n returns the captured command's full argv (cmd.Args), or nil.
func argvHkly0n(cmd *exec.Cmd) []string {
	if cmd == nil {
		return nil
	}
	return cmd.Args
}

// flagValueHkly0n returns the token immediately following flag in argv, or "".
func flagValueHkly0n(argv []string, flag string) string {
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == flag {
			return argv[i+1]
		}
	}
	return ""
}

func TestCaptainLaunch_MintsUUIDv4AndBuildsArgv_hkly0n(t *testing.T) {
	run, captured := captureRunHkly0n()
	// ES2 (hk-bcd0): drive through the ops-injectable core with a fake so this
	// unit test never touches a real tmux server. The default session name is now
	// the hashed namespace (asserted in captain_launch_hkbcd0_test.go); here we
	// only check the agent-window argv SHAPE.
	code := runCaptainLaunchWithOps([]string{"--project", t.TempDir()}, run, noopKeeperHkly0n, &fakeCaptainOps{})
	if code != 0 {
		t.Fatalf("runCaptainLaunch exit = %d, want 0", code)
	}
	argv := argvHkly0n(*captured)
	if len(argv) == 0 {
		t.Fatal("run func was not invoked with a command")
	}

	// tmux new-session -d -s <tmux> -n agent ...
	if argv[0] != "tmux" || argv[1] != "new-session" {
		t.Fatalf("argv must start with `tmux new-session`, got %v", argv)
	}
	if !containsHkly0n(argv, "-d") {
		t.Errorf("argv missing -d (detached): %v", argv)
	}
	// Session name is the hashed namespace now — it must NOT be the old plain
	// "captain" (the explicit hashed-name assertion lives in the hkbcd0 test).
	if got := flagValueHkly0n(argv, "-s"); got == "captain" || got == "" {
		t.Errorf("tmux session name = %q, want the hashed harmonik-<hash>-captain namespace", got)
	}

	// -e HARMONIK_AGENT=captain
	if got := flagValueHkly0n(argv, "-e"); got != "HARMONIK_AGENT=captain" {
		t.Errorf("env = %q, want %q", got, "HARMONIK_AGENT=captain")
	}

	// claude --remote-control captain --session-id <uuidv4>
	if !containsHkly0n(argv, "claude") {
		t.Errorf("argv missing claude binary: %v", argv)
	}
	if got := flagValueHkly0n(argv, "--remote-control"); got != "captain" {
		t.Errorf("--remote-control = %q, want default %q", got, "captain")
	}
	sid := flagValueHkly0n(argv, "--session-id")
	if !keeper.IsPrimarySID(sid) {
		t.Errorf("minted --session-id %q is not a canonical lowercase UUIDv4", sid)
	}
}

func TestCaptainLaunch_HonorsNameAndTmuxFlags_hkly0n(t *testing.T) {
	run, captured := captureRunHkly0n()
	code := runCaptainLaunchWithOps([]string{
		"--name", "skipper", "--tmux", "cap-pane", "--project", t.TempDir(),
	}, run, noopKeeperHkly0n, &fakeCaptainOps{})
	if code != 0 {
		t.Fatalf("runCaptainLaunch exit = %d, want 0", code)
	}
	argv := argvHkly0n(*captured)
	if got := flagValueHkly0n(argv, "-s"); got != "cap-pane" {
		t.Errorf("tmux session = %q, want %q", got, "cap-pane")
	}
	if got := flagValueHkly0n(argv, "-e"); got != "HARMONIK_AGENT=skipper" {
		t.Errorf("env = %q, want %q", got, "HARMONIK_AGENT=skipper")
	}
	if got := flagValueHkly0n(argv, "--remote-control"); got != "skipper" {
		t.Errorf("--remote-control = %q, want %q", got, "skipper")
	}
}

func TestCaptainLaunch_AcceptsValidSessionID_hkly0n(t *testing.T) {
	const want = "11111111-2222-4333-8444-555555555555" // canonical lowercase UUIDv4
	run, captured := captureRunHkly0n()
	code := runCaptainLaunchWithOps([]string{"--session-id", want, "--project", t.TempDir()}, run, noopKeeperHkly0n, &fakeCaptainOps{})
	if code != 0 {
		t.Fatalf("runCaptainLaunch exit = %d, want 0", code)
	}
	if got := flagValueHkly0n(argvHkly0n(*captured), "--session-id"); got != want {
		t.Errorf("--session-id = %q, want supplied %q", got, want)
	}
}

func TestCaptainLaunch_RejectsNonUUIDv4_hkly0n(t *testing.T) {
	cases := map[string]string{
		"not-a-uuid":     "totally-not-a-uuid",
		"uuidv7":         "0190aaaa-bbbb-7ccc-8ddd-eeeeeeeeeeee", // version nibble 7
		"uppercase":      "ABCDEF01-2222-4333-8444-555555555555", // uppercase hex (transcript-dir id)
		"empty-ish-junk": "----",
	}
	for label, sid := range cases {
		t.Run(label, func(t *testing.T) {
			run, captured := captureRunHkly0n()
			code := runCaptainLaunchWithOps([]string{"--session-id", sid, "--project", t.TempDir()}, run, noopKeeperHkly0n, &fakeCaptainOps{})
			if code != 1 {
				t.Fatalf("runCaptainLaunch(%q) exit = %d, want 1", sid, code)
			}
			if *captured != nil {
				t.Errorf("tmux must NOT be launched for invalid session-id %q", sid)
			}
		})
	}
}

func containsHkly0n(argv []string, want string) bool {
	for _, a := range argv {
		if a == want {
			return true
		}
	}
	return false
}
