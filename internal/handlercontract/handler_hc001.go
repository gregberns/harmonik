package handlercontract

import (
	"context"
	"fmt"
)

// Handler is the Go interface every handler implementation MUST satisfy per
// specs/handler-contract.md §6.1 HC-001.
//
// The interface is the single contract for launching an agent subprocess.
// Real handlers, twin handlers, and any future execution-shape handlers
// (cloud-execution, remote-container) MUST implement the same interface.
// Adding an agent type MUST NOT alter the Handler interface — it adds an
// adapter per §4.3.
//
// Every method MUST take ctx as its first parameter (HC-021). Implementations
// MUST propagate ctx to the subprocess via the wire protocol.
type Handler interface {
	// Launch starts an agent subprocess for the given spec and returns a
	// Session representing it.
	//
	// Launch MUST be idempotent on the key returned by LaunchKey(spec): a
	// second call with the same key MUST return the existing Session without
	// spawning a new subprocess (HC-004). This prevents duplicate work on
	// daemon restart.
	//
	// When spec.Phase and spec.IterationCount are both present (multi-phase
	// modes such as review-loop), the key is the 4-tuple
	// (run_id, node_id, phase, iteration_count). When both fields are absent
	// (workflow_mode = single or omitted), the key is the 2-tuple
	// (run_id, node_id). See LaunchKey for the canonical derivation.
	//
	// On success, the returned Session's lifetime begins at this return and
	// ends when Session.Wait returns. On failure, Launch returns nil and one
	// of the sentinel errors declared in sentinels.go:
	//   - ErrProtocolMismatch — subprocess spoke an unrecognised wire version
	//   - ErrSkillProvisioningFailed — structural skill provisioning failure (wraps ErrStructural)
	//   - ErrStructural — other structural failure (binary not found, hash mismatch, etc.)
	//   - ErrTransient — transient failure (network blip during provisioning, etc.)
	//
	// Spec ref: handler-contract.md §6.1 HC-001; HC-004 (idempotency).
	Launch(ctx context.Context, spec *LaunchSpec) (Session, error)

	// AgentType returns the lowercase-hyphenated agent_type identifier for
	// this handler implementation (e.g., "claude-code", "twin-claude-code").
	//
	// The returned value MUST match the identifier registered in the
	// architecture.md §6.1 conformance-class taxonomy. The value is used
	// to select the handler by agent_type at runtime (HC-003) and to
	// populate SessionMetadataSidecar.AgentType per WM-026.
	//
	// Spec ref: handler-contract.md §6.1 HC-001; architecture.md §6.1.
	AgentType() string
}

// LaunchKey derives the idempotency key for a Launch call from spec per
// specs/handler-contract.md §4.2 HC-004.
//
// Key shape:
//   - 2-tuple "(run_id)/(node_id)" when spec.Phase and spec.IterationCount
//     are both nil (workflow_mode = single, or mode field omitted).
//   - 4-tuple "(run_id)/(node_id)/(phase)/(iteration_count)" when both
//     spec.Phase and spec.IterationCount are non-nil (multi-phase modes such
//     as review-loop). Within a single review-loop cycle, distinct
//     (phase, iteration_count) tuples produce distinct keys, so the daemon
//     MAY issue Launch calls for different phases without the idempotency
//     guard returning an existing Session from a prior phase.
//
// LaunchKey panics if spec is nil. Handlers MUST call LaunchKey after
// validating the spec with spec.Valid(); an invalid spec (mismatched
// Phase/IterationCount co-presence) is a caller bug.
func LaunchKey(spec *LaunchSpec) string {
	if spec == nil {
		panic("handlercontract: LaunchKey called with nil spec")
	}
	if spec.Phase != nil && spec.IterationCount != nil {
		// 4-tuple key for multi-phase modes (e.g., review-loop).
		return fmt.Sprintf("%s/%s/%s/%d",
			spec.RunID.String(),
			string(spec.NodeID),
			string(*spec.Phase),
			*spec.IterationCount,
		)
	}
	// 2-tuple key for single-mode or mode-omitted runs.
	return fmt.Sprintf("%s/%s",
		spec.RunID.String(),
		string(spec.NodeID),
	)
}
