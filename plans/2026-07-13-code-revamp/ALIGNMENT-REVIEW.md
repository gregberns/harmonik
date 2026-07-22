# Post-Implementation Alignment Review — standing trigger

> **Why this exists:** the code-revamp phases (M2/M3/M4/M5, validation-net) are being *planned* ahead of / in parallel with implementation — in particular M2/M3 design is authored BEFORE P1 finishes proving the `internal/substrate` reactor seam. Plans written against an unproven seam can drift from what actually gets built. This note arms a review that fires **after each phase's implementation lands** to catch that drift while it's cheap to fix.
>
> **Operator directive (2026-07-13):** "put a note somewhere so an agent reviews these plans after the implementation is complete to ensure alignment."

## How to use
When a phase's implementation MERGES, run the matching review below **before** starting the next phase's implementation. Each review is a read-only diff of *plan-vs-built*: does the code match the spec the plan assumed? Where it diverged, is the divergence better (update the plan) or a mistake (fix the code)? Delegate to a sub-agent; report drift to the operator.

## Triggers (check on every admiral boot until all are ✅)

| # | Fires when… | Review that plan… | Against… | Status |
|---|---|---|---|---|
| AR-1 | **P1 (`session-restart-substrate`) merges** | M2 (`agent-input-substrate`) + M3 (`run-state-machine`) design | the ACTUAL proven `internal/substrate` seam + keeper reactor as built (not the as-planned seam). This is THE load-bearing one — M2/M3 design was authored ahead of the proof. | ⏳ pending P1 |
| AR-2 | **M2 merges** | M4 (`remote-substrate`) design (rebuilds remote on the M2 `handler.Substrate` input/ack contract) | the actual M2-1 seam input/ack contract as built | ⏳ |
| AR-3 | **M3 first slice (merge-queue) merges** | M5 (daemon god-package decompose) problem-space | the actual `runexec`/merge-queue extraction as built | ⏳ |
| AR-4 | **validation-net lands** | the M-phase plans' concurrency assumptions | what the concurrency net actually catches (does it exercise the real merge/dispatch races the plans assume?) | ⏳ |
| AR-5 | **each M-phase merges** | Track C complexity/coverage ceilings | did new code stay under the ceilings, or were `//nolint` escape hatches used? (ratchet-integrity check) | ⏳ |

## What "aligned" means (pass criteria)
- Every contract the downstream plan depends on (seam method signatures, ack semantics, event shapes) exists in the built code with the assumed shape — or the plan is updated to the real shape.
- No planned deletion removed something the built code still needs (the M2-6 keeper-input hazard is the canonical example — verify the keeper actually migrated before M2-6 deletes `pasteinject.go`).
- The plan's blast-radius / line-count / dependency claims still hold against the merged tree.
- Divergences are classified: **plan-was-wrong** (update plan) vs **code-drifted** (fix code or accept + record).
