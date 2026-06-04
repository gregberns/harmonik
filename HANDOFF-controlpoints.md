<!-- PP-TRIAL:v2 2026-06-03 (night) main — controlpoints thread. ONBOARDING-DOCS TIER COMPLETE: landed 5 reviewed-clean docs/onboarding beads this session + self-fixed a path bug. The codename:productization docs lane is DRAINED; everything left is daemon/DOT CODE in named-queues' zone — do NOT blind-dispatch. The shared HANDOFF.md + flywheel/named-queues threads are SEPARATE concurrent work — do NOT clobber. -->

ROLE: orchestrator. Delegate via the persistent daemon's queue (skill `harmonik-dispatch`). Use your OWN `--queue controlpoints` (isolated from `main` churn). Do NOT edit main's working tree while a queue is ACTIVE (escape-detector); enrich beads → flush → commit BEFORE submit. Failed-twice → investigator, never a 3rd blind re-dispatch.

# State: CLEAN. Daemon idle at -c6, healthy. main tree clean after final commit. My `controlpoints` queue drained+cleared.
- **hk-4l7zs (spawn-semaphore slot-leak) is RESOLVED** — fix `9b2848e3` deployed, bead CLOSED `79251a83`, daemon back at `--max-concurrent 6`. The handoff predecessor's "wait for named-queues' redeploy broadcast" is OBE. Residual to watch: `hk-9vp51` (P1, narrower launch-stall remnant, worktree fix in flight).
- `main` queue still `paused-by-failure` holding peer's `hk-mgoo7` — LEAVE IT (named-queues owns; its fix `8df0a852` already landed, bead may be stale-open). ~15 stale `paused-by-failure` cruft queues from peers — not mine to cancel.

## What this session landed (all merged+CLOSED, all reviewed clean)
- **hk-q75ej** (P1) — README rewrite (225 lines): what-is/install/quickstart, pinned install cmds (harmonik=`go install ./cmd/harmonik`; br=cargo-from-git, flagged unverified; kerf=OPTIONAL, not needed for core loop), corrected safety banner (integration-branch P0 gate LANDED). Commit `ba3c2801`.
- **hk-3nabd** (P1) — AGENT_OPERATING_MANUAL.md (223 lines): distills AGENTS.md + the 5 private-memory gotchas (env-strip billing, wide-waves disk+CPU, epic-dep insta-fail, $TMUX, stale-binary). Commit `ba511a27`.
- **hk-y5ke5** (P2) — AGENT_INDEX→README/manual bridge links + flipped docs/orchestration-protocol-v2.md to `Status: SUPERSEDED`.
- **hk-704db** (P3) — work-project-deployment section in AGENTS.md (`.harmonik/branching.yaml` template + `--target-branch`/`--protect-branch` flags + migration + integration→main as human PR) + hk-keeper.sh launch line. Converged over 3 review iterations (gate caught wrong path/field-name).
- **hk-nmni6** (P3) — templates/AGENT_OPERATING_MANUAL.template.md (placeholders + reusable-vs-project-specific fences). 3 review iterations.
- **README path self-fix** `3a71559a` — I had injected the WRONG path `config/branching.yaml`; canonical is **`.harmonik/branching.yaml`** (key `defaults.lands_on` + `defaults.protect_branches`; proven at `internal/branching/branching.go:41` + `cmd/harmonik/init_cmd.go:348`). `harmonik init` + daemon were already correct — docs-only.

## Validated this session (durable facts)
- **Review-loop is LIVE on the `--beads` path** — all 5 beads emitted `reviewer_verdict`+`run_completed`; 2 genuinely iterated to APPROVE. The `436920da` fix is deployed (memory `reference_review_loop_default_outage` updated).
- **BLOCK→enrich→re-dispatch pattern works again** AND the in-loop iteration converges: inject the exact correct content into `br update --design` (the worktree implementer can't see private memories or know the canonical path). I pre-injected install cmds + the 5 gotchas + the branching path before dispatch.

## Next step
1. **Watch the bus** (`harmonik comms recv --agent controlpoints --follow`) — wait, that flag is gone; use `harmonik comms recv --from <peer> --follow` or `harmonik comms log --since 30m`. Watch for named-queues coming online before touching their code zone.
2. **Remaining `codename:productization` = 7 CODE beads, all named-queues' daemon/DOT zone — COORDINATE before dispatch:**
   - `hk-tldws` (P1 bug) queue-submit must stamp resolved `workflow_mode` per item — relates to the review-off root cause; touches the dispatch path named-queues actively edits.
   - `hk-p0kum`/`hk-30vlb`/`hk-n7fw3` (standard-bead.dot: ship the review-embedding process / make it the dispatch default / fold the scenario gate in) — DOT design work, not blind dispatch.
   - `hk-tnmjy` (P2) review_gate_anomaly alarm; `hk-4rkrg` (P2) smoke-bead self-verification; `hk-gax8v` (P3) `harmonik promote` PR helper (needs design).
3. If named-queues is offline and you want to keep moving: these need ENRICHMENT/DESIGN first (read the bead, file a kerf work for the DOT process, or pin the exact code change), then dispatch with the fix embedded in `--design`. Do NOT submit them raw.

## Files to open first
1. Memory `project_productization_initiative.md` (P0 DONE + onboarding-docs tier DONE + the 7 remaining code beads) + `reference_spawn_semaphore_wedge.md` (hk-4l7zs RESOLVED).
2. `br list --label codename:productization --status open` — the 7 remaining.
3. skill `harmonik-dispatch` + `docs/known-workarounds.md`.

## Translations glossary
- **codename:productization** — make harmonik deployable on new/work repos (onboarding docs, README, integration-branch enforcement, review-embedding DOT).
- **onboarding-docs tier** — README + AGENT_OPERATING_MANUAL + templates + bridge links + work-deploy docs. DONE this session.
- **integration-branch P0 gate** — daemon merges to a configured branch (`.harmonik/branching.yaml` `lands_on`) and fail-closes refusing main. LANDED + verified.
- **hk-4l7zs** — spawn-semaphore slot-leak (daemon wedged launches under concurrency). RESOLVED+deployed; daemon back at -c6.
- **standard-bead.dot** — the DOT graph defining a bead's process (implement→commit-gate→review); the productization goal is to embed review as a non-bypassable DOT floor. Beads hk-p0kum/hk-30vlb/hk-n7fw3.
- **controlpoints queue** — my isolated `--queue controlpoints`; clears on success, `paused-by-failure` on BLOCK (clear with `harmonik queue cancel controlpoints`).
