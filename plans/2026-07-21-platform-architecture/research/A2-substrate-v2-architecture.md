# A2 — The architecture inside `2026-07-15-agent-substrate-v2`

**Extractor:** research agent, 2026-07-21. **Method:** read the core docs in full
(`ARCHITECTURE.md`, both reconciled proto files, `BRIEF.md`, `00-README.md`, `ROADMAP.md`);
the `design/20–24` and `investigate/10–15` docs are supporting evidence and were used only
where `ARCHITECTURE.md` cites them. Every claim below is grounded in a `file:line`.

Path prefix for all refs: `plans/2026-07-15-agent-substrate-v2/`.

---

## Bottom line

The plan already contains, almost exactly, the architecture the operator now says he wants:
a per-box daemon (`fleetd`) whose **kernel is a pure data-transport-plus-address layer** —
19 RPCs that move opaque bytes between named/typed channels, write bytes to local disk, and
keep a live list of reachable boxes, while *knowing nothing* about agents, logs, comms, or
SSH (`ARCHITECTURE.md:118-120`, `kernel.proto:1-6`). Everything domain-specific is a
**separate-process plugin** that speaks the *same* protobuf interface the kernel exposes,
declares its surface in an inspectable manifest, and holds no in-memory state — so tooling is
composed from consistent interfaces rather than mushed into one bundle (`plugin.proto:37-71`,
`ARCHITECTURE.md:282-303`). The transport layer connects multiple machines (embedded NATS
mesh, no box in charge) and the **LOOKUP channel + roster** are the identity/address system
that lets components discover what's reachable (`ARCHITECTURE.md:152, 240-268`,
`kernel.proto:29-32, 187-242`). The plan's *stated premise* — "the fleet has no shared memory
and no shared address book, centralize learning so it's searchable" (`BRIEF.md:23-33`) — is
the sharing-data framing the operator now rejects, but it is **cleanly separable**: it lives
in the plugin layer and the roadmap's later milestones, not in the transport/addressing
substrate. Reuse the kernel, the proto contract, the plugin model, and the layering
discipline; drop the "centralize all agent learning / searchable memory" goal as the reason
for building it.

---

## Rejected premise (quoted)

The plan states its own premise repeatedly and unambiguously as **shared memory + shared
searchable address book across machines** — this is the "sharing data" framing the operator
(2026-07-21) now says is *incorrect as the point*.

- `00-README.md:7`: *"what should we build so the fleet has **a shared memory and a shared
  address book**, and so agents on different machines can send each other messages — without
  any single machine being in charge?"*
- `ARCHITECTURE.md:33`: *"**The through-line: the fleet has no shared memory and no shared
  address book.** Everything an agent learns dies on the box it learned it on."* (repeated
  verbatim at `BRIEF.md:31`).
- `BRIEF.md:28`: *"how could all learning of all agents be centralized and searchable?"*
- `BRIEF.md:27`: *"if I did something yesterday one one project on one machine, how can I find
  that thing from another machine in another agent?"*
- The pitch itself, `ARCHITECTURE.md:19`: *"Everything an agent learns on one of them dies
  there, and an agent that wants to reach another machine has to rediscover how to get there."*

So the plan's *motivating problem* is "sync/centralize what agents learn so it is searchable
fleet-wide" (`BRIEF.md:23-33, 95`, roadmap M6 "search attaches" `ROADMAP.md:237-249`). That is
the premise the operator rejects. Note that even *inside* the plan, "search / centralized
learning" is already demoted to a downstream **consumer**, not a kernel concern
(`BRIEF.md:95`, `ARCHITECTURE.md:636`, `ROADMAP.md:246-249`) — which is exactly why the
architecture survives dropping the premise.

---

## The architecture (faithful)

### The layer cake and the one hard line

Two named artifacts (`ARCHITECTURE.md:49-52`):

- **`fleetd`** — the daemon, one per box, *contains the kernel*, always running.
- **`fleetctl`** — the CLI a human or agent uses to talk to a local `fleetd`.
- A **plugin** is defined precisely as *"a separate ordinary program — its own file, its own
  process — that `fleetd` launches as a child and talks to over a socket. It is not a library,
  not a `.so`, not code loaded into `fleetd`"* (`ARCHITECTURE.md:52`).

