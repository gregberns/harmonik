# 24 — Three plugins that test whether the kernel API is right

**Date:** 2026-07-15
**Scope:** BRIEF §2 (agent comms), §3.1 (channels, roster, storage, registration), §3.2 (plugins, protobuf, live reload), §4 (not-centralized; durability is a plugin decision; metadata rides along), §5 (message semantics + the ACK gap).
**Job:** design the comms plugin, the registry plugin, and the logtail plugin *against the real kernel API* — and if the API cannot express them cleanly, **say so**. §5 of this doc ("Does the kernel API survive?") is the point of the whole exercise; everything before it is the work that earns that list.

**A note on process, stated up front because it changes how you should read this.**
I was told to read `design/20-kernel.md` and `design/22-plugin-system.md` first. When I started, **`design/` did not exist** — `ls` returned *"No such file or directory"*. I am running in parallel with those authors, not after them. So I designed all three plugins against a kernel API I derived myself from BRIEF §3.1 and the five investigation docs.

**Then, while I was writing, `design/20-kernel-proto/` appeared** — `fleet/kernel/v1/kernel.proto` and `plugin.proto`, real files [MEASURED]. **I reconciled everything below against the real API rather than shipping a test of my own invention.** This turned out to be the luckiest thing that could have happened to this document, for two reasons:

1. **The two designs converged independently on nearly every contested point** — per-node sequences with no fleet-wide order, lookup-as-a-channel-type, name clashes returned as multiple claimants rather than auto-resolved, plugins holding no in-memory state. Convergent independent derivation is much stronger evidence than agreement would have been if I'd read it first. §5.3 lists the convergences precisely.
2. **The places we diverged are now real findings against a real API**, not quibbles with a straw man. One of them (§5.1 **F9**) is, I think, the most important thing in this document: *the kernel's log has no durability knob, so "durability is a plugin decision" is not currently true.*

§1 below describes the **real** API, marking where my independent version differed and which one I think is right.

**Labels, used throughout:**
- **[MEASURED]** — I ran it today, on this fleet, and the output is quoted.
- **[CLAIMED]** — a doc, spec, or vendor says so; I did not verify it myself.
- **[DEDUCED]** — reasoning from the other two. Flagged because BRIEF §9 says confident deduction is exactly how the last round died.

---

## 0. Terms, defined once

Greg is a strong engineer but not deep in this space, so nothing below is used before it is defined.

| Term | Plain meaning |
|---|---|
| **Kernel** | The daemon's built-in part. Moves bytes, tracks who's alive, stores what plugins hand it. Knows nothing about agents, logs, or messages. |
| **Plugin** | A separate program the daemon starts as a child process and talks to over a local socket. Where all the actual logic lives. |
| **Channel** | BRIEF §3.1's unit: a **name** plus a **type**. `comms.deliver` is a name; `request/reply` is a type. |
| **Envelope** | The kernel's wrapper around a plugin's bytes: channel, origin, sequence, tags, timestamp, opaque payload. The kernel reads the wrapper and never the payload. |
| **Cursor** | A bookmark: "reader X has consumed up to position N of log Y". |
| **Store-and-forward** | Write the message to your own disk *first*, then try to send it. If the send fails, it's still on disk, so retry later. This is how the post office works, and it is the whole trick that makes §4 and §5 compatible. |
| **ACK** (acknowledgement) | A reply that says "I got it". The interesting question is always *who* is acknowledging *what* — see §2.4. |
| **Lookup table** | A replicated map: every box holds a copy, each box may only write its own rows. Greg's §3.1 menu lists this first ("publish/lookup table"). |
| **Roster** | The kernel's list of *boxes* and whether each is up. Not agents — boxes. |
| **Registry** | The *plugin's* list of *agents*. Sits on top of the roster. Different thing, confusingly similar word; I keep them strictly separate. |
| **Transcript** | The `.jsonl` file a coding agent (Claude Code, Codex, Pi) writes as it works. One JSON object per line, appended live. |
| **tmux** | A terminal multiplexer — it lets a terminal session keep running when nobody's looking at it, and lets another program type into it. It is how you poke a running agent. |
| **JSONL / NDJSON** | A file of one JSON object per line. Append-only friendly; you can tail it. |
| **fsync** | "Actually write it to the physical disk now." Slow. The difference between "the OS has it" and "a power cut won't lose it". |
| **go-plugin** | HashiCorp's library: plugins are ordinary programs run as child processes, spoken to over gRPC/protobuf. The live-reload investigation (12) recommends it. |

---

## 1. The kernel API this design is tested against

**The API is real:** `design/20-kernel-proto/fleet/kernel/v1/{kernel,plugin}.proto` [MEASURED — read in full]. `KernelService` is 19 methods; `PluginService` is 4. This section describes it, notes where my independent derivation differed, and says which I think is right. §5 is the verdict.

### 1.1 The shape

Each box runs one daemon. The daemon starts each plugin as a **child process** and speaks protobuf to it over a **local unix socket** (go-plugin; investigation 12 measured 45.4 µs/call, 22,008 calls/sec, 16.6 MB RSS per plugin — ~1000× more headroom than this fleet needs). Plugins never talk to each other, never open sockets to other boxes, and never touch the network. They call the kernel; the kernel calls them back.

```
   agent (claude/codex/pi)            agent
        │ CLI / hook                    │
        ▼                               ▼
  ┌──────────────────────┐        ┌──────────────────────┐
  │  daemon on gb-mbp    │◄──────►│  daemon on dgx       │   full mesh,
  │  ┌────────────────┐  │  chan  │  ┌────────────────┐  │   no hub
  │  │ kernel         │  │  nels  │  │ kernel         │  │
  │  │  channels      │  │        │  │                │  │
  │  │  roster        │  │        │  │                │  │
  │  │  storage       │  │        │  │                │  │
  │  └───┬────────────┘  │        │  └───┬────────────┘  │
  │   gRPC/protobuf      │        │      │               │
  │  ┌───▼───┐┌───▼───┐  │        │  ┌───▼───┐           │
  │  │ comms ││logtail│  │        │  │ comms │  ...      │
  │  └───────┘└───────┘  │        │  └───────┘           │
  └──────────────────────┘        └──────────────────────┘
```

**The line I am not allowed to cross** (DESIGN RULES, and BRIEF §3.1): the kernel does transport, the plugin does logic. If I find myself putting "what a message means" or "what an agent is" into the kernel, I have failed. §5 reports every time I was tempted.

### 1.2 The envelope — and the convergence that matters most

The real one [MEASURED — `kernel.proto:37-51`], abridged:

```proto
message Envelope {
  string channel = 1;
  bytes payload = 2;                  // OPAQUE. Never parsed by the kernel.
  map<string, string> headers = 3;    // plugin metadata; kernel does not read it

  string origin_node = 10;            // which box published it
  google.protobuf.Timestamp origin_time = 11;  // that box's clock. NOT a fleet order.
  string message_id = 12;             // unique; the hook a plugin builds ACKs on
  uint64 origin_seq = 13;             // monotonic per (origin_node, channel). NEVER fleet-wide.
  string producer = 14;               // publishing plugin's namespace
}
```

**My independent version had the same three load-bearing decisions**, and I am recording that because convergence is the evidence, not the agreement:

- **Per-node sequence, no fleet-wide order.** Salvage row 35 [MEASURED at `extract/10:257`]: harmonik sorts by UUIDv7 in four load-bearing places; UUIDv7 sorts by wall clock; across three unsynchronised boxes that sorts by *clock skew*, not causality. The prior round's fix was a single writer — which was the hub, and the hub is dead (BRIEF §4). The kernel's comment states the reasoning in one line: *"a fleet-wide order needs a single writer, a single writer is a fatal hub, and BRIEF 4 forbids fatal hubs."* Two independent derivations, same conclusion, from the same measured bug. **This is now the design's ordering rule and I am no longer flagging it as my [DEDUCED] guess — but see open question 2: neither of us measured that `thread_id` is a sufficient causality story for a human.**
- **`origin_time` is display-only.** Salvage row 34. Three boxes, three clocks.
- **`message_id` is dedupe, never ordering.** Store-and-forward makes retries routine, so dedupe is load-bearing.

One delta: I proposed a `content_type` field; the real one has none, and `headers` covers it by convention. **The kernel is right** — a typed field the kernel never reads is a field pretending to be enforced.

### 1.3 The channel types

### 1.3 The channel types

BRIEF §3.1's menu: *"publish/lookup table, point to point, pubsub, fanout, whatever"* plus request/reply (*"the internet is based on that"*). The kernel ships **four** [MEASURED — `kernel.proto:18-36`]:

```proto
enum ChannelType {
  CHANNEL_TYPE_PUBSUB = 1;          // "Subscribing to an exact name is what people call
                                    //  'fanout'; subscribing to a pattern is what people
                                    //  call 'pubsub'. Same primitive, so one type."
  CHANNEL_TYPE_POINT_TO_POINT = 2;  // competing consumers
  CHANNEL_TYPE_REQUEST_REPLY = 3;   // one question, one answer, correlated by the kernel
  CHANNEL_TYPE_LOOKUP = 4;          // replicated map, single-writer-per-key BY CONSTRUCTION
}
```

**Greg's menu had five items; the kernel ships four, and `FANOUT` is the one that's gone.** I reached the same conclusion from the opposite direction — by tallying what my three plugins actually use and finding fanout had no consumer (§5.2). Two agents, two routes, one answer. That is about as much confidence as this exercise can produce, and it is a direct answer to BRIEF §7 Q3.

A channel is **declared, not implied** — `ChannelDecl{name, type, public}` in the manifest, and *"a later conflicting TYPE for the same name is rejected AT REGISTRATION, before any byte moves"* [MEASURED — `plugin.proto:12-20`]. This is the thing investigation 11's open question #4 warned about: in NATS, "type" is a property of *how you subscribe*, not of the subject, so nothing stops two plugins disagreeing about what a channel is. The kernel holding the `(name→type)` table and refusing the mismatch **is** the difference between Greg's design and "we just used NATS subjects".

### 1.4 The three things that answer BRIEF §5's ACK gap for free

```proto
// Does anyone, anywhere in the fleet, subscribe to this channel right now?
// This is a LOCAL lookup against gossiped interest: microseconds, no network.
// It says "someone is listening", NOT "it was delivered".
enum Interest { INTEREST_PRESENT = 1; INTEREST_NONE = 2; INTEREST_UNKNOWN = 3; }

message PublishResponse { string message_id = 1; Interest interest = 2; uint64 origin_seq = 3; }
message RequestResponse { Envelope envelope = 1; Interest interest = 2; }
                         // INTEREST_NONE => nobody serves this; returned immediately
```

`Interest` rides on **both** `Publish` and `Request` responses, and `INTEREST_UNKNOWN` is the honest third state for *"a node is suspect so I can't tell you"*. My version had this as a separate `Interest` RPC and a `Status` enum; **the kernel's is better** — piggybacking it on the response means you cannot forget to check, and the third state means it never lies. This is the transport half of §2.4's ACK ladder, and it is free.

