# 20 — The Kernel: the daemon's entire API

**Date:** 2026-07-15
**Scope:** BRIEF §3.1 in full — channels, storage, roster, plugin registration, the protobuf API, and the boundary.
**Status:** Complete spec. The `.proto` files in this document are real: they lint clean under `buf` STANDARD, they compile, they generate Go + ConnectRPC code, and that code cross-compiles to the DGX. Everything labelled [MEASURED] I ran today.

Companion artifacts, both real and both copied verbatim from the working build at `/tmp/kproto`:
- **`design/20-kernel-proto/fleet/kernel/v1/{kernel,plugin}.proto`** — the kernel API. Lints clean, builds, generates, cross-compiles.
- **`design/20-kernel-boundary-test.sh`** — the §10.2 enforcement script. Runnable; it caught two boundary violations in my own draft, and then a false negative in itself (§10.2).

Labels used throughout: **[MEASURED]** I ran it, this is the output. **[CLAIMED]** an upstream doc says so, I did not verify. **[DEDUCED]** I reasoned from the other two — flagged because BRIEF §9 says confident deduction is how the last round died.

---

## 0. Verdict up front

The kernel is **19 remote procedure calls and one rule**.

> **The kernel moves bytes between named, typed channels; it writes bytes down when a plugin asks; it knows which boxes are alive. It does not know what any byte means.**

Four decisions carry the whole design:

1. **The kernel is at-most-once on every channel type, always.** It never queues, never retries, never redelivers. Durability, retry, dedup, and acknowledgement are plugin decisions (BRIEF §4) — and the kernel makes them cheap by handing the plugin exactly four tools: local storage, a stamped `message_id`, an interest signal, and a liveness signal.
2. **Storage is box-local and plugin-private by construction.** There is no `namespace` field in any storage call — the kernel derives it from the caller, so reading another plugin's storage is not *forbidden*, it is *unrepresentable*. Storage is never replicated. If you want data on another box, publish it on a channel. **Replication, like durability, is a plugin decision.**
3. **The kernel replicates exactly one thing, and only because it can do so with no conflict-resolution policy:** LOOKUP channels are **single-writer-per-key by construction** — a node owns every key it writes. No conflicts exist, so no resolver exists, so CAP never comes up. This is how BRIEF §6's *"I dont want to get hung up on CAP theorem and shit"* gets honoured rather than deferred.
4. **There is no fleet-wide message order, and the kernel will not pretend otherwise.** A global order requires a single writer; a single writer is a fatal hub; BRIEF §4 forbids fatal hubs. Therefore: every sequence number is per-`(node, channel)`. This is the decisive answer to the salvage doc's top open question, and it is a refusal, not a solution.

**Transport: embedded core NATS in a full mesh, JetStream off.** This contradicts a stated preference of Greg's and I argue it loudly in §2 rather than sliding it past him.

**The channel types shipped first: `PUBSUB`, `POINT_TO_POINT`, `REQUEST_REPLY`, `LOOKUP`.** Four, not five — "fanout" and "pubsub" are one primitive (§4.3).

---

## 1. Terms, defined once

Nothing below is used before it is defined here.

| Term | Plain meaning |
|---|---|
| **Kernel** | The part of the daemon that is compiled in and always present. The opposite of a plugin. |
| **Channel** | A named pipe with a declared behaviour. `comms.msg` is a channel. The kernel moves bytes through it and never looks inside. |
| **Envelope** | The wrapper the kernel puts around a plugin's bytes: which channel, which box, what time, what id. The bytes inside are the **payload**. |
| **Payload** | The plugin's actual data. To the kernel it is an opaque blob — literally typed `bytes`, so the kernel *cannot* parse it even by accident. |
| **Namespace** | A plugin's private prefix. The `comms` plugin owns channels named `comms.*` and storage keys nobody else can name. |
| **protobuf** | A way to write down a data format and an API in a plain text file, independently of any programming language, and generate code from it. Greg asked for this by name and for a stated reason. |
| **`.proto` file** | The text file that *is* the contract. |
| **RPC** (remote procedure call) | A function call that goes over a network or a socket instead of staying inside one program. |
| **gRPC / ConnectRPC** | Two ways to actually send an RPC. **ConnectRPC** is the one that also answers plain `curl`. |
| **buf** | The tool that lints `.proto` files and tells you whether a change to them breaks existing callers. Installed here at `~/go/bin/buf`, v1.71.0 [MEASURED]. |
| **At-most-once** | Every message is delivered zero or one times. It is never duplicated, and it may be lost. The opposite is at-least-once (never lost, may be duplicated) — which needs storage and retries, which is why it's a plugin's job. |
| **Roster** | The list of boxes and whether each is up. "Who's around." |
| **Gossip** | Boxes periodically tell each other what they know, instead of all reporting to a central server. No hub. |
| **memberlist** | HashiCorp's Go library that does gossip membership. The thing under Consul and Nomad. |
| **Broker** | A process that receives messages and forwards them. A middleman. |
| **Full mesh** | Every box talks directly to every other box. No middle. |
| **Raft / quorum** | An algorithm where a majority of machines vote to agree on one history. Needs more than half alive. In a 3-box fleet, 2 must be up or it stops. This is the thing Greg is trying to avoid. |
| **JetStream** | The optional disk-persistence layer bolted onto NATS. **This is the part that uses Raft.** We turn it off. |
| **MTU** | The biggest packet a network link will carry. Tailscale's is 1280 bytes [MEASURED, today: `ifconfig utun0` → `mtu 1280`]. |
| **WAL** (write-ahead log) | A SQLite mode where writes go to a side file first. Makes writes fast and crash-safe. |
| **go-plugin** | HashiCorp's library for running plugins as separate programs that talk over gRPC. The thing under Terraform and Vault. |
| **Live reload** | Replacing a plugin's code while the daemon keeps running. |

---

## 2. The loud part: I am recommending NATS, and Greg said not to

**Design rule says say this loudly rather than assume it away. So:**

Greg wrote (BRIEF §4): *"Part of the problem is that I dont trust my boxes. So I strongly lean away from NATS."* I am recommending NATS anyway. Here is the honest argument, and Greg should push back if it doesn't land.

**His requirement and his product preference are two different things, and the brief itself separates them.** BRIEF §4 defines the requirement precisely: *"Read this precisely: **no single box's death may kill the system.** It is NOT a claim about Byzantine faults, and NOT a request for consensus or HA rigor. He wants no hub whose loss is fatal."* That is a property. "Avoid NATS" is a guess about which product has that property.

**The guess is wrong, and it's wrong in a specific, measurable way.** NATS is two systems in one binary:

| | Uses Raft? | Fatal hub? | Verdict vs §4 |
|---|---|---|---|
| **Core NATS** — pub/sub, queue groups, request/reply, headers, no-responders, server gossip | **no** | **none** | ✅ passes |
| **JetStream** — durable streams, KV, object store | **yes** | 2-of-3 down = dead | ❌ violates |

A sibling read every `startRaftNode` / `bootstrapRaftNode` call site in nats-server v2.14.3 and found all four inside `jetstream_cluster.go` (lines 1046, 1082, 2990, 2993); with JetStream off the `raftNodes` map stays empty and the Raft machinery is inert [CLAIMED — I did not re-read the source myself; investigate/11 §2(c)].

And it was measured on the real fleet, not argued: 3 embedded servers in a full mesh, **two of three killed, and the lone survivor still served pub/sub and request/reply with zero errors**; a node **boots alone in 30ms with both peers dead** and **rejoins in 30ms** [MEASURED — investigate/11 §2(a), `/tmp/natslab/`].

**The last round's mistake was the topology, not the product.** BRIEF §9 says the prior round chose *"a hub-and-spoke NATS topology on the DGX"*. The salvage agent then tried to un-hub it and found it **structurally impossible** — nats-server rejects a mesh of leafnode links outright with `Loop detected for leafnode account="$G"` → `Protocol Violation`, and the attempt tore down the working link too [MEASURED — investigate/15]. Leafnodes are a tree. **But routes are not.** Core NATS *routes* form a full mesh and gossip; that is a different feature that the prior round did not use.

**What we are actually choosing is Greg's own idea.** His middle path was *"a broker on every node."* Core NATS in a full mesh **is literally that**: each box runs its own embedded broker in-process, agents talk to their local broker over loopback, brokers route to each other, and there is no middle. It is already Go, already a library (~15 lines to embed [MEASURED — investigate/11 §2(d)]), 24 modules, 15 MB.

**And the feature he objected to is the feature the brief already threw away.** §4 says *"durability is a PLUGIN decision, not a kernel guarantee."* JetStream is kernel-level durability. Turning it off is not a sacrifice we make to dodge his objection — it is what §4 already told us to do. The objection and the requirement point the same way.

**Two independent siblings converged on this** (investigate/11 from the transport side, investigate/15 from the salvage side), from different evidence.

**Why not mangos/NNG** (investigate/10's recommendation — a real disagreement between siblings, so I'll be specific):

- **It structurally cannot deliver a stated §5 requirement.** Greg: *"we actaully should probably notify the sender that the receiver is not listening. I believe we can find that out."* The ZeroMQ Guide, on its own model: *"ZeroMQ doesn't do this, and **there's no way to layer it on top because subscribers are invisible to publisher applications**"* [MEASURED quote — investigate/10, `/tmp/zguide/chapter5.txt:148`]. Core NATS returns `no responders` **in 0 seconds despite a 5-second timeout**, cluster-wide, across nodes [MEASURED — investigate/11].
- **Its own author measured it losing 98.9% of messages, silently.** 100,000 published with **0 send errors**; 1,094 received [MEASURED — investigate/10, `mangos_durability_loss.go`]. That is the exact failure mode the kernel must never have: silent loss with a successful return code.
- **The fleet evidence is asymmetric.** NATS was measured over the real Tailscale link (3.67ms best RTT, 900KB payload at 101ms, cross-compiled and deployed to the DGX). mangos was measured on loopback only, and that agent flagged it as *"the most important untested thing in the document."*
- **Maintenance:** nats-server v2.14.3 is current [MEASURED today]. mangos' last release is v3.4.2, 2022-08-11 — 3.9 years, older than every ZMQ option [MEASURED — investigate/10, whose own author calls this "the weakest link in the recommendation"].

**What mangos wins on, honestly:** it is a library with nothing to run, and its SURVEY pattern is a neat roll-call. But the kernel's roster is memberlist (§6), so SURVEY is redundant here, and "nothing to run" is also true of an embedded NATS server — it is a Go library in the same process.

**The hedge, and it is cheap.** The kernel API in §8 does not contain the word NATS. Transport sits behind one Go interface (§9.1) with six methods. If the sleeping-laptop test in §12 fails badly, swapping the implementation is one file. **The 19-RPC API is what Greg is approving; NATS is an implementation detail behind it.**

---

## 3. The shape, in one picture

