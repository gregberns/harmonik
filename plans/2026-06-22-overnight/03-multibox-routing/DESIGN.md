# C3 — Multi-box execution routing + worker scaling (DESIGN)

**Date:** 2026-06-22 (overnight) · **Cluster:** C3 of `plans/2026-06-22-overnight/00-BRIEF.md`
**Scope:** DESIGN-ONLY. Configurable local-vs-remote routing, per-box worker scaling, run-on-both, and a tonight go/no-go for shifting load onto gb-mbp.

---

## TL;DR — tonight go/no-go

**PARK FOR MORNING (do NOT point unattended load at gb-mbp tonight).** Two independent gaps block it:

1. **Worker selection is currently silently broken.** On the last attempt (logged in `.harmonik/workers.yaml:16-22`), a proof bead with `enabled:true` on the fixed binary ran **local**, not remote — the daemon log showed *zero* worker-registry engagement (no `SelectWorker`/`workers.Load` lines). Until that "why didn't routing fire" question is answered *attended*, arming the worker overnight just runs everything local anyway (best case) or wedges (worst case).
2. **Concurrency on the remote box is entirely unproven.** Every GREEN remote run to date was a single sequential bead. All 6 concurrency proofs are OPEN. The fetch-race retry fix (082fddc4) has never fired in a live run. Unattended = concurrent dispatch against exactly the shared `worktree-create` + `run/<id>` ref paths where the races live.

The remote path IS real and single-run GREEN (review-loop hk-620j→15ca1eb3 and DOT hk-4lrj on 2026-06-21). The routing *model* below is buildable now. But "flip it on and walk away" is not safe tonight. The safe bridge is one attended re-prove session in the morning (steps in §6).

---

## 1. Current remote-substrate state (file:line + open-bug status)

### 1.1 The execution path (what already works)

| Stage | Where | Note |
|---|---|---|
| Worker config load | `internal/workers/workers.go:20-67` (`Load`, `Config`, `Worker`) | `.harmonik/workers.yaml`; **v1 = at most ONE worker** (`ErrTooManyWorkers`). Absent file → local-only. |
| Worker registry / selection | `internal/workers/registry.go:35-50` (`SelectWorker`) | Single worker, reserves a slot if `Enabled && inFlight < MaxSlots`. **This is the entire routing decision today.** |
| **Routing decision point** | `internal/daemon/workloop.go:2746-2753` | `if w := deps.workerRegistry.SelectWorker(); w != nil { rbc = &remoteBeadCtx{...} }`. Everything downstream branches on `rbc != nil`. |
| Reverse tunnel (TCP loopback) | `internal/daemon/workloop.go:2779-2846` | Allocates a free port, `tcp://127.0.0.1:<port>`, `ssh -N -R`, then `waitWorkerSocketLive` readiness gate. |
| Hookrelay TCP dial | `internal/hookrelay/hookrelay.go:492-542` | `tcp://` prefix → TCP dial (remote); else unix socket (local). Fixes root-owned-socket gap (hk-ege6). |
| Spec-write runner | `internal/daemon/reviewloop.go:280-296` (`claudeRunCtx.runner`) | settings/task/trust land on the WORKER for remote (`rbc.sshRunner`), nil for local. |
| Substrate-spawn runner | `internal/daemon/workloop.go:3520-3524` and `reviewloop.go:341-349` | claude PROCESS + pasteinject/liveness probes spawn on the WORKER's tmux server. |
| SSHRunner argv quoting | `internal/lifecycle/tmux/runner.go:92-121` | Per-token single-quoting so tmux `#{pane_id}` survives the remote login shell (hk-fxy9/hk-538l). |
| Box-A fetch run branch | `internal/daemon/codesync_rs_b8.go:103-175` (`fetchRunBranchBoxA`) | SSH-direct `git fetch ssh://<host>/<repo> run/<id>:...`; 3× backoff retry on "couldn't find remote ref" (082fddc4). |
| run_started worker metadata | `internal/daemon/workloop.go:2973-2979, 4811-4827` | event payload carries `worker_name`/`worker_os` — the only place worker selection is observable (text log does NOT show it; hk-w1z9). |

### 1.2 Open-bug status (for the go/no-go)

