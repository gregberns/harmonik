# Remote-node telemetry & autoscale — prior art and naming

> Research doc for the remote-execution evolution. Grounding read: `.harmonik/workers.yaml`
> (the `gb-mbp` worker registry — `max_slots`, `enabled`, ssh transport) and
> `docs/remote-substrate/WORKER-SETUP-macos.md` (box A SSHes work to a worker; results
> return as a pushed branch box A merges).
>
> Where we are today: a single central Go daemon ("box A") owns the queue, picks a worker
> from a static `workers.yaml`, and PUSHES one bead at a time over SSH (`tmux new-window` →
> headless `claude` in a worktree → push branch → box A merges). Worker capacity is a hand-set
> `max_slots: 4`. There is no live process on the worker, no telemetry, no autoscaling, and
> liveness is "is the box ssh-reachable right now."
>
> Where the operator wants to go: (a) a live harmonik presence ON the remote, (b) telemetry +
> health reported back to the center, (c) the center autoscales per-box concurrency. And: "we
> might want a name for the central node."

---

## 1. Prior-art mapping

The shape harmonik is growing into is the canonical **control-plane / node-agent / node**
triad that every mature distributed-worker system converges on. Map each piece, steal one
lesson, avoid one trap.

### 1.1 Kubernetes — control-plane / kubelet / node

**The model.** The API server + scheduler form the control plane. On every node a **kubelet**
runs as a persistent local agent: it registers the node, posts a periodic **heartbeat** (in
modern k8s a tiny `Lease` object, decoupled from the heavier status update precisely so the
heartbeat stays cheap), and **reports the node's allocatable resources** (cpu/mem/pods). The
scheduler does **bin-packing**: it places pods using the node's *advertised capacity minus
already-committed requests*, never by probing the node live.

**Map onto harmonik.**
- *Node object* → already exists: a `workers.yaml` entry (`gb-mbp`, `max_slots: 4`,
  `enabled`). This is the static half of a Node object. The missing half is the *status*
  subobject the node itself writes: live slot occupancy, load, last-heartbeat.
- *kubelet* → this is the "live harmonik presence on the remote" the operator wants. A small
  long-lived process on the worker that registers, heartbeats, and reports capacity/occupancy.
  Today there is NO such process — box A spawns a transient `claude` per bead and SSH-reach is
  the only liveness signal.
- *scheduler bin-packing* → the center's autoscale/placement decision: how many concurrent
  beads to admit per box, computed from advertised capacity minus in-flight, not from live
  probing.

**Steal:** the **Lease/heartbeat split** — make the liveness heartbeat a tiny, frequent,
cheap message, separate from the fuller (and rarer) capacity/telemetry report. Conflating them
is why naive health checks get expensive and then get throttled into uselessness.

**Trap:** kubelet's **node-status timeout → pod-eviction** cascade. When a node goes silent,
k8s waits a grace period, then evicts and reschedules. Harmonik's analogue — "box goes
silent, reclaim its in-flight beads and re-dispatch" — MUST be conservative. A silent box may
just have a slow/saturated tmux or a flaky tailnet while its `claude` is mid-commit. Premature
"eviction" = double-dispatching a bead that's about to push (and harmonik's history is full of
double-dispatch / stranded-`in_progress` pain — see the `no_commit` and stuck-bead memory
notes). Grace period must exceed a worker's realistic commit budget.

### 1.2 Nomad — server / client-agent / allocation

**The model.** Nomad servers hold the queue and make placement decisions. A **client agent**
runs on every node, and on boot it **fingerprints** the machine — advertising capabilities and
attributes (OS, arch, kernel, installed drivers, available cpu/mem, custom attributes). The
client then **pulls** allocations the server has assigned to it. Placement is constraint-based:
a job says "needs `os=darwin` and driver=X", the server matches against advertised fingerprints.

**Map onto harmonik.** `workers.yaml` already carries `os: darwin` — that's a hand-written
fingerprint. The natural evolution is the remote agent **fingerprinting itself**: OS, arch,
which toolchain deps are present (Go/Node/gh/claude versions), repo path, free slots. The
`WORKER-SETUP-macos.md` "Part 5 — dependency drift" pain (a bead fails on the worker because a
dep is missing) is *exactly* the problem fingerprinting solves: don't dispatch a bead that
needs dep X to a box that hasn't advertised dep X.

