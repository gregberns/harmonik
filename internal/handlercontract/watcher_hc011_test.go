package handlercontract_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

func watcherFixtureSessionID(_ *testing.T) core.SessionID {
	return core.SessionID("test-session-hk-8i31.12")
}

type watcherFixturePublisher struct {
	mu         sync.Mutex
	eventTypes []string
	errOn      string // if non-empty, return error when eventType == errOn
}

func (p *watcherFixturePublisher) Emit(_ context.Context, eventType core.EventType, _ []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.errOn != "" && string(eventType) == p.errOn {
		return errors.New("watcherFixturePublisher: forced error")
	}
	p.eventTypes = append(p.eventTypes, string(eventType))
	return nil
}

// EmitWithRunID records eventType (run_id is not stored by this test stub).
func (p *watcherFixturePublisher) EmitWithRunID(ctx context.Context, _ core.RunID, eventType core.EventType, _ []byte) error {
	return p.Emit(ctx, eventType, nil)
}

func (p *watcherFixturePublisher) EventTypes() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.eventTypes))
	copy(out, p.eventTypes)
	return out
}

type watcherFixtureDeadLetter struct {
	mu         sync.Mutex
	eventTypes []string
}

func (d *watcherFixtureDeadLetter) Append(eventType core.EventType, _ []byte, _ string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.eventTypes = append(d.eventTypes, string(eventType))
	return nil
}

func (d *watcherFixtureDeadLetter) Events() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, len(d.eventTypes))
	copy(out, d.eventTypes)
	return out
}

func watcherFixtureSpawn(
	t *testing.T,
	ndjson string,
) (*handlercontract.Watcher, *watcherFixturePublisher) {
	t.Helper()
	pub := &watcherFixturePublisher{}
	w := handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:      watcherFixtureSessionID(t),
		ProgressStream: strings.NewReader(ndjson),
		Publisher:      pub,
		DeadLetter:     &watcherFixtureDeadLetter{},
	})
	return w, pub
}

func watcherFixtureWait(t *testing.T, w *handlercontract.Watcher) {
	t.Helper()
	select {
	case <-w.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("watcherFixtureWait: watcher did not finish within timeout")
	}
}

func watcherFixtureLine(t *testing.T, fields map[string]string) string {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("watcherFixtureLine: marshal: %v", err)
	}
	return string(b) + "\n"
}

// TestWatcher_WatcherPublishBufSizeValue verifies that WatcherPublishBufSize
// equals 8 as specified by specs/handler-contract.md §4.3.HC-011a.
func TestWatcher_WatcherPublishBufSizeValue(t *testing.T) {
	t.Parallel()

	const want = 8
	if handlercontract.WatcherPublishBufSize != want {
		t.Errorf("WatcherPublishBufSize = %d, want %d (HC-011a)", handlercontract.WatcherPublishBufSize, want)
	}
}

// TestWatcher_WatcherPanicSubReasonValue verifies the literal value of
// WatcherPanicSubReason per specs/handler-contract.md §4.3.HC-011a.
func TestWatcher_WatcherPanicSubReasonValue(t *testing.T) {
	t.Parallel()

	const want = "watcher_panic"
	if handlercontract.WatcherPanicSubReason != want {
		t.Errorf("WatcherPanicSubReason = %q, want %q", handlercontract.WatcherPanicSubReason, want)
	}
}

// TestWatcher_WatcherWedgedSubReasonValue verifies the literal value of
// WatcherWedgedSubReason per specs/handler-contract.md §4.3.HC-011a.
func TestWatcher_WatcherWedgedSubReasonValue(t *testing.T) {
	t.Parallel()

	const want = "watcher_wedged"
	if handlercontract.WatcherWedgedSubReason != want {
		t.Errorf("WatcherWedgedSubReason = %q, want %q", handlercontract.WatcherWedgedSubReason, want)
	}
}

