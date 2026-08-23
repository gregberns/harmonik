//go:build darwin

package scenario

import (
	"context"

	"github.com/gregberns/harmonik/internal/core"
)

func checkLeakedProcesses(_ context.Context, _ []core.RunID) ([]LeakDescriptor, error) {
	return nil, nil
}
