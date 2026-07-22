package daemon

// crewlaunchspec.go — buildCrewLaunchSpec, persistent-session argv builder for C2.
//
// Builds the argv/env spec for launching a persistent crew session under
// claude --remote-control. Sibling of buildClaudeLaunchSpec; does NOT modify it.
//
// Spec ref: docs/plans/captain/05-specs/c2-spec.md §3.2 (Launch construction).
// Acceptance criterion: C2 AC-5.
// Bead: hk-kbqto.

import (
	"fmt"

	"github.com/gregberns/harmonik/internal/handler"
)

// JoinRemoteControlName builds the Claude Code --remote-control session LABEL
// from a per-project prefix and the agent name. It is the SINGLE source of the
// label format, shared by every launch site (daemon crew launch, captain CLI,
// and indirectly the bash launchers via the read-side CLI) so the format never
// drifts:
//
//	prefix == ""  → name                 (backward compatible — bare label)
//	prefix != ""  → prefix + "-" + name  (e.g. "hk" + "paul" → "hk-paul")
//
// The label is COSMETIC: it disambiguates the global-per-host Remote-Control
// session picker across concurrent projects. Harmonik's own identity keys
// (HARMONIK_AGENT, crew-registry name, tmux name, --session-id) stay BARE and
// MUST NOT be derived from this label. (hk-igpg)
func JoinRemoteControlName(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "-" + name
}

// crewLaunchCtx carries the inputs to buildCrewLaunchSpec.
type crewLaunchCtx struct {
	// claudeBinary is the handler executable, resolved like daemon HandlerBinary
	// (daemon.go:99). Empty is normalised to "claude".
	claudeBinary string

	// name is the crew member identifier — used as the BARE HARMONIK_AGENT and as
	// the basis for the --remote-control title. Must be non-empty. The
	// --remote-control LABEL is JoinRemoteControlName(rcPrefix, name); HARMONIK_AGENT
	// stays the bare name (hk-igpg).
	name string

	// rcPrefix is the per-project Claude Code Remote-Control label prefix
	// (daemon.remote_control_prefix). Empty = bare label (today's behavior). It
	// is folded into the --remote-control title ONLY, via JoinRemoteControlName —
	// HARMONIK_AGENT, the crew-registry name, and --session-id stay bare (hk-igpg).
	rcPrefix string

	// sessionID is the caller-minted UUID for --session-id. Must be non-empty.
	// C2 mints it before calling this function and writes it to the crew
	// registry; there is no capture step here.
	sessionID string

	// projectDir is the harmonik project root directory: set as HARMONIK_PROJECT
	// and used as WorkDir so the crew session runs at the project root.
	projectDir string

	// resume, when true, builds argv with --resume <uuid> instead of
	// --session-id <uuid>. Used for stale re-launches (spec §7): the session
	// already exists on disk; --resume continues the same conversation without
	// forking a new session id.
	resume bool

	// model is the optional Claude model alias (opus | sonnet | haiku) read from
	// the mission handoff's `model:` front-matter field (specs/crew-handoff-schema.md
	// §4). When non-empty, --model <model> is appended to argv. Empty inherits the
	// compiled default (currently sonnet) — no flag is added.
	model string

	// harness is the resolved crew-scoped harness selection (hk-l63b9): "" or
	// "claude" builds today's Claude --remote-control spec unchanged; any other
	// value is a harness whose crew-orchestrator substrate isn't wired yet and
	// buildCrewLaunchSpec returns an explicit "not yet supported" error rather
	// than silently falling back to Claude. This is a crew-scoped resolution,
	// deliberately SEPARATE from core.AgentType (the per-bead worker taxonomy) —
	// a crew has no bead to carry a harness:<type> label.
	harness string
}

// crewHarnessClaude is the crew-scoped harness resolver's default value and the
// only harness buildCrewLaunchSpec currently knows how to build a spec for.
// hk-l63b9: deliberately NOT core.AgentTypeClaudeCode ("claude-code") — the crew
// harness resolver is a separate, substrate-neutral vocabulary from the per-bead
// worker harness taxonomy (see resolveCrewHarness doc comment).
const crewHarnessClaude = "claude"

