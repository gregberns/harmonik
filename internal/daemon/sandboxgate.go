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
//     shared.ArtifactAgentType(artifacts). That is a defect for any harness whose
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
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/projectconfig"
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
//	otherwise: any non-"srt" backend, a harness not in the list, or a REMOTE
//	run (see the DaemonSockPath guard below).
func sandboxSpawnForRun(cfg projectconfig.SandboxConfig, agentType core.AgentType, in SandboxProfileInput) *SrtSpawnConfig {
	if cfg.Backend != "srt" {
		return nil
	}
	if !cfg.HasHarness(string(agentType)) {
		return nil
	}
	// hk-ybuts: skip the srt wrap on a REMOTE worker run. srt is a box-A-LOCAL
	// macOS sandbox; a remote run's agent executes on the WORKER's OS, outside
	// box A entirely, so there is nothing local to sandbox. On a remote run
	// DaemonSockPath is a reverse-tunnel TCP endpoint ("tcp://127.0.0.1:<port>",
	// see tunnel.ResolveAgentDaemonSocket) rather than an absolute unix-socket path —
	// so wrapping is not just pointless but FATAL: GenerateSandboxProfile rejects
	// the non-absolute DaemonSockPath ("must be an absolute path"), killing every
	// remote run in ~2s. Gating here — the single source of truth for "should
	// this run be srt-wrapped" — means BOTH launch paths (single-mode's exec wrap
	// and the DOT cascade's) inherit the no-op and cannot diverge. An empty
	// DaemonSockPath is deliberately left to fall through so a misconfigured
	// LOCAL run still surfaces the "must be non-empty" error downstream, and a
	// (never-observed) relative LOCAL path stays fail-CLOSED via that same
	// downstream "must be an absolute path" check — we key ONLY on the tcp://
	// prefix, the sole signal tunnel.ResolveAgentDaemonSocket emits for a remote run,
	// so this guard never trades a local run's sandbox for a fail-open skip.
	if strings.HasPrefix(in.DaemonSockPath, "tcp://") {
		return nil
	}
	return &SrtSpawnConfig{ProfileInput: in}
}

// srtDefaultChildTmpDir is the sandboxed-child TMPDIR srt falls back to when
// nothing tells it otherwise (hk-cdpxu). See srtWrapArgv and sandboxprofile.go
// allowWrite step 6b.
const srtDefaultChildTmpDir = "/tmp/claude"

// srtChildTmpDirEnvVar is the variable srt reads from its OWN environment to
// decide what TMPDIR the sandboxed child gets. MEASURED, srt 0.0.63,
// sandbox-utils.js generateProxyEnvVars:
//
//	const tmpdir = process.env.CLAUDE_CODE_TMPDIR ||
//	               process.env.CLAUDE_TMPDIR || '/tmp/claude';
//	envVars.push(`TMPDIR=${tmpdir}`);
//
// The name carries one harness's brand because srt does, but the knob does not:
// srt sets the child's TMPDIR from it whatever runs inside, so pointing it at
// the run's own scratch directory is the harness-agnostic fix. It is set on the
// srt PARENT process, not on the agent — the agent sees only TMPDIR.
//
// The push is CONDITIONAL: generateProxyEnvVars takes a skipTmpdir parameter,
// and macos-sandbox-utils.js passes `writeConfig === undefined` for it, so srt
// leaves the child's TMPDIR alone when the profile carries no filesystem policy.
// Harmonik always emits a filesystem block (see GenerateSandboxProfile), so
// writeConfig is defined and the injection always happens on our runs.
const srtChildTmpDirEnvVar = "CLAUDE_CODE_TMPDIR"

