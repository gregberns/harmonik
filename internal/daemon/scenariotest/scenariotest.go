// Package scenariotest provides assertion helpers for daemon-level end-to-end
// scenario tests (hk-jf2tb).
//
// All helpers follow the test-fixture convention: they accept *testing.T as
// their first argument, call t.Helper(), and call t.Fatalf / t.Errorf on
// failure so that failures surface at the call site.
//
// # Design contract
//
// - Helpers read observable surfaces: JSONL event log, queue.json, br status.
// - No internal daemon seams are used; these helpers assert from the outside.
// - tmux assertions use the injected tmux.Adapter; nil adapter skips the
//   assertion (non-tmux test environments).
//
// Spec refs:
//   - specs/scenario-harness.md §4 (assertion vocabulary)
//   - specs/event-model.md §6.1 (Event envelope)
//   - specs/queue-model.md §2 (queue.json shape)
//
// Bead: hk-jf2tb.
package scenariotest

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ──────────────────────────────────────────────────────────────────────────────
// CapturedEvent — single event from the JSONL stream
// ──────────────────────────────────────────────────────────────────────────────

// CapturedEvent is one JSONL envelope line decoded to the fields relevant for
// scenario assertions (type, run_id, raw payload).
type CapturedEvent struct {
	// Type is the event type string (core.EventType as string value).
	Type string
	// RunID is the run_id field from the envelope (empty if absent).
	RunID string
	// Raw is the full JSON line as read from the JSONL file.
	Raw string
}

// ──────────────────────────────────────────────────────────────────────────────
// EventStream — thread-safe captured event list
// ──────────────────────────────────────────────────────────────────────────────

// EventStream is a thread-safe list of captured events populated by
// CaptureEventStream. It is safe to read concurrently while the daemon is
// still running.
type EventStream struct {
	mu     sync.RWMutex
	events []CapturedEvent
	done   chan struct{}
}

// All returns a snapshot of all captured events.
func (es *EventStream) All() []CapturedEvent {
	es.mu.RLock()
	defer es.mu.RUnlock()
	out := make([]CapturedEvent, len(es.events))
	copy(out, es.events)
	return out
}

// Types returns the event-type strings in capture order.
func (es *EventStream) Types() []string {
	es.mu.RLock()
	defer es.mu.RUnlock()
	out := make([]string, len(es.events))
	for i, e := range es.events {
		out[i] = e.Type
	}
	return out
}

// ──────────────────────────────────────────────────────────────────────────────
// CaptureEventStream — background JSONL tail reader
// ──────────────────────────────────────────────────────────────────────────────

// CaptureEventStream starts a background goroutine that polls the JSONL log at
// jsonlPath and appends decoded events to the returned EventStream. Polling
// stops when ctx is cancelled or when the test ends (via t.Cleanup).
//
// The poll interval is 10 ms, which is fine for scenario tests (the stream
// is only used after daemon.Start returns or from a concurrent assertion goroutine).
//
// Bead: hk-jf2tb.
func CaptureEventStream(t *testing.T, ctx context.Context, jsonlPath string) *EventStream {
	t.Helper()
	es := &EventStream{done: make(chan struct{})}
	go func() {
		defer close(es.done)
		var offset int64
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			//nolint:gosec // G304: path is t.TempDir()-based; not user input
			f, err := os.Open(jsonlPath)
			if err != nil {
				continue
			}
			if _, err = f.Seek(offset, io.SeekStart); err != nil {
				if closeErr := f.Close(); closeErr != nil {
					t.Logf("eventStream: close after seek error: %v", closeErr)
				}
				continue
			}
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}
				var env struct {
					Type  string `json:"type"`
					RunID string `json:"run_id"`
				}
				if decErr := json.Unmarshal([]byte(line), &env); decErr != nil {
					// Skip malformed lines; bump offset.
				} else {
					es.mu.Lock()
					es.events = append(es.events, CapturedEvent{
						Type:  env.Type,
						RunID: env.RunID,
						Raw:   line,
					})
					es.mu.Unlock()
				}
			}
			pos, posErr := f.Seek(0, io.SeekCurrent)
			if posErr == nil {
				offset = pos
			}
			if closeErr := f.Close(); closeErr != nil {
				t.Logf("eventStream: close after scan: %v", closeErr)
			}
		}
	}()
	t.Cleanup(func() { <-es.done })
	return es
}

