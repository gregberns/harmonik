package daemon

// survive_shutdown_recovery_test.go — what actually happens to a run that asked
// to outlive the daemon, and how its bead gets back.
//
// A bead run launched in a tmux session of its own writes a run registry record
// so a later daemon boot can find the session by name. The intent is that the
// agent keeps working across a daemon restart and the next boot adopts it. The
// system does not deliver that, and these tests say so plainly rather than
// asserting the intent:
//
//   - The boot orphan sweep kills every tmux session carrying the project
//     prefix that is not in its exclusion set. It applies no liveness test, run
//     sessions are not excluded, and it runs before the pass that looks for a
//     surviving run. So the session is already dead by the time anything asks.
//   - The bead still gets back, because the adoption pass then classifies the
//     run as dead, resets the bead and drops the record. That recovery is real
//     and worth defending. Survival is not.
//
// Tests here are therefore named after what the code does. There is no test
// called "the session survives a daemon restart", and there must not be: it
// would assert a promise the system does not keep, and would either fail or
// pass for the wrong reason.
//
// The defeat itself is recorded in
// plans/2026-07-27-delete-and-rewrite/STEP-6-RESOURCE-LEASES.md §4 and in
// specs/run-state-machine.md §4a under RSM-037. Fixing it is separate work.
//
// Helper prefix: surviveRecovery.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	runpkg "github.com/gregberns/harmonik/internal/run"
)

// surviveRecoveryHash is the project hash every session name in this file is
// built from.
const surviveRecoveryHash = core.ProjectHash("abcdef012345")

// surviveRecoveryRunID is a fixed run id so the derived session name is stable
// and a reader can see it is the same one in each test.
const surviveRecoveryRunID = "0f0e0d0c-0b0a-0908-0706-050403020100"

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

// TestBootSweep_KillsTheRunSessionTheAdoptionPassIsAboutToLookFor pins the
// defeat, as behaviour, at the site that causes it.
//
// The sweep is presented with the four session kinds a real boot sees: the
// daemon's own spawn target, the flywheel, the captain and a live crew — all of
// which the daemon excludes — plus one bead-run session in the shape a run that
// asked to outlive the daemon leaves behind. The run session is killed.
//
// This is why the survive path buys nothing today. The disposition in
// specs/run-state-machine.md §4a is what a run ASKS for; this test is what it
// GETS. Anyone who later teaches the sweep to tell a live surviving run from a
// genuine orphan should expect this test to fail, and should replace it with
// the new promise rather than deleting it quietly.
func TestBootSweep_KillsTheRunSessionTheAdoptionPassIsAboutToLookFor(t *testing.T) {
	t.Parallel()

	runSession := surviveRecoveryRunSessionName(t)
	daemonSession := lifecycle.TmuxSessionName(surviveRecoveryHash, "default")
	flywheel := lifecycle.TmuxSessionName(surviveRecoveryHash, "flywheel")
	captain := lifecycle.TmuxSessionName(surviveRecoveryHash, "captain")
	crew := lifecycle.TmuxSessionName(surviveRecoveryHash, "crew-paul")
	// A crew whose registry record is gone: a genuine orphan, and the thing the
	// sweep exists to reap. It keeps the sweep demonstrably live in this
	// fixture, so the run-session assertion below stands on its own rather than
	// on "the sweep did something".
	staleCrew := lifecycle.TmuxSessionName(surviveRecoveryHash, "crew-departed")
	unrelated := "someone-elses-tmux-session"

	server := &surviveRecoverySessions{
		names: []string{daemonSession, flywheel, captain, crew, staleCrew, runSession, unrelated},
	}

	// The exclusion set the daemon builds at boot: its own spawn target, the
	// coordinator, the captain and every live crew. A bead-run session is not
	// in it, and nothing adds it.
	excluded := map[string]struct{}{
		daemonSession: {},
		flywheel:      {},
		captain:       {},
		crew:          {},
	}

	killed, err := lifecycle.SweepOrphanTmuxSessions(t.Context(), surviveRecoveryHash,
		server, server, nil, excluded)
	if err != nil {
		t.Fatalf("SweepOrphanTmuxSessions: %v", err)
	}
	if !server.wasKilled(staleCrew) {
		t.Fatalf("the sweep left the orphan crew session %q standing, so it is not doing its job in this fixture and nothing below is meaningful (killed=%d)", staleCrew, killed)
	}

	for _, name := range []string{daemonSession, flywheel, captain, crew, unrelated} {
		if server.wasKilled(name) {
			t.Errorf("the sweep killed %q, which is excluded or not this project's", name)
		}
	}
	if !server.wasKilled(runSession) {
		t.Errorf("the sweep left the bead-run session %q standing.\n"+
			"CURRENT BEHAVIOUR IS THAT IT KILLS IT. The sweep matches on the project prefix alone, applies no liveness test, and holds no exclusion for a run session — which is why a run that asks to outlive the daemon does not. If this now fails because the sweep learned to tell a live surviving run from an orphan, that is the fix: replace this test with the promise the sweep now keeps.", runSession)
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

// TestRunSessionAdoption_ARecordWithNoSessionNameIsTreatedAsDead pins the
// fallback. A registry write that could not resolve a session name leaves the
// field empty, and there is then no way to ask whether anything is alive.
//
// The pass treats that as dead and resets the bead. That is the safe direction:
// a bead reset while its agent still runs is re-dispatched, while a bead left
// in progress behind a name nobody can check is stuck for ever.
func TestRunSessionAdoption_ARecordWithNoSessionNameIsTreatedAsDead(t *testing.T) {
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

	if resets := resetter.resets(); len(resets) != 1 {
		t.Errorf("bead resets = %v, want exactly one.\n"+
			"A record with no session name cannot be checked for liveness. Leaving its bead in progress strands it behind a question nobody can answer.", resets)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// The record the whole recovery depends on
// ─────────────────────────────────────────────────────────────────────────────

// TestRunRegistry_ARecordIsReadableByNameAndCarriesTheSessionName states the
// contract the two passes above consume, and that the bounded session
// constructors now lean on: a late-arriving run session is discoverable because
// its name is written down before the spawn.
//
// The session name is the only field either adoption pass uses to decide
// anything. A record written without it is adopted as dead no matter what is
// running, which is the case pinned above.
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
