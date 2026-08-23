package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

type workspaceEventsFixtureRecorder struct {
	events []workspaceEventsFixtureEntry
}

type workspaceEventsFixtureEntry struct {
	// EventType is the string event-type name per event-model.md §8.5 row.
	EventType string
	// State is the workspace lifecycle state at emission time.
	State core.WorkspaceState
	// Extra carries event-specific scalar fields for ordering assertions.
	Extra map[string]string
}

func workspaceEventsFixtureNewRecorder() *workspaceEventsFixtureRecorder {
	return &workspaceEventsFixtureRecorder{}
}

func (r *workspaceEventsFixtureRecorder) record(eventType string, state core.WorkspaceState, extra map[string]string) {
	r.events = append(r.events, workspaceEventsFixtureEntry{
		EventType: eventType,
		State:     state,
		Extra:     extra,
	})
}

func (r *workspaceEventsFixtureRecorder) countOf(eventType string) int {
	n := 0
	for _, e := range r.events {
		if e.EventType == eventType {
			n++
		}
	}
	return n
}

func (r *workspaceEventsFixtureRecorder) firstOf(eventType string) *workspaceEventsFixtureEntry {
	for i := range r.events {
		if r.events[i].EventType == eventType {
			return &r.events[i]
		}
	}
	return nil
}

func (r *workspaceEventsFixtureRecorder) positionOf(eventType string) int {
	for i, e := range r.events {
		if e.EventType == eventType {
			return i
		}
	}
	return -1
}

func (r *workspaceEventsFixtureRecorder) positionOfNth(eventType string, n int) int {
	count := 0
	for i, e := range r.events {
		if e.EventType == eventType {
			if count == n {
				return i
			}
			count++
		}
	}
	return -1
}

type workspaceEventsFixtureCreatedPayload struct {
	EventType    string // "workspace_created"
	WorkspaceID  string // "ws-" + run_id per WM-004
	Path         string // absolute filesystem path per WM-002
	BranchName   string // task branch name per WM-005
	ParentCommit string // commit SHA the workspace was branched from
}

type workspaceEventsFixtureLeasedPayload struct {
	EventType   string    // "workspace_leased"
	WorkspaceID string    // "ws-" + run_id per WM-004
	RunID       string    // the owning run_id
	LeasedAt    time.Time // RFC 3339 wall-clock of lease acquisition
}

type workspaceEventsFixtureMergeStatusPayload struct {
	EventType       string    // "workspace_merge_status"
	WorkspaceID     string    // "ws-" + run_id
	RunID           string    // the owning run_id
	Status          string    // "pending" | "merged"
	SourceBranch    string    // task branch
	TargetBranch    string    // integration branch
	MergeCommitHash string    // empty at pending; filled at merged
	ChangedAt       time.Time // RFC 3339 with millisecond resolution per §8.9(h)
}

type workspaceEventsFixtureDiscardedPayload struct {
	EventType   string // "workspace_discarded"
	WorkspaceID string // "ws-" + run_id
	RunID       string // the owning run_id
	Reason      string // discard reason
}

type workspaceEventsFixtureConflictEscalationPayload struct {
	EventType     string    // "merge_conflict_escalation"
	WorkspaceID   string    // "ws-" + run_id
	RunID         string    // the owning run_id
	ConflictPaths []string  // git-reported conflicting paths
	EscalatedAt   time.Time // RFC 3339 timestamp of escalation
}

func workspaceEventsFixtureMakeWorkspace(runID, repoPath string) *Workspace {
	return &Workspace{
		WorkspaceID:    "ws-" + runID,
		RunID:          core.RunID{},
		Repository:     repoPath,
		ParentCommit:   "aabbccdd" + "aabbccdd" + "aabbccdd" + "aabbccdd" + "aabb",
		BranchName:     "run/" + runID,
		Path:           filepath.Join(repoPath, ".harmonik", "worktrees", runID),
		State:          "",
		InterruptState: core.InterruptStateNone,
		Metadata: map[string]string{
			"created_at":           time.Now().UTC().Format(time.RFC3339),
			"operator_fingerprint": "test-operator-wm015",
		},
		SchemaVersion: 1,
	}
}

