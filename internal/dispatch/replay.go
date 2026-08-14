package dispatch

import (
	"errors"
	"fmt"
)

// QueueFact is the exact queue-item state observed for one intent binding.
type QueueFact string

const (
	// QueueOfferable through QueueConflict are the admitted queue observations.
	QueueOfferable QueueFact = "offerable"
	// QueueReserved means the exact item carries this run ID.
	QueueReserved QueueFact = "reserved"
	// QueueTerminalSuccess means the queue consumed a successful outcome.
	QueueTerminalSuccess QueueFact = "terminal_success"
	// QueueTerminalRetryable means a failed item was followed by a durable reopen.
	QueueTerminalRetryable QueueFact = "terminal_retryable"
	// QueueTerminalUnreopened means failure was consumed without a Beads write.
	QueueTerminalUnreopened QueueFact = "terminal_unreopened"
	// QueueConflict means queue identity or state cannot be classified.
	QueueConflict QueueFact = "conflict"
)

// BeadFact is the coarse ledger fact needed before run outcome recovery.
type BeadFact string

const (
	// BeadOpen through BeadConflict are the admitted ledger observations.
	BeadOpen BeadFact = "open"
	// BeadInProgress means the ledger shows an active claim.
	BeadInProgress BeadFact = "in_progress"
	// BeadClosed means the ledger reports terminal completion.
	BeadClosed BeadFact = "closed"
	// BeadOther is a supported non-open, non-terminal status.
	BeadOther BeadFact = "other"
	// BeadConflict means ledger ownership cannot be classified.
	BeadConflict BeadFact = "conflict"
)

// RunRecordFact is the validated universal record phase.
type RunRecordFact string

const (
	// RunRecordAbsent through RunRecordConflict are the admitted record observations.
	RunRecordAbsent RunRecordFact = "absent"
	// RunRecordBase has identity but no placement.
	RunRecordBase RunRecordFact = "base"
	// RunRecordLocated has a durable execution location.
	RunRecordLocated RunRecordFact = "located"
	// RunRecordSession has a durable handoff identity.
	RunRecordSession RunRecordFact = "session_bound"
	// RunRecordConflict means record identity or bytes cannot be classified.
	RunRecordConflict RunRecordFact = "conflict"
)

// WorktreeFact is the exact lease state for the dispatch run.
type WorktreeFact string

const (
	// WorktreeAbsent through WorktreeConflict are the admitted lease observations.
	WorktreeAbsent WorktreeFact = "absent"
	// WorktreePrepared means the exact registered worktree matches the durable
	// branch and parent commit and has no later session or lease facts.
	WorktreePrepared WorktreeFact = "prepared"
	// WorktreeLeased means the exact run owns the lease.
	WorktreeLeased WorktreeFact = "leased"
	// WorktreeConflict means lease ownership cannot be classified.
	WorktreeConflict WorktreeFact = "conflict"
)

// SessionFact is the exact state of the bound handoff session.
type SessionFact string

const (
	// SessionAbsent through SessionConflict are the admitted session observations.
	SessionAbsent SessionFact = "absent"
	// SessionLive means the exact bound session is running.
	SessionLive SessionFact = "live"
	// SessionDead means the exact bound session is no longer running.
	SessionDead SessionFact = "dead"
	// SessionConflict means session ownership cannot be classified.
	SessionConflict SessionFact = "conflict"
)

// SessionReceiptFact is the durable start acknowledgement for one exact target.
type SessionReceiptFact string

const (
	// SessionReceiptAbsent means no receipt exists for the run.
	SessionReceiptAbsent SessionReceiptFact = "absent"
	// SessionReceiptExact means the receipt matches the intent and run record.
	SessionReceiptExact SessionReceiptFact = "exact"
	// SessionReceiptConflict means receipt identity cannot be classified.
	SessionReceiptConflict SessionReceiptFact = "conflict"
)

// GitFact is the exact completion evidence for a closed bead.
type GitFact string

const (
	// GitAbsent means matching completion evidence is absent.
	GitAbsent GitFact = "absent"
	// GitMatching means immutable completion evidence matches this dispatch.
	GitMatching GitFact = "matching"
	// GitConflict means completion evidence cannot be classified.
	GitConflict GitFact = "conflict"
)

// ClaimFact classifies an attempted claim while the intent is prepared.
type ClaimFact string

