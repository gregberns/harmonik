package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// activeRunDiscoveryFixtureFakeQuerier is a test-injectable BeadsQuerier that
// returns deterministic responses keyed by status. Tests set statusMap to
// control which records are returned for each status query.
type activeRunDiscoveryFixtureFakeQuerier struct {
	// statusMap maps status string → records to return for that status.
	// If a status key is absent, returns empty slice (no error).
	statusMap map[string][]core.BeadRecord
	// err, if non-nil, is returned for every call regardless of status.
	err error
}

// ListBeadsByStatus implements BeadsQuerier.
func (f *activeRunDiscoveryFixtureFakeQuerier) ListBeadsByStatus(_ context.Context, status string) ([]core.BeadRecord, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.statusMap == nil {
		return []core.BeadRecord{}, nil
	}
	records, ok := f.statusMap[status]
	if !ok {
		return []core.BeadRecord{}, nil
	}
	return records, nil
}

// activeRunDiscoveryFixtureEmptyQuerier returns a BeadsQuerier that returns
// empty results for all status queries.
func activeRunDiscoveryFixtureEmptyQuerier() BeadsQuerier {
	return &activeRunDiscoveryFixtureFakeQuerier{}
}

// activeRunDiscoveryFixtureErrorQuerier returns a BeadsQuerier that returns
// the given error for every call.
func activeRunDiscoveryFixtureErrorQuerier(err error) BeadsQuerier {
	return &activeRunDiscoveryFixtureFakeQuerier{err: err}
}

// activeRunDiscoveryFixtureBeadRecord builds a minimal valid BeadRecord for use
// in test status maps.
func activeRunDiscoveryFixtureBeadRecord(id string, status core.CoarseStatus) core.BeadRecord {
	return core.BeadRecord{
		BeadID:        core.BeadID(id),
		Title:         "Test bead " + id,
		BeadType:      "task",
		Status:        status,
		AuditTrailRef: id,
	}
}

// activeRunDiscoveryFixtureQuerierWithNonTerminal returns a BeadsQuerier where
// the given bead IDs appear as `open` (non-terminal) and all other statuses
// return empty.
func activeRunDiscoveryFixtureQuerierWithNonTerminal(beadIDs ...string) BeadsQuerier {
	records := make([]core.BeadRecord, 0, len(beadIDs))
	for _, id := range beadIDs {
		records = append(records, activeRunDiscoveryFixtureBeadRecord(id, core.CoarseStatusOpen))
	}
	return &activeRunDiscoveryFixtureFakeQuerier{
		statusMap: map[string][]core.BeadRecord{
			"open": records,
		},
	}
}

// activeRunDiscoveryFixtureQuerierWithTerminal returns a BeadsQuerier where
// the given bead IDs appear as `closed` (terminal) and all other statuses
// return empty.
func activeRunDiscoveryFixtureQuerierWithTerminal(beadIDs ...string) BeadsQuerier {
	records := make([]core.BeadRecord, 0, len(beadIDs))
	for _, id := range beadIDs {
		records = append(records, activeRunDiscoveryFixtureBeadRecord(id, core.CoarseStatusClosed))
	}
	return &activeRunDiscoveryFixtureFakeQuerier{
		statusMap: map[string][]core.BeadRecord{
			"closed": records,
		},
	}
}

// fakeBranchTipReader is a test-injectable BranchTipReader that returns a
// deterministic list of branch tips without invoking git.
type fakeBranchTipReader struct {
	tips []BranchTip
	err  error
}

// ListTaskBranchTips implements BranchTipReader.
func (f *fakeBranchTipReader) ListTaskBranchTips(_ context.Context) ([]BranchTip, error) {
	return f.tips, f.err
}

// activeRunDiscoveryFixtureEmptyReader returns a BranchTipReader with no branches.
func activeRunDiscoveryFixtureEmptyReader() BranchTipReader {
	return &fakeBranchTipReader{}
}