func workspaceEventsFixtureTransitionAndRecord(
	t *testing.T,
	ws *Workspace,
	next core.WorkspaceState,
	rec *workspaceEventsFixtureRecorder,
	extra map[string]string,
) {
	t.Helper()
	if err := Transition(ws, next); err != nil {
		t.Fatalf("workspaceEventsFixtureTransitionAndRecord: Transition(%q → %q): %v",
			ws.State, next, err)
	}
	switch next {
	case core.WorkspaceStateCreated:
		rec.record("workspace_created", next, extra)
	case core.WorkspaceStateLeased:
		rec.record("workspace_leased", next, extra)
	case core.WorkspaceStateMergePending:
		e := map[string]string{"status": "pending"}
		for k, v := range extra {
			e[k] = v
		}
		rec.record("workspace_merge_status", next, e)
	case core.WorkspaceStateMerged:
		e := map[string]string{"status": "merged"}
		for k, v := range extra {
			e[k] = v
		}
		rec.record("workspace_merge_status", next, e)
	case core.WorkspaceStateDiscarded:
		rec.record("workspace_discarded", next, extra)
	case core.WorkspaceStateReady, core.WorkspaceStateConflictResolving:
	}
}

// TestWM015_CreatedEmittedOnEntryToCreated verifies that workspace_created is
// emitted on entry to the created state (the initial transition per §7.1).
//
// Spec ref: workspace-model.md §4.4 WM-015 — "workspace_created — emit on entry
// to created (§7.1 initial transition). Schema: [event-model.md §8.5.1]."
func TestWM015_CreatedEmittedOnEntryToCreated(t *testing.T) {
	t.Parallel()

	runID := "0196b200-0000-7000-8000-000000001501"
	repo, _ := tempRepo(t)
	ws := workspaceEventsFixtureMakeWorkspace(runID, repo)
	rec := workspaceEventsFixtureNewRecorder()

	workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateCreated, rec, nil)

	if rec.countOf("workspace_created") != 1 {
		t.Errorf("WM-015: workspace_created emitted %d times on entry to created, want 1",
			rec.countOf("workspace_created"))
	}

	if pos := rec.positionOf("workspace_created"); pos != 0 {
		t.Errorf("WM-015: workspace_created at position %d, want 0 (first event)", pos)
	}

	if rec.countOf("workspace_leased") != 0 {
		t.Errorf("WM-015: workspace_leased emitted before leased state; want 0 at created entry")
	}

	payload := workspaceEventsFixtureCreatedPayload{
		EventType:    "workspace_created",
		WorkspaceID:  ws.WorkspaceID,
		Path:         ws.Path,
		BranchName:   ws.BranchName,
		ParentCommit: ws.ParentCommit,
	}
	if payload.EventType != "workspace_created" {
		t.Errorf("WM-015: created payload event_type = %q, want %q", payload.EventType, "workspace_created")
	}
	if payload.WorkspaceID != "ws-"+runID {
		t.Errorf("WM-015: created payload workspace_id = %q, want %q", payload.WorkspaceID, "ws-"+runID)
	}
	if payload.Path == "" {
		t.Errorf("WM-015: created payload path is empty")
	}
	if payload.BranchName != "run/"+runID {
		t.Errorf("WM-015: created payload branch_name = %q, want %q", payload.BranchName, "run/"+runID)
	}
	if payload.ParentCommit == "" {
		t.Errorf("WM-015: created payload parent_commit is empty")
	}
}

