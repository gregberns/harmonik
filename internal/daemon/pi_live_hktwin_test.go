package daemon_test

// pi_live_hktwin_test.go — M6 WS3-pi (pi-A: the real-pi oracle).
//
// This is the REAL-BOX-GATED live leg: it drives a real `pi --mode json`
// single-turn session, asserts the terminal NDJSON sequence (session first,
// agent_end last, per internal/daemon/pijsonlparser.go), and writes the capture
// to testdata/twin-parity/pi/<scn>/{ndjson,events.jsonl} so it can replace the
// deterministic twin sample as the parity-gate oracle.
//
// It is DEFAULT-SKIPPED without PI_LIVE=1 — a box without pi provider auth is
// clean. It uses the anti-false-green require idiom (mirrors WS1.3
// rsb12RequireSSHOrSkip / WS3-codex-B codexDriftRequireOrSkip): with PI_LIVE=1
// but pi unavailable/unconfigured, HARMONIK_REQUIRE_PI_LIVE=1 turns the skip into
// a Fatalf so an environment that INTENDS to run the live oracle fails loudly
// rather than silently greening.
//
// Bead: M6 WS3-pi.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// piLiveRequireOrSkip mirrors the WS3-codex-B codexDriftRequireOrSkip anti-false-
// green pattern. By default it SKIPs (clean on boxes without live pi). With
// HARMONIK_REQUIRE_PI_LIVE=1 it FATALs instead — so an environment that INTENDS
// to run the live pi oracle fails loudly rather than silently greening.
func piLiveRequireOrSkip(t *testing.T, msg string) {
	t.Helper()
	if os.Getenv("HARMONIK_REQUIRE_PI_LIVE") == "1" {
		t.Fatalf("%s (HARMONIK_REQUIRE_PI_LIVE=1)", msg)
	}
	t.Skipf("%s", msg)
}

// piLiveScenario names the capture directory the live oracle writes into.
const piLiveScenario = "happy-path"

