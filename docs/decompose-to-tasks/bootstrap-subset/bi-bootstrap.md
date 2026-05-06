# BI Bootstrap Subset — Cluster E (Beads adapter)

**Date:** 2026-05-05 · **Cluster:** E — Beads adapter · **Epic:** `hk-872` · **Total beads:** 66 = 1 epic + 54 children + 11 step beads (verified `br list -l spec:beads-integration --limit 0`).

Foothold (per `bootstrap-subset-opening.md` §1, Q1/Q2/Q4 user-resolved): daemon dispatches one trivial bead end-to-end via `br`, twin handler in worktree, commit with bead-ID trailer, bead closes. BI must deliver: adapter root + JSON + exit-code class; claim/close write path; `br ready` + bead-detail reads; bead-ID propagation across run/trailer/event/session-log; `br --version` handshake; three-store authority rules. Crash-recovery (BI-031), out-of-band detection (INV-004), orphan-`br` sweep are advanced concurrency the bootstrap test does not exercise.

## Counts

- **INCLUDE: 26** (1 epic + 25 children). **EXCLUDE: 40** (29 children + 11 step). **Ratio:** 26 / 66 ≈ 39 %. Higher than the opening pass's ~15 estimate because the schema/enum tier is small but mostly non-skippable; only crash-recovery + sweep drop out cleanly.

## INCLUDE

### Spec parent · `hk-872` (epic envelope).

### §4.1–4.2 Selection + access surface
- `.1` (BI-001) Adopt SQLite fork · `.2` (BI-002) All I/O via `br` CLI · `.3` (BI-003) Forbid `br serve` · `.4` (BI-004) Daemon→`br` direct; agents via skill.

### §4.3 Beads-managed data
- `.5` (BI-005) Beads authoritative for content · `.6` (BI-006) Beads owns typed edges · `.7` (BI-007) `CoarseStatus`/`HarmonikWriteStatus` split · `.8` (BI-008+8a) Stable opaque IDs · `.9` (BI-009) Atomic-claim (the dispatch invariant the end-to-end asserts).

### §4.4 Write surface
- `hk-872.10` (BI-010+10a+10b) — Terminal-transition writes (claim/close/reopen) + status-mapping table.
- `hk-872.11` (BI-011) — Forbid intra-run writes; pairs with INV-001.
- `hk-872.12` (BI-012) — Route every terminal write through the adapter.

### §4.5 Read surface
- `hk-872.13` (BI-013) — `br ready` query; dispatch-loop input.
- `hk-872.14` (BI-014) — Dependency-graph query.
- `hk-872.15` (BI-015) — Bead-detail query.
- `hk-872.16` (BI-016) — Reconciliation queries; PL startup steps 3–4 consume.

### §4.6 Bead-ID propagation (end-to-end byte-equal)
- `hk-872.18` (BI-017) Run metadata · `hk-872.19` (BI-018) Checkpoint trailer · `hk-872.20` (BI-019) Event payload · `hk-872.21` (BI-020) Session-log metadata.

### §4.7 Authority rules + sensors
- `hk-872.22` (BI-021) Beads authoritative for content+status · `hk-872.23` (BI-022) Git authoritative for completion · `hk-872.24` (BI-023) JSONL observational · `hk-872.43` (INV-003 sensor — reduced shape).

### §4.8 Adapter (thinnest cut)
- `hk-872.27` (BI-025) — Single adapter. 18-dependent star root; nothing else compiles without it.
- `hk-872.28` (BI-025a) Exit-code → `BrError` · `hk-872.29` (BI-025b) Mandatory `--format json` · `hk-872.26` (BI-024a) `br --version` handshake (PL-005 step 4 Cat 0 pre-check).

### §6 Schemas (minimal)
- `.45` BeadRecord · `.46` CoarseStatus · `.47` HarmonikWriteStatus · `.48` DependencyEdge · `.49` EdgeKind · `.52` TerminalOp.

### §10.2 Sensors
- `hk-872.41` (INV-001 sensor) — No intra-run writes; lint + os/exec contract test + scenario.

## EXCLUDE — by category

**Crash-recovery / idempotency-key / intent-log family (15 beads).** Bootstrap does not crash mid-write; whole BI-029..BI-032 family post-bootstrap. `.36` (BI-029 idempotency key), `.37` + `.37.1`–`.37.6` (BI-030 intent-log umbrella + 6 step beads), `.38` + `.38.1`–`.38.5` (BI-031 status-check umbrella + 5 step beads), `.39` (BI-031b JSON-consistency), `.40` (BI-032), `.44` (INV-004 sensor), `.54` (crash harness).

