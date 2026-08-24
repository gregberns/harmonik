---
id: eager-refill-decision
title: Decide whether eager refill stays, then fence whichever answer wins
type: task
priority: 1
labels: [daemon, queue, needs-operator, clear-the-ground]
depends_on: []
blocks: []
workstream: W1
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

## What to bring the operator

Before asking, measure: how often does eager refill actually place work that on-demand refill would
not have placed? If the answer is "almost never", the decision makes itself.

## Limits

Do not implement either branch until the decision is recorded here with a date.
