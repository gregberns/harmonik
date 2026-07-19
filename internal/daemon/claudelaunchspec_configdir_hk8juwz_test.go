package daemon

// claudelaunchspec_configdir_hk8juwz_test.go — asserts that a claude:LOCAL launch
// (rc.runner == nil) provisions a private CLAUDE_CONFIG_DIR and exports it in the
// LaunchSpec env, so claude v2.1.214 reads its onboarding state from an ISOLATED
// config instead of the fleet-raced shared global ~/.claude.json (hk-8juwz).

import (
	"context"
	"os"
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

// TestBuildClaudeLaunchSpec_Local_SetsClaudeConfigDir asserts that a local run
// (nil runner) sets CLAUDE_CONFIG_DIR in the launch env, pointing at the private
// per-worktree config dir, and that the isolated .claude.json is seeded + trusted.
func TestBuildClaudeLaunchSpec_Local_SetsClaudeConfigDir(t *testing.T) {
	// Hermetic source: point HOME at a temp dir with a fake onboarded config so
	// PrepareIsolatedClaudeConfigDir copies THAT, not the operator's real config.
	ctx := context.Background()
	home := t.TempDir()
	srcCfg := filepath.Join(home, ".claude.json")
	if err := os.WriteFile(srcCfg, []byte(`{"firstStartTime":"2026-01-02T03:04:05.678Z","migrationVersion":13,"someKey":"keepme"}`), 0o600); err != nil {
		t.Fatalf("write fake source config: %v", err)
	}
	t.Setenv("HOME", home)
	// Redirect the shared-global trust writer off the real ~/.claude.json too.
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
		beadTitle:        "config-dir isolation",
		beadDescription:  "body",
		runner:           nil, // LOCAL run
	}

	spec, _, err := buildClaudeLaunchSpec(ctx, rc)
	if err != nil {
		t.Fatalf("buildClaudeLaunchSpec (local): %v", err)
	}

	got, ok := envValue(spec.Env, "CLAUDE_CONFIG_DIR")
	if !ok {
		t.Fatalf("CLAUDE_CONFIG_DIR absent from local launch env:\n%v", spec.Env)
	}
	wantDir := filepath.Join(wt, ".harmonik", "claude-config")
	if got != wantDir {
		t.Errorf("CLAUDE_CONFIG_DIR = %q, want %q", got, wantDir)
	}

	// The isolated config must exist inside the worktree, carry the copied
	// onboarding keys, and be folder-trusted.
	isoCfg := filepath.Join(got, ".claude.json")
	data, rerr := os.ReadFile(isoCfg) //nolint:gosec // G304: path from test tempdir, not user input
	if rerr != nil {
		t.Fatalf("isolated .claude.json not written: %v", rerr)
	}
	s := string(data)
	if !strings.Contains(s, "firstStartTime") || !strings.Contains(s, "keepme") {
		t.Errorf("isolated config did not preserve copied onboarding keys:\n%s", s)
	}
	if !strings.Contains(s, "hasTrustDialogAccepted") {
		t.Errorf("isolated config missing trust entry:\n%s", s)
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
