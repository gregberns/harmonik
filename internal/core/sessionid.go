package core

// SessionID is a daemon-assigned session identifier (handler-contract.md §4.1, §6.1).
//
// SessionID is a named type over string so that it is not interchangeable with
// other string-typed identifiers at compile time.  The underlying value is
// opaque to non-handler consumers; event-model.md §8.3 notes that handlers
// generate it as a UUIDv7 string, but callers outside the handler layer MUST
// treat it as an opaque string per handler-contract.md §6.1.
//
// The value is daemon-assigned (unique within a daemon generation) and is
// carried on every handler-lifecycle event in the session's lifetime.
type SessionID string
