# Remote-worker program — corrections & next steps

_Capture doc, 2026-06-25. PLAN artifact — does not edit any live file. Sibling to
`REMOTE-PROGRAM-STATUS.md` (the done-vs-open ledger); read that first. Every claim here
was verified against `br show` / `git log` / source on `main` (HEAD `5ba405cf`); the
verification command is named inline so a future reader can re-check._

This doc exists to record **operator corrections** so good work and good ideas from the
remote-worker program don't get lost between sessions. Four things:

1. Correct the record on the "two daemons" idea — it was misread, and the operator's
   actual idea was never evaluated. Evaluated here with a feasibility verdict + a spike.
2. Confirm the L1 twin-clone harness and a real-remote e2e are **complementary**, not
   substitutes — a real remote run is still wanted.
3. OrbStack is **now running** → the L3 container scenarios are unblocked to run live.
4. Multi-remote (worker-pool) must not get lost — inventory of the associated work.

---

## 1. CORRECT THE RECORD — the "two daemons" idea was misinterpreted

### What got written down (and why it's a misread)

`REMOTE-PROGRAM-STATUS.md` Q2 and the impasse plan
(`.harmonik/crew/designs/remote-iteration-impasse-plan.md` line 38, "move ④") both record
the idea as **"two daemons on the SAME repo"** and reject it on two grounds:

- daemon pidfile flock makes it a singleton — a second instance in the same project dir
  exits 5;
- two daemons on one working dir mutually destroy each other's tmux/worktree state and
  contend on the beads WAL.

Both of those rejections are **correct for the same-dir framing**. The problem: the
operator **never specified same-dir.** "Two daemons on the SAME repo" is a crew/admiral
gloss, not the operator's words. The singleton-lock objection only bites when both daemons
point at the *same project directory* — which the operator's actual idea does not.

### The operator's ACTUAL idea

Spin up a **separate worktree (or separate clone / project dir)**, stand up a **second
"TEST" daemon there** configured to point at the **REMOTE worker**, run a test through it,
and **if it finds issues, file them back to the MAIN daemon's working queue.** A disposable
test daemon that exercises the *real remote* without perturbing the production fleet — and
whose findings flow back as beads into the live queue.

This is materially different from move ④ and **was never properly evaluated.**

### Feasibility verdict: FEASIBLE — a separate project dir sidesteps the singleton concern

**Verdict: feasible, low-to-moderate effort, no new daemon-core code required for a first
spike.** Each sub-point checked against source:

- **Does a separate worktree / project dir sidestep the pidfile-singleton concern?**
  **Yes.** The pidfile + socket + tmux namespace are derived **per project directory**
  (the project-hash). The impasse plan itself records this under move ① (line 20):
  > `git clone` to a different path → distinct project-hash → fully isolated socket +
  > tmux namespace; `harmonik --project <scratch>` runs STANDALONE (no supervisor/revive)
  > so a crew can `pkill` + rebuild it in seconds without touching the fleet daemon.

  And fleet-portability (landed 2026-06-13) **explicitly sanctions one-daemon-per-project**.
  So a TEST daemon in a *separate clone or worktree* is two daemons on **two distinct project
  dirs** — never the singleton-collision case. The rejection in move ④ simply does not apply
  to the operator's framing.

  One caveat for a true `git worktree` (vs a fresh `git clone`): worktrees of the same repo
  share one `.git/` object store and one set of branch refs. The daemon's own worktree/merge
  machinery operates on branches, so a TEST daemon in a linked worktree could still collide
  on *ref* operations even though pidfile/socket/tmux are isolated. A **separate `git clone`**
  (own `.git`, syncing via the GitHub remote — exactly how the real remote worker already
  syncs) is the clean, collision-free substrate. Recommend the spike use a separate clone,
  not a linked worktree, unless ref-isolation is proven.

- **What config points a daemon at the remote?** The worker registry. `internal/workers/
  workers.go` defines `Config{ Version, Workers []Worker, ... }`, loaded from
  `.harmonik/workers.yaml` at daemon boot (`internal/workers.Load`). A `Worker` carries
  `name / transport / host / os / repo_path / max_slots / enabled / harmonik_path`. So the
  TEST daemon's project dir just needs its own `.harmonik/workers.yaml` with the gb-mbp
  entry `enabled: true` (host `100.87.151.114`, the Tailscale IP — `ssh gb-mbp` MagicDNS
  does not resolve on box A). No code change: pointing a daemon at the remote is purely a
  config-file fact, and the registry already builds from that file at startup.

