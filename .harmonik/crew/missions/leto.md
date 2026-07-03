---
schema_version: 1
crew_name: leto
queue: pi-q
epic_id: hk-nxjwo
captain_name: captain
model: sonnet
---

# Crew mission — leto — PI LIVE-TESTING (operator-greenlit via admiral 2026-07-03)

> RE-TASKED 2026-07-03 from the Pi Phase-0 BUILD (hk-94c3t, DONE — all 9 beads landed;
> the ~-expansion auth fix hk-sv3vg also landed, commit 37a4df93 on main) to PI
> LIVE-TESTING. Prove the Pi harness runs live on a GPT model end-to-end, then route
> mechanical scavenger beads through it. Your LIVE mission is also the captain comms
> thread (topic=assign) — trust that if this file and comms ever disagree.

## On boot
1. `harmonik comms join` + confirm identity = leto.
2. `br update hk-nxjwo --assignee leto` (mirror for attribution — load-bearing).
3. Post a boot status to captain (`--topic status`) + a journal comment.
4. Arm `harmonik comms recv --agent leto --follow --json` (dedupe on event_id).

## goal — SEQUENCED (do in order)

1. **hk-sv3vg has LANDED** (37a4df93 on main — daemon expands ~ in api_key_file at
   config parse). That fix IS the durable auth path. leto-q is drained.

2. **REDEPLOY the daemon** to 37a4df93 (runbook `docs/daemon-redeploy.md`; fleet idle =
   ideal window). You own daemon ops + supervisor-env coordination + are authorized to
   restart.
   - **AUTH — prefer the FILE-KEY path** (daemon reads api_key_file
     `~/.config/harmonik/openrouter.key`, ~ now expands). SUPERIOR to env-var because it
     **survives supervisor revives**. Only if the PI-040 guard still checks env
     EXCLUSIVELY, fall back to exporting `OPENROUTER_API_KEY` in BOTH the daemon launch
     AND the supervisor's revive command (else the next auto-revive drops it).
   - DO NOT read the key contents.

3. **Set model:** `.harmonik/config.yaml` `harnesses.pi.model` -> `openai/gpt-5.4-mini`
   (admiral correction — gpt-4o is STALE; gpt-5.4-mini is the current OpenRouter OpenAI
   mini, released 2026-03, tuned for high-throughput coding+tool-use). Use gpt-5.4-mini
   for the canary AND the scavenger load. Provider stays openrouter.

4. **Canary:** submit `hk-nxjwo` to a DEDICATED `pi-q` (PI-070 needs an explicit
   per-queue cap). Verify it reaches `dot:close` authenticating via GPT.

5. **ONCE GREEN:** route a batch of mechanical scavenger beads (grep=0 /
   failing-test->green, deterministically checkable — the ~110 banked scavenger beads are
   the FUEL) through `pi-q` on gpt-5.4-mini, behind the DOT test+review gate. Keep them
   file-disjoint from any other active lane.

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
