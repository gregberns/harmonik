package main_test

// e2e_chb021_test.go — end-to-end test for harmonik-twin-claude per
// specs/claude-hook-bridge.md §4.8.CHB-021 (twin-parity) and §4.8.CHB-022
// (daemon is twin-blind).
//
// # What this file tests
//
// 1. E2E scenario=single-happy-path: builds the twin binary, runs it via
//    handler.Launch (so the daemon's Watcher reads its stdout, exactly as it
//    would for a real claude subprocess), and asserts the Watcher observes the
//    complete expected event sequence.
//
// 2. Twin-parity smoke: runs the single-happy-path scenario against a
//    bytes.Buffer (bypassing the subprocess) and asserts the NDJSON byte
//    sequence matches the expected message-type sequence derived from CHB-018/020.
//
// 3. CHB-022 guard: the handler.Handler and handlercontract.Watcher used in
//    the E2E test carry zero "if isTwin" branches (they are the production
//    types); the test itself is the conformance proof.
//
// # Test helper prefix
//
// chbE2EFixture (per implementer-protocol.md §Helper-prefix discipline;
// bead hk-w5vra.2).
//
// Cite: specs/claude-hook-bridge.md §4.8.CHB-021, §4.8.CHB-022;
// specs/handler-contract.md §4.3.HC-011, §4.8.HC-036.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// chbE2EFixtureBuildBinary builds the harmonik-twin-claude binary into a temp
// directory and returns its absolute path.
//
// The binary is built without the ldflags commit-hash stamp (unstamped build):
// tests do not go through VerifyTwinLaunch, which requires the stamp.
func chbE2EFixtureBuildBinary(t *testing.T) string {
	t.Helper()
	outDir := t.TempDir()
	binPath := filepath.Join(outDir, "harmonik-twin-claude")

	// Resolve the module root: walk up from the test file until go.mod is found.
	// In the worktree the module root is at the repo root (one level above cmd/).
	// Use exec.LookPath("go") to locate the Go toolchain (avoids hardcoding path).
	goTool, lookErr := exec.LookPath("go")
	if lookErr != nil {
		t.Skipf("chbE2EFixtureBuildBinary: 'go' not found in PATH; skipping E2E test: %v", lookErr)
		return ""
	}

	// Find module root: the directory containing go.mod relative to this test.
	// This test file lives at cmd/harmonik-twin-claude/ so the module root is
	// two directories up.  Use runtime.Caller for robustness.
	// Since we're in package main_test at cmd/harmonik-twin-claude/, we find
	// the go.mod by resolving the module path via 'go env GOMOD'.
	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		t.Fatalf("chbE2EFixtureBuildBinary: getwd: %v", cwdErr)
	}

	goModCmd := exec.CommandContext(t.Context(), goTool, "env", "GOMOD") //nolint:gosec // goTool from LookPath
	goModCmd.Dir = cwd

	goModOut, goModErr := goModCmd.Output()
	if goModErr != nil {
		t.Skipf("chbE2EFixtureBuildBinary: go env GOMOD: %v; skipping E2E test", goModErr)
		return ""
	}
	moduleRoot := filepath.Dir(strings.TrimSpace(string(goModOut)))

	// Build the binary from the module root using the package import path.
	pkgPath := "github.com/gregberns/harmonik/cmd/harmonik-twin-claude"
	buildCmd := exec.CommandContext(t.Context(), goTool, "build", "-o", binPath, pkgPath) //nolint:gosec // goTool from LookPath
	buildCmd.Dir = moduleRoot
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	if out, buildErr := buildCmd.CombinedOutput(); buildErr != nil {
		t.Fatalf("chbE2EFixtureBuildBinary: build failed: %v\n%s", buildErr, out)
	}
	return binPath
}

// chbE2EFixtureInitGitRepo creates a temp directory, runs git init + baseline
// commit, and returns the directory path. Used by scenarios that need a real
// git worktree (e.g., commit-on-cue-startup-delay).
func chbE2EFixtureInitGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git with controlled args; test helper
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=chb-e2e-test",
			"GIT_AUTHOR_EMAIL=test@harmonik.local",
			"GIT_COMMITTER_NAME=chb-e2e-test",
			"GIT_COMMITTER_EMAIL=test@harmonik.local",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "-b", "main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "chb-e2e-test")

	baselinePath := filepath.Join(dir, ".gitkeep")
	if err := os.WriteFile(baselinePath, []byte("baseline\n"), 0o600); err != nil {
		t.Fatalf("chbE2EFixtureInitGitRepo: write baseline: %v", err)
	}
	run("add", ".gitkeep")
	run("commit", "-m", "baseline")

	return dir
}

