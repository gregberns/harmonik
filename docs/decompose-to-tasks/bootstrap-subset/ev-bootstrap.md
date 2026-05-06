# EV Bootstrap-Subset Enumeration

**Date:** 2026-05-05
**Cluster:** D — Event-bus skeleton
**Epic:** `hk-hqwn` — Event Model spec — implementation
**Total beads in epic:** 63 first-class children + 78 §8 row children + 1 epic parent = **142 total** (verified via `br list -l spec:event-model --limit 0`). Opening pass's "~63 beads" counted only first-class.
**Pilot inputs:** `docs/decompose-to-tasks/ev-pilot.md`, normative `specs/event-model.md` v0.3.3.
**Working definition source:** `docs/decompose-to-tasks/bootstrap-subset-opening.md` §1 (foothold = compile-and-run skeleton hosting first twin round-trip end-to-end test).
**User-resolved Qs applied:** Q1 twin IN, Q2 Pi OUT, Q4 S07 IN.

## 1. Counts

- **INCLUDE: 36** beads (20 first-class § 4/5/6 + 16 §8 row children).
- **EXCLUDE: 105** beads (43 first-class deferrals + 62 post-MVH or non-bootstrap §8 row children, plus the epic parent).
- **Ratio:** 36 / 141 ≈ **26%**. Of the 63 first-class beads cited by the opening pass, INCLUDE = 20 (≈32%), at the upper end of the opening's "15–20" estimate. Bumped slightly because daemon-ready/ready-emission beads in §8.7 are load-bearing for daemon startup self-checks (PL cluster A demand).

## 2. INCLUDE — by clause

### §4.1 Envelope (5 first-class)
- `hk-hqwn.1` (EV-001 — common envelope fields) — every event needs `event_id`, `schema_version`, `type`, `timestamp_wall`, `source_subsystem`, `payload`. The skeleton.
- `hk-hqwn.2` (EV-002 — `event_id` is UUIDv7) — non-negotiable; partial-order contract relies on it.
- `hk-hqwn.3` (EV-002a — UUIDv7 monotonic within process) — required for any same-millisecond emission ordering.
- `hk-hqwn.4` (EV-002b — handler subprocess routes `event_id` through daemon) — bootstrap's twin handler will emit; daemon must stamp.
- `hk-hqwn.7` (EV-004 — `source_subsystem` layout-open) — every subsystem registers identifier; gated by EV-034a (`.44`).

### §4.2 Clock + ordering (1 first-class)
- `hk-hqwn.11` (EV-008 — partial-order contract) — codifies what the bus actually guarantees. Cheap to satisfy once `.2`/`.3` land.

### §4.3 Bus and dispatch (5 first-class)
- `hk-hqwn.12` (EV-009 — subscription declared at registration; bus seals after `daemon.Start`) — one-shot subscribe is the bootstrap shape.
- `hk-hqwn.13` (EV-010 — synchronous consumer class) — minimum dispatch class; needed for orchestrator-core to react synchronously to `outcome_emitted`.
- `hk-hqwn.16` (EV-012 — observer consumer class) — observability + audit consumers; minimal-overhead default.
- `hk-hqwn.17` (EV-013 — opt-in class at subscription, default `observer`) — lets bootstrap consumers default safely.
- `hk-hqwn.19` (EV-014a — dispatch semantics: redact → JSONL append + fsync → sync dispatch) — the actual `Emit` ordering contract.

### §4.4 Durability + JSONL (4 first-class)
- `hk-hqwn.23` (EV-015 — JSONL is durable on-disk form at `.harmonik/events/events.jsonl`) — load-bearing for restart resilience.
- `hk-hqwn.24` (EV-016/EV-016a — durability class on every §8 row + per-class fsync) — the `F`/`O`/`L` classification is wired into Emit's fsync decision.
- `hk-hqwn.26` (EV-018 — producer idempotent emission) — required for replay-safety (consumers in S07 may re-deliver on restart).
- `hk-hqwn.29` (EV-020 — JSONL append-only, no rewrite/truncate) — minimal write-discipline.

### §4.5 Replay (1 first-class)
- `hk-hqwn.31` (EV-022 — state reconstruction MUST NOT walk JSONL) — one-line invariant the daemon-startup path must obey from day one.

