package daemon

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/dispatch"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/queue"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/workers"
)

func TestReplayLocationBindsExactRemoteRouteWithOneSlot(t *testing.T) {
	projectDir := t.TempDir()
	intent := replayLocationIntent(t, projectDir)
	writeReplayLocationFacts(t, projectDir, intent, false, "")
	registry := workers.NewRegistry(workers.Config{Workers: []workers.Worker{{
		Name: "worker-a", Transport: "ssh", Host: "worker.example", RepoPath: "/srv/worker/project",
		Enabled: true, MaxSlots: 1,
	}}})
	executor := dispatchReplayExecutor{
		projectDir: projectDir, workers: registry, localKind: runpkg.ExecutionLocalShared,
	}
	if err := executor.execute(t.Context(), dispatchReplayStep{Intent: intent, Action: dispatch.ResumeProvision}); err != nil {
		t.Fatal(err)
	}
	record := loadReplayLocationRecord(t, projectDir, intent)
	want := runpkg.ExecutionLocation{
		Kind: runpkg.ExecutionRemote, WorkerName: "worker-a", Transport: "ssh",
		Host: "worker.example", RepositoryPath: "/srv/worker/project",
	}
	if record.Location == nil || *record.Location != want || registry.InFlight() != 1 {
		t.Fatalf("location = %+v, in-flight %d", record.Location, registry.InFlight())
	}
}

func TestReplayLocationPreservesWorkerFallbackRules(t *testing.T) {
	tests := []struct {
		name        string
		target      string
		enabled     bool
		fill        bool
		nilRegistry bool
		wantRemote  bool
	}{
		{name: "named worker", target: "worker-a", enabled: true, wantRemote: true},
		{name: "unknown target", target: "worker-b", enabled: true},
		{name: "disabled worker", enabled: false},
		{name: "full worker", enabled: true, fill: true},
		{name: "no registry", nilRegistry: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			intent := replayLocationIntent(t, projectDir)
			writeReplayLocationFacts(t, projectDir, intent, false, tc.target)
			var registry *workers.Registry
			if !tc.nilRegistry {
				registry = workers.NewRegistry(workers.Config{Workers: []workers.Worker{{
					Name: "worker-a", Transport: "ssh", Host: "worker.example", RepoPath: "/srv/worker/project",
					Enabled: tc.enabled, MaxSlots: 1,
				}}})
				if tc.fill {
					if worker, err := registry.SelectBoundWorker(mustReplayRunID(t, "0197d100-0000-7000-8000-000000000071"), ""); err != nil || worker == nil {
						t.Fatalf("fill worker = (%+v, %v)", worker, err)
					}
				}
			}
			executor := dispatchReplayExecutor{projectDir: projectDir, workers: registry, localKind: runpkg.ExecutionLocalShared}
			if err := executor.execute(t.Context(), dispatchReplayStep{Intent: intent, Action: dispatch.ResumeProvision}); err != nil {
				t.Fatal(err)
			}
			record := loadReplayLocationRecord(t, projectDir, intent)
			gotRemote := record.Location != nil && record.Location.Kind == runpkg.ExecutionRemote
			if gotRemote != tc.wantRemote {
				t.Fatalf("location = %+v, want remote %v", record.Location, tc.wantRemote)
			}
		})
	}
}

