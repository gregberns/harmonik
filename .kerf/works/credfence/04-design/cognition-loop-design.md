# Change Design — `specs/cognition-loop.md` (CL-090 unified meter + C6/C7 notes)

> Pass 4 (`change-design`) of the `credfence` spec work. Covers component C4 (unified spend meter over Pi turns + daemon `claude` sessions; finite default; max-runs ceiling) and the informative C6 (model-tier knob) / C7 (dry-run, retry budget) cross-refs that land in `cognition-loop.md`. NORMATIVE output is `05-spec-drafts/cognition-loop.md`. Grounded in `03-research/cognition-loop/findings.md`, `02-components.md §2`, and the live spec (`specs/cognition-loop.md:35-55, 162-219`, re-verified 2026-05-31).

## Current state

- **§2.1 (line ~47):** "Safety: per-day budget kill-switch, reaction-rate circuit breaker, untrusted-input threat boundary." The budget bullet scopes only the kill-switch; nothing names "covers both layers."
- **§4.11 CL-090 (line 169):** "Loop MUST enforce per-day USD cap (`--budget-usd-per-day`). Track cumulative **substrate spend**; stop dispatching new turns when cap exceeded. Emits `flywheel_budget_exhausted{spent_usd, cap_usd, model}`; SHOULD pause in `budget-paused` pending operator intervention. Day boundary = local-midnight in operator TZ at v0.1. Reset NOT automatic; operator MUST `harmonik supervise resume`." Gaps (research F1): (1) "substrate spend" = Pi's own turns only, ignoring the 26+ daemon-spawned `claude` sessions that burned the credit; (2) no finite default stated (code defaults `Infinity` in two places — `index.ts:62`, `budget.ts` `?? Infinity`, research F2); (3) no max-runs ceiling.
- **§6 LoopStatus (line 191):** already carries `budget-paused` — no §6 change needed (research F6).
- **§9 (line 213):** already cross-refs `operator-nfr.md §4.3` for surfacing `budget-paused`.
- **CL-100 composition root (line 174):** "Only this root may wire the harness, substrate, watermark store, digest fetcher, wake-filter table, **budget tracker**." The budget tracker is already a composition-root concern.
- No dry-run requirement; no retry-budget requirement; no model-tier knob mention.

## Target state

A normative EXTENSION of CL-090 (not a fork — constraint, problem-space §4), plus three small additive notes/sub-requirements. Day-boundary and resume semantics are KEPT as-is.

**§2.1 scope note.** Broaden the budget bullet: "per-day budget kill-switch metering BOTH the cognition layer (Pi turns) AND the execution layer (daemon-spawned `claude` sessions)." One-line edit.

**§4.11 CL-090 — rewritten as the unified meter:**
- The per-day USD meter MUST sum BOTH (a) the loop's own Pi model-turn cost AND (b) daemon-spawned `claude` implementer/reviewer/resume session cost. The two are attributed to ONE shared per-day cap.
- **Cost attribution (OQ-1 resolved — research F3).** Daemon `claude` cost reaches the Pi-side meter by the meter consuming the EXISTING `budget_accrual` event (`event-model.md §8.4.2`, payload `{run_id, session_id, chunk_index?, cost_units, cost_basis}`) from the `harmonik subscribe` stream the loop already consumes — NOT a new bespoke `run_cost` event (research F3 supersedes the problem-space lean). When `cost_basis` is non-USD (e.g. tokens), the per-model rate table (below) converts. Documented fallback: when `budget_accrual` is absent/lossy, estimate per-run cost from `run_started`→`run_completed` + the rate table.
- **Max-runs ceiling (NEW sub-requirement, CL-090a).** Alongside the per-day-USD cap, the loop MUST enforce a per-day **max-runs** ceiling: a count of daemon `run_started` events today. When `runsToday ≥ max-runs`, the loop halts dispatch identically to the USD cap. `run_started` is class F (fsync-backed, loss-proof — research F3 caveat), so max-runs is the deterministic backstop against (a) cost-estimate error AND (b) `budget_accrual` event loss (class L is lossy-tail-ok). State explicitly that max-runs is the loss-proof ceiling.
- **Finite default (CL-090, default value).** The default per-day-USD cap MUST be FINITE (recommended 20 USD; the spec states a finite default and that the value is operator-tunable, not a magic constant locked in spec). The inert `Infinity` default is removed (`index.ts:62`, `budget.ts ?? Infinity`). Operators opt into unlimited explicitly (`--budget-usd-per-day=unlimited` / empty `FLYWHEEL_BUDGET_USD_PER_DAY`). Safe-by-default principle stated. Max-runs likewise carries a finite default.
- **Eventual-consistency window (NEW note — research R3).** Because the meter lives Pi-side and daemon cost arrives via the event stream, the USD meter is EVENTUALLY consistent — a burst of daemon spawns between accrual events MAY momentarily exceed the cap before the meter catches up. The max-runs ceiling (incremented on `run_started`, near-synchronous) bounds the burst. State the window and the bound.
- **Reset semantics (CL-090, unchanged + extended — research F4).** Both ceilings reset on the SAME day boundary (local-midnight, operator TZ); a single "new day" rollover resets `spentUsd` AND `runsToday`. ONE reset semantics, ONE resume action. Reset is NOT automatic across the cap-hit; operator clears the pause (see hard-halt design / handler-pause). Day-boundary text reused verbatim.
- **Exhaustion event (the C4/C5 seam — see handler-pause + event-model design).** On exhaustion (USD ratio ≥ 1.0 OR `runsToday ≥ max-runs`), the unified meter emits the DAEMON-shaped `budget_exhausted{budget_scope=handler-account, ...}` so `handler-pause.md` HP-012's existing consumer fires — in ADDITION to (or in place of) the Pi-internal `flywheel_budget_exhausted` (research F3 option 1). CL-090 references the handler-pause hard-halt path; the producer-set + scope-enum additions are owned by the event-model and control-points designs. Loop SHOULD pause in `budget-paused` (unchanged).

