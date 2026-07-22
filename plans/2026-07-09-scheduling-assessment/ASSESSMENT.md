# Scheduling-Internals Assessment (2026-07-09)

> Built by the admiral from a 3-agent read-only fan-out over the live dispatch code, config,
> and all scheduling-adjacent plans. Question posed by operator: as harmonik grows to dispatch
> across local + remote machines using a mix of models (Claude / Codex / Pi-DGX / Pi-OpenRouter),
> each with different capacity and cost, we are entering a classic constrained-scheduling regime.
> What internals would a real scheduler require, and what do we have today?

## TL;DR

Harmonik has the **hooks** a scheduler would plug into, but almost none of the **signals** a
scheduler consumes. Dispatch today is a **flat counter-based admission loop**: it walks queues
round-robin and admits a bead if two integer counters (a local cap + one remote worker's slot
count) have room. Model/harness is resolved *after* placement and *independently* of it. There is
no per-box load input, no per-(box,model) capacity, no per-bead weight, and no live per-provider
cost/quota signal. It is uniform-cost bin-packing with unit-weight items onto 1 local pool + 0-or-1
remote worker.

**The objective is already effectively chosen** — MR3's stated policy is "fill Pi+local-DGX first,
then spread to Claude+Codex, auto-cut Claude when tokens run low." The work is not to invent an
objective; it's to build the **four missing signals** that policy needs, and to fold model/machine
selection into one placement decision.

## 1. How dispatch works today (the current "scheduler")

The whole decision path is one function: `runWorkLoop` in `internal/daemon/workloop.go` (~line 1459).
Per tick it does three things, all counter-based:

1. **Pick the next queue/bead** — `selectNextQueue` (`workloop.go:1357`): round-robin cursor, skips a
   queue if its local run count ≥ its per-queue worker cap.
2. **Admit or sleep** — the split capacity gate (`workloop.go:1689-1711`): admit if `localInFlight <
   max_concurrent`, OR (queue not LocalOnly AND the one remote worker has a free slot). Else sleep and
   re-tick. There is no admitted-but-waiting priority queue — pending work is re-scanned from scratch
   each tick; kerf's upstream ranking is the only cross-queue priority.
3. **Place local vs remote** — worker pre-selection (`workloop.go:2887-2906`): `SelectWorker()` returns
   the single remote worker iff `enabled && inFlight < max_slots`, atomically taking a slot; `nil` →
   local. This is a **fallback, not a choice** — remote is tried opportunistically; there is no scoring
   of "which box is best for this bead."

**Concurrency = two independent integer gates** (the "two-level gate"): fleet `max_concurrent`
(local-only counter, runtime-mutable via RPC, no restart) + per-queue `Workers` cap. Remote is bounded
separately by the worker's `max_slots` in the registry. Asymmetry (by design): **remote runs do NOT
count against `max_concurrent`** — so total ceiling = `max_concurrent` (local) + `max_slots` (remote).
Today: `max_concurrent:4` + gb-mbp `max_slots:6` = 10 admission ceiling, local guaranteed ≤4.

**Harness & model are resolved AFTER placement, independently** — two static 4-tier precedence walks
(per-bead label → per-queue → per-node → global default). `resolveHarness` (`harnessresolve.go:53`) and
`ResolveModelPreference` (`modelpreference.go:190`). Per-**bead** label routing is live; per-**queue**
harness is scaffolded but passed empty at the dispatch seam (hk-4x3rg not landed). **The gate does not
know or care which model a bead will use** — model selection happens downstream of, and blind to,
placement and capacity.

**Structural ceiling:** the worker registry is hard-capped at **one** worker (`workers.go:282`,
`ErrTooManyWorkers`). No N-machine fleet abstraction exists.

**Hardcoded knobs a scheduler would want to own:** cold-start spawn semaphore = fixed 3
(`workloop.go:1145`); agent_ready timeout = 150s default, no per-worker/per-model override
(`agentready.go:64`); poll cadence; round-robin policy.

## 2. Constraint dimensions a real scheduler must model

