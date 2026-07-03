# Flywheel — Prior-Art Literature Review (harmonik knowledge base)

> Research pass for kerf work `flywheel` ("Long-running agent loop: indefinite execution with managed context").
> Scope: mine ALL prior harmonik thinking relevant to a custom long-running orchestrator-agent loop runtime with managed (no-compaction) context.
> Date: 2026-05-27. All citations are `path:line` against `/Users/gb/github/harmonik`.

## 0. The load-bearing distinction (read this first)

Harmonik already has TWO distinct "loops," and the flywheel problem lives in the second one — which is **explicitly undesigned and post-MVH**:

1. **The daemon work-loop** — deterministic Go, `internal/daemon/workloop.go`. Polls the bead ledger, claims beads, materializes worktrees, spawns *handler subprocesses* (the implementer/reviewer claude sessions), commits, closes beads. This loop is long-running and robust, but it is a **deterministic Go process with NO LLM and NO context window** (`specs/process-lifecycle.md:509` PL-020: "The daemon MUST NOT call any LLM, MUST NOT import any LLM SDK"). It is NOT the thing the flywheel problem is about.

2. **The orchestrator-agent** — a *separate Claude Code session* (cognition-bearing) that drives the daemon via its CLI (`specs/process-lifecycle.md:524` PL-019). This IS the flywheel target: the meta-loop that decides what to dispatch, watches events, keeps the queue full. The spec corpus declares it **OPTIONAL in MVH** and **post-MVH** (`specs/process-lifecycle.md:526`, `:953`), with only a one-line INFORMATIVE delegation note (`:530`). **Its context lifecycle, restart/respawn semantics, and consistency-across-resets are entirely unspecified.** That is the gap flywheel fills.

So: the daemon already solves "keep the *work* moving forever." Flywheel must solve "keep the *cognition that drives the daemon* moving forever, across context resets, token-cheaply, without losing track of in-flight runs."

---

## 1. Existing problem / goal framing & how it maps to the orchestrator-loop problem

### P02 — Agent Persistence Gap (`docs/problems/agent-persistence-gap.md`)
The closest existing problem statement. Direct hits:
- "If the problem is not fully solved within that session... there is no mechanism for the agent to 'come back to it'. The human must remember, re-frame, and re-launch." (`:20`) — exactly today's `/session-handoff` → restart → `/session-resume` cycle the flywheel automates.
- "the context window fills" named as a primary cause of session death (`:20`).
- Open Q1 (`:40`): "What is the right persistence primitive -- checkpointed agent state, a durable task queue with structured handoff, or something else?"
- Open Q3 (`:42`): "Can persistence be layered on top of existing tools (tmux sessions, cron, message queues) or does it require a purpose-built runtime?" — flywheel = "purpose-built runtime," the user's chosen branch.

### G02 — Persistent Problem Pursuit (`docs/goals/persistent-problem-pursuit.md`)
The goal flywheel serves:
- "Daemon-style execution... check state, find eligible work, execute, record outcome, repeat." (`:24`)
- "**State management:** Every task has durable state that survives session boundaries... reconstruct context without the human re-explaining anything." (`:28`) — the "where-things-are digest" the user wants.
- Success measures: "After a system restart, in-progress work resumes within minutes" (`:47`); "a task described Monday is still being actively worked Friday without any human re-prompting" (`:46`).
- Open Q (`:58`): "How much context can realistically be reconstructed from persisted state versus requiring fresh human input?" — central flywheel tension (fixed instructions + digest vs. compaction).

### G05 — Idea-to-Implementation Pipeline (`docs/goals/idea-to-implementation-pipeline.md`)
The orchestrator-agent does step 4 "Scheduling" (`:28`) and keeps the pipeline saturated; flywheel keeps *it* alive.

### Where the framing is THIN
P02/G02 were written 2026-04-13 (`:7`), predating a working daemon. They speak generically of "agents" persisting and never distinguish the daemon-loop (solved) from the orchestrator-agent-loop (unsolved). Flywheel's problem-space should make that split explicit — the chief reframing this prior art needs.

---

## 2. Prior art summaries grouped by source

