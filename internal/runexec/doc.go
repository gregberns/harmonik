// Package runexec holds the two pure run-lifecycle reactors that express the
// harmonik daemon's per-bead run lifecycle (the logic historically carried by
// the beadRunOne god-function): a per-session Dispatch machine (dispatch.go) and
// a per-run Run machine (run.go), over a shared flat event/action vocabulary
// (vocab.go). Both reactors are TOTAL and pure — no I/O, no clock reads, no
// identifier minting — and MUST NOT import internal/daemon (RSM-001/028;
// depguard-enforced). The daemon shell (internal/daemon/runshell.go, RT7) owns
// every effect and drives each reactor to a terminal.
//
// Three-way state-vocabulary disambiguation (avoid confusing these):
//
//   - runexec.DispatchPhase / RunPhase — this package: the two run-lifecycle
//     reactors' states (Idle…Working; Resolving…Done).
//   - handler-contract session lifecycle (HC-065) — the formal per-session
//     lifecycle machine, driven here as a downstream projection via
//     ActDriveLifecycleTerminated (RSM-023); it is NOT this package's state.
//   - queue-model bead states — the bead-queue ledger states (ready, in_progress,
//     …), owned by internal/queue and the daemon's terminal transitions
//     (beads-integration §4.4); the Run machine factors their INVOCATION
//     (ActCloseBead / ActReopenBead) but does not own the transition.
package runexec
