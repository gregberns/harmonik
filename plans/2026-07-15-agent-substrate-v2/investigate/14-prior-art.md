# 14 — Prior art: who already built "light kernel + plugins + transport", and what to steal

**Date:** 2026-07-15
**Task:** BRIEF §3 — *"maybe the core (or kernal?) is actually really light, and its the plugins that do a lot of the work."* Find the production systems shaped like that; extract concrete, stealable decisions.
**Method:** cloned and read real source (Telegraf, Benthos, hashicorp/go-plugin, memberlist), fetched real docs, and ran real experiments on this laptop and against this fleet. Everything below is labelled **[MEASURED]** (I ran it / read the code), **[CLAIMED]** (a doc or maintainer says so), or **[DEDUCED]** (my inference).

---

## Jargon, defined once

Greg is not deep in this corner of the tech space, so every term is defined at first use. The ones that recur:

- **Daemon** — a program that runs in the background forever. The thing we'd put on each of the three boxes.
- **Kernel** — here, *not* the Linux kernel. Just "the part of our daemon that is always there and that plugins are built on top of." Greg's word from §3.
- **Plugin** — a chunk of code that adds a capability (log tail, comms, notes) without changing the kernel.
- **Protobuf (Protocol Buffers)** — Google's format for defining a data structure and an API in a plain text `.proto` file, independent of any programming language. Code for Go/Python/whatever is generated from it. This is the thing Greg's instinct pointed at in §3.2.
- **gRPC** — the standard way to make function calls over a network using protobuf messages.
- **RPC (Remote Procedure Call)** — calling a function that lives in another process or on another machine.
- **Gossip** — a way for machines to track who's alive without a central server: each machine randomly pings a few others and passes on what it heard, like rumour spreading. Converges fast; no hub.
- **SWIM** — the specific, well-tested gossip algorithm ("Scalable Weakly-consistent Infection-style process group Membership") that Consul and others use for exactly Greg's §3.1 roster problem.
- **DAG (Directed Acyclic Graph)** — a wiring diagram where data flows one way and never loops back.
- **WASM (WebAssembly)** — a portable binary code format that can be safely run inside another program, sandboxed.
- **MTU (Maximum Transmission Unit)** — the biggest packet a network link will carry. Exceed it and packets get dropped, often silently. This bites us; see §5.4.
- **Ack / Nack** — "I got it and handled it" / "I got it and failed; send it again."

---

## Part 1 — Data-pipeline daemons (the closest analogues)

### 1.1 Telegraf (Go) — *the single closest thing to what Greg described*

**What it is.** InfluxData's metrics agent. One daemon per box. Everything it does is a plugin: **inputs** (collect), **processors** (transform), **aggregators** (roll up), **outputs** (ship). Greg's own "log tail → transport → dump" is literally `inputs.tail` → pipeline → `outputs.*`.

**Where the kernel/plugin line is drawn.** Startlingly far toward "tiny kernel." The entire contract for an output plugin is four methods **[MEASURED — `/tmp/priorart/tg/output.go:3-15`, commit `7c40d48`]**:

```go
type Output interface {
	PluginDescriber                 // SampleConfig() string
	Connect() error
	Close() error
	Write(metrics []Metric) error
}
```

And an input is *two* **[MEASURED — `/tmp/priorart/tg/input.go:3-9`]**: `SampleConfig()` and `Gather(Accumulator) error`. Everything else — retries, buffering, batching, flush intervals, logging, secrets, state persistence — is kernel-side and offered to the plugin as **optional interfaces it may implement if it cares** (`Initializer`, `PluginWithID`, `StatefulPlugin`, `ProbePlugin`) **[MEASURED — `/tmp/priorart/tg/plugin.go:13-63`]**. That optional-interface trick is how the contract stays two methods wide while still supporting rich plugins.

Registration is a name → constructor map, populated from `init()` **[MEASURED — `/tmp/priorart/tg/plugins/inputs/registry.go:6-14`]**:

```go
type Creator func() telegraf.Input
var Inputs = make(map[string]Creator)
func Add(name string, creator Creator) { Inputs[name] = creator }
```

**How data is routed between plugins.** This is the important part, and it is *not* what most people guess. **Plugins never address each other.** The topology is fixed and baked into the kernel — inputs fan into one channel, processors chain, outputs **fan out to all of them** **[MEASURED — `/tmp/priorart/tg/agent/agent.go:38-104`; the source literally contains ASCII art of the fan-out]**. Which output actually keeps a given message is decided by *content matching*, not by wiring: each plugin carries a `Filter` with `NamePass`/`NameDrop`/`TagPass`/`TagDrop` glob patterns **[MEASURED — `/tmp/priorart/tg/models/filter.go:35-95`]**.

**The data model is worth copying verbatim.** A Telegraf `Metric` is `Name + Tags + Fields + Time` **[MEASURED — `/tmp/priorart/tg/metric.go:20-63`]**. Tags are indexed string key/values that ride with every single message. That is exactly BRIEF §4's *"the machine, time, etc — must ride along because search will need it"*, solved at the kernel level a decade ago.

**Out-of-process plugins.** Telegraf supports plugins that are separate programs, and the design is elegant: the subprocess boundary is itself implemented **as a plugin** (`inputs.execd`), not as kernel machinery. `execd` runs your program and reads metrics off its **stdout** in a text line format **[MEASURED — `/tmp/priorart/tg/plugins/inputs/execd/README.md:1-8`]**. A helper library ("the shim") lets the same plugin code run either compiled-in or standalone **[MEASURED — `/tmp/priorart/tg/plugins/common/shim/README.md:1-10`, `goshim.go:34-56`]**. There is a real ecosystem of ~40 externally-maintained plugins built this way **[MEASURED — `/tmp/priorart/tg/EXTERNAL_PLUGINS.md`]**.

**What to steal:**
- The 2–4 method plugin contract, with everything else as *optional* interfaces.
- `name → constructor` registry.
- `Name + Tags + Fields + Time` as the kernel envelope. Metadata is kernel-level, not plugin-level.
- Content-based routing (`namepass`/`tagpass` globs) instead of wiring plugins to each other.
- Implementing the subprocess boundary **as a plugin**, so the kernel doesn't grow a process manager.

**What to avoid:**
- The hard-coded pipeline shape (input→processor→aggregator→output). That's right for metrics, wrong for us; Greg wants request/reply too, which this topology cannot express.
- stdout-as-transport. Fine for one-way metrics, hopeless for request/reply and back-pressure.

---

### 1.2 Benthos / Redpanda Connect (Go) — *the best-designed plugin API of the four*

**What it is.** A stream-processing daemon. Same four-ish component types (input/processor/output/buffer/cache/rate-limit).

**Where the kernel/plugin line is drawn.** Registration is `name + config schema + constructor` **[MEASURED — `/tmp/priorart/bt/public/service/environment.go:263-381`, commit `a9fc41b`]**:

```go
func (e *Environment) RegisterInput(name string, spec *ConfigSpec, ctor InputConstructor) error
func (e *Environment) RegisterOutput(name string, spec *ConfigSpec, ctor OutputConstructor) error
```

The `*ConfigSpec` is the difference from Telegraf. The plugin *declares its config schema to the kernel as data*, and the kernel does validation, linting, and doc generation from it. Telegraf's equivalent (`SampleConfig() string`) is just a blob of example text — much weaker.

**Three things here are directly load-bearing for this project:**

**(a) The ack is a closure handed back with the message.** **[MEASURED — `/tmp/priorart/bt/public/service/input.go:24,55`]**

```go
type AckFunc func(ctx context.Context, err error) error
Read(context.Context) (*Message, AckFunc, error)
```

