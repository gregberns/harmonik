<!-- Operator decisions captured 2026-07-11 (admiral session). Input to the Stage-2 revamp pass.
     These OVERRIDE the drafts + 02-cutover open questions where they conflict. -->

# Operator decisions — startup revamp

## Governing design principle (applies to the WHOLE revamp)

**PRINCIPLES, NOT RULES.** The captain/crew operating docs must give agents *principles they
reason from*, not a checklist they obey. Specific rules survive only as illustrations and
guardrails UNDERNEATH a principle — never as the primary mode. Operator, 2026-07-11:
"This is where we need PRINCIPLES NOT RULES." Reason from the operator's global CLAUDE.md too
("Principles, not procedures. Reason from these; don't obey them as a checklist").

Consequences for the pass:
- The captain/crew operating docs LEAD with principles; the current condensed rule-piles get
  re-approached, not just trimmed.
- Any place that reads as a closed category list or a checklist is a smell to fix.

## The escalation principle (Q1 — resolved, and it is a PRINCIPLE)

Do NOT encode "escalate only architectural/functional decisions" — that is a rule, and its
failure mode is that a genuinely important decision outside those two labels silently never
gets raised.

The principle: **agents decide and verify their own work; they raise to a human only what a
reasonable operator would genuinely want a say in — judged by stakes and reversibility, each
time.** The "pass it to 3 others to check" consensus worked BECAUSE it caught mistakes while
filtering the dumb stuff — keep that adopt-then-verify model; do NOT switch to blocking
escalate-first. Stop over-raising operational trivia (the operator flagged several of the
revamp's own open questions as exactly this over-raising).

## Chain of communication (Q2 — resolved)

The captain communicates to the **admiral**, not to the operator. Reason the admiral exists:
the captain is always too busy to plan with the operator, and the operator is a human who is
NOT wired into the message feed. **Strengthen the admiral's duty to SURFACE pending decisions
to the operator when the operator is actually present/interacting** — today decisions "just sit
there because no one brings them up correctly when the operator is interacting."

## Fail fast and loud (Q3 — resolved)

Crew-start name/queue collision → **fail fast and loud, no auto-rename/auto-retry.** A collision
almost always means the lane is already staffed; auto-relaunch double-staffs the epic (captain
starts helga, but bruno is already working it = bad). Matches the project's fail-fast rule set.

## Keep the "get the system to actually do work" rules (Q4, Q13 — resolved)

- Keep all four relocated STARTUP rules INCLUDING the goal re-check (d). These aren't really
  "decisions" — they are what makes the system do the work it needs to.
- **ANTI-IDLE stays strong.** "We need instructions to get the captain and crew to DO things —
  otherwise they sit around doing nothing." An idle slot with ready work is a defect; agents
  take initiative and GO.

## Dropped as non-operator-decisions

- **Q5 (kynes-q paused)** — captain's operational call, not the operator's. Removed. (Example of
  the over-raising to stop.)

## Boot defaults (Q8 — resolved)

- Captain listens for only TWO event kinds at boot (epic-completed + urgent) — deliberately NOT
  per-task chatter (that was the context/token bloat). CONFIRMED.
- Captain escalates to the admiral by default. CONFIRMED — "I'd like that to start happening."

## HARD-RULE tag set (Q9 — resolved with operator input)

Keep the inviolable "HARD" tag on (structural):
1. Work goes through the daemon queue (not become-a-daemon).
2. The 3 exceptions for using a sub-agent instead of the queue.
3. The daemon owns closing/claiming beads (never pre-set in_progress).
4. Every batch gets a review phase.
5. Never `cd` into a worktree.
6. No daemon deploy without new end-to-end tests (operator 2026-07-05 gate).
7. Big recurring failures trigger a fan-out.
8. **STREAM-NOT-WAVES — ADD BACK / keep HARD.** "Waves are terrible" — either keep this
   inviolable OR remove waves from the tooling entirely (follow-up to consider).
9. **Scratch-lane discipline — keep HARD.** "This seems really important" (no scratch files on
   main; real-daemon validation uses the smoke scratch lane).

Soften to a plain rule / recommendation (drop the HARD tag), but KEEP the behavior:
- Submit a batch as one group (mechanical).
- "Stale" ≠ "wedged" — don't kill a run too early (operational).
- **Throwaway-canary (Q12): recast as a clearly-explained PROCESS/recommendation, not an
  inviolable rule.** Plain meaning: when probing whether the system can even spawn a worker
  (a health check), use a fake throwaway task so a real piece of work isn't burned on a
  diagnostic. Operator: "maybe a recommendation or process thing to define."
- **Friction-gets-priority (Q10): keep the behavior; tag strength is negotiable.** Operator
  mixed but leans keep: "We are dogfooding it — if agents don't fix their own painpoints who
  will?" This is the dogfooding engine — keep it working.

## Deferred to research, NOT a rule (Q7)

Do NOT cement "an always-on watch must exist." Instead QUEUE RESEARCH: do the **watch**, the
**watchdog**, and the old **flywheel** window actually do anything useful, and should any be
cut? Operator "kinda doubts" they earn their keep. The revamp adds no watch-durability rule
until this is answered.

## Housekeeping (Q6)

49 git stashes exist (mostly abandoned `leak`/`v36-leak`/worktree-agent WIP). Operator: delegate
a triage — keep anything genuinely useful, delete the rest; a full cleanup PROCESS is wanted
eventually but NOT being pursued now. (Delegated to a sub-agent this session.)
