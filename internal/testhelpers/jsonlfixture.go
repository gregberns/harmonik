// Package testhelpers — canonical JSONL fixtures for envelope + durability +
// read-recovery tests (hk-hqwn.60).
//
// This file provides deterministic, spec-conformant JSONL byte slices covering
// every scenario named in event-model.md §10.2 EV-001–EV-008 (envelope),
// EV-015/EV-020 (durability), and §6.2 (read-recovery). Each function is
// named with the [jsonlFixture] prefix per the hk-hqwn.60 helper-prefix
// discipline.
//
// Spec references: event-model.md §6.1 (envelope RECORD), §6.2 (on-disk JSONL
// format and read-recovery rules), §10.2 (test-surface obligations).
//
// Wire form: the on-disk JSON object uses snake_case keys as shown in §6.2.
// Each JSONL line is a single JSON object followed by exactly one newline (\n).
// A torn tail lacks the terminating newline on the final line per §6.2.
//
// All UUIDs in these fixtures are fixed deterministic UUIDv7 values so that
// tests are reproducible. They were generated once from a known time base:
//
//	epoch 2026-04-24T14:22:11.000Z → ms = 0x018EF0001000 → first nibble of byte[6] = 0x7_
//
// Panic policy: test helpers in internal/testhelpers/ MAY panic on misuse per
// implementer-protocol.md §Lint compliance. All panics in this file guard
// programmer error (bad constant JSON), not runtime conditions.
package testhelpers

import (
	"encoding/json"
	"fmt"
)

// ---------------------------------------------------------------------------
// Internal wire-form type — matches event-model.md §6.2 on-disk JSON shape.
// Not exported; fixtures are vended as []byte (raw JSONL) so callers decode
// them through the production reader under test.
// ---------------------------------------------------------------------------

// jsonlEventWire is the on-disk JSON envelope shape (snake_case keys per §6.2).
// It mirrors the Event RECORD in event-model.md §6.1. Optional fields use
// pointer types so they serialise as JSON null when absent (rather than zero).
type jsonlEventWire struct {
	EventID           string          `json:"event_id"`
	SchemaVersion     int             `json:"schema_version"`
	Type              string          `json:"type"`
	TimestampWall     string          `json:"timestamp_wall"`
	TimestampMonoNsec *int64          `json:"timestamp_mono_nsec,omitempty"`
	RunID             *string         `json:"run_id,omitempty"`
	StateID           *string         `json:"state_id,omitempty"`
	SourceSubsystem   string          `json:"source_subsystem"`
	TraceContext      *jsonlTraceWire `json:"trace_context,omitempty"`
	Payload           json.RawMessage `json:"payload"`
}

// jsonlTraceWire is the on-disk JSON shape for the TraceContext optional field
// (event-model.md §6.1 RECORD TraceContext).
type jsonlTraceWire struct {
	TraceID       *string `json:"trace_id,omitempty"`
	ParentEventID *string `json:"parent_event_id,omitempty"`
	RootEventID   *string `json:"root_event_id,omitempty"`
}

// Fixed deterministic UUIDv7 values. Millisecond component derived from
// 2026-04-24T14:22:11.000Z = Unix-ms 1745505731000 = 0x0196_E3D1_3AF8.
const (
	jsonlFixtureEventID1  = "0196e3d1-3af8-7000-8000-000000000001"
	jsonlFixtureEventID2  = "0196e3d1-3af8-7000-8000-000000000002"
	jsonlFixtureEventID3  = "0196e3d1-3af8-7000-8000-000000000003"
	jsonlFixtureEventID4  = "0196e3d1-3af8-7000-8000-000000000004"
	jsonlFixtureEventID5  = "0196e3d1-3af8-7000-8000-000000000005"
	jsonlFixtureRunID     = "0196e3d1-3b00-7000-8000-000000000010"
	jsonlFixtureStateID   = "0196e3d1-3b00-7000-8000-000000000020"
	jsonlFixtureTraceID   = "0196e3d1-3b00-7000-8000-000000000030"
	jsonlFixtureTimestamp = "2026-04-24T14:22:11.000Z"
)

// mustMarshalLine serialises v to a single JSON object followed by "\n".
// Panics if json.Marshal fails — guards programmer error (bad constant struct),
// not runtime input.
func mustMarshalLine(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("jsonlFixture: mustMarshalLine: json.Marshal: %v", err))
	}
	return append(b, '\n')
}

