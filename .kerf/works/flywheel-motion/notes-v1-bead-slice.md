# Note — v1 bead slice (DRAFT, kynes 2026-06-15)

> Pre-staged so we are ONE step from filing beads the instant the operator (a) ratifies the spec draft and
> (b) picks the §5.4 C option. Derived from `05-spec-drafts/flywheel-motion.md` §8.1 (negative-loop-first).
> NOT yet filed — filing waits for ratification (mission discipline: file-not-dispatch only post-ratify).
> Ordering reflects blocker-A "thinnest negative-loop slice first" so the approach is falsified early.

## Epic
- **hk-0oca** (existing, `codename:flywheel`, pinned to `flywheel-motion`) is the umbrella. v1 beads attach
  via `codename:flywheel` label (NOT a parent-dep — epic-dep blocks dispatch).

## v1 beads (negative loop + goal-persistence + G-liveness; positive loop = §5.4 pick)

| # | Bead (title) | Scope / touches | Dep |
|---|---|---|---|
| V1 | **Movement governor (LLM-free)** | New `internal/sentinel` (or fold into `internal/digest`): read `events.jsonl`, compute the §1 weighted-terminal-progress movement over the sliding window; discrete/inverse activation; opportunity-gate (≥1 ready bead OR undeployed tail); cold-start + sustained(≥2) gates. Emits a `movement` digest signal. | — |
| V2 | **Sentinel exception → blocks all-clear** | Wire the trip to write ONE `decision_required`-class exception via the shipped `decision_block_ev043a.go` DecisionBlocker + ack-token; surface through the EV-044 digest summary (`builder.go:165`) so the projector structurally blocks the all-clear; exception names the un-dispatched bead IDs / undeployed tail; clears only on real movement or re-adjudicated legitimate-halt. | V1 |
| V3 | **Mode-detection / suppression resolver** | Deterministic resolver: EXECUTE-BACKLOG default; self-expiring suppression from `operatorAttached()` (`keeper/cycle.go:671`) + `comms log --from operator` recency, each on a decaying TTL + attached-but-inactive timeout (guards the pins-forever bug). | V1 |
| V4 | **Independent fresh-context adversary** | A `harmonik schedule`-spawned separate session reviewing the captain's recent comms/commits as a foreign artifact; adjudicates the trip and writes the V2 exception; runs only when the V1 governor trips (gated, not hot-clock). | V2 |
| V5 | **G-liveness self-kill** | Invert the §1 movement metric: N cycles (`liveness_no_progress_n`) with no HEAD-advance/bead-close/run-complete → halt + page. Required before v1 ships auto-dispatch. | V1 |
| V6 | **Goal-state + ephemeral goal-keeper** | `.harmonik/intent/goal-state.json` schema (objectives/antigoals/verbatim operator directives/`last_event_id`); `harmonik schedule`-spawned goal-keeper (read `comms log --from operator` → distil → rewrite → exit); restart re-reads goal-state; idle-triggered realign. NO injection. | — |
| V7 | **`sentinel:` config block + seed defaults** | New `sentinel:` block in `.harmonik/config.yaml` (sibling to `keeper:`): §7 tunables. **Seed a shippable default low-window threshold** (closes drafter gap #3 — per-project-iterable but ships with a default). | V1 |
| V8 | **Two-phase `done_definition` (shared)** | Per-class `done_definition` in config (default=merged; deploy-relevant classes opt into Phase-2 deployed+verified); the undeployed tail counts as actionable work for V1's opportunity-gate. | V7 |
| **V9 — §5.4 OPERATOR-PICK (parameterized)** | **positive loop** | _Resolves on the C pick:_ | V8 |
| ↳ if **(A) detector-only** | deterministic Go detector → digest `decision_required` exception for landed-but-undeployed; no bead created. | | |
| ↳ if **(B) staged-bead generator** | emit a STAGED deploy+verify bead via `eagerfill_em063.go` refill path + 4 guardrails (rule-only / land-open / WIP==max_concurrent / at-most-once ledger keyed `(target_bead_id, follow_up_class)`); captain-greenlit. | | |
| ↳ if **(C) A-then-B** | file V9 as (A) now; file a v2 follow-up bead for (B). | | |

## Notes
- **Not in v1** (per spec §8.2): crew-utilization axis (drafter gap #2 — the operator's §11.2 case; **confirm v2-deferral**), continuous-governor curve, work-generates-work sources b/c/d, ACT-mode auto-deploy.
- **Sequencing:** V1→V2→{V3,V4} is the critical negative-loop chain; V5 (liveness) gates the slice; V6 (goal-persistence) and V7/V8 (config + done) parallelize; V9 lands last on the C pick.
- **Filing:** on ratify+pick → `br create … --label codename:flywheel` (no parent-dep, no pre-assign); then this becomes the formal `07-tasks.md`.
