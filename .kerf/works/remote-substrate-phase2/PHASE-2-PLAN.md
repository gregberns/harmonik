I have everything needed — three plans plus the adversarial critic with a clear synthesis directive. I'll merge them into one final Phase-2 kerf plan now, holding the interactive-not-`-p` constraint, the reuse-the-seam principle, and keeping P3 items out.

# remote-substrate — Phase 2 FINAL PLAN (Local Container Bead-Isolation)

> Merged from 3 independent planners + the adversarial critic's synthesis directive. Grounded in the live Phase-1 seam (`tmux.CommandRunner`), not the design docs alone. Hard constraints held throughout: **interactive claude in tmux + paste-inject, NEVER `claude -p`**; **reuse the one `CommandRunner` seam, no new substrate clone**; **P3 items stay out**.
>
> **Skeleton from Plan 2** (only plan correct on the per-container-tmux trap; best build order). **Grafted:** Plan 3's `IsTransportFailure` exit-code precision + `buildCodexEnv` env template + tightest non-goals; Plan 1's `runnerForWorker`/`Bin` factory + `AuthMode` gate + container-label GC.

---

## 1. PROBLEM-SPACE

### Goals
- **G1 (headline) — local-container bead-isolation.** Each bead's implementer workflow runs inside a per-bead **Linux container on box A** (gb-mac-mini), giving real blast-radius isolation (filesystem, process, network) for a misbehaving/prompt-injected agent — **without a second machine**.
- **G2 — reuse the interactive machinery verbatim.** The daemon `docker exec -i <ctr> tmux …` substitutes for `ssh host -- tmux …`; the existing interactive `claude`-in-tmux + paste-inject path is reused unchanged. Never `claude -p`.
- **G3 — two-phase egress.** Internet ON for a setup phase; default-deny + per-bead allowlist during the agent phase; per-bead opt-in for network-needing tests. Structural inoculation against the 2026-05-30 credit-burn / exfil class.
- **G4 — multiple workers + scheduling.** N entries in `workers.yaml` (today capped at 1 by `ErrTooManyWorkers`); a scheduler picks among them; a "worker" is now an SSH box (Phase 1) **or** a local container-pool, unified behind one registry.
- **G5 — Linux worker over SSH.** A Linux box (Asahi Mac / x86 VPS) reached by the existing `SSHRunner`; `os: linux`; token auth; arch + PATH/dep handling.
- **G6 — provisioning runbook reuse.** One declarative artifact stands up an SSH worker AND builds the container image (two render targets, one source).

### Non-goals (explicitly OUT — deferred to Phase 3)
- **NG1** — Remote crews + a network-reachable comms bus (bus is unix-socket-only; networking it is the P3 heavy lift). *The `CLAUDE_CODE_OAUTH_TOKEN`-cannot-`--remote-control` limitation is exactly why crews are P3 — bead implementers are plain interactive sessions, so this phase is unaffected.*
- **NG2** — Cloud-provisioned sandboxes (E2B / Azure ACI / Morph / Fly / Fargate).
- **NG3** — Elastic / auto-scaling fleets driven by queue depth.
- **NG4** — Distributing the daemon/captain off box A (control plane stays on box A).
- **NG5** — Real microVM isolation (Firecracker / Kata). OCI container + optional gVisor is the ceiling for our own trusted beads.
- **NG6** — Remoting the reviewer (stays on box A per Phase-1 DEC-C — it reads the already-fetched run-branch on box A; no container needed).

