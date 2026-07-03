# Distributed-fleet — planning summary (pre-consensus read)

**Written:** 2026-07-03. **Purpose:** synthesize the whole `plans/2026-06-30-distributed-fleet/`
cluster into (a) what it is, (b) an inventory, (c) how it relates to already-built work, (d) what's
buildable now vs. what needs a decision, and (e) a proposed sequence. This is the *pre-plan read*
so the admiral can run a consensus pass on the open decisions BEFORE any kerf work or beads exist.
No beads, no kerf work, no code created here.

---

## 1. What the initiative is

**One goal, three axes.** Distributed-fleet is the operator's push to run **crew (and tasks) across
more than one machine, in their own isolated environments, driven by more than one model harness.**
It is the "isolated crew / distributed compute" north star: give an agent its *own full environment*
— own filesystem, own git branch/worktree, own queue to dispatch into, still tmux-attachable — so it
can own sustained multi-round work (planning, heavy epics with subagent fan-out) without ever being
able to write to the primary repo / main.

The end state: a captain on box A can stand up a crew on box B (or in a container, or driven by Pi/
Codex instead of Claude); every node knows who else is online and healthy; the comms bus reaches
across node boundaries so any crew can talk to the captain; and the operator can `harmonik attach`
straight into any remote crew's tmux. The daemon on the hub stays the single scheduler — the fleet
grows peer-ish for *presence and comms*, but *scheduling stays central* (a locked decision).

**The connective tissue (00-the-case):** the trigger to give a crew its own isolated env is when
work is "more than a single queue item" — planning (needs research + agent↔operator rounds) or a
complicated epic (needs subagent fan-out). Both share the shape "an agent with a queue OR delegation
power that MUST run isolated." Isolation is also what makes a non-Claude harness (Codex, Pi) *safe*
to run — the concrete pain that motivated this was **Codex writing to the main repo when it should
have started in its own worktree.**

---

## 2. Inventory

| File | Purpose | Kind |
|---|---|---|
| `README.md` | Cluster index: the 5 ideas, how they relate, what already exists, sequencing (idea 1 first). | Index / decided framing |
| `00-the-case-for-isolated-crew.md` | The *why* — operator's argument that crew-level isolation (own fs/branch/queue, tmux-attachable) is the shared objective tying 1/2/5 together. Points at `plans/2026-07-02-pi-sandbox/` as the first buildable slice. | Design rationale — decided direction |
| `01-p2p-comms/README.md` | **Primary idea.** Multi-node fleet: 3 separated concerns (A membership/presence, B scheduling, C comms transport), dynamic join protocol, roster, comms-bus distribution, `attach`/`fleet` ergonomics. Full phasing P0–P3 + 6 explicit operator decisions. | Design sketch + **open decisions** |
| `02-container-sandbox/README.md` | Pluggable, framework-agnostic isolation layer + the warm-build-cache problem (Go/Rust/Haskell/OCaml compile cost). Backend candidates (Daytona/Singularity/Modal), per-language cache matrix. | **Scoping stub** — problem framed, not solved |
| `03-pi-crew/README.md` | Run a *crew* (not just a per-bead task) through the Pi harness for model diversity. Needs a control-flow shim emulating Claude's crew-launch flags. | **Scoping stub** — builds on Pi/OpenRouter brief |
| `04-auto-comms-startup/README.md` | Make comms-subscribe a *forced boot property* of the agent process (harness-enforced), not a prompt-driven step the model can skip/lose on `/clear`. Pi + Claude Code surfaces. | **Scoping stub** — small, high-leverage |
| `05-hermes-harness-study/README.md` | Prior-art study of Nous Research's Hermes (gateway / forked-home-dir profiles / SQLite kanban / delegate_task). Frames the meta-fork: build our own programmatic harness vs. support existing ones; recommends **"harness = pluggable seam (bead+context in, structured result out)."** | **Research complete — decision pending** |
| `PLANNING-SUMMARY.md` | This file. | Synthesis |

---

## 3. Relationship to current work

Distributed-fleet is **the next layer up** from the per-bead isolation slices already in flight. The
stack, bottom to top:

- **Remote-substrate (LANDED + partly proven).** Box A drives a remote macOS box (gb-mbp) over
  one-shot SSH; no persistent process on the remote. Per-run reverse-SSH tunnel carries hook events;
  results come back as a run branch fetched over SSH. Telemetry (Phase 1) + live breach alerts
  (Phase 2) landed 2026-06-20. This is *where work can run off-box today* — but statically, one
  worker, `workers.yaml` hand-edited, and the **live e2e is still owed** (`hk-nepva`, gb-mbp
  `enabled:false`). Distributed-fleet's idea 1 turns that static single worker into a **dynamic
  multi-node roster** (lift the `workers.NewRegistry` single-worker cap; join protocol replaces the
  hand-edited file).

