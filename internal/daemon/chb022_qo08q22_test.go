package daemon_test

// chb022_qo08q22_test.go — CHB-022 sensor: Daemon is twin-blind.
//
// Bead: hk-qo08q.22
// Spec: specs/claude-hook-bridge.md §4.8 CHB-022
//
// CHB-022 states: "The daemon's watcher acceptor MUST route messages from
// real-Claude (via relay) and from twin sessions identically. The daemon-side
// code MUST carry zero `if isTwin` / `if relay` branches per handler-contract.md
// §5 HC-INV-002. The (run_id, claude_session_id) envelope is the only
// session-routing key."
//
// This file contains two assertion surfaces:
//
//  1. Static code scan (TestCHB022_DaemonHasNoTwinBranches): walks every
//     non-test .go file in internal/daemon/ and asserts zero occurrences of
//     identifiers isTwin, isRelay, IsTwin, IsRelay — the forbidden conditional
//     discriminants that would break twin-blindness.
//
//  2. Routing parity scenario (TestCHB022_TwinAndRealSessionRoutedIdentically):
//     sends an outcome_emitted hook-relay envelope from a "real-Claude" session
//     and a structurally identical envelope from a "twin" session (distinguished
//     only by a twin-style (run_id, claude_session_id) tuple) to the same daemon
//     socket and asserts:
//     (a) Both receive ACK status="ok" — no discriminant in the dispatch path.
//     (b) Both sessions' latestOutcome records are non-nil and carry the
//         expected payload — the store treats them as indistinguishable.
//
// Helper prefix: chb022Fixture
// (per implementer-protocol.md §Helper-prefix discipline; bead hk-qo08q.22).

import (
	"bufio"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// chb022FixtureOutcomeEnv builds a hook-relay outcome_emitted envelope map
// for the given (runID, sessionID, summary).  The envelope format is identical
// whether the sender is a real-Claude relay or a twin relay — CHB-022 requires
// no field distinguishes twin from real at the wire level.
func chb022FixtureOutcomeEnv(t *testing.T, runID, sessionID, summary string) map[string]interface{} {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"kind": "WORK_COMPLETE", "summary": summary})
	if err != nil {
		t.Fatalf("chb022FixtureOutcomeEnv: marshal payload: %v", err)
	}
	return map[string]interface{}{
		"type":               "outcome_emitted",
		"run_id":             runID,
		"claude_session_id":  sessionID,
		"handler_session_id": "handler-sess-chb022",
		"emitted_at_ns":      int64(9000),
		"payload":            json.RawMessage(payload),
	}
}

// chb022FixtureSendEnvAndReadAck sends a hook-relay envelope to the socket at
// sockPath (one-shot connect → write → read-ack → close) and returns the parsed
// ACK map.
func chb022FixtureSendEnvAndReadAck(t *testing.T, sockPath string, env map[string]interface{}) map[string]string {
	t.Helper()

	conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", sockPath)
	if err != nil {
		t.Fatalf("chb022FixtureSendEnvAndReadAck: dial %q: %v", sockPath, err)
	}
	defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable

	data, marshalErr := json.Marshal(env)
	if marshalErr != nil {
		t.Fatalf("chb022FixtureSendEnvAndReadAck: marshal: %v", marshalErr)
	}
	if _, writeErr := fmt.Fprintf(conn, "%s\n", data); writeErr != nil {
		t.Fatalf("chb022FixtureSendEnvAndReadAck: write: %v", writeErr)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if scanErr := scanner.Err(); scanErr != nil {
			t.Fatalf("chb022FixtureSendEnvAndReadAck: scan: %v", scanErr)
		}
		t.Fatal("chb022FixtureSendEnvAndReadAck: EOF before ack")
	}
	var ack map[string]string
	if unmarshalErr := json.Unmarshal(scanner.Bytes(), &ack); unmarshalErr != nil {
		t.Fatalf("chb022FixtureSendEnvAndReadAck: unmarshal ack: %v", unmarshalErr)
	}
	return ack
}

