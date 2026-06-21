package daemon

// draindetect.go — drain-fact oracle (hk-95uf / hk-pfr4, epic hk-rl4b /
// codename:sleep-wake, fleet-state initiative).
//
// GatherDrainFacts is the M0 fact-tool: it reads every work axis and returns a
// typed FleetFacts bundle (counts + ids per axis). It is a FACT reporter, not
// a decision-maker. The captain (LLM) decides whether to sleep; Go only vetoes
// execution (P1-c).
//
// Five false-negative defenses are preserved from GenuineDrain:
//  1. br-ready pagination — ReadyAll (`br ready --limit 0`); paginated Ready is
//     never trusted.
//  2. ledger-dep gating — deferred-for-ledger-dep queue items; open-epic →
//     otherwise-ready-but-blocked child edges.
//  3. paused-by-failure — paused-by-* queue statuses; un-reconciled
//     `.json.failed-*` archives scanned directly (bypassing archive filter).
//  4. in-flight runs — RunRegistry.Len() > 0 OR live `.harmonik/worktrees/*`.
//  5. kerf-next-empty — oracle MUST NOT consult `kerf next`; `br ready
//     --limit 0` + the ledger are authoritative.
//
// GenuineDrain remains as a backward-compat bridge wrapper over GatherDrainFacts
// for callers in quiesce.go until P1-b (hk-kj7d) removes the auto-park tick.
//
// NOTE ON RECEIVER: the oracle spec names `func (d *Daemon) GenuineDrain`, but
// the harmonik daemon is a composition-root FUNCTION (daemon.Start), not a
// `Daemon` struct. DrainDetector is the honest receiver: a small value bundling
// exactly the dependency seams the predicate needs, constructable from the same
// shared instances Start() already builds. M1 constructs one via NewDrainDetector.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// ---------------------------------------------------------------------------
// Fleet facts types (GatherDrainFacts output)
// ---------------------------------------------------------------------------

// FleetFacts is the read-only fact bundle the captain reads to decide whether
// to wind the fleet down. It reports facts per axis as counts + lists; it never
// renders a DRAINED/HAS_WORK decision and never short-circuits at the first
// sign of work. The ZFC invariant: facts, never a decision; generative
// categories are flagged, not scored. Produced by GatherDrainFacts.
type FleetFacts struct {
	// Dispatchable-now (defenses #1, #5: br ready --limit 0).
	Ready BeadAxis `json:"ready"`

	// In-flight work (defense #4).
	InProgress BeadAxis `json:"in_progress"` // beads the ledger reports in_progress
	Runs       RunAxis  `json:"runs"`        // RunRegistry + live worktrees

	// Lined-up / queued-but-not-yet-dispatchable (defenses #2 queue, #3).
	Queued QueueAxis `json:"queued"`

	// Standalone blocked-by-an-open-epic (defense #2 epic).
	BlockedByOpenEpic []EpicBlockEdge `json:"blocked_by_open_epic"`

	// The other dropped buckets (not dispatchable, not lost).
	NeedsAttention BeadAxis `json:"needs_attention"` // needs-attention label set
	Draft          BeadAxis `json:"draft"`           // loaded-but-not-dispatchable
	Deferred       BeadAxis `json:"deferred"`        // operator-deferred

	// The ONE generative category: flagged, never scored.
	// Childless OPEN epics — the captain decides what to do with them.
	NeedsDecomposition []core.BeadID `json:"needs_decomposition"`

	// Read quality (NOT a control signal).
	// Unsure is true when any axis hit a read error or a transient
	// inconsistency (e.g. QM "all items terminal, status not yet rolled"
	// race, or unrecognised queue status, or nil epic seam). It is a
	// data-quality caveat, not a verdict and not a license/veto.
	Unsure        bool     `json:"unsure"`
	UnsureReasons []string `json:"unsure_reasons,omitempty"`

	// GatheredAt is the wall-clock read time, for staleness reasoning.
	GatheredAt time.Time `json:"gathered_at"`
}

// markUnsure appends reason to UnsureReasons and sets Unsure = true.
func (f *FleetFacts) markUnsure(reason string) {
	f.Unsure = true
	f.UnsureReasons = append(f.UnsureReasons, reason)
}