// activeRunDiscoveryFixtureReaderWithTips returns a BranchTipReader with the
// given tips.
func activeRunDiscoveryFixtureReaderWithTips(tips ...BranchTip) BranchTipReader {
	return &fakeBranchTipReader{tips: tips}
}

// activeRunDiscoveryFixtureErrorReader returns a BranchTipReader that always
// returns the given error.
func activeRunDiscoveryFixtureErrorReader(err error) BranchTipReader {
	return &fakeBranchTipReader{err: err}
}

// activeRunDiscoveryFixtureRunID returns a valid UUIDv7-shaped string for use
// in tips. We use deterministic fake UUIDs for test repeatability.
func activeRunDiscoveryFixtureRunID(n int) string {
	return fmt.Sprintf("01900000-0000-7000-8000-00000000%04d", n)
}

// --- Tests for DiscoverActiveRuns ---

// TestEM031a_DiscoverActiveRuns_EmptyBeadsEmptyBranches verifies that an empty
// Beads result and no task branches produces an empty ActiveRunSet.
//
// Spec ref: execution-model.md §4.7 EM-031a.
func TestEM031a_DiscoverActiveRuns_EmptyBeadsEmptyBranches(t *testing.T) {
	t.Parallel()

	set, err := DiscoverActiveRuns(t.Context(), activeRunDiscoveryFixtureEmptyQuerier(), activeRunDiscoveryFixtureEmptyReader())
	if err != nil {
		t.Fatalf("DiscoverActiveRuns: unexpected error: %v", err)
	}
	if set.Len() != 0 {
		t.Errorf("ActiveRunSet.Len() = %d; want 0", set.Len())
	}
}

// TestEM031a_DiscoverActiveRuns_BeadsUnavailable_ReturnsErrBeadsUnavailable
// verifies that when Beads is unreachable (BrDbLocked), DiscoverActiveRuns
// returns ErrBeadsUnavailable and does NOT proceed.
//
// Spec ref: execution-model.md §4.7 EM-031a — "Beads is unreachable at startup
// (SQLite locked, CLI missing, `br` hang beyond timeout), active-run discovery
// MUST NOT proceed."
func TestEM031a_DiscoverActiveRuns_BeadsUnavailable_ReturnsErrBeadsUnavailable(t *testing.T) {
	t.Parallel()

	querier := activeRunDiscoveryFixtureErrorQuerier(brcli.BrDbLocked)
	_, err := DiscoverActiveRuns(t.Context(), querier, activeRunDiscoveryFixtureEmptyReader())
	if err == nil {
		t.Fatal("expected ErrBeadsUnavailable, got nil")
	}
	if !errors.Is(err, ErrBeadsUnavailable) {
		t.Errorf("errors.Is(err, ErrBeadsUnavailable) = false; got: %v", err)
	}
}

// TestEM031a_DiscoverActiveRuns_BrUnavailable_ReturnsErrBeadsUnavailable
// verifies that when br returns BrUnavailable (timeout / exec failure),
// DiscoverActiveRuns returns ErrBeadsUnavailable.
//
// Spec ref: execution-model.md §4.7 EM-031a.
func TestEM031a_DiscoverActiveRuns_BrUnavailable_ReturnsErrBeadsUnavailable(t *testing.T) {
	t.Parallel()

	querier := activeRunDiscoveryFixtureErrorQuerier(brcli.BrUnavailable)
	_, err := DiscoverActiveRuns(t.Context(), querier, activeRunDiscoveryFixtureEmptyReader())
	if err == nil {
		t.Fatal("expected ErrBeadsUnavailable, got nil")
	}
	if !errors.Is(err, ErrBeadsUnavailable) {
		t.Errorf("errors.Is(err, ErrBeadsUnavailable) = false; got: %v", err)
	}
}

