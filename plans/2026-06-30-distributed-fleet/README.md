# Distributed fleet — idea cluster (2026-06-30)

**Origin:** operator session, 2026-06-30. A cluster of related-but-separable ideas, all pointing
at the same north star: **run crew (and tasks) across more than one machine** — for isolation,
for compute, and for access to more models. Each idea gets its own subfolder so we can sketch,
then go through them one at a time.

These are **design sketches**, not specs. Nothing here reopens a locked decision without saying
so explicitly. Where an idea overlaps work that already landed or was already designed, that's
called out so we build on it instead of re-deriving it.

---

## The four ideas

| # | Idea | Subfolder | One-line | Status |
|---|------|-----------|----------|--------|
| 1 | **Peer-to-peer comms / multi-node fleet** | [`01-p2p-comms/`](01-p2p-comms/README.md) | Nodes discover each other, track who's online + healthy (zookeeper-like), and a transport layer distributes the comms bus across them. Plus: easy SSH into a node's crew tmux. | **Sketched — primary, go first** |
| 2 | **Container / sandbox layer + build-cache** | [`02-container-sandbox/`](02-container-sandbox/README.md) | Run tasks (then crew) in isolated containers, framework-agnostic. Solve the heavy-compile-per-sandbox problem (Go/Rust/Haskell/OCaml) so a sandbox is ~as fast as the main box. | Scoping stub |
| 3 | **Crew on Pi (more model options)** | [`03-pi-crew/`](03-pi-crew/README.md) | Run a crew through the `pi` agent so crew can be driven by any OpenRouter/provider model, not just Claude. | Scoping stub — builds on the existing Pi/OpenRouter brief |
| 4 | **Auto-listen-to-comms on startup** | [`04-auto-comms-startup/`](04-auto-comms-startup/README.md) | Force an agent (Pi *and* Claude Code) to wire up its comms subscription at boot, so crew don't have to manually set up the connection. | Scoping stub |
| 5 | **Hermes harness study + our-own-harness question** | [`05-hermes-harness-study/`](05-hermes-harness-study/README.md) | Study Nous Research's Hermes distributed multi-agent layer (gateway process, named agents with isolated profiles, kanban dispatcher, agent templates) as prior art for how we run crew — and the meta-question of building/supporting a programmatic harness. | **Research done — decision pending** |

How they relate: **1 is the substrate** (where do nodes live, how do they find each other, how
does the bus reach them). **2** is the *isolation* dimension on top of that substrate. **3** is a
*new kind of crew* that runs on it. **4** removes boot friction that gets worse the more node/model
diversity we add (1 and 3 both make 4 more valuable).

---

## What already exists (don't re-derive)

The remote-execution lane has real, landed work and a real, written design. New sketches build on
it. The short version:

- **Remote-substrate execution path — LANDED + partly proven.** Central daemon ("box A") drives a
  remote macOS box over one-shot SSH; the remote runs **no persistent harmonik process** today. A
  per-run reverse-SSH tunnel (TCP loopback → daemon socket, locked `hk-ege6`) carries hook events;
  results come back as a run branch box A fetches directly over SSH. Telemetry (Phase 1) and live
  breach alerts (Phase 2) landed 2026-06-20. See `plans/2026-06-20-remote-node-telemetry-autoscale/`
  and `docs/remote-substrate/`.
- **A resident node-agent was already designed but deferred (Phase 3).** The prior-art doc
  (`.../04-prior-art-and-naming.md`) maps harmonik onto k8s / Nomad / CI-runners / SWIM and lands
  on **hybrid, center-authoritative: "push placement, pull pickup."** It recommends a persistent
  worker agent that dials home, heartbeats, advertises capability, and pulls its assigned work —
  while the **center stays the sole scheduler**. Liveness is **graded (alive → suspect → dead)**
  borrowed from SWIM, **not** a gossip mesh. Naming recommendation: central = **Harbor**, box =
  **port**, resident agent = **harbormaster** (plain `hub` / `node-agent` in code).
- **Two transports, by design.** Slow/coarse capacity reporting (central-pull, on a timer) is a
  *different channel* from live during-a-run breach alerts (event-driven, over the open tunnel).
  Idea 1 adds a *third* concern — the **comms bus itself** crossing node boundaries — which is
  genuinely not yet designed.

**The one real tension to decide (see idea 1):** the operator's framing this session leans
*peer-to-peer* ("a way to know who's online... like zookeeper"); the prior design deliberately
chose *hub-and-spoke* and rejected gossip at this scale. Idea 1 reconciles these by splitting
**presence/membership** (can be peer-ish, every node holds the roster) from **scheduling** (stays
central) from **transport** (the new part). That split is the whole point of going first.

---

## Sequencing

1. **Idea 1 (P2P comms / multi-node)** — go first; it's the substrate the others assume.
2. Then **idea 4 (auto-comms-on-startup)** — small, and it unblocks clean crew boot on any node/model.
3. Then **idea 3 (Pi crew)** and **idea 2 (containers)** in either order — independent of each other.
