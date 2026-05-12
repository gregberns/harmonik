package handlercontract

import "github.com/gregberns/harmonik/internal/core"

// EventEnvelope is a type alias for core.EventEnvelope, re-exported so that
// handler-side packages can name the type without importing internal/core
// directly (which is forbidden by EV-002b; event-model.md §4.1).
//
// Because this is a true Go alias (not a distinct named type), values of type
// handlercontract.EventEnvelope and core.EventEnvelope are interchangeable in
// all contexts — no conversion is required.
type EventEnvelope = core.EventEnvelope

// SessionID is a type alias for core.SessionID, re-exported for the same
// EV-002b import-boundary reason as EventEnvelope above.
type SessionID = core.SessionID

// Outcome is a type alias for core.Outcome, re-exported for the same
// EV-002b import-boundary reason as EventEnvelope above.
type Outcome = core.Outcome

// EventID is a type alias for core.EventID, re-exported for the same
// EV-002b import-boundary reason as EventEnvelope above.
type EventID = core.EventID

// AgentTypeClaudeCode is a re-export of core.AgentTypeClaudeCode — the
// reserved MVH agent-type identifier for the "claude-code" handler.
//
// Handler-side packages (internal/handler, etc.) MUST use this constant
// rather than importing internal/core directly (EV-002b boundary;
// event-model.md §4.1).
const AgentTypeClaudeCode = core.AgentTypeClaudeCode
