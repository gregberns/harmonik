package queue_test

// stream_requeue_hkwifef_test.go — streamEligible re-queue correctness.
//
// Regression for hk-wifef: when a stream group contains a terminal entry
// for beadX followed by a later pending entry for the same bead_id
// (re-appended after failure), streamEligible must treat the later pending
// entry as eligible and return it at the terminal entry's stream position,
// preserving original stream order.
//
// Uses existing state_test.go helpers (stateFixtureGroup / stateFixtureItem).

import (
	"testing"

	"github.com/gregberns/harmonik/internal/queue"
)

// TestEligibleItems_Stream_RequeueAfterTerminal is a table test covering the
// re-append eligibility fix (hk-wifef).
func TestEligibleItems_Stream_RequeueAfterTerminal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		items         []queue.Item
		wantBeadID    string // empty → expect nil (nothing eligible)
		wantNilResult bool
	}{
		{
			// [terminal(beadX), pending(beadY), pending(beadX)]
			// beadX failed and was re-appended after beadY.  streamEligible
			// must return the re-appended pending(beadX) — at beadX's
			// original stream position — rather than pending(beadY).
			name: "requeue_before_sibling",
			items: []queue.Item{
				stateFixtureItem("hk-wifef-x", queue.ItemStatusFailed),
				stateFixtureItem("hk-wifef-y", queue.ItemStatusPending),
				stateFixtureItem("hk-wifef-x", queue.ItemStatusPending),
			},
			wantBeadID: "hk-wifef-x",
		},
		{
			// [terminal(beadX), pending(beadX)]
			// Simple re-append with no intervening sibling.
			name: "requeue_no_sibling",
			items: []queue.Item{
				stateFixtureItem("hk-wifef-x2", queue.ItemStatusFailed),
				stateFixtureItem("hk-wifef-x2", queue.ItemStatusPending),
			},
			wantBeadID: "hk-wifef-x2",
		},
		{
			// [terminal(beadX)] only — no later pending entry.
			// Must return nil; terminal entry alone is not eligible.
			name: "terminal_only_no_requeue",
			items: []queue.Item{
				stateFixtureItem("hk-wifef-x3", queue.ItemStatusFailed),
			},
			wantNilResult: true,
		},
		{
			// [terminal(beadX), pending(beadY), pending(beadZ), pending(beadX)]
			// beadX re-appended after two other beads.  Must return pending(beadX)
			// at beadX's original position (before beadY and beadZ).
			name: "requeue_multiple_intervening_siblings",
			items: []queue.Item{
				stateFixtureItem("hk-wifef-x4", queue.ItemStatusFailed),
				stateFixtureItem("hk-wifef-y4", queue.ItemStatusPending),
				stateFixtureItem("hk-wifef-z4", queue.ItemStatusPending),
				stateFixtureItem("hk-wifef-x4", queue.ItemStatusPending),
			},
			wantBeadID: "hk-wifef-x4",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := stateFixtureGroup(0, queue.GroupKindStream, queue.GroupStatusActive, tc.items)
			eligible := queue.EligibleItems(&g)

			if tc.wantNilResult {
				if len(eligible) != 0 {
					t.Errorf("EligibleItems = %d items, want 0 (nil/empty)", len(eligible))
				}
				return
			}

			if len(eligible) != 1 {
				t.Fatalf("EligibleItems = %d items, want 1", len(eligible))
			}
			if got := string(eligible[0].BeadID); got != tc.wantBeadID {
				t.Errorf("eligible[0].BeadID = %q, want %q", got, tc.wantBeadID)
			}
		})
	}
}