| Issue | Verdict | Evidence |
|---|---|---|
| Box-A fetch "couldn't find remote ref" (hk-zsn7 / hk-h106) | **FIX-LANDED-UNVALIDATED** | hk-zsn7 CLOSED by 082fddc4 (retry); but retry never fired live, and the re-proof bead **hk-h106 is OPEN** (proof file absent on disk). |
| Worktree-create silent/intermittent (workloop.go ~2578-2618) | **CLOSED 2026-06-20** by a6fdba7c | Now emits `run_failed("worktree_create_failed")` + routes cleanup through SSH runner. Reviewed APPROVE, unit GREEN. No open remote worktree-create defect. |
| agent_ready over the tunnel | **CLOSED & PROVEN** | hk-ege6 (3c5f8121 TCP loopback), hk-gglt (e3bc73f7 trust), hk-fxy9/hk-538l (two-runner-split, 4befa185). Zero agent_ready_timeouts after 2026-06-21 03:47Z. DOT proof hk-4lrj GREEN. |
| **Worker SELECTION not firing** (NEW, undiagnosed) | **OPEN — blocking** | `.harmonik/workers.yaml:16-22`: with `enabled:true` on the fixed binary, proof bead hk-h106 ran LOCAL (pane %6), daemon log had ZERO worker-registry lines. Root cause unknown (boot probe? flag? gated path?). |
| Concurrency on remote | **UNPROVEN** | All 6 proofs OPEN (hk-icdz/3zij/d2z1/tzfw/xbpm/k0pz). Every GREEN run was single-bead. |

**Live config state:** `.harmonik/workers.yaml` exists, gb-mbp at Tailscale `100.87.151.114`, `max_slots: 4`, **`enabled: false`** (window closed). Worker telemetry dark since 2026-06-21 06:41Z. The running daemon is NOT pointed at gb-mbp.

---

## 2. Routing design (config surface + resolution order + where it hooks in)

### 2.1 The model

Today routing is implicit and binary: "remote worker has a free slot → go remote, else local." That is not a *router* — there's no policy, no per-queue/per-item control, no force-local, no OS-targeting. The design adds an explicit **routing policy** evaluated at the existing decision point (`workloop.go:2746`), keeping the single-worker substrate intact for V1 but shaping the config so multi-worker is a later extension, not a rewrite.

A route resolves to a **target**: `local` or a named **box** (`gb-mbp`). The resolver answers one question per dispatch: *which box runs this bead?*

### 2.2 Config surface

Routing config lives in `.harmonik/workers.yaml` (it already owns box identity; co-locating routing keeps boxes and routing in one file). The `daemon:` block in `.harmonik/config.yaml` tolerates unknown keys (PL-004b, `projectconfig.go:216-224`) but the keeper block is strict — **do not** put routing under keeper.

```yaml
# .harmonik/workers.yaml
version: 2                      # bump; v1 stays single-worker-only
routing:
  default: prefer-remote        # prefer-remote | local | round-robin | <box-name>
  # prefer-remote = current behavior (remote if a slot is free, else local)
  # local         = force-local (testing; ignore all workers)
  # round-robin   = alternate across enabled boxes (incl. local as a pseudo-box)
  # <box-name>    = pin everything to that box (e.g. "gb-mbp")
workers:
  - name: gb-mbp
    transport: ssh
    host: 100.87.151.114
    os: darwin                  # ALREADY a field — drives OS-targeting
    repo_path: /Users/gb/harmonik-worker/repo
    max_slots: 4
    enabled: true
queues:                         # OPTIONAL per-queue overrides
  - name: remote-substrate
    route: gb-mbp               # whole-queue pinned to a box
  - name: local-smoke
    route: local                # force-local for a queue
```

Per-item override (lowest scope, highest priority) rides on the queue document each item already carries (queue items are self-describing per hk-tldws). Add an optional `route` field per item and a submit flag:

```
harmonik queue submit --beads hk-a --route gb-mbp     # NEEDS-OPERATOR-DECISION (new flag)
harmonik queue submit --beads hk-b --route local      # force-local one item
# OS constraint (item must run on macOS): --requires-os darwin
```

### 2.3 Resolution order (mirrors C1)