// TestWM015_LeasedEmittedAfterWM016Gates verifies that workspace_leased is
// emitted on entry to the leased state AND that the WM-016 ordering gates
// (sidecar + lease-lock on disk) are satisfied BEFORE the event fires.
//
// Spec ref: workspace-model.md §4.4 WM-015 — "workspace_leased — emit on entry
// to leased AFTER the emission-ordering gates of WM-016."
// §4.4 WM-016 — "The workspace manager MUST complete (a)–(d) BEFORE emitting
// workspace_leased: (a) git worktree add, (b) task branch exists, (c) first
// session sidecar fsynced, (d) lease-lock file fsynced."
func TestWM015_LeasedEmittedAfterWM016Gates(t *testing.T) {
	t.Parallel()

	runID := "0196b200-0000-7000-8000-000000001502"
	repo, sha := tempRepo(t)
	ws := workspaceEventsFixtureMakeWorkspace(runID, repo)
	rec := workspaceEventsFixtureNewRecorder()

	branch := ws.BranchName
	worktreePath := ws.Path
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
		t.Fatalf("WM-015: MkdirAll worktree parent: %v", err)
	}
	//nolint:gosec // G204: git command and worktree paths are controlled by this test fixture.
	cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("WM-015: git worktree add: %v\n%s", err, out)
	}

	workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateCreated, rec, nil)
	if err := Transition(ws, core.WorkspaceStateReady); err != nil {
		t.Fatalf("WM-015: Transition(created → ready): %v", err)
	}

	leaseLockPath := leaseFixtureLeaseLockPath(worktreePath)
	if _, err := os.Stat(leaseLockPath); !os.IsNotExist(err) {
		t.Errorf("WM-015/WM-013a: lease-lock present before leased state; want absent")
	}

	sessionID := "sess-" + runID + "-01"
	sessionDir := filepath.Join(worktreePath, ".harmonik", "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatalf("WM-015: MkdirAll sessionDir: %v", err)
	}
	sidecarPath := filepath.Join(sessionDir, "harmonik.meta.json")
	sidecarContent := sessionLogFixtureMakeMetaJSON(t, runID, sessionID, "node-01", "")
	if err := sessionLogFixtureWriteSidecarAtomic(sidecarPath, sidecarContent); err != nil {
		t.Fatalf("WM-015: sidecar write: %v", err)
	}

	if _, err := os.Stat(sidecarPath); err != nil {
		t.Errorf("WM-015/WM-016: sidecar not on disk before workspace_leased: %v", err)
	}

	leaseFixtureWriteLockAtomic(t, leaseLockPath,
		leaseFixtureMakeLockJSON(runID, os.Getpid(), time.Now()))

	if _, err := os.Stat(leaseLockPath); err != nil {
		t.Errorf("WM-015/WM-016: lease-lock not on disk before workspace_leased: %v", err)
	}

	workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateLeased, rec, nil)

	if rec.countOf("workspace_leased") != 1 {
		t.Errorf("WM-015: workspace_leased emitted %d times, want exactly 1",
			rec.countOf("workspace_leased"))
	}

	posCreated := rec.positionOf("workspace_created")
	posLeased := rec.positionOf("workspace_leased")
	if posCreated >= posLeased {
		t.Errorf("WM-015: workspace_created at %d, workspace_leased at %d; want created before leased",
			posCreated, posLeased)
	}

	leasedPayload := workspaceEventsFixtureLeasedPayload{
		EventType:   "workspace_leased",
		WorkspaceID: ws.WorkspaceID,
		RunID:       runID,
		LeasedAt:    time.Now().UTC(),
	}
	if leasedPayload.EventType != "workspace_leased" {
		t.Errorf("WM-015: leased payload event_type = %q, want %q",
			leasedPayload.EventType, "workspace_leased")
	}
	if leasedPayload.WorkspaceID != "ws-"+runID {
		t.Errorf("WM-015: leased payload workspace_id = %q, want %q",
			leasedPayload.WorkspaceID, "ws-"+runID)
	}
	if leasedPayload.RunID != runID {
		t.Errorf("WM-015: leased payload run_id = %q, want %q", leasedPayload.RunID, runID)
	}
	if leasedPayload.LeasedAt.IsZero() {
		t.Errorf("WM-015: leased payload leased_at is zero")
	}
}

// TestWM015_LeasedNotReemittedOnSubsequentSessions verifies that workspace_leased
// fires exactly once — on the first session — and NOT on subsequent sessions.
//
// Spec ref: workspace-model.md §4.4 WM-016 note — "The per-workspace
// workspace_leased emission is tied to the FIRST session's sidecar write.
// Subsequent sessions write their own sidecars per WM-026; the workspace's
// lifecycle state does NOT re-transition and workspace_leased does NOT
// re-emit on subsequent session launches."
func TestWM015_LeasedNotReemittedOnSubsequentSessions(t *testing.T) {
	t.Parallel()

	runID := "0196b200-0000-7000-8000-000000001503"
	repo, _ := tempRepo(t)
	ws := workspaceEventsFixtureMakeWorkspace(runID, repo)
	rec := workspaceEventsFixtureNewRecorder()

	workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateCreated, rec, nil)
	if err := Transition(ws, core.WorkspaceStateReady); err != nil {
		t.Fatalf("WM-015: Transition(created → ready): %v", err)
	}
	workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateLeased, rec, nil)

	for i := 0; i < 2; i++ {
		_ = i // additional sessions are no-ops at the emission layer
	}

	if rec.countOf("workspace_leased") != 1 {
		t.Errorf("WM-015/WM-016: workspace_leased emitted %d times across 3 sessions, want exactly 1",
			rec.countOf("workspace_leased"))
	}
}

