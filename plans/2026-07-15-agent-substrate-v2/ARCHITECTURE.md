# ARCHITECTURE — the agent substrate

**Date:** 2026-07-16
**Status:** Reconciled design. Five independent subsystem designs (`design/20`–`24`) were written against `BRIEF.md`; this document is the single architecture they collapse into. Where they disagreed, §11 records the verdict and why the loser lost.
**Authority:** `BRIEF.md` is the problem statement. This document answers it. Where this document contradicts a subsystem doc, this document wins.

**Labels, used strictly throughout:**

- **[MEASURED-HERE]** — I ran it today, in this session, and the output is quoted.
- **[MEASURED-nn]** — a subsystem doc ran it; I read their doc and cite it. `nn` is the doc number.
- **[VERIFIED-HERE]** — I opened the source or the box myself and confirmed a claim someone else made.
- **[CLAIMED]** — someone asserts it; nobody here verified it.
- **[DEDUCED]** — inference. Could be wrong. Flagged every time.

---

## 1. The pitch

You have three computers. Everything an agent learns on one of them dies there, and an agent that wants to reach another machine has to rediscover how to get there every single time. So: put one small background program on each machine. Its entire job is to move bytes between named mailboxes, write bytes to the local disk when asked, and keep an honest list of which machines are currently reachable. That program understands nothing else — it has never heard of an agent, a log file, or an SSH key. Everything that *does* understand those things is a separate little program that plugs into it and can be swapped out while everything keeps running. No machine is in charge. Any two can talk with the third one dead or asleep, and a brand-new machine boots and works alone in about 30 milliseconds without asking anyone's permission.

---

## 2. The problem

Straight from BRIEF §2, in Greg's words, and nothing else:

> - *"something figured out on one machine cannot be used on another machine."*
> - *"something an agent figures out cant be sent to another agent on another machine."*
> - *"if I did something yesterday one one project on one machine, how can I find that thing from another machine in another agent?"*
> - *"how could all learning of all agents be centralized and searchable?"*
> - *"I bring up zookeeper - not for high availability - but to sync who is on/off line and as a 'shared' address book - so an agent doesn't stumble over the wrong machine name/ip/username/inability to connect via ssh."*

**The through-line: the fleet has no shared memory and no shared address book.** Everything an agent learns dies on the box it learned it on. An agent that wants to reach another box has to rediscover how.

And agent comms is first-class, not a footnote:

> *"I've also started plenty of agents and then converge on similar things I'm working on and I need them to send messages back and forth... I can have one agent comm with another and figure something out - but they can still have different directives."*

Agents converge on similar work and need to talk it out **themselves**. They keep different directives. **The system's job is to carry the message. It never judges, never detects overlap, never warns.**

That last sentence is a design constraint, not a mood. It shows up in this architecture as concrete refusals — the kernel returns *every* claimant of a contested name and picks none of them (§4.3); the system reports a mismatch and never resolves it (§11 row 3). Every place the system could have an opinion, it reports a fact instead.

---

## 3. The layer cake

Two names, introduced here and used for the rest of the document:

- **`fleetd`** — the daemon. One per box. Contains the kernel. This is the thing that is always running.
- **`fleetctl`** — the command-line tool a human (or an agent) uses to talk to a local `fleetd`.

A word you need: a **plugin** here is *a separate ordinary program* — its own file, its own process — that `fleetd` launches as a child and talks to over a socket. It is not a library, not a `.so`, not code loaded into `fleetd`. That choice is what makes both live reload and crash isolation work (§7).

```
        BOX 1 (gb-mbp, laptop)                              BOX 3 (dgx, always-on)
        ══════════════════════                              ══════════════════════

  agents: Claude Code, Codex, Pi ...              agents: Claude Code, Codex, ...
        │                                                       │
        │ curl / ConnectRPC over LOOPBACK                       │  (same, loopback)
        │ (never crosses a machine)                             │
        ▼                                                       ▼
  ┌───────────────────────────────────┐             ┌───────────────────────────────────┐
  │ PLUGINS  (separate processes)     │             │ PLUGINS                           │
  │  comms │ registry │ logtail │ ssh │             │  comms │ registry │ logtail │ ssh │
  │   ▲        ▲          ▲       ▲   │             │   ▲        ▲          ▲       ▲   │
  └───┼────────┼──────────┼───────┼───┘             └───┼────────┼──────────┼───────┼───┘
      │        │          │       │                     │        │          │       │
      │  gRPC over a unix socket (one file on disk).    │   ═══ THE KERNEL/PLUGIN LINE ═══
      │  LOCAL ONLY. Never crosses a machine.           │   Above: knows what an agent is.
      │        │          │       │                     │   Below: has never heard of one.
  ┌───▼────────▼──────────▼───────▼───┐             ┌───▼────────▼──────────▼───────▼───┐
  │ KERNEL  (inside fleetd)           │             │ KERNEL  (inside fleetd)           │
  │                                   │             │                                   │
  │  channels · storage · roster      │             │  channels · storage · roster      │
  │  19 RPCs. Payload = opaque bytes. │             │  19 RPCs. Payload = opaque bytes. │
  │                                   │             │                                   │
  │  ┌─────────────┐  ┌────────────┐  │             │  ┌─────────────┐  ┌────────────┐  │
  │  │ SQLite      │  │ probe loop │  │             │  │ SQLite      │  │ probe loop │  │
  │  │ box-local.  │  │ own socket │  │             │  │ box-local.  │  │ own socket │  │
  │  │ NEVER       │  │            │  │             │  │ NEVER       │  │            │  │
  │  │ replicated. │  │            │  │             │  │ replicated. │  │            │  │
  │  └─────────────┘  └──────┬─────┘  │             │  └─────────────┘  └──────┬─────┘  │
  │  ┌─────────────────────┐ │        │             │  ┌─────────────────────┐ │        │
  │  │ embedded NATS       │ │        │             │  │ embedded NATS       │ │        │
  │  │ (a Go library, NOT  │ │        │             │  │ (a Go library, NOT  │ │        │
  │  │  a separate server) │ │        │             │  │  a separate server) │ │        │
  │  └──────────┬──────────┘ │        │             │  └──────────┬──────────┘ │        │
  └─────────────┼────────────┼────────┘             └─────────────┼────────────┼────────┘
                │            │                                    │            │
                │            └────── liveness probes ─────────────┼────────────┘
                │                    (1/sec, its own listener)    │
                │                                                 │
                └──────────── channel data (NATS routes) ─────────┘
                             ▲                                    ▲
                             │        BOX 2 (gb-mac-mini)         │
                             └────────────┐         ┌─────────────┘
                                        ┌─▼─────────▼─┐
                                        │   fleetd    │   FULL MESH: 3 links, no middle.
                                        └─────────────┘   Any 1 box works alone.

  WHAT CROSSES A MACHINE BOUNDARY — the complete list. Nothing else. Ever:
    1. Channel data      (bytes on a named channel)          — over NATS routes
    2. LOOKUP entries    (the one replicated thing)          — over NATS routes
    3. Liveness probes   ("are you alive?" / "yes, boot_id") — over its own socket

  WHAT NEVER CROSSES A MACHINE BOUNDARY:
    · Kernel storage (SQLite). Box-local, always. Want it elsewhere? Publish it.
    · Plugin binaries. Records cross; bytes are fetched and verified locally.
    · go-plugin traffic. fleetd talks to ITS OWN children only.
    · Any opinion about a third box's liveness or clock.
```

**How to read the line.** Everything above it is domain knowledge — agents, logs, SSH, tmux. Everything below it is bytes, boxes, and disk. The line is not a convention; it is enforced three ways, and the strongest one is that the kernel's `payload` field is typed `bytes` rather than protobuf's `Any`. `Any` would require the kernel to keep a registry of message types in order to decode them. With `bytes`, **the kernel cannot parse a payload even if a future contributor wants it to.** The leak is impossible to write, not merely caught in review. (The other two mechanisms are a dependency allowlist test and a vocabulary test — §6.4.)

---

## 4. The kernel

The kernel is 19 remote procedure calls (**RPC** = calling a function that lives in another process) and one rule: **it moves bytes between named, typed channels, writes bytes down when a plugin asks, and knows which boxes are alive. It does not know what any byte means.**

The full reconciled interface is on disk at **`design/25-reconciled-proto/fleet/kernel/v1/{kernel,plugin}.proto`**.

**[MEASURED-HERE]** I verified the reconciled proto end to end this session:

```
buf lint (STANDARD)                              rc=0
buf build                                        rc=0
boundary test                                    VOCABULARY CLEAN -- the kernel names no domain concept.
buf generate (protoc-gen-go + protoc-gen-connect-go)
  -> gen/fleet/kernel/v1/{kernel,plugin}.pb.go
  -> gen/fleet/kernel/v1/kernelv1connect/{kernel,plugin}.connect.go
go build ./...                                   BUILD OK           (darwin/arm64)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build   CROSS-COMPILE OK   (the dgx)
```

That last line is not a nicety. **The dgx has no Go toolchain and no C toolchain [MEASURED-22]**, so every artifact must cross-compile from the laptop with cgo off. This makes cgo anywhere in the dependency tree fatal, fleet-wide — which is why several otherwise-reasonable options are structurally disqualified before anyone argues about their merits (§6.2).

### 4.1 Channels — a name and a type

This is Greg's own architectural realization (BRIEF §3.1) and the whole design hangs off it:

> *"What if the daemon had 'channels', a channel had a name and a type (pubsub, etc), then the daemon would do data transport, while the plugin handled all the logic."*

