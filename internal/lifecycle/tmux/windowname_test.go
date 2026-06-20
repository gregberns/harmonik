package tmux

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// windowNameFixtureHash6 is the first 6 hex chars of the fixture project hash,
// used to construct expected sentinel-prefixed names.
const windowNameFixtureHash6 = "a1b2c3"

// windowNameFixtureHash returns a valid 12-char hex ProjectHash for tests.
func windowNameFixtureHash(t *testing.T) core.ProjectHash {
	t.Helper()
	var h core.ProjectHash
	if err := h.UnmarshalText([]byte("a1b2c3d4e5f6")); err != nil {
		t.Fatalf("windowNameFixtureHash: %v", err)
	}
	return h
}

// windowNameFixtureBeadID returns a core.BeadID from a raw string for tests.
func windowNameFixtureBeadID(raw string) core.BeadID {
	return core.BeadID(raw)
}

// windowNameFixtureTruncSuffix computes the expected truncation suffix for a
// given bead ID string: "~" + lowercase-hex(SHA-256(beadID))[:8].
func windowNameFixtureTruncSuffix(beadID string) string {
	sum := sha256.Sum256([]byte(beadID))
	return "~" + fmt.Sprintf("%x", sum[:4])
}

func TestWindowName(t *testing.T) {
	hash := windowNameFixtureHash(t)
	hash6 := windowNameFixtureHash6
	sentinel := "hk-" + hash6 + "-"

	shortID := "hk-abc123"
	// Long bead_id: 65 bytes, which forces truncation for single/owns
	// (no sentinel, no suffix → total 65 > 64).
	longID := strings.Repeat("x", 65)

	longIDTruncBase := longID[:beadIDMaxBytes] + windowNameFixtureTruncSuffix(longID)

	tests := []struct {
		name        string
		beadID      string
		phase       Phase
		iteration   int
		ownsSession bool
		want        string
	}{
		// ── single-mode, ownsSession=true ──────────────────────────────────────
		{
			name:        "single/owns",
			beadID:      shortID,
			phase:       PhaseSingle,
			iteration:   1,
			ownsSession: true,
			want:        shortID,
		},
		// ── single-mode, ownsSession=false ────────────────────────────────────
		{
			name:        "single/reuse",
			beadID:      shortID,
			phase:       PhaseSingle,
			iteration:   1,
			ownsSession: false,
			want:        sentinel + shortID,
		},
		// ── implementer-initial, ownsSession=true ─────────────────────────────
		{
			name:        "implementer-initial/owns/iter1",
			beadID:      shortID,
			phase:       PhaseImplementerInitial,
			iteration:   1,
			ownsSession: true,
			want:        shortID + "/i1",
		},
		{
			name:        "implementer-initial/owns/iter2",
			beadID:      shortID,
			phase:       PhaseImplementerInitial,
			iteration:   2,
			ownsSession: true,
			want:        shortID + "/i2",
		},
		// ── implementer-initial, ownsSession=false ────────────────────────────
		{
			name:        "implementer-initial/reuse/iter1",
			beadID:      shortID,
			phase:       PhaseImplementerInitial,
			iteration:   1,
			ownsSession: false,
			want:        sentinel + shortID + "/i1",
		},
		// ── implementer-resume, ownsSession=true ──────────────────────────────
		{
			name:        "implementer-resume/owns/iter1",
			beadID:      shortID,
			phase:       PhaseImplementerResume,
			iteration:   1,
			ownsSession: true,
			want:        shortID + "/i1",
		},
		// ── implementer-resume, ownsSession=false ─────────────────────────────
		{
			name:        "implementer-resume/reuse/iter3",
			beadID:      shortID,
			phase:       PhaseImplementerResume,
			iteration:   3,
			ownsSession: false,
			want:        sentinel + shortID + "/i3",
		},
		// ── reviewer, ownsSession=true ────────────────────────────────────────
		{
			name:        "reviewer/owns/iter1",
			beadID:      shortID,
			phase:       PhaseReviewer,
			iteration:   1,
			ownsSession: true,
			want:        shortID + "/r1",
		},
		{
			name:        "reviewer/owns/iter2",
			beadID:      shortID,
			phase:       PhaseReviewer,
			iteration:   2,
			ownsSession: true,
			want:        shortID + "/r2",
		},
		// ── reviewer, ownsSession=false ───────────────────────────────────────
		{
			name:        "reviewer/reuse/iter1",
			beadID:      shortID,
			phase:       PhaseReviewer,
			iteration:   1,
			ownsSession: false,
			want:        sentinel + shortID + "/r1",
		},
		// ── truncation: long bead_id, no suffix (single/owns) ─────────────────
		{
			name:        "truncation/single/owns",
			beadID:      longID,
			phase:       PhaseSingle,
			iteration:   1,
			ownsSession: true,
			want:        longIDTruncBase,
		},
		// ── truncation: long bead_id, with suffix (implementer/owns) ──────────
		{
			name:        "truncation/implementer/owns/iter1",
			beadID:      longID,
			phase:       PhaseImplementerInitial,
			iteration:   1,
			ownsSession: true,
			want:        longIDTruncBase + "/i1",
		},
		// ── truncation: long bead_id + sentinel prefix (implementer/reuse) ────
		{
			name:        "truncation/implementer/reuse/iter2",
			beadID:      longID,
			phase:       PhaseImplementerInitial,
			iteration:   2,
			ownsSession: false,
			want:        sentinel + longIDTruncBase + "/i2",
		},
		// ── truncation: long bead_id, reviewer suffix ─────────────────────────
		{
			name:        "truncation/reviewer/reuse/iter1",
			beadID:      longID,
			phase:       PhaseReviewer,
			iteration:   1,
			ownsSession: false,
			want:        sentinel + longIDTruncBase + "/r1",
		},
		// ── boundary: bead_id exactly at 56 bytes (no truncation) ─────────────
		{
			name:        "boundary/56bytes/no-truncation/single/owns",
			beadID:      strings.Repeat("a", 56),
			phase:       PhaseSingle,
			iteration:   1,
			ownsSession: true,
			want:        strings.Repeat("a", 56),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := WindowName(
				windowNameFixtureBeadID(tc.beadID),
				tc.phase,
				tc.iteration,
				hash,
				tc.ownsSession,
			)
			if got != tc.want {
				t.Errorf("WindowName() =\n  %q\nwant\n  %q", got, tc.want)
			}
		})
	}
}