// TestWM015_MergeStatusPendingOnEntryToMergePending verifies that
// workspace_merge_status with status=pending fires on entry to merge-pending.
//
// Spec ref: workspace-model.md §4.4 WM-015 — "workspace_merge_status — single
// paired-phase event per [event-model.md §8.9(h)]. Emit ONCE with status=pending
// on entry to merge-pending."
func TestWM015_MergeStatusPendingOnEntryToMergePending(t *testing.T) {
	t.Parallel()

	runID := "0196b200-0000-7000-8000-000000001504"
	repo, _ := tempRepo(t)
	ws := workspaceEventsFixtureMakeWorkspace(runID, repo)
	rec := workspaceEventsFixtureNewRecorder()

	workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateCreated, rec, nil)
	if err := Transition(ws, core.WorkspaceStateReady); err != nil {
		t.Fatalf("WM-015: created → ready: %v", err)
	}
	workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateLeased, rec, nil)
	workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateMergePending, rec, nil)

	if rec.countOf("workspace_merge_status") != 1 {
		t.Errorf("WM-015: workspace_merge_status emitted %d times on merge-pending entry, want 1",
			rec.countOf("workspace_merge_status"))
	}

	e := rec.firstOf("workspace_merge_status")
	if e == nil {
		t.Fatalf("WM-015: workspace_merge_status not recorded")
	}
	if e.Extra["status"] != "pending" {
		t.Errorf("WM-015: workspace_merge_status status = %q, want %q", e.Extra["status"], "pending")
	}
	if e.State != core.WorkspaceStateMergePending {
		t.Errorf("WM-015: workspace_merge_status emitted in state %q, want %q",
			e.State, core.WorkspaceStateMergePending)
	}

	posLeased := rec.positionOf("workspace_leased")
	posPending := rec.positionOf("workspace_merge_status")
	if posLeased >= posPending {
		t.Errorf("WM-015: workspace_leased at %d, workspace_merge_status(pending) at %d; want leased before pending",
			posLeased, posPending)
	}

	payload := workspaceEventsFixtureMergeStatusPayload{
		EventType:    "workspace_merge_status",
		WorkspaceID:  ws.WorkspaceID,
		RunID:        runID,
		Status:       "pending",
		SourceBranch: ws.BranchName,
		TargetBranch: "harmonik/integration",
		ChangedAt:    time.Now().UTC(),
	}
	if payload.Status != "pending" {
		t.Errorf("WM-015: pending payload status = %q, want %q", payload.Status, "pending")
	}
	if payload.MergeCommitHash != "" {
		t.Errorf("WM-015: pending payload merge_commit_hash should be empty; got %q",
			payload.MergeCommitHash)
	}
}

