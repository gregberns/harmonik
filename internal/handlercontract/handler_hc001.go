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
	// HC-004 idempotency — NOT ENFORCED (A9 reconciliation, 2026-07-17).
	// The historical contract asserted Launch MUST be idempotent on a
	// (run_id, node_id[, phase, iteration_count]) key and return an existing
	// Session on a second call, to prevent duplicate work on daemon restart.
	// The shipping handler.Launch (internal/handler/handler.go) mints a fresh
	// session id via handlercontract.NewSessionID and spawns the subprocess
	// UNCONDITIONALLY — there is no dedup guard, at either the handler or the
	// daemon-dispatch level, keyed on (run_id, node_id). A daemon restart that
	// re-dispatches the same (run_id, node_id) therefore spawns a SECOND
	// subprocess (duplicate work / double budget). This is a real latent
	// defect. The former LaunchKey helper and its self-referential idempotency
	// tests were deleted rather than left asserting an unenforced MUST; wiring
	// a real launch-dedup registry into handler.Launch is a scoped
	// daemon/handler-lane follow-up, out of scope for this contract pass.
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