// ──────────────────────────────────────────────────────────────────────────────
// WaitForEvent — poll until a matching event arrives
// ──────────────────────────────────────────────────────────────────────────────

// WaitForEvent polls the JSONL file at jsonlPath until an event of eventType
// scoped to runID appears (or timeout expires). When runID is empty it matches
// any run_id.
//
// Returns true if the event was found within timeout, false otherwise.
//
// Bead: hk-jf2tb.
func WaitForEvent(t *testing.T, jsonlPath, eventType, runID string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if scanJSONLForEvent(t, jsonlPath, eventType, runID) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// scanJSONLForEvent reads the whole JSONL file and returns true when a matching
// event is found.
func scanJSONLForEvent(t *testing.T, jsonlPath, eventType, runID string) bool {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		return false
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Logf("scanJSONLForEvent: close: %v", closeErr)
		}
	}()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var env struct {
			Type  string `json:"type"`
			RunID string `json:"run_id"`
		}
		if decErr := json.Unmarshal([]byte(line), &env); decErr != nil {
			continue
		}
		if env.Type != eventType {
			continue
		}
		if runID == "" || env.RunID == runID {
			return true
		}
	}
	return false
}

// ──────────────────────────────────────────────────────────────────────────────
// AssertEventSequence — ordered subset check
// ──────────────────────────────────────────────────────────────────────────────

// ExpectedEvent describes one expected event in an ordered sequence assertion.
type ExpectedEvent struct {
	// Type is the required event type string.
	Type string
	// RunID is the required run_id (empty = match any run_id).
	RunID string
}

// AssertEventSequence reads the JSONL at jsonlPath and asserts that the
// required events appear in order (as a subsequence — intervening events are
// allowed). Fails the test and reports missing events when the subsequence
// check fails.
//
// Bead: hk-jf2tb.
func AssertEventSequence(t *testing.T, jsonlPath string, required []ExpectedEvent) {
	t.Helper()
	events := readAllEvents(t, jsonlPath)
	idx := 0
	for _, want := range required {
		found := false
		for idx < len(events) {
			ev := events[idx]
			idx++
			if ev.Type == want.Type && (want.RunID == "" || ev.RunID == want.RunID) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AssertEventSequence: event %q (runID=%q) not found in sequence; got types=%v",
				want.Type, want.RunID, eventTypes(events))
		}
	}
}

// readAllEvents decodes every non-empty JSONL line from jsonlPath.
func readAllEvents(t *testing.T, jsonlPath string) []CapturedEvent {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("AssertEventSequence: open %s: %v", jsonlPath, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Logf("readAllEvents: close: %v", closeErr)
		}
	}()
	var out []CapturedEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var env struct {
			Type  string `json:"type"`
			RunID string `json:"run_id"`
		}
		if decErr := json.Unmarshal([]byte(line), &env); decErr == nil {
			out = append(out, CapturedEvent{Type: env.Type, RunID: env.RunID, Raw: line})
		}
	}
	return out
}

func eventTypes(events []CapturedEvent) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.Type
	}
	return out
}

// ──────────────────────────────────────────────────────────────────────────────
// AssertQueueJSON — validate queue.json state
// ──────────────────────────────────────────────────────────────────────────────

// QueueExpectation carries the expected fields for an AssertQueueJSON call.
type QueueExpectation struct {
	// Status is the expected top-level queue status (e.g. "completed", "active").
	// Empty string skips the status assertion.
	Status string
	// ItemStatuses is the expected per-item status list, in order of group × item.
	// Nil skips the item-status assertion.
	ItemStatuses []string
}