The central structural device is **THE KERNEL/PLUGIN LINE** (`ARCHITECTURE.md:54-112`, diagram):
*"Above: knows what an agent is. Below: has never heard of one"* (`ARCHITECTURE.md:70-71`).
The line is not a convention — it is enforced three ways, the strongest being that the
kernel's `payload` field is typed `bytes` not protobuf `Any`, so *"the kernel cannot parse a
payload even if a future contributor wants it to"* (`ARCHITECTURE.md:114`; enforced in proto
at `kernel.proto:39` `bytes payload = 2; // OPAQUE. Never parsed by the kernel.`). The other
two enforcers: a dependency-allowlist test and a vocabulary/boundary test
(`ARCHITECTURE.md:114`, `00-README.md:67`, script `design/20-kernel-boundary-test.sh`).

**The complete list of what crosses a machine boundary** — nothing else, ever
(`ARCHITECTURE.md:102-112`): (1) channel data (bytes on a named channel, over NATS routes);
(2) LOOKUP entries (the one replicated thing); (3) liveness probes. What *never* crosses:
kernel SQLite storage, plugin binaries (records cross, bytes fetched+verified locally),
go-plugin traffic (fleetd talks only to its own children), and any opinion about a third box's
liveness or clock.

### The kernel = the data transport layer + storage + roster (19 RPCs)

Kernel rule (`ARCHITECTURE.md:120`): *"it moves bytes between named, typed channels, writes
bytes down when a plugin asks, and knows which boxes are alive. It does not know what any byte
means."* The authoritative interface is `design/25-reconciled-proto/fleet/kernel/v1/kernel.proto`
— one `KernelService`, **19 methods, all unary or server-streaming, no bidi**
(`kernel.proto:253-281`). Bidi is banned deliberately so the *same* service answers plain
`curl` over HTTP/1.1 and a plugin over gRPC (`kernel.proto:253-255`, `ARCHITECTURE.md:646`).

**Channels — a name and a type** (`ARCHITECTURE.md:139-160`, `kernel.proto:15-33`). This is
the load-bearing realization, quoted from Greg at `ARCHITECTURE.md:143` / `BRIEF.md:51`:
*"the daemon had 'channels', a channel had a name and a type (pubsub, etc), then the daemon
would do data transport, while the plugin handled all the logic."* Four channel types ship
(`kernel.proto:18-33`, table `ARCHITECTURE.md:147-152`):

| Type (proto enum) | Semantics | Plain English |
|---|---|---|
| `CHANNEL_TYPE_PUBSUB` (`kernel.proto:23`) | every matching subscriber gets a copy | radio broadcast |
| `CHANNEL_TYPE_POINT_TO_POINT` (`kernel.proto:26`) | competing consumers; one member of a group gets each msg | work queue |
| `CHANNEL_TYPE_REQUEST_REPLY` (`kernel.proto:28`) | one question → one answer, correlated by kernel | phone call |
| `CHANNEL_TYPE_LOOKUP` (`kernel.proto:32`) | replicated map; **the only thing the kernel replicates** | shared address book |

The five kernel RPC groups (`kernel.proto:256-281`):

1. **Messaging / transport:** `Publish`, `Subscribe` (stream), `Request`, `Serve` (stream),
   `Respond` (`kernel.proto:257-261`). The `Envelope` is the unit carried
   (`kernel.proto:37-47`): plugin-owned `channel`, opaque `payload`, `headers` map (fields
   1-3); kernel-stamped `origin_node`, `origin_time`, `message_id`, `origin_seq`, `producer`
   (fields 10-14). `origin_seq` is *"monotonic per (origin_node, channel). NEVER fleet-wide"*
   (`kernel.proto:45`) — a deliberate refusal of global order (`ARCHITECTURE.md:631`).
2. **Lookup (identity/address replication):** `LookupPut`, `LookupGet`, `LookupList`
   (`kernel.proto:263-265`). `LookupGet` *"returns EVERY claimant of the key… the kernel does
   not pick"* (`kernel.proto:120-123`); single-writer-per-key by construction
   (`kernel.proto:29-31`).
