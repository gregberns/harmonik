# 01 — Problem Space: flywheel-motion

> **STATUS: STRAWMAN DRAFT for operator redline (crew `kynes`, 2026-06-14).** Built from the locked
> design in `docs/flywheel-self-reinforcing-design.md` (§1–§11) + the operator's verbatim transcript
> capture. Not confirmed. Redline freely — every line is a proposal, not a commitment.
> Pinned bead: `hk-0oca` (epic, `codename:flywheel`). Reuses the finalized `flywheel` cognition-loop
> work as input (deterministic primitives only), does not reopen it.

## Summary (one paragraph)
Today the operator is the system's **pull-cord**: the captain/orchestrator runs down to idle and a
human must re-spin it — handoff → restart → resume, or "go dispatch the ready work," or "deploy the
fix you landed." That is the *opposite* of a flywheel. This work makes the system's own progress
**sustain and compound its motion**, so the operator becomes an occasional energy **top-up** (rank a
brand-new initiative), not the restarter. It does so by grafting two coupled, **deterministic-skeleton-first**
mechanisms onto the live captain+daemon: a **negative loop** — an independent, fresh-context
adversarial **sentinel** that makes "idle-or-drifted while actionable work exists" a `harmonik digest`
exception that *structurally blocks the all-clear* and can re-task the captain; and a **positive loop** —
**work-generates-work**, where the system's own outputs (completions, landed-but-undeployed code,
reviews, harvest) deterministically refill the backlog so motion compounds instead of decaying. Goals
stay re-grounded by a durable **goal-state** maintained by an **idle-triggered goal-keeper** — no
per-turn injection. The decisive principle throughout: **drift and over-deference are beaten by
INDEPENDENCE or DETERMINISM, never by more prompt text in the same context.**

## Goals (what this work achieves)
1. **Self-correcting motion.** The system does not sit idle while actionable work exists without
   self-correcting. Idleness/drift becomes *mechanically observable and self-correcting* — the human
   stops being the control loop.
2. **A positive-feedback momentum source.** Completed work deterministically generates its own
   follow-ups / deploy / verify; reviews→fixes and harvest refill the queue from the system's own
   output, so the backlog tends to *grow from progress*, not drain to idle.
3. **Goal-coherence that survives context pressure & restarts.** A durable goal-state + correct
   restart re-grounding + idle-triggered realign — *without* per-turn prompt injection.
4. **A sentinel that is tunable and well-behaved.** Its activation function is **per-project
   config-editable and iterable**; it **respects a captain-declared legitimate halt**; it does **not
   over-fire** (cold-start warm-up, design / issue-clearing quiet modes).
5. **Judgment preserved, not replaced.** The captain remains the judgment organ plugged into the
   deterministic skeleton; the flywheel *forces judgment to be exercised* (dispatch, realign), it does
   not try to make the LLM "remember" to.

## Non-goals (explicitly out of scope)
- **NOT another prompt-text / contract-wording iteration.** That class has failed ≥3× in a month;
  `hk-trg5` already shipped the contract-layer bridge. This work is the *structural* layer.
- **NOT per-turn goal injection.** Letta core-memory-block / Ralph-loop re-injection are **rejected**
  by the operator. Goal-persistence = clear goal-state + correct restarts + idle-realign.
- **NOT a full rebuild of the cognition-loop** (the Architecture-B deterministic loop that *replaces*
  the agent). Graft deterministic pieces onto the **live captain** incrementally; reuse the old work's
  primitives (digest projector, watermark, two-phase done, queue-pressure refill) without its
  agent-replacement framing. *(OPEN: confirm graft-onto-live-captain vs. fuller rebuild — see §Open.)*
- **NOT a fixed-timer focus-check.** The 12-minute tick is dropped; realign is **idle/event-triggered**
  (a busy captain already knows its initiatives).
- **NOT expanded unattended-push autonomy** beyond today's gates until the safety gates exist
  (replay-test harness, threat-model, cost kill-switch).

