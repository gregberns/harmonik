package handlercontract

import (
	"context"
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
	// Launch MUST be idempotent on (spec.RunID, spec.NodeID): a second call
	// with the same pair MUST return the existing Session without spawning a
	// new subprocess (HC-004). This prevents duplicate work on daemon restart.
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
