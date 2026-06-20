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

// crewLaunchCtx carries the inputs to buildCrewLaunchSpec.
type crewLaunchCtx struct {
	// claudeBinary is the handler executable, resolved like daemon HandlerBinary
	// (daemon.go:99). Empty is normalised to "claude".
	claudeBinary string

	// name is the crew member identifier — used as the --remote-control title
	// and as HARMONIK_AGENT. Must be non-empty.
	name string

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

	var args []string
	if rc.resume {
		args = []string{"--dangerously-skip-permissions", "--remote-control", rc.name, "--resume", rc.sessionID}
	} else {
		args = []string{"--dangerously-skip-permissions", "--remote-control", rc.name, "--session-id", rc.sessionID}
	}

	// Optional per-crew model injection (specs/crew-handoff-schema.md §4): the
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
