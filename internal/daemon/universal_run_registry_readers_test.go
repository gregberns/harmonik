package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/lifecycle"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/workspace"
)

type replayOwnershipBeadLedger struct {
	beads  []core.BeadRecord
	resets []core.BeadID
}

func (l *replayOwnershipBeadLedger) ListInFlightBeads(context.Context) ([]core.BeadRecord, error) {
	return l.beads, nil
}

func (l *replayOwnershipBeadLedger) ResetBead(
	_ context.Context,
	_ string,
	_ brcli.TimeoutConfig,
	beadID core.BeadID,
	_ core.ProjectHash,
	_ int64,
) error {
	l.resets = append(l.resets, beadID)
	return nil
}

func writeUniversalRunRecord(t *testing.T, projectDir string, beadID core.BeadID, sessionName string) runpkg.DispatchRecord {
	t.Helper()
	binding := dispatch.Binding{
		QueueID:           "0197d100-0000-7000-8000-000000000032",
		QueueName:         "main",
		GroupIndex:        0,
		ItemIndex:         0,
		BeadID:            beadID,
		RunID:             core.RunID(uuid.MustParse("0197d100-0000-7000-8000-000000000031")),
		ClaimTransitionID: core.TransitionID(uuid.MustParse("0197d100-0000-7000-8000-000000000033")),
	}
	record, err := runpkg.NewDispatchRecord(binding, time.Date(2026, 8, 11, 13, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := runpkg.CreateDispatchRecord(projectDir, record); err != nil {
		t.Fatal(err)
	}
	if sessionName == "" {
		return record
	}
	located, err := record.BindLocation(runpkg.ExecutionLocation{Kind: runpkg.ExecutionLocalIndependent})
	if err != nil {
		t.Fatal(err)
	}
	if err := runpkg.AdvanceDispatchRecord(projectDir, record, located); err != nil {
		t.Fatal(err)
	}
	bound, err := located.BindSession(sessionName)
	if err != nil {
		t.Fatal(err)
	}
	if err := runpkg.AdvanceDispatchRecord(projectDir, located, bound); err != nil {
		t.Fatal(err)
	}
	return bound
}

func TestLegacyStartupAdoptionExcludesUniversalRecords(t *testing.T) {
	projectDir := t.TempDir()
	writeUniversalRunRecord(t, projectDir, "hk-universal", "harmonik-universal-session")
	if err := runpkg.Write(projectDir, runpkg.Record{
		SchemaVersion: 1,
		RunID:         "0f0e0d0c-0b0a-4908-8706-050403020188",
		BeadID:        "hk-legacy",
		SessionName:   "harmonik-legacy-session",
		StartedAt:     time.Date(2026, 8, 11, 13, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	records, err := legacyRunSessionsForAdoption(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].BeadID != "hk-legacy" {
		t.Fatalf("legacy startup adoption records = %+v", records)
	}
}

func TestProbeRunProcessDeadReadsUniversalSessionBinding(t *testing.T) {
	projectDir := t.TempDir()
	record := writeUniversalRunRecord(t, projectDir, "hk-universal", "harmonik-universal-session")
	bs := &bootState{cfg: Config{ProjectDir: projectDir}}
	if !bs.probeRunProcessDead(t.Context(), &noopTmuxAdapter{}, record.RunID) {
		t.Fatal("probeRunProcessDead ignored the universal session binding")
	}
}

func TestBootReconcileProtectsUniversalRunBead(t *testing.T) {
	projectDir := t.TempDir()
	const liveBead = core.BeadID("hk-universal-live")
	const crashedBead = core.BeadID("hk-universal-crashed-control")
	writeUniversalRunRecord(t, projectDir, liveBead, "")

	ledger := &noWriterLedger{}
	eventsPath := filepath.Join(t.TempDir(), "events.jsonl")
	writer, err := eventbus.OpenJSONLWriter(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	bs := &bootState{
		cfg: Config{ProjectDir: projectDir, JSONLLogPath: eventsPath},
		bus: eventbus.NewBusImplWithWriter(core.NewRedactionRegistry(), writer),
	}
	st := &reconcileState{
		beadResetter:       ledger,
		orphanStatusReader: ledger,
		queueDispatched: lifecycle.QueueDispatchedSet{
			liveBead:    {},
			crashedBead: {},
		},
	}
	bs.reconcileInFlightRuns(t.Context(), time.Now(), st)
	if ledger.wasReset(liveBead) {
		t.Fatal("reconcile reset the bead owned by a universal run record")
	}
	if !ledger.wasReset(crashedBead) {
		t.Fatal("reconcile control bead was not reset")
	}
}

func TestRunOrphanSweepProtectsUniversalRunSession(t *testing.T) {
	projectDir := surviveRunRepo(t)
	liveSession := lifecycle.TmuxSessionName(surviveRecoveryHash, "run-universal1")
	record := writeUniversalRunRecord(t, projectDir, "hk-universal-live", liveSession)
	deadSession := lifecycle.TmuxSessionName(surviveRecoveryHash, "run-deadbeef00")

	headSHA, err := resolveHEAD(t.Context(), projectDir)
	if err != nil {
		t.Fatal(err)
	}
	liveWorktree, liveCleanup, err := productionWorktreeFactory(t.Context(), projectDir, record.RunID.String(), headSHA)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(liveCleanup)
	const abandonedRunID = "0197d100-0000-7000-8000-000000000039"
	const replayOwnedRunID = "0197d100-0000-7000-8000-000000000040"
	const runOwnedRunID = "0197d100-0000-7000-8000-000000000041"
	abandonedWorktree, abandonedCleanup, err := productionWorktreeFactory(t.Context(), projectDir, abandonedRunID, headSHA)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(abandonedCleanup)
	replayOwnedWorktree, replayOwnedCleanup, err := productionWorktreeFactory(t.Context(), projectDir, replayOwnedRunID, headSHA)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(replayOwnedCleanup)
	runOwnedWorktree, runOwnedCleanup, err := productionWorktreeFactory(t.Context(), projectDir, runOwnedRunID, headSHA)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runOwnedCleanup)
	for _, fixture := range []struct {
		path  string
		runID core.RunID
	}{
		{path: liveWorktree, runID: record.RunID},
		{path: abandonedWorktree, runID: core.RunID(uuid.MustParse(abandonedRunID))},
		{path: replayOwnedWorktree, runID: core.RunID(uuid.MustParse(replayOwnedRunID))},
		{path: runOwnedWorktree, runID: core.RunID(uuid.MustParse(runOwnedRunID))},
	} {
		if err := workspace.ReleaseLeaseLock(workspace.LeaseLockPath(fixture.path)); err != nil {
			t.Fatal(err)
		}
		if err := workspace.WriteLeaseLockAtomic(workspace.LeaseLockPath(fixture.path), &core.LeaseLockFile{
			RunID: fixture.runID, PID: 99999999, CreatedAt: time.Now().UTC(), TTLSec: 3600,
		}); err != nil {
			t.Fatal(err)
		}
	}
	replayOwnedSession := lifecycle.TmuxSessionName(surviveRecoveryHash, "run-replayowned")
	adapter := &surviveRecoveryPanePIDs{live: map[string]int{
		liveSession: os.Getpid(), replayOwnedSession: os.Getpid(), deadSession: 0,
	}}
	server := &surviveRecoverySessions{names: []string{liveSession, replayOwnedSession, deadSession}}

	_, err = RunOrphanSweep(t.Context(), projectDir, surviveRecoveryHash, time.Now(), OrphanSweepConfig{
		TmuxAdapter: adapter, TmuxLister: server, TmuxKiller: server,
		HandlerLister: surviveRecoveryNoProcesses{}, BrLister: surviveRecoveryNoProcesses{},
		DispatchOwnership: DispatchReplayOwnership{
			Sessions:  map[string]struct{}{replayOwnedSession: {}},
			Worktrees: map[core.RunID]struct{}{core.RunID(uuid.MustParse(replayOwnedRunID)): {}},
			Runs:      map[core.RunID]struct{}{core.RunID(uuid.MustParse(runOwnedRunID)): {}},
		},
	})
	if err != nil {
		t.Logf("RunOrphanSweep() reported non-fatal errors: %v", err)
	}
	if server.wasKilled(liveSession) || adapter.wasKilled(liveSession) {
		t.Fatal("orphan sweep killed a session bound by a universal run record")
	}
	if server.wasKilled(replayOwnedSession) || adapter.wasKilled(replayOwnedSession) {
		t.Fatal("orphan sweep killed a session owned by dispatch replay")
	}
	if !server.wasKilled(deadSession) && !adapter.wasKilled(deadSession) {
		t.Fatal("orphan sweep control session was not reaped")
	}
	if _, err := os.Stat(liveWorktree); err != nil {
		t.Fatalf("orphan sweep removed the universal run worktree: %v", err)
	}
	if _, err := os.Stat(replayOwnedWorktree); err != nil {
		t.Fatalf("orphan sweep removed the dispatch-replay worktree: %v", err)
	}
	if _, err := os.Stat(runOwnedWorktree); err != nil {
		t.Fatalf("orphan sweep removed the dispatch-replay run worktree: %v", err)
	}
	if _, err := os.Stat(abandonedWorktree); !os.IsNotExist(err) {
		t.Fatalf("orphan sweep did not remove the abandoned control worktree: %v", err)
	}
}

func TestRunOrphanSweepProtectsReplayOwnedBeadWithoutMutatingQueueMaps(t *testing.T) {
	const replayBead = core.BeadID("hk-replay-owned")
	const resetControl = core.BeadID("hk-reset-control")
	ledger := &replayOwnershipBeadLedger{beads: []core.BeadRecord{
		{BeadID: replayBead, Status: core.CoarseStatusInProgress},
		{BeadID: resetControl, Status: core.CoarseStatusInProgress},
	}}
	dispatched := lifecycle.QueueDispatchedSet{}
	// Both beads have independent queue provenance. Dispatch replay adds only
	// replayBead to the dispatched exclusion set.
	owned := lifecycle.QueueOwnedSet{resetControl: {}, replayBead: {}}
	_, err := RunOrphanSweep(t.Context(), t.TempDir(), surviveRecoveryHash, time.Now(), OrphanSweepConfig{
		HandlerLister:   surviveRecoveryNoProcesses{},
		BrLister:        surviveRecoveryNoProcesses{},
		BeadLedger:      ledger,
		BeadResetter:    ledger,
		IntentLogDir:    t.TempDir(),
		DaemonStartNS:   time.Now().UnixNano(),
		QueueDispatched: dispatched,
		QueueOwned:      owned,
		DispatchOwnership: DispatchReplayOwnership{
			Beads: map[core.BeadID]struct{}{replayBead: {}},
		},
	})
	if err != nil {
		t.Logf("RunOrphanSweep() reported non-fatal errors: %v", err)
	}
	if len(ledger.resets) != 1 || ledger.resets[0] != resetControl {
		t.Fatalf("bead resets = %v, want [%s]", ledger.resets, resetControl)
	}
	if len(dispatched) != 0 || len(owned) != 2 {
		t.Fatalf("caller queue maps were mutated: dispatched=%v owned=%v", dispatched, owned)
	}
}

func TestQueueOwnershipWithDispatchAddsReplayBeadsWithoutMutation(t *testing.T) {
	const queueBead = core.BeadID("hk-queue-owned")
	const replayBead = core.BeadID("hk-replay-owned")
	cfg := OrphanSweepConfig{
		QueueDispatched: lifecycle.QueueDispatchedSet{queueBead: {}},
		QueueOwned:      lifecycle.QueueOwnedSet{queueBead: {}},
		DispatchOwnership: DispatchReplayOwnership{
			Beads: map[core.BeadID]struct{}{replayBead: {}},
		},
	}
	dispatched, owned := queueOwnershipWithDispatch(cfg)
	for _, set := range []map[core.BeadID]struct{}{dispatched, owned} {
		if _, ok := set[queueBead]; !ok {
			t.Fatal("queue-owned bead was lost")
		}
		if _, ok := set[replayBead]; !ok {
			t.Fatal("dispatch-replay bead was not protected")
		}
	}
	if _, changed := cfg.QueueOwned[replayBead]; changed {
		t.Fatal("queue ownership input was mutated")
	}
}

func TestStartupReconcilePreparesQueueNamespaceBeforeOtherRecovery(t *testing.T) {
	projectDir := t.TempDir()
	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.MkdirAll(queuesDir, 0o750); err != nil {
		t.Fatal(err)
	}
	receiptRoot := filepath.Join(queuesDir, ".completion-receipts")
	if err := os.WriteFile(receiptRoot, []byte("wrong type"), 0o600); err != nil {
		t.Fatal(err)
	}

	bs := &bootState{cfg: Config{ProjectDir: projectDir}}
	err := bs.runStartupReconcile(t.Context(), time.Now(), "main")
	if err == nil {
		t.Fatal("runStartupReconcile() accepted an invalid queue namespace")
	}
	if !strings.Contains(err.Error(), "prepare queue namespace before dispatch replay") {
		t.Fatalf("runStartupReconcile() error = %v", err)
	}
}
