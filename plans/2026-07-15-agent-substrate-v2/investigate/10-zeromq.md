# ZeroMQ — the honest deep dive

**Date:** 2026-07-15
**Question:** BRIEF §7.1 — *"ZeroMQ — does it make sense? Asked twice, never answered."*
**Method:** Read the ZeroMQ Guide's own source text and reference code; fetched every candidate library from GitHub's API for hard dates; built and ran real Go programs against all three libraries on this laptop. Everything labelled **[MEASURED]** I ran. Everything labelled **[CLAIMED]** is someone else's assertion. Everything labelled **[DEDUCED]** is my inference and could be wrong.

---

## 0. The verdict up front

**The ZeroMQ *idea* is right for this. The ZeroMQ *library* is the wrong way to get it.**

Three findings drive that, all measured:

1. **ZeroMQ's socket-pattern model is a genuinely close fit** to BRIEF §3.1's "a channel has a name and a type; the daemon does transport; the plugin does logic." Not superficially close — structurally close. Greg's instinct here is good. (§6 below.)
2. **ZeroMQ's Go story is bad in a way that specifically hurts this fleet.** The mature binding (`pebbe/zmq4`) is a C wrapper that **cannot cross-compile** — I tried, it fails [MEASURED, §4]. The pure-Go one (`go-zeromq/zmq4`) has a README that literally says *"zmq4 needs a caring maintainer"* [MEASURED, §4]. And `libzmq` itself has gone **2.8 years without a release**, with commits collapsing from 167/yr to 9/yr [MEASURED, §4].
3. **NNG's protocols, via the pure-Go `mangos` library, deliver the same idea plus two patterns that map onto Greg's requirements that ZeroMQ does not have** — SURVEY (fleet roll-call → §3.1's machine roster) and BUS (peer mesh → §4's not-centralized). I ran all of them, first try, and cross-compiled a static binary to every box in the fleet [MEASURED, §5].

**Recommendation: use `go.nanomsg.org/mangos/v3` as the kernel's transport, not ZeroMQ.**

But the headline finding is bigger than which library wins, and it's in §7:

> **The transport choice is the small decision.** Whichever you pick, ZeroMQ and mangos both give you *exactly the same thing*: bytes that move, and nothing written down. BRIEF §5 wants messages written down. BRIEF §4 wants no central hub. **ZeroMQ's own answer to durability requires a central broker** — which violates §4. The way out is a per-node durable log, which you write yourself, and which is roughly the same amount of code on either library. That log is the actual project. Pick the transport that costs least to *operate*, then go build the log.

---

## 1. What ZeroMQ actually is

**Plain version: ZeroMQ is a library that makes one socket act like a whole messaging pattern.**

Some definitions first, because these terms get thrown around:

- **Socket** — the normal way one program talks to another over a network. Think of it as one end of a pipe. Normally a socket is dumb: you shove bytes in one end, they come out the other, and *everything* else — who's on the other end, what happens if they're gone, how to find them again — is your problem.
- **Broker** — a separate server program that sits in the middle and holds messages. RabbitMQ, Kafka, and NATS are brokers. You run it, you babysit it, and everything flows through it. It's the post office.
- **Library** — code that runs *inside your program*. No separate process, nothing to run, nothing to babysit.

**ZeroMQ is the library, not the post office.** There is no ZeroMQ server. You don't install a ZeroMQ. You `import` it, and your program's sockets get smarter.

What "smarter" buys you, concretely — a normal TCP socket connects to exactly one peer, dies when that peer dies, and stays dead. A ZeroMQ socket:

- can be connected to **many peers at once**, and treats them as one thing;
- **reconnects automatically** and silently when a peer goes away and comes back;
- lets you `bind` (wait for others) or `connect` (go find someone) **in either order** — you can connect to something that doesn't exist yet and it just waits;
- sends **whole messages**, not a byte stream — you never write framing code, never parse a length prefix;
- has a **built-in behaviour** — a *pattern* — for what "send" means when there are three peers. Round-robin? Everyone? Whoever asked?

That last one is the whole point. **The pattern is baked into the socket type.**

### What follows from "library, not broker"

This is the part that matters for BRIEF §4, and it cuts both ways.

**The good, and it's exactly what Greg wants:** there is no hub. No box is special. No box's death kills the system. When Greg says *"I dont trust my boxes... I strongly lean away from NATS... with ZeroMQ we can create more of a distributed system"* — he is right, and this is precisely why. NATS wants you to run NATS servers. ZeroMQ wants you to run nothing. His instinct is sound.

**The bad, and this is the part nobody tells you:** *a broker is not just overhead — a broker is where the messages live.* Take the post office away and you haven't just removed a bottleneck, you've removed **the floor the letters sit on**. With no broker there is nowhere for a message to wait. If the receiver isn't there right now, the message is not "queued" — it is *gone*, or it's sitting in RAM in the sender's process waiting to be lost on restart.

So "brokerless" is not a free win. It is a **trade**: you give up the hub, and in exchange you inherit the hub's job. Every single thing the broker was quietly doing — remembering messages, knowing who's online, retrying, acknowledging, letting a latecomer catch up — is now **your** code.

**[DEDUCED]** This is the single most important thing to understand about ZeroMQ before choosing it, and it is why §3 of this document is the crux rather than a footnote.

---

## 2. The patterns

A ZeroMQ socket type is one half of a pattern; they're used in fixed pairs. Two bits of jargon you need first:

- **HWM (High Water Mark)** — the size limit on a socket's internal queue, measured in messages. Default is 1,000. **What happens when it's full is different per socket type, and this is where the bodies are buried.**
- **Late joiner** — a peer that connects *after* messages were already sent. The question "what does a late joiner get?" has one answer in ZeroMQ across the board: **nothing**.

### The six patterns

