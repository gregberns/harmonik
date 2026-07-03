# 01 — Problem Space: flywheel

> Long-running agent loop — indefinite execution with managed context.
> Kerf work `flywheel`, spec jig. Drafted 2026-05-27 by the research orchestrator from the user's brief + prior harmonik exploration. The user explicitly delegated problem definition ("act independently and autonomously … if you run into major questions, document them in the work item"). Open questions for the user are collected in §7 rather than blocking.

## 0. One-paragraph statement

The harmonik **orchestrator** — the agent that decides what work to dispatch, runs `harmonik run --beads …` batches, keeps the queue full, watches the daemon's event stream, and reacts to outcomes — is today a *Claude Code interactive session*. It cannot run for more than a few hours because its context window fills. When it fills, a human must run `/session-handoff`, kill the session, start a fresh one, and run `/session-resume`. Across "the last week or two" the project has burned dozens of these manual restart loops to push hundreds of beads through harmonik. We want to replace the human-in-the-restart-loop with a **custom long-running loop runtime** that keeps one or more agents executing essentially indefinitely, with **deliberately managed context** (no reliance on LLM compaction), that is **prompt-cache-friendly and token-frugal**, and that **never loses track of in-flight work across a context reset**.

## 1. What is actually broken (the friction, concretely)

1. **The orchestrator is mortal.** A Claude Code session has a finite context window. The orchestrator's job is unbounded (dispatch forever). So the session always dies, and a human always restarts it. This is the central friction the user named: *"It is extremely inefficient to have to constantly babysit the agent and restart the agent every time context starts filling up."*

2. **Restart is manual and lossy.** Continuity today rides on two skills — `/session-handoff` (write `HANDOFF.md`) and `/session-resume` (read it and continue). They work, but a human triggers them, and they capture a hand-curated digest, not a guaranteed-complete picture of in-flight state.

3. **In-flight work can be orphaned across a restart.** If 10 beads are running concurrently under `harmonik run` and the orchestrator is recycled mid-batch, the new session must reconstruct which of those 10 completed, failed, or are still running — from durable artifacts (events.jsonl, git, beads), not from the dead session's memory. Today nothing guarantees this; the new session can double-dispatch, miss a failure, or stall.

4. **The queue runs dry.** Observed pattern: agents are *"not aggressive enough at putting items on the queue."* The orchestrator does a batch, then drifts into investigation/triage and lets concurrency slots sit idle. Throughput is gated by orchestrator attention, exactly the thing we're trying to remove.

5. **Naïve context management would wreck economics.** The obvious fix — "keep the last N messages, drop the rest" — breaks the Anthropic prompt cache (a moved/truncated prefix invalidates the cached prefix) and inflates token spend. Compaction (LLM-summarize-the-history) is explicitly unwanted: the user wants each cycle to *start from a fixed instruction set + a small, freshly-built "where things are" digest*, not a model-summarized blob that drifts.

## 2. The reframe that matters

The harmonik knowledge base already has docs about **agent persistence** (`docs/problems/agent-persistence-gap.md`, `docs/goals/persistent-problem-pursuit.md`). Those are about **worker** agents persisting on a *problem* across sessions. **This work is about a different layer**: the **control loop / orchestrator** persisting as a *process*. The worker-persistence docs are upstream context, not the target. Naming it explicitly so research doesn't conflate the two:

- **Worker persistence** (prior docs): "describe a bug once, the system keeps working it until solved." Concerns a *task's* lifecycle across sessions.
- **Loop persistence** (this work, "flywheel"): "the dispatcher process never dies; it keeps the queue full and reacts to events forever, surviving its own context resets." Concerns the *orchestrator's* lifecycle as an always-on runtime.

The flywheel is the engine; worker persistence is one thing the engine can drive. We design the engine here.

### 2.1 Harmonik's two loops (precise vocabulary — from prior-art recon)

Harmonik **already has two loops**, and flywheel lives in the second:

1. **The daemon work-loop** (`internal/daemon/workloop.go`, spec `process-lifecycle.md` PL-020). A **deterministic Go process** — *no LLM, no context window*. It already keeps *work* moving forever: walks the queue, spawns handler claude instances, watches for completion, commits, merges, closes beads. This loop is **not** the problem; it is robust and is a **locked constraint**: the daemon stays LLM-free (`process-lifecycle.md` boundary). It already has a bounded-retry / per-iteration progress invariant (`docs/design/workloop-bounded-retry.md`) the cognition loop should imitate.
2. **The orchestrator-agent / cognition loop** (PL-019). The **Claude Code session** that drives the daemon via CLI — picks beads, decides batches, reads `kerf next`, reacts to outcomes. The spec names it only to **box it off as "OPTIONAL in MVH, post-MVH"**. Its context lifecycle, restart, and cross-reset consistency are **entirely unspecified**. **This is flywheel's space.**

So "flywheel" = *give the cognition loop the same indefinite-lifetime, progress-invariant robustness the daemon work-loop already has — but for an agent that has a context window the daemon doesn't.* The context window is the entire reason this is hard.