// BeadAxis is one bead bucket: a count plus the bead facts that compose it.
// Count == len(Beads) by construction; both are emitted so a JSON consumer can
// read the count without walking the list.
type BeadAxis struct {
	Count int        `json:"count"`
	Beads []BeadFact `json:"beads"`
}

// BeadFact is the minimal per-bead fact (id + human context). It is a
// projection of core.BeadRecord: id + title + type + the labels that explain
// why the bead landed in its axis.
type BeadFact struct {
	ID     core.BeadID `json:"id"`
	Title  string      `json:"title"`
	Type   string      `json:"type"`
	Labels []string    `json:"labels,omitempty"`
}

// RunAxis is the in-flight-runs axis (defense #4): registry runs + live
// worktree dirs, reported separately so a stale-worktree-with-empty-registry
// case is legible.
type RunAxis struct {
	RegistryCount int      `json:"registry_count"` // RunRegistry.Len()
	LiveWorktrees int      `json:"live_worktrees"` // entries under .harmonik/worktrees
	WorktreePaths []string `json:"worktree_paths,omitempty"`
}

// QueueAxis is the lined-up / queued-but-not-dispatchable axis (defenses #2
// queue-portion, #3). PausedQueues and FailedArchives are the defense-#3
// hold-the-fleet-awake signals reported as facts.
type QueueAxis struct {
	NonTerminalItems []QueueItemFact `json:"non_terminal_items"`
	PausedQueues     []PausedQueue   `json:"paused_queues"`   // paused-by-failure/-drain/-budget
	FailedArchives   []string        `json:"failed_archives"` // un-reconciled *.json.failed-*
	Count            int             `json:"count"`           // total non-terminal items
}

// QueueItemFact is one non-terminal queue item: which queue, which bead, and
// its item status (pending / dispatched / deferred-for-ledger-dep / …).
type QueueItemFact struct {
	Queue  string `json:"queue"`
	BeadID string `json:"bead_id"`
	Status string `json:"status"`
}

// PausedQueue names a paused queue and the pause reason (the QueueStatus value).
type PausedQueue struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// EpicBlockEdge is one open-epic → otherwise-ready-child blocks edge
// (defense #2 epic-portion). The child is genuine pending work `br ready`
// cannot see.
type EpicBlockEdge struct {
	EpicID  core.BeadID `json:"epic_id"`
	ChildID core.BeadID `json:"child_id"`
}

// beadToFact converts a core.BeadRecord to a BeadFact projection.
func beadToFact(b core.BeadRecord) BeadFact {
	return BeadFact{
		ID:     b.BeadID,
		Title:  b.Title,
		Type:   b.BeadType,
		Labels: b.Labels,
	}
}

// ---------------------------------------------------------------------------
// Backward-compat verdict types (used by the GenuineDrain bridge wrapper)
// ---------------------------------------------------------------------------

// DrainState is the tri-state verdict of the GenuineDrain bridge wrapper.
// The DRAINED state is no longer a control signal in GatherDrainFacts; it
// exists here only for the bridge callers in quiesce.go until P1-b (hk-kj7d)
// and P1-c (hk-zqb3) land and remove those callers.
type DrainState string

const (
	DrainStateDrained DrainState = "DRAINED"
	DrainStateHasWork DrainState = "HAS_WORK"
	DrainStateUnsure  DrainState = "UNSURE"
)

// DrainResult is the verdict plus human-readable reasons, used by GenuineDrain.
type DrainResult struct {
	State   DrainState
	Reasons []string
}

// ---------------------------------------------------------------------------
// Dependency seams
// ---------------------------------------------------------------------------

// readySource is the br-ready seam. The production implementation is
// *brcli.Adapter via its ReadyAll method (`br ready --limit 0`); tests inject a
// fake. ONLY ReadyAll is exposed — defense #1 forbids trusting the default-
// paginated Ready.
type readySource interface {
	ReadyAll(ctx context.Context) ([]core.BeadRecord, error)
}

// openBeadLister enumerates beads by ledger status. The production
// implementation is *brcli.Adapter.ListBeadsByStatus; the ledger axis
// (draindetect_epic.go) uses it to enumerate open epics.
type openBeadLister interface {
	ListBeadsByStatus(ctx context.Context, status string) ([]core.BeadRecord, error)
}

