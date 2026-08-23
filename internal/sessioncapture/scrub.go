package sessioncapture

import (
	"regexp"
	"strings"
)

const redactedSentinel = "<redacted>"

var valuePatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{8,}`),
	// Generic OpenAI-style secret/publishable keys (sk-..., pk-...).
	regexp.MustCompile(`\b(?:sk|pk)-[A-Za-z0-9]{16,}\b`),
	// GitHub tokens (ghp_, gho_, ghs_, ghr_, github_pat_...).
	regexp.MustCompile(`\bgh[posr]_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`),
	// AWS access key ids.
	regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`),
}

var keyValueCandidate = regexp.MustCompile(`(?i)("([^"]*(?:secret|token|password|api[_-]?key|auth)[^"]*)")(\s*:\s*")((?:[^"\\]|\\.)*)(")`)

var nonLetter = regexp.MustCompile(`[^A-Za-z]+`)

var unambiguousSecretWords = []string{"password", "secret", "authorization", "apikey"}

var denyKeys = map[string]bool{
	"stoptoken": true, "stoptokens": true,
	"maxtokens": true, "maxtoken": true, "mintokens": true,
	"numtokens": true, "ntokens": true,
	"tokencount": true, "tokencounts": true,
	"totaltokens": true, "prompttokens": true, "completiontokens": true,
	"inputtokens": true, "outputtokens": true, "cachedtokens": true,
	"reasoningtokens": true,
}

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
	if strings.Contains(compact, "auth") && !strings.Contains(compact, "author") {
		return true
	}
	return false
}

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