### Constraints (HARD)
- **C1 — interactive, never `-p`.** Verified in-tree: launch is `--session-id`/`--resume` (`claudelaunchspec.go:372-374`); the prompt arrives via `tmux load-buffer -` (stdin) + `paste-buffer` + bracketed-paste-aware `send-keys Enter`. The container must host a **real PTY tmux session**. No dispatch path may invoke `claude -p`. (The only `-p` hits in-tree are benign: `ps -o comm= -p`, tmux `display-message -p`, ssh `-p` port flag.)
- **C2 — token auth, no Keychain.** A Linux container has no macOS Keychain and no interactive login. Auth = `CLAUDE_CODE_OAUTH_TOKEN` (1-yr `claude setup-token`, subscription-billed, works for interactive sessions). `ANTHROPIC_API_KEY` MUST be **absent** (it wins over OAuth → silent credit billing) — fail-closed.
- **C3 — macOS docker = a Linux VM.** OrbStack / Docker Desktop / Apple `container` run `linux/arm64` guests in a lightweight VM on Apple Silicon. Images MUST be `linux/arm64` (else slow QEMU). The guest is Linux even though box A is macOS — so the container path and the SSH-Linux path share image/PATH/dep concerns.
- **C4 — reuse the `CommandRunner` seam.** `DockerRunner` implements `tmux.CommandRunner.Command(ctx, name, args...) *exec.Cmd` and MUST forward stdin (`load-buffer -` streams the prompt) — so it produces `docker exec -i <ctr> <name> <args…>`. No `handler.Substrate` change (DEC-A rejected that sibling; the single seam is `tmux.CommandRunner`).
- **C5 — unify SSH + container behind one registry.** A `transport` discriminator selects the runner; selection/slot-tracking/live-disable/health/offline-detect are transport-agnostic.
- **C6 — box A keeps merge authority.** Results exit via `git push origin run/<id>`; box A fetches + merges one-at-a-time under `mergeMu` (DEC-B, UNCHANGED). No distributed merge.
- **C7 — `workers.yaml version:1` caps at one worker** (`ErrTooManyWorkers`, `workers.go:121`). Phase 2 bumps to `version: 2`; the count-guard and the single-worker `Registry` both change.

### Success criteria (concrete, testable)
- **SC1** — With one `transport: docker` worker, a real bead dispatches into a fresh `linux/arm64` container; the daemon `docker exec`s into an **in-container tmux session** and paste-injects the prompt; interactive claude works, runs the build/test gate inside the container, commits `Refs: <bead>`, pushes `run/<id>`, box A merges to main. **Assert via recorded argv: contains `docker exec … tmux new-window … -- claude`, never `-p`/`--print`.**
- **SC2** — During the agent phase the container cannot reach a non-allowlisted host (`curl https://example.com` fails; `git push origin` + `api.anthropic.com` succeed). A bead with `egress: open` (or per-bead allowlist) reaches its declared hosts.
- **SC3** — `CLAUDE_CODE_OAUTH_TOKEN` present in the spawn env (subscription-billed); `ANTHROPIC_API_KEY` **absent** — the `api_key_absent` probe runs **inside the container** via DockerRunner and fails-closed if it leaked; companion `oauth_token_present` probe passes.
- **SC4** — `workers.yaml` holds ≥2 mixed-transport workers (`ssh` + `docker`); the scheduler spreads N concurrent beads honoring per-worker `max_slots` + a global cap; a worker at capacity/unhealthy is skipped; disabling one mid-flight reroutes new beads.
- **SC5** — A Linux SSH worker (`os: linux, transport: ssh`) completes a bead end-to-end with token auth and correct arch handling.
- **SC6** — One provisioning artifact builds the container image AND stands up an SSH worker; re-running converges (idempotent); a fresh image passes the boot health-check including `uname -m == aarch64`.
- **SC7 (regression / NFR7)** — Zero workers configured ⇒ dispatch byte-identical to today (`LocalRunner`). Only-SSH-worker ⇒ Phase-1 behavior unchanged.

---

## 2. DECOMPOSITION (buildable components, one line each)

