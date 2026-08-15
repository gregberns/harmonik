package workspace

import (
	"context"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

const (
	dispatchCreateRunID  = "0197d200-0000-7000-8000-000000000021"
	dispatchCreateParent = "0123456789abcdef0123456789abcdef01234567"
)

func TestCreateDispatchWorktreeRemoteUsesOnlyRunner(t *testing.T) {
	runner := &tmux.RecordingRunner{CmdFunc: successfulDispatchCreateCommand}
	err := CreateDispatchWorktree(
		t.Context(), "/worker/repo", dispatchCreateRunID, dispatchCreateParent,
		NoWorktreeRootOverride().WithRunner(runner),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []tmux.RecordingCall{
		{Name: "mkdir", Args: []string{"-p", "/worker/repo/.harmonik/worktrees"}},
		{Name: "git", Args: []string{"-C", "/worker/repo", "worktree", "add", "-b", "run/" + dispatchCreateRunID, "/worker/repo/.harmonik/worktrees/" + dispatchCreateRunID, dispatchCreateParent}},
		{Name: "git", Args: []string{"-C", "/worker/repo/.harmonik/worktrees/" + dispatchCreateRunID, "rev-parse", "HEAD"}},
	}
	if !reflect.DeepEqual(runner.Calls, want) {
		t.Fatalf("runner calls = %#v, want %#v", runner.Calls, want)
	}
}

func TestCreateDispatchWorktreeLocalIsImmediatelyDiscoverable(t *testing.T) {
	repo, parent := tempRepo(t)
	if err := CreateDispatchWorktree(
		t.Context(), repo, dispatchCreateRunID, parent, NoWorktreeRootOverride(),
	); err != nil {
		t.Fatal(err)
	}
	discovered, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered) != 1 {
		t.Fatalf("discovered = %+v", discovered)
	}
	got := discovered[0]
	if got.RunID != dispatchCreateRunID || !got.RegisteredInGit ||
		got.GitBranch != "run/"+dispatchCreateRunID || got.HeadCommit != parent ||
		got.LeaseLock != nil || got.LeaseLockUnreadable || got.HasSessionsDir {
		t.Fatalf("prepared worktree = %+v", got)
	}
}

func TestCreateDispatchWorktreePreservesUncertainRemoteFacts(t *testing.T) {
	tests := []struct {
		name     string
		failCall int
	}{
		{name: "create side effect then error", failCall: 2},
		{name: "head probe error", failCall: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			runner := &tmux.RecordingRunner{CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
				calls++
				if calls == tc.failCall {
					return exec.CommandContext(ctx, "sh", "-c", "exit 1")
				}
				return successfulDispatchCreateCommand(ctx, name, args...)
			}}
			if err := CreateDispatchWorktree(
				t.Context(), "/worker/repo", dispatchCreateRunID, dispatchCreateParent,
				NoWorktreeRootOverride().WithRunner(runner),
			); err == nil {
				t.Fatal("CreateDispatchWorktree() = nil")
			}
			for _, call := range runner.Calls {
				if call.Name == "rm" || call.Name == "rmdir" ||
					(call.Name == "git" && len(call.Args) > 2 && (call.Args[2] == "branch" || call.Args[2] == "worktree" && len(call.Args) > 3 && call.Args[3] == "prune")) {
					t.Fatalf("destructive cleanup call = %#v", call)
				}
			}
		})
	}
}

func TestCreateDispatchWorktreePreservesWrongRemoteHEAD(t *testing.T) {
	const otherHEAD = "abcdef0123456789abcdef0123456789abcdef01"
	runner := &tmux.RecordingRunner{CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
		if name == "git" && len(args) >= 3 && args[len(args)-2] == "rev-parse" {
			return exec.CommandContext(ctx, "sh", "-c", "printf '%s\\n' \"$1\"", "sh", otherHEAD)
		}
		return exec.CommandContext(ctx, "sh", "-c", "exit 0")
	}}
	err := CreateDispatchWorktree(
		t.Context(), "/worker/repo", dispatchCreateRunID, dispatchCreateParent,
		NoWorktreeRootOverride().WithRunner(runner),
	)
	if err == nil || !strings.Contains(err.Error(), otherHEAD) || strings.Contains(err.Error(), "%!w") {
		t.Fatalf("wrong HEAD error = %v", err)
	}
	if len(runner.Calls) != 3 {
		t.Fatalf("calls after wrong HEAD = %#v", runner.Calls)
	}
}

func TestCreateDispatchWorktreeRejectsInvalidInputBeforeEffects(t *testing.T) {
	runner := &tmux.RecordingRunner{}
	tests := []struct {
		name   string
		repo   string
		runID  string
		parent string
	}{
		{name: "relative repository", repo: "repo", runID: dispatchCreateRunID, parent: dispatchCreateParent},
		{name: "invalid run", repo: "/repo", runID: "../run", parent: dispatchCreateParent},
		{name: "empty parent", repo: "/repo", runID: dispatchCreateRunID},
		{name: "symbolic parent", repo: "/repo", runID: dispatchCreateRunID, parent: "HEAD"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := CreateDispatchWorktree(t.Context(), tc.repo, tc.runID, tc.parent, NoWorktreeRootOverride().WithRunner(runner)); err == nil {
				t.Fatal("CreateDispatchWorktree() = nil")
			}
		})
	}
	if len(runner.Calls) != 0 {
		t.Fatalf("invalid input performed calls: %#v", runner.Calls)
	}
}

func successfulDispatchCreateCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	if name == "git" && len(args) >= 3 && args[len(args)-2] == "rev-parse" {
		return exec.CommandContext(ctx, "sh", "-c", "printf '%s\\n' \"$1\"", "sh", dispatchCreateParent)
	}
	return exec.CommandContext(ctx, "sh", "-c", "exit 0")
}
