package lifecycle

// agentwatcherreap_hk6629b_test.go — tests for the hk-6629b launch-path reap
// of prior same-agent `comms recv --follow` / `subscribe --follow` watchers.
//
// GATE-0 (captain ruling, bead comment 2176): "a test that a prior same-agent
// --follow pid is reaped on the agent's next boot even while it's
// live/reading." TestHk6629b_ReapPriorAgentFollowWatchers_KillsLiveWatcher
// below is that test: it spawns a REAL, live child process (not a dead/exited
// one) and asserts ReapPriorAgentFollowWatchers still kills it — proving the
// reap does not depend on liveness or process-tree reparenting, matching the
// captain's explicit warning that a "kill only dead procs" heuristic would
// miss the actual bug.

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// agentWatcherFakeLister is a test-injectable AgentWatcherLister returning a
// deterministic PID list without consulting the OS process table.
type agentWatcherFakeLister struct {
	pids []int
	err  error
}

func (f *agentWatcherFakeLister) ListAgentFollowWatcherPIDs(_ context.Context, _ string) ([]int, error) {
	return f.pids, f.err
}

// TestHk6629b_MatchesAgentFollowWatcher_CommsRecv verifies the argv matcher
// recognizes `comms recv --agent X --follow` for agent X.
func TestHk6629b_MatchesAgentFollowWatcher_CommsRecv(t *testing.T) {
	t.Parallel()

	cmdline := "harmonik comms recv --agent captain --follow --json"
	if !matchesAgentFollowWatcher(cmdline, "captain") {
		t.Errorf("expected match for comms-recv --follow addressed to captain")
	}
	if matchesAgentFollowWatcher(cmdline, "paul") {
		t.Errorf("must not match a different agent name")
	}
}

// TestHk6629b_MatchesAgentFollowWatcher_Subscribe verifies the argv matcher
// recognizes `subscribe --to X --follow`.
func TestHk6629b_MatchesAgentFollowWatcher_Subscribe(t *testing.T) {
	t.Parallel()

	cmdline := "harmonik subscribe --to captain --follow --json"
	if !matchesAgentFollowWatcher(cmdline, "captain") {
		t.Errorf("expected match for subscribe --follow addressed to captain")
	}
}

// TestHk6629b_MatchesAgentFollowWatcher_EqualsForm verifies the "--flag=value"
// token form is recognized, not just "--flag value".
func TestHk6629b_MatchesAgentFollowWatcher_EqualsForm(t *testing.T) {
	t.Parallel()

	cmdline := "harmonik comms recv --agent=captain --follow"
	if !matchesAgentFollowWatcher(cmdline, "captain") {
		t.Errorf("expected match for --agent=captain equals-form token")
	}
}

// TestHk6629b_MatchesAgentFollowWatcher_NoSubstringCollision verifies that
// agent "captain" does not match a watcher addressed to "captain2" — a plain
// substring check would wrongly collide here.
func TestHk6629b_MatchesAgentFollowWatcher_NoSubstringCollision(t *testing.T) {
	t.Parallel()

	cmdline := "harmonik comms recv --agent captain2 --follow"
	if matchesAgentFollowWatcher(cmdline, "captain") {
		t.Errorf("must not match: agent name is a distinct token, not a substring")
	}
}

// TestHk6629b_MatchesAgentFollowWatcher_AnonymousSubscribeNoMatch verifies
// that an anonymous type-filtered `subscribe --follow` (no --to, e.g.
// captain's Watcher 2 shape: `subscribe --types epic_completed --follow`)
// never matches any agent name — it carries no agent identity to reap by.
func TestHk6629b_MatchesAgentFollowWatcher_AnonymousSubscribeNoMatch(t *testing.T) {
	t.Parallel()

	cmdline := "harmonik subscribe --types epic_completed --follow --json"
	if matchesAgentFollowWatcher(cmdline, "captain") {
		t.Errorf("an anonymous (no --to) subscribe --follow must not match any agent")
	}
}

// TestHk6629b_MatchesAgentFollowWatcher_NotFollowOrNotHarmonik verifies the
// non-matching guard rails: missing --follow, or a non-harmonik process that
// happens to mention an agent name, never match.
func TestHk6629b_MatchesAgentFollowWatcher_NotFollowOrNotHarmonik(t *testing.T) {
	t.Parallel()

	if matchesAgentFollowWatcher("harmonik comms recv --agent captain", "captain") {
		t.Errorf("must not match without --follow (a one-shot recv, not a persistent watcher)")
	}
	if matchesAgentFollowWatcher("some-other-tool --agent captain --follow", "captain") {
		t.Errorf("must not match a non-harmonik process")
	}
}

