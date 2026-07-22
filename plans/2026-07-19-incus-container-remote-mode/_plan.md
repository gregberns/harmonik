# Incus Container Remote Mode — Planning Doc

**Date:** 2026-07-19
**Status:** planning scaffold (not a spec, not too deep — a grounded frame for a decision)
**Author:** investigation + design pass

## Frame

The operator wants to offload harmonik's build/support load onto **on-demand, isolated,
disposable containers** instead of a single always-on remote Mac. The new topology is a
**two-hop chain**: macOS host `gb-mbp` runs the harmonik daemon → a **lima** VM named
`fleet` (Ubuntu) → **incus** containers launched inside `fleet` as ephemeral workers, e.g.
`limactl shell fleet -- sudo incus launch agent-golden agent-03 --profile default --profile agent`.
This differs structurally from harmonik's current remote model, which assumes **one static
SSH worker host** reachable directly over Tailscale. The question this doc answers: does the
container model warrant a **new remote mode**, and if so, what are the load-bearing design
decisions and risks — grounded in the code that already exists so the design is not
hand-waved. The riskiest surface is the event relay back through two network hops; the same
class of bug (`hk-ege6` socket-perm / `hk-go6nq` `agent_ready_timeout`) will reappear here,
so that path is called out explicitly.

---

## Current remote architecture (grounded)

Harmonik today has a **single-static-SSH-worker** remote substrate. The pieces:

### Worker registry — declaration, selection, the version-1 single-worker cap
- Config file: `.harmonik/workers.yaml`, loaded by `internal/workers/workers.go`
  (`configRelPath`, `internal/workers/workers.go:32`). Missing file ⇒ zero Config, nil error
  ⇒ "local execution only."
- **Version-1 invariant: at most ONE worker** (`currentVersion = 1`,
  `internal/workers/workers.go:34`; `ErrTooManyWorkers` when `len(workers) > 1`,
  `internal/workers/workers.go:239,283`). This is the single most important constraint the
  container model collides with — a pool of N containers is N workers.
- The `Worker` struct (`internal/workers/workers.go:37`) carries `Name`, `Transport`
  (`"ssh"`), `Host`, `OS`, `RepoPath`, `MaxSlots`, `Enabled`, `HarmonikPath` (worker-side
  binary path for the hook relay, `hk-z8ek`). `DefaultHarmonikPath` is
  `/Users/gb/go/bin/harmonik` (`internal/workers/workers.go:62`).
- Selection: `internal/workers/registry.go` — `PrimaryWorkerIndex` always returns 0 or -1
  (registry.go:19), and `SelectWorker` reserves one of `MaxSlots` slots, re-reading `Enabled`
  every call for live-disable (FR12). `NewRegistry` with no workers ⇒ `SelectWorker` always
  returns nil ⇒ local path.
- The registry is **boot-only** ("remote_worker_registry_startup_only"): live
  `worker enable/disable` mutates in-memory state, but the slot count / host set is fixed at
  daemon boot. (Seen all over the `.harmonik/workers.yaml` operator history.)
- The live worker is `gb-mbp` (spare Mac), reached at a Tailscale IP, `enabled: false` as of
  the 2026-07-12 incident. See `.harmonik/workers.yaml`.

### The fail-closed isolation boundary (hk-5h759)
- `cmd/harmonik/substrate_select.go` — `HARMONIK_SUBSTRATE=codexdriver`
  (`substrateSelectEnv`, substrate_select.go:29) opts into the structured Codex app-server
  driver, which runs `sandbox_mode=danger-full-access` + `approval_policy=never`. That
  posture is safe **only inside a real isolation boundary** — an enabled ssh worker IS the
  boundary.
- `selectSubstrate` (substrate_select.go:70) returns `requireIsolationBoundary=true` on the
  codex path. `codexWorkerRoutingRunner.requireBoundary` (substrate_select.go:111) makes the
  spawn seam **fail closed**: at `Command()` time, with no enabled ssh worker bound, it
  refuses rather than falling through to `LocalRunner` (which would run codex UNSANDBOXED on
  the daemon host). The refusal is materialized as a deliberately non-existent argv0
  (`refusedIsolationBoundaryArgv0`, substrate_select.go:119) so `exec.Start` fails fast with a
  diagnostic path. This is a **race-free, spawn-time** check (closes the admission→spawn TOCTOU).
- Empirically proven by `hk-g0ror.4`: codexdriver + no enabled worker → `run_failed` with the
  guard message, zero `run_started`, zero LOCAL exec.