```
        one box (x3: gb-mbp, dgx, gb-mac-mini)
   ┌──────────────────────────────────────────────────┐
   │  fleetd (the daemon)                             │
   │  ┌────────────────────────────────────────────┐  │
   │  │ KERNEL — 19 RPCs, knows nothing            │  │
   │  │  channels ── transport ──┐                 │  │
   │  │  storage  ── SQLite      │                 │  │
   │  │  roster   ── memberlist ─┼── Tailscale ────┼──┼──> other 2 boxes
   │  │  lookup   ── replicated  │   (routes+gossip)│  │
   │  └───────┬────────────────┬─┘                 │  │
   │  gRPC over│unix socket    │ ConnectRPC on     │  │
   │  (go-plugin)              │ loopback:7777     │  │
   └──────────┼────────────────┼────────────────────┘ │
              │                │                      │
       ┌──────▼─────┐   ┌──────▼──────┐        ┌──────▼──────┐
       │ comms      │   │ agent-list  │        │ an agent    │
       │ (plugin)   │   │ (plugin)    │        │ with curl   │
       └────────────┘   └─────────────┘        └─────────────┘
       separate OS processes                   no SDK needed
```

Two listeners, **one service**. Plugins reach `KernelService` over a unix socket; agents and `fleetctl` reach *the same* `KernelService` over loopback HTTP. That is BRIEF §3.2's *"the same interfaces could be available through REST/pubsub/whatever"* — not as an aspiration, as the same Go handler on two ports.

---

## 4. Channels

### 4.1 What a channel is, exactly

**A channel is a `(name, type)` pair, declared by a plugin, held in a registry in the kernel.**

It is **not** an object you open, hold, or close. There is no channel handle, no connection, no lifecycle. A channel exists because some plugin declared it, and it is addressed by name on every call. This matters: a channel with no state is a channel that cannot leak, cannot be half-open, and cannot survive a plugin reload in a broken condition.

The kernel stores per channel: `name`, `type`, `owner` (namespace), `public` (may other namespaces subscribe?), `declared_at`. That is the whole record.

**What the kernel knows about a channel: its name, its type, and who owns it. What the kernel knows about the bytes inside: nothing, enforced by the type `bytes` (§10).**

### 4.2 Naming — the grammar

Channel names are dot-separated lowercase tokens. Formal grammar:

```abnf
channel-name = namespace "." local-name
namespace    = token                      ; == the declaring plugin's namespace
local-name   = token *( "." token )
token        = ALPHA-LOWER *( ALPHA-LOWER / DIGIT / "-" / "_" )
ALPHA-LOWER  = %x61-7A                    ; a-z  (must START with a letter)
DIGIT        = %x30-39

pattern      = token *( "." ( token / "*" ) ) [ "." ">" ]
             ; "*" matches exactly ONE token
             ; ">" matches one or more trailing tokens; only valid as the LAST token
```

Hard limits, enforced by the kernel and rejected at declaration:

| Rule | Value | Why |
|---|---|---|
| total length | ≤ 200 bytes | it rides on every envelope |
| token count | ≤ 12 | |
| token length | ≤ 48 | |
| case | lowercase only — **reject**, do not fold | folding `Comms` → `comms` hides a typo; rejecting shows it |
| first token | must equal the declaring plugin's namespace | this is what makes ownership structural |
| reserved | `kernel` | see §4.6 |

Examples: `comms.msg.dgx.pi-refactor-3` ✅ · `logtail.line.claude` ✅ · `Comms.Msg` ❌ (case) · `comms..msg` ❌ (empty token) · `2fast` ❌ (leading digit).

**Why this grammar:** it is a strict *subset* of NATS subject syntax, which uses the same `*` and `>` semantics. Being a subset means it costs zero to implement today and does not marry us to NATS — a subset is portable to any transport, whereas using NATS' full syntax (which allows uppercase and more) would be a leak. [DEDUCED, but the subset relationship is not in dispute.]

### 4.3 The starting type set — and why four, not five

Greg's menu: *"publish/lookup table, point to point, pubsub, fanout, whatever"* plus request/reply (*"the internet is based on that - probably a good idea, lol"*).

**Ship: `PUBSUB`, `POINT_TO_POINT`, `REQUEST_REPLY`, `LOOKUP`.**

**"Fanout" and "pubsub" collapse into one type, and this is the one place I override the menu.** In every transport that offers both, they are the same primitive: *deliver a copy to every interested subscriber*. The only difference is whether a subscriber names the channel exactly or by pattern — and pattern matching is free. Shipping two names for one mechanism creates a distinction plugins will get wrong and the kernel cannot enforce.

A sibling flagged the ambiguity as worth asking Greg about (investigate/10: does "fanout" mean *everyone gets a copy* or *exactly one worker gets it*?). **My answer makes the question moot: both readings are already shipped.** If "fanout" meant broadcast → that's `PUBSUB`. If it meant a work queue → that's `POINT_TO_POINT`. There is no third thing. So the set is safe under either reading and no round-trip to Greg is needed to start building. (Still worth telling him the word was ambiguous, so he isn't surprised his list came back one item shorter.)

**"Point to point"** — I kept his exact word. It means the classic messaging sense (JMS "point-to-point"): **competing consumers, exactly-one delivery** among a named group. If he meant "a direct 1:1 link from A to B", that's the degenerate case — a group with one consumer — so again, covered either way.

**"Publish/lookup table"** → `LOOKUP` (§4.5). This is the one that needed real design.

### 4.4 The semantics table

This is the contract. **Every row of "kernel guarantees" is deliberately weak; every row of "plugin owns" is deliberately load-bearing.**

| | **PUBSUB** | **POINT_TO_POINT** | **REQUEST_REPLY** | **LOOKUP** |
|---|---|---|---|---|
| **Delivery** | at-most-once to **every** subscriber whose pattern matches, fleet-wide | at-most-once to **exactly one** member of each named group | at-most-once; one request → zero or one reply | at-most-once delta broadcast + convergent replicated state |
| **Ordering** | FIFO per `(origin_node, channel)`. **No fleet-wide order.** | same | n/a | per writer, by `revision`. **Revisions from different writers are incomparable.** |
| **Slow receiver** | kernel **drops for that subscriber only**, then **closes its stream with `RESOURCE_EXHAUSTED` naming the drop count**. Never silent. | same | request times out; caller gets `DEADLINE_EXCEEDED` | slow node lags; anti-entropy (§4.5) repairs it |
| **Absent receiver** | publish returns `INTEREST_NONE`. Message goes nowhere. **Kernel does not queue it.** | `INTEREST_NONE`; not queued | returns `INTEREST_NONE` **immediately, not after the timeout** | writes still land locally and replicate on rejoin |
| **Dead receiver** | roster marks it `DEAD`/`LEFT` in ~6s (~0.6s if announced); interest drops to `NONE` within gossip lag | same | same | writer's entries **remain readable** and are tagged with its `writer_node`; the kernel does **not** delete them |
| **Kernel guarantees** | routing; at-most-once; metadata stamping; interest signal; loud drops | + exactly-one-of-group | + reply correlation; + immediate no-responder signal | + single-writer-per-key; + convergence; + full local copy on every box |
| **Plugin owns** | **durability, retry, dedup, ACK, cross-box ordering, backpressure policy** | same | same + idempotency | value schema, key naming, **name-clash policy** |

**The load-bearing sentence:** *the kernel never writes a channel message to disk on its own.* Storage exists (§5), but a channel does not touch it unless a plugin explicitly calls `JournalAppend`. That is precisely BRIEF §4's *"durability is a PLUGIN decision, not a kernel guarantee"* — the kernel supplies the mechanism and never the policy.

**How BRIEF §5 is satisfied without kernel durability.** Greg's model: *"all the messages are written down. If an agent is subscribed, then they get a notification. If they are polling, then they read their messages the next time they come in."* In this design that is entirely the **comms plugin**, and it is three calls:

1. `JournalAppend("msgs", record)` — *written down*, durably, in kernel storage (SQLite, WAL, fsync — §5).
2. `Publish("comms.msg.<addressee>", doorbell)` — *the subscriber gets a notification*.
3. A poller calls `JournalRead("msgs", after_seq=<cursor>)` — *they read their messages next time they come in*. The cursor lives in kernel KV, not a file the plugin invented (§5.3).

The kernel contributes storage, a doorbell, and an id. The **policy** — how long to keep, when to retry, what an ack means — is comms'. §5 is met; §4 is not violated. The salvage doc notes harmonik's `jsonlwriter.go` (413 lines, *"best file in the repo"*) already **is** step 1, which suggests this decomposition matches something that already works.

**The ACK gap (§5), answered concretely.** Greg: *"we actaully should probably notify the sender that the receiver is not listening. I believe we can find that out."* **He is right, and here is exactly how — but it is two different mechanisms and only one is free:**

- **`REQUEST_REPLY`: free and exact.** Core NATS propagates subscription interest cluster-wide; a request with no responder anywhere returns in **0 seconds despite a 5-second timeout** — a positive signal, not a timeout [MEASURED — investigate/11].
- **`PUBSUB` / `POINT_TO_POINT`: not free. The kernel builds it.** No transport gives "is anyone listening" for a fire-and-forget publish. But the kernel *already knows every subscription*, because every subscription goes through `Subscribe` on `KernelService`. So the kernel publishes its own subscription set as a LOOKUP channel `kernel.interest` — **single-writer-per-node**, which is exactly the primitive §4.5 already provides. `INTEREST_NONE` then becomes a **local map lookup: microseconds, no network, no timeout.**
- **The honest caveat, stated as loudly as the feature:** interest is *eventually consistent*. `INTEREST_PRESENT` means "someone was listening as of the last gossip", **not "it was delivered"**. `INTEREST_NONE` can be stale. **This is not an end-to-end ACK and must never be sold as one.** An end-to-end ACK requires storage and retries, i.e. it is a plugin's job — and the kernel gives that plugin the `message_id` to correlate on and the roster to know where to retry.

### 4.5 LOOKUP — the one replicated thing, and why it needs no conflict resolution

`LOOKUP` is Greg's *"publish/lookup table"*: publish a value under a key, anyone can look it up later. It is the one place channels and storage meet.

**The design constraint that makes it safe: the writing node owns every key it writes.** A `LookupPut` records `writer_node = me`. Another node may write the *same key name*, but that creates a **separate entry under a different writer** — it never overwrites. Therefore:

- **Two writers can never produce conflicting versions of one entry.** There is no last-writer-wins. There is no vector clock. **There is no conflict-resolution policy because there are no conflicts.** This is how §6's *"I dont want to get hung up on CAP theorem and shit"* is honoured structurally rather than deferred.
- **`revision` is monotonic per writer and is compared only within one writer.** It is never compared across nodes, so skewed clocks are irrelevant. This directly obeys the salvage doc's rule: *"never order cross-machine messages by wall clock."*
- **Replication is unilateral.** Each node repairs other nodes' view of *its own* keys. No coordination.

**Name clashes: the kernel returns every claimant and refuses to pick.**

```proto
message LookupGetResponse { repeated LookupEntry entries = 1; }   // note: repeated
```

If two boxes both claim the agent name `pi-refactor-3`, `LookupGet` returns **two entries**, each tagged with its `writer_node` and `revision`. The plugin decides. This answers investigate/14's open question (*"What resolves a name clash? Undesigned"*) with a deliberate non-answer: **the kernel makes the clash visible instead of silently resolving it.** Silent resolution is a judgement, and BRIEF §2 says the system *"never judges"*. In the common case the list has one element and the plugin ignores the subtlety.

**Anti-entropy (the repair path).** Each node keeps, per LOOKUP channel, a version vector `map[writer_node]max_revision`. On route reconnect and every 30s it publishes a digest on `kernel.sync.<channel>`; anyone holding higher revisions for a writer sends the missing deltas.

Note the second half: **any node holding the data may serve the repair, not just the writer.** This is safe *only because* of single-writer-per-key — a given `(writer, revision)` has exactly one possible value, so any holder is a valid replica. Without that constraint this would be unsound. It closes a real hole: dgx writes keys → dgx dies → gb-mac-mini was asleep and missed them → if only the writer could repair, mac-mini could *never* learn them.

**The correctness precondition, stated plainly:** the writer's revision counter **must persist across restarts** (it lives in that node's own kernel KV, which is SQLite on disk). If a node's disk is wiped and it restarts at revision 1, it can re-issue a `(writer, revision)` pair with a *different* value, and the read-repair reasoning above breaks. **[DEDUCED — this is the sharpest edge in the design and I have not implemented or tested it.]** Mitigation: persist the counter; on detecting a lost counter, generate a fresh `incarnation` and include it in the writer identity. Listed in §12.