const (
	// ClaimNone means no claim result is durable.
	ClaimNone ClaimFact = "none"
	// ClaimMatching means the exact claim transition took effect.
	ClaimMatching ClaimFact = "matching"
	// ClaimDependencyRefusal is the typed dependency refusal.
	ClaimDependencyRefusal ClaimFact = "dependency_refusal"
	// ClaimAlreadyAssigned is a typed refusal with a different owner.
	ClaimAlreadyAssigned ClaimFact = "already_assigned"
	// ClaimExternalRefusal is another definite refusal with no admitted claim.
	ClaimExternalRefusal ClaimFact = "external_refusal"
	// ClaimConflict means claim ownership or result cannot be classified.
	ClaimConflict ClaimFact = "conflict"
)

// PreclaimFact is one joined queue fact for pre-claim compensation.
type PreclaimFact string

const (
	// PreclaimAbsent means no pre-claim compensation fact exists.
	PreclaimAbsent PreclaimFact = "absent"
	// PreclaimMaxAttemptsItemTerminal means the exact failed item needs group finalization.
	PreclaimMaxAttemptsItemTerminal PreclaimFact = "max_attempts_item_terminal"
	// PreclaimMaxAttemptsGroupDurable means max-attempt group finalization is durable.
	PreclaimMaxAttemptsGroupDurable PreclaimFact = "max_attempts_group_durable"
	// PreclaimCrossQueueItemTerminal means the exact duplicate item needs group finalization.
	PreclaimCrossQueueItemTerminal PreclaimFact = "cross_queue_item_terminal"
	// PreclaimCrossQueueGroupDurable means duplicate group finalization is durable.
	PreclaimCrossQueueGroupDurable PreclaimFact = "cross_queue_group_durable"
	// PreclaimDependencyItemTerminal means the exact refused item needs group finalization.
	PreclaimDependencyItemTerminal PreclaimFact = "dependency_item_terminal"
	// PreclaimDependencyGroupDurable means dependency-refusal group finalization is durable.
	PreclaimDependencyGroupDurable PreclaimFact = "dependency_group_durable"
	// PreclaimReleased means the exact reservation is durably pending again.
	PreclaimReleased PreclaimFact = "released"
	// PreclaimConflict means joined queue facts do not match the intent.
	PreclaimConflict PreclaimFact = "conflict"
)

// ReplayAction is the one next transition startup may execute.
type ReplayAction string

const (
	// ReplayReservation through ReplayRepairRequired are closed restart actions.
	ReplayReservation ReplayAction = "replay-reservation"
	// ReplayClaim repeats the exact claim transition.
	ReplayClaim ReplayAction = "replay-claim"
	// AdvanceClaimRefusal makes a typed refusal durable before compensation.
	AdvanceClaimRefusal ReplayAction = "advance-claim-refusal"
	// FailQueueItem consumes a typed dependency refusal.
	FailQueueItem ReplayAction = "fail-item"
	// ReleaseReservation gives back a definitely unclaimed reservation.
	ReleaseReservation ReplayAction = "release"
	// AdvanceClaim records a claim already visible in Beads.
	AdvanceClaim ReplayAction = "advance-claim"
	// WriteRunRecord writes the exact base universal record.
	WriteRunRecord ReplayAction = "write-run-record"
	// AdvanceRunPhase records an exact existing universal record.
	AdvanceRunPhase ReplayAction = "advance-run-phase"
	// ResumeProvision resumes location or worktree preparation.
	ResumeProvision ReplayAction = "resume-provision"
	// PrepareHandoff binds the exact session identity.
	PrepareHandoff ReplayAction = "prepare-handoff"
	// AdvanceHandoffPhase records an existing handoff binding.
	AdvanceHandoffPhase ReplayAction = "advance-handoff-phase"
	// ReplaySessionStart starts the exact durable handoff.
	ReplaySessionStart ReplayAction = "replay-session-start"
	// RemoveDeadUnstartedTarget removes an exact dead target that has no receipt.
	RemoveDeadUnstartedTarget ReplayAction = "remove-dead-unstarted-target"
	// AdoptLive adopts the exact live session.
	AdoptLive ReplayAction = "adopt-live"
	// ResumeDead resumes a stopped run with no durable outcome.
	ResumeDead ReplayAction = "resume-dead"
	// AdvanceRunOutcome consumes an existing durable outcome.
	AdvanceRunOutcome ReplayAction = "advance-run-outcome"
	// AdvanceClose continues from a closed bead.
	AdvanceClose ReplayAction = "advance-close"
	// ReplayCleanupOnly removes residue after queue terminal application.
	ReplayCleanupOnly ReplayAction = "cleanup-only"
	// FinalizePreclaimGroup applies the exact failed-item group decision.
	FinalizePreclaimGroup ReplayAction = "finalize-preclaim-group"
	// RemoveDispatchIntent removes an exact finalized pre-claim intent.
	RemoveDispatchIntent ReplayAction = "remove-dispatch-intent"
	// ReplayRepairRequired preserves evidence for an invalid combination.
	ReplayRepairRequired ReplayAction = "repair-required"
)

