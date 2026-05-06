# Bootstrap Subset (`hk-ahvq.41`) — Pass 2 Summary

**Date:** 2026-05-05
**Status:** Pass 2 complete — 7 cluster reports + 1 AR-verification scratch written. Pass 3 (synthesis + cross-cluster edge closure + label application) blocked on user input on the **S07 scenario-harness gap** (see §"Critical findings" below).

## Per-cluster results

| Cluster | Spec | Epic | INCLUDE | EXCLUDE | Total | Report |
|---|---|---|---|---|---|---|
| A — Process skeleton | PL | `hk-8mup` | 37 | 22 | 59 | `pl-bootstrap.md` |
| B-WM — Workspace substrate | WM | `hk-8mwo` | 45 | 26 | 71 | `wm-bootstrap.md` |
| B-EM + F — Workflow execution | EM | `hk-b3f` | 65 | 23 | 88 | `em-bootstrap.md` |
| C — Handler interface + twin | HC | `hk-8i31` | 47 | 33 | 80 | `hc-bootstrap.md` |
| D — Event-bus skeleton | EV | `hk-hqwn` | 36 | 105 | 141 | `ev-bootstrap.md` |
| E — Beads adapter | BI | `hk-872` | 26 | 40 | 66 | `bi-bootstrap.md` |
| Deferred — AR / CP / ON / RC | (4 specs) | (4 epics) | 15 | ~288 | ~303 | `deferred-bootstrap.md` + `ar-verification.md` |
| **Total** | | | **~271** | **~537** | **~808** | |

Per-spec INCLUDE: PL 37/59 (63%), WM 45/71 (63%), EM 65/88 (74%), HC 47/80 (59%), EV 36/141 (26%), BI 26/66 (39%), AR 5/55 (9%), CP 0/85 (0%), ON 6/85 (7%), RC 4/79 (5%).

**Bootstrap subset is ~271 of ~808 child beads (~34% of corpus).**

## Findings vs the opening pass's estimates

- **Total INCLUDE landed at ~271, not 150–180.** Two reasons:
  - The corpus is larger than the opening pass assumed (~806 child beads, not ~639 — HANDOFF's "~639" was a session-start figure; the EV §8 row taxonomy + BI step-bead expansion push the real total to 806).
  - Schemas (esp. EM §6.1, BI §6) and §-section-anchor beads pull in more than estimated. Schemas are type vocabulary every other cluster consumes — they have to be in.
- **EM** is bigger than estimated (65 INCLUDE vs est. 40–45). §6.1 schemas (16 of 17) are bootstrap-essential.
- **EV** total is much bigger (141 vs est. 63). The 78 §8 row children dominate. INCLUDE landed at 36 = 20 first-class + 16 §8 rows.
- **HC** at 47 INCLUDE vs est. 20–25 — §8.3 row family + schema dependencies pull more.
- **WM** at 45 INCLUDE vs est. 25–30 — working-definition steps 3–4 (merge-back + sidecar/trailer commit) pull §4.5 and §4.7 entirely.

## Critical findings

1. **S07 scenario-harness has NO dedicated spec or epic in the corpus.** `bootstrap.md` step 8 names S07; the decompose-to-tasks pass never authored an S07 spec or any S07 beads. Only adjacent bead is `hk-8i31.77` (canonical twin binary, in HC cluster C). Q4 = YES on scenario-harness needs a **user decision**: (a) author a thin S07 spec + epic now (delays Pass 3), (b) declare S07 = "code-only-no-bead" and rely on the harness being authored as test code in the first self-build cycle, or (c) some hybrid.

2. **BI is the chokepoint dependency** for almost every cluster's bootstrap. PL deterministic startup blocks on `hk-872.13` + `.16`; WM/EV/EM bootstrap each block on 1–2 BI propagation beads. **Implementation order should put BI first** (consistent with `bootstrap.md` §5).

3. **HC has highest fan-in** — 76 IN edges across 6 clusters (EV 27, WM 13, CP 12, PL 9, RC 8, ON 7). Handler interface + schemas drive everything else.

4. **AR INCLUDE list disagreement.** The `deferred-bootstrap.md` agent listed `zs0.41,.50,.51,.52,.53`. The focused `ar-verification.md` agent found that **`zs0.41` is NOT a sensor** (it's `ar-042`, the meta-rule about how invariants must declare sensors) and that **`zs0.54` (agent_type regex)** is a hard prerequisite of HC's `LaunchSpec` (`hk-8i31.74`) and `Handler` (`hk-8i31.71`). **Pass 3 should use AR INCLUDE = `.50, .51, .52, .53, .54`** (5 beads — same count, different IDs).

5. **CP cleanly defers.** WM bootstrap surprise: `cp-037` cites land on EXCLUDED wm beads. CP is fully deferred for the bootstrap subset (0 INCLUDE).

## Open per-cluster questions surfaced (for Pass 3)

- **EM:** `hk-b3f.84` outcome-kind enum has a borderline `reconciliation_verdict` value (RC-coupled), but the enum TYPE is needed by the Outcome record. Recommendation: INCLUDE with runtime-unreachable assertion at bootstrap.
- **EM:** `hk-b3f.31` (em-024a) branch-tip monotonicity splits into WRITE half (bootstrap) + READ half (RC defensive routing — EXCLUDE). Partial-include needed.
- **EV:** `EventBus` `ReplayFrom` / `DeadLetterReplay` ship as `ErrNotImplemented` stubs in v0?
- **EV:** `timestamp_mono_nsec` confirmed essential; `trace_context.parent_event_id` not.
- **BI:** BI-INV-001 fully sensor-implementable; INV-003 in reduced shape (scenario test without RC-013 dispatch); INV-002 EXCLUDED (byte-equal asserted directly).
- **ON:** 8-step drain umbrella ambiguous — depends on whether §1 "clean shutdown" means SIGTERM-with-drain or `daemon stop` exit-0.
- **WM:** `wm-019a` (scratch merge-worktree) added beyond the opening pass's slice — mandatory for §4.5 to work without `--ignore-other-worktrees` hazards.

## Cross-cluster dependency closure (Pass 3 work)

Multiple cluster reports flagged edges into beads that may or may not be INCLUDE (e.g. HC depends on `hk-b3f.86` EM failure-class taxonomy — confirm INCLUDE; HC `workspace_path` cite forward-deferred to WM `.001`; etc.). Pass 3 must run a closure check: every dependency target of an INCLUDE bead must itself be INCLUDE, or the dependency is a violation.

## What Pass 3 needs

1. **User decision on the S07 gap** (above).
2. Reconcile AR INCLUDE list (use `ar-verification.md`, not `deferred-bootstrap.md`).
3. Resolve the ~7 open per-cluster borderline questions above.
4. Cross-cluster DAG closure check over the INCLUDE set; identify and resolve violations.
5. Apply `scope:bootstrap` labels to the final ~271 bead IDs via `br update --label`.
6. Produce a final consolidated `bootstrap-subset.md` document.
