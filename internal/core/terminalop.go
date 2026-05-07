package core

import "fmt"

// TerminalOp is the operation harmonik issues to Beads at a terminal workflow
// boundary (beads-integration.md §6.1).
// One of: claim, close, reopen.
// Validators reject any other value.
type TerminalOp string

// TerminalOp values per beads-integration.md §6.1 ENUM declaration.
const (
	TerminalOpClaim  TerminalOp = "claim"
	TerminalOpClose  TerminalOp = "close"
	TerminalOpReopen TerminalOp = "reopen"
)

// Valid reports whether op is one of the three declared TerminalOp constants.
func (op TerminalOp) Valid() bool {
	switch op {
	case TerminalOpClaim, TerminalOpClose, TerminalOpReopen:
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
// It rejects any value that is not one of the three declared constants.
func (op *TerminalOp) UnmarshalText(text []byte) error {
	v := TerminalOp(text)
	if !v.Valid() {
		return fmt.Errorf("terminalop: unknown value %q; must be one of claim, close, reopen", string(text))
	}
	*op = v
	return nil
}