3. **Storage (box-local, plugin-private):** `KVPut/Get/Delete/List/Watch` (CAS via
   `if_revision`, `kernel.proto:140`) and `JournalAppend/Read/Truncate` (append-only,
   per-call `sync` durability flag `kernel.proto:161-167`) (`kernel.proto:267-275`).
   **There is no `namespace` field anywhere** (`kernel.proto:128-134`): the kernel derives the
   namespace from caller identity, so reading another plugin's storage is *"not forbidden, it
   is UNREPRESENTABLE"* (`ARCHITECTURE.md:227`).
4. **Roster (box liveness / addressing):** `RosterList`, `RosterWatch` (stream)
   (`kernel.proto:277-278`). `Node` carries name, Tailscale `address`, `os`, `arch`, `port`,
   `intent` (`kernel.proto:192-207`); `Liveness` is *"computed LOCALLY … never gossiped"*
   with states `ALIVE/SUSPECT/DEAD/UNKNOWN` + a `Reason` (`kernel.proto:209-232`).
5. **Introspection:** `Info` returns the caller's assigned `namespace` and
   `max_payload_bytes` (*"read it; never hardcode a guess"*) (`kernel.proto:244-251, 280`).

**Delivery guarantee = at-most-once, always** (`ARCHITECTURE.md:162-166`): *"the kernel never
queues, never retries, never redelivers."* Durability is pushed up to plugins, which get four
tools: local journal, stamped `message_id`, an interest signal, a liveness signal
(`ARCHITECTURE.md:168-174`). The `Interest` enum (`kernel.proto:53-58`) answers "is anyone
listening?" but is *"a HINT … never the ACK"* (`ARCHITECTURE.md:204`, `kernel.proto:52-53`).

### The plugin model = general tools with consistent interfaces

The kernel→plugin side is `plugin.proto`, *"deliberately tiny: lifecycle only. ALL data flows
the other way, through KernelService, which the plugin calls. That is why the plugin API and
the REST API are the same service"* (`plugin.proto:1-3`). `PluginService` is **4 lifecycle
methods**: `Describe`, `Start`, `Stop`, `Health` (`plugin.proto:66-71`).

- A plugin's **manifest** (`PluginManifest`, `plugin.proto:37-44`) declares its `namespace`
  (*"owns `<namespace>.*` and its own storage. THE identity"* `plugin.proto:38`), version,
  API version, its `ChannelDecl`s, and its `InterestDecl`s — its *"MAXIMAL surface,
  inspectable without running it"* (`plugin.proto:42`). This is Greg's registration idea
  implemented literally (`ARCHITECTURE.md:270-278`, `BRIEF.md:69`).
- **Interests are exactly two kinds** (`plugin.proto:25-35`): a `ChannelInterest` (pattern +
  optional group) or a `RosterInterest` ("tell me when the node list changes"). *"and that is
  the point"* (`plugin.proto:24`).
- **Channels declared statically, refined dynamically within what was declared**
  (`ARCHITECTURE.md:276`, `plugin.proto:12-14`): a conflicting *type* for a name is rejected
  at registration *"before any byte moves."*
- **Plugins hold NO in-memory state across a reload** (`plugin.proto:63-65`,
  `ARCHITECTURE.md:431-437`): a plugin is *"a pure function of kernel-held state plus its
  inputs."* State lives in kernel storage.

**Four shipping plugins**, each an ordinary Go program, each using only kernel surface
(`ARCHITECTURE.md:284-293` table):

| Plugin | Responsibility | Kernel surface used |
|---|---|---|
| **comms** | agent↔agent messages; owns durability, retry, de-dup, end-to-end ACK, outbox/inbox | `JournalAppend(sync)`, `JournalRead`, `Publish`, `Subscribe`, `Request/Serve/Respond`, `KVPut`, `LookupGet`, `RosterWatch` |
| **registry** | the agent list — names, presence, "what is this agent doing" | `LookupPut/Get/List`, `KVPut`, `RosterWatch` |
| **logtail** | watch transcript files, ship lines onto channels | `JournalAppend`, `Publish`, `KVPut` |
| **ssh** | continuously verify every box can reach every box; hand agents a verified command | `RosterList/Watch`, one LOOKUP channel |

The ssh plugin is cited as *"the best evidence the boundary is drawn right … The kernel needs
zero SSH knowledge"* (`ARCHITECTURE.md:293`).

### Live reload (the "swap tooling without stopping" property)

