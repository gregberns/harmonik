<!-- Mission handoff — locked 6-field schema -->
```yaml
schema_version: 1
crew_name: chani
queue: chani-q
epic_id: hk-fdbhf
goal: "Pi cheap-harness — make the pi agent harness run GREEN in-daemon on a proven, token-cheap provider (OpenRouter/MiniMax), off the ornith/DGX reasoning-model path that emits no tool_calls. This is the single highest-leverage unblock in the fleet: pi is the only token-cheap cell of the T9 full-matrix (hk-jjt6w), which gates PR #20 out of draft, which gates the assessor merge-gate + deploy-gate — the whole quality-system. MiniMax is already proven green OUT-of-daemon; close the in-daemon gap."
captain_name: captain
```

# Mission: chani — pi cheap-harness (green in-daemon)

> ## ⚠️ OPERATOR DIRECTIVE OVERRIDE — 2026-07-08 19:21 (via admiral, relayed by captain)
> **This SUPERSEDES the MiniMax/OpenRouter provider-switch framing below where they conflict.**
> - The operator clarified there is **NO provider key** and **DGX/ornith IS the target** — it is a **LOCAL model server**. Do NOT switch pi off DGX onto MiniMax. Point pi at the DGX/ornith provider.
> - **The only thing the `.9` canary needs is the endpoint: `dgx.local`** (or the box's IP). No key.
> - **Task now = run the `.9` live canary:** a real pi run through the DGX/ornith provider at `dgx.local` producing **working `tool_calls` end-to-end**. That closes **hk-fdbhf** (the operator's core pi goal).
> - **Out-of-daemon FIRST** to get the live proof (per the known srt-sandbox finding — the daemon can't reach `dgx.local` under srt: `reference_pi_noop_is_harness_not_model`). Then wire the in-daemon path.
> - **Report to captain the canary result: DGX request count + tool_calls observed.** Captain relays to operator + admiral.
> - **⚠️ VERIFY-THIS TENSION:** the mission's bug hk-4ir08 says the DGX model is a *reasoning* model that returns `content:null` + no `tool_calls`. The operator now expects tool_calls FROM dgx.local — so the DGX box must be serving a **tool_calls-capable** model (not the old reasoning model). Confirm which model `dgx.local` is serving BEFORE concluding; if it still emits no tool_calls, that is the finding to report — do NOT silently fall back to MiniMax without telling captain.
> - This is **parallel to the A5 release lane — it is NOT gated by A5.**


You are crew **chani**, owning epic **hk-fdbhf** on queue **chani-q**. Report status to **captain**.

Admiral's diagnosis (2026-07-08): quality-system is ~82% built but delivers nothing because the full green matrix can never RUN cheaply — only Claude greens, and that burns the weekly cap. Pi is the only token-cheap path to a repeatable green matrix. Make pi green **in-daemon**.

## On boot
0. `harmonik agent brief` — pull current operating context.
1. `harmonik comms join` + confirm identity = chani.
2. `br update hk-fdbhf --assignee chani` (re-affirm the mirror on adopt — load-bearing for attribution).
3. Post a boot status to captain (`--topic status`) + a journal comment on hk-fdbhf.
4. Arm your event stream via a **durable poll loop** (`comms recv --agent chani --json` + `queue status chani-q` every ~45s) — NOTE: daemon subscribe slots are currently saturated (`subscribe_capacity_exceeded`, bug hk-qsz0p), so do NOT rely on `subscribe --follow`; poll instead.

## The two bugs you own
- **hk-4ir08** — the ornith DGX model is a *reasoning* model: it returns `content:null` and no `tool_calls`, so the pi agent harness can't drive it. The fix is NOT to wait on a DGX model-serving decision — it is to **switch pi's provider** to the proven OpenRouter/MiniMax path (operator-directed `pi-provider-switch` lane, 2026-07-05). MiniMax is proven green out-of-daemon.
- **hk-pkugu** — in-daemon, the claude tier-3 default model **shadows** the configured pi model, so pi runs silently execute as claude-sonnet. Find where the daemon injects the default model and make the configured pi model/provider win for pi runs.

## Known landmines (read before you start — these cost days already)
- **The in-daemon pi no-op was the `srt` SANDBOX, not the model.** Out-of-daemon pi succeeded end-to-end while the daemon pi run never reached the model (0 provider requests). `srt` wrapping local pi blocks egress/fs. Check whether pi is still in `sandbox.harnesses`; if so, dropping it there is part of the fix. (See memory `reference_pi_noop_is_harness_not_model`.)
- **`run_started` is NOT green.** A default-harness=pi + srt combination once produced 34h of silent fleet no-op that looked green. The real green signals are `agent_ready` AND actual provider requests landing. Verify BOTH.
- **Single-mode canary is the discriminator.** Run one pi cell in isolation and watch for provider requests — 0 requests = the run never reached the model (sandbox/PATH/model-pin problem), not a model-quality problem.
- Prior stacked bugs on this path: srt-wrap (fixed), DOT model-pin, empty-PATH. Don't assume one fix is the whole story — re-canary after each.

## Where to look
- The pi harness wiring: search for the pi harness definition, provider/wire-format config (`openai-completions` wire format via ornith today), and `sandbox.harnesses`.
- Proven-green reference: the out-of-daemon MiniMax path already works — mirror its provider/model/wire config into the in-daemon pi harness.
- Memories worth pulling: `project_pi_provider_switch_kerf`, `reference_pi_noop_is_harness_not_model`, `reference_pi_indaemon_three_stacked_bugs`, `reference_pi_ornith_api_wire_format`, `reference_pi_default_harness_srt_34h_fleet_noop`.

## Definition of done
A pi cell runs **in-daemon** on MiniMax (or the chosen proven provider), reaches the model (provider requests confirmed), completes a real task, and greens — repeatably, token-cheap. Prove it with a single-mode pi canary showing `agent_ready` + provider requests + terminal green, then report to captain. That green pi cell is what T9 (hk-jjt6w) has been waiting on.

## Operating loop
Follow the crew-launch skill dispatch loop. Decompose hk-fdbhf into child beads (`br create ... --parent hk-fdbhf`, label `codename:pi-provider-switch`) as needed. Standard `queue submit` to **chani-q** (NEVER `main`). The daemon owns terminal transitions — do NOT pre-set in_progress or close on merge.

## Discipline
- **Triage your own failures.** On a run_failed, reproduce + diagnose before re-dispatch. Escalate to captain only if a root cause is refuted ≥2× or a wedge survives ≥2 fix attempts (major-issue fan-out trigger) — this path has a history of false root causes, so verify with the single-mode canary before declaring a cause.
- Do NOT touch the fleet daemon's live config in a way that changes the default harness for other lanes — scope your changes to the pi harness path. If a change would flip the fleet-wide default harness, STOP and escalate to captain.
- Post progress to **both** `comms send --to captain --topic status` AND `br` comments: on every bead close + a ≤10-min timer while dispatching (≤15-min when idle/draining) + boot and drain bookends.

## Keeper restart
Re-read this file, re-join comms as chani, re-arm the poll loop. Claim the next ready bead and continue.
