package daemon_test

// sandboxexecwrap_hkr4p0l_test.go — EXEC-path srt argv-wrap (hk-r4p0l part 2).
//
// # The bug (RED-before)
//
// The gate fix (part 1) makes sandboxSpawnForRun return a non-nil SrtSpawnConfig
// for a pi run under backend=srt + harnesses:[pi]. But pi is a SessionIDCaptured
// harness: the workloop forces spec.Substrate = nil so the run takes the EXEC
// path (exec.CommandContext(spec.Binary, spec.Args...)) to capture stdout/
// session-id. The srt argv-wrap previously lived ONLY in
// perRunSubstrate.SpawnWindow — which is NEVER invoked on the exec path. So the
// gate attached sandboxSpawn to a substrate that was discarded, and pi launched
// UN-sandboxed: spec.Binary/spec.Args reached exec.CommandContext with no
// `srt --settings` prefix.
//
// TestExecPath_Pi_WasNotWrapped_BeforeFix documents that RED state: it asserts
// that the mere existence of a non-nil gate decision does NOT, by itself, change
// the exec-path argv — the wrap has to be applied explicitly. Before this bead
// there was no exec-path wrap site at all, so a sandboxed pi run's launched argv
// was byte-identical to the un-sandboxed argv.
//
// # The fix (GREEN-after)
//
// sandboxWrapExecArgv (sandboxgate.go) applies the SAME srtWrapArgv the substrate
// path uses, directly to the exec-path (binary, args). The workloop calls it in
// the SessionIDCaptured branch after forcing spec.Substrate = nil. These tests
// exercise sandboxWrapExecArgv with the EXACT SrtSpawnConfig that
// sandboxSpawnForRun produces for a pi run, so together with
// sandboxgate_hkr4p0l_test.go they cover the full workloop composition:
//
//	gate(pi, srt, harnesses:[pi]) -> non-nil spawn -> exec-wrap -> srt-prefixed argv.
//
// Helper prefix: hkr4p0lexec.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// hkr4p0lexecProfileInput returns a minimal SandboxProfileInput satisfying the
// REQUIRED fields so GenerateSandboxProfile succeeds.
func hkr4p0lexecProfileInput(t *testing.T, runID string) daemon.SandboxProfileInput {
	t.Helper()
	tmp := t.TempDir()
	return daemon.SandboxProfileInput{
		WorktreePath:   tmp,
		GitDir:         filepath.Join(tmp, ".git"),
		RunID:          runID,
		DaemonSockPath: filepath.Join(tmp, "daemon.sock"),
	}
}