// ---------------------------------------------------------------------------
// DrainDetector
// ---------------------------------------------------------------------------

// DrainDetector bundles the dependency seams the drain oracle reads.
// Construct one with NewDrainDetector from the daemon composition root's shared
// instances. The zero value is NOT valid.
type DrainDetector struct {
	ready      readySource
	lister     openBeadLister
	ledger     queue.BeadLedger
	runs       *RunRegistry
	queues     *QueueStore
	projectDir string
}

// NewDrainDetector wires a DrainDetector from the shared daemon instances.
//
//   - ready: the br-ready adapter exposing ReadyAll (defense #1).
//   - lister: the br adapter exposing ListBeadsByStatus (ledger/epic axis).
//   - ledger: the queue.BeadLedger bridge exposing BlocksEdge (defense #2).
//   - runs: the shared in-flight RunRegistry (defense #4).
//   - queues: the shared QueueStore (defenses #2, #3 in-memory portion).
//   - projectDir: the project root, for the `.harmonik/queues/*.json.failed-*`
//     archive scan (defense #3) and the `.harmonik/worktrees/*` live-run scan
//     (defense #4).
func NewDrainDetector(
	ready readySource,
	lister openBeadLister,
	ledger queue.BeadLedger,
	runs *RunRegistry,
	queues *QueueStore,
	projectDir string,
) *DrainDetector {
	return &DrainDetector{
		ready:      ready,
		lister:     lister,
		ledger:     ledger,
		runs:       runs,
		queues:     queues,
		projectDir: projectDir,
	}
}

// ---------------------------------------------------------------------------
// GatherDrainFacts — the new fact-tool (P1-a, hk-pfr4)
// ---------------------------------------------------------------------------