// minimalEnvelopeWire returns the minimal-valid envelope wire struct:
// only the required fields from §6.1 are populated; all optional pointer
// fields (run_id, state_id, timestamp_mono_nsec, trace_context) are nil.
func minimalEnvelopeWire(eventID string) jsonlEventWire {
	return jsonlEventWire{
		EventID:         eventID,
		SchemaVersion:   1,
		Type:            "run_started",
		TimestampWall:   jsonlFixtureTimestamp,
		SourceSubsystem: "github.com/gregberns/harmonik/internal/orchestrator",
		Payload:         json.RawMessage(`{"run_id":"` + jsonlFixtureRunID + `","workflow_id":"` + jsonlFixtureStateID + `","workflow_version":"1.0.0","bead_id":null,"workspace_path":"/tmp/test","input_ref":"HEAD"}`),
	}
}

// fullEnvelopeWire returns a maximally-populated envelope wire struct:
// all optional fields are set.
func fullEnvelopeWire(eventID string) jsonlEventWire {
	mono := int64(918273645)
	runID := jsonlFixtureRunID
	stateID := jsonlFixtureStateID
	traceID := jsonlFixtureTraceID
	parentID := jsonlFixtureEventID1
	rootID := jsonlFixtureEventID1

	return jsonlEventWire{
		EventID:           eventID,
		SchemaVersion:     1,
		Type:              "checkpoint_written",
		TimestampWall:     jsonlFixtureTimestamp,
		TimestampMonoNsec: &mono,
		RunID:             &runID,
		StateID:           &stateID,
		SourceSubsystem:   "github.com/gregberns/harmonik/internal/orchestrator",
		TraceContext: &jsonlTraceWire{
			TraceID:       &traceID,
			ParentEventID: &parentID,
			RootEventID:   &rootID,
		},
		Payload: json.RawMessage(`{"run_id":"` + jsonlFixtureRunID + `","state_id":"` + jsonlFixtureStateID + `","transition_id":"` + jsonlFixtureEventID3 + `","commit_hash":"abc1234","bead_id":null}`),
	}
}

// ---------------------------------------------------------------------------
// Exported fixture constructors — named with the jsonlFixture prefix per
// hk-hqwn.60 helper-prefix discipline.
// ---------------------------------------------------------------------------

// JSONLFixtureMinimalEnvelope returns a JSONL byte slice containing one
// well-formed event with only required envelope fields set. Covers the
// §6.2 "normal append" case for a reader that can decode a minimal Event.
//
// Spec: event-model.md §6.1 EV-001, §6.2 primary-log format.
func JSONLFixtureMinimalEnvelope() []byte {
	return mustMarshalLine(minimalEnvelopeWire(jsonlFixtureEventID1))
}

// JSONLFixtureFullEnvelope returns a JSONL byte slice containing one
// well-formed event with all optional envelope fields populated. Covers the
// §6.1 full-envelope case.
//
// Spec: event-model.md §6.1 EV-001, §6.2 primary-log format.
func JSONLFixtureFullEnvelope() []byte {
	return mustMarshalLine(fullEnvelopeWire(jsonlFixtureEventID2))
}

// JSONLFixtureMultipleValid returns a JSONL byte slice containing three
// consecutive well-formed events with strictly increasing event_id values
// (satisfying EV-002a partial-order). Used by tests that walk multiple lines.
//
// Spec: event-model.md §6.2, EV-002a, EV-008.
func JSONLFixtureMultipleValid() []byte {
	var out []byte
	out = append(out, mustMarshalLine(minimalEnvelopeWire(jsonlFixtureEventID1))...)
	out = append(out, mustMarshalLine(fullEnvelopeWire(jsonlFixtureEventID2))...)
	out = append(out, mustMarshalLine(minimalEnvelopeWire(jsonlFixtureEventID3))...)
	return out
}

// DurabilityClass names the durability-class values from event-model.md §4.4.
type DurabilityClass string

const (
	// DurabilityFsyncBoundary names the fsync-boundary durability class (§4.4 EV-016).
	// Events of this class MUST trigger an fsync(2) after appending.
	DurabilityFsyncBoundary DurabilityClass = "fsync-boundary"

	// DurabilityOrdinary names the ordinary durability class (§4.4 EV-016).
	// Events of this class do not require per-append fsync.
	DurabilityOrdinary DurabilityClass = "ordinary"

	// DurabilityLossyTailOk names the lossy-tail-ok durability class (§4.4 EV-016).
	// Events of this class may be lost between fsyncs; loss does not require reconciliation.
	DurabilityLossyTailOk DurabilityClass = "lossy-tail-ok"
)