// ReplayFacts are validated observations for one durable intent.
type ReplayFacts struct {
	IntentPhase       Phase
	Queue             QueueFact
	Bead              BeadFact
	RunRecord         RunRecordFact
	Worktree          WorktreeFact
	Session           SessionFact
	SessionReceipt    SessionReceiptFact
	Git               GitFact
	Claim             ClaimFact
	RefusalCause      ClaimRefusalCause
	Preclaim          PreclaimFact
	RunOutcomeDurable bool
}

// DecideReplay returns one restart action without performing effects.
func DecideReplay(f ReplayFacts) (ReplayAction, error) {
	if err := f.validate(); err != nil {
		return "", err
	}
	if f.hasConflict() {
		return ReplayRepairRequired, nil
	}
	if !f.receiptPhaseCoherent() {
		return ReplayRepairRequired, nil
	}
	if action, handled := decidePreclaimReplay(f); handled {
		return action, nil
	}
	if action, terminal := decideTerminalReplay(f); terminal {
		return action, nil
	}
	if f.Bead == BeadClosed {
		if f.Git == GitMatching && f.closedResidueCoherent() {
			return AdvanceClose, nil
		}
		return ReplayRepairRequired, nil
	}
	if !f.coherent() {
		return ReplayRepairRequired, nil
	}
	switch f.IntentPhase {
	case PhasePrepared:
		return decidePreparedReplay(f), nil
	case PhaseClaimDurable:
		return decideClaimReplay(f), nil
	case PhaseRunDurable:
		return decideRunReplay(f), nil
	case PhaseHandoffDurable:
		return decideHandoffReplay(f), nil
	default:
		return "", fmt.Errorf("dispatch: invalid replay phase %q", f.IntentPhase)
	}
}

func decidePreparedReplay(f ReplayFacts) ReplayAction {
	if action, handled := decideClaimRefusal(f); handled {
		return action
	}
	if f.Queue == QueueOfferable && f.Bead == BeadOpen && f.RunRecord == RunRecordAbsent {
		return ReplayReservation
	}
	if f.Queue == QueueReserved && f.Bead == BeadOpen && f.RunRecord == RunRecordAbsent {
		return ReplayClaim
	}
	if f.Queue == QueueReserved && f.Bead == BeadInProgress && f.RunRecord == RunRecordAbsent && f.Claim == ClaimMatching {
		return AdvanceClaim
	}
	return ReplayRepairRequired
}

func decideClaimRefusal(f ReplayFacts) (ReplayAction, bool) {
	if f.Queue != QueueReserved {
		return "", false
	}
	switch f.Claim {
	case ClaimDependencyRefusal:
		return AdvanceClaimRefusal, true
	case ClaimAlreadyAssigned, ClaimConflict:
		return ReplayRepairRequired, true
	case ClaimExternalRefusal:
		return AdvanceClaimRefusal, true
	case ClaimNone, ClaimMatching:
		return "", false
	default:
		return ReplayRepairRequired, true
	}
}

func (f ReplayFacts) coherent() bool {
	if f.Git == GitMatching && f.Bead != BeadClosed {
		return false
	}
	switch f.IntentPhase {
	case PhasePrepared:
		return f.preparedFactsCoherent()
	case PhaseClaimDurable:
		return f.claimFactsCoherent()
	case PhaseRunDurable:
		return f.runFactsCoherent()
	case PhaseHandoffDurable:
		return f.handoffFactsCoherent()
	default:
		return false
	}
}