Mechanism: `hashicorp/go-plugin` v1.8.0 — reload = kill the child, exec the new binary,
`fleetd` never stops; measured 4–9ms (`ARCHITECTURE.md:404-418`). Free consequences: crash
isolation (a plugin panic is an ordinary `error` at the call site, `ARCHITECTURE.md:422`) and
a deliberate-reload-vs-crash diagnostic (`ARCHITECTURE.md:423`). The kernel owns a drain gate
because go-plugin's own GracefulStop is broken (`ARCHITECTURE.md:427-429`). The honest limit:
**the kernel itself cannot be live-reloaded** — the only mitigation is keeping the kernel tiny
(~3000-line advisory tripwire) (`ARCHITECTURE.md:451-453`, `ROADMAP.md:314`).

### Transport decision (implementation detail behind an interface)

Verdict: embed `nats-server` core (JetStream OFF) as a Go library in each `fleetd`; full mesh
over Tailscale IPs; agents connect over loopback (`ARCHITECTURE.md:317`). Crucially this is
**hidden behind a 6-method Go interface in one file**: *"The API contains no reference to NATS
… `Publish/Subscribe/Request/Serve/Interest/SetPeers/Close` in one file,
`internal/kernel/natstransport.go` — the only file permitted to import `nats-server`"*
(`ARCHITECTURE.md:386`). *"The 19-RPC API is what Greg approves. NATS is an implementation
detail"* (`ARCHITECTURE.md:388`). No-box-in-charge is measured: kill 2 of 3 boxes, survivor
serves everything; cold-boot alone in ~28ms with all peers dead (`ARCHITECTURE.md:329-351`).

### Walkthroughs proving the layering

Three end-to-end walkthroughs show data crossing machines through the transport while the
kernel stays domain-blind: daemon boot/join (`ARCHITECTURE.md:461-501`), cross-box agent
message awake-then-asleep (`ARCHITECTURE.md:503-563`), and SSH-howto
(`ARCHITECTURE.md:565-597`). Each shows a plugin composing kernel primitives (`LookupGet` +
`RosterList` + `JournalAppend` + `Publish`) with **no new kernel verb**.

---

## Mapping to the operator's 2026-07-21 target

The operator named four properties. All four are already present in the plan; the mapping is
near one-to-one.

| Operator's 2026-07-21 target (INPUTS.md §3) | Where the plan already delivers it | Fit |
|---|---|---|
| **A data transport layer hosted by the daemon that connects multiple machines** | The kernel's messaging RPCs over the embedded-NATS mesh; channels with a name+type; `fleetd` one-per-box (`kernel.proto:256-261`, `ARCHITECTURE.md:120, 317`) | **Strong match.** This *is* the plan's core. |
| **An identity / address system so components know what other systems are available** | Two mechanisms: `LOOKUP` channel = replicated address book / name resolution (`kernel.proto:29-32, 263-265`) + `Roster` = which boxes are alive & their Tailscale address (`kernel.proto:187-242`). Agent names are hierarchical `<node>/<local>` (`ARCHITECTURE.md:266`). | **Strong match.** LOOKUP + roster is exactly "who/what is reachable." |
| **Plugins use the transport to generically move information** | Plugins declare channels+interests and call `Publish/Subscribe/Request/Serve` — the kernel moves opaque bytes and never interprets them (`plugin.proto:37-44`, `kernel.proto:39`) | **Strong match.** "Generic" is enforced by `bytes payload`. |
| **General tools with consistent interfaces — NOT tooling mushed into one hard-to-test bundle** | The kernel/plugin line + `bytes`-not-`Any` + no-`namespace` + separate-process plugins speaking one shared `KernelService`; each plugin independently testable and reloadable (`ARCHITECTURE.md:70-71, 114, 227, 282-293`) | **Strong match — this is the plan's explicit thesis.** The whole doc is an argument *against* mushing. |

**Where the plan is already ahead of the framing:** it not only proposes the layers, it
supplies the *enforcement* (boundary test, dependency-allowlist test, `buf breaking` at
PACKAGE so the service surface is an append-only contract, `ARCHITECTURE.md:618`) and explicit
**kill criteria** for when the kernel has absorbed too much domain knowledge
(`ROADMAP.md:307-317`). The operator's "consistent interfaces / easy to test" goal is
operationalized as testable gates, not just prose.

**Where the plan is thin against the new framing:**

