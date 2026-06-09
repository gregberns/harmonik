package daemon

// codexlaunchspec.go — buildCodexLaunchSpec helper (codex-harness C2/T7, hk-rgxwd).
//
// Builds the argv/env spec for launching a codex subprocess for any workflow
// phase:
//
//   - Initial turn (priorThreadID == nil):
//       codex exec --json --sandbox workspace-write -a never -C <worktree> <seed-prompt>
//   - Resume turn (priorThreadID != nil):
//       codex exec resume <thread_id> --json --sandbox workspace-write -a never -C <worktree> <seed-prompt>
//
// The seed prompt instructs codex to read .harmonik/agent-task.md, implement
// the task, and commit with a "Refs: <beadID>" trailer.
//
// Env:
//   - Strips OPENAI_API_KEY and CODEX_API_KEY from baseEnv and re-emits them as
//     empty overrides so the tmux server's additive -e mechanism cannot leak live
//     keys (C3 credential-strip, AC3.1).
//   - Sets CODEX_HOME to codexHome (default: "$HOME/.codex") so token refresh
//     works and the pre-flight billing guard can read auth state (AC3.4).
//
// Spec refs:
//   - .kerf/works/codex-harness/05-specs/C2-codex-adapter-spec.md
//   - .kerf/works/codex-harness/05-specs/C3-auth-billing-spec.md
//
// Bead: hk-rgxwd [C2/T7]

import (
	"fmt"
	"os"
	"strings"

	"github.com/gregberns/harmonik/internal/handler"
)

// codexCredentialDenyKeys lists the credential environment variable names that
// MUST be stripped from the codex child environment and re-emitted as empty
// overrides. The tmux server's additive -e mechanism means merely omitting a
// key leaves the server env value intact; only an explicit KEY= zeros it.
//
// Spec: C3-auth-billing-spec.md AC3.1; specs/harness-contract.md §2 N1.
var codexCredentialDenyKeys = []string{
	"OPENAI_API_KEY",
	"CODEX_API_KEY",
}

// codexSeedPromptTemplate is the seed prompt template passed to `codex exec` as
// a positional argument. It instructs codex to read agent-task.md (written by
// the shared launch path before buildCodexLaunchSpec is called), implement the
// task, and commit with the required Refs: trailer so the daemon's
// commit-detection path can confirm the work landed.
//
// The trailer instruction is load-bearing: harmonik detects bead completion by a
// git commit whose body carries an exact "Refs: <bead-id>" trailer line
// (workloop.go beadAlreadySubsumedInMain). The instruction is deliberately
// explicit — single work commit, trailer on its own line in the body — to
// maximise the chance codex obeys it. The codex commit-after-exit fallback
// (codexcommit.go ensureCodexRefsTrailer) is the deterministic backstop for when
// codex edits files but does not produce a trailer-carrying commit; this prompt
// is the happy-path INSTRUCT half of the T9 guarantee (hk-bpxci).
//
// %s is replaced with the bead ID.
const codexSeedPromptTemplate = `Read .harmonik/agent-task.md to understand your task. Implement the changes described. When you are done, commit ALL your changes in a single git commit, and the commit message MUST include the line "Refs: %s" on its own line in the commit body. This trailer is required — without it the system cannot detect that your work is complete.`

// codexRunCtx carries the per-launch inputs to buildCodexLaunchSpec.
type codexRunCtx struct {
	// codexBinary is the codex executable path. Empty is normalised to "codex".
	codexBinary string

	// workspacePath is the absolute path to the worktree (-C flag).
	workspacePath string

	// beadID is the bead correlation identifier, embedded in the seed prompt's
	// Refs: trailer instruction and in the WorkDir.
	beadID string

	// priorThreadID is non-nil for resume turns (iteration >= 2). It holds the
	// codex thread_id captured from the prior turn's first thread.started event.
	// Nil means this is the initial turn.
	priorThreadID *string

	// baseEnv is the base environment inherited from daemon Config.HandlerEnv.
	// codexCredentialDenyKeys are stripped and re-emitted as empty overrides.
	// CODEX_HOME is set to codexHome (overwriting any prior value).
	baseEnv []string

	// codexHome is the path written to CODEX_HOME. Empty is normalised to
	// "$HOME/.codex" (using os.UserHomeDir). A non-writable path is not
	// validated here; the pre-flight billing guard (C3/T11) enforces that.
	codexHome string
}