Every message comes with a function to call when you're done; pass `nil` for ack, an error for nack. This is the cleanest answer I found to BRIEF §5's open ACK question. And critically — **the retry policy is an opt-in wrapper, not a kernel guarantee**: *"If your input implementation doesn't have a specific mechanism for dealing with a nack then you can wrap your input implementation with AutoRetryNacks to get automatic retries"* **[MEASURED — `input.go:20-23`]**. That is BRIEF §4's *"durability is a plugin decision... How about: the plugin defines that!"* already built and shipping.

**(b) Named resources, accessed through a callback, are how you hot-swap safely.** **[MEASURED — `/tmp/priorart/bt/public/service/resources.go:225-241`]**

```go
// AccessCache attempts to access a cache resource by name. This action can
// block if CRUD operations are being actively performed on the resource.
func (r *Resources) AccessCache(ctx context.Context, name string, fn func(c Cache)) error
func (r *Resources) HasCache(name string) bool
```

You never get a pointer to a resource. You get *scoped access inside a callback*, under a read-lock. That exists precisely so the thing can be swapped underneath you at runtime. The swap side is equally instructive **[MEASURED — `/tmp/priorart/bt/internal/manager/type.go:628-652`]**:

```go
// StoreCache attempts to store a new cache resource. If an existing resource
// has the same name it is closed and removed _before_ the new one is
// initialized in order to avoid duplicate connections.
```

...and if the old one won't close cleanly, **the replacement is refused** (`type.go:634-641`). This is a complete, working, pure-Go live-reload design for a named-thing table. No BEAM required.

**(c) Benthos has no dynamic plugin loading at all.** You import Benthos as a Go *library* and build your own binary. **[MEASURED — the entire `public/service` package is a library API; there is no loader]**

**What to steal:** `AckFunc` returned per message; opt-in `AutoRetryNacks`-style wrappers for delivery policy; `Access<Thing>(ctx, name, fn)` scoped-callback access to named resources; close-old-before-init-new with refusal on failure; plugin declares config schema as data.

**What to avoid:** the "build your own binary" model — that's Greg's harmonik pain (*"having to stop the whole system every time is so annoying"*) with extra steps.

---

### 1.3 Vector (Rust) and Fluent Bit (C) — *the two routing schools*

**Vector** wires components into an **explicit DAG**: *"Vector's pipeline model is based on a directed acyclic graph of components"*, *"Events must flow in a single direction from sources to sinks and can't create cycles"*, and *"Configuration is checked at pipeline compile time (when Vector boots). This prevents simple mistakes and enforces DAG properties"* **[CLAIMED — vector.dev/docs/architecture/pipeline-model]**. Components name each other explicitly via an `inputs` field, with glob support **[CLAIMED — vector.dev/docs/reference/configuration]**:

```yaml
transforms:
  apache_parser:
    inputs: ["apache_logs"]
    type: "remap"
```

**Fluent Bit** does the opposite: every record gets a **Tag** at ingest, and each output declares a `Match` pattern. *"The router relies on the concept of tags and matching rules"* **[CLAIMED — docs.fluentbit.io/manual/concepts/data-pipeline]**. Nobody names anybody.

**The verdict for us.** These are the two schools, and Greg has already picked one without knowing it. His words — *"a channel had a name and a type"* — are Fluent Bit's tag, not Vector's DAG. **[DEDUCED]** Tag/match is also the only one of the two that survives crossing a machine boundary: an explicit DAG has to be re-wired by hand at every hop (Vector genuinely makes you configure a matched `vector` sink and `vector` source pair to cross hosts), whereas a name is a name on any box. At n=3 with a plugin author who is an LLM agent, "publish to channel `logs.dgx.vllm`; subscribe to `logs.*`" is dramatically less to get wrong than a DAG.

**What to steal:** Fluent Bit's tag+match routing; Vector's compile-time config validation at boot and its glob support in subscriptions.
**What to avoid:** Vector's explicit DAG wiring. Wrong shape for a multi-machine, agent-authored system.

---

## Part 2 — Erlang/OTP: what's actually transferable (BRIEF §3.2, "Wish we had Erlangs Beam")

Greg wants BEAM. He can't have BEAM (§4: Go). But **three of the four things he wants from BEAM are design patterns, not runtime features**, and Go can have all three. The fourth — hot code loading — turns out to be the one he should *not* want.

### 2.1 Behaviours = the plugin interface. Steal this; you already are.

OTP's core idea: *"Common process patterns are formalized through behaviours, which separate generic code (provided by Erlang/OTP) from application-specific logic (callback modules)"* **[CLAIMED — erlang.org/doc/system/design_principles.html]**. The generic half (`gen_server`) owns the loop, the mailbox, timeouts, and lifecycle; your half is a handful of callbacks.

That is *precisely* Telegraf's `Input`/`Output` and Benthos's `Input`/`Output`. **[DEDUCED]** Three independent teams converged on gen_server. The design decision "the kernel provides generic code, the plugin provides callbacks" is settled prior art — stop treating it as a design question.

### 2.2 Supervision trees = how to get live reload without BEAM.

*"A basic concept in Erlang/OTP is the supervision tree. This is a process structuring model based on the idea of workers and supervisors"* **[CLAIMED — same]**. Workers do work; supervisors restart them when they die.

The insight Greg is circling: **BEAM's real gift isn't hot code loading, it's that restarting one thing doesn't restart everything.** That's a *structure*, and OS processes provide it. See §5.

### 2.3 Registered names = Greg's naming idea. He's right, and it's `global`.

Greg: *"a daemon defined name, thats like the key/domain name"* (§4). He asked whether that's Erlang's registered-process model. **Yes — specifically `global`.**

Erlang's `global` module provides *"Registration of global names"*, and *"the global name server monitors globally registered pids"* — meaning **when a process dies or its node goes down, the name automatically unregisters across all nodes**. The tables are replicated, not central: it *"stores names in replica tables on every node rather than centrally, enabling fast local lookup"*, and *"All nodes involved in these actions have the same view of the information"* **[CLAIMED — erlang.org/doc/apps/kernel/global.html]**.

Read that against BRIEF §4's not-centralized requirement: replicated tables on every node, no hub, name dies when the thing dies. That is the agent address book, and it is a 30-year-old shipped design.

The caveats are honest and they matter: `global` needs a fully-connected mesh (`connect_all`), and it needs `prevent_overlapping_partitions` — *"you are strongly advised not to disable this fix"*. On name clashes a resolver function picks a winner, and *"If the function crashes, or returns anything other than one of the pids, the name is unregistered"* **[CLAIMED — same]**. **[DEDUCED]** At n=3 on one Tailscale network, fully-connected is 3 links and free. The clash-resolver requirement is real though: two agents registering the same name must have a deterministic tiebreak, or you lose the name entirely.

Erlang's *other* answer, `pg` (process groups), is the pubsub-shaped one, and it makes the weaker promise on purpose: *"Process Groups implement strong eventual consistency. Process Groups membership view may temporarily diverge"* and *"Membership view is not transitive. If node1 is not directly connected to node2, they will not see each other's groups"* **[CLAIMED — erlang.org/doc/apps/kernel/pg.html]**. **[DEDUCED]** Note what OTP did here: it shipped **two** name systems with **different consistency guarantees** for different jobs, rather than one system with a CAP argument attached. That's the move for §4's *"I dont want to get hung up on CAP theorem and shit"* — pick eventual consistency, write it down, move on.

### 2.4 Hot code loading — the thing to NOT copy.

Even in Erlang, where this is a headline feature, it's genuinely hard. You write `.appup` files per application and `.relup` files per release; state-changing upgrades suspend the process and call `code_change/3`. The docs warn: *"Complicated or circular dependencies can make it difficult or even impossible to decide in which order things must be done without risking runtime errors"* and *"new processes created in the time window between suspending processes using a certain module, and loading a new version of this module, can execute old code"* **[CLAIMED — erlang.org/doc/system/release_handling.html]**.

