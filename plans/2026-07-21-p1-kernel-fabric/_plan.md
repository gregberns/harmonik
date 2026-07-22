# P1 — Kernel / Fabric: Plan (design-only, pre-kerf review draft)

**Date:** 2026-07-21 · **Revised:** 2026-07-22 (kilo) · **Status:** REVIEWED — must-fixes applied, KERF-READY
**Daemon/comms:** DOWN (design only)

**Review disposition.** The 2026-07-21 chief-architect pass returned **NEEDS-REVISION (minor)**
on P1: the frame and C1–C6 are honored, the substrate-v2 recast is accurate, and scope is
right-sized — with two must-fixes and one deletion. All three are applied:

| Review item | Where | Status |
|---|---|---|
| Must-fix 6 — dispatch-plugin channel ownership is ambiguous; a separately-namespaced worker plugin cannot use `dispatch.*` | §3 | **Applied** — one plugin, `role: primary\|worker`, one namespace |
| Must-fix 1 (P1 half) — state the LOOKUP deferral as a *contract with P3*, which currently marks it REQUIRED | §4 | **Applied** — trigger named; static-config reading wins per C3 |
| Drop Q-2 — already answered verbatim by locked C3 | §7 | **Applied** — deleted, recorded as a scope line |

The review also resolved P1's Q-1 / Q-4 / Q-5 without operator input. Those are recorded in §7
as adopted defaults, leaving **one** genuine operator question in this plan (Q-3, guardrail
ratification). Nothing in P1 violates a locked decision.
**Governing decisions:** `../2026-07-21-platform-architecture/DECISIONS.md` (C1–C6, locked).
**Reused architecture:** `../2026-07-21-platform-architecture/research/A2-substrate-v2-architecture.md`
(the fleetd kernel) — recast for in-proc plugins per C6, premise dropped per §6 below.

This plan is honest about what is settled vs. open. Everything requiring operator judgment is
in the final section, not buried in prose.

---

## 1. Goal + boundary

**P1 is the KERNEL/FABRIC:** the transport + addressing + plugin-interface layer that
applications (execution-dispatch, comms, …) run on top of. It is the network-aware,
cross-machine layer. Plugins are in-proc libraries that reach other machines *through* it.

### What the kernel IS

- **Transport.** Named, typed **channels** that move **opaque bytes** between plugins on the
  same or different machines. Four channel types (pubsub, point-to-point, request-reply,
  lookup) — the substrate-v2 set, unchanged.
- **Addressing / identity.** A **lookup** surface (replicated key→value map, single-writer-
  per-key) + a **roster** (which boxes are alive, their address, their liveness). Together
  these answer "who/what is reachable" without any component being in charge.
- **Plugin-interface lifecycle.** A Go interface a plugin implements (`Describe/Start/Stop/
  Health`) + the namespace-scoped `Kernel` handle the kernel hands it. Static channel
  declaration validated at registration.
- **A reserved resource-API seam.** A standard mechanism through which plugins get **state/
  storage** (box-local KV + append-only journal) instead of each inventing its own. State is
  the *only* resource shipped now; the seam is designed to admit later resources without
  touching transport.

### What the kernel is NOT

- **No domain knowledge.** It has never heard of a bead, a DOT node, a queue, a run, a
  session, a repo, an agent, or an SSH key. `Payload` is `[]byte`, never a typed/`Any` value —
  the kernel *cannot* parse a payload even if a future contributor wants it to. (substrate-v2's
  one hard line, kept verbatim.)
- **Not the data/artifact plane (C1).** Git remains the artifact plane. The fabric carries
  control, events, and addresses — **never** code, diffs, or repos. This bounds kernel size:
  no artifact durability, no big-payload design, a `max_payload_bytes` the caller must read.
- **Not the work-dispatcher.** "One central queue owner" (C3) is an *application on top*, not a
  kernel property. The kernel has no queue, no scheduler, no leases.
- **Not a delivery guarantor.** The kernel is **at-most-once**: never queues, never retries,
  never redelivers. Durability (comms' at-least-once + `event_id` dedupe; dispatch's leases)
  is built *by plugins* from journal + `message_id` + interest + liveness.

---

## 2. The minimal kernel interface (in-proc Go)

