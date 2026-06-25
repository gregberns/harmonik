package queue

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// schemaVersion is the only supported queue.json schema version in v0.1.
// Any envelope with a different value is rejected at unmarshal time.
const schemaVersion = 1

// QueueNameMain is the reserved default queue name. Submits that omit the
// name field are routed to this queue. The name is valid per the naming rule
// (QM-002/2.1): lowercase alphanum + hyphens, 1–64 chars.
//
// Bead ref: hk-tigaf.2.
const QueueNameMain = "main"

// MaxItemAttempts is the maximum number of outer-loop dispatch attempts per
// queue item. Items that reach this limit are skipped and marked failed with
// reason "max_attempts_exceeded". The workloop enforces this bound at Phase 3
// (dispatch-stamp); waveEligible/streamEligible apply it as defense-in-depth.
//
// Bead ref: hk-6pspu; design: docs/design/workloop-bounded-retry.md.
const MaxItemAttempts = 3

// MaxReviewLoopFailures is the maximum number of review-loop runs that may
// fail with needs-attention (no_progress, cap_hit, blocked, or error) for a
// single queue item before the item is permanently closed instead of reopened.
// Caps the total Claude-session spend for a bead that is structurally stuck.
//
// Each failure corresponds to a full review-loop run (up to reviewLoopIterationCap
// implementer+reviewer cycles), so MaxReviewLoopFailures=2 allows at most
// 2 × reviewLoopIterationCap paid sessions before the bead is triage-flagged.
//
// Bead ref: hk-c1ah6.
const MaxReviewLoopFailures = 2

// QueueStatus is the queue-level lifecycle state (specs/queue-model.md §2.2).
type QueueStatus string

const (
	// QueueStatusActive means groups are advancing per §5.
	QueueStatusActive QueueStatus = "active"

	// QueueStatusPausedByFailure means a group reached complete-with-failures
	// and the queue was paused per §8.3.
	QueueStatusPausedByFailure QueueStatus = "paused-by-failure"

	// QueueStatusPausedByDrain means the daemon entered operator-pause or
	// shutdown drain per §8.5.
	QueueStatusPausedByDrain QueueStatus = "paused-by-drain"

	// QueueStatusPausedByBudget means this queue's own per-queue spend ceiling
	// (Queue.SpendCapUSD, NQ-X1) tripped: the queue's attributed daily spend
	// reached its cap, so the per-queue spend meter paused ONLY this queue while
	// leaving sibling queues (and the global daemon ceiling) untouched. Distinct
	// from the global DaemonSpendMeter trip, which pauses the entire claude
	// handler type via handler-pause. Cleared back to active on the next UTC
	// day-rollover by the per-queue spend meter.
	//
	// Bead ref: hk-tigaf.11 (NQ-X1).
	QueueStatusPausedByBudget QueueStatus = "paused-by-budget"

	// QueueStatusCompleted means all groups are complete-success; the
	// queue.json file is unlinked per QM-003.
	QueueStatusCompleted QueueStatus = "completed"

	// QueueStatusCancelled means the operator cancelled the run (SIGINT/SIGTERM
	// or a global timeout) before all groups reached a terminal state. The
	// queue.json file is left on disk with this status so the next harmonik run
	// can detect and overwrite it cleanly without the QM-027 "already active"
	// guard triggering. Exit code 1 is returned to the operator.
	//
	// Bead ref: hk-ppt32.
	QueueStatusCancelled QueueStatus = "cancelled"
)

// GroupKind distinguishes the two group primitives (specs/queue-model.md §2.4).
type GroupKind string

const (
	// GroupKindWave is a fixed closed set dispatched concurrently up to
	// --max-concurrent; not appendable post-submit.
	GroupKindWave GroupKind = "wave"

	// GroupKindStream is an ordered open-ended sequence dispatched as slots
	// open; appendable while pending or active.
	GroupKindStream GroupKind = "stream"
)

// GroupStatus is the per-group state machine state (specs/queue-model.md §2.5).
type GroupStatus string

