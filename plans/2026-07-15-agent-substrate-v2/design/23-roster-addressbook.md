# 23 — The Roster, Presence, and the Address Book

**Date:** 2026-07-16
**Scope:** BRIEF §3.1 (machine roster baked into core), §2 and §4 (the reachability pain, the address book, SSH as a plugin), §5 (liveness as a routing input, the ACK gap).
**Input read in full:** `investigate/13-roster-and-addressbook.md`. Also read: `investigate/11-transport-options.md` (§5, §7), `investigate/14-prior-art.md` (memberlist findings), BRIEF in full.

**Labels used throughout, per the ground rules:**
- **[MEASURED]** — I ran it today and this is the output.
- **[CLAIMED]** — an upstream doc or a sibling investigation says so; I did not verify it myself.
- **[DEDUCED]** — I reasoned to it. Flagged because the last round's worst errors were confident deductions.

---

## 0. Verdict up front

**Do not use `hashicorp/memberlist`. Write the roster: a static peer list plus direct all-to-all probing, ~350–500 lines of Go.** This overturns the recommendation in `investigate/13`, and I do not do that lightly — investigation 13 is careful, well-measured work and its measurements all reproduce. What it got wrong is the inference, and I re-verified its own load-bearing source facts to show why:

> **At n=3, memberlist's flagship feature is disabled by its own code.** `state.go:1211-1219`: `k := SuspicionMult - 2` (= 2), then `if n-2 < k { k = 0 }`. With 3 nodes, `n-2 = 1 < 2`, so **k = 0**. SWIM's independent-confirmation machinery — the thing you import the library *for* — does not engage on this fleet. [MEASURED — I read the source myself; see §1.2.]

Investigation 13 measured this fact and reported it accurately, then recommended the library anyway. The three properties it says justify the import — incarnation numbers, suspicion, anti-entropy — **are all machinery for problems that only exist because you chose gossip.** Delete the gossip and all three problems vanish rather than needing solutions. I built the alternative and measured it (§1.4): it matches memberlist's detection latency, and it makes investigation 13's own #1 open question ("the true 8-hour sleep — I did not verify this") *structurally impossible to fail*.

**Three more decisions, stated up front:**

1. **The kernel/plugin line, which is the real question in this subsystem.** The kernel owns the node list, liveness, and addresses. It also owns a generic **opaque per-node state slot** mechanism. The agent list is a *plugin's payload inside that mechanism* — the kernel never parses it. This makes both of Greg's apparently-conflicting quotes true at once (§3). The test: `grep -ri agent kernel/` returns **zero hits**.

2. **The address book is thin, because Tailscale already does the hard half.** The kernel stores exactly **one address per box** (the Tailscale IP) and **never resolves a name**. Investigation 13 measured that DNS, `.local`, LAN IPs and `known_hosts` are the *source* of the pain, not the cure — so the design deletes the step where an agent has to find out (§5).

3. **The SSH plugin never moves a credential.** Its highest-value action is a one-line human decision — enable Tailscale SSH on `dgx` — which deletes most of the key-distribution problem outright (§6).

---

## 1. The membership mechanism

### 1.1 What the requirement actually is

BRIEF §3.1, Greg's own words: *"machines have a list of other machines. Then send pings to each other to check they're online."*

Read that literally, because it is exactly right: **a list** (static, known in advance) **and pings** (direct, to each box). He also scoped it: *"Its kinda only there to keep track of whos around. I dont want to get hung up on CAP theorem and shit."* And BRIEF §6 puts *"scale beyond ~3 boxes"* out of scope.

This matters more than it looks. There are two different jobs that get bundled together and called "membership":

- **Membership discovery** — *finding out which machines exist* when you don't know in advance. This is what gossip is for. Nodes join and leave a cluster you can't enumerate, so knowledge has to spread epidemically.
- **Failure detection** — *finding out whether machines you already know about are answering*.

**We need only the second.** The fleet is three boxes named in a config file. Investigation 13 reached the same place from a different direction — its own open question #7 says *"the roster should be an **explicit allow-list of the 3 boxes**, not 'everything on the tailnet'."*

I measured why that allow-list is not optional. The tailnet today has **8 peers, of which 3 are fleet** [MEASURED, `tailscale status --json`]:

| HostName | Tailscale IP | OS | Online | Fleet? |
|---|---|---|---|---|
| `gb-mbp` | 100.87.151.114 | macOS | true | **yes** |
| `dgx` | 100.115.27.55 | linux | true | **yes** |
| `gb-mac-mini` | 100.120.22.74 | macOS | true | **yes** |
| `localhost` | 100.106.72.17 | iOS | **true** | no |
| `localhost` | 100.106.65.121 | iOS | false | no |
| `gb-mac` | 100.82.109.44 | macOS | false | no |
| `gb-mac` | 100.78.163.71 | macOS | false | no |
| `nvsync-gb-mac-mini` | 100.103.144.28 | macOS | false | no |

Note the two devices **both named `localhost`**, one of them **currently online**, and the two **both named `gb-mac`**. Any design that auto-discovers tailnet peers inherits a namespace with live duplicate names in it. **The node list is a config file. This is settled by measurement, not taste.**

Once the list is static, membership discovery is not a requirement at all — and gossip is a solution to a problem we don't have.

### 1.2 Why memberlist loses — its own source, re-verified today

I re-read every load-bearing claim from investigation 13 in `~/go/pkg/mod/github.com/hashicorp/memberlist@v0.6.0`. All reproduce exactly. Here is what they add up to:

**(a) SWIM's indirect confirmation is switched off at n=3.** [MEASURED — `state.go:1211-1219`]

```go
k := m.config.SuspicionMult - 2        // = 4 - 2 = 2
n := m.estNumNodes()                   // = 3
if n-2 < k {                           // 1 < 2  -> TRUE
    k = 0
}
```
and `suspicion.go:72-75`:
```go
timeout := max
if k < 1 {           // k == 0
    timeout = min    // flat minimum, no waiting for confirmations
}
```
The whole point of SWIM over naive pinging is that *k other nodes independently confirm* before a node is declared dead. At n=3 there is nobody to ask, and the library knows it and turns the feature off. Investigation 14 independently measured the same shape: `IndirectChecks: 3` [MEASURED — `config.go:312`] has at most 2 candidates at n=3.

**You would be importing a 13-year-old distributed-systems library for a feature it disables on your fleet.**