func (f ReplayFacts) claimFactsCoherent() bool {
	return f.Claim == ClaimNone && f.Worktree == WorktreeAbsent && f.Session == SessionAbsent &&
		f.SessionReceipt == SessionReceiptAbsent && !f.RunOutcomeDurable &&
		(f.RunRecord == RunRecordAbsent || f.RunRecord == RunRecordBase)
}

func (f ReplayFacts) runFactsCoherent() bool {
	return f.Claim == ClaimNone && f.Session == SessionAbsent && f.SessionReceipt == SessionReceiptAbsent &&
		!f.RunOutcomeDurable && f.Git == GitAbsent
}

func (f ReplayFacts) handoffFactsCoherent() bool {
	if f.Claim != ClaimNone || f.RunRecord != RunRecordSession {
		return false
	}
	if f.Worktree == WorktreePrepared {
		return f.Session == SessionAbsent && f.SessionReceipt == SessionReceiptAbsent && !f.RunOutcomeDurable
	}
	if f.Worktree != WorktreeLeased {
		return false
	}
	return !f.RunOutcomeDurable ||
		(f.SessionReceipt == SessionReceiptExact && (f.Session == SessionAbsent || f.Session == SessionDead))
}

func (f ReplayFacts) receiptPhaseCoherent() bool {
	return f.IntentPhase == PhaseHandoffDurable || f.SessionReceipt == SessionReceiptAbsent
}

func (f ReplayFacts) preparedFactsCoherent() bool {
	if f.RunRecord != RunRecordAbsent || f.Worktree != WorktreeAbsent || f.Session != SessionAbsent ||
		f.SessionReceipt != SessionReceiptAbsent || f.RunOutcomeDurable {
		return false
	}
	switch f.Claim {
	case ClaimNone:
		return true
	case ClaimMatching:
		return f.Bead == BeadInProgress
	case ClaimDependencyRefusal:
		return f.Bead == BeadOpen
	case ClaimExternalRefusal:
		return f.Bead == BeadOther
	case ClaimAlreadyAssigned:
		return f.Bead == BeadOpen || f.Bead == BeadInProgress
	case ClaimConflict:
		return false
	default:
		return false
	}
}

func decideClaimReplay(f ReplayFacts) ReplayAction {
	if f.Queue != QueueReserved {
		return ReplayRepairRequired
	}
	if f.Bead == BeadOpen && f.RunRecord == RunRecordAbsent {
		return ReplayClaim
	}
	if f.Bead != BeadInProgress {
		return ReplayRepairRequired
	}
	if f.RunRecord == RunRecordAbsent {
		return WriteRunRecord
	}
	if f.RunRecord == RunRecordBase {
		return AdvanceRunPhase
	}
	return ReplayRepairRequired
}

func decideRunReplay(f ReplayFacts) ReplayAction {
	if f.Queue != QueueReserved || f.Bead != BeadInProgress || f.RunRecord == RunRecordAbsent {
		return ReplayRepairRequired
	}
	switch f.RunRecord {
	case RunRecordBase:
		if f.Worktree != WorktreeAbsent {
			return ReplayRepairRequired
		}
		return ResumeProvision
	case RunRecordLocated:
		switch f.Worktree {
		case WorktreePrepared:
			return PrepareHandoff
		case WorktreeAbsent:
			return ResumeProvision
		default:
			return ReplayRepairRequired
		}
	case RunRecordSession:
		if f.Worktree == WorktreePrepared {
			return AdvanceHandoffPhase
		}
	case RunRecordAbsent, RunRecordConflict:
		return ReplayRepairRequired
	}
	return ReplayRepairRequired
}

func decideHandoffReplay(f ReplayFacts) ReplayAction {
	if f.Queue != QueueReserved || f.Bead != BeadInProgress || f.RunRecord != RunRecordSession {
		return ReplayRepairRequired
	}
	switch {
	case (f.Worktree == WorktreePrepared || f.Worktree == WorktreeLeased) &&
		f.SessionReceipt == SessionReceiptAbsent && f.Session == SessionAbsent:
		return ReplaySessionStart
	case f.Worktree == WorktreeLeased && f.SessionReceipt == SessionReceiptAbsent && f.Session == SessionDead:
		return RemoveDeadUnstartedTarget
	case f.Worktree == WorktreeLeased && f.SessionReceipt == SessionReceiptExact && f.Session == SessionLive:
		return AdoptLive
	case f.Worktree == WorktreeLeased && f.SessionReceipt == SessionReceiptExact && (f.Session == SessionAbsent || f.Session == SessionDead):
		if f.RunOutcomeDurable {
			return AdvanceRunOutcome
		}
		return ResumeDead
	default:
		return ReplayRepairRequired
	}
}