**Overlap check (clean):** No existing spec or kerf work covers the cognition loop. `handler-contract` / `claude-hook-bridge` govern the *handler* (implementer/reviewer) sessions — a different layer. **`harmonik subscribe` is already LANDED** (bead hk-6ynv4, sha d73bdbc, 2026-05-22) — an NDJSON event stream with a 60s server heartbeat — and `since_event_id` gap-free replay landed **2026-05-27** (hk-a5sil, sha 994c6d2). It is the **enabling event-transport dependency** for the loop's event capture (the CLI help text still says `--since-event-id` "NOT YET IMPLEMENTED" — a stale-doc-drift one-liner to fix). NOTE: the original brief and an earlier draft of this doc called subscribe "planned"; that was wrong — it exists. This materially de-risks the event-capture component.

**Prior art to lift, not reinvent:** Kilroy per-node *fidelity modes* (`truncate`, `summary:low/medium/high` — `docs/concepts/kilroy.md`) are the closest existing model to "fixed instructions + small digest." `events.jsonl` is replay-safe (`docs/subsystems/event-bus.md`) — the concrete mechanism to rebuild "which of 10 runs completed during a restart." The `session-handoff`/`session-resume` skills are the **manual prototype** of the digest (their content schema is already validated); flywheel automates it, makes it cache-cheap, and fixes its weak spot: authoritative in-flight-run tracking.

## 3. Goals (what should be true after this work)

The spec corpus should describe a **loop runtime** such that:

