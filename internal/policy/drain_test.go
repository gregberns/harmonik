package policy

// drain_test.go — pure truth-table tests for the quiesce/drain DECISION
// predicates: ClassifyDrain (GenuineDrain semantics), SleepVeto
// (the SS-INV-005 veto strands), and HasLatentWork (§4.2 latent-work).
//
// These assert the DECISION over projected scalar inputs — no DrainDetector, no
// br-ready reads, no worktree readdir, no bus. The daemon-side fact-gathering,
// oracle, marker-I/O and projection-shell coverage stays in package daemon
// (draindetect_test.go, quiesce_sleep_veto_hkzqb3_test.go, stategather tests).
//
// Spec ref: codename:sleep-wake (SS-INV-005) + GenuineDrain §4.2 latent-work.

import (
	"strings"
	"testing"
)

// --- ClassifyDrain -----------------------------------------------------------

func TestClassifyDrain_EmptyIsDrained(t *testing.T) {
	t.Parallel()
	if got := ClassifyDrain(DrainSnapshot{}); got != DrainStateDrained {
		t.Fatalf("empty snapshot: got %q, want DRAINED", got)
	}
}

func TestClassifyDrain_UnsureShortCircuits(t *testing.T) {
	t.Parallel()
	// Unsure wins even when a work axis is non-empty (fail-closed).
	s := DrainSnapshot{Unsure: true, ReadyCount: 5}
	if got := ClassifyDrain(s); got != DrainStateUnsure {
		t.Fatalf("unsure snapshot: got %q, want UNSURE", got)
	}
}

// TestClassifyDrain_EachAxisIsWork asserts every one of the eight GenuineDrain
// work axes independently classifies as HAS_WORK.
func TestClassifyDrain_EachAxisIsWork(t *testing.T) {
	t.Parallel()
	cases := map[string]DrainSnapshot{
		"ready":             {ReadyCount: 1},
		"in_progress":       {InProgressCount: 1},
		"registry_runs":     {RegistryRuns: 1},
		"live_worktrees":    {LiveWorktrees: 1},
		"queued":            {QueuedCount: 1},
		"paused_queues":     {PausedQueues: 1},
		"failed_archives":   {FailedArchives: 1},
		"blocked_open_epic": {BlockedByOpenEpic: 1},
	}
	for name, s := range cases {
		if got := ClassifyDrain(s); got != DrainStateHasWork {
			t.Errorf("%s: got %q, want HAS_WORK", name, got)
		}
	}
}

// TestClassifyDrain_NeedsDecompositionNotWork asserts the load-bearing divergence
// from HasLatentWork: GenuineDrain / ClassifyDrain drops NeedsDecomposition, so a
// snapshot with only NeedsDecomposition is DRAINED.
func TestClassifyDrain_NeedsDecompositionNotWork(t *testing.T) {
	t.Parallel()
	s := DrainSnapshot{NeedsDecomposition: 3}
	if got := ClassifyDrain(s); got != DrainStateDrained {
		t.Fatalf("needs-decomposition only: got %q, want DRAINED (GenuineDrain drops it)", got)
	}
}

// --- HasLatentWork -----------------------------------------------------------

func TestHasLatentWork_EmptyIsFalse(t *testing.T) {
	t.Parallel()
	if HasLatentWork(DrainSnapshot{}) {
		t.Fatal("empty snapshot: got true, want false")
	}
}

func TestHasLatentWork_UnsureIsLatent(t *testing.T) {
	t.Parallel()
	if !HasLatentWork(DrainSnapshot{Unsure: true}) {
		t.Fatal("unsure snapshot: got false, want true")
	}
}

// TestHasLatentWork_NeedsDecompositionIsLatent asserts the divergence: unlike
// ClassifyDrain, HasLatentWork counts NeedsDecomposition as latent work.
func TestHasLatentWork_NeedsDecompositionIsLatent(t *testing.T) {
	t.Parallel()
	if !HasLatentWork(DrainSnapshot{NeedsDecomposition: 1}) {
		t.Fatal("needs-decomposition: got false, want true")
	}
}

func TestHasLatentWork_EachDispatchAxis(t *testing.T) {
	t.Parallel()
	cases := map[string]DrainSnapshot{
		"ready":             {ReadyCount: 1},
		"in_progress":       {InProgressCount: 1},
		"registry_runs":     {RegistryRuns: 1},
		"live_worktrees":    {LiveWorktrees: 1},
		"queued":            {QueuedCount: 1},
		"paused_queues":     {PausedQueues: 1},
		"failed_archives":   {FailedArchives: 1},
		"blocked_open_epic": {BlockedByOpenEpic: 1},
	}
	for name, s := range cases {
		if !HasLatentWork(s) {
			t.Errorf("%s: got false, want true", name)
		}
	}
}

