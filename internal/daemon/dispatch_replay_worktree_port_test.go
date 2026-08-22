package daemon

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
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
	cfg, registry := worktreePortWorkers(workers.Worker{
		Name: "worker-a", Transport: "ssh", Host: "worker.example", RepoPath: "/srv/worker/project",
	})
	resolver := newDispatchWorktreeObserverResolverWithOwnership(cfg, func(worker workers.Worker) ltmux.CommandRunner {
		factoryCalls++
		return runner
	}, nil, registry)
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

func TestDispatchWorktreeProductionResolverAcquiresBeforeRunner(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	record := replayFactRunRecord(t, intent)
	location := runpkg.ExecutionLocation{
		Kind: runpkg.ExecutionRemote, WorkerName: "worker-a", Transport: "ssh",
		Host: "worker.example", RepositoryPath: "/srv/worker/project",
	}
	record.Location = &location
	worker := workers.Worker{
		Name: location.WorkerName, Transport: location.Transport, Host: location.Host,
		RepoPath: location.RepositoryPath, Enabled: true, MaxSlots: 1,
	}
	registry := workers.NewRegistry(workers.Config{Workers: []workers.Worker{worker}})
	factoryCalls := 0
	resolver := newDispatchWorktreeObserverResolverWithOwnership(
		workers.Config{Workers: []workers.Worker{worker}},
		func(workers.Worker) ltmux.CommandRunner {
			if registry.InFlight() != 1 {
				t.Fatalf("runner factory ran before bound slot: in-flight %d", registry.InFlight())
			}
			factoryCalls++
			return &ltmux.RecordingRunner{}
		}, nil, registry,
	)
	for range 2 {
		if _, err := resolver(record); err != nil {
			t.Fatal(err)
		}
	}
	if registry.InFlight() != 1 || factoryCalls != 2 {
		t.Fatalf("in-flight = %d, runner factories = %d", registry.InFlight(), factoryCalls)
	}
}

func TestDispatchWorktreeProductionResolverStopsBeforeRunnerWhenWorkerUnavailable(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	record := replayFactRunRecord(t, intent)
	location := runpkg.ExecutionLocation{
		Kind: runpkg.ExecutionRemote, WorkerName: "worker-a", Transport: "ssh",
		Host: "worker.example", RepositoryPath: "/srv/worker/project",
	}
	record.Location = &location
	for _, tc := range []struct {
		name    string
		enabled bool
		fill    bool
	}{
		{name: "disabled"},
		{name: "full", enabled: true, fill: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			worker := workers.Worker{
				Name: location.WorkerName, Transport: location.Transport, Host: location.Host,
				RepoPath: location.RepositoryPath, Enabled: tc.enabled, MaxSlots: 1,
			}
			registry := workers.NewRegistry(workers.Config{Workers: []workers.Worker{worker}})
			if tc.fill {
				if owned, err := registry.SelectBoundWorker(mustReplayRunID(t, "0197d100-0000-7000-8000-000000000076"), ""); err != nil || owned == nil {
					t.Fatalf("fill worker = (%+v, %v)", owned, err)
				}
			}
			factoryCalls := 0
			resolver := newDispatchWorktreeObserverResolverWithOwnership(
				workers.Config{Workers: []workers.Worker{worker}},
				func(workers.Worker) ltmux.CommandRunner {
					factoryCalls++
					return &ltmux.RecordingRunner{}
				}, nil, registry,
			)
			if _, err := resolver(record); !errors.Is(err, errDispatchReplayPending) {
				t.Fatalf("resolver error = %v", err)
			}
			if factoryCalls != 0 {
				t.Fatalf("runner factory calls = %d", factoryCalls)
			}
		})
	}
}