### Reaching the worker + creating a worktree there (hk-czb11 / hk-fufel)
- `SSHRunner` (`internal/lifecycle/tmux/runner.go:92`) tunnels every command as
  `ssh [opts] <host> -- <shell-quoted cmd>` (`Command`, runner.go:111).
- Remote cwd is the subtle part: a remote transport runs the child ON the worker, so the run's
  worktree path (`Cwd` / `WorkDir`) is a **remote** path. Setting it as the LOCAL `exec.Cmd.Dir`
  fork/exec-ENOENTs the local `ssh` process. The fix is `CommandInDir` (runner.go:139), which
  emits `ssh host -- cd <dir> && exec <cmd>` and leaves the local `cmd.Dir` unset.
  - `hk-czb11` fixed this in the codexdriver app-server seam
    (`internal/codexdriver/driver.go:73` `RemoteCwdRunner`, applied at driver.go:220).
  - `hk-fufel` fixed the same bug in the handler **direct-exec** path — the path per-bead codex
    actually takes (`internal/handler/handler.go:169` `RemoteCwdRunner`, applied at
    handler.go:291). Both landed on main 2026-07-19 (PR#32 / PR#33).
- Remote worktree/branch flow: the daemon does fetch-base → remote worktree-add → run →
  push run branch → box-A fetches the run branch directly over SSH (no worker→GitHub push,
  `hk-7bwx`). The DD1 code-sync path is threaded through
  `internal/daemon/workloop.go:740` (`workerRegistry`, `hk-rs-b8-codesync-3fk0`). Concurrent
  remote `git worktree add` has been a chronic empty-HEAD race (`hk-2hfyt` / `hk-5qp7z`),
  serialized via a create-mutex (`worktreeCreateMu`, workloop.go:412).

### The event tunnel back to the daemon (hookrelay + ssh -R, hk-ege6)
- The hook relay (`internal/hookrelay/hookrelay.go`) is a short-lived subprocess Claude
  invokes via command-type hooks; it ships one NDJSON message to the daemon's socket.
- For remote runs the daemon stands up a **separate, long-lived `ssh -N -R` reverse tunnel**
  per run (`internal/daemon/reversetunnel.go`). It cannot ride the detached spawn ssh (that
  returns immediately), so it is its own process keyed to the run's lifetime.
- **hk-ege6 (the load-bearing lesson):** the tunnel originally bound a **UNIX socket** on the
  worker via `-R <sock>:<daemonSock>`. macOS sshd runs as **root**, so the `-R` StreamLocal
  bind created that socket **root-owned, mode 0600** → Claude's unprivileged hook subprocess
  got `connect: permission denied` → `agent_ready` never relayed → `agent_ready_timeout` @90s
  → `run_failed`. `handler_capabilities` traversed fine, masking the failure. Client-side
  `StreamLocalBindMask` is IGNORED for `-R` binds. **Fix:** switch to a **TCP loopback**
  listener — `ssh -R 127.0.0.1:<port>:<daemonSock>` (`buildReverseTunnelArgs`,
  reversetunnel.go:232), which has no filesystem permission bits. The hookrelay dialer keys off
  the `tcp://` prefix (`hookEndpointTCPPrefix`, hookrelay.go:503; `resolveDialTarget`,
  hookrelay.go:509) to `net.Dial("tcp", …)`. The readiness gate was hardened from `test -S`
  (existence, false-green) to a real connect probe (`waitWorkerSocketLive`, reversetunnel.go:329).
- Concurrency hardening in the same file: per-run ephemeral port allocation with an in-process
  reserved-port set (`allocateReverseTunnelPort`, reversetunnel.go:153) and a **dedicated,
  non-multiplexed** ssh connection per tunnel (`ControlMaster=no` / `ControlPath=none`,
  `hk-cnp17`) so per-run `-R` forwards don't collapse onto a shared master.
- **hk-go6nq (OPEN, regression-suspect):** a review-node `agent_ready_timeout` with the SAME
  symptom as hk-ege6, surfaced in the codex crit3 E2E and an hk-8ut5j smoke. Root-cause lead
  (admiral): check the tunnel/socket-perm path FIRST — may be a regression or a claude-review-node
  sibling. The operator wants this kept moving **because remote load is coming** (this plan).

### Provisioning CLI surface (today)
- There is **no** `harmonik worker provision`. The worker CLI surface is limited: boot-time
  override wiring in `cmd/harmonik/workers_boot.go` (`applyWorkerOverrides` — `--worker-host` /
  `--worker-enabled` flags) and live `harmonik worker enable/disable` (in-memory only).
  Provisioning is a **manual runbook** today: `docs/remote-substrate/WORKER-SETUP-macos.md`
  (install toolchain, join tailnet, clone repo, log Claude into the subscription by hand).

---

## Proposed incus mode

### 1. Where this fits: new mode vs registry extension
**Recommendation: a NEW remote mode ("incus"/"container" transport), NOT a stretch of the
version-1 SSH registry.** Reasons:
- The version-1 registry hard-caps at ONE worker (`ErrTooManyWorkers`) and is boot-only. A
  container pool is inherently multi-worker and dynamic (launch/destroy mid-life). Bending the
  single-static-host model to a dynamic pool fights every invariant it was built on.
- BUT reuse the seams, don't rewrite them: the `Transport` field already exists on `Worker`;
  add `transport: "incus"`. Reuse `SSHRunner`/`RemoteCwdRunner` (or an `IncusRunner` sibling
  that satisfies the same `CommandInDir` shape) so the remote-cwd fix (hk-czb11/hk-fufel) and
  the fail-closed guard (hk-5h759) apply unchanged. The addressing + relay + provisioning are
  new; the spawn seam contract is not.
- Net: **new mode = a new transport + a new lifecycle (provisioning/pool) layer + a
  two-hop addressing/relay adapter**, composed over the existing registry/runner/guard seams.
  This likely requires a **workers.yaml version 2** (lift the one-worker cap, add a pool/profile
  block).

### 2. Provisioning lifecycle
Who launches/destroys containers — three options:
- **Per-run ephemeral (launch→run→destroy):** cleanest isolation, matches "disposable," and
  incus launch is fast. A container never outlives one bead run, so credential/state leakage
  and empty-HEAD-style cross-run races largely vanish. Cost: per-run launch + repo-materialize
  latency on the critical path (mitigated by a golden image, §5); more churn on the two-hop
  control plane.
- **Warm pool (pool manager keeps K containers ready):** amortizes launch/repo-clone latency,
  higher throughput. Cost: containers accrue state between runs (the very thing the operator
  wants to avoid), pool-manager complexity, and reconciliation of a leaked/wedged container.
- **Hybrid:** warm pool of golden containers, but **reset-or-recycle** each container after a
  run (destroy + relaunch, or `incus restore` from a snapshot). Gets pool latency with
  per-run cleanliness.

**Recommendation:** start **per-run ephemeral** for the prototype (simplest, matches the
disposable goal, exercises the launch/destroy path we must get right anyway), then add a warm
pool only if launch+materialize latency proves to be the bottleneck. **Who owns it:** a new
**pool/provision manager** inside the daemon (not the per-run work loop, which must stay
fast), fronted by a `harmonik worker provision` / `harmonik fleet …` CLI surface for
operator-driven manual launch and for building/refreshing the golden image. The daemon
manager launches/destroys; the CLI is for humans and for the golden-image build.

### 3. Addressing across the two hops
The daemon on macOS must reach a command inside `agent-03`, which lives inside `fleet`. Two
approaches:
- **(A) Nested exec:** `limactl shell fleet -- sudo incus exec agent-03 -- <cmd>`. No
  network/IP setup, works the moment the container exists, mirrors exactly what the operator
  typed. Risks: it is a **command adapter, not an ssh transport** — the current spawn seam is
  ssh-shaped (`SSHRunner` builds `ssh host -- …`). We'd need an `IncusRunner` that builds the
  `limactl shell … incus exec …` argv and still implements `CommandInDir` (`cd <dir> && exec …`).
  stdin/stdout/tty semantics through `limactl shell` + `incus exec` need verification (the tmux
  substrate and hook relay assume clean pipes). No stable "host:port" to reverse-tunnel to
  (see §4 — this is the crux).
- **(B) Direct SSH to the container's bridge IP:** run sshd in each container; the container
  gets an incus-bridge IP reachable **from inside `fleet`**. Reuses `SSHRunner` and the entire
  existing reverse-tunnel machinery almost verbatim. Risk: the bridge IP is **not reachable
  from macOS** without a hop — need either (b1) a lima port-forward / a jump-host through
  `fleet` (`ssh -J fleet-user@fleet-ip container-ip`, i.e. `SSHRunner.Opts = ["-J", …]`), or
  (b2) an incus proxy device / lima `portForwards` mapping container:22 → a `fleet` port →
  macOS. Per-container dynamic port mapping adds a moving part per launch.

**Recommendation:** **(B) with a ProxyJump through `fleet`** — `ssh -J <fleet> <container-ip>`
via `SSHRunner.Opts`. It reuses the ssh transport, `CommandInDir`, and the reverse-tunnel path
with the least new code, and treats the container as "just another ssh worker two hops away."
Fall back to (A) nested-exec only if standing sshd + a jump path inside each ephemeral
container proves heavier than the incus-exec adapter. **Risk to note:** ProxyJump doubles the
ssh handshake surface and the reverse tunnel now has to survive the jump (see §4).

### 4. Event relay back through the hops (the riskiest part — connect to hk-ege6 / hk-go6nq)
> **SCOPE CORRECTION (2026-07-19 operator pass): this whole section is a CLAUDE-path risk, not a
> universal one.** The reverse-tunnel / hook-relay machinery is load-bearing ONLY for the tmux/Claude
> substrate. The **codex app-server "sidecar" carries its events back IN-BAND over the ssh stdout
> pipe** and never touches the reverse tunnel (see the 2026-07-19 section, "Codex sidecar"). The
> workloop still *builds* the tunnel + runs `waitWorkerSocketLive` for every remote run regardless of
> substrate (workloop.go:3639,3722), so for a codex run it is dead weight that can still fail the run
> on setup it never uses — flagged there as a simplification.

The hook relay inside `agent-03` must reach the daemon's socket on macOS, **two network hops
away**. This is the hk-ege6 failure surface, amplified:
- The reverse tunnel today is `ssh -N -R 127.0.0.1:<port>:<daemonSock>` from the worker back
  to box A (reversetunnel.go:232). With a ProxyJump, the `-R` forward must terminate on macOS
  through `fleet`. Two sub-questions that MUST be answered empirically before trusting it:
  (i) does the container's hook subprocess connect to the loopback listener the tunnel binds
  **inside the container** (owner/mode — the exact hk-ege6 failure: is incus/sshd binding it
  root-owned? Linux sshd is typically not root the way macOS sshd is, but this MUST be
  verified, and it is the #1 thing hk-go6nq's root-causer should check); (ii) does the
  loopback listener bind on the **container's** loopback or on `fleet`'s — with a jump, the
  `-R` bind lands on the ssh **server** end (the container), which is what we want, but the
  jump host in between must not swallow it.
- **Keep the TCP-loopback invariant (hk-ege6): never a UNIX socket for the `-R` bind across
  the hops.** TCP loopback has no filesystem perm bits and dodges the root-owned-0600 trap
  regardless of who sshd runs as. `hookEndpointTCPPrefix` / `resolveDialTarget` already handle
  the `tcp://` env value; reuse unchanged.
- **hk-go6nq is the canary.** The operator wants it kept moving precisely because container
  load will hammer this path. Its root-cause (tunnel/socket-perm vs review-node readiness)
  should be resolved **before** the container mode's relay is trusted — otherwise we'll
  rediscover it per-container. This plan should keep hk-go6nq unblocked and treat its fix as a
  gate for the addressing+relay phase.
- Alternative worth prototyping if `-R`-through-a-jump is flaky: an **incus proxy device**
  mapping a container port → `fleet` → a macOS-reachable endpoint for the daemon socket
  (forward direction), inverting the tunnel. Decide by measurement, not theory.

### 5. The golden image (hk-u6qu6)
- Build a **harmonik-specific golden container image** (`agent-golden` today has pi/claude/codex;
  extend it): pi + claude + codex + the **harmonik worker binary at a known path** (so the hook
  relay fires — the `HarmonikPath` / `DefaultHarmonikPath` contract, workers.go:62), plus a
  pre-warmed Go build cache (cold gates have historically blown the commit_gate timeout on the
  Mac worker) and a pre-cloned repo.
- **Provisioning the pi runtime files (hk-u6qu6):** `pilaunchspec.go` writes `models.json` +
  `.harmonik/pi-agent/` locally and injects `PI_CODING_AGENT_DIR` — on a remote run that dir
  exists only on box A. For a container this MUST be staged into the container (bake into the
  golden image, or push at launch via the DD1 code-sync path which may exclude `.harmonik/`).
  This is the container analogue of hk-u6qu6 and blocks any remote-pi run.
- **Build/version:** golden image built by a `harmonik fleet build-image` (or a documented
  runbook, successor to `WORKER-SETUP-macos.md` but for Ubuntu/incus). Version the image
  (tag / snapshot name) and **pin the harmonik binary SHA** into it so a stale image can't run
  work against a moved contract — but per operator preference, do NOT bind external tool
  versions (codex/claude/model) into a hard pin; degrade gracefully.

### 6. Fail-closed posture (preserve hk-5h759)
- The isolation-boundary guard must treat "an enabled container transport bound" as the
  boundary, exactly as it treats an enabled ssh worker today. **No container bound ⇒ codex
  spawn REFUSES**, never a local-host fallback — reuse `codexWorkerRoutingRunner.requireBoundary`
  / `refusedIsolationBoundaryArgv0` (substrate_select.go:111,119) unchanged; only the predicate
  "is a boundary available?" learns about the container transport.
- **New requirement: a launched-but-unreachable container must ALSO fail closed.** A container
  that launched but whose ssh/exec path or reverse tunnel never came up must NOT degrade to
  local exec. The spawn-time check must verify **reachability of the specific container**, not
  merely "a container transport is configured" — the same race-free, at-spawn-time discipline
  hk-5h759 uses, extended to "the boundary is not just declared but live." The
  `waitWorkerSocketLive` connect-probe gate (reversetunnel.go:329) is the model: a
  non-connectable container endpoint fails the run rather than launching the agent into a dead
  tunnel or a local fallback.

### 7. Auth (the overnight OAuth wall — hk-bkd6h)
- Containers need claude/codex credentials. **hk-bkd6h:** a freshly-spawned Claude session does
  NOT inherit the operator's OAuth — it boots to an interactive `claude.com/oauth/authorize`
  wall and never joins comms. For **ephemeral containers this is fatal**: every launch is a
  fresh session, so every container would hit the login wall. The macOS worker sidestepped it
  by a **human-once** `/login` to the subscription (`WORKER-SETUP-macos.md` Part 1) that
  persists on that box — an ephemeral container has no persisted login.
- Options: (a) bake a **long-lived OAuth token** (`CLAUDE_CODE_OAUTH_TOKEN` in the worker
  env — the existing macOS pattern via `~/.zshenv`) into the golden image or inject it at
  launch (secret-management question: it's a credential, per credential-isolation.md it must be
  scoped-injected, not baked into a shared image); (b) mount a persisted credential volume
  read-only into each container; (c) **prefer Codex** for implementer crews — the Codex
  subscription/app-server path may sidestep the interactive OAuth wall entirely (aligns with
  the standing "prefer Codex for implementation, Claude for oversight" preference). **Flag:**
  which of claude-token-injection vs codex-only is viable overnight is an **open, load-bearing
  question** — it gates unattended container operation and must be answered before the E2E gate.

---

## Key open questions
1. **Reverse tunnel through the jump:** does `ssh -N -R 127.0.0.1:<port>:<daemonSock>` survive a
   ProxyJump through `fleet` and bind a container-user-CONNECTABLE loopback listener inside the
   container? (Direct descendant of hk-ege6; the whole mode's relay hinges on it.)
2. **hk-go6nq:** is the review-node `agent_ready_timeout` a tunnel/socket-perm regression or a
   claude-review-node readiness gap? Must be resolved before trusting the container relay.
3. **workers.yaml v2:** lift the one-worker cap and add a pool/profile schema — or model the
   pool entirely outside the registry (registry stays "one logical container-transport worker,"
   pool manager owns the fleet)?
4. **Ephemeral vs pool:** is per-run launch+repo-materialize latency acceptable, or is a warm
   pool required from day one? (Measure before deciding.)
5. **Auth overnight:** claude-token injection (secret handling) vs codex-only for unattended
   container crews (hk-bkd6h). Which is actually viable headless?
6. **Addressing:** ProxyJump-SSH (reuse everything) vs incus-exec adapter (new runner, tty/pipe
   semantics unverified). Prototype both minimally, decide by measurement.
7. **Concurrency:** the reverse-tunnel port allocation + dedicated-connection hardening
   (hk-cnp17) was tuned for one host with N slots; does it hold for N distinct containers each
   with its own tunnel and jump?

---

## Rough phases
1. **Investigation / spike (GO-NO/GO):** stand up one incus container in `fleet` by hand; from
   macOS, prove (a) a command runs inside it via ProxyJump-ssh AND/OR nested incus-exec, and
   (b) a reverse tunnel from the container reaches a listener on macOS through `fleet`. Answer
   open questions 1 and 6. Nothing wired into the daemon yet.
2. **Prototype provisioning:** a `harmonik worker provision` / pool-manager launch→destroy path
   (per-run ephemeral first). Golden-image built by hand for now.
3. **Addressing + relay:** wire the chosen transport (IncusRunner or ProxyJump-SSHRunner) into
   the registry/runner seam; make the reverse tunnel work end-to-end through both hops;
   **gate on hk-go6nq being root-caused.** Reuse `CommandInDir` + `resolveDialTarget` unchanged.
4. **Golden image:** harmonik-specific image (pi/claude/codex + worker binary + warm Go cache +
   pi runtime files per hk-u6qu6); a build/version runbook (Ubuntu/incus successor to
   `WORKER-SETUP-macos.md`).
5. **Fail-closed + auth:** extend the hk-5h759 guard so an unbound OR unreachable container
   refuses (never local fallback); resolve the hk-bkd6h auth path (codex-only or scoped token
   injection) so unattended launch works.
6. **E2E gate:** an isolated end-to-end proof — a codex crew runs sandboxed inside an ephemeral
   container, its commit lands inside the boundary, the run reaches `agent_ready` and completes
   through the review node, and the container is destroyed clean. Mirror the hk-g0ror.4 rigor
   (stamped binary, isolated daemon/socket/origin, prod untouched).

---

## Relationship to existing beads / initiatives
- **hk-g0ror** (Codex-as-Crew Phase-2, OPEN P1): the container mode is the natural isolation
  boundary for the danger-full-access codex posture this epic productizes. Its acceptance gate
  (hk-g0ror.4) is the template for phase-6's E2E gate.
- **hk-go6nq** (review-node `agent_ready_timeout`, OPEN P2): the canary for the two-hop relay;
  keep it moving — remote/container load is exactly what makes it load-bearing. Gates phase 3.
- **hk-5h759** (fail-closed isolation guard, CLOSED): the invariant to preserve and extend — no
  boundary (or an unreachable one) ⇒ refuse, never local fallback.
- **hk-u6qu6** (remote pi: materialize models.json + `.harmonik/pi-agent` on the worker, OPEN
  P1): the golden-image / launch-staging requirement for remote pi; same problem, container
  flavor.
- **hk-bkd6h** (fresh session OAuth wall, OPEN P2): the auth blocker for ephemeral containers;
  either resolved via token injection or sidestepped by preferring Codex.
- **hk-ege6 / hk-czb11 / hk-fufel** (CLOSED): the socket-perm + remote-cwd fixes whose seams
  (`resolveDialTarget`, `CommandInDir`, the TCP-loopback tunnel) the container mode reuses
  wholesale rather than reinventing.

---

## 2026-07-19 (operator-framing pass)

Sharpening pass answering the operator's two flagged questions — the Codex remote sidecar and the
Claude container model + comms-back — grounded in the driver/tunnel code. Where §1–§7 above already
answered something, this deepens or corrects it rather than repeating.

### Codex sidecar — what it is, and that it ALREADY runs remotely

**The "sidecar" the operator wants "running from that machine" is the `codex app-server` child
process** (`internal/codexdriver/driver.go:6`, spawned at `driver.go:170`; default argv
`{"app-server"}` at `driver.go:165`). The critical architecture fact: **the driver is NOT the
sidecar — the driver stays on box A.** `codexdriver` (the driver), `codexwire` (the JSON-RPC 2.0
NDJSON parser), `codexinput` (the pure reactor), and `resident.go` (the reconnecting supervisor) all
run **inside the daemon on gb-mbp**. The driver owns the child's stdin/stdout **directly**
(`driver.go:14-21`, AIS-009): `SpawnWindow` wires `cmd.StdinPipe()` / `cmd.StdoutPipe()`
(`driver.go:230-239`) and reads the app-server's event stream straight off stdout.

**Can the sidecar run from an incus container? YES — it already does the remote analogue today, with
no new transport.** When a codex crew is routed to an enabled ssh worker, the composition-root
`codexWorkerRoutingRunner` swaps the local exec for `SSHRunner{Host}` at `Command` /
`CommandInDir` time (`cmd/harmonik/substrate_select.go:150-206`). That means **only the
`codex app-server` process + its worktree + its credentials run on the worker**; its JSON-RPC wire
flows back to the box-A driver **over the ssh stdio pipes** — stdout of the `ssh host -- codex
app-server` process IS the app-server's stdout. For a container the picture is identical: the
container needs only the `codex` binary + repo worktree + codex auth; the driver, reactor, and wire
parser never leave box A.

**What reaches back, concretely:** (1) the app-server's JSON-RPC NDJSON event stream over ssh stdout
(this is the entire comms-back channel — turn output, tool calls, completion); (2) the git push of
the run branch, which box A then fetches directly (hk-7bwx). **Readiness is in-band:** the reactor
reaches `Ready` when the `initialize`→`thread/start` handshake completes off stdout
(`session.go:339-374`, `publishPhase` at `:368`). There is **no `agent_ready` hook, no hook relay,
no reverse tunnel** on the codex path — confirmed by the harness test note "codex ProcessExit harness
never emits agent_ready" (`internal/daemon/codex_daemon_commit_hkgd9r_test.go:46`). The `codexinput`
reactor's only emissions are `agent_input_submitted/acked/stale` + `agent_launch_failure`
(`internal/codexinput/reactor.go:68-77`), all derived from the stdout wire.

**What changes vs the current single-SSH-worker codex path:** essentially only **addressing**
(reaching the container's shell two hops away, lima→incus) and **provisioning**. The wire contract,
the fail-closed guard (`substrate_select.go:111`), and the remote-cwd fix (`CommandInDir`,
hk-czb11/hk-fufel) all apply **unchanged**. The reverse-tunnel / hk-ege6 class **does not gate
codex** — but the workloop still builds the tunnel and runs `waitWorkerSocketLive` for *every*
remote run (`workloop.go:3639`, `:3722`), so a codex remote run can still be failed by tunnel setup
it never uses. **Simplification (worth a bead): gate reverse-tunnel construction on the substrate
actually needing hooks** (tmux/Claude), skipping it for codexdriver — removes a whole failure mode
from the codex sidecar path.

### Claude container model + comms-back

**Recommendation: don't containerize Claude at all for the prototype — scope the container fleet to
CODEX implementers (per-run ephemeral), and keep Claude oversight on a persisted-login host.** This
falls straight out of three locked facts: (a) the standing posture is "prefer Codex for
implementation, Claude for oversight only"; (b) only the danger-full-access codex crews *need* the
isolation boundary the fail-closed guard demands — Claude oversight runs no permissive sandbox, so it
needs no container; (c) the OAuth wall (hk-bkd6h) makes ephemeral Claude fatal (every fresh session
hits the interactive `claude.com/oauth/authorize` wall). So: **codex implementers = per-run ephemeral
containers** (the disposable isolation boundary; codex auth via token/subscription sidesteps the
interactive wall); **Claude oversight = box A / a single long-lived host with a human-once `/login`**,
not a container. This avoids the Claude-in-container comms-back problem entirely for v1.

**IF the operator specifically wants Claude *inside* a container** (e.g. a containerized crew lead),
then the verdict is **one persistent container, not per-run** — the OAuth wall forces it (do the
human-once login once, persist the token/session; per-run relogins every launch). And only then does
the full two-hop reverse-tunnel burden below apply.

**Comms-back channel for Claude-in-container (the operator's "figure that out"):** three things must
traverse both hops, all on the **same ProxyJump addressing** (`SSHRunner.Opts = ["-J", <fleet>]`,
opts-before-host at `runner.go:117-119`; the reverse tunnel already carries those opts via
`sshHostOpts`, `reversetunnel.go:253`):
  1. **Spawn** — the detached `ssh … tmux new-window -d` that launches Claude: box A → fleet →
     container.
  2. **The reverse tunnel** — the long-lived `ssh -N -R 127.0.0.1:<port>:<daemonSock>`
     (`reversetunnel.go:232`) carrying **every hook** (SessionStart/agent_ready, progress,
     outcome_emitted) container → fleet → box A `daemon.sock`. With `-J`, the `-R` bind lands on the
     ssh **server** end = the **container's** loopback, which is exactly where Claude's hook
     subprocess dials — correct by construction, but the jump host must not swallow it.
  3. **Git fetch** of the run branch: box A → container.

**Where it breaks (tie to hk-ege6 / hk-go6nq):**
  - **The exact hk-ege6 trap:** is the `-R` TCP-loopback listener the container's sshd binds
    *connectable by the unprivileged agent user*? On macOS sshd-as-root it was root-owned 0600 →
    `permission denied`. Linux sshd normally runs the session as the target user, so it is *likely*
    fine — but this is the #1 thing to verify empirically and the #1 thing hk-go6nq's root-causer
    should check. **Keep the TCP-loopback invariant — never a UNIX socket for the `-R` bind**
    (`reversetunnel.go` WHY-TCP comment): TCP has no filesystem perm bits and dodges the trap
    regardless of who sshd runs as.
  - **The readiness gate** `nc -z 127.0.0.1 <port>` (`waitWorkerSocketLive`, `reversetunnel.go:329`)
    runs *through the ProxyJump on the container* — so `nc` must be in the golden image, and the
    probe must exercise the container's loopback, not fleet's.
  - **The daemon-side forward destination** `<daemonSock>` unix path is validated for length before
    the tunnel starts (hk-ta6dg, `workloop.go:3671`) — unchanged, still applies.
  - **Orthogonal but load-bearing — the chronic worktree-create bug (hk-2hfyt).** The workers.yaml
    operator history shows the daemon's ssh-runner-wrapped `git worktree add` *no-ops* (HEAD does not
    resolve) on the worker even when a manual `git worktree add` there succeeds — it recurred
    repeatedly (2026-07-07, 2026-07-11, 2026-07-12) and is a **daemon-side runner-wrapper defect, not
    the tunnel**. It is substrate-independent (hits codex AND claude remote runs) and **will replay
    in containers**. This, not the tunnel, may be the bigger relay risk; it must be root-caused
    before any container E2E is trusted.

### Open questions → decisions (where the code supports one)

- **Q6 Addressing → DECIDE ProxyJump-SSH (`-J` via `SSHRunner.Opts`).** For Claude it is essentially
  forced: the reverse tunnel needs a stable host:port to `-R` to, which nested `incus exec` cannot
  give. For codex it is not strictly required (no tunnel) but still reuses `SSHRunner` /
  `CommandInDir` / `sshHostOpts` verbatim. Nested `incus exec` would need a brand-new `IncusRunner`
  with unverified tty/pipe semantics *and* has no `-R` target. Reserve nested-exec only as a fallback
  if standing sshd in each ephemeral container proves heavier than expected.
- **Q3 workers.yaml v2 → DEFER the cap fight for the prototype.** Keep the version-1 registry as
  "one logical container-transport worker" (the fail-closed guard already keys on
  `WorkerSnapshot()` being `Enabled && Transport=="ssh"`, `substrate_select.go:152` /
  `workloop.go:3624`), and let a separate pool-manager own the N containers, presenting the
  currently-bound one as the single registry worker. **Tradeoff:** this only supports *serialized*
  single-container dispatch until v2 lifts `ErrTooManyWorkers`. If the prototype needs *concurrent*
  containers, v2 is required up front — so measure single-container throughput first.
- **Q5 Auth → DECIDE codex-only for unattended container crews.** Scope containers to codex
  implementers, whose token/subscription auth is headless; keep Claude oversight on a persisted-login
  host. This *resolves* hk-bkd6h for the fleet by scoping it out, rather than solving headless Claude
  OAuth.
- **Q2 hk-go6nq / Q1 tunnel-through-jump → still a SPIKE, but DOWNGRADED to the Claude path only.**
  If v1 is codex-only containers (per the auth decision), the two-hop reverse tunnel is not on the
  critical path at all — hk-go6nq stops being a launch blocker for the container mode and becomes a
  prerequisite only for the later "Claude in a container" option.

### The single spike that resolves the most (operator can run it)

From gb-mbp, with one hand-launched container
(`limactl shell fleet -- sudo incus launch agent-golden agent-03 --profile default --profile agent`):
1. **Codex path (the high-value, low-risk one):** point a `transport: ssh` worker at
   `-J <fleet> agent-03` and run one codex bead. If the app-server's JSON-RPC wire returns over ssh
   stdout and the run reaches `Ready` + lands a commit, the **entire codex sidecar story is proven**
   with zero new transport code — this is the fast GO signal.
2. **Worktree-create canary:** confirm the daemon's runner-wrapped `git worktree add` resolves HEAD
   inside the container (hk-2hfyt) — the substrate-independent risk.
3. **(Only if pursuing Claude-in-container):** stand a `ssh -N -R 127.0.0.1:<port>:<daemonSock>`
   through `-J <fleet>` and `nc -z` the container's loopback as the agent user — the hk-ege6 check.
