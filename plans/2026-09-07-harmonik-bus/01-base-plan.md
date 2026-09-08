# Harmonik Bus — Base Plan

**Date:** 2026-09-07 · **Track:** 2 of 2 (sibling: `../2026-09-07-harmonik-restructure/`)
**Status:** Grounding plan. Consolidates prior deep planning; recommends a first slice. No code.

> This plan does not invent a bus. A large amount of design already exists in the repo, and the
> operator remembers it ("the plan actually went pretty deep"). The job here is to find that work,
> line it up against the seed idea (`00-seed-bus.md`) and against the code that runs today, then
> state one reconciled design and a minimal first slice. Every non-obvious claim cites a plan file
> or a package.

---

## 0. TL;DR

- The seed's `Bus`/`Service`/`Mount` idea is **the same architecture** the platform program already
  designed at depth in `plans/2026-07-21-p1-kernel-fabric/_plan.md` and
  `plans/2026-07-15-agent-substrate-v2/` (extracted in
  `plans/2026-07-21-platform-architecture/research/A2-substrate-v2-architecture.md`). The seed uses
  three verbs (Publish/Subscribe/Request); the prior work uses four channel *types* (pubsub,
  point-to-point, request-reply, lookup) plus a roster and a box-local state seam. The seed is the
  smaller, right-first surface; the prior work is the full contract the seed grows into.
- Today harmonik has **no bus of this shape**. It has an append-only event log (`internal/eventbus`),
  a presence view (`internal/presence`), a dispatch path (`internal/dispatch`), a queue, and a
  daemon-mediated `harmonik comms` message surface. These are in-process or daemon-local. None is a
  named-subject transport a tool mounts a `Service` on.
- The prior program locked six decisions (`plans/2026-07-21-platform-architecture/DECISIONS.md`,
  C1–C6). They resolve the biggest questions the seed leaves open: control-plane only (git stays the
  artifact plane), kernel-now built in parallel with its first consumer, transport leaderless /
  dispatch central-as-an-application, and **plugin↔kernel is an in-proc Go interface, not a separate
  binary** (C6). That last one is the seed's "never use Go's `plugin` package" — same conclusion.
- **Build order:** the interfaces and the in-memory bus first, `Mount`, one real `Service` (keeper),
  and the first cross-tool test on one in-memory bus. Defer NATS, the roster, the replicated lookup,
  and runtime registration until the in-memory seam carries real traffic.
- **Biggest open question:** the seed's flat `Bus` (three verbs) versus the prior work's richer
  channel-typed kernel (pubsub / point-to-point / request-reply / lookup + roster + state). Start
  with the seed's three verbs, but shape the subject and header conventions so point-to-point work
  distribution and a later roster are additive, not a rewrite. Details in §6.

---

## 1. What already exists in code

> This section is an honest map of today's coordination code. The short version: harmonik already
> has one thing with real pub/sub semantics (`internal/eventbus`), but no `Bus` interface with
> dynamic subjects, no first-class request/reply, and no `Service`/`Mount` plugin model. The dynamic
> fan-out and the request/response wire that *feel* bus-like live one layer up in the daemon, reached
> over a single Unix socket, not in a reusable transport. **There is no NATS anywhere in the repo**
> (grep of `*.go`, `go.mod`, `go.sum` is empty).

### `internal/eventbus` — a durable in-process event log with a sealed subscriber set

