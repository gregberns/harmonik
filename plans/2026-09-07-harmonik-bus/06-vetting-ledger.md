# 06 — Vetting ledger: the agent-substrate-v2 / P1-kernel-fabric design

**Date:** 2026-09-07
**Role:** adversarial vetting, not adoption. The operator's caution: *"be careful of everything
you're pulling in. Those need to be well thought through — and they may have conflicting ideas or
over-architected areas. Make sure they are well vetted."*
**Method:** read `../2026-07-15-agent-substrate-v2/ARCHITECTURE.md` (esp. §9–§13),
`design/29-critique.md` in full, `ROADMAP.md` (esp. §5), `../2026-07-21-p1-kernel-fabric/_plan.md`
(esp. §6), `../2026-07-21-platform-architecture/DECISIONS.md` (C1–C6), and the first consumer
`../2026-07-21-p3-distributed-execution/_plan.md` + `REVIEW.md`. No code changed.

**The yardstick.** Harmonik's near-term need is narrow: agents talk to each other; tools
communicate; a **hub** exposes beads/jobs and **spoke** workers pick one up and run it, maybe on
another machine. Every element below is graded against THAT. Anything serving the older
"centralize and search all agent memory across the fleet" goal is marked out-of-scope-for-now even
where it is well designed.

**One reading rule.** "Measured" in the source means measured on the standalone `fleetd` + embedded
NATS mesh across three real boxes. **C6 recast the plugin↔kernel line to an in-proc Go interface
inside `internal/kernel/`.** No transport measurement has been re-run under that recast. So every
[MEASURED] claim below is evidence for the *networked* kernel the recast defers, not for the
in-memory kernel the recast builds first. This distinction decides most of the verdict.

---

## Bucket 1 — MEASURED & LOAD-BEARING (the safe foundation)

Proven on the real fleet AND needed for hub/spoke + comms. These are the parts worth keeping.

| Element | Evidence | Why it is load-bearing for harmonik |
|---|---|---|
| **No-hub full mesh survives 2-of-3 kill; a node cold-starts alone in ~28 ms with both peers dead** | ARCH §6.1 [MEASURED-21], incl. killing the real DGX on the real tailnet | The cross-machine hub/spoke must have no fatal box in the middle. This is the requirement met structurally, not by careful coding. |
| **`POINT_TO_POINT` = NATS queue groups = competing consumers, exactly one group member per message, load-balanced across boxes** ("10 jobs → dgx=5 + mini=5") | ARCH §2 transport table; §4.1 | This **is** hub/spoke. "A hub exposes jobs, a spoke picks one up" is this primitive + the dispatch plugin (C3), already thought through — not a gap. |
| **At-most-once kernel + four durability tools (journal, `message_id`, interest, liveness) → store-and-forward with dedupe and re-ship measured working** | ARCH §4.2, §8.2 [MEASURED-21 Phase 4]; the comms model decomposes into 3 kernel calls | This is what makes agent-to-agent comms reliable over a sleeping peer. It is the spine of the reliability story. |
| **Roster: local 1 s probe loop, per-observer liveness, ~6.5 s DEAD detection matched memberlist's 5.7–6.6 s** | ARCH §4.5 [MEASURED-23] | The hub needs to notice a dead worker to requeue its in-flight bead (P3 L6). Per-observer ("can *I* reach B") is the right answer for routing, not a bug. |
| **SQLite (`modernc.org/sqlite`), pure-Go, WAL, `CGO_ENABLED=0`, 227k appends/sec, human-openable file** | ARCH §4.4 [MEASURED-20] | The dispatch plugin's durable running-bead registry and comms cursors live here. Cross-compiles to the dgx with cgo off, which several rejected options cannot. |
| **The kernel/plugin boundary: opaque `[]byte` payload, no domain vocabulary; boundary test reports VOCABULARY CLEAN** | ARCH §3, §4; [MEASURED-HERE]; p1 §2 keeps it verbatim | This discipline is what keeps the kernel small and stops a bead/queue leaking in. It is the single most valuable idea to carry over, and it survives the C6 recast unchanged. |
| **One proto answers curl+JSON, binary protobuf, and stock gRPC; lints/builds/generates/cross-compiles to linux-arm64 today** | ARCH §8.1; §13 [MEASURED-HERE] | Real only for the *networked* kernel (kernel↔kernel). C6 drops this wire between plugin and kernel, so treat it as evidence the cross-machine seam is buildable, not as a property of the in-proc build. |