### NTM — Named Tmux Manager (`docs/components/external/ntm.md`)
Richest prior art on **respawn / process-level persistence**.
- **Auto-respawn with graceful degradation** + multi-signal health monitoring (`:23`); recovers crash/rate-limit without human.
- **Agent-specific knowledge profiles** — per-type exit sequences, ready-state, rate-limit indicators (`:25-26`).
- **Event system**: pub-sub, bounded concurrency, wildcard subs, **history buffer** (`:31-32`).
- **Checkpoint and Recovery**: "Complete session snapshots capturing tmux layout, pane states, git state, and scrollback... restored after crashes." (`:34-35`) — restores *process/terminal* state, NOT *LLM-context* state. That gap is flywheel's whole point.
- **Account rotation** for rate-limit recovery (`:43-44`).
- **Limitations directly relevant**: "Tmux dependency... Not suitable for headless cloud execution" (`:58`); "No workflow semantics... manages processes, not workflows" (`:61`). NTM gives respawn but no context/what-next semantics — flywheel is the layer NTM lacks.

### Symphony (`docs/concepts/symphony.md`)
The **daemon-as-orchestrator loop shape**.
- **Daemon-as-Orchestrator**: "continuously running process that polls a tracker... always running, always watching." (`:18-19`) — already realized in harmonik's Go daemon.
- **Multi-Turn Execution**: "re-checking tracker state between turns... not one-shot." (`:24-25`).
- **Prompt-as-Policy** (`:21-22`): version-controlled, hot-reloadable WORKFLOW.md → model for flywheel's "fixed instructions" (cache-stable prompt half).
- **Continuation vs Retry** (`:30-31`): clean exit → 1s "did state advance?" check; failure → backoff. → "did last cycle make progress?".
- **Implicit Coordination** (`:36-37`): coordinate only through shared state, never directly → reconstruct world from beads+events.jsonl, not conversation.
- **Per-State Concurrency Limits** (`:39-40`) → "10 concurrent tasks" tracking.

### AlphaGo-Modeled System (`docs/concepts/alphago-system.md`) — north star
- **Meta-Process — four nested loops** (`:58-59`): execution / analysis / policy-proposal / governance, each at a different timescale. Flywheel hosts the *execution loop*.
- **Context rollback as first-class backtracking** (`:51-56`): "**Context rollback**: Restore a previous context window." The ONLY place in the KB treating context as a restorable/checkpointable object. Strong anchor.
- **Positive Emergence** (`:64-65`): "small tasks, auto-retries with caps, cheap rollback" — per-cycle unit-of-work constraints.
- **Comprehensive Transition Logging** (`:48-49`): the durable trace a resuming loop reads to rebuild "where things are."

### Kilroy (`docs/concepts/kilroy.md`, `docs/components/external/kilroy.md`)
The **context-management-strategy** prior art — most directly relevant to "managed context, no compaction."
- **Fidelity Modes** (`docs/concepts/kilroy.md:44-45`): SIX per-node context strategies — `full`, `truncate` (fresh, goal+run-id only), `compact` (fresh, bullet summary), `summary:low/medium/high` (~600/1500/3000 tokens). Precedence edge→node→graph→`compact`. **Closest model to the user's "fixed instructions + small state digest"** — lift this vocabulary.
- **Git-Native Checkpointing** (`:26-27`): one commit/node; three resume sources (filesystem, CXDB context-db, git branch).
- **Failure Classification + Cycle Detection** (`:35-36`): six classes; caps repeated traversals — prevents loop spin.
- **Separation of Concerns** (`:50-51`): orchestration / agent-exec-loop / **LLM client (context management)** as three layers.
- **Limitation**: static graph (`:62`) — orchestrator-agent behavior is open-ended, so checkpointing maps but static-graph routing does not.

### Harness Engineering (`docs/concepts/harness-engineering.md`)
- **Filesystem-Backed Coordination** (`:48-49`): "Git-tracked artifacts -- not conversation history -- are the coordination substrate... Conversations are ephemeral; files are durable." — bedrock for a no-compaction loop.
- **Middleware Architecture** (`:42-43`): LocalContext, **LoopDetection (prevent repetition)**, ReasoningSandwich, PreCompletionChecklist — per-cycle middleware candidates.
- **Progressive Disclosure** (`:30-31`): 100-line ToC not 800-line monolith — shape of token-minimal cache-stable instructions.
- **Guides + Sensors** (`:21-22`): loop inputs = fixed guide-prompt + sensor-stream (events.jsonl).

