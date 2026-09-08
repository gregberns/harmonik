# Harmonik Bus — Execution Plan

**Date:** 2026-09-07 · **Track:** 2 of 2 (sibling: `../2026-09-07-harmonik-restructure/`)
**Status:** Executable, phased, tasked. Turns `01-base-plan.md` into work a captain or crew can run.
**Inputs:** `00-seed-bus.md` (the contract), `01-base-plan.md` (the grounding — authoritative), and
the prior program it cites: `plans/2026-07-21-p1-kernel-fabric/_plan.md`,
`plans/2026-07-21-platform-architecture/DECISIONS.md` (C1–C6 locked),
`plans/2026-06-30-distributed-fleet/01-p2p-comms/README.md`.

> This plan does not re-open the design. The design is settled in `01-base-plan.md` §3 and in the
> prior program. This plan states the order of work, the exit gate of each phase, the reviewer
> check at each gate, and an ordered task list. It changes no code. Every non-obvious claim cites a
> plan file or a package by name.

---

## Reconciliation with the restructure track (2026-09-07) — READ FIRST

An adversarial review (`03-review.md`, verdict REWORK) found this plan and its sibling
(`../2026-09-07-harmonik-restructure/02-execution-plan.md`) deadlocked: this plan pinned its join point
to `libs/transport`, `libs/cli.Env.Bus`, and `tools/keeper`, none of which the restructure's execution
plan builds. The restructure builds ONE `clean/` module and defers the `libs/` + `tools/` split to
END-STATE. The two same-day siblings could not both be right. The reconciliation, now written into
BOTH plans:

- **The bus lands FIRST as packages INSIDE the restructure's single `clean/` module.** The bus surface
  (`Bus` / `Service` / `Message` / `Mount` + the in-memory router) lands as **`clean/transport`**. The
  shared env seam (carrying `Env.Bus`) lands as **`clean/cli`**. NOT a separate `libs/` module yet.
- **`libs/transport` + `tools/keeper` + the separate-module split stay the END-STATE promotion**,
  deferred until the clean module is proven — exactly as the restructure defers every other package.
- **What the bus needs from the restructure is only its Phase-0 scaffold:** the `clean/` module,
  `go.work`, and the one-way wall (restructure task 10 done). The bus builds `clean/transport` and the
  minimal `clean/cli` it needs itself; it does not wait on a `libs/cli` the restructure never plans.
- **Keeper is NOT the bus's first Service.** Keeper is the restructure's late, spike-gated Track-B tail
  (it must first sever `digest` AND `presence` AND `dashboard`). The bus proves itself on trivial stub
  services first; keeper becomes a bus `Service` only AFTER Track B lands `clean/keeper` (§3).

Every `libs/transport`, `libs/cli`, `tools/keeper` reference below has been retargeted to
`clean/transport`, `clean/cli`, and the post-Track-B keeper. Both plans now name the SAME paths and the
SAME keeper sequencing.

---

## 0. Decisions to ratify up front

Each decision below carries a recommended default so work can start today. Each is
operator-overridable. None blocks Phase 0 — Phase 0 is pure contract design and holds under every
default. Mark an override before Phase 1 lands code.

### D1 — Public name: `Bus` in `clean/transport` (recommended default)

The seed and the restructure playbook say **bus**; the platform program (`p1-kernel-fabric/_plan.md`)
says **kernel / fabric**. They are the same architecture under two names (`01-base-plan.md` §6.6).
**Recommended default:** the public surface is `Bus` in **`clean/transport`** — a package inside the
restructure's single clean module (see §Reconciliation), NOT a separate `libs/transport` module, which
is the END-STATE promotion the restructure defers. The interface doc names `kernel/fabric` as the
design lineage, so a later reader does not think there are two systems, and notes that P1's
dep-allowlist / no-domain-noun guardrail machinery (P1 §5, written against `internal/kernel`) now lives
against `clean/transport` so the D4 guardrail does not evaporate in the move.
*Override path:* the operator picks `kernel` / `internal/kernel` instead. Low cost to flip before
Phase 1; high cost to run both names in parallel.

