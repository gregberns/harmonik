# Research — `handler-pause.md` + `control-points.md` — Component C5 (hard-halt wiring + `budget_scope`)

> Pass 3 (`research`) of the `credfence` spec work. Covers the budget-exhaustion hard-halt path (Pi meter trips -> event -> existing handler-pause consumer pauses the claude handler type) and the `budget_scope` control-points amendment HP-012 depends on. Grounded in incident assessment §"Genuinely lost" #1 (the halt that never fired) and the live specs (verified 2026-05-31). Planning artifact; does not modify `specs/`.

## Research questions

- **RQ1.** What does HP-012 require today, and what consumer already exists to pause the claude handler on budget exhaustion?
- **RQ2.** What is the exact `budget_scope` gap, and what does `control-points.md §4.5` (the Budget primitive) look like — is there an existing `scope` field to extend?
- **RQ3.** Which event trips the consumer, and does the Pi-emitted `flywheel_budget_exhausted` match, or is there an impedance mismatch with the daemon-side `budget_exhausted`?
- **RQ4.** Is the control-points amendment in-scope for credfence (OQ-4), and is it additive (no invariant break)?
- **RQ5.** What is the end-to-end exhaustion path and where does `supervise resume` clear it?

## Findings

### F1 — HP-012 + the consumer already exist; only the trip-source and the `budget_scope` field are missing (RQ1)

- `handler-pause.md:224` **HP-012 — Account-budget exhaustion trip:** "A handler type MUST be paused immediately (no hysteresis) when a `budget_exhausted` failure is observed with `budget_scope = handler-account`. Per-run budget exhaustion (per-node budget) is NOT handler-fatal and MUST NOT trip a handler pause." — The normative controller behavior is **already specified**. credfence does NOT rewrite HP-012; it confirms it and supplies the event + field that make it fire.
- `handler-pause.md:217` (§8 classification table): "`budget_exhausted` | Conditional | Handler-fatal only when the budget is per-handler-account (session-token cap, daily-quota). Per-run budget exhaustion is per-bead." — The **daily-quota** case is exactly credfence's per-day cap. So the unified meter's exhaustion maps cleanly to `budget_scope=handler-account`.
- The consumer the problem-space names (`handler-pause-policy-budget-exhausted-claude-code`, `internal/daemon/composition_registry_hkndysh.go`) is the registered policy that reacts to HP-012's classification and pauses the `claude` handler type. credfence reuses it (constraint, problem-space §4: "do not invent a parallel halt path"). The two halves credfence supplies: (1) the meter emits the right event with `budget_scope=handler-account` (C4 seam), (2) the `budget_scope` field exists in control-points so the policy can carry it.

### F2 — `control-points.md §4.5` Budget primitive has a `scope` field already — but its enum lacks `handler-account` (RQ2) — KEY FINDING

The Budget primitive is NOT a blank slate. `control-points.md`:
- `CP-022` (line ~226): "A `Budget` MUST declare: (a) `resource in {tokens, wall_clock_seconds, iterations}`, (b) **`scope in {per_role, per_run, per_state}`**, (c) `limit`, (d) `warning_threshold`."
- `CP-005` per-Kind table (line ~110): Budget trigger = "Dispatch attempt; per-chunk accrual; threshold cross"; evaluator input = "Counter state, proposed dispatch cost, per-chunk delta"; outcome = `{admit, warn, deny}`.
- `CP-023` (line ~234): the runner checks remaining allowance AT DISPATCH; on would-exceed it emits `budget_exhausted` per `event-model.md §8.4` and DENIES (handler not launched); failure class `budget_exhausted` per `execution-model.md §8.5`.

