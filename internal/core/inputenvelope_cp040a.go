package core

// inputenvelope_cp040a.go — CP-040a input-envelope hash computation.
//
// Every persisted cognition-tagged verdict record MUST include an
// input_envelope_hash field computed deterministically over the evaluator's
// resolved input envelope per specs/control-points.md §4.8.CP-040a.
//
// This file owns the canonical five-input envelope type and the single
// hash-computation function. All cognition-tagged evaluator paths (Gate via
// cp011_gate_cognition_s01.go, Hook via hooksystem) MUST use
// ComputeInputEnvelopeHash to produce the hash co-stored with the verdict.
//
// Tags: mechanism
// Bead: hk-a8bg.42

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

// ContextSubsetMode declares which of the two CP-040a context-subset modes a
// ControlPoint applies. Mixing is forbidden within a single ControlPoint.
//
// Implementations MUST declare one mode; the mode is recorded alongside the
// InputEnvelope so that auditors can verify whether the hash surface is full or
// reduced.
type ContextSubsetMode string

const (
	// ContextSubsetModeASTWalk means the context_subset field carries only the
	// keys reachable from the expression or prompt template via a static AST
	// walk. A change to an unreachable key does NOT bust the hash. This is the
	// preferred mode when the templating grammar is AST-walkable.
	ContextSubsetModeASTWalk ContextSubsetMode = "ast_walk"

	// ContextSubsetModeConservative means the context_subset field carries the
	// whole run.Context map (conservative fallback for non-AST-walkable
	// templating). Any change to any key in run.Context busts the hash and
	// triggers Cat 6 escalation per CP-041. Implementations MUST document which
	// ControlPoints apply this mode.
	ContextSubsetModeConservative ContextSubsetMode = "conservative"
)

// Valid reports whether m is a recognised ContextSubsetMode value.
func (m ContextSubsetMode) Valid() bool {
	return m == ContextSubsetModeASTWalk || m == ContextSubsetModeConservative
}

// InputEnvelope is the canonical five-input record hashed to produce the
// input_envelope_hash stored in a cognition-tagged verdict per
// specs/control-points.md §4.8.CP-040a.
//
// Field order is fixed per spec; ComputeInputEnvelopeHash marshals this struct
// directly to JSON, which preserves Go struct field declaration order in the
// output. Callers MUST NOT reorder the fields.
//
// # ExpressionText
//
// Nil for pure-cognition evaluators that have no mechanism-tagged expression
// body. Non-nil when the ControlPoint has an expression (mechanism-tagged
// expression text is item 1 of the five envelope inputs).
//
// # PromptTemplate
//
// The resolved prompt template body at the prompt_template_ref version pinned
// on the ControlPoint record — NOT the template ref name itself. An update to
// the template content that leaves the ref name unchanged MUST result in a
// different resolved body, a different hash, and (if a verdict already exists)
// a Cat 6 escalation per CP-041. Passing the ref name instead of the resolved
// body is a correctness bug.
//
// # SkillPackages
//
// The set of skill-package identifiers and versions snapshotted from the
// originating handler launch's skills_provisioned event per CP-050. Each
// element is formatted as "name@version" (or just "name" when no version is
// declared). ComputeInputEnvelopeHash sorts this slice before hashing to
// guarantee determinism regardless of insertion order. Callers MUST source
// these from the skills_provisioned event payload, NOT from the current
// on-disk skill-package versions.
//
// # ContextSubset
//
// The subset of run.Context reachable from the expression or prompt template
// via static AST walk (ContextSubsetModeASTWalk), or the whole run.Context map
// as the conservative fallback (ContextSubsetModeConservative). Callers MUST
// set ContextSubsetMode to declare which mode applies; mixing is forbidden per
// CP-040a.
//
// # ContextSubsetMode
//
// Declares which of the two modes was used to produce ContextSubset. Required;
// ComputeInputEnvelopeHash returns an error when Mode is not valid. The mode is
// included in the canonical JSON so that an auditor reading a stored verdict
// can verify whether the hash surface was AST-walked or conservative — per the
// spec requirement that "implementations MUST declare which mode they apply."
//
// # PolicyMeta
//
// The metadata block of the policy document that declared the ControlPoint, at
// its registered schema_version. The full block — not just the schema_version
// integer — is included so that changes to any metadata field (name, author,
// version) bust the hash.
type InputEnvelope struct {
	// ExpressionText is the mechanism-tagged expression source text.
	// Nil for pure-cognition evaluators. Item 1 of 5.
	ExpressionText *string `json:"expression_text"`

	// PromptTemplate is the resolved prompt template body (not the ref).
	// Required (non-empty for cognition-tagged evaluators). Item 2 of 5.
	PromptTemplate string `json:"prompt_template"`

	// SkillPackages is the sorted list of "name@version" (or "name") strings
	// snapshotted from the skills_provisioned event. Item 3 of 5.
	SkillPackages []string `json:"skill_packages"`

	// ContextSubset is the context keys reachable via AST walk, or the whole
	// run.Context map under the conservative fallback. Item 4 of 5.
	ContextSubset map[string]any `json:"context_subset"`

	// PolicyMeta is the metadata block of the declaring policy document at its
	// registered schema_version. Item 5 of 5.
	PolicyMeta map[string]any `json:"policy_meta"`

	// ContextSubsetMode declares which subset mode was applied to produce
	// ContextSubset. Required; ComputeInputEnvelopeHash returns an error if
	// this field is not a valid ContextSubsetMode value. Included in the
	// canonical JSON so the mode is machine-readable in the stored envelope.
	ContextSubsetMode ContextSubsetMode `json:"context_subset_mode"`
}

// ComputeInputEnvelopeHash computes the SHA-256 hex digest of the canonical
// JSON serialization of e per specs/control-points.md §4.8.CP-040a.
//
// The SkillPackages slice is sorted (a copy is made; the caller's slice is not
// mutated) before marshaling to guarantee a deterministic result regardless of
// the order in which skills were provisioned.
//
// ContextSubsetMode must be a valid ContextSubsetMode value; an error is
// returned when it is not. The mode is included in the canonical JSON so the
// auditor can verify the hash surface from the stored envelope.
//
// The returned hash is a 64-character lowercase hex string.
//
// Tags: mechanism
func ComputeInputEnvelopeHash(e InputEnvelope) (string, error) {
	if !e.ContextSubsetMode.Valid() {
		return "", fmt.Errorf("ComputeInputEnvelopeHash: invalid ContextSubsetMode %q", e.ContextSubsetMode)
	}

	// Sort a copy of SkillPackages so the hash is order-independent.
	sorted := make([]string, len(e.SkillPackages))
	copy(sorted, e.SkillPackages)
	sort.Strings(sorted)

	canonical := struct {
		ExpressionText    *string           `json:"expression_text"`
		PromptTemplate    string            `json:"prompt_template"`
		SkillPackages     []string          `json:"skill_packages"`
		ContextSubset     map[string]any    `json:"context_subset"`
		PolicyMeta        map[string]any    `json:"policy_meta"`
		ContextSubsetMode ContextSubsetMode `json:"context_subset_mode"`
	}{
		ExpressionText:    e.ExpressionText,
		PromptTemplate:    e.PromptTemplate,
		SkillPackages:     sorted,
		ContextSubset:     e.ContextSubset,
		PolicyMeta:        e.PolicyMeta,
		ContextSubsetMode: e.ContextSubsetMode,
	}

	data, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("ComputeInputEnvelopeHash: marshal: %w", err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum), nil
}
