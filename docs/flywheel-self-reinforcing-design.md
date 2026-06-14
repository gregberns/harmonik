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

## 11. Captured from transcript (reconciliation 2026-06-14, crew `kynes`)
A two-agent sweep of the prior captain session transcript (`8f68d96f…`, msgs 124–156) reconciled the operator's verbatim design words against §1–§10 above. Most was already faithfully captured (inverse-movement governor, GAN framing, config-editable equation, captain-overrule-stop, injection-rejection, design-phase-quiet, real-progress weighting, idle-triggered realign, the "opposite of a flywheel" vision). The items below were **missing or under-captured** and are recorded verbatim so nothing is lost. (The current captain session `748968d3…` held no operator design content — it is all restart cycles.)

### 11.1 Over-deference root-cause hypothesis: the handoff embeds the last operator request "as law" [extends §2]
> *"there is text somewhere that is causing the captain to defer all decision making to the operator. The captains instructions should be clear that it can make decisions but it constantly becomes wishy washy. It seems like maybe in the handoff (maybe) it embeds the last operators request and makes that law - instead of taking that as guidance and letting the 'get shit done' protocol over rule it."*

The operator's concrete, testable hypothesis for **where** over-deference is injected — not just that it happens. Design constraint: a handoff's inherited operator request is **guidance**, not standing law; get-shit-done overrules it. (This is exactly what `hk-trg5`, the contract-redesign, landed — but the *root-cause framing* belongs in the flywheel design as the reason the goal-state/restart layer must re-classify inherited "pending" items rather than freeze on them.)

### 11.2 The idle-state model needs a second axis — crew utilization [extends §5 "3-way idle states"]
> *"if the captain is quiet but crew is working - that's excellent. If only the captain is working (assuming a lot of big tasks are needed) and crew is not used then there may be an issue. If captain and crew is NOT woking and there are goals defined, initiatives with no blockers, beads able to be worked, untested or undeployed code - THIS is what we want to try and solve."*

The §5 idle-state split keys only on **captain** idleness. The operator adds a **crew-utilization** axis with three verdicts: (captain-quiet + crew-working = excellent — the goal), (captain-working + crew-unused = a *soft/possible* problem — captain hogging delegable work), (both idle + work-exists = the hard trip to solve). The middle case is a movement-signal nuance the sentinel must model and §5 currently omits.

### 11.3 The keeper-warn message is the proximate stop-working trigger — 5 design requirements [new; relates §2 + §6]
> *"that message almost seems to guarentee the captain stops doing work when there's more todo… First don't say it's approaching its limit because it's not and so then the model argues that. Second say who the message is from (keeper not operator). Third we have the handoff protocol but that's not mentioned. Fourth, that doesn't say it's a warning. Fifth that message almost seems to guarentee the captain stops doing work… get the captain to keep working AND correctly transition so it's contexts stay tight to minimize token usage and keep quality up."*

The doc cites `hk-4zy9` (keeper-warn fix, DEPLOYED) only in passing. The operator's five acceptance criteria for the warn message — (1) don't claim "approaching limit"; (2) attribute sender = keeper, not operator; (3) reference the handoff protocol; (4) label it a WARNING; (5) don't phrase it so the captain stops — are the *proximate* mechanism behind the idle failure the flywheel exists to cure. Keep-working-AND-clean-transition is the design intent behind the deployed fix.

### 11.4 Creative mandate: stop repeating the same fix; combine mechanism + agent [extends §1 / §2]
> *"we should be creative in how we try and solve it. We've kinda been trying the same thing over and over - and it's not working."* … *"we could have a more mechanistic process - or figure out how to use an agent to try and get the system back on track - probably need a combination."*

The operator's explicit directive that produced the §3–§6 shape: a **combination of a deterministic mechanism AND an agent**, not another single prompt-text iteration. Recorded as a guiding principle, not just provenance.

### 11.5 Unsolved: where does `/quit` language originate? [extends §9 open questions]
> *"we need to I understand where the hell all this slash quit messaging is coming from. We battled with this causing sessions not restarting many days ago… Where the hell is slash quit language introduced with the keeper (or wherever) so that there needs to be language to tell the agent that does[n't] need to be done?? That's been driving me nuts."*

The doc references `hk-hxkz` (/quit excision) as done packaging. The operator treats it as an **open root-cause question** in the same "sessions not restarting/continuing" failure family — trace where `/quit` is introduced such that counter-language is even needed. Open item, not resolved.

### 11.6 Nuance tightening [already captured]
- §6 idle-trigger: the operator's exact trigger is two-pronged — *not working* **OR** *thinks it has nothing to do* (doc says only the latter).
