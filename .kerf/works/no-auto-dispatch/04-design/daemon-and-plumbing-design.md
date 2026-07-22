# 04 — Change design: daemon dispatch loop + plumbing (C1 + C2 + C3 + C5)

> Design for the code deletion. Back-filled from `hk-04q2j.1`-`.5` + the verified C1 anchors.
> Spec-side design is in `04-design/spec-surface-design.md`.

## Design principle

**Collapse, don't rewrite.** The `noAutoPull==true` branch already implements the desired terminal
behavior (idle + sleep on `submitWakeC` + continue). The whole change is: promote that branch to be
unconditional and delete everything that only existed to serve the alternative (`br ready`) branch.
Nothing new is built.

## C1 — workloop.go (the primary edit)

Before (shape):
```
if queueItemIndex < 0 {                 // no active-queue item to dispatch
    if deps.noAutoPull {                // queue-only mode
        sleep(submitWakeC); continue
    }
    // operator-pause br-ready copy ...
    // handler-pause  br-ready copy ...
    readyRecords := deps.brAdapter.Ready(ctx)   // <-- self-start dispatch
    pick ready[0]; dispatch; readyPathAttempts[...]++
}
```
After:
```
if queueItemIndex < 0 {                 // no active-queue item: idle until an agent submits
    sleep(submitWakeC); continue
}
```
Plus: delete the `noAutoPull` struct field (~712-717) + its assignment (~1183); rewrite the
work-loop godoc at ~79-84 to state "queue-only: the daemon dispatches ONLY agent-submitted queue
items; a bare boot dispatches zero runs." PRESERVE the queue-pull path (upstream of this fork) and
the sentinel `Ready()` reads at 1919/1970.

## C2 — plumbing removal (gated on C1)

Remove every `NoAutoPull`/`autoPullFlag` reference enumerated in the C2 research findings. Order is
mechanical: field is dead after C1, so deletion is a compile-driven sweep — remove the field, then
fix each resulting unused reference.

- **D2 (needs human):** keep `--auto-pull`/`--no-auto-pull` as accepted-but-ignored no-ops vs
  delete. RECOMMENDATION (planning agent, non-binding): keep them as parsed-but-ignored no-ops for
  one release so supervisors/scripts passing the flag do not hard-error, with a deprecation log
  line; delete in a follow-up. Operator decides.

## C3 — tests (shim first, migration after)

- **Step 1 shim** (`export_test.go`): synthesize a single-item queue from `BrAdapter` when deps
  carry no `QueueStore`. Lands BEFORE C1 so the suite stays green. (In progress by juliet.)
- **Step 4 migration** (after C1): delete the fallback-pinned tests; rewrite `boot_redispatch_gate`
  onto a submitted queue; keep the queue-path pause cases. Per C3 research findings.

## C5 — vestigial cleanup (optional, gated on C2)

- `restartbackoff.go`: rationale (throttle repeated boot auto-pulls) is gone. Deletion is safe but
  optional — it throttles nothing meaningful now. RECOMMENDATION: delete it in the same cleanup as
  the flags, but this is operator's call (P3).

## Verification plan (end-to-end, not just unit)

1. Build the daemon; boot with an empty project (no submitted queue). Observe `events.jsonl` /
   `harmonik subscribe` for a bounded window: assert ZERO `run_started`, no agent subprocess, daemon
   parked in the idle/submit-wake branch.
2. Submit a queue (`harmonik queue submit`) with one bead; assert it dispatches normally (queue-pull
   path intact).
3. `grep -rn 'noAutoPull\|NoAutoPull\|autoPullFlag' cmd/ internal/` returns nothing (or only the
   no-op flag stubs if D2 = keep).
4. Full `go test ./...` on daemon + scenario packages green.
