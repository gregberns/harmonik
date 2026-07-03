# Research — `cognition-loop.md` CL-090 — Component C4 (+ C6/C7 notes)

> Pass 3 (`research`) of the `credfence` spec work. Covers the unified spend meter (Pi turns + daemon `claude` sessions), finite default, and max-runs ceiling (C4), plus the informative model-tier (C6) and retry-budget/dry-run (C7) cross-refs that land in `cognition-loop.md`. Grounded in the incident assessment §"Genuinely lost" #1/#6/#7 and the live spec + Pi code (verified 2026-05-31). Planning artifact; does not modify `specs/`.

## Research questions

- **RQ1.** What does CL-090 say today, and what is the exact gap (meter scope, default, missing max-runs)?
- **RQ2.** How is the budget implemented in `budget.ts`/`index.ts` today, and where is the `Infinity` default?
- **RQ3 (OQ-1, the central design question).** How does a daemon-spawned `claude` session's approximate USD reach Pi's meter? What event surface already exists to carry cost?
- **RQ4 (OQ-3).** Should max-runs reset on the same day boundary as per-day-USD, or be a per-session lifetime cap?
- **RQ5 (OQ-6).** Where does the per-model USD rate table live, and how kept current?
- **RQ6.** Does `LoopStatus` already carry `budget-paused`? Any §6 change needed?

## Findings

### F1 — CL-090 today: right home, wrong meter scope, inert default, no max-runs (RQ1)

`specs/cognition-loop.md:168` (§4.11 CL-090, "Per-day budget kill-switch"): "Loop MUST enforce per-day USD cap (`--budget-usd-per-day`). Track cumulative **substrate spend**; stop dispatching new turns when cap exceeded. Emits `flywheel_budget_exhausted{spent_usd, cap_usd, model}`; SHOULD pause in `budget-paused` pending operator intervention. Day boundary = local-midnight in operator TZ at v0.1. Reset NOT automatic; operator MUST `harmonik supervise resume`."

Gaps vs the desired contract:
1. **Scope is "substrate spend" = Pi's own turns only.** It does not mention daemon-spawned `claude` implementer/reviewer sessions — the literal root-cause spend (assessment §"Genuinely lost" #1: "the 26+ implementer sessions that burned the credit are invisible to it").
2. **No finite default stated.** CL-090 names the knob but not a default value; the code defaults to `Infinity` (F2).
3. **No max-runs ceiling.** Only a per-day-USD cap; no count backstop against cost-estimate error.
4. **Day-reset and resume semantics are already correct** and reusable as-is (local-midnight, non-automatic, `supervise resume`). C4 keeps these.

CL-090 is confirmed as the right home (constraint, problem-space §4: "extend it, don't fork it"). The §2.1 scope note and the CL-090 text both need the "two layers" broadening.

### F2 — `budget.ts`/`index.ts`: meters only Pi turns; `?? Infinity` default in two places (RQ2)

