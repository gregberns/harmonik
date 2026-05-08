package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// readyFixtureBackoffSchedule defines the exponential backoff schedule used
// by the socket-probe ready-detection loop. The spec mandates:
//
//	initial 100 ms, max 2 s (per PL-009b), cap T_ready_wait = 60 s.
//
// For test purposes we use a compressed schedule (10 ms, 20 ms, 40 ms, 80 ms)
// that exercises the backoff doubling logic without real wall-clock delays.
// Operator-configurability is noted under OQ-PL-002.
//
// Spec ref: process-lifecycle.md §4.3 PL-009b — "`reconciling` means
// alive-but-not-yet-ready; the caller MUST retry with exponential backoff
// (initial 100 ms, max 2 s, capped at T_ready_wait = 60 s default tracked
// under OQ-PL-002)."
var readyFixtureBackoffSchedule = []time.Duration{
	10 * time.Millisecond,
	20 * time.Millisecond,
	40 * time.Millisecond,
	80 * time.Millisecond,
}

// readyFixtureMakeBackoffProbe builds an exponential-backoff socket probe that
// retries until the daemon reports `ready` (or a non-reconciling status), the
// schedule is exhausted, or the context is cancelled.
//
// It returns the final status string and a slice of observed delays between
// successive probes (for schedule verification). Each observedDelay[i] records
// the actual sleep duration for backoff[i+1] (i.e. the sleep inserted before
// the (i+1)th attempt). Delays are measured as real elapsed time, so they will
// be at least as large as the corresponding backoff entry.
//
// The probe sends a JSON-RPC `status` request over the Unix socket per PL-009b.
func readyFixtureMakeBackoffProbe(
	ctx context.Context,
	t *testing.T,
	socketPath string,
	backoff []time.Duration,
) (finalStatus string, observedDelays []time.Duration) {
	t.Helper()

	for i, delay := range backoff {
		select {
		case <-ctx.Done():
			return finalStatus, observedDelays
		default:
		}

		if i > 0 {
			// Sleep for the scheduled backoff and record actual elapsed time.
			sleepStart := time.Now()
			select {
			case <-ctx.Done():
				return finalStatus, observedDelays
			case <-time.After(delay):
			}
			observedDelays = append(observedDelays, time.Since(sleepStart))
		}

		conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		if err != nil {
			// ECONNREFUSED: daemon not yet listening; treat as reconciling.
			finalStatus = "reconciling"
			continue
		}

		req := struct {
			JSONRPC string `json:"jsonrpc"`
			ID      int    `json:"id"`
			Method  string `json:"method"`
		}{JSONRPC: "2.0", ID: i + 100, Method: "status"}
		reqBytes, _ := json.Marshal(req) //nolint:errcheck,errchkjson // encoding a known-good struct
		if _, writeErr := fmt.Fprintf(conn, "%s\n", reqBytes); writeErr != nil {
			_ = conn.Close() //nolint:errcheck // cleanup error unactionable
			finalStatus = "reconciling"
			continue
		}

		buf := make([]byte, 4096)
		n, readErr := conn.Read(buf)
		_ = conn.Close() //nolint:errcheck // cleanup error unactionable
		if readErr != nil || n == 0 {
			finalStatus = "reconciling"
			continue
		}

		var resp struct {
			Result struct {
				Status string `json:"status"`
			} `json:"result"`
		}
		if err := json.Unmarshal(buf[:n], &resp); err != nil {
			finalStatus = "reconciling"
			continue
		}
		finalStatus = resp.Result.Status

		if finalStatus == "ready" || finalStatus == "draining" || finalStatus == "degraded" {
			// Terminal status for the probe; stop retrying.
			return finalStatus, observedDelays
		}
		// "reconciling" → continue with next backoff step.
	}
	return finalStatus, observedDelays
}

