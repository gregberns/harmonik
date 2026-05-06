# PL Cluster — Bootstrap Subset (`.41` Pass 2 enumeration)

**Date:** 2026-05-05
**Cluster:** A — Process skeleton
**Epic:** `hk-8mup` (Process Lifecycle spec — implementation)
**Total bead count verified:** 59 children (`hk-8mup.1` … `hk-8mup.59`, contiguous; epic itself = `hk-8mup`).
**Pass 2 user-resolved questions applied:** Q1 INCLUDE basic claude-twin, Q2 EXCLUDE Pi handler, Q4 INCLUDE basic scenario harness.

## 1. Counts

| | Count | % of PL corpus |
|---|---|---|
| INCLUDE | 37 | 63% |
| EXCLUDE | 22 | 37% |
| **Total** | **59** | 100% |

Aligns with the opening pass's "~30–40 of PL's 59" estimate. INCLUDE covers the entire pidfile/socket/startup/ready/shutdown/agent-supervision happy path plus the harness fixtures verifying it; EXCLUDE concentrates in §4.7 ntm constraint-rules, §4.8 crash recovery, §4.9 upgrade, §5 sensor beads, and the crash/upgrade harnesses.

## 2. INCLUDE — bead-by-bead

### §4.a envelope (1)

- `hk-8mup.1` — Subsystem envelope declaration (`internal/daemon`). Sets the package boundary; nothing else compiles without it.

### §4.1 per-project daemon scope (8)

- `hk-8mup.2` — One daemon per project (PL-001). Foundational uniqueness invariant.
- `hk-8mup.3` — Pidfile at `.harmonik/daemon.pid` (PL-002).
- `hk-8mup.4` — Pidfile lock fd-lifetime advisory `flock`/`F_OFD_SETLK` (PL-002a).
- `hk-8mup.5` — Atomic pidfile write truncate-rewrite-keep-fd (PL-002b).
- `hk-8mup.6` — Socket at `.harmonik/daemon.sock` (mode 0600) (PL-003).
- `hk-8mup.7` — Socket wire format JSON-RPC 2.0 over NDJSON (PL-003a). Twin handler talks here.
- `hk-8mup.8` — Pre-ready request rejection on unknown run_id (PL-003b).
- `hk-8mup.9` — Daemon-owned per-project file surface under `.harmonik/` (PL-004). Owns the layout twin/worktree code references.

### §4.2 startup sequence (6)

- `hk-8mup.10` — Startup order steps 0–9 + 3a + 8a deterministic (PL-005). The end-to-end backbone of "start cleanly."
- `hk-8mup.11` — Orphan sweep precedes reconciliation, 6-bullet (PL-006). Empty on first start; load-bearing on restart.
- `hk-8mup.12` — Project hash + provenance marker env-var + PGID (PL-006a). Required by orphan sweep + future events.
- `hk-8mup.13` — Orphan sweep deterministic + complete before classification (PL-007).
- `hk-8mup.14` — Startup failure-mode catalog obligation, consumed from ON-003 (PL-008).
- `hk-8mup.15` — Exit-code consumption from ON §8 (PL-008a). Need typed exits for the test harness assertions.

### §4.3 ready-state transition (3)

- `hk-8mup.16` — Ready criteria + `daemon_ready` emission with monotonic companion (PL-009). Test waits on this.
- `hk-8mup.18` — Ready-protocol surface for external callers — 3 mechanisms (PL-009b). `harmonik status` needs at least one.
- `hk-8mup.19` — Degraded state on Cat 0 infrastructure failure, pre-ready only (PL-010). Cat 0 IS in MVH per opening §1.

### §4.4 shutdown (4)

- `hk-8mup.20` — Graceful shutdown drains in-flight runs, 9-step (PL-011). Required for "clean shutdown + restart."
- `hk-8mup.21` — Shutdown event emission with monotonic companion (PL-011a).
- `hk-8mup.22` — Immediate shutdown aborts in-flight runs (PL-012). Test signal path.
- `hk-8mup.23` — Daemon does not exit on queue-empty (PL-013). Daemon stays alive between bead dispatches.

