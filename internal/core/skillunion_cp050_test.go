package core_test

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// TestEffectiveSkillSet verifies the CP-050 set-union semantics:
// node required_skills ∪ role default_skills, node-first, no duplicates.
//
// Spec: specs/control-points.md §4.11.CP-050.
// Bead: hk-a8bg.52.
func TestEffectiveSkillSet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		nodeSkills   []string
		roleDefaults []core.SkillName
		wantSkills   []string
	}{
		{
			name:         "both empty",
			nodeSkills:   nil,
			roleDefaults: nil,
			wantSkills:   []string{},
		},
		{
			name:         "node only",
			nodeSkills:   []string{"agent-reviewer", "beads-cli"},
			roleDefaults: nil,
			wantSkills:   []string{"agent-reviewer", "beads-cli"},
		},
		{
			name:         "role only",
			nodeSkills:   nil,
			roleDefaults: []core.SkillName{"beads-cli", "session-resume"},
			wantSkills:   []string{"beads-cli", "session-resume"},
		},
		{
			name:         "disjoint — node skills first",
			nodeSkills:   []string{"agent-reviewer"},
			roleDefaults: []core.SkillName{"beads-cli"},
			wantSkills:   []string{"agent-reviewer", "beads-cli"},
		},
		{
			name:         "overlap — no duplicate",
			nodeSkills:   []string{"beads-cli", "agent-reviewer"},
			roleDefaults: []core.SkillName{"beads-cli", "session-resume"},
			wantSkills:   []string{"beads-cli", "agent-reviewer", "session-resume"},
		},
		{
			name:         "node duplicates collapsed",
			nodeSkills:   []string{"beads-cli", "beads-cli"},
			roleDefaults: []core.SkillName{"beads-cli"},
			wantSkills:   []string{"beads-cli"},
		},
		{
			name:         "role only — preserves role order",
			nodeSkills:   []string{},
			roleDefaults: []core.SkillName{"session-resume", "beads-cli"},
			wantSkills:   []string{"session-resume", "beads-cli"},
		},
		{
			name:         "complete overlap — role adds nothing",
			nodeSkills:   []string{"beads-cli", "session-resume"},
			roleDefaults: []core.SkillName{"beads-cli", "session-resume"},
			wantSkills:   []string{"beads-cli", "session-resume"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := core.EffectiveSkillSet(tc.nodeSkills, tc.roleDefaults)
			if len(got) != len(tc.wantSkills) {
				t.Fatalf("EffectiveSkillSet(%v, %v): got %v (len %d), want %v (len %d)",
					tc.nodeSkills, tc.roleDefaults, got, len(got), tc.wantSkills, len(tc.wantSkills))
			}
			for i, s := range got {
				if s != tc.wantSkills[i] {
					t.Errorf("EffectiveSkillSet index %d: got %q, want %q", i, s, tc.wantSkills[i])
				}
			}
		})
	}
}
