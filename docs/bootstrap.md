---
title: Harmonik Bootstrap and Self-Build Plan
status: seed
type: plan
related: [docs/goals/bootstrapping-self-building.md, docs/subsystems/improvement-loop.md, docs/subsystems/scenario-harness.md]
created: 2026-04-19
updated: 2026-04-19
---

# Harmonik Bootstrap and Self-Build Plan

> Companion to [G06: Bootstrapping and Self-Building](goals/bootstrapping-self-building.md). This document is the "thinking-through" piece -- it describes how we get from zero to a self-building harmonik, what the minimum viable system looks like, what the lifecycle of a self-build cycle is, and what the operator controls (stop, pause-improve, pause-upgrade) actually do.
>
> This is a seed. The point is to surface the right questions early so the operator can react to them, not to lock in answers. Each section ends with a "Decisions needed" list.

---

## 1. The Two Phases

### Phase 1: Bootstrap

Hand-built. Humans plus assistants in conventional sessions. The output is the **minimum viable harmonik (MVH)** -- the smallest system that can run a workflow against its own repository and produce useful output.

Phase 1 is a normal software project. The leverage we get from harmonik does not yet exist; we are building the ladder.

### Phase 2: Self-build

Harmonik runs against the harmonik repository. Workflows take subsystem specs (the docs in this repo) as input and produce implementation. Humans review at policy-defined gates. Each completed self-build cycle adds capability that the next cycle can use.

Phase 2 is a meta-project. The leverage exists; we are climbing the ladder.

The boundary between the two is operational, not architectural: as soon as MVH can run a non-trivial workflow against this repo and produce a mergeable change, we are in Phase 2.

---

## 2. Minimum Viable Harmonik (MVH)

The MVH is the *smallest* slice of harmonik that supports a self-build cycle. Smaller is better -- everything we hand-build is everything we have to maintain by hand until self-build can extend it.

A defensible MVH includes:

| Subsystem | MVH cut |
|---|---|
| **S01 Orchestrator Core** | Static graph execution. Linear and simple branch workflows. No dynamic graph modification yet. Git-commit-per-node checkpointing. |
| **S02 Policy Engine** | YAML policies. Role assignment + transition guards + freedom profiles. No OPA, no fancy rule language. |
| **S03 Event Bus** | In-process pub/sub + JSONL persistence to disk. No external broker. |
| **S04 Agent Runner** | NTM-wrapped Go runner. Claude Code handler at minimum. Pi handler ideally. Twin binary support from day one. |
| **S05 Hook System** | Claude Code hooks for completion signaling and trigger-next-state. No universal hook framework yet. |
| **S06 Workspace Manager** | `git worktree` + adze for environment setup. No containers. Sequential multi-agent on one workspace supported. |
| **S07 Scenario Harness** | Enough to run one or two end-to-end scenarios against the Claude twin. CI integration optional initially. |
| **S08 Memory Layer** | CASS pointed at the canonical session-log directory. No three-layer cognition yet. |
| **S09 Improvement Loop** | *Not in MVH.* Improvement is a Phase-2 capability. MVH cycles are reviewed by humans; improvement-loop authoring waits. |

MVH excludes a lot deliberately. Everything excluded becomes a candidate target for the first self-build cycles.

### Decisions needed
- Is the MVH cut above too aggressive (insufficient capability to run a meaningful self-build) or too conservative (more hand-built code than necessary)?
- Should the Pi handler be in MVH or wait? (Argument for: forces the "agent type abstraction" honest. Argument against: more hand-built code.)
- Do we want CI integration of the scenario harness in MVH or post-MVH?

---

## 3. Lifecycle of a Self-Build Cycle

A single self-build cycle takes one subsystem (or one slice of one) from spec to merged implementation. The cycle:

```
spec_ready
   v
plan_decomposition       (planner agent)
   v
plan_review              (reviewer agent + human gate if policy says so)
   v
implementation_ready
   v
implement                (builder agent in isolated workspace)
   v
verify                   (non-agentic node: tests, lints, type-check)
   v
review                   (reviewer agent reads diff)
   v
revision_loop            (back to implement if review/verify failed; capped retries)
   v
human_review_gate        (policy-controlled; lower-risk subsystems may auto-approve)
   v
merge                    (fast-forward or PR)
   v
post_merge               (event-bus emits cycle_completed; memory layer indexes; improvement loop, when present, picks up signal)
```

Each transition is checkpointed. Each agent runs in an isolated workspace (or a shared workspace passed sequentially). The orchestrator drives the graph; policies decide gates and roles.

### Decisions needed
- What are the policy-defined human gates? At minimum: pre-merge for high-risk subsystems (S01, S02, S05). Should plan_review also be a human gate initially?
- What is the cap on revision_loop retries? Three? Five? Configurable per subsystem?
- Does each cycle build *one* subsystem, or one *spec slice* (which might span subsystems)? My instinct: spec slice -- subsystems are too large for a single cycle.