// TestPiA_LiveSingleTurn drives a real pi single-turn session and captures it.
//
// Default-SKIPPED (PI_LIVE!=1). When PI_LIVE=1 but pi is unavailable/unconfigured
// it defers to piLiveRequireOrSkip so HARMONIK_REQUIRE_PI_LIVE=1 cannot be
// silently greened.
func TestPiA_LiveSingleTurn(t *testing.T) {
	if os.Getenv("PI_LIVE") != "1" {
		t.Skip("PI_LIVE=1 required for the real-pi oracle (default-skipped; needs pi provider auth)")
	}

	// Resolve the pi binary: PI_BIN overrides, else "pi" on PATH.
	piBin := os.Getenv("PI_BIN")
	if piBin == "" {
		piBin = "pi"
	}
	if _, err := exec.LookPath(piBin); err != nil {
		piLiveRequireOrSkip(t, fmt.Sprintf("pi binary %q not found on PATH (set PI_BIN)", piBin))
		return
	}

	provider := os.Getenv("PI_PROVIDER")
	model := os.Getenv("PI_MODEL")
	if provider == "" || model == "" {
		piLiveRequireOrSkip(t, "live pi oracle needs PI_PROVIDER and PI_MODEL set (provider auth + model id)")
		return
	}

	// Drive a real single turn: pi --mode json --no-extensions --provider <p>
	// --model <m> "<prompt>" — the initial-turn argv the adapter builds
	// (internal/daemon/pilaunchspec.go §buildPiLaunchSpec).
	ctxTimeout := 120 * time.Second
	if v := os.Getenv("PI_LIVE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			ctxTimeout = d
		}
	}
	cmd := exec.Command(piBin, //nolint:gosec // G204: piBin is operator-provided on a live-gated box.
		"--mode", "json", "--no-extensions",
		"--provider", provider, "--model", model,
		"Reply with the single word: ok.")
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start pi: %v", err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("pi exited non-zero: %v\nstderr: %s", err, stderr.String())
		}
	case <-time.After(ctxTimeout):
		_ = cmd.Process.Kill()
		t.Fatalf("pi did not complete within %s (pi process-exit is unreliable; agent_end is the real terminal)", ctxTimeout)
	}

	out := stdout.Bytes()
	if len(bytes.TrimSpace(out)) == 0 {
		t.Fatal("pi produced no NDJSON on stdout")
	}

	// Assert the terminal NDJSON sequence via the REAL parser path: session
	// captured (PI-012), agent_end fired (PI-014), session first + agent_end last.
	var gotSessionID string
	var agentEndFired bool
	interceptor := daemon.ExportedNewPiSessionIDInterceptor(
		bytes.NewReader(out),
		func(id string) { gotSessionID = id },
		func() { agentEndFired = true },
	)
	_, _ = interceptor.Read(make([]byte, len(out)+1)) // drive the interceptor over the buffered stream
	if gotSessionID == "" {
		t.Error("live pi: no session id captured (PI-012)")
	}
	if !agentEndFired {
		t.Error("live pi: agent_end never fired (PI-014 terminal)")
	}

	firstKind, lastKind := piFirstLastKind(t, out)
	if firstKind != "session" {
		t.Errorf("live pi: first NDJSON kind = %q, want %q", firstKind, "session")
	}
	if lastKind != "agent_end" {
		t.Errorf("live pi: last NDJSON kind = %q, want %q", lastKind, "agent_end")
	}

	// Write the capture: ndjson (raw pi stream) + a projected durable events.jsonl
	// (terminal triad the daemon projects on success). These OVERWRITE the
	// deterministic twin sample so the parity gate can grade against a real run.
	dir := filepath.Join("..", "..", "testdata", "twin-parity", "pi", piLiveScenario)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir capture dir: %v", err)
	}
	//nolint:gosec // G306: capture artifacts are non-sensitive test fixtures.
	if err := os.WriteFile(filepath.Join(dir, "ndjson"), out, 0o644); err != nil {
		t.Fatalf("write ndjson: %v", err)
	}
	events := piProjectDurableTriad(gotSessionID)
	//nolint:gosec // G306: capture artifacts are non-sensitive test fixtures.
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), events, 0o644); err != nil {
		t.Fatalf("write events.jsonl: %v", err)
	}
	t.Logf("live pi oracle GREEN: captured %d NDJSON bytes into %s", len(out), dir)
}

// piFirstLastKind extracts the "type" of the first and last non-blank NDJSON
// lines using the real parser so it sees exactly what the daemon does.
func piFirstLastKind(t *testing.T, out []byte) (first, last string) {
	t.Helper()
	for _, raw := range bytes.Split(out, []byte("\n")) {
		line := bytes.TrimSpace(raw)
		if len(line) == 0 {
			continue
		}
		_, rawType, _, _, err := daemon.ExportedParsePiNDJSONEvent(line)
		if err != nil {
			continue
		}
		if first == "" {
			first = rawType
		}
		last = rawType
	}
	return first, last
}

// piProjectDurableTriad renders the daemon-projected durable terminal triad for a
// successful pi run into events.jsonl bytes. A raw pi exec has no daemon, so the
// oracle projects the same triad the daemon would journal on success.
func piProjectDurableTriad(sessionID string) []byte {
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	lines := []string{
		fmt.Sprintf(`{"event_type":"agent_ready","timestamp":%q,"session_id":%q}`, stamp, sessionID),
		fmt.Sprintf(`{"event_type":"outcome_emitted","timestamp":%q,"outcome_status":"success","node_id":"implement"}`, stamp),
		fmt.Sprintf(`{"event_type":"bead_closed","timestamp":%q,"bead_id":"hk-pi-live"}`, stamp),
		fmt.Sprintf(`{"event_type":"run_completed","timestamp":%q,"success":true}`, stamp),
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}