// --- SleepVeto ---------------------------------------------------------------

func TestSleepVeto_EmptyNotVetoed(t *testing.T) {
	t.Parallel()
	res := SleepVeto(DrainSnapshot{})
	if res.Vetoed() {
		t.Fatalf("empty snapshot: got vetoed (%+v), want not vetoed", res)
	}
}

// TestSleepVeto_UnsureShortCircuits asserts Unsure is checked FIRST and returns
// with reasons and no strands, even when work axes are non-empty.
func TestSleepVeto_UnsureShortCircuits(t *testing.T) {
	t.Parallel()
	s := DrainSnapshot{
		Unsure:        true,
		UnsureReasons: []string{"nil epic seam"},
		ReadyCount:    9, // would strand, but Unsure wins
	}
	res := SleepVeto(s)
	if !res.Unsure {
		t.Fatal("want Unsure=true")
	}
	if len(res.Strands) != 0 {
		t.Fatalf("want no strands on Unsure, got %v", res.Strands)
	}
	if !res.Vetoed() {
		t.Fatal("Unsure must veto")
	}
	if len(res.UnsureReasons) != 1 || res.UnsureReasons[0] != "nil epic seam" {
		t.Fatalf("reasons not passed through: %v", res.UnsureReasons)
	}
}

// TestSleepVeto_StrandOrderingAndText asserts the exact strand phrasing and the
// fixed axis ORDER preserved from quiesce.go vetoCheck.
func TestSleepVeto_StrandOrderingAndText(t *testing.T) {
	t.Parallel()
	s := DrainSnapshot{
		ReadyCount:        1,
		InProgressCount:   2,
		RegistryRuns:      3,
		LiveWorktrees:     4,
		QueuedCount:       5,
		PausedQueues:      6,
		FailedArchives:    7,
		BlockedByOpenEpic: 8,
	}
	want := []string{
		"1 ready bead(s)",
		"2 in-progress bead(s)",
		"3 in-flight run(s)",
		"4 live worktree(s)",
		"5 queued item(s)",
		"6 paused queue(s)",
		"7 failed archive(s)",
		"8 bead(s) blocked by open epic(s)",
	}
	res := SleepVeto(s)
	if strings.Join(res.Strands, ", ") != strings.Join(want, ", ") {
		t.Fatalf("strands mismatch:\n got %q\nwant %q", res.Strands, want)
	}
}

// TestSleepVeto_SingleAxis asserts each axis alone vetoes with its own strand.
func TestSleepVeto_SingleAxis(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		snap DrainSnapshot
		want string
	}{
		{"ready", DrainSnapshot{ReadyCount: 1}, "ready bead"},
		{"in_progress", DrainSnapshot{InProgressCount: 1}, "in-progress bead"},
		{"registry_runs", DrainSnapshot{RegistryRuns: 1}, "in-flight run"},
		{"live_worktrees", DrainSnapshot{LiveWorktrees: 1}, "live worktree"},
		{"queued", DrainSnapshot{QueuedCount: 1}, "queued item"},
		{"paused_queues", DrainSnapshot{PausedQueues: 1}, "paused queue"},
		{"failed_archives", DrainSnapshot{FailedArchives: 1}, "failed archive"},
		{"blocked_open_epic", DrainSnapshot{BlockedByOpenEpic: 1}, "blocked by open epic"},
	}
	for _, c := range cases {
		res := SleepVeto(c.snap)
		if !res.Vetoed() {
			t.Errorf("%s: want vetoed", c.name)
			continue
		}
		if len(res.Strands) != 1 || !strings.Contains(res.Strands[0], c.want) {
			t.Errorf("%s: strands %v, want one containing %q", c.name, res.Strands, c.want)
		}
	}
}

// TestSleepVeto_NeedsDecompositionNotStrand asserts NeedsDecomposition is never a
// veto strand (vetoCheck never tested it).
func TestSleepVeto_NeedsDecompositionNotStrand(t *testing.T) {
	t.Parallel()
	res := SleepVeto(DrainSnapshot{NeedsDecomposition: 5})
	if res.Vetoed() {
		t.Fatalf("needs-decomposition only: got vetoed (%v), want not vetoed", res.Strands)
	}
}
