package projectconfig

import (
	"errors"
	"strings"
	"testing"
)

func TestWorkflowModeFloorViolation_ExplainsLegacyNoReviewDOTSelection(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
daemon:
  workflow_mode: single
`)

	_, err := LoadProjectConfig(root)
	if err == nil {
		t.Fatal("LoadProjectConfig: error = nil, want workflow-mode floor violation")
	}
	var floorErr *ErrWorkflowModeFloorViolation
	if !errors.As(err, &floorErr) {
		t.Fatalf("error type = %T (%v), want *ErrWorkflowModeFloorViolation", err, err)
	}
	if !strings.Contains(err.Error(), "workflow:single label selects the no-review DOT graph") {
		t.Fatalf("error = %q, want legacy no-review DOT selection guidance", err)
	}
}
