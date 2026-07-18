package sessioncapture

import (
	"regexp"
	"strings"
	"unicode"
)

// redactedSentinel is the replacement for any scrubbed value. Matches
// core.RedactedSentinel / HC-031 / HC-032 ("<redacted>"). Duplicated (not
// imported) to keep this consumer package free of a core dependency.
const redactedSentinel = "<redacted>"

// valuePatterns are the HC-032-style value-shape regexes: secret formats
// recognisable by their VALUE, independent of any surrounding JSON key. These
// are what makes OUTPUT scrubbing work on a raw NDJSON byte stream where field
// structure is not parsed.
//
// The set is intentionally conservative (known provider key shapes + long
// bearer/hex tokens); it is the per-handler pattern surface HC-032 requires a
// secret-bearing provider to declare. Extend it as new providers land on the
// structured driver.
var valuePatterns = []*regexp.Regexp{
	// Anthropic API keys (sk-ant-...), the canonical HC-032 example.
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{8,}`),
	// Generic OpenAI-style secret/publishable keys (sk-..., pk-...).
	regexp.MustCompile(`\b(?:sk|pk)-[A-Za-z0-9]{16,}\b`),
	// GitHub tokens (ghp_, gho_, ghs_, ghr_, github_pat_...).
	regexp.MustCompile(`\bgh[posr]_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`),
	// AWS access key ids.
	regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`),
}

// keyValueCandidate finds a JSON "key":"value" string pair whose key CONTAINS
// one of the HC-031 secret-indicating substrings. It is only a cheap prefilter:
// keyIsSensitive makes the precise decision, so a substring match here does NOT
// by itself redact. This split (broad regex + exact classifier) is what lets us
// avoid over-redaction without a lookaround (Go's RE2 has none).
//
// Capture groups: 1=quoted key, 2=key inner text, 3=`":"` separator (with the
// opening value quote), 4=value body, 5=closing value quote.
//
// The value group `(?:[^"\\]|\\.)*` matches a JSON string body: it consumes any
// escape sequence (`\.`) — including an escaped quote `\"` — so the value
// terminates only at the first UNescaped closing quote. A plain `[^"]*` would
// stop at the escaped quote inside {"token":"abc\"def"}, leaking the real secret
// tail (`def`) into the persisted corpus (hk-13ff4).
var keyValueCandidate = regexp.MustCompile(`(?i)("([^"]*(?:secret|token|password|api[_-]?key|auth)[^"]*)")(\s*:\s*")((?:[^"\\]|\\.)*)(")`)

// nonLetter strips every non-letter rune (separators AND digits) to form a key's
// compact comparison form.
var nonLetter = regexp.MustCompile(`[^A-Za-z]+`)

// unambiguousSecretWords are HC-031 secret indicators specific enough to match
// as a compact SUBSTRING of the key: nothing benign contains them, so a
// substring test here can only over-redact, never leak. This catches casings
// that segment-splitting would miss, e.g. "passWord" or "clientSecret".
var unambiguousSecretWords = []string{"password", "secret", "authorization", "apikey"}

// segmentSecretWords are secret indicators that share a PREFIX with common
// non-secret keys ("auth" ⊂ author/authority/authored), so they are matched only
// as a WHOLE key segment (snake_case or camelCase), never as a bare substring
// (hk-nqkoz).
var segmentSecretWords = map[string]bool{"auth": true}

// denyKeys are compact key forms that share a segment with a sensitive word but
// are known NON-secret token metadata (LLM sampling / usage counters). They are
// allowed through un-redacted to keep the replay corpus faithful (hk-nqkoz).
// Every entry is a well-known non-credential field name; none is a secret, so
// listing it here cannot cause a leak.
var denyKeys = map[string]bool{
	"stoptoken": true, "stoptokens": true,
	"maxtokens": true, "maxtoken": true, "mintokens": true,
	"numtokens": true, "ntokens": true,
	"tokencount": true, "tokencounts": true,
	"totaltokens": true, "prompttokens": true, "completiontokens": true,
	"inputtokens": true, "outputtokens": true, "cachedtokens": true,
	"reasoningtokens": true,
}

// keyIsSensitive reports whether a JSON key NAME denotes a secret-bearing field.
// An explicit denyKeys metadata field is never sensitive. Otherwise the key is
// sensitive if:
//   - its compact form contains an unambiguousSecretWords indicator; OR
//   - its compact form contains "token" but not "tokeniz" (tokenizer/tokenize
//     are not secrets, and the token-usage counters are in denyKeys) — "token"
//     is a specific enough substring that it does not occur in benign keys like
//     author/authority, so a compact-substring test both catches concatenated
//     forms with no separator (authtoken, accesstoken, ACCESSTOKEN — which
//     WHOLE-segment matching would miss and leak) and stays false on non-secrets;
//     OR
//   - a WHOLE segment (split on non-letters AND lower→upper camelCase) is a
//     segmentSecretWords indicator ("auth"), distinguishing auth_token/authHeader
//     (secret) from author/authority (not).
func keyIsSensitive(key string) bool {
	compact := strings.ToLower(nonLetter.ReplaceAllString(key, ""))
	if denyKeys[compact] {
		return false
	}
	for _, w := range unambiguousSecretWords {
		if strings.Contains(compact, w) {
			return true
		}
	}
	if strings.Contains(compact, "token") && !strings.Contains(compact, "tokeniz") {
		return true
	}
	for _, seg := range splitKeySegments(key) {
		if segmentSecretWords[seg] {
			return true
		}
	}
	return false
}

// splitKeySegments lowercases and splits a key into word segments on any
// non-letter rune and on each lower→upper camelCase boundary. "accessToken" ->
// [access token]; "auth_token" -> [auth token]; "authority" -> [authority].
func splitKeySegments(key string) []string {
	var segs []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			segs = append(segs, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	var prev rune
	for _, r := range key {
		if !unicode.IsLetter(r) {
			flush()
			prev = r
			continue
		}
		if unicode.IsLower(prev) && unicode.IsUpper(r) {
			flush()
		}
		cur = append(cur, r)
		prev = r
	}
	flush()
	return segs
}

// scrubLine applies the HC-032 value-pattern scrub to one line of captured
// OUTPUT bytes, returning a redacted copy. The line's newline (if any) is
// preserved. Verbatim in-memory tee + scrub-at-persist is the AIS-014 shape:
// the redaction happens here in the consumer, never in the tee.
func scrubLine(line []byte) []byte {
	out := keyValueCandidate.ReplaceAllFunc(line, func(m []byte) []byte {
		g := keyValueCandidate.FindSubmatch(m)
		if !keyIsSensitive(string(g[2])) {
			return m // false-positive key (e.g. author, stop_token): leave verbatim.
		}
		res := make([]byte, 0, len(g[1])+len(g[3])+len(redactedSentinel)+len(g[5]))
		res = append(res, g[1]...)
		res = append(res, g[3]...)
		res = append(res, redactedSentinel...)
		res = append(res, g[5]...)
		return res
	})
	for _, re := range valuePatterns {
		out = re.ReplaceAll(out, []byte(redactedSentinel))
	}
	return out
}