**§4.11 — additive notes (informative cross-refs):**
- **CL-090b (model-tier knob, C6).** Informative: the Pi judgment model is an operator-tunable tier via `FLYWHEEL_MODEL_TIER1/2/3` (default tier-3 judgment = Sonnet, Opus gated behind opt-in); the daemon `claude` baseline is a single operator-facing default. Both are ON-004 config-inventory entries (operator-nfr design). This note cross-refs operator-nfr; the normative knob text lives there.
- **CL-090c (retry budget, C7/G9 — research F4).** Informative: review-loop iterations and re-dispatches each launch a paid daemon `claude` session and therefore each COUNT toward the max-runs ceiling — paid retries draw down the SAME finite budget rather than being free. No third budget surface (problem-space §4). Precedence: max-runs is the GLOBAL backstop; the per-bead `iteration_cap` / `no_progress_detected` (operator-nfr ON-002) is the LOCAL bound; whichever fires first wins (research R2).
- **CL-090d (dry-run, C7/G8 — research F3).** Informative: a daemon `--dry-run`/plan-only mode previews intended spawns ("N implementers + N reviewers across M beads") WITHOUT launching `claude`, reading the credential source, or emitting spend. Mirrors `harmonik queue dry-run` + orphan-sweep report-vs-act gating. Normative flag is an ON-004 entry (operator-nfr design); this note records the spend-safety rationale.

**§6 / §8 / §9.** §6 LoopStatus unchanged (`budget-paused` present). §8 open questions: mark OQ-CL-001..005 untouched; OQ-1/OQ-3/OQ-6 from this work are RESOLVED in the design (no new open question). §9 operator-nfr cross-ref unchanged (already present).

## Rationale

- **Extend, don't fork (constraint).** CL-090 is the named home for the budget contract; the unified meter broadens its scope and flips its default rather than minting a parallel budget surface.
- **Reuse `budget_accrual` (OQ-1).** Research F3 found the daemon ALREADY emits per-run cost via `budget_accrual` — reusing it is lower-cost than the problem-space's proposed new `run_cost` event and keeps the meter on the event surface Pi already consumes.
- **Max-runs is the loss-proof backstop.** `budget_accrual` is class L (lossy); a USD-only meter could under-count past the cap. Counting class-F `run_started` makes max-runs a deterministic, loss-proof, burst-bounding ceiling — exactly why the problem-space added it, now also justified as an event-loss backstop (research F3 caveat).
- **Finite default is a deliberate behavior change (research R4).** Infinity→finite is intentional, not a regression — the prior behavior was the vulnerability. Operators who want unlimited still opt in. Changelog flags this.
- **Retry budget folds into max-runs (research F4).** Treating each retry as a run that draws the budget is the single-meter answer and avoids a third budget surface.

## Requirements traceability

| 02-components requirement | Goal (01 §2) | Target requirement |
|---|---|---|
| C4 unified meter (Pi + daemon claude) (SC4) | G4 | CL-090 (rewritten), CL-090a (max-runs) |
| C4 cost attribution (OQ-1) | G4 | CL-090 (consumes `budget_accrual`) |
| C4 finite default + opt-out (SC5) | G5 | CL-090 (finite default), CL-090a (max-runs default) |
| C5 exhaustion event seam (SC6) | G6 | CL-090 (emits daemon-shaped `budget_exhausted{handler-account}`) — full wiring in handler-pause/event-model/control-points designs |
| C6 model-tier knob (SC7) | G7 | CL-090b (informative; normative in operator-nfr) |
| C7 retry budget (SC9) | G9 | CL-090c (informative; folds into max-runs) |
| C7 dry-run (SC8) | G8 | CL-090d (informative; normative flag in operator-nfr) |

Every C4/C6/C7-in-cognition-loop requirement has a target; no target lacks a backing requirement. §6 confirmed no-change. The C5 hard-halt and the C6 normative knob text land in sibling specs (cross-referenced).