| # | Dimension (operator's words) | Natural unit | State today |
|---|---|---|---|
| 1 | Per-box throughput/volume (mini struggles, gb-mbp handles many) | concurrent **slots per machine** | **Partial** — static `max_slots` per worker + local `max_concurrent`; but registry holds ONE worker |
| 2 | Live load pressure per box ("only so much load") | load5/ncpu, mem/swap/disk frac | **Telemetry built, not acted on** — worker-report/breach reports it; only drives a binary enable/disable kill-switch, never placement |
| 3 | Instances of a model per box (DGX hosts only N sessions) | sessions per **(box × model)** | **Absent** — no (box,model) capacity anywhere; Pi is daemon-global one-model-per-pass |
| 4 | Provider cost model (Claude 20x sub / Codex $100mo / Pi+OR pay-per-token) | Claude,Codex: **quota headroom** (marginal≈0); Pi/OR: **$/token**; DGX: ~0 marginal | **Absent as live signal** — cost analyzed in plans only; the one live meter is an output-bytes proxy, Claude-only |
| 5 | Bead/test weight (same tests choke mini, fine on gb-mbp) | per-task **cost estimate** (cpu-s / tokens / class) | **Absent** — every bead = 1 slot regardless of weight. Operator's sharpest constraint, zero modeling |
| 6 | Quality bar per task category (cheap model must clear it) | quality score per (model,category) vs bar | **Designed-only** — eval harness measures, no routing consumes it |
| 7 | Fleet + per-queue concurrency gate | run-registry counters | **Built** — the two-level gate (§1) |

The scheduler's placement cell is a **(machine × model-instance) slot**; the load on a cell is a
**per-bead weight** (unmodeled); the global constraints are **provider quota/budget** (Claude/Codex)
and **marginal $/token** (Pi/OpenRouter). Today the system only counts *(machine)-slots*.

## 3. Prior art — build on this, don't reinvent

**BUILT / SHIPPED (the hooks):**
- Two-level concurrency gate — real, load-bearing (`workloop.go`, `concurrencycontroller.go`).
- Worker telemetry (worker-report WR1-5 + breach PB1-4) — landed, but **never run against a live
  worker; thresholds unvalidated**; observability only (`internal/workers/{telemetry,report_poll,breach}.go`).
- Retrospective cost accounting — `harmonik usage` (`internal/usage/`); accurate per-bead/model, but
  **batch, Claude-transcript-only, no budget field, no provider split**.
- Live budget-cutoff hook — `spendmeter_hkk3f8g.go` + per-queue meter; daily/per-queue caps emit
  `budget_exhausted`. **Cost basis = output-bytes proxy, Claude-only USD.** (The cutoff *mechanism*
  exists — it's the signal feeding it that's crude.)
- Dispatch-time harness/model seam — `resolveHarness` + per-DOT-node `model=` (`dot_cascade.go`). The
  place a router would slot into **already exists**.
- Pi/OpenRouter/ornith(DGX) harness plumbing — provider/model/cost per-config, one-model-per-pass.

**DESIGNED-ONLY (greenfield):**
- **AIMD autoscaler** (`plans/2026-06-20-remote-node-telemetry-autoscale/02-*`, epic `hk-e6gs`) —
  additive-increase/multiplicative-decrease on effective slots from load/mem/swap. **Deliberately
  deferred** — operator chose "keep hardcoded max until we have real load data." This is exactly the
  control loop dimension-2 would feed.
- **Dispatch-time model routing** (`plans/2026-07-05-model-selection/thread-5-routing.md`) — static
  category→tier table, precedence label > table > default. Proposals only, not filed as beads.
- **MR program** (`.harmonik/crew/admiral-initiatives.md`) — MR1 crew-on-Codex/Pi (no bead), MR2
  Pi-multi-provider (`hk-8ziid`, gate lifted), **MR3 dispatch-time model selection (`hk-vlvyg`)** whose
  stated policy IS the scheduling objective (below). None shipped.
- **Token-audit Phases 2-3** — real `token_usage` events on the bus + budget meter wired to real
  tokens (not the bytes proxy). Unbuilt. **This is the single biggest missing input for cost-awareness.**

