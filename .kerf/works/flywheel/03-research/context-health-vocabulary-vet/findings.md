# Vet — Context-health 75/85/95 banding + 4 strategies (gateway leverage item #3)

> Component: `context-health-vocabulary-vet`. Round-4 vet. Source: sub-agent (opus), 2026-05-30. **Honest verdict: only the band-name vocabulary survives mapping; no new behavior.**

## TL;DR
- Of the gateway's 4 strategies, only `fresh_start` survives an honest mapping onto our spec; `summarize_and_continue` and `graceful_handoff` collide with **CL-013 / CL-INV-001** (LLM in the deterministic floor) and **CL-020** (no mid-stream trimming); `checkpoint_and_restart` reduces to `fresh_start` because **CL-040/041** already makes durable notes survive recycle.
- We should adopt the **band-naming vocabulary** (`healthy / warning / critical / emergency`) as cosmetic alignment but keep our 70/90/100 thresholds and our single recycle action — **net: a renamed `FullnessBand` enum and one new CL clause, no new behavior.** One small docs bead (P3); no implementation bead.

## §1 TS shape (file:line)
`context-rotation.ts:29-33` declares the 4 strategies; `:38-42` declares thresholds 75/85/95; `:116-124` defaults to `checkpoint_and_restart` + autoRotate. `:239` shows `needsRotation` fires ONLY on `emergency` (warning/critical do NOT trigger strategy execution).

Strategies:
- `rotateSummarizeAndContinue` (`:317-356`) sends a `summarizationPrompt` to the live agent and asks it to compact in place.
- `rotateFreshStart` (`:362-400`) checkpoints, spawns new agent with `agent.config` (no history), terminates old.
- `rotateCheckpointAndRestart` (`:406-464`) checkpoints, restores `conversationHistory + toolState` into the new agent (full state transfer).
- `rotateGracefulHandoff` (`:470-526`) asks the old agent to write a handoff document and seeds the new agent's system prompt with it.

`context-health.service.ts:360-468` band→action map: WARNING=emit-event-only, CRITICAL=`compact()` (LLM summarize+prune in place), EMERGENCY=`rotate()` (which itself builds an LLM `transfer` summary at `:694-713`). Three of the four strategies depend on LLM-generated summary text.

## §2 Our cognition-loop position (CL-NNN)
CL-011 enforces 70/90/100 with band labels `{nominal, soft, strong, critical}`; at ≥100 the harness forces recycle. CL-020 mandates regime-B fresh substrate per recycle; mid-stream trimming explicitly FORBIDDEN. CL-024 specifies recycle: persist digest → tear down → fresh session seeded with `[stable prefix | digest]`. CL-040–042 already make notes durable across recycles. CL-013 + CL-INV-001 require byte-clean mechanism/cognition separation — LLM-generated text MUST NOT live in the deterministic floor.

## §3 Strategy-by-strategy mapping
- **`summarize_and_continue` → OUT.** Asks the live agent to summarize and keeps the same session running; that's mid-stream compaction (cache-hostile, violates CL-020) and bakes LLM-generated text into the floor (violates CL-INV-001). Their own code at `:341` even concedes the pattern is "wait for the response and inject as new context start" — exactly the cache-hostile pattern.
- **`fresh_start` → MAPS DIRECTLY to CL-024 recycle.** Spawn new session, terminate old, seed with config (in our case: stable prefix + digest). Adopt the name.
- **`checkpoint_and_restart` → COLLAPSES into fresh_start.** Their distinction is restoring `conversationHistory + toolState` into the new agent — but we explicitly DON'T want conversation history transferred (CL-020; the whole point of regime-B is the prefix-cache hit). The "checkpoint" role of state-preservation IS our digest + notes.jsonl (CL-040–042 already mandate survival). No new behavior.
- **`graceful_handoff` → OUT.** Their handoff payload is an LLM-generated handoff document seeded into the new agent's system prompt. That mutates the stable prefix (violates CL-021 / CL-INV-003 byte-stability) AND inserts cognition into mechanism. Also designed for cross-agent handoff, which CL-002 (single-instance lock) doesn't have.

## §4 Vet — real wins vs jargon
Three of four strategies require LLM-in-the-loop summary text we explicitly reject. The fourth is what we already specified in CL-024. **The vocabulary gives us nothing operationally new.**
What it DOES give us: (a) clearer band naming — "critical" appearing at different positions in both ladders is confusing; (b) a thinking aid making explicit that at 70% we nudge / at 90% we strongly nudge / at 100% we recycle is essentially their three-band model. Their fourth `healthy` band is just `<warning`. Our existing 4-band `FullnessBand` maps 1:1 to `{healthy, warning, critical, emergency}` — pure rename. **No threshold change recommended:** 70/90/100 is grounded in MemGPT research (findings.md §Q4b); 75/85/95 is an unjustified gateway-local tuning.

## §5 Proposed CL-NNN amendment text

> **CL-014 — Band-name vocabulary alignment.** The four fullness bands defined by CL-011 MUST use the names `{healthy, warning, critical, emergency}` for `{<70, ≥70<90, ≥90<100, ≥100}` respectively, replacing the prior `{nominal, soft, strong, critical}` names. Threshold values unchanged. `FullnessBand` type in §6 MUST be updated. Rationale: vendor-neutral readability alignment with the flywheel_gateway context-health vocabulary (`apps/gateway/src/services/context-rotation.ts`) without altering behavior.
>
> **CL-015 — Recycle is the only strategy.** The harness recognizes exactly one rotation action at the `emergency` band: fresh-start recycle per CL-024 (substrate teardown + fresh session seeded with stable prefix + digest). The gateway-vocabulary strategies `summarize_and_continue`, `checkpoint_and_restart`, and `graceful_handoff` are explicitly NON-CONFORMANT for flywheel because: (a) `summarize_and_continue` requires LLM-generated text in the deterministic floor (violates CL-INV-001) and mid-stream message-array mutation (violates CL-020); (b) `checkpoint_and_restart` transfers full `conversationHistory` into the new session, defeating the prefix-cache hit CL-021/CL-023 require, and its state-preservation role is already filled by durable notes (CL-040–042) + digest (CL-030); (c) `graceful_handoff` mutates the stable prefix with an LLM-authored handoff document (violates CL-INV-003) and presupposes multi-agent topology incompatible with CL-002. Operator-configurable strategy selection is NOT exposed at v0.1.

## §6 Proposed bead
```
Title:  Adopt gateway band-name vocabulary in cognition-loop.md (CL-014 + CL-015)
Type:   docs
Priority: 3
Labels: codename:flywheel
Description:
Amend specs/cognition-loop.md to add CL-014 (band-name rename to
healthy/warning/critical/emergency at the existing 70/90/100 thresholds) and CL-015
(explicit non-conformance of summarize_and_continue / checkpoint_and_restart /
graceful_handoff strategies, with rationale citing CL-INV-001, CL-020, CL-021,
CL-023, CL-INV-003, CL-002). Update §6 FullnessBand type accordingly. Update §10
revision history.

Acceptance criteria:
- CL-014 + CL-015 added to §4.2 (architecture invariants).
- FullnessBand type renamed in §6 table.
- All other references to {nominal,soft,strong,critical} band names updated.
- §10 revision history entry added.
- No threshold values change (70/90/100 preserved).
- Reviewer-agent APPROVE verdict on the amendment.
```

Bead 2 is NOT proposed — there is no new behavior to implement; the amendment is pure spec text.
