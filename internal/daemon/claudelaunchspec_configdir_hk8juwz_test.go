package daemon

// claudelaunchspec_configdir_hk8juwz_test.go — pins WHO gets CLAUDE_CONFIG_DIR.
// REMOTE (rc.runner != nil) provisions a private, worker-absolute config dir and
// exports it (hk-qxvc2). LOCAL (rc.runner == nil) must export NOTHING: the local
// isolation was reverted after it broke claude auth (hk-8juwz).

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// envValue returns the value of key in a KEY=VALUE env slice, and whether it was
// present.
func envValue(env []string, key string) (string, bool) {
	pfx := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, pfx) {
			return kv[len(pfx):], true
		}
	}
	return "", false
}

// TestBuildClaudeLaunchSpec_Local_NoClaudeConfigDir is the REGRESSION GUARD for the
// hk-8juwz revert: a claude:LOCAL launch spec must carry NO CLAUDE_CONFIG_DIR entry
// at all, so claude inherits the operator's real ~/.claude.
//
// Do NOT "fix" this test by re-adding the isolation. It was live-refuted in an A/B
// on one daemon with one line toggled: isolation ON → agent_ready_timeout at 150s,
// pane parked on the Bypass Permissions modal (relocating CLAUDE_CONFIG_DIR drops
// ~/.claude/settings.json and its skipDangerousModePermissionPrompt); worse,
// claude then reports "Not logged in · Please run /login" and does no work at all.
// Isolation OFF → agent_ready in 2.0s and the run completed. If you reintroduce
// local isolation, you must first prove on a LIVE run that the relocated dir keeps
// claude authenticated AND suppresses the bypass modal.
func TestBuildClaudeLaunchSpec_Local_NoClaudeConfigDir(t *testing.T) {
	ctx := context.Background()
	// Redirect the shared-global trust writer off the real ~/.claude.json.
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", filepath.Join(t.TempDir(), ".claude.json"))

	wt := t.TempDir()
	rc := claudeRunCtx{
		runID:            z8ekRunID(t),
		beadID:           "hk-8juwz-local",
		workspacePath:    wt,
		daemonSocket:     filepath.Join(wt, ".harmonik", "daemon.sock"),
		workflowMode:     core.WorkflowModeSingle,
		phase:            "",
		iterationCount:   1,
		handlerBinary:    "claude",
		daemonBinaryPath: "/Users/gb/go/bin/harmonik",
		beadTitle:        "no config-dir isolation",
		beadDescription:  "body",
		runner:           nil, // LOCAL run
	}

	spec, _, err := buildClaudeLaunchSpec(ctx, rc)
	if err != nil {
		t.Fatalf("buildClaudeLaunchSpec (local): %v", err)
	}

	if got, ok := envValue(spec.Env, "CLAUDE_CONFIG_DIR"); ok {
		t.Fatalf("local launch env must NOT set CLAUDE_CONFIG_DIR (got %q); the local isolation was reverted (hk-8juwz):\n%v", got, spec.Env)
	}
}

// TestBuildClaudeLaunchSpec_Remote_SetsClaudeConfigDir asserts that a remote run
// (non-nil runner) ALSO provisions a private CLAUDE_CONFIG_DIR — now pointing at
// the WORKER-absolute isolated dir — and routes the isolation-provisioning program
// through the runner so it executes ON THE WORKER (hk-qxvc2). This is the remote
// counterpart of the local isolation (hk-8juwz): without it, the worker's claude
// reads the fleet-raced shared ~/.claude.json and wedges on the onboarding modal
// before SessionStart fires → agent_ready_timeout.
func TestBuildClaudeLaunchSpec_Remote_SetsClaudeConfigDir(t *testing.T) {
	ctx := context.Background()
	wt := t.TempDir()
	rr := newNoOpRecorderZ8ek() // REMOTE run
	rc := claudeRunCtx{
		runID:            z8ekRunID(t),
		beadID:           "hk-qxvc2-remote",
		workspacePath:    wt,
		daemonSocket:     filepath.Join(wt, ".harmonik", "daemon.sock"),
		workflowMode:     core.WorkflowModeSingle,
		phase:            "",
		iterationCount:   1,
		handlerBinary:    "claude",
		daemonBinaryPath: "/Users/gb/go/bin/harmonik",
		beadTitle:        "remote run",
		beadDescription:  "body",
		runner:           rr,
		workerBinaryPath: "/home/worker/harmonik",
	}

	spec, _, err := buildClaudeLaunchSpec(ctx, rc)
	if err != nil {
		t.Fatalf("buildClaudeLaunchSpec (remote): %v", err)
	}

	// CLAUDE_CONFIG_DIR must now be exported, pointing at the worker-absolute
	// isolated dir under the (worker) worktree path.
	got, ok := envValue(spec.Env, "CLAUDE_CONFIG_DIR")
	if !ok {
		t.Fatalf("CLAUDE_CONFIG_DIR absent from remote launch env:\n%v", spec.Env)
	}
	wantDir := filepath.Join(wt, ".harmonik", "claude-config")
	if got != wantDir {
		t.Errorf("CLAUDE_CONFIG_DIR = %q, want %q", got, wantDir)
	}

	// The isolation-provisioning program must run ON THE WORKER — i.e. through the
	// runner as `python3 - <workspacePath>` (fed on stdin, not via -c).
	var sawPrepare bool
	for _, c := range rr.Calls {
		if c.Name == "python3" && len(c.Args) >= 2 && c.Args[0] == "-" && c.Args[len(c.Args)-1] == wt {
			sawPrepare = true
			break
		}
	}
	if !sawPrepare {
		t.Errorf("isolated-config prepare not routed through runner as `python3 - %s`; calls:\n%+v", wt, rr.Calls)
	}
}
