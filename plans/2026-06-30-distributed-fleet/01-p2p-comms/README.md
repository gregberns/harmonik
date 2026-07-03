# Idea 1 — Peer-to-peer comms / multi-node fleet

**Sketch, not a spec.** Date 2026-06-30. Primary idea of the cluster; the others assume this
substrate exists.

---

## The vision (operator, distilled)

> "Connect harmonik via a peer-to-peer comms network. A lead machine SSHs into another, pulls a
> harmonik build, starts harmonik with details about the origin machine. The machines exchange node
> names, then know about each other and have some system to keep updated. On top of that there's a
> transport layer — the comms logs get distributed across the nodes. Crew running on a box hook into
> the harmonik daemon for comms. It would also be nice to SSH into those boxes easily and get access
> to the crew running in the tmux sessions."
>
> "Seems like this P2P would be kinda like zookeeper — a way to know who's online, if they're
> healthy, etc. Then the actual transport of whatever would be handled separately."

The operator's own framing already names the key decomposition: **a membership/presence layer
("who's online, healthy") is one thing; the transport of payloads is a separate thing.** This
sketch takes that split as load-bearing.

---

## Three concerns, kept separate

The single most important move is to **not build one blob**. There are three concerns, each with a
different freshness/consistency need, and conflating them is how distributed systems rot:

| Concern | "Zookeeper-like?" | Freshness | Who's authoritative |
|---|---|---|---|
| **A. Membership + presence + health** — the roster: who is in the fleet, are they up, are they healthy, what can they run | **Yes** — this is the zookeeper-ish part | Seconds (heartbeat) | Shared / replicated; every node can read it |
| **B. Scheduling** — which bead/crew goes to which node | **No** | On-dispatch | **Central** — the daemon owns the queue (locked decision) |
| **C. Transport** — moving payloads between nodes: the **comms bus**, run branches, hook events | **No** | Per-message | The bus's own delivery guarantees (N3 at-least-once) |

A maps to the operator's "zookeeper / who's online." B is **already decided and must not be
dissolved** (see §"The tension"). C — distributing the comms bus across nodes — is the genuinely
**new, undesigned** piece, and it's what makes crew-on-any-node actually work.

---

## What's genuinely new here (vs. what's already designed)

The prior-art doc already designed the **resident node-agent + graded liveness + capability
fingerprinting** (deferred Phase 3). This sketch does **not** redo that. The new, undesigned deltas
the operator is asking for are:

1. **Dynamic join / bootstrap flow.** Today the fleet is a *static* `workers.yaml` hand-edited on
   box A. The operator wants a node to **join at runtime**: lead SSHs in, ships/pulls a build,
   starts harmonik with the origin's coordinates, and the two **exchange names** and register each
   other. That turns `workers.yaml` from a hand-maintained file into the **output of a join
   protocol**.
2. **A presence roster every node can read (concern A as a first-class surface).** Phase-3
   telemetry was framed as *worker → center reporting*. The operator wants something more
   peer-ish: a queryable roster of the whole fleet — `harmonik fleet` shows every node, its
   health, its crew. The membership state is *replicated to nodes*, not just hoarded centrally.
3. **Comms-bus distribution across nodes (concern C — the new transport).** Today the comms bus is
   single-daemon-local: `harmonik comms` reads/writes one node's event store. For crew on box B to
   talk to the captain on box A, **the bus has to span nodes.** This is not designed anywhere.
4. **Easy SSH-into-crew ergonomics.** `harmonik attach <node>/<crew>` (or similar) to drop straight
   into a remote crew's tmux pane, instead of hand-rolling `ssh box -t tmux attach -t <session>`.

Everything else (the heartbeat split, suspect→dead grace window, capability fingerprinting, the
reverse-tunnel hook transport, harbor/port/harbormaster naming) is **borrowed wholesale** from the
prior design — this sketch sits on top of it.

---

## The tension to decide: peer-to-peer vs. hub-and-spoke

This is the one real decision and it should be made explicitly, because the operator's words this
session ("peer-to-peer", "zookeeper") point one way and a **locked prior decision** points the
other.