## Constraints
- **Independence or determinism, never prompt text** — the load-bearing principle; reject any
  "stronger wording" proposal as the failing class.
- **Movement metric weighted to REAL progress** — HEAD-advances, beads-closed, runs-completed
  dominate; comms/chatter counts ~nothing (else it is gameable by "the captain typed something").
- **Two axes, not one.** "Is the system moving?" reads *system-progress* (crew runs **OR** captain
  work) AND *available-work* (ready/unblocked beads + unblocked initiatives + **undeployed/untested
  code**); plus a **crew-utilization** read (captain-quiet+crew-working = excellent; captain-working
  +crew-idle = soft signal; both-idle+work = the hard trip). *(from transcript capture §11.2)*
- **Per-project config-editable equation** — activation fn + thresholds live in `.harmonik/config.yaml`
  (the keeper section's surface), iterable per project.
- **Captain legitimate-overrule-stop respected** — a declared real halt (ENOSPC, infra blocker) is
  surfaced, not fought; the sentinel distinguishes "stopped for a real reason" from "drifted."
- **Modes** — *execute-backlog* (fire on idle+work) vs *design / issue-clearing* (quiet is legitimate
  progress); plus a **cold-start warm-up** guard and a **sustained** (not momentary) low-movement
  requirement.
- **Preserve** tmux-inspectability and the daemon's LLM-free work-loop (PL-018); the sentinel/digest
  layer is deterministic Go, the LLM surface stays thin (rank new initiative, name lane, adjudicate
  drift).
- **Billing reality** — any always-on/headless component bills API per-token (Max OAuth path banned);
  needs a cost ceiling / kill-switch.

## Success criteria (concrete, verifiable)
1. **Idle-with-work trips.** Given ready/unblocked beads OR undeployed/unverified landed code, AND no
   in-flight crew runs, AND captain idle — sustained past the warm-up window — the system emits a
   digest exception that blocks the all-clear and either auto-staffs the top-ready lane or pages with
   the *specific un-dispatched bead IDs*, **before a human notices**.
2. **Legitimate halt respected.** Given a captain-declared halt (e.g. ENOSPC), the sentinel surfaces
   the blocker and does **not** fire an idle-violation.
3. **Quiet modes hold.** Given active operator design dialogue or a declared design/issue-clearing
   phase, the sentinel stays quiet (no false trip).
4. **Work generates work.** Given a completed bead of a class with a defined follow-up rule (e.g.
   landed-but-undeployed → deploy+verify), the system deterministically enqueues that follow-up with
   no human prompting.
5. **Restart re-grounds.** After a context-fill restart, the captain re-grounds from the durable
   goal-state (objectives / antigoals / recent operator directives) with **no per-turn injection**.
6. **Tunable.** The activation fn + thresholds are editable in `.harmonik/config.yaml`, and a changed
   value measurably changes firing behavior (per-project iterable).
7. **Cold-start safe.** A fresh / just-restarted session does **not** trip the sentinel during warm-up
   despite naturally-low movement.

## Open questions to resolve in this pass (drive these with the operator)
- **A. Graft vs rebuild** — graft deterministic pieces onto the *live* captain (incremental, no loop
  rebuild) vs a fuller cognition-loop. *(Lean graft; confirm.)*
- **B. Movement metric** — exact event set, weights, and window for the inverse-activation function.
- **C. Work-generates-work sources** — *which* outputs are allowed to refill the backlog
  (follow-up beads, deploy+verify of own merged work, logmine harvest, review→fix), and the guardrail
  against runaway self-generated busywork. **(Least-specified; highest leverage.)**
- **D. Mode-detection** — how the system *knows* it is in execute-backlog vs design/issue-clearing
  (declared phase? active operator dialogue signal? bead-class heuristic?).
- **E. Sentinel authority & trigger** — what makes its verdict binding enough to re-task the captain
  (the fresh-context separation), and *when* it fires (idle-triggered vs continuous inverse-movement).
- **F. Safety gates scope** — which gates are required before any expansion of unattended action.
