package orchestrator

import "github.com/gregberns/harmonik/internal/core"

// ClaimFailureKind is the boundary classification of a failed bead claim.
type ClaimFailureKind uint8

const (
	ClaimFailureOther ClaimFailureKind = iota
	ClaimFailureDependencyBlocked
)

// ClaimFailureDisposition tells the scheduler shell which effect to perform.
type ClaimFailureDisposition uint8

const (
	ClaimFailureRetryReady ClaimFailureDisposition = iota
	ClaimFailureReleaseQueueReservation
	ClaimFailureFailQueueItem
)

// DecideClaimFailure is the pure policy for a failed claim. The scheduler owns
// reads and writes. This function only selects their disposition.
func DecideClaimFailure(
	queuePath bool,
	kind ClaimFailureKind,
	shownStatus core.CoarseStatus,
	showSucceeded bool,
) ClaimFailureDisposition {
	if !queuePath {
		return ClaimFailureRetryReady
	}
	if kind == ClaimFailureDependencyBlocked ||
		(showSucceeded && shownStatus == core.CoarseStatusBlocked) {
		return ClaimFailureFailQueueItem
	}
	return ClaimFailureReleaseQueueReservation
}
