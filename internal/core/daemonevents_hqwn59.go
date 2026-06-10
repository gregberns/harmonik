package core

import "github.com/google/uuid"

// daemonevents_hqwn59.go — event-bus payload types for §8.7 operator-control and
// daemon lifecycle events:
//   - daemon_started                  (§8.7.1)
//   - daemon_ready                    (§8.7.2)
//   - daemon_shutdown                 (§8.7.3)
//   - daemon_startup_failed           (§8.7.4)
//   - daemon_degraded                 (§8.7.5)
//   - operator_pause_status           (§8.7.6)
//   - operator_resuming               (§8.7.7)
//   - operator_stopped                (§8.7.8)
//   - operator_upgrading              (§8.7.9)
//   - operator_upgrade_completed      (§8.7.10)
//   - operator_upgrade_rejected       (§8.7.11)
//   - operator_command_rejected       (§8.7.12)
//   - dispatch_deferred               (§8.7.13)
//   - daemon_orphan_sweep_completed   (§8.7.14)
//   - infrastructure_unavailable      (§8.7.15)
//   - operator_command_failed         (§8.7.16)
//   - operator_escalation_cleared     (§8.7.17)
//
// Spec ref: specs/event-model.md §8.7, §6.3.
// Bead refs: hk-hqwn.59.57 through hk-hqwn.59.73.

// ---------------------------------------------------------------------------
// Enum types for §8.7 payload discriminators
// ---------------------------------------------------------------------------

// ShutdownMode is the typed discriminator for the `mode` field of daemon_shutdown
// (§8.7.3) and operator_stopped (§8.7.8) payloads.
//
// Values per event-model.md §8.7.3 and §8.7.8.
type ShutdownMode string

const (
	// ShutdownModeGraceful indicates a graceful shutdown (SIGTERM; in-flight runs
	// drained per process-lifecycle.md §6.2).
	ShutdownModeGraceful ShutdownMode = "graceful"

	// ShutdownModeImmediate indicates an immediate shutdown (SIGKILL or forced
	// stop; in-flight runs abandoned).
	ShutdownModeImmediate ShutdownMode = "immediate"
)

// Valid reports whether m is one of the two declared ShutdownMode constants.
func (m ShutdownMode) Valid() bool {
	switch m {
	case ShutdownModeGraceful, ShutdownModeImmediate:
		return true
	default:
		return false
	}
}

// OperatorPauseStatusValue is the typed discriminator for the `status` field of
// operator_pause_status (§8.7.6).
//
// Per §8.9(h): operator_pause_status is a paired-phase lifecycle event (pausing →
// paused). Emitters MUST only emit on status transitions; re-emission with the
// same status for the same scope is forbidden.
type OperatorPauseStatusValue string

const (
	// OperatorPauseStatusValuePausing indicates the pause sequence is in progress
	// (draining in-flight runs per operator-nfr.md §4.3).
	OperatorPauseStatusValuePausing OperatorPauseStatusValue = "pausing"

	// OperatorPauseStatusValuePaused indicates the pause sequence is complete and
	// the daemon is fully paused.
	OperatorPauseStatusValuePaused OperatorPauseStatusValue = "paused"
)

