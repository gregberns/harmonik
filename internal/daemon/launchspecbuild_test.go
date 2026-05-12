package daemon_test

// launchspecbuild_test.go — tests for review-loop LaunchSpec builders (T-WM-019).
//
// Verifies that each of the three builder functions produces a LaunchSpec with
// the correct field shape per specs/handler-contract.md §6.1 HC-006 and
// specs/execution-model.md §4.3 EM-015d.
//
// Helper prefix: launchspecBuildFixture (bead hk-7om2q.19).

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// launchspecBuildFixtureBase returns a valid handlercontract.LaunchSpec with
// all required fields populated. Optional review-loop fields are nil.
// This is the "base" spec that the builders layer review-loop fields onto.
func launchspecBuildFixtureBase(t *testing.T) handlercontract.LaunchSpec {
	t.Helper()
	runID := core.RunID(uuid.MustParse("0196f000-0000-7000-8000-000000000019"))
	wfID := core.WorkflowID(uuid.MustParse("0196f000-0000-7000-8000-000000000020"))
	return handlercontract.LaunchSpec{
		RunID:               runID,
		WorkflowID:          wfID,
		NodeID:              core.NodeID("impl-node-wm019"),
		AgentType:           core.AgentType("claude-code"),
		WorkspacePath:       "/tmp/harmonik-test/wm019/worktrees/run-019",
		RequiredSkills:      []string{"beads-cli"},
		SkillSearchPaths:    []string{"/usr/local/share/harmonik/skills"},
		Timeout:             3600,
		ProvisioningTimeout: 60,
		Budget:              core.BudgetRef("default"),
		FreedomProfileRef:   "standard",
		SchemaVersion:       handlercontract.LaunchSpecSchemaVersion,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Table-driven tests: three call sites produce correct HC-006 field shape
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildLaunchSpec_ImplementerInitial verifies that
// BuildLaunchSpecImplementerInitial produces a LaunchSpec with:
//   - WorkflowMode = "review-loop"
//   - Phase = implementer-initial
//   - IterationCount = supplied value
//   - ClaudeSessionID = nil (no prior session on first launch)
func TestBuildLaunchSpec_ImplementerInitial(t *testing.T) {
	t.Parallel()

	type tc struct {
		name           string
		iterationCount int
		wantErr        bool
	}
	cases := []tc{
		{name: "iter1", iterationCount: 1, wantErr: false},
		{name: "iter2", iterationCount: 2, wantErr: false},
		{name: "iter3", iterationCount: 3, wantErr: false},
		{name: "zero_iter", iterationCount: 0, wantErr: true},
		{name: "negative_iter", iterationCount: -1, wantErr: true},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			base := launchspecBuildFixtureBase(t)
			spec, err := daemon.ExportedBuildLaunchSpecImplementerInitial(base, c.iterationCount)
			if c.wantErr {
				if err == nil {
					t.Errorf("T-WM-019: BuildLaunchSpecImplementerInitial(%d) = nil err; want error", c.iterationCount)
				}
				return
			}
			if err != nil {
				t.Fatalf("T-WM-019: BuildLaunchSpecImplementerInitial(%d) = %v; want nil", c.iterationCount, err)
			}

			// HC-006 field shape assertions.
			if spec.WorkflowMode == nil || *spec.WorkflowMode != "review-loop" {
				t.Errorf("T-WM-019: WorkflowMode = %v; want &\"review-loop\"", spec.WorkflowMode)
			}
			if spec.Phase == nil || *spec.Phase != handlercontract.ReviewLoopPhaseImplementerInitial {
				t.Errorf("T-WM-019: Phase = %v; want &implementer-initial", spec.Phase)
			}
			if spec.IterationCount == nil || *spec.IterationCount != c.iterationCount {
				t.Errorf("T-WM-019: IterationCount = %v; want &%d", spec.IterationCount, c.iterationCount)
			}
			if spec.ClaudeSessionID != nil {
				t.Errorf("T-WM-019: ClaudeSessionID = %v; want nil (no prior session on initial launch)", spec.ClaudeSessionID)
			}

			// Validate the assembled spec is well-formed per HC-006.
			if err := spec.Valid(); err != nil {
				t.Errorf("T-WM-019: assembled LaunchSpec.Valid() = %v; want nil", err)
			}
		})
	}
}