func TestReplayLocationRejectsEveryRunBindingMismatchBeforeWorkerSlot(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*runpkg.DispatchRecord)
	}{
		{name: "run id", mutate: func(record *runpkg.DispatchRecord) {
			record.RunID = mustReplayRunID(t, "0197d100-0000-7000-8000-000000000072")
		}},
		{name: "bead id", mutate: func(record *runpkg.DispatchRecord) { record.BeadID = "hk-other" }},
		{name: "queue name", mutate: func(record *runpkg.DispatchRecord) { record.QueueName = "other" }},
		{name: "queue id", mutate: func(record *runpkg.DispatchRecord) { record.QueueID = "0197d100-0000-7000-8000-000000000073" }},
		{name: "group index", mutate: func(record *runpkg.DispatchRecord) { record.GroupIndex++ }},
		{name: "item index", mutate: func(record *runpkg.DispatchRecord) { record.ItemIndex++ }},
		{name: "claim transition", mutate: func(record *runpkg.DispatchRecord) {
			record.ClaimTransitionID = mustReplayTransitionID(t, "0197d100-0000-7000-8000-000000000074")
		}},
		{name: "parent commit", mutate: func(record *runpkg.DispatchRecord) { record.ParentCommit = "1123456789abcdef0123456789abcdef01234567" }},
		{name: "repository", mutate: func(record *runpkg.DispatchRecord) { record.RepositoryPath = "/srv/other/project" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			intent := replayLocationIntent(t, projectDir)
			writeReplayReaderQueue(t, projectDir, intent, true)
			record, err := runpkg.NewDispatchRecord(intent.Binding, time.Date(2026, 8, 15, 2, 3, 4, 0, time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(&record)
			if err := runpkg.CreateDispatchRecord(projectDir, record); err != nil {
				t.Fatal(err)
			}
			registry := workers.NewRegistry(workers.Config{Workers: []workers.Worker{{
				Name: "worker-a", Transport: "ssh", Host: "worker.example", RepoPath: "/srv/worker/project",
				Enabled: true, MaxSlots: 1,
			}}})
			executor := dispatchReplayExecutor{projectDir: projectDir, workers: registry, localKind: runpkg.ExecutionLocalShared}
			if err := executor.execute(t.Context(), dispatchReplayStep{Intent: intent, Action: dispatch.ResumeProvision}); err == nil {
				t.Fatal("replay accepted mismatched run record")
			}
			if registry.InFlight() != 0 {
				t.Fatalf("mismatch consumed %d worker slots", registry.InFlight())
			}
		})
	}
}

func TestReplayLocationReleasesOnlyDefiniteAdvanceFailure(t *testing.T) {
	for _, tc := range []struct {
		name         string
		advanceErr   error
		wantInFlight int
	}{
		{name: "definite", advanceErr: errors.New("definite replace failure")},
		{name: "ambiguous", advanceErr: &runpkg.DispatchAmbiguousError{Err: errors.New("sync uncertain")}, wantInFlight: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			intent := replayLocationIntent(t, projectDir)
			writeReplayLocationFacts(t, projectDir, intent, false, "")
			registry := workers.NewRegistry(workers.Config{Workers: []workers.Worker{{
				Name: "worker-a", Transport: "ssh", Host: "worker.example", RepoPath: "/srv/worker/project",
				Enabled: true, MaxSlots: 1,
			}}})
			executor := dispatchReplayExecutor{
				projectDir: projectDir, workers: registry, localKind: runpkg.ExecutionLocalShared,
				advanceRun: func(string, runpkg.DispatchRecord, runpkg.DispatchRecord) error { return tc.advanceErr },
			}
			if err := executor.execute(t.Context(), dispatchReplayStep{Intent: intent, Action: dispatch.ResumeProvision}); err == nil {
				t.Fatal("replay accepted advance failure")
			}
			if registry.InFlight() != tc.wantInFlight {
				t.Fatalf("in-flight = %d, want %d", registry.InFlight(), tc.wantInFlight)
			}
		})
	}
}

func TestReplayLocationReportsNonReservedQueueWithoutFormattingArtifact(t *testing.T) {
	projectDir := t.TempDir()
	intent := replayLocationIntent(t, projectDir)
	writeReplayReaderQueue(t, projectDir, intent, false)
	executor := dispatchReplayExecutor{projectDir: projectDir, localKind: runpkg.ExecutionLocalShared}
	err := executor.execute(t.Context(), dispatchReplayStep{Intent: intent, Action: dispatch.ResumeProvision})
	if err == nil || !strings.Contains(err.Error(), "requires exact reserved queue item") || strings.Contains(err.Error(), "%!w") {
		t.Fatalf("non-reserved error = %v", err)
	}
}