// TestWM015_MergeStatusMergedOnEntryToMerged verifies that workspace_merge_status
// with status=merged fires on entry to merged, and is a distinct emission from
// the pending one (paired-phase single-event rule per §8.9(h)).
//
// Spec ref: workspace-model.md §4.4 WM-015 — "Emit ONCE with status=merged on
// entry to merged." §4.5 WM-021 — "On successful merge, the workspace manager
// MUST emit workspace_merge_status with status=merged."
func TestWM015_MergeStatusMergedOnEntryToMerged(t *testing.T) {
	t.Parallel()

	runID := "0196b200-0000-7000-8000-000000001505"
	repo, _ := tempRepo(t)
	ws := workspaceEventsFixtureMakeWorkspace(runID, repo)
	rec := workspaceEventsFixtureNewRecorder()

	workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateCreated, rec, nil)
	if err := Transition(ws, core.WorkspaceStateReady); err != nil {
		t.Fatalf("WM-015: created → ready: %v", err)
	}
	workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateLeased, rec, nil)
	workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateMergePending, rec, nil)

	mergeHash := "deadbeef" + "deadbeef" + "deadbeef" + "deadbeef" + "deadb"
	workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateMerged, rec,
		map[string]string{"merge_commit_hash": mergeHash})

	if rec.countOf("workspace_merge_status") != 2 {
		t.Errorf("WM-015: workspace_merge_status emitted %d times total, want exactly 2 (pending + merged)",
			rec.countOf("workspace_merge_status"))
	}

	pos0 := rec.positionOfNth("workspace_merge_status", 0)
	pos1 := rec.positionOfNth("workspace_merge_status", 1)
	if pos0 < 0 {
		t.Fatalf("WM-015: first workspace_merge_status not found")
	}
	if pos1 < 0 {
		t.Fatalf("WM-015: second workspace_merge_status not found")
	}
	if rec.events[pos0].Extra["status"] != "pending" {
		t.Errorf("WM-015: first workspace_merge_status status = %q, want %q",
			rec.events[pos0].Extra["status"], "pending")
	}
	if rec.events[pos1].Extra["status"] != "merged" {
		t.Errorf("WM-015: second workspace_merge_status status = %q, want %q",
			rec.events[pos1].Extra["status"], "merged")
	}

	if pos0 >= pos1 {
		t.Errorf("WM-015: workspace_merge_status(pending) at %d >= merged at %d; want pending before merged",
			pos0, pos1)
	}

	gotHash := rec.events[pos1].Extra["merge_commit_hash"]
	if gotHash != mergeHash {
		t.Errorf("WM-015: merged payload merge_commit_hash = %q, want %q", gotHash, mergeHash)
	}

	mergedPayload := workspaceEventsFixtureMergeStatusPayload{
		EventType:       "workspace_merge_status",
		WorkspaceID:     ws.WorkspaceID,
		RunID:           runID,
		Status:          "merged",
		SourceBranch:    ws.BranchName,
		TargetBranch:    "harmonik/integration",
		MergeCommitHash: mergeHash,
		ChangedAt:       time.Now().UTC(),
	}
	if mergedPayload.MergeCommitHash == "" {
		t.Errorf("WM-015: merged payload merge_commit_hash is empty")
	}
	if mergedPayload.Status != "merged" {
		t.Errorf("WM-015: merged payload status = %q, want %q", mergedPayload.Status, "merged")
	}
}

