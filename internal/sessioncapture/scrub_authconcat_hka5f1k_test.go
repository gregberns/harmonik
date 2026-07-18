package sessioncapture

// scrub_authconcat_hka5f1k_test.go — regression for hk-a5f1k.
//
// The hk-nqkoz fix (9d179da7) matched "auth" only as a WHOLE key segment, giving
// the compact-substring escape hatch to "token" but NOT "auth". A separator-less
// / non-camelCase auth-family key (authcode, authkey, authheader) therefore
// classified non-sensitive and its value leaked verbatim into the capture corpus
// — a NARROWING in the leak direction (worse for a scrubber). keyIsSensitive now
// gives "auth" the same compact-substring treatment as "token", guarded against
// the author/authority family. This test pins the newly-closed leaks AND re-pins
// the over-redaction guard so the two stay balanced.

import (
	"strings"
	"testing"
)

func TestKeyIsSensitive_AuthConcatLeak_hka5f1k(t *testing.T) {
	t.Parallel()

	// Concatenated / separator-less auth-family keys that WHOLE-segment matching
	// missed and LEAKED before this fix. All must now redact.
	secret := []string{
		"authcode", "authkey", "authheader", "authz", "authsecret",
		"AUTHCODE", "auth-code", "x-authcode", "xauthkey",
		"authtoken", "AUTHTOKEN", // also caught by the "token" rule; belt-and-suspenders
		"oauthtoken", "oauthkey",
	}
	for _, k := range secret {
		if !keyIsSensitive(k) {
			t.Errorf("keyIsSensitive(%q) = false, want true — concatenated auth-family secret must be redacted (hk-a5f1k)", k)
		}
	}

	// The author/authority family must STILL pass through — the compact "auth"
	// rule is guarded on "author", which every one of these contains. A regression
	// re-introducing hk-nqkoz over-redaction would trip here.
	notSecret := []string{
		"author", "authors", "authored", "authored_at", "authored_by", "authoredAt",
		"authoring", "authorship", "co_author", "authority", "authorities",
	}
	for _, k := range notSecret {
		if keyIsSensitive(k) {
			t.Errorf("keyIsSensitive(%q) = true, want false — author/authority family must not be redacted (hk-nqkoz guard)", k)
		}
	}
}

// TestScrubLine_AuthConcatLeak_hka5f1k drives the full scrubLine path: a
// concatenated auth-family secret is redacted while a same-line "author" value
// survives verbatim.
func TestScrubLine_AuthConcatLeak_hka5f1k(t *testing.T) {
	t.Parallel()

	line := `{"author":"Ada Lovelace","authcode":"AQABsecretGrantCode123","authority":"root"}`
	got := string(scrubLine([]byte(line)))

	if strings.Contains(got, "AQABsecretGrantCode123") {
		t.Fatalf("authcode secret LEAKED: %q", got)
	}
	if !strings.Contains(got, redactedSentinel) {
		t.Fatalf("authcode not redacted: %q", got)
	}
	if !strings.Contains(got, `"author":"Ada Lovelace"`) {
		t.Errorf("author over-redacted: %q", got)
	}
	if !strings.Contains(got, `"authority":"root"`) {
		t.Errorf("authority over-redacted: %q", got)
	}
}