**Orphan `br` subprocess sweep (1).** `.17` (BI-014a) — opening-pass slice; also gated on spec-edit OQ-BI-010 (PL-006 extension).

**Advanced concurrency / operational-tunable surface (3).** `.30` (BI-025c 5s/10s timeout + SIGTERM-then-SIGKILL), `.31` (BI-025d 1 MiB stderr + panic/argparse/SIGKILL scenarios), `.32` (BI-025e concurrent + `BrDbLocked` retry).

**Beads-CLI skill (2).** `.34` (BI-027), `.35` (BI-028) — twin bootstrap takes `br` direct; HC delivers skill post-bootstrap.

**Release-engineering process (3).** `.25` (BI-024 manifest), `.33` (BI-026 absorb-breakage), `.53` (BrError→RC routing — Cat 3a/0 not exercised).

**Schemas not exercised (2).** `.50` (BrError enum — bootstrap classifies OK-vs-other), `.51` (IntentLogEntry — lands with intent-log family).

**Cross-store byte-equal sensor (1, borderline).** `.42` (INV-002 sensor) — bootstrap asserts byte-equal directly; reconsider in synthesis (Q3).

## Cross-cluster edges OUT (BI INCLUDE → other-cluster targets)

`.4` → HC §4.11 skill-injection (C). `.10` → EM failure-class enum (F), WM-007 (B), RC-020 (post-bootstrap, degrades to docs). `.16` → PL §4.2 steps 3–4 (A), RC Cat 3a (post-bootstrap). `.18` → `em-014` (F). `.19` → `em-017` (F). `.20` → EV §6.3 (D). `.21` → WM §4.7 (B). `.26` → `pl-005` (A), ON exit-codes (post-bootstrap), EV `daemon_startup_failed` (D). `.43` → `rc-013` (post-bootstrap; reduces to scenario test).

**Tally OUT:** ~10 distinct edges into Clusters A/B/C/D/F; 3 degrade to docs (RC, ON exit-codes).

## Cross-cluster edges IN (other-cluster → BI INCLUDE)

Verified via `br show` Dependents. `hk-8mup.10` (PL deterministic startup) → `hk-872.13` AND `.16` — core "PL daemon dispatch through BI read surface" edge. `hk-8mup.26` (PL agent↔daemon socket) → `.34` (EXCLUDE; degrades to direct-`br` fallback). `hk-sx9r.19`/`.20`/`.44` (ON queue/restart) → `.10`/`.25`/`.26`/`.13`/`.16` (ON largely post-bootstrap). `hk-63oh.16` (RC Cat 0) → `.25` (EX), `.26` (IN). `hk-63oh.35` (RC verdict durability) → `.10`. WM/EV/EM bootstrap likely block on `.21`/`.20`/`.18`/`.19`.

**Tally IN:** PL bootstrap blocks on ≥2 BI reads (`.13`, `.16`); WM/EV/EM each on 1–2 propagation beads. ~6–8 distinct dependents from Clusters A/B/D/F. **BI is the chokepoint dependency for nearly every other cluster's bootstrap** — the brief's hint ("everything that creates beads runs through here") is confirmed.

## Open questions / ambiguities

**Q1 — Adapter idempotency (BI-INV-001..003) sensor-implementability in v0** (the called-out question):
- **INV-001 (no intra-run writes):** YES — lint + os/exec contract test + scenario all bootstrap-runnable. INCLUDE as `.41`.
- **INV-002 (bead-ID byte-equal):** PARTIAL — sensor packaging needs four cross-cluster propagation beads landed; bootstrap test asserts byte-equal directly without the sensor bead. EXCLUDE; revisit in synthesis.
- **INV-003 (git wins):** PARTIAL — full sensor depends on RC-013 (post-bootstrap). Reduced shape: scenario injects Beads-`closed` / no-merge-commit divergence, asserts no silent Beads-side correction. INCLUDE as `.43` reduced.
- **INV-004 (out-of-band detection):** NO — depends on BI-029..BI-032 family + RC-014. EXCLUDE.

**Q2 — Edges to EXCLUDE-targeted RC beads.** `.10` → RC-020, `.16` → RC Cat 3a, `.43` → RC-013. Synthesis must confirm these reduce to scenario-test, not blocking edges.

**Q3 — `.42` (INV-002 sensor) borderline.** If synthesis demands a sensor bead for auditability of bead-ID propagation, promote to INCLUDE (total then 27).

**Q4 — Skill beads `.34`/`.35`.** If HC bootstrap pulls skill injection in, both come back IN.
