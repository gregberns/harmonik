<!-- Mission handoff — locked 6-field schema -->
```yaml
schema_version: 1
crew_name: alia
queue: alia-q
epic_id: hk-hcrvb
goal: "Build the core-loop-proof TEST HARNESS on integration/core-loop-proof: live-verify the real task-processing loop (bead->queue->harness w/ correct model->real changes->provider-comms through the sandbox->DOT review-back) across {claude,codex,pi}x{local,remote}. Acceptance = the top-5 coverage gaps + the branch-targeting assertion (T10). This is Phase-1 of the operator's single-focus quality-system."
captain_name: captain
model: opus
```

## Operating model — "C" (confirmed by admiral+operator). READ THIS FIRST — you are NOT a standard daemon-dispatch crew.
You BUILD HARNESS CODE on an integration branch in your OWN worktree; you do NOT let the daemon merge your harness to main.
1. **Work on `integration/core-loop-proof`, never main.** Check it out in your OWN worktree (e.g. `git -C <your-worktree> checkout integration/core-loop-proof`). Commit harness code (scripts + test code) directly to that branch. NEVER push harness code to main.
2. **The daemon is the System Under Test.** Use it ONLY to *execute* the matrix loop (real bead->queue->harness runs the harness drives as test executions). NEVER dispatch your harness-BUILD beads to the daemon for auto-merge — its default merge target is main (daemon-wide; per-crew integration targeting is dead code, tracked as hk-lgykq). Merging the harness via the daemon-under-test is the self-fix bootstrap trap — don't.
3. **integration→main is ONE human PR at the epic boundary, gated by the `assessor`** (independent CR + LT + XT). Your harness code STILL needs that independent review before the PR — do not self-merge, do not skip it.
4. **Prefer the codex / non-Claude path** for any sub-agent build work under the token crunch (Claude weekly cap ~98%). The **pi** harness path is BLOCKED (reasoning-model, hk-4ir08) — do not route to pi.

## Build sequence (epic hk-hcrvb; beads labeled codename:quality-system)
Serial foundation, then parallel gap tasks, then the gate:
- **T1 hk-h6fej (P0, READY):** matrix-runner skeleton on `scripts/scratch-daemon.sh batch`. Reuse the remote-test-pyramid runner seam (10/10 done) + existing event types — no new Docker (that's a later phase), no new isolation machinery.
- **T2 hk-1yxhh (P0):** assertion-library module + seed-bead fixtures + expected-cell spec. **This is the load-bearing contract** the Phase-2 scripted-twin must satisfy — the admiral hands the twin design the moment T2 lands green on this branch, so make the assertion package clean + stable.
- **Gap tasks (parallel after T2):** T4 hk-qa1oo (model reaches harness per family + node-pin no-leak), T5 hk-bkn5a (queue-submit->dispatch field fidelity), T6 hk-i21pt (provider-comms through the sandbox), T7 hk-wf9lv (remote tcp:// == local — SKIP-LOUD when no tcp:// worker reachable, never false-green), T8 hk-4vwlx (real Claude-worktree->agent_ready, flag-gated to save cap).
- **T10 hk-xke2i (NEW acceptance item):** assert a bead directed to integration branch X actually LANDS on X, not main. This **REDs today** because per-bead integration targeting is dead code — that RED IS the known-issue evidence for **hk-lgykq**. Wire it into the matrix as a known-RED cell (recorded, not a false-green pass), linked to hk-lgykq.
- **T3 hk-9cw6q (anytime after T1):** red-cell -> deduped bead wiring (`scratch-daemon.sh feedback`).
- **T9 hk-jjt6w (gate):** full-matrix green (minus the known-RED T10 cell) + clean-reset reproducibility.

## HANDOFF TRIGGER — tell the captain the moment T1 lands
When **T1 (the matrix-runner skeleton) lands on integration/core-loop-proof**, post to captain (`comms send --to captain --topic status`): "T1 skeleton landed." The captain staffs the **hk-lgykq daemon-core fix crew** at that point (the fix dogfoods against your harness). Also flag when **T2 lands green** (unblocks the admiral's Phase-2 twin handoff).

## Discipline
- Boot: read this file; confirm identity (`$HARMONIK_AGENT`==alia); `harmonik comms join --name alia`; `br update hk-hcrvb --assignee alia` (mirror, load-bearing); arm `harmonik comms recv --follow --json`; post a boot status to captain.
- Daemon owns terminal bead transitions — do NOT pre-set in_progress or close on merge for any bead you DO route through the daemon. Your harness-code commits on the branch are yours; the beads track the checklist.
- Post progress to BOTH `comms --to captain --topic status` AND `br` comments: on each bead close + a <=10-min timer while building (<=15-min idle/draining) + boot/drain bookends.
- Triage your own failures (Opus lane): reproduce+diagnose before re-dispatch; escalate to captain only on the major-issue trigger (root cause refuted >=2x or a wedge survives >=2 fix attempts).
- CWD discipline: operate from your worktree via absolute paths / `git -C`; never `cd` around; never touch main.