`internal/eventbus` is the strongest existing seed of a bus, and it is still far from one. Its
`EventBus` interface (`eventbus.go`, anchored to `specs/event-model.md §6.1`) is `Emit`,
`EmitWithRunID`, `Subscribe`, `Seal`, `ReplayFrom`, `DeadLetterReplay`, `Drain`. Extension today is
**Go type-assertion to optional capability interfaces**, not a mountable service: `RunDrainer`,
`CommsMessageEmitter` (`EmitAgentMessage`), `CommsPresenceEmitter` (`EmitAgentPresence`),
`TypedEmitter`. It is durable and in-process: every emit mints a UUIDv7 `event_id`, redacts secret
fields, appends to `.harmonik/events/events.jsonl` and fsyncs F-class events
(`fsyncBoundaryEventTypes`, `jsonlwriter.go`), then fans out to matching subscribers. Consumers come
in three `ConsumerClass` kinds: synchronous (runs on the emitter's goroutine, can fail the emit),
asynchronous, and observer (dispatched off the critical path, failures go to a dead-letter log).

Measured against the seed's three verbs:

- **Publish — present.** `Emit*` is a real publish surface with id-minting, redaction, and durable
  append.
- **Subscribe — present but static.** `Subscribe` is **boot-only and forbidden after `Seal()`**
  (EV-009). It is a fixed subscriber set decided at startup, and it filters by typed
  `core.EventPattern` (event-type match), **not** by a dotted subject/topic string. The dynamic,
  per-connection subscribe that a general bus needs lives one layer up in `daemon.SubscribeHub` +
  `CursorStore`, not in the bus interface.
- **Request/reply — absent.** There is no correlation-id request primitive. The nearest thing is a
  synchronous consumer whose error the emitter observes — not reply routing.

So `eventbus` is a durable in-process pub/sub keyed by event type with a sealed subscriber set. Read
it two ways for the bus: its **journal** is exactly the durability tool the prior kernel design hands
to plugins (P1 `Resources.State().JournalAppend/Read`), and its **pub/sub** is the closest existing
kin to the seed's `Bus` — but it lacks dynamic subscribe, subject-string routing, and request/reply.

### `internal/presence`, `internal/dispatch`, `internal/dispatchstore`, `internal/queue`, `internal/mergeq`

- **`internal/presence`** — a **pure projection** (leaf; imports only `core` + `eventbus`).
  `ComputeRegistry(eventsPath)` folds `agent_presence` events into a liveness view
  (`Record`/`State`, `IsOnline`/`IsStale`/`IsOffline`); it also owns the hitl-decisions projection
  and the orphan reaper. This is the seed's future `Roster` in embryo, but it is a file-read
  projection, not a fabric-replicated roster. It maps onto the prior work's `Roster` primitive
  (P1 §2 `Roster.List/Watch`), deferred out of the first bus slice.
- **`internal/dispatch`** — a pure **durable value contract** for one queue-to-run handoff: the
  `Intent` type plus `Phase`/`Binding` variants and classifier/validator helpers
  (`ClassifyBeadRecord`, `ValidateParentCommit`). No I/O, no network. Value types the daemon/queue
  consume.
- **`internal/dispatchstore`** — `Store` (`New(projectDir)`): atomic filesystem persistence of
  `dispatch.Intent` records and session receipts. Local disk only.
- **`internal/queue`** — the daemon-owned **execution-plan data model** (`specs/queue-model.md`);
  many RECORD types are not-yet-shipped. Today it is state-transition + completion-recovery helpers
  over local state; its `rpc.go` frames requests but **delivery is via the daemon socket owned
  elsewhere**, not this package. This is the central "one brain decides what runs" plane (locked,
  `01-p2p-comms` concern B). The bus does not replace it; a future `queue` service would expose it on
  subjects (`queue.task.enqueue`, per the seed).
- **`internal/mergeq`** — a **single-writer FIFO exclusion domain**, not a bus: `Queue`/`Submit`
  serialize merge-critical sections, and the "payload" is a `critical func(ctx) error` closure. This
  is the git artifact plane, ruled **out of the fabric** by C1 (git stays the artifact plane; the bus
  carries control/events/addresses, never diffs/repos). It stays as-is.

None of these five crosses a machine boundary. The one existing cross-process boundary in the whole
system is the daemon's Unix socket.

### `harmonik comms` — the agent-to-agent surface (a façade over eventbus, across the daemon socket)

`harmonik comms` is how agents talk **now** (`cmd/harmonik/comms.go` `runCommsSubcommand`; daemon
side `internal/daemon/agent_message.go`, `commsrecvhandler_nnwaa.go`, `subscribe.go`). Verbs: send /
recv / log / join / leave / who. It is best understood as a thin agent-facing **façade over
`eventbus`**, and its transport is a **hybrid** that matters for the bus design:

1. **`send`, `recv`, `join`, `leave`, `recv --follow`** dial the daemon's Unix socket
   `.harmonik/daemon.sock` and issue hand-rolled JSON ops (`{"op":"comms-send"}`, `comms-recv`,
   `subscribe`). The daemon turns these into bus emits via the `CommsMessageEmitter` /
   `CommsPresenceEmitter` capability interfaces. If the socket is down, `send` exits 17. **This
   socket request/response (`{"op":...}` → `{"ok":...,"result":...}`) is the only request/reply in
   the system, and it is ad-hoc per verb — not a bus-level `Request`.**
2. **`log` and `who` are daemon-free** — they read `events.jsonl` directly (`eventbus.ScanAfter`,
   `presence.ComputeRegistry`).

Storage is the single append-only JSONL log; messages are `agent_message` events (durability class
**F**, always fsync — durable before `send` returns), presence beats are `agent_presence` (class
**O**, lossy-by-design, TTL reconciles). Delivery is **at-least-once** (N3): the per-agent
`CursorStore` advances to the last delivered `event_id` *after* read, so a crash between read and
advance redelivers — which is why the recipient must **dedupe on `event_id`** (normative,
`agent-comms` skill; `CommsRecvMessage.EventID`). The dynamic per-connection fan-out lives in
`daemon.SubscribeHub`, not in the bus.

Two facts are load-bearing for this plan. First, comms is **daemon-mediated and daemon-local** — one
node's log; crew on another box cannot reach the captain unless the bus spans nodes, the undesigned
piece `01-p2p-comms` §C names. Second, in the prior kernel design **comms is the archetypal plugin,
not the kernel** (A2 §"Four shipping plugins") — it builds its own at-least-once + dedupe from the
kernel's four durability tools. So today's `comms` is exactly a `Service` waiting for a `Bus` under
it, and the daemon socket + `SubscribeHub` + `CursorStore` is the ad-hoc transport a real bus would
replace.

### Bottom line for §1

There is **no bus** in the seed's sense. There is: a durable in-process event log with a sealed
subscriber set (`eventbus`), projections over it (`presence`, `comms log`/`who`), durable value/state
models (`dispatch`, `dispatchstore`, `queue`), a git merge-exclusion queue (`mergeq`), and an
agent-messaging façade reached over one daemon Unix socket (`comms`). Against a real `Bus` +
`Service`/`Mount`: **Publish** exists; **Subscribe** exists but is static/sealed and type-keyed, not
dynamic and subject-routed; **Request/reply** is absent from the bus and hand-rolled per verb at the
socket; **Service/Mount** is absent (extension is compile-time type-assertion); **cross-network** is
absent (in-process + one Unix socket, no NATS). The reusable foundations to build *on*, not around:
the durable event-log substrate + UUIDv7 `event_id` + F/O durability classes (`eventbus`), the
dynamic fan-out + cursor machinery (`daemon.SubscribeHub`, `CursorStore`), and the at-least-once +
client-dedupe contract (`agent-comms`). The distance to the seed's `Bus`/`Service`/`Mount` is: define
the two interfaces, add a subject router with dynamic subscribe and real request/reply (the in-memory
bus), and re-express `comms` (then `dispatch`) as a `Service` mounted on it — the restructure's port
loop, with today's daemon-socket RPC and `SubscribeHub` retired into the bus.

---

## 2. What was already planned (consolidation)

The operator's memory is correct: the design went deep. It lives in four layered artifacts, oldest
to newest. Read them in this order; each supersedes the framing of the one before it.

### 2.1 `plans/2026-06-30-distributed-fleet/` — the hub/spoke + peer-comms sketch

This is where hub/spoke and "who's online" first became a decomposition rather than a vibe.
`01-p2p-comms/README.md` makes the **single most important move**: it splits the problem into three
concerns that must never be one blob (its table):

- **A. Membership + presence + health** — the "zookeeper-like" roster. Replicated so every node can
  *read* the whole fleet. Freshness: seconds (heartbeat), graded liveness alive→suspect→dead.
- **B. Scheduling** — which bead goes to which node. **Central. Locked.** The daemon owns the queue.
- **C. Transport** — moving payloads (the comms bus, run branches, hook events) between nodes.
  Per-message; the bus's own delivery guarantee (N3 at-least-once + `event_id` dedupe).

Its reconciliation of the operator's "peer-to-peer" language with the locked central-queue decision
is the sentence the whole program keeps: **peer-to-peer for membership + comms transport (A & C),
central for scheduling (B).** The join flow (a lead box SSHes in, ships a build, the two exchange
node names, the roster becomes the *output* of a join protocol rather than a hand-edited
`workers.yaml`) is described in §B, and hub-relayed comms (C1) is picked over peer replication (C2)
as the first cut. `PLANNING-SUMMARY.md` sequences it: lift the single-worker cap first, then the
resident node-agent + roster, then the comms-bus relay.

**What this contributes to the bus:** the three-concern split, the hub/spoke-with-central-queue
reconciliation, and the insight that distributing comms is "deliver the same events to more stores,"
not new semantics — because N3 + `event_id` dedupe already make it survivable.

### 2.2 `plans/2026-07-15-agent-substrate-v2/` — the fleetd kernel (the deepest contract)

This is the richest prior artifact and the one closest to a finished design. It is extracted in
`plans/2026-07-21-platform-architecture/research/A2-substrate-v2-architecture.md`, which read the
`ARCHITECTURE.md`, `BRIEF.md`, `ROADMAP.md`, and the reconciled `kernel.proto` / `plugin.proto` in
full. The design:

- **A kernel/plugin split with one hard line:** "above knows what an agent is; below has never heard
  of one." Enforced not by convention but by `payload` being `bytes`, never protobuf `Any` — the
  kernel *cannot* parse a payload. Plus a dependency-allowlist test and a vocabulary/boundary test.
- **The kernel = transport + addressing + box-local storage**, expressed originally as 19 gRPC RPCs.
  **Channels have a name and a type**, and there are exactly four types: `PUBSUB` (broadcast),
  `POINT_TO_POINT` (competing consumers / work queue), `REQUEST_REPLY` (one question one answer),
  `LOOKUP` (a replicated map — the *only* replicated thing).
- **LOOKUP + Roster are the identity/address system.** Roster liveness is computed locally, never
  gossiped. LOOKUP is a single-writer-per-key replicated address book.
- **Delivery is at-most-once, always.** The kernel never queues, retries, or redelivers. Durability
  is pushed up to plugins, which get four tools: a local journal, a stamped `message_id`, an
  interest hint, and a liveness signal. (This is why today's `eventbus` journal and `comms`
  `event_id` dedupe are exactly the right primitives to keep.)
- **Plugins are separate processes** speaking the same service, declaring a manifest (namespace +
  channels + interests), holding no in-memory state (state lives in kernel storage), and
  hot-reloadable via `hashicorp/go-plugin`.
- **Transport is embedded NATS**, hidden behind a 6-method Go interface in one file — "NATS is an
  implementation detail; the 19-RPC API is what gets approved."

The extraction is explicit about what to **keep** (the kernel/plugin split, the channel abstraction,
LOOKUP+roster, box-local storage, at-most-once + four durability tools, transport-behind-an-interface,
the boundary/kill-criteria discipline, the refusal list) and what to **drop** (the "centralize all
agent learning, make it searchable" premise, the logtail/search milestone). The operator rejected
the *premise*, not the *architecture*.

### 2.3 `plans/2026-07-21-platform-architecture/` — the decisions that lock the shape

The platform-architecture plan took the substrate-v2 architecture, dropped its premise, and locked
six decisions in `DECISIONS.md`. These are the load-bearing choices the bus must honor:

- **C1 — Control-plane only.** Git stays the artifact plane. The fabric carries control, events, and
  addresses — never code, diffs, or repos. Enforced later as a hard low payload ceiling (~256 KB) so
  streaming a diff *fails* rather than merely breaking a rule.
- **C2 — Build the kernel now, in parallel with its first consumer (P3), using P3 as the test bed.**
  Governing rule: **no hacks in either P1 or P3.** No "temporary pipe, migrate later" — that is
  exactly how the ssh model happened.
- **C3 — Transport is leaderless; work-dispatch is one central owner, but only as an application on
  top.** First cut is static config: configure one instance `primary`/hub, the rest `worker`/spoke;
  workers' config just names the primary. Dynamic discovery is a later capability.
- **C5 — Dispatch + liveness kept simple now**, but with one non-skippable minimum: a dead or hung
  container's in-flight bead must not strand as `in_progress` — heartbeat/timeout marks it failed and
  requeues. This is a *plugin* concern, not the kernel's.
- **C6 — The plugin boundary is an in-proc Go interface, not a separate binary / gRPC.** This is the
  operator's own steer: "a plugin is going to be in-proc… just a library in this system with
  particular interfaces." **The kernel is the network-aware layer; plugins are always local to their
  daemon and reach other machines *through* the kernel's transport.** plugin↔kernel = Go interface
  in-proc; kernel↔kernel = network. This is the same conclusion the seed reaches with "never use Go's
  `plugin` package" — extensibility is either compile-time import or a separate process speaking the
  protocol, both statically compiled.
- Plus the four **platform principles** (`DECISIONS.md` §Building principles): the kernel carries
  signals not meaning; a healthy kernel stops growing; the fabric moves references not cargo; code
  lives in the component that owns it and the composition root wires but does not accumulate.

The one still-open operator question in this whole program is **ratifying the kill-criteria /
boundary-test guardrail** as shared, pre-agreed ground rules (dep-allowlist test, vocabulary test,
`[]byte`-payload rule, kernel-size tripwire, "two plugins needing a new kernel verb = stop").

### 2.4 `plans/2026-07-21-p1-kernel-fabric/_plan.md` — the recast, in-proc kernel interface

P1 is the reviewed, kerf-ready recast of substrate-v2 for the in-proc (C6) world. It is the most
directly reusable document for the bus because it is already Go interfaces, not proto. It defines:

- **`Transport`** — `Publish` (with a `groupKey` for point-to-point routing), `Subscribe(pattern,
  group)`, `Request`, `Serve`, `Respond`. Streaming RPCs become **Go channels + a cancel func**.
- **`Lookup`** and **`Roster`** — the addressing/identity surface (deferred out of the first slice).
- **`Resources.State()`** — the reserved resource seam: box-local KV (with CAS via `ifRevision`) +
  append-only journal. State is the only resource shipped; Blob/Secrets are reserved, not built.
- **`Kernel`** = Transport + Lookup + Roster + Resources + Info, handed to a plugin already
  **namespace-scoped**, so naming another plugin's storage is *unrepresentable*, not merely forbidden.
- **`Plugin`** = `Describe/Start/Stop/Health` with a static `Manifest` (namespace + channels +
  interests). A conflicting channel *type* for a name is rejected **at registration**.
- **Package home `internal/kernel/`**, dep-allowlisted, outside `internal/daemon`. Two
  implementations behind one interface **from day one**: an in-memory kernel (the default test
  double) and a networked kernel (transport product deferred). Two in-memory kernels wired together
  by an in-process transport double prove cross-node behavior **in a unit test, no network**.

P1 §4 gives the exact minimal slice its first consumer (P3 execution dispatch) needs, and what is
**deferred**: LOOKUP's replicated implementation and any consumer of it, and the real cross-machine
mesh transport. The `Lookup` *interface* ships day one; the replicated *implementation* lands when
dynamic worker join is scheduled.

`plans/2026-07-21-p3-distributed-execution/_plan.md` is the first consumer: it maps container-based
bead execution onto exactly these primitives (a `POINT_TO_POINT` work channel, a `PUBSUB` status
channel, `REQUEST_REPLY` kickoff/ack, `Roster` for worker liveness, `State` for the durable
running-bead registry) and proves the boundary by needing **no new kernel verb**. Its L1–L9 liveness
table is the C5 "simple-now" control set.

The kerf work `kernel-fabric` (`.kerf/works/kernel-fabric/`) opened on this plan but only reached the
problem-space pass; `01-problem-space.md` is a clean consolidation and its "already decided" table is
the authoritative list of closed questions. It did not produce a spec draft or tasks, so **P1
`_plan.md` remains the deepest design artifact**, and no bus code was written.

### 2.5 One-paragraph synthesis of the prior planning

The prior program already designed the seed's bus, at more depth than the seed, and named it the
**kernel/fabric**. It chose: a domain-blind transport of opaque bytes over named+typed channels;
four channel types (the seed's three verbs plus point-to-point work distribution and a replicated
lookup); at-most-once delivery with durability pushed up to plugins built from a journal + message-id
+ interest + liveness; an in-proc Go plugin interface (not `.so`, not a separate binary for the
plugin↔kernel hop); a leaderless transport with a central *queue-as-application*; and embedded NATS
behind a swappable interface for the eventual cross-machine hop. It locked C1–C6 to hold that shape
and left exactly one operator question open (ratify the guardrail). The seed idea and this program
are the same idea; the program is the grown-up version.

---

## 3. The reconciled bus design

This section merges the seed's `Bus`/`Service`/`Mount` with the prior kernel design and the existing
code. Where they agree, it states the decision. Where they differ, it names the conflict and
recommends.

### 3.1 The two interfaces — AGREE (adopt the seed's names, keep the kernel's shape underneath)

Seed `Bus` (`Publish`/`Subscribe`/`Request`) and P1 `Transport`
(`Publish`/`Subscribe`/`Request`/`Serve`/`Respond`) are the same transport. **Decision:** adopt the
seed's three-verb `Bus` as the public surface tools depend on, because it is the smallest sufficient
contract and it is what a tool actually calls. Treat `Serve`/`Respond` as the reply-side mechanism
`Mount` uses internally to answer a `Request` — a tool never has to see them. This matches the seed's
own rule: "anything richer is added as optional interfaces a concrete bus *may* also satisfy — never
forced on callers."

`Service` (`Name`/`Subjects`/`Handle`) and P1's `Plugin` (`Describe`/`Start`/`Stop`/`Health` +
`Manifest`) also agree, at different weights. **Decision:** start with the seed's `Service` — it is
the minimum a tool needs to attach. The kernel's `Manifest`/lifecycle (namespace, static channel
declaration, health, start/stop) is the richer form the same interface grows into when a service
needs lifecycle or state; do not build it until a service needs it. `Service.Name()` *is* the
manifest's namespace by another name.

