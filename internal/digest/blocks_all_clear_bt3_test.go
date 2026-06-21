package digest

// blocks_all_clear_bt3_test.go — BT3 integration tests for the deterministic
// digest projector's flywheel behaviour.
//
// These exercise the REAL projector (digest.Build) end-to-end against on-disk
// surfaces written by the REAL trip emitter (sentinel.EmitTrip) and a fake br,
// rather than stubbing the projector. They cover two of the three BT3 scenarios:
//
//   - blocks-all-clear (§2.1, §3.5): a PENDING sentinel-class decision makes the
//     projector surface a non-empty PendingDecisions, so the captain CANNOT
//     all-clear; resolving it (ClearTrip) restores the all-clear.
//
//   - undeployed-tail (§5.2, §5.3): a Phase-2 class with a closed bead makes the
//     digest actionable (HasUndeployedTail=true) EVEN WHEN `br ready` is empty —
//     the flywheel does not stall on an empty ready-beads list.
//
// Bead: hk-vdk4 (flywheel-BT3). Epic: hk-0oca (codename:flywheel).

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/sentinel"
)

// digestIsAllClear reports whether the projector would let the captain return
// "nothing to do". Per flywheel-motion.md §2.1/§3.5 the all-clear is structurally
// blocked while ANY decision_required exception is pending — i.e. the all-clear
// holds iff PendingDecisions is empty.
func digestIsAllClear(d *DigestJSON) bool {
	return len(d.PendingDecisions) == 0
}

// TestBT3_BlocksAllClear_PendingSentinelDecision is the highest-value BT3
// scenario. With the REAL projector (digest.Build) and a PENDING sentinel-class
// decision written by the REAL trip emitter (sentinel.EmitTrip), the captain
// CANNOT all-clear: the projector surfaces the exception in PendingDecisions and
// the all-clear stays blocked until the decision resolves.
//
// Spec: flywheel-motion.md §2.1 ("structurally BLOCKS the all-clear"),
// §3.5, §2.2 (clears only on real movement / explicit ack).
func TestBT3_BlocksAllClear_PendingSentinelDecision(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// Pre-condition: with no pending decision the projector all-clears.
	base, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     DefaultLimits(),
		Now:        now,
	})
	if err != nil {
		t.Fatalf("baseline Build: %v", err)
	}
	if !digestIsAllClear(base) {
		t.Fatalf("baseline (no pending decision) must all-clear; got %d pending decisions",
			len(base.PendingDecisions))
	}

	// The sentinel trips: emit ONE real decision_required exception. This writes
	// the durable ack-state file AND the EV-044 event — the exact machinery the
	// projector reads. No projector stubbing.
	ackToken, err := sentinel.EmitTrip(context.Background(), sentinel.TripInput{
		ProjectDir:   dir,
		ReadyBeadIDs: []string{"hk-bt3a"},
		Now:          now,
	})
	if err != nil || ackToken == "" {
		t.Fatalf("EmitTrip: tok=%q err=%v", ackToken, err)
	}

	// With the pending sentinel exception, the REAL projector must surface it and
	// the all-clear must be BLOCKED.
	blocked, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     DefaultLimits(),
		Now:        now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("Build with pending decision: %v", err)
	}
	if digestIsAllClear(blocked) {
		t.Fatalf("captain must NOT all-clear while a sentinel decision is pending; "+
			"got %d pending decisions", len(blocked.PendingDecisions))
	}

	// The surfaced exception must be the sentinel one, carrying its ack_token so
	// the captain has a concrete handle (it cannot be silently dropped).
	var found bool
	for _, pd := range blocked.PendingDecisions {
		if pd.AckToken == ackToken {
			found = true
			if pd.SubjectKind != "queue" || pd.SubjectID != "sentinel" {
				t.Errorf("sentinel exception subject = %s/%s; want queue/sentinel",
					pd.SubjectKind, pd.SubjectID)
			}
		}
	}
	if !found {
		t.Errorf("pending sentinel exception (ack_token=%s) not surfaced by projector; got %+v",
			ackToken, blocked.PendingDecisions)
	}

	// Resolving via real movement (ClearTrip) must restore the all-clear — the
	// block is held by the PENDING status, not permanently.
	if err := sentinel.ClearTrip(context.Background(), dir, ackToken, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("ClearTrip: %v", err)
	}
	cleared, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     DefaultLimits(),
		Now:        now.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Build after ClearTrip: %v", err)
	}
	if !digestIsAllClear(cleared) {
		t.Errorf("after ClearTrip the projector must all-clear again; got %d pending decisions: %+v",
			len(cleared.PendingDecisions), cleared.PendingDecisions)
	}
}

// TestBT3_UndeployedTail_ActionableWhenReadyEmpty verifies that a Phase-2 class
// with a CLOSED bead makes the digest actionable (HasUndeployedTail=true) EVEN
// WHEN `br ready` is empty. The opportunity gate must treat the merged-but-
// undeployed tail as work so the flywheel does not stall on an empty ready list.
//
// The fake br returns NO ready beads (default branch → {"issues":[]}) and ONE
// closed bead carrying the Phase-2 class label, so the only actionable signal is
// the undeployed tail.
//
// Spec: flywheel-motion.md §5.2 ("does not stall on an empty ready-beads list"),
// §5.3 (Phase-2 done_definition).
func TestBT3_UndeployedTail_ActionableWhenReadyEmpty(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)

	// Configure a Phase-2 class (done_definition != "merged").
	cfgPath := filepath.Join(dir, ".harmonik", "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sentinel:
  done_definition:
    deploy-class: make deploy && make smoke
`), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	// Fake br: `ready` (the default branch) returns {"issues":[]} (EMPTY ready
	// list); `list --status closed --json` returns one bead with the Phase-2 label.
	fakeBr := makeFakeBr(t, dir,
		`{"issues":[{"id":"hk-bt3tail","title":"deploy thing","labels":["deploy-class"]}]}`)

	d, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     DefaultLimits(),
		Now:        time.Unix(1700000000, 0),
		BrPath:     fakeBr,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Precondition the scenario depends on: br ready is genuinely empty.
	if len(d.ReadyBeads) != 0 {
		t.Fatalf("scenario requires an empty ready list; got %d ready beads: %+v",
			len(d.ReadyBeads), d.ReadyBeads)
	}

	// The actionable signal: the undeployed tail.
	if !d.HasUndeployedTail {
		t.Errorf("HasUndeployedTail must be true (Phase-2 closed bead) even with empty ready list; got false")
	}

	// Control: with NO Phase-2 class configured, the same empty-ready project is
	// NOT actionable via the tail — proving the tail (not br) drives the signal.
	dirNoPhase2 := makeMinimalProject(t)
	fakeBrNoMatch := makeFakeBr(t, dirNoPhase2, `{"issues":[]}`)
	d2, err := Build(context.Background(), BuildInput{
		ProjectDir: dirNoPhase2,
		Limits:     DefaultLimits(),
		Now:        time.Unix(1700000000, 0),
		BrPath:     fakeBrNoMatch,
	})
	if err != nil {
		t.Fatalf("control Build: %v", err)
	}
	if d2.HasUndeployedTail {
		t.Errorf("control: HasUndeployedTail must be false with no Phase-2 class + empty ready; got true")
	}
}
