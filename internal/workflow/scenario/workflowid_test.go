package scenario_test

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

func mustWorkflowID(t testing.TB, raw string) core.WorkflowID {
	t.Helper()
	id, err := core.NewWorkflowID(raw)
	if err != nil {
		t.Fatalf("new workflow ID %q: %v", raw, err)
	}
	return id
}
