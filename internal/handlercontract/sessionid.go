package handlercontract

import (
	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// NewSessionID generates a fresh daemon-assigned session identifier.
//
// The returned value is a UUIDv4 string wrapped as core.SessionID.  Per
// handler-contract.md §6.1, session IDs are daemon-assigned at Launch time and
// carried on every handler-lifecycle event in the session's lifetime.
//
// Callers in internal/handler MUST use this function rather than importing
// internal/core directly — the EV-002b import boundary (event-model.md §4.1)
// forbids handler-side packages from importing core so they cannot accidentally
// instantiate EventIDGenerator independently.
func NewSessionID() core.SessionID {
	return core.SessionID(uuid.New().String())
}
