package handler

// deadletterlog_hk0eqik_test.go — regression for the dead-letter failure logger
// (hk-0eqik, remediation round).
//
// Two things are pinned here, both of which were invisible to the original
// change's tests:
//
//  1. The hook SAMPLES. The first shape of this hook wrote one unbuffered
//     stderr line per lost event, inline on the watcher's read loop. That turns
//     a broken dead-letter sink — the exact failure the bead is about, and one
//     that fires once per progress line when the bus is also down — into an
//     unbounded write storm on the goroutine whose progress HC-011a wedge
//     detection watches.
//  2. The hook is actually INSTALLED by Launch. Nothing asserted that either
//     SpawnWatcher config literal in handler.go carried OnDeadLetterFailure, so
//     dropping the line was a silent regression.
//
// Helper prefix: dlLogFixture.

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// dlLogFixtureFailEmitter fails every Emit, so every progress line the watcher
// reads spills to the dead-letter sink.
type dlLogFixtureFailEmitter struct{}

func (dlLogFixtureFailEmitter) Emit(_ context.Context, _ core.EventType, _ []byte) error {
	return errors.New("dlLogFixtureFailEmitter: bus unavailable")
}

func (e dlLogFixtureFailEmitter) EmitWithRunID(ctx context.Context, _ core.RunID, et core.EventType, pl []byte) error {
	return e.Emit(ctx, et, pl)
}

// dlLogFixtureFailSink fails every Append, so every spill is a lost event.
type dlLogFixtureFailSink struct{}

func (dlLogFixtureFailSink) Append(_ core.EventType, _ []byte, _ string) error {
	return errors.New("dlLogFixtureFailSink: sink is down")
}

// dlLogFixtureBuf is a concurrency-safe bytes.Buffer. The logger runs on the
// watcher goroutine; the test reads after Done(), but a mutex keeps -race
// honest regardless of when the read lands.
type dlLogFixtureBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *dlLogFixtureBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *dlLogFixtureBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestNewDeadLetterFailureLogger_SamplesAtPowersOfTwo is the rate-limit pin: n
// failures must produce O(log n) lines, not n. Without sampling this test sees
// 1000 lines instead of 10.
func TestNewDeadLetterFailureLogger_SamplesAtPowersOfTwo(t *testing.T) {
	t.Parallel()

	var buf dlLogFixtureBuf
	log := newDeadLetterFailureLogger(&buf)

	const failures = 1000
	for i := 0; i < failures; i++ {
		log(core.EventTypeAgentOutputChunk, "emit failed: bus unavailable", errors.New("sink is down"))
	}

	got := strings.Count(buf.String(), "\n")
	// Powers of two ≤ 1000: 1 2 4 8 16 32 64 128 256 512 → 10 lines.
	const want = 10
	if got != want {
		t.Errorf("logger wrote %d lines for %d failures, want %d — the hook runs inline on the watcher read loop and MUST NOT emit one line per lost event",
			got, failures, want)
	}
}

// TestNewDeadLetterFailureLogger_FirstFailureIsReported guards the other edge of
// sampling: the FIRST failure must always be reported, and the line must carry
// the event type, the reason, the error, and a pointer to the exact count.
func TestNewDeadLetterFailureLogger_FirstFailureIsReported(t *testing.T) {
	t.Parallel()

	var buf dlLogFixtureBuf
	log := newDeadLetterFailureLogger(&buf)
	log(core.EventTypeAgentOutputChunk, "emit failed: bus unavailable", errors.New("sink is down"))

	line := buf.String()
	if line == "" {
		t.Fatal("logger wrote nothing for the first failure, want one line")
	}
	for _, want := range []string{
		string(core.EventTypeAgentOutputChunk),
		"emit failed: bus unavailable",
		"sink is down",
		"Watcher.DeadLetterFailures()",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("first log line = %q, want it to contain %q", line, want)
		}
	}
}

