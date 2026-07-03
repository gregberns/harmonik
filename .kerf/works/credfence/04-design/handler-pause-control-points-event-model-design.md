# Change Design — `handler-pause.md` + `control-points.md` + `event-model.md` (C5 hard-halt wiring)

> Pass 4 (`change-design`) of the `credfence` spec work. Covers component C5: wiring the unified-budget exhaustion into the EXISTING handler-pause consumer so dispatch hard-halts, the `budget_scope`/`scope`-enum amendment HP-012 depends on, and the event-model producer-set addition that lets the Pi-side meter emit the daemon-shaped event. These three specs are co-designed because they are one seam (the meter trips → an event → the consumer pauses). NORMATIVE outputs: `05-spec-drafts/{handler-pause,control-points,event-model}.md`. Grounded in `03-research/handler-pause-control-points/findings.md`, `02-components.md §2`, and the live specs (re-verified 2026-05-31).

## C5 is three coupled spec edits, deliberately co-designed

The research found the central seam: **Pi's `flywheel_budget_exhausted` and the daemon's `budget_exhausted` are DIFFERENT events; HP-012's consumer only sees the latter** (research F3, the make-or-break finding). credfence's unified meter lives Pi-side; HP-012's existing consumer pauses on the daemon-shaped event with `budget_scope = handler-account`. Bridging them requires:
1. the `scope` enum to carry `handler_account` (control-points), and
2. the event-model to allow the cognition-loop/Pi as a producer of `budget_exhausted` (event-model),
so that (3) the Pi-side meter can emit the daemon-shaped event into the shared stream and HP-012's UNCHANGED consumer fires (handler-pause).

This is the chosen approach (research F3 option 1): **Pi emits the daemon-shaped event; reuse HP-012's existing consumer; no new halt path** (constraint, problem-space §4: "do not invent a parallel halt path"). Sequencing inside C5: control-points scope-enum FIRST (handler-pause depends on the field existing), then event-model producer-set, then handler-pause confirmation.

---

## `specs/control-points.md` — add `handler_account` to the Budget `scope` enum

### Current state
- **CP-022 (line 228):** "A `Budget` MUST declare: (a) `resource ∈ {tokens, wall_clock_seconds, iterations}`, (b) `scope ∈ {per_role, per_run, per_state}`, (c) `limit`, (d) `warning_threshold`." There IS already a `scope` field (research F2 KEY FINDING) — but its enum lacks `handler-account`.
- **CP-001 (line 81):** single-typed-primitive invariant — one struct, one lifecycle, one registration.
- **CP-005 (line 110):** per-Kind table; Budget trigger/evaluator/outcome.
- **CP-023 (line 234):** runner emits `budget_exhausted` per `event-model.md §8.4` on would-exceed; failure class `budget_exhausted`.
- `handler-pause.md:411` §13 #3 explicitly DEFERS the field "to control-points amendment" — credfence IS that amendment.

### Target state
- **Extend CP-022's existing `scope` enum** to `scope ∈ {per_role, per_run, per_state, handler_account}` (research F2 recommendation (a)). This is the minimal, invariant-preserving amendment: adding an enum VALUE, not a parallel `budget_scope` field. CP-001's single-primitive invariant is about ONE struct/lifecycle/registration; an enum value adds no second primitive (research F4). CP-005's Budget row (trigger/evaluator/outcome) is UNCHANGED.
- Add an INFORMATIVE note at CP-022: a `handler_account`-scoped Budget represents a per-handler-account ceiling (session-token cap, daily-quota); its exhaustion is handler-fatal per `handler-pause.md` HP-012, distinct from `per_run` exhaustion which is per-bead. This is the credfence unified per-day cap's scope value.
- **Field-name reconciliation (research R2):** the Budget primitive's field is `scope` (CP-022); `handler-pause.md` HP-012's prose says `budget_scope`. The amendment pins `scope` as the canonical field name on the Budget primitive carrying value `handler_account`; the handler-pause spec text's `budget_scope` reads as "the Budget's `scope` field = `handler_account`." Note added to both specs so a reader does not infer two fields. (Enum value spelled `handler_account` to match CP-022's snake_case enum members; HP-012's `handler-account` prose is the same concept — reconciliation note pins this.)

### Rationale
Additive, invariant-preserving (research F4); extends the existing field rather than duplicating the concept (research F2/R2); resolves the exact gap `handler-pause.md:411` §13 #3 defers. CP-026/CP-026a (counter rehydration) are unaffected — a new scope value does not change accrual replay.

---

## `specs/event-model.md` — add cognition-loop/Pi to the `budget_exhausted` producer set

### Current state
- **§8.4.3 (line 182):** `budget_exhausted` | class O | producer **agent-runner (S04)** | consumers orchestrator-core, audit | payload `{run_id, session_id?, budget_ref, attempted_dispatch_cost}`.
- **§8.4.2 (line 181):** `budget_accrual` | class L | producer handler (via daemon watcher) | consumers improvement-loop, observability | `{run_id, session_id, chunk_index?, cost_units, cost_basis}` — the cost surface the Pi meter consumes (cognition-loop design).
- **§6.4 additive-field rule / §4.6 amendment protocol** govern adding a producer/value cleanly (research F3).

