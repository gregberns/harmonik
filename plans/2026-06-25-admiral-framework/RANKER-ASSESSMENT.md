# Ranker / Starvation Assessment — `codename:ranker-starvation-assessment`

> **Bead:** hk-qi479 · P1  
> **Gates:** Part 1b priority-order LOCK (PLAN-v2 task 4 / dated-directive ordering semantics)  
> **Does NOT gate:** LI-1 (lanes.json), SD-1 (ops-monitor known-ready-lane), or the `expires:`/LAPSE/owner mechanism — those ship independently (PLAN-v2, already landed).  
> **Assessment date:** 2026-07-05  
> **Source:** Live kerf-feedback log (`docs/kerf-feedback/`), ops-monitor script (Check 8 / Check 9), PLAN-v2 Workstream 2 definition.

---

## Three Criteria

From PLAN-v2 §Workstream 2:

1. **(i) Does it surface small starved beads?**
2. **(ii) Does it respect the standing dated-directive ordering?**
3. **(iii) Can the ops-monitor read its lane-ready predicate cheaply?** *(largely de-risked by lanes.json + `br ready --parent` — SD-1/Check 9)*

---

## Candidate 1 — `kerf next`

### Finding: FAILS (i) and (ii); N/A for (iii)

**Scoring collapses to two buckets (documented defect).**  
KF-026-02 (2026-05-21) and KF-026-01 (2026-05-26): of 66 beads in the live feed, 50+
carry exactly `score=3.486` and the rest carry `score=0`. The rubric is dependency fan-out,
momentum, rework, and creation order — `br` priority is not a factor. P0 and P3 beads are
indistinguishable in the ranked feed.

**Structural bias against starved beads.**  
A small, isolated bead with few dependents (exactly the starvation profile — forgotten
leaf work) gets low fan-out → sinks to score 0. kerf-next would surface it *last*, not
first. The beads most likely to be starved (old, low-connectivity) are the ones kerf-next
systematically buries.

**No concept of dated-directive ordering.**  
kerf-next has no read path into `direction-log.md`, `captain-lanes.md` dated blocks, or
`lanes.json`. The operator's temporal sequencing intent (RETURN-PATH / lane ordering) is
entirely opaque to the scoring algorithm. There is no mechanism to express "this session we
care about lane X first."

