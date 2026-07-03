# Note — A–F blocker convergence (18 adversarial reviewers + 6 synth, 2026-06-15)

> Operator-priority-1 deliverable. 3 INDEPENDENT adversarial reviewers per open blocker (angles:
> minimalist / skeptic-adversary / architecture-fit), then per-blocker CONVERGE-vs-SPLIT synthesis.
> **RESULT: 5 of 6 CONVERGED (locked); exactly ONE operator-only decision (C).** Workflow
> `wf_e222e0c6-26b`. These feed the kerf decompose/design passes.

## ⭐ THE SINGLE OPERATOR DECISION — Blocker C, v1 positive-loop shape
Everything else in C **converged**: v1 = the *merged-but-undeployed* source only (defer review→fix /
harvest / declared-follow-ups); `done_definition` lives **per-CLASS in `.harmonik/config.yaml`**,
default = `merged`; deploy/verify of harmonik-on-itself is **STAGED / captain-greenlit, NEVER
autonomous** (self-deploy races daemon merges + can pkill the dispatcher); **"verified" = a per-class
declared command asserting an OBSERVABLE post-condition** (e.g. the restarted daemon answers
`queue status`), run *outside* the work-loop — not an always-exit-0 probe.

**The fork (only the operator can pick the risk/scope appetite):** for the merged-but-undeployed tail, does v1…
- **(A) DETECTOR-ONLY** — a deterministic Go detector surfaces "landed-but-undeployed" as a digest
  `decision_required` exception on the existing rail; **no bead is auto-created**. *Thinnest; ships
  without generator/WIP-ceiling/ledger — but no new bead enters the backlog, so "work-generates-work"
  is satisfied by detection alone (weaker positive loop).*
- **(B) STAGED-BEAD GENERATOR** — emit a **staged deploy+verify bead** via the existing eagerfill
  refill path with all 4 guardrails (rule-only / land-open / WIP==max_concurrent / at-most-once ledger
  keyed on `(target_bead_id, follow_up_class)`). *Literal work-generates-work, reuses existing
  machinery; captain-greenlit, never auto-deployed.*
- **(C) SHIP A NOW, add B** if observation shows detection-alone goes quiescent. *Lowest risk; defers
  the "does the loop self-refill" answer one iteration.*

**kynes lean:** B is the true positive loop the operator asked for; A→B (option C) is the safest path to it.

## LOCKED by 3-way adversarial convergence

### A — Graft vs rebuild → **GRAFT incrementally onto the live captain+daemon**
Do NOT rebuild the Architecture-B cognition-loop that replaces the interactive captain. Reuse shipped
primitives: digest-exception / `decision_required`-blocks-dispatch as the sentinel (new field on the
same digest struct), a `sentinel:` block in the versioned `.harmonik/config.yaml`, eager-refill rules
for work-generates-work, a `harmonik schedule`-spawned ephemeral goal-keeper, and the fresh-context
overruling adversary as a separate scheduled crew on the comms bus. Captain stays the judgment organ.
Ship the **thinnest negative-loop slice first** (idle-with-actionable-work exception + independent
re-tasking adversary) — which also falsifies the whole approach early if over-deference proves
un-fixable from outside.

### B — Movement metric → **weighted terminal-progress events, ~30m window, inverse discrete, opportunity-gated**
Movement = a weighted sum of **TERMINAL-progress** events already in `.harmonik/events/events.jsonl`
(`bead_closed`, `run_completed{success}`, HEAD-advance / reviewer-approve weighted high; all
starts/chatter — `run_started`, `launch_initiated`, `agent_message`, presence, heartbeat — weight 0),
over a **~30-min sliding window**, weights/window/warmup in a per-project `sentinel:` config block
(sibling to `keeper:`). Activation is **INVERSE and DISCRETE** (a step/staircase, auditable — not a
smooth EWMA), **gated on opportunity** (≥1 ready bead OR undeployed/unverified landed code),
suppressed by a cold-start warm-up watermark, and trips only on **SUSTAINED (≥2 consecutive windows)**
low movement → a digest entry that blocks the all-clear, cleared by a captain-overrule ack token.
**Defer** crew-utilization, mode-detection nuance, and a continuous-governor curve to **v2** once v1
firing is observed against the real stream.

