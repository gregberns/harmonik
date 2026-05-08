package core

import (
	"encoding/json"
	"reflect"
	"time"

	"github.com/google/uuid"
)

// Event is the common envelope record every event carries (event-model.md §6.1).
//
// Every event emitted to the bus or appended to JSONL MUST carry these fields per
// EV-001. The envelope is type-discriminated: the payload bytes are decoded by the
// per-type registry keyed on Type per §6.3.
//
// Type uses string directly. The EventType enum (~79 rows) is owned by the parent
// bead hk-hqwn.59 and is too large to define here. The hoist to EventType is
// non-breaking once the enum lands.
//
// Idempotent emission (event-model.md §4.4 EV-018): every producer MUST emit each
// event in idempotent form. Re-emitting the same event (same EventID, same payload)
// during recovery MUST be safe for downstream observational consumers. Producers MUST
// NOT encode one-shot side-effect semantics into event payloads; side effects belong
// in the run's checkpoint trail (git) or in Beads. Use EquivalentTo to assert
// idempotency-equivalence between two Event values at the data-shape level.
type Event struct {
	// EventID is the unique identifier for this event (EV-002).
	// MUST be a UUIDv7 stamped by the daemon watcher at enqueue time per EV-002b
	// (event-model.md §4.1 EV-002b). Handler subprocesses MUST NOT populate this
	// field; they write an envelope with no event_id (or a placeholder the daemon
	// discards) and the daemon watcher stamps event_id, envelope timestamps, and
	// source_subsystem at the moment of enqueue. Monotonic non-decrease within the
	// daemon process is enforced per EV-002a.
	// Required (non-nil UUID).
	EventID EventID

	// SchemaVersion is the envelope schema version. Bump on envelope-level changes
	// per EV-028. Non-zero; the per-type registry independently tracks per-payload
	// versions. "N-1 readable" applies per EV-029.
	SchemaVersion int

	// Type identifies the event type; MUST be one of the §8 rows (event-model.md §8).
	// The EventType enum is declared in a separate bead (hk-hqwn.59); this field
	// uses string until that enum lands (non-breaking hoist).
	// Required (non-empty).
	Type string

	// TimestampWall is the RFC 3339 wall-clock time at the emitter (EV-001).
	// The emitter MUST perform exactly one wall-clock read per emission and reuse
	// that reading for both TimestampWall and UUIDv7 generation so the envelope is
	// self-consistent.
	//
	// Advisory only for cross-process ordering (EV-006, event-model.md §4.2 EV-006):
	// TimestampWall MUST NOT be used for ordering decisions across processes; NTP
	// skew, clock adjustments, and container-host time sync make it unreliable.
	// Consumers that need cross-process ordering MUST use EventID (UUIDv7) per
	// EV-002. TimestampWall is for audit, human-readable display, and external
	// correlation only.
	//
	// Required (non-zero).
	TimestampWall time.Time

	// TimestampMonoNsec is the optional monotonic nanoseconds from the emitter's
	// process clock (event-model.md §6.1 EV-001, EV-003, EV-007). Process-scoped;
	// MUST NOT be compared across daemon restarts or across processes. Meaningful
	// ONLY for intra-process ordering within the emitter's lifetime.
	// Within a single emitter process, TimestampMonoNsec (when present) MUST be
	// non-decreasing across emissions in emission order (EV-007).
	// When non-nil must be > 0.
	TimestampMonoNsec *int64

	// RunID is present when the event is scoped to a run (EV-001).
	// Optional; when non-nil must not be uuid.Nil.
	RunID *RunID

	// StateID is present when the event is scoped to a run-state (EV-001).
	// Optional; when non-nil must not be uuid.Nil.
	StateID *StateID

	// SourceSubsystem is the Go-package-identifier string of the emitting subsystem
	// per EV-004 and architecture.md §4.5. Required (non-empty).
	SourceSubsystem string

	// TraceContext carries optional cross-subsystem correlation identifiers (§6.1).
	// Optional; when non-nil must satisfy TraceContext.Valid().
	TraceContext *TraceContext

	// Payload is the type-specific event body as raw JSON bytes (json.RawMessage).
	// Decoded by the per-type registry keyed on Type per §6.3.
	// Required (non-nil). A zero-length (but non-nil) json.RawMessage is valid for
	// event types that carry no body (e.g., a heartbeat); decoding is the consumer's
	// responsibility.
	Payload json.RawMessage
}

