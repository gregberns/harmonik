// Tests for harmonik-twin-codex (codex-harness C6/T15, hk-of3h4).
//
// Coverage:
//   - Four scenario variants: trailer-commit, edits-no-commit, no-edits, turn-failed.
//   - JSONL output shape: thread.started always first, correct terminal event.
//   - Git side effects: trailer-commit produces a Refs: commit; edits-no-commit
//     leaves uncommitted dirt; no-edits leaves the worktree clean.
//   - run() entry-point: --version, --scenario required, unknown scenario → exit 1.
//
// Test helpers use the per-bead prefix codexTwinFixture per implementer-protocol.md
// §Helper-prefix discipline.
//
// Cite: codex-harness C6-migration-test-spec.md §Verification;
// codex-harness C2-codex-adapter-spec.md §AC2.3–AC2.5.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test helpers
// ─────────────────────────────────────────────────────────────────────────────

// codexTwinFixtureDecodeAll splits buf into NDJSON lines and decodes all into
// a []map[string]any.  Calls t.Fatalf on any JSON error.
func codexTwinFixtureDecodeAll(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	raw := buf.String()
	if raw == "" {
		return nil
	}
	parts := bytes.Split(bytes.TrimRight([]byte(raw), "\n"), []byte("\n"))
	out := make([]map[string]any, 0, len(parts))
	for i, part := range parts {
		if len(part) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(part, &m); err != nil {
			t.Fatalf("codexTwinFixtureDecodeAll: line %d unmarshal: %v — raw: %q", i, err, string(part))
		}
		out = append(out, m)
	}
	return out
}

// codexTwinFixtureEmitter returns a codexEmitter writing to a fresh bytes.Buffer.
func codexTwinFixtureEmitter(t *testing.T) (*codexEmitter, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return newCodexEmitter(&buf), &buf
}

// codexTwinFixtureGitRepo initialises a bare git repo in t.TempDir and returns
// its path.  The initial empty commit is required so that HEAD is valid and git
// operations have a base commit to append to.
func codexTwinFixtureGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	gitEnv := append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.local",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.local",
	)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = gitEnv
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init")
	run("commit", "--allow-empty", "-m", "init")
	return dir
}

// codexTwinFixtureGitLog returns the one-line log of the HEAD commit in dir.
func codexTwinFixtureGitLog(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "log", "--oneline", "-1")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// codexTwinFixtureGitCommitMsg returns the full commit message of HEAD in dir.
func codexTwinFixtureGitCommitMsg(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "log", "--format=%B", "-1")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log --format=%%B: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// codexTwinFixtureGitStatusClean returns true iff the worktree in dir has no
// uncommitted changes (tracked or untracked).
func codexTwinFixtureGitStatusClean(t *testing.T, dir string) bool {
	t.Helper()
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status --porcelain: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out)) == ""
}

// codexTwinFixtureGitStatusDirty returns true iff the worktree in dir has at
// least one uncommitted file (tracked or untracked — any porcelain output).
func codexTwinFixtureGitStatusDirty(t *testing.T, dir string) bool {
	return !codexTwinFixtureGitStatusClean(t, dir)
}

// ─────────────────────────────────────────────────────────────────────────────
// threadIDForScenario
// ─────────────────────────────────────────────────────────────────────────────

// TestThreadIDForScenarioDeterministic verifies that threadIDForScenario
// returns the same value on repeated calls for the same scenario name.
func TestThreadIDForScenarioDeterministic(t *testing.T) {
	for _, s := range []string{ScenarioTrailerCommit, ScenarioEditsNoCommit, ScenarioNoEdits, ScenarioTurnFailed} {
		a := threadIDForScenario(s)
		b := threadIDForScenario(s)
		if a != b {
			t.Errorf("threadIDForScenario(%q): got %q then %q (not deterministic)", s, a, b)
		}
		if a == "" {
			t.Errorf("threadIDForScenario(%q) returned empty string", s)
		}
	}
}

