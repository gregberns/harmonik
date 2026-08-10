package brcli

import (
	"errors"
	"fmt"
	"strings"
)

// ErrClaimDependencyBlocked reports that br refused a claim because an open
// dependency prevents the bead from starting.
var ErrClaimDependencyBlocked = errors.New("brcli: claim refused by dependency")

// ErrClaimAlreadyAssigned reports that br refused a claim because the bead
// already has an assignee.
var ErrClaimAlreadyAssigned = errors.New("brcli: claim refused by assignee")

// TerminalWriteError retains the structured br result for a refused terminal
// transition. Callers can inspect it with errors.As without parsing Error().
type TerminalWriteError struct {
	Op     string
	Result Result
}

func (e *TerminalWriteError) Error() string {
	return fmt.Sprintf("brcli: br %s failed: %s (exit %d): stderr=%q",
		e.Op, e.Result.BrErr, e.Result.ExitCode, e.Result.Stderr)
}

// Unwrap keeps the adapter error taxonomy available through errors.Is.
func (e *TerminalWriteError) Unwrap() error { return e.Result.BrErr }

// classifyClaimRefusal parses br presentation text at the external boundary.
// Code above ClaimBead receives typed errors and does not depend on this text.
func classifyClaimRefusal(err error) error {
	var terminalErr *TerminalWriteError
	if !errors.As(err, &terminalErr) {
		return err
	}
	stderr := string(terminalErr.Result.Stderr)
	switch {
	case strings.Contains(stderr, "already assigned"):
		return fmt.Errorf("%w: %w", ErrClaimAlreadyAssigned, err)
	case strings.Contains(stderr, "cannot claim blocked issue"):
		return fmt.Errorf("%w: %w", ErrClaimDependencyBlocked, err)
	default:
		return err
	}
}