const (
	// GroupStatusPending means the predecessor group has not yet reached
	// complete-success; no items dispatched.
	GroupStatusPending GroupStatus = "pending"

	// GroupStatusActive means the predecessor is complete-success (or this is
	// group 0); items are eligible for dispatch.
	GroupStatusActive GroupStatus = "active"

	// GroupStatusCompleteSuccess is the terminal success state: every item is
	// terminal and zero have failed.
	GroupStatusCompleteSuccess GroupStatus = "complete-success"

	// GroupStatusCompleteWithFailures is the terminal failure state: every item
	// is terminal and at least one has failed.
	GroupStatusCompleteWithFailures GroupStatus = "complete-with-failures"
)

// ItemStatus is the per-item execution state (specs/queue-model.md §2.7).
type ItemStatus string

const (
	// ItemStatusPending means the item is eligible for dispatch once the group
	// is active and capacity allows.
	ItemStatusPending ItemStatus = "pending"

	// ItemStatusDispatched means the daemon has handed the bead to the
	// execution-model dispatcher; run_id is populated.
	ItemStatusDispatched ItemStatus = "dispatched"

	// ItemStatusCompleted means the run reached run_completed terminal per
	// execution-model.md §7.1.
	ItemStatusCompleted ItemStatus = "completed"

	// ItemStatusFailed means the run reached run_failed terminal per
	// execution-model.md §7.1.
	ItemStatusFailed ItemStatus = "failed"

	// ItemStatusDeferredForLedgerDep is transient; a Beads blocks edge is open
	// against this bead per QM-025.
	ItemStatusDeferredForLedgerDep ItemStatus = "deferred-for-ledger-dep"
)

// Item is a single bead execution entry within a Group
// (specs/queue-model.md §2.6 RECORD Item).
type Item struct {
	// BeadID is the Beads ledger reference; immutable.
	BeadID core.BeadID `json:"bead_id"`

	// Status is the per-item execution state (§2.7).
	Status ItemStatus `json:"status"`

	// RunID is daemon-minted on transition to dispatched per
	// execution-model.md §4.3. Nil until the item is dispatched.
	//
	// TODO(hk-9s6yr): replace *string with a typed core.RunID pointer once
	// JSON round-trip helpers land for RunID (uuid.UUID underlying type is not
	// directly JSON-marshallable without the TextMarshaler wrapping applied here).
	// For now, the canonical UUID string is used to stay JSON-clean.
	RunID *string `json:"run_id"`

	// AppendedAt is set when the item was appended post-submit (streams only).
	// None (nil) for submit-time items.
	AppendedAt *time.Time `json:"appended_at"`

	// Context is an optional operator-supplied free-form string injected into
	// the agent-task.md as an "## Extra Context" section (hk-boiwe). When
	// non-empty the daemon threads it through to WriteAgentTask via
	// claudeRunCtx.extraContext. Empty means no section is rendered.
	Context string `json:"context,omitempty"`

	// WorkflowMode is an optional per-item workflow-mode override (hk-hiqrl).
	// When non-empty it takes precedence over the per-bead workflow:<mode>
	// label (tier-1) and the daemon default (tier-3) in the EM-012a resolution
	// walk. Valid values: "single", "review-loop", "dot". Empty means no override.
	WorkflowMode string `json:"workflow_mode,omitempty"`

	// WorkflowRef is an optional path to the workflow definition file used when
	// WorkflowMode is "dot" (hk-qo9pq). Relative paths are resolved against the
	// project directory at dispatch time. Empty falls back to the project-level
	// convention (workflow.dot in the project root).
	WorkflowRef string `json:"workflow_ref,omitempty"`

	// TemplateParams is the map of KEY→VALUE template parameters sealed into the
	// item at claim time (hk-55zv2 / WG-045).  Applied as a pre-parse substitution
	// pass over the raw .dot source before dot.Parse is called.  nil or empty means
	// no substitution (token-free .dot passes through byte-identical).
	TemplateParams map[string]string `json:"template_params,omitempty"`

	// Attempts counts outer-loop dispatch attempts for this item. Incremented
	// each time the workloop stamps the item as dispatched (Phase 3). Monotonic
	// within a queue lifetime — never reset on claim-failure revert. Items that
	// reach MaxItemAttempts are skipped by the workloop and marked failed.
	// Zero-value default is backward-compatible with existing queue.json files.
	//
	// Bead ref: hk-6pspu.
	Attempts int `json:"attempts"`

	// LastFailureReason records the most recent failure reason when a dispatch
	// attempt fails (e.g. ClaimBead error, max_attempts_exceeded). Diagnostic
	// only — not used for control flow.
	//
	// Bead ref: hk-6pspu.
	LastFailureReason string `json:"last_failure_reason,omitempty"`

	// ReviewLoopFailures counts how many review-loop runs for this item have
	// terminated with needs-attention (no_progress, cap_hit, blocked, or error).
	// Monotonic within a queue lifetime. When this reaches MaxReviewLoopFailures,
	// beadRunOne permanently closes the bead (CloseBead needsAttention=true)
	// instead of reopening it for another retry, capping total session spend.
	// Zero-value is backward-compatible with existing queue.json files.
	//
	// Bead ref: hk-c1ah6.
	ReviewLoopFailures int `json:"review_loop_failures,omitempty"`
}

