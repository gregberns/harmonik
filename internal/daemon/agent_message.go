package daemon

// agent_message.go — shared predicate for agent_message subscribe/recv filtering.
//
// N1 (agent-comms spec §8): there MUST be exactly ONE exported predicate,
// MatchAgentMessage, called by BOTH the live subscribe offer path
// (subscriptionStream.offer in subscribe.go) AND the durable replay scan
// (HandleSubscribe ScanAfter loop in subscribe.go). Do not add a second copy
// of this logic.

// AgentMessagePayload is the payload shape for "agent_message" events.
// Spec: agent-comms §1.1.
type AgentMessagePayload struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Topic     string `json:"topic,omitempty"`
	Body      string `json:"body"`
	InReplyTo string `json:"in_reply_to,omitempty"`
}

// MatchAgentMessage reports whether an agent_message payload satisfies the
// subscribe/recv addressing filter (to, from, topic). Empty filter values are
// wildcards (match any). Implements the §3 deliver predicate from the
// agent-comms spec:
//
//   - to=="" OR payload.To ∈ {to, "*"}   (directed-to-me or broadcast)
//   - from=="" OR payload.From == from    (sender filter)
//   - topic=="" OR payload.Topic == topic (topic filter)
//
// This is the single shared predicate (N1). It is called by BOTH the live
// subscribe offer path and the durable replay scan. There must not be two
// independent copies of this addressing logic.
func MatchAgentMessage(payload AgentMessagePayload, to, from, topic string) bool {
	if to != "" && payload.To != to && payload.To != "*" {
		return false
	}
	if from != "" && payload.From != from {
		return false
	}
	if topic != "" && payload.Topic != topic {
		return false
	}
	return true
}