**Four types ship. Greg's menu listed five; here is the honest accounting.**

| Type | What it does | Plain English |
|---|---|---|
| `PUBSUB` | Every subscriber whose pattern matches gets a copy. | A radio broadcast. |
| `POINT_TO_POINT` | Competing consumers: exactly one member of a named group gets each message. | A work queue. 1:1 is the case with one worker. |
| `REQUEST_REPLY` | One question, one answer, correlated by the kernel. | A phone call. Greg asked for this by name. |
| `LOOKUP` | A replicated map. **The only thing the kernel replicates.** | The shared address book. |

**Why "fanout" is not a fifth type.** Greg's menu was *"publish/lookup table, point to point, pubsub, fanout, whatever"*. The word "fanout" is ambiguous and three subsystem docs hit that ambiguity independently. It has two possible meanings and **both are already shipped**: if it means *broadcast*, that is `PUBSUB`; if it means *work queue*, that is `POINT_TO_POINT`. In every transport that offers both "fanout" and "pubsub", they are the same primitive — deliver a copy to every interested subscriber — differing only in exact-name versus pattern subscription, and pattern matching is free. A third reading, *scatter-gather* (one question, many answers, bounded deadline), is a **plugin** built from `PUBSUB` + collect-with-a-deadline; it does not need kernel support.

**So: he listed five and gets four, and he should hear that from us rather than notice his list came back shorter.** Building can start either way, because every reading of the ambiguous word is covered. This is worth 30 seconds of his time to confirm, not a blocker.

**LOOKUP is the load-bearing one, and it is the only thing the kernel replicates — deliberately, and only because it can do so with no conflict-resolution policy at all.** A `LookupPut` records `writer_node = me`. **A node owns every key it writes.** Another node writing the same key name creates a *separate* entry; it never overwrites. Therefore conflicts cannot exist, therefore no resolver exists, therefore CAP theorem never comes up. That honours BRIEF §6's *"I dont want to get hung up on CAP theorem and shit"* **structurally rather than by deferring it**. `revision` is monotonic *per writer* and is only ever compared within one writer — so the three boxes' unsynchronised clocks are irrelevant.

**Name clashes: `LookupGet` returns ALL claimants and the kernel refuses to pick.** If two boxes claim one name you get two entries. Silent resolution is a judgement, and BRIEF §2 says the system never judges. Zero code, and the common case has exactly one element.

### 4.2 The delivery guarantee: at-most-once, on every channel type, always

**This is the spine of the design, so it gets stated bluntly: the kernel never queues, never retries, never redelivers.** Publish something to a sleeping box and it is gone.

That sounds like a bug. It is BRIEF §4's *"durability is a PLUGIN decision, not a kernel guarantee"* made **structural**: there is no kernel-side queue to lose, and no kernel durability promise to violate. The kernel does not offer a `SendReliably` verb, because a kernel verb would quietly make durability a kernel guarantee, and then §4 is false.

Instead the kernel hands plugins **four tools to build any durability class they want**:

1. **A local journal** — append-only, on this box's disk, with a per-call `sync` flag.
2. **A stamped `message_id`** — the hook a plugin builds acknowledgement and de-duplication on.
3. **An interest signal** — "is anyone, anywhere, listening?"
4. **A liveness signal** — "is that box reachable from here?"

**And BRIEF §5's entire model is three kernel calls.** Greg's stated model —

> *"all the messages are written down. If an agent is subscribed, then they get a notification. If they are polling, then they read their messages the next time they come in."*

— decomposes exactly:

| Greg's words | Kernel call | Owner |
|---|---|---|
| "all the messages are written down" | `JournalAppend(sync=true)` | comms plugin |
| "if subscribed, they get a notification" | `Publish` (a doorbell) | comms plugin |
| "if polling, they read next time" | `JournalRead(after_seq)` | comms plugin |

**§5 is satisfied entirely by the comms plugin, and §4 is not violated.** That is the whole architecture in one table.

> ⚠️ **The loaded gun, stated as loudly as the feature.** Core NATS is memory-only. If you skip the plugin layer and just `Publish` to a sleeping box, the message is **gone and `Publish` returns `nil` — it does not even tell you** [MEASURED-15, re-cited by 21]. That is exactly the trade BRIEF §4 asked for. But it means **the durability plugin is not optional polish; it is the thing that makes §5 true.**

### 4.3 The ACK gap (BRIEF §5) — and a correction that matters

Greg's known flaw:

> *"we actaully should probably notify the sender that the receiver is not listening. I believe we can find that out. So we could do some type of ACK system."*

**He is right that we can find it out. Two earlier investigations got the mechanism wrong in a way that would ship a real bug, and this correction is the single most important measured finding in the corpus.**

Investigations 11 and 15 measured NATS's `no responders available` signal and told Greg §5's gap closes *"for free / zero lines of code."* They tested it by **killing a process**. A killed process sends a TCP `FIN` or `RST` — a polite goodbye. **A sleeping laptop sends nothing at all.** The sockets stay `ESTABLISHED` and the bytes simply vanish. Design 21 built a blackhole proxy to model exactly that — which is BRIEF §1's *normal state* for `gb-mbp`, not an edge case — and measured the sender getting an ambiguous **timeout, not an honest signal, for 61 seconds** [MEASURED-21]. It then root-caused the number in the NATS source before believing it (`route.go:140`, `defaultRouteMaxPingInterval = 30s`, which clamps the 2-minute global default).

**Consequences, both adopted:**

1. **`Cluster.PingInterval = 2s` (with `MaxPingsOut = 2`) is mandatory configuration, not tuning.** It cuts the stale-interest window from **61.4s → 5.0s** [MEASURED-21]. A 12× improvement for one field, costing one ping every 2s on two links whose measured round-trip is 4–7ms.
2. **`Interest` is a HINT. It is never the ACK.** The proto says so in a comment, and it must never be sold as more. `INTEREST_PRESENT` means *"someone was listening as of a few seconds ago"* — **not** *"it was delivered."*

**So what is the real answer to §5?** A **four-rung ladder**, designed by 24, where each rung is a mechanical fact and none is a judgement:

| Rung | Question | Where the answer comes from | Cost |
|---|---|---|---|
| 1 | Does the name even exist? | `LookupGet` on the registry | microseconds, local |
| 2 | Is the box up? | roster | microseconds, local |
| 3 | Is the daemon + plugin listening? | kernel `Interest` | ~310µs [MEASURED-24] |
| 4 | **Is the agent actually reading?** | the receiver's cursor, piggybacked on the reply | free |

**Rung 4 is the one Greg actually wants and the one no transport gives you.** The end-to-end ACK is the comms plugin's own, and it carries the receiver's cursor lag back with it. The result is a sentence like:

> `DELIVERED, but receiver stale — 47 unread, last read 3h12m ago.`

That is actionable. An ACK bit is not. And it is all mechanical facts — no judgement, no warning, no opinion. §2 preserved.

### 4.4 Storage — box-local and plugin-private *by construction*

BRIEF §3.1: *"One thing that might be useful is to have a storage mechanism in the daemon. Then the plugins dont make up their own thing."*

Two decisions carry this, and both are structural rather than enforced:

**1. There is no `namespace` field in any storage RPC.** Look at the proto — it is not there. The kernel derives the namespace from the calling plugin's registered identity and prefixes it server-side. **Reading another plugin's storage is not forbidden; it is unrepresentable.** There is no field to put the wrong value in.

*Scoped honestly:* this is a **bug-prevention** boundary, not a **security** one. Any local process on the loopback port can claim any identity; `fleetctl kv` reads any namespace for debugging. That is correct and deliberate under BRIEF §4's trusted network of three owned boxes — and it becomes wrong the moment a machine Greg does not own joins, which §6 puts out of scope *"for now"*. The word "now" is doing work; write it down.

**2. Storage is never replicated.** *"If you want data on another box, publish it on a channel."* This is the second-strongest simplification in the design after at-most-once: no conflict resolution, no quorum, no leader, no CAP argument — and **the store can never be the reason one box's death hurts another.**

**Two shapes, because they solve different problems:**

- **KV** — `KVPut/Get/Delete/List/Watch`, with `if_revision` for compare-and-swap. This is where plugin cursors live. *Never in a plugin-owned file* — that is a bug class the prior round actually shipped [MEASURED-15].
- **Journal** — `JournalAppend/Read/Truncate`. Append-only. Sequence is monotonic **per (plugin, journal) on this box only.**

**The engine is SQLite (`modernc.org/sqlite` — a pure-Go rewrite, no C).** [MEASURED-20]: WAL mode works with `CGO_ENABLED=0`, 227k appends/sec, static aarch64 binary, 26 modules. It loses on raw throughput to bbolt and wins anyway, because **a human can open the file with `/usr/bin/sqlite3` at 2am.** At tens of agents, inspectability beats throughput. Durability knobs are per-call, never a silent kernel default.

### 4.5 Roster — baked in, and the honest thing about liveness

BRIEF §3.1: *"The machine roster probably should be baked in... Its kinda only there to keep track of whos around... I dont want to get hung up on CAP theorem and shit."*
His model: *"machines have a list of other machines. Then send pings to each other to check they're online."*

**We are building exactly what he described, almost literally: a static list of boxes from a config file, and a probe every second.** ~350–500 lines of Go. Not a library. §11 row 2 explains why the obvious library lost, with source I read myself.