**[DEDUCED]** BEAM envy is aimed at the wrong feature. Greg doesn't want `code_change/3` — he wants "don't make me stop the whole system." Supervision + process restart gives him that. Hot code loading is the 5% on top that costs 95% of the effort, and Erlang's own docs are the best argument against it.

**What to steal:** behaviours (already are), supervision trees (via OS processes), `global`-style replicated name tables with death-triggered deregistration, and OTP's willingness to ship two consistency models for two jobs.
**What to avoid:** hot code loading. Also `global`'s fully-connected requirement is only free because n=3 — don't build on it as if it scales.

---

## Part 3 — Extensible daemons

### 3.1 Caddy — *build-time plugins, and it's a deliberate choice*

Web server; everything is a module. Registration via `caddy.RegisterModule()` in `init()`. Module IDs are dotted namespaces (`http.handlers.reverse_proxy`) where *"Namespaces are like classes, i.e. a namespace defines some functionality that is common among all modules within it"*. Optional lifecycle interfaces: `Provisioner`, `Validator`, `CleanerUpper` — the same optional-interface trick as Telegraf. And decisively: plugins are **compiled in, not dynamically loaded**; *"The `xcaddy` command... compiles Caddy with your plugin, then runs it"* **[CLAIMED — caddyserver.com/docs/extending-caddy]**.

**Steal:** dotted namespaced module IDs (`comms.channel.pubsub`, `roster.node`) — this is a good naming scheme for Greg's channels *and* plugins. Optional lifecycle interfaces.
**Avoid:** build-time-only plugins. Caddy can get away with it because operators recompile once and deploy; we want an agent to add a plugin at 2am without a rebuild-and-redeploy of all three boxes.

### 3.2 Traefik — *the most important negative result in this document*

Traefik is the one system that took the "interpret Go at runtime" path, via **Yaegi**, its own Go interpreter: plugins are *"executed on the fly by Yaegi, a Go interpreter embedded in Traefik"* — no precompilation, no linking **[CLAIMED — doc.traefik.io/traefik/extend/extend-traefik/]**. It also now supports WASM plugins (any language, sandboxed, middleware only) **[CLAIMED — traefik-traefik.mintlify.app/plugins/overview]**.

**And after all that, it still won't hot-load them.** *"To add a new plugin to a Traefik instance, you must change that instance's install (static) configuration"* **[CLAIMED — doc.traefik.io/traefik/extend/extend-traefik/]** — static config, i.e. restart. A search-result snippet of an earlier doc revision states it even more bluntly: *"Plugins are parsed and loaded exclusively during startup... For security reasons, it is not possible to start a new plugin or modify an existing one while Traefik is running"* **[CLAIMED — search snippet attributed to Traefik docs; I could not re-verify this exact sentence in the current JS-rendered docs, so treat the wording as unconfirmed even though the current doc's "static configuration" wording implies the same]**.

**[DEDUCED]** This is the strongest available evidence on Greg's live-reload question. The team that built an entire Go interpreter to make dynamic plugins possible **chose not to make them dynamic**. If interpreting Go were the answer, Traefik would be the proof, and it isn't.

Yaegi's own limitations seal it **[CLAIMED — github.com/traefik/yaegi]**: *"Go modules are not yet supported"*, no cgo, *"Interpreting computation intensive code is likely to remain significantly slower than in compiled mode"*, and *"Support the latest 2 major releases of Go"* — i.e. it lags the toolchain. Our plugins are I/O-shaped (log tail, ssh helper, notes) and will want ordinary Go libraries. No modules is a dealbreaker.

**Steal:** the plugin manifest file idea (every Traefik plugin ships a `.traefik.yml` at its root describing itself, with `testData` for testing) **[CLAIMED — plugins.traefik.io/create]**.
**Avoid:** Yaegi. Avoid interpreting Go, full stop.

### 3.3 Envoy — *xDS teaches one genuinely important thing*

Envoy is configured dynamically by a management server pushing config over gRPC: **LDS** (listeners), **RDS** (routes), **CDS** (clusters = groups of backends), **EDS** (endpoints = the actual live members of a cluster) **[CLAIMED — envoyproxy.io/docs/envoy/latest/intro/arch_overview/operations/dynamic_configuration]**.

Why are CDS and EDS separate? The docs answer it directly: *"when a cluster definition is updated, the operation is graceful. However, all existing connection pools will be drained and reconnected. EDS does not suffer from this limitation"* — hence *"we recommend still using the EDS API for clusters specified via CDS"* **[CLAIMED — same]**.

**[DEDUCED] This is the lesson: separate the slow-changing structure from the fast-changing membership, or the fast churn will constantly tear down the structure.** For us: *which plugins and channels exist* changes rarely. *Which of the three boxes is up right now* changes constantly — gb-mbp is a laptop that sleeps (§1). If node liveness is part of plugin/channel config, every time Greg shuts his lid, every plugin gets reconfigured. Envoy already paid for this lesson. Two separate streams.

**Steal:** the CDS/EDS split — roster events and config events are different streams with different rates.
**Avoid:** the management-server topology itself. xDS is hub-and-spoke by construction; that's §4's violation and exactly the mistake the previous research round made.

### 3.4 osquery — *the cleanest argument for subprocess plugins*

osquery's extensions are *"separate processes that communicate over a Thrift IPC channel to osquery core in order to register one or more plugins or virtual tables."* Core *"start[s] an 'extension manager' Thrift service thread that listens for extension register calls on a UNIX domain socket."* The stated payoff: *"Your extension will be version-compatible with our publicly-built binary packages"*, plus *"better isolates OS API dependencies"* and lets extensions be written in *"C++, Python, Go, or any Thrift-compatible language"* **[CLAIMED — osquery.readthedocs.io/en/stable/development/osquery-sdk/]**.

Note the shape: **plugins connect *inward* to a socket and register themselves**, rather than the core hunting for `.so` files. Greg's §3.1 — *"There would be a registration process with the daemon, and the plugin would say what resources it was interested in"* — is this, exactly.

**Steal:** unix-domain-socket registration; plugin declares its interests at registration; out-of-process for version independence from the core binary.

### 3.5 HashiCorp go-plugin — *the one to actually use*

**What it is.** The plugin system behind Terraform, Vault, Nomad, Packer, Boundary, Consul. *"has been in use by HashiCorp tooling for over 4 years"*, *"used on millions of machines"* **[MEASURED — `/tmp/priorart/gp/README.md:1-16`]**.

**How it works.** Plugin = subprocess. Communication = gRPC over a local socket. *"Plugins are Go interface implementations... you just implement an interface as if it were going to run in the same process."* Explicitly scoped to our situation: *"it is currently only designed to work over a local [reliable] network"* **[MEASURED — `README.md:12-14`]** — which is fine, because plugins are local to their daemon; the *daemons* talk to each other separately.

**Versioning is done at two levels, and this matters for §3.2.** There's a coarse integer handshake, not just protobuf field rules **[MEASURED — `/tmp/priorart/gp/server.go:28-76`]**:

```go
const CoreProtocolVersion = 1        // the plugin system's own protocol
type HandshakeConfig struct {
	ProtocolVersion uint             // your interface's version
	MagicCookieKey   string          // "not a security measure, just a UX feature"
	MagicCookieValue string
}
VersionedPlugins map[int]PluginSet  // serve several versions at once, negotiate
```

The handshake itself is one line printed on the subprocess's stdout **[MEASURED — `/tmp/priorart/gp/server.go:421-432`]**:

```go
protocolLine := fmt.Sprintf("%d|%d|%s|%s|%s|%s",
	CoreProtocolVersion, protoVersion, listener.Addr().Network(),
	listener.Addr().String(), protoType, serverCert)
```

