# 07 — Tasks: no-auto-dispatch

> The task breakdown is the operator/captain-authored bead tree `hk-04q2j.1`-`.5`. This pass records
> it as the implementation plan and maps each bead to its component + the spec-first C4 obligation
> the beads do not name. Beads are the source of truth; do not fork a parallel task list.

## Bead → component → sequence map

| Bead | P | Component | Blocks on | Status |
|------|---|-----------|-----------|--------|
| hk-04q2j.1 | P2 | C3-shim (export_test.go synthetic queue) | — | IN PROGRESS (juliet; dot implement-node failure recorded) |
| hk-04q2j.2 | P1 | C1-delete (workloop.go fallback + noAutoPull field) | .1 | OPEN |
| hk-04q2j.3 | P1 | C2-plumbing (NoAutoPull across cmd/daemon/core/scenario) | .2 | OPEN |
| hk-04q2j.4 | P2 | C3-tests (delete/rewrite fallback-pinned tests) | .2 | OPEN |
| hk-04q2j.5 | P3 | C5-cleanup (restartbackoff.go + flags) OPTIONAL | .3 | OPEN |
| (unfiled) | P1 | **C4-spec** (execution-model.md EM-066/067 + §7.4/§10.1/§10.2 + queue-model QM-054) | rides .2/.3 | **NOT YET A BEAD** |

## Gap surfaced by this planning pass

**C4 (the spec amendment) has no bead.** harmonik is spec-first; deleting the code without amending
`execution-model.md` §4.11/§7.4/§10.1/§10.2 (and the queue-model.md §8.5 cross-ref) leaves the spec
sanctioning a path the code no longer has. RECOMMENDATION: file a P1 bead
`codename:no-auto-dispatch` for the spec amendment, blocked-by hk-04q2j.2, and land it in lockstep.
The draft is ready in `05-spec-drafts/execution-model.md` (pending D1). *(Planning agent did NOT
file the bead — that is a dispatch action outside this planning task; flagged for the operator/
captain.)*

## Per-bead acceptance (from the bead notes, condensed)

- **.1** legacy no-QueueStore tests route a synthetic single-item queue from BrAdapter; suite green
  before deletion.
- **.2** `queueItemIndex<0` = idle+continue (sleep submitWakeC); br-ready block + noAutoPull field/
  assignment/godoc gone; queue-pull path + sentinel Ready() (1919/1970) preserved.
- **.3** no `NoAutoPull`/`autoPullFlag` symbol survives the enumerated sites; D2 decides flag no-op
  vs delete.
- **.4** fallback-pinned tests deleted; boot_redispatch_gate rewritten onto a submitted queue;
  queue-path pause cases kept.
- **.5** restartbackoff.go retired (optional); flags retired per D2.
- **C4** spec no longer sanctions the fallback; grep sweeps in 06-integration all clean.

## Decisions blocking `ready` (pass 8)

- **D1** — retire EM-066/EM-067 vs repurpose EM-066 (spec shape). Recommendation: retire both.
- **D2** — `--auto-pull`/`--no-auto-pull`: keep as no-op stubs (one release) vs delete now.
  Recommendation: keep as no-op + deprecation log, delete in follow-up.
- **File the C4 spec bead** (or confirm C4 folds into hk-04q2j.2/.3).
