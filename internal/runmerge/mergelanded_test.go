package runmerge_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/runmerge"
	"github.com/gregberns/harmonik/internal/workspace"
)

type landedEvent struct {
	runID     core.RunID
	eventType core.EventType
	payload   []byte
}

type landedEmitter struct {
	mu     sync.Mutex
	events []landedEvent
}

func (e *landedEmitter) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, landedEvent{eventType: eventType, payload: append([]byte(nil), payload...)})
	return nil
}

func (e *landedEmitter) EmitWithRunID(_ context.Context, runID core.RunID, eventType core.EventType, payload []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, landedEvent{runID: runID, eventType: eventType, payload: append([]byte(nil), payload...)})
	return nil
}

func (e *landedEmitter) ofType(t core.EventType) []landedEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []landedEvent
	for _, ev := range e.events {
		if ev.eventType == t {
			out = append(out, ev)
		}
	}
	return out
}

func (e *landedEmitter) types() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, 0, len(e.events))
	for _, ev := range e.events {
		out = append(out, string(ev.eventType))
	}
	return out
}

func landedGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (dir=%s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

func landedSetupRepo(t *testing.T) (projectDir, originDir string, runID core.RunID) {
	t.Helper()

	projectDir = t.TempDir()
	landedGit(t, projectDir, "init", "--initial-branch=main")
	landedGit(t, projectDir, "config", "user.email", "daemon@harmonik.local")
	landedGit(t, projectDir, "config", "user.name", "Harmonik Test")
	writeFile(t, filepath.Join(projectDir, "README"), "init\n")
	landedGit(t, projectDir, "add", ".")
	landedGit(t, projectDir, "commit", "-m", "init")

	originDir = t.TempDir()
	landedGit(t, originDir, "init", "--bare", "--initial-branch=main")
	landedGit(t, projectDir, "remote", "add", "origin", originDir)
	landedGit(t, projectDir, "push", "origin", "main")

	runID = core.RunID(uuid.MustParse("0190a000-0000-7000-8000-00000003c4a0"))
	runBranch := workspace.TaskBranchName(runID.String())
	landedGit(t, projectDir, "branch", runBranch, "main")

	wt := filepath.Join(t.TempDir(), "runwt")
	landedGit(t, projectDir, "worktree", "add", wt, runBranch)
	writeFile(t, filepath.Join(wt, "work.txt"), "agent work\n")
	landedGit(t, wt, "add", "work.txt")
	landedGit(t, wt, "commit", "-m", "agent commit")
	landedGit(t, projectDir, "worktree", "remove", "--force", wt)

	return projectDir, originDir, runID
}

// TestRunBranchToTarget_SuccessfulMergeEmitsWorkspaceMergeStatusMerged is the
// hk-3kw4a regression: a merge that lands MUST leave a record in the event
// stream naming the target branch and the commit the target now points at.
func TestRunBranchToTarget_SuccessfulMergeEmitsWorkspaceMergeStatusMerged(t *testing.T) {
	t.Parallel()

	projectDir, originDir, runID := landedSetupRepo(t)
	emitter := &landedEmitter{}

	out := runmerge.RunBranchToTarget(
		context.Background(),
		nil, // nil Submit → runmerge.InlineSubmit
		projectDir,
		runID,
		emitter,
		core.BeadID("hk-3kw4a"),
		"",     // headSHA — unset, so the no-change guard does not fire
		"main", // targetBranch
		nil,    // protectBranches
		"",     // brPath — disables the bead-ledger sync step
	)
	if !out.Success {
		t.Fatalf("precondition: the merge itself failed, so this test is not measuring the event gap: reason=%q noChange=%v",
			out.Reason, out.NoChange)
	}

	wantCommit := landedGit(t, projectDir, "rev-parse", "refs/heads/main")
	if originTip := landedGit(t, originDir, "rev-parse", "refs/heads/main"); originTip != wantCommit {
		t.Fatalf("precondition: origin main %s != local main %s; the push did not publish", originTip, wantCommit)
	}

	got := emitter.ofType(core.EventTypeWorkspaceMergeStatus)
	if len(got) != 1 {
		t.Fatalf("a merge landed on main at %s and the event stream carries %d workspace_merge_status event(s), want exactly 1;"+
			" the stream a consumer would see is %v — with no merge event in it, 'did this run land' cannot be answered from events at all",
			wantCommit, len(got), emitter.types())
	}
	ev := got[0]

	if ev.runID != runID {
		t.Errorf("envelope run_id = %s, want %s", ev.runID, runID)
	}

	var pl core.WorkspaceMergeStatusPayload
	if err := json.Unmarshal(ev.payload, &pl); err != nil {
		t.Fatalf("workspace_merge_status payload does not decode into its own core type: %v\npayload: %s", err, ev.payload)
	}
	if !pl.Valid() {
		t.Errorf("emitted payload fails its own Valid() (event-model.md §8.5.3): %s", ev.payload)
	}

	if pl.Status != core.WorkspaceMergeStatusMerged {
		t.Errorf("status = %q, want %q", pl.Status, core.WorkspaceMergeStatusMerged)
	}
	if pl.TargetBranch != "main" {
		t.Errorf("target_branch = %q, want \"main\" — a consumer that reads the wrong branch is the failure this event prevents", pl.TargetBranch)
	}
	switch {
	case pl.MergeCommitHash == nil:
		t.Error("merge_commit_hash is null on a status=merged event; the landing commit is unrecoverable from the stream")
	case *pl.MergeCommitHash != wantCommit:
		t.Errorf("merge_commit_hash = %q, want %q (the tip refs/heads/main actually points at)", *pl.MergeCommitHash, wantCommit)
	}

	wantSource := workspace.TaskBranchName(runID.String())
	if pl.SourceBranch != wantSource {
		t.Errorf("source_branch = %q, want %q", pl.SourceBranch, wantSource)
	}
	if pl.RunID != runID {
		t.Errorf("payload run_id = %s, want %s", pl.RunID, runID)
	}
	if uuid.UUID(pl.WorkspaceID) != uuid.UUID(runID) {
		t.Errorf("workspace_id = %s, want the run_id UUID %s (WM-004 derivation)", pl.WorkspaceID, runID)
	}

	if _, err := time.Parse(time.RFC3339, pl.ChangedAt); err != nil {
		t.Errorf("changed_at %q is not RFC 3339: %v", pl.ChangedAt, err)
	}
	if dot := strings.LastIndex(pl.ChangedAt, "."); dot < 0 || len(pl.ChangedAt) < dot+4 {
		t.Errorf("changed_at = %q, want millisecond resolution per event-model.md §8.9(h)", pl.ChangedAt)
	}
}

// TestRunBranchToTarget_NoChangeEmitsNoMergeStatus is the other half of the
// distinction. A run that made no commits did not land anything, so it must NOT
// claim a merge. Without this, the presence of the event would stop meaning
// "the target advanced" and the bead's question would still be unanswerable.
func TestRunBranchToTarget_NoChangeEmitsNoMergeStatus(t *testing.T) {
	t.Parallel()

	projectDir, _, runID := landedSetupRepo(t)
	landedGit(t, projectDir, "update-ref",
		"refs/heads/"+workspace.TaskBranchName(runID.String()),
		landedGit(t, projectDir, "rev-parse", "refs/heads/main"))

	emitter := &landedEmitter{}
	out := runmerge.RunBranchToTarget(
		context.Background(), nil, projectDir, runID, emitter,
		core.BeadID("hk-3kw4a"), "", "main", nil, "")

	if !out.NoChange {
		t.Fatalf("precondition: want a no-change outcome; got success=%v reason=%q", out.Success, out.Reason)
	}
	if got := emitter.ofType(core.EventTypeWorkspaceMergeStatus); len(got) != 0 {
		t.Errorf("a no-change run emitted %d workspace_merge_status event(s); it merged nothing and must claim nothing: %s",
			len(got), got[0].payload)
	}
}
