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
	"encoding/json"
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

	// hk-gglt: the trust upsert MUST run `python3 - <worktreePath>` with the
	// program piped on STDIN, NOT `python3 -c <prog>`. Rationale: tmux.SSHRunner
	// emits `ssh <host> -- python3 -c <prog> <path>`; the ssh client space-joins
	// the argv and the worker's login shell re-splits it, shredding a multi-line
	// `-c` program so python never runs the upsert and the worker's ~/.claude.json
	// never gets the worktree trust key → untrusted per-run worktree → Claude's
	// trust/bypass modal → daemon paste-Enter selects "No, exit" → no_commit.
	// Feeding the program on stdin keeps its bytes off the remote command line.
	//
	rr := newNoOpRecorder()

	if err := EnsureWorktreeTrustVia(ctx, rr, z8ekWorkerWt); err != nil {
		t.Fatalf("EnsureWorktreeTrustVia (remote): %v", err)
	}

	if len(rr.Calls) != 1 {
		t.Fatalf("expected exactly 1 runner call, got %d: %v", len(rr.Calls), rr.Calls)
	}
	call := rr.Calls[0]
	if call.Name != "python3" {
		t.Fatalf("trust call.Name = %q, want python3", call.Name)
	}
	// Argv must be `- <worktreePath>` — `-` = read program from stdin (NOT `-c`).
	if len(call.Args) != 2 || call.Args[0] != "-" {
		t.Fatalf("trust call.Args = %v, want [- <worktreePath>] (program via stdin, not -c)", call.Args)
	}
	if call.Args[1] != z8ekWorkerWt {
		t.Errorf("trust worktree arg = %q, want %q", call.Args[1], z8ekWorkerWt)
	}
	// Regression guard: the program must NOT travel as a `-c` argv token.
	for _, a := range call.Args {
		if a == "-c" {
			t.Errorf("trust call still passes the program via -c (hk-gglt regression): args=%v", call.Args)
		}
	}

	// The program piped on stdin must upsert hasTrustDialogAccepted under
	// projects, keyed by a realpath-normalized worktree path, into the worker HOME
	// ~/.claude.json. The helper pipes the package constant verbatim on stdin
	// (proven by the `-` argv above); assert the constant carries the contract.
	prog := workerTrustUpsertProgram
	for _, want := range []string{"hasTrustDialogAccepted", "projects", "realpath", ".claude.json", "sys.argv[1]"} {
		if !strings.Contains(prog, want) {
			t.Errorf("trust program (stdin) missing %q:\n%s", want, prog)
		}
	}
}

// TestEnsureWorktreeTrustVia_PipesProgramOnStdin physically proves the trust
// program bytes reach the python3 process's STDIN (not the command line). The
// CmdFunc substitutes `cat`, capturing what the helper pipes via cmd.Stdin into
// a buffer; the captured bytes must equal the package trust program (hk-gglt).
func TestEnsureWorktreeTrustVia_PipesProgramOnStdin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Substitute `sh -c 'cat > <capFile>'` for python3 so the bytes the helper
	// pipes on cmd.Stdin are persisted to capFile (CombinedOutput sets stdout/
	// stderr, which the redirect ignores). capFile then holds exactly what reached
	// the process stdin.
	capFile := filepath.Join(t.TempDir(), "stdin.txt")
	rr := &tmux.RecordingRunner{
		CmdFunc: func(c context.Context, name string, _ ...string) *exec.Cmd {
			if name == "python3" {
				return exec.CommandContext(c, "sh", "-c", "cat > "+capFile)
			}
			return exec.Command("true")
		},
	}

	if err := EnsureWorktreeTrustVia(ctx, rr, z8ekWorkerWt); err != nil {
		t.Fatalf("EnsureWorktreeTrustVia: %v", err)
	}
	got, err := os.ReadFile(capFile)
	if err != nil {
		t.Fatalf("read captured stdin: %v (nothing piped on stdin?)", err)
	}
	if string(got) != workerTrustUpsertProgram {
		t.Errorf("stdin-piped program mismatch.\n got: %q\nwant: %q", got, workerTrustUpsertProgram)
	}
}