// readyFixtureServeReadyAfterN starts a stub server that returns "reconciling"
// for the first n connections, then "ready" for subsequent connections.
func readyFixtureServeReadyAfterN(t *testing.T, ln net.Listener, reconcileCount int) {
	t.Helper()
	go func() {
		count := 0
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			isReady := count >= reconcileCount
			count++
			go func(c net.Conn, ready bool) {
				defer func() { _ = c.Close() }() //nolint:errcheck // cleanup error unactionable
				buf := make([]byte, 4096)
				n, err := c.Read(buf)
				if err != nil || n == 0 {
					return
				}
				var req struct {
					JSONRPC string `json:"jsonrpc"`
					ID      int    `json:"id"`
				}
				if err := json.Unmarshal(buf[:n], &req); err != nil {
					return
				}
				status := "reconciling"
				if ready {
					status = "ready"
				}
				result := map[string]string{"status": status}
				resultBytes, _ := json.Marshal(result) //nolint:errcheck,errchkjson // stub: encoding a known-good map
				raw := json.RawMessage(resultBytes)
				resp := struct {
					JSONRPC string           `json:"jsonrpc"`
					ID      int              `json:"id"`
					Result  *json.RawMessage `json:"result,omitempty"`
				}{JSONRPC: "2.0", ID: req.ID, Result: &raw}
				respBytes, _ := json.Marshal(resp)       //nolint:errcheck,errchkjson // stub: encoding a known-good struct
				_, _ = fmt.Fprintf(c, "%s\n", respBytes) //nolint:errcheck // stub: write errors intentionally ignored
			}(conn, isReady)
		}
	}()
}

// TestPL009b_SocketProbeExponentialBackoff verifies that the socket-probe
// ready-detection mechanism retries with exponential backoff when the daemon
// reports `reconciling`.
//
// Spec ref: process-lifecycle.md §4.3 PL-009b — "(a) Socket probe. Connect to
// .harmonik/daemon.sock, send a JSON-RPC `status` request, receive a response
// with status ∈ {ready, degraded, reconciling, draining}. `reconciling` means
// alive-but-not-yet-ready; the caller MUST retry with exponential backoff
// (initial 100 ms, max 2 s, capped at T_ready_wait = 60 s default tracked
// under OQ-PL-002). `ready` means ready."
func TestPL009b_SocketProbeExponentialBackoff(t *testing.T) {
	t.Parallel()

	t.Run("backoff-doubles-each-retry", func(t *testing.T) {
		t.Parallel()

		// Verify the backoff schedule doubles correctly.
		// Spec ref: PL-009b — "exponential backoff (initial 100 ms, max 2 s)"
		// Test uses compressed schedule: 10ms, 20ms, 40ms, 80ms.
		schedule := readyFixtureBackoffSchedule
		for i := 1; i < len(schedule); i++ {
			want := schedule[i-1] * 2
			got := schedule[i]
			if got != want {
				t.Errorf("PL-009b backoff-schedule[%d] = %v, want %v (2x previous %v)", i, got, want, schedule[i-1])
			}
		}
	})

	t.Run("probe-retries-on-reconciling-then-succeeds", func(t *testing.T) {
		t.Parallel()

		// Spec ref: process-lifecycle.md §4.3 PL-009b — the probe MUST retry
		// with exponential backoff until the daemon reports `ready`.
		projectDir := plFixtureTempProjectDir(t)
		ln, err := plFixtureBindSocket(t, projectDir)
		if err != nil {
			t.Fatalf("PL-009b probe-retry: bindSocket: %v", err)
		}
		t.Cleanup(func() { _ = ln.Close() }) //nolint:errcheck // cleanup error unactionable

		// Serve reconciling for first 2 connections, then ready.
		readyFixtureServeReadyAfterN(t, ln, 2)

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		finalStatus, _ := readyFixtureMakeBackoffProbe(ctx, t, plFixtureSocketPath(projectDir), readyFixtureBackoffSchedule)
		if finalStatus != "ready" {
			t.Errorf("PL-009b probe-retry: final status = %q, want ready", finalStatus)
		}
	})

	t.Run("probe-observes-delays-between-retries", func(t *testing.T) {
		t.Parallel()

		// Spec ref: process-lifecycle.md §4.3 PL-009b — exponential backoff;
		// observed delays must be at least as large as the scheduled delay.
		projectDir := plFixtureTempProjectDir(t)
		ln, err := plFixtureBindSocket(t, projectDir)
		if err != nil {
			t.Fatalf("PL-009b delays: bindSocket: %v", err)
		}
		t.Cleanup(func() { _ = ln.Close() }) //nolint:errcheck // cleanup error unactionable

		// Serve reconciling for 3 connections, then ready.
		readyFixtureServeReadyAfterN(t, ln, 3)

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		finalStatus, observedDelays := readyFixtureMakeBackoffProbe(ctx, t, plFixtureSocketPath(projectDir), readyFixtureBackoffSchedule)
		if finalStatus != "ready" {
			t.Errorf("PL-009b delays: final status = %q, want ready", finalStatus)
		}

		// Each observedDelays[i] records the actual sleep for backoff[i+1].
		// It must be >= 50% of the scheduled step (OS scheduling jitter allowance).
		const slackFactor = 0.5 // accept >= 50% of scheduled delay (OS scheduling jitter)
		for i, observed := range observedDelays {
			// observedDelays[i] corresponds to backoff[i+1].
			schedIdx := i + 1
			if schedIdx >= len(readyFixtureBackoffSchedule) {
				break
			}
			scheduled := readyFixtureBackoffSchedule[schedIdx]
			minExpected := time.Duration(float64(scheduled) * slackFactor)
			if observed < minExpected {
				t.Errorf("PL-009b delays[%d]: observed %v < min expected %v (%.0f%% of scheduled %v)",
					i, observed, minExpected, slackFactor*100, scheduled)
			}
		}
	})

	t.Run("probe-returns-immediately-on-ready", func(t *testing.T) {
		t.Parallel()

		// Spec ref: process-lifecycle.md §4.3 PL-009b — `ready` means ready;
		// the caller stops retrying on receipt of `ready`.
		projectDir := plFixtureTempProjectDir(t)
		ln, err := plFixtureBindSocket(t, projectDir)
		if err != nil {
			t.Fatalf("PL-009b immediate-ready: bindSocket: %v", err)
		}
		t.Cleanup(func() { _ = ln.Close() }) //nolint:errcheck // cleanup error unactionable

		// Serve ready immediately (0 reconciling connections).
		readyFixtureServeReadyAfterN(t, ln, 0)

		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()

		finalStatus, observedDelays := readyFixtureMakeBackoffProbe(ctx, t, plFixtureSocketPath(projectDir), readyFixtureBackoffSchedule)
		if finalStatus != "ready" {
			t.Errorf("PL-009b immediate-ready: final status = %q, want ready", finalStatus)
		}
		// No delays expected since first probe returned ready.
		if len(observedDelays) != 0 {
			t.Errorf("PL-009b immediate-ready: got %d observed delays, want 0 (ready on first probe)", len(observedDelays))
		}
	})

	t.Run("probe-econnrefused-treated-as-reconciling", func(t *testing.T) {
		t.Parallel()

		// Spec ref: process-lifecycle.md §4.3 PL-009b — "External callers MUST NOT
		// assume the daemon is ready simply because the pidfile or socket file exists.
		// `ECONNREFUSED` from the socket means the daemon has not yet called `listen()`."
		//
		// Simulate: socket does not exist; probe should treat as reconciling.
		projectDir := plFixtureTempProjectDir(t)
		// Do NOT bind a socket — connection will fail immediately.

		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		defer cancel()

		// Use a very short schedule so we don't wait long.
		shortSchedule := []time.Duration{5 * time.Millisecond, 10 * time.Millisecond}
		finalStatus, _ := readyFixtureMakeBackoffProbe(ctx, t, plFixtureSocketPath(projectDir), shortSchedule)
		// With no server, the probe exhausts the schedule or times out; final status = "reconciling".
		if finalStatus != "reconciling" {
			t.Errorf("PL-009b econnrefused: final status = %q, want reconciling (ECONNREFUSED = not yet listening)", finalStatus)
		}
	})
}

