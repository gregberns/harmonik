package handlercontract_test

// redactionregistry_test.go — sensors for RedactionRegistry + RedactionMiddleware
// (HC-030, HC-031, HC-032).
//
// Spec refs: specs/handler-contract.md §4.7.HC-030, §4.7.HC-031, §4.7.HC-032.
// Bead ref: hk-8i31.83.
//
// Helper prefix: registryFixture (per implementer-protocol.md §Helper-prefix
// discipline; distinct from redactionFixture used by hk-8i31.81).
//
// What this file provides:
//
//  1. TestRegistryFixture_MiddlewareAppliesHC031FieldNameRedaction —
//     RedactionMiddleware redacts secret-named fields (HC-031) even when no
//     per-handler patterns are registered.
//
//  2. TestRegistryFixture_MiddlewareAppliesHC032ValuePatternRedaction —
//     RedactionMiddleware redacts values matching a registered per-handler
//     pattern (HC-032).
//
//  3. TestRegistryFixture_HC031AndHC032Compose — a payload with both a
//     secret-named field AND a secret-valued field are both redacted when a
//     registry with HC-032 patterns is used.
//
//  4. TestRegistryFixture_SafeFieldsPassThrough — neither HC-031 nor HC-032
//     redacts fields whose names and values are safe (no over-redaction).
//
//  5. TestRegistryFixture_NilPayloadReturnsNil — RedactionMiddleware returns
//     nil for a nil input.
//
//  6. TestRegistryFixture_MultiSubsystemPatternsCompose — patterns from two
//     different subsystems are both applied to the same payload.

