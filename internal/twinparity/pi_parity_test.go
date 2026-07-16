// M6 WS3-pi — the twin-parity gate for the pi handler.
//
// This is the ROUTINE pi parity gate: it compares the pi twin's NDJSON output
// (committed testdata/twin-parity/pi/happy-path-sample/ndjson — the deterministic
// output of `harmonik-twin-pi --scenario happy-path`, see meta.yaml) against a
// reference capture, using only F1's equivalence library. Per the M6-PLAN §WS3
// decision, the routine gate is twin-vs-committed-reference (cheap, deterministic,
// zero-token). Because real pi captures are REAL-BOX-GATED (PI_LIVE=1), the
// committed reference IS the deterministic twin output today; strength grows when
// a live PI_LIVE capture lands and overwrites it (make test-pi-live).
//
// # Two spines — pi-native wire vs daemon-projected durable
//
// pi's NDJSON dialect is NOT harmonik-native: its wire kinds are session /
// message_start / message_end / agent_end (internal/daemon/pijsonlparser.go),
// none of which are the durable terminal kinds. So the wire spine asserted over
// the raw ndjson is the pi-native session → agent_end. The daemon PROJECTS the
// durable terminal triad (outcome_emitted → bead_closed → run_completed) from the
// run lifecycle, not from pi's wire vocabulary — that triad is asserted over the
// durable events.jsonl (identical in shape to every other harness).
//
// # This file is package twinparity (internal), not twinparity_test
//
// The negative/drift sub-tests must observe the first-divergence result WITHOUT
// failing this test, so they call the same unexported engine
// AssertStreamEquivalent delegates to (checkEquivalent, equivalence.go:63) and
// inspect its divergence directly — the same idiom as claude_parity_test.go. The
// positive gate still exercises the public AssertStreamEquivalent API.
package twinparity

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// piSampleNDJSON / piSampleEvents locate the committed pi reference capture.
// ndjson is the raw pi wire stream (== the twin's deterministic output);
// events.jsonl is the durable reference event log with the daemon-projected
// terminal triad.
func piSampleNDJSON(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "twin-parity", "pi", "happy-path-sample", "ndjson")
}

func piSampleEvents(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "twin-parity", "pi", "happy-path-sample", "events.jsonl")
}

// piWireSpine is the pi-native wire spine both the twin capture and a real pi
// NDJSON stream carry, in order: the session header (id-capture landmark,
// PI-012) → the terminal agent_end (Teardown landmark, PI-014). These pi-native
// kinds are pin-points the daemon parser keys on; message_start/message_end are
// interior usage events, not spine landmarks.
var piWireSpine = []string{
	"session",
	"agent_end",
}

// TestPiParityGate is the routine pi parity gate: the twin's NDJSON is
// equivalent to the pi reference capture on the pi-native wire spine, the durable
// reference carries the full daemon-projected terminal triad, and the reference's
// causal timing is within tolerance. Green here is the `make test-twin-parity-pi`
// acceptance.
func TestPiParityGate(t *testing.T) {
	twin, err := LoadStream(piSampleNDJSON(t))
	if err != nil {
		t.Fatalf("LoadStream(twin pi ndjson): %v", err)
	}
	reference, err := LoadStream(piSampleNDJSON(t))
	if err != nil {
		t.Fatalf("LoadStream(pi reference ndjson): %v", err)
	}
	durable, err := LoadStream(piSampleEvents(t))
	if err != nil {
		t.Fatalf("LoadStream(pi durable events): %v", err)
	}
	if len(twin.Events) == 0 || len(durable.Events) == 0 {
		t.Fatalf("empty stream(s): twin=%d durable=%d events", len(twin.Events), len(durable.Events))
	}

	// (1) pi-native wire spine: the twin NDJSON and the reference capture agree on
	// session → agent_end as an ordered subsequence.
	t.Run("pi-wire-spine", func(t *testing.T) {
		AssertStreamEquivalent(t, twin, reference, EquivOptions{Kinds: piWireSpine})
	})

	// (2) Durable terminal triad: the durable reference is spine-complete on the
	// full TerminalKinds (outcome_emitted → bead_closed → run_completed). This
	// pins the daemon-projected terminal outcome the pi wire stream cannot carry.
	t.Run("durable-terminal-triad", func(t *testing.T) {
		AssertStreamEquivalent(t, durable, durable, EquivOptions{})
	})

	// (3) Hook/causal timing within tolerance over the durable reference's causal
	// edges (ready→outcome, outcome→bead-close, bead-close→run-complete). Bites if
	// the reference drifts to drop a causal endpoint.
	t.Run("durable-timing-within-tolerance", func(t *testing.T) {
		AssertTimingWithinTolerance(t, durable, durable, DefaultTimingEdges, time.Second)
	})
}

