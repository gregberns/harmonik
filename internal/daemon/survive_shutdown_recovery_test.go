package daemon

// survive_shutdown_recovery_test.go — what actually happens to a run that asked
// to outlive the daemon, and how its bead gets back.
//
// A bead run launched in a tmux session of its own writes a run registry record
// so a later daemon boot can find the session by name. Two things then have to
// hold, and these tests pin one each:
//
//   - The boot sweep must leave that session alone while an agent is working in
//     it. The session-level sweep kills every session carrying the project
//     prefix that is not excluded, and it applies no liveness test of its own,
//     so the boot builds the exclusion from the run registry BEFORE the kill
//     pass. It has to be before: the sweep runs ahead of the adoption pass, so a
//     session killed there is already gone by the time anything asks about it.
//   - When the session really is gone the adoption pass resets the bead and
//     drops the record, so the work is dispatched again.
//
// This file used to say the sweep killed the run session and that survival was
// fiction. It was, and the test that pinned it invited its own replacement once
// the sweep learned to tell a live run from an orphan. That is what happened.
//
// Helper prefix: surviveRecovery.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/runloop"
)

// surviveRecoveryHash is the project hash every session name in this file is
// built from.
const surviveRecoveryHash = core.ProjectHash("abcdef012345")

// surviveRecoveryRunID is a fixed run id so the derived session name is stable
// and a reader can see it is the same one in each test.
const surviveRecoveryRunID = "0f0e0d0c-0b0a-4908-8706-050403020100"

// surviveRecoveryRunSessionName returns the tmux session name a bead run in its
// own session carries, built by the production namer rather than by a literal,
// so a change to the naming rule reaches these tests.
func surviveRecoveryRunSessionName(t *testing.T) string {
	t.Helper()
	sub, ok := NewTmuxSubstrate(&surviveRecoveryAdapter{}, "survive-recovery-default",
		WithCrewProjectHash(surviveRecoveryHash)).(*tmuxSubstrate)
	if !ok {
		t.Fatal("surviveRecovery: NewTmuxSubstrate did not return *tmuxSubstrate")
	}
	name, err := sub.runSessionName(surviveRecoveryRunID)
	if err != nil {
		t.Fatalf("surviveRecovery: runSessionName: %v", err)
	}
	return name
}

// ─────────────────────────────────────────────────────────────────────────────
// The boot sweep
// ─────────────────────────────────────────────────────────────────────────────

// surviveRecoverySessions is a tmux server reduced to a list of session names
// and a record of which ones were killed.
type surviveRecoverySessions struct {
	mu     sync.Mutex
	names  []string
	killed []string
}

var (
	_ lifecycle.TmuxSessionLister = (*surviveRecoverySessions)(nil)
	_ lifecycle.TmuxSessionKiller = (*surviveRecoverySessions)(nil)
)

func (s *surviveRecoverySessions) ListTmuxSessions(context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.names))
	for _, n := range s.names {
		if !s.wasKilledLocked(n) {
			out = append(out, n)
		}
	}
	return out, nil
}

func (s *surviveRecoverySessions) KillTmuxSession(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.killed = append(s.killed, name)
	return nil
}

func (s *surviveRecoverySessions) wasKilledLocked(name string) bool {
	for _, k := range s.killed {
		if k == name {
			return true
		}
	}
	return false
}

func (s *surviveRecoverySessions) wasKilled(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.wasKilledLocked(name)
}

// surviveRecoveryPanePIDs is a tmux adapter that answers the two questions the
// boot sweep asks about a session: does it exist, and what PID is in its first
// pane. Everything else comes from the package's shared no-op adapter.
//
// A pane PID of 0 is how this fixture spells "nothing is running in there". The
// sweep reads a non-positive PID as dead without touching the process table, so
// the dead case is decided by the fixture rather than by whatever the host
// happens to be running.
type surviveRecoveryPanePIDs struct {
	noopTmuxAdapter
	live map[string]int
	mu   sync.Mutex
	// killed records the sessions killed through the ADAPTER path, which is the
	// second of the sweep's two kill passes.
	killed []string
}