### §4.5 agent-subprocess management (3)

- `hk-8mup.24` — Agent subprocesses are children of the daemon, single `cmd.Wait` reaper (PL-014). Twin handler needs this reaper.
- `hk-8mup.26` — Agent ↔ daemon communication routes through the socket (PL-015).
- `hk-8mup.27` — Agent-subprocess failure is observed by the daemon (PL-016). Twin can't terminate silently.

### §4.6 daemon vs orchestrator-agent (2)

- `hk-8mup.30` — Panic recovery barrier in daemon main goroutine (PL-018a). Basic safety; cheap.
- `hk-8mup.33` — Cross-subsystem registries reside in the composition root (PL-020a). Need handler/event/control-point registries instantiated.

### §4.7 ntm adapter scope (1)

- `hk-8mup.35` — ntm version pin and absence-detection (Cat 0 failure) (PL-021a). Twin spawn path requires ntm presence verified.

### §4.8 crash semantics (1)

- `hk-8mup.38` — Daemon crash leaves a stale pidfile (PL-024). Detected on next startup; observable property of clean restart.

### §4.10 command surface (1)

- `hk-8mup.43` — Daemon command surface (8 entry points) (PL-028). At least `start`/`stop`/`status` for the test.

### §6 schema (1)

- `hk-8mup.49` — Define `DaemonStatus` ENUM (§6.1). Consumed by PL-028 status output.

### §10 test-infra fixtures + harnesses (6)

- `hk-8mup.50` — Pidfile + socket twin-driven fixture (PL-001..PL-003b + PL-INV-001 + PL-INV-004).
- `hk-8mup.51` — Startup + orphan sweep scenario harness (PL-005, PL-006, PL-006a, PL-007, PL-008a, PL-INV-003, PL-INV-005). Q4 anchor.
- `hk-8mup.52` — Ready-state + degraded scenario fixture (PL-009, PL-009a, PL-009b, PL-010).
- `hk-8mup.53` — Shutdown + drain scenario fixture (PL-011, PL-011a, PL-012, PL-013).
- `hk-8mup.54` — Agent supervision + spawn discipline fixture (PL-014, PL-014a, PL-015, PL-016, PL-017).
- `hk-8mup.59` — CLI command surface fixture (PL-028).

## 3. EXCLUDE categories

- **Sensor beads (§5 invariants).** `hk-8mup.44`–`.48` — PL-INV-001..005. Sensors verify properties INCLUDE beads already implement; sensors are first-self-build-cycle territory and depend on `go-arch-lint` infrastructure (.45) plus orphan-sweep precedence rule (.46) which itself needs RC integration.
- **Cat 3 fallback / silent-hang / orchestrator-agent boundary.** `hk-8mup.17` (PL-009a Cat 3 dispatch) — Cat 3 is post-MVH per opening; `hk-8mup.28` (PL-017 silent-hang detection, owned by HC §4.6) — defer with HC silent-hang slice; `hk-8mup.31` (PL-019 orchestrator-agent) — explicitly post-MVH in spec body.
- **Crash recovery / reconciliation re-runs.** `hk-8mup.39` (PL-025), `hk-8mup.40` (PL-025a), `hk-8mup.41` (PL-026). Bootstrap restart is Cat 0 only; full crash routing through reconciliation is RC cluster's first-self-build territory.
- **Upgrade.** `hk-8mup.42` (PL-027 upgrade contract). Upgrade exec-replacement is post-bootstrap.
- **§4.7 ntm constraint-rules.** `hk-8mup.34` (PL-021), `hk-8mup.36` (PL-022), `hk-8mup.37` (PL-023). Constraint declarations (sensor-targets); only PL-021a is executable code in §4.7.
- **§4.6 sensor-target rules.** `hk-8mup.29` (PL-018 deterministic Go binary), `hk-8mup.32` (PL-020 composition-root path). Lint-enforced; sensors run later.
- **Concurrency-ceiling.** `hk-8mup.25` (PL-014a rlimit-derived default). MVH dispatches one bead at a time; ceiling can be hardcoded for v0.
- **Crash + upgrade harnesses.** `hk-8mup.55` (composition-root + panic-barrier lint — pairs with deferred .29/.32), `hk-8mup.56` (ntm fixture — pairs with deferred .34/.36/.37), `hk-8mup.57` (crash recovery harness — pairs with §4.8 deferred), `hk-8mup.58` (upgrade harness — pairs with §4.9 deferred).

