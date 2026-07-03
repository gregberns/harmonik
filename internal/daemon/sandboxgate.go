package daemon

// sandboxgate.go — srt sandbox argv-wrap gate decision (hk-r4p0l).
//
// The srt sandbox (hk-6596l wiring, hk-rlxgx argv-wrap) engages only when a
// run's harness is listed in sandbox.harnesses and sandbox.backend == "srt".
// Two pure helpers isolate that decision so it is unit-testable independent of
// the ~1000-line beadRunOne body:
//
//   - resolveGateAgentType picks the AUTHORITATIVE harness identity for the
//     gate. The originally-shipped gate (hk-6596l) keyed off
//     artifactAgentType(artifacts). That is a defect for any harness whose
//     resolved identity is not reflected by the artifacts value read at the
//     gate: a pi run could observe "claude-code" and silently skip the wrap.
//     The resolved Harness (implHarnessWL, looked up via HarnessRegistry.ForAgent)
//     exposes AgentType() — the guaranteed-correct identity — so the gate keys
//     off that when available, falling back to the artifacts-derived value only
//     when no resolved Harness is in scope (nil registry / lookup miss).
//
//   - sandboxSpawnForRun applies the two config predicates (backend == "srt"
//     AND agentType ∈ sandbox.harnesses) and returns the SrtSpawnConfig to
//     attach, or nil for a strict no-op. Backend != "srt" is always nil; a
//     harness not listed in sandbox.harnesses is always nil.
//
// Bead: hk-r4p0l. Precedes: hk-6596l (wiring), hk-rlxgx (argv-wrap).

import (
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// resolveGateAgentType returns the harness identity the sandbox gate must match
// against sandbox.harnesses. It prefers the resolved Harness's AgentType() (the
// authoritative identity) and falls back to fromArtifacts only when implHarness
// is nil (no HarnessRegistry, or ForAgent returned an error at the call site).
func resolveGateAgentType(implHarness handlercontract.Harness, fromArtifacts core.AgentType) core.AgentType {
	if implHarness != nil {
		return implHarness.AgentType()
	}
	return fromArtifacts
}

// sandboxSpawnForRun decides whether a run under agentType should be srt-wrapped.
//
//	Returns a non-nil *SrtSpawnConfig (carrying in) when cfg.Backend == "srt"
//	AND agentType is listed in cfg.Harnesses. Returns nil (strict no-op)
//	otherwise: any non-"srt" backend, or a harness not in the list.
func sandboxSpawnForRun(cfg SandboxConfig, agentType core.AgentType, in SandboxProfileInput) *SrtSpawnConfig {
	if cfg.Backend != "srt" {
		return nil
	}
	if !cfg.HasHarness(string(agentType)) {
		return nil
	}
	return &SrtSpawnConfig{ProfileInput: in}
}
