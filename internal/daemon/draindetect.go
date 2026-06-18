package daemon

// draindetect.go — the genuine-drain oracle (hk-95uf, epic hk-rl4b /
// codename:sleep-wake).
//
// GenuineDrain is the load-bearing safety interlock for the fleet sleep/wake
// idle-down feature (M0). The fleet may put long-lived LLM sessions to sleep
// ONLY when bead work is TRULY drained. The captain historically
// false-declares "no work" via several false-negative sources; a false "no
// work" sleep stalls the fleet with ready beads pending — the #1 failure mode.
//
// GenuineDrain is a deterministic predicate that is correct against ALL known
// false-negative sources and FAIL-CLOSED on any doubt:
//
//   - DRAINED requires positive emptiness evidence on EVERY axis.
//   - HAS_WORK is returned the moment any axis shows pending/in-flight work.
//   - UNSURE is returned on any evaluation error OR the "all items terminal but
//     queue status not yet rolled" race. UNSURE and HAS_WORK both keep the
//     fleet AWAKE; only DRAINED licenses sleep.
//
// This is M0: a pure work-detection predicate. It introduces ZERO threshold /
// band knobs and MUST NOT read or alter any keeper warn/act/force/window value
// (operator HARD-NO). Sleep-grace / wake-trigger / bands are POLICY, deferred
// to a later pass (M1 wires this predicate into a sleep decision).
//
// NOTE ON RECEIVER: the oracle spec names `func (d *Daemon) GenuineDrain`, but
// the harmonik daemon is a composition-root FUNCTION (daemon.Start), not a
// `Daemon` struct — there is no such type to hang the method on. DrainDetector
// is the honest receiver: a small value bundling exactly the dependency seams
// the predicate needs, constructable from the same shared instances Start()
// already builds (the *brcli.Adapter, the brQueueLedger bridge, the shared
// *RunRegistry and *QueueStore). M1 constructs one via NewDrainDetector.
//
// Five false-negative defenses, each a concrete check:
//  1. br-ready pagination — uses ReadyAll (`br ready --limit 0`), never the
//     default-paginated Ready (brcli/ready.go). A paginated empty is NOT
//     trusted.
//  2. ledger-dep gating — any queue item deferred-for-ledger-dep ⇒ HAS_WORK;
//     and every OPEN epic with a ready-but-epic-blocked child ⇒ HAS_WORK
//     (defense lives in the ledger axis, draindetect_epic; reuses BlocksEdge).
//  3. paused-by-failure — any paused-by-* queue status ⇒ HAS_WORK; and an
//     un-reconciled `.json.failed-*` archive on disk ⇒ HAS_WORK (scanned
//     DIRECTLY, bypassing EnumerateQueueNames' archive filter).
//  4. in-flight runs — RunRegistry.Len()==0 AND no live `.harmonik/worktrees/*`
//     run.
//  5. kerf-next-empty — the oracle MUST NOT consult `kerf next` (external,
//     reports false-empty for works lacking bead_filter). `br ready --limit 0`
//     is authoritative.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// DrainState is the tri-state verdict of GenuineDrain.
type DrainState string

const (
	// DrainStateDrained means EVERY axis shows positive emptiness: no ready
	// beads, no in-flight runs, no non-terminal queue items, no paused/failed
	// queues, no open-epic-blocked ready children. The caller MAY sleep.
	DrainStateDrained DrainState = "DRAINED"

	// DrainStateHasWork means at least one axis shows pending or in-flight work.
	// The caller MUST stay awake.
	DrainStateHasWork DrainState = "HAS_WORK"

	// DrainStateUnsure means an evaluation error occurred, or a transient
	// inconsistency (the "all items terminal but queue status not yet rolled"
	// race) was observed. The caller MUST stay awake — sleeping requires
	// positive emptiness evidence, which UNSURE does not provide.
	DrainStateUnsure DrainState = "UNSURE"
)

