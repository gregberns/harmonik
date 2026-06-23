package scenario_test

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/scenario"
)

// sh028_network_sandbox_test.go — sensor tests for SH-028
// (Harness MUST NOT depend on external network access).
//
// Spec refs: specs/scenario-harness.md §4.8 SH-028, §10.2.
// Bead: hk-q62pg.
//
// Verifies:
//
//	(a) ErrNetworkSandboxNotApplied sentinel is non-nil.
//	(b) ErrNetworkSandboxUnsupported sentinel is non-nil.
//	(c) ApplyNetworkSandbox function is callable and returns a handle or error.
//	(d) NetworkSandboxHandle.Release is safe to call on nil / zero handle.
//	(e) IsNetworkSandboxActive returns false on a normal (unsandboxed) system.
//	(f) OrchestrationConfig.EnableNetworkSandbox field exists (zero-value compiles).
//	(g) DriveOrchestration returns ErrNetworkSandboxNotApplied when
//	    EnableNetworkSandbox=true and sandbox is not active.
//	(h) Linux: ApplyNetworkSandbox enters a loopback-only namespace and
//	    non-loopback TCP dials fail; IsNetworkSandboxActive returns true.
//	(i) Spec-corpus sensor: scenario-harness.md contains SH-028 and
//	    the required mechanism keywords.
//	(j) Probe scenario file for §10.2 conformance lane is present and parses.

// ─────────────────────────────────────────────────────────────────────────────
// (a) ErrNetworkSandboxNotApplied sentinel
// ─────────────────────────────────────────────────────────────────────────────

