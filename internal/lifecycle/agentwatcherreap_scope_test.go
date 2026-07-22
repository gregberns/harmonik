package lifecycle

// agentwatcherreap_scope_test.go — regression coverage for the two ways the
// hk-6629b launch-path reap could kill a process it does not own:
//
//	(1) no project scope — every project on the box runs a crew called "mike",
//	    so a name-only match reaps peer projects' watchers too;
//	(2) substring matching — an agent's `/bin/zsh -c '... harmonik comms recv
//	    --agent mike --follow ...'` tool wrapper matches as readily as the
//	    watcher it wraps, and killing it takes out live agent work.
//
// Both were reachable from `harmonik start crew <name>` / `harmonik captain`.
// The fixtures below are real command lines captured from a running fleet.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// --- (2) the process must BE the watcher, not merely mention it -------------

// TestWatcherMatch_WrapperShellNotMatched is the hazard-2 regression: a zsh
// tool wrapper whose script text contains the watcher command must NOT match.
// This exact command line was live on the fleet; under the old substring
// matcher `harmonik start crew assessor` would have SIGKILLed it, taking out
// the agent's Bash-tool subprocess rather than a stray watcher.
func TestWatcherMatch_WrapperShellNotMatched(t *testing.T) {
	t.Parallel()

	wrapper := "/bin/zsh -c source /Users/gb/.claude/shell-snapshots/snapshot-zsh-1784698986137-e4xw5t.sh 2>/dev/null || true && " +
		"setopt NO_EXTENDED_GLOB NO_BARE_GLOB_QUAL 2>/dev/null || true && " +
		"eval 'harmonik comms recv --agent assessor --follow --json 2>&1 | head -200' < /dev/null"
	if matchesAgentFollowWatcher(wrapper, "assessor") {
		t.Error("wrapper shell matched: killing it takes out a live agent's tool subprocess, not a watcher")
	}
}

// TestWatcherMatch_RearmingLoopNotMatched covers the other live wrapper shape:
// a `while true` loop that re-spawns the watcher. Killing the loop is a
// different act from killing the watcher, and is not what the reap is for.
func TestWatcherMatch_RearmingLoopNotMatched(t *testing.T) {
	t.Parallel()

	loop := "/bin/zsh -c while true; do harmonik comms recv --agent juliet --follow --json; sleep 3; done"
	if matchesAgentFollowWatcher(loop, "juliet") {
		t.Error("re-arming loop wrapper matched; only the watcher process itself may be reaped")
	}
}

// TestWatcherMatch_RealWatcherStillMatches guards against over-correction: the
// actual watcher shapes, including an absolute argv[0] (a peer project runs
// harmonik from its own build directory), must still match.
func TestWatcherMatch_RealWatcherStillMatches(t *testing.T) {
	t.Parallel()

	for _, cmdline := range []string{
		"harmonik comms recv --agent mike --follow --json",
		"harmonik comms recv --follow --json --agent mike",
		"harmonik comms recv --agent=mike --follow",
		"/tmp/hk155gs/harmonik comms recv --agent mike --follow",
		"harmonik subscribe --to mike --follow",
	} {
		if !matchesAgentFollowWatcher(cmdline, "mike") {
			t.Errorf("real watcher no longer matched: %q", cmdline)
		}
	}
}

// TestWatcherMatch_WrongSubcommandNotMatched verifies the subcommand is read
// positionally: a harmonik invocation that merely carries the word elsewhere
// is not a watcher.
func TestWatcherMatch_WrongSubcommandNotMatched(t *testing.T) {
	t.Parallel()

	for _, cmdline := range []string{
		"harmonik comms send --agent mike --follow --body subscribe",
		"harmonik queue submit --agent mike --follow",
		"grep -n harmonik comms recv --agent mike --follow",
	} {
		if matchesAgentFollowWatcher(cmdline, "mike") {
			t.Errorf("non-watcher matched: %q", cmdline)
		}
	}
}

// --- (1) project scope ------------------------------------------------------

// scopeTestHelperEnv marks a re-exec of this test binary as the blocking
// helper child rather than a normal test run.
const scopeTestHelperEnv = "HARMONIK_SCOPE_TEST_HELPER"

// TestScopeHelperProcess is not a test: it is the body of the helper child
// startEnvChild spawns. Under a normal run the sentinel is absent and it
// returns immediately.
//
// The helper re-execs THIS test binary rather than a system tool such as
// /bin/sleep, because darwin withholds the environment of SIP-protected
// platform binaries from `ps -E` — a /bin/sleep child is unreadable on darwin
// for reasons that have nothing to do with the code under test. A Go-built
// binary is readable, and matches the population this code really runs on.
func TestScopeHelperProcess(t *testing.T) {
	if os.Getenv(scopeTestHelperEnv) != "1" {
		return
	}
	time.Sleep(30 * time.Second)
}

