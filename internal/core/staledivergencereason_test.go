package core

import (
	"encoding/json"
	"testing"
)

func TestStaleDivergenceReasonValid(t *testing.T) {
	t.Parallel()

	valid := []StaleDivergenceReason{
		StaleDivergenceReasonGitBranchAdvanced,
		StaleDivergenceReasonBeadsAuditAdvanced,
	}
	for _, r := range valid {
		if !r.Valid() {
			t.Errorf("expected %q to be valid", r)
		}
	}

	invalid := []StaleDivergenceReason{
		"",
		"Git-Branch-Advanced",
		"GIT-BRANCH-ADVANCED",
		"gitBranchAdvanced",
		"git_branch_advanced",
		"Beads-Audit-Advanced",
		"BEADS-AUDIT-ADVANCED",
		"beadsAuditAdvanced",
		"beads_audit_advanced",
		"unknown",
		"git-branch-advanced|beads-audit-advanced",
	}
	for _, r := range invalid {
		if r.Valid() {
			t.Errorf("expected %q to be invalid", r)
		}
	}
}

func TestStaleDivergenceReasonMarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reason StaleDivergenceReason
		want   string
	}{
		{StaleDivergenceReasonGitBranchAdvanced, "git-branch-advanced"},
		{StaleDivergenceReasonBeadsAuditAdvanced, "beads-audit-advanced"},
	}
	for _, tc := range tests {
		got, err := tc.reason.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%q) error: %v", tc.reason, err)
		}
		if string(got) != tc.want {
			t.Errorf("MarshalText(%q) = %q, want %q", tc.reason, string(got), tc.want)
		}
	}

	if _, err := StaleDivergenceReason("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}

	if _, err := StaleDivergenceReason("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

func TestStaleDivergenceReasonUnmarshalText(t *testing.T) {
	t.Parallel()

	type staleDivergenceReasonFixtureWrapper struct {
		Reason StaleDivergenceReason `json:"reason"`
	}

	tests := []struct {
		name    string
		input   string
		want    StaleDivergenceReason
		wantErr bool
	}{
		{
			name:  "git-branch-advanced",
			input: `{"reason":"git-branch-advanced"}`,
			want:  StaleDivergenceReasonGitBranchAdvanced,
		},
		{
			name:  "beads-audit-advanced",
			input: `{"reason":"beads-audit-advanced"}`,
			want:  StaleDivergenceReasonBeadsAuditAdvanced,
		},
		{
			name:    "mixed-case Git-Branch-Advanced rejected",
			input:   `{"reason":"Git-Branch-Advanced"}`,
			wantErr: true,
		},
		{
			name:    "uppercase GIT-BRANCH-ADVANCED rejected",
			input:   `{"reason":"GIT-BRANCH-ADVANCED"}`,
			wantErr: true,
		},
		{
			name:    "camelCase gitBranchAdvanced rejected",
			input:   `{"reason":"gitBranchAdvanced"}`,
			wantErr: true,
		},
		{
			name:    "underscore form git_branch_advanced rejected",
			input:   `{"reason":"git_branch_advanced"}`,
			wantErr: true,
		},
		{
			name:    "mixed-case Beads-Audit-Advanced rejected",
			input:   `{"reason":"Beads-Audit-Advanced"}`,
			wantErr: true,
		},
		{
			name:    "uppercase BEADS-AUDIT-ADVANCED rejected",
			input:   `{"reason":"BEADS-AUDIT-ADVANCED"}`,
			wantErr: true,
		},
		{
			name:    "camelCase beadsAuditAdvanced rejected",
			input:   `{"reason":"beadsAuditAdvanced"}`,
			wantErr: true,
		},
		{
			name:    "underscore form beads_audit_advanced rejected",
			input:   `{"reason":"beads_audit_advanced"}`,
			wantErr: true,
		},
		{
			name:    "unknown value rejected",
			input:   `{"reason":"unknown"}`,
			wantErr: true,
		},
		{
			name:    "empty string rejected",
			input:   `{"reason":""}`,
			wantErr: true,
		},
		{
			name:    "partial match git-branch rejected",
			input:   `{"reason":"git-branch"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w staleDivergenceReasonFixtureWrapper
			err := json.Unmarshal([]byte(tc.input), &w)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for input %q: %v", tc.input, err)
				return
			}
			if w.Reason != tc.want {
				t.Errorf("got %q, want %q", string(w.Reason), string(tc.want))
			}
		})
	}
}