**Steal:** **capability fingerprinting as the basis for placement** — let the box advertise
what it can run, and route by constraint-match instead of the current binary `enabled` flag.
This directly retires the "dispatched to a box missing a dependency" failure class.

**Trap:** **fingerprint staleness.** A box fingerprints `claude v1.2` at boot, the operator
upgrades it, the advertised attribute lies. Re-fingerprint on a schedule (or on agent restart),
and treat the toolchain gate failure as a signal to force a re-fingerprint, not just a retry.

### 1.3 CI runner pools — GitHub Actions self-hosted, Buildkite, GitLab runner

**The model.** This is the cleanest prior art for harmonik's "headless agent runs one job in a
fresh workspace" loop. Universally these are **agent-pulls-job**: the runner process lives on
the box, registers with the control plane under **tags/labels** (`os=macos`,
`self-hosted`, `arch=arm64`), then **long-polls** the center for jobs whose labels it matches.
The center never opens a connection *into* the runner. Two runner lifestyles:
- **Persistent** runners stay up and take job after job (state can leak between jobs → must
  clean the workspace).
- **Ephemeral** runners take exactly one job then exit/re-register (clean isolation, higher
  spin-up cost). GitHub's `--ephemeral`, Buildkite's job lifecycle.

**Map onto harmonik.** Harmonik's per-bead `claude` is effectively an *ephemeral* runner
(fresh worktree, one bead, then gone) — but it's **pushed**, not pulled, and there is no
persistent registered agent. The CI-runner world says: keep a **persistent registered agent**
on the box (that's the "live presence"), and let *it* spawn ephemeral per-bead workspaces. You
get the clean-isolation benefit of ephemeral execution AND a durable thing to heartbeat and
report telemetry. The tag/label model maps onto Nomad-style fingerprinting.

**Steal:** **persistent registered agent + ephemeral per-job workspace** — exactly the split
harmonik wants (durable telemetry endpoint; clean per-bead worktree it already has).

**Trap:** runner **registration-token / trust sprawl** and **zombie runners**. Self-hosted
pools accumulate runners that registered, went offline, and never deregistered — the center
keeps "scheduling" to ghosts. Harmonik must make deregistration / TTL-expiry first-class
(an agent that misses N heartbeats is *removed from the schedulable set*, not left as a
ghost slot). This is the same hazard as harmonik's existing zombie-crew / presence-stale
reconciliation — reuse that muscle.

### 1.4 Serf / SWIM gossip & heartbeat liveness

**The model.** Serf (and the SWIM protocol underneath) does **decentralized failure
detection**: members gossip, and a node suspected dead is **probed indirectly** (ask K other
members to ping it) before being declared failed — this crushes false positives from a single
flaky link. Liveness is *eventually-consistent* and *confidence-graded* (alive / suspect /
dead), not a binary up/down.

**Map onto harmonik.** Directly relevant to "box goes silent." Harmonik's center is a single
authority (not a gossip mesh), so it won't adopt SWIM wholesale — but it should steal SWIM's
**graded liveness + suspicion timeout** idea instead of today's instantaneous "ssh failed =
down." A missed heartbeat → `suspect` (keep its in-flight work, stop sending NEW work) → after
a suspicion window with no recovery → `dead` (reclaim work). That intermediate `suspect` state
is the whole point.

**Steal:** **graded liveness with a suspicion window** — alive → suspect → dead, never a
binary flip, and stop *new* dispatch at `suspect` while only reclaiming in-flight at `dead`.

**Trap:** don't import full gossip/anti-entropy machinery for a hub-and-spoke topology with one
control node and a handful of workers. SWIM's complexity pays off at hundreds of peer nodes;
at harmonik's scale it's over-engineering — take the *state machine*, not the *protocol*.

### 1.5 Ray / Dask — head node + workers + autoscaler

**The model.** A **head node** holds the scheduler (and, in Ray, the object store / GCS);
**worker nodes** join, heartbeat, and report resource availability. The **autoscaler** watches
the pending-task queue vs. live cluster capacity and scales worker *count* up/down — scale up
when the queue backs up, scale down (and reclaim) idle workers after a cooldown. Dask's
distributed scheduler is the same head/worker/heartbeat shape.

**Map onto harmonik.** The operator's autoscale goal is *per-box concurrency* (how many beads
to admit on `gb-mbp`), which is a slightly different axis than Ray's *node-count* autoscaling —
but the **control loop is identical**: compare offered load (queue depth) against reported
capacity/health, adjust the admitted-concurrency target, apply a cooldown so it doesn't
oscillate. Today `max_slots: 4` is a static hand-tuned constant; the Ray autoscaler pattern
turns it into a *target the center computes* from the box's reported headroom (load average,
memory pressure, recent failure rate).