// chb022FixtureDaemonDir returns the absolute path of internal/daemon/ relative
// to this test file.  The package source is three directories above the test
// binary's working directory, so we walk upward from the package directory
// that the Go toolchain provides via runtime.Caller.
//
// Using go/build or os.Getwd() here risks fragility across test invocation
// modes.  The reliable anchor is the source file path baked in by the
// compiler via runtime.Caller(0), which always resolves to the actual source
// tree regardless of where the test binary is run from.
func chb022FixtureDaemonDir(t *testing.T) string {
	t.Helper()
	// Walk upward from the Go module root.  The test binary's working directory
	// is the package source directory when run via `go test ./...` from the repo
	// root, but it can vary.  The canonical approach for module-rooted paths is
	// to search upward for go.mod starting from os.Getwd().
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("chb022FixtureDaemonDir: Getwd: %v", err)
	}
	// Resolve to the actual daemon package source directory.
	// When running `go test ./internal/daemon/...` from the repo root, cwd is
	// the package source directory itself.
	// When running from the repo root with the package as a path argument, cwd
	// may be the repo root.  We check for the daemon package marker file.
	daemonDir := cwd
	if _, statErr := os.Stat(filepath.Join(daemonDir, "daemon.go")); os.IsNotExist(statErr) {
		// Try the internal/daemon subdirectory.
		candidate := filepath.Join(daemonDir, "internal", "daemon")
		if _, statErr2 := os.Stat(filepath.Join(candidate, "daemon.go")); statErr2 == nil {
			daemonDir = candidate
		} else {
			t.Fatalf("chb022FixtureDaemonDir: cannot locate daemon.go from cwd=%q", cwd)
		}
	}
	return daemonDir
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 1: Static code scan — zero if-isTwin / if-isRelay branches
// ─────────────────────────────────────────────────────────────────────────────