**Budgets, because LOOKUP is replicated to every box and held in memory:**

| Limit | Value | Why |
|---|---|---|
| entry value | ≤ 64 KB | |
| entries per channel | ≤ 10,000 | |
| total LOOKUP memory | ≤ 64 MB | the mac-mini has 37GiB free (BRIEF §1); this is not tight, it is a **tripwire** |

The kernel **rejects** an over-budget write with `RESOURCE_EXHAUSTED`. It does not truncate. Rationale is measured, not stylistic: investigate/13 pushed 1,563 bytes into memberlist's 512-byte metadata slot and got `err = nil` **with corrupt truncated JSON at the receiver** — and had the delegate not truncated defensively, `memberlist.go:460` would have `panic`ed the daemon. I re-verified both facts today [MEASURED]:

```
$ grep -n "MetaMaxSize" .../memberlist@v0.6.0/net.go
83:	MetaMaxSize            = 512 // Maximum size for node meta data
$ grep -n "longer than the limit" .../memberlist@v0.6.0/memberlist.go
460:			panic("Node meta data provided is longer than the limit")
519:			panic("Node meta data provided is longer than the limit")
```

**Reject loudly > truncate silently > panic.** That ranking is a kernel-wide rule.

**Liveness never deletes data.** If dgx is dead, its LOOKUP entries stay readable and stay tagged `writer_node: dgx`; the reader can consult the roster and decide. The kernel does **not** garbage-collect on liveness — the laptop sleeps every night, and a kernel that dropped its keys nightly would be actively harmful.

### 4.6 Who creates a channel: the plugin, statically, at registration

Three options were available — a config file, dynamic creation on first publish, or static declaration by the plugin. **Static declaration in the plugin manifest wins.**

- **Not a config file:** the plugin knows its own channels; a config file is a second place to get them wrong.
- **Not dynamic-on-first-publish:** if a channel springs into being on first use, two plugins can disagree about its *type* and **the kernel has no basis to arbitrate**. Type mismatch across boxes (pubsub here, queue-group there) is a silent, awful bug — messages randomly vanish.
- **Static declaration** gives four things: (1) conflicting types are rejected **at registration, before any byte moves**; (2) the plugin's full surface is inspectable **without running it** (`fleetctl plugin describe comms`); (3) it is the Benthos "config schema as data" pattern that investigate/14 measured as strictly better than Telegraf's opaque `SampleConfig() string`; (4) **live reload can diff the new binary's manifest against the old one and refuse a reload that would change a live channel's type** — a whole class of reload disaster, rejected by construction.

**Static declaration, dynamic refinement.** The manifest declares *patterns* (the maximal surface); the plugin subscribes to concrete names at runtime *within* them. `comms` declares `comms.msg.>` and subscribes to `comms.msg.dgx.pi-refactor-3` when that agent appears. A runtime `Subscribe` outside the declared patterns is **rejected** — which is a bug-catcher, not a security control.

**Cross-namespace access:** subscribing outside your own namespace requires (a) an explicit manifest interest and (b) the owner having marked the channel `public: true`. Publishing outside your namespace is impossible — the first token must be your namespace.

**Registry consistency across boxes is detected, never resolved.** Manifests gossip via the kernel-owned LOOKUP channel `kernel.plugins` (single-writer per node). If dgx declares `comms.msg` as PUBSUB and gb-mbp declares it POINT_TO_POINT, **every box logs a loud mismatch and `fleetctl channels` shows it in red. The kernel does not pick a winner** — that would need consensus, which needs quorum, which §4 forbids. In practice the same plugin binary registers everywhere so declarations match; this check exists to catch the day they don't.

**Reserved kernel channels** (namespace `kernel`, public read-only, no plugin may publish):

| Channel | Type | Carries |
|---|---|---|
| `kernel.interest` | LOOKUP | each node's subscription set → powers the `Interest` signal (§4.4) |
| `kernel.plugins` | LOOKUP | each node's registered plugin manifests → powers `fleetctl plugins` and the future plugin-library plugin (§3.2's "[Idea]") |
| `kernel.sync.<channel>` | internal | anti-entropy digests (§4.5) |

Note `kernel.plugins` is exactly what BRIEF §3.2's *"a plugin gets added to one node, the plugin gets synced across nodes"* needs to even begin — and it falls out of LOOKUP for free.

---

## 5. Storage

> *"One thing that might be useful is to have a storage mechanism in the daemon. Then the plugins dont make up their own thing."* — BRIEF §3.1

### 5.1 Why this is not a convenience feature

Kernel storage is **the precondition for live reload**, and that reframing is the most useful thing in this section.

investigate/12 measured go-plugin reloads five times, reading back state written before each one: `state after reload: ""` — **every time, gone**. This is not a bug; it is the defining property of the family. Vault hit the same wall and drew the same conclusion: *"Plugins don't maintain persistent in-memory state... State management relies on Vault's storage backend"* [CLAIMED, authoritative — HashiCorp docs, via investigate/12].

So the rule is not a suggestion:

> **A plugin is a pure function of kernel-held state plus its inputs. A plugin that holds state in memory is a plugin that silently loses it on reload.**

Greg asked for storage and asked for live reload as separate wishes. **They are the same wish.** Vault discovered this the expensive way; investigate/12 and the prior round both independently re-derived it (the salvage doc calls host-owned storage its *"strongest [surviving] item"*, reached independently by Greg himself in §3.1 — two derivations, now three).

The prior round found the other half of the reason: host-owned storage makes plugin-owned cursor files **inexpressible**, which kills a bug class. This design takes that literally (§5.3).

### 5.2 Engine: SQLite, pure Go — MEASURED

**`modernc.org/sqlite` v1.53.0** — a pure-Go SQLite. Not the cgo one.

This is a hard requirement, not a preference: **the DGX has no Go toolchain** (`go: command not found` [MEASURED — investigate/13]), so everything ships as a cross-compiled static binary, so **`CGO_ENABLED=0` is mandatory**. I verified the whole chain today rather than trusting it:

```
=== NATIVE RUN (darwin/arm64, CGO_ENABLED=0) ===
journal_mode = wal
kv get -> "seq=42"
log append: 10000 records in 44ms (227483/sec), max seq=10000
  read seq=9998 rec="record-9997"

=== CROSS-COMPILE TO DGX (linux/arm64, CGO_ENABLED=0) ===
kstore-linux-arm64: ELF 64-bit LSB executable, ARM aarch64, statically linked
size: 9.0M
=== dependency tree size ===
26 modules
```
[MEASURED — `/tmp/kstore`, full source in Sources]

227,000 appends/sec against a fleet of 3 boxes and tens of agents is ~1000x more headroom than needed. WAL mode works with cgo off. It cross-compiles statically to the DGX. 26 modules.

**Why not bbolt/Pebble** (both also pure Go, both faster): **a human can open a SQLite file at 2am and just look.** `sqlite3` is already on this box (`/usr/bin/sqlite3`, v3.51.0 [MEASURED]). For a 3-box fleet with one operator, inspectability beats throughput at a ratio that isn't close. Prefix scans, TTL expiry and the journal's monotonic sequence are all one line of SQL each.

**Durability knobs are the plugin's, not the kernel's** — consistent with §4. Default `synchronous=NORMAL`; a plugin that wants an fsync barrier per record asks for it per call (`durable: true` on `JournalAppend`). The kernel's default must not silently be either "fast and lossy" or "slow and safe" on the plugin's behalf.

### 5.3 The API, and the thing that is deliberately absent

Two shapes: **KV** (`KVPut/Get/Delete/List/Watch`) and **Journal** (`JournalAppend/Read/Truncate`). Both are namespaced per plugin.

> **Look at what is missing: there is no `namespace` field in any storage RPC.**

```proto
message KVGetRequest { string key = 1; }              // <- no namespace
message JournalAppendRequest { string journal = 1; repeated bytes records = 2; }
```

The kernel derives the namespace from the calling plugin's registered identity and prefixes server-side. **Reading another plugin's storage is not forbidden — it is unrepresentable.** There is no field to put the wrong value in. That is a stronger guarantee than an ACL check, and it costs nothing.

**Honest scope of that claim:** this is a **bug-prevention boundary, not a security boundary**. The operator has root on the box and can open the SQLite file; `fleetctl kv` (§9.3) does exactly that for debugging and doesn't pretend otherwise. BRIEF §4 says trusted network, all three boxes are Greg's. The boundary exists to stop `comms` from accidentally stomping `logtail`'s cursor, not to stop an attacker.

**Storage is box-local and is never replicated.** State it as a rule:

> **If you want data on another box, publish it on a channel. Replication, like durability, is a plugin decision.**

This is the second-strongest simplification in the design (after at-most-once). It means the kernel's store has no conflict resolution, no quorum, no leader, no CAP argument, and cannot be the reason a box's death hurts another box. The **only** replicated thing is LOOKUP, and only because single-writer-per-key removes conflict entirely (§4.5).

**Cursors: the bug class this kills.** `JournalRead` takes `after_seq`. The cursor is stored with `KVPut("cursor/<whatever>", seq)`. A plugin *could* write a cursor file — but it would have to invent the file, the format, the fsync discipline, the crash-recovery, and the cleanup, all outside the kernel's view, and it would lose it on reload anyway. Making the ergonomic path the correct path is the point.