// chbE2EFixtureHandler constructs a handler.Handler with a CollectingEmitter
// and NoopWatcherDeadLetter for E2E test use.
func chbE2EFixtureHandler(t *testing.T) (handler.Handler, *handlercontract.CollectingEmitter) {
	t.Helper()
	pub := &handlercontract.CollectingEmitter{}
	dl := handlercontract.NoopWatcherDeadLetter{}
	reg := handlercontract.NewAdapterRegistry()
	h := handler.NewHandler(pub, dl, reg)
	return h, pub
}

// chbE2EFixtureSingleHappyPathExpectedTypes returns the ordered event-type
// sequence the watcher MUST observe for scenario=single-happy-path per
// CHB-018/020 + HC-009/010/049/039 ordering.
//
// Note: the watcher's knownProgressMsgTypes filter drops unknown types; the
// types listed here are the subset the watcher will actually publish.
func chbE2EFixtureSingleHappyPathExpectedTypes() []string {
	return []string{
		"handler_capabilities",
		"session_log_location",
		"skills_provisioned",
		"agent_ready",
		"agent_started",
		"agent_heartbeat",
		"agent_heartbeat",
		"agent_output_chunk",
		"outcome_emitted",
		"agent_completed",
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// E2E test 1: single-happy-path via handler.Launch (CHB-021 primary assertion)
// ─────────────────────────────────────────────────────────────────────────────

// TestCHB021_E2E_SingleHappyPath wires handler.NewHandler + handler.Launch with
// the built harmonik-twin-claude binary (scenario=single-happy-path) and asserts
// the Watcher observes the complete expected event sequence.
//
// This test is the CHB-022 conformance proof: handler.Handler and
// handlercontract.Watcher carry zero "if isTwin" branches; the twin binary
// produces the same observable outcome as a real claude subprocess would.
//
// Cite: specs/claude-hook-bridge.md §4.8.CHB-021, §4.8.CHB-022; §10.
func TestCHB021_E2E_SingleHappyPath(t *testing.T) {
	t.Parallel()

	binPath := chbE2EFixtureBuildBinary(t)
	h, pub := chbE2EFixtureHandler(t)

	spec := handler.LaunchSpec{
		Binary:  binPath,
		Args:    []string{"--scenario", "single-happy-path"},
		Env:     []string{},
		WorkDir: t.TempDir(),
		Role:    "implementer",
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	sess, watcher, err := h.Launch(ctx, spec)
	if err != nil {
		t.Fatalf("handler.Launch: %v", err)
	}

	// Wait for the watcher to drain all output (subprocess stdout EOF → watcher done).
	select {
	case <-watcher.Done():
	case <-ctx.Done():
		t.Fatalf("context cancelled before watcher finished: %v", ctx.Err())
	}

	if watcherErr := watcher.Err(); watcherErr != nil {
		t.Errorf("watcher.Err(): expected nil (clean exit), got %v", watcherErr)
	}

	if err := sess.Wait(ctx); err != nil {
		t.Errorf("Session.Wait: %v", err)
	}

	// Assert the observed event types match the expected sequence (CHB-021).
	got := pub.EventTypes()
	want := chbE2EFixtureSingleHappyPathExpectedTypes()

	if len(got) == 0 {
		t.Fatal("publisher received no events; twin binary produced no output")
	}

	// Verify all expected types appear in order (sequence prefix check).
	// Use an index to walk the want list against the got list, tolerating any
	// additional events the bus may emit (run_started etc.) not in the progress
	// stream.
	wi := 0
	for _, gotType := range got {
		if wi >= len(want) {
			break
		}
		if gotType == want[wi] {
			wi++
		}
	}
	if wi < len(want) {
		t.Errorf("CHB-021 E2E: expected event sequence not observed\n  want: %v\n  got:  %v\n  matched %d of %d", want, got, wi, len(want))
	}

	// Assert agent_completed is the last known-type progress-stream event (CHB-020).
	lastProgressType := ""
	for _, et := range got {
		if isKnownProgressType(et) {
			lastProgressType = et
		}
	}
	if lastProgressType != "agent_completed" {
		t.Errorf("CHB-021 E2E: last progress-stream event = %q, want agent_completed (CHB-020 terminal-event obligation)", lastProgressType)
	}
}

// isKnownProgressType reports whether et is one of the 12 known progress-stream
// message types per specs/handler-contract.md §4.2.HC-007.
func isKnownProgressType(et string) bool {
	switch et {
	case "handler_capabilities", "session_log_location", "skills_provisioned",
		"agent_ready", "agent_started", "agent_output_chunk",
		"agent_completed", "agent_failed",
		"agent_rate_limited", "agent_rate_limit_cleared",
		"agent_heartbeat", "outcome_emitted":
		return true
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// E2E test 2: twin-parity smoke (CHB-021 wire-format assertion)
// ─────────────────────────────────────────────────────────────────────────────

// TestCHB021_TwinParity_SingleHappyPath verifies that the single-happy-path
// scenario emits the expected NDJSON byte sequence (modulo timestamps and
// Claude-content payload fields) when run against a bytes.Buffer.
//
// This test constructs the canned scenario in-process (calling cannedScenario
// from the main package is not possible in _test external package; instead it
// drives the built binary with --scenario and captures its stdout).
//
// Wire-bytes comparison: we compare "type" fields in emission order, tolerating
// dynamic timestamp values, matching CHB-021 §10 "identical progress-stream byte
// sequences (modulo timestamp fields and Claude transcript-text payload contents)".
//
// Cite: specs/claude-hook-bridge.md §4.8.CHB-021; §10.
func TestCHB021_TwinParity_SingleHappyPath(t *testing.T) {
	t.Parallel()

	binPath := chbE2EFixtureBuildBinary(t)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	// Run the twin binary and capture its stdout.
	cmd := exec.CommandContext(ctx, binPath, "--scenario", "single-happy-path") //nolint:gosec // binPath from build
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	cmd.Env = []string{}
	cmd.Dir = t.TempDir()

	if err := cmd.Run(); err != nil {
		t.Fatalf("twin binary exited with error: %v", err)
	}

	// Parse the NDJSON output into type-sequence.
	gotTypes := chbE2EFixtureParseNDJSONTypes(t, &stdout)

	wantTypes := chbE2EFixtureSingleHappyPathExpectedTypes()

	if len(gotTypes) != len(wantTypes) {
		t.Errorf("CHB-021 twin-parity: got %d NDJSON lines, want %d\n  got:  %v\n  want: %v",
			len(gotTypes), len(wantTypes), gotTypes, wantTypes)
		return
	}
	for i, want := range wantTypes {
		if gotTypes[i] != want {
			t.Errorf("CHB-021 twin-parity: line %d type = %q, want %q", i, gotTypes[i], want)
		}
	}
}

// chbE2EFixtureParseNDJSONTypes parses all NDJSON lines from buf and returns
// the "type" field value from each JSON object.  Skips blank lines.
func chbE2EFixtureParseNDJSONTypes(t *testing.T, buf *bytes.Buffer) []string {
	t.Helper()
	var types []string
	scanner := bufio.NewScanner(buf)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(line, &obj); err != nil {
			t.Errorf("chbE2EFixtureParseNDJSONTypes: unmarshal line %q: %v", string(line), err)
			continue
		}
		var typStr string
		if raw, ok := obj["type"]; ok {
			_ = json.Unmarshal(raw, &typStr)
		}
		if typStr != "" {
			types = append(types, typStr)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Errorf("chbE2EFixtureParseNDJSONTypes: scanner error: %v", err)
	}
	return types
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario flag smoke tests (hk-w5vra.2 — all 5 scenarios must be recognised)
// ─────────────────────────────────────────────────────────────────────────────

// TestCHB021_AllScenariosRecognised verifies that every conformance-class
// scenario name is accepted by the twin binary (exit 0) and produces at least
// one NDJSON line on stdout.
//
// Cite: specs/claude-hook-bridge.md §10 "Scenario tests MUST cover...".
func TestCHB021_AllScenariosRecognised(t *testing.T) {
	t.Parallel()

	binPath := chbE2EFixtureBuildBinary(t)

	// scenarioSpec carries the args for each scenario. Most scenarios run without
	// extra flags; commit-on-cue-startup-delay requires --worktree-path pointing
	// to a git repo because it includes a commit_on_cue step.
	type scenarioSpec struct {
		name         string
		extraArgssFn func(t *testing.T) []string // returns additional flags; nil = no extras
		// needsEnv: when true, pass os.Environ() to the subprocess rather than
		// an empty env. Required for scenarios that spawn git subprocesses.
		needsEnv bool
	}

	scenarios := []scenarioSpec{
		{name: "single-happy-path"},
		{name: "review-loop-3iter"},
		{name: "rate-limit"},
		{name: "dial-failed"},
		{name: "daemon-not-ready-retry"},
		{name: "partial-pre-exec"},
		{
			name:     "commit-on-cue-startup-delay",
			needsEnv: true, // commit_on_cue spawns git; needs PATH
			extraArgssFn: func(t *testing.T) []string {
				t.Helper()
				return []string{"--worktree-path", chbE2EFixtureInitGitRepo(t)}
			},
		},
	}

	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			args := []string{"--scenario", sc.name}
			if sc.extraArgssFn != nil {
				args = append(args, sc.extraArgssFn(t)...)
			}

			cmd := exec.CommandContext(ctx, binPath, args...) //nolint:gosec // binPath from build
			var stdout bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = os.Stderr
			if sc.needsEnv {
				cmd.Env = os.Environ()
			} else {
				cmd.Env = []string{}
			}
			cmd.Dir = t.TempDir()

			if err := cmd.Run(); err != nil {
				t.Errorf("scenario %q: twin exited with error: %v", sc.name, err)
				return
			}

			types := chbE2EFixtureParseNDJSONTypes(t, &stdout)
			if len(types) == 0 {
				t.Errorf("scenario %q: twin produced no NDJSON output", sc.name)
			}
		})
	}
}

// TestCHB021_UnknownScenarioExitsOne verifies that an unrecognised --scenario
// value causes the twin to exit 1 with a diagnostic on stderr.
func TestCHB021_UnknownScenarioExitsOne(t *testing.T) {
	t.Parallel()

	binPath := chbE2EFixtureBuildBinary(t)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, "--scenario", "this-does-not-exist") //nolint:gosec // binPath from build
	cmd.Env = []string{}
	cmd.Dir = t.TempDir()

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for unknown scenario, got exit 0")
	}
	// exec.ExitError wraps the non-zero exit code.
	var exitErr *exec.ExitError
	if !isExitError(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Errorf("expected exit code 1, got: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// E2E test 3: relay-failure (dial-failed) scenario (hk-pcgms)
// ─────────────────────────────────────────────────────────────────────────────

// TestCHB021_E2E_RelayFailure_DialFailed wires handler.NewHandler + handler.Launch
// with the harmonik-twin-claude binary (scenario=dial-failed) and asserts that:
//
//   - The Watcher observes agent_failed as the terminal progress-stream event.
//   - No agent_completed event is emitted (CHB-020: exactly one terminal event).
//
// This exercises the relay-failure path specified in specs/claude-hook-bridge.md
// §10 "A relay-can't-dial scenario": daemon socket missing → relay emits
// bridge_dial_failed → handler Wait-return emits agent_failed.
//
// Spec: specs/claude-hook-bridge.md §8, §10; CHB-013, CHB-015, CHB-020.
// Cite: bead hk-pcgms (relay-failure scenario: daemon socket missing →
// bridge_dial_failed).
func TestCHB021_E2E_RelayFailure_DialFailed(t *testing.T) {
	t.Parallel()

	binPath := chbE2EFixtureBuildBinary(t)
	h, pub := chbE2EFixtureHandler(t)

	spec := handler.LaunchSpec{
		Binary:  binPath,
		Args:    []string{"--scenario", "dial-failed"},
		Env:     []string{},
		WorkDir: t.TempDir(),
		Role:    "implementer",
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	sess, watcher, err := h.Launch(ctx, spec)
	if err != nil {
		t.Fatalf("handler.Launch: %v", err)
	}

	// Wait for the watcher to drain all output.
	select {
	case <-watcher.Done():
	case <-ctx.Done():
		t.Fatalf("context cancelled before watcher finished: %v", ctx.Err())
	}

	if watcherErr := watcher.Err(); watcherErr != nil {
		t.Errorf("watcher.Err(): expected nil (clean twin exit), got %v", watcherErr)
	}

	if err := sess.Wait(ctx); err != nil {
		t.Errorf("Session.Wait: %v", err)
	}

	got := pub.EventTypes()
	if len(got) == 0 {
		t.Fatal("publisher received no events; twin binary produced no output")
	}

	// CHB-020: agent_failed MUST be the terminal progress-stream event.
	lastProgressType := ""
	for _, et := range got {
		if isKnownProgressType(et) {
			lastProgressType = et
		}
	}
	if lastProgressType != "agent_failed" {
		t.Errorf("relay-failure (E2E): last progress-stream event = %q, want agent_failed (CHB-020 terminal-event obligation)", lastProgressType)
	}

	// CHB-020 + CHB-INV-002: no agent_completed must be emitted on the failure path.
	for _, et := range got {
		if et == "agent_completed" {
			t.Errorf("relay-failure (E2E): agent_completed observed but must not appear when bridge_dial_failed")
		}
	}
}

// TestCHB021_TwinParity_RelayFailure_DialFailed verifies that the dial-failed
// scenario emits the correct NDJSON sequence including agent_failed with
// reason="bridge_dial_failed" and error_category="transient" per CHB §8.
//
// Wire-level companion to TestCHB021_E2E_RelayFailure_DialFailed: inspects the
// raw NDJSON payload fields rather than the emitter's collected type list.
//
// Spec: specs/claude-hook-bridge.md §8 (bridge_dial_failed, ErrTransient),
// §10; CHB-013, CHB-015, CHB-020.
// Cite: bead hk-pcgms.
func TestCHB021_TwinParity_RelayFailure_DialFailed(t *testing.T) {
	t.Parallel()

	binPath := chbE2EFixtureBuildBinary(t)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, "--scenario", "dial-failed") //nolint:gosec // binPath from build
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	cmd.Env = []string{}
	cmd.Dir = t.TempDir()

	if err := cmd.Run(); err != nil {
		t.Fatalf("twin binary exited with error: %v", err)
	}

	// Parse NDJSON and find the agent_failed message.
	agentFailedPayload := chbE2EFixtureParseAgentFailedPayload(t, &stdout)
	if agentFailedPayload == nil {
		t.Fatal("relay-failure (twin): no agent_failed line found in twin NDJSON output")
	}

	// CHB §8: bridge_dial_failed must be the reason.
	reason, _ := agentFailedPayload["reason"].(string)
	if reason != "bridge_dial_failed" {
		t.Errorf("relay-failure (twin): agent_failed.reason = %q, want %q (CHB §8 bridge_dial_failed)", reason, "bridge_dial_failed")
	}

	// CHB §8: bridge_dial_failed maps to ErrTransient.
	errCat, _ := agentFailedPayload["error_category"].(string)
	if errCat != "transient" {
		t.Errorf("relay-failure (twin): agent_failed.error_category = %q, want %q (CHB §8 ErrTransient)", errCat, "transient")
	}
}

// chbE2EFixtureParseAgentFailedPayload scans the NDJSON output for an
// agent_failed line and returns its fields as a generic map.  The twin emits
// agent_failed as a flat NDJSON object (fields at the top level, no nested
// "payload" key).  Returns nil if no agent_failed line is found.
func chbE2EFixtureParseAgentFailedPayload(t *testing.T, buf *bytes.Buffer) map[string]interface{} {
	t.Helper()
	scanner := bufio.NewScanner(buf)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal(line, &obj); err != nil {
			t.Errorf("chbE2EFixtureParseAgentFailedPayload: unmarshal line %q: %v", string(line), err)
			continue
		}
		typStr, _ := obj["type"].(string)
		if typStr != "agent_failed" {
			continue
		}
		// The twin emits all fields at the top level of the NDJSON object.
		return obj
	}
	if err := scanner.Err(); err != nil {
		t.Errorf("chbE2EFixtureParseAgentFailedPayload: scanner error: %v", err)
	}
	return nil
}

// isExitError reports whether err is *exec.ExitError and stores it in target.
func isExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}
