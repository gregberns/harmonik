package daemon

// run_session_collision_test.go — a graph run launches an agent per node into
// ONE session, so the second node can arrive before the first node's session is
// gone.
//
// The run's session is named after the run, so every node of that run asks tmux
// for the same name. Each node kills its agent when it ends, and killing the
// only window in a session destroys the session — but tmux does that on its own
// schedule, and the next node does not wait for it. `tmux new-session` on a name
// that still exists answers "duplicate session".
//
// Refusing there would fail the node, and the run with it, over a teardown that
// was already under way. The session that exists belongs to this run and nothing
// else can hold that name, and the previous node was already told to die, so
// finishing that teardown and asking again takes nothing that was not going
// anyway.
//
// Helper prefix: sessionCollide.

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/runlease"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/substrate"
)

// sessionCollideRunID is the run every launch below belongs to. Both nodes of a
// graph run carry the same one, which is why they collide.
//
// The version nibble is load-bearing. runpkg.ScanRegistry is the only reader
// production has for the run registry, and it refuses a record whose basename
// carries no UUID version. A run id with a zero version makes the record this
// test writes unreadable by the daemon, so the test would prove nothing.
const sessionCollideRunID = "0f0e0d0c-0b0a-4908-8706-050403020102"

// sessionCollideAdapter is a tmux server that reports the run's session already
// exists until something kills it, which is what tmux does while the previous
// node's session is still being torn down.
//
// failWith replaces that behaviour with a different error, so the same fixture
// can also drive a failure that is NOT a collision.
type sessionCollideAdapter struct {
	w4cFixtureAdapter
	mu       sync.Mutex
	exists   bool
	failWith error
	killed   []string
}

func (a *sessionCollideAdapter) NewSessionIn(ctx context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.mu.Lock()
	if a.failWith != nil {
		a.mu.Unlock()
		return tmux.Outcome{Err: a.failWith}
	}
	if a.exists {
		a.mu.Unlock()
		return tmux.Outcome{Err: tmux.ErrWindowCollision}
	}
	a.exists = true
	a.mu.Unlock()
	return a.w4cFixtureAdapter.NewSessionIn(ctx, params)
}

func (a *sessionCollideAdapter) KillSession(_ context.Context, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.killed = append(a.killed, name)
	a.exists = false
	return nil
}

func (a *sessionCollideAdapter) killedSessions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.killed))
	copy(out, a.killed)
	return out
}

// TestSpawnRunSession_ASecondNodeGetsTheSessionAfterTheFirstOnesTeardown is the
// hazard a run session per RUN creates for a graph of several nodes.
//
// The first launch makes the session. The second finds it still there and must
// still put its agent outside the daemon's own session. A failure here is not a
// lost window — it fails the node, which fails the run, on graphs of more than
// one agentic node, which is all of them.
func TestSpawnRunSession_ASecondNodeGetsTheSessionAfterTheFirstOnesTeardown(t *testing.T) {
	t.Parallel()

	adapter := &sessionCollideAdapter{}
	sub := w4cFixtureSubstrate(t, adapter)
	wantSession, nameErr := sub.runSessionName(sessionCollideRunID)
	if nameErr != nil {
		t.Fatalf("sessionCollide: name the run's session: %v", nameErr)
	}

	first, err := sub.SpawnRunSession(context.Background(), sessionCollideRunID,
		handler.SubstrateSpawn{Argv: []string{"/usr/local/bin/claude"}})
	if err != nil {
		t.Fatalf("the first node could not take the run's session: %v", err)
	}
	if first == nil {
		t.Fatal("the first node got no session back, so the collision below is not the " +
			"second node meeting the first")
	}

	second, err := sub.SpawnRunSession(context.Background(), sessionCollideRunID,
		handler.SubstrateSpawn{Argv: []string{"/usr/local/bin/claude"}})
	if err != nil {
		t.Fatalf("the second node of the same run was refused because the run's session "+
			"already exists: %v\n"+
			"Every node of a graph run asks for the same session name, and the previous node's "+
			"teardown is not synchronous. Refusing here fails the node, and the run with it, on "+
			"any graph with more than one agentic node.", err)
	}
	if second == nil {
		t.Fatal("the second node got neither a session nor an error, so nothing was launched")
	}

	if killed := adapter.killedSessions(); len(killed) != 1 || killed[0] != wantSession {
		t.Errorf("the recovery killed %v, want exactly [%s].\n"+
			"Finishing the previous node's teardown is what makes room for the retry. Killing "+
			"anything else reaches a session this run does not own.", killed, wantSession)
	}

	if windows := adapter.newWindowCopy(); len(windows) != 0 {
		t.Errorf("the recovery opened %d ordinary tmux window(s): %v.\n"+
			"An ordinary window is recorded for the daemon's exit-time window sweep, so the "+
			"agent would be killed with the daemon — which is the one thing a session of the "+
			"run's own exists to prevent. The retry must go back through new-session.",
			len(windows), windows)
	}
}