**So there IS an existing `scope` field** (`per_role | per_run | per_state`), but its enum does **not** include `handler-account` — and HP-012 specifically requires `budget_scope = handler-account`. This is the precise gap recorded in `handler-pause.md:411` (§13 deferred #3): "`budget_exhausted{handler-account}` requires `budget_scope` on the budget-point policy. That field does not exist in control-points.md §4.5. Deferred to control-points amendment."

**Reconciliation of naming:** HP-012 says `budget_scope`; CP-022 says `scope`. The amendment must either (a) add `handler-account` as a new value to CP-022's existing `scope` enum (making `scope in {per_role, per_run, per_state, handler_account}`), OR (b) add a distinct `budget_scope` field. **Research recommendation (flag for design):** prefer (a) — extend the EXISTING `scope` enum with `handler_account` (and possibly `daily_quota`), because CP-001's invariant is "single typed primitive, one struct"; adding a parallel `budget_scope` field alongside `scope` duplicates the concept. The handler-pause text's `budget_scope` name then maps to the Budget primitive's `scope` field carrying value `handler-account`. This is the minimal, invariant-preserving amendment. **Design must pin the field name + reconcile HP-012's `budget_scope` wording.**

### F3 — Event impedance: Pi emits `flywheel_budget_exhausted`; daemon/HP-012 consume `budget_exhausted` — they are DIFFERENT events (RQ3) — CRITICAL SEAM

Two distinct exhaustion events exist:
- **Pi-side:** `flywheel_budget_exhausted{spent_usd, cap_usd, model}` — emitted by `budget.ts` recordSpend (CL-090). This is the COGNITION-layer signal.
- **Daemon-side:** `budget_exhausted{run_id, session_id?, budget_ref, attempted_dispatch_cost}` — `event-model.md §8.4.3`, producer "agent-runner (S04)", consumed by orchestrator-core. This is the EXECUTION-layer signal that HP-012's consumer reacts to.

HP-012's consumer pauses on **`budget_exhausted{budget_scope=handler-account}`** — i.e. the daemon-side event. But credfence's UNIFIED meter lives Pi-side and emits `flywheel_budget_exhausted`. **These do not currently connect.** This is the central C4/C5 seam the problem-space flagged ("the budget event is the C4/C5 contract seam"). Research pins the options:

1. **Pi emits, daemon consumes:** when the unified meter trips, Pi emits a `budget_exhausted{budget_scope=handler-account}` (the daemon-shaped event, not just `flywheel_budget_exhausted`) into the shared stream, so HP-012's existing consumer fires. Requires Pi to be an allowed producer of `budget_exhausted` (today producer is "agent-runner (S04)") — a small event-model producer-set amendment.
2. **Daemon meters + emits:** move the unified meter daemon-side so the daemon itself emits `budget_exhausted{handler-account}` natively. Rejected — CL-090 is normatively Pi-side, and the meter must also see Pi turns (which the daemon can't).
3. **Pi emits `flywheel_budget_exhausted`; a thin daemon consumer translates** it to a handler-pause. Adds a translation hop.

**Research recommendation (flag for design):** option 1 — when the unified meter exhausts, Pi emits the daemon-shaped `budget_exhausted{budget_scope=handler-account, ...}` (in addition to or instead of `flywheel_budget_exhausted`), reusing HP-012's existing consumer with no new halt path. This requires (a) the `budget_scope=handler-account` enum value (F2) and (b) `event-model.md §8.4.3` to allow Pi (cognition-loop / flywheel) as a producer of `budget_exhausted`. Both are small additive amendments credfence owns. **This is the make-or-break design decision for the hard-halt.** Note `event-model.md §6.4` additive-field rule + the §4.6 amendment protocol govern adding the producer/value cleanly.

### F4 — The control-points amendment IS in-scope for credfence and is additive (RQ4, OQ-4)

OQ-4 leans: land the minimal `budget_scope`/`scope`-enum addition within credfence, since the halt wiring is credfence's deliverable and cannot be specified without it. Research confirms:
- The amendment is **additive**: adding `handler_account` to CP-022's existing `scope` enum is a new enum value; per `control-points.md` CP-001 the single-typed-primitive invariant is about ONE struct/lifecycle/registration — adding an enum value does not create a second primitive. CP-005's per-Kind table row for Budget is unchanged (trigger/evaluator/outcome unaffected by a new scope value). **No invariant break** (problem-space §6 predicted "additive optional field; no change to CP-001").
- `handler-pause.md:411` §13 #3 explicitly defers the field "to control-points amendment" — credfence IS that amendment. The handler-pause change is then a **note** in §13 marking the dependency satisfied, plus an end-to-end path doc (F5). HP-012 text itself is unchanged.

### F5 — End-to-end exhaustion path (RQ5)

The documented path C5 must record (and confirm every hop already exists or is the small addition above):
1. Unified meter (C4, Pi) sums Pi turns + daemon `budget_accrual` -> ratio >= 1.0 OR runsToday >= max-runs.
2. Meter emits `budget_exhausted{budget_scope=handler-account, spent_usd, cap_usd, ...}` into the shared event stream (F3 option 1; requires the producer-set + scope-enum additions).
3. `handler-pause-policy-budget-exhausted-claude-code` consumer observes it; HP-012 fires: pause the `claude` handler type immediately, no hysteresis. No new `claude` implementer/reviewer sessions launch.
4. Cognition loop enters `budget-paused` (`cognition-loop.md` §6 LoopStatus, already present; CL-090 already says "SHOULD pause in `budget-paused`").
5. Reset is non-automatic; operator runs `harmonik supervise resume` (CL-090 + `operator-nfr.md §4.3`). Note: the assessment flagged that `supervise resume` was referenced by `budget.ts`/`circuit-breaker.ts` comments but the verb's *full* lifecycle is a `pilot`-scope concern (S9 pause/resume producer); credfence relies on the EXISTING `supervise resume`/handler-resume surface (HP-resume) to clear the handler pause. **Flag: confirm handler-resume (`handler-pause.md` HP-resume / `harmonik supervise resume`) clears the budget-exhaustion handler pause; if the only resume verb is pilot-scoped, sequence that dependency explicitly.**

## Patterns to follow

- **Reuse HP-012 + its existing consumer**; do not rewrite, do not add a parallel halt (constraint).
- **Extend the EXISTING `scope` enum** (CP-022) with `handler_account` rather than add a parallel `budget_scope` field — preserves CP-001 single-primitive invariant (F2).
- **Pi emits the daemon-shaped `budget_exhausted{handler-account}`** to reuse the consumer (F3 option 1); add Pi to the §8.4.3 producer set via the §4.6 amendment protocol.
- **handler-pause change = a §13 dependency-satisfied note + an end-to-end path doc**, not an HP-012 rewrite (F4).
- **Additive amendments only** (problem-space §6).

## Risks / conflicts

- **R1 (CRITICAL — the make-or-break seam, F3).** Pi's `flywheel_budget_exhausted` and the daemon's `budget_exhausted` are different events; HP-012's consumer only sees the latter. If credfence does not bridge them, the unified meter trips but the handler never pauses — exactly the incident failure (meter inert -> no halt). Design MUST pin: Pi emits the daemon-shaped event with `budget_scope=handler-account`, and `event-model.md §8.4.3` adds Pi/cognition-loop to the producer set.
- **R2 (naming, F2).** HP-012 says `budget_scope`; CP-022 says `scope`. Design must reconcile to one field name to avoid two fields meaning the same thing.
- **R3 (resume verb, F5).** The handler pause must be clearable by a resume verb that exists today. If `harmonik supervise resume`'s budget-paused clear is pilot-scoped, credfence must sequence that dependency or rely on handler-resume (HP-resume). Confirm in design.
- **R4 (producer-set amendment scope).** Adding Pi as a `budget_exhausted` producer touches `event-model.md §8.4.3`. That is a fourth spec in C5's blast radius (beyond handler-pause + control-points). Small + additive, but design/integration must list it.

## Open questions carried to design

- OQ-4 (amendment in-scope) — confirmed in-scope + additive (F4); lean stands.
- NEW (R1/F3): which event bridges Pi-meter to HP-012 — design pins Pi emitting daemon-shaped `budget_exhausted{handler-account}` + the §8.4.3 producer-set addition.
- NEW (R2/F2): field name reconciliation `scope` vs `budget_scope` — extend existing `scope` enum.
- NEW (R3/F5): resume-verb ownership — confirm handler-resume clears budget-paused, else sequence the pilot dependency.