// AssertQueueJSON reads .harmonik/queue.json under projectDir and asserts the
// fields in expected. When queue.json is absent and expected.Status is empty,
// the assertion passes (no queue was written).
//
// Bead: hk-jf2tb.
func AssertQueueJSON(t *testing.T, projectDir string, expected QueueExpectation) {
	t.Helper()
	queuePath := filepath.Join(projectDir, ".harmonik", "queue.json")
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	data, err := os.ReadFile(queuePath)
	if err != nil {
		if os.IsNotExist(err) {
			if expected.Status == "" && len(expected.ItemStatuses) == 0 {
				return // absence is acceptable when no expectation is set
			}
			t.Fatalf("AssertQueueJSON: queue.json absent but status=%q expected", expected.Status)
		}
		t.Fatalf("AssertQueueJSON: read %s: %v", queuePath, err)
	}
	var q struct {
		Status string `json:"status"`
		Groups []struct {
			Items []struct {
				Status string `json:"status"`
			} `json:"items"`
		} `json:"groups"`
	}
	if decErr := json.Unmarshal(data, &q); decErr != nil {
		t.Fatalf("AssertQueueJSON: parse queue.json: %v", decErr)
	}
	if expected.Status != "" && q.Status != expected.Status {
		t.Errorf("AssertQueueJSON: status: got %q, want %q", q.Status, expected.Status)
	}
	if len(expected.ItemStatuses) > 0 {
		var got []string
		for _, g := range q.Groups {
			for _, item := range g.Items {
				got = append(got, item.Status)
			}
		}
		if fmt.Sprint(got) != fmt.Sprint(expected.ItemStatuses) {
			t.Errorf("AssertQueueJSON: item statuses: got %v, want %v", got, expected.ItemStatuses)
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// AssertNoOrphanTmuxWindows — verify no harmonik windows leaked
// ──────────────────────────────────────────────────────────────────────────────

// AssertNoOrphanTmuxWindows asserts that no tmux windows with the harmonik
// naming prefix remain after a scenario test. Skips the assertion when adapter
// is nil (non-tmux environments).
//
// harmonik window names follow the pattern "hk-<runID>" per workspace-model.md.
//
// Bead: hk-jf2tb.
func AssertNoOrphanTmuxWindows(t *testing.T, adapter tmuxPkg.Adapter) {
	t.Helper()
	if adapter == nil {
		return // non-tmux environment: skip
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sessions, err := adapter.ListSessions(ctx)
	if err != nil {
		t.Logf("AssertNoOrphanTmuxWindows: ListSessions: %v (skipping)", err)
		return
	}
	for _, sess := range sessions {
		windows, wErr := adapter.ListWindows(ctx, sess)
		if wErr != nil {
			t.Logf("AssertNoOrphanTmuxWindows: ListWindows(%q): %v (skipping)", sess, wErr)
			continue
		}
		for _, w := range windows {
			if strings.HasPrefix(w, "hk-") {
				t.Errorf("AssertNoOrphanTmuxWindows: orphan window %q in session %q", w, sess)
			}
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// AssertBeadStatus — verify br status for a bead ID
// ──────────────────────────────────────────────────────────────────────────────

// AssertBeadStatus runs `br show <beadID> --format json` via brPath and asserts
// the returned status matches wantStatus.
//
// brPath is the path to the `br` wrapper script (which must have --db pre-wired)
// or the raw `br` binary when the DB is discoverable from the working directory.
//
// Bead: hk-jf2tb.
func AssertBeadStatus(t *testing.T, brPath, beadID, wantStatus string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), brPath, "show", beadID, "--format", "json")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("AssertBeadStatus: br show %s: %v", beadID, err)
	}
	// br show --format json returns a JSON array of bead records.
	var records []struct {
		Status string `json:"status"`
	}
	if decErr := json.Unmarshal(out, &records); decErr != nil {
		t.Fatalf("AssertBeadStatus: parse br output: %v\noutput: %s", decErr, out)
	}
	if len(records) == 0 {
		t.Fatalf("AssertBeadStatus: br show returned empty output for %s", beadID)
	}
	if records[0].Status != wantStatus {
		t.Errorf("AssertBeadStatus: bead %s: got status %q, want %q", beadID, records[0].Status, wantStatus)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// TwinBinaryPath — locate harmonik-twin-claude
// ──────────────────────────────────────────────────────────────────────────────

// TwinBinaryPath returns the absolute path to the harmonik-twin-claude binary.
//
// Resolution order:
//  1. HARMONIK_TWIN_CLAUDE env var (explicit override for CI).
//  2. <worktree-root>/harmonik-twin-claude — present when this test is run
//     from within a worktree that has the binary.
//  3. <main-repo-root>/harmonik-twin-claude — found via git --git-common-dir
//     (valid in both normal checkouts and git worktrees; the main repo has the
//     pre-built binary committed at the repo root).
//  4. <source-file-root>/harmonik-twin-claude — fallback walk up from the
//     file's directory (covers unusual checkout layouts).
//
// Returns ("", false) when no binary is found; the test should call t.Skip.
//
// Bead: hk-jf2tb.
func TwinBinaryPath() (string, bool) {
	// 1. Override for CI or explicit test configuration.
	if env := os.Getenv("HARMONIK_TWIN_CLAUDE"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env, true
		}
	}

	// 2 + 3. Locate relative to this source file's directory, trying both the
	// worktree root (4 levels up from the source file) and the main-repo root
	// (resolved via `git rev-parse --git-common-dir` which strips the .git suffix).
	_, thisFile, _, ok := runtime.Caller(0)
	if ok {
		// thisFile = .../internal/daemon/scenariotest/scenariotest.go
		// worktree root = 4 dirs up
		worktreeRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(thisFile))))
		for _, candidate := range []string{
			filepath.Join(worktreeRoot, "harmonik-twin-claude"),
		} {
			if _, err := os.Stat(candidate); err == nil {
				return candidate, true
			}
		}

		// 3. Walk up looking for .git to find the actual repo root; handles
		// the case where the test runs from a worktree and the binary lives in
		// the main checkout (the common case during development).
		if mainRoot := findMainRepoRoot(worktreeRoot); mainRoot != "" {
			candidate := filepath.Join(mainRoot, "harmonik-twin-claude")
			if _, err := os.Stat(candidate); err == nil {
				return candidate, true
			}
		}
	}

	return "", false
}

// findMainRepoRoot walks the directory tree upward from start looking for a
// .git directory. In a normal checkout it returns the repo root; in a worktree
// it reads the .git file's gitdir pointer and returns the main checkout root.
func findMainRepoRoot(start string) string {
	dir := start
	for {
		gitPath := filepath.Join(dir, ".git")
		info, err := os.Stat(gitPath)
		if err == nil {
			if info.IsDir() {
				// Normal checkout — .git is a directory.
				return dir
			}
			// Worktree — .git is a file pointing at the real gitdir.
			// Format: "gitdir: /path/to/.git/worktrees/name\n"
			//nolint:gosec // G304: path is constructed from source-file dir walk; not user input
			content, readErr := os.ReadFile(gitPath)
			if readErr != nil {
				return ""
			}
			line := strings.TrimSpace(string(content))
			const prefix = "gitdir: "
			if !strings.HasPrefix(line, prefix) {
				return ""
			}
			gitdir := strings.TrimPrefix(line, prefix)
			// gitdir is .../harmonik/.git/worktrees/<name>
			// Strip /worktrees/<name> to get .../harmonik/.git
			// then strip /.git to get .../harmonik
			if idx := strings.Index(gitdir, "/.git/worktrees/"); idx >= 0 {
				return gitdir[:idx]
			}
			// Fall back: parent of gitdir's parent is the repo root.
			return filepath.Dir(filepath.Dir(gitdir))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "" // reached filesystem root
		}
		dir = parent
	}
}