// DrainResult is the verdict plus human-readable reasons. Reasons is purely
// diagnostic (for logging / operator surfaces); control flow keys off State.
type DrainResult struct {
	State   DrainState
	Reasons []string
}

// readySource is the br-ready seam. The production implementation is
// *brcli.Adapter via its ReadyAll method (`br ready --limit 0`); tests inject a
// fake. ONLY ReadyAll is exposed here — defense #1 forbids trusting the
// default-paginated Ready.
type readySource interface {
	ReadyAll(ctx context.Context) ([]core.BeadRecord, error)
}

// openBeadLister enumerates beads by ledger status. The production
// implementation is *brcli.Adapter.ListBeadsByStatus; the ledger axis
// (draindetect_epic.go, Phase B) uses it to enumerate open epics. Defined here
// so the DrainDetector constructor is stable across both phases.
type openBeadLister interface {
	ListBeadsByStatus(ctx context.Context, status string) ([]core.BeadRecord, error)
}

// DrainDetector bundles the dependency seams the genuine-drain oracle reads.
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

// GenuineDrain evaluates every drain axis and returns the fail-closed verdict.
//
// Evaluation order is fixed for deterministic reasons output. The first axis to
// observe an evaluation ERROR returns (UNSURE, err) immediately — staying awake
// is the default on any doubt. The "all items terminal but queue status not yet
// rolled" race returns (UNSURE, nil). Otherwise HAS_WORK reasons accumulate
// across axes; if any axis showed work the verdict is HAS_WORK, else DRAINED.
//
// The caller (M1) sleeps ONLY on State == DRAINED.
func (d *DrainDetector) GenuineDrain(ctx context.Context) (DrainResult, error) {
	res := DrainResult{State: DrainStateDrained}

	// Axis: paused-by-* / un-reconciled failed-archive (defense #3).
	archives, err := d.failedArchives()
	if err != nil {
		return unsure("failed-archive scan error: " + err.Error()), err
	}
	for _, a := range archives {
		res.flagWork(fmt.Sprintf("un-reconciled failed-queue archive on disk: %s", filepath.Base(a)))
	}

	// Axis: queue items (defenses #2 deferred-portion, #3 paused-portion).
	qres := d.scanQueues()
	if qres.State == DrainStateUnsure {
		return qres, nil
	}
	res.merge(qres)

	// Axis: in-flight runs (defense #4).
	if n := d.runs.Len(); n > 0 {
		res.flagWork(fmt.Sprintf("%d in-flight run(s) in RunRegistry", n))
	}
	live, err := d.liveWorktrees()
	if err != nil {
		return unsure("worktree scan error: " + err.Error()), err
	}
	if live > 0 {
		res.flagWork(fmt.Sprintf("%d live worktree(s) under .harmonik/worktrees", live))
	}

	// Axis: br ready --limit 0 (defenses #1, #5). Authoritative dispatchable set.
	ready, err := d.ready.ReadyAll(ctx)
	if err != nil {
		return unsure("br ready --limit 0 error: " + err.Error()), err
	}
	if len(ready) > 0 {
		res.flagWork(fmt.Sprintf("%d dispatchable bead(s) from br ready --limit 0", len(ready)))
	}

	// Ledger/epic axis (defense #2 epic-portion) — Phase B, draindetect_epic.go.
	eres, err := d.scanOpenEpics(ctx)
	if err != nil {
		return unsure("open-epic scan error: " + err.Error()), err
	}
	res.merge(eres)

	return res, nil
}

