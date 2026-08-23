package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/lifecycle"
	runpkg "github.com/gregberns/harmonik/internal/run"
)

var (
	badBootRegistryLiveRun    = core.RunID(uuid.MustParse("0f0e0d0c-0b0a-4908-8706-05040302ee01"))
	badBootRegistryCrashedRun = core.RunID(uuid.MustParse("0f0e0d0c-0b0a-4908-8706-05040302ee02"))
)

const (
	badBootRegistryLiveBead    = core.BeadID("hk-agent-is-still-working-it")
	badBootRegistryCrashedBead = core.BeadID("hk-daemon-died-under-it")
)

type badBootRegistryOutcome struct {
	ledger     *noWriterLedger
	failedRuns map[core.RunID]struct{}
}

func (o badBootRegistryOutcome) beadReset(beadID core.BeadID) bool {
	return o.ledger.wasReset(beadID)
}

func (o badBootRegistryOutcome) markedFailed(runID core.RunID) bool {
	_, failed := o.failedRuns[runID]
	return failed
}

func badBootRegistryReconcile(t *testing.T, withTornRecord bool) badBootRegistryOutcome {
	t.Helper()

	projectDir := t.TempDir()
	if writeErr := runpkg.Write(projectDir, runpkg.Record{
		SchemaVersion: 1,
		RunID:         badBootRegistryLiveRun.String(),
		BeadID:        string(badBootRegistryLiveBead),
		SessionName:   lifecycle.TmuxSessionName(surviveRecoveryHash, "run-0f0e0d0cee01"),
		StartedAt:     time.Date(2026, 8, 11, 13, 0, 0, 0, time.UTC),
	}); writeErr != nil {
		t.Fatalf("badBootRegistry: write the live run's record: %v", writeErr)
	}

	if withTornRecord {
		tornPath := filepath.Join(projectDir, ".harmonik", "runs", badRegistryTornRecord)
		if writeErr := os.WriteFile(tornPath, []byte(`{"schema_version":`), 0o600); writeErr != nil {
			t.Fatalf("badBootRegistry: write the torn record: %v", writeErr)
		}
		if _, scanErr := runpkg.ScanRegistry(projectDir); scanErr == nil {
			t.Fatalf("badBootRegistry: the registry at %s still reads cleanly with %s in it.\n"+
				"This test is about what the reconcile does when the scan FAILS, and here it did not.",
				projectDir, badRegistryTornRecord)
		}
	}

	eventsPath := filepath.Join(t.TempDir(), "events.jsonl")
	writer, openErr := eventbus.OpenJSONLWriter(eventsPath)
	if openErr != nil {
		t.Fatalf("badBootRegistry: open the event log: %v", openErr)
	}
	t.Cleanup(func() { _ = writer.Close() })
	bus := eventbus.NewBusImplWithWriter(core.NewRedactionRegistry(), writer)

	for runID, beadID := range map[core.RunID]core.BeadID{
		badBootRegistryLiveRun:    badBootRegistryLiveBead,
		badBootRegistryCrashedRun: badBootRegistryCrashedBead,
	} {
		emitRunStarted(t.Context(), bus, runID, beadID, "/tmp/workspace", nil, nil,
			standardBeadDescriptor, core.WorkflowModeDot, core.ReviewPolicyReviewed,
			core.WorkflowSelectionEmbeddedDefault, nil, nil)
	}

	ledger := &noWriterLedger{}
	bs := &bootState{
		cfg: Config{ProjectDir: projectDir, JSONLLogPath: eventsPath},
		bus: bus,
	}
	st := &reconcileState{
		beadResetter:       ledger,
		orphanStatusReader: ledger,
		// The durable queue records both beads as dispatched, which is all it
		// knows. Telling the two apart is what the registry is for.
		queueDispatched: lifecycle.QueueDispatchedSet{
			badBootRegistryLiveBead:    struct{}{},
			badBootRegistryCrashedBead: struct{}{},
		},
	}

	bs.reconcileInFlightRuns(t.Context(), time.Now(), st)

	return badBootRegistryOutcome{
		ledger:     ledger,
		failedRuns: badBootRegistryFailedRuns(t, eventsPath),
	}
}

