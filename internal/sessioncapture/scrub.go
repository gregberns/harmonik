package sessioncapture

import "regexp"

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

// keyValuePattern additionally scrubs the VALUE of any JSON key whose NAME
// matches the HC-031 common-prefix shape (secret|token|password|api_key|auth),
// regardless of the value's shape — the belt to valuePatterns' braces. The
// value's quotes are preserved; only the inner text is replaced.
var keyValuePattern = regexp.MustCompile(`(?i)("(?:[^"]*(?:secret|token|password|api[_-]?key|auth)[^"]*)"\s*:\s*")([^"]*)(")`)

// scrubLine applies the HC-032 value-pattern scrub to one line of captured
// OUTPUT bytes, returning a redacted copy. The line's newline (if any) is
// preserved. Verbatim in-memory tee + scrub-at-persist is the AIS-014 shape:
// the redaction happens here in the consumer, never in the tee.
func scrubLine(line []byte) []byte {
	out := keyValuePattern.ReplaceAll(line, []byte(`${1}`+redactedSentinel+`${3}`))
	for _, re := range valuePatterns {
		out = re.ReplaceAll(out, []byte(redactedSentinel))
	}
	return out
}
