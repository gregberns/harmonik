package crewrun

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
	crewIdleReapDefaultGrace = 5 * time.Minute

	crewIdleReapDefaultScanInterval = 30 * time.Second
)

type crewStopper interface {
	HandleCrewStop(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)
}

type crewQueueLookup interface {
	QueueByName(name string) *queue.Queue
}

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
//
// Traceability (hk-do173): hk-s2eac is the FEATURE bead that introduced SD-3; the
// DISABLE and its guards are tracked under hk-98at0 (the teardown-level guard) and
// hk-do173 (the tighter no-scan guard). Two regression tests in idlereap_test.go
// pin this no-op — TestCrewIdleReaper_StartWatcher_Disabled_NeverReaps and
// TestCrewIdleReaper_StartWatcher_Disabled_NeverScans — both FAIL if this body is
// reverted to launch loop(), so a re-enable cannot land silently. They assert on
// behaviour (no crew stopped, registry never read), not on the body being empty,
// so any re-enable REACHED THROUGH StartWatcher fails them — including one that
// hand-rolls its own pump instead of restoring loop(). They do not cover a caller
// that bypasses StartWatcher and drives loop()/scan() itself; the only production
// call site is bootworkloop.go's bs.crewIdleReaper.StartWatcher(ctx), and adding
// a second entry point is the change to be suspicious of.
//
// Those two tests did NOT exist between 2026-07-22 and 2026-07-28: they lived in
// idlereap_hks2eac_test.go, which the Phase 1 ticket-named-test-file deletion
// (ec66da798) removed wholesale, while this comment went on asserting they were
// there. If you are moving or renaming them, run them against a re-enabled
// StartWatcher first — a claim in a comment is not a test.
func (r *CrewIdleReaper) StartWatcher(ctx context.Context) {
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

	r.mu.Lock()
	for name := range r.doneSince {
		if _, ok := live[name]; !ok {
			delete(r.doneSince, name)
		}
	}
	r.mu.Unlock()
}

func (r *CrewIdleReaper) checkCrew(ctx context.Context, rec crew.Record, now time.Time) {
	if rec.Queue == "" {
		r.clearCandidate(rec.Name)
		return
	}
	if r.crewIsPersistent(rec) {
		r.clearCandidate(rec.Name)
		return
	}
	q := r.cfg.Queues.QueueByName(rec.Queue)
	if q == nil || q.Status != queue.QueueStatusCompleted {
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

func (r *CrewIdleReaper) crewIsPersistent(rec crew.Record) bool {
	if r.cfg.PersistentType == nil {
		return false
	}
	return r.cfg.PersistentType(rec.EffectiveType())
}

func (r *CrewIdleReaper) clearCandidate(name string) {
	r.mu.Lock()
	delete(r.doneSince, name)
	r.mu.Unlock()
}

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