### 1.5 Storage, and the sentence that decides the plugin boundary

```proto
// NOTE WHAT IS ABSENT: there is no namespace field anywhere below. The kernel
// derives it from the calling plugin's registered identity. Reading another
// plugin's storage is not forbidden, it is UNREPRESENTABLE. Storage is
// box-local and never replicated; if you want data on another box, publish it
// on a channel. Replication, like durability, is a plugin decision.
```
[MEASURED — `kernel.proto:128-135`]

Three primitives: `Log` (append/read/truncate, per-`(plugin, log)` sequence, `follow` on read), `KV` (get/put/delete/list/watch, **with `optional uint64 if_revision` compare-and-swap**), and cursors — which are *not* a separate primitive: *"cursors live in KV, not in a plugin's own file"* [MEASURED — `kernel.proto:165`].

**That decision is worth more than it looks.** My design ported `commscursor.go` — 323 lines of cross-process `flock`, bounded retry, monotonic-only advance, temp→fsync→rename, corrupt-cursor recovery [MEASURED, read at HEAD]. **Against a kernel KV with compare-and-swap, all 323 lines collapse to a CAS loop**, because the file locking existed only to arbitrate between *processes*, and now exactly one process — the daemon — owns the file. The "cursor can only ever move forward" invariant that harmonik enforces with a lock and a re-read becomes `if_revision`. This is the best example in this document of the kernel earning its keep: it didn't add a feature, it *deleted a class of problem* by moving the writer.

### 1.6 The plugin side, and where my "local ingress" concern went

```proto
message PluginManifest {
  string namespace = 1;      // owns <namespace>.* and its own storage. THE identity.
  string version = 2;
  uint32 api_version = 3;
  repeated ChannelDecl channels = 4;
  repeated InterestDecl interests = 5;  // the plugin's MAXIMAL surface,
                                        // inspectable without running it
}
service PluginService {  // four methods, lifecycle only
  rpc Describe(...); rpc Start(...); rpc Stop(...); rpc Health(...);
}
```

`Describe` returning the manifest **as data** is Benthos's shape, not Telegraf's `SampleConfig() string` blob — investigation 14 measured both (`/tmp/priorart/bt/public/service/environment.go:263-381` vs `/tmp/priorart/tg/plugin.go:13-63`) and called the data version strictly better. It matches osquery, where extensions *"broadcast"* their plugins to the core on connect [CLAIMED — osquery docs, via inv. 14] — literally BRIEF §3.1's *"the plugin would say what resources it was interested in"*. `interests` being the *maximal* surface, inspectable without running the plugin, is a nice property neither I nor §3.1 thought of.

**The thing I expected to be a hole, and what actually happened.** An agent runs `comms send --to pi-refactor-3 "…"`. That CLI must reach the comms plugin. I assumed the kernel would need to proxy typed plugin services and wrote it up as a blocking finding. The real design solves it differently, in the first three lines of `plugin.proto`:

> *"The kernel -> plugin side. Deliberately tiny: lifecycle only. **ALL data flows the other way, through KernelService, which the plugin calls. That is why the plugin API and the REST API are the same service.**"*

So the CLI is just another `KernelService` client: it calls `Request(channel:"comms.send", payload:…)`, and the comms plugin is sitting in `Serve("comms.send")`. **No proxy, no service registry, no new concept.** That is a genuinely better answer than mine and I am recording that I was wrong about it.

**But it is not free, and the cost lands exactly on the property Greg asked for** — see **F1** in §5.1.

---

## 2. Plugin (a) — comms

> *"I've also started plenty of agents and then converge on similar things I'm working on and I need them to send messages back and forth... I can have one agent comm with another and figure something out - but they can still have different directives."* (BRIEF §2)

The system carries the message. It never judges, never detects overlap, never warns (BRIEF §2, §6). The plugin is a post office, and the most important design property is that **it has no opinion about the mail**.

### 2.1 The message

```proto
syntax = "proto3";
package fleet.comms.v1;

message Message {
  string msg_id     = 1;   // uuid. dedupe key. never an ordering key.
  string from       = 2;   // registry agent name, e.g. "claude-github-a7f"
  string to         = 3;   // registry agent name. NOT box:pid. NOT a session id.
  string body       = 4;   // opaque text. The plugin NEVER parses this. (§2, §6)
  string thread_id  = 5;   // set to the root msg_id. The only cross-node causality we have.
  string in_reply_to= 6;   // msg_id being replied to.
  google.protobuf.Timestamp sent_at = 7;  // display only
  map<string,string> meta = 8;  // free-form, sender's business
}

// NOT a gRPC service — under the real kernel these are CHANNELS the plugin Serves,
// and the agent's CLI reaches them as an ordinary KernelService.Request caller (§1.6).
// Written service-style because that is what the agent experiences, and because it is
// exactly what F1 says we lose: the kernel sees `bytes`, so nothing below is curl-able
// as typed JSON without the descriptor fix in §5.1.
//
//   comms.send      REQUEST_REPLY  SendReq    -> SendResp     an agent sends
//   comms.inbox     REQUEST_REPLY  InboxReq   -> InboxResp    POLL: read since my cursor
//   comms.follow    PUBSUB         (subscribe)-> Message      SUBSCRIBE: notify me
//   comms.ack       REQUEST_REPLY  AckReq     -> AckResp      advance my cursor
//   comms.receipt   REQUEST_REPLY  Receipt    -> ()           sender learns what happened
```

`from` is **not authenticated**, deliberately. Harmonik does the same (`agentcommspayloads_djqc9.go:67`, an explicit non-goal [CLAIMED via salvage]) and BRIEF §4 scopes this to a trusted network of three boxes Greg owns. Salvage says plainly: don't let anyone reopen this. Agreed.

### 2.2 Which channel types, and why

