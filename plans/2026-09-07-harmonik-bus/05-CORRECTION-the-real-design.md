# CORRECTION — the bus already has a deep, decided design. Use it.

**Date:** 2026-09-07
**Status:** This supersedes the shallow parts of `01-base-plan.md` and `02-execution-plan.md`.
Those were built off the 8 KB seed (`00-seed-bus.md`) and a first research pass that read the
*derivative* (`plans/2026-07-21-platform-architecture/`) but never opened the *source*. The
operator was right to push back. This document is the corrected grounding, cited to the real
work.

## What the real work is, and where it lives

The "bus" is not a new idea to design. It was designed to the **reconciled-protobuf level**,
measured on the real three-box fleet, then **recast and locked with six operator decisions**.
Read these, in this order — they are authoritative over anything in this folder:

1. `../2026-07-15-agent-substrate-v2/ARCHITECTURE.md` — the reconciled design. Five independent
   subsystem designs collapsed into one. 19-RPC kernel, four channel types, roster, storage,
   transport verdict, all with `[MEASURED-HERE]` evidence (two of three boxes killed; the
   survivor still serves).
2. `../2026-07-15-agent-substrate-v2/ROADMAP.md` — the build order M0–M8, with kill criteria,
   line budgets, and a runnable demo per milestone.
3. `../2026-07-15-agent-substrate-v2/design/25-reconciled-proto/fleet/kernel/v1/kernel.proto`
   — the authoritative interface. Lint-clean, build-clean, cross-compiles `CGO_ENABLED=0` to
   the dgx today.
4. `../2026-07-21-p1-kernel-fabric/_plan.md` — the **current** framing: the standalone `fleet`
   design recast into an **in-proc `internal/kernel/`** inside harmonik. KERF-READY.
5. `../2026-07-21-platform-architecture/DECISIONS.md` — the **locked** operator decisions C1–C6.

## The name

It is the **kernel** (a.k.a. the **fabric**). The package is **`internal/kernel/`**. "Transport"
is *one subsystem* of it (`../2026-07-15-agent-substrate-v2/design/21-transport.md`) — the
6-method seam that hides the NATS-vs-TCP product choice. **`clean/transport` in `02` was wrong
on both counts: wrong name and wrong scope.** The operator's instinct ("transport seems way too
generic… that system could grow to be sizable") is exactly the finding — the kernel is
Transport + Lookup + Roster + Resources + Plugin-lifecycle, not a transport.

## The contract (locked, not to be re-invented)

From `p1-kernel-fabric/_plan.md` §2 — the in-proc Go interface (C6 recast of the 19-RPC proto):

- **`Transport`** — `Publish / Subscribe / Request / Serve / Respond` over **named, typed
  channels** carrying **opaque `[]byte`**. The kernel never parses a payload.
- **`Lookup`** — replicated map, **single-writer-per-key by construction** (no conflict, no
  resolver, no CAP argument). The one replicated thing.
- **`Roster`** — which boxes are alive; liveness computed **locally, never gossiped**.
- **`Resources`** — a reserved seam; **`State` (KV CAS + append-only journal)** is the only
  resource shipped. The operator's "Android Resource APIs" mental model (DECISIONS §"Resource
  APIs naming"). A later resource is a new method here, never a Transport change.
- **`Plugin`** lifecycle — `Describe / Start / Stop / Health`; the kernel hands the plugin a
  **namespace-scoped `Kernel`** at `Start`, so naming another plugin's storage is
  *unrepresentable*, not merely forbidden.

**Four channel types, locked** (substrate-v2 §4.1, proto `ChannelType`):
`PUBSUB` · `POINT_TO_POINT` · `REQUEST_REPLY` · `LOOKUP`. There is no fifth ("fanout" is
already covered — both readings ship).

## The single biggest correction: hub/spoke is CORE, not deferred

`02-execution-plan.md` deferred point-to-point / competing-consumers and proposed an interim
pull model. **That was wrong.** The real design already has it, designed *and measured*:

- **`CHANNEL_TYPE_POINT_TO_POINT` = NATS queue groups = "competing consumers: exactly one
  member of a named group gets each message."** Measured on the real fleet: *"10 jobs → dgx=5 +
  mini=5 = 10 (exactly-once across boxes, load balanced)"* (substrate-v2 §2 transport table).
- **The hub/spoke model is locked as C3** and specified in `p1-kernel-fabric/_plan.md` §3–4:
  **one `dispatch` plugin, `role: primary | worker`, deployed on every daemon.** The `primary`
  holds the queue and `Publish`es beads on a `dispatch.work` POINT_TO_POINT channel; workers
  `Subscribe(group="workers")` and pull one each. Status flows back on a `dispatch.status`
  PUBSUB channel. **Primary dies → restart it; you do not rebuild the network.** That is the
  operator's "central harmonik server holds the queues" with a leaderless transport underneath.

