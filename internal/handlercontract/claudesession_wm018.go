package handlercontract

import (
	"encoding/json"
	"fmt"
	"io"
)

type claudeOutputJSON struct {
	// SessionID is the Claude Code session identifier emitted by
	// `claude --output-format json`.  Present on every successful completion.
	SessionID string `json:"session_id"`
}

// ErrMissingClaudeSessionID is returned by ParseClaudeSessionID when the
// parsed JSON carries an empty or absent `session_id` field.
//
// It wraps ErrStructural: a missing session_id is a structural defect in the
// subprocess output that cannot be recovered without a fresh subprocess launch.
// errors.Is(ErrMissingClaudeSessionID, ErrStructural) is true.
var ErrMissingClaudeSessionID = fmt.Errorf(
	"handlercontract: claude output missing session_id: %w", ErrStructural,
)

// ParseClaudeSessionID reads the JSON output of a `claude -p ... --output-format
// json` invocation from r and returns the Claude Code session identifier.
//
// The identifier is captured from the implementer's first (phase =
// implementer-initial) launch and persisted into Run.context.claude_session_id
// by the daemon per [execution-model.md §4.3.EM-015d].  On subsequent
// implementer iterations the daemon drives `claude --resume <session_id>` so
// the implementer continues the same Claude session.
//
// Returns ErrMissingClaudeSessionID (wrapping ErrStructural) when the JSON
// contains an empty or absent session_id field.  Returns a wrapped
// ErrStructural for JSON parse errors.  The caller is responsible for closing
// r; ParseClaudeSessionID does not close it.
func ParseClaudeSessionID(r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("handlercontract: reading claude output: %w: %w", err, ErrStructural)
	}

	var out claudeOutputJSON
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("handlercontract: parsing claude output JSON: %w: %w", err, ErrStructural)
	}

	if out.SessionID == "" {
		return "", ErrMissingClaudeSessionID
	}

	return out.SessionID, nil
}