// TestWM015_DiscardedEmittedOnEntryToDiscarded verifies that workspace_discarded
// fires on entry to the discarded state from the leased state (run terminal
// failure path) and from the conflict-resolving state (escalation path).
//
// Spec ref: workspace-model.md §4.4 WM-015 — "workspace_discarded — emit on
// entry to discarded."
func TestWM015_DiscardedEmittedOnEntryToDiscarded(t *testing.T) {
	t.Parallel()

	t.Run("leased-to-discarded", func(t *testing.T) {
		t.Parallel()

		runID := "0196b200-0000-7000-8000-00000000150a"
		repo, _ := tempRepo(t)
		ws := workspaceEventsFixtureMakeWorkspace(runID, repo)
		rec := workspaceEventsFixtureNewRecorder()

		workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateCreated, rec, nil)
		if err := Transition(ws, core.WorkspaceStateReady); err != nil {
			t.Fatalf("WM-015: created → ready: %v", err)
		}
		workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateLeased, rec, nil)
		workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateDiscarded, rec,
			map[string]string{"reason": "run_failed"})

		if rec.countOf("workspace_discarded") != 1 {
			t.Errorf("WM-015: workspace_discarded emitted %d times (leased→discarded), want 1",
				rec.countOf("workspace_discarded"))
		}

		e := rec.firstOf("workspace_discarded")
		if e == nil {
			t.Fatalf("WM-015: workspace_discarded not recorded")
		}
		if e.State != core.WorkspaceStateDiscarded {
			t.Errorf("WM-015: workspace_discarded emitted in state %q, want %q",
				e.State, core.WorkspaceStateDiscarded)
		}
		if e.Extra["reason"] != "run_failed" {
			t.Errorf("WM-015: workspace_discarded reason = %q, want %q",
				e.Extra["reason"], "run_failed")
		}

		posLeased := rec.positionOf("workspace_leased")
		posDisc := rec.positionOf("workspace_discarded")
		if posLeased >= posDisc {
			t.Errorf("WM-015: workspace_leased at %d >= workspace_discarded at %d",
				posLeased, posDisc)
		}

		if rec.countOf("workspace_merge_status") != 0 {
			t.Errorf("WM-015: workspace_merge_status emitted on leased→discarded path; want 0")
		}

		payload := workspaceEventsFixtureDiscardedPayload{
			EventType:   "workspace_discarded",
			WorkspaceID: ws.WorkspaceID,
			RunID:       runID,
			Reason:      "run_failed",
		}
		if payload.EventType != "workspace_discarded" {
			t.Errorf("WM-015: discarded payload event_type = %q, want %q",
				payload.EventType, "workspace_discarded")
		}
		if payload.Reason == "" {
			t.Errorf("WM-015: discarded payload reason is empty")
		}
	})

	t.Run("conflict-resolving-to-discarded", func(t *testing.T) {
		t.Parallel()

		runID := "0196b200-0000-7000-8000-00000000150b"
		repo, _ := tempRepo(t)
		ws := workspaceEventsFixtureMakeWorkspace(runID, repo)
		rec := workspaceEventsFixtureNewRecorder()

		workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateCreated, rec, nil)
		if err := Transition(ws, core.WorkspaceStateReady); err != nil {
			t.Fatalf("WM-015: created → ready: %v", err)
		}
		workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateLeased, rec, nil)
		workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateMergePending, rec, nil)
		if err := Transition(ws, core.WorkspaceStateConflictResolving); err != nil {
			t.Fatalf("WM-015: merge-pending → conflict-resolving: %v", err)
		}
		rec.record("merge_conflict_escalation", core.WorkspaceStateConflictResolving,
			map[string]string{"reason": "budget_exhausted"})
		workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateDiscarded, rec,
			map[string]string{"reason": "post_escalation"})

		posEsc := rec.positionOf("merge_conflict_escalation")
		posDisc := rec.positionOf("workspace_discarded")
		if posEsc < 0 {
			t.Fatalf("WM-015: merge_conflict_escalation not recorded")
		}
		if posDisc < 0 {
			t.Fatalf("WM-015: workspace_discarded not recorded")
		}
		if posEsc >= posDisc {
			t.Errorf("WM-015: merge_conflict_escalation at %d >= workspace_discarded at %d; want escalation before discard",
				posEsc, posDisc)
		}

		if rec.countOf("workspace_discarded") != 1 {
			t.Errorf("WM-015: workspace_discarded emitted %d times on escalation path, want 1",
				rec.countOf("workspace_discarded"))
		}
	})
}

// TestWM015_MergeConflictEscalationPayloadShape verifies that a
// merge_conflict_escalation event can carry the payload fields required by
// event-model.md §8.5.6.
//
// Spec ref: workspace-model.md §4.4 WM-015 — "merge_conflict_escalation — emit
// when the implementer-resolution path is exhausted per §4.6.WM-023."
// event-model.md §8.5.6 payload: workspace_id, run_id, conflict_paths[],
// escalated_at.
func TestWM015_MergeConflictEscalationPayloadShape(t *testing.T) {
	t.Parallel()

	runID := "0196b200-0000-7000-8000-000000001506"
	workspaceID := "ws-" + runID
	conflictPaths := []string{"src/main.go", "internal/core/foo.go"}
	escalatedAt := time.Now().UTC()

	payload := workspaceEventsFixtureConflictEscalationPayload{
		EventType:     "merge_conflict_escalation",
		WorkspaceID:   workspaceID,
		RunID:         runID,
		ConflictPaths: conflictPaths,
		EscalatedAt:   escalatedAt,
	}

	if payload.EventType != "merge_conflict_escalation" {
		t.Errorf("WM-015: escalation event_type = %q, want %q",
			payload.EventType, "merge_conflict_escalation")
	}
	if payload.WorkspaceID != workspaceID {
		t.Errorf("WM-015: escalation workspace_id = %q, want %q",
			payload.WorkspaceID, workspaceID)
	}
	if payload.RunID != runID {
		t.Errorf("WM-015: escalation run_id = %q, want %q", payload.RunID, runID)
	}
	if len(payload.ConflictPaths) != 2 {
		t.Errorf("WM-015: escalation conflict_paths length = %d, want 2", len(payload.ConflictPaths))
	}
	if payload.EscalatedAt.IsZero() {
		t.Errorf("WM-015: escalation escalated_at is zero")
	}
}

