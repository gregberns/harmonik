package handlercontract_test

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const redactionFixtureCommonPrefixRegex = `(?i)(secret|token|password|api[_-]?key|auth)`

const redactionFixtureRedactedSentinel = "<redacted>"

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

var redactionFixtureSecretNamedFieldNames = []string{
	"secret",
	"token",
	"password",
	"api_key",
	"api-key",
	"apikey",
	"auth",
}

var redactionFixtureSafeFieldNames = []string{
	"node_id",
	"run_id",
	"status",
	"exit_code",
	"agent_type",
	"worker_id",
}

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

const redactionFixtureAnthropicKeyStub = "sk-ant-api03-" +
	"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

type redactionFixtureSafePayload struct {
	NodeID    string `json:"node_id"`
	RunID     string `json:"run_id"`
	Status    string `json:"status"`
	ExitCode  int    `json:"exit_code"`
	AgentType string `json:"agent_type"`
}

type redactionFixtureSchemaViolation struct {
	// "password" matches HC-031; any registered event type with this field MUST
	// be rejected at startup by the schema checker (hk-8i31.40).
	Password string `json:"password"`

	// Safe field — present to confirm the check triggers on the bad field, not
	// on the entire struct.
	NodeID string `json:"node_id"`
}

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

func redactionFixtureJSONFieldNames(t *testing.T, typ reflect.Type) []string {
	t.Helper()
	if typ.Kind() != reflect.Struct {
		t.Fatalf("redactionFixtureJSONFieldNames: %v is a %s, not a struct", typ, typ.Kind())
	}
	names := make([]string, 0, typ.NumField())
	for i := range typ.NumField() {
		tag, ok := typ.Field(i).Tag.Lookup("json")
		if !ok {
			t.Fatalf("redactionFixtureJSONFieldNames: %v field %q has no json tag", typ, typ.Field(i).Name)
		}
		names = append(names, strings.Split(tag, ",")[0])
	}
	return names
}

// TestRedaction_HC031_FixtureStructsMatchFieldNameLists asserts that the json
// field names carried by the four fixture payload structs are exactly the
// name lists the HC-031/HC-033 sensors assert against.
//
// The lists above are hand-maintained copies of the struct tags. Without this
// sensor the two can drift silently — a field added to (or renamed on) a
// fixture struct would simply stop being covered, and the sensors would keep
// passing against a stale list while claiming to detect drift.
//
// Spec ref: specs/handler-contract.md §4.7.HC-031, §4.7.HC-033.
func TestRedaction_HC031_FixtureStructsMatchFieldNameLists(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		typ  reflect.Type
		want []string
	}{
		{
			name: "secret_named_payload",
			typ:  reflect.TypeOf(redactionFixtureSecretNamedPayload{}),
			// The secret-named fields plus the documented safe control field.
			want: append(append([]string{}, redactionFixtureSecretNamedFieldNames...), "worker_id"),
		},
		{
			name: "safe_payload",
			typ:  reflect.TypeOf(redactionFixtureSafePayload{}),
			want: []string{"node_id", "run_id", "status", "exit_code", "agent_type"},
		},
		{
			name: "secret_value_payload",
			typ:  reflect.TypeOf(redactionFixtureSecretValuePayload{}),
			want: []string{"provider_key", "alt_provider_key", "safe_value"},
		},
		{
			name: "schema_violation",
			typ:  reflect.TypeOf(redactionFixtureSchemaViolation{}),
			want: []string{"password", "node_id"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := redactionFixtureJSONFieldNames(t, tc.typ)
			if !slices.Equal(got, tc.want) {
				t.Errorf(
					"%v json field names = %v; want %v — the fixture struct and the "+
						"hand-maintained field-name list have drifted apart",
					tc.typ, got, tc.want,
				)
			}
		})
	}
}

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

	re := regexp.MustCompile(redactionFixtureCommonPrefixRegex)

	for _, fieldName := range redactionFixtureSafeFieldNames {
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

// TestRedaction_HC032_PerHandlerPatternsCompile verifies that every pattern in
// redactionFixturePerHandlerPatterns is a valid Go regex. A syntax error here
// means the handler spec declares an uncompilable pattern, which would prevent
// daemon init per HC-032.
//
// Spec ref: specs/handler-contract.md §4.7.HC-032.
func TestRedaction_HC032_PerHandlerPatternsCompile(t *testing.T) {
	t.Parallel()

	for _, entry := range redactionFixturePerHandlerPatterns {
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

	const violationField = "password"
	if !re.MatchString(violationField) {
		t.Errorf(
			"HC-031 regex %q does not match the schema-violation fixture field %q; "+
				"the fixture is not a valid negative case for the HC-033 startup check",
			redactionFixtureCommonPrefixRegex, violationField,
		)
	}

	const safeField = "node_id"
	if re.MatchString(safeField) {
		t.Errorf(
			"HC-031 regex %q unexpectedly matches safe field %q in schema-violation fixture; "+
				"fixture design is broken",
			redactionFixtureCommonPrefixRegex, safeField,
		)
	}
}

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
