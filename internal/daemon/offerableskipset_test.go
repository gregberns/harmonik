package daemon

import (
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// offerableskipset_test.go — the daemon half of the head-of-line fallback
// (hk-nown4). offerableSkipSet owns the clock so the pure selector does not:
// it merges the loop's two refusal sets into the plain set SelectNextQueue
// reads, and purges what has expired.
//
// The two sets differ in a way the types do not show, and the difference is
// load-bearing:
//
//   - refusedUntil is clock-based and outlives the tick. It holds a bead a
//     sibling queue is running, on the five-minute in-progress cooldown
//     (hk-403fw).
//   - tickRefusals has NO clock and covers one tick's walk. A clock there
//     would let the earliest refusal lapse while the loop is still walking the
//     later ones, so the loop would re-offer a bead it already refused and the
//     walk would never end.
//
// These tests pin that difference. Give tickRefusals a window and
// TestOfferableSkipSet_TickRefusalsIgnoreTheClock goes red.

func TestOfferableSkipSet_RefusesOnlyWhileTheWindowIsOpen(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	refused := map[core.BeadID]time.Time{
		"open-window":    now.Add(4 * time.Minute),
		"expired-window": now.Add(-1 * time.Second),
	}

	skip := offerableSkipSet(refused, nil, now)

	if !skip["open-window"] {
		t.Error("a bead inside its refusal window is offerable; it must be refused")
	}
	if skip["expired-window"] {
		t.Error("a bead past its refusal window is refused; it must be offerable again")
	}
}

// TestOfferableSkipSet_TickRefusalsIgnoreTheClock is the termination guard.
//
// A tick refusal must hold for the whole walk however long the walk takes. The
// walk costs one `br show` subprocess per held bead, so a group of held beads
// can take longer to walk than any window a reader would think generous. If a
// clock ever governs this set, the loop re-offers a bead it already refused and
// spins — measured at 186,176 passes in 600 ms on a build without the arming.
func TestOfferableSkipSet_TickRefusalsIgnoreTheClock(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	tickRefusals := map[core.BeadID]bool{"held-this-tick": true}

	// Far past any window the clock-based set could carry. The tick set must be
	// unmoved by it.
	for _, at := range []time.Time{now, now.Add(time.Hour), now.Add(72 * time.Hour)} {
		if skip := offerableSkipSet(nil, tickRefusals, at); !skip["held-this-tick"] {
			t.Fatalf("at %v past the arming the tick refusal lapsed. It must have no clock at all: "+
				"the walk is only bounded while this set grows monotonically, and a lapse mid-walk "+
				"re-offers a bead the loop already refused.", at.Sub(now))
		}
	}
	if len(tickRefusals) != 1 {
		t.Errorf("the tick set was purged (%d entries left); only the per-tick clear may empty it", len(tickRefusals))
	}
}

func TestOfferableSkipSet_MergesBothSets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	refused := map[core.BeadID]time.Time{"sibling-claimed": now.Add(5 * time.Minute)}
	tickRefusals := map[core.BeadID]bool{"greenlight-held": true}

	skip := offerableSkipSet(refused, tickRefusals, now)

	if !skip["sibling-claimed"] {
		t.Error("the clock-based refusal is missing from the merged set")
	}
	if !skip["greenlight-held"] {
		t.Error("the tick refusal is missing from the merged set")
	}
	if len(skip) != 2 {
		t.Errorf("merged set holds %d entries; want 2 — %v", len(skip), skip)
	}
}

// TestOfferableSkipSet_PurgesExpiredEntries pins the bound on map growth. The
// loop arms an entry per contended bead and never deletes one itself, so the
// purge has to happen here or the clock-based map only ever grows.
func TestOfferableSkipSet_PurgesExpiredEntries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	refused := map[core.BeadID]time.Time{
		"stale-01": now.Add(-10 * time.Minute),
		"stale-02": now.Add(-1 * time.Millisecond),
		"live-01":  now.Add(1 * time.Minute),
	}

	offerableSkipSet(refused, nil, now)

	if len(refused) != 1 {
		t.Fatalf("refusal map holds %d entries after the purge; want 1 — %v", len(refused), refused)
	}
	if _, ok := refused["live-01"]; !ok {
		t.Error("the purge removed an entry whose window is still open")
	}
}

// TestOfferableSkipSet_NilWhenNothingIsRefused keeps the ordinary path free of
// an allocation, and pins that an all-expired map reads as "refuse nothing"
// rather than as an empty-but-present set.
func TestOfferableSkipSet_NilWhenNothingIsRefused(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	if skip := offerableSkipSet(nil, nil, now); skip != nil {
		t.Errorf("empty input produced %v; want nil", skip)
	}
	allExpired := map[core.BeadID]time.Time{"stale-01": now.Add(-time.Hour)}
	if skip := offerableSkipSet(allExpired, nil, now); skip != nil {
		t.Errorf("all-expired input produced %v; want nil", skip)
	}
}

// TestOfferableSkipSet_BoundaryIsExclusive pins that a clock-based refusal
// expires AT its deadline rather than one tick later. time.Before is the whole
// of it, and a flip to !After would hold the bead for an extra poll interval.
func TestOfferableSkipSet_BoundaryIsExclusive(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	refused := map[core.BeadID]time.Time{"exactly-now": now}

	if skip := offerableSkipSet(refused, nil, now); skip["exactly-now"] {
		t.Error("a refusal whose deadline is exactly now is still refused; it must be offerable")
	}
}
