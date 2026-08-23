package codex

import (
	"context"
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
)

var codexCredentialDenyKeys = []string{
	"OPENAI_API_KEY",
	"CODEX_API_KEY",
}

const codexSeedPromptTemplate = `Read .harmonik/agent-task.md to understand your task. Implement the changes described. When you are done, commit ALL your changes in a single git commit, and the commit message MUST include the line "Refs: %s" on its own line in the commit body. This trailer is required — without it the system cannot detect that your work is complete.`

// RunCtx carries the per-launch inputs to BuildLaunchSpec.
type RunCtx struct {
	// CodexBinary is the codex executable path. Empty is normalised to "codex".
	CodexBinary string

	// WorkspacePath is the absolute path to the worktree (-C flag).
	WorkspacePath string

	// BeadID is the bead correlation identifier, embedded in the seed prompt's
	// Refs: trailer instruction and in the WorkDir.
	BeadID string

	// Model is the codex model name passed as --model on the initial turn
	// (e.g. "o4-mini", "o3"). OPTIONAL: an empty model means "launch with no
	// --model flag", so codex resolves the model from $CODEX_HOME/config.toml —
	// i.e. the account default. This is the ONLY working configuration on a
	// ChatGPT-subscription auth box, which specs/harness-contract.md HN-022
	// mandates as codex's default path: under that auth every explicitly-named
	// model (o4-mini, gpt-5, gpt-5-codex, …) is rejected with HTTP 400
	// "not supported when using Codex with a ChatGPT account" (verified
	// 2026-07-16), and only omitting --model succeeds. Ignored on resume turns
	// (the thread context already encodes the model).
	//
	// hk-heh3t history: on codex 0.139.0 an omitted --model hung on stdin for
	// ~30 min, so this field was once REQUIRED (empty → fail loud). That hang no
	// longer reproduces on the pinned codex (0.142.5 verified: a --model-less
	// `codex exec` completes in seconds and edits the tree), and requiring a
	// model directly contradicts the HN-022 ChatGPT path — so the guard is
	// retired. The never-spawned reaper (stalewatch neverSpawnedTimeout) remains
	// the backstop if a future codex version ever re-hangs on an omitted --model.
	//
	// Operator caveat: a live empty-model run still depends on the ChatGPT account
	// default being a model the installed codex-cli can serve — a rotating default
	// (e.g. gpt-5.6-sol vs codex-cli 0.142.5) can 400. See
	// scenarios/core-loop-proof/known-red.md §"Operator caveat" (COORD c072).
	Model string

	// PriorThreadID is non-nil for resume turns (iteration >= 2). It holds the
	// codex thread_id captured from the prior turn's first thread.started event.
	// Nil means this is the initial turn.
	PriorThreadID *string

	// IterationCount is the 1-based DOT iteration index. On a resume turn
	// (PriorThreadID != nil) it selects the reviewer-feedback.iter-<N-1>.md the
	// resume seed prompt points at. Zero on the initial turn. See
	// implementerResumeSeedPrompt (agentseedprompt.go).
	IterationCount int

	// BaseEnv is the base environment inherited from daemon Config.HandlerEnv.
	// codexCredentialDenyKeys are stripped and re-emitted as empty overrides.
	// CODEX_HOME is set to CodexHome (overwriting any prior value).
	BaseEnv []string

	// CodexHome is the path written to CODEX_HOME. Empty is normalised to
	// "$HOME/.codex" (using os.UserHomeDir). A non-writable path is not
	// validated here; the pre-flight billing guard (C3/T11) enforces that.
	CodexHome string

	// BillingEmitter, when non-nil, receives codex_billing_guard events from the
	// positive billing guard (C3/T11). Nil disables event emission; the guard's
	// enforcement (materialize + fail-closed assert) still runs regardless.
	BillingEmitter handlercontract.EventEmitter

	// RunID correlates the codex_billing_guard events with a run. May be the zero
	// (uuid.Nil) RunID when the spec is built before a run_id is minted, in which
	// case the events are emitted run-unscoped.
	RunID core.RunID

	// SkipBillingGuard disables the positive billing guard (C3/T11). It exists
	// solely so unit tests that only exercise argv/env shape do not have to
	// materialize a config.toml. Production callers MUST leave it false so the
	// fail-closed guard runs.
	SkipBillingGuard bool
}

