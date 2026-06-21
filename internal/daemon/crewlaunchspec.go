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

	env := []string{
		"HARMONIK_AGENT=" + rc.name,
		"HARMONIK_PROJECT=" + rc.projectDir,
	}

	return handler.LaunchSpec{
		Binary:  binary,
		Args:    args,
		Env:     env,
		WorkDir: rc.projectDir,
		Role:    "crew",
	}, nil
}