// TestThreadIDForScenarioDistinct verifies that each scenario produces a
// different thread_id so scenario tests can distinguish which variant ran.
func TestThreadIDForScenarioDistinct(t *testing.T) {
	ids := map[string]bool{}
	for _, s := range []string{ScenarioTrailerCommit, ScenarioEditsNoCommit, ScenarioNoEdits, ScenarioTurnFailed} {
		id := threadIDForScenario(s)
		if ids[id] {
			t.Errorf("threadIDForScenario(%q) = %q collides with a prior scenario", s, id)
		}
		ids[id] = true
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// codexEmitter — unit tests
// ─────────────────────────────────────────────────────────────────────────────

// TestEmitThreadStarted verifies that emitThreadStarted emits a single JSONL
// line with type=thread.started and the supplied thread_id.
func TestEmitThreadStarted(t *testing.T) {
	e, buf := codexTwinFixtureEmitter(t)
	if err := e.emitThreadStarted("test-thread-001"); err != nil {
		t.Fatalf("emitThreadStarted: %v", err)
	}
	msgs := codexTwinFixtureDecodeAll(t, buf)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if got := msgs[0]["type"].(string); got != "thread.started" {
		t.Errorf("type = %q, want thread.started", got)
	}
	if got := msgs[0]["thread_id"].(string); got != "test-thread-001" {
		t.Errorf("thread_id = %q, want test-thread-001", got)
	}
}

// TestEmitTurnCompleted verifies that emitTurnCompleted emits a single JSONL
// line with type=turn.completed and a usage object.
func TestEmitTurnCompleted(t *testing.T) {
	e, buf := codexTwinFixtureEmitter(t)
	if err := e.emitTurnCompleted(); err != nil {
		t.Fatalf("emitTurnCompleted: %v", err)
	}
	msgs := codexTwinFixtureDecodeAll(t, buf)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	m := msgs[0]
	if got := m["type"].(string); got != "turn.completed" {
		t.Errorf("type = %q, want turn.completed", got)
	}
	if _, ok := m["usage"]; !ok {
		t.Errorf("turn.completed missing usage field")
	}
}

// TestEmitTurnFailed verifies that emitTurnFailed emits a single JSONL line
// with type=turn.failed and the supplied error message.
func TestEmitTurnFailed(t *testing.T) {
	e, buf := codexTwinFixtureEmitter(t)
	if err := e.emitTurnFailed("test error message"); err != nil {
		t.Fatalf("emitTurnFailed: %v", err)
	}
	msgs := codexTwinFixtureDecodeAll(t, buf)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	m := msgs[0]
	if got := m["type"].(string); got != "turn.failed" {
		t.Errorf("type = %q, want turn.failed", got)
	}
	errObj, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatalf("turn.failed missing error object; got %T: %v", m["error"], m["error"])
	}
	if got := errObj["message"].(string); got != "test error message" {
		t.Errorf("error.message = %q, want test error message", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario: no-edits (C2 AC2.5 — noChange path)
// ─────────────────────────────────────────────────────────────────────────────

// TestScenarioNoEditsJSONL verifies that the no-edits scenario emits exactly
// two JSONL lines: thread.started then turn.completed.
func TestScenarioNoEditsJSONL(t *testing.T) {
	var buf bytes.Buffer
	if err := runScenario(&buf, ScenarioNoEdits, scenarioConfig{}); err != nil {
		t.Fatalf("runScenario(no-edits): %v", err)
	}

	msgs := codexTwinFixtureDecodeAll(t, &buf)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d: %v", len(msgs), msgs)
	}
	if got := msgs[0]["type"].(string); got != "thread.started" {
		t.Errorf("msgs[0].type = %q, want thread.started", got)
	}
	if got := msgs[1]["type"].(string); got != "turn.completed" {
		t.Errorf("msgs[1].type = %q, want turn.completed", got)
	}
}

// TestScenarioNoEditsThreadID verifies that the no-edits scenario emits the
// deterministic thread_id for that scenario.
func TestScenarioNoEditsThreadID(t *testing.T) {
	var buf bytes.Buffer
	if err := runScenario(&buf, ScenarioNoEdits, scenarioConfig{}); err != nil {
		t.Fatalf("runScenario(no-edits): %v", err)
	}
	msgs := codexTwinFixtureDecodeAll(t, &buf)
	want := threadIDForScenario(ScenarioNoEdits)
	if got := msgs[0]["thread_id"].(string); got != want {
		t.Errorf("thread_id = %q, want %q", got, want)
	}
}

// TestScenarioNoEditsGitClean verifies that the no-edits scenario leaves the
// worktree with no uncommitted changes (triggering the noChange path in the
// shared loop, C2 AC2.5).
func TestScenarioNoEditsGitClean(t *testing.T) {
	dir := codexTwinFixtureGitRepo(t)
	cfg := scenarioConfig{worktreePath: dir}

	var buf bytes.Buffer
	if err := runScenario(&buf, ScenarioNoEdits, cfg); err != nil {
		t.Fatalf("runScenario(no-edits): %v", err)
	}

	if !codexTwinFixtureGitStatusClean(t, dir) {
		t.Error("no-edits scenario left uncommitted changes in the worktree; expected clean")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario: turn-failed (C2 edge case)
// ─────────────────────────────────────────────────────────────────────────────

// TestScenarioTurnFailedJSONL verifies that the turn-failed scenario emits
// exactly two JSONL lines: thread.started then turn.failed.
func TestScenarioTurnFailedJSONL(t *testing.T) {
	var buf bytes.Buffer
	if err := runScenario(&buf, ScenarioTurnFailed, scenarioConfig{}); err != nil {
		t.Fatalf("runScenario(turn-failed): %v", err)
	}

	msgs := codexTwinFixtureDecodeAll(t, &buf)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d: %v", len(msgs), msgs)
	}
	if got := msgs[0]["type"].(string); got != "thread.started" {
		t.Errorf("msgs[0].type = %q, want thread.started", got)
	}
	if got := msgs[1]["type"].(string); got != "turn.failed" {
		t.Errorf("msgs[1].type = %q, want turn.failed", got)
	}
}

// TestScenarioTurnFailedErrorField verifies that turn.failed carries a
// non-empty error.message field.
func TestScenarioTurnFailedErrorField(t *testing.T) {
	var buf bytes.Buffer
	if err := runScenario(&buf, ScenarioTurnFailed, scenarioConfig{}); err != nil {
		t.Fatalf("runScenario(turn-failed): %v", err)
	}
	msgs := codexTwinFixtureDecodeAll(t, &buf)
	errObj, ok := msgs[1]["error"].(map[string]any)
	if !ok {
		t.Fatalf("turn.failed missing error object")
	}
	msg, _ := errObj["message"].(string)
	if msg == "" {
		t.Error("turn.failed error.message is empty")
	}
}

// TestScenarioTurnFailedNoGitSideEffects verifies that the turn-failed
// scenario leaves the worktree with no uncommitted changes.
func TestScenarioTurnFailedNoGitSideEffects(t *testing.T) {
	dir := codexTwinFixtureGitRepo(t)
	cfg := scenarioConfig{worktreePath: dir}

	var buf bytes.Buffer
	if err := runScenario(&buf, ScenarioTurnFailed, cfg); err != nil {
		t.Fatalf("runScenario(turn-failed): %v", err)
	}

	if !codexTwinFixtureGitStatusClean(t, dir) {
		t.Error("turn-failed scenario left uncommitted changes; expected clean")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario: edits-no-commit (C2 AC2.4 — adapter commit-after-exit fallback)
// ─────────────────────────────────────────────────────────────────────────────

// TestScenarioEditsNoCommitJSONL verifies that the edits-no-commit scenario
// emits thread.started then turn.completed.
func TestScenarioEditsNoCommitJSONL(t *testing.T) {
	dir := codexTwinFixtureGitRepo(t)
	var buf bytes.Buffer
	if err := runScenario(&buf, ScenarioEditsNoCommit, scenarioConfig{worktreePath: dir}); err != nil {
		t.Fatalf("runScenario(edits-no-commit): %v", err)
	}

	msgs := codexTwinFixtureDecodeAll(t, &buf)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d: %v", len(msgs), msgs)
	}
	if got := msgs[0]["type"].(string); got != "thread.started" {
		t.Errorf("msgs[0].type = %q, want thread.started", got)
	}
	if got := msgs[1]["type"].(string); got != "turn.completed" {
		t.Errorf("msgs[1].type = %q, want turn.completed", got)
	}
}

// TestScenarioEditsNoCommitLeavesUncommittedChanges verifies that the
// edits-no-commit scenario leaves at least one uncommitted file in the
// worktree (triggering the adapter's commit-after-exit fallback, C2 AC2.4).
func TestScenarioEditsNoCommitLeavesUncommittedChanges(t *testing.T) {
	dir := codexTwinFixtureGitRepo(t)
	cfg := scenarioConfig{worktreePath: dir}

	var buf bytes.Buffer
	if err := runScenario(&buf, ScenarioEditsNoCommit, cfg); err != nil {
		t.Fatalf("runScenario(edits-no-commit): %v", err)
	}

	if codexTwinFixtureGitStatusClean(t, dir) {
		t.Error("edits-no-commit scenario left no uncommitted changes; expected dirty worktree")
	}
}

// TestScenarioEditsNoCommitNoNewCommit verifies that the edits-no-commit
// scenario does NOT create a new commit (the sentinel file is untracked/unstaged,
// leaving commit responsibility to the adapter's fallback path).
func TestScenarioEditsNoCommitNoNewCommit(t *testing.T) {
	dir := codexTwinFixtureGitRepo(t)
	cfg := scenarioConfig{worktreePath: dir}

	// Capture HEAD SHA before the scenario.
	headBefore := codexTwinFixtureGitLog(t, dir)

	var buf bytes.Buffer
	if err := runScenario(&buf, ScenarioEditsNoCommit, cfg); err != nil {
		t.Fatalf("runScenario(edits-no-commit): %v", err)
	}

	headAfter := codexTwinFixtureGitLog(t, dir)
	if headBefore != headAfter {
		t.Errorf("edits-no-commit created a new commit: HEAD changed from %q to %q", headBefore, headAfter)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario: trailer-commit (C2 AC2.3 — Refs: commit on process exit)
// ─────────────────────────────────────────────────────────────────────────────

// TestScenarioTrailerCommitJSONL verifies that the trailer-commit scenario
// emits thread.started then turn.completed.
func TestScenarioTrailerCommitJSONL(t *testing.T) {
	dir := codexTwinFixtureGitRepo(t)
	var buf bytes.Buffer
	if err := runScenario(&buf, ScenarioTrailerCommit, scenarioConfig{worktreePath: dir, beadID: "hk-test01"}); err != nil {
		t.Fatalf("runScenario(trailer-commit): %v", err)
	}

	msgs := codexTwinFixtureDecodeAll(t, &buf)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d: %v", len(msgs), msgs)
	}
	if got := msgs[0]["type"].(string); got != "thread.started" {
		t.Errorf("msgs[0].type = %q, want thread.started", got)
	}
	if got := msgs[1]["type"].(string); got != "turn.completed" {
		t.Errorf("msgs[1].type = %q, want turn.completed", got)
	}
}

// TestScenarioTrailerCommitCreatesCommit verifies that the trailer-commit
// scenario creates a new commit in the worktree (C2 AC2.3: codex committed
// with Refs; shared merge lands it on process exit).
func TestScenarioTrailerCommitCreatesCommit(t *testing.T) {
	dir := codexTwinFixtureGitRepo(t)
	cfg := scenarioConfig{worktreePath: dir, beadID: "hk-test01"}

	headBefore := codexTwinFixtureGitLog(t, dir)

	var buf bytes.Buffer
	if err := runScenario(&buf, ScenarioTrailerCommit, cfg); err != nil {
		t.Fatalf("runScenario(trailer-commit): %v", err)
	}

	headAfter := codexTwinFixtureGitLog(t, dir)
	if headBefore == headAfter {
		t.Error("trailer-commit did not create a new commit; HEAD unchanged")
	}
}

// TestScenarioTrailerCommitRefsTrailer verifies that the commit created by the
// trailer-commit scenario contains a Refs: <beadID> line in the commit message.
func TestScenarioTrailerCommitRefsTrailer(t *testing.T) {
	dir := codexTwinFixtureGitRepo(t)
	const beadID = "hk-of3h4"
	cfg := scenarioConfig{worktreePath: dir, beadID: beadID}

	var buf bytes.Buffer
	if err := runScenario(&buf, ScenarioTrailerCommit, cfg); err != nil {
		t.Fatalf("runScenario(trailer-commit): %v", err)
	}

	msg := codexTwinFixtureGitCommitMsg(t, dir)
	wantTrailer := "Refs: " + beadID
	if !strings.Contains(msg, wantTrailer) {
		t.Errorf("commit message %q does not contain Refs trailer %q", msg, wantTrailer)
	}
}

// TestScenarioTrailerCommitRefsTrailerDefaultWhenNoBeadID verifies that the
// trailer-commit scenario emits a placeholder Refs trailer when --bead-id is
// not supplied (graceful degradation).
func TestScenarioTrailerCommitRefsTrailerDefaultWhenNoBeadID(t *testing.T) {
	dir := codexTwinFixtureGitRepo(t)
	cfg := scenarioConfig{worktreePath: dir} // no beadID

	var buf bytes.Buffer
	if err := runScenario(&buf, ScenarioTrailerCommit, cfg); err != nil {
		t.Fatalf("runScenario(trailer-commit, no bead-id): %v", err)
	}

	msg := codexTwinFixtureGitCommitMsg(t, dir)
	if !strings.Contains(msg, "Refs: ") {
		t.Errorf("commit message %q missing Refs: line", msg)
	}
}

// TestScenarioTrailerCommitCleanAfterCommit verifies that the worktree is
// clean after the trailer-commit scenario (sentinel file was committed, no
// leftover dirt).
func TestScenarioTrailerCommitCleanAfterCommit(t *testing.T) {
	dir := codexTwinFixtureGitRepo(t)
	cfg := scenarioConfig{worktreePath: dir, beadID: "hk-test01"}

	var buf bytes.Buffer
	if err := runScenario(&buf, ScenarioTrailerCommit, cfg); err != nil {
		t.Fatalf("runScenario(trailer-commit): %v", err)
	}

	if codexTwinFixtureGitStatusDirty(t, dir) {
		t.Error("trailer-commit left uncommitted changes after committing; expected clean")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// runScenario — error paths
// ─────────────────────────────────────────────────────────────────────────────

// TestRunScenarioUnknownScenario verifies that runScenario returns an error
// for an unrecognised scenario name.
func TestRunScenarioUnknownScenario(t *testing.T) {
	var buf bytes.Buffer
	err := runScenario(&buf, "not-a-real-scenario", scenarioConfig{})
	if err == nil {
		t.Fatal("runScenario: expected error for unknown scenario, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// run() entry-point tests
// ─────────────────────────────────────────────────────────────────────────────

// TestRunVersionFlag verifies that run() returns exit code 0 when --version
// is passed and does not require --scenario.
func TestRunVersionFlag(t *testing.T) {
	orig := os.Args
	os.Args = []string{"harmonik-twin-codex", "--version"}
	defer func() { os.Args = orig }()

	code := run()
	if code != 0 {
		t.Errorf("run() with --version returned %d, want 0", code)
	}
}

// TestRunMissingScenario verifies that run() returns exit code 1 when
// --scenario is absent.
func TestRunMissingScenario(t *testing.T) {
	orig := os.Args
	os.Args = []string{"harmonik-twin-codex"}
	defer func() { os.Args = orig }()

	code := run()
	if code != 1 {
		t.Errorf("run() without --scenario returned %d, want 1", code)
	}
}

// TestRunUnknownScenario verifies that run() returns exit code 1 for an
// unknown scenario name.
func TestRunUnknownScenario(t *testing.T) {
	orig := os.Args
	os.Args = []string{"harmonik-twin-codex", "--scenario", "not-a-real-scenario"}
	defer func() { os.Args = orig }()

	code := run()
	if code != 1 {
		t.Errorf("run() with unknown scenario returned %d, want 1", code)
	}
}

// TestRunNoEditsScenario verifies that run() returns exit code 0 for the
// no-edits scenario (no worktree required).
func TestRunNoEditsScenario(t *testing.T) {
	orig := os.Args
	os.Args = []string{"harmonik-twin-codex", "--scenario", ScenarioNoEdits}
	defer func() { os.Args = orig }()

	code := run()
	if code != 0 {
		t.Errorf("run() with no-edits scenario returned %d, want 0", code)
	}
}

// TestRunExecSubcommandStripped verifies that run() accepts the "exec" prefix
// (mirroring `codex exec --scenario no-edits`) and exits 0.
func TestRunExecSubcommandStripped(t *testing.T) {
	orig := os.Args
	os.Args = []string{"harmonik-twin-codex", "exec", "--scenario", ScenarioNoEdits}
	defer func() { os.Args = orig }()

	code := run()
	if code != 0 {
		t.Errorf("run() with 'exec' prefix returned %d, want 0", code)
	}
}

// TestRunExecResumeSubcommandStripped verifies that run() accepts the
// "exec resume <id>" prefix and exits 0.
func TestRunExecResumeSubcommandStripped(t *testing.T) {
	orig := os.Args
	os.Args = []string{
		"harmonik-twin-codex", "exec", "resume", "codex-twin-thread-abc",
		"--scenario", ScenarioNoEdits,
	}
	defer func() { os.Args = orig }()

	code := run()
	if code != 0 {
		t.Errorf("run() with 'exec resume <id>' prefix returned %d, want 0", code)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// stripExecSubcommand
// ─────────────────────────────────────────────────────────────────────────────

// TestStripExecSubcommandNoExec verifies that args without a leading "exec"
// are returned unchanged.
func TestStripExecSubcommandNoExec(t *testing.T) {
	args := []string{"--scenario", "no-edits"}
	got := stripExecSubcommand(args)
	if len(got) != len(args) || got[0] != args[0] {
		t.Errorf("stripExecSubcommand(%v) = %v, want unchanged", args, got)
	}
}

// TestStripExecSubcommandExecOnly verifies that ["exec", "--json"] strips to
// ["--json"].
func TestStripExecSubcommandExecOnly(t *testing.T) {
	args := []string{"exec", "--json", "--scenario", "no-edits"}
	got := stripExecSubcommand(args)
	if len(got) == 0 || got[0] != "--json" {
		t.Errorf("stripExecSubcommand(%v) = %v, want first elem --json", args, got)
	}
}

// TestStripExecSubcommandResume verifies that ["exec", "resume", "<id>",
// "--json"] strips to ["--json"].
func TestStripExecSubcommandResume(t *testing.T) {
	args := []string{"exec", "resume", "thread-abc-123", "--json", "--scenario", "no-edits"}
	got := stripExecSubcommand(args)
	if len(got) == 0 || got[0] != "--json" {
		t.Errorf("stripExecSubcommand(%v) = %v, want first elem --json", args, got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// version stamp (HC-043)
// ─────────────────────────────────────────────────────────────────────────────

// TestCommitHashVarIsSettable verifies that the commitHash package-level
// variable can be set from a test, confirming that -ldflags "-X
// main.commitHash=<sha>" will work at build time.
func TestCommitHashVarIsSettable(t *testing.T) {
	orig := commitHash
	defer func() { commitHash = orig }()

	const testHash = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	commitHash = testHash
	if commitHash != testHash {
		t.Errorf("commitHash = %q, want %q", commitHash, testHash)
	}
}

// TestVersionLineUnstamped verifies that versionLine() returns "(unstamped)"
// when commitHash is the zero string.
func TestVersionLineUnstamped(t *testing.T) {
	orig := commitHash
	commitHash = ""
	defer func() { commitHash = orig }()

	got := versionLine()
	const want = "harmonik-twin-codex commit=(unstamped)"
	if got != want {
		t.Errorf("versionLine() = %q, want %q", got, want)
	}
}

// TestVersionLineStamped verifies that versionLine() includes the stamped SHA.
func TestVersionLineStamped(t *testing.T) {
	orig := commitHash
	const stamp = "abc1234abc1234abc1234abc1234abc1234abc123"
	commitHash = stamp
	defer func() { commitHash = orig }()

	got := versionLine()
	want := "harmonik-twin-codex commit=" + stamp
	if got != want {
		t.Errorf("versionLine() = %q, want %q", got, want)
	}
}

// TestWriteVersion verifies that writeVersion writes the version line followed
// by a newline to the supplied writer.
func TestWriteVersion(t *testing.T) {
	orig := commitHash
	commitHash = ""
	defer func() { commitHash = orig }()

	var buf bytes.Buffer
	writeVersion(&buf)

	got := buf.String()
	if !strings.HasPrefix(got, "harmonik-twin-codex commit=") {
		t.Errorf("writeVersion output %q does not start with expected prefix", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("writeVersion output %q does not end with newline", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// JSONL ordering invariant: thread.started is always first
// ─────────────────────────────────────────────────────────────────────────────

// TestThreadStartedAlwaysFirst verifies that every scenario emits thread.started
// as the first JSONL line.
func TestThreadStartedAlwaysFirst(t *testing.T) {
	dir := codexTwinFixtureGitRepo(t)
	scenarios := []struct {
		name string
		cfg  scenarioConfig
	}{
		{ScenarioTrailerCommit, scenarioConfig{worktreePath: dir, beadID: "hk-test"}},
		{ScenarioEditsNoCommit, scenarioConfig{worktreePath: dir}},
		{ScenarioNoEdits, scenarioConfig{}},
		{ScenarioTurnFailed, scenarioConfig{}},
	}

	for _, tc := range scenarios {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			// Each trailer-commit or edits-no-commit needs a fresh git dir.
			if tc.name == ScenarioTrailerCommit {
				fresh := codexTwinFixtureGitRepo(t)
				tc.cfg.worktreePath = fresh
			}
			if err := runScenario(&buf, tc.name, tc.cfg); err != nil {
				t.Fatalf("runScenario(%q): %v", tc.name, err)
			}
			msgs := codexTwinFixtureDecodeAll(t, &buf)
			if len(msgs) == 0 {
				t.Fatal("no JSONL output emitted")
			}
			if got := msgs[0]["type"].(string); got != "thread.started" {
				t.Errorf("first message type = %q, want thread.started", got)
			}
		})
	}
}

// TestTerminalEventIsLast verifies that every scenario's final JSONL line is
// either turn.completed or turn.failed.
func TestTerminalEventIsLast(t *testing.T) {
	dir := codexTwinFixtureGitRepo(t)
	scenarios := []struct {
		name    string
		cfg     scenarioConfig
		wantEnd string
	}{
		{ScenarioTrailerCommit, scenarioConfig{worktreePath: dir, beadID: "hk-test"}, "turn.completed"},
		{ScenarioEditsNoCommit, scenarioConfig{worktreePath: dir}, "turn.completed"},
		{ScenarioNoEdits, scenarioConfig{}, "turn.completed"},
		{ScenarioTurnFailed, scenarioConfig{}, "turn.failed"},
	}

	for _, tc := range scenarios {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if tc.name == ScenarioTrailerCommit {
				fresh := codexTwinFixtureGitRepo(t)
				tc.cfg.worktreePath = fresh
			}
			if err := runScenario(&buf, tc.name, tc.cfg); err != nil {
				t.Fatalf("runScenario(%q): %v", tc.name, err)
			}
			msgs := codexTwinFixtureDecodeAll(t, &buf)
			if len(msgs) == 0 {
				t.Fatal("no JSONL output emitted")
			}
			last := msgs[len(msgs)-1]
			if got := last["type"].(string); got != tc.wantEnd {
				t.Errorf("last message type = %q, want %q", got, tc.wantEnd)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario: no-worktree-path is not a hard failure for commit-free scenarios
// ─────────────────────────────────────────────────────────────────────────────

// TestScenarioTrailerCommitNoWorktreeReturnsNil verifies that trailer-commit
// with no worktree path still emits JSONL successfully (no git ops attempted).
func TestScenarioTrailerCommitNoWorktreeReturnsNil(t *testing.T) {
	var buf bytes.Buffer
	err := runScenario(&buf, ScenarioTrailerCommit, scenarioConfig{})
	if err != nil {
		t.Errorf("runScenario(trailer-commit, no worktree) = %v, want nil", err)
	}
	msgs := codexTwinFixtureDecodeAll(t, &buf)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}

// TestScenarioEditsNoCommitNoWorktreeReturnsNil verifies that edits-no-commit
// with no worktree path still emits JSONL successfully.
func TestScenarioEditsNoCommitNoWorktreeReturnsNil(t *testing.T) {
	var buf bytes.Buffer
	err := runScenario(&buf, ScenarioEditsNoCommit, scenarioConfig{})
	if err != nil {
		t.Errorf("runScenario(edits-no-commit, no worktree) = %v, want nil", err)
	}
	msgs := codexTwinFixtureDecodeAll(t, &buf)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Parallel commit safety: multiple trailer-commits use unique sentinel files
// ─────────────────────────────────────────────────────────────────────────────

// TestTrailerCommitSentinelFilesUnique verifies that two back-to-back
// trailer-commit invocations on the same worktree each create a distinct
// sentinel file (no collision on the nanosecond timestamp).
func TestTrailerCommitSentinelFilesUnique(t *testing.T) {
	dir := codexTwinFixtureGitRepo(t)
	cfg := scenarioConfig{worktreePath: dir, beadID: "hk-test"}

	// First run.
	var buf1 bytes.Buffer
	if err := runScenario(&buf1, ScenarioTrailerCommit, cfg); err != nil {
		t.Fatalf("first runScenario(trailer-commit): %v", err)
	}

	// Second run.
	var buf2 bytes.Buffer
	if err := runScenario(&buf2, ScenarioTrailerCommit, cfg); err != nil {
		t.Fatalf("second runScenario(trailer-commit): %v", err)
	}

	// Each run creates exactly one new commit; two runs → two commits ahead of the initial.
	cmd := exec.Command("git", "log", "--oneline", "-3")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		t.Errorf("expected at least 2 commits after two trailer-commits, got %d lines:\n%s", len(lines), out)
	}
	// First two lines should be distinct commits.
	if lines[0] == lines[1] {
		t.Errorf("two consecutive trailer-commits produced the same commit line: %q", lines[0])
	}
}

// TestEditsNoCommitSentinelFileCreated verifies that the edits-no-commit
// scenario writes exactly one new untracked file to the worktree.
func TestEditsNoCommitSentinelFileCreated(t *testing.T) {
	dir := codexTwinFixtureGitRepo(t)
	cfg := scenarioConfig{worktreePath: dir}

	// Capture the list of untracked files before.
	untrackedBefore := func() []string {
		cmd := exec.Command("git", "ls-files", "--others", "--exclude-standard")
		cmd.Dir = dir
		out, _ := cmd.CombinedOutput()
		return strings.Fields(string(out))
	}

	countBefore := len(untrackedBefore())

	var buf bytes.Buffer
	if err := runScenario(&buf, ScenarioEditsNoCommit, cfg); err != nil {
		t.Fatalf("runScenario(edits-no-commit): %v", err)
	}

	countAfter := len(untrackedBefore())
	if countAfter != countBefore+1 {
		t.Errorf("edits-no-commit: expected %d untracked file(s) after, got %d",
			countBefore+1, countAfter)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario: worktree path with -C flag via run()
// ─────────────────────────────────────────────────────────────────────────────

// TestRunTrailerCommitWithCFlag verifies that run() honours the -C flag to
// specify the worktree path, creating a Refs: commit in the supplied dir.
func TestRunTrailerCommitWithCFlag(t *testing.T) {
	dir := codexTwinFixtureGitRepo(t)

	orig := os.Args
	os.Args = []string{
		"harmonik-twin-codex",
		"--scenario", ScenarioTrailerCommit,
		"--bead-id", "hk-runtest",
		"-C", dir,
	}
	defer func() { os.Args = orig }()

	code := run()
	if code != 0 {
		t.Fatalf("run() returned %d, want 0", code)
	}

	msg := codexTwinFixtureGitCommitMsg(t, dir)
	if !strings.Contains(msg, "Refs: hk-runtest") {
		t.Errorf("commit message %q missing Refs: hk-runtest", msg)
	}
}

// TestRunEditsNoCommitWithCFlag verifies that run() with the edits-no-commit
// scenario and -C leaves uncommitted changes in the supplied dir.
func TestRunEditsNoCommitWithCFlag(t *testing.T) {
	dir := codexTwinFixtureGitRepo(t)

	orig := os.Args
	os.Args = []string{
		"harmonik-twin-codex",
		"--scenario", ScenarioEditsNoCommit,
		"-C", dir,
	}
	defer func() { os.Args = orig }()

	code := run()
	if code != 0 {
		t.Fatalf("run() returned %d, want 0", code)
	}

	if codexTwinFixtureGitStatusClean(t, dir) {
		t.Error("edits-no-commit via run() left no uncommitted changes; expected dirty")
	}
}

// TestRunCFlagViaExecInterface verifies that run() strips the "exec" prefix
// AND honours -C from the remaining flags.
func TestRunCFlagViaExecInterface(t *testing.T) {
	dir := codexTwinFixtureGitRepo(t)

	orig := os.Args
	os.Args = []string{
		"harmonik-twin-codex",
		"exec",
		"--json",
		"-C", dir,
		"--scenario", ScenarioTrailerCommit,
		"--bead-id", "hk-exec-test",
	}
	defer func() { os.Args = orig }()

	code := run()
	if code != 0 {
		t.Fatalf("run() with exec prefix returned %d, want 0", code)
	}

	msg := codexTwinFixtureGitCommitMsg(t, dir)
	if !strings.Contains(msg, "Refs: hk-exec-test") {
		t.Errorf("commit message %q missing Refs: hk-exec-test", msg)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario: filepath for edits-no-commit uses .harmonik-twin-codex- prefix
// ─────────────────────────────────────────────────────────────────────────────

// TestEditsNoCommitSentinelFileNamePrefix verifies that the untracked file
// created by edits-no-commit uses the expected ".harmonik-twin-codex-edit-"
// prefix, making it distinguishable from other files.
func TestEditsNoCommitSentinelFileNamePrefix(t *testing.T) {
	dir := codexTwinFixtureGitRepo(t)
	cfg := scenarioConfig{worktreePath: dir}

	var buf bytes.Buffer
	if err := runScenario(&buf, ScenarioEditsNoCommit, cfg); err != nil {
		t.Fatalf("runScenario(edits-no-commit): %v", err)
	}

	entries, err := filepath.Glob(filepath.Join(dir, ".harmonik-twin-codex-edit-*"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(entries) == 0 {
		t.Error("edits-no-commit: no .harmonik-twin-codex-edit-* file found in worktree")
	}
}

// TestTrailerCommitSentinelFileNamePrefix verifies that the committed sentinel
// file created by trailer-commit uses the ".harmonik-twin-codex-commit-" prefix.
func TestTrailerCommitSentinelFileNamePrefix(t *testing.T) {
	dir := codexTwinFixtureGitRepo(t)
	cfg := scenarioConfig{worktreePath: dir, beadID: "hk-test"}

	var buf bytes.Buffer
	if err := runScenario(&buf, ScenarioTrailerCommit, cfg); err != nil {
		t.Fatalf("runScenario(trailer-commit): %v", err)
	}

	entries, err := filepath.Glob(filepath.Join(dir, ".harmonik-twin-codex-commit-*"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(entries) == 0 {
		t.Error("trailer-commit: no .harmonik-twin-codex-commit-* file found in worktree")
	}
}