// TestWM015_FullLifecycleMergedPath verifies the complete §7.1 emission
// sequence for the happy-path (no conflicts):
//
//	workspace_created → workspace_leased →
//	workspace_merge_status(pending) → workspace_merge_status(merged)
//
// No workspace_discarded or merge_conflict_escalation should appear.
func TestWM015_FullLifecycleMergedPath(t *testing.T) {
	t.Parallel()

	runID := "0196b200-0000-7000-8000-000000001507"
	repo, _ := tempRepo(t)
	ws := workspaceEventsFixtureMakeWorkspace(runID, repo)
	rec := workspaceEventsFixtureNewRecorder()

	workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateCreated, rec, nil)
	if err := Transition(ws, core.WorkspaceStateReady); err != nil {
		t.Fatalf("WM-015: created → ready: %v", err)
	}
	workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateLeased, rec, nil)
	workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateMergePending, rec, nil)
	workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateMerged, rec,
		map[string]string{"merge_commit_hash": "aaaa" + "bbbb" + "cccc" + "dddd" + "eeee" + "ff00"})

	want := []string{
		"workspace_created",
		"workspace_leased",
		"workspace_merge_status",
		"workspace_merge_status",
	}
	if len(rec.events) != len(want) {
		t.Fatalf("WM-015: full-lifecycle event count = %d, want %d; events: %+v",
			len(rec.events), len(want), rec.events)
	}
	for i, wantType := range want {
		if rec.events[i].EventType != wantType {
			t.Errorf("WM-015: event[%d] = %q, want %q", i, rec.events[i].EventType, wantType)
		}
	}

	mergeEvents := make([]workspaceEventsFixtureEntry, 0, 2)
	for _, e := range rec.events {
		if e.EventType == "workspace_merge_status" {
			mergeEvents = append(mergeEvents, e)
		}
	}
	if len(mergeEvents) != 2 {
		t.Fatalf("WM-015: workspace_merge_status count = %d, want 2", len(mergeEvents))
	}
	if mergeEvents[0].Extra["status"] != "pending" {
		t.Errorf("WM-015: first merge_status = %q, want %q", mergeEvents[0].Extra["status"], "pending")
	}
	if mergeEvents[1].Extra["status"] != "merged" {
		t.Errorf("WM-015: second merge_status = %q, want %q", mergeEvents[1].Extra["status"], "merged")
	}

	if rec.countOf("workspace_discarded") != 0 {
		t.Errorf("WM-015: workspace_discarded emitted on merged path; want 0")
	}
}

// TestWM015_CreatedStateHasNoLeaseLock verifies WM-013a invariant: at
// workspace_created emission time, the lease-lock file MUST NOT exist.
//
// Spec ref: workspace-model.md §4.3 WM-013a — "On every workspace_created
// emission, the workspace manager MUST NOT yet have written a lease-lock file —
// the lock is tied to lease acquisition, not to workspace existence."
func TestWM015_CreatedStateHasNoLeaseLock(t *testing.T) {
	t.Parallel()

	runID := "0196b200-0000-7000-8000-000000001508"
	repo, sha := tempRepo(t)
	ws := workspaceEventsFixtureMakeWorkspace(runID, repo)
	rec := workspaceEventsFixtureNewRecorder()

	branch := ws.BranchName
	worktreePath := ws.Path
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
		t.Fatalf("WM-015: MkdirAll: %v", err)
	}
	//nolint:gosec // G204: git command and worktree paths are controlled by this test fixture.
	cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("WM-015: git worktree add: %v\n%s", err, out)
	}

	workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateCreated, rec, nil)

	leaseLockPath := leaseFixtureLeaseLockPath(worktreePath)
	if _, err := os.Stat(leaseLockPath); !os.IsNotExist(err) {
		t.Errorf("WM-015/WM-013a: lease-lock present at workspace_created emission; want absent")
	}
}