| # | Component | One-line | Pkg |
|---|---|---|---|
| **D1** | `DockerRunner` | `tmux.CommandRunner` sibling of `SSHRunner`; `Command` → `docker exec -i [Opts…] <ctr> <name> <args…>`; forwards stdin; discrete argv tokens; engine-agnostic via `Bin`. | `internal/lifecycle/tmux` |
| **D2** | `IsTransportFailure(runner, err)` | transport-neutral "substrate vanished" classifier replacing the SSH-specific check at the dispatch site (SSH 255; docker exec 125/126/127). | `internal/lifecycle/tmux` |
| **D3** | Container image + Dockerfile/devcontainer | pinned `linux/arm64`: git, tmux, claude CLI, Go, gh; rc-prompt suppression; non-root `agent` user; baked reference clone at `/repo`. | `deploy/container/` |
| **D4** | `ContainerProvider` lifecycle manager | `Acquire`/`Release`/`orphanSweep`; **ephemeral default**, pooled behind the same interface; starts an in-container tmux session at container-start; container labels for GC. | `internal/container` (new) |
| **D5** | OAuth token injection + key-strip + probes | `AuthMode: token` injects `CLAUDE_CODE_OAUTH_TOKEN` via the spawn env, fail-closed strips `ANTHROPIC_API_KEY`; mirrors `buildCodexEnv`; adds `oauth_token_present` probe + runs `api_key_absent` inside the container. | `internal/daemon` (launchspec) + `internal/workers` |
| **D6** | `workers.yaml` v2 schema | `version: 2`, lift the one-worker cap; add `transport`, `image`, `pool_size`, `arch`, `engine`, `auth_mode`, `oauth_token_file`, `egress` block, `scheduler`, `max_global_slots`. Backward-compatible v1 load. | `internal/workers` |
| **D7** | Multi-worker registry + scheduler | N workers, per-worker slot accounting + global cap; selection strategy (round-robin / **least-loaded** default); live-disable; local fallback (NFR7). | `internal/workers` |
| **D8** | Transport→runner resolver at dispatch | `workloop.go` dispatch site: `SelectWorker()` → `runnerForWorker(w)` → `SSHRunner` \| `DockerRunner` (+ per-container `sessionName` + teardown); everything downstream unchanged. | `internal/daemon` |
| **D9** | Two-phase egress controller | per-bead egress mode (`deny`+allowlist default / `open` / `allowlist:[hosts]`); enforced via egress proxy on box A; phase transition at the paste-inject boundary; **fails LOUD** (`egress_blocked` typed event). | `internal/container` / `internal/egress` |
| **D10** | Linux-SSH worker support | `os: linux` PATH/dep + arch (arm64/x86); token-auth (no Keychain); reuses `SSHRunner` verbatim. | `internal/workers` + runbook |
| **D11** | Container offline/orphan GC | dead-container/exec-fail via `IsTransportFailure` → `worker_offline` → `run_stale`; label-filtered orphan-container sweep on daemon boot (security/billing, not hygiene). | `internal/daemon` + `internal/container` |
| **D12** | Provisioning runbook reuse | one adze manifest → `adze` for SSH workers, `adze render`→Dockerfile `RUN` for the image; idempotent. | `deploy/` + docs |
| **D13** | Scenario e2e (docker-local) | full lifecycle on box A's docker: spawn→paste→commit→push→merge; asserts interactive (no `-p`), **spawn targets the in-container session not box A's `-default`**, token-billed, egress-blocked; `t.Skip` if docker absent. | scenario test |

---

## 3. KEY DESIGN — the local-container substrate (headline)

### 3.1 The runner — `DockerRunner` (D1) — the whole transport change

Direct sibling of `SSHRunner` (`runner.go:84`). New type in `internal/lifecycle/tmux/runner.go`:

```go
// DockerRunner tunnels every command through `docker exec` into a running
// container. Each call produces:
//
//      docker exec -i [Opts...] <Container> <name> <args...>
//
// Like SSHRunner, args are discrete argv tokens (never shell-joined) so the
// container's tmux/git receive exactly one token per argument — quoting-safe.
// -i is MANDATORY: the LoadBuffer prompt-delivery path sets
// cmd.Stdin = bytes.NewReader(payload) (osadapter.go:386) and DockerRunner —
// like SSHRunner — does not touch stdin, so the bytes must flow over -i.
type DockerRunner struct {
    Container string   // container id/name (per-run ephemeral; per-slot for pool)
    Bin       string   // "docker" | "orb" | "container"; default "docker" (engine-agnostic)
    Opts      []string // e.g. ["-u","agent","-w","/repo"] or env -e overrides
}

func (d DockerRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
    bin := d.Bin; if bin == "" { bin = "docker" }
    a := make([]string, 0, 3+len(d.Opts)+len(args))
    a = append(a, "exec", "-i")
    a = append(a, d.Opts...)
    a = append(a, d.Container, name)
    a = append(a, args...)
    return exec.CommandContext(ctx, bin, a...)
}
```

Why this is (almost) the whole substrate change: every consumer already takes a `tmux.CommandRunner` — spawn/tmux via `OSAdapter.effectiveRunner()` (`osadapter.go:41`, injected by `WithRunner`); paste-inject + liveness (`hasAnyDirectChildVia`, `probeLivenessOrSSHFail`, `resolveWorktreeHEADVia`, `pasteinject.go:281-427`); worktree (`CreateWorktree(...).WithRunner(r)`); diff/commit probes (`ComputeDiffHashVia`); code-sync (`fetchBaseOnWorker`/`pushRunBranchOnWorker`, `codesync_rs_b8.go`). Swapping `SSHRunner{Host}` → `DockerRunner{Container}` re-routes all of them — including pasteinject's hard-won quit→grace→kill auto-recovery — with zero changes to that logic. `docker exec` has no SSH handshake cost, so per-probe it's cheaper than the SSH path.

