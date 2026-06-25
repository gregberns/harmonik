# Remote program — done vs open + questions for next session

_Reconstructed 2026-06-25 from `br show`, `git log`, and files on `main` (HEAD `4c391307`). Evidence-based; comms claims were spot-checked, not trusted._

## One-screen summary (done vs open)

**DONE + VERIFIED ON MAIN:**
- The **test pyramid (L0–L5)** — all 6 layers landed, tests on main, epic `hk-6l941` CLOSED.
- **Daemon-restart-survival** for bead-runs (M3): `hk-o85ye` impl + `hk-78tji` adoption-path tests, both on main.
- **Remote reviewer-verdict fix** `hk-f3u6o` — landed `5999a39a` (confirmed an ancestor of HEAD), CLOSED. **But not yet deployed to the running daemon binary.**
- **Linux OS target support** for the worker telemetry/collector path — landed with L3 (`hk-yflqo`, commit `4bdf7e93`).

**OPEN / OWED:**
- **The live e2e on the real `gb-mbp` remote has NOT been run** in this program. Everything above is unit/scenario-level proof + out-of-daemon fixes. The headline owed step is `hk-nepva`.
- **`hk-t1t00`** (remote stale-base gate fix) — IN_PROGRESS on gurney-q, in review. It blocks `hk-nepva`.
- **Multi-remote (N-worker) support does NOT exist** — the registry is single-worker by construction. L3/L5 fake-run *multiple* containers/runners in tests, but the production daemon can only drive ONE configured worker.
- **`gb-mbp` is DISABLED** in `workers.yaml` (repointed local for the test program) and worker enable/disable is **restart-only** (`hk-xjbvi`, open).

---

## Q1 — The test pyramid (L0–L5): what each layer proves + status

Epic `hk-6l941` (`codename:remote-test-pyramid`), CLOSED 2026-06-25. All 6 child beads CLOSED; all test files confirmed present on `main`.