**Caveat on the whole bucket.** The measurements are genuine and the factual record is exceptional
(the critique fact-checked every load-bearing external claim to the line and found zero hallucinated
facts — `29-critique.md` §0, §4). But they were taken on the design C6 then changed. The in-memory
kernel that P1 builds first is trivial and safe; the cross-machine kernel that carries these
measurements is deferred behind the transport seam (Q-1) and still gated by M0 (see Bucket 2). Do
not present the distributed hub/spoke as "measured" for the recast — it is measured for its
predecessor.

---

## Bucket 2 — DEDUCED / UNBUILT / UNTESTED (real risk before relying on it)

For each: what must be true, and the cheapest way to de-risk.

1. **LOOKUP replication — the riskiest code, no upstream, and a known [DEDUCED] disk-wipe hole.**
   ARCH §11 Q2; ROADMAP M3. Everything routes through it (name resolution, host keys, agent/worker
   registry). Read-repair is sound *only if* a writer never re-issues a `(writer, revision)` pair
   with a different value; a disk wipe that restarts a node at revision 1 lets peers converge on
   wrong values. **What must be true:** the revision counter survives restarts and a wipe forces a
   fresh incarnation. **Cheapest de-risk:** it is already deferred (p1 §4 — the `Lookup` interface
   ships with a trivial in-memory impl day one; the replicated cross-node version lands only when
   dynamic worker join is scheduled). Keep it deferred. When built, adopt the roster's `boot_id` as
   the incarnation and persist the counter in SQLite (~10 lines, ARCH §13). Do not hand-roll a
   second incarnation scheme.

2. **M0 — the real macOS sleep test — was never run.** ARCH §11 Q1; ROADMAP M0 and §5 kill
   criterion #1. Three of five designs flagged it #1 and none closed it. A blackhole proxy, an
   `SIGSTOP`, and a frozen HTTP handler are not a sleeping laptop, where `utun0` goes down and the
   Tailscale IP is withdrawn — and BRIEF §1 says the sleeping laptop is the fleet's *normal* state.
   **What must be true:** the mesh re-forms on wake with no restart and no `Join()`. **Cheapest
   de-risk:** half a day plus one overnight, before any *networked* kernel code. **Important nuance
   the recast introduces:** M0 gates the networked kernel only. The in-memory kernel + dispatch
   plugin (two in-mem kernels wired by an in-process transport double, p1 §5) can be built and
   unit-tested before M0 runs. So M0 is a hard gate on the cross-machine half, not on a first build.

3. **Per-node ordering is [DEDUCED] by two authors and measured by none.** ARCH §11 Q9. Independent
   agreement is not measurement. **Risk:** a human (or a search consumer) reading a cross-machine
   conversation gets a plausible, wrong story from three unsynchronised clocks. **De-risk:** accept
   the design's own rule — `origin_seq` is per-node and is the ordering key; `origin_time` is
   display-only (ROADMAP M6 contract). Low risk for hub/spoke: dispatch needs no fleet-wide order.
   The design explicitly refuses to build one (ARCH §10), which is the correct call and also fixes a
   real harmonik bug (`bytes.Compare` over UUIDv7 in four load-bearing places sorts by clock skew).

4. **Memory bounding of a plugin is [DEDUCED to exist, untested] — and the C6 recast makes it
   worse.** ARCH §11 Q6. `RLIMIT_AS` semantics differ Linux vs macOS and were never exercised. Under
   the original subprocess model a leaking plugin only OOMs the box; under C6's **in-proc** plugins
   there is no process boundary at all, so a leaking or panicking plugin takes the **daemon** down —
   the exact failure the design rejected Yaegi for (ARCH §7.3). **De-risk:** observe RSS and report
   it now; build a real bound when a real plugin leaks. But log this as a genuine loss of the
   crash-isolation the source design sold as "you cannot get this wrong" (see Bucket 4).

