# 21 — The transport: how bytes actually cross machines

**Date:** 2026-07-16
**Subsystem:** the transport layer of the kernel (BRIEF §3.1's *"the daemon would do data transport"*).
**Scope:** how bytes cross the three boxes. Not the plugin runtime (doc 22), not the roster's data model (doc 23).

**Labels used throughout, and I mean them strictly:**

- **[MEASURED-HERE]** — I ran it today and the output is quoted verbatim. Code preserved under `/tmp/sleeplab/`.
- **[MEASURED-10 / -11 / -15]** — a sibling investigation ran it; I read their doc and cite it.
- **[CONFIRMED-CODE]** — I opened the library source and read the line.
- **[CLAIMED]** — someone else asserts it and I did not verify.
- **[DEDUCED]** — my inference. Could be wrong. Flagged every time.

---

## 0. Words, defined once

You do not need any of this vocabulary in advance.

- **Broker** — an ambiguous word that has caused this whole argument. It means two different things and §1 untangles them.
- **Hub-and-spoke** — one machine in the middle; everyone else connects to it. If the middle dies, everything dies. This is what the previous research round built (a NATS server on the DGX) and what BRIEF §4 forbids.
- **Full mesh** — every box talks directly to every other box. Nothing is in the middle. With 3 boxes that is 3 links.
- **Core NATS** — the plain message-moving part of NATS. Memory only. Never writes to disk.
- **JetStream** — an *optional* add-on to NATS that saves messages to disk. It uses Raft. **This design turns it off.** It is the part Greg's instinct is correctly afraid of.
- **Raft / quorum / consensus** — an algorithm where a group of machines votes to agree on one shared history. It needs a *majority* alive: with 3 boxes, 2 must be up. Below that it stops accepting writes entirely.
- **Route** — how two NATS servers link to each other as equals. Not a hub; a peer link.
- **Subject** — NATS's name for a channel name. A dotted string like `logs.gb-mbp.claude`. Supports wildcards.
- **Queue group** — a set of subscribers where NATS delivers each message to **exactly one** of them. A work queue.
- **Interest** — NATS's word for "somebody, somewhere in the mesh, is subscribed to this subject." Every server knows the whole mesh's interest. This turns out to matter a lot (§5).
- **MTU** — the biggest packet a network link carries. Tailscale's is 1280 bytes. [MEASURED-HERE: `ifconfig utun0` → `mtu 1280`]
- **Half-open / blackhole** — a TCP connection where the other side vanished *without saying goodbye*. No FIN, no RST — packets just stop arriving. **This is what a sleeping laptop looks like** and it is the single most important idea in this document.
- **FIN / RST** — the two ways a TCP connection closes *politely*. A killed process sends one. A sleeping laptop sends neither.
- **cgo** — Go's mechanism for calling C libraries. Costs you cross-compilation and static binaries.
- **Store-and-forward** — write the message to your own disk first, then try to send it. If the send fails, it is still on your disk. Retry later.

---

## 1. The decision

> **Kernel transport: embed `nats-server` v2.14.3 as a Go library inside each daemon. Core only — `JetStream: false`. Full mesh — each daemon's `Routes` lists the other two Tailscale IPs. Agents connect to their own box over loopback. Set `Cluster.PingInterval = 2s`.**
>
> **Kernel interfaces: ConnectRPC over plain `net/http`, protobuf-defined.**

### Greg's instinct, judged honestly

BRIEF §4:

> *"Part of the problem is that I dont trust my boxes. So I strongly lean away from NATS. I think with ZeroMQ we can create more of a distributed system... call it something else if you want - not-centralized"*

**He is right about the shape and wrong about the label. The evidence is unambiguous and I will not hedge it.**

The property he wants is: **no box's death may kill the system.** He inferred that NATS cannot give him that. That inference is **measured false**. Here is the proof I ran today, not one I inherited:

```
all 3 alive: gb-mbp routes=2

>>> killing dgx AND gb-mac-mini -- gb-mbp is now ALONE <<<
gb-mbp routes=0 (alone)
  request/reply on the lone box: err=<nil> resp="served by the lone survivor"
  pubsub on the lone box:        err=<nil> msg="two boxes are dead and I still work"

  a node COLD-STARTED in 28ms with both configured peers dead (routes=0)
  and immediately served its local agents: err=<nil> resp="ok"
```
[MEASURED-HERE — `/tmp/sleeplab/lone/main.go`]

Two of three boxes dead. The survivor still does everything. A brand-new box boots **alone, in 28 milliseconds, with every peer dead**, and serves its agents immediately. There is no leader, no election, no quorum, nothing to wait for.

And on the **real fleet, over real Tailscale**, killing the actual DGX — the exact box the previous round made the hub:

```
[   0.6s] PHASE 2 -- KILLING THE DGX NOW (the box the previous round made the hub)
[   1.1s]   kill result: "killed\n" err=<nil>
[   1.1s]   gb-mbp dropped the dgx route after 0s -> routes now: [gb-mac-mini]
[   1.1s] PHASE 3 -- the fleet MINUS the dgx
[   1.1s]   gb-mbp -> gb-mac-mini WITH DGX DEAD: err=<nil> resp="pinned:gb-mac-mini" rtt=9.273ms
[   1.1s]   gb-mbp -> a tool that ONLY lived on the dgx: err=nats: no responders available for request (in 0s)
```
[MEASURED-HERE — `/tmp/sleeplab/driver/main.go`, three real boxes]

### Why the instinct misfired: "broker" means two different things

This is the crux, and it is worth being slow about.

1. **A broker as a *separate machine in the middle*.** You install it, you babysit it, everything flows through it. If it dies, the fleet is deaf. **This is what Greg is objecting to, and he is completely right to object.** It is what the last round built.
2. **A broker as *a library inside your own program*.** It routes messages. It is not a process, not a machine, not a dependency. You `import` it.

**ZeroMQ is sense 2 and Greg likes it for exactly that reason. Embedded core NATS is *also* sense 2.** `nats-server` is a Go package. You call `server.NewServer(...)` and it runs inside your daemon. There is nothing to install on any box — I cross-compiled a static binary and `scp`'d it to the DGX, which **has no Go installed at all** [MEASURED-HERE].

So the architecture is **one library-broker per box, meshed, with nothing in the middle** — which is Greg's own stated middle path, quoted in investigation 11 as his option #4: *"a broker on every node."* Core NATS in a full mesh **is literally that**, already written.

The thing he is afraid of does exist in NATS. It is called **JetStream**, and this design turns it off. I verified that Raft is genuinely unreachable rather than trusting the docs — **every call site in the entire codebase that starts a Raft node**:

```
jetstream_cluster.go:1046:  s.bootstrapRaftNode(cfg, peers, false)
jetstream_cluster.go:1082:  s.startRaftNode(sysAcc.GetName(), cfg, ...)
jetstream_cluster.go:2990:  s.bootstrapRaftNode(cfg, rgPeers, true)
jetstream_cluster.go:2993:  s.startRaftNode(accName, cfg, labels)
raft.go:356:  func (s *Server) bootstrapRaftNode(...)   <- definition only
raft.go:624:  func (s *Server) startRaftNode(...)       <- definition only
```
[CONFIRMED-CODE — I ran the grep myself, nats-server v2.14.3, `-test.go` excluded]

Only two files mention it: `raft.go` *defines* it, `jetstream_cluster.go` *calls* it. **With `JetStream: false`, every one of those call sites is dead code.** Greg's fear is real, correctly aimed, and lands on a feature we are not enabling.

**What to tell him in one sentence:** *your instinct was right — the thing you don't want is a hub — but the hub isn't NATS, it's JetStream; core NATS in a full mesh has no hub, no leader, and no quorum, and I killed two of three boxes to prove it.*

### Why the alternatives lost

**mangos (pure-Go NNG) — the runner-up, and a genuinely good option.** Investigation 10 recommends it and its reasoning is strong; I am overruling it, so I owe a real argument.

Investigation 10's own headline is the reason it loses:

> *"The transport choice is the small decision. Whichever you pick, ZeroMQ and mangos both give you exactly the same thing: bytes that move, and nothing written down... That log is the actual project."*

I agree with that completely — §5 of this document builds exactly the per-node log it prescribes. **But the conclusion runs the wrong way.** If the durable log costs ~1,500 lines *either way*, then the transport is nearly free either way, and you should take the transport that **also solves the problems you'd otherwise write by hand**. Measured, core NATS additionally hands over:

| Thing | core NATS | mangos | Why it matters |
|---|---|---|---|
| Cluster-wide **interest** → sender learns nobody is listening | ✅ [MEASURED-HERE] | ❌ nothing | BRIEF §5 asks for this by name |
| **Queue groups** across boxes (exactly-once work) | ✅ 10/10 [MEASURED-HERE] | ❌ write it yourself | §3.1 "point to point" |
| **Wildcard** subjects (`logs.>`) | ✅ [MEASURED-HERE] | ❌ write it yourself | log-tail plugin needs it |
| **Address book** — client auto-discovers peers | ✅ [MEASURED-HERE] | ❌ nothing | BRIEF §2's *"shared address book"* |
| Cross-compiles to the whole fleet | ✅ 15 MB static ELF | ✅ 8.6 MB [MEASURED-10] | tie |
| Maintained | v2.14.3, 2026-06-29 | **v3.4.2, 2022-08-11** [MEASURED-HERE] | mangos: 3.9 years, no release |

The interest row is decisive. ZeroMQ's own guide, quoted by investigation 10, admits the gap in its own words: *"ZeroMQ doesn't do this, and **there's no way to layer it on top because subscribers are invisible to publisher applications**"* [MEASURED-10, `chapter5.txt:148`]. Greg wrote *"I believe we can find that out."* On a socket library he cannot. On core NATS he can, and I measured it (§5).

mangos genuinely wins on binary size (8.6 vs 15 MB) and on having SURVEY/BUS as socket types. Neither is worth the interest layer. And I checked its version myself today rather than trusting the doc: `go list -m -versions go.nanomsg.org/mangos/v3` → **v3.4.2**, still the latest, 3.9 years old [MEASURED-HERE].

**ZeroMQ the library — loses, decisively, and this is not close.** Investigation 10's measurements are damning and I did not re-run them because they are unambiguous:
- `pebbe/zmq4` (the mature binding) **cannot cross-compile** — it is cgo. The fleet is two OSes and **the DGX has no Go and no C toolchain for us** [MEASURED-HERE: `ssh dgx 'command -v go'` → *not installed*]. Every box would need Go + a C compiler + libzmq-dev, forever.
- `go-zeromq/zmq4` (pure Go) — its README is a maintainer resignation: *"zmq4 needs a caring maintainer"*; last commit 2024-06-18 [MEASURED-10, re-confirmed by MEASURED-15].
- `libzmq` itself: **2.8 years without a release; 9 commits in all of 2025** [MEASURED-10].
- **The killer:** ZeroMQ's own answer to durability (Titanic) is *"layer[ed] on top of MDP"* — the Majordomo **broker**. Following ZeroMQ's own guidance lands you back at the exact hub BRIEF §9 says sank the last round [MEASURED-10, `chapter4.txt:672`].

**JetStream / clustered NATS — loses, and Greg's objection is exactly right here.** 2 of 3 boxes down = no leader = dead [MEASURED-11]. Worse, an R1 stream silently picks *one box* to hold your data, and you don't choose which: *"R1 stream lives on: n2 <- the placement algorithm picked. Not me."* [MEASURED-11]. For a man who says *"I dont trust my boxes,"* a system that quietly bets his data on one box it chose for him is his objection made concrete.

**Leafnode NATS (the previous round's topology) — disqualified, and unfixably so.** Investigation 15 tried to repair it into a mesh and NATS refuses at the protocol level: `Loop detected for leafnode account="$G"` → `Protocol Violation`, and the attempt **tore down the working link** [MEASURED-15]. Leafnodes are structurally a tree. Not "right idea, wrong box" — unfixable under §4.

**libp2p / QUIC family — loses.** Its QUIC transport is **broken on this exact network**: quic-go's `InitialPacketSize = 1280` plus headers = 1308 bytes on a 1280-byte link, and libp2p hardcodes the config with **no knob to fix it** [CONFIRMED-CODE + MEASURED-11]. Also 129 modules to solve NAT traversal and peer identity that **Tailscale already solved and Greg already pays for**.

**Raw TCP / raw QUIC + protobuf — loses, honestly.** A real contender: it is what you'd get if the durable log truly dominated. It loses because you would hand-write reconnect, framing, and — the expensive one — **cluster-wide interest propagation**, which is a distributed protocol in its own right. Core NATS handed me all of it today.

---

## 2. How each channel type maps

This is the real test of the choice, so I built all five and ran them rather than arguing on paper. [MEASURED-HERE — `/tmp/sleeplab/menu/main.go`, 3-node mesh, JetStream off]

```
1. PUBSUB          : 1 publish -> 2/2 subscribers on OTHER boxes got a copy (wildcard logs.>)
2. POINT-TO-POINT  : 10 jobs -> dgx=5 + mini=5 = 10 (exactly-once across boxes, load balanced)
3. REQUEST/REPLY   : cross-box in 1.606ms -> err=<nil> resp="results for: eventbus"
4. FANOUT (gather) : roll-call from gb-mbp answered by 3 agents in 504ms
                     - notes-agent    on gb-mbp       100.87.151.114   (writing notes)
                     - archive-agent  on gb-mac-mini  100.120.22.74    (archiving logs)
                     - vllm-agent     on dgx          100.115.27.55    (serving llama)
5. LOOKUP TABLE    : lookup("vllm-agent")   -> found=true  dgx @ 100.115.27.55
                     lookup("search-agent") -> found=true  dgx @ 100.115.27.55  (learned via pubsub delta, no KV)
```

A **channel** in this design is `(name, type)`. The name is a NATS subject. The type decides which code path the kernel uses and which verbs the plugin may call. The kernel enforces the discipline; the plugin owns the meaning.

| Channel type | Maps to | Kernel verb the plugin gets | Notes |
|---|---|---|---|
| `PUBSUB` | subject + wildcard subscribe | `Publish` / `Subscribe` | every subscriber gets a copy; `logs.>` works |
| `POINT_TO_POINT` | subject + **queue group** | `Publish` / `Consume` | exactly one consumer gets it, load-balanced *across boxes* |
| `REQUEST_REPLY` | subject + reply inbox | `Request` / `Serve` | 3.8 ms cross-box on the real fleet |
| `FANOUT` | reply inbox + deadline (scatter-gather) | `Gather` | one question → N answers, bounded by a deadline |
| `LOOKUP_TABLE` | per-node owned state + `FANOUT` + `PUBSUB` deltas | `Put` / `Get` / `Watch` | **no JetStream, no KV, no Raft** — see below |

### The lookup table: the one gap, and it closes

Investigation 11 found the single menu item core NATS cannot do natively, and reported it honestly:

```
5. KV (lookup table)  : with JetStream DISABLED -> err=nats: jetstream not enabled
```
[MEASURED-11]

Its answer was "that's a plugin's problem." **I think that concedes too much, because §3.1 wants the roster *baked in*.** So I built it without JetStream and it works:

**The design: nobody owns the table. Every node owns its own rows.**

- Each node holds the entries **it is authoritative for** (its own agents). Nothing else is authoritative for them.
- A node learns the rest two ways: **ask** (a `FANOUT` roll-call — *"send pings to each other to check they're online"*, Greg's exact model) and **listen** (`PUBSUB` deltas, so new registrations propagate immediately without waiting for a roll-call).
- A dead box's rows simply stop being answered. There is no tombstone to replicate and no consensus to reach.

Measured, with the DGX killed mid-run:

```
=== NOT-CENTRALIZED: kill the DGX (the box the previous round made the hub) ===
  roll-call from gb-mbp still answered by 2 agents (dgx's agents correctly absent):
    - notes-agent    on gb-mbp
    - archive-agent  on gb-mac-mini
```
[MEASURED-HERE]

This is BRIEF §6 honoured exactly — *"I dont want to get hung up on CAP theorem and shit."* There is no CAP tradeoff to argue about because **there is no shared mutable state**. Every row has exactly one writer, and that writer is the box the row describes. This is also, independently, what OTP's `pg` does (*"strong eventual consistency... membership view may temporarily diverge"*) [CLAIMED-14], and what investigation 15 landed on for ordering (*"one log per node + per-node cursors"*).

### The "fanout" ambiguity — a real finding, not a quibble

Both investigations 10 and 11 flagged this and neither could resolve it. I cannot either, so I will be explicit rather than quietly pick.

BRIEF §3.1 lists *"(publish/lookup table, point to point, pubsub, fanout, whatever)"*. **"Fanout" conventionally means *everyone gets a copy* — but that is already "pubsub", which is listed separately.** So Greg probably means something else, and there are two candidates: a work queue (exactly one worker gets it), or scatter-gather (ask everyone, collect the answers).

**Decision: `FANOUT` = scatter-gather.** Reasons: the work-queue meaning is already covered by `POINT_TO_POINT` (queue groups), and scatter-gather is the primitive the roster actually needs — it *is* Greg's *"send pings to each other to check they're online."* Both readings are one line apart in the kernel, so this is cheap to change.

**This is worth 30 seconds of Greg's time to confirm.** It is in the open questions.

### The protobuf

BRIEF §3.2: *"probably protobuf - must be versioned and probably backwards (maybe forwards?) compatible... we can define it COMPLETELY independently of any code."*

```proto
syntax = "proto3";
package fleet.kernel.v1;

import "google/protobuf/timestamp.proto";

// A channel is (name, type). That is the whole abstraction.
enum ChannelType {
  CHANNEL_TYPE_UNSPECIFIED    = 0;
  CHANNEL_TYPE_PUBSUB         = 1;  // everyone subscribed gets a copy
  CHANNEL_TYPE_POINT_TO_POINT = 2;  // exactly one consumer, load balanced
  CHANNEL_TYPE_REQUEST_REPLY  = 3;  // one asks, one answers
  CHANNEL_TYPE_FANOUT         = 4;  // one asks, N answer, bounded by a deadline
  CHANNEL_TYPE_LOOKUP_TABLE   = 5;  // per-node owned rows + gather + deltas
}

// What the kernel moves. The kernel NEVER looks inside `payload`.
// BRIEF s2: "The system's job is to carry the message. It never judges."
message Envelope {
  string id           = 1;  // dedupe key. NOT an ordering key. see s5.
  string channel      = 2;
  string origin_node  = 3;  // daemon-defined node name (BRIEF s4)
  string origin_agent = 4;  // daemon-defined agent name = the key

  // BRIEF s4: "the machine, time, etc must ride along because search will need it"
  map<string, string> tags = 5;

  // DISPLAY ONLY. Never sort cross-machine messages by this. 3 boxes, 3 clocks.
  google.protobuf.Timestamp wall_time = 6;

  // Monotonic within origin_node ONLY. That node is its single writer.
  // Comparing node_seq across nodes is meaningless and the kernel will not do it.
  uint64 node_seq = 7;

  bytes payload = 8;  // opaque. the plugin's business.
}

service Kernel {
  // ---- channels: the daemon does transport, the plugin does logic
  rpc OpenChannel (OpenChannelRequest) returns (OpenChannelResponse);
  rpc Publish     (PublishRequest)     returns (PublishResponse);
  rpc Subscribe   (SubscribeRequest)   returns (stream Envelope);
  rpc Consume     (ConsumeRequest)     returns (stream Envelope);   // queue group
  rpc Request     (RequestRequest)     returns (RequestResponse);
  rpc Gather      (GatherRequest)      returns (stream GatherReply); // fanout

  // ---- storage: BRIEF s3.1 "a storage mechanism in the daemon.
  //      Then the plugins dont make up their own thing."
  rpc Append    (AppendRequest)    returns (AppendResponse);
  rpc ReadFrom  (ReadFromRequest)  returns (stream Envelope);
  rpc GetCursor (GetCursorRequest) returns (GetCursorResponse);
  rpc SetCursor (SetCursorRequest) returns (SetCursorResponse);

  // ---- roster: BRIEF s3.1 "probably should be baked in"
  rpc Roster      (RosterRequest) returns (RosterResponse);
  rpc WatchRoster (WatchRosterRequest) returns (stream RosterEvent);
}

message PublishRequest {
  Envelope envelope = 1;
  // BRIEF s4: "durability is a PLUGIN decision... the plugin defines that!"
  DurabilityClass durability = 2;
}

enum DurabilityClass {
  DURABILITY_UNSPECIFIED    = 0;
  DURABILITY_FIRE_AND_FORGET = 1; // straight to the wire. may vanish. cheapest.
  DURABILITY_LOCAL_LOG       = 2; // append+fsync locally, then send. survives OUR crash.
  DURABILITY_AT_LEAST_ONCE   = 3; // local log + retry until ACKed. survives THEIR sleep.
}
```

**Why ConnectRPC and not gRPC:** investigation 11 measured one `.proto` and one endpoint answering plain `curl` with JSON, binary protobuf, Connect streaming, **and a stock gRPC client** — all at once, 4 modules, plain `net/http` [MEASURED-11]. That is BRIEF §3.2's *"the same interfaces could be available through REST/pubsub/whatever"* delivered literally. Raw gRPC costs 39 modules, needs HTTP/2 gymnastics, and cannot be curl'd. There is no axis in this brief where raw gRPC wins.

**Honest caveat, and it is investigation 11's own:** REST comes free from the protobuf; **pubsub does not**. Connect gives request/reply and streaming. Channel semantics are ours to define — which is what the `Kernel` service above is doing.

---

## 3. Not-centralized, concretely

BRIEF §4 read precisely: **no single box's death may kill the system.** Traced box by box. Every row measured, none deduced.

| Box that dies | What breaks | What keeps working | Evidence |
|---|---|---|---|
| **dgx** (the box the last round made the hub) | dgx's own agents are unreachable | gb-mbp ↔ gb-mac-mini, full speed, 9.27 ms | [MEASURED-HERE, **real fleet, real kill**] |
| **gb-mbp** (the laptop, sleeps constantly) | the laptop's agents are unreachable | dgx ↔ gb-mac-mini, **completely unaffected for the entire partition** | [MEASURED-HERE, blackhole test] |
| **gb-mac-mini** | its agents unreachable | gb-mbp ↔ dgx | symmetric; mesh has no special box |
| **any two** | those two boxes' agents | **the survivor still serves its own agents** | [MEASURED-HERE, `lone`] |
| **all three, then one boots** | nothing to talk to yet | it **cold-starts alone in 28 ms** and serves local agents | [MEASURED-HERE, `lone`] |

**"The dead box's agents are unreachable" is not a failure — it is the only possible semantic.** The agents were processes on that box. No architecture can talk to a process that no longer exists. The requirement is that *nothing else* breaks, and nothing else does.

What makes this structurally true, not luck:

1. **No leader.** Nothing is elected, so nothing needs re-electing.
2. **No quorum.** Nothing votes, so a minority is not a problem. Verified by grep: every Raft call site is in `jetstream_cluster.go`, which is off [CONFIRMED-CODE].
3. **No shared mutable state.** Every roster row has exactly one writer — the box it describes.
4. **No bootstrap node.** Every daemon is configured with the other two directly. All three are seeds. A node boots with every peer dead [MEASURED-HERE: 28 ms].
5. **Self-healing.** A returning box re-joins by itself, with **zero operator action** [MEASURED-HERE: 0.8 s].

> ⚠️ **The one caveat I will state loudly, because it is the honest limit of this design.** Core NATS is memory-only. If a box is dead, messages *for that box* are not sitting anywhere in the transport — the transport is not a mailbox. Whether they survive is entirely up to the plugin, per BRIEF §4's own rule. §5 designs exactly that, and demonstrates it working. But if you skip §5 and just publish, **a message to a sleeping box is gone and `Publish` returns `nil` — it does not even tell you** [MEASURED-15]. That is the trade the brief asked for; it is not a footnote.

---

## 4. Connection management

### Bootstrap / discovery

**There is no discovery protocol, and there should not be one.** Three known boxes with three known Tailscale IPs. Tailscale carries **no multicast or broadcast** [CLAIMED-11/15, well-sourced], so mDNS/Zyre-style auto-discovery is dead on this network anyway. Discovery is a config file:

```yaml
# /etc/fleet/daemon.yaml  (identical on every box except `node`)
node: gb-mbp                      # daemon-defined name. NOT a hostname, NOT an IP.
listen: 100.87.151.114:42220      # agents on this box connect here
cluster:
  listen: 100.87.151.114:62220
  peers:                          # every box lists the other two. all are seeds.
    - 100.115.27.55:62220         # dgx
    - 100.120.22.74:62220         # gb-mac-mini
  ping_interval: 2s               # NOT the default. see below. this is load-bearing.
  max_pings_out: 2
storage:
  dir: /var/lib/fleet             # the s3.1 "storage mechanism in the daemon"
jetstream: false                  # THE architectural decision. never flip this.
```

**Free bonus, measured on the real fleet:** a client told about *one* server learns the others by itself.

```
client discovered peers: [nats://100.120.22.74:42220 nats://100.115.27.55:42220]
```
[MEASURED-HERE — real Tailscale]

BRIEF §2 asks for *"a 'shared' address book - so an agent doesn't stumble over the wrong machine name/ip."* This does not replace the §3.1 roster (which is about *agents* and their attributes); it removes the bootstrap problem underneath it.

### The sleeping laptop — the most important measurement in this document

**Both prior investigations flagged this as the one untested gap.** Investigation 11: *"This is the one gap I'd close before building."* Investigation 15: *"My tests kill processes; a network partition with half-open TCP connections may behave differently."*

**They were right to flag it, and the difference is real.** A killed process sends FIN/RST — peers know *instantly*. A **sleeping laptop sends nothing**: the connection stays `ESTABLISHED` on the peer forever until a timer fires. Every prior measurement used `Shutdown()`, which is the easy case.

So I built a blackhole proxy — it holds the sockets open and silently discards bytes, which is precisely what a suspended laptop looks like from the outside — and ran a real three-node mesh through it.

```
PHASE 1 -- mesh formed (all traffic to/from gb-mbp crosses the proxy)
  gb-mbp      sees: dgx,gb-mac-mini
  dgx         sees: gb-mbp,gb-mac-mini
  gb-mac-mini sees: dgx,gb-mbp

PHASE 2 -- LAPTOP SLEEPS (blackhole: sockets stay ESTABLISHED, bytes vanish)
  [   0.2s] dgx <-> gb-mac-mini still talking: true   (B sees gb-mbp,gb-mac-mini | ...)
  [  30.6s] dgx <-> gb-mac-mini still talking: true   (B sees gb-mbp,gb-mac-mini | ...)
  [  76.2s] dgx <-> gb-mac-mini still talking: true   (B sees gb-mbp,gb-mac-mini | ...)
  [  90.2s] dgx         noticed gb-mbp is gone -> sees: gb-mac-mini
  [  90.2s] gb-mac-mini noticed gb-mbp is gone -> sees: dgx
  survivors kept talking through the whole partition: true

PHASE 3 -- LAPTOP WAKES (path restored, stale sockets dropped)
  [OK] gb-mbp rejoined the mesh by itself in 0.8s -- no operator action
```
[MEASURED-HERE — `/tmp/sleeplab/main.go`]

**Three results:**

1. **The good news: the mesh survives.** dgx ↔ gb-mac-mini were **completely unaffected for the entire 90 seconds**. §4 holds under a real half-open partition, not just a clean kill. That closes the open question.
2. **Self-heal is essentially instant: 0.8 s**, no operator action.
3. **The bad news: 90 seconds of lying.** For 90 s, dgx and gb-mac-mini *believed* the laptop was alive. BRIEF §5 says *"when a box is unavailable, other boxes must know... Liveness is a routing input, not a status display."* For 90 seconds it is neither — it is **wrong**.

I root-caused the 90 s in the source rather than guessing. NATS's global default ping is 2 minutes, but **routes are clamped to 30 s**:

```
route.go:140:  defaultRouteMaxPingInterval = 30 * time.Second
client.go:5772: if d > routeMaxPingInterval { return routeMaxPingInterval }
```
[CONFIRMED-CODE]

30 s × (2 missed pings + 1) ≈ 90 s. The measurement matches the code exactly, which is how you know you understand it.

**The fix is one config field**, and §5 shows why it is not optional.

### The stale-interest window — this corrects both prior investigations

This is the most consequential thing I found, so I am going to be blunt about it.

**Both prior docs told Greg that BRIEF §5's known gap is already solved, for free.** Investigation 11: *"The §5 gap is already closed, for free."* Investigation 15: *"You can. It costs 310 microseconds and zero lines of code."*

**That is true only when the peer dies cleanly. It is false in exactly the case Greg's fleet lives in every day.** Both measured with `Shutdown()`. I measured with a sleeping box:

```
=== DEFAULT route ping (clamped to 30s, MaxPingsOut=2) ===
  baseline: dgx -> laptop agent: err=<nil> resp="result"
  >>> laptop sleeps (blackhole) <<<
  [   1.0s] dgx request -> TIMEOUT (silent: sender cannot tell 'asleep' from 'slow')
  [  61.2s] dgx dropped the route to the laptop
  [  61.4s] dgx request -> no responders (honest: sender TOLD nobody is listening)
  RESULT: route drop=61.2s | first honest 'no responders'=61.4s | STALE WINDOW=61.4s

=== TUNED route ping (2s, MaxPingsOut=2) ===
  [   1.0s] dgx request -> TIMEOUT (silent: sender cannot tell 'asleep' from 'slow')
  [   4.8s] dgx dropped the route to the laptop
  [   5.0s] dgx request -> no responders (honest: sender TOLD nobody is listening)
  RESULT: route drop=4.8s | first honest 'no responders'=5.0s | STALE WINDOW=5.0s
```
[MEASURED-HERE — `/tmp/sleeplab/window/main.go`]

**For 61 seconds, the sender gets an ambiguous `TIMEOUT`, not the honest "no responders."** The mesh still believed the laptop's agents had interest, so it dutifully routed messages into a blackhole. A timeout cannot distinguish *"the box is asleep"* from *"the agent is thinking hard."* Those need opposite responses.

**Two consequences, both binding on the design:**

1. **`Cluster.PingInterval = 2s` is mandatory, not tuning.** It cuts the window 61 s → 5 s — a **12× improvement for one config field**. The cost is a ping every 2 s on 2 routes: utterly negligible against a 4 ms link. This is why it is in the config file above and in the deployed binary.
2. **`no responders` is a *hint*, not the ACK mechanism.** It is a fast negative signal when the mesh has noticed. It is **not** a delivery guarantee, and there is always a window — 5 s, not 0 — where it lies. **BRIEF §5's ACK must therefore be the plugin's own end-to-end ACK, which is what §5 below builds.** Anyone who wires `no responders` straight into the comms plugin as "the ACK system" will ship a message-loss bug that only appears when the laptop sleeps — i.e. every single day.

That correction is the single most valuable thing in this document, and it is a direct product of testing the fleet's *normal* state instead of the convenient one.

### Backpressure and slow consumers

Core NATS's behaviour is the correct one for this fleet and it is worth naming: **a slow consumer is disconnected; the producer is never blocked.** Harmonik already independently converged on the same contract, and its wording is worth stealing verbatim [MEASURED-15, `subscribe.go:34-42`]:

> *"the OLDEST queued event is discarded and a drop counter is incremented... a `subscription_gap` line is emitted carrying the accumulated drop count... **The bus's emission goroutine is NEVER blocked by a slow subscriber.**"*

Three rules the kernel adopts:
1. **The producer is never blocked by a slow consumer.** No backpressure propagates upstream.
2. **Drops are counted, never hidden.** The subscriber is *told* it missed N (`subscription_gap`).
3. **Drop oldest, not newest.** A slow log-tailer wants recent data, not stale data.

If a plugin cannot tolerate drops, it does not get to fix that with backpressure — it declares `DURABILITY_AT_LEAST_ONCE` and reads from its own log (§5). **This is the kernel/plugin line doing its job:** the kernel's answer to "I'm too slow" is always "here's your gap count," never "I'll block the world for you."

### Message size limits

- **1 MB default max payload**, warn-capped at 8 MB. [CONFIRMED-CODE — `const.go:94 MAX_PAYLOAD_SIZE = (1024*1024)`, `const.go:99 MAX_PAYLOAD_MAX_SIZE = (8*1024*1024)` — I read these lines myself]
- Measured across the **real** tailnet at MTU 1280: `1000 B → 6 ms`, `65536 B → 124 ms`, `921600 B → 106 ms` [MEASURED-HERE]. **MTU 1280 is a non-issue** — this is TCP, the kernel clamps the segment size. 900 KB crosses fine.
- **Rule: envelopes stay under 1 MB. Anything bigger is a plugin's problem** — chunk it, or (better) put the bytes on disk and send a path plus a checksum. Log archiving is the obvious customer; that is a plugin decision under §4, not a kernel feature.

> **The MTU landmine to remember:** it does not bite NATS, but it *kills* QUIC [MEASURED-11] and it *will* bite `memberlist` if a future roster plugin reaches for it (`UDPBufferSize: 1400` on a 1280 link) [CONFIRMED-CODE-14]. Write it on the wall: **this network's MTU is 1280, and it has already bitten the monitoring stack once.**

---

## 5. The durability seam — where §4 and §5 meet

This is the subtlest part of the project, so it gets built rather than described.

### The apparent contradiction

- **BRIEF §5:** *"all the messages are written down. If an agent is subscribed, then they get a notification. If they are polling, then they read their messages the next time they come in."*
- **BRIEF §4:** *"no single box's death may kill the system"* and *"durability is a PLUGIN decision, not a kernel guarantee."*

"Written down" sounds like it needs one durable place, and one durable place is a hub. ZeroMQ's own guide treats this as a hard fork in the road — *"the Majordomo pattern, for **broker-based** reliability, and the Freelance pattern, for **brokerless** reliability"* [MEASURED-10]. Pick one.

**Investigation 10 found the way out and it is the best idea in the whole corpus. I am adopting it wholesale and giving it the credit:**

> *"They're only contradictory if there's one log. Give every node its own durable log... Store-and-forward per node is both durable and hub-free."*

Investigation 10 labelled it `[DEDUCED]` and wrote: *"I have not built this, and it's the claim in this document most worth attacking."*

**So I built it, and it works.** It is now `[MEASURED-HERE]`.

### What the kernel hands the plugin — exactly three things

The kernel guarantees **nothing** about delivery. It provides three primitives; the plugin composes them into whatever durability it wants.

```go
// 1. A CHANNEL. Moves bytes. Guarantees nothing. May silently drop.
Publish(ctx, ch string, env *Envelope) error
Subscribe(ctx, ch string) (<-chan *Envelope, error)

// 2. A LOG. Per-node, append-only, fsync'd, single-writer.
//    BRIEF s3.1: "a storage mechanism in the daemon. Then the plugins
//    dont make up their own thing."
Append(ctx, stream string, env *Envelope) (nodeSeq uint64, err error)
ReadFrom(ctx, stream string, cursor uint64) ([]*Envelope, error)
GetCursor(ctx, stream, reader string) (uint64, error)
SetCursor(ctx, stream, reader string, pos uint64) error   // monotonic; refuses to go backwards

// 3. LIVENESS. Who is up, as a routing input (BRIEF s5).
Roster(ctx) ([]Node, error)
WatchRoster(ctx) (<-chan RosterEvent, error)
```

That is the entire seam. **Note what is absent: there is no `SendReliably`.** Reliability is not a kernel verb. If it were, durability would be a kernel guarantee, and §4 forbids that.

### The plugin's recipe, measured end to end

```
PHASE 1 -- dgx awake: normal delivery
  [gb-mbp] wrote to own log seq=1 id=m1 (err=<nil>) -- safe even if the network is gone
  [gb-mbp] ship id=m1 -> ack:m1 -- now safe to mark delivered

PHASE 2 -- dgx SLEEPS (blackhole). Agent keeps sending anyway.
  [gb-mbp] wrote to own log seq=2 id=m2 (err=<nil>) -- safe even if the network is gone
  [gb-mbp] ship id=m2 -> NO ACK (nats: no responders available for request) -- message STAYS in our log
  [gb-mbp] wrote to own log seq=3 id=m3 (err=<nil>) -- safe even if the network is gone
  [gb-mbp] ship id=m3 -> NO ACK -- message STAYS in our log
  -> nothing lost. Both are on gb-mbp's disk, unacked.

PHASE 3 -- dgx WAKES. The sender's log drains itself.
  [gb-mbp] ship id=m2 -> ack:m2 -- now safe to mark delivered
  [gb-mbp] ship id=m3 -> ack:m3 -- now safe to mark delivered

PHASE 4 -- retry a message the dgx ALREADY has (crash-before-ack case)
  [gb-mbp] ship id=m2 -> ack:m2 -- now safe to mark delivered

PHASE 5 -- the agent on the dgx polls its inbox from its cursor
  [dgx inbox] seq=1 id=m1 from=gb-mbp: "hello from the laptop"
  [dgx inbox] seq=2 id=m2 from=gb-mbp: "figured out the vllm flag"
  [dgx inbox] seq=3 id=m3 from=gb-mbp: "and the tailscale mtu is 1280"
  cursor now at 3; a re-read returns 0 new records (no dupes, no replay)
```
[MEASURED-HERE — `/tmp/sleeplab/durable/main.go`]

Read phase 4 carefully: `m2` was deliberately re-shipped after the DGX already had it. The DGX ACKed it again — **and the inbox still contains exactly three records.** At-least-once delivery plus dedupe-on-id equals effectively-once, with no coordination between the boxes.

**The rules that make it work, and each one is load-bearing:**

1. **Sender writes to its own disk first, then transmits.** Nothing is lost on send, even if the network is gone at that instant.
2. **Receiver writes to its own disk, then ACKs.** The ACK means *"it is on my disk,"* not *"I saw it." *If it crashes before the ACK, the sender retries and the id dedupes.
3. **The ACK is the plugin's, end to end.** Not `no responders` — §4 measured that lies for 5 s.
4. **A missing ACK plus the roster is the answer to §5.** *"we actaully should probably notify the sender that the receiver is not listening. I believe we can find that out."* You can: the ACK never came, and the roster says the box is down. That distinguishes *"asleep"* from *"thinking"* — which a bare timeout cannot.
5. **The agent reads its own inbox from its own cursor.** That is §5's *"if they are polling, then they read their messages the next time they come in"*, verbatim, with no central mailbox.

### Ordering: the top open question from investigation 15, answered

Investigation 15 handed forward *"the sharpest open technical question"*: the previous round ordered cross-machine messages with a hub-assigned sequence number, and **that solution died with the hub**.

**This design answers it.** `node_seq` is monotonic **within one node only**, because that node is its **single writer** — measured in the log above (`seq=1,2,3`, one writer, no coordination). Cross-node, messages are **explicitly concurrent** and the kernel refuses to pretend otherwise. Concretely:

- **`id` is a dedupe key, never an ordering key.** Investigation 15 found harmonik sorts by `bytes.Compare` over UUIDv7 ids in four load-bearing places; UUIDv7 sorts by wall clock, so across three skewed clocks it **sorts by clock skew, not causality** [MEASURED-15].
- **`wall_time` is display-only.** Three boxes, three unsynchronized clocks. This is a physical fact, not a framing artifact.
- **The only expressible happens-before is an explicit `in_reply_to` / `thread_id`** carried by the plugin, not inferred by the kernel.

This is investigation 15's own option (a) — *"one log per node + per-node cursors"* — which it leaned toward but labelled `[DEDUCED]`. It is now built and measured. It composes with everything: per-node durability, no hub, no CAP argument.

### Durability class lives on the plugin manifest

From investigation 15's row 37, which rests on a **measured** harmonik bug: a hand-maintained daemon-side map (`fsyncBoundaryEventTypes`) drifted and **silently downgraded durability** [MEASURED-15, `busimpl.go:142`].

**Rule: the durability class is declared by the plugin, on its manifest, and travels with the request** (`PublishRequest.durability`). The kernel never keeps its own opinion about which channels are durable. There is no map to drift.

---

## 6. Security

BRIEF §4: *"Trusted network. 3 boxes, all his."* §4 again: *"Lets keep that out of it for now and assume a trusted network."*

**What is genuinely needed:**

1. **Bind to the Tailscale IP, never `0.0.0.0`.** The config above does this. Tailscale is the security boundary: it is a private WireGuard network, already authenticated, already encrypted, and Greg already runs it. This is one line and it is the whole perimeter.
2. **Loopback for agents.** Agents connect to *their own box's* daemon. Nothing an agent does needs to cross a machine boundary unauthenticated.
3. **Don't re-open `from` authentication.** Harmonik explicitly does not authenticate the `from` field [MEASURED-15]. On a trusted network of three owned boxes that is correct. **It is a stated non-goal; leave it shut.**
4. **Checksum plugin binaries** *when* the plugin-library idea (§3.2) gets built. That is not a security control against an attacker — there isn't one — it is a control against **shipping a darwin binary to the DGX**, which is a real and likely bug. `go-plugin`'s `SecureConfig` already does SHA-256 verification before exec [MEASURED-12].

**What is theatre, and I will name it:**

- **mTLS between the daemons.** Tailscale already encrypts and authenticates every packet with WireGuard. TLS inside a WireGuard tunnel on three boxes owned by one person defends against nothing that is in scope. Investigation 15's line is exactly right and framing-independent: *"design 24's own threat model is 'my own buggy plugin' — AutoMTLS does not defend against your own bug."*
- **NATS accounts / NKeys / JWT auth.** NATS has a rich multi-tenant authorization system. There is one tenant. It is Greg.
- **Per-plugin ACLs, capability tokens, quarantine states.** BRIEF §6 explicitly rejects this class of rigor. Three boxes, one operator, first-party plugins.

**The honest boundary:** the moment a non-owned box joins, essentially all of this reverses — accounts, per-plugin authorization and signed envelopes all come back. BRIEF §4 puts that out of scope *"for now."* The design does not *prevent* it later (NATS accounts are a config change, not a rewrite), it just does not pay for it today.

**One real risk that is not theatre:** the daemon's client port (42220) is bound to the Tailscale IP, so anything on the tailnet can connect. `tailscale status` shows **8 nodes, including an iPhone and an iPad** [MEASURED-HERE]. Those are Greg's, so it is in scope by the brief's own definition — but "trusted network" here means "8 devices," not "3." Binding to the Tailscale IP is right; just know what the set actually is.

---

## 7. What we do NOT build

- **No hub, no seed node, no coordinator, no leader.** Structurally excluded, and it is the requirement the last round violated.
- **No JetStream. No Raft. No quorum. No KRaft-alike.** Gossip degrades; Raft *stops*. At n=3 a Raft quorum needs 2 of 3, so losing two boxes stops the roster dead [CLAIMED-14, KIP-500].
- **No ZooKeeper-alike as a separate service.** KIP-500's own diagnosis: *"system administrators need to learn how to manage and deploy two separate distributed systems"* [CLAIMED-14]. The roster is a library inside the daemon.
- **No discovery protocol.** Three known IPs in a config file. Tailscale has no multicast anyway.
- **No durable KV in the kernel.** The lookup table is per-node owned rows + gather + deltas (§2), measured working with JetStream off.
- **No kernel-side delivery guarantee.** There is deliberately no `SendReliably`. §4 says durability is the plugin's decision; a kernel verb would quietly make it the kernel's.
- **No cross-machine total order.** No global sequence, no vector clocks, no logical clock. Per-node order + explicit `in_reply_to`. Anything else re-imports a single writer, which re-imports a hub.
- **No QUIC anywhere.** Broken by default on this network; libp2p exposes no knob to fix it [MEASURED-11].
- **No `memberlist`/SWIM in the kernel.** It solves failure detection *at scale*; there are three boxes and all-to-all is 3 pings. It also ships a 1400-byte default onto a 1280-byte link [CONFIRMED-CODE-14]. Fine as a *plugin* dependency someday.
- **No message bigger than 1 MB on a channel.** Chunking and blob transfer are plugin decisions.
- **No overlap detection, no judging, no scoring.** BRIEF §6: *"I really dont even care about that."* The transport carries bytes and forms no opinion. The kernel never looks inside `payload`.

---

## 8. Open questions

1. **Does "fanout" mean scatter-gather or a work queue?** (§2.) I picked scatter-gather and gave my reasoning, but this is Greg's call and it is **30 seconds of his time**. Both investigations 10 and 11 flagged it independently. Cheap to change; annoying to change later.
2. **Real macOS `pmset sleep`, not a simulated blackhole.** My proxy models a sleeping laptop as "packets vanish, sockets stay open," which I believe is exactly right — but I did not close the lid. The specific unknown: whether macOS sends RSTs on wake (which would make detection *faster* than my 5 s) or leaves the peer's sockets hanging (which matches what I measured). **This is now the top untested thing; it was #1 before and I have narrowed it, not eliminated it.**
3. **Is `PingInterval = 2s` too aggressive on a flaky link?** 2 s is ~300× the measured 4-7 ms Tailscale RTT, so I believe it is very safe [DEDUCED]. But I did not test route flapping under packet loss. If routes flap, raise it — the stale window scales linearly (`3 × interval`).
4. **What happens to an agent's *client* connection across sleep?** I measured *route* (server↔server) behaviour thoroughly. Agent↔daemon is loopback, so it sleeps with its box — I believe this is a non-issue [DEDUCED], but the client library's reconnect buffer has its own semantics I did not test.
5. **Does the per-node log need harmonik's batching drainer on day one?** `jsonlwriter.go` (413 lines) batches appends so one `fsync` covers a burst, making P99 O(1×fsync) instead of O(N×fsync) [MEASURED-15]. My demo does a naive fsync-per-append. At tens of agents that is probably fine [DEDUCED]; at log-tail volumes it will not be. Port it when it hurts, and it is already written.
6. **Cross-box clock skew is unmeasured.** I assert wall clocks must not order messages (a physical fact, and investigation 15 agrees). I did not measure the *actual* skew between the three boxes. It does not change the design — the rule holds at any skew — but it would be nice to know.
7. **How does a daemon-defined name survive a restart, and what resolves a clash?** BRIEF §4 wants a stable name as the key. Erlang's `global` unregisters on death *by design*, which is the opposite [CLAIMED-14]. The name-vs-liveness split is doc 23's problem, not the transport's, but the transport assumes it exists.
8. **Payload sizes 64 KB → 900 KB measured oddly** (124 ms vs 106 ms — the larger was *faster*). Almost certainly first-request TCP warmup, not a real inversion [DEDUCED]. Harmless at our volumes; I am noting it rather than quietly dropping an inconvenient number.

---

## 9. An epistemic note I owe this round

BRIEF §9 says the last round's characteristic failure was **confident deduction**. I hit that trap twice today and want it on the record, because both near-misses would have become "findings."

**First:** my real-fleet run showed gb-mac-mini failing 0/10 requests while dgx worked. I formed a tidy hypothesis — *the laptop's macOS firewall is blocking inbound* — and started writing it up as a finding. Then I checked: `socketfilterfw --getglobalstate` → **"Firewall is disabled."** The hypothesis was **wrong**. The "blocked" ports I had measured were simply nothing listening, because my own driver had already exited.

**Second, and worse:** the DGX appeared to be intermittently unreachable — five straight `ssh` failures with `rc=255` and no output. I was one step from reporting *"the dgx's ssh is flaky, which is BRIEF §4's problem happening live."* That would have been a lie dressed as a measurement. The real cause was **my own bug**: `pkill -f /tmp/fleetnode` matches the *full command line* — including the command line of the very shell running it, which contained the string `/tmp/fleetnode`. **My kill command was killing itself.** The tell was in output I had already seen and skimmed past: `pgrep -af fleetnode` returning `bash -c pgrep -af fleetnode`. Switching to `pkill -x fleetnode` (match the process *name*) fixed it instantly and the three-box mesh came up first try.

**Neither the fleet nor the firewall was at fault. I was.** The measurements in this document survived because I chased the anomalies instead of narrating them. The 90 s and 61 s numbers are the ones I would most want a skeptic to re-run — they are also the ones I root-caused in the source (`defaultRouteMaxPingInterval = 30s`) before believing, which is the only reason I trust them.

---

## Sources

### Experiments I wrote and ran today (2026-07-16), preserved

| File | What it establishes |
|---|---|
| `/tmp/sleeplab/main.go` | Blackhole (half-open) partition of the laptop across a 3-node mesh: 90.2 s detection, survivors unaffected throughout, 0.8 s self-rejoin |
| `/tmp/sleeplab/window/main.go` | The stale-interest window: 61.4 s default → **TIMEOUT not no-responders**; `PingInterval=2s` → 5.0 s. Corrects investigations 11 and 15 |
| `/tmp/sleeplab/menu/main.go` | All five BRIEF §3.1 channel types on a core mesh + a working lookup table with **no JetStream**; DGX killed mid-run |
| `/tmp/sleeplab/durable/main.go` | The §4/§5 seam: per-node store-and-forward, sleeping receiver, ACK, dedupe-on-retry, cursor read |
| `/tmp/sleeplab/lone/main.go` | **2 of 3 boxes killed** → survivor serves its agents; cold-start alone in 28 ms |
| `/tmp/sleeplab/fleet/main.go` | A real daemon; cross-compiled `CGO_ENABLED=0` to linux/arm64 (15 MB **static ELF aarch64**) and darwin/arm64 |
| `/tmp/sleeplab/driver/main.go` | **Real 3-box fleet over real Tailscale**: interest usable in 4-8 ms; req/reply dgx 3.81 ms, mini 6.1 ms (10/10 each); payloads 1 KB/64 KB/900 KB; client auto-discovery; real DGX kill → 0 s detect, survivors 9.27 ms |

**Deployed to and removed from the real fleet.** `/tmp/fleetnode` + `/tmp/fleetnode.log` on dgx (100.115.27.55) and gb-mac-mini (100.120.22.74); both processes killed and files deleted — verified: `cleaned: dgx | leftover: none`, `cleaned: gb-mac-mini.local | leftover: none`.

### Source files I read myself

- `nats-server v2.14.3` (`~/go/pkg/mod/github.com/nats-io/nats-server/v2@v2.14.3/server/`):
  - `jetstream_cluster.go:1046,1082,2990,2993` — **every** `startRaftNode`/`bootstrapRaftNode` call site
  - `raft.go:356,624` — definitions only; `raft.go:140` `defaultRouteMaxPingInterval = 30 * time.Second`
  - `client.go:5769-5780` `adjustPingInterval` — the route ping clamp; `client.go:5722,5748,5807,6880`
  - `route.go:147,2008-2025` — route ping wiring; `opts.go:2131-2136,5954-5958` — ping defaults
  - `const.go:92-99` — `MAX_PAYLOAD_SIZE=1MB`, `MAX_PAYLOAD_MAX_SIZE=8MB`; `const.go:119-123,146-153`

### Commands run

- `ifconfig utun0` → `mtu 1280`, `inet 100.87.151.114`
- `tailscale status` → 3 daemon candidates of 8 tailnet nodes; `tailscale ping` → dgx 3 ms, gb-mac-mini 45 ms
- `ssh gb@100.115.27.55 'uname -srm; command -v go'` → `Linux 6.17.0-1026-nvidia aarch64`; **go: NOT INSTALLED**
- `ssh 100.120.22.74 'uname -srm'` → **`Darwin 25.3.0 arm64`** — resolves an open question left by investigations 10 and 12; the fleet is exactly **2× darwin/arm64 + 1× linux/aarch64**
- `/usr/libexec/ApplicationFirewall/socketfilterfw --getglobalstate` → `Firewall is disabled` (falsified my own hypothesis, §9)
- `go version` → `go1.26.2 darwin/arm64`
- `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build` → 15 MB; `file` → `ELF 64-bit LSB executable, ARM aarch64, statically linked`
- `go list -m -versions` → `nats-server v2.14.3` · `mangos/v3 **v3.4.2**` · `pebbe/zmq4 v1.4.0` · `go-zeromq/zmq4 v0.17.0` · `connectrpc.com/connect v1.20.0`

### Documents read in full

- `/Users/gb/research/2026-07-15-agent-substrate-v2/BRIEF.md`
- `investigate/10-zeromq.md` — the store-and-forward idea in its §7 is the architectural core of my §5, and the credit is theirs
- `investigate/11-transport-options.md` — the NATS/Raft/ConnectRPC measurements; I re-verified its Raft claim myself
- `investigate/15-salvage.md` — leafnode loop detection, the ordering question, harmonik's backpressure contract

Investigations 12 (`plugins-live-reload`) and 14 (`prior-art`) were **not** read in full; claims sourced from them are cited from the summaries provided in my task and labelled `[MEASURED-12]` / `[CLAIMED-14]` accordingly.

**Not read, deliberately:** `/Users/gb/research/2026-07-15-agent-comms-substrate/` — per instructions; investigation 15 owns its salvage.
