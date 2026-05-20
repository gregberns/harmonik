package lifecycle

// orphansweepbeads.go — PL-006 sixth-bullet orphan sweep of stale `in_progress`
// bead markers. Extends the PL-006 orphan-sweep enumeration with the BI-010d
// reset op that transitions a stale in_progress bead back to open when no live
// run, no pending close/reopen intent, and no merge-commit-on-target-branch
// claim its terminal-transition handling.
//
// Naming note: the bead that delivered Cat 3c auto-resolution (hk-lgtq2) was
// filed under the title "Cat 3a / Cat 3c reconciler" for historical reasons
// (the original filing used "Cat 3a" to refer to the subsumed-bead pattern).
// The canonical pattern name in specs/reconciliation/spec.md §8.6 is Cat 3c
// ("inverse premature-close" — bead still in_progress despite implementation
// having merged). Cat 3a in the spec refers to pending close/reopen intents
// (exclusion (b) in this file). All code in this file uses the canonical Cat 3c
// label.
//
// Bead ref: hk-iuaed.4 (imrest-impl-sweep).
// Spec refs:
//   - specs/process-lifecycle.md §4.5 PL-006 sixth bullet ("Stale `in_progress`
//     bead markers") and the four exclusion conditions (a)–(c) plus the default
//     reset path.
//   - specs/beads-integration.md §4.4 BI-010d (ResetBead op).
//   - specs/beads-integration.md §4.10 BI-030 (intent-log discipline).
//   - specs/beads-integration.md §4.8a BI-024a (`br --version` handshake) —
//     drives the sequencing decision documented below.
//
// # Sequencing decision (PL-006 sixth bullet vs PL-005 step ordering)
//
// The bead `br show` and `br update` invocations issued by this sweep are
// BI-write-surface operations: they depend on the BI-024a `br --version`
// handshake (PL-005 step 4 Cat 0 pre-check) having succeeded, otherwise we
// could issue an `update` against a version-mismatched `br` and corrupt the
// intent-log discipline. The other PL-006 bullets (tmux sessions, worktree
// locks, subprocess sweeps, stale intent enumeration, stale recon-locks) do
// NOT touch the BI write surface — they operate on the filesystem and the
// process table directly.
//
// The bead brief (hk-iuaed.4) explicitly delegates this sequencing question to
// the implementation task. The chosen ordering is:
//
//	step 3 — PL-006 filesystem+process orphan sweep (existing 5 bullets)
//	step 4 — PL-005 Cat 0 pre-check (includes BI-024a `br --version` handshake)
//	step 4.5 — PL-006 sixth bullet: stale-in_progress bead reset (this sweep)
//	step 5+ — git walk, Beads ready query, in-memory model rebuild, etc.
//
// In other words: the bead-reset sweep is fired AFTER the rest of PL-006 has
// quiesced the project's filesystem and process tree AND AFTER the BI-024a
// handshake has confirmed the `br` binary is on the pinned version. This
// matches the spec text in PL-006 sixth bullet, which references the in-memory
// model rebuilt at PL-005 step 7 in exclusion (a) — the bead-reset enumeration
// CANNOT precede the handshake.
//
// At MVH the in-memory model rebuild (PL-005 step 7) is not yet wired as a
// distinct phase; exclusion (a) reduces to the OR clause in the spec text —
// "a `claim` intent file is still present and the BI adapter's BI-031 recovery
// will re-drive it" — which is observable directly via the intent-log
// directory listing.
//
// The single `daemon_orphan_sweep_completed` event (event-model.md §8.7.14)
// covers both the filesystem+process sweep AND this bead-reset sweep:
// `bead_in_progress_reset` is an additive payload field on the same event,
// emitted once after the bead-reset sweep completes. This matches the spec's
// "On completion, the daemon MUST emit `daemon_orphan_sweep_completed` ... with
// counts of ... and `bead_in_progress_reset`" wording.
//
// # Exclusion logic
//
// For each bead returned by `br list --status in_progress --json` that is
// owned by this project (per the provenance match described below):
//
//	(a) Live run reattached. If the in-memory model rebuilt at PL-005 step 7
//	    re-attaches a live in-flight run to this bead, the bead is NOT reset.
//	    At MVH the in-memory model is not yet wired, so exclusion (a) reduces
//	    to the OR clause: a `claim` intent file at
//	    `.harmonik/beads-intents/<key>.json` references this bead AND the BI
//	    adapter's BI-031 recovery will re-drive it.
//
//	(b) Pending close/reopen intent. A `close` or `reopen` intent file at
//	    `.harmonik/beads-intents/<key>.json` references this bead. Cat 3a
//	    handles it — the orphan sweep MUST NOT preempt the Cat 3a detector.
//
//	(c) Merged commit present. A merge commit on the target branch bears
//	    `Harmonik-Bead-ID: <bead_id>` (Cat 3c condition). The Cat 3c
//	    auto-resolver owns the close — the orphan sweep MUST NOT reset
//	    preemptively.
//
// If none of the exclusions apply, the daemon MUST issue a `reset` write via
// the §4.8 BI adapter (BI-010d op). The reset write is intent-logged
// identically to claim/close/reopen writes per BI-030.
//
// # Provenance match
//
// PL-006 sixth bullet specifies provenance match via the audit-trail `actor`
// field carrying this project's `project_hash` per PL-006a, OR — if the
// `actor` field is unsuitable — via cross-referencing `claim` op entries in
// the daemon's own intent-log at `.harmonik/beads-intents/*.json`.
//
// At MVH the audit-trail actor field is not reliably populated with the
// project hash (Beads v0.1.x records the user's git config `user.name`); the
// implementation therefore uses the intent-log cross-reference as the default
// provenance signal. A bead with NO intent file of ANY op type in the local
// intent-log and no positive [ProvenanceChecker] verdict is NOT owned by this
// project's daemon and MUST NOT be touched. This is consistent with PL-006a's
// project-scoped-provenance discipline.
//
// hk-sc3o4 fix: the initial implementation used only the `claim` intent as the
// provenance signal. Dogfood #2 showed stale_intents_observed=4 but
// bead_in_progress_reset=0: hk-a0htu's claim intent had been cleared by BI-031
// recovery, but a close intent (from a timed-out close attempt) or reset
// intent (from a prior sweep that crashed mid-write) was still on disk. The fix
// broadens the provenance signal to any op type — any intent file in the
// project's .harmonik/beads-intents/ directory establishes ownership.
//
// The [ProvenanceChecker] seam lets a future Beads release whose audit-log
// actor field carries the project_hash plug in a deterministic owner check
// independent of the intent-log presence (the MVH-fallback). When a
// ProvenanceChecker is wired and returns true, the reset path becomes
// reachable for beads where all intent files were already cleared. See
// hk-iuaed.4 follow-up.
//
// # Idempotency
//
// The reset write carries the idempotency key
// `<project_hash>:<bead_id>:reset:<daemon_start_ns>` per BI-010d. Two restarts
// of the same daemon produce distinct keys, so a surviving intent file from
// one restart cannot be misclassified as ambiguous by the BI-031 crash-recovery
// scan of the next restart.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// InFlightBeadLedger is the read surface of the BI adapter consumed by
// SweepStaleInProgressBeads. It is satisfied by *brcli.Adapter in production
// and by a fake in tests.
type InFlightBeadLedger interface {
	// ListInFlightBeads returns BeadRecords for every bead currently in coarse
	// status `in_progress` per BI-016. Implementations route through
	// `br list --status in_progress --json`.
	ListInFlightBeads(ctx context.Context) ([]core.BeadRecord, error)
}