func TestDispatchWorktreeProvisionerSyncsBeforeOneRemoteCreate(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	record := replayFactRunRecord(t, intent)
	location := runpkg.ExecutionLocation{
		Kind: runpkg.ExecutionRemote, WorkerName: "worker-a", Transport: "ssh",
		Host: "worker.example", RepositoryPath: "/srv/worker/project",
	}
	record.Location = &location
	runner := &ltmux.RecordingRunner{CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
		if name == "git" && len(args) >= 3 && args[2] == "rev-parse" {
			return exec.CommandContext(ctx, "printf", "0123456789abcdef0123456789abcdef01234567\\n")
		}
		return exec.CommandContext(ctx, "true")
	}}
	cfg, registry := worktreePortWorkers(workers.Worker{
		Name: location.WorkerName, Transport: location.Transport, Host: location.Host, RepoPath: location.RepositoryPath,
	})
	resolver := newDispatchWorktreeObserverResolverWithOwnership(
		cfg, func(workers.Worker) ltmux.CommandRunner { return runner }, nil, registry)
	provisioner, err := resolver(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := provisioner.PrepareBase(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	if err := provisioner.Create(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	runID := record.RunID.String()
	want := []ltmux.RecordingCall{
		{Name: "git", Args: []string{"-C", location.RepositoryPath, "fetch", "origin", record.ParentCommit}},
		{Name: "git", Args: []string{"-C", location.RepositoryPath, "cat-file", "-t", record.ParentCommit}},
		{Name: "mkdir", Args: []string{"-p", location.RepositoryPath + "/.harmonik/worktrees"}},
		{Name: "git", Args: []string{"-C", location.RepositoryPath, "worktree", "add", "-b", "run/" + runID, location.RepositoryPath + "/.harmonik/worktrees/" + runID, record.ParentCommit}},
		{Name: "git", Args: []string{"-C", location.RepositoryPath + "/.harmonik/worktrees/" + runID, "rev-parse", "HEAD"}},
	}
	if !reflect.DeepEqual(runner.Calls, want) {
		t.Fatalf("runner calls = %#v, want %#v", runner.Calls, want)
	}
}

func TestDispatchWorktreeProvisionerFallbackBindsBothRepositoriesAndRoute(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	record := replayFactRunRecord(t, intent)
	location := runpkg.ExecutionLocation{
		Kind: runpkg.ExecutionRemote, WorkerName: "worker-a", Transport: "ssh",
		Host: "worker.example", RepositoryPath: "/srv/worker/project",
	}
	record.Location = &location
	remote := &ltmux.RecordingRunner{CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
		if name == "git" && len(args) >= 3 && args[2] == "cat-file" {
			return exec.CommandContext(ctx, "false")
		}
		return exec.CommandContext(ctx, "true")
	}}
	local := &ltmux.RecordingRunner{CmdFunc: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "true")
	}}
	cfg, registry := worktreePortWorkers(workers.Worker{
		Name: location.WorkerName, Transport: location.Transport, Host: location.Host, RepoPath: location.RepositoryPath,
	})
	resolver := newDispatchWorktreeObserverResolverWithOwnership(
		cfg, func(workers.Worker) ltmux.CommandRunner { return remote }, local, registry)
	provisioner, err := resolver(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := provisioner.PrepareBase(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	wantRemote := []ltmux.RecordingCall{
		{Name: "git", Args: []string{"-C", location.RepositoryPath, "fetch", "origin", record.ParentCommit}},
		{Name: "git", Args: []string{"-C", location.RepositoryPath, "cat-file", "-t", record.ParentCommit}},
	}
	wantLocal := []ltmux.RecordingCall{{
		Name: "git", Args: []string{
			"-C", record.RepositoryPath,
			"-c", "core.sshCommand=ssh -o ControlMaster=no -o ControlPath=none",
			"push", "ssh://worker.example/srv/worker/project",
			record.ParentCommit + ":refs/harmonik/base",
		},
	}}
	if !reflect.DeepEqual(remote.Calls, wantRemote) || !reflect.DeepEqual(local.Calls, wantLocal) {
		t.Fatalf("remote calls = %#v, want %#v; local calls = %#v, want %#v", remote.Calls, wantRemote, local.Calls, wantLocal)
	}
}

func TestDispatchWorktreeProvisionerLocalPrepareRunsNoCommand(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	record := replayFactRunRecord(t, intent)
	location := runpkg.ExecutionLocation{
		Kind: runpkg.ExecutionLocalShared, RepositoryPath: record.RepositoryPath,
	}
	record.Location = &location
	remoteFactoryCalls := 0
	local := &ltmux.RecordingRunner{}
	resolver := newDispatchWorktreeObserverResolverWithOwnership(workers.Config{}, func(workers.Worker) ltmux.CommandRunner {
		remoteFactoryCalls++
		return &ltmux.RecordingRunner{}
	}, local, nil)
	provisioner, err := resolver(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := provisioner.PrepareBase(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	if remoteFactoryCalls != 0 || len(local.Calls) != 0 {
		t.Fatalf("remote factory calls = %d, local calls = %+v", remoteFactoryCalls, local.Calls)
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
			cfg, registry := worktreePortWorkers(tc.worker)
			resolver := newDispatchWorktreeObserverResolverWithOwnership(cfg, func(workers.Worker) ltmux.CommandRunner {
				factoryCalls++
				return &ltmux.RecordingRunner{}
			}, nil, registry)
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
	cfg, registry := worktreePortWorkers(workers.Worker{
		Name: location.WorkerName, Transport: location.Transport, Host: location.Host, RepoPath: location.RepositoryPath,
	})
	resolver := newDispatchWorktreeObserverResolverWithOwnership(cfg, func(workers.Worker) ltmux.CommandRunner {
		factoryCalls++
		return &ltmux.RecordingRunner{}
	}, nil, registry)
	if _, err := resolver(record); err == nil {
		t.Fatal("resolver accepted unsupported transport")
	}
	if factoryCalls != 0 {
		t.Fatalf("runner factory calls = %d, want zero", factoryCalls)
	}
}

// worktreePortWorkers returns the trusted worker configuration and the process
// registry the daemon builds from it.
//
// It takes ONE worker, not a list, because workers.NewRegistry keeps one:
// workers.PrimaryWorkerIndex returns index 0 or -1, so a second worker in the
// configuration would be dropped without a word. Give the helper a list and the
// next writer learns that only after a test asserts on a route the registry
// never held.
//
// The helper overwrites Enabled and MaxSlots. The resolver takes a slot before
// it hands out a runner, and workers.Registry.AcquireBoundWorker returns no
// worker and no error when the worker is disabled or has no slot. A caller that
// wants a disabled worker or a full registry is asking a different question and
// must build the configuration itself.
func worktreePortWorkers(worker workers.Worker) (workers.Config, *workers.Registry) {
	worker.Enabled, worker.MaxSlots = true, 1
	cfg := workers.Config{Workers: []workers.Worker{worker}}
	return cfg, workers.NewRegistry(cfg)
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