// TestEM031a_DiscoverActiveRuns_BranchWithNoRunID_Excluded verifies that a
// task branch whose tip commit has no Harmonik-Run-ID trailer is excluded from
// the active-run set (not a harmonik checkpoint commit).
//
// Spec ref: execution-model.md §4.7 EM-031a.
func TestEM031a_DiscoverActiveRuns_BranchWithNoRunID_Excluded(t *testing.T) {
	t.Parallel()

	reader := activeRunDiscoveryFixtureReaderWithTips(BranchTip{
		BranchName: "run/no-trailer-branch",
		RunID:      "", // no Harmonik-Run-ID trailer
		BeadID:     "",
	})

	set, err := DiscoverActiveRuns(t.Context(), activeRunDiscoveryFixtureEmptyQuerier(), reader)
	if err != nil {
		t.Fatalf("DiscoverActiveRuns: unexpected error: %v", err)
	}
	if set.Len() != 0 {
		t.Errorf("ActiveRunSet.Len() = %d; want 0 (branch without RunID must be excluded)", set.Len())
	}
}

// TestEM031a_DiscoverActiveRuns_BranchWithRunID_Included verifies that a task
// branch whose tip carries a valid Harmonik-Run-ID trailer appears in the
// active-run set when no terminal-state bead exists for it.
//
// Spec ref: execution-model.md §4.7 EM-031a — "branches whose tip carries a
// Harmonik-Run-ID trailer matching no terminal-state bead."
func TestEM031a_DiscoverActiveRuns_BranchWithRunID_Included(t *testing.T) {
	t.Parallel()

	runID := activeRunDiscoveryFixtureRunID(1)
	reader := activeRunDiscoveryFixtureReaderWithTips(BranchTip{
		BranchName: "run/" + runID,
		RunID:      runID,
		BeadID:     "",
	})

	set, err := DiscoverActiveRuns(t.Context(), activeRunDiscoveryFixtureEmptyQuerier(), reader)
	if err != nil {
		t.Fatalf("DiscoverActiveRuns: unexpected error: %v", err)
	}
	if set.Len() != 1 {
		t.Fatalf("ActiveRunSet.Len() = %d; want 1", set.Len())
	}
	entries := set.Entries()
	if entries[0].RunID.String() != runID {
		t.Errorf("entries[0].RunID = %q; want %q", entries[0].RunID.String(), runID)
	}
}

// TestEM031a_DiscoverActiveRuns_BranchWithTerminalBeadID_Excluded verifies that
// a task branch whose Harmonik-Bead-ID trailer corresponds to a closed bead is
// EXCLUDED from the active-run set (run is already complete).
//
// Spec ref: execution-model.md §4.7 EM-031a — "A run whose current state is
// `completed`, `failed`, or `canceled` is NOT in the active-run set."
func TestEM031a_DiscoverActiveRuns_BranchWithTerminalBeadID_Excluded(t *testing.T) {
	t.Parallel()

	const closedBeadID = "hk-done.1"
	runID := activeRunDiscoveryFixtureRunID(2)

	// Querier: closed bead exists in terminal set.
	querier := activeRunDiscoveryFixtureQuerierWithTerminal(closedBeadID)
	// Branch: points to the closed bead.
	reader := activeRunDiscoveryFixtureReaderWithTips(BranchTip{
		BranchName: "run/" + runID,
		RunID:      runID,
		BeadID:     closedBeadID,
	})

	set, err := DiscoverActiveRuns(t.Context(), querier, reader)
	if err != nil {
		t.Fatalf("DiscoverActiveRuns: unexpected error: %v", err)
	}
	if set.Len() != 0 {
		t.Errorf("ActiveRunSet.Len() = %d; want 0 (terminal bead must exclude branch run)", set.Len())
	}
}