// GatherDrainFacts reads every drain axis and returns a typed FleetFacts
// bundle. It is a fact reporter, not a decider: it never short-circuits at the
// first sign of work, never renders a DRAINED/HAS_WORK verdict, and never
// collapses to a single boolean. All 5 false-negative defenses are preserved
// in the per-axis sourcing.
//
// Axis read errors set facts.Unsure = true and continue — the captain receives
// the partial bundle. The error return is non-nil for the first hard axis error
// encountered (so the bridge GenuineDrain can surface it to quiesce.go callers
// until P1-b/P1-c land). If only transient races (not errors) are encountered,
// error is nil but facts.Unsure is true.
func (d *DrainDetector) GatherDrainFacts(ctx context.Context) (*FleetFacts, error) {
	facts := &FleetFacts{GatheredAt: time.Now()}
	var firstErr error

	captureErr := func(reason string, err error) {
		facts.markUnsure(reason)
		if firstErr == nil {
			firstErr = err
		}
	}

	// Axis: failed archives (defense #3 on-disk).
	// Direct glob — bypasses EnumerateQueueNames which filters out archives
	// (an un-reconciled archive is pending work and must not be hidden).
	archives, err := d.failedArchives()
	if err != nil {
		captureErr("failed-archive scan error: "+err.Error(), err)
	} else {
		facts.Queued.FailedArchives = archives
	}

	// Axis: queue items (defenses #2 queue-portion, #3 in-memory).
	d.collectQueueFacts(facts)

	// Axis: in-flight runs (defense #4).
	facts.Runs.RegistryCount = d.runs.Len()
	paths, err := d.liveWorktreeList()
	if err != nil {
		captureErr("worktree scan error: "+err.Error(), err)
	} else {
		facts.Runs.LiveWorktrees = len(paths)
		facts.Runs.WorktreePaths = paths
	}

	// Axis: br ready --limit 0 (defenses #1, #5). Authoritative dispatchable set.
	ready, err := d.ready.ReadyAll(ctx)
	if err != nil {
		captureErr("br ready --limit 0 error: "+err.Error(), err)
	} else {
		for _, b := range ready {
			facts.Ready.Beads = append(facts.Ready.Beads, beadToFact(b))
		}
		facts.Ready.Count = len(facts.Ready.Beads)
	}

	// Ledger-based axes require the lister seam; fail-closed if not wired.
	if d.lister == nil || d.ledger == nil {
		facts.markUnsure("ledger/epic axis not wired (lister/ledger nil)")
		return facts, firstErr
	}

	// Axis: in_progress beads.
	inprog, err := d.lister.ListBeadsByStatus(ctx, string(core.CoarseStatusInProgress))
	if err != nil {
		captureErr("br list in_progress error: "+err.Error(), err)
	} else {
		for _, b := range inprog {
			facts.InProgress.Beads = append(facts.InProgress.Beads, beadToFact(b))
		}
		facts.InProgress.Count = len(facts.InProgress.Beads)
	}

	// Axis: draft beads.
	draft, err := d.lister.ListBeadsByStatus(ctx, string(core.CoarseStatusDraft))
	if err != nil {
		captureErr("br list draft error: "+err.Error(), err)
	} else {
		for _, b := range draft {
			facts.Draft.Beads = append(facts.Draft.Beads, beadToFact(b))
		}
		facts.Draft.Count = len(facts.Draft.Beads)
	}

	// Axis: deferred beads.
	deferred, err := d.lister.ListBeadsByStatus(ctx, string(core.CoarseStatusDeferred))
	if err != nil {
		captureErr("br list deferred error: "+err.Error(), err)
	} else {
		for _, b := range deferred {
			facts.Deferred.Beads = append(facts.Deferred.Beads, beadToFact(b))
		}
		facts.Deferred.Count = len(facts.Deferred.Beads)
	}

	// Fetch open+blocked once; both epic-facts and needs-attention need them.
	open, err := d.lister.ListBeadsByStatus(ctx, string(core.CoarseStatusOpen))
	if err != nil {
		captureErr("br list open error: "+err.Error(), err)
		return facts, firstErr
	}
	blocked, err := d.lister.ListBeadsByStatus(ctx, string(core.CoarseStatusBlocked))
	if err != nil {
		captureErr("br list blocked error: "+err.Error(), err)
		return facts, firstErr
	}

	// Axis: BlockedByOpenEpic (defense #2 epic) + NeedsDecomposition —
	// one pass over open epics to avoid double ledger round-trips.
	epicBlocked, needsDecomp, epicErr := d.gatherEpicFacts(ctx, open, blocked)
	if epicErr != nil {
		captureErr("epic-facts scan error: "+epicErr.Error(), epicErr)
	} else {
		facts.BlockedByOpenEpic = epicBlocked
		facts.NeedsDecomposition = needsDecomp
	}

	// Axis: NeedsAttention — open ∪ blocked beads carrying the needs-attention
	// label. ReadyAll already silently excludes these (BI-013a); this axis makes
	// the exclusion visible instead of dropping the beads on the floor.
	for _, b := range append(open, blocked...) {
		if hasNeedsAttentionLabel(b.Labels) {
			facts.NeedsAttention.Beads = append(facts.NeedsAttention.Beads, beadToFact(b))
		}
	}
	facts.NeedsAttention.Count = len(facts.NeedsAttention.Beads)

	return facts, firstErr
}

// hasNeedsAttentionLabel reports whether the needs-attention label appears in
// the given label slice.
func hasNeedsAttentionLabel(labels []string) bool {
	for _, l := range labels {
		if l == labelNeedsAttention {
			return true
		}
	}
	return false
}

// labelNeedsAttention is the Beads label constant shared with brcli/ready.go.
const labelNeedsAttention = "needs-attention"