### 3.2 The per-container tmux server — the load-bearing trap (NOT "zero changes")

**This is the decisive correctness fix.** Today the daemon targets ONE pre-frozen `harmonik-<hash>-default` tmux session as the spawn target for all runs (`tmux new-window -t <session>:`), and project memory marks it load-bearing ("NEVER kill a `*-default` spawn session"). When the `tmux` binary runs **inside the container**, there is **no `-default` session there — no tmux server at all** until one is started. So "just swap the runner, everything works unchanged" is **false** for containers.

Two companion changes are mandatory:
1. **D4 starts an in-container tmux server + named session at container-start:** `docker exec <ctr> tmux new-session -d -s harmonik-ctr -c /repo`.
2. **D8 threads that per-container session name into the spawn target.** `remoteBeadCtx` gains a `sessionName` field (empty ⇒ SSH worker's long-lived `-default` session; set ⇒ the container's `harmonik-ctr`). The window-spawn targets `sessionName` instead of hard-coding box A's `-default`.

The daemon then spawns the window via the runner:

```
docker exec -i <ctr> tmux new-window -P -F '#{pane_id}' -d \
  -t harmonik-ctr: -n <win> -c <worktree> \
  -e CLAUDE_CODE_OAUTH_TOKEN=… -e ANTHROPIC_API_KEY= \
  -- claude --session-id <uuid> --dangerously-skip-permissions
```

then paste-injects over the same runner: `docker exec -i <ctr> tmux load-buffer -` (prompt streamed on stdin) → `paste-buffer -t <pane>` → `send-keys Enter`. Fully interactive, never `-p`. **D13 must assert the spawn target is the in-container session and never box A's `-default`.**

### 3.3 Dispatch wiring — the transport switch (D8)

The single behavioral change in the daemon. Today (`workloop.go:2137-2145`):

```go
if w := deps.workerRegistry.SelectWorker(); w != nil {
    rbc = &remoteBeadCtx{ worker: *w, sshRunner: tmuxpkg.SSHRunner{Host: w.Host} }
    defer deps.workerRegistry.ReleaseSlot()
}
```

Becomes a transport resolver (`remoteBeadCtx.sshRunner` renamed to transport-neutral `runner tmuxpkg.CommandRunner`, plus `sessionName` + `container`):

```go
type remoteBeadCtx struct {
    worker      workers.Worker
    runner      tmuxpkg.CommandRunner // SSHRunner OR DockerRunner
    sessionName string                // in-container tmux session ("" ⇒ SSH -default)
    container   string                // container id ("" ⇒ ssh/local); for D11 GC
    teardown    func()                // container rm/pool-return; ssh no-op
}

if lease := deps.workerRegistry.SelectWorker(); lease != nil {
    runner, sess, ctr, teardown, err := runnerForWorker(ctx, *lease, deps.containerProvider)
    if err != nil { notifyWorkerOffline("spawn", …); /* reopen bead */ return }
    rbc = &remoteBeadCtx{worker: *lease, runner: runner, sessionName: sess, container: ctr, teardown: teardown}
    defer teardown()
    defer deps.workerRegistry.ReleaseSlot(lease.Name)
}

func runnerForWorker(ctx context.Context, w workers.Worker, prov container.Provider) (tmuxpkg.CommandRunner, string, string, func(), error) {
    switch w.Transport {
    case "docker":
        h, err := prov.Acquire(ctx, runID, specFor(w)) // ephemeral: create/start; pool: lease warm + reset
        if err != nil { return nil, "", "", nil, err }
        return tmuxpkg.DockerRunner{Container: h.Container, Bin: w.Engine,
            Opts: []string{"-u", w.User, "-w", w.RepoPath}}, h.Session, h.Container, h.Teardown, nil
    case "ssh":
        return tmuxpkg.SSHRunner{Host: w.Host}, "", "", noop, nil
    default: // "" / local
        return nil, "", "", noop, nil // → LocalRunner via effectiveRunner() (NFR7)
    }
}
```

`runnerForWorker` is the ONLY place the world branches on transport. Downstream — code-sync (`:2163,:2185`), worktree (`:2206`), dot (`:2565`), substrate `newPerRunSubstrate(deps.substrate, deps.handlerBinary, runRunner)` (`:2740-2742`), offline detection (`:2167`) — all consume the interface and keep working unchanged. Run metadata stamps `WorkerTransport` + `ContainerID` so D11 cleanup can find the container.

