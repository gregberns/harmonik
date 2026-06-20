package daemon

// claudelaunchspec_remote_hkz8ek_test.go — gate-runnable tests that
// buildClaudeLaunchSpec threads the run's CommandRunner into the three
// materialization writes for a REMOTE run, and uses the unchanged box-A-local
// path for a LOCAL run (hk-z8ek). All remote writes are intercepted by a
// RecordingRunner; NO real ssh / worker is touched.

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// newNoOpRecorderZ8ek returns a RecordingRunner that succeeds for every call
// without side effects (exec.Command("true")).
func newNoOpRecorderZ8ek() *tmux.RecordingRunner {
	return &tmux.RecordingRunner{
		CmdFunc: func(_ context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.Command("true")
		},
	}
}

func z8ekRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("mint run id: %v", err)
	}
	return core.RunID(u)
}

// TestBuildClaudeLaunchSpec_Remote_RoutesWritesThroughRunner asserts that when
// rc.runner is set (remote run), buildClaudeLaunchSpec issues the settings.json,
// agent-task.md, and trust writes THROUGH the runner, targeting the worker-side
// worktree path, and that the settings content carries the WORKER's hook command
// plus the resolved HARMONIK_DAEMON_SOCKET-bearing hook wiring.
func TestBuildClaudeLaunchSpec_Remote_RoutesWritesThroughRunner(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const (
		workerWt   = "/Users/gb/harmonik-worker/repo/.harmonik/worktrees/run-z8ek"
		workerBin  = "/Users/gb/go/bin/harmonik"
		workerSock = "/Users/gb/harmonik-worker/repo/.harmonik/run-z8ek.sock"
		boxABin    = "/Users/gb/go/bin/harmonik-boxa"
	)
	rr := newNoOpRecorderZ8ek()

	rc := claudeRunCtx{
		runID:            z8ekRunID(t),
		beadID:           "hk-z8ek",
		workspacePath:    workerWt,
		daemonSocket:     workerSock, // resolveAgentDaemonSocket already picked the worker sock
		workflowMode:     core.WorkflowModeSingle,
		phase:            "",
		iterationCount:   1,
		handlerBinary:    "claude",
		daemonBinaryPath: boxABin, // box A path — MUST NOT be used in remote settings
		beadTitle:        "remote materialization",
		beadDescription:  "Implement the SSH-aware materialization seam.",
		runner:           rr,
		workerBinaryPath: workerBin,
	}

	_, _, err := buildClaudeLaunchSpec(ctx, rc)
	if err != nil {
		t.Fatalf("buildClaudeLaunchSpec (remote): %v", err)
	}

	// Three remote writes expected, in CHB order: settings (sh) → trust (python3)
	// → agent-task (sh). Collect and classify by command.
	//
	// hk-gglt: the trust upsert is now `python3 - <worktreePath>` (program on
	// STDIN, NOT `python3 -c <prog>`), because over SSH the ssh client space-joins
	// the argv and the worker's login shell re-splits it — a multi-line `-c`
	// program is shredded by that re-split and python never runs the upsert. With
	// `python3 -` the program bytes ride cmd.Stdin and never touch the remote
	// command line. The program-content assertion lives in the workspace package
	// test (TestEnsureWorktreeTrustVia_*), which can read the stdin reader; here we
	// assert the argv shape (`-` mode + worktree path token).
	var settingsScript, taskScript, trustArg string
	trustSawDash := false
	for _, c := range rr.Calls {
		switch c.Name {
		case "sh":
			if len(c.Args) == 2 && c.Args[0] == "-lc" {
				s := c.Args[1]
				if strings.Contains(s, "settings.json") {
					settingsScript = s
				} else if strings.Contains(s, "agent-task.md") {
					taskScript = s
				}
			}
		case "python3":
			if len(c.Args) == 2 && c.Args[0] == "-" {
				trustSawDash = true
				trustArg = c.Args[1]
			}
		}
	}

	if settingsScript == "" {
		t.Fatalf("no remote settings.json write recorded; calls=%v", rr.Calls)
	}
	if taskScript == "" {
		t.Fatalf("no remote agent-task.md write recorded; calls=%v", rr.Calls)
	}
	if !trustSawDash {
		t.Fatalf("no remote trust (python3 - <path>) write recorded; calls=%v", rr.Calls)
	}

	// settings.json must target the WORKER worktree path and carry the WORKER
	// hook command — NOT box A's path.
	wantSettingsDest := filepath.Join(workerWt, ".claude", "settings.json")
	if !strings.Contains(settingsScript, wantSettingsDest) {
		t.Errorf("settings write does not target worker path %q:\n%s", wantSettingsDest, settingsScript)
	}
	content := decodeBase64FromScript(t, settingsScript)
	if !strings.Contains(content, workerBin) {
		t.Errorf("settings.json hook command is not the worker harmonik path %q:\n%s", workerBin, content)
	}
	if strings.Contains(content, boxABin) {
		t.Errorf("settings.json leaked box-A harmonik path %q (must use worker path):\n%s", boxABin, content)
	}
	for _, want := range []string{"hook-relay", "SessionStart", "Stop"} {
		if !strings.Contains(content, want) {
			t.Errorf("settings.json missing hook wiring %q:\n%s", want, content)
		}
	}

	// agent-task.md must target the WORKER worktree path and carry the bead body.
	wantTaskDest := filepath.Join(workerWt, ".harmonik", "agent-task.md")
	if !strings.Contains(taskScript, wantTaskDest) {
		t.Errorf("agent-task write does not target worker path %q:\n%s", wantTaskDest, taskScript)
	}
	taskContent := decodeBase64FromScript(t, taskScript)
	if !strings.Contains(taskContent, "Implement the SSH-aware materialization seam.") {
		t.Errorf("agent-task.md missing bead body:\n%s", taskContent)
	}

	// trust upsert must be keyed by the WORKER worktree path.
	if trustArg != workerWt {
		t.Errorf("trust worktree arg = %q, want %q", trustArg, workerWt)
	}
}

