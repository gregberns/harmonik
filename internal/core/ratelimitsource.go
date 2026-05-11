package core

import (
	"fmt"
	"regexp"
)

// RateLimitSource is the typed provider-source identifier carried by the
// rate_limit_source field of an agent_rate_limit_status event (event-model.md
// §8.3.6 §6.3).
//
// The spec declares rate_limit_source as <String> | null with no enumerated
// vocabulary — the provider-source set is open and expected to grow as
// additional LLM providers are supported. RateLimitSource is therefore an opaque
// typed string constrained by a regex shape, not a closed enum. Known providers
// at MVH are listed as constants below; additional values are accepted by Valid()
// as long as they match the regex shape.
//
// Shape: ^[a-z][a-z0-9-]*$ — lowercase letter start, followed by lowercase
// alphanumeric characters and hyphens. This allows identifiers like "anthropic",
// "openai", "vertex-ai", "anthropic-tier-1", etc.
//
// The vocabulary will be formally enumerated in a future event-model.md revision
// when the provider-source surface stabilizes across MVH adapters.
type RateLimitSource string

// Known MVH rate-limit-source identifiers. Additional provider values are valid
// as long as they match the ^[a-z][a-z0-9-]*$ regex shape.
const (
	// RateLimitSourceAnthropic identifies rate limits reported by the Anthropic API.
	RateLimitSourceAnthropic RateLimitSource = "anthropic"

	// RateLimitSourceOpenAI identifies rate limits reported by the OpenAI API.
	RateLimitSourceOpenAI RateLimitSource = "openai"
)

// rateLimitSourceRegex enforces the shape: lowercase letter start, followed by
// zero or more lowercase alphanumeric characters and hyphens.
var rateLimitSourceRegex = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Valid reports whether s matches the rate-limit-source regex shape.
// It does NOT restrict to the declared constants — the vocabulary is open
// per event-model.md §6.3.
func (s RateLimitSource) Valid() bool {
	return rateLimitSourceRegex.MatchString(string(s))
}

// MarshalText implements encoding.TextMarshaler so RateLimitSource serialises
// correctly in JSON and YAML.
// It rejects any value that does not match the ^[a-z][a-z0-9-]*$ regex shape.
func (s RateLimitSource) MarshalText() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("ratelimitsource: invalid value %q; must match ^[a-z][a-z0-9-]*$", string(s))
	}
	return []byte(s), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that does not match the ^[a-z][a-z0-9-]*$ regex shape.
func (s *RateLimitSource) UnmarshalText(text []byte) error {
	v := RateLimitSource(text)
	if !v.Valid() {
		return fmt.Errorf("ratelimitsource: invalid value %q; must match ^[a-z][a-z0-9-]*$", string(text))
	}
	*s = v
	return nil
}
