// Package digest implements the harmonik digest status-sheet builder.
//
// The builder is mechanism-only (no LLM). It reads durable file surfaces and
// returns a schema-versioned DigestJSON suitable for consumption by the
// cognition loop (CL-030..CL-033) or direct operator inspection.
//
// Spec ref: specs/cognition-loop.md §4.4 CL-030..CL-033;
// specs/process-lifecycle.md §PL-028d.
package digest

import "time"

// SuppressionState is the output of the deterministic suppression resolver
// (flywheel-motion.md §3). EXECUTE-BACKLOG is the default (Suppressed=false)
// when no source is active.
type SuppressionState struct {
	// Suppressed is true when at least one source is currently active.
	Suppressed bool `json:"suppressed"`

	// Sources lists the evaluated suppression sources with their resolved state.
	Sources []SuppressionSourceState `json:"sources"`

	// ConfigError is set when the sentinel config has a validation error
	// (e.g., phase_flag without phase_flag_expiry). The resolver fails-open:
	// the invalid source is treated as inactive so dispatch is not blocked.
	ConfigError string `json:"config_error,omitempty"`
}

// SuppressionSourceState is the resolved state of one suppression source.
type SuppressionSourceState struct {
	// Name identifies the source:
	//   "operator_attached"  — keeper session_keeper_operator_attached events
	//   "operator_dialogue"  — comms agent_message events from "operator"
	//   "phase_flag"         — sentinel.phase_flag in .harmonik/config.yaml
	Name string `json:"name"`

	// Active is true when this source is currently suppressing dispatch.
	Active bool `json:"active"`

	// LastSeen is when this source was last observed (zero if never seen).
	// For phase_flag, this is zero (the source is config-driven, not event-driven).
	LastSeen time.Time `json:"last_seen,omitempty"`

	// ExpiresAt is when the suppression from this source expires.
	// For operator_attached and operator_dialogue: LastSeen + effective TTL.
	// For phase_flag: PhaseFlagExpiry from the config.
	ExpiresAt time.Time `json:"expires_at,omitempty"`

	// Reason is a short diagnostic string.
	Reason string `json:"reason,omitempty"`
}

// SchemaVersion is the current digest JSON schema version (CL-033).
// N-1 consumers MUST accept version 1 when the current version is 2, etc.
const SchemaVersion = 1

// DigestJSON is the schema-versioned status sheet (CL-033).
// --json emits one NDJSON line containing this struct (CL-030).
type DigestJSON struct {
	// SchemaVersion is CL-033's required field. Loop MUST refuse unknown versions.
	SchemaVersion int `json:"schema_version"`

	// GeneratedAt is the wall-clock time when the digest was computed.
	GeneratedAt time.Time `json:"generated_at"`

	// Queue summarises the current queue.json state.
	Queue QueueSummary `json:"queue"`

	// RecentCommits is a slice of recent commits on origin/main (up to 10).
	RecentCommits []CommitSummary `json:"recent_commits"`

	// RecentEvents is a slice of events from events.jsonl after the watermark.
	// Limited to the 20 most recent by default; --full lifts this cap.
	RecentEvents []EventSummary `json:"recent_events"`

	// ReadyBeads is the output of `br ready` (unblocked open beads).
	ReadyBeads []BeadSummary `json:"ready_beads"`

	// InProgressBeads is the output of `br list --status in_progress`.
	InProgressBeads []BeadSummary `json:"in_progress_beads"`

	// OpenNotes is the list of unresolved entries from notes.jsonl.
	// Capped to 20 by default per CL-032; --full lifts this cap.
	OpenNotes []NoteSummary `json:"open_notes"`

	// KerfNext holds the raw output from `kerf next --format=json`, or nil
	// when kerf is not installed or returns an error.
	KerfNext interface{} `json:"kerf_next,omitempty"`

	// PendingDecisions lists every unacknowledged decision_required event (EV-044).
	// Surfaced unconditionally — not filtered by SinceEventID — so that quiet
	// periods (watermark advanced past the event) cannot suppress them. An entry
	// is removed only after the matching decision_acknowledged is observed.
	PendingDecisions []DecisionRequiredSummary `json:"pending_decisions,omitempty"`

	// Truncated reports what was truncated per CL-032 size budget.
	Truncated *TruncationReport `json:"truncated,omitempty"`

	// HasUndeployedTail is true when at least one closed bead carries a
	// Phase-2 class label (a done_definition that requires deploy+verify beyond
	// "merged") and has not yet been verified (flywheel-motion.md §5.3).
	//
	// Until Phase-2 verify is implemented a closed Phase-2 bead counts as
	// merged-but-undeployed. The opportunity gate MUST treat this as actionable
	// work so the flywheel does not stall on an empty ready-beads list
	// (flywheel-motion.md §5.2).
	//
	// False when no Phase-2 classes are configured, br is unavailable, or no
	// closed beads carry Phase-2 labels.
	HasUndeployedTail bool `json:"has_undeployed_tail"`

	// SuppressionState is the output of the deterministic suppression resolver
	// (flywheel-motion.md §3). Suppressed=false means EXECUTE-BACKLOG (default).
	// Present in every digest so the cognition loop can always read the current state.
	SuppressionState SuppressionState `json:"suppression_state"`

	// Errors lists non-fatal collection errors so the loop can surface them.
	Errors []string `json:"errors,omitempty"`
}