### D2 — Defer hub/spoke distribution (point-to-point + State + Roster) as ONE unit (recommended default)

The seed's three verbs (Publish / Subscribe / Request) do **not** carry competing-consumer work
distribution: `Subscribe` copies to every subscriber, `Request` asks one responder
(`01-base-plan.md` §3.5). Hub/spoke job dispatch needs "exactly one of N spokes takes this job",
which is the prior program's fourth channel type — `ChannelPointToPoint`
(`p1-kernel-fabric/_plan.md` §2). **Recommended default:** ship the three verbs first, and build **NO
hub/spoke distribution of any kind in the first slices** — not even an interim pull model. The review
(`03-review.md` finding 3) showed the interim "spoke `Request`s, hub `Serve`s one job" model violates
two locked decisions: **C5** (a dead spoke strands its job — the interim has no durable lease) and
**C2** (building a pull dispatcher and later rewriting it to competing-consumers is the
"temporary-pipe-then-migrate" C2 bans). Real hub/spoke needs point-to-point AND the durable lease in
`Resources.State()` AND worker liveness in `Roster` — **all three deferrals fire together, they cannot
land alone** (P1 §4). §5 now describes only the END-STATE mapping, explicitly marked deferred, with the
single trigger below. Keep command and event subjects distinct so the later addition is additive.
*Trigger to pull the unit forward:* dispatch becomes a real bus service (a hub must hand exactly one of
N spokes a job across a process boundary). *Override path:* the operator wants competing-consumer
distribution in the first slice — that pulls point-to-point + State + Roster forward together and grows
the first `Bus` past three verbs.

### D3 — The `Bus` is at-most-once by contract; durability is a service concern (recommended default)

The prior kernel is at-most-once, always: it never queues, never retries, never redelivers
(`p1-kernel-fabric/_plan.md` §1 "Not a delivery guarantor"). Today's `comms` builds at-least-once +
`event_id` dedupe (N3) *on top* of that, from the event journal (`01-base-plan.md` §1). The seed says
nothing about delivery guarantees. **Recommended default:** state in the interface doc that the `Bus`
is at-most-once — it does not retry, and a service that needs stronger delivery builds it from a
journal, a stamped message id, and `event_id` dedupe, exactly as `comms` does today. This keeps the
bus small and honest.
*Override path:* the operator wants the in-memory `Bus` to inherit a durable journal. Not
recommended — it hard-wires a durability policy into every subject and every service.

### D4 — (program-wide, inherited) The boundary guardrail is a principle, not a bolt-on

The one operator question the platform program left open is ratifying the boundary / kill-criteria
guardrail (`DECISIONS.md` OQ-1; `p1-kernel-fabric/_plan.md` Q-3). The review already **reframed it as
principles** folded into the platform building principles (`DECISIONS.md` §"Building principles"), not
a separate construct. **Recommended default:** adopt the four checks as principle-backed signals —
the `[]byte`-only payload, no domain noun in `clean/transport`, a payload ceiling (order 256 KB) that
makes C1 a mechanism not a rule, and "two services needing a new bus verb = stop and re-examine the
boundary". Reference them from the interface doc. This is one yes that also covers P1/P3.
*Override path:* the operator wants hard mechanical laws instead of principle-backed signals. Runs
against the operator's own principles-not-laws direction.

---

## 1. Phase 0 — Design freeze (starts NOW, before restructure scaffolding)

**Goal:** freeze the public contract and prove it with a working prototype, in a scratch package,
before `clean/` exists. This has **no restructure dependency** (`01-base-plan.md` §5) — the contract
depends on no legacy code.

