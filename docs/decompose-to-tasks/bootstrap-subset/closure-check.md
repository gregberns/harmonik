# Cross-Cluster Dependency Closure Check — Bootstrap Subset

**Date:** 2026-05-05 · **Scope:** `hk-ahvq.41` Pass 3 step 4.

**INCLUDE set examined: 285 beads** = PL 37 + WM 45 + EM 65 + HC 45 + EV 42 (24 first-class + 18 §8 rows) + BI 36 + AR 5 + ON 6 + RC 4. HC and BI counts taken from each report's explicit bullet enumeration (top-line tally figures in those two reports undercount the bullets). AR uses `ar-verification.md` correction (`zs0.50–.54`), not `deferred-bootstrap.md`.

## Edge counts

| | Count |
|---|---:|
| Total `blocks` edges walked from INCLUDE beads | **792** |
| Satisfied within INCLUDE | 640 (81%) |
| Satisfied within EXCLUDE-with-rationale | 84 (11%) |
| **Violations** | **68 (9%)** |
| Distinct violation targets | 48 |

## Violations — by source cluster

| Cluster | Violations |
|---|---:|
| A — PL | 10 |
| B-WM | 7 |
| B-EM+F | 5 |
| C — HC | 5 |
| D — EV | 5 |
| E — BI | 0 |
| Deferred — AR | 24 |
| Deferred — ON | 3 |
| Deferred — RC | 9 |
| **Total** | **68** |

BI closes cleanly. AR sensor self-references account for the largest absolute count, but they're all benign declarative-rule references (see classification below).

## Violations — classified by recommendation

### IGNORE — AR §4 declarative-requirement targets (38 edges, 28 unique targets)

Per `deferred-bootstrap.md` §1, AR §4 declarative beads (`.1`–`.40`, `.42`–`.49`) are "satisfied by structural conformance of A–F clusters and do not require a dedicated implementation task at MVH." They showed up as violations only because no cluster report enumerated them in EXCLUDE-with-rationale (rationale was global). Cited targets: `zs0.1, .2, .3, .5, .7-.18, .25, .28, .33, .37-.49`. Sources span every cluster (PL 4, WM 3, EM 5, HC 3, EV 2, ON 1) plus 24 AR-internal sensor self-edges where `.50/.51/.52/.53` cite the §4 rules they audit. **Recommendation: IGNORE; record the global rationale once in Pass 3 synthesis.**

### IGNORE — EV §8 row children paired with deferred mechanisms (3 edges, 3 unique targets)

Cited row → deferred mechanism: `hqwn.11 → .59.12 hook_fired` (CP deferred); `hqwn.13 → .59.75 consumer_failed` (async/dead-letter EV-011/.14 deferred); `hqwn.58 → .59.50 store_divergence_detected` (RC detector deferred). All three are EV-internal forward references to event rows whose emitting subsystems are out of bootstrap. **IGNORE.**

### PULL_IN — EV §8 rows that PL bootstrap emits (5 unique targets, 7 edges)

These are NOT covered by the EV report's deferred-mechanism rationale because PL bootstrap explicitly emits them on the daemon-startup path. **Recommendation: PULL_IN all 5.**

- **`hqwn.59.44 reconciliation_category_assigned`** — emitted by PL-005 (`hk-8mup.10`) per pl-bootstrap.md §4. Also referenced by RC `.70`.
- **`hqwn.59.46 reconciliation_verdict_executed`** — emitted by PL-005.
- **`hqwn.59.61 daemon_degraded`** — emitted by PL-010 (`hk-8mup.19`); also referenced by RC `.17`. Cat 0 degraded path is in MVH per opening §1.
- **`hqwn.59.70 daemon_orphan_sweep_completed`** — emitted by PL-006 (`hk-8mup.11`). Cluster A startup orphan sweep is INCLUDE; its emission target must be too.
- **`hqwn.59.71 infrastructure_unavailable`** — emitted by PL-010; also referenced by RC `.16, .62`. Cat 0 detection path emits this.

### PULL_IN — HC adapter surface (1 unique target, 2 edges)

- **`hk-8i31.15` Adapter surface is fixed (HC-013)** — depended on by INCLUDE `.48` (DetectReady) and `.64` (one-watcher sensor). HC report enumerated `.73` (Adapter interface schema) but missed `.15`, the rule that fixes the four-method Adapter surface. **Recommendation: PULL_IN.**

### IGNORE — CP forward-referenced primitive (1 unique target, 2 edges)

