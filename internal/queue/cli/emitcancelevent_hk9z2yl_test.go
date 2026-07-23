package cli

// emitcancelevent_hk9z2yl_test.go — coverage for emitQueueCancelEvent's new
// error return (de9aac48, follow-up bead hk-9z2yl).
//
// The function used to be `func(projectDir, queueID, priorStatus string)` with
// two bare `return`s on failure (after json.Marshal and after os.OpenFile) and
// three `_ =` discards (os.MkdirAll, the deferred f.Close, and f.Write), so an
// unjournalled cancel was indistinguishable from a journalled one. It now
// returns an error on every failure path, which is what lets journalCancel warn
// the operator.
//
// Which of the four error returns each test pins:
//   - os.MkdirAll  → …ReportsMkdirFailure
//   - os.OpenFile  → …ReportsOpenFailure
//   - f.Write      → …ReportsWriteFailure_JoinsCloseResult
//   - json.Marshal → nothing, and deliberately so: queueCancelOperatorEvent is
//     five plain string fields, which encoding/json cannot fail on, so the
//     branch is unreachable and a revert of it would be unobservable.
//
// …ReportsWriteFailure_JoinsCloseResult additionally pins the deferred
// `err = errors.Join(err, f.Close())`: reverting that defer to `_ = f.Close()`
// leaves the returned error a plain *fmt.wrapError instead of an errors.Join
// fold, which the test asserts on directly.
//
// Bead ref: hk-9z2yl.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// TestEmitQueueCancelEvent_AppendsAJournalLine verifies the success path: the
// event lands in .harmonik/events/events.jsonl with the operator fields set,
// nil is returned, and a second call APPENDS rather than truncating (the file
// is a shared append-only journal with internal/eventbus's writer).
func TestEmitQueueCancelEvent_AppendsAJournalLine(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()

	if err := emitQueueCancelEvent(projectDir, "qid-first", "active"); err != nil {
		t.Fatalf("emitQueueCancelEvent (first): %v", err)
	}
	if err := emitQueueCancelEvent(projectDir, "qid-second", "paused-by-failure"); err != nil {
		t.Fatalf("emitQueueCancelEvent (second): %v", err)
	}

	journal := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	raw, err := os.ReadFile(journal) //nolint:gosec // G304: test-only path under t.TempDir()
	if err != nil {
		t.Fatalf("read %q: %v", journal, err)
	}

	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("journal has %d lines (%q), want 2 — the second emit did not append", len(lines), raw)
	}

	var got queueCancelOperatorEvent
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("decode journal line %q: %v", lines[0], err)
	}
	if got.EventType != "queue_cancelled_operator" {
		t.Errorf("event_type = %q, want %q", got.EventType, "queue_cancelled_operator")
	}
	if got.QueueID != "qid-first" {
		t.Errorf("queue_id = %q, want %q", got.QueueID, "qid-first")
	}
	if got.PriorStatus != "active" {
		t.Errorf("prior_status = %q, want %q", got.PriorStatus, "active")
	}
	if got.By != "operator" {
		t.Errorf("by = %q, want %q", got.By, "operator")
	}
	if got.EmittedAt == "" {
		t.Error("emitted_at is empty")
	}
}

// TestEmitQueueCancelEvent_ReportsMkdirFailure pins the MkdirAll failure path:
// a regular file planted where .harmonik/events belongs makes the directory
// creation fail, and that must be REPORTED rather than swallowed.
func TestEmitQueueCancelEvent_ReportsMkdirFailure(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	harmonikDir := filepath.Join(projectDir, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o750); err != nil {
		t.Fatalf("MkdirAll %q: %v", harmonikDir, err)
	}
	eventsPath := filepath.Join(harmonikDir, "events")
	if err := os.WriteFile(eventsPath, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatalf("plant a file at %q: %v", eventsPath, err)
	}

	err := emitQueueCancelEvent(projectDir, "qid-mkdir", "active")
	if err == nil {
		t.Fatal("emitQueueCancelEvent swallowed a MkdirAll failure and returned nil")
	}
	if !strings.Contains(err.Error(), "mkdir") {
		t.Errorf("error %q does not identify the failing step (mkdir)", err)
	}
}