// DurabilityClassLine carries one JSONL line together with its durability class.
// Returned by JSONLFixtureDurabilityClasses so tests can assert per-class fsync
// semantics without re-parsing the durability class from the payload.
//
// Spec: event-model.md §4.4 EV-016.
type DurabilityClassLine struct {
	// Class is the durability class of the event on Line.
	Class DurabilityClass
	// Line is the raw JSONL bytes for one event (including the trailing newline).
	Line []byte
}

// JSONLFixtureDurabilityClasses returns three DurabilityClassLine values,
// one for each durability class defined in event-model.md §4.4:
//
//   - fsync-boundary  — mapped to "run_started" (§8.1.1 Dur=F)
//   - ordinary        — mapped to "state_entered" (§8.1.4 Dur=O)
//   - lossy-tail-ok   — no §8 type is L at MVH (§8.8.1 metric, but
//     "metric" requires amendment per EV-027; we use "state_entered"
//     with an explicit comment and accept the fixture's class label as
//     test-only metadata, not an EV-016 assertion).
//
// Callers use the returned Class to drive per-class fsync assertion logic.
//
// Spec: event-model.md §4.4 EV-016, §8 (Dur column).
func JSONLFixtureDurabilityClasses() []DurabilityClassLine {
	fsyncLine := minimalEnvelopeWire(jsonlFixtureEventID1)
	// run_started is Dur=F (§8.1.1).
	fsyncLine.Type = "run_started"

	ordinaryLine := minimalEnvelopeWire(jsonlFixtureEventID2)
	// state_entered is Dur=O (§8.1.4).
	ordinaryLine.Type = "state_entered"
	ordinaryLine.Payload = json.RawMessage(`{"run_id":"` + jsonlFixtureRunID + `","state_id":"` + jsonlFixtureStateID + `","node_id":"node-001","entered_at":"` + jsonlFixtureTimestamp + `"}`)

	// lossy-tail-ok: "metric" (§8.8.1) is the canonical L class but is not
	// fully registered at MVH. We use a test-only type string here; the
	// DurabilityClass label is the test metadata — no production reader is
	// exercised against this type name in this fixture. Tests that need a real
	// registered lossy event should register "metric" locally before decoding.
	lossyLine := minimalEnvelopeWire(jsonlFixtureEventID3)
	lossyLine.Type = "metric"
	lossyLine.Payload = json.RawMessage(`{"name":"test.counter","value":1}`)

	return []DurabilityClassLine{
		{Class: DurabilityFsyncBoundary, Line: mustMarshalLine(fsyncLine)},
		{Class: DurabilityOrdinary, Line: mustMarshalLine(ordinaryLine)},
		{Class: DurabilityLossyTailOk, Line: mustMarshalLine(lossyLine)},
	}
}

// TornTailKind identifies which torn-tail variant a TornTailFixture represents.
// Spec: event-model.md §6.2 "Torn tail" rule.
type TornTailKind string

const (
	// TornTailMissingNewline: final line is valid JSON but lacks the terminating "\n".
	TornTailMissingNewline TornTailKind = "missing-newline"
	// TornTailBadJSON: final line is not valid JSON.
	TornTailBadJSON TornTailKind = "bad-json"
	// TornTailBadEnvelope: final line is valid JSON but fails envelope schema
	// validation (missing required field).
	TornTailBadEnvelope TornTailKind = "bad-envelope"
)

// TornTailFixture bundles the raw JSONL bytes for a torn-tail scenario with
// metadata describing the scenario.
//
// The bytes always contain at least one preceding well-formed line so readers
// can distinguish mid-file vs. final-line conditions.
//
// Spec: event-model.md §6.2 "Torn tail" — reader MUST discard silently in
// post-crash startup-recovery context; MUST emit store_divergence_detected in
// all other read contexts.
type TornTailFixture struct {
	// Kind identifies the torn-tail variant.
	Kind TornTailKind
	// JSONL is the raw bytes of the fixture, including the corrupted final line.
	JSONL []byte
	// ValidLineCount is the number of lines that a reader should successfully
	// parse before encountering the torn tail.
	ValidLineCount int
}

