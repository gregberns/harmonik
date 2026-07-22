# 04 — Change design: spec surface (C4, execution-model.md)

> Design for the spec amendment that must accompany the code deletion (harmonik is spec-first).
> Depends on operator decision D1 (retire vs repurpose EM-066/EM-067).

## What changes, independent of D1

- **§7.4 pseudocode:** collapse the `queue IS None` two-way fork to a single unconditional
  `idle_wait_for_queue_submission; CONTINUE`. Remove the `no_auto_pull()` branch and the `br ready`
  fallback arm + its defense-in-depth operator-pause re-assert.
- **§10.1 Core MVH:** replace "queue-only is the default for all topologies … `--auto-pull` … is a
  conforming opt-in … `--no-auto-pull` … no-op alias" with "queue-only is the ONLY topology; a bare
  boot with no submitted queue MUST dispatch zero runs; there is no `br ready` fallback." Drop
  EM-066/EM-067 from the Core-MVH required-set if D1 = retire (see below).
- **§10.2 test obligations:** delete the "historical-topology" `--auto-pull` fallback-dispatch test;
  KEEP the quiet-daemon zero-runs test as the sole boot obligation.
- **§12 revision log:** add an amendment row recording the removal (source: operator directive
  2026-07-21 / DECISIONS.md; epic hk-04q2j).

## D1 branch A — RETIRE EM-066 + EM-067 (recommended)

Mark both IDs retired in §12; fold the "zero runs on bare boot" guarantee into §7.4 + §10.1 as an
unconditional invariant (candidate: a one-line EM-INV or a sentence in §10.1). Cleanest: the fallback
concept leaves the spec entirely. Retired IDs are never reused (matches this spec's discipline: "No
prior IDs renumbered or retired" is the norm, so an explicit retirement row is the correct signal).

## D1 branch B — REPURPOSE EM-066, retire only EM-067

Rewrite EM-066's body to "The daemon dispatches EXCLUSIVELY from the active queue; a bare boot with
no submitted queue dispatches zero runs. There is no auto-pull / `br ready` fallback." Retire EM-067
(it is purely about the fallback's pause binding). Preserves EM-066 as the stable anchor other specs
cite (queue-model.md §8.5 QM-054 and execution-model §10.1 both reference the EM-066/EM-067 pair).

## Cross-spec ripple to check at draft time

- `queue-model.md §8.5 QM-054` INFORMATIVE note cites "the execution-model br-ready fallback gate
  (EM-067)" as a co-consumer of `operator_pause_status`. If EM-067 is retired, that informative
  cross-ref must be updated to drop the fallback co-consumer (the queue transition remains).
- `execution-model.md §7.4` glossary line "absent an active queue, no dispatch occurs (§4.11, §7.4)"
  already matches the target end-state — it needs no change, and is evidence the spec's own glossary
  was already ahead of the retained fallback code.

## Recommendation

**D1 = branch A (retire both).** The fallback is being removed, not repurposed; keeping a
repurposed EM-066 invites future confusion about whether an opt-in still exists. But EM-066 is
cross-cited, so branch B is defensible. **Operator's call.**
