package core

import "fmt"

// jsonlformat_hqwn58.go — on-disk JSONL line format shapes and file path
// constants for the event log (event-model.md §6.2).
//
// Spec ref: specs/event-model.md §6.2.
// Bead ref: hk-hqwn.58.

// ---------------------------------------------------------------------------
// File path constants (relative to the .harmonik directory root)
// ---------------------------------------------------------------------------

// EventsJSONLPath is the path of the primary event log relative to the
// project's .harmonik directory (event-model.md §6.2).
//
// Full path: <project-root>/.harmonik/events/events.jsonl
// The primary log is append-only, single-file-per-project per EV-020.
const EventsJSONLPath = "events/events.jsonl"

// DeadLettersJSONLPath is the path of the dead-letter log relative to the
// project's .harmonik directory (event-model.md §6.2).
//
// Full path: <project-root>/.harmonik/events/dead-letters.jsonl
// Entries carry the same line format as the primary log with an additional
// top-level dead_letter object (see DeadLetterLine).
const DeadLettersJSONLPath = "events/dead-letters.jsonl"

// SpillJSONLPath returns the path of the per-consumer spill file relative to
// the project's .harmonik directory (event-model.md §6.2; EV-011a).
//
// Full path: <project-root>/.harmonik/events/spill-<consumerName>.jsonl
// Spill files hold fsync-boundary (F-class) events that could not be queued
// to the named consumer. Spill files MUST be pre-created at subscription-
// registration time with O_CREAT|O_APPEND|O_DSYNC semantics; failure to
// create MUST fail daemon startup per EV-011a.
func SpillJSONLPath(consumerName string) string {
	return fmt.Sprintf("events/spill-%s.jsonl", consumerName)
}

// ---------------------------------------------------------------------------
// Dead-letter line wrapper
// ---------------------------------------------------------------------------

// DeadLetterAnnotation is the metadata block embedded in every dead-letter
// log entry (event-model.md §6.2).
//
// The dead-letter log uses the same JSONL line format as the primary event
// log with an additional top-level `dead_letter` field carrying this struct.
// The full on-disk line is represented by DeadLetterLine.
type DeadLetterAnnotation struct {
	// ConsumerName is the name of the consumer whose retry policy was exhausted.
	// Required (non-empty).
	ConsumerName string `json:"consumer_name"`

	// RetriesAttempted is the number of delivery attempts made before the event
	// was moved to the dead-letter log. Required (must be >= 0).
	RetriesAttempted int `json:"retries_attempted"`

	// EnqueuedAt is the RFC 3339 wall-clock timestamp at dead-letter enqueue.
	// Required (non-empty).
	EnqueuedAt string `json:"enqueued_at"`
}

// Valid reports whether a is a well-formed DeadLetterAnnotation.
//
// Rules:
//   - ConsumerName must be non-empty.
//   - RetriesAttempted must be >= 0.
//   - EnqueuedAt must be non-empty.
func (a DeadLetterAnnotation) Valid() bool {
	return a.ConsumerName != "" && a.RetriesAttempted >= 0 && a.EnqueuedAt != ""
}

// DeadLetterLine is the full on-disk shape of a dead-letter log entry
// (event-model.md §6.2).
//
// A dead-letter entry is a copy of the original Event envelope plus a
// `dead_letter` metadata block identifying the consumer and retry count.
// The dead-letter log file is at DeadLettersJSONLPath.
//
// Because DeadLetterLine embeds Event (by value), all envelope fields are
// present at the top level when marshalled to JSON, and the dead_letter block
// appears as a top-level sibling field — matching the §6.2 shape.
//
// Consumers replaying from the dead-letter log MUST treat the embedded Event
// as the original event and MUST be idempotent on replay per EV-014b.
type DeadLetterLine struct {
	Event

	// DeadLetter carries the metadata about why this event was dead-lettered.
	// Required; must satisfy DeadLetterAnnotation.Valid().
	DeadLetter DeadLetterAnnotation `json:"dead_letter"`
}

// Valid reports whether l is a well-formed DeadLetterLine.
//
// Rules:
//   - The embedded Event must satisfy Event.Valid().
//   - DeadLetter must satisfy DeadLetterAnnotation.Valid().
func (l DeadLetterLine) Valid() bool {
	return l.Event.Valid() && l.DeadLetter.Valid()
}

// ---------------------------------------------------------------------------
// Read-recovery context
// ---------------------------------------------------------------------------

// JSONLReadContext identifies the context in which a JSONL read is occurring
// (event-model.md §6.2). The context determines which read-recovery rule
// applies to a torn-tail line.
type JSONLReadContext string

const (
	// JSONLReadContextPostCrashStartup indicates the read is occurring during
	// post-crash startup recovery. Torn-tail lines MUST be discarded silently
	// per §6.2 (expected lossy-tail shape). This is the only context in which
	// silent discard is permitted.
	JSONLReadContextPostCrashStartup JSONLReadContext = "post_crash_startup"

	// JSONLReadContextLiveDaemon indicates the read is occurring on a live
	// daemon (investigator walk or observational replay). A torn-tail line
	// MUST emit store_divergence_detected{divergence_kind=schema_mismatch,
	// post_crash_window=false} before discarding per §6.2.
	JSONLReadContextLiveDaemon JSONLReadContext = "live_daemon"
)

// Valid reports whether c is one of the two declared JSONLReadContext constants.
func (c JSONLReadContext) Valid() bool {
	switch c {
	case JSONLReadContextPostCrashStartup, JSONLReadContextLiveDaemon:
		return true
	default:
		return false
	}
}