func (f ReplayFacts) hasConflict() bool {
	return f.Queue == QueueConflict || f.Bead == BeadConflict || f.RunRecord == RunRecordConflict ||
		f.Worktree == WorktreeConflict || f.Session == SessionConflict ||
		f.SessionReceipt == SessionReceiptConflict || f.Git == GitConflict ||
		f.Claim == ClaimConflict || f.Preclaim == PreclaimConflict
}

func (f ReplayFacts) validate() error {
	if !f.IntentPhase.valid() {
		return fmt.Errorf("dispatch: invalid replay phase %q", f.IntentPhase)
	}
	if !validQueueFact(f.Queue) || !validBeadFact(f.Bead) || !validRunRecordFact(f.RunRecord) ||
		!validWorktreeFact(f.Worktree) || !validSessionFact(f.Session) ||
		!validSessionReceiptFact(f.SessionReceipt) || !validGitFact(f.Git) ||
		!validClaimFact(f.Claim) || !validPreclaimFact(f.Preclaim) {
		return fmt.Errorf("dispatch: invalid replay facts")
	}
	if f.IntentPhase == PhaseClaimRefused {
		if err := (ClaimRefusalBinding{Cause: f.RefusalCause}).validate(); err != nil {
			return err
		}
	} else if f.RefusalCause != "" {
		return errors.New("dispatch: replay refusal cause requires claim_refused phase")
	}
	return nil
}

func validPreclaimFact(v PreclaimFact) bool {
	switch v {
	case PreclaimAbsent, PreclaimMaxAttemptsItemTerminal, PreclaimMaxAttemptsGroupDurable,
		PreclaimCrossQueueItemTerminal, PreclaimCrossQueueGroupDurable,
		PreclaimDependencyItemTerminal, PreclaimDependencyGroupDurable,
		PreclaimReleased, PreclaimConflict:
		return true
	default:
		return false
	}
}

func decidePreclaimReplay(f ReplayFacts) (ReplayAction, bool) {
	if f.Preclaim == PreclaimAbsent && f.IntentPhase != PhaseClaimRefused {
		return "", false
	}
	switch f.IntentPhase {
	case PhasePrepared:
		if !preclaimBaseCoherent(f) {
			return ReplayRepairRequired, true
		}
		return decidePreparedPreclaim(f), true
	case PhaseClaimRefused:
		if !preclaimBaseCoherent(f) {
			return ReplayRepairRequired, true
		}
		return decideRefusedPreclaim(f), true
	default:
		return ReplayRepairRequired, true
	}
}

func preclaimBaseCoherent(f ReplayFacts) bool {
	return f.Claim == ClaimNone && f.RunRecord == RunRecordAbsent && f.Worktree == WorktreeAbsent &&
		f.Session == SessionAbsent && f.Git == GitAbsent && !f.RunOutcomeDurable
}

func decidePreparedPreclaim(f ReplayFacts) ReplayAction {
	if f.Bead != BeadOpen {
		return ReplayRepairRequired
	}
	switch f.Preclaim {
	case PreclaimMaxAttemptsItemTerminal, PreclaimCrossQueueItemTerminal:
		if f.Queue == QueueTerminalUnreopened {
			return FinalizePreclaimGroup
		}
	case PreclaimMaxAttemptsGroupDurable, PreclaimCrossQueueGroupDurable:
		if f.Queue == QueueTerminalUnreopened {
			return RemoveDispatchIntent
		}
	default:
		return ReplayRepairRequired
	}
	return ReplayRepairRequired
}

func decideRefusedPreclaim(f ReplayFacts) ReplayAction {
	switch f.RefusalCause {
	case ClaimRefusalDependency:
		return decideDependencyRefusal(f)
	case ClaimRefusalSupportedNonOpen:
		return decideExternalRefusal(f)
	default:
		return ReplayRepairRequired
	}
}