## OPERATOR DECISIONS (2026-07-09) — binding design constraints

1. **Objective must be a PLUGIN.** Do NOT hardcode the scheduling policy. Build the objective/policy
   as a swappable component so it can be changed as constraints change (e.g. a Claude subscription-limit
   change), and so others can supply their own. MR3's "fill-cheap-first + auto-cut-Claude" is the *first*
   plugin, not the baked-in behavior. **KEY VALUE (operator, sharpened 2026-07-09): the policy must be
   changeable WITHOUT MODIFYING/REBUILDING THE BINARY AND WITHOUT RESTARTING THE DAEMON** — hot-reloaded
   live. This is the important-and-valuable part, and it's non-trivial.
   **DECLARATIVE, NOT IMPERATIVE (operator, 2026-07-09):** the operator's instinct — and preference — is
   that a "policy" should be a **STRUCTURED DATA file (yaml/json), not imperative code.** The value:
   declarative modeling makes the scheduling problem *modelable and testable* (you can reason about /
   simulate / diff a data file; you can't easily test arbitrary imperative strategy code). So Lane C is
   NOT "a Go interface with compiled strategy structs" — it's **a declarative policy schema** (what the
   knobs are: substrate fill-order, per-provider budget caps, cut-thresholds, weights, tie-breaks) that
   the daemon LOADS and HOT-RELOADS, plus a small evaluator that applies the data to live signals. Design
   the schema first; the "language" of the policy IS the deliverable. Open research: what does that
   yaml/json actually look like — this is genuinely unknown and worth a dedicated modeling/design pass.
2. **SIGNALS BEFORE OBJECTIVE — and signals are DATA-FIRST.** Build the signal layer first. Signals must
   be **LOGGED/PERSISTED in a form an analysis tool can consume** — the near-term goal is *understanding
   the system*, not yet acting on it. Sequence: (a) collect telemetry/load from machines + pull token
   usage (⚠️ caution below), (b) log it durably, (c) build an analysis tool to model these systems from
   the logged data, (d) THEN design the objective/placement engine on top of a validated model. Do not
   skip to routing before the data exists.
3. **Token-usage caution (operator-flagged).** Pulling/using per-provider token data needs care — treat
   it as sensitive; scope how it's collected, stored, and exposed before wiring it into any live path.

## 4. The objective function — already chosen as the FIRST plugin, not the mechanism

The operator never stated the objective precisely, but **MR3 already fuses the answer into a policy**:

> "fill Pi + local-DGX first, then spread across Claude + Codex; when Claude tokens run low, CUT Claude
> off automatically."

That is a **fill-cheapest-substrate-first + respect-Claude-quota** objective. The assessment's
recommendation is to treat MR3's policy as the working objective rather than re-derive one. The only
genuinely open tie-break: when cheap substrate is saturated, is lighting up an expensive (metered)
slot worth it? — that depends on **per-bead weight** (dimension 5) and throughput value, which is a
knob for the operator, not a fixed answer.

## 5. Gaps — today's slot-counter vs a constraint-aware scheduler

1. **No per-bead weight** (dim 5) — uniform-cost bin-packing; a mini-choking test counts the same as a
   doc edit. Operator's sharpest constraint, nothing models it.
2. **No (box × model) capacity** (dim 3) — capacity is per-machine only; DGX's "N sessions" limit and
   heterogeneous throughput aren't representable beyond one static `max_slots`.
3. **Load telemetry disconnected from dispatch** (dim 2) — data exists, the AIMD loop that turns it
   into slot decisions is deferred (`hk-e6gs`). Slots are hand-typed and hand-tuned.
4. **No live per-provider spend/quota signal** (dim 4) — the one live meter is a Claude-only bytes
   proxy. MR3's "cut Claude when low" is **unbuildable** without this.
5. **Routing seam exists but no router** — `resolveHarness`+`model=` can place a bead on any
   (harness,model), but selection is static, not objective-driven over live capacity + cost.
6. **Placement and model-selection are separate, downstream-of-each-other decisions** — a real
   scheduler must fold them into one assignment (a bead needs (machine × model) chosen jointly).
