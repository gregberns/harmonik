# A4 — Current Coupling Survey (ground truth)

Scope: verify or qualify the operator's thesis that harmonik is "a large monolith with too
much layer-to-layer hard-coding" whose tight coupling "has burned us." Method: read the Go
package graph, imports, interfaces, and the enforced depguard matrix. Daemon/comms are DOWN;
this is a static read. Every claim is grounded in `pkg` or `file:line` + import evidence.

---

## Bottom line (5 sentences)

Harmonik is **not** an undifferentiated monolith: it is 60+ small `internal/` packages with an
**enforced** component-boundary matrix (`.golangci.yml` depguard, ~230 lines of allow/deny edges),
and the two components the operator worried about most — the **keeper** and the **agent harnesses**
— are the *cleanest* seams in the tree, not the dirtiest. The genuine coupling problem is
**two god packages**: `internal/core` (501 files, a shared-kernel "types" package that fan-in 35
other packages import) and `internal/daemon` (641 files, the composition-root/coordinator that
imports 31 internal packages and *contains the concrete harness implementations, the queue wiring,
the ssh transport, the crew wiring, and the DOT run-loop all in one package*). So the "layer-to-layer
hard-coding" the operator feels is real but it is **concentrated in `daemon`**, where every seam is
wired together, rather than smeared across the whole system. The harness↔queue/bead/DOT/crew coupling
— the operator's sharpest concern — is a **CLEAN SEAM at the interface level** (`handler`/`handlercontract`
import only `core`, never queue/beads/crew/DOT), but the concrete claude/codex/pi harnesses live
*inside* `daemon`, so the seam is defined-but-not-extracted, which is why codex churn thrashes `daemon`.
Net: the assets for a layered redesign already exist (Substrate, Harness registry, CommandRunner,
eventbus, queue API, workers registry); the work is to **extract implementations out of `daemon` and
break up `core`**, not to invent seams from scratch.

---

## Module structure + god packages

Layout: one Go module (`github.com/gregberns/harmonik`, go 1.25). `cmd/` holds the CLI
(`cmd/harmonik`) plus five twin-harness shims (`harmonik-twin-{claude,codex,pi,generic,session}`).
`internal/` holds **60+ packages**. Rough file counts (go files, incl. tests):

| pkg | files | fan-in (pkgs that import it) | fan-out (internal pkgs it imports) | role |
|---|---|---|---|---|
| `internal/daemon` | 641 | 8 | **31** | composition root / coordinator — GOD |
| `internal/core` | 501 | **35** | 2 | shared kernel of domain/event/payload types — GOD (leaf) |
| `internal/specaudit` | 133 | — | — | spec-drift tooling |
| `internal/lifecycle` | 130 | 11 | 9 | tmux/ssh spawn + runner |
| `internal/workspace` | 121 | — | — | worktree management |
| `internal/keeper` | 94 | 5 | 7 | context-fill watcher |
| `internal/handlercontract` | 70 | 15 | 1 (`core` only) | **harness contract — CLEAN** |
| `internal/queue` | 54 | 8 | 2 (`core`,self) | queue engine — CLEAN leaf |
| `internal/handler` | 44 | 15 | 2 (`handlercontract`,`lifecycle`) | **spawn loop — CLEAN** |
| `internal/eventbus` | 15 | 14 | 1 (`core` only) | the event/comms bus — CLEAN leaf |
| `internal/substrate` | 7 | 14 | 0 (stdlib only) | **test-seam ports (Clock/Effector) — CLEAN leaf** |

Shape: **not** layered top-to-bottom, and **not** "everything imports one central coordinator."
It is a **hub-and-kernel**: a shared kernel `core` at the bottom (huge fan-in, tiny fan-out), a
constellation of clean single-purpose leaf packages in the middle (queue, eventbus, substrate,
handlercontract, handler, crew, presence, workers), and **one coordinator `daemon` at the top**
(fan-out 31) that wires them together. `daemon`'s fan-in is only 8 — almost nothing depends *on*
daemon, which is correct for a top-level composition root, and several sub-packages
(`daemon/router`, `orchestrator`, `policy`, `socketrouter`) are explicitly denied from importing
`daemon` back (`.golangci.yml:217-222`, `:189`). The pain isn't a cyclic hairball; it's that
`daemon` is a 641-file kitchen sink and `core` is a 501-file everything-types bag.

