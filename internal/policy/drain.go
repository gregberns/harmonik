package policy

import "fmt"

// drain.go — the pure quiesce/drain DECISION predicates: the drain-state
// classification and the sleep-veto strand logic for the fleet quiesce arbiter.
//
// Moved out of internal/daemon (hk-zqb3 / M5 slice 2 sub-slice B2) WITHOUT
// semantic change. The daemon shell still owns every effect: it runs
// GatherDrainFacts (br-ready reads, worktree readdir, ledger, oracle), projects
// the resulting FleetFacts into the narrow DrainSnapshot below at the call site,
// then acts on the returned decision (builds the DrainResult / the veto error,
// parks sessions, nudges panes). Only the classification/veto DECISION — "does
// the fleet have work?", "should we veto sleeping, and why?" — lives here.
//
// Snapshot rationale (B2, narrow projection): FleetFacts and its axis sub-types
// stay in internal/daemon. Rather than relocate them into internal/core, the
// daemon projects the exact scalar counts the predicates read into
// DrainSnapshot. The three source predicates read ONLY these fields:
//
//   - vetoCheck (quiesce.go):        Unsure, UnsureReasons, Ready.Count,
//     InProgress.Count, Runs.RegistryCount, Runs.LiveWorktrees, Queued.Count,
//     len(Queued.PausedQueues), len(Queued.FailedArchives), len(BlockedByOpenEpic).
//   - GenuineDrain (draindetect.go): the same set minus UnsureReasons (the
//     daemon shell reattaches reasons), and NOT NeedsDecomposition.
//   - hasLatentWork (stategather.go): the GenuineDrain set PLUS
//     len(NeedsDecomposition).
//
// Spec ref: codename:sleep-wake (SS-INV-005 veto gate) + the GenuineDrain
// five-defense drain oracle (§4.2 latent-work).

// DrainState is the tri-state drain classification returned by ClassifyDrain.
// The string values are identical to the daemon's legacy DrainState so the
// daemon shell can map 1:1 without changing any frozen wire or log text.
type DrainState string

const (
	// DrainStateDrained means no work on any axis and the read was confident.
	DrainStateDrained DrainState = "DRAINED"
	// DrainStateHasWork means at least one work axis is non-empty.
	DrainStateHasWork DrainState = "HAS_WORK"
	// DrainStateUnsure means a read error / transient inconsistency made the
	// fleet state uncertain; callers stay awake (fail-closed).
	DrainStateUnsure DrainState = "UNSURE"
)

// DrainSnapshot is the daemon's narrow projection of FleetFacts into the exact
// scalar values the drain/veto predicates read. The daemon builds it at the
// call site (see internal/daemon/draindetect.go GenuineDrain and
// internal/daemon/quiesce.go vetoCheck), keeping FleetFacts and its axis
// sub-types out of the pure package.
//
// Each field maps 1:1 to a FleetFacts read:
//
//	ReadyCount          = FleetFacts.Ready.Count
//	InProgressCount     = FleetFacts.InProgress.Count
//	RegistryRuns        = FleetFacts.Runs.RegistryCount
//	LiveWorktrees       = FleetFacts.Runs.LiveWorktrees
//	QueuedCount         = FleetFacts.Queued.Count
//	PausedQueues        = len(FleetFacts.Queued.PausedQueues)
//	FailedArchives      = len(FleetFacts.Queued.FailedArchives)
//	BlockedByOpenEpic   = len(FleetFacts.BlockedByOpenEpic)
//	NeedsDecomposition  = len(FleetFacts.NeedsDecomposition)  // HasLatentWork only
//	Unsure              = FleetFacts.Unsure
//	UnsureReasons       = FleetFacts.UnsureReasons
type DrainSnapshot struct {
	ReadyCount         int
	InProgressCount    int
	RegistryRuns       int
	LiveWorktrees      int
	QueuedCount        int
	PausedQueues       int
	FailedArchives     int
	BlockedByOpenEpic  int
	NeedsDecomposition int
	Unsure             bool
	UnsureReasons      []string
}

// hasDispatchableOrInFlightWork is the shared "any active-work axis non-empty?"
// test used by ClassifyDrain and the sleep-veto. It mirrors GenuineDrain's
// hasWork expression EXACTLY (the eight axes, in order) and deliberately does
// NOT count NeedsDecomposition — that generative bucket is a HAS_LATENT_WORK
// signal (see HasLatentWork), never a GenuineDrain / sleep-veto strand.
func hasDispatchableOrInFlightWork(s DrainSnapshot) bool {
	return s.ReadyCount > 0 ||
		s.InProgressCount > 0 ||
		s.RegistryRuns > 0 ||
		s.LiveWorktrees > 0 ||
		s.QueuedCount > 0 ||
		s.PausedQueues > 0 ||
		s.FailedArchives > 0 ||
		s.BlockedByOpenEpic > 0
}