**TTLs:** yes, on KV and LOOKUP. **Honest caveat:** TTL is evaluated against the **local box's wall clock**. It is a cache expiry, **not a distributed lease**. Do not build mutual exclusion on it. Stated here because someone will try.

**Watches:** `KVWatch(key_prefix)` streams PUT/DELETE/EXPIRE events. This is half of BRIEF §3.1's *"the plugin would say what resources it was interested in/needed to react to"*; the roster is the other half (§6).

**Journals are per-node, forever.** Sequence is monotonic per `(plugin, journal)` **on this box only**. Repeating the headline because it is the single most consequential refusal in the design:

> The kernel does **not** order records across boxes and will not pretend to. A fleet-wide order needs a single writer; a single writer is a fatal hub; BRIEF §4 forbids fatal hubs. **Read N journals and merge by a rule you chose.**

The salvage doc named this its top unresolved question and noted the prior round's answer — the hub's `stream_seq` — **died with the hub**. Of its menu, it leaned toward "one log per node + per-node cursors" while flagging it as unmeasured. **This design takes that option and makes it structural**: there is no API through which a plugin could request a global order, so no plugin can accidentally depend on one. It also fixes the specific bug the salvage doc found: harmonik sorts by `bytes.Compare` over UUIDv7 event ids in 4 load-bearing places, which across 3 skewed clocks sorts by *skew*, not causality.

---

## 6. The roster in core

BRIEF §3.1 is unambiguous: *"The machine roster probably should be baked in."* It is the one thing Greg explicitly wants in the kernel. Building on **investigate/13**, whose measurements I take as sound and partly re-verified.

### 6.1 Library: `hashicorp/memberlist` v0.6.0 — and why the kernel carries two "membership" things

**Confirmed today:** `go list -m -versions github.com/hashicorp/memberlist` → **v0.6.0** [MEASURED].

There is an apparent smell here worth confronting: if the transport is core NATS (which gossips server addresses and forms a full mesh), why also run memberlist? Because **they answer different questions**:

| | NATS routes | memberlist roster |
|---|---|---|
| Answers | "is there a TCP route to dgx's broker, and does anyone subscribe to X?" | "is the **box** dgx alive, at what address, is it sleeping **on purpose**?" |
| Layer | data plane | **directory** |

**And the roster is upstream of the transport, which is what makes it load-bearing rather than decorative:** the roster supplies the Tailscale IPs that become NATS routes, and `s.ReloadOptions(newOpts)` [CLAIMED — `reload.go:1417`, via investigate/11] lets the kernel add/remove routes live without restarting. Bootstrap is not circular: `config file (3 Tailscale IPs) → memberlist → roster → NATS routes`.

**Why memberlist over a hand-rolled ping list** — and this is the honest version. Three boxes is 3 links; "ping everyone, mark down after N misses" is ~200 lines. **The library does not win on scale. It wins on rejoin**, and rejoin is what `gb-mbp` does to you every single day. investigate/13 measured a 75-second freeze (`SIGSTOP`, the closest available analogue to sleep):

```
23:40:50.881 [dgx]         LEAVE/DEAD gb-mbp     <- 6.6s after freeze
23:40:50.882 [gb-mac-mini] LEAVE/DEAD gb-mbp     <- 1ms apart
...
23:41:59.994 [dgx]         JOIN gb-mbp           <- 0.7s after wake
2026/07/15 23:41:59 [WARN] memberlist: Refuting a suspect message (from: dgx)
```
[MEASURED — investigate/13 §5]

That `Refuting` line is the incarnation counter beating a stale "gb-mbp is dead" rumour still in flight. A hand-rolled list re-kills that node and flaps forever. **You would write incarnation numbers, suspicion and anti-entropy yourself, badly, and debug them at 11pm.**

**Why not etcd/ZooKeeper/Consul:** consensus needs a quorum. With 3 boxes, the laptop can't be a voter (it sleeps), leaving 2 voters, and **a 2-voter quorum tolerates zero failures**. That is a fatal hub — the exact §4 violation, and the exact shape of the last round's mistake. Also: Greg said *"I dont want to get hung up on CAP theorem and shit"*, and these products are a CAP argument with a binary attached.

### 6.2 Config: three settings that are not defaults, each for a measured reason

| Setting | Value | Why |
|---|---|---|
| `UDPBufferSize` | **1252** (not 1400) | **The landmine.** Default is 1400 [MEASURED today: `config.go:336: UDPBufferSize: 1400`]; Tailscale MTU is 1280 [MEASURED today: `ifconfig utun0` → `mtu 1280`]. 1400+8+20 = **1428 bytes on a 1280-byte link**. Vicious failure mode (investigate/14): TCP push/pull still syncs every 30s so it *"mostly works"*, while UDP probes vanish and nodes flap suspect/alive forever. 1252 = 1280−20−8. **Two independent siblings found this. It is the same Tailscale-MTU gotcha that already bit the monitoring stack on these boxes, in a new costume.** |
| `BindAddr`/`AdvertiseAddr` | **the Tailscale IP** | not `0.0.0.0`. The roster lives on the tailnet and nowhere else. |
| `Name` | the roster name | **not `os.Hostname()`** — `gb-mac-mini` reports its hostname as `gb-mac-mini.local`, silently creating a second, wrong identity [MEASURED — investigate/13]. |
| `SecretKey` | set | cheap; a random device joining the tailnet cannot join the roster. |
| everything else | **defaults** | 6s detection is already far better than this fleet needs. **Do not tune what you have not measured a problem with.** |

**Membership is an explicit allow-list of the 3 boxes, not "everything on the tailnet."** I confirmed why today [MEASURED]:

```
$ tailscale status
100.87.151.114  gb-mbp        macOS  -
100.115.27.55   dgx           linux  active; direct 192.168.1.86:52291
100.120.22.74   gb-mac-mini   macOS  active; direct 192.168.1.219:50955
100.78.163.71   gb-mac-old    macOS  offline, last seen 38d ago
100.82.109.44   gb-mac        macOS  offline, last seen 16d ago
```

There are **five** boxes on this tailnet and only three are fleet members. A roster that trusted the tailnet would carry two permanently-dead ghosts.

### 6.3 What the roster carries — and the 512-byte problem, dissolved

investigate/13 called the 512-byte metadata cap *"the single most important implementation constraint in this document"* and designed a two-tier scheme around a size budget (Tier 1 = small stuff in `Meta`; Tier 2 = agent list via 30s TCP push/pull).

**I am adopting the two tiers but re-cutting the line, and it makes the cap stop mattering.** Instead of splitting by *size*, split by *purpose*:

> **Tier 1 (memberlist `Meta`) = the minimum you need in order to open a connection to this box. Nothing else, ever. Kernel-owned; no plugin can write it.**
> **Everything else = LOOKUP channels over the data plane.**

The `Node` message (§8) is name, address, node_id, os, arch, port, intent, schema — **~200 bytes, fixed, with no free-text field and no plugin-writable slot**. It cannot grow to 512 because there is nothing in it that varies with load.

This dissolves the cap **by construction rather than by budgeting**, and it deletes the two problems the budget approach carries: (1) the agent list — the thing that actually blew the cap in investigate/13's test (1,563 bytes → silent corruption) — is a **plugin's** LOOKUP channel with a 64KB budget and no UDP packet to fit in; (2) investigate/13 §8.3 asked for *"a slot in Tier-1 metadata the plugin can write"* for the SSH host-key fingerprint, and noted the kernel would have to police the byte budget to avoid a plugin panicking the daemon. **With no plugin-writable slot, that risk is gone**: the SSH plugin puts its fingerprint in its own LOOKUP channel (`sshkeys.hostkey`, single-writer = that node), which is ~50 bytes in a 64KB budget instead of ~50 bytes in a 512-byte budget with a panic behind it.

