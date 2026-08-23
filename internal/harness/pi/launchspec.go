package pi

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
	"github.com/gregberns/harmonik/internal/harness/shared"
)

const (
	piConfigMissingAPIKeyMessage = "Pi harness: refusing to start — harnesses.pi config is absent or incomplete; " +
		"missing: harnesses.pi.api_key_env. " +
		"Fix: run 'harmonik pi config --example' to print a complete harnesses.pi: block, " +
		"then add it to .harmonik/config.yaml. " +
		"(R1 de-hardcode mandate: the product imposes ZERO baked Pi defaults.)"
	piConfigMissingProviderMessage = "Pi harness: refusing to start — harnesses.pi config is absent or incomplete; " +
		"missing: harnesses.pi.provider. " +
		"Fix: run 'harmonik pi config --example' to print a complete harnesses.pi: block, " +
		"then add it to .harmonik/config.yaml."
	piConfigMissingModelMessage = "Pi harness: refusing to start — harnesses.pi config is absent or incomplete; " +
		"missing: harnesses.pi.model. " +
		"Fix: run 'harmonik pi config --example' to print a complete harnesses.pi: block, " +
		"then add it to .harmonik/config.yaml."
)

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

const piSeedPromptTemplate = `Read .harmonik/agent-task.md to understand your task. Implement the changes described. When you are done, commit ALL your changes in a single git commit, and the commit message MUST include the line "Refs: %s" on its own line in the commit body. This trailer is required — without it the system cannot detect that your work is complete.`

// RunCtx carries the per-launch inputs to BuildLaunchSpec.
//
// It is EXPORTED, along with BuildLaunchSpec, for one reason and it is not
// "callers might want it": a Go export_test.go seam is visible only inside its
// own package's test binary, so internal/daemon/export_test.go cannot alias a
// shim living here — it can only alias something genuinely exported. Two
// cross-harness parity tables in package daemon_test
// (crossharness_empty_model_test.go pins the codex-vs-pi empty-model asymmetry;
// crossharness_seedprompt_test.go pins resume-seed-prompt parity) assert about
// BOTH harnesses in ONE table each, so neither can move into package pi_test,
// and neither can reach SkipBillingGuard through the handlercontract.Harness
// seam — Harness.LaunchSpec builds this struct without it, so the fail-closed
// PI-040 guard would run and error without a real provider key. Splitting those
// tables would destroy the only place each asymmetry/parity claim is pinned
// together. Codex made the identical call in E1a (codex.RunCtx).
//
// Production callers do NOT use this type: they go through the registry-resolved
// handlercontract.Harness. When the two parity tables move to a cross-harness
// test package, RunCtx and BuildLaunchSpec should be un-exported again.
type RunCtx struct {
	// PiBinary is the pi executable path. Empty is normalised to "pi".
	PiBinary string

	// WorkspacePath is the absolute path to the run worktree. Set as WorkDir in
	// the returned LaunchSpec. Pi's file tools (read/write/edit/bash/grep/find/ls)
	// operate relative to CWD; no -C flag (unlike codex exec). Required.
	WorkspacePath string

	// BeadID is the bead correlation identifier embedded in the seed prompt's
	// Refs: trailer instruction. Required.
	BeadID string

	// Provider is the Pi provider string (from harnesses.pi.provider config).
	// Required on the initial turn. Ignored on resume turns.
	Provider string

	// Model is the Pi model string in "provider/id" form (from harnesses.pi.model).
	// Required on the initial turn. Ignored on resume turns.
	Model string

	// APIKeyEnv is the name of the env var the Pi child expects for the provider
	// API key (from harnesses.pi.api_key_env). REQUIRED — name only, no secret.
	// The VALUE comes from apiKeyFile (when set) or the operator env (PI-020).
	APIKeyEnv string

	// APIKeyFile is the OPTIONAL path (pre-expanded by ResolvePiConfig) to a file
	// holding the raw provider API key. When non-empty, resolvePiAPIKeyValue reads
	// the file in preference to the ambient env. The daemon ambient env MUST NOT
	// carry the secret (PI-050/hk-xmfoi). Absent → fall back to ambient env.
	APIKeyFile string

	// BaseURL is the OPTIONAL base URL for a locally-hosted OpenAI-compatible
	// endpoint (from harnesses.pi.base_url). When non-empty, BuildLaunchSpec
	// injects PI_CODING_AGENT_DIR into the child env on EVERY turn — that
	// directory holds both models.json and Pi's session store, so a resume turn
	// needs it to find the session it is told to resume (hk-6hfev). The
	// models.json under that dir is generated on the initial turn only. Absent =
	// today's cloud-provider behavior byte-for-byte unchanged. Bead: hk-z13jz.
	BaseURL string

	// API is the OPTIONAL Pi wire-format string written into the generated
	// models.json "api" field. When empty and baseURL is set, defaults to "openai"
	// at launch time. Bead: hk-z13jz.
	API string

	// PriorSessionID is non-nil for resume turns (iteration >= 2). It holds the
	// Pi session ID captured from the prior turn's first {"type":"session",...}
	// NDJSON line. Nil means this is the initial turn.
	PriorSessionID *string

	// IterationCount is the 1-based DOT iteration index. On a resume turn
	// (priorSessionID != nil) it selects the reviewer-feedback.iter-<N-1>.md the
	// resume seed prompt points at. Zero on the initial turn (single-mode or
	// iteration 1). See shared.ImplementerResumeSeedPrompt (internal/harness/shared/seedprompt.go).
	IterationCount int

	// BaseEnv is the base environment inherited from daemon Config.HandlerEnv.
	// buildPiEnv strips all credential keys (allowlist semantics, PI-021) and
	// injects only the selected provider's key.
	BaseEnv []string

	// PiHome is the Pi home directory used by the PI-042 billing guard check.
	// When empty, piDefaultHome() is used (production behaviour). Injectable
	// for tests to exercise the PI-042 deny path through BuildLaunchSpec
	// without touching the real ~/.pi. Bead: hk-6g5iu.
	PiHome string

	// BillingEmitter, when non-nil, receives pi_billing_guard events from the
	// fail-closed billing guard (PI-040/PI-042/PI-043, billingguard.go). Nil
	// disables event emission; the guard's enforcement (fail-closed assert) still
	// runs regardless.
	BillingEmitter handlercontract.EventEmitter

	// RunID correlates the pi_billing_guard events with a run. May be the zero
	// (uuid.Nil) RunID when the spec is built before a run_id is minted, in which
	// case the events are emitted run-unscoped.
	RunID core.RunID

	// SkipBillingGuard disables the pre-flight billing guard (PI-040,
	// billingguard.go). Exists SOLELY so unit tests that exercise argv/env
	// shape do not require a real api key in the environment. Production callers
	// MUST leave it false so the fail-closed guard runs.
	SkipBillingGuard bool
}

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

