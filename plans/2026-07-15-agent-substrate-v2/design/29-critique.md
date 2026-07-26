# 29 — Adversarial critique of the five subsystem designs

**Date:** 2026-07-16
**Role:** find where designs 20–24 are wrong. Harsh, specific, fair.
**Method:** read BRIEF.md first and in full; read designs 20, 21, 22 (§0–4, §8, §11), 23, 24 and investigate/10, /11; re-ran the corpus's own boundary test against its own artifacts; re-verified every load-bearing source claim against the real library source on this laptop. Everything labelled **[VERIFIED]** I ran today and the output is quoted.

**Labels:** **[VERIFIED]** = I ran it, output quoted. **[CONFIRMED-CODE]** = I opened the file and read the line. **[DEDUCED]** = my inference, flagged every time.

---

## 0. Verdict up front

**The corpus is factually excellent and architecturally un-converged.**

I fact-checked every load-bearing external claim I could reach — memberlist's suspicion code, NATS's Raft call sites and route-ping clamp, go-plugin's shutdown path, harmonik's `bytes.Compare` sites, the Tailscale MTU. **Every single one verified exactly.** Some verified to the line number. This is a better factual record than the last round's critic found, and it is not close. **I found zero hallucinated technical facts.**

I found **one fabricated attribution to Greg**, and it is load-bearing for the corpus's most consequential recommendation. That is §1.1, and it is the most important finding in this document.

I found **no hub**. The last round's fatal flaw does not recur. Every design is full-mesh; doc 24's archive-on-dgx explicitly pulls rather than receives; doc 22's plugin library ships pointers and refuses to be a central artifact server. On the #1 checklist item, the corpus is clean, and it earned that honestly.

The real problem is different and serious: **there are three mutually incompatible kernel protobuf APIs across docs 20, 21 and 22, two of which claim the same package name.** Nobody can build from this corpus as it stands. That is §2.

Nothing was quietly cut. Plugins, live reload, request/reply, the roster and protobuf are all present and all designed. **Do not cut the project.** Cut the duplication.

---

## 1. BRIEF VIOLATIONS

### 1.1 THE BIG ONE — "a broker on every node" is not Greg's. It is not in the brief. It is not anywhere. [VERIFIED]

Both docs 20 and 21 justify overriding Greg's single most explicit stated preference by telling him the override is *his own idea*:

> **20-kernel.md:86:** "**What we are actually choosing is Greg's own idea.** His middle path was *"a broker on every node."*"
> **21-transport.md:91:** "which is **Greg's own stated middle path**, quoted in investigation 11 as his option #4: *"a broker on every node."*"

Both put it in quotation marks. The brief's header says: *"Where quoted, the words are his."*

**They are not his.**

```
=== 'broker' in the authoritative BRIEF? ===
>>> THE WORD 'broker' DOES NOT APPEAR IN THE BRIEF AT ALL <<<

=== provenance grep of the prior round's folder ===
(no results)
```
[VERIFIED — `grep -in "broker" BRIEF.md`; `grep -rl "broker on every node" ~/research/2026-07-15-agent-comms-substrate/`]

The phrase does not appear in the brief. It does not appear in the prior round's folder either. It originates in `investigate/11-transport-options.md:45`, which asserts without citation:

> "It is *"a broker on every node"* — **his own middle-path option #4**"

**There is no option #4.** Investigation 11's §5 "Middle paths" is *investigation 11's own* enumeration of options it invented, and "A broker on every node" is the **first** heading in that list, not the fourth [VERIFIED — `sed -n '/^## 5. Middle paths/,/^## 6/p'` returns 5 `###` headings; "A broker on every node" is #1]. The index refers to nothing. Investigation 11 cites no source. Docs 20 and 21 then quoted it back as Greg's words without checking, and each cited *the other's* lineage as corroboration.

**Why this is the worst finding in the corpus:** it is BRIEF §9's failure mode, exactly, reproduced at the highest-stakes decision point. §9's diagnosis of the last round: framing not in the brief was promoted to load-bearing, and the corpus bent to serve it. Here, an invented claim about what Greg wants is used to convert *"I am overruling your explicit preference"* into *"I am agreeing with you."* Those are very different things to say to someone, and only one of them is true.

**It is also unnecessary, which makes it worse.** The technical argument for core NATS does not need the quote and is not weakened by deleting it. Two of three boxes killed, lone survivor serves pub/sub and request/reply; 28ms cold-start alone; every Raft call site inside `jetstream_cluster.go` — I verified that last one myself:

