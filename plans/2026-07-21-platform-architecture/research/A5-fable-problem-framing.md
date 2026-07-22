# A5 — Problem framing + option space (independent architect pass)

**Author:** independent architect pass (Fable), 2026-07-21. **Inputs:** README/INPUTS, A1–A4,
realignment DECISIONS.md, realignment research/03. This is problem framing and option-space
analysis, not a design. Where I disagree with the main agent's current framing I say so
explicitly (§4). Where I think a piece of the operator's vision needs adjusting I say that
too, with reasons — nowhere do I shrink it by default.

---

## 1. The problem, framed at the right altitude

There are **three problems wearing one coat**, and most of the historical friction — including
the operator feeling dismissed — comes from arguing about them as if they were one.

### P1 — The FABRIC problem (greenfield): no generic transport/addressing/plugin layer exists

Harmonik has no way for a component on one machine (or in one container, or in one process)
to find and talk to a component elsewhere except by hand-building a bespoke pipe. The scrapped
ssh model is the proof, not just an incident: every one of its wedges is a **missing fabric
feature reinvented badly, per-path** —

- no addressing → per-run reverse tunnels with hand-allocated ports (03-remote, `reversetunnel.go`);
- no identity/registration → one hardcoded primary worker (`PrimaryWorkerIndex` always 0, A4);
- no generic channel for events → hook relay smuggled over `ssh -N -R` with silent
  `bridge_dial_failed` (03-remote);
- no env/context transport → `exec env KEY=VAL` argv-prefix hacks (03-remote, hk-okqyx);
- no plugin boundary → every one of those hacks landed inside `internal/daemon`.

