package daemon_test

// stophook_e2e_hk6pbe3_test.go — E2E integration test: harmonik-twin-claude
// running a Stop hook via the real harmonik hook-relay binary delivers
// outcome_emitted to the daemon Unix socket within stopHookGrace.
//
// Addresses the reviewer flag on hk-cj0gm (hk-6pbe3): the merged processDead
// fix improves diagnostics but adds no guard for the original symptom —
// "zero outcome_emitted events ever delivered". This test exercises the full
// production chain:
//
//   twin exits call_stop_hook step →
//   wrapper script constructs Stop hook JSON →
//   harmonik hook-relay Stop reads JSON from stdin, reads HARMONIK_* from env →
//   writes outcome_emitted envelope to daemon Unix socket →
//   hookSessionStore records the payload →
//   ExportedHookLatestOutcome returns non-nil WORK_COMPLETE
//
// Two test cases:
//
//  1. TwinRelayFastPath — twin runs to completion (call_stop_hook is
//     synchronous, so the outcome is in the store before twin even exits).
//     ExportedHookLatestOutcome must return non-nil immediately.
//
//  2. TwinRelayWaitGrace — same setup, but the assertion uses
//     ExportedWaitWithSocketGrace with a pre-completed mock session.  Guards
//     the stopHookGrace timing contract: the fast-path check inside
//     waitWithSocketGrace must pick up the stored outcome.
//
// claudesettings_wm040a.go wires the Stop hook correctly per source review
// but no prior test asserted production end-to-end behaviour (bead hk-6pbe3).
//
// Helper prefix: stopHookE2E (implementer-protocol.md §Helper-prefix discipline;
// bead hk-6pbe3).
//
// Spec refs:
//   - specs/claude-hook-bridge.md §4.7 CHB-020 (terminal-event mapping)
//   - specs/claude-hook-bridge.md §4.10 CHB-025 (last-received-wins)
//   - internal/daemon/waitsocketgrace.go (stopHookGrace = 3 s)
//
// Bead: hk-6pbe3.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
)

// ─────────────────────────────────────────────────────────────────────────────
// Build helpers
// ─────────────────────────────────────────────────────────────────────────────

// stopHookE2EFixtureModuleRoot resolves the Go module root by running
// `go env GOMOD` from the current working directory.
func stopHookE2EFixtureModuleRoot(t *testing.T) string {
	t.Helper()
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("stopHookE2EFixtureModuleRoot: 'go' not found in PATH; skipping: %v", err)
		return ""
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("stopHookE2EFixtureModuleRoot: getwd: %v", err)
	}
	//nolint:gosec // G204: goTool from LookPath; args are literals
	cmd := exec.CommandContext(t.Context(), goTool, "env", "GOMOD")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("stopHookE2EFixtureModuleRoot: go env GOMOD: %v; skipping", err)
		return ""
	}
	return filepath.Dir(strings.TrimSpace(string(out)))
}

// stopHookE2EFixtureBuildBinary builds the Go package at pkgImportPath into a
// temp directory and returns the binary path. Skips the test if the Go
// toolchain is unavailable or the build fails.
func stopHookE2EFixtureBuildBinary(t *testing.T, moduleRoot, pkgImportPath, binName string) string {
	t.Helper()
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("stopHookE2EFixtureBuildBinary: 'go' not found in PATH; skipping: %v", err)
		return ""
	}
	outDir := t.TempDir()
	binPath := filepath.Join(outDir, binName)
	//nolint:gosec // G204: goTool from LookPath; pkgImportPath and binPath are test-controlled
	buildCmd := exec.CommandContext(t.Context(), goTool, "build", "-o", binPath, pkgImportPath)
	buildCmd.Dir = moduleRoot
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, buildErr := buildCmd.CombinedOutput(); buildErr != nil {
		t.Fatalf("stopHookE2EFixtureBuildBinary: build %q: %v\n%s", pkgImportPath, buildErr, out)
	}
	return binPath
}

// ─────────────────────────────────────────────────────────────────────────────
// Worktree / settings helpers
// ─────────────────────────────────────────────────────────────────────────────