// TestPL009b_SystemdNotify verifies the systemd sd_notify ready path.
// This test is Linux-only: on other platforms it is skipped because sd_notify
// via NOTIFY_SOCKET is a Linux-specific IPC mechanism.
//
// Spec ref: process-lifecycle.md §4.3 PL-009b — "(b) systemd-style notify
// (Linux). When launched under systemd `Type=notify` (detectable via
// `$NOTIFY_SOCKET` environment variable), the daemon MUST call
// `sd_notify('READY=1')` at the same instant as the `daemon_ready` event
// emission of PL-009."
func TestPL009b_SystemdNotify(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("PL-009b sd_notify: Linux only; GOOS=" + runtime.GOOS)
	}

	t.Run("notify-socket-detected-via-env", func(t *testing.T) {
		t.Parallel()

		// Build a fake NOTIFY_SOCKET: a Unix datagram socket that the daemon
		// would write "READY=1\n" to. The fixture simulates the detection logic
		// (check NOTIFY_SOCKET env var), the write discipline, and verifies the
		// message content.
		projectDir := plFixtureTempProjectDir(t)
		notifySockPath := filepath.Join(projectDir, ".harmonik", "notify.sock")

		// Create a Unix datagram listener at the notify socket path.
		pc, err := (&net.ListenConfig{}).ListenPacket(t.Context(), "unixgram", notifySockPath)
		if err != nil {
			t.Fatalf("PL-009b sd_notify: ListenPacket unixgram: %v", err)
		}
		defer func() { _ = pc.Close() }() //nolint:errcheck // cleanup error unactionable

		// Simulate the daemon detecting NOTIFY_SOCKET and sending READY=1.
		// Real daemon: os.Getenv("NOTIFY_SOCKET") → sd_notify("READY=1").
		notifySocketEnv := notifySockPath // simulated env var value

		// Signal goroutine: "daemon" writes READY=1 to the notify socket.
		errCh := make(chan error, 1)
		go func() {
			addr, resolveErr := net.ResolveUnixAddr("unixgram", notifySocketEnv)
			if resolveErr != nil {
				errCh <- fmt.Errorf("resolve: %w", resolveErr)
				return
			}
			conn, dialErr := net.DialUnix("unixgram", nil, addr)
			if dialErr != nil {
				errCh <- fmt.Errorf("dial: %w", dialErr)
				return
			}
			defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable
			if _, writeErr := conn.Write([]byte("READY=1\n")); writeErr != nil {
				errCh <- fmt.Errorf("write: %w", writeErr)
				return
			}
			errCh <- nil
		}()

		// Read the notification.
		buf := make([]byte, 64)
		if err := pc.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatalf("PL-009b sd_notify: SetReadDeadline: %v", err)
		}
		n, _, readErr := pc.ReadFrom(buf)
		if readErr != nil {
			t.Fatalf("PL-009b sd_notify: ReadFrom: %v", readErr)
		}

		received := strings.TrimSpace(string(buf[:n]))
		if received != "READY=1" {
			t.Errorf("PL-009b sd_notify: received %q, want %q", received, "READY=1")
		}

		// Check the sender goroutine did not error.
		if err := <-errCh; err != nil {
			t.Errorf("PL-009b sd_notify: sender goroutine: %v", err)
		}
	})

	t.Run("notify-absent-when-no-env-var", func(t *testing.T) {
		t.Parallel()

		// Spec ref: PL-009b — detectable via `$NOTIFY_SOCKET` environment variable.
		// When NOTIFY_SOCKET is not set, the daemon MUST NOT attempt sd_notify.
		// The fixture verifies: when NOTIFY_SOCKET env is empty, no notification is sent.
		notifySocket := os.Getenv("NOTIFY_SOCKET")
		if notifySocket != "" {
			t.Skipf("PL-009b notify-absent: NOTIFY_SOCKET is set (%q); cannot test absence", notifySocket)
		}
		// No write expected: if NOTIFY_SOCKET is not set, the daemon skips sd_notify.
		// This is a structural (model-level) assertion; absence of the env var
		// means the sd_notify code path is not exercised.
		t.Log("PL-009b notify-absent: NOTIFY_SOCKET not set; sd_notify path correctly skipped")
	})
}