1. **It is fleet/box-scoped, not harmonik-component-scoped.** The plan's "identity" is
   *machines and agents*. The operator's coupling worries (INPUTS §4) are about
   *keeper/watchdog/harness ↔ queue/comms/bead/DOT*. The plan never addresses how *harmonik's
   existing subsystems* map onto plugins — comms is a plugin, but queue, beads, DOT, keeper,
   watchdog, harnesses are not modeled at all. That mapping is un-done work.
2. **No container / worker-execution story.** The 2026-07-21 options (pull-based worker,
   central server, minimal-harmonik-in-container; INPUTS §2) and incus are absent. The plan
   assumes long-lived daemons on owned boxes, not ephemeral containers dialing in.
3. **The transport is explicitly *not* a request/response job-dispatch bus for remote work.**
   It carries messages/lookups/liveness; running a bead in a remote container is out of scope.
4. **Addressing is Tailscale-IP + config-file allow-list** (`ARCHITECTURE.md:396, 464-468`) —
   no dynamic worker *registration* (the operator's Option 2 "workers register with the
   central node"). LOOKUP could carry it, but it isn't designed for join/leave of ephemeral
   workers.

---

## Layering / decoupling content

The plan is unusually explicit about layers-building-on-interfaces vs hard-coding — this is
the part most directly useful to the operator's decoupling thesis (INPUTS §4).

- **The whole design is a decoupling argument.** Greg's own words the plan is built on
  (`BRIEF.md:51`): the alternative he *rejected* was *"a plugin could connect into the comms
  system … Then the comms system is hard coded into the daemons internals."* The channels
  abstraction exists precisely to avoid hard-coding a subsystem into the daemon. This is the
  operator's "too much layer-to-layer hard-coding" complaint, pre-solved.
- **Comms is a plugin, not kernel** (`ARCHITECTURE.md:284`, `BRIEF.md:53`) — the single most
  important decoupling decision. The thing harmonik hard-codes (comms) becomes a swappable
  tool over a generic transport.
- **One service surface, reused** (`plugin.proto:1-3`): the plugin API *is* the REST/curl API
  because all data flows plugin→kernel through `KernelService`. No second private host API —
  the plan explicitly refuses `GRPCBroker` to avoid *"a second, private, plugin-only host API
  — a drift surface. One surface"* (`ARCHITECTURE.md:643`).
- **State lives in one place (kernel storage), not scattered per-plugin.** Greg's storage
  instinct (*"the plugins dont make up their own thing"*, `BRIEF.md:67`) turns out to be *"the
  precondition that makes reload safe"* (`ARCHITECTURE.md:433`). Decoupling of state from code.
- **Interfaces as append-only contracts** (`ARCHITECTURE.md:618`, `ROADMAP.md:335-336`): `buf
  breaking` at PACKAGE means the service surface is the contract; you cannot silently delete a
  method. This is "consistent versioned interfaces between layers" made mechanical
  (`BRIEF.md:73-76` is Greg asking for exactly this: *"define it COMPLETELY independently of
  any code"*).
- **Fate isolation as a decoupling principle** (`ARCHITECTURE.md:268`): the roster keeps its
  own dedicated network listener because *"a failure detector that shares fate with the thing
  it monitors is not a failure detector."* Directly analogous to the operator's worry that
  keeper/watchdog are entangled with the thing they watch.
- **Boundary is the real test, applied at M4** (`ROADMAP.md:221-224`): *"comms needs a kernel
  RPC that isn't in the 19 → the boundary is wrong."* Two consecutive plugins needing new
  kernel RPCs is a project kill criterion (`ROADMAP.md:315`). This is a concrete method for
  *validating* a layer boundary rather than asserting one.
- **On the harmonik-side coupling the operator names:** the plan does touch harmonik's
  internals as *migration targets*, not as a decoupling model — e.g. delete `commscursor.go`
  (323 lines) because the daemon becomes the single writer via `KVPut(if_revision)`
  (`ARCHITECTURE.md:301`, `ROADMAP.md:200`), and delete the `fsyncBoundaryEventTypes` policy
  map that *"drifted and silently downgraded durability"* (`ARCHITECTURE.md:609`,
  `ROADMAP.md:201`). It does **not** model keeper/watchdog/harness↔queue/DOT coupling — that
  is a gap this new plan must fill.

---

## Reusable-as-foundation vs drop-with-premise

### Reuse as the foundation (independent of the rejected premise)

1. **The kernel/plugin split and the boundary line** (`ARCHITECTURE.md:54-114`) — the core
   architectural bet; entirely premise-independent.
2. **The reconciled proto contract** — `KernelService` (19 RPCs) + `PluginService` (4
   lifecycle) + manifest/interest model (`kernel.proto` whole, `plugin.proto` whole). This is
   a concrete, lint-clean, boundary-clean, cross-compiling interface (`ARCHITECTURE.md:124-135`).
3. **Channel abstraction (name + type; 4 types)** as the transport primitive
   (`kernel.proto:15-33`).
4. **LOOKUP + roster as the identity/address system** (`kernel.proto:187-242, 263-265`,
   `ARCHITECTURE.md:240-268`).
5. **Box-local kernel storage with per-call durability, no namespace field, never replicated**
   (`kernel.proto:128-185`, `ARCHITECTURE.md:221-238`).
6. **Live-reload via separate-process plugins + kernel-held state + drain gate**
   (`ARCHITECTURE.md:404-437`).
7. **Transport hidden behind a 6-method interface** so the product choice is swappable
   (`ARCHITECTURE.md:386-388`) — directly serves "consistent interfaces, don't hard-code."
8. **The enforcement + kill-criteria discipline** (boundary test, dep-allowlist, PACKAGE
   contract, ~3000-line tripwire, "two plugins needing new RPCs = stop")
   (`ROADMAP.md:307-317`, `ARCHITECTURE.md:114`).
9. **The refusal list** (`ARCHITECTURE.md:624-649`) — what NOT to build (no consensus/quorum,
   no fleet-wide order, no kernel queuing, no plugin-to-plugin addressing) is reusable design
   guardrail regardless of premise.

### Drop or de-emphasize (tied to the "data-sharing / centralized-searchable-memory" premise)

1. **The "centralize all agent learning, make it searchable" goal as the *reason* to build**
   (`BRIEF.md:28, 95`, `ARCHITECTURE.md:33`). Keep search only as an optional downstream
   consumer if wanted; do not let it motivate the substrate.
2. **The logtail + archive + search backbone milestone (M6)** as a headline deliverable
   (`ROADMAP.md:237-249`). It is the purest expression of the rejected premise (ship all agent
   transcripts to a central archive for search). Defer or drop; it does not gate the substrate.
3. **The registry-plugin "what is every agent doing, fleet-wide" framing** (`ROADMAP.md:228-233`)
   — the *agent-presence-as-shared-state* is premise-flavored. The *mechanism* (LOOKUP-based
   naming) is reusable; the *goal* of a fleet-wide agent activity view is premise-tied.
4. **Motivating anecdotes** ("something figured out on one machine cannot be used on another",
   `BRIEF.md:25-28`) — these framed the problem as data-sharing; they should not drive the new
   plan's requirements.

### Caveats to carry forward (the plan's own honest gaps, `ARCHITECTURE.md:653-674`)

- **Milestone 0 is unclosed:** nobody has tested a real macOS laptop sleep; the whole
  "laptop is just a peer" premise is unvalidated (`ROADMAP.md:20-43`). Top risk.
- **LOOKUP is the only component with no upstream library** (~150-250 lines) and everything
  routes through it, with a disk-wipe correctness edge (mitigated to ~10 lines via roster
  `boot_id`, but unbuilt) (`ARCHITECTURE.md:658`).
- **The kernel is "on the edge of light"** — 19 RPCs, 11 storage-shaped
  (`00-README.md:92`). A heavier kernel would betray the whole thesis.
- **Scope was explicitly 3 owned boxes / trusted Tailscale, no untrusted machines**
  (`BRIEF.md:89`, `ARCHITECTURE.md:229`). The new plan's container/worker directions push past
  this boundary and will need the security posture re-opened.

---

## Source files

Read in full: `ARCHITECTURE.md`, `BRIEF.md`, `00-README.md`, `ROADMAP.md`,
`design/25-reconciled-proto/fleet/kernel/v1/kernel.proto`,
`design/25-reconciled-proto/fleet/kernel/v1/plugin.proto` (all under
`/Users/gb/github/harmonik/plans/2026-07-15-agent-substrate-v2/`). Supporting
`design/20-24` and `investigate/10-15` used via `ARCHITECTURE.md` citations.
