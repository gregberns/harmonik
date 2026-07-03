package daemon_test

// tmuxsubstrate_srt_wrap_hkrlxgx_test.go — unit tests for the srt argv-wrap
// in perRunSubstrate.SpawnWindow (hk-rlxgx).
//
// # What is tested
//
//   - TestSrtWrap_NilSandboxSpawn_ArgvPassthrough: when sandboxSpawn is nil
//     (today's default for all harnesses), the argv reaches the adapter
//     unchanged — no "srt" prefix, no profile file. (No-op gate.)
//
//   - TestSrtWrap_SrtBinaryPrepended: when sandboxSpawn is set with a valid
//     ProfileInput, SpawnWindow prepends [srtBinary, "--settings", profilePath]
//     to the agent argv before handing off to the inner substrate.
//
//   - TestSrtWrap_EmptySrtBinary_DefaultsToSrt: when SrtBinary is empty,
//     the literal string "srt" is used as the binary name.
//
//   - TestSrtWrap_ProfileFileIsValidJSON: the settings file written by
//     buildSrtArgv is valid JSON (exercises the GenerateSandboxProfile path).
//
// # Helper prefix
//
// Helpers use the prefix "hkrlxgx" to avoid redeclaration collisions with
// parallel daemon test beads (implementer-protocol.md §Helper-prefix discipline).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// hkrlxgxAdapter — captures NewWindowIn command for inspection
// ─────────────────────────────────────────────────────────────────────────────

type hkrlxgxAdapter struct {
	capturedCommand string
	panePIDResult   int
}

