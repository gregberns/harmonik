package lifecycle

import (
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// ReadyCriteria captures which PL-009 readiness criteria have been satisfied
// during daemon startup. All five fields MUST be true before the daemon may
// transition to the `ready` DaemonStatus and emit the `daemon_ready` event.
//
// Field semantics (spec ref: process-lifecycle.md §4.3 PL-009):
//
//   - OrphanSweepDone       — PL-006 orphan sweep has completed (all six
//     bullets: tmux sessions, re-parented handler subprocesses, br
//     subprocesses, intent files, reconciliation locks, and spill files).
//
//   - Cat0PreCheckPassed    — Cat 0 infrastructure prerequisites per
//     reconciliation/spec.md §4.3 RC-012 have been checked and all passed.
//     On failure the daemon enters `degraded` per §PL-010 and MUST NOT set
//     this field until prerequisites clear.
//
//   - GitWalkDone           — PL-005 steps 5–6 (git walk + Beads query) have
//     completed. The git walk discovers in-flight run states; the Beads query
//     resolves dispatchable beads via `br ready` and in-progress audit-log
//     reads per beads-integration.md §4.5 BI-013, BI-016.
//
//   - InMemoryModelBuilt    — PL-005 step 7 in-memory model construction has
//     completed. The daemon MAY NOT dispatch reconciliation actions until the
//     model is built.
//
//   - ReconciliationDispatchDone — PL-005 step 8 reconciliation dispatch has
//     completed for every in-flight run: each run has received a category
//     emission per reconciliation/spec.md §4.3 RC-013 or has been routed to
//     an investigator workflow per PL-009a. Investigator workflows MAY remain
//     in-flight and MUST NOT block this field from being set to true.
//
// Spec ref: process-lifecycle.md §4.3 PL-009.
type ReadyCriteria struct {
	// OrphanSweepDone is true once PL-006 orphan sweep has completed.
	//
	// Spec ref: process-lifecycle.md §4.3 PL-009 — "orphan sweep complete."
	OrphanSweepDone bool

	// Cat0PreCheckPassed is true once the Cat 0 pre-check (RC-012) has passed
	// for all infrastructure prerequisites.
	//
	// Spec ref: process-lifecycle.md §4.3 PL-009 — "Cat 0 pre-check passed."
	Cat0PreCheckPassed bool

	// GitWalkDone is true once the git walk (PL-005 step 5) and Beads query
	// (step 6) have both completed.
	//
	// Spec ref: process-lifecycle.md §4.3 PL-009 — "git walk + Beads query
	// complete (PL-005 steps 5–6)."
	GitWalkDone bool

	// InMemoryModelBuilt is true once the in-memory model (PL-005 step 7) has
	// been constructed.
	//
	// Spec ref: process-lifecycle.md §4.3 PL-009 — "in-memory model built
	// (step 7)."
	InMemoryModelBuilt bool

	// ReconciliationDispatchDone is true once PL-005 step 8 reconciliation
	// dispatch has completed for every in-flight run (category emission OR
	// investigator-workflow routing per PL-009a).
	//
	// Spec ref: process-lifecycle.md §4.3 PL-009 — "reconciliation dispatch
	// complete (step 8)."
	ReconciliationDispatchDone bool
}

// Met reports whether all five PL-009 readiness criteria are satisfied. The
// daemon MUST NOT transition to `ready` or emit `daemon_ready` unless Met()
// returns true.
//
// Spec ref: process-lifecycle.md §4.3 PL-009 — "daemon MUST transition to
// `ready` only when ALL hold."
func (c ReadyCriteria) Met() bool {
	return c.OrphanSweepDone &&
		c.Cat0PreCheckPassed &&
		c.GitWalkDone &&
		c.InMemoryModelBuilt &&
		c.ReconciliationDispatchDone
}

// BuildDaemonReadyPayload constructs a core.DaemonReadyPayload for emission
// as the `daemon_ready` event. It records the wall-clock time as ReadyAt
// (RFC 3339 with millisecond precision) and the monotonic companion from
// MonotonicNsSinceBoot(). The investigatorRunIDs slice contains the run IDs
// of any investigator workflows dispatched before the daemon reached ready
// (per PL-009a); it MAY be nil or empty.
//
// The caller MUST ensure criteria.Met() is true before calling
// BuildDaemonReadyPayload. This function does not re-check criteria.
//
// On monotonic-clock failure the returned error wraps the underlying errno;
// the caller SHOULD abort the ready transition and log the error.
//
// Spec ref: process-lifecycle.md §4.3 PL-009 — "On transition to `ready`,
// the daemon MUST emit `daemon_ready` (per event-model.md §8.7.2) with
// {ready_at, ready_at_ns_since_boot, investigator_run_ids[]}."
//
// Spec ref: operator-nfr.md §4.8 ON-033 — monotonic-companion field required
// for RTO measurement.
func BuildDaemonReadyPayload(investigatorRunIDs []core.RunID) (core.DaemonReadyPayload, error) {
	now := time.Now().UTC()
	monoNs, err := MonotonicNsSinceBoot()
	if err != nil {
		return core.DaemonReadyPayload{}, err
	}

	// Normalise nil slice to empty slice so that JSON serialization emits []
	// rather than null, matching the event-model §8.7.2 array requirement.
	if investigatorRunIDs == nil {
		investigatorRunIDs = []core.RunID{}
	}

	return core.DaemonReadyPayload{
		ReadyAt:            now.Format(time.RFC3339Nano),
		ReadyAtNsSinceBoot: monoNs,
		InvestigatorRunIDs: investigatorRunIDs,
	}, nil
}