import (
	"regexp"
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// registryFixtureAnthropicPattern is a compiled version of the canonical
// Anthropic API key shape from HC-032. Mirrors the pattern declared in
// redactionFixturePerHandlerPatterns (redaction_hc028_test.go) without
// importing from that file (test helpers are not importable cross-file).
var registryFixtureAnthropicPattern = regexp.MustCompile(`^sk-ant-[A-Za-z0-9_\-]{10,}$`)

// registryFixtureGenericSKPattern is the generic sk- prefix pattern.
var registryFixtureGenericSKPattern = regexp.MustCompile(`^sk-[A-Za-z0-9_\-]{20,}$`)

// registryFixtureAnthropicKeyStub is a structural key stub matching the
// Anthropic pattern shape. Uses 'x' padding only (HC-034 compliance).
const registryFixtureAnthropicKeyStub = "sk-ant-" +
	"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

// ─────────────────────────────────────────────────────────────────────────────
// HC-031 via middleware: field-name redaction applies even with zero patterns
// ─────────────────────────────────────────────────────────────────────────────

// TestRegistryFixture_MiddlewareAppliesHC031FieldNameRedaction asserts that
// RedactionMiddleware applies HC-031 field-name redaction even when the
// registry has zero registered per-handler patterns.
//
// This ensures HC-031 and HC-032 are independent: an empty registry still
// provides field-name defence-in-depth.
//
// Spec ref: specs/handler-contract.md §4.7.HC-031.
func TestRegistryFixture_MiddlewareAppliesHC031FieldNameRedaction(t *testing.T) {
	t.Parallel()

	reg := handlercontract.NewRedactionRegistry()
	// No patterns registered — empty registry.

	payload := map[string]any{
		"token":   "tok-super-secret",
		"node_id": "node-abc-123",
	}

	got := reg.RedactionMiddleware(payload)

	if got == nil {
		t.Fatal("RedactionMiddleware returned nil for non-nil input")
	}

	if got["token"] != handlercontract.RedactedSentinel {
		t.Errorf(`RedactionMiddleware["token"] = %v, want %q (HC-031)`,
			got["token"], handlercontract.RedactedSentinel)
	}
	if got["node_id"] != "node-abc-123" {
		t.Errorf(`RedactionMiddleware["node_id"] = %v, want "node-abc-123"`, got["node_id"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-032: per-handler value-pattern redaction
// ─────────────────────────────────────────────────────────────────────────────

// TestRegistryFixture_MiddlewareAppliesHC032ValuePatternRedaction asserts that
// RedactionMiddleware redacts a payload value that matches a registered
// per-handler pattern (HC-032), even when the field name is benign.
//
// Spec ref: specs/handler-contract.md §4.7.HC-032.
func TestRegistryFixture_MiddlewareAppliesHC032ValuePatternRedaction(t *testing.T) {
	t.Parallel()

	reg := handlercontract.NewRedactionRegistry()
	reg.RegisterPattern("claude_handler", []*regexp.Regexp{registryFixtureAnthropicPattern})

	payload := map[string]any{
		// Field name is benign — HC-031 must NOT redact it.
		// Value matches the Anthropic key shape — HC-032 MUST redact it.
		"provider_key": registryFixtureAnthropicKeyStub,
		"node_id":      "node-abc-123",
	}

	got := reg.RedactionMiddleware(payload)

	if got == nil {
		t.Fatal("RedactionMiddleware returned nil for non-nil input")
	}

	if got["provider_key"] != handlercontract.RedactedSentinel {
		t.Errorf(`RedactionMiddleware["provider_key"] = %v, want %q (HC-032)`,
			got["provider_key"], handlercontract.RedactedSentinel)
	}
	if got["node_id"] != "node-abc-123" {
		t.Errorf(`RedactionMiddleware["node_id"] = %v, want "node-abc-123"`, got["node_id"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-031 + HC-032 compose
// ─────────────────────────────────────────────────────────────────────────────

// TestRegistryFixture_HC031AndHC032Compose asserts that both HC-031 and HC-032
// are applied in the same middleware call.
//
// Payload has:
//   - A secret-named field (matches HC-031, value irrelevant).
//   - A benign-named field with an Anthropic key stub value (matches HC-032).
//   - A safe field (matches neither rule).
//
// Spec refs: specs/handler-contract.md §4.7.HC-031, §4.7.HC-032.
func TestRegistryFixture_HC031AndHC032Compose(t *testing.T) {
	t.Parallel()

	reg := handlercontract.NewRedactionRegistry()
	reg.RegisterPattern("claude_handler", []*regexp.Regexp{registryFixtureAnthropicPattern})

	payload := map[string]any{
		"token":        "tok-super-secret",              // HC-031 match
		"provider_key": registryFixtureAnthropicKeyStub, // HC-032 match
		"node_id":      "node-abc-123",                  // safe
	}

	got := reg.RedactionMiddleware(payload)

	if got == nil {
		t.Fatal("RedactionMiddleware returned nil for non-nil input")
	}

	if got["token"] != handlercontract.RedactedSentinel {
		t.Errorf(`RedactionMiddleware["token"] = %v, want %q (HC-031 compose)`,
			got["token"], handlercontract.RedactedSentinel)
	}
	if got["provider_key"] != handlercontract.RedactedSentinel {
		t.Errorf(`RedactionMiddleware["provider_key"] = %v, want %q (HC-032 compose)`,
			got["provider_key"], handlercontract.RedactedSentinel)
	}
	if got["node_id"] != "node-abc-123" {
		t.Errorf(`RedactionMiddleware["node_id"] = %v, want "node-abc-123" (no over-redaction)`,
			got["node_id"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// No over-redaction
// ─────────────────────────────────────────────────────────────────────────────

// TestRegistryFixture_SafeFieldsPassThrough asserts that RedactionMiddleware
// does not redact fields whose names and values are safe (no false positives).
//
// Spec refs: specs/handler-contract.md §4.7.HC-031, §4.7.HC-032.
func TestRegistryFixture_SafeFieldsPassThrough(t *testing.T) {
	t.Parallel()

	reg := handlercontract.NewRedactionRegistry()
	reg.RegisterPattern("claude_handler", []*regexp.Regexp{registryFixtureAnthropicPattern})

	safePayload := map[string]any{
		"node_id":    "node-abc-123",
		"run_id":     "run-xyz-456",
		"status":     "SUCCESS",
		"exit_code":  0,
		"agent_type": "claude",
	}

	got := reg.RedactionMiddleware(safePayload)

	if got == nil {
		t.Fatal("RedactionMiddleware returned nil for non-nil input")
	}

	for k, want := range safePayload {
		k, want := k, want
		t.Run(k, func(t *testing.T) {
			t.Parallel()
			v, ok := got[k]
			if !ok {
				t.Fatalf("RedactionMiddleware: key %q missing from output", k)
			}
			if v != want {
				t.Errorf("RedactionMiddleware[%q] = %v, want %v; safe field MUST NOT be redacted", k, v, want)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Nil input
// ─────────────────────────────────────────────────────────────────────────────

// TestRegistryFixture_NilPayloadReturnsNil asserts that RedactionMiddleware
// returns nil for a nil input map (consistent with RedactByFieldName contract).
//
// Spec ref: specs/handler-contract.md §4.7.HC-031.
func TestRegistryFixture_NilPayloadReturnsNil(t *testing.T) {
	t.Parallel()

	reg := handlercontract.NewRedactionRegistry()
	got := reg.RedactionMiddleware(nil)
	if got != nil {
		t.Errorf("RedactionMiddleware(nil) = %v, want nil", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Multi-subsystem composition
// ─────────────────────────────────────────────────────────────────────────────

// TestRegistryFixture_MultiSubsystemPatternsCompose asserts that patterns
// contributed by two different subsystems are both applied to the same
// payload.
//
// Spec ref: specs/handler-contract.md §4.7.HC-032.
func TestRegistryFixture_MultiSubsystemPatternsCompose(t *testing.T) {
	t.Parallel()

	reg := handlercontract.NewRedactionRegistry()
	reg.RegisterPattern("subsystem_a", []*regexp.Regexp{registryFixtureAnthropicPattern})
	reg.RegisterPattern("subsystem_b", []*regexp.Regexp{registryFixtureGenericSKPattern})

	// generic_sk_payload: value matches the generic sk- pattern (subsystem_b).
	// Note: the Anthropic pattern is more specific; use a value that matches
	// generic sk- but NOT the Anthropic shape (no "ant" infix).
	const genericSKStub = "sk-" + "xxxxxxxxxxxxxxxxxxxx" // 20 chars after sk-

	payload := map[string]any{
		"provider_key": registryFixtureAnthropicKeyStub, // matches subsystem_a
		"alt_key":      genericSKStub,                   // matches subsystem_b
		"node_id":      "node-safe",                     // safe
	}

	got := reg.RedactionMiddleware(payload)

	if got == nil {
		t.Fatal("RedactionMiddleware returned nil for non-nil input")
	}

	if got["provider_key"] != handlercontract.RedactedSentinel {
		t.Errorf(`RedactionMiddleware["provider_key"] = %v, want %q (subsystem_a pattern)`,
			got["provider_key"], handlercontract.RedactedSentinel)
	}
	if got["alt_key"] != handlercontract.RedactedSentinel {
		t.Errorf(`RedactionMiddleware["alt_key"] = %v, want %q (subsystem_b pattern)`,
			got["alt_key"], handlercontract.RedactedSentinel)
	}
	if got["node_id"] != "node-safe" {
		t.Errorf(`RedactionMiddleware["node_id"] = %v, want "node-safe"`, got["node_id"])
	}
}
