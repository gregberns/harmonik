package sessioncapture

// scrub_keyclass_hknqkoz_test.go — regression for hk-nqkoz.
//
// The key alternation matched bare substrings, so any key CONTAINING "auth" or
// "token" (author, authored_at, authority, stop_token, max_tokens) had its value
// redacted, corrupting the record→replay corpus. keyIsSensitive now classifies
// keys by WHOLE segment (snake_case + camelCase) with a deny-set for known
// non-secret token metadata. This test pins BOTH directions: false-positive keys
// pass through verbatim, and — the load-bearing half — real secret keys still
// redact (a classifier bug here would be a secret LEAK, worse than the original).

import (
	"strings"
	"testing"
)

func TestKeyIsSensitive_hknqkoz(t *testing.T) {
	t.Parallel()

	secret := []string{
		"secret", "client_secret", "clientSecret",
		"token", "access_token", "accessToken", "auth_token", "refresh_token",
		"password", "passWord",
		"api_key", "apiKey", "api-key", "x-api-key", "X-Api-Key",
		"auth", "authorization", "Authorization", "x-auth-token",
		// Concatenated, no separator / no camelCase boundary — WHOLE-segment
		// matching alone would miss these and LEAK (hk-nqkoz review).
		"authtoken", "accesstoken", "sessiontoken", "bearertoken", "idtoken",
		"ACCESSTOKEN", "AUTHTOKEN", "authtoken_v2", "access_tokens",
	}
	for _, k := range secret {
		if !keyIsSensitive(k) {
			t.Errorf("keyIsSensitive(%q) = false, want true — a secret key must be redacted", k)
		}
	}

	notSecret := []string{
		"author", "authored_at", "authored_by", "authoredAt", "co_author", "authority",
		"stop_token", "stopToken", "stop_tokens",
		"max_tokens", "maxTokens", "min_tokens", "num_tokens",
		"total_tokens", "prompt_tokens", "completion_tokens", "cached_tokens",
		"token_count", "event", "status", "message",
		// "token" derivatives that are not secrets (excluded via "tokeniz").
		"tokenizer", "tokenize", "tokenized", "tokenization",
	}
	for _, k := range notSecret {
		if keyIsSensitive(k) {
			t.Errorf("keyIsSensitive(%q) = true, want false — over-redaction corrupts the replay corpus", k)
		}
	}
}

// TestScrubLine_KeyClassification_hknqkoz drives the full scrubLine path: a
// secret-bearing value is redacted while a same-line "author" / "stop_token"
// value survives verbatim.
func TestScrubLine_KeyClassification_hknqkoz(t *testing.T) {
	t.Parallel()

	line := `{"author":"Ada Lovelace","stop_token":"<END>","access_token":"bearerVALUE123456","max_tokens":4096}`
	got := string(scrubLine([]byte(line)))

	// False positives preserved.
	if !strings.Contains(got, `"author":"Ada Lovelace"`) {
		t.Errorf("author over-redacted: %q", got)
	}
	if !strings.Contains(got, `"stop_token":"<END>"`) {
		t.Errorf("stop_token over-redacted: %q", got)
	}
	if !strings.Contains(got, `"max_tokens":4096`) {
		t.Errorf("max_tokens (numeric) disturbed: %q", got)
	}
	// Real secret still redacted.
	if strings.Contains(got, "bearerVALUE123456") {
		t.Fatalf("access_token secret LEAKED: %q", got)
	}
	if !strings.Contains(got, redactedSentinel) {
		t.Fatalf("access_token not redacted: %q", got)
	}
}