| Setting | Value | Grounded in |
|---|---|---|
| probe interval | 1s | — |
| probe timeout | 1s | 11× the worst RTT measured on this fleet (92ms) [MEASURED-23] |
| SUSPECT | 3 consecutive failures | — |
| DEAD | 6 consecutive failures (~6.5s) | matches memberlist's own 5.7–6.6s [MEASURED-23] |
| UP | 1 success | rejoin ≤1 probe interval |

**Liveness is computed locally and never gossiped, and that is the subtle part.** BRIEF §5 needs a **per-observer** answer — *"can **I** reach B?"* — not a global one. Gossip answers *"is the fleet's consensus that B is up?"*, which would **launder away the exact fact a sender needs** when the A→B path is broken but C→B is fine. Per-observer disagreement is the **correct answer**, not an inconsistency to fix.

**Four states, each a pure function of a local counter: `ALIVE` / `SUSPECT` / `DEAD` / `UNKNOWN`.**

Two states were **rejected** and the reasoning generalises:

- **`LEFT`** ("it announced its departure") is not mechanically detectable, so a state that depends on a shutdown hook is **silently wrong exactly when the hook didn't fire** — which is the case you built it for. Supporting evidence: memberlist's own `NotifyLeave` reports `State=0` (Alive) for a graceful leave [MEASURED-13] — *you literally cannot read the state to tell "left" from "dead."* So departure becomes `DEAD(reason = ANNOUNCED_SLEEP)`: an `Intent` we were **told** plus a `Reason` we **inferred**, never a fact we claim to observe. It degrades correctly when the hook doesn't fire.
- **`stale`** is a property of *data*, not of a *node*. Return an `age`; let the reader judge.

**The address book is thin, because Tailscale already did the hard half.** Store **exactly one address per box — the Tailscale IP — and never resolve a name.** The six address forms and nine failure modes investigation 13 measured are *the disease, not the cure*. This design does not depend on DNS working, which is fortunate: **both Macs' MagicDNS is broken and `ssh dgx` from the laptop silently goes over the home LAN instead of the tailnet** [MEASURED-24].

**Agent names are hierarchical: `<node>/<local>`,** e.g. `dgx/refactor-3`. This **deletes** the name-clash problem rather than solving it — flat namespaces need a resolver *because* they are flat — and the name contains the route.

**The roster keeps its own dedicated listener, regardless of the transport.** ~100 lines of `net/http`. **A failure detector that shares fate with the thing it monitors is not a failure detector.** This is the one place we deliberately accept a second network path, and it is worth it: it means liveness stays honest even if the NATS mesh is wedged.

### 4.6 Plugin registration

BRIEF §3.1: *"There would be a registration process with the daemon, and the plugin would say what resources it was interested in/needed to react to. Maybe it needed changes in the node list."*

That is implemented literally. A plugin's **manifest** declares its namespace, its channels, and its interests (channel patterns, and/or "tell me when the node list changes"). It is **data, inspectable without running the plugin.**

**Channels are declared statically in the manifest and refined dynamically within what was declared.** Not a config file (a second place to be wrong). Not dynamic-on-first-publish — because two plugins could disagree about a channel's *type* and the kernel would have no basis to arbitrate, so messages would silently vanish. Static declaration buys three things: type conflicts are **rejected at registration, before any byte moves**; the plugin's full surface is inspectable without executing it; and a live reload can diff the new manifest and **refuse** a reload that would change a live channel's type.

**Cross-box registry mismatch is detected and reported, never resolved.** Manifests gossip on a LOOKUP channel. A type mismatch across boxes logs loudly and shows red in `fleetctl channels`. **The kernel does not pick a winner** — that needs consensus, which needs quorum, which §4 forbids.

---

## 5. The plugins

Four ship. Each is an ordinary Go program. **Note what is *not* in the kernel: comms is a plugin. That is Greg's decision (BRIEF §3.1) and it is the correct one.**

| Plugin | What it does | Kernel surface it uses |
|---|---|---|
| **comms** | Agent-to-agent messages. Owns durability, retry, de-dup, end-to-end ACK, the outbox/inbox. | `JournalAppend(sync=true)`, `JournalRead`, `Publish`, `Subscribe`, `Request`/`Serve`/`Respond`, `KVPut` (cursors), `LookupGet`, `RosterWatch` |
| **registry** | The agent list. Names, presence, "what is this agent doing". | `LookupPut`/`Get`/`List`, `KVPut`, `RosterWatch` |
| **logtail** | Watches agent transcript files, ships lines onto channels. | `JournalAppend`, `Publish`, `KVPut` (file offsets) |
| **ssh** | Continuously verifies every box can reach every box. Hands agents a *verified* command. | `RosterList`/`Watch`, one LOOKUP channel |

**The ssh plugin is the best evidence the boundary is drawn right.** The fiddliest plugin anyone imagined needs exactly: roster reads, roster change events, and **one** self-declared LOOKUP channel for host-key fingerprints (~50 bytes). **The kernel needs zero SSH knowledge.** If that plugin had needed more, the boundary would be in the wrong place.

**Three things worth stealing from the plugin designs (24), because they are already-learned lessons:**

1. **`tmux_target` is a registered fact, never derived from a naming convention.** This deletes a bug class that bit harmonik twice.
2. **Vendor file paths are discovered by watching directories, never derived.** [MEASURED-24]: Claude maps `.`→`-` and keeps a leading `-`; Pi preserves dots and wraps in `--`. **Two incompatible undocumented rules mean a path-deriver is a bug with a release schedule.** Harmonik's deriver produces 0-of-3 correct paths today.
3. **The doorbell injects only a fixed nudge, never the message body.** The log is truth; the pane poke is a doorbell. Poll is the contract; the doorbell is an optimisation **allowed to fail**.

**And one thing worth *not* porting:** harmonik's `commscursor.go` (323 lines) is **deleted entirely** by `KVPut(if_revision=…)`, because the daemon becomes the single writer.

**The archive PULLS; it never receives.** The dgx has 3.2TB free [VERIFIED-HERE: `df -h /` → `3.2T` available] versus the mini's 37GiB, so it is the natural archive. **That single property — pull, not push — is what makes the dgx a storage *destination* rather than the fatal hub BRIEF §9 warns about.** If the dgx dies, nothing stops: every box keeps its own complete log. §4 forbids a box whose death **stops** the system, not one whose death degrades a **consumer** — and BRIEF §4 explicitly makes search a consumer.

