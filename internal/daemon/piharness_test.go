package daemon_test

// piharness_test.go — PiHarness + pijsonlparser unit tests (codename:pilot, PI-010/012/013).
//
// Coverage (PI-100 MUST):
//   - parsePiNDJSONEvent: session header, agent_end, unknown type, malformed line.
//   - piSessionIDInterceptor: fires callback on first session header; passthrough
//     bytes unchanged; first non-empty id wins; no callback on non-session lines.
//   - PiHarness constant methods: AgentType=pi, SessionIDPolicy=Captured,
//     Completion=ProcessExit.
//   - DetectReady HC-041: false for launch_initiated, true for agent_ready, false
//     for unrelated events.
//   - Seed/Retask: no-op (nil error, no side effects).
//   - Teardown: nil session is a no-op; live session is Kill()ed.

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// parsePiNDJSONEvent tests
// ─────────────────────────────────────────────────────────────────────────────

func TestParsePiNDJSONEvent_SessionHeader(t *testing.T) {
	t.Parallel()

	line := []byte(`{"type":"session","version":3,"id":"550e8400-e29b-41d4-a716-446655440000","cwd":"/tmp/wt"}`)
	kind, rawType, sessionID, err := daemon.ExportedParsePiNDJSONEvent(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kind != daemon.ExportedPiEventKindSession {
		t.Errorf("Kind = %v; want piEventKindSession", kind)
	}
	if rawType != "session" {
		t.Errorf("RawType = %q; want %q", rawType, "session")
	}
	if sessionID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("SessionID = %q; want UUID", sessionID)
	}
}