- **How would found-issues be filed back to the MAIN daemon's queue?** Beads are the
  shared ledger and `br` is the single CLI surface — `br create` writes to `.beads/` for the
  project the TEST daemon belongs to, but the MAIN daemon's queue is fed by *its* beads DB.
  Two clean paths:
  1. **`br create` in the MAIN project dir** (`git -C <main-repo> ... br create ...`), then
     `harmonik queue submit/append` against the **main** daemon's socket. The TEST daemon
     produces findings; the operator/crew files them as beads into the main project and the
     main daemon dispatches them normally. Simplest; no new mechanism.
  2. **comms bus** — the TEST daemon's crew posts findings over `harmonik comms` to the
     captain/operator, who triages them into main-queue beads. Heavier, but already the
     standard cross-session coordination channel.

  Either works today with zero new code. The "file back to the main queue" half is just
  "create a bead in the main project and submit it" — the two daemons never share state,
  they hand off via the beads ledger, which is the intended seam.

**Net:** the operator's idea is sound and structurally already supported. The only reason it
read as "rejected" is that it got collapsed into the genuinely-unsafe same-dir variant.

### Proposed spike (small, ~half-day, no daemon-core changes)

**Goal:** prove a disposable TEST daemon in a separate clone can drive the REAL gb-mbp
remote and feed a finding back to the main queue, without touching the production daemon.

1. `git clone` the repo to a scratch path (e.g. `/Users/gb/harmonik-remote-test`) →
   distinct project-hash → isolated socket/tmux/pidfile. (Confirm: `harmonik --project
   <scratch> status` binds a fresh socket; `harmonik --project <main> status` unaffected.)
2. Drop a scratch `.harmonik/workers.yaml` with the gb-mbp entry `enabled: true`
   (host `100.87.151.114`, `repo_path: /Users/gb/harmonik-worker/repo`, `max_slots: 1`).
   Pre-flight the 4 boot probes over non-interactive ssh (tmux / claude / repo HEAD /
   OAuth token) — same checklist chani ran (epic `hk-rs-phase1-qfn1` comments).
3. Start the TEST daemon STANDALONE (no `supervise`, so no auto-revive — a crew can
   `pkill`+rebuild it freely; bootstrap-trap avoided structurally).
4. Dispatch one tiny proof bead routed to gb-mbp on the TEST daemon; confirm routing via
   `events.jsonl` `run_started.worker_name=gb-mbp` (**NOT** daemon stderr — the stderr
   grep always returns 0; documented false signal).
5. **Find-back loop:** if the run surfaces a defect, `br create` it in the **main** project
   dir and `queue submit` it to the **main** daemon — proving the hand-off seam end-to-end.
6. Tear down: `pkill` the TEST daemon, `rm -rf` the scratch clone. Production daemon never
   perturbed.

If step 4 lands clean, this spike *also* discharges much of `hk-nepva` (the headline live
e2e) on a substrate that can't hurt the fleet — a strong argument for doing the spike first.

---

## 2. The L1 twin-clone harness is good — but a REAL remote test is STILL wanted

**Confirmed: complementary, not substitutes.** Both are needed.

- The **L1 twin scenario harness** (`hk-52xnr`, on main `60aaf419`) backs its "remote"
  runner with a **separate FS + separate git clone on localhost** and boots the real daemon
  through route→launch→implement→gate→review→merge against a stubbed agent in ~6s, no LLM.
  It deterministically catches the *whole bug class* (verdict-read asymmetry `hk-f3u6o`,
  gate-base staleness) fast. It is the right tool for fast regression and for reproducing
  separate-machine bugs without a 30-90 min live loop.

- **What L1 does NOT test:** a real network hop, real SSH transport/quoting against a real
  remote host, real Tailscale datapath, real remote `claude` auth (subscription/OAuth on the
  worker), real worker FS/git/tmux, and the agent_ready hook firing back over the reverse
  tunnel from a genuinely-separate machine. Those are exactly where the historical remote
  bugs lived (the `#{pane_id}` SSH-quoting class, agent_ready_timeout, worker overload).

So the **headline real-remote e2e (`hk-nepva`, P1, OPEN, `codename:remote-hardening`) is
still owed.** It runs the worker end-to-end on the REAL gb-mbp and proves the pyramid's
FS/git/tmux/SSH separation predictions + all landed fixes hold live. It is blocked-by
`hk-t1t00` (remote gate-base staleness, IN_PROGRESS on gurney-q). The L1 harness makes the
*iteration* fast and de-risks the live run; it does not replace it.