// ClassifyDrain renders the tri-state drain verdict. Semantics preserved EXACTLY
// from internal/daemon/draindetect.go GenuineDrain (the non-error path):
//
//   - Unsure → UNSURE (fail-closed; the daemon shell reattaches UnsureReasons
//     and any GatherDrainFacts error onto its DrainResult).
//   - any work axis non-empty → HAS_WORK.
//   - all axes empty, not Unsure → DRAINED.
//
// NeedsDecomposition is intentionally excluded (GenuineDrain dropped it).
func ClassifyDrain(s DrainSnapshot) DrainState {
	if s.Unsure {
		return DrainStateUnsure
	}
	if hasDispatchableOrInFlightWork(s) {
		return DrainStateHasWork
	}
	return DrainStateDrained
}

// HasLatentWork mirrors internal/daemon/stategather.go hasLatentWork EXACTLY:
// Unsure counts as latent work, and the work set INCLUDES NeedsDecomposition
// (the one axis where it diverges from GenuineDrain / ClassifyDrain).
func HasLatentWork(s DrainSnapshot) bool {
	if s.Unsure {
		return true
	}
	return hasDispatchableOrInFlightWork(s) || s.NeedsDecomposition > 0
}

// VetoResult is the value-out of SleepVeto: the fleet-state uncertainty flag
// (with its reasons) and the ordered list of strand descriptions. The daemon
// shell turns this into the exact "sleep vetoed: ..." errors it returned before
// (see quiesce.go vetoCheck).
type VetoResult struct {
	// Unsure is true when the fleet state is uncertain (fail-closed veto). When
	// set, Strands is always empty — the original vetoCheck short-circuits on
	// Unsure before computing any strands.
	Unsure bool
	// UnsureReasons carries the read-quality caveats for the uncertain error.
	UnsureReasons []string
	// Strands names each dispatchable/in-flight axis that would be stranded by a
	// sleep, in the EXACT order and phrasing vetoCheck produced.
	Strands []string
}

// Vetoed reports whether the sleep request should be refused.
func (r VetoResult) Vetoed() bool {
	return r.Unsure || len(r.Strands) > 0
}

// SleepVeto is the pure SS-INV-005 veto decision. Semantics preserved EXACTLY
// from internal/daemon/quiesce.go vetoCheck:
//
//   - Unsure is checked FIRST and short-circuits: a VetoResult{Unsure:true} is
//     returned with the reasons, and no strands are computed.
//   - Otherwise each dispatchable/in-flight axis is tested in the same order and
//     appended as a strand string when non-zero. Zero strands → not vetoed.
//
// NeedsDecomposition is NOT a strand (vetoCheck never tested it).
func SleepVeto(s DrainSnapshot) VetoResult {
	if s.Unsure {
		return VetoResult{Unsure: true, UnsureReasons: s.UnsureReasons}
	}
	var strands []string
	if s.ReadyCount > 0 {
		strands = append(strands, fmt.Sprintf("%d ready bead(s)", s.ReadyCount))
	}
	if s.InProgressCount > 0 {
		strands = append(strands, fmt.Sprintf("%d in-progress bead(s)", s.InProgressCount))
	}
	if s.RegistryRuns > 0 {
		strands = append(strands, fmt.Sprintf("%d in-flight run(s)", s.RegistryRuns))
	}
	if s.LiveWorktrees > 0 {
		strands = append(strands, fmt.Sprintf("%d live worktree(s)", s.LiveWorktrees))
	}
	if s.QueuedCount > 0 {
		strands = append(strands, fmt.Sprintf("%d queued item(s)", s.QueuedCount))
	}
	if s.PausedQueues > 0 {
		strands = append(strands, fmt.Sprintf("%d paused queue(s)", s.PausedQueues))
	}
	if s.FailedArchives > 0 {
		strands = append(strands, fmt.Sprintf("%d failed archive(s)", s.FailedArchives))
	}
	if s.BlockedByOpenEpic > 0 {
		strands = append(strands, fmt.Sprintf("%d bead(s) blocked by open epic(s)", s.BlockedByOpenEpic))
	}
	return VetoResult{Strands: strands}
}