// Group is one execution group within the Queue envelope
// (specs/queue-model.md §2.3 RECORD Group).
type Group struct {
	// GroupIndex is the 0-based dense index; immutable after submit.
	GroupIndex int `json:"group_index"`

	// Kind distinguishes wave (fixed) from stream (open-ended) groups.
	Kind GroupKind `json:"kind"`

	// Status is the per-group state machine state (§2.5).
	Status GroupStatus `json:"status"`

	// Items is the ordered list of work items. Waves are immutable after
	// submit; streams are append-only.
	Items []Item `json:"items"`

	// CreatedAt is set at submit accept time.
	CreatedAt time.Time `json:"created_at"`

	// StartedAt is set when the group transitions pending → active.
	StartedAt *time.Time `json:"started_at"`

	// CompletedAt is set when the group transitions to a terminal status.
	CompletedAt *time.Time `json:"completed_at"`
}

// Queue is the daemon-owned execution-plan envelope submitted by an external
// orchestrator (specs/queue-model.md §2.1 RECORD Queue).
//
// Use [UnmarshalQueue] to deserialise from JSON; it enforces the
// schema_version == 1 invariant. Direct json.Unmarshal skips that check.
type Queue struct {
	// SchemaVersion MUST equal 1. Enforced on unmarshal via [UnmarshalQueue].
	SchemaVersion int `json:"schema_version"`

	// QueueID is the daemon-minted UUIDv7 at queue-submit accept.
	//
	// TODO: replace string with a typed core.QueueID alias once that alias is
	// minted in core/ (follow-up bead; typed-alias-deferral pattern per
	// implementer-protocol.md).
	QueueID string `json:"queue_id"`

	// Name is the durable routing key for this queue (QM-002/2.1 queue-naming
	// rule). Charset: [a-z0-9-], length 1–64. The reserved default is "main"
	// (QueueNameMain); omitted or empty fields are treated as "main" at submit
	// and stored normalised.
	//
	// Name is distinct from QueueID: QueueID is a per-submission UUIDv7 that
	// changes on every submit; Name is a stable operator-chosen identifier that
	// persists across submissions (the per-name single-active guard uses Name,
	// not QueueID).
	//
	// omitempty preserves round-trip compat with existing queue.json files that
	// predate the name field; absent fields unmarshal to "" and are normalised
	// to "main" on first use.
	//
	// Bead ref: hk-tigaf.2.
	Name string `json:"name,omitempty"`

	// Workers is the per-queue concurrent-dispatch ceiling (QM-066). The
	// dispatcher admits at most Workers in-flight runs for this queue at any
	// instant, independent of (and never exceeding) the global --max-concurrent
	// ceiling (QM-062). When zero or absent the daemon defaults it to the global
	// --max-concurrent at submit/load time (so a queue with no explicit Workers
	// behaves exactly like the pre-named-queues single-queue daemon). A value
	// greater than the global cap is permitted (oversubscription) but the global
	// ceiling still wins at runtime; the daemon logs the oversubscription once at
	// submit time.
	//
	// omitempty preserves round-trip compat with queue.json files that predate
	// the field; absent unmarshals to 0 and is defaulted on first use.
	//
	// Bead ref: hk-tigaf.4 (NQ-B1).
	Workers int `json:"workers,omitempty"`

	// SpendCapUSD is the OPTIONAL per-queue daily spend ceiling in USD (NQ-X1).
	// When positive, the per-queue spend meter accumulates this queue's
	// attributed daily spend (via budget_accrual events keyed back to this queue
	// by RunID → RunHandle.QueueName) and pauses ONLY this queue
	// (Status = QueueStatusPausedByBudget) when the attributed spend reaches the
	// cap — sibling queues keep dispatching, so a single busy queue can no longer
	// starve the whole daemon. The global DaemonSpendMeter ceiling remains the
	// daemon-wide ceiling and binds independently; admission requires
	// global-remaining > 0 AND (no per-queue cap OR queue-remaining > 0), so the
	// STRICTER ceiling wins. A cap greater than the effective global ceiling is
	// permitted (the global ceiling still binds), mirroring the Workers
	// oversubscription rule; the daemon logs that case once at submit.
	//
	// Zero or absent means NO per-queue cap = behaviour identical to the
	// pre-NQ-X1 daemon (only the global meter applies). omitempty preserves
	// round-trip compat with queue.json files that predate the field; absent
	// unmarshals to 0 (no cap).
	//
	// Bead ref: hk-tigaf.11 (NQ-X1).
	SpendCapUSD float64 `json:"spend_cap_usd,omitempty"`

	// DefaultHarness is the per-queue harness-selection default — tier 2 of the
	// four-tier precedence walk (bead-label > per-queue > node > global) in
	// resolveHarness (internal/daemon/harnessresolve.go). When set to a valid
	// core.AgentType it serves as resolveHarness's queueDefault argument so every
	// bead dispatched from this queue selects that harness unless a per-bead
	// harness:<agent-type> label (tier 1) overrides it.
	//
	// The value MUST satisfy core.AgentType.Valid() (AR-025: ^[a-z][a-z0-9-]{1,62}$).
	// Invalid values are normalised to empty at submit time (treated as absent,
	// so the precedence walk falls through to the node/global tiers) — consistent
	// with resolveHarness's tier-2 .Valid() guard. The empty value means "no
	// per-queue default" and is the backward-compatible default for queue.json
	// files that predate this field.
	//
	// NOTE: wiring this field into the dispatch/cascade resolveHarness call at
	// launch is C5/T12 (hk-xhawy) — this field only needs to exist, persist,
	// validate, and be readable here (C4/T6, hk-4x3rg).
	//
	// omitempty preserves round-trip compat with queue.json files that predate
	// the field; absent unmarshals to "" (no default).
	//
	// Bead ref: hk-4x3rg [C4/T6].
	DefaultHarness core.AgentType `json:"default_harness,omitempty"`

	// LocalOnly forces every bead dispatched from this queue to run locally,
	// bypassing remote-worker selection entirely. When true the SelectWorker call
	// is skipped and the run executes on box A, even when a worker is configured
	// and available. This permanently kills "can't land a local fix while remote
	// is enabled" — the queue itself is the routing gate, not a daemon restart.
	//
	// omitempty: absent/false = default behaviour (remote when available).
	// Bead ref: hk-f10xl [L5 Move 2].
	LocalOnly bool `json:"local_only,omitempty"`

	// WorkerTarget pins every bead dispatched from this queue to the named remote
	// worker. When non-empty and the named worker is enabled with a free slot, the
	// run is dispatched there; when the worker is absent, disabled, or at capacity
	// the run falls back to local (same semantics as a nil SelectWorker result).
	// Empty = no pinning; any available worker is eligible (default behaviour).
	//
	// omitempty: absent/empty = default behaviour.
	// Bead ref: hk-f10xl [L5 Move 2].
	WorkerTarget string `json:"worker_target,omitempty"`

	// SubmittedAt is set at queue-submit accept; ISO 8601 / UTC.
	SubmittedAt time.Time `json:"submitted_at"`

	// Groups is the ordered list of Group records; at least one entry.
	Groups []Group `json:"groups"`

	// Status is the queue-level lifecycle state (§2.2).
	Status QueueStatus `json:"status"`
}