// buildCodexLaunchSpec constructs a handler.LaunchSpec for launching a codex
// subprocess for one turn (initial or resume).
//
// The returned spec is suitable for passing directly to handler.Launch. The
// caller is responsible for writing agent-task.md into the worktree before
// calling this function (the spec does not write it).
//
// Spec: C2-codex-adapter-spec.md §Approach; C3-auth-billing-spec.md §Approach.
func buildCodexLaunchSpec(rc codexRunCtx) (handler.LaunchSpec, error) {
	if rc.workspacePath == "" {
		return handler.LaunchSpec{}, fmt.Errorf(
			"buildCodexLaunchSpec: workspacePath must be non-empty")
	}
	if rc.beadID == "" {
		return handler.LaunchSpec{}, fmt.Errorf(
			"buildCodexLaunchSpec: beadID must be non-empty")
	}
	if rc.priorThreadID != nil && *rc.priorThreadID == "" {
		return handler.LaunchSpec{}, fmt.Errorf(
			"buildCodexLaunchSpec: priorThreadID must not be an empty string (pass nil for initial turn)")
	}

	binary := rc.codexBinary
	if binary == "" {
		binary = "codex"
	}

	// Build argv.
	// Initial:  codex exec --json --sandbox workspace-write -a never -C <wt> <seed>
	// Resume:   codex exec resume <thread_id> --json --sandbox workspace-write -a never -C <wt> <seed>
	seedPrompt := fmt.Sprintf(codexSeedPromptTemplate, rc.beadID)
	var args []string
	if rc.priorThreadID != nil {
		args = []string{
			"exec", "resume", *rc.priorThreadID,
			"--json",
			"--sandbox", "workspace-write",
			"-a", "never",
			"-C", rc.workspacePath,
			seedPrompt,
		}
	} else {
		args = []string{
			"exec",
			"--json",
			"--sandbox", "workspace-write",
			"-a", "never",
			"-C", rc.workspacePath,
			seedPrompt,
		}
	}

	// Build env: copy baseEnv, strip credential keys, set CODEX_HOME.
	env := buildCodexEnv(rc.baseEnv, rc.codexHome)

	return handler.LaunchSpec{
		Binary:  binary,
		Args:    args,
		Env:     env,
		WorkDir: rc.workspacePath,
		Role:    "implementer",
	}, nil
}

// buildCodexEnv constructs the codex child environment from baseEnv.
//
//   - Strips OPENAI_API_KEY and CODEX_API_KEY, re-emitting them as empty
//     overrides so the tmux server's additive -e cannot leak live keys (C3 AC3.1).
//   - Sets CODEX_HOME to codexHome (empty → "$HOME/.codex"). If os.UserHomeDir
//     fails, the fallback is the literal "$HOME/.codex" string; the pre-flight
//     billing guard in C3/T11 is the backstop for a misconfigured home directory.
//   - Preserves all other baseEnv entries unchanged.
func buildCodexEnv(baseEnv []string, codexHome string) []string {
	// Resolve CODEX_HOME before iterating baseEnv.
	resolvedCodexHome := codexHome
	if resolvedCodexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			// Fallback: literal string; the billing-guard pre-flight (C3/T11) will
			// catch a bad CODEX_HOME before any real codex run launches.
			resolvedCodexHome = "$HOME/.codex"
		} else {
			resolvedCodexHome = home + "/.codex"
		}
	}

	denySet := make(map[string]bool, len(codexCredentialDenyKeys))
	for _, k := range codexCredentialDenyKeys {
		denySet[k] = true
	}

	// Allocate with capacity for baseEnv + deny-key empty overrides + CODEX_HOME.
	env := make([]string, 0, len(baseEnv)+len(codexCredentialDenyKeys)+1)

	// Copy non-credential, non-CODEX_HOME entries from baseEnv.
	for _, kv := range baseEnv {
		key := envKey(kv)
		if denySet[key] || key == "CODEX_HOME" {
			continue
		}
		env = append(env, kv)
	}

	// Emit empty overrides for credential keys (C3 AC3.1 / CI-INV-002 pattern).
	for _, k := range codexCredentialDenyKeys {
		env = append(env, k+"=")
	}

	// Set CODEX_HOME (C3 AC3.4).
	env = append(env, "CODEX_HOME="+resolvedCodexHome)

	// Shell rc-prompt suppression (hk-5s6re). The codex harness spawns through
	// the same tmux substrate as the claude harness, so its pane shell is the
	// same interactive login zsh that sources the operator's ~/.zshrc and can
	// hang at an oh-my-zsh `[Y/n] Would you like to update?` prompt — the spawn
	// wedge described in ClaudeEnvVars. Injecting the same disable vars makes the
	// prompt structurally unable to fire here too. Additive env only; never
	// touches PATH/shell/aliases (see ClaudeEnvVars for the full rationale).
	env = append(env,
		"DISABLE_AUTO_UPDATE=true",
		"DISABLE_UPDATE_PROMPT=true",
	)

	return env
}

// envKey returns the key portion of a "KEY=VALUE" environment entry.
// Returns the whole string if no "=" is present.
func envKey(kv string) string {
	idx := strings.IndexByte(kv, '=')
	if idx < 0 {
		return kv
	}
	return kv[:idx]
}