### 3.2 Subjects / addressing — AGREE

The seed's dotted `<tool>.<noun>.<verb>` (commands via `Request`) and `<tool>.<noun>.<pastTense>`
(events via `Publish`), with wildcard family subscriptions (`task.exec.*`), is compatible with the
kernel's named channels. **Decision:** adopt the seed's convention verbatim. Add one rule from the
kernel side: a subject/channel has an implied *type*, and mixing an event subject with a request
subject is a smell — keep command subjects and event subjects distinct in the namespace, so a later
move to explicit channel types (§3.5) is additive.

### 3.3 In-memory vs NATS — AGREE

Both plans say: **in-memory first, NATS later, tool code identical.** The in-memory bus is the
embedded single-binary transport *and* the test harness. The networked bus (embedded NATS per the
seed and A2) is a wiring decision in `main`, hidden behind the same `Bus` interface — A2's "6-method
interface in one file; NATS is an implementation detail." **Decision:** build the in-memory bus now;
defer the NATS bus behind the interface until the in-memory seam carries real traffic and a real
cross-machine run is scheduled (C2 / P1 Q-1).

### 3.4 Compile-time vs runtime registration — AGREE, with the prior work's boundary

The seed offers both: compile-time (`cmd/harmonik` imports a tool and `Mount`s it) and runtime
(a process announces on `_harmonik.register`). The prior program is firmer: **plugin↔kernel is
in-proc (C6)**, so the *plugin* is always mounted compile-time or as a same-daemon library; **runtime
registration is a kernel↔kernel / LOOKUP concern**, and LOOKUP is deferred. **Decision:** ship
compile-time `Mount` first (the seed's default, the kernel's C6 reality). Treat the seed's
`_harmonik.register` runtime-announce as the LOOKUP-based join protocol from
`01-p2p-comms/README.md` §B — real, designed, and explicitly deferred until dynamic worker join is
scheduled (P1 §4). Do not build a registry service in the first slice.