// TestBuildClaudeLaunchSpec_Local_UsesLocalFS asserts that with a nil runner
// (local run) buildClaudeLaunchSpec writes the three artifacts to box A's local
// filesystem and makes NO runner-routed remote write (NFR7).
func TestBuildClaudeLaunchSpec_Local_UsesLocalFS(t *testing.T) {
	// Not parallel: sets HARMONIK_CLAUDE_CONFIG_PATH via t.Setenv.
	ctx := context.Background()
	wt := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), ".claude.json")
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", cfgPath)

	rc := claudeRunCtx{
		runID:            z8ekRunID(t),
		beadID:           "hk-z8ek-local",
		workspacePath:    wt,
		daemonSocket:     filepath.Join(wt, ".harmonik", "daemon.sock"),
		workflowMode:     core.WorkflowModeSingle,
		phase:            "",
		iterationCount:   1,
		handlerBinary:    "claude",
		daemonBinaryPath: "/Users/gb/go/bin/harmonik",
		beadTitle:        "local materialization",
		beadDescription:  "local body",
		runner:           nil, // LOCAL run
		workerBinaryPath: "",
	}

	if _, _, err := buildClaudeLaunchSpec(ctx, rc); err != nil {
		t.Fatalf("buildClaudeLaunchSpec (local): %v", err)
	}

	// Local-FS artifacts must exist on box A's disk.
	if _, err := os.Stat(filepath.Join(wt, ".claude", "settings.json")); err != nil {
		t.Errorf("local settings.json not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, ".harmonik", "agent-task.md")); err != nil {
		t.Errorf("local agent-task.md not written: %v", err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Errorf("local trust config not written: %v", err)
	} else if !strings.Contains(string(data), "hasTrustDialogAccepted") {
		t.Errorf("local trust config missing hasTrustDialogAccepted:\n%s", data)
	}
}

// decodeBase64FromScript extracts and decodes the base64 payload from a
// `... printf %s '<b64>' | base64 -d > '<path>'` remote-write script.
func decodeBase64FromScript(t *testing.T, script string) string {
	t.Helper()
	const pfx = "printf %s '"
	i := strings.Index(script, pfx)
	if i < 0 {
		t.Fatalf("no printf in script: %q", script)
	}
	rest := script[i+len(pfx):]
	j := strings.Index(rest, "'")
	if j < 0 {
		t.Fatalf("unterminated base64 in script: %q", script)
	}
	raw, err := base64.StdEncoding.DecodeString(rest[:j])
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	return string(raw)
}