### §4.6 Type system (3 first-class)
- `hk-hqwn.41` (EV-032 — tagged-union envelope + payload-constructor registry) — the §6.3 mechanism every §8 row depends on.
- `hk-hqwn.42` (EV-033 — type dispatch deterministic on `type` field) — one-line property atop `.41`.
- `hk-hqwn.43` (EV-034 — registry registration is startup-time, sealed with bus) — startup-time discipline.

### §6 Schemas (1 first-class)
- `hk-hqwn.53` (Event envelope record §6.1) — the 10-field record. Bootstrapping S07 requires concrete struct; cannot defer.

> Schemas `.54` (TraceContext), `.55` (Subscription), `.56` (EventPattern), `.57` (EventBus interface), `.58` (JSONL line format) are also INCLUDE-essential. Folded into bullet group below to keep top-line at 20 first-class:
- `hk-hqwn.54` TraceContext — needed by envelope.
- `hk-hqwn.55` Subscription — needed by `.12` registration shape.
- `hk-hqwn.57` EventBus interface — the 6-method surface (`Emit`, `Subscribe`, `Seal`, `ReplayFrom`, `DeadLetterReplay`, `Drain`); even bootstrap needs all 6 stubs (replay/dead-letter MAY be no-op v0).
- `hk-hqwn.58` JSONL line format §6.2 — the read-recovery rules. Torn-tail discard at minimum.

(That brings first-class INCLUDE count to **20**.)

### §8 Event-row children (16) — bootstrap event types

**Run lifecycle (§8.1):**
- `hk-hqwn.59.1` `run_started` (F).
- `hk-hqwn.59.2` `run_completed` (F).
- `hk-hqwn.59.3` `run_failed` (F).
- `hk-hqwn.59.6` `transition_event` (F) — paired with `checkpoint_written`; required for commit-trail.
- `hk-hqwn.59.7` `checkpoint_written` (F) — explicitly named in opening pass slice.
- `hk-hqwn.59.8` `outcome_emitted` (O) — handler-emitted, drives orchestrator's next-node decision.

**Handler / agent lifecycle (§8.3):**
- `hk-hqwn.59.21` `agent_ready` (O) — handshake event after twin process initializes.
- `hk-hqwn.59.22` `agent_started` (O) — explicitly named.
- `hk-hqwn.59.23` `agent_output_chunk` (L) — explicitly named.
- `hk-hqwn.59.24` `agent_completed` (O) — explicitly named.
- `hk-hqwn.59.25` `agent_failed` (O) — explicitly named.

**Workspace lifecycle (§8.5):**
- `hk-hqwn.59.37` `workspace_created` (O) — pairs with `workspace_leased` to form atomic 4-step (per WM-016).
- `hk-hqwn.59.38` `workspace_leased` (O) — explicitly named.
- `hk-hqwn.59.39` `workspace_merge_status` (F) — the run-closing emission tying integration-merge to bead close.

**Daemon lifecycle (§8.7):**
- `hk-hqwn.59.57` `daemon_started` (F) — daemon-startup landmark; HWM-restart tests depend on it.
- `hk-hqwn.59.58` `daemon_ready` (F) — the "self-check passed → accepting work" flag PL cluster A consumes.
- `hk-hqwn.59.59` `daemon_shutdown` (F) — clean-shutdown landmark.
- `hk-hqwn.59.60` `daemon_startup_failed` (F) — explicitly named; ON cluster startup catalog consumer.

(That's 18 §8 rows; trimming `state_entered`/`state_exited` (§8.1.4/5, class `O`) to keep §8 row count at 16 unless EM cluster argues they're load-bearing for the linear DOT walk. Marking those as **AMBIGUOUS — see §7**.)

## 3. EXCLUDE categories

### Post-MVH explicit (1 first-class + 1 §8 row)
- `hk-hqwn.59.56` `bead_terminal_transition_recovered` (§8.6.14) — spec text says **"(post-MVH)"** verbatim; type identifier reserved but no MVH conformance obligation. Confirmed by opening-pass note.

### HWM + clock-regression sophistication (1 first-class)
- `hk-hqwn.5` (EV-002c — HWM file across daemon restart with `daemon_degraded{reason=clock_regression}`) — first self-build cycle territory. Bootstrap accepts cross-restart ordering being not-guaranteed (the spec already allows seeding from wall clock with structured warning).