// Valid reports whether all required fields carry non-zero values and all
// optional pointer fields, when non-nil, satisfy their structural constraints.
//
// Rules:
//   - EventID is not uuid.Nil
//   - SchemaVersion is not zero
//   - Type is non-empty (enum validation is a consumer concern; the enum does not exist yet)
//   - TimestampWall is not the zero Time
//   - TimestampMonoNsec, when non-nil, must be > 0
//   - RunID, when non-nil, must dereference to a non-uuid.Nil RunID
//   - StateID, when non-nil, must dereference to a non-uuid.Nil StateID
//   - SourceSubsystem is non-empty
//   - TraceContext, when non-nil, must satisfy TraceContext.Valid()
//   - Payload is non-nil (zero-length is permitted for no-body event types)
func (e Event) Valid() bool {
	if uuid.UUID(e.EventID) == uuid.Nil {
		return false
	}
	if e.SchemaVersion == 0 {
		return false
	}
	if e.Type == "" {
		return false
	}
	if e.TimestampWall.IsZero() {
		return false
	}
	if e.TimestampMonoNsec != nil && *e.TimestampMonoNsec <= 0 {
		return false
	}
	if e.RunID != nil && uuid.UUID(*e.RunID) == uuid.Nil {
		return false
	}
	if e.StateID != nil && uuid.UUID(*e.StateID) == uuid.Nil {
		return false
	}
	if e.SourceSubsystem == "" {
		return false
	}
	if e.TraceContext != nil && !e.TraceContext.Valid() {
		return false
	}
	if e.Payload == nil {
		return false
	}
	return true
}

// EquivalentTo reports whether e and other are idempotency-equivalent per
// event-model.md §4.4 EV-018.
//
// Two Event values are equivalent when they carry the same idempotency identity:
// same EventID, same Type, and same Payload. Envelope fields that do not
// participate in idempotency identity (TimestampWall, TimestampMonoNsec,
// SourceSubsystem, RunID, StateID, TraceContext, SchemaVersion) are intentionally
// excluded so that minor re-emission differences (e.g., wall-clock drift between
// the original emit and a recovery re-emit) do not break equivalence.
//
// Payload comparison uses reflect.DeepEqual on the decoded form to achieve
// order-independent comparison of JSON object fields: two JSON payloads that
// represent the same logical value but differ only in key ordering are considered
// equal. If either payload fails to unmarshal (malformed JSON), the comparison
// falls back to a byte-level equality check so that two identically-malformed
// payloads are still considered equivalent.
//
// This method is a data-shape sensor only. It does NOT suppress duplicate
// emissions or interact with the EventBus (see hk-hqwn.57).
func (e Event) EquivalentTo(other Event) bool {
	if e.EventID != other.EventID {
		return false
	}
	if e.Type != other.Type {
		return false
	}

	// Unmarshal both payloads into generic any values so that reflect.DeepEqual
	// performs an order-independent structural comparison of JSON object fields.
	// This handles the common case where map key ordering differs between two
	// otherwise identical JSON payloads.
	var ep, op any
	if err := json.Unmarshal(e.Payload, &ep); err != nil {
		// Malformed JSON: fall back to byte-level comparison.
		return string(e.Payload) == string(other.Payload)
	}
	if err := json.Unmarshal(other.Payload, &op); err != nil {
		// Malformed JSON: fall back to byte-level comparison.
		return string(e.Payload) == string(other.Payload)
	}
	return reflect.DeepEqual(ep, op)
}
