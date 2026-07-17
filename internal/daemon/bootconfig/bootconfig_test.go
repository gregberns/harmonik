package bootconfig_test

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon/bootconfig"
)

func TestValidateWorkflowMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mode    core.WorkflowMode
		wantErr string // substring; "" means no error
	}{
		{"empty", core.WorkflowMode(""), "WorkflowModeDefault must be set"},
		{"invalid", core.WorkflowMode("bogus"), "invalid workflow_mode_default"},
		{"single", core.WorkflowModeSingle, ""},
		{"review-loop", core.WorkflowModeReviewLoop, ""},
		{"dot", core.WorkflowModeDot, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := bootconfig.ValidateWorkflowMode(tc.mode)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateWorkflowMode(%q) = %v; want nil", tc.mode, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateWorkflowMode(%q) = %v; want error containing %q", tc.mode, err, tc.wantErr)
			}
		})
	}
}

func TestResolveTargetBranch(t *testing.T) {
	t.Parallel()
	if got := bootconfig.ResolveTargetBranch(""); got != "main" {
		t.Errorf(`ResolveTargetBranch("") = %q; want "main"`, got)
	}
	if got := bootconfig.ResolveTargetBranch("release"); got != "release" {
		t.Errorf(`ResolveTargetBranch("release") = %q; want "release"`, got)
	}
}

func TestMergeBranchingDefaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		in          bootconfig.Input
		wantTarget  string // merged-but-unresolved
		wantProtect []string
	}{
		{
			name:        "flag_set_wins_over_yaml",
			in:          bootconfig.Input{FlagTargetBranch: "flagbr", YAMLLandsOn: "yamlbr"},
			wantTarget:  "flagbr",
			wantProtect: nil,
		},
		{
			name:        "yaml_fills_when_flag_empty",
			in:          bootconfig.Input{FlagTargetBranch: "", YAMLLandsOn: "yamlbr"},
			wantTarget:  "yamlbr",
			wantProtect: nil,
		},
		{
			name:        "both_empty_target",
			in:          bootconfig.Input{FlagTargetBranch: "", YAMLLandsOn: ""},
			wantTarget:  "",
			wantProtect: nil,
		},
		{
			name:        "flag_protect_wins",
			in:          bootconfig.Input{FlagProtectBranches: []string{"main"}, YAMLProtectBranches: []string{"prod"}},
			wantTarget:  "",
			wantProtect: []string{"main"},
		},
		{
			name:        "yaml_protect_fills_when_flag_empty",
			in:          bootconfig.Input{FlagProtectBranches: nil, YAMLProtectBranches: []string{"prod", "release"}},
			wantTarget:  "",
			wantProtect: []string{"prod", "release"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := bootconfig.MergeBranchingDefaults(tc.in)
			if got.TargetBranch != tc.wantTarget {
				t.Errorf("TargetBranch = %q; want %q", got.TargetBranch, tc.wantTarget)
			}
			if !equalStrings(got.ProtectBranches, tc.wantProtect) {
				t.Errorf("ProtectBranches = %v; want %v", got.ProtectBranches, tc.wantProtect)
			}
		})
	}
}

func TestValidateBranchProtection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		forbid       bool
		mergedTarget string
		protect      []string
		wantErr      string
	}{
		{"forbid_empty_target", true, "", nil, "--forbid-default-main"},
		{"forbid_with_target_ok", true, "release", nil, ""},
		{"explicit_target_in_protect", false, "main", []string{"main"}, "ProtectBranches"},
		{"default_target_in_protect", false, "", []string{"main"}, "ProtectBranches"},
		{"target_not_protected_ok", false, "release", []string{"main"}, ""},
		// seam-3 regression: flag empty, YAML supplies lands_on (merged=release),
		// forbid=true — must NOT error (mergedTarget is non-empty).
		{"forbid_yaml_supplied_target_ok", true, "release", nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := bootconfig.ValidateBranchProtection(tc.forbid, tc.mergedTarget, tc.protect)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateBranchProtection = %v; want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateBranchProtection = %v; want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		in          bootconfig.Input
		wantErr     string
		wantTarget  string // post-resolve ("" → "main")
		wantProtect []string
	}{
		{
			name:    "empty_mode_errors_first",
			in:      bootconfig.Input{WorkflowMode: ""},
			wantErr: "WorkflowModeDefault must be set",
		},
		{
			name:       "happy_default_target",
			in:         bootconfig.Input{WorkflowMode: core.WorkflowModeDot},
			wantTarget: "main",
		},
		{
			name:       "yaml_lands_on_resolved",
			in:         bootconfig.Input{WorkflowMode: core.WorkflowModeDot, YAMLLandsOn: "release"},
			wantTarget: "release",
		},
		{
			name:    "forbid_default_main_empty_target",
			in:      bootconfig.Input{WorkflowMode: core.WorkflowModeDot, ForbidUnprotectedDefault: true},
			wantErr: "--forbid-default-main",
		},
		{
			name:    "target_in_protect",
			in:      bootconfig.Input{WorkflowMode: core.WorkflowModeDot, FlagTargetBranch: "main", FlagProtectBranches: []string{"main"}},
			wantErr: "ProtectBranches",
		},
		{
			name:        "flag_target_and_protect_ok",
			in:          bootconfig.Input{WorkflowMode: core.WorkflowModeReviewLoop, FlagTargetBranch: "release", FlagProtectBranches: []string{"main"}, ForbidUnprotectedDefault: true},
			wantTarget:  "release",
			wantProtect: []string{"main"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := bootconfig.Resolve(tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Resolve = (%+v, %v); want error containing %q", got, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve = %v; want nil error", err)
			}
			if got.TargetBranch != tc.wantTarget {
				t.Errorf("TargetBranch = %q; want %q", got.TargetBranch, tc.wantTarget)
			}
			if !equalStrings(got.ProtectBranches, tc.wantProtect) {
				t.Errorf("ProtectBranches = %v; want %v", got.ProtectBranches, tc.wantProtect)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