### Target state
- **Amend §8.4.3:** add `cognition-loop (flywheel)` to the `budget_exhausted` producer set, so the producer column reads "agent-runner (S04); cognition-loop (flywheel)". This lets the Pi-side unified meter emit the daemon-shaped `budget_exhausted` into the shared stream that HP-012's consumer reads.
- **Payload note:** the cognition-loop-emitted `budget_exhausted` carries `budget_scope = handler_account` (mapping to the Budget primitive's `scope` field per control-points) plus the existing fields; `run_id`/`session_id` MAY be absent for an account-level (non-per-run) exhaustion — add an informative note that account-scoped exhaustion is run-agnostic. Keep the event class O (ordinary) and mechanism-tagged per §8.4's section axes.
- **§8.4 section-axes note** updated to mention the cognition-loop as an additional producer of the account-scoped variant; no durability-class change.

### Rationale
The make-or-break seam (research F3/R1): without this producer addition the Pi meter trips but the handler never pauses — exactly the incident failure (meter inert → no halt). Small, additive, governed by the §4.6 amendment protocol. This is the fourth spec in C5's blast radius (research R4) and integration must list it.

---

## `specs/handler-pause.md` — confirm HP-012 fires; mark §13 #3 satisfied; document the path

### Current state
- **HP-012 (line 224):** "A handler type MUST be paused immediately (no hysteresis) when a `budget_exhausted` failure is observed with `budget_scope = handler-account`. Per-run budget exhaustion ... MUST NOT trip a handler pause." Normative controller behavior ALREADY specified (research F1) — credfence does NOT rewrite it.
- **§8 classification table (line 217):** `budget_exhausted` is "Handler-fatal only when ... per-handler-account (session-token cap, **daily-quota**)" — the daily-quota case is exactly credfence's per-day cap.
- **§13 #3 (line 411):** defers the `budget_scope` field to a control-points amendment.
- The consumer `handler-pause-policy-budget-exhausted-claude-code` (`internal/daemon/composition_registry_hkndysh.go`) already reacts and pauses the `claude` handler type — credfence reuses it (constraint).

### Target state
- **HP-012 — UNCHANGED text.** Add a NOTE that `budget_scope = handler-account` maps to the Budget primitive's `scope` field value `handler_account` (control-points reconciliation, research R2) and that the credfence unified per-day/max-runs meter is the producer of the qualifying `budget_exhausted` (the daily-quota case in the §8 classification table).
- **§13 #3 — mark SATISFIED.** Replace the deferral with: "RESOLVED by the credfence work — `control-points.md` CP-022's `scope` enum now includes `handler_account`; the cognition-loop/Pi meter emits the qualifying `budget_exhausted` per `event-model.md §8.4.3`." Keep the item for traceability rather than deleting it.
- **End-to-end path doc (research F5)** — add an informative subsection (or extend §8's informative note) recording the full hop chain:
  1. Unified meter (cognition-loop CL-090) sums Pi turns + daemon `budget_accrual` → USD ratio ≥ 1.0 OR `runsToday ≥ max-runs`.
  2. Meter emits `budget_exhausted{budget_scope=handler_account, ...}` into the shared stream (event-model §8.4.3 producer addition).
  3. `handler-pause-policy-budget-exhausted-claude-code` consumer observes it; HP-012 fires — `claude` handler type paused immediately, no hysteresis; no new implementer/reviewer sessions launch.
  4. Cognition loop enters `budget-paused` (`cognition-loop.md` §6, already present).
  5. Reset is non-automatic; operator clears via the EXISTING handler-resume surface (`harmonik supervise resume` / handler-pause HP-resume). **Resume-verb scope (research R3/F5):** credfence relies on the EXISTING handler-resume / `supervise resume` surface to clear the budget-exhaustion handler pause; it does NOT design a new pause/resume producer (that is `pilot`'s S9). If the only budget-paused-clearing resume verb proves pilot-scoped, integration sequences that dependency explicitly. The handler pause itself is cleared by the existing handler-resume path.
- **Appendix A** gains a credfence-amendments subsection naming the control-points `scope`-enum extension and the event-model producer addition (so handler-pause stays the self-describing home for the hard-halt, mirroring its existing Appendix A pattern).

### Rationale
HP-012 + its consumer already exist (research F1); credfence supplies only the event + the field that make it fire. The handler-pause change is a §13 dependency-satisfied note + an end-to-end path doc, NOT an HP-012 rewrite (research F4). Reusing the existing resume surface avoids scope-creep into `pilot`'s pause/resume control plane (research R3).

## Requirements traceability

| 02-components requirement | Goal (01 §2) | Target |
|---|---|---|
| C5 budget_scope field (SC6, OQ-4) | G6 | control-points CP-022 `scope` += `handler_account` |
| C5 event seam (SC6, research R1) | G6 | event-model §8.4.3 producer += cognition-loop |
| C5 HP-012 fires + path doc (SC6) | G6 | handler-pause HP-012 note + §13 #3 SATISFIED + end-to-end path + Appendix A |
| field-name reconciliation (research R2) | G6 | control-points + handler-pause reconciliation notes (`scope`=`handler_account`) |
| resume-verb scope (research R3) | G6 | handler-pause path step 5 (reuse existing handler-resume; sequence pilot dep if needed) |

Every C5 requirement has a target across the three specs; no target lacks a backing requirement. No contradiction with the cognition-loop design (CL-090 emits exactly the event this design consumes — the C4/C5 seam is consistent end to end).
