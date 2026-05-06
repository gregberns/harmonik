# YAML-Rewrite Forward-Zero Cleanup — 2026-05-05

**Scope:** (b1) yaml-rewrite-missed survivors from the four already-CLOSED backfill beads
(`hk-ahvq.16/.23/.30/.37`) + (a-corner) unscheduled WM→BI direction.

**Out of scope:** (b2) §3.2 violations under `.46` (parallel discipline-patch agent) and
(b3) missing-EV-events under `.45`.

Companion to [`forward-zero-2026-05-05.md`](forward-zero-2026-05-05.md).

## Pre/Post grep counts

| Stage | Count | Composition |
|---|---|---|
| Pre-cleanup `forward:*` edges | 99 | 60 (b1) + 36 (b2) + 3 (b3) |
| Post-cleanup `forward:*` edges | 39 | 36 (b2) + 3 (b3) — all (b1) cleared |

**Reconciliation against report's expected post=35.** The report's scoreboard
(line 252) said `post=35 = 32 b2 + 3 b3`. Actual post=39 because the report's
"~32 (b2)" was approximate (caveat acknowledged in the report itself, line 25:
"Bucket counts are approximate"). True b2 count is 36 — 10 CP + 26 PL — visible
in the report's detailed per-file tables (lines 36–46 + 117–143). No anomaly;
just a count discrepancy in the summary header.

## Edges fixed

| Pilot | Lines touched | Rewrites | Deletes | Notes |
|---|---:|---:|---:|---|
| `hc-pilot-data.yaml` | 7 | 7 | 0 | All forward:pl-* → resolved pl-*; `pl` added to cross_specs |
| `on-pilot-data.yaml` | 12 | 9 | 3 | 3 cycle-rejected (on-014→rc-018, on-014→rc-025, on-032→rc-018); `rc` added to cross_specs |
| `pl-pilot-data.yaml` | 13 | 6 | 7 | All in forward:rc-* block; 7 cycle-rejected per `.37` log; `rc` added to cross_specs |
| `wm-pilot-data.yaml` | 28 | 22 | 8 | Split: rc 11/1 delete, on 6 (incl. 1 multi-target split into 2)/4 delete, pl 0/3 delete, bi 6 (incl. 2 multi-target splits)/0; `rc`/`on`/`bi` added to cross_specs |
| **Totals** | **60** | **44** | **18** | |

**60 stale `forward:*` lines cleared** (matches 99 pre − 39 post). Of the 44
rewrites, 6 are new edges from 2 multi-target splits (wm-001 and wm-028 each
split into bi-017 + bi-018 per `.30` precedent on wm-env-001).

## `br dep add` commands run (a-corner, 6 calls from 4 yaml lines)

The 4 unscheduled WM→BI edges (per report §`wm-pilot-data.yaml#### WM forward:bi-*`)
needed both `br dep add` and yaml rewrite. Per inline comment guidance, two of
the four lines were multi-target citations (wm-001 → bi-017+bi-018; wm-028 →
bi-017+bi-018) and were split per the `.30` multi-target precedent on
wm-env-001 → on-018+on-027, yielding 6 actual edges.

```sh
br dep add hk-8mwo.3 hk-872.18 -t blocks   # wm-001 → bi-017
br dep add hk-8mwo.3 hk-872.19 -t blocks   # wm-001 → bi-018
br dep add hk-8mwo.10 hk-872.14 -t blocks  # wm-006 → bi-014
br dep add hk-8mwo.40 hk-872.18 -t blocks  # wm-028 → bi-017
br dep add hk-8mwo.40 hk-872.19 -t blocks  # wm-028 → bi-018
br dep add hk-8mwo.56 hk-872.10 -t blocks  # wm-inv-002 → bi-010
```

All 6 returned `✓ Added dependency` (no duplicates, no cycles). `br dep cycles`
clean afterward.

## `br dep cycles` post-cleanup

```
✓ No dependency cycles detected.
```

Run after each pilot's worth of rewrites and once at the end. Clean throughout.

## Resolution method for placeholder mnems

PL forward:rc-* lines used the `rc-NNN` placeholder (no specific target in the
yaml string), and the report flagged most as TBD. Resolution method: for each
PL bead, ran `br show <id> | grep "hk-63oh"` to see which RC-direction edges
exist in beads, then rewrote yaml to the resolved RC mnem (or DELETED if no
edge existed = cycle-rejected by `.37`). This is the "log-replay-by-DB-inspection"
method since `.37`'s rejection log isn't separately persisted.

Result for PL forward:rc-* (13 lines):
- 6 rewrites: pl-009→rc-013, pl-009a→rc-error.cat-3, pl-017→rc-007,
  pl-021a→rc-error.cat-0, pl-025→rc-002, pl-026→rc-007.
- 7 deletes (cycle-rejected): pl-005→rc-NNN, pl-006→rc-NNN, pl-008→rc-NNN,
  pl-010→rc-NNN, pl-inv-003→rc-NNN, pl-005→rc-002a, pl-005→rc-002b.

Same method used for WM forward:rc-* (1 delete: wm-032 — no edge, deferred per `.37`).

## Anomalies / follow-ups

1. **PL forward:rc-* count discrepancy.** Report's `.37` closure note said
   "PL 7/14 success, 7 cycle-rejected". Actual yaml had 13 lines in the
   forward:rc-* block (lines 1189–1201), and DB inspection confirmed 6 success
   + 7 reject = 13. The "/14" in the report appears to be an off-by-one in
   the prior closure note. No corrective action needed — yaml reflects DB
   ground truth.

2. **Multi-target split convention.** For 3 yaml lines that cited 2 BI/ON
   targets (wm-001 → BI-017+BI-018; wm-028 → BI-017+BI-018; wm-env-001 →
   ON-018+ON-027), I split each into 2 separate `{from, to}` edges per the
   `.30` precedent. Comments include "1 of 2"/"2 of 2; multi-target split"
   markers. If a future loader run rejects the second-half edge for any
   reason, the comment trail makes the split visible.

3. **`cross_specs` additions.** Where rewriting introduced edges to a spec
   not yet in the yaml's `cross_specs:` block, the entry was added so the
   loader can resolve. Specifically:
   - `hc-pilot-data.yaml`: added `pl`.
   - `on-pilot-data.yaml`: added `rc`.
   - `pl-pilot-data.yaml`: added `rc`. (`on` deliberately still omitted —
     the 26 PL→ON forward edges remain b2 §3.2 violations under `.46`.)
   - `wm-pilot-data.yaml`: added `rc`, `on`, `bi`. (`pl` deliberately
     omitted — all 3 WM→PL forward edges were blocked per `.23`.)

4. **No new BI→priors backfill bead needed.** The report (line 234)
   suggested filing a new bead for the WM→BI a-corner direction. Since this
   pass closed those 4 yaml lines (6 actual edges) directly, that
   recommendation is superseded — `.39` itself becomes the BI-direction
   resolution bead.

## Files modified

- `docs/decompose-to-tasks/hc-pilot-data.yaml`
- `docs/decompose-to-tasks/on-pilot-data.yaml`
- `docs/decompose-to-tasks/pl-pilot-data.yaml`
- `docs/decompose-to-tasks/wm-pilot-data.yaml`

## What remains (not this agent's scope)

- 36 (b2) §3.2-violation edges in CP (10) + PL (26) — handled by parallel
  discipline-patch agent re-tagging as `cite:cycle-break:on`.
- 3 (b3) missing-EV-event edges in CP — pending `.45` (EV r2) landing.
