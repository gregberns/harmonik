package lifecycle

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// ErrBeadsUnavailable is returned by DiscoverActiveRuns when the Beads store
// cannot be reached (BrUnavailable, BrDbLocked, exec failure). Per EM-031a,
// the caller MUST NOT proceed with active-run classification; the daemon
// transitions to DaemonStatusDegraded and waits for infrastructure resolution.
//
// Spec ref: execution-model.md §4.7 EM-031a — "Beads-unreachable defers
// classification per the Cat 0 pre-check in [reconciliation/spec.md §8.1]
// and enters `degraded` status per [process-lifecycle.md §4.3]."
var ErrBeadsUnavailable = errors.New("lifecycle: Beads unreachable at startup — active-run discovery halted per EM-031a")

type activeRunSource int

const (
	activeRunSourceBeads activeRunSource = iota

	activeRunSourceBranch

	activeRunSourceBoth
)

// ActiveRunEntry is one element of the active-run set produced by
// DiscoverActiveRuns. Each entry records the run's RunID, the source(s) from
// which it was discovered, and the associated bead ID (if any).
//
// Spec ref: execution-model.md §4.7 EM-031a — "the union of (Beads-linked
// runs) ∪ (branches whose tip carries a Harmonik-Run-ID trailer matching no
// terminal-state bead) is the active-run set."
type ActiveRunEntry struct {
	// RunID is the stable UUIDv7 run identifier extracted from Beads records
	// or the git task-branch tip's Harmonik-Run-ID trailer.
	//
	// TODO(hk-b3f.40): RunID here is a placeholder using core.RunID, which is
	// already defined in internal/core. If a richer typed alias is introduced
	// per execution-model.md §6.1, this field adopts it automatically.
	RunID core.RunID

	// BeadID is the Beads bead ID associated with this run. Nil when the run
	// was discovered via git-only branch scan and no Beads record was found.
	BeadID *core.BeadID

	// source records how the run was discovered (Beads query, branch scan, or both).
	source activeRunSource
}

// BranchName returns the canonical task-branch name for this run per
// workspace-model.md §4.2 WM-005 ("run/<run_id>").
func (e ActiveRunEntry) BranchName() string {
	return "run/" + e.RunID.String()
}

// ActiveRunSet is the result of DiscoverActiveRuns. It is the union of
// Beads-linked runs and git-branch-linked runs that have not reached a
// terminal Beads status.
//
// An empty ActiveRunSet (Len()==0) means no in-flight runs were found;
// the daemon proceeds to ready without reconciliation.
//
// Spec ref: execution-model.md §4.7 EM-031a.
type ActiveRunSet struct {
	entries []ActiveRunEntry
}

// Len returns the number of entries in the active-run set.
func (s ActiveRunSet) Len() int { return len(s.entries) }