func decideDependencyRefusal(f ReplayFacts) ReplayAction {
	if f.Bead != BeadOpen {
		return ReplayRepairRequired
	}
	switch f.Preclaim {
	case PreclaimAbsent:
		if f.Queue == QueueReserved {
			return FailQueueItem
		}
	case PreclaimDependencyItemTerminal:
		if f.Queue == QueueTerminalUnreopened {
			return FinalizePreclaimGroup
		}
	case PreclaimDependencyGroupDurable:
		if f.Queue == QueueTerminalUnreopened {
			return RemoveDispatchIntent
		}
	default:
		return ReplayRepairRequired
	}
	return ReplayRepairRequired
}

func decideExternalRefusal(f ReplayFacts) ReplayAction {
	if f.Bead != BeadOther {
		return ReplayRepairRequired
	}
	if f.Preclaim == PreclaimAbsent && f.Queue == QueueReserved {
		return ReleaseReservation
	}
	if f.Preclaim == PreclaimReleased && f.Queue == QueueOfferable {
		return RemoveDispatchIntent
	}
	return ReplayRepairRequired
}

func validQueueFact(v QueueFact) bool {
	return v == QueueOfferable || v == QueueReserved || v == QueueTerminalSuccess ||
		v == QueueTerminalRetryable || v == QueueTerminalUnreopened || v == QueueConflict
}

func validBeadFact(v BeadFact) bool {
	return v == BeadOpen || v == BeadInProgress || v == BeadClosed || v == BeadOther || v == BeadConflict
}

func validRunRecordFact(v RunRecordFact) bool {
	return v == RunRecordAbsent || v == RunRecordBase || v == RunRecordLocated || v == RunRecordSession || v == RunRecordConflict
}

func validWorktreeFact(v WorktreeFact) bool {
	return v == WorktreeAbsent || v == WorktreePrepared || v == WorktreeLeased || v == WorktreeConflict
}

func validSessionFact(v SessionFact) bool {
	return v == SessionAbsent || v == SessionLive || v == SessionDead || v == SessionConflict
}

func validSessionReceiptFact(v SessionReceiptFact) bool {
	return v == SessionReceiptAbsent || v == SessionReceiptExact || v == SessionReceiptConflict
}

func validGitFact(v GitFact) bool { return v == GitAbsent || v == GitMatching || v == GitConflict }

func validClaimFact(v ClaimFact) bool {
	return v == ClaimNone || v == ClaimMatching || v == ClaimDependencyRefusal ||
		v == ClaimAlreadyAssigned || v == ClaimExternalRefusal || v == ClaimConflict
}

func decideTerminalReplay(f ReplayFacts) (ReplayAction, bool) {
	if f.IntentPhase != PhaseHandoffDurable {
		switch f.Queue {
		case QueueTerminalSuccess, QueueTerminalRetryable, QueueTerminalUnreopened:
			return ReplayRepairRequired, true
		case QueueOfferable, QueueReserved, QueueConflict:
			return "", false
		}
	}
	switch f.Queue {
	case QueueTerminalSuccess:
		if f.Bead == BeadClosed && f.Git == GitMatching && f.terminalResidueCoherent() {
			return ReplayCleanupOnly, true
		}
		return ReplayRepairRequired, true
	case QueueTerminalRetryable:
		if f.Bead == BeadOpen && f.Git == GitAbsent && f.terminalResidueCoherent() {
			return ReplayCleanupOnly, true
		}
		return ReplayRepairRequired, true
	case QueueTerminalUnreopened:
		return ReplayRepairRequired, true
	case QueueOfferable, QueueReserved, QueueConflict:
		return "", false
	default:
		return ReplayRepairRequired, true
	}
}

func (f ReplayFacts) closedResidueCoherent() bool {
	if f.Session == SessionLive {
		return false
	}
	if f.IntentPhase == PhaseHandoffDurable {
		return (f.RunRecord == RunRecordAbsent || f.RunRecord == RunRecordSession) &&
			(f.Worktree == WorktreeAbsent || f.Worktree == WorktreeLeased)
	}
	return f.RunRecord == RunRecordAbsent && f.Worktree == WorktreeAbsent && f.Session == SessionAbsent
}

func (f ReplayFacts) terminalResidueCoherent() bool {
	return f.IntentPhase == PhaseHandoffDurable && f.Claim == ClaimNone &&
		(f.RunRecord == RunRecordAbsent || f.RunRecord == RunRecordSession) &&
		(f.Worktree == WorktreeAbsent || f.Worktree == WorktreeLeased) &&
		(f.Session == SessionAbsent || f.Session == SessionDead)
}
