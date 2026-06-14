# Flywheel + Sentinel — self-reinforcing-motion design (working doc)

**Status:** DESIGN / iterating (operator-driven, 2026-06-14). Not yet a kerf work or implementation beads. **Pick up here next session.**
**Bead:** `hk-0oca` (epic, `codename:flywheel`) — this doc consolidates its comment thread. Related: `hk-trg5` (captain-initiative contract redesign), `hk-4zy9` (keeper-warn fix, DEPLOYED), `hk-hxkz` (/quit excision).
**Supersedes framing of:** the older `flywheel` cognition-loop change-design (`.kerf/.../flywheel/04-design/cognition-loop-design.md`) — operator is **not bound to it**; reuse its deterministic pieces (digest/watermark/queue-pressure) but re-spec around the vision below.

---

## 1. Vision (operator, verbatim intent)
> "I'd like an **actual flywheel — a system that reinforces its continued motion.**"

A real flywheel stores momentum so each rotation eases the next; external input is an occasional *top-up*, not a *pull-cord*. Today the operator keeps having to "get the flywheel spinning again" — the opposite of a flywheel. Goal: the system's own progress sustains and compounds its motion.

## 2. The problem — why every prior fix regressed
Evidence (6-agent fan-out + transcript analysis, 2026-06-14): the captain idled **7.4h / 42 keeper warn-cycles / ~69M tokens re-processed** repeating "holding," with only 2 genuine surface-and-aways out of ~25 deferrals. The autonomy "fix" has been re-applied **≥3× in a month** — because **every fix has been prompt-layer text, and the failure is *structural*.** Prompt text shares the drifted context (diluted, out-scored by recency, self-over-approved). Decisive principle:

> **Drift and over-deference are beaten by INDEPENDENCE or DETERMINISM — never by more prompt text in the same context.**
> Corollary (the framing that landed): **make idleness/drift *mechanically observable* — then the system self-corrects instead of the human being the control loop.**

Structural root: the keeper's only autonomous levers detect **context-fill** (`act_pct`) and **pane-waiting** (`CrispIdle`). **Nothing anywhere detects "ready work exists and nothing is progressing."** No task-idle / dispatch-rate / drift signal → nothing corrects passivity.

## 3. The actual-flywheel model (the parts)
- **Axle / bearing** = deterministic dispatch (daemon + `harmonik digest` + queue-pressure refill) — low-friction execution.
- **Governor** = the sentinel/adversary — keeps RPM from dropping (anti-decay; *negative* feedback).
- **Fuel / momentum** = **work-generates-work** (*positive* feedback — the piece prior designs under-emphasized): completed work spawns its own follow-ups + deploy + verify; logmine harvest and review→fix loops refill the queue *from the system's own output*, so motion COMPOUNDS instead of decaying to idle-then-restart. (Today is a live example: the keeper-warn fix generated the /quit excision, the restart-now bug, and this redesign — work spawning work.)
- **Steering** = goals + judgment (goal-keeper + captain).
- **Energy top-up** = the operator (rank a brand-new initiative) — occasional, NOT the restarter.

Self-reinforcement = when the system's outputs become its next inputs. Both loops required: **negative** (sentinel stops it stopping) **+ positive** (work-generates-work makes it speed up).

## 4. Component #1 — fresh-context overruling adversary  [ADOPTED — primary bet]
A scheduled **separate-session** crew reads the captain's recent comms/commits and emits a **binding** drift/idle verdict over the bus that can **re-task** the captain. Separation is the active ingredient: self-critique is biased to approve (perplexity self-preference + sycophancy-under-rebuttal; in-session-twice scores *worse* than once); a fresh-context review of the same content as a "foreign artifact" lifts correction **23–93pp** (arXiv 2606.05976, DEBATE ACL-2024). This is the load-bearing fix for over-deference. (= the goal-keeper + the sentinel, framed adversarially.)

## 5. Component #2 — the sentinel  [ADOPTED + requirements]
Make "off-track" a **`harmonik digest` exception that structurally BLOCKS the all-clear** — the same proven pattern as `decision_required`-blocks-dispatch — so the system *cannot proceed while idle/drifted*, rather than relying on the LLM to notice.

- **Activation = INVERSE of system movement** (continuous, not binary): movement metric per window; high movement → sentinel dormant; as movement decays, scrutiny escalates. A governor, not an on/off trip.
- **GAN / adversarial framing:** captain = generator (activity + "all-clear/nothing-to-do" claims); sentinel = discriminator challenging whether that's *real progress* vs idle-dressed-as-busy / over-deference / drift.
- **Movement metric weighted to REAL PROGRESS, not chatter** [operator AGREED]: HEAD-advances / beads-closed / runs-completed dominate; comms volume counts little (else the captain games it by talking). Judge progress, not activity-theater.
- **Equation is PER-PROJECT CONFIG-EDITABLE & ITERABLE** [operator requirement]: the activation fn + thresholds live in `.harmonik/config.yaml` (same surface as the keeper section from `hk-lhu2`), so it can be tuned per project.
- **Captain LEGITIMATE-OVERRULE-STOP** [operator requirement]: the captain must be able to declare a real halt (e.g. disk-space/ENOSPC, a genuine infra blocker) that the sentinel **respects** (surface the blocker), NOT fight. The sentinel must distinguish "stopped for a real reason" from "drifted."
- **MODES — don't over-fire** [operator]:
  - *execute-backlog* mode: ready work + idle → fire.
  - *design / issue-clearing* mode: when the operator is actively designing/discussing, or work is clearing-issues-to-unblock-progress → **quiet** (that IS progress of another kind). Quiet-trigger ≈ active operator dialogue / a declared design phase.
  - **cold-start guard** [operator "very good catch"]: warm-up grace after start/restart; require *sustained* low-movement, not a momentary lull (a fresh/just-restarted system has naturally-low movement but is fine).
