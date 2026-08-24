---
id: eager-refill-decision
title: Decide whether eager refill stays, then fence whichever answer wins
type: task
priority: 1
labels: [daemon, queue, needs-operator, clear-the-ground]
depends_on: []
blocks: []
workstream: W2
batch: 1
status: NOT READY — needs an operator decision before it can be listed
---

## Problem

The eager-refill filler runs on every 2-second tick, ranks candidates with a metric that never reads
the bead priority field, and tops up only the first active group. The 2026-08-22 review flagged it as
an operator gate: it does not block the extraction work, but it blocks any claim that several
independently planned queues can run safely.

## Why this is not on TASKS.md

It requires a decision that no agent can make: whether eager refill should exist at all. The two
answers lead to different work, and doing either one speculatively wastes it.

- **Keep it** → the ranking must read priority, and the top-up must cover every active group, not the
  first. Then it needs a test proving a P0 bead is not starved behind a P3.
- **Remove it** → the queue is refilled only on demand, and the tick loses one responsibility. This is
  the smaller change and it removes a thing rather than fixing it.

## The measurement, taken 2026-08-23 evening

**The answer is zero. Eager refill has never placed a bead, on any build, since it was written.**

`kerfNextBeads` in `internal/daemon/eagerfill_em063.go` shells out to:

```
kerf next --format=json --only=bead --limit=<N>
```

`kerf next` has no `--limit` flag. Run it and it prints `Error: unknown flag: --limit` and exits
non-zero. The call uses `cmd.Output()`, so it returns an error, and `eagerRefillEval` takes its
silent `if err != nil { return }` branch before it reaches the pre-screen or the append. Nothing is
logged and no event is emitted, which is why nobody noticed.

This was never a working feature that regressed. The `--limit` argument was added the same day
`kerfNextBeads` was written, 2026-06-10, and `kerf next` has never registered that flag in any commit
of the kerf repository. That is 2.5 months, roughly 7,100 dispatches, and every daemon build
including the one signed off for production.

Corroboration from the event log (277,884 events, 2026-05-14 to 2026-08-24): the
`stale_open_bead_detected` event is emitted from one place, reachable only from eager refill's
pre-screen. It has **never** fired. Meanwhile the fleet ran normally throughout, refilled entirely by
agents calling `queue submit` and `queue append`.

### A correction to the framing above

There is no separate "on-demand refill" implementation to compare against. The tick call and the
group-completion call are **the same function**. So the real choice is not "eager versus on-demand" —
it is "keep this filler, or leave the queue agent-fed", and the queue has been agent-fed all along.

### Priority-blindness: real, but not quite as stated

Harmonik's own code reads no priority. `kerf` does read the issue priority as of 2026-06-22, but only
as a tie-break — the primary sort key is a work-level score built from dependency fan-out and
momentum. So a P0 can still rank below a P3 whenever the two sit in works with different scores,
which is the normal case. The starvation risk is real; "never reads the priority field" is slightly
stale.

## Recommended ruling, for the operator to accept or reject

**Delete it.** Keeping it means first fixing the flag, and then discovering that four more things were
never exercised either: the scan that only ever tops up the first active queue, a missing per-queue
worker gate, the priority ordering, and the silent error swallow that hid all of it. That is building
a feature, not repairing one — and it would be the first time anyone learns whether the behaviour is
wanted.

If the unattended-flywheel case later proves real, reintroduce it with an event that says it fired,
priority in the ranking, and one end-to-end test that invokes the real `kerf`. The absence of that
last item is exactly why a wrong flag survived 2.5 months.

**Care needed on deletion:** `internal/daemon/eagerfill_em063.go` also holds
`stagedBeadGeneratorEval` and its follow-up ledger. Those are a separate, live feature and must stay.

**Related defect worth filing either way:** a subprocess failure in the dispatch path is swallowed
with no log and no event. That is the reason this was invisible, and it is not specific to eager
refill.

## Limits

Do not implement either branch until the decision is recorded here with a date.