C6 recast: substrate-v2's contract was a gRPC/protobuf `KernelService` (19 RPCs, cross-process)
+ `PluginService` (4 lifecycle). We keep the **shape and the boundary**, drop the wire. plugin↔
kernel is a **Go interface, in-process**; kernel↔kernel stays network. Consequences of the
recast, all deliberate:

- **Streaming RPCs → Go channels + cancel func.** `Subscribe/Serve/KVWatch/JournalRead(follow)/
  RosterWatch` return `(<-chan T, cancel func(), error)`.
- **No namespace field anywhere → a namespace-scoped handle.** The `Kernel` passed to `Start`
  is already bound to the plugin's identity, so naming another plugin's storage is
  *unrepresentable*, not merely forbidden — the in-proc equivalent of substrate-v2's "kernel
  derives namespace from caller identity."
- **No go-plugin, no separate binary, no `GRPCBroker`.** A plugin is a Go value the daemon
  constructs. One surface, no second private host API.

```go
package kernel // internal/kernel

// ---- transport primitives -------------------------------------------------

type ChannelType int
const (
    ChannelPubSub       ChannelType = iota + 1 // every matching subscriber gets a copy
    ChannelPointToPoint                        // competing consumers; one group member per msg
    ChannelRequestReply                        // one question, one answer, correlated by kernel
    ChannelLookup                              // replicated map; the ONLY replicated thing
)

type Interest int // Present | None | Unknown  — a HINT ("someone listens"), never an ACK.

type Envelope struct {
    Channel    string
    Payload    []byte            // OPAQUE. Never parsed by the kernel.
    Headers    map[string]string // plugin metadata; kernel does not read it
    OriginNode string            // stamped by kernel
    OriginTime time.Time         // that box's clock — NOT a fleet order
    MessageID  string            // the hook a plugin builds ACKs/dedupe on
    OriginSeq  uint64            // monotonic per (OriginNode, Channel). NEVER fleet-wide.
    Producer   string            // publishing plugin's namespace
}

// Transport = messaging. Namespace-scoped; a plugin publishes only inside its
// declared channels. group_key routes POINT_TO_POINT competing consumers.
type Transport interface {
    Publish(ctx context.Context, ch string, payload []byte, hdrs map[string]string, groupKey string) (msgID string, in Interest, seq uint64, err error)
    Subscribe(ctx context.Context, pattern, group string) (<-chan Envelope, func(), error)
    Request(ctx context.Context, ch string, payload []byte, hdrs map[string]string, timeout time.Duration) (Envelope, Interest, error)
    Serve(ctx context.Context, pattern string) (<-chan ServeReq, func(), error)
    Respond(reqID string, payload []byte, hdrs map[string]string, errMsg string) error
}

// ---- addressing / identity ------------------------------------------------

type Lookup interface {
    Put(ctx context.Context, ch, key string, value []byte, ttl time.Duration) (rev uint64, err error) // this node = sole writer of key
    Get(ctx context.Context, ch, key string) ([]LookupEntry, error) // EVERY claimant; kernel does not pick
    List(ctx context.Context, ch, keyPrefix string) ([]LookupEntry, error)
}

type Roster interface {
    List(ctx context.Context) (nodes []NodeStatus, self string, err error)
    Watch(ctx context.Context) (<-chan RosterEvent, func(), error) // JOIN | UPDATE | DOWN
}
// NodeStatus{Node{name,address,os,arch,port,intent}, Liveness{ALIVE|SUSPECT|DEAD|UNKNOWN,
// last_seen, reason}} — liveness computed LOCALLY, never gossiped.

// ---- reserved resource-API seam (state/storage FIRST; do NOT build past it) --

// Resources is the seam the operator asked us to *reserve*: a standard place a
// plugin gets resources so no app invents its own storage. State (KV + journal)
// is the ONLY resource shipped. A later resource is a NEW method here, never a
// change to Transport — that is the point of isolating it behind this seam.
type Resources interface {
    State() State
    // reserved (NOT built): Blob() Blob; Secrets() Secrets; ...
}

type State interface { // box-local, namespace-scoped, NEVER replicated
    KVPut(ctx context.Context, key string, value []byte, ttl time.Duration, ifRevision *uint64) (rev uint64, err error) // CAS; ifRevision=&0 => must not exist
    KVGet(ctx context.Context, key string) (value []byte, rev uint64, found bool, err error)
    KVDelete(ctx context.Context, key string) (existed bool, err error)
    KVList(ctx context.Context, prefix string, limit int) ([]KVEntry, error)
    KVWatch(ctx context.Context, prefix string) (<-chan KVEvent, func(), error)
    JournalAppend(ctx context.Context, journal string, records [][]byte, sync bool) (seqs []uint64, synced bool, err error) // sync = per-call fsync, plugin's call
    JournalRead(ctx context.Context, journal string, afterSeq uint64, limit int, follow bool) (<-chan JournalRecord, func(), error)
    JournalTruncate(ctx context.Context, journal string, beforeSeq uint64) (removed uint64, err error)
}

// ---- the whole plugin-facing surface (in-proc replacement for KernelService) --

type Kernel interface {
    Transport
    Lookup
    Roster
    Resources
    Info(ctx context.Context) (Info, error) // node, api_version, assigned namespace, max_payload_bytes
}

// ---- plugin lifecycle (kernel -> plugin; in-proc analog of PluginService) ----

type Plugin interface {
    Describe() Manifest                                  // static, inspectable without Start
    Start(ctx context.Context, k Kernel) error           // k already namespace-scoped
    Stop(ctx context.Context, grace time.Duration) error
    Health(ctx context.Context) (ok bool, detail string)
}

type Manifest struct {
    Namespace   string          // owns <namespace>.* and its storage. THE identity.
    Version     string
    APIVersion  uint32
    Channels    []ChannelDecl   // static; conflicting type for a name rejected AT registration
    Interests   []InterestDecl  // ChannelInterest | RosterInterest — exactly two kinds
    Description string
}
```