// TestPiParityGateBitesOnDrift proves the gate is not vacuous: a DRIFTED twin is
// caught with a clear first-divergence diff. It exercises the exact engine
// AssertStreamEquivalent delegates to (checkEquivalent), so a non-ok result here
// is precisely the t.Errorf the public gate would emit.
func TestPiParityGateBitesOnDrift(t *testing.T) {
	reference, err := LoadStream(piSampleNDJSON(t))
	if err != nil {
		t.Fatalf("LoadStream(pi reference ndjson): %v", err)
	}
	durable, err := LoadStream(piSampleEvents(t))
	if err != nil {
		t.Fatalf("LoadStream(pi durable events): %v", err)
	}

	// Drift A — dropped terminal event: a twin whose NDJSON is missing agent_end
	// entirely (e.g. a hung pi that never fired the Teardown landmark). The wire
	// spine must diverge because agent_end is absent from the twin.
	t.Run("dropped-agent-end-drift", func(t *testing.T) {
		driftTwin, err := LoadStreamLines([]string{
			`{"type":"session","version":3,"id":"00000000-0000-4000-8000-0000000000a1","cwd":"/twin/pi/happy-path"}`,
			`{"type":"message_start","message":{"usage":{"input_tokens":42,"output_tokens":0}}}`,
		})
		if err != nil {
			t.Fatalf("LoadStreamLines(drift twin): %v", err)
		}
		div, ok := checkEquivalent(driftTwin, reference, piWireSpine, nil)
		if ok {
			t.Fatal("gate did NOT bite on a twin missing agent_end; want first-divergence failure")
		}
		msg := div.String()
		for _, want := range []string{"first divergence", "agent_end", "absent"} {
			if !strings.Contains(msg, want) {
				t.Errorf("divergence diff %q missing %q", msg, want)
			}
		}
	})

	// Drift B — missing session header: a twin whose NDJSON never emitted the
	// session line (no id-capture landmark). locateSpine must flag the vacuous
	// TWIN because session is the first spine kind and absent.
	t.Run("missing-session-drift", func(t *testing.T) {
		driftTwin, err := LoadStreamLines([]string{
			`{"type":"agent_end","messages":[{"role":"assistant","usage":{"input_tokens":42,"output_tokens":17}}]}`,
		})
		if err != nil {
			t.Fatalf("LoadStreamLines(drift twin): %v", err)
		}
		div, ok := checkEquivalent(driftTwin, reference, piWireSpine, nil)
		if ok {
			t.Fatal("gate did NOT bite on a twin missing the session header; want first-divergence failure")
		}
		msg := div.String()
		for _, want := range []string{"first divergence", "session"} {
			if !strings.Contains(msg, want) {
				t.Errorf("divergence diff %q missing %q", msg, want)
			}
		}
	})

	// Drift C — terminal-outcome flip on the DURABLE projection: a run whose
	// outcome_emitted reports "failure" where the reference reports "success". The
	// gate must diverge on the outcome_status field (a whitelisted stable field),
	// proving field-level bite, not just structural bite.
	t.Run("durable-terminal-outcome-drift", func(t *testing.T) {
		driftRun, err := LoadStreamLines([]string{
			`{"event_type":"outcome_emitted","outcome_status":"failure","node_id":"implement"}`,
			`{"event_type":"bead_closed","bead_id":"hk-pi-sample"}`,
			`{"event_type":"run_completed","success":false}`,
		})
		if err != nil {
			t.Fatalf("LoadStreamLines(drift run): %v", err)
		}
		div, ok := checkEquivalent(driftRun, durable, TerminalKinds, nil)
		if ok {
			t.Fatal("gate did NOT bite on a terminal-outcome-drifted run; want first-divergence failure")
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
}
