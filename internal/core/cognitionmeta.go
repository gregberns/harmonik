package core

// CognitionMeta carries audit metadata about a cognition-tagged evaluator that
// produced a Gate or Hook verdict (specs/control-points.md §6.1.6 RECORD
// CognitionMeta).
//
// CognitionMeta is set on GateVerdictRecord and HookVerdictRecord when the
// evaluator that produced the verdict was cognition-tagged (evaluator.mode =
// cognition). It is nil when the verdict was produced by a non-cognition
// evaluator.
//
//	RECORD CognitionMeta:
//	    delegation_path       : DelegationPath  -- snapshot of path used (§6.1.5)
//	    model_response_digest : String          -- hash of raw model output (for audit)
//	    token_usage           : Integer | None  -- tokens consumed producing the verdict
type CognitionMeta struct {
	// DelegationPath is a snapshot of the DelegationPath record used to produce
	// this verdict (specs/control-points.md §6.1.5). Captured at verdict time so
	// the audit record is self-contained even if the registered path is later
	// updated.
	DelegationPath DelegationPath `json:"delegation_path"`

	// ModelResponseDigest is the SHA-256 hex digest of the raw model output
	// returned by the cognition-tagged evaluator. Used for audit and
	// replay-safety checks.
	// Required (non-empty). Must be a 64-character lowercase hex string.
	ModelResponseDigest string `json:"model_response_digest"`

	// TokenUsage is the number of tokens consumed producing the verdict.
	// Nil when the runtime did not report usage (e.g., streamed responses or
	// provider-suppressed counts).
	TokenUsage *int `json:"token_usage,omitempty"`
}

// Valid reports whether m is a structurally well-formed CognitionMeta.
// DelegationPath must be valid, and ModelResponseDigest must be non-empty.
// TokenUsage is optional; when set it must be non-negative.
func (m CognitionMeta) Valid() bool {
	if !m.DelegationPath.Valid() {
		return false
	}
	if m.ModelResponseDigest == "" {
		return false
	}
	if m.TokenUsage != nil && *m.TokenUsage < 0 {
		return false
	}
	return true
}
