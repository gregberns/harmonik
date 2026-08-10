package orchestrator

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

func TestDecideClaimFailureAnswersEveryDisposition(t *testing.T) {
	tests := []struct {
		name          string
		queuePath     bool
		kind          ClaimFailureKind
		status        core.CoarseStatus
		showSucceeded bool
		want          ClaimFailureDisposition
	}{
		{"ready path retries a dependency refusal", false, ClaimFailureDependencyBlocked, core.CoarseStatusBlocked, true, ClaimFailureRetryReady},
		{"typed dependency refusal fails a queue item", true, ClaimFailureDependencyBlocked, core.CoarseStatusOpen, true, ClaimFailureFailQueueItem},
		{"blocked bead status fails a queue item", true, ClaimFailureOther, core.CoarseStatusBlocked, true, ClaimFailureFailQueueItem},
		{"unrelated queue failure releases its reservation", true, ClaimFailureOther, core.CoarseStatusOpen, true, ClaimFailureReleaseQueueReservation},
		{"failed status lookup releases its reservation", true, ClaimFailureOther, "", false, ClaimFailureReleaseQueueReservation},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideClaimFailure(tc.queuePath, tc.kind, tc.status, tc.showSucceeded)
			if got != tc.want {
				t.Fatalf("DecideClaimFailure() = %v, want %v", got, tc.want)
			}
		})
	}
}