// UnmarshalQueue deserialises a Queue from JSON and enforces the
// schema_version == 1 invariant (specs/queue-model.md §2.1, QM-002).
//
// Returns [ErrSchemaVersion] when the envelope carries any schema_version
// value other than 1.
func UnmarshalQueue(data []byte) (Queue, error) {
	var q Queue
	if err := json.Unmarshal(data, &q); err != nil {
		return Queue{}, err
	}
	if q.SchemaVersion != schemaVersion {
		return Queue{}, fmt.Errorf("%w: got %d, want %d", ErrSchemaVersion, q.SchemaVersion, schemaVersion)
	}
	return q, nil
}

// ErrSchemaVersion is returned by [UnmarshalQueue] when the envelope's
// schema_version is not equal to 1.
var ErrSchemaVersion = fmt.Errorf("unsupported queue schema_version")

// ---------------------------------------------------------------------------
// JSON-RPC request/response payload types (specs/queue-model.md §2.10)
// ---------------------------------------------------------------------------

// QueueSubmitRequest is the payload for the queue-submit JSON-RPC method
// (specs/queue-model.md §2.10 RECORD QueueSubmitRequest).
//
// Clients MUST NOT supply queue_id, submitted_at, status, or any item's
// run_id or status fields; those are daemon-minted at accept time and
// silently ignored if present in the request.
type QueueSubmitRequest struct {
	// Groups is one or more group definitions; field schemas per §2.3.
	Groups []Group `json:"groups"`

	// SchemaVersion MUST equal 1; forward-incompatible value refuses per QM-002.
	SchemaVersion int `json:"schema_version"`

	// Name is the durable routing key for the queue to create or extend. When
	// absent or empty it defaults to QueueNameMain ("main"). Must satisfy the
	// queue-naming rule: [a-z0-9-], 1–64 chars.
	//
	// The single-active guard (QM-027) is now per-name: submitting to a name
	// that already has an active (non-completed) queue is rejected.
	//
	// Bead ref: hk-tigaf.2.
	Name string `json:"name,omitempty"`

	// Workers is the requested per-queue concurrent-dispatch ceiling (QM-066).
	// When zero or absent the daemon defaults it to the global --max-concurrent.
	// A value greater than the global cap is accepted (oversubscription) and
	// logged once; the global ceiling still wins at runtime.
	//
	// Bead ref: hk-tigaf.4 (NQ-B1).
	Workers int `json:"workers,omitempty"`

	// SpendCapUSD is the requested OPTIONAL per-queue daily spend ceiling in USD
	// (NQ-X1; see Queue.SpendCapUSD). When zero or absent the queue has no
	// per-queue cap and only the global DaemonSpendMeter ceiling applies. A
	// positive value is carried verbatim onto the persisted Queue; a value
	// greater than the global ceiling is accepted (oversubscription) and logged
	// once — the global ceiling still binds at runtime.
	//
	// Bead ref: hk-tigaf.11 (NQ-X1).
	SpendCapUSD float64 `json:"spend_cap_usd,omitempty"`

	// DefaultHarness is the requested per-queue harness-selection default
	// (tier 2 of the precedence walk; see Queue.DefaultHarness). When set to a
	// valid core.AgentType it is carried onto the persisted Queue. An invalid or
	// empty value is normalised to empty (treated as absent) so the daemon's
	// harness resolver falls through to the node/global tiers — consistent with
	// the silently-ignored daemon-minted fields documented above.
	//
	// Bead ref: hk-4x3rg [C4/T6].
	DefaultHarness core.AgentType `json:"default_harness,omitempty"`
}