// TestSpawnRunSession_ATmuxFailureThatIsNotACollisionStillFails is the negative
// control, and it was added because its absence was measured: widening the
// recovery to accept ANY new-session error passed the whole package.
//
// The recovery is safe only because a duplicate-session error names a session
// this run already owns. Any other failure — a tmux server that is gone, a
// refused command — says nothing about who owns what, and killing a session on
// that reasoning reaches for something the run cannot account for.
func TestSpawnRunSession_ATmuxFailureThatIsNotACollisionStillFails(t *testing.T) {
	t.Parallel()

	adapter := &sessionCollideAdapter{failWith: errors.New("tmux: server not running")}
	sub := w4cFixtureSubstrate(t, adapter)

	sess, err := sub.SpawnRunSession(context.Background(), sessionCollideRunID,
		handler.SubstrateSpawn{Argv: []string{"/usr/local/bin/claude"}})
	if err == nil {
		t.Fatalf("SpawnRunSession reported success (session %v) on a tmux failure that is not a "+
			"duplicate session.\n"+
			"The launch would then believe it has an agent that was never started.", sess)
	}
	if killed := adapter.killedSessions(); len(killed) != 0 {
		t.Errorf("the failure path killed %v.\n"+
			"Only a duplicate-session error identifies a session this run owns. Killing on any "+
			"other error reaches for a session on reasoning that does not hold.", killed)
	}
}

// newWindowCopy returns the new-window requests the adapter recorded.
func (a *w4cFixtureAdapter) newWindowCopy() []tmux.NewWindowIn {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]tmux.NewWindowIn, len(a.newWindowParams))
	copy(out, a.newWindowParams)
	return out
}

// sessionCollideProjectHash keeps this file honest about which project hash the
// fixture substrate is built with: the session name is derived from it, and a
// mismatch would make the assertion above compare two names neither of which is
// production's.
var _ = core.ProjectHash("abcdef012345")

// ─────────────────────────────────────────────────────────────────────────────
// One fact, read at both ends
// ─────────────────────────────────────────────────────────────────────────────

// TestSetUpRunSession_ARunWithItsOwnCommandRunnerRecordsNothing pins the
// agreement between the two halves of the decision.
//
// The run writes the record; the LAUNCH creates the session the record names.
// They are hundreds of lines apart and each has its own reason to ask "is this
// run local". The launch asks whether it has a command runner. So the run has to
// ask the same question and not an equivalent-looking one, or it writes a record
// naming a session no launch will ever create — a record pointing at nothing,
// which the adoption pass reads as a dead run and acts on.
//
// The pair below is the whole claim: the same call, the same substrate, the same
// project, differing only in whether a runner exists.
func TestSetUpRunSession_ARunWithItsOwnCommandRunnerRecordsNothing(t *testing.T) {
	t.Parallel()

	runID := core.RunID(uuid.MustParse(sessionCollideRunID))

	withRunner := sessionCollideSetUp(t, runID, true)
	if withRunner.took {
		t.Error("a run whose agents execute through a command runner took a tmux session on " +
			"this host.\n" +
			"Its agents run somewhere this tmux server does not reach, so the session would be " +
			"empty and the record would name it anyway.")
	}
	if withRunner.recorded {
		t.Error("a run whose agents execute through a command runner wrote a registry record.\n" +
			"The launch takes the run's session only when it has NO runner, so this record names " +
			"a session that is never created. The next boot finds the session missing, reads the " +
			"run as dead and resets a bead that is being worked.")
	}
	if withRunner.sessionID != "" {
		t.Errorf("the run's session id was set to %q for a run with a command runner",
			withRunner.sessionID)
	}

	local := sessionCollideSetUp(t, runID, false)
	if !local.took || !local.recorded {
		t.Fatalf("a local run took no session (took=%v recorded=%v).\n"+
			"Both checks above are claims that something did NOT happen, and they are free in a "+
			"fixture where nothing happens either way.", local.took, local.recorded)
	}
}