// TestNewDeadLetterFailureLogger_SamplingIsPerWatcher pins that the factory
// returns independent closures: a busy failing session must not sample away a
// different session's first report.
func TestNewDeadLetterFailureLogger_SamplingIsPerWatcher(t *testing.T) {
	t.Parallel()

	var busy, quiet dlLogFixtureBuf
	logBusy := newDeadLetterFailureLogger(&busy)
	logQuiet := newDeadLetterFailureLogger(&quiet)

	for i := 0; i < 100; i++ {
		logBusy(core.EventTypeAgentOutputChunk, "reason", errors.New("boom"))
	}
	logQuiet(core.EventTypeAgentOutputChunk, "reason", errors.New("boom"))

	if got := strings.Count(quiet.String(), "\n"); got != 1 {
		t.Errorf("second watcher's logger wrote %d lines for its first failure, want 1 — sampling state must be per-watcher, not package-global", got)
	}
}

// dlLogFixtureBrokenWriter fails every Write and counts the attempts — a stderr
// that has become a closed pipe.
type dlLogFixtureBrokenWriter struct {
	attempts int
}

func (w *dlLogFixtureBrokenWriter) Write(p []byte) (int, error) {
	w.attempts++
	return 0, errors.New("dlLogFixtureBrokenWriter: broken pipe")
}

// TestNewDeadLetterFailureLogger_MutesAfterWriteError pins the second bound on
// this hook: when the log sink itself is broken, the logger stops trying rather
// than paying for the same failing write on every sampled failure. There is no
// second place to report a failed log write, and the count on the watcher handle
// is unaffected either way.
func TestNewDeadLetterFailureLogger_MutesAfterWriteError(t *testing.T) {
	t.Parallel()

	w := &dlLogFixtureBrokenWriter{}
	log := newDeadLetterFailureLogger(w)
	for i := 0; i < 1000; i++ {
		log(core.EventTypeAgentOutputChunk, "reason", errors.New("boom"))
	}

	if w.attempts != 1 {
		t.Errorf("logger attempted %d writes against a broken sink, want 1 — it must mute after the first write error", w.attempts)
	}
}

// TestHandler_Launch_InstallsDeadLetterFailureHook is the wiring pin: it drives
// a real Launch with a bus that always fails AND a dead-letter sink that always
// fails, and asserts the report reaches the handler's log writer. Removing
// OnDeadLetterFailure from handler.go's SpawnWatcher config fails this test.
func TestHandler_Launch_InstallsDeadLetterFailureHook(t *testing.T) {
	h, ok := NewHandler(dlLogFixtureFailEmitter{}, dlLogFixtureFailSink{}, handlercontract.NewAdapterRegistry()).(*handler)
	if !ok {
		t.Fatal("NewHandler did not return *handler")
	}
	var buf dlLogFixtureBuf
	h.deadLetterFailureLog = &buf

	spec := LaunchSpec{
		Binary:  "sh",
		Args:    []string{"-c", `printf '{"type":"agent_ready"}\n'`},
		Env:     []string{},
		WorkDir: t.TempDir(),
		Role:    "test",
	}

	sess, watcher, err := h.Launch(t.Context(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	select {
	case <-watcher.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("watcher.Done() did not close within timeout")
	}
	if err := sess.Wait(t.Context()); err != nil {
		t.Logf("Session.Wait: %v (not fatal: this test is about the hook, not the child)", err)
	}

	if got := watcher.DeadLetterFailures(); got == 0 {
		t.Fatal("Watcher.DeadLetterFailures() = 0; the fixture bus and sink both fail, so the child's agent_ready line must have been lost")
	}
	if got := buf.String(); !strings.Contains(got, "watcher dead-letter sink failed") {
		t.Errorf("handler's dead-letter log = %q, want the OnDeadLetterFailure report — Launch must install the hook on the watcher it spawns", got)
	}
}
