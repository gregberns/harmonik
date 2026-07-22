# PROBLEM statement — for alignment (2026-07-21)

Triangulated from the operator's prior work (`research/A1` distributed-fleet, `A2`
substrate-v2), the container/scheduling context (`A3`), the real code coupling (`A4`), and an
independent architect pass (`A5`, Fable). This is the problem to align on BEFORE any design.

---

## The core reframe: three problems wearing one coat

Most of the historical friction — including the operator feeling dismissed — comes from
arguing these as if they were one thing. They are three, with different clocks and risks.

### P1 — The FABRIC problem (greenfield)
Harmonik has **no generic way for a component on one machine / container / process to find and
talk to another** except by hand-building a bespoke pipe. The scrapped ssh model is the
*proof*: every one of its wedges was a missing fabric feature reinvented badly — no addressing
→ hand-allocated reverse-tunnel ports; no registration → one hardcoded worker; no generic
event channel → hook relay smuggled over ssh; no plugin boundary → every hack landed inside
the daemon. **The operator's "we underestimated the need for a strong underlying system" is
vindicated by this evidence.** And the target already exists in detail: substrate-v2's `fleetd`
kernel (transport + LOOKUP/roster addressing + separate-process plugins), separable from its
rejected "shared searchable memory" premise. **P1 is a scoping/adoption problem, not research.**

### P2 — The EXTRACTION problem (brownfield)
Harmonik is **not a formless monolith** — 60+ packages, CI-enforced boundaries; keeper and
watch are among the *cleanest* seams (the earlier worry about them was wrong). The damage is
concentrated in **two god packages**: `internal/daemon` (641 files — composition root that
*contains* the harness impls + queue + ssh + crew + DOT loop) and `internal/core` (501-file
shared-types bag). Codex churn thrashed the daemon because the codex *implementation* was never
extracted, not because the harness *seam* is dirty. Extraction is **strangler-pattern surgery
that can start now, incrementally**, using seams that already exist — a different clock than P1.

### P3 — The DISTRIBUTED-EXECUTION problem
Replace the scrapped ssh model with **beads running in containers** (incus is ready now), on
this machine or others, for **security** (D3's uniform sandbox needs an isolation boundary
that isn't per-harness) and **load distribution** (the one-worker cap is the named chokepoint),
plus eventual **scheduling** (signals-first, declarative policy — already locked). This is
where delivery pressure lives — and where the next ssh-shaped disaster happens if it's built
without P1.

---

## Corrections to the earlier (main-agent) framing — owned

- **"Don't design D without P" was too strong as a *sequence*.** Right as a *constraint*
  (P3's control-plane must speak P1's vocabulary, not another bespoke websocket), wrong as
  "finish the kernel, then start distribution" — that's the big-bang stall that forces hacks
  back into the daemon. Correct rule: **contract-first, co-validated** — adopt P1's contracts
  now, build the *minimal* kernel slice that P3's first container actually consumes, make
  **bead-execution-in-a-container the kernel's first plugin (its boundary test)**, and run P2
  extraction in parallel with one hard rule: *nothing new for P3 lands in `internal/daemon`.*
- **Thread P conflated two jobs.** Fabric-building (P1, greenfield) and god-package extraction
  (P2, brownfield) have different risk profiles and almost no shared blocking edge → they are
  **separate threads**, not one "foundation."
- **The three "options" are not rivals — they compose.** Option 1 (pull-worker) and Option 2
  (central server + registered workers) are the *same topology from opposite ends*; Option 2's
  "what happens to in-flight work when a worker drops" IS the lease semantics Option 1 needs.
  Option 3 (minimal-harmonik-in-container) is an *execution-agent shape* that runs on either.
  And all three decompose onto the fleetd kernel primitives (queue channel + request/reply +
  pub/sub + LOOKUP). **They are applications; the kernel is the layer they run on** — which is
  itself evidence the layering instinct is correct. Kill the three-way bake-off framing.
- **A delivery scaffold was missing.** The codex sidecar already streams in-band over ssh with
  no reverse tunnel, and incus is ready — so a containerized-bead proof is buildable now with
  near-zero new transport code. Run it explicitly as a **scaffold with a demolition date** (it
  dies when the P1-based execution plugin can run the same bead), so it can't calcify like ssh.

---

## The six crux decisions (the real alignment agenda)

Option-picking is downstream. THESE determine the architecture:

- **C1 — Data plane vs control plane.** Does the fabric carry artifacts (code/diffs/repos) or
  only control/events/addresses, with **git staying the artifact plane**? (Leaning: control-
  plane only — git already works; re-carrying it is the #1 scope-creep vector.)
- **C2 — Kernel now, or ad-hoc pipe migrated later?** (Leaning: kernel-now but *minimal* — the
  thin slice P3 consumes, kill-criteria armed — because "temporary pipe, migrate later" is
  literally how the ssh model was born.)
- **C3 — Where does "central" live?** Dissolve the peer-vs-hub fight by layer: **transport is
  peerless** (no box in charge — honors the operator's peer instinct: the fabric outlives any
  single box), **dispatch is a central plugin** (daemon-owns-the-queue as an app-layer fact,
  not a fabric property). Ratify this two-layer answer so the argument stops recurring.
- **C4 — Extract-then-distribute or distribute-then-extract?** (Leaning: neither strict order —
  parallel, with the execution path born OUTSIDE `internal/daemon`; the minimum extraction P3
  forces is exactly the highest-value extraction anyway.)
- **C5 — Unit of dispatch + failure semantics.** Whole-bead-run per container (v1) with an
  explicit **lease + heartbeat + typed orphan-recovery** path. This is Option 2's open question
  and **the actual distributed-systems hard part** — design it, don't discover it. Don't
  structurally forbid per-node distribution later.
- **C6 — Process boundary policy: plugin (separate binary) vs package (in-proc, depguarded)?**
  A written rule (crash/reload isolation, cross-machine, credential isolation ⇒ process;
  otherwise ⇒ package) — prevents both under-building (everything stays in daemon) and
  over-building (60 packages become 60 gRPC services). Also reconcile: the operator doesn't
  want many deploy artifacts, but the plugin model IS multiple binaries.

---

## The shared discipline (guardrail against BOTH failure modes)

"Not over-architecting" is a legitimate first-class goal — but a plan whose premise is
"objections were the pattern to break" has no brake. The fix is on the shelf: adopt
**substrate-v2's kill criteria + boundary tests as shared, pre-agreed ground rules** (e.g.
"two consecutive plugins needing a new kernel RPC ⇒ the boundary is wrong"; a kernel line-count
tripwire; the dependency-allowlist test). Then future scope disputes are settled by a test both
sides agreed to in advance — not by whoever says "over-engineering" or "dismissal" first.

The named over-build failure modes to arm tripwires against: kernel absorbing domain knowledge
(bead/DOT-aware RPCs); fabric trying to be the artifact plane (C1); process-boundary maximalism
(C6); building ephemeral-worker generality before one container has run one bead.

---

## Status

FOR ALIGNMENT. Nothing below the frame is decided. Next step after the operator aligns on this
frame: resolve C1–C6, then (and only then) design. The likely plan split is **three threads**
(P1 fabric / P2 extraction / P3 distributed execution) + a time-boxed delivery scaffold — NOT
the two-thread split in the README, which is superseded by this.