---

## 4. The Three Operator Controls

These are the operator's safety net. They must be exercisable while harmonik is processing a stream of tasks, without losing in-flight work.

> **Important framing.** These controls are *general harmonik features* that apply to any project running harmonik, not bootstrap-specific machinery. The bootstrap and self-build phases are one important use case for them; ordinary harmonik usage (a developer running harmonik against any codebase) uses the same controls.
>
> **Granularity.** Controls 4.2 (pause for improvement) and 4.3 (pause to upgrade) operate **between tasks**, not within a task's workflow execution. The orchestrator processes a queue of tasks (each task is one work item flowing through its workflow graph). Pause means: complete the current in-flight tasks, then stop pulling new tasks from the queue. Once the improvement or upgrade completes, the orchestrator resumes pulling tasks. A task once started runs to its terminal node uninterrupted.
>
> Control 4.1 (stop on major issue) is the exception -- a major issue may justify aborting an in-flight task, since "let it finish" is unsafe when the issue is, e.g., runaway cost or security regression.

### 4.1 Stop on Major Issue

**What it does.** Halts the orchestrator. Default behavior is to halt the task stream (no new tasks pulled) and let in-flight tasks complete. For severe issues, an immediate-abort variant terminates in-flight tasks too, preserving checkpoints and workspaces for forensic inspection. Emits an `orchestrator_stopped` event with reason. Notifies the operator.

**Triggers.** Any of:
- Verifier signal: build broken, security regression, critical test failure.
- Policy violation: an agent attempted a transition it was not permitted to make.
- Resource budget exhaustion: cumulative token spend or wall-clock time over a hard cap.
- Manual operator command (CLI: `harmonik stop` -- with flags for graceful vs immediate).
- Improvement-loop signal: a pattern detector flagged execution as anomalous (Phase 2 only).

**Recovery.** Operator inspects state, decides: resume the queue after fixing the underlying issue, abandon specific tasks, or rewind specific tasks to an earlier checkpoint and retry.

### 4.2 Pause for Improvement Cycle

**What it does.** A queue-level pause. The orchestrator finishes any in-flight tasks but stops pulling new ones. Once the queue is quiet, an improvement cycle runs: the improvement loop (S09) analyzes the logs and events from the recent run of completed tasks, identifies patterns -- excessive retry loops, prompts that performed poorly, slow nodes, frequently-failing transitions -- and proposes changes. Approved changes are applied (updated prompts, policy tweaks, workflow definition adjustments). The orchestrator then resumes pulling tasks from the queue, now executing under the updated configuration.

**Example.** Orchestrator has 20 tasks in its queue. Configuration says "after every 5 completed tasks, run an improvement cycle." Tasks 1-5 complete. Orchestrator pauses. Improvement loop reviews logs from those 5 runs, proposes prompt and policy adjustments, applies approved ones. Orchestrator resumes; tasks 6-10 run with the updated configuration; pause again; and so on.

**Triggers.**
- Manual operator command.
- Scheduled cadence in the orchestrator config (every N completed tasks, every M hours, etc.).
- Improvement-loop-initiated when it has accumulated enough signal to be confident a change is worth applying.

**Recovery.** Improvement cycle completes. Orchestrator resumes pulling tasks. Tasks completed before the pause are unaffected (they are already done); tasks pulled after the pause execute under the new configuration.

### 4.3 Pause to Upgrade Harmonik Version

**What it does.** Same queue-level pause as 4.2. In-flight tasks complete; no new tasks pulled. The running harmonik binary is then swapped for a newer version (typically one produced by a recent self-build cycle, but in general just any newer release). The new binary is started, picks up the queue where the prior orchestrator left off, and resumes pulling tasks. Subsequent tasks run on the new core.

**Example.** Orchestrator has been processing tasks for hours. After task 10, the operator (or the orchestrator config: "upgrade if a new version is available at task boundary") triggers an upgrade. Orchestrator finishes any in-flight tasks, stops. New harmonik binary started in its place. Tasks 11+ run on the new version.

**Triggers.**
- Operator command (CLI: `harmonik upgrade --to <version>`).
- Configured policy: "upgrade at task-N boundary" or "upgrade if a new version is available at the next pause."
- Improvement-loop-recommended (Phase 2+).

**Recovery.** New harmonik binary is now the orchestrator. The contract: the new binary must be able to read the queue state and any persisted task metadata left by the prior version. Since pause happens at task boundaries (no in-flight task state to migrate), this contract is much weaker than mid-task migration would require -- queue format compatibility is enough.

### Decisions needed
- For 4.1 (stop), what is the right default -- graceful (let in-flight finish) or immediate (abort)? My lean: graceful is the default; immediate requires an explicit flag and is justified by a small set of severity reasons.
- What is the contract on queue state format across harmonik versions? My lean: queue format is part of harmonik's public API and changes follow a documented compatibility window (N-1 always supported, breaking changes get a migration release).
- Do all three controls share an interface (operator CLI + dashboard + API), or are they differentiated by risk?
- Should the pause/upgrade configuration live in the orchestrator's runtime config, in the workflow definition, or in a separate "operator policy" file? They are different scopes -- runtime config seems right for cadence; workflow definition seems wrong because a workflow can be reused across projects with different operator preferences.

