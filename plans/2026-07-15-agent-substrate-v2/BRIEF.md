# BRIEF — Agent Substrate

**Date:** 2026-07-15
**Status:** This is the authoritative problem statement. 
**Author:** honed directly with Greg over several rounds. Where quoted, the words are his.

---

## 1. The situation

Three machines, all Greg's, all on one trusted Tailscale network:

| Box | Role | Uptime character |
|---|---|---|
| `gb-mbp` (100.87.151.114) | laptop, human's driver | sleeps, reboots |
| `dgx` (100.115.27.55) | Linux, GB10/DGX Spark, 3.2TB free, vLLM | always-on |
| `gb-mac-mini` (100.120.22.74) | macOS, 37GiB free | always-on |

> *"I've got right now 3 machines with a whole bunch of shit happening on them."*

Lots of agent work runs on all of them — Claude Code, Codex, Pi Agent, OpenCode.

## 2. The problem, in his words

> - *"something figured out on one machine cannot be used on another machine."*
> - *"something an agent figures out cant be sent to another agent on another machine."*
> - *"if I did something yesterday one one project on one machine, how can I find that thing from another machine in another agent?"*
> - *"how could all learning of all agents be centralized and searchable?"*
> - *"I bring up zookeeper - not for high availability - but to sync who is on/off line and as a 'shared' address book - so an agent doesn't stumble over the wrong machine name/ip/username/inability to connect via ssh."*

**The through-line: the fleet has no shared memory and no shared address book.** Everything an agent learns dies on the box it learned it on. An agent that wants to reach another box has to rediscover how.

**On agent comms specifically** — this is a first-class need, not a footnote:

> *"I've also started plenty of agents and then converge on similar things I'm working on and I need them to send messages back and forth... I can have one agent comm with another and figure something out - but they can still have different directives."*

Agents converge on similar work and need to talk it out **themselves**. They keep different directives. The system's job is to **carry the message**. It never judges, never detects overlap, never warns.

## 3. The vision — light kernel, heavy plugins

> *"what if the daemons provided a robust data transport and connection layer - and then we'd layer maybe plugins on top of that which could do things like log tail, comms, log archiving, etc"*

> *"maybe the core (or kernal?) is actually really light, and its the plugins that do a lot of the work."*

A daemon on each box. Kernel + plugins.

### 3.1 KERNEL — what's baked in

**Channels — the key architectural realization.** In his words:

> *"I was thinking a plugin could connect into the comms system... but I dont love that idea. Then the comms system is hard coded into the daemons internals. What if the daemon had 'channels', a channel had a name and a type (pubsub, etc), then the daemon would do data transport, while the plugin handled all the logic."*

So: **a channel has a name and a type. The daemon moves bytes. The plugin owns all logic.** Comms is NOT in the kernel — comms is a *plugin* that uses channels.

**A menu of transport types.** His brainstorm:

> *"the daemon could provide say a handful of transports (publish/lookup table, point to point, pubsub, fanout, whatever) - then the plugins could choose which one was used."*

Request/reply is explicitly wanted: *"the internet is based on that - probably a good idea, lol. Would be good to come up with a few transport types to start with."* Note his own examples ("a search tool on the network", "a publish note tool") are request/reply shaped.

**Machine roster — baked in.**

> *"The machine roster probably should be baked in. Thats why I used zookeeper as an example. Its kinda only there to keep track of whos around. Then on top of that, for example is Kafka which does other stuff. But theres that one 'thing' that keeps track of 'whos where'. I dont want to get hung up on CAP theorem and shit."*

His model: *"machines have a list of other machines. Then send pings to each other to check they're online. then they can also sync agent lists and maybe a summary of what the agent is doing."*

**Storage.** *"One thing that might be useful is to have a storage mechanism in the daemon. Then the plugins dont make up their own thing."*

**Plugin registration.** *"There would be a registration process with the daemon, and the plugin would say what resources it was interested in/needed to react to. Maybe it needed changes in the node list."*

### 3.2 PLUGINS — where the work happens

**Interface in protobuf**, and the reason matters:

> *"A plugin has a very well defined interface - probably protobuf - must be versioned and probably backwards (maybe forwards?) compatible"*
> *"I say protobuf because we can the define it COMPLETELY independently of any code. Also could be cool because the same interfaces could be available through REST/pubsub/whatever"*

**Live-loadable.**

> *"In harmonik having to stop the whole system every time is so annoying. Wish we had Erlangs Beam, lol. I don't know much about the options (and I'd kinda like to stick with go) - so I'm open to discussions - would be nice to understand whats available for live reload."*

**Plugin candidates:** comms, agent list/registry (*"The idea of an agent list could probably also be a plugin"*), log tail, log archiving, SSH connection helper, notes, search.