// TestWindowName_Determinism verifies that identical inputs produce
// byte-identical output across repeated calls.
func TestWindowName_Determinism(t *testing.T) {
	hash := windowNameFixtureHash(t)
	id := windowNameFixtureBeadID("hk-det-test")

	calls := 10
	first := WindowName(id, PhaseImplementerInitial, 2, hash, false)
	for i := 1; i < calls; i++ {
		got := WindowName(id, PhaseImplementerInitial, 2, hash, false)
		if got != first {
			t.Errorf("call %d: got %q, want %q", i+1, got, first)
		}
	}
}

// TestWindowName_TruncationDeterminism verifies that the truncation hash is
// stable across repeated calls for a long bead_id.
func TestWindowName_TruncationDeterminism(t *testing.T) {
	hash := windowNameFixtureHash(t)
	longID := windowNameFixtureBeadID(strings.Repeat("z", 60))

	calls := 10
	first := WindowName(longID, PhaseReviewer, 1, hash, true)
	for i := 1; i < calls; i++ {
		got := WindowName(longID, PhaseReviewer, 1, hash, true)
		if got != first {
			t.Errorf("call %d: got %q, want %q", i+1, got, first)
		}
	}
}

// TestWindowName_NoTruncationAt64 verifies that a name of exactly 64 bytes
// is NOT truncated (the threshold is strictly > 64).
func TestWindowName_NoTruncationAt64(t *testing.T) {
	hash := windowNameFixtureHash(t)
	// Construct a bead_id such that len(bead_id) = 64 for single/owns.
	id := windowNameFixtureBeadID(strings.Repeat("b", 64))
	got := WindowName(id, PhaseSingle, 1, hash, true)
	if len(got) != 64 {
		t.Errorf("expected 64-byte name unchanged, got len=%d: %q", len(got), got)
	}
	if got != strings.Repeat("b", 64) {
		t.Errorf("expected verbatim bead_id, got %q", got)
	}
}

// TestWindowConstants pins the exported window-name constants that name the two
// windows inside a captain/crew session per the tmux-session-organization
// CONTRACT. Consumers (daemon slice C, keeper slice K via mirrored literals)
// depend on these exact values.
func TestWindowConstants(t *testing.T) {
	if WindowAgent != "agent" {
		t.Errorf("WindowAgent = %q; want %q", WindowAgent, "agent")
	}
	if WindowKeeper != "keeper" {
		t.Errorf("WindowKeeper = %q; want %q", WindowKeeper, "keeper")
	}
}