### Gas Town Hooks (`docs/concepts/gas-town-hooks.md`)
- **Completion detection via hooks** (`:21`): agent declares "done", deterministic hook verifies/routes → don't trust the loop's self-report of "queue full"; durable state is authority.
- Open Qs (`:71-73`): hook ordering/failure/composition/infinite-chains — same loop-safety questions flywheel faces.
- Claude Code hooks (SessionStart, Stop, etc.) named as the concrete attachment surface (`:42-45`).

---

## 3. Existing harmonik subsystems that already touch this

### Daemon work-loop (`internal/daemon/workloop.go`) — the SOLVED long-running loop
- **Goroutine-per-active-bead**, in-flight gated by `MaxConcurrent` via `RunRegistry` claim semaphore (`:9-18`). Runs "forever" against the ledger.
- **Idle-no-exit obligation**: daemon MUST NOT exit on queue empty/absent/completed (`specs/process-lifecycle.md:1096`, PL-013 retirement preserves it) — the *work* loop is already a forever-loop; only the cognition driving it dies.
- **Bounded retry / progress invariant** (`docs/design/workloop-bounded-retry.md`): `maxItemAttempts=3` (`:36`), monotonic `Attempts` persisted in queue.json surviving restart (`:27`), "No iteration can cycle without state change. Infinite loops are structurally impossible." (`:53-58`). **The prior-art template the flywheel cognition-loop should mirror** — a forever-loop needs a per-cycle progress invariant or it spins.
- Crash/restart recovery is deterministic: startup re-runs reconciliation idempotently from step 0; queue reconstructed from `.harmonik/queue.json` at startup step 8a (`docs/decompose-to-tasks/pl-pilot.md:7`).

### Event Bus (`docs/subsystems/event-bus.md`, `specs/event-model.md`) — the loop's "sensor"
- **In-process pub/sub backed by JSONL on disk** (`:64-67`): "The file is the source of truth; the in-process bus is a notification mechanism over it." JSONL root `~/.harmonik/events/...` (`:80`).
- **Replay-safe / replay-from-any-point** (`:60`) — a resuming loop reads the JSONL tail to rebuild "which of 10 runs completed during my restart." Concrete mechanism for the user's "don't lose track of completed tasks" requirement.
- **Live distinct event types in `.harmonik/events/events.jsonl` today**: run_started, run_completed, run_failed, run_stale, reviewer_verdict, reviewer_launched, review_loop_cycle_complete, queue_submitted, queue_appended, queue_group_completed, queue_paused, bead_closed, outcome_emitted, agent_ready, agent_ready_timeout, agent_heartbeat, implementer_phase_complete, implementer_resumed, daemon_started, daemon_orphan_sweep_completed, skills_provisioned, session_log_location, handler_capabilities, launch_initiated, reconciliation_mismatch_observed, queue_item_reconciled. These are the exact signals the loop consumes each cycle.
- **Future `harmonik subscribe` = hk-6ynv4** (`CLAUDE.md` §Future): planned NDJSON-to-stdout + server-side heartbeat (default 60s) so the orchestrator wakes periodically even if the daemon goes quiet. Direct enabling primitive — flywheel should depend on it, not re-invent the tail-two-files pattern.

### Memory Layer (`docs/subsystems/memory-layer.md`)
- MVH = just CASS over session-log JSONL (`:16`). Three-layer cognition (episodic/working/procedural) **deferred until usage clarifies curation** (`:16`, `:60`). Flywheel's "state digest" is essentially a *working-memory* artifact the memory layer deliberately does NOT yet provide — flywheel must define its own digest format.
- **No in-process log capture** (`:62`) — reinforces filesystem-as-substrate.

### Agent Runner (`docs/subsystems/agent-runner.md`)
- **"Keep agents running" default** (`:61`): "restarts it with context recovery" — but about *handler* agents, not the orchestrator-agent. "context recovery" named, never specified.
- Pi (`pi-mono`) is a second-class agent type (`:39`) — relevant since user floated Pi as a flywheel runtime.
- **tmux inspectability is a requirement** (`:43-45`) — locked decision #4; a flywheel runtime must preserve it or justify departing.

### Filesystem-as-Coordination-Substrate (`docs/ideas/filesystem-as-coordination-substrate.md`)
Argues *against* conversation-history-as-state — the philosophical core of "no compaction":
- Conversation history is "**Ephemeral**... **Bounded** (context windows have limits; long-running tasks overflow)... **Unversioned**." (`:19-24`)
- Cold-start problem (`:61`): needs filesystem context to start, but it doesn't exist until agents have run — flywheel's first-cycle bootstrap.
- State-file format Q (`:60`): JSON vs Markdown vs both — the digest format decision.

