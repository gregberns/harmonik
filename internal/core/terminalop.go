package core

import "fmt"

// TerminalOp is the operation harmonik issues to Beads at a terminal workflow
// boundary (beads-integration.md §6.1).
// One of: claim, close, reopen, reset.
// Validators reject any other value.
type TerminalOp string

// TerminalOp values per beads-integration.md §6.1 ENUM declaration.
//
//   - TerminalOpClaim  — activity-marker write (open → in_progress); auto-resettable per BI-010d.
//   - TerminalOpClose  — truth-claim write (in_progress → closed).
//   - TerminalOpReopen — truth-claim write (closed → open).
//   - TerminalOpReset  — activity-marker write (in_progress → open); startup orphan-sweep only per BI-010d.
const (
	TerminalOpClaim  TerminalOp = "claim"
	TerminalOpClose  TerminalOp = "close"
	TerminalOpReopen TerminalOp = "reopen"
	// TerminalOpReset is the fourth terminal op, introduced by BI-010d.
	// It is issued exclusively by the daemon startup orphan-sweep (PL-006 extended per hk-iuaed.2)
	// to reset stale in_progress beads back to open. It is NOT an intra-run write.
	//
	// Spec ref: specs/beads-integration.md §4.4 BI-010d; §6.1 ENUM TerminalOp.
	TerminalOpReset TerminalOp = "reset"
)

// Valid reports whether op is one of the four declared TerminalOp constants.
func (op TerminalOp) Valid() bool {
	switch op {
	case TerminalOpClaim, TerminalOpClose, TerminalOpReopen, TerminalOpReset:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so TerminalOp serialises
// correctly in JSON and YAML workflow definitions.
func (op TerminalOp) MarshalText() ([]byte, error) {
	if !op.Valid() {
		return nil, fmt.Errorf("terminalop: unknown value %q", string(op))
	}
	return []byte(op), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the four declared constants.
func (op *TerminalOp) UnmarshalText(text []byte) error {
	v := TerminalOp(text)
	if !v.Valid() {
		return fmt.Errorf("terminalop: unknown value %q; must be one of claim, close, reopen, reset", string(text))
	}
	*op = v
	return nil
}
