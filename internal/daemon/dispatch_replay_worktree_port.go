package daemon

import (
	"context"
	"errors"
	"fmt"

	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	runpkg "github.com/gregberns/harmonik/internal/run"
	codesyncpkg "github.com/gregberns/harmonik/internal/transport/codesync"
	"github.com/gregberns/harmonik/internal/workers"
	"github.com/gregberns/harmonik/internal/workspace"
)

type dispatchWorktreeProvisioner interface {
	Observe(context.Context, runpkg.DispatchRecord) ([]workspace.DiscoveredWorktree, error)
	PrepareBase(context.Context, runpkg.DispatchRecord) error
	Create(context.Context, runpkg.DispatchRecord) error
}

type dispatchWorktreeObserverResolver func(runpkg.DispatchRecord) (dispatchWorktreeProvisioner, error)

type locationOwnedDispatchWorktreeObserver struct {
	repositoryPath string
	config         workspace.WorktreeRootConfig
	runner         ltmux.CommandRunner
	workerHost     string
	sshOptions     []string
	localRunner    ltmux.CommandRunner
}

func (o locationOwnedDispatchWorktreeObserver) PrepareBase(ctx context.Context, record runpkg.DispatchRecord) error {
	if o.runner == nil {
		return nil
	}
	return codesyncpkg.EnsureBaseOnWorker(
		ctx, o.runner, o.repositoryPath, record.ParentCommit,
		o.localRunner, record.RepositoryPath, o.workerHost, o.sshOptions,
	)
}

func (o locationOwnedDispatchWorktreeObserver) Create(ctx context.Context, record runpkg.DispatchRecord) error {
	return workspace.CreateDispatchWorktree(ctx, o.repositoryPath, record.RunID.String(), record.ParentCommit, o.config)
}

func (o locationOwnedDispatchWorktreeObserver) Observe(
	ctx context.Context,
	record runpkg.DispatchRecord,
) ([]workspace.DiscoveredWorktree, error) {
	return workspace.ObserveDispatchWorktree(ctx, o.repositoryPath, record.RunID.String(), o.config)
}

func newDispatchWorktreeObserverResolver(cfg workers.Config, registry *workers.Registry) dispatchWorktreeObserverResolver {
	return newDispatchWorktreeObserverResolverWithOwnership(cfg, func(worker workers.Worker) ltmux.CommandRunner {
		return ltmux.SSHRunner{
			Host: worker.Host,
			Opts: []string{"-o", "ControlMaster=no", "-o", "ControlPath=none"},
		}
	}, nil, registry)
}

func newDispatchWorktreeObserverResolverWithFactory(
	cfg workers.Config,
	remoteRunner func(workers.Worker) ltmux.CommandRunner,
) dispatchWorktreeObserverResolver {
	return newDispatchWorktreeObserverResolverWithOwnership(cfg, remoteRunner, nil, nil)
}

func newDispatchWorktreeObserverResolverWithRunners(
	cfg workers.Config,
	remoteRunner func(workers.Worker) ltmux.CommandRunner,
	localRunner ltmux.CommandRunner,
) dispatchWorktreeObserverResolver {
	return newDispatchWorktreeObserverResolverWithOwnership(cfg, remoteRunner, localRunner, nil)
}

func newDispatchWorktreeObserverResolverWithOwnership(
	cfg workers.Config,
	remoteRunner func(workers.Worker) ltmux.CommandRunner,
	localRunner ltmux.CommandRunner,
	registry *workers.Registry,
) dispatchWorktreeObserverResolver {
	return func(record runpkg.DispatchRecord) (dispatchWorktreeProvisioner, error) {
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
			return resolveRemoteDispatchWorktreeObserver(record, cfg, location, remoteRunner, localRunner, registry)
		default:
			return nil, fmt.Errorf("daemon: unsupported dispatch worktree location %q", location.Kind)
		}
	}
}

func resolveRemoteDispatchWorktreeObserver(
	record runpkg.DispatchRecord,
	cfg workers.Config,
	location runpkg.ExecutionLocation,
	remoteRunner func(workers.Worker) ltmux.CommandRunner,
	localRunner ltmux.CommandRunner,
	registry *workers.Registry,
) (dispatchWorktreeProvisioner, error) {
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
		if registry != nil {
			owned, err := registry.AcquireBoundWorker(record.RunID, workers.BoundWorker{
				Name: worker.Name, Transport: worker.Transport, Host: worker.Host, RepositoryPath: worker.RepoPath,
			})
			if err != nil {
				return nil, fmt.Errorf("daemon: acquire worker before dispatch worktree access: %w", err)
			}
			if owned == nil {
				return nil, fmt.Errorf("%w: exact worker is disabled or full", errDispatchReplayPending)
			}
		}
		runner := remoteRunner(worker)
		if runner == nil {
			return nil, fmt.Errorf("daemon: worker %q dispatch worktree runner is not available", worker.Name)
		}
		options := []string{"-o", "ControlMaster=no", "-o", "ControlPath=none"}
		return locationOwnedDispatchWorktreeObserver{
			repositoryPath: location.RepositoryPath,
			config:         workspace.NoWorktreeRootOverride().WithRunner(runner),
			runner:         runner,
			workerHost:     worker.Host,
			sshOptions:     options,
			localRunner:    localRunner,
		}, nil
	}
	return nil, fmt.Errorf("daemon: dispatch worktree worker %q is not configured", location.WorkerName)
}
