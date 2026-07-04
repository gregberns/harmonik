package main

// crew_brief_seed_hkncg9m_test.go — RED-then-GREEN coverage for the agent-brief
// boot seed wired to crew start (hk-ncg9m).
//
// Verifies that runCrewStartCoreWith fires the injectable briefSeed seam after a
// successful crew-start RPC, passing the resolved project dir, crew name, and the
// minted session ID returned by the daemon. Uses a minimal fake daemon socket so
// the post-RPC code path is exercised without a real daemon or tmux.
//
// Mirror: captain_launch_hkbcd0_test.go TestCaptainLaunch_PastesSeedToAgentPane_T10.
// Bead ref: hk-ncg9m.

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// startCrewBriefSeedMockDaemon spins up a Unix socket listener at
// <project>/.harmonik/daemon.sock and responds to every connection with a
// successful crew-start result containing sessionID. Returns a stop func.
//
// The mock responds immediately without reading the request (the socket buffer
// absorbs the small JSON payload). This mirrors the run_via_daemon_test.go
// pattern for fake daemon sockets.
func startCrewBriefSeedMockDaemon(t *testing.T, project, sessionID, crewName string) func() {
	t.Helper()
	harmonikDir := filepath.Join(project, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("startCrewBriefSeedMockDaemon: mkdir: %v", err)
	}
	sockPath := filepath.Join(harmonikDir, "daemon.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("startCrewBriefSeedMockDaemon: listen: %v", err)
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

// TestCrewStartCoreWith_CallsBriefSeedAfterRPC verifies that runCrewStartCoreWith
// fires the injectable briefSeed seam exactly once after a successful crew-start
// RPC, forwarding the resolved project dir, crew name, and daemon-minted session
// ID (T10/hk-ncg9m).
func TestCrewStartCoreWith_CallsBriefSeedAfterRPC(t *testing.T) {
	proj := socketSafeTempDir(t) // short /tmp path — avoids macOS 104-char socket limit
	const fixedSID = "abcdef12-1234-4abc-8def-abcdef123456"

	stop := startCrewBriefSeedMockDaemon(t, proj, fixedSID, "alpha")
	defer stop()

	var seedCalls int
	var gotProject, gotName, gotSID string
	captureSeed := crewBriefSeedFn(func(project, name, sessionID string) {
		seedCalls++
		gotProject = project
		gotName = name
		gotSID = sessionID
	})

	keeper, _, _ := captureCrewKeeperHkxxcv9(0)
	code := runCrewStartCoreWith([]string{"alpha", "--project", proj}, keeper, captureSeed)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if seedCalls != 1 {
		t.Fatalf("briefSeed called %d times, want 1", seedCalls)
	}
	if gotProject != proj {
		t.Errorf("briefSeed project = %q, want %q", gotProject, proj)
	}
	if gotName != "alpha" {
		t.Errorf("briefSeed name = %q, want %q", gotName, "alpha")
	}
	if gotSID != fixedSID {
		t.Errorf("briefSeed sessionID = %q, want %q", gotSID, fixedSID)
	}
}

// TestCrewStartCoreWith_NoBriefSeedWhenNoDaemon verifies that the brief seed is
// NOT called when the daemon is unreachable (RPC returns exit 17 early, before
// any post-RPC code). Complements the positive test above.
func TestCrewStartCoreWith_NoBriefSeedWhenNoDaemon(t *testing.T) {
	proj := t.TempDir()
	var seedCalls int
	captureSeed := crewBriefSeedFn(func(_, _, _ string) { seedCalls++ })

	keeper, _, _ := captureCrewKeeperHkxxcv9(0)
	code := runCrewStartCoreWith([]string{"beta", "--project", proj}, keeper, captureSeed)
	if code != 17 {
		t.Fatalf("exit = %d, want 17 (no daemon)", code)
	}
	if seedCalls != 0 {
		t.Errorf("briefSeed called %d times on daemon-down path, want 0", seedCalls)
	}
}