- **Pi-sandbox (`plans/2026-07-02-pi-sandbox/`, operator-flagged HIGH, in scoping).** The daemon
  dispatches a bead → spawns **Pi inside a sandbox** (own fs view, own worktree, own subprocess
  tree) → result branch merged → sandbox torn down. This is explicitly named as **the concrete first
  buildable slice** of distributed-fleet's isolated-crew case — smallest thing that exercises the
  whole sandbox seam, using per-bead jobs (not full crew yet). It is idea 2 (container/sandbox) ∩
  idea 3 (Pi) ∩ idea 5 (harness seam) reduced to one shippable increment.

- **Pi harness (`plans/2026-06-23-pi-openrouter-harness/`).** Two scopes: Pi as per-bead implementer
  (substrate mostly built — `AgentTypePi` declared but unimplemented, codex is the template) and Pi
  as crew runner (bigger — crew launch hard-codes `claude`). Distributed-fleet's **idea 3 is scope 2**
  (Pi as a *crew* runner), which sits on top of the per-bead Pi work.

**So the layering is concrete:** per-bead sandbox (pi-sandbox, HIGH, now) → isolated *crew*
(distributed-fleet 00 + idea 3) → *multi-node* scheduling of those crews (idea 1) → cross-node
*comms* so they coordinate (idea 1 concern C) → *any harness* in any of those slots (idea 5 seam).
Idea 4 (forced comms-subscribe) is the small enabler that removes boot friction as node/model
diversity grows.

**~60% of idea 1 is "finish already-designed work":** the resident node-agent + graded liveness +
capability fingerprinting was *already designed* as deferred Phase 3 of the remote-node plan
(prior-art doc, recommends hub-and-spoke, "push placement, pull pickup", Harbor/port/harbormaster
naming). Idea 1 is largely the operator green-lighting that phase, plus ~40% genuinely new (runtime
join protocol, roster-as-shared-surface, comms-bus distribution).

---

## 4. Buildable now vs. needs a decision

### (a) Clearly-scoped buildable chunks

1. **Pi-per-bead sandbox** (`plans/2026-07-02-pi-sandbox/`) — already scoped HIGH; the natural first
   build. Reuses the L3 `DockerExecRunner` shape + the `Runner` abstraction. Independent of idea 1's
   decisions.
2. **Lift the single-worker registry cap** (`workers.NewRegistry` → N workers keyed by name). Named
   as *the* load-bearing gap (idea 1 P0, REMOTE-NEXT.md). Prerequisite for everything multi-node;
   mechanically scoped.
3. **`harmonik attach <node>[/<crew>]` + `harmonik fleet`** — thin CLI over a roster + the existing
   tmux-session-organization naming convention. Cheap once a roster exists; pure ergonomics the
   operator explicitly asked for.
4. **Forced comms-subscribe on Pi boot (idea 4, Pi surface)** — greenfield; the Pi launcher/shim can
   `comms join` + spawn `recv --follow` as a sidecar the launcher owns, before the model gets
   control. No decision blocked (Claude-Code surface via SessionStart hook is the harder half).
5. **Pluggable-isolation `Runner`-style interface (idea 2 problem 1)** — generalize
   `DockerExecRunner` into a config-driven backend seam. The *interface* is buildable; the *warm-cache*
   half (problem 2) needs the per-language decision below.
6. **Deferred Phase-3 resident node-agent (harbormaster)** — dial-home + heartbeat/lease + graded
   liveness + capability fingerprint. Already designed; buildable once P0 lands and the roster-authority
   decision is made (A1 is the recommended default, so this is low-risk to proceed on).

### (b) Genuine open decisions (for 3-agent consensus)

**D1 — Peer-to-peer vs. hub-and-spoke (the load-bearing one).**
- *Option A (recommended in sketch):* synthesis — **peer-ish membership + comms transport, central
  scheduling.** Keeps the locked daemon-owns-the-queue decision; nodes gossip presence & relay comms
  but the hub still decides what runs where.
- *Option B:* true peer scheduling (multiple nodes pull from a shared queue, no central brain).
  **Reopens a locked decision**; much bigger blast radius (git/bead authority, double-dispatch).
- *Trade-off:* A gives the operator's stated visibility+comms outcome with the least new machinery and
  no locked-decision reopen; B is only worth it if the hub becomes a real bottleneck (not evidenced yet).

**D2 — Comms-bus distribution: hub-relay vs. peer replication.**
- *C1 hub-relayed (recommended):* nodes forward local sends to the hub + subscribe to the hub stream,
  mirror locally. Star topology; reuses the existing bus + `event_id` dedupe + reverse tunnel end-to-end.
- *C2 peer replication:* nodes gossip events to peers; every node holds the full log; survives hub
  outage but needs conflict-free merge + an ordering scheme (lamport/seq — wall-clock is a known soft spot).
- *Trade-off:* C1 is a straight extension of what exists and satisfies the stated outcome; C2 is the
  "real P2P" evolution, defer unless hub-relay hurts.

