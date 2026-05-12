// Package daemon is the composition root for the harmonik daemon process.
//
// This package is the sole package permitted to import across subsystem
// boundaries. It wires all cross-subsystem registries together on startup
// per [specs/process-lifecycle.md §4.6 PL-020] and
// [specs/process-lifecycle.md §4.6 PL-020a].
//
// # Startup protocol
//
// The entry point is [Start]. It executes the startup sequence defined by
// [specs/process-lifecycle.md §4.2 PL-005]:
//
//   - Step 0: bootstrap the composition root — instantiate the event bus,
//     control-point registry, handler registry, skill registry, and policy
//     registry in-process per AR-INV-007 and PL-020a; mint a
//     daemon_instance_id (UUIDv7); write .harmonik/daemon.instance-id.
//
// Subsequent startup steps (pidfile lock, socket bind, orphan sweep,
// reconciliation dispatch, ready transition) are added by follow-on beads
// hk-8mup.62 (EventBus wiring) and hk-8i31.83 (RedactionRegistry wiring).
//
// # Spec alignment
//
//   - PL-020 — "Only internal/daemon is allowed to import across subsystem
//     boundaries." (specs/process-lifecycle.md §4.6)
//   - PL-020a — "All cross-subsystem registries … MUST be instantiated inside
//     the composition root (internal/daemon) on startup per §PL-005 step 0."
//     (specs/process-lifecycle.md §4.6)
//   - PL-005 step 0 — composition-root bootstrap is the first deterministic
//     startup step. (specs/process-lifecycle.md §4.2)
//
// Governed by specs/process-lifecycle.md; if any value or constraint in this
// package appears to conflict with the spec, the spec is authoritative.
package daemon