// resolveCrewHarness implements the crew-scoped harness-selection precedence
// walk (hk-l63b9):
//
//	Tier 1 — flag: the --harness CLI override (CrewStartRequest.Harness)
//	Tier 2 — mission front-matter: the harness: field (readMissionHarness)
//	Tier 3 — per-crew config: .harmonik/config.yaml crews.<name>.harness
//	Tier 4 — default: "claude"
//
// This is NOT the worker per-bead resolveHarness (harnessresolve.go) — a crew
// has no bead to carry a harness:<type> label, so the tiers and their sources
// are entirely different. The returned value is not validated here; callers
// pass it to buildCrewLaunchSpec, which rejects anything but "claude" with an
// explicit error (no silent fallback).
func resolveCrewHarness(flagHarness, missionHarness, configHarness string) string {
	switch {
	case flagHarness != "":
		return flagHarness
	case missionHarness != "":
		return missionHarness
	case configHarness != "":
		return configHarness
	default:
		return crewHarnessClaude
	}
}

// buildCrewLaunchSpec constructs a handler.LaunchSpec for launching a
// persistent interactive crew session:
//
//	argv = [<claudeBinary> --dangerously-skip-permissions --remote-control "<name>" --session-id <uuid>]
//	env  = [HARMONIK_AGENT=<name>, HARMONIK_PROJECT=<projectDir>]
//
// --dangerously-skip-permissions is required so crew sessions don't wedge on
// mid-loop permission prompts (e.g. python3 monitor scripts) that would
// otherwise require captain to approve via tmux.
// No worktree. The caller mints and supplies sessionID; this function does not
// generate one.
//
// Spec ref: docs/plans/captain/05-specs/c2-spec.md §3.2, AC-5.
// Bead ref: hk-672di.
func buildCrewLaunchSpec(rc crewLaunchCtx) (handler.LaunchSpec, error) {
	if rc.name == "" {
		return handler.LaunchSpec{}, fmt.Errorf("buildCrewLaunchSpec: name must be non-empty")
	}
	if rc.sessionID == "" {
		return handler.LaunchSpec{}, fmt.Errorf("buildCrewLaunchSpec: sessionID must be non-empty")
	}
	// hk-l63b9: branch on the resolved crew harness. "" (unresolved callers,
	// e.g. existing tests) and "claude" both build today's Claude spec below,
	// unchanged. Any other harness has no crew-orchestrator substrate wired yet —
	// fail loud rather than silently falling back to Claude.
	if rc.harness != "" && rc.harness != crewHarnessClaude {
		return handler.LaunchSpec{}, fmt.Errorf("crew harness %q not yet supported", rc.harness)
	}

	binary := rc.claudeBinary
	if binary == "" {
		binary = "claude"
	}

	// The --remote-control LABEL folds in the per-project prefix via the shared
	// helper so the --resume and --session-id branches emit the SAME label (resume
	// parity, hk-igpg): a keeper clear→resume must not rename the picker session.
	rcLabel := JoinRemoteControlName(rc.rcPrefix, rc.name)

	var args []string
	if rc.resume {
		args = []string{"--dangerously-skip-permissions", "--remote-control", rcLabel, "--resume", rc.sessionID}
	} else {
		args = []string{"--dangerously-skip-permissions", "--remote-control", rcLabel, "--session-id", rc.sessionID}
	}

	// Optional per-crew model injection (specs/crew-handoff-schema.md §3): the
	// captain may pin a lane to a specific model via the mission `model:` field.
	// Empty inherits the compiled default (currently sonnet) — append nothing.
	if rc.model != "" {
		args = append(args, "--model", rc.model)
	}

	// hk-137y6: pin the crew's Go build cache to a FIXED per-agent path so a
	// crew that runs `go test` by hand inherits a bounded, non-purgeable cache
	// outside the daemon's reap — instead of following the hk-gjbpp guidance to
	// `GOCACHE=$(mktemp -d)`, which leaked a ~220 MiB cache per invocation until
	// the box fell under the disk watermark and dispatch silently stopped.
	env := []string{
		"HARMONIK_AGENT=" + rc.name,
		"HARMONIK_PROJECT=" + rc.projectDir,
	}
	env = append(env, GoCacheEnvFor(rc.projectDir, rc.name)...)

	return handler.LaunchSpec{
		Binary:  binary,
		Args:    args,
		Env:     env,
		WorkDir: rc.projectDir,
		Role:    "crew",
	}, nil
}