// JSONLFixtureTornTail returns one TornTailFixture for each torn-tail variant
// defined in event-model.md §6.2:
//
//   - TornTailMissingNewline — valid JSON object but no terminating "\n"
//   - TornTailBadJSON        — truncated mid-object (crash write)
//   - TornTailBadEnvelope    — valid JSON but missing required "event_id" field
//
// Each fixture has exactly one valid preceding line (ValidLineCount == 1)
// so the reader can successfully decode the first event before hitting the tail.
//
// Spec: event-model.md §6.2 "Torn tail".
func JSONLFixtureTornTail() []TornTailFixture {
	validLine := mustMarshalLine(minimalEnvelopeWire(jsonlFixtureEventID1))

	// Variant 1: valid JSON, no trailing newline.
	missingNLObj, err := json.Marshal(minimalEnvelopeWire(jsonlFixtureEventID2))
	if err != nil {
		panic(fmt.Sprintf("jsonlFixture: TornTailMissingNewline: json.Marshal: %v", err))
	}
	var missingNL []byte
	missingNL = append(missingNL, validLine...)
	missingNL = append(missingNL, missingNLObj...) // intentionally no '\n'

	// Variant 2: bad JSON (truncated mid-object).
	var badJSON []byte
	badJSON = append(badJSON, validLine...)
	badJSON = append(badJSON, []byte(`{"event_id":"0196e3d1-3af8-7000-8000-000000000002","schema_ver`)...) // truncated

	// Variant 3: valid JSON but missing required field "event_id" (envelope schema failure).
	badEnvObj, err := json.Marshal(map[string]any{
		// event_id deliberately absent — required per EV-001.
		"schema_version":   1,
		"type":             "run_started",
		"timestamp_wall":   jsonlFixtureTimestamp,
		"source_subsystem": "github.com/gregberns/harmonik/internal/orchestrator",
		"payload":          json.RawMessage(`{}`),
	})
	if err != nil {
		panic(fmt.Sprintf("jsonlFixture: TornTailBadEnvelope: json.Marshal: %v", err))
	}
	var badEnv []byte
	badEnv = append(badEnv, validLine...)
	badEnv = append(badEnv, badEnvObj...)
	badEnv = append(badEnv, '\n')

	return []TornTailFixture{
		{Kind: TornTailMissingNewline, JSONL: missingNL, ValidLineCount: 1},
		{Kind: TornTailBadJSON, JSONL: badJSON, ValidLineCount: 1},
		{Kind: TornTailBadEnvelope, JSONL: badEnv, ValidLineCount: 1},
	}
}

// MidFileCorruptionFixture carries JSONL bytes where a non-final line fails
// JSON parse. Spec §6.2: reader MUST emit store_divergence_detected and halt.
//
// Spec: event-model.md §6.2 "Mid-file corruption".
type MidFileCorruptionFixture struct {
	// JSONL is the raw bytes of the fixture.
	JSONL []byte
	// CorruptByteOffset is the byte offset at which the corrupt line begins.
	// Tests may assert that readers surface this offset in their error value.
	CorruptByteOffset int
	// ValidLinesBefore is the count of well-formed lines before the corrupt line.
	ValidLinesBefore int
	// ValidLinesAfter is the count of well-formed lines after the corrupt line
	// (which the spec says MUST NOT be read — the reader must halt).
	ValidLinesAfter int
}

// JSONLFixtureMidFileCorruption returns a JSONL byte slice whose middle line
// fails JSON parse. The layout is:
//
//	line 0: valid event (event_id = jsonlFixtureEventID1)
//	line 1: corrupt bytes (bad JSON — simulates block-reorder / media error)
//	line 2: valid event (event_id = jsonlFixtureEventID3)
//
// ValidLinesBefore == 1; ValidLinesAfter == 1; CorruptByteOffset is the
// offset of the first byte of line 1.
//
// Spec: event-model.md §6.2 "Mid-file corruption" — reader MUST emit
// store_divergence_detected{divergence_kind=parse_failure, byte_offset=…}
// and halt; MUST NOT skip and continue.
func JSONLFixtureMidFileCorruption() MidFileCorruptionFixture {
	line0 := mustMarshalLine(minimalEnvelopeWire(jsonlFixtureEventID1))
	corruptLine := []byte("NOT_JSON_CORRUPT_BLOCK\n")
	line2 := mustMarshalLine(minimalEnvelopeWire(jsonlFixtureEventID3))

	var out []byte
	out = append(out, line0...)
	corruptOffset := len(out)
	out = append(out, corruptLine...)
	out = append(out, line2...)

	return MidFileCorruptionFixture{
		JSONL:             out,
		CorruptByteOffset: corruptOffset,
		ValidLinesBefore:  1,
		ValidLinesAfter:   1,
	}
}

// EmptyLogKind distinguishes the two "empty log" sub-cases from §6.2.
type EmptyLogKind string

