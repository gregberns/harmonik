package keeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// awaitack_test.go — deterministic unit tests for the agent-side ACK observer
// (hk-uldg). All cases inject a fake PaneCapturer so NO real tmux is touched —
// this is the whole point of the CLI-over-prose choice (design §5).

// fakeCapturer is a programmable PaneCapturer. It returns bufs[i] (clamped to
// the last entry) on the i-th call, optionally erroring, and records the call
// count. Concurrency-safe (AwaitAck is single-goroutine, but be defensive).
type fakeCapturer struct {
	mu    sync.Mutex
	calls int
	// fn computes the (buffer, error) for the given 1-based call number.
	fn func(call int) (string, error)
}

func (f *fakeCapturer) capture(_ context.Context, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.fn(f.calls)
}

func (f *fakeCapturer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeClock returns a Now() func that starts at t0 and advances by step on each
// call. With step == poll and a small timeout, the deadline is crossed
// deterministically after a known number of polls — no wall-clock sleeps that
// matter (sleepCtx still runs, but the test uses a tiny poll so it is fast).
func fakeClock(t0 time.Time, step time.Duration) func() time.Time {
	var mu sync.Mutex
	cur := t0
	first := true
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		if first {
			first = false
			return cur
		}
		cur = cur.Add(step)
		return cur
	}
}

// TestAwaitAck_AckPresentImmediately: the capturer returns the ACK line on the
// first poll → AwaitAck returns nil and emits no timeout event (case a).
func TestAwaitAck_AckPresentImmediately(t *testing.T) {
	nonce := "rn-123"
	cap := &fakeCapturer{fn: func(int) (string, error) {
		return "some pane noise\n" + AckLine(nonce, "restart") + "\nmore", nil
	}}
	em := &RecordingEmitter{}
	err := AwaitAck(context.Background(), AwaitAckConfig{
		AgentName:  "captain",
		TmuxTarget: "sess:0.0",
		Nonce:      nonce,
		Kind:       "restart",
		Timeout:    5 * time.Second,
		Poll:       time.Millisecond,
		Capture:    cap.capture,
	}, em)
	if err != nil {
		t.Fatalf("want nil (ack present), got %v", err)
	}
	if got := len(em.EventsOfType(core.EventTypeSessionKeeperAckTimeout)); got != 0 {
		t.Fatalf("want 0 ack_timeout events, got %d", got)
	}
	if cap.count() != 1 {
		t.Fatalf("want exactly 1 capture call, got %d", cap.count())
	}
}

// TestAwaitAck_AckOnThirdPoll: the ACK appears only on the 3rd capture → returns
// nil and polled >= 3 times (case b-ish from §5).
func TestAwaitAck_AckOnThirdPoll(t *testing.T) {
	nonce := "ping-77"
	cap := &fakeCapturer{fn: func(call int) (string, error) {
		if call >= 3 {
			return AckLine(nonce, "ping"), nil
		}
		return "no ack yet", nil
	}}
	em := &RecordingEmitter{}
	err := AwaitAck(context.Background(), AwaitAckConfig{
		AgentName:  "crew-a",
		TmuxTarget: "sess:0.0",
		Nonce:      nonce,
		Kind:       "ping",
		Timeout:    5 * time.Second,
		Poll:       time.Millisecond,
		Capture:    cap.capture,
	}, em)
	if err != nil {
		t.Fatalf("want nil (ack on 3rd poll), got %v", err)
	}
	if cap.count() < 3 {
		t.Fatalf("want >= 3 capture calls, got %d", cap.count())
	}
	if got := len(em.EventsOfType(core.EventTypeSessionKeeperAckTimeout)); got != 0 {
		t.Fatalf("want 0 ack_timeout events, got %d", got)
	}
}

// TestAwaitAck_NeverAppears: the ACK never shows → with a fake clock advanced
// past the timeout, AwaitAck returns ErrAckTimeout and emits EXACTLY ONE
// session_keeper_ack_timeout event with reason ack_not_observed (case b/§5.3).
func TestAwaitAck_NeverAppears(t *testing.T) {
	nonce := "rn-999"
	cap := &fakeCapturer{fn: func(int) (string, error) {
		return "unrelated pane text without the token", nil
	}}
	em := &RecordingEmitter{}
	// step == 200ms, timeout 500ms → deadline crossed after a few polls.
	err := AwaitAck(context.Background(), AwaitAckConfig{
		AgentName:  "captain",
		TmuxTarget: "sess:0.0",
		Nonce:      nonce,
		Kind:       "restart",
		Timeout:    500 * time.Millisecond,
		Poll:       time.Millisecond,
		Capture:    cap.capture,
		Now:        fakeClock(time.Unix(0, 0), 200*time.Millisecond),
	}, em)
	if !errors.Is(err, ErrAckTimeout) {
		t.Fatalf("want ErrAckTimeout, got %v", err)
	}
	evs := em.EventsOfType(core.EventTypeSessionKeeperAckTimeout)
	if len(evs) != 1 {
		t.Fatalf("want exactly 1 ack_timeout event, got %d", len(evs))
	}
	var p core.SessionKeeperAckTimeoutPayload
	if uerr := json.Unmarshal(evs[0].Payload, &p); uerr != nil {
		t.Fatalf("unmarshal payload: %v", uerr)
	}
	if p.AgentName != "captain" || p.Nonce != nonce || p.Kind != "restart" {
		t.Fatalf("payload mismatch: %+v", p)
	}
	if p.Reason != "ack_not_observed" {
		t.Fatalf("want reason ack_not_observed, got %q", p.Reason)
	}
	if p.TimeoutSeconds != 0.5 {
		t.Fatalf("want timeout_seconds 0.5, got %v", p.TimeoutSeconds)
	}
}

