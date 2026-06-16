package workspace

// remotematerialize_hkz8ek_test.go — gate-runnable tests for the SSH-aware
// claude-launch materialization (hk-z8ek). All remote writes are intercepted by
// a RecordingRunner; NO real ssh and NO real worker are touched.
//
// Coverage:
//   - REMOTE (runner != nil): each of the three writes is issued THROUGH the
//     runner, targets the worker-side worktree/HOME, and the settings.json
//     content carries the correct hook command (worker harmonik path) and the
//     HARMONIK_DAEMON_SOCKET-bearing hook wiring.
//   - LOCAL (runner == nil): each *Via helper delegates to the existing local FS
//     path — the file lands on box A's local disk, no runner call recorded.

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// newNoOpRecorder returns a RecordingRunner whose CmdFunc delegates to
// exec.Command("true") so commands always succeed without side effects.
func newNoOpRecorder() *tmux.RecordingRunner {
	return &tmux.RecordingRunner{
		CmdFunc: func(_ context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.Command("true")
		},
	}
}

const (
	z8ekWorkerWt  = "/Users/gb/harmonik-worker/repo/.harmonik/worktrees/019ec83c-z8ek-7001-0001-000000000001"
	z8ekWorkerBin = "/Users/gb/go/bin/harmonik"
)

// decodeRemoteWriteContent extracts the original file content from a recorded
// `sh -lc "... printf %s <b64> | base64 -d > <path>"` command. Returns the
// decoded content and the destination path. Fails the test if the call does not
// match the expected remote-write shape.
func decodeRemoteWriteContent(t *testing.T, call tmux.RecordingCall) (content string, dest string) {
	t.Helper()
	if call.Name != "sh" {
		t.Fatalf("remote write: call.Name = %q, want sh", call.Name)
	}
	if len(call.Args) != 2 || call.Args[0] != "-lc" {
		t.Fatalf("remote write: call.Args = %v, want [-lc <script>]", call.Args)
	}
	script := call.Args[1]
	// Extract the base64 blob between "printf %s '" and "' | base64 -d".
	const pfx = "printf %s '"
	i := strings.Index(script, pfx)
	if i < 0 {
		t.Fatalf("remote write: no printf in script: %q", script)
	}
	rest := script[i+len(pfx):]
	j := strings.Index(rest, "'")
	if j < 0 {
		t.Fatalf("remote write: unterminated base64 in script: %q", script)
	}
	b64 := rest[:j]
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("remote write: base64 decode: %v (blob=%q)", err, b64)
	}
	// Destination path is the last single-quoted token after "> '".
	const redir = "> '"
	k := strings.LastIndex(script, redir)
	if k < 0 {
		t.Fatalf("remote write: no redirect in script: %q", script)
	}
	dtail := script[k+len(redir):]
	d := strings.Index(dtail, "'")
	if d < 0 {
		t.Fatalf("remote write: unterminated dest in script: %q", script)
	}
	return string(raw), dtail[:d]
}

func TestMaterializeClaudeSettingsVia_Remote(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rr := newNoOpRecorder()

	if err := MaterializeClaudeSettingsVia(ctx, rr, z8ekWorkerWt, z8ekWorkerBin, ""); err != nil {
		t.Fatalf("MaterializeClaudeSettingsVia (remote): %v", err)
	}

	if len(rr.Calls) != 1 {
		t.Fatalf("expected exactly 1 runner call, got %d: %v", len(rr.Calls), rr.Calls)
	}
	content, dest := decodeRemoteWriteContent(t, rr.Calls[0])

	wantDest := filepath.Join(z8ekWorkerWt, ".claude", "settings.json")
	if dest != wantDest {
		t.Errorf("settings dest = %q, want %q", dest, wantDest)
	}
	// The hook command MUST be the WORKER's harmonik path (not box A's).
	if !strings.Contains(content, z8ekWorkerBin) {
		t.Errorf("settings.json does not carry worker hook command %q:\n%s", z8ekWorkerBin, content)
	}
	// Bridge hooks + hook-relay wiring must be present.
	for _, want := range []string{"hooks", "hook-relay", "SessionStart", "Stop"} {
		if !strings.Contains(content, want) {
			t.Errorf("settings.json missing %q:\n%s", want, content)
		}
	}

	// Byte-identical to the local "file-absent" content (parity guard).
	wantContent, err := marshalSettings(buildBridgeOnlySettings(z8ekWorkerBin))
	if err != nil {
		t.Fatalf("marshalSettings: %v", err)
	}
	if content != string(wantContent) {
		t.Errorf("remote settings content differs from local bridge-only content.\n got: %q\nwant: %q", content, string(wantContent))
	}
}

