# Spec — flywheel-motion (self-reinforcing motion)

> **STATUS: NORMATIVE.** All design blockers resolved by 18-reviewer convergence; C resolved to (B)
> staged-bead generator (operator delegated the pick to the captain 2026-06-16; kynes fresh-analysis
> concurred). kynes 2026-06-16. v1 components FW1–FW3, FW5–FW6, AC1–AC3, AC5 landed on `main`
> (2026-06-21); FW4 (adversary spawn, hk-jsvc) + AC4 (legitimate-halt clear, hk-jvul) pending — see §9.1.
> Spec finalized from kerf bench draft (`flywheel-motion/05-spec-drafts/flywheel-motion.md`) by ED1
> (hk-zf4n). Design vision: `docs/flywheel-self-reinforcing-design.md`. Problem space:
> `~/.kerf/projects/gregberns-harmonik/flywheel-motion/01-problem-space.md`. Pinned bead: `hk-0oca`.

---

## 0. Overview

### 0.1 Two-loop model
The flywheel is **two coupled feedback loops** grafted onto the live captain+daemon:

- **Negative loop (the sentinel/governor).** Keeps RPM from dropping. A deterministic movement-governor
  detects sustained low real-progress while actionable work exists; an independent fresh-context adversary
  adjudicates and writes ONE dispatch-blocking digest exception that the captain cannot all-clear past.
- **Positive loop (work-generates-work).** Makes RPM rise. The system's own terminal outputs
  (merged-but-undeployed code first) deterministically refill the backlog, so motion compounds instead of
  decaying to idle-then-operator-restart.

Both loops are REQUIRED. The negative loop stops the system stopping; the positive loop makes it speed up.

### 0.2 GRAFT posture [blocker A — locked]
The system MUST be built as an **incremental graft onto the live captain+daemon**, reusing shipped
primitives. It MUST NOT rebuild the Architecture-B cognition-loop that *replaces* the interactive captain.
The captain REMAINS the judgment organ (rank a brand-new initiative, name a lane, adjudicate drift); the
flywheel is the deterministic skeleton that *forces judgment to be exercised*, not a substitute for it.
The thinnest negative-loop slice MUST ship first (§8) so the whole approach is falsified early if
over-deference proves un-fixable from outside the captain's context.

### 0.3 Decisive principle [locked]
> **Drift and over-deference are beaten by INDEPENDENCE or DETERMINISM — never by more prompt text in the
> same context.**

Every mechanism in this spec is either (a) deterministic Go in the digest/daemon layer, or (b) a
fresh-context independent session. No part of the fix is "stronger wording" in the captain's own context;
any such proposal MUST be rejected as the historically-failing class (≥3 regressions in a month).

---

## 1. Movement metric [blocker B — locked]

### 1.1 Definition
The sentinel SHALL compute **movement** as a weighted sum of **terminal-progress** events read from
`.harmonik/events/events.jsonl` over a sliding window. Only terminal-progress events carry weight; all
start/chatter events carry weight **0** (so the captain cannot game the metric by talking).

| Event | Class | Default weight |
|---|---|---|
| `bead_closed` | terminal progress | high |
| `run_completed{status=success}` | terminal progress | high |
| HEAD-advance on `origin/main` | terminal progress | high |
| `reviewer_verdict{APPROVE}` | terminal progress | high |
| `run_started`, `launch_initiated` | start | **0** |
| `agent_message`, presence, `heartbeat` | chatter | **0** |

The exact numeric weights are config (§7); the table fixes which events are *eligible*.

### 1.2 Window & shape
- **Window:** a ~30-minute sliding window (config `window`).
- **Activation is INVERSE and DISCRETE.** Activation = a step/staircase function of movement (high
  movement → dormant; low movement → escalated scrutiny), computed as **discrete, auditable steps** —
  NOT a smooth EWMA. An operator MUST be able to read the events.jsonl window and reproduce the activation
  level by hand.