// startEnvChild spawns a real, live child process carrying env, and returns
// its PID. The command line is irrelevant to candidateInProject (which is
// passed a cmdline separately) — what matters is that the process really
// exists with a real exec-time environment to read back.
func startEnvChild(t *testing.T, env []string) int {
	t.Helper()

	//nolint:gosec // G204: os.Args[0] is this test binary and the argument is a constant
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=TestScopeHelperProcess")
	cmd.Env = append([]string{scopeTestHelperEnv + "=1"}, env...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper child: %v", err)
	}
	t.Cleanup(func() {
		if killErr := cmd.Process.Kill(); killErr != nil {
			t.Logf("kill helper child: %v", killErr)
		}
		if _, waitErr := cmd.Process.Wait(); waitErr != nil {
			t.Logf("wait helper child: %v", waitErr)
		}
	})
	return cmd.Process.Pid
}

// TestCandidateInProject_PeerProjectNotClaimed is the hazard-1 regression, run
// against a real process and the real platform environment reader: a watcher
// belonging to a peer project must not be claimed by this project's reap.
func TestCandidateInProject_PeerProjectNotClaimed(t *testing.T) {
	t.Parallel()

	ours := t.TempDir()
	peer := t.TempDir()
	pid := startEnvChild(t, []string{ProjectPathEnvKey + "=" + peer})
	const cmdline = "harmonik comms recv --agent mike --follow --json"

	if candidateInProject(t.Context(), pid, cmdline, canonicalProjectPath(ours)) {
		t.Error("peer project's watcher claimed as ours — this is the cross-project kill")
	}
	if !candidateInProject(t.Context(), pid, cmdline, canonicalProjectPath(peer)) {
		t.Error("watcher not claimed by its OWN project; the reap would never fire")
	}
}

// TestCandidateInProject_NoMarkerIsLeftAlone verifies the fail-closed default:
// a process that can prove nothing is not ours to kill.
func TestCandidateInProject_NoMarkerIsLeftAlone(t *testing.T) {
	t.Parallel()

	ours := t.TempDir()
	pid := startEnvChild(t, []string{"PATH=/usr/bin:/bin"}) // no project marker at all

	if candidateInProject(t.Context(), pid, "harmonik comms recv --agent mike --follow", canonicalProjectPath(ours)) {
		t.Error("unmarked process claimed; an unprovable candidate must be left alone")
	}
}

// TestCandidateInProject_ExplicitProjectFlag verifies the cheaper proof: an
// explicit --project flag is honoured without reading the environment, in both
// the "--project=<path>" and "--project <path>" forms.
func TestCandidateInProject_ExplicitProjectFlag(t *testing.T) {
	t.Parallel()

	ours := t.TempDir()
	peer := t.TempDir()
	// PID 1 is live and certainly carries no harmonik marker, so a match here
	// can only have come from the command line.
	for _, form := range []string{"--project " + ours, "--project=" + ours} {
		cmdline := "harmonik comms recv --agent mike --follow " + form
		if !candidateInProject(t.Context(), 1, cmdline, canonicalProjectPath(ours)) {
			t.Errorf("--project not honoured in form %q", form)
		}
		if candidateInProject(t.Context(), 1, cmdline, canonicalProjectPath(peer)) {
			t.Errorf("--project matched the wrong project in form %q", form)
		}
	}
}

// TestCanonicalProjectPath_EquatesSpellings verifies two spellings of one live
// path compare equal, so a trailing separator or a relative spelling does not
// silently turn into "different project" (which would fail closed and quietly
// disable the reap).
func TestCanonicalProjectPath_EquatesSpellings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	want := canonicalProjectPath(dir)
	for _, spelling := range []string{dir + string(filepath.Separator), filepath.Join(dir, "sub", "..")} {
		if got := canonicalProjectPath(spelling); got != want {
			t.Errorf("canonicalProjectPath(%q) = %q, want %q", spelling, got, want)
		}
	}
}

// --- fail-closed wiring -----------------------------------------------------

// TestOSAgentWatcherLister_UnscopedListsNothing verifies a lister with no
// project reaps nothing rather than everything. This is the guard that keeps a
// future caller who forgets the scope from re-introducing the cross-project
// kill.
func TestOSAgentWatcherLister_UnscopedListsNothing(t *testing.T) {
	t.Parallel()

	pids, err := OSAgentWatcherLister{}.ListAgentFollowWatcherPIDs(t.Context(), "captain")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pids) != 0 {
		t.Errorf("unscoped lister returned %d pid(s), want 0", len(pids))
	}
}

// TestReapPriorAgentFollowWatchers_NoProjectIsNoOp verifies the production
// entry point refuses to build an unscoped lister: with no injected lister and
// no project, it must not touch the process table at all.
func TestReapPriorAgentFollowWatchers_NoProjectIsNoOp(t *testing.T) {
	t.Parallel()

	survived, err := ReapPriorAgentFollowWatchers(t.Context(), nil, "captain", "", os.Getpid(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(survived) != 0 {
		t.Errorf("survived = %v, want empty", survived)
	}
}