- **`hk-a8bg.1` ControlPoint primitive** — sources `hk-8mup.10, .33`. Bootstrap workflow has no ControlPoint nodes (CP fully deferred per `core-scope.md` §10); PL ships an empty registry. **IGNORE.**

### IGNORE — RC + WM-env forward references (9 edges)

- **RC self → deferred RC test-infra (4 edges):** `63oh.16, .17 → .74` (detector harness); `63oh.62, .70 → .79` (taxonomy conformance harness). Cat 0 + Cat 5 fire without the harnesses. **IGNORE.**
- **`63oh.17 → sx9r.53` heartbeats** — heartbeat infrastructure post-MVH; the Cat 0 post-ready rule is a negative invariant ("MUST NOT transition daemon state"). **IGNORE.**
- **`8mwo.45 → 63oh.65` Cat 3 store-disagreement** — first-start orphan sweep finds nothing; Cat 3 dispatch unreachable at bootstrap. **IGNORE.**
- **`8mup.16 → 63oh.18` RC-013 detector emits reconciliation_category_assigned** — PL emits the row directly (covered by PULL_IN of `.59.44`); RC detector will replace direct emission in cycle 2. **IGNORE.**
- **`8mwo.1 → sx9r.22, .32`** — WM envelope inherits ON NFRs declaratively; WM bootstrap §4 already notes "ON-018/027 inherited NFRs — supporting framing, not blocking." **IGNORE.**

### RELAX — ON internal forward-deferred (2 edges)

`hk-sx9r.20` (ON-016 queue schema version-check) cites `.22` (N-1 compat window) and `.77` (compat harness). Bootstrap pins to v1, so the rule is trivially satisfied. **Recommendation: drop `.20` from ON INCLUDE (RELAX) and rely on Cat 0 `br --version` for version sanity, OR keep `.20` and PULL_IN `.22` as a one-line declaration.**

## Per-cluster closure status

| Cluster | PULL_IN | IGNORE | RELAX | Status |
|---|---:|---:|---:|---|
| A — PL | 5 | 5 | 0 | needs 5 EV-row pull-ins |
| B-WM | 0 | 7 | 0 | closed pending IGNORE-rationale acceptance |
| B-EM+F | 0 | 5 | 0 | closed |
| C-HC | 1 (`8i31.15`) | 4 | 0 | needs 1 pull-in |
| D-EV | 0 | 5 | 0 | closed |
| E-BI | 0 | 0 | 0 | **closed cleanly** |
| Deferred-AR | 0 | 24 | 0 | closed (sensor self-edges) |
| Deferred-ON | 0 | 1 | 1 (`sx9r.20`) | borderline |
| Deferred-RC | 0 | 9 | 0 | closed (covered by PL pull-ins) |

(The 5 EV-row PULL_INs are attributed to the cluster that emits them — PL — but they materialize as additions to D-EV's INCLUDE set.)

## Closure summary

**68 violations across 48 unique targets.** Closure resolves to:

- **6 PULL_IN** (5 EV rows + 1 HC adapter constraint) — genuine additions to the INCLUDE set. After these, INCLUDE rises from 285 to **291**.
- **1 RELAX-candidate** (`hk-sx9r.20`): borderline; synthesis can keep it (and PULL_IN `.22`) or drop it (and lean on Cat 0 `br --version` for adapter-side sanity).
- **61 IGNORE** — justified forward-deferred references (28 AR §4 declarative beads, 9 EV/RC/CP/ON-NFR forwards). Synthesis pass should log the IGNORE rationale once globally rather than per-bead.

**Bootstrap subset is dependency-closed once the 6 PULL_IN beads are added.** No cluster boundary needs renegotiation; no INCLUDE bead needs RELAXING beyond the optional ON-016 borderline. BI's chokepoint role is confirmed clean — zero outbound violations.

### Recommended INCLUDE additions (6 beads)

1. `hk-hqwn.59.44` — `reconciliation_category_assigned` (PL emits, RC consumes).
2. `hk-hqwn.59.46` — `reconciliation_verdict_executed` (PL emits).
3. `hk-hqwn.59.61` — `daemon_degraded` (PL Cat 0 emits).
4. `hk-hqwn.59.70` — `daemon_orphan_sweep_completed` (PL emits).
5. `hk-hqwn.59.71` — `infrastructure_unavailable` (PL/RC Cat 0 emits).
6. `hk-8i31.15` — Adapter surface is fixed (HC-013; depended on by INCLUDE `.48, .64`).

After PULL_IN: EV §8 row count 18 → 23 (of 78); HC INCLUDE 45 → 46; **final bootstrap subset = 291 beads**.