var _ ltmux.Adapter = (*surviveRecoveryPanePIDs)(nil)

func (a *surviveRecoveryPanePIDs) ListSessions(context.Context) ([]string, error) {
	out := make([]string, 0, len(a.live))
	for name := range a.live {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func (a *surviveRecoveryPanePIDs) ListWindows(_ context.Context, _ string) ([]string, error) {
	// One window, not an idle shell, so the generic classifier has to fall
	// through to the pane-PID question rather than calling every session orphaned
	// on window names alone.
	return []string{ltmux.WindowAgent}, nil
}

func (a *surviveRecoveryPanePIDs) WindowPanePID(_ context.Context, handle ltmux.WindowHandle) (int, error) {
	name := strings.TrimSuffix(string(handle), ":")
	return a.live[name], nil
}

func (a *surviveRecoveryPanePIDs) KillSession(_ context.Context, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.killed = append(a.killed, name)
	return nil
}

func (a *surviveRecoveryPanePIDs) wasKilled(name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, k := range a.killed {
		if k == name {
			return true
		}
	}
	return false
}

// surviveRecoveryNoProcesses answers the sweep's process-level passes with an
// empty list, so the test never shells out to ps or pgrep and never depends on
// what else is running on the host.
type surviveRecoveryNoProcesses struct{}

var (
	_ lifecycle.HandlerProcessLister = surviveRecoveryNoProcesses{}
	_ lifecycle.ProcessLister        = surviveRecoveryNoProcesses{}
)

func (surviveRecoveryNoProcesses) ListOrphanHandlerPIDs(context.Context, core.ProjectHash) ([]int, error) {
	return nil, nil
}

func (surviveRecoveryNoProcesses) ListOrphanBrPIDs(context.Context) ([]int, error) {
	return nil, nil
}

// TestBootSweep_LeavesALiveRunSessionAloneAndStillReapsADeadOne is the promise
// the sweep now keeps, and it replaces the test that pinned the opposite.
//
// The session-level sweep kills every session carrying this project's hash that
// is not excluded, and it asks nothing about what is inside. That is right for
// the sessions it was written for and wrong for a bead run: a run in a session
// of its own is precisely a run built to outlive the daemon, and the boot that
// is supposed to adopt it used to kill it first. The adoption pass then found a
// dead session and reset a bead whose agent this same boot had just destroyed —
// so the recovery reported success over work it had thrown away.
//
// The boot now builds its exclusion from the run registry, before the kill pass.
//
// The dead run is not decoration. "The live session was left alone" is a claim
// that something did NOT happen, and it is free in a sweep that killed nothing.
// The dead run's session goes through the same call with the same registry and
// the same adapter, and it IS killed — so the live one surviving is the
// exclusion working rather than the sweep being inert.
func TestBootSweep_LeavesALiveRunSessionAloneAndStillReapsADeadOne(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()

	liveSession := surviveRecoveryRunSessionName(t)
	if err := runpkg.Write(projectDir, runpkg.Record{
		SchemaVersion: 1,
		RunID:         surviveRecoveryRunID,
		BeadID:        "hk-still-being-worked",
		SessionName:   liveSession,
	}); err != nil {
		t.Fatalf("surviveRecovery: write the live run's record: %v", err)
	}

	const deadRunID = "0f0e0d0c-0b0a-0908-0706-050403020199"
	deadSession := lifecycle.TmuxSessionName(surviveRecoveryHash, "run-deadbeef0000")
	if err := runpkg.Write(projectDir, runpkg.Record{
		SchemaVersion: 1,
		RunID:         deadRunID,
		BeadID:        "hk-agent-already-exited",
		SessionName:   deadSession,
	}); err != nil {
		t.Fatalf("surviveRecovery: write the dead run's record: %v", err)
	}

	adapter := &surviveRecoveryPanePIDs{live: map[string]int{
		// This process. The sweep asks the operating system whether the pane's PID
		// is alive, and this is the one PID a test can name that certainly is.
		liveSession: os.Getpid(),
		// Nothing is running in there any more.
		deadSession: 0,
	}}
	server := &surviveRecoverySessions{names: []string{liveSession, deadSession}}

	_, err := RunOrphanSweep(t.Context(), projectDir, surviveRecoveryHash, time.Now(),
		OrphanSweepConfig{
			TmuxAdapter:   adapter,
			TmuxLister:    server,
			TmuxKiller:    server,
			HandlerLister: surviveRecoveryNoProcesses{},
			BrLister:      surviveRecoveryNoProcesses{},
		})
	if err != nil {
		// The sweep reports its non-fatal step failures through this error and the
		// daemon proceeds anyway (PL-006). Report it and keep going, because the
		// kills below are what this test is about.
		t.Logf("RunOrphanSweep reported non-fatal step errors: %v", err)
	}

	if !server.wasKilled(deadSession) && !adapter.wasKilled(deadSession) {
		t.Fatalf("the sweep left %q standing, and nothing is running in it.\n"+
			"The sweep is inert in this fixture, so the check below would report a protected "+
			"live session when the truth is that nothing was swept at all.", deadSession)
	}

	if server.wasKilled(liveSession) || adapter.wasKilled(liveSession) {
		t.Errorf("the boot sweep killed %q, the tmux session of a run whose agent is still working.\n"+
			"The run wrote that session's name to the registry before it spawned, precisely so "+
			"this boot could find it. The sweep runs BEFORE the adoption pass, so the agent is "+
			"already dead by the time anything asks whether to adopt it — and the adoption pass "+
			"then resets the bead and reports a clean recovery over work this boot destroyed.",
			liveSession)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// The adoption pass — the recovery that is real
// ─────────────────────────────────────────────────────────────────────────────

// surviveRecoveryResetter records each bead the adoption pass reset.
type surviveRecoveryResetter struct {
	mu    sync.Mutex
	beads []core.BeadID
	err   error
}

var _ runBeadResetter = (*surviveRecoveryResetter)(nil)

func (r *surviveRecoveryResetter) ResetBead(_ context.Context, _ string, _ brcli.TimeoutConfig,
	beadID core.BeadID, _ core.ProjectHash, _ int64,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.beads = append(r.beads, beadID)
	return nil
}

func (r *surviveRecoveryResetter) resets() []core.BeadID {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]core.BeadID, len(r.beads))
	copy(out, r.beads)
	return out
}

// surviveRecoveryAdapter is a tmux adapter that knows about exactly the
// sessions it was given. The adoption pass asks it one question — which
// sessions are live — so everything else comes from the package's shared
// noopTmuxAdapter and a silent no-op is the honest answer for all of it.
type surviveRecoveryAdapter struct {
	noopTmuxAdapter
	live []string
}

var _ ltmux.Adapter = (*surviveRecoveryAdapter)(nil)

func (a *surviveRecoveryAdapter) ListSessions(context.Context) ([]string, error) {
	return a.live, nil
}

// surviveRecoveryProject writes one run registry record into a fresh project
// directory and returns the directory.
func surviveRecoveryProject(t *testing.T, rec runpkg.Record) string {
	t.Helper()
	dir := t.TempDir()
	if rec.StartedAt.IsZero() {
		rec.StartedAt = time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	}
	if err := runpkg.Write(dir, rec); err != nil {
		t.Fatalf("surviveRecovery: write run record: %v", err)
	}
	if _, err := runpkg.Load(dir, rec.RunID); err != nil {
		t.Fatalf("surviveRecovery: the record did not land, so the test observes nothing: %v", err)
	}
	return dir
}

// TestRunSessionAdoption_ASweptSessionIsAdoptedAsDeadAndItsBeadGoesBackOnTheQueue
// is the recovery that actually happens, and it is the reason the boot sweep's
// defeat went unnoticed for so long: the bead comes back either way.
//
// The record names a session the tmux server no longer has, which is exactly
// what the boot sweep leaves behind a few lines earlier. The pass resets the
// bead so the queue can revert the item to pending, and drops the record so the
// next boot does not do it again.
func TestRunSessionAdoption_ASweptSessionIsAdoptedAsDeadAndItsBeadGoesBackOnTheQueue(t *testing.T) {
	t.Parallel()

	runSession := surviveRecoveryRunSessionName(t)
	projectDir := surviveRecoveryProject(t, runpkg.Record{
		SchemaVersion: 1,
		RunID:         surviveRecoveryRunID,
		BeadID:        "hk-survive-recovery",
		SessionName:   runSession,
	})

	resetter := &surviveRecoveryResetter{}
	// The sweep already killed it, so the server no longer lists it.
	adapter := &surviveRecoveryAdapter{live: []string{
		lifecycle.TmuxSessionName(surviveRecoveryHash, "default"),
	}}

	adoptDeadRunSessions(t.Context(), projectDir, surviveRecoveryHash, 0, t.TempDir(),
		adapter, resetter)

	resets := resetter.resets()
	if len(resets) != 1 || resets[0] != core.BeadID("hk-survive-recovery") {
		t.Fatalf("bead resets = %v, want exactly [hk-survive-recovery].\n"+
			"The session the run left behind is gone, so nothing is working the bead. Without the reset it stays in progress for ever and is never dispatched again.", resets)
	}
	if _, err := runpkg.Load(projectDir, surviveRecoveryRunID); !errors.Is(err, runpkg.ErrNotFound) {
		t.Errorf("the run record is still present after adoption (Load err = %v, want ErrNotFound).\n"+
			"A record left behind makes every later boot re-adopt a run that is long gone.", err)
	}
}

// TestRunSessionAdoption_ALiveSessionKeepsItsBeadAndItsRecord is the control,
// and it is what stops the test above from passing for free.
//
// The pass has one decision to make. If it reset every record it found, the
// test above would be green in a fixture where the pass simply did the same
// thing to everything. Here the session IS live, so the pass must leave the
// bead alone — an agent is still working it — and must leave the record in
// place, because the work loop's own adoption is what watches a live session.
//
// A pass that reset this bead would take the work away from a running agent and
// hand the same bead to a second one.
func TestRunSessionAdoption_ALiveSessionKeepsItsBeadAndItsRecord(t *testing.T) {
	t.Parallel()

	runSession := surviveRecoveryRunSessionName(t)
	projectDir := surviveRecoveryProject(t, runpkg.Record{
		SchemaVersion: 1,
		RunID:         surviveRecoveryRunID,
		BeadID:        "hk-survive-recovery",
		SessionName:   runSession,
	})

	resetter := &surviveRecoveryResetter{}
	adapter := &surviveRecoveryAdapter{live: []string{
		lifecycle.TmuxSessionName(surviveRecoveryHash, "default"),
		runSession,
	}}

	adoptDeadRunSessions(t.Context(), projectDir, surviveRecoveryHash, 0, t.TempDir(),
		adapter, resetter)

	if resets := resetter.resets(); len(resets) != 0 {
		t.Errorf("bead resets = %v, want none.\n"+
			"The session is live, so an agent is still working this bead. Resetting it hands the same work to a second agent.", resets)
	}
	if _, err := runpkg.Load(projectDir, surviveRecoveryRunID); err != nil {
		t.Errorf("the run record is gone after adoption (Load err = %v).\n"+
			"The work loop's own adoption reads this record to watch the live session. Dropping it here loses the run.", err)
	}
}

func TestRunSessionAdoption_DoesNotResetUniversalDispatchRecords(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	runID := core.RunID(uuid.MustParse("0197d100-0000-7000-8000-000000000021"))
	binding := dispatch.Binding{
		QueueID:           "0197d100-0000-7000-8000-000000000022",
		QueueName:         "main",
		GroupIndex:        0,
		ItemIndex:         0,
		BeadID:            "hk-universal-run",
		RunID:             runID,
		ClaimTransitionID: core.TransitionID(uuid.MustParse("0197d100-0000-7000-8000-000000000023")),
		ParentCommit:      "0123456789abcdef0123456789abcdef01234567",
		RepositoryPath:    "/srv/harmonik/project",
	}
	record, err := runpkg.NewDispatchRecord(binding, time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := runpkg.CreateDispatchRecord(projectDir, record); err != nil {
		t.Fatal(err)
	}
	if !strandedBeadHasOnDiskRun(projectDir, binding.BeadID) {
		t.Fatal("stranded-bead guard ignored a universal dispatch record")
	}

	resetter := &surviveRecoveryResetter{}
	adoptDeadRunSessions(t.Context(), projectDir, surviveRecoveryHash, 0, t.TempDir(), nil, resetter)

	if resets := resetter.resets(); len(resets) != 0 {
		t.Fatalf("universal dispatch record caused bead resets: %v", resets)
	}
	snapshot, err := runpkg.ScanRegistry(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Dispatch) != 1 || snapshot.Dispatch[0].RunID != runID {
		t.Fatalf("dispatch records after legacy adoption = %+v", snapshot.Dispatch)
	}
}

func TestRunSessionAdoption_InvalidLegacyRunIdentityFailsClosed(t *testing.T) {
	t.Parallel()

	const invalidRunID = "00000000-0000-0000-0000-000000000000"
	projectDir := surviveRecoveryProject(t, runpkg.Record{
		SchemaVersion: 1,
		RunID:         invalidRunID,
		BeadID:        "hk-invalid-run",
		SessionName:   "harmonik-invalid-run",
	})
	resetter := &surviveRecoveryResetter{}
	adoptDeadRunSessions(t.Context(), projectDir, surviveRecoveryHash, 0, t.TempDir(), nil, resetter)

	if resets := resetter.resets(); len(resets) != 0 {
		t.Fatalf("invalid run identity caused bead resets: %v", resets)
	}
	if _, err := runpkg.Load(projectDir, invalidRunID); err != nil {
		t.Fatalf("invalid run identity was removed: %v", err)
	}
}

// TestRunSessionAdoption_ARecordWithNoSessionNameFailsClosed pins the strict
// registry boundary. A partial record cannot authorize a bead reset.
func TestRunSessionAdoption_ARecordWithNoSessionNameFailsClosed(t *testing.T) {
	t.Parallel()

	projectDir := surviveRecoveryProject(t, runpkg.Record{
		SchemaVersion: 1,
		RunID:         surviveRecoveryRunID,
		BeadID:        "hk-survive-recovery",
	})

	resetter := &surviveRecoveryResetter{}
	// Every session in the world is live. Only the missing NAME can make this
	// record dead.
	adapter := &surviveRecoveryAdapter{live: []string{
		surviveRecoveryRunSessionName(t),
		lifecycle.TmuxSessionName(surviveRecoveryHash, "default"),
	}}

	adoptDeadRunSessions(t.Context(), projectDir, surviveRecoveryHash, 0, t.TempDir(),
		adapter, resetter)

	if resets := resetter.resets(); len(resets) != 0 {
		t.Errorf("partial record caused bead resets: %v", resets)
	}
	if _, err := runpkg.Load(projectDir, surviveRecoveryRunID); err != nil {
		t.Errorf("partial record was removed: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// The record the whole recovery depends on
// ─────────────────────────────────────────────────────────────────────────────

// TestRunRegistry_ARecordIsReadableByNameAndCarriesTheSessionName states the
// storage contract the passes above consume: a record written under a run id is
// readable back by that id, and it carries the session name.
//
// It is a round trip through the store and NOTHING MORE. It says nothing about
// when the record is written, and in particular it does not show that the write
// happens before the spawn — the earlier wording claimed that and was wrong.
// TestRunRegistry_TheRecordIsOnDiskAndNamesTheSessionBeforeTheAgentIsLaunched,
// in run_registry_has_no_writer_test.go, is the test that observes the ordering,
// and it takes the observation from inside the spawn call.
//
// The session name is the only field either adoption pass uses to decide
// anything. A record written without it is adopted as dead no matter what is
// running.
func TestRunRegistry_ARecordIsReadableByNameAndCarriesTheSessionName(t *testing.T) {
	t.Parallel()

	runSession := surviveRecoveryRunSessionName(t)
	projectDir := t.TempDir()

	if err := runpkg.Write(projectDir, runpkg.Record{
		SchemaVersion: 1,
		RunID:         surviveRecoveryRunID,
		BeadID:        "hk-survive-recovery",
		SessionName:   runSession,
	}); err != nil {
		t.Fatalf("write run record: %v", err)
	}

	got, err := runpkg.Load(projectDir, surviveRecoveryRunID)
	if err != nil {
		t.Fatalf("load run record: %v", err)
	}
	if got.SessionName != runSession {
		t.Errorf("record SessionName = %q, want %q.\n"+
			"Both adoption passes ask tmux about this exact string. A record that does not carry it cannot be matched to a live session, so the run is adopted as dead however healthy it is.",
			got.SessionName, runSession)
	}

	// The record is on disk under the run id, which is how a later boot with no
	// memory of this run finds it at all.
	path := filepath.Join(projectDir, ".harmonik", "runs", surviveRecoveryRunID+".json")
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("no record at %s: %v — a later boot enumerates this directory and would find nothing", path, statErr)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// The live half: what finally settles a run that outlived the daemon
// ─────────────────────────────────────────────────────────────────────────────

// surviveRecoveryFadingAdapter lists the run's session until it is asked once,
// then stops. That is a session whose agent finishes shortly after the new
// daemon adopted it.
type surviveRecoveryFadingAdapter struct {
	noopTmuxAdapter
	mu    sync.Mutex
	name  string
	asked int
}

var _ ltmux.Adapter = (*surviveRecoveryFadingAdapter)(nil)

func (a *surviveRecoveryFadingAdapter) ListSessions(context.Context) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asked++
	if a.asked == 1 {
		return []string{a.name}, nil
	}
	return nil, nil
}

// TestRunSessionAdoption_TheLiveMonitorGivesTheBeadBackWhenTheAgentFinallyExits
// covers the pass that is now the ONLY thing that settles a surviving run.
//
// Every other route deliberately leaves such a run alone. The boot sweep exempts
// its tmux session, the dead-session pass skips it because the session is live,
// and the resume reconcile skips its bead because the registry says an agent has
// it. That is correct, and it means this monitor is the last one holding the
// bead. If it stopped working the bead would sit in progress for ever with every
// guard reporting success.
//
// The first poll finds the session and the monitor keeps waiting, so the reopen
// below is the monitor deciding the agent has gone rather than the monitor
// firing at anything it is handed.
func TestRunSessionAdoption_TheLiveMonitorGivesTheBeadBackWhenTheAgentFinallyExits(t *testing.T) {
	t.Parallel()

	runSession := surviveRecoveryRunSessionName(t)
	projectDir := surviveRecoveryProject(t, runpkg.Record{
		SchemaVersion: 1,
		RunID:         surviveRecoveryRunID,
		BeadID:        "hk-survive-recovery",
		SessionName:   runSession,
	})

	ledger := &surviveRunLedger{}
	adapter := &surviveRecoveryFadingAdapter{name: runSession}

	adoptLiveRunSession(t.Context(), ledger,
		runloop.RunEnv{ProjectDir: projectDir, IntentLogDir: t.TempDir()},
		nil, core.NewTransitionIDGenerator(),
		runpkg.Record{
			SchemaVersion: 1,
			RunID:         surviveRecoveryRunID,
			BeadID:        "hk-survive-recovery",
			SessionName:   runSession,
		}, adapter)

	reopens, _ := ledger.beadSettled()
	if len(reopens) != 1 || reopens[0] != "run_session_adopted_dead" {
		t.Fatalf("the monitor reopened the bead %d time(s) with reasons %v, want exactly one "+
			"\"run_session_adopted_dead\".\n"+
			"Nothing else gives this bead back. The boot sweep, the dead-session pass and the "+
			"resume reconcile all step over a bead whose run is live, on purpose.", len(reopens), reopens)
	}
	if _, err := runpkg.Load(projectDir, surviveRecoveryRunID); !errors.Is(err, runpkg.ErrNotFound) {
		t.Errorf("the run record is still on disk after the monitor settled the run "+
			"(Load err = %v, want ErrNotFound).\n"+
			"A record left behind keeps the next boot exempting a session that is gone, and the "+
			"bead is reopened again on every boot after this one.", err)
	}
}