5. **The probe port has no shared secret, justified by a [DEDUCED, unverified, load-bearing]
   WireGuard source-IP claim.** ARCH §11 Q10. If WireGuard does not bind source IPs to node keys as
   assumed, the liveness channel is unauthenticated. **De-risk:** verify the claim, or add a
   pre-shared key to the probe. Cheap, and only matters once the networked kernel exists.

6. **Consumer ground truth is not what the plan says.** P3 REVIEW R1: the "real NOW" substrate
   (lima `fleet` VM, incus, `agent-golden` image, hand-launched container) **does not exist on the
   box** — `limactl list` shows one stopped `test` instance and `which incus` fails. **Risk:**
   "buildable now with near-zero new transport code" (P3 §5) overstates readiness — the container
   execution path is unbuilt and unproven, so a first container run is not a real green until the
   worktree-in-container behaviour is re-verified from scratch (do not trust any prior "closed"
   status on it). **De-risk:** treat substrate provisioning as unscoped work before trusting any
   container end-to-end. This is a P3-consumer risk, not a kernel flaw, but it is the ground the
   hub/spoke actually stands on.

7. **Warm-cache economics are unvalidated** (P3 §6.3) — decides whether per-run ephemeral containers
   survive at all. Not a kernel risk; a delivery risk for the spoke side. Measure single-container
   throughput before committing to per-run-ephemeral.

Lower-priority untested items, listed so they are not lost: `PingInterval=2s` flap under packet loss
(ARCH §11 Q3), the `Deliver`/request deadline chosen by analogy (Q7), whether `buf breaking` at
PACKAGE erodes (Q8). None block a first build.

---

## Bucket 3 — ALREADY DROPPED / OVER-ARCHITECTED for our need (do NOT pull in)

Each is genuinely cut in the current framing, and resurrecting it would be a mistake.

| Not for us | Confirmed cut by | Why it would be a mistake to pull in |
|---|---|---|
| **The "centralize all agent learning, make it searchable" premise as the reason to build** | p1 §6 DROP #1, #4 | This is the fleet-memory goal, not the hub/spoke + comms need. P1 exists to be a generic control-plane fabric; the anecdotes must not drive its requirements. |
| **logtail + archive + search backbone (M6)** | p1 §6 DROP #2 | The purest expression of the dropped premise. Not a substrate concern. Search is a later consumer that attaches by reading journals; the kernel must never gain a search-shaped field (ARCH §10). |
| **registry plugin's "fleet-wide view of what every agent is doing" *purpose*** | p1 §6 DROP #3 | The LOOKUP *mechanism* is reused for the worker/agent address book; the shared-activity-*view* is premise-flavored. Keep the mechanism, drop the purpose. |
| **go-plugin subprocess + gRPC live-reload machinery, the ~40-line drain gate, sha256-verify-before-exec, pre-warm, `ReattachConfig`** | C6 (in-proc); p1 §6 RECAST; Q-4 | All premised on the subprocess model C6 replaced. Live reload is an accepted casualty. Reintroducing any of it reopens the whole in-proc-vs-process boundary. |
| **The plugin library (M8) — sync manifests, fetch+verify bytes per platform** | C6 removes its host (subprocess plugins); ROADMAP M8 is itself gated on "is `scp` actually annoying yet?" | With in-proc plugins there are no separate binaries to sync. It was the plugin "most likely to be a mistake" even in the source design and the one power that could break all three boxes at once. |
| **FANOUT as a fifth channel type** | ARCH §9 row 7, §4.1 | Zero consumers once both roster designs reject roll-call. Both readings of "fanout" already ship (broadcast=PUBSUB, work-queue=POINT_TO_POINT). |
| **`STATE_LEFT` / `KIND_LEAVE` liveness states** | ARCH §9 row 2 | "Left" is not mechanically detectable; it is silently wrong exactly when the shutdown hook did not fire. One `DOWN` state + a `reason` is correct. |
| **memberlist / SWIM in the kernel** | ARCH §9 row 1 | At n=3, `k=0` — the suspicion-confirmation machinery you import it for no-ops. 102 modules, a 1400-vs-1280 MTU landmine, and two panic sites for nothing. Hand-rolled ~400-line prober wins below ~20 boxes. |
| **JetStream / Raft / quorum; ZeroMQ; mangos; mTLS/NKeys/ACLs; a supervision DSL; WASM/Yaegi/stdlib `plugin`** | ARCH §6, §7.3, §10 | All argued down with measurement. Note the current seed's own steer already forbids Go's `plugin` package (00-seed "Important Go steer"), which agrees. |
| **The ssh plugin (M7)** | not dropped, but out-of-scope-for-now | A real fleet convenience (verified connectivity, host-key distribution) but not needed for the first hub/spoke + comms build. Defer without prejudice; Tailscale-SSH Phase 0 is a five-minute human action that deletes most of it anyway. |