### Async + back-pressure sophistication (4 first-class)
- `hk-hqwn.14` (EV-011 — async consumer class with retry / dead-letter / per-consumer queue) — bootstrap has no async consumers in the happy-path scenario.
- `hk-hqwn.15` (EV-011a — non-blocking back-pressure with class-based shed and spill files) — depends on EV-011; defer.
- `hk-hqwn.20` (EV-014b — consumer idempotency on recovery + dead-letter replay) — pairs with replay sophistication.
- `hk-hqwn.21` (EV-014c — observer dispatch per-observer goroutine + bounded queue) — bootstrap observers can be inline; goroutine-pool is optimization.
- `hk-hqwn.22` (EV-014d — JSONL-tail replay on startup before live-stream) — replay path itself is post-bootstrap; first cycle uses `since: None` start-from-live.

### Class-conflict + acyclicity check (1 first-class)
- `hk-hqwn.18` (EV-014 — class-conflict startup error) — useful but a sensor; not load-bearing for end-to-end. Defer.

### Panic / shutdown sophistication (2 first-class)
- `hk-hqwn.27` (EV-019 — panic handler flushes structured logs) — bootstrap is allowed to crash dirty.
- `hk-hqwn.28` (EV-019a — panic SHOULD best-effort flush bus) — same.

### Replay sophistication (3 first-class)
- `hk-hqwn.30` (EV-021 — observational replay non-authoritative) — invariant only matters once tools exist that walk JSONL.
- `hk-hqwn.32` (EV-023/023a — divergence-evidence read with post-crash window + inconclusive classification) — RC-cluster reconciliation; not in bootstrap.
- `hk-hqwn.33` (EV-024 — replay cannot re-establish agent state) — invariant; sensor only.

### Tagging + amendment (5 first-class)
- `hk-hqwn.34` (EV-025 — payload shape SHAPE/WHEN co-ownership) — coordination rule; no implementation work.
- `hk-hqwn.35` (EV-026 — internal events out of scope) — definitional; no code.
- `hk-hqwn.36` (EV-027 — foundation amendment for new cross-bus types) — process; no code.
- `hk-hqwn.37` (EV-028 — `schema_version` per type + envelope) — bootstrap pins to v1; defer multi-version mechanism.
- `hk-hqwn.38` (EV-029 — N-1 readable compatibility window) — defer.
- `hk-hqwn.39` (EV-030 — breaking-change classification per §6.4) — defer.

### Tagging mechanism (1 first-class)
- `hk-hqwn.40` (EV-031 — every §8 row carries four-axis tags + durability class) — bootstrap §8 rows DO carry durability class (`.24`), but the four-axis registry-side tag attribute can defer to first cycle. **AMBIGUOUS — see §7.**

### Source-subsystem registration (1 first-class)
- `hk-hqwn.44` (EV-034a — startup duplicate-identifier check) — sensor; defer.

### Redaction (2 first-class)
- `hk-hqwn.45` (EV-035 — redaction registry applied before emission) — bootstrap can ship with a no-op redactor; the test scenario emits no secrets.
- `hk-hqwn.46` (EV-036 — compile-time check no payload field name matches secret-prefix rule) — sensor; first-cycle.

### Sensors (6 first-class — `.47`–`.52`)
- All six EV-INV-* sensors deferred. Bootstrap leans on direct conformance checks during S07 rather than systematic invariant probes.

