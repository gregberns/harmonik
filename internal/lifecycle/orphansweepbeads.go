package lifecycle

import (
	"bufio"
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
	"github.com/gregberns/harmonik/internal/gitprobe"
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
// fallback provenance signal. Production callers MAY leave this nil; the
// sweep then uses the claim-intent presence as the sole provenance signal (the
// OR clause of PL-006's provenance discipline). When non-nil, Owns returning
// true establishes provenance even when the claim intent is absent — this is
// the seam by which a future Beads release whose audit-log actor field carries
// project_hash will plug in, and the seam that unit tests use to exercise the
// reset-firing path (the current layering otherwise rules it unreachable; see the
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
// It shells out to `git log` against the configured target branch (commonly
// `main`) under the project directory and confirms the Cat 3c condition with
// TWO independent pieces of evidence, not a mere body mention:
//
//  1. Trailer equality — a commit must carry an ACTUAL git trailer
//     `Harmonik-Bead-ID: <beadID>` whose value equals the target bead ID
//     exactly. The prior implementation matched `git log --grep` against commit
//     BODIES, so a docs-only commit that merely *mentioned* the bead id in its
//     prose falsely satisfied the condition (B2 false-close bug). Extracting the
//     structured trailer with `%(trailers:key=...,valueonly=true)` and requiring
//     exact value equality closes that hole — a body mention no longer matches.
//  2. Non-docs diff — the matched commit's diff must touch at least one
//     non-docs file. A trailer-bearing commit whose diff is EXCLUSIVELY docs
//     (`*.md`, files under a docs/ path, or the captain-lanes tracker) does not
//     represent merged implementation work and MUST NOT trigger a Cat 3c close.
//
// A scan error (git absent, branch missing, etc.) is treated as "no merge
// commit found" — the bead-reset sweep will then proceed with the reset.
// This is the conservative behavior given that a missed Cat 3c condition will
// be re-detected on the next daemon restart, but a false-positive
// merge-commit detection would skip a needed reset.
//
// # Change-still-present verification (H3)
//
// A bare `git log --grep` match on the trailer is NOT sufficient to auto-close a
// bead as subsumed: a commit bearing the trailer can have been REVERTED or
// otherwise superseded, leaving the bead's work absent from the current tree.
// Auto-closing on the mere historical presence of the trailer would mark such a
// bead DONE even though its change is gone. HasMergeCommitForBead therefore, after
// finding the trailer-bearing commit, (1) confirms it is still an ancestor of the
// target-branch tip and (2) confirms no later commit on the branch REVERTS it —
// only then does it report the change present.
type GitMergeCommitScanner struct {
	ProjectDir   string
	TargetBranch string // empty defaults to "main"
}

// HasMergeCommitForBead implements MergeCommitScanner.
//
// It requires BOTH a genuine `Harmonik-Bead-ID` trailer whose value equals
// beadID exactly AND a non-docs diff on the matched commit, AND confirms the
// matched commit's change is still present on the branch (ancestor of the tip
// and not reverted by a later commit). On any scan error it returns (false, nil)
// — conservative, as documented on the type.
func (s GitMergeCommitScanner) HasMergeCommitForBead(ctx context.Context, beadID core.BeadID) (bool, error) {
	branch := s.TargetBranch
	if branch == "" {
		branch = "main"
	}
	const format = "%H%x00%(trailers:key=Harmonik-Bead-ID,valueonly=true,separator=%x00)"
	//nolint:gosec // G204: branch is validated (defaulted); format is a constant.
	cmd := exec.CommandContext(ctx, "git", "-C", s.ProjectDir, "log",
		"--format="+format, branch)
	out, err := cmd.Output()
	if err != nil {
		return false, nil //nolint:nilerr // intentional: scan failure is non-fatal
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		hash, trailerField, ok := strings.Cut(line, "\x00")
		if !ok {
			continue
		}
		if !trailerHasExactValue(trailerField, string(beadID)) {
			continue
		}
		touchesNonDocs, diffErr := s.commitTouchesNonDocs(ctx, hash)
		if diffErr != nil {
			return false, nil //nolint:nilerr // intentional: scan failure is non-fatal
		}
		if touchesNonDocs {
			stillPresent, presentErr := s.changeStillPresent(ctx, hash, branch)
			if presentErr != nil {
				return false, nil //nolint:nilerr // intentional: scan failure is non-fatal
			}
			if !stillPresent {
				continue
			}
			return true, nil
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return false, nil //nolint:nilerr // intentional: scan failure is non-fatal
	}
	return false, nil
}

func trailerHasExactValue(field, beadID string) bool {
	if field == "" {
		return false
	}
	for _, v := range strings.Split(field, "\x00") {
		if v == beadID {
			return true
		}
	}
	return false
}

func (s GitMergeCommitScanner) commitTouchesNonDocs(ctx context.Context, hash string) (bool, error) {
	//nolint:gosec // G204: hash is a %H value read from git output, not user input.
	cmd := exec.CommandContext(ctx, "git", "-C", s.ProjectDir,
		"diff-tree", "--no-commit-id", "--name-only", "-r", hash)
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		path := strings.TrimSpace(scanner.Text())
		if path == "" {
			continue
		}
		if !isDocsPath(path) {
			return true, nil
		}
	}
	return false, scanner.Err()
}

func isDocsPath(path string) bool {
	if strings.HasSuffix(path, ".md") {
		return true
	}
	if strings.Contains(path, "captain-lanes") {
		return true
	}
	if path == "docs" || strings.HasPrefix(path, "docs/") || strings.Contains(path, "/docs/") {
		return true
	}
	return false
}

func (s GitMergeCommitScanner) changeStillPresent(ctx context.Context, hash, branch string) (bool, error) {
	stillAncestor, err := gitprobe.IsAncestor(ctx, s.ProjectDir, hash, branch)
	if err != nil {
		return false, err
	}
	if !stillAncestor {
		return false, nil
	}

	// (2) Confirm the change has not been reverted by a LATER commit on the
	// branch. `git revert` records "This reverts commit <full-sha>." in the revert
	// commit message; a match in the hash..branch range means the work was undone.
	//nolint:gosec // G204: hash is a %H value from git output; branch is validated.
	revOut, revErr := exec.CommandContext(ctx, "git", "-C", s.ProjectDir, "log",
		"--grep", "This reverts commit "+hash, "--format=%H", hash+".."+branch).Output()
	if revErr == nil && strings.TrimSpace(string(revOut)) != "" {
		return false, nil
	}

	return true, nil
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
// fallback provenance signal used when [ProvenanceChecker] is nil.
//
// The set is a strict superset of IntentClaimSet ∪ IntentMutationSet: it
// captures beads whose claim intent was cleared by BI-031 recovery but whose
// close, reopen, or reset intent is still on disk. This is precisely the
// scenario where stale_intents_observed > 0 but bead_in_progress_reset == 0
// (PL-006 gap, hk-sc3o4).
type IntentProvenanceSet map[core.BeadID]struct{}

// ScanIntentLog walks intentLogDir and returns:
//   - provenance: bead IDs referenced by ANY intent file (claim, close, reopen,
//     reset, or unknown op). Used as the fallback provenance signal.
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
			continue
		}
		entry, readEntryErr := core.ReadIntentLogEntry(filepath.Join(intentLogDir, name))
		if readEntryErr != nil {
			orphanLog(logger, "ScanIntentLog: skipping malformed %q: %v", name, readEntryErr)
			continue
		}
		provenance[entry.BeadID] = struct{}{}
		switch entry.Op {
		case core.TerminalOpClaim:
			claims[entry.BeadID] = struct{}{}
		case core.TerminalOpClose, core.TerminalOpReopen:
			mutations[entry.BeadID] = struct{}{}
		case core.TerminalOpReset:
		default:
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
	// callers SHOULD leave this nil (the OR-clause fallback governs);
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

		if _, hasClaim := claims[bead.BeadID]; hasClaim {
			orphanLog(cfg.Logger, "SweepStaleInProgressBeads: bead %s has live claim intent; exclusion (a) — skip reset", bead.BeadID)
			continue
		}
		if _, isDispatched := cfg.QueueDispatched[bead.BeadID]; isDispatched {
			orphanLog(cfg.Logger, "SweepStaleInProgressBeads: bead %s is dispatched in queue.json; exclusion (a-queue) — skip reset", bead.BeadID)
			continue
		}

		if _, hasMutation := mutations[bead.BeadID]; hasMutation {
			orphanLog(cfg.Logger, "SweepStaleInProgressBeads: bead %s has pending close/reopen intent; exclusion (b) — skip reset", bead.BeadID)
			continue
		}

		if cfg.MergeScanner != nil {
			merged, mergeErr := cfg.MergeScanner.HasMergeCommitForBead(ctx, bead.BeadID)
			if mergeErr != nil {
				orphanLog(cfg.Logger, "SweepStaleInProgressBeads: bead %s merge-commit scan error (proceeding to reset): %v", bead.BeadID, mergeErr)
			} else if merged {
				if cfg.Cat3cCloser != nil {
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
		combinedErr = fmt.Errorf("lifecycle: SweepStaleInProgressBeads: reset error: %w; cat3c error: %w", lastResetErr, lastCat3cErr)
	case lastResetErr != nil:
		combinedErr = fmt.Errorf("lifecycle: SweepStaleInProgressBeads: at least one reset failed (last: %w)", lastResetErr)
	case lastCat3cErr != nil:
		combinedErr = fmt.Errorf("lifecycle: SweepStaleInProgressBeads: at least one Cat 3c close failed (last: %w)", lastCat3cErr)
	}
	return result, combinedErr
}