The offline check at `:2167` / `:2187` changes from the SSH-specific `IsSSHConnectionFailure` to `IsTransportFailure(rbc.runner, err)` (D2) — `docker exec` returns 125/126/127 (daemon error / container-not-running / exec-failed), not SSH's 255, so a dead container would otherwise be misclassified as a remote-command failure and never trigger `worker_offline`/`run_stale` recovery.

### 3.4 Worktree / git — inside the container, git is the spine

Same DEC-B flow, `docker exec` instead of `ssh`:
1. Box A resolves `baseSHA` locally and ensures it's on origin (steady-state main push, unchanged).
2. **Container has a baked reference clone at `/repo`** (D3); per bead, incremental `fetchBaseOnWorker(runner, "/repo", baseSHA)` — reuses `codesync_rs_b8.go` verbatim; bulk objects already present (the `--reference`-a-local-mirror optimization).
3. `CreateWorktree(ctx, "/repo", runID, baseSHA, …WithRunner(runner))` → worktree at `/repo/.harmonik/worktrees/<run_id>` **inside the container**, identical call shape to the SSH path.
4. Interactive claude spawns with cwd = that worktree (container-local).
5. **Result out (v1): the container pushes `run/<id>`** — `pushRunBranchOnWorker(runner, wtPath, runID)` → `git push origin run/<id>` from inside the container (git remote is in the agent-phase allowlist). Box A `fetchRunBranchBoxA` + `mergeRunBranchToMain` — UNCHANGED on box A under `mergeMu`.
6. **Teardown:** ephemeral ⇒ `docker rm -f <ctr>` (destroys the worktree); pooled ⇒ `git worktree remove --force` + `git branch -D run/<id>` + `git reset --hard` + `git clean -ffdx` + re-apply default egress before return to pool. Wired as the `teardown` func (D8), joining the existing slot-release + worktree-cleanup defers.

**Rejected — bind-mounting box A's working tree into the container:** defeats isolation (a runaway agent could corrupt box A's repo), reintroduces the merge race, and opens a host-FS write channel. The pushed run-branch is the only thing that crosses the boundary — same trust boundary as a remote Mac. (Plan 2's no-push-credential bind-mount-fetch is the documented **hardening follow-up**, not v1 — the more elegant mechanism must not gate the headline.)

### 3.5 Token-auth injection + key-strip + probes (D5) — the one fundamental env change

Today the env-strip block **zeros `CLAUDE_CODE_OAUTH_TOKEN`** (`claudehandler_chb006_024.go:335`) — correct for a Keychain-logged-in Mac. A container has no Keychain, so the token is the only auth. `LaunchConfig` gains an `AuthMode` field gating the credential block at `:324-341`:

- **`auth_mode: keychain`** (default; Mac SSH + local) → current behavior: zero all credential vars, rely on interactive login.
- **`auth_mode: token`** (container + Linux SSH) → inject `CLAUDE_CODE_OAUTH_TOKEN=<value>` and **still** zero `ANTHROPIC_API_KEY` + `ANTHROPIC_AUTH_TOKEN`, mirroring the proven `buildCodexEnv` strip/reinject pattern (`codexlaunchspec.go:219`).

**Narrowest-blast-radius delivery:** inject via the **spawned session's env** (the launchspec `-e` on the `tmux new-window` / spawn exec), NOT `docker create -e` global env (which puts the token in every `docker inspect` for the container's whole life). The token value is read on box A from a 0600 file (`oauth_token_file` in `workers.yaml`, never the literal), rides the existing `cfg.SecretVars` override-last path (`:345`), and is **never baked into the image, never committed, never in a worktree file**.

**Probes (run inside the container via DockerRunner):** keep the existing `api_key_absent` (`health.go:101-106`, `sh -c 'test -z "$ANTHROPIC_API_KEY"'`) — fail-closed if a key leaked in — and add a companion `oauth_token_present` (`test -n "$CLAUDE_CODE_OAUTH_TOKEN"`). Add a `uname -m == aarch64` arch assertion (cheap; catches the silent QEMU 10×-slowdown of the build/test gate, C3).

### 3.6 Two-phase egress (D9) — must fail LOUD

The credit-burn/exfil inoculation. Per bead, two phases:
- **Setup phase** (container create → just before paste-inject): full internet — `git fetch`, dep install, toolchain warmup.
- **Agent phase** (paste-inject → commit-detect): default-deny + allowlist. **Mandatory allowlist:** `api.anthropic.com` (claude inference) + the git remote (push). Per-bead opt-in: `egress: open` (full network for network-needing tests) or `egress: allowlist:[hosts]`.

