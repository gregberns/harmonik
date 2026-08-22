package daemon

// run_registry_has_no_writer_test.go — the run registry is read by four
// production files and written by none, and these tests are what that costs.
//
// .harmonik/runs/<run_id>.json is the only durable statement that a bead is
// being worked on right now. internal/run.Write puts one there. Nothing in
// production calls it: the call lived in the imperative single-workflow tail of
// beadRunOne and went out with that tail. Every run is a graph run now, and the
// graph launch in dot_cascade_core.go passes no ConfigurePerRunSubstrate hook,
// so nothing sets the independent-session flag and nothing writes a record.
//
// The directory is therefore always empty, so every reader takes its empty-set
// branch on every boot for ever. Three tests here pin a DIFFERENT thing that
// branch decides. They are RED on purpose and they stay red until a run again
// records itself before it spawns its agent.
//
// Each drives the REAL run through the fixture in
// survive_shutdown_run_resources_test.go rather than writing a record by hand.
// A hand-written record makes every one of them pass today, which is exactly
// why the defect survived: the package already has tests that seed the registry
// themselves, and they are all green.
//
// # Why the fourth test is here
//
// The three consequence tests are the acceptance criteria for the repair, and
// on their own they accept a repair that does not work. Each one only needs a
// record carrying the right BeadID, written at any time, with every other field
// left zero. That was measured, not reasoned: a probe that wrote
// Record{SchemaVersion, RunID, BeadID} from the post-launch callback turned all
// three green, and the property test in internal/run with them.
//
// Such a record is useless. adoptDeadRunSessions matches a live session by
// SessionName and treats an empty one as dead, so it would reset the bead of an
// agent that is still working. probeRunProcessDead gives up on an empty
// SessionName. And a record written after the spawn is not written at all for a
// daemon killed in between, which is the exact crash the registry exists for.
//
// So TheRecordIsOnDiskAndNamesTheSessionBeforeTheAgentIsLaunched pins the shape
// of the record rather than a consequence of its absence. It is what stops the
// other three from being satisfied by a repair that writes something.
//
// Helper prefix: noWriter.

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/lifecycle"
	runpkg "github.com/gregberns/harmonik/internal/run"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixture
// ─────────────────────────────────────────────────────────────────────────────

// noWriterLedger records every bead reset and answers every status query with
// in_progress, which is the status a bead has while an agent works it. The
// reconcile skips any bead that is not open or in_progress, so a ledger that
// answered with the zero value would make every "the bead was left alone"
// assertion below pass for the wrong reason.
type noWriterLedger struct {
	mu     sync.Mutex
	resets []core.BeadID
}

var (
	_ runBeadResetter  = (*noWriterLedger)(nil)
	_ beadStatusReader = (*noWriterLedger)(nil)
)

func (l *noWriterLedger) ResetBead(_ context.Context, _ string, _ brcli.TimeoutConfig,
	beadID core.BeadID, _ core.ProjectHash, _ int64,
) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.resets = append(l.resets, beadID)
	return nil
}

func (l *noWriterLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusInProgress}, nil
}

// wasReset reports whether beadID was reset in_progress → open.
func (l *noWriterLedger) wasReset(beadID core.BeadID) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, b := range l.resets {
		if b == beadID {
			return true
		}
	}
	return false
}

