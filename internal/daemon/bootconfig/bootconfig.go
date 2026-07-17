package bootconfig

import (
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// Input carries the boot-time branching / workflow-mode configuration into the
// pure resolution seam. All fields are plain primitives so this package never
// needs to import the daemon (or branching) packages back: the daemon performs
// the branching.Load I/O and projects the result into YAMLLandsOn /
// YAMLProtectBranches.
type Input struct {
	// WorkflowMode is cfg.WorkflowModeDefault (PL-004a).
	WorkflowMode core.WorkflowMode
	// FlagTargetBranch is cfg.TargetBranch as supplied (flag value, or "" when
	// unset) — the PRE-MERGE target.
	FlagTargetBranch string
	// YAMLLandsOn is branchingDefaults.LandsOn ("" when the file is absent or the
	// key is omitted).
	YAMLLandsOn string
	// FlagProtectBranches is cfg.ProtectBranches as supplied (flag value).
	FlagProtectBranches []string
	// YAMLProtectBranches is branchingDefaults.ProtectBranches.
	YAMLProtectBranches []string
	// ForbidUnprotectedDefault is cfg.ForbidUnprotectedDefault (--forbid-default-main).
	ForbidUnprotectedDefault bool
}

// Resolved is the post-merge, post-default branching configuration the daemon
// writes back onto cfg.
type Resolved struct {
	// TargetBranch is the resolved merge target ("" → "main").
	TargetBranch string
	// ProtectBranches is the merged protect-branch set (flag > YAML).
	ProtectBranches []string
}

// ResolveTargetBranch returns branch when non-empty, otherwise the production
// default "main". This mirrors the convention used by the reconciliation
// scanner ("defaults to 'main' inside the scanner").
func ResolveTargetBranch(branch string) string {
	if branch == "" {
		return "main"
	}
	return branch
}

// ValidateWorkflowMode implements the PL-004a fail-closed workflow-mode check
// (hk-81n9r). The zero value (empty string) is a startup error; any
// unrecognised non-empty value is also rejected so the daemon fails fast rather
// than silently using a wrong mode. The daemon wraps the returned error with a
// "daemon.Start: " prefix, so the messages here match the historical text once
// wrapped.
func ValidateWorkflowMode(mode core.WorkflowMode) error {
	if mode == "" {
		return fmt.Errorf("WorkflowModeDefault must be set (PL-004a); set cfg.WorkflowModeDefault = core.WorkflowModeDot for the standard dot default")
	}
	if !mode.Valid() {
		return fmt.Errorf("invalid workflow_mode_default %q: must be one of single, review-loop, dot (PL-004a)", mode)
	}
	return nil
}

// MergeBranchingDefaults applies the WM-005b flag > YAML > built-in precedence
// (hk-zl4sl): a field is filled from the YAML value ONLY when the flag value is
// at its zero value (empty string / nil-or-empty slice); a flag-supplied value
// is never overwritten. The returned TargetBranch is the merged-but-UNRESOLVED
// target (may still be ""); ResolveTargetBranch applies the "" → "main" default.
func MergeBranchingDefaults(in Input) Resolved {
	target := in.FlagTargetBranch
	if target == "" && in.YAMLLandsOn != "" {
		target = in.YAMLLandsOn
	}
	protect := in.FlagProtectBranches
	if len(protect) == 0 && len(in.YAMLProtectBranches) > 0 {
		protect = in.YAMLProtectBranches
	}
	return Resolved{TargetBranch: target, ProtectBranches: protect}
}

// ValidateBranchProtection implements the hk-sul12 fail-closed branch-protection
// checks. mergedTarget is the POST-MERGE target (may still be "") — NOT the raw
// flag value; case (1) tests mergedTarget == "" and case (2) tests
// ResolveTargetBranch(mergedTarget) ∈ protect. The daemon wraps the returned
// error with "daemon.Start: ".
//
//	(1) forbidDefault && mergedTarget == "": --forbid-default-main is set but no
//	    target branch was provided; the daemon would silently merge into "main".
//	(2) resolved target is in protect: the daemon would merge into a protected
//	    branch, violating the operator's explicit protection policy.
func ValidateBranchProtection(forbidDefault bool, mergedTarget string, protect []string) error {
	if forbidDefault && mergedTarget == "" {
		return fmt.Errorf("--forbid-default-main is set but --target-branch is empty; provide an explicit --target-branch to proceed (hk-sul12)")
	}
	resolved := ResolveTargetBranch(mergedTarget)
	for _, p := range protect {
		if resolved == p {
			return fmt.Errorf("target branch %q is in ProtectBranches; choose a different --target-branch (hk-sul12)", resolved)
		}
	}
	return nil
}

// Resolve composes the boot-config resolution: it re-validates the workflow mode
// (idempotent — the daemon has already called ValidateWorkflowMode before
// branching.Load, per the seam ordering contract), applies the branching
// precedence merge, runs the hk-sul12 protection check on the POST-MERGE target,
// and returns the resolved config with the target branch defaulted ("" → "main").
func Resolve(in Input) (Resolved, error) {
	if err := ValidateWorkflowMode(in.WorkflowMode); err != nil {
		return Resolved{}, err
	}
	merged := MergeBranchingDefaults(in)
	if err := ValidateBranchProtection(in.ForbidUnprotectedDefault, merged.TargetBranch, merged.ProtectBranches); err != nil {
		return Resolved{}, err
	}
	merged.TargetBranch = ResolveTargetBranch(merged.TargetBranch)
	return merged, nil
}
