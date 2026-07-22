package main

// watcherreap_hk6629b_test.go — coverage for the hk-6629b launch-path reap
// hook wired into captain launch (captain.go) and crew start (crew.go): both
// call sites must invoke the reap hook with the launching agent's name before
// arming anything new, so a relaunch after /clear reaps its predecessor's
// `comms recv --follow` / `subscribe --follow` watcher process.
//
// Bead ref: hk-6629b.

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// TestCaptainLaunch_ReapsPriorAgentWatchers_Hk6629b verifies that
// runCaptainLaunchWithOps invokes captainReapPriorWatchers with the resolved
// captain name exactly once, before the launch completes.
func TestCaptainLaunch_ReapsPriorAgentWatchers_Hk6629b(t *testing.T) {
	var calls int
	var gotAgent, gotProject string
	orig := captainReapPriorWatchers
	captainReapPriorWatchers = func(agent, project string) {
		calls++
		gotAgent = agent
		gotProject = project
	}
	t.Cleanup(func() { captainReapPriorWatchers = orig })

	run, captured := captureRunHkly0n()
	ops := &fakeCaptainOps{}
	proj := t.TempDir()

	code := runCaptainLaunchWithOps([]string{"--name", "captain", "--project", proj}, run, noopKeeperHkly0n, ops)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if *captured == nil {
		t.Fatal("agent window not launched")
	}
	if calls != 1 {
		t.Fatalf("captainReapPriorWatchers called %d times, want 1", calls)
	}
	if gotAgent != "captain" {
		t.Errorf("captainReapPriorWatchers agent = %q, want %q", gotAgent, "captain")
	}
	// The reap MUST be scoped to the launching project: agent names repeat
	// across projects sharing a box, and an unscoped reap kills a peer
	// project's same-named watcher.
	if gotProject != proj {
		t.Errorf("captainReapPriorWatchers project = %q, want %q", gotProject, proj)
	}
}

// TestCaptainLaunch_ReapsPriorAgentWatchers_D7Refuse_Hk6629b verifies the reap
// hook is NOT invoked when the D7 pre-flight refuses the launch (a live
// captain already occupies the session) — the launch itself does not
// proceed, so nothing should be reaped on its behalf.
func TestCaptainLaunch_ReapsPriorAgentWatchers_D7Refuse_Hk6629b(t *testing.T) {
	var calls int
	orig := captainReapPriorWatchers
	captainReapPriorWatchers = func(_, _ string) { calls++ }
	t.Cleanup(func() { captainReapPriorWatchers = orig })

	run, _ := captureRunHkly0n()
	ops := &fakeCaptainOps{existsResult: true, aliveResult: true} // live captain already running
	proj := t.TempDir()

	code := runCaptainLaunchWithOps([]string{"--name", "captain", "--project", proj}, run, noopKeeperHkly0n, ops)
	if code == 0 {
		t.Fatalf("exit = %d, want non-zero (D7 refuse)", code)
	}
	if calls != 0 {
		t.Errorf("captainReapPriorWatchers called %d times on a refused launch, want 0", calls)
	}
}

// startWatcherReapMockDaemon spins up a Unix socket listener at
// <project>/.harmonik/daemon.sock and responds to every connection with a
// successful crew-start result. Mirrors
// crew_brief_seed_hkncg9m_test.go's startCrewBriefSeedMockDaemon.
func startWatcherReapMockDaemon(t *testing.T, project, sessionID, crewName string) func() {
	t.Helper()
	harmonikDir := filepath.Join(project, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("startWatcherReapMockDaemon: mkdir: %v", err)
	}
	sockPath := filepath.Join(harmonikDir, "daemon.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("startWatcherReapMockDaemon: listen: %v", err)
	}
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				result, _ := json.Marshal(struct {
					SessionID string `json:"session_id"`
					Name      string `json:"name"`
				}{SessionID: sessionID, Name: crewName})
				resp, _ := json.Marshal(crewSocketResponse{Ok: true, Result: result})
				_, _ = fmt.Fprintf(c, "%s\n", resp)
			}(conn)
		}
	}()
	return func() { _ = ln.Close() }
}

// TestCrewStart_ReapsPriorAgentWatchers_Hk6629b verifies that
// runCrewStartCoreWith invokes crewReapPriorWatchers with the resolved crew
// name exactly once for a successful crew start.
func TestCrewStart_ReapsPriorAgentWatchers_Hk6629b(t *testing.T) {
	var calls int
	var gotAgent, gotProject string
	orig := crewReapPriorWatchers
	crewReapPriorWatchers = func(agent, project string) {
		calls++
		gotAgent = agent
		gotProject = project
	}
	t.Cleanup(func() { crewReapPriorWatchers = orig })

	proj := socketSafeTempDir(t) // short /tmp path — avoids macOS 104-char socket limit
	const fixedSID = "abcdef12-1234-4abc-8def-abcdef123456"
	stop := startWatcherReapMockDaemon(t, proj, fixedSID, "gamma")
	defer stop()

	keeper, _, _ := captureCrewKeeperHkxxcv9(0)
	code := runCrewStartCoreWith([]string{"gamma", "--project", proj}, keeper, nil)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if calls != 1 {
		t.Fatalf("crewReapPriorWatchers called %d times, want 1", calls)
	}
	if gotAgent != "gamma" {
		t.Errorf("crewReapPriorWatchers agent = %q, want %q", gotAgent, "gamma")
	}
	// Scoped to the launching project — see the captain-side assertion.
	if gotProject != proj {
		t.Errorf("crewReapPriorWatchers project = %q, want %q", gotProject, proj)
	}
}

// TestCrewStart_ReapsPriorAgentWatchers_NoDaemonSkips_Hk6629b verifies the
// reap hook is called on the launch path BEFORE the daemon RPC is even
// attempted (it runs alongside ensureBootAssets, ahead of the crew-start
// send) — so it still fires even when the daemon is down. This documents the
// current wiring: the reap runs client-side, independent of RPC success,
// matching the "launch-path only, no daemon edit" scope of the captain
// ruling.
func TestCrewStart_ReapsPriorAgentWatchers_NoDaemonSkips_Hk6629b(t *testing.T) {
	var calls int
	orig := crewReapPriorWatchers
	crewReapPriorWatchers = func(_, _ string) { calls++ }
	t.Cleanup(func() { crewReapPriorWatchers = orig })

	proj := t.TempDir() // no mock daemon listening here

	keeper, _, _ := captureCrewKeeperHkxxcv9(0)
	code := runCrewStartCoreWith([]string{"delta", "--project", proj}, keeper, nil)
	if code != 17 {
		t.Fatalf("exit = %d, want 17 (no daemon)", code)
	}
	if calls != 1 {
		t.Errorf("crewReapPriorWatchers called %d times, want 1 (reap runs before the RPC attempt)", calls)
	}
}