**Mechanism (recommend): an allowlisting egress proxy on box A** that the container is forced through (`HTTP(S)_PROXY` env + drop direct routes). This matches prior art (Codex/Claude-web), composes with brokering git auth at the proxy (the hardening path where no push credential need enter the container), and is per-bead-configurable by editing that run's allowlist. The phase transition is a `ContainerProvider` hook at the paste-inject boundary (the moment prompt-injection-exfil risk begins); pool reset restores full egress for the next bead's setup.

**Fail LOUD (mandatory):** a blocked dial to `api.anthropic.com` is indistinguishable from a hung agent — the run would burn the full 30-min commit budget then `no_commit`. The egress layer MUST emit a typed `egress_blocked` event on a denied allowlist-critical dial, not silently hang.

### 3.7 Ephemeral vs pool (D4) — ephemeral is the v1 default

Both modes behind one `ContainerProvider` interface; `pool_size: 0 ⇒ ephemeral`:

```go
// internal/container
type Provider interface {
    Acquire(ctx context.Context, runID string, spec Spec) (*Handle, error)
    // Handle{ Container, Session string; Teardown func() }
    OrphanSweep(ctx context.Context) error // label-filtered docker ps; reap on daemon boot
}
```

- **Ephemeral (`pool_size: 0`, DEFAULT):** `docker create/start` per bead, `docker rm -f` on teardown. Strongest isolation — fresh FS every bead, which is the whole point of G1. Spin-up on OrbStack arm64 is sub-second-to-a-few-seconds; the incremental `git fetch` is the only real added latency.
- **Pool (`pool_size: N`):** N pre-started warm containers leased per bead; on release, reset (`git worktree remove` + `reset --hard` + `clean -ffdx` + re-apply egress) and return. A latency optimization to add **only after** cold-start proves to hurt throughput. The reset-correctness burden + cross-bead state-bleed risk is why it is NOT the default.

Containers are labeled `harmonik.run=<run_id>` / `harmonik.pool=<name>`; `OrphanSweep` runs on daemon boot — a crashed daemon leaks a running container **holding the OAuth token in its env**, so this is a security/billing concern, not hygiene (sibling of the existing tmux orphansession reap).

### 3.8 Go types/functions that change (concrete inventory)

| Where | Change |
|---|---|
| `internal/lifecycle/tmux/runner.go` | **+ `DockerRunner`** (sibling of `SSHRunner`, `-i` + stdin pass-through + `Bin`); **+ `IsTransportFailure(r, err)`** (D2; SSH 255 / docker 125/126/127). |
| `internal/workers/workers.go` | `Worker` gains `Transport, Image, PoolSize, Arch, Engine, AuthMode, OAuthTokenFile, Egress`; loader → `version: 2`, lift `ErrTooManyWorkers`; v1 backward-compat. |
| `internal/workers/registry.go` | `Registry` → N-worker + global cap + `SchedulingStrategy` (least-loaded default); `SelectWorker() *Lease` / `ReleaseSlot(name)`. |
| `internal/workers/health.go` | run the probe set **via DockerRunner** for docker workers; + `oauth_token_present` + `uname -m == aarch64`; `os: linux`/`arch` awareness. |
| **new `internal/container/`** | `Provider` (`Acquire`/`Release`/`OrphanSweep`); ephemeral + pool; in-container tmux session start; container labels. |
| **new `internal/egress/`** (or in `internal/container`) | two-phase egress proxy + allowlist + phase-transition hook + `egress_blocked` typed event. |
| `internal/daemon/workloop.go` | `:2132-2145` — `runnerForWorker` resolver; `remoteBeadCtx{runner, sessionName, container, teardown}`; `:2167/:2187` → `IsTransportFailure`; spawn targets `sessionName`. |
| `internal/daemon/claudelaunchspec.go` (+ env builder `:324-345`) | `AuthMode`-gated env: `token` mode injects `CLAUDE_CODE_OAUTH_TOKEN` via spawn env, still zeroes `ANTHROPIC_API_KEY`; mirrors `buildCodexEnv`. |
| `internal/daemon/codesync_rs_b8.go` | **unchanged signatures** — already `tmux.CommandRunner`-parametrized; just receives a `DockerRunner`. |
| **bead schema / dispatch** | per-bead `egress` field (default from worker `egress` block) plumbed to the egress controller. |
| `deploy/container/` | Dockerfile/devcontainer (`linux/arm64`) + the shared adze provisioning source (D12). |
| **scenario test** | `internal/.../scenario_*_test.go` (`//go:build scenario`) — D13. |