// QueueSubmitResponse is the response payload for queue-submit
// (specs/queue-model.md §2.10 RECORD QueueSubmitResponse).
type QueueSubmitResponse struct {
	// QueueID is the daemon-minted UUIDv7 per QM-010.
	QueueID string `json:"queue_id"`

	// Status is always "active" on a successful submit.
	Status QueueStatus `json:"status"`

	// GroupCount is the count of groups accepted.
	GroupCount int `json:"group_count"`
}

// QueueAppendRequest is the payload for the queue-append JSON-RPC method
// (specs/queue-model.md §2.10 RECORD QueueAppendRequest).
type QueueAppendRequest struct {
	// QueueID is an identity guard; rejected if it does not match the active
	// queue_id. When empty, the daemon resolves the active queue by Name.
	QueueID string `json:"queue_id,omitempty"`

	// Name is the durable routing key for append-by-name. When non-empty and
	// QueueID is absent, the daemon loads the active queue for this name and
	// uses its queue_id as the identity guard. When both Name and QueueID are
	// supplied, QueueID takes precedence. Defaults to QueueNameMain ("main")
	// when both are absent.
	//
	// Bead ref: hk-tigaf.8.
	Name string `json:"name,omitempty"`

	// GroupIndex is the 0-based index of the target stream group.
	GroupIndex int `json:"group_index"`

	// BeadIDs are the beads to append; validated per QM-020..QM-024.
	BeadIDs []core.BeadID `json:"bead_ids"`
}

