package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/dispatchstore"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/queue"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/workspace"
)

type replayReaderBeads struct {
	calls  int
	record core.BeadRecord
}

func (r *replayReaderBeads) ShowBead(context.Context, core.BeadID) (core.BeadRecord, error) {
	r.calls++
	return r.record, nil
}

func TestExactDispatchRecordReturnsDetachedMatch(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	record := replayFactRunRecord(t, intent)
	match, err := exactDispatchRecord(intent.Binding.RunID, []runpkg.DispatchRecord{record})
	if err != nil || match == nil {
		t.Fatalf("exactDispatchRecord() = (%+v, %v)", match, err)
	}
	match.BeadID = "changed"
	if record.BeadID == match.BeadID {
		t.Fatal("exact record aliases scan input")
	}
	if missing, missingErr := exactDispatchRecord(core.RunID{}, []runpkg.DispatchRecord{record}); missingErr != nil || missing != nil {
		t.Fatalf("missing record = (%+v, %v)", missing, missingErr)
	}
}

func TestExactDispatchRecordRejectsDuplicateAuthority(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhasePrepared)
	record, err := runpkg.NewDispatchRecord(intent.Binding, time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exactDispatchRecord(intent.Binding.RunID, []runpkg.DispatchRecord{record, record}); err == nil {
		t.Fatal("exactDispatchRecord() accepted duplicate authority")
	}
}

func TestMapDiscoveredWorktreesPreservesAbsentUnreadableAndExactLease(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhasePrepared)
	runID := intent.Binding.RunID.String()
	values := []workspace.DiscoveredWorktree{
		{RunID: "absent", WorktreePath: "/tmp/absent", RegisteredInGit: true},
		{RunID: "unreadable", WorktreePath: "/tmp/unreadable", RegisteredInGit: true, LeaseLockUnreadable: true},
	}
	got := mapDiscoveredWorktrees(values)
	if len(got) != 2 || got[0].LeasePresent || got[0].LeaseReadable || !got[1].LeasePresent || got[1].LeaseReadable {
		t.Fatalf("mapped worktrees = %+v", got)
	}
	if fact := dispatch.ClassifyWorktreeObservations(intent, append(got, dispatch.WorktreeObservation{
		RunID: runID, Path: "/tmp/" + runID, Registered: true, CanonicalPath: true,
		GitBranch:       "run/" + runID,
		HasSessions:     true,
		HasExactSidecar: true,
		LeasePresent:    true, LeaseReadable: true, LeaseRunID: runID,
		LeasePID: 42, LeaseCreatedAt: "2026-08-13T01:02:03Z", LeaseTTLSec: 60,
	})); fact != dispatch.WorktreeLeased {
		t.Fatalf("worktree fact = %q", fact)
	}
}

func TestMapDiscoveredWorktreesClassifiesExactPreparedAuthority(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseRunDurable)
	runID := intent.Binding.RunID.String()
	got := mapDiscoveredWorktrees([]workspace.DiscoveredWorktree{{
		RunID: runID, WorktreePath: "/tmp/" + runID, RegisteredInGit: true,
		GitBranch: "run/" + runID, HeadCommit: intent.Binding.ParentCommit,
	}})
	if fact := dispatch.ClassifyWorktreeObservations(intent, got); fact != dispatch.WorktreePrepared {
		t.Fatalf("prepared fact = %q from %+v", fact, got)
	}
	conflict := []workspace.DiscoveredWorktree{{
		RunID: runID, WorktreePath: "/tmp/" + runID, RegisteredInGit: true,
		GitBranch: "run/" + runID, HeadCommit: intent.Binding.ParentCommit,
		SessionsPathConflict: true,
	}}
	if fact := dispatch.ClassifyWorktreeObservations(intent, mapDiscoveredWorktrees(conflict)); fact != dispatch.WorktreeConflict {
		t.Fatalf("conflicting fact = %q", fact)
	}
}

