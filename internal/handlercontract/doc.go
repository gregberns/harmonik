// Package handlercontract contains the Go realization of harmonik's
// handler-contract spec — error sentinels, version/protocol types, and
// related classification helpers. See specs/handler-contract.md.
//
// All type definitions and sentinel values in this package are normatively
// governed by that spec; if a field description or constraint appears to
// conflict with the spec, the spec is authoritative.
//
// # A9 clause reconciliation (2026-07-17)
//
// A mega-review (finding A9) found that several HC-xxx "behavioral contract"
// helpers in this package had silently diverged from the shipping
// implementation: they had ZERO non-test callers and sat on no production
// path, while the real code did the equivalent work a different way. Each such
// helper (and its self-referential theater test) was DELETED and the clause
// re-pointed at the mechanism that actually ships:
//
//   - HC-044a orphan / worker-pidfile (CheckOrphanHeldWorkspace,
//     WriteWorkerPidfileAtomic, RemoveWorkerPidfile, WorkerPidfilePath):
//     the shipping orphan mechanism is internal/daemon RunOrphanSweep
//     (tmux-session + crew-registry sweep) plus workspace.SweepStaleLeaseLocks
//     (the real per-worktree lease-lock in internal/workspace). The per-run
//     .harmonik/worktrees/<runID>/.lock pidfile was never wired.
//   - HC-048a provisioning backoff (RetryProvisionWithBackoff): the adapters
//     (internal/handler adapter_claudecode.go / adapter_pi.go) only classify a
//     rate-limit signal and delegate backoff to the daemon workloop
//     (per-queue backoff / perqueuespendmeter). No adapter ran the base=1s /
//     cap=16s / maxAttempts=4 loop this helper asserted.
//   - HC-013a turn/rotate boundary (TurnBoundaryGuard): no handler-side
//     turn-rotate boundary ships; log/history rotation lives in
//     internal/daemon brhistoryrotate.go.
//   - HC-015 run-lock discipline (RunLock): the shipping run/merge
//     serialization is the workspace lease-lock (internal/workspace wm013*)
//     plus daemon workloop mutexes (mergeMu).
//   - HC-016 per-role work queues (WorkQueueSet): the shipping dispatch
//     surface is per-queue-name in internal/daemon (queuestore_hkj808w.go,
//     perqueuespendmeter_tigaf11.go, queueledger_bridge.go), not
//     per-actor-role.
//   - HC-018 cancellation bounds (CancelGoSideBound, CancelSubprocessBound):
//     real bounded cancellation is context.WithTimeout / drain caps in
//     internal/daemon (workloop drain timeout) and the handler's ctx-bounded
//     stdin/watcher goroutines.
//   - HC-033 startup secret-leak schema guard (CheckPayloadSchema): the
//     shipping startup schema scan is EV-036 in internal/core/eventregistry.go
//     wired at daemon startup (delivered under mega-review finding A4). The
//     dead handlercontract copy was removed; see the A4 lane for the live
//     guard.
//
// HC-004 launch idempotency: the LaunchKey helper was dead and is deleted; the
// Handler.Launch doc comment (handler_hc001.go) now records that idempotency is
// NOT enforced by the shipping handler.Launch (fresh session id + unconditional
// spawn) — a real latent double-spawn defect flagged as a daemon/handler-lane
// follow-up rather than an unenforced MUST.
package handlercontract