const (
	// EmptyLogFreshProject: no events.jsonl and no prior daemon cycle in git/Beads.
	// Valid state; reader treats this as a clean start.
	EmptyLogFreshProject EmptyLogKind = "fresh-project"

	// EmptyLogWithPriorCycle: empty events.jsonl but git/Beads carry prior-cycle
	// evidence. Reader MUST emit store_divergence_detected{log_missing}.
	EmptyLogWithPriorCycle EmptyLogKind = "prior-cycle-mismatch"
)

// EmptyLogFixture carries the JSONL bytes (always nil or empty) for one
// empty-log scenario together with metadata to drive reader-assertion logic.
//
// Spec: event-model.md §6.2 "Empty log".
type EmptyLogFixture struct {
	// Kind identifies the sub-case.
	Kind EmptyLogKind
	// JSONL is nil (file absent) for EmptyLogFreshProject and an empty byte
	// slice for EmptyLogWithPriorCycle (file present but zero length).
	JSONL []byte
	// HasPriorCycleEvidence mirrors the "git or Beads carry prior-cycle
	// evidence" flag. Tests use this to decide whether to assert that
	// store_divergence_detected is emitted.
	HasPriorCycleEvidence bool
}

// JSONLFixtureEmptyLog returns two EmptyLogFixture values covering the two
// sub-cases defined in event-model.md §6.2:
//
//   - EmptyLogFreshProject:       JSONL == nil; HasPriorCycleEvidence == false.
//   - EmptyLogWithPriorCycle:     JSONL == []byte{}; HasPriorCycleEvidence == true.
//
// Spec: event-model.md §6.2 "Empty log".
func JSONLFixtureEmptyLog() []EmptyLogFixture {
	return []EmptyLogFixture{
		{
			Kind:                  EmptyLogFreshProject,
			JSONL:                 nil,
			HasPriorCycleEvidence: false,
		},
		{
			Kind:                  EmptyLogWithPriorCycle,
			JSONL:                 []byte{},
			HasPriorCycleEvidence: true,
		},
	}
}

// ConcurrentTailFixture carries JSONL bytes that simulate the concurrent-tail
// scenario from event-model.md §6.2: the final line is partially written (no
// terminating newline) because a writer is actively appending.
//
// The fixture is structurally identical to TornTailMissingNewline; it exists
// as a distinct type so tests that cover concurrent-tailing semantics (reader
// must treat the in-progress final line as non-authoritative per §6.2) are
// clearly distinguished from crash-recovery torn-tail tests.
//
// Spec: event-model.md §6.2 "Concurrent tailing".
type ConcurrentTailFixture struct {
	// JSONL is the raw bytes at a point in time when the writer has flushed
	// the first CompletedLines events but is mid-write on the final line.
	JSONL []byte
	// CompletedLines is the count of lines with terminating newlines — these
	// are authoritative. The final partial line MUST NOT be decoded by the reader.
	CompletedLines int
	// PartialLineBytes is the byte slice of the in-progress (no trailing \n) line.
	// Tests may use this to verify the reader has not decoded a partial event.
	PartialLineBytes []byte
}

// JSONLFixtureConcurrentTail returns a ConcurrentTailFixture where two events
// have been durably written (CompletedLines == 2) and a third event's JSON
// object has been partially flushed without a terminating newline.
//
// This simulates the state a tailing reader observes mid-write on a live
// daemon: POSIX O_APPEND does not guarantee atomicity beyond PIPE_BUF so
// the reader must treat the growing final line as non-authoritative per §6.2.
//
// Spec: event-model.md §6.2 "Concurrent tailing".
func JSONLFixtureConcurrentTail() ConcurrentTailFixture {
	line0 := mustMarshalLine(minimalEnvelopeWire(jsonlFixtureEventID1))
	line1 := mustMarshalLine(fullEnvelopeWire(jsonlFixtureEventID2))

	// Partial write: JSON object exists but no trailing newline.
	partial, err := json.Marshal(minimalEnvelopeWire(jsonlFixtureEventID3))
	if err != nil {
		panic(fmt.Sprintf("jsonlFixture: ConcurrentTail: json.Marshal: %v", err))
	}
	// Simulate mid-write by truncating after 40 bytes (well inside the object).
	if len(partial) > 40 {
		partial = partial[:40]
	}

	var out []byte
	out = append(out, line0...)
	out = append(out, line1...)
	out = append(out, partial...)

	return ConcurrentTailFixture{
		JSONL:            out,
		CompletedLines:   2,
		PartialLineBytes: partial,
	}
}