- **G1 — Indefinite execution.** One or more agents run in a loop that has no built-in end. The loop's lifetime is bounded only by the operator stopping it, not by any single LLM context window.
- **G2 — Context reset without loss.** When an agent's context approaches its limit (or on any cycle boundary), the runtime starts a fresh LLM context from *(a)* a fixed, cache-stable instruction prefix plus *(b)* a compact, freshly-derived state digest — never an LLM-summarized history, never a sliding message window. The agent loses no awareness of outstanding work.
- **G3 — Cache-friendly + token-frugal.** The context-management scheme is designed around the prompt-cache prefix model: the stable prefix stays byte-identical across cycles so it stays cached; only the small tail (the digest + the current turn) changes. Token spend per cycle is minimized and roughly constant, not growing with loop age.
- **G4 — Consistency across resets, including concurrent in-flight work.** The runtime guarantees that a freshly-started context can reconstruct the true state of all outstanding work from durable artifacts. No double-dispatch, no dropped completion, no missed failure when the agent recycles mid-batch.
- **G5 — Event capture & reaction.** The loop consumes a stream of events (the harmonik daemon's `run_completed`, `run_failed`, `reviewer_verdict`, etc.) and reacts — without a human relaying them. (How events physically arrive is deferred; see §4.)
- **G6 — Queue-pressure / aggressiveness.** The runtime has an explicit mechanism that keeps the work queue full — it treats idle concurrency slots as a defect to be corrected, not a state to drift in.
- **G7 — Single-vs-multi-agent decided on evidence.** The spec takes a position on whether this is one looping agent or several cooperating role-agents (e.g. prioritizer / enqueuer / completion-checker), justified by research, not assumed.
- **G8 — Custom runtime, Max-subscription-aware.** The design uses a *custom* agent-driver (Claude Code SDK / headless, or Pi coding-agent, or equivalent) rather than an interactive Claude Code session. This is the **deliberate exception** to the project's standing "use interactive Claude Code, not `-p`/SDK, to keep Max-subscription billing" rule — the user has explicitly authorized it for this runtime. Research must surface the billing/auth consequence of each candidate so the exception is taken with eyes open.

## 4. Non-goals / scope boundaries (this pass)

- **NG1 — Harmonik integration is deferred (but in-scope for the work overall).** *How* the runtime is wired into harmonik — how it's started/supervised, how daemon events are physically delivered to it, how it shares state with the existing Go daemon — is **explicitly out of scope for the problem/research/early-design passes**. The user: *"how we integrate this into harmonik is NOT in scope … lets stay focused on how if harmonik were running we could have one or more agents in a loop."* It **returns to scope** in the Integration pass (Pass 6) of this same work.
- **NG2 — Not solving the whole persistence vision.** We are not building worker-side cross-session task persistence, escalation policy, learning loops, or the idea-to-implementation pipeline here. Just the loop engine + context discipline.
- **NG3 — Event *transport* design is deferred.** We assume *some* event stream exists (today: `.harmonik/events/events.jsonl`). Designing a new transport/bus is out of scope now; the loop's *reaction* model is in scope.
- **NG4 — Not changing the worker/implementer agents.** The beads still get executed by harmonik-spawned claude instances as today. This work is about the loop above them.
- **NG5 — No new product vocabulary or abstractions** beyond what the loop runtime genuinely needs (per project "don't add abstraction layers" rule).

## 5. Constraints (things the design must respect)

- **C1 — No LLM compaction as the continuity mechanism.** Fixed instructions + derived digest only. (User, emphatic.)
- **C2 — Prompt-cache integrity.** No scheme that invalidates the cached prefix every cycle. Sliding-window message dropping is specifically called out as forbidden because it breaks caching.
- **C3 — Minimize token use** where possible; per-cycle cost should not grow with loop age.
- **C4 — Durable-artifact source of truth.** State reconstruction must come from durable artifacts (events.jsonl, git, beads SQLite/JSONL), consistent with harmonik's existing 4-layer state model (git = completion authority; JSONL = workflow state; beads = task ledger). The loop must not invent a competing source of truth.
- **C5 — Must tolerate its own crash/kill.** Auto-respawn / health is part of "indefinite." A killed loop process must resume cleanly from durable state (NTM-style infrastructure persistence, not prompt persistence).
- **C6 — Operator control.** A human must be able to stop, pause, and inspect the loop. (Harmonik value: tmux inspectability; signals = operator control.)
- **C7 — Honest about Max-subscription/billing tradeoff** for whichever runtime is chosen (see G8).
- **C8 — Daemon stays LLM-free (locked).** The deterministic Go daemon work-loop must not gain LLM cognition. Flywheel is a *separate* cognition process *on top of* the daemon, not a modification of it.
- **C9 — Tmux inspectability (locked harmonik value).** An operator must be able to attach and watch. A headless/SDK runtime that gives up tmux-attachability must explicitly justify the departure and replace the inspection affordance.

## 6. Success criteria (concrete, spec-level)

This work is complete when the spec corpus describes the following, and a reviewer agent confirms each is concretely specified (not hand-waved):

- **S1.** A loop-runtime spec exists that defines the cycle: *start fresh context → load fixed prefix + state digest → consume pending events / pick work → act (dispatch, react, triage) → persist outcome → decide whether to continue this context or recycle → loop.*
- **S2.** The spec defines the **state digest**: what it contains, where it is derived from (durable artifacts), how it is built deterministically (not by an LLM), and a worked example of its content for a "10 beads in flight" moment.
- **S3.** The spec defines the **context-lifecycle / recycle policy**: the trigger for starting a fresh context, and how the prefix is kept byte-stable for cache reuse. It explains *why* this beats both compaction and sliding-window, with the caching mechanics spelled out.
- **S4.** The spec defines the **consistency / reconciliation contract** a fresh context runs at startup to learn the true state of in-flight work (reusing/extending harmonik's existing reconciliation thinking), with the no-double-dispatch / no-dropped-completion guarantee stated.
- **S5.** The spec takes and justifies a **single-vs-multi-agent** position, including how roles (if multiple) divide responsibility and share state without stepping on each other.
- **S6.** The spec defines the **queue-pressure mechanism** that keeps slots full and names what "the queue is too empty" means operationally.
- **S7.** The spec names the **chosen runtime substrate** (SDK / headless / Pi / other) with a comparison table covering: caching control, context-injection control, billing/auth (Max), supervisability, crash-recovery, and multi-agent fit.
- **S8.** An **Integration** section (Pass 6) describes, at design level, how this loop runtime attaches to harmonik — deferred until the engine design is settled, but delivered within this work.
- **S9.** Open questions that genuinely need the user are surfaced explicitly (not silently resolved).

## 7. Open questions for the user (non-blocking — documented, will proceed with stated defaults)

1. **Codename/area.** Work is codenamed `flywheel`. OK, or prefer alignment with existing "persistence" vocabulary? *Default: keep `flywheel`.*
2. **Runtime substrate preference.** You named Claude Code SDK *and* Pi coding-agent as candidates. Any hard lean, or should research pick on merits? *Default: research picks on merits, you ratify.*
3. **Multi-agent appetite.** Is a multi-role design (prioritizer/enqueuer/checker) something you *want* explored as a serious option, or a "probably overkill, prove it" option? *Default: explore seriously but with a skeptic reviewer per anti-over-abstraction rule.*
4. **"Indefinitely" scale target.** Hours? Days? Weeks of unattended run? Affects how hard we push crash-recovery + cost. *Default: design for days-to-weeks unattended; degrade gracefully.*
5. **Acceptable per-cycle latency.** Is a fresh-context cold start every cycle acceptable, or must most cycles reuse a warm context and recycle only near the limit? *Default: warm-reuse within a context, recycle near the limit — minimizes both latency and cache thrash.*

## 8. Preliminary affected / new spec areas (refined in Pass 2)

Existing specs likely touched:
- `specs/process-lifecycle.md` — the loop is a new long-running process class.
- `specs/event-model.md` — the loop is a new event *consumer*.
- `specs/execution-model.md` — dispatch/queue-pressure semantics.
- `specs/reconciliation/*` — startup state-reconstruction is the consistency contract.
- `specs/control-points.md` / `specs/operator-nfr.md` — operator stop/pause/inspect of the loop.
- `specs/claude-launchspec.md` / `specs/handler-contract.md` — how a loop agent is launched/instructed.

Likely **new** spec area:
- `specs/loop-runtime.md` (working name) — the flywheel itself: cycle, digest, recycle policy, cache discipline, queue pressure, single-vs-multi-agent, runtime substrate.

(Decompose pass will confirm against the real spec contents and the prior-art findings.)