// BuildLaunchSpec constructs a handler.LaunchSpec for launching a Pi
// subprocess for one turn (initial or resume).
//
// The returned spec is suitable for passing directly to handler.Launch. The
// caller is responsible for writing agent-task.md into the worktree before
// calling this function (the spec does not write it).
//
// Spec: PI-015, PI-020, PI-021.
func BuildLaunchSpec(rc RunCtx) (handler.LaunchSpec, error) {
	if rc.WorkspacePath == "" {
		return handler.LaunchSpec{}, fmt.Errorf(
			"BuildLaunchSpec: workspacePath must be non-empty")
	}
	if rc.BeadID == "" {
		return handler.LaunchSpec{}, fmt.Errorf(
			"BuildLaunchSpec: beadID must be non-empty")
	}
	if rc.APIKeyEnv == "" {
		return handler.LaunchSpec{}, fmt.Errorf("%s", piConfigMissingAPIKeyMessage)
	}
	if rc.PriorSessionID != nil && *rc.PriorSessionID == "" {
		return handler.LaunchSpec{}, fmt.Errorf(
			"BuildLaunchSpec: priorSessionID must not be an empty string (pass nil for initial turn)")
	}
	if rc.PriorSessionID == nil {
		if rc.Provider == "" {
			return handler.LaunchSpec{}, fmt.Errorf("%s", piConfigMissingProviderMessage)
		}
		if rc.Model == "" {
			return handler.LaunchSpec{}, fmt.Errorf("%s", piConfigMissingModelMessage)
		}
	}

	binary := rc.PiBinary
	if binary == "" {
		binary = "pi"
	}

	seedPrompt := fmt.Sprintf(piSeedPromptTemplate, rc.BeadID)
	var args []string
	if rc.PriorSessionID != nil {
		seedPrompt = shared.ImplementerResumeSeedPrompt(rc.BeadID, rc.IterationCount-1)
		args = []string{
			"--mode", "json",
			"--no-extensions",
			"--session", *rc.PriorSessionID,
			seedPrompt,
		}
	} else {
		args = []string{
			"--mode", "json",
			"--no-extensions",
			"--provider", rc.Provider,
			"--model", rc.Model,
			seedPrompt,
		}
	}

	env := buildPiEnv(rc.BaseEnv, rc.APIKeyFile, rc.APIKeyEnv)

	if !rc.SkipBillingGuard {
		piHome := rc.PiHome
		if piHome == "" {
			piHome = piDefaultHome()
		}
		if err := runPiBillingGuard(context.Background(), rc.BillingEmitter, rc.RunID, rc.BeadID, rc.APIKeyFile, rc.APIKeyEnv, piHome); err != nil {
			return handler.LaunchSpec{}, err
		}
	}

	if rc.BaseURL != "" {
		piAgentDir := filepath.Join(rc.WorkspacePath, ".harmonik", "pi-agent")
		if mkdirErr := os.MkdirAll(piAgentDir, 0o700); mkdirErr != nil {
			return handler.LaunchSpec{}, fmt.Errorf(
				"BuildLaunchSpec: create pi-agent dir %q: %w", piAgentDir, mkdirErr)
		}
		if rc.PriorSessionID == nil {
			modelsJSON, buildErr := buildPiModelsJSON(rc.Provider, rc.BaseURL, rc.API, rc.APIKeyFile, rc.APIKeyEnv, rc.Model)
			if buildErr != nil {
				return handler.LaunchSpec{}, fmt.Errorf(
					"BuildLaunchSpec: build models.json: %w", buildErr)
			}
			modelsPath := filepath.Join(piAgentDir, "models.json")
			if writeErr := os.WriteFile(modelsPath, modelsJSON, 0o600); writeErr != nil {
				return handler.LaunchSpec{}, fmt.Errorf(
					"BuildLaunchSpec: write models.json to %q: %w", modelsPath, writeErr)
			}
		}
		env = append(env, "PI_CODING_AGENT_DIR="+piAgentDir)
	}

	return handler.LaunchSpec{
		Binary:       binary,
		Args:         args,
		Env:          env,
		WorkDir:      rc.WorkspacePath,
		Role:         "implementer",
		StdinDevNull: true, // PI-020 / #4303: Pi (ProcessExit) may hang on pane PTY stdin with /dev/null
	}, nil
}

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