That's the whole bootstrap: launch subprocess, read one line from its stdout, learn what socket to dial. Also present: **plugins can be reattached so the host can be upgraded while the plugin keeps running** **[MEASURED — `README.md:68-73`]**, logs are relayed to the host, and plugins can be checksum-verified and mTLS'd.

And — small but telling — **go-plugin itself is built with buf** **[MEASURED — `/tmp/priorart/gp/buf.yaml`, `/tmp/priorart/gp/buf.gen.yaml`]**. The reference implementation of protobuf plugin interfaces uses the exact tool §5 recommends.

**Steal:** essentially the whole thing. Use the library.
**Avoid:** nothing significant. Its "local reliable network only" caveat is a match for our design, not a problem.

---

## Part 4 — ZooKeeper / Kafka / KRaft (BRIEF §3.1, Greg's own framing)

Greg's framing: *"Its kinda only there to keep track of whos around. Then on top of that, for example is Kafka which does other stuff."* He's describing the layering accurately. The question is whether to copy it.

**What Kafka did and why.** Kafka spent a decade on ZooKeeper and then removed it (KIP-500). The stated motivations, verbatim **[CLAIMED — cwiki.apache.org KIP-500]**:

1. **Two systems is the tax.** *"system administrators need to learn how to manage and deploy two separate distributed systems"* — and it leaks: *"administrators may set up SASL on Kafka, and incorrectly think that they have secured all of the data travelling over the network"* while ZooKeeper sits there unsecured.
2. **Divergence is inevitable.** *"although ZooKeeper is the store of record, the state in ZooKeeper often doesn't match the state that is held in memory in the controller"* — with no reliable way to resync short of restarting the controller.
3. **Metadata-as-writes, not metadata-as-log, is the root cause.** *"We treat changes to metadata as isolated changes with no relationship to each other"*, therefore *"it is possible for brokers to get some of the changes, but not all."*

**The verdict for n=3.** Two answers, and they pull in the same direction:

- **On the layering: bake the roster in — Greg's instinct (§3.1) is right, and KIP-500 is the evidence.** He wants ZooKeeper's *job*, not ZooKeeper's *deployment*. KIP-500's headline regret is running a second distributed system. Spawning a separate roster service next to our daemon on three boxes would be re-making Kafka's mistake on the day we start. **[DEDUCED]**
- **On the mechanism: steal the "metadata is a log" fix.** The roster should be an ordered stream of events (`node joined`, `node left`, `agent registered`) that each daemon replays, not a set of independent key writes. Reason #3 above is exactly the bug you get otherwise — *"some of the changes, but not all"* — and it's how you end up with gb-mac-mini believing dgx is up while gb-mbp believes it's down. **[DEDUCED]** Conveniently, this is also what memberlist's `EventDelegate` hands you (§5.4): a stream of join/leave/update events.

**What does KRaft *not* teach us?** KRaft replaced ZooKeeper with an internal **Raft consensus quorum**. Do not copy that. Raft is a leader-elected majority-vote protocol; it exists to make metadata linearizable, which is a property BRIEF §6 explicitly disclaims (*"High availability, consensus, CAP rigor"* — not the problem). At n=3, Raft also means a majority is 2, so **losing two boxes stops the roster entirely** — and Greg's §4 line is *"Part of the problem is that I dont trust my boxes."* Gossip degrades (each box keeps its own best guess); Raft stops. Take KIP-500's *diagnosis*, not its *cure*. **[DEDUCED]**

---

## Part 5 — Protobuf plugin APIs (the load-bearing section)

Greg's instinct, from §3.2: *"probably protobuf — must be versioned and probably backwards (maybe forwards?) compatible"* and *"I say protobuf because we can define it COMPLETELY independently of any code. Also could be cool because the same interfaces could be available through REST/pubsub/whatever."*

**Verdict up front: the instinct is correct on all three counts, and one of them ("maybe forwards?") is more correct than he realised.** Details, with receipts.

### 5.1 Protobuf's own compatibility rules — forwards compatibility is free, with one trap

From the official spec **[CLAIMED — protobuf.dev/programming-guides/proto3/]**:

- Safe: *"Adding new fields is safe"*, *"Removing fields is safe"* (if you reserve the number), *"Adding additional values to an enum is safe"*.
- Not safe: *"Changing field numbers for any existing field is not safe"*, *"Moving fields into an existing `oneof` is not safe"*.
- Field numbers: *"This number cannot be changed once your message type is in use."* Deleting requires `reserved 2; reserved "payload";` — because *"Reusing a field number makes decoding wire-format messages ambiguous"*, with consequences listed as *"A parse/merge error (best case scenario), Leaked PII/SPII, Data corruption."*

**On Greg's "maybe forwards?" question — yes, and it's automatic:** *"Proto3 messages preserve unknown fields and include them during parsing and in the serialized output."* An old plugin that receives a message containing a field it has never heard of keeps that field intact and passes it through. That is forwards compatibility for free.

**The trap:** unknown fields are lost if you *"Serialize to JSON"* or copy field-by-field. The guidance is explicit: *"Use binary; avoid using text formats for data exchange"* and *"Use message-oriented APIs, such as CopyFrom() and MergeFrom()."* **[DEDUCED]** So: the plugin↔daemon and daemon↔daemon hops must be **binary protobuf**. Offering JSON at the edge (§5.3) is fine — just never round-trip an envelope through it.

### 5.2 buf — I ran the experiments, and the default advice is wrong for us

**buf** is a linter and, more importantly, a **breaking-change detector**: it compares your `.proto` files against a baseline (e.g. git `HEAD`) and fails CI if you broke someone. Its four rule categories, strictest to loosest, are **FILE**, **PACKAGE**, **WIRE_JSON**, **WIRE**; *"Passing a stricter category implies passing every looser one"*, and **WIRE_JSON is buf's own recommended minimum**: *"Because JSON is ubiquitous, this is the recommended minimum level"* **[CLAIMED — buf.build/docs/breaking/rules/, via search]**.

I installed buf 1.71.0 and ran six experiments against a mock plugin interface. **[MEASURED — all of the following; setup at `/tmp/bufexp`, `buf --version` = 1.71.0]**

| # | Change | `WIRE_JSON` verdict |
|---|---|---|
| 1 | Add a new field (`int64 sent_unix_ms = 4`) | **pass** — safe |
| 2 | Renumber existing field (`payload 2 → 5`) | **fail** — *"Previously present field \"2\" with name \"payload\" ... was deleted without reserving the number \"2\""* |
| 3 | Delete a field **and** `reserved 2; reserved "payload";` | **pass** — safe |
| 4 | Change type `string → int64` on same number | **fail** |
| 5 | Widen `int32 → int64` on same number | **fail** |
| 6 | **Delete an entire RPC method from a service** | **PASS — not detected** |

Two of those deserve attention:

**Experiment 5 — buf is stricter than the wire, on purpose.** Protobuf's own doc says int32↔int64 is wire-compatible; buf's WIRE_JSON flags it. I re-ran it at `WIRE` only and it passed **[MEASURED]**. Both are right: it's fine in binary, and it breaks JSON, because protobuf's JSON mapping encodes 64-bit ints as *strings* and 32-bit ints as *numbers*. Good example of why you want the tool rather than the rules in your head.

**Experiment 6 — buf's *recommended* level silently allows deleting an RPC.** This is the finding that changes the recommendation. `WIRE_JSON` cares about *encoding*, and deleting a method doesn't break any encoding — it just means callers get "unimplemented" at runtime. I re-ran the identical deletion at `PACKAGE` **[MEASURED]**:

```
proto/substrate/v1/plugin.proto:11:1: Previously present RPC "Publish" on service "PluginHostService" was deleted.
```

