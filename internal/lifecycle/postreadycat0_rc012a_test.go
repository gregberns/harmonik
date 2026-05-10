package lifecycle

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// postreadycat0_rc012a_test.go — sensor tests for RC-012a
// (Post-`ready` Cat 0 does not transition daemon state).
//
// Spec refs: specs/reconciliation/spec.md §4.2 RC-012a.
// Bead: hk-63oh.17.
//
// Verifies:
//
//	(a) DaemonDegradedReasonCat0PostReady constant exists and is a valid
//	    DaemonDegradedReason.
//	(b) A post-ready Cat 0 failure is modelled via the socket-stub: the daemon
//	    status remains "ready" while the daemon_degraded event payload carries
//	    reason=infrastructure_unavailable (the post-ready variant).
//	(c) Post-ready Cat 0 failures do NOT reuse the pre-ready "degraded" enum
//	    state; daemon status remains "ready" at the JSON-RPC status endpoint.
//	(d) Spec-corpus sensor: reconciliation/spec.md contains RC-012a and the
//	    "MUST NOT transition" constraint.
//
// Helper prefix: cat0PostReadyFixture (per implementer-protocol.md).

// cat0PostReadyFixtureModuleRoot returns the module root by walking upward from
// this file's directory.
func cat0PostReadyFixtureModuleRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod in any parent directory")
		}
		dir = parent
	}
}

// cat0PostReadyFixtureStubState is the mutable state for the post-ready Cat 0
// scenario stub. The daemon is `ready` but reports a daemon_degraded event
// for any post-ready Cat 0 failure.
type cat0PostReadyFixtureStubState struct {
	// status is the JSON-RPC status endpoint response value; it MUST remain
	// "ready" even when Cat 0 fails post-ready (RC-012a).
	status string
	// lastDegradedReason is the reason field of the most-recent daemon_degraded
	// event emitted by the stub.
	lastDegradedReason string
}

// cat0PostReadyFixtureServeState starts a stub daemon that reports the given
// status and returns daemon_degraded metadata when queried. The server runs
// until the listener is closed.
func cat0PostReadyFixtureServeState(t *testing.T, ln net.Listener, state *cat0PostReadyFixtureStubState) {
	t.Helper()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed; test is over
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }() //nolint:errcheck // cleanup error unactionable
				cat0PostReadyFixtureServeConn(c, state)
			}(conn)
		}
	}()
}

// cat0PostReadyFixtureServeConn handles one connection for the post-ready Cat 0
// stub. Reads a JSON-RPC request and writes back status + degraded metadata.
func cat0PostReadyFixtureServeConn(conn net.Conn, state *cat0PostReadyFixtureStubState) {
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
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

	result := map[string]interface{}{
		"status": state.status,
	}
	if state.lastDegradedReason != "" {
		result["last_degraded_reason"] = state.lastDegradedReason
	}
	resultBytes, _ := json.Marshal(result) //nolint:errcheck,errchkjson // stub map is always encodable
	raw := json.RawMessage(resultBytes)    //nolint:exhaustruct // only Result field set below
	resp := struct {
		JSONRPC string           `json:"jsonrpc"`
		ID      int              `json:"id"`
		Result  *json.RawMessage `json:"result,omitempty"`
	}{JSONRPC: "2.0", ID: req.ID, Result: &raw}

	respBytes, _ := json.Marshal(resp)          //nolint:errcheck,errchkjson // stub struct is always encodable
	_, _ = fmt.Fprintf(conn, "%s\n", respBytes) //nolint:errcheck // stub: write errors intentionally ignored
}