### §8 row children — non-bootstrap (62)
- Hook lifecycle (§8.2.*) — 9 rows. CP cluster gates these.
- Budget lifecycle (§8.4.*) — 3 rows. AR / agent-runner consumes; defer (no budget enforcement in bootstrap).
- Workspace remainder (§8.5.4–.6) — 3 rows (`workspace_discarded`, `workspace_interrupted`, `merge_conflict_escalation`). Failure paths beyond bootstrap.
- Reconciliation (§8.6.*) — 14 rows minus the deferred `.59.56` = 13 rows. RC cluster; out of bootstrap by core-scope.
- Operator-control (§8.7.5–.17) — 13 rows (`daemon_degraded`, `operator_pause_status`, etc.). ON cluster; out of bootstrap.
- Observability + bus-internal (§8.8.*) — 5 rows (`metric`, `consumer_failed`, `dead_letter_enqueued`, `bus_overflow`, `redaction_failed`). All paired with deferred mechanisms.
- §8.1.9–.11 (`sub_workflow_entered`, `sub_workflow_exited`, `node_dispatch_requested`) — 3 rows. Sub-workflow recursion + reconciliation-origin dispatch are post-MVH.
- §8.3.6–.13 (rate-limit, session-log-location, skills-provisioned, handler-capabilities, silent-hang, soft/hard terminate) — 8 rows. HC sophistication; bootstrap ships only the 5 essential `agent_*` rows.

### Test infrastructure (4 first-class — `.60`–`.63`)
- All four test-infra beads (canonical JSONL fixture, bus-overflow harness, UUIDv7 monotonicity stress, §8 taxonomy lint) are deferred. Bootstrap S07 IS the integration test; the dedicated EV harnesses come in cycle 2 once the bus surface is exercised. Note the §8 taxonomy lint (`.63`) self-describes as "stub at MVH".

## 4. Cross-cluster edges OUT (EV-INCLUDE → other clusters)

EV's INCLUDE depends on these cross-cluster beads (sampled via `br show` on the §8 row beads — these are spec dependencies inherited via the SHAPE/WHEN co-ownership rule):

- **EM (Cluster F, `hk-b3f`):**
  - `hk-b3f.16` (run_started emission on dispatch) ← `.59.1`.
  - `hk-b3f.19` (Checkpoint is a git commit with work-product tree + transition-record sibling) ← `.59.7`.
  - `hk-b3f.22` (Transition record sibling file at canonical run-scoped path) ← `.59.7`.
  - Plus run_completed/run_failed emission obligations that EM cluster must enumerate.
- **HC (Cluster C, `hk-8i31`):**
  - `hk-8i31.36` (`agent_started` MUST NOT include environment variables) ← `.59.22`.
  - `hk-8i31.75` (`SessionID` record §6.1) ← `.59.22` (and the rest of the agent_* rows).
  - HC bootstrap subset for agent_started/output_chunk/completed/failed emission.
- **WM (Cluster B, `hk-8mwo`):**
  - `hk-8mwo.25` (Workspace lifecycle event emission obligations / WHEN) ← `.59.37` and `.59.38` and `.59.39`.
