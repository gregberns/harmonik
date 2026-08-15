package daemon

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/dispatch"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/workers"
	"github.com/gregberns/harmonik/internal/workspace"
)

type recordingReplayWorktreeProvisioner struct {
	observations [][]workspace.DiscoveredWorktree
	observeErr   error
	prepareErr   error
	createErr    error
	calls        []string
}

func (p *recordingReplayWorktreeProvisioner) Observe(
	context.Context,
	runpkg.DispatchRecord,
) ([]workspace.DiscoveredWorktree, error) {
	p.calls = append(p.calls, "observe")
	if p.observeErr != nil {
		return nil, p.observeErr
	}
	if len(p.observations) == 0 {
		return nil, nil
	}
	value := p.observations[0]
	p.observations = p.observations[1:]
	return value, nil
}

func (p *recordingReplayWorktreeProvisioner) PrepareBase(context.Context, runpkg.DispatchRecord) error {
	p.calls = append(p.calls, "prepare")
	return p.prepareErr
}

func (p *recordingReplayWorktreeProvisioner) Create(context.Context, runpkg.DispatchRecord) error {
	p.calls = append(p.calls, "create")
	return p.createErr
}

func TestReplayProvisionCreatesThenRequiresExactPreparedAuthority(t *testing.T) {
	projectDir := t.TempDir()
	intent := replayLocationIntent(t, projectDir)
	record := writeReplayLocatedFacts(t, projectDir, intent, runpkg.ExecutionLocation{
		Kind: runpkg.ExecutionLocalShared, RepositoryPath: projectDir,
	})
	provisioner := &recordingReplayWorktreeProvisioner{observations: [][]workspace.DiscoveredWorktree{
		nil,
		{preparedReplayWorktree(intent, record)},
	}}
	executor := dispatchReplayExecutor{
		projectDir: projectDir,
		worktrees: func(got runpkg.DispatchRecord) (dispatchWorktreeProvisioner, error) {
			if !reflect.DeepEqual(got, record) {
				t.Fatalf("resolved record = %+v", got)
			}
			return provisioner, nil
		},
	}
	if err := executor.execute(t.Context(), dispatchReplayStep{Intent: intent, Action: dispatch.ResumeProvision}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(provisioner.calls, []string{"observe", "prepare", "create", "observe"}) {
		t.Fatalf("calls = %v", provisioner.calls)
	}
}

func TestReplayProvisionClassifiesCreateErrorByReloadedAuthority(t *testing.T) {
	tests := []struct {
		name         string
		wantSuccess  bool
		wantPending  bool
		wantConflict bool
		createErr    error
	}{
		{name: "effect happened", wantSuccess: true, createErr: errors.New("create result uncertain")},
		{name: "full absence", wantPending: true, createErr: errors.New("create result uncertain")},
		{name: "partial conflict", wantConflict: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			intent := replayLocationIntent(t, projectDir)
			record := writeReplayLocatedFacts(t, projectDir, intent, runpkg.ExecutionLocation{
				Kind: runpkg.ExecutionLocalShared, RepositoryPath: projectDir,
			})
			var after []workspace.DiscoveredWorktree
			if tc.wantSuccess {
				after = []workspace.DiscoveredWorktree{preparedReplayWorktree(intent, record)}
			}
			if tc.wantConflict {
				conflict := preparedReplayWorktree(intent, record)
				conflict.GitRegistrationConflict = true
				after = []workspace.DiscoveredWorktree{conflict}
			}
			provisioner := &recordingReplayWorktreeProvisioner{
				observations: [][]workspace.DiscoveredWorktree{nil, after},
				createErr:    tc.createErr,
			}
			executor := dispatchReplayExecutor{
				projectDir: projectDir,
				worktrees:  func(runpkg.DispatchRecord) (dispatchWorktreeProvisioner, error) { return provisioner, nil },
			}
			err := executor.execute(t.Context(), dispatchReplayStep{Intent: intent, Action: dispatch.ResumeProvision})
			if (err == nil) != tc.wantSuccess || errors.Is(err, errDispatchReplayPending) != tc.wantPending {
				t.Fatalf("execute error = %v, want success %v pending %v", err, tc.wantSuccess, tc.wantPending)
			}
			if err != nil && strings.Contains(err.Error(), "%!w") {
				t.Fatalf("execute error has formatting artifact: %v", err)
			}
			if !reflect.DeepEqual(provisioner.calls, []string{"observe", "prepare", "create", "observe"}) {
				t.Fatalf("calls = %v", provisioner.calls)
			}
		})
	}
}

