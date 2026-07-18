package daemon

// crewidlereap.go — SD-3: tear down crews that COMPLETED work and went idle,
// reclaiming their slot after a short grace window.
//
// CrewIdleReaper is a daemon-hosted periodic sweep, shaped after StaleWatcher
// (stalewatch.go): a ticker re-checks every crew registry record, and a crew
// only reaps after its bound queue has read queue.QueueStatusCompleted for at
// least GraceAfter — not on the first tick that sees it. The grace window
// exists so a captain re-tasking the same crew to the next-ranked lane (the
// common case right after an epic finishes) does not trigger a stop+respawn
// cycle for the slot it is about to reuse.
//
// Detection signal: a crew's bound queue (crew.Record.Queue) reads
// queue.QueueStatusCompleted — all groups reached complete-success, so there
// is no more ready or in-flight work for that crew. A crew whose queue has
// never been loaded (QueueByName returns nil) or is paused/active/cancelled
// is NEVER a reap candidate: absence of a completion signal must not read as
// "idle-completed" (mirrors the negative-test guard in
// plans/2026-07-02-stall-sentinel/DESIGN.md §2 Layer B — a correctly-idle
// crew must not be swept).
//
// Teardown reuses the exact operator crew-stop path (CrewHandler.HandleCrewStop):
// quit→grace→kill the pane/session, remove the .managed marker, remove the
// registry record.
//
// Plan ref: plans/2026-06-25-admiral-framework/PLAN-v2.md Part 5 "SD-3" +
// open question 2 ("recommend a short grace window").
// Bead ref: hk-s2eac.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/queue"
)

const (
	// crewIdleReapDefaultGrace is the default window a crew's bound queue
	// must continuously read QueueStatusCompleted before its slot is
	// reclaimed. Short enough to free the slot promptly; long enough that a
	// captain re-tasking the same crew right after an epic completes does not
	// trigger avoidable spawn churn.
	crewIdleReapDefaultGrace = 5 * time.Minute

	// crewIdleReapDefaultScanInterval is how often the sweep re-checks every
	// crew record. Small relative to GraceAfter so detection latency stays a
	// small fraction of the grace window itself.
	crewIdleReapDefaultScanInterval = 30 * time.Second
)

// crewStopper is the seam CrewIdleReaper uses to tear a crew down. Satisfied
// by CrewHandler.HandleCrewStop (crewstart.go); a test double may substitute
// any function matching this shape.
type crewStopper interface {
	HandleCrewStop(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)
}

// crewQueueLookup is the seam CrewIdleReaper uses to read a crew's bound
// queue. Satisfied by *QueueStore.QueueByName.
type crewQueueLookup interface {
	QueueByName(name string) *queue.Queue
}

// crewListFunc enumerates crew registry records. Satisfied by crew.List;
// overridable in tests.
type crewListFunc func(projectDir string) ([]crew.Record, error)

// CrewIdleReaperConfig holds the construction-time parameters for
// CrewIdleReaper.
type CrewIdleReaperConfig struct {
	// ProjectDir is the project root passed to ListCrews. Required for the
	// sweep to do anything; empty makes scan a no-op (unit-test mode).
	ProjectDir string

	// Queues resolves a crew's bound queue by name. Required for the sweep to
	// do anything; nil makes scan a no-op.
	Queues crewQueueLookup

	// Stopper tears a detected idle-completed crew down. Required for actual
	// teardown; nil skips the reap call but detection/tracking still runs so
	// tests can assert on candidacy without a real substrate.
	Stopper crewStopper

	// GraceAfter is how long a crew's bound queue must continuously read
	// QueueStatusCompleted before its slot is reclaimed. Zero →
	// crewIdleReapDefaultGrace.
	GraceAfter time.Duration

	// ScanInterval is how often the background goroutine re-checks every
	// crew record. Zero → crewIdleReapDefaultScanInterval.
	ScanInterval time.Duration

	// Now is the wall-clock source. Nil → time.Now.
	Now func() time.Time

	// ListCrews enumerates the crew registry. Nil → crew.List.
	ListCrews crewListFunc

	// PersistentType reports whether the given agent type is a persistent
	// oversight role (manifest lifecycle.persistent: true) that must never be
	// reaped. Nil → no crew is ever treated as persistent (unit-test mode); the
	// daemon wires this to an agentmanifest lookup.
	PersistentType func(typeName string) bool
}

// CrewIdleReaper periodically scans the crew registry and tears down any crew
// whose bound queue has read QueueStatusCompleted for at least GraceAfter.
type CrewIdleReaper struct {
	cfg CrewIdleReaperConfig

	mu sync.Mutex
	// doneSince maps crew name → the first tick its bound queue was observed
	// at QueueStatusCompleted. Cleared whenever the queue leaves Completed
	// (re-armed with new work) or the crew record disappears.
	doneSince map[string]time.Time
}

