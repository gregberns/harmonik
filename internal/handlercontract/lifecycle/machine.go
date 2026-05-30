package lifecycle

import (
	"sync"
	"time"
)

const maxHistorySize = 50

// Machine is the per-session lifecycle state machine (HC-064..HC-067).
//
// All methods are safe for concurrent use. The zero value is not valid;
// obtain a Machine via New.
type Machine struct {
	mu        sync.Mutex
	sessID    string // harmonik session_id (UUIDv7)
	runID     string // bead run_id for event correlation
	current   LifecycleState
	enteredAt time.Time
	history   [maxHistorySize]Transition
	head      uint8 // ring: index of oldest entry
	count     uint8 // ring: number of valid entries (0..maxHistorySize)
}

// New constructs a Machine in StateSpawning.
//
// sessID is the harmonik session_id (UUIDv7) assigned at Session.Launch time.
// runID is the bead run_id carried on lifecycle events.
func New(sessID, runID string) *Machine {
	return &Machine{
		sessID:    sessID,
		runID:     runID,
		current:   StateSpawning,
		enteredAt: time.Now(),
	}
}

// Transition attempts to move the machine from its current state to to.
//
// Returns *InvalidStateTransitionError (wrapping ErrInvalidStateTransition)
// if the edge is absent from the valid-transitions table.
//
// errCode and errMsg are recorded in the history Transition only when
// to==StateFailed; they are ignored for other target states.
func (m *Machine) Transition(to LifecycleState, reason TransitionReason, errCode, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	from := m.current
	if !isValidTransition(from, to) {
		return &InvalidStateTransitionError{From: from, To: to, SessionID: m.sessID}
	}

	now := time.Now()
	t := Transition{
		From:   from,
		To:     to,
		At:     now,
		Reason: reason,
	}
	if to == StateFailed {
		t.ErrCode = errCode
		t.ErrMsg = errMsg
	}

	m.appendHistory(t)
	m.current = to
	m.enteredAt = now
	return nil
}

// Current returns the machine's current state. Safe for concurrent use.
func (m *Machine) Current() LifecycleState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

// EnteredAt returns the time the machine entered its current state.
func (m *Machine) EnteredAt() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enteredAt
}

// History returns a snapshot copy of the transition ring, oldest entry first
// (HC-067). The slice length is at most maxHistorySize (50).
func (m *Machine) History() []Transition {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.count == 0 {
		return nil
	}
	out := make([]Transition, m.count)
	for i := uint8(0); i < m.count; i++ {
		out[i] = m.history[(uint(m.head)+uint(i))%maxHistorySize]
	}
	return out
}

// SessionID returns the session identifier supplied at construction.
func (m *Machine) SessionID() string {
	return m.sessID
}

// RunID returns the run identifier supplied at construction.
func (m *Machine) RunID() string {
	return m.runID
}

// RecordActivity updates the machine's enteredAt timestamp to now without
// performing a state transition. Call this for heartbeat events and other
// activity signals that should reset the silent-hang watchdog timer without
// advancing lifecycle state (HC-026a).
//
// Thread-safe.
func (m *Machine) RecordActivity() {
	m.mu.Lock()
	m.enteredAt = time.Now()
	m.mu.Unlock()
}

// appendHistory appends t to the ring buffer. When the buffer is full the
// oldest entry is silently evicted (tail-keep, drop-oldest per HC-067).
func (m *Machine) appendHistory(t Transition) {
	idx := (uint(m.head) + uint(m.count)) % maxHistorySize
	m.history[idx] = t
	if m.count < maxHistorySize {
		m.count++
	} else {
		// Buffer full: overwrite oldest slot and advance head.
		m.head = (m.head + 1) % maxHistorySize
	}
}