func TestReplayProvisionRemoteAcquiresExactSlotBeforeIO(t *testing.T) {
	projectDir := t.TempDir()
	intent := replayLocationIntent(t, projectDir)
	location := runpkg.ExecutionLocation{
		Kind: runpkg.ExecutionRemote, WorkerName: "worker-a", Transport: "ssh",
		Host: "worker.example", RepositoryPath: "/srv/worker/project",
	}
	record := writeReplayLocatedFacts(t, projectDir, intent, location)
	for _, tc := range []struct {
		name        string
		enabled     bool
		fill        bool
		wantCalls   bool
		wantPending bool
	}{
		{name: "available", enabled: true, wantCalls: true},
		{name: "disabled", wantPending: true},
		{name: "full", enabled: true, fill: true, wantPending: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := workers.NewRegistry(workers.Config{Workers: []workers.Worker{{
				Name: location.WorkerName, Transport: location.Transport, Host: location.Host,
				RepoPath: location.RepositoryPath, Enabled: tc.enabled, MaxSlots: 1,
			}}})
			if tc.fill {
				if worker, err := registry.SelectBoundWorker(mustReplayRunID(t, "0197d100-0000-7000-8000-000000000075"), ""); err != nil || worker == nil {
					t.Fatalf("fill worker = (%+v, %v)", worker, err)
				}
			}
			provisioner := &recordingReplayWorktreeProvisioner{observations: [][]workspace.DiscoveredWorktree{{preparedReplayWorktree(intent, record)}}}
			resolverCalls := 0
			executor := dispatchReplayExecutor{
				projectDir: projectDir, workers: registry,
				worktrees: func(runpkg.DispatchRecord) (dispatchWorktreeProvisioner, error) {
					resolverCalls++
					return provisioner, nil
				},
			}
			err := executor.execute(t.Context(), dispatchReplayStep{Intent: intent, Action: dispatch.ResumeProvision})
			if errors.Is(err, errDispatchReplayPending) != tc.wantPending {
				t.Fatalf("execute error = %v", err)
			}
			if (resolverCalls > 0) != tc.wantCalls || (len(provisioner.calls) > 0) != tc.wantCalls {
				t.Fatalf("resolver=%d calls=%v", resolverCalls, provisioner.calls)
			}
			wantSlots := 0
			if tc.wantCalls {
				wantSlots = 1
			} else if tc.fill {
				wantSlots = 1
			}
			if registry.InFlight() != wantSlots {
				t.Fatalf("in-flight = %d, want %d", registry.InFlight(), wantSlots)
			}
		})
	}
}

func writeReplayLocatedFacts(
	t *testing.T,
	projectDir string,
	intent dispatch.Intent,
	location runpkg.ExecutionLocation,
) runpkg.DispatchRecord {
	t.Helper()
	writeReplayLocationFacts(t, projectDir, intent, false, "")
	base := loadReplayLocationRecord(t, projectDir, intent)
	located, err := base.BindLocation(location)
	if err != nil {
		t.Fatal(err)
	}
	if err := runpkg.AdvanceDispatchRecord(projectDir, base, located); err != nil {
		t.Fatal(err)
	}
	return located
}

func preparedReplayWorktree(intent dispatch.Intent, record runpkg.DispatchRecord) workspace.DiscoveredWorktree {
	return workspace.DiscoveredWorktree{
		RunID:           intent.Binding.RunID.String(),
		WorktreePath:    workspace.WorktreePath(record.Location.RepositoryPath, intent.Binding.RunID.String(), workspace.NoWorktreeRootOverride()),
		RegisteredInGit: true,
		GitBranch:       workspace.TaskBranchName(intent.Binding.RunID.String()),
		HeadCommit:      intent.Binding.ParentCommit,
	}
}