func TestWriteAgentTaskVia_Remote(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rr := newNoOpRecorder()

	payload := AgentTaskPayload{
		BeadID:        "hk-z8ek",
		Title:         "remote materialization",
		Phase:         "",
		Iteration:     1,
		RunID:         "019ec83c-z8ek-7001-0001-000000000001",
		WorkspacePath: z8ekWorkerWt,
		Body:          "Implement the SSH-aware materialization seam.",
	}
	if err := WriteAgentTaskVia(ctx, rr, z8ekWorkerWt, payload); err != nil {
		t.Fatalf("WriteAgentTaskVia (remote): %v", err)
	}

	if len(rr.Calls) != 1 {
		t.Fatalf("expected exactly 1 runner call, got %d: %v", len(rr.Calls), rr.Calls)
	}
	content, dest := decodeRemoteWriteContent(t, rr.Calls[0])

	wantDest := filepath.Join(z8ekWorkerWt, ".harmonik", "agent-task.md")
	if dest != wantDest {
		t.Errorf("agent-task dest = %q, want %q", dest, wantDest)
	}
	for _, want := range []string{"bead_id: hk-z8ek", "Implement the SSH-aware materialization seam.", "Session Completion"} {
		if !strings.Contains(content, want) {
			t.Errorf("agent-task.md missing %q:\n%s", want, content)
		}
	}

	// Parity: byte-identical to the local builder output.
	if content != buildAgentTaskContent(payload) {
		t.Errorf("remote agent-task content differs from local builder output")
	}
}

func TestWriteAgentTaskVia_Remote_EmptyBodyRejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rr := newNoOpRecorder()

	err := WriteAgentTaskVia(ctx, rr, z8ekWorkerWt, AgentTaskPayload{
		BeadID: "hk-z8ek", WorkspacePath: z8ekWorkerWt, Body: "   ",
	})
	if err == nil {
		t.Fatal("WriteAgentTaskVia (remote, empty body): want error, got nil")
	}
	if len(rr.Calls) != 0 {
		t.Errorf("empty body should issue NO runner call, got %d: %v", len(rr.Calls), rr.Calls)
	}
}

func TestEnsureWorktreeTrustVia_Remote(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rr := newNoOpRecorder()

	if err := EnsureWorktreeTrustVia(ctx, rr, z8ekWorkerWt); err != nil {
		t.Fatalf("EnsureWorktreeTrustVia (remote): %v", err)
	}

	if len(rr.Calls) != 1 {
		t.Fatalf("expected exactly 1 runner call, got %d: %v", len(rr.Calls), rr.Calls)
	}
	call := rr.Calls[0]
	// The trust upsert runs python3 -c <prog> <worktreePath> ON THE WORKER.
	if call.Name != "python3" {
		t.Fatalf("trust call.Name = %q, want python3", call.Name)
	}
	if len(call.Args) != 3 || call.Args[0] != "-c" {
		t.Fatalf("trust call.Args = %v, want [-c <prog> <worktreePath>]", call.Args)
	}
	if call.Args[2] != z8ekWorkerWt {
		t.Errorf("trust worktree arg = %q, want %q", call.Args[2], z8ekWorkerWt)
	}
	// The program must upsert hasTrustDialogAccepted under projects, keyed by a
	// realpath-normalized worktree path, into the worker HOME ~/.claude.json.
	prog := call.Args[1]
	for _, want := range []string{"hasTrustDialogAccepted", "projects", "realpath", ".claude.json"} {
		if !strings.Contains(prog, want) {
			t.Errorf("trust program missing %q:\n%s", want, prog)
		}
	}
}

// TestVia_LocalDelegatesToLocalFS asserts that with a nil runner each *Via
// helper writes to box A's local filesystem (NFR7 byte-identical path) and
// issues NO runner call.
func TestVia_LocalDelegatesToLocalFS(t *testing.T) {
	ctx := context.Background()
	wt := t.TempDir()

	// Redirect the trust config to a temp file so EnsureWorktreeTrust (local
	// path) never touches the real ~/.claude.json.
	cfgPath := filepath.Join(t.TempDir(), ".claude.json")
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", cfgPath)

	// settings.json — local path writes to <wt>/.claude/settings.json.
	if err := MaterializeClaudeSettingsVia(ctx, nil, wt, "/box-a/harmonik", ""); err != nil {
		t.Fatalf("MaterializeClaudeSettingsVia (local): %v", err)
	}
	settingsPath := filepath.Join(wt, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); err != nil {
		t.Errorf("local settings.json not written: %v", err)
	}

	// agent-task.md — local path writes to <wt>/.harmonik/agent-task.md.
	payload := AgentTaskPayload{
		BeadID: "hk-z8ek", Title: "local", Iteration: 1,
		RunID: "019ec83c-z8ek-7001-0001-000000000002", WorkspacePath: wt,
		Body: "local body",
	}
	if err := WriteAgentTaskVia(ctx, nil, wt, payload); err != nil {
		t.Fatalf("WriteAgentTaskVia (local): %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, ".harmonik", "agent-task.md")); err != nil {
		t.Errorf("local agent-task.md not written: %v", err)
	}

	// trust — local path upserts the temp ~/.claude.json.
	if err := EnsureWorktreeTrustVia(ctx, nil, wt); err != nil {
		t.Fatalf("EnsureWorktreeTrustVia (local): %v", err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read local trust config: %v", err)
	}
	if !strings.Contains(string(data), "hasTrustDialogAccepted") {
		t.Errorf("local trust config missing hasTrustDialogAccepted:\n%s", data)
	}
}
