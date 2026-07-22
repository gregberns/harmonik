# P3 — Distributed Execution Plan (containerized beads, local + remote)

> Status: DRAFT for review (pre-kerf). Daemon + comms DOWN — design only, all claims grounded
> in the research set: A3 (`research/A3-incus-and-scheduling.md`), A5 (`research/A5-fable-problem-framing.md`),
> realignment 03 (`plans/2026-07-20-codex-strategy-realignment/research/03-remote-architecture.md`),
> A2 (`research/A2-substrate-v2-architecture.md`), and the LOCKED `DECISIONS.md` (C1–C6, Priority-0).
>
> Scope discipline: this replaces the scrapped ssh remote-worker model. P3 is the FIRST consumer /
> boundary-test of the P1 kernel. Any capability P3 needs from the fabric is a **P1 requirement**,
> not a P3 hack (governing rule from C2: "no hacks in either P1 or P3").

---

## 1. Goal

A bead runs **fully inside a container** — repo in, agent in, result out — on this machine or
another, for two reasons the operator named: **security** (a real isolation boundary that is not
per-harness, so the danger-full-access codex posture is safe) and **load** (lift the one-worker
`ErrTooManyWorkers` cap so more than one bead runs at once).

The container is **the unit of execution**. "Local" and "remote" are the *same shape*: a worker
supervises containers on its box; "remote" only means that worker lives on a different machine and
is reached through the kernel's transport. There is no separate remote code path — same dispatch,
same liveness, same result-return, whether the worker is `gb-mbp` itself or a box across Tailscale.

Concretely for v1 (per Priority-0 + C5): **codex-first, whole-bead-run per container, results home
as a git branch, basic dispatch, deterministic liveness, no scheduler.**

---

## 2. The execution model

### 2.1 Substrate (real NOW)

`gb-mbp` (macOS, daemon host) → lima VM **`fleet`** (Ubuntu) → **incus** containers launched inside
`fleet`. Golden image **`agent-golden`** already carries pi/claude/codex. One container launches by
hand today:

```
limactl shell fleet -- sudo incus launch agent-golden agent-03 --profile default --profile agent
```

The v1 golden image (hk-u6qu6 line of work) must additionally carry: the harmonik worker binary at
a known path, a pre-warmed Go build cache, a pre-cloned repo, and pi runtime staging
(`models.json`, `.harmonik/pi-agent/`, `PI_CODING_AGENT_DIR`) if pi is ever run in-box. Version the
image; pin the harmonik binary SHA; do NOT hard-pin external tool versions (degrade gracefully, per
the no-external-version-binding standing rule).

### 2.2 How a bead + its DOT plan get into the container

The container receives a **reference, not a payload** (this is the whole point of C1):

1. **The bead**: the primary hands the worker a bead id + base SHA over the kernel work channel.
   The container's agent-runner resolves the bead's DOT graph from the bead itself (same DOT the
   daemon builds today) — the plan is derived in-box, not shipped as a blob over the fabric.
2. **The repo**: already pre-cloned in the golden image; the container `git fetch`es the base SHA
   into its ODB and cuts the run worktree locally. **Git is the code/data plane (C1)** — the repo
   never travels over the fabric.
3. **The dial-back address**: for codex, this is nothing (see 2.3 — codex is in-band). The only
   control-plane wire the container needs is its channel back to the worker/primary.

### 2.3 How the agent runs (codex sidecar — the de-risking finding)

The "sidecar" is the **`codex app-server` child process**. The driver
(`codexdriver`/`codexwire`/`codexinput`/`resident.go`) **stays on the daemon host**. When a codex
run routes to a container, only `codex app-server` + the run worktree + credentials run in-box; its
JSON-RPC NDJSON wire flows back **in-band over the ssh stdout pipe**; **readiness is in-band** (the
`initialize` → `thread/start` handshake off stdout). Consequences that make codex the v1 target:

- **No `agent_ready` hook, no hook relay, no reverse tunnel** on the codex path. The entire
  `ssh -N -R` / `bridge_dial_failed` / `agent_ready_timeout` failure class (hk-ege6, hk-qxvc2)
  **does not gate codex.** (Flagged simplification worth carrying: today's workloop still *builds*
  the reverse tunnel + runs `waitWorkerSocketLive` for every remote run even when codex never uses
  it — gate tunnel construction on the substrate actually needing hooks.)