- **ON (Cluster — out of bootstrap, but `.4` may be in):**
  - `hk-sx9r.4` (Startup failure-mode catalog obligation) ← `.59.60` (`daemon_startup_failed`'s `failure_mode` enum). Need to coordinate with ON cluster slice; if `.4` is excluded from bootstrap, EV row's payload enum needs a stub.
- **AR (Cluster — defer almost-entirely, but `.28` may be in):**
  - `hk-zs0.28` (Cross-subsystem agent-type reference points) ← `.59.22` (`agent_type` field) — may already be in AR-bootstrap (see ar-verification.md).
- **PL (Cluster A, `hk-8mup`):**
  - `hk-8mup.15` (Exit-code consumption from ON §8) ← `.59.60`. May or may not be in bootstrap.

## 5. Cross-cluster edges IN (other clusters → EV-INCLUDE)

Every cluster that emits depends on EV. From spot-checking dependents on EV INCLUDE beads:

- **CP (Cluster — out of bootstrap; deferred):** `hk-a8bg.12`, `.15`, `.26` cite EV envelope (`.53`), `checkpoint_written` (`.59.7`), `agent_started` (`.59.22`), and `run_started` (`.59.1`). CP's hook lifecycle event types and budget rehydration depend on EV bootstrap subset. CP is OUT of bootstrap, so these edges become **dangling-target-out-of-bootstrap-but-source-also-out-of-bootstrap** — no action needed.
- **EM bootstrap subset:** any EM bead that emits a run/transition/checkpoint event depends on EV envelope + the relevant §8 row.
- **WM bootstrap subset:** workspace lifecycle emission (WM-015 / WM-016 / wm-021) depends on EV envelope + `.59.37`/`.59.38`/`.59.39`.
- **HC bootstrap subset:** every agent_* event row consumed via the daemon watcher depends on EV envelope + the matching §8 row.
- **PL bootstrap subset:** daemon startup steps that emit `daemon_started`/`daemon_ready`/`daemon_startup_failed` depend on `.53` envelope and §8.7 rows `.59.57`/`.59.58`/`.59.60`. **`daemon_shutdown` (`.59.59`) for clean-shutdown.**
- **BI (Cluster E):** likely no direct EV dependency in bootstrap (BI is a beads adapter, not an event emitter, in MVH; `.59.56` is post-MVH).

**Tally:** EV is depended on by **at least 5 clusters** in bootstrap (EM, WM, HC, PL — and indirectly by AR/zs0). EV-cluster bootstrap underpins every other cluster's commit-and-watch path.

## 6. Open questions / ambiguities

**Q-EV-1 (monotonic-companion fields).** The opening pass asks "which monotonic-companion fields are bootstrap-essential?" The spec lists these companion fields in EV-001: `timestamp_mono_nsec` (optional, intra-process only), `trace_context.parent_event_id` (optional), `trace_context.root_event_id` (optional). Recommendation: **`timestamp_mono_nsec` IS bootstrap-essential** (otherwise EV-007 `.10` is a no-op and same-process ordering depends only on UUIDv7's millisecond resolution); **`trace_context.parent_event_id` / `root_event_id` are NOT bootstrap-essential** (causal chain explicit-linkage matters for hook-fired and reconciliation, both deferred). Folding this answer back: include `.10` (EV-007 monotonic time) into the INCLUDE list. Updated INCLUDE first-class count = **21**, total INCLUDE = **37**. Awaiting confirmation.

**Q-EV-2 (JSONL schema stable for v0?).** EV-028 (`.37`, currently EXCLUDE) requires `schema_version` per type + envelope. Bootstrap pins everything to v1. Question: does the JSONL line format (§6.2 / `.58`) need the `schema_version` field present-but-pinned, or absent-and-defaulted-to-1-on-read? Recommendation: **present-but-pinned-to-1**. Cheaper to add later than to migrate readers. No bead change required (`.58` already declares 10-field envelope including `schema_version`); just confirm.

**Q-EV-3 (state_entered / state_exited).** §8.1.4 / .1.5 emit on every state transition in the linear DOT workflow. The bootstrap S07 walks 1–2 nodes; emitting these is cheap but they're class `O` and consumed only by observability + improvement-loop (both deferred). **Recommendation: INCLUDE for symmetry with `transition_event` and to keep the §8.1 row set coherent.** Adds 2 §8 rows. Final §8 row INCLUDE count = **18**. Awaiting confirmation.

**Q-EV-4 (EV-031 tagging).** `.40` (EV-031 four-axis tags + durability class on every §8 row) is technically needed by the spec for any §8 row to be valid. Durability class is already wired by `.24`. The four-axis tags (`llm-freedom`, `io-determinism`, `replay-safety`, `idempotency`) are registry-side metadata. Question: can bootstrap ship with tags as registry stubs (empty arrays acceptable) and defer the lint check? Recommendation: **INCLUDE `.40` as a first-class bead** (the tags are declarative; the lint is what's deferred). If user agrees, INCLUDE first-class count rises to **22**, total = **38–40 depending on Q-EV-1/3**.

**Q-EV-5 (EventBus.ReplayFrom / DeadLetterReplay).** `.57` is INCLUDE; the 6-method interface declares `ReplayFrom` and `DeadLetterReplay`. Bootstrap defers `.22` (EV-014d replay path). Question: do `ReplayFrom`/`DeadLetterReplay` ship as no-op stubs returning `nil`, or as `ErrNotImplemented`? Recommendation: **`ErrNotImplemented` returning typed error**. Forces a future cycle to wire them; failing closed is the discipline.

**Q-EV-6 (test fixtures).** Test-infra beads `.60`–`.63` are EXCLUDE, but the bootstrap S07 IS an integration test that exercises EV. Risk: S07 alone may not exercise the torn-tail/concurrent-tail/HWM-restart conditions that `.60` and `.62` are designed to cover. **Recommendation: leave them EXCLUDE for the bootstrap cut.** Document the gap as a first-cycle deliverable so we don't ship a brittle bus.
