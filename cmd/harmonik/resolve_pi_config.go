package main

// resolve_pi_config.go — the Pi harness config resolver (hk-v7q5u).
//
// # OPERATOR-FACING CHOKEPOINT — imposes NO built-in defaults.
//
// ResolvePiConfig is the validation gate for the harnesses.pi block in
// .harmonik/config.yaml. Per the R1 de-hardcode mandate and PI-051, the product
// imposes ZERO baked Pi defaults (provider, model, or key): EVERY required value
// must be set by the operator. When a required value is unset the resolver
// AGGREGATES all the missing keys and returns a single *PiConfigMissingError so
// the Pi harness REFUSES TO START — it never silently defaults.
//
// Why it lives in cmd/harmonik (NOT internal/daemon): the resolver needs
// daemon.PiHarnessConfig (the parsed .harmonik/config.yaml harnesses.pi block),
// and the depguard bans internal packages from importing internal/daemon
// (.golangci.yml). This mirrors the keeper resolver pattern (resolve_keeper_config.go).
//
// # Precedence
//
// For Pi, all config is config-only (no CLI flags for provider/model/api_key_env):
//
//	CONFIG (required) — missing → refuse to start
//
// # Spec refs
//
// specs/pi-harness.md §5 (PI-050, PI-051, PI-052).
// Bead ref: hk-v7q5u.

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gregberns/harmonik/internal/daemon"
)

// piModelShapeRe is the HC-055a shape validation regex for Pi model fields.
// Allows any provider/model string matching ^[A-Za-z0-9._:/-]+$, ≤128 chars.
// Value-validation (checking the model is actually supported by the provider) is
// intentionally NOT done here — the authoritative check is handler-side launch
// failure (PI-052).
var piModelShapeRe = regexp.MustCompile(`^[A-Za-z0-9._:/-]+$`)

// PiConfigMissingError is returned by ResolvePiConfig when one or more REQUIRED
// Pi config values are unset (no config value). It aggregates EVERY missing key
// (not just the first) so the operator can fix them all in one pass, and its
// message names the real dotted yaml key paths and points at
// 'harmonik pi config --example'.
//
// Spec ref: PI-051. Bead ref: hk-v7q5u.
type PiConfigMissingError struct {
	// ProjectDir is the project root whose .harmonik/config.yaml needs the keys.
	ProjectDir string
	// Missing is the dotted yaml key paths the operator must set, e.g.
	// "harnesses.pi.provider". Aggregated, never first-only.
	Missing []string
}

func (e *PiConfigMissingError) Error() string {
	dir := e.ProjectDir
	if dir == "" {
		dir = "<project>"
	}
	return fmt.Sprintf(
		"refusing to start Pi harness — harmonik imposes NO built-in Pi defaults; "+
			"every required value must be set by the operator. Missing %d value(s): %s. "+
			"Fix: run 'harmonik pi config --example' to print a complete starting harnesses.pi: block, "+
			"add it to %s/.harmonik/config.yaml. "+
			"(R1 de-hardcode mandate: the product imposes ZERO baked Pi defaults — provider, model, or key.)",
		len(e.Missing), strings.Join(e.Missing, ", "), dir)
}

// PiConfigError is returned by ResolvePiConfig when a PRESENT config value fails
// shape validation (e.g. HC-055a model regex). It is a sibling of
// *PiConfigMissingError (which is for unset required values); the Pi start path
// surfaces both via the same refuse-to-start path.
//
// Spec ref: PI-052. Bead ref: hk-v7q5u.
type PiConfigError struct {
	// Field names the offending config key (dotted yaml path).
	Field string
	// Reason is the human-readable explanation.
	Reason string
}

func (e *PiConfigError) Error() string {
	return fmt.Sprintf("Pi harness config: %s: %s", e.Field, e.Reason)
}

