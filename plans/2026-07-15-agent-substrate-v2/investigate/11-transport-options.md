# Transport options, judged against "I don't trust my boxes"

**Date:** 2026-07-15
**Scope:** BRIEF §4's not-centralized constraint applied to the transport option space. ZeroMQ/NNG are owned by a sibling agent and are deliberately not assessed in depth here.
**Method:** Everything below labelled [MEASURED] was run today, most of it on Greg's actual fleet over the actual Tailscale link. [CONFIRMED-CODE] means I read the library source. [CLAIMED] means a vendor/doc says it and I did not verify. [DEDUCED] means I reasoned from the other two.

---

## 0. Words used here, defined once

- **Broker** — a process that receives messages and forwards them to whoever wants them. A middleman.
- **Hub-and-spoke** — one broker in the middle, everyone connects to it. If it dies, everything dies. This is the topology the *previous* research round chose (a NATS broker on the DGX) and it is what BRIEF §9 flags as violating §4.
- **Full mesh** — every box talks directly to every other box. There is no middle.
- **Raft / quorum / consensus** — an algorithm where a group of machines votes to agree on a single ordered history. Requires a majority ("quorum") to be alive: in a 3-box cluster, 2 must be up. Below quorum, the thing stops accepting writes.
- **Leader election** — Raft's way of picking one machine to be temporarily in charge.
- **Core NATS** — the plain message-passing part of NATS. Memory only, no disk, fire-and-forget.
- **JetStream** — the optional persistence layer bolted on top of NATS. This is the part that uses Raft.
- **MTU** — the largest packet a network link will carry. Tailscale's is 1280 bytes. [MEASURED: `ifconfig utun0` → `mtu 1280`]
- **QUIC** — a newer transport protocol that runs over UDP instead of TCP. Used by libp2p and Iroh.
- **cgo** — Go's mechanism for calling C libraries. It costs you cross-compilation and static binaries.
- **Gossip** — machines periodically tell each other what they know, so knowledge spreads without a central registry.

---

## 1. The headline

**Greg's instinct to avoid NATS is right about the topology and wrong about the product.**

His objection is centralization: *"I dont trust my boxes... I strongly lean away from NATS."* That objection is 100% valid against **JetStream** and 0% valid against **core NATS in a full mesh**. These are two different systems that ship in one binary, and the brief's own §4 ("durability is a PLUGIN decision, not a kernel guarantee") happens to remove the exact feature that causes the problem.

I measured this rather than assuming it:

| What died | Core NATS full mesh | JetStream R3 | JetStream R1 |
|---|---|---|---|
| Nothing | works | works | works |
| 1 of 3 boxes | **works** | works | **stream gone** (if it lived there) |
| 2 of 3 boxes | **still works on the survivor** | **dead — no leader** | dead |

[MEASURED — `/tmp/natslab/core`, `/tmp/natslab/js`, nats-server v2.14.3]

With two of three boxes killed, the lone survivor still did pub/sub *and* request/reply with zero errors. JetStream on that same survivor returned `no response from stream` and reported its leader as `""`. That is the entire disagreement, reduced to one table.

**Recommendation: embed `nats-server` as a Go library on each of the three boxes, core-only (JetStream disabled), routed to each other in a full mesh.** Use **ConnectRPC** for the protobuf request/reply interfaces. Durability, when a plugin wants it, is that plugin's problem — exactly as §4 says.

This is not "use NATS" in the sense Greg is objecting to. It is *"a broker on every node"* — his own middle-path option #4 — and core NATS full mesh **is literally that**, already built, already Go, already a library.

---

## 2. NATS, honestly and specifically

### (a) A full-mesh NATS *cluster* of 3 — what dies when one box dies?

**Nothing that wasn't on the dead box.** [MEASURED]

Three embedded servers in a full mesh, clients pinned to specific nodes with reconnection disabled so nothing could hide a failure:

```
routes: n1=8 n2=8 n3=8 (full mesh)
PHASE 1: all 3 alive
  [all-alive] req/reply n1->n3: OK "hit:all-alive"
  [all-alive] pubsub  n1->n3: OK "log-all-alive"
PHASE 2: kill n2 (a node NEITHER client is connected to)
  routes now: n1=4 n3=4
  [n2-dead] req/reply n1->n3: OK "hit:n2-dead"     <- unaffected
  [n2-dead] pubsub  n1->n3: OK "log-n2-dead"       <- unaffected
PHASE 3: kill n3 (the node client B lives on)
  req/reply after n3 death: err=nats: no responders available for request
  client A (on n1) still connected? true
```

Killing a box you weren't using changes nothing. Killing a box removes *that box's agents* — which is not a bug, that is the correct and only possible semantic. Crucially, node 1 kept working; there is no box whose death is fatal.

**There is no leader in core NATS.** The docs say it outright: *"NATS servers achieve this by gossiping about and connecting to, all of the servers they know, thus dynamically forming a full mesh"* and *"a cluster can grow, shrink and self heal."* [CLAIMED — docs.nats.io/running-a-nats-service/configuration/clustering] — and I confirmed the self-heal claim by measurement below.

**The laptop-sleeps case works.** [MEASURED — `/tmp/natslab/rejoin`]

```
TEST A: start n2 FIRST, when both of its seed peers are down
  n2 started alone in 30ms, routes=0 -- no peer needed to boot
  local client can use lone n2: connected=true err=<nil>
TEST C: 'laptop sleeps' -- kill n1 entirely, then bring it back
  n1 rejoined mesh in 30ms, routes: n1=8 n2=8 n3=8
  n1->n3 request after rejoin: err=<nil> resp=from-n3
```

A box boots in 30ms with **every peer down**, serves its local agents immediately, and re-joins the mesh in 30ms when the others reappear. No coordinator, no bootstrap node, no "wait for the cluster." This is the exact behaviour a sleeping laptop needs.