// QueueAppendResponse is the response payload for queue-append
// (specs/queue-model.md §2.10 RECORD QueueAppendResponse).
type QueueAppendResponse struct {
	// AppendedCount is the number of items accepted and appended.
	AppendedCount int `json:"appended_count"`

	// NewTailIndices contains the 0-based item indices of the newly appended
	// items within the target group.
	NewTailIndices []int `json:"new_tail_indices"`
}

// QueueStatusRequest is the optional payload for the queue-status JSON-RPC
// method. All fields are optional: when both are absent the daemon returns the
// status of the "main" queue (backward-compatible default).
//
// Bead ref: hk-1k5as.
type QueueStatusRequest struct {
	// Name is the durable routing key; when non-empty the daemon loads the
	// queue for that name. Defaults to "main" when both Name and QueueID are
	// absent.
	Name string `json:"name,omitempty"`

	// QueueID is an identity selector; when non-empty (and Name is absent) the
	// daemon enumerates all active queues and returns the one whose queue_id
	// matches. Returns {queue: null} when no match is found.
	QueueID string `json:"queue_id,omitempty"`
}

// QueueStatusResponse is the response payload for queue-status
// (specs/queue-model.md §2.10 RECORD QueueStatusResponse).
//
// Queue is nil when the daemon has no active queue loaded (file absent or
// queue completed and unlinked per QM-003).
type QueueStatusResponse struct {
	// Queue is the full Queue envelope, or nil when no queue is active.
	Queue *Queue `json:"queue"`

	// MaxConcurrent is the current daemon-wide dispatch ceiling. Zero when
	// the daemon did not wire a ConcurrencyController (legacy/test callers).
	//
	// Bead ref: hk-ohiaf.
	MaxConcurrent int `json:"max_concurrent,omitempty"`
}

// QueueSummary is a single-queue row in a QueueListResponse.
//
// Bead ref: hk-tigaf.8.
type QueueSummary struct {
	// Name is the durable routing key for the queue.
	Name string `json:"name"`

	// QueueID is the daemon-minted UUIDv7 for the current submission.
	QueueID string `json:"queue_id"`

	// Status is the queue-level lifecycle state.
	Status QueueStatus `json:"status"`

	// PendingItems is the count of items in pending or deferred-for-ledger-dep
	// status across all groups.
	PendingItems int `json:"pending_items"`

	// Workers is the count of items currently dispatched (in-flight).
	Workers int `json:"workers"`

	// CompletedItems is the count of items that reached completed status.
	CompletedItems int `json:"completed_items"`

	// FailedItems is the count of items that reached failed status.
	FailedItems int `json:"failed_items"`
}

// QueueListResponse is the response payload for queue-list
// (specs/queue-model.md §2.10 RECORD QueueListResponse).
//
// Bead ref: hk-tigaf.8.
type QueueListResponse struct {
	// Queues is the list of queue summaries, one per active queue file.
	// Empty when no queues are present in .harmonik/queues/.
	Queues []QueueSummary `json:"queues"`

	// MaxConcurrent is the current daemon-wide dispatch ceiling. Zero when
	// the daemon did not wire a ConcurrencyController (legacy/test callers).
	//
	// Bead ref: hk-ohiaf.
	MaxConcurrent int `json:"max_concurrent,omitempty"`
}