// TestBuildLaunchSpec_ImplementerResume verifies that
// BuildLaunchSpecImplementerResume produces a LaunchSpec with:
//   - WorkflowMode = "review-loop"
//   - Phase = implementer-resume
//   - IterationCount = supplied value
//   - ClaudeSessionID = supplied session ID (required for resume)
func TestBuildLaunchSpec_ImplementerResume(t *testing.T) {
	t.Parallel()

	type tc struct {
		name            string
		iterationCount  int
		claudeSessionID string
		wantErr         bool
	}
	cases := []tc{
		{name: "iter2_session", iterationCount: 2, claudeSessionID: "claude-session-abc123", wantErr: false},
		{name: "iter3_session", iterationCount: 3, claudeSessionID: "claude-session-xyz789", wantErr: false},
		{name: "zero_iter", iterationCount: 0, claudeSessionID: "claude-session-abc123", wantErr: true},
		{name: "empty_session", iterationCount: 2, claudeSessionID: "", wantErr: true},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			base := launchspecBuildFixtureBase(t)
			spec, err := daemon.ExportedBuildLaunchSpecImplementerResume(base, c.iterationCount, c.claudeSessionID)
			if c.wantErr {
				if err == nil {
					t.Errorf("T-WM-019: BuildLaunchSpecImplementerResume(%d,%q) = nil err; want error",
						c.iterationCount, c.claudeSessionID)
				}
				return
			}
			if err != nil {
				t.Fatalf("T-WM-019: BuildLaunchSpecImplementerResume(%d,%q) = %v; want nil",
					c.iterationCount, c.claudeSessionID, err)
			}

			// HC-006 field shape assertions.
			if spec.WorkflowMode == nil || *spec.WorkflowMode != "review-loop" {
				t.Errorf("T-WM-019: WorkflowMode = %v; want &\"review-loop\"", spec.WorkflowMode)
			}
			if spec.Phase == nil || *spec.Phase != handlercontract.ReviewLoopPhaseImplementerResume {
				t.Errorf("T-WM-019: Phase = %v; want &implementer-resume", spec.Phase)
			}
			if spec.IterationCount == nil || *spec.IterationCount != c.iterationCount {
				t.Errorf("T-WM-019: IterationCount = %v; want &%d", spec.IterationCount, c.iterationCount)
			}
			if spec.ClaudeSessionID == nil || *spec.ClaudeSessionID != c.claudeSessionID {
				t.Errorf("T-WM-019: ClaudeSessionID = %v; want &%q", spec.ClaudeSessionID, c.claudeSessionID)
			}

			// Validate the assembled spec is well-formed per HC-006.
			if err := spec.Valid(); err != nil {
				t.Errorf("T-WM-019: assembled LaunchSpec.Valid() = %v; want nil", err)
			}
		})
	}
}

// TestBuildLaunchSpec_Reviewer verifies that BuildLaunchSpecReviewer produces
// a LaunchSpec with:
//   - WorkflowMode = "review-loop"
//   - Phase = reviewer
//   - IterationCount = supplied value
//   - ClaudeSessionID = nil (each reviewer launch is a fresh Claude session)
func TestBuildLaunchSpec_Reviewer(t *testing.T) {
	t.Parallel()

	type tc struct {
		name           string
		iterationCount int
		wantErr        bool
	}
	cases := []tc{
		{name: "iter1", iterationCount: 1, wantErr: false},
		{name: "iter2", iterationCount: 2, wantErr: false},
		{name: "iter3", iterationCount: 3, wantErr: false},
		{name: "zero_iter", iterationCount: 0, wantErr: true},
		{name: "negative_iter", iterationCount: -1, wantErr: true},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			base := launchspecBuildFixtureBase(t)
			spec, err := daemon.ExportedBuildLaunchSpecReviewer(base, c.iterationCount)
			if c.wantErr {
				if err == nil {
					t.Errorf("T-WM-019: BuildLaunchSpecReviewer(%d) = nil err; want error", c.iterationCount)
				}
				return
			}
			if err != nil {
				t.Fatalf("T-WM-019: BuildLaunchSpecReviewer(%d) = %v; want nil", c.iterationCount, err)
			}

			// HC-006 field shape assertions.
			if spec.WorkflowMode == nil || *spec.WorkflowMode != "review-loop" {
				t.Errorf("T-WM-019: WorkflowMode = %v; want &\"review-loop\"", spec.WorkflowMode)
			}
			if spec.Phase == nil || *spec.Phase != handlercontract.ReviewLoopPhaseReviewer {
				t.Errorf("T-WM-019: Phase = %v; want &reviewer", spec.Phase)
			}
			if spec.IterationCount == nil || *spec.IterationCount != c.iterationCount {
				t.Errorf("T-WM-019: IterationCount = %v; want &%d", spec.IterationCount, c.iterationCount)
			}
			if spec.ClaudeSessionID != nil {
				t.Errorf("T-WM-019: ClaudeSessionID = %v; want nil (fresh session per reviewer phase)", spec.ClaudeSessionID)
			}

			// Validate the assembled spec is well-formed per HC-006.
			if err := spec.Valid(); err != nil {
				t.Errorf("T-WM-019: assembled LaunchSpec.Valid() = %v; want nil", err)
			}
		})
	}
}