// collectQueueFacts walks the in-memory QueueStore and populates
// facts.Queued.NonTerminalItems, facts.Queued.PausedQueues, and
// facts.Queued.Count. The QM race (all items terminal but queue status not
// yet rolled) sets facts.Unsure rather than short-circuiting — the bundle
// must be complete even under transient inconsistency.
func (d *DrainDetector) collectQueueFacts(facts *FleetFacts) {
	for name, q := range d.queues.AllQueues() {
		if q == nil {
			continue
		}
		switch q.Status {
		case queue.QueueStatusPausedByFailure,
			queue.QueueStatusPausedByDrain,
			queue.QueueStatusPausedByBudget:
			facts.Queued.PausedQueues = append(facts.Queued.PausedQueues, PausedQueue{
				Name:   name,
				Status: string(q.Status),
			})
			continue
		case queue.QueueStatusCompleted, queue.QueueStatusCancelled:
			continue
		case queue.QueueStatusActive:
			// Inspect items below.
		default:
			facts.markUnsure(fmt.Sprintf("queue %q has unrecognised status %q", name, q.Status))
			continue
		}

		nonTerminal := 0
		for gi := range q.Groups {
			for ii := range q.Groups[gi].Items {
				it := &q.Groups[gi].Items[ii]
				switch it.Status {
				case queue.ItemStatusCompleted, queue.ItemStatusFailed:
					// terminal — drained for this item
				default:
					nonTerminal++
					facts.Queued.NonTerminalItems = append(facts.Queued.NonTerminalItems, QueueItemFact{
						Queue:  name,
						BeadID: string(it.BeadID),
						Status: string(it.Status),
					})
				}
			}
		}

		// QM race: every item terminal but the queue status has not yet rolled.
		if nonTerminal == 0 {
			facts.markUnsure(fmt.Sprintf(
				"queue %q is active with all items terminal (status not yet rolled)", name))
		}
	}
	facts.Queued.Count = len(facts.Queued.NonTerminalItems)
}

// failedArchives returns the paths of all un-reconciled
// `.harmonik/queues/*.json.failed-*` archive files. Defense #3 requires
// scanning DIRECTLY rather than via EnumerateQueueNames (which filters them
// out): an un-reconciled failed archive is pending operator work.
func (d *DrainDetector) failedArchives() ([]string, error) {
	pattern := filepath.Join(d.projectDir, ".harmonik", "queues", "*.json.failed-*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob %q: %w", pattern, err)
	}
	return matches, nil
}

// liveWorktreeList returns the paths of all entries under
// `.harmonik/worktrees`. Any entry is treated, fail-closed, as a live
// in-progress run (defense #4). A missing directory means zero live runs.
func (d *DrainDetector) liveWorktreeList() ([]string, error) {
	dir := filepath.Join(d.projectDir, ".harmonik", "worktrees")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("readdir %q: %w", dir, err)
	}
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		paths = append(paths, filepath.Join(dir, e.Name()))
	}
	return paths, nil
}

// ---------------------------------------------------------------------------
// GenuineDrain — backward-compat bridge wrapper (P1-b / P1-c will remove it)
// ---------------------------------------------------------------------------

// GenuineDrain calls GatherDrainFacts and derives the legacy DrainResult
// verdict for callers in quiesce.go. It will be removed when P1-b (hk-kj7d)
// deletes the auto-park tick that calls it.
//
// Bridge semantics:
//   - facts.Unsure || any axis error → UNSURE (fail-closed, stays awake).
//   - any work axis non-empty         → HAS_WORK.
//   - all axes empty, not Unsure      → DRAINED.
func (d *DrainDetector) GenuineDrain(ctx context.Context) (DrainResult, error) {
	facts, err := d.GatherDrainFacts(ctx)
	if err != nil {
		return DrainResult{
			State:   DrainStateUnsure,
			Reasons: append(facts.UnsureReasons, "GatherDrainFacts error: "+err.Error()),
		}, err
	}
	if facts.Unsure {
		return DrainResult{State: DrainStateUnsure, Reasons: facts.UnsureReasons}, nil
	}

	hasWork := facts.Ready.Count > 0 ||
		facts.InProgress.Count > 0 ||
		facts.Runs.RegistryCount > 0 ||
		facts.Runs.LiveWorktrees > 0 ||
		facts.Queued.Count > 0 ||
		len(facts.Queued.PausedQueues) > 0 ||
		len(facts.Queued.FailedArchives) > 0 ||
		len(facts.BlockedByOpenEpic) > 0

	if hasWork {
		return DrainResult{State: DrainStateHasWork}, nil
	}
	return DrainResult{State: DrainStateDrained}, nil
}

// unsure builds an UNSURE DrainResult carrying reason. Used by GenuineDrain
// tests that rely on the legacy helper.
func unsure(reason string) DrainResult {
	return DrainResult{State: DrainStateUnsure, Reasons: []string{reason}}
}
