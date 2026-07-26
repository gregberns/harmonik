package digestcmd

// watch_render_test.go — behaviour tests for the --watch render/loop plumbing:
// the watchFrame accumulator, renderWatchFrame's degrade + formatting branches,
// and RunWatch's immediate-render + context-cancel shutdown path.
//
// These complement watch_test.go (which already covers formatDuration,
// filterEventsByType, and uuidv7Age).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/digest"
)

// failingWriter always errors, so writeTo's write-error branch is exercised.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("boom") }

// TestWatchFrame_Accumulate verifies print/printf/println accumulate into one
// buffer and writeTo emits the whole frame to the destination writer.
func TestWatchFrame_Accumulate(t *testing.T) {
	t.Parallel()
	var f watchFrame
	f.print("a", "b")
	f.printf("-%d-", 7)
	f.println("z")

	var out bytes.Buffer
	if err := f.writeTo(&out); err != nil {
		t.Fatalf("writeTo: %v", err)
	}
	if got := out.String(); got != "ab-7-z\n" {
		t.Errorf("frame = %q; want %q", got, "ab-7-z\n")
	}
}

// TestWatchFrame_StickyError verifies that once a frame records an error every
// subsequent print is a no-op and writeTo surfaces the recorded error rather
// than emitting a partial frame.
func TestWatchFrame_StickyError(t *testing.T) {
	t.Parallel()
	var f watchFrame
	f.print("before")
	f.err = errors.New("seeded")
	// These must all no-op now (sticky-error guard).
	f.print("x")
	f.printf("%d", 1)
	f.println("y")

	var out bytes.Buffer
	err := f.writeTo(&out)
	if err == nil {
		t.Fatal("expected writeTo to return the sticky error")
	}
	if !strings.Contains(err.Error(), "format watch frame") {
		t.Errorf("error = %v; want it to wrap 'format watch frame'", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no bytes written on sticky error; got %q", out.String())
	}
}

// TestWatchFrame_WriteError verifies writeTo wraps a destination write failure.
func TestWatchFrame_WriteError(t *testing.T) {
	t.Parallel()
	var f watchFrame
	f.print("payload")
	err := f.writeTo(failingWriter{})
	if err == nil || !strings.Contains(err.Error(), "write watch frame") {
		t.Fatalf("expected wrapped write error; got %v", err)
	}
}

// TestRenderWatchFrame_NoHarmonikDir verifies the graceful-degrade branch: a
// project dir with no .harmonik/ renders the friendly not-found message and
// returns nil (never a hard error).
func TestRenderWatchFrame_NoHarmonikDir(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	err := renderWatchFrame(context.Background(), &out, digest.BuildInput{
		ProjectDir: t.TempDir(), // no .harmonik/ inside
		Limits:     digest.DefaultLimits(),
	})
	if err != nil {
		t.Fatalf("renderWatchFrame: %v", err)
	}
	if !strings.Contains(out.String(), ".harmonik/ directory not found") {
		t.Errorf("expected not-found message; got:\n%s", out.String())
	}
}

// TestRenderWatchFrame_EmptyProject verifies the success path over an empty
// .harmonik/: header, metadata, and every empty section render.
func TestRenderWatchFrame_EmptyProject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o700); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := renderWatchFrame(context.Background(), &out, digest.BuildInput{
		ProjectDir: dir,
		Limits:     digest.DefaultLimits(),
	})
	if err != nil {
		t.Fatalf("renderWatchFrame: %v", err)
	}
	s := out.String()
	for _, want := range []string{
		"harmonik digest --watch",
		"schema_version:",
		"watermark age:  (no events)",
		"(no active queue)",
		"=== Recent completions (0) ===",
		"=== Open notes (0) ===",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q; got:\n%s", want, s)
		}
	}
}

