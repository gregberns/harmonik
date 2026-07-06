package daemon_test

// sandboxgate_hkr4p0l_test.go — srt sandbox gate decision (hk-r4p0l).
//
// The originally-shipped gate (hk-6596l) keyed the srt argv-wrap off
// string(artifactAgentType(artifacts)). For a pi run that value can read
// "claude-code", so backend=srt + harnesses:[pi] silently no-op'd and pi runs
// were NOT sandboxed. The fix keys the gate off the RESOLVED harness identity
// (implHarnessWL.AgentType()). These tests pin both the source-selection helper
// (resolveGateAgentType) and the config-predicate helper (sandboxSpawnForRun).

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// TestResolveGateAgentType_PrefersResolvedHarness is the RED-before/GREEN-after
// regression for hk-r4p0l: a resolved pi Harness MUST win over an
// artifacts-derived "claude-code" value. Pre-fix (gate keyed off the artifacts
// value) this scenario yielded claude-code and the sandbox skipped; post-fix it
// yields pi.
func TestResolveGateAgentType_PrefersResolvedHarness(t *testing.T) {
	t.Parallel()

	piHarness := daemon.ExportedNewPiHarness("pi", "openrouter", "gpt-5.4-mini", "OPENROUTER_API_KEY", "", "", "")

	// Resolved harness is pi; the artifacts-derived value is the misleading
	// claude-code. The gate must key off the resolved harness.
	got := daemon.ExportedResolveGateAgentType(piHarness, core.AgentTypeClaudeCode)
	if got != core.AgentTypePi {
		t.Fatalf("resolveGateAgentType(pi-harness, claude-code) = %q; want %q "+
			"(gate must key off the resolved harness, not the artifacts value)", got, core.AgentTypePi)
	}

	// Nil harness (no registry / lookup miss) falls back to the artifacts value.
	if got := daemon.ExportedResolveGateAgentType(nil, core.AgentTypeClaudeCode); got != core.AgentTypeClaudeCode {
		t.Fatalf("resolveGateAgentType(nil, claude-code) = %q; want %q (fallback)", got, core.AgentTypeClaudeCode)
	}
}

// TestSandboxSpawnForRun_GateInvariants pins the three config-predicate
// invariants required by hk-r4p0l.
func TestSandboxSpawnForRun_GateInvariants(t *testing.T) {
	t.Parallel()

	in := daemon.SandboxProfileInput{
		WorktreePath:   "/wt",
		GitDir:         "/repo/.git",
		RunID:          "run-1",
		DaemonSockPath: "/repo/.harmonik/daemon.sock",
	}
	srtPiCfg := daemon.SandboxConfig{Backend: "srt", Harnesses: []string{"pi"}}

	// (i) pi run + srt + harnesses:[pi] → wrapped.
	if sb := daemon.ExportedSandboxSpawnForRun(srtPiCfg, core.AgentTypePi, in); sb == nil {
		t.Fatal("pi run + backend=srt + harnesses:[pi]: want non-nil SrtSpawnConfig; got nil (sandbox skipped — the hk-r4p0l bug)")
	} else if sb.ProfileInput.RunID != "run-1" {
		t.Fatalf("SrtSpawnConfig.ProfileInput not threaded: RunID=%q want %q", sb.ProfileInput.RunID, "run-1")
	}

	// (ii) claude run + harnesses:[pi] → NOT wrapped (harness not listed).
	if sb := daemon.ExportedSandboxSpawnForRun(srtPiCfg, core.AgentTypeClaudeCode, in); sb != nil {
		t.Fatal("claude run + harnesses:[pi]: want nil (harness not listed); got non-nil")
	}

	// (iii) backend=none → strict no-op even when the harness is listed.
	noneCfg := daemon.SandboxConfig{Backend: "none", Harnesses: []string{"pi"}}
	if sb := daemon.ExportedSandboxSpawnForRun(noneCfg, core.AgentTypePi, in); sb != nil {
		t.Fatal("backend=none: want nil (strict no-op); got non-nil")
	}

	// (iv) backend="" (block absent) → strict no-op.
	absentCfg := daemon.SandboxConfig{Backend: "", Harnesses: []string{"pi"}}
	if sb := daemon.ExportedSandboxSpawnForRun(absentCfg, core.AgentTypePi, in); sb != nil {
		t.Fatal("backend=\"\" (absent block): want nil (strict no-op); got non-nil")
	}
}

// TestSandboxSpawnForRun_RemoteSocketSkipsWrap is the regression for hk-ybuts:
// on a REMOTE worker run DaemonSockPath is a reverse-tunnel TCP endpoint, not an
// absolute unix-socket path. srt is a box-A-local sandbox and cannot wrap a run
// whose agent executes on the worker's OS; worse, GenerateSandboxProfile rejects
// the non-absolute DaemonSockPath and every remote run dies in ~2s. The gate must
// no-op for a remote socket while still wrapping a genuine local unix-socket run.
func TestSandboxSpawnForRun_RemoteSocketSkipsWrap(t *testing.T) {
	t.Parallel()

	srtPiCfg := daemon.SandboxConfig{Backend: "srt", Harnesses: []string{"pi"}}

	base := daemon.SandboxProfileInput{
		WorktreePath: "/wt",
		GitDir:       "/repo/.git",
		RunID:        "run-1",
	}

	// (i) REMOTE run: tcp:// reverse-tunnel endpoint → gate no-ops (nil).
	remote := base
	remote.DaemonSockPath = "tcp://127.0.0.1:52345"
	if sb := daemon.ExportedSandboxSpawnForRun(srtPiCfg, core.AgentTypePi, remote); sb != nil {
		t.Fatal("remote tcp:// DaemonSockPath: want nil (srt skipped on remote run); got non-nil " +
			"— GenerateSandboxProfile would reject the non-absolute path and kill the run (hk-ybuts)")
	}

	// (ii) LOCAL run: absolute unix-socket path → still wrapped (non-nil).
	local := base
	local.DaemonSockPath = "/repo/.harmonik/daemon.sock"
	if sb := daemon.ExportedSandboxSpawnForRun(srtPiCfg, core.AgentTypePi, local); sb == nil {
		t.Fatal("local absolute unix-socket DaemonSockPath: want non-nil SrtSpawnConfig; got nil (local run must still be sandboxed)")
	}

	// (iii) EMPTY DaemonSockPath (misconfigured local run): the gate must NOT
	// swallow it as a remote skip — it falls through non-nil so GenerateSandboxProfile
	// surfaces the downstream "must be non-empty" error (fail-CLOSED, hk-ybuts).
	empty := base
	empty.DaemonSockPath = ""
	if sb := daemon.ExportedSandboxSpawnForRun(srtPiCfg, core.AgentTypePi, empty); sb == nil {
		t.Fatal("empty DaemonSockPath: want non-nil (fall through to downstream must-be-non-empty error); got nil (a misconfigured local run must fail closed, not silently skip the sandbox)")
	}
}