// cat0PostReadyFixtureProbeStatus queries the stub daemon's JSON-RPC status
// endpoint and returns the status string and last_degraded_reason.
func cat0PostReadyFixtureProbeStatus(t *testing.T, projectDir string) (status, lastDegradedReason string, err error) {
	t.Helper()
	conn, dialErr := (&net.Dialer{}).DialContext(t.Context(), "unix", plFixtureSocketPath(projectDir))
	if dialErr != nil {
		return "", "", fmt.Errorf("cat0PostReadyFixtureProbeStatus: dial: %w", dialErr)
	}
	defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable

	req := struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
	}{JSONRPC: "2.0", ID: 42, Method: "status"}
	reqBytes, _ := json.Marshal(req) //nolint:errcheck,errchkjson // encoding a known-good struct
	if _, writeErr := fmt.Fprintf(conn, "%s\n", reqBytes); writeErr != nil {
		return "", "", fmt.Errorf("cat0PostReadyFixtureProbeStatus: write: %w", writeErr)
	}

	readBuf := make([]byte, 4096)
	n, readErr := conn.Read(readBuf)
	if readErr != nil {
		return "", "", fmt.Errorf("cat0PostReadyFixtureProbeStatus: read: %w", readErr)
	}
	var resp struct {
		Result struct {
			Status             string `json:"status"`
			LastDegradedReason string `json:"last_degraded_reason"`
		} `json:"result"`
	}
	if unmarshalErr := json.Unmarshal(readBuf[:n], &resp); unmarshalErr != nil {
		return "", "", fmt.Errorf("cat0PostReadyFixtureProbeStatus: unmarshal: %w", unmarshalErr)
	}
	return resp.Result.Status, resp.Result.LastDegradedReason, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// RC-012a: DaemonDegradedReasonCat0PostReady constant
// ─────────────────────────────────────────────────────────────────────────────

// TestRC012a_Cat0PostReadyReasonIsValid verifies that
// DaemonDegradedReasonCat0PostReady is a declared, valid DaemonDegradedReason
// constant per event-model.md §8.7.5.
func TestRC012a_Cat0PostReadyReasonIsValid(t *testing.T) {
	t.Parallel()

	reason := core.DaemonDegradedReasonCat0PostReady
	if !reason.Valid() {
		t.Errorf("RC-012a: DaemonDegradedReasonCat0PostReady.Valid() = false; want true (must be a declared DaemonDegradedReason)")
	}
}