// BeadResetter is the write surface of the BI adapter consumed by
// SweepStaleInProgressBeads. It is satisfied by *brcli.Adapter in production
// (Adapter.ResetBead) and by a fake in tests.
type BeadResetter interface {
	// ResetBead issues the BI-010d reset write (in_progress → open) for beadID.
	// The full BI-030 intent-log protocol is applied; see brcli.Adapter.ResetBead.
	ResetBead(
		ctx context.Context,
		intentLogDir string,
		cfg brcli.TimeoutConfig,
		beadID core.BeadID,
		projectHash core.ProjectHash,
		daemonStartNS int64,
	) error
}

// BeadCat3cCloser is the write surface for Cat 3c auto-resolution: closing a
// bead that is IN_PROGRESS but whose implementation has already merged to the
// target branch ("subsumed-bead pattern"). Satisfied by *brcli.Adapter in
// production (Adapter.SweepCloseBead) and by a fake in tests.
//
// Unlike BeadResetter, SweepCloseBead does NOT use the BI-030 intent-log
// protocol — there is no associated in-flight run, so no RunID/TransitionID
// exists. Idempotency is provided at the Beads level: a closed bead will not
// appear in the next startup's `br list --status in_progress` query.
//
// Spec ref: hk-lgtq2 (Cat 3c auto-reconciler).
type BeadCat3cCloser interface {
	SweepCloseBead(ctx context.Context, cfg brcli.TimeoutConfig, beadID core.BeadID) error
}