// BuildLaunchSpec constructs a handler.LaunchSpec for launching a codex
// subprocess for one turn (initial or resume).
//
// The returned spec is suitable for passing directly to handler.Launch. The
// caller is responsible for writing agent-task.md into the worktree before
// calling this function (the spec does not write it).
//
// Spec: C2-codex-adapter-spec.md §Approach; C3-auth-billing-spec.md §Approach.
func BuildLaunchSpec(rc RunCtx) (handler.LaunchSpec, error) {
	if rc.WorkspacePath == "" {
		return handler.LaunchSpec{}, fmt.Errorf(
			"BuildLaunchSpec: workspacePath must be non-empty")
	}
	if rc.BeadID == "" {
		return handler.LaunchSpec{}, fmt.Errorf(
			"BuildLaunchSpec: beadID must be non-empty")
	}
	if rc.PriorThreadID != nil && *rc.PriorThreadID == "" {
		return handler.LaunchSpec{}, fmt.Errorf(
			"BuildLaunchSpec: priorThreadID must not be an empty string (pass nil for initial turn)")
	}

	binary := rc.CodexBinary
	if binary == "" {
		binary = "codex"
	}

	seedPrompt := fmt.Sprintf(codexSeedPromptTemplate, rc.BeadID)
	var args []string
	if rc.PriorThreadID != nil {
		seedPrompt = shared.ImplementerResumeSeedPrompt(rc.BeadID, rc.IterationCount-1)
		args = []string{"exec", "resume", *rc.PriorThreadID, "--json", "-c", `sandbox_mode="danger-full-access"`}
		args = append(args, seedPrompt)
	} else {
		args = []string{"exec", "--json", "-c", `sandbox_mode="danger-full-access"`}
		if rc.Model != "" {
			args = append(args, "--model", rc.Model)
		}
		args = append(args, "-C", rc.WorkspacePath, seedPrompt)
	}

	env := buildCodexEnv(rc.BaseEnv, rc.CodexHome)

	if !rc.SkipBillingGuard {
		guardedHome := resolveCodexHome(rc.CodexHome)
		if err := runCodexBillingGuard(context.Background(), rc.BillingEmitter, rc.RunID, rc.BeadID, guardedHome); err != nil {
			return handler.LaunchSpec{}, err
		}
	}

	return handler.LaunchSpec{
		Binary:       binary,
		Args:         args,
		Env:          env,
		WorkDir:      rc.WorkspacePath,
		Role:         "implementer",
		StdinDevNull: true, // hk-rpr6: codex (ProcessExit) blocks on pane PTY stdin without EOF
	}, nil
}

func resolveCodexHome(codexHome string) string {
	if codexHome != "" {
		return codexHome
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "$HOME/.codex"
	}
	return home + "/.codex"
}

func buildCodexEnv(baseEnv []string, codexHome string) []string {
	resolvedCodexHome := resolveCodexHome(codexHome)

	denySet := make(map[string]bool, len(codexCredentialDenyKeys))
	for _, k := range codexCredentialDenyKeys {
		denySet[k] = true
	}

	env := make([]string, 0, len(baseEnv)+len(codexCredentialDenyKeys)+1)

	hasPath := false
	for _, kv := range baseEnv {
		key := shared.EnvKey(kv)
		if denySet[key] || key == "CODEX_HOME" {
			continue
		}
		if key == "PATH" {
			hasPath = true
		}
		env = append(env, kv)
	}

	if !hasPath {
		if procPath := os.Getenv("PATH"); procPath != "" {
			env = append(env, "PATH="+procPath)
		}
	}

	for _, k := range codexCredentialDenyKeys {
		env = append(env, k+"=")
	}

	env = append(env,
		"CODEX_HOME="+resolvedCodexHome,
		"DISABLE_AUTO_UPDATE=true",
		"DISABLE_UPDATE_PROMPT=true",
	)

	return env
}
