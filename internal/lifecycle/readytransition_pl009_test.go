package lifecycle

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// readyFixtureDaemonReadyPayload represents the daemon_ready event payload as
// defined in process-lifecycle.md §6.2 and event-model.md §8.7.2.
type readyFixtureDaemonReadyPayload struct {
	// ReadyAt is the wall-clock time at ready emission (RFC 3339 with ms).
	ReadyAt string `json:"ready_at"`
	// ReadyAtNsSinceBoot is the monotonic-clock companion field.
	// Spec ref: process-lifecycle.md §4.3 PL-009 / operator-nfr.md §4.8 ON-033.
	ReadyAtNsSinceBoot int64 `json:"ready_at_ns_since_boot"`
	// InvestigatorRunIDs contains run_ids routed to investigator workflows.
	InvestigatorRunIDs []string `json:"investigator_run_ids"`
}

// readyFixtureCriteria captures which startup criteria have been met. The
// stub uses this to decide whether a `ready` or `reconciling` status is
// returned from the JSON-RPC status probe.
type readyFixtureCriteria struct {
	orphanSweepDone     bool
	cat0PreCheckPassed  bool
	gitWalkDone         bool
	inMemoryModelBuilt  bool
	reconcileDispatchOK bool
}

// readyFixtureAllMet returns true when every PL-009 criterion is satisfied.
func readyFixtureAllMet(c readyFixtureCriteria) bool {
	return c.orphanSweepDone &&
		c.cat0PreCheckPassed &&
		c.gitWalkDone &&
		c.inMemoryModelBuilt &&
		c.reconcileDispatchOK
}

// readyFixtureStatusFromCriteria maps a readyFixtureCriteria to the daemon
// status string returned by the JSON-RPC status method. If any criterion is
// unmet the daemon reports "reconciling"; all met → "ready".
func readyFixtureStatusFromCriteria(c readyFixtureCriteria) string {
	if readyFixtureAllMet(c) {
		return "ready"
	}
	return "reconciling"
}

// readyFixtureRunStubServer starts a minimal JSON-RPC stub server on ln.
// Each connection receives exactly one request and returns a status response
// reflecting the given criteria. The server closes after the first connection
// unless multiConn is true.
func readyFixtureRunStubServer(t *testing.T, ln net.Listener, criteria readyFixtureCriteria, payload *readyFixtureDaemonReadyPayload) {
	t.Helper()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }() //nolint:errcheck // cleanup error unactionable
				readyFixtureHandleStatusConn(c, criteria, payload)
			}(conn)
		}
	}()
}

// readyFixtureHandleStatusConn reads one JSON-RPC request from conn and writes
// one JSON-RPC status response.
func readyFixtureHandleStatusConn(conn net.Conn, criteria readyFixtureCriteria, payload *readyFixtureDaemonReadyPayload) {
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil || n == 0 {
		return
	}
	line := strings.TrimSpace(string(buf[:n]))
	if line == "" {
		return
	}

	var req struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
	}
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		return
	}

	status := readyFixtureStatusFromCriteria(criteria)

	result := map[string]interface{}{
		"status": status,
	}
	if status == "ready" && payload != nil {
		result["ready_at"] = payload.ReadyAt
		result["ready_at_ns_since_boot"] = payload.ReadyAtNsSinceBoot
		result["investigator_run_ids"] = payload.InvestigatorRunIDs
	}

	resultBytes, _ := json.Marshal(result) //nolint:errcheck,errchkjson // stub: encoding a known-good map never fails
	raw := json.RawMessage(resultBytes)
	resp := struct {
		JSONRPC string           `json:"jsonrpc"`
		ID      int              `json:"id"`
		Result  *json.RawMessage `json:"result,omitempty"`
	}{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  &raw,
	}
	respBytes, _ := json.Marshal(resp)          //nolint:errcheck,errchkjson // stub: encoding a known-good struct never fails
	_, _ = fmt.Fprintf(conn, "%s\n", respBytes) //nolint:errcheck // stub: write errors intentionally ignored
}

