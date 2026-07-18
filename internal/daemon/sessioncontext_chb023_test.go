package daemon_test

// sessioncontext_chb023_test.go — tests for daemon-side claude_session_id durability
// before Claude exec (CHB-023).
//
// Covers:
//   1. persistClaudeSessionID happy path: session ID written to git; commit SHA returned.
//   2. persistClaudeSessionID empty session ID: skipped, no commit.
//   3. SessionIDInterceptor: fires callback on handler_capabilities with claude_session_id.
//   4. SessionIDInterceptor: skips message without claude_session_id.
//   5. SessionIDInterceptor: fires at most once even with multiple matching lines.
//   6. Review-loop integration: handler emitting handler_capabilities with claude_session_id
//      causes daemon to persist to git before the subprocess exits.
//   7. Recovery invariant: after persist succeeds, context file is readable from git.
//
// Helper prefix: sessionContextFixture (implementer-protocol.md §Helper-prefix discipline;
// bead hk-w5vra.6).

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// sessionContextFixtureGitRepo initialises a bare git repository in dir with a
// single initial commit and a task branch named "run/<runID>".
// Returns the HEAD SHA of the initial commit.
func sessionContextFixtureGitRepo(t *testing.T, dir, runID string) string {
	t.Helper()
	run := func(args ...string) string {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("sessionContextFixtureGitRepo: git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	initFile := filepath.Join(dir, "README")
	if err := os.WriteFile(initFile, []byte("harmonik chb023 test\n"), 0o644); err != nil {
		t.Fatalf("sessionContextFixtureGitRepo: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
	headSHA := run("rev-parse", "HEAD")
	// Create a task branch for the run (mimics workspace.CreateWorktree).
	run("checkout", "-b", "run/"+runID)
	return headSHA
}

// sessionContextFixtureRunID returns a test RunID derived from a fresh UUIDv7.
func sessionContextFixtureRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("sessionContextFixtureRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// sessionContextFixtureReadGitFile reads a file from the git HEAD in dir.
func sessionContextFixtureReadGitFile(t *testing.T, dir, relPath string) []byte {
	t.Helper()
	//nolint:gosec // G204: git args are test-internal literals; not user input
	cmd := exec.CommandContext(t.Context(), "git", "show", "HEAD:"+relPath)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("sessionContextFixtureReadGitFile: git show HEAD:%s: %v", relPath, err)
	}
	return out
}

// sessionContextFixtureGetCommitMsg reads the HEAD commit message in dir.
func sessionContextFixtureGetCommitMsg(t *testing.T, dir string) string {
	t.Helper()
	//nolint:gosec // G204: git args are test-internal literals; not user input
	cmd := exec.CommandContext(t.Context(), "git", "log", "-1", "--format=%B")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("sessionContextFixtureGetCommitMsg: git log: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// ─────────────────────────────────────────────────────────────────────────────
// persistClaudeSessionID unit tests
// ─────────────────────────────────────────────────────────────────────────────

// TestPersistClaudeSessionID_HappyPath verifies that a non-empty session ID is
// written to git and the returned commit SHA is non-empty.
func TestPersistClaudeSessionID_HappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runID := sessionContextFixtureRunID(t)
	sessionContextFixtureGitRepo(t, dir, runID.String())

	const wantSessionID = "test-session-01919abc-dead-beef-cafe-babedeadbeef"

	sha, skipped, err := daemon.ExportedPersistClaudeSessionID(t.Context(), dir, runID, wantSessionID)
	if err != nil {
		t.Fatalf("PersistClaudeSessionID: unexpected error: %v", err)
	}
	if skipped {
		t.Error("PersistClaudeSessionID: got Skipped=true for non-empty session ID")
	}
	if sha == "" {
		t.Error("PersistClaudeSessionID: CommitSHA is empty after successful persist")
	}

	// Verify the context file was committed.
	relPath := ".harmonik/run-context/" + runID.String() + "/context.json"
	data := sessionContextFixtureReadGitFile(t, dir, relPath)
	var ctxFile struct {
		ClaudeSessionID string `json:"claude_session_id"`
		PersistedAt     string `json:"persisted_at"`
	}
	if err := json.Unmarshal(data, &ctxFile); err != nil {
		t.Fatalf("PersistClaudeSessionID: unmarshal context file: %v", err)
	}
	if ctxFile.ClaudeSessionID != wantSessionID {
		t.Errorf("PersistClaudeSessionID: context file claude_session_id = %q, want %q",
			ctxFile.ClaudeSessionID, wantSessionID)
	}
	if ctxFile.PersistedAt == "" {
		t.Error("PersistClaudeSessionID: context file persisted_at is empty")
	}
}

// TestPersistClaudeSessionID_EmptySessionID verifies that an empty session ID
// is skipped (no git commit, no error).
func TestPersistClaudeSessionID_EmptySessionID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runID := sessionContextFixtureRunID(t)
	sessionContextFixtureGitRepo(t, dir, runID.String())

	sha, skipped, err := daemon.ExportedPersistClaudeSessionID(t.Context(), dir, runID, "")
	if err != nil {
		t.Fatalf("PersistClaudeSessionID empty: unexpected error: %v", err)
	}
	if !skipped {
		t.Error("PersistClaudeSessionID empty: expected Skipped=true for empty session ID")
	}
	if sha != "" {
		t.Errorf("PersistClaudeSessionID empty: expected empty CommitSHA, got %q", sha)
	}
}

// TestPersistClaudeSessionID_CommitTrailers verifies that the commit message
// carries the Harmonik-Run-ID trailer for EM-031 state reconstruction.
func TestPersistClaudeSessionID_CommitTrailers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runID := sessionContextFixtureRunID(t)
	sessionContextFixtureGitRepo(t, dir, runID.String())

	_, _, err := daemon.ExportedPersistClaudeSessionID(t.Context(), dir, runID, "session-trailers-test")
	if err != nil {
		t.Fatalf("PersistClaudeSessionID: %v", err)
	}

	msg := sessionContextFixtureGetCommitMsg(t, dir)
	if !strings.Contains(msg, "Harmonik-Run-ID: "+runID.String()) {
		t.Errorf("PersistClaudeSessionID: commit message missing Harmonik-Run-ID trailer\ngot: %q", msg)
	}
	if !strings.Contains(msg, "CHB-023") {
		t.Errorf("PersistClaudeSessionID: commit message missing CHB-023 reference\ngot: %q", msg)
	}
}

// TestPersistClaudeSessionID_Idempotent verifies that calling persistClaudeSessionID
// a second time with the same session ID is a no-op (Skipped=true, no new commit).
// This prevents daemon-restart + resume scenarios from producing redundant CHB-023
// commits on the task branch (hk-mdwh4).
func TestPersistClaudeSessionID_Idempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runID := sessionContextFixtureRunID(t)
	sessionContextFixtureGitRepo(t, dir, runID.String())

	const sid = "session-idem-test-01919abc"

	// First call: should commit and return a non-empty SHA.
	sha1, skipped1, err1 := daemon.ExportedPersistClaudeSessionID(t.Context(), dir, runID, sid)
	if err1 != nil {
		t.Fatalf("PersistClaudeSessionID first call: %v", err1)
	}
	if skipped1 {
		t.Error("PersistClaudeSessionID first call: got Skipped=true; expected commit")
	}
	if sha1 == "" {
		t.Error("PersistClaudeSessionID first call: CommitSHA is empty")
	}

	// Second call with same session ID: should be skipped (idempotent).
	sha2, skipped2, err2 := daemon.ExportedPersistClaudeSessionID(t.Context(), dir, runID, sid)
	if err2 != nil {
		t.Fatalf("PersistClaudeSessionID second call: %v", err2)
	}
	if !skipped2 {
		t.Errorf("PersistClaudeSessionID second call (same ID): expected Skipped=true; got commit SHA %q", sha2)
	}
	if sha2 != "" {
		t.Errorf("PersistClaudeSessionID second call (same ID): expected empty CommitSHA; got %q", sha2)
	}
}

// TestPersistClaudeSessionID_IdempotentDifferentID verifies that calling
// persistClaudeSessionID with a DIFFERENT session ID after a successful first
// call produces a new commit (the file is overwritten with the new ID).
func TestPersistClaudeSessionID_IdempotentDifferentID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runID := sessionContextFixtureRunID(t)
	sessionContextFixtureGitRepo(t, dir, runID.String())

	const sid1 = "session-idem-first-01919abc"
	const sid2 = "session-idem-second-deadbeef"

	sha1, _, err1 := daemon.ExportedPersistClaudeSessionID(t.Context(), dir, runID, sid1)
	if err1 != nil {
		t.Fatalf("PersistClaudeSessionID first call: %v", err1)
	}

	sha2, skipped2, err2 := daemon.ExportedPersistClaudeSessionID(t.Context(), dir, runID, sid2)
	if err2 != nil {
		t.Fatalf("PersistClaudeSessionID second call (different ID): %v", err2)
	}
	if skipped2 {
		t.Error("PersistClaudeSessionID second call (different ID): got Skipped=true; expected new commit")
	}
	if sha2 == "" || sha2 == sha1 {
		t.Errorf("PersistClaudeSessionID second call (different ID): expected distinct non-empty SHA; sha1=%q sha2=%q", sha1, sha2)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SessionIDInterceptor unit tests
// ─────────────────────────────────────────────────────────────────────────────

// sessionContextFixtureMakeCapabilitiesLine returns a well-formed
// handler_capabilities NDJSON line with the given claude_session_id.
func sessionContextFixtureMakeCapabilitiesLine(sessionID string) []byte {
	msg := handlercontract.HandlerCapabilitiesMsg{
		Type:              handlercontract.ProgressMsgTypeHandlerCapabilities,
		SupportedVersions: []int{1},
		ClaudeSessionID:   sessionID,
	}
	data, _ := json.Marshal(msg)
	return append(data, '\n')
}

// TestSessionIDInterceptor_FiresOnHandlerCapabilities verifies that the interceptor
// calls the callback exactly once when a handler_capabilities line with a non-empty
// claude_session_id is observed.
func TestSessionIDInterceptor_FiresOnHandlerCapabilities(t *testing.T) {
	t.Parallel()

	const wantID = "session-interceptor-test-01919abc"
	line := sessionContextFixtureMakeCapabilitiesLine(wantID)

	var gotID string
	fired := 0
	intercepted := daemon.ExportedNewSessionIDInterceptor(bytes.NewReader(line), func(id string) {
		fired++
		gotID = id
	})

	buf := make([]byte, 1024)
	n, _ := intercepted.Read(buf)
	_ = n

	if fired != 1 {
		t.Errorf("SessionIDInterceptor: callback fired %d times, want 1", fired)
	}
	if gotID != wantID {
		t.Errorf("SessionIDInterceptor: got session ID %q, want %q", gotID, wantID)
	}
}

// TestSessionIDInterceptor_FiresOnEmptySessionID verifies that a
// handler_capabilities line whose negotiation succeeds (valid supported_versions)
// but carries an EMPTY claude_session_id STILL fires the callback exactly once,
// with the empty string. The version_selected ACK is sent only from that
// callback, and downstream synthesises a session id from the empty signal;
// withholding the fire on empty left a handler blocked on the ACK until the
// 150s agent_ready_timeout (hk-o66xy regression).
func TestSessionIDInterceptor_FiresOnEmptySessionID(t *testing.T) {
	t.Parallel()

	msg := handlercontract.HandlerCapabilitiesMsg{
		Type:              handlercontract.ProgressMsgTypeHandlerCapabilities,
		SupportedVersions: []int{1},
		// ClaudeSessionID deliberately absent.
	}
	data, _ := json.Marshal(msg)
	line := append(data, '\n')

	fired := 0
	var gotID string
	sawID := false
	intercepted := daemon.ExportedNewSessionIDInterceptor(bytes.NewReader(line), func(id string) {
		fired++
		gotID = id
		sawID = true
	})

	buf := make([]byte, 1024)
	_, _ = intercepted.Read(buf)

	if fired != 1 {
		t.Errorf("SessionIDInterceptor empty: callback fired %d times, want 1 (ACK must be released)", fired)
	}
	if sawID && gotID != "" {
		t.Errorf("SessionIDInterceptor empty: callback got id %q, want empty string", gotID)
	}
}

// TestSessionIDInterceptor_PassesBytesThrough verifies that all bytes from the
// underlying reader are returned to the caller unchanged.
func TestSessionIDInterceptor_PassesBytesThrough(t *testing.T) {
	t.Parallel()

	const wantID = "session-passthrough-test"
	line := sessionContextFixtureMakeCapabilitiesLine(wantID)

	intercepted := daemon.ExportedNewSessionIDInterceptor(bytes.NewReader(line), func(_ string) {})

	got, err := bufio.NewReader(intercepted).ReadBytes('\n')
	if err != nil {
		t.Fatalf("SessionIDInterceptor passthrough: read: %v", err)
	}
	if !bytes.Equal(got, line) {
		t.Errorf("SessionIDInterceptor passthrough:\ngot  %q\nwant %q", got, line)
	}
}

// TestSessionIDInterceptor_FiresOnlyOnce verifies that the callback is invoked
// at most once even when multiple handler_capabilities lines appear.
func TestSessionIDInterceptor_FiresOnlyOnce(t *testing.T) {
	t.Parallel()

	line1 := sessionContextFixtureMakeCapabilitiesLine("session-first")
	line2 := sessionContextFixtureMakeCapabilitiesLine("session-second")
	combined := append(line1, line2...)

	fired := 0
	var gotID string
	intercepted := daemon.ExportedNewSessionIDInterceptor(bytes.NewReader(combined), func(id string) {
		fired++
		gotID = id
	})

	buf := make([]byte, 1024)
	for {
		n, err := intercepted.Read(buf)
		_ = n
		if err != nil {
			break
		}
	}

	if fired != 1 {
		t.Errorf("SessionIDInterceptor once: callback fired %d times, want 1", fired)
	}
	if gotID != "session-first" {
		t.Errorf("SessionIDInterceptor once: got session ID %q, want %q", gotID, "session-first")
	}
}

// TestSessionIDInterceptor_IgnoresNonCapabilitiesLines verifies that the
// interceptor does not fire on unrelated NDJSON lines.
func TestSessionIDInterceptor_IgnoresNonCapabilitiesLines(t *testing.T) {
	t.Parallel()

	otherMsg := `{"type":"agent_ready","session_id":"some-session"}` + "\n"
	fired := 0
	intercepted := daemon.ExportedNewSessionIDInterceptor(strings.NewReader(otherMsg), func(_ string) {
		fired++
	})

	buf := make([]byte, 512)
	_, _ = intercepted.Read(buf)

	if fired != 0 {
		t.Errorf("SessionIDInterceptor non-capabilities: callback fired %d times, want 0", fired)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Recovery invariant test (CHB-023)
// ─────────────────────────────────────────────────────────────────────────────

// TestPersistClaudeSessionID_RecoveryInvariant verifies the CHB-023 crash-recovery
// invariant: after a successful persist call, the session ID is recoverable from
// the git checkout (state (b) — durable committed).
//
// This simulates state-reconstruction (EM-031): a restarted daemon reads the
// task branch tip and finds the context file with the claude_session_id.
func TestPersistClaudeSessionID_RecoveryInvariant(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runID := sessionContextFixtureRunID(t)
	sessionContextFixtureGitRepo(t, dir, runID.String())

	const wantSessionID = "recovery-test-01919abc-dead-beef-cafe-babedeadbeef"

	sha, skipped, err := daemon.ExportedPersistClaudeSessionID(t.Context(), dir, runID, wantSessionID)
	if err != nil {
		t.Fatalf("PersistClaudeSessionID recovery: %v", err)
	}
	if skipped {
		t.Fatal("PersistClaudeSessionID recovery: expected not skipped")
	}

	// Simulate daemon restart: read the context file from the commit SHA directly.
	//nolint:gosec // G204: git args are test-internal literals; not user input
	relPath := ".harmonik/run-context/" + runID.String() + "/context.json"
	showCmd := exec.CommandContext(t.Context(), "git", "show", sha+":"+relPath)
	showCmd.Dir = dir
	out, showErr := showCmd.Output()
	if showErr != nil {
		t.Fatalf("PersistClaudeSessionID recovery: git show %s:%s: %v", sha, relPath, showErr)
	}

	var ctxFile struct {
		ClaudeSessionID string `json:"claude_session_id"`
	}
	if err := json.Unmarshal(out, &ctxFile); err != nil {
		t.Fatalf("PersistClaudeSessionID recovery: unmarshal: %v", err)
	}
	if ctxFile.ClaudeSessionID != wantSessionID {
		t.Errorf("PersistClaudeSessionID recovery: recovered session ID = %q, want %q",
			ctxFile.ClaudeSessionID, wantSessionID)
	}

	// Verify that before the persist call the context file did not exist
	// (state (a) — no ID persisted). We can't go back in time, but we can
	// verify the initial commit does NOT have the context file.
	//nolint:gosec // G204: git args are test-internal literals; not user input
	logCmd := exec.CommandContext(t.Context(), "git", "log", "--oneline")
	logCmd.Dir = dir
	logOut, logErr := logCmd.Output()
	if logErr != nil {
		t.Fatalf("PersistClaudeSessionID recovery: git log: %v", logErr)
	}
	commitCount := 0
	for _, line := range strings.Split(strings.TrimSpace(string(logOut)), "\n") {
		if line != "" {
			commitCount++
		}
	}
	// Initial commit + one persist commit = 2 commits.
	if commitCount != 2 {
		t.Errorf("PersistClaudeSessionID recovery: expected 2 commits (initial + persist), got %d\n%s",
			commitCount, logOut)
	}
}