// Entries returns a copy of the active-run entries in stable order
// (branch-linked entries first, then Beads-only entries). The returned slice
// is a copy; callers MUST NOT modify it.
func (s ActiveRunSet) Entries() []ActiveRunEntry {
	out := make([]ActiveRunEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

const taskBranchPrefix = "refs/heads/run/"

func isTerminalBeadStatus(s core.CoarseStatus) bool {
	return s.IsTerminal()
}

// BeadsQuerier is the interface for querying bead status from the Beads store.
// The production implementation delegates to *brcli.Adapter; tests inject a
// deterministic fake.
//
// ListBeadsByStatus returns all bead records in the given status. Empty status
// is an error. Returns an error wrapping brcli.BrUnavailable or
// brcli.BrDbLocked when the Beads store is unreachable at startup per EM-031a.
//
// Spec ref: execution-model.md §4.7 EM-031a — "querying Beads for beads in
// non-terminal state."
type BeadsQuerier interface {
	ListBeadsByStatus(ctx context.Context, status string) ([]core.BeadRecord, error)
}

// BranchTipReader is the interface for reading the tip commit of a git task
// branch. Implementations enumerate branches and extract the Harmonik-Run-ID
// trailer from each tip commit. Tests inject a deterministic fake.
//
// ListTaskBranchTips returns one (branchName, runID, beadID) tuple for each
// task branch found under refs/heads/run/. beadID is empty string when the
// tip commit does not carry a Harmonik-Bead-ID trailer.
//
// Spec ref: execution-model.md §4.7 EM-031a — "scanning the project's git
// refs for task branches matching the naming convention declared in
// [workspace-model.md §4.2]."
type BranchTipReader interface {
	ListTaskBranchTips(ctx context.Context) ([]BranchTip, error)
}

// BranchTip is one entry from a BranchTipReader result. It records the
// run ID and optional bead ID found in the tip commit's trailers for a
// single task branch.
type BranchTip struct {
	// BranchName is the short branch name, e.g. "run/01234567-...".
	BranchName string

	// RunID is the value of the Harmonik-Run-ID trailer on the branch tip.
	// Empty string means the trailer was absent (the branch tip is not a
	// harmonik checkpoint commit).
	RunID string

	// BeadID is the value of the Harmonik-Bead-ID trailer on the branch tip.
	// Empty string means the bead trailer was absent (non-bead-tied run, or
	// the bead ID is tracked only in Beads, not in the most-recent trailer).
	BeadID string
}

// GitBranchTipReader is the production BranchTipReader. It invokes
// `git for-each-ref` against repoDir to enumerate task branches and extract
// Harmonik-Run-ID / Harmonik-Bead-ID trailers from each tip commit.
//
// Spec ref: execution-model.md §4.7 EM-031a; workspace-model.md §4.2 WM-005
// ("run/<run_id>" naming convention).
type GitBranchTipReader struct {
	// RepoDir is the absolute path to the git repository root (not the worktree).
	RepoDir string
}

// ListTaskBranchTips implements BranchTipReader using git for-each-ref.
//
// The format string uses git's `trailers:key=X,valueonly=true,separator=%x00`
// token which is available from git 2.34 (required by workspace-model.md
// §4.2 WM-ENV-002). The separator=%x00 (NUL) collapses multi-value trailers
// onto a single field; absent trailers produce an empty field.
//
// Each output line has a labeled field format to handle empty values reliably:
//
//	REF:<branch> RUN:<run_id> BEAD:<bead_id>
//
// Parsing strips the "REF:", "RUN:", "BEAD:" prefixes to extract values.
// When a trailer is absent, the prefix is followed immediately by the next
// labeled field or end-of-line, producing an empty string after stripping.
func (r GitBranchTipReader) ListTaskBranchTips(ctx context.Context) ([]BranchTip, error) {
	const format = "REF:%(refname:short) RUN:%(trailers:key=Harmonik-Run-ID,valueonly=true,separator=%x00) BEAD:%(trailers:key=Harmonik-Bead-ID,valueonly=true,separator=%x00)"

	//nolint:gosec // G204: arguments are hard-coded constants or RepoDir resolved at startup; not user input
	cmd := exec.CommandContext(ctx, "git",
		"-C", r.RepoDir,
		"for-each-ref",
		"--format="+format,
		taskBranchPrefix,
	)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("lifecycle: GitBranchTipReader: git for-each-ref: %w", err)
	}
	if len(out) == 0 {
		return nil, nil
	}

	var tips []BranchTip
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		tip, ok := parseGitBranchTipLine(line)
		if !ok {
			continue
		}
		tips = append(tips, tip)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("lifecycle: GitBranchTipReader: scan: %w", err)
	}
	return tips, nil
}