7. **Single-remote-worker structural cap** — no N-machine fleet abstraction.

## 6. Recommended shape of the work (for operator decision)

A real scheduler is a **medium-large initiative**. It decomposes naturally into signal-building
(cheap, incremental, independently useful) then a placement engine (the hard part). Suggested order —
each step is useful on its own even if we stop:

1. **Signals first (unlock everything, low risk, independently valuable):**
   - **Per-provider live token/spend + quota signal** — finish token-audit Phase 2-3 (real
     `token_usage` events) + a Claude/Codex quota-headroom gauge. This is the #1 missing input and it
     makes MR3's auto-cut buildable. Also directly serves the token-crunch program.
   - **Per-bead weight/class estimate** — even a coarse 3-class (light/heavy/test) label or a
     historical-duration lookup from `harmonik usage`. Turns unit-bin-packing into weighted.
   - **(box × model) capacity in workers.yaml** — extend the worker model from one `max_slots` to a
     small capacity table; requires lifting the single-worker registry cap.
2. **Close the telemetry→control loop** — un-defer the AIMD autoscaler (`hk-e6gs`) now that we'll have
   validated load data from the remote re-stand. This makes per-box slots self-tuning instead of
   hand-typed.
3. **The placement engine (MR3 core)** — fold harness/model resolution INTO placement: a single
   assignment step at `selectNextQueue` + worker pre-selection that, given (bead weight, quality bar,
   live capacity per (box,model), live per-provider cost/quota), picks the (machine × model) slot per
   the MR3 policy. Precedence still honors explicit per-bead labels as overrides.

**Sequencing note:** steps 1-2 are the prerequisites MR3 has been blocked on ("BUILD from research,
needs MR2 first" — but really it needs *signals* first). They're also individually valuable (cost
visibility, autoscaling) even if the full engine slips. Recommend building signals as their own lane
regardless of when the placement engine is scheduled.

**This is an ASSESSMENT, not a committed plan** — the next step, if the operator wants to proceed, is
to scope a kerf work for the signal layer (the cheap, high-value prerequisite) and decide whether the
placement engine is the next flagship or runs behind the quality/pi/remote front line already in flight.

## 7. Revised path per operator decisions (2026-07-09) — DATA-FIRST, PLUGGABLE

The operator directed: signals before objective, signals are data-first (log for analysis, not yet act),
and the objective is a swappable plugin. Revised sequence:

- **LANE A — Signal collection + logging (START HERE).** Persist, don't act. (1) Machine telemetry/load:
  the worker-report/breach payloads already exist — route them to a **durable log/store**, not just
  in-memory + kill-switch. (2) Token usage: finish token-audit toward real per-provider `token_usage`
  events, logged durably — ⚠️ scope collection/storage/exposure carefully first (operator caution). Done
  = load + token data for every run landing in a queryable log.
- **LANE B — Analysis tool.** A tool that reads Lane-A logs to model the systems: per-(box,model)
  throughput curves, per-provider cost/usage, per-bead-class weight distributions. Done = we can *see*
  the constraints from real data instead of guessing thresholds (retires the "unvalidated thresholds"
  and "hand-typed slots" gaps).
- **LANE C — Objective as a DECLARATIVE, HOT-RELOADED plugin (LATER, after A+B model the system).**
  NOT a compiled Go strategy interface — a **structured yaml/json policy schema** the daemon loads and
  **hot-reloads with NO restart** (operator hard constraints: no binary rebuild, no daemon restart, and
  declarative-not-imperative for modelability/testability). Deliverable = the policy SCHEMA (fill-order,
  per-provider budget caps, cut-thresholds, weights, tie-breaks) + an evaluator that applies it to live
  signals, folding model+machine selection into one placement step. MR3's fill-cheap-first+auto-cut-Claude
  is the first policy *file*, not baked-in code. Start with a dedicated **modeling/design pass on "what
  does the policy yaml/json look like"** (genuinely open). Do NOT build until A+B give validated data to
  design the schema against.

Lane A is the immediate, low-risk, independently-valuable start and it directly serves the token-crunch
program already in flight. Lane C stays behind the current quality/pi/remote front line.