- The container is the isolation boundary the danger-full-access codex posture (`approval_policy=never`,
  `danger-full-access`) needs. A bead's codex run lands its commit inside the boundary, then the
  container is destroyed clean.

Claude in-container is explicitly **out of v1** (OAuth wall — see §6). Claude stays on the
persisted-login host for oversight/review only.

### 2.4 How results come home (git branch — C1, NOT over the fabric)

The container commits to a `run/<id>` branch inside its worktree. The daemon pulls it home with
`git fetch ssh://<container-or-worker>/<repo> run/<id>:refs/heads/run/<id>`, then the UNCHANGED
local `mergeRunBranchToMain` merges and only the daemon host's credentials push `main` to origin.
**The fabric carries the kickoff, the status events, and the liveness — never the diff.** This is
C1 made literal: control plane over the kernel, artifact plane over git.

### 2.5 Contrast with the scrapped ssh model (so we don't rebuild it)

| Dimension | Scrapped ssh model (why it was wrong) | P3 container model |
|---|---|---|
| Addressing | per-run `ssh -N -R` reverse tunnel, hand-allocated ephemeral ports (`reversetunnel.go`) | kernel channel; no bespoke tunnel on the codex path |
| Identity/registration | one hardcoded primary worker (`PrimaryWorkerIndex` always 0), boot-only registry, `ErrTooManyWorkers` cap | worker/container registers itself via kernel LOOKUP; N workers |
| Status/events | hook relay smuggled over `ssh -N -R`, silent `bridge_dial_failed` | kernel PUBSUB status channel (codex readiness is in-band) |
| Env/context | `exec env KEY=VAL` argv-prefix hacks (hk-okqyx), ssh drops env | control-plane headers on the kernel envelope |
| Where the code lived | every hack inside `internal/daemon` | execution path born OUTSIDE `internal/daemon` (C4 non-negotiable) |
| Result return | git fetch of `run/<id>` branch — **KEEP THIS**, it was the one right call | identical (C1) |

The lesson (D4, A5 §1): every ssh wedge was **a missing fabric feature reinvented badly, per-path.**
P3 must express each of these as a kernel primitive, not a bespoke pipe. The failure mode to avoid
is "temporary websocket, migrate later" — that is exactly how the ssh model came to exist.

---

## 3. How P3 consumes P1 (the boundary test)

P3's execution-dispatch is the kernel's **first plugin** and therefore the boundary test: if P3
needs a kernel RPC that isn't in the 19, the boundary is wrong (substrate-v2 kill criterion). Per
**C6 (LOCKED): the plugin↔kernel line is a Go interface, IN-PROCESS** — the dispatch plugin is a Go
library linking the kernel interface, NOT a separate binary/gRPC. The **kernel is the network-aware
layer**; the plugin is always local to its daemon and reaches other machines THROUGH the kernel's
transport (kernel↔kernel = network).

### 3.1 Exact kernel primitives P3 consumes

| P3 need | Kernel primitive (P1) | Channel type |
|---|---|---|
| Hand a bead to some worker (competing consumers, one worker gets each) | work channel | `POINT_TO_POINT` |
| Kickoff + ack of a specific run; per-run control queries | request/reply | `REQUEST_REPLY` |
| Stream run status/progress events back to the primary | status channel | `PUBSUB` |
| Worker/container registers itself; primary discovers what's reachable | self-registration / address book | `LOOKUP` |
| Primary detects a whole worker going dark (box liveness) | roster | `RosterList` / `RosterWatch` |
| Dispatch plugin's durable state (running-bead registry, requeue bookkeeping) | box-local kernel storage | `KVPut/Get`, `JournalAppend` |

Mapped to the kernel RPCs: `Publish`/`Subscribe` (status), `Request`/`Serve`/`Respond` (kickoff/ack),
`LookupPut`/`LookupGet`/`LookupList` (registration), `RosterList`/`RosterWatch` (worker liveness),
`KVPut`/`JournalAppend` (dispatch durable state), `Info` (read `max_payload_bytes`, never hardcode).