// QueueSummary holds queue.json–derived fields.
type QueueSummary struct {
	// Present is false when queue.json does not exist (no active queue).
	Present bool `json:"present"`

	// Status is the queue-level lifecycle state (e.g. "active", "completed").
	Status string `json:"status,omitempty"`

	// ActiveRunCount is the number of dispatched-but-not-terminal items.
	ActiveRunCount int `json:"active_run_count"`

	// PendingCount is the number of items in pending status.
	PendingCount int `json:"pending_count"`

	// ActiveRuns lists currently dispatched items (limited to 10 by default).
	ActiveRuns []QueueItemSummary `json:"active_runs,omitempty"`

	// ActiveRunsOmitted carries the count of active runs hidden by the cap so
	// the builder can flow it into the top-level TruncationReport (DC-005).
	// Not serialized here — the canonical surface is Truncated.ActiveRunsOmitted.
	ActiveRunsOmitted int `json:"-"`
}

// QueueItemSummary is a single dispatched queue item.
type QueueItemSummary struct {
	BeadID string `json:"bead_id"`
	RunID  string `json:"run_id,omitempty"`
	Status string `json:"status"`
}

// CommitSummary is a single recent commit on origin/main.
type CommitSummary struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
}

// EventSummary is a single event record from events.jsonl.
type EventSummary struct {
	EventID string `json:"event_id"`
	Type    string `json:"type"`
	RunID   string `json:"run_id,omitempty"`
}

// BeadSummary is a bead entry from `br ready` or `br list`.
type BeadSummary struct {
	BeadID   string `json:"bead_id"`
	Title    string `json:"title"`
	Priority int    `json:"priority,omitempty"`
	Status   string `json:"status,omitempty"`
}

// NoteSummary is a single unresolved entry from notes.jsonl.
type NoteSummary struct {
	Kind       string    `json:"kind"`
	Text       string    `json:"text"`
	Ts         time.Time `json:"ts"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
	SessionID  string    `json:"session_id,omitempty"`
	Refs       []string  `json:"refs,omitempty"`
}

// DecisionRequiredSummary is an unacknowledged decision_required event (EV-044).
// Surfaced in the digest regardless of the SinceEventID watermark so that
// consumers cannot silently suppress it during quiet periods.
type DecisionRequiredSummary struct {
	EventID         string `json:"event_id"`
	AckToken        string `json:"ack_token"`
	SubjectKind     string `json:"subject_kind"`
	SubjectID       string `json:"subject_id"`
	Reason          string `json:"reason"`
	SuggestedAction string `json:"suggested_action,omitempty"`
}

// TruncationReport describes what was omitted per CL-032.
type TruncationReport struct {
	// ActiveRunsOmitted is the count of active runs not shown.
	ActiveRunsOmitted int `json:"active_runs_omitted,omitempty"`
	// RecentEventsOmitted is the count of recent events not shown.
	RecentEventsOmitted int `json:"recent_events_omitted,omitempty"`
	// OpenNotesOmitted is the count of open notes not shown.
	OpenNotesOmitted int `json:"open_notes_omitted,omitempty"`
}

// Limits controls truncation thresholds (CL-032).
// Default values apply the ordinary-conditions caps; --full disables them.
type Limits struct {
	// MaxActiveRuns is the maximum number of active run entries to include.
	// 0 means use the default (10).
	MaxActiveRuns int
	// MaxRecentEvents is the maximum number of recent events to include.
	// 0 means use the default (20).
	MaxRecentEvents int
	// MaxOpenNotes is the maximum number of open notes to include.
	// 0 means use the default (20).
	MaxOpenNotes int
}

// DefaultLimits returns the ordinary-conditions caps per CL-032.
func DefaultLimits() Limits {
	return Limits{
		MaxActiveRuns:   10,
		MaxRecentEvents: 20,
		MaxOpenNotes:    20,
	}
}

// FullLimits returns limits that disable truncation (--full mode).
func FullLimits() Limits {
	return Limits{
		MaxActiveRuns:   0,
		MaxRecentEvents: 0,
		MaxOpenNotes:    0,
	}
}

func (l Limits) maxActiveRuns() int {
	if l.MaxActiveRuns <= 0 {
		return 0 // no cap
	}
	return l.MaxActiveRuns
}

func (l Limits) maxRecentEvents() int {
	if l.MaxRecentEvents <= 0 {
		return 0
	}
	return l.MaxRecentEvents
}

func (l Limits) maxOpenNotes() int {
	if l.MaxOpenNotes <= 0 {
		return 0
	}
	return l.MaxOpenNotes
}
