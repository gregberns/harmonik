---
schema_version: 1
crew_name: leto
queue: pi-q
epic_id: hk-nxjwo
captain_name: captain
model: sonnet
---

# Crew mission — leto — PI LIVE-TESTING → HELD (2026-07-03)

> ⛔ CURRENT STATE = **HELD / IDLE ON-CALL.** Pi is PROVEN end-to-end (hk-d5q5l merged
> by a pi agent on gpt-5.4-mini — goal DONE). The scavenger DRAIN is HELD pending an
> operator model decision: gpt-5.4-mini commits trivial doc/string changes but EXITS
> WITHOUT COMMITTING on real Go code (capability ceiling). Do NOT submit any more Pi
> beads until the operator picks doc-only-scope vs a stronger model. On boot: join
> comms, post a boot status, then IDLE on your inbox. Do NOT drain. The history below
> is reference only.
>
> RE-TASKED 2026-07-03 from the Pi Phase-0 BUILD (hk-94c3t, DONE) to PI LIVE-TESTING
> (proven). Your LIVE mission is also the captain comms thread (topic=assign) — trust
> that if this file and comms ever disagree.

## On boot
1. `harmonik comms join` + confirm identity = leto.
2. `br update hk-nxjwo --assignee leto` (mirror for attribution — load-bearing).
3. Post a boot status to captain (`--topic status`) + a journal comment.
4. Arm `harmonik comms recv --agent leto --follow --json` (dedupe on event_id).

## PROGRESS (2026-07-03) — DONE so far
- ✅ hk-sv3vg (~-expansion fix) LANDED (37a4df93).
- ✅ Daemon REDEPLOYED by captain to c5771a5e (includes the fix); last-good pinned;
  tagged daemon-20260703-01. File-key auth path is LIVE.
- ✅ config `harnesses.pi.model` = `openai/gpt-5.4-mini` (set).
- ✅ AUTH PROVEN LIVE: canary hk-nxjwo selected harness=pi, billing guard PASSED,
  authenticated to OpenRouter, reasoned on gpt-5.4-mini ~1min. PI-040 blocker GONE.
  It "failed" only because the canary bead has NO committable task (design gap, not a
  Pi defect). hk-nxjwo is CLOSED-OUT as a canary; pi-q went paused-by-failure from it.

## goal — THE LAST MILE (do this now)

Prove full dot:close with a REAL bead, which also STARTS the scavenger drain:

1. **Clear pi-q** (paused-by-failure from the no-op canary): resume it or submit a
   fresh group; do NOT let hk-nxjwo re-dispatch (it will re-fail).
2. **Pick 1-2 SMALL REAL MECHANICAL scavenger beads** — grep=0 / failing-test->green,
   deterministically checkable, file-disjoint from any active lane (the ~110 banked
   scavenger beads are the FUEL). YOU own selection.
3. **Submit to pi-q on `openai/gpt-5.4-mini` behind the DOT test+review gate.**
4. **The FIRST bead that reaches dot:close + passes review + MERGES = the true
   end-to-end proof AND the start of the scavenger drain.** Report GREEN with the
   bead id + commit SHA.
5. **Keep it to 1-2 while the operator is away** (prove it, don't flood — we scale the
   batch when they're back).

Re-arm `comms recv --agent leto --follow` so the captain can reach you without a pane nudge.

## Not a gate
- fork-bomb blocker hk-9s5fx: PRIMARY FIX DONE (353fc3c1 — dead `.pi/extensions/flywheel`
  deleted+pushed; worktrees no longer inherit it). Optional `pi -ne` hardening remains,
  NOT a gate.

## Do NOT
- Do NOT build hk-z13jz (base_url passthrough for local OpenAI endpoints / DGX-Spark)
  yet — operator sending sandbox details soon.
- Do NOT `br close` any bead — the daemon closes on merge.
- Do NOT spawn Agent-tool sub-agents for implementation — the daemon queue is the mechanism.

## progress feed (mandatory)
Post `--topic status` to captain on: daemon redeployed, canary submitted, canary
GREEN/RED. Plus a ≤15-min idle tick. **Report to captain when the canary is GREEN.**

## translations
hk-nxjwo = "the Pi canary bead (throwaway, proves file-key auth + dot:close)" ·
hk-sv3vg = "daemon ~-expansion fix (the durable auth path) — LANDED 37a4df93" ·
pi-q = "your new dedicated Pi queue (per-queue cap, PI-070)" ·
hk-9s5fx = "flywheel fork-bomb bead (primary fix already done)" ·
hk-z13jz = "base_url passthrough for local OpenAI endpoints — DO NOT build yet".