> ⚠️ One honest caveat on the docs: the NATS clustering page warns that *"the ability for other server and clients to successfully connect depends on seed server running."* That caveat applies to a large fleet where new nodes discover the cluster through a designated seed. It does **not** apply here: with 3 boxes that are all Greg's and all known, each daemon is configured with the other two addresses directly, so every node is a seed. TEST A measured that a node with all its seeds down boots and works anyway.

**Free bonus — the address book Greg asked for, at the transport layer.** [MEASURED]

```
TEST D: client given only n1 now knows servers: [nats://127.0.0.1:4222 nats://127.0.0.1:4223 nats://127.0.0.1:4224]
        discovered (not configured): [nats://127.0.0.1:4223 nats://127.0.0.1:4224]
```
And across the real tailnet: a laptop client told only about its local server auto-discovered `nats://100.115.27.55:4222` (dgx). BRIEF §2 asks for *"a 'shared' address book - so an agent doesn't stumble over the wrong machine name/ip."* Core NATS clients gossip server addresses for free. This does not replace the §3.1 machine roster (which is about *agents* and *attributes*, not servers) but it removes the bootstrap problem underneath it.

### (b) Core NATS (no JetStream) full mesh — does that dodge the objection?

**Yes. Completely.** And it costs almost nothing, because I measured that core NATS already covers 6 of the 7 things BRIEF §3.1 and §5 ask for. [MEASURED — `/tmp/natslab/menu`, 3-node full mesh, JetStream disabled]

```
1. request/reply      : err=<nil> resp="result from dgx"
2. fanout             : dgx got 1, mac-mini got 1 (both = fanout works)
3. point-to-point     : dgx=6 + mac-mini=4 = 10 of 10 (sum=10 => exactly-one delivery)
4. pubsub+wildcard+meta: subject=logs.mbp.claude machine=gb-mbp ts=2026-07-15T23:00:00Z body="agent said something"
5. KV (lookup table)  : with JetStream DISABLED -> err=nats: jetstream not enabled
6. roster scan (no KV): alive peers replying in <500ms: [mac-mini dgx]
7. sender learns 'no listener': err=nats: no responders available for request (immediate, not a timeout)
   ^ returned in 0s despite a 5s timeout => it is a real signal, not a wait
```

Mapping that to the brief line by line:

| BRIEF asks for | Core NATS, no JetStream | Evidence |
|---|---|---|
| §3.1 "point to point" | queue groups — exactly-one delivery, load balanced across boxes | [MEASURED] 10 of 10, no dupes, no drops |
| §3.1 "pubsub" | native, with `>` wildcards | [MEASURED] |
| §3.1 "fanout" | native — every subscriber gets a copy | [MEASURED] |
| §3.1 request/reply, *"the internet is based on that"* | native, first-class | [MEASURED] |
| §4 metadata *"the machine, time, etc"* must ride along | message headers | [MEASURED] `machine=gb-mbp ts=... agent=claude-code-7` |
| §3.1 "publish/lookup table" | ❌ **needs JetStream** | [MEASURED] `nats: jetstream not enabled` |
| §3.1 machine roster baked in | doesn't need a KV — see below | [MEASURED] |
| §5 *"notify the sender that the receiver is not listening"* | ✅ **already solved** — see below | [MEASURED] |

**The §5 gap is already closed, for free.** Greg wrote: *"we actaully should probably notify the sender that the receiver is not listening. I believe we can find that out. So we could do some type of ACK system."* Core NATS has this. `no responders available for request` came back **in 0 seconds despite a 5-second timeout** — it is a positive signal that no subscriber exists anywhere in the cluster, not a timeout. I verified it works *across* nodes (the requester was on n1, the vanished responder was on n3), which means NATS propagates subscription-interest cluster-wide and every node knows when fleet-wide interest hits zero. This is the ACK system Greg sketched, already built, in the core protocol, with no persistence involved.

**The KV gap is real but doesn't bite.** The one thing on the menu that core NATS cannot do is a durable key/value "publish/lookup table" — that is a JetStream feature. Two reasons this doesn't matter:

1. §4 says *"durability is a PLUGIN decision."* A durable lookup table is by definition durable, so by the brief's own rule it is a plugin's problem, not the kernel's. A plugin that wants one can run JetStream, or SQLite, or a file — its choice, its blast radius.
2. The roster — the one thing Greg explicitly wants *baked in* — does not need a KV. His own stated model is *"machines have a list of other machines. Then send pings to each other to check they're online."* That is a request/fanout scan, and I measured it returning both live peers in under 500ms with zero storage. §5 wants liveness as *"a routing input, not a status display"* — a scan is exactly that. Building the roster on a quorum-backed KV would import the Raft dependency Greg is trying to avoid, to solve a problem a broadcast ping already solves.

### (c) What exactly requires Raft/quorum in NATS, and what does not?

**Only JetStream. Confirmed by reading the source, not by trusting docs.** [CONFIRMED-CODE — nats-server v2.14.3]

Every call site that actually *starts* a Raft node lives in one file:

```
jetstream_cluster.go:1046:  s.bootstrapRaftNode(cfg, peers, false)
jetstream_cluster.go:1082:  s.startRaftNode(sysAcc.GetName(), cfg, ...)
jetstream_cluster.go:2990:  s.bootstrapRaftNode(cfg, rgPeers, true)
jetstream_cluster.go:2993:  s.startRaftNode(accName, cfg, labels)
```

`server.go` holds a `raftNodes map[string]RaftNode` and calls `shutdownRaftNodes()` on exit, but nothing outside `jetstream_cluster.go` ever populates it. With JetStream off, that map stays empty and the Raft machinery is inert. This is not inference from documentation — it is every `startRaftNode`/`bootstrapRaftNode` call site in the codebase.

