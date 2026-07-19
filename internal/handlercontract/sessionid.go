package handlercontract

import (
	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// NewSessionID generates a fresh daemon-assigned session identifier.
//
// The returned value is a UUIDv7 string wrapped as core.SessionID.  Per
// handler-contract.md §4.1/§6.1, the handler-side session_id is a UUIDv7 minted
// at Launch time (time-ordered, matching run/event/transition IDs) and carried
// on every handler-lifecycle event in the session's lifetime.  A v4 is used only
// as a fallback if the OS entropy read for v7 fails (astronomically rare), so a
// caller always receives a syntactically valid UUID.
//
// Callers in internal/handler MUST use this function rather than importing
// internal/core directly — the EV-002b import boundary (event-model.md §4.1)
// forbids handler-side packages from importing core so they cannot accidentally
// instantiate EventIDGenerator independently.
func NewSessionID() core.SessionID {
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	return core.SessionID(id.String())
}