// stopHookE2EFixtureWorktree creates a temporary worktree directory containing:
//   - wrapper.sh: a shell script that constructs the Stop hook JSON from
//     $HARMONIK_CLAUDE_SESSION_ID and pipes it to harmonik hook-relay Stop.
//   - .claude/settings.json: declares wrapper.sh as the Stop hook command.
//
// The twin binary's call_stop_hook step reads stopHookCommand from settings.json
// and executes it (no args are forwarded); wrapper.sh bridges the gap by
// constructing the hook input JSON itself and passing it to hook-relay via
// stdin — matching the wire protocol the real claude would use.
//
// Returns the worktree root directory.
func stopHookE2EFixtureWorktree(t *testing.T, harmonikBin string) string {
	t.Helper()
	root := t.TempDir()

	// Write wrapper.sh.
	// The script:
	//   1. Reads HARMONIK_CLAUDE_SESSION_ID from env (set by the daemon at twin launch).
	//   2. Constructs a minimal Stop hook input JSON per CHB-012.
	//   3. Pipes it to `harmonik hook-relay Stop` which reads HARMONIK_* env vars,
	//      validates the JSON, and sends outcome_emitted to the daemon socket.
	//
	// Quoting: hookCommand is the absolute harmonik binary path; no shell injection
	// risk since it is derived from exec.LookPath + temp build dir.
	wrapperPath := filepath.Join(root, "wrapper.sh")
	wrapperContent := "#!/bin/sh\n" +
		`printf '{"session_id":"%s","hook_event_name":"Stop"}' "$HARMONIK_CLAUDE_SESSION_ID"` +
		" | " + harmonikBin + " hook-relay Stop\n"
	if err := os.WriteFile(wrapperPath, []byte(wrapperContent), 0o755); err != nil { //nolint:gosec // G306: wrapper.sh needs executable bit
		t.Fatalf("stopHookE2EFixtureWorktree: write wrapper.sh: %v", err)
	}

	// Write .claude/settings.json with Stop hook pointing to wrapper.sh.
	// The twin's settings parser extracts only the first Stop hook's command
	// field (cmd/harmonik-twin-claude/settings.go loadCloneSettings).
	claudeDir := filepath.Join(root, ".claude")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("stopHookE2EFixtureWorktree: mkdir .claude: %v", err)
	}
	settings := map[string]any{
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{
					"matcher": "",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": wrapperPath,
							"args":    []string{},
							"timeout": 30,
						},
					},
				},
			},
		},
	}
	settingsBytes, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Fatalf("stopHookE2EFixtureWorktree: marshal settings: %v", err)
	}
	settingsPath := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(settingsPath, settingsBytes, 0o600); err != nil {
		t.Fatalf("stopHookE2EFixtureWorktree: write settings.json: %v", err)
	}
	return root
}

// ─────────────────────────────────────────────────────────────────────────────
// Script helper
// ─────────────────────────────────────────────────────────────────────────────

// stopHookE2EFixtureScript writes a YAML twin script that exercises the
// call_stop_hook step and returns its path.
//
// The script emits the minimal progress-stream sequence required by the twin:
// handler_capabilities → agent_ready → call_stop_hook → agent_completed.
// The call_stop_hook step causes the twin to execute the worktree's Stop hook
// (wrapper.sh) which delivers outcome_emitted to the daemon socket.
func stopHookE2EFixtureScript(t *testing.T, runID, handlerSessID string) string {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "twin-script.yaml")
	scriptContent := `heartbeat_mode: scripted
messages:
  - type: handler_capabilities
    payload:
      run_id: "` + runID + `"
      session_id: "` + handlerSessID + `"
      protocol_versions_supported: [1]
  - type: agent_ready
    payload:
      run_id: "` + runID + `"
      session_id: "` + handlerSessID + `"
      capabilities: []
  - type: call_stop_hook
  - type: agent_completed
    payload:
      run_id: "` + runID + `"
      session_id: "` + handlerSessID + `"
      ended_at: "2026-05-20T00:00:00Z"
      exit_code: 0
      outcome_ref: "` + runID + `/outcome"
`
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o600); err != nil {
		t.Fatalf("stopHookE2EFixtureScript: write: %v", err)
	}
	return scriptPath
}