// ProvenanceChecker reports whether a given bead is owned by this project's
// daemon per PL-006a — independent of the claim-intent presence used as the
// MVH-fallback provenance signal. Production callers MAY leave this nil; the
// sweep then uses the claim-intent presence as the sole provenance signal (the
// OR clause of PL-006's provenance discipline). When non-nil, Owns returning
// true establishes provenance even when the claim intent is absent — this is
// the seam by which a future Beads release whose audit-log actor field carries
// project_hash will plug in, and the seam that unit tests use to exercise the
// reset-firing path (the MVH layering otherwise rules it unreachable; see the
// package doc).
//
// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet — provenance via
// "audit-trail `actor` field carrying this project's `project_hash` per
// PL-006a, OR — if Beads's audit `actor` field is unsuitable — cross-
// referencing `claim` op entries in the daemon's own intent-log".
type ProvenanceChecker interface {
	Owns(ctx context.Context, beadID core.BeadID) (bool, error)
}

// MergeCommitScanner reports whether the target branch has a commit bearing
// the `Harmonik-Bead-ID: <beadID>` trailer (PL-006 exclusion condition (c) —
// Cat 3c condition).
//
// Implementations typically shell out to
// `git log --grep "Harmonik-Bead-ID: <beadID>" <target-branch>`; tests inject
// a fake.
type MergeCommitScanner interface {
	HasMergeCommitForBead(ctx context.Context, beadID core.BeadID) (bool, error)
}

// GitMergeCommitScanner is the production MergeCommitScanner implementation.
// It shells out to `git log --grep` against the configured target branch
// (commonly `main`) under the project directory.
//
// A scan error (git absent, branch missing, etc.) is treated as "no merge
// commit found" — the bead-reset sweep will then proceed with the reset.
// This is the conservative behavior given that a missed Cat 3c condition will
// be re-detected on the next daemon restart, but a false-positive
// merge-commit detection would skip a needed reset.
type GitMergeCommitScanner struct {
	ProjectDir   string
	TargetBranch string // empty defaults to "main"
}