// noWriterInFlightRun drives one bead through the real run on the ordering the
// registry exists for: the substrate CAN give the agent a tmux session of its
// own, and the daemon then stops while the agent is still coming up. That is
// the survive case — the agent keeps working in a session this daemon no longer
// owns, and a record on disk is the only thing that will tell the next boot so.
//
// It asks for a session of its own and does not get one today. Nothing sets the
// independent-session flag, so the spawn takes the shared-window path. That is
// the same deletion these tests are about, which is why the guard below accepts
// a window OR a session: it is there to prove an agent was launched, not to
// pre-judge which kind of launch it was.
//
// It returns the project directory the run used. It fails the test if the run
// never reached a spawn, because "the registry is empty" is free in a fixture
// where no run ever started.
func noWriterInFlightRun(t *testing.T) string {
	t.Helper()
	out := surviveRunDriveWith(t, surviveRunOpts{
		ownSession:        true,
		realWorktree:      true,
		stopAtPanePIDCall: 1,
	})
	if out.adapter.windows() == 0 && len(out.adapter.sessions()) == 0 {
		t.Fatal("the run never asked tmux for anything, so no agent was ever launched.\n" +
			"Every claim below is about what a LIVE run leaves on disk. A fixture whose run " +
			"stopped before the spawn would satisfy them all by doing nothing.")
	}
	return out.projectDir
}