---

## 4. What session-handoff / session-resume capture today (the manual prototype)

### `~/.claude/skills/session-handoff/SKILL.md`
- A single file `./HANDOFF.md`, <~40 lines body. Next session "has none of this conversation — only the file you write and the repo."
- **Staleness marker** first line `<!-- PP-TRIAL:v2 YYYY-MM-DD <branch-name> -->`.
- A **verbatim-preserved directives block** between `<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT -->` markers — "standing instructions across sessions" (the *fixed-instructions* half).
- Plain-English: clean/blocked/broken; what+why; where things stand + next step; files to open first; real blocker.
- A **one-line translations glossary** mapping every internal code to plain English.
- Precondition: "Save your work first (commits, shelved planning works, scratch notes filed)" — durable-state-first.

### `~/.claude/skills/session-resume/SKILL.md`
- Reads HANDOFF.md + pointed-at files in CLAUDE.md reading order.
- **Staleness check** vs `git branch --show-current`; flags if stale.
- Says back <10 lines, translates codes, then **proceeds without asking "shall I continue?"** (default yes). Follows directives block verbatim.

### Live instance: `/Users/gb/github/harmonik/HANDOFF.md`
Real current example (v68): `Main at <sha>, clean, all pushed` + "what this session did" + "next step" + open-beads list + "files to open first" + caveats + translations glossary + final "Next action:" line (~57 lines).

**Implication for flywheel:** the *content schema* of a usable cross-reset digest is empirically validated — (status, what/why, next-step, open-items, files-to-open, blockers, glossary). The manual version is human-triggered, human-paced, lossy on in-flight detail, and LLM-regenerated each time (expensive). Flywheel = make this digest (a) auto-generated from durable state, (b) cache-friendly, (c) precise about in-flight runs — the one thing HANDOFF.md does poorly (it free-texts "open beads" rather than authoritatively tracking which of N runs are mid-flight).

---

## 5. Overlap check — does any existing spec or kerf work already cover this?

**Conclusion: NO existing spec or kerf work covers the orchestrator-agent's long-running context-managed loop. The flywheel space is genuinely open.**

### specs/ : architecture, beads-integration, claude-hook-bridge, claude-launchspec, control-points, event-model, execution-model, handler-contract, handler-pause, operator-nfr, process-lifecycle, queue-model, scenario-harness, workflow-graph, workspace-model, reconciliation/, examples/.
- **`process-lifecycle.md`** is the ONLY spec that mentions the orchestrator-agent, and only to **draw the boundary and declare it post-MVH**: PL-019 (`:524-530`) — separate Claude Code session driving the daemon via CLI, OPTIONAL in MVH (`:526`, `:953`). It specifies *nothing* about that session's context lifecycle, restart, digest, or token economy. INFORMATIVE note (`:530`) names only role/model-class/input-shape.
- **`handler-contract.md` / `claude-hook-bridge.md`** govern the *handler* (implementer/reviewer) sessions launched by the daemon — a different layer. `docs/claude-session-comms-audit-2026-05-13.md` deep-audits that layer and confirms there is *no daemon→claude task channel* even for handlers (TL;DR items 1-2) and that interactive-substrate messaging is unspecified. Useful context, NOT the orchestrator-agent loop.
- No spec mentions "context management," "compaction," "session handoff," "state digest," or a forever-running cognition loop for the orchestrator. (specs "loop"/"context" grep matches are all about the daemon work-loop and handler context.)

### kerf works (`kerf list`): flywheel (this, problem-space), phase-3-dot, daemon-liveness, phase-2-completion, bead-ledger-worktree-merge, testing-strategy-uplift, handler-pause, extqueue, bridge-integration, workflow-modes, claude-hook-bridge.
- **None** target the orchestrator-agent loop. Closest adjacents and why NOT overlaps:
  - `daemon-liveness` — keeping the *daemon* (deterministic Go) alive/healthy, not the cognition loop.
  - `handler-pause` / `extqueue` — daemon-side dispatch + queue surface; primitives flywheel *consumes* (queue append, pause-on-rate-limit) but no context concern.
  - `phase-2-completion` / `phase-3-dot` — workflow execution maturity, not the meta-loop.