func buildPiEnv(baseEnv []string, apiKeyFile, apiKeyEnv string) []string {
	knownCredSet := make(map[string]bool, len(piProviderCredentialKeys))
	for _, k := range piProviderCredentialKeys {
		knownCredSet[k] = true
	}

	strippedSet := make(map[string]bool, len(piProviderCredentialKeys))
	for k := range knownCredSet {
		if k != apiKeyEnv {
			strippedSet[k] = true
		}
	}
	for _, kv := range baseEnv {
		key := shared.EnvKey(kv)
		if key == apiKeyEnv {
			continue
		}
		if !knownCredSet[key] && isPiAPIKeyPattern(key) {
			strippedSet[key] = true
		}
	}

	env := make([]string, 0, len(baseEnv)+len(strippedSet)+4)

	hasPath := false
	for _, kv := range baseEnv {
		key := shared.EnvKey(kv)
		if key == apiKeyEnv || strippedSet[key] {
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

	for k := range strippedSet {
		env = append(env, k+"=")
	}

	apiKeyValue := resolvePiAPIKeyValue(apiKeyFile, apiKeyEnv)

	env = append(env,
		apiKeyEnv+"="+apiKeyValue,
		"DISABLE_AUTO_UPDATE=true",
		"DISABLE_UPDATE_PROMPT=true",
	)

	return env
}

func isPiAPIKeyPattern(key string) bool {
	return strings.HasSuffix(key, "_API_KEY")
}
