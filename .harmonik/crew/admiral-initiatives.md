# Admiral — Major-Initiatives Registry

> **STANDING RULE (operator-mandated 2026-07-05) — PRE-DEPLOY E2E TEST GATE.** No daemon
> deploy ships without new end-to-end tests, added that deploy, that reproduce the changed
> behavior on a real launch path IN ISOLATION from the live daemon (never test on the primary
> daemon; green units are not the gate). Enforce every deploy. Canonical: orchestrator-rules
> §"PRE-DEPLOY END-TO-END TEST GATE" + `docs/daemon-redeploy.md` GATE 0.

> **Admiral-owned.** The complete set of major initiatives + their status — the admiral's
> oversight anchor. Every audit reconciles it against ground truth (captain-lanes.md tier-2
> + `kerf next` + comms). Complements captain-lanes.md (which crew is on which lane right now);
> THIS file tracks *all the big rocks + which are active / on-deck / parked*.
>
> **Maintenance:** update each audit when an initiative's status changes. Keep it SHORT — one
> line per initiative; detail lives in the plan/bead/epic it points at. Re-read every restart.
>
> **Status vocabulary:** ACTIVE (worked now) · ON-DECK (next to staff, no blocker) · PARKED
> (zero ready beads now — a FACT, not a hold; self-authorizable to resume) · GATED (held by a
> NAMED, DATED, OWNED, EXPIRING gate) · DONE (landed).
>
> **Pre-freeze registry (Pi / Remote / Codex-as-crew / Quality-enforcement / comms-test-harness
> + the full dated audit-marker log through the v0.5.0 cut):** archived at
> `.harmonik/archive/2026-07-12-freeze-and-carve/admiral-initiatives.md` (not boot-read). That
> whole priority order is SUPERSEDED by the freeze-and-carve pivot below.

## ★★ CURRENT INITIATIVE — FREEZE-AND-CARVE (operator, 2026-07-12 ~18:00Z)

> Stop the bug-fix treadmill. Keep the proven core (queue model, lifecycle sweeps, harness
> axes, ~466 regression tests); rebuild the two ack-free IO boundaries (agent input, remote)
> and extract the daemon god-function; delete the test-theater. **NO big-bang rewrite.**
> Full diagnosis: `plans/2026-07-12-codebase-census/REPORT.md`. Sequenced program:
> `plans/2026-07-12-codebase-census/PLAN.md` (review-hardened v2).

**Status: PLAN-FIRST, execution FROZEN.** Fleet clean-slated (all crews down, worktrees
reaped, 267 beads closed; captain + admiral up). Nothing dispatches until the operator
ratifies PLAN.md and lifts the freeze.

| # | Initiative | State |
|---|---|---|
| 1 | **freeze-and-carve** — the carve/rebuild program (P1 → Track B/C → M1 → {M2, M3} → M4 → M5) | **ACTIVE (executing).** Ratified 2026-07-13. As of 2026-07-15: P1 + Track B/C ✅, **M3 done** (closed out-of-jig), **M2 code-complete** (T5–T10; T11 parked by operator — delete once no callers), M1 mostly done (M1-5 finishing; M1-2/M1-3 operator-gated), **M4 next** (at `analyze`; planner running AR-2 + design→ready), M5 held (un-hold trigger now met). Order + T11 rationale: COORD c035. |

**On ratification** the ordered work is: STEP-0 (resume-hang + noChange false-close +
honest-probe re-land, OUT-OF-PIPELINE) → M1 delete test-theater (concurrent) → M2 agent-input
substrate → M3 run-state-machine extraction → M4 remote rebuild. M2/M3/M4 are kerf-first.

**UPDATED 2026-07-13 (operator):** the "generative-system" meta-framing is **PARKED** (to be
approached differently later; not moving forward now). The line is the **codebase overhaul**.
Its concrete findings are kept as inputs: a stable **event/port substrate + record→replay +
property-tested invariants**, and **replay-vs-frozen-baseline measurement** (not live A/B). The
Codex substrate template is already built (`internal/apptap|codexwire|codexreactor|codexdigitaltwin`).
**Next: a larger overhaul plan deciding REWRITE-vs-REFACTOR per section** (STEP-0 pipeline / M1
test-theater / M2 agent-input / M3 run-lifecycle god-fn / M4 remote), tuned before dispatch;
start small (session-restart vertical, resume-hang = its first property test). Exploration
docs: `plans/2026-07-12-codebase-census/generative-system-exploration/` (docs 1–4). Baseline:
`.harmonik/events/baseline-2026-07-13/`.

**PARKED / superseded:** the entire pre-freeze lane set (Pi, Remote, Codex-as-crew,
Quality-enforcement, comms-test-harness, captain-startup-revamp, eval-program, flywheel,
dehardcode). Their durable value folds into the carve moves where relevant (e.g. remote →
M4, quality/test-theater → M1). Do NOT re-stand any of them as a standalone lane; the carve
program is the single front line now.