**Steal:** the **closed autoscale loop with a cooldown/hysteresis band** — adjust per-box
concurrency from reported headroom, but damp it so it doesn't flap. (Harmonik already believes
in hysteresis bands — see the keeper warn/act band design — same instinct, new surface.)

**Trap:** **scaling on a lagging or wrong signal.** Ray autoscalers misbehave when they scale
on queue depth alone and ignore that in-flight tasks are already saturating a node. Harmonik
must scale per-box concurrency on the *box's own reported pressure* (load/mem/failure-rate),
not just central queue depth — otherwise it'll cram a 5th bead onto a box that's already
thrashing on 4 and watch them all fail `no_commit`. The existing remote pain (claude exits in
~9s with no commit under contention) is a preview of exactly this.

---

## 2. Push vs. pull

**Today: push.** Box A's daemon owns the queue and *pushes* a chosen bead over SSH into a
`tmux new-window` on the worker. There is no agent on the worker to pull anything.

**The case for staying push-ish.** Harmonik's whole identity is *daemon-owns-the-queue*: the
center is the single scheduler, holds beads/JSONL/git authority, and makes the placement call.
SSH-spawn is push by nature — you open a connection and start a process. A pure pull model
(worker long-polls "give me a bead") would invert that ownership and duplicate scheduling logic
onto every box, which fights the architecture and the locked decisions.

**The case the telemetry/autoscale goal makes for pull.** Push-only has a structural blind
spot the operator's three goals all run into:
- You can't autoscale per-box concurrency without the box *reporting back* — that report is an
  inbound (worker→center) channel, which is the pull-direction connection.
- "Live presence on the remote" + "report telemetry/health back" is, definitionally, a
  persistent worker-initiated connection to the center. That's the CI-runner agent model.
- Liveness-on-silence (Serf/kubelet) needs the worker to *send* heartbeats, not the center to
  *probe* — center-probing over SSH is exactly today's brittle binary check.

**Verdict — hybrid, center-authoritative ("push placement, pull pickup").** Keep the center as
the sole scheduler that *decides* which bead goes to which box (push semantics for the
*decision*), but add a persistent agent on the worker that **opens the connection to the
center, heartbeats, advertises capacity, and pulls the work the center has already assigned to
it** (pull semantics for *transport + telemetry*). This is precisely the GitHub-Actions /
Nomad-client shape: the control plane assigns, the agent dials home and pulls its assignment.

Why this fits harmonik specifically:
1. It preserves *daemon-owns-the-queue* — placement stays central; only delivery + reporting
   invert.
2. The worker-initiated connection is the **same channel** that carries heartbeat, capability
   fingerprint, and live telemetry — one connection solves liveness, autoscale input, and
   work pickup at once. Push-only would need a *second*, separately-built reporting channel.
3. It sidesteps the brittle "ssh-reach = liveness" probe and the root-owned-socket /
   tunnel-direction fights already in the memory notes (the inbound -R socket pain): the agent
   dialing *out* to the center is firewall- and NAT-friendlier than the center reaching in.
4. The per-bead worktree/push/merge flow is **unchanged** — only *who initiates* changes. The
   agent pulls its assigned bead and runs the exact same ephemeral worktree loop locally,
   which also kills a whole class of SSH-spawn fragility (the `tmux new-window` quoting /
   premature-completion-detection bugs in `workers.yaml`'s comments are all artifacts of
   *remotely* driving tmux; a local agent spawns its own tmux and reports the result).

So: **don't go pure-pull (it dissolves the central queue), don't stay pure-push (it can't
carry telemetry or autoscale).** Center decides; agent dials home, heartbeats, and pulls.

---

## 3. Naming