// ResolvePiConfig validates the harnesses.pi block parsed from .harmonik/config.yaml.
//
// It aggregates ALL missing required keys into one *PiConfigMissingError (never
// first-only — PI-051), validates the model and fallback.model fields by shape
// only (HC-055a regex ^[A-Za-z0-9._:/-]+$, ≤128 chars — PI-052), and refuses to
// start on any error. projectDir names the file to fix in the missing-value message.
//
// Required fields: provider, model, api_key_env.
// Optional: fallback{provider, model, api_key_env}. When HasFallback is true,
// all three fallback fields are required (partial fallback blocks are rejected).
//
// The product imposes ZERO baked Pi defaults. Missing required field → refuse to
// start. Model shape is validated; model VALUE is never validated — Pi's full
// provider/model range is selectable (PI-052 / HC-055a value-opacity invariant).
func ResolvePiConfig(cfg daemon.PiHarnessConfig, projectDir string) (daemon.PiHarnessConfig, error) {
	// ── Missing-value gate (checked first, aggregates ALL missing keys). ──
	// Required: provider, model, api_key_env. No defaults — R1 mandate.
	var missing []string
	if cfg.Provider == "" {
		missing = append(missing, "harnesses.pi.provider")
	}
	if cfg.Model == "" {
		missing = append(missing, "harnesses.pi.model")
	}
	if cfg.APIKeyEnv == "" {
		missing = append(missing, "harnesses.pi.api_key_env")
	}
	// When the fallback block is present, all three fallback fields are required.
	if cfg.HasFallback {
		if cfg.Fallback.Provider == "" {
			missing = append(missing, "harnesses.pi.fallback.provider")
		}
		if cfg.Fallback.Model == "" {
			missing = append(missing, "harnesses.pi.fallback.model")
		}
		if cfg.Fallback.APIKeyEnv == "" {
			missing = append(missing, "harnesses.pi.fallback.api_key_env")
		}
	}
	if len(missing) > 0 {
		return daemon.PiHarnessConfig{}, &PiConfigMissingError{
			ProjectDir: projectDir,
			Missing:    missing,
		}
	}

	// ── Shape validation (HC-055a, PI-052). ──
	// Model is shape-validated only — never against a curated value enum. The
	// authoritative compatibility check is handler-side launch failure.
	if err := validatePiModelShape("harnesses.pi.model", cfg.Model); err != nil {
		return daemon.PiHarnessConfig{}, err
	}
	if cfg.HasFallback && cfg.Fallback.Model != "" {
		if err := validatePiModelShape("harnesses.pi.fallback.model", cfg.Fallback.Model); err != nil {
			return daemon.PiHarnessConfig{}, err
		}
	}

	return cfg, nil
}

// validatePiModelShape enforces the HC-055a shape invariant:
// ^[A-Za-z0-9._:/-]+$, ≤128 chars. Returns *PiConfigError on violation.
// Value-validation is intentionally absent — the full Pi provider/model range
// MUST be selectable (PI-052).
func validatePiModelShape(field, model string) error {
	if len(model) > 128 {
		return &PiConfigError{
			Field:  field,
			Reason: fmt.Sprintf("model string is %d chars; HC-055a requires ≤128 chars", len(model)),
		}
	}
	if !piModelShapeRe.MatchString(model) {
		return &PiConfigError{
			Field:  field,
			Reason: fmt.Sprintf("model %q contains invalid characters; HC-055a allows only ^[A-Za-z0-9._:/-]+$", model),
		}
	}
	return nil
}

// piConfigExampleYAML returns the complete harnesses.pi: block template for
// 'harmonik pi config --example'. The comment text serves as operator documentation;
// no value here is a baked runtime default.
func piConfigExampleYAML() string {
	return `harnesses:
  pi:
    # Pi implementer harness config. All three fields are REQUIRED — no defaults.
    # The product imposes ZERO baked Pi defaults (R1 de-hardcode mandate, PI-050).
    # harnesses.pi.provider: Pi provider string (e.g. openrouter, anthropic, openai)
    provider: openrouter
    # harnesses.pi.model: Pi model string — shape-validated only (HC-055a: ^[A-Za-z0-9._:/-]+$, ≤128 chars)
    # Use a PAID model for unattended fleet work; ':free' models are hand-attended experiments only (PI-069).
    model: openrouter/qwen/qwen3-coder
    # harnesses.pi.api_key_env: name of the env var carrying the provider API key.
    # The key VALUE is never stored in config — it lives in the operator environment (PI-020/PI-040).
    api_key_env: OPENROUTER_API_KEY
    # fallback: optional paid-fallback target. V1 has NO automatic fallback (PI-072) —
    # this block exists for operator convenience (manual lane flip on cap exhaustion).
    # fallback:
    #   provider: anthropic
    #   model: anthropic/claude-haiku-4-5-20251001
    #   api_key_env: ANTHROPIC_API_KEY
`
}