func parseGitBranchTipLine(line string) (BranchTip, bool) {
	const refPfx = "REF:"
	const runPfx = " RUN:"
	const beadPfx = " BEAD:"

	if !strings.HasPrefix(line, refPfx) {
		return BranchTip{}, false
	}

	runIdx := strings.Index(line, runPfx)
	if runIdx < 0 {
		return BranchTip{}, false
	}
	branchName := line[len(refPfx):runIdx]

	rest := line[runIdx+len(runPfx):]

	beadIdx := strings.Index(rest, beadPfx)
	var runID, beadID string
	if beadIdx >= 0 {
		runID = rest[:beadIdx]
		beadID = rest[beadIdx+len(beadPfx):]
	} else {
		runID = rest
	}

	runID = strings.TrimRight(strings.ReplaceAll(runID, "\x00", ","), ",")
	beadID = strings.TrimRight(strings.ReplaceAll(beadID, "\x00", ","), ",")

	return BranchTip{
		BranchName: strings.TrimSpace(branchName),
		RunID:      strings.TrimSpace(runID),
		BeadID:     strings.TrimSpace(beadID),
	}, true
}

// DiscoverActiveRuns determines the set of in-flight runs that require
// reconciliation or resumption at daemon startup, per EM-031a.
//
// Discovery follows the two-source rule:
//  1. Query Beads for beads in non-terminal status (all statuses except
//     `closed` and `tombstone`) via querier. Each non-terminal bead becomes a candidate.
//  2. Scan git task branches (refs/heads/run/*) via reader. For each branch
//     whose tip commit carries a Harmonik-Run-ID trailer that matches no
//     terminal-state bead, add the run to the active-run set.
//  3. Return the union of (Beads-linked runs) ∪ (branch-linked runs).
//
// A run whose bead status is terminal (closed/tombstone) is EXCLUDED from the
// active-run set even when a task branch still exists for it (the branch is a
// stale artifact of a completed run).
//
// Beads-unreachable behaviour: if querier returns an error wrapping
// brcli.BrUnavailable or brcli.BrDbLocked (or any exec-level error that
// prevents querying Beads), DiscoverActiveRuns returns ErrBeadsUnavailable.
// The caller MUST NOT proceed with classification; it MUST transition the daemon
// to DaemonStatusDegraded per [process-lifecycle.md §4.3] and enter a
// wait-and-retry loop per [reconciliation/spec.md §8.1 Cat 0].
//
// Production callers pass a *brcli.Adapter (which implements BeadsQuerier) for
// querier. Tests inject a deterministic fake.
//
// Pre-dispatch worktree rule: callers that dispatch a reconciliation workflow
// for any entry in the returned ActiveRunSet MUST NOT modify the run's worktree
// before the investigator has run. No git clean, git checkout, branch switch,
// or file deletion is permitted pre-dispatch per EM-031a + RC §4.4 RC-019.
// This function enforces the read-only posture on its own git access (for-each-ref
// is read-only); enforcement of the caller's pre-dispatch behaviour is a contract
// obligation on the caller, not a runtime check performed here.
//
// JSONL is explicitly NOT an input to this function. State reconstruction MUST
// NOT walk the JSONL event log per EV-022 (event-model.md §4.5) and EM-031
// (execution-model.md §4.7). The function signature enforces this structurally:
// there is no JSONL reader parameter.
//
// Spec ref: execution-model.md §4.7 EM-031a; event-model.md §4.5 EV-022.
func DiscoverActiveRuns(ctx context.Context, querier BeadsQuerier, reader BranchTipReader) (ActiveRunSet, error) {
	beadEntries, terminalBeadIDs, err := queryNonTerminalBeads(ctx, querier)
	if err != nil {
		return ActiveRunSet{}, err
	}

	branchRuns, err := scanTaskBranchTips(ctx, reader, terminalBeadIDs)
	if err != nil {
		return ActiveRunSet{}, fmt.Errorf("lifecycle: DiscoverActiveRuns: branch scan: %w", err)
	}

	return unionActiveRuns(beadEntries, branchRuns), nil
}

// NewBeadsQuerierFromAdapter wraps a *brcli.Adapter as a BeadsQuerier for
// production use. The adapter must be non-nil.
//
// Spec ref: execution-model.md §4.7 EM-031a.
func NewBeadsQuerierFromAdapter(adapter *brcli.Adapter) BeadsQuerier {
	return adapter
}

type beadRunEntry struct {
	// beadID is the Beads bead identifier.
	beadID core.BeadID
}

