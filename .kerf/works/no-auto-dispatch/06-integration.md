# 06 — Integration: no-auto-dispatch

> How the code deletion (C1/C2/C3/C5) and the spec amendment (C4) land coherently, and what other
> subsystems must not break.

## Landing sequence (from the bead `blocks` graph)

1. **C3-shim (hk-04q2j.1, P2)** — `export_test.go` synthetic-queue helper. Keeps the suite green.
   *In progress by crew juliet on `juliet-q`; a `dot` implement-node failure is recorded on the
   bead — this is the current gating item.*
2. **C1-delete (hk-04q2j.2, P1)** — the primary workloop.go deletion. Blocked on #1.
3. **C2-plumbing (hk-04q2j.3, P1)** and **C3-tests (hk-04q2j.4, P2)** — parallel after #2.
4. **C5-cleanup (hk-04q2j.5, P3, optional)** — after #3.
5. **C4-spec (execution-model.md + queue-model.md)** — amend in lockstep with #2/#3 so no
   spec-vs-code contradiction exists at any commit. (Spec-first: ideally the spec draft is finalized
   into `specs/` as the code lands, not after.)

## Coherence checks (must all hold post-landing)

- **Queue-pull path intact.** Crews' `harmonik queue submit`/`append` still dispatches. This is the
  "agents decide what runs" surface and the whole point — verify a submitted queue dispatches
  normally (verification plan step 2).
- **Boot-time queue RESTORE intact.** `LoadQueueAtStartup` (lifecycle/startup_pl005_qm002.go) still
  restores agent-submitted queues across a restart. This is agent intent replayed, NOT daemon
  self-start — it must survive. (Explicit PRESERVE in the epic.)
- **Sentinel governor intact.** `workloop.go:1919, 1970` `Ready()` observe-only reads still feed the
  movement/opportunity governor. Deleting the dispatch `Ready()` must not touch these.
- **Operator-pause / handler-pause on the QUEUE path intact.** Only the `br ready` copies of those
  gates are deleted; the queue-path pause gates (QueueStatusPausedByDrain etc.) are untouched.
- **Spec cross-refs reconciled.** queue-model.md §8.5 QM-054 informative co-consumer list updated
  (see C4). No dangling reference to EM-067 anywhere in `specs/`.

## Interaction with the flywheel / cognition-loop topology

The queue-only topology is precisely what the flywheel design (CL-013/070/071, referenced in the
workloop godoc) already assumed — a Pi cognition loop curates dispatch timing via the queue surface.
Removing the fallback REMOVES a contradiction with that design rather than creating one. No flywheel
spec change is required.

## Blast-radius confirmation

grep sweep at finalize time (belt-and-suspenders):
- `grep -rn 'noAutoPull\|NoAutoPull\|autoPullFlag\|auto-pull' cmd/ internal/ specs/` → empty (or
  only D2 no-op stubs + retirement-notice prose).
- `grep -rn 'brAdapter.Ready' internal/daemon/workloop.go` → only the two sentinel reads remain.
- `grep -rn 'EM-066\|EM-067' specs/` → only retirement rows / revision log.