func badBootRegistryFailedRuns(t *testing.T, eventsPath string) map[core.RunID]struct{} {
	t.Helper()
	raw, readErr := os.ReadFile(eventsPath) //nolint:gosec // G304: the path comes from t.TempDir
	if readErr != nil {
		t.Fatalf("badBootRegistry: read the event log: %v", readErr)
	}
	failed := map[core.RunID]struct{}{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var envelope struct {
			Type  string  `json:"type"`
			RunID *string `json:"run_id"`
		}
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			t.Fatalf("badBootRegistry: decode the event envelope %q: %v", line, err)
		}
		if envelope.Type != string(core.EventTypeRunFailed) || envelope.RunID == nil {
			continue
		}
		runUUID, parseErr := uuid.Parse(*envelope.RunID)
		if parseErr != nil {
			t.Fatalf("badBootRegistry: the run_failed names %q, which is not a run id: %v",
				*envelope.RunID, parseErr)
		}
		failed[core.RunID(runUUID)] = struct{}{}
	}
	return failed
}

// TestBootReconcile_AnUnreadableRunRegistryWritesOffNothing is the guard.
//
// One unparseable file under .harmonik/runs/ fails the whole scan, and the
// reconcile used to read that failure as "no run is live". It is not: it is the
// boot not knowing. Acting on it reported a working agent's run failed and put
// its bead back on the queue, and the next dispatch sent a second agent to the
// same bead and the same branch.
//
// So the reconcile stands down for the boot. What that costs is a queue item
// left dispatched on a run that really did crash, and any later boot that reads
// the registry clears it. What it buys is that no agent is written off while it
// is still typing.
func TestBootReconcile_AnUnreadableRunRegistryWritesOffNothing(t *testing.T) {
	t.Parallel()

	out := badBootRegistryReconcile(t, true)

	if out.beadReset(badBootRegistryLiveBead) {
		t.Errorf("the boot reconcile reset %s, whose agent is still working it in a session that "+
			"outlived the daemon.\n"+
			"The scan of .harmonik/runs/ failed, so the live-run set was empty — which reads exactly "+
			"like a restart no run survived. The bead goes back on the queue and a second agent is "+
			"dispatched onto work already in hand.", badBootRegistryLiveBead)
	}
	if out.markedFailed(badBootRegistryLiveRun) {
		t.Errorf("the boot reconcile emitted run_failed against %s, a run that has not ended.\n"+
			"That is its own damage even when the bead survives: the queue advances past a run that "+
			"is still going, and the terminal event its agent eventually produces lands after the "+
			"run was written off.", badBootRegistryLiveRun)
	}

	if out.beadReset(badBootRegistryCrashedBead) || out.markedFailed(badBootRegistryCrashedRun) {
		t.Errorf("the boot reconcile acted on the crashed run (bead reset: %v, marked failed: %v) "+
			"while it could not read the registry.\n"+
			"Nothing in this state distinguishes that run from the live one, so a reconcile that "+
			"acts on one is acting on both.",
			out.beadReset(badBootRegistryCrashedBead), out.markedFailed(badBootRegistryCrashedRun))
	}
}

// TestBootReconcile_AReadableRunRegistryStillRecoversTheCrashedRun is the
// control, and without it the test above passes for a reconcile that stopped
// working entirely.
//
// Same fixture, same two runs, same event log — only the unparseable file is
// missing. The crashed run MUST be marked failed and its bead MUST be reset, so
// that "the live run survived" over there is the stand-down doing its job rather
// than a reconcile that recovers nothing at all.
func TestBootReconcile_AReadableRunRegistryStillRecoversTheCrashedRun(t *testing.T) {
	t.Parallel()

	out := badBootRegistryReconcile(t, false)

	if !out.beadReset(badBootRegistryCrashedBead) {
		t.Errorf("the reconcile did not reset %s, the bead it exists to recover.\n"+
			"The reset is inert in this fixture, so the unreadable-registry test proves nothing: "+
			"a reconcile that resets no bead protects the live one for free.",
			badBootRegistryCrashedBead)
	}
	if !out.markedFailed(badBootRegistryCrashedRun) {
		t.Errorf("the reconcile emitted no run_failed against %s, a run the daemon died under.\n"+
			"The emit is inert in this fixture, so the unreadable-registry test proves nothing "+
			"about the event it claims not to write.", badBootRegistryCrashedRun)
	}

	if out.beadReset(badBootRegistryLiveBead) {
		t.Errorf("the reconcile reset %s, and the registry it needed was readable.",
			badBootRegistryLiveBead)
	}
	if out.markedFailed(badBootRegistryLiveRun) {
		t.Errorf("the reconcile emitted run_failed against %s, and the registry it needed was "+
			"readable.", badBootRegistryLiveRun)
	}
}
