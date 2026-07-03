# Run Pi in a sandbox for daemon-dispatched jobs — HIGH PRIORITY

**Date:** 2026-07-02. **Priority:** operator-flagged HIGH — this is the near-term slice to build.
Status: scoping (two research threads in flight — sandbox options + code seams).

---

## Objective

> **Run the `pi` agent harness inside a sandbox for jobs dispatched through the daemon.**

The daemon picks a bead, launches **Pi** (headless coding agent, provider-agnostic via OpenRouter/
etc.) **inside an isolated environment** — own filesystem view, own git branch/worktree, own
subprocess tree — works the bead, returns a result branch the daemon merges. The sandbox is the
guarantee that Pi (a non-Claude harness we trust less to respect worktree discipline) **cannot write
to the host's main repo / main branch.**

This is the concrete, buildable first slice of the broader "isolated crew" case
(`plans/2026-06-30-distributed-fleet/00-the-case-for-isolated-crew.md`): start with **Pi + per-bead
job** (not full crew yet), because it's the smallest thing that exercises the whole sandbox seam.

## Why Pi + sandbox, and why now

- **Pi gives us more models** (OpenRouter/vLLM/local) — trade tokens for time on mechanical work.
  Already scoped in `plans/2026-06-23-pi-openrouter-harness/`.
- **Pi needs a sandbox more than Claude does.** Claude runs interactively in a tmux worktree with
  our discipline baked into its context. A cheaper/other-provider Pi run is exactly the case where
  "accidentally wrote to main" is a real risk — the sandbox is the *enforcement* that makes running
  an unfamiliar harness safe. (Mirrors the operator's Codex-wrote-to-main pain.)
- It's the smallest end-to-end proof of the **pluggable-harness-in-a-sandbox** seam that ideas 1/2/5
  all depend on.

## Scope

**In:** daemon dispatches a bead → spawns Pi inside a sandbox with an isolated worktree → Pi works
it → result branch fetched/merged → sandbox torn down. Sandbox backend is **pluggable + config-
driven** (no hard-coded framework — locked project principle). Warm build-cache reuse so spin-up
isn't dominated by re-compiling.

**Out (later):** full *crew* (queue-owning / subagent-delegating) in a sandbox — this plan is
per-bead job first. Multi-node placement (idea 1). Non-Pi harnesses in the sandbox (the seam should
allow it, but proving it is later).

## Key constraints (drive the whole design)

1. **macOS-primary (Apple Silicon).** The crew boxes are macOS — this is the dominant constraint and
   it narrows the sandbox options hard (see the options research below). Any "Linux container" on mac
   is really a Linux VM, which is a real tradeoff vs. true host-process isolation.
2. **Process-isolation preferred over full-VM / composable-computer.** Lightest thing that gives
   *real* isolation. Docker acceptable if needed; prefer lighter if it exists and is viable on mac.
3. **Warm build-cache is first-class.** Go (and maybe Rust/Haskell/OCaml) heavy compile — the
   sandbox must share/reuse a warm dep+build cache or spin-up is too slow. This axis can *veto* an
   otherwise-attractive backend.
4. **Daemon-drivable programmatically** (spawn, feed workdir, stream, detect exit) — reuse the
   existing Runner/substrate seam, not an interactive-only tool.
5. **Still attachable.** Operator can drop into the running Pi session (tmux/shell). Inspectability is
   a locked design preference; the sandbox must not kill it.
6. **No hard-coded backend or credentials.** Sandbox backend + Pi provider/model/keys are operator
   config; fail loud if unset.

---

## Prior work to build on (confirmed to exist)

- **`plans/2026-06-23-pi-openrouter-harness/`** — the Pi harness brief. `AgentTypePi` already
  declared (`internal/core/agenttype.go:19`) but unimplemented; **codex is the template**. Crew data
  plane already model-agnostic (shell CLIs + `jq`, no MCP). Config-not-hardcoded principle locked.
- **`plans/2026-06-30-distributed-fleet/02-container-sandbox/`** — the container/sandbox stub +
  backend notes (Daytona/Singularity/Modal; process-not-VM steer). ON-024/ON-025 confinement spec
  invariants. Test-pyramid **L3 `DockerExecRunner`** (`hk-yflqo`, merged `4bdf7e93`) proves harmonik
  can drive a container runner — starting point for a production one.
- **`plans/2026-06-30-distributed-fleet/05-hermes-harness-study/`** — Hermes' **workspace *kinds***
  (`scratch`/`worktree`/`dir:`) and terminal-backend sandboxing (Docker/SSH/Modal/Daytona/
  Singularity) are direct prior art for a per-job sandbox property.
- Remote-substrate path (worktree-per-run, branch fetch-back) — the sandbox job reuses this shape,
  swapping "remote box" for "local sandbox."

---

## Sandbox options — comparison + recommendation

### The one fact that frames the decision

**On macOS there is no native Linux-container isolation.** Docker / OrbStack / Colima / Podman /
Apptainer / bubblewrap / nsjail / Landlock every one *is* or *requires* a Linux VM — a "container on
a Mac" is namespace isolation *inside a shared Linux VM*, not host-process isolation on Darwin. Since
the crew boxes are macOS, the real field is three choices:

1. **Native host-process sandbox** → **`sandbox-exec`/Seatbelt** — the *only* true host-process
   option on mac.
2. **Native micro-VM** → Apple's `container` (macOS 26 + Apple-Silicon only; young v1.0; volume/exec
   story under-documented — **skip for now, revisit in 6–12 mo**).
