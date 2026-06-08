package daemon_test

// t6_scale_shape_test.go — T6 exploratory tester: scale and shape stress tests.
//
// Scope (T6):
//   1. 10 ready beads queued at start — does the daemon drain them sequentially? Wall-clock OK?
//   2. Bead body of 1 MB — does it survive ClaimBead and reach the handler?
//   3. Bead body of 0 bytes / near-empty.
//   4. Unicode-heavy bead body (CJK, emoji, RTL text).
//   5. Large worktree base (mkdir -p <tmpdir>/.lots-of-files/{1..1000}/) — does git worktree add stall?
//   6. Concurrent br create adding beads while the daemon is running — do new beads appear in the next poll cycle?

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// t6FixtureDir creates the standard fixture layout: git repo + .harmonik dirs + br wrapper + handler.
// Returns projectDir, jsonlPath, brWrapperPath, handlerPath.
func t6FixtureDir(t *testing.T) (projectDir, jsonlPath, brWrapper, handlerScript string) {
	t.Helper()
	// Resolve symlinks so that br receives the canonical path (macOS /var → /private/var).
	raw := t.TempDir()
	resolved, resolveErr := filepath.EvalSymlinks(raw)
	if resolveErr != nil {
		t.Fatalf("t6FixtureDir: EvalSymlinks %q: %v", raw, resolveErr)
	}
	projectDir = resolved

	// Init git repo
	gitRun := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = projectDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("t6FixtureDir: git %v: %v\n%s", args, err, out)
		}
	}
	gitRun("init", "--initial-branch=main")
	gitRun("config", "user.email", "t6@harmonik.local")
	gitRun("config", "user.name", "T6 Tester")
	initFile := filepath.Join(projectDir, "README")
	if err := os.WriteFile(initFile, []byte("t6 test repo\n"), 0o644); err != nil {
		t.Fatalf("t6FixtureDir: WriteFile README: %v", err)
	}
	gitRun("add", "README")
	gitRun("commit", "-m", "Initial commit")

	// Create a bare repo as the "origin" remote so the daemon's post-merge
	// `git push origin main` succeeds. Without an origin the single-mode merge
	// path returns push_failed (fatal) once the committing smoke handler
	// produces a real worktree commit (hk-4f5ua). Mirrors smokeFixtureGitRepo.
	originDir := t.TempDir()
	gitRunIn := func(dir string, args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("t6FixtureDir: git -C %s %v: %v\n%s", dir, args, err, out)
		}
	}
	gitRunIn(originDir, "init", "--bare", "--initial-branch=main")
	gitRun("remote", "add", "origin", originDir)
	gitRun("push", "origin", "main")

	// .harmonik dirs
	//nolint:gosec // G301: test-only temp directory
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("t6FixtureDir: mkdir events: %v", err)
	}
	//nolint:gosec // G301: test-only temp directory
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("t6FixtureDir: mkdir beads-intents: %v", err)
	}
	jsonlPath = filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")

	// br init
	realBr := smokeFixtureBrPath(t)
	//nolint:gosec // G204: br args are test-internal literals
	initCmd := exec.CommandContext(t.Context(), realBr, "init", "--prefix", "t6")
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	if initErr != nil {
		t.Fatalf("t6FixtureDir: br init: %v\n%s", initErr, initOut)
	}

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper = smokeFixtureBrWrapperScript(t, realBr, dbPath)
	handlerScript = smokeFixtureHandlerScript(t)
	return projectDir, jsonlPath, brWrapper, handlerScript
}

// t6SeedBeads creates N beads in projectDir via brWrapper.
// Returns a slice of bead IDs.
func t6SeedBeads(t *testing.T, brWrapper string, count int, bodyFn func(i int) string) []string {
	t.Helper()
	ids := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		body := bodyFn(i)
		title := fmt.Sprintf("T6 bead %d", i)
		var createCmd *exec.Cmd
		if body != "" {
			//nolint:gosec // G204: test-internal literals
			createCmd = exec.CommandContext(t.Context(), brWrapper, "create", title, "--body", body, "--silent")
		} else {
			//nolint:gosec // G204: test-internal literals
			createCmd = exec.CommandContext(t.Context(), brWrapper, "create", title, "--silent")
		}
		out, err := createCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("t6SeedBeads: br create bead %d: %v\n%s", i, err, out)
		}
		id := strings.TrimSpace(string(out))
		if id == "" {
			t.Fatalf("t6SeedBeads: br create bead %d returned empty ID", i)
		}
		ids = append(ids, id)
	}
	return ids
}