---

## 4. MULTI-WORKER SCHEDULER + LINUX-SSH + PROVISIONING (briefly)

### Multi-worker scheduler (D6/D7)
`workers.yaml` lifts to `version: 2`, drops the `ErrTooManyWorkers` guard. The single-`worker`/single-`inFlight` `Registry` generalizes to N entries with per-worker slot accounting under a `max_global_slots` cap (the disk/CPU knee — ~4–5 on box A). `SelectWorker() *Lease` honors a `scheduler` strategy (round-robin / **least-loaded** default — `inFlight` is already tracked / capacity-aware later) over enabled, sub-`max_slots` workers; `nil` ⇒ local fallback (NFR7). Live-disable skips a worker for new beads. Example:

```yaml
version: 2
scheduler: least-loaded
max_global_slots: 5
workers:
  - name: local-containers
    transport: docker
    engine: orb
    image: harmonik-worker:arm64
    pool_size: 0                 # 0 = ephemeral (v1 default)
    arch: linux/arm64
    auth_mode: token
    oauth_token_file: ~/.harmonik/secrets/claude-oauth-token
    egress: { agent_phase: deny, allow: [github.com, api.anthropic.com] }
    max_slots: 3
  - name: worker-mac-1           # Phase-1 SSH worker, unchanged
    transport: ssh
    host: worker-mac-1
    os: darwin
    auth_mode: keychain
    max_slots: 4
  - name: linux-vps              # D10 Linux SSH worker
    transport: ssh
    host: linux-vps
    os: linux
    arch: linux/x86_64
    auth_mode: token
    oauth_token_file: ~/.harmonik/secrets/claude-oauth-token
    max_slots: 6
```

### Linux-SSH worker (D10)
Reuses `SSHRunner` verbatim (already OS-agnostic). Deltas only: `os: linux` PATH/dep handling; `arch` (arm64/x86) in the health-check + provisioning; `auth_mode: token` token-auth (a bare Linux VPS has no Keychain) — same `CLAUDE_CODE_OAUTH_TOKEN` injection + `ANTHROPIC_API_KEY` strip as the container path. Largely a config + auth-delivery change atop Phase 1.

### Provisioning runbook reuse (D12)
Author once in an `adze` manifest (desired state: git, tmux, claude, go, gh + token setup). `adze` converges a persistent SSH worker (drift-capture for a pet box); `adze render` emits bash folded into the container image's Dockerfile `RUN`s. Two render targets, one source (G6); D13 validates the image builds + runs from it.

---

## 5. OPEN DECISIONS + BUILD SEQUENCE

### Open decisions
- **OD1 — docker engine on box A:** OrbStack (fast, free, arm64-native, mature per-container networking — **lean OrbStack for v1**) vs Apple `container` (macOS 26 native, least proven) vs Docker Desktop (skip — 4GB idle, paid). `Bin` is configurable so `container` is a later swap. *Operator-shaping — surface; confirm the mini's engine before D4.*
- **OD2 — egress enforcement mechanism:** allowlisting egress proxy on box A (**lean — portable, brokers git secret outside the sandbox, per-bead allowlist edit**) vs in-container iptables/nftables (needs `CAP_NET_ADMIN`). *Surface — security-shaping.*
- **OD3 — result-out mechanism (the one real design fork):** container pushes `run/<id>` (**recommend for v1** — reuses `pushRunBranchOnWorker` verbatim, git remote already allowlisted) vs Plan 2's no-push-credential bind-mount-fetch (tightest security, composes with the proxy, but couples container FS to box A and is the less-proven mechanism — documented **hardening follow-up**, must not gate the headline). *Agent-owned: push for v1.*
- **OD4 — ephemeral vs pool default:** **ephemeral** behind the `Provider` interface; pool opt-in after measuring cold-start on real hardware. *Agent-owned: ephemeral, note revisit.*
- **OD5 — scheduler strategy default:** **least-loaded** (the `inFlight` counter already exists); round-robin trivial fallback; capacity-aware needs a per-worker CPU signal we don't have. *Agent-owned.*
- **OD6 — OAuth token delivery:** spawned-session env (**recommend — narrowest blast radius**) vs `docker create -e` global env. *Agent-owned: session env.*

### BUILD SEQUENCE (throughput-maximizing; what gates what)