// NewCrewIdleReaper constructs a CrewIdleReaper from cfg, applying defaults
// for zero-valued duration fields.
func NewCrewIdleReaper(cfg CrewIdleReaperConfig) *CrewIdleReaper {
	if cfg.GraceAfter <= 0 {
		cfg.GraceAfter = crewIdleReapDefaultGrace
	}
	if cfg.ScanInterval <= 0 {
		cfg.ScanInterval = crewIdleReapDefaultScanInterval
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.ListCrews == nil {
		cfg.ListCrews = crew.List
	}
	return &CrewIdleReaper{
		cfg:       cfg,
		doneSince: make(map[string]time.Time),
	}
}

// StartWatcher launches the background scan goroutine. Returns immediately;
// the goroutine runs until ctx is cancelled.
//
// DISABLED (operator directive 2026-07-18): the SD-3 idle-completed-crew sweep
// was tearing down worker/gate crews (alpha, bravo, assessor) ~5 min after they
// drained their queue and went to QueueStatusCompleted, mid-standby. The operator
// wants idle crews with an empty/completed queue to STAY ALIVE. The sweep is
// turned fully off here: the scan goroutine is never started, so no crew is ever
// reclaimed for being idle. This does NOT touch legitimate cleanup — the operator
// crew-stop path (HandleCrewStop), the StaleWatcher wedged-run force-reap, the
// BranchReapWatcher, and the boot-time orphan sweep (RunOrphanSweep, which reaps
// sessions MISSING from the registry) are all unaffected. loop/scan/checkCrew/reap
// are retained (unreferenced) so re-enabling is a one-line revert if ever wanted.
func (r *CrewIdleReaper) StartWatcher(ctx context.Context) {
	// Idle-crew reaping is disabled; the sweep goroutine is never launched.
}

// loop is the background goroutine body.
//
//nolint:unused // retained for a one-line revert of the operator-disabled sweep (hk-s2eac)
func (r *CrewIdleReaper) loop(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.ScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.scan(ctx)
		}
	}
}

// scan re-evaluates every crew record once. Exposed (not just loop-private)
// so tests can drive a single deterministic tick instead of waiting on the
// ticker.
func (r *CrewIdleReaper) scan(ctx context.Context) {
	if r.cfg.ProjectDir == "" || r.cfg.Queues == nil {
		return
	}
	records, err := r.cfg.ListCrews(r.cfg.ProjectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: crew-idle-reap: list crews: %v\n", err)
		return
	}

	now := r.cfg.Now()
	live := make(map[string]struct{}, len(records))
	for _, rec := range records {
		live[rec.Name] = struct{}{}
		r.checkCrew(ctx, rec, now)
	}

	// Prune tracking entries for crews no longer in the registry (already
	// removed by a manual crew-stop, or otherwise gone).
	r.mu.Lock()
	for name := range r.doneSince {
		if _, ok := live[name]; !ok {
			delete(r.doneSince, name)
		}
	}
	r.mu.Unlock()
}

// checkCrew evaluates a single crew record and, once its bound queue has read
// QueueStatusCompleted for at least GraceAfter, reaps it.
func (r *CrewIdleReaper) checkCrew(ctx context.Context, rec crew.Record, now time.Time) {
	if rec.Queue == "" {
		// No bound queue recorded — no completion signal available.
		r.clearCandidate(rec.Name)
		return
	}
	if r.crewIsPersistent(rec) {
		// GATE-0: a persistent OVERSIGHT role (admiral, watch — manifest
		// lifecycle.persistent: true) is NEVER reclaimed. Such roles never submit
		// work, so their crew-start formality queue reads QueueStatusCompleted
		// forever (ensureQueue, hk-vrnh3); without this gate the reaper culls them
		// at exactly GraceAfter (hk-dy5gw, same root cause as the watch
		// self-terminate hk-kojyr). The manifest property is the durable source of
		// truth — not a queue-shape heuristic.
		r.clearCandidate(rec.Name)
		return
	}
	q := r.cfg.Queues.QueueByName(rec.Queue)
	if q == nil || q.Status != queue.QueueStatusCompleted {
		// Either no completion signal yet, or the queue was re-armed with new
		// work (status left Completed) — clear any pending candidacy so a
		// re-tasked crew is never torn down mid-flight.
		r.clearCandidate(rec.Name)
		return
	}

	r.mu.Lock()
	since, tracked := r.doneSince[rec.Name]
	if !tracked {
		r.doneSince[rec.Name] = now
		r.mu.Unlock()
		return
	}
	idleFor := now.Sub(since)
	if idleFor < r.cfg.GraceAfter {
		r.mu.Unlock()
		return
	}
	delete(r.doneSince, rec.Name)
	r.mu.Unlock()

	r.reap(ctx, rec, idleFor)
}

// crewIsPersistent reports whether rec's agent type is a persistent oversight
// role (manifest lifecycle.persistent: true) that must never be reaped. When no
// PersistentType seam is wired (unit-test mode), no crew is persistent.
func (r *CrewIdleReaper) crewIsPersistent(rec crew.Record) bool {
	if r.cfg.PersistentType == nil {
		return false
	}
	return r.cfg.PersistentType(rec.EffectiveType())
}

// clearCandidate removes any pending-teardown tracking for the named crew.
func (r *CrewIdleReaper) clearCandidate(name string) {
	r.mu.Lock()
	delete(r.doneSince, name)
	r.mu.Unlock()
}

// reap tears the crew down via the same stop path an operator-driven
// crew-stop uses (CrewStopRequest → HandleCrewStop).
func (r *CrewIdleReaper) reap(ctx context.Context, rec crew.Record, idleFor time.Duration) {
	fmt.Fprintf(os.Stderr,
		"daemon: crew-idle-reap: crew %q queue %q completed and idle %s — reclaiming slot\n",
		rec.Name, rec.Queue, idleFor.Round(time.Second))
	if r.cfg.Stopper == nil {
		return
	}
	payload, err := json.Marshal(CrewStopRequest{Name: rec.Name})
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: crew-idle-reap: marshal stop request for %q: %v\n", rec.Name, err)
		return
	}
	if _, stopErr := r.cfg.Stopper.HandleCrewStop(ctx, payload); stopErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: crew-idle-reap: stop crew %q: %v\n", rec.Name, stopErr)
	}
}
