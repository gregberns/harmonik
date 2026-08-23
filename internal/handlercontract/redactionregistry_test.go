package handlercontract_test

import (
	"regexp"
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

var registryFixtureAnthropicPattern = regexp.MustCompile(`^sk-ant-[A-Za-z0-9_\-]{10,}$`)

var registryFixtureGenericSKPattern = regexp.MustCompile(`^sk-[A-Za-z0-9_\-]{20,}$`)

const registryFixtureAnthropicKeyStub = "sk-ant-" +
	"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

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