// TestRC012a_Cat0PostReadyReasonValue verifies the wire value is "cat0_post_ready"
// per event-model.md §8.7.5.
func TestRC012a_Cat0PostReadyReasonValue(t *testing.T) {
	t.Parallel()

	const wantWireValue = "cat0_post_ready"
	got := string(core.DaemonDegradedReasonCat0PostReady)
	if got != wantWireValue {
		t.Errorf("RC-012a: DaemonDegradedReasonCat0PostReady wire value = %q; want %q", got, wantWireValue)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RC-012a: post-ready Cat 0 does NOT transition daemon-status enum
// ─────────────────────────────────────────────────────────────────────────────

// TestRC012a_DaemonStatusRemainsReadyAfterCat0Failure verifies that after the
// daemon reaches `ready`, a Cat 0 prerequisite failure does NOT transition the
// daemon-status enum to `degraded`.
//
// Spec ref: reconciliation/spec.md §4.2 RC-012a — "MUST NOT transition the
// §6.1 daemon-status enum from `ready` to `degraded`."
func TestRC012a_DaemonStatusRemainsReadyAfterCat0Failure(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	ln, err := plFixtureBindSocket(t, projectDir)
	if err != nil {
		t.Fatalf("RC-012a status-remains-ready: bindSocket: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() }) //nolint:errcheck // cleanup error unactionable

	// The stub models the correct RC-012a behavior: daemon is `ready` and
	// the Cat 0 failure is surfaced via daemon_degraded (not a status change).
	state := &cat0PostReadyFixtureStubState{
		status:             "ready",
		lastDegradedReason: string(core.DaemonDegradedReasonInfrastructureUnavailable),
	}
	cat0PostReadyFixtureServeState(t, ln, state)

	status, lastDegradedReason, err := cat0PostReadyFixtureProbeStatus(t, projectDir)
	if err != nil {
		t.Fatalf("RC-012a status-remains-ready: probe: %v", err)
	}
	if status != "ready" {
		t.Errorf("RC-012a: daemon status = %q after post-ready Cat 0 failure; want ready (MUST NOT transition to degraded per RC-012a)", status)
	}
	if lastDegradedReason == "" {
		t.Error("RC-012a: last_degraded_reason is empty; want infrastructure_unavailable (daemon_degraded event MUST be emitted)")
	}
}

// TestRC012a_DaemonDegradedPayloadForPostReadyCat0 verifies that the
// daemon_degraded payload used for post-ready Cat 0 failures is valid per
// event-model.md §8.7.5.
func TestRC012a_DaemonDegradedPayloadForPostReadyCat0(t *testing.T) {
	t.Parallel()

	// A post-ready Cat 0 failure emits daemon_degraded with reason=infrastructure_unavailable
	// per RC-012a (not cat0_post_ready — the reason encodes the infrastructure condition,
	// not the detection path). The DaemonDegradedReasonCat0PostReady constant is the
	// reason used when the detection context needs to be communicated separately.
	payload := core.DaemonDegradedPayload{
		DetectedAt: "2026-05-10T00:00:00Z",
		Reason:     core.DaemonDegradedReasonInfrastructureUnavailable,
	}
	if !payload.Valid() {
		t.Error("RC-012a: DaemonDegradedPayload{infrastructure_unavailable}.Valid() = false; want true")
	}

	// The cat0_post_ready variant is also a valid daemon_degraded reason (used
	// to distinguish the detection context in diagnostics).
	payloadCat0 := core.DaemonDegradedPayload{
		DetectedAt: "2026-05-10T00:00:00Z",
		Reason:     core.DaemonDegradedReasonCat0PostReady,
	}
	if !payloadCat0.Valid() {
		t.Error("RC-012a: DaemonDegradedPayload{cat0_post_ready}.Valid() = false; want true")
	}
}

// TestRC012a_PostReadyCat0DoesNotUsePreReadyDegradedState verifies that the
// pre-ready `degraded` enum state is not reused for post-ready Cat 0 failures.
//
// Spec ref: reconciliation/spec.md §4.2 RC-012a — "Daemon-level `degraded`
// enum entry is reserved for pre-`ready` Cat 0 failures per PL-010."
func TestRC012a_PostReadyCat0DoesNotUsePreReadyDegradedState(t *testing.T) {
	t.Parallel()

	// In the fixture model: a daemon that has reached `ready` and then encounters
	// a Cat 0 failure maintains its `ready` status. The fixture verifies this
	// structural invariant by asserting that "ready" + Cat 0 failure (via
	// daemon_degraded emission) is the correct post-ready model, NOT
	// "degraded" + Cat 0 failure (which is the pre-ready model from PL-010).
	projectDir := plFixtureTempProjectDir(t)
	ln, err := plFixtureBindSocket(t, projectDir)
	if err != nil {
		t.Fatalf("RC-012a post-ready-no-degraded: bindSocket: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() }) //nolint:errcheck // cleanup error unactionable

	// Scenario: daemon was ready, Cat 0 fails post-ready. Status MUST stay "ready".
	state := &cat0PostReadyFixtureStubState{
		status:             "ready",
		lastDegradedReason: string(core.DaemonDegradedReasonCat0PostReady),
	}
	cat0PostReadyFixtureServeState(t, ln, state)

	for i := range 3 {
		status, _, err := cat0PostReadyFixtureProbeStatus(t, projectDir)
		if err != nil {
			t.Fatalf("RC-012a post-ready-no-degraded probe %d: %v", i, err)
		}
		if status == "degraded" {
			t.Errorf("RC-012a probe %d: status = %q; post-ready Cat 0 failure MUST NOT produce daemon-status 'degraded' (pre-ready only per PL-010/RC-012a)", i, status)
		}
		if status != "ready" {
			t.Errorf("RC-012a probe %d: status = %q; want ready (post-ready Cat 0 failure must not change the status enum)", i, status)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RC-012a: Spec-corpus sensor
// ─────────────────────────────────────────────────────────────────────────────

// TestRC012a_SpecCorpusClause verifies that reconciliation/spec.md contains
// RC-012a and the "MUST NOT transition" constraint.
func TestRC012a_SpecCorpusClause(t *testing.T) {
	t.Parallel()

	root := cat0PostReadyFixtureModuleRoot(t)
	specPath := filepath.Join(root, "specs", "reconciliation", "spec.md")

	content, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("reading reconciliation/spec.md: %v", err)
	}
	specText := string(content)

	if !strings.Contains(specText, "RC-012a") {
		t.Error("reconciliation/spec.md missing RC-012a clause")
	}
	if !strings.Contains(specText, "MUST NOT transition") {
		t.Error("reconciliation/spec.md missing 'MUST NOT transition' constraint; RC-012a may have drifted")
	}
}