```
TRACK 1 (FIRST — pure box-A Go, no docker dep): D6 → D7 → D10
   workers.yaml v2 + N-worker registry/scheduler + Linux-SSH worker.
   Delivers G4 + G5; de-risks the ErrTooManyWorkers-lift + registry-v2 generalization
   independently of any docker work; reuses SSHRunner verbatim.

TRACK 2 (container chain — serial behind merges on the hot core files):
   D1 DockerRunner ──► D5 token/strip/probes ──► D3 image ──►
   D4 ContainerProvider (ephemeral + in-container tmux session) ──►
   D8 dispatch resolver (+ sessionName + D2 IsTransportFailure) ──►
   [working OPEN-egress container run — proves SC1 + SC3]

TRACK 3 (after a proven spawn): D9 two-phase egress  [proves SC2]
                                D11 offline/orphan GC

PARALLEL doc/ops track: D12 provisioning runbook (folds D3's image as a render target)

LAST: D13 scenario e2e (authored via a worktree sub-agent; daemon gate skips //go:build scenario)
```

**Recommended order, with rationale:**
1. **Track 1 first** (D6→D7→D10). Pure box-A Go, no docker dependency on the critical path, delivers two goals immediately, and de-risks the registry-v2 lift before any container complexity. This is the throughput-maximizing start (Plan 2's instinct, endorsed by the critic).
2. **The container critical chain** (D1→D5→D3→D4→D8) to a **working open-egress container run** — proves SC1 (interactive, no `-p`, in-container tmux session, commit→push→merge) + SC3 (token-billed, key-absent) on real hardware **before** investing in egress. **D1 lands first** (everything builds on the runner). The earliest de-risking gate: a one-off manual run proving interactive claude auths via `CLAUDE_CODE_OAUTH_TOKEN` inside an OrbStack Linux container with an in-container tmux session — if that fails, the whole headline needs rework, so prove it with the least code.
3. **Two-phase egress** (D9) — the headline's *value* (SC2), but it gates on a working spawn so it lands after. **Egress must fail loud** from day one.
4. **Hardening + proof** (D11 orphan/offline GC, D13 scenario e2e). D12 rides alongside as a doc/ops track.

**Same-file serialization (project memory — mandatory):** D1, D2, D8, D11 all touch `runner.go` + `workloop.go` (the hot core). Dispatch those **serially, waiting for each merge**, to avoid same-file conflict auto-skips. `internal/container/`, `internal/egress/`, `internal/workers/`, and `deploy/container/` are separate packages and run concurrently.

**Critical correctness gates carried into SPEC (the critic's required additions):** (1) per-container tmux session is NOT free — start it at container-start + thread `sessionName`, assert in D13; (2) `IsTransportFailure` is mandatory (docker exit codes ≠ 255); (3) egress fails LOUD via `egress_blocked`, mandatory allowlist = `api.anthropic.com` + git remote; (4) orphan-container GC reaps on boot (token-in-env security concern); (5) ephemeral default behind `Provider`; (6) token via spawned-session env + `oauth_token_present` probe + `api_key_absent` inside the container; (7) `uname -m == aarch64` assert; (8) Track-1-first sequencing; (9) same-file serialization; (10) result-out = push for v1, bind-mount-fetch documented as hardening.

---

**Translations glossary:** box A = gb-mac-mini (the daemon host) · `CommandRunner` = the `tmux.CommandRunner` interface every shell-out funnels through (the real Phase-1 seam, `runner.go:15`) · `SSHRunner`/`DockerRunner`/`LocalRunner` = its three impls (`ssh host --` / `docker exec -i ctr` / direct `exec`) · paste-inject = the `tmux load-buffer`+`paste-buffer`+`send-keys Enter` prompt-delivery path (interactive, NOT `claude -p`) · `claude -p` = headless one-shot mode (forbidden) · `CLAUDE_CODE_OAUTH_TOKEN` = the 1-yr subscription-billed token for container/headless interactive auth (no Keychain) · `-default` session = the single pre-frozen box-A tmux spawn target (load-bearing; not present inside a container) · `IsTransportFailure` = the transport-neutral "substrate vanished" classifier (SSH 255 / docker 125/126/127) · DEC-A = Phase-1's "single `CommandRunner` seam, no `handler.Substrate` sibling" decision · DEC-B = "box A keeps merge authority, worker pushes a run-branch" · NFR7 = "zero workers → behavior identical to today" · ephemeral vs pool = throwaway-per-bead vs warm reset-between-beads containers · `run/<id>` = the per-bead branch box A merges.