// srtWrap is what one srt argv-wrap produces: the argv to spawn, and the extra
// environment the srt PARENT process must carry for the wrap to mean what it
// says. They are grouped so a caller gets both from one return and does not have
// to know a second value exists.
//
// Grouping does not enforce use. A launch path that keeps the argv and drops the
// env append still compiles, and the child then falls back to srt's default
// TMPDIR instead of this run's own directory. The two tests in
// sandboxscratchtmpdir_wireup_test.go are what catch that drop: they read the
// environment on the far side of each launch path, not the value this type
// carries.
type srtWrap struct {
	// Argv is [srtBinary, "--settings", profilePath, agentArgv...].
	Argv []string

	// Env holds "KEY=VALUE" entries to APPEND to the environment of the process
	// named by Argv[0]. Never a complete environment.
	Env []string
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
// It also creates the run's scratch directory and returns the environment entry
// that points the sandboxed child's TMPDIR at it, so a process inside the
// sandbox can do what any POSIX process may assume it can do: write a temp file.
//
// The profile JSON is produced by GenerateSandboxProfile(spawn.ProfileInput) and
// written to os.TempDir()/harmonik-srt-<RunID>.json (mode 0600). The file is NOT
// cleaned up here — srt reads it at startup and the OS reclaims it at reboot.
//
// Returns an error (NOT wrapped with ErrStructural — the caller wraps) when
// scratch-directory creation, profile generation, or the file write fails.
//
// Bead: hk-rlxgx (original substrate wrap), hk-r4p0l (extraction + exec-path
// reuse), hk-sandbox-no-writable-tmpdir-7484h (scratch dir + child TMPDIR).
func srtWrapArgv(spawn *SrtSpawnConfig, agentArgv []string) (srtWrap, error) {
	// hk-sandbox-no-writable-tmpdir-7484h: give the run a temp directory of its
	// OWN and point the child at it. The child was never without a writable
	// TMPDIR — srt's default is /tmp/claude, and the profile grants it — but that
	// is ONE directory for the whole box, so every concurrent run spooled into
	// it. The run that threw away finished work was refused for a different
	// reason: it was told to write the literal path /tmp/commit-msg.txt, which
	// nothing granted, so it got EPERM, retried with sudo, and abandoned a
	// correct commit message. That instruction is fixed in internal/workspace
	// buildAgentTaskContent; this gives the run somewhere of its own to put the
	// file. The directory must EXIST before the child starts — a granted path
	// that is not on disk still gives ENOENT to the first tool that opens it.
	//
	// 0o700 because a temp directory holds whatever the agent spools into it and
	// nothing else has business reading it. MkdirAll is idempotent, so a rerun
	// against an existing directory is not an error.
	scratchDir := SandboxScratchDir(spawn.ProfileInput.WorktreePath)
	if err := os.MkdirAll(scratchDir, 0o700); err != nil { //dirmode:allow tighter on purpose: the run's sandbox scratch TMPDIR holds agent-spooled files, 0o700 by design
		return srtWrap{}, fmt.Errorf("create sandbox scratch TMPDIR %s: %w", scratchDir, err)
	}

	// hk-cdpxu: absent srtChildTmpDirEnvVar, srt injects TMPDIR=/tmp/claude into
	// the sandboxed child regardless of the parent's own TMPDIR and of anything
	// else in the profile. That path is granted (sandboxprofile.go allowWrite
	// step 6b) but a grant is not a directory: srt stats it on child startup
	// ("stat /tmp/claude: no such file or directory") before any sandboxed
	// process runs. The env entry below means no run should reach that fallback
	// any more; the directory is still created so a host running an srt without
	// that knob degrades to the old behaviour instead of to ENOENT.
	if err := os.MkdirAll(srtDefaultChildTmpDir, 0o700); err != nil { //dirmode:allow not a .harmonik state dir: srt's default /tmp/claude sandbox scratch TMPDIR, 0o700 by design
		return srtWrap{}, fmt.Errorf("create srt fallback TMPDIR %s: %w", srtDefaultChildTmpDir, err)
	}

	profileBytes, err := GenerateSandboxProfile(spawn.ProfileInput)
	if err != nil {
		return srtWrap{}, fmt.Errorf("generate srt profile: %w", err)
	}
	profilePath := filepath.Join(os.TempDir(), "harmonik-srt-"+spawn.ProfileInput.RunID+".json")
	if err := os.WriteFile(profilePath, profileBytes, 0o600); err != nil {
		return srtWrap{}, fmt.Errorf("write srt profile to %s: %w", profilePath, err)
	}
	srtBin := spawn.SrtBinary
	if srtBin == "" {
		srtBin = "srt"
	}
	argv := make([]string, 0, 3+len(agentArgv))
	argv = append(argv, srtBin, "--settings", profilePath)
	argv = append(argv, agentArgv...)
	return srtWrap{
		Argv: argv,
		Env:  []string{srtChildTmpDirEnvVar + "=" + scratchDir},
	}, nil
}

// sandboxWrapExecArgv applies the srt argv-wrap to an EXEC-path LaunchSpec
// (spec.Substrate == nil), used by SessionIDCaptured harnesses (pi) that run via
// exec.CommandContext rather than through the substrate's SpawnWindow (hk-r4p0l
// part 2). Given the gate-produced spawn config and the run's (binary, args), it
// returns the new (binary, args):
//
//	srt --settings <profilePath> <binary> <args...>
//
// plus the environment entries the caller must APPEND to spec.Env — the child's
// TMPDIR among them.
//
// When spawn is nil (the strict no-op gate: backend != "srt", or the harness is
// not in sandbox.harnesses), it returns (binary, args) UNCHANGED with a nil env
// and no error — the exec path is byte-identical to an unsandboxed launch. This
// shares srtWrapArgv with the substrate path so the two launch paths cannot
// diverge.
func sandboxWrapExecArgv(spawn *SrtSpawnConfig, binary string, args []string) (wrappedBinary string, wrappedArgs, extraEnv []string, err error) {
	if spawn == nil {
		return binary, args, nil, nil
	}
	wrapped, wrapErr := srtWrapArgv(spawn, append([]string{binary}, args...))
	if wrapErr != nil {
		return "", nil, nil, wrapErr
	}
	return wrapped.Argv[0], wrapped.Argv[1:], wrapped.Env, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Production sandbox-engagement verification (hk-5wdon, follow-up to hk-tch4t)
// ─────────────────────────────────────────────────────────────────────────────

// srtEngagementMaxAttempts bounds the retry budget for verifySandboxEngaged.
// hk-tch4t diagnosed a TRANSIENT srt sandbox_init apply-failure under heavy
// concurrent-fork saturation (the sandbox silently fails to attach even though
// srt itself exits 0). A single observed apply-failure must be retried — not
// treated as fatal on the first sample — or the production gate would flake
// red under the exact host conditions hk-tch4t already characterized. A
// CONSISTENT apply-failure across the whole budget is never absorbed: it is
// the "srt runs the child unsandboxed" hole this bead exists to close.
const srtEngagementMaxAttempts = 5

// srtEngagementCanaryMarker is written by the probe command and read back to
// distinguish "the write reached disk" from "the write was denied and the
// canary file was never touched."
const srtEngagementCanaryMarker = "harmonik-srt-engagement-canary"

// srtEngagementCanaryPath returns the path the engagement probe attempts to
// write to. It is deliberately OUTSIDE the profile's allowWrite set: per
// GenerateSandboxProfile (hk-p7smp), allowWrite contains the run's
// WorktreePath and specific git-internal paths, never the project root
// itself. A file directly under projectDir mirrors the "main-file.txt"
// canary the hk-tch4t/hk-i0377 test suite already uses to prove denial, so
// the production probe exercises the identical isolation invariant. The
// RunID suffix keeps concurrent runs' canaries from colliding.
func srtEngagementCanaryPath(projectDir, runID string) string {
	return filepath.Join(projectDir, ".harmonik-srt-engagement-"+runID+".canary")
}

// verifySandboxEngaged proves the srt sandbox spawn actually engaged, rather
// than trusting srt's own exit code (which hk-tch4t showed can be 0 even when
// sandbox_init silently failed to apply). It runs a canary probe under the
// SAME profile the real spawn will use: an attempted write to canaryPath, a
// path guaranteed outside allowWrite. Engagement is proven only when BOTH:
//   - the srt process itself reports a non-zero exit, AND
//   - canaryPath was not written (the write never reached disk).
//
// Either condition failing alone is the production analogue of "srt exited 0
// but sandbox_init never applied" — the caller MUST treat a non-nil return as
// fatal and refuse to launch the real agent unsandboxed.
//
// spawn.SrtBinary is honored (not hardcoded to "srt"), so a test can point it
// at a stub binary that fakes the apply-failure deterministically without any
// dependency on macOS Seatbelt or fork-saturation timing.
func verifySandboxEngaged(ctx context.Context, spawn *SrtSpawnConfig, canaryPath string, logf func(format string, args ...any)) error {
	profileBytes, err := GenerateSandboxProfile(spawn.ProfileInput)
	if err != nil {
		return fmt.Errorf("verifySandboxEngaged: generate probe profile: %w", err)
	}
	profilePath := filepath.Join(os.TempDir(), "harmonik-srt-engagement-"+spawn.ProfileInput.RunID+".json")
	if err := os.WriteFile(profilePath, profileBytes, 0o600); err != nil {
		return fmt.Errorf("verifySandboxEngaged: write probe profile: %w", err)
	}
	defer func() { removeProbeFile(profilePath, "probe profile", logf) }()

	srtBin := spawn.SrtBinary
	if srtBin == "" {
		srtBin = "srt"
	}
	script := fmt.Sprintf("echo %s > %s", srtEngagementCanaryMarker, shellQuoteSingle(canaryPath))

	var lastDetail string
	for attempt := 1; attempt <= srtEngagementMaxAttempts; attempt++ {
		// Clean slate: a prior attempt's leaked write must not taint this one.
		removeProbeFile(canaryPath, "canary before attempt", logf)

		//nolint:gosec // G204: srtBin/profilePath/script are daemon-controlled, not attacker input.
		cmd := exec.CommandContext(ctx, srtBin, "--settings", profilePath, "-c", script)
		out, runErr := cmd.CombinedOutput()

		content, readErr := os.ReadFile(canaryPath)
		wrote := readErr == nil && strings.Contains(string(content), srtEngagementCanaryMarker)
		removeProbeFile(canaryPath, "canary after attempt", logf)

		if runErr != nil && !wrote {
			return nil // engaged: srt itself failed AND the denied write never landed
		}

		lastDetail = fmt.Sprintf("attempt %d/%d: srt_exit_err=%v canary_written=%v output=%q",
			attempt, srtEngagementMaxAttempts, runErr, wrote, strings.TrimSpace(string(out)))
		if logf != nil {
			logf("srt sandbox engagement probe observed apply-failure (%s); retrying (hk-tch4t transient) canary=%s",
				lastDetail, canaryPath)
		}
	}
	return fmt.Errorf("srt sandbox engagement verification FAILED after %d attempts — sandbox_init did not engage "+
		"(hk-tch4t/hk-5wdon apply-failure): %s", srtEngagementMaxAttempts, lastDetail)
}

// shellQuoteSingle wraps s in single quotes for use inside an `srt -c "..."`
// shell script, escaping any embedded single quote. canaryPath values are
// daemon-derived (projectDir + a UUID RunID), never attacker input.
func shellQuoteSingle(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// removeProbeFile deletes one file the srt engagement probe created. An absent
// file is the normal case. Any other failure leaves the file in place, which can
// taint the next attempt's canary reading, so report it through logf.
func removeProbeFile(path, what string, logf func(format string, args ...any)) {
	rmErr := os.Remove(path)
	if rmErr == nil || errors.Is(rmErr, os.ErrNotExist) || logf == nil {
		return
	}
	logf("srt sandbox engagement probe: remove %s %s: %v", what, path, rmErr)
}
