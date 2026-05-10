package handlercontract_test

// redaction_hc028_test.go — fixture and sensors for HC-028..HC-034 (secrets
// pipeline) and HC-INV-003 (no secret value crosses the event-bus boundary).
//
// Spec refs: specs/handler-contract.md §4.7.HC-028 through §4.7.HC-034,
// §10.2.HC-028..HC-034; bead hk-8i31.81.
//
// Helper prefix: redactionFixture (per implementer-protocol.md
// §Helper-prefix discipline).
//
// What this file provides:
//
//   1. redactionFixtureSecretNamedPayload — a struct whose field names match
//      the HC-031 common-prefix regex. Used by middleware and startup-check
//      tests when those implementations land (hk-8i31.37, hk-8i31.38,
//      hk-8i31.40).
//
//   2. redactionFixtureSecretValuePayload — a struct with benign field names
//      carrying values that match the HC-032 per-handler value patterns (e.g.,
//      sk-ant-api03-... Anthropic key shape). Used by per-handler-pattern tests
//      (hk-8i31.39).
//
//   3. redactionFixtureSafePayload — a struct with no secret-named fields and
//      no secret-shaped values. Used as a negative / control case in both
//      middleware and startup-check tests.
//
//   4. redactionFixtureSchemaViolation — a struct whose field name matches
//      HC-031's regex, representing the negative-case input to HC-033 (compile-
//      time schema check must reject a registered event type whose payload
//      schema declares such a field).
//
//   5. redactionFixturePerHandlerPatterns — the set of value-regex patterns a
//      handler subsystem declares in its envelope per HC-032. Used by
//      per-handler-pattern registration tests (hk-8i31.39).
//
//   6. Static sensors asserting HC-031's regex coverage and HC-034's "no
//      literal secret value in test payloads" policy.
//
// None of these tests wire up the actual middleware or compile-time checker;
// the fixture types and sensor assertions are load-bearing for the downstream
// beads that implement those mechanisms.

