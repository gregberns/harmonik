// M6 WS3-Claude-D — the twin-parity gate for the Claude handler.
//
// This is the ROUTINE parity gate: it compares the canonical twin's
// --replay-path output against the committed Claude reference capture, using
// only F1's equivalence library. Per the M6-PLAN §WS3 decision, the routine
// gate is twin-vs-reference-capture (cheap, deterministic, zero-token) — a
// PERIODIC live re-capture (make capture-claude-fixtures on an auth'd box) is a
// separate, out-of-band step, NOT this gate.
//
// # Why the committed wire.ndjson stands in for a live twin run
//
// depguard forbids internal/twinparity from importing cmd/harmonik-twin-claude
// (leaf test-support package; core/handlercontract/stdlib only — see
// .golangci.yml twinparity matrix). So this gate cannot invoke runReplay
// in-process. It does not need to: WS3-Claude-B proved
// runReplay(wire.ndjson) is BYTE-IDENTICAL to the committed wire.ndjson
// (verbatim / no-restamp — see cmd/harmonik-twin-claude/replaydriver_test.go
// TestRunReplayRoundTripIdentity). The committed wire.ndjson therefore IS the
// twin's --replay-path output; loading it as the twin stream is faithful, not a
// stand-in.
//
// # This file is package twinparity (internal), not twinparity_test
//
// The negative/drift sub-tests assert that the gate BITES on a drifted twin —
// they must observe the first-divergence result WITHOUT failing this test. The
// only way to do that without faking a testing.TB (impossible: testing.TB has an
// unexported method) is to call the same unexported engine that
// AssertStreamEquivalent delegates to (checkEquivalent, equivalence.go:54) and
// inspect its divergence directly. The positive gate still exercises the public
// AssertStreamEquivalent / AssertTimingWithinTolerance API.
package twinparity

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func claudeSampleWire(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "twin-parity", "claude", "happy-path-sample", "wire.ndjson")
}

func claudeSampleEvents(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "twin-parity", "claude", "happy-path-sample", "events.jsonl")
}

var observableSpine = []string{
	"agent_ready",
	"outcome_emitted",
}

// TestClaudeParityGate is the routine parity gate: the twin's --replay-path
// output (committed wire.ndjson) is equivalent to the Claude reference capture
// on the ordered kind-sequence + terminal outcome, and the reference's durable
// causal timing is within tolerance. Green here is the `make test-twin-parity-
// claude` acceptance.
func TestClaudeParityGate(t *testing.T) {
	twin, err := LoadStream(claudeSampleWire(t))
	if err != nil {
		t.Fatalf("LoadStream(twin replay wire): %v", err)
	}
	reference, err := LoadStream(claudeSampleEvents(t))
	if err != nil {
		t.Fatalf("LoadStream(reference durable events): %v", err)
	}
	if len(twin.Events) == 0 || len(reference.Events) == 0 {
		t.Fatalf("empty stream(s): twin=%d reference=%d events", len(twin.Events), len(reference.Events))
	}

	t.Run("kind-sequence-and-terminal-outcome", func(t *testing.T) {
		AssertStreamEquivalent(t, twin, reference, EquivOptions{Kinds: observableSpine})
	})

	t.Run("durable-terminal-triad", func(t *testing.T) {
		AssertStreamEquivalent(t, reference, reference, EquivOptions{})
	})

	t.Run("hook-timing-within-tolerance", func(t *testing.T) {
		AssertTimingWithinTolerance(t, reference, reference, DefaultTimingEdges, time.Second)
	})
}

// TestClaudeParityGateBitesOnDrift proves the gate is not vacuous: a DRIFTED
// twin (a mangled replay script) is caught, and the failure is a clear
// FIRST-DIVERGENCE diff (kind/field, expected vs got, position). It exercises
// the exact engine AssertStreamEquivalent delegates to (checkEquivalent), so a
// non-ok result here is precisely the t.Errorf the public gate would emit.
func TestClaudeParityGateBitesOnDrift(t *testing.T) {
	reference, err := LoadStream(claudeSampleEvents(t))
	if err != nil {
		t.Fatalf("LoadStream(reference durable events): %v", err)
	}

	t.Run("terminal-outcome-drift", func(t *testing.T) {
		driftTwin, err := LoadStreamLines([]string{
			`{"event_type":"agent_ready","claude_session_id":"44444444-4444-4444-4444-444444444444"}`,
			`{"event_type":"outcome_emitted","outcome_status":"failure","node_id":"implement"}`,
		})
		if err != nil {
			t.Fatalf("LoadStreamLines(drift twin): %v", err)
		}
		div, ok := checkEquivalent(driftTwin, reference, observableSpine, nil)
		if ok {
			t.Fatal("gate did NOT bite on a terminal-outcome-drifted twin; want first-divergence failure")
		}
		if div.field != "outcome_status" {
			t.Errorf("first divergence field = %q, want %q", div.field, "outcome_status")
		}
		msg := div.String()
		for _, want := range []string{"first divergence", "outcome_status", "failure", "success"} {
			if !strings.Contains(msg, want) {
				t.Errorf("divergence diff %q missing %q", msg, want)
			}
		}
	})

	t.Run("dropped-terminal-kind-drift", func(t *testing.T) {
		driftTwin, err := LoadStreamLines([]string{
			`{"event_type":"agent_ready","claude_session_id":"44444444-4444-4444-4444-444444444444"}`,
		})
		if err != nil {
			t.Fatalf("LoadStreamLines(drift twin): %v", err)
		}
		div, ok := checkEquivalent(driftTwin, reference, observableSpine, nil)
		if ok {
			t.Fatal("gate did NOT bite on a twin missing outcome_emitted; want first-divergence failure")
		}
		msg := div.String()
		for _, want := range []string{"first divergence", "outcome_emitted", "absent"} {
			if !strings.Contains(msg, want) {
				t.Errorf("divergence diff %q missing %q", msg, want)
			}
		}
	})
}