So "a hub exposes jobs and a spoke picks one up and runs with it" is not a gap in the three
verbs — it is `POINT_TO_POINT` + the dispatch plugin, and it was thought through in full. My
earlier deferral recommendation is withdrawn.

## The other corrections

| `01`/`02` said | The real design says | Source |
|---|---|---|
| Public surface = 3-verb `Bus` (Publish/Subscribe/Request) + `Service`/`Mount` | Full `Kernel` interface (Transport+Lookup+Roster+Resources+Plugin) | p1 §2 |
| Compile-time `Mount` of in-process `Service`s | In-proc **`Plugin`** interface (`Describe/Start/Stop/Health`) + namespace-scoped `Kernel` handle | C6 locked; p1 §2 |
| Lands in `clean/transport` inside the restructure module | `internal/kernel/`, dep-allowlisted, **out of `internal/daemon`** | p1 §5 |
| In-mem first, NATS "someday behind an interface" | In-mem kernel first as the **test double**; NATS-embed-vs-owned-TCP deferred behind the 6-method seam (Q-1) — but the transport verdict (embedded core NATS, JetStream off, full mesh) is already argued and measured | ROADMAP §6; substrate-v2 §6 |
| First Service = keeper | First real plugin = **comms** (M4 — "harmonik migrates here"); first consumer in the P-program = **dispatch** (P3) | ROADMAP M4; p1 §4 |
| Defer point-to-point + State + Roster "as one unit" | Roster and State are **day-one**; point-to-point is **day-one**; only real cross-machine mesh + LOOKUP-based dynamic worker join are deferred, each with a named trigger | p1 §4 |

## What is genuinely still deferred (with triggers) — this part `02` had roughly right

- **Cross-machine transport implementation** (NATS-embed vs owned TCP). In-memory kernel first;
  decide when the first cross-machine P3 run is scheduled (Q-1).
- **LOOKUP replicated implementation + dynamic worker registration.** The `Lookup` *interface*
  ships day one (in-mem trivial impl); the replicated cross-node version lands when dynamic
  worker join is scheduled (p1 §4). Resolves a real P1↔P3 contradiction: static-config wins per
  C3.
- **Live reload.** A casualty of the C6 in-proc choice — accepted (Q-4). In-proc plugins can't
  hot-swap; the substrate-v2 subprocess/go-plugin model that gave 4–9 ms reload was **dropped**
  by C6. Do not plan for it.

## The gate that comes before any of this: M0

`ROADMAP.md` M1 is not the first step. **M0 is: a real overnight macOS sleep test.** Three of
the five subsystem designs independently flagged "nobody has closed a real laptop lid" as the
#1 risk, and BRIEF §1 says the sleeping laptop is the fleet's *normal* state. Kill criterion:
if the mesh does not re-form on wake without a restart, the "laptop is a peer" premise is wrong
and the transport decision reopens. Half a day plus one night, before kernel code.

> Caveat carried from the source: M0 and the transport measurements target a **cross-machine
> fleet** (gb-mbp / dgx / gb-mac-mini). The P1 recast (C6) builds `internal/kernel` **in-proc
> first**, cross-machine later behind the seam. So M0 gates the *networked* kernel, not the
> in-memory one — the in-memory kernel and the dispatch plugin can be built and unit-tested
> (two in-mem kernels wired by an in-process transport double, p1 §5) before M0 is run. State
> that split cleanly in any execution plan; do not let it hide.

## How this sits in the bigger program (the framing question for the operator)

The two 2026-09-07 seed docs (bus + restructure) describe the **same system** as the
**P1 / P2 / P3 platform program** locked on 2026-07-21:

- **Bus (this track) = P1 (kernel/fabric)**, consumed by **P3 (distributed execution / the
  dispatch plugin)**.
- **Restructure (sibling track)** — decomposition of the daemon/core. NOTE: the `p2-extraction`
  program was SUPERSEDED on 2026-07-27 by the active `delete-and-rewrite` program; the restructure
  is the same *target* as delete-and-rewrite, and whether the segment-module strategy replaces /
  follows / runs alongside it is an open operator decision (see the restructure track's
  `05-relation-to-active-program.md`). What holds either way: "split `internal/core` LAST" — the
  restructure agent, delete-and-rewrite, and the old P2 all agree (60-fan-in giant leaf).
- **Priority-0 was Codex-first**, to conserve Claude tokens for exactly this platform work.

So the honest open question is **not** a design question — the design is done. It is a
**program question**: are these new folders a *restart / re-grounding* of P1/P2/P3 (which
appears to have stalled in the ~7 weeks since it was locked), or a fresh start that should
formally supersede it? Everything downstream — whether we revive the kerf works, re-ratify
C1–C6, re-check what P2 already extracted — hangs on that answer. That is the one thing to put
to the operator before spawning build crews.