**Kernel↔kernel boundary (the only network hop).** Exactly what crosses a machine boundary,
nothing else: (1) channel bytes on a route, (2) LOOKUP entries (the one replicated thing),
(3) liveness probes. Storage never crosses; a plugin's bytes stay on its box unless it
publishes them. The cross-machine transport sits behind a tiny internal interface
(`Publish/Subscribe/Request/Serve/Interest/SetPeers/Close`) in one file — the substrate-v2
pattern — so the product choice (embedded NATS vs. minimal owned TCP; **open Q-1**) is a
swappable detail, not part of the contract plugins approve.

---

## 3. Primary / worker model (C3)

Two layers, answered separately — this is the decision that stops the peer-vs-hub argument
recurring:

- **Transport = leaderless/peerless at the contract level.** No method names a coordinator;
  the transport interface takes a peer *set* (`SetPeers`), not a leader. A kernel can cold-boot
  alone and keep serving when peers die. "No box in charge" is a property of the *network*.
- **Work-dispatch = one central owner, as an application.** **ONE dispatch plugin, deployed on
  every daemon, with `role: primary | worker` in its config.** The instance configured
  `role: primary` holds the queue; every other instance runs the worker side. Primary dies →
  **restart it**; you do not rebuild the network. This is the operator's "central harmonik
  server holds the queues" with the clarification that the fabric underneath has no single
  point of failure.

  **Why one plugin and not two (a primary plugin + a worker plugin).** Under §2's manifest rule
  a namespace owns `<namespace>.*` and its storage — that ownership is what makes cross-plugin
  storage access *unrepresentable* rather than merely forbidden. Two separately-namespaced
  plugins therefore could not talk: a `worker`-namespaced plugin could neither publish on
  `dispatch.status` nor subscribe-as-group on `dispatch.work`, because it does not own
  `dispatch.*`. Resolving that by inventing cross-namespace publish/subscribe rights would
  weaken the single hardest boundary in the kernel for one application's convenience.
  One plugin, one namespace (`dispatch`), deployed everywhere, behaving differently by config,
  needs no such exception. substrate-v2 never hit this because the plugin that ran on every box
  (comms) was the same plugin — the same resolution, arrived at by not having the problem.

  **This is a contract with P3, not a P1 implementation detail:** P3's dispatch plugin ships as
  one artifact with a role switch. If P3 ever proposes split primary/worker plugins, that is the
  boundary test firing (§6) — the answer is to re-examine the split, not to add cross-namespace
  channel rights to the kernel.