func TestReplayLocationForcesLocalWithoutWorkerOwnership(t *testing.T) {
	for _, tc := range []struct {
		name      string
		localOnly bool
		crossRepo bool
	}{
		{name: "queue local only", localOnly: true},
		{name: "cross repository", crossRepo: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			intent := replayLocationIntent(t, projectDir)
			if tc.crossRepo {
				intent = replayLocationIntent(t, "/srv/other/project")
			}
			writeReplayLocationFacts(t, projectDir, intent, tc.localOnly, "")
			registry := workers.NewRegistry(workers.Config{Workers: []workers.Worker{{
				Name: "worker-a", Transport: "ssh", Host: "worker.example", RepoPath: "/srv/worker/project",
				Enabled: true, MaxSlots: 1,
			}}})
			executor := dispatchReplayExecutor{
				projectDir: projectDir, workers: registry, localKind: runpkg.ExecutionLocalShared,
			}
			if err := executor.execute(t.Context(), dispatchReplayStep{Intent: intent, Action: dispatch.ResumeProvision}); err != nil {
				t.Fatal(err)
			}
			record := loadReplayLocationRecord(t, projectDir, intent)
			if record.Location == nil || record.Location.Kind != runpkg.ExecutionLocalShared ||
				record.Location.RepositoryPath != intent.Binding.RepositoryPath || registry.InFlight() != 0 {
				t.Fatalf("location = %+v, in-flight %d", record.Location, registry.InFlight())
			}
		})
	}
}

func TestReplayLocalExecutionKindMatchesSessionCapability(t *testing.T) {
	if got := replayLocalExecutionKind(nil); got != runpkg.ExecutionLocalShared {
		t.Fatalf("nil substrate kind = %q", got)
	}
	substrate := NewTmuxSubstrate(ltmux.OSAdapter{}, "daemon-session")
	if got := replayLocalExecutionKind(substrate); got != runpkg.ExecutionLocalIndependent {
		t.Fatalf("session-capable substrate kind = %q", got)
	}
	shared := NewTmuxSubstrate(&noDispatchTargetProbeAdapter{}, "daemon-session")
	if got := replayLocalExecutionKind(shared); got != runpkg.ExecutionLocalShared {
		t.Fatalf("tmux substrate without session creation kind = %q", got)
	}
}

func replayLocationIntent(t *testing.T, repositoryPath string) dispatch.Intent {
	t.Helper()
	binding := replayOwnershipIntent(t, dispatch.PhasePrepared).Binding
	binding.RepositoryPath = repositoryPath
	intent, err := dispatch.NewPrepared(binding)
	if err == nil {
		intent, err = intent.WithClaimDurable()
	}
	if err == nil {
		intent, err = intent.WithRunDurable()
	}
	if err != nil {
		t.Fatal(err)
	}
	return intent
}

func writeReplayLocationFacts(t *testing.T, projectDir string, intent dispatch.Intent, localOnly bool, workerTarget string) {
	t.Helper()
	writeReplayReaderQueue(t, projectDir, intent, true)
	snapshot, err := queue.Load(t.Context(), projectDir, intent.Binding.QueueName)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.LocalOnly = localOnly
	snapshot.WorkerTarget = workerTarget
	if err := queue.Persist(t.Context(), projectDir, snapshot); err != nil {
		t.Fatal(err)
	}
	record, err := runpkg.NewDispatchRecord(intent.Binding, time.Date(2026, 8, 15, 2, 3, 4, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := runpkg.CreateDispatchRecord(projectDir, record); err != nil {
		t.Fatal(err)
	}
}

func loadReplayLocationRecord(t *testing.T, projectDir string, intent dispatch.Intent) runpkg.DispatchRecord {
	t.Helper()
	records, err := runpkg.ScanRegistry(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	record, err := exactDispatchRecord(intent.Binding.RunID, records.Dispatch)
	if err != nil || record == nil {
		t.Fatalf("exact record = (%+v, %v)", record, err)
	}
	return *record
}