- **3-way idle states** (the over-fire problem is mostly getting this split right):
  - (a) idle **+ ready/unblocked work OR undeployed/untested code** → **TRIP** (the target state to solve).
  - (b) idle **+ all work genuinely BLOCKED** → *surface-the-blockers* (a different channel), NOT a sentinel fire.
  - (c) idle **+ genuinely no work** → healthy quiescence, stay dormant.
- **"Actionable work" includes UNTESTED/UNDEPLOYED code**, not just ready beads — the exact failure this session (the keeper-warn fix sat landed-but-undeployed because the captain punted the deploy).

## 6. Component #3 — goal-persistence  [INJECTION REJECTED]
Operator: *"we don't need to inject anything — just need clear goals, correct session restarts, and a way to get the captain realigned."* So **drop** the Letta core-memory-block / Ralph-loop per-turn injection. Instead:
- **Clear goal-state** — a durable source/file (e.g. `.harmonik/intent/goal-state.json`: objectives, antigoals, recent verbatim operator directives, `last_event_id` cursor), maintained by a **goal-keeper agent** (ephemeral, `harmonik schedule`-spawned, minimal-context: reads `comms log --from operator` → distills → rewrites goal-state → exits; never needs the keeper cycle).
- **Correct session restarts** — restart-now / clear→resume re-reads goal-state on resume (now working: `hk-4zy9` deployed, restart-now `--agent` fixed in `hk-fhsp`).
- **Idle-triggered realign** [operator: NOT a 12m timer] — re-assess/realign fires when the captain is idle / "thinks it has nothing to do," NOT on a clock (a busy captain already knows its initiatives).

## 7. Captain ↔ Flywheel relationship
The Captain did **not** supersede the Flywheel — the Flywheel is the structural fix for the Captain's failures. **Captain = a full-agent loop** (the LLM *is* the loop → drifts/over-defers/bloats). **Flywheel = a deterministic skeleton + LLM-as-called-judgment-function** (a deterministic skeleton *can't* over-defer/forget-to-dispatch). Two layers: the Flywheel loop is the **skeleton** (deterministic dispatch/refill/idle+drift detection); the Captain is the **judgment organ** plugged in (rank new initiative, name lane, adjudicate drift). Reuse the existing cognition-loop's deterministic pieces (digest projector, watermark, two-phase done, queue-pressure) — but re-spec around §1–§3.

## 8. External prior-art mapped (cross-pollination)
Magentic-One dual-ledger progress-gate (deterministic non-progress counter forces re-plan) · Letta/MemGPT core-memory block (rejected per #3) · Letta sleep-time consolidation (idle-time re-ground off the hot path → goal-keeper) · Ralph-loop harness re-injection (rejected per #3) · **fresh-context adversary that can overrule (= §4, the load-bearing fix)** · NeMo deterministic tripwire (code halts on invariant breach → the captain-overrule-stop + digest-exception). Common thread: **independence or determinism, never more prompt text.**

## 9. Open questions / next steps
1. **Re-spec as a kerf work** around "self-reinforcing motion" (positive work-generates-work loop + negative sentinel loop + goal-keeper + deterministic skeleton), rescoping the old cognition-loop. *(Operator was asked whether to kick this off with a fresh captain — pending.)*
2. Define the **movement metric** precisely (which events, weights, window).
3. Define the **work-generates-work** sources concretely (follow-up-bead generation, harvest, review→fix) — the positive-feedback loop is the least-specified piece.
4. Define the **mode-detection** (how the system knows it's in a design/issue-clearing phase vs execute-backlog).
5. `operatorAttached` keeper gate refinement (separate, noticed today): an attached-but-idle operator pins a session at 60%+ forever because the keeper won't `/clear` under an attachment — consider an attached-but-inactive grace or an operator-consented clear-now. (Logged for `session-keeper`.)

## 10. Session provenance
Designed across the 2026-06-14 captain session via a 6-agent fan-out (transcript-behavior, /quit-source, restart-now-deploy, contract-redesign, buffer/token, prior-art) + a flywheel creative fan-out (goal-keeper, focus-injection, mechanistic, why-it-keeps-failing, external-prior-art) + live operator design dialogue. Full agent findings are in `hk-0oca` comments.