### 1.3 Opportunity gate
The governor MUST NOT trip unless **opportunity** exists: **≥1 ready bead OR landed-but-undeployed/unverified
code** (the Phase-2 tail of §5). Zero-movement with genuinely-no-work is healthy quiescence and MUST stay
dormant.

### 1.4 Cold-start & sustained gates
- **Cold-start warm-up watermark.** A fresh or just-restarted system has naturally-low movement; the
  governor MUST suppress activation until a `warmup_window` watermark has elapsed/accumulated. This is a
  SEPARATE gate from mode-suppression (§3).
- **Sustained-low requirement.** The governor SHALL trip only on **sustained** low movement — ≥2
  consecutive low windows (config `sustained_windows`, default 2) — never a momentary lull.

### 1.5 v2 deferrals (NOT in v1)
The following are explicitly DEFERRED to v2 and MUST NOT block v1:
- **Crew-utilization axis** (captain-working + crew-idle = a soft signal). v1 keys movement on
  terminal-progress regardless of which actor produced it.
- **Continuous-governor curve.** v1 uses the discrete staircase (§1.2); a smooth continuous activation
  curve is a v2 refinement once v1 firing is observed against the real stream.

---

## 2. Sentinel authority & trigger [blocker E — locked]

### 2.1 Bindingness is DETERMINISTIC, not authority-based
The adversary's ONLY power SHALL be to write ONE `decision_required`-class exception under `.harmonik/`,
reusing the **shipped DecisionBlocker / ack-token machinery** (`internal/daemon/decision_block_ev043a.go`;
the `.harmonik/decision_acks/<ack_token>` ack-state files; the EV-044 digest summary in
`internal/digest/builder.go:165` / `internal/digest/types.go:50`). The deterministic digest projector then
**structurally BLOCKS the all-clear** on that exception: **the captain CANNOT return "nothing to do" while
it is pending.** Bindingness comes from the projector, not from any claimed authority of the adversary
session.

### 2.2 Clearing rule
The exception SHALL clear ONLY on one of:
1. **Observed real movement** in-window — a HEAD-advance, `bead_closed`, or `run_completed{success}`; OR
2. **A captain-recorded legitimate-halt reason** (e.g. ENOSPC, a genuine infra blocker) that the **NEXT**
   adversary pass re-adjudicates.

The exception SHALL **never** clear on a bare captain self-ack. A legitimate-halt is a *recorded reason
subject to re-adjudication*, not a self-issued dismissal.

### 2.3 Independence (the active ingredient)
The adversary MUST run as a **separate scheduled fresh-context session** (`harmonik schedule` / spawn-crew),
reviewing the captain's recent comms/commits as a **foreign artifact**. Self-critique in the captain's own
context is rejected: it is biased to approve (self-preference + sycophancy-under-rebuttal). Fresh-context
review of the same content lifts correction materially (design §4).

### 2.4 Trigger — movement-gated, not clock-gated
- A **cheap, LLM-free, continuous movement-governor** (§1) fires the **expensive LLM adversary** on
  **sustained-low-movement-with-actionable-work past warm-up**. The expensive judgment is gated by the
  cheap trip-wire; the adversary is not run on a hot clock.
- **"Idle" ≡ zero-real-progress-in-window** (NOT pane-idle). This definition folds the *busy-but-stuck*
  failure mode into the gate: a captain typing endlessly with no terminal progress is idle for sentinel
  purposes.
- A low-frequency scheduled cadence MAY exist as a deferrable config backstop; it is not the primary
  trigger.

---

## 3. Mode-detection / suppression [blocker D — locked]

### 3.1 Deterministic resolver, asymmetric default
A **deterministic, LLM-free resolver** in the digest builder SHALL decide whether a §1 trip fires or is
suppressed. The default is asymmetric and safe:

- **EXECUTE-BACKLOG (idle + ready work → TRIP) is the DEFAULT.**
- **DESIGN / ISSUE-CLEARING quiet is ONLY a self-expiring suppression** — never a sticky inferred state.

### 3.2 Suppression sources (all decaying)
Suppression MAY be granted only by durable surfaces the daemon already owns, each gated by a config-tunable
**decaying TTL plus an attached-but-inactive timeout**:

| Source | Surface | Guard |
|---|---|---|
| Operator attached | keeper `operatorAttached()` (`internal/keeper/cycle.go:671`) | TTL + attached-but-inactive timeout so the known *operatorAttached-pins-forever* bug cannot silently disarm the sentinel |
| Operator dialogue recency | `comms log --from operator` | decaying TTL |
| Operator phase flag (optional) | `sentinel:` block in `.harmonik/config.yaml` | MUST carry a **mandatory expiry**; a flag without expiry is invalid config |

### 3.3 Issue-clearing is NOT a mode
A genuinely-progressing issue-clear emits `bead_closed`/HEAD-advances that the §1 governor already credits,
so the sentinel stays dormant on it WITHOUT any mode classification. A STALLED clear correctly trips. The
resolver MUST NOT attempt to infer an "issue-clearing mode."

### 3.4 Cold-start is separate
The cold-start warm-up (§1.4) is a SEPARATE deterministic gate, NOT a suppression mode.

### 3.5 On trip
On a fired, unsuppressed trip the system SHALL emit **ONE dispatch-blocking digest exception naming the
un-dispatched bead IDs** (and/or the un-deployed tail). It MUST NOT emit a repeating warn-nag.

---

## 4. Goal-persistence [design #3 — NO injection — locked]

### 4.1 Goal-state file
The system SHALL maintain a durable **goal-state file** (e.g. `.harmonik/intent/goal-state.json`)
containing at minimum:
- `objectives` — current standing goals;
- `antigoals` — explicit things to avoid;
- `operator_directives` — recent **verbatim** operator directives (guidance, not standing law — a handoff's
  inherited operator request is guidance that get-shit-done overrules, per design §11.1);
- `last_event_id` — a cursor into `events.jsonl`/comms so the goal-keeper distils incrementally.

### 4.2 Goal-keeper agent (ephemeral)
Goal-state SHALL be maintained by an **ephemeral goal-keeper agent**, `harmonik schedule`-spawned, running
minimal-context:
> read `comms log --from operator` (since `last_event_id`) → distil → rewrite goal-state → exit.

It MUST NOT require the keeper cycle and MUST NOT be a long-lived session.

### 4.3 Restart re-grounding
A correct session restart (clear→resume / restart-now) SHALL **re-read goal-state on resume** to re-ground
the captain.

### 4.4 Idle-triggered realign
A realign/re-assessment SHALL fire when the captain is **idle or "thinks it has nothing to do"** — NOT on a
fixed clock timer (the dropped 12-minute tick). A busy captain already knows its initiatives.

### 4.5 NO injection (locked)
Per-turn goal injection (Letta core-memory block / Ralph-loop re-injection) is **REJECTED**. Goal-persistence
is achieved by durable goal-state + correct restarts + idle-realign ONLY.

---

## 5. Work-generates-work + two-phase done [blocker C — RESOLVED → (B) staged-bead generator]

> All of §5 is now normative. The v1 positive-loop shape is **(B) the staged-bead generator** — the
> operator delegated the pick to the captain (2026-06-16) and kynes fresh-analysis concurred: B delivers
> the self-refilling positive loop the operator named as the flywheel's purpose, and its only added surface
> (the eagerfill generator + ledger) is bounded because the staged bead is captain-greenlit, never
> auto-deployed.

### 5.1 Scope (converged)
v1 SHALL handle **only the merged-but-undeployed source**. Review→fix, logmine/harvest, and
explicitly-declared follow-ups are DEFERRED to v2.

