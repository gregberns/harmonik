# Reconciliation of the 2026-08-10 backlog

Status is based on current source, focused tests, and the integrated commit history
at `43ad681c4`, not task labels alone.

| Task | Status | Current evidence / disposition |
|---|---|---|
| C01 | Complete | Detached queue event intent exists. |
| C02 | Complete | `AdvanceGroup` returns ordered value intents with supplied time. |
| C03 | Complete | `AppendItems` uses detached intents. |
| C04 | Complete | Callers persist before event-bus envelope creation. |
| C05 | Complete | Strict completion receipt, binding, and marker values landed. |
| C06 | Complete | Completion transaction preparation is value-driven before I/O. |
| C07 | Complete | Completion bindings are validated on replacement intents. |
| C08 | Complete | Receipt-aware completion transaction and phased results landed. |
| C09 | Complete | Startup recovery covers interrupted completion transactions. |
| C10 | Complete | Status can resolve exact terminal facts from receipts. |
| C11 | Complete | Ownership-release markers and startup recovery landed. |
| C12 | Complete | Fail-safe receipt/marker garbage collection landed. |
| C13 | Complete | `queue.CloneQueue` centralizes detached deep copies. |
| C14 | Complete | Typed completion input/result/error vocabulary landed. |
| C15 | Complete | Pure group-completion decision landed. |
| C16 | Complete | Outer completion durability policy is pure and fail-closed. |
| C17 | Complete | Live completion uses the receipt-aware transaction. |
| C18 | Complete | Fault composition and recovery joins landed. |
| C19 | Complete | Live bead state model landed. |
| C20 | Complete | Durable handoff contract landed. |
| C21 | Active but activation-blocked | Durable dispatch records and much replay machinery landed. Production has no `Store.Create` caller; replay is partial and unsafe to activate. Preserve but park producer wiring. |
| C22 | Complete | `internal/orchestrator.SelectNextQueue` is pure and the daemon wrapper delegates to it. The 2026-08-22 plan’s “unstarted” label is stale. |
| C23 | Not started | No single supervisor owns run goroutine lifecycle and terminal result. Becomes N09. |
| C24 | Not started | Universal run records are durability infrastructure, not the complete plan/provision state machine requested here. Defer until N09. |
| C25 | Not started | DOT traversal does not yet expose the requested typed child outcome. Defer behind the current graph-spine work. |
| C26 | Not started | `RunEnv` has 29 fields and `SharedHandles` 18. Becomes N10. |
| C27 | Not started | No minimal charter-core composition root and fence exists. Becomes N12. |
| C28 | Not started | Optional admission providers remain scheduler dependencies. Becomes N13. |
| C29 | Not started | Safe same-package A1 count remains 54. Folded into N17. |
| C30 | Not started | A4 total remains 13; workflow-loader ownership is not typed once. Folded into N17. |
| C31 | Not started | Tmux not-found classification remains an adapter task. Folded into N17. |
| C32 | Not started | Remaining owned CLI/socket error-text sites require individual review. Folded into N17. |

## Net assessment

C01–C20 are the strongest completed block: they replaced an implicit, effectful
completion path with typed values and durable transactions. C21 produced valuable
components but must not be called complete while its production path is intentionally
absent and unsafe. C22 is complete and should be removed from future work queues.
The remaining priority moves from detector cleanup toward package ownership,
supervision, narrow phase inputs, and one compiler-enforced core composition root.