- **Locked (prior-art doc §2):** *"Don't go pure-pull — it dissolves the central queue."* Harmonik's
  identity is **daemon-owns-the-queue**: one central scheduler holds beads/JSONL/git authority. The
  prior design deliberately **rejected a gossip mesh** at this scale ("take the *state machine*, not
  the *protocol*") and chose hub-and-spoke with graded liveness.
- **This session's framing:** more peer-to-peer — nodes know about *each other*, keep *each other*
  updated, the comms log is *distributed across* nodes.

**These are reconcilable, and the reconciliation is the §"Three concerns" table.** The synthesis:

> **Peer-to-peer for membership + comms transport (concerns A & C). Central for scheduling
> (concern B).**

That is: nodes *can* gossip presence and *can* relay the comms bus peer-to-peer (so a crew on box
B reaches the captain on box A even if they're not co-located), **but the daemon still decides what
work goes where.** This keeps everything the locked decision protects (single queue, git/bead
authority) while giving the operator the peer-ish *visibility and comms* they're describing. It
also matches the existing transport split: presence/comms are the "many nodes talk" plane;
scheduling is the "one brain decides" plane.

**Recommendation:** adopt the synthesis above. If the operator genuinely wants peer scheduling too
(multiple nodes pulling from a shared queue with no central brain), that's a much bigger reopen of a
locked decision and should be its own explicit decision — flag it, don't assume it.

---

## Sketch of the mechanism

### A. Membership + presence (the zookeeper-like layer)

- **A node record:** `{ name, addr (tailnet), os, arch, toolchains[], max_slots, state, last_heartbeat }`.
  This is `workers.yaml`'s entry promoted to a live object with a *status* half it writes itself —
  exactly the k8s Node-object split the prior-art doc described.
- **Heartbeat / lease split (borrowed):** a tiny frequent liveness beat, separate from the rarer
  fuller capability+load report. Graded liveness: **alive → suspect → dead**, with the
  suspect-window grace exceeding a realistic commit budget (the double-dispatch trap).
- **Where the roster lives — the real design choice:**
  - *Option A1 — central-authoritative, replicated to nodes (recommended first cut).* The hub holds
    the canonical roster; nodes get a read-replica pushed/pulled over their existing dial-home
    connection. Simplest; consistent with "hub-and-spoke + central authority"; no consensus
    protocol. Every node *can read* the whole fleet (satisfies the operator's "nodes know about each
    other") without every node *writing* it.
  - *Option A2 — gossip mesh (SWIM/Serf).* True peer-to-peer membership. The prior-art doc
    **explicitly rejected this at current scale** as over-engineering. Revisit only if node count
    grows past ~dozens or the hub-as-single-point becomes the real bottleneck.
  - **Pick A1 now; A2 is a later evolution, not v1.**

### B. The join / bootstrap flow (new)

The operator's concrete sequence, made into a protocol:

```
lead (box A) ──ssh──> box B
  1. ship or pull the harmonik build onto box B   (scp the binary, or `go install`, or pull a release)
  2. start harmonik on box B with the origin's coordinates:
        harmonik node join --hub tcp://<boxA-tailnet>:<port> --name <auto-or-given>
  3. box B dials home to box A, registers its node record (fingerprint: os/arch/toolchains/slots)
  4. box A records box B in the roster, returns box A's identity + the current roster snapshot
  5. both sides now hold each other's names → `workers.yaml` becomes the persisted output of this
```

Open questions for the join flow (§decisions): name assignment (operator-given vs. auto from
hostname/tailnet?), auth/trust (what stops a rogue node joining — tailnet ACL is probably enough
given the locked "transport over Tailscale" decision), and idempotency (re-join after restart must
reconcile, not duplicate — reuse the existing zombie/presence-stale reconciliation muscle).

### C. Comms-bus distribution across nodes (the new transport — the crux)

Today: `harmonik comms` is one daemon's local event store; `recv --follow` tails it; dedupe on
`event_id` (N3 at-least-once). For multi-node, a crew on box B must `comms send --to captain` and
have it reach box A, and `comms recv` on box B must see events originated on box A.

Design options, lightest-first:

- **C1 — Hub-relayed bus (recommended first cut).** The hub's comms store stays canonical. Each
  node's daemon **forwards local sends to the hub and subscribes to the hub's stream** over the
  dial-home connection, mirroring relevant events into its local store so local `comms recv` works
  offline-ish. This is a star topology on the comms plane — *not* peer-to-peer in the strict sense,
  but it gives the operator's outcome ("comms logs distributed across nodes") with the least new
  machinery, and it reuses the already-built event bus + `event_id` dedupe end-to-end. The reverse
  tunnel already proves the transport substrate.
- **C2 — Peer-to-peer bus replication.** Each node's bus gossips events to peers; eventually every
  node holds the full log. Truly matches "distributed across the nodes" and survives hub outage, but
  needs conflict-free merge (the `event_id` dedupe + an ordering scheme — likely a per-node
  lamport/seq, since wall-clock ordering is already a known soft spot). More machinery; defer unless
  hub-as-relay becomes the bottleneck.
- **The N3 at-least-once + `event_id`-dedupe contract is the thing that makes either survivable** —
  it's *already* the normative bus guarantee, so distributing the bus is "deliver the same events to
  more stores," not "invent new semantics." That's the reason this is tractable.

**Pick C1 now.** It satisfies the operator's stated outcome and is a straight extension of the
existing tunnel + bus. C2 is the "real P2P" evolution if/when hub-relay hurts.

### D. Easy SSH-into-crew ergonomics (new, small, high-value)

- `harmonik attach <node>` → resolves the node's tailnet addr from the roster, opens
  `ssh -t <addr> tmux attach -t <fleet-session>`.
- `harmonik attach <node>/<crew>` → attaches directly to that crew's window (we already nest
  agent+keeper windows per the tmux-session-organization epic, so the window name is derivable).
- `harmonik fleet` → the roster as a table: node, state (alive/suspect/dead), crew running, load.
- This is mostly a thin CLI over the roster (A) + the known tmux naming convention — cheap, and it's
  the day-to-day ergonomic the operator explicitly asked for.

---

## How it maps onto what's already built

| This sketch | Reuses / extends |
|---|---|
| Node record + heartbeat/lease split + graded liveness | Prior-art doc §1 (k8s Lease, SWIM grading) + Phase-1/2 telemetry already in `internal/workers/` |
| Dial-home connection carrying presence + comms | The locked reverse-SSH-tunnel → TCP-loopback transport (`hk-ege6`); Tailscale + `accept`-mode ACL |
| Comms-bus relay (C1) | The existing `harmonik comms` bus + N3 at-least-once + `event_id` dedupe (agent-comms skill) |
| Dynamic join replacing static `workers.yaml` | `workers.yaml` schema + the single-worker registry cap that **must** be lifted (`workers.NewRegistry` holds one worker — known gap, REMOTE-NEXT.md) |
| `harmonik attach` / `harmonik fleet` | The tmux-session-organization naming convention (unified `harmonik-<hash>-*` namespace) |
| Resident node-agent ("harbormaster") | **Is** Phase 3 of the remote-node plan — this sketch is largely the operator green-lighting that deferred phase, plus concerns A-roster and C-comms it didn't cover |

**Net:** roughly 60% of this is "do the deferred Phase 3 + lift the single-worker cap," and ~40% is
genuinely new design (the join protocol as a runtime flow, the roster-as-shared-surface, and
comms-bus distribution).

---

## Phasing (proposed)

- **P0 — Lift the single-worker cap + dynamic join.** `workers.NewRegistry` → N workers keyed by
  name; `harmonik node join` writes a roster entry at runtime. Prerequisite for everything; it's
  already flagged as *the* load-bearing gap (no bead filed — file `codename:multi-remote`).
- **P1 — Resident node-agent (harbormaster) + presence roster (A1).** Stand up Phase 3's deferred
  agent: dial-home, heartbeat/lease, graded liveness, capability fingerprint. Roster replicated to
  nodes. `harmonik fleet` + `harmonik attach` land here (cheap once the roster exists).
- **P2 — Comms-bus relay (C1).** Node daemons forward sends to the hub and mirror the hub stream
  locally. Crew on any node can `comms send/recv` against the whole fleet. This is what unblocks
  crew-on-other-machines for real.
- **P3 (later / maybe-never) — peer gossip (A2) + peer bus replication (C2).** Only if hub-as-relay
  or hub-as-roster-authority becomes the bottleneck. Explicitly out of v1.

**Gate:** P0–P2 all assume the **live remote e2e is finally green** on the real box (still owed —
`hk-nepva`, gb-mbp `enabled:false`). Don't build the multi-node roster on a single-node path that's
never run end-to-end live. That's the same precondition the Phase-3 pickup checklist already names.

---

## Decisions for the operator

1. **Synthesis or pure-P2P?** Adopt "peer-ish membership + comms, central scheduling" (recommended),
   or do you actually want peer scheduling (reopens the locked daemon-owns-the-queue decision)?
2. **Roster authority:** central-replicated-to-nodes (A1, recommended) vs. gossip mesh (A2, deferred)?
3. **Comms distribution:** hub-relay (C1, recommended) vs. peer replication (C2, deferred)?
4. **Join trust model:** is tailnet membership + `accept`-mode ACL sufficient auth for a node to
   join, or do we want an explicit join token?
5. **Name assignment:** operator-given node names, or auto-derive from hostname/tailnet?
6. **Naming the roles in CLI/prose:** adopt the prior-art recommendation (Harbor / port /
   harbormaster), or keep plain (hub / node / node-agent)? (Cosmetic; can defer.)

Each of these is a *runtime/architecture* call, not a code detail — the recommended path is named
on every one so we can proceed without stalling, and you redirect any you'd choose differently.