// Valid reports whether v is one of the two declared OperatorPauseStatusValue constants.
func (v OperatorPauseStatusValue) Valid() bool {
	switch v {
	case OperatorPauseStatusValuePausing, OperatorPauseStatusValuePaused:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Payload structs for §8.7 events
// ---------------------------------------------------------------------------

// DaemonStartedPayload is the typed event payload for the daemon_started event
// (event-model.md §8.7.1).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (fsync-boundary — daemon startup landmark; used as
// post-crash-window anchor per EV-023 / process-lifecycle.md §6.2).
//
// Emitted by daemon-core on daemon startup, before reconciliation begins.
// The started_at event is the post-crash-window landmark per EV-023: the
// most recent daemon_started event_id marks the start of the current daemon
// cycle.
//
// # Payload fields (event-model.md §8.7.1)
//
//   - started_at          — RFC 3339 wall-clock timestamp at startup emission
//   - pid                 — OS process ID of the daemon
//   - binary_commit_hash  — git commit hash of the running binary
type DaemonStartedPayload struct {
	// StartedAt is the RFC 3339 wall-clock timestamp at startup. Required (non-empty).
	StartedAt string `json:"started_at"`

	// PID is the OS process ID of the daemon process. Required (must be > 0).
	PID int `json:"pid"`

	// BinaryCommitHash is the git commit hash of the running daemon binary.
	// Required (non-empty). Used for crash-recovery corroboration.
	//
	// TODO(hk-hqwn.71): hoist to typed CommitHash alias when that type lands.
	BinaryCommitHash string `json:"binary_commit_hash"`
}

// Valid reports whether p is a well-formed DaemonStartedPayload.
//
// Rules per event-model.md §8.7.1:
//   - StartedAt must be non-empty.
//   - PID must be > 0.
//   - BinaryCommitHash must be non-empty.
func (p DaemonStartedPayload) Valid() bool {
	if p.StartedAt == "" {
		return false
	}
	if p.PID <= 0 {
		return false
	}
	if p.BinaryCommitHash == "" {
		return false
	}
	return true
}

// DaemonReadyPayload is the typed event payload for the daemon_ready event
// (event-model.md §8.7.2; §6.3 daemon_ready block).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (fsync-boundary — RTO measurement endpoint per
// operator-nfr.md §4.8 ON-033; process-lifecycle.md §6.2 §8.2).
//
// Emitted by daemon-core when §PL-009 readiness criteria are met and the
// daemon transitions to the `ready` DaemonStatus. The ready_at_ns_since_boot
// field is REQUIRED for ON-033 RTO measurement (monotonic-corrected source).
//
// # Payload fields (event-model.md §8.7.2; §6.3)
//
//   - ready_at                 — RFC 3339 wall-clock at the daemon's ready transition
//   - ready_at_ns_since_boot   — uint64 monotonic clock reading at ready, in ns since boot
//   - investigator_run_ids     — run IDs of any reconciliation investigators dispatched
//     before ready (may be empty)
type DaemonReadyPayload struct {
	// ReadyAt is the RFC 3339 wall-clock timestamp at the daemon's ready transition.
	// Required (non-empty).
	ReadyAt string `json:"ready_at"`

	// ReadyAtNsSinceBoot is the uint64 monotonic clock reading at ready, in
	// nanoseconds since the host's boot epoch. Required (must be > 0).
	//
	// Per §6.3 daemon_ready block: on boot-transition cycles where the monotonic
	// clock resets, the ON-033 RTO computation marks the cycle `rto_undefined`;
	// this field is still emitted (well-defined within a single boot epoch).
	ReadyAtNsSinceBoot uint64 `json:"ready_at_ns_since_boot"`

	// InvestigatorRunIDs are the run IDs of any reconciliation investigators
	// dispatched before the daemon reached ready. May be empty (nil or zero-length
	// slice is valid). Non-nil UUID values must not be uuid.Nil.
	InvestigatorRunIDs []RunID `json:"investigator_run_ids"`
}

// Valid reports whether p is a well-formed DaemonReadyPayload.
//
// Rules per event-model.md §8.7.2 and §6.3:
//   - ReadyAt must be non-empty.
//   - ReadyAtNsSinceBoot must be > 0.
//   - Each InvestigatorRunID must not be uuid.Nil.
func (p DaemonReadyPayload) Valid() bool {
	if p.ReadyAt == "" {
		return false
	}
	if p.ReadyAtNsSinceBoot == 0 {
		return false
	}
	for _, id := range p.InvestigatorRunIDs {
		if uuid.UUID(id) == uuid.Nil {
			return false
		}
	}
	return true
}

// DaemonShutdownPayload is the typed event payload for the daemon_shutdown event
// (event-model.md §8.7.3; §6.3 daemon_shutdown block).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (fsync-boundary — SIGTERM-receipt landmark for ON-033
// RTO reconstruction; resolves OQ-PL-012 per spec §6.3 daemon_shutdown note).
//
// Emitted by daemon-core on graceful shutdown (SIGTERM). SIGKILL terminations
// produce no daemon_shutdown emission (no defer-recover gets to run); ON-033
// marks those RTO cycles `rto_undefined`.
//
// Per §6.3: shutdown_at_ns_since_boot is REQUIRED for graceful shutdowns.
//
// # Payload fields (event-model.md §8.7.3; §6.3)
//
//   - shutdown_at              — RFC 3339 wall-clock at the daemon's shutdown emission
//   - shutdown_at_ns_since_boot — uint64 monotonic clock reading at shutdown, in ns since boot
//   - mode                     — graceful or immediate
type DaemonShutdownPayload struct {
	// ShutdownAt is the RFC 3339 wall-clock timestamp at shutdown. Required (non-empty).
	ShutdownAt string `json:"shutdown_at"`

	// ShutdownAtNsSinceBoot is the uint64 monotonic clock reading at shutdown, in
	// nanoseconds since the host's boot epoch. Required (must be > 0).
	ShutdownAtNsSinceBoot uint64 `json:"shutdown_at_ns_since_boot"`

	// Mode identifies the shutdown mode. Required; must be a valid ShutdownMode constant.
	Mode ShutdownMode `json:"mode"`
}

// Valid reports whether p is a well-formed DaemonShutdownPayload.
//
// Rules per event-model.md §8.7.3 and §6.3:
//   - ShutdownAt must be non-empty.
//   - ShutdownAtNsSinceBoot must be > 0.
//   - Mode must be a valid ShutdownMode constant.
func (p DaemonShutdownPayload) Valid() bool {
	if p.ShutdownAt == "" {
		return false
	}
	if p.ShutdownAtNsSinceBoot == 0 {
		return false
	}
	if !p.Mode.Valid() {
		return false
	}
	return true
}

// DaemonStartupFailedPayload is the typed event payload for the
// daemon_startup_failed event (event-model.md §8.7.4).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (fsync-boundary — operator-observability and audit;
// startup failure is a fatal event that MUST reach JSONL durability).
//
// Emitted by daemon-core when startup fails before reaching the `ready` state.
//
// # Payload fields (event-model.md §8.7.4)
//
//   - failed_at                  — RFC 3339 wall-clock timestamp at failure
//   - exit_code                  — OS exit code the daemon will report on exit
//   - failure_mode               — failure mode string per operator-nfr.md §8
//   - required_migration_release — (optional) migration release the operator
//     must install to resolve the failure; REQUIRED when failure_mode is
//     "queue-format-unsupported" per operator-nfr.md §4.4.ON-016. Empty for
//     failure modes that do not require a migration release.
//
// NOTE: event-model.md §8.7.4 does not yet list required_migration_release.
// operator-nfr.md §4.4.ON-016 is normative for ON-016 ("naming the required
// migration release in the failure event payload"); the event-model table is
// stale and needs a cross-spec amendment. Follow-up: TODO(hk-sx9r.20-ev-patch).
type DaemonStartupFailedPayload struct {
	// FailedAt is the RFC 3339 wall-clock timestamp at startup failure.
	// Required (non-empty).
	FailedAt string `json:"failed_at"`

	// ExitCode is the OS exit code the daemon will report. Required; nonzero
	// for failures (0 is technically valid but unexpected for a startup failure).
	ExitCode int `json:"exit_code"`

	// FailureMode is the failure mode per operator-nfr.md §8.
	// Required (non-empty).
	FailureMode FailureMode `json:"failure_mode"`

	// RequiredMigrationRelease names the harmonik release the operator must
	// install to resolve the version incompatibility. REQUIRED when
	// FailureMode is "queue-format-unsupported" (operator-nfr.md §4.4.ON-016).
	// Empty string for failure modes that do not require a migration release.
	RequiredMigrationRelease string `json:"required_migration_release,omitempty"`
}

// Valid reports whether p is a well-formed DaemonStartupFailedPayload.
//
// Rules per event-model.md §8.7.4:
//   - FailedAt must be non-empty.
//   - FailureMode must be non-empty (any non-empty FailureMode string is valid;
//     the set is open per operator-nfr.md §8).
func (p DaemonStartupFailedPayload) Valid() bool {
	if p.FailedAt == "" {
		return false
	}
	if p.FailureMode == "" {
		return false
	}
	return true
}

// DaemonDegradedPayload is the typed event payload for the daemon_degraded event
// (event-model.md §8.7.5; §6.3 daemon_degraded block).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — operator-observability and audit; degraded
// events do not require fsync-backed durability).
//
// Emitted by daemon-core or the reconciliation subsystem when a degradation
// condition is detected. The `reason` enum is exhaustive; new variants require
// an EV-027 amendment.
//
// # Payload fields (event-model.md §8.7.5; §6.3)
//
//   - detected_at — RFC 3339 wall-clock timestamp at detection
//   - reason      — exhaustive DaemonDegradedReason enum
type DaemonDegradedPayload struct {
	// DetectedAt is the RFC 3339 wall-clock timestamp at which the degraded
	// condition was detected. Required (non-empty).
	DetectedAt string `json:"detected_at"`

	// Reason identifies which degradation condition was detected.
	// Required; must be a valid DaemonDegradedReason constant.
	Reason DaemonDegradedReason `json:"reason"`
}

// Valid reports whether p is a well-formed DaemonDegradedPayload.
//
// Rules per event-model.md §8.7.5 and §6.3:
//   - DetectedAt must be non-empty.
//   - Reason must be a valid DaemonDegradedReason constant.
func (p DaemonDegradedPayload) Valid() bool {
	if p.DetectedAt == "" {
		return false
	}
	if !p.Reason.Valid() {
		return false
	}
	return true
}

// OperatorPauseStatusPayload is the typed event payload for the
// operator_pause_status event (event-model.md §8.7.6; §6.3 operator_pause_status block).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability and audit).
//
// Per §8.9(h): this is a paired-phase lifecycle event (pausing → paused).
// Emitters MUST emit only on status transitions; successive emissions with
// identical status for the same scope are forbidden.
//
// # Payload fields (event-model.md §8.7.6; §6.3)
//
//   - status      — pausing or paused (OperatorPauseStatusValue)
//   - changed_at  — RFC 3339 wall-clock timestamp at the status transition;
//     MUST carry millisecond resolution per §8.9(h)
//   - operator_id — optional operator identifier
type OperatorPauseStatusPayload struct {
	// Status is the current pause lifecycle phase. Required; must be a valid
	// OperatorPauseStatusValue constant.
	Status OperatorPauseStatusValue `json:"status"`

	// ChangedAt is the RFC 3339 wall-clock timestamp at the status transition.
	// Required (non-empty). MUST carry millisecond resolution per §8.9(h).
	ChangedAt string `json:"changed_at"`

	// OperatorID is an optional operator identifier. Corresponds to operator_id?
	// in event-model.md §8.7.6. Nil when no operator context is available.
	//
	// TODO(hk-hqwn.71): hoist to typed OperatorID alias when that type lands.
	OperatorID *string `json:"operator_id,omitempty"`

	// QueueName is the optional named-queue scope for a per-queue pause
	// (NQ-C1 hk-tigaf.6). When non-empty, only the named queue is
	// transitioned; when empty, the pause is global (all queues).
	QueueName string `json:"queue_name,omitempty"`
}

// Valid reports whether p is a well-formed OperatorPauseStatusPayload.
//
// Rules per event-model.md §8.7.6:
//   - Status must be a valid OperatorPauseStatusValue constant.
//   - ChangedAt must be non-empty.
func (p OperatorPauseStatusPayload) Valid() bool {
	if !p.Status.Valid() {
		return false
	}
	if p.ChangedAt == "" {
		return false
	}
	return true
}

// OperatorResumingPayload is the typed event payload for the operator_resuming
// event (event-model.md §8.7.7).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability and audit).
//
// Emitted by daemon-core when the operator issues a resume command and the
// daemon begins the `paused → ready` transition per operator-nfr.md §4.3.
//
// # Payload fields (event-model.md §8.7.7)
//
//   - resumed_at — RFC 3339 wall-clock timestamp at the resume transition
type OperatorResumingPayload struct {
	// ResumedAt is the RFC 3339 wall-clock timestamp at the resume transition.
	// Required (non-empty).
	ResumedAt string `json:"resumed_at"`

	// QueueName is the optional named-queue scope for a per-queue resume
	// (NQ-C1 hk-tigaf.6). When non-empty, only the named queue is
	// transitioned; when empty, the resume is global (all paused-by-drain queues).
	QueueName string `json:"queue_name,omitempty"`
}

// Valid reports whether p is a well-formed OperatorResumingPayload.
//
// Rules per event-model.md §8.7.7:
//   - ResumedAt must be non-empty.
func (p OperatorResumingPayload) Valid() bool {
	return p.ResumedAt != ""
}

// OperatorStoppedPayload is the typed event payload for the operator_stopped
// event (event-model.md §8.7.8).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability and audit).
//
// Emitted by daemon-core when the daemon reaches the `stopped` terminal state.
//
// # Payload fields (event-model.md §8.7.8)
//
//   - stopped_at — RFC 3339 wall-clock timestamp at the stop transition
//   - mode       — graceful or immediate
type OperatorStoppedPayload struct {
	// StoppedAt is the RFC 3339 wall-clock timestamp at the stop transition.
	// Required (non-empty).
	StoppedAt string `json:"stopped_at"`

	// Mode identifies the shutdown mode. Required; must be a valid ShutdownMode constant.
	Mode ShutdownMode `json:"mode"`
}

// Valid reports whether p is a well-formed OperatorStoppedPayload.
//
// Rules per event-model.md §8.7.8:
//   - StoppedAt must be non-empty.
//   - Mode must be a valid ShutdownMode constant.
func (p OperatorStoppedPayload) Valid() bool {
	if p.StoppedAt == "" {
		return false
	}
	if !p.Mode.Valid() {
		return false
	}
	return true
}

// OperatorUpgradingPayload is the typed event payload for the operator_upgrading
// event (event-model.md §8.7.9).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability and audit).
//
// Emitted by daemon-core when an upgrade sequence begins.
//
// # Payload fields (event-model.md §8.7.9)
//
//   - upgrade_version — version string of the target upgrade binary
//   - started_at      — RFC 3339 wall-clock timestamp at upgrade start
type OperatorUpgradingPayload struct {
	// UpgradeVersion is the version string of the target upgrade binary.
	// Required (non-empty).
	//
	// TODO(hk-hqwn.71): hoist to typed UpgradeVersion alias when that type lands.
	UpgradeVersion string `json:"upgrade_version"`

	// StartedAt is the RFC 3339 wall-clock timestamp at which the upgrade
	// sequence started. Required (non-empty).
	StartedAt string `json:"started_at"`
}

// Valid reports whether p is a well-formed OperatorUpgradingPayload.
//
// Rules per event-model.md §8.7.9:
//   - UpgradeVersion must be non-empty.
//   - StartedAt must be non-empty.
func (p OperatorUpgradingPayload) Valid() bool {
	if p.UpgradeVersion == "" {
		return false
	}
	if p.StartedAt == "" {
		return false
	}
	return true
}

// OperatorUpgradeCompletedPayload is the typed event payload for the
// operator_upgrade_completed event (event-model.md §8.7.10).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (fsync-boundary — observability and audit; upgrade
// completion is a version-boundary landmark).
//
// Emitted by daemon-core when the upgrade sequence completes successfully.
//
// # Payload fields (event-model.md §8.7.10)
//
//   - upgrade_version     — version string of the newly running binary
//   - completed_at        — RFC 3339 wall-clock timestamp at upgrade completion
//   - binary_commit_hash  — git commit hash of the newly running binary
type OperatorUpgradeCompletedPayload struct {
	// UpgradeVersion is the version string of the newly running binary.
	// Required (non-empty).
	//
	// TODO(hk-hqwn.71): hoist to typed UpgradeVersion alias when that type lands.
	UpgradeVersion string `json:"upgrade_version"`

	// CompletedAt is the RFC 3339 wall-clock timestamp at upgrade completion.
	// Required (non-empty).
	CompletedAt string `json:"completed_at"`

	// BinaryCommitHash is the git commit hash of the newly running daemon binary.
	// Required (non-empty).
	//
	// TODO(hk-hqwn.71): hoist to typed CommitHash alias when that type lands.
	BinaryCommitHash string `json:"binary_commit_hash"`
}

// Valid reports whether p is a well-formed OperatorUpgradeCompletedPayload.
//
// Rules per event-model.md §8.7.10:
//   - UpgradeVersion must be non-empty.
//   - CompletedAt must be non-empty.
//   - BinaryCommitHash must be non-empty.
func (p OperatorUpgradeCompletedPayload) Valid() bool {
	if p.UpgradeVersion == "" {
		return false
	}
	if p.CompletedAt == "" {
		return false
	}
	if p.BinaryCommitHash == "" {
		return false
	}
	return true
}

// OperatorUpgradeRejectedPayload is the typed event payload for the
// operator_upgrade_rejected event (event-model.md §8.7.11).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — operator-observability and audit).
//
// Emitted by daemon-core when an upgrade attempt is rejected.
//
// # Payload fields (event-model.md §8.7.11)
//
//   - upgrade_version — optional version string of the rejected upgrade binary
//   - rejected_at     — RFC 3339 wall-clock timestamp at rejection
//   - reason          — hash_mismatch / schema_incompatible / not_paused
type OperatorUpgradeRejectedPayload struct {
	// UpgradeVersion is the optional version string of the rejected upgrade binary.
	// Corresponds to upgrade_version? in event-model.md §8.7.11. Nil when the
	// version could not be determined at rejection time.
	//
	// TODO(hk-hqwn.71): hoist to typed UpgradeVersion alias when that type lands.
	UpgradeVersion *string `json:"upgrade_version,omitempty"`

	// RejectedAt is the RFC 3339 wall-clock timestamp at rejection.
	// Required (non-empty).
	RejectedAt string `json:"rejected_at"`

	// Reason identifies why the upgrade was rejected.
	// Required; must be a valid OperatorUpgradeRejectedReason constant.
	Reason OperatorUpgradeRejectedReason `json:"reason"`
}

// Valid reports whether p is a well-formed OperatorUpgradeRejectedPayload.
//
// Rules per event-model.md §8.7.11:
//   - RejectedAt must be non-empty.
//   - Reason must be a valid OperatorUpgradeRejectedReason constant.
func (p OperatorUpgradeRejectedPayload) Valid() bool {
	if p.RejectedAt == "" {
		return false
	}
	if !p.Reason.Valid() {
		return false
	}
	return true
}

// OperatorCommandRejectedPayload is the typed event payload for the
// operator_command_rejected event (event-model.md §8.7.12).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — operator-observability and audit).
//
// Emitted by daemon-core when an operator command is rejected because the
// daemon is not in a state that accepts the command.
//
// # Payload fields (event-model.md §8.7.12)
//
//   - command       — the rejected operator command
//   - current_state — current DaemonStatus at rejection time
//   - rejected_at   — RFC 3339 wall-clock timestamp at rejection
type OperatorCommandRejectedPayload struct {
	// Command is the operator command that was rejected.
	// Required; must be a valid OperatorCommand constant.
	Command OperatorCommand `json:"command"`

	// CurrentState is the DaemonStatus at the time the command was rejected.
	// Required; must be a valid DaemonStatus constant.
	CurrentState DaemonStatus `json:"current_state"`

	// RejectedAt is the RFC 3339 wall-clock timestamp at rejection.
	// Required (non-empty).
	RejectedAt string `json:"rejected_at"`
}

// Valid reports whether p is a well-formed OperatorCommandRejectedPayload.
//
// Rules per event-model.md §8.7.12:
//   - Command must be a valid OperatorCommand constant.
//   - CurrentState must be a valid DaemonStatus constant.
//   - RejectedAt must be non-empty.
func (p OperatorCommandRejectedPayload) Valid() bool {
	if !p.Command.Valid() {
		return false
	}
	if !p.CurrentState.Valid() {
		return false
	}
	if p.RejectedAt == "" {
		return false
	}
	return true
}

// DispatchDeferredPayload is the typed event payload for the dispatch_deferred
// event (event-model.md §8.7.13).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability and audit; dispatch deferral is
// non-fatal and may occur frequently under high concurrency).
//
// Emitted by daemon-core when a node dispatch is deferred due to resource
// constraints (e.g., machine concurrency ceiling exhausted).
//
// # Payload fields (event-model.md §8.7.13)
//
//   - run_id     — optional run ID of the deferred dispatch
//   - node_id    — optional node ID of the deferred dispatch
//   - reason     — machine_ceiling_exhausted or other string reason
//   - deferred_at — RFC 3339 wall-clock timestamp at deferral
type DispatchDeferredPayload struct {
	// RunID is the optional run ID of the deferred dispatch. Corresponds to
	// run_id? in event-model.md §8.7.13. Nil when no run context is available.
	// Non-nil must not be uuid.Nil.
	RunID *RunID `json:"run_id,omitempty"`

	// NodeID is the optional node ID of the deferred dispatch. Corresponds to
	// node_id? in event-model.md §8.7.13. Nil when no node context is available.
	// Non-nil must be non-empty.
	NodeID *NodeID `json:"node_id,omitempty"`

	// Reason is the deferral reason. Required (non-empty). The canonical value is
	// DispatchDeferredReasonMachineCeilingExhausted; other strings are accepted
	// per event-model.md §8.7.13 (`machine_ceiling_exhausted` / other).
	Reason DispatchDeferredReason `json:"reason"`

	// DeferredAt is the RFC 3339 wall-clock timestamp at deferral.
	// Required (non-empty).
	DeferredAt string `json:"deferred_at"`
}

// Valid reports whether p is a well-formed DispatchDeferredPayload.
//
// Rules per event-model.md §8.7.13:
//   - RunID, when non-nil, must not be uuid.Nil.
//   - NodeID, when non-nil, must not be empty.
//   - Reason must be non-empty.
//   - DeferredAt must be non-empty.
func (p DispatchDeferredPayload) Valid() bool {
	if p.RunID != nil && uuid.UUID(*p.RunID) == uuid.Nil {
		return false
	}
	if p.NodeID != nil && *p.NodeID == "" {
		return false
	}
	if p.Reason == "" {
		return false
	}
	if p.DeferredAt == "" {
		return false
	}
	return true
}

// DaemonOrphanSweepCompletedPayload is the typed event payload for the
// daemon_orphan_sweep_completed event (event-model.md §8.7.14).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability and audit; orphan sweep is a
// startup housekeeping operation).
//
// Emitted by daemon-core after the orphan sweep phase of daemon startup
// completes per process-lifecycle.md §6.1.
//
// # Payload fields (event-model.md §8.7.14)
//
//   - tmux_sessions_killed         — number of orphan tmux sessions killed
//   - locks_cleared                — number of stale worktree lease-locks cleared
//   - subprocesses_killed          — number of orphan handler subprocesses killed
//   - br_subprocesses_killed       — number of orphan br subprocesses killed (OQ-BI-010)
//   - reconciliation_locks_removed — number of stale reconciliation lock files removed
//   - stale_intents_observed       — count of stale intent files left for RC Cat 3a
//   - swept_at                     — RFC 3339 wall-clock timestamp at sweep completion
type DaemonOrphanSweepCompletedPayload struct {
	// TmuxSessionsKilled is the number of orphan tmux sessions killed during the
	// sweep. Required (must be >= 0).
	TmuxSessionsKilled int `json:"tmux_sessions_killed"`

	// TmuxWindowsKilled is the number of orphan tmux windows killed during the
	// sweep. These are windows inside operator-owned sessions whose name begins
	// with "hk-<hash6>-" for this project's hash. Required (must be >= 0).
	//
	// Spec ref: process-lifecycle.md §4.7 PL-021c — "The daemon_orphan_sweep_completed
	// event payload MUST gain a new field tmux_windows_killed: <integer >= 0>."
	TmuxWindowsKilled int `json:"tmux_windows_killed"`

	// LocksCleared is the number of stale worktree lease-lock files cleared
	// during the sweep. Required (must be >= 0).
	LocksCleared int `json:"locks_cleared"`

	// SubprocessesKilled is the number of orphan handler subprocesses killed
	// during the sweep. Required (must be >= 0).
	SubprocessesKilled int `json:"subprocesses_killed"`

	// BrSubprocessesKilled is the number of orphan br (Beads CLI) subprocesses
	// killed during the sweep per BI-029 / OQ-BI-010. Required (must be >= 0).
	BrSubprocessesKilled int `json:"br_subprocesses_killed"`

	// ReconciliationLocksRemoved is the number of stale reconciliation lock
	// files removed from .harmonik/reconciliation-locks/ during the sweep.
	// Required (must be >= 0).
	ReconciliationLocksRemoved int `json:"reconciliation_locks_removed"`

	// StaleIntentsObserved is the count of stale intent files retained on disk
	// for the reconciliation Cat 3a detector (RC-013) — i.e. files where the
	// bead has NOT yet reached its IntendedPostState. When IntentGCLedger is
	// not wired, this is the raw count of all stale files (legacy behavior).
	// Required (must be >= 0).
	StaleIntentsObserved int `json:"stale_intents_observed"`

	// IntentsGCd is the count of stale intent files removed during this sweep
	// because the target bead had already reached its IntendedPostState (op
	// landed in a prior run; file was a BI-030 step-6 leftover). Zero when
	// IntentGCLedger is not wired. Required (must be >= 0).
	//
	// Bead ref: hk-cizvu — stale_intents_observed GC fix.
	IntentsGCd int `json:"intents_gc_d"`

	// BeadInProgressReset is the count of stale `in_progress` beads reset to
	// `open` by the PL-006 sixth-bullet bead-reset sweep (BI-010d). Required
	// (must be >= 0).
	//
	// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet — "the daemon
	// MUST emit `daemon_orphan_sweep_completed` ... with ... `bead_in_progress_reset`
	// (count of `in_progress` beads reset to `open` by this sweep)."
	// Cross-spec note: PL-006 sixth bullet declares this field as an additive
	// payload extension to §8.7.14, consistent with the `tmux_windows_killed`
	// precedent (PL-021c). EV §8.7.14 schema-bump confirmation is tracked as
	// hk-iuaed.5.
	BeadInProgressReset int `json:"bead_in_progress_reset"`

	// BeadCat3cClosed is the count of subsumed `in_progress` beads auto-closed
	// by the Cat 3c auto-reconciler (hk-lgtq2): beads whose implementation has
	// already merged to the target branch (Harmonik-Bead-ID trailer present) but
	// were still marked `in_progress`. Required (must be >= 0).
	//
	// Spec ref: specs/reconciliation/spec.md §8.6 Cat 3c — "inverse premature-close".
	// Bead ref: hk-lgtq2.
	BeadCat3cClosed int `json:"bead_cat3c_closed"`

	// CoordinatorSessionsSkipped is the count of coordinator (flywheel) tmux
	// sessions that were excluded from the orphan-kill pass because the
	// supervisor sentinel was present AND the supervisor PID was live (PL-006d).
	// Required (must be >= 0).
	//
	// Spec ref: process-lifecycle.md §4.2 PL-006d — sentinel-present+PID-live →
	// SKIP with structured-log orphan_sweep_skipped_coordinator_session.
	// Bead ref: hk-9eury.
	CoordinatorSessionsSkipped int `json:"coordinator_sessions_skipped"`

	// SweptAt is the RFC 3339 wall-clock timestamp at sweep completion.
	// Required (non-empty).
	SweptAt string `json:"swept_at"`
}

// Valid reports whether p is a well-formed DaemonOrphanSweepCompletedPayload.
//
// Rules per event-model.md §8.7.14:
//   - TmuxSessionsKilled must be >= 0.
//   - TmuxWindowsKilled must be >= 0.
//   - LocksCleared must be >= 0.
//   - SubprocessesKilled must be >= 0.
//   - BrSubprocessesKilled must be >= 0.
//   - ReconciliationLocksRemoved must be >= 0.
//   - StaleIntentsObserved must be >= 0.
//   - SweptAt must be non-empty.
func (p DaemonOrphanSweepCompletedPayload) Valid() bool {
	if p.TmuxSessionsKilled < 0 {
		return false
	}
	if p.TmuxWindowsKilled < 0 {
		return false
	}
	if p.LocksCleared < 0 {
		return false
	}
	if p.SubprocessesKilled < 0 {
		return false
	}
	if p.BrSubprocessesKilled < 0 {
		return false
	}
	if p.ReconciliationLocksRemoved < 0 {
		return false
	}
	if p.StaleIntentsObserved < 0 {
		return false
	}
	if p.BeadInProgressReset < 0 {
		return false
	}
	if p.BeadCat3cClosed < 0 {
		return false
	}
	if p.SweptAt == "" {
		return false
	}
	return true
}

// InfrastructureUnavailablePayload is the typed event payload for the
// infrastructure_unavailable event (event-model.md §8.7.15; §6.3
// infrastructure_unavailable block).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — operator-observability and audit).
//
// Emitted by daemon-core when a required infrastructure prerequisite is
// unavailable. May be emitted multiple times with increasing retry_count.
//
// # Payload fields (event-model.md §8.7.15; §6.3)
//
//   - failed_prerequisite — which prerequisite is unavailable (InfrastructurePrerequisite enum)
//   - detail_string        — human-readable detail string for operator diagnosis
//   - retry_count          — number of retries attempted so far (>= 0)
type InfrastructureUnavailablePayload struct {
	// FailedPrerequisite identifies which infrastructure prerequisite is unavailable.
	// Required; must be a valid InfrastructurePrerequisite constant.
	FailedPrerequisite InfrastructurePrerequisite `json:"failed_prerequisite"`

	// DetailString is a human-readable detail string for operator diagnosis.
	// Required (non-empty).
	DetailString string `json:"detail_string"`

	// RetryCount is the number of retries attempted so far. Required (must be >= 0).
	RetryCount int `json:"retry_count"`
}

// Valid reports whether p is a well-formed InfrastructureUnavailablePayload.
//
// Rules per event-model.md §8.7.15 and §6.3:
//   - FailedPrerequisite must be a valid InfrastructurePrerequisite constant.
//   - DetailString must be non-empty.
//   - RetryCount must be >= 0.
func (p InfrastructureUnavailablePayload) Valid() bool {
	if !p.FailedPrerequisite.Valid() {
		return false
	}
	if p.DetailString == "" {
		return false
	}
	if p.RetryCount < 0 {
		return false
	}
	return true
}

// OperatorCommandFailedPayload is the typed event payload for the
// operator_command_failed event (event-model.md §8.7.16; ON-013a).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — operator-observability and audit).
//
// Emitted by the ON-013a panic-barrier when an operator command panics. The
// emitter is operator-nfr.md §4.4 ON-013a. Per §6.5, this event is
// ON-emission-owned.
//
// # Payload fields (event-model.md §8.7.16)
//
//   - command       — the operator command that failed (enum per §8.7.12 / §8.7.16)
//   - failure_class — FailureClass enum per §6.3
//   - run_id        — optional run ID if the failure occurred in a run context
//   - failed_at     — RFC 3339 wall-clock timestamp at failure
type OperatorCommandFailedPayload struct {
	// Command is the operator command that failed.
	// Required; must be a valid OperatorCommand constant.
	Command OperatorCommand `json:"command"`

	// FailureClass is the failure class per FailureClass enum (§6.3).
	// Required; must be a valid FailureClass constant.
	FailureClass FailureClass `json:"failure_class"`

	// RunID is the optional run ID if the failure occurred in a run context.
	// Corresponds to run_id? in event-model.md §8.7.16. Nil when no run context.
	// Non-nil must not be uuid.Nil.
	RunID *RunID `json:"run_id,omitempty"`

	// FailedAt is the RFC 3339 wall-clock timestamp at failure.
	// Required (non-empty).
	FailedAt string `json:"failed_at"`
}

// Valid reports whether p is a well-formed OperatorCommandFailedPayload.
//
// Rules per event-model.md §8.7.16:
//   - Command must be a valid OperatorCommand constant.
//   - FailureClass must be a valid FailureClass constant.
//   - RunID, when non-nil, must not be uuid.Nil.
//   - FailedAt must be non-empty.
func (p OperatorCommandFailedPayload) Valid() bool {
	if !p.Command.Valid() {
		return false
	}
	if !p.FailureClass.Valid() {
		return false
	}
	if p.RunID != nil && uuid.UUID(*p.RunID) == uuid.Nil {
		return false
	}
	if p.FailedAt == "" {
		return false
	}
	return true
}

// OperatorEscalationClearedPayload is the typed event payload for the
// operator_escalation_cleared event (event-model.md §8.7.17).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability and audit).
//
// Emitted by daemon-core (ON-emission-owned per §6.5) as the companion event
// to RC-emitted operator_escalation_required (§8.6.9). Signals that an
// operator escalation has been resolved.
//
// # Payload fields (event-model.md §8.7.17)
//
//   - target_run_id    — optional run ID of the run whose escalation was cleared
//   - cleared_at       — RFC 3339 wall-clock timestamp at clearance
//   - clearance_reason — verdict_executed / manual_clear / superseded
type OperatorEscalationClearedPayload struct {
	// TargetRunID is the optional run ID of the run whose escalation was cleared.
	// Corresponds to target_run_id? in event-model.md §8.7.17. Nil when the
	// escalation was not scoped to a specific run. Non-nil must not be uuid.Nil.
	TargetRunID *RunID `json:"target_run_id,omitempty"`

	// ClearedAt is the RFC 3339 wall-clock timestamp at clearance.
	// Required (non-empty).
	ClearedAt string `json:"cleared_at"`

	// ClearanceReason identifies why the escalation was cleared.
	// Required; must be a valid ClearanceReason constant.
	ClearanceReason ClearanceReason `json:"clearance_reason"`
}

// Valid reports whether p is a well-formed OperatorEscalationClearedPayload.
//
// Rules per event-model.md §8.7.17:
//   - TargetRunID, when non-nil, must not be uuid.Nil.
//   - ClearedAt must be non-empty.
//   - ClearanceReason must be a valid ClearanceReason constant.
func (p OperatorEscalationClearedPayload) Valid() bool {
	if p.TargetRunID != nil && uuid.UUID(*p.TargetRunID) == uuid.Nil {
		return false
	}
	if p.ClearedAt == "" {
		return false
	}
	if !p.ClearanceReason.Valid() {
		return false
	}
	return true
}

// DaemonConfigPayload is the typed event payload for the daemon_config event
// (event-model.md §8.7.18).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — operator-observability; states the resolved
// merge-target and active branch-protection policy at startup).
//
// Emitted by daemon-core after boot-time config validation passes and before the
// socket is bound. Records the effective runtime configuration so operators can
// confirm what the daemon resolved from flags.
//
// # Payload fields (event-model.md §8.7.18)
//
//   - target_branch      — resolved merge target branch (never empty)
//   - protect_branches   — list of branch names the daemon must never merge into
//   - forbid_unprotected_default — whether --forbid-default-main is active
//
// Bead ref: hk-sul12.
type DaemonConfigPayload struct {
	// TargetBranch is the resolved merge target branch. Required (non-empty).
	// This is the value after resolveTargetBranch() normalisation: when the
	// operator passed no --target-branch flag, this is "main".
	TargetBranch string `json:"target_branch"`

	// ProtectBranches is the operator-supplied list of protected branch names.
	// May be nil or empty when no branches are protected.
	ProtectBranches []string `json:"protect_branches,omitempty"`

	// ForbidUnprotectedDefault mirrors Config.ForbidUnprotectedDefault: when
	// true the daemon rejected any configuration where TargetBranch was empty
	// (i.e., would have defaulted to "main" without an explicit --target-branch).
	ForbidUnprotectedDefault bool `json:"forbid_unprotected_default"`
}

// Valid reports whether p is a well-formed DaemonConfigPayload.
//
// Rules per event-model.md §8.7.18:
//   - TargetBranch must be non-empty.
func (p DaemonConfigPayload) Valid() bool {
	return p.TargetBranch != ""
}
