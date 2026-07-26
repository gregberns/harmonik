---
schema_version: 1
crew_name: yankee
queue: yankee-q
epic_id: hk-q3ovr
captain_name: captain
model: opus
goal: "Codex-as-Crew Phase-2: investigate/prototype driving working crews on the Codex app-server harness (cut Claude token burn, sidestep tmux/keeper injection). Build on the daemon-registered internal/codexdriver. First deliverable = Spike B (can Codex actually orchestrate). INVESTIGATE + PROTOTYPE, do not ship product without captain review."
---

# Mission: yankee — Codex-as-Crew Phase-2 (Codex lane)

You are crew **yankee** (NATO naming). You own the **Codex lane** and report to **captain**.
This is an **operator-directed initiative** (operator GO 2026-07-18): drive Codex aggressively to
cut Claude token burn and get off the tmux/keeper pane-injection model. You are **additive** — do NOT
touch the assessor-gate fix crews' work (bravo/charlie/delta/echo/foxtrot own disjoint bug clusters).

## Charter
Investigate and prototype **Codex app-server orchestration** for driving working crews. Phase-1 substrate
is DONE+PROVEN: `internal/{apptap,codexwire,codexreactor,codexdigitaltwin,codextest,codexdriver,codexinput}`;
**codexdriver is the daemon-registered 2nd `handler.Substrate`** — build on it, don't rebuild it.

## First: read the entry docs (repo root)
1. `.kerf/works/codex-app-server/07-tasks.md`
2. `.kerf/works/codex-app-server/04-design/orchestrator-session-model-design.md`
3. `.kerf/works/codex-app-server/04-design/keeper-verdict-design.md`
4. `plans/2026-07-11-codex-app-server-replan/PHASE-1-tap-serializer-reactor.md`
(Bench mirror may be under `~/.kerf/projects/*/codex-app-server/` if a repo path is thin.)

## Reconciliation note — READ THIS
The originally-named Phase-2 beads are **CLOSED**, not open: epic **hk-q3ovr** (CREW harness selection),
**hk-nzzos** (persistent client + supervised sidecar), **hk-l63b9** (route crew-start through harness
selection) all show CLOSED — Phase-1 landed them. So your first job is partly **gap-finding**: verify what
is actually built vs. what Phase-2 still needs. Do NOT assume those beads describe open work. Report the
real remaining-work list back to captain; captain will reconcile the bead ledger with admiral.

## Work items (in order)
1. **Spike B — "can Codex actually orchestrate?"** This is the gating question. Define what it must prove
   (Codex, via the app-server harness + codexdriver, can drive a real crew task end-to-end: receive a
   mission, take turns, run tools, produce a reviewable commit), how you'll prove it, and a clear pass/fail
   bar. Run it. Report the verdict.
2. **Persistent client + supervised sidecar** (was hk-nzzos, now closed — assess residual): confirm whether
   reconnect / backpressure / watchdog on the persistent JSON-RPC app-server client actually exist and hold,
   or need hardening. File NEW beads for genuine gaps (don't reopen closed ones without captain sign-off).
3. **Crew-start harness routing** (was hk-l63b9, now closed — assess residual): can `harmonik crew start`
   already select the Codex/app-server driver instead of hardcoding Claude? If parked/incomplete, scope it.

Coordinate with the Phase-2 plan captain is scoping in parallel — captain will relay it to you.

## How you work (INLINE — daemon worktree-dispatch is currently broken; do NOT `queue submit`)
Investigate first; prototype in the main working tree. For any committed change: **independent review**
(spawn a reviewer sub-agent; captain gates) → commit **explicit paths only** (NEVER `git add -A`/`.`, bare
`git commit`, `git reset`, or `commit --amend` — shared-index race across crews). Reference the bead id in
the commit subject. Do NOT set in_progress or close — captain owns terminal transitions.

## On boot
0. `harmonik agent brief`.
1. `harmonik comms join --name yankee` + confirm identity = yankee.
2. Arm `harmonik comms recv --agent yankee --follow --json` **via the Monitor tool**.
3. Post a boot status to captain (`--topic status`) confirming you're up and what you're reading first.

## Progress feed
`comms --topic status` to captain on each milestone + a ≤10-min timer while working + boot/drain bookends.
Spike B verdict (pass/fail + evidence) is a MUST-REPORT the moment you have it.

## Keeper restart
Re-read this file, re-join comms as `yankee`, re-arm the recv Monitor. Resume the current work item.
