---
schema_version: 1
crew_name: leto
queue: sandbox-q
epic_id: hk-f39ny
captain_name: captain
model: opus
---

# Crew mission — leto — PI-SANDBOX BUILD (operator priority, admiral 2026-07-03)

> RE-TASKED 2026-07-03 to the **pi-sandbox** lane (codename:pi-sandbox, queue sandbox-q).
> This is now THE priority — the unlock for running Pi (and later DGX/local models) in an
> OS-level sandbox so a non-Claude harness can't touch the host main repo/branch.
> Pi itself is FULLY HELD until this lands. Model = Opus (this is investigative GATE work).

## On boot
1. `harmonik comms join` + confirm identity = leto.
2. `br update hk-f39ny --assignee leto` (mirror for attribution).
3. Post a boot status to captain (`--topic status`).
4. Arm `harmonik comms recv --agent leto --follow --json`.
5. READ THE BRIEF FIRST: `plans/2026-07-02-pi-sandbox/HANDOFF.md` (self-contained; §5 = the
   Go-CLI-TLS problem, §8.1 = the spike). Companion research: `README.md` in that folder.

## STEP 1 — THE SPIKE (GATE — do this FIRST, report BEFORE proceeding)

**hk-f39ny** (P1, GATE — blocks all other pi-sandbox beads). Manually srt-wrap
(`@anthropic-ai/sandbox-runtime`, `npm i -g @anthropic-ai/sandbox-runtime`) a shell that:
  (a) runs `br ready` + `harmonik comms recv --json` against the LIVE local daemon over the
      unix socket (`.harmonik/daemon.sock`), AND
  (b) makes ONE live OpenRouter model API call.
**Resolve the Go-CLI-TLS question** (br/harmonik/gh are Go binaries that FAIL TLS under srt's
MITM proxy — brief §5). Decide among: (a) enableWeakerNetworkIsolation=true (exfil caveat),
(b) Go CLIs on the local unix socket only + allowlist remote domains, (c) run local-only tools
outside the network sandbox. **Land the working srt settings recipe + the TLS decision, documented.**
v1 network mode = OPEN (locked) — rely on the FS boundary; the spike proves the MECHANISM, not egress lockdown.

**REPORT spike findings to captain BEFORE proceeding to the build.** This is a hard gate.

## STEP 2 — the dependency-ordered build (ONLY after spike findings are reviewed)

Dispatch through `sandbox-q` in this order (each blocks the next):
1. **hk-p7smp** — profile/settings generator (`internal/daemon/sandboxprofile.go`)
2. **hk-rlxgx** — argv-wrap srt in the substrate (`internal/daemon/tmuxsubstrate.go`)
3. **hk-6596l** — sandbox config block + threading (`projectconfig.go`, `workloop.go`, composition root)
4. **hk-i0377** — acceptance: commit-inside-SUCCEEDS / write-to-main-DENIED / branch-merges

## Locked design (from the brief — do NOT relitigate)
- Mechanism = **srt** (`@anthropic-ai/sandbox-runtime`); do NOT hand-roll SBPL/bubblewrap.
- Both platforms, **macOS first**; v1 network = **OPEN** (FS boundary is the isolation).
- Config-driven backend `sandbox: {backend: srt|none}` — NO hard-coded framework/creds; fail loud if unset.
- Warm cache = read-only warm base + per-run private writable area (never a shared concurrent writer —
  avoids the cache-reaper TOCTOU class).

## queue / discipline
- Use `sandbox-q` for every submit. NEVER main. Do NOT `br close` (daemon closes on merge).
- Do NOT spawn Agent-tool sub-agents for implementation — the daemon queue is the mechanism.
- gurney STAYS stood down (reserved for the operator's incoming real work → gb-mbp) — do not touch its lane.

## progress feed
Post `--topic status` to captain on: boot, spike findings (the gate report), each build bead close,
and a ≤15-min idle/working tick.

## translations
hk-f39ny = "the GATE spike (srt reaches daemon + model call + Go-CLI-TLS decision)" ·
srt = "@anthropic-ai/sandbox-runtime, the argv-wrapper sandbox" · sandbox-q = "your queue" ·
hk-p7smp/hk-rlxgx/hk-6596l/hk-i0377 = "the 4 dependency-ordered build beads (profile→argv-wrap→config→acceptance)".
