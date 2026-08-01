package codextest_test

// L3 live tests — real codex app-server (codex-app-server T5, hk-oe86p)
//
// L3 tests require CODEX_LIVE=1 and a codex binary on PATH (or CODEX_BIN set).
// They are SKIPPED by default (CODEX_LIVE=0). The L3 happy-path is the
// PRE-DEPLOY E2E GATE for the codex-app-server integration.
//
// Budget: token-capped (one minimal text turn, "ping" prompt, timeout 90s).
// Wire-canary only: asserts the protocol handshake completes, not content.
//
// Run with: CODEX_LIVE=1 make test-codex-live
//
// Bead: hk-oe86p [codex-app-server T5]

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/codexwire"
)

// skipUnlessLive skips the test when CODEX_LIVE is not "1".
func skipUnlessLive(t *testing.T) {
	t.Helper()
	if os.Getenv("CODEX_LIVE") != "1" {
		t.Skip("CODEX_LIVE=1 required for L3 live tests (set env var to run)")
	}
}

// codexBinaryPath resolves the codex binary path from CODEX_BIN or PATH.
func codexBinaryPath(t *testing.T) string {
	t.Helper()
	if bin := os.Getenv("CODEX_BIN"); bin != "" {
		return bin
	}
	path, err := exec.LookPath("codex")
	if err != nil {
		t.Fatalf("codex binary not found on PATH (set CODEX_BIN= to override): %v", err)
	}
	return path
}

func cleanupAppServer(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	killed := false
	if err := cmd.Process.Kill(); err != nil {
		if !errors.Is(err, os.ErrProcessDone) {
			t.Errorf("kill codex app-server: %v", err)
		}
	} else {
		killed = true
	}
	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if !killed || !errors.As(err, &exitErr) {
			t.Errorf("wait for codex app-server: %v", err)
		}
	}
}

// ─── L3 — happy-path live wire canary ────────────────────────────────────────

// TestL3_HappyPathLive is the PRE-DEPLOY E2E gate for the codex-app-server
// wire protocol. It launches a real codex app-server subprocess, sends the
// minimal handshake (initialize → initialized → thread/start → turn/start),
// and asserts that a turn/completed frame arrives within the budget window.
//
// Token budget: one minimal prompt ("ok"), expected ~100 tokens.
// Timeout: 90s (the test kills the subprocess on timeout).
func TestL3_HappyPathLive(t *testing.T) {
	skipUnlessLive(t)
	t.Parallel()

	binary := codexBinaryPath(t)
	deadline := time.Now().Add(90 * time.Second)

	//nolint:gosec // G204: explicitly opt-in live test launches only the Codex binary resolved from CODEX_BIN or PATH.
	cmd := exec.CommandContext(t.Context(), binary, "app-server")
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Start codex app-server: %v", err)
	}
	t.Cleanup(func() {
		cleanupAppServer(t, cmd)
	})

	// writeFrame sends a JSON-RPC frame to the server's stdin.
	writeFrame := func(v any) {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal frame: %v", err)
		}
		b = append(b, '\n')
		if _, werr := stdin.Write(b); werr != nil {
			t.Fatalf("write frame: %v", werr)
		}
	}

	// Handshake step 1: initialize request
	writeFrame(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"clientInfo":   map[string]any{"name": "harmonik-l3", "title": "L3", "version": "0.0.1"},
			"capabilities": nil,
		},
	})

	// Read frames from stdout until we get the initialize result.
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4<<20), 4<<20)

	var threadID string

	readUntil := func(want string) (codexwire.Frame, error) {
		for scanner.Scan() {
			if time.Now().After(deadline) {
				return codexwire.Frame{}, fmt.Errorf("deadline exceeded waiting for %q", want)
			}
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			frame, parseErr := codexwire.Parse([]byte(line))
			if parseErr != nil {
				// Non-fatal: log and continue
				t.Logf("parse error (continuing): %v (raw: %q)", parseErr, line)
				continue
			}
			t.Logf("frame: kind=%v method=%q id=%s", frame.Kind, frame.Method, frame.ID)
			if want == "initialize_result" && frame.Kind == codexwire.FrameKindServerResponse && string(frame.ID) == "1" {
				return frame, nil
			}
			if frame.Method == want {
				return frame, nil
			}
		}
		if err := scanner.Err(); err != nil {
			return codexwire.Frame{}, fmt.Errorf("scan error: %w", err)
		}
		return codexwire.Frame{}, fmt.Errorf("EOF before seeing %q", want)
	}

	// Wait for initialize result
	if _, err := readUntil("initialize_result"); err != nil {
		t.Fatalf("L3: initialize result: %v", err)
	}

	// Handshake step 2: initialized notification (client → server)
	writeFrame(map[string]any{"jsonrpc": "2.0", "method": "initialized"})

	// Handshake step 3: thread/start
	writeFrame(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "thread/start",
		"params":  map[string]any{},
	})

	// Wait for thread/started notification (carries thread id)
	threadStartedFrame, err := readUntil("thread/started")
	if err != nil {
		t.Fatalf("L3: thread/started: %v", err)
	}
	// Extract thread id from the raw params.
	if threadStartedFrame.RawParams != nil {
		var params map[string]any
		if jsonErr := json.Unmarshal(threadStartedFrame.RawParams, &params); jsonErr == nil {
			if threadObj, ok := params["thread"].(map[string]any); ok {
				if id, ok := threadObj["id"].(string); ok {
					threadID = id
				}
			}
		}
	}
	if threadID == "" {
		t.Fatal("L3: thread/started: could not extract thread id from params")
	}
	t.Logf("L3: thread id = %s", threadID)

	// Handshake step 4: turn/start with a minimal prompt (wire-canary: "ok" prompt, budget-capped)
	writeFrame(map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "turn/start",
		"params": map[string]any{
			"threadId": threadID,
			"input": []map[string]any{{
				"type":          "text",
				"text":          "Reply with exactly one word: ok",
				"text_elements": []any{},
			}},
		},
	})

	// Wait for turn/completed — the E2E wire canary assertion
	_, err = readUntil("turn/completed")
	if err != nil {
		t.Fatalf("L3: turn/completed: %v", err)
	}
	t.Log("L3: turn/completed received — wire canary GREEN")
}