Evidence: fan-in/out computed by `grep -rl`/`grep -rho` over `gregberns/harmonik/internal/*`
import paths; `daemon`'s 31 imports listed from `internal/daemon/*.go` = agentlaunch, agentmanifest,
branching, brcli, core, crew, dashboard, digest, eventbus, handler, handlercontract, hook, keeper,
keepertwin, lifecycle, mergeq, orchestrator, policy, presence, queue, release, run, runexec,
schedule, sentinel, sessiondata, substrate, twinparity, workers, workflow.

---

## Keeper coupling — **CLEAN-SEAM-EXISTS** (verdict: operator's worry is unfounded)

The operator suspected the keeper has bindings to the queue/comms/daemon it "shouldn't have."
It does not. Keeper's production imports: `core`, `substrate` (the *test-seam clock* package —
`ClockPort`/`SystemClock`, injected for deterministic replay, `internal/keeper/cycle.go:14,94`),
`presence`, `eventbus` (2 refs), `dashboard`, `digest`. It **does not import `queue` or `daemon`
at all** — and that boundary is **enforced by depguard**:

```
# .golangci.yml:172-190  (keeper rule)
keeper:
  files: ["**/internal/keeper/**"]
  allow: [ core, eventbus, presence, substrate, self ]
  deny:
    - internal/daemon    "keeper MUST NOT import daemon (session-keeper spec hk-ekap1)"
    - internal/workloop  "keeper MUST NOT import workloop"
```