// TestEnsureWorktreeTrustVia_RealPythonWritesTrust runs the ACTUAL trust upsert
// path end-to-end against a real python3 with HOME redirected to a temp dir,
// proving the program — fed on stdin to `python3 - <path>` — lands
// projects[<realpath>].hasTrustDialogAccepted=true in <HOME>/.claude.json. This
// is the regression that the prior `-c` form silently failed over SSH (hk-gglt).
func TestEnsureWorktreeTrustVia_RealPythonWritesTrust(t *testing.T) {
	// Not parallel: mutates HOME for the duration of the call.
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	ctx := context.Background()
	home := t.TempDir()
	wt := filepath.Join(t.TempDir(), "worktrees", "run-gglt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}

	// A LocalRunner that forces HOME=<temp> so the python program's
	// os.path.expanduser("~") resolves to the temp dir, mirroring the worker's
	// HOME resolution without touching the real ~/.claude.json.
	rr := &tmux.RecordingRunner{
		CmdFunc: func(c context.Context, name string, args ...string) *exec.Cmd {
			cmd := exec.CommandContext(c, name, args...)
			cmd.Env = append(os.Environ(), "HOME="+home)
			return cmd
		},
	}

	if err := EnsureWorktreeTrustVia(ctx, rr, wt); err != nil {
		t.Fatalf("EnsureWorktreeTrustVia (real python): %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatalf("read temp ~/.claude.json: %v (the upsert did not run — the bug)", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse ~/.claude.json: %v\n%s", err, data)
	}
	projects, _ := cfg["projects"].(map[string]interface{})
	if projects == nil {
		t.Fatalf("no projects map in written config:\n%s", data)
	}
	// Key is the realpath-normalized worktree path.
	realWt, _ := filepath.EvalSymlinks(wt)
	entry, _ := projects[realWt].(map[string]interface{})
	if entry == nil {
		entry, _ = projects[wt].(map[string]interface{})
	}
	if entry == nil {
		t.Fatalf("no trust entry for worktree %q (or %q) in:\n%s", realWt, wt, data)
	}
	if trusted, _ := entry["hasTrustDialogAccepted"].(bool); !trusted {
		t.Errorf("hasTrustDialogAccepted not true for worktree entry:\n%s", data)
	}
}

// TestWriteReviewTargetVia_Remote asserts the reviewer brief is routed THROUGH
// the runner onto the worker-side worktree with content byte-identical to the
// local builder (the DOT-mode remote reviewer defect: WriteReviewTarget wrote the
// brief box-A-local, so the worker reviewer never saw it → no verdict).
func TestWriteReviewTargetVia_Remote(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rr := newNoOpRecorder()

	payload := ReviewTargetPayload{
		WorkspacePath: z8ekWorkerWt,
		BeadID:        "hk-z8ek",
		Iteration:     2,
		BeadTitle:     "remote reviewer brief",
		BeadBody:      "Review the SSH-aware review-target routing.",
		BaseSHA:       "aaaaaaa",
		HeadSHA:       "bbbbbbb",
	}
	if err := WriteReviewTargetVia(ctx, rr, payload); err != nil {
		t.Fatalf("WriteReviewTargetVia (remote): %v", err)
	}

	if len(rr.Calls) != 1 {
		t.Fatalf("expected exactly 1 runner call, got %d: %v", len(rr.Calls), rr.Calls)
	}
	content, dest := decodeRemoteWriteContent(t, rr.Calls[0])

	wantDest := filepath.Join(z8ekWorkerWt, ".harmonik", "review-target.md")
	if dest != wantDest {
		t.Errorf("review-target dest = %q, want %q", dest, wantDest)
	}
	// Parity: byte-identical to the local builder output.
	if content != buildReviewTargetContent(payload) {
		t.Errorf("remote review-target content differs from local builder output")
	}
	for _, want := range []string{"Review target — bead hk-z8ek, iteration 2", "READ-ONLY reviewer"} {
		if !strings.Contains(content, want) {
			t.Errorf("review-target.md missing %q:\n%s", want, content)
		}
	}
}

// TestWriteReviewTargetVia_LocalDelegates asserts that with a nil runner the
// helper writes to box A's local filesystem (byte-identical to WriteReviewTarget)
// and issues NO runner call.
func TestWriteReviewTargetVia_LocalDelegates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	wt := t.TempDir()

	payload := ReviewTargetPayload{
		WorkspacePath: wt,
		BeadID:        "hk-z8ek",
		Iteration:     1,
		BeadTitle:     "local reviewer brief",
		BeadBody:      "local body",
	}
	if err := WriteReviewTargetVia(ctx, nil, payload); err != nil {
		t.Fatalf("WriteReviewTargetVia (local): %v", err)
	}
	got, err := os.ReadFile(ReviewTargetPath(wt))
	if err != nil {
		t.Fatalf("local review-target.md not written: %v", err)
	}
	if string(got) != buildReviewTargetContent(payload) {
		t.Errorf("local review-target content differs from builder output")
	}
}

// TestRemoveReviewVerdictVia_Remote asserts stale-verdict cleanup is routed as an
// `rm -f` of the worker-side review.json THROUGH the runner (box-A os.Remove would
// no-op on the worker, leaving a stale verdict for the reviewer's next iteration).
func TestRemoveReviewVerdictVia_Remote(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rr := newNoOpRecorder()

	if err := RemoveReviewVerdictVia(ctx, rr, z8ekWorkerWt); err != nil {
		t.Fatalf("RemoveReviewVerdictVia (remote): %v", err)
	}
	if len(rr.Calls) != 1 {
		t.Fatalf("expected exactly 1 runner call, got %d: %v", len(rr.Calls), rr.Calls)
	}
	call := rr.Calls[0]
	if call.Name != "sh" || len(call.Args) != 2 || call.Args[0] != "-lc" {
		t.Fatalf("remove call = %s %v, want sh [-lc <script>]", call.Name, call.Args)
	}
	wantPath := ReviewVerdictPath(z8ekWorkerWt)
	if !strings.Contains(call.Args[1], "rm -f") || !strings.Contains(call.Args[1], wantPath) {
		t.Errorf("remove script = %q, want `rm -f` of %q", call.Args[1], wantPath)
	}
}

// TestRemoveReviewVerdictVia_LocalDelegates asserts the nil-runner path uses
// os.Remove on box A and issues NO runner call (tolerating a missing file).
func TestRemoveReviewVerdictVia_LocalDelegates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	wt := t.TempDir()

	// Create a stale verdict, then remove it via the local path.
	verdictPath := ReviewVerdictPath(wt)
	if err := os.MkdirAll(filepath.Dir(verdictPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(verdictPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write stale verdict: %v", err)
	}
	if err := RemoveReviewVerdictVia(ctx, nil, wt); err != nil {
		t.Fatalf("RemoveReviewVerdictVia (local): %v", err)
	}
	if _, err := os.Stat(verdictPath); !os.IsNotExist(err) {
		t.Errorf("local review.json not removed: stat err = %v", err)
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