// TestAwaitAck_WrongNonce: the pane contains a DIFFERENT nonce's ACK → no match
// → timeout path. Proves nonce discrimination / no false positive across cycles
// (case c, §5.4).
func TestAwaitAck_WrongNonce(t *testing.T) {
	wantNonce := "rn-555"
	cap := &fakeCapturer{fn: func(int) (string, error) {
		// A stale ACK from a previous cycle with a different nonce.
		return AckLine("rn-OTHER", "restart"), nil
	}}
	em := &RecordingEmitter{}
	err := AwaitAck(context.Background(), AwaitAckConfig{
		AgentName:  "captain",
		TmuxTarget: "sess:0.0",
		Nonce:      wantNonce,
		Kind:       "restart",
		Timeout:    500 * time.Millisecond,
		Poll:       time.Millisecond,
		Capture:    cap.capture,
		Now:        fakeClock(time.Unix(0, 0), 200*time.Millisecond),
	}, em)
	if !errors.Is(err, ErrAckTimeout) {
		t.Fatalf("want ErrAckTimeout (wrong nonce must not match), got %v", err)
	}
	if got := len(em.EventsOfType(core.EventTypeSessionKeeperAckTimeout)); got != 1 {
		t.Fatalf("want 1 ack_timeout event, got %d", got)
	}
}

// TestAwaitAck_CapturerError: the capturer errors every poll → bounded retries
// then a timeout-with-reason naming the capture failure; no panic (§5.5).
func TestAwaitAck_CapturerError(t *testing.T) {
	cap := &fakeCapturer{fn: func(int) (string, error) {
		return "", fmt.Errorf("tmux: no such pane")
	}}
	em := &RecordingEmitter{}
	err := AwaitAck(context.Background(), AwaitAckConfig{
		AgentName:  "captain",
		TmuxTarget: "sess:0.0",
		Nonce:      "rn-1",
		Kind:       "ping",
		Timeout:    10 * time.Second, // long timeout: the error budget must trip first
		Poll:       time.Millisecond,
		Capture:    cap.capture,
	}, em)
	if !errors.Is(err, ErrAckTimeout) {
		t.Fatalf("want ErrAckTimeout, got %v", err)
	}
	if cap.count() != captureErrorBudget {
		t.Fatalf("want exactly %d capture attempts (error budget), got %d", captureErrorBudget, cap.count())
	}
	if got := len(em.EventsOfType(core.EventTypeSessionKeeperAckTimeout)); got != 1 {
		t.Fatalf("want 1 ack_timeout event, got %d", got)
	}
}

// TestAwaitAck_NoTmuxTarget: an empty target fails fast with a no_tmux_target
// event and a timeout error (nothing to watch).
func TestAwaitAck_NoTmuxTarget(t *testing.T) {
	em := &RecordingEmitter{}
	err := AwaitAck(context.Background(), AwaitAckConfig{
		AgentName:  "captain",
		TmuxTarget: "",
		Nonce:      "rn-1",
		Kind:       "restart",
	}, em)
	if !errors.Is(err, ErrAckTimeout) {
		t.Fatalf("want ErrAckTimeout, got %v", err)
	}
	evs := em.EventsOfType(core.EventTypeSessionKeeperAckTimeout)
	if len(evs) != 1 {
		t.Fatalf("want 1 ack_timeout event, got %d", len(evs))
	}
	var p core.SessionKeeperAckTimeoutPayload
	if uerr := json.Unmarshal(evs[0].Payload, &p); uerr != nil {
		t.Fatalf("unmarshal payload: %v", uerr)
	}
	if p.Reason != "no_tmux_target" {
		t.Fatalf("want reason no_tmux_target, got %q", p.Reason)
	}
}

// TestAwaitAck_ContextCancel: a cancelled context returns the cancellation error
// and emits NO timeout event (the operator interrupted; the keeper is not
// implicated).
func TestAwaitAck_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cap := &fakeCapturer{fn: func(call int) (string, error) {
		if call == 1 {
			cancel() // cancel after the first (non-matching) capture
		}
		return "no ack", nil
	}}
	em := &RecordingEmitter{}
	err := AwaitAck(ctx, AwaitAckConfig{
		AgentName:  "captain",
		TmuxTarget: "sess:0.0",
		Nonce:      "rn-1",
		Kind:       "ping",
		Timeout:    10 * time.Second,
		Poll:       50 * time.Millisecond,
		Capture:    cap.capture,
	}, em)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if got := len(em.EventsOfType(core.EventTypeSessionKeeperAckTimeout)); got != 0 {
		t.Fatalf("want 0 ack_timeout events on cancel, got %d", got)
	}
}

// TestAckMatchToken pins the exact match token shape — it is the bracket prefix
// of AckLine, independent of the kind tail.
func TestAckMatchToken(t *testing.T) {
	tok := AckMatchToken("rn-42")
	if tok != "[KEEPER ACK rn-42]" {
		t.Fatalf("unexpected token %q", tok)
	}
	// The token must be a substring of BOTH restart and ping ACK lines.
	for _, kind := range []string{"restart", "ping"} {
		line := AckLine("rn-42", kind)
		if !contains(line, tok) {
			t.Fatalf("token %q not found in ack line %q", tok, line)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