**[DEDUCED] Therefore: use `PACKAGE`, not the recommended `WIRE_JSON`.** For a plugin API, the *service surface* is the contract, and WIRE_JSON doesn't protect it. This is a two-line config decision that would otherwise be discovered the hard way, at runtime, on a box Greg isn't sitting at.

buf's linter also earns its place: my first-draft service was named `PluginHost` and buf rejected it — *"Service name \"PluginHost\" should be suffixed with \"Service\""* **[MEASURED]**. With LLM agents authoring plugins, a mechanical style gate is worth a lot.

### 5.3 ConnectRPC — Greg's "same interfaces through REST/whatever" is real. I checked.

Connect is *"a family of libraries for building browser and gRPC-compatible HTTP APIs: you write a short Protocol Buffer schema and implement your application logic, and Connect generates code to handle marshaling, routing, compression, and content type negotiation"*, and *"Connect servers and clients support three protocols: gRPC, gRPC-Web, and Connect's own protocol"* — the last being *"a straightforward HTTP-based protocol that works over HTTP/1.1, HTTP/2, and HTTP/3"* **[CLAIMED — connectrpc.com/docs/introduction/]**.

I didn't take their word for it. Against their live demo **[MEASURED]**:

```
$ curl --http1.1 -i -H "Content-Type: application/json" \
    --data '{"sentence": "I feel happy."}' \
    https://demo.connectrpc.com/connectrpc.eliza.v1.ElizaService/Say
HTTP/1.1 200 OK
content-type: application/json
{"sentence":"Feeling happy? Tell me more."}
```

Then the **same URL**, same service, with a gRPC-Web binary body: `200 application/grpc-web+proto` **[MEASURED]**. One protobuf definition, one endpoint, answering both plain-JSON-over-HTTP/1.1 and binary gRPC. That is BRIEF §3.2's *"the same interfaces could be available through REST"*, verified, today.

**The honest correction to the instinct:** this gives you **REST for free, but not pubsub for free.** Connect covers request/reply and streaming over HTTP. Publishing a message to a channel via a `.proto` definition is something we'd map ourselves (a `Publish(Envelope)` RPC is trivial; a *subscription* is a server-streaming RPC). So: "same interface over REST" — yes, measured. "Same interface over pubsub" — the *messages* are shared for free; the *pubsub semantics* are ours to define. Don't let the protobuf definition imply the transport semantics come with it. **[DEDUCED]**

### 5.4 gRPC reflection — discovery for free, and it's better than it sounds

**Reflection** lets a server tell a client what services and methods it has, at runtime, so the client needs no local `.proto` file. I tested it with `grpcurl` against the same demo **[MEASURED]**:

```
$ grpcurl demo.connectrpc.com:443 list
connectrpc.eliza.v1.ElizaService

$ grpcurl demo.connectrpc.com:443 describe connectrpc.eliza.v1.ElizaService.Say
rpc Say ( .connectrpc.eliza.v1.SayRequest ) returns ( .connectrpc.eliza.v1.SayResponse )

$ grpcurl -d '{"sentence":"prior art"}' demo.connectrpc.com:443 connectrpc.eliza.v1.ElizaService/Say
{ "sentence": "How do you feel when you say that?" }
```

Three commands, zero schema files, full call. **[DEDUCED]** This matters more here than in a normal system: BRIEF §4 wants agents to never *"screw around figuring out the network crap."* With reflection on, an agent can ask a daemon "what can you do?" and get a machine-readable, always-current answer — no docs to go stale, no schema to ship. Greg's own examples (*"a search tool on the network"*, *"a publish note tool"*) become discoverable rather than documented.

### 5.5 The versioning verdict for §3.2

Do **all three**, because they cover different failure modes and the real systems all do more than one:

1. **Coarse handshake version** (go-plugin's `ProtocolVersion` + `VersionedPlugins`) — catches "this plugin is from a different era" at connect time with a human-readable error, instead of a weird decode failure later.
2. **Protobuf field rules within a version** — free forwards compatibility via unknown-field preservation, provided the envelope stays binary.
3. **`buf breaking` at `PACKAGE` level in CI** — mechanically enforces #2 *and* protects the service surface, which #2 alone does not (Experiment 6).

---

## Part 6 — The plugin library (BRIEF §3.2: *"A plugin gets added to one node, the plugin gets synced across nodes"*)

**Verdict: do it — but sync the *manifest*, never the *bytes*. As stated ("the plugin gets synced across nodes"), it is a famous trap. One word changed, it's a solved pattern that two independent giants converged on.**

### The evidence

**Istio/Envoy built exactly this and it took years.** The mechanism they landed on is the interesting part: config distribution and code distribution are **deliberately separated**. Envoy's **ECDS** (Extension Config Discovery Service) *"allows extension configurations (e.g. HTTP filter configuration) to be served independently from the listener"* — and then: *"istio-agent intercepts the extension config resource update from istiod, reads the remote fetch hint from it, downloads the Wasm module, and rewrites the ECDS configuration with the path of the downloaded Wasm module"* **[CLAIMED — istio.io/latest/blog/2021/wasm-progress/ and envoyproxy.io ECDS docs, via search]**.

Read that carefully: **the control plane ships a pointer; a local agent fetches the bytes over a normal HTTP fetch and rewrites the config to point at the local file.** The binary never travels on the control channel.

Their hard-won operational rules **[CLAIMED — same]**:
- *"It is highly recommended to provide the checksum, since missing checksum will cause the Wasm module to be downloaded repeatedly."* (Content-addressing isn't optional; it's your cache key.)
- *"If the download fails, the agent will reject the ECDS update to prevent invalid Wasm filter configuration from reaching the Envoy proxy."* (**Fetch failure must fail the config update, not the data plane.**)
- Initial fetch timeout of `0s` means the listener *"will wait indefinitely for the first extension configuration update"* — a plugin that won't download can wedge the thing that depends on it.

**Nomad converged on the identical shape** for its `artifact` block: *"Nomad downloads artifacts using the popular `go-getter` library"*, with `checksum = "md5:df6a..."` and *"if the checksum is invalid, an error will be returned"* **[CLAIMED — developer.hashicorp.com/nomad/docs/job-specification/artifact]**. URL + checksum, fetched by the node, verified before use.

**Traefik shows the failure mode.** Its remote plugins are *"downloaded automatically from GitHub repositories"* at startup, and *"the module name and version are validated, and the archive hash is optionally checked"* **[CLAIMED — traefik-traefik.mintlify.app/plugins/overview]**. **Optionally.** **[DEDUCED]** That's the trap in miniature: once plugins auto-download, an optional integrity check means a box can silently run different code from its siblings, and you will debug the *symptom* on one machine for an hour before suspecting the *binary*.

### Why "sync the bytes" specifically is the trap

**[DEDUCED]** If a plugin binary replicates across nodes as a blob on the transport, you have quietly signed up to build:
- a package manager (versions, upgrades, rollback, GC of old versions);
- an integrity story (what stops a corrupt copy propagating to all three boxes?);
- a platform matrix — and ours is genuinely heterogeneous: `darwin/arm64` on gb-mbp and gb-mac-mini, `linux/arm64` on dgx (§1). A darwin binary synced to the DGX is not a plugin, it's garbage. Any bytes-sync must be platform-keyed from day one;
- an answer to "gb-mbp was asleep when the plugin landed" (§1 — it sleeps and reboots), which is a full anti-entropy problem.

**The shape that works, with both Istio and Nomad as precedent:**

1. A plugin is identified by `name + version + platform + sha256`.
2. What replicates across the fleet is **that record** — small, ordered, gossip-friendly, and exactly the kind of thing the roster stream already carries.
3. Each daemon fetches the bytes itself, over plain HTTP/SSH/whatever, from wherever the record says, and **verifies the sha256 before it ever executes**.
4. Fetch failure is loud, local, and non-fatal: the node reports "plugin X v3 wanted, not running" into the roster. It does not take the daemon down; §4 forbids one box's problem becoming the fleet's problem.
5. The origin can be another daemon serving the file over HTTP — which gets Greg his *"add it to one node and it spreads"* experience without a central artifact server and without a hub whose loss is fatal.

And per Greg's §3.2, this whole thing is **a plugin**. The kernel gets one new obligation: expose the plugin-manifest list as a replicated, subscribable resource. It already needs that for the roster.

---

## Part 7 — Where the "live reload" answer actually comes from (the experiments)

Greg's §7.2 open question: *"Live reload in Go — what's actually available?"* I tested the options rather than reciting folklore.

### 7.1 Go's native `plugin` package: it works here, and you still must not use it

**[MEASURED]** On this laptop (`go1.26.2 darwin/arm64`), `-buildmode=plugin` built and loaded cleanly — which genuinely surprised me, and is why I tested instead of asserting:

```
$ go build -buildmode=plugin -o /tmp/goplug/p.so ./plug   # 4.0 MB .so
$ /tmp/goplug/host
plugin init ran
CALL OK: hello from plugin v1
```

So the usual "it doesn't work on macOS" claim is stale. It works. It's still disqualified, for two reasons I measured:

**Reason 1 — it cannot unload. Ever.** From the official docs on this machine **[MEASURED — `go doc plugin`]**: *"A plugin is only initialized once, and cannot be closed."* There is no `Close`, no `Unload` — the `plugin.Plugin` type has exactly two operations, `Open` and `Lookup` **[MEASURED — `go doc plugin.Plugin`]**. So `plugin` gives you *load-once-at-startup*, which is what a plain import gives you, but slower and more fragile. **It is not live reload. It cannot become live reload.**

**Reason 2 — the killer, and it's beautifully ironic.** I built a host and a plugin sharing a type, then made the single most ordinary change imaginable — **added one field to the shared struct** — and rebuilt only the host **[MEASURED]**:

```go
// before
type Envelope struct{ Channel string; Payload []byte }
// after
type Envelope struct{ Channel string; Payload []byte; OriginNode string }
```
```
OPEN ERR: plugin.Open("/tmp/goplug/p"): plugin was built with a different version of package goplug/shared
```

Every plugin binary on the fleet is invalidated by adding a field. Now compare with §5.2, Experiment 1: **adding a field is the canonical *safe* change in protobuf, and buf confirms it passes.** So Go's `plugin` package has *precisely the opposite* compatibility properties from the interface style Greg's instinct picked. Choosing `plugin` would mean the most common evolution of the system — adding a field to the envelope — requires a synchronised rebuild-and-redeploy of every plugin on all three boxes. That is a worse version of the harmonik pain he's trying to escape.

The official docs pile on **[MEASURED — `go doc plugin`]**: *"Plugins are currently supported only on Linux, FreeBSD, and macOS"*, *"Plugins are poorly supported by the Go race detector"*, and *"Reasoning about program initialization is more difficult when some packages may not be initialized until long after the application has started running."*

### 7.2 So what *is* live reload in Go? — restart the plugin, not the daemon

**[DEDUCED]** Line the results up:

| Option | Verdict | Basis |
|---|---|---|
| `plugin` / `.so` | **No.** Cannot unload; adding a field breaks every plugin | **[MEASURED]** §7.1 |
| Yaegi (interpret Go) | **No.** No Go modules, no cgo, lags toolchain — and Traefik built it and still won't hot-load | **[CLAIMED]** §3.2 |
| WASM | **Not yet.** Sandboxing we don't need on a trusted network (§4); painful I/O — and our plugins are all I/O (tail files, ssh, disk) | **[DEDUCED]** |
| Rebuild the binary (Caddy/Benthos) | **No.** That *is* the harmonik complaint | §1.2, §3.1 |
| **Subprocess + gRPC (go-plugin)** | **Yes** | §3.5 |

Greg's actual requirement is in his own words: *"having to stop the whole system every time is so annoying."* He didn't ask to swap code inside a running process — he asked not to stop everything. **Subprocess plugins deliver that literally: to reload a plugin, kill it and start the new binary. The daemon never stops. The other plugins never notice.**

And notice what that is: **a supervision tree (§2.2) with OS processes as the workers.** It's the OTP structure Greg wants, minus the BEAM he can't have — and OS processes give you something BEAM doesn't, since a plugin that segfaults or leaks can't take the daemon with it. go-plugin's reattach feature even inverts it: *"Plugins can be 'reattached' so that the host process can be upgraded while the plugin is still running"* **[MEASURED — `/tmp/priorart/gp/README.md:68-73`]** — you can upgrade the *kernel* under live plugins.

For state *inside* the daemon (the channel table, the resource registry), Benthos's pattern is the answer and needs no processes at all: named entries, scoped-callback access under a read-lock, close-old-before-init-new, refuse the swap if the old one won't close (§1.2).

### 7.3 A measured landmine on this specific fleet

**[MEASURED]** memberlist (§5.4 below, and the recommended roster) defaults to `UDPBufferSize: 1400` **[MEASURED — `/tmp/priorart/ml/config.go:336`]**, with the comment *"A safe value for this is typically 1400 bytes (which is the default). However, depending on your network's MTU... you may be able to increase this"* **[MEASURED — `config.go:234-241`]**.

This fleet's Tailscale interface is **MTU 1280** **[MEASURED — `ifconfig utun0` on gb-mbp: `mtu 1280`, `inet 100.87.151.114`]**. I probed the real path to dgx with the don't-fragment bit set **[MEASURED]**:

```
payload=1200B (IP pkt=1228B): 1 packets transmitted, 1 packets received, 0.0% packet loss
payload=1252B (IP pkt=1280B): 1 packets transmitted, 1 packets received, 0.0% packet loss
payload=1272B (IP pkt=1300B): 0 received     <-- silently dropped
payload=1300B (IP pkt=1328B): 0 received
payload=1372B (IP pkt=1400B): 0 received
```

**Anything over 1280 bytes is silently dropped on this network.** memberlist's default gossip packet would be ~1428 bytes on the wire — dropped, with no error. The failure mode is vicious: TCP push/pull (every 30s) still works, so membership *mostly* syncs, while the UDP failure-detection probes vanish and nodes flap between "suspect" and "alive" forever. **Set `UDPBufferSize` to ~1200 on day one.** (Both other boxes are up and fast right now — dgx 9.05ms avg, gb-mac-mini 11.74ms avg **[MEASURED — `ping -c2`]**.)

This is also the Tailscale-MTU gotcha that already bit the monitoring stack on these same boxes, arriving in a new costume.

---

## Part 8 — Synthesis: the 8 design decisions to copy

Ranked by how much they'd cost to get wrong.

### 1. Plugins are subprocesses speaking protobuf/gRPC over a unix socket. Use `hashicorp/go-plugin`.
**From:** go-plugin (Terraform/Vault/Nomad, *"millions of machines"*), osquery (Thrift over a unix socket), Telegraf execd.
**Why it wins:** it's the only option that delivers Greg's actual live-reload requirement — *"having to stop the whole system every time is so annoying"* — because you restart one plugin, not the daemon. Go's `plugin` package is disqualified by measurement: it *"cannot be closed"* and adding one field to a shared struct invalidates every plugin binary (§7.1). Yaegi is disqualified by Traefik's own choice not to hot-load despite having built it (§3.2). Rebuilding the binary is the problem, not the fix.
**Bonus:** this is an OTP supervision tree (§2.2) with OS processes as workers — the thing BEAM envy is really about — plus crash isolation BEAM doesn't offer.

### 2. Keep the plugin contract at 2–4 methods; make everything else an *optional* interface.
**From:** Telegraf (`Output` = Connect/Close/Write; `Input` = SampleConfig/Gather; extras via `Initializer`, `StatefulPlugin`, `ProbePlugin`), Caddy (`Provisioner`/`Validator`/`CleanerUpper`), OTP behaviours.
**Why it wins:** it's the direct, mechanical expression of §3's *"the core is really light and the plugins do the work"* — and three independent teams plus OTP converged on it. The optional-interface trick is how you keep a two-method contract without crippling rich plugins. Registration is `name → constructor`, plus a **config schema declared as data** (Benthos's `RegisterInput(name, spec, ctor)`), so the kernel does validation and docs for free. Don't copy Telegraf's `SampleConfig() string` blob.

### 3. Route by channel name + glob match. Never wire plugins to each other.
**From:** Fluent Bit (Tag + `Match`), Telegraf (`namepass`/`tagpass`, plus fan-out-to-all-outputs), Vector (`inputs: ["app*"]` globs).
**Why it wins:** Greg already picked this — *"a channel had a name and a type"* is Fluent Bit's tag. It's also the only routing model that crosses a machine boundary unchanged: a name is a name on any box, whereas an explicit DAG must be re-wired at every hop. Publish to `logs.dgx.vllm`, subscribe to `logs.*`. Validate the whole config at boot, Vector-style, so mistakes surface at start rather than at 3am. **Avoided:** Vector's DAG.

### 4. The envelope is `Channel + Type + Tags + Time + Payload`. Metadata is kernel-level.
**From:** Telegraf's `Metric` = `Name + Tags + Fields + Time`.
**Why it wins:** BRIEF §4 requires metadata — *"the machine, time, etc"* — to ride along for a search layer that doesn't exist yet. Telegraf solved this by making indexed tags a property of *every* message in the kernel, which is also what makes `tagpass` routing possible (#3) at zero extra cost. If tags live in the plugin's payload instead, the kernel can't route on them and the future search plugin has to parse N payload formats. Keep the payload opaque bytes — that's §3.1's *"the daemon would do data transport, while the plugin handled all the logic."*

### 5. Ack is a closure returned with the message. Delivery guarantees are opt-in wrappers.
**From:** Benthos (`Read(ctx) (*Message, AckFunc, error)`, `AckFunc(ctx, err)`, and `AutoRetryNacks` as a wrapper).
**Why it wins:** it answers §5's open question — *"we actaully should probably notify the sender that the receiver is not listening... some type of ACK system"* — and §4's *"durability is a PLUGIN decision... How about: the plugin defines that! Then we can have multiple options"* with one mechanism. The closure carries the "how do I ack this" context so the kernel doesn't track message IDs; the wrapper library is where "at-least-once" or "fire-and-forget" gets chosen, per plugin, exactly as Greg specified. Shipping, in Go, today.

### 6. The roster is `hashicorp/memberlist` (SWIM gossip) inside the daemon. Not a separate service. Not Raft.
**From:** Consul/Serf, Erlang `global`+`pg`, and KIP-500 as the cautionary tale.
**Why it wins:** it satisfies §3.1 (*"machines have a list of other machines. Then send pings to each other to check they're online"* — that is SWIM, verbatim) and §4's not-centralized (gossip has no hub; each box keeps its own view) with a Go library and no extra process. KIP-500's headline regret is *"system administrators need to learn how to manage and deploy two separate distributed systems"* — so bake it in, exactly as Greg's instinct said. Reject Raft/KRaft: it needs a majority (2 of 3), so losing two boxes stops the roster dead, and it buys linearizability that §6 explicitly disclaims.
**The plugin hook is already built:** `EventDelegate.NotifyJoin/NotifyLeave/NotifyUpdate` **[MEASURED — `/tmp/priorart/ml/event_delegate.go:10-23`]** is precisely §3.1's *"the plugin would say what resources it was interested in... Maybe it needed changes in the node list."* Take KIP-500's diagnosis too — *"it is possible for brokers to get some of the changes, but not all"* — and make the roster an ordered event stream, not independent key writes, which is what `EventDelegate` hands you anyway.
**Three measured constraints, free of charge:**
- **`UDPBufferSize: 1400` default vs this fleet's measured 1280 MTU → set it to ~1200 or watch nodes flap forever** (§7.3).
- **`MetaMaxSize = 512`** bytes **[MEASURED — `/tmp/priorart/ml/net.go:83`]** — the agent list will not fit in `NodeMeta`. Put it in `LocalState`/`MergeRemoteState` (TCP push/pull, 30s default **[MEASURED — `config.go:316`]**).
- At n=3, `IndirectChecks: 3` **[MEASURED — `config.go:312`]** has at most 2 candidates. SWIM's indirect probing is weaker here than the marketing implies; tune and expect it.

### 7. Two streams, not one: roster events and config/plugin events are separate.
**From:** Envoy's CDS/EDS split — *"when a cluster definition is updated... all existing connection pools will be drained and reconnected. EDS does not suffer from this limitation."*
**Why it wins:** gb-mbp is a laptop that *"sleeps, reboots"* (§1). If node liveness is carried in plugin/channel config, every lid close reconfigures every plugin on every box. Envoy already paid for this lesson in production. Membership churns constantly and must be cheap; structure changes rarely and may be expensive. This also serves §5's *"when a box is unavailable, other boxes must know... in case one of its agents wants to communicate with an agent on the dead box"* — liveness is a **routing input on a fast path**, not config.

### 8. Protobuf, `buf breaking` at **PACKAGE** level, served through ConnectRPC — plus a coarse handshake version.
**From:** protobuf's own rules, buf, ConnectRPC, gRPC reflection, go-plugin's `HandshakeConfig` (and go-plugin builds with buf itself).
**Why it wins:** it validates §3.2 completely, with two corrections worth real money:
- **Use `PACKAGE`, not buf's recommended `WIRE_JSON`.** **[MEASURED]** `WIRE_JSON` did **not** flag deleting an entire RPC method; `PACKAGE` did. For a plugin API the service surface *is* the contract.
- **"Maybe forwards?" — yes, free**, via proto3 unknown-field preservation, *but only in binary*. Never round-trip an envelope through JSON or you drop the fields you were trying to preserve.
- **"The same interfaces available through REST" — measured true.** One endpoint answered plain HTTP/1.1 JSON *and* gRPC-Web (§5.3). **Honest caveat: REST comes free; pubsub does not.** Connect gives request/reply and streaming; channel semantics are ours to define.
- **Turn on gRPC reflection.** **[MEASURED]** I listed, described, and called a service with zero local schema. This is how agents stop *"screwing around figuring out the network crap"* (§4) — they ask the daemon what it can do.
- **Belt and braces:** go-plugin's coarse `ProtocolVersion` handshake catches era mismatches at connect with a readable error, which protobuf field rules never will.

### Bonus decision — the plugin library (§3.2): sync the manifest, never the bytes.
**From:** Istio/Envoy ECDS (control plane ships a pointer + hint; a local agent fetches and verifies the bytes), Nomad `artifact` (`go-getter` + `checksum`, *"if the checksum is invalid, an error will be returned"*), Traefik as the counterexample (*"the archive hash is optionally checked"*).
**Verdict:** **do it, with one word changed.** As literally stated — *"the plugin gets synced across nodes"* — it's a trap: you'd be building a package manager, an integrity story, and a platform matrix (**darwin/arm64 on two boxes, linux/arm64 on the DGX** — a synced darwin binary is garbage on the dgx). Replicate the record `name+version+platform+sha256+url`; each daemon fetches its own bytes and verifies the hash before executing. Fetch failure is loud, local, and non-fatal — it reports "wanted X v3, not running" into the roster and never takes the box down (§4). The origin can just be another daemon serving the file over HTTP, which gives Greg *"add it to one node and it spreads"* with no central artifact server and no hub whose death is fatal. And per his instruction, this is **a plugin** — the kernel's only new job is exposing the manifest list as a replicated, subscribable resource, which it already must do for the roster.

---

## What this contradicts from the previous round

Not read (§9 poison rule), but the brief lists its conclusions, and prior art contradicts four of them **[DEDUCED]**:

- **Hub-and-spoke NATS on the DGX** — Envoy's xDS is the hub model, and it's the one thing from Envoy I'd reject; Consul/Serf, Erlang `global`, and memberlist all ship the not-centralized alternative as a *library*. §4 is satisfiable off the shelf.
- **"Cut the plugin system as over-engineering"** — Telegraf, Benthos, Fluent Bit, Vector, Caddy, Traefik, Envoy, osquery, Terraform, Vault, and Nomad are all this shape. It is the single most-copied daemon architecture in the industry.
- **"Request/reply has no use case"** — it's the *default* shape of every plugin API here. go-plugin, osquery, and ConnectRPC are all request/reply first.
- **"The machine roster was rejected"** — it's `memberlist.Create()` plus an `EventDelegate`. KIP-500 says bake it in, and Greg said bake it in.

---

## Open questions

1. **Does memberlist actually converge over Tailscale at n=3 with `UDPBufferSize≈1200`?** I measured the MTU cliff (1280) and read the default (1400), but I did not run three memberlist nodes across gb-mbp/dgx/gb-mac-mini. This is a half-day experiment and it de-risks decision #6. Also unmeasured: what a sleeping laptop does to the suspicion timers.
2. **What is the plugin subprocess overhead at our scale?** go-plugin costs one OS process per plugin per box. With ~6 plugins × 3 boxes that's fine on the DGX; unmeasured on the mac mini (**37GiB free** per §1). Should be measured before committing.
3. **How does a plugin get a durable name across restarts?** Erlang's `global` unregisters on death by design. Greg wants *"a daemon defined name"* as a stable key/domain name (§4) — so agent identity must outlive the process that registered it. `global`'s model alone doesn't give that; the name-vs-liveness split needs designing.
4. **What resolves a name clash?** `global` needs a resolver, and *"If the function crashes, or returns anything other than one of the pids, the name is unregistered"* — you can lose the name entirely. Two agents claiming one name needs a deterministic tiebreak. Undesigned.
5. **What does "channel type" mean in protobuf terms?** §3.1 says a channel has a name *and a type* (pubsub, point-to-point, request/reply, fanout). Whether these are distinct protobuf services, or one service with a type field, is undecided — and §5.3 showed the transport semantics don't fall out of the schema for free. This is the main open design question the transport-types investigation should answer.
6. **Traefik's "cannot start a plugin while running" sentence** — I could not re-verify the exact wording in the current JS-rendered docs; only the weaker *"you must change that instance's install (static) configuration."* The conclusion holds either way, but the quote is unconfirmed.
7. **ZeroMQ vs this shape** (§7.1) is not my task — but note that decisions #1 and #6 are transport-agnostic: go-plugin is local-only (daemon↔plugin), and memberlist brings its own gossip transport (daemon↔daemon roster). Whatever wins the ZeroMQ question governs a **third**, narrower link: daemon↔daemon *channel data*. That scoping should be checked against whatever that investigation concludes.

---

## Sources

**Source cloned and read (commit pinned):**
- `influxdata/telegraf` @ `7c40d48b559065672441cd877a9de3728ab8bee8` (2026-07-15) — `/tmp/priorart/tg/`: `plugin.go`, `input.go`, `output.go`, `metric.go`, `agent/agent.go`, `models/filter.go`, `plugins/inputs/registry.go`, `plugins/outputs/registry.go`, `plugins/processors/registry.go`, `plugins/common/shim/README.md`, `plugins/common/shim/goshim.go`, `plugins/inputs/execd/README.md`, `EXTERNAL_PLUGINS.md`
- `redpanda-data/benthos` @ `a9fc41bdd0e8a1894e3b1f0faa6c700752b87158` (2026-06-25) — `/tmp/priorart/bt/`: `public/service/input.go`, `public/service/output.go`, `public/service/environment.go`, `public/service/resources.go`, `internal/manager/type.go`, `internal/impl/pure/` (listing)
- `hashicorp/go-plugin` — `/tmp/priorart/gp/`: `README.md`, `server.go`, `buf.yaml`, `buf.gen.yaml`
- `hashicorp/memberlist` — `/tmp/priorart/ml/`: `README.md`, `delegate.go`, `event_delegate.go`, `config.go`, `net.go`, `memberlist.go`

**Experiments run (this machine, 2026-07-15):**
- `buf` 1.71.0 (`go install github.com/bufbuild/buf/cmd/buf@latest`); 8 breaking-change/lint experiments at `/tmp/bufexp` (add field; renumber; delete+reserve; type change; int32→int64 at WIRE_JSON and WIRE; delete RPC at WIRE_JSON and PACKAGE; service-name lint)
- `grpcurl` (`go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest`) — reflection `list`/`describe`/call against `demo.connectrpc.com:443`
- `curl` — Connect protocol over HTTP/1.1+JSON and gRPC-Web against `demo.connectrpc.com`
- Go `plugin`/`-buildmode=plugin` experiment at `/tmp/goplug` on `go1.26.2 darwin/arm64`: successful load, then shared-type version-skew failure; `go doc plugin`, `go doc plugin.Plugin`
- `ifconfig` (utun0 MTU) and `ping -c1 -t2 -D -s {1200,1252,1272,1300,1372}` path-MTU probes to `100.115.27.55`; `ping -c2` to `100.115.27.55` and `100.120.22.74`

**URLs fetched:**
- https://www.erlang.org/doc/system/design_principles.html
- https://www.erlang.org/doc/apps/kernel/global.html
- https://www.erlang.org/doc/apps/kernel/pg.html
- https://www.erlang.org/doc/system/release_handling.html
- https://protobuf.dev/programming-guides/proto3/
- https://connectrpc.com/docs/introduction/
- https://caddyserver.com/docs/extending-caddy
- https://github.com/traefik/yaegi
- https://doc.traefik.io/traefik/extend/extend-traefik/
- https://traefik-traefik.mintlify.app/plugins/overview
- https://plugins.traefik.io/create
- https://vector.dev/docs/architecture/pipeline-model/
- https://vector.dev/docs/reference/configuration/
- https://docs.fluentbit.io/manual/concepts/data-pipeline
- https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/operations/dynamic_configuration
- https://www.envoyproxy.io/docs/envoy/latest/configuration/other_features/wasm (thin; superseded by search)
- https://cwiki.apache.org/confluence/display/KAFKA/KIP-500%3A+Replace+ZooKeeper+with+a+Self-Managed+Metadata+Quorum
- https://osquery.readthedocs.io/en/stable/development/osquery-sdk/
- https://developer.hashicorp.com/consul/docs/architecture/gossip
- https://developer.hashicorp.com/nomad/docs/job-specification/artifact

**Web searches run (results used as [CLAIMED] where the page itself was JS-rendered or redirected):**
- "buf breaking change detection rule categories FILE PACKAGE WIRE WIRE_JSON docs" → buf.build/docs/breaking/rules/
- "Traefik plugins Yaegi WASM wasip1 documentation how plugins are loaded local plugins"
- "Envoy wasm remote_code http_uri sha256 remote data source required async fetch ECDS extension config discovery" → istio.io wasm-module-distribution + envoyproxy.io ECDS proto docs

**Project files read:**
- `/Users/gb/research/2026-07-15-agent-substrate-v2/BRIEF.md` (in full)