3. **Linux-VM-backed container** → **OrbStack** (best of the group) / Docker Desktop / Colima.

### The decisive axis: warm build-cache — and it cuts *against* containers on mac

- Every heavy toolchain cache (Go `GOCACHE`/`GOMODCACHE`, cargo, cabal, opam, dune) is
  content-addressed → **safe as a read-only warm base, unsafe as a concurrent shared writer.**
  Canonical shape: **RO warm base + per-run private writable overlay** (overlayfs / `cp --reflink` /
  sccache).
- **On any Linux-VM container on mac, a host-bind-mounted cache pays the VirtioFS per-file penalty** —
  pathological for caches of 10k–100k tiny files. The fix (VM-native named volume) runs fast **but is
  no longer a plain host dir shared with your native `go`/`cargo`.** That's the real cost of the
  container path.
- **Seatbelt has no VM boundary → the cache is the native host filesystem.** Warm reuse is *free* and
  *shared with host-native tooling*. This is Seatbelt's single biggest win for our build-heavy load.

### Prior art (load-bearing): the three native-macOS agents all converge on Seatbelt

**OpenAI Codex CLI, Claude Code (shipped Oct 2025, ~84% fewer prompts), and Cursor local Agent mode
all use `sandbox-exec`/Seatbelt on macOS** and **bubblewrap+seccomp / Landlock on Linux.** Anthropic
even publishes the mechanism standalone — **`@anthropic-ai/sandbox-runtime` ("srt")** — which
sandboxes an arbitrary process *with no container*. Their FS model is exactly our requirement: write
only to CWD + `$TMPDIR`, read broadly, and Codex forces **`.git` read-only** inside the writable root
— the precise "agent must not write the main repo" guarantee. Network is handled *outside* the
profile via an allowlist proxy.

### Comparison (condensed)

| Option | Class | macOS | Warm cache | Drive / attach | Caveat |
|---|---|---|---|---|---|
| **Seatbelt / `sandbox-exec`** | Host-process | **Native, no VM** | **Trivial — native host FS** | SBPL profile + exec; native tmux/ps/lldb | Man page "deprecated" (functional, no removal date); SBPL thinly documented; **Go CLIs fail TLS under it (exclude)**; not a hardware boundary |
| **OrbStack** | Container-in-VM | VM-backed | VM-native volume (not host-shared) | Docker API; `docker exec`+tmux | ~2s boot, best bind I/O; **proprietary/paid commercial** |
| **Colima+Lima** | Container-in-VM | VM-backed | VM-side cache | Standard Docker socket | **Free/OSS**; slower boot, fixed resources |
| **Docker Desktop** | Container-in-VM | VM-backed | BuildKit/named volumes | Docker API | mature; paid for big orgs; ~4GB idle |
| Apple `container` | Micro-VM | mac26+ARM only | under-documented ⚠️ | CLI/Swift | young v1.0 — defer |
| bubblewrap / Landlock / nsjail | Host-process | **Linux only** | clean RO-repo/RW-cache bind | CLI / kernel API | for Linux crew nodes; mirror Codex |
| Apptainer | Rootless container | needs Linux VM on mac | excellent | CLI | disqualified on mac by the VM req |
| E2B / Modal / Daytona / Fly | Remote VM | runs off-machine | snapshots | SDK | **wrong shape** — repo leaves the box |

