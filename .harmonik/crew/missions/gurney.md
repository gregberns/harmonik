---
schema_version: 1
crew_name: gurney
queue: gurney-q
epic_id: hk-z13jz
captain_name: captain
model: opus
---

# Crew mission — gurney — PI base_url PASSTHROUGH (overnight P1, admiral 2026-07-03)

> RE-TASKED 2026-07-03 for the **overnight P1 push** (operator asleep, 8h target).
> Your ONE bead is **hk-z13jz** — it is the last blocker before Pi can run coding beads
> against the DGX local model. leto is finishing the sandbox in parallel (sandbox-q); the
> moment BOTH land, captain wires Pi→Ornith + turns the sandbox on + runs numerous test beads.
> This is the critical path. Move fast; build via CLAUDE (this crew), model = Opus.
> Your prior remote-reliability lane is PARKED for the overnight — do not touch gb-mbp / workers.yaml.

## On boot
1. `harmonik comms join` + confirm identity = gurney.
2. `br update hk-z13jz --assignee gurney` (attribution).
3. Post a boot status to captain (`--topic status`).
4. Arm `harmonik comms recv --agent gurney --follow --json`.

## THE BEAD — hk-z13jz (P1)
**"Pi harness must pass base_url/api_base through to the Pi child for locally-hosted
OpenAI-format endpoints (DGX Spark local models)."**

**Target endpoint (what this unblocks):** DGX at `http://dgx.local:8551/v1`, model `ornith`,
256K ctx, vLLM / OpenAI-compatible, dummy api_key accepted.

**The seam (from captain's read of the code):**
- `cmd/harmonik/resolve_pi_config.go` — the Pi config resolver. Today it requires
  `provider`, `model`, `api_key_env` and imposes ZERO baked defaults (R1 de-hardcode mandate —
  KEEP that invariant). Add an **optional** `base_url` (a.k.a. api_base) field here, shape-validated
  (a URL/host shape, ≤ sane length; do NOT value-validate reachability). Optional = absent is valid
  (today's cloud-provider behavior unchanged); present = passed through.
- `internal/daemon/pilaunchspec.go` — builds the Pi child argv
  (`pi --mode json --provider <prov> --model <prov/id> "<seed>"`). Thread the resolved `base_url`
  to the Pi child **only when set**.
- **DETERMINE how the Pi child accepts a custom base_url** — check `pi --help` and the installed
  `@earendil-works/pi-coding-agent` package (`/opt/homebrew/bin/pi` → its node script /
  node_modules). It's likely either a `--base-url` / `--api-base` CLI flag OR an env var
  (`OPENAI_BASE_URL` / `OPENAI_API_BASE`). Use whatever Pi actually honors; if it's an env var,
  inject it into the Pi child env in pilaunchspec.go (mirror how api_key_env is injected). Prove
  the mechanism, don't guess.
- Config-driven, **fail-loud** if referenced-but-malformed; absent base_url = no-op. No hard-coded URL.
- **Tests** covering: absent base_url (today's behavior, no flag/env emitted), present base_url
  (flag/env correctly emitted to the Pi child). Reviewer gate required (DOT).

## queue / discipline
- Submit hk-z13jz to **gurney-q** (`harmonik queue submit --queue gurney-q ...`). NEVER main.
- Do NOT `br close` — the daemon closes on merge.
- Do NOT spawn Agent-tool sub-agents for the implementation — the daemon queue is the mechanism.
- Do NOT touch leto's sandbox lane, gb-mbp, or workers.yaml.
- **CONCURRENT-EDIT WARNING (load-bearing, cost us 47min tonight):** if another bead merges to main
  while your run is in flight and touches the SAME file (esp. config structs), your merge will
  rebase-conflict and the run fails. resolve_pi_config.go + pilaunchspec.go are Pi-specific and
  unlikely to collide with leto's sandbox work, but if a merge-fail happens, re-submit fresh (it
  re-branches off new main) and tell captain.

## progress feed
Post `--topic status` to captain on: boot, the base_url mechanism you found (flag vs env),
bead close/merge, and a ≤15-min idle/working tick. This is the overnight critical path —
if you wedge or hit a real blocker, escalate to captain IMMEDIATELY (don't sit).

## translations
hk-z13jz = "your bead: pass a custom base_url through to the Pi child so it can hit the DGX
local model instead of a cloud provider" · Ornith = "the DGX-hosted local model (ornith @
http://dgx.local:8551/v1, OpenAI-compatible)" · gurney-q = "your queue" · Pi = the
@earendil-works/pi-coding-agent harness · the sandbox = leto's parallel build (srt FS isolation).