Tier 2 also gets faster as a side effect: LOOKUP rides the NATS mesh (~4ms RTT measured) rather than memberlist's 30s push/pull, so investigate/13's own open question about needing broadcast-queue deltas for a live agent list [its open question #2, unmeasured] **does not arise**.

**Liveness is computed locally and never gossiped** — I am taking this from investigate/13 unchanged and it is right. `Liveness{state, last_seen}` where `last_seen` is **this box's clock**. Peers never exchange opinions about third parties' timestamps; memberlist's suspect/refute already handles disagreement, correctly (two boxes converged 1ms apart, measured). Gossiping timestamps across unsynchronised clocks is how you get a roster that argues with itself.

**Asleep vs dead must be announced, not inferred.** SWIM has three states — alive, suspect, dead — and a frozen laptop is byte-for-byte identical to a dead one from the outside. **No library can distinguish them because the distinction exists only in intent.** So `Node.Intent` is a first-class field: before sleeping, the daemon sets `INTENT_SLEEPING` and calls `Leave()` (**~0.6s** to propagate, vs **~6s** to infer death, and no false-suspicion window) [MEASURED — investigate/13 §5.3]. A box that dies without that sequence is genuinely dead. **Gotcha inherited and worth repeating:** the `Node` passed to `NotifyLeave` reports `State=0` (Alive) even for a graceful leave, so **you cannot read `n.State` in the callback to tell "left" from "dead"** — anyone who assumes otherwise writes a bug.

### 6.4 Liveness as a routing input

BRIEF §5: *"in case one of its agents wants to communicate with an agent on the dead box."* Liveness is a routing input, not a status display. Because it is local state on every box, the send path never touches the network to find out:

```go
// local map lookup. microseconds. no timeout. no hang.
switch k.roster.Liveness("dgx").State {
case Dead:  return ErrNodeDown{Node: "dgx", LastSeen: t}
case Left:  return ErrNodeAsleep{Node: "dgx", LastSeen: t}   // it TOLD us
}
```

Worst-case window where a sender thinks a dead box is alive: **~6 seconds** [MEASURED]. The alternative today is a 30-second TCP timeout that is indistinguishable from a typo, a firewall, or a bad key.

**The roster is the kernel's answer to *"agents dont have to figure that out"* (§4).** The resolution chain has no DNS, no `known_hosts`, no `~/.ssh/config` in it:

```
agent name ──(agent-list PLUGIN, a LOOKUP channel)──> node name ──(kernel roster)──> Tailscale IP ──> dial
```

investigate/13 measured why this matters and it is worse than Greg described: **from `dgx`, SSH to every peer fails, all six addressing forms** (no private key, empty `known_hosts`); from the laptop **1 of 9 forms works**; and `ssh dgx` from the laptop **silently goes over the home LAN**, not Tailscale. `ssh dgx` works from the mac-mini and fails from the laptop; `ssh 100.115.27.55` works from the laptop and fails from the mac-mini — **exactly inverted**. There is no single command an agent can be told to use. The kernel's answer is to delete the step where anything has to *find out*: **the daemon never resolves a name; it looks the peer up in the roster and dials the Tailscale IP it was handed.**

**The agent list is NOT in the kernel.** This is the boundary test for this whole section: the kernel knows what a **box** is, because it must dial one. It does not know what runs on a box. Greg said it himself: *"The idea of an agent list could probably also be a plugin."* That plugin uses `LOOKUP` — the same primitive the SSH plugin uses, the same primitive `kernel.interest` uses. **One mechanism, three consumers, and the kernel stays ignorant.**

---

## 7. Plugin registration

**Mechanism: `hashicorp/go-plugin` v1.8.0** — plugin = an ordinary Go binary, run as a child process, spoken to over gRPC on a unix socket. Live reload = kill the child, spawn the new binary, daemon never stops. [Confirmed today: v1.8.0 is current — MEASURED.] investigate/12 measured reload at **12ms warm** (~450ms first-exec of a fresh binary on macOS, which is Gatekeeper), crash isolation as OS-enforced and airtight, and cross-compilation to linux/arm64 in 1.8s producing a static ELF. Go's stdlib `plugin` package is disqualified: it **cannot reload at all** (`plugin already loaded`) and **cannot cross-compile** (needs cgo) — which is fatal, since the DGX has no Go toolchain.

### 7.1 The handshake

```
1. kernel spawns the plugin binary          (go-plugin magic cookie + protocol version)
2. plugin prints ONE line on stdout:        1|3|unix|/tmp/plugin2951246566|grpc|
3. kernel calls  PluginService.Describe()   -> PluginManifest
4. kernel VALIDATES the manifest            -> reject => kill child, log, do not retry-loop
5. kernel calls  PluginService.Start(node, kernel_endpoint, caller_id, api_version)
6. plugin calls back into KernelService     (Subscribe / Serve / KV / Journal / Roster...)
```

Step 4 is where the value is. Validation, in order:

| Check | On failure |
|---|---|
| `api_version` matches the kernel's major | reject: `FAILED_PRECONDITION` |
| namespace is well-formed, not `kernel`, not already claimed by a *different* plugin | reject |
| every `ChannelDecl.name` starts with this namespace and parses under §4.2 | reject |
| no declared channel conflicts in **type** with an existing registry entry | reject |
| every out-of-namespace interest names a channel declared `public` by its owner | reject |
| **on reload**: no live channel changes type vs the previous manifest | **refuse the reload, keep the old plugin running** |

That last row is why declaration is static and why it's worth the rigidity.

**Direction matters and is deliberate:** the kernel calls the plugin only for **lifecycle** (4 methods). **All data flows the other way**, through `KernelService`, which the plugin calls. That is why the plugin API and the REST API are literally the same service — and it is why `PluginService` is small enough to read in one screen.

### 7.2 Resource-interest declaration

BRIEF §3.1: *"the plugin would say what resources it was interested in/needed to react to. Maybe it needed changes in the node list."*

**Exactly two kinds of interest exist, and that is the point:**

```proto
message InterestDecl {
  oneof kind {
    ChannelInterest channel = 1;   // "deliver me messages matching this pattern"
    RosterInterest  roster  = 2;   // "tell me when the node list changes"  <- Greg's exact ask
  }
}
```

If a third kind ever seems necessary, that is a signal the kernel is growing domain knowledge — treat it as a design review trigger, not a ticket. (Storage watches are *not* a third kind: your own storage is yours, so `KVWatch` needs no declaration.)

**Validation of investigate/13's boundary test.** That doc ended by asking what the kernel must expose for the SSH plugin — the fiddliest plugin imagined — and answered: roster reads + change events, one Tier-1 slot, one Tier-2 slot, *"nothing else."* Under this design it needs **`RosterInterest` + one LOOKUP channel it declares itself**, and the Tier-1 slot request disappears (§6.3). A plugin that fiddly needing that little is the best available evidence the boundary is drawn in the right place.

### 7.3 Live reload, and the rule it imposes

```
fleetctl plugin reload comms
  -> kernel stops dispatching to comms
  -> drains in-flight (bounded, default 5s)
  -> Describe() the NEW binary, diff manifest vs old  -> REFUSE if a live channel's type changed
  -> verify binary sha256 against the manifest        (go-plugin SecureConfig, client.go:327-340)
  -> Stop(grace) -> Kill -> spawn new -> Start()
  -> resume dispatch
```

`SecureConfig` already does SHA256 verification of the plugin binary before exec — which is exactly what BRIEF §3.2's plugin-library idea needs, already written [CLAIMED — investigate/12].

**The rule, enforced from day one:** *plugins hold no in-memory state across a reload.* Not a style guide — a measured property (§5.1). The kernel makes the correct path the easy one: `KVPut` is right there, and there is nowhere else to put anything.

**What this does NOT give, stated plainly:** BEAM's `code_change/3` state migration. Nothing in Go gives it and nothing can — there is no mechanism to hand a struct built by one binary's compiler to a different binary's compiler and have it mean the same thing. investigate/12's framing is worth quoting because it's the crux of Greg's BEAM envy: **two Go options each give half of BEAM and they are different halves** — Yaegi gives code-loading with *anti*-supervision (a plugin panic hard-kills the daemon, measured `HOST EXIT CODE: 2`), go-plugin gives supervision with no code-loading. **We take the isolation half because it fails safely, and buy back state-survival by making the kernel own storage.** Erlang's own docs are the best argument here: hot code loading means `.appup`/`.relup` files and a race where *"new processes... can execute old code"*. Greg doesn't want `code_change/3` — he wants *"don't make me stop the whole system"*, and that is what this gives.

**The honest limit: the kernel itself is not live-reloadable.** Changing channels/roster/storage means restarting `fleetd` — which is close to the thing Greg actually complained about in harmonik. The only mitigation is keeping the kernel small enough that its restarts are rare. That is why §10 has teeth. Flagged in §12.

---

## 8. The protobuf API

**These files are real.** They lint clean under `buf` STANDARD, compile, generate Go + ConnectRPC, and cross-compile to the DGX:

```
=== buf lint ===   LINT CLEAN (STANDARD)
=== buf build ===  BUILD OK
=== generate ===   GENERATE OK
   gen/fleet/kernel/v1/kernel.pb.go
   gen/fleet/kernel/v1/kernelv1connect/kernel.connect.go
   gen/fleet/kernel/v1/kernelv1connect/plugin.connect.go
   gen/fleet/kernel/v1/plugin.pb.go
go build darwin/arm64 OK
go build linux/arm64 (dgx) OK
KernelService: 19 RPCs
     260 proto/fleet/kernel/v1/kernel.proto
      71 proto/fleet/kernel/v1/plugin.proto
```
[MEASURED — `/tmp/kproto`; copied to `design/20-kernel-proto/`]

Writing them — and *compiling* them — found three real defects a reviewer would have missed:
1. `Interest` collided: an enum in `kernel.proto` and a message in `plugin.proto`, same package → `` `Interest` declared multiple times ``. Renamed to `InterestDecl`. **Only the compiler catches this; no amount of reading does.**
2. The boundary test (§10) rejected my own first draft over `LogAppend` and `token`. Both renamed — **the kernel's storage primitive is a *journal*, not a log, because "log" is a domain word.**
3. The rename itself **silently failed**, and my own boundary test **passed the broken file**. See §10.2 — it is the most instructive thing in this document.

### 8.1 `fleet/kernel/v1/kernel.proto` — the whole kernel

Full file at `design/20-kernel-proto/fleet/kernel/v1/kernel.proto`. The load-bearing parts:

```proto
syntax = "proto3";
package fleet.kernel.v1;

// A channel is (name, type). The kernel moves bytes; the plugin owns meaning.
enum ChannelType {
  CHANNEL_TYPE_UNSPECIFIED = 0;
  CHANNEL_TYPE_PUBSUB = 1;           // exact-name subscribe = "fanout"; pattern = "pubsub". Same primitive.
  CHANNEL_TYPE_POINT_TO_POINT = 2;   // competing consumers; 1:1 is the degenerate case
  CHANNEL_TYPE_REQUEST_REPLY = 3;
  CHANNEL_TYPE_LOOKUP = 4;           // replicated map, single-writer-per-key BY CONSTRUCTION
}

// Fields 1-3 are the plugin's. Fields 10+ are stamped by the kernel and
// OVERWRITTEN if a caller sets them.
message Envelope {
  string channel = 1;
  bytes payload = 2;                  // OPAQUE. Never parsed by the kernel.
  map<string, string> headers = 3;    // plugin metadata; kernel does not read it

  string origin_node = 10;            // BRIEF §4: "the machine, time, etc must ride along"
  google.protobuf.Timestamp origin_time = 11;  // that box's clock. NOT a fleet order.
  string message_id = 12;             // the hook a plugin builds ACKs on
  uint64 origin_seq = 13;             // monotonic per (origin_node, channel). NEVER fleet-wide.
  string producer = 14;               // publishing plugin's namespace
}

enum Interest {
  INTEREST_UNSPECIFIED = 0;
  INTEREST_PRESENT = 1;   // someone is listening -- NOT "it was delivered"
  INTEREST_NONE = 2;      // nobody is listening; the send went nowhere
  INTEREST_UNKNOWN = 3;   // fleet view incomplete (a node is suspect)
}
```

**Three things to notice, each doing real work:**

- **`payload` is `bytes`, not `google.protobuf.Any`.** `Any` would require a type URL, which would require the kernel to carry a type registry, which would mean the kernel knows about plugin types. `bytes` means the kernel **cannot** parse the payload even if a future contributor wants it to. **This is the primary enforcement mechanism for §10** — the boundary is in the type system, not in a code review.
- **Fields 10–14 are kernel-stamped and unforgeable.** BRIEF §4 says *"the machine, time, etc must ride along because search will need it."* Making them plugin-supplied would make them wrong within a month.
- **`origin_seq` is named for its scope.** Not `seq`. Not `id`. A field called `seq` invites someone to sort by it across boxes; `origin_seq` makes the mistake visible at the call site. Same reasoning as `writer_node`/`revision` in `LookupEntry`.

The roster surface:

```proto
message Node {
  string name = 1;          // roster name = the key. Stable. NOT os.Hostname().
  string address = 2;       // the Tailscale IP. The ONE address. Never DNS, never LAN.
  string node_id = 3;       // stable external id (Tailscale node id)
  string os = 4;  string arch = 5;  uint32 port = 6;
  Intent intent = 7;  uint32 schema = 8;
  enum Intent { INTENT_UNSPECIFIED = 0; INTENT_UP = 1; INTENT_SLEEPING = 2; INTENT_DRAINING = 3; }
}
// Computed LOCALLY from this box's own observations. Never gossiped.
message Liveness {
  State state = 1;
  google.protobuf.Timestamp last_seen = 2;   // THIS box's clock
  enum State { STATE_UNSPECIFIED = 0; STATE_ALIVE = 1; STATE_SUSPECT = 2; STATE_DEAD = 3; STATE_LEFT = 4; }
}
```

And the whole service — **19 methods**:

```proto
service KernelService {
  // channels
  rpc Publish(PublishRequest) returns (PublishResponse);
  rpc Subscribe(SubscribeRequest) returns (stream SubscribeResponse);
  rpc Request(RequestRequest) returns (RequestResponse);
  rpc Serve(ServeRequest) returns (stream ServeResponse);
  rpc Respond(RespondRequest) returns (RespondResponse);
  // lookup (replicated, single-writer-per-key)
  rpc LookupPut(LookupPutRequest) returns (LookupPutResponse);
  rpc LookupGet(LookupGetRequest) returns (LookupGetResponse);     // returns ALL claimants
  rpc LookupList(LookupListRequest) returns (LookupListResponse);
  // storage (box-local, plugin-private: NOTE no namespace field anywhere)
  rpc KVPut(KVPutRequest) returns (KVPutResponse);
  rpc KVGet(KVGetRequest) returns (KVGetResponse);
  rpc KVDelete(KVDeleteRequest) returns (KVDeleteResponse);
  rpc KVList(KVListRequest) returns (KVListResponse);
  rpc KVWatch(KVWatchRequest) returns (stream KVWatchResponse);
  rpc JournalAppend(JournalAppendRequest) returns (JournalAppendResponse);
  rpc JournalRead(JournalReadRequest) returns (stream JournalReadResponse);
  rpc JournalTruncate(JournalTruncateRequest) returns (JournalTruncateResponse);
  // roster
  rpc RosterList(RosterListRequest) returns (RosterListResponse);
  rpc RosterWatch(RosterWatchRequest) returns (stream RosterWatchResponse);
  rpc Info(InfoRequest) returns (InfoResponse);
}
```

**Every method is unary or server-streaming. There is no bidirectional streaming anywhere, and that is a deliberate constraint, not an accident.** Bidi needs HTTP/2 trailers; avoiding it is what lets this exact service answer plain `curl` over HTTP/1.1 as well as a plugin over gRPC. That is why the responder side of request/reply is `Serve` (server-stream: receive requests) + `Respond` (unary: answer one, correlated by `request_id`) instead of the obvious bidi stream. investigate/11 measured a Connect handler answering `curl --http1.1` with JSON, binary protobuf, Connect server-streaming via curl, **and** a stock `google.golang.org/grpc` client — one `.proto`, one handler, one port. **BRIEF §3.2 is satisfied by construction, not by an adapter.**

### 8.2 `fleet/kernel/v1/plugin.proto` — the kernel→plugin side

Deliberately tiny — lifecycle only:

```proto
message PluginManifest {
  string namespace = 1;      // owns <namespace>.* and its own storage. THE identity.
  string version = 2;
  uint32 api_version = 3;
  repeated ChannelDecl channels = 4;
  repeated InterestDecl interests = 5;  // the MAXIMAL surface, inspectable without running it
  string description = 6;
}
service PluginService {
  rpc Describe(DescribeRequest) returns (DescribeResponse);
  rpc Start(StartRequest) returns (StartResponse);
  rpc Stop(StopRequest) returns (StopResponse);
  rpc Health(HealthRequest) returns (HealthResponse);
}
```

### 8.3 Versioning — MEASURED, and buf's own recommendation is wrong for us

**Use `buf breaking` at `PACKAGE` level, not the recommended `WIRE_JSON`.** investigate/14 found this; I reproduced it against **my** protos rather than trusting it [MEASURED — `/tmp/kproto`, buf 1.71.0]:

| Change to the kernel API | `WIRE_JSON` (buf's own rec.) | `PACKAGE` (this rec.) |
|---|---|---|
| **add** `Envelope.trace_id = 15` | PASS ✅ correct | PASS ✅ correct |
| **delete `rpc Respond`** from `KernelService` | **PASS ← WRONG** | **FAIL** ✅ `Previously present RPC "Respond" on service "KernelService" was deleted.` |
| renumber `payload` 2 → 20 | FAIL ✅ | FAIL ✅ |
| delete `origin_node` w/o `reserved` | FAIL ✅ | FAIL ✅ |
| `payload` `bytes` → `string` | FAIL ✅ | FAIL ✅ |
| **add enum value** `CHANNEL_TYPE_STREAM = 5` | — | **PASS ← see the enum hazard below** |

`WIRE_JSON` only cares about *encoding*, and deleting a method breaks no encoding — callers just get "unimplemented" at runtime. **For a plugin API the service surface IS the contract.** Use `PACKAGE`.

**The enum hazard, which no tool will catch for you.** Adding an enum value passes even at `PACKAGE` — correctly, since it's wire-safe. But an old plugin receiving `CHANNEL_TYPE_STREAM = 5` sees the bare number `5` and its `switch` falls through to `default`. **Therefore: fail closed on unknown enum values.** The kernel rejects a `ChannelDecl` whose type it doesn't know; a plugin rejects a delivery on a type it doesn't know. This is a code rule because it cannot be a tool rule.

**Backwards compatibility** (new kernel, old plugin): free, from protobuf's field rules + `buf breaking` at PACKAGE in CI.

**Forwards compatibility** (old kernel/plugin, new message): BRIEF §3.2 asks *"(maybe forwards?)"*. **The honest answer is: yes for data, no for behaviour, and there is one trap that will silently eat your fields.** I measured all three [MEASURED — `/tmp/fwd`]:

```
NEW kernel emits Envelope with trace_id="TRACE-ABC-123" (field 15), 40 bytes on the wire

PATH A -- OLD plugin passes it through as BINARY protobuf
  old plugin parsed it: channel="comms.msg". Its Envelope has no trace_id field at all.
  unknown-field bytes retained by the old plugin: 15
  after the OLD plugin re-emitted it: trace_id="TRACE-ABC-123" origin_node="gb-mbp"
  ==> SURVIVED -- forwards compatibility holds

PATH B -- SAME old plugin, but it round-trips through JSON
  old plugin -> JSON: {"channel":"comms.msg","payload":"aGk=","originNode":"dgx","messageId":"m-1"}
  back to binary, read by NEW kernel: trace_id=""
  ==> *** LOST *** -- the field was silently eaten
```

So, three rules:

1. **Forwards compatibility is real for pass-through, and it is free.** An old plugin retained 15 bytes it could not interpret and re-emitted them intact. This is what makes rolling upgrades across 3 boxes safe.
2. **It is NOT achievable for behaviour, at any cost.** An old plugin preserves a new field's *bytes*; it cannot act on its *meaning*. So: **additive data = forwards compatible, free. New required behaviour = `api_version` bump, not free.** There is no third option, and pretending otherwise is how you ship a fleet where half the boxes silently ignore an instruction.
3. **The trap, now measured: JSON destroys it.** Therefore — **binary protobuf on every kernel↔plugin and daemon↔daemon hop. JSON only at the human/REST edge, and never re-ingested.** `fleetctl` may print JSON; nothing may parse JSON and re-emit an envelope. This is the concrete cost of ConnectRPC's lovely curl-ability, and it needs to be a written rule because the failure is silent.

**Two-level versioning**, following go-plugin: a coarse integer `api_version` (kernel API major) checked at registration and refused loudly, sitting on top of protobuf's field rules. Coarse catches "this plugin is from a different era"; protobuf handles everything smaller.

**`.proto` files are the source of truth and generated code is checked in.** `buf` is not installed system-wide but is present at `~/go/bin/buf` v1.71.0 along with `protoc-gen-go`, `protoc-gen-connect-go`, `protoc-gen-go-grpc` [MEASURED — a sibling installed them; investigate/12's note that "protoc/buf are NOT installed" is stale as of today]. CI runs `buf lint`, `buf breaking --against` the previous tag at PACKAGE, and `buf generate` + `git diff --exit-code`.

---

## 9. Go interfaces, file layout, CLI

### 9.1 The internal seams

Three interfaces. **The first is the hedge on §2**: swapping NATS for mangos is one file.

```go
// internal/kernel/transport.go -- the ONLY place that knows what NATS is.
type Transport interface {
	Publish(ctx context.Context, ch string, t ChannelType, env *Envelope, group string) error
	Subscribe(ctx context.Context, pattern string, t ChannelType, group string) (Subscription, error)
	Request(ctx context.Context, ch string, env *Envelope, timeout time.Duration) (*Envelope, error)
	Serve(ctx context.Context, pattern string) (Responder, error)
	Interest(ch string) Interest        // local lookup, no network
	SetPeers(nodes []Node) error        // roster -> routes, live (nats ReloadOptions)
	Close() error
}

// internal/kernel/store.go -- ns is a Go parameter but is NEVER on the wire.
type Store interface {
	KVPut(ns, key string, val []byte, ttl time.Duration, ifRev *uint64) (uint64, error)
	KVGet(ns, key string) (val []byte, rev uint64, found bool, err error)
	KVDelete(ns, key string) (existed bool, err error)
	KVList(ns, prefix string, limit int) ([]KVEntry, error)
	KVWatch(ctx context.Context, ns, prefix string) (<-chan KVEvent, error)
	JournalAppend(ns, journal string, recs [][]byte, durable bool) ([]uint64, error)
	JournalRead(ns, journal string, afterSeq uint64, limit int) ([]JournalRecord, error)
	JournalTruncate(ns, journal string, beforeSeq uint64) (uint64, error)
	Close() error
}

// internal/kernel/roster.go
type Roster interface {
	List() (nodes []NodeStatus, self string)
	Watch(ctx context.Context) (<-chan NodeEvent, error)
	Self() Node
	SetIntent(Intent) error   // sleep hook: SLEEPING + Leave()
	Rejoin() error            // wake hook: Join(seeds), idempotent
}
```

`ns` being a Go parameter but absent from the wire is the exact seam where the "unrepresentable" property lives (§5.3): the kernel injects it from the authenticated caller in `service.go`, and it is the only place that can.

### 9.2 File layout

```
fleet/
  buf.yaml                       # lint: STANDARD   breaking: PACKAGE
  buf.gen.yaml
  proto/fleet/kernel/v1/{kernel,plugin}.proto     # the contract. source of truth.
  gen/fleet/kernel/v1/...                         # checked in; CI verifies no drift
  cmd/fleetd/main.go             # the daemon
  cmd/fleetctl/main.go           # the CLI
  internal/kernel/
      channel.go        # §4.2 grammar + the registry
      transport.go      # the Transport interface
      natstransport.go  # <- the ONLY file that imports nats-server
      store.go          # the Store interface
      sqlitestore.go    # <- the ONLY file that imports modernc.org/sqlite
      roster.go         # <- the ONLY file that imports memberlist
      lookup.go         # §4.5 replicated map + anti-entropy
      interest.go       # §4.4 gossiped interest table
      registry.go       # §7 registration + validation
      supervise.go      # <- the ONLY file that imports go-plugin
      service.go        # the 19 RPCs; injects namespace from caller identity
  plugins/comms/        # separate binary. separate go.mod. NOT imported by the kernel.
  test/boundary/        # §10. runs in CI.
```

The "ONLY file that imports X" comments are load-bearing and are enforced by §10.3, not by hope.

### 9.3 CLI

```
fleetctl nodes                       # roster: name, addr, os, intent, state, last-seen
fleetctl channels                    # registry: name, type, owner, public; MISMATCHES IN RED
fleetctl plugins                     # what's registered, on which box (via kernel.plugins)
fleetctl plugin describe comms       # the manifest = the plugin's entire surface, without running it
fleetctl plugin reload comms         # §7.3
fleetctl pub  <channel> [-H k=v]     # payload on stdin
fleetctl sub  <pattern>              # streams; this is the log-tail shape, over HTTP/1.1
fleetctl req  <channel>              # payload on stdin; prints INTEREST_NONE immediately if nobody serves
fleetctl lookup list <channel>       # shows writer_node + revision per entry -> clashes are visible
fleetctl kv   get|put|list <ns> ...  # ADMIN. reads any namespace. see §5.3: this is a debug tool.
```

`fleetctl sub` and `fleetctl pub` are the whole substrate, usable by a human in one line — which is also the answer to *"agents dont have to figure that out"*: an agent that can run `curl` can use the fleet.

---

## 10. What the kernel must NOT know — and how that is enforced

### 10.1 The list

The kernel must not know what any of these are:

> **agent, session, repo, log, log line, transcript, project, file, path, tmux, SSH, git, commit, branch, worktree, Claude, Codex, harmonik, model, prompt, LLM token, human, note, search, task, message.**

And the meta-rule that generates the rest: **the kernel must not know what a payload means.**

Some of these deserve a word, because they are the ones that will actually be argued about:

- **"log"** — BRIEF §3.2 names *log tail* and *log archiving* as **plugins**. The kernel's append-only primitive is therefore a **journal**. This is not pedantry; see §10.2.
- **"message"** — the kernel carries an `Envelope` with an opaque `payload`. A *message* between agents is a comms concept. The kernel must never have a field that means "who is this for".
- **"search"** — BRIEF §4: *"We handle search later - we can solve search once we have a data backbone."* Search is a **consumer**. The kernel's contribution is that `origin_node` and `origin_time` ride along (§8.1), and that is all.
- **"agent"** — the kernel knows what a **box** is, because it has to dial one. It does not know what runs on one. The agent list is a plugin over a LOOKUP channel (§6.4).

### 10.2 Enforcement #1: the vocabulary test — and it already earned its keep

Intent is not enforcement. This is a real script that runs in CI over the `.proto` files, checking **declared identifiers** (not raw text, so protobuf's own `message`/`service` keywords can't cause false hits):

Full script: **`design/20-kernel-boundary-test.sh`** (runnable; it is what produced the output below).

```bash
BANNED='agent|session|repo|log|tail|transcript|project|tmux|ssh|git|claude|codex|harmonik|
        prompt|model|token|human|note|search|task|worktree|commit|branch|file|path'
ALLOW='^(catalog|logic|dialog)$'   # substring matching needs an escape hatch; see below
```

**It rejected my own first draft** [MEASURED], and both hits were real, not pedantic:

1. **`LogAppend` / `LogRead` / `LogTruncate` → `JournalAppend` / `JournalRead` / `JournalTruncate`.** "Log" is a domain word here. A reader seeing `LogAppend` in the kernel would reasonably conclude the kernel knows about log files — and the log-tail plugin will own channels named `logtail.*` and storage of its own. Sharing the word invites exactly the confusion the boundary exists to prevent.
2. **`StartRequest.token` → `caller_id`.** "Token" collides with LLM token. It's a name tag, not a credential.

**Domain leakage arrives as names first.** That is why this cheap, slightly silly test has the highest signal-to-effort ratio of anything in this section — it caught two leaks before a single line of kernel code existed.

#### The test's own bug, recorded because it is the most instructive thing that happened

My **first version of this test was wrong, and I nearly shipped a fix that had not happened.** Two compounding bugs [both MEASURED]:

1. **BSD `sed` on macOS does not support `\b`.** My rename `s/\bLogAppend/JournalAppend/g` **silently did nothing** — no error, exit 0. The `.proto` still said `LogAppend`.
2. **The test then passed the un-renamed file.** I had "hardened" the pattern to `\blog\b`, and `\b` cannot match inside CamelCase: `LogAppend` has no word boundary between `Log` and `Append`. **So the test reported `VOCABULARY CLEAN` on a file containing three banned identifiers**, and I wrote that result into this document.

Caught only by grepping the delivered artifact instead of trusting the tool's output:

```
=== PROOF of bug 1: BSD sed \b silently no-op'd ===
161:message LogAppendRequest { string log = 1; repeated bytes records = 2; }
252:  rpc LogAppend(LogAppendRequest) returns (LogAppendResponse);
=== PROOF of bug 2: my v2 test passes a file containing LogAppend ===
VOCABULARY CLEAN -- the kernel names no domain concept.
   ^ FALSE NEGATIVE
```

Both are fixed: the rename was redone in Python, and the test now matches **substrings, case-insensitively**, with an explicit allowlist (`catalog|logic|dialog`) instead of relying on word boundaries. The regression test is kept:

```
-- file containing 'LogAppend' (the case the old test PASSED):
   BANNED IDENTIFIER in regress.proto: LogAppendRequest
   BANNED IDENTIFIER in regress.proto: LogAppend
>>> BOUNDARY VIOLATION   (exit 1 -- must be 1)
```

Two lessons, both worth more than the test itself. **(a) A test that has never failed on a real violation is not a test, it is decoration** — the `AgentInfo` negative test passed all along, which is exactly what made the false negative invisible; a guard needs a regression case per *pattern class*, not one case overall. **(b) Verify the artifact, not the tool's exit code.** This is the same failure mode BRIEF §9 describes at a different scale: a confident report resting on an unchecked assumption. It happened to me, in the small, while writing the section about preventing it.

**Proof it actually fails** (a test that never fails is decoration). I added the kind of thing a well-meaning contributor adds — an `AgentInfo` to `RosterListResponse`:

```
-- it compiles and lints just fine:
   buf build: OK
   buf lint : OK  <-- neither tool objects
-- but the boundary test:
   BANNED IDENTIFIER: 107:message AgentInfo
   BANNED IDENTIFIER: 108:  string agent_name = 1
   BANNED IDENTIFIER: 109:  string session_id = 2
   BANNED IDENTIFIER: 110:  string repo_path = 3
>>> BOUNDARY VIOLATION   (exit 1)
```
[MEASURED] **`buf build` and `buf lint` both accept it. Only the boundary test objects.** That is the gap this test exists to fill. The final API passes:

```
VOCABULARY CLEAN -- the kernel names no domain concept.
```

Exceptions live in an allowlist file, each with a comment. That friction is a feature: adding an entry forces a conversation.

### 10.3 Enforcement #2: the import boundary

`internal/kernel/**` may not import `plugins/**`, and the dependency allowlist is asserted in a test:

```go
// test/boundary/deps_test.go
func TestKernelImports(t *testing.T) {
	out, _ := exec.Command("go", "list", "-deps", "./internal/kernel/...").Output()
	for _, pkg := range strings.Fields(string(out)) {
		if strings.Contains(pkg, "/plugins/") { t.Fatalf("kernel imports a plugin: %s", pkg) }
		if isThirdParty(pkg) && !allowed[pkg] { t.Fatalf("kernel grew a dependency: %s", pkg) }
	}
}
```

The allowlist is short and every addition is a design decision: `nats-server`, `nats.go`, `memberlist`, `modernc.org/sqlite`, `go-plugin`, `connectrpc.com/connect`, `google.golang.org/protobuf`. **Plugins live in their own `go.mod` and depend only on `gen/` — never on `internal/kernel`.** A plugin that imports the kernel is not a plugin.

### 10.4 Enforcement #3: the type system

`payload` is `bytes`. Not `Any`. The kernel has no type registry and no payload codec, so **it cannot parse a payload even if someone wants it to.** Of the three mechanisms this is the strongest, because it needs no test to run and no reviewer to notice: the leak is not caught, it is *impossible to write*.

### 10.5 A smell check, not a gate

investigate/10 independently budgeted this kernel at **~1,200–2,200 lines** and noted the number is materially the same on any transport — which is its own useful finding (**the transport is not where the difficulty lives**). If `internal/kernel` passes ~3,000 lines, something has moved in that shouldn't have. Advisory: a CI warning, not a failure. Nobody should refactor to hit a line count; everybody should have to explain a 2x miss.

---

## 11. What we do NOT build

| Not built | Why |
|---|---|
| **Kernel durability / message replay / at-least-once** | BRIEF §4: *"durability is a PLUGIN decision."* The kernel is at-most-once, always. This is the design's spine, not a shortcut. |
| **JetStream (or any Raft, quorum, or leader)** | It is the only part of NATS with a fatal-hub failure mode: 2-of-3 down = dead, and an R1 stream silently bets your data on **one box the placement algorithm chose for you** [MEASURED — investigate/11]. §4 forbids it and §6 disclaims the rigor it buys. |
| **A fleet-wide message order** | Needs a single writer → a fatal hub → §4 forbids it. **The refusal is the feature**: with no API for it, no plugin can accidentally depend on it. |
| **Kernel-side conflict resolution / vector clocks / CRDTs** | The kernel replicates only single-writer-per-key data, where conflicts cannot occur. Everything else is a plugin's problem, with the plugin's blast radius. |
| **Overlap / conflict / duplicate-work detection** | BRIEF §6: *"That is not my instinct... I really dont even care about that."* The kernel carries messages and never judges. Its consumption of a large fraction of the last round is BRIEF §9's headline failure. |
| **RAG / vector search / embeddings / search** | BRIEF §6. Search is a downstream consumer. The kernel's entire contribution is `origin_node` + `origin_time` riding along. |
| **A separate ZooKeeper-like service** | KIP-500's own diagnosis: *"system administrators need to learn how to manage and deploy two separate distributed systems"* [CLAIMED — via investigate/14]. The roster is a library inside our daemon, ~300–400 lines around memberlist. |
| **Auto-discovery of boxes** | The roster is an explicit allow-list of 3. There are **5 boxes on this tailnet** and 2 are permanently offline ghosts [MEASURED, §6.2]. |
| **Auth, ACLs, mTLS, capability tokens** | BRIEF §4: trusted network, 3 boxes, all his. `caller_id` is a **name tag, not a lock** — it exists for attribution and namespacing. The prior round's AutoMTLS-on-loopback is rightly cut (investigate/15 agrees). |
| **Bidirectional streaming** | Would need HTTP/2 trailers and would cost the curl-ability that §3.2 asks for. |
| **`google.protobuf.Any` payloads** | Would put a type registry in the kernel. See §10.4. |
| **Go stdlib `plugin`, Yaegi, WASM** | stdlib: cannot reload (`plugin already loaded`) and cannot cross-compile — fatal, the DGX has no Go. Yaegi: abandoned (no release since 2024-04), broken generics, and **a plugin goroutine panic hard-kills the daemon** (`HOST EXIT CODE: 2`). WASM: a real runner-up, revisitable against the same `.proto` — but 7.8MB modules, 1.58s compiles, and one-instance-one-caller concurrency. [All MEASURED — investigate/12.] |
| **BEAM's `code_change/3`** | Impossible in Go; nothing can hand a struct from one binary's compiler to another's. We take supervision + isolation and buy back state via kernel storage. |
| **Kernel live-reload** | Out of reach; mitigated only by keeping the kernel small (§10.5). Flagged in §12. |
| **A DAG / plugins addressing each other** | Fluent-Bit-style name+match routing crosses machine boundaries; a Vector-style DAG must be re-wired at every hop [CLAIMED — investigate/14]. Greg's *"a channel had a name and a type"* already picked this side without knowing it. |
| **The kernel deleting data because a node is dead** | The laptop sleeps every night. A kernel that GC'd its keys nightly would be actively harmful. |

---

## 12. Open questions

Ordered by how much they'd hurt. The first three are worth answering before writing kernel code.

1. **Does the NATS route mesh survive a real macOS sleep, not a process kill?** [The most important untested thing here.] Every NATS measurement used `Shutdown()`/SIGKILL on loopback or a clean cross-tailnet run. A real sleep leaves a **half-open TCP connection**, and NATS' defaults (`PingInterval` 2 min × `MaxPingsOut` 2) suggest **~4 minutes** to reap a dead route — against memberlist's measured ~6s. If that's true, the `Interest` signal (§4.4) is stale for minutes exactly when it matters most, and the fix (tune the ping interval) is tuning a thing nobody has measured. **BRIEF §1 says gb-mbp "sleeps, reboots" — that is the fleet's normal state, not an edge case.** A siblings' open question too (investigate/11 #1, investigate/10). Half a day with `pmset sleep`.
2. **The LOOKUP revision-counter persistence hole.** §4.5's read-repair is sound *only* if a writer never re-issues a `(writer, revision)` pair with a different value. That requires the counter to survive restarts (it lives in SQLite) **and** a disk wipe to force a new incarnation. [DEDUCED — not implemented, not tested.] This is the sharpest edge in the design and it is mine, not inherited.
3. **Does memberlist actually converge over Tailscale at n=3 with `UDPBufferSize: 1252`?** The MTU cliff (1280) and the default (1400) are both measured, and packets ≥1300B are silently dropped on this fleet [MEASURED — investigate/14]. But **nobody has run three memberlist nodes across the real boxes.** investigate/13's 3-node test was all on 127.0.0.1. Half a day; de-risks the roster.
4. **The true 8-hour sleep.** investigate/13 measured a 75-second `SIGSTOP`. After 8 hours peers will have *reaped* the entry entirely, not merely marked it dead, and the real sleep takes the interface down. The repair path (30s push/pull anti-entropy + `Join(seeds)` on wake) is [DEDUCED], not observed. The unconditional `Rejoin()` on the wake hook exists precisely because this is unmeasured.
5. **Is "fanout" really redundant?** §4.3 argues both readings are covered by `PUBSUB` and `POINT_TO_POINT`, so building can start either way. But Greg listed five and gets four — worth 30 seconds of his time to confirm I didn't drop something he meant.
6. **Is `caller_id` enough identity for the loopback ConnectRPC port?** Any local process can claim any namespace. On a trusted single-user box that is fine and it is deliberate (§11). It becomes wrong the moment a non-owned machine joins — which BRIEF §6 puts out of scope *"for now"*. The word "now" is doing work.
7. **Does `plugin.Client.Kill()` drain in-flight gRPC calls or hard-kill?** Determines whether §7.3's drain step is ~30 lines of kernel code or free. investigate/12 confirmed it kills the process but did not trace outstanding streams.
8. **The kernel is not live-reloadable.** Changing channels/roster/storage restarts `fleetd` — close to what Greg actually complained about. Mitigated only by keeping the kernel small. **Whether "small enough that restarts are rare" is achievable is not answerable from a spec**; it needs the comms plugin built to find out.
9. **Anti-entropy cost at rest.** §4.5's digest exchange every 30s per LOOKUP channel per node is trivially cheap at n=3 with 3 channels. I did not model it and it does not need modelling at this scale — noted so that nobody assumes I did.
10. **What happens when a plugin subscribes to a channel whose owner is a plugin that isn't installed on this box?** Manifests gossip (`kernel.plugins`) so the kernel *can* know the channel exists elsewhere. Should `Subscribe` succeed and simply never deliver, or fail fast? [Undesigned.] My lean is succeed-and-report-`INTEREST_NONE`, which is consistent with §4.4 — but it is a lean, not a decision.
11. **`gb-mac-mini` was unreachable to two siblings during this work** (`Host key verification failed`). Its arch is assumed darwin/arm64, unverified. If anything in the fleet is amd64, the plugin-library idea needs 4 artifacts, not 2. It does not affect any decision here — but it is BRIEF §4's stated problem happening live, to the investigation itself.

---

## Sources

### Files read
- `/Users/gb/research/2026-07-15-agent-substrate-v2/BRIEF.md` — in full, first, before anything else.
- `/Users/gb/research/2026-07-15-agent-substrate-v2/investigate/13-roster-and-addressbook.md` — in full (assigned: "build on investigate/13").
- `/Users/gb/research/2026-07-15-agent-substrate-v2/investigate/11-transport-options.md` — in full. **Its provided summary was a placeholder ("Test"), so the doc had to be read directly; its actual content is the transport recommendation this design rests on.**
- Investigation summaries for 10 (zeromq), 12 (plugins/live-reload), 14 (prior-art), 15 (salvage), as supplied in the task.
- `~/go/pkg/mod/github.com/hashicorp/memberlist@v0.6.0/net.go` (`MetaMaxSize = 512`, line 83); `memberlist.go` (panic sites, lines 460, 519); `config.go` (`UDPBufferSize: 1400` line 336; `SuspicionMult: 4`, `ProbeInterval: 1s`, `ProbeTimeout: 500ms`, `PushPullInterval: 30s`, `GossipInterval: 200ms`, lines 314–323).
- **Not read, deliberately:** `/Users/gb/research/2026-07-15-agent-comms-substrate/` — per instructions; a sibling owns its salvage.

### Commands run (all today)
- `go version` → `go1.26.2 darwin/arm64`.
- `which buf protoc protoc-gen-go sqlite3 nats-server` → buf/protoc **not on PATH**; `sqlite3` at `/usr/bin/sqlite3` v3.51.0. Then `ls ~/go/bin` → **`buf`, `protoc-gen-go`, `protoc-gen-connect-go`, `protoc-gen-go-grpc` are installed there**; `~/go/pkg/mod/github.com/bufbuild/buf@v1.71.0`. `buf --version` → `1.71.0`.
- `go list -m -versions` → memberlist **v0.6.0**, nats-server **v2.14.3**, nats.go **v1.52.0**, connectrpc.com/connect **v1.20.0**, go-plugin **v1.8.0**, modernc.org/sqlite **v1.53.0**, protobuf **v1.36.11**.
- `ifconfig utun0` → `mtu 1280`. `tailscale status` → 5 boxes: gb-mbp/dgx/gb-mac-mini active, gb-mac-old (38d) + gb-mac (16d) offline.

### Experiments written and run
- **`/tmp/kstore/`** — pure-Go SQLite kernel store: `kv` + `journal` tables, WAL, namespace isolation, cursor read. Ran `CGO_ENABLED=0 go run` (darwin/arm64) → `journal_mode = wal`, 10,000 appends in 44ms (227,483/sec), monotonic seq. Cross-compiled `CGO_ENABLED=0 GOOS=linux GOARCH=arm64` → **statically linked aarch64 ELF, 9.0M**. `go list -m all` → 26 modules.
- **`/tmp/kproto/`** — the kernel API itself. `buf lint` (STANDARD) → clean; `buf build` → OK; `buf generate` → `kernel.pb.go`, `plugin.pb.go`, `kernelv1connect/{kernel,plugin}.connect.go`; `go build` OK on darwin/arm64 **and** linux/arm64. Found two real defects: `Interest` declared twice (enum + message, same package) and the two boundary violations in §10.2. **Copied verbatim to `design/20-kernel-proto/`.**
- **`/tmp/kproto/` buf-breaking matrix** — `buf breaking --against '.git#ref=HEAD'` at WIRE_JSON vs PACKAGE over 6 real changes to *this* API. Reproduced investigate/14's finding independently: **deleting `rpc Respond` PASSES at WIRE_JSON and FAILS at PACKAGE**. Note: an inline `--config` silently replaces `modules:` and breaks the build — use config *files*, or the run reports import errors that look like results.
- **`/tmp/fwd/`** — forward-compat measurement. Two generated packages (old Envelope without field 15, new with `trace_id = 15`). Binary path: old plugin **retained 15 unknown-field bytes** and `trace_id` survived the round-trip. JSON path (`protojson`): **field silently lost**. (First attempt panicked with `proto: file ... is already registered` — two packages cannot share a `.proto` path in one binary; distinct paths fix it without affecting the wire result.)
- **`/tmp/vocab.sh`** → delivered as **`design/20-kernel-boundary-test.sh`** — the §10.2 boundary test. v1 (raw grep) false-positived on protobuf's own `message`/`service` keywords; v2 checks declared identifiers only but **introduced a false negative via `\b`**; v3 matches substrings case-insensitively with an allowlist. Rejected my own draft (`LogAppend`, `token`); passes the final API; **catches an injected `AgentInfo`/`agent_name`/`session_id`/`repo_path` that `buf build` and `buf lint` both accept**; and now catches the `LogAppend` case v2 missed (regression test kept).
- **The self-correction that matters most** (§10.2): `sed -i '' 's/\bLogAppend/JournalAppend/g'` **silently did nothing** — BSD `sed` has no `\b` — and the v2 test **passed the un-renamed file** because `\b` cannot match inside CamelCase. I had already written "renamed" into this document. Caught by grepping the delivered `.proto` rather than trusting the tool's exit code; both the rename (redone in Python) and the test are now verified by output, not by assumption.

### Claims inherited from siblings and NOT re-verified by me
Flagged so they are not mistaken for my own measurements:
- Every `startRaftNode`/`bootstrapRaftNode` call site is inside `jetstream_cluster.go` (investigate/11 read the source).
- Core NATS full-mesh survival of 2-of-3 deaths; 30ms lone boot; 30ms rejoin; 3.67ms tailnet RTT; `no responders` in 0s; the R1-placement result (investigate/11, `/tmp/natslab/`).
- The leafnode mesh being a protocol violation (investigate/15).
- memberlist's 6.6s death detection / 0.7s rejoin / 190ms meta propagation / graceful-Leave 0.6s / `NotifyLeave` reporting `State=0` (investigate/13, `/tmp/mlprobe/`).
- go-plugin's 12ms reload, crash isolation, `SecureConfig` SHA256, and the stdlib `plugin`/Yaegi/WASM disqualifications (investigate/12).
- mangos' 98.9% silent PUB/SUB loss and the ZeroMQ Guide quote on invisible subscribers (investigate/10).
- The ≥1300B silent-drop path-MTU probe and the ConnectRPC/curl/gRPC interop results (investigate/14, investigate/11).