**(b) Its data model is actively hostile to what Greg asked for.** [MEASURED — `net.go:83`, `memberlist.go:458-460` and `:517-519`]
```go
MetaMaxSize = 512                                          // net.go:83
if len(meta) > MetaMaxSize {
    panic("Node meta data provided is longer than the limit")   // memberlist.go:460
}
```
Greg wants *"agent lists and maybe a summary of what the agent is doing"* synced (§3.1). Investigation 13 measured that pushing a 25-agent list (1,563 bytes) into node metadata **silently truncates and delivers corrupt JSON**, and that the only thing standing between that and a **daemon panic** is your own defensive code. Its escape hatch — 30-second TCP push/pull — is 30x too slow for a live agent list, which forces a third mechanism (a broadcast queue) that investigation 13 **did not measure** (its open question #2).

So memberlist costs **three mechanisms** (UDP gossip @190ms / TCP push-pull @30s / broadcast queue @~200ms, one of them unmeasured) to move data that fits in **one**.

**(c) It carries a live landmine on this specific network.** [MEASURED, mine, today]
```
$ ifconfig utun0 | grep mtu
utun0: ... mtu 1280
```
and `config.go:336`: `UDPBufferSize: 1400`. Investigation 14 measured the consequence directly — DF-bit probes to `dgx` showed **packets ≥1300 bytes are silently dropped** (1228B ok, 1280B ok, 1300B/1328B/1400B all zero received). The failure mode it describes is vicious: TCP push/pull still syncs at 30s so membership "mostly works", while the UDP failure detector silently dies and nodes flap forever. This is the same Tailscale-MTU gotcha already recorded against Greg's monitoring stack, in a new costume.

It is fixable (`UDPBufferSize: 1200`). The point is that it is **a bug you inherit for free** from a component whose flagship feature you can't use.

**(d) It brings a second, redundant network stack.** memberlist opens its own UDP and TCP listeners with its own wire protocol, alongside whatever the transport design lands on. Two stacks, two ports, two failure modes.

**The honest summary:** memberlist's constraints — 512 bytes, gossip, incarnation numbers, anti-entropy, UDP datagrams sized to an assumed 1500-byte MTU — are **the price of scaling to a thousand nodes**. We have three. We would pay every cost and collect no benefit.

### 1.3 The counter-argument, taken seriously

Investigation 13's core argument deserves a direct answer, not a dismissal. It says:

> *"At this scale, a library is not warranted for scale. It is warranted for **correctness on rejoin**… You would write all three, badly, and debug them at 11pm."*

It names three things a hand-rolled list gets wrong: **incarnation numbers**, **suspicion**, **anti-entropy**. Here is why each evaporates:

| Property | Why memberlist needs it | Why direct probing doesn't |
|---|---|---|
| **Incarnation numbers** | Gossip spreads *third-party rumours* ("dgx says gb-mbp is dead"). A rejoining node must out-argue rumours about itself still in flight. | A node's belief about a peer is derived **only from its own probes of that peer**. No third-party rumour is ever accepted, so there is nothing to refute. The first successful probe ends the argument because there was no argument. |
| **Suspicion** | Prevents one dropped UDP packet from declaring a node dead. | Kept — as a consecutive-failure counter. That is ~4 lines, and it is the *only* one of the three that was ever a real requirement. |
| **Anti-entropy (push/pull)** | Repairs state that gossip **dropped**. Needed because state is *accumulated from deltas*. | Every peer's row is **re-sent by its author every second**. State is never accumulated, so there is nothing to repair. Staleness is bounded by one probe interval by construction. |

The pattern: **two of the three "hard parts" are self-inflicted wounds of gossip.** Investigation 13 is right that they are hard — and right that you'd get them wrong — but you only have to get them right if you choose the architecture that creates them.

**And there is a claim in the other direction that I think is decisive: for routing, gossip is not merely unnecessary, it is *wrong*.**

BRIEF §5 wants liveness as *"a routing input"* — the sender needs to know *"can I get a message to that box"*. Gossip answers a **global** question ("is the fleet's consensus that B is up?"). Direct probing answers a **local** one ("can *I* reach B?"). When those disagree — say the `gb-mbp → dgx` path is broken but `gb-mac-mini → dgx` is fine — gossip tells `gb-mbp` that `dgx` is **alive**, because `gb-mac-mini` says so. `gb-mbp` then sends, and fails. **Gossip would actively launder the exact fact the sender needed.** Direct probing measures precisely the thing the router must know, and per-observer disagreement is the *correct answer*, not an inconsistency to be resolved.

### 1.4 The design, and the measurement that backs it

**Each daemon holds a static list of the other two boxes. Every second, it makes one small request to each. That's it.**

I built it and ran it rather than asserting it. Prototype: `/tmp/rosterproto/main.go`, 217 lines including the test harness; archived alongside this doc as `23-roster-probe-prototype.go.txt`. Three in-process nodes named `dgx` / `gb-mac-mini` / `gb-mbp`, real TCP listeners, real HTTP probes, timings scaled down 5x (200ms probe interval) so tests run fast. **[MEASURED], all of it:**

**Steady state** — all six directed edges UP:
```
    dgx          sees gb-mac-mini  UP
    dgx          sees gb-mbp       UP
    gb-mac-mini  sees dgx          UP
    gb-mac-mini  sees gb-mbp       UP
    gb-mbp       sees dgx          UP
    gb-mbp       sees gb-mac-mini  UP
```

**Failure detection** — freeze `gb-mbp` with no announcement (crash, or hard sleep):
```
    FREEZE at 00:06:03.790
00:06:04.490 [gb-mac-mini ] gb-mbp  UP -> SUSPECT (consecutive fails=3)
00:06:04.491 [dgx         ] gb-mbp  UP -> SUSPECT (consecutive fails=3)
00:06:05.091 [gb-mac-mini ] gb-mbp  SUSPECT -> DOWN (consecutive fails=6)
00:06:05.091 [dgx         ] gb-mbp  SUSPECT -> DOWN (consecutive fails=6)
```
UP→DOWN in **1.30s at a 200ms interval = 6.5 intervals**, three runs: 1.302s / 1.300s / 1.306s. **Scaled to the proposed 1s interval: ~6.5s.** Both observers agreed within **1ms**.

Compare investigation 13's memberlist measurements: **5.7–6.6s** detection, peers agreeing within **~1ms**. **The hand-rolled detector matches the library to within noise** — which is unsurprising, since at n=3 memberlist is also just pinging with a 4-second flat timeout (§1.2a).

**Rejoin — the case investigation 13 could not verify.** Its open question #1:

> *"**The true 8-hour sleep.** I measured a 75-second `SIGSTOP`… after 8 hours the peers will have **reaped** the dead entry from their node maps entirely… **I have not measured it, and I am flagging it rather than asserting it.**"*

That risk **does not exist in this design**, and the reason is structural rather than empirical:

```
$ grep -n 'delete(' main.go
  none: 'delete(' appears nowhere. The peer list is immutable after wiring.
```

There is no reaping because there is no membership state to reap. The only per-peer state is a consecutive-failure counter that resets to zero on the first success. **An 8-hour absence and a 3-second absence execute identical code.** I demonstrated it at 60 probe intervals — 10x past the DOWN threshold — [MEASURED]:

```
=== LONG freeze: 12s == 60 probe intervals == 10x past DOWN threshold ===
    FREEZE at 00:08:14.256
    -> dgx's view of gb-mbp: DOWN, UP->DOWN transition 1.305s after freeze
    dgx          sees gb-mbp UP, DOWN->UP transition 99ms after wake
    gb-mac-mini  sees gb-mbp UP, DOWN->UP transition 99ms after wake
```

Rejoin across three runs at a 200ms interval: **2ms / 201ms / 1ms / 99ms** — i.e. **bounded by exactly one probe interval**, whichever way the wake lands relative to the tick. **No `Join()` call. No re-registration. No incarnation number. No operator action.** Investigation 13's belt-and-braces recommendation (*"On wake, call `Join(seedList)` unconditionally"*, plus a 60s retry timer, plus a macOS wake hook) is **not needed** — there is nothing to rejoin, because nothing was ever left.

**An epistemic note, because the brief asks for it.** My first rejoin measurement read **1.001s** and I nearly wrote it down as "≈5 probe intervals" and went hunting for a TCP connection-reuse bug. I even built the experiment to test that hypothesis (`NO_REUSE=1`), and it changed nothing (1.2s → 1.0s). **The bug was my instrument, not the system**: I was reporting `LastSeen`, which is the *most recent* successful probe and therefore just measured my own `time.Sleep`, not the recovery. Fixing it to record the DOWN→UP transition instant gave the real answer (~1 interval). I am recording this because the prior round's characteristic failure was exactly this — a plausible number, confidently explained, never checked.

**Timeouts, grounded in this fleet's measured latency** rather than copied from memberlist's defaults. [MEASURED, today, over the tailnet]:

| Link | min | avg | max | stddev | loss |
|---|---|---|---|---|---|
| `gb-mbp → dgx` (wired) | 4.37ms | 6.35ms | **15.8ms** | 3.3ms | 0% |
| `gb-mbp → gb-mac-mini` | 7.73ms | 20.4ms | **92.0ms** | **25.5ms** | 0% |
| TCP dial `dgx:22` | — | — | 56.3ms cold, ~5ms warm | — | — |

The mac-mini link has **real jitter** — 92ms worst case, stddev 25.5ms, at zero packet loss (probably WiFi power-save; see open questions). This is why the numbers are what they are:

| Parameter | Value | Justification |
|---|---|---|
| Probe interval | **1s** | 2 probes/sec/node, 6 fleet-wide. Free. |
| Probe timeout | **1s** | **11x the worst RTT observed (92ms).** A 250ms timeout would be only 2.7x and would flap on the mini's jitter. |
| → SUSPECT | **3 consecutive failures** (~3s) | Survives a burst of loss without flapping. |
| → DOWN | **6 consecutive failures** (~6s) | Matches memberlist's measured 5.7–6.6s. Not tuned further: BRIEF §6 disclaims rigor, and ~6s is far better than a one-operator fleet needs. |
| → UP | **1 success** (~1s) | Recovery should be instant; there is no reason to be slow to believe good news. |

**Cost: O(n²) probes.** At n=3, 6/sec. At n=10, 90/sec — still nothing. It breaks around n≈50. BRIEF §6 scopes out *"scale beyond ~3 boxes"*, so this is fine — but **state the swap-out point honestly: past ~20 boxes, throw this away and put memberlist in**, at which point you will have real nodes to confirm suspicions with and the library will actually earn its keep. This is a deliberate, reversible bet on the stated scale, not a claim that gossip is bad.

**Why the alternatives lost, in one line each:**
- **memberlist** — flagship feature disabled at n=3 [MEASURED]; 512B cap that panics [MEASURED]; UDP default over this network's MTU [MEASURED]; three mechanisms where one suffices; a second network stack. Its three justifying properties are all gossip's self-inflicted problems (§1.3).
- **Serf** — memberlist plus a whole agent/CLI. Greg is *writing* the daemon. Inherits every memberlist problem and adds a process.
- **etcd / ZooKeeper / Consul** — consensus needs a quorum. At n=3 with a sleeping laptop you have 2 real voters, which tolerates **zero** failures. That is a hub whose loss is fatal — **the exact §4 violation that sank the last round**. Also drags in the CAP reasoning BRIEF §6 explicitly refuses. (Investigation 13's arithmetic here is correct and I adopt it wholesale.)
- **Tailscale's own online flag** — it is a **control-plane** view, i.e. a third-party central dependency, philosophically at odds with §4. And it is measurably not the data we need: for the three *online* fleet boxes, `LastSeen` is the zero value `0001-01-01T00:00:00Z` [MEASURED] — Tailscale only populates it for **offline** peers. It also cannot see the daemon, only the box (§5.1).

---

## 2. Not-centralized (§4): why this satisfies it, trivially

BRIEF §4: *"no single box's death may kill the system."*

Investigation 13 confirmed gossip survives seed death by experiment. **The static-list design satisfies §4 more strongly, by construction:** there is no seed, no bootstrap node, no join protocol, and therefore no node that is special even for one second. Each daemon reads the same config file and probes the other two. Kill any box — or any two — and the survivors keep probing each other and keep an accurate view of everything they can reach.

The measured steady-state table in §1.4 is a **complete mesh of 6 directed edges with no distinguished node**. There is no topology to be wrong about. This is the cheapest §4 compliance available: the property is not engineered, it is what remains when you remove the hub.

A single-box cold start also works with both peers dead: the daemon binds, probes, gets errors, marks both DOWN, and serves a correct roster to its local plugins. No quorum, no waiting. (Contrast investigation 15's measured NATS finding that a **JetStream cluster node refuses to start without routes** — `Can't start JetStream: JetStream cluster requires configured routes`.)

---

## 3. The node entry, and the kernel/plugin boundary

This is the sharpest question in my brief, so I'll argue it before writing schema.

### 3.1 The apparent contradiction

Greg says both of these:

> §3.1: *"machines have a list of other machines. Then send pings to each other to check they're online. **then they can also sync agent lists** and maybe a summary of what the agent is doing."* — the roster syncs agent lists. Roster is core.

> §3.2: *"The idea of an agent list could probably **also be a plugin**"* — the agent list is a plugin.

Both are true, and they are only in tension if you conflate **the mechanism** with **the content**. This is the identical move BRIEF §3.1 already makes for comms:

> *"What if the daemon had 'channels', a channel had a name and a type, then the daemon would do data transport, while the plugin handled all the logic."*

So apply the same rule to state:

> **The kernel replicates opaque bytes per node. The plugin owns the schema and the meaning.**

A **channel** is to the comms plugin what a **state slot** is to the agent-registry plugin. The kernel has exactly two replication primitives, both opaque, both driven by plugins:

| Primitive | Shape | Lifetime | Who owns the bytes |
|---|---|---|---|
| **Channel** (sibling design) | messages, name + type | ephemeral, in flight | the plugin |
| **State slot** (this doc) | one blob per (node, namespace) | continuously refreshed, last-writer-wins | the plugin |

### 3.2 Where exactly the line is

**The test: does the kernel need it to do its own job?**

- **Node liveness and addresses** — **yes**. The kernel cannot route a channel message to `dgx` without knowing where `dgx` is and whether it is answering. It needs this *for itself*. → **core**.
- **The agent list** — **no**. Nothing in the kernel's job requires knowing what an agent is. → **plugin**.

That single question resolves both quotes cleanly and non-arbitrarily. Greg's *"they can also sync agent lists"* is satisfied — the roster mechanism does sync them, on the same 1-second probe. §3.2's *"could probably also be a plugin"* is satisfied — the plugin defines, writes, and interprets them. **The kernel carries the bytes and has no opinion.**

**The falsifiable check on this boundary:**
```
grep -ri "agent" kernel/     ->  0 hits
```
If that ever returns a hit, domain knowledge has leaked into the kernel and the design rule has been broken. Similarly `grep -ri "ssh" kernel/` → 0 hits (§6).

### 3.3 The wire schema

Protobuf, per BRIEF §3.2 (*"we can define it COMPLETELY independently of any code"*).

```protobuf
syntax = "proto3";
package substrate.roster.v1;

import "google/protobuf/timestamp.proto";
import "google/protobuf/duration.proto";

// ---------------------------------------------------------------------------
// Identity + addressing. Written by the KERNEL from config + `tailscale status`.
// This is the entire address book. One address per box, on purpose (see §5).
// ---------------------------------------------------------------------------
message NodeIdentity {
  string name         = 1;  // roster name, from config. THE key. "dgx"
  string tailscale_id = 2;  // stable node id, "nXBLGEb3iL11CNTRL". Cross-check only (§3.5)
  string addr         = 3;  // Tailscale IPv4. "100.115.27.55". The ONE address.
  string addr6        = 4;  // Tailscale IPv6. "fd7a:115c:a1e0::8539:1b38"
  uint32 port         = 5;  // roster/daemon port
  string user         = 6;  // login user. Measured: "gb" on all three boxes.
  string os           = 7;  // "linux" | "darwin"
  string arch         = 8;  // "arm64"
  uint32 schema       = 9;  // schema version
}

// ---------------------------------------------------------------------------
// Liveness. LOCAL to each observer. NEVER transmitted. See §4.
// ---------------------------------------------------------------------------
enum Liveness {
  LIVENESS_UNSPECIFIED = 0;  // == UNKNOWN: never probed since this daemon started
  LIVENESS_UP          = 1;
  LIVENESS_SUSPECT     = 2;
  LIVENESS_DOWN        = 3;
}

enum DownReason {
  DOWN_REASON_UNSPECIFIED     = 0;
  DOWN_REASON_PROBE_TIMEOUT   = 1;  // accepted or black-holed, never answered
  DOWN_REASON_CONN_REFUSED    = 2;  // box up, daemon down  <- a genuinely different fact
  DOWN_REASON_NO_ROUTE        = 3;  // network path gone
  DOWN_REASON_ANNOUNCED_SLEEP = 4;  // peer told us, before it went  <- §4.2
  DOWN_REASON_ANNOUNCED_DRAIN = 5;  // peer is shutting down on purpose
}

message PeerView {
  string                    node                 = 1;
  Liveness                  state                = 2;
  DownReason                down_reason          = 3;
  google.protobuf.Timestamp last_seen            = 4;  // OBSERVER'S OWN CLOCK. Never a peer's.
  uint32                    consecutive_failures = 5;
  google.protobuf.Duration  last_rtt             = 6;
  string                    last_error           = 7;  // verbatim dial/transport error
}

// ---------------------------------------------------------------------------
// The opaque state slot. The kernel does not parse `payload`. Ever.
// ---------------------------------------------------------------------------
message NamespaceState {
  uint64                    version     = 1;  // monotonic per (node, namespace)
  bytes                     payload     = 2;  // OPAQUE. The plugin owns this schema.
  google.protobuf.Timestamp authored_at = 3;  // author's clock. Advisory only.
}

// ---------------------------------------------------------------------------
// The probe. This is the whole protocol.
// ---------------------------------------------------------------------------
message ProbeRequest {
  string from                        = 1;
  uint64 seq                         = 2;
  uint64 peer_boot_id                = 3;  // the boot_id I think you have
  map<string, uint64> known_versions = 4;  // namespace -> highest version I hold
}

message ProbeResponse {
  NodeIdentity identity = 1;
  string       intent   = 2;  // "up" | "sleeping" | "draining"
  uint64       boot_id  = 3;  // random per daemon start. See §3.4 -- load-bearing.
  // Only namespaces whose version exceeds the caller's known_versions.
  map<string, NamespaceState> state = 4;
}

service Roster {
  // Peer-facing: the only RPC crossing a machine boundary.
  rpc Probe(ProbeRequest) returns (ProbeResponse);

  // Plugin-facing (loopback only).
  rpc ListNodes(ListNodesRequest) returns (ListNodesResponse);
  rpc GetNode(GetNodeRequest)     returns (GetNodeResponse);
  rpc WatchNodes(WatchNodesRequest) returns (stream NodeEvent);  // §3.1 "changes in the node list"

  rpc PutLocalState(PutLocalStateRequest) returns (PutLocalStateResponse);
  rpc GetPeerState(GetPeerStateRequest)   returns (GetPeerStateResponse);
  rpc WatchState(WatchStateRequest)       returns (stream StateEvent);
}
```

**One RPC crosses the network.** Compare memberlist's three mechanisms. The version map makes it self-optimising: a steady-state probe carries ~100 bytes and returns ~200; a changed namespace rides **the very next probe**, so propagation is **≤1s** — versus memberlist's 30s push/pull, or its unmeasured broadcast-queue path. **Investigation 13's open question #2 is answered by not needing the mechanism it was about.**

### 3.4 `boot_id`: the subtle bug this exists to prevent

**[DEDUCED — a design bug I reasoned out, not one I hit.]** A naive version scheme breaks silently on restart:

1. `dgx` publishes `agents` at version 41. `gb-mbp` caches it, remembers `known_versions["agents"] = 41`.
2. `dgx`'s daemon restarts. Its version counter resets to 1.
3. `gb-mbp` probes with `known_versions["agents"] = 41`. `dgx` sees `1 <= 41` and **sends nothing**.
4. **`gb-mbp` serves stale agent data forever.**

`boot_id` — a random number regenerated at every daemon start — fixes it: when the observer sees a `boot_id` different from the one it recorded, it **discards all cached state for that node and resets its known-versions to zero**. This is ~6 lines and it is the difference between correct and quietly wrong. It also gives a *free and genuinely useful fact*: a peer whose `boot_id` changed **restarted**, which is different from "was never down" and different from "is down", and no probe-based detector can tell you otherwise (a restart between two probes is invisible without it).

### 3.5 Node names, and clash detection

The node name comes from **config**, not `os.Hostname()`. Investigation 13 measured why: `gb-mac-mini` reports its hostname as `gb-mac-mini.local`, which would silently create a second, wrong identity. [CLAIMED — investigation 13; I did not re-verify, but the tailnet table in §1.1 independently shows two devices named `localhost` and two named `gb-mac`, so hostname-derived identity is measurably unsafe on this network.]

`tailscale_id` is a **cross-check, not the key**. If the config says `dgx` but the Tailscale ID at that address doesn't match the recorded one, that is a **misconfiguration** — two boxes claiming one name, or a name pointed at the wrong box. **Refuse and log loudly. Do not resolve it.** This is the first instance of a rule applied consistently throughout this design (§7.3): *identity conflicts are reported, never silently arbitrated.*

### 3.6 The state-slot API, and the quota

```go
// The complete kernel surface for the roster. Everything a plugin gets.
type Roster interface {
    // --- identity & addressing ---
    LocalNode() NodeIdentity
    ListNodes() []NodeIdentity
    GetNode(name string) (NodeIdentity, PeerView, bool)

    // --- liveness ---
    Liveness(node string) PeerView
    WatchNodes(ctx context.Context) <-chan NodeEvent   // UP/SUSPECT/DOWN/RESTARTED

    // --- opaque replicated state. The kernel never parses payload. ---
    PutLocalState(ns string, payload []byte) (version uint64, err error)
    GetPeerState(node, ns string) (payload []byte, age time.Duration, ok bool)
    ListPeerState(ns string) map[string]StateBlob     // node -> blob, all peers
    WatchState(ctx context.Context, ns string) <-chan StateEvent
}
```

**That is the entire kernel surface for this subsystem.** Nine methods. If a plugin needs a tenth, that is a signal to re-examine the boundary.

**The quota, and the explicit contrast with memberlist.** The kernel enforces a per-namespace cap and **returns an error**:

```go
const MaxNamespaceBytes = 256 << 10  // 256 KiB per (node, namespace)

func (r *roster) PutLocalState(ns string, payload []byte) (uint64, error) {
    if len(payload) > MaxNamespaceBytes {
        return 0, &ErrStateTooLarge{NS: ns, Size: len(payload), Max: MaxNamespaceBytes}
    }
    ...
}
```

Three deliberate differences from memberlist's measured behaviour:
1. **It returns an error.** It does not `panic` (memberlist.go:460) and does not silently truncate (which investigation 13 measured delivering corrupt JSON with `err = nil`).
2. **256 KiB, not 512 bytes** — because the bandwidth arithmetic says we can. Worst case 256KiB × 2 peers × 1/s = 512 KB/s, and that only occurs if a namespace *changes every second*. Steady state is ~200 B/s because of the version map. Typical agent list: 20 agents × ~200 B = **4 KB**. **memberlist's 512-byte cap is a scaling decision for 1,000 nodes that we are not obliged to inherit.**
3. **TCP, not UDP datagrams.** The 1280-byte MTU cliff measured in §1.2c **cannot bite this design** — TCP segments transparently. The landmine is not defused; it is absent.

Bound the content too, in the *plugin's* schema, so the quota is never reached in practice: cap `summary` at 256 bytes and agent count at 256/node (BRIEF §6: *"tens of agents"*). Bounded by construction.

### 3.7 The agent list — a plugin's payload

This is **the agent-registry plugin's** schema. It appears here only to show the shape; the kernel never sees these field names.

```protobuf
// package substrate.agents.v1 -- OWNED BY THE PLUGIN.
// Serialized into NamespaceState.payload under namespace "agents".
message AgentEntry {
  string name = 1;  // "dgx/refactor-3" -- canonical, hierarchical, daemon-defined. §7
  string node = 2;  // "dgx". Redundant with the name prefix, carried for convenience.
  string kind = 3;  // "claude-code" | "codex" | "pi" | "opencode"
  int32  pid  = 4;

  enum Lifecycle {
    LIFECYCLE_UNSPECIFIED = 0;
    LIFECYCLE_RUNNING     = 1;
    LIFECYCLE_IDLE        = 2;
    LIFECYCLE_EXITED      = 3;  // record OUTLIVES the process. §7.2
  }
  Lifecycle lifecycle = 5;

  string  summary = 6;  // Greg's "what the agent is doing". Plugin caps at 256B.
  string  cwd     = 7;
  google.protobuf.Timestamp started_at = 8;
  google.protobuf.Timestamp exited_at  = 9;
}

message AgentList { repeated AgentEntry agents = 1; }
```

The plugin's whole job on the write side is:
```go
list := plugin.currentAgents()
blob, _ := proto.Marshal(list)
roster.PutLocalState("agents", blob)   // replicated to every peer within 1s
```
and on the read side, `roster.ListPeerState("agents")` returns every box's blob, each with an `age`. **The plugin gets fleet-wide agent replication for two function calls, and the kernel learned nothing about agents.**

---

## 4. Liveness states

### 4.1 The set: UP / SUSPECT / DOWN / UNKNOWN — and I reject two of the five offered

My brief offered `up / unreachable / asleep / stale / unknown` and said **pick**, with the binding rule: *"Each must be mechanically detectable — no state that requires an agent or a human to remember to declare something."* Applying that rule strictly is what decides this.

| State | Mechanical definition — a pure function of local probe history |
|---|---|
| **UNKNOWN** | No probe has completed since this daemon started. The honest state at boot. |
| **UP** | Last probe succeeded. |
| **SUSPECT** | ≥3 consecutive failures, <6. *"Not answering, but I'm not ready to route around it."* |
| **DOWN** | ≥6 consecutive failures. *"Do not send here."* |

Every one is a pure function of a counter. No declaration, no agent, no human, no clock comparison against another machine.

**`asleep` is rejected as a state.** It is not mechanically detectable, and investigation 13 proved the point precisely: *"A frozen laptop and a dead laptop are byte-for-byte identical from the outside. **No membership library can distinguish them, because the distinction does not exist on the wire** — it exists only in *intent*."* That is exactly right. A state that depends on a hook having fired is a state that is **silently wrong** whenever the hook didn't fire — SIGKILL, power loss, kernel panic, a dead battery.

**So intent becomes a field, not a state:** `DOWN(reason=ANNOUNCED_SLEEP)` vs `DOWN(reason=PROBE_TIMEOUT)`. The state machine stays mechanical and always correct; the announcement is a strictly-optional **refinement** that improves the message when present and breaks nothing when absent. **Measured working:**

```
=== TEST 3: gb-mbp ANNOUNCES sleep, then freezes ===
    before freeze: dgx sees gb-mbp UP   intent="sleeping"
    after  freeze: dgx sees gb-mbp DOWN intent="sleeping"   <- DOWN(reason=announced_sleep)
```

The announcement rides the ordinary probe response (`intent` field), so it needs no new mechanism and arrives within 1s. This also sidesteps a **measured trap** investigation 13 found in memberlist: its `NotifyLeave` callback reports `State=0` (Alive) even for a graceful leave, so *"you cannot read `n.State` in the callback to tell 'left' from 'dead'"*. Here, left-vs-dead is an explicit enum field that cannot be confused with anything.

**`stale` is rejected as a state — it's a property of *data*, not of a *node*.** Every state blob carries an `age` (§3.6 `GetPeerState` returns it). Staleness is a continuous quantity the *reader* judges against its own needs: the SSH plugin may happily use a 60s-old connectivity matrix while a router wants sub-second liveness. Promoting it to a node state would force one threshold on every consumer. **Return the age; let the caller decide.** Note the ages have a hard bound by construction: a peer that is UP has state ≤1s old, because it re-authors every probe.

### 4.2 Sleep and wake

- **Sleep:** a macOS sleep hook sets `intent = "sleeping"`. Peers learn on their next probe (≤1s), then see DOWN ~6s later with `reason=ANNOUNCED_SLEEP`. **If the hook doesn't fire, you get `DOWN(reason=PROBE_TIMEOUT)`** — less informative, equally correct. The design degrades, it does not break.
- **Wake:** **nothing happens.** The daemon answers the next probe. Peers mark it UP within one interval. [MEASURED: 1–201ms at a 200ms interval; ~1s at the proposed interval.] **No wake hook is required** — investigation 13 needed one (`Join(seeds)`) only because memberlist reaps. Wire one anyway if convenient, but it is an optimisation, not a correctness requirement.
- **Rejoin after 8 hours:** identical to rejoin after 3 seconds. See §1.4 — no reaping path exists, demonstrated at 60 intervals.

---

## 5. Liveness as a routing input, and the ACK gap

### 5.1 What the roster owns, and what it must not

BRIEF §5's flaw, in Greg's words:

> *"we actaully should probably notify the sender that the receiver is not listening. I believe we can find that out."*

**He's right that you can find it out.** But "the receiver is not listening" is three different questions and conflating them is how you get a bad ACK system:

| Question | Owner | Answer time |
|---|---|---|
| 1. Is the receiver's **box** reachable from here? | **the roster (core)** | **microseconds** — local map lookup, bounded ≤6s stale |
| 2. Is the receiving **daemon** up? | **the roster (core)** | microseconds — `DOWN_REASON_CONN_REFUSED` distinguishes it from a dead box |
| 3. Is the target **agent subscribed**? | **the comms plugin** | see §5.3 |
| 4. Did this specific **message** arrive? | **the comms plugin** (BRIEF §4: *"durability is a PLUGIN decision"*) | not the roster's business |

**The roster answers 1 and 2 and stops.** It hands the plugin the half the plugin cannot get for itself. Designing message ACKs here would be putting domain knowledge in the kernel — the exact failure the design rules warn about.

### 5.2 What the sender learns, and how fast

Because liveness is local state, **the send path never touches the network to find out**:

```go
// Kernel-side. A local map lookup. Microseconds. No timeout. No hang. No network.
func (k *kernel) SendTo(node string, ch string, msg []byte) error {
    v := k.roster.Liveness(node)
    switch v.State {
    case LIVENESS_UNSPECIFIED:
        return &ErrNodeUnknown{Node: node}
    case LIVENESS_DOWN:
        return &ErrNodeDown{                 // <- the §5 notification, delivered instantly
            Node:     node,
            Reason:   v.DownReason,          // ANNOUNCED_SLEEP | PROBE_TIMEOUT | CONN_REFUSED | ...
            LastSeen: v.LastSeen,
            Failures: v.ConsecutiveFailures,
        }
    case LIVENESS_SUSPECT:
        // Deliberately NOT an error. Try it -- it's probably one dropped packet.
        // The plugin may inspect v.State and choose to queue instead. Its call.
    }
    return k.transport.Send(node, ch, msg)
}
```

**The contrast with today is the whole payoff.** Investigation 13 measured the current world: an agent reaching a sleeping laptop over SSH waits for a TCP timeout — tens of seconds — and gets `Operation timed out`, which is **indistinguishable from a typo, a firewall, a wrong username, or a broken key**. That ambiguity *is* Greg's *"screw around figuring out the network crap"*.

With the roster it gets, immediately: `ErrNodeDown{Node: "gb-mbp", Reason: ANNOUNCED_SLEEP, LastSeen: 22:04:11}`. **A message a plugin can act on** — queue it, pick another agent, tell the human — rather than a timeout it can only log.

Worst-case window in which a sender believes a dead box is alive: **~6s** [MEASURED, §1.4].

### 5.3 The roster's real contribution to the ACK problem

Two of them, and they fall out of the design rather than being bolted on.

**(a) Subscription state rides the same 1-second replication.** The comms plugin publishes *its own* namespace — say `comms.subs` — listing which agent names are currently subscribed on its box. That blob reaches every peer within 1s via the ordinary probe. So the sender's check *"is `dgx/refactor-3` actually listening?"* becomes a **local lookup, in microseconds, before the message is ever sent**:

```go
blob, age, ok := roster.GetPeerState("dgx", "comms.subs")   // local. ~1s old, max.
```

**This is the direct answer to Greg's "I believe we can find that out": yes — and better than he framed it. You can find it out *before you send*, locally, without a round trip.** It converts the two cases he actually hits — box asleep, agent exited — from a timeout into an instant typed error. It does **not** replace end-to-end ACKs (a message can still be lost after a green pre-check); it removes the *common* failure from the ACK path's shoulders.

**(b) DOWN is an event, so in-flight sends fail fast instead of timing out.** `WatchNodes()` streams transitions. The comms plugin subscribes and, on `DOWN(dgx)`, fails every in-flight send to `dgx` **immediately (~6s after the box died)** rather than waiting on a TCP timeout — and on `UP(dgx)` ~1s after it returns, retries them. **The roster is the trigger for the plugin's durability and retry logic**, which is precisely the §4 division of labour: the kernel supplies the fact, the plugin decides the policy.

**(c) Restart detection, free.** `boot_id` change (§3.4) means the peer daemon restarted — so a plugin holding in-flight state for that peer knows to discard it, even if the restart was fast enough that no probe ever failed. No probe-based detector can supply this otherwise.

---

## 6. The address book

### 6.1 Does Tailscale already solve this? Mostly — so the design is thin

My brief said: *"If Tailscale already solves most of this, the right design is thin, and you should say so."* **It does, and it is.**

Investigation 13's measurement is unambiguous, and the split is clean:

**Tailscale solves the hard half:** every box reaches every box, encrypted, through NAT, from anywhere — the actual distributed-systems problem, done. Plus stable node IDs and stable IPs that don't move (unlike DHCP — investigation 13 measured `dgx` anchored in `gb-mbp`'s `known_hosts` at `192.168.1.86` while the router now hands out `192.168.1.155`; a lease waiting to bite).

**Tailscale is failing the easy half:** name→address is broken on both Macs, and trust anchoring is per-box and inconsistent. Investigation 13 measured 1-of-9 addressing forms working from `gb-mbp` and **0-of-6 from `dgx`**.

**The design's response is to not participate in the broken half at all:**

> **The kernel stores exactly one address per box — the Tailscale IP — and never resolves a name.**

Deliberately **not** stored: LAN IPs, `.local` mDNS names, MagicDNS names, `known_hosts` name lists, `~/.ssh/config` aliases. Investigation 13 measured that `dgx` answers to **six** address forms, three of them stale or wrong, anchored inconsistently on each box. **Those six forms are the disease.** The roster carries the one form measured stable and reachable from every box, and the daemon **dials an IP it was handed by a peer** — no DNS, no `known_hosts`, no resolution step.

**Not one of investigation 13's nine measured failure modes is on that path**, because the path deletes the step where an agent has to *find out*. That is the whole of *"agents dont have to figure that out"*.

The kernel populates `NodeIdentity` from `tailscale status --json` at startup and on change — measured to supply hostname, both IPs, OS, and stable ID for every box, and measured correct on all three boxes **even where DNS is broken**. **Tailscale is the network and the address *source*. The roster is the *directory*.** They don't compete: Tailscale has no concept of an agent named `dgx/refactor-3`, and that mapping is the thing Greg actually asked for.

One measured caveat on using Tailscale's feed for liveness (which we do **not** do, §1.2): for the three *online* fleet boxes, `LastSeen` is `0001-01-01T00:00:00Z` — the zero value. **Tailscale only populates `LastSeen` for offline peers.** Anyone reaching for it as a freshness signal gets a garbage timestamp.

### 6.2 The lookup an agent performs

Three surfaces over one protobuf service. Per BRIEF §3.2 (*"the same interfaces could be available through REST"*) and investigation 11's measurement that **ConnectRPC serves the same endpoint as both JSON/HTTP-1.1 and gRPC** [CLAIMED — investigation 11 §4; measured by that agent, not by me], this costs nothing extra:

**1. CLI — for agents and humans, identical on all three boxes:**
```bash
$ substrate node ls
NAME          STATE   ADDR             OS      ARCH   LAST-SEEN  RTT
dgx           UP      100.115.27.55    linux   arm64  0.4s       5.8ms
gb-mac-mini   UP      100.120.22.74    darwin  arm64  0.2s       20.4ms
gb-mbp        DOWN    100.87.151.114   darwin  arm64  8h12m      -        (announced_sleep)

$ substrate node get dgx -o json
{"name":"dgx","addr":"100.115.27.55","user":"gb","os":"linux","arch":"arm64",
 "state":"UP","last_seen":"2026-07-16T00:31:02Z","rtt_ms":5.8}

$ substrate node addr dgx        # the one thing a script actually wants
100.115.27.55
```

**2. REST — free from ConnectRPC, so `curl` is the debugger:**
```bash
$ curl -sX POST --http1.1 -H 'Content-Type: application/json' \
    http://127.0.0.1:7947/substrate.roster.v1.Roster/GetNode -d '{"name":"dgx"}'
```

**3. Proto — for plugins**, the `Roster` service in §3.3.

**No generated `~/.ssh/config` from the kernel.** That is the SSH plugin's business (§6.3), and it is exactly the domain knowledge the kernel must not have.

### 6.3 Access control on the probe port

**Bind the listener to the Tailscale IP only — never `0.0.0.0` — and check the caller's source IP against the config allow-list.** §1.1 measured why: the tailnet has 8 devices, 5 of them not fleet, one an online iPhone named `localhost`.

**[DEDUCED — and I want to flag the reasoning, because it's the kind of claim that deserves scepticism.]** On a WireGuard network, a source IP is not a hint — Tailscale IPs are cryptographically bound to node keys, so a packet arriving on `utun0` from `100.115.27.55` was signed by dgx's private key. **A source-IP check on a tailnet is therefore an authenticated identity check**, and it is stronger than the shared secret investigation 13 recommended (memberlist's `SecretKey`), while being one config line instead of a key to rotate. What it does **not** defend against: a compromised fleet box, or a compromised `tailscaled`. BRIEF §4 scopes both out (*"assume a trusted network"*). **I have not verified the WireGuard source-IP binding property myself** — see open questions.

---

## 7. Agent identity

### 7.1 The naming scheme: hierarchical, and the clash problem disappears

BRIEF §4: *"probably has a 'daemon defined name', thats like the key/domain name, and then there would be other data (box name, IP) that would also be associated with the entry."*

**Take "like the key/domain name" literally — domain names are hierarchical, and that is not decoration.**

> **Canonical agent name: `<node>/<local>` — e.g. `dgx/refactor-3`.**
> Assigned by the daemon (agent-registry plugin) at registration. Node-scoped by construction.

Investigation 14 flagged this as Erlang's `global` registered-name model and identified its two problems, which the brief asked me to steal from carefully:

> *"What resolves a name clash? `global` needs a resolver function and 'If the function crashes, or returns anything other than one of the pids, the name is unregistered.' Two agents claiming one name needs a deterministic tiebreak. Undesigned."*

**Steal the model, delete the problem.** Erlang's `global` needs a resolver, a lock, and a distributed agreement protocol **because its namespace is flat**. Make names hierarchical and:

- **Clashes are impossible.** `dgx` alone assigns names under `dgx/`. Uniqueness is a *local* invariant — a mutex on one box. No resolver, no lock, no consensus, no tiebreak. **The clash problem is self-inflicted by flat namespaces.**
- **The name contains the route.** `dgx/refactor-3` → node `dgx` → roster → `100.115.27.55`. **You don't even need the agent registry to route** — the routing key is the prefix. This is exactly why DNS is hierarchical, and it is Greg's own instinct followed to its conclusion.

**Answering investigation 13's objection to box-in-the-name.** It argued the key should not be `box:pid` because *"it forces every sender to know where the target lives — which is the coupling the roster exists to remove."* That objection is correct about `box:pid` and does not transfer:
- `box:pid` breaks on restart because **the pid changes**. `dgx/refactor-3` has a stable local part chosen by the daemon.
- **Agents do not migrate.** An agent is a process on a box; it cannot move. Encoding the box costs nothing real, and buys route-from-name.
- Box and IP remain "associated data" on the entry (§3.7 `AgentEntry.node`), exactly as Greg described — same as DNS, where `www.google.com` is *both* hierarchical *and* has an A record.

### 7.2 Identity vs liveness — the same split, applied twice

Investigation 14's other open question:

> *"How does a plugin/agent get a durable name across restarts? Erlang's `global` unregisters on death BY DESIGN, but Greg wants 'a daemon defined name' as a stable key/domain name that outlives the process that registered it."*

**Answer: separate the record from the process — the identical move made for nodes (a static list + a liveness overlay).** The name is a **record** in the registry with a `lifecycle` field (§3.7). The process exits; the record persists with `LIFECYCLE_EXITED` and `exited_at`. Nothing unregisters anything.

This consistency is a good sign the shape is right: **at both levels, identity is a durable record and liveness is a mechanically-derived overlay on top of it.** Erlang's `global` conflates them, which is why it unregisters on death — and it's why it doesn't fit Greg's stated requirement.

### 7.3 The resolution path, split exactly at the kernel/plugin line

```
"dgx/refactor-3"  --[PLUGIN: agent registry, reads its own replicated state]-->  node "dgx"
       "dgx"      --[KERNEL: roster]-->  100.115.27.55  +  UP/DOWN + last_seen
                  --[dial]-->
```

**The split is the boundary from §3.2.** The plugin does name→node (it knows what an agent is). The kernel does node→address→reachable (it knows what a box is). Neither knows the other's domain. If the *kernel* did the agent lookup, the kernel would know what an agent is, and the boundary would be broken.

**Aliases: report ambiguity, never guess.** The plugin may accept the unqualified `refactor-3` as convenience, searching every node's `agents` blob. If two boxes both have one:

```go
// The plugin's rule. Deterministic, and it does not judge.
func (p *registry) Resolve(name string) (AgentEntry, error) {
    if strings.Contains(name, "/") { return p.exact(name) }   // canonical: always unambiguous
    m := p.searchAllNodes(name)
    switch len(m) {
    case 0: return AgentEntry{}, &ErrNoSuchAgent{Name: name}
    case 1: return m[0], nil
    default:
        return AgentEntry{}, &ErrAmbiguousName{Name: name, Candidates: m}  // NEVER pick
    }
}
```

**Never silently pick a winner.** This is the same rule as node-name clash detection (§3.5), and it is downstream of BRIEF §2: *"The system's job is to carry the message. It never judges."* A daemon that guesses which `refactor-3` you meant is judging, and it will be wrong at 2am.

---

## 8. The SSH-connectivity plugin

BRIEF §4 is explicit: **SSH key sync is a plugin, not core**, but the fleet must *programmatically verify every box can connect* so *"agents dont have to figure that out"*. Investigation 13 measured `dgx` at **0-for-6** — the box you most want to run agents on cannot initiate a connection to anything, because it has **no private key at all** and an **empty `known_hosts`**. So this plugin has real work.

My brief: *"Do not design anything that silently distributes credentials — state clearly what requires a human decision."* Taking that seriously changes the shape of the answer.

### 8.1 Phase 0 — the thinnest fix, and it's a human decision: turn on Tailscale SSH on `dgx`

Investigation 13 measured that **Tailscale SSH is already on for `gb-mbp` and `gb-mac-mini` and OFF for `dgx`** (`RunSSH: false`), and that it authenticates by tailnet identity, needing **no `known_hosts` and no `authorized_keys`**. It measured the proof: `gb-mbp` has **no `authorized_keys` file at all**, yet `ssh 100.87.151.114` from `gb-mac-mini` **succeeds**.

> **`tailscale set --ssh` on `dgx`, plus the matching ACL grant in the admin console, deletes most of the key-distribution problem outright.** No key sync, no `authorized_keys` edit, no host-key trust — for the majority of traffic.

This is the *thin* answer my brief invited, and it is a **human decision by construction**: the ACL lives in Tailscale's admin console, where a person must grant it. The plugin's job is to *notice and report* that dgx has it off — not to change it.

**The honest caveat [DEDUCED]:** Tailscale SSH moves session *authorization* to Tailscale's control plane, a central third party. Two reasons that's acceptable and one reason to keep a fallback: (a) the fleet already depends on Tailscale for reachability, so this is not a *new class* of dependency; (b) §4's not-centralized rule is about **no single box's death killing the system**, and Tailscale is not one of the boxes. But (c) I **could not verify** whether *new* Tailscale-SSH authorizations survive a control-plane outage (the WireGuard data plane does). **So keep plain SSH working as a fallback** — which is what Phases 1–3 are for.

### 8.2 Phase 1 — Verify. Read-only, zero credentials, high value

Every N minutes each box runs, against every peer's **Tailscale IP from the roster**:
```bash
ssh -o BatchMode=yes -o ConnectTimeout=4 -o StrictHostKeyChecking=yes gb@<peer-ip> true
```
`BatchMode=yes` is the honest test: it disables prompts, which is **the mode an agent runs in**. A human gets asked "unknown host, continue?"; an agent gets a hard failure.

Results go into the plugin's own state namespace — the same mechanism the agent list uses (§3.6):
```go
sshPlugin.publish()  ->  roster.PutLocalState("ssh.reach", blob)
```
```protobuf
// package substrate.ssh.v1 -- OWNED BY THE PLUGIN. Kernel never parses this.
enum Reach {
  REACH_UNSPECIFIED = 0;
  REACH_OK               = 1;
  REACH_HOST_KEY_UNKNOWN = 2;
  REACH_HOST_KEY_MISMATCH= 3;   // loud: possible MITM, or a rebuilt box
  REACH_NO_KEY           = 4;   // dgx today, x2
  REACH_AUTH_DENIED      = 5;
  REACH_UNREACHABLE      = 6;
  REACH_TIMEOUT          = 7;
}
message ReachEntry { string to = 1; Reach reach = 2; string detail = 3;
                     google.protobuf.Timestamp checked_at = 4; }
message ReachMatrix { string from = 1; repeated ReachEntry entries = 2; }
```

Because every box's row replicates within 1s, **every box holds the full 3×3 connectivity matrix locally**. Any agent asks *"can I get from here to there?"* and gets an **instant, local, honest answer** instead of discovering it via a 30-second timeout:

```bash
$ substrate ssh matrix
FROM \ TO      dgx              gb-mac-mini      gb-mbp
dgx            -                no_key           no_key
gb-mac-mini    ok               -                ok
gb-mbp         ok               host_key_unknown -
```

**This table is the deliverable.** Investigation 13 built it by hand today with nothing but read-only commands; the plugin builds it continuously. **It would have surfaced `dgx`'s 0-for-6 the day it started** rather than leaving it to be discovered during a research project.

### 8.3 Phase 2 — Distribute host *public* keys. Safe, and it fixes real failures

**Host public keys are not secrets.** They are readable by anyone with `ssh-keyscan` from an unprivileged shell. Distributing them is not credential distribution, and I want to be precise about that rather than hiding behind the word "key".

- Each daemon publishes its own SSH **host public key** in its `ssh.reach` namespace (~50 bytes).
- The plugin writes a **managed, clearly-marked, separate** file — **never** the human's `known_hosts`:

```
# ~/.ssh/known_hosts.roster
# BEGIN managed by roster ssh plugin -- do not edit, regenerated every 60s
100.115.27.55 ssh-ed25519 AAAAC3Nza...
dgx           ssh-ed25519 AAAAC3Nza...
dgx.tailf4fa3f.ts.net ssh-ed25519 AAAAC3Nza...
# END managed by roster ssh plugin
```
referenced from a generated `~/.ssh/config.d/roster.conf`:
```
Host dgx dgx.tailf4fa3f.ts.net 100.115.27.55
  HostName 100.115.27.55
  User gb
  UserKnownHostsFile ~/.ssh/known_hosts.roster ~/.ssh/known_hosts
```
**The human's file is listed second and still wins for anything it already knows.** This anchors each box under *every* name it answers to, killing the measured inconsistency where `gb-mbp` knows `dgx` only as an IP and `gb-mac-mini` knows it only as a LAN name — and it makes plain `ssh dgx` work identically from every box, which is high leverage: it fixes **the tool agents already reach for**, rather than requiring them to learn a new one.

**Trust-on-first-gossip, stated plainly [DEDUCED].** A box that joins the tailnet can assert a host key for its own name. Two mitigations and one honest limit:
- The source-IP allow-list (§6.3) means only the 3 configured boxes are heard at all.
- **Cross-check:** before writing an entry, `ssh-keyscan` the peer directly and require it to match what the roster reports. **Be honest about what this buys:** it catches *misconfiguration* (a box publishing a stale key after an OS reinstall), **not** an attacker — an attacker positioned to forge the gossip is positioned to forge the keyscan too.
- On a 3-box network of machines Greg owns, which BRIEF §4 explicitly scopes as trusted, this is a sound trade. **On an untrusted network it would not be, and this is the first thing to revisit if §4's scope ever changes.**

### 8.4 Phase 3 — Propose `authorized_keys` changes. Never apply them

**The bright lines, stated as absolutes:**

1. **Never move a private key between boxes.** Not over the roster, not over a channel, not ever. A private key is generated on the box it belongs to and dies there. `gb-mbp`'s `id_ed25519` stays on `gb-mbp`. Not a performance trade-off — not negotiable.
2. **Never write to `authorized_keys` without an explicit human command.** Appending a key grants shell access. That is a decision a person makes.
3. **Never auto-accept host keys** (`StrictHostKeyChecking=no`, or blind `accept-new`). That converts SSH's one real defence into decoration. Note this is **already happening on the fleet** [investigation 13, MEASURED]: `gb-mac-mini`'s `~/.ssh/config.d/sd-vm1` and `sd-t1` carry `StrictHostKeyChecking no` + `UserKnownHostsFile /dev/null`. Fine for throwaway VMs; **not a pattern to spread across the fleet**.

When the plugin sees `no_key` for `dgx → gb-mac-mini`, it **does not fix it**. It does all the discovery and hands the human the decision:

```
ROSTER: dgx cannot ssh to gb-mac-mini (no_key)  [seen continuously since 2026-07-15]
  Cause: dgx has no SSH private key at all (~/.ssh/id_* missing).
  Note:  dgx also has Tailscale SSH disabled (RunSSH: false). Enabling it would
         fix this and 5 other cells WITHOUT any key management:
             ON dgx:  tailscale set --ssh      (+ ACL grant in the admin console)

  Or, to use plain SSH keys instead:
     1. ON dgx:   ssh-keygen -t ed25519 -C "gb@dgx"
     2. THEN:     substrate ssh approve dgx --to gb-mac-mini,gb-mbp
                  ^ appends dgx's PUBLIC key to authorized_keys on those boxes.
                    This grants shell access. Requires you to run it.
```

**`approve` is a human typing a command.** The daemon does the tedious part — knowing *who* can't reach *whom*, *why*, and *what would fix it*. The human keeps the one decision that grants shell access. **This is the line where "helpful" becomes "wrote itself a backdoor across three machines."** I hold it firmly, and I'd rather the plugin be annoying than clever here.

### 8.5 What core exposes for this — the boundary check

The SSH plugin needs, from the kernel:
1. **Roster reads + change events** — `ListNodes`, `GetNode`, `WatchNodes`. (BRIEF §3.1 anticipated this: *"Maybe it needed changes in the node list."*)
2. **One state namespace** — `PutLocalState("ssh.reach", ...)` / `ListPeerState("ssh.reach")`.
3. **Nothing else.**

**The kernel needs zero SSH knowledge**: no key handling, no `known_hosts` awareness, no shelling out. `grep -ri ssh kernel/` → **0 hits**. And note it needs **the same two things the agent-registry plugin needs** — the API in §3.6, unchanged, no additions. **Two plugins with completely different domains, and neither required the kernel to grow.** That is the strongest available evidence that the boundary is drawn in the right place.

---

## 9. What we do NOT build

- **No gossip. No SWIM. No incarnation numbers. No anti-entropy. No suspicion confirmation.** All of it is machinery for problems that only exist if you choose gossip (§1.3), and memberlist disables its own version at n=3 (§1.2a).
- **No consensus, Raft, quorum, or leader election.** BRIEF §6 refuses it; at n=3 it manufactures the fatal hub §4 forbids.
- **No `asleep` liveness state.** Not mechanically detectable. It's `DOWN(reason=ANNOUNCED_SLEEP)` (§4.1).
- **No `stale` liveness state.** Staleness is an `age` on data, judged by the reader (§4.1).
- **No wake hook as a correctness requirement.** Nothing is reaped, so nothing must rejoin (§4.2).
- **No relay routing.** If `gb-mbp` can't reach `dgx` but `gb-mac-mini` can, we do **not** silently route through the mini. That's a mesh router, unrequested, and it would hide a fact the sender needs. Report the disagreement; it is the correct answer (§1.3).
- **No agent knowledge in the kernel.** `grep -ri agent kernel/` → 0.
- **No SSH knowledge in the kernel.** `grep -ri ssh kernel/` → 0.
- **No name-clash resolver.** Hierarchical names make node-scoped clashes impossible; alias ambiguity is **reported**, never arbitrated (§7.3).
- **No credential distribution.** Private keys never move. `authorized_keys` edits require a human command (§8.4).
- **No DNS resolution, no `known_hosts` parsing, no `~/.ssh/config` reading in the kernel.** Measured to be the disease (§6.1).
- **No auto-discovery of tailnet peers.** Explicit allow-list. The tailnet has two live devices named `localhost` (§1.1).
- **No dashboard, no status UI.** *"Liveness is a routing input, not a status display"* (BRIEF §5). The CLI in §6.2 is a debugging tool, not a product.
- **No message ACKs, no retry, no queueing, no durability.** BRIEF §4: durability is a plugin decision. The roster supplies facts and events; the comms plugin sets policy (§5.1).
- **No clock synchronisation, and no cross-machine timestamp comparison.** `last_seen` is the observer's own clock and is never transmitted (§3.3).

---

## 10. Open questions

1. **A real macOS sleep, with the network interface torn down.** My prototype froze an HTTP handler on loopback; investigation 13 used `SIGSTOP`. **Neither is a real sleep**, which also takes `utun0` and the Tailscale IP away and leaves half-open TCP connections on peers. My design is *more* robust to this than memberlist's (no reaping, and a fresh probe per interval), and I explicitly tested and rejected a connection-reuse hypothesis (§1.4) — but **I did not verify it against a real overnight sleep on real hardware.** This is the most important untested thing in this document. Investigations 11 and 15 flag the same gap for their transports; **one overnight test would serve all three.**
2. **Does the macOS sleep hook actually fire reliably before sleep?** The `intent=sleeping` refinement (§4.2) depends on it. The design degrades correctly if it doesn't (you get `PROBE_TIMEOUT`), so this is a nice-to-have, not a risk — but the ~0.6s-vs-6s difference investigation 13 measured for the graceful path is worth having.
3. **Why is `gb-mac-mini`'s RTT so jittery?** [MEASURED: min 7.7ms, max 92ms, stddev 25.5ms, **0% loss**, over 10 pings.] Zero loss with 12x spread smells like WiFi power-save. It doesn't threaten a 1s timeout (11x headroom), but if the mini is on WiFi and could be on Ethernet, that's free reliability. Worth 5 minutes.
4. **Is my WireGuard source-IP claim right?** §6.3 rests on [DEDUCED] reasoning that Tailscale IPs are cryptographically bound to node keys, making a source-IP check an authenticated identity check. **I did not verify this**, and it is the load-bearing assumption behind having no shared secret. If it's wrong, add a `SecretKey`-style shared secret — cheap, and investigation 13 already recommended one.
5. **Does Tailscale SSH still authorize *new* sessions during a control-plane outage?** §8.1's recommendation leans on it. The WireGuard data plane survives; session authorization may not. Determines how much the Phase 2/3 fallback matters.
6. **The transport decision is unresolved and it is not mine.** Investigation 10 recommends mangos (NNG); investigation 11 recommends embedded core NATS full-mesh + ConnectRPC. **They conflict.** This design is deliberately transport-agnostic — it needs only "send a small request to a known IP, get a reply or an error" — and I recommend the roster keep **its own dedicated listener regardless of how that fight resolves**, for a reason worth stating: *a failure detector that shares fate with the thing it monitors is not a failure detector.* If the channel transport wedges, the roster must still work and still be able to tell you the transport is wedged. That is ~100 lines of `net/http`, not a second stack.
7. **MagicDNS is broken on both Macs** [investigation 13, MEASURED: `/etc/resolver/search.tailscale` sets a search domain with **no nameserver**; `tailscale dns status` says "no resolvers configured"]. **My design does not care** — it never resolves a name. But it's 5 minutes in the admin console ("Override Local DNS") and would fix 6 of 9 rows in investigation 13's matrix for every *other* tool on these boxes, including `tailscale ssh` itself. Worth doing on its own merits; **not a dependency of this design.**
8. **Quota sizing.** I picked 256 KiB/namespace from bandwidth arithmetic (§3.6), not measurement. The arithmetic has ~1000x headroom so I'm not worried, but the *right* number depends on how chatty the agent-registry plugin turns out to be.
9. **`boot_id` is [DEDUCED], not measured.** I reasoned out the stale-cache-on-restart bug (§3.4) and designed against it; I did not build the broken version to watch it fail. The reasoning is simple enough that I'm confident, but it is not an experiment.
10. **Three offline `gb-mac`-ish boxes and an iPhone ShellFish key** sit in the tailnet and in `gb-mac-mini`'s `authorized_keys`. The allow-list handles them for the roster (§1.1). Whether they should be *fleet* members is Greg's call, not a design question — but the SSH plugin will report on them and someone should decide.

### 10.1 Reconciliation with `design/20-kernel-proto` (appeared while I was writing)

The sibling kernel proto (`20-kernel-proto/fleet/kernel/v1/kernel.proto`) contains a roster section written independently of this doc. **The convergence is strong and worth recording as corroboration** — it independently reached:

- `Node.name` as the key, explicitly *"Not os.Hostname()"* (my §3.5)
- `address` = *"the Tailscale IP. The ONE address. Never DNS, never LAN."* (my §6.1, verbatim in spirit)
- *"Liveness is computed LOCALLY from this box's own observations and is never gossiped."* (my §3.3, §4)
- `Intent` enum `UP | SLEEPING | DRAINING` (my §3.3)
- *"An agent list is a PLUGIN"* and the kernel *"does not know what runs on a box"* (my §3.2)
- Name claims returning **every** claimant rather than arbitrating (`kernel.proto:120`) — the same never-judge rule as my §7.3

**Three things must be reconciled before either proto is built. None is deep, but they are real:**

1. **`STATE_LEFT` — a substantive disagreement, and I'll argue my side.** Their `Liveness.State` has `STATE_DEAD` ("died without warning") **and** `STATE_LEFT` ("announced its departure") as sibling *states*. **I reject that shape (§4.1):** `LEFT` is not mechanically detectable — it exists only if a hook fired. When the hook doesn't fire (SIGKILL, power loss, dead battery) the box is `DEAD` while *actually* having gone to sleep, so the state is **silently wrong exactly when you most want it**. My design makes it `DOWN(reason=ANNOUNCED_SLEEP)`: the state stays a pure function of probe history and always correct; the announcement is an optional refinement that improves the message when present and breaks nothing when absent. Supporting evidence that this distinction bites in practice: investigation 13 **measured** memberlist's `NotifyLeave` callback reporting `State=0` (Alive) for a graceful leave, concluding *"you cannot read `n.State` in the callback to tell 'left' from 'dead'"*. Their `RosterWatchResponse.Kind` has the same `KIND_LEAVE` / `KIND_DEAD` split and inherits the same hazard. **Recommendation: collapse to my four states plus a `DownReason` field.**
2. **The mechanism for replicated plugin state.** They describe the agent list as *"a PLUGIN built on a LOOKUP channel"*, and `kernel.proto:29` describes *"Replicated map. Single-writer-per-key BY CONSTRUCTION: the writing node…"* — which **is** my state slot (§3.6) under a different name. These are almost certainly the same primitive and should be unified rather than shipped twice. My `PutLocalState`/`GetPeerState`/`WatchState` may simply be the roster's view of their LOOKUP channel type. **Someone must decide which name wins; they must not both exist.**
3. **Naming.** Package `fleet.kernel.v1` vs my `substrate.roster.v1`; their `STATE_ALIVE` vs my `LIVENESS_UP`. Cosmetic, but it must be settled once — and I have no attachment to my names.

---

## Sources

**Files read:**
- `/Users/gb/research/2026-07-15-agent-substrate-v2/BRIEF.md` (full)
- `/Users/gb/research/2026-07-15-agent-substrate-v2/investigate/13-roster-and-addressbook.md` (full — the primary input)
- `/Users/gb/research/2026-07-15-agent-substrate-v2/investigate/11-transport-options.md` (headings, §0–§2, §5, §7 — for the transport conflict and ConnectRPC claims)
- `/Users/gb/research/2026-07-15-agent-substrate-v2/investigate/10-zeromq-experiments/mangos_all_patterns.go` (the SURVEY roll-call experiment, lines 111–143)
- `~/go/pkg/mod/github.com/hashicorp/memberlist@v0.6.0/net.go` — line 83, `MetaMaxSize = 512`
- `~/go/pkg/mod/github.com/hashicorp/memberlist@v0.6.0/memberlist.go` — lines 457–460, 516–519, `panic("Node meta data provided is longer than the limit")`
- `~/go/pkg/mod/github.com/hashicorp/memberlist@v0.6.0/config.go` — lines 312–336 (`IndirectChecks: 3`, `SuspicionMult: 4`, `ProbeInterval: 1s`, `PushPullInterval: 30s`, `UDPBufferSize: 1400`)
- `~/go/pkg/mod/github.com/hashicorp/memberlist@v0.6.0/state.go` — lines 1211–1223 (`k := SuspicionMult - 2`; `if n-2 < k { k = 0 }`)
- `~/go/pkg/mod/github.com/hashicorp/memberlist@v0.6.0/suspicion.go` — lines 69–76 (`if k < 1 { timeout = min }`)

**Commands run (all on `gb-mbp`, 2026-07-15/16):**
- `ifconfig utun0 | grep mtu` → `mtu 1280`
- `ping -c 3 100.115.27.55`, `ping -c 3 100.120.22.74`, `ping -c 10 -i 0.3 <both>` → RTT/jitter table in §1.4
- `tailscale status --json` piped through `python3` → the 8-peer table in §1.1 (names, IPs, OS, Online, ID, LastSeen)
- `go version` → `go1.26.2 darwin/arm64`
- `lsof -nP -iTCP:19001 -iTCP:19002 -iTCP:19003 -sTCP:LISTEN` → found `window` pid 68436 holding two of my chosen ports; python `socket.bind` confirmed `Errno 48`
- Python `socket.create_connection(("100.115.27.55", 22))` × 3 → TCP dial 56.3ms cold, 4.8/5.3ms warm
- `grep -n 'delete(' main.go` → no matches (no reaping path exists)
- `go vet`, `gofmt -w`, `go run .`

**Experiment written and run:** `/tmp/rosterproto/` (`main.go` 217 lines, `flag.go`) — archived beside this doc as `23-roster-probe-prototype.go.txt`. Three in-process nodes named `dgx`/`gb-mac-mini`/`gb-mbp`, real TCP listeners on OS-assigned ports, real HTTP probes, static peer lists, timings scaled 5x (200ms interval). Produced:
- steady-state 6/6 directed edges UP
- UP→DOWN transition 1.302 / 1.300 / 1.306s = 6.5 probe intervals (≈6.5s at the proposed 1s interval)
- DOWN→UP transition 2ms / 201ms / 1ms / 99ms = ≤1 probe interval, including after a **12s / 60-interval** freeze
- announced-sleep intent surviving into the DOWN state
- a falsified hypothesis: `NO_REUSE=1` (`http.Transport{DisableKeepAlives}`) changed rejoin 1.2s→1.0s, disproving my connection-reuse theory; the real cause was my own instrument reporting `LastSeen` instead of the transition instant

**Not read, deliberately:** `~/research/2026-07-15-agent-comms-substrate/` (BRIEF §9 — context poison risk; salvage is another agent's task, and I relied on `investigate/15`'s summary of it via the task briefing).
