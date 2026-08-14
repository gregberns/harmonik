package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/workspace"
)

func TestDiscoverMapAndClassifyExactLeasedWorktree(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		// #nosec G204 -- all command arguments come from this test fixture.
		out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	git("init", "-q")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "README")
	git("commit", "-qm", "fixture")
	parent := git("rev-parse", "HEAD")
	parent = parent[:len(parent)-1]

	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	runID := intent.Binding.RunID.String()
	intent.Binding.ParentCommit = parent
	if err := workspace.CreateWorktree(t.Context(), repo, runID, parent, workspace.NoWorktreeRootOverride()); err != nil {
		t.Fatal(err)
	}
	worktreePath := workspace.WorktreePath(repo, runID, workspace.NoWorktreeRootOverride())
	const sessionID = "session-a"
	if err := workspace.CreateSessionLogDir(worktreePath, sessionID); err != nil {
		t.Fatal(err)
	}
	workflowID, err := core.NewWorkflowID("0196a1b2-c3d4-713c-8a1b-2c3d4e5f0001")
	if err != nil {
		t.Fatal(err)
	}
	sidecar := workspace.SessionMetadataSidecar{
		RunID: core.RunID(uuid.MustParse(runID)), NodeID: "node-a", AgentType: "agent",
		WorkflowID: workflowID, LaunchedAt: "2026-08-13T01:02:03Z",
		SchemaVersion: workspace.SessionMetadataSidecarSchemaVersion,
	}
	if err := workspace.WriteSessionMetadataSidecarAtomic(workspace.SessionMetadataSidecarPath(worktreePath, sessionID), &sidecar); err != nil {
		t.Fatal(err)
	}
	if err := workspace.WriteLeaseLockAtomic(workspace.LeaseLockPath(worktreePath), &core.LeaseLockFile{
		RunID: core.RunID(uuid.MustParse(runID)), PID: os.Getpid(),
		CreatedAt: time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC), TTLSec: 60,
	}); err != nil {
		t.Fatal(err)
	}
	discovered, err := workspace.DiscoverWorktrees(t.Context(), repo, workspace.NoWorktreeRootOverride())
	if err != nil {
		t.Fatal(err)
	}
	if fact := dispatch.ClassifyWorktreeObservations(intent, mapDiscoveredWorktrees(discovered)); fact != dispatch.WorktreeLeased {
		t.Fatalf("worktree fact = %q from %+v", fact, discovered)
	}
}