// TestEM031a_DiscoverActiveRuns_NonTerminalBead_Included verifies that a
// non-terminal bead produces an entry in the active-run set even when no
// task branch exists for it yet (claimed but not yet checkpointed).
//
// Spec ref: execution-model.md §4.7 EM-031a — "(Beads-linked runs) ∪
// (branches whose tip carries a Harmonik-Run-ID trailer matching no terminal-state bead)."
func TestEM031a_DiscoverActiveRuns_NonTerminalBead_Included(t *testing.T) {
	t.Parallel()

	const openBeadID = "hk-test.1"
	querier := activeRunDiscoveryFixtureQuerierWithNonTerminal(openBeadID)
	reader := activeRunDiscoveryFixtureEmptyReader() // no branches

	set, err := DiscoverActiveRuns(t.Context(), querier, reader)
	if err != nil {
		t.Fatalf("DiscoverActiveRuns: unexpected error: %v", err)
	}
	if set.Len() != 1 {
		t.Fatalf("ActiveRunSet.Len() = %d; want 1 (non-terminal bead must be included)", set.Len())
	}
	entries := set.Entries()
	if entries[0].BeadID == nil {
		t.Errorf("entries[0].BeadID = nil; want non-nil for Beads-sourced entry")
	} else if *entries[0].BeadID != core.BeadID(openBeadID) {
		t.Errorf("entries[0].BeadID = %q; want %q", *entries[0].BeadID, openBeadID)
	}
}

// TestEM031a_DiscoverActiveRuns_BranchAndBeadMatchByBeadID verifies that when
// a non-terminal bead and a task branch share the same BeadID, they are merged
// into a single entry (not duplicated).
//
// Spec ref: execution-model.md §4.7 EM-031a — union deduplication.
func TestEM031a_DiscoverActiveRuns_BranchAndBeadMatchByBeadID(t *testing.T) {
	t.Parallel()

	const activeBeadID = "hk-active.1"
	runID := activeRunDiscoveryFixtureRunID(3)

	querier := activeRunDiscoveryFixtureQuerierWithNonTerminal(activeBeadID)
	reader := activeRunDiscoveryFixtureReaderWithTips(BranchTip{
		BranchName: "run/" + runID,
		RunID:      runID,
		BeadID:     activeBeadID, // same bead as in Beads
	})

	set, err := DiscoverActiveRuns(t.Context(), querier, reader)
	if err != nil {
		t.Fatalf("DiscoverActiveRuns: unexpected error: %v", err)
	}
	// Union deduplication: one entry (not two).
	if set.Len() != 1 {
		t.Fatalf("ActiveRunSet.Len() = %d; want 1 (bead+branch union should dedup to one entry)", set.Len())
	}
	e := set.Entries()[0]
	if e.RunID.String() != runID {
		t.Errorf("e.RunID = %q; want %q", e.RunID.String(), runID)
	}
	if e.BeadID == nil || *e.BeadID != core.BeadID(activeBeadID) {
		t.Errorf("e.BeadID = %v; want %q", e.BeadID, activeBeadID)
	}
	if e.source != activeRunSourceBoth {
		t.Errorf("e.source = %v; want activeRunSourceBoth", e.source)
	}
}

// TestEM031a_DiscoverActiveRuns_BranchScanError_Propagated verifies that
// errors from the BranchTipReader are propagated (not silently swallowed).
//
// Spec ref: execution-model.md §4.7 EM-031a.
func TestEM031a_DiscoverActiveRuns_BranchScanError_Propagated(t *testing.T) {
	t.Parallel()

	scanErr := errors.New("git for-each-ref: repository not found")
	reader := activeRunDiscoveryFixtureErrorReader(scanErr)

	_, err := DiscoverActiveRuns(t.Context(), activeRunDiscoveryFixtureEmptyQuerier(), reader)
	if err == nil {
		t.Fatal("expected error from branch scan, got nil")
	}
	if !errors.Is(err, scanErr) {
		t.Errorf("errors.Is(err, scanErr) = false; got: %v", err)
	}
}

