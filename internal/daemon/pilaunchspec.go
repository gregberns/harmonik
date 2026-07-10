package daemon

// pilaunchspec.go — buildPiLaunchSpec helper (codename:pilot, hk-1c16h).
//
// Builds the argv/env spec for launching a Pi subprocess for any workflow phase:
//
//   - Initial turn (priorSessionID == nil):
//       pi --mode json --no-extensions --provider <prov> --model <prov/id> "<seed-prompt>"
//   - Resume turn (priorSessionID != nil):
//       pi --mode json --no-extensions --session <session-id> "<seed-prompt>"
//
// No --sandbox flag (Pi is unsandboxed — PI-015). WorkDir is set via
// LaunchSpec.WorkDir, NOT a -C flag. The API key MUST NOT be passed as
// --api-key (ps/argv leak); env injection only (PI-020).
// --no-extensions is always present (PI-022): Pi auto-loads .pi/extensions/*
// from the operator home; the flywheel extension calls kerf-next on every turn
// (fork bomb). Explicit -e paths still work.
//
// Env (buildPiEnv, PI-021 allowlist strip, review B1):
//
//   - Empty-overrides EVERY provider credential env var whose key is in the
//     maintained piProviderCredentialKeys table OR matches the *_API_KEY suffix
//     pattern, EXCEPT the operator-selected api_key_env. An enumerated denylist
//     cannot be complete against Pi's open provider set (MISTRAL_API_KEY,
//     GROQ_API_KEY, DEEPSEEK_API_KEY, … would survive); allowlist-strip is the
//     correct semantics.
//   - Injects ONLY the selected provider's key from the operator environment via
//     resolvePiAPIKeyValue.
//   - Sets shell rc-prompt suppression vars (oh-my-zsh anti-hang).
//
// resolvePiAPIKeyValue is the ONE shared key-resolution helper feeding BOTH
// buildPiEnv (for injection) and the billing guard (PI-040, pibillingguard.go)
// so they can never disagree about which value Pi receives at launch.
//
// Spec refs: specs/pi-harness.md §2 (PI-015, PI-020, PI-021).
// Design: ~/.kerf/projects/gregberns-harmonik/pilot/04-design/pi-harness-design.md §3.2.
// Codename: pilot. Bead: hk-1c16h.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// piProviderCredentialKeys is the maintained table of known provider API key
// environment variable names. All entries are empty-overridden in the Pi child
// environment EXCEPT the operator-selected api_key_env.
//
// The *_API_KEY suffix pattern in buildPiEnv handles keys not yet listed here
// (e.g. new providers), so forward-compatibility is preserved without requiring
// table updates. Together they enforce the PI-021 allowlist-strip invariant.
//
// Spec: PI-021 (allowlist strip); design §3.2.
var piProviderCredentialKeys = []string{
	"OPENROUTER_API_KEY",
	"ANTHROPIC_API_KEY",
	"OPENAI_API_KEY",
	"GEMINI_API_KEY",
	"GOOGLE_API_KEY",
	"MISTRAL_API_KEY",
	"GROQ_API_KEY",
	"DEEPSEEK_API_KEY",
	"COHERE_API_KEY",
	"XAI_API_KEY",
	"TOGETHER_API_KEY",
	"PERPLEXITY_API_KEY",
	"FIREWORKS_API_KEY",
	"AZURE_OPENAI_API_KEY",
	"CODEX_API_KEY",
}

// piSeedPromptTemplate is the seed prompt template passed to Pi as the
// positional task argument. It instructs Pi to read agent-task.md (written by
// the shared launch path before buildPiLaunchSpec is called), implement the
// task, and commit with the required Refs: trailer.
//
// The trailer instruction is load-bearing: harmonik detects bead completion by
// a git commit whose body carries an exact "Refs: <bead-id>" trailer line
// (workloop.go beadAlreadySubsumedInMain). Pi is unsandboxed (PI-015) so it
// can git-commit itself; ensurePiRefsTrailer (picommit.go) is the deterministic
// backstop for when Pi edits but does not produce a trailer-carrying commit.
//
// %s is replaced with the bead ID. Spec: PI-015.
const piSeedPromptTemplate = `Read .harmonik/agent-task.md to understand your task. Implement the changes described. When you are done, commit ALL your changes in a single git commit, and the commit message MUST include the line "Refs: %s" on its own line in the commit body. This trailer is required — without it the system cannot detect that your work is complete.`