The spike in §1 is one concrete way to discharge `hk-nepva` on a non-fleet-perturbing
substrate.

---

## 3. OrbStack is NOW RUNNING → L3 container scenarios are UNBLOCKED (immediate actionable)

**Verified live (`docker ps` returns running containers; `orbctl status` → "Running";
OrbStack vmgr process up).** During the test-pyramid program the Docker/Lima daemon was
**down**, so L3 was sequenced LAST and the container scenarios were written to **skip when
Docker is unavailable.** They have been **present but never executed live** (status doc Q3:
"present but unproven live").

That blocker is now **gone.** The L3 scenarios in
`internal/daemon/scenario_container_l3_hkyflqo_test.go` (epic child `hk-yflqo`, on main
`4bdf7e93`) — the FS-axis `DockerExecRunner` vs host `os.Stat` scenario, the Linux collector
against a real `alpine:latest` container, and the multi-container disjoint-FS gate-decision
scenario — can now **run for real** and convert "present" → "proven."

**Immediate actionable:** fold ONE live L3 run into the remote-worker lane — execute the
container scenarios with OrbStack up (the run is `go test -tags=scenario -run
<L3 scenarios> ./internal/daemon/` plus whatever Docker-detection tag the suite uses). The
captain has already been nudged to take this into the remote-worker lane. This is a
self-contained, low-risk first win that needs no remote machine and no daemon restart.

---

## 4. MULTI-REMOTE must not get lost — sub-epic inventory

### The load-bearing gap (verified in source)

The **production registry is single-worker by construction.**
`internal/workers/registry.go`:

```go
type Registry struct { worker Worker; hasWorker bool; inFlight int }   // a single Worker, not a slice
func NewRegistry(cfg Config) *Registry {
    if len(cfg.Workers) > 0 { r.worker = cfg.Workers[0]; r.hasWorker = true }  // takes ONLY Workers[0]
}
```

`SelectWorker()` / `SelectWorkerByName(target)` operate on that one worker; "failover" in
the L5 property tests means **fall back to LOCAL**, not "pick another remote." So even though
`workers.yaml` can *list* N workers (`Config.Workers []Worker` is already a slice — verified
in `workers.go`), the daemon drives **exactly one** (the first). Genuine N-machine dispatch
needs the registry generalized from a single `worker` to a **worker pool** (slice + per-worker
slot tracking + a selection policy across the pool).