Constraints: harmonik already runs **musical** (the project name; "harmonik"), **nautical**
(captain / crew), and **Mad-Max Gas-Town** (worktrees/merges) metaphors, with tmux sessions as
the substrate. A good set should (a) name the **central node**, (b) the **remote node/box**,
(c) the **lightweight agent process** on it — and read cleanly next to *captain/crew*. Keep it
tasteful; avoid cutesy.

### Set A — Nautical (extends captain/crew; **recommended**)

| Role | Name | Why |
|---|---|---|
| Central node | **Harbor** | The home port the whole fleet reports to and returns to. Captain/crew already sail; the center is the harbor they dispatch from and push branches back into. Evokes "home base" without being literally "home". |
| Remote node / box | **Port** (or **berth**) | A reachable port the fleet can put in at. `workers.yaml` entries become "ports". |
| Remote agent process | **Harbormaster** | The small resident authority at each port that registers it, reports conditions, and hands incoming crews their berth. Exactly the kubelet/runner-agent role — and "harbormaster ↔ harbor" is a crisp center/edge pairing. |

Reads naturally: *"the harbor dispatches a crew to a port; the port's harbormaster heartbeats
back."* Coheres with captain/crew, no new metaphor introduced. **This is the recommendation** —
it's the only set that extends an *existing* harmonik metaphor rather than adding a fourth.

### Set B — Musical (ties to "harmonik" itself)

| Role | Name | Why |
|---|---|---|
| Central node | **Conductor** | Leads the ensemble; the operator's own candidate, and the strongest musical fit for "central scheduler." |
| Remote node / box | **Stand** (music stand) / **section** | Where a player sits; an orchestra "section" is a pool of players. |
| Remote agent process | **Player** (or **desk**) | The resident performer at the stand that takes its cues. |

Clean and on-brand for "harmonik," but it competes with captain/crew rather than extending it,
and "player/stand" is fuzzier than "harbormaster/port." Strong second choice if the operator
wants to lean into the *musical* identity over the nautical one. Note: **conductor** as the
center name is excellent on its own and could be borrowed into Set A if "harbor" feels too soft
for the central authority.

### Set C — Plain infrastructure (no metaphor; safest for docs/code)

| Role | Name | Why |
|---|---|---|
| Central node | **Hub** | Unambiguous, industry-standard, greppable. |
| Remote node / box | **Node** (keep **worker** for the box) | Matches `workers.yaml` already. |
| Remote agent process | **Node-agent** (or **workerd**) | The kubelet analogue, named plainly. |

Zero cuteness, instantly legible to anyone who's run k8s/Nomad, and it won't age badly in code
identifiers. Use this if the metaphor sets feel too precious for internal type names — and note
you can *mix*: ship plain names in code (`nodeAgent`, `hub`) and use the nautical names in
operator-facing prose/CLI.

### Recommendation

**Adopt Set A (nautical), with one borrow.** Name the central node **Harbor**, remote boxes
**ports** (`workers.yaml` entries = ports), and the resident process the **harbormaster**. It's
the only option that *extends* an existing harmonik metaphor (captain/crew → harbor/port),
gives a tight center/edge word-pair (harbor ↔ harbormaster), and reads cleanly in a sentence.
If the operator prefers the central node to sound more authoritative, swap **Harbor →
Conductor** for the center only and keep **port / harbormaster** for the edge — "the Conductor
assigns; the harbormaster reports" still scans. Keep plain `node-agent`/`hub` available as the
in-code identifier spelling if the metaphor feels heavy in Go type names.

---

## Appendix — one-line cross-walk

| harmonik concept | k8s | Nomad | CI runners | Ray |
|---|---|---|---|---|
| Central node ("Harbor") | control plane / scheduler | server | control plane | head node |
| `workers.yaml` entry | Node object (spec half) | node + fingerprint | runner registration/tags | worker registration |
| Remote agent ("harbormaster") | kubelet | client agent | self-hosted runner | worker process |
| Live slot/capacity report | node status / Lease | fingerprint + resources | runner busy/idle | resource heartbeat |
| Per-box concurrency target | scheduler bin-pack | bin-pack | concurrency limit | autoscaler |
| Box-goes-silent handling | node-status timeout→evict | heartbeat→down | runner offline→deregister | dead worker→reschedule |