### 5.2 Two-phase `done_definition` (converged)
Completion SHALL be a per-CLASS `done_definition` living **per-class in `.harmonik/config.yaml`**:
- **Phase 1 — merged:** the `run_id` trailer is on `origin/main` (today's notion of done). **Default
  `done_definition` = `merged`** — no surprise infinite tails.
- **Phase 2 — terminal:** deployed + verified, for deploy-relevant classes that opt in.

Until Phase 2, the work is an **in-flight tail** that **counts as ACTIONABLE WORK** for the §1 opportunity
gate — the system CANNOT go quiescent while a tail exists.

### 5.3 "Verified" (converged)
"Verified" SHALL be a **per-class declared command asserting an OBSERVABLE post-condition** (e.g. the
restarted daemon answers `queue status`), run **outside** the work-loop. It MUST NOT be an always-exit-0
probe. A `done_definition` value of `true`, `:`, or whitespace-only is REJECTED at config-load time
(`LoadSentinelConfig` returns `ErrTrivialVerifyCommand`).

Deploy/verify of harmonik-on-itself SHALL be **STAGED / captain-greenlit, NEVER autonomous** (self-deploy
races daemon merges and can `pkill` the dispatcher).

### 5.4 v1 positive-loop shape — (B) STAGED-BEAD GENERATOR [resolved]

On a Phase-1 completion of a deploy-relevant class, the system SHALL emit a **staged deploy+verify bead**
via `stagedBeadGeneratorEval` in `eagerfill_em063.go`, which calls `br create` directly (not the
`AppendItems` eager-refill pipeline — the file is shared, the code path is distinct), with all four
guardrails:
1. **Rule-only** — generated only from a declared rule, never LLM-invented (no speculative beads in the
   refill path).
2. **Land-open** — loop-created beads land **`open`**, never auto-dispatched the same tick (the created
   bead lands open via `br create --status open`; never auto-dispatched the same tick).
3. **WIP == `max_concurrent`** — refill ceiling equals `max_concurrent`, so over-aggression is structurally
   impossible.
4. **At-most-once ledger** — idempotency via `reacted_ledger` keyed on `(target_bead_id, follow_up_class)`,
   so a deploy+verify follow-up is spawned exactly once (no duplicate tails). Ledger is persisted to disk
   (AC1, hk-3ndb) so a daemon restart does not double-emit.

The staged bead is captain-greenlit, NEVER auto-deployed (see §5.3 and §6.2). This is the literal
work-generates-work loop: a merged-but-undeployed unit deterministically generates its own deploy+verify
successor as `open` backlog, so motion compounds rather than decaying to idle.

> **Decision record (2026-06-16).** Two alternatives were considered and rejected for v1: **(A) detector-only**
> — flags the undeployed tail as a digest exception but creates no bead, so no new work enters the backlog
> (detection-as-momentum only → weaker positive loop); and **(C) ship-A-then-B** — lowest risk but defers
> the "does the loop self-refill?" answer one iteration. **(B) chosen** (operator-delegated to captain; kynes
> concurred): it is the true positive loop, and its only added surface over (A) — the eagerfill generator +
> at-most-once ledger — is bounded by the captain-greenlit gate (never auto-deploys).

---

## 6. Safety gates [blocker F — locked]

### 6.1 G-LIVENESS — required before v1 (the one genuinely-new gate)
A doom-loop **self-kill** SHALL be built by **INVERTING the flywheel's own movement metric** (§1): N cycles
with no HEAD-advance / bead-close / run-complete → **halt + page** (config `liveness_no_progress_n`). This is
the safety counterweight to the sentinel's anti-idle pressure — it prevents a stuck loop from spinning
forever.

### 6.2 Provenance / WIP bound
The work-generates-work loop SHALL only enqueue **deploy+verify follow-ups of its OWN merged commits** as
`open`, **never** same-tick auto-dispatch; refill ceiling == `max_concurrent` (§5.4 B guardrails 2–3).

Provenance gate (AC3, hk-zlwq): the generator gates on a **confirmed `origin/main` landing of the run's
`Refs:` SHA**, not merely a local `runSucceeded` flag. A run that succeeds but whose commit is absent from
`origin/main` spawns NO follow-up.

### 6.3 v1 autonomy posture
- **Auto-DISPATCH** ships in v1, behind G-liveness.
- **Auto-DEPLOY / PUSH stays HUMAN-gated.** integration→main remains a human `harmonik promote`. This lets
  G-security + G-cost ride the ALREADY-SHIPPED daemon fail-closed gates — **extend, do not rebuild**:
  - `--protect-branch` (deny-list, fail-closed at boot/dispatch/merge);
  - `--forbid-default-main` (refuse to start if target resolves to `main`);
  - no-force-push refusal;
  - per-run budget;
  - review-loop-on by default.

### 6.4 Deferrable gates
- **G-test** (replay harness) — DEFERRABLE; the scenario harness substantially covers replay.
- **G-inspect** (tmux) — DEFERRABLE; the sentinel runs in a tmux pane.

### 6.5 PROMOTION RULE (non-negotiable trigger)
The moment v1 would **push unattended OR run the sentinel in ACT mode**, the following PROMOTE from
deferrable to **non-negotiable** and MUST land first:
- **G-cost** — a per-day kill-switch;
- **G-security** — a no-force-push allowlist on the new loop's actions;
- **G-test** — a flywheel-specific replay harness.

---

## 7. Config surface — the `sentinel:` block

A new **`sentinel:`** block SHALL live in `.harmonik/config.yaml`, **sibling to the existing `keeper:`
block** (the established per-project config precedent). Tunables (NEW):

| Key | Meaning | Default |
|---|---|---|
| `mode` | `""` / `"observe"` = evaluate + emit `governor_signal` only; `"act"` = full trip/clear/halt | `""` (observe) |
| `movement_weights` | per-event-type weights for the §1 metric (terminal events high; starts/chatter 0) | `bead_closed`/`run_completed`/`reviewer_verdict` = 10 |
| `window` | sliding-window duration (§1.2) | `30m` |
| `warmup_window` | cold-start watermark before activation is allowed (§1.4) | `30m` |
| `sustained_windows` | consecutive low windows required to trip (§1.4) | `2` |
| `suppression_ttl` | decaying TTL for operator-attached / dialogue-recency suppression (§3.2) | `10m` |
| `attached_inactive_timeout` | attached-but-inactive timeout guarding the operatorAttached-pins-forever bug | `5m` |
| `phase_flag` (optional) | operator-forced suppression — INVALID without `phase_flag_expiry` | unset |
| `phase_flag_expiry` | mandatory expiry for `phase_flag` | — |
| `liveness_no_progress_n` | N cycles with no terminal progress → G-liveness self-kill (§6.1) | `10` |
| `done_definition` (per-class) | `merged` (default) or a Phase-2 deploy+verify class opt-in (§5.2) | `merged` |

---

## 8. v1 minimal slice + deferred

### 8.1 v1 thinnest slice (blocker A — negative-loop-first)
v1 SHALL be the thinnest negative-loop slice that falsifies the approach early:
1. **Movement governor** (§1) — LLM-free, terminal-progress-weighted, discrete, opportunity-gated,
   cold-start- and sustained-gated.
2. **Idle-with-actionable-work digest exception** (§2 + §3.5) — one `decision_required` exception that
   blocks the all-clear, naming the un-dispatched bead IDs / un-deployed tail.
3. **Independent re-tasking adversary** (§2.3) — the fresh-context scheduled session that adjudicates and
   writes the exception.
4. **G-liveness** (§6.1) — the inverted-metric self-kill, required before v1.
5. **Goal-state + idle-realign** (§4) — durable goal-state, restart re-grounding, idle-triggered realign.
6. **Work-generates-work (B)** — the staged-bead deploy+verify generator (§5.4) with the §5.2 two-phase
   `done_definition`, the §5.3 verified-command, and the four guardrails (rule-only / land-open /
   WIP==max_concurrent / at-most-once ledger).

### 8.2 Deferred to v2
- Crew-utilization axis (§1.5);
- Continuous-governor curve (§1.5);
- Work-generates-work sources b/c/d — review→fix, logmine/harvest, declared follow-ups (§5.1);
- ACT-mode auto-deploy / unattended push (§6.5 — gated behind G-cost/G-security/G-test promotion).

---

## 9. Implementation wiring map (v1 — as of 2026-06-21)

This section records which components are live on `main` and which remain pending, so the activation
runbook (`docs/flywheel/sentinel-runbook.md`) can reference a canonical status.

### 9.1 Component status

| Component | Bead | Commit | Status | Notes |
|---|---|---|---|---|
| **FW1** config adapter + deps plumbing | hk-y9fn | `6272ba34` | ✅ LIVE | `LoadSentinelConfig` → `GovernorConfig()` bridge; `governorState`/`governorCfg`/`sentinelMode`/`sentinelPhase2Classes` on `workLoopDeps`; initialized at `daemon.go:1667` |
| **FW2** Evaluate observe-only | hk-z1lr | `c468bed4` | ✅ LIVE | Fires each tick when `sentinel.mode` is `""` or `"observe"`; emits `governor_signal` typed event; no EmitTrip, no halt |
| **FW3** ACT mode trip/clear/halt | hk-4toh | `e2252c3a` | ✅ LIVE | Fires each tick when `sentinel.mode == "act"`; `EmitTrip` on `ActivationActive`; `ClearTrip` on `ActivationDormant`+pending; G-liveness halt+page on `ActivationHalt`; wires `DecisionBlocker.AddQueueBlock("sentinel")` |
| **FW4** adversary spawn | hk-jsvc | (deferred) | ⏳ PENDING | Spawn fresh-context adversary session on trip; bounded by overlap-skip |
| **FW5** goal-keeper schedule seed | hk-z25w | `9c3e442d` | ✅ LIVE | `harmonik init` seeds the goal-keeper schedule entry in `.harmonik/schedules/` |
| **FW6** resume re-grounding | hk-psv4 | `bde324fc` | ✅ LIVE | Captain `/session-resume` re-reads `goal-state.json`; `restart-now` path also triggers re-read |
| **AC1** durable staged-bead ledger | hk-3ndb | `7a47f592` | ✅ LIVE | `reacted_ledger` keyed `(target_bead_id, follow_up_class)` persisted to disk; restart does not double-emit |
| **AC2** captain greenlight gate | hk-lacr | `2b9d16e0` | ✅ LIVE | `needs-greenlight` label blocks dispatch until captain clears; independent of `--no-auto-pull` |
| **AC3** provenance-verified merge | hk-zlwq | `21071f2e` | ✅ LIVE | Generator gates on confirmed `origin/main` SHA landing, not just local `runSucceeded` |
| **AC4** legitimate-halt clear path | hk-jvul | (pending) | ⏳ PENDING | `ClearTrip` with `ack_method:"legitimate_halt"` + next-pass re-adjudication |
| **AC5** verify+clear-trip+spec | hk-kgwv | `0d31cfdc` | ✅ LIVE | `ErrTrivialVerifyCommand` at config-load; `harmonik sentinel clear-trip` operator escape; spec comment reconciled |

### 9.2 What is and is not wired

**Already live (require only config to activate):**
- Governor evaluation fires on every daemon tick once `sentinel:` block is present in config.yaml.
- Observe mode (`sentinel.mode: observe` or unset) emits `governor_signal` events — zero risk; no dispatch side-effects.
- ACT mode (`sentinel.mode: act`) wires trip/clear/halt — HIGH risk; activates only when explicitly set.
- Goal-keeper schedule entry seeded by `harmonik init` — runs on `harmonik schedule` poll.
- Resume re-grounding on restart — live, requires a populated `goal-state.json`.
- Staged-bead generator fires when a Phase-2 `done_definition` class is configured and a bead closes — already executing (the eagerfill path always ran; it now has the durable ledger + provenance + greenlight gate).

**Not yet wired (pending future beads):**
- FW4 adversary spawn (hk-jsvc): the adversary session is not yet auto-spawned on trip; an operator can manually run `harmonik sentinel emit-trip` + spawn a fresh-context session against the exception.
- AC4 legitimate-halt clear (hk-jvul): `ClearTrip` today only writes `ack_method:"governor_movement"` (auto) or `"operator"` (manual escape); a captain-recorded legitimate-halt with next-pass re-adjudication is not yet implemented.

---