---

## 5. Sequencing the Bootstrap

A reasonable order to build the MVH, hand-built:

| Step | Builds | Why this order |
|---|---|---|
| 1 | Workspace Manager (S06) skeleton: worktree create/cleanup + adze invocation | Everything else needs isolated workspaces to test against |
| 2 | Event Bus (S03): in-process pub/sub + JSONL persistence | Required for observability of everything that follows |
| 3 | Agent Runner (S04): NTM wrapper + Claude Code handler + twin binary support | Required to launch *anything* |
| 4 | Claude twin binary | Required for testable workflows -- ship the twin alongside the handler |
| 5 | Orchestrator Core (S01): static graph + checkpoint | The driver everything plugs into |
| 6 | Hook System (S05): Claude Code hooks for completion + state-transition triggers | The glue that makes step 5 actually advance state |
| 7 | Policy Engine (S02): YAML policies + transition guards + freedom profiles | Wraps step 5 with constraint enforcement |
| 8 | Scenario Harness (S07): minimal scenario runner + first end-to-end scenario | Proves steps 1-7 actually work end-to-end |
| 9 | Memory Layer (S08): CASS pointed at the session-log dir | Optional for first self-build cycle, required soon after |
| 10 | Pi handler + Pi twin | Forces the "agent type abstraction" honest |

After step 8, MVH exists. After step 9, we are ready for Phase 2 with reasonable observability. Step 10 can happen in Phase 1 or Phase 2.

### Decisions needed
- Is this the right order? Specific concern: building the orchestrator (step 5) before the policy engine (step 7) means early hand-built code may bypass policies that get added later. Possible alternative: build a stub policy engine in step 5's pass, refine in step 7.
- Where does the scenario harness (step 8) start? At step 8 it exists; should an even earlier "scenario stub" exist by step 5 to drive smoke tests during construction?
- Does step 4 (Claude twin) need to be a fully separate binary on day one, or can a stub-script suffice for the first end-to-end scenario?

---

## 6. Risks Specific to Self-Build

The self-build loop introduces failure modes that ordinary software does not have.

| Risk | Description | Mitigation |
|---|---|---|
| **Self-break** | A self-build cycle modifies the orchestrator (or another core subsystem) in a way that breaks the orchestrator's ability to run the next cycle. | Scenario harness regression suite must run before any merge. "Can harmonik still build harmonik" is a goal gate. |
| **Drift** | Many small individually-reasonable changes accumulate to a worse overall system. Hard to detect because no single change is obviously bad. | Periodic baselines (e.g., every N cycles, snapshot the system; allow rollback to last baseline). Improvement loop must propose with explicit metrics. |
| **Goodhart** | The system optimizes for the metric the improvement loop tracks (e.g., cycle completion time) at the expense of metrics it does not track (code quality). | Multiple metrics, periodic human audits, "no improvement-loop changes to its own metric definitions." |
| **Quiet failure** | A cycle "completes" with degraded quality that automated verification did not catch. Subsequent cycles build on the degraded foundation. | Every merge produces an event; sample reviews by humans on a cadence; trigger sample reviews when verifier signals partial-pass repeatedly. |
| **Upgrade-during-build** | A version upgrade mid-build leaves checkpoints or events in an incompatible format. | Backward-compatibility contract on the harmonik core (decision needed in section 4.3). |

### Decisions needed
- What does the "regression baseline" look like? A tagged git commit + a captured event log + a known-good scenario suite output?
- What is the cadence for sample human reviews in Phase 2?

---

## 7. What This Plan Does Not Cover

- The actual Go module structure of the harmonik repo.
- Build/release tooling.
- Specifics of the CLI/API for the three operator controls.
- The improvement loop's internal design (handled by [S09](subsystems/improvement-loop.md)).
- The scenario definition format (handled by [S07](subsystems/scenario-harness.md) + open question).
- How harmonik is distributed/installed for users who run it on their own projects (this plan focuses on harmonik building harmonik, not on harmonik being deployed elsewhere).

These are deferred until the bootstrap shape is agreed.

---

## Cross-References

- [G06: Bootstrapping and Self-Building](goals/bootstrapping-self-building.md) -- The goal this plan serves
- [G07: End-to-End Testability](goals/end-to-end-testability.md) -- The testing posture self-build depends on
- [S07: Scenario Harness](subsystems/scenario-harness.md) -- The regression net for self-build
- [S09: Improvement Loop](subsystems/improvement-loop.md) -- The engine for "pause for improvement cycle"
- [S01: Orchestrator Core](subsystems/orchestrator-core.md) -- The MVH centerpiece
