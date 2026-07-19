package handlercontract_test

// watcher_protocolmismatch_hk9ngiv_test.go — regression for the version-negotiation
// sentinel-loss bug (hk-9ngiv).
//
// Bug: when a handler advertises an unsupported wire version (or never emits
// handler_capabilities within the caps timeout), the SessionIDInterceptor sets a
// sticky negoErr wrapping ErrProtocolMismatch, which surfaces through the
// progress-stream scanner. The watcher's scanner-error branch flattened it with
// fmt.Errorf("...: %v: %w", scanErr, ErrStructural), string-collapsing the
// sentinel out of the chain: errors.Is(watcher.Err(), ErrProtocolMismatch) was
// false, so the orchestrator's retry policy could not distinguish a genuine
// protocol mismatch (which MUST NOT retry-spin the same pinned binary, §8.7 /
// HC-021) from a transient framing error.
//
// Fix (hk-9ngiv): a dedicated `errors.Is(scanErr, ErrProtocolMismatch)` branch
// (mirroring the isLineTooLong branch) preserves the sentinel with %w and emits
// agent_failed with sub_reason=protocol_mismatch.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// protoMismatchReader models the progress stream after a failed version
// negotiation: every Read returns the interceptor's sticky negoErr, which wraps
// ErrProtocolMismatch (exactly as the SessionIDInterceptor produces it).
type protoMismatchReader struct{ err error }

func (r *protoMismatchReader) Read(_ []byte) (int, error) { return 0, r.err }

// capturingPublisher records the full payload of every emitted event so the test
// can inspect the agent_failed sub_reason and error_category fields (the shared
// watcherFixturePublisher discards payloads).
type capturingPublisher struct {
	types    []string
	payloads [][]byte
}

func (p *capturingPublisher) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	p.types = append(p.types, string(eventType))
	cp := make([]byte, len(payload))
	copy(cp, payload)
	p.payloads = append(p.payloads, cp)
	return nil
}

func (p *capturingPublisher) EmitWithRunID(ctx context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	return p.Emit(ctx, eventType, payload)
}

// TestWatcher_ProtocolMismatch_PreservesSentinel_HK9NGIV asserts that a
// version-negotiation failure surfacing through the scanner preserves the
// ErrProtocolMismatch sentinel in watcher.Err() (the load-bearing regression
// guard — false pre-fix) and emits agent_failed with sub_reason=protocol_mismatch
// and error_category=structural.
func TestWatcher_ProtocolMismatch_PreservesSentinel_HK9NGIV(t *testing.T) {
	t.Parallel()

	// The interceptor's sticky negoErr wraps ErrProtocolMismatch.
	negoErr := fmt.Errorf("handlercontract: no common version (handler supported_versions=[2]): %w", handlercontract.ErrProtocolMismatch)

	pub := &capturingPublisher{}
	w := handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:      watcherFixtureSessionID(t),
		ProgressStream: &protoMismatchReader{err: negoErr},
		Publisher:      pub,
		DeadLetter:     &watcherFixtureDeadLetter{},
	})
	watcherFixtureWait(t, w)

	// Primary regression guard: the sentinel must survive the error chain so the
	// retry policy can route protocol mismatch away from retry-spin.
	if w.Err() == nil {
		t.Fatal("Err() = nil, want non-nil wrapping ErrProtocolMismatch on version-negotiation failure")
	}
	if !errors.Is(w.Err(), handlercontract.ErrProtocolMismatch) {
		t.Fatalf("hk-9ngiv regression: Err() = %v, want errors.Is(...ErrProtocolMismatch)==true. "+
			"The sentinel was flattened out of the chain (%%v not %%w), so the orchestrator's retry "+
			"policy cannot distinguish a protocol mismatch from a transient framing error.", w.Err())
	}
	// ErrProtocolMismatch wraps ErrStructural, so the structural class is retained.
	if !errors.Is(w.Err(), handlercontract.ErrStructural) {
		t.Errorf("Err() = %v, want also wrapping ErrStructural (ErrProtocolMismatch is a structural sub-sentinel)", w.Err())
	}

	// The published agent_failed must carry sub_reason=protocol_mismatch and
	// error_category=structural per spec §8.7.
	var failed map[string]string
	for i, tp := range pub.types {
		if tp == handlercontract.ProgressMsgTypeAgentFailed {
			if err := json.Unmarshal(pub.payloads[i], &failed); err != nil {
				t.Fatalf("unmarshal agent_failed payload: %v", err)
			}
			break
		}
	}
	if failed == nil {
		t.Fatalf("agent_failed not published on version-negotiation failure; published types = %v", pub.types)
	}
	if got := failed["sub_reason"]; got != handlercontract.ProtocolMismatchSubReason {
		t.Errorf("agent_failed sub_reason = %q, want %q", got, handlercontract.ProtocolMismatchSubReason)
	}
	if got := failed["error_category"]; got != "structural" {
		t.Errorf("agent_failed error_category = %q, want \"structural\"", got)
	}
}

// TestWatcher_ProtocolMismatchSubReasonValue pins the literal sub_reason string
// to the spec-normative value (§8.7).
func TestWatcher_ProtocolMismatchSubReasonValue(t *testing.T) {
	t.Parallel()

	const want = "protocol_mismatch"
	if handlercontract.ProtocolMismatchSubReason != want {
		t.Errorf("ProtocolMismatchSubReason = %q, want %q", handlercontract.ProtocolMismatchSubReason, want)
	}
}