// t6PollAllClosed polls until all beadIDs are closed (or budget expires).
// Returns true if all closed within budget, and the elapsed time.
func t6PollAllClosed(t *testing.T, brWrapper string, beadIDs []string, budget time.Duration) (bool, time.Duration) {
	t.Helper()
	start := time.Now()
	deadline := start.Add(budget)
	for time.Now().Before(deadline) {
		allClosed := true
		for _, id := range beadIDs {
			//nolint:gosec // G204: test-internal literals
			cmd := exec.CommandContext(t.Context(), brWrapper, "show", id, "--format", "json")
			out, err := cmd.Output()
			if err != nil {
				allClosed = false
				break
			}
			var items []struct {
				Status string `json:"status"`
			}
			if jsonErr := json.Unmarshal(out, &items); jsonErr != nil || len(items) != 1 || items[0].Status != "closed" {
				allClosed = false
				break
			}
		}
		if allClosed {
			return true, time.Since(start)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false, time.Since(start)
}

// t6CountJSONLEvents reads a JSONL file and counts occurrences of each event type.
//
// NOTE (T6 finding F-001): The current busimpl.Emit appends only the redacted
// payload bytes to JSONL, not a full EV-001 envelope (which would include "type",
// "event_id", "schema_version"). As a result, event types cannot be detected by
// looking for their string name in the log. Instead, we use distinctive payload
// field names as proxies:
//   - run_started  → "workspace_path" (workloopRunStartedPayload)
//   - run_completed → "auto-close: exit=0" or "auto-reopen" in summary field
//   - run_failed   → "success":false in workloopRunCompletedPayload
//   - daemon_started → "pid" field (DaemonStartedPayload)
//
// This proxy approach is itself a finding: JSONL is not self-describing at the
// event-type level, contradicting EV-001 which requires the "type" field in the envelope.
func t6CountJSONLEvents(t *testing.T, jsonlPath string) map[string]int {
	t.Helper()
	counts := map[string]int{}
	//nolint:gosec // G304: path is t.TempDir()-based
	f, err := os.Open(jsonlPath)
	if err != nil {
		return counts
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024) // 4MB buffer for large payloads
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Proxy detection — see NOTE above.
		if strings.Contains(line, `"workspace_path"`) {
			counts[string(core.EventTypeRunStarted)]++
		}
		if strings.Contains(line, `"auto-close`) || strings.Contains(line, `"auto-reopen`) {
			counts[string(core.EventTypeRunCompleted)]++
		}
		if strings.Contains(line, `"success":false`) {
			counts[string(core.EventTypeRunFailed)]++
		}
		if strings.Contains(line, `"pid"`) && strings.Contains(line, `"started_at"`) {
			counts[string(core.EventTypeDaemonStarted)]++
		}
	}
	return counts
}

// ─────────────────────────────────────────────────────────────────────────────
// T6-1: 10-bead sequential drain
// ─────────────────────────────────────────────────────────────────────────────

func TestT6_10BeadSequentialDrain(t *testing.T) {
	// Not parallel: ctx cancellation stops the daemon (converted from SIGINT self-signal per hk-i4mtq)
	projectDir, jsonlPath, brWrapper, handlerScript := t6FixtureDir(t)

	// Seed 10 beads with varied ASCII bodies
	beadIDs := t6SeedBeads(t, brWrapper, 10, func(i int) string {
		return fmt.Sprintf("T6 test bead %d: ASCII body for sequential drain test.", i)
	})
	t.Logf("T6-1: seeded %d beads", len(beadIDs))

	cfg := daemon.Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              brWrapper,
		HandlerBinary:       handlerScript,
		// Single mode (hk-4f5ua): T6 is a scale/shape drain suite — each bead is a
		// single-mode happy path that asserts run_completed counts. The smoke
		// handler commits but writes no reviewer verdict, so review-loop would trip
		// "verdict absent at iteration 1" and reopen every bead.
		WorkflowModeDefault: core.WorkflowModeSingle,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startDone := make(chan error, 1)
	go func() { startDone <- daemon.Start(ctx, cfg) }()

	allClosed, elapsed := t6PollAllClosed(t, brWrapper, beadIDs, 120*time.Second)

	cancel()
	select {
	case err := <-startDone:
		if err != nil {
			t.Errorf("daemon.Start returned error after cancel: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("daemon.Start did not return within 10s after cancel")
	}

	t.Logf("T6-1: all_closed=%v elapsed=%.2fs per_bead=%.2fs", allClosed, elapsed.Seconds(), elapsed.Seconds()/10)

	if !allClosed {
		t.Errorf("T6-1 FAIL: not all 10 beads closed within 120s (elapsed=%.2fs)", elapsed.Seconds())
	}

	counts := t6CountJSONLEvents(t, jsonlPath)
	t.Logf("T6-1: JSONL events: run_started=%d run_completed=%d run_failed=%d",
		counts[string(core.EventTypeRunStarted)],
		counts[string(core.EventTypeRunCompleted)],
		counts[string(core.EventTypeRunFailed)])

	if counts[string(core.EventTypeRunStarted)] != 10 {
		t.Errorf("T6-1: expected 10 run_started events, got %d", counts[string(core.EventTypeRunStarted)])
	}
	if counts[string(core.EventTypeRunCompleted)] != 10 {
		t.Errorf("T6-1: expected 10 run_completed events, got %d", counts[string(core.EventTypeRunCompleted)])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T6-2: Large bead body (at br's 100KB validation ceiling)
// ─────────────────────────────────────────────────────────────────────────────
//
// Finding T6-F002 (severity: INFO): br enforces a 100KB max body size
// validation ("description: exceeds 100KB"). The originally planned 1MB test
// was blocked by two independent limits: (1) macOS ARG_MAX of 1MB (errno
// E2BIG when the --body arg alone approaches 1MB), and (2) br's own
// 100KB validation ceiling. This test uses 100KB (exactly at the ceiling)
// as the maximum achievable via br create --body.

func TestT6_1MBBeadBody(t *testing.T) {
	// Not parallel: ctx cancellation stops the daemon (converted from SIGINT self-signal per hk-i4mtq)
	projectDir, jsonlPath, brWrapper, handlerScript := t6FixtureDir(t)

	// Generate a 100KB body (br's max; 1MB is rejected by br validation).
	// See Finding T6-F002 in test/exploratory/findings-T6.md.
	const targetBytes = 100 * 1024 // 102400 bytes — br 100KB ceiling
	chunk := strings.Repeat("A", 1000)
	var sb strings.Builder
	for sb.Len() < targetBytes {
		sb.WriteString(chunk)
	}
	bigBody := sb.String()[:targetBytes]
	t.Logf("T6-2: body size = %d bytes", len(bigBody))

	beadIDs := t6SeedBeads(t, brWrapper, 1, func(_ int) string { return bigBody })
	t.Logf("T6-2: seeded bead ID = %s", beadIDs[0])

	// Verify the body survives in the DB by reading it back.
	// Note: br show --format json uses "description" field, not "body".
	//nolint:gosec // G204: test-internal literals
	showCmd := exec.CommandContext(t.Context(), brWrapper, "show", beadIDs[0], "--format", "json")
	showOut, showErr := showCmd.Output()
	if showErr != nil {
		t.Fatalf("T6-2: br show after create: %v", showErr)
	}
	var items []struct {
		Description string `json:"description"`
	}
	if jsonErr := json.Unmarshal(showOut, &items); jsonErr == nil && len(items) == 1 {
		t.Logf("T6-2: body in DB (via description field) = %d bytes", len(items[0].Description))
		if len(items[0].Description) != targetBytes {
			t.Errorf("T6-2: body size mismatch in DB: stored %d, want %d", len(items[0].Description), targetBytes)
		}
	} else {
		t.Logf("T6-2: could not parse br show JSON: unmarshal err=%v, items=%d", jsonErr, len(items))
	}
	// NOTE (Finding T6-F004): the daemon work loop does NOT read the bead body.
	// ClaimBead takes only the BeadID; the body stays in Beads-SQLite.
	// The handler subprocess (when real, not handler.sh) must call `br show`
	// itself to read the body. This test verifies the body survives in storage,
	// not that it is forwarded to the handler.

	cfg := daemon.Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              brWrapper,
		HandlerBinary:       handlerScript,
		// Single mode (hk-4f5ua): T6 is a scale/shape drain suite — each bead is a
		// single-mode happy path that asserts run_completed counts. The smoke
		// handler commits but writes no reviewer verdict, so review-loop would trip
		// "verdict absent at iteration 1" and reopen every bead.
		WorkflowModeDefault: core.WorkflowModeSingle,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startDone := make(chan error, 1)
	go func() { startDone <- daemon.Start(ctx, cfg) }()

	allClosed, elapsed := t6PollAllClosed(t, brWrapper, beadIDs, 60*time.Second)

	cancel()
	select {
	case err := <-startDone:
		if err != nil {
			t.Errorf("daemon.Start returned error after cancel: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("daemon.Start did not return within 10s after cancel")
	}

	t.Logf("T6-2: all_closed=%v elapsed=%.2fs", allClosed, elapsed.Seconds())
	if !allClosed {
		t.Errorf("T6-2 FAIL: 1MB bead not closed within 60s")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T6-3: Zero-byte / near-empty bead body
// ─────────────────────────────────────────────────────────────────────────────

func TestT6_EmptyAndNearEmptyBody(t *testing.T) {
	// Not parallel: ctx cancellation stops the daemon (converted from SIGINT self-signal per hk-i4mtq)
	projectDir, jsonlPath, brWrapper, handlerScript := t6FixtureDir(t)

	// Seed 2 beads: one with empty body (no --body flag), one with a single space
	var ids []string

	// Empty body (no --body)
	//nolint:gosec // G204: test-internal literals
	emptyCmd := exec.CommandContext(t.Context(), brWrapper, "create", "T6 empty body bead", "--silent")
	emptyOut, emptyErr := emptyCmd.CombinedOutput()
	if emptyErr != nil {
		t.Fatalf("T6-3: create empty body bead: %v\n%s", emptyErr, emptyOut)
	}
	ids = append(ids, strings.TrimSpace(string(emptyOut)))

	// Near-empty body
	nearEmptyBody := " "
	moreIDs := t6SeedBeads(t, brWrapper, 1, func(_ int) string { return nearEmptyBody })
	ids = append(ids, moreIDs...)

	t.Logf("T6-3: seeded bead IDs = %v", ids)

	cfg := daemon.Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              brWrapper,
		HandlerBinary:       handlerScript,
		// Single mode (hk-4f5ua): T6 is a scale/shape drain suite — each bead is a
		// single-mode happy path that asserts run_completed counts. The smoke
		// handler commits but writes no reviewer verdict, so review-loop would trip
		// "verdict absent at iteration 1" and reopen every bead.
		WorkflowModeDefault: core.WorkflowModeSingle,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startDone := make(chan error, 1)
	go func() { startDone <- daemon.Start(ctx, cfg) }()

	allClosed, elapsed := t6PollAllClosed(t, brWrapper, ids, 60*time.Second)

	cancel()
	select {
	case err := <-startDone:
		if err != nil {
			t.Errorf("daemon.Start returned error after cancel: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("daemon.Start did not return within 10s after cancel")
	}

	t.Logf("T6-3: all_closed=%v elapsed=%.2fs", allClosed, elapsed.Seconds())
	if !allClosed {
		t.Errorf("T6-3 FAIL: empty/near-empty body beads not closed within 60s")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T6-4: Unicode-heavy bead body
// ─────────────────────────────────────────────────────────────────────────────

func TestT6_UnicodeHeavyBody(t *testing.T) {
	// Not parallel: ctx cancellation stops the daemon (converted from SIGINT self-signal per hk-i4mtq)
	projectDir, jsonlPath, brWrapper, handlerScript := t6FixtureDir(t)

	unicodeBody := "CJK: 这是一个测试条目，用于验证Unicode处理。" +
		"日本語: このビードはUnicodeタイトルと本文を含む。" +
		"한국어: 유니코드 테스트 항목입니다。" +
		"Emoji: 🎯 ✅ 🔍 🧪 🚀 💡 ⚠️ 🌍。" +
		"RTL Arabic: مرحبا بالعالم。" +
		"RTL Hebrew: שלום עולם。" +
		"Mixed: Café au lait, résumé, naïve, über, Ñoño。"

	beadIDs := t6SeedBeads(t, brWrapper, 1, func(_ int) string { return unicodeBody })
	t.Logf("T6-4: seeded bead ID = %s, body_bytes = %d", beadIDs[0], len(unicodeBody))

	// Verify the unicode body survives round-trip.
	// Note: br show --format json uses "description" field, not "body".
	//nolint:gosec // G204: test-internal literals
	showCmd := exec.CommandContext(t.Context(), brWrapper, "show", beadIDs[0], "--format", "json")
	showOut, showErr := showCmd.Output()
	if showErr != nil {
		t.Fatalf("T6-4: br show after create: %v", showErr)
	}
	var items []struct {
		Description string `json:"description"`
	}
	if jsonErr := json.Unmarshal(showOut, &items); jsonErr == nil && len(items) == 1 {
		if items[0].Description != unicodeBody {
			t.Errorf("T6-4: unicode body round-trip mismatch: got %q, want %q",
				items[0].Description[:min(50, len(items[0].Description))], unicodeBody[:50])
		} else {
			t.Logf("T6-4: unicode body round-trip OK (description field)")
		}
	}

	cfg := daemon.Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              brWrapper,
		HandlerBinary:       handlerScript,
		// Single mode (hk-4f5ua): T6 is a scale/shape drain suite — each bead is a
		// single-mode happy path that asserts run_completed counts. The smoke
		// handler commits but writes no reviewer verdict, so review-loop would trip
		// "verdict absent at iteration 1" and reopen every bead.
		WorkflowModeDefault: core.WorkflowModeSingle,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startDone := make(chan error, 1)
	go func() { startDone <- daemon.Start(ctx, cfg) }()

	allClosed, elapsed := t6PollAllClosed(t, brWrapper, beadIDs, 60*time.Second)

	cancel()
	select {
	case err := <-startDone:
		if err != nil {
			t.Errorf("daemon.Start returned error after cancel: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("daemon.Start did not return within 10s after cancel")
	}

	t.Logf("T6-4: all_closed=%v elapsed=%.2fs", allClosed, elapsed.Seconds())
	if !allClosed {
		t.Errorf("T6-4 FAIL: unicode bead not closed within 60s")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T6-5: Large worktree base (1000 subdirectories)
// ─────────────────────────────────────────────────────────────────────────────

func TestT6_LargeWorktreeBase(t *testing.T) {
	// Not parallel: ctx cancellation stops the daemon (converted from SIGINT self-signal per hk-i4mtq)
	projectDir, jsonlPath, brWrapper, handlerScript := t6FixtureDir(t)

	// Create 1000 subdirectories in the project dir to simulate a large repo
	t.Logf("T6-5: creating 1000 subdirs in %s", projectDir)
	createStart := time.Now()
	for i := 1; i <= 1000; i++ {
		dirPath := filepath.Join(projectDir, ".lots-of-files", fmt.Sprintf("%d", i))
		//nolint:gosec // G301: test-only temp directory
		if err := os.MkdirAll(dirPath, 0o755); err != nil {
			t.Fatalf("T6-5: mkdir %s: %v", dirPath, err)
		}
		// Add a placeholder file so git has something to see
		fPath := filepath.Join(dirPath, "placeholder.txt")
		if err := os.WriteFile(fPath, []byte(fmt.Sprintf("placeholder %d\n", i)), 0o644); err != nil {
			t.Fatalf("T6-5: WriteFile %s: %v", fPath, err)
		}
	}
	t.Logf("T6-5: dir creation took %.2fs", time.Since(createStart).Seconds())

	beadIDs := t6SeedBeads(t, brWrapper, 1, func(_ int) string {
		return "T6-5 bead with large worktree base"
	})
	t.Logf("T6-5: seeded bead ID = %s", beadIDs[0])

	cfg := daemon.Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              brWrapper,
		HandlerBinary:       handlerScript,
		// Single mode (hk-4f5ua): T6 is a scale/shape drain suite — each bead is a
		// single-mode happy path that asserts run_completed counts. The smoke
		// handler commits but writes no reviewer verdict, so review-loop would trip
		// "verdict absent at iteration 1" and reopen every bead.
		WorkflowModeDefault: core.WorkflowModeSingle,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startDone := make(chan error, 1)
	go func() { startDone <- daemon.Start(ctx, cfg) }()

	// Budget is 90s — if worktree add stalls this will catch it
	allClosed, elapsed := t6PollAllClosed(t, brWrapper, beadIDs, 90*time.Second)

	cancel()
	select {
	case err := <-startDone:
		if err != nil {
			t.Errorf("daemon.Start returned error after cancel: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("daemon.Start did not return within 10s after cancel")
	}

	t.Logf("T6-5: all_closed=%v elapsed=%.2fs (1000 subdirs in worktree base)", allClosed, elapsed.Seconds())
	if !allClosed {
		t.Errorf("T6-5 FAIL: bead with large worktree base not closed within 90s")
	}
	if elapsed > 30*time.Second {
		t.Logf("T6-5 WARNING: large worktree base slowed dispatch: %.2fs (threshold 30s)", elapsed.Seconds())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T6-6: Concurrent br create while daemon running
// ─────────────────────────────────────────────────────────────────────────────

func TestT6_ConcurrentBeadCreate(t *testing.T) {
	// Not parallel: ctx cancellation stops the daemon (converted from SIGINT self-signal per hk-i4mtq)
	projectDir, jsonlPath, brWrapper, handlerScript := t6FixtureDir(t)

	// Seed 1 initial bead to get the daemon started
	initialIDs := t6SeedBeads(t, brWrapper, 1, func(_ int) string {
		return "T6-6 initial bead before daemon start"
	})
	t.Logf("T6-6: initial bead = %s", initialIDs[0])

	cfg := daemon.Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              brWrapper,
		HandlerBinary:       handlerScript,
		// Single mode (hk-4f5ua): T6 is a scale/shape drain suite — each bead is a
		// single-mode happy path that asserts run_completed counts. The smoke
		// handler commits but writes no reviewer verdict, so review-loop would trip
		// "verdict absent at iteration 1" and reopen every bead.
		WorkflowModeDefault: core.WorkflowModeSingle,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startDone := make(chan error, 1)
	go func() { startDone <- daemon.Start(ctx, cfg) }()

	// Wait a bit for the daemon to start processing
	time.Sleep(1 * time.Second)

	// Create 3 additional beads while the daemon is running
	laterIDs := t6SeedBeads(t, brWrapper, 3, func(i int) string {
		return fmt.Sprintf("T6-6 late-arriving bead %d created after daemon start", i)
	})
	t.Logf("T6-6: late-arriving bead IDs = %v", laterIDs)

	allIDs := append(initialIDs, laterIDs...)

	allClosed, elapsed := t6PollAllClosed(t, brWrapper, allIDs, 90*time.Second)

	cancel()
	select {
	case err := <-startDone:
		if err != nil {
			t.Errorf("daemon.Start returned error after cancel: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("daemon.Start did not return within 10s after cancel")
	}

	t.Logf("T6-6: all_closed=%v elapsed=%.2fs (1 initial + 3 late-arriving beads)", allClosed, elapsed.Seconds())
	if !allClosed {
		t.Errorf("T6-6 FAIL: not all beads (including late-arriving) closed within 90s")
	}

	counts := t6CountJSONLEvents(t, jsonlPath)
	t.Logf("T6-6: JSONL events: run_started=%d run_completed=%d run_failed=%d",
		counts[string(core.EventTypeRunStarted)],
		counts[string(core.EventTypeRunCompleted)],
		counts[string(core.EventTypeRunFailed)])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
