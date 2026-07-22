package core

import (
	"fmt"
	"regexp"
)

// AgentType is a handler-contract conformance class identifier (architecture.md §4.7, §6.1).
// An AgentType value MUST match the regex ^[a-z][a-z0-9-]{1,62}$ (AR-025).
// The four reserved MVH identifiers are declared as constants below.
//
// AgentType appears byte-for-byte identical across four cross-subsystem surfaces
// per AR-027: YAML policies, DOT node attributes, LaunchSpec.agent_type, and event payloads.
type AgentType string

// Reserved MVH agent-type identifiers (AR-025).
const (
	AgentTypeClaudeCode AgentType = "claude-code"
	AgentTypePi         AgentType = "pi"
	AgentTypeClaudeTwin AgentType = "claude-twin"
	AgentTypePiTwin     AgentType = "pi-twin"
	// AgentTypeCodex is the OpenAI codex harness identifier (codex-harness T1, hk-e8omz).
	// Registered but unreachable until C4 (ResolveHarness) wires the selection path.
	AgentTypeCodex AgentType = "codex"
)

// ReservedAgentTypes returns the reserved MVH agent-type identifiers declared
// above, in declaration order. It is the single source of truth for "is this a
// real agent type", so callers that must reject an unknown name do not carry
// their own copy of the list and drift from it.
//
// Shape validation is NOT sufficient for that question and this exists because
// of it: Valid() only checks the AR-025 regex, and a plausible-but-wrong name
// like "claude" satisfies the regex while matching no harness. A caller doing a
// membership test needs this set, not Valid().
func ReservedAgentTypes() []AgentType {
	return []AgentType{
		AgentTypeClaudeCode,
		AgentTypePi,
		AgentTypeClaudeTwin,
		AgentTypePiTwin,
		AgentTypeCodex,
	}
}

// Reserved reports whether a is one of the reserved MVH agent-type identifiers.
// This is the membership check that Valid() deliberately does not perform.
func (a AgentType) Reserved() bool {
	for _, r := range ReservedAgentTypes() {
		if a == r {
			return true
		}
	}
	return false
}

// AgentTypeRegexPattern is the canonical regex string for the agent_type
// identifier shape declared in architecture.md §6.1 (AR-025).
// It is exported so that specaudit sensors can verify the spec-text regex
// and the runtime regex stay byte-for-byte identical.
const AgentTypeRegexPattern = `^[a-z][a-z0-9-]{1,62}$`

// agentTypeRegex enforces the AR-025 shape: lowercase alphanumeric + hyphen,
// must start with a letter, length 2..63 inclusive.
var agentTypeRegex = regexp.MustCompile(AgentTypeRegexPattern)

// Valid reports whether a matches the agent-type regex shape (AR-027).
// It does NOT check membership in the reserved set — that is a separate concern.
func (a AgentType) Valid() bool { return agentTypeRegex.MatchString(string(a)) }

// MarshalText implements encoding.TextMarshaler so AgentType serialises
// correctly in JSON and YAML workflow definitions.
// It rejects any value that does not match the AR-025 regex.
func (a AgentType) MarshalText() ([]byte, error) {
	if !a.Valid() {
		return nil, fmt.Errorf("agenttype: invalid value %q; must match ^[a-z][a-z0-9-]{1,62}$", string(a))
	}
	return []byte(a), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that does not match the AR-025 regex shape,
// satisfying the AR-027 requirement that malformed identifiers are rejected at decode time.
func (a *AgentType) UnmarshalText(text []byte) error {
	v := AgentType(text)
	if !v.Valid() {
		return fmt.Errorf("agenttype: invalid value %q; must match ^[a-z][a-z0-9-]{1,62}$", string(text))
	}
	*a = v
	return nil
}