// TestEM031a_DiscoverActiveRuns_EntriesAreCopy verifies that mutating the
// slice returned by ActiveRunSet.Entries() does not affect subsequent calls.
//
// Spec ref: execution-model.md §4.7 EM-031a (ActiveRunSet contract).
func TestEM031a_DiscoverActiveRuns_EntriesAreCopy(t *testing.T) {
	t.Parallel()

	runID := activeRunDiscoveryFixtureRunID(4)
	reader := activeRunDiscoveryFixtureReaderWithTips(BranchTip{
		BranchName: "run/" + runID,
		RunID:      runID,
	})

	set, err := DiscoverActiveRuns(t.Context(), activeRunDiscoveryFixtureEmptyQuerier(), reader)
	if err != nil {
		t.Fatalf("DiscoverActiveRuns: %v", err)
	}
	if set.Len() != 1 {
		t.Fatalf("Len = %d; want 1", set.Len())
	}

	first := set.Entries()
	// Mutate the returned slice.
	first[0] = ActiveRunEntry{} // zero out

	// Second call must still return the original entry.
	second := set.Entries()
	if second[0].RunID.String() != runID {
		t.Errorf("entries[0].RunID = %q after external mutation; want %q (Entries must return a copy)", second[0].RunID.String(), runID)
	}
}

// TestEM031a_DiscoverActiveRuns_MalformedRunIDOnBranch_Excluded verifies that
// a branch tip with a syntactically invalid Harmonik-Run-ID trailer is skipped
// rather than causing a fatal error. The remaining runs are still discovered.
//
// Spec ref: execution-model.md §4.7 EM-031a.
func TestEM031a_DiscoverActiveRuns_MalformedRunIDOnBranch_Excluded(t *testing.T) {
	t.Parallel()

	validRunID := activeRunDiscoveryFixtureRunID(5)
	reader := activeRunDiscoveryFixtureReaderWithTips(
		BranchTip{BranchName: "run/bad", RunID: "not-a-uuid", BeadID: ""},
		BranchTip{BranchName: "run/" + validRunID, RunID: validRunID, BeadID: ""},
	)

	set, err := DiscoverActiveRuns(t.Context(), activeRunDiscoveryFixtureEmptyQuerier(), reader)
	if err != nil {
		t.Fatalf("DiscoverActiveRuns: unexpected error: %v", err)
	}
	// The malformed entry is silently skipped; the valid one is included.
	if set.Len() != 1 {
		t.Fatalf("ActiveRunSet.Len() = %d; want 1 (malformed UUID skipped, valid one included)", set.Len())
	}
	entries := set.Entries()
	if entries[0].RunID.String() != validRunID {
		t.Errorf("entries[0].RunID = %q; want %q", entries[0].RunID.String(), validRunID)
	}
}

// TestEM031a_DiscoverActiveRuns_DuplicateBeadInMultipleStatusQueries verifies
// that the same bead appearing in multiple status queries is deduplicated in
// the active-run set.
//
// Spec ref: execution-model.md §4.7 EM-031a.
func TestEM031a_DiscoverActiveRuns_DuplicateBeadInMultipleStatusQueries(t *testing.T) {
	t.Parallel()

	const dupBeadID = "hk-dup.1"
	// Return the same bead for both "open" and "in_progress" queries.
	querier := &activeRunDiscoveryFixtureFakeQuerier{
		statusMap: map[string][]core.BeadRecord{
			"open":        {activeRunDiscoveryFixtureBeadRecord(dupBeadID, core.CoarseStatusOpen)},
			"in_progress": {activeRunDiscoveryFixtureBeadRecord(dupBeadID, core.CoarseStatusInProgress)},
		},
	}

	set, err := DiscoverActiveRuns(t.Context(), querier, activeRunDiscoveryFixtureEmptyReader())
	if err != nil {
		t.Fatalf("DiscoverActiveRuns: unexpected error: %v", err)
	}
	// Deduplication: even though the bead appeared in two queries, only one entry.
	if set.Len() != 1 {
		t.Errorf("ActiveRunSet.Len() = %d; want 1 (duplicate bead must be deduplicated)", set.Len())
	}
}

