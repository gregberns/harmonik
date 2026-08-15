package daemon

import (
	"context"
	"errors"
	"fmt"

	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/workers"
	"github.com/gregberns/harmonik/internal/workspace"
)

type dispatchWorktreeObserver interface {
	Observe(context.Context, runpkg.DispatchRecord) ([]workspace.DiscoveredWorktree, error)
}

type dispatchWorktreeObserverResolver func(runpkg.DispatchRecord) (dispatchWorktreeObserver, error)

type locationOwnedDispatchWorktreeObserver struct {
	repositoryPath string
	config         workspace.WorktreeRootConfig
}

func (o locationOwnedDispatchWorktreeObserver) Observe(
	ctx context.Context,
	record runpkg.DispatchRecord,
) ([]workspace.DiscoveredWorktree, error) {
	return workspace.ObserveDispatchWorktree(ctx, o.repositoryPath, record.RunID.String(), o.config)
}

func newDispatchWorktreeObserverResolver(cfg workers.Config) dispatchWorktreeObserverResolver {
	return newDispatchWorktreeObserverResolverWithFactory(cfg, func(worker workers.Worker) ltmux.CommandRunner {
		return ltmux.SSHRunner{
			Host: worker.Host,
			Opts: []string{"-o", "ControlMaster=no", "-o", "ControlPath=none"},
		}
	})
}

func newDispatchWorktreeObserverResolverWithFactory(
	cfg workers.Config,
	remoteRunner func(workers.Worker) ltmux.CommandRunner,
) dispatchWorktreeObserverResolver {
	return func(record runpkg.DispatchRecord) (dispatchWorktreeObserver, error) {
		if err := record.Validate(); err != nil {
			return nil, fmt.Errorf("daemon: validate dispatch worktree owner: %w", err)
		}
		if record.Location == nil {
			return nil, errors.New("daemon: dispatch worktree owner has no execution location")
		}
		location := *record.Location
		switch location.Kind {
		case runpkg.ExecutionLocalIndependent, runpkg.ExecutionLocalShared:
			return locationOwnedDispatchWorktreeObserver{
				repositoryPath: location.RepositoryPath,
				config:         workspace.NoWorktreeRootOverride(),
			}, nil
		case runpkg.ExecutionRemote:
			return resolveRemoteDispatchWorktreeObserver(cfg, location, remoteRunner)
		default:
			return nil, fmt.Errorf("daemon: unsupported dispatch worktree location %q", location.Kind)
		}
	}
}

func resolveRemoteDispatchWorktreeObserver(
	cfg workers.Config,
	location runpkg.ExecutionLocation,
	remoteRunner func(workers.Worker) ltmux.CommandRunner,
) (dispatchWorktreeObserver, error) {
	for _, worker := range cfg.Workers {
		if worker.Name != location.WorkerName {
			continue
		}
		if worker.Transport != location.Transport || worker.Host != location.Host || worker.RepoPath != location.RepositoryPath {
			return nil, fmt.Errorf("daemon: durable worker route for %q no longer matches trusted configuration", worker.Name)
		}
		if worker.Transport != "ssh" {
			return nil, fmt.Errorf("daemon: worker %q has unsupported dispatch worktree transport %q", worker.Name, worker.Transport)
		}
		runner := remoteRunner(worker)
		if runner == nil {
			return nil, fmt.Errorf("daemon: worker %q dispatch worktree runner is not available", worker.Name)
		}
		return locationOwnedDispatchWorktreeObserver{
			repositoryPath: location.RepositoryPath,
			config:         workspace.NoWorktreeRootOverride().WithRunner(runner),
		}, nil
	}
	return nil, fmt.Errorf("daemon: dispatch worktree worker %q is not configured", location.WorkerName)
}