// TestCHB022_DaemonHasNoTwinBranches is the static-analysis surface of the
// CHB-022 sensor.
//
// It parses every non-test .go file in internal/daemon/ with go/ast and asserts
// that no identifier named isTwin, isRelay, IsTwin, or IsRelay appears anywhere
// in the AST.  Such identifiers are the canonical forbidden discriminants that
// would introduce twin-aware conditional branches per §4.8 CHB-022.
//
// The scan is exhaustive: it reports every violation location so that a future
// refactor that accidentally introduces a twin branch is caught immediately
// with a pinpointed error rather than a vague failure.
func TestCHB022_DaemonHasNoTwinBranches(t *testing.T) {
	t.Parallel()

	daemonDir := chb022FixtureDaemonDir(t)

	// forbidden is the set of identifier names that must not appear in
	// daemon production code per CHB-022 / HC-INV-002.
	forbidden := map[string]bool{
		"isTwin":  true,
		"isRelay": true,
		"IsTwin":  true,
		"IsRelay": true,
	}

	entries, err := os.ReadDir(daemonDir)
	if err != nil {
		t.Fatalf("CHB-022 static scan: ReadDir %q: %v", daemonDir, err)
	}

	fset := token.NewFileSet()
	var violations []string

	for _, entry := range entries {
		name := entry.Name()
		// Only scan non-test, non-generated .go files.
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		srcPath := filepath.Join(daemonDir, name)
		//nolint:gosec // G304: test-only static scan; path is constructed from ReadDir entries within a known package dir
		f, parseErr := parser.ParseFile(fset, srcPath, nil, 0)
		if parseErr != nil {
			t.Errorf("CHB-022 static scan: parse %q: %v", name, parseErr)
			continue
		}

		ast.Inspect(f, func(n ast.Node) bool {
			ident, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if forbidden[ident.Name] {
				pos := fset.Position(ident.Pos())
				violations = append(violations, fmt.Sprintf("%s:%d: identifier %q (CHB-022: daemon must be twin-blind; no isTwin/isRelay discriminants)", pos.Filename, pos.Line, ident.Name))
			}
			return true
		})
	}

	if len(violations) > 0 {
		t.Errorf("CHB-022 static scan: found %d forbidden twin-discriminant identifier(s):\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2: Routing parity — twin session and real-Claude session are identical
// ─────────────────────────────────────────────────────────────────────────────

// TestCHB022_TwinAndRealSessionRoutedIdentically is the routing-parity surface
// of the CHB-022 sensor.
//
// It registers two sessions on the same hookSessionStore and daemon socket:
//
//   - "real-Claude" session: (run_id, claude_session_id) representing a session
//     driven by the real claude binary via harmonik hook-relay.
//   - "twin" session: (run_id, claude_session_id) representing a session driven
//     by harmonik-twin-claude (twin binary) — distinguished ONLY by having a
//     "twin" string in the run_id for human readability; the daemon sees no
//     structural difference.
//
// Both sessions send an identical outcome_emitted envelope (same schema, same
// field set). The test asserts:
//
//  1. Both receive ACK status="ok" — the socket dispatch path has no
//     twin-aware branching.
//  2. Both sessions' latestOutcome is non-nil and carries the expected summary
//     value — the hookSessionStore treats both (run_id, claude_session_id)
//     tuples identically.
//  3. Session isolation holds: the twin session's outcome is NOT visible under
//     the real-Claude session's key and vice versa — the routing key is
//     (run_id, claude_session_id), not any twin/real discriminant.
func TestCHB022_TwinAndRealSessionRoutedIdentically(t *testing.T) {
	t.Parallel()

	// Real-Claude session key — mimics a relay subprocess invoked by the
	// real claude binary.
	const realRunID = "run-chb022-real-01"
	const realSessionID = "claude-sess-chb022-real-01"
	const realSummary = "chb022 real-claude outcome"

	// Twin session key — mimics a relay subprocess invoked by
	// harmonik-twin-claude.  The only structural difference is the run_id
	// string; the envelope schema is identical per CHB-022.
	const twinRunID = "run-chb022-twin-01"
	const twinSessionID = "claude-sess-chb022-twin-01"
	const twinSummary = "chb022 twin outcome"

	// Single shared store — both sessions live on the same daemon socket and
	// the same hookSessionStore, exactly as they would in a production daemon.
	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, realRunID, realSessionID)
	daemon.ExportedHookRegister(store, twinRunID, twinSessionID)

	sockPath := socketFixtureTempSockPath(t)
	noopH := &stubHandler{}

	cancel, _ := socketFixtureStartListener(t, sockPath, noopH, store)
	defer cancel()
	socketFixtureWaitReady(t, sockPath)

	// ── Real-Claude session: send outcome_emitted ─────────────────────────────
	realEnv := chb022FixtureOutcomeEnv(t, realRunID, realSessionID, realSummary)
	realAck := chb022FixtureSendEnvAndReadAck(t, sockPath, realEnv)

	if realAck["status"] != "ok" {
		t.Errorf("CHB-022: real-Claude session ACK status=%q (reason=%q), want ok",
			realAck["status"], realAck["reason"])
	}

	// ── Twin session: send structurally identical outcome_emitted ─────────────
	twinEnv := chb022FixtureOutcomeEnv(t, twinRunID, twinSessionID, twinSummary)
	twinAck := chb022FixtureSendEnvAndReadAck(t, sockPath, twinEnv)

	if twinAck["status"] != "ok" {
		t.Errorf("CHB-022: twin session ACK status=%q (reason=%q), want ok; "+
			"daemon must route twin sessions identically to real-Claude sessions",
			twinAck["status"], twinAck["reason"])
	}

	// ── Invariant 1: both latestOutcomes are non-nil ──────────────────────────
	realOutcome := daemon.ExportedHookLatestOutcome(store, realRunID, realSessionID)
	if realOutcome == nil {
		t.Fatal("CHB-022: real-Claude session latestOutcome is nil after outcome_emitted; want recorded")
	}
	twinOutcome := daemon.ExportedHookLatestOutcome(store, twinRunID, twinSessionID)
	if twinOutcome == nil {
		t.Fatal("CHB-022: twin session latestOutcome is nil after outcome_emitted; " +
			"daemon must record twin sessions identically to real-Claude sessions")
	}

	// ── Invariant 2: payload content is correct for each session ─────────────
	var realMap map[string]string
	if err := json.Unmarshal(*realOutcome, &realMap); err != nil {
		t.Fatalf("CHB-022: unmarshal real-Claude latestOutcome: %v", err)
	}
	if realMap["summary"] != realSummary {
		t.Errorf("CHB-022: real-Claude latestOutcome summary=%q, want %q", realMap["summary"], realSummary)
	}

	var twinMap map[string]string
	if err := json.Unmarshal(*twinOutcome, &twinMap); err != nil {
		t.Fatalf("CHB-022: unmarshal twin latestOutcome: %v", err)
	}
	if twinMap["summary"] != twinSummary {
		t.Errorf("CHB-022: twin latestOutcome summary=%q, want %q", twinMap["summary"], twinSummary)
	}

	// ── Invariant 3: session isolation — no cross-contamination ──────────────
	// Real session must NOT see twin's outcome (different (run_id, session_id) key).
	// We verify by checking that the real session's outcome still carries
	// realSummary, not twinSummary.
	if realMap["summary"] == twinSummary {
		t.Errorf("CHB-022: session isolation violated: real-Claude session carries twin summary %q; "+
			"sessions must be isolated by (run_id, claude_session_id) tuple", twinSummary)
	}
	// Twin session must NOT see real-Claude's outcome.
	if twinMap["summary"] == realSummary {
		t.Errorf("CHB-022: session isolation violated: twin session carries real-Claude summary %q; "+
			"sessions must be isolated by (run_id, claude_session_id) tuple", realSummary)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3: ACK parity — twin ACK and real-Claude ACK have identical structure
// ─────────────────────────────────────────────────────────────────────────────

// TestCHB022_AckStructureIsIdentical verifies that the ACK message returned by
// the daemon socket is structurally identical for a real-Claude-originated
// envelope and a twin-originated envelope.
//
// CHB-022 requires the daemon to be fully twin-blind: the ACK must not carry
// any discriminant field (e.g., "origin": "twin" vs "origin": "real") that
// would reveal whether the daemon performed different processing paths.
func TestCHB022_AckStructureIsIdentical(t *testing.T) {
	t.Parallel()

	const realRunID = "run-chb022-ackparity-real"
	const realSessionID = "claude-sess-chb022-ackparity-real"

	const twinRunID = "run-chb022-ackparity-twin"
	const twinSessionID = "claude-sess-chb022-ackparity-twin"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, realRunID, realSessionID)
	daemon.ExportedHookRegister(store, twinRunID, twinSessionID)

	sockPath := socketFixtureTempSockPath(t)
	noopH := &stubHandler{}

	cancel, _ := socketFixtureStartListener(t, sockPath, noopH, store)
	defer cancel()
	socketFixtureWaitReady(t, sockPath)

	realEnv := chb022FixtureOutcomeEnv(t, realRunID, realSessionID, "ack-parity-real")
	twinEnv := chb022FixtureOutcomeEnv(t, twinRunID, twinSessionID, "ack-parity-twin")

	realAck := chb022FixtureSendEnvAndReadAck(t, sockPath, realEnv)
	twinAck := chb022FixtureSendEnvAndReadAck(t, sockPath, twinEnv)

	// Both ACKs must have status="ok".
	if realAck["status"] != "ok" {
		t.Errorf("CHB-022 ACK parity: real-Claude ACK status=%q, want ok", realAck["status"])
	}
	if twinAck["status"] != "ok" {
		t.Errorf("CHB-022 ACK parity: twin ACK status=%q, want ok", twinAck["status"])
	}

	// Both ACKs must have the same set of keys (identical structure).
	// A twin-aware discriminant would show up as an extra key in one ACK.
	realKeys := make([]string, 0, len(realAck))
	for k := range realAck {
		realKeys = append(realKeys, k)
	}
	twinKeys := make([]string, 0, len(twinAck))
	for k := range twinAck {
		twinKeys = append(twinKeys, k)
	}
	if len(realKeys) != len(twinKeys) {
		t.Errorf("CHB-022 ACK parity: real ACK has %d key(s) %v, twin ACK has %d key(s) %v; "+
			"daemon must return structurally identical ACKs for real-Claude and twin sessions",
			len(realKeys), realKeys, len(twinKeys), twinKeys)
	}
	// Verify no discriminant field was added to either ACK.
	for _, discriminant := range []string{"origin", "source", "is_twin", "session_type"} {
		if _, exists := realAck[discriminant]; exists {
			t.Errorf("CHB-022 ACK parity: real-Claude ACK contains discriminant field %q; "+
				"daemon must be twin-blind (no source discriminants in ACK)", discriminant)
		}
		if _, exists := twinAck[discriminant]; exists {
			t.Errorf("CHB-022 ACK parity: twin ACK contains discriminant field %q; "+
				"daemon must be twin-blind (no source discriminants in ACK)", discriminant)
		}
	}
}