// piRunCtx carries the per-launch inputs to buildPiLaunchSpec.
type piRunCtx struct {
	// piBinary is the pi executable path. Empty is normalised to "pi".
	piBinary string

	// workspacePath is the absolute path to the run worktree. Set as WorkDir in
	// the returned LaunchSpec. Pi's file tools (read/write/edit/bash/grep/find/ls)
	// operate relative to CWD; no -C flag (unlike codex exec). Required.
	workspacePath string

	// beadID is the bead correlation identifier embedded in the seed prompt's
	// Refs: trailer instruction. Required.
	beadID string

	// provider is the Pi provider string (from harnesses.pi.provider config).
	// Required on the initial turn. Ignored on resume turns.
	provider string

	// model is the Pi model string in "provider/id" form (from harnesses.pi.model).
	// Required on the initial turn. Ignored on resume turns.
	model string

	// apiKeyEnv is the name of the env var the Pi child expects for the provider
	// API key (from harnesses.pi.api_key_env). REQUIRED — name only, no secret.
	// The VALUE comes from apiKeyFile (when set) or the operator env (PI-020).
	apiKeyEnv string

	// apiKeyFile is the OPTIONAL path (pre-expanded by ResolvePiConfig) to a file
	// holding the raw provider API key. When non-empty, resolvePiAPIKeyValue reads
	// the file in preference to the ambient env. The daemon ambient env MUST NOT
	// carry the secret (PI-050/hk-xmfoi). Absent → fall back to ambient env.
	apiKeyFile string

	// baseURL is the OPTIONAL base URL for a locally-hosted OpenAI-compatible
	// endpoint (from harnesses.pi.base_url). When non-empty and this is the
	// initial turn, buildPiLaunchSpec generates a models.json under the run's
	// pi-agent dir and injects PI_CODING_AGENT_DIR into the child env. Absent =
	// today's cloud-provider behavior byte-for-byte unchanged. Bead: hk-z13jz.
	baseURL string

	// api is the OPTIONAL Pi wire-format string written into the generated
	// models.json "api" field. When empty and baseURL is set, defaults to "openai"
	// at launch time. Bead: hk-z13jz.
	api string

	// priorSessionID is non-nil for resume turns (iteration >= 2). It holds the
	// Pi session ID captured from the prior turn's first {"type":"session",...}
	// NDJSON line. Nil means this is the initial turn.
	priorSessionID *string

	// baseEnv is the base environment inherited from daemon Config.HandlerEnv.
	// buildPiEnv strips all credential keys (allowlist semantics, PI-021) and
	// injects only the selected provider's key.
	baseEnv []string

	// piHome is the Pi home directory used by the PI-042 billing guard check.
	// When empty, piDefaultHome() is used (production behaviour). Injectable
	// for tests to exercise the PI-042 deny path through buildPiLaunchSpec
	// without touching the real ~/.pi. Bead: hk-6g5iu.
	piHome string

	// billingEmitter, when non-nil, receives pi_billing_guard events from the
	// fail-closed billing guard (PI-040/PI-042/PI-043, pibillingguard.go). Nil
	// disables event emission; the guard's enforcement (fail-closed assert) still
	// runs regardless.
	billingEmitter handlercontract.EventEmitter

	// runID correlates the pi_billing_guard events with a run. May be the zero
	// (uuid.Nil) RunID when the spec is built before a run_id is minted, in which
	// case the events are emitted run-unscoped.
	runID core.RunID

	// skipBillingGuard disables the pre-flight billing guard (PI-040,
	// pibillingguard.go). Exists SOLELY so unit tests that exercise argv/env
	// shape do not require a real api key in the environment. Production callers
	// MUST leave it false so the fail-closed guard runs.
	skipBillingGuard bool
}

// resolvePiAPIKeyValue reads the Pi API key value, preferring an explicit file
// (api_key_file, PI-050/hk-xmfoi) over the ambient env (api_key_env).
//
// This is the ONE shared key-resolution helper: BOTH buildPiEnv (for key
// injection into the child env) and the billing guard (PI-040, pibillingguard.go,
// for the fail-closed pre-flight assert) MUST call this function so they can
// never disagree about which value Pi receives at launch.
//
// Precedence: apiKeyFile (when non-empty) > ambient env named by apiKeyEnv.
// File content is trimmed of leading/trailing whitespace. An empty return means
// the key is absent from both sources. The billing guard treats absence as a
// launch refusal (PI-040 fail closed).
//
// Spec: PI-021 (allowlist strip); PI-040 (fail-closed guard); PI-050 (api_key_file).
func resolvePiAPIKeyValue(apiKeyFile, apiKeyEnv string) string {
	if apiKeyFile != "" {
		data, err := os.ReadFile(apiKeyFile)
		if err == nil {
			if v := strings.TrimSpace(string(data)); v != "" {
				return v
			}
		}
	}
	return os.Getenv(apiKeyEnv)
}