**The plugin library** (Greg's *"[Idea] the first plugin is a plugin library"*): **build it, with one word changed, and not first.**

- **The word:** sync the **manifest**, never the **bytes**. The fleet is two platforms [MEASURED-22: gb-mbp `Darwin arm64`, gb-mac-mini `Darwin arm64`, dgx `Linux aarch64` — I re-verified both remote boxes this session]. **A darwin binary synced to the dgx is not a plugin, it is garbage.** So the unit of replication is a *record* (name, version, platform, sha256, url); each box fetches its own platform's bytes and **verifies the checksum before exec**. Istio and Nomad both converged on exactly this; Traefik is the counterexample, where *"the archive hash is **optionally** checked"* [CLAIMED-14].
- **The order:** it cannot be first — *you cannot distribute plugins before you can run plugins.* And `fleetctl plugin install <path>` (~50 lines, built first) makes the library **optional forever**, which is what defuses "the recovery path is the thing you broke."
- **The test is empirical and Greg should apply it himself: is `scp`-ing two binaries actually annoying yet?** With 4 plugins × 2 platforms and one operator, it may simply not be. It is an **[Idea]** in the brief and should be held to that standard.

---

## 6. The transport decision

**Greg asked this twice and never got an answer (BRIEF §7.1). Here it is, straight.**

> **Verdict: embed `nats-server` v2.14.3 as a Go library inside each `fleetd`. Core NATS only — `JetStream: false`. Full mesh: each daemon's route list is the other two Tailscale IPs. Agents connect to their own box over loopback. `Cluster.PingInterval = 2s`. Bind to the Tailscale IP, never `0.0.0.0`.**
>
> **This contradicts a stated preference of Greg's, and it is argued here rather than slid past him.**

### 6.1 Greg's instinct, judged honestly

> *"Part of the problem is that I dont trust my boxes. So I strongly lean away from NATS. I think with ZeroMQ we can create more of a distributed system... call it something else if you want - not-centralized"* (BRIEF §4)

**He is right about the shape and wrong about the label.**

His **requirement** is: *no box's death may kill the system.* His **guess** is that NATS cannot give him that. The brief itself separates these two things — it says *"Read this precisely: no single box's death may kill the system."* **The requirement is binding. The guess is testable. The guess is measured false:**

```
all 3 alive: gb-mbp routes=2

>>> killing dgx AND gb-mac-mini -- gb-mbp is now ALONE <<<
gb-mbp routes=0 (alone)
  request/reply on the lone box: err=<nil> resp="served by the lone survivor"
  pubsub on the lone box:        err=<nil> msg="two boxes are dead and I still work"

  a node COLD-STARTED in 28ms with both configured peers dead (routes=0)
  and immediately served its local agents: err=<nil> resp="ok"
```
[MEASURED-21]

And on the **real fleet over real Tailscale**, killing **the actual DGX** — the exact box the previous round made the hub:

```
[   0.6s] PHASE 2 -- KILLING THE DGX NOW
[   1.1s]   gb-mbp dropped the dgx route after 0s -> routes now: [gb-mac-mini]
[   1.1s]   gb-mbp -> gb-mac-mini WITH DGX DEAD: err=<nil> resp="pinned:gb-mac-mini" rtt=9.273ms
```
[MEASURED-21]

Two of three boxes dead; the survivor does everything. A new box boots **alone in 28ms with every peer dead**. No leader, no election, no quorum, nothing to wait for.

**The word "broker" caused this entire argument, and untangling it dissolves the disagreement.** It means two completely different things:

1. **"A separate machine in the middle."** ← What Greg correctly rejects. What the last round built. What §4 forbids.
2. **"A library inside your program."** ← What ZeroMQ is. **And what an embedded `nats-server` also is.**

**The thing Greg fears is real — but it is JetStream, not NATS.** JetStream is NATS's optional disk-persistence add-on, and it runs Raft (a voting algorithm needing a majority alive: with 3 boxes, 2 must be up, so **losing the laptop and one other box stops writes dead**). Design 21 verified by grep that **every `startRaftNode`/`bootstrapRaftNode` call site in the entire codebase lives in `jetstream_cluster.go`** [MEASURED-21, CONFIRMED-CODE]. **With `JetStream: false`, the Raft machinery is unreachable dead code.**

**So the shape is "one library-broker per box, meshed, with nothing in the middle" — and that shape is exactly what Greg's requirement demands.** To be clear about who is saying what: Greg did **not** propose "a broker on every node," and this document earlier claimed he did — that was wrong and is struck (see Amendments). His words are *"I strongly lean away from NATS"* (BRIEF §4). **We are overruling that stated preference**, on the measurements above, and he should get the chance to reject the override after reading them. What is genuinely his is the *requirement* the preference was protecting — *"no single box's death may kill the system"* — and that requirement is met, structurally, by the mesh.

> ⚠️ **`JetStream: false` is one boolean away from re-importing every single problem Greg fears.** Someone will eventually want the key-value store and flip it "just for this one thing." **It must be a guarded, commented, tested invariant — not a config default.**

### 6.2 So does ZeroMQ make sense? Yes as an idea. No as a library.

**The ZeroMQ *idea* is right for this. The ZeroMQ *library* is the wrong way to get it, and the reasons are specific to this fleet rather than general grumbling.**

The idea — *a socket that has a messaging pattern baked into it; a library, not a server* — is a genuinely close structural match to BRIEF §3.1's "a channel has a name and a type." **Greg's instinct here is good.** Three measured facts kill the library anyway:

1. **It cannot reach the dgx.** The mature Go binding (`pebbe/zmq4`) wraps C, so it **cannot cross-compile — tried, it fails** [MEASURED-10]. The dgx has no Go and no C toolchain [MEASURED-22]. **That is fleet-fatal before any feature discussion.** The pure-Go alternative's README says *"zmq4 needs a caring maintainer"* [MEASURED-10]. `libzmq` itself has gone **2.8 years without a release**, commits collapsing 167/yr → 9/yr [MEASURED-10].
2. **It structurally cannot deliver BRIEF §5's ACK requirement.** The ZeroMQ Guide says it itself: there is *"no way to layer it on top because subscribers are invisible to publisher applications."* **The publisher cannot see its subscribers at all.** That is Greg's *"notify the sender that the receiver is not listening"*, refused at the architecture level.
3. **Its own answer to durability is a central broker.** ZeroMQ's durability pattern (Titanic) is layered on its Majordomo **broker**. **Following ZeroMQ's own guidance lands you back at the exact hub BRIEF §9 says sank the last round.**

Also measured, and worth seeing because it is the failure mode Greg would actually hit: PUB/SUB **silently dropped 98,906 of 100,000 messages with zero errors returned** [MEASURED-10].

**The genuine runner-up is `mangos` (pure-Go NNG — the ZeroMQ author's own successor).** It fixes the cross-compile problem completely and its patterns fit well. It loses on three grounds:

- Its **own doc's headline argument cuts the other way.** Investigation 10 argued *"the transport is the small decision; the log is the project"* — the ~1,500-line durable log costs the same either way. **Correct. And therefore: take the transport that also gives you fleet-wide interest, cross-box queue groups, wildcards, and a free server address book — none of which mangos has.** If the log costs the same, the tiebreak is everything else.
- It **cannot deliver §5's ACK signal either** — the same subscriber-invisibility problem, where core NATS returns `no responders` fleet-wide.
- v3.4.2 is **3.9 years old** [MEASURED-21], and its fleet evidence is loopback-only, versus NATS's real-Tailscale numbers on Greg's actual boxes.

**Others, briefly:** *JetStream/clustered NATS* loses on 2-of-3 quorum. *Leafnode-hub* is structurally unfixable (NATS rejects a leaf mesh as a protocol violation). *libp2p/QUIC* is broken on this network — a hardcoded 1280-byte initial packet against a **1280 MTU** [VERIFIED-HERE: `ifconfig utun0` → `mtu 1280`] — and pays 129 modules for NAT traversal **Tailscale already solved**.

### 6.3 The hedge, and it is cheap

**The API contains no reference to NATS.** The transport sits behind a **6-method Go interface** (`Publish/Subscribe/Request/Serve/Interest/SetPeers/Close`) in one file, `internal/kernel/natstransport.go` — **the only file permitted to import `nats-server`**, enforced by the dependency allowlist test.

**The 19-RPC API is what Greg approves. NATS is an implementation detail.** If he rejects it after reading §6.1, the API does not change and one file gets rewritten. That is the whole cost of being wrong here, and it is why recommending against his stated lean is defensible rather than reckless.

### 6.4 Security, and what is theatre

**Tailscale IS the perimeter.** WireGuard: already authenticated, already encrypted, already paid for. What is actually needed: bind to the Tailscale IP never `0.0.0.0`; loopback for agents; checksum plugin binaries (as a control against shipping a darwin binary to the dgx, **not** against an attacker).

**Named as theatre, so nobody builds it:** mTLS inside WireGuard on 3 owned boxes; NATS accounts/NKeys/JWT (there is one tenant); per-plugin ACLs.

> ⚠️ **One honest correction to the phrase "trusted network": the tailnet has 8 devices, not 3** — including an iPhone and an iPad, and **two devices named `localhost`** (one online) and two named `gb-mac` [MEASURED-23]. All Greg's, so in scope by the brief's own definition. But binding to the Tailscale IP exposes `fleetd` to **that whole set**, and it is why the node list is an explicit **config-file allow-list** rather than tailnet auto-discovery. Membership *discovery* — the thing gossip exists for — **is not a requirement here at all.** Settled by measurement, not taste.

---

## 7. Live reload

> *"In harmonik having to stop the whole system every time is so annoying. Wish we had Erlangs Beam, lol."* (BRIEF §3.2)

**Mechanism: `hashicorp/go-plugin` v1.8.0. A plugin is a separate program; reload = kill the child, start the new binary. `fleetd` never stops.**

### 7.1 What he gets

**[MEASURED-22]** Full reload cycle — spawn, handshake, register, first successful call:

```
=== dgx (linux/arm64) ===         === gb-mbp (darwin/arm64) ===
  warm reload: 3-4 ms                warm reload: 7-11 ms
  first-ever exec: 7ms               first-ever exec: 505ms  <- macOS signature check
```

The 505ms is macOS validating a binary it has never seen, and **it can be moved off the reload path entirely**: exec the binary once at *install* time and throw the result away (385ms, paid once), and the real reload drops to **9ms** [MEASURED-22].

**Greg's complaint is measured in "annoying" — i.e. human seconds. This is 4 milliseconds. The requirement is beaten by ~1000×.**

Two things come free and both matter:

- **Crash isolation.** Separate processes, separate address spaces, enforced by the CPU. A plugin panic is an ordinary Go `error` at the call site. **You cannot get this wrong.** Given §4's *"I dont trust my boxes"*, a plugin system where a buggy plugin kills the daemon would make every box *less* trustworthy.
- **A free diagnostic.** [MEASURED-22] `Canceled` = "I did this on purpose"; `Unavailable` = "it died." **So the kernel tells a deliberate reload from a crash with zero bookkeeping** — which is what makes the crash-loop budget correct.

### 7.2 What it costs

**Cost 1 — the kernel must own the drain, because go-plugin does not.** This was an open question; design 22 answered it by reading the source and then running it. `grpc_controller.go` carries a `// TODO: figure out why GracefullStop doesn't work` and calls the hard `Stop()` — **its doc comment describes behaviour the code does not have.** [MEASURED-22] A 3-second call in flight was **destroyed after 504ms**. With a ~40-line kernel-side drain gate, **the same call completed successfully.**

The asymmetry inside that gate is non-obvious and load-bearing: **unary calls are drained (waited for); streams are cancelled (never waited for).** A subscription never ends by itself — *a drain that waits for one waits forever.* Get this wrong and your first reload of the logtail plugin hangs the daemon.

**Cost 2 — and this is the real price: plugins may hold NO in-memory state across a reload.** [MEASURED-12] Write state, reload, read it back: `""`. Every time.

**The refund is that Greg already independently asked for the fix**, in §3.1: *"a storage mechanism in the daemon. Then the plugins dont make up their own thing."* **His storage instinct and his reload instinct are the same decision seen from two sides.** He wrote the storage line for tidiness; it turns out to be **the precondition that makes reload safe.**

> **The rule: a plugin is a pure function of kernel-held state plus its inputs. Enforce it day one.**

Four independent derivations agree: Vault reached it in production, investigation 12 reached it from measurement, design 23 reached it from the roster, and Greg reached it from tidiness. **Treat it as settled.** All four shipping plugins pass — and three hold no state at all.

### 7.3 The honest gap versus the BEAM

**Greg wants BEAM and is not going to get BEAM. Stating it plainly is more useful than a workaround.**

The specific thing he cannot have is Erlang's **`code_change/3`** — the callback that hands your *old in-memory state* to your *new code* so you can migrate it. **Nothing outside the BEAM has this, and nothing in Go can**: there is no mechanism to hand a struct built by one binary's compiler to a different binary's compiler and have it mean the same thing. **This is not hard; it is impossible. Do not design toward it.**

| | BEAM | This design |
|---|---|---|
| Swap code without stopping the system | ✅ | ✅ **4–9ms** |
| Crash isolation | ✅ | ✅ (OS processes) |
| Plugin keeps its memory across the swap | ✅ `code_change/3` | ❌ **Impossible. State lives in the kernel.** |
| Two versions live at once | ✅ | ❌ |
| **Reload the kernel itself** | ✅ | ❌ **← the real gap** |

**The last row is the honest limit, and it is close to what Greg actually complained about.** Changing the kernel — channels, roster, storage — still restarts `fleetd`. **The only mitigation is keeping the kernel genuinely small**, which is BRIEF §3's whole thesis (*"maybe the core is actually really light"*). The plugin system does not make the kernel reloadable; **keeping the kernel tiny does.** Hence the advisory ~3000-line tripwire (§10). Whether "small enough that restarts are rare" is achievable **is not answerable from a spec** — it needs the comms plugin built to find out. `go-plugin`'s `ReattachConfig` (plugins keep running while the daemon restarts under them) is the eventual escape hatch. **Know it exists; don't build for it.**

**The runners-up, briefly:** *WASM* is the genuine second place — faster, and **one artifact for all platforms** — but one module instance serves exactly one caller (8 concurrent callers corrupted the guest's allocator), guests can't do their own I/O (and logtail is a named plugin that wants exactly that), and the industry record includes Vector *removing* WASM and Envoy calling it experimental after four years [all MEASURED/CLAIMED-12]. **Not foreclosed — the same `.proto` can be served by a WASM module later.** *Yaegi* is the only Go option that reproduces a real BEAM property (two live versions, 3.6ms) and loses because **a plugin goroutine panic hard-kills the daemon** — unfixable, it is a Go language rule. **It offers BEAM's code-loading half with anti-supervision. Wrong half.** *Go's stdlib `plugin`* is disqualified by measurement: it **cannot load a second build of the same plugin at all**, and adding one field to a shared struct invalidates every plugin binary — **the exact opposite of protobuf's compatibility rules**, and therefore the opposite of what §3.2 asks for.

---

## 8. Three walkthroughs

### 8.1 A daemon starts and joins the fleet

```
1. fleetd reads ~/.fleet/config.toml
     node = "gb-mbp"; address = 100.87.151.114
     peers = [100.115.27.55 (dgx), 100.120.22.74 (gb-mac-mini)]
   ^ A FILE. Not discovery. The tailnet has 8 devices; exactly 3 are fleet, and two
     of the others are both named "localhost". [MEASURED-23]

2. Opens SQLite: ~/.fleet/storage/kernel.db  (modernc.org/sqlite, WAL, cgo off)

3. Starts the probe listener on 100.87.151.114:7946  — ITS OWN socket.
   Fate isolation: liveness must not share fate with the thing it monitors.

4. Starts the embedded NATS server (a library call, not a process):
     JetStream: false          <- the whole architectural decision
     Routes: [dgx, gb-mac-mini]
     Cluster.PingInterval: 2s  <- mandatory: 61s -> 5s stale window [MEASURED-21]
     bind: the Tailscale IP, never 0.0.0.0
   >>> If BOTH peers are dead this still succeeds, in ~28ms. [MEASURED-21]
       No leader. No election. No quorum. Boot order is irrelevant.

5. Serves KernelService (ConnectRPC over plain net/http) on loopback:7947.
   ONE .proto answers: curl+JSON, binary protobuf, AND a stock gRPC client.

6. Probe loop starts. Every 1s, probe each peer -> "alive? boot_id?"
     first success -> RosterWatch emits KIND_JOIN
     6 consecutive failures (~6.5s) -> STATE_DEAD

7. Plugin host, for each symlink in ~/.fleet/plugins/enabled/:
     verify sha256 -> REFUSE if mismatch, never exec unverified bytes [MEASURED-22]
     pre-warm (exec once, discard)  <- moves macOS's 500ms off the reload path
     exec -> plugin prints ONE line: "1|1|unix|/tmp/plugin2951246566|grpc|"
     Describe() -> manifest -> VALIDATE -> open declared channels -> Start()

8. Each plugin dials kernel_endpoint back and calls Subscribe(...) on its
   declared patterns. Data now flows plugin -> kernel, which is why the
   plugin API and the REST API are the same service.
```

**The property to notice: step 4 has no failure mode involving other boxes.** There is nothing to join, no seed to wait for, no consensus to reach. `fleetd` on a laptop in an airport works, serves its local agents, and merges into the mesh when the network returns. **That is BRIEF §4 satisfied structurally, not by careful coding.**

### 8.2 An agent on box 1 messages an agent on box 3 — awake, then asleep

An agent on `gb-mbp` wants to reach `dgx/refactor-3`. **It knows only that name.**

```
1. The agent, with no SDK and no library:
     curl -X POST http://127.0.0.1:7947/fleet.kernel.v1.KernelService/Request \
       -d '{"channel":"comms.send","payload":"<base64>"}'
   The comms plugin serves comms.send (REQUEST_REPLY).

2. comms resolves the name:  LookupGet(channel="registry.agents", key="dgx/refactor-3")
     -> entry{ writer_node: "dgx" }.   LOCAL map read. Microseconds. No network.
     (Two claimants? You get BOTH entries and the kernel picks neither. §2: never judge.)

3. comms checks liveness:  RosterList() -> dgx. Local. Microseconds.
```

**Path A — dgx is ALIVE:**

```
4a. JournalAppend(journal="outbox", records=[msg], sync=true)   <- ON DISK. fsync'd.
        ^ "all the messages are written down" (BRIEF §5). This is the ONLY reason
          the next step is allowed to be unreliable.
5a. Publish(channel="comms.deliver.dgx", group_key="dgx")       <- POINT_TO_POINT
6a. dgx's comms receives -> JournalAppend(journal="inbox", sync=true) -> THEN Respond(ACK)
        ^ Receiver writes to ITS OWN disk before acknowledging. Store-and-forward.
7a. gb-mbp's comms marks the outbox record acked: KVPut(key="acked/<id>", if_revision=…)
8a. Doorbell: Publish("comms.notify.dgx.refactor-3") -> dgx's comms pokes the tmux
    pane with a FIXED NUDGE, never the message body. The log is truth.
    If the doorbell fails: the agent reads its inbox next poll. Nothing is lost.
9a. The reply to the sender carries rung 4:
      "DELIVERED — receiver read it 12s ago."
      or "DELIVERED, but receiver stale — 47 unread, last read 3h12m ago."
```

**Path B — dgx is ASLEEP (BRIEF §1 says this is NORMAL, not an edge case):**

```
4b. Roster already says: dgx STATE_DEAD, reason=ANNOUNCED_SLEEP, last_seen 3h12m ago.
      - announced (lid closed with the hook firing): known in ~0.6s
      - crashed (hook didn't fire):                  known in ~6.5s
      Either way it is a LOCAL fact. No network call. No TCP timeout. No hang.

5b. comms STILL does JournalAppend(outbox, sync=true) — the message is safe on
    gb-mbp's disk — and returns IMMEDIATELY to the agent:

      QUEUED — dgx is asleep (announced 3h12m ago). Your message is on disk in
      the outbox and will deliver when dgx wakes. Nothing was lost.

    ^ THIS IS BRIEF §5's REQUIREMENT, ANSWERED. The sender is told, in one
      sentence, with a reason, in microseconds.

6b. dgx wakes. Its probe listener answers. gb-mbp's probe loop sees it within <=1s.
    RosterWatch fires KIND_JOIN.
7b. comms's outbox drainer wakes on that event and replays every unacked record.
    dgx's comms DEDUPES on message_id — some may already have landed.
```

> ⚠️ **What would happen without the comms plugin:** step 5a alone, with no journal. The message hits a sleeping box, is **gone**, and **`Publish` returns `nil`** [MEASURED-15]. **The plugin layer is not polish. It is what makes §5 true.**

**And the honest caveat about step 3:** between the laptop sleeping and the probe loop noticing (≤6.5s), the roster says `ALIVE` and the message takes path A. It lands in the outbox (`sync=true`), the ACK never comes, and the drainer retries it on wake. **The window is real, it is bounded at ~6.5s, and it costs a retry — not a message.** That is what store-and-forward buys.

### 8.3 An agent needs to SSH to the dgx and doesn't know how

This is BRIEF §2's *"an agent doesn't stumble over the wrong machine name/ip/username/inability to connect via ssh"* and §4's *"agents dont have to figure that out."*

```
1. curl -X POST http://127.0.0.1:7947/fleet.kernel.v1.KernelService/Request \
     -d '{"channel":"ssh.howto","payload":"<base64 of {target:\"dgx\"}>"}'

2. The ssh plugin (which the kernel knows nothing about) does:
     RosterList() -> dgx: address=100.115.27.55, os=linux, arch=arm64, ALIVE
       ^ ONE address. The Tailscale IP. Never DNS -- BOTH Macs' MagicDNS is broken
         and `ssh dgx` silently goes over the home LAN instead. [MEASURED-24]
     LookupGet(channel="ssh.hostkeys", key="dgx")
       ^ written ONLY by dgx, about itself. Single-writer-by-construction: no
         conflict is possible, so no resolver exists.
     LookupGet(channel="ssh.reachability", key="gb-mbp->dgx")
       ^ written by THIS box's ssh plugin, which continuously runs a READ-ONLY
         verify (open a TCP conn, compare the host key). Zero credentials.

3. Returns a VERIFIED command, not a guess:
     ssh -o UserKnownHostsFile=~/.fleet/known_hosts gb@100.115.27.55
     # verified from this box 40s ago; host key matches dgx's own published entry

4. If verification FAILS, it returns the failure and a PROPOSED fix:
     ssh gb-mbp->dgx FAILED 40s ago: no matching host key.
     Proposed: fleetctl ssh approve dgx     <- A HUMAN RUNS THIS. Always.
```

**Three hard lines, and they are the same line:** private keys never move; the daemon never edits `authorized_keys`; `approve` is always a human typing a command. **The daemon does the tedious part; the human keeps the decision that can hurt.**

**Phase 0 beats all of it, and it is a human decision:** run `tailscale set --ssh` on the dgx and grant it in the ACL. That **deletes most key distribution outright** — the highest-leverage fix available and it is five minutes in an admin console. Keep plain SSH as the fallback, because nobody verified Tailscale SSH survives a control-plane outage.

> **A note that is evidence, not colour:** BRIEF §4's stated problem **happened live to this research three separate times.** Three independent agents hit `Host key verification failed` reaching `gb-mac-mini` and could not measure it. One eventually got in with `-o StrictHostKeyChecking=accept-new` [MEASURED-22; I re-confirmed both boxes this session]. **The address-book problem blocked the investigation into the address-book problem.** That is about as direct a confirmation of the premise as one could ask for.

---

## 9. Conflicts resolved

The five designs were written independently and genuinely disagreed. Papering over these would be the §9 failure mode repeating. **Rows 1–3 I settled with my own measurements rather than by picking a favourite.**

| # | Conflict | Options | **Verdict** | Why the loser lost |
|---|---|---|---|---|
| 1 | **Roster membership** | `memberlist` (20, 13) vs hand-rolled static list + probing (23) | **Hand-rolled, ~400 lines** | **[VERIFIED-HERE]** I read `memberlist@v0.6.0/state.go:1211-1219` myself: `k := SuspicionMult - 2` (=2), then `if n-2 < k { k = 0 }` — **at n=3, `1 < 2`, so k=0**, and `suspicion.go:72-75` takes the flat minimum. **SWIM's suspicion-confirmation machinery — the timed "let peers vouch before I declare you dead" step, a big reason you import the library — collapses to its floor on this fleet.** *(Honest scope, per the critic: SWIM's* indirect *probing — asking a third box to try B when A→B fails — does still engage at n=3. We forgo it on purpose: BRIEF §5 wants "can **I** reach B", a per-observer fact, not "does the fleet think B is up." §4.5 makes that argument on its own merits; it does not rest on the suspicion finding.)* You would pay 102 modules, a second network stack, `UDPBufferSize: 1400` on a **1280-byte MTU** link, and two `panic()` sites on metadata size (`memberlist.go:460,519` — all verified by me) for machinery that no-ops. 23's prototype **measured 6.5s detection vs memberlist's 5.7–6.6s** — matched to noise. Its killer argument: 13's own #1 open question (the untestable 8-hour sleep, where peers *reap* the entry) is **structurally impossible** here — `grep 'delete('` returns nothing; a 60-interval freeze rejoined in 99ms. **An 8-hour and a 3-second absence execute identical code.** *Honest swap-out point: past ~20 boxes, throw this away and use memberlist, where indirect confirmation actually engages.* |
| 2 | **Liveness states** | `STATE_LEFT` + `STATE_DEAD` as siblings (20) vs `UP/SUSPECT/DOWN/UNKNOWN` + reason (23) | **23's shape** | "Left" is **not mechanically detectable**, so a state depending on a shutdown hook is **silently wrong exactly when the hook didn't fire** — the case you built it for. Decisive evidence: memberlist's own `NotifyLeave` reports `State=0` (Alive) for a *graceful leave* [MEASURED-13] — **you cannot read the state to tell left from dead.** Departure becomes `DEAD(reason=ANNOUNCED_SLEEP)`: an Intent we were *told* plus a Reason we *inferred*. **[MEASURED-HERE]** this is the one change that is genuinely *breaking* (`buf breaking` flagged the enum renames), which is precisely why it must be settled **before** v1 ships. |
| 3 | **`JournalAppend` has no durability knob (F9)** | ship as-is (20) vs add `bool sync` (24) | **Add `bool sync`** | 24 is right and it is **blocking**. The kernel's own comment says *"durability is a plugin decision"* while the API offered **no choice** — and store-and-forward is only correct if the message is on disk before Send returns. Per-call, **never** a daemon-side policy map: harmonik's hand-maintained `fsyncBoundaryEventTypes` map drifted and **silently downgraded durability** [MEASURED-15]. **[MEASURED-HERE]** `buf breaking --against` at PACKAGE: **this change is additive and passes clean. It is free.** |
| 4 | **Transport product** | ZeroMQ (Greg's lean) vs mangos/NNG (10) vs embedded core NATS (11, 20, 21) | **Embedded core NATS, JetStream off** | See §6. ZeroMQ **cannot cross-compile to the dgx** [MEASURED-10] and **cannot signal "nobody is listening"** by its own Guide's admission. mangos is the real runner-up and loses because **its own argument** — "the log is the project, the transport is small" — **means the tiebreak is everything else**, and NATS has fleet-wide interest, cross-box queue groups, and real-Tailscale evidence. Two designs converged on NATS from independent evidence. |
| 5 | **Kernel→plugin data push** | `PluginService.Deliver` (22) vs plugin calls `Subscribe` (20) | **BOTH, split: the manifest DECLARES, `Subscribe` ATTACHES** | Neither doc had both halves. 20 is right that all data must flow plugin→kernel — *that is exactly what makes the plugin API and the REST API the same service.* 22 is right that a subscription must be **kernel state**, or a reload becomes a re-subscribe. **Synthesis:** the manifest declares the interest (kernel-held, survives reload, so the kernel keeps a small bounded buffer during the ~10ms swap); `Subscribe` is merely the running process attaching to it. `PluginService` stays at **4 lifecycle methods**. |
| 6 | **Is `no responders` a free ACK?** | "free / zero lines" (11, 15) vs "a hint" (21) | **A HINT. Never the ACK.** | 11 and 15 tested with `Shutdown()` — **a killed process says goodbye; a sleeping laptop does not.** Under a blackhole (BRIEF §1's *normal* state) the sender gets an ambiguous timeout for **61 seconds** [MEASURED-21], root-caused in source first. **Anyone wiring this in as "the ACK system" ships a loss bug that appears every day the laptop sleeps.** Fix: `PingInterval=2s` → 5.0s, and the real ACK is the plugin's own (§4.3). |
| 7 | **Channel types** | 5 with fanout=broadcast vs 5 with fanout=scatter-gather (21) vs 4 (20, 24) | **4** | Both readings of "fanout" are **already shipped** (broadcast=PUBSUB, work-queue=POINT_TO_POINT). Scatter-gather is a plugin (publish + collect with a deadline). Two methods — a kernel author's primitive analysis and a consumer tally across three real plugins — reached the same answer. **Tell Greg his list came back shorter and why.** |
| 8 | **Storage namespace** | explicit field + token enforcement (22) vs no field at all (20) | **No field.** | **Unrepresentable beats enforced.** With no `namespace` field there is nowhere to put the wrong value; the kernel derives it from caller identity. 22's `if_revision` CAS survives — it is already in 20's `KVPut`. |
| 9 | **Two gossip systems** | NATS routes + memberlist (20) vs NATS routes + probe loop (23) | **NATS routes + probe loop** | 20 flagged this as its own smell. Resolving row 1 dissolves it: **one gossip system** (NATS routes, for data) plus ~400 lines of dedicated probing (for liveness). Fate isolation is preserved and the second *stack* is gone. |
| 10 | **LOOKUP channel vs roster "state slots"** | two primitives (20 vs 23) | **LOOKUP channel** | 23 conceded these are *"almost certainly the same thing under a different name"* and stated *"I have no attachment to my names."* LOOKUP is Greg's own word (*"publish/lookup table"*) and is already in the proto. **Ship one primitive, not two.** |
| 11 | **Envelope shape** | `substrate.type.v1` w/ `origin_agent`, `tags` (22) vs `fleet.kernel.v1` (20) | **20's** | 22's `origin_agent` **fails 20's own boundary test** — the kernel may not name an agent. **[MEASURED-HERE]** the reconciled proto passes: `VOCABULARY CLEAN`. BRIEF §4's *"the machine, time, etc"* is covered by kernel-stamped `origin_node`/`origin_time` + `headers`. |
| 12 | **`buf breaking` level** | `WIRE_JSON` (buf's own recommendation) vs `PACKAGE` (12, 14, 20, 22) | **PACKAGE** | Reproduced independently on **two different schemas**: **WIRE_JSON silently allows deleting an entire RPC method**, because deleting a method breaks no *encoding*. For a plugin API **the service surface IS the contract.** Cost, taken deliberately: at PACKAGE the contract is **append-only** — deleting a field is breaking *even when correctly `reserved`*. |
| 13 | **Plugin APIs aren't curl-able (F1)** | descriptor registry in the kernel (24) vs accept bytes | **Accept bytes now; a `gateway` plugin later** | 24's finding is real: `payload` is `bytes`, so you curl the kernel with a base64 blob. But a kernel descriptor registry **destroys the property that makes the boundary unbreakable** (§3). And note the exact quote: *"**could be cool** because the same interfaces could be available through REST"* — a nice-to-have. The clause that **is** a requirement (*"define it COMPLETELY independently of any code"*) is **fully delivered today**. Fix later, out of the kernel: a plugin publishes its own descriptor set (opaque bytes to the kernel) and a gateway plugin transcodes JSON↔protobuf. **[MEASURED-HERE]** adding that manifest field later passes `buf breaking` at PACKAGE — **so deferring costs nothing.** |
| 14 | **Payload limit invisible (F7)** | hardcode vs expose | **Expose `max_payload_bytes` in `InfoResponse`** | Plugins must **read** the limit, not guess about a transport they don't know. A 309KB line is measured real [MEASURED-24] against NATS's 1MB cap [CONFIRMED-CODE-21, `const.go:94`]. **[MEASURED-HERE]** additive; passes clean. |

---

## 10. What we deliberately do NOT build

Written down so it does not creep back in.

| Not built | Why |
|---|---|
| **JetStream / Raft / consensus / quorum** | 2-of-3 quorum means losing the laptop + one box stops writes dead. BRIEF §6 disclaims CAP rigor. **`JetStream: false` is a guarded, tested invariant — not a default.** |
| **A fleet-wide message order** | A global order needs a single writer; a single writer is a fatal hub; §4 forbids fatal hubs. **This is a refusal, not an omission: with no API for it, no plugin can accidentally depend on one.** Every sequence is per-`(node, channel)` and the field is named `origin_seq`, not `seq`, so the mistake is visible at the call site. *(This also fixes a measured harmonik bug: `bytes.Compare` over UUIDv7 ids in 4 load-bearing places sorts by clock skew, not causality.)* |
| **Kernel-side queuing, retry, or redelivery** | At-most-once, always. Durability is the plugin's (§4.2). |
| **Storage replication** | *"If you want data on another box, publish it on a channel."* |
| **Overlap / conflict detection** | BRIEF §6: *"I really dont even care about that."* **Not now, and not disguised as anything else** — this is what ate the last round. |
| **RAG / vector search / embeddings** | BRIEF §6. Later, downstream of the backbone. |
| **Search (for now)** | BRIEF §4: later, **and it is a consumer**. It attaches by reading per-node journals. Its only kernel dependency: `origin_node` and `origin_time` ride on every envelope, kernel-stamped. **The kernel must never gain a search-shaped field.** |
| **mTLS / NATS accounts / NKeys / per-plugin ACLs** | Theatre inside WireGuard on 3 owned boxes with one tenant (§6.4). |
| **`code_change/3` / state migration across reload** | **Impossible in Go.** Not hard — impossible. Put state in the kernel. |
| **Kernel live reload** | Not achievable. **Keep the kernel small instead.** Advisory tripwire: ~3000 lines. `ReattachConfig` exists if it ever becomes urgent. |
| **Byte-sync of plugin binaries** | Sync **records**; fetch and verify locally. The fleet is 2 platforms; a darwin binary on the dgx is garbage. |
| **Auto-upgrade of enabled plugin versions** | **The one power that could break all three boxes at once.** Replication *adds* records and *offers* versions; a human repoints. |
| **A supervision-tree DSL** | Telegraf has one knob (`restart_delay`) and it's been fine for years. One knob. |
| **`GRPCBroker` for the host API** | It would create a second, private, plugin-only host API — a drift surface. **One surface.** |
| **A `Log` RPC** | go-plugin already streams the plugin's stdout/stderr. `fmt.Println` is the API. |
| **Plugin-to-plugin addressing** | Route by channel name. The only routing model that survives a machine boundary unchanged. |
| **Bidirectional streaming, anywhere** | Deliberate. Bidi needs HTTP/2 trailers; avoiding it lets the **same** service answer plain `curl` over HTTP/1.1 **and** a plugin over gRPC. That is why request/reply's responder side is `Serve` (stream) + `Respond` (unary), not the obvious bidi stream. §3.2 satisfied by construction. |
| **WASM / Yaegi / stdlib `plugin`** | §7.3. WASM is banked, not foreclosed — same `.proto`. |
| **Tailnet auto-discovery of fleet members** | The tailnet has 8 devices, two named `localhost`. Config-file allow-list. Membership *discovery* is not a requirement. |
| **A fifth channel type ("fanout")** | Both readings already ship (§4.1). |

---

## 11. Open questions, ranked by how much they'd change the design

**1. Does any of this survive a REAL macOS sleep?** ⚠️ **Do this before writing kernel code. Half a day. It is the top risk in three separate designs.**
Nobody has closed the lid. Design 21 built a blackhole proxy (sockets stay open, bytes vanish) — *"I narrowed this open question, I did not eliminate it."* Design 23 froze an HTTP handler; investigation 13 used `SIGSTOP`. **None is a real sleep**, where `utun0` goes down and the Tailscale IP is withdrawn. Unknown: whether macOS sends RSTs on wake (detection *faster* than the modelled 5s) or leaves peers hanging (matches what was measured). **BRIEF §1 says the laptop sleeping is the fleet's NORMAL state.** **One overnight test serves all three subsystems** — it would validate or move `PingInterval`, the probe thresholds, and the `Interest` staleness bound at once.

**2. LOOKUP replication is the only real code with no upstream to lean on.** ~150–250 lines, and **everything routes through it** (name resolution, host keys, agent registry). Core NATS needs JetStream for a KV (disqualified); mangos lacks it; ZeroMQ's Clone pattern exists in five languages, not Go. **Sharpest edge, and it is [DEDUCED], not built:** read-repair is sound *only* if a writer never re-issues a `(writer, revision)` pair with a different value. That requires the revision counter to survive restarts **and** a disk wipe to force a fresh incarnation. **Wipe a node's disk, restart it at revision 1, and nodes can converge on wrong values.** Mitigation specified (persist the counter; force a fresh incarnation on loss), unbuilt — **but the incarnation mechanism already exists elsewhere in this design and neither the kernel doc nor the roster doc noticed.** The roster mints a random `boot_id` on every daemon start (§4.5's restart detection, built in M2) for exactly this reason: *a counter that resets to 1 on restart makes peers hold stale state forever.* Same bug, same fix. **Adopt the roster's `boot_id` as LOOKUP's incarnation:** the writer identity becomes `(writer_node, boot_id, revision)`; persist the counter in SQLite; if the store is missing or empty on boot, the fresh `boot_id` alone forces convergence. That is ~10 lines wired into code already being written, not a new subsystem.

**3. Does `PingInterval=2s` flap under packet loss?** It is ~300× the measured 4–7ms RTT so it should be very safe **[DEDUCED]** — but route flapping on a lossy link was not tested. `gb-mac-mini` shows a **12× RTT spread (7.7ms → 92ms, stddev 25.5ms) at 0% loss** [MEASURED-23], cause unknown (probably WiFi power-save). It does not threaten the 1s probe timeout (11× headroom). If routes flap, raise it; the stale window scales linearly at ~3× the interval.

**4. Does a plugin binary fetched over HTTP still pre-warm?** macOS may attach `com.apple.quarantine` to it, which Gatekeeper may treat **worse than 500ms — possibly a hard refusal**. The pre-warm trick is measured only for a locally-built binary. **The most important untested thing in the plugin-library plan, and it is cheap to check.**

**5. Does "fanout" mean broadcast or work-queue?** Both readings ship (§4.1), so **this blocks nothing** — but Greg listed five types and gets four, and he should hear it from us. **30 seconds of his time.**

**6. Can a leaking plugin be bounded?** Process isolation protects the *daemon*; it does **not** bound *memory*. A leaking plugin OOMs **the box** — and on the dgx that box also runs vLLM. `RLIMIT_AS` via `exec.Cmd.SysProcAttr` **[DEDUCED to exist; untested]**, and its semantics differ between Linux and macOS. **Mitigation now: observe RSS and report it. Build the knob when a real plugin has a real leak.**

**7. What is the right `Deliver` deadline?** 30s is suggested **by analogy, with no measurement behind it.** Needs a real plugin's real latency distribution, per channel.

**8. Does `buf breaking` at PACKAGE erode?** The append-only rule is right, but nobody has lived with it. If it blocks reasonable cleanups, the fallback is PACKAGE for the contract and WIRE_JSON for internal messages — **untested**.

**9. Is `thread_id`/`in_reply_to` a sufficient causality story for a human reading a cross-machine conversation?** Per-node ordering is **[DEDUCED] by two authors and [MEASURED] by neither. Independent agreement is not measurement.**

**10. Does the WireGuard source-IP argument hold?** The probe port has no shared secret, justified by *"WireGuard binds source IPs to node keys, making it an authenticated identity check"* **[DEDUCED, unverified, load-bearing]**. If wrong, add a pre-shared key to the probe.

---

## 12. A note on method

BRIEF §9 asked us to avoid a specific failure: competent work on a false premise, stated with false confidence. Two things are worth recording because they are evidence the process worked rather than reassurance that it did.

**The designs caught themselves being wrong.** Design 21 hypothesised a macOS firewall was blocking `gb-mac-mini`, checked, and found *"Firewall is disabled"* — hypothesis wrong, discarded. It was then one step from reporting *"the dgx's ssh is flaky, which is BRIEF §4 happening live"* after five failures — **the real cause was its own `pkill -f` matching the shell that ran it.** Neither the fleet nor the firewall was at fault. Design 20's own boundary test shipped a false negative and it nearly reported a fix that never happened: **BSD `sed` silently ignores `\b`, so a rename no-op'd with exit code 0**, and the test printed `VOCABULARY CLEAN` on a file with three banned identifiers. It was caught only by grepping the artifact instead of trusting the exit code. **Two rules came out of that and both are kept: a test that has never failed on a real violation is decoration, and you verify the artifact, not the tool's exit code.**

**That is why this document re-ran the load-bearing claims rather than citing them.** The three conflicts that most needed a tiebreak — memberlist's `k=0`, whether the F9/F7 fixes are free, and whether the reconciled proto is real — were settled by reading the source and running the tools **here**, not by preferring one sibling's prose over another's. The result changed the outcome twice: the `bool sync` and `max_payload_bytes` fixes are provably free, and the liveness-state change is provably breaking — which is what turns "settle this eventually" into "settle this before v1 ships."

---

## 13. Amendments (post-critique)

An adversarial critic reviewed the five subsystem designs and this document (`design/29-critique.md`). This section records what its review changed here and, where it overstated, why the text stands. The verdicts below were re-checked against the artifacts this session, not taken on the critic's word.

**Fixed — a BRIEF §9 violation, and it is not negotiable:**

- **The fabricated Greg quote is struck (§6.1, was line 360).** Two subsystem docs (`20-kernel.md:86`, `21-transport.md:91`) justified overruling Greg's *"I strongly lean away from NATS"* by telling him *"a broker on every node"* was **his own idea**, in quotation marks. I re-verified the critic's finding: `grep -in "broker" BRIEF.md` returns **nothing** — the word is not in the brief, and the brief's header says *"Where quoted, the words are his."* The phrase originates uncited in `investigate/11:45` as "his own middle-path option #4"; there is no option #4 (it is heading #1 of investigation 11's *own* list). This is exactly BRIEF §9's failure mode — framing not in the brief promoted to load-bearing — and it landed on the highest-stakes decision, converting "I am overruling you" into "I am agreeing with you." **This document reproduced it once, at §6.1.** It now says the honest thing: the *requirement* ("no box's death may kill the system") is Greg's and is binding; the NATS *preference* is his and we are overruling it on measurement; he gets to reject the override. The technical case for core NATS is measured and survives deletion of the quote intact — which is what made the quote gratuitous as well as wrong.

**Already fixed before the critique landed — verified this session:** The critic found four other BRIEF violations in the *subsystem* docs (20–24). All four were already resolved in the reconciled proto (`design/25-reconciled-proto/`) that this architecture ships, and I re-confirmed each on disk today:

| Critic's finding (in docs 20–24) | State in the shipped contract |
|---|---|
| `origin_agent` — a domain concept in the kernel (doc 21) | **Absent.** `grep origin_agent` → 0 hits; boundary test → `VOCABULARY CLEAN`. |
| `DurabilityClass` / a `SendReliably` in disguise (doc 21) | **Absent.** No durability enum on `Publish`; at-most-once is structural (§4.2). |
| `JournalAppend` has no `sync` knob (F9) | **Present.** `bool sync = 3` / `bool synced = 2` are in the proto; `buf breaking` confirms additive. |
| `namespace` on the wire in storage RPCs (doc 22) | **Absent.** No `namespace` field on any KV/Journal RPC; the kernel derives it (§4.4). |

**Adopted — the critic sharpened a claim:**

- **§9 row 1 no longer overstates SWIM.** The critic is right that only SWIM's *suspicion-confirmation* collapses at n=3; *indirect probing* still engages with one helper node. The row now says exactly that, and moves the real justification for hand-rolling onto per-observer liveness (§4.5), where it belongs.
- **§11 open question 2 (the LOOKUP disk-wipe hole) is downgraded from "unbuilt subsystem" to "~10 lines."** The critic noticed that the roster's `boot_id` — already built in M2 — is the incarnation mechanism the kernel doc described as missing. Recorded in §11 and `ROADMAP.md` M3.

**Rebuttals — where the critic's point does not change this document:**

- **The critic's contradiction-resolutions (three kernel APIs → doc 20's; push vs pull → pull; three failure detectors → doc 23's prober; FANOUT cut; STATE_LEFT cut) were *already* the verdicts in §9 and §10 before the review.** The critique was written against the five subsystem inputs, several of which this architecture had already superseded. So most of its "resolutions" are agreements with decisions already shipped here, not corrections. They are not re-litigated.
- **The critic notes doc 24's F10 is dead and its F9 citation is stale.** That is a fact about the subsystem doc's line references, not about this architecture or the shipped proto (which has the `sync` field at a live line). No change here.
- **The `Interest` proto comment.** The critic wanted doc 20's comment to stop overselling `Interest` as answering §5. The shipped proto already carries the correcting line — *"It says 'someone is listening', NOT 'it was delivered'"* — and §4.3 spends a full subsection making `Interest` a hint and never the ACK. Left as is.

---

## Sources

**Project files read in full:**
- `/Users/gb/research/2026-07-15-agent-substrate-v2/BRIEF.md`
- `/Users/gb/research/2026-07-15-agent-substrate-v2/design/22-plugin-system.md` (1043 lines)
- `/Users/gb/research/2026-07-15-agent-substrate-v2/design/20-kernel-proto/fleet/kernel/v1/kernel.proto`
- `/Users/gb/research/2026-07-15-agent-substrate-v2/design/20-kernel-proto/fleet/kernel/v1/plugin.proto`
- `/Users/gb/research/2026-07-15-agent-substrate-v2/design/20-kernel-boundary-test.sh`

**Project files read in part (plus the full structured summaries of each supplied to this synthesis):**
- `design/20-kernel.md` · `design/21-transport.md` (§0–1, headline measurements) · `design/23-roster-addressbook.md` (§1.2 memberlist source analysis, sources) · `design/24-validation-plugins.md` · `investigate/10-zeromq.md` (§0–2) · `investigate/13-roster-and-addressbook.md` (suspicion/config analysis)

**Library source read directly this session (verifying a load-bearing claim two docs disagreed about):**
- `~/go/pkg/mod/github.com/hashicorp/memberlist@v0.6.0/state.go:1205-1225` — `k := m.config.SuspicionMult - 2`; `n := m.estNumNodes()`; `if n-2 < k { k = 0 }` — **confirmed: k=0 at n=3**
- `~/go/pkg/mod/github.com/hashicorp/memberlist@v0.6.0/suspicion.go:66-78` — `timeout := max; if k < 1 { timeout = min }` — **confirmed**
- `~/go/pkg/mod/github.com/hashicorp/memberlist@v0.6.0/config.go:312-336` — `IndirectChecks: 3`, `SuspicionMult: 4`, `ProbeInterval: 1s`, `PushPullInterval: 30s`, **`UDPBufferSize: 1400`** (vs this network's 1280 MTU) — **confirmed**
- `~/go/pkg/mod/github.com/hashicorp/memberlist@v0.6.0/memberlist.go:460,519` — **two `panic("Node meta data provided is longer than the limit")` sites** — confirmed

**Commands run this session:**
- `ifconfig utun0` → **`mtu 1280`**, `inet 100.87.151.114` (matches BRIEF §1's gb-mbp)
- `buf --version` → **1.71.0**; `go version` → **go1.26.2 darwin/arm64**; `ls ~/go/bin` → `buf`, `grpcurl`, `protoc-gen-go`, `protoc-gen-connect-go`, `protoc-gen-go-grpc` all present
- `buf lint` (STANDARD) on `design/20-kernel-proto` → **rc=0**; `buf build` → **rc=0**; `20-kernel-boundary-test.sh` → **`VOCABULARY CLEAN`**
- Reconciled proto (`design/25-reconciled-proto/`): `buf lint` → rc=0; `buf build` → rc=0; boundary test → `VOCABULARY CLEAN`; `buf generate` → 4 Go files incl. ConnectRPC handlers; `go build ./...` → **BUILD OK** (darwin/arm64); `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...` → **CROSS-COMPILE OK** (the dgx)
- `buf breaking --against '.git#ref=HEAD'` at **PACKAGE**, reconciled vs original → **`bool sync`, `bool synced`, `max_payload_bytes`, and `Liveness.Reason` all pass clean (additive/free); only the deliberate `STATE_LEFT`→`STATE_UNKNOWN` and `KIND_LEAVE` enum changes are flagged breaking**
- `ssh gb@100.115.27.55` → `Linux 6.17.0-1026-nvidia aarch64`, 20 cores, **`3.2T` free** (matches BRIEF §1)
- `ssh gb@100.120.22.74` → **`Darwin 25.3.0 arm64`** (confirms design/22's platform finding; the fleet is exactly 2 artifacts, not 4)

**Artifacts written:**
- `design/25-reconciled-proto/fleet/kernel/v1/{kernel,plugin}.proto` + `buf.yaml` — **the authoritative interface.** Verified lint-clean, build-clean, boundary-clean, generates Go+ConnectRPC, cross-compiles to both fleet platforms.

**URLs fetched:** none. Every external claim is re-cited from the investigation that fetched it, labelled `[CLAIMED-nn]` or `[MEASURED-nn]` at the point of use.