### 3.2 What this flags as P1 requirements (build minimal-but-sufficient)

These are **P1 requirements surfaced by its first consumer**, not P3 hacks:

1. **Ephemeral join/leave in LOOKUP + roster.** A2 thin-spot #4: LOOKUP/roster as specced assume
   long-lived daemons on static Tailscale addresses. A worker registering, and a *container*
   appearing/vanishing, is an extension P1 must support. **P3 REQUIRES this from P1.**
2. **A `POINT_TO_POINT` work channel with competing-consumer group semantics** so exactly one
   worker claims each dispatched bead. Present in the channel-type enum; P3 is its first real user.
3. **Box-local storage exposed to the in-proc plugin** (the "resource API" reserved in the kernel
   design notes) — the dispatch plugin needs `KV`/`Journal` for its running-bead registry and
   requeue bookkeeping. Design the seam now; P3 is the first plugin that actually needs it, so it
   gets built here (not speculatively).
4. **At-most-once is fine; durability is P3's job.** The kernel never queues/retries/redelivers by
   design. P3's dispatch plugin owns the durable-dispatch layer (requeue on death/hang). For v1
   this is the *simple* version (C5), not full leases/redelivery — but it is still plugin-layer,
   built from journal + message_id + the liveness signals, never a new kernel verb.

Guardrail (to ratify): if two consecutive plugins need a new kernel RPC, stop and re-draw the line.

---

## 4. Dispatch + liveness (C5 — simple now)