// TestExecPath_Pi_IsSrtWrapped is the GREEN-after: a sandboxed pi run (the gate
// returns a non-nil SrtSpawnConfig) has its exec-path argv rewritten to
// `srt --settings <profile> <origBinary> <origArgs...>`. This is the argv that
// reaches exec.CommandContext on the SessionIDCaptured (pi) path.
func TestExecPath_Pi_IsSrtWrapped(t *testing.T) {
	t.Parallel()

	// Reproduce the workloop's gate decision for a pi run.
	cfg := daemon.SandboxConfig{Backend: "srt", Harnesses: []string{"pi"}}
	in := hkr4p0lexecProfileInput(t, "hkr4p0lexec-pi-run")
	spawn := daemon.ExportedSandboxSpawnForRun(cfg, core.AgentTypePi, in)
	if spawn == nil {
		t.Fatal("precondition: gate must return non-nil spawn for pi + srt + harnesses:[pi]")
	}

	// The un-sandboxed exec-path argv the workloop would otherwise launch.
	origBinary := "/opt/pi/bin/pi"
	origArgs := []string{"--mode=json", "--session-id", "abc-123"}

	gotBinary, gotArgs, err := daemon.ExportedSandboxWrapExecArgv(spawn, origBinary, origArgs)
	if err != nil {
		t.Fatalf("sandboxWrapExecArgv: %v", err)
	}

	// Binary must now be srt (default binary name — SrtBinary empty).
	if gotBinary != "srt" {
		t.Errorf("exec-path binary = %q; want \"srt\" (pi run must be srt-wrapped)", gotBinary)
	}

	// Args must be: ["--settings", <profilePath>, origBinary, origArgs...].
	profilePath := filepath.Join(os.TempDir(), "harmonik-srt-"+in.RunID+".json")
	wantArgs := append([]string{"--settings", profilePath, origBinary}, origArgs...)
	if len(gotArgs) != len(wantArgs) {
		t.Fatalf("exec-path args length = %d (%v); want %d (%v)", len(gotArgs), gotArgs, len(wantArgs), wantArgs)
	}
	for i := range wantArgs {
		if gotArgs[i] != wantArgs[i] {
			t.Errorf("exec-path args[%d] = %q; want %q\nfull got:  %v\nfull want: %v", i, gotArgs[i], wantArgs[i], gotArgs, wantArgs)
		}
	}

	// The profile file must have been generated + written and be valid JSON.
	data, readErr := os.ReadFile(profilePath)
	if readErr != nil {
		t.Fatalf("exec-path wrap did not write the srt profile at %s: %v", profilePath, readErr)
	}
	var m map[string]any
	if unmarshalErr := json.Unmarshal(data, &m); unmarshalErr != nil {
		t.Errorf("srt profile at %s is not valid JSON: %v", profilePath, unmarshalErr)
	}
}

// TestExecPath_Claude_NotWrapped: a claude run is NOT in sandbox.harnesses, so
// the gate returns nil and the exec-path argv is byte-identical (strict no-op).
// This is the "must remain a no-op for the substrate/claude cases" invariant.
func TestExecPath_Claude_NotWrapped(t *testing.T) {
	t.Parallel()

	cfg := daemon.SandboxConfig{Backend: "srt", Harnesses: []string{"pi"}}
	in := hkr4p0lexecProfileInput(t, "hkr4p0lexec-claude-run")
	spawn := daemon.ExportedSandboxSpawnForRun(cfg, core.AgentTypeClaudeCode, in)
	if spawn != nil {
		t.Fatal("precondition: claude run not in harnesses:[pi] must gate to nil")
	}

	origBinary := "/opt/claude/bin/claude"
	origArgs := []string{"--session-id", "xyz"}

	gotBinary, gotArgs, err := daemon.ExportedSandboxWrapExecArgv(spawn, origBinary, origArgs)
	if err != nil {
		t.Fatalf("sandboxWrapExecArgv: %v", err)
	}
	if gotBinary != origBinary {
		t.Errorf("claude exec-path binary = %q; want unchanged %q (must be a strict no-op)", gotBinary, origBinary)
	}
	if len(gotArgs) != len(origArgs) {
		t.Fatalf("claude exec-path args = %v; want unchanged %v", gotArgs, origArgs)
	}
	for i := range origArgs {
		if gotArgs[i] != origArgs[i] {
			t.Errorf("claude exec-path args[%d] = %q; want unchanged %q", i, gotArgs[i], origArgs[i])
		}
	}
}

// TestExecPath_BackendNone_NotWrapped: backend=none/"" gates to nil for a pi run
// too, so the exec-path argv is unchanged (the sandbox must be OFF).
func TestExecPath_BackendNone_NotWrapped(t *testing.T) {
	t.Parallel()

	origBinary := "/opt/pi/bin/pi"
	origArgs := []string{"--mode=json"}

	for _, backend := range []string{"none", ""} {
		cfg := daemon.SandboxConfig{Backend: backend, Harnesses: []string{"pi"}}
		in := hkr4p0lexecProfileInput(t, "hkr4p0lexec-none-run")
		spawn := daemon.ExportedSandboxSpawnForRun(cfg, core.AgentTypePi, in)
		if spawn != nil {
			t.Fatalf("backend=%q: gate must return nil (sandbox off)", backend)
		}
		gotBinary, gotArgs, err := daemon.ExportedSandboxWrapExecArgv(spawn, origBinary, origArgs)
		if err != nil {
			t.Fatalf("backend=%q: sandboxWrapExecArgv: %v", backend, err)
		}
		if gotBinary != origBinary || len(gotArgs) != len(origArgs) {
			t.Errorf("backend=%q: exec-path argv changed (binary=%q args=%v); want unchanged", backend, gotBinary, gotArgs)
		}
	}
}