D4's verdict ("every wedge is a symptom of 'make ssh behave like local'") is really a verdict
that **distribution was attempted without a fabric**. The operator's read — that the prior
planning underestimated the need for a powerful underlying system — is, on this evidence,
correct. The dismissals A1 documents ("gossip is over-engineering", "C1 hub-relay now, real
P2P maybe-never") were each locally defensible and **collectively produced the ssh
architecture**, which then failed for exactly the reasons a real fabric prevents.

The target for P1 already exists in remarkable detail: the substrate-v2 fleetd kernel
(A2). Channels (name + type), LOOKUP + roster as the identity/address system, opaque-bytes
transport, separate-process plugins with declared manifests, the kernel/plugin line enforced
mechanically. A2's finding stands: this architecture is cleanly separable from its rejected
"centralize agent learning" premise. **P1 is not a research problem; it is a scoping and
adoption problem.**

### P2 — The EXTRACTION problem (brownfield): two god packages, not a formless monolith

A4 is the most important correction in the whole input set. Harmonik is **not** a monolith of
hard-coded layers — it is 60+ packages with a CI-enforced depguard matrix, and the two
components the operator worried most about (keeper, watch) are the *cleanest* seams in the
tree. The real coupling is concentrated:

- `internal/daemon` — 641 files, fan-out 31: composition root that also *contains* the
  claude/codex/pi harness implementations, the queue wiring, the ssh transport, the crew
  wiring, and the DOT run-loop. Codex churn thrashes daemon because the codex *implementation*
  was never extracted, not because the harness *seam* is dirty (`handler`/`handlercontract`
  import only core/lifecycle — zero queue/bead/DOT/crew surface).
- `internal/core` — 501 files, fan-in 35: an undifferentiated shared-types kernel.

This matters for framing because P2 has a **different clock** than P1. Extraction (move
harness impls behind the existing `HarnessRegistry`; carve daemon into owned subsystems;
split core by type-family) can start now, incrementally, with no new fabric, and every step
reduces the risk of everything else. P1 is new construction; P2 is strangler-pattern surgery
on a live system. Lumping them into one "Thread P — foundation" (README) hides that they can
and should proceed on independent schedules.

### P3 — The DISTRIBUTED-EXECUTION problem: containerized beads, local and remote, scheduled

Replace the scrapped ssh model with: beads run inside containers (incus now; framework-
agnostic later per the operator's A1 steer), on this machine or others, for **security**
(the D3 uniform-sandbox workstream needs an isolation boundary that isn't per-harness) and
**load** (the one-worker cap is the named chokepoint, A3). Plus, eventually, placement — the
scheduling assessment's locked constraints apply (signals-first, declarative hot-reloaded
policy; A3/S).

P3 is where the delivery pressure lives (incus substrate is physically ready; the codex
sidecar already streams in-band over ssh stdout with **zero reverse-tunnel machinery** —
A3's key de-risking finding). It is also where the next ssh-shaped disaster happens if it's
built without P1.

### Is "foundation first" correct?

**Directionally yes; as a sequencing rule, refine it.** The strong form the operator is
right about: *do not design P3's control plane ad-hoc again.* The kickoff/status-back/
results-back/failure-semantics questions that D4 escalated ARE fabric questions; answering
them with another bespoke websocket protocol just builds ssh-model v2 with a different pipe.

But "foundation first" must not mean "complete the fleetd kernel, then start distribution."
That's the big-bang failure mode (§5, R1). The de-risked sequence is **contract-first,
co-validated**:

1. **Adopt P1's contracts now** — the reconciled kernel/plugin protos already exist and are
   lint-clean (A2). The contract is what gets approved, exactly as substrate-v2 intended
   ("The 19-RPC API is what Greg approves. NATS is an implementation detail").
2. **Build the minimal kernel slice that P3's first application actually consumes** — and
   make bead-execution-in-a-container the **first plugin**, i.e. the boundary test.
   Substrate-v2's own discipline says the first consumer validates the line ("comms needs a
   kernel RPC that isn't in the 19 → the boundary is wrong"). A dispatch/execution plugin is
   a *harder* and therefore *better* first test than comms.
3. **Run P2 extraction in parallel**, with one hard rule: nothing new for P3 lands in
   `internal/daemon`. The execution path is born outside the god package or not at all.

That sequencing honors "get the foundation right" without betting months of zero delivery on
a speculative kernel. The foundation is *proved by* the first distributed workload, not
*prerequisite to starting* it.

---

## 2. The option space, pressure-tested

### The three named options are not three rivals

**Options 1 and 2 are the same topology described from opposite ends.** Both have a central
queue-holder; both have workers that dial out and pull; both deliver results via git. Option
1 ("pull-based resident worker") emphasizes the worker's residency and local container
launch; Option 2 ("central server + registered workers") emphasizes the registration/lease
relationship and server⟂crew separation. Option 2's open question — *what happens to
in-flight work when a worker drops* — is precisely the lease/liveness semantics Option 1's
worker needs anyway. Presenting them as competing choices invites a false decision. The real
variables inside this family: does a per-machine resident exist at all; what are the lease
semantics; is registration dynamic.

**Option 3 is not a topology — it's an execution-agent shape.** "Minimal harmonik inside the
container, dials the server, pulls bead + DOT, reports as it advances" answers *what runs
inside the isolation boundary*, and it composes with either flavor of 1/2:

- 1+3: resident worker per machine launches Option-3 containers locally (worker = local
  container supervisor + cache-warmth owner — which is where A1's warm-build-cache problem
  naturally lives).
- 2+3: no resident; containers dial the central server directly (pure ephemeral — simplest,
  but every container pays cold-cache and the server manages N container lifecycles remotely).

**And Option 3 over the fleetd kernel is not even a new protocol.** "Pull a bead over a
websocket, stream status back" decomposes exactly onto kernel primitives: a POINT_TO_POINT
channel (competing consumers = work queue), REQUEST_REPLY for kickoff/ack, PUBSUB for status
events, LOOKUP for the container registering itself. The websocket is an implementation
detail *behind* the 6-method transport interface, precisely where substrate-v2 put NATS. So
the correct reading is: **Options 1/2/3 are applications; the fleetd kernel is the layer they
run on.** They are composable, not competing — which is itself strong evidence the operator's
layering instinct is right.

Two honest caveats from A2 about using the kernel this way (these are the *real* design work,
not blockers):

- The kernel is at-most-once by design; **durable work dispatch (leases, redelivery,
  orphan recovery) is plugin-layer machinery** built from journal + message_id + interest +
  liveness. That's the intended shape — but it means the dispatch plugin is a real distributed-
  systems artifact, not a thin adapter (§5, R4).
- LOOKUP/roster as specced assume long-lived daemons on static Tailscale addresses; **ephemeral
  worker/container join-leave is an extension** (A2 thin-spot #4), not free.

### The full option table

| Option | What it really is | Real leverage | Real risk |
|---|---|---|---|
| **1. Pull-based resident worker** | Per-machine supervisor in the central-queue family | Residency owns cache warmth (the A1 warm-build problem), amortizes provisioning, one dialer per box not per container | A per-machine daemon is a second lifecycle to manage; if built bespoke it becomes another one-off transport |
| **2. Central server + registered workers** | Same family; the registration/lease half | Makes worker join/leave + in-flight-failure semantics explicit — the part ssh never had | "Central server separate from crew" can drift into a second deploy artifact + coordination service before it's needed |
| **3. Minimal-harmonik-in-container** | Execution-agent shape inside the boundary | Container needs only a bead ref + a dial-back address — no repo-shipping choreography, no reverse tunnel; the isolation boundary D3's uniform sandbox needs | "Minimal harmonik" must actually be minimal: if it links the daemon god package it drags the monolith into every container (forcing P2 extraction — arguably a feature) |
| **fleetd kernel (P1)** | The layer under all of the above | One transport/identity/plugin substrate for BOTH audiences: fleet comms (crew/captain) and execution dispatch (beads). Enforced boundary + kill criteria already designed | Kernel scope creep; M0 (laptop-sleep) unvalidated; LOOKUP is the one no-upstream-library component |
| **4. Harness-native remote (unnamed)** | Resident `codex app-server` on the target, driver at home, JSON-RPC streamed in-band — A3 shows this works TODAY over ssh stdout with no tunnel | Fastest possible delivery proof; zero new transport code; the right *interim* bridge | Per-harness by construction — rebuilds "N bespoke transports," the exact coupling being escaped. Fine as a proof, poison as the architecture |
| **5. Control-plane-only transport (unnamed, really a scoping decision)** | The fabric carries control + events + lookups ONLY; **git remains the artifact/data plane** (it already is: results come home as `run/<id>` branches) | Keeps the kernel small (no artifact durability, no big-payload design); matches `max_payload_bytes` + no-fleet-order refusals | If unstated, someone will try to stream diffs/repos over the fabric and blow up its scope. Must be decided explicitly (§3, C1) |
| **6. Buy-not-build (devil's advocate)** | Expose NATS/JetStream directly, or adopt Temporal/k8s-shaped machinery for durable work | Less code; JetStream literally is durable work-queues + leases | Surrenders the contract: harmonik's surface becomes NATS's surface; substrate-v2 already made the sane call (embed NATS *behind* the owned 19-RPC contract, JetStream OFF). Reject on the record, not by default |
| **7. Per-node DOT distribution (axis, not option)** | Break the whole-run-pinned-to-one-worker constraint (03-remote flags it self-imposed) | Reviewer on a trusted host + implementer in a container becomes natural (the hk-qxvc2 entanglement dissolves) | Cross-machine cascade state; keep whole-run-per-container for v1 but don't design a protocol that forbids per-node later |

---

## 3. The crux decisions

Six decisions actually determine the architecture. Alignment should resolve these — nothing
downstream is designable until they're settled.

**C1 — Data plane vs control plane.** Does the fabric carry artifacts (code, diffs, repos) or
only control, events, and addresses — with git staying the artifact plane? Everything about
kernel size, durability, and payload limits follows from this. *My position: control-plane-
only. Git is already a working, credentialed, content-addressed artifact plane; re-carrying it
over the fabric is the #1 scope-creep vector.*

**C2 — Is the kernel in scope NOW, or is P3 built on an ad-hoc pipe and migrated later?**
This is the operator's central bet. *My position: kernel-now, minimally — "temporary
websocket, migrate later" is precisely how the ssh model came to exist and why D4 happened.
But "kernel-now" means the thin slice P3 consumes (transport iface + channels + LOOKUP/roster
+ plugin lifecycle), with substrate-v2's kill criteria armed, not all 19 RPCs polished first.*

**C3 — Topology authority: where does "central" live?** The peer-vs-hub fight that A1
documents dissolves if it's placed at the right layer: **transport is peerless** (no box in
charge — substrate-v2 proves cold-boot-alone + survive-peer-death), while **dispatch is a
central plugin** (daemon-owns-the-queue survives as an application-layer fact, not a fabric
property). This honors the strongest form of the operator's peer instinct — the fabric
outlives any single box — without reopening the locked queue decision. Alignment should
ratify this two-layer answer explicitly so the argument stops recurring.

**C4 — Extract-then-distribute vs distribute-then-extract.** Does P3 wait on P2? *My
position: neither strict order. P2 runs in parallel with one non-negotiable: the execution
path (worker plugin, container supervisor, dispatch plugin) is born OUTSIDE `internal/daemon`.
The minimum extraction P3 forces — harness impls out from behind the already-clean
`HarnessRegistry` seam — is exactly the extraction A4 says is highest-value anyway. Option 3's
"minimal harmonik" is the forcing function: it can't be minimal until it stops linking daemon.*

**C5 — Unit of dispatch + failure semantics.** Whole-bead-run per container (v1, matches
today) with an explicit lease: worker/container registers, holds a lease per run, heartbeats;
lease expiry ⇒ a *typed* orphan-recovery path (requeue vs park vs escalate — A1's
typed-block-reasons prior art applies). This is Option 2's open question and it is THE
distributed-systems hard part; it must be designed, not discovered (§5, R4). Decide also that
the protocol must not structurally forbid per-node distribution later (axis 7).

**C6 — Process boundary policy: what is a plugin vs what is a package?** Substrate-v2 plugins
are separate processes — right for harnesses, execution workers, comms, anything that crashes
or reloads independently. But NOT every decoupled subsystem needs a process boundary; keeper
and watch are already clean as depguard-fenced in-proc packages (A4). A written rule for
"process when X, package when Y" (crash/reload isolation, cross-machine, credential isolation
⇒ process; otherwise ⇒ package + depguard) prevents both under-building (everything stays in
daemon) and the over-building failure mode (60 packages become 60 gRPC services). This also
surfaces a latent tension: README says the operator doesn't necessarily want multiple deploy
artifacts, but the plugin model IS multiple binaries — reconcile explicitly.

---

## 4. Where I disagree with the current framing

**4a. "Do NOT design D without P" is stated too strongly as sequence, too weakly as
constraint.** The README's sentence reads as P-completes-first. The defensible version is:
*P3's control-plane protocol must be expressed in P1's contract vocabulary from day one* —
but P1 unconsumed is speculation, and its own methodology says the first real plugin is the
boundary test. Make P3's execution plugin that first test. Sequential threading also creates
schedule coupling: any P slip freezes all of D, which is how delivery pressure ends up
justifying "just one quick hack in daemon."

**4b. Thread P conflates two different jobs.** Fabric-building (P1, greenfield) and
god-package extraction (P2, brownfield strangler work) have different risk profiles, different
skills, different clocks, and almost no shared blocking edge. A4's bottom line — "the work is
extraction + kernel-splitting, not seam invention" — deserves to be its own thread, startable
immediately, not a sub-bullet of the transport layer.

**4c. INPUTS §4 asserts coupling that ground truth refutes — correct the problem statement.**
Keeper and watch are *clean, CI-fenced seams* (A4: keeper is depguard-banned from daemon and
workloop; watch imports core/agentmanifest/eventbus only). Leaving "keeper and watchdog
shouldn't have bindings... but suspected they do" in the problem statement will misdirect
effort at solved problems and — worse — undermines the case where the operator is *right*:
daemon and core. Precision here is what honoring the vision looks like; the strongest version
of the coupling thesis is "the damage is concentrated in two god packages and the trapped
harness implementations," which is more actionable than "it's a monolith," not less.

**4d. The pull-worker proposal was under-specified in exactly the dimension that matters —
and Option 2 contains the missing piece.** The operator's rejection of locking the pull
direction (D4: "the control-plane questions — kickoff, status-back, status-check — were
unanswered") was correct. But the right synthesis is not "broaden to three rival options"; it
is: Options 1/2 are one family, Option 2's registration/lease semantics are the missing
control-plane answer, Option 3 defines the in-boundary agent, and the fabric underneath is
P1. The alignment doc should present that composition, not a three-way bake-off.

**4e. The "not over-architecting" framing needs a falsifiable discipline attached, or it will
curdle.** Breaking the dismissal pattern is a legitimate first-class goal — A1 documents real,
repeated narrowing of the operator's vision, and the ssh postmortem vindicates the instinct.
But a plan whose stated premise is "objections were the pattern to break" has no brake left.
The fix is already on the shelf: substrate-v2's kill criteria and boundary tests ("two
consecutive plugins needing new kernel RPCs ⇒ stop"; ~3000-line kernel tripwire; the
dep-allowlist test). Adopt them as *shared* ground rules at alignment time, so future scope
disputes are settled by a test both sides pre-agreed to — not by whoever invokes
"over-engineering" or "dismissal" first.

**4f. Delivery is missing from the split entirely.** README names Threads P and D; nothing
names the bridge that keeps proof flowing while they build. A3 hands it over gift-wrapped:
codex sidecar in-band + incus golden image + per-run ephemeral container ≈ a provable
containerized-bead slice with near-zero new transport code (Option 4). Run it explicitly as a
*scaffold with a demolition date* — its lifetime ends when the P1-based execution plugin can
run the same bead — rather than letting it emerge unplanned and calcify the way ssh did.

---

## 5. The biggest risks to getting this right

**R1 — Big-bang foundation stall (the under-delivery failure).** P becomes a months-long
kernel build with no shipped work; delivery pressure then forces expedient hacks back into
`daemon`; you end with the monolith AND a half-finished fleetd. Mitigation is structural, not
willpower: first kernel consumer = P3's execution plugin (§1); the Option-4 scaffold keeps
proof flowing; P2 extraction ships value weekly regardless.

**R2 — Over-building (naming it because the operator asked for honesty, not to shrink
scope).** The layered fabric is not over-engineering — the ssh postmortem settles that. The
*actual* over-build failure modes are specific: (a) the kernel absorbing domain knowledge
(bead/DOT-aware RPCs — the `bytes`-not-`Any` rule and kill criteria exist to catch this);
(b) the fabric trying to be the artifact plane (C1); (c) process-boundary maximalism —
plugin-izing subsystems that are already clean in-proc packages, paying gRPC + lifecycle +
versioning tax on every seam (C6); (d) building ephemeral-worker LOOKUP generality before one
container has run one bead. Every one of these has a cheap tripwire; arm them at alignment.

**R3 — God-package extraction as merge-conflict warfare.** `core` (fan-in 35) and `daemon`
(641 files) are edited constantly by active lanes; a broad refactor mid-flight is conflict
hell and regression roulette. Discipline: freeze first (depguard tripwire: no NEW files in
daemon for extracted concerns; additions to core require a named type-family), extract at the
edges behind existing seams (harness impls first — the registry is already there), split
`core` last and by type-family, never as one rename storm.

**R4 — Hand-waving the durable-dispatch layer.** The kernel is at-most-once *on purpose*;
leases, redelivery, orphan recovery, and exactly-once-effects live in the dispatch plugin.
This is the layer where distributed systems actually hurt, and it's currently one sentence in
Option 2 ("OPEN: what to do about in-flight work"). If it isn't designed with the same rigor
as the kernel protos, fleet-scale stranded-`in_progress` beads (today's pain, multiplied by N
containers) are guaranteed. Prior art to lift: typed block reasons (A1/Hermes), the existing
run-registry counters, `br show --assignee` durable attribution.

**R5 — Building on unvalidated substrate assumptions.** Substrate-v2's own top risk stands:
M0 (macOS laptop sleep as a peer) never tested. Plus three P3-specific landmines that are
independent of architecture and will replay inside containers: the chronic worktree-create
defect (hk-2hfyt — daemon-side runner-wrapper bug, A3 flags it as possibly the bigger relay
risk), the Claude OAuth wall for ephemeral containers (hk-bkd6h — codex-only for unattended
containers is the standing scope-out), and unvalidated warm-cache economics (per-language
build cost is why "per-run ephemeral" may not survive contact). None of these are fabric
problems; all of them can masquerade as fabric failures and burn trust in the new
architecture if not ring-fenced first.

**R6 — Requirements conflation across the fabric's two audiences.** The same kernel should
serve fleet comms (crew/captain messaging: at-least-once + event_id dedupe, N3) and execution
dispatch (beads: leases + competing consumers). That dual use is the strongest argument FOR
the kernel — one substrate, two plugins. The risk is letting either plugin's requirements
leak into kernel semantics (comms durability or dispatch leasing "just this once" as a kernel
RPC). The boundary test catches this only if both plugins are held to it.

---

## Recommended alignment agenda (condensed)

1. Ratify the three-problem decomposition (P1 fabric / P2 extraction / P3 distributed
   execution) + the delivery scaffold (Option 4, with demolition date).
2. Resolve C1–C6 (§3) — these, not option-picking, are the decisions.
3. Correct the problem statement per §4c (coupling is daemon+core, keeper/watch are clean).
4. Adopt substrate-v2's kill criteria + boundary tests as shared ground rules (§4e).
5. Confirm the composition reading of Options 1/2/3-on-P1 (§2) and kill the three-way
   bake-off framing.