// HasMergeCommitForBead implements MergeCommitScanner.
func (s GitMergeCommitScanner) HasMergeCommitForBead(ctx context.Context, beadID core.BeadID) (bool, error) {
	branch := s.TargetBranch
	if branch == "" {
		branch = "main"
	}
	// `git log -1` exits non-zero when no commit matches, so a non-empty
	// stdout signals a match.
	//nolint:gosec // G204: branch is validated (defaulted), beadID is opaque project-scoped.
	cmd := exec.CommandContext(ctx, "git", "-C", s.ProjectDir, "log", "-1",
		"--grep", "Harmonik-Bead-ID: "+string(beadID), "--format=%H", branch)
	out, err := cmd.Output()
	if err != nil {
		// git log exits non-zero when the branch doesn't exist or no match;
		// treat as "no merge commit" rather than propagating the error.
		return false, nil //nolint:nilerr // intentional: scan failure is non-fatal
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// QueueDispatchedSet is the set of bead IDs that appear in queue.json with
// status=dispatched at the time the orphan sweep runs. Membership means a live
// run is still registered in the queue and exclusion (a) applies — the daemon
// MUST NOT reset the bead while the queue believes it is being executed.
//
// This set is populated by the caller from a raw queue.Load before the full
// LoadQueueAtStartup cross-check runs, giving the sweep an authoritative
// "live run" signal that survives SIGKILL recovery even when the BI-030 intent
// log has been fully drained.
//
// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet — exclusion (a).
// Bug ref: hk-2ty0g (SIGKILL recovery — intent log drained, queue not checked).
type QueueDispatchedSet map[core.BeadID]struct{}

// QueueOwnedSet is the set of bead IDs that appear in queue.json in ANY item
// status (pending, dispatched, completed, failed, deferred-for-ledger-dep).
// Membership establishes provenance: the bead was submitted to THIS project's
// daemon via queue-submit and is therefore owned by this project, regardless of
// whether intent files survive.
//
// When a bead is in QueueOwnedSet but NOT in QueueDispatchedSet, the daemon
// may have been SIGKILL'd after dispatching the bead and clearing the claim
// intent but before the queue could record the completion. That bead is an
// orphan: it must be reset so the next `harmonik run` can reclaim it.
//
// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet — provenance via
// queue.json as an alternative to intent-log presence.
// Bug ref: hk-2ty0g.
type QueueOwnedSet map[core.BeadID]struct{}

// IntentClaimSet is the set of bead IDs for which a `claim` intent file is
// still present on disk under .harmonik/beads-intents/. Membership means
// exclusion condition (a) applies (the BI adapter's BI-031 recovery will
// re-drive the run for this bead).
type IntentClaimSet map[core.BeadID]struct{}

// IntentMutationSet is the set of bead IDs for which a `close` or `reopen`
// intent file is still present on disk. Membership means exclusion condition
// (b) applies (Cat 3a handles it).
type IntentMutationSet map[core.BeadID]struct{}

// IntentProvenanceSet is the set of bead IDs for which ANY intent file exists in
// the project's intent-log directory, regardless of op type. Membership
// establishes provenance: any intent file in .harmonik/beads-intents/ was
// written by this project's daemon (or a prior instance of it). This is the
// MVH-fallback provenance signal used when [ProvenanceChecker] is nil.
//
// The set is a strict superset of IntentClaimSet ∪ IntentMutationSet: it
// captures beads whose claim intent was cleared by BI-031 recovery but whose
// close, reopen, or reset intent is still on disk. This is precisely the
// scenario where stale_intents_observed > 0 but bead_in_progress_reset == 0
// (PL-006 gap, hk-sc3o4).
type IntentProvenanceSet map[core.BeadID]struct{}

// ScanIntentLog walks intentLogDir and returns:
//   - provenance: bead IDs referenced by ANY intent file (claim, close, reopen,
//     reset, or unknown op). Used as the MVH-fallback provenance signal.
//   - claims:     bead IDs with a pending `claim` intent (exclusion (a)).
//   - mutations:  bead IDs with a pending `close` or `reopen` intent (exclusion (b)).
//
// Reset intent files are included in provenance but are NOT added to claims or
// mutations: a stale reset intent does not constitute a live-run signal (a) nor
// a Cat 3a hand-off (b), and the BI-031 recovery path will resolve it on its own.
//
// A missing directory yields empty sets and no error. Malformed entries are
// logged and skipped.
func ScanIntentLog(intentLogDir string, logger *log.Logger) (provenance IntentProvenanceSet, claims IntentClaimSet, mutations IntentMutationSet, err error) {
	provenance = make(IntentProvenanceSet)
	claims = make(IntentClaimSet)
	mutations = make(IntentMutationSet)

	entries, readErr := os.ReadDir(intentLogDir)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			return provenance, claims, mutations, nil
		}
		return nil, nil, nil, fmt.Errorf("lifecycle: ScanIntentLog: ReadDir %q: %w", intentLogDir, readErr)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		if strings.Contains(name, ".tmp-") {
			// BI-030 temp-file pattern (mid-rename); skip.
			continue
		}
		entry, readEntryErr := core.ReadIntentLogEntry(filepath.Join(intentLogDir, name))
		if readEntryErr != nil {
			orphanLog(logger, "ScanIntentLog: skipping malformed %q: %v", name, readEntryErr)
			continue
		}
		// Every successfully-parsed entry establishes provenance regardless of op.
		provenance[entry.BeadID] = struct{}{}
		switch entry.Op {
		case core.TerminalOpClaim:
			claims[entry.BeadID] = struct{}{}
		case core.TerminalOpClose, core.TerminalOpReopen:
			mutations[entry.BeadID] = struct{}{}
		case core.TerminalOpReset:
			// Stale reset intent — not a live-run signal (a) nor a Cat 3a
			// hand-off (b); provenance only.
		default:
			// Unknown op (future schema extension): conservative — treat as a
			// mutation so we DO NOT preempt whatever it represents.
			mutations[entry.BeadID] = struct{}{}
		}
	}
	return provenance, claims, mutations, nil
}

