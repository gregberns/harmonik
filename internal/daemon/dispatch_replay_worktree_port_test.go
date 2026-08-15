package daemon

import (
	"context"
	"os/exec"
	"testing"

	"github.com/gregberns/harmonik/internal/dispatch"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/workers"
)

func TestDispatchWorktreeObserverResolverOwnsExactRemoteRoute(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	record := replayFactRunRecord(t, intent)
	location := runpkg.ExecutionLocation{
		Kind: runpkg.ExecutionRemote, WorkerName: "worker-a", Transport: "ssh",
		Host: "worker.example", RepositoryPath: "/srv/worker/project",
	}
	record.Location = &location
	runner := &ltmux.RecordingRunner{CmdFunc: absentDispatchWorktreeCommand}
	factoryCalls := 0
	resolver := newDispatchWorktreeObserverResolverWithFactory(workers.Config{Workers: []workers.Worker{{
		Name: "worker-a", Transport: "ssh", Host: "worker.example", RepoPath: "/srv/worker/project",
	}}}, func(worker workers.Worker) ltmux.CommandRunner {
		factoryCalls++
		return runner
	})
	observer, err := resolver(record)
	if err != nil {
		t.Fatal(err)
	}
	got, err := observer.Observe(t.Context(), record)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil || factoryCalls != 1 || len(runner.Calls) == 0 {
		t.Fatalf("Observe() = %+v; factory=%d calls=%+v", got, factoryCalls, runner.Calls)
	}
}

func TestDispatchWorktreeObserverResolverRejectsEveryRouteDriftBeforeRunner(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	base := replayFactRunRecord(t, intent)
	wantLocation := runpkg.ExecutionLocation{
		Kind: runpkg.ExecutionRemote, WorkerName: "worker-a", Transport: "ssh",
		Host: "old.example", RepositoryPath: "/srv/worker/project",
	}
	base.Location = &wantLocation
	trusted := workers.Worker{
		Name: "worker-a", Transport: "ssh", Host: "old.example", RepoPath: "/srv/worker/project",
	}
	tests := []struct {
		name   string
		worker workers.Worker
	}{
		{name: "missing worker", worker: workers.Worker{Name: "worker-b", Transport: trusted.Transport, Host: trusted.Host, RepoPath: trusted.RepoPath}},
		{name: "transport changed", worker: workers.Worker{Name: trusted.Name, Transport: "local", Host: trusted.Host, RepoPath: trusted.RepoPath}},
		{name: "host changed", worker: workers.Worker{Name: trusted.Name, Transport: trusted.Transport, Host: "new.example", RepoPath: trusted.RepoPath}},
		{name: "repository changed", worker: workers.Worker{Name: trusted.Name, Transport: trusted.Transport, Host: trusted.Host, RepoPath: "/srv/other/project"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			factoryCalls := 0
			resolver := newDispatchWorktreeObserverResolverWithFactory(workers.Config{Workers: []workers.Worker{tc.worker}}, func(workers.Worker) ltmux.CommandRunner {
				factoryCalls++
				return &ltmux.RecordingRunner{}
			})
			if _, err := resolver(base); err == nil {
				t.Fatal("resolver accepted changed worker route")
			}
			if factoryCalls != 0 {
				t.Fatalf("runner factory calls = %d, want zero", factoryCalls)
			}
		})
	}
}

func TestDispatchWorktreeObserverResolverRejectsUnsupportedTransportBeforeRunner(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	record := replayFactRunRecord(t, intent)
	location := runpkg.ExecutionLocation{
		Kind: runpkg.ExecutionRemote, WorkerName: "worker-a", Transport: "https",
		Host: "worker.example", RepositoryPath: "/srv/worker/project",
	}
	record.Location = &location
	factoryCalls := 0
	resolver := newDispatchWorktreeObserverResolverWithFactory(workers.Config{Workers: []workers.Worker{{
		Name: location.WorkerName, Transport: location.Transport, Host: location.Host, RepoPath: location.RepositoryPath,
	}}}, func(workers.Worker) ltmux.CommandRunner {
		factoryCalls++
		return &ltmux.RecordingRunner{}
	})
	if _, err := resolver(record); err == nil {
		t.Fatal("resolver accepted unsupported transport")
	}
	if factoryCalls != 0 {
		t.Fatalf("runner factory calls = %d, want zero", factoryCalls)
	}
}

func absentDispatchWorktreeCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	if name == "git" {
		if len(args) >= 3 && args[2] == "show-ref" {
			return exec.CommandContext(ctx, "false")
		}
		return exec.CommandContext(ctx, "sh", "-c", "printf 'worktree /srv/worker/project\\nHEAD 0123456789abcdef0123456789abcdef01234567\\nbranch refs/heads/main\\n\\n'")
	}
	if name == "sh" {
		return exec.CommandContext(ctx, "printf", "absent\\n")
	}
	return exec.CommandContext(ctx, "false")
}