// buildPiLaunchSpec constructs a handler.LaunchSpec for launching a Pi
// subprocess for one turn (initial or resume).
//
// The returned spec is suitable for passing directly to handler.Launch. The
// caller is responsible for writing agent-task.md into the worktree before
// calling this function (the spec does not write it).
//
// Spec: PI-015, PI-020, PI-021.
func buildPiLaunchSpec(rc piRunCtx) (handler.LaunchSpec, error) {
	if rc.workspacePath == "" {
		return handler.LaunchSpec{}, fmt.Errorf(
			"buildPiLaunchSpec: workspacePath must be non-empty")
	}
	if rc.beadID == "" {
		return handler.LaunchSpec{}, fmt.Errorf(
			"buildPiLaunchSpec: beadID must be non-empty")
	}
	if rc.apiKeyEnv == "" {
		return handler.LaunchSpec{}, fmt.Errorf(
			"Pi harness: refusing to start — harnesses.pi config is absent or incomplete; " +
				"missing: harnesses.pi.api_key_env. " +
				"Fix: run 'harmonik pi config --example' to print a complete harnesses.pi: block, " +
				"then add it to .harmonik/config.yaml. " +
				"(R1 de-hardcode mandate: the product imposes ZERO baked Pi defaults.)")
	}
	if rc.priorSessionID != nil && *rc.priorSessionID == "" {
		return handler.LaunchSpec{}, fmt.Errorf(
			"buildPiLaunchSpec: priorSessionID must not be an empty string (pass nil for initial turn)")
	}
	if rc.priorSessionID == nil {
		if rc.provider == "" {
			return handler.LaunchSpec{}, fmt.Errorf(
				"Pi harness: refusing to start — harnesses.pi config is absent or incomplete; " +
					"missing: harnesses.pi.provider. " +
					"Fix: run 'harmonik pi config --example' to print a complete harnesses.pi: block, " +
					"then add it to .harmonik/config.yaml.")
		}
		if rc.model == "" {
			return handler.LaunchSpec{}, fmt.Errorf(
				"Pi harness: refusing to start — harnesses.pi config is absent or incomplete; " +
					"missing: harnesses.pi.model. " +
					"Fix: run 'harmonik pi config --example' to print a complete harnesses.pi: block, " +
					"then add it to .harmonik/config.yaml.")
		}
	}

	binary := rc.piBinary
	if binary == "" {
		binary = "pi"
	}

	// Build argv.
	//
	// Initial: pi --mode json --no-extensions --provider <prov> --model <prov/id> "<seed>"
	// Resume:  pi --mode json --no-extensions --session <session-id> "<seed>"
	//
	// No --sandbox flag: Pi is unsandboxed (PI-015). WorkDir in the returned
	// LaunchSpec sets the subprocess CWD; no -C flag. The key is NEVER passed
	// as --api-key (PI-020 — ps/argv leak).
	// --no-extensions (PI-022): suppresses .pi/extensions/* auto-load so the
	// flywheel extension cannot call kerf-next and fork-bomb the daemon.
	seedPrompt := fmt.Sprintf(piSeedPromptTemplate, rc.beadID)
	var args []string
	if rc.priorSessionID != nil {
		args = []string{
			"--mode", "json",
			"--no-extensions",
			"--session", *rc.priorSessionID,
			seedPrompt,
		}
	} else {
		args = []string{
			"--mode", "json",
			"--no-extensions",
			"--provider", rc.provider,
			"--model", rc.model,
			seedPrompt,
		}
	}

	// Build env: allowlist-strip all provider credential keys except the selected
	// one, then inject only the selected provider's key (PI-021/PI-050).
	// apiKeyFile takes precedence over the ambient env when set (file-first).
	env := buildPiEnv(rc.baseEnv, rc.apiKeyFile, rc.apiKeyEnv)

	// Pre-flight billing guard (PI-040/PI-042/PI-043, pibillingguard.go).
	// Fail-closed: absent/empty provider key → error → launch refused BEFORE
	// agent_ready. Also checks for a persisted on-disk credential (PI-042).
	// skipBillingGuard is false in production (see piRunCtx); tests that only
	// exercise argv/env shape set it to avoid requiring a real key.
	if !rc.skipBillingGuard {
		piHome := rc.piHome
		if piHome == "" {
			piHome = piDefaultHome()
		}
		if err := runPiBillingGuard(context.Background(), rc.billingEmitter, rc.runID, rc.beadID, rc.apiKeyFile, rc.apiKeyEnv, piHome); err != nil {
			return handler.LaunchSpec{}, err
		}
	}

	// base_url passthrough (hk-z13jz): when baseURL is set AND this is the
	// initial turn (priorSessionID == nil), generate a models.json so Pi can
	// target the locally-hosted OpenAI-compatible endpoint.
	//
	// The generated models.json is written to a deterministic per-run pi-agent
	// dir under the run worktree (<workspacePath>/.harmonik/pi-agent/). Pi
	// reads it via PI_CODING_AGENT_DIR, which is injected into the child env
	// only (never argv — mirror the api-key injection pattern). When baseURL is
	// absent this block is a no-op: today's behavior unchanged.
	if rc.baseURL != "" && rc.priorSessionID == nil {
		piAgentDir := filepath.Join(rc.workspacePath, ".harmonik", "pi-agent")
		if mkdirErr := os.MkdirAll(piAgentDir, 0o755); mkdirErr != nil {
			return handler.LaunchSpec{}, fmt.Errorf(
				"buildPiLaunchSpec: create pi-agent dir %q: %w", piAgentDir, mkdirErr)
		}
		modelsJSON, buildErr := buildPiModelsJSON(rc.provider, rc.baseURL, rc.api, rc.apiKeyFile, rc.apiKeyEnv, rc.model)
		if buildErr != nil {
			return handler.LaunchSpec{}, fmt.Errorf(
				"buildPiLaunchSpec: build models.json: %w", buildErr)
		}
		modelsPath := filepath.Join(piAgentDir, "models.json")
		if writeErr := os.WriteFile(modelsPath, modelsJSON, 0o644); writeErr != nil {
			return handler.LaunchSpec{}, fmt.Errorf(
				"buildPiLaunchSpec: write models.json to %q: %w", modelsPath, writeErr)
		}
		env = append(env, "PI_CODING_AGENT_DIR="+piAgentDir)
	}

	return handler.LaunchSpec{
		Binary:       binary,
		Args:         args,
		Env:          env,
		WorkDir:      rc.workspacePath,
		Role:         "implementer",
		StdinDevNull: true, // PI-020 / #4303: Pi (ProcessExit) may hang on pane PTY stdin with /dev/null
	}, nil
}

