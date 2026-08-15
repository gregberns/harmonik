package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

type dispatchObserveFixture struct {
	pathKinds  map[string]string
	porcelain  string
	branch     string
	findOutput string
	files      map[string]string
}

func (f dispatchObserveFixture) command(ctx context.Context, name string, args ...string) *exec.Cmd {
	output, ok := f.output(name, args...)
	if !ok {
		return exec.CommandContext(ctx, "sh", "-c", "exit 9")
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", "printf '%s' \"$DISPATCH_OBSERVE_OUTPUT\"")
	cmd.Env = append(cmd.Environ(), "DISPATCH_OBSERVE_OUTPUT="+output)
	return cmd
}

func (f dispatchObserveFixture) output(name string, args ...string) (string, bool) {
	switch {
	case name == "git" && len(args) > 0 && args[len(args)-1] == "--porcelain":
		return f.porcelain, true
	case name == "sh" && len(args) == 4:
		value, found := f.pathKinds[args[3]]
		return value + "\n", found
	case name == "sh" && len(args) == 5:
		return f.branch + "\n", f.branch != ""
	case name == "find":
		return f.findOutput, true
	case name == "cat" && len(args) == 1:
		value, found := f.files[args[0]]
		return value, found
	default:
		return "", false
	}
}

func TestObserveDispatchWorktreeRemoteDoesNotReadCoordinatorPath(t *testing.T) {
	repo := t.TempDir()
	runID := dispatchCreateRunID
	coordinatorPath := filepath.Join(repo, ".harmonik", "worktrees", runID)
	if err := os.MkdirAll(coordinatorPath, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(coordinatorPath, "coordinator-only")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := dispatchObserveFixture{
		pathKinds: map[string]string{coordinatorPath: "absent"},
		porcelain: "worktree /worker/main\nHEAD " + dispatchCreateParent + "\nbranch refs/heads/main\n",
		branch:    "absent",
	}
	runner := &tmux.RecordingRunner{CmdFunc: fixture.command}
	got, err := ObserveDispatchWorktree(t.Context(), repo, runID, NoWorktreeRootOverride().WithRunner(runner))
	if err != nil || len(got) != 0 {
		t.Fatalf("remote absence = (%+v, %v)", got, err)
	}
	info, err := os.Stat(sentinel)
	if err != nil || info.Size() != int64(len("keep")) {
		t.Fatalf("coordinator sentinel = (%+v, %v)", info, err)
	}
}

func TestObserveDispatchWorktreeRemotePreparedAndConflictFacts(t *testing.T) {
	repo := "/worker/repo"
	runID := dispatchCreateRunID
	path := filepath.Join(repo, ".harmonik", "worktrees", runID)
	base := dispatchObserveFixture{
		pathKinds: map[string]string{
			path: "directory", LeaseLockPath(path): "absent", SessionLogRootPath(path): "absent",
		},
		porcelain: "worktree " + path + "\nHEAD " + dispatchCreateParent + "\nbranch refs/heads/run/" + runID + "\n",
		branch:    "present",
	}
	t.Run("prepared", func(t *testing.T) {
		runner := &tmux.RecordingRunner{CmdFunc: base.command}
		got, err := ObserveDispatchWorktree(t.Context(), repo, runID, NoWorktreeRootOverride().WithRunner(runner))
		if err != nil || len(got) != 1 {
			t.Fatalf("observation = (%+v, %v)", got, err)
		}
		if !got[0].RegisteredInGit || got[0].GitRegistrationConflict || got[0].GitBranch != "run/"+runID || got[0].HeadCommit != dispatchCreateParent {
			t.Fatalf("prepared authority = %+v", got[0])
		}
	})
	t.Run("indeterminate path", func(t *testing.T) {
		fixture := base
		fixture.pathKinds = map[string]string{path: "indeterminate"}
		runner := &tmux.RecordingRunner{CmdFunc: fixture.command}
		got, err := ObserveDispatchWorktree(t.Context(), repo, runID, NoWorktreeRootOverride().WithRunner(runner))
		if err == nil || got != nil {
			t.Fatalf("indeterminate path = (%+v, %v)", got, err)
		}
	})
	t.Run("wrong type", func(t *testing.T) {
		fixture := base
		fixture.pathKinds = cloneStringMap(base.pathKinds)
		fixture.pathKinds[path] = "symlink"
		runner := &tmux.RecordingRunner{CmdFunc: fixture.command}
		got, err := ObserveDispatchWorktree(t.Context(), repo, runID, NoWorktreeRootOverride().WithRunner(runner))
		if err != nil || len(got) != 1 || !got[0].GitRegistrationConflict {
			t.Fatalf("wrong-type authority = (%+v, %v)", got, err)
		}
	})
	t.Run("standalone branch residue", func(t *testing.T) {
		fixture := base
		fixture.pathKinds = map[string]string{path: "absent"}
		fixture.porcelain = "worktree /worker/main\nHEAD " + dispatchCreateParent + "\nbranch refs/heads/main\n"
		runner := &tmux.RecordingRunner{CmdFunc: fixture.command}
		got, err := ObserveDispatchWorktree(t.Context(), repo, runID, NoWorktreeRootOverride().WithRunner(runner))
		if err != nil || len(got) != 1 || !got[0].GitRegistrationConflict {
			t.Fatalf("branch authority = (%+v, %v)", got, err)
		}
	})
}

func TestObserveDispatchWorktreeRemoteCorruptLeaseFailsClosed(t *testing.T) {
	repo := "/worker/repo"
	runID := dispatchCreateRunID
	path := filepath.Join(repo, ".harmonik", "worktrees", runID)
	lease := LeaseLockPath(path)
	fixture := dispatchObserveFixture{
		pathKinds: map[string]string{
			path: "directory", lease: "regular", SessionLogRootPath(path): "absent",
		},
		porcelain: "worktree " + path + "\nHEAD " + dispatchCreateParent + "\nbranch refs/heads/run/" + runID + "\n",
		branch:    "present", files: map[string]string{lease: "{"},
	}
	runner := &tmux.RecordingRunner{CmdFunc: fixture.command}
	got, err := ObserveDispatchWorktree(t.Context(), repo, runID, NoWorktreeRootOverride().WithRunner(runner))
	if err != nil || len(got) != 1 || !got[0].LeaseLockUnreadable || got[0].LeaseLock != nil {
		t.Fatalf("corrupt lease = (%+v, %v)", got, err)
	}
}

func TestObserveDispatchWorktreeRemoteForeignRegistrationFailsClosed(t *testing.T) {
	repo := "/worker/repo"
	runID := dispatchCreateRunID
	path := filepath.Join(repo, ".harmonik", "worktrees", runID)
	foreign := "/worker/foreign/" + runID
	fixture := dispatchObserveFixture{
		pathKinds: map[string]string{path: "absent"},
		porcelain: "worktree " + foreign + "\nHEAD " + dispatchCreateParent + "\nbranch refs/heads/run/" + runID + "\n",
		branch:    "present",
	}
	runner := &tmux.RecordingRunner{CmdFunc: fixture.command}
	got, err := ObserveDispatchWorktree(t.Context(), repo, runID, NoWorktreeRootOverride().WithRunner(runner))
	if err != nil || len(got) != 1 || !got[0].GitRegistrationConflict {
		t.Fatalf("foreign registration = (%+v, %v)", got, err)
	}
}

func TestObserveDispatchWorktreeRemoteMalformedGitAuthorityFailsClosed(t *testing.T) {
	repo := "/worker/repo"
	runID := dispatchCreateRunID
	path := filepath.Join(repo, ".harmonik", "worktrees", runID)
	fixture := dispatchObserveFixture{
		pathKinds: map[string]string{path: "absent"}, porcelain: "truncated authority\n", branch: "absent",
	}
	runner := &tmux.RecordingRunner{CmdFunc: fixture.command}
	got, err := ObserveDispatchWorktree(t.Context(), repo, runID, NoWorktreeRootOverride().WithRunner(runner))
	if err == nil || got != nil {
		t.Fatalf("malformed authority = (%+v, %v)", got, err)
	}
}

func TestObserveDispatchWorktreeRemoteCorruptSidecarFailsClosed(t *testing.T) {
	repo := "/worker/repo"
	runID := dispatchCreateRunID
	path := filepath.Join(repo, ".harmonik", "worktrees", runID)
	sessions := SessionLogRootPath(path)
	session := filepath.Join(sessions, "session-a")
	sidecar := filepath.Join(session, "harmonik.meta.json")
	fixture := dispatchObserveFixture{
		pathKinds: map[string]string{
			path: "directory", LeaseLockPath(path): "absent", sessions: "directory",
			session: "directory", sidecar: "regular",
		},
		porcelain:  "worktree " + path + "\nHEAD " + dispatchCreateParent + "\nbranch refs/heads/run/" + runID + "\n",
		branch:     "present",
		findOutput: session + "\n",
		files:      map[string]string{sidecar: "{"},
	}
	runner := &tmux.RecordingRunner{CmdFunc: fixture.command}
	got, err := ObserveDispatchWorktree(t.Context(), repo, runID, NoWorktreeRootOverride().WithRunner(runner))
	if err != nil || len(got) != 1 || !got[0].SessionsPathConflict || got[0].HasExactSidecar {
		t.Fatalf("corrupt sidecar = (%+v, %v)", got, err)
	}
}

func cloneStringMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
