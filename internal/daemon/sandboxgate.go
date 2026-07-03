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
	"fmt"
	"os"
	"path/filepath"

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

// srtWrapArgv generates a per-run srt settings profile, writes it to a temp file,
// and returns the srt-prefixed argv:
//
//	[SrtBinary, "--settings", profilePath, agentArgv...]
//
// This is the single source of truth for the srt argv-wrap, shared by BOTH wrap
// sites (hk-r4p0l): the substrate path (perRunSubstrate.SpawnWindow, for
// non-session-id-captured harnesses that spawn via a tmux window) and the exec
// path (workloop, for SessionIDCaptured harnesses like pi that run via
// exec.CommandContext with spec.Substrate=nil). Keeping one function guarantees
// both paths produce a byte-identical wrapper and reuse the same gate-produced
// SrtSpawnConfig, so pi and claude/codex share one wrap contract.
//
// The profile JSON is produced by GenerateSandboxProfile(spawn.ProfileInput) and
// written to os.TempDir()/harmonik-srt-<RunID>.json (mode 0600). The file is NOT
// cleaned up here — srt reads it at startup and the OS reclaims it at reboot.
//
// Returns an error (NOT wrapped with ErrStructural — the caller wraps) when
// profile generation or file write fails.
//
// Bead: hk-rlxgx (original substrate wrap), hk-r4p0l (extraction + exec-path reuse).
func srtWrapArgv(spawn *SrtSpawnConfig, agentArgv []string) ([]string, error) {
	profileBytes, err := GenerateSandboxProfile(spawn.ProfileInput)
	if err != nil {
		return nil, fmt.Errorf("generate srt profile: %w", err)
	}
	profilePath := filepath.Join(os.TempDir(), "harmonik-srt-"+spawn.ProfileInput.RunID+".json")
	//nolint:gosec // G306: 0600 is correct — profile contains literal filesystem paths, readable only by daemon uid.
	if err := os.WriteFile(profilePath, profileBytes, 0o600); err != nil {
		return nil, fmt.Errorf("write srt profile to %s: %w", profilePath, err)
	}
	srtBin := spawn.SrtBinary
	if srtBin == "" {
		srtBin = "srt"
	}
	result := make([]string, 0, 3+len(agentArgv))
	result = append(result, srtBin, "--settings", profilePath)
	result = append(result, agentArgv...)
	return result, nil
}

// sandboxWrapExecArgv applies the srt argv-wrap to an EXEC-path LaunchSpec
// (spec.Substrate == nil), used by SessionIDCaptured harnesses (pi) that run via
// exec.CommandContext rather than through the substrate's SpawnWindow (hk-r4p0l
// part 2). Given the gate-produced spawn config and the run's (binary, args), it
// returns the new (binary, args):
//
//	srt --settings <profilePath> <binary> <args...>
//
// When spawn is nil (the strict no-op gate: backend != "srt", or the harness is
// not in sandbox.harnesses), it returns (binary, args) UNCHANGED with no error —
// the exec path is byte-identical to today's behaviour. This shares srtWrapArgv
// with the substrate path so the two launch paths cannot diverge.
func sandboxWrapExecArgv(spawn *SrtSpawnConfig, binary string, args []string) (string, []string, error) {
	if spawn == nil {
		return binary, args, nil
	}
	wrapped, err := srtWrapArgv(spawn, append([]string{binary}, args...))
	if err != nil {
		return "", nil, err
	}
	return wrapped[0], wrapped[1:], nil
}