// TestBeadRunOne_ARunWithACommandRunnerWritesNoRecord is the same claim as the
// test above, made one layer up, where the defect actually was.
//
// The test above drives setUpRunSession directly and passes the bool itself, so
// it pins that function's contract and nothing else. The bug was never in that
// contract — the guard body is unchanged. The bug was the CALLER handing it the
// wrong fact: beadRunOne asked "is there a remote bead context" where the launch
// asks "is there a command runner", and those two differ in exactly one case —
// no remote context AND a runner set. Only a test that drives beadRunOne can see
// which fact the caller passed, so only a test at this layer fails when it is
// the wrong one.
//
// This is the mirror of
// TestRunRegistry_TheRecordIsOnDiskAndNamesTheSessionBeforeTheAgentIsLaunched,
// which drives the same fixture with the same options and asserts the record IS
// written. That test is this one's positive control: it is what says the fixture
// can write a record at all, so "no record" here is a refusal and not an empty
// run. The two differ in one field — this one gives the run a command runner.
func TestBeadRunOne_ARunWithACommandRunnerWritesNoRecord(t *testing.T) {
	t.Parallel()

	// The registry is observed from the RUNNER, not from the spawn hook the
	// sibling tests use, and that is forced rather than chosen. A run with a
	// command runner never reaches tmux at all: its launch takes the local branch
	// only when it has no runner, and the remote branch needs a worker session
	// name a local run does not have, so neither fires and no session or window is
	// ever requested. The spawn hook therefore never runs, and an assertion hung
	// on it would hold for free in exactly the case under test.
	//
	// Every runner call is a point the run reached, so checking the registry on
	// each one asks "was a record on disk at any moment the run was executing".
	// The record is written before the cascade and given back by the run scope at
	// exit, so this is inside its whole lifetime.
	var mu sync.Mutex
	var projectDir string
	var recordSeen bool
	var trustCalls int

	runner := &tmux.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			mu.Lock()
			if name == "python3" {
				trustCalls++
			}
			if projectDir != "" {
				// The claim is that the run wrote NO record, so both registry tiers
				// count. A check on the legacy tier alone would pass while the run
				// wrote a schema-v2 dispatch record.
				if snapshot, err := runpkg.ScanRegistry(projectDir); err == nil &&
					len(snapshot.Legacy)+len(snapshot.Dispatch) > 0 {
					recordSeen = true
				}
			}
			mu.Unlock()

			// This neutralises both HOME-mutating programs on the launch
			// path, not just one: EnsureWorktreeTrustVia and
			// PrepareIsolatedClaudeConfigDirVia, both called from
			// internal/harness/claude/launchspec.go. Each takes a pure-Go branch
			// when the runner is nil, but with one set each spawns `python3 -` and
			// upserts into the REAL ~/.claude.json — the operator's own Claude Code
			// config, outside any t.TempDir(). Left to run, the trust call also
			// wedged: the run sat in CombinedOutput for nine minutes and took the
			// package to its timeout. `true` keeps this test off the operator's
			// machine state and bounded in time. Both are inside
			// BuildLaunchSpec and so strictly downstream of the record decision, so
			// nothing the test asserts is masked. The leak itself is hk-85pqo.
			if name == "python3" {
				return exec.CommandContext(ctx, "true")
			}
			// Everything else stays real. The run has to get far enough for the
			// assertions to be about a refusal rather than about an empty run.
			return exec.CommandContext(ctx, name, args...)
		},
	}

	out := surviveRunDriveWith(t, surviveRunOpts{
		ownSession:   true,
		realWorktree: true,
		runner:       runner,
		seedProject: func(dir string) {
			mu.Lock()
			projectDir = dir
			mu.Unlock()
		},
	})

	mu.Lock()
	sawTrust, sawRecord := trustCalls > 0, recordSeen
	mu.Unlock()

	// Those python3 programs are all run from claude.BuildLaunchSpec, inside the
	// cascade, which is strictly downstream of the record decision on BOTH the
	// fixed and the unfixed layout. Seeing one is how this test states that the
	// run got PAST that decision — without it, "no record" could mean the run
	// ended before anything was decided.
	if !sawTrust {
		t.Fatal("the run never reached the launch-spec build, so it never got past the point " +
			"where the record is decided. The assertion below would hold for free.")
	}

	if sawRecord {
		t.Error("a run with a command runner wrote a registry record.\n" +
			"The launch takes the run's own session only when it has NO runner, so no session " +
			"by that name is ever created. The record outlives the run pointing at nothing, and " +
			"the next boot's adoption pass reads a session it cannot find as a dead run — it " +
			"resets the bead and re-dispatches it under an agent that is still working it.\n" +
			"This is what the caller passing `rbc != nil` rather than `dotRunner != nil` does: " +
			"with no remote context but a runner set the two disagree, and the record is written " +
			"on the answer the launch will not act on.")
	}

	if len(out.adapter.sessions()) != 0 {
		t.Errorf("a run with a command runner took tmux sessions %v on this host.\n"+
			"Its agents execute through the runner, somewhere this tmux server does not reach, "+
			"so the session would stand empty.", out.adapter.sessions())
	}
}

// sessionCollideSetUpResult is what one drive of setUpRunSession left behind.
type sessionCollideSetUpResult struct {
	took      bool
	recorded  bool
	sessionID string
}

// sessionCollideSetUp drives the real setUpRunSession over a substrate that can
// create sessions, and reports what it did.
func sessionCollideSetUp(t *testing.T, runID core.RunID, hasRunner bool) sessionCollideSetUpResult {
	t.Helper()
	env := runloop.RunEnv{ProjectDir: t.TempDir(), QueueItemIndex: -1}
	handles := runloop.SharedHandles{Substrate: w4cFixtureSubstrate(t, &w4cFixtureAdapter{})}
	ports := runloop.RunPorts{Clock: substrate.SystemClock{}}

	took := setUpRunSession(&env, ports, handles, &runlease.Scope{}, hasRunner, runID,
		core.BeadID("hk-one-fact"))
	_, loadErr := legacyRunRecord(env.ProjectDir, runID.String())
	return sessionCollideSetUpResult{
		took:      took,
		recorded:  loadErr == nil,
		sessionID: env.RunSessionID,
	}
}