// ─────────────────────────────────────────────────────────────────────────────
// Shared setup helper
// ─────────────────────────────────────────────────────────────────────────────

// stopHookE2EFixtureCoreSetup builds both binaries, starts the socket listener,
// registers the hook session, creates the worktree and script, and runs the
// twin binary to completion. Returns the store so callers can make assertions.
//
// The twin binary runs synchronously; by the time it returns the call_stop_hook
// step has already dispatched wrapper.sh → hook-relay → daemon socket, so the
// outcome is in the store (fast path for waitWithSocketGrace).
func stopHookE2EFixtureCoreSetup(t *testing.T, runID, handlerSessID, claudeSessionID string) (
	store *daemon.HookSessionStoreExported,
	sockPath string,
) {
	t.Helper()

	moduleRoot := stopHookE2EFixtureModuleRoot(t)
	harmonikBin := stopHookE2EFixtureBuildBinary(t, moduleRoot,
		"github.com/gregberns/harmonik/cmd/harmonik", "harmonik")
	twinBin := stopHookE2EFixtureBuildBinary(t, moduleRoot,
		"github.com/gregberns/harmonik/cmd/harmonik-twin-claude", "harmonik-twin-claude")

	// Start daemon socket listener with a real hookSessionStore.
	sockPath = socketFixtureTempSockPath(t)
	store = daemon.ExportedNewHookSessionStore()
	socketFixtureStartListener(t, sockPath, nil, store)
	socketFixtureWaitReady(t, sockPath)

	// Register hook session before the twin runs so the relay's envelope is
	// accepted (unknown_session → no store update).
	daemon.ExportedHookRegister(store, runID, claudeSessionID)

	worktreePath := stopHookE2EFixtureWorktree(t, harmonikBin)
	scriptPath := stopHookE2EFixtureScript(t, runID, handlerSessID)

	// HARMONIK_* env vars the twin passes through to its hook subprocess.
	// The hook-relay subprocess (wrapper.sh → harmonik hook-relay Stop) reads
	// all of these from its inherited env per CHB-006.
	twinEnv := []string{
		"HARMONIK_RUN_ID=" + runID,
		"HARMONIK_DAEMON_SOCKET=" + sockPath,
		"HARMONIK_WORKSPACE_PATH=" + worktreePath,
		"HARMONIK_HANDLER_SESSION_ID=" + handlerSessID,
		"HARMONIK_CLAUDE_SESSION_ID=" + claudeSessionID,
		"HARMONIK_WORKFLOW_ID=wf-6pbe3-e2e",
		"HARMONIK_NODE_ID=node-6pbe3-e2e",
		"HARMONIK_AGENT_TYPE=implementer",
		// PATH is required so wrapper.sh can invoke /bin/sh and the harmonik
		// binary path is absolute, so PATH is needed only for the shell itself.
		"PATH=" + os.Getenv("PATH"),
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	//nolint:gosec // G204: twinBin is from temp build; args are test-controlled
	cmd := exec.CommandContext(ctx, twinBin,
		"--script-path", scriptPath,
		"--worktree-path", worktreePath,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = twinEnv
	cmd.Dir = worktreePath

	if err := cmd.Run(); err != nil {
		t.Fatalf("stopHookE2EFixtureCoreSetup: twin exited non-zero: %v\nstderr: %s\nstdout: %s",
			err, stderr.String(), stdout.String())
	}
	return store, sockPath
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 1: fast-path — outcome in store before twin exits
// ─────────────────────────────────────────────────────────────────────────────

// TestStopHookE2E_TwinRelayFastPath verifies that running harmonik-twin-claude
// with a call_stop_hook step, a real daemon socket, and the harmonik hook-relay
// as the Stop hook results in outcome_emitted being stored in the daemon's
// hookSessionStore by the time the twin process exits.
//
// This is the primary guard for hk-6pbe3: the complete production chain
// (twin → wrapper → hook-relay → socket → store) must function end-to-end.
func TestStopHookE2E_TwinRelayFastPath(t *testing.T) {
	t.Parallel()

	const runID = "run-e2e-6pbe3-fast-01"
	const handlerSessID = "sess-6pbe3-fast-01"
	const claudeSessionID = "claude-6pbe3-fast-01"

	store, _ := stopHookE2EFixtureCoreSetup(t, runID, handlerSessID, claudeSessionID)

	// After twin.Run() returns, the call_stop_hook step has already completed
	// synchronously: wrapper.sh ran hook-relay, which got a socket ACK before
	// returning to the twin. The outcome must be in the store.
	raw := daemon.ExportedHookLatestOutcome(store, runID, claudeSessionID)
	if raw == nil {
		t.Fatal("outcome_emitted not in store after twin+hook-relay E2E run; " +
			"expected WORK_COMPLETE delivered via real daemon socket")
	}

	// Verify the stored payload decodes to WORK_COMPLETE.
	var p handler.ExportedOutcomeEmittedPayload
	if err := json.Unmarshal(*raw, &p); err != nil {
		t.Fatalf("unmarshal stored outcome payload: %v (raw: %s)", err, string(*raw))
	}
	if p.Kind != "WORK_COMPLETE" {
		t.Errorf("outcome.Kind=%q; want WORK_COMPLETE", p.Kind)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2: waitWithSocketGrace timing contract
// ─────────────────────────────────────────────────────────────────────────────

// TestStopHookE2E_TwinRelayWaitGrace verifies that ExportedWaitWithSocketGrace
// returns the WORK_COMPLETE outcome via its fast-path LatestOutcome check when
// the outcome arrived before the grace window (the call_stop_hook step
// completes synchronously, so the outcome is already in the store by the time
// waitWithSocketGrace is called).
//
// This is the timing-contract guard for stopHookGrace: the original hk-ajhqw
// failure was "outcome arrived ~5 minutes before waitWithSocketGrace was called
// and the fast-path check missed it". This test confirms the fast path works
// end-to-end with real binaries.
func TestStopHookE2E_TwinRelayWaitGrace(t *testing.T) {
	t.Parallel()

	const runID = "run-e2e-6pbe3-grace-01"
	const handlerSessID = "sess-6pbe3-grace-01"
	const claudeSessionID = "claude-6pbe3-grace-01"

	store, _ := stopHookE2EFixtureCoreSetup(t, runID, handlerSessID, claudeSessionID)

	// Simulate a completed session (exit code 0, no watcher).
	// waitGraceFixtureNewSession is defined in waitsocketgrace_hkgql2022_test.go.
	sess := waitGraceFixtureNewSession(0, nil)
	sess.unblockWait()

	// waitWithSocketGrace must find the stored outcome via LatestOutcome (fast
	// path) and return it without waiting the full 3 s grace window.
	start := time.Now()
	got, ei := daemon.ExportedWaitWithSocketGrace(
		t.Context(), store, nil, sess, runID, claudeSessionID,
	)
	elapsed := time.Since(start)

	if got == nil {
		t.Fatal("ExportedWaitWithSocketGrace returned nil outcome; " +
			"expected WORK_COMPLETE from fast path (outcome already stored by hook-relay)")
	}
	if got.Kind != "WORK_COMPLETE" {
		t.Errorf("outcome.Kind=%q; want WORK_COMPLETE", got.Kind)
	}
	if ei.ExitCode != 0 {
		t.Errorf("exitCode=%d; want 0", ei.ExitCode)
	}
	// Fast-path timing guard: the outcome is already in the store before
	// waitWithSocketGrace is called, so the fast-path LatestOutcome check must
	// return without sleeping the full stopHookGrace window.  Use half of
	// stopHookGrace as the bound — a 2.9 s regression (implicit near-timeout)
	// would exceed this and surface as a test failure.
	const graceMargin = daemon.ExportedStopHookGrace / 2
	if elapsed > graceMargin {
		t.Errorf("ExportedWaitWithSocketGrace took %v; want <%v (half of stopHookGrace %v); "+
			"fast-path should not delay when outcome is already stored",
			elapsed, graceMargin, daemon.ExportedStopHookGrace)
	}
}