### 3.5 Hub/spoke job distribution mapped onto the bus — RECONCILE (name the gap)

Here the seed and the prior work differ in a way that matters. The seed's three verbs
(Publish/Subscribe/Request) do **not** include competing-consumer work distribution: `Subscribe` is
one-to-many fan-out (every subscriber gets a copy), and `Request` is one-to-one to *a* responder. A
hub that publishes a job on `task.exec.run` and wants **exactly one** of N spokes to take it needs
the kernel's fourth semantic — `POINT_TO_POINT` with a consumer group (P1 `Transport.Subscribe(
pattern, group)` + `Publish(groupKey)`; P3 §3.1's work channel). Both prior docs treat this as
essential and as the dispatch application's channel.

**Recommendation:** for the first slice, model hub/spoke the seed's way where it already works — a
spoke `Request`s work and the hub `Serve`s it (pull), or the hub `Publish`es an *offer* and spokes
race a `Request` to claim — and **explicitly record that true competing-consumer distribution is the
one transport capability the seed's three verbs lack.** When dispatch becomes a real bus service
(post-first-slice), add point-to-point as the optional richer interface the seed already anticipates,
matching P1's `POINT_TO_POINT` channel. Do not force it into the first `Bus`. The hub-owns-the-queue
decision (C3, concern B) means the hub is authoritative regardless of which mechanism delivers the
job; the queue is `internal/queue` exposed as a `queue` service, not a new bus primitive.

### 3.6 Agents-as-services — AGREE

The seed's "agents are just services" and the kernel's "comms is a plugin" are the same statement.
An agent participates by using `Request`/`Subscribe` as a client, or by implementing `Service` to be
addressable. Today's `harmonik comms` is the concrete proof: re-expressed as a `comms` `Service`
mounted on the bus, agent-to-agent messaging is just traffic on `comms.*` subjects, and the existing
N3 at-least-once + `event_id` dedupe contract is the durability the service builds on top of an
at-most-once bus. **Decision:** the bus makes no distinction between a tool and an agent; both are
participants addressed by subject.

### 3.7 The one hard line — ADOPT from the prior work

The seed is quieter about the boundary than the prior work. **Adopt the kernel's hard line
explicitly:** the bus moves opaque `[]byte`; it never parses a payload; it has never heard of a bead,
a DOT node, a queue, a run, or an agent (DECISIONS §Building principles; A2's `bytes`-not-`Any`
rule). This is what keeps the bus a bus. Git stays the artifact plane (C1): the bus carries the
kickoff and the status and the address, never the diff. A payload ceiling enforces it as a mechanism.

---

## 4. The fundamentals to build FIRST

The operator wants the fundamentals in place once the restructure basics land. The minimal first
slice, in build order:

1. **The two interfaces + `Message`, in `libs/transport`.** `Bus` (Publish/Subscribe/Request),
   `Handler`, `Message` (Subject/Reply/Data/Header), `Subscription`, `Service`
   (Name/Subjects/Handle), and `Mount`. This is the entire public contract and it is designable and
   reviewable **in parallel with the restructure**, because it depends on no legacy code. Keep the
   payload `[]byte` and put `content-type` in `Header` (seed §Payload format).

2. **The in-memory bus.** A subject router in RAM: `Publish` fans out to matching subscribers,
   `Subscribe` registers interest (with dotted-wildcard matching), `Request` delivers to one
   responder and returns its reply via the `Reply` subject. This is both the embedded transport and
   the test harness. No serialization, no network. Model it on P1's in-memory kernel — the default
   test double the whole design leans on.

3. **`Mount`.** Subscribe `s.Handle` to each of `s.Subjects()`; on a request, publish the returned
   payload to `m.Reply`. One helper, works for every service (seed §Service).

4. **One real `Service`: keeper.** Keeper is the right first tool — it already has a clean
   operating contract (the `keeper` skill), a natural set of subjects
   (`keeper.session.restart` command, `keeper.session.grew` event — the seed's own examples), and it
   is a leaf that few things depend on, so porting it exercises the seam without a large blast radius.
   Its domain logic stays a plain library; a thin `NewService()` adapter maps subjects to method
   calls and is the only part that imports `transport` (seed §"What belongs in `libs/transport` vs a
   tool"). This proves the "logic is a library, the bus is a thin adapter" rule.

5. **The first cross-tool test on one in-memory bus.** Mount two real services on one
   `transport.InMem()` and let them talk — one process, milliseconds (seed §Testing). The concrete
   first assertion: a second participant `Request`s `keeper.session.restart` and gets keeper's reply;
   or keeper `Publish`es `keeper.session.grew` and a subscriber observes it. This is the proof the
   seam works before any second binary or any network exists.

**Explicitly deferred until the in-memory seam is proven** (each named, with its trigger):

- **The NATS bus** — deferred until a real cross-machine run is scheduled (C2, P1 Q-1). Ships behind
  the same `Bus` interface; the transport product choice (embedded NATS vs. owned TCP) is not part of
  the contract tools approve.
- **The roster** (`Roster.List/Watch`) — presence as a fabric surface. Deferred; `internal/presence`
  covers the daemon-local need today.
- **LOOKUP / runtime registration** (the `_harmonik.register` announce, the join protocol) —
  deferred until dynamic worker join is scheduled (P1 §4). Compile-time `Mount` covers the first
  slice.
- **Point-to-point competing-consumer distribution** — deferred until dispatch becomes a real bus
  service; added then as the optional richer interface (§3.5).
- **The state/resource seam** (`Resources.State()` KV + journal) — reserved, not built, until a
  service needs durable state (DECISIONS §"Resource APIs — reserve, don't build yet"). Note that
  `internal/eventbus`'s journal is the natural implementation when that day comes.

The discipline throughout: **do not build a channel type, a roster, or a registry the first two
services do not use.** Two services needing a capability the bus lacks is the signal to add it
(the boundary test); one speculative addition is the smell the platform principles warn against.

---

## 5. Dependency on the restructure

The bus lives in `libs/transport`, and `libs/` only exists once the restructure stands up the
clean-room module (`00-seed-playbook.md` §"Target architecture"). The dependency is real but narrow.

**What the restructure must deliver before bus *code* can land in its target home:**

- The clean-room module + `go.work` + the one-way wall check (playbook Step 0). `libs/` is a
  first-class location in the clean module, and the wall (`libs/*` imports no tool) is the same CI
  check the playbook already specifies.
- `libs/cli` with the `Env` struct that carries `Bus transport.Bus` as the composition seam
  (playbook §"One entrypoint"). Keeper's port is the first place `Env.Bus` is exercised.

**What can proceed in parallel, now, with no restructure dependency:**

- **Interface design** — `Bus`, `Service`, `Message`, `Mount` are pure contract. They can be written,
  reviewed, and even prototyped in a scratch package today. The seed and P1 `_plan.md` already give
  the shapes; this is consolidation and refinement, not discovery.
- **This consolidation plan** — done here.
- **The in-memory bus + a throwaway cross-tool test** — buildable in a scratch package to prove the
  router and `Mount` before `libs/transport` exists, then moved in with `git mv` (the playbook's
  move-don't-copy loop).

**What must wait for the restructure:**

- Landing `transport` in `libs/transport` as its own module with the wall enforced.
- Porting keeper into `tools/keeper` as a library + thin main + bus adapter — this *is* a port-loop
  iteration and should ride the restructure's strangler-fig process, not run ahead of it.
- Any second binary / distributed mode — that needs the module boundaries (`go.work`) the restructure
  establishes.

**Recommended coupling:** keep bus interface design and in-memory-bus prototyping moving now, in a
scratch location, so the design is settled and proven when the clean room opens. Make keeper's port
the **first port-loop iteration that also carries the bus** — it lands `libs/transport` (via the
prototype's `git mv`), `libs/cli.Env.Bus`, and `tools/keeper` together, and the cross-tool test is
the proof the whole seam works. Everything past that (NATS, dispatch-as-service, roster) is post-slice.

---

## 6. Open questions and conflicts

1. **Three verbs vs. four channel types (the biggest one).** The seed's `Bus` is three verbs;
   the prior kernel is four channel *types* (pubsub / point-to-point / request-reply / lookup). The
   missing capability is **point-to-point competing-consumer work distribution** (§3.5), which
   hub/spoke dispatch genuinely needs. *Recommendation:* start with three verbs; keep command and
   event subjects distinct so point-to-point is additive; add it as an optional richer interface when
   dispatch becomes a bus service. **Confirm the operator is content to defer point-to-point out of
   the first slice** — the seed defers it by omission; the prior work treats it as core.

2. **`Service.Name`/`Subjects`/`Handle` vs. the kernel's manifest + lifecycle.** The seed's `Service`
   has no `Start/Stop/Health`, no namespace-scoped handle, no static channel declaration validated at
   registration. The prior work makes namespace ownership the boundary that renders cross-plugin
   state access *unrepresentable*. *Recommendation:* start minimal; treat `Name()` as the namespace;
   add lifecycle and the namespace-scoped handle when a service needs state. *Open:* whether the
   first-slice `Service` should already carry `Start(ctx, bus)` so a service can subscribe on its own
   subjects at mount time rather than relying on `Mount` to do it. Low stakes; recommend `Mount`-does-it
   for the first slice.

3. **Where does the durability contract live?** The kernel is at-most-once; `comms` builds
   at-least-once + `event_id` dedupe on top (N3). The seed's `Bus` says nothing about delivery
   guarantees. *Conflict to resolve before comms is ported:* is the in-memory `Bus` at-most-once
   (matching the kernel, forcing services to own durability) or does it inherit eventbus's durable
   journal? *Recommendation:* the `Bus` is at-most-once by contract (keep the kernel's discipline);
   durability is a service concern built on the journal. This keeps the bus small and honest, and it
   matches what `comms` already does. State it in the interface doc so no one assumes the bus retries.

4. **The guardrail ratification is still open program-wide.** The kill-criteria / boundary-test
   package (dep-allowlist, vocabulary test, `[]byte`-payload rule, size tripwire, "two plugins needing
   a new verb = stop") is the *one* operator question the platform program left open (DECISIONS OQ-1;
   kernel-fabric problem-space §"The one open question"). The bus inherits it: these are exactly the
   checks that keep `libs/transport` from growing domain knowledge. *Recommendation:* adopt them as
   principle-backed signals (per the operator's principles-not-laws direction) and reference them from
   the interface doc. Ratification is the operator's call.

5. **Package home: `libs/transport` (restructure) vs. `internal/kernel/` (P1).** The two tracks name
   the same thing two ways. P1 says `internal/kernel/`, dep-allowlisted, out of `internal/daemon`;
   the restructure says `libs/transport` in the clean module. These are not in conflict — they are the
   same package before and after the clean-room move. *Recommendation:* prototype under `internal/`
   (or a scratch package) if it must exist before the clean room, then land it as `libs/transport`.
   Name the alignment explicitly so the two tracks do not create two buses.

6. **Naming: "bus" vs. "kernel/fabric."** The seed and this track say **bus**; the platform program
   says **kernel/fabric**. Same architecture, two names. *Recommendation:* pick one term for the code
   (recommend `transport`/`Bus` per the seed and the restructure playbook, since that is the operator's
   current framing) and note in the interface doc that it is the same thing P1 called the kernel, so a
   future reader does not think there are two systems.

7. **Deferred-but-designed pieces that will resurface fast.** The roster (presence-as-fabric), the
   LOOKUP join protocol, and the NATS hop are all *designed* (A2, P1 §2, `01-p2p-comms`) and all
   *deferred*. The risk is re-deriving them under time pressure instead of reading the existing
   design. *Recommendation:* when each is scheduled, the trigger named in §4 points at the exact prior
   doc. Do not re-plan them; consolidate as this plan did.

---

## Appendix — source map

| Artifact | What it carries |
|---|---|
| `plans/2026-09-07-harmonik-bus/00-seed-bus.md` | The seed: `Bus`/`Service`/`Mount`, subjects, in-mem vs NATS, "never Go `plugin`". |
| `plans/2026-09-07-harmonik-restructure/00-seed-playbook.md` | The module layout (`libs/transport`, `tools/*`, `cmd/harmonik`), `cli.Env.Bus` seam, strangler-fig port loop. |
| `plans/2026-06-30-distributed-fleet/01-p2p-comms/README.md` | Three concerns (membership / scheduling / transport); hub/spoke reconciliation; join flow; comms-bus distribution (C1 vs C2). |
| `plans/2026-06-30-distributed-fleet/PLANNING-SUMMARY.md` | Sequencing; relationship to landed remote-substrate; the single-worker cap. |
| `plans/2026-07-21-platform-architecture/DECISIONS.md` | **C1–C6 locked**; the four platform principles; the guardrail open question. |
| `plans/2026-07-21-platform-architecture/research/A2-substrate-v2-architecture.md` | Faithful extraction of the fleetd kernel (channels, LOOKUP+roster, at-most-once + four durability tools, keep-vs-drop). |
| `plans/2026-07-21-p1-kernel-fabric/_plan.md` | **The in-proc kernel interface** (Transport/Lookup/Roster/Resources/Info + Plugin lifecycle); the minimal first slice; deferrals. |
| `plans/2026-07-21-p3-distributed-execution/_plan.md` | First consumer: container dispatch on the kernel; L1–L9 liveness; boundary test. |
| `.kerf/works/kernel-fabric/01-problem-space.md` | Consolidated problem space; "already decided" table; the one open question. |
| `internal/eventbus`, `internal/presence`, `internal/dispatch`, `internal/queue`, `internal/mergeq` | Today's coordination code the bus unifies (event journal, presence, dispatch, central queue, git merge queue). |
| `agent-comms` skill / `harmonik comms` | Today's agent-to-agent surface; the first `Service` waiting for a `Bus`; N3 at-least-once + `event_id` dedupe. |
