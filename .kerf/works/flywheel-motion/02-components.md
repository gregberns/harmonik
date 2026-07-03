# 02 — Component decomposition: flywheel-motion

> **STATUS: COMPONENT DECOMPOSITION (locked parts A,B,D,E,F; C stubbed pending operator pick), kynes 2026-06-15.**
> Derived from `notes-blocker-convergence.md` (THE locked answers) + `docs/flywheel-self-reinforcing-design.md` §1–§11.
> Pinned bead: `hk-0oca` (epic, `codename:flywheel`). Reuses the finalized cognition-loop work's deterministic
> primitives only; does not reopen its agent-replacement framing.

## Shared substrate (the rails every component plugs onto)

All seven components are a **graft onto the live captain+daemon** [blocker A] — not a rebuild. They share three
already-shipped surfaces, so almost no new infrastructure is introduced:

- **The `harmonik digest` exception rail** — the deterministic projector that structurally blocks the captain's
  all-clear on any unacknowledged `decision_required` event (EV-044; `internal/digest/builder.go:165`,
  `internal/digest/types.go:50`). The sentinel writes onto this rail; it never invents new bindingness.
- **The `sentinel:` block in `.harmonik/config.yaml`** — a new per-project config section, sibling to the existing
  `keeper:` block, holding every tunable (weights, windows, TTLs, liveness N). The config file is the iteration
  surface the operator asked for.
- **`.harmonik/events/events.jsonl`** — the typed event log the movement metric reads (`bead_closed`,
  `run_completed`, `reviewer_verdict`, HEAD-advance) and the liveness self-kill inverts.

## (1) Movement metric / governor  [blocker B]

A cheap, LLM-free, deterministic governor that reads `events.jsonl` over a ~30-min sliding window and computes
**movement** = a weighted sum of *terminal-progress* events (bead-closed, run-completed{success}, HEAD-advance,
reviewer-approve weighted high; all starts/chatter weight 0). Its activation is the **inverse** of movement and
**discrete** (a step/staircase, auditable — not a smooth EWMA), **gated on opportunity** (≥1 ready bead OR
landed-but-undeployed/unverified code), suppressed during a cold-start warm-up watermark, and trips only on
**sustained** low movement (≥2 consecutive low windows). It is the *trigger source* for component (2): cheap and
always-on, it decides when the expensive LLM adversary is worth firing. Weights/window/warmup live in `sentinel:`.

## (2) Sentinel authority & trigger + digest-exception-blocks-all-clear  [blocker E]

The negative-feedback governor. When (1) signals sustained-low-movement-with-actionable-work past warm-up, an
**independent fresh-context adversary** (a `harmonik schedule`/spawn-crew session reviewing the captain's
comms/commits as a *foreign artifact*) adjudicates whether the captain's "nothing to do" is real progress or
idle-dressed-as-busy. Its **only power is deterministic, not authority-based**: it writes ONE
`decision_required`-class exception under `.harmonik/` (reusing the shipped `DecisionBlocker` / ack-token machinery —
`decision_block_ev043a.go`, the `.harmonik/decision_acks/` ack-state files). The digest projector then structurally
blocks the all-clear on it: the captain *cannot* return "nothing to do" while it is pending. It clears ONLY on
observed real movement OR a captain-recorded legitimate-halt that the **next** adversary pass re-adjudicates — never
on bare self-ack. Relationship to (1): (1) is the LLM-free trip-wire; (2) is the expensive judgment it gates plus the
binding-by-determinism mechanism.

## (3) Mode-detection / suppression  [blocker D]