**Topology (C3, LOCKED, static config):** one daemon configured as **primary/hub** (holds the
queue), the rest as **workers/spokes**. Workers' config simply *names the primary* — static config,
not dynamic discovery (that's a later capability). Dispatch is **pull**: the worker pulls a bead off
the `POINT_TO_POINT` work channel, starts a container, supervises it. The transport underneath is
leaderless/resilient (no single point of failure); the *queue* is a central application fact — if
the primary dies, restart it, don't rebuild the network.

### 4.1 The non-skippable minimum

A dead or hung container's in-flight bead **must not strand as `in_progress`.** This is today's pain
(the reason `gb-mbp` was disabled after the 2026-07-12 empty-HEAD incident) multiplied by N
containers — we cannot reintroduce it. Heartbeat/timeout → **mark the bead failed and requeue it.**
This is not scheduling; it is the one control the simple version still must have.

### 4.2 The deterministic liveness/health control set (C5 sub-task: enumerate the "handful")

Operator's core set (1–4), plus the additional controls the enumeration surfaces (5–9). Controls
1–5 are the **simple-now baseline**; 6–9 are cheap, deterministic, and should be included because
each closes a distinct strand-the-bead hole. None require an agent or a scheduler.

| # | Control | Catches | Simple-now? |
|---|---|---|---|
| L1 | **Worker running-bead registry** — worker knows every bead running on it (durable in kernel KV, survives worker restart) | lost accounting of in-flight work | YES (C5.1) |
| L2 | **Container process liveness poll** — worker periodically checks the container process is alive; on actual death → restart / notify / flag | container/agent crash | YES (C5.2) |
| L3 | **Per-DOT-node timeout** — a run stuck on the same DOT node too long is killed and flagged back to the primary | HANGS (which L2's alive-check cannot see) | YES (C5.3) |
| L4 | **Container-info handoff to primary** — worker passes container identity/address to the primary at start | enables later agent-based deep inspection | YES (C5.4) |
| L5 | **Orphan → requeue (the non-skippable §4.1)** — any bead whose container is dead/hung is marked failed and requeued, never left `in_progress` | stranded `in_progress` beads | YES (mandatory) |
| L6 | **Worker heartbeat / roster** — primary watches the roster; a whole worker going dark requeues ALL its in-flight beads (not just one container) | a worker/box vanishing (kernel liveness: ALIVE/SUSPECT/DEAD) | YES (roster is a kernel primitive) |
| L7 | **Spawn-time reachability fail-closed** — a launched-but-UNREACHABLE container fails closed; the spawn check verifies reachability of the *specific* container, never falls back to local unsandboxed | a container that launches but can't be driven (extends hk-5h759 fail-closed guard) | YES (safety-critical) |
| L8 | **Startup/onboarding timeout** — container never reaches codex `Ready` (in-band `initialize`→`thread/start`) within a bound → fail + requeue | dead-on-arrival containers | YES |
| L9 | **Result-return watchdog** — `run/<id>` branch doesn't come home within a bound after the run reports complete → treat as failure, don't hang the run | git-fetch/merge-back stalls | YES |

**Explicitly DEFERRED (named, not now):**
- **Full leases with redelivery + typed orphan-recovery** (park vs requeue vs escalate). v1 does
  the simple mark-failed-and-requeue; typed block-reasons (A1/Hermes prior art) come later.
- **Agent-based deep inspection** ("reach into the box and inspect") — L4 passes the container info
  that *enables* this later; the inspection itself is a later capability.
- **Scheduling / placement** — no per-bead weight, no (box×model) capacity, no policy engine. The
  scheduling assessment's data-first mandate holds: log signals first, analysis tool second,
  declarative hot-reloaded policy plugin last. A container fleet is a *producer* of those signals;
  it does not need the placement engine to run. Defer per S-lanes A/B/C.

### 4.3 Concurrency note

v1 keeps the registry as "one logical container-transport worker" is the OLD framing — P3's whole
point is to lift `ErrTooManyWorkers`. But start with **serialized single-container dispatch** to
measure single-container throughput and warm-cache economics before committing to concurrent
containers. Concurrency is an increment on top of a working single-container path, not a v1 gate.

---

## 5. The delivery scaffold (Option 4) — with a demolition date

**What it is:** the codex sidecar already runs remotely over ssh stdout in-band with **zero
reverse-tunnel machinery** (A3's key de-risking finding). A container is "just addressing +
provisioning" away. So the scaffold is: **codex `app-server` sidecar inside one incus container,
per-run ephemeral, driver at home, results home as a git branch** — buildable NOW with near-zero
new transport code.

**Purpose:** keep proof flowing while P1's kernel + P3's in-proc dispatch plugin are built. It
proves the *container-as-isolation-boundary* and the *git-branch-home* half end-to-end, independent
of the fabric.

**Build (near-zero new transport code):**
1. Reuse the landed seams: `SSHRunner`/`CommandInDir` remote-cwd fix (hk-czb11/hk-fufel, PR#32/#33),
   the fail-closed spawn guard (hk-5h759). Reach the container via **ProxyJump through `fleet`**
   (`ssh -J <fleet> <container-ip>`) — reuses the transport + `CommandInDir` with least new code.
2. Golden-image the container (harmonik binary at known path, pre-cloned repo, warm Go cache).
3. Route ONE codex bead to ONE ephemeral container: launch → `git fetch` base → cut worktree → run
   codex `app-server` (in-band readiness) → commit to `run/<id>` → daemon `git fetch` home → merge →
   destroy container clean.

**E2E gate (mirror hk-g0ror.4 rigor):** codex crew runs sandboxed inside an ephemeral container,
commit lands, run reaches Ready + completes review, container destroyed clean, branch merged.

**DEMOLITION DATE — explicit:** the scaffold **dies the day the P1-based execution plugin can run
the same bead through the kernel.** It is a scaffold, not the architecture. Per-harness, ssh-shaped
transport is *poison as the architecture* (it rebuilds "N bespoke transports") — acceptable only as
a time-boxed proof. Track its removal as a bead blocked-by the first green run of the kernel plugin;
do not let it calcify the way the ssh model did. Recommended hard cap: retire within **one P1
milestone** of the dispatch plugin's first green container run (operator to set the calendar date at
kerf — see Questions).

---

## 6. Ring-fence the landmines (pre-work, so they don't masquerade as fabric failures)

These are independent of the architecture and **will replay inside containers.** If not
ring-fenced, each will look like a fabric failure and burn trust in the new system (A5 R5). Treat as
pre-work / parallel hardening, NOT discovered mid-E2E.

1. **hk-2hfyt — worktree-create defect (chronic, substrate-independent, possibly the bigger relay
   risk).** The daemon's ssh-runner-wrapped `git worktree add` no-ops (HEAD doesn't resolve) even
   when a manual `git worktree add` succeeds on the same box. It is a **daemon-side runner-wrapper
   defect, not the tunnel**, and it hits codex AND claude. It **will replay in containers** and must
   be **root-caused before any container E2E is trusted.** This is the #1 pre-work item — a green
   container run on top of an unfixed hk-2hfyt is not a real green.
2. **Claude OAuth wall (hk-bkd6h).** A fresh Claude session does NOT inherit operator OAuth — it
   hits an interactive `claude.com/oauth/authorize` wall and never joins comms. **Fatal for
   ephemeral Claude containers** (every launch is fresh). Standing scope-out: **codex-only for
   unattended containers**; Claude oversight/review stays on a persisted-login host. This is why v1
   is codex-first (also = Priority-0). Do not attempt headless Claude in-container in P3.
3. **Warm-cache economics (unvalidated).** Per-language build cost is why "per-run ephemeral" may
   not survive contact — a cold container re-warming a Go/node/etc. build cache per run may dominate
   runtime. Measure single-container throughput (§4.3) BEFORE committing to per-run-ephemeral vs a
   warm pool. Residency (a per-machine worker owning cache warmth) is the natural home if ephemeral
   proves too expensive — but that is a measured decision, not a v1 assumption.

Also carry forward from A3/A2 as pre-work checks (not blockers, but verify early):
- **Linux `-R` TCP-loopback connectability** — on the codex path there's no reverse tunnel so this
  is moot for v1; keep the TCP-loopback invariant (never a UNIX socket for `-R`) if any hook path is
  ever added.
- **Container fail-closed (L7)** — a launched-but-unreachable container must fail closed at spawn.

---

## 7. Open questions

Resolved here (design calls, defensible from the research + locked decisions):

- **Whole-run vs per-node in a container?** → **Whole bead run per container for v1** (matches
  today; simplest). BUT the dispatch protocol must not *structurally forbid* per-node distribution
  later (axis 7 — reviewer-on-trusted-host + implementer-in-container dissolves the hk-qxvc2
  entanglement). Design the channel/registration so a future per-node split is additive.
- **Ephemeral vs warm pool?** → **Start per-run ephemeral**, add a warm pool only if launch +
  cache-materialize latency proves to be the bottleneck (§4.3, §6.3). Owned by a provision/pool
  manager OUTSIDE the fast work loop, fronted by a future `harmonik fleet`/`worker provision` CLI.
- **Addressing across the two hops?** → **ProxyJump-SSH through `fleet`** for the scaffold (reuses
  landed seams, least new code). For the kernel path, the container reaches its worker over the
  kernel transport; the two-hop is a transport-config detail, not a new protocol.
- **Where does the execution path live?** → **OUTSIDE `internal/daemon`** (C4, non-negotiable). The
  in-proc dispatch plugin + container supervisor are born outside the god package or not at all.
- **Codex vs Claude in-container?** → **Codex-only for v1** (OAuth wall + Priority-0).

### Questions for operator (judgment calls — not for the plan to decide)

1. **Scaffold demolition date (calendar).** §5 sets the *condition* (dies when the kernel plugin
   runs the same bead) and proposes "within one P1 milestone of first green." Do you want a hard
   calendar date, or is the condition-based trigger enough?
2. **Concurrency ceiling for v1.** Start serialized single-container (§4.3) to measure throughput +
   warm-cache cost first, THEN turn on N-concurrent — or do you want a target concurrency (e.g. 2–3
   containers) from the first green run?
3. **Ephemeral-vs-warm decision owner.** If warm-cache economics (§6.3) prove per-run-ephemeral too
   expensive, adopting a warm pool means a per-machine *resident worker* that owns cache warmth (a
   second lifecycle to manage). OK to introduce that resident in P3 if measurements demand it, or do
   you want to see the numbers and decide separately?
4. **Security posture past "3 owned boxes / trusted Tailscale."** A2 scoped the substrate to trusted
   owned boxes. Containers-for-security implies possibly-less-trusted execution. For v1 (codex on
   your own incus on your own VM) this is still trusted — confirm v1 stays inside that trust
   boundary and untrusted-machine execution is a later, separately-scoped effort.
5. **Liveness set sign-off.** §4.2 enumerates L1–L9 as the deterministic simple-now set (extending
   your L1–L4). Confirm L5–L9 belong in v1, or trim to a smaller baseline.
