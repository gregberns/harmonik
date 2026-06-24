//go:build !linux && !darwin

package scenario

// postsuiteleaksensor_other.go — stub checkLeakedProcesses for platforms
// other than Linux and Darwin.
//
// Process-tree enumeration is not implemented for these platforms; the check
// is silently skipped. Checks (ii) and (iii) (lease and fd) still run and are
// cross-platform.
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-002(i).

import (
	"context"

	"github.com/gregberns/harmonik/internal/core"
)

// checkLeakedProcesses is a no-op on platforms other than Linux and Darwin.
// Returns nil, nil (no leaks detected, no error).
func checkLeakedProcesses(_ context.Context, _ []core.RunID) ([]LeakDescriptor, error) {
	return nil, nil
}
