// Package core — EM-042a requirement-traceable sensors.
//
// This file covers the gate-pending sub-state defined in
// execution-model.md §4.10.EM-042a:
//
//   - When [DispatchEdge] returns Stay=true (gate denial), the daemon MUST enter
//     gate-pending sub-state: no re-dispatch of the source node, no re-run of
//     the cascade against the same context and outcome.
//   - The daemon MUST wait for a [GateResolutionSignal] before re-evaluating.
//   - Three resolution signals are declared: context-change, operator-override,
//     timeout. On timeout with continued denial, the run fails with class
//     `structural` per §8.2.
//
// Test naming pattern:
//
//	TestGatePendingEM042a_<Case>
//	TestGateResolutionSignalEM042a_<Case>
//
// Run all sensors for EM-042a with:
//
//	go test -run TestGatePendingEM042a ./internal/core/...
//	go test -run TestGateResolutionSignalEM042a ./internal/core/...
package core

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// ── fixtures ────────────────────────────────────────────────────────────────

// gateDenyContFixtureRunID returns a fresh non-nil RunID backed by a UUIDv7.
func gateDenyContFixtureRunID(t *testing.T) RunID {
	t.Helper()
	return RunID(uuid.Must(uuid.NewV7()))
}

// gateDenyContFixtureEdge returns a minimal valid Edge usable as a DeniedEdge.
func gateDenyContFixtureEdge(t *testing.T) Edge {
	t.Helper()
	return Edge{
		FromNode:    "node-src",
		ToNode:      "node-dst",
		Weight:      1,
		OrderingKey: "a",
	}
}

// gateDenyContFixtureRecord returns a fully-populated, valid GatePendingRecord.
func gateDenyContFixtureRecord(t *testing.T) GatePendingRecord {
	t.Helper()
	return GatePendingRecord{
		RunID:       gateDenyContFixtureRunID(t),
		GateName:    "quality-check",
		SourceNode:  "node-src",
		DeniedEdge:  gateDenyContFixtureEdge(t),
		ContextHash: "a3f1e2d4b5c6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2",
		OutcomeHash: "b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5",
		EnteredAt:   time.Now().UTC().Format(time.RFC3339),
	}
}

// ── GatePendingRecord validity sensors ──────────────────────────────────────

// TestGatePendingEM042a_ValidRecordAccepted verifies that a fully-populated
// GatePendingRecord reports Valid() == true.
func TestGatePendingEM042a_ValidRecordAccepted(t *testing.T) {
	t.Parallel()

	r := gateDenyContFixtureRecord(t)
	if !r.Valid() {
		t.Error("EM-042a: fully-populated GatePendingRecord.Valid() returned false, want true")
	}
}

// TestGatePendingEM042a_ZeroRunIDRejected verifies that a GatePendingRecord
// with a zero RunID is invalid (no run can be gate-pending without identity).
func TestGatePendingEM042a_ZeroRunIDRejected(t *testing.T) {
	t.Parallel()

	r := gateDenyContFixtureRecord(t)
	r.RunID = RunID(uuid.Nil)
	if r.Valid() {
		t.Error("EM-042a: GatePendingRecord with zero RunID reported Valid()=true, want false")
	}
}

// TestGatePendingEM042a_EmptyGateNameRejected verifies that a GatePendingRecord
// with an empty GateName is invalid (cannot re-evaluate without knowing which gate).
func TestGatePendingEM042a_EmptyGateNameRejected(t *testing.T) {
	t.Parallel()

	r := gateDenyContFixtureRecord(t)
	r.GateName = ""
	if r.Valid() {
		t.Error("EM-042a: GatePendingRecord with empty GateName reported Valid()=true, want false")
	}
}