## 4. Cross-cluster edges OUT (PL INCLUDE → other clusters)

PL INCLUDE beads emit hard deps into seven other epics. Tally: **23 cross-cluster edges** to **5 other clusters** (EV most, then HC, BI, WM, EM, CP, RC, AR).

- → **EV** (`hk-hqwn`): 11 edges. PL-005 (`.10`) → `hk-hqwn.59.57` daemon_started, `.46` reconciliation_verdict_executed, `.44` reconciliation_category_assigned, `.2` event_id UUIDv7. PL-006 (`.11`) → `.70` daemon_orphan_sweep_completed. PL-008a (`.15`) → `.60` daemon_startup_failed. PL-009 (`.16`) → `.58` daemon_ready. PL-010 (`.19`) → `.71` infrastructure_unavailable + `.61` daemon_degraded. PL-011a (`.21`) → `.59` daemon_shutdown. PL-016 (`.27`) → `.25` agent_failed.
- → **HC** (`hk-8i31`): 5 edges. PL-003a (`.7`) and PL-015 (`.26`) → `hk-8i31.7` socket wire protocol. PL-006 (`.11`) → `hk-8i31.22` cancellation. PL-014 (`.24`) → `hk-8i31.51` subprocess child + `.28` agent_failed + `.12` watcher goroutine + `.1` Handler interface. PL-016 (`.27`) → `hk-8i31.12` watcher. PL-018a (`.30`) → `hk-8i31.13` watcher liveness/panic.
- → **BI** (`hk-872`): 5 edges. PL-005 (`.10`) → `hk-872.16` reconciliation queries + `.13` `br ready` query. PL-006 (`.11`) → `.36` deterministic idempotency key. PL-015 (`.26`) → `.34` Beads-CLI skill. PL-028 (`.43`) → `.7` HarmonikWriteStatus 5-value subset.
- → **WM** (`hk-8mwo`): 4 edges. PL-005 (`.10`) → `hk-8mwo.38` metadata sidecar + `.8` task branch `run/<run_id>`. PL-006 (`.11`) → `hk-8mwo.45` orphan-sweep stale lease-locks + `.19` lease-lock canonical path + `.4` worktree path convention.
- → **EM** (`hk-b3f`): 4 edges. PL-005 (`.10`) → `hk-b3f.85` checkpoint-trailer registry + `.75` Run record + `.20` checkpoint trailers. PL-016 (`.27`) → `.62` RETRY outcome.
- → **CP** (`hk-a8bg`): 2 edges. PL-005 (`.10`) and PL-020a (`.33`) → `hk-a8bg.1` ControlPoint primitive.
- → **RC** (`hk-63oh`): 2 edges. PL-009 (`.16`) → `hk-63oh.18` detector emits `reconciliation_category_assigned`. PL-021a (`.35`) → `hk-63oh.62` Cat 0 — Infrastructure unavailable.
- → **AR** (`hk-zs0`): 2 edges. PL-ENV (`.1`) → `hk-zs0.2` envelope slot + `.1` spec-category front-matter.

## 5. Cross-cluster edges IN (other clusters → PL INCLUDE)

Tally: **15+ cross-cluster dependents** on PL INCLUDE beads (RC heaviest, then ON, HC). All point to PL beads that ARE in the bootstrap subset, so no incoming-edge gaps.