// buildPiModelsJSON generates the models.json content for a Pi agent dir targeting
// a locally-hosted OpenAI-compatible endpoint (hk-z13jz). The structure follows
// Pi's ModelsConfigSchema (core/model-registry.js) for a custom provider:
//
//	{"providers":{"<provider>":{"baseUrl":"<baseURL>","api":"<api>","apiKey":"<key>","models":[{"id":"<modelID>"}]}}}
//
// api defaults to "openai" when empty. modelID is the substring of model after
// the last "/" (whole string when no "/"). The key value is resolved via
// resolvePiAPIKeyValue (file-first, env fallback — same as buildPiEnv).
func buildPiModelsJSON(provider, baseURL, api, apiKeyFile, apiKeyEnv, model string) ([]byte, error) {
	if api == "" {
		api = "openai"
	}
	modelID := model
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		modelID = model[idx+1:]
	}
	apiKeyValue := resolvePiAPIKeyValue(apiKeyFile, apiKeyEnv)
	type modelEntry struct {
		ID string `json:"id"`
	}
	type providerConfig struct {
		BaseURL string       `json:"baseUrl"`
		API     string       `json:"api"`
		APIKey  string       `json:"apiKey"`
		Models  []modelEntry `json:"models"`
	}
	payload := struct {
		Providers map[string]providerConfig `json:"providers"`
	}{
		Providers: map[string]providerConfig{
			provider: {
				BaseURL: baseURL,
				API:     api,
				APIKey:  apiKeyValue,
				Models:  []modelEntry{{ID: modelID}},
			},
		},
	}
	return json.Marshal(payload)
}