// TestBuildLaunchSpec_BaseFieldsPreserved verifies that the builders do not
// mutate required base-spec fields (RunID, WorkflowID, NodeID, etc.) — only
// the four review-loop optional fields are layered on.
func TestBuildLaunchSpec_BaseFieldsPreserved(t *testing.T) {
	t.Parallel()

	base := launchspecBuildFixtureBase(t)

	// Build all three variants and verify required fields are unchanged.
	specs := make([]handlercontract.LaunchSpec, 0, 3)

	s1, err := daemon.ExportedBuildLaunchSpecImplementerInitial(base, 1)
	if err != nil {
		t.Fatalf("T-WM-019: ImplementerInitial: %v", err)
	}
	specs = append(specs, s1)

	s2, err := daemon.ExportedBuildLaunchSpecImplementerResume(base, 2, "claude-session-001")
	if err != nil {
		t.Fatalf("T-WM-019: ImplementerResume: %v", err)
	}
	specs = append(specs, s2)

	s3, err := daemon.ExportedBuildLaunchSpecReviewer(base, 1)
	if err != nil {
		t.Fatalf("T-WM-019: Reviewer: %v", err)
	}
	specs = append(specs, s3)

	for i, s := range specs {
		if s.RunID != base.RunID {
			t.Errorf("T-WM-019: spec[%d].RunID mutated; got %v, want %v", i, s.RunID, base.RunID)
		}
		if s.WorkflowID != base.WorkflowID {
			t.Errorf("T-WM-019: spec[%d].WorkflowID mutated; got %v, want %v", i, s.WorkflowID, base.WorkflowID)
		}
		if s.NodeID != base.NodeID {
			t.Errorf("T-WM-019: spec[%d].NodeID mutated; got %v, want %v", i, s.NodeID, base.NodeID)
		}
		if s.AgentType != base.AgentType {
			t.Errorf("T-WM-019: spec[%d].AgentType mutated; got %v, want %v", i, s.AgentType, base.AgentType)
		}
		if s.WorkspacePath != base.WorkspacePath {
			t.Errorf("T-WM-019: spec[%d].WorkspacePath mutated; got %v, want %v", i, s.WorkspacePath, base.WorkspacePath)
		}
		if s.Timeout != base.Timeout {
			t.Errorf("T-WM-019: spec[%d].Timeout mutated; got %d, want %d", i, s.Timeout, base.Timeout)
		}
		if s.SchemaVersion != base.SchemaVersion {
			t.Errorf("T-WM-019: spec[%d].SchemaVersion mutated; got %d, want %d", i, s.SchemaVersion, base.SchemaVersion)
		}
	}

	// Confirm base was not mutated by the builders.
	if base.WorkflowMode != nil {
		t.Error("T-WM-019: base.WorkflowMode was mutated by a builder; want nil")
	}
	if base.Phase != nil {
		t.Error("T-WM-019: base.Phase was mutated by a builder; want nil")
	}
	if base.ClaudeSessionID != nil {
		t.Error("T-WM-019: base.ClaudeSessionID was mutated by a builder; want nil")
	}
}