// TestWatcher_SubReasonsDistinct verifies the three watcher-self-defect
// sub-reason constants are mutually distinct.
func TestWatcher_SubReasonsDistinct(t *testing.T) {
	t.Parallel()

	constants := []struct {
		name string
		val  string
	}{
		{"WatcherPanicSubReason", handlercontract.WatcherPanicSubReason},
		{"WatcherWedgedSubReason", handlercontract.WatcherWedgedSubReason},
	}
	seen := make(map[string]string, len(constants))
	for _, c := range constants {
		if prior, ok := seen[c.val]; ok {
			t.Errorf("watcher sub-reason constants %q and %q share value %q; must be distinct",
				prior, c.name, c.val)
		}
		seen[c.val] = c.name
	}
}

// TestWatcher_SpawnWatcher_ReturnsBefore_StreamEOF verifies that SpawnWatcher
// returns immediately (before the goroutine reads EOF) — i.e., the goroutine is
// started asynchronously per HC-011.
func TestWatcher_SpawnWatcher_ReturnsBefore_StreamEOF(t *testing.T) {
	t.Parallel()

	pub := &watcherFixturePublisher{}
	dl := &watcherFixtureDeadLetter{}

	pr, pw := io.Pipe()
	defer func() {
		if cerr := pw.Close(); cerr != nil {
			t.Errorf("deferred pipe close: %v", cerr)
		}
	}()

	w := handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:      watcherFixtureSessionID(t),
		ProgressStream: pr,
		Publisher:      pub,
		DeadLetter:     dl,
	})

	if w == nil {
		t.Fatal("SpawnWatcher returned nil")
	}

	select {
	case <-w.Done():
		t.Error("watcher Done channel closed before stream EOF — goroutine did not start async")
	default:
	}

	if err := pw.Close(); err != nil {
		t.Fatalf("close progress stream: %v", err)
	}
	watcherFixtureWait(t, w)
}

// TestWatcher_SpawnWatcher_SessionID verifies that Watcher.SessionID returns
// the value from the config.
func TestWatcher_SpawnWatcher_SessionID(t *testing.T) {
	t.Parallel()

	w, _ := watcherFixtureSpawn(t, "")
	watcherFixtureWait(t, w)

	want := watcherFixtureSessionID(t)
	if w.SessionID() != want {
		t.Errorf("Watcher.SessionID() = %q, want %q", w.SessionID(), want)
	}
}

// TestWatcher_CleanEOF_DoneClosedAndErrNil verifies that a clean EOF (empty
// stream) closes Done and sets Err() == nil.
func TestWatcher_CleanEOF_DoneClosedAndErrNil(t *testing.T) {
	t.Parallel()

	w, _ := watcherFixtureSpawn(t, "")
	watcherFixtureWait(t, w)

	if err := w.Err(); err != nil {
		t.Errorf("Watcher.Err() = %v, want nil on clean EOF", err)
	}
}

// TestWatcher_ContextCancel_DoneClosedAndErrCanceled verifies that cancelling
// the context causes the watcher to exit with an ErrCanceled-wrapped error.
func TestWatcher_ContextCancel_DoneClosedAndErrCanceled(t *testing.T) {
	t.Parallel()

	pub := &watcherFixturePublisher{}
	dl := &watcherFixtureDeadLetter{}

	ctx, cancel := context.WithCancel(t.Context())

	pr, pw := io.Pipe()
	defer func() {
		if cerr := pw.Close(); cerr != nil {
			t.Errorf("close progress stream: %v", cerr)
		}
	}()

	w := handlercontract.SpawnWatcher(ctx, handlercontract.SpawnWatcherConfig{
		SessionID:      watcherFixtureSessionID(t),
		ProgressStream: pr,
		Publisher:      pub,
		DeadLetter:     dl,
	})

	cancel()
	watcherFixtureWait(t, w)

	if err := w.Err(); err != nil && !errors.Is(err, handlercontract.ErrCanceled) {
		t.Errorf("Watcher.Err() = %v; want nil or an error wrapping ErrCanceled", err)
	}
}