// TestL3_ProtocolVersionCanary verifies that the codex app-server binary
// responds to initialize with a non-empty userAgent field, which carries the
// version string. This is a protocol-version drift canary: a breaking version
// change would likely change the initialize result schema.
func TestL3_ProtocolVersionCanary(t *testing.T) {
	skipUnlessLive(t)
	t.Parallel()

	binary := codexBinaryPath(t)
	//nolint:gosec // G204: explicitly opt-in live test launches only the Codex binary resolved from CODEX_BIN or PATH.
	cmd := exec.CommandContext(t.Context(), binary, "app-server")
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start codex app-server: %v", err)
	}
	t.Cleanup(func() {
		cleanupAppServer(t, cmd)
	})

	req, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"clientInfo":   map[string]any{"name": "harmonik-l3-canary", "title": "L3", "version": "0.0.1"},
			"capabilities": nil,
		},
	})
	if err != nil {
		t.Fatalf("marshal initialize request: %v", err)
	}
	if _, err := stdin.Write(append(req, '\n')); err != nil {
		t.Fatalf("write initialize request: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		if time.Now().After(deadline) {
			t.Fatal("L3 version canary: timeout waiting for initialize result")
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		frame, parseErr := codexwire.Parse([]byte(line))
		if parseErr != nil {
			continue
		}
		if frame.Kind == codexwire.FrameKindServerResponse && string(frame.ID) == "1" {
			// Verify userAgent field present and non-empty via raw result.
			var result map[string]any
			if frame.RawResult != nil {
				if jsonErr := json.Unmarshal(frame.RawResult, &result); jsonErr == nil {
					ua, ok := result["userAgent"].(string)
					if !ok || ua == "" {
						t.Error("L3 version canary: initialize result missing userAgent (protocol drift?)")
					} else {
						t.Logf("L3 version canary: userAgent = %q", ua)
					}
				}
			}
			return
		}
	}
	t.Fatal("L3 version canary: EOF before initialize result")
}