func queryNonTerminalBeads(ctx context.Context, querier BeadsQuerier) (
	beadEntries []beadRunEntry, terminalBeadIDs map[core.BeadID]struct{}, err error,
) {
	terminalBeadIDs = make(map[core.BeadID]struct{})

	nonTerminalStatuses := []string{"open", "in_progress", "blocked", "deferred", "draft", "pinned"}

	for _, status := range nonTerminalStatuses {
		records, queryErr := querier.ListBeadsByStatus(ctx, status)
		if queryErr != nil {
			if isBeadsUnavailable(queryErr) {
				return nil, nil, fmt.Errorf("%w: %w", ErrBeadsUnavailable, queryErr)
			}
			return nil, nil, fmt.Errorf("lifecycle: queryNonTerminalBeads: status=%s: %w", status, queryErr)
		}
		for _, rec := range records {
			beadEntries = append(beadEntries, beadRunEntry{beadID: rec.BeadID})
		}
	}

	for _, status := range core.TerminalCoarseStatuses() {
		records, queryErr := querier.ListBeadsByStatus(ctx, string(status))
		if queryErr != nil {
			if isBeadsUnavailable(queryErr) {
				return nil, nil, fmt.Errorf("%w: %w", ErrBeadsUnavailable, queryErr)
			}
			return nil, nil, fmt.Errorf("lifecycle: queryNonTerminalBeads: terminal status=%s: %w", status, queryErr)
		}
		for _, rec := range records {
			terminalBeadIDs[rec.BeadID] = struct{}{}
		}
	}

	return beadEntries, terminalBeadIDs, nil
}

func scanTaskBranchTips(ctx context.Context, reader BranchTipReader, terminalBeadIDs map[core.BeadID]struct{}) ([]ActiveRunEntry, error) {
	tips, err := reader.ListTaskBranchTips(ctx)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: scanTaskBranchTips: %w", err)
	}

	entries := make([]ActiveRunEntry, 0, len(tips))
	for _, tip := range tips {
		if tip.RunID == "" {
			continue
		}

		if tip.BeadID != "" {
			beadID := core.BeadID(tip.BeadID)
			if _, terminal := terminalBeadIDs[beadID]; terminal {
				continue
			}
		}

		var runID core.RunID
		if unmarshalErr := runID.UnmarshalText([]byte(tip.RunID)); unmarshalErr != nil {
			continue
		}

		var beadIDPtr *core.BeadID
		if tip.BeadID != "" {
			bid := core.BeadID(tip.BeadID)
			beadIDPtr = &bid
		}

		entries = append(entries, ActiveRunEntry{
			RunID:  runID,
			BeadID: beadIDPtr,
			source: activeRunSourceBranch,
		})
	}
	return entries, nil
}

func unionActiveRuns(beadEntries []beadRunEntry, branchEntries []ActiveRunEntry) ActiveRunSet {
	result := make([]ActiveRunEntry, 0, len(beadEntries)+len(branchEntries))

	branchByBeadID := make(map[core.BeadID]int, len(branchEntries))
	for _, e := range branchEntries {
		idx := len(result)
		result = append(result, e)
		if e.BeadID != nil {
			branchByBeadID[*e.BeadID] = idx
		}
	}

	seenBeads := make(map[core.BeadID]struct{}, len(beadEntries))
	for _, be := range beadEntries {
		if _, seen := seenBeads[be.beadID]; seen {
			continue
		}
		seenBeads[be.beadID] = struct{}{}

		if idx, found := branchByBeadID[be.beadID]; found {
			result[idx].source = activeRunSourceBoth
		} else {
			bid := be.beadID
			result = append(result, ActiveRunEntry{
				BeadID: &bid,
				source: activeRunSourceBeads,
			})
		}
	}

	return ActiveRunSet{entries: result}
}

func isBeadsUnavailable(err error) bool {
	return errors.Is(err, brcli.BrUnavailable) || errors.Is(err, brcli.BrDbLocked)
}