// QueueSetConcurrencyRequest is the payload for the queue-set-concurrency
// JSON-RPC method. N must be >= 1.
//
// Bead ref: hk-ohiaf.
type QueueSetConcurrencyRequest struct {
	// N is the new concurrency ceiling (must be >= 1).
	N int `json:"n"`
}

// QueueSetConcurrencyResponse is the response for queue-set-concurrency.
//
// Bead ref: hk-ohiaf, hk-vfeeo.
type QueueSetConcurrencyResponse struct {
	// OldN is the previous concurrency ceiling.
	OldN int `json:"old_n"`
	// NewN is the new concurrency ceiling (echoes the request N).
	NewN int `json:"new_n"`
	// SpawnCap is the substrate's non-terminal session ceiling (hk-vfeeo).
	// 0 means uncapped or the daemon has no cap configured. The safe
	// max_concurrent ceiling is SpawnCap/2 (each bead uses 2 sessions).
	// Raise SpawnCap by restarting with a higher --max-concurrent value.
	SpawnCap int `json:"spawn_cap,omitempty"`
}

// WorkerSetEnabledRequest is the payload for the worker-set-enabled JSON-RPC
// method — the live operator toggle behind `harmonik worker enable/disable`
// (hk-xjbvi). It flips the named worker's Enabled flag in the daemon's LIVE
// worker registry so remote dispatch can be turned on/off without a restart.
//
// Bead ref: hk-xjbvi.
type WorkerSetEnabledRequest struct {
	// Name selects which configured worker to toggle (must match a worker in
	// .harmonik/workers.yaml; v1 allows a single worker).
	Name string `json:"name"`
	// Enabled is the new state: true for `worker enable`, false for `worker disable`.
	Enabled bool `json:"enabled"`
}

// WorkerSetEnabledResponse is the response for worker-set-enabled.
//
// Bead ref: hk-xjbvi.
type WorkerSetEnabledResponse struct {
	// Name echoes the resolved worker name the daemon toggled.
	Name string `json:"name"`
	// Enabled is the worker's new Enabled state (echoes the request).
	Enabled bool `json:"enabled"`
}

// QueueDryRunRequest is the payload for the queue-dry-run JSON-RPC method
// (specs/queue-model.md §2.10 RECORD QueueDryRunRequest).
//
// The shape mirrors [QueueSubmitRequest]; the method name differs.
// The daemon routes the request through the full validation pipeline without
// persisting state or emitting events.
type QueueDryRunRequest struct {
	// Groups is one or more group definitions; field schemas per §2.3.
	Groups []Group `json:"groups"`

	// SchemaVersion MUST equal 1; forward-incompatible value refuses per QM-002.
	SchemaVersion int `json:"schema_version"`

	// Name is the durable routing key for the queue to validate against. When
	// absent or empty it defaults to QueueNameMain ("main"). The per-name
	// single-active guard (QM-027) is evaluated against this name so that a
	// dry-run targeting a non-main named queue does not falsely collide with an
	// active "main" queue (NQ-A1).
	//
	// Bead ref: hk-40r9b.
	Name string `json:"name,omitempty"`
}

// LedgerDepNotice records a single would-be deferred-for-ledger-dep item
// discovered during a dry-run (specs/queue-model.md §2.10).
type LedgerDepNotice struct {
	// BeadID is the bead that would start deferred.
	BeadID core.BeadID `json:"bead_id"`

	// BlockerBeadID is the open blocks-edge bead per QM-025.
	BlockerBeadID core.BeadID `json:"blocker_bead_id"`
}

// QueueDryRunResponse is the response payload for queue-dry-run on validation
// success (specs/queue-model.md §2.10 RECORD QueueDryRunResponse).
//
// On validation failure the dry-run returns the same typed JSON-RPC error as
// queue-submit would (§6.11a).
type QueueDryRunResponse struct {
	// ResolvedQueue is the would-be Queue envelope as it would exist post-submit.
	ResolvedQueue Queue `json:"resolved_queue"`

	// LedgerDepNotices lists items that would start in deferred-for-ledger-dep
	// per QM-025.
	LedgerDepNotices []LedgerDepNotice `json:"ledger_dep_notices"`

	// ParallelismNarrowed is true when LedgerDepNotices is non-empty.
	ParallelismNarrowed bool `json:"parallelism_narrowed"`
}