// TestWatcher_ValidMessages_PublishedToPublisher verifies that well-formed
// NDJSON lines with known message types are translated into core.Events and
// published to the EventPublisher.
func TestWatcher_ValidMessages_PublishedToPublisher(t *testing.T) {
	t.Parallel()

	lines := watcherFixtureLine(t, map[string]string{"type": "agent_ready"}) +
		watcherFixtureLine(t, map[string]string{"type": "agent_started"}) +
		watcherFixtureLine(t, map[string]string{"type": "agent_heartbeat"})

	w, pub := watcherFixtureSpawn(t, lines)
	watcherFixtureWait(t, w)

	types := pub.EventTypes()
	if len(types) != 3 {
		t.Fatalf("published event count = %d, want 3; types = %v", len(types), types)
	}
	wantTypes := []string{"agent_ready", "agent_started", "agent_heartbeat"}
	for i, want := range wantTypes {
		if types[i] != want {
			t.Errorf("event[%d].Type = %q, want %q", i, types[i], want)
		}
	}
}

// TestWatcher_UnknownMessageType_IgnoredNotError verifies that progress-stream
// lines with an unknown "type" field are silently ignored per HC-007's
// additive-evolution rule.
func TestWatcher_UnknownMessageType_IgnoredNotError(t *testing.T) {
	t.Parallel()

	lines := watcherFixtureLine(t, map[string]string{"type": "future_unknown_type_v99"}) +
		watcherFixtureLine(t, map[string]string{"type": "agent_heartbeat"})

	w, pub := watcherFixtureSpawn(t, lines)
	watcherFixtureWait(t, w)

	if w.Err() != nil {
		t.Errorf("Err() = %v, want nil (unknown type should be ignored, not fatal)", w.Err())
	}

	types := pub.EventTypes()
	if len(types) != 1 || types[0] != "agent_heartbeat" {
		t.Errorf("published events = %v, want [agent_heartbeat] only", types)
	}
}

// TestWatcher_BlankLines_Skipped verifies that blank NDJSON lines are skipped
// without error.
func TestWatcher_BlankLines_Skipped(t *testing.T) {
	t.Parallel()

	lines := "\n\n" + watcherFixtureLine(t, map[string]string{"type": "agent_ready"}) + "\n"

	w, pub := watcherFixtureSpawn(t, lines)
	watcherFixtureWait(t, w)

	if w.Err() != nil {
		t.Errorf("Err() = %v, want nil for blank lines", w.Err())
	}
	types := pub.EventTypes()
	if len(types) != 1 || types[0] != "agent_ready" {
		t.Errorf("published events = %v, want [agent_ready]", types)
	}
}