// noWriterSeedRecord writes one registry record by hand. This is the control
// every test below needs: it is what the reader does when the registry is NOT
// empty, so a reader that ignores the seeded record is broken in its own right
// rather than starved of input.
func noWriterSeedRecord(t *testing.T, projectDir string, beadID core.BeadID, sessionName string) {
	t.Helper()
	runID := uuid.NewString()
	if err := runpkg.Write(projectDir, runpkg.Record{
		SchemaVersion: 1,
		RunID:         runID,
		BeadID:        string(beadID),
		SessionName:   sessionName,
		StartedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("noWriter: seed a control record: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Consequence 1 — the protection that runs backwards
// ─────────────────────────────────────────────────────────────────────────────

// TestBootReconcile_TheDispatchTrackerSweepLeavesABeadWithALiveRunAlone is the
// dangerous one.
//
// reconcileInFlightRuns builds liveRunBeadIDs from the registry and hands it to
// the resume reconcile as the set of beads that must be left alone: an agent is
// on them, in a session that outlived the daemon. Everything else the queue
// still records as dispatched is an orphan of the crash, and its bead is reset
// so the work can be dispatched again.
//
// With no producer the set is empty on every boot, so nothing is ever excluded
// and a bead with a live agent on it is reset exactly like a crashed one. The
// queue then re-dispatches work that is already being done — two agents, one
// bead, one branch.
//
// The crashed bead is not decoration. "The live bead was left alone" is a claim
// of the form "X did not happen", which is free in a run where the reconcile
// never fired. The crashed bead proves the pass ran, reached this ledger and
// reset what it was supposed to reset, in the same call.
//
// # What this test does NOT say, and why the name is narrow
//
// reconcileOrphanedRunsOnResume has three passes and only two of them read
// liveRunBeadIDs: the dispatch-tracker sweep, which this test drives, and the
// terminated-but-locked sweep. The PRIMARY loop — every run with run_started
// and no terminal event — resets unconditionally and never consults the set at
// all. A surviving run emits run_started and, by construction, no terminal
// event, so that loop resets a live bead whatever the registry holds.
//
// The event log this test opens is empty, which is what keeps the primary loop
// out of the way so the dispatch-tracker sweep can be observed on its own. That
// is a deliberate narrowing, not the production shape.
//
// So this test going green does NOT mean a bead with a live agent survives the
// resume reconcile. It means the one guard that exists has an input again. The
// primary loop ignoring liveRunBeadIDs is a SECOND defect, it is not fixed by
// restoring the writer, and a test for it would stay red after this one turns
// green.
func TestBootReconcile_TheDispatchTrackerSweepLeavesABeadWithALiveRunAlone(t *testing.T) {
	t.Parallel()

	projectDir := noWriterInFlightRun(t)

	// The bead the daemon was killed on before its run ever announced itself.
	// The queue records it dispatched, nothing is working it, and it must come
	// back. It is the reconcile's whole job.
	const crashedBead = core.BeadID("hk-crashed-before-run-started")

	ledger := &noWriterLedger{}
	eventsPath := filepath.Join(t.TempDir(), "events.jsonl")
	writer, err := eventbus.OpenJSONLWriter(eventsPath)
	if err != nil {
		t.Fatalf("noWriter: open the event log: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	bs := &bootState{
		cfg: Config{ProjectDir: projectDir, JSONLLogPath: eventsPath},
		bus: eventbus.NewBusImplWithWriter(core.NewRedactionRegistry(), writer),
	}
	st := &reconcileState{
		beadResetter:       ledger,
		orphanStatusReader: ledger,
		// Both beads are dispatched as far as the durable queue knows. That is
		// the whole input the reconcile has, and telling the two apart is what
		// the registry is for.
		queueDispatched: lifecycle.QueueDispatchedSet{
			surviveRunProbeBead: struct{}{},
			crashedBead:         struct{}{},
		},
	}

	bs.reconcileInFlightRuns(t.Context(), time.Now(), st)

	if !ledger.wasReset(crashedBead) {
		t.Fatalf("the reconcile did not reset %s, the bead it exists to recover.\n"+
			"Nothing below means anything until this pass is shown to act. A reconcile that "+
			"resets nothing leaves every bead alone, including the live one, and would pass "+
			"the check underneath while protecting nothing.", crashedBead)
	}

	if ledger.wasReset(surviveRunProbeBead) {
		t.Errorf("the dispatch-tracker sweep reset %s, whose agent is still working it in a "+
			"session that outlived the daemon.\n"+
			"reconcileInFlightRuns must exclude it, and it builds that exclusion from the run "+
			"registry — which has no producer, so the set is empty and nothing is ever excluded. "+
			"The bead goes back on the queue, a second agent is dispatched to work it, and two "+
			"agents now share one bead and one branch.", surviveRunProbeBead)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Consequence 2 — nothing is adopted across a restart
// ─────────────────────────────────────────────────────────────────────────────

// TestRunSessionAdoption_ARunLaunchedBeforeARestartIsAdoptedAfterIt pins the
// recovery the registry was built for.
//
// A run launches into its own tmux session. The daemon is killed. On the next
// boot adoptDeadRunSessions lists the registry, asks tmux which of those
// sessions still exist, and for each one that is gone resets the bead so the
// queue can re-dispatch it. Nothing else on disk remembers that run: its
// in-memory handle died with the daemon.
//
// With no producer there is nothing to list, so nothing is adopted and the bead
// stays in_progress for ever. The work is neither being done nor re-dispatched.
//
// The seeded record is the control. It goes through the same call, on the same
// ledger, with the same adapter, and it IS adopted — so a real run left alone
// below is the registry being empty and not the pass being inert.
func TestRunSessionAdoption_ARunLaunchedBeforeARestartIsAdoptedAfterIt(t *testing.T) {
	t.Parallel()

	projectDir := noWriterInFlightRun(t)

	const seededBead = core.BeadID("hk-seeded-control")
	noWriterSeedRecord(t, projectDir, seededBead, "harmonik-run-seeded-control")

	ledger := &noWriterLedger{}
	// A tmux server with no sessions at all: this is the next boot, and every
	// session the previous daemon left is gone.
	adapter := &surviveRecoveryAdapter{}

	adoptDeadRunSessions(
		t.Context(),
		projectDir,
		core.ProjectHash("abcdef012345"),
		time.Now().UnixNano(),
		t.TempDir(),
		adapter,
		ledger,
	)

	if !ledger.wasReset(seededBead) {
		t.Fatalf("the adoption pass did not reset %s, whose record it was handed directly.\n"+
			"The pass cannot reach this ledger at all, so the check below would report an empty "+
			"registry when the real fault is elsewhere.", seededBead)
	}

	if !ledger.wasReset(surviveRunProbeBead) {
		t.Errorf("the run that was in flight when the daemon stopped was not adopted: %s was "+
			"left in_progress.\n"+
			"Adoption reads .harmonik/runs/, and the run wrote nothing there, so the next boot "+
			"has no record that the run ever existed. The bead is now claimed by an agent that "+
			"is gone and by a daemon that never heard of it: it is never re-dispatched and never "+
			"finished.", surviveRunProbeBead)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Consequence 3 — a reader that cannot tell its two answers apart
// ─────────────────────────────────────────────────────────────────────────────

// TestStrandedBeadGuard_TellsARunningBeadApartFromOneWithNoRun pins the reader
// whose empty-set branch is indistinguishable from its real answer.
//
// strandedBeadHasOnDiskRun asks one question: is anything working this bead
// right now? "No" lets the stranded-bead auto-reset take the bead back. The
// guard exists because "yes" means an adoptLiveRunSession goroutine is watching
// a live session, and resetting then races a running agent.
//
// The function is not broken. It is starved. It answers "no" for a bead with a
// live agent on it and "no" for a bead nobody has ever touched, and those are
// the only two answers it has. A caller cannot tell them apart, and neither can
// a test that only checks the second one — which is why this test checks all
// three cases in one place. The seeded bead proves the guard still reports
// "yes" when the registry is not empty. The unrelated bead proves it is not
// wired to "yes". The real run is the case it gets wrong.
func TestStrandedBeadGuard_TellsARunningBeadApartFromOneWithNoRun(t *testing.T) {
	t.Parallel()

	projectDir := noWriterInFlightRun(t)

	const seededBead = core.BeadID("hk-seeded-control")
	const untouchedBead = core.BeadID("hk-never-dispatched")
	noWriterSeedRecord(t, projectDir, seededBead, "harmonik-run-seeded-control")

	if !strandedBeadHasOnDiskRun(projectDir, seededBead) {
		t.Fatalf("the guard reports no run for %s, whose record is on disk.\n"+
			"It is not reading the registry at all, so the two checks below would be measuring "+
			"something other than what they name.", seededBead)
	}
	if strandedBeadHasOnDiskRun(projectDir, untouchedBead) {
		t.Fatalf("the guard reports a run for %s, which has never been dispatched.\n"+
			"A guard that answers yes for everything would pass the check below while "+
			"distinguishing nothing.", untouchedBead)
	}

	if !strandedBeadHasOnDiskRun(projectDir, surviveRunProbeBead) {
		t.Errorf("the guard reports no run for %s, which has a live agent on it.\n"+
			"That is the same answer it gives for a bead nobody has ever dispatched, because "+
			"the run recorded nothing. The stranded-bead auto-reset therefore fires on a bead "+
			"whose agent is mid-flight, which is the one case the guard was added to prevent.",
			surviveRunProbeBead)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// The shape of the record — what the three tests above do not constrain
// ─────────────────────────────────────────────────────────────────────────────

// TestRunRegistry_TheRecordIsOnDiskAndNamesTheSessionBeforeTheAgentIsLaunched
// is the precondition for all three claims above, and it is the one that stops
// them being satisfied by a repair that writes a record with nothing in it.
//
// A run that means to outlive the daemon is discoverable only by name. The next
// boot lists the registry, reads SessionName and asks tmux whether that session
// is still there. Two facts have to hold for that to work, and neither is
// implied by "a record exists":
//
//   - The record is on disk BEFORE the agent's session is created. A daemon
//     killed in between leaves a live tmux session that nothing on disk names —
//     untracked, unadoptable, and swept as an orphan with no record of what it
//     was. That crash is the whole reason the registry is durable.
//   - The record names the session that was actually created. On an empty
//     SessionName adoptDeadRunSessions treats the run as dead however healthy
//     the agent is, and resets a bead out from under a working agent.
//     probeRunProcessDead gives up on the same empty string. On a WRONG
//     SessionName the next boot asks tmux about a session that does not exist
//     and reaps a live run.
//
// The observation is taken from inside the spawn call itself, through the
// fixture's onSpawn hook, so the ordering is a fact about the run rather than a
// reading of the source.
func TestRunRegistry_TheRecordIsOnDiskAndNamesTheSessionBeforeTheAgentIsLaunched(t *testing.T) {
	t.Parallel()

	// The daemon stays up. This test is only about what is true at the instant
	// the agent is launched, and a shutdown would add a second reason for a
	// record to be absent afterwards.
	out := surviveRunDriveWith(t, surviveRunOpts{ownSession: true, realWorktree: true})

	if out.adapter.windows() == 0 && len(out.adapter.sessions()) == 0 {
		t.Fatal("the run never asked tmux for anything, so no agent was launched and there is " +
			"no spawn for the record to come before.")
	}

	// runpkg.ScanRegistry fails closed, so one error covers "nothing was written"
	// and "something unusable was written". The raw decode separates them, and
	// the two need different fixes.
	if out.recordAtSpawnErr != nil && out.rawRecordAtSpawnErr != nil {
		t.Errorf("the run registry held no record when the agent was launched "+
			"(reader: %v; file: %v)\n"+
			"A daemon killed between the launch and the write leaves a live session that nothing "+
			"on disk names. The next boot cannot adopt it and cannot even say what it was.",
			out.recordAtSpawnErr, out.rawRecordAtSpawnErr)
	} else if out.recordAtSpawnErr != nil {
		t.Errorf("a record was on disk when the agent was launched, and production cannot read "+
			"it: %v\n"+
			"The bytes are %+v.\n"+
			"Adoption reads this registry with the same reader, and the reader fails closed: one "+
			"record it refuses makes the WHOLE registry unreadable, so this record strands every "+
			"other live run as well as its own.",
			out.recordAtSpawnErr, out.rawRecordAtSpawn)
	}

	if len(out.adapter.sessions()) == 0 {
		t.Error("the run got no tmux session of its own, so there is no session for the record " +
			"to name.\n" +
			"The substrate offered one: this fixture's adapter creates sessions. Nothing asked, " +
			"because nothing sets the per-run independent-session flag any more. A run inside the " +
			"daemon's own session dies with the daemon, and no record can save it.")
	}

	// Everything below reads the bytes on disk, so it means nothing if there were
	// none. It reads the RAW record on purpose: production's reader refuses a
	// record whose session name is empty or whose run id does not match its own
	// file name, so the field checks below would be unreachable through it and
	// every one of these defects would report as the single error above.
	if out.rawRecordAtSpawnErr != nil {
		return
	}

	if out.rawRecordAtSpawn.SessionName == "" {
		t.Error("the record was written with no session name.\n" +
			"Both adoption passes match on that string. A record without it is adopted as dead " +
			"however healthy the agent is, so the bead is reset and re-dispatched under a working " +
			"agent — the opposite of what the record is for.")
	} else if len(out.adapter.sessions()) > 0 {
		if got, want := out.rawRecordAtSpawn.SessionName, out.adapter.sessions()[0]; got != want {
			t.Errorf("the record names session %q but the session created is %q.\n"+
				"The two must be the same string, or the next boot asks tmux about a session that "+
				"does not exist and reaps a live run.", got, want)
		}
	}

	if out.rawRecordAtSpawn.RunID != out.runID {
		t.Errorf("record RunID = %q, want %q.\n"+
			"The registry reader takes the run id from the file name and refuses a record whose "+
			"body disagrees with it, so this record is not adopted and nothing says why.",
			out.rawRecordAtSpawn.RunID, out.runID)
	}
	if out.rawRecordAtSpawn.BeadID != string(surviveRunProbeBead) {
		t.Errorf("record BeadID = %q, want %q.\n"+
			"Without it the adoption pass has a session to check and no bead to reset.",
			out.rawRecordAtSpawn.BeadID, surviveRunProbeBead)
	}
	if out.rawRecordAtSpawn.StartedAt.IsZero() {
		t.Error("the record was written with no start time.\n" +
			"The registry reader refuses a record without one, so the whole registry becomes " +
			"unreadable and every live run on this project is stranded, not just this one.")
	}
}