**How a worker daemon connects to the primary's kernel (first cut, static — C3):**

1. Worker config carries `primary = <node-name / address>`. No dynamic discovery yet.
2. Worker's kernel adds the primary to its peer set (`SetPeers`) → the two kernels form the
   transport link. Roster now shows both boxes + liveness.
3. The **dispatch plugin** does the rest as an application: primary `Publish`es beads on a
   `dispatch.work` POINT_TO_POINT channel; workers `Subscribe(group="workers")` as competing
   consumers and pull. Status flows back on a `dispatch.status` PUBSUB channel. Lease/liveness
   state lives in the dispatch plugin's `State` (KV CAS + journal).

**Honest scope line (feeds open Q-2):** "leaderless" is guaranteed *at the interface*. The
first *implementation* is static-peer (worker dials primary) and does **not** yet prove
partition survival or multi-peer mesh. That is deliberate minimalism per C3 ("static config,
not dynamic discovery yet"), but it means the leaderless property is *contracted, not yet
demonstrated*. Full mesh + dynamic worker registration via LOOKUP is a later capability.

---

## 4. How P3 consumes it (the minimal-but-sufficient slice)

P3's first plugin is **execution dispatch**: hand a bead to a container, run it, report status,
requeue on death. Per A5 §2 it decomposes exactly onto kernel primitives — no new kernel verb.
The **exact slice P3 needs on day one**, and nothing more:

| P3 need | Kernel primitive consumed |
|---|---|
| Primary offers beads; workers pull one each (competing consumers) | `Transport.Publish(groupKey)` + `Subscribe(pattern, group)` on a POINT_TO_POINT channel |
| Worker streams progress / DOT-node advances / heartbeats back to primary | `Transport.Publish` + `Subscribe` on a PUBSUB channel |
| Kickoff + ack (primary confirms a worker took a specific bead) | `Transport.Request` / `Serve` / `Respond` (REQUEST_REPLY) |
| Worker liveness so a dead worker's in-flight bead can be requeued (C5 non-skippable) | `Roster.List` / `Watch` |
| Durable lease / in-flight state so a restart of the primary doesn't strand `in_progress` | `Resources.State()` — KV CAS (`ifRevision`) + append-only journal |
| Node/self identity, payload ceiling | `Info` |

**Deferred out of the day-one slice (built when P3 forces it, not before):**

- **LOOKUP / dynamic worker registration.** Static config (workers naming the primary) covers
  the first cut; LOOKUP-based join/leave of ephemeral containers is the "later capability" C3
  names and A2 flags as a thin spot.

  **Concrete trigger, so "deferred" cannot drift into "contested":** the `Lookup` *interface*
  ships on day one and the in-memory kernel MAY implement it trivially — deferral is of the
  replicated cross-node implementation and of any *consumer*, not of the contract. **LOOKUP
  lands when dynamic worker join is scheduled.** Until then v1 has no LOOKUP consumer: worker
  addresses are static per C3, and containers are supervised by their local worker rather than
  being fabric-visible, so nothing ephemeral needs an address in the fabric.

  **This resolves a live contradiction with P3, recorded here so it is not re-litigated:** P3
  §3.2.1 currently declares ephemeral LOOKUP join/leave "P3 REQUIRES this from P1," while P3 §4
  states topology is static config. Both cannot hold. **The static-config reading wins — it is
  what C3 locked** — and P3 §3.1/§3.2/§2.5 move LOOKUP + ephemeral join/leave to the first
  post-v1 increment. Without this, P1 and P3 would be scoped against contradictory contracts.
- **Real cross-machine mesh transport.** First co-validation uses an in-memory transport
  double (see §5); the NATS-or-TCP implementation lands behind the transport interface without
  changing what P3 consumes.

**Governing rule (C2): no hacks in either P1 or P3.** If the dispatch plugin needs a kernel
verb that isn't above, that is the boundary test firing — we fix the boundary, we do not add a
bead-aware RPC to the kernel. Two consecutive plugins needing new kernel verbs = stop (kill
criterion, §6).

**The distributed-systems hard part lives in the plugin, by design (A5 R4).** Leases,
redelivery, orphan recovery, the C5 heartbeat/timeout/per-DOT-node-timeout controls — all
**dispatch-plugin machinery** built from journal + `message_id` + interest + liveness. The
kernel stays at-most-once. This must be designed with the same rigor as the kernel interface;
it is P3's design, not P1's, but P1 must expose exactly the four durability tools it needs
(journal, stamped id, interest hint, liveness) — and it does.

---

## 5. Build-as-its-own-thing (fast iteration, co-validated with P3)

- **Package.** `internal/kernel/` (module `github.com/gregberns/harmonik`) is P1's own package
  with its own test harness. It imports **nothing** domain-specific; a dependency-allowlist
  test enforces that (a bead/queue/DOT import fails CI). It does not live in `internal/daemon`
  (A5 R1/R3: nothing new for the execution path is born in the god package).
- **Two implementations behind one interface, from day one:**
  1. **In-memory kernel** — single process, loopback, all primitives. Lets P1 iterate in
     milliseconds with zero network. This is the default test double P3 links against.
  2. **Networked kernel** — real cross-machine transport (NATS-embed or owned TCP, Q-1),
     swapped in behind the transport interface later. The 6-method transport interface is the
     seam; the product choice is not part of the contract.
- **Cross-node test without a network:** wire *two* in-memory kernels together with an
  in-process transport double. P1 and P3 can prove worker-pulls-bead-from-primary,
  status-flows-back, worker-dies-bead-requeues — all in a unit test, no incus, no Tailscale.
  This is how P1 stays fast while P3 validates it.
- **P3 is the test bed (C2).** P1 ships the minimal slice §4; P3's dispatch plugin is the first
  real consumer and *is* the boundary test. P1 and P3 iterate in tandem: P3 discovers a need →
  P1 adds it to the contract *only if* it is domain-blind → both advance. No "temporary pipe,
  migrate later" (that is exactly how the ssh model happened).
- **Guardrails armed at kerf time (proposed, §6 / open Q-3):** dependency-allowlist test,
  vocabulary/boundary test (no domain noun in `internal/kernel`), `[]byte`-payload rule,
  ~kernel-size tripwire, and "two plugins needing new verbs = stop."

---

## 6. Reuse from substrate-v2 vs. drop with its premise

**REUSE (premise-independent — this is the foundation):**

1. The **kernel/plugin split and the hard boundary line** ("above knows what an agent is; below
   has never heard of one"), enforced by `[]byte` payloads + dep-allowlist + vocabulary test.
2. The **channel abstraction** — name + type, four types (pubsub / point-to-point /
   request-reply / lookup).
3. **LOOKUP + roster** as the identity/address system (who/what is reachable, box liveness
   computed locally, never gossiped).
4. **Box-local storage** — KV (CAS) + append-only journal, per-call durability, **no namespace
   field** (identity-derived), never replicated. This becomes the **first resource API** behind
   the reserved seam (§2).
5. **At-most-once transport + the four durability tools** pushed up to plugins.
6. **Transport hidden behind a tiny interface** so the product choice is swappable.
7. **The enforcement + kill-criteria discipline** (boundary test, dep-allowlist, size tripwire,
   "two plugins needing new RPCs = stop") and **the refusal list** (no consensus/quorum, no
   fleet-wide order, no kernel queuing, no plugin-to-plugin addressing).

**RECAST (C6 — the one structural change):** separate-process plugins + gRPC + go-plugin +
protobuf `Any`-free wire → **in-proc Go interfaces**. Streaming → Go channels. Wire boundary
moves from plugin↔kernel to kernel↔kernel only. **Casualty to surface (open Q-4):** live-reload
(kill child / exec new binary, daemon never stops) was a property of *separate-process*
plugins. In-proc plugins cannot be hot-swapped without restarting the daemon.

**DROP (tied to the rejected "share/centralize searchable memory" premise):**

1. The **"centralize all agent learning, make it searchable"** goal as the *reason to build*.
2. The **logtail + archive + search backbone** milestone (M6) — the purest expression of the
   dropped premise. Not a substrate concern.
3. The **registry-plugin "fleet-wide view of what every agent is doing"** *goal* (the LOOKUP
   *mechanism* is reused; the shared-activity-view *purpose* is premise-flavored — drop it).
4. The **motivating anecdotes** ("something learned on one machine dies there") — they framed
   the problem as data-sharing and must not drive P1's requirements. P1's reason to exist is
   §1 (a generic control-plane fabric), not shared memory.

---

## 7. Open questions

Resolved here (no operator input needed):

- **Streaming shape** → Go channels + cancel func (idiomatic in-proc; mocks cleanly).
- **Namespace isolation** → namespace-scoped `Kernel` handle at `Start`; cross-namespace access
  unrepresentable. No namespace argument anywhere.
- **Minimal P3 slice** → §4 table: transport (pubsub + p2p + req/reply) + roster + State
  resource + Info. LOOKUP and real mesh deferred, **with the trigger and the P3 contradiction
  resolved in §4** — the earlier version of this line claimed resolution it did not have, since
  P3 simultaneously marked LOOKUP required.
- **Dispatch channel ownership** → ONE `dispatch` plugin with a `role: primary|worker` config
  switch, deployed on every daemon (§3). Two separately-namespaced plugins could not share
  `dispatch.*` without weakening namespace ownership.
- **Where durable dispatch lives** → the dispatch *plugin* (P3), not the kernel. P1 exposes the
  four tools; it does not grow a queue.
- **Package home** → `internal/kernel/`, dep-allowlisted, out of `internal/daemon`.

### Questions for operator

The pre-kerf review consolidated 18 raw questions across the three plans down to 3 that
genuinely need operator judgment. **Exactly one of those is P1's.** The rest of P1's original
five are answered below as adopted defaults — either because a locked decision already settled
them, or because they are reversible implementation choices that should not consume operator
attention. **Any of them can be vetoed; none blocks kerf.**

#### NEEDS OPERATOR JUDGMENT (1)

- **Q-3 — Ratify the kill-criteria / boundary-test guardrail.** DECISIONS lists this as
  "proposed — to ratify," so it is explicitly reserved for you. Adopt as *shared, pre-agreed*
  ground rules: dep-allowlist test, vocabulary/boundary test (no domain noun in
  `internal/kernel`), `[]byte`-payload rule, kernel-size tripwire, and "two plugins needing new
  kernel verbs = stop." The point is that scope disputes get settled by a test both sides agreed
  to in advance, rather than by whoever says "over-engineering" or "dismissal" first.
  **Recommendation: ratify the package.** One yes here also covers the equivalent question in P2
  and P3 — the review dedupes them into this single ask.

  This is P1's *only* genuine operator question. The other two the review escalated (the C5
  liveness set, and the review-token posture) belong to P3 and codex-first.

#### ADOPTED DEFAULTS — recorded, vetoable, not blocking

- **Q-1 — Cross-machine transport implementation → DEFER.** Ship the in-memory kernel first
  (§5); decide NATS-embed vs. minimal owned TCP when the first *cross-machine* P3 run is
  actually scheduled. The 6-method transport interface is what makes this genuinely deferrable
  rather than merely postponed: the choice sits behind a seam the plugin contract never sees.

- **Q-2 — DELETED, already locked.** The former question asked whether to accept a static-primary
  first cut whose leaderless property is contracted but not demonstrated. **C3 locked exactly
  that, verbatim** ("static config, not dynamic discovery yet"). Asking again would re-open a
  locked decision. Recorded instead as a known scope line: *leaderless is contracted at the
  interface; partition survival and mesh are demonstrated later.* §3's "honest scope line" is
  the durable statement of it.

- **Q-4 — Live-reload loss → ACCEPTED CONSEQUENCE of locked C6, FYI not a question.** In-proc
  plugins cannot be hot-swapped; swapping one means restarting the daemon. C6 chose in-proc
  deliberately (fewer deploy artifacts, easier testing) with this as a known trade. Reopen only
  if hot-swap ever becomes an actual requirement — which would reopen the in-proc-vs-process
  boundary itself, not just this line.

- **Q-5 — `max_payload_bytes` → HARD LOW CEILING (order 256 KB).** This converts locked C1 from
  a discipline into a mechanism: with a low hard ceiling, streaming a diff or a repo over the
  fabric *fails* rather than merely violating a rule someone has to remember. Nothing in P3 needs
  a large payload — its messages carry a bead id and a SHA. A ceiling that is advisory is a C1
  guardrail that only holds while everyone is paying attention.
