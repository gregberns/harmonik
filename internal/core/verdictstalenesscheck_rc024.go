package core

// VerdictStalenessResult is the outcome of the RC-024 staleness check.
//
// When Stale is false the verdict executor proceeds with RC-025 mechanical
// execution. When Stale is true the executor MUST emit the
// reconciliation_verdict_stale event using Payload as its payload and then
// re-dispatch a fresh reconciliation workflow; it MUST NOT execute the verdict.
//
// The result is value-typed (no heap allocation on the non-stale path).
type VerdictStalenessResult struct {
	// Stale is true when the staleness check detected that system state has
	// advanced beyond the snapshot captured at investigator-dispatch time.
	Stale bool

	// Payload is the ready-to-emit StaleVerdictPayload for the
	// reconciliation_verdict_stale event (RC-024; schemas.md §6.1).
	// Non-nil only when Stale is true; nil otherwise.
	Payload *StaleVerdictPayload
}

// CheckVerdictStaleness performs the RC-024 staleness check.
//
// It compares the snapshot captured at investigator-dispatch time
// (SnapshotToken.GitHeadHash, SnapshotToken.BeadsAuditEntryID) against the
// current values re-captured at verdict-execution time. The verdict is stale
// if either:
//
//   - the target run's checkpoint trail has gained a new commit since the
//     snapshot (currentGitHeadHash != snapshot.GitHeadHash), OR
//   - the target run's bead has changed status in the Beads audit log since
//     the snapshot (currentBeadsAuditID != snapshot.BeadsAuditEntryID).
//
// Changes to sibling beads or to the daemon's JSONL event log do NOT
// contribute to staleness per RC-024 — only the two fields above count.
//
// When both stores have advanced, git staleness takes priority (it is listed
// first in RC-024 and corresponds to the more fundamental state authority).
//
// The caller is responsible for re-capturing currentGitHeadHash and
// currentBeadsAuditID from live system state immediately before calling this
// function. Neither this function nor VerdictStalenessResult performs any I/O.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-024;
// specs/reconciliation/schemas.md §6.1 RECORD StaleVerdictPayload.
func CheckVerdictStaleness(snapshot SnapshotToken, currentGitHeadHash, currentBeadsAuditID string) VerdictStalenessResult {
	if snapshot.GitHeadHash != currentGitHeadHash {
		return VerdictStalenessResult{
			Stale: true,
			Payload: &StaleVerdictPayload{
				Snapshot:            snapshot,
				CurrentGitHeadHash:  currentGitHeadHash,
				CurrentBeadsAuditID: currentBeadsAuditID,
				DivergenceReason:    StaleDivergenceReasonGitBranchAdvanced,
			},
		}
	}
	if snapshot.BeadsAuditEntryID != currentBeadsAuditID {
		return VerdictStalenessResult{
			Stale: true,
			Payload: &StaleVerdictPayload{
				Snapshot:            snapshot,
				CurrentGitHeadHash:  currentGitHeadHash,
				CurrentBeadsAuditID: currentBeadsAuditID,
				DivergenceReason:    StaleDivergenceReasonBeadsAuditAdvanced,
			},
		}
	}
	return VerdictStalenessResult{Stale: false}
}