### D — Mode-detection → **deterministic resolver, execute-backlog default, self-expiring suppression**
LLM-free resolver in the sentinel/digest builder with an asymmetric safe default: **EXECUTE-BACKLOG
(idle+ready-work trips) is the DEFAULT**; DESIGN/ISSUE-CLEARING quiet is only a **SELF-EXPIRING
suppression**, never a sticky inferred state. Suppression granted by durable surfaces the daemon
already owns: keeper `operatorAttached()` (`internal/keeper/cycle.go:671`) **AND** operator-dialogue
recency from `comms log --from operator`, both gated by a config-tunable **decaying TTL + an
attached-but-inactive timeout** so the known operatorAttached-pins-forever bug cannot silently disarm
the sentinel; an optional operator phase flag in the `sentinel:` config block may force suppression
**but only with a mandatory expiry**. **Issue-clearing is NOT a mode** — a genuinely-progressing clear
emits beads-closed/HEAD-advances the movement governor already credits (stays dormant); a STALLED
clear correctly trips. Cold-start warm-up = a **separate** deterministic gate. On trip: ONE
dispatch-blocking digest exception naming the un-dispatched bead IDs, NOT a repeating warn-nag.

### E — Sentinel authority & trigger → **deterministic bindingness; movement-gated continuous trigger**
Bindingness is **DETERMINISTIC, not authority-based**: the adversary's only power is to write ONE
`decision_required`-class exception under `.harmonik/` (reuse the shipped DecisionBlocker / ack-token
machinery, `decision_block_ev043a.go`), which the deterministic digest projector structurally blocks
the all-clear on — the captain cannot return "nothing to do" while it is pending, and it clears ONLY
on observed real movement (HEAD-advance / bead-closed / run-completed in-window) or a captain-recorded
legitimate-halt reason that the **NEXT** adversary pass re-adjudicates, **never on bare self-ack**.
Independence = running the adversary as a **separate scheduled fresh-context session** reviewing the
captain's comms/commits as a foreign artifact. **Trigger:** a cheap LLM-free continuous
**movement-governor** fires the expensive LLM adversary on **sustained-low-movement-with-actionable-work
past warm-up** — so "idle" ≡ **zero-real-progress-in-window** (not pane-idle), folding the
busy-but-stuck failure mode into the gate. A low-frequency scheduled cadence is a deferrable config
backstop.

### F — Safety gates → **G-liveness before v1; auto-dispatch yes, auto-deploy/push human-gated**
Require **G-LIVENESS before v1** — the one genuinely-new gate: a doom-loop self-kill built by
**INVERTING the flywheel's own movement metric** (N cycles with no HEAD-advance / bead-close /
run-complete → halt + page) — plus a provenance/WIP bound so the work-generates-work loop only
enqueues deploy+verify follow-ups of its own merged commits as `open` (never same-tick auto-dispatch;
refill ceiling == `max_concurrent`). Ship **auto-DISPATCH** in v1 behind that; keep
**auto-DEPLOY/PUSH human-gated** (integration→main stays a human `harmonik promote`), which lets the
rest of **G-security** + **G-cost** ride the ALREADY-SHIPPED daemon fail-closed gates
(`--protect-branch` / `--forbid-default-main`, no-force-push refusal, per-run budget, review-loop-on)
— extend, don't rebuild. **G-test** (replay harness) and **G-inspect** (tmux) are **DEFERRABLE** (the
scenario harness substantially covers replay; the sentinel runs in a tmux pane). The moment v1 would
push unattended or run sentinel in ACT mode, **G-cost** (per-day kill-switch), **G-security**
(no-force-push allowlist on new loop actions), and **G-test** (flywheel-specific replay) all
**promote to non-negotiable**.

## NET
A–F collapses to **a single operator decision** = C's v1 shape (detector-only / staged-generator /
A-then-B). Everything else is locked and graft-able onto the live captain+daemon with existing
primitives. On the operator's C pick → turn the locked set into the kerf spec + a v1 bead slice.