// TestHk6629b_ReapPriorAgentFollowWatchers_EmptyAgent verifies a no-op for an
// empty agent name (nothing to reap "on behalf of nobody").
func TestHk6629b_ReapPriorAgentFollowWatchers_EmptyAgent(t *testing.T) {
	t.Parallel()

	survived, err := ReapPriorAgentFollowWatchers(t.Context(), &agentWatcherFakeLister{pids: []int{123}}, "", 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(survived) != 0 {
		t.Errorf("survived = %v, want empty for blank agent name", survived)
	}
}

// TestHk6629b_ReapPriorAgentFollowWatchers_ExcludesSelf verifies excludePID is
// never targeted even if the lister (incorrectly) returns it.
func TestHk6629b_ReapPriorAgentFollowWatchers_ExcludesSelf(t *testing.T) {
	t.Parallel()

	selfPID := os.Getpid()
	survived, err := ReapPriorAgentFollowWatchers(t.Context(), &agentWatcherFakeLister{pids: []int{selfPID}}, "captain", selfPID, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(survived) != 0 {
		t.Errorf("survived = %v, want empty", survived)
	}
	// The strongest assertion: the test process itself (the caller) is still
	// alive — if excludePID were not honored, the test binary would have
	// signaled itself.
	if !orphanSweepIsPidLive(selfPID) {
		t.Fatal("test process was signaled despite excludePID matching it")
	}
}

// TestHk6629b_ReapPriorAgentFollowWatchers_ListError propagates lister errors.
func TestHk6629b_ReapPriorAgentFollowWatchers_ListError(t *testing.T) {
	t.Parallel()

	wantErr := context.DeadlineExceeded
	_, err := ReapPriorAgentFollowWatchers(t.Context(), &agentWatcherFakeLister{err: wantErr}, "captain", 0, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "enumerate") {
		t.Errorf("error %q does not mention enumerate", err.Error())
	}
}

// TestHk6629b_ReapPriorAgentFollowWatchers_KillsLiveWatcher is GATE-0: it
// spawns a REAL, live child process (simulating a prior same-agent --follow
// watcher that is still fully alive and reading — the exact shape of the
// observed bug, reparented to init but otherwise healthy) and asserts
// ReapPriorAgentFollowWatchers terminates it. This proves the reap is NOT
// gated on liveness or process-tree parentage — a "kill only dead procs"
// heuristic would pass a lister returning only exited PIDs and miss this
// case entirely, which is precisely the captain's stated concern.
func TestHk6629b_ReapPriorAgentFollowWatchers_KillsLiveWatcher(t *testing.T) {
	t.Parallel()

	const sentinelEnv = "GO_HK6629B_CHILD_STUB"
	if os.Getenv(sentinelEnv) == "1" {
		// Child: spin forever, simulating a live, still-reading --follow watcher.
		select {}
	}

	testBin := os.Args[0]
	//nolint:gosec // G204: testBin is os.Args[0] — the test binary itself
	cmd := exec.CommandContext(t.Context(), testBin, "-test.run=^TestHk6629b_ReapPriorAgentFollowWatchers_KillsLiveWatcher$")
	cmd.Env = append(os.Environ(), sentinelEnv+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start: %v", err)
	}
	childPID := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill() //nolint:errcheck // cleanup error unactionable
		_ = cmd.Wait()         //nolint:errcheck // cleanup error unactionable
	})

	time.Sleep(50 * time.Millisecond)
	if !orphanSweepIsPidLive(childPID) {
		t.Fatalf("child PID %d not live after Start", childPID)
	}

	// Fake lister returns the live child PID regardless of its liveness — the
	// point of this test is that ReapPriorAgentFollowWatchers trusts the
	// lister's identity match, not any liveness re-check of its own, before
	// deciding to signal it.
	lister := &agentWatcherFakeLister{pids: []int{childPID}}

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	// The "survived" return value is checked for liveness immediately after
	// SIGKILL is sent (inside ReapPriorAgentFollowWatchers), which races the
	// kernel's own signal delivery — discard it here, as the sibling
	// SweepOrphanBr test (BI-014a) does for the same reason. The durable
	// assertion is the post-Wait() liveness probe below.
	if _, err := ReapPriorAgentFollowWatchers(ctx, lister, "captain", 0, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_ = cmd.Wait() //nolint:errcheck // expect a signal-killed exit; reap the child before the liveness probe below

	if orphanSweepIsPidLive(childPID) {
		t.Errorf("child PID %d still live after reap; want dead (this is GATE-0: reap must kill a LIVE watcher, not just a dead one)", childPID)
	}
}

// TestHk6629b_OSAgentWatcherLister_ParsesRealProcessTable is a light smoke
// test of the production lister against the real process table — it does not
// assert on specific PIDs (the process table varies), only that `ps` invoked
// as OSAgentWatcherLister does not error or panic on this host.
func TestHk6629b_OSAgentWatcherLister_ParsesRealProcessTable(t *testing.T) {
	t.Parallel()

	_, err := OSAgentWatcherLister{}.ListAgentFollowWatcherPIDs(t.Context(), "captain")
	if err != nil {
		t.Logf("OSAgentWatcherLister returned error (acceptable on unusual hosts without ps): %v", err)
	}
}
