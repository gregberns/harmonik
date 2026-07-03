# Remote substrate — test-hardening program (2026-06-25)

> **TOP PRIORITY** (operator directive, 2026-06-25; operator went to bed → admiral runs the program
> autonomously, captain coordinates execution). Builds on `remote-iteration-impasse-plan.md`. **All work
> here is TESTING / hardening — low blast-radius; it must not break existing fleet work.**

## Decisions locked tonight (operator)

1. **Do ALL of moves ①②③** from the impasse plan. **SKIP ④** (two daemons on the same repo).
2. **Build the full TEST PYRAMID (L0–L5 below).** Sequence the local-container layer **(L3) LAST** — it's the most work.
3. **Daemon repointed LOCAL** (remote worker `gb-mbp` disabled in `workers.yaml`), **concurrency = 4**. *(Done by admiral 2026-06-25 ~05:35Z; daemon restarted to apply.)*
4. **Linux remote support — previously held off — is now IN SCOPE,** sequenced with L3: **L3 uses Docker containers, NOT full VMs (Lima)** (operator 2026-06-25 — "easy and fast"). Docker containers run Linux, so the remote-worker path must support a **Linux OS target** to host containerized "remotes." Docker (OrbStack) is installed; **its daemon must be running** when L3 work starts (currently down — start OrbStack first; not blocking now since L3 is LAST).
5. **Operating rules for this program** (operator, 2026-06-25):
   - **Blocking bugs found mid-stream → identify + fix ASAP.** Small fixes: **just do them directly** (isolated worktree → review → land), **do NOT route them through the slow daemon pipeline.** (Pattern proven tonight: hk-f3u6o landed out-of-daemon in ~20 min.)
   - **Crews MAY use sub-agents** for portions of work — **but every change MUST be reviewed.**
   - **Review gate = multi-agent consensus of DIVERSE agent types, NOT human signoff.** ≥2 independent reviewers; consensus APPROVE → land. Disagreement → admiral adjudicates or spawns a tiebreaker. **Do NOT let "needs signoff" block progress** — if reviewers can't reach consensus, find a workaround and decide.
   - It's all testing → low risk → **keep moving.**

## The reframe (why these tests)

Every remote bug came from **separation** the code assumed away: filesystem (verdict written on the worker, read locally — hk-f3u6o), git-ref (worker `origin/main` stale — hk-t1t00), tmux/process (`#{pane_id}` quoting), transport (SSH argv). A single-machine ssh-localhost test **shares all four → false green.** Good testing = **reproduce separation cheaply, at rising fidelity.** That gives a pyramid.

## The test pyramid

| Layer | What it is | Catches | Cost | Order |
|---|---|---|---|---|
| **L0 — Runner-seam contract** | Fake runner backed by a *different* temp dir than the daemon reads; run the full pipeline → **any** bare `os.ReadFile`/`os.Stat`/`exec` on a run-scoped path FAILS. Plus a static check: "no bare `os.*` on a run path in the remote code path." | The **whole bug class** (any I/O that bypasses the runner) | unit-test fast | **1st** |
| **L1 — ① done right** | Twin scenario-harness, but its "remote" runner backs onto a **separate FS + separate git clone** (NOT shared localhost). Full route→review→merge in ~6s. | hk-f3u6o / hk-t1t00 class, deterministic | ~6s | **1st** |
| **L2 — ssh-to-localhost, isolated** | Real SSH, into a sandboxed `$HOME` + separate tmux socket + separate checkout. | Transport/quoting bugs (`#{pane_id}` class) | seconds | 2nd |
| **L4 — fault/chaos + record-replay** | Inject stale ref, dropped verdict, agent timeout, flaky net; replay a recorded real session. | **Resilience** — what a real server tests *worse* | unit-test fast | 3rd |
| **L5 — scheduler property tests** | N in-process fake runners; assert routing / failover / no-collision / concurrent local+remote invariants. **Move ② (per-queue local/remote routing) lands here.** | the multi-worker `SelectWorker` future | thousands of cases, instant | 4th |
| **L3 — local Docker containers** (NOT VMs) | Real separate FS/git/tmux/OS; spin up **N** Docker "remotes" + a local. **Needs Linux-remote support (decision 4).** | multi-remote + local-at-once, faithfully; no cloud | minutes | **LAST** |

**Move → layer mapping:** ① = L1 (+L0). ② per-queue routing = lands with L5. ③ bead-runs survive daemon restart = independent durable fix (extend the crew-survives-restart pattern to runs), schedule mid-program. ④ = skipped.

## Division of labor

- **CAPTAIN — coordinates execution.** Turn this into a **kerf work + bead set**, prioritize via `kerf next`, dispatch via the daemon queue (now LOCAL), staff/run crews, apply the operating rules above. First research task: **map the real code seams** — what the twin harness stubs today, where the `Runner` interface boundary is, which run-path reads/execs still hit bare `os.*` (the L0/L1 bead specifics depend on this). Re-enabling `gb-mbp` is a later phase (L2/real-remote smoke) — not now.
- **ADMIRAL — oversight.** Produced this plan; adjudicates review consensus; runs the **hourly progress-watchdog** (unsticks the captain/crews if stalled); surfaces only genuine operator decisions. Holds NO beads, dispatches nothing.

## Deferred / for the operator later (non-blocking)

- Re-enabling `gb-mbp` for the real-remote smoke (after the fix is deployed to the daemon binary + remote proven reliable).
- L3: run containers in CI vs locally only.
- Parked behind this program: codex-on-remote, Pi model-gateway, de-hardcode-messages.