// TestExecPath_Pi_WasNotWrapped_BeforeFix documents the RED-before state: the
// gate producing a non-nil spawn is NOT sufficient on the exec path — the wrap
// has to be applied. Passing nil (the pre-fix exec path, which had no wrap site)
// leaves the argv un-sandboxed. This pins that the exec path is a genuine wrap
// site now, not a discard.
func TestExecPath_Pi_WasNotWrapped_BeforeFix(t *testing.T) {
	t.Parallel()

	origBinary := "/opt/pi/bin/pi"
	origArgs := []string{"--mode=json"}

	// Pre-fix: the exec path never consulted the gate, equivalent to spawn=nil
	// reaching the wrap. That yields an UN-sandboxed argv — the exact defect.
	gotBinary, gotArgs, err := daemon.ExportedSandboxWrapExecArgv(nil, origBinary, origArgs)
	if err != nil {
		t.Fatalf("sandboxWrapExecArgv(nil): %v", err)
	}
	if gotBinary == "srt" {
		t.Fatal("nil spawn must NOT wrap")
	}
	if gotBinary != origBinary || len(gotArgs) != len(origArgs) {
		t.Errorf("nil spawn changed argv (binary=%q args=%v); want unchanged pi argv — the RED state is 'no srt prefix'", gotBinary, gotArgs)
	}
}

// TestExecPath_Pi_SrtWrapCreatesClaudeTmpDir is the hk-cdpxu regression: srt
// 1.0.0 hardcodes TMPDIR=/tmp/claude for the sandboxed child regardless of the
// profile (verified empirically), and srt itself stats that directory on
// startup before the sandboxed process runs — a missing directory fails the
// spawn immediately ("stat /tmp/claude: no such file or directory"), which in
// turn breaks any tool the agent invokes that honors TMPDIR for scratch/work-dir
// creation (e.g. `go build`). sandboxWrapExecArgv (via srtWrapArgv) must create
// the directory as a side effect of wrapping a sandboxed run, not merely
// allowlist it in the profile JSON.
func TestExecPath_Pi_SrtWrapCreatesClaudeTmpDir(t *testing.T) {
	// Not t.Parallel: asserts on a fixed, shared filesystem path (/tmp/claude).
	cfg := daemon.SandboxConfig{Backend: "srt", Harnesses: []string{"pi"}}
	in := hkr4p0lexecProfileInput(t, "hkcdpxu-claude-tmpdir-run")
	spawn := daemon.ExportedSandboxSpawnForRun(cfg, core.AgentTypePi, in)
	if spawn == nil {
		t.Fatal("precondition: gate must return non-nil spawn for pi + srt + harnesses:[pi]")
	}

	if _, _, err := daemon.ExportedSandboxWrapExecArgv(spawn, "/opt/pi/bin/pi", []string{"--mode=json"}); err != nil {
		t.Fatalf("sandboxWrapExecArgv: %v", err)
	}

	fi, statErr := os.Stat("/tmp/claude")
	if statErr != nil {
		t.Fatalf("hk-cdpxu: sandboxWrapExecArgv did not create /tmp/claude: %v", statErr)
	}
	if !fi.IsDir() {
		t.Fatalf("hk-cdpxu: /tmp/claude exists but is not a directory")
	}
}
