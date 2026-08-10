package orchestrator

import "github.com/gregberns/harmonik/internal/core"

// ClaimFailureKind is the boundary classification of a failed bead claim.
type ClaimFailureKind uint8

// The claim-failure kinds. ClaimFailureOther is every cause the scheduler does
// not tell apart; ClaimFailureDependencyBlocked is a claim refused because the
// bead still has an open dependency.
const (
	ClaimFailureOther ClaimFailureKind = iota
	ClaimFailureDependencyBlocked
)

// ClaimFailureDisposition tells the scheduler shell which effect to perform.
type ClaimFailureDisposition uint8

// The dispositions the scheduler shell can perform. ClaimFailureRetryReady asks
// for another ready bead, ClaimFailureReleaseQueueReservation gives the queue
// reservation back, and ClaimFailureFailQueueItem fails the queue item.
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
