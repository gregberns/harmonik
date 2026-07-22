# P1 — Kernel / Fabric: Plan (design-only, pre-kerf review draft)

**Date:** 2026-07-21 · **Status:** DRAFT for review, then kerf · **Daemon/comms:** DOWN (design only)
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
- **Work-dispatch = one central owner, as an application.** A single **dispatch plugin**
  instance is configured `role: primary` and holds the queue. Every other daemon runs a
  **worker plugin** whose static config names the primary. Primary dies → **restart it**; you
  do not rebuild the network. This is the operator's "central harmonik server holds the queues"
  with the clarification that the fabric underneath has no single point of failure.

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

- **LOOKUP / dynamic worker registration.** Static config (Q workers naming the primary) covers
  the first cut; LOOKUP-based join/leave of ephemeral containers is the "later capability" C3
  names and A2 flags as a thin spot.
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
  resource + Info. LOOKUP and real mesh deferred.
- **Where durable dispatch lives** → the dispatch *plugin* (P3), not the kernel. P1 exposes the
  four tools; it does not grow a queue.
- **Package home** → `internal/kernel/`, dep-allowlisted, out of `internal/daemon`.

### Questions for operator

- **Q-1 — Cross-machine transport implementation.** substrate-v2 chose *embed NATS core behind
  the 6-method interface* (JetStream off). C6 didn't change kernel↔kernel (still network). Keep
  the NATS-embed bet for the networked kernel, or start the first real cut on something simpler
  (minimal owned TCP/gRPC between kernels) and revisit? Either way the contract plugins approve
  is unchanged — this is purely the hidden implementation. **Recommendation: defer the choice;
  ship the in-memory kernel first (§5), decide Q-1 when the first *cross-machine* P3 run is
  actually scheduled.** Need your OK to defer.

- **Q-2 — First cut is static-primary, not yet leaderless-in-practice.** C3 says static config
  first. That means "no single point of failure" is *contracted at the interface* but the first
  implementation (worker dials primary) does **not** demonstrate partition survival or mesh.
  Confirm we ship the static cut now and treat mesh + dynamic LOOKUP registration as an
  explicit *later* capability — i.e. we are OK that the leaderless property is designed-for but
  unproven in v1.

- **Q-3 — Ratify the kill-criteria / boundary-test guardrail.** DECISIONS lists this as
  "proposed — to ratify." I want to adopt it as *shared, pre-agreed* ground rules (dep-
  allowlist, vocabulary test, `[]byte` payload, size tripwire, "two plugins needing new verbs =
  stop") so scope disputes are settled by a test both sides agreed to in advance. Ratify?

- **Q-4 — Live-reload: accept losing it under C6?** In-proc plugins (C6) cannot be hot-swapped
  the way substrate-v2's separate-process plugins could; swapping a plugin means restarting the
  daemon. C6 was chosen deliberately (fewer deploy artifacts, easier testing) and I think the
  trade is right, but it *does* forfeit the "swap tooling without stopping the daemon"
  property. Confirm that's acceptable (daemon restart to swap a plugin), or flag if hot-reload
  is a requirement — because it would reopen the in-proc-vs-process boundary.

- **Q-5 — `max_payload_bytes` posture given C1.** Since the fabric is control-plane-only (git
  is the artifact plane), the payload ceiling should be small enough to *structurally* prevent
  someone streaming a diff/repo over it. Want a hard, low ceiling (e.g. tens–hundreds of KB) as
  a C1 tripwire, or leave it advisory? **Recommendation: hard low ceiling** — it makes C1
  enforced by the kernel, not by discipline.