---

## Bucket 4 — CONFLICTS between the two eras (substrate-v2 vs the P1 recast)

Where the source design and the locked P1 / C1–C6 disagree, the winner and the consequence.

1. **Plugin model — subprocess + gRPC + go-plugin (substrate-v2 §3, §7) vs in-proc Go interface
   (C6).** **Winner: C6, in-proc.** `internal/kernel/`, one surface, no second private host API,
   mocks cleanly. **Consequences, and a sanity-check of each:**
   - *Live reload dies.* Q-4 accepts this. Sanity-check: this was Greg's explicit BEAM wish ("having
     to stop the whole system every time is so annoying") and the source design's **headline "money
     demo"** (ROADMAP M1 demo #2, live reload under load, zero loss). Trading it away for fewer
     deploy artifacts is a real regression against a stated desire — but the operator locked C6
     himself, and in-proc restart of a single embedded binary is cheap. **Acceptable, but do not
     let anyone re-sell live reload as a feature of this build; it is gone.**
   - *Crash isolation dies.* The source design sold OS-process isolation as "you cannot get this
     wrong" (ARCH §7.1) and rejected Yaegi precisely because an in-proc panic kills the daemon (ARCH
     §7.3). C6 chooses exactly that in-proc model. **This is under-examined in the P1 plan** — Q-4
     mentions the reload loss but not the isolation loss. Flag it (also Bucket 2 item 4).
   - *Push-vs-pull and the drain gate dissolve.* Under in-proc `Subscribe` returns a Go channel the
     plugin holds; there is no kernel-held subscription to drain. The whole §9-row-5 / critique
     §2.2 argument is moot. Clean simplification.

2. **Transport — "embed core NATS now, it is measured" (ARCH §6 verdict) vs "defer the product
   choice behind the 6-method seam, in-memory first" (p1 Q-1).** **Winner: P1, defer.** Consequence:
   the 2-of-3-kill survival evidence is for NATS specifically; the in-memory-first path has no
   cross-machine story yet and none re-measured under the recast. Fine for a first single-binary
   build; but the cross-machine hub/spoke the operator named is **contracted, not demonstrated** (p1
   §3 "honest scope line"). The operator also still owes a yes/no on overruling his "I strongly lean
   away from NATS" (ARCH §6.1) — deferred with Q-1, so not blocking now.

3. **Scope — fleet shared-memory (substrate-v2 BRIEF §2) vs generic control-plane fabric (p1 §1).**
   **Winner: P1, generic control plane.** The substrate-v2 problem statement is explicitly *not*
   P1's reason to exist (p1 §6 DROP #4). This matches the operator's actual yardstick: carry
   messages and jobs, do not centralize searchable memory.

4. **Streaming — gRPC server-streams (substrate-v2) vs Go channels + cancel func (p1 §2).**
   **Winner: Go channels.** Idiomatic in-proc, mocks cleanly. Trivial and correct.

5. **Public surface — the seed's 3-verb `Bus` + `Service`/`Mount` in `libs/transport`
   (00-seed, 02) vs the full `Kernel` interface + `Plugin` lifecycle in `internal/kernel/`
   (p1 §2).** **Winner: P1's full Kernel** — already resolved by `05-CORRECTION`. The three-verb Bus
   is under-scoped: it omits Lookup, Roster, Resources, and namespace-scoped identity, and it names
   the package "transport," which is one subsystem of the kernel, not the whole. Hub/spoke
   competing-consumers is `POINT_TO_POINT` + the dispatch plugin, not a gap to fill later.

**Net:** every era-conflict resolves toward the P1 recast, and each resolution is defensible. The
only one that trades away something the operator explicitly wanted is #1 (live reload + crash
isolation), and it is the one to keep visible rather than buried.

---