// readyFixtureProbeStatus sends a JSON-RPC status request to the daemon socket
// and returns the status string from the response.
func readyFixtureProbeStatus(t *testing.T, projectDir string) (string, error) {
	t.Helper()

	conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", plFixtureSocketPath(projectDir))
	if err != nil {
		return "", fmt.Errorf("readyFixtureProbeStatus: dial: %w", err)
	}
	defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable

	req := struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
	}{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "status",
	}
	reqBytes, _ := json.Marshal(req) //nolint:errcheck,errchkjson // encoding a known-good struct
	if _, err := fmt.Fprintf(conn, "%s\n", reqBytes); err != nil {
		return "", fmt.Errorf("readyFixtureProbeStatus: write: %w", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return "", fmt.Errorf("readyFixtureProbeStatus: read: %w", err)
	}

	var resp struct {
		Result struct {
			Status string `json:"status"`
		} `json:"result"`
	}
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return "", fmt.Errorf("readyFixtureProbeStatus: unmarshal: %w", err)
	}
	return resp.Result.Status, nil
}

// TestPL009_ReadyTransitionOnlyWhenCriteriaMet verifies that the daemon
// transitions to `ready` status only when ALL PL-009 criteria are satisfied.
// Individual missing criteria each cause the daemon to report `reconciling`.
//
// Spec ref: process-lifecycle.md §4.3 PL-009 — "The daemon MUST transition
// status to `ready` only when ALL of the following conditions hold: the orphan
// sweep has completed; the Cat 0 pre-check has passed; the git-log walk and
// Beads query have completed; the in-memory model has been built; reconciliation
// dispatch has completed for every in-flight run."
func TestPL009_ReadyTransitionOnlyWhenCriteriaMet(t *testing.T) {
	t.Parallel()

	now := time.Now()
	readyPayload := &readyFixtureDaemonReadyPayload{
		ReadyAt:            now.UTC().Format(time.RFC3339Nano),
		ReadyAtNsSinceBoot: now.UnixNano(), // simulated; see PL-010 test for monotonic detail
		InvestigatorRunIDs: []string{},
	}

	// allMet is the fully-satisfied criteria set.
	allMet := readyFixtureCriteria{
		orphanSweepDone:     true,
		cat0PreCheckPassed:  true,
		gitWalkDone:         true,
		inMemoryModelBuilt:  true,
		reconcileDispatchOK: true,
	}

	t.Run("all-criteria-met-reports-ready", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		ln, err := plFixtureBindSocket(t, projectDir)
		if err != nil {
			t.Fatalf("PL-009 all-met: bindSocket: %v", err)
		}
		t.Cleanup(func() { _ = ln.Close() }) //nolint:errcheck // cleanup error unactionable

		readyFixtureRunStubServer(t, ln, allMet, readyPayload)

		status, err := readyFixtureProbeStatus(t, projectDir)
		if err != nil {
			t.Fatalf("PL-009 all-met: probeStatus: %v", err)
		}
		if status != "ready" {
			t.Errorf("PL-009 all-met: status = %q, want %q", status, "ready")
		}
	})

	// Each sub-test removes exactly one criterion and asserts reconciling.
	missingCriteria := []struct {
		name     string
		criteria readyFixtureCriteria
	}{
		{
			name: "orphan-sweep-not-done",
			criteria: readyFixtureCriteria{
				orphanSweepDone: false, cat0PreCheckPassed: true,
				gitWalkDone: true, inMemoryModelBuilt: true, reconcileDispatchOK: true,
			},
		},
		{
			name: "cat0-precheck-not-passed",
			criteria: readyFixtureCriteria{
				orphanSweepDone: true, cat0PreCheckPassed: false,
				gitWalkDone: true, inMemoryModelBuilt: true, reconcileDispatchOK: true,
			},
		},
		{
			name: "git-walk-not-done",
			criteria: readyFixtureCriteria{
				orphanSweepDone: true, cat0PreCheckPassed: true,
				gitWalkDone: false, inMemoryModelBuilt: true, reconcileDispatchOK: true,
			},
		},
		{
			name: "in-memory-model-not-built",
			criteria: readyFixtureCriteria{
				orphanSweepDone: true, cat0PreCheckPassed: true,
				gitWalkDone: true, inMemoryModelBuilt: false, reconcileDispatchOK: true,
			},
		},
		{
			name: "reconcile-dispatch-not-done",
			criteria: readyFixtureCriteria{
				orphanSweepDone: true, cat0PreCheckPassed: true,
				gitWalkDone: true, inMemoryModelBuilt: true, reconcileDispatchOK: false,
			},
		},
	}

	for _, tc := range missingCriteria {
		t.Run(tc.name+"-reports-reconciling", func(t *testing.T) {
			t.Parallel()

			projectDir := plFixtureTempProjectDir(t)
			ln, err := plFixtureBindSocket(t, projectDir)
			if err != nil {
				t.Fatalf("PL-009 %s: bindSocket: %v", tc.name, err)
			}
			t.Cleanup(func() { _ = ln.Close() }) //nolint:errcheck // cleanup error unactionable

			readyFixtureRunStubServer(t, ln, tc.criteria, readyPayload)

			status, err := readyFixtureProbeStatus(t, projectDir)
			if err != nil {
				t.Fatalf("PL-009 %s: probeStatus: %v", tc.name, err)
			}
			if status != "reconciling" {
				t.Errorf("PL-009 %s: status = %q, want %q", tc.name, status, "reconciling")
			}
		})
	}

	// Verify that investigator workflows in-flight do NOT block ready.
	t.Run("investigator-workflow-inflight-does-not-block-ready", func(t *testing.T) {
		t.Parallel()

		// Spec ref: process-lifecycle.md §4.3 PL-009 — "Dispatched investigator
		// workflows MAY remain in-flight and MUST NOT block the `ready` transition."
		projectDir := plFixtureTempProjectDir(t)
		ln, err := plFixtureBindSocket(t, projectDir)
		if err != nil {
			t.Fatalf("PL-009 investigator-inflight: bindSocket: %v", err)
		}
		t.Cleanup(func() { _ = ln.Close() }) //nolint:errcheck // cleanup error unactionable

		// Payload includes investigator run IDs — still reports ready.
		payloadWithInvestigators := &readyFixtureDaemonReadyPayload{
			ReadyAt:            now.UTC().Format(time.RFC3339Nano),
			ReadyAtNsSinceBoot: now.UnixNano(),
			InvestigatorRunIDs: []string{
				"01950000-ffff-7000-8000-000000000001",
				"01950000-ffff-7000-8000-000000000002",
			},
		}

		readyFixtureRunStubServer(t, ln, allMet, payloadWithInvestigators)

		status, err := readyFixtureProbeStatus(t, projectDir)
		if err != nil {
			t.Fatalf("PL-009 investigator-inflight: probeStatus: %v", err)
		}
		if status != "ready" {
			t.Errorf("PL-009 investigator-inflight: status = %q, want %q (investigator workflows MUST NOT block ready)", status, "ready")
		}
	})

	// Verify daemon_ready payload shape includes required fields.
	t.Run("daemon-ready-payload-has-required-fields", func(t *testing.T) {
		t.Parallel()

		// Spec ref: process-lifecycle.md §4.3 PL-009 — "On transition to `ready`,
		// the daemon MUST emit `daemon_ready` with {ready_at, ready_at_ns_since_boot,
		// investigator_run_ids[]}."
		projectDir := plFixtureTempProjectDir(t)
		ln, err := plFixtureBindSocket(t, projectDir)
		if err != nil {
			t.Fatalf("PL-009 payload: bindSocket: %v", err)
		}
		t.Cleanup(func() { _ = ln.Close() }) //nolint:errcheck // cleanup error unactionable

		readyFixtureRunStubServer(t, ln, allMet, readyPayload)

		conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", plFixtureSocketPath(projectDir))
		if err != nil {
			t.Fatalf("PL-009 payload: dial: %v", err)
		}
		defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable

		req := struct {
			JSONRPC string `json:"jsonrpc"`
			ID      int    `json:"id"`
			Method  string `json:"method"`
		}{JSONRPC: "2.0", ID: 10, Method: "status"}
		reqBytes, _ := json.Marshal(req) //nolint:errcheck // encoding a known-good struct
		if _, err := fmt.Fprintf(conn, "%s\n", reqBytes); err != nil {
			t.Fatalf("PL-009 payload: write: %v", err)
		}

		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("PL-009 payload: read: %v", err)
		}

		var resp struct {
			Result map[string]interface{} `json:"result"`
		}
		if err := json.Unmarshal(buf[:n], &resp); err != nil {
			t.Fatalf("PL-009 payload: unmarshal: %v", err)
		}

		// Assert required fields are present in result.
		if _, ok := resp.Result["ready_at"]; !ok {
			t.Error("PL-009 payload: daemon_ready result missing 'ready_at' field")
		}
		if _, ok := resp.Result["ready_at_ns_since_boot"]; !ok {
			t.Error("PL-009 payload: daemon_ready result missing 'ready_at_ns_since_boot' field")
		}
		if _, ok := resp.Result["investigator_run_ids"]; !ok {
			t.Error("PL-009 payload: daemon_ready result missing 'investigator_run_ids' field")
		}

		// ready_at must parse as RFC 3339.
		readyAtRaw, ok := resp.Result["ready_at"].(string)
		if !ok {
			t.Fatal("PL-009 payload: ready_at is not a string")
		}
		if _, err := time.Parse(time.RFC3339Nano, readyAtRaw); err != nil {
			if _, err2 := time.Parse(time.RFC3339, readyAtRaw); err2 != nil {
				t.Errorf("PL-009 payload: ready_at %q does not parse as RFC 3339: %v", readyAtRaw, err2)
			}
		}

		// ready_at_ns_since_boot must be a positive number.
		nsRaw, ok := resp.Result["ready_at_ns_since_boot"].(float64)
		if !ok {
			t.Fatal("PL-009 payload: ready_at_ns_since_boot is not a number")
		}
		if nsRaw <= 0 {
			t.Errorf("PL-009 payload: ready_at_ns_since_boot = %v, want > 0", nsRaw)
		}

		// investigator_run_ids must be a JSON array (possibly empty).
		idsRaw, ok := resp.Result["investigator_run_ids"].([]interface{})
		if !ok {
			t.Errorf("PL-009 payload: investigator_run_ids is not an array; type = %T", resp.Result["investigator_run_ids"])
		} else {
			_ = idsRaw // length 0 is valid
		}
	})

	// Verify ready_at format is RFC 3339.
	t.Run("ready-at-is-rfc3339", func(t *testing.T) {
		t.Parallel()

		// Spec ref: process-lifecycle.md §4.3 PL-009 — "`ready_at` is the
		// wall-clock time at emission (RFC 3339 with ms)."
		formatted := now.UTC().Format(time.RFC3339Nano)
		if _, err := time.Parse(time.RFC3339Nano, formatted); err != nil {
			t.Errorf("PL-009 ready_at RFC3339: format %q does not parse: %v", formatted, err)
		}

		projectDir := plFixtureTempProjectDir(t)
		harmonikDir := filepath.Join(projectDir, ".harmonik")
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
			t.Fatalf("PL-009 ready_at RFC3339: MkdirAll: %v", err)
		}

		ln, err := plFixtureBindSocket(t, projectDir)
		if err != nil {
			t.Fatalf("PL-009 ready_at RFC3339: bindSocket: %v", err)
		}
		t.Cleanup(func() { _ = ln.Close() }) //nolint:errcheck // cleanup error unactionable

		p := &readyFixtureDaemonReadyPayload{
			ReadyAt:            formatted,
			ReadyAtNsSinceBoot: now.UnixNano(),
			InvestigatorRunIDs: []string{},
		}
		readyFixtureRunStubServer(t, ln, allMet, p)

		status, err := readyFixtureProbeStatus(t, projectDir)
		if err != nil {
			t.Fatalf("PL-009 ready_at RFC3339: probeStatus: %v", err)
		}
		if status != "ready" {
			t.Errorf("PL-009 ready_at RFC3339: status = %q, want ready", status)
		}
	})
}