```
jetstream_cluster.go:1046: s.bootstrapRaftNode(cfg, peers, false)
jetstream_cluster.go:1082: s.startRaftNode(sysAcc.GetName(), cfg, ...)
jetstream_cluster.go:2990: s.bootstrapRaftNode(cfg, rgPeers, true)
jetstream_cluster.go:2993: s.startRaftNode(accName, cfg, labels)
raft.go:356, raft.go:624:  definitions only
```
[CONFIRMED-CODE — nats-server v2.14.3, `-test.go` excluded. Doc 21's claim is exact.]

**Required fix:** strike the quote from 20:86, 21:91 and investigate/11:45,448. Replace with the honest sentence, which doc 21 elsewhere already writes well (21:107): *"Your instinct was right — the thing you don't want is a hub — but the hub isn't NATS, it's JetStream. I am overruling your stated preference on NATS, here is the measurement, and you should get the chance to reject it."* Doc 20's §12 risk list already says exactly this. The flattering version is the one that has to go.

### 1.2 Doc 21 puts a domain concept in the kernel — caught by the corpus's own tool [VERIFIED]

BRIEF §3.1: the daemon moves bytes, the plugin owns all logic. Doc 21's kernel `Envelope` (21-transport.md:239):

```proto
string origin_agent = 4;  // daemon-defined agent name = the key
```

I ran doc 20's own boundary test against doc 21's proto:

```
=== doc 20's boundary test run against doc 21's proto ===
   BANNED IDENTIFIER in t21.proto: origin_agent
>>> BOUNDARY VIOLATION
exit=1

=== sanity: same test against doc 20's own shipped proto ===
VOCABULARY CLEAN -- the kernel names no domain concept.
exit=0
```
[VERIFIED — `design/20-kernel-boundary-test.sh`]

Doc 20's rule (20:839) is right and doc 21 breaks it: *"the kernel knows what a **box** is, because it has to dial one. It does not know what runs on one."* An `origin_agent` field means the kernel knows what an agent is. Doc 24 independently builds the agent registry as a plugin; doc 23 independently proves `grep -ri agent kernel/` → 0 hits is achievable. **Doc 21 is the only design that leaks, and it leaks in the one file that is supposed to be the contract.** Delete the field; it belongs in the comms plugin's payload or in `headers`.

### 1.3 Doc 21 makes durability a kernel guarantee — and contradicts itself doing it [VERIFIED]

BRIEF §4: *"Durability is a PLUGIN decision, not a kernel guarantee."*

Doc 21 says this loudly and correctly, twice:

> **21:495:** "**Note what is absent: there is no `SendReliably`.** Reliability is not a kernel verb. If it were, durability would be a kernel guarantee, and §4 forbids that."
> **21:586:** "There is deliberately no `SendReliably`."

And then ships it as an enum on the kernel's own `PublishRequest` (21:281-286):

```proto
message PublishRequest {
  Envelope envelope = 1;
  DurabilityClass durability = 2;
}
enum DurabilityClass {
  DURABILITY_FIRE_AND_FORGET = 1;
  DURABILITY_LOCAL_LOG       = 2;  // append+fsync locally, then send
  DURABILITY_AT_LEAST_ONCE   = 3;  // local log + retry until ACKed
}
```

`Publish(durability: AT_LEAST_ONCE)` **is** `SendReliably`. It is spelled as a parameter instead of a method name, and the kernel is now the thing that owns the local log, the retry loop, and the ACK wait. Doc 21:442 confirms the intent — *"it declares `DURABILITY_AT_LEAST_ONCE` and reads from its own log"* — but a plugin that reads *its own* log does not need to tell the *kernel* a durability class at all.

This is not a naming quibble. It is the §4 violation the whole doc is written to avoid, sitting in the doc's own contract. Doc 20 and doc 24 both got this right (kernel is at-most-once always; the plugin composes durability from journal + message_id + roster). **Delete `DurabilityClass` from the kernel API.** Doc 21's own §5 recipe — sender writes own disk, then transmits, receiver writes own disk, then ACKs — needs no such enum, and doc 21 measured that recipe working without one.

### 1.4 Doc 20 promises a durability knob its shipped contract does not have — doc 24's F9 is CONFIRMED [VERIFIED]

Doc 24's top finding is real. I checked it against the artifact on disk, not against 24's quote of it:

```
=== durable/sync in proto? ===
NO durable/sync FIELD ANYWHERE IN THE SHIPPED PROTO
```
[VERIFIED — `grep -in "durable|sync|fsync" design/20-kernel-proto/fleet/kernel/v1/*.proto`]

Meanwhile doc 20's own prose (20:336) says the knob exists:

> "a plugin that wants an fsync barrier per record asks for it per call (`durable: true` on `JournalAppend`)"

and doc 20's own Go interface (20:759) has it:

```go
JournalAppend(ns, journal string, recs [][]byte, durable bool) ([]uint64, error)
```

but `JournalAppendRequest { string journal = 1; repeated bytes records = 2; }` does not. **The doc, the Go seam, and the wire contract disagree, and the wire contract is the one Greg is being asked to approve.**

**Fairness to doc 20, and a correction to doc 24:** this is a one-field omission, not a philosophical dispute. Doc 20 *already agrees* the field should exist. Doc 24 frames F9 as "the kernel taking back the decision that makes the rest of the architecture legal" — that overstates it. The kernel author intended the knob and dropped it in the file. **Fix: add `bool sync = 3` to `JournalAppendRequest`, `bool synced = 2` to the response, done.** Doc 24 is right that it must be per-call and never a daemon-side policy map; harmonik's `fsyncBoundaryEventTypes` map is real and I confirmed it exists [CONFIRMED-CODE — `internal/eventbus/busimpl.go`, referenced from `decision_fsync_33p_test.go:27`].

**Also: doc 24's F10 is already dead.** It asks to "unify `LogReadRequest.journal` vs `LogAppendRequest.log`". The shipped proto says `JournalAppendRequest.journal` throughout. Doc 24 read the file during the window described in doc 20 §10.2, when the `Log*`→`Journal*` rename had silently failed under BSD `sed`. Doc 24's F9 quote cites `kernel.proto:161` for a line that no longer exists. **The substance of F9 survives; its citation does not.** Anyone actioning doc 24's dependency list should re-check it against the current artifact — at least one item on it is stale.

### 1.5 Doc 22 makes plugin storage cross-readable — destroying doc 20's headline decision [VERIFIED]

Doc 20's decision #2, stated in its verdict (20:24) and again at 20:342 and 20:349:

> "**there is no `namespace` field in any storage RPC.** The kernel derives it from the calling plugin's registered identity... **Reading another plugin's storage is not forbidden — it is unrepresentable.** There is no field to put the wrong value in."

Doc 22's kernel storage API (22-plugin-system.md:556-567):

```proto
message StorageGetRequest    { string namespace = 1; string key = 2; }
message StoragePutRequest    { string namespace = 1; ... }
message StorageDeleteRequest { string namespace = 1; string key = 2; }
message StorageListRequest   { string namespace = 1; string key_prefix = 2; }
```

**Doc 22 puts the field on the wire in all four storage RPCs.** Doc 20's "unrepresentable" property is destroyed — it becomes an ACL check that someone has to remember to write, which is precisely the weaker thing doc 20 argued against. Doc 22's own dependency #2 (22:1000) even asks for it in the weak form: *"enforces namespaces per plugin token."*

Doc 20 is right and doc 22 should yield. The property costs nothing and kills the cursor-file bug class by making the ergonomic path the correct path.

### 1.6 Nothing was cut. [VERIFIED by inspection]

Against BRIEF §9's list of the last round's cuts:

| §9 casualty | Status in this corpus |
|---|---|
| Plugin system cut entirely | **Present.** Doc 22, 1000+ lines, measured, with a state machine and a plugin-library section. |
| Request/reply cut | **Present in all three kernel APIs.** Doc 20 `Request`/`Serve`/`Respond`; doc 21 `Request`; doc 22 `Request`. |
| Machine roster rejected | **Present and baked into core**, as §3.1 asks. Doc 23 is 73KB on it alone. |
| Overlap detection dominating | **Absent.** Every design explicitly refuses it and cites §6. |
| Search dropped by category error | **Correctly deferred as a consumer**, with `origin_node`/`origin_time` riding along per §4. |
| Hub-and-spoke NATS on the DGX | **Absent.** See §1.7. |

Protobuf: present, real, linting, generating, cross-compiling. Live reload: present and measured at 3–11ms. **The brief's requirements are all covered.** This is the corpus's biggest win and it should be said plainly.

### 1.7 No hub. [VERIFIED by inspection of all five designs]

I went looking for a re-imported hub and did not find one:

- **Transport:** full mesh, all boxes are seeds, no bootstrap node, cold-start alone in 28ms [doc 21, measured].
- **Storage:** box-local, never replicated [doc 20].
- **LOOKUP / state:** single-writer-per-key; every row's writer is the box it describes [docs 20, 21, 23 — three independent derivations of the same property].
- **Archive on dgx:** doc 24 makes the archive **pull**, cursor-driven. If dgx dies, every node keeps its own complete log. §4 forbids a box whose death *stops* the system; the brief itself makes search a consumer.
- **Plugin library:** doc 22 §8 ships **pointers, not bytes**; any box serves the file; origin asleep = "loud, local, non-fatal."

The one place worth watching: doc 22's artifact URL example is `http://100.115.27.55:7947/...` — the DGX. If every plugin is built on dgx and served from dgx, dgx becomes the *de facto* artifact origin. Doc 22 argues correctly that this degrades rather than kills. It is not a §4 violation. It is worth one sentence in the build docs so it does not become one by habit.

---

## 2. CONTRADICTIONS — with resolutions

### 2.1 THREE incompatible kernel APIs, two claiming the same package name [VERIFIED]

This is the finding that blocks building.

| Doc | Package | Service | Surface |
|---|---|---|---|
| **20** | `fleet.kernel.v1` | `KernelService` | 19 RPCs: Publish/Subscribe/Request/Serve/Respond, LookupPut/Get/List, KV×5, Journal×3, Roster×2, Info |
| **21** | **`fleet.kernel.v1`** | `Kernel` | 12 RPCs: OpenChannel/Publish/Subscribe/Consume/Request/Gather, Append/ReadFrom/GetCursor/SetCursor, Roster/WatchRoster |
| **22** | `substrate.kernel.v1` + `substrate.type.v1` + `substrate.plugin.v1` | `KernelService` | 7 RPCs: Publish/Request, Storage×4, RosterGet |
| **23** | `substrate.roster.v1` | (roster) | 9-method state-slot API: PutLocalState/GetPeerState/ListPeerState/WatchState… |
| **24** | `fleet.comms.v1`, `fleet.logtail.v1` | (plugins) | built on **20** |

[VERIFIED — `grep -n "^package"` across all five docs]

**Docs 20 and 21 both declare `package fleet.kernel.v1` with different `Envelope` messages and different services.** These are not two proposals. They are two files that cannot coexist in one build — `protoc` would reject them, and doc 20's own experiment log records exactly this class of failure (`proto: file ... is already registered`, 20:998).

Their `Envelope`s disagree on every field number:

| Field | doc 20 | doc 21 |
|---|---|---|
| 1 | `channel` | `id` |
| 2 | `payload` | `channel` |
| 3 | `headers` | `origin_node` |
| 4 | — | `origin_agent` |
| 8 | — | `payload` |
| 13 | `origin_seq` | — |

**Resolution: doc 20's proto is the contract. Docs 21, 22, 23 conform to it.** Reasons, in order:

1. **It is the only one that exists as a real, verified artifact.** It lints clean under buf STANDARD, builds, generates Go+ConnectRPC, cross-compiles to linux/arm64, and passes its own boundary test. I re-ran the boundary test today: `VOCABULARY CLEAN`. Docs 21 and 22's protos are inline code blocks in a markdown file.
2. **It is the only one that passes the boundary test** (§1.2).
3. **Doc 24 already built three plugins against it** and reported the boundary held. That is a real integration test the others do not have.
4. **Doc 22 explicitly declines to vote** on the transport (22:1008) and doc 23 says "I have no attachment to my names." Only doc 21 is actually contesting, and doc 21's contribution is the *transport measurement*, not the API — its own §1 frames it as "the transport layer," and its API sketch is incidental to that.

Doc 21's genuinely load-bearing contributions survive intact under doc 20's proto: `PingInterval=2s`, the blackhole finding, the store-and-forward proof, the 61s→5s window. None of them require its `Envelope`.

### 2.2 Kernel→plugin push vs plugin→kernel pull — and this one is deep

**Doc 20** (20:501, 20:667): *"PluginService is deliberately 4 lifecycle methods... **All data flows the other way**, through `KernelService`, which the plugin calls."* Describe/Start/Stop/Health. Doc 20 says a 5th method "is a boundary review trigger."

**Doc 22** (22:149-154): `PluginService` = Describe/**Start**/**Deliver**/**Notify**/**Drain**. Five methods, no Stop, no Health, and `Deliver` is the kernel **pushing** envelopes into the plugin. Doc 22:162: *"`DeliverResponse` is the ack."*

These are opposite architectures, and the consequences are not cosmetic:

- **Doc 22's central live-reload argument requires push.** 22:155: *"the plugin does not **hold** a subscription — the kernel holds it... When a plugin is swapped, its subscriptions do not go anywhere."* Under doc 20's pull model this is **false**: `Subscribe` is a server-streaming RPC *the plugin calls*, so the stream dies with the process and the new plugin must re-subscribe on `Start()`.
- **Doc 22's drain gate is impossible under pull.** The gate (22:212-258, measured working) protects in-flight `Deliver` calls. Under pull, the kernel has no idea the plugin is mid-work — there is nothing to drain because there is nothing the kernel can see. Doc 22's headline "**messages are not lost; they wait**" (22:320) is false under doc 20's kernel: doc 20's decision #1 (20:23) is *"It never queues, never retries, never redelivers."*
- **Doc 22's stated dependency #3 (22:1000) — "The kernel queues messages for a plugin that is briefly not dispatching" — is a direct request for the one thing doc 20 refuses to build.**

**Resolution: pull wins (doc 20). Doc 22 gives up "subscriptions are kernel state" and the drain gate.**

Why: pull is what makes the plugin API and the REST API *literally the same service*, which is BRIEF §3.2's stated reason for choosing protobuf at all. Push means plugins must serve a surface agents cannot use, and the "same interfaces over REST" property splits in two.

**And the loss is smaller than it looks, because store-and-forward already covers it.** Under pull, a reload abandons in-flight work → no end-to-end ACK → the sender's outbox retries → the receiver dedupes on `message_id`. Doc 21 measured exactly this working, including the re-ship case (its Phase 4: `m2` re-shipped after the DGX already had it; inbox still held exactly 3 records). **The architecture already tolerates abandoned in-flight work. That is what makes pull safe.** Doc 22's measured finding — that `Kill()` hard-stops rather than drains — remains true and worth keeping in the record; it just stops being a 40-line gate the kernel owes.

Doc 22's `Kill()` finding is real and I confirmed it in full:

```go
// Shutdown stops the grpc server. It first will attempt a graceful stop, then a
// full stop on the server.
func (s *grpcControllerServer) Shutdown(...) (*plugin.Empty, error) {
	// TODO: figure out why GracefullStop doesn't work.
	s.server.Stop()
```
[CONFIRMED-CODE — go-plugin v1.8.0 `grpc_controller.go`, quoted verbatim. `GracefulStop` exists at `grpc_server.go:129` and is called only from `grpc_broker.go:398`, an unrelated path. **The doc comment describes behaviour the code does not have.** Doc 22's claim is exact, and it answers doc 20's open question #7.]

### 2.3 THREE failure detectors for three boxes

| Doc | Mechanism | Detection | Verdict on the others |
|---|---|---|---|
| **20** | memberlist gossip, in the kernel | ~6s | "You would write incarnation numbers, suspicion and anti-entropy yourself, badly, and debug them at 11pm." |
| **21** | NATS route pings | 61s default → **5s tuned** | 21:589: "**No `memberlist`/SWIM in the kernel.**" |
| **23** | hand-rolled all-to-all probing | 6.5s measured | "Reject memberlist." Keeps its **own dedicated listener** regardless of transport. |

Compose the corpus as written and every box runs **three independent liveness systems** that will disagree with each other, at three different timescales, on three boxes. This is the single fattest thing in the corpus.

**Resolution: doc 23 wins. Drop memberlist. Keep NATS route pings for transport-interest only, tuned to 2s.**

I verified doc 23's decisive claim in memberlist's own source, and it is exactly right:

```go
// state.go:1211-1219
k := m.config.SuspicionMult - 2          // default SuspicionMult=4 -> k=2
n := m.estNumNodes()                     // n=3
if n-2 < k {                             // 1 < 2  -> TRUE
    k = 0
}
// suspicion.go:73-75
if k < 1 {
    timeout = min
}
```
[CONFIRMED-CODE — memberlist v0.6.0. Also verified: `net.go:83 MetaMaxSize = 512`; `memberlist.go:460` and `:519` both `panic("Node meta data provided is longer than the limit")`; `config.go:336 UDPBufferSize: 1400`.]

At n=3, SWIM's suspicion-confirmation machinery sets itself to zero. Doc 13 measured this and recommended the library anyway; doc 23 caught it. Two of three designs reject memberlist. Dropping it also deletes doc 20's open questions #3 and #4, the 512-byte panic, the 1400-vs-1280 MTU landmine, and 102 modules — all at once.

**But doc 23 overstates its own case and I will not let it pass.** It says *"at n=3 SWIM's flagship feature is disabled by its own code."* Only *suspicion confirmation* is disabled. **Indirect probing** — arguably SWIM's actual flagship, the thing that stops one bad link from producing a false positive — still works at n=3 with one helper node. Doc 23's own design has **no indirect probing at all**, so a single bad A→B link marks B DOWN at A with no second opinion. Doc 23 argues this is a *feature* (§1.3: per-observer liveness is the correct answer for routing, because the sender needs "can *I* reach B", not "does the fleet think B is up"). **That argument is good and I accept it** — but it should be made on its own merits, not under cover of "the flagship is disabled." Fix the sentence; keep the design.

### 2.4 Four channel types or five — and FANOUT has zero consumers

Doc 20 and doc 24 ship four (PUBSUB, POINT_TO_POINT, REQUEST_REPLY, LOOKUP) and independently conclude fanout collapses into pubsub. Doc 21 ships five, defining `FANOUT` = scatter-gather (21:229).

**Resolution: four. Cut FANOUT.** Not because two docs outvote one, but because **FANOUT's only consumer in doc 21 is the roster roll-call** (21:188: *"a `FANOUT` roll-call — 'send pings to each other', Greg's exact model"*) — and **both roster designs reject that mechanism.** Doc 20's roster is memberlist; doc 23's is direct probing. Neither does a roll-call. Doc 24's consumer tally independently found the same thing: two of the five menu types have no consumer among the three real plugins.

A channel type with no consumer is a kernel feature built for nobody. Cut it. **Tell Greg his list came back one item shorter and why** — all three docs agree the word was ambiguous, and doc 20's argument is sound: if "fanout" meant broadcast that is PUBSUB; if it meant a work queue that is POINT_TO_POINT. Both readings already ship. This does not need to block building, but he should hear it from us rather than notice it.

### 2.5 LOOKUP channel vs state slot — same primitive, two names

Doc 20: LOOKUP channels, single-writer-per-key, `LookupPut/Get/List`. Doc 23: state slots, `PutLocalState/GetPeerState/ListPeerState/WatchState`. Doc 23 flags this itself: *"almost certainly the same thing as my state slot under a different name. These must be unified, not shipped twice."*

It is right. Both are: a replicated map where every key's sole writer is the node it describes; no conflict; no resolver; no CAP argument. **Three docs derived the same property independently** (20, 21, 23) — that convergence is the strongest signal in the corpus and it deserves one name.

**Resolution: LOOKUP wins.** Doc 24 built three plugins on it; doc 23 has no attachment to its names. `PutLocalState(ns, blob)` becomes `LookupPut(channel, key=self, blob)`.

**Consequence doc 23 must accept:** its state slots ride the *probe* RPC (≤1s, fate-shared with liveness). LOOKUP rides the transport. Keeping both means two replication paths again. **Clean split: the roster's probe carries liveness only; all replicated data rides LOOKUP over the transport.** Doc 23's fate-isolation argument for a dedicated liveness listener is good and survives — it just carries less.

### 2.6 STATE_LEFT — doc 23 is right, doc 20's proto is wrong

Doc 20's proto ships `STATE_LEFT` and `KIND_LEAVE` as sibling states. Doc 23 rejects them: *"LEFT isn't mechanically detectable, so it's silently wrong exactly when the sleep hook didn't fire."*

Doc 23 wins, and doc 20 already carries the evidence against itself (20:445): memberlist's `NotifyLeave` reports `State=0` (Alive) even for a graceful leave, so *"you cannot read `n.State` in the callback to tell 'left' from 'dead'."* **A state you cannot derive from an observation is not a state.** Doc 24 independently flagged the same thing.

**Resolution:** one state `DOWN`, plus a `reason` field (`ANNOUNCED_SLEEP` | `PROBE_TIMEOUT`). Degrades correctly when the hook does not fire. Doc 20's `Node.Intent` (announced before sleep) stays — that is a *declaration*, which is exactly the thing that is mechanically knowable.

### 2.7 Doc 21 answers doc 20's #1 open question — not a conflict, a resolution to bank

Doc 20's open question #1: *"Does the NATS route mesh survive a real macOS sleep? Defaults suggest ~4 minutes to reap a dead route."*

Doc 21 answered it, root-caused it in source before believing it, and I verified the root cause:

```
route.go:140:   defaultRouteMaxPingInterval = 30 * time.Second
client.go:5772: if d > routeMaxPingInterval { return routeMaxPingInterval }
```
[CONFIRMED-CODE — nats-server v2.14.3. Doc 20 guessed ~4 min from the 2-minute global default; the real clamp is 30s, so the true default window is ~61s, and `PingInterval=2s` cuts it to ~5s. Doc 21's measurement matches its code reading exactly, which is why I trust it.]

**Doc 20's #1 open question is answered; its estimate was wrong in the safe direction.** `Cluster.PingInterval=2s` is mandatory config. Note doc 21's Sources section miscites this as `raft.go:140` — it is `route.go:140`, as its own §4 body correctly says. Trivial, but fix it before someone re-greps.

---

## 3. THE KERNEL API'S HONESTY — did doc 24 rubber-stamp?

**No. Doc 24 genuinely tried, and it is the most useful doc in the corpus for a builder.** It found F9 (real — I confirmed it against the artifact), F1 (real), F7 (real), and it caught the one thing I most expected the corpus to miss (below). It reports being refused twice by the boundary and says both refusals were correct. That is not a rubber stamp.

**Three real limits on its verdict, which it partly admits:**

1. **It validated against doc 20's proto only** — docs 21 and 22 did not exist for it. So "the kernel API survives" is a verdict on *one* of the three competing APIs. It never saw doc 22's `namespace`-on-the-wire or doc 21's `origin_agent`. Its verdict does not cover the corpus.
2. **Its citations are stale** (§1.4). F10 is already fixed; F9's line reference is dead.
3. **It never tested the plugin library**, which is the ugliest thing in §3.2.

### 3.1 So I tried to break the API myself — the plugin library

**The test:** BRIEF §3.2's `[Idea]`: *"A plugin gets added to one node, the plugin gets synced across nodes."* A plugin binary is ~18MB [doc 22 measured 18.3MB]. The kernel's channel payload cap is 1MB:

```
const.go:94  MAX_PAYLOAD_SIZE = (1024 * 1024)
```
[CONFIRMED-CODE — nats-server v2.14.3]

LOOKUP entries cap at 64KB [doc 20 §4.5]. KV and Journal are box-local and never replicated [doc 20 decision #2]. **So there is no kernel primitive through which an 18MB binary can cross the fleet. Every replicated surface is 1MB or smaller, and every large surface is box-local.**

**The API does not express the requirement — and doc 22 got there first and answered it correctly.** Its answer (22 §8) is: *sync the manifest, never the bytes.* A record replicates (small, gossip-friendly); each box fetches its own platform's bytes over plain HTTP from whichever box has them and verifies sha256 before exec. **Bytes deliberately route around the kernel.** That is the right call, it matches what Istio and Nomad both converged on, and doc 22 verified the gate works in both directions (correct checksum → launches; tampered checksum or mutated binary → refuses).

**So the API survives this test, by routing around itself.** That is legitimate — but it should be *written down as a kernel non-goal*, because right now it is only implicit. **The kernel does not move blobs. Anything over 1MB is a plugin's problem, and the plugin's answer is a URL plus a checksum, not a channel.** Doc 21 says this in passing (21:448); it deserves to be a rule, since log archiving is a named plugin with the same need (doc 24 measured a real 309KB line and says >1MB is possible).

**Doc 22's best judgement, which I want to endorse explicitly:** it demotes the plugin library from *first* plugin to *last*, and gives Greg a falsifiable test — *"is `scp`-ing two binaries actually annoying yet?"* With 2 platforms (verified: darwin/arm64 ×2 + linux/aarch64 ×1) and one operator, it may never be. It is an `[Idea]` in the brief, not a requirement, and doc 22 holds it to that standard. **That is the single best piece of scope discipline in the corpus.**

### 3.2 The one place the kernel API is genuinely oversold

Doc 20's shipped proto comment on `Interest`:

```proto
// Does anyone, anywhere in the fleet, subscribe to this channel right now?
// Answers BRIEF 5's "notify the sender that the receiver is not listening".
```

**It does not answer that.** BRIEF §5's receiver is an *agent* — a Claude Code process in a tmux pane. The kernel's Interest answers "does any *plugin* subscribe to this channel." Under doc 20's own "static declaration, dynamic refinement" (20:275), comms subscribes per-agent, so Interest degrades to "the comms plugin somewhere has registered that agent name and its box is up." That is useful. **It is not "the receiver is listening."**

Doc 20's §4.4 prose says this correctly and forcefully — *"This is NOT an end-to-end ACK and must never be sold as one"* — **and then the proto comment sells it as one.** The artifact contradicts the doc's own caveat, and the artifact is what a plugin author reads at 2am.

**Doc 24 caught this and its answer is better than doc 20's:** the 4-rung ladder, where rung 4 ("is the agent actually *reading*?") is answered by piggybacking the receiver's `cursor_lag` + `last_read` on the delivery reply. *"DELIVERED, but receiver stale — 47 unread, last read 3h12m ago"* is actionable; an ACK bit is not. All mechanical facts, no judgement — §2-compliant. **Adopt doc 24's ladder; fix doc 20's proto comment to say what the field actually means.**

---

## 4. INVENTED FACTS — the honest report

**Zero hallucinated technical facts.** I checked every load-bearing external claim I could reach. Every one verified, most to the line number:

| Claim | Doc | Result |
|---|---|---|
| memberlist `k = SuspicionMult-2`, `n-2 < k → k=0` at n=3 | 23 | ✅ exact, `state.go:1211-1219` |
| memberlist `suspicion.go:72-75` takes flat min when k<1 | 23 | ✅ exact |
| `MetaMaxSize = 512`; panic at `memberlist.go:460`, `:519` | 20, 13 | ✅ exact, both sites |
| `UDPBufferSize: 1400` default | 20, 21 | ✅ `config.go:336` |
| Every Raft call site inside `jetstream_cluster.go` | 21 | ✅ exact, 4 call sites + 2 definitions |
| `defaultRouteMaxPingInterval = 30s`; clamp at `client.go:5772` | 21 | ✅ exact (Sources miscites as `raft.go:140`) |
| `MAX_PAYLOAD_SIZE = 1MB`, `MAX_PAYLOAD_MAX_SIZE = 8MB` | 21 | ✅ `const.go:94,99` |
| go-plugin `Shutdown` calls `Stop()` with `TODO: figure out why GracefullStop doesn't work` | 22 | ✅ verbatim |
| `GracefulStop` never called on the shutdown path | 22 | ✅ only `grpc_broker.go:398`, unrelated |
| harmonik `jsonlwriter.go` = 413 lines | 15, 21, 24 | ✅ exactly 413 |
| harmonik `bytes.Compare` in **4** load-bearing places | 15, 21 | ✅ exactly 4 non-test: `busimpl.go:386,1417`, `jsonlwriter.go:337`, `subscribe.go:519` |
| harmonik `fsyncBoundaryEventTypes` map exists | 15, 21, 24 | ✅ confirmed |
| Tailscale MTU 1280 | all | ✅ `mtu 1280` |
| mangos latest = v3.4.2 | 10, 21 | ✅ `go list -m -versions` |
| Doc 20's proto lints/builds/generates/cross-compiles; boundary test clean | 20 | ✅ re-ran: `VOCABULARY CLEAN` |

**This is an exceptional factual record and the corpus deserves credit for it.** Several docs also recorded their own falsified hypotheses rather than quietly dropping them — doc 21's firewall hypothesis (checked, wrong) and its `pkill -f` self-kill bug (its own fault, not the fleet's); doc 20's BSD-`sed` false negative, where its own test reported `VOCABULARY CLEAN` on a file with three banned identifiers and it had *already written the wrong result into the doc*. Those confessions are worth more than the tests they describe.

**The one fabrication is not a technical fact. It is a quote from Greg** (§1.1), and it is worse than a hallucinated benchmark would have been, because it is a claim about what the user wants, used to justify overriding what he actually said. Note the shape: doc 20 labelled it `[CLAIMED]` where it inherited *NATS measurements* from investigate/11, but propagated investigate/11's *attribution to Greg* with no label at all. **The labelling discipline covered library internals and not the user's own words** — which is exactly backwards, given §9.

**Two smaller gaps in the discipline:**

- **Doc 20's boundary test cannot check the word "message"**, though §10.1's prose bans it. `message` is protobuf's own keyword, so including it in `BANNED` would false-positive on every line. The kernel ships `message_id` as a result. This is defensible (a `message_id` is a dedupe handle, not a domain concept) but it means **the enforced list and the documented list differ, and nobody said so.** Doc 20's own lesson — *"a test that has never failed on a real violation is decoration"* — applies to its own blind spot.
- **Doc 24's citations are stale** (§1.4) because the artifact changed under it mid-work.

---

## 5. TOP CUTS

Calibrated: 3 boxes, tens of agents, one operator. Greg wants this built. **None of these cut a brief requirement.**

**1. Two of the three failure detectors. [biggest]** Keep doc 23's ~350-500-line prober. Drop memberlist entirely (deletes 102 modules, the 512-byte panic, the MTU landmine, and doc 20's open questions #3 and #4). Keep NATS route pings at 2s for transport interest only — that is not a roster, it is a route timer. **From three liveness systems to one, plus one timer.**

**2. Two of the three kernel APIs.** Doc 20's proto is the contract (§2.1). This is not a cut of function; it is a cut of ~60% of the corpus's protobuf surface that cannot compile together.

**3. `FANOUT` / `CHANNEL_TYPE_FANOUT`.** Zero consumers once both roster designs reject roll-call (§2.4).

**4. `DurabilityClass` from the kernel API.** A §4 violation that doc 21's own §5 recipe does not need (§1.3).

**5. Doc 22's drain gate + kernel message queue.** Falls out with the push model (§2.2). Store-and-forward + dedupe already covers reload. **Keep the *finding* about `Kill()`; drop the 40 lines.**

**6. Doc 23's per-namespace version map on the probe.** Once state slots become LOOKUP (§2.5), the probe carries liveness only. Also cuts its 256KiB quota logic.

**7. Doc 20's LOOKUP incarnation-on-disk-wipe machinery — simplify, don't build twice.** Doc 20 calls this "the sharpest edge in the design and it is mine, not inherited" [DEDUCED, unbuilt]. **Doc 23 already solved it and neither noticed:** doc 23's `boot_id` (random per daemon start) exists precisely because *"a version counter resetting on restart makes peers hold stale state FOREVER."* Same bug, same fix. **Adopt doc 23's `boot_id` generator as doc 20's "incarnation on loss"** — persist the counter in SQLite; if the store is missing or empty on boot, mint a fresh `boot_id` and include it in the writer identity `(writer_node, boot_id, revision)`. Doc 20's hole closes with code doc 23 already designed. ~10 lines, not a subsystem.

**What I am explicitly NOT cutting**, because the last critic over-corrected here:

- **The plugin system.** Greg wants it, doc 22 measured it at 3–11ms, and it is where he thinks the value is.
- **LOOKUP.** Doc 24 is right that it is the most load-bearing type and the only one with no upstream implementation. It is real code (~150-250 lines). Build it carefully.
- **The kernel's 19 RPCs.** It is on the edge of "light" (Greg: *"the core is actually really light"*), and 11 of 19 are storage-shaped, which is worth a second look. But every one has a named consumer in doc 24. Leave it; enforce doc 20's ~3,000-line tripwire.
- **The project.** BRIEF §2's five problems are real, the shell script does not sync a roster across a sleeping laptop, and §2's "carry the message, never judge" is a coherent product. Build it.

---

## 6. UNDER-SPECIFIED / "we'll figure it out"

1. **Nobody closed the laptop lid.** All three transport-adjacent docs flag real macOS sleep as their #1 open question; doc 21 simulated it with a blackhole proxy (excellent, and it changed the design), doc 13 used `SIGSTOP`, doc 23 froze an HTTP handler. **BRIEF §1 says the laptop sleeping is the fleet's normal state, not an edge case.** After six documents nobody ran `pmset sleep` and read a peer's log on the other side. It is a half-day and it serves all three subsystems at once. **This is the single highest-value missing experiment in the corpus and it should happen before a line of kernel code.**
2. **LOOKUP replication has two competing designs and no reconciliation.** Doc 20: version vectors + anti-entropy digests on `kernel.sync.<channel>` every 30s. Doc 21: per-node rows + roll-call + pubsub deltas (prototyped and measured working). The most load-bearing primitive in the system has two sketches. Pick doc 21's mechanism (it exists) and doc 20's semantics (single-writer, revision-per-writer, return-all-claimants).
3. **Doc 20's open question #10** — subscribing to a channel whose owning plugin isn't installed locally — is "a lean, not a decision." Fine to leave, but decide it before comms ships.
4. **Doc 23's WireGuard source-IP authentication claim is [DEDUCED] and load-bearing** for having no shared secret. It flags this honestly. Verify or add the secret.
5. **Doc 24's `LOOKUP.ttl_seconds` reconcile on wake is unmeasured** and lands on the comms send path. A woken laptop holds rows that expired elsewhere. Doc 20 says TTL is a cache expiry, not a lease, and *"stated here because someone will try"* — doc 24 is the someone. Resolve it.

---

## 7. WHAT'S MISSING — §2's five problems, §7's four questions

**§2's five problems:**

| Problem | Covered? |
|---|---|
| "figured out on one machine cannot be used on another" | ✅ channels + comms plugin |
| "cant be sent to another agent on another machine" | ✅ comms plugin (doc 24), first-class |
| "how can I find that thing from another machine" | ⚠️ **deferred to search**, correctly per §4 — but the *backbone* is designed (logtail + archive, doc 24) |
| "how could all learning be centralized and searchable" | ⚠️ same — §4 explicitly makes this later and a consumer |
| "zookeeper... who's online + shared address book + ssh" | ✅ roster (doc 23) + SSH plugin, incl. the Tailscale-SSH Phase 0 win |

**§7's four questions:**

1. **ZeroMQ — does it make sense?** ✅ **Answered decisively.** See §8.
2. **Live reload in Go — what's available?** ✅ **Answered thoroughly and measured.** Doc 22 covers go-plugin, stdlib `plugin`, Yaegi, WASM, and compile-time modules, with the BEAM comparison Greg actually wants. The honest headline — *"Greg wants BEAM and is not going to get BEAM"* — is the right thing to tell him, and the doc explains precisely which half he gets (supervision + isolation) and how he buys back the other half (kernel storage).
3. **What transport types should the kernel start with?** ⚠️ **Answered three times, inconsistently** (4 / 5 / 4). Resolvable today: **four** (§2.4).
4. **Is anything in the previous research recoverable?** — assigned to investigate/15; outside my scope. The designs use its mechanism findings (harmonik's backpressure contract, the `bytes.Compare` bug, `jsonlwriter.go`) and I verified those hold.

**Genuinely missing, and worth one line each:**

- **No kernel non-goal for blobs.** §3.1 above. Write it down.
- **Nobody designed the `notes` plugin.** Named in §3.2's candidates. Not required, and §3.1's request/reply examples ("a publish note tool") suggest it is ~50 lines against `Serve`/`Respond`. Non-issue; noting it for completeness.
- **The kernel is not live-reloadable, and that is close to Greg's actual complaint.** Doc 20 flags this honestly in §12: changing channels/roster/storage restarts `fleetd`, which restarts every plugin. Greg's words were *"having to stop the whole system every time is so annoying."* The corpus's only mitigation is "keep the kernel small," and doc 20 admits *"whether 'small enough that restarts are rare' is achievable is not answerable from a spec."* **That is the right answer, but Greg should hear it in one sentence rather than discover it.**

---

## 8. THE ZEROMQ ANSWER

**Greg asked twice. He got a real answer, and it is the right kind of answer: it tells him he is wrong, with measurements, and it tells him which part he was right about.**

The corpus's answer, which I endorse:

> **The ZeroMQ *idea* is right. The ZeroMQ *library* is the wrong way to get it. And the requirement you inferred from it — no box's death may kill the system — is measurably available from the product you rejected.**

Three layers, all honest:

**(a) The library: a clean, decisive no, on evidence he can check.** `pebbe/zmq4` is cgo and cannot cross-compile to a fleet whose DGX has no Go and no C toolchain [verified: `ssh dgx 'command -v go'` → not installed]. `go-zeromq/zmq4`'s README is a maintainer resignation. `libzmq` had 9 commits in all of 2025. **And the killer, which is the honest heart of it:** ZeroMQ's own answer to durability (Titanic) is layered on the Majordomo **broker** — so following ZeroMQ's own guidance lands you back at the exact hub BRIEF §9 says sank the last round. *ZeroMQ's own documentation argues against ZeroMQ for this requirement.* That is a genuinely excellent finding and it is doc 10's.

**(b) The idea: he was right, and doc 10 says so plainly** — the socket-pattern model is *structurally* close to §3.1's "a channel has a name and a type." Not flattery; it is why "a channel is (name, type)" survives into the final design intact.

**(c) The thing he actually wanted: measured, and his inference was wrong.** He inferred NATS cannot be not-centralized. Two of three boxes killed and the survivor served pub/sub and request/reply; a node cold-starts alone in 28ms; the real DGX killed on the real tailnet and gb-mbp↔gb-mac-mini kept working at 9.27ms. The thing he fears is **JetStream**, not NATS, and every Raft call site lives in `jetstream_cluster.go` — **I verified that grep myself.** His fear is real, correctly aimed, and lands on a feature we are not enabling.

**And the best line in the whole corpus is doc 10's, which cuts against its own recommendation:**

> *"a broker is not just overhead — a broker is where the messages live. Take the post office away and you haven't just removed a bottleneck, you've removed the floor the letters sit on... 'brokerless' is not a free win. It is a trade: you give up the hub, and in exchange you inherit the hub's job."*

That is the sentence Greg needed and had not been told. It reframes the whole question: the transport is the small decision, the durable log is the project, and store-and-forward is what makes §4 and §5 stop contradicting each other. **Doc 10 deduced it, flagged it as "the claim most worth attacking," and doc 21 attacked it by building it and measuring it working, dedupe and all.** That is the corpus at its best and it is a model for how the rest of it should have handled its disagreements.

**Where the answer is compromised, and it must be fixed before he reads it:**

1. **The flattery (§1.1).** The corpus tells him the override is his own idea, using a quote he never said. **Strike it.** The answer is strong enough to survive being honest, and the brief asked for honest exploration, not rubber-stamping — this is rubber-stamping in reverse: manufacturing agreement to soften a disagreement he explicitly invited.
2. **The runner-up is contested and he should be told.** Doc 10 says mangos; doc 21 overrules to NATS. **Doc 21 wins** — doc 10's own headline ("the transport is the small decision; the log is the project") cuts against doc 10: if the log costs ~1,500 lines either way, take the transport that also gives cluster-wide interest, cross-box queue groups, wildcards and peer auto-discovery. Plus mangos' own author measured it losing 98.9% of messages with 0 send errors, and v3.4.2 is 3.9 years old [verified today]. **But "ZeroMQ: no" is unanimous and independent of that fight**, which is the part Greg asked about.
3. **One thing the corpus should say out loud and does not:** the `no responders` signal — sold by two investigations as closing §5's gap "for free / zero lines of code" — **is a hint, not an ACK.** Doc 21 corrected this by testing the fleet's *normal* state instead of the convenient one, and found the sender gets an ambiguous TIMEOUT for 61 seconds. **That correction is the most valuable measurement in the corpus** and it must reach Greg, because §5's ACK is the thing he explicitly asked for and "it's free" is the answer he'd otherwise get.

**Bottom line for Greg, in one sentence:** *ZeroMQ: no, and its own guide explains why. Your not-centralized requirement: kept, structurally, and measured by killing two of three boxes. Your NATS instinct: we are overruling it — the hub you fear is JetStream, which we turn off — and you should get the chance to say no.*

---

## 9. What to do, in order

1. **Strike the "broker on every node" quote** from 20:86, 21:91, investigate/11:45,448. Rewrite the NATS recommendation as an honest override. *(§1.1 — do this first; it changes what Greg is being asked to approve.)*
2. **Close the lid.** `pmset sleep`, read a peer's log from the other side. Half a day, serves all three subsystems, and it is the fleet's normal state. *(§6.1)*
3. **Adopt doc 20's proto as the single contract.** Add `bool sync`/`bool synced` (F9). Delete `origin_agent`, `DurabilityClass`, `FANOUT`, `STATE_LEFT`. Add `max_payload_bytes` to `InfoResponse` (F7). Fix the `Interest` comment to say what it means. *(§1.2–1.5, §2.1, §2.4, §2.6, §3.2)*
4. **Pick one failure detector: doc 23's prober.** Drop memberlist. Fix doc 23's "flagship disabled" overstatement. *(§2.3)*
5. **Unify LOOKUP and state slots.** LOOKUP wins; probe carries liveness only; adopt doc 23's `boot_id` as doc 20's incarnation-on-loss. *(§2.5, §5.7)*
6. **Settle push vs pull: pull.** Doc 22 gives up kernel-held subscriptions and the drain gate; keeps the `Kill()` finding. *(§2.2)*
7. **Write down the blob non-goal.** The kernel does not move bytes over 1MB; the answer is a URL plus a checksum. *(§3.1)*
8. **Tell Greg the four things he'd otherwise discover:** his five channel types came back as four and why; `no responders` is a hint not an ACK; the kernel itself is not live-reloadable; and we are overruling him on NATS.

---

## Sources

### Documents read
- `/Users/gb/research/2026-07-15-agent-substrate-v2/BRIEF.md` — in full, first, before anything else.
- `design/20-kernel.md` — in full (1,011 lines).
- `design/21-transport.md` — in full (666 lines).
- `design/22-plugin-system.md` — §0–4, §8 (plugin library), §11 (dependencies), plus targeted greps of §5–7.
- `design/23-roster-addressbook.md` — targeted: package/API decls, state-slot API, liveness states, §6.3.
- `design/24-validation-plugins.md` — targeted: F1/F7/F9/F10, verdict, dependencies, sources.
- `investigate/10-zeromq.md` — §0–1 (verdict + "the floor the letters sit on").
- `investigate/11-transport-options.md` — §1, §5 (middle paths), §6 — provenance of the "broker on every node" quote.
- **Not read, deliberately:** `/Users/gb/research/2026-07-15-agent-comms-substrate/` — per instructions. One targeted `grep -rl` for a single phrase, to establish provenance; no content read.

### Files read (verification)
- `~/go/pkg/mod/github.com/hashicorp/memberlist@v0.6.0/`: `state.go:1205-1222`, `suspicion.go:68-78`, `net.go:83`, `memberlist.go:460,519`, `config.go:336`.
- `~/go/pkg/mod/github.com/nats-io/nats-server/v2@v2.14.3/server/`: `jetstream_cluster.go:1046,1082,2990,2993`, `raft.go:356,624`, `route.go:140,147`, `client.go:5772-5773`, `const.go:92-99`.
- `~/go/pkg/mod/github.com/hashicorp/go-plugin@v1.8.0/`: `grpc_controller.go` (full), `grpc_server.go:127-130`, `grpc_broker.go:394-398`, `client.go:51-61`.
- `/Users/gb/harmonik-worker/repo/`: `internal/eventbus/jsonlwriter.go` (413 lines), `internal/eventbus/busimpl.go:386,1417`, `internal/daemon/subscribe.go:33-41,519`, `internal/eventbus/decision_fsync_33p_test.go:27`.
- `design/20-kernel-proto/fleet/kernel/v1/kernel.proto`, `plugin.proto` — read in full.
- `design/20-kernel-boundary-test.sh` — read and executed.

### Commands run (all today, 2026-07-16)
- `grep -in "broker" BRIEF.md` → **no matches** (the §1.1 finding).
- `grep -rl "broker on every node" ~/research/2026-07-15-agent-comms-substrate/` → no matches.
- `sed -n '/^## 5. Middle paths/,/^## 6/p' investigate/11-transport-options.md | grep "^###"` → 5 headings; "A broker on every node" is **#1**, not #4.
- `design/20-kernel-boundary-test.sh` against doc 21's extracted proto → `BANNED IDENTIFIER: origin_agent`, exit 1.
- `design/20-kernel-boundary-test.sh` against doc 20's shipped proto → `VOCABULARY CLEAN`, exit 0.
- `grep -in "durable|sync|fsync" design/20-kernel-proto/**/*.proto` → **no matches** (F9 confirmed against the artifact).
- `grep -n "^package" design/2*.md` → the three-API finding.
- `grep -rn "bytes.Compare" --include="*.go" /Users/gb/harmonik-worker/repo` → exactly 4 non-test load-bearing sites.
- `wc -l jsonlwriter.go` → 413.
- `ifconfig utun0` → `mtu 1280`.
- `go list -m -versions go.nanomsg.org/mangos/v3` → latest `v3.4.2`.

### Claims I did NOT verify
Flagged so they are not mistaken for confirmed:
- Every runtime measurement in docs 20–24 (NATS survival under 2-of-3 kill, 28ms cold start, 61s→5s window, 9.27ms cross-tailnet, go-plugin 3–11ms reload, 227k appends/sec, doc 23's 6.5s/99ms prototype). I verified the *source code* these rest on and the code matches the reported numbers' explanations; I did not re-run the experiments.
- Doc 24's harmonik-derived plugin claims (Claude `ai-title` in 55/60 transcripts, vendor slug rules, tmux ~1/3 hang rate).
- The ZeroMQ Guide quotes (`chapter4.txt:672`, `chapter5.txt:148`) and the libzmq commit counts — doc 10's, not re-fetched.
- Doc 22's WASM/Yaegi disqualifications, inherited from investigate/12.