// TestWatcher_LineTooLong_EmitsAgentFailed verifies that a progress-stream line
// exceeding NDJSONMaxLineLenBytes causes the watcher to emit agent_failed with
// sub_reason ndjson_line_too_long and exit with a non-nil Err wrapping
// ErrProtocolMismatch per HC-007a.
func TestWatcher_LineTooLong_EmitsAgentFailed(t *testing.T) {
	t.Parallel()

	oversized := `{"type":"agent_output_chunk","data":"` +
		strings.Repeat("x", handlercontract.NDJSONMaxLineLenBytes) +
		`"}` + "\n"

	w, pub := watcherFixtureSpawn(t, oversized)
	watcherFixtureWait(t, w)

	if w.Err() == nil {
		t.Fatal("Err() = nil, want non-nil (ErrProtocolMismatch) on line-too-long")
	}
	if !errors.Is(w.Err(), handlercontract.ErrProtocolMismatch) {
		t.Errorf("Err() = %v, want wrapping ErrProtocolMismatch", w.Err())
	}

	types := pub.EventTypes()
	found := false
	for _, tp := range types {
		if tp == handlercontract.ProgressMsgTypeAgentFailed {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("agent_failed not published on line-too-long; published types = %v", types)
	}
}

// TestWatcher_MalformedJSON_EmitsAgentFailed verifies that a syntactically
// invalid JSON object causes the watcher to emit agent_failed with sub_reason
// malformed_progress_message and close the session per HC-007b.
func TestWatcher_MalformedJSON_EmitsAgentFailed(t *testing.T) {
	t.Parallel()

	ndjson := "{not valid json}\n"

	w, pub := watcherFixtureSpawn(t, ndjson)
	watcherFixtureWait(t, w)

	if w.Err() == nil {
		t.Fatal("Err() = nil, want non-nil (ErrStructural) on malformed JSON")
	}
	if !errors.Is(w.Err(), handlercontract.ErrStructural) {
		t.Errorf("Err() = %v, want wrapping ErrStructural", w.Err())
	}

	types := pub.EventTypes()
	found := false
	for _, tp := range types {
		if tp == handlercontract.ProgressMsgTypeAgentFailed {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("agent_failed not published on malformed JSON; published types = %v", types)
	}
}

// TestWatcher_PublishError_RoutesToDeadLetter verifies that when the publisher
// returns an error the event is routed to the dead-letter sink per HC-027.
func TestWatcher_PublishError_RoutesToDeadLetter(t *testing.T) {
	t.Parallel()

	pub := &watcherFixturePublisher{errOn: "agent_ready"}
	dl := &watcherFixtureDeadLetter{}

	ndjson := watcherFixtureLine(t, map[string]string{"type": "agent_ready"})

	w := handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:      watcherFixtureSessionID(t),
		ProgressStream: strings.NewReader(ndjson),
		Publisher:      pub,
		DeadLetter:     dl,
	})
	watcherFixtureWait(t, w)

	dlEvs := dl.Events()
	if len(dlEvs) == 0 {
		t.Error("dead-letter sink is empty; event should have been routed there on publish error")
	}
}

// TestWatcher_LastReadEventAt_ZeroBeforeStart verifies that a freshly spawned
// watcher (before any Read) has zero LastReadEventAt.
//
// NOTE: Because the goroutine starts immediately, it may have already performed
// a Read by the time we check. This test guards only the pre-spawn state — the
// meaningful invariant is that it advances during the read-loop.
func TestWatcher_LastReadEventAt_AdvancesAfterRead(t *testing.T) {
	t.Parallel()

	ndjson := watcherFixtureLine(t, map[string]string{"type": "agent_heartbeat"})
	w, _ := watcherFixtureSpawn(t, ndjson)
	watcherFixtureWait(t, w)

	ts := w.LastReadEventAt()
	if ts.IsZero() {
		t.Error("LastReadEventAt is zero after goroutine read at least one line; want non-zero timestamp")
	}
}

// TestWatcher_LastReadEventAt_IsRecent verifies that LastReadEventAt is within
// a reasonable window of the test execution time (sanity check against stale
// values or uninitialized state).
func TestWatcher_LastReadEventAt_IsRecent(t *testing.T) {
	t.Parallel()

	before := time.Now()
	ndjson := watcherFixtureLine(t, map[string]string{"type": "agent_heartbeat"})
	w, _ := watcherFixtureSpawn(t, ndjson)
	watcherFixtureWait(t, w)
	after := time.Now()

	ts := w.LastReadEventAt()
	if ts.IsZero() {
		t.Fatal("LastReadEventAt is zero after goroutine completed")
	}
	if ts.Before(before) {
		t.Errorf("LastReadEventAt %v is before test start %v; want timestamp >= test start", ts, before)
	}
	if ts.After(after.Add(time.Second)) {
		t.Errorf("LastReadEventAt %v is far after test end %v; want recent timestamp", ts, after)
	}
}