// TestEM031a_ActiveRunEntry_BranchName verifies the BranchName() helper
// returns the correct run/<run_id> format per workspace-model.md §4.2 WM-005.
//
// Spec ref: workspace-model.md §4.2 WM-005 — "task branch MUST be named run/<run_id>."
func TestEM031a_ActiveRunEntry_BranchName(t *testing.T) {
	t.Parallel()

	runIDStr := activeRunDiscoveryFixtureRunID(6)
	var runID core.RunID
	if err := runID.UnmarshalText([]byte(runIDStr)); err != nil {
		t.Fatalf("UnmarshalText: %v", err)
	}

	entry := ActiveRunEntry{RunID: runID}
	got := entry.BranchName()
	want := "run/" + runIDStr
	if got != want {
		t.Errorf("BranchName() = %q; want %q", got, want)
	}
}

// TestEM031a_isTerminalBeadStatus verifies the terminal-status classifier
// returns true for closed/tombstone and false for all other statuses.
//
// Spec ref: execution-model.md §4.7 EM-031a — "completed, failed, or canceled"
// maps to Beads closed/tombstone.
func TestEM031a_isTerminalBeadStatus(t *testing.T) {
	t.Parallel()

	terminalCases := []core.CoarseStatus{
		core.CoarseStatusClosed,
		core.CoarseStatusTombstone,
	}
	for _, s := range terminalCases {
		if !isTerminalBeadStatus(s) {
			t.Errorf("isTerminalBeadStatus(%q) = false; want true", s)
		}
	}

	nonTerminalCases := []core.CoarseStatus{
		core.CoarseStatusOpen,
		core.CoarseStatusInProgress,
		core.CoarseStatusBlocked,
		core.CoarseStatusDeferred,
		core.CoarseStatusDraft,
		core.CoarseStatusPinned,
	}
	for _, s := range nonTerminalCases {
		if isTerminalBeadStatus(s) {
			t.Errorf("isTerminalBeadStatus(%q) = true; want false", s)
		}
	}
}

