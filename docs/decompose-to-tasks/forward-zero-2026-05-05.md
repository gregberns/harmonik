# Forward-Zero Verification — `hk-ahvq.39`

**Date:** 2026-05-05
**Scope:** all 10 `*-pilot-data.yaml` files under `docs/decompose-to-tasks/`
**Raw grep:** [`forward-zero-grep-2026-05-05.txt`](forward-zero-grep-2026-05-05.txt)

## Result

**unresolved-survivors** — 99 surviving `forward:*` mnemonics in pilot-data yamls, plus 99 prose mentions (comments / narrative). Forward-zero is NOT yet achieved.

`hk-ahvq.39` left **OPEN**. Two upstream blockers (`.45`, `.46`) are still DRAFT, and four already-CLOSED backfill beads (`.16`, `.23`, `.30`, `.37`) added Beads `br dep add` edges but did NOT rewrite the citing pilot-data yamls — every yaml entry that issued a successful `br dep add` is still carrying its `forward:` placeholder string.

## Grep summary

| Metric | Count |
|---|---|
| Total `forward:*` lines (any context, all yamls) | 137 |
| Total `forward:*` **edges** (`{from: …, to: "forward:…"}`) | 99 |
| Case (a) out-of-scope-with-spec-text | 0 |
| Case (b1) yaml-rewrite-missed (target in depends-on, target loaded) | ~64 |
| Case (b2) §3.2 violations (target NOT in citing pilot's depends-on) — `.46` lane | ~32 |
| Case (b3) 3 missing EV events — `.45` lane | 3 |
| Case (c) S07-pending (`forward:sh-*`) | 0 |

(b1 + b2 + b3 = 99. Bucket counts are approximate because some PL `forward:rc-NNN` edges are within depends-on (b1) and PL `forward:on-*` are §3.2 violations (b2); see per-file breakdown.)

## Per-file hits

### `cp-pilot-data.yaml` (13 edges) — ALL pending `.46`/`.45`

CP depends-on: `[architecture, execution-model, event-model, handler-contract]`.
WM/ON/RC/BI are NOT in CP's depends-on → §3.2 violations per `.46`.

| Line | Mnem | Target | Class | Action |
|---|---|---|---|---|
| 1211 | cp-040 | forward:wm-NNN | (b2) §3.2 violation | DELETE yaml entry; log as F-pilot-EV-3 in cp-pilot.md §3 (per `.46`) |
| 1214 | cp-013 | forward:on-NNN | (b2) §3.2 violation | DELETE; log as F-pilot-EV-3 |
| 1215 | cp-037 | forward:on-NNN | (b2) §3.2 violation | DELETE; log as F-pilot-EV-3 |
| 1216 | cp-038 | forward:on-NNN | (b2) §3.2 violation | DELETE; log as F-pilot-EV-3 |
| 1217 | cp-047 | forward:on-NNN | (b2) §3.2 violation | DELETE; log as F-pilot-EV-3 |
| 1220 | cp-027 | forward:rc-NNN | (b2) §3.2 violation | DELETE; log as F-pilot-EV-3 |
| 1221 | cp-040a | forward:rc-NNN | (b2) §3.2 violation | DELETE; log as F-pilot-EV-3 |
| 1222 | cp-041 | forward:rc-NNN | (b2) §3.2 violation | DELETE; log as F-pilot-EV-3 |
| 1223 | cp-inv-003 | forward:rc-NNN | (b2) §3.2 violation | DELETE; log as F-pilot-EV-3 |
| 1226 | cp-031 | forward:bi-NNN | (b2) §3.2 violation | DELETE; log as F-pilot-EV-3 |
| 1229 | cp-034b | forward:ev-events.policy-expression-exceeded-cost | (b3) | Resolve after `.45` lands EV r2 events; rewrite to `ev-events.policy_expression_exceeded_cost` |
| 1230 | cp-041 | forward:ev-events.verdict-envelope-mismatch | (b3) | Resolve after `.45`; rewrite to `ev-events.verdict_envelope_mismatch` |
| 1231 | cp-043 | forward:ev-events.control-points-registration-started | (b3) | Resolve after `.45`; rewrite to `ev-events.control_points_registration_started` |

### `hc-pilot-data.yaml` (7 edges) — ALL (b1) `.23` yaml-rewrite-missed

HC depends-on includes `process-lifecycle`. PL mnem map is loaded; all 5 distinct PL targets present:
- `pl-001` → hk-8mup.2; `pl-003b` → hk-8mup.8; `pl-005` → hk-8mup.10; `pl-006` → hk-8mup.11; `pl-009b` → hk-8mup.18.

Backfill `hk-ahvq.23` (PL → priors) closure note: "HC: 7/8 edges added" — the `br dep add` calls were issued, but the yaml strings were never rewritten.

| Line | Mnem | Target | Class | Action |
|---|---|---|---|---|
| 864 | hc-007 | forward:pl-001 | (b1) | Rewrite yaml: `forward:pl-001` → `pl-001`. Backfill: `.23` |
| 865 | hc-016a | forward:pl-003b | (b1) | Rewrite: `forward:pl-003b` → `pl-003b`. Backfill: `.23` |
| 866 | hc-016a | forward:pl-009b | (b1) | Rewrite: `forward:pl-009b` → `pl-009b`. Backfill: `.23` |
| 867 | hc-044 | forward:pl-001 | (b1) | Rewrite: `forward:pl-001` → `pl-001`. Backfill: `.23` |
| 868 | hc-044 | forward:pl-005 | (b1) | Rewrite: `forward:pl-005` → `pl-005`. Backfill: `.23` |
| 869 | hc-044a | forward:pl-001 | (b1) | Rewrite: `forward:pl-001` → `pl-001`. Backfill: `.23` |
| 870 | hc-051 | forward:pl-006 | (b1) | Rewrite: `forward:pl-006` → `pl-006`. Backfill: `.23` |

### `on-pilot-data.yaml` (12 edges) — ALL (b1) `.37` yaml-rewrite-missed

ON depends-on includes `reconciliation`. RC mnem map loaded; all 7 distinct RC targets present.

`.37` closure note: ON 9/12 success, 3 cycle-rejected (on-014→rc-018, on-014→rc-025, on-032→rc-018). The 3 cycle-rejected entries should be DELETED (RC pilot already carries the reverse direction); the 9 successful ones should be rewritten in yaml.

| Line | Mnem | Target | Class | Action |
|---|---|---|---|---|
| 1676 | on-003 | forward:rc-012 | (b1) | Rewrite → `rc-012`. Backfill: `.37` |
| 1677 | on-009 | forward:rc-014 | (b1) | Rewrite → `rc-014`. Backfill: `.37` |
| 1678 | on-010 | forward:rc-002 | (b1) | Rewrite → `rc-002`. Backfill: `.37` |
| 1679 | on-014 | forward:rc-018 | (b1) cycle-rejected | DELETE (RC→ON owned by RC pilot). Backfill: `.37` |
| 1680 | on-014 | forward:rc-025 | (b1) cycle-rejected | DELETE (RC→ON owned by RC pilot). Backfill: `.37` |
| 1681 | on-027.s2 | forward:rc-002 | (b1) | Rewrite → `rc-002`. Backfill: `.37` |
| 1682 | on-028 | forward:rc-007 | (b1) | Rewrite → `rc-007`. Backfill: `.37` |
| 1683 | on-030 | forward:rc-008 | (b1) | Rewrite → `rc-008`. Backfill: `.37` |
| 1684 | on-032 | forward:rc-018 | (b1) cycle-rejected | DELETE (RC→ON owned by RC pilot). Backfill: `.37` |
| 1687 | on-047 | forward:rc-018 | (b1) | Rewrite → `rc-018`. Backfill: `.37` |
| 1688 | on-048 | forward:rc-018 | (b1) | Rewrite → `rc-018`. Backfill: `.37` |
| 1689 | on-inv-005 | forward:rc-008 | (b1) | Rewrite → `rc-008`. Backfill: `.37` |

### `pl-pilot-data.yaml` (39 edges) — Mixed `.37` (b1) + `.46` (b2)

PL depends-on: `[architecture, execution-model, event-model, handler-contract, control-points, reconciliation, beads-integration, workspace-model]` (RC IS in depends-on; ON is intentionally NOT).

#### PL forward:rc-* (13 edges) — (b1) `.37` yaml-rewrite-missed

`.37` closure note: "PL 7/14 success, 7 cycle-rejected as F13 bidirectional cites". 7 PL→RC edges should be DELETED (RC pilot already carries the reverse: rc-002a→pl-006/pl-011/pl-002a; rc-012→pl-005/pl-010; rc-012a→pl-009/pl-010; rc-020a→pl-005; rc-020b→pl-018a; rc-025a→pl-018a). Remaining ones rewritable, but most pl-*→rc-NNN are unresolved-label form (target was section-cite, no single-owner) so the safer fix is to convert each individually based on `.37`'s success/reject log.

| Line | Mnem | Target | Class | Notes |
|---|---|---|---|---|
| 1189 | pl-005 | forward:rc-NNN | (b1) | Likely cycle-rejected per `.37` (rc-012→pl-005 exists) |
| 1190 | pl-006 | forward:rc-NNN | (b1) | Cycle-rejected per `.37` |
| 1191 | pl-008 | forward:rc-NNN | (b1) | TBD — check `.37` log |
| 1192 | pl-009 | forward:rc-NNN | (b1) | Cycle-rejected per `.37` |
| 1193 | pl-009a | forward:rc-NNN | (b1) | TBD |
| 1194 | pl-010 | forward:rc-NNN | (b1) | Cycle-rejected per `.37` |
| 1195 | pl-017 | forward:rc-NNN | (b1) | TBD |
| 1196 | pl-021a | forward:rc-NNN | (b1) | TBD |
| 1197 | pl-025 | forward:rc-NNN | (b1) | TBD |
| 1198 | pl-026 | forward:rc-NNN | (b1) | TBD |
| 1199 | pl-inv-003 | forward:rc-NNN | (b1) deferred | `.37` deferred — section-cite-only, no single-owner |
| 1200 | pl-005 | forward:rc-002a | (b1) | Cycle-rejected per `.37` |
| 1201 | pl-005 | forward:rc-002b | (b1) | TBD |

#### PL forward:on-* (26 edges) — (b2) §3.2 violations per `.46`

ON is intentionally NOT in PL's depends-on (cycle-break). All 26 are §3.2 violations.

| Line | Mnem | Target | Class | Action |
|---|---|---|---|---|
| 1206 | pl-002 | forward:on-008 | (b2) | DELETE; log as F-pilot-EV-3 |
| 1207 | pl-003 | forward:on-NNN | (b2) | DELETE; log as F-pilot-EV-3 |
| 1208 | pl-004 | forward:on-020a | (b2) | DELETE; log as F-pilot-EV-3 |
| 1209 | pl-004 | forward:on-030a | (b2) | DELETE; log as F-pilot-EV-3 |
| 1210 | pl-005 | forward:on-020a | (b2) | DELETE; log as F-pilot-EV-3 |
| 1211 | pl-005 | forward:on-030a | (b2) | DELETE; log as F-pilot-EV-3 |
| 1212 | pl-008 | forward:on-003 | (b2) | DELETE; log as F-pilot-EV-3 |
| 1213 | pl-008a | forward:on-NNN | (b2) | DELETE; log as F-pilot-EV-3 |
| 1214 | pl-009 | forward:on-031 | (b2) | DELETE; log as F-pilot-EV-3 |
| 1215 | pl-009 | forward:on-033 | (b2) | DELETE; log as F-pilot-EV-3 |
| 1216 | pl-010 | forward:on-002 | (b2) | DELETE; log as F-pilot-EV-3 |
| 1217 | pl-010 | forward:on-035 | (b2) | DELETE; log as F-pilot-EV-3 |
| 1218 | pl-011 | forward:on-027 | (b2) | DELETE; log as F-pilot-EV-3 |
| 1219 | pl-011 | forward:on-029 | (b2) | DELETE; log as F-pilot-EV-3 |
| 1220 | pl-011a | forward:on-033 | (b2) | DELETE; log as F-pilot-EV-3 |
| 1221 | pl-014a | forward:on-041 | (b2) | DELETE; log as F-pilot-EV-3 |
| 1222 | pl-018a | forward:on-NNN | (b2) | DELETE; log as F-pilot-EV-3 |
| 1223 | pl-021a | forward:on-017 | (b2) | DELETE; log as F-pilot-EV-3 |
| 1224 | pl-027 | forward:on-020 | (b2) | DELETE; log as F-pilot-EV-3 |
| 1225 | pl-027 | forward:on-020a | (b2) | DELETE; log as F-pilot-EV-3 |
| 1226 | pl-028 | forward:on-002 | (b2) | DELETE; log as F-pilot-EV-3 |
| 1227 | pl-028 | forward:on-008 | (b2) | DELETE; log as F-pilot-EV-3 |
| 1228 | pl-028 | forward:on-014 | (b2) | DELETE; log as F-pilot-EV-3 |
| 1229 | pl-028 | forward:on-041 | (b2) | DELETE; log as F-pilot-EV-3 |
| 1230 | pl-env-001 | forward:on-NNN | (b2) | DELETE; log as F-pilot-EV-3 |
| 1231 | pl-inv-001 | forward:on-NNN | (b2) | DELETE; log as F-pilot-EV-3 |

### `wm-pilot-data.yaml` (28 edges) — Mixed `.16`/`.23`/`.30`/`.37` (all b1)

WM depends-on includes RC, ON, PL, BI — all loadable.

#### WM forward:rc-* (12 edges)

`.37` says "WM 11/11 success" — yaml-rewrite missed for 11; `wm-032` deferred (section-cite-only).

| Line | Mnem | Target | Class | Action |
|---|---|---|---|---|
| 1260 | wm-022a | forward:rc-NNN | (b1) | Rewrite per `.37` log. Backfill: `.37` |
| 1261 | wm-023 | forward:rc-NNN | (b1) | Rewrite. Backfill: `.37` |
| 1262 | wm-024 | forward:rc-NNN | (b1) | Rewrite. Backfill: `.37` |
| 1263 | wm-034 | forward:rc-NNN | (b1) | Rewrite. Backfill: `.37` |
| 1264 | wm-035 | forward:rc-NNN | (b1) | Rewrite. Backfill: `.37` |
| 1265 | wm-036 | forward:rc-NNN | (b1) | Rewrite. Backfill: `.37` |
| 1266 | wm-038 | forward:rc-NNN | (b1) | Rewrite. Backfill: `.37` |
| 1267 | wm-040 | forward:rc-NNN | (b1) | Rewrite. Backfill: `.37` |
| 1268 | wm-inv-001 | forward:rc-NNN | (b1) | Rewrite. Backfill: `.37` |
| 1269 | wm-inv-002 | forward:rc-NNN | (b1) | Rewrite. Backfill: `.37` |
| 1270 | wm-032 | forward:rc-NNN | (b1) deferred | `.37` deferred — section-cite-only, no single-owner |
| 1271 | wm-033 | forward:rc-NNN | (b1) | Rewrite. Backfill: `.37` |

#### WM forward:on-* (9 edges)

`.30` ON-backfill: 6 added, 4 §-only deferred.

| Line | Mnem | Target | Class | Action |
|---|---|---|---|---|
| 1274 | wm-008 | forward:on-NNN | (b1) deferred | §-only — `.30` deferred |
| 1275 | wm-019 | forward:on-NNN | (b1) deferred | §-only — `.30` deferred |
| 1276 | wm-024 | forward:on-NNN | (b1) deferred | §-only — `.30` deferred |
| 1277 | wm-031 | forward:on-NNN | (b1) deferred | §-only — `.30` deferred |
| 1278 | wm-036 | forward:on-014 | (b1) | Rewrite → `on-014`. Backfill: `.30` |
| 1279 | wm-038 | forward:on-NNN | (b1) | Rewrite → `on-013` per `.30` log. Backfill: `.30` |
| 1280 | wm-040 | forward:on-NNN | (b1) | Rewrite → `on-013` per `.30` log. Backfill: `.30` |
| 1281 | wm-009 | forward:on-NNN | (b1) | Rewrite → `on-018` per `.30` log. Backfill: `.30` |
| 1282 | wm-env-001 | forward:on-NNN | (b1) | Rewrite (multi-target — split into `on-018` + `on-027`) per `.30` log. Backfill: `.30` |

#### WM forward:pl-* (3 edges)

`.23` says "WM: 0/3 added — see findings". All 3 are blocked (reciprocal-cite cycles + OQ-coordination ambiguity), should be DELETED + logged.

| Line | Mnem | Target | Class | Action |
|---|---|---|---|---|
| 1285 | wm-013c | forward:pl-NNN | (b1) blocked | DELETE (PL-006 already cites wm-013a as prereq). Backfill: `.23` |
| 1286 | wm-033 | forward:pl-NNN | (b1) blocked | DELETE (PL-006 cites wm-033 as prereq). Backfill: `.23` |
| 1287 | wm-env-002 | forward:pl-NNN | (b1) blocked | DELETE (OQ-coordination, not a real edge). Backfill: `.23` |

#### WM forward:bi-* (4 edges)

`.16` (WM backfill — covers WM forward edges to its own loaded depends-on?). The `.16` bead description is "Resolve forward:wm-* edges in {bi, ar, em, ev, hc, cp} pilots' yamls" — that's the OUTBOUND direction (others → WM). WM's INBOUND (WM → BI) backfill belongs to a BI-direction backfill that isn't listed in `.39`'s blockers. **Unexpected pattern (escalate):** these 4 WM `forward:bi-*` edges have no listed backfill bead — there is no `hk-ahvq.X` BI-direction backfill task in `.39`'s dependency list.

| Line | Mnem | Target | Class | Action |
|---|---|---|---|---|
| 1290 | wm-001 | forward:bi-NNN | (b1) UNCOVERED | Need new BI→priors backfill bead; targets bi-017/bi-018 |
| 1291 | wm-006 | forward:bi-NNN | (b1) UNCOVERED | Targets bi-014 |
| 1292 | wm-028 | forward:bi-NNN | (b1) UNCOVERED | Targets bi-017/bi-018 |
| 1293 | wm-inv-002 | forward:bi-NNN | (b1) UNCOVERED | Targets bi-010 |

(BI mnem map IS loaded — `mnem-maps/bi-mnem-map.csv` — so the targets are resolvable; the only thing missing is a backfill bead to do the rewrite.)

### `meta-pilot-data.yaml` / `rc-pilot-data.yaml`

Zero `forward:` edges. Only narrative mentions in meta-pilot describing the backfill protocol. Clean.

## Recommended patches

The patches needed to drive `.39` to clean:

1. **Land `hk-ahvq.46` (CP r2 pilot patch)** — DRAFT → CLOSED.
   - DELETE 10 §3.2-violating CP entries (cp-pilot-data.yaml lines 1211, 1214–1217, 1220–1223, 1226).
   - DELETE 26 §3.2-violating PL entries (pl-pilot-data.yaml lines 1206–1231).
   - Log all 36 deletions as F-pilot-EV-3 informational findings in cp-pilot.md / pl-pilot.md §3.
   - Bump cp-pilot version + pl-pilot version.

2. **Land `hk-ahvq.45` (EV r2 spec patch)** — DRAFT → CLOSED.
   - Add 3 events to `specs/event-model.md` §8 + §6.3 payload schemas.
   - Add 3 rows to `mnem-maps/ev-mnem-map.csv` for `ev-events.policy_expression_exceeded_cost`, `ev-events.verdict_envelope_mismatch`, `ev-events.control_points_registration_started`.
   - Rewrite cp-pilot-data.yaml lines 1229–1231 from kebab-case `policy-expression-exceeded-cost` (etc.) to underscore-case `policy_expression_exceeded_cost` (or whatever EV r2 settles on) and drop the `forward:` prefix.

3. **Re-open + extend `hk-ahvq.16`/`.23`/`.30`/`.37` work as a yaml-rewrite pass** (or file a NEW bead `.50`-ish "yaml-rewrite forward-zero pass"). All four backfill beads issued `br dep add` calls but did NOT mutate their citing pilot-data yamls. The rewrite is a separate (mechanical) pass:
   - HC: 7 lines → drop `forward:` prefix on `pl-*` targets (all loadable).
   - ON: 9 of 12 → drop `forward:` prefix on `rc-*` targets; DELETE the 3 cycle-rejected (on-014→rc-018, on-014→rc-025, on-032→rc-018).
   - PL forward:rc-*: split into 7 cycle-rejected (DELETE) + 6 rewrite — needs `.37` log replay.
   - WM forward:rc-*: 11 → drop prefix; 1 deferred (wm-032) → log + DELETE.
   - WM forward:on-*: 5 → drop prefix (resolve to on-014, on-013, on-018, on-027); 4 deferred → log + DELETE.
   - WM forward:pl-*: 3 → all blocked, DELETE + log.

4. **File new BI→priors backfill bead** — covers WM→BI 4 edges (lines 1290–1293) which are not in any listed backfill bead's scope but ARE within WM's depends-on and BI mnem map IS loaded. Rewrite `forward:bi-NNN` → resolved bi-* targets per inline comments (bi-017, bi-018, bi-014, bi-010).

## S07-pending references

**None.** Zero `forward:sh-*` mnemonics. Either S07 is not yet cited from any loaded pilot, or its citers are still informational (not edge-form). No S07-pending caveat applies.

## Status decision

**`hk-ahvq.39` left OPEN.** Not flipped to `done`.

**Why:** 99 forward-edge survivors across 5 pilot yamls. Two upstream blockers (`.45`, `.46`) still DRAFT. Four already-CLOSED backfills (`.16`, `.23`, `.30`, `.37`) added Beads `br dep add` calls but did NOT mutate pilot yaml strings — the yaml-rewrite step was skipped. Plus an uncovered BI→priors direction (4 WM→BI edges with no listed backfill).

**Path to clean:**
1. `.46` lands → −36 edges (CP §3.2 + PL §3.2 deletions).
2. `.45` lands → −3 edges (CP ev-events).
3. New yaml-rewrite pass on the 4 closed backfills → −56 edges (HC 7 + ON 12 + PL rc 13 + WM 24).
4. New BI→priors backfill bead → −4 edges (WM→BI).

After all four, expected survivors = 0. Then `.39` flips to `done`.

**Unexpected patterns surfaced:**

- **YAML-rewrite gap as a class issue.** The discipline distinguishes "edge added in Beads" from "yaml string rewritten" but no closed backfill bead has the second step in its closure criteria. This is a discipline-lane finding worth folding into the discipline-batch queue (sibling to `.23`'s findings #1–#3).
- **Uncovered BI→priors direction.** `.39`'s blocker list omits a BI-direction backfill. WM has 4 inbound-to-BI edges that are loadable but unscheduled.
- **`.46` violations broader than its description.** `.46`'s prose says "CP ~10 entries + PL 26 entries" — this verification confirms 36 §3.2 violations matching the description. Not a surprise, but logging.