- `.pi/extensions/flywheel/budget.ts` — `BudgetTracker.recordSpend(record: SpendRecord)` where `SpendRecord = { turnUsd, model }`. The tracker only ever sees **Pi turn** spend; nothing feeds it daemon `claude` cost. The header comment documents graceful-downgrade (80% Opus->Sonnet, 90% Sonnet->Haiku, 100% halt -> `flywheel_budget_exhausted` -> `budget-paused`) — that ladder is Pi-tier-only and irrelevant to daemon claude sessions (which are not Pi tiers).
- `budget.ts` constructor: `limitUsd: config.limitUsd ?? Infinity` — inert default #1.
- `.pi/extensions/flywheel/index.ts:62` (activate): `const budgetUsdPerDay = process.env["FLYWHEEL_BUDGET_USD_PER_DAY"] ? parseFloat(...) : Infinity;` — inert default #2. **Both must flip to a finite default** (C4 / hk-60csa). The `FLYWHEEL_BUDGET_USD_PER_DAY` env knob already exists and is the natural place to keep the override (set to `unlimited`/empty for opt-out).
- `recordSpend` emits `flywheel_budget_exhausted{spent_usd, cap_usd, model}` once at >=1.0 ratio (matches CL-090's event shape). The event surface is already there; what is missing is (a) the daemon-claude spend feeding the meter and (b) a non-Infinity cap so the event can fire.

### F3 — OQ-1 RESOLVED by research: the daemon already emits run-lifecycle + budget events Pi can sum (RQ3) — DECISIVE

The cost-attribution mechanism does NOT need a new bespoke event. `event-model.md §8` already defines a usable surface:

- `event-model.md §8.4.2` **`budget_accrual`** (class L, lossy) — payload `{run_id, session_id, chunk_index?, cost_units, cost_basis}`, producer "handler (via daemon watcher)", consumers "improvement-loop, observability". This is a **per-run, per-chunk cost-accrual event the daemon already emits** with `cost_units` + `cost_basis`. Pi's unified meter can subscribe to `budget_accrual` (already in the `harmonik subscribe` stream Pi consumes via `bridge.ts`) and sum daemon claude cost alongside its own turn cost.
- `event-model.md §8.1.1/8.1.2` **`run_started`/`run_completed`** (class F) — carry `run_id`, and `run_completed` carries `summary?`. A coarser fallback: Pi estimates per-run cost from `run_started`->`run_completed` + a per-model rate table (F5) if `budget_accrual` is too granular/lossy.
- `event-model.md §8.4.3` **`budget_exhausted`** (class O) — payload `{run_id, session_id?, budget_ref, attempted_dispatch_cost}`, producer "agent-runner (S04)". This is the daemon-side per-run exhaustion event, distinct from Pi's `flywheel_budget_exhausted`. Important for C5 wiring (see handler-pause findings).

**Design recommendation (OQ-1):** Pi's unified meter sums (a) its own `turnUsd` per turn AND (b) daemon claude cost from `budget_accrual` events (preferred — already carries `cost_units`/`cost_basis`), with `run_started`->`run_completed` + rate-table estimation as the documented fallback when accrual events are absent/lossy. This is **lower-cost than the problem-space's lean** (which proposed a NEW `run_cost` event) — research found the accrual event already exists, so C4 should reuse it rather than mint a new type. **Flag for design: confirm `budget_accrual`'s `cost_basis` is USD-convertible; if `cost_units` is tokens, the rate table (F5) converts.**

> CAVEAT: `budget_accrual` is class L (lossy-tail-ok per `event-model.md` durability table, line ~284: "high-cardinality granular events (chunk, accrual, metric) are lossy-tail-ok"). A lossy meter could *under*-count and let spend slip past the cap. **Governance-conservatism mitigation:** pair the USD meter with the **max-runs** ceiling (F4) as a deterministic, loss-proof backstop — max-runs counts `run_started` events (class F, fsync-backed, not lossy). This is exactly why the problem-space added max-runs "as a simpler backstop against cost-estimate error" — research confirms it is also a backstop against *event loss*. **Strong design point.**

### F4 — Max-runs reset semantics: same day boundary as USD (RQ4, OQ-3)

OQ-3 leans: max-runs is a per-day ceiling resetting at local-midnight with the USD cap. Research supports it: CL-090's day boundary is already "local-midnight in operator TZ" with non-automatic reset; a single `rolloverIfNewDay()` (already in `budget.ts`) resets `spentUsd` on a new `dayKey`. Adding a `runsToday` counter to the same `BudgetState` and resetting it in the same rollover gives ONE reset semantics, ONE resume action. A separate per-session lifetime cap would invite two reset rules and two operator mental models. **Lean stands; design: extend `BudgetState` with `runsToday`, increment on each daemon `run_started`, reset in `rolloverIfNewDay`.**

### F5 — Rate table: small operator-overridable config in the flywheel extension (RQ5, OQ-6)

OQ-6 leans: a small operator-overridable rate table in the flywheel extension config, defaulting to published Anthropic per-MTok rates. Research findings:
- `router.ts` already hardcodes the three model IDs per tier (`claude-opus-4-7-20260219`, `claude-sonnet-4-6-20251022`, `claude-haiku-4-5-20251001`) — a rate table keyed by these same model IDs is the natural companion and lives in the same extension.
- `operator-nfr.md §4.1 ON-004` config-inventory obligation already requires enumerating "budget warning threshold (control-points.md §4.5)" as a knob with default + precedence — the rate table and the per-day cap and max-runs all become ON-004 inventory entries. C4's spec text should reference ON-004 rather than invent a new config surface.
- Stale rates are a **governance-conservatism** issue, not a correctness one (over-estimating cost halts earlier = safe). The spec should say rates are operator-overridable and conservatism is the design bias.

### F6 — `LoopStatus` already carries `budget-paused`; no §6 type change (RQ6)

`cognition-loop.md:191` §6 Types table: `LoopStatus (§4.10) | mechanism | {starting, ready, paused, budget-paused, circuit-tripped, draining, stopped}`. `budget-paused` is already present. **No §6 change needed** (problem-space §6 predicted this; confirmed). C4 reuses the existing state. `cognition-loop.md:213` §9 already cross-refs `operator-nfr.md §4.3` for surfacing `budget-paused` — reuse, don't add.

## Patterns to follow

- **Extend CL-090, do not fork** (constraint). Broaden "substrate spend" -> "Pi turns + daemon `claude` sessions"; add finite default + max-runs; keep day-boundary/resume semantics.
- **Reuse the existing event surface** (`budget_accrual` §8.4.2; `run_started`/`run_completed` §8.1) for cost attribution — do NOT mint a new `run_cost` event (F3 supersedes the problem-space lean).
- **Max-runs counts class-F `run_started`** (loss-proof backstop), USD meter sums class-L `budget_accrual` (precise but lossy) — two ceilings, complementary failure modes.
- **One reset semantics** (local-midnight, both ceilings) (F4).
- **Rate table + cap + max-runs are ON-004 config-inventory entries** (F5), not a new config surface.
- **Conservatism bias:** over-estimate -> halt earlier -> safe.

## Risks / conflicts

- **R1 (event-loss under-count).** `budget_accrual` is lossy (class L). A USD-only meter can under-count past the cap. **Mitigated** by the class-F max-runs backstop (F3 caveat / F4). Design must state max-runs is the loss-proof ceiling.
- **R2 (cost_basis units).** `budget_accrual.cost_units` may be tokens, not USD. The rate table converts; design must pin the conversion and the `cost_basis` enum values.
- **R3 (Pi vs daemon process boundary).** The meter lives in Pi (`budget.ts`); daemon claude runs in the daemon process. Attribution is cross-process and event-mediated (F3). This is acceptable (Pi already consumes the daemon's event stream via `bridge.ts`/`harmonik subscribe`), but it means the meter is **eventually-consistent**, not synchronous — a burst of daemon spawns between accrual events could momentarily exceed the cap before the meter catches up. Max-runs (synchronous-ish, on `run_started`) bounds the burst. **Flag for design: state the eventual-consistency window and that max-runs bounds it.**
- **R4 (default-flip is a behavior change).** Infinity->finite is a deliberate breaking change (the prior behavior was the vulnerability) per problem-space §4. Operators who opt into unlimited still can. Changelog must call this out as intentional, not a regression.

## Open questions carried to design

- OQ-1 (cost attribution) — **research resolves toward reuse of `budget_accrual`** (F3); design confirms `cost_basis` units + conversion.
- OQ-3 (max-runs reset) — confirmed same-boundary-as-USD (F4); lean stands.
- OQ-6 (rate table) — confirmed extension-local + ON-004 entry (F5); lean stands.
- NEW: eventual-consistency window of the cross-process meter (R3) — design states the window and the max-runs bound.