func TestSH028_ErrNetworkSandboxNotAppliedDeclared(t *testing.T) {
	t.Parallel()
	if scenario.ErrNetworkSandboxNotApplied == nil {
		t.Fatal("SH-028: scenario.ErrNetworkSandboxNotApplied is nil; must be non-nil sentinel")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (b) ErrNetworkSandboxUnsupported sentinel
// ─────────────────────────────────────────────────────────────────────────────

func TestSH028_ErrNetworkSandboxUnsupportedDeclared(t *testing.T) {
	t.Parallel()
	if scenario.ErrNetworkSandboxUnsupported == nil {
		t.Fatal("SH-028: scenario.ErrNetworkSandboxUnsupported is nil; must be non-nil sentinel")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (c) ApplyNetworkSandbox is callable
// ─────────────────────────────────────────────────────────────────────────────

// TestSH028_ApplyNetworkSandboxCallable verifies that ApplyNetworkSandbox is
// callable and either returns a non-nil handle (sandbox applied) or a non-nil
// error (sandbox not available / no permissions). A nil handle + nil error is
// never acceptable.
//
// NOTE: This test must NOT run in parallel when sandbox is applied (the sandbox
// mutates OS-level state). It runs in parallel here because it checks the API
// shape on unsupported platforms without actually applying the sandbox.
func TestSH028_ApplyNetworkSandboxCallable(t *testing.T) {
	// We do NOT apply the sandbox here to avoid mutating OS state for the
	// main test run. This test only verifies the API is callable.
	t.Parallel()

	// On unsupported platforms the function must return (nil, non-nil error).
	// On supported platforms without privileges it also returns (nil, non-nil error).
	// On supported platforms with privileges it returns (non-nil, nil).
	// In no case is (nil, nil) acceptable.
	//
	// We cannot deterministically know which branch we're in, so we just call
	// the function and check the nil+nil invariant.
	//
	// NOTE: To avoid actually applying the sandbox (which would affect the
	// test process), we skip if we are on a platform that would succeed.
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		t.Skip("skipping ApplyNetworkSandbox invocation in this test to avoid mutating OS state; " +
			"see TestSH028_LinuxNetworkSandboxIsolation for the live test")
	}

	h, err := scenario.ApplyNetworkSandbox()
	if h == nil && err == nil {
		t.Fatal("SH-028: ApplyNetworkSandbox returned (nil, nil); one must be non-nil")
	}
	if h != nil {
		_ = h.Release()
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (d) NetworkSandboxHandle.Release is nil-safe
// ─────────────────────────────────────────────────────────────────────────────

func TestSH028_HandleReleaseNilSafe(t *testing.T) {
	t.Parallel()

	// Nil pointer must not panic.
	var h *scenario.NetworkSandboxHandle
	if err := h.Release(); err != nil {
		t.Errorf("SH-028: nil NetworkSandboxHandle.Release() returned non-nil error: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (e) IsNetworkSandboxActive returns false on a normal (unsandboxed) system
// ─────────────────────────────────────────────────────────────────────────────

// TestSH028_IsNetworkSandboxActiveReturnsFalseByDefault verifies that on a
// normal (unsandboxed) test host, IsNetworkSandboxActive() returns false.
//
// The test is skipped on platforms where there is no sandbox mechanism (the
// return value would always be false by definition, making the assertion trivially
// true but uninformative). We only verify on linux/darwin where the mechanism
// has observable side-effects.
func TestSH028_IsNetworkSandboxActiveReturnsFalseByDefault(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("sandbox detection not implemented on this platform; skipping")
	}

	if scenario.IsNetworkSandboxActive() {
		t.Errorf("SH-028: IsNetworkSandboxActive() = true on a normal test host; "+
			"expected false (tests run outside a network sandbox by default). "+
			"If this host IS intentionally sandboxed, the test environment is unusual.")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (f) OrchestrationConfig.EnableNetworkSandbox field
// ─────────────────────────────────────────────────────────────────────────────

func TestSH028_OrchestrationConfigEnableNetworkSandboxField(t *testing.T) {
	t.Parallel()

	cfg := scenario.OrchestrationConfig{}
	if cfg.EnableNetworkSandbox != false {
		t.Errorf("SH-028: OrchestrationConfig zero value EnableNetworkSandbox = %v; want false",
			cfg.EnableNetworkSandbox)
	}

	cfg.EnableNetworkSandbox = true
	if !cfg.EnableNetworkSandbox {
		t.Error("SH-028: OrchestrationConfig.EnableNetworkSandbox assignment failed")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (g) DriveOrchestration returns ErrNetworkSandboxNotApplied when sandbox required but not active
// ─────────────────────────────────────────────────────────────────────────────

// TestSH028_DriveOrchestrationRejectsWhenSandboxNotActive verifies that
// DriveOrchestration returns ErrNetworkSandboxNotApplied when
// EnableNetworkSandbox=true but IsNetworkSandboxActive() is false (normal host).
//
// This test is skipped on platforms where IsNetworkSandboxActive always returns
// false regardless of sandbox state (since on such platforms the check would
// always fire, making the test trivially correct but not representative).
func TestSH028_DriveOrchestrationRejectsWhenSandboxNotActive(t *testing.T) {
	t.Parallel()

	// Only test on platforms with a real sandbox mechanism.
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("sandbox enforcement only meaningful on linux/darwin; skipping on " + runtime.GOOS)
	}

	// Pre-condition: verify we are NOT in a sandbox (normal test host).
	if scenario.IsNetworkSandboxActive() {
		t.Skip("test host appears to be sandboxed; skipping to avoid false pass")
	}

	cfg := scenario.OrchestrationConfig{
		ProjectDir:           "/nonexistent-sh028-sandbox-test/project",
		JSONLLogPath:         "/nonexistent-sh028-sandbox-test/project/.harmonik/events/events.jsonl",
		HandlerBinary:        "/nonexistent-sh028-sandbox-test/twin",
		EnableNetworkSandbox: true,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := scenario.DriveOrchestration(ctx, cfg)
	if !errors.Is(err, scenario.ErrNetworkSandboxNotApplied) {
		t.Errorf("SH-028: DriveOrchestration with EnableNetworkSandbox=true on unsandboxed host "+
			"returned %v; want errors.Is(err, ErrNetworkSandboxNotApplied)", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (h) Linux: apply sandbox, verify isolation
// ─────────────────────────────────────────────────────────────────────────────

// TestSH028_LinuxNetworkSandboxIsolation verifies the Linux network sandbox
// mechanism end-to-end:
//
//  1. ApplyNetworkSandbox enters a new network namespace.
//  2. IsNetworkSandboxActive returns true after apply.
//  3. A TCP dial to a non-loopback address fails (network unreachable or
//     similar kernel error — not a timeout).
//  4. A TCP dial to 127.0.0.1 also fails (no listener), but with a "connection
//     refused" error, not a "network unreachable" — proving loopback is still up.
//  5. Release restores the OS thread lock.
//
// This test is Linux-only, cannot run in parallel (mutates OS thread network
// namespace), and is skipped if unshare(CLONE_NEWNET) is unavailable (no CAP_SYS_ADMIN
// and no user namespace support).
func TestSH028_LinuxNetworkSandboxIsolation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only sandbox test (unshare(CLONE_NEWNET))")
	}
	// NOT t.Parallel() — mutates OS thread network namespace.

	h, err := scenario.ApplyNetworkSandbox()
	if err != nil {
		t.Skipf("SH-028: ApplyNetworkSandbox: %v (skipping; requires CAP_SYS_ADMIN or user namespaces)", err)
	}
	defer func() {
		if releaseErr := h.Release(); releaseErr != nil {
			t.Errorf("SH-028: NetworkSandboxHandle.Release: %v", releaseErr)
		}
	}()

	// Verify IsNetworkSandboxActive now returns true.
	if !scenario.IsNetworkSandboxActive() {
		t.Error("SH-028: IsNetworkSandboxActive() = false after ApplyNetworkSandbox; expected true " +
			"(only loopback interface should be present in the new namespace)")
	}

	// Verify non-loopback TCP dial fails with a network-level error.
	// Use a well-known non-loopback address (8.8.8.8:53 — Google DNS).
	// After unshare(CLONE_NEWNET), this MUST fail with "network unreachable"
	// or similar, NOT with a timeout (the kernel rejects it immediately).
	conn, dialErr := net.DialTimeout("tcp", "8.8.8.8:53", nonLoopbackDialTimeout) //nolint:noctx
	if conn != nil {
		_ = conn.Close()
		t.Error("SH-028: non-loopback TCP dial to 8.8.8.8:53 SUCCEEDED inside network sandbox; " +
			"this violates SH-028 (external network access must be blocked)")
	}
	if dialErr == nil {
		t.Error("SH-028: net.DialTimeout to 8.8.8.8:53 returned nil error inside sandbox; expected a network error")
	}
	// Confirm the error is not a timeout (the kernel must reject immediately
	// in a namespace with no non-loopback routes).
	if isNetworkTimeout(dialErr) {
		t.Errorf("SH-028: non-loopback dial failed with a timeout error %v; "+
			"expected an immediate kernel rejection (network unreachable), not a timeout", dialErr)
	}

	// Verify loopback is still up by attempting a dial to 127.0.0.1:1
	// (no listener). Expect "connection refused", NOT "network unreachable".
	_, loopErr := net.DialTimeout("tcp", "127.0.0.1:1", nonLoopbackDialTimeout) //nolint:noctx
	if loopErr == nil {
		// Extremely unlikely but harmless if something is listening on port 1.
		t.Log("SH-028: unexpected successful dial to 127.0.0.1:1; loopback appears up (OK)")
	} else if isNetworkUnreachable(loopErr) {
		t.Errorf("SH-028: loopback dial to 127.0.0.1:1 returned %v (network unreachable); "+
			"loopback must be up after ApplyNetworkSandbox", loopErr)
	}
	// "connection refused" is the expected error for loopback (no listener) — pass.
}

// nonLoopbackDialTimeout is the maximum time allowed for a dial that is
// expected to fail immediately with a kernel network error.
// We set it conservatively short (100ms) to fail fast in tests; the kernel
// should reject a non-loopback dial in a CLONE_NEWNET namespace almost instantly.
const nonLoopbackDialTimeout = 100 * time.Millisecond

// isNetworkTimeout reports whether err is a timeout error.
func isNetworkTimeout(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

// isNetworkUnreachable reports whether err contains a "network unreachable"
// message, which indicates no routes (as in a fresh CLONE_NEWNET namespace).
func isNetworkUnreachable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "network unreachable") ||
		strings.Contains(msg, "no route to host") ||
		strings.Contains(msg, "enetunreach")
}

// ─────────────────────────────────────────────────────────────────────────────
// (i) Spec-corpus sensor
// ─────────────────────────────────────────────────────────────────────────────

// TestSH028_SpecCorpusClause verifies that scenario-harness.md contains SH-028
// and the required mechanism keywords (CLONE_NEWNET, pf, harness-internal-error).
func TestSH028_SpecCorpusClause(t *testing.T) {
	t.Parallel()

	root := sh028ModuleRoot(t)
	specPath := filepath.Join(root, "specs", "scenario-harness.md")

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("SH-028: reading scenario-harness.md: %v", err)
	}
	text := string(data)

	checks := []struct {
		needle string
		detail string
	}{
		{"SH-028", "SH-028 clause must be present"},
		{"CLONE_NEWNET", "Linux mechanism (unshare(CLONE_NEWNET)) must be named"},
		{"pf", "macOS mechanism (pf packet-filter) must be named"},
		{"harness-internal-error", "non-loopback connection → harness-internal-error must be stated"},
		{"conformance lane", "§10.2 conformance lane sandbox obligation must be stated"},
	}

	for _, c := range checks {
		if !strings.Contains(text, c.needle) {
			t.Errorf("SH-028: scenario-harness.md missing %q — %s", c.needle, c.detail)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (j) Probe scenario file for §10.2 conformance lane
// ─────────────────────────────────────────────────────────────────────────────

// TestSH028_ProbeScenarioFileExists verifies that the network-sandbox probe
// scenario (scenarios/regression/network-sandbox-probe.yaml) exists and parses
// as a valid ScenarioFile per SH-028's §10.2 test obligation.
//
// The probe scenario declares a twin that attempts a non-loopback connection;
// when run in the conformance lane (sandbox enabled), it MUST fail with
// harness-internal-error. The full end-to-end execution is tested by the
// conformance lane (requires the harness CLI, G-01 / SH-032).
func TestSH028_ProbeScenarioFileExists(t *testing.T) {
	t.Parallel()

	root := sh028ModuleRoot(t)
	probePath := filepath.Join(root, "scenarios", "regression", "network-sandbox-probe.yaml")

	if _, statErr := os.Stat(probePath); statErr != nil {
		t.Fatalf("SH-028: scenarios/regression/network-sandbox-probe.yaml not found: %v\n"+
			"  This file is required by §10.2 test obligation for SH-028 (network sandbox probe scenario).",
			statErr)
	}

	sf, parseErr := scenario.ParseScenarioFile(probePath)
	if parseErr != nil {
		t.Fatalf("SH-028: parse error in network-sandbox-probe.yaml: %v", parseErr)
	}

	if !sf.Valid() {
		t.Error("SH-028: network-sandbox-probe.yaml failed ScenarioFile.Valid()")
	}

	if sf.CadenceTag != scenario.CadenceTagRegression {
		t.Errorf("SH-028: network-sandbox-probe.yaml cadence_tag = %q; want %q (regression cadence for SH-028 probes)",
			sf.CadenceTag, scenario.CadenceTagRegression)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

func sh028ModuleRoot(t *testing.T) string {
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