func (a *hkrlxgxAdapter) ProbeTmux(_ context.Context) error       { return nil }
func (a *hkrlxgxAdapter) ListSessions(_ context.Context) ([]string, error) {
	return nil, nil
}
func (a *hkrlxgxAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (a *hkrlxgxAdapter) NewWindowIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.capturedCommand = params.Command
	return tmux.Outcome{Handle: tmux.WindowHandle("hkrlxgx-session:hkrlxgx-window")}
}
func (a *hkrlxgxAdapter) KillWindow(_ context.Context, _ tmux.WindowHandle) error { return nil }
func (a *hkrlxgxAdapter) WindowPanePID(_ context.Context, _ tmux.WindowHandle) (int, error) {
	return a.panePIDResult, nil
}
func (a *hkrlxgxAdapter) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return "", nil
}
func (a *hkrlxgxAdapter) KillSession(_ context.Context, _ string) error  { return nil }
func (a *hkrlxgxAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error { return nil }
func (a *hkrlxgxAdapter) PasteBuffer(_ context.Context, _, _ string) error       { return nil }
func (a *hkrlxgxAdapter) SendKeysLiteral(_ context.Context, _, _ string) error   { return nil }
func (a *hkrlxgxAdapter) SendKeysEnter(_ context.Context, _ string) error        { return nil }
func (a *hkrlxgxAdapter) SendKeysQuit(_ context.Context, _ string) error         { return nil }
func (a *hkrlxgxAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error { return nil }

var _ tmux.Adapter = (*hkrlxgxAdapter)(nil)

// hkrlxgxMinimalProfileInput returns a SandboxProfileInput that satisfies all
// REQUIRED fields so GenerateSandboxProfile succeeds without touching real FS paths.
func hkrlxgxMinimalProfileInput(t *testing.T) daemon.SandboxProfileInput {
	t.Helper()
	tmp := t.TempDir()
	return daemon.SandboxProfileInput{
		WorktreePath:   tmp,
		GitDir:         filepath.Join(tmp, ".git"),
		RunID:          "hkrlxgx-test-run-001",
		DaemonSockPath: filepath.Join(tmp, "daemon.sock"),
	}
}

// hkrlxgxSpawn returns a baseline SubstrateSpawn with the given argv.
func hkrlxgxSpawn(t *testing.T, argv []string) handler.SubstrateSpawn {
	t.Helper()
	return handler.SubstrateSpawn{
		WindowName: "hkrlxgx-window",
		Cwd:        t.TempDir(),
		Argv:       argv,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestSrtWrap_NilSandboxSpawn_ArgvPassthrough verifies the no-op gate: when
// sandboxSpawn is nil (today's default for every harness), the argv passes
// through to the adapter unchanged — no "srt" prefix, no profile file.
func TestSrtWrap_NilSandboxSpawn_ArgvPassthrough(t *testing.T) {
	t.Parallel()

	adapter := &hkrlxgxAdapter{panePIDResult: 10}
	sub := daemon.NewTmuxSubstrate(adapter, "hkrlxgx-session")
	// ExportedNewPerRunSubstrate leaves sandboxSpawn nil → no-op.
	prs := daemon.ExportedNewPerRunSubstrate(sub)
	if prs == nil {
		t.Fatal("ExportedNewPerRunSubstrate returned nil")
	}

	_, err := prs.SpawnWindow(t.Context(), hkrlxgxSpawn(t, []string{"pi", "--mode=json"}))
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	cmd := adapter.capturedCommand
	if strings.Contains(cmd, "srt") {
		t.Errorf("NilSandboxSpawn: command %q must not contain 'srt'", cmd)
	}
	if !strings.Contains(cmd, "'pi'") {
		t.Errorf("NilSandboxSpawn: command %q must contain 'pi' (the agent binary)", cmd)
	}
}

// TestSrtWrap_SrtBinaryPrepended verifies that when sandboxSpawn is set with a
// valid ProfileInput, SpawnWindow prepends [srtBinary, "--settings", profilePath]
// to the agent argv. The profile path token appears in the captured command.
func TestSrtWrap_SrtBinaryPrepended(t *testing.T) {
	t.Parallel()

	adapter := &hkrlxgxAdapter{panePIDResult: 20}
	sub := daemon.NewTmuxSubstrate(adapter, "hkrlxgx-session")

	cfg := &daemon.ExportedSrtSpawnConfig{
		SrtBinary:    "/usr/local/bin/srt",
		ProfileInput: hkrlxgxMinimalProfileInput(t),
	}
	prs := daemon.ExportedNewPerRunSubstrateWithSandbox(sub, cfg)
	if prs == nil {
		t.Fatal("ExportedNewPerRunSubstrateWithSandbox returned nil")
	}

	agentArgv := []string{"pi", "--mode=json", "--session-id", "abc"}
	_, err := prs.SpawnWindow(t.Context(), hkrlxgxSpawn(t, agentArgv))
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	cmd := adapter.capturedCommand

	// srt binary must appear first.
	if !strings.HasPrefix(cmd, "'/usr/local/bin/srt'") {
		t.Errorf("SrtBinaryPrepended: command %q must start with \"'/usr/local/bin/srt'\"", cmd)
	}

	// --settings flag must appear.
	if !strings.Contains(cmd, "'--settings'") {
		t.Errorf("SrtBinaryPrepended: command %q must contain '--settings'", cmd)
	}

	// harmonik-srt-<runID>.json must appear in the command.
	if !strings.Contains(cmd, "harmonik-srt-"+cfg.ProfileInput.RunID) {
		t.Errorf("SrtBinaryPrepended: command %q must contain profile filename with run ID %q",
			cmd, cfg.ProfileInput.RunID)
	}

	// Agent argv elements must still appear after the srt prefix.
	if !strings.Contains(cmd, "'pi'") {
		t.Errorf("SrtBinaryPrepended: command %q must still contain agent binary 'pi'", cmd)
	}
	if !strings.Contains(cmd, "'--mode=json'") {
		t.Errorf("SrtBinaryPrepended: command %q must still contain '--mode=json'", cmd)
	}
}

// TestSrtWrap_EmptySrtBinary_DefaultsToSrt verifies that an empty SrtBinary
// in SrtSpawnConfig is resolved to the literal string "srt" (PATH lookup).
func TestSrtWrap_EmptySrtBinary_DefaultsToSrt(t *testing.T) {
	t.Parallel()

	adapter := &hkrlxgxAdapter{panePIDResult: 30}
	sub := daemon.NewTmuxSubstrate(adapter, "hkrlxgx-session")

	cfg := &daemon.ExportedSrtSpawnConfig{
		SrtBinary:    "", // empty → should default to "srt"
		ProfileInput: hkrlxgxMinimalProfileInput(t),
	}
	prs := daemon.ExportedNewPerRunSubstrateWithSandbox(sub, cfg)
	if prs == nil {
		t.Fatal("ExportedNewPerRunSubstrateWithSandbox returned nil")
	}

	_, err := prs.SpawnWindow(t.Context(), hkrlxgxSpawn(t, []string{"pi"}))
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	cmd := adapter.capturedCommand
	if !strings.HasPrefix(cmd, "'srt'") {
		t.Errorf("EmptySrtBinary: command %q must start with \"'srt'\" (default binary name)", cmd)
	}
}

// TestSrtWrap_ProfileFileIsValidJSON verifies that the settings file written
// by buildSrtArgv is parseable JSON (exercising the GenerateSandboxProfile round-trip).
func TestSrtWrap_ProfileFileIsValidJSON(t *testing.T) {
	t.Parallel()

	adapter := &hkrlxgxAdapter{panePIDResult: 40}
	sub := daemon.NewTmuxSubstrate(adapter, "hkrlxgx-session")

	profileInput := hkrlxgxMinimalProfileInput(t)
	profileInput.RunID = "hkrlxgx-json-test-run"
	cfg := &daemon.ExportedSrtSpawnConfig{
		SrtBinary:    "srt",
		ProfileInput: profileInput,
	}
	prs := daemon.ExportedNewPerRunSubstrateWithSandbox(sub, cfg)
	if prs == nil {
		t.Fatal("ExportedNewPerRunSubstrateWithSandbox returned nil")
	}

	_, err := prs.SpawnWindow(t.Context(), hkrlxgxSpawn(t, []string{"pi"}))
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	// Reconstruct the expected profile path and verify the file is valid JSON.
	profilePath := filepath.Join(os.TempDir(), "harmonik-srt-"+profileInput.RunID+".json")
	data, readErr := os.ReadFile(profilePath)
	if readErr != nil {
		t.Fatalf("ProfileFileIsValidJSON: cannot read profile at %s: %v", profilePath, readErr)
	}
	var m map[string]interface{}
	if unmarshalErr := json.Unmarshal(data, &m); unmarshalErr != nil {
		t.Errorf("ProfileFileIsValidJSON: profile at %s is not valid JSON: %v\ncontent: %s",
			profilePath, unmarshalErr, data)
	}
	// Sanity: top-level keys match srt settings schema.
	for _, key := range []string{"network", "filesystem"} {
		if _, ok := m[key]; !ok {
			t.Errorf("ProfileFileIsValidJSON: profile missing top-level key %q", key)
		}
	}
}
