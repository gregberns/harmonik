---
id: ops-monitor-never-flags-a-stopped-queue
title: The stopped-queue health check needs the crew online to fire, which is exactly when nobody is watching
type: bug
priority: 1
labels: [scripts, daemon, clear-the-ground]
depends_on: []
blocks: []
workstream: W7
batch: 3
---

## Problem

`scripts/ops-monitor-check.sh` runs a `paused-queues` health check. Its own header (§2) says it
covers "main queue or active crew queue paused-by-failure". It covers neither reliably. On
2026-08-24 two queues sat stopped on this box that this check is supposed to cover, and it would
never have flagged either. `main` had been `paused-by-failure` since 2026-08-22T05:54:44Z — two
days.

Measured against the current script on `work/alpha-integration-merge`.

## Three defects, all in the same block

**1. The alert needs the crew to be ONLINE, which is exactly when it is not needed.**

    if is_crew_online or is_live_allow:
        paused_queues.append(qname)

A stopped queue whose crew is offline is dropped silently. An online crew notices its own stopped
queue. An offline crew is the case where nobody is watching, and that is the case this check
discards. The condition is inverted with respect to its purpose.

**2. The crew name is guessed by stripping a trailing `-q`, so any other queue name never matches.**

    crew_guess = qname[:-2] if qname.endswith('-q') else qname

`charlie-batch` does not end in `-q`, so `crew_guess` becomes `charlie-batch`, and the crew is
named `charlie`. It can never match an entry in `online_crews`. The batch queue naming is an
operator directive and is not going away, so this is not a one-off typo.

**3. `main` is inert-suppressed, so the header's first claim is false.**

    INERT_SUPPRESS_JSON='["main","remote-substrate","chani-q*","duncan-q*","liet-q*","stilgar-q*"]'

`main` is the first entry. A `paused-by-failure` on `main` never fires an immediate alert, while the
header says the check covers the main queue. `LIVE_ALLOW_JSON` is `[]`, so the documented escape
hatch is unused and cannot rescue either case.

## Why both queues in the incident were invisible

- `charlie-batch` — not inert, but `crew_guess` resolves to `charlie-batch`, which matches no online
  crew, and `live_allow` is empty. Dropped.
- `main` — inert-suppressed by the list above. Dropped.

The daemon did its part: `queue_paused` fired seven times into the event log. Nothing consumed it.

## Scope

Make a stopped queue visible whether or not its crew is online.

- An offline crew must make a paused queue MORE alert-worthy, not less. Fix the condition rather
  than adding another name to an allow list.
- Resolve the owning crew from the queue registry rather than guessing it from the queue name. If no
  registry lookup exists, alert on the queue itself and name no crew — a paused queue is worth an
  alert even when its owner is unknown.
- Decide `main` deliberately. Either remove it from the suppression list, or correct the header so
  it stops claiming a coverage the code does not provide. Do not leave the two disagreeing.

## Done when

1. A test drives the check with a `paused-by-failure` queue whose crew is OFFLINE and asserts the
   check flags it. Show this test RED against the current script before it goes green.
2. A test uses a queue name that does not end in `-q` — use `charlie-batch`, the real name from the
   incident — and asserts the check flags it.
3. The header's stated coverage and the code's actual coverage agree for `main`. A test or a
   grep-free assertion pins whichever way the decision goes.
4. The new tests go in `test/exploratory/ops_monitor_check_test.sh`. That harness is the right
   home and it is already the behavioural one: it runs the script against stubbed inputs and
   asserts on its OUTPUT, it greps no source text, and it already holds a paused-queue case
   (`Test 3: paused-queue (non-inert crew online)`). Extend it; do not start a new harness.

   **Read its header checklist before you change anything.** Two entries there record the defective
   behaviour as the expected behaviour — `paused-queue -> immediate signal (non-inert crew online)`
   and `inert-queue suppression (main queue paused -> no alert)`. The suite pins the bug as correct,
   so a fix makes those cases fail. That is the fix working. Update them to state the new rule
   rather than deleting them, and say in the commit body which expectation changed and why.

   Separately, four Go tests DO assert on this script's source text — `strings.Contains(raw,
   "_dedup_key")` and similar, in `cmd/harmonik/watch_escalation_we_soak2_test.go` and
   `cmd/harmonik/watch_zombie_soak3_test.go`. Add no more of that kind. Those greps are the defect
   that `ops-monitor-check-to-go` records; they are not your job here.

## Notes

This is NOT the same work as `ops-monitor-check-to-go`, which is a shell-to-Go port parked on an
open operator ruling about whether shell-to-Go is a workstream. This is a behavioural bug and it
should not wait for that ruling. If the port lands first, the port must carry these three fixes and
these tests.