The only textual `internal/daemon` matches inside keeper are **comments** explaining the ban
(`internal/keeper/dashboardnag.go:10-11`: "internal/keeper importing internal/daemon is banned by
the depguard"; `injector.go:144` mirrors `daemon/pasteinject.go` behaviorally, not by import). The
keeper reaches the dashboard/digest **directly** rather than through daemon, precisely to stay off
the daemon edge. Its `eventbus` touch (2 refs, `internal/watch`... no — keeper: `presence`+event
emission) is the shared bus, not a queue binding. Verdict: **the keeper is a clean, depguard-fenced
leaf-consumer; the queue/comms coupling the operator feared is absent.**

---

## Watchdog / watch coupling — **CLEAN-SEAM-EXISTS**

`internal/watch` (the always-on triage session's Go side) production imports: `core`
(`escalation.go:21`, `ledger.go:30`, `markers.go:19`), `agentmanifest` (`markers.go:18`),
`eventbus` (`ledger.go:31`). Fan-out 4, no `queue`, no `daemon`. It reads the shared `eventbus`
ledger and classifies — exactly the "consume the bus, record to ledger, escalate" contract. No
queue/daemon binding. (Note: the ops-monitor/worker-health side is a separate concern — see
Transport below — carried by `internal/workers` payloads, not by `watch`.) Verdict: clean bus
consumer.

---

## Harness ↔ queue/bead/DOT/crew coupling — **MIXED (interface CLEAN; implementations trapped in `daemon`)** — the key one

This is the operator's sharpest concern and the answer is nuanced, so both halves matter.

**The seam is genuinely clean at the contract level.** The harness abstraction is two packages that
know *nothing* about queues, beads, DOT, or crews:

- `internal/handler` imports **only** `handlercontract` + `lifecycle` + stdlib
  (`internal/handler/handler.go` import block). No queue, beads, crew, DOT.
- `internal/handlercontract` imports **only** `core` (fan-out 1). No queue, beads, crew, DOT.
- The agent-harness `Substrate` interface (`internal/handler/substrate.go:30`) has **one method**,
  `SpawnWindow(ctx, SubstrateSpawn) (SubstrateSession, error)` — pure subprocess hosting. Its `Cwd`
  field is documented as "typically the bead worktree path" (`substrate.go:56`) but it is just a
  `string`; the harness never learns it is a bead.
- The `Harness` interface (`internal/handlercontract/harness.go:202`) is purely per-agent turn
  lifecycle: `AgentType / LaunchSpec / Seed / Retask / Teardown / DetectReady / SessionIDPolicy /
  Completion / NewSessionIDInterceptor`. No queue/bead/crew/DOT surface anywhere in it.
- Harnesses register through a **pluggable registry** (`handlercontract.HarnessRegistry`,
  `harnessregistry.go:50`, keyed by `core.AgentType`, seal-once) and an `AdapterRegistry`
  (`adapterregistry_hc012.go:41`) — a clean plugin seam.
- The only `DOT`/`cascade`/`bead` tokens found inside `handler`/`handlercontract` production code are
  **comments / spec refs** (`handler/runtime.go:22` "terminal outcome escapes to parent cascade";
  `handler/handler.go:112` "Beads: hk-x882o (DOT consolidate)"). No import, no call. The one
  `handler`→`brcli` hit is a **comment in a test** (`bi004_handler_gate_test.go:55`).

**But the concrete harnesses live inside the god `daemon` package.** The implementations of the
`Harness` interface are `internal/daemon/claudeharness.go`, `internal/daemon/codexharness.go`,
`internal/daemon/piharness.go`, wired by `internal/daemon/harnessregistry.go`, and driven by
`internal/daemon/workloop.go` / `reviewloop.go` — all in `daemon`, alongside the queue wiring, the
DOT run-loop, the crew wiring, and the ssh transport. So `daemon` is where harness-meets-queue-
meets-bead-meets-DOT-meets-crew actually happens, by design (`internal/handler/substrate.go:21-29`:
"the interface through which the **daemon composition root** injects a subprocess-hosting mechanism").

**Consequence:** the codex churn thrashes `daemon` not because the seam is dirty but because the
codex *implementation* was never extracted out of the coordinator — every codex tweak edits a file
that sits in the same 641-file package as the queue, DOT, and crew logic. Verdict: **MIXED — the
harness *contract* is a clean seam (CLEAN-SEAM-EXISTS); the harness *implementations* are
co-located in `daemon`, which is where the felt coupling lives (TIGHT).** The remedy implied is
extraction (move claude/codex/pi into their own packages behind the existing registry), not
seam invention.

---

## Existing clean seams (assets for a layered redesign)

These already exist and are the raw material for a layered platform:

1. **`handlercontract.Harness` + `HarnessRegistry` + `AdapterRegistry`** — pluggable per-agent
   harness plugin system, `core`-only deps (`harness.go:202`, `harnessregistry.go:50`,
   `adapterregistry_hc012.go:41`). The intended extraction point for claude/codex/pi.
2. **`handler.Substrate` (`substrate.go:30`)** — one-method subprocess-hosting seam; concrete impl
   is `daemon/tmuxsubstrate.go`, kept out of `handler` to satisfy depguard.
3. **`internal/substrate` test-seam ports** — `ClockPort`/`Ticker` (`clock.go:10,21`),
   `EventSource`/`Effector` (`seam.go:7,13`), `ReplayCodec` (`replay.go:59`). stdlib-only leaf
   (fan-out 0), the determinism/replay seam; depguard forces driver waits through
   `substrate.ClockPort` (`.golangci.yml:101-111`).
4. **`lifecycle/tmux.CommandRunner` (`runner.go:16`)** — local-vs-ssh execution seam:
   "tests and future **ssh transports** supply their own implementation." `LocalRunner` is prod;
   ssh is threaded in for remote workers.
5. **`internal/eventbus` (`busimpl.go`)** — the event/comms bus, `core`-only leaf, fan-in 14. The
   `harmonik comms` CLI (`cmd/harmonik/comms.go`) and the watch ledger sit on top of it.
6. **`internal/queue` API (`append.go`, `persistence.go`, `rpc.go`)** — the queue engine as a
   `core`-only leaf (fan-out 2) with a typed RPC surface (`QueueSetter`/`LockedQueueView`/
   `MutationLocker`, `rpc.go:57-84`). `daemon` consumes it; it does not reach back.
7. **`internal/crew` registry** — leaf durable-state (fan-out 1), depguard-denied from importing
   `daemon` (`.golangci.yml:157-162`, "crew registry is a leaf read by daemon and CLI").
8. **The depguard matrix itself (`.golangci.yml:112-` )** — an *enforced* component contract:
   PL-INV-002 bans any LLM SDK from the daemon's transitive closure
   (`:146-150` — anthropic/openai/etc denied), keeper/crew/router/orchestrator/policy each have
   allow/deny edges. This is a designed layering, machine-checked in CI.

---

## Transport / addressing today

There **is** an inter-machine layer, but it is early and single-primary-worker shaped:

- **`internal/workers`** is the worker/machine registry: `Registry` (`registry.go:10`) wraps a
  `Config` of `Worker`s (`workers.go:37`) with slot tracking + live-disable ("remote-substrate B5").
  It emits `WorkerReportPayload` (`telemetry.go:40`), `WorkerOfflinePayload` (`offline.go:29`),
  `WorkerUnhealthyPayload` (`health.go:32`), `WorkerTunnelFailedPayload` (`tunnelfailed.go:31`) —
  a real health/telemetry/tunnel surface. **But** `PrimaryWorkerIndex` always returns `0`
  (`registry.go:18-24`): there is exactly one live remote worker — "the single source of truth for
  which worker is live." So addressing is **one-primary-remote**, not a multi-node fabric.
- **`lifecycle/tmux.CommandRunner`** (above) is the execution transport: local `exec.CommandContext`
  today, ssh-quoted remote runner threaded in for the worker (ssh-quote tests at
  `runner_ssh_quote_hkfxy9_test.go`, real-argv at `runner_real_argv_hkfxy9_test.go`).
- **`daemon/workloop.go`** carries the remote run path: reverse-tunnel readiness gating
  (`workloop.go:422-430`), per-worker cold-start concurrency bound of 3 (`:422`),
  `RemoteAgentReadyTimeout` (`:523-524`), a `CommandRunner` "threaded into the DOT run path for
  remote-aware" spawns (`:757-764`).

Assessment: the transport is **structured, not ad hoc** — there is a typed runner seam, a worker
registry, tunnel/health payloads, and remote-aware timeouts. It is, however, **narrow**: a single
primary ssh worker, tunnel-based, with the remote orchestration logic living inside `daemon/workloop`.
There is no general machine-to-machine addressing/routing fabric — it is "local, or the one remote
worker over ssh+reverse-tunnel."

---

## Classification summary

| Coupling in question | Verdict | Key evidence |
|---|---|---|
| Overall structure | **MIXED** | 60+ fenced packages + enforced depguard, BUT `core` (501f, fan-in 35) and `daemon` (641f, fan-out 31) are god packages |
| Keeper ↔ queue/comms/daemon | **CLEAN-SEAM-EXISTS** | keeper imports core/substrate/presence/eventbus/dashboard/digest; NO queue/daemon; depguard bans daemon+workloop (`.golangci.yml:172-190`) |
| Watch ↔ queue/comms | **CLEAN-SEAM-EXISTS** | `watch` imports core/agentmanifest/eventbus only; bus consumer, no queue/daemon |
| Harness ↔ queue/bead/DOT/crew | **MIXED** | contract clean: `handler`/`handlercontract` import only core/lifecycle, `Harness`/`Substrate` ifaces have zero queue/bead/crew/DOT surface; BUT claude/codex/pi impls live in `internal/daemon` alongside queue+DOT+crew wiring → felt coupling is real, concentrated in daemon |
| Transport / addressing | **MIXED** | typed seams exist (workers.Registry, tmux.CommandRunner, tunnel/health payloads) BUT single-primary-worker, remote logic embedded in `daemon/workloop` |
| Existing seams as assets | **CLEAN** | Harness/AdapterRegistry, Substrate, substrate ClockPort, CommandRunner, eventbus, queue RPC, crew registry, depguard matrix |

**One-line thesis check:** the operator is *right that coupling has burned us* and *wrong that it is a
formless monolith* — the seams exist and are CI-enforced; the damage is that the coordinator
`daemon` never had its harness/transport/DOT/crew implementations extracted out, and `core` is an
undifferentiated shared kernel. Fix = extraction + kernel-splitting, not seam invention.
