<!-- PP-TRIAL:v2 2026-06-03 (evening) main — controlpoints thread. Productization-build session. Landed 4 reviewed beads + verified the integration-branch P0 gate end-to-end. ACTIVE INCIDENT: hk-4l7zs spawn-semaphore slot-leak (daemon at -c4, intermittent launch wedges) — named-queues OWNS the fix (authoring in a worktree, offline mid-fix), will broadcast at redeploy. The shared HANDOFF.md + flywheel/named-queues threads are SEPARATE concurrent work — do NOT clobber. -->

ROLE: orchestrator. Delegate via the persistent daemon's queue (skill `harmonik-dispatch`). Use your OWN `--queue controlpoints` (isolated from `main` churn). Do NOT edit main's working tree while a queue is ACTIVE (escape-detector). Failed-twice → investigator, never a 3rd blind re-dispatch.

# State: CLEAN. Daemon idle (no active queues). main tree clean after this commit.
- `main` queue is `paused-by-failure` holding the peer's `hk-mgoo7` (wedged on hk-4l7zs) — LEAVE IT; named-queues re-dispatches post-fix.
- ~9 stale `paused-by-failure` cruft queues from peers (agc*, fw*, nqfix, nqops, gatefix, followups) clutter `queue list` — not mine to cancel.

## What this session landed (all merged+closed, all reviewed clean)
- **hk-rqx5o** (P1) — `docs/templates/AGENTS.template.md` + `docs/setup-agent-prompt.md` (onboarding, parameterized by $PROJECT_DIR/$TARGET_BRANCH). Commit 12c9accb.
- **hk-y171w** (P2) — `harmonik init` bootstrap subcommand (11 init steps: doctor, .harmonik dirs, br init, config/branching.yaml, template render, supervise). Commit 9b3ab859.
- **hk-zl4sl** (P1) — `branching.yaml` `protect_branches` + daemon lands_on/deny-list fallback. Commit 0f48f44a. **BLOCKED on attempt 1** (review gate caught a real safety-bypass: wiring block placed after resolveTargetBranch+the hk-sul12 guard → lands_on DOA + YAML protect_branches bypassed the deny-list). Re-dispatched with the reviewer's precise fix embedded in `br update --design` → clean APPROVE. Pattern worth reusing.
- **hk-gwkgr** (P2) — checked `scripts/hk-keeper.sh` into the repo (parameterized project-path + concurrency). Commit ef291043.

## Verified, not built: the P0 integration-branch gate
Read-only end-to-end audit (commits hk-6r6xv/mkxw1/sul12/eun55): fail-closed enforced at **3 layers** — boot validation (`daemon.go:633-641`), dispatch `lands_on` guard (`workloop.go:1691`), in-merge guard (`workloop.go:3370-3385`); merge ops parameterized; unit + 4 scenario tests PASS incl. `TestBranchGuard_TargetBranchMergeIsolation` (main reflog pinned). **Code-level deploy-to-a-work-repo gate is CLEARED.** Remaining gate is ONBOARDING (see below), not the merge path.

## Two incidents handled
1. **Disk 100% / 1.2GiB free** (wide-waves no-space class) → `go clean -cache` reclaimed 6.8GiB. Caused hk-rqx5o's first failure (transient socket drop).
2. **hk-4l7zs spawn-semaphore SLOT-LEAK** (`tmuxsubstrate.go:216`) — multiple beads wedge at `launch_initiated` (no implementer spawn) → `no_commit` fail at 30min, intermittent under contention. named-queues OWNS it; daemon now at `--max-concurrent 4`; they are authoring the code fix in a worktree off the daemon and will broadcast at redeploy. Signature + workaround recorded in memory `reference_spawn_semaphore_wedge`.

## Next step
1. **Watch the bus** (`harmonik comms recv --agent controlpoints --follow`) for named-queues' hk-4l7zs **fix-redeploy** broadcast. After redeploy, concurrency may go back up; `hk-mgoo7` gets re-dispatched by them.
2. **Remaining `codename:productization` beads I did NOT take** (`br list --label codename:productization --status open`), with WHY:
   - **Content-gated (need context a worktree agent lacks):** `hk-q75ej` (README rewrite — blocked on pinned br/kerf install commands), `hk-3nabd` (AGENT_OPERATING_MANUAL — distills 5 PRIVATE memories not in the repo). To dispatch these well, first pin install cmds / inject the memory content into the bead `--design`.
   - **named-queues conflict zone (daemon/DOT code):** `hk-tldws` (queue-submit workflow_mode stamp bug), `hk-p0kum`/`hk-30vlb`/`hk-n7fw3` (standard-bead.dot process), `hk-tnmjy` (review_gate_anomaly alarm), `hk-4rkrg` (smoke-bead verification). Leave until named-queues' hk-4l7zs work merges to avoid merge races.
   - **Then-unblocked docs:** `hk-y5ke5` (AGENT_INDEX bridge links) needs README + operating-manual to exist first; `hk-704db`/`hk-nmni6`/`hk-gax8v` are P3.
3. Open risks (memory `project_productization_initiative`): pin br/kerf install cmds (blocks runnable README); API-key credit-burn warning in onboarding; keep review-loop as the DOT floor-fallback.

## Files to open first
1. Memory `project_productization_initiative.md` (plan, P0-DONE+VERIFIED, risks) + `reference_spawn_semaphore_wedge.md` (the incident).
2. `br list --label codename:productization --status open` — the remaining backlog.
3. skill `harmonik-dispatch` + `docs/known-workarounds.md`.

## Translations glossary
- **codename:productization** — make harmonik deployable on new/work repos (onboarding docs, README, integration-branch enforcement, review-embedding DOT).
- **hk-4l7zs** — spawn-semaphore slot-leak: daemon wedges new launches under concurrency contention; interim workaround = run at `-c4`. named-queues owns the fix.
- **integration-branch P0 gate** — daemon merges to a configured branch and fail-closed refuses main; VERIFIED safe this session.
- **BLOCK→enrich→re-dispatch** — when the review gate BLOCKs a bead, put the reviewer's precise fix into `br update <id> --design` and re-submit; got hk-zl4sl to a clean APPROVE on attempt 2.
- **controlpoints queue** — my isolated `--queue controlpoints`; completes+clears on success, goes `paused-by-failure` on a BLOCK (clear with `harmonik queue cancel controlpoints`, which archives it).