// TestGatePendingEM042a_EmptySourceNodeRejected verifies that a GatePendingRecord
// with an empty SourceNode is invalid (daemon cannot enforce no-redispatch constraint).
func TestGatePendingEM042a_EmptySourceNodeRejected(t *testing.T) {
	t.Parallel()

	r := gateDenyContFixtureRecord(t)
	r.SourceNode = ""
	if r.Valid() {
		t.Error("EM-042a: GatePendingRecord with empty SourceNode reported Valid()=true, want false")
	}
}

// TestGatePendingEM042a_InvalidDeniedEdgeRejected verifies that a GatePendingRecord
// with an invalid DeniedEdge (missing OrderingKey) is rejected.
func TestGatePendingEM042a_InvalidDeniedEdgeRejected(t *testing.T) {
	t.Parallel()

	r := gateDenyContFixtureRecord(t)
	r.DeniedEdge = Edge{FromNode: "node-src", ToNode: "node-dst"} // missing OrderingKey
	if r.Valid() {
		t.Error("EM-042a: GatePendingRecord with invalid DeniedEdge reported Valid()=true, want false")
	}
}

// TestGatePendingEM042a_EmptyContextHashRejected verifies that a GatePendingRecord
// with an empty ContextHash is invalid (cannot detect context changes without baseline).
func TestGatePendingEM042a_EmptyContextHashRejected(t *testing.T) {
	t.Parallel()

	r := gateDenyContFixtureRecord(t)
	r.ContextHash = ""
	if r.Valid() {
		t.Error("EM-042a: GatePendingRecord with empty ContextHash reported Valid()=true, want false")
	}
}

// TestGatePendingEM042a_EmptyOutcomeHashRejected verifies that a GatePendingRecord
// with an empty OutcomeHash is invalid (cannot guard against same-context+outcome loop).
func TestGatePendingEM042a_EmptyOutcomeHashRejected(t *testing.T) {
	t.Parallel()

	r := gateDenyContFixtureRecord(t)
	r.OutcomeHash = ""
	if r.Valid() {
		t.Error("EM-042a: GatePendingRecord with empty OutcomeHash reported Valid()=true, want false")
	}
}

// TestGatePendingEM042a_EmptyEnteredAtRejected verifies that a GatePendingRecord
// with an empty EnteredAt timestamp is invalid (cannot compute timeout elapsed time).
func TestGatePendingEM042a_EmptyEnteredAtRejected(t *testing.T) {
	t.Parallel()

	r := gateDenyContFixtureRecord(t)
	r.EnteredAt = ""
	if r.Valid() {
		t.Error("EM-042a: GatePendingRecord with empty EnteredAt reported Valid()=true, want false")
	}
}

// ── GateResolutionSignal sensors ─────────────────────────────────────────────

// TestGateResolutionSignalEM042a_ThreeValuesValid verifies that all three
// declared GateResolutionSignal constants report Valid() == true.
func TestGateResolutionSignalEM042a_ThreeValuesValid(t *testing.T) {
	t.Parallel()

	signals := []GateResolutionSignal{
		GateResolutionSignalContextChange,
		GateResolutionSignalOperatorOverride,
		GateResolutionSignalTimeout,
	}
	for _, s := range signals {
		if !s.Valid() {
			t.Errorf("EM-042a: GateResolutionSignal(%q).Valid() returned false, want true", s)
		}
	}
}

// TestGateResolutionSignalEM042a_UnknownValueRejected verifies that an unknown
// GateResolutionSignal value reports Valid() == false.
func TestGateResolutionSignalEM042a_UnknownValueRejected(t *testing.T) {
	t.Parallel()

	unknown := GateResolutionSignal("auto-resolve")
	if unknown.Valid() {
		t.Error("EM-042a: unknown GateResolutionSignal reported Valid()=true, want false")
	}
}