// TestRenderWatchFrame_WithActiveQueue verifies the in-flight-runs branch: a
// populated queue renders the active count and each dispatched run, truncating
// long run_ids to 8 chars and labelling empty run_ids.
func TestRenderWatchFrame_WithActiveQueue(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeActiveQueue(t, dir)

	var out bytes.Buffer
	err := renderWatchFrame(context.Background(), &out, digest.BuildInput{
		ProjectDir: dir,
		Limits:     digest.DefaultLimits(),
	})
	if err != nil {
		t.Fatalf("renderWatchFrame: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "In-flight runs (2 active, 1 pending)") {
		t.Errorf("expected active/pending counts; got:\n%s", s)
	}
	if !strings.Contains(s, "hk-alpha") || !strings.Contains(s, "hk-beta") {
		t.Errorf("expected both dispatched bead ids; got:\n%s", s)
	}
	// run_id "0123456789..." truncates to first 8 hex chars.
	if !strings.Contains(s, "run=01234567") {
		t.Errorf("expected truncated run id; got:\n%s", s)
	}
	if !strings.Contains(s, "[1 pending in queue]") {
		t.Errorf("expected pending-in-queue line; got:\n%s", s)
	}
}

// TestRunWatch_CancelledContextStops verifies RunWatch renders an immediate
// frame and, on a cancelled context, clears the screen and prints the shutdown
// line, returning nil. Interval:0 also exercises the default-cadence branch.
func TestRunWatch_CancelledContextStops(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already done before the loop starts

	var out bytes.Buffer
	err := RunWatch(ctx, WatchInput{
		Build:    digest.BuildInput{ProjectDir: t.TempDir(), Limits: digest.DefaultLimits()},
		Interval: 0, // default-cadence branch
	}, &out)
	if err != nil {
		t.Fatalf("RunWatch: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "harmonik digest --watch: stopped.") {
		t.Errorf("expected shutdown line; got:\n%s", s)
	}
	// The immediate pre-tick frame rendered before shutdown (no .harmonik/).
	if !strings.Contains(s, ".harmonik/ directory not found") {
		t.Errorf("expected immediate frame before shutdown; got:\n%s", s)
	}
}

// writeActiveQueue writes .harmonik/queues/main.json with two dispatched items
// (one with a long run_id, exercising truncation) and one pending item.
func writeActiveQueue(t *testing.T, dir string) {
	t.Helper()
	type itemJSON struct {
		BeadID   string  `json:"bead_id"`
		Status   string  `json:"status"`
		RunID    *string `json:"run_id"`
		Attempts int     `json:"attempts"`
	}
	type groupJSON struct {
		GroupIndex int        `json:"group_index"`
		Kind       string     `json:"kind"`
		Status     string     `json:"status"`
		Items      []itemJSON `json:"items"`
		CreatedAt  string     `json:"created_at"`
	}
	type queueJSON struct {
		SchemaVersion int         `json:"schema_version"`
		QueueID       string      `json:"queue_id"`
		SubmittedAt   string      `json:"submitted_at"`
		Groups        []groupJSON `json:"groups"`
		Status        string      `json:"status"`
	}
	longRun := "0123456789ab-cdef-0000-0000-000000000000"
	q := queueJSON{
		SchemaVersion: 1,
		QueueID:       "00000000-0000-0000-0000-000000000099",
		SubmittedAt:   "2026-01-01T00:00:00Z",
		Status:        "active",
		Groups: []groupJSON{{
			GroupIndex: 0,
			Kind:       "stream",
			Status:     "active",
			CreatedAt:  "2026-01-01T00:00:00Z",
			Items: []itemJSON{
				{BeadID: "hk-alpha", Status: "dispatched", RunID: &longRun, Attempts: 1},
				{BeadID: "hk-beta", Status: "dispatched", RunID: nil, Attempts: 1},
				{BeadID: "hk-gamma", Status: "pending", RunID: nil, Attempts: 0},
			},
		}},
	}
	data, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	queuesDir := filepath.Join(dir, ".harmonik", "queues")
	if err := os.MkdirAll(queuesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(queuesDir, "main.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