// scanQueues evaluates the in-memory QueueStore for paused queues, non-terminal
// items, and the terminal-but-unrolled race. Returns a DrainResult whose State
// is HAS_WORK / DRAINED, or UNSURE for the race or an unrecognised status.
func (d *DrainDetector) scanQueues() DrainResult {
	res := DrainResult{State: DrainStateDrained}
	for name, q := range d.queues.AllQueues() {
		if q == nil {
			continue
		}
		switch q.Status {
		case queue.QueueStatusPausedByFailure,
			queue.QueueStatusPausedByDrain,
			queue.QueueStatusPausedByBudget:
			res.flagWork(fmt.Sprintf("queue %q is %s", name, q.Status))
			continue
		case queue.QueueStatusCompleted, queue.QueueStatusCancelled:
			// Terminal queue — no work from this slot.
			continue
		case queue.QueueStatusActive:
			// Inspect items below.
		default:
			// Unrecognised status — fail-closed toward doubt.
			return unsure(fmt.Sprintf("queue %q has unrecognised status %q", name, q.Status))
		}

		nonTerminal := 0
		for gi := range q.Groups {
			for ii := range q.Groups[gi].Items {
				it := &q.Groups[gi].Items[ii]
				switch it.Status {
				case queue.ItemStatusCompleted, queue.ItemStatusFailed:
					// terminal — drained for this item
				case queue.ItemStatusDeferredForLedgerDep:
					nonTerminal++
					res.flagWork(fmt.Sprintf("queue %q item %s deferred-for-ledger-dep", name, it.BeadID))
				default: // pending, dispatched, or any future non-terminal status
					nonTerminal++
					res.flagWork(fmt.Sprintf("queue %q item %s is %s", name, it.BeadID, it.Status))
				}
			}
		}

		// QM race: every item terminal but the queue status has not yet rolled
		// to completed/cancelled. Treat as UNSURE — emptiness is not yet final.
		if nonTerminal == 0 {
			return unsure(fmt.Sprintf("queue %q is active with all items terminal (status not yet rolled)", name))
		}
	}
	return res
}

// failedArchives returns the paths of all un-reconciled
// `.harmonik/queues/*.json.failed-*` archive files. Per defense #3 these are
// scanned DIRECTLY rather than via EnumerateQueueNames (which filters them
// out): an un-reconciled failed archive is pending operator work and MUST keep
// the fleet awake.
func (d *DrainDetector) failedArchives() ([]string, error) {
	pattern := filepath.Join(d.projectDir, ".harmonik", "queues", "*.json.failed-*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob %q: %w", pattern, err)
	}
	return matches, nil
}

// liveWorktrees returns the count of entries under `.harmonik/worktrees`. Any
// entry is treated, fail-closed, as a live in-progress run (defense #4): the
// daemon removes a run's worktree on completion, so a non-empty worktrees
// directory means a run is in flight (or a stale worktree needs reconciling —
// either way, not drained). A missing directory means zero live runs.
func (d *DrainDetector) liveWorktrees() (int, error) {
	dir := filepath.Join(d.projectDir, ".harmonik", "worktrees")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("readdir %q: %w", dir, err)
	}
	return len(entries), nil
}

// unsure builds an UNSURE DrainResult carrying reason.
func unsure(reason string) DrainResult {
	return DrainResult{State: DrainStateUnsure, Reasons: []string{reason}}
}

// flagWork records a HAS_WORK reason and promotes the result to HAS_WORK unless
// it is already UNSURE (UNSURE dominates — both keep the fleet awake, but
// UNSURE signals doubt rather than confirmed work).
func (r *DrainResult) flagWork(reason string) {
	if r.State != DrainStateUnsure {
		r.State = DrainStateHasWork
	}
	r.Reasons = append(r.Reasons, reason)
}

// merge folds other into r. If other is UNSURE, r becomes UNSURE; else if other
// is HAS_WORK, r becomes HAS_WORK (unless already UNSURE). Reasons concatenate.
func (r *DrainResult) merge(other DrainResult) {
	switch other.State {
	case DrainStateUnsure:
		r.State = DrainStateUnsure
	case DrainStateHasWork:
		if r.State != DrainStateUnsure {
			r.State = DrainStateHasWork
		}
	}
	r.Reasons = append(r.Reasons, other.Reasons...)
}