- ← **RC** (`hk-63oh`): `hk-63oh.16` Cat 0 pre-check → PL-005 (`.10`); `.21` detector cadence → PL-005 (`.10`); `.37` verdict-execution discovery on restart → PL-005 + PL-009 (`.10`, `.16`); `.4` reconciliation flock → PL-002a, PL-006, PL-011 (`.4`, `.11`, `.20`); `.17` post-ready Cat 0 → PL-009, PL-010 (`.16`, `.19`); `.62` Cat 0 row → PL-010 (`.19`); `.22` detector panic recovery → PL-018a (`.30`); `.36` daemon-side verdict-executor → PL-018a (`.30`).
- ← **ON** (`hk-sx9r`): `.10`, `.12` pause/upgrade between-task + reconciliation carve-out → PL-009 (`.16`); `.25` upgrade-intent durable marker → PL-005 (`.10`); `.33` step 1 stop pulling tasks → PL-013 (`.23`); `.40` step 7 orchestrator exits → PL-011 (`.20`); `.46`–`.48` RTO target/criteria/measurement → PL-009 (`.16`) + DaemonStatus ENUM (`.49`); `.52`–`.53` health/heartbeat → ENUM (`.49`); `.57` multi-daemon commands → PL-028 (`.43`); `.58`/`.60` multi-tenancy/distributed-tracing post-MVH → PL-001 (`.2`); `.16` per-command supervision → PL-018a (`.30`); `.49` post-panic forensic → PL-018a (`.30`); `.63` `harmonik status` pause-reason → ENUM (`.49`).
- ← **HC** (`hk-8i31`): `.20` orphan-reconnect retry vs daemon_not_ready → PL-003b + PL-009b (`.8`, `.18`); `.51`, `.52`, `.7` subprocess-is-child / launch-fail-fast / wire protocol → PL-001 (`.2`).

No PL INCLUDE bead has incoming dependents from clusters whose contributing beads are EXCLUDED in PL — incoming edges land on already-INCLUDE beads.

## 6. Open questions / ambiguities

- **OQ-A. PL-008 + PL-008a forward:on edges.** PL-008 (`.14`) and PL-008a (`.15`) emit ~17 forward:on-* edges per F-pilot-PL-4 / `hk-ahvq.46`. Bootstrap needs ON exit codes 5/6/7/8. Does cluster-G ON contribute the catalog now, or does PL bootstrap ship with a hardcoded 4-code subset and the full ON-008/ON-014/ON-027 list arrives later? Same question for PL-005 step 8a marker semantics (ON-020a / ON-030a).
- **OQ-B. Orphan sweep on first start.** `hk-8mup.11` (PL-006) drags 3 WM lease-lock beads + 1 BI idempotency-key bead. On first start the sweep finds nothing, so the WM/BI implementations only need empty-state correctness. Should bootstrap pin a "no-op orphan sweep" sub-slice (zero-state branch only), or implement full sweep semantics? Affects WM cluster sizing.
- **OQ-C. PL-014a (`.25`) concurrency ceiling.** Excluded above as deferrable to a hardcoded constant for v0. If WM cluster's lease-lock contention test wants a real ceiling sensor, this would need to move IN.
- **OQ-D. §4.7 ntm scope.** Including only PL-021a (.35) means real twin spawn happens through ntm but the *constraint* rules (PL-021/022/023) — "ntm consumes process/tmux only", "MUST NOT consume workflow-semantic features", "handler contract is the ntm boundary" — aren't enforced. This is a posture question: do we admit a v0 where ntm could legally drift outside its scope and rely on lint sensors landing later?
- **OQ-E. Harness `.55` (composition-root + panic-barrier lint).** Excluded because its sensor targets (.29/.32) are excluded. But its panic-barrier dimension covers `.30` which IS in. Worth a lighter "panic-barrier-only" lint variant in bootstrap, or drop entirely?
- **OQ-F. PL ↔ ON cycle-break NOTE consequences.** PL intentionally omits ON from `depends-on` per §9.3 NOTE, but Section-5 inbound edges from ON to PL `.10`/`.16`/`.19`/`.20`/`.30`/`.43`/`.49` are dense. Pass-3 labelling needs a way to declare "PL depended on by ON without PL depending on ON" and not collapse both into a single cluster boundary marker.