**There is NO bead filed for the worker-pool generalization** (verified: `br list` title
search for pool / n-worker / multi-worker / second-worker / workers[ returns nothing). It is
the single biggest unfiled gap. **Recommend filing it now** so it's tracked — proposed:
`feature`, P2, `codename:multi-remote`, scope = "generalize `workers.Registry` from
single-`worker` (`Workers[0]`) to a worker-pool: slice of workers, per-worker slot tracking,
cross-pool selection policy; promote `SelectWorker` to choose among enabled workers with free
slots; keep local fallback."

### Inventory of the associated good work (each is a real, tracked sub-epic / bead)

All IDs verified via `br show` / `br list` on 2026-06-25.

| ID | Type / status | One-line |
|---|---|---|
| `hk-rs-phase1-qfn1` | epic · OPEN (assignee chani) | remote-substrate Phase 1: remote macOS SSH worker over SSH (the 12 build beads B1-B12; Phase-1 core LANDED — agent_ready/heartbeat PROVEN on gb-mbp, `hk-z8ek` materialization on main `ec7af221`). The foundation all multi-remote work builds on. |
| `hk-gx0dl` | epic · OPEN (assignee gurney) | Remote-worker **hardening** lane — durable gate-base fix + remote reviewer-verdict consistency. Children: `hk-f3u6o` (CLOSED), `hk-t1t00` (in flight). |
| `hk-6l941` | epic · **CLOSED** | remote **test-pyramid** L0-L5 (`codename:remote-test-pyramid`). L5 (`hk-f10xl`) + L3 (`hk-yflqo`) built the N-runner / N-container routing/scheduler scaffolding that a worker-pool would be validated against. |
| `hk-nepva` | task · OPEN · P1 | **headline** live e2e on the REAL gb-mbp; proves separation predictions hold live. Blocked-by `hk-t1t00`. |
| `hk-t1t00` | bug · IN_PROGRESS · P1 | remote gate: stale worker `origin/main` inflates the scenario-gate affected-set → 900s timeout loop → cap. Corrected premise (no `HK_GATE_BASE_SHA` symbol). On gurney-q. |
| `hk-f3u6o` | bug · **CLOSED** · P1 | remote reviewer verdict-read asymmetry — landed `5999a39a` (on main, **not yet in the running daemon binary**). |
| `hk-yflqo` | task · **CLOSED** | L3 Docker/Lima container harness + **Linux OS** target support (Linux telemetry collector + `os:` field). On main `4bdf7e93`. The Linux path is a prerequisite for a mixed macOS+Linux pool. |
| `hk-xjbvi` | bug · OPEN · P2 | worker enable/disable is **restart-only** — no live operator toggle; flipping `workers.yaml` is a no-op without a daemon restart. A pool would make a live toggle even more valuable. |
| `hk-tagp` | task · OPEN · P1 | rs gap7 e2e: remote bead edits a text file + commits on gb-mbp (proves agent_ready over the tunnel). |
| `hk-h106` | task · OPEN · P1 | remote e2e proof: worker writes a hostname proof file + commits. The re-run target once the reviewer fix is deployed. |
| `hk-4lrj` | task · OPEN · P1 | rs e2e proof DOT: triple-review remote run on gb-mbp lands on main. |
| `hk-rs-validate-remote-898a` | task · OPEN · P1 | rs VALIDATE: first remote-dispatch proof on gb-mbp. |
| `hk-icdz` / `hk-3zij` / `hk-d2z1` / `hk-tzfw` / `hk-xbpm` / `hk-k0pz` | tasks · OPEN · P2 | rs e2e-**concurrent** proofs #1-#6 — gb-mbp worktree-create under concurrency. These exercise *multiple concurrent remote runs against one worker*; the natural extension to *multiple workers* is the pool. |
| `hk-n5md3` | task · OPEN · P3 | L5 follow-up: direct test of the `LocalOnly`/`WorkerTarget` gate in `beadRunOne` (`workloop.go:2840`). The per-queue routing seam a pool selection policy would plug into. |
| `hk-dhe6` | epic · OPEN · P2 | rc-prefix: per-project Remote-Control session-label prefix (LANDED per memory; supports multiple remote-control sessions cleanly — relevant once multiple workers each host sessions). |

**NOT in the inventory (intentionally):** `hk-ljyyy` (crew-start mission-seed bug),
`hk-m3mln` (API-500 hang), `hk-bsdr`/`hk-kwyv` (token-burn / GH-bug lanes) surfaced in the
label search but are not remote-worker-program work.

### Multi-remote bottom line

The scaffolding is built (N-runner L5 property tests, N-container L3 scenarios, Linux OS
support, per-queue routing seam, concurrent-remote proofs). The **production registry is the
one piece still single-worker**, and that generalization is **unfiled** — the recommended
action is to file the `codename:multi-remote` worker-pool feature bead (P2) so it's explicitly
tracked rather than living only as "NOT FILED" in the status ledger.

---

## Translations glossary

- `hk-nepva` = the live "run it on the real spare MacBook" e2e proof (headline, owed).
- `hk-t1t00` = "the remote worker's stale copy of main makes the commit-gate think ~374
  files changed → 15-min timeout loop." Premise corrected: no `HK_GATE_BASE_SHA` symbol.
- `hk-f3u6o` = "the reviewer's verdict file is written on the remote box but the daemon read
  it locally." Landed on main, not yet in the running binary.
- `hk-yflqo` = the L3 Docker/Lima container harness + Linux-worker support.
- `hk-6l941` = the test-pyramid epic (L0-L5), CLOSED.
- `hk-rs-phase1-qfn1` = remote-substrate Phase 1 (the original "ship a remote SSH worker").
- `hk-gx0dl` = the remote-worker hardening lane epic (gurney).
- `hk-xjbvi` = "you can't turn the remote worker on/off without restarting the daemon."
- `gb-mbp` = the spare MacBook Pro remote worker (Tailscale `100.87.151.114`), `enabled:
  false` today (repointed local for the test program).
- "move ④" = the crew's "two daemons on the SAME repo" gloss — the genuinely-unsafe variant.
  The operator's ACTUAL idea (separate clone, TEST daemon → remote, file findings to the main
  queue) is feasible — see §1.