func TestReadDispatchTargetObservationRequiresExactHandoffProbe(t *testing.T) {
	prepared := replayOwnershipIntent(t, dispatch.PhasePrepared)
	if got := readDispatchTargetObservation(t.Context(), nil, prepared, nil); got.Status != dispatch.SessionTargetSessionAbsent {
		t.Fatalf("prepared target = %+v", got)
	}
	handoff := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	if got := readDispatchTargetObservation(t.Context(), nil, handoff, nil); got.Status != dispatch.SessionTargetOptionsUnreadable {
		t.Fatalf("unprobeable handoff target = %+v", got)
	}
}

func TestReadDispatchTargetObservationUsesLocationOwner(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	record := replayFactRunRecord(t, intent)
	local := &dispatchTargetProbeAdapter{probe: replayReaderTargetProbe(intent)}
	remote := &dispatchTargetProbeAdapter{probe: replayReaderTargetProbe(intent)}
	for _, tc := range []struct {
		name     string
		location runpkg.ExecutionLocation
		want     *dispatchTargetProbeAdapter
	}{
		{name: "local", location: runpkg.ExecutionLocation{Kind: runpkg.ExecutionLocalIndependent}, want: local},
		{name: "remote", location: runpkg.ExecutionLocation{Kind: runpkg.ExecutionRemote, WorkerName: "worker-a"}, want: remote},
	} {
		t.Run(tc.name, func(t *testing.T) {
			local.calls, remote.calls = 0, 0
			recordCopy := record
			recordCopy.Location = &tc.location
			resolver := func(location runpkg.ExecutionLocation) (ltmux.Adapter, error) {
				if location.Kind == runpkg.ExecutionRemote {
					return remote, nil
				}
				return local, nil
			}
			got := readDispatchTargetObservation(t.Context(), resolver, intent, &recordCopy)
			if fact := dispatch.ClassifySessionTarget(intent, got); fact != dispatch.SessionLive {
				t.Fatalf("target fact = %q from %+v", fact, got)
			}
			if tc.want.calls != 1 || local.calls+remote.calls != 1 {
				t.Fatalf("probe calls local=%d remote=%d", local.calls, remote.calls)
			}
		})
	}
}

func TestFilesystemDispatchReplayReaderRejectsInvalidIntentBeforeIO(t *testing.T) {
	beads := &replayReaderBeads{}
	reader := filesystemDispatchReplayReader{projectDir: t.TempDir(), beads: beads}
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	intent.Handoff = nil
	if _, err := reader.ReadDispatchReplayFacts(t.Context(), intent); err == nil {
		t.Fatal("ReadDispatchReplayFacts() accepted invalid intent")
	}
	if beads.calls != 0 {
		t.Fatalf("Beads calls = %d, want zero before validation", beads.calls)
	}
}

func TestFilesystemDispatchReplayReaderReadsPreparedFacts(t *testing.T) {
	projectDir := t.TempDir()
	intent := replayOwnershipIntent(t, dispatch.PhasePrepared)
	writeReplayReaderIntent(t, projectDir, intent)
	writeReplayReaderQueue(t, projectDir, intent, false)
	beads := &replayReaderBeads{record: replayFactBead(intent.Binding.BeadID, core.CoarseStatusOpen)}
	reader := filesystemDispatchReplayReader{projectDir: projectDir, beads: beads}
	facts, err := reader.ReadDispatchReplayFacts(t.Context(), intent)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Queue != dispatch.QueueOfferable || facts.Bead != dispatch.BeadOpen || facts.RunRecord != dispatch.RunRecordAbsent {
		t.Fatalf("prepared facts = %+v", facts)
	}
}