**D3 — Warm-build-cache strategy per language (idea 2 problem 2 — the hard part).**
- *Options:* shared/bind-mounted cache volumes (fast, but the cache-reaper TOCTOU lesson = shared
  mutable cache is a footgun; needs per-lang locking/CoW) · warm base images · content-addressed remote
  cache (sccache/`GOCACHEPROG`) · CoW snapshots (APFS/zfs/OrbStack clones — closest to "as fast as main",
  ties backend to a fs feature) · warm-affinity routing (route to a node whose cache is hot — reuses idea 1).
- *Trade-off:* likely **per-language** (Go's cache benign to share, Rust's `target/` not, Haskell/OCaml
  have switch/store semantics). This is a matrix decision, not one pick — consensus should pick the
  *default* strategy + which languages get special-cased first.

**D4 — Sandbox backend / harness heaviness (idea 2 + idea 5 fork).**
- *Option A (support a driven harness):* drive Hermes/Pi/Codex process-level or over ACP the same way
  we drive `claude`. Fits daemon-spawns-processes; Hermes is MIT + already speaks ACP.
- *Option B (build our own programmatic, no-TTY harness):* bead+context on stdin → structured result,
  no pane/tmux → far easier to sandbox + run remote; sidesteps the interactive-launch fragility
  (pane-wake, unsubmitted-prompt wedge, `/clear` session-id flips).
- *Synthesis (sketch's recommendation):* make **"harness" a pluggable seam (bead+context in, structured
  result out, transport-agnostic)**; evaluate Hermes-over-ACP as the *first* non-Claude/codex proof.
- *Trade-off:* the interactive-tmux model is a **locked design preference (Gas-Town inspectability)** —
  B must not lose attachability. Decision: is tmux-interactive load-bearing for *all* harnesses, or an
  artifact we make optional per-harness?
- *Sub-decision (backend heaviness):* operator steer = **process isolation, not full micro-VM / hosted
  sandbox-as-a-service** → biases away from Daytona/Modal (hosted, framework lock-in) toward
  Singularity/Apptainer/OrbStack/plain-namespace. Confirm the lean.

**D5 — Join trust + node naming (lower stakes, bundle-able).**
- Trust: is tailnet membership + `accept`-mode ACL sufficient auth for a node to join, or do we want an
  explicit join token? Name: operator-given vs. auto-derive from hostname/tailnet? Role naming: adopt
  Harbor/port/harbormaster or keep plain hub/node/node-agent (cosmetic, deferrable).

**Also worth a consensus look — 3 Hermes concepts to adopt independent of adopting Hermes:**
workspace-*kinds* per bead (`scratch`/`worktree`/`dir:`), **typed block reasons**
(dependency/needs_input/capability/transient — directly addresses our stranded-`in_progress` pain),
and **profile-as-forked-identity** for credential isolation (maps onto `credential-isolation.md` +
a sandboxed Pi crew holding its own `OPENROUTER_API_KEY`).

---

## 5. Proposed sequencing

**Gate on everything multi-node:** the live remote e2e must finally be green on the real box
(`hk-nepva`, gb-mbp `enabled:false`). Don't build a multi-node roster on a single-node path that's
never run end-to-end live.

- **Phase 0 (now, unblocked): Pi-per-bead sandbox** (`plans/2026-07-02-pi-sandbox/`). Smallest slice
  that exercises the whole sandbox seam. Also lift the single-worker registry cap (idea 1 P0) in
  parallel — it's mechanical and prerequisite for multi-node. **This is the natural first step.**
- **Phase 1: Harness seam + auto-comms-subscribe (idea 5 D4 + idea 4).** Define the pluggable-harness
  contract (bead+context → structured result); make comms-subscribe a forced boot property (Pi surface
  first, Claude-Code hook second). Small, unblocks clean crew boot on any node/model.
- **Phase 2: Multi-node substrate (idea 1 P0–P2).** Dynamic join → resident node-agent + presence
  roster (A1) → `fleet`/`attach` → comms-bus relay (C1). This is what unblocks crew-on-other-machines
  for real. Requires D1, D2, D5 decided.
- **Phase 3: Pi crew (idea 3) + container generalization (idea 2 full).** Independent of each other;
  both sit on Phases 0–2. Pi-crew needs the crew-launch shim; container-full needs D3 (warm-cache) +
  D4 (backend) decided.
- **Phase 4 (later / maybe-never): peer gossip (A2) + peer bus replication (C2).** Only if hub-as-relay
  or hub-as-authority becomes the real bottleneck. Explicitly out of v1.

**Order of decisions the consensus pass should resolve first:** D1 (gates all of idea 1) → D4 (gates
the harness seam that Phases 1/3 both need) → D2/D3/D5 (gate Phases 2/3 detail). D1 and D4 are the two
that everything downstream hangs on.