## Bucket 5 — UNRESOLVED OPERATOR QUESTIONS the design leaves open

| Question | Source | Blocks a first build? |
|---|---|---|
| **Program framing: is this a restart of P1/P2/P3, or a fresh start that supersedes it?** Do we revive the kerf works and re-ratify C1–C6, or start clean? | 05-CORRECTION close | **YES — this is the one that actually blocks.** Everything downstream (which plans are live, what P2 already extracted, whether C1–C6 still bind) hangs on it. Put it to the operator first. |
| **Cross-machine transport: embedded NATS vs minimal owned TCP (and the standing NATS override)** | p1 Q-1; ARCH §6.1 | Blocks the **cross-machine** build only. Deferrable for an in-memory / single-binary first build. |
| **M0 real-sleep gate** | ROADMAP M0 | Blocks the **networked** kernel only, not the in-memory kernel. Half a day + one night when the cross-machine half is scheduled. |
| **Guardrail / kill-criteria ratification (Q-3)** | p1 §7; DECISIONS OQ-1 | Effectively resolved: OQ-1 reframed it as *principles referenced from AGENTS.md*, not a bolt-on package. Does not block. |
| **C5 liveness control set (L1–L9), operator sign-off on L5–L9** | DECISIONS C5/OQ-2; P3 Q5 | A P3/dispatch concern, resolved as "lean, reactive, start with timeouts." Does not block the kernel. |
| **Fanout naming — five types listed, four ship** | ARCH §11 Q5 | Moot; both readings ship. 30 seconds of the operator's time, blocks nothing. |
| **Scaffold demolition date, concurrency ceiling, ephemeral-vs-warm, security posture past "3 owned boxes"** | P3 §7 Q1–Q4 | P3-delivery judgment calls, not kernel questions. Do not block a first kernel build. |

---

## VERDICT

**Adopt P1's recast (`p1-kernel-fabric/_plan.md` + C1–C6) as the design of record. Treat raw
substrate-v2 as reference and evidence only — not as the thing to build.** That is the honest
framing and it is what `05-CORRECTION` already leans toward; this ledger confirms it against the
yardstick.

P1 is sound and correctly scoped for harmonik's actual need. The kernel/plugin boundary, the four
channel types, `POINT_TO_POINT` competing-consumers for hub/spoke, at-most-once + store-and-forward
for comms, the local-probe roster, and box-local SQLite state are a genuine, well-reasoned
foundation, and the factual record behind them is unusually strong. The in-memory kernel plus the
dispatch plugin can be built and unit-tested **now**, with no gated risk, because the only hard
gates (M0, the transport product choice) sit on the cross-machine half that P1 deliberately defers.

It is **not** yet a "measured" basis for the *distributed* hub/spoke. Every transport measurement
belongs to the predecessor design; none has been re-run under the in-proc recast, and the
cross-machine property is contracted, not demonstrated. Do not let the distributed story inherit the
word "measured."

**The three things most likely to bite if we build on this:**

1. **C6's in-proc choice quietly removes crash isolation, not just live reload.** A buggy or leaking
   in-proc plugin can take the whole daemon down — the exact failure the source design rejected
   Yaegi for. The P1 plan records the reload loss (Q-4) but not the isolation loss. Decide
   deliberately whether that is acceptable, and if so, put back a lightweight bound (RSS observation
   now, a real limit when a plugin misbehaves).
2. **LOOKUP replication is the riskiest code, has no upstream, and carries an unbuilt disk-wipe
   convergence hole.** Keep it deferred (its interface ships with a trivial in-memory impl); build
   the replicated version only when dynamic worker join is scheduled, and use the roster's `boot_id`
   as the incarnation. Do not let a first cross-machine push force it early.
3. **The ground the consumer stands on is not real yet.** M0 has never been run, so the "laptop is a
   peer" premise is unproven for a genuine sleeping laptop; and P3's substrate (the `fleet` VM /
   incus / golden image) does not exist, and the container execution path is unbuilt. "Buildable
   now" is true for the in-memory kernel and overstated for the cross-machine container path.

**The one operator question that blocks starting:** is this a re-grounding of the stalled P1/P2/P3
program or a fresh start that supersedes it? Answer that before spawning build crews; the rest are
deferrable.