func TestFilesystemDispatchReplayReaderUsesScannedRemoteLocationForTarget(t *testing.T) {
	projectDir := t.TempDir()
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	writeReplayReaderIntent(t, projectDir, intent)
	writeReplayReaderQueue(t, projectDir, intent, true)
	record := replayFactRunRecord(t, intent)
	remoteLocation := runpkg.ExecutionLocation{Kind: runpkg.ExecutionRemote, WorkerName: "worker-a"}
	record.Location = &remoteLocation
	writeReplayReaderRecord(t, projectDir, intent, record)
	receipt, err := dispatch.NewSessionStartReceipt(intent)
	if err != nil {
		t.Fatal(err)
	}
	if err := dispatchstore.New(projectDir).InstallSessionStartReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	beads := &replayReaderBeads{record: replayFactBead(intent.Binding.BeadID, core.CoarseStatusInProgress)}
	remote := &dispatchTargetProbeAdapter{probe: replayReaderTargetProbe(intent)}
	resolverCalls := 0
	reader := filesystemDispatchReplayReader{
		projectDir: projectDir, beads: beads,
		resolve: func(location runpkg.ExecutionLocation) (ltmux.Adapter, error) {
			resolverCalls++
			if location != remoteLocation {
				t.Fatalf("resolved location = %+v", location)
			}
			return remote, nil
		},
	}
	facts, err := reader.ReadDispatchReplayFacts(t.Context(), intent)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Session != dispatch.SessionLive || facts.SessionReceipt != dispatch.SessionReceiptExact || resolverCalls != 1 || remote.calls != 1 {
		t.Fatalf("handoff facts = %+v; resolver=%d probe=%d", facts, resolverCalls, remote.calls)
	}
}

func writeReplayReaderIntent(t *testing.T, projectDir string, intent dispatch.Intent) {
	t.Helper()
	prepared := replayOwnershipIntent(t, dispatch.PhasePrepared)
	if err := dispatchstore.New(projectDir).Create(prepared); err != nil {
		t.Fatal(err)
	}
	if intent.Phase != dispatch.PhasePrepared {
		if err := advanceReplayOwnershipIntent(t, projectDir, intent); err != nil {
			t.Fatal(err)
		}
	}
}

func writeReplayReaderQueue(t *testing.T, projectDir string, intent dispatch.Intent, dispatched bool) {
	t.Helper()
	item := queue.Item{BeadID: intent.Binding.BeadID, Status: queue.ItemStatusPending}
	if dispatched {
		runID := intent.Binding.RunID.String()
		item.Status, item.RunID = queue.ItemStatusDispatched, &runID
	}
	snapshot := replayFactQueue(intent, item)
	if err := queue.Persist(t.Context(), projectDir, &snapshot); err != nil {
		t.Fatal(err)
	}
}

func writeReplayReaderRecord(t *testing.T, projectDir string, intent dispatch.Intent, want runpkg.DispatchRecord) {
	t.Helper()
	base, err := runpkg.NewDispatchRecord(intent.Binding, want.StartedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := runpkg.CreateDispatchRecord(projectDir, base); err != nil {
		t.Fatal(err)
	}
	located, err := base.BindLocation(*want.Location)
	if err != nil {
		t.Fatal(err)
	}
	if err := runpkg.AdvanceDispatchRecord(projectDir, base, located); err != nil {
		t.Fatal(err)
	}
	bound, err := located.BindSession(want.SessionName, want.WindowName)
	if err != nil {
		t.Fatal(err)
	}
	if err := runpkg.AdvanceDispatchRecord(projectDir, located, bound); err != nil {
		t.Fatal(err)
	}
}

func replayReaderTargetProbe(intent dispatch.Intent) ltmux.TargetProbe {
	return ltmux.TargetProbe{
		Status: ltmux.TargetProbeExact,
		RunID:  intent.Binding.RunID.String(), ClaimTransitionID: intent.Binding.ClaimTransitionID.String(),
		SessionName: intent.Handoff.SessionName, WindowName: intent.Handoff.WindowName,
		PanePID: "42", PaneDead: "0",
	}
}
