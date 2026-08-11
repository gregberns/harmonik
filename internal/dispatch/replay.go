package dispatch

import "fmt"

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

// ReplayAction is the one next transition startup may execute.
type ReplayAction string

const (
	// ReplayReservation through ReplayRepairRequired are closed restart actions.
	ReplayReservation ReplayAction = "replay-reservation"
	// ReplayClaim repeats the exact claim transition.
	ReplayClaim ReplayAction = "replay-claim"
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
	Git               GitFact
	Claim             ClaimFact
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
		return FailQueueItem, true
	case ClaimAlreadyAssigned, ClaimConflict:
		return ReplayRepairRequired, true
	case ClaimExternalRefusal:
		return ReleaseReservation, true
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
		!f.RunOutcomeDurable && (f.RunRecord == RunRecordAbsent || f.RunRecord == RunRecordBase)
}

func (f ReplayFacts) runFactsCoherent() bool {
	return f.Claim == ClaimNone && f.Session == SessionAbsent && !f.RunOutcomeDurable && f.Git == GitAbsent
}

func (f ReplayFacts) handoffFactsCoherent() bool {
	return f.Claim == ClaimNone && f.RunRecord == RunRecordSession && f.Worktree == WorktreeLeased &&
		(!f.RunOutcomeDurable || f.Session == SessionDead)
}

func (f ReplayFacts) preparedFactsCoherent() bool {
	if f.RunRecord != RunRecordAbsent || f.Worktree != WorktreeAbsent || f.Session != SessionAbsent || f.RunOutcomeDurable {
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
		return ResumeProvision
	case RunRecordLocated:
		if f.Worktree == WorktreeLeased {
			return PrepareHandoff
		}
		return ResumeProvision
	case RunRecordSession:
		if f.Worktree == WorktreeLeased {
			return AdvanceHandoffPhase
		}
	case RunRecordAbsent, RunRecordConflict:
		return ReplayRepairRequired
	}
	return ReplayRepairRequired
}

func decideHandoffReplay(f ReplayFacts) ReplayAction {
	if f.Queue != QueueReserved || f.Bead != BeadInProgress || f.RunRecord != RunRecordSession || f.Worktree != WorktreeLeased {
		return ReplayRepairRequired
	}
	switch f.Session {
	case SessionAbsent:
		return ReplaySessionStart
	case SessionLive:
		return AdoptLive
	case SessionDead:
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
		f.Worktree == WorktreeConflict || f.Session == SessionConflict || f.Git == GitConflict || f.Claim == ClaimConflict
}

func (f ReplayFacts) validate() error {
	if !f.IntentPhase.valid() {
		return fmt.Errorf("dispatch: invalid replay phase %q", f.IntentPhase)
	}
	if !validQueueFact(f.Queue) || !validBeadFact(f.Bead) || !validRunRecordFact(f.RunRecord) ||
		!validWorktreeFact(f.Worktree) || !validSessionFact(f.Session) || !validGitFact(f.Git) || !validClaimFact(f.Claim) {
		return fmt.Errorf("dispatch: invalid replay facts")
	}
	return nil
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
	return v == WorktreeAbsent || v == WorktreeLeased || v == WorktreeConflict
}

func validSessionFact(v SessionFact) bool {
	return v == SessionAbsent || v == SessionLive || v == SessionDead || v == SessionConflict
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