**[Idea] The first plugin is a plugin library:** *"A plugin gets added to one node, the plugin gets synced across nodes."*

## 4. Hard constraints and stated preferences

- **Go.** *"I'd kinda like to stick with go"*.
- **Trusted network. 3 boxes, all his.** Non-owned machines explicitly out of scope for now: *"Lets keep that out of it for now and assume a trusted network."*
- **NOT-CENTRALIZED — and this is load-bearing:**
  > *"Part of the problem is that I dont trust my boxes. So I strongly lean away from NATS. I think with ZeroMQ we can create more of a distributed system (I say that only meaning that it spreads across several nodes - without any other implications - call it something else if you want - not-centralized)"*

  Read this precisely: **no single box's death may kill the system.** It is NOT a claim about Byzantine faults, and NOT a request for consensus or HA rigor. He wants no hub whose loss is fatal.
- **Durability is a PLUGIN decision, not a kernel guarantee:** *"How about: the plugin defines that! Then we can have multiple options."*
- **Search is later, and it is a consumer:** *"We handle search later - we can solve search once we have a data backbone."* And: *"the logs will go on the transport, get dumped, then something will expose them for search (probably in multiple ways)."* Metadata — *"the machine, time, etc"* — must ride along because search will need it.
- **Agent identity:** a **daemon-defined name** is the key/domain name; box name, IP, and other attributes hang off that entry.
- **SSH key sync is a plugin, not core.** But the goal is that the fleet **programmatically verifies every box can connect**, so *"agents dont have to figure that out"* — no agent should ever *"screw around figuring out the network crap."*

## 5. Message semantics (today, and the known gap)

Today's harmonik model, which he considers a reasonable starting point:

> *"all the messages are written down. If an agent is subscribed, then they get a notification. If they are polling, then they read their messages the next time they come in. When we're sending cross machine - I'd suggest it might be similar."*

The known flaw, in his words:

> *"the current model is a little flawed - we actaully should probably notify the sender that the receiver is not listening. I believe we can find that out. So we could do some type of ACK system. This is open for discussion and further refinement down the road."*

Also: when a box is unavailable, **other boxes must know**, *"in case one of its agents wants to communicate with an agent on the dead box."* Liveness is a routing input, not a status display.

## 6. Explicitly NOT the problem

- **Overlap/conflict detection.** He was clear: *"That is not my instinct... thats BARELY a point here - I really dont even care about that."* Agents that converge just need to talk. Carry messages; never judge.
- **RAG / vector search / embeddings.** Later, and downstream of the backbone.
- **High availability, consensus, CAP rigor.** *"I dont want to get hung up on CAP theorem and shit."*
- **Scale beyond ~3 boxes and tens of agents.** One human operator.
- **Non-owned / untrusted machines.** Out of scope for now.

## 7. The open questions he wants answered

1. **ZeroMQ — does it make sense?** Asked twice, never answered. He leans toward it and away from NATS, for the not-centralized reason in §4. He wants this explored honestly, not rubber-stamped. *"we want to explore these things."*
2. **Live reload in Go — what's actually available?** BEAM envy is explicit. He wants to understand the option space.
3. **What transport types should the kernel start with?**
4. **Is anything in the previous research recoverable?**

## 8. The shape of a good answer

- Decisive. Pick, and say why the alternatives lost. He wants execution, not menus.
- Honest about what's unknown vs measured. Verify claims against real files and real experiments.
- Plain language. Define every term and acronym on first use. No invented product names used before they're introduced.
- Grounded in *this* brief. Do not import motivating anecdotes from elsewhere.

## 9. Why the previous research failed (read this — it is the main lesson)

The prior round (`~/research/2026-07-15-agent-comms-substrate/`) was given a brief that took two passing anecdotes from Greg's first message — two agents overlapping once, and a misplaced coding-principles doc — and elevated them to **the** motivating problems. It then explicitly asked the reviewer whether a shell script could solve those two anecdotes. It said yes, and the whole corpus bent toward "don't build this."

The agents did competent work on a false premise. Specific damage:

- **Overlap detection** — a thing Greg does not care about — shaped a large fraction of the design.
- **A hub-and-spoke NATS topology on the DGX** was chosen. This **directly violates §4's not-centralized requirement**, which the old brief never stated.
- **The plugin system was cut entirely** as over-engineering. Greg wants it, wants it live-loadable, and thinks the plugins may be where most of the value is.
- **Request/reply was cut** as having no use case. Greg wants it.
- **The machine roster was rejected** ("zookeeper-like is a shape, not a request"). Greg wants it baked into core.
- **Search was dropped** through a category error, then partially re-added.

**The mechanism findings appear sound and are worth salvaging; every conclusion that rests on the old framing is void.** Assume nothing from that folder without re-deriving it against this brief.