**per-item `route` > queue `route` > `routing.default` > compiled default (`prefer-remote`, = today's behavior).**

A separate, *harder* constraint layer sits on top: **OS-targeting is a filter, not a preference.** If an item declares `--requires-os darwin`, the resolver eliminates any box whose `os` ≠ `darwin` *before* applying the preference order; if that leaves no eligible box, the bead is held (not silently run on the wrong OS). Local box OS is the daemon host's OS.

Resolution pseudocode (new `internal/workers/router.go`):

```
ResolveTarget(item, queue, cfg) -> Target | Hold:
    candidates = [local] + enabledWorkers(cfg)
    if item.RequiresOS != "":                       # hard filter first
        candidates = filter(candidates, os == item.RequiresOS)
        if empty: return Hold("no box satisfies requires_os=...")
    policy = firstNonEmpty(item.Route, queue.Route, cfg.Routing.Default, "prefer-remote")
    switch policy:
      "local":        return local (if local in candidates, else Hold)
      "<box-name>":   return that box (if in candidates & has free slot, else Hold/defer)
      "prefer-remote":return firstRemoteWithFreeSlot(candidates) ?? local
      "round-robin":  return nextWithFreeSlot(candidates, cursor)
```

### 2.4 Where it hooks in

The router slots in at the **single existing decision point**, `internal/daemon/workloop.go:2746-2753`. Today:

```go
if w := deps.workerRegistry.SelectWorker(); w != nil { rbc = &remoteBeadCtx{...} }
```

becomes:

```go
target := deps.router.ResolveTarget(item, queue, cfg)   // NEW
if target.IsRemote() {
    if w := deps.workerRegistry.SelectWorkerByName(target.Box); w != nil { rbc = &remoteBeadCtx{...} }
    // else: no slot on the chosen box → fall through to local OR defer (policy-dependent)
}
```

This is the *only* place the daemon decides local-vs-remote (confirmed by the research: every remote branch keys off `rbc != nil`). The router is a pure function over (item, queue, config) + live slot state; it does not touch the spawn/tunnel/fetch plumbing, all of which already correctly branch on `rbc`.

---

## 3. Per-box worker-cap / scaling model

### 3.1 What exists today (three independent caps)

1. **Global cap** — `ConcurrencyController` (`internal/daemon/concurrencycontroller.go:22-52`), read every dispatch tick at `workloop.go:1461-1467`. Box-AGNOSTIC. Live-mutable via `queue set-concurrency`.
2. **Per-queue cap** — `Queue.Workers` (`internal/queue/types.go:273-287`), tallied by `RunRegistry.LenForQueue` (`runregistry.go:155-173`), gated in `selectNextQueue` (`workloop.go:1157-1198`). Box-AGNOSTIC.
3. **Per-box cap** — `Worker.MaxSlots` (`workers.go:43`), tallied by `Registry.inFlight` (`registry.go:11,44-47`). This is the ONLY box-aware cap, and it gates ONLY remote selection.

Key fact: **concurrency accounting is box-agnostic except for `MaxSlots`.** `RunHandle` has no box field (`runregistry.go:32-49`). The global cap counts local + remote runs together. So with `max_concurrent=4` and a remote `max_slots=4`, the *effective* total is still 4 (global wins) — you do NOT automatically get 4 local + 4 remote.

### 3.2 The real ceiling on gb-mbp is NOT CPU/RAM — it's the Claude session/rate limit

gb-mbp authenticates via `CLAUDE_CODE_OAUTH_TOKEN` (subscription) in its `~/.zshenv` (`workers.yaml:4`). Each concurrent remote run is a full `claude` interactive session under that ONE subscription. RAM/CPU on the box is ample; the binding constraint is **how many concurrent Claude sessions + how much token throughput that subscription tolerates before rate-limiting/queuing**. CPU is a red herring here.

**Recommended starting worker count: keep `max_slots: 4`, but prove at 2 first.** Rationale: the box has never run >1 concurrent remote bead. Jump to 2 concurrent for the first concurrency proof (catches the worktree/ref races cheaply), then 4 once 2 is GREEN. Don't crank to 8+ until session-limit behavior is observed — a rate-limited session manifests as agent stalls/timeouts that look like wedges.

**How to watch session limits:** the only structured signal is the event bus. Watch for:
- `harmonik subscribe --json` filtered for `run_started` with `worker_name=gb-mbp` (confirms routing fired — directly addresses the §1.2 selection bug).
- a spike in `agent_ready_timeout` / `run_failed` on remote runs = likely session throttling, not a code bug.
- `worker_report` telemetry cadence (it went dark — its return is the heartbeat that the box is live).
- ccusage on the worker's subscription for token-burn (per memory `reference_token_burn_diagnosis_ccusage`).

### 3.3 The needed model change

For "push the remote box harder than the local box," the global cap must stop being the de-facto total. **Make the global cap a ceiling and let per-box caps sum under it:**

- Keep `max_concurrent` (global) as a hard ceiling on total in-flight (safety backstop).
- Each box has its own slot count: local gets an implicit `local_max_slots`, gb-mbp keeps `max_slots`. Total dispatch = `min(sum of per-box in-use-able slots, global ceiling)`.
- Add a box field to `RunHandle` so the registry can tally per-box in-flight (today `Registry.inFlight` is internal to the remote registry; local has no equivalent). This is the load-bearing change that makes per-box scaling real.

V1 can skip this and just raise the global cap when pinning a queue to gb-mbp — but that's the "manager-heavy 3 crews / 4 slots" complaint restated: without per-box accounting, bumping the global cap pressures BOTH boxes.

---

## 4. Run-on-both — minimal V1

Goal: some work runs local, some on gb-mbp, simultaneously. Minimal change that requires NO new accounting subsystem:

**V1 = per-queue route pin + raise the global cap.**
- Pin one queue to `gb-mbp` (`queues: [{name: X, route: gb-mbp}]`), leave `main` at `prefer-remote`/`local`.
- Set global `max_concurrent` to (local slots you want) + (gb-mbp `max_slots`), e.g. 4 + 2 = 6 during the experiment.
- Result: the gb-mbp-pinned queue dispatches up to its `max_slots` remote; the local queue fills the rest locally; they run concurrently under the shared global ceiling.

This reuses everything that exists: per-queue `Workers`, the `MaxSlots` remote cap, and `selectNextQueue` round-robin. The only NEW code is the queue→box `route` field + the router reading it at `workloop.go:2746`. It does NOT need the per-box `RunHandle` accounting from §3.3 (that's the "shared pool spanning boxes" V2). It DOES need the §1.2 selection bug fixed first — otherwise the pinned queue silently runs local.

The "manager-heavy 3 crews / 4 slots" observation: run-on-both directly helps — pinning a crew's queue to gb-mbp moves that crew's slot pressure off the local box, so the same 3 crews drive more total throughput without 3 crews fighting over 4 local slots.

---

## 5. Risks

- **Silent local fallback.** `prefer-remote` falls back to local when no remote slot is free. Under the §1.2 selection bug, "remote" work runs local with no error — wasted setup, false sense of remote load. Mitigation: make the router emit a structured `routing_decision` event so fallback is observable (today only `run_started.worker_name` shows it, and only post-selection).
- **Orphaned remote worktrees on daemon crash.** Daemon shutdown does NOT clean in-flight remote worktrees (research §6); `git worktree remove` runs only on normal per-run completion (`workloop.go:2919-2926`). An unattended crash leaves litter on gb-mbp that the next run's worktree-add may collide with.
- **Session throttling looks like a wedge.** A rate-limited subscription stalls agents → `agent_ready_timeout`, indistinguishable from the bugs just fixed. Don't auto-escalate; check ccusage/telemetry first.
- **No live worker-disable RPC.** `SetEnabled(false)` is internal (health-check only); the only operator disable is restart with `--worker-enabled=false` or edit workers.yaml + restart. Rollback is ~2s but NOT a single live command (see §6). This is itself a small buildable gap (RPC + CLI verb).
- **Concurrency races (the real blast radius).** Shared `run/<id>` ref namespace + worktree-create under concurrency are the unproven paths. A wedge here strands beads `in_progress` on the worker. Reversible (restart resets to `open`) but messy unattended.

---

## 6. Tonight go/no-go + rollback

### Verdict: PARK FOR MORNING. Do an ATTENDED re-prove, then arm.

Why not tonight: the worker is `enabled:false`, the last enable attempt hit an *undiagnosed* selection failure (work ran local), and concurrency is unproven. Arming it unattended risks (a) running everything local anyway = no benefit, or (b) a concurrency/selection wedge stranding the overnight queue with no one watching. The path is single-run GREEN but not unattended-ready.

### Attended morning sequence (the safe bridge)

1. Diagnose selection: start daemon with the worker enabled and confirm engagement —
   `git -C /Users/gb/github/harmonik` edit `workers.yaml` `enabled: false → true`, restart daemon, then
   `harmonik subscribe --json | grep -i gb-mbp` while submitting ONE proof bead. Confirm a `run_started` with `worker_name=gb-mbp` (NOT a local pane). This answers §1.2.
2. Re-prove the fetch fix: run hk-h106 (the rfix e2e proof) attended; confirm GREEN + write the proof file.
3. Prove concurrency at 2: `max_slots: 2`, submit 2 beads to a gb-mbp-pinned queue, watch both reach `run_started` on gb-mbp and both commit. Run one of hk-icdz/3zij/etc.
4. Only if 1–3 are GREEN: bump `max_slots` to 4, raise global `max_concurrent` to 6, and let run-on-both ride.

### Rollback (instant-ish, ~2s — keep this ready)

- **Fastest local-only:** edit `.harmonik/workers.yaml` `enabled: false`, then `pkill harmonik` (supervisor revives on the fixed config; in-flight `in_progress` beads reset to `open` and re-dispatch local). Equivalent: restart with `--worker-enabled=false` (`cmd/harmonik/main.go:837-850,938`).
- **Throttle without disabling:** `harmonik queue set-concurrency 1` (live, no restart) — caps total in-flight to 1, drains the experiment safely.
- **Pause a runaway queue:** `harmonik queue pause <name>` (live, per-queue; no global pause verb).
- **Blast radius if it wedges:** bounded to in-flight remote beads (≤ `max_slots`); they strand `in_progress` on gb-mbp, recovered by restart. No effect on the local lanes (paul/stilgar/logmine) as long as run-on-both keeps them on a separate local queue.

---

## 7. Phased bead breakdown

### BUILDABLE-NOW (no operator decision; reversible; mostly the router + observability)

- **R1 — `internal/workers/router.go`**: pure `ResolveTarget(item, queue, cfg)` with resolution order (per-item > queue > config-default > `prefer-remote`) + OS hard-filter. Unit-tested in isolation. *Buildable; no behavior change until wired.*
- **R2 — workers.yaml v2 schema**: add `routing.default`, per-queue `route`, optional per-item `route`/`requires_os`; keep v1 single-worker as a compat path. Strict-parse the new block.
- **R3 — wire R1 at `workloop.go:2746`**: replace the bare `SelectWorker()` with `router.ResolveTarget` → `SelectWorkerByName`. Behind a config that defaults to today's behavior (`prefer-remote`) so it's a no-op until configured.
- **R4 — `routing_decision` event**: emit target + reason (incl. local-fallback) so routing is observable on `subscribe --json`. Directly fixes the §1.2 blind spot.
- **R5 — diagnose worker-selection-not-firing** (§1.2): reproduce attended, find why `SelectWorker` didn't engage with `enabled:true`. *Investigation; gates everything remote.*
- **R6 — live worker enable/disable RPC + CLI** (`harmonik worker disable|enable`): make rollback a single live command instead of restart. Mirrors `queue set-concurrency` (`socket.go:472`, `registry.SetEnabled` already exists).

### NEEDS-OPERATOR-DECISION

- **D1 — per-box concurrency accounting** (§3.3): add box to `RunHandle`, sum per-box caps under the global ceiling. This is the "push gb-mbp harder than local" enabler but it changes the concurrency contract (global cap goes from total to ceiling). Operator should sign off on that semantics change.
- **D2 — multi-worker (v2 lifting the one-worker invariant)**: today `ErrTooManyWorkers`. Round-robin/random across >1 box needs this. Decide if/when a 2nd box exists.
- **D3 — default routing policy**: should the shipped default stay `prefer-remote`, or `local` (opt-in remote)? Bears on safety posture — `local` default is safer (remote never fires unless asked), `prefer-remote` maximizes the box.
- **D4 — fallback-vs-defer on no-slot**: when a pinned box is full, fall back to local (throughput) or hold/defer (honor the pin)? Affects whether a gb-mbp-pinned queue can leak onto local.

### Dependency order

R5 (diagnose selection) → R1/R2/R3 (router) → R4 (observability) → run-on-both V1 (§4) → D1 (per-box accounting) → D2 (multi-worker). R6 is independent and worth doing early (cheap rollback).

---

## Appendix — key file:line index

- Routing decision point: `internal/daemon/workloop.go:2746-2753`
- Worker registry/selection: `internal/workers/registry.go:35-50` (`SelectWorker`), `:64-70` (`SetEnabled`)
- Worker schema: `internal/workers/workers.go:36-67`; live config `.harmonik/workers.yaml` (gb-mbp, `enabled:false`)
- Global cap: `internal/daemon/concurrencycontroller.go:22-52`; gate `workloop.go:1461-1467`
- Per-queue cap: `internal/queue/types.go:273-287`; `runregistry.go:155-173`; `workloop.go:1157-1198`
- Per-box cap: `workers.go:43` (`MaxSlots`); `registry.go:44-47`
- Tunnel + readiness: `workloop.go:2779-2846`; hookrelay `internal/hookrelay/hookrelay.go:492-542`
- Box-A fetch + retry: `internal/daemon/codesync_rs_b8.go:103-175`
- run_started worker metadata: `workloop.go:2973-2979, 4811-4827`
- Config block (tolerant): `internal/daemon/projectconfig.go:216-224`
- Queue control CLI: `internal/queue/cli/setconcurrency.go`, `.../pause.go`, `.../resume.go`; RPC `internal/daemon/socket.go:472-475`
- Boot worker flags: `cmd/harmonik/main.go:837-850, 938`