**Criterion (iii) — N/A.**  
The ops-monitor's lane-ready predicate (SD-1 / Check 9) uses `lanes.json + br ready
--parent <epic_id>`, a design independent of kerf-next. This criterion is already de-risked
and does not constrain the ranker choice.

**What kerf-next IS good at.**  
Work-level navigation: identifying which kerf *work* has ready beads and where each bead
belongs in the planning lineage. This context is valuable for an agent picking up a new
bead. kerf-next should remain in the loop for that purpose; it should NOT be the priority
source of truth for dispatch ordering or starvation detection.

---

## Candidate 2 — `bv --robot-insights` (PageRank / betweenness centrality)

### Finding: ANTI-PATTERN for (i); FAILS (ii); N/A for (iii)

**PageRank and betweenness are connectivity metrics — they are the logical inverse of the
starvation signal.**

- **PageRank** of a bead is proportional to how many important beads depend on it.
  A starved bead (forgotten, isolated, no downstream dependents) has PageRank ≈ 0 by
  definition. bv --robot-insights would rank it last.
- **Betweenness centrality** counts how many shortest dependency paths pass through a
  node. A leaf bead that blocks nothing and is blocked by nothing sits on zero shortest
  paths; betweenness = 0.

Using bv --robot-insights as a starvation detector is structurally backwards: it maximally
surfaces hub beads that block many others (already high-visibility, already well-known),
and it minimally surfaces the lone beads that have been forgotten (zero betweenness, zero
dependents). These are precisely the beads we want to surface.

**bv has no concept of dated-directive ordering.** Graph-metric scores have no relationship
to operator timing intent, direction-log RETURN-PATHs, or captain-lanes dated blocks.

**Criterion (iii) — N/A.** Same as kerf-next; SD-1 / Check 9 is the correct surface for
the ops-monitor lane-ready predicate.

**What bv --robot-insights IS useful for.**  
Identifying high-impact *blocking* beads — hub beads that unblock many others. This is a
valid, complementary signal for "what to unblock first in the dependency graph" (different
from "what old work has been forgotten"). Use bv --robot-insights for that specific
sub-problem, not for starvation.

---

## Recommendation: Composed primitives over a new ranker

Neither kerf-next nor bv-robot-insights satisfies (i) or (ii). Rather than introduce a
third opaque ranker, the right answer is a **two-layer composed approach** using existing
`br` primitives:

### Layer A — Starvation surface (criterion i)

**Tool:** `br ready` sorted by `updated_asc` (oldest-updated ready bead first).

The starvation signal is *age without progress*. A bead that has been open and unblocked for
months with no status changes is, by definition, starved. `br ready` already computes
"unblocked + open" (the necessary precondition); sorting by ascending last-updated surfaces
the most neglected beads first. This is O(1) to read, requires no graph analysis, and
produces exactly the right ordering for starvation purposes.

Suggested periodic surface: Check 8 in `scripts/ops-monitor-check.sh` already runs
`br ready --limit 0`. Extending it to emit the **top-N oldest-updated** ready beads as a
digest item would close the starvation loop without new tooling. (Exact threshold: 60+ days
without update in the open+ready state is a reasonable starvation cutoff to surface in
digest, not as an IMMEDIATE.)

### Layer B — Within-lane dispatch ordering (criterion i, complementary)

**Tool:** `br ready --parent <epic_id>` sorted by priority.

When the ops-monitor or captain is deciding which bead to dispatch within a lane,
`br ready --parent <lane_epic_id>` gives the unblocked beads under that lane's epic.
Sort by `br` priority (P0 first, P4 last). This is already the correct within-lane
dispatch mechanism and is already used by Check 9 / SD-1 for the lane-ready predicate.
No new infrastructure required.

### Layer C — Cross-lane ordering / sequencing (criterion ii)

**Tool:** `direction-log.md` RETURN-PATH (human/admiral-authored).

The dated-directive ordering question (which lane to staff first when multiple are ready)
is NOT a ranking problem for any automated tool. It is a temporal sequencing intent that
the operator expresses in `direction-log.md` RETURN-PATH entries and `captain-lanes.md`
dated directives. The captain reads this BEFORE acting on any ranked feed. The RETURN-PATH
is already the correct surface for cross-lane sequencing; no ranker can encode it better.

**The ranker's role is within-lane dispatch, not cross-lane sequencing.** The direction-log
owns sequencing; `br ready --parent` owns within-lane ordering.

---

## Part 1b Priority-Order LOCK: Recommendation

The LOCK should bind to the following semantics (not to any ranker's opaque score):

1. **Cross-lane ordering** — governed by `direction-log.md` RETURN-PATH + `captain-lanes.md`
   dated directives with `expires:`/LAPSE/owner (already shipped per PLAN-v2 Track A step 4).
   The captain reads the direction-log RETURN-PATH as ground truth for sequencing.

2. **Within-lane dispatch ordering** — governed by `br` priority field (`br ready --parent
   <epic_id>` sorted by priority). P0 beats P4 deterministically; no judgment required.

3. **Starvation surfacing** — governed by age signal (`br ready` sorted by `updated_asc`);
   emitted as a periodic digest item in Check 8, not an IMMEDIATE.

**The lock is NOT on kerf-next or bv-robot-insights output.** Both are unsuitable for the
priority-order semantics this gate was intended to validate. The lock should capture the
three-layer composition above: direction-log (sequencing) → `br priority` (within-lane) →
age (starvation backstop).

**kerf-next retains its current role** as a work-navigation surface (which kerf work owns
each bead; planning context), used by agents picking up a bead, not by the ops-monitor
or captain's dispatch loop.

---

## Summary Table

| Criterion | kerf-next | bv --robot-insights | Recommendation (composed) |
|-----------|-----------|---------------------|--------------------------|
| (i) Surface starved beads | FAIL — hub bias, 2-bucket scoring | ANTI-PATTERN — PageRank buries leaves | `br ready` sorted `updated_asc` |
| (ii) Respect dated ordering | FAIL — no direction-log read | FAIL — no directive concept | direction-log RETURN-PATH (human) |
| (iii) Ops-monitor lane-ready | N/A (de-risked) | N/A (de-risked) | lanes.json + `br ready --parent` (SD-1 / Check 9, already live) |

**Verdict: lock Part 1b to the three-layer composed primitives above.** No new ranker
binary or tool is required. The existing `br`, `direction-log.md`, and `lanes.json` surface
covers all three criteria with better fidelity than either candidate.

---

## Ops-Monitor Extension (optional, low-risk)

If the team wants automated starvation surfacing in the existing check cycle, extend Check 8
to emit the top-3 oldest-updated ready beads as a digest line alongside the backlog-ready
count. This is a ~10-line bash/Python addition to `scripts/ops-monitor-check.sh` at the
Check 8 block — no new tool, no rebuild, same daemon re-read path as SD-1. Recommend filing
as a follow-on `chore` bead (P3) rather than blocking the priority-order lock on it.
