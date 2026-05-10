package operatornfr

// BinarySigningPolicy captures the ON-006 post-MVH deferral of full
// cryptographic binary signing.
//
// MVH conformance requires ONLY the commit-hash integrity gate of ON-005.
// Post-MVH introduction of code-signing certs, reproducible-build attestations,
// and signature-chain verification is additive and does NOT invalidate MVH
// conformance.  Callers that would gate on a signing check MUST instead gate
// on CommitHashIntegrityCheck during the MVH era.
//
// Spec ref: operator-nfr.md §4.2 ON-006 — "Conforming MVH implementations
// MUST NOT be required to verify signatures beyond the commit-hash match of
// §4.2.ON-005. Post-MVH introduction of signing is additive and does NOT
// invalidate MVH conformance."
type BinarySigningPolicy struct {
	// SigningRequired is false for the MVH era: no signature verification beyond
	// the commit-hash gate is required per ON-006.
	SigningRequired bool

	// MVHEra indicates whether this policy record applies to the MVH era.
	// Non-MVH policies (post-MVH builds) may set SigningRequired = true.
	MVHEra bool
}

// MVHBinarySigningPolicy is the canonical MVH-era BinarySigningPolicy per
// ON-006.  It declares that binary signing is NOT required for MVH conformance.
//
// Spec ref: operator-nfr.md §4.2 ON-006.
var MVHBinarySigningPolicy = BinarySigningPolicy{
	SigningRequired: false,
	MVHEra:          true,
}

// PromptInjectionOwner identifies who owns prompt-injection defense per ON-026.
//
// Spec ref: operator-nfr.md §4.7 ON-026 — "Prompt-injection defense is
// handler-owned."
type PromptInjectionOwner string

const (
	// PromptInjectionOwnerHandler declares that the handler subprocess is
	// responsible for sanitizing user-provided content in the input workspace
	// so it cannot alter the agent's system-prompt instructions.
	//
	// The daemon and orchestrator MUST NOT re-sanitize handler inputs; the
	// obligation is exclusively the handler's per [handler-contract.md §4.1].
	//
	// Spec ref: operator-nfr.md §4.7 ON-026 — "Input sanitization for
	// user-provided content in the input workspace MUST be the handler's
	// responsibility per [handler-contract.md §4.1]."
	PromptInjectionOwnerHandler PromptInjectionOwner = "handler"
)

// Valid reports whether o is a declared PromptInjectionOwner constant.
func (o PromptInjectionOwner) Valid() bool {
	return o == PromptInjectionOwnerHandler
}