A deterministic, LLM-free resolver inside the digest builder that decides whether (2)'s trip should fire or be
suppressed. **EXECUTE-BACKLOG (idle + ready work → trip) is the safe DEFAULT.** Design / issue-clearing quiet is
*only* a **self-expiring suppression** (never a sticky inferred state), granted by durable surfaces the daemon
already owns: keeper `operatorAttached()` (`internal/keeper/cycle.go:671`) AND operator-dialogue recency from
`comms log --from operator`, both on a config-tunable decaying TTL plus an attached-but-inactive timeout (so the
known operatorAttached-pins-forever bug cannot silently disarm the sentinel). Issue-clearing is explicitly *not* a
mode — a progressing clear credits movement in (1) and stays dormant; a stalled clear trips. Relationship: (3) is the
suppression filter sitting between (1)'s trip and (2)'s exception emission.

## (4) Goal-persistence: goal-state + goal-keeper + restart re-grounding + idle-realign  [design #3, NO injection]

The steering subsystem. A durable **goal-state file** (`.harmonik/intent/goal-state.json`: objectives, antigoals,
recent verbatim operator directives, `last_event_id` cursor) is maintained by an **ephemeral goal-keeper agent**
(`harmonik schedule`-spawned, minimal-context: reads `comms log --from operator` → distills → rewrites goal-state →
exits). Correct session **restarts** re-read goal-state on resume; an **idle-triggered realign** (NOT a clock timer)
re-grounds the captain when it goes idle or "thinks it has nothing to do." Explicitly rejects per-turn injection
(Letta core-memory / Ralph re-injection). Relationship: provides the durable goals (2)'s adversary and the captain
both reference; the idle-realign trigger shares the idle signal computed by (1).

## (5) Work-generates-work + two-phase done  [blocker C — STUB, operator decision pending]

The positive-feedback fuel. Two deterministic pieces: a **two-phase `done_definition`** (Phase-1 merged →
Phase-2 deployed+verified) that makes a merged-but-undeployed tail *count as actionable work* for (1)/(2) so the
system cannot go idle on top of it; and a **generator** that, on a Phase-1 completion of a class with a declared
follow-up rule, deterministically enqueues the next step via the existing eagerfill refill path
(`internal/daemon/eagerfill_em063.go` — beads land `open`, provenance-guarded). v1 handles only the
merged-but-undeployed source; deploy/verify is staged/captain-greenlit, never autonomous. **The v1 *shape* (detector-only
vs staged-bead generator vs A-then-B) is the single open operator decision** — see the spec's §5 stub. Relationship:
feeds new actionable work back into the backlog that (1) reads as opportunity, closing the self-reinforcing loop.

## (6) Safety gates incl. liveness self-kill  [blocker F]

The bounds that make autonomy safe. **G-LIVENESS is required before v1**: a doom-loop self-kill that *inverts* (1)'s
movement metric — N cycles with no HEAD-advance/bead-close/run-complete → halt + page. A provenance/WIP bound caps
(5): the generator only enqueues deploy+verify follow-ups of its own merged commits as `open`, never same-tick
auto-dispatch, refill ceiling == `max_concurrent`. v1 ships auto-DISPATCH behind G-liveness but keeps
auto-DEPLOY/PUSH human-gated (integration→main stays a human `harmonik promote`), so G-security and G-cost ride the
ALREADY-SHIPPED daemon fail-closed gates (`--protect-branch` / `--forbid-default-main` / no-force-push / per-run
budget / review-loop-on). G-test (replay) and G-inspect (tmux) are deferrable. Relationship: bounds (2)'s dispatch
and (5)'s refill; inverts (1)'s metric for its own trip.

## (7) Graft architecture (cross-cutting)  [blocker A]

Not a separate runtime piece — the *posture* that binds the other six. It mandates: reuse shipped primitives (digest
exception rail, `decision_required`/`DecisionBlocker`, eagerfill refill, `harmonik schedule`, the comms bus, the
`config.yaml` keeper-block precedent); keep the captain as the **judgment organ** (rank new initiative, name lane,
adjudicate drift) plugged into a deterministic skeleton; and ship the **thinnest negative-loop slice first** so the
whole approach is falsified early if over-deference proves un-fixable from outside the captain's context. Every
component above is specified as an *extension* of an existing surface, never a green-field subsystem.