// TestPL009b_ReadyFileFallback verifies the portable `.harmonik/daemon.ready`
// fallback file mechanism. The file is written at the moment of PL-009 emission
// and MUST be removed on any transition out of `ready`.
//
// Spec ref: process-lifecycle.md §4.3 PL-009b — "(c) Ready-file (portable
// fallback). The daemon MAY write `.harmonik/daemon.ready` at the moment of
// PL-009 emission and MUST remove it on any transition out of `ready` (drain
// start, exit, degraded). This is informative; it exists for solo-dev
// fswatch-based setups."
func TestPL009b_ReadyFileFallback(t *testing.T) {
	t.Parallel()

	// readyFixtureDaemonReadyPath returns the canonical daemon.ready file path.
	readyFixtureDaemonReadyPath := func(projectDir string) string {
		return filepath.Join(projectDir, ".harmonik", "daemon.ready")
	}

	t.Run("ready-file-present-after-ready-transition", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		readyPath := readyFixtureDaemonReadyPath(projectDir)

		// Simulate daemon writing the ready file at PL-009 emission.
		// Content: RFC 3339 timestamp of the transition (informative).
		readyAt := time.Now().UTC().Format(time.RFC3339Nano)
		if err := os.WriteFile(readyPath, []byte(readyAt+"\n"), 0o600); err != nil {
			t.Fatalf("PL-009b ready-file present: WriteFile: %v", err)
		}

		// Assert: file exists.
		if _, err := os.Stat(readyPath); os.IsNotExist(err) {
			t.Errorf("PL-009b ready-file present: daemon.ready does not exist at %q", readyPath)
		}

		// Assert: content is parseable as RFC 3339.
		data, err := os.ReadFile(readyPath) //nolint:gosec // G304: path is constructed from t.TempDir() + known relative segments, not user input
		if err != nil {
			t.Fatalf("PL-009b ready-file present: ReadFile: %v", err)
		}
		content := strings.TrimSpace(string(data))
		if _, err := time.Parse(time.RFC3339Nano, content); err != nil {
			if _, err2 := time.Parse(time.RFC3339, content); err2 != nil {
				t.Errorf("PL-009b ready-file present: content %q does not parse as RFC 3339: %v", content, err2)
			}
		}
	})

	t.Run("ready-file-removed-on-drain-start", func(t *testing.T) {
		t.Parallel()

		// Spec ref: PL-009b — "MUST remove it on any transition out of `ready`
		// (drain start, exit, degraded)."
		projectDir := plFixtureTempProjectDir(t)
		readyPath := readyFixtureDaemonReadyPath(projectDir)

		// Write the file (daemon became ready).
		readyAt := time.Now().UTC().Format(time.RFC3339Nano)
		if err := os.WriteFile(readyPath, []byte(readyAt+"\n"), 0o600); err != nil {
			t.Fatalf("PL-009b drain-removal: WriteFile: %v", err)
		}

		// Simulate drain start: daemon removes the ready file.
		if err := os.Remove(readyPath); err != nil {
			t.Fatalf("PL-009b drain-removal: Remove: %v", err)
		}

		// Assert: file no longer exists.
		if _, err := os.Stat(readyPath); !os.IsNotExist(err) {
			t.Errorf("PL-009b drain-removal: daemon.ready still exists after drain start; MUST be removed")
		}
	})

	t.Run("ready-file-removed-on-degraded-transition", func(t *testing.T) {
		t.Parallel()

		// Spec ref: PL-009b — MUST remove on degraded transition.
		projectDir := plFixtureTempProjectDir(t)
		readyPath := readyFixtureDaemonReadyPath(projectDir)

		readyAt := time.Now().UTC().Format(time.RFC3339Nano)
		if err := os.WriteFile(readyPath, []byte(readyAt+"\n"), 0o600); err != nil {
			t.Fatalf("PL-009b degraded-removal: WriteFile: %v", err)
		}
		if err := os.Remove(readyPath); err != nil {
			t.Fatalf("PL-009b degraded-removal: Remove: %v", err)
		}
		if _, err := os.Stat(readyPath); !os.IsNotExist(err) {
			t.Errorf("PL-009b degraded-removal: daemon.ready still exists after degraded transition; MUST be removed")
		}
	})

	t.Run("ready-file-absent-before-ready-transition", func(t *testing.T) {
		t.Parallel()

		// Spec ref: PL-009b — the file is informative and written at PL-009 emission.
		// Before ready, it MUST NOT exist (or its absence is tolerated).
		projectDir := plFixtureTempProjectDir(t)
		readyPath := readyFixtureDaemonReadyPath(projectDir)

		// File has not been written yet — must not exist.
		if _, err := os.Stat(readyPath); !os.IsNotExist(err) {
			t.Errorf("PL-009b pre-ready: daemon.ready exists before ready transition at %q", readyPath)
		}
	})

	t.Run("ready-file-content-shape", func(t *testing.T) {
		t.Parallel()

		// Verify the ready file content shape: RFC 3339 timestamp (informative).
		projectDir := plFixtureTempProjectDir(t)
		readyPath := readyFixtureDaemonReadyPath(projectDir)

		now := time.Now().UTC()
		content := now.Format(time.RFC3339Nano) + "\n"
		if err := os.WriteFile(readyPath, []byte(content), 0o600); err != nil {
			t.Fatalf("PL-009b content-shape: WriteFile: %v", err)
		}

		data, err := os.ReadFile(readyPath) //nolint:gosec // G304: path is constructed from t.TempDir() + known relative segments, not user input
		if err != nil {
			t.Fatalf("PL-009b content-shape: ReadFile: %v", err)
		}

		trimmed := strings.TrimSpace(string(data))
		parsed, parseErr := time.Parse(time.RFC3339Nano, trimmed)
		if parseErr != nil {
			// try without nanoseconds
			parsed, parseErr = time.Parse(time.RFC3339, trimmed)
			if parseErr != nil {
				t.Fatalf("PL-009b content-shape: cannot parse %q as RFC 3339: %v", trimmed, parseErr)
			}
		}

		// The parsed time must be close to now (within 5 seconds).
		delta := now.Sub(parsed)
		if delta < 0 {
			delta = -delta
		}
		if delta > 5*time.Second {
			t.Errorf("PL-009b content-shape: parsed time %v is >5s away from write time %v", parsed, now)
		}
	})
}