func TestParsePiNDJSONEvent_AgentEnd(t *testing.T) {
	t.Parallel()

	line := []byte(`{"type":"agent_end","messages":[]}`)
	kind, rawType, _, err := daemon.ExportedParsePiNDJSONEvent(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kind != daemon.ExportedPiEventKindAgentEnd {
		t.Errorf("Kind = %v; want piEventKindAgentEnd", kind)
	}
	if rawType != "agent_end" {
		t.Errorf("RawType = %q; want %q", rawType, "agent_end")
	}
}

func TestParsePiNDJSONEvent_UnknownType(t *testing.T) {
	t.Parallel()

	line := []byte(`{"type":"tool_use","name":"bash"}`)
	kind, rawType, _, err := daemon.ExportedParsePiNDJSONEvent(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kind != daemon.ExportedPiEventKindOther {
		t.Errorf("Kind = %v; want piEventKindOther", kind)
	}
	if rawType != "tool_use" {
		t.Errorf("RawType = %q; want %q", rawType, "tool_use")
	}
}

func TestParsePiNDJSONEvent_Malformed(t *testing.T) {
	t.Parallel()

	_, _, _, err := daemon.ExportedParsePiNDJSONEvent([]byte("not json"))
	if err == nil {
		t.Error("expected error for malformed input; got nil")
	}
}

func TestParsePiNDJSONEvent_EmptyLine(t *testing.T) {
	t.Parallel()

	_, _, _, err := daemon.ExportedParsePiNDJSONEvent([]byte(""))
	if err == nil {
		t.Error("expected error for empty line; got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// piSessionIDInterceptor tests
// ─────────────────────────────────────────────────────────────────────────────

// TestPiSessionIDInterceptor_FiresOnSessionHeader verifies the interceptor fires
// the callback with the session id from the first {"type":"session",...} line.
func TestPiSessionIDInterceptor_FiresOnSessionHeader(t *testing.T) {
	t.Parallel()

	const wantID = "550e8400-e29b-41d4-a716-446655440000"
	ndjson := `{"type":"session","version":3,"id":"` + wantID + `","cwd":"/tmp/wt"}` + "\n"

	var gotID string
	cb := func(id string) { gotID = id }

	interceptor := daemon.ExportedNewPiSessionIDInterceptor(strings.NewReader(ndjson), cb, nil)
	if _, err := io.ReadAll(interceptor); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if gotID != wantID {
		t.Errorf("captured session id = %q; want %q", gotID, wantID)
	}
}

// TestPiSessionIDInterceptor_PassesThrough verifies all bytes from the stream
// are returned unchanged.
func TestPiSessionIDInterceptor_PassesThrough(t *testing.T) {
	t.Parallel()

	const wantID = "abc-123"
	input := `{"type":"session","id":"` + wantID + `"}` + "\n" +
		`{"type":"tool_use","name":"bash"}` + "\n"

	interceptor := daemon.ExportedNewPiSessionIDInterceptor(strings.NewReader(input), func(string) {}, nil)
	got, err := io.ReadAll(interceptor)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != input {
		t.Errorf("passthrough mismatch:\ngot  %q\nwant %q", string(got), input)
	}
}

// TestPiSessionIDInterceptor_FirstSessionWins verifies only the first
// {"type":"session",...} event's id is captured when the stream has multiple.
func TestPiSessionIDInterceptor_FirstSessionWins(t *testing.T) {
	t.Parallel()

	const firstID = "first-session-id"
	const secondID = "second-session-id"
	ndjson := `{"type":"session","id":"` + firstID + `"}` + "\n" +
		`{"type":"session","id":"` + secondID + `"}` + "\n"

	var captured []string
	cb := func(id string) { captured = append(captured, id) }

	interceptor := daemon.ExportedNewPiSessionIDInterceptor(strings.NewReader(ndjson), cb, nil)
	if _, err := io.ReadAll(interceptor); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(captured) != 1 {
		t.Fatalf("callback fired %d times; want 1", len(captured))
	}
	if captured[0] != firstID {
		t.Errorf("captured id = %q; want %q", captured[0], firstID)
	}
}

// TestPiSessionIDInterceptor_NoSessionLine verifies the callback is never fired
// when no {"type":"session",...} line appears in the stream.
func TestPiSessionIDInterceptor_NoSessionLine(t *testing.T) {
	t.Parallel()

	ndjson := `{"type":"tool_use","name":"bash"}` + "\n" +
		`{"type":"agent_end","messages":[]}` + "\n"

	fired := false
	interceptor := daemon.ExportedNewPiSessionIDInterceptor(strings.NewReader(ndjson), func(string) {
		fired = true
	}, nil)
	if _, err := io.ReadAll(interceptor); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if fired {
		t.Error("callback fired without a session line in the stream")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// PiHarness constant-method tests
// ─────────────────────────────────────────────────────────────────────────────

func TestPiHarness_AgentType(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewPiHarness("", "", "", "")
	if got := h.AgentType(); got != core.AgentTypePi {
		t.Errorf("AgentType = %q; want %q", got, core.AgentTypePi)
	}
}

func TestPiHarness_SessionIDPolicy(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewPiHarness("", "", "", "")
	if got := h.SessionIDPolicy(); got != handlercontract.SessionIDCaptured {
		t.Errorf("SessionIDPolicy = %v; want SessionIDCaptured", got)
	}
}

func TestPiHarness_Completion(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewPiHarness("", "", "", "")
	if got := h.Completion(); got != handlercontract.CompletionProcessExit {
		t.Errorf("Completion = %v; want CompletionProcessExit", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// DetectReady HC-041 tests (PI-013)
// ─────────────────────────────────────────────────────────────────────────────

// TestPiHarness_DetectReady_LaunchInitiated verifies DetectReady returns false
// for launch_initiated — HC-041 hard rule; MUST NOT return true for this event.
func TestPiHarness_DetectReady_LaunchInitiated(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewPiHarness("", "", "", "")
	ev := handlercontract.EventEnvelope{Type: string(core.EventTypeLaunchInitiated)}
	if h.DetectReady(ev) {
		t.Error("DetectReady(launch_initiated) = true; want false (HC-041)")
	}
}

// TestPiHarness_DetectReady_AgentReady verifies DetectReady returns true for
// agent_ready (the only event for which it may return true).
func TestPiHarness_DetectReady_AgentReady(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewPiHarness("", "", "", "")
	ev := handlercontract.EventEnvelope{Type: string(core.EventTypeAgentReady)}
	if !h.DetectReady(ev) {
		t.Error("DetectReady(agent_ready) = false; want true")
	}
}

// TestPiHarness_DetectReady_OtherEvent verifies DetectReady returns false for
// an arbitrary unrelated event type.
func TestPiHarness_DetectReady_OtherEvent(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewPiHarness("", "", "", "")
	ev := handlercontract.EventEnvelope{Type: "run_started"}
	if h.DetectReady(ev) {
		t.Error("DetectReady(run_started) = true; want false")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Seed / Retask no-op tests
// ─────────────────────────────────────────────────────────────────────────────

func TestPiHarness_Seed_NoOp(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewPiHarness("", "", "", "")
	if err := h.Seed(nil, handlercontract.RunCtx{}); err != nil {
		t.Errorf("Seed returned error: %v", err)
	}
}

func TestPiHarness_Retask_NoOp(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewPiHarness("", "", "", "")
	if err := h.Retask(nil, "feedback", handlercontract.RunCtx{}); err != nil {
		t.Errorf("Retask returned error: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Teardown tests
// ─────────────────────────────────────────────────────────────────────────────

func TestPiHarness_Teardown_NilSession(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewPiHarness("", "", "", "")
	if err := h.Teardown(nil); err != nil {
		t.Errorf("Teardown(nil) returned error: %v", err)
	}
}

// killTrackingSession is a minimal handlercontract.Session implementation that
// records Kill calls for Teardown verification (PI-100).
type killTrackingSession struct {
	killCalled int
}

func (s *killTrackingSession) ID() core.SessionID                          { return "test-pi-session" }
func (s *killTrackingSession) SendInput(_ context.Context, _ string) error { return nil }
func (s *killTrackingSession) Attach(_ context.Context) (io.Reader, error) { return nil, nil }
func (s *killTrackingSession) Kill(_ context.Context) error                { s.killCalled++; return nil }
func (s *killTrackingSession) Wait(_ context.Context) (core.Outcome, error) {
	return core.Outcome{}, nil
}
func (s *killTrackingSession) LogLocation() string { return "" }

// TestPiHarness_Teardown_LiveSession_Kill verifies Teardown calls Kill on a
// live (non-nil) session — PI-100 "live session is Kill()ed" coverage.
func TestPiHarness_Teardown_LiveSession_Kill(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewPiHarness("", "", "", "")
	sess := &killTrackingSession{}
	if err := h.Teardown(sess); err != nil {
		t.Errorf("Teardown(live session) returned error: %v", err)
	}
	if sess.killCalled != 1 {
		t.Errorf("Kill called %d times; want 1 (Teardown must Kill live session)", sess.killCalled)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// PI-014: agent_end watcher tests (hk-mkcwg)
// ─────────────────────────────────────────────────────────────────────────────

// TestPiSessionIDInterceptor_AgentEndCb_Fires verifies agentEndCb fires on the
// first {"type":"agent_end",...} line, even when no session line appears.
func TestPiSessionIDInterceptor_AgentEndCb_Fires(t *testing.T) {
	t.Parallel()

	ndjson := `{"type":"tool_use","name":"bash"}` + "\n" +
		`{"type":"agent_end","messages":[]}` + "\n"

	var fired int
	agentEndCb := func() { fired++ }

	interceptor := daemon.ExportedNewPiSessionIDInterceptor(strings.NewReader(ndjson), func(string) {}, agentEndCb)
	if _, err := io.ReadAll(interceptor); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if fired != 1 {
		t.Errorf("agentEndCb fired %d times; want 1", fired)
	}
}

// TestPiSessionIDInterceptor_AgentEndCb_AfterSessionID verifies both callbacks
// fire independently: sessionIDCb on the session header, agentEndCb on agent_end.
func TestPiSessionIDInterceptor_AgentEndCb_AfterSessionID(t *testing.T) {
	t.Parallel()

	const wantID = "test-session-uuid"
	ndjson := `{"type":"session","id":"` + wantID + `"}` + "\n" +
		`{"type":"tool_use","name":"bash"}` + "\n" +
		`{"type":"agent_end","messages":[]}` + "\n"

	var gotID string
	var agentEndFired int
	interceptor := daemon.ExportedNewPiSessionIDInterceptor(
		strings.NewReader(ndjson),
		func(id string) { gotID = id },
		func() { agentEndFired++ },
	)
	if _, err := io.ReadAll(interceptor); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if gotID != wantID {
		t.Errorf("sessionIDCb got %q; want %q", gotID, wantID)
	}
	if agentEndFired != 1 {
		t.Errorf("agentEndCb fired %d times; want 1", agentEndFired)
	}
}

// TestPiSessionIDInterceptor_AgentEndCb_FiringOnce verifies agentEndCb fires at
// most once even when multiple agent_end lines appear in the stream.
func TestPiSessionIDInterceptor_AgentEndCb_FiringOnce(t *testing.T) {
	t.Parallel()

	ndjson := `{"type":"agent_end","messages":[]}` + "\n" +
		`{"type":"agent_end","messages":[]}` + "\n"

	var fired int
	interceptor := daemon.ExportedNewPiSessionIDInterceptor(strings.NewReader(ndjson), func(string) {}, func() { fired++ })
	if _, err := io.ReadAll(interceptor); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if fired != 1 {
		t.Errorf("agentEndCb fired %d times; want 1", fired)
	}
}

// TestPiSessionIDInterceptor_NilAgentEndCb_Safe verifies a nil agentEndCb does
// not panic on an agent_end event.
func TestPiSessionIDInterceptor_NilAgentEndCb_Safe(t *testing.T) {
	t.Parallel()

	ndjson := `{"type":"agent_end","messages":[]}` + "\n"

	interceptor := daemon.ExportedNewPiSessionIDInterceptor(strings.NewReader(ndjson), func(string) {}, nil)
	if _, err := io.ReadAll(interceptor); err != nil {
		t.Fatalf("ReadAll: %v (nil agentEndCb must not panic)", err)
	}
}