import (
	"regexp"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// HC-031 regex constant
// ─────────────────────────────────────────────────────────────────────────────

// redactionFixtureCommonPrefixRegex is the normative case-insensitive regex
// from §4.7.HC-031. Field names matching this regex MUST be replaced with
// "<redacted>" before emission.
//
// Spec: `(secret|token|password|api[_-]?key|auth)`
const redactionFixtureCommonPrefixRegex = `(?i)(secret|token|password|api[_-]?key|auth)`

// redactionFixtureRedactedSentinel is the literal replacement value required
// by HC-031 and HC-032. Field values that match a redaction rule MUST be
// replaced with exactly this string before emission.
const redactionFixtureRedactedSentinel = "<redacted>"

// ─────────────────────────────────────────────────────────────────────────────
// Fixture: secret-named payload (HC-031 positive cases)
// ─────────────────────────────────────────────────────────────────────────────

// redactionFixtureSecretNamedPayload carries fields whose NAMES match the
// HC-031 common-prefix regex. The middleware (hk-8i31.38) MUST replace every
// non-empty string value in these fields with "<redacted>" before the event
// reaches any consumer or is written to disk.
//
// Field names are chosen to cover all five branches of the HC-031 regex:
//   - "secret"   (direct match)
//   - "token"    (direct match)
//   - "password" (direct match)
//   - "api_key"  (api + underscore + key)
//   - "auth"     (direct match)
//
// An additional "api-key" variant (hyphen form) covers the `api[_-]?key`
// alternation branch. "apikey" (no separator) covers the `api[_-]?key`
// optional-separator branch.
//
// HC-031 note: the match is on FIELD NAME, not value. A payload struct that
// names a field "secret" is a producer-side bug; the middleware provides
// defence-in-depth, not a primary control.
type redactionFixtureSecretNamedPayload struct {
	Secret   string `json:"secret"`
	Token    string `json:"token"`
	Password string `json:"password"`
	APIKey   string `json:"api_key"`
	APIKeyH  string `json:"api-key"`
	APIKeyN  string `json:"apikey"`
	Auth     string `json:"auth"`
	// Safe control field — MUST NOT be redacted by the common-prefix rule.
	WorkerID string `json:"worker_id"`
}

// redactionFixtureSecretNamedFieldNames is the canonical list of field NAMES
// from redactionFixtureSecretNamedPayload that must match HC-031's regex.
// Used by TestRedaction_HC031_CommonPrefixRegexCoversFixtureFields.
var redactionFixtureSecretNamedFieldNames = []string{
	"secret",
	"token",
	"password",
	"api_key",
	"api-key",
	"apikey",
	"auth",
}

// ─────────────────────────────────────────────────────────────────────────────
// Fixture: secret-valued payload (HC-032 positive cases)
// ─────────────────────────────────────────────────────────────────────────────

// redactionFixtureSecretValuePayload carries fields with BENIGN names but
// values that match per-handler value-shaped patterns declared by HC-032.
//
// The concrete example in §4.7.HC-032 is Anthropic API keys matching
// "sk-ant-*". The fixture uses representative values drawn from known provider
// key shapes. Values MUST be syntactically valid prefixes for the patterns but
// MUST NOT be real secrets (see HC-034 note below).
//
// HC-034 compliance: the values here are structural stubs (prefix + repeated
// 'x') that match the pattern shape but are not extractable credentials.
type redactionFixtureSecretValuePayload struct {
	// ProviderKey carries an Anthropic-shaped API key stub (sk-ant-api03-...).
	// The per-handler pattern for the Claude handler MUST match this value.
	ProviderKey string `json:"provider_key"`

	// AltProviderKey carries a generic sk- prefixed key stub used by some
	// providers as a secondary shape example.
	AltProviderKey string `json:"alt_provider_key"`

	// SafeValue carries a non-secret value; the per-handler patterns MUST NOT
	// match it.
	SafeValue string `json:"safe_value"`
}

// redactionFixtureAnthropicKeyStub is a structural key stub whose value matches
// the Anthropic API key shape (sk-ant-api03- prefix) declared in HC-032.
// It is NOT a real key; the body is 93 'x' characters to match the regex shape
// without being an extractable credential.
//
// HC-034: body MUST contain only 'x' padding — verified by
// TestRedaction_HC034_FixtureStubsContainNoRealSecrets.
const redactionFixtureAnthropicKeyStub = "sk-ant-api03-" +
	"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

// ─────────────────────────────────────────────────────────────────────────────
// Fixture: safe payload (negative / control case)
// ─────────────────────────────────────────────────────────────────────────────

// redactionFixtureSafePayload carries fields with names and values that MUST
// NOT match any redaction rule. Used as a control in middleware tests to verify
// the middleware does not over-redact legitimate payloads.
type redactionFixtureSafePayload struct {
	NodeID    string `json:"node_id"`
	RunID     string `json:"run_id"`
	Status    string `json:"status"`
	ExitCode  int    `json:"exit_code"`
	AgentType string `json:"agent_type"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Fixture: HC-033 schema-violation type (negative case for startup check)
// ─────────────────────────────────────────────────────────────────────────────

// redactionFixtureSchemaViolation is a payload type whose field name matches
// the HC-031 regex. Per HC-033, registering an event type whose payload schema
// contains such a field MUST be a startup-time error.
//
// This struct is the negative-case input to
// TestRedaction_HC033_StartupCheckRejectsSecretFieldInSchema (below). It is
// deliberately NOT registered via core.RegisterEventType; attempting to register
// it is the action under test.
type redactionFixtureSchemaViolation struct {
	// "password" matches HC-031; any registered event type with this field MUST
	// be rejected at startup by the schema checker (hk-8i31.40).
	Password string `json:"password"`

	// Safe field — present to confirm the check triggers on the bad field, not
	// on the entire struct.
	NodeID string `json:"node_id"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Fixture: per-handler redaction patterns (HC-032)
// ─────────────────────────────────────────────────────────────────────────────

// redactionFixturePerHandlerPatterns is the slice of value-regex patterns a
// handler subsystem contributes to the redaction registry at daemon init per
// HC-032. Each entry is a compiled regex matching the handler's provider-secret
// value shape.
//
// These patterns are consumed by per-handler-pattern registration tests
// (hk-8i31.39). The regex strings are normative; the compiled forms are
// compiled once at test init.
var redactionFixturePerHandlerPatterns = []struct {
	// Name is a human-readable label for the pattern (used in test sub-test names).
	Name string
	// Pattern is the Go regex string the handler declares in its subsystem envelope.
	Pattern string
}{
	{
		Name:    "anthropic_api_key",
		Pattern: `^sk-ant-[A-Za-z0-9_\-]{10,}$`,
	},
	{
		Name:    "generic_sk_prefix",
		Pattern: `^sk-[A-Za-z0-9_\-]{20,}$`,
	},
}

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: HC-031 regex covers all fixture field names
// ─────────────────────────────────────────────────────────────────────────────

// TestRedaction_HC031_CommonPrefixRegexCoversFixtureFields asserts that every
// field name in redactionFixtureSecretNamedFieldNames is matched by the HC-031
// common-prefix regex.
//
// This is a schema sensor: if HC-031's regex or the fixture field list drifts,
// this test catches it before middleware implementation starts.
//
// Spec ref: specs/handler-contract.md §4.7.HC-031.
func TestRedaction_HC031_CommonPrefixRegexCoversFixtureFields(t *testing.T) {
	t.Parallel()

	re := regexp.MustCompile(redactionFixtureCommonPrefixRegex)

	for _, fieldName := range redactionFixtureSecretNamedFieldNames {
		fieldName := fieldName
		t.Run(fieldName, func(t *testing.T) {
			t.Parallel()
			if !re.MatchString(fieldName) {
				t.Errorf(
					"HC-031 regex %q does not match fixture field name %q; "+
						"either the regex or the fixture field list is out of sync with §4.7.HC-031",
					redactionFixtureCommonPrefixRegex, fieldName,
				)
			}
		})
	}
}

// TestRedaction_HC031_CommonPrefixRegexDoesNotMatchSafeFields asserts that
// the safe control fields in redactionFixtureSafePayload do NOT match HC-031's
// regex. A false positive here would indicate the middleware over-redacts
// legitimate payloads.
//
// Spec ref: specs/handler-contract.md §4.7.HC-031.
func TestRedaction_HC031_CommonPrefixRegexDoesNotMatchSafeFields(t *testing.T) {
	t.Parallel()

	safeFields := []string{
		"node_id",
		"run_id",
		"status",
		"exit_code",
		"agent_type",
		"worker_id",
	}

	re := regexp.MustCompile(redactionFixtureCommonPrefixRegex)

	for _, fieldName := range safeFields {
		fieldName := fieldName
		t.Run(fieldName, func(t *testing.T) {
			t.Parallel()
			if re.MatchString(fieldName) {
				t.Errorf(
					"HC-031 regex %q unexpectedly matches safe field name %q; "+
						"middleware would over-redact legitimate payloads",
					redactionFixtureCommonPrefixRegex, fieldName,
				)
			}
		})
	}
}

// TestRedaction_HC031_RedactedSentinelShape verifies that the redacted sentinel
// string is exactly the literal `"<redacted>"` required by §4.7.HC-031.
//
// Spec ref: specs/handler-contract.md §4.7.HC-031.
func TestRedaction_HC031_RedactedSentinelShape(t *testing.T) {
	t.Parallel()
	const want = "<redacted>"
	if redactionFixtureRedactedSentinel != want {
		t.Errorf("redactionFixtureRedactedSentinel = %q, want %q (§4.7.HC-031)", redactionFixtureRedactedSentinel, want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: HC-032 per-handler patterns compile and match stubs
// ─────────────────────────────────────────────────────────────────────────────

// TestRedaction_HC032_PerHandlerPatternsCompile verifies that every pattern in
// redactionFixturePerHandlerPatterns is a valid Go regex. A syntax error here
// means the handler spec declares an uncompilable pattern, which would prevent
// daemon init per HC-032.
//
// Spec ref: specs/handler-contract.md §4.7.HC-032.
func TestRedaction_HC032_PerHandlerPatternsCompile(t *testing.T) {
	t.Parallel()

	for _, entry := range redactionFixturePerHandlerPatterns {
		entry := entry
		t.Run(entry.Name, func(t *testing.T) {
			t.Parallel()
			_, err := regexp.Compile(entry.Pattern)
			if err != nil {
				t.Errorf(
					"per-handler redaction pattern %q (name=%q) does not compile: %v; "+
						"HC-032 requires patterns to be registered at daemon init",
					entry.Pattern, entry.Name, err,
				)
			}
		})
	}
}

// TestRedaction_HC032_AnthropicPatternMatchesStub asserts that the
// "anthropic_api_key" pattern matches redactionFixtureAnthropicKeyStub (the
// canonical Anthropic-shaped key stub).
//
// This confirms the fixture and the pattern agree on the key shape before
// per-handler-pattern registration (hk-8i31.39) is implemented.
//
// Spec ref: specs/handler-contract.md §4.7.HC-032.
func TestRedaction_HC032_AnthropicPatternMatchesStub(t *testing.T) {
	t.Parallel()

	var anthropicPattern string
	for _, entry := range redactionFixturePerHandlerPatterns {
		if entry.Name == "anthropic_api_key" {
			anthropicPattern = entry.Pattern
			break
		}
	}
	if anthropicPattern == "" {
		t.Fatal("anthropic_api_key pattern not found in redactionFixturePerHandlerPatterns")
	}

	re := regexp.MustCompile(anthropicPattern)
	if !re.MatchString(redactionFixtureAnthropicKeyStub) {
		t.Errorf(
			"anthropic_api_key pattern %q does not match fixture stub %q; "+
				"update fixture or pattern to agree on key shape (§4.7.HC-032)",
			anthropicPattern, redactionFixtureAnthropicKeyStub,
		)
	}
}

// TestRedaction_HC032_AnthropicPatternDoesNotMatchSafeValue asserts that the
// Anthropic API key pattern does NOT match a benign non-secret value.
//
// Spec ref: specs/handler-contract.md §4.7.HC-032.
func TestRedaction_HC032_AnthropicPatternDoesNotMatchSafeValue(t *testing.T) {
	t.Parallel()

	var anthropicPattern string
	for _, entry := range redactionFixturePerHandlerPatterns {
		if entry.Name == "anthropic_api_key" {
			anthropicPattern = entry.Pattern
			break
		}
	}
	if anthropicPattern == "" {
		t.Fatal("anthropic_api_key pattern not found in redactionFixturePerHandlerPatterns")
	}

	safeValues := []string{
		"node-abc-123",
		"SUCCESS",
		"run-id-xyz",
		"agent_type=claude",
		"",
		"sk-ant-", // prefix only — too short to match
		"not-an-anthropic-key-at-all",
	}

	re := regexp.MustCompile(anthropicPattern)
	for _, v := range safeValues {
		v := v
		t.Run(v, func(t *testing.T) {
			t.Parallel()
			if re.MatchString(v) {
				t.Errorf(
					"anthropic_api_key pattern %q unexpectedly matches safe value %q; "+
						"pattern over-matches and would redact legitimate payload fields",
					anthropicPattern, v,
				)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: HC-033 compile-time schema check (startup-check negative case)
// ─────────────────────────────────────────────────────────────────────────────

// TestRedaction_HC033_SchemaViolationFixtureFieldMatchesRegex asserts that the
// "password" field in redactionFixtureSchemaViolation matches the HC-031 regex.
//
// This is the pre-condition for the startup-check negative test: the schema
// checker (hk-8i31.40) MUST reject any event type whose registered payload
// struct has a field that matches this regex. The startup-check implementation
// will consume redactionFixtureSchemaViolation as its negative-case input.
//
// Spec ref: specs/handler-contract.md §4.7.HC-033.
func TestRedaction_HC033_SchemaViolationFixtureFieldMatchesRegex(t *testing.T) {
	t.Parallel()

	re := regexp.MustCompile(redactionFixtureCommonPrefixRegex)

	// "password" is the violation field in redactionFixtureSchemaViolation.
	const violationField = "password"
	if !re.MatchString(violationField) {
		t.Errorf(
			"HC-031 regex %q does not match the schema-violation fixture field %q; "+
				"the fixture is not a valid negative case for the HC-033 startup check",
			redactionFixtureCommonPrefixRegex, violationField,
		)
	}

	// "node_id" is the safe field in the same struct; it MUST NOT match.
	const safeField = "node_id"
	if re.MatchString(safeField) {
		t.Errorf(
			"HC-031 regex %q unexpectedly matches safe field %q in schema-violation fixture; "+
				"fixture design is broken",
			redactionFixtureCommonPrefixRegex, safeField,
		)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: HC-034 no literal secret in fixture stubs
// ─────────────────────────────────────────────────────────────────────────────

// TestRedaction_HC034_FixtureStubsContainNoRealSecrets asserts that the
// redactionFixtureAnthropicKeyStub constant does NOT contain any non-'x'
// characters in its body portion (i.e., it is a structural stub, not a real
// credential).
//
// HC-034 requires that no secret value appear in any persisted record. This
// test is a defence-in-depth check ensuring the fixture itself cannot be a real
// key accidentally committed to the codebase.
//
// Spec ref: specs/handler-contract.md §4.7.HC-034.
func TestRedaction_HC034_FixtureStubsContainNoRealSecrets(t *testing.T) {
	t.Parallel()

	const prefix = "sk-ant-api03-"
	stub := redactionFixtureAnthropicKeyStub

	if !strings.HasPrefix(stub, prefix) {
		t.Fatalf("anthropic key stub %q does not start with expected prefix %q", stub, prefix)
	}

	body := stub[len(prefix):]
	for i, ch := range body {
		if ch != 'x' {
			t.Errorf(
				"anthropic key stub body contains non-'x' character %q at index %d; "+
					"stubs MUST use 'x'-padding only to prevent real credentials entering the codebase (§4.7.HC-034)",
				ch, i,
			)
		}
	}
}