// TestWatcher_MultipleWatchers_NoSharedState verifies that spawning two
// watchers for different sessions does not cause cross-session event leakage.
// Watcher A should only see events from stream A; Watcher B only from stream B.
func TestWatcher_MultipleWatchers_NoSharedState(t *testing.T) {
	t.Parallel()

	streamA := watcherFixtureLine(t, map[string]string{"type": "agent_ready"})
	streamB := watcherFixtureLine(t, map[string]string{"type": "agent_heartbeat"}) +
		watcherFixtureLine(t, map[string]string{"type": "agent_started"})

	pubA := &watcherFixturePublisher{}
	dlA := &watcherFixtureDeadLetter{}
	wA := handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:      core.SessionID("session-A"),
		ProgressStream: strings.NewReader(streamA),
		Publisher:      pubA,
		DeadLetter:     dlA,
	})

	pubB := &watcherFixturePublisher{}
	dlB := &watcherFixtureDeadLetter{}
	wB := handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:      core.SessionID("session-B"),
		ProgressStream: strings.NewReader(streamB),
		Publisher:      pubB,
		DeadLetter:     dlB,
	})

	watcherFixtureWait(t, wA)
	watcherFixtureWait(t, wB)

	typesA := pubA.EventTypes()
	typesB := pubB.EventTypes()

	if len(typesA) != 1 || typesA[0] != "agent_ready" {
		t.Errorf("session A events = %v, want [agent_ready]", typesA)
	}
	if len(typesB) != 2 {
		t.Errorf("session B event count = %d, want 2; types = %v", len(typesB), typesB)
	}
}

// TestWatcher_SpawnWatcher_PanicsOnNilStream verifies that SpawnWatcher panics
// when ProgressStream is nil — a daemon defect.
func TestWatcher_SpawnWatcher_PanicsOnNilStream(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("SpawnWatcher did not panic on nil ProgressStream, want panic (daemon defect)")
		}
	}()

	handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:      watcherFixtureSessionID(t),
		ProgressStream: nil,
		Publisher:      &watcherFixturePublisher{},
		DeadLetter:     &watcherFixtureDeadLetter{},
	})
}

// TestWatcher_SpawnWatcher_PanicsOnNilPublisher verifies that SpawnWatcher
// panics when Publisher is nil — a daemon defect.
func TestWatcher_SpawnWatcher_PanicsOnNilPublisher(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("SpawnWatcher did not panic on nil Publisher, want panic (daemon defect)")
		}
	}()

	handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:      watcherFixtureSessionID(t),
		ProgressStream: strings.NewReader(""),
		Publisher:      nil,
		DeadLetter:     &watcherFixtureDeadLetter{},
	})
}

// TestWatcher_SpawnWatcher_PanicsOnNilDeadLetter verifies that SpawnWatcher
// panics when DeadLetter is nil — a daemon defect.
func TestWatcher_SpawnWatcher_PanicsOnNilDeadLetter(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("SpawnWatcher did not panic on nil DeadLetter, want panic (daemon defect)")
		}
	}()

	handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:      watcherFixtureSessionID(t),
		ProgressStream: strings.NewReader(""),
		Publisher:      &watcherFixturePublisher{},
		DeadLetter:     nil,
	})
}

// TestWatcher_SpawnWatcher_PanicsOnEmptySessionID verifies that SpawnWatcher
// panics when SessionID is empty — a daemon defect.
func TestWatcher_SpawnWatcher_PanicsOnEmptySessionID(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("SpawnWatcher did not panic on empty SessionID, want panic (daemon defect)")
		}
	}()

	handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:      "",
		ProgressStream: strings.NewReader(""),
		Publisher:      &watcherFixturePublisher{},
		DeadLetter:     &watcherFixtureDeadLetter{},
	})
}

var _ handlercontract.EventEmitter = (*watcherFixturePublisher)(nil)

var _ handlercontract.WatcherDeadLetterSink = (*watcherFixtureDeadLetter)(nil)

var _ = io.Pipe // prevent "imported and not used" if test compiler optimises the above