// TestEmitQueueCancelEvent_ReportsOpenFailure pins the OpenFile failure path:
// events.jsonl is a DIRECTORY, so the append open fails after MkdirAll has
// already succeeded.
func TestEmitQueueCancelEvent_ReportsOpenFailure(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	journal := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	if err := os.MkdirAll(journal, 0o750); err != nil {
		t.Fatalf("MkdirAll %q: %v", journal, err)
	}

	err := emitQueueCancelEvent(projectDir, "qid-open", "active")
	if err == nil {
		t.Fatal("emitQueueCancelEvent swallowed an OpenFile failure and returned nil")
	}
	if !strings.Contains(err.Error(), "open") {
		t.Errorf("error %q does not identify the failing step (open)", err)
	}
}

// TestEmitQueueCancelEvent_ReportsWriteFailure_JoinsCloseResult pins the two
// remaining de9aac48 changes in this function: `_, _ = f.Write(line)` became an
// error return, and `defer func() { _ = f.Close() }()` became
// `defer func() { err = errors.Join(err, f.Close()) }()`.
//
// emitQueueCancelEvent opens its own file, so unlike writeTempAndClose it cannot
// be handed a pre-broken *os.File. Forcing a write failure therefore needs a
// path whose OPEN succeeds and whose first WRITE fails. A symlink to the write
// end of a pipe whose read end is already closed does exactly that: opening
// /dev/fd/N re-opens the pipe (darwin and linux both expose /dev/fd), and the
// first write returns EPIPE. Go only converts EPIPE to SIGPIPE on fds 1 and 2,
// so on a dup'd descriptor it surfaces as an ordinary error.
//
// Closing that descriptor then SUCCEEDS, which is what makes this a pin on the
// join rather than on the close error: with the join in place the result is an
// errors.Join fold (`Unwrap() []error`) carrying the write error; with the
// `_ = f.Close()` discard restored it is a bare *fmt.wrapError.
func TestEmitQueueCancelEvent_ReportsWriteFailure_JoinsCloseResult(t *testing.T) {
	t.Parallel()

	if _, statErr := os.Stat("/dev/fd"); statErr != nil {
		t.Skipf("no /dev/fd on this platform, cannot force a write failure: %v", statErr)
	}

	readEnd, writeEnd, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("os.Pipe: %v", pipeErr)
	}
	defer func() { _ = writeEnd.Close() }()
	// Closing the read end is what makes the later write fail with EPIPE.
	if closeErr := readEnd.Close(); closeErr != nil {
		t.Fatalf("close pipe read end: %v", closeErr)
	}

	projectDir := t.TempDir()
	eventsDir := filepath.Join(projectDir, ".harmonik", "events")
	if mkErr := os.MkdirAll(eventsDir, 0o750); mkErr != nil {
		t.Fatalf("MkdirAll %q: %v", eventsDir, mkErr)
	}
	journal := filepath.Join(eventsDir, "events.jsonl")
	target := fmt.Sprintf("/dev/fd/%d", writeEnd.Fd())
	if linkErr := os.Symlink(target, journal); linkErr != nil {
		t.Fatalf("symlink %q -> %q: %v", journal, target, linkErr)
	}

	gotErr := emitQueueCancelEvent(projectDir, "qid-write", "active")
	if gotErr == nil {
		t.Fatal("emitQueueCancelEvent swallowed a Write failure and returned nil")
	}
	if !strings.Contains(gotErr.Error(), "append to") {
		t.Errorf("error %q does not identify the failing step (append to)", gotErr)
	}
	if !errors.Is(gotErr, syscall.EPIPE) {
		t.Errorf("errors.Is(%v, syscall.EPIPE) = false; the write cause was lost", gotErr)
	}

	// The fold SHAPE is the regression target, so this asserts on the concrete
	// errors.Join type rather than going through errors.Is/As.
	joined, ok := gotErr.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("returned error %T (%v) is not an errors.Join fold: the deferred Close result was discarded", gotErr, gotErr)
	}
	parts := joined.Unwrap()
	if len(parts) == 0 {
		t.Fatal("errors.Join fold carries no causes")
	}
	var sawWriteErr bool
	for _, part := range parts {
		if errors.Is(part, syscall.EPIPE) {
			sawWriteErr = true
		}
	}
	if !sawWriteErr {
		t.Errorf("no joined cause reports the write failure: %v", parts)
	}
}