| What | Channel | Type | Why not something else |
|---|---|---|---|
| `agent name → node` resolution | `registry.agents` | **LOOKUP** | comms must not call the registry plugin — plugins don't address each other (Telegraf, [MEASURED at `/tmp/priorart/tg/agent/agent.go:38-104`]). The registry *publishes* the table; comms *reads* it. Neither knows the other exists. |
| cross-node delivery | `comms.deliver` | **REQUEST_REPLY** | Gives the durable ACK **and** the no-responder signal in one round trip (§2.4). Pubsub would broadcast private mail to boxes that don't want it and force every box to store every message. |
| read receipts back to sender | `comms.receipt` | **REQUEST_REPLY** | Same shape, reverse direction. Best-effort; the receipt is an optimisation, the cursor is the truth. |
| `comms.cursors` (this agent's read position) | `comms.cursors` | **LOOKUP** | Owned by comms, not the registry — the kernel's namespace rule forces the split (§3.6). This is what makes §2.4's rung 4 answerable. |
| agent's CLI ↔ its own daemon | `comms.send`, `comms.inbox`, `comms.follow`, `comms.ack` | **REQUEST_REPLY** / **PUBSUB** | Never crosses the network; the CLI is just another `KernelService` client (§1.6). |

**Point-to-point and fanout are not used.** See §5.2 — this turns out to be a finding, not an omission.

### 2.3 The send path — store-and-forward, and why this is the whole design

BRIEF §5 says *"all the messages are written down"*. BRIEF §4 says no box's death may kill the system, and durability is the plugin's job. These look contradictory — "written down" sounds like a log, and a log sounds like a place, and a place sounds like a hub. **They are only contradictory if there is one log.** The zeromq investigation reached the same conclusion independently and stated it as that document's key architectural contribution; I reached it from the durability side and I am adopting it as the load-bearing pattern of this plugin.

**Per-node store-and-forward: the sender writes it down locally, then transmits. The receiver writes it down locally, then ACKs.** Two copies, both local, no third place. Durable AND hub-free.

```
comms.Send(to="pi-refactor-3", body="...")  on gb-mbp
  │
  1. resolve: LookupGet("registry.agents", "pi-refactor-3")     ← LOCAL map read, µs
  │    not found                → return NO_SUCH_AGENT immediately
  │    found in 2+ rows         → return AMBIGUOUS_NAME{nodes:[...]}  (§3.3)
  │    found → node="dgx"
  │
  2. LogAppend("comms/outbox", msg, sync:true)  → seq=4471
  │    ** the message is now "written down" (§5). Nothing after this can lose it. **
  │    ⚠ `sync` DOES NOT EXIST in the real LogAppendRequest today. See F9 — this
  │      is the one line of this design the kernel cannot currently express.
  │
  3. RosterList() → "dgx"                                          ← LOCAL map read, µs
  │    dead   → leave in outbox, return QUEUED{reason:NODE_DOWN,  since:…}
  │    left   → leave in outbox, return QUEUED{reason:NODE_ASLEEP,since:…}
  │    alive  → continue
  │
  4. Request("comms.deliver", node="dgx", msg)  — the receiving daemon's comms plugin:
  │      a. LogAppend("comms/inbox", msg, sync:true)  ← IT writes it down before replying
  │      b. LookupGet("registry.agents","pi-refactor-3") → cursor, presence
  │      c. reply DeliverResp{written:true, seq:9912,
  │                           agent_state:"idle", cursor_lag:3, last_read:T}
  │      d. ring the doorbell (§2.5) — best effort, never blocks the reply
  │
  5. mark outbox entry delivered → return DELIVERED{ + the receiver's presence facts }
```

**Failure at every step is a non-event**, and that is the property worth buying:

| Failure | What happens |
|---|---|
| Step 4 returns `NO_RESPONDER` (daemon up, comms plugin reloading — 12 ms window [MEASURED, inv. 12]) | stays in outbox, retried in 1s |
| Target box asleep/dead | never attempted; `RosterWatch` fires `NodeJoin` on wake → drain outbox |
| **Sender's box dies between step 2 and step 4** | the message is on the sender's disk, `sync:true`. On restart, the outbox drains. Nothing is lost, no hub was involved. |
| Receiver's box dies after 4a, before the agent reads | it is on the *receiver's* disk. It's there when the box comes back. |
| The archive box (dgx) dies | **nothing happens.** Comms never touches it. |

The retry loop is the *only* background work: scan `comms/outbox` for undelivered entries whose target node is alive; retry; exponential backoff capped at 30s. Roster events make it event-driven rather than a poll — BRIEF §3.1 anticipated exactly this (*"Maybe it needed changes in the node list"*), and investigation 14 found the mechanism already exists in memberlist's `EventDelegate` / `NotifyJoin` (`/tmp/priorart/ml/event_delegate.go:10-23`).

**Sending to an agent on my own box** takes the same path minus step 4's network hop — `origin_node == target_node`, so it's a local `LogAppend` to `comms/inbox`. One code path, no special case.

### 2.4 The ACK gap — designed

> *"the current model is a little flawed - we actaully should probably notify the sender that the receiver is not listening. I believe we can find that out. So we could do some type of ACK system."* (BRIEF §5)

He is right that you can find it out. The trap is that **"the receiver is not listening" is not one condition, it is four**, and a single ACK bit answers none of them well. Here is the whole ladder, and who answers each rung:

| # | Question | Answered by | Cost | When the sender learns |
|---|---|---|---|---|
| 1 | Does this name exist at all? | lookup table, local read | µs | **at send**, `NO_SUCH_AGENT` |
| 2 | Is the receiver's *box* up? | roster, local read | µs | **at send**, `QUEUED{NODE_ASLEEP}` |
| 3 | Is the receiver's *daemon+plugin* up? | kernel `NO_RESPONDER` | 310 µs [MEASURED, salvage] / 395 µs pinned | **at send** |
| 4 | Is the *agent* actually reading? | **the receiver's cursor** | µs, piggybacked on the delivery reply | **at send** |
| 5 | Did it read *this* message? | `comms.receipt` when the cursor passes `msg_id` | async | later, if it happens |

Rungs 1–3 are free — they fall out of the roster and the transport. Investigation 11 measured NATS returning `no responders available for request` **in 0 seconds despite a 5-second timeout**, cluster-wide; the zeromq investigation measured mangos SURVEY returning `[gb-mac-mini:alive dgx:alive]` in ~25 lines. Both candidate transports can answer rung 3; they just spell it differently. That is why §1.4 names it `Interest`/`Status` as a *kernel* concept rather than borrowing either spelling.

**Rung 4 is the one Greg actually wants, and it is the one nobody gives you free.** An agent can be alive, its box up, its daemon up — and it simply isn't reading its mail. That is "not listening" in the sense that bites. And it is answerable, because **the receiving comms plugin owns the receiver's cursor**: it knows the last message the agent consumed and when. So the delivery reply carries it back:

```proto
message DeliverResp {
  bool   written      = 1;   // I have it on disk. (rung 3 answered positively)
  uint64 seq          = 2;
  string agent_state  = 3;   // "working"|"idle"|"stale"|"offline"  ← observed, §3.4
  uint32 cursor_lag   = 4;   // how many messages unread BEFORE this one
  google.protobuf.Timestamp last_read = 5;  // when the cursor last moved
  bool   doorbell_rung= 6;   // did we manage to poke the pane? (§2.5)
}
```

So `comms send` returns, synchronously, in about a millisecond:

```
DELIVERED to pi-refactor-3 on dgx (written, seq 9912)
  receiver: stale — 47 unread, last read 3h12m ago, doorbell: no tmux target registered
```

**That is a better answer than an ACK bit and it is strictly mechanical** — every field is an observed fact (a cursor position, a file's mtime, a roster state), not a judgement. It respects BRIEF §2's *"never judges"* while telling the sender exactly what it needs: *your message is safe, and the agent it's addressed to hasn't looked at anything in three hours.* A human or an agent can act on that. An ACK bit tells you nothing you can act on.

Rung 5 (`Receipts`) is a *stream the sender may subscribe to*, delivered over `comms.receipt` when the receiver's cursor passes the message. It is best-effort by construction: if the receipt is lost, the cursor is still correct, and the sender can ask again. **The cursor is the truth; the receipt is a convenience.** This is the same discipline as the doorbell in §2.5 and it is the reason nothing in this plugin needs a distributed transaction.

**What I deliberately did NOT build:** an ACK that blocks the sender until the receiver reads. That converts every message into an open-ended wait on a process that may be mid-turn for twenty minutes, and it would make comms the thing that hangs. Greg's model (§5) is a mailbox, not a phone call, and the four-rung answer preserves that.

### 2.5 Reaching a live agent mid-session — the doorbell

Salvage §2.5 [MEASURED, and I re-read the source today] documents three delivery paths in harmonik, and only one wakes an idle agent:

- **Pull** — the agent asks (`Inbox`). Always works. Requires the agent to ask.
- **Long-poll** — `Follow` streams. Only wakes an agent *already blocked reading*. A Claude agent that finished its turn is reading nothing.
- **tmux paste** — the only thing that makes an *idle* agent act.

**The single best decision in harmonik's comms, which I am keeping verbatim** (salvage §2.5, citing `extract/10:338`): `--wake` injects **only** a fixed nudge string — *"You have a new comms message. Please check your inbox."* — **never the message body**. Delivery and notification are completely decoupled. The log is the source of truth; the pane poke is only a doorbell. Keep this regardless of transport.

**The proven sequence.** I read `internal/keeper/injector.go` at HEAD `0553d4b6` today [MEASURED]:

```
load-buffer -b hk-keeper-inject -    →  paste-buffer -b … -t <target> -d
   → settle (submitSettle)           →  send-keys -t <target> Enter
   → 2 bounded retries (submitRetryDelay), errors non-fatal
```

Its own comment explains the race, and it is the reason the settle exists:
> *"Settle so the REPL finishes ingesting the pasted text before the submit Enter; otherwise the first Enter races ahead and is dropped (hk-89g)."*

and the retry rationale, which is good engineering:
> *"Bounded retries defend against a dropped first keypress. Failures here are non-fatal — the line is already submitted by the first Enter on the happy path, and a redundant Enter is a harmless empty line."*

**What is fragile about it, honestly.** `internal/daemon/pasteinject.go:129` [MEASURED, verbatim]:
> *"on a REMOTE SSH worker under concurrent cold-boots, ~1/3 of runs hang because the seed paste is silently lost… tmux returns exit 0 once it has handed the buffer to the pane, **NOT once claude's React/ink TUI has rendered it**."*

**tmux exit 0 means "handed to the pane", not "the TUI accepted it". There is no positive acknowledgement primitive.** That is empirical knowledge bought with real failures across 2,632 lines of scar tissue, and it is the most valuable thing harmonik has to give this project.

Harmonik's own mitigation is to capture the pane and grep for a marker: `pasteVerifyAttempts = 3`, `pasteVerifyScrollback = 200` [MEASURED at `pasteinject.go:161-162`]. And note the trap salvage found: harmonik has **two** injectors and the comms one is the bad one — `cmd/harmonik/comms.go:448` fires load-buffer → paste-buffer → Enter with **zero settle and zero retries**, while its comment at `:440` claims it is *"the same approach used by keeper.InjectText"*. It is not. **The comment asserting a parity that doesn't exist is worse than the bug, because it tells the next reader not to look.**

**So the design, resolving the tension salvage flagged (§3.5: "a 1ms doorbell that fails 1/3 of the time vs a 60s poll that always works"):**

> **The poll is the contract. The doorbell is an optimisation. The doorbell is allowed to fail and nothing depends on it succeeding.**

Concretely:
1. Port `injector.go`'s sequence exactly — settle and bounded retries included. Do not port `comms.go:448`.
2. Add `pasteinject.go`'s capture-and-grep verification (3 attempts, 200 lines of scrollback), because tmux's exit code is not evidence.
3. Report the outcome as a *fact* in `DeliverResp.doorbell_rung`, never as an error. A failed doorbell is not a failed delivery — the message is on disk either way.
4. **Every agent polls its inbox on turn boundaries regardless.** For Claude Code that is the `Stop` hook (§3.2); it costs one local call per turn and it makes the doorbell's 1/3 failure rate a latency issue instead of a correctness issue.
5. Keep a test seam. `injector.go` has one (`tmuxRunFn` package var) so the sequence can be tested against a fake. Port that too.

**And one bug class I am deleting outright.** Harmonik derives the tmux target by naming convention with fallbacks and it has been bug-fixed twice — the captain has no `crew-` prefix so a third candidate pattern was bolted on, and the project hash *must* be computed on the `EvalSymlinks`-resolved path or the wake silently targets a pane that never existed [MEASURED via salvage §2.5]. **A derived pane target is a guess.** In this design `tmux_target` is a **registered fact** written at registration time by whatever launched the agent (§3.2). If it isn't registered, there is no doorbell, and `DeliverResp.doorbell_rung=false` says so out loud. No convention, no fallback chain, no silent miss.

### 2.6 Subscribe vs poll — §5's actual sentence

> *"all the messages are written down. If an agent is subscribed, then they get a notification. If they are polling, then they read their messages the next time they come in."*

This falls out of the above with nothing extra:

- **Written down** — `LogAppend("comms/inbox", sync:true)` on the receiving box, before the ACK. Non-negotiable, and it is the plugin's own choice per BRIEF §4, expressed as one bool at one call site. **That bool does not exist in the API today (F9)** — which is precisely why F9 is the top finding rather than a nit: the sentence *"all the messages are written down"* is the plugin's promise, and the plugin currently has no way to keep it.
- **Subscribed → notified** — `Follow` streams from the inbox log at the agent's cursor. Port harmonik's `SubscribeHub` back-pressure contract verbatim [MEASURED, `internal/daemon/subscribe.go:34-42`]: a non-blocking send into a 256-slot buffer; if full, **discard the oldest** and count it; emit a `subscription_gap` line carrying the accumulated drop count on the next successful send; **the emission goroutine is never blocked by a slow subscriber**. Three properties worth naming: the producer is never blocked by a slow consumer; drops are *counted, not hidden*; drop-oldest, not drop-newest, so a slow reader gets recent data rather than stale data. Plus a server-side heartbeat (default 60s) so a quiet stream still wakes the client — and `subscription_gap`/`heartbeat` are **connection-only, never written to the log**, because telemetry about the transport must not pollute the data.
- **Polling → reads next time** — `Inbox` scans the log from the cursor. Cursor semantics ported from `commscursor.go` [MEASURED, 323 lines]: cross-process `flock(LOCK_EX|LOCK_NB)` on a sidecar lock, bounded retry every 25ms up to 10s then **error rather than hang**, re-read the persisted cursor under the lock and refuse any write that isn't strictly greater (*"the cursor can only ever move forward"*), temp→fsync→rename, and a corrupt cursor treated as "no usable floor" so a well-formed advance can recover instead of wedging forever.

**Two warts I am fixing on the port, not inheriting:**
- `ScanAfter` (`jsonlwriter.go:312`) — which all of harmonik's comms read paths use — has **no torn-tail detection**, while `replayAndDetectTrunc` (`busimpl.go:1390`) does. Two readers, two behaviours. A partial last line after a crash must be a **signal**, not a silently skipped malformed line. In this design that check belongs in the *kernel's* `LogRead`, once, so no plugin can get it wrong.
- `recv` is an **O(entire log) linear rescan from the cursor, re-opening the file every call** (`jsonlwriter.go:314`). Invisible for one project. It is the scaling wall, and it arrives before search does. The kernel's log needs a seek index (offset per seq); ~40 lines, and it must exist from day one.

---

## 3. Plugin (b) — registry

> *"The idea of an agent list could probably also be a plugin"* (§3.2) — while the machine roster is core (§3.1). So this plugin sits on top of core's roster.

### 3.1 The kernel measurement that forces this split

Investigation 13 measured `MetaMaxSize = 512` in memberlist (`net.go:83`), pushed a 25-agent list (1,563 bytes) into node metadata, and got **silent truncation, `err=nil`, and corrupt JSON at the receiver** — and noted that without its own defensive clamp, `memberlist.go:460` would have **`panic("Node meta data provided is longer than the limit")` and taken the daemon down.**

This is not a footnote; it is the load-bearing argument for Greg's own instinct. **The agent list physically cannot live in the roster.** The roster carries boxes (≈330 bytes/node, [MEASURED headroom]); the agent list — names, summaries, cwd, tmux targets — is unbounded and changes constantly. A measured 512-byte cap forces the agent list out of the kernel and into a plugin, which is **exactly where BRIEF §3.2 puts it**. Greg's design instinct and the library's hard limit agree. That is the best kind of validation.

So the registry publishes to a **LOOKUP channel**, not to roster metadata.

### 3.2 Registration — three routes, union of them, never self-declared

An agent is a Claude Code / Codex / Pi process. It does not know this daemon exists. So registration is something done *to* it, and there are exactly three ways to learn about it. All three are mechanical.

**Route 1 — hooks (Claude Code only; best signal).** Harmonik's spec [CLAIMED — `specs/claude-hook-bridge.md:167`, i.e. harmonik's belief about Claude Code's config format] configures:
```json
"SessionStart": [{ "matcher": "", "hooks": [{ "type": "command",
   "command": "harmonik", "args": ["hook-relay","SessionStart"], "timeout": 30 }] }]
```
and the relay reads this off stdin [MEASURED — `internal/hookrelay/hookrelay.go:41-54`, real struct at HEAD]:
```go
type hookInput struct {
    SessionID      string `json:"session_id"`
    HookEventName  string `json:"hook_event_name"`
    TranscriptPath string `json:"transcript_path"`   // ← absolute path, handed to us
    CWD            string `json:"cwd"`
    PermissionMode string `json:"permission_mode"`
}
```
Known event kinds [MEASURED — `hookrelay.go:33-39`]: `SessionStart`, `Stop`, `SessionEnd`, `StopFailure`, `Notification`.

**`transcript_path` arrives in every hook payload, and salvage noted nothing tails it** — that is the cheap route to presence, and it means the registry never has to *derive* a path (§4.2 explains why deriving is fatal).

Steal the connection regime verbatim too [MEASURED — `hookrelay.go:11`]: one-shot NDJSON, dial timeout ≤5s, message ≤1MiB, ack-line read with a 5s deadline, then close. A hook that blocks is a hook that hangs the agent.

**Honest caveat:** `~/.claude/settings.json` on this box has **no hooks configured today** [MEASURED — the file contains only `skipDangerousModePermissionPrompt`, `theme`, `tui`, `enabledPlugins`, `extraKnownMarketplaces`]. So route 1 is *available* but not *installed*. And I found no evidence Codex or Pi have a hook mechanism at all — I did not look hard, and it is an open question.

**Route 2 — discovery (all vendors; always works).** The logtail plugin (§4) watches the vendor session directories. A new `.jsonl` file *is* a new session, and its first line carries the identity. All three, [MEASURED] today:

| Vendor | Path | First-line facts |
|---|---|---|
| Claude Code | `~/.claude/projects/<slug>/<uuid>.jsonl` | `sessionId`, then per-line `cwd`, `gitBranch`, `version`, `entrypoint` |
| Codex | `~/.codex/sessions/YYYY/MM/DD/rollout-<ISO>-<uuid>.jsonl` | `session_meta`: `session_id`, `cwd`, `originator`, `cli_version`, `model_provider`, `source` |
| Pi | `~/.pi/agent/sessions/--<slug>--/<ISO>_<uuid>.jsonl` | `session`: `id`, `cwd`, `timestamp`, `version` |

Real examples, verbatim from the fleet:
```json
{"type":"session_meta","payload":{"session_id":"019f6709-4b42-73c1-9e9a-250a6bd7712a",
 "cwd":"/Users/gb/github/gitnaut-codex-visual-harness","originator":"codex_exec",
 "cli_version":"0.142.0","source":"exec","model_provider":"openai"}}          ← Codex
{"type":"session","version":3,"id":"019f29b2-b468-784e-8f41-acbe3c7226ed",
 "timestamp":"2026-07-03T20:36:45.288Z","cwd":"/Users/gb/harmonik-worker/repo/…"} ← Pi
```
**Every vendor hands you `cwd` and a session id in the first line of the file.** Discovery needs nothing from the vendor beyond what it already writes. This route requires zero cooperation, zero config, and works for vendors that will never have hooks.

**Route 3 — launcher (adds what neither of the others knows).** Whatever starts the agent in a tmux pane registers `tmux_target` — the one fact that cannot be observed from the transcript, and the one the doorbell needs (§2.5).

**The rule: union of what is known, never a self-report.** An agent claiming "I am claude-github-3 and I am working" is exactly what harmonik's bug #3 punishes — see §3.4. Every field in the registry is written by a hook, observed on disk, or supplied by the launcher.

### 3.3 The agent name — §4's "daemon-defined name"

> *"a **daemon-defined name** is the key/domain name; box name, IP, and other attributes hang off that entry."* (§4)

Investigation 14 flagged this as an open question and it is genuinely mine to answer: *how does an agent get a durable name across restarts?*

**The name is minted by the registry, and it is keyed on the agent's `(node, tmux_target)` if known, else `(node, vendor, cwd)`.**

```
claude-github-a7f
└─┬──┘ └─┬──┘ └┬┘
  │      │     └── 3 hex chars, minted once, checked against the lookup table
  │      └──────── basename of cwd — human-recognisable, which is the whole point
  └─────────────── vendor
```

Three properties, and each is a decision:

- **It survives `/clear` and session-resume.** Claude Code mints a *new session id* on `/clear` but the pane and cwd don't change. Keying the name on the session id would rename the agent mid-conversation. Keying it on `(node, tmux_target)` means the name is stable for as long as the *seat* exists, which is what a human means by "that agent over there". The session id becomes an *attribute* that changes underneath a stable name.
- **The box is an attribute, not part of the name** — literally as §4 asks. `claude-github-a7f` never contains `dgx`. Routing does `name → node → Tailscale IP`, all local map reads.
- **Uniqueness without consensus.** The 3-hex suffix is minted locally after a read of the lookup table. Two boxes *could* mint the same name in the same instant — 1 in 4,096, per vendor, per project basename. **I am accepting that risk rather than importing consensus, and here is what happens when it fires:** both rows exist in the table under one key, `LookupGet` returns two, and `Send` fails with `AMBIGUOUS_NAME{nodes:["dgx","gb-mbp"]}` telling the sender exactly what happened. It degrades visibly, it never silently misroutes, and `registry rename` fixes it in one command. BRIEF §6 says *"I dont want to get hung up on CAP theorem and shit"* — this is what taking that seriously looks like: name the risk, bound it, make it loud, move on.

Erlang's `global` is the prior art here and it is instructive as a *warning*: it needs a resolver function and *"If the function crashes, or returns anything other than one of the pids, the name is unregistered"* [CLAIMED — erlang.org, via investigation 14]. **You can lose the name entirely.** A visible ambiguity error beats an automatic resolution that can delete your identity.

### 3.4 Presence — mechanically detected, never self-declared

The empirical foundation is harmonik bug #3: **presence is outbound-only, so an actively-*receiving* agent reads OFFLINE** — `EffectiveLastSeen = max(beat, last send)` in `internal/presence/presence.go` [CLAIMED — verified by the prior round in crew `stilgar`; salvage did not re-run it, and neither did I]. An agent that spends an hour reading and thinking is, by that formula, dead. **The lesson: presence must come from what you can *observe about* the agent, not from what the agent *tells* you or from what it happens to *send*.**

**Two clocks** (salvage row 21, survives because it rests on that measured bug, not on the dead framing):

| Clock | Question | Source |
|---|---|---|
| **Lease** | Is this registration still valid? | process alive (pid signal-0), or `SessionEnd` hook, or the roster says the node is gone |
| **Activity** | Is it doing anything? | **the transcript file grew** |

**The transcript is the heartbeat, and the vendor writes it for free.** No cooperation, no agent-side code, no self-report — the file's size and mtime are facts on disk. This is salvage row 20, and it is the correct answer.

Turn boundaries are also observable, per vendor, [MEASURED] today:

| Vendor | "working" | "idle" |
|---|---|---|
| Claude Code | file grew in last 5s | `Stop` hook fired; or last line `type:"assistant"` with no growth for 30s |
| Codex | `event_msg.payload.type == "task_started"` (carries `turn_id`, `started_at`) | `task_complete` (carries `turn_id`, `last_agent_message`) |
| Pi | file grew | last `message` line, no growth |

Codex's `task_started`/`task_complete` are clean mechanical turn boundaries **inside the file** — [MEASURED, verbatim]:
```json
{"type":"task_started","turn_id":"019f6709-4bf6-76a3-8256-89c266267c82","started_at":1784140090,…}
{"type":"task_complete","turn_id":"019f6709-4bf6-76a3-8256-89c266267c82","last_agent_message":"Implemented Unit 11.…"}
```

The state machine, entirely from observed facts:

```
working  : transcript grew within activity_window (5s)
idle     : no growth ≥ 5s AND last line is a turn-end AND process alive
stale    : no growth ≥ 15m AND process alive          ← the interesting one
offline  : process gone, OR SessionEnd hook, OR roster says the node is dead/asleep
```

`stale` is the state that makes §2.4's rung 4 useful: it is how the sender learns "that agent is alive but nobody's home."

**Presence is computed locally and published as a fact; it is never gossiped as an opinion.** Investigation 13 makes the same argument for the roster and it applies here identically: each box observes only its *own* agents' files and publishes rows for only its own agents. No box ever forms an opinion about another box's agents. There is nothing to reconcile, so there is no conflict to resolve.

### 3.5 The summary — and this is the find of the day

> *"they can also sync agent lists and maybe **a summary of what the agent is doing**"* (§3.1)

The requirement is a summary; the constraint (BRIEF §2, §6) is that the daemon must not think. Generating a summary is thinking. This looked like the hardest requirement in my subsystem.

**It is already solved, on disk, by the vendor.** [MEASURED, today]:

```json
{"type":"ai-title","aiTitle":"Set up worker node for Harmonik","sessionId":"48280737-…"}
```

**Claude Code writes a natural-language title of what the session is about, into the transcript, itself.** It is present in **55 of 60** recent transcripts I checked [MEASURED]. The daemon reads a field. Zero cognition. That is *"a summary of what the agent is doing"*, delivered exactly, requiring nothing but a `grep`.

**Honest, and important: this is uneven across vendors.** I checked all three [MEASURED]:

| Vendor | Vendor-written summary | Fallback |
|---|---|---|
| Claude Code | ✅ `ai-title` line — a real title | — |
| Codex | ❌ none (`turn_context.summary` is the string `"auto"` — a config setting, not a summary) | `task_complete.last_agent_message`, truncated — the agent's own words about what it just did |
| Pi | ❌ none | last assistant `message`, truncated |

So the rule: **vendor-written when the vendor writes one; the agent's own most recent words when it doesn't; observed facts always; synthesised never.**

```proto
message Summary {
  string text   = 1;  // aiTitle | last_agent_message | first user prompt, truncated
  string source = 2;  // "vendor:ai-title" | "vendor:last-agent-message" | "observed:first-prompt"
  google.protobuf.Timestamp as_of = 3;
}
```

**`source` is not decoration — it is the honesty field**, and it is what stops this from drifting into cognition later. A reader can always tell whether the vendor said it or whether we picked a line out of the file. If a future version wants a *better* summary, that is an LLM call, and it belongs in a *different plugin* that writes into the same table — which, as investigation 12's salvage of design/24 noted, is structurally guaranteed by subprocess plugins: *a plugin that shells out to an LLM is a different process by definition.* "No cognition in the daemon" stops being a coding rule and becomes a property of the deployment.

### 3.6 The row

Published to the `registry.agents` LOOKUP. Key = the agent name. Each node writes only its own rows.

```proto
message AgentRow {
  string name       = 1;  // "claude-github-a7f" — THE KEY. daemon-defined (§4).
  string node       = 2;  // "dgx" — an ATTRIBUTE. roster resolves it to an IP.
  string vendor     = 3;  // "claude-code" | "codex" | "pi"
  string session_id = 4;  // vendor's id. Changes on /clear; the name does not.
  string transcript = 5;  // absolute path, from the hook or from discovery. NEVER derived.
  string cwd        = 6;  // measured: every vendor gives this in line 1
  string git_branch = 7;  // measured: Claude gives it per line
  string tmux_target= 8;  // registered fact, not a guess (§2.5). "" = no doorbell.
  int32  pid        = 9;
  string state      =10;  // working|idle|stale|offline — observed (§3.4)
  Summary summary   =11;  // vendor-written or observed (§3.5)
  google.protobuf.Timestamp registered_at = 12;
  google.protobuf.Timestamp last_activity = 13; // transcript mtime. LOCAL clock.
}

// comms.cursors — a SEPARATE channel, owned by the comms plugin. Key = agent name.
message InboxCursor { uint64 cursor = 1; uint64 head = 2;   // head-cursor = cursor_lag
                      google.protobuf.Timestamp last_read = 3; }
```

**Those last two fields used to be in `AgentRow`, and the real kernel is why they aren't.** My first draft had comms writing `inbox_cursor`/`inbox_head` into the registry's row, with the registry owning the rest — and I flagged my own discomfort about it. The real API makes it **unrepresentable**: `PluginManifest.namespace` *"owns `<namespace>.*` and its own storage"*, and a `ChannelDecl.name` *"must sit inside the plugin's own namespace"* [MEASURED — `plugin.proto:16,38`]. `registry.agents` belongs to the registry. Comms cannot write a field of it, ever.

So comms publishes its own `comms.cursors` channel and §2.4's rung-4 lookup becomes **two** local map reads instead of one — microseconds either way. **I wanted the shared row for convenience; the kernel removed the option.** That is the boundary doing its job, and it is why **F6** in §5.1 is a resolved finding rather than an open one.

---

## 4. Plugin (c) — logtail and shipping

> *"the logs will go on the transport, get dumped, then something will expose them for search (probably in multiple ways)"* (§4)

Search is not my problem. **Delivering clean data with good metadata is.** This section is the contract a future search layer consumes.

### 4.1 Volume — measured, and it changes exactly one thing

I measured the real corpus today rather than inheriting a number. First, the honest correction: the obvious method is wrong. `find -mtime -1` + sum file sizes gives **82.6 MB/day**, but that counts *whole files* that were merely *touched*, including bytes written days ago. The right method is to sum the byte length of lines whose *own* timestamp falls on that day. [MEASURED, all three vendors, gb-mbp]:

| Day | Bytes appended | Lines |
|---|---|---|
| 2026-07-15 | **62.9 MB** | 21,057 |
| 2026-07-14 | 15.9 MB | 5,564 |
| 2026-07-13 | 0.0 MB | 0 |
| 2026-07-12 | 38.0 MB | 10,760 |
| 2026-07-11 | 22.0 MB | 5,716 |

| Corpus fact | Value |
|---|---|
| **Transcripts, all vendors, gb-mbp** | **264.9 MB / 1,008 files** — Claude 193.3 MB/804 files/203 project dirs; Pi 57.4 MB/186; Codex 14.2 MB/18 |
| Mean file | 268 KB |
| Largest single transcript | **6.1 MB** |
| **Largest single line** | **309,007 bytes (0.29 MB)** |
| **Non-transcript files sitting in `~/.claude/projects`** | **73.4 MB / 433 files** — 331 `.json`, 79 `.txt`, 10 `.pdf`, 9 `.md`, 4 `.js` |
| dgx corpus | 13 MB / 40 files, **3.2 TB free** |
| gb-mac-mini corpus | **unknown — `ssh` → `Host key verification failed`** |

*(Methodology note, because I got it wrong first: `du -sh ~/.claude/projects` says `270M`, but that counts the whole directory. The transcripts are 193.3 MB; the other 73.4 MB is attachments the vendor parked next to them. `du` and "bytes of JSONL" are different questions and the gap is 40%. Numbers above are `stat`-summed actual bytes of `*.jsonl` only.)*

**The attachments are a finding, not noise.** A `*.jsonl` glob ignores them correctly, so logtail is unaffected — but 433 files including PDFs live inside the vendor's session directory, and transcripts reference them. A future search layer will want them and **they are not transcripts**, so they are not this plugin's job. Noted so nobody later "fixes" the glob to `*` and starts shipping PDFs down a channel with a 1 MB payload cap.

**What it changes: nothing about bandwidth, one thing about line size.**

- **Bandwidth is a non-issue.** 63 MB/day peak ≈ **730 bytes/sec average**. Investigation 11 measured 900 KB crossing the tailnet in 101 ms. This is four orders of magnitude of headroom. Nobody needs to be clever.
- **Storage matters at the archive, and it decides *which box*.** Full-fleet replication at the peak rate ≈ 190 MB/day ≈ 69 GB/year. dgx has **3.2 TB free** [MEASURED today] — decades. gb-mac-mini has 37 GiB free (§1) — **about 6 months.** So dgx is the archive on capacity grounds, and the mini can mirror only with a retention policy. That is a *capacity* argument, not an architecture argument, which matters for §4.3.
- **The one real constraint: a single line hit 309 KB, and there is no ceiling on it.** A tool result containing a large file read can exceed 1 MB. Investigation 11 confirmed by reading the source that NATS caps payloads at `MAX_PAYLOAD_SIZE = 1MB` (`const.go:94`), warn-capped at 8 MB. **A line over the cap is a silent shipping failure on a bad day.** So:

> **Lines over 256 KB ship as a reference, not as bytes:** `{node, path, offset, length, sha256}`. The archive fetches the body over request/reply and verifies the hash.

This is Istio's ECDS pattern, which investigation 14 found independently converged on by Nomad: the control plane ships a **pointer**, the local agent fetches and verifies the bytes, and *"missing checksum will cause the Wasm module to be downloaded repeatedly"* [CLAIMED — Istio docs, via inv. 14]. Same shape, same reason. It also means the transport never has to care about the payload cap, which keeps §5's transport question from being contaminated by log volume.

### 4.2 Discovery — never derive a path

**The rule: discover paths by watching directories. Never compute one from a workspace path.**

The evidence is a measured production bug and a measured schema difference.

Salvage §4 root-caused harmonik's `DeriveCIaudeTranscriptPath` (`internal/handler/claudehandler_chb006_024.go:696`): it compiled the function verbatim and ran it — **0 of 3 derived directories exist; 3 of 3 corrected ones do.** Two independent defects: `strings.TrimPrefix(slug, "-")` strips a leading hyphen that Claude Code actually *keeps*, and dots are never mapped. I re-verified the premise independently today: **203 of 203 Claude project dirs begin with `-`** [MEASURED].

And here is why no amount of careful fixing saves the approach — **the slug rules are per-vendor and they disagree.** Same workspace, two vendors, [MEASURED]:

```
/Users/gb/harmonik-worker/repo/.harmonik/worktrees/019f29a6-…
  Claude → ~/.claude/projects/-Users-gb-harmonik-worker-repo--harmonik-worktrees-019f29a6-…
                              ^leading '-'          ^^ dot became '-'
  Pi     → ~/.pi/agent/sessions/--Users-gb-harmonik-worker-repo-.harmonik-worktrees-019f29a6-…--
                                ^^leading '--'      ^ dot PRESERVED        trailing '--'^^
```
[MEASURED: 203/203 Claude dirs start with `-`; 27/27 Pi dirs start with `--`.]

Two vendors, two incompatible escaping rules, both undocumented, both free to change in any release. **A path deriver is a bug with a release schedule.** Discovery is immune: watch the directory, read `cwd` out of line 1, and the vendor's escaping rule becomes irrelevant because you never inverted it.

```go
// Watch roots. Everything else falls out of the file itself.
var roots = []Root{
  {vendor: "claude-code", glob: "~/.claude/projects/*/*.jsonl"},
  {vendor: "codex",       glob: "~/.codex/sessions/*/*/*/rollout-*.jsonl"},
  {vendor: "pi",          glob: "~/.pi/agent/sessions/*/*.jsonl"},
}
```
Hooks are strictly better where available (`transcript_path` is *handed* to you [MEASURED, `hookrelay.go:44`]) and discovery is the floor that always works. Use both; they agree or you have found a bug worth knowing about.

### 4.3 Shipping — and the hub question, argued rather than assumed

The task asks the sharp question: *if logs "get dumped" somewhere, is that a hub, and does that violate §4?*

**First, the test, because "hub" is being used loosely.** BRIEF §4 says: *"no single box's death may kill the system."* So the test is not "is there a box that has more stuff on it". The test is: **if that box dies, does anything stop?**

| Design | dgx dies. What stops? | Verdict |
|---|---|---|
| Logs *route through* dgx | every log line stops; comms stops if it shares the path | **fatal hub. Forbidden.** This is BRIEF §9's mistake. |
| dgx *subscribes to* logs that already exist on each node | **nothing.** Each node still has its own complete log. No agent blocks, no send fails, no line is lost. Search goes stale until dgx returns and catches up from its cursors. | **not a hub. Allowed.** |

> **§4 forbids a box whose death *stops* the system. It does not forbid a box whose death *degrades a consumer*.** And BRIEF §4 explicitly makes search a consumer: *"We handle search later - we can solve search once we have a data backbone."*

**The mechanism that makes this true, and it is the whole point: the archive PULLS. It does not receive.**

The source of truth is the per-node log, which exists whether or not anyone ever reads it — the transcript is already on the local disk because the vendor wrote it there, and the local kernel log indexes it. The archive is a **cursor over other nodes' logs**. If the archive is down, its cursor simply doesn't advance. Producers never know. There is no queue to fill, no buffer to overflow, no backpressure to propagate — because the producer was never pushing.

If push were the design, the producer would have to hold undelivered lines *for* the archive, and the archive's absence would become the producer's problem. That is how a storage destination quietly turns into a hub. **Pull is the property; the rest is bookkeeping.**

```
each node:                                  archive (any node that opts in):
  logtail tails ~/.claude/projects/…          for each peer in roster:
    → LogAppend("logtail/lines")                cursor = KVGet("archive/"+peer)
    → Publish("logs.announce",                  loop:
        {node, log, head_seq})   ─────────►       Request("logs.pull",
        (a hint, not a delivery)                    {log, after: cursor, max: 512})
                                                  ← LogRecords
                                                  write locally; KVPut(if_revision)
```

- `logs.announce` (**PUBSUB**) is a **hint** — "I have new data" — so the archive doesn't have to poll blindly. **Losing it costs latency, never data**, because the puller's fallback is a 30s timer and the cursor is authoritative. This is deliberately the same discipline as the comms doorbell (§2.5): *the cursor is the contract, the notification is an optimisation.* Two subsystems, one rule, and it is the rule that makes both of them tolerant of a transport that drops things. The zeromq investigation measured mangos PUB/SUB delivering **1,094 of 100,000** messages with **0 send errors** — a 98.9% silent loss. A design where a lost announce costs data would be destroyed by that. This one shrugs.
- `logs.pull` (**REQUEST_REPLY**) does the actual moving, paginated, driven by the consumer's cursor.
- **Multiple archives need zero coordination.** Two subscribers = two independent cursors = redundancy with no consensus, no leader, no split-brain. Nothing to elect. If Greg wants the mini to mirror the last 90 days and the dgx to keep everything, that is two config lines and no design change.

**Verdict: dgx is the archive.** On measured capacity (3.2 TB vs 37 GiB) and because it is always-on. It is a **storage destination, not a transport hub**, and the pull discipline is what earns that distinction. If anyone later proposes that producers *push* to the archive, that is the moment this becomes the thing BRIEF §9 warns about, and the reviewer should reject it on those grounds.

### 4.4 The metadata schema — the contract for search

> *"Metadata — the machine, time, etc — must ride along because search will need it."* (§4)

Everything here is either an observed fact or a vendor-written field. [MEASURED] means I read this exact key out of a real transcript today.

```proto
package fleet.logtail.v1;

message LogLine {
  // ---- identity / ordering ----
  string node        = 1;  // "gb-mbp" — §4's "the machine". Roster name, not hostname.
  uint64 node_seq    = 2;  // per-node monotonic. THE ordering key, WITHIN this node.
  string line_id     = 3;  // sha256(node|path|offset)[:16]. Dedupe. Not an ordering key.

  // ---- where it came from ----
  string vendor      = 4;  // "claude-code"|"codex"|"pi"          [MEASURED: 3 real dirs]
  string path        = 5;  // absolute transcript path. Discovered, never derived (§4.2).
  uint64 offset      = 6;  // byte offset in that file — so you can go back to the source
  uint32 length      = 7;

  // ---- what it is about ----
  string agent       = 8;  // registry name, "claude-github-a7f" — joins to (b)
  string session_id  = 9;  // [MEASURED: claude .sessionId | codex .payload.session_id | pi .id]
  string project     =10;  // basename(cwd)                        [MEASURED: all 3 vendors]
  string cwd         =11;  //                                      [MEASURED: all 3 vendors]
  string git_branch  =12;  //                                      [MEASURED: claude only]
  string vendor_ver  =13;  // [MEASURED: claude "2.1.181" | codex cli_version "0.142.0" | pi version 3]
  string model       =14;  // [MEASURED: codex turn_context.model "gpt-5.5"; claude in message]

  // ---- when ----
  google.protobuf.Timestamp vendor_ts   = 15; // the line's own timestamp. DISPLAY ONLY.
  google.protobuf.Timestamp observed_at = 16; // when we tailed it. DISPLAY ONLY.

  // ---- the payload ----
  string line_type   = 17; // [MEASURED claude: user|assistant|system|summary|ai-title|
                           //  file-history-snapshot|mode|permission-mode
                           //  codex: session_meta|response_item|event_msg|turn_context
                           //  pi: session|message|model_change|thinking_level_change]
  bytes  raw         = 18; // the vendor's line, BYTE-EXACT. Never reformatted.
  BodyRef ref        = 19; // set INSTEAD of raw when length > 256 KB (§4.1)
}

message BodyRef {           // the >256KB escape hatch. Pointer + checksum, Istio/Nomad style.
  string node = 1; string path = 2; uint64 offset = 3; uint32 length = 4; string sha256 = 5;
}
```

Four decisions worth defending:

1. **`raw` is byte-exact and never reformatted.** Every vendor will change its schema without telling us. A normalised-only pipeline silently drops the new fields; a raw-preserving one lets a future parser re-derive them from history. The typed fields above are a *projection for search*, not a replacement for the data. This is also why protobuf's *"Use binary; avoid using text formats for data exchange"* rule matters [CLAIMED — protobuf.dev, via inv. 14]: unknown fields survive a binary round-trip and are **lost through JSON**. Envelopes must be binary protobuf on every daemon↔daemon hop; JSON at the edge is fine, just never round-trip an envelope through it.
2. **`node_seq` is the ordering key and it is per-node.** Salvage rows 34–35 again. Search will want a time axis, and it must be given `vendor_ts` clearly labelled as *display*. If a future search UI sorts a cross-machine result set by `vendor_ts`, it will produce a plausible, wrong story on three unsynchronised clocks. **Say it in the schema comment so the search author cannot miss it** — which is what those comments are for.
3. **`agent` joins to the registry, but the line ships whether or not the join resolves.** If the registry hasn't minted a name yet, `agent` is empty and `session_id` still identifies it. **A missing registry must never stop the data.**
4. **`line_type` is the vendor's own word, not our taxonomy.** I listed the measured values rather than inventing an enum. The instant we map `claude:assistant` and `pi:message` onto one invented `AGENT_OUTPUT` constant, we have put domain knowledge in the pipeline and we will be wrong about the fourth vendor. Let search decide what these mean; ship the vendor's word.

### 4.5 State lives in the kernel, because reload wipes the plugin

Investigation 12 measured the cost of live reload and it is absolute: **go-plugin reload ×5, reading back state written before each reload — `state after reload: ""` every time.** Vault reached the same conclusion in production: *"Plugins don't maintain persistent in-memory state... State management relies on Vault's storage backend."* [CLAIMED — HashiCorp docs, via inv. 12].

So logtail's per-file byte offsets go in kernel storage (`KVPut("logtail/offset/"+path, offset)`), not in a map in the plugin. **BRIEF §3.1's storage requirement and BRIEF §3.2's live-reload requirement are the same requirement seen from two sides** — Greg asked for both independently without connecting them, and the connection is that you cannot have the second without the first. Reload comms while it holds the queue in memory and you lose the queue. Enforce it day one:

> **Rule: a plugin is a pure function of kernel-held state plus its inputs.** logtail keeps offsets in the kernel. comms holds nothing (BRIEF §5 already said *"all the messages are written down"*). The registry holds nothing (it can rebuild every row by re-reading the transcript dir — which is worth stating as a test: *kill the registry, restart it, does the table repopulate?*).

---

## 5. Does the kernel API survive?

**Yes — with one contradiction that must be fixed, and it is fixed by adding one bool.**

All three plugins expressed cleanly. The API is small, the boundary held, and it refused me twice when I tried to cross it (F6, and the `content_type` field it was right to omit). **The one place it contradicts a stated requirement is `LogAppend`, which offers no durability control while BRIEF §4 says durability is the plugin's decision — see F9.** That is not a philosophical objection; §2.3's store-and-forward is only correct if the message is on disk before `Send` returns, and today the plugin cannot ask for that.

**The thing that makes the whole design work is store-and-forward** (§2.3), and it is not a decision about the transport. BRIEF §5 wants messages written down; BRIEF §4 forbids a fatal hub *and* forbids the kernel from owning durability. Those three look like a contradiction, and the last round resolved it by building a hub (BRIEF §9). **They are only contradictory if there is one log.** Once each node writes its own copy before transmitting, the kernel never needs a durability *feature*, so the channel API stays two-axis `(name, type)` — exactly as Greg drew it. The zeromq investigation reached this from the transport side and named the missing third axis (durability) as "the entire project"; my three plugins say the same thing from the other end: **the third axis never appears in the kernel because the plugin already paid for it.** Greg's instinct that a channel is `(name, type)` survives contact, and it survives *because* of his other instinct that durability is the plugin's decision. The two rules hold each other up — which is exactly why F9 matters: **it is the one place the kernel takes back the decision that makes the rest of the architecture legal.**

Now the honest list, **against the real proto**. Of the eight gaps I found while designing blind, **five were already closed by the kernel author, one is closed better than I proposed, and two survive.** Then there is a ninth I only found *because* the real API arrived — and it is the important one.

### 5.1 The falsification list

| # | Finding | Status vs the real API | What to do |
|---|---|---|---|
| **F9** | **`LogAppend` has no durability control, so "durability is a plugin decision" is not currently true.** `message LogAppendRequest { string log = 1; repeated bytes records = 2; }` — there is no `sync` flag, no `if_durable`, nothing [MEASURED — `kernel.proto:161`]. The plugin cannot say "fsync this one before you answer me". It gets whatever policy the kernel's log happens to implement. **BRIEF §4 is explicit: *"Durability is a PLUGIN decision, not a kernel guarantee: 'How about: the plugin defines that! Then we can have multiple options.'"*** The kernel's own comment even says *"Replication, like durability, is a plugin decision"* (`kernel.proto:134`) — but the API it sits above doesn't offer the choice. And this is not theoretical for me: **§2.3's entire store-and-forward correctness rests on the message being on disk before `Send` returns.** Without it, a box that dies between accept and forward loses a message that the sender was told was safe. | **OPEN — and it contradicts a stated requirement** | Add `bool sync = 3;` to `LogAppendRequest` and `bool synced` to the response. Per-call, not per-log, and **not a daemon-side policy map**: salvage row 37 is the measured warning — harmonik's hand-maintained `fsyncBoundaryEventTypes` map (`busimpl.go:142`) drifted and **silently downgraded durability**, precisely because the daemon owned the decision. The caller says `sync:true` at the call site or it doesn't. |
| **F1** | **Plugin APIs are channel+bytes, so they are not curl-able — and that was the reason for protobuf.** The kernel resolves the local-ingress problem elegantly (§1.6): the CLI calls `KernelService.Request(channel:"comms.send", payload:…)`. But `payload` is `bytes`. Greg's stated reason for protobuf (§3.2) is *"the same interfaces could be available through REST/pubsub/whatever"*, and investigation 11 measured what that's worth: `curl -d '{"query":"how to ssh to dgx"}'` returning `{"text":"found: …"}` from a real endpoint. Under channel+bytes you instead curl `KernelService/Request` with a **base64 blob**, and neither the kernel nor `curl` knows `comms.send` takes a `SendRequest`. **The typed, self-describing, curl-able property survives at the kernel's edge and dies at every plugin's edge — which is where all the actual functionality lives.** | **OPEN — narrower than I thought, but real** | Cheapest fix: let `ChannelDecl` name the request/response message types (`string request_type = 5; string response_type = 6;`), register those descriptors, and have the ingress transcode JSON↔protobuf for `REQUEST_REPLY` channels. That is ConnectRPC + a descriptor registry, and it restores `curl -d '{"to":"pi-refactor-3","body":"hi"}' …/comms.send`. **Alternative worth considering: accept it, and let each plugin ship a CLI.** But then "define the interface completely independently of any code" buys less than Greg thinks it does, and he should be told that in one sentence rather than discovering it. |
| **F2** | **LOOKUP is not optional, and neither candidate transport gives it.** Comms must resolve `agent name → node` without calling the registry plugin. Core NATS can't without JetStream (*"nats: jetstream not enabled"* [MEASURED, inv. 11]), and JetStream is disqualified by §4. Mangos has no equivalent, and ZMQ's answer (the Clone pattern) *"exists in C/C++/Java/Python/Tcl — not Go"* [MEASURED, inv. 10]. So the kernel must **build** it. | **CLOSED** — `CHANNEL_TYPE_LOOKUP` exists, with *"single-writer-per-key BY CONSTRUCTION"* | Nothing. But note for whoever sizes the work: this is the one channel type with no upstream implementation to lean on, so it is real code (my estimate: 150–250 lines of per-node-owned rows over pubsub + reconcile). Greg listed *"publish/lookup table"* **first** in his §3.1 menu and his instinct was right — it turns out to be the type that carries the most weight and the only one nobody gives you free. |
| **F3** | **"A storage mechanism" (§3.1) is three things, and one is non-obvious.** All three plugins need an append-only **log**, a **cursor** store, and a **KV**. | **CLOSED, and better than my version** | Kernel ships Log + KV and makes cursors *a KV convention* — *"cursors live in KV, not in a plugin's own file"* — with `optional uint64 if_revision` compare-and-swap. **That deletes 323 lines** (`commscursor.go`'s flock/temp/rename/corrupt-recovery) because the daemon is now the single writer and CAS replaces the lock (§1.5). Remaining asks, both inside the kernel: port `jsonlwriter.go`'s batching drainer (one `write`, one `fsync` per burst — P99 becomes O(1×fsync) instead of O(N×fsync)), add **torn-tail detection** to `LogRead` (harmonik has two readers with two behaviours — `ScanAfter` lacks it, `replayAndDetectTrunc` has it), and add a **seek index** to kill the O(entire log) rescan (`jsonlwriter.go:314`). |
| **F4** | **The roster must expose change events, not just a snapshot.** Comms's outbox drains on node-join; without events it's a poll. | **CLOSED** — `RosterWatch` streams `KIND_JOIN/LEAVE/UPDATE/DEAD`, and `RosterInterest` is a declarable manifest interest | Encode one measured gotcha in the implementation: memberlist's `NotifyLeave` reports `State=0` (Alive) even for a *graceful* leave, so **you cannot read `n.State` in the callback** to tell "left" from "dead" [MEASURED, inv. 13]. The kernel's separate `KIND_LEAVE` vs `KIND_DEAD` is the right shape — just make sure it isn't derived from `n.State`. |
| **F5** | **Log shipping needs streaming or pagination.** | **CLOSED** — `LogRead` is server-streaming and has `bool follow = 4` | Nothing. `follow` is nicer than the pagination I designed; §4.3's puller uses it directly. |
| **F6** | **Two plugins wanted to write one row** (comms's cursor fields inside the registry's `AgentRow`). | **CLOSED — the kernel made it unrepresentable** | Nothing. `namespace` *"owns `<namespace>.*` and its own storage"* and channel names must sit inside it, so comms publishes `comms.cursors` and joins locally (§3.6). I wanted the shared row for convenience; convenience is how domain knowledge gets into kernels, and the boundary refused it. |
| **F7** | **Nothing bounds a plugin's payload, and the transport does.** A 309 KB line is measured real (§4.1); >1 MB is possible; NATS caps at 1 MB (`const.go:94` [CONFIRMED-CODE, inv. 11]). | **OPEN — minor** | `InfoResponse` carries `node`, `api_version`, `kernel_version`, `namespace` — **no `max_payload_bytes`** [MEASURED — `kernel.proto:225-230`]. Add it, and return a typed `PAYLOAD_TOO_LARGE`. §4.1's 256 KB reference-instead-of-bytes rule is logtail's answer, but the plugin must be able to *read* the limit rather than hardcode a guess about a transport that isn't chosen yet. |
| **F8** | **`Stop` must drain.** | **CLOSED** — `StopRequest { uint32 grace_ms = 1; }` | Nothing. And store-and-forward means the worst case was only ever a duplicate delivery, which `message_id` dedupes. |
| **F10** | **Naming nit:** `LogAppendRequest.log` and `LogTruncateRequest.log` vs `LogReadRequest.journal` [MEASURED — `kernel.proto:161,163,175`]. Same concept, two names, in one file. | Trivial | Pick one. Ten seconds now, or a small confusion forever in every plugin that does both. |

### 5.2 The channel-type menu is too long — and two of us found that independently

Tally of what my three plugins actually use:

| Type | comms | registry | logtail | Used? |
|---|---|---|---|---|
| **LOOKUP** | ✅ resolve name→node (`registry.agents`), publish `comms.cursors` | ✅ publishes the agent table | — | **yes — the most load-bearing type** |
| **REQUEST_REPLY** | ✅ `comms.deliver`, `comms.receipt`, `comms.send` (from the CLI) | — | ✅ `logs.pull` | **yes** |
| **PUBSUB** | — | — | ✅ `logs.announce` | **yes** |
| **POINT_TO_POINT** | ✗ | ✗ | ✗ | **no consumer** |
| **FANOUT** (Greg's 5th) | ✗ | ✗ | ✗ | **no consumer — and the kernel already dropped it** |

**Fanout is redundant, and I did not have to argue it — the kernel author got there first, from the other side.** `kernel.proto:19-22`: *"Subscribing to an exact name is what people call 'fanout'; subscribing to a pattern is what people call 'pubsub'. Same primitive, so one type."* I reached the same place by tallying consumers and finding none. **Two agents, two methods, same answer.** The zeromq investigation flagged the pubsub-vs-fanout ambiguity as worth 30 seconds of asking Greg; the answer is that it doesn't matter, because it's redundant on either reading — if fanout means broadcast, PUBSUB does it; if it means exactly-one-of-N, POINT_TO_POINT does it.

**Point-to-point has no consumer either, and that one is still in the kernel.** There are no worker pools here: agents are addressed by name, not load-balanced. Nothing in BRIEF §3.2's plugin list (comms, registry, log tail, log archiving, SSH helper, notes, search) is a work queue at three boxes with one operator.

**Recommendation: the kernel's four types are right, and three of them earn their keep today.** Keep POINT_TO_POINT — core NATS queue groups give it free (inv. 11 measured `dgx=6 + mac-mini=4 = 10 of 10`, exactly-once, no dupes, no drops), and `PublishRequest.group_key` is already speced. Free is free. But **nothing should block on it, and no plugin should be designed around it** until something asks. This is a direct answer to BRIEF §7 Q3 (*"What transport types should the kernel start with?"*) derived from plugins rather than from a menu: **start with LOOKUP, REQUEST_REPLY, PUBSUB.**

Honest bound on the claim: it is [DEDUCED] from *these three* plugins. The SSH-helper plugin (§3.2; inv. 13 §8) wants a fleet-wide "everyone answer this" scan, which looks fanout-shaped — but it is really `Interest`, which is a kernel primitive here, not a channel. If a fourth plugin needs real fanout, this is wrong and cheap to fix. It is not wrong *yet*.

### 5.3 Where the API held up — and the convergences, which are the real evidence

A falsification exercise that only reports damage is not honest. And because I designed blind and reconciled afterwards, the agreements below are *independent derivations*, not agreement:

| Both of us, independently | Why it matters |
|---|---|
| **Per-node sequence; no fleet-wide order; wall clock is display-only** | Same measured root (harmonik's UUIDv7 sort, salvage row 35), same conclusion, same stated reason: a fleet order needs a single writer, a single writer is a hub, §4 forbids hubs. |
| **Lookup returns *every* claimant; clashes are plugin policy** | I designed `AMBIGUOUS_NAME` (§3.3) before reading `kernel.proto:120-122`: *"Returns EVERY claimant of the key… the kernel does not pick: name clashes are a plugin policy."* Exactly the same refusal to auto-resolve, for exactly the same §6 reason. |
| **Plugins hold no in-memory state; reload is safe because the kernel owns storage** | `plugin.proto:63-65` states it as the reason the four-method lifecycle works; §4.5 derives it from investigation 12's measured `state after reload: ""`. Vault reached it in production. Three routes, one answer. |
| **Fanout is redundant** | §5.2. |
| **`(name, type)` is enough — no third axis** | I never wanted one. Store-and-forward is why (§5 preamble). Greg's original realisation survives contact with three real plugins. |

Plus two things that held up on their own:

- **The daemon never needed to understand a message.** `comms.Message.body` is never parsed; `LogLine.raw` is never reformatted; `Summary.text` is copied out of a vendor field (§3.5). **BRIEF §2's "never judges" cost me nothing to honour** — a strong sign the line is in the right place.
- **The transport choice barely mattered to me.** Comms needs: bytes to a named node, a reply-or-no-responder, and a replicated map the kernel builds itself either way. Core NATS and mangos both do the first two; neither does the third. **My three plugins do not discriminate between the sibling investigations' recommendations**, which independently corroborates the zeromq doc's framing that the transport is the *small* decision. If design/20 picks either, nothing in this document changes.

### 5.4 The one thing that would falsify this design

**If `logs.announce` (pubsub) were load-bearing, this design would be broken by the measured transport.** The zeromq investigation measured PUB/SUB losing **98.9%** of messages with zero send errors, and NATS core is fire-and-forget by construction ([MEASURED, salvage]: *"publish to an ABSENT subscriber: returned nil error (fire-and-forget, silently dropped)"*).

It isn't load-bearing — the cursor is, and the announce is a hint whose loss costs latency only (§4.3). But that is a property I designed *in* deliberately, and it is the kind of thing that erodes. **If a future change makes any pubsub message the only copy of a fact, this design is broken and the measurement above is the proof.** The rule to hold: **on this substrate, a pubsub message may never be the only copy of anything.**

---

## 6. What we do NOT build

- **No overlap detection.** No `--touching`, no `focus_paths`, no "these two agents are converging" warning. BRIEF §6: *"That is not my instinct... I really dont even care about that."* Salvage puts every artifact of it in bucket C. Carry the message; never judge.
- **No search, no index, no embeddings, no RAG.** §4 defers it and calls it a consumer. §4.4 of this doc is the *contract* search will consume; building search is a different plugin on a different day.
- **No summarisation.** The daemon reads `aiTitle`; it never writes one (§3.5). If a better summary is wanted, it is an LLM call in a *different process* — which subprocess plugins make structurally true rather than a rule someone has to remember.
- **No blocking read-ACK.** §2.4. A mailbox, not a phone call.
- **No `from` authentication.** Trusted network, three boxes, all Greg's (§4). Harmonik does the same deliberately. Do not reopen.
- **No consensus, anywhere.** Name clashes surface as `AMBIGUOUS_NAME` (§3.3); duplicate archives are independent cursors (§4.3); nothing votes. §6: *"I dont want to get hung up on CAP theorem and shit."*
- **No fanout channel type, and no point-to-point unless it's free** (§5.2).
- **No derived transcript paths.** Ever (§4.2). Discovery only, hooks where available.
- **No agent self-reported presence.** Observed facts only (§3.4).
- **No cross-machine wall-clock ordering.** `observed_at` and `vendor_ts` are display-only, and the schema says so where the search author will read it.
- **No pushing logs to the archive.** Pull only — it is the property that keeps §4 satisfied (§4.3).
- **No plugin-to-plugin calls.** They meet in lookup tables or not at all.

---

## 7. Open questions

1. **I reconciled against `20-kernel-proto/`, but not against a finished `20-kernel.md`.** The `.proto` files appeared mid-work and I re-derived §1 and §5 against them [MEASURED]. If the accompanying prose changes the proto, **F9 and F1 are the two to re-check** — F9 because it contradicts BRIEF §4 as written, F1 because it decides whether Greg's stated reason for choosing protobuf actually pays off. `22-plugin-system.md` still does not exist; my go-plugin assumptions (§1.1, §4.5) come from investigation 12, not from that author.
2. **Per-node ordering is [DEDUCED] by both of us, and measured by neither.** §1.2: one log per node, cross-node explicitly concurrent. It is the only option compatible with no-hub, and the kernel author reached it independently — but **independent agreement is not measurement.** The specific untested claim: that `thread_id`/`in_reply_to` is a sufficient causality story for a human reading a cross-machine conversation. **This is the top technical risk in the design and two agents agreeing has not reduced it.**
3. **Do Codex or Pi have a hook mechanism?** I found Claude's (measured, via harmonik) and did not look hard for the others. If they do, presence for those vendors gets a positive turn-end signal instead of a growth heuristic. Cheap to check; changes §3.4 for two of three vendors.
4. **Claude Code hooks are not installed on this box today** [MEASURED — `~/.claude/settings.json` has no `hooks` key]. So route 1 of §3.2 is available but unproven *here*. Everything I know about the payload comes from harmonik's source and spec, i.e. harmonik's *belief* about Claude Code. Worth one real hook firing before committing.
5. **Is `aiTitle` stable API?** It is in 55 of 60 recent transcripts [MEASURED] and it is undocumented as far as I know. If it vanishes in a Claude Code release, §3.5 falls back to the Codex/Pi path (last agent message) with no design change — but the *quality* of the summary drops, and it's the best thing in the registry.
6. **What is the real doorbell success rate on *this* fleet?** `pasteinject.go:129` says ~1/3 of runs hang **on a remote SSH worker under concurrent cold-boots** — a specific, hostile condition, not the steady state. I did not measure it for a local idle pane, which is the common case. The design tolerates any failure rate (§2.5), but the number decides whether the doorbell is a nice-to-have or the main path. One tmux session (`claude-remote`) is live right now [MEASURED] and could be tested in an hour.
7. **gb-mac-mini is unmeasured.** `ssh 100.120.22.74` → `Host key verification failed` [MEASURED, today, from gb-mbp]. **This is BRIEF §4's stated problem happening live, to me, during the design** — the third independent agent to hit it. I could not measure its corpus, so §4.1's fleet-wide volume is gb-mbp + dgx only. Investigation 13's Phase-1 SSH verifier is the fix and it should be built early, precisely because it keeps costing people hours.
8. **Does the kernel's LOOKUP replication actually converge across a laptop sleep?** My §2.3 send path assumes `LookupGet("registry.agents", …)` is correct on gb-mbp for an agent on dgx after the laptop wakes. `LookupPutRequest.ttl_seconds` is documented as *"Local wall clock; not a fleet lease"* [MEASURED — `kernel.proto:108`], which is the honest caveat, and it means a woken laptop may hold rows that expired elsewhere or expire rows that are fine. Nobody has measured the reconcile. This is the registry's equivalent of investigation 13's untested 8-hour-sleep question, and it lands on the send path.
9. **Does the registry actually rebuild from cold?** §4.5's rule says it must. The test (*kill the registry, restart, does the table repopulate from the transcript dirs?*) is the acceptance test for "plugins hold no state" and I did not write it.
10. **Retention.** Nothing in this design deletes anything. 69 GB/year fleet-wide is decades on dgx and ~6 months on the mini [MEASURED]. Fine to defer, and it becomes a real question the day a second archive appears.
11. **Does an agent's `Stop`-hook inbox poll (§2.5 item 4) cost a turn?** It is one local call, but it happens inside the agent's hook path, and a hook that blocks hangs the agent (which is why harmonik bounds everything at 5s). Unmeasured.

---

## Sources

**Authoritative brief:** `/Users/gb/research/2026-07-15-agent-substrate-v2/BRIEF.md` (read in full).

**Investigation docs read in full:**
- `investigate/15-salvage.md` — harmonik reverse-engineering, the three bugs, per-node ordering, core-mesh measurements
- `investigate/11-transport-options.md` — channel-type coverage, no-responders, ConnectRPC, NATS payload cap, MTU
- `investigate/13-roster-and-addressbook.md` — the 512-byte cap, presence latencies, the SSH matrix, name→node resolution
- Summaries only (full docs not read; cited claims are attributed to their summaries): `investigate/10-zeromq.md` (mangos PUB/SUB 1,094/100,000 loss; SURVEY; Clone-not-in-Go; the durability-axis framing), `investigate/12-plugins-live-reload.md` (go-plugin reload 12ms, state loss, perf, Vault), `investigate/14-prior-art.md` (Telegraf/Benthos/memberlist/osquery/Istio/protobuf rules)

**The kernel API, read in full after it appeared mid-work** — every `[MEASURED — kernel.proto:NN]` / `[MEASURED — plugin.proto:NN]` citation in this doc points here:
- `design/20-kernel-proto/fleet/kernel/v1/kernel.proto` — `ChannelType:18-36`, `Envelope:37-51`, `Interest:53-58`, `PublishRequest/Response:60-70`, `RequestRequest/Response:78-88`, `Serve/Respond:89-100`, `LookupPut/Get/List:104-126`, the storage namespace comment `:128-135`, `KVPutRequest.if_revision:136-141`, `KVWatch:150-157`, the log-ordering comment + `LogAppendRequest:158-176`, `Node/Liveness/Roster:183-222`, `InfoResponse:224-231`, `service KernelService:233-260`
- `design/20-kernel-proto/fleet/kernel/v1/plugin.proto` — the "all data flows the other way" header `:1-3`, `ChannelDecl:12-20`, `InterestDecl/ChannelInterest/RosterInterest:22-35`, `PluginManifest:37-44`, `StartRequest:49-54`, `StopRequest:57`, the no-in-memory-state comment + `service PluginService:63-71`
- `design/` at the start of my work → `No such file or directory` (checked twice). `design/22-plugin-system.md` still absent at the end.

**Harmonik source read directly** (`/Users/gb/harmonik-worker/repo`, HEAD `0553d4b6`, `.harmonik/worktrees/**` ignored per instructions):
- `internal/hookrelay/hookrelay.go:1-70` — the `hookInput` struct (`session_id`, `hook_event_name`, `transcript_path`, `cwd`, `permission_mode`), `knownEventKinds`, the one-shot NDJSON connection regime
- `internal/keeper/injector.go:125-190` — `InjectText`'s load-buffer → paste-buffer -d → settle → Enter → 2 retries, the hk-89g comment, `sendEnter`, `SendEscapeKey`, the `tmuxRunFn` test seam
- `internal/daemon/pasteinject.go:114,126-165` — `resumeSubmitRetries=2`, `pasteVerifyAttempts=3`, `pasteVerifyScrollback=200`
- `specs/claude-hook-bridge.md:44,90,167,258,274,290` — hook event list, the settings.json hook shape, `transcript_path` contract
- `git log -1` → `0553d4b6 Sat Jul 11 08:21:41 2026 -0700`

**Commands run on the live fleet today (2026-07-15), all on `gb-mbp` unless noted:**
- `find … -name '*.jsonl' | wc -l` → `804`; `ls ~/.claude/projects | wc -l` → `203`
- `ls ~/.claude/projects | grep -c '^-'` → **203 of 203** (every Claude project dir starts with `-`)
- `ls ~/.pi/agent/sessions | grep -c '^--'` → **27 of 27** (every Pi session dir starts with `--`)
- Per-vendor actual bytes via `stat -f '%z'` over `*.jsonl` only: Claude **193.3 MB/804**, Pi **57.4 MB/186**, Codex **14.2 MB/18**; combined **264.9 MB / 1,008**
- `du -sh` for contrast (block-rounded, counts non-transcripts): `270M` / `58M` / `14M`; non-`*.jsonl` files in `~/.claude/projects` → **73.4 MB across 433 files** (331 json, 79 txt, 10 pdf, 9 md, 4 js). `du -sh ~/.codex` → `425M` (mostly sqlite/caches, not sessions).
- Per-day growth: `find … -print0 | xargs -0 awk 'index($0,"\"timestamp\":\"<day>")'` summing line lengths → 07-15: **62.9 MB / 21,057 lines**; 07-14: 15.9 MB; 07-13: 0; 07-12: 38.0 MB; 07-11: 22.0 MB. (The naive `find -mtime -1` + whole-file-size method gives 82.6 MB and is **wrong** — it counts old bytes in recently-touched files.)
- Longest line via `awk '{if(length($0)>m)m=length($0)}'` → **309,007 bytes**
- Schemas via `jq`: Claude line-type key sets and the `{"type":"ai-title","aiTitle":…}` line (**55 of 60** recent transcripts contain `aiTitle`); Codex `session_meta` payload, `turn_context` keys (`summary` = `"auto"`), `task_started`/`task_complete` payloads; Pi `session` line
- `cat ~/.claude/settings.json` → **no `hooks` key**
- `ssh -o BatchMode=yes gb@100.115.27.55` (dgx) → `Linux 6.17.0-1026-nvidia aarch64`; `~/.claude/projects` = `13M` / 40 files; `~/.pi` present; `df /` → **3.2T free**
- `ssh -o BatchMode=yes gb@100.120.22.74` (gb-mac-mini) → **`Host key verification failed`** — unmeasured
- `command -v tmux` → `/opt/homebrew/bin/tmux`; `tmux ls` → `claude-remote: 1 windows (attached)`
- `ls /Users/gb/research/2026-07-15-agent-substrate-v2/design/` → `No such file or directory`

**Files created:** none. No experiment code was needed for this doc; every measurement above is a read of the live fleet or of existing source.