The docs agree on which pieces are Raft-backed: the **Meta Group** (all servers, elects a leader that *"owns the API and takes care of server placement"*), **Stream Groups**, and **Consumer Groups**. *"If there is no leader the stream will not accept messages."* [CLAIMED — docs.nats.io jetstream_clustering]

So the honest summary of Greg's objection:

| NATS feature | Raft? | Fatal-hub risk | Verdict vs §4 |
|---|---|---|---|
| pub/sub, fanout, queue groups, request/reply, headers, no-responders, server gossip | **no** | **none** | ✅ passes |
| JetStream streams / consumers / KV / object store | **yes** | 2-of-3 down = dead; R1 stream = single box owns your data | ❌ violates |

And note the sharpest finding in the JetStream test — **R1 placement is an accidental hub you don't choose**:

```
create R1 stream (replicas=1): err=<nil>
  R1 stream lives on: n2          <- the placement algorithm picked. Not me.
PHASE 2: kill n2
  [1-down] JS publish R1: err=nats: no response from stream
```

I asked for a stream. JetStream silently put it on n2. When n2 died, that data was gone. Greg says *"I dont trust my boxes"* — R1 JetStream is a system that quietly bets your data on one box it chose for you. That is his objection made concrete, and it's a better argument against JetStream than the one he actually made.

### (d) Embedding nats-server as a Go library — is it real?

**Yes. It's ~15 lines and I ran it, including across the real tailnet.** [MEASURED]

```go
s, _ := server.NewServer(&server.Options{
    ServerName: "dgx", Host: "100.115.27.55", Port: 4222,
    Cluster: server.ClusterOpts{Name: "fleet", Host: "...", Port: 6222},
    Routes:  server.RoutesFromStr("nats://100.87.151.114:6222,nats://100.120.22.74:6222"),
})
go s.Start()
s.ReadyForConnections(10 * time.Second)
```