- **Enabling dependency, not overlap: hk-6ynv4** (`harmonik subscribe`). Filed, unbuilt. Flywheel should depend on / co-design with it.

### Locked decisions to respect (not reopen)
- **Daemon has NO LLM** (`specs/process-lifecycle.md:509`, architecture AR-INV-007). Keep the cognition loop a *separate process on top* — do NOT propose embedding it in the daemon.
- **tmux inspectability** locked (decision #4; `docs/subsystems/agent-runner.md:43`). A headless/SDK flywheel runtime must preserve inspectability or explicitly justify departing.
- **Filesystem/durable-state over conversation-history** is the repeated KB verdict. "No compaction, reconstruct from state" is *consistent* with locked thinking, not a new bet.

---

## 6. Gaps — what nobody has designed yet

1. **Orchestrator-agent context lifecycle.** PL-019 names the session and stops. Undesigned: when the loop resets context, what survives a reset, the per-cycle prompt assembly. Core flywheel deliverable.
2. **State-digest format + generation.** HANDOFF.md proves a content schema but is human-prose, lossy on in-flight runs, LLM-regenerated. Undesigned: machine-generated, token-minimal, cache-stable digest from beads + events.jsonl. (Memory-layer working-memory tier explicitly deferred — `docs/subsystems/memory-layer.md:60`.)
3. **In-flight-run consistency across resets.** The acute requirement. Primitive exists (events.jsonl replay-safe, `docs/subsystems/event-bus.md:60`) but no one designed the reconciliation read turning the event tail into an authoritative in-flight-run table for a resuming cognition loop. (Daemon reconciliation covers beads/worktrees — `reconciliation/spec.md` — not an external cognition loop's run model.)
4. **No-compaction prompt economy.** Kilroy fidelity modes (`docs/concepts/kilroy.md:44`) give vocabulary but no impl for a *continuously-running* loop. Undesigned: cache-friendly layout (stable prefix = fixed instructions; volatile suffix = digest) and digest-refresh cadence vs. cache reuse.
5. **Cycle-trigger + progress-invariant for the cognition loop.** Daemon work-loop has bounded-retry + structural progress invariant (`docs/design/workloop-bounded-retry.md:53`). The cognition loop has NO analogue — undesigned: what wakes a cycle (event arrival? heartbeat? hk-6ynv4 stream?), what counts as cycle progress, how to avoid a spinning no-op loop burning tokens.
6. **Respawn of the cognition process itself.** NTM respawns process state + scrollback (`docs/components/external/ntm.md:34`) but NOT LLM-context. Undesigned: who supervises the flywheel runtime, how it restarts after its own crash, how it re-attaches to a daemon with in-flight runs.
7. **Runtime substrate choice.** Claude Code SDK / headless vs. Pi (`pi-mono`). NTM "not suitable for headless cloud" (`:58`) and tmux-inspectability lock (`:43`) are in tension; no doc resolves headless-vs-inspectable for the orchestrator layer.
8. **Idle / keep-queue-full backfill.** User wants "aggressive about keeping the queue full." `kerf next` + `harmonik queue append` exist; no design ties them into a watermark-driven continuous-refill control loop.

---

## Appendix — key file:line index
- Boundary (daemon=no-LLM, orchestrator-agent=post-MVH): `specs/process-lifecycle.md:509,524-530,953`
- Persistence problem/goal: `docs/problems/agent-persistence-gap.md:20,40,42`; `docs/goals/persistent-problem-pursuit.md:24,28,46-47,58`
- Context strategies prior art: `docs/concepts/kilroy.md:44-45,26-27`
- Daemon loop + loop-safety template: `internal/daemon/workloop.go:9-18`; `docs/design/workloop-bounded-retry.md:36,53-58`
- Event sensor stream: `docs/subsystems/event-bus.md:60,64-67,80`; live types in `.harmonik/events/events.jsonl`
- Filesystem-over-conversation: `docs/ideas/filesystem-as-coordination-substrate.md:19-24`; `docs/concepts/harness-engineering.md:48`
- Manual prototype: `~/.claude/skills/session-handoff/SKILL.md`, `~/.claude/skills/session-resume/SKILL.md`, `/Users/gb/github/harmonik/HANDOFF.md`
- Enabling dep: `harmonik subscribe` = hk-6ynv4 (`CLAUDE.md` §Future)