// TestGateResolutionSignalEM042a_MarshalTextAcceptsKnownValues verifies that
// MarshalText succeeds for all three declared constants.
func TestGateResolutionSignalEM042a_MarshalTextAcceptsKnownValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		signal GateResolutionSignal
		want   string
	}{
		{GateResolutionSignalContextChange, "context-change"},
		{GateResolutionSignalOperatorOverride, "operator-override"},
		{GateResolutionSignalTimeout, "timeout"},
	}
	for _, tc := range cases {
		got, err := tc.signal.MarshalText()
		if err != nil {
			t.Errorf("EM-042a: MarshalText(%q) returned error: %v", tc.signal, err)
			continue
		}
		if string(got) != tc.want {
			t.Errorf("EM-042a: MarshalText(%q) = %q, want %q", tc.signal, string(got), tc.want)
		}
	}
}

// TestGateResolutionSignalEM042a_MarshalTextRejectsUnknown verifies that
// MarshalText returns an error for unknown GateResolutionSignal values.
func TestGateResolutionSignalEM042a_MarshalTextRejectsUnknown(t *testing.T) {
	t.Parallel()

	unknown := GateResolutionSignal("auto-resolve")
	_, err := unknown.MarshalText()
	if err == nil {
		t.Error("EM-042a: MarshalText for unknown GateResolutionSignal returned nil error, want error")
	}
}

// TestGateResolutionSignalEM042a_UnmarshalTextAcceptsKnownValues verifies that
// UnmarshalText round-trips all three declared constants.
func TestGateResolutionSignalEM042a_UnmarshalTextAcceptsKnownValues(t *testing.T) {
	t.Parallel()

	cases := []string{"context-change", "operator-override", "timeout"}
	expected := []GateResolutionSignal{
		GateResolutionSignalContextChange,
		GateResolutionSignalOperatorOverride,
		GateResolutionSignalTimeout,
	}
	for i, raw := range cases {
		var got GateResolutionSignal
		if err := got.UnmarshalText([]byte(raw)); err != nil {
			t.Errorf("EM-042a: UnmarshalText(%q) returned error: %v", raw, err)
			continue
		}
		if got != expected[i] {
			t.Errorf("EM-042a: UnmarshalText(%q) = %v, want %v", raw, got, expected[i])
		}
	}
}

// TestGateResolutionSignalEM042a_UnmarshalTextRejectsUnknown verifies that
// UnmarshalText returns an error for unknown GateResolutionSignal values.
func TestGateResolutionSignalEM042a_UnmarshalTextRejectsUnknown(t *testing.T) {
	t.Parallel()

	var s GateResolutionSignal
	if err := s.UnmarshalText([]byte("auto-resolve")); err == nil {
		t.Error("EM-042a: UnmarshalText for unknown value returned nil error, want error")
	}
}

// TestGateResolutionSignalEM042a_TimeoutSignalImpliesStructuralFailure documents
// the EM-042a semantic that [GateResolutionSignalTimeout] with continued gate
// denial MUST result in a `structural` failure class per execution-model.md §8.2.
// This test verifies that the timeout constant is distinct from the other two
// (enforcement is in the daemon dispatch loop, not in the record type itself).
func TestGateResolutionSignalEM042a_TimeoutSignalImpliesStructuralFailure(t *testing.T) {
	t.Parallel()

	// The timeout signal is semantically distinct: it is the only signal that,
	// when the gate still denies after re-evaluation, MUST escalate to a
	// structural failure per §8.2.
	if GateResolutionSignalTimeout == GateResolutionSignalContextChange {
		t.Error("EM-042a: timeout and context-change signals must be distinct constants")
	}
	if GateResolutionSignalTimeout == GateResolutionSignalOperatorOverride {
		t.Error("EM-042a: timeout and operator-override signals must be distinct constants")
	}
	// Verify the timeout signal is the normative string value declared by the spec.
	if string(GateResolutionSignalTimeout) != "timeout" {
		t.Errorf("EM-042a: GateResolutionSignalTimeout = %q, want %q",
			GateResolutionSignalTimeout, "timeout")
	}
}