// SweepStaleInProgressBeadsConfig carries injected dependencies for
// SweepStaleInProgressBeads. Production callers wire production
// implementations; tests inject fakes.
type SweepStaleInProgressBeadsConfig struct {
	// Ledger is the read surface: br list --status in_progress.
	// REQUIRED (non-nil).
	Ledger InFlightBeadLedger

	// Resetter is the write surface: br update --status open via the BI adapter.
	// REQUIRED (non-nil).
	Resetter BeadResetter

	// Provenance, when non-nil, overrides the claim-intent-presence-only
	// provenance signal with a deterministic per-bead owner check. Production
	// callers SHOULD leave this nil at MVH (the OR-clause fallback governs);
	// when a future Beads release exposes a project_hash-carrying actor field
	// on the audit log, the production wiring plugs an audit-log-based checker
	// in here.
	//
	// Note: even when Provenance is non-nil, the claim-intent-presence check is
	// still consulted as exclusion (a) — the two checks are independent.
	Provenance ProvenanceChecker

	// MergeScanner detects Cat 3c condition (exclusion c). Nil → exclusion (c)
	// always returns false (no merge commit), which is the conservative behavior
	// in test contexts where no git repo exists. Production callers SHOULD
	// supply a [GitMergeCommitScanner].
	MergeScanner MergeCommitScanner

	// IntentLogDir is the absolute path of .harmonik/beads-intents/ for this
	// project. The sweep scans this directory to compute exclusion (a) — claim
	// intent present — and exclusion (b) — close/reopen intent present.
	// REQUIRED (non-empty).
	IntentLogDir string

	// ProjectHash is the per-project provenance marker per PL-006a.
	// REQUIRED (non-zero-length).
	ProjectHash core.ProjectHash

	// DaemonStartNS is the daemon's startup wall-clock time in nanoseconds.
	// Used to derive the BI-010d idempotency key
	// `<project_hash>:<bead_id>:reset:<daemon_start_ns>`.
	// REQUIRED (> 0).
	DaemonStartNS int64

	// Cat3cCloser, when non-nil, enables Cat 3c auto-resolution: when a merged
	// commit bearing Harmonik-Bead-ID is detected for an in_progress bead
	// (exclusion c), the sweep CLOSES the bead via SweepCloseBead instead of
	// skipping it. When nil the sweep skips the bead (old behavior — safe but
	// leaves the bead permanently in_progress until operator intervention).
	//
	// Spec ref: hk-lgtq2 (Cat 3c auto-reconciler).
	Cat3cCloser BeadCat3cCloser

	// QueueDispatched, when non-nil, provides the set of bead IDs that queue.json
	// records as status=dispatched at startup. Membership triggers exclusion (a):
	// the queue still believes a live run exists, so the sweep MUST NOT reset the
	// bead. This complements the intent-log exclusion (a) and survives SIGKILL
	// scenarios where the intent log has been fully drained.
	//
	// Nil is safe — the queue-dispatched check is then skipped (old behavior).
	// Production callers SHOULD supply this; tests that do not exercise the queue
	// path may leave it nil.
	//
	// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet — exclusion (a).
	// Bug ref: hk-2ty0g.
	QueueDispatched QueueDispatchedSet

	// QueueOwned, when non-nil, provides the set of bead IDs that appear in
	// queue.json in ANY item status. Membership establishes provenance for the
	// bead: it was submitted to THIS project's daemon and is therefore owned,
	// independent of whether intent files remain on disk. This closes the
	// SIGKILL-recovery gap where intent files have been drained and the bead
	// appears unowned to the intent-log-only provenance check.
	//
	// Nil is safe — the queue-ownership provenance signal is then not consulted.
	// Production callers SHOULD supply this alongside QueueDispatched.
	//
	// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet — provenance.
	// Bug ref: hk-2ty0g.
	QueueOwned QueueOwnedSet

	// BrTimeoutCfg is the BI-025c timeout configuration forwarded to ResetBead.
	// Zero value is acceptable (defaults apply).
	BrTimeoutCfg brcli.TimeoutConfig

	// Logger receives diagnostic messages. Nil → silent.
	Logger *log.Logger
}

// SweepResult reports the outcome of a single SweepStaleInProgressBeads call.
type SweepResult struct {
	// ResetCount is the number of beads successfully reset (in_progress → open).
	ResetCount int
	// Cat3cCloseCount is the number of subsumed beads auto-closed (hk-lgtq2 Cat 3c).
	Cat3cCloseCount int
}

