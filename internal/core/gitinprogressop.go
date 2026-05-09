package core

import "fmt"

// GitInProgressOp describes a Git operation currently in progress within a
// harmonik-managed worktree, as defined in specs/reconciliation/schemas.md §6.1
// ENUM GitInProgressOp.
//
// The five values are:
//
//	none, rebase, merge, cherry-pick, bisect
//
// Any non-none value is a Cat 6a trigger per specs/reconciliation/schemas.md
// §8.11: the reconciliation investigator must escalate to human when it detects
// an uncommitted git-in-progress operation of these kinds.
//
// The enum is harmonik-owned and closed: unknown values are never tolerated.
type GitInProgressOp string

// GitInProgressOp constants per specs/reconciliation/schemas.md §6.1.
const (
	// GitInProgressOpNone means no Git operation is currently in progress.
	// This is the expected steady-state value.
	GitInProgressOpNone GitInProgressOp = "none"

	// GitInProgressOpRebase means a rebase is currently in progress.
	// Non-none: Cat 6a trigger per specs/reconciliation/schemas.md §8.11.
	GitInProgressOpRebase GitInProgressOp = "rebase"

	// GitInProgressOpMerge means a merge is currently in progress.
	// Non-none: Cat 6a trigger per specs/reconciliation/schemas.md §8.11.
	GitInProgressOpMerge GitInProgressOp = "merge"

	// GitInProgressOpCherryPick means a cherry-pick is currently in progress.
	// Non-none: Cat 6a trigger per specs/reconciliation/schemas.md §8.11.
	GitInProgressOpCherryPick GitInProgressOp = "cherry-pick"

	// GitInProgressOpBisect means a bisect is currently in progress.
	// Non-none: Cat 6a trigger per specs/reconciliation/schemas.md §8.11.
	GitInProgressOpBisect GitInProgressOp = "bisect"
)

// Valid reports whether op is one of the five declared GitInProgressOp constants.
// The enum is harmonik-owned and closed; unknown values are never valid.
func (op GitInProgressOp) Valid() bool {
	switch op {
	case GitInProgressOpNone, GitInProgressOpRebase, GitInProgressOpMerge,
		GitInProgressOpCherryPick, GitInProgressOpBisect:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so GitInProgressOp serialises
// correctly in JSON and YAML.
func (op GitInProgressOp) MarshalText() ([]byte, error) {
	if !op.Valid() {
		return nil, fmt.Errorf("gitinprogressop: unknown value %q", string(op))
	}
	return []byte(op), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the five declared constants.
func (op *GitInProgressOp) UnmarshalText(text []byte) error {
	v := GitInProgressOp(text)
	if !v.Valid() {
		return fmt.Errorf(
			"gitinprogressop: unknown value %q; must be one of none, rebase, merge, cherry-pick, bisect",
			string(text),
		)
	}
	*op = v
	return nil
}