// TestEM031a_GitBranchTipReader_EmptyRepo verifies that GitBranchTipReader
// returns nil (no tips) when there are no run/* branches.
//
// Spec ref: execution-model.md §4.7 EM-031a; workspace-model.md §4.2 WM-005.
func TestEM031a_GitBranchTipReader_EmptyRepo(t *testing.T) {
	t.Parallel()

	// Create a minimal git repo with no task branches.
	repoDir := t.TempDir()

	//nolint:gosec // G204: arguments are hard-coded constants; repoDir is t.TempDir()
	if out, err := exec.CommandContext(t.Context(), "git", "-C", repoDir, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	//nolint:gosec // G204: arguments are hard-coded constants
	if out, err := exec.CommandContext(t.Context(), "git", "-C", repoDir, "config", "user.email", "test@test").CombinedOutput(); err != nil {
		t.Fatalf("git config email: %v: %s", err, out)
	}
	//nolint:gosec // G204: arguments are hard-coded constants
	if out, err := exec.CommandContext(t.Context(), "git", "-C", repoDir, "config", "user.name", "Test").CombinedOutput(); err != nil {
		t.Fatalf("git config name: %v: %s", err, out)
	}

	reader := GitBranchTipReader{RepoDir: repoDir}
	tips, err := reader.ListTaskBranchTips(t.Context())
	if err != nil {
		t.Fatalf("ListTaskBranchTips: %v", err)
	}
	if len(tips) != 0 {
		t.Errorf("len(tips) = %d; want 0 (no run/* branches)", len(tips))
	}
}

// TestEM031a_GitBranchTipReader_BranchWithTrailers verifies that
// GitBranchTipReader correctly reads Harmonik-Run-ID and Harmonik-Bead-ID
// trailers from a task-branch tip commit.
//
// Spec ref: execution-model.md §4.7 EM-031a; execution-model.md §6.2
// (Harmonik-Run-ID trailer).
func TestEM031a_GitBranchTipReader_BranchWithTrailers(t *testing.T) {
	t.Parallel()

	runIDStr := activeRunDiscoveryFixtureRunID(7)
	const beadID = "hk-trailer.1"

	// Create a git repo with a task branch and a checkpoint commit carrying trailers.
	repoDir := t.TempDir()

	runGit := func(args ...string) {
		t.Helper()
		cmdArgs := append([]string{"-C", repoDir}, args...)
		//nolint:gosec // G204: args are test-only constants and repoDir is t.TempDir()
		out, err := exec.CommandContext(t.Context(), "git", cmdArgs...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	runGit("init")
	runGit("config", "user.email", "test@test")
	runGit("config", "user.name", "Test")

	// Create a root commit on main.
	tmpFile := filepath.Join(repoDir, "README")
	if err := os.WriteFile(tmpFile, []byte("harmonik test repo\n"), 0o644); err != nil {
		t.Fatalf("WriteFile README: %v", err)
	}
	runGit("add", "README")
	runGit("commit", "-m", "root commit")

	// Create the task branch.
	branchName := "run/" + runIDStr
	runGit("checkout", "-b", branchName)

	// Commit a checkpoint with Harmonik-Run-ID + Harmonik-Bead-ID trailers.
	checkpointFile := filepath.Join(repoDir, "checkpoint.txt")
	if err := os.WriteFile(checkpointFile, []byte("state\n"), 0o644); err != nil {
		t.Fatalf("WriteFile checkpoint: %v", err)
	}
	runGit("add", "checkpoint.txt")

	// Trailers require a blank line before them per git trailer convention.
	commitMsg := "checkpoint: node-a\n\n" +
		"Harmonik-Run-ID: " + runIDStr + "\n" +
		"Harmonik-Bead-ID: " + beadID + "\n"
	runGit("commit", "-m", commitMsg)

	// Return to main before calling for-each-ref (the branch still exists as a ref).
	runGit("checkout", "main")

	// Verify GitBranchTipReader finds the branch and parses the trailers.
	reader := GitBranchTipReader{RepoDir: repoDir}
	tips, err := reader.ListTaskBranchTips(t.Context())
	if err != nil {
		t.Fatalf("ListTaskBranchTips: %v", err)
	}
	if len(tips) != 1 {
		t.Fatalf("len(tips) = %d; want 1", len(tips))
	}

	tip := tips[0]
	if tip.RunID != runIDStr {
		t.Errorf("tip.RunID = %q; want %q", tip.RunID, runIDStr)
	}
	if tip.BeadID != beadID {
		t.Errorf("tip.BeadID = %q; want %q", tip.BeadID, beadID)
	}
	if tip.BranchName != branchName {
		t.Errorf("tip.BranchName = %q; want %q", tip.BranchName, branchName)
	}
}

// TestEM031a_NewBeadsQuerierFromAdapter verifies that NewBeadsQuerierFromAdapter
// wraps a *brcli.Adapter as a BeadsQuerier. Uses a non-existent br path; the
// test validates the wrapper is created without panicking.
//
// Spec ref: execution-model.md §4.7 EM-031a.
func TestEM031a_NewBeadsQuerierFromAdapter(t *testing.T) {
	t.Parallel()

	adapter, err := brcli.New("/nonexistent/br")
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	querier := NewBeadsQuerierFromAdapter(adapter)
	if querier == nil {
		t.Fatal("NewBeadsQuerierFromAdapter returned nil")
	}
	// Verify the querier implements BeadsQuerier by calling it (will fail with
	// exec error — that's acceptable; we just confirm it doesn't panic).
	_, _ = querier.ListBeadsByStatus(t.Context(), "open") //nolint:errcheck // exec failure expected; testing wrapper shape
}