// buildPiEnv constructs the Pi child environment from baseEnv.
//
// Allowlist-strip semantics (PI-021, review B1):
//
//  1. Every key in the maintained piProviderCredentialKeys table EXCEPT the
//     selected apiKeyEnv is added to strippedSet and emitted as KEY= (empty
//     override). This pre-seeds from the full table to guard against the tmux
//     server's additive -e mechanism, which would inject a key from the server
//     env even if it is absent from baseEnv.
//  2. Any additional *_API_KEY key found in baseEnv (not in the maintained table,
//     not apiKeyEnv) is also added to strippedSet and emitted as KEY=. This
//     catches provider keys not yet in the maintained table (e.g. a future
//     HYPOTHETICAL_API_KEY).
//  3. Non-credential baseEnv entries are passed through unchanged.
//  4. Only the selected provider's key (apiKeyEnv) is injected: the VALUE is
//     resolved via resolvePiAPIKeyValue (file-first, env fallback — PI-050).
//
// An enumerated denylist (codex's 2-key approach) cannot be complete against
// Pi's open provider set; this allowlist approach is correct belt-and-suspenders
// regardless of whether Pi auto-detects a provider from env.
func buildPiEnv(baseEnv []string, apiKeyFile, apiKeyEnv string) []string {
	// knownCredSet: the maintained table for O(1) lookup.
	knownCredSet := make(map[string]bool, len(piProviderCredentialKeys))
	for _, k := range piProviderCredentialKeys {
		knownCredSet[k] = true
	}

	// strippedSet: all keys to empty-override. Pre-seeded with the full
	// maintained table (EXCEPT apiKeyEnv) to handle the tmux additive -e path.
	strippedSet := make(map[string]bool, len(piProviderCredentialKeys))
	for k := range knownCredSet {
		if k != apiKeyEnv {
			strippedSet[k] = true
		}
	}
	// Extend with any *_API_KEY vars in baseEnv not already in the table.
	for _, kv := range baseEnv {
		key := envKey(kv)
		if key == apiKeyEnv {
			continue
		}
		if !knownCredSet[key] && isPiAPIKeyPattern(key) {
			strippedSet[key] = true
		}
	}

	env := make([]string, 0, len(baseEnv)+len(strippedSet)+4)

	// Pass through non-credential, non-apiKeyEnv entries from baseEnv.
	hasPath := false
	for _, kv := range baseEnv {
		key := envKey(kv)
		if key == apiKeyEnv || strippedSet[key] {
			continue
		}
		if key == "PATH" {
			hasPath = true
		}
		env = append(env, kv)
	}

	// Guarantee a working PATH (hk-6atjk / codename:pi-model-leak). The exec
	// substrate FULLY replaces the child environment with this env (handler.go
	// cmd.Env = spec.Env), and cfg.HandlerEnv is not populated at the composition
	// root, so baseEnv can arrive with NO PATH. Without one, the pi CLI's
	// `#!/usr/bin/env node` shebang resolves against the libc default PATH
	// (/usr/bin:/bin) — which excludes /opt/homebrew/bin where node lives — and
	// the child dies with `env: node: No such file or directory` (exit 127)
	// before HEAD advances. Fall back to the daemon process PATH only when
	// baseEnv did not already carry one (existing PATH is preserved above). PATH
	// is not a credential, so this does not weaken the PI-021 allowlist-strip.
	if !hasPath {
		if procPath := os.Getenv("PATH"); procPath != "" {
			env = append(env, "PATH="+procPath)
		}
	}

	// Emit empty overrides for all stripped credential keys (PI-021 / CI-INV-002
	// pattern). The tmux server's additive -e mechanism means merely omitting a
	// key leaves the server env value intact; only KEY= zeros it.
	for k := range strippedSet {
		env = append(env, k+"=")
	}

	// Inject ONLY the selected provider's key (PI-021). resolvePiAPIKeyValue is
	// the shared helper: the billing guard (PI-040) calls the same function to
	// verify presence, so both agree on the exact value Pi receives.
	// File-first precedence: apiKeyFile (when set) > ambient env (PI-050).
	apiKeyValue := resolvePiAPIKeyValue(apiKeyFile, apiKeyEnv)
	env = append(env, apiKeyEnv+"="+apiKeyValue)

	// Shell rc-prompt suppression (oh-my-zsh anti-hang — same as codex harness,
	// hk-5s6re). Pi spawns through the exec substrate; the launch shell can still
	// source ~/.zshrc and hang at an interactive update prompt.
	env = append(env,
		"DISABLE_AUTO_UPDATE=true",
		"DISABLE_UPDATE_PROMPT=true",
	)

	return env
}

// isPiAPIKeyPattern returns true when the env var name ends with "_API_KEY" —
// the catch-all suffix for provider credential variables not yet listed in
// piProviderCredentialKeys. Prevents forward-compat gaps (new providers).
//
// Spec: PI-021 (allowlist strip).
func isPiAPIKeyPattern(key string) bool {
	return strings.HasSuffix(key, "_API_KEY")
}