**REQ/REP — request/reply.** The classic: client asks, server answers. REQ is a **strict lockstep state machine** — send, receive, send, receive. Call `send` twice in a row and you get an error, not a queued message.

> **Sharp edge — this is the famous one.** If a REQ socket sends and the server dies before replying, that REQ socket is **permanently wedged**. It will not let you send again (it's waiting for a reply) and no reply is coming. Not an error — a hang. There is no timeout. There is no reset. The only fix is to **destroy the socket and build a new one**, which is exactly what the Lazy Pirate pattern (§3) exists to do. Martin Sustrik, ZeroMQ's own author, later cited "stuck REQ sockets" as a design mistake he set out to fix in nanomsg [CLAIMED — nanomsg.org/documentation-zeromq.html].

**DEALER/ROUTER — async request/reply.** The grown-up version. DEALER is REQ without the lockstep (round-robins out, fair-queues in). ROUTER is REP that tells you *who* sent each message, via an identity frame it prepends, and lets you address replies to a specific peer. **Everything serious in ZeroMQ is built from these two.** Sharp edge: ROUTER **silently discards** a message addressed to an identity it doesn't know. Typo a peer name and your message evaporates with no error. The Guide lists this as one of its four named message-loss cases.

**PUB/SUB — broadcast with topic filtering.** Publisher sends, every subscriber whose topic filter matches gets a copy. Filtering happens on the subscriber side in modern ZeroMQ.

> **Sharp edges — three of them, and they are severe.**
> 1. **Silent drop.** When a subscriber is too slow and the HWM fills, PUB **throws messages away and does not tell anyone.** Not an error. Not a return code. Nothing. I measured this: **98,906 of 100,000 messages vanished, zero errors returned** [MEASURED, §3].
> 2. **Late joiner.** A subscriber that connects late gets **nothing** that came before. Measured: 10 messages sent before connect, subscriber received zero [MEASURED, §3].
> 3. **Publishers are blind.** The publisher cannot see its subscribers *at all*. This one kills a stated requirement — see the quote in §3.

**XPUB/XSUB — plumbing for pub/sub.** Same as PUB/SUB but subscriptions are visible as real messages you can read, so you can build a forwarder in the middle. **XPUB is the only way a publisher can learn a subscriber exists** — it sees subscribe/unsubscribe events. Sharp edge: by default XPUB deduplicates subscriptions, so you see the *first* subscriber for a topic and not the rest; you need `ZMQ_XPUB_VERBOSE` to see all of them. The Guide notes its own example "sneakily gets around this by using random topics so the chance of it not working is one in a million" [zguide chapter5.txt:134].

**PUSH/PULL — pipeline / work distribution.** PUSH round-robins messages across connected PULLs; each message goes to **exactly one** worker. This is a work queue, **not** broadcast. Sharp edge: PUSH **blocks** when the HWM fills (it does not drop — unlike PUB). If one worker stalls, its share of the queue backs up and eventually the whole PUSH socket blocks, stalling workers that were perfectly healthy.

**PAIR — exclusive one-to-one.** Exactly two peers, no patterning, no reconnect semantics. The Guide steers you to use it only for coordinating threads inside one process.

### Mapping to the brief's wanted menu

BRIEF §3.1: *"the daemon could provide say a handful of transports (publish/lookup table, point to point, pubsub, fanout, whatever)"* plus request/reply, explicitly wanted.

| Brief wants | ZeroMQ gives | Honest assessment |
|---|---|---|
| **request/reply** | REQ/REP, or DEALER/ROUTER | ✅ Real. But use DEALER/ROUTER — REQ's wedge bug makes it unusable for a daemon that must stay up. |
| **point to point** | PAIR | ⚠️ Nominally. PAIR is documented as an inter-thread tool. Real cross-machine p2p → DEALER/ROUTER. |
| **pubsub** | PUB/SUB | ⚠️ Works, but silently drops and has no late-joiner story. Conflicts with §5. |
| **fanout** | PUSH/PULL | ⚠️ **Name collision — read carefully.** "Fanout" usually means *everyone gets a copy*. PUSH/PULL means *exactly one worker gets it*. If Greg means "everyone gets it," that's PUB/SUB, not PUSH/PULL. **Worth confirming which he meant.** |
| **publish/lookup table** | ❌ **Nothing** | The Clone pattern in the Guide — and it's **C-only**, ~799 lines, and needs Binary Star failover underneath [MEASURED, §3]. |
| **machine roster** (§3.1, "baked in") | ❌ **Nothing** | No presence, no discovery, no membership. Entirely yours to build. |

**Two of the six things asked for do not exist in ZeroMQ at all**, and they happen to be the two Greg called out as most core — the lookup table and the roster ("*The machine roster probably should be baked in*").

---

## 3. What you must build yourself — the crux

This is the section that decides the question. **[MEASURED]** unless noted.

### First, the measurement that matters most

BRIEF §5 states the target model in Greg's words:

> *"all the messages are written down. If an agent is subscribed, then they get a notification. If they are polling, then they read their messages the next time they come in."*

I tested whether ZeroMQ-style pub/sub does this. It does the opposite. Source: `10-zeromq-experiments/mangos_durability_loss.go`.

```
=== BRIEF §5 test: are messages 'written down'? ===
A. LATE JOINER : subscriber received NOTHING (receive time out)
   -> all 10 messages sent before it connected are GONE. No replay. No log.
B. SLOW SUB    : published 100000 msgs in 29ms with 0 send errors
   -> PUB never blocked and never errored. Overflow was dropped SILENTLY.
   subscriber actually received 1094 of 100000 (1.1%). first=flood-076913 last=flood-099993
   -> 98906 messages VANISHED with no error, no ACK, no way for the sender to know.
```

Read the `first=flood-076913` carefully. The subscriber didn't even get the *earliest* messages — it got a **random recent window**. 98.9% loss, reported as complete success.

*(This run is `mangos`, but the behaviour is ZeroMQ's design, not a mangos bug — mangos implements the same semantics. The ZeroMQ Guide confirms it in its own words below.)*

### The Guide admits all of it

Direct quotes from the ZeroMQ Guide's source text (cloned repo, line numbers cited):

> **"With ZeroMQ, there are several cases where we may lose messages. One is the 'late joiner' syndrome. Two is when we close sockets without sending everything. Three is when we overflow the high-water mark on a ROUTER or PUB socket. Four is when we use an unknown address with a ROUTER socket."**
> — `chapter8.txt:796`

> **"Stop queuing new messages after a while... it's what ZeroMQ does when the publisher sets a HWM. However, it still doesn't help us fix the slow subscriber. Now we just get gaps in our message stream."**
> — `chapter5.txt:146`

And the one that directly kills a stated requirement. BRIEF §5:

> *"we actaully should probably notify the sender that the receiver is not listening. I believe we can find that out."*

The Guide, on doing exactly that:

> **"Punish slow subscribers with disconnect... It's a nice brutal strategy... and would be ideal, but ZeroMQ doesn't do this, and there's no way to layer it on top because subscribers are invisible to publisher applications."**
> — `chapter5.txt:148`

**"There's no way to layer it on top."** Greg believes the sender can find out the receiver isn't listening. On PUB/SUB, the ZeroMQ Guide says he cannot. It's not hard — it's *not exposed*. (The escape hatch is XPUB, which sees subscribe events, or abandoning pub/sub for DEALER/ROUTER where peers are addressable. Both mean the "pubsub" channel type can't be plain PUB/SUB.)

### ZeroMQ's own answer to reliability — and what it really costs

The Guide's Chapter 4 is entirely about clawing reliability back. I counted the actual Go reference implementations in the Guide's repo. Non-blank, non-comment lines:

| Pattern | What it adds | Go code | vs. baseline |
|---|---|---|---|
| Hello World REQ/REP | nothing — the baseline | **40** | 1× |
| **Lazy Pirate** | client-side timeout + retry | **92** | 2.3× |
| **Simple Pirate** | + load-balancing queue (a broker) | **88** | 2.2× |
| **Paranoid Pirate** | + heartbeating, dead-worker detection | **199** | 5.0× |
| **Majordomo** | + service names, protocol spec, worker API | **519** | **13×** |
| **Titanic** | + durability ("messages written down") | **❌ NO GO VERSION** | — |

**Thirteen times the code to get to Majordomo — and Majordomo still has no durability.** Restart the broker, lose the messages.

### The two facts that decide this

**Fact 1 — ZeroMQ's durability answer is a broker, which violates BRIEF §4.**

Titanic is the Guide's disk-persistence pattern. From `chapter4.txt:672`:

> **"here's the Titanic pattern, in which we write messages to disk... we're going to layer Titanic on top of MDP rather than extend it."**

MDP = Majordomo Protocol. **Titanic is not standalone — it is a worker bolted onto the Majordomo broker.** So ZeroMQ's blessed path to "messages are written down" (§5) is *"run a central broker and hang a disk service off it."* That is a hub whose death is fatal. **That is precisely what §4 forbids, and precisely the mistake the last research round made with NATS-on-the-DGX** (BRIEF §9).

The Guide is explicit that its brokerless option is a *different* pattern with *different* properties:

> **"Of all the different patterns, the two that stand out for production use are the Majordomo pattern, for broker-based reliability, and the Freelance pattern, for brokerless reliability."**
> — `chapter4.txt:1208`

So: **you may have brokerless (Freelance), or you may have durable (Titanic). ZeroMQ does not offer both in one pattern.** Greg's brief asks for both.

**Fact 2 — every pattern that matches this brief is missing in Go.**

I listed the Guide's example directories:

| Pattern | Why this brief needs it | Languages available |
|---|---|---|
| **Freelance** | brokerless reliable req/rep — **§4's answer** | C, C++, C#, Java, Lua, PHP — **not Go** |
| **Titanic** | durability — **§5's answer** | C, C++, C#, Haxe, Java, PHP, Python, Ruby, Tcl — **not Go** |
| **Clone** | shared key-value table — **§3.1's "publish/lookup table"** | C, C++, Java, Python, Tcl — **not Go** |
| **Binary Star** | failover, needed under Clone | C, C++, Java, Python, Ruby, Tcl, GoCZMQ* — **not plain Go** |

\* GoCZMQ is a *different* cgo binding, not `pebbe` or `go-zeromq`.

**The Go examples stop at Majordomo.** Every single pattern that answers this brief's actual requirements — brokerless reliability, durability, the lookup table — exists in nine languages and **not the one Greg said he wants** (§4: *"I'd kinda like to stick with go"*).

For scale, the C versions: `titanic.c` = 208 lines, `clone.c` + `clonesrv6.c` + `bstar.c` = **799 lines**. That's what you'd be porting, in C idiom, from a guide whose Go track ends before it.

### The honest bill

With no broker, here's what the kernel owes you that ZeroMQ does not provide. **[DEDUCED]** — estimates, from reading the reference implementations, not from building it:

| Thing | Brief ref | ZeroMQ gives | You write |
|---|---|---|---|
| Auto-reconnect | — | ✅ **free, and genuinely good** | 0 |
| Message framing | — | ✅ **free** | 0 |
| Many-peers-one-socket | — | ✅ **free** | 0 |
| **Durability** ("written down") | §5 | ❌ nothing | ~300–600 |
| **ACKs** ("notify the sender") | §5 | ❌ nothing (and "no way to layer it on top" for PUB/SUB) | ~150–300 |
| **Message IDs / dedupe** | §5 | ❌ nothing | ~100 |
| **Replay for an agent that was away** | §5 | ❌ nothing | ~200–400 |
| **Discovery** (who exists) | §3.1 | ❌ nothing | ~150–300 |
| **Presence / liveness** | §3.1, §5 | ❌ nothing | ~200–300 |
| **Roster / lookup table** | §3.1 | ❌ nothing (Clone is C-only, 799 lines) | ~300–500 |

**Rough total: 1,400–2,900 lines of hard, stateful, concurrent, easy-to-get-wrong code** — the kind with race conditions you find in month three.

**And here is the punchline: that column is nearly identical whether you choose ZeroMQ or mangos.** Neither gives you *any* of it. So the durability layer is not a tiebreaker between them — **it's the project**, and the transport choice should be made on other grounds entirely (§7).

---

## 4. Go support — verified, not assumed

All **[MEASURED]** via GitHub API and real builds on this laptop (`go1.26.2 darwin/arm64`), 2026-07-15.

### Is libzmq itself still maintained in 2026?

**Barely. It's on life support.**

| Metric | Value |
|---|---|
| Last tagged release | **v4.3.5, 2023-10-09** — **1,010 days ago (2.8 years)** |
| Commits 2021 | 167 |
| Commits 2022 | 109 |
| Commits 2023 | 113 |
| **Commits 2024** | **28** |
| **Commits 2025** | **9** |
| Commits 2026 (to July) | 23 |
| Open issues | 376 |

Commits are still landing (most recent 2026-07-04, a GSSAPI auth fix), so it isn't dead. But **9 commits in all of 2025** and nearly three years without cutting a release is a project coasting. Homebrew still ships 4.3.5 — the 2023 release [MEASURED: `brew info zeromq` → `stable: 4.3.5`].

### `github.com/pebbe/zmq4` — the cgo binding

**cgo** = the mechanism that lets Go call C code. Convenient framing: **the moment you use cgo, your Go program stops being a Go program.** It needs a C compiler, C headers, and the C library present at build time *and* at run time.

| Metric | Value |
|---|---|
| Latest release | **v1.4.0, 2025-05-02** (1.2 years ago) |
| Last commit | 2025-07-06 |
| Stars / open issues | 1,259 / 58 |
| License | BSD-2 |

**This is the healthiest ZeroMQ-in-Go option, and it is the most mature.** Real releases, recent-ish commits, complete API. If you must use ZeroMQ from Go, this is the one.

**But I tried to build it, and here's what happened:**

```
=== TEST 1: build pebbe/zmq4 natively, macOS, libzmq NOT installed ===
github.com/pebbe/zmq4: exec: "pkg-config": executable file not found in $PATH

=== TEST 2: cross-compile pebbe/zmq4 -> linux/arm64 (DGX) from macOS ===
# github.com/pebbe/zmq4
.../pebbe/zmq4@v1.4.0/reactor.go:10:4: undefined: State
.../pebbe/zmq4@v1.4.0/reactor.go:21:16: undefined: Socket
.../pebbe/zmq4@v1.4.0/reactor.go:23:12: undefined: Poller
[...]

=== TEST 3: cross-compile with CGO_ENABLED=0 ===
[identical failure]
```

**Read Test 2 carefully — it's worse than a clean error.** Cross-compiling turns cgo off by default, which makes Go **silently skip every C-backed file** in the package and then fail with "undefined: Socket" — a confusing error that looks like a broken library rather than "you can't do this." That's a genuinely nasty developer experience.

**What this means for this specific fleet** [MEASURED — I checked the actual boxes]:

```
gb-mbp   : Darwin 25.4.0 arm64
dgx      : Linux 6.17.0-1026-nvidia aarch64   (libzmq: present; go: NOT installed)
```

Two operating systems, arm64 both. With `pebbe/zmq4` you **cannot build the daemon on your laptop and copy it to the DGX.** You must install Go + a C toolchain + libzmq-dev on every box, and build on each box. Every plugin that touches the socket API inherits the same constraint. To cross-compile properly you'd need a full macOS→linux/arm64 C cross-toolchain plus a linux/arm64 libzmq — doable, and a genuinely miserable afternoon.

**[DEDUCED]** For a 3-box hobby fleet where the whole *point* is that agents shouldn't *"screw around figuring out the network crap"* (§4), making the build system this fragile is a real, recurring tax — paid every time you touch the daemon.

### `github.com/go-zeromq/zmq4` — the pure-Go implementation

| Metric | Value |
|---|---|
| Description | **"[WIP] Pure-Go implementation of ZeroMQ-4"** — still says WIP after 8 years |
| Latest release | **v0.17.0, 2024-05-16** (2.2 years ago) |
| **Last commit** | **2024-06-18 — over 2 years ago** |
| Stars / open issues | 392 / 31 |

**Its own README, fetched today:**

> **"`zmq4` needs a caring maintainer.**
> **I (`sbinet`) have not much time to dedicate anymore to this project (as `$WORK` doesn't need it anymore)."**

That is the maintainer publicly resigning. **[CLAIMED — but by the maintainer himself, which makes it about as authoritative as it gets.]**

The open-issue list is not cosmetic polish — it's core function, with ages:

```
2024-12-06 #158 Push to TCP not round robining
2022-11-06 #136 Can't get a proxy to work (XSUB/XPUB)
2021-12-06 #115 router node restart recv block
2021-06-24 #108 No messages getting through XPUB/XSUB proxy
2020-11-01  #97 Socket can only Listen to one endpoint
2018-07-14  #31 High/Low Water mark support?
```

**XPUB/XSUB proxying broken (2 separate issues, open 4-5 years)** is notable, because §2 showed XPUB is the *only* way a publisher learns its subscribers exist — i.e. the only route to Greg's §5 ACK requirement on a pubsub channel.

**I did build and run it, and basic REQ/REP works** [MEASURED — `10-zeromq-experiments/gozeromq_issues.go`]:
```
=== go-zeromq/zmq4 v0.17.0 : verifying reported open issues ===
[#97] Listen returned: first=<nil> second=<nil>
[#158] sent 20 jobs to 2 PULL workers -> received 20, distribution=[10 10]
```

**A correction I owe, since I nearly made the exact error this research round exists to avoid.** My first run of the fanout test returned `[4 16]` and I wrote down "confirms issue #158, round-robin is broken." **That was wrong.** I re-ran it five times:

```
go-zeromq/zmq4:  [10 10]  [13 7]  [9 11]  [10 10]  [10 10]
mangos:          [19 1]   [1 19]  [18 2]  [11 9]   [1 19]
```

`[4 16]` was a fluke of my own test's startup timing. go-zeromq is in fact **evener than mangos**, and neither lost a single message. Both are "send to whichever pipe is ready," not fair scheduling — my test was measuring socket buffering, not fairness. **I could not reproduce #158, and I'm not going to claim I confirmed it.** (This is what BRIEF §9 means by DEDUCED-and-stated-with-false-confidence. One run is not a measurement.)

**Verdict on go-zeromq/zmq4: don't.** Not because I found it broken — I didn't — but because it is an unmaintained WIP whose author has asked for a successor, and you would be adopting it as **the foundation of everything**.

### The Go summary

| Library | Kind | Latest release | Age | Cross-compiles? | Verdict |
|---|---|---|---|---|---|
| `pebbe/zmq4` | cgo → libzmq | v1.4.0 (2025-05) | 1.2 yr | ❌ **No** [MEASURED] | Mature, but poisons the build for a 2-OS fleet |
| `go-zeromq/zmq4` | pure Go | v0.17.0 (2024-05) | 2.2 yr | ✅ Yes | **"needs a caring maintainer"**; WIP; 2 yr silent |
| *(underneath)* `libzmq` | C | v4.3.5 (2023-10) | **2.8 yr** | — | 9 commits in 2025 |

**Choosing ZeroMQ in Go in 2026 means choosing between "mature but won't cross-compile" and "cross-compiles but abandoned."** There is no third door. That is a genuinely bad menu, and it is the strongest practical argument against ZeroMQ here.

---

## 5. NanoMsg / NNG — the contender Greg hasn't heard of

Here's the thing worth knowing: **ZeroMQ's original author wrote a successor because he thought ZeroMQ had design mistakes.**

The lineage:
1. **ZeroMQ** (2007), Martin Sustrik — C++, the original.
2. **nanomsg** (2013), *same author* — a rewrite in C, explicitly to fix ZeroMQ's mistakes. He published a document titled "nanomsg vs. ZeroMQ" listing them.
3. **NNG** ("nanomsg next generation", 2016), Garrett D'Amore — rewrote nanomsg again after hitting *its* limits. **This is the live project.**
4. **mangos**, D'Amore — a **pure-Go** implementation of the same protocols. Not a binding. No C. No cgo.

The protocols have a name: **SP, "Scalability Protocols."** They're published as open RFCs — the pattern is a *spec*, not one library's behaviour. mangos and NNG interoperate.

### What Sustrik said was wrong with ZeroMQ

[CLAIMED — nanomsg.org/documentation-zeromq.html, by ZeroMQ's own author]:

- **Threading model.** ZeroMQ binds objects "exclusively by a single thread," causing "**stuck REQ sockets**" — the wedge from §2. nanomsg decouples objects from threads and adds **built-in request retry**. *(That's Lazy Pirate — 2.3× the code in ZeroMQ — moved into the library.)*
- **No pluggable transports.** ZeroMQ "lacked a formal API for plugging in new transports."
- **The C++ dependency**, exceptions, STL allocations.
- **inproc's bind-before-connect ordering bug.**
- **Two new patterns ZeroMQ doesn't have** — and read these against Greg's brief:
  - **SURVEY** — "send queries to multiple peers and wait for responses from all of them."
  - **BUS** — "deliver messages from anyone to everyone else."

**Those two are not incidental.** SURVEY is *"send pings to each other to check they're online"* (§3.1, Greg's own words for the roster). BUS is a **peer mesh with no hub** — §4's not-centralized requirement as a socket type.

### I ran all of it

`10-zeromq-experiments/mangos_all_patterns.go`, pure Go, no libzmq, **first try**:

```
=== mangos v3 pattern test (pure Go, no libzmq) ===
  REQ/REP        -> "echo:ping"
  PUB/SUB        -> "topicA:hello" (topic filter works: got topicA not topicB)
  PUSH/PULL      -> "work-item"
  PAIR (p2p)     -> "p2p-msg"
  SURVEYOR       -> [gb-mac-mini:alive dgx:alive]  (one-shot fleet roll-call, no ZMQ equivalent)
  BUS node1      -> "node0-broadcast" (mesh: no hub)
  BUS node2      -> "node0-broadcast" (mesh: no hub)
=== done ===
```

**That SURVEYOR line is a fleet roll-call in ~25 lines of Go.** Ask "who's alive?", collect every answer, deadline ends the round. In ZeroMQ you build that yourself out of DEALER/ROUTER plus your own timers.

### And it cross-compiles to the whole fleet

The test `pebbe` failed, run against the real box list:

```
=== mangos cross-compile matrix (CGO_ENABLED=0, from macOS arm64 laptop) ===
  OK    darwin   arm64   gb-mbp + gb-mac-mini     size=8.8M
  OK    linux    arm64   dgx (GB10/Spark)         size=8.6M
  OK    linux    amd64   generic linux            size=9.2M
  OK    windows  amd64   bonus                    size=9.5M

=== is the linux/arm64 output a real static binary? ===
ELF 64-bit LSB executable, ARM aarch64, statically linked
```

**One command, from the laptop, to every box in the fleet. Statically linked — nothing to install on the target. No Go, no C compiler, no libzmq.** [MEASURED]

This matters more than it looks: the DGX **has no Go installed** [MEASURED]. With mangos that's irrelevant — `scp` the binary. With `pebbe/zmq4` you'd install Go, a C toolchain, and libzmq-dev on it first.

Protocols and transports available [MEASURED, GitHub API]:
- **Protocols:** `bus, pair, pair1, pub, sub, push, pull, req, rep, respondent, surveyor, star, xbus, xpair, xpub, xsub, xpush, xpull, xreq, xrep, xrespondent, xsurveyor, xstar`
- **Transports:** `inproc, ipc, tcp, tlstcp, ws, wss`

Both supersets of what ZeroMQ-in-Go offers, and the `x*` variants are the raw versions for building proxies.

### The honest case against mangos

I am not going to sell this without the bad news.

| Metric | Value |
|---|---|
| Latest release | **v3.4.2, 2022-08-11 — 3.9 years ago** |
| Commits 2023 / 2024 / 2025 / 2026 | **0 / 13 / 16 / 6** |
| Commits ahead of last release | 39 (mostly dependabot + CI) |
| Stars / open issues | 757 / 29 |

**mangos' last release is older than every ZeroMQ option, including libzmq's.** By the "recent release" metric alone, `pebbe/zmq4` (v1.4.0, 2025-05) wins outright. That is a real point against, and I won't dress it up.

The counter-argument, and you should weigh it yourself:

1. **[MEASURED]** The 39 unreleased commits are dependabot bumps, CI fixes, a branch rename, a `SECURITY.md`, one TLS config fix, one race fix in an *example*. **No unreleased bug fixes to the protocol code.** That's the signature of *finished*, not *rotting*.
2. **[MEASURED]** The owner is still present — `gdamore` committed in 2025-07 and 2025-08 (declaring Go version support policy, adding CODEOWNERS). That's caretaking, not abandonment.
3. **[MEASURED]** The same owner is **extremely** active on NNG: **575 commits in 2024, 207 in 2025**, releases **v1.12.0 on 2026-06-28** (17 days ago) and a v2.0 alpha line. Compare libzmq's **9 commits in 2025**. The *protocol family* is vividly alive; the Go implementation of it is simply done.
4. **[MEASURED]** It works. Every pattern, first try, no workarounds.

**[DEDUCED]** So: "mangos is stale" and "go-zeromq is stale" look identical on a release-date chart and are not the same thing. go-zeromq is a **WIP with core bugs open for 5 years whose maintainer publicly asked for a replacement**. mangos is a **complete implementation of a frozen spec whose author still shows up and is pouring 500+ commits/year into the sibling project**. I'd rather depend on the finished one. **I could be wrong about this** — it's a judgment call about intent from commit patterns, and it's the weakest link in my recommendation. It's in the open questions.

---

## 6. The fit question — is the resemblance real?

BRIEF §3.1, Greg's words:

> *"What if the daemon had 'channels', a channel had a name and a type (pubsub, etc), then the daemon would do data transport, while the plugin handled all the logic."*

Now the ZeroMQ/mangos API:

```go
sock, _ := pub.NewSocket()              // a type
sock.Listen("tcp://100.115.27.55:40899") // a name
sock.Send(payload)                       // transport. logic is yours.
```

**A socket has a name and a type. The library does transport. Your code does logic.**

**This is real, not superficial.** Three independent reasons:

1. **The type genuinely determines the semantics, at the library boundary.** `pub.NewSocket()` vs `push.NewSocket()` isn't a config flag — different code paths, different queueing, different overflow behaviour, different wire protocol. Greg's "a channel has a *type*" and ZeroMQ's "a socket *is* a pattern" are **the same design idea**, arrived at independently. That's a real signal his instinct is good.
2. **The layering he wants is the layering it enforces.** ZeroMQ has no opinion about your payload — it moves frames. *"the daemon would do data transport, while the plugin handled all the logic"* is a description of what ZeroMQ already does. Comms-as-a-plugin-over-channels (§3.1) drops straight onto it.
3. **[MEASURED]** The type is a **string away** from being data. `pub`/`sub`/`req`/`rep`/`bus`/`surveyor` are constructors in a package; a channel registry mapping `"agent-notes" → pubsub` to a constructor is a `map[string]func() (mangos.Socket, error)` — a few dozen lines. Greg's channel abstraction is **nearly a direct rename** of the socket abstraction.

### Where the resemblance breaks — and it matters

**The type does not determine durability, and Greg's model needs it to.**

Every ZeroMQ/mangos socket type is **in-memory and ephemeral**. Meanwhile BRIEF §5 wants messages written down, and §3.1 wants storage in the daemon so *"the plugins dont make up their own thing"*, and §4 says durability is a **plugin decision**: *"the plugin defines that! Then we can have multiple options."*

**So a channel in Greg's model is `(name, pattern, durability)`. A ZeroMQ socket is `(name, pattern)`.** The third axis doesn't exist and cannot be bolted on by choosing a different socket type.

**[DEDUCED]** This is a good discovery rather than a problem, because it tells you exactly where the seam goes:

```
channel = name + pattern + durability policy
                    │           │
                    │           └── YOUR CODE (per-node log; plugin picks the policy)
                    └────────────── mangos socket type (free)
```

The pattern axis is free. **The durability axis is the entire project** (§3's 1,400–2,900 lines). ZeroMQ's resemblance to Greg's vision is real but covers **the half that was already easy.**

---

## 7. The honest verdict

### Does ZeroMQ make sense here?

**The model: yes, and Greg is right to like it. The library: no.**

**Why ZeroMQ the library loses** — in the brief's terms, not fashion:

1. **It fails §4 exactly where it claims to win.** "Brokerless" is why Greg leans ZMQ over NATS, and the reasoning is sound. But ZeroMQ's *own* answer to §5's durability is **Titanic layered on the Majordomo broker** — a central hub whose death is fatal. **Choosing ZeroMQ and then following its own guidance lands you back at the exact hub-and-spoke mistake BRIEF §9 says sank the last round.** The brokerless alternative (Freelance) has no durability, and no Go version.
2. **It fails §4's "stick with Go" with no good option.** `pebbe` **cannot cross-compile** [MEASURED] — Go + C toolchain + libzmq on every box, forever. `go-zeromq` cross-compiles but its maintainer publicly asked for a successor and it's had no commit in 2 years [MEASURED]. Underneath, libzmq: **2.8 years, no release; 9 commits in 2025** [MEASURED].
3. **Every pattern this brief needs is missing from its Go track.** Freelance (§4), Titanic (§5), Clone (§3.1's lookup table) — nine languages between them, **none of them Go** [MEASURED].
4. **It cannot do §5's ACK on a pubsub channel.** The Guide: *"there's no way to layer it on top because subscribers are invisible to publisher applications"* — against Greg's *"I believe we can find that out."*
5. **It gives nothing toward the roster (§3.1, "baked in").** No presence, no discovery, no membership.

**Why mangos wins:**

1. **Same model, same brokerless property** — the resemblance in §6 is a property of SP protocols, not of ZeroMQ specifically. The thing Greg likes, he keeps.
2. **Pure Go. One command to the whole fleet.** Static binaries to darwin/arm64 + linux/arm64 [MEASURED]. Against `pebbe`'s hard failure. The DGX has no Go installed — with mangos, that stays true.
3. **Two patterns that answer the brief and don't exist in ZeroMQ:** SURVEY = §3.1's roll-call (~25 lines, measured). BUS = §4's mesh, as a socket type.
4. **Fewer sharp edges by design** — REQ retry is in the library, so Lazy Pirate (2.3× code in ZeroMQ) is free.
5. **The protocols are a spec**, not one library's behaviour. If mangos ever truly dies, the wire format is an open RFC that NNG also implements — a real exit.

**What mangos costs, honestly:** last release 3.9 years ago — worse than every ZeroMQ option on that metric. My read (§5) is "finished, not rotting," and I flagged it as the weakest link in this recommendation.

**Alternatives, and why they lost:**
- **`pebbe/zmq4`** — most mature ZeroMQ-in-Go, genuinely. Lost on cross-compilation [MEASURED]; a permanent per-box toolchain tax on a fleet whose stated purpose is that nobody should *"screw around figuring out the network crap."*
- **`go-zeromq/zmq4`** — lost on *"zmq4 needs a caring maintainer"* + 2 years silent + XPUB/XSUB open 5 years. I did **not** find it broken in my tests, and I corrected my own false claim that I had (§4).
- **NATS** — rejected by §4, and correctly. Worth noting it's the *only* option here that ships durability, presence, and request/reply in the box; it loses on not-centralized alone. That trade is Greg's to make, and he's made it.
- **Raw TCP / gRPC** — a real contender I'm not dismissing lightly, especially given §3.2 wants protobuf anyway. It loses because you'd rebuild reconnect, framing, and many-peers-per-socket, which mangos gives free — but if the durability layer (§3) ends up dominating the code, **this gap narrows a lot.** Flagged as an open question, not settled here.

### If it's "yes but you'll write a durability layer" — quantify it

**It is, and here it is** [DEDUCED, ±50%]:

| Layer | Lines | Basis |
|---|---|---|
| Channel registry (name+type → socket) | ~50–100 | measured: constructors are a map away |
| Roster / presence (SURVEY + node table) | ~200–400 | measured: roll-call working in ~25 |
| **Durable log** (write-down, ids, offsets, replay) | **~500–900** | Titanic is 208 C lines *but assumes a broker*; per-node is more |
| ACK / delivery tracking | ~150–300 | Paranoid Pirate's heartbeat half is ~199 total |
| Plugin registration + protobuf plumbing | ~300–500 | §3.2 |
| **Total kernel** | **~1,200–2,200** | |

**~1,500 lines is a real project and an achievable one.** It is not 10,000. And the number is **materially the same on ZeroMQ** — so it is not an argument against mangos; it's an argument that **the transport is not where this project's difficulty lives.**

### The one architectural idea worth taking from this

§4 (no fatal hub) and §5 (messages written down) look contradictory, and ZeroMQ's own guide treats them as a fork in the road — Majordomo *or* Freelance, pick one.

**They're only contradictory if there's one log.** Give **every node its own durable log**:

- Sender writes to **its own** disk first, *then* transmits. Nothing is lost on send.
- Receiver writes to **its own** disk on arrival, *then* ACKs. Nothing is lost on receipt.
- Peer down? The message **sits in the sender's log** and ships on reconnect (mangos auto-redials, free).
- Agent was away? It **reads its own log** from its last offset — which is §5's *"if they are polling, then they read their messages the next time they come in"*, exactly.
- Sender needs to know the receiver isn't listening? **The ACK never arrives**, and the roster (SURVEY) says the box is down. That's §5's ACK requirement, satisfied without XPUB and without a broker.

**Store-and-forward per node is both durable and hub-free. No box's death kills anything — it just delays it.** [DEDUCED — I have not built this, and it's the claim in this document most worth attacking. But it's the standard shape of every hub-free durable system, and it dissolves the §4/§5 tension that ZeroMQ's guide treats as a hard fork.]

And notice what it does to the transport requirement: **it collapses.** If every node has its own log and ACKs, the transport needs to be a reliable point-to-point pipe with automatic reconnect and message framing — which is the free part of both libraries. **Which is the real reason to stop agonizing over ZeroMQ vs mangos and pick on operational cost: pure Go, one static binary, `scp` to the fleet.**

---

## Open questions

1. **Does "fanout" mean *everyone gets a copy* or *one worker gets it*?** (§2's table.) The brief lists "pubsub" and "fanout" as separate menu items, which suggests he means the work-queue sense (PUSH/PULL) — but "fanout" normally means broadcast. **This changes which socket types the kernel ships and is worth 30 seconds of asking.**
2. **Is my "mangos is finished, not rotting" read correct?** The weakest link here. It's an inference about maintainer intent from commit patterns (§5). Falsifiable: file an issue and see if `gdamore` responds. Cheap to test, high value.
3. **Would raw TCP + protobuf beat both?** If the durability layer (~1,500 lines) dominates, the transport's value drops and §3.2 wants protobuf anyway. I did not test this and it deserves a fair look.
4. **Does mangos' auto-reconnect actually behave under real Tailscale partitions and laptop sleep?** I tested on loopback only. `gb-mbp` *"sleeps, reboots"* (§1) — this is the fleet's normal state, not an edge case, and it's the most important untested thing here.
5. **What are the SP protocols' wire-level guarantees on reconnect?** Specifically: are in-flight messages dropped on redial? This determines how much the per-node log must re-send, and I did not verify it.
6. **`gb-mac-mini` was unreachable during this work** — `ssh 100.120.22.74` → `Host key verification failed`. I assume darwin/arm64 but did **not** verify. Amusingly, this is exactly the §4 problem — *"an agent doesn't stumble over the wrong machine name/ip/username/inability to connect via ssh"* — happening live, to me, during the investigation.

---

## Sources

**Files read (cloned repo `github.com/booksbyus/zguide` → `/tmp/zguide`, 2026-07-15):**
- `examples/Go/` — full listing (47 files); line counts of `hwclient.go`, `hwserver.go`, `lpclient.go`, `lpserver.go`, `spqueue.go`, `spworker.go`, `ppqueue.go`, `ppworker.go`, `mdp.go`, `mdbroker.go`, `mdcliapi.go`, `mdclient.go`, `mdwrkapi.go`, `mdworker.go`, `zhelpers.go`
- `examples/C/titanic.c` (208), `clone.c` (296), `clonesrv6.c` (288), `bstar.c` (215), `flcliapi.c` (246)
- `chapter4.txt` — lines 13, 672, 696, 735, 752, 763, 1045, 1047, 1208
- `chapter5.txt` — lines 134, 136–150
- `chapter8.txt` — lines 485–489, 676–678, 794–796
- Language-availability listings for `titanic*`, `flclient*`/`flserver*`, `clonesrv*`, `bstar*`

**Experiment code written and run (preserved at `/Users/gb/research/2026-07-15-agent-substrate-v2/investigate/10-zeromq-experiments/`):**
- `mangos_all_patterns.go` — REQ/REP, PUB/SUB, PUSH/PULL, PAIR, SURVEYOR, BUS
- `mangos_durability_loss.go` — late-joiner + 100k-message silent-drop test
- `gozeromq_issues.go` — go-zeromq/zmq4 issues #97, #158
- `mangos_fanout.go` — PUSH distribution comparison
- `pebbe_build.go` — cgo build/cross-compile probe
- `go.mod.reference` — resolved versions: `pebbe/zmq4 v1.4.0`, `go-zeromq/zmq4 v0.17.0`, `mangos/v3 v3.4.2`
- Toolchain: `go version go1.26.2 darwin/arm64`

**Commands run:**
- `go build` / `go run` against all three libraries; cross-compile matrix `GOOS`×`GOARCH` ∈ {darwin,linux,windows}×{arm64,amd64}, `CGO_ENABLED=0`
- `file /tmp/zmqtest/m_linux_arm64` → confirmed static ELF aarch64
- `uname -srm` on `gb-mbp` (Darwin 25.4.0 arm64) and `dgx` via `ssh gb@100.115.27.55` (Linux 6.17.0-1026-nvidia aarch64; libzmq present; **no Go**)
- `ssh 100.120.22.74` → **failed**, host key verification
- `brew info --json=v2 zeromq` → stable 4.3.5, not installed locally
- `pkg-config --modversion libzmq` → not found

**GitHub API (api.github.com, 2026-07-15):**
- `/repos/{zeromq/libzmq, pebbe/zmq4, go-zeromq/zmq4, nanomsg/nng, nanomsg/mangos, nanomsg/nanomsg}` — metadata, releases, tags, commits, open issues
- `/search/commits?q=repo:…+committer-date:…` — per-year commit counts for libzmq (2021–2026), nng and mangos (2023–2026)
- `/repos/nanomsg/mangos/compare/v3.4.2...main` — 39 commits ahead, itemized
- `/repos/nanomsg/mangos/contents/{protocol,transport}`, `/repos/go-zeromq/zmq4/contents/` — type listings

**URLs fetched:**
- `https://raw.githubusercontent.com/go-zeromq/zmq4/main/README.md` — *"zmq4 needs a caring maintainer"*
- `https://raw.githubusercontent.com/nanomsg/mangos/main/README.md`
- `https://raw.githubusercontent.com/pebbe/zmq4/master/README.md`
- `https://nanomsg.org/documentation-zeromq.html` — Sustrik's "nanomsg vs. ZeroMQ"
- `https://nng.nanomsg.org/RATIONALE.html` — D'Amore's NNG rationale (nanomsg→NNG, not ZMQ→nanomsg)
- `https://zguide.zeromq.org/docs/chapter4/` — cross-checked against the cloned source text