// SweepStaleInProgressBeads enumerates beads in coarse status `in_progress`
// and resets those owned by this project's daemon that meet none of the
// PL-006 exclusion conditions (a)–(c) — issuing a BI-010d reset write
// (in_progress → open) via cfg.Resetter for each.
//
// Returns the number of beads successfully reset. A reset error on one bead
// does NOT abort the sweep — remaining beads are still processed, and the
// last error is wrapped into the returned error. The integer return reflects
// only successful resets and is safe to surface as the
// `bead_in_progress_reset` payload count.
//
// Provenance discipline: a bead is considered owned by this project iff a
// `claim` intent for it is recorded in the local intent log (the spec's OR
// clause of the provenance-match rule). Beads with no local claim intent are
// NOT touched, consistent with PL-006a's project-scoped-provenance discipline.
// (When the audit-log actor field is widened to carry the project hash in a
// future Beads release, this routine can be extended to consume that as a
// provenance signal — tracked as a follow-up.)
//
// Spec ref: specs/process-lifecycle.md §4.5 PL-006 sixth bullet;
// specs/beads-integration.md §4.4 BI-010d; §4.10 BI-030.
func SweepStaleInProgressBeads(ctx context.Context, cfg SweepStaleInProgressBeadsConfig) (result SweepResult, err error) {
	if cfg.Ledger == nil {
		return SweepResult{}, fmt.Errorf("lifecycle: SweepStaleInProgressBeads: cfg.Ledger is nil")
	}
	if cfg.Resetter == nil {
		return SweepResult{}, fmt.Errorf("lifecycle: SweepStaleInProgressBeads: cfg.Resetter is nil")
	}
	if cfg.IntentLogDir == "" {
		return SweepResult{}, fmt.Errorf("lifecycle: SweepStaleInProgressBeads: cfg.IntentLogDir is empty")
	}
	if cfg.ProjectHash == "" {
		return SweepResult{}, fmt.Errorf("lifecycle: SweepStaleInProgressBeads: cfg.ProjectHash is empty")
	}
	if cfg.DaemonStartNS <= 0 {
		return SweepResult{}, fmt.Errorf("lifecycle: SweepStaleInProgressBeads: cfg.DaemonStartNS must be > 0")
	}

	beads, listErr := cfg.Ledger.ListInFlightBeads(ctx)
	if listErr != nil {
		return SweepResult{}, fmt.Errorf("lifecycle: SweepStaleInProgressBeads: ListInFlightBeads: %w", listErr)
	}
	if len(beads) == 0 {
		return SweepResult{}, nil
	}

	provenance, claims, mutations, scanErr := ScanIntentLog(cfg.IntentLogDir, cfg.Logger)
	if scanErr != nil {
		return SweepResult{}, fmt.Errorf("lifecycle: SweepStaleInProgressBeads: ScanIntentLog: %w", scanErr)
	}

	var lastResetErr error
	var lastCat3cErr error
	for _, bead := range beads {
		// Provenance check (PL-006a OR clause): the bead is owned by this
		// project iff any of the following holds:
		//   (i)  cfg.Provenance.Owns(...) reports true, OR
		//   (ii) ANY intent file (claim, close, reopen, or reset) references it
		//        in the local intent log (hk-sc3o4 broadened provenance signal), OR
		//   (iii) the bead appears in QueueOwned — it was submitted to THIS
		//         project's daemon via queue-submit (hk-2ty0g SIGKILL-recovery fix).
		//
		// Signal (iii) closes the gap where the intent log has been fully drained
		// after SIGKILL recovery but the bead is still in_progress in the ledger.
		owned := false
		if cfg.Provenance != nil {
			provOwned, provErr := cfg.Provenance.Owns(ctx, bead.BeadID)
			if provErr != nil {
				orphanLog(cfg.Logger, "SweepStaleInProgressBeads: bead %s provenance check error (falling back to intent-log signal): %v", bead.BeadID, provErr)
			} else if provOwned {
				owned = true
			}
		}
		if !owned {
			if _, hasProvenance := provenance[bead.BeadID]; hasProvenance {
				owned = true
			}
		}
		if !owned {
			if _, inQueue := cfg.QueueOwned[bead.BeadID]; inQueue {
				owned = true
			}
		}
		if !owned {
			orphanLog(cfg.Logger, "SweepStaleInProgressBeads: bead %s in_progress but no provenance signal — not owned by this project; skipping", bead.BeadID)
			continue
		}

		// (a) Live run will be reattached. Two signals can fire exclusion (a):
		//
		//   (a-intent) A claim intent file is present in the local intent log.
		//              BI-031 recovery WILL re-drive the run, so resetting now
		//              would race with the re-drive.
		//
		//   (a-queue)  The bead appears in QueueDispatched — queue.json still
		//              records an active dispatch for this bead. The queue
		//              considers the run live; the sweep MUST NOT preempt it.
		//              This covers SIGKILL recovery where the claim intent was
		//              drained by BI-031 recovery on a previous restart but
		//              queue.json was not yet updated (hk-2ty0g).
		//
		// When NEITHER signal fires and provenance is established, the reset
		// path proceeds. This is the imrest scenario the PL-006 sixth bullet
		// targets.
		if _, hasClaim := claims[bead.BeadID]; hasClaim {
			orphanLog(cfg.Logger, "SweepStaleInProgressBeads: bead %s has live claim intent; exclusion (a) — skip reset", bead.BeadID)
			continue
		}
		if _, isDispatched := cfg.QueueDispatched[bead.BeadID]; isDispatched {
			orphanLog(cfg.Logger, "SweepStaleInProgressBeads: bead %s is dispatched in queue.json; exclusion (a-queue) — skip reset", bead.BeadID)
			continue
		}

		// (b) Pending close/reopen intent — Cat 3a handles it.
		if _, hasMutation := mutations[bead.BeadID]; hasMutation {
			orphanLog(cfg.Logger, "SweepStaleInProgressBeads: bead %s has pending close/reopen intent; exclusion (b) — skip reset", bead.BeadID)
			continue
		}

		// (c) Merged commit on target branch — Cat 3c handles it.
		if cfg.MergeScanner != nil {
			merged, mergeErr := cfg.MergeScanner.HasMergeCommitForBead(ctx, bead.BeadID)
			if mergeErr != nil {
				orphanLog(cfg.Logger, "SweepStaleInProgressBeads: bead %s merge-commit scan error (proceeding to reset): %v", bead.BeadID, mergeErr)
			} else if merged {
				if cfg.Cat3cCloser != nil {
					// Cat 3c auto-resolution (hk-lgtq2): close the subsumed bead.
					orphanLog(cfg.Logger, "SweepStaleInProgressBeads: bead %s subsumed — Harmonik-Bead-ID merge commit detected; Cat 3c auto-close", bead.BeadID)
					if closeErr := cfg.Cat3cCloser.SweepCloseBead(ctx, cfg.BrTimeoutCfg, bead.BeadID); closeErr != nil {
						orphanLog(cfg.Logger, "SweepStaleInProgressBeads: bead %s Cat 3c close failed: %v", bead.BeadID, closeErr)
						lastCat3cErr = closeErr
					} else {
						result.Cat3cCloseCount++
					}
				} else {
					orphanLog(cfg.Logger, "SweepStaleInProgressBeads: bead %s has Harmonik-Bead-ID merge commit on target branch; exclusion (c) — skip reset (Cat3cCloser not wired)", bead.BeadID)
				}
				continue
			}
		}

		// No exclusion fires — issue the BI-010d reset write.
		orphanLog(cfg.Logger, "SweepStaleInProgressBeads: resetting bead %s (in_progress → open) per PL-006 sixth bullet", bead.BeadID)
		if resetErr := cfg.Resetter.ResetBead(
			ctx,
			cfg.IntentLogDir,
			cfg.BrTimeoutCfg,
			bead.BeadID,
			cfg.ProjectHash,
			cfg.DaemonStartNS,
		); resetErr != nil {
			orphanLog(cfg.Logger, "SweepStaleInProgressBeads: bead %s reset failed: %v", bead.BeadID, resetErr)
			lastResetErr = resetErr
			continue
		}
		result.ResetCount++
	}

	var combinedErr error
	switch {
	case lastResetErr != nil && lastCat3cErr != nil:
		combinedErr = fmt.Errorf("lifecycle: SweepStaleInProgressBeads: reset error: %w; cat3c error: %v", lastResetErr, lastCat3cErr)
	case lastResetErr != nil:
		combinedErr = fmt.Errorf("lifecycle: SweepStaleInProgressBeads: at least one reset failed (last: %w)", lastResetErr)
	case lastCat3cErr != nil:
		combinedErr = fmt.Errorf("lifecycle: SweepStaleInProgressBeads: at least one Cat 3c close failed (last: %w)", lastCat3cErr)
	}
	return result, combinedErr
}