**Where:** a scratch/prototype package, e.g. `internal/transportproto/` (throwaway; a later `git mv`
carries the frozen files into **`clean/transport`** once the clean module exists, per the playbook's
move-don't-copy loop). Do **not** create `clean/` yet; that is the restructure's Phase-0 job. The
scratch package stays where it is until the clean module exists, then moves into `clean/transport` (NOT
`libs/transport`).

**What to nail (the whole public contract):**

- **`Bus`** — `Publish(ctx, subject, msg []byte) error`,
  `Subscribe(ctx, subject, Handler) (Subscription, error)`,
  `Request(ctx, subject, msg []byte) ([]byte, error)`. Three verbs, no more (`00-seed-bus.md`
  §"The two interfaces"; `01-base-plan.md` §3.1). `Serve`/`Respond` from the prior `Transport` are the
  reply-side mechanism `Mount` uses internally to answer a `Request` — a service never sees them.
- **`Message`** — `Subject`, `Reply` (set on requests; publish here to answer), `Data []byte`,
  `Header map[string]string` (correlation id, `content-type`, trace). Payload stays `[]byte`; a
  `content-type` header lets a subject carry protobuf/msgpack later without breaking callers
  (`00-seed-bus.md` §"Payload format").
- **`Handler`** = `func(ctx, Message) error`. **`Subscription`** = `Unsubscribe() error`.
- **`Service`** — `Name() string` (stable identity, e.g. `"keeper"`; this *is* the namespace by
  another name, `01-base-plan.md` §3.1), `Subjects() []string`, `Handle(ctx, Message) ([]byte, error)`.
  Start minimal: no `Start`/`Stop`/`Health` yet. Recommend `Mount`-does-the-subscribe for the first
  slice rather than a `Service.Start(ctx, bus)` (`01-base-plan.md` §6 Q-2, low stakes).
- **`Mount(ctx, b Bus, s Service) (Subscription, error)`** — subscribe `s.Handle` to each of
  `s.Subjects()`; on a request, publish the returned payload to `m.Reply`. One helper, works for
  every service (`00-seed-bus.md` §"Service").
- **Subject convention** — dotted `<tool>.<noun>.<verb>` for commands (used with `Request`),
  `<tool>.<noun>.<pastTense>` for events (used with `Publish`); wildcard family subscribe
  (`task.exec.*`). Adopt verbatim from the seed (`00-seed-bus.md` §"Subjects"). Add one rule from the
  kernel side: keep command subjects and event subjects distinct in the namespace, so a later move to
  explicit channel types (point-to-point) is additive (`01-base-plan.md` §3.2).
- **The one hard line, written into the doc** — the bus moves opaque `[]byte`; it never parses a
  payload; it has never heard of a bead, a run, a queue, or an agent; git stays the artifact plane
  (C1); a payload ceiling (order 256 KB) makes that a mechanism (`01-base-plan.md` §3.7,
  `p1-kernel-fabric/_plan.md` Q-5). State that the `Bus` is at-most-once (D3).

**The in-memory subject router (the prototype):**

- A subject router in RAM. `Publish` fans out to every matching subscriber; `Subscribe` registers
  interest with dotted-wildcard matching (`task.exec.*`); `Request` mints a `Reply` subject, delivers
  to one responder, and returns its reply payload (`01-base-plan.md` §4). No serialization, no
  network. Model it on the prior in-memory kernel — the default test double the whole design leans on
  (`p1-kernel-fabric/_plan.md` §5).
- Wildcard matching, subscriber fan-out, and request/reply correlation are the three router behaviors
  that need real tests. Concurrency is real (fan-out runs handlers); keep the first cut simple and
  race-clean under `go test -race`.

**Exit gate (all must hold):**

1. The `Bus`, `Service`, `Message`, `Handler`, `Subscription` interfaces + `Mount` signature are
   written and **reviewed** (reviewer check §6).
2. The in-memory bus prototype compiles and passes its own unit tests: publish fan-out, wildcard
   subscribe, request/reply round-trip, unsubscribe.
3. A **cross-service** test passes: mount two trivial services on one in-memory bus and let them talk
   — one `Request`s a subject the other serves and gets the reply; and a `Publish` on an event
   subject reaches a subscriber. This is the proof the seam works before any second binary or any
   network exists (`01-base-plan.md` §4 step 5).
4. The interface doc records D1–D4 as applied (or the operator's override), and states the one hard
   line and the at-most-once contract.

**Nothing in Phase 0 lands in `clean/`.** It is scratch, proven, and ready to move.

---

## 2. Phase 1 — Land in the clean room

**Goal:** move the frozen contract + in-memory bus into `clean/transport` (a package in the
restructure's single clean module), behind the restructure's one-way wall, and wire the composition
seam as `clean/cli`.

**This is the join point with the restructure track. The exact dependency (see §Reconciliation and
restructure `02-execution-plan.md` §Phase 0):**

- **Restructure must have delivered first, and this is ALL that is needed:** the `clean/` module +
  `go.work` + the one-way wall check (restructure task 10 — the wall is the allow-list closure check:
  anything under `github.com/gregberns/harmonik/` that is not under `clean/` is a breach). `clean/`
  does not exist until then.
- **The bus builds its own composition seam:** `clean/cli` carries the `Env` struct with
  `Bus transport.Bus`. The restructure reserves the path and does not build a `libs/cli`; this plan
  lands the minimal `clean/cli` the bus needs. Keeper (§3, after Track B) is the first tool `Run` that
  is driven with `Env.Bus`.

**Work:**

1. `git mv internal/transportproto → clean/transport` (move, do not copy — preserve history, the
   playbook's port loop). Rewrite the import path; run `goimports -w`. Its tests move with it and run
   in the clean module's fast suite.
2. Confirm the wall: `cd clean && go list -deps ./transport/... | grep '^github.com/gregberns/harmonik/'
   | grep -v '^github.com/gregberns/harmonik/clean/'` returns nothing. `clean/transport` imports **no**
   tool and no legacy — it is the shared vocabulary layer, and the same wall check the restructure
   already wired protects it.
3. Add `clean/cli` with `Env.Bus transport.Bus`. The umbrella constructs `transport.InMem()` and hands
   it to every tool's `Run` (playbook §"Composition seam"). No NATS yet.

**Exit gate:**

1. `clean/transport` lives in the clean module, the wall check is green, its unit + cross-service tests
   pass in the isolated fast suite.
2. `clean/cli.Env` carries `Bus transport.Bus`; a tool `Run(ctx, cli.Env{Bus: transport.InMem()})`
   compiles and runs.
3. No second bus exists: confirm there is one `Bus` interface in `clean/transport` and nothing under
   `internal/` re-declares it (guards against the D1 two-names risk, `01-base-plan.md` §6.5).

---

## 3. Phase 2 — Prove the seam on STUB services; keeper is a later, Track-B-gated slice

**Why not keeper first.** The base plan and an earlier draft made keeper the bus's first real
`Service`, "a clean leaf, small blast radius." The restructure's execution plan says the opposite with
evidence (`../2026-09-07-harmonik-restructure/02-execution-plan.md` §Track B): keeper is NOT clean
today and earns its move by cuts, not relocation. Its clean move is the L-sized culmination of a
five-plus-step Track B that first severs `digest` (which drags the whole run machine), then
`dashboard`, then `presence`, routes `time.Now`, and ports keeper-relevant core types — itself gated on
the Phase-1 core-vocab spike. Betting the bus's only real Service on the single messiest keeper
refactor means that if Track B slips or the vocab spike returns RED, the bus reaches its exit gate with
no real service at all (`03-review.md` finding 2). So the bus decouples from keeper.

**Goal:** prove two services talk on one in-memory bus using **trivial throwaway stub services** — no
dependency on the keeper untangling. This is the seam proof the whole design rests on
(`01-base-plan.md` §4 step 5), and it needs nothing from any real tool.

**Work:**

1. Write two tiny **stub/echo services** in the clean module's test tree — for example a `ping` service
   that answers `test.ping.ask` with a `pong`, and a `counter` service that `Publish`es
   `test.count.ticked` and a subscriber observes it. Each implements the same `Service`
   (`Name`/`Subjects`/`Handle`) every real tool implements; neither carries domain logic.
2. Mount both on one `transport.InMem()` via `transport.Mount` and let them talk — one `Request`/reply
   across services and one `Publish`/subscribe event. One process, milliseconds, no network.
3. This exercises `clean/cli.Env.Bus` end to end: the umbrella constructs `transport.InMem()` and hands
   it to each stub's `Run`, exactly as it will hand it to a real tool later.

**Exit gate:**

1. Two stub services on one in-memory bus exchange a request/reply and an event, `-race` clean.
2. `clean/cli.Env.Bus` drives both stubs — the composition seam is proven with real `Service`
   implementations, not just the router's own unit tests.
3. No real tool is coupled to this phase; keeper is not on the critical path to the bus being proven.

---

## 3a. Later slice — keeper as a bus Service (AFTER restructure Track B lands `clean/keeper`)

This is a **deferred slice with a named trigger**, not part of the first proof. **Trigger:** the
restructure's Track B has landed `clean/keeper` — keeper's `digest`, `dashboard`, and `presence`
tentacles are severed, `time.Now` is routed, and the keeper-relevant core types are in the production
vocabulary (restructure tasks through 27). Only then is keeper a clean tool the bus can wear.

**Work when the trigger fires:**

1. Keeper's domain logic stays a **plain library** in `clean/keeper` — it does not import `transport`
   at all (`00-seed-bus.md` §"What belongs in transport vs a tool"). It is called *by* the adapter.
2. Add a thin **`NewService()` adapter** — the only part that imports `transport`. It maps subjects to
   method calls. Be honest that keeper's real surface is enable / doctor / set-dispatching (the
   `keeper` skill), not the seed's invented `keeper.session.restart` / `keeper.session.grew` examples;
   name the subjects from what keeper actually does, and if keeper has no natural request/reply
   counterpart, the adapter is packaging, not a "two tools that needed to talk" proof.
3. Mount keeper on the umbrella's in-memory bus and confirm the standalone binary
   (`go build ./tools/keeper/cmd/keeper`, an END-STATE promotion) and the embedded `harmonik keeper …`
   subcommand both call the same `keeper.Run` (playbook §"One entrypoint").

---

## 4. Deferred slices, with named triggers

Each slice below is **already designed** in the prior program. Do not re-derive it under time
pressure — the trigger points at the exact prior doc (`01-base-plan.md` §4, §6.7). "Two services
needing a capability the bus lacks" is the signal to add it (the boundary test, D4); one speculative
addition is the smell the platform principles warn against.

| Slice | Trigger to pull it forward | Roughly what it takes | Design source |
|---|---|---|---|
| **NATS bus** | A real cross-machine run is scheduled (C2; `p1-kernel-fabric` Q-1). | A second `Bus` impl behind the same interface — `transport.DialNATS`. Tool code is byte-for-byte identical (`00-seed-bus.md` §"Distributed"). Product choice (embedded NATS vs owned TCP) sits behind the 6-method transport seam and is not part of the contract services approve. | `A2-substrate-v2-architecture.md`; `p1-kernel-fabric` §2 "Kernel↔kernel boundary". |
| **Runtime registration** (`_harmonik.register`) | The first out-of-process tool or agent needs to be routed to *without* the umbrella importing it. | A tiny registry lib subscribed to `_harmonik.register` — ordinary code on ordinary subjects, tracks live services + heartbeats + expires the dead (`00-seed-bus.md` §"Registration"). This is a LOOKUP concern; plugin↔bus stays in-proc (C6). | `00-seed-bus.md` §"Registration"; `01-base-plan.md` §3.4. |
| **Roster / LOOKUP** (`Roster.List/Watch`, replicated address book) | Dynamic worker join is scheduled (the join protocol; `p1-kernel-fabric` §4). | The `Lookup` interface may ship trivially in the in-memory bus; the **replicated** implementation + a consumer land when ephemeral join/leave is real. `internal/presence` covers the daemon-local need until then. | `p1-kernel-fabric` §2, §4; `01-p2p-comms/README.md` §B; `01-base-plan.md` §4. |
| **Hub/spoke distribution** = point-to-point + State + Roster (ONE coupled unit) | Dispatch becomes a real bus service — a hub hands exactly one of N spokes a job across a process boundary (D2, §5). **These three land together, not alone.** | `ChannelPointToPoint` as an **optional richer interface** (`Subscribe(pattern, group)` + `Publish(groupKey)`) + the durable lease in `Resources.State()` (so a dead spoke's job requeues, C5) + worker liveness in `Roster`. Not forced into the three-verb `Bus`. | `p1-kernel-fabric` §2, §4; `p3-distributed-execution/_plan.md`; `01-base-plan.md` §3.5; `DECISIONS.md` C5. |
| **Reconcile / absorb `comms`** (retire `SubscribeHub` + the daemon socket RPC into the bus) | The bus carries real traffic and the split between the two comms systems must close (see the honesty note below). **Depends on the State seam first** — comms' N3 at-least-once + `event_id` dedupe must rebuild on a journal the State seam owns. | Re-express `harmonik comms` (then `dispatch`) as a `Service` on the bus; retire `daemon.SubscribeHub`, the `CursorStore`, and the daemon Unix-socket `{"op":…}` RPC; fold `internal/eventbus`'s journal into the State-seam implementation. | `01-base-plan.md` §1 (bottom line), §4; `internal/eventbus`, `daemon.SubscribeHub`, `internal/dispatch`. |

**Discipline:** do not build a channel type, a roster, a registry, or a comms port the first services do
not use (`01-base-plan.md` §4). Re-consolidate from the source doc when the trigger fires; do not
re-plan. **The three hub/spoke deferrals are coupled** — the §4 table lists point-to-point + State +
Roster as one row on purpose, because none can land alone (a competing-consumer channel with no durable
lease and no liveness strands jobs, violating C5).

**Honesty note — two comms systems until the reconcile slice lands.** Standing up a NEW in-memory
subject router is the right call: the real `internal/eventbus` (`Subscribe` is boot-only, sealed after
`Seal()` per EV-009, type-keyed by `core.EventPattern`, no request/reply — confirmed in `eventbus.go`)
genuinely cannot be extended into the bus. But be plain about the consequence: until the
reconcile-`comms` slice above lands, harmonik runs BOTH planes at once — the live
eventbus / `harmonik comms` / `daemon.SubscribeHub` path carries ALL real agent traffic, and the new
bus carries only stub-test and (later) keeper traffic. This plan does not silently create two permanent
overlapping comms systems: the split is a tracked deferred slice with a named trigger, not an accident.

---

## 5. Hub/spoke mapping onto the bus — END-STATE ONLY, DEFERRED (no interim built)

The operator's model: a **hub** exposes jobs to be completed; a **spoke** picks up a job and runs with
it (`README.md` §"Why this exists"). This section is **forward design, not a "works today" path a crew
builds in the first slices.** Real hub/spoke is the coupled deferred unit in §4 (point-to-point + State
+ Roster), pulled forward only by its trigger. **Nothing in §1–§3 builds any hub/spoke distribution.**

**Why no interim.** An earlier draft recommended an interim "pull" model — a spoke `Request`s work and
the hub `Serve`s one job in reply — to model hub/spoke on the three verbs today. The review
(`03-review.md` finding 3) showed it is unbuildable as a real dispatcher without violating locked
decisions:

- **It breaks C5.** Over an at-most-once bus (D3), the hub hands job X to a spoke, the spoke dies, and
  nothing requeues it — there is no durable lease. C5 makes requeue-on-death a non-skippable minimum.
  The lease lives in `Resources.State()` (the deferred State seam) and worker liveness in `Roster`
  (also deferred), so the interim is only safe single-process, where a spoke cannot independently die.
- **Hub-has-no-job is unspecified.** With `Request`/`Serve`, a spoke asking an empty queue forces
  either long-poll (the hub parks open requests — real, unbuilt machinery) or return-empty-and-retry
  (polling latency). Neither is designed, so the interim is not actually complete.
- **The rewrite breaks C2.** Pull (`spoke Requests`, `hub Serves one`) and competing-consumers (`hub
  Publishes groupKey`, `spokes Subscribe(group)`) are different control flows — the interim dispatcher
  is throwaway. Building it and later rewriting it to point-to-point is exactly the
  "temporary-pipe-then-migrate" C2 forbids ("that is how the ssh model happened").

So the interim is removed. Hub/spoke lands when point-to-point + State + Roster land **together** (§4,
one coupled trigger, P1 §4).

**How dispatch works today (unchanged, and the bus does NOT rebuild it in the first slices):** the
daemon owns the central queue (`internal/queue`, the "one brain decides what runs" plane — locked, C3
concern B). A queue-to-run handoff is a `dispatch.Intent` value (`internal/dispatch`), persisted by
`internal/dispatchstore`. Dynamic fan-out to subscribers is `daemon.SubscribeHub` + `CursorStore`;
delivery is at-least-once with client dedupe on `event_id` (`01-base-plan.md` §1). This plane keeps
carrying ALL real dispatch traffic until the reconcile-`comms`/dispatch slices land (§4). The hub stays
authoritative regardless of which mechanism ever delivers the job (C3: hub owns the queue).

**The END-STATE mapping (deferred, for design continuity only):** the hub `Publish`es on a
`task.exec.run` point-to-point channel with `group="workers"`; spokes `Subscribe(group)` as competing
consumers and pull one each; status flows back on a `task.exec.completed` PUBSUB channel; the durable
lease / in-flight state (so a hub restart does not strand `in_progress`, C5) lives in the dispatch
service's `Resources.State()` — the reserved seam (§4). A future `queue` service exposes
`internal/queue` on `queue.task.enqueue`. This is exactly P3's decomposition
(`p3-distributed-execution/_plan.md`; `p1-kernel-fabric` §4), reached with no new bus verb beyond the
one optional interface.

---

## 6. Review gates and task breakdown

### Per-phase reviewer check and pass bar

**Phase 0 — Design freeze.** Reviewer confirms: the `Bus` is exactly three verbs (no `Serve`/`Respond`
leaking onto the public surface); `Message.Data` is `[]byte` and `Header` carries `content-type`; the
subject convention is the seed's verbatim; the doc states the one hard line (opaque bytes, no domain
noun, git is the artifact plane, at-most-once) and records D1–D4. The prototype's three router
behaviors (fan-out, wildcard, request/reply) each have a test, and the cross-service test passes under
`-race`. **Pass bar:** interfaces reviewed + in-memory prototype green + cross-service test green.

**Phase 1 — Land in clean room.** Reviewer confirms: history preserved (`git mv`, not copy); the wall
check is green (`clean/transport` imports no tool, no legacy — the allow-list closure check); one `Bus`
interface exists project-wide; `clean/cli.Env.Bus` compiles and an injected `transport.InMem()` drives
a `Run`. **Pass bar:** package stands in the clean module, wall green, no second bus.

**Phase 2 — Stub-service seam proof.** Reviewer confirms: two trivial stub services (each a real
`Service` with no domain logic) mount on one `transport.InMem()` and exchange a request/reply and an
event, `-race` clean; `clean/cli.Env.Bus` drives both; no real tool is coupled to this phase. Watch for
the unwanted-abstraction smell: a stub that grew logic, or a `Service` that carries state the slice does
not use. **Pass bar:** two stubs talk on one bus + seam driven through `Env.Bus`.

**Later slice (§3a) — Keeper service, after Track B lands `clean/keeper`.** Reviewer confirms: keeper's
domain library does not import `transport` (the adapter is the only importer); the `NewService()`
adapter maps subjects to methods and nothing more; both binary forms call the same `keeper.Run`. The
subjects are named from keeper's real surface, not the seed's invented examples. **Pass bar:** keeper
answers on the bus + both binary forms build.

All non-trivial commits carry the review trailer per project rules; `make full` is the merge decision.

### Ordered task list

A crew can pick these up top to bottom. Size is rough (S ≤ half a day, M ≈ 1–2 days, L ≈ several).

| # | Outcome | Size | Depends on | Phase |
|---|---|---|---|---|
| T1 | Interface doc: `Bus`/`Service`/`Message`/`Handler`/`Subscription`/`Mount` written; D1–D4 recorded; the hard line + at-most-once stated. | M | — | 0 |
| T2 | Scratch package `internal/transportproto` with the interfaces from T1 (compiles, no impl yet). | S | T1 | 0 |
| T3 | In-memory subject router: `Publish` fan-out, wildcard `Subscribe`, `Request`/reply correlation, `Unsubscribe`. Unit tests, `-race` clean. | M | T2 | 0 |
| T4 | `Mount` helper: subscribe `Handle` to each `Subjects()`; answer a request to `m.Reply`. | S | T3 | 0 |
| T5 | Cross-service test: two trivial services on one in-mem bus exchange request/reply + event. **Phase 0 exit gate.** | S | T4 | 0 |
| T6 | (restructure prereq — track its landing) `clean/` module + `go.work` + the allow-list one-way wall check exists (restructure task 10). | — | restructure task 10 | 1 |
| T7 | `git mv internal/transportproto → clean/transport`; rewrite imports; tests move and pass in the fast suite; wall check green. | M | T5, T6 | 1 |
| T8 | Add `clean/cli.Env.Bus transport.Bus`; umbrella constructs `transport.InMem()` and injects it. **Phase 1 exit gate.** | S | T7 | 1 |
| T9 | Two trivial stub/echo services in the clean test tree (each a real `Service`, no domain logic). | S | T8 | 2 |
| T10 | Stub-service seam proof: two stubs on one `transport.InMem()` exchange a request/reply and an event, `-race` clean, driven through `clean/cli.Env.Bus`. **Phase 2 exit gate.** | S | T9 | 2 |
| T11 | *(Later slice §3a — TRIGGER: restructure Track B has landed `clean/keeper`.)* Keeper `NewService()` adapter over the clean keeper library; subjects named from keeper's real surface; only file importing `transport`. | M | T10, restructure task 27 | §3a |
| T12 | *(Later slice §3a.)* Mount keeper on the umbrella's in-mem bus; `harmonik keeper …` and the standalone binary both call `keeper.Run`. | S | T11 | §3a |

T9–T10 (the seam proof) depend on NOTHING from keeper — they are the bus's real exit gate and cannot be
blocked by the keeper untangling. T11–T12 are the §3a later slice, gated on restructure Track B landing
`clean/keeper` (restructure task 27). Everything past that — the NATS bus, runtime registration,
hub/spoke distribution (point-to-point + State + Roster as one unit), and the reconcile-`comms` slice —
is a deferred slice in §4, pulled forward only by its named trigger.

---

## Appendix — source map

| Artifact | What it carries for this plan |
|---|---|
| `00-seed-bus.md` | The public contract: `Bus`/`Service`/`Mount`, subjects, in-mem vs NATS, payload format, "never Go `plugin`". |
| `01-base-plan.md` | The grounding: what exists today, the reconciled design (§3), the fundamentals to build first (§4), the restructure dependency (§5), the open decisions (§6). Authoritative. |
| `../2026-09-07-harmonik-restructure/00-seed-playbook.md` | The clean-room module, the one-way wall, the port loop, one-entrypoint tools. Its `libs/transport` + `libs/cli.Env.Bus` are the END-STATE names; this plan lands them as `clean/transport` + `clean/cli` first (see §Reconciliation). |
| `../2026-09-07-harmonik-restructure/02-execution-plan.md` | The sibling execution plan this one reconciles with: the single `clean/` module, the allow-list wall, keeper as the late Track-B tail, the reserved `clean/transport` + `clean/cli` join point. |
| `plans/2026-07-21-p1-kernel-fabric/_plan.md` | The in-proc kernel interface (Transport/Lookup/Roster/Resources + Plugin); the four channel types; the minimal slice + deferrals; at-most-once. |
| `plans/2026-07-21-platform-architecture/DECISIONS.md` | C1–C6 locked; the four building principles; the guardrail (D4). |
| `plans/2026-06-30-distributed-fleet/01-p2p-comms/README.md` | The three-concern split (membership / scheduling / transport); hub/spoke-with-central-queue; the join protocol (roster/LOOKUP trigger). |
| `internal/eventbus`, `internal/queue`, `internal/dispatch`, `daemon.SubscribeHub` | Today's coordination code the bus builds on: the journal, the central queue, the dispatch value model, the dynamic fan-out. |