| Layer | Bead | What it proves | Status |
|---|---|---|---|
| **L0 — Runner-seam contract** | `hk-hd2w6` | Every run-scoped read/exec goes through `daemon.Config.Runner`; a static audit fails any bare `os.*` on a run path in the remote code. Catches the whole bug class. | ✅ on main (`02d62a14`, `c940a8a5`) |
| **L1 — Twin scenario harness** | `hk-52xnr` | The harness's "remote" runner backs a **separate FS + separate git clone** (not shared localhost), so the verdict-read class (`hk-f3u6o`) + gate-verdict class fail loud before the fix and pass after — deterministically, ~6s, no LLM. | ✅ on main (`60aaf419`) |
| **L2 — ssh-to-localhost, isolated** | `hk-8u2al` | Real SSH into a sandboxed `$HOME` + separate tmux socket + separate checkout — catches transport/quoting bugs (the `#{pane_id}` class). | ✅ on main (`8ff0b8de`, `e2415d43`) |
| **L3 — Docker/Lima containers + Linux** | `hk-yflqo` | N Docker containers running **Linux** give genuine FS/git/tmux/**OS** separation; multi-container disjoint-FS gate decisions. Skips when Docker unavailable. **Also added the Linux collector/telemetry path.** | ✅ on main (`4bdf7e93`) |
| **L4 — fault/chaos + record-replay** | `hk-3q92c` | Injects stale ref, dropped verdict, agent timeout, flaky net; replays a recorded session — resilience the real server tests *worse*. | ✅ on main (`17469d95`) |
| **L5 — per-queue routing + scheduler property tests** | `hk-f10xl` | N in-process fake runners assert routing / failover / no-collision / concurrent local+remote invariants. Move ② (per-queue local/remote routing) lands here. | ✅ on main (`6a51ab01`) |

Carried-forward backlog (de-scoped, NOT lost): `hk-n5md3` (P3 — direct test of the LocalOnly/WorkerTarget gate in `beadRunOne`), still OPEN.

---

## Q2 — The "isolated / disposable daemon" testing idea

**This was DESIGNED and EXPLICITLY SKIPPED. It was never built.** It is NOT one of the pyramid layers.

- It is **move ④ "two daemons on the SAME repo"** in `.harmonik/crew/designs/remote-iteration-impasse-plan.md` (lines 38–40).
- It was **rejected as unsafe + blocked**: the daemon pidfile flock makes it a singleton (second instance exits 5), and two daemons on one repo cause mutual tmux/worktree destruction + beads WAL contention.
- The `remote-test-strategy-plan.md` "Decisions locked tonight" #1 records the operator decision plainly: **"Do ALL of moves ①②③. SKIP ④ (two daemons on the same repo)."**
- The design says the *goal* the operator remembers (test a daemon in isolation so you don't perturb the production queue) is **already met a different way** — by the **separate-clone in move ① / L1** (the twin harness backs onto a separate FS + separate git clone) plus **fleet-portability** (landed 2026-06-13), which sanctions one-daemon-per-project. So you get an isolated test daemon via a separate *project/clone*, not a second daemon on the live repo.

**Bottom line for the operator:** the idea you recall exists as a deliberately-skipped design option. We do NOT spin up a disposable daemon in a worktree to test against the live queue. The equivalent safety is achieved by (a) the L1 twin harness running the full route→review→merge pipeline in ~6s against a separated FS/clone with zero impact on the production daemon, and (b) the operating rule that mid-stream blocking bugs get fixed **out-of-daemon** (isolated worktree → review → ff-land), proven by `hk-f3u6o` landing in ~20 min without touching the production pipeline. If you want a real second live daemon-in-a-worktree, it would be net-new work and would need the pidfile-singleton + worktree-collision problems solved first.

---

## Q3 — Docker / Linux / multi-remote progress

**Linux OS support — PARTIAL, the worker-side telemetry path is DONE.** Commit `4bdf7e93` (L3, `hk-yflqo`) added:
- `internal/workers/telemetry.go`: `linuxCollectorScript` (uses `/proc/loadavg`, `/proc/meminfo`, `df -m`, `ps+grep`) + OS dispatch in `CollectReport` (`Worker.OS=="linux"` → Linux script).
- `internal/daemon/scenario_container_l3_hkyflqo_test.go` (on main): three scenarios — FS-axis (`DockerExecRunner` vs host `os.Stat`), Linux collector against a real `alpine:latest` container, and multi-container disjoint-FS gate decisions. All skip when Docker is unavailable.
- `workers.yaml` already carries an `os:` field (gb-mbp is `os: darwin`), so a `linux` worker is config-expressible.

**What's still missing for real Docker-based testing:** the L3 tests are container-aware but skip without Docker; OrbStack/Docker daemon must be running (it was down during this program — L3 was sequenced LAST precisely so this wasn't blocking). No evidence the L3 container scenarios have been *executed* with Docker actually up — they are present but unproven live. The full agent-launch / SSH-into-Linux-container path beyond telemetry collection is not demonstrated.

**Multi-remote (N machines) — NOT BUILT.** This is the load-bearing gap. The production worker registry is **single-worker by construction**:
- `internal/workers/registry.go`: `type Registry` holds a single `worker` + `hasWorker bool` (not a slice). `Load` does `r.worker = cfg.Workers[0]` — **it takes only the first worker** in `workers.yaml` and ignores the rest.
- `SelectWorker()` / `SelectWorkerByName(target)` operate on that one worker; "failover" in the L5 property tests means "fall back to LOCAL," not "pick another remote."
- So L3 (N containers) and L5 (N fake runners) prove the *routing/scheduler invariants* against multiple targets in tests, but the real daemon can drive **exactly one** remote machine today. Genuine multi-remote dispatch needs the registry generalized from single-worker to a worker pool — that work is not filed as a near-term bead and not started.

**Also blocking real Docker/remote testing operationally:** `gb-mbp` is `enabled: false` (repointed local for this program), and `hk-xjbvi` (P2, OPEN) records that worker enable/disable is **restart-only** — flipping `workers.yaml` does nothing without a full daemon restart.

---

## Q4 — The live e2e validation (`hk-nepva`) + gate chain

`hk-nepva` (P1, OPEN, `codename:remote-hardening`) is **the headline owed step**: run the worker end-to-end on the REAL `gb-mbp` remote and prove the pyramid's FS/git/tmux/SSH separation predictions + all landed remote fixes hold live. It is **blocked-by `hk-t1t00`**.

**Gate chain (from `hk-nepva`'s own comment, operator nuance via admiral 2026-06-25):**
1. **STAGE 1 — gate-base fix lands.** `hk-t1t00` (P1 bug, IN_PROGRESS on gurney-q, dispatched as DOT triple-review run `019effd5-851b`). Its premise was corrected: NOT "export `HK_GATE_BASE_SHA`" (that symbol does not exist in Go); real scope = the remote `commit_gate` must compute its affected-set against a correct, non-stale base, regression-tested on the L1 twin harness. Blocks `hk-nepva`.
2. **STAGE 2 — captain deploys the daemon + re-enables `gb-mbp`.** Deploy activates the reviewer fix `hk-f3u6o` (`5999a39a`, on main but not yet in the running binary) + the gate-base fix. Deploy must set config `daemon.liveness_no_progress_n` (per the `hk-drygf` required-key landmine) + concurrency. Then re-enable `gb-mbp` in `workers.yaml` (registry builds at startup → same restart).
3. **STAGE 3 — real-remote e2e.** gurney runs the proof beads on the real `gb-mbp`: `hk-h106` (worker writes a hostname proof file + commits) + `hk-4lrj` (DOT triple-review remote run lands on main) + confirm the pyramid FS/git/tmux/SSH predictions hold. **Confirm routing via `events.jsonl` `run_started.worker_name=gb-mbp`, NOT daemon stderr** (the stderr grep always returns 0 — known false signal).

Lane epic for the hardening work: `hk-gx0dl` (`codename:remote-hardening`, OPEN, assignee gurney) — children are `hk-f3u6o` (CLOSED) + `hk-t1t00` (in flight).

---

## Q5 — Done vs open ledger + questions for next session

### DONE (on main, verified)
- Pyramid L0–L5 (`hk-6l941` epic + 6 children CLOSED; all test files present on main).
- Daemon-restart-survival for runs: `hk-o85ye` + `hk-78tji`.
- Remote reviewer-verdict-read fix `hk-f3u6o` (`5999a39a`, ancestor of HEAD).
- Linux worker-telemetry/collector path + L3 container scenarios (`hk-yflqo`, `4bdf7e93`).

### OPEN
- `hk-t1t00` — remote stale-base gate fix; IN_PROGRESS / in review on gurney-q. **Critical path for the live e2e.**
- `hk-nepva` — live e2e on real `gb-mbp` (headline owed proof); blocked by `hk-t1t00`.
- `hk-h106`, `hk-4lrj`, `hk-tagp`, plus 6× worktree-create concurrency proofs (`hk-icdz`/`3zij`/`d2z1`/`tzfw`/`xbpm`/`k0pz`) — remote e2e proof beads, all OPEN, run in STAGE 3.
- `hk-xjbvi` (P2) — worker enable/disable is restart-only; no live operator toggle.
- `hk-n5md3` (P3) — L5 direct gate test, de-scoped backlog.
- **NOT FILED:** multi-remote (worker-pool) generalization of the single-worker registry; live execution of the L3 Docker scenarios with Docker up.

### Questions for the operator to review next session
1. **Greenlight the deploy?** STAGE 2 restarts the production daemon to pick up `5999a39a` + the gate-base fix + re-enable `gb-mbp`. This is the only step that perturbs the live fleet. Go / hold?
2. **Multi-remote scope.** The registry is single-worker (`cfg.Workers[0]`). Real "N Docker remotes + a local" (the L3 vision against production, not tests) needs a worker-pool. Do you want that filed now, or is one remote enough for the foreseeable phase?
3. **Run L3 with Docker up?** The container scenarios are on main but skip without Docker (OrbStack was down). Worth starting OrbStack and actually executing them once, to convert "present" → "proven"?
4. **Live worker toggle (`hk-xjbvi`).** Without it, every remote enable/disable needs a manual daemon restart. Prioritize alongside the e2e, or defer?
5. **The "disposable daemon in a worktree" idea** (Q2) is currently a *skipped* design, with the isolation goal met via separate-clone + out-of-daemon fixes. If you actually want a live second daemon for testing, it's net-new work (pidfile-singleton + worktree-collision must be solved). Revisit, or leave skipped?

### Translations glossary
- `hk-6l941` = the test-pyramid epic (CLOSED). `hk-hd2w6/52xnr/8u2al/yflqo/3q92c/f10xl` = pyramid layers L0/L1/L2/L3/L4/L5.
- `hk-nepva` = the live "run it on the real spare MacBook" e2e proof (headline, owed).
- `hk-t1t00` = fix for "the remote worker's stale copy of main makes the commit-gate think 374 files changed → 15-min timeout loop."
- `hk-f3u6o` = fix for "the reviewer's verdict file is written on the remote box but the daemon read it locally."
- `hk-gx0dl` = the remote-worker hardening lane epic (gurney).
- `hk-xjbvi` = "you can't turn the remote worker on/off without restarting the daemon."
- `gb-mbp` = the spare MacBook Pro remote worker (Tailscale `100.87.151.114`), currently disabled.
- move ④ = "two daemons on the same repo" — the disposable/isolated-daemon idea, deliberately skipped.