### Recommendation — the straight answer

- **v1 default: `sandbox-exec`/Seatbelt, cloning the Codex/Claude-Code pattern (ideally adopt
  `@anthropic-ai/sandbox-runtime`).** It's the *only* option satisfying all four hard constraints at
  once on macOS: native host-process isolation (no VM), **trivial warm-cache reuse** (native FS,
  no VirtioFS tax, shared with host tooling), daemon-drivable (generate an SBPL profile per run,
  exec, stream, wait), and attachable with ordinary host tools. Three shipping agents prove the
  daemon-spawns-sandboxed-subprocess pattern in production. It's also the **lightest real isolation**
  — matching the "lightest thing that gives real isolation" preference exactly.
- **Escalation: OrbStack**, when a task needs a *true* kernel/VM boundary or a real Linux container
  (put the warm cache in a **VM-native named volume**, not a host bind mount). Colima if the OrbStack
  commercial license matters.
- **Skip now:** Apple `container` (too young), all hosted platforms (wrong shape), Apptainer (needs a
  VM on mac).
- **Linux crew nodes (secondary):** mirror Codex — **bubblewrap + seccomp + Landlock**. Avoid
  firejail (setuid CVE history).

**Honest cost of the Seatbelt default:** it's a *policy on a host process*, not a hardware boundary —
weaker than a VM; a broad `allow-write` or an exposed socket is an escape hatch. SBPL is sparsely
documented (learn it from Codex's `seatbelt.rs`, not Apple). Known toolchain rough edges: **Go-based
CLIs fail TLS verification under Seatbelt and must be excluded**, `docker` is incompatible, some
tools need Apple-Events carve-outs — budget a per-toolchain shakeout.

## Code seams this plugs into

**Headline finding: Pi is already fully implemented and harness-blind. There is NO harness work to
do — the entire task is the isolation/substrate axis.** (`piharness.go`, `pilaunchspec.go`,
`pijsonlparser.go` are complete — PI-010…050 landed; Pi is structurally a clone of codex,
`CompletionProcessExit` + `SessionIDCaptured`, selected via the clean 4-tier `resolveHarness`
precedence in `internal/daemon/harnessresolve.go:53`). So "run Pi in a sandbox" = "give the daemon a
sandbox *substrate*," nothing Pi-specific.

The isolation seam is already clean and single-implementation today:

- **`handler.Substrate`** (`internal/handler/substrate.go:30`) — a **1-method interface**
  (`SpawnWindow(ctx, SubstrateSpawn) → SubstrateSession`). The only production impl is
  **`tmuxSubstrate`** (`internal/daemon/tmuxsubstrate.go`), which runs the agent argv in a `tmux
  new-window`. A **container substrate is a drop-in alternative** — this is the primary insertion
  point.
- **`tmux.CommandRunner`** (`internal/lifecycle/tmux/runner.go:16`) — a 1-method
  `Command(ctx,name,args...) *exec.Cmd` seam with prod impls `LocalRunner` and **`SSHRunner`** (the
  remote-worker path). **The L3 test's `hkyflqoDockerExecRunner` already satisfies this exact
  interface** (`docker exec <cid> <cmd>`) — it just needs promoting from test to a production
  `DockerExecRunner`.
- **The remote/SSH path already solved the near-identical problem.** `perRunSubstrate`
  (`tmuxsubstrate.go:1058`) carries a `runner tmux.CommandRunner` + session cwd, and
  `spawnWindowVia(... remote=true)` (`tmuxsubstrate.go:733`) routes `tmux new-window` + worktree onto
  a worker over SSH. **A container backend is the same shape with a docker/OrbStack runner instead of
  `SSHRunner`.** This is the single most important architectural fact: containers are a *sibling axis*
  to remote workers, reusing the same abstraction the remote path already proved.

**Worktree / filesystem view:** worktree-create is `internal/workspace/createworktree.go` (`git -C
<repo> worktree add --detach`), flowing as `RunCtx.WorkspacePath` → `SpawnSpec.WorkDir` →
`SubstrateSpawn.Cwd`. Today the worktree is deliberately "a plain subfolder" of the main repo
(`specs/workspace-model.md:198` excludes provisioning layers from MVH). For a real sandbox the
worktree + caches must be **mounted/relocated inside the container** so the agent's whole FS view is
the sandbox — intercept at `createworktree.go` (run `git worktree add` via the container-aware
runner) and the `Substrate.SpawnWindow` cwd/env/mount handling.

**Config surface:** harness choice is `Config.DefaultHarness` + `harness:pi` bead label; Pi runtime
config is the `harnesses:` block (`projectconfig.go`, `PiHarnessConfig` — provider/model/api_key_env).
A `sandbox:`/container backend config lands either as a **new block peer to `harnesses:`** in
`projectconfig.go`, or — closer to the existing grain — as a **`transport: docker` / `sandbox:` field
on `workers.Worker`** (`internal/workers/workers.go:39`) so the per-bead dispatch gate
(`workloop.go:~2977`, `itemLocalOnly`/`itemWorkerTarget`) picks a `DockerExecRunner` instead of
`SSHRunner`. **Reusing the worker-routing grain is the lighter path** — it treats "a local sandbox"
as just another routing target the daemon already knows how to select.

### Files a first cut would most likely touch

1. `internal/lifecycle/tmux/runner.go` — promote a production `DockerExecRunner` (from the L3 test)
   implementing `CommandRunner`.
2. `internal/daemon/tmuxsubstrate.go` — route `perRunSubstrate.spawnWindowVia` via the docker runner,
   mirroring the existing `remote=true` SSH branch.
3. `internal/workspace/createworktree.go` / `worktreepath.go` — relocate `git worktree add` + cwd/
   mount mapping inside the container.
4. `internal/daemon/projectconfig.go` — add the `sandbox:`/container config block.
5. `internal/workers/workers.go` — extend `Worker`/`Config` with a sandbox transport (if reusing the
   worker-routing grain).
6. `internal/daemon/workloop.go` — per-bead dispatch gate (~2977) + substrate wiring in
   `newWorkLoopDeps` (~984).
7. `cmd/harmonik/main.go` / `run.go` — composition root that constructs the Substrate.
8. `internal/handler/substrate.go` — only if `SubstrateSpawn` needs new fields (mounts, image, cache
   volumes).

**Implication for scoping:** because Pi is done and the substrate seam is clean + already exercised
by the remote path, the v1 is genuinely tractable.

### ⚠️ The Seatbelt choice makes the wiring *lighter* than the container-substrate seam above

The code-seam map above assumes the **container** path (a new `DockerExecRunner` swapped in as a
`CommandRunner`/`Substrate`). But the recommended v1 backend is **Seatbelt**, which is **not a
runner/substrate swap** — it's an **argv wrapper on the existing local tmux spawn**:
`sandbox-exec -f <run-profile>.sb <agent-argv>`. So for the Seatbelt v1:

- **No `DockerExecRunner`, no container substrate.** The existing `tmuxSubstrate` + `LocalRunner`
  stay; you **prefix the agent argv** with `sandbox-exec -f <profile>` at launch-spec assembly
  (`buildCodexRoutedLaunchSpec` / the Pi routed path in `harnessregistry.go`) — or, cleaner, wrap it
  in the substrate spawn so *every* harness can opt in.
- **The worktree stays a plain host subfolder** (no relocation/mount needed) — Seatbelt just denies
  writes outside it + `$TMPDIR` and forces `.git` read-only. This sidesteps the whole
  `createworktree.go`-relocation problem the container path required.
- **New work is really: (1)** an SBPL profile generator (writable-root = the run's worktree +
  `$TMPDIR` + the toolchain caches; read = broad; `.git` RO; Go-CLI TLS exclusions), **(2)** wrap the
  launch argv, **(3)** a `sandbox:` config block (backend = `seatbelt|orbstack|none`, cache paths,
  network-proxy allowlist). Reuse `@anthropic-ai/sandbox-runtime` if we don't want to hand-roll SBPL.
- **The container path (`DockerExecRunner` + substrate)** from the seam map becomes the **escalation
  backend** (OrbStack), built behind the same `sandbox:` config seam later — *not* v1.

So the two backends land behind one config seam but touch very different code: **Seatbelt = argv
wrapper (light, v1); OrbStack = the container substrate/runner (heavier, later).** This is the single
most important scoping takeaway — v1 does **not** need the container substrate work at all.

---

## Proposed v1 shape (both big tensions now resolved by the research)

The two questions I'd flagged as needing discussion are answered by the evidence:

- **Backend:** Seatbelt wins the v1 default; OrbStack is the escalation behind the same config seam.
- **Boundary shape:** filesystem-sandbox-around-the-worktree (Seatbelt argv wrapper), **not** a
  container-with-mounted-worktree, for v1.

So the concrete v1:

> The daemon dispatches a bead → assembles the Pi launch argv → **wraps it in `sandbox-exec -f
> <generated-profile>.sb`** → spawns it in the existing local tmux worktree. The SBPL profile makes
> the run's worktree + `$TMPDIR` + toolchain caches writable, everything else read-only, and `.git`
> read-only-inside-the-writable-root. Result branch merges as today. A `sandbox: {backend: seatbelt,
> ...}` config block gates it; `backend: none` = today's behavior; `backend: orbstack` is the later
> container path.

This needs **no container substrate and no worktree relocation** — it's the SBPL profile generator +
argv wrap + config block. Cache reuse is free (native host FS), sidestepping the VirtioFS penalty and
the mounted-shared-cache TOCTOU footgun (each run writes a private overlay/worktree; caches are RO
warm bases).

## Decisions locked (operator, 2026-07-02)

- **Support both macOS and Linux; ship macOS first.** `srt` covers both with one interface (Seatbelt
  on mac, bubblewrap on Linux), so Linux is mostly config + testing on top of the mac work.
- **Adopt `@anthropic-ai/sandbox-runtime` (`srt`) — do NOT hand-roll.** Operator steer: "go with
  whatever is reliable and we don't have to hand-roll." `srt` is the reliable, cross-platform,
  production-proven (Claude Code / Codex / Cursor) choice; accept the Node dependency at the launch
  site.

**→ The full implementation brief for the next agent is [`HANDOFF.md`](HANDOFF.md).**

## Remaining open questions (for discussion)
- **Go-CLI TLS-under-Seatbelt exclusions:** `br`, `harmonik`, `gh` are Go binaries the sandboxed Pi
  must still call (comms/beads/dispatch). They fail TLS verification under Seatbelt → need explicit
  carve-outs or must run *outside* the sandbox boundary. This is the fiddliest real-world detail —
  worth a spike early.
- **Network policy:** SBPL can't inspect TLS, so egress control needs the external allowlist-proxy
  pattern (Codex/Claude model). Is v1 network-open (rely on the FS boundary) or do we stand up the
  proxy? Lean network-open for v1, proxy later.
- **Per-bead fresh sandbox vs. warm pooled sandbox** (Hermes' scratch-vs-persistent): fresh per bead
  is simplest and matches the worktree lifecycle; revisit pooling only if spin-up hurts.
- **Attachability check:** confirm `tmux attach` into a Seatbelt-wrapped Pi session behaves normally
  (it should — native host process) — a 10-minute validation, not a design risk.
- **Then: crew (not just per-bead job) in the sandbox** — the follow-on once the per-bead Pi path is
  green (ties back to `00-the-case-for-isolated-crew.md`).