**Real-fleet numbers, laptop ↔ dgx over Tailscale** [MEASURED — I cross-compiled a Go daemon to linux/arm64, scp'd it to dgx (100.115.27.55), clustered gb-mbp to it over the tailnet, then removed it]:

```
routes=4 (cluster route over Tailscale established)
client auto-discovered servers: [nats://100.115.27.55:4222]
  req->dgx OK "pong from dgx"
  req/reply to dgx over Tailscale: best=3.67ms mean=5.669ms (n=20)
  1000-byte payload cross-box: OK rtt=4.021ms
  65536-byte payload cross-box: OK rtt=18.965ms
  921600-byte payload cross-box: OK rtt=101.611ms
```

Notes for the build:
- **Binary cost:** 15 MB stripped for a full embedded server + client on linux/arm64; 22 MB unstripped on darwin/arm64. [MEASURED]
- **Dependency cost:** 24 modules in the tree. [MEASURED] For comparison, libp2p is 129.
- **MTU 1280 is a non-issue for NATS** — it's TCP, the kernel clamps the segment size, and a 900 KB payload crossed the tailnet fine. [MEASURED]
- **Max payload is 1 MB by default**, warn-capped at 8 MB. [CONFIRMED-CODE — `const.go:94 MAX_PAYLOAD_SIZE = (1024 * 1024)`, `const.go:99 MAX_PAYLOAD_MAX_SIZE = (8 * 1024 * 1024)`]. Log lines and agent messages are fine. Anything bigger is a plugin's job (chunk it, or pass a path).
- **Live config reload exists in-process:** `func (s *Server) ReloadOptions(newOpts *Options) error` [CONFIRMED-CODE — `reload.go:1417`]. Relevant to §7.2's live-reload question: you can add/remove channels and routes without restarting the daemon. This is *not* Go plugin hot-loading — that's a separate agent's question — but the transport layer itself will not force a restart.

---

## 3. Peer-to-peer / no-broker-by-design

### The MTU finding that kills the whole QUIC family — [MEASURED, then root-caused, then proven]

This is the most important thing in this section and it applies to **libp2p's QUIC transport, raw quic-go, and Iroh alike**.

**Raw quic-go, default config, laptop → dgx over Tailscale:**
```
QUIC DIAL FAIL: timeout: no recent network activity
```
**libp2p over QUIC, same link:**
```
CONNECT FAIL: failed to dial: all dials failed
  * [/ip4/100.115.27.55/udp/9111/quic-v1] context deadline exceeded
```

First I ruled out the obvious explanation — that Tailscale blocks UDP. It doesn't: [MEASURED]
```
  UDP    64 bytes: ok=true  rtt=4.505ms
  UDP  1252 bytes: ok=true  rtt=4.801ms
  UDP  1472 bytes: ok=true  rtt=3.443ms
  UDP  9000 bytes: ok=true  rtt=6.158ms
```

The real cause, found in the source: [CONFIRMED-CODE — quic-go v0.60.0]
```
internal/protocol/params.go:12: const InitialPacketSize = 1280
```
That 1280 is the **UDP payload** size. Add an 8-byte UDP header and a 20-byte IPv4 header and you are putting **1308 bytes on a 1280-byte link**. QUIC sets the don't-fragment bit, so the handshake packets are simply dropped. Plain UDP survived at 1472 and 9000 bytes only because it *allows* fragmentation; QUIC refuses it by design.

**Proof:** I set one field, `InitialPacketSize: 1252` (= 1280 − 20 − 8), redeployed to dgx, and re-ran the identical test: [MEASURED]
```
quic handshake over Tailscale: 14ms
  quic echo 64 bytes: got=64 rtt=5.019ms
```
From total failure to a 14 ms handshake, from one number. The diagnosis is confirmed.

**Now the part that matters for libp2p:** libp2p hardcodes its QUIC config in an unexported package-level variable and **never sets `InitialPacketSize`**: [CONFIRMED-CODE — go-libp2p v0.48.0, `p2p/transport/quicreuse/config.go`]
```go
var quicConfig = &quic.Config{
	MaxIncomingStreams:         256,
	MaxIncomingUniStreams:      5,
	MaxStreamReceiveWindow:     10 * (1 << 20),
	MaxConnectionReceiveWindow: 15 * (1 << 20),
	KeepAlivePeriod:            15 * time.Second,
	Versions:                   []quic.Version{quic.Version1},
	EnableDatagrams: true,
	EnableStreamResetPartialDelivery: true,
}
```
A grep for `InitialPacketSize` across the entire go-libp2p module returns **nothing**. So libp2p inherits the broken 1280 default and **exposes no knob to fix it**. On Greg's network, libp2p is TCP-only, and you have to know to disable QUIC or half your dials mysteriously hang.

### libp2p — loses

It *does* work over TCP on the real link: [MEASURED, laptop → dgx over Tailscale]
```
=== TCP over Tailscale:
connected in 23ms
echo rtt=5.474ms resp="ping"
```
5.47 ms — same ballpark as NATS's 3.67 ms. So it's viable. It's just enormously worse value:

- **129 modules** in the dependency tree vs NATS's 24. [MEASURED]
- **22.1 MB** stripped binary for a host that does nothing but echo. [MEASURED]
- **Its QUIC transport is broken on this network with no supported fix.** [CONFIRMED-CODE + MEASURED]
- **It gives you none of the brief's transport menu.** libp2p gives you streams between peers. Pub/sub means adding go-libp2p-pubsub (gossipsub). Request/reply you write yourself. Fanout you write yourself. Queue-group/point-to-point you write yourself. Every row of §3.1's menu that core NATS handed me in one measured run is a component you assemble here.
- **You are paying for problems you don't have.** libp2p exists to solve NAT traversal, peer identity among strangers, DHT-based discovery, and transport negotiation across a hostile internet. BRIEF §4: *"Trusted network. 3 boxes, all his."* Tailscale already did NAT traversal and identity. libp2p's entire reason for existing has already been paid for by a tool Greg is already running.

Alive and healthy (v0.48.0, 2026-03-17 [MEASURED — GitHub API]) — it just isn't for this.

### Iroh — disqualified, decisively

**There is no Go story.** [MEASURED]

`go get github.com/n0-computer/iroh@v1.0.2` "succeeds" — Go's module proxy will happily vendor any tagged repository. But I cloned it: **zero `.go` files, a `Cargo.toml` at the root.** It is a pure Rust crate. The Go proxy is lying to you.

The official bindings repo says so explicitly: *"This repo defines Python, Swift, Kotlin and Node.js bindings for iroh, which is written in Rust."* [CLAIMED — github.com/n0-computer/iroh-ffi README]. Go appears in that README exactly once, in a list, as: *"[Go](https://git.coopcloud.tech/decentral1se/iroh-go) (Community maintained)"*.

I cloned that community binding too. It is alive (last commit 2026-07-03 [MEASURED]) and it is not usable here:

```
libs.go:3: // #cgo linux,amd64 LDFLAGS: -L${SRCDIR}/libs/x86_64-unknown-linux-musl -liroh -lm
libs.go:4: // #cgo linux,arm64 LDFLAGS: -L${SRCDIR}/libs/aarch64-unknown-linux-musl -liroh -lm
$ ls libs/
aarch64-unknown-linux-musl
x86_64-unknown-linux-musl
```

**Linux only. There is no darwin build.** Two of Greg's three boxes are macOS (`gb-mbp`, `gb-mac-mini`). It is also cgo (94 MB of prebuilt Rust static libs), its own README says it *"need[s] to patch the upstream iroh-ffi"* in two places to build at all, and that *"error handling is extremely inconvenient."*

And on top of all that: Iroh is QUIC-based, so §3's MTU finding applies to it too. Against *"I'd kinda like to stick with go"* this isn't close.

### Raw QUIC (quic-go) — loses, but is honestly decent

It works once you know the MTU trick (14 ms handshake, 5.0 ms echo, 20 modules, 2.6 MB — all [MEASURED]). quic-go is excellent software. But it is a *transport*, not a *messaging system*. Choosing it means writing, yourself: connection management across a sleeping laptop, reconnect/backoff, framing, pub/sub, fanout, queue groups, request/reply correlation, subscription-interest propagation, and the no-responders signal. That last one alone — knowing cluster-wide that nobody is listening (§5) — is a distributed interest-propagation protocol. Core NATS handed me all of it, measured, today. Rebuilding it is months, for one operator, to arrive at a worse core NATS.

### Plain TCP + protobuf + a connection manager you write — loses, and it's the same argument, more so

Everything in the quic-go paragraph, plus you also write the encryption story (Tailscale gives you that, fine), plus you get none of quic-go's stream multiplexing. Zero dependencies is a real virtue and §4's *"one operator's operational budget"* is a real axis — but the budget argument cuts *against* this, not for it. The code you don't write is the code you don't debug at 11pm when the laptop wakes up and half the fleet thinks it's still asleep.

---

## 4. RPC layers

### ConnectRPC — **wins**, and BRIEF §3.2 is nearly a description of it

Greg wrote: *"I say protobuf because we can the define it COMPLETELY independently of any code. Also could be cool because the same interfaces could be available through REST/pubsub/whatever."*

I built it and measured it. One `.proto`, one Go handler, one port. [MEASURED — connect-go v1.20.0, `/tmp/connlab`]

```proto
service SearchService {
  rpc Search(SearchRequest) returns (SearchResult);
  rpc Tail(SearchRequest) returns (stream SearchResult);
}
```

**1. Plain curl with JSON — no client library, no gRPC tooling, no codegen on the caller side:**
```
$ curl -s -X POST http://127.0.0.1:8080/fleet.v1.SearchService/Search \
    -H "Content-Type: application/json" -d '{"query":"how to ssh to dgx"}'
{"text":"found: how to ssh to dgx", "machine":"dgx", "ts":"1752624000"}
```

**2. The same endpoint, binary protobuf:**
```
http=200 type=application/proto
```

**3. Server-streaming (the log-tail plugin shape) over plain curl:**
```
{"text":"log line 0", "machine":"dgx"}
{"text":"log line 1", "machine":"dgx", "ts":"1"}
{"text":"log line 2", "machine":"dgx", "ts":"2"}
```

**4. A real `google.golang.org/grpc` client — no Connect code on the client side at all — against that same handler:**
```
grpc unary  -> err=<nil> resp="found: unary via real grpc" machine="dgx"
grpc stream -> "log line 0"
grpc stream -> "log line 1"
grpc stream -> "log line 2"
grpc stream end: EOF
```

That is §3.2, measured, in one afternoon. The docs' claim — *"By default, connect-go servers support ingress from all three protocols without any configuration"* (Connect, gRPC, gRPC-Web) [CLAIMED — connectrpc.com/docs/go/getting-started] — is true; I verified two of the three directly.

Why this matters beyond the demo: **an agent can call a fleet interface with `curl`.** No SDK, no stub generation, no gRPC reflection dance. BRIEF §4 says no agent should *"screw around figuring out the network crap."* A protobuf-defined interface that answers curl is the shortest path from "an agent wants a thing" to "the agent got the thing."

- **4 modules** in the dependency tree — the lightest thing in this entire report except Twirp. [MEASURED]
- Alive: v1.20.0, 2026-05-20. [MEASURED — GitHub API]
- It is plain `net/http`. `http.Handler` in, `http.Handler` out. No custom server, no HTTP/2 requirement (works on HTTP/1.1), no special transport.

**How it composes with NATS:** they solve different problems and stack cleanly. Connect defines and serves the *interfaces* (§3.2's versioned protobuf contracts, curl-able). Core NATS moves the *channel* bytes (§3.1's pub/sub, fanout, point-to-point) and provides interest/liveness. A plugin's request/reply can go over either — over Connect when the caller knows the box, over NATS request/reply when it wants the fleet to route by subject and wants the no-responders signal. The protobuf message types are the same objects in both cases, which is the whole point of defining them *"COMPLETELY independently of any code."*

### gRPC — loses to Connect on every axis Greg named

It works — I proved it interoperates by pointing a real gRPC client at the Connect handler. But: 39 modules vs Connect's 4 [MEASURED]; you cannot curl it without extra tooling; it needs HTTP/2 (so h2c gymnastics on a plain port); and its bidirectional streaming — the specific feature named in the task — requires HTTP/2 trailers, which Connect's own protocol deliberately avoids. Connect gives you the gRPC wire protocol *as an option* while defaulting to something a human can debug with curl. There is no axis in this brief on which raw gRPC wins.

### Cap'n Proto RPC — loses

The promise is real (zero-copy, promise pipelining). The Go reality: **v3.1.0-alpha.2** is the latest tag [MEASURED — `go list -m -versions`]. The repo says *"Until the official Cap'n Proto spec is finalized, this repository should be considered beta software"* and **Level 3 RPC is a work in progress** [CLAIMED — github.com/capnproto/go-capnp]. Last commit 2025-10-24 — about 9 months stale [MEASURED — GitHub API]. It also abandons protobuf, which §3.2 asks for by name and for a stated reason. Betting a fleet's interface layer on an alpha with a non-final spec, to gain performance on a 3.67 ms link that is nowhere near the bottleneck, is a bad trade.

### Twirp — loses on a single fact

**No streaming.** *"Currently, Twirp does not implement streaming because streams require assumptions about how the connection state is managed, they are complex and not required by the majority of Twirp users."* [CLAIMED — twitchtv/twirp issue #3 / project docs]. Streaming has been proposed since issue #3 and never landed.

BRIEF §3.2 lists *log tail* and *log archiving* as first-class plugins. Log tail is server-streaming. Twirp cannot express it.

Also: last commit **2024-08-05**, last release **v8.1.3 (Oct 2022)** — roughly two and four years stale respectively [MEASURED — GitHub API]. Its one genuine virtue (1 module, the lightest possible dep tree [MEASURED]) is beaten by Connect at 4 modules, which also streams and also speaks gRPC.

---

## 5. Middle paths

### "A broker on every node with plugin-chosen replication" — **this is the winner, and it already exists**

This option is the recommendation. The insight is that **core NATS in a full mesh IS a broker on every node.** Each box runs its own embedded broker; agents talk to *their local* broker over loopback; brokers route to each other in a full mesh; there is no hub. "Plugin-chosen replication" is precisely §4's *"the plugin defines that!"* — the kernel replicates nothing, so there is nothing to lose quorum over.

Reframing Greg's objection: he is not objecting to NATS, he is objecting to **hub-and-spoke** — the topology the previous round chose (a NATS broker on the DGX, which §9 flags as violating §4). His instinct was sound and his diagnosis was aimed one layer too high. The bug was the topology, not the product.

### Gossip (memberlist) for membership + direct connections for data — loses, and carries an MTU landmine

memberlist is unicast gossip with no multicast [CONFIRMED-CODE — grep for `multicast|224\.` across the module returns nothing], so it is compatible with Tailscale's L3-no-multicast reality. But:

- **It has the same MTU ambush as QUIC, in a quieter form.** `DefaultLANConfig()` sets `UDPBufferSize: 1400` [CONFIRMED-CODE — `config.go:336`] and uses it to size outgoing gossip packets [CONFIRMED-CODE — `net.go:800`, `state.go:614`]. 1400 + 8 + 20 = 1428 bytes on a 1280-byte link. Unlike QUIC this *degrades* rather than *fails* — plain UDP fragments, and I measured 1472-byte datagrams surviving the tailnet — but you'd be IP-fragmenting every gossip packet forever unless you set `UDPBufferSize: 1252`. [DEDUCED from the two measurements; I did not run memberlist end-to-end.]
- **102 modules** [MEASURED] — 4× NATS's tree, for membership *only*.
- It solves **one row** of the brief. You still need pub/sub, fanout, queue groups, request/reply, and the no-responders signal on top, over connections you manage yourself.
- **The problem it solves is already solved here.** memberlist is SWIM: scalable failure detection for large clusters where you can't afford all-to-all probing. Greg has **three boxes**. All-to-all is 3 pings. I measured the whole roster answering in <500ms with a single fanout request. §6: *"Scale beyond ~3 boxes... explicitly NOT the problem."*

If a future roster plugin wants richer membership semantics, memberlist is a fine *plugin* dependency. It is not kernel material here.

### SQLite + Litestream — loses (it's the wrong category)

Litestream is *"a background process that continuously copies write-ahead log pages from disk to a replica"*, is **single-writer**, is asynchronous, and is *"fundamentally a backup and disaster-recovery tool, not a clustering solution"* [CLAIMED — litestream.io/how-it-works]. It's excellent and alive (v0.5.14, 2026-07-06 [MEASURED]).

It does not give the fleet shared memory. It gives *one box's* SQLite file a backup. If that box is asleep, its data is not readable by anyone. BRIEF §2 is *"something figured out on one machine cannot be used on another machine"* — Litestream does not address it. (Wonderful footnote: one of Litestream's supported replica targets is *NATS JetStream*. It is a consumer of a backbone, not a backbone.)

Perfectly reasonable as a **plugin's** durability choice under §4 — a notes plugin backing up its SQLite to the DGX is a fine, small, box-local decision.

### rqlite — loses, for exactly the JetStream reason

rqlite is Raft. *"For a cluster of N nodes in size to remain operational, at least (N/2)+1 nodes must be up and running"* — 2 of 3 [CLAIMED — rqlite.io/docs/clustering/general-guidelines]. Same quorum wall as JetStream, same failure mode. Worse, recovery from quorum loss is **manual**: *"Recovery requires... stopping all nodes and creating a peers.json file."*

For a man whose laptop sleeps and who says *"I dont trust my boxes"*, a storage layer that needs hand surgery when two boxes hiccup is the opposite of the ask. Very alive (v10.2.7, 2026-07-06 [MEASURED]) and a legitimate **plugin** choice under §4 — the kernel just must not depend on it.

### ZeroMQ / NNG

Owned by a sibling agent; not assessed here. One boundary note that is mine to give and shouldn't collide: ZeroMQ is a *brokerless socket library*, so it sits in the same category as raw QUIC and raw TCP above — it gives you excellent socket patterns (REQ/REP, PUB/SUB, PUSH/PULL) and hands you the connection management, liveness, interest propagation, and no-responders problems to solve yourself. My measured claim that core NATS full mesh has no hub, no Raft, and no fatal box should be weighed against ZeroMQ's assessment on its own merits — but Greg's premise in §4, *"with ZeroMQ we can create more of a distributed system... without any other implications - call it something else if you want - not-centralized"*, is a property core NATS full mesh **also has**, measured. Not-centralized is not a reason to prefer ZeroMQ over core NATS. There may be others; that's the sibling's call.

---

## 6. Comparison table

Judged against BRIEF §4's real axes. ✅ = measured/confirmed good, ⚠️ = works with a caveat, ❌ = fails.

| Candidate | Not-centralized | Go | Tailscale L3 / MTU 1280 | Laptop sleeps | Plugin durability | Protobuf | req/rep + pubsub + p2p + fanout | Operator budget | Verdict |
|---|---|---|---|---|---|---|---|---|---|
| **Core NATS, embedded, full mesh** | ✅ no leader, no Raft, survives 2/3 dead [M] | ✅ library, 24 mods, 15 MB [M] | ✅ TCP, 900 KB @ 101 ms [M] | ✅ boots alone 30 ms, rejoins 30 ms [M] | ✅ kernel stores nothing | ✅ headers + any payload [M] | ✅ **all four measured** [M] | ✅ one dep, one config struct | **WIN** |
| NATS + JetStream | ❌ **dies at 2/3 down; R1 picks a box for you** [M] | ✅ | ✅ | ⚠️ | ❌ kernel-level durability | ✅ | ✅ + KV | ⚠️ Raft to operate | **Greg's objection is right, here** |
| **ConnectRPC** | ✅ nothing central | ✅ **4 mods** [M] | ✅ HTTP/1.1 | ✅ stateless | ✅ n/a | ✅ **native, curl-able** [M] | ⚠️ req/rep + streaming only | ✅ plain net/http | **WIN (pairs with NATS)** |
| gRPC | ✅ | ✅ 39 mods [M] | ✅ | ✅ | ✅ n/a | ✅ not curl-able | ⚠️ req/rep + streams only | ⚠️ HTTP/2, h2c | loses to Connect |
| libp2p | ✅ | ✅ **129 mods, 22 MB** [M] | ⚠️ **TCP ok (5.47 ms); QUIC broken, no knob** [M+C] | ✅ | ✅ | ⚠️ BYO | ❌ **BYO all four** | ❌ solves NAT/identity you don't have | loses |
| Iroh | ✅ | ❌ **Rust; Go binding is cgo, linux-only** [M] | ❌ QUIC → MTU trap | ? | ✅ | ⚠️ | ⚠️ | ❌ patch upstream to build | **disqualified** |
| Raw QUIC (quic-go) | ✅ | ✅ 20 mods, 2.6 MB [M] | ⚠️ **broken by default; 1 field fixes it** [M+C] | ⚠️ BYO | ✅ | ⚠️ BYO | ❌ BYO all four | ❌ months of rebuild | loses |
| TCP + protobuf + own conn mgr | ✅ | ✅ 0 deps | ✅ | ❌ BYO | ✅ | ⚠️ BYO | ❌ BYO all four | ❌ worst | loses |
| Cap'n Proto RPC | ✅ | ⚠️ **alpha, 9 mo stale** [M] | ✅ | ✅ | ✅ | ❌ not protobuf | ⚠️ | ❌ alpha + non-final spec | loses |
| Twirp | ✅ | ✅ 1 mod [M] | ✅ | ✅ | ✅ | ✅ | ❌ **no streaming, ever** | ⚠️ **2 yr stale** [M] | loses |
| memberlist + direct conns | ✅ | ✅ **102 mods** [M] | ⚠️ **1400 > 1280; fragments** [C] | ✅ SWIM | ✅ | ⚠️ BYO | ❌ membership only | ⚠️ SWIM for 3 boxes | loses |
| rqlite | ❌ **quorum; manual peers.json recovery** | ✅ | ✅ | ❌ | ❌ kernel-level | ⚠️ | ❌ | ⚠️ | loses (fine as plugin) |
| SQLite + Litestream | ⚠️ **single-writer backup, not shared** | ✅ | ✅ | ❌ asleep = unreadable | ✅ | ⚠️ | ❌ | ✅ | wrong category (fine as plugin) |

[M] = measured today · [C] = confirmed by reading source

---

## 7. The recommendation

**Kernel transport: embed `nats-server` v2.14.x as a Go library on each of the three boxes. Core only — `JetStream: false`. Full mesh — each daemon's `Routes` lists the other two Tailscale IPs. Agents connect to their own box's daemon over loopback.**

**Kernel interfaces: ConnectRPC over plain `net/http`, protobuf-defined, curl-able.**

Why this and not the alternatives:

1. **It satisfies §4 as measured, not as argued.** No leader, no Raft, no quorum, no fatal box. Two of three boxes killed and the survivor still served pub/sub and request/reply. Every `startRaftNode` call site in the codebase is inside `jetstream_cluster.go`, and JetStream is off.
2. **It is "a broker on every node"** — Greg's own middle path — already written, already Go, already a library, already tested by the world.
3. **It hands over §3.1's entire transport menu on day one**, measured: request/reply, pub/sub with wildcards, fanout, point-to-point via queue groups, and metadata headers. The only menu item it misses is the durable lookup table, which §4 makes a plugin's job anyway.
4. **It closes §5's known gap for free.** `no responders available` returns in 0 seconds, cluster-wide, in core. Greg said *"I believe we can find that out"* — he was right, and it's already built.
5. **It answers §3.2 exactly.** One protobuf service, answered by curl+JSON, by binary protobuf, by Connect streaming, and by a stock gRPC client — all measured, all on one port, all from one `.proto`.
6. **It fits one operator's budget.** Two dependencies, 28 modules combined, ~15 MB of binary, one config struct, zero new operational concepts. libp2p asks for 129 modules to solve NAT traversal that Tailscale already solved. quic-go and raw TCP ask for months of rebuilding interest propagation to arrive at a worse core NATS.

**What Greg gives up by not using JetStream:** durable message replay, at-least-once delivery, and the KV store. All three are — by §4's own rule — plugin decisions. A plugin that wants them can enable JetStream *in its own scope*, or use SQLite, or use a file. The kernel stays memory-only and unkillable. That is the trade, and it is the trade the brief already asked for.

**What to tell Greg in one sentence:** *your instinct was right about the shape and wrong about the label — the thing you don't want is a hub, and core NATS in a full mesh doesn't have one; the Raft you're afraid of lives entirely in JetStream, which the brief already told us to throw away.*

### Build notes worth pinning

- `JetStream: false` is not a footnote, it is the whole architectural decision. Enabling it later, casually, re-imports every problem in §2(c).
- **Do not use QUIC anywhere on this network without setting `InitialPacketSize: 1252`.** If a future component uses QUIC, this is the ambush. libp2p offers no way to set it.
- Payloads over 1 MB need a plugin-level answer (chunk, or pass a path). [CONFIRMED-CODE]
- `s.ReloadOptions(newOpts)` lets you add channels/routes without restarting the daemon. [CONFIRMED-CODE — `reload.go:1417`]

---

## 8. Open questions

1. **Sleeping-laptop reconnect behaviour under macOS suspend.** I measured process kill/restart (30 ms rejoin), not a real `pmset sleep` cycle with a half-open TCP connection. A suspended laptop's routes may sit in a half-open state until TCP keepalive or NATS's ping interval reaps them. NATS has `PingInterval`/`MaxPingsOut` for exactly this; I did not measure the actual detection latency across a real sleep. **This is the one gap I'd close before building.**
2. **How the §3.1 machine roster is actually built.** I measured that a fanout ping answers "who's alive" in <500ms with no KV, which is Greg's own model. I did *not* design the roster — where agent-name→box/IP/attributes lives, whether it's gossiped over a channel or held per-daemon and reconciled. That is a design question for whoever owns §3.1, and my finding is only that it does not *need* a quorum-backed KV.
3. **Go plugin live-loading (§7.2).** Out of my scope. I confirmed only that the *transport* doesn't force restarts (`ReloadOptions`). Whether Go can hot-load plugin code at all is a different agent's question, and BEAM envy may not survive it.
4. **Whether NATS subjects are the right shape for channel names (§3.1).** A channel has "a name and a type." NATS subjects are hierarchical dot-separated strings with wildcards, which maps naturally onto names — but the *type* (pubsub vs fanout vs queue-group vs request/reply) is a property of how you *subscribe*, not of the subject itself. Making "type" a first-class channel property means the kernel must enforce subscription discipline. Doable, not designed here.
5. **memberlist over Tailscale end-to-end.** I read the 1400-byte default in the source and measured that fragmented UDP survives the link, but did not run memberlist itself. My "degrades, doesn't fail" claim is deduced from those two facts, not observed.
6. **Twirp's streaming stance.** The quote is from search results summarizing the project's FAQ/issue #3; the canonical FAQ URLs I tried returned 404. The substance (no streaming, proposed since issue #3, never landed) is corroborated by the issue titles and the 2-year-stale commit history, but I did not read the FAQ page itself.

---

## Sources

### Commands run / experiments written (all executed today, 2026-07-15)

- `/tmp/natslab/core/main.go` — 3-node embedded core-NATS full mesh; killed n2 then n3; measured req/reply + pubsub survival and the `no responders` signal.
- `/tmp/natslab/js/main.go` — 3-node embedded cluster **with** JetStream; R1 and R3 streams; killed 1 then 2 nodes; measured quorum loss vs core NATS survival on the same lone node.
- `/tmp/natslab/rejoin/main.go` — boot-with-all-peers-down; mesh formation with no coordinator; kill-and-restart rejoin timing; client server auto-discovery.
- `/tmp/natslab/menu/main.go` — core-NATS coverage of BRIEF §3.1's transport menu: request/reply, fanout, queue-group point-to-point, pubsub+wildcards+headers, KV absence, roster scan, no-responders immediacy.
- `/tmp/natslab/node/main.go` — embedded NATS daemon, cross-compiled `GOOS=linux GOARCH=arm64`, deployed to dgx (100.115.27.55) over Tailscale, clustered from gb-mbp; measured 3.67 ms best RTT and 900 KB payload transfer. **Removed from dgx after testing.**
- `/tmp/real/libp2p/main.go` — real libp2p host with TCP + QUIC listeners, deployed to dgx; measured TCP success (5.47 ms) and QUIC failure over Tailscale. **Removed from dgx.**
- `/tmp/udp/main.go` — raw UDP echo, dgx ↔ gb-mbp, datagram sizes 64→9000 bytes; ruled out a general UDP block. **Removed from dgx.**
- `/tmp/qtest/main.go` — raw quic-go echo, dgx ↔ gb-mbp; measured default-config failure, then success with `InitialPacketSize: 1252`. **Removed from dgx.**
- `/tmp/connlab/` — `proto/fleet.proto` + buf codegen + Connect handler + real gRPC client; measured curl+JSON, binary protobuf, Connect streaming via curl, and gRPC unary+streaming interop against one handler.
- `ifconfig utun0` → `mtu 1280`, `inet 100.87.151.114` (Tailscale interface identified by IP match).
- `tailscale status` — fleet liveness: gb-mbp 100.87.151.114, dgx 100.115.27.55, gb-mac-mini 100.120.22.74.
- `go version` → go1.26.2 darwin/arm64.
- `go list -m -versions` for all candidate modules; `go list -m all | wc -l` for dependency-tree sizes; `go build -ldflags="-s -w"` for binary sizes.
- `git clone` of `github.com/n0-computer/iroh` (zero `.go` files, `Cargo.toml` present), `github.com/n0-computer/iroh-ffi`, `git.coopcloud.tech/decentral1se/iroh-go` (cgo, linux-only `libs/`).
- GitHub API (`api.github.com/repos/.../releases/latest`, `.../commits?per_page=1`) for project liveness of twirp, go-libp2p, connect-go, nats-server, go-capnp, rqlite, litestream.

### Source files read

- `nats-server v2.14.3`: `server/jetstream_cluster.go:1046,1082,2990,2993` (all `startRaftNode`/`bootstrapRaftNode` call sites); `server/server.go:336,2576,2605` (`raftNodes` map, lifecycle sweeps); `server/const.go:92-99` (`MAX_PAYLOAD_SIZE`, `MAX_PAYLOAD_MAX_SIZE`); `server/reload.go:1396,1417` (`Reload`, `ReloadOptions`); `server/opts.go:5911` (`RoutesFromStr`).
- `quic-go v0.60.0`: `internal/protocol/params.go:12` (`const InitialPacketSize = 1280`); `config.go:42-46,103-123`.
- `go-libp2p v0.48.0`: `p2p/transport/quicreuse/config.go` (hardcoded `quicConfig`, no `InitialPacketSize`); module-wide grep for `InitialPacketSize` → no results.
- `hashicorp/memberlist v0.6.0`: `config.go:241,336` (`UDPBufferSize: 1400`); `net.go:800`, `state.go:614` (packet sizing); module-wide grep for `multicast|224\.` → no results.
- `git.coopcloud.tech/decentral1se/iroh-go`: `libs.go:3-5` (cgo LDFLAGS, linux-only), `README.md` (known issues), `libs/` directory listing.
- `github.com/n0-computer/iroh-ffi`: `README.md` (bindings language list).
- `/Users/gb/research/2026-07-15-agent-substrate-v2/BRIEF.md` (in full).

### URLs fetched

- https://docs.nats.io/running-a-nats-service/configuration/clustering
- https://docs.nats.io/running-a-nats-service/configuration/clustering/jetstream_clustering
- https://connectrpc.com/docs/go/getting-started
- https://litestream.io/how-it-works/
- https://rqlite.io/docs/clustering/general-guidelines/
- https://github.com/twitchtv/twirp
- https://github.com/capnproto/go-capnp
- https://github.com/twitchtv/twirp/issues/3 (Twirp streaming proposal — via search results)
- https://twitchtv.github.io/twirp/docs/intro.html (via search results)
- https://github.com/quic-go/quic-go/wiki/UDP-Buffer-Sizes (referenced by quic-go's runtime warning; not fetched)

### Not read, deliberately

- `/Users/gb/research/2026-07-15-agent-comms-substrate/` — per instructions, context-poison risk; a sibling agent owns its salvage.
