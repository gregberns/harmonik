# Process Lifecycle

```yaml
---
title: Process Lifecycle
spec-id: process-lifecycle
requirement-prefix: PL
status: draft
spec-shape: requirements-first
version: 0.1.0
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-04-23
depends-on:
  - architecture
  - execution-model
  - event-model
  - handler-contract
  - operator-nfr
  - reconciliation
  - beads-integration
  - workspace-model
---
```

## 1. Purpose

This spec defines harmonik's process lifecycle: the per-project headless daemon, its startup and shutdown sequences, the local socket and pidfile layout, the agent-subprocess relationship, the composition-root boundary between the deterministic daemon and any cognition-bearing orchestrator-agent, and the crash semantics that make restart deterministic. It names cross-cutting obligations (the `harmonik upgrade` contract and the silent-hang detection obligation) that other specs cite by reference.

The spec exists separately from `operator-nfr.md` because the daemon is the process-shape carrier — its pre-`ready` states and its composition root are the structural preconditions every operator-control requirement (`7.3`), restart-RTO measurement (`7.8`), and graceful-shutdown ordering (`7.7`) depends on. Folding this content into `operator-nfr.md` would entangle structural process rules with non-functional operator surfaces.

## 2. Scope

### 2.1 In scope

- Per-project daemon scope: one daemon per `.harmonik/` directory, socket and pidfile layout.
- Startup sequence, including the pre-classification orphan sweep (tmux, worktree locks, subprocesses, stale intent files).
- Ready-state transition criteria and the `starting` → `reconciling` → `ready` prefix of the daemon status machine.
- Shutdown semantics: graceful (drain in-flight runs) and immediate (abort).
- Agent-subprocess spawning, parentage, and daemon-ward socket communication.
- Daemon-vs-orchestrator-agent distinction: the daemon is a deterministic Go binary with no LLM logic.
- ntm adapter scope: bounded to process/tmux concerns; SwarmPlan, checkpoint/recovery, and Agent Mail explicitly NOT consumed.
- Crash semantics for daemon crash, crash during startup reconciliation, and agent-subprocess crash.
- Named obligation for the `harmonik upgrade` contract (owned by [operator-nfr.md §7.5]).
- Named obligation for silent-hang detection (owned by [handler-contract.md §4.6]).

### 2.2 Out of scope

- Reconciliation classification, category taxonomy, investigator contract, and verdict vocabulary — owned by [reconciliation.md §9.2, §9.3, §9.4, §9.5].
- Handler launch mechanics, watcher goroutine, and subprocess error propagation — owned by [handler-contract.md §4.1, §4.3, §4.6].
- Workspace leasing, worktree creation, and lease-lock implementation — owned by [workspace-model.md §5.1, §5.5].
- Operator command semantics (pause/stop/upgrade behavior beyond daemon state-prefix) — owned by [operator-nfr.md §7.3].
- `harmonik upgrade` contract content (binary-hash verification, schema cross-version behavior, exec-replacement retry) — owned by [operator-nfr.md §7.5].
- Graceful-shutdown step ordering across subsystems beyond daemon-level sequencing — owned by [operator-nfr.md §7.7].
- Restart RTO numeric target and its measurement criteria — owned by [operator-nfr.md §7.8].
- Event payload schemas for lifecycle events — owned by [event-model.md §3.2].
- Startup failure-mode catalog content (exit codes per prerequisite failure) — owned by [operator-nfr.md §7.1] spec-draft obligation + [reconciliation.md §9.3] Cat 0 pre-check.

## 3. Glossary

- **daemon** — the per-project headless Go process that owns workflow state, dispatches runs, and exposes the local socket. Deterministic; never calls an LLM. (see §4.1, §4.6)
- **orchestrator-agent** — a separate Claude Code session (cognition-bearing) that drives the daemon via its CLI. Post-MVH; NOT a component of the daemon. (see §4.6)
- **project** — a git repo containing a `.harmonik/` directory. One daemon per project. (see §4.1)
- **pidfile** — the file at `.harmonik/daemon.pid` holding the running daemon's PID; lock asserts per-project singularity. (see §4.1)
- **socket** — the local Unix socket at `.harmonik/daemon.sock` used for daemon ↔ agent-subprocess and daemon ↔ CLI-client communication. (see §4.1, §4.5)
- **orphan** — a harmonik-owned resource (tmux session, worktree lock, subprocess, stale intent file) surviving a prior daemon instance that no current daemon tracks in memory. (see §4.2)
- **ntm adapter** — the thin layer that consumes ntm's process/tmux capabilities (spawning agents in panes, profile-based ready-state detection, rate-limit signals) while ignoring ntm's workflow-semantic features. (see §4.7)
- **composition root** — the `internal/daemon` Go package that wires all subsystems together and is the only package allowed to import most subsystems (per [architecture.md §1.4] subsystem-envelope rule).

## 4. Normative requirements

### 4.1 Per-project daemon scope

#### PL-001 — One daemon per project

Each project (a git repo containing a `.harmonik/` directory) MUST run exactly one daemon. Multiple projects on the same machine MAY run multiple independent daemons, one per project. The daemon has no awareness of other daemons on the same machine (multi-daemon operator commands are owned by [operator-nfr.md §7.10]).

Tags: mechanism

#### PL-002 — Pidfile at `.harmonik/daemon.pid`

The daemon MUST write its PID to `.harmonik/daemon.pid` on startup and MUST hold an advisory lock on that file for the duration of its lifetime. A second daemon invocation against the same project that finds the pidfile held by a live process MUST exit with a specific error code directing the operator to query the running daemon (per [operator-nfr.md §7.1]).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-003 — Socket at `.harmonik/daemon.sock`

The daemon MUST listen on a local Unix socket at `.harmonik/daemon.sock`. Agent subprocesses and CLI clients (`harmonik enqueue`, `harmonik status`, etc.) MUST communicate with the daemon exclusively through this socket. The daemon MUST remove a stale socket file on startup before binding.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-004 — Daemon owns per-project files under `.harmonik/`

The daemon's per-project file surface includes: `.harmonik/daemon.pid`, `.harmonik/daemon.sock`, the event log directory at `${event_log_dir}/events.jsonl` and `${event_log_dir}/dead-letters.jsonl` (per [event-model.md §3.4]), the transition-record directory at `.harmonik/transitions/` (per [execution-model.md §2.1]), and the intent log at `.harmonik/beads-intents/` (per [beads-integration.md §10.8]). The daemon MUST NOT read or write harmonik-owned state outside this surface.

Tags: mechanism

### 4.2 Startup sequence

#### PL-005 — Startup order is deterministic

The daemon's startup sequence MUST execute the following steps in order, and each step MUST complete before the next begins:

1. Acquire the pidfile lock (§PL-002). Exit on lock-contention failure.
2. Execute the orphan sweep per §PL-006.
3. Cat 0 pre-check per [reconciliation.md §9.3]; on prerequisite failure, enter `degraded` state per §PL-010 and do not proceed to step 4 until prerequisites clear.
4. Walk the git log for the project's repo, collecting checkpoint commits identified by the `Harmonik-Run-ID` trailer per [execution-model.md §2.1].
5. Query Beads via `br` for all `open` and `in_progress` beads (per [beads-integration.md §10.8]).
6. Build the in-memory model of completed, open, and in-flight beads from git + Beads state.
7. Dispatch reconciliation per [reconciliation.md §9.2a] action-mapping: auto-resolvable categories resolve inline; investigator-required categories dispatch reconciliation workflows.
8. Transition the daemon status to `ready` per §PL-009 and emit the status event.

The sequence (steps 1–8) is deterministic; no cognition participates. Investigator-workflow execution triggered by step 7 runs in parallel with `ready` and has its own per-workflow budget.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### PL-006 — Orphan sweep precedes reconciliation

Before the daemon executes the Cat 0 pre-check (§PL-005 step 3), the daemon MUST enumerate and clean up residual resources from any prior daemon instance:

- **Tmux sessions.** The daemon MUST list tmux sessions matching the project's harmonik naming convention (prefix `harmonik-<project-hash>-`) and kill every matching session via `tmux kill-session`. Because the new daemon has no in-memory tracking at this point, every matching session is an orphan by definition. After kill, the daemon MUST wait (bounded; ≤2 seconds) for underlying processes to exit before proceeding.
- **Worktree locks.** The daemon MUST inspect each worktree under the project's configured worktree root for lock files (`.harmonik/lease.lock` or equivalent per [workspace-model.md §5.1]). Locks whose mtime predates the current daemon's start time MUST be removed.
- **Subprocess cleanup.** The daemon MUST identify processes that have been re-parented to init (parent pid 1) whose binary path matches a handler binary under the project's expected launch path, and kill them via SIGTERM followed by SIGKILL with a bounded interval between them.
- **Stale intent files.** The daemon MUST enumerate `.harmonik/beads-intents/` for entries older than the current daemon's start time; each stale entry triggers a Cat 3a detector invocation per [reconciliation.md §9.3].
- **Event.** On completion, the daemon MUST emit `daemon_orphan_sweep_completed` (per [event-model.md §3.2]) with counts of tmux sessions killed, locks cleared, subprocesses killed, and stale intents referred to Cat 3a.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### PL-007 — Orphan sweep is deterministic and complete before classification

The orphan sweep MUST be deterministic given the filesystem + process state. After the sweep completes, no harmonik-owned process from a prior daemon instance is alive and no harmonik-owned worktree is locked by a prior-instance lease. The git walk (step 4) and Beads query (step 5) of §PL-005 operate against a quiescent filesystem.

Tags: mechanism

#### PL-008 — Startup failure-mode catalog obligation

This spec DEPENDS on the normative startup failure-mode catalog produced by the [operator-nfr.md §7.1] spec-draft obligation. The catalog MUST enumerate every prerequisite failure (git bad state, Beads SQLite state, schema-version mismatch, stale-pidfile race, filesystem-unwritable, disk-full during checkpoint commit) with failure-detection rule, exit code, operator remediation procedure, and per-failure event emission. The Cat 0 pre-check (§PL-005 step 3) consumes this catalog; the operator surface commands consume it for `harmonik status` reporting.

Tags: mechanism

### 4.3 Ready-state transition

#### PL-009 — Ready criteria

The daemon MUST transition status to `ready` only when ALL of the following conditions hold:

- The orphan sweep (§PL-006) has completed.
- The Cat 0 pre-check ([reconciliation.md §9.3]) has passed (no infrastructure prerequisite is failing).
- The git-log walk and Beads query (§PL-005 steps 4–5) have completed.
- The in-memory model has been built (§PL-005 step 6).
- Reconciliation dispatch (§PL-005 step 7) has completed its synchronous action-mapping for every in-flight run; dispatched investigator workflows MAY remain in-flight and MUST NOT block the `ready` transition.

On transition to `ready`, the daemon MUST emit a `daemon_ready` status event (per [event-model.md §3.2]) carrying the run IDs of any investigator workflows still in-flight. Restart RTO ([operator-nfr.md §7.8]) is measured from SIGTERM to `daemon_ready` emission.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-010 — Degraded state on Cat 0 infrastructure failure

When the Cat 0 pre-check (§PL-005 step 3) fails, the daemon MUST transition to `degraded` status and remain there until all prerequisites clear. In `degraded`, the daemon MUST NOT classify in-flight runs, MUST NOT dispatch runs, and MUST NOT transition to `ready`. The daemon MUST emit `infrastructure_unavailable` (per [event-model.md §3.2]) naming the specific prerequisite that failed, and MUST periodically retry the pre-check at a configurable cadence. `harmonik status` MUST report the `degraded` state and the failing prerequisite per [operator-nfr.md §7.1].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### 4.4 Shutdown

#### PL-011 — Graceful shutdown drains in-flight runs

On `harmonik stop --graceful` or SIGTERM, the daemon MUST execute a drain sequence:

1. Stop pulling new beads from the queue.
2. Allow in-flight runs to proceed to their next checkpoint, then suspend.
3. Wait for agent-subprocess termination (bounded by the operator-configurable drain timeout per [operator-nfr.md §7.7]).
4. Flush the event bus (fsync per [event-model.md §3.4]).
5. Release worktree leases (per [workspace-model.md §5.1]).
6. Release the pidfile lock and remove the socket file.
7. Exit with code 0 on clean drain; exit with code 1 if the drain timeout escalated to forced termination.

The daemon status transitions through `draining` during this sequence (per the operator-control state machine in [operator-nfr.md §7.3]). Subsystem-level shutdown ordering is owned by [operator-nfr.md §7.7]; this requirement names the daemon-level sequence.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-012 — Immediate shutdown aborts in-flight runs

On `harmonik stop --immediate` or SIGKILL (where SIGKILL cannot be intercepted, but its effect is what this requirement models), the daemon MUST skip the drain steps (§PL-011 steps 2–3). In-flight agent subprocesses are killed; in-flight state is recoverable via the next startup's orphan sweep (§PL-006) + reconciliation (per [reconciliation.md §9.2]). On `stop --immediate` (interceptable), the daemon MUST still attempt steps 4–7 (flush, unlock, remove socket, exit) on a best-effort basis.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-013 — Daemon does not exit on queue-empty

When all beads are closed or deferred and nothing is in-flight, the daemon MUST sleep (suspend CPU consumption) and wait for a subsequent `harmonik enqueue` or for external changes to the Beads store (periodically re-queried at a configurable cadence). The daemon MUST NOT exit on queue-empty. Daemon exit occurs only on explicit `harmonik stop`, on an operator upgrade transition (`running` → `upgrading` per [operator-nfr.md §7.3]), or on crash (§PL-024).

Tags: mechanism

### 4.5 Agent-subprocess management

#### PL-014 — Agent subprocesses are children of the daemon

The daemon MUST spawn agent processes as children of the daemon process (via the ntm adapter or equivalent — see §PL-020). Child parentage is structural: it allows the OS to re-parent orphans to init on daemon crash, which the next startup's orphan sweep (§PL-006) cleans up.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-015 — Agent ↔ daemon communication routes through the socket

Agent subprocesses MUST communicate with the daemon exclusively through `.harmonik/daemon.sock` (§PL-003). Operator-exposed agent commands (`harmonik claim-next`, `harmonik emit-outcome`, Beads-CLI invocations delivered via the injected skill per [beads-integration.md §10.9] and [handler-contract.md §4.11]) MUST route daemon-ward over this socket.

Tags: mechanism

#### PL-016 — Agent-subprocess failure is observed by the daemon

Agent-subprocess failure (crash, hang, policy violation) MUST be observed by the daemon's watcher goroutine per [handler-contract.md §4.3] and MUST produce typed events (`agent_failed`, etc., per [event-model.md §3.2]). The daemon-side watcher is S01-owned; the per-agent-type adapter (S04-owned) supplies the signal-interpretation layer.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-017 — Silent-hang detection obligation

This spec NAMES the silent-hang detection obligation owned by [handler-contract.md §4.6]. A silent hang is an agent subprocess that remains alive but produces no output, no heartbeat, and no lifecycle signal for longer than a bounded interval. The handler-contract spec owns the detection rule, the wall-clock ceiling, and the cleanup path; this spec requires that the daemon's watcher goroutine (§PL-016) implement the handler-contract detection rule and route silent-hang outcomes into reconciliation per [reconciliation.md §9.2].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### 4.6 Daemon vs orchestrator-agent distinction

#### PL-018 — Daemon is a deterministic Go binary with no LLM logic

The daemon MUST be a deterministic Go binary. The daemon MUST NOT call any LLM, MUST NOT import any LLM SDK, and MUST NOT embed any cognition-bearing component. All cognition in harmonik lives in agent subprocesses launched via handlers (per [handler-contract.md §4.1]) or in orchestrator-agent sessions interacting via the CLI (§PL-019). A proposal to embed cognition in the daemon (e.g., "let the daemon decide which bead to claim next using an LLM") violates this requirement.

Tags: mechanism

#### PL-019 — Orchestrator-agent is a separate Claude Code session

An orchestrator-agent (or coordinator-agent) MUST be a separate Claude Code session sitting on top of the daemon. It MUST interact with the daemon through the CLI (`harmonik enqueue`, `harmonik status`, priority triage, backlog grooming). It MUST NOT share process space with the daemon. The orchestrator-agent is OPTIONAL in MVH — the daemon is the sole driver (per [core-scope.md §5]); the orchestrator-agent layer is post-MVH.

Tags: cognition

> INFORMATIVE: The cognition tag on PL-019 names the delegation path: role = orchestrator-agent; model-class = Claude Code (Sonnet or Opus per project configuration); input shape = CLI responses from `harmonik status` and the Beads store read via the injected Beads-CLI skill. A reviewer verifies the path is wired correctly at integration time.

#### PL-020 — Composition root is `internal/daemon`

The daemon's code organization MUST treat the `internal/daemon` Go package as the composition root. Only `internal/daemon` is allowed to import across subsystem boundaries (per [architecture.md §1.4] subsystem-envelope rule and the `go-arch-lint` enforcement declared in [core-scope.md §6]). Subsystems MUST NOT import each other directly except through the interfaces each subsystem exposes.

Tags: mechanism

### 4.7 ntm adapter scope

#### PL-021 — ntm adapter consumes process/tmux surface only

The ntm adapter layer MUST consume only the following ntm capabilities: (a) agent process spawning in a tmux pane, (b) agent-profile knowledge (ready-state detection per agent type, rate-limit signals, clean exit sequences), (c) lifecycle events (process start, ready, rate-limited, stopped), and (d) account rotation (for agent types that support it).

Tags: mechanism

#### PL-022 — ntm adapter MUST NOT consume workflow-semantic features

The ntm adapter MUST NOT import or consume: (a) ntm's Pipeline System (harmonik's workflow semantics live in DOT graphs, not ntm pipelines), (b) ntm's SwarmPlan format (harmonik uses DOT per locked decision #7, not SwarmPlan), (c) ntm's checkpoint/recovery (tmux-session-resume is NOT equivalent to harmonik's git-checkpoint-based workflow-state recovery; the two solve different problems), or (d) ntm's file-reservation / Agent Mail features (harmonik uses Gas Town worktree+merge per locked decision #7; file reservations are explicitly rejected).

Tags: mechanism

#### PL-023 — Handler contract is the ntm boundary

The handler contract at [handler-contract.md §4.12] is where the ntm-vs-daemon boundary lives. Proposals that cross it by importing ntm pipeline types, SwarmPlan records, or Agent Mail primitives into the daemon MUST fail review. The ntm adapter is a thin layer bounded by the handler contract on the daemon side.

Tags: mechanism

### 4.8 Crash semantics

#### PL-024 — Daemon crash leaves a stale pidfile

On unexpected daemon termination (panic, SIGKILL, OS crash), the pidfile (§PL-002) is left stale. The next `harmonik daemon` invocation MUST detect a stale pidfile by checking that the recorded PID is no longer a live process, remove the stale pidfile, and proceed with startup per §PL-005. On restart, §PL-006 orphan sweeps residual tmux sessions, locks, and re-parented subprocesses before reconciliation classifies in-flight runs.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### PL-025 — Crash during startup reconciliation re-runs from step 1

If the daemon crashes during startup reconciliation (after §PL-005 step 2 but before reaching `ready`), the next restart MUST re-run §PL-005 from step 1. Reconciliation is idempotent per [reconciliation.md §9.1a]: re-running detection rules against the same git + Beads state produces the same classifications. Reconciliation workflows that were in-flight at crash time are re-classified (typically as Cat 5 for the outer run and Cat 3b for the investigator's verdict-unexecuted case) per the rules in [reconciliation.md §9.2a].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### PL-026 — Agent-subprocess crash routes through handler contract

An agent-subprocess crash that occurs while the daemon is alive MUST be handled per [handler-contract.md §4.6] (error propagation across async boundaries). The daemon routes the resulting outcome into reconciliation per [reconciliation.md §9.2] only if the resulting run state is ambiguous; a cleanly-failed agent subprocess (explicit `FAIL` outcome, bounded exit code) produces a normal run-failure transition and does not trigger reconciliation.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### 4.9 `harmonik upgrade` contract obligation

#### PL-027 — Upgrade contract obligation

This spec NAMES the `harmonik upgrade` contract obligation. The contract itself is owned by [operator-nfr.md §7.5] (spec-draft obligation) and MUST specify: (a) binary-source mechanism (repo path / hash-supply flag), (b) operator-supplied expected-commit-hash check procedure, (c) drain-vs-reconciliation interaction — what `upgrade` does if reconciliation workflows are in-flight per [operator-nfr.md §7.3], (d) cross-version state contract — what upgrade does if the new binary's schema-version is N−1, N, or N+1 vs the on-disk state, and (e) socket/client-CLI retry behavior during exec-replacement. This spec's only obligation is that the daemon's startup sequence (§PL-005) and shutdown sequence (§PL-011) be consistent with whatever the operator-nfr spec-draft produces; any inconsistency between the two specs is a finalize-time reconciliation task.

Tags: mechanism

### 4.10 Command surface (daemon side)

#### PL-028 — Daemon command surface

The daemon MUST support the following entry points:

- **`harmonik daemon`** — start the daemon headless. Blocks until signaled to stop; suitable for process-supervisor invocation.
- **`harmonik attach`** — open an observability TUI over the socket. Multiple simultaneous attaches MUST be supported; detaching MUST NOT kill the daemon.
- **`harmonik runner`** — convenience wrapper for solo-dev ergonomics: starts the daemon (if not running), opens a tmux session showing all agent processes (per the ntm inspectability requirement — locked decision #4), and optionally spawns an orchestrator-agent session. `runner` is sugar on top of `daemon` + `attach`, NOT a distinct execution mode.
- **`harmonik enqueue`, `harmonik status`, `harmonik pause`, `harmonik stop`, `harmonik upgrade`** — operator commands that communicate with the running daemon via the socket (§PL-003). `harmonik status` MUST report the current `degraded` state (Cat 0) per §PL-010.

Command-dispatch is deterministic CLI; semantic behavior of `pause`, `stop`, and `upgrade` is owned by [operator-nfr.md §7.3, §7.5].

Tags: mechanism

## 5. Invariants

#### PL-INV-001 — One daemon per project

For each project directory at any instant, at most one daemon process MUST hold the pidfile lock at `.harmonik/daemon.pid`. This invariant spans [operator-nfr.md §7.3] (operator-control state machine requires a singular daemon to track status against), [beads-integration.md §10.8] (the intent log is keyed against a single-writer daemon), and [workspace-model.md §5.1] (worktree leases assume a single leasing authority per project).

Tags: mechanism

#### PL-INV-002 — Daemon is deterministic

The daemon binary MUST contain no LLM invocations and no cognition-bearing components. This invariant spans [architecture.md §1.1] (four-axis classification assigns `llm-freedom=none` to the daemon as a whole), [architecture.md §1.8] (centralized-controller principle), and every subsystem spec's Go-package declaration.

Tags: mechanism

#### PL-INV-003 — Orphan sweep completes before reconciliation classification

§PL-006's orphan sweep MUST complete before any reconciliation detector (per [reconciliation.md §9.3]) runs. This invariant is load-bearing for reconciliation correctness: detectors scope on runs ([reconciliation.md §9.3] scoping invariant), and a run with a live orphan subprocess or stale worktree lock cannot be classified correctly until those orphans are cleared.

Tags: mechanism

## 6. Schemas and data shapes

### 6.1 Daemon status — state enumeration

Daemon status is a small enum consumed by the status event ([event-model.md §3.2]) and by `harmonik status`. The full operator-control state machine is owned by [operator-nfr.md §7.3]; this spec owns the `starting → reconciling → ready` prefix and the `degraded` side state.

```
ENUM DaemonStatus:
    starting       -- pidfile locked; orphan sweep not yet complete
    reconciling    -- Cat 0 passed; reconciliation dispatch in progress
    degraded       -- Cat 0 prerequisite failing; classification halted
    ready          -- §PL-009 criteria met; normal dispatch active
    paused         -- operator-initiated pause; in-flight runs drained (per [operator-nfr.md §7.3])
    draining       -- graceful-shutdown sequence active (§PL-011)
    stopped        -- terminal; pidfile released (per [operator-nfr.md §7.3])
```

### 6.2 Co-owned event payloads

Events emitted by this spec whose payload schemas are registered in [event-model.md §3.2]:

- `daemon_ready` — emitted on transition to `ready` (§PL-009); payload carries in-flight investigator-run IDs. Schema in [event-model.md §3.2].
- `daemon_orphan_sweep_completed` — emitted on completion of §PL-006; payload carries kill counts. Schema in [event-model.md §3.2].
- `infrastructure_unavailable` — emitted on Cat 0 failure (§PL-010); payload names the failing prerequisite. Schema in [event-model.md §3.2] (declared via amendment per [reconciliation.md §9.2]).

The emitting spec is normative for the *when*; event-model is normative for the *shape*.

### 6.3 Schema evolution

This spec declares no persistent on-disk schema. The daemon-status enum is an in-memory and wire-format type; additions are backward-compatible when new statuses are introduced (consumers MUST tolerate unknown statuses by falling through to a default display, per [operator-nfr.md §7.6] N−1 compatibility).

## 7. Protocols and state machines

### 7.1 Daemon status state machine

The daemon status machine's full transition set is owned by [operator-nfr.md §7.3]. This spec owns the `starting → reconciling → ready` prefix (and the `degraded` side state) that operator-nfr builds on.

| From | Event | Guard | To | Emits |
|---|---|---|---|---|
| (init) | daemon process launches | pidfile lock acquired | starting | — |
| starting | orphan sweep complete | §PL-006 complete | reconciling | `daemon_orphan_sweep_completed` |
| starting | Cat 0 prerequisite failing | [reconciliation.md §9.3] pre-check fails | degraded | `infrastructure_unavailable` |
| degraded | Cat 0 prerequisites cleared | retry pre-check succeeds | reconciling | — |
| reconciling | synchronous reconciliation complete | §PL-009 criteria met | ready | `daemon_ready` |
| ready | operator pause | per [operator-nfr.md §7.3] | (owned by operator-nfr) | — |
| ready | SIGTERM / `stop --graceful` | — | draining | `operator_pausing` (per [operator-nfr.md §7.3]) |
| draining | drain complete | §PL-011 steps 2–6 complete | stopped | — |
| any | SIGKILL / panic | — | (crash; next startup recovers per §PL-024) | — |

The orchestrator-agent (§PL-019) is NOT a state in this machine; it is a separate process with its own lifecycle that interacts with the daemon over the CLI.

## 8. Error and failure taxonomy

This spec does not own a failure taxonomy. Startup failure modes are cataloged per §PL-008 (obligation owned by [operator-nfr.md §7.1] + [reconciliation.md §9.3]). Run-failure taxonomy is owned by [execution-model.md §6]. Crash semantics (§PL-024, §PL-025, §PL-026) route through those taxonomies rather than defining their own.

## 9. Cross-references

### 9.1 Depends on

- **[architecture.md §1.1]** — four-axis classification; daemon is `llm-freedom=none` by design.
- **[architecture.md §1.4]** — subsystem envelope; §PL-020 composition root enforces it.
- **[architecture.md §1.8]** — centralized-controller principle; daemon-as-sole-driver follows from it.
- **[execution-model.md §2.1]** — checkpoint trailers; §PL-005 step 4 walks them.
- **[event-model.md §3.2]** — event taxonomy; lifecycle events emitted by this spec are declared there.
- **[event-model.md §3.4]** — event-log file layout; §PL-004 owns the daemon-side surface.
- **[handler-contract.md §4.1]** — handler interface; daemon spawns subprocesses via handlers.
- **[handler-contract.md §4.3]** — watcher goroutine; §PL-016 routes through it.
- **[handler-contract.md §4.6]** — error propagation and silent-hang detection; §PL-017 names the obligation.
- **[handler-contract.md §4.11]** — skill injection at launch; §PL-015 references Beads-CLI skill routing.
- **[handler-contract.md §4.12]** — handler-as-modularity-boundary; §PL-023 names it as the ntm boundary.
- **[operator-nfr.md §7.1]** — exit-code taxonomy and health surface; §PL-008 consumes the startup failure catalog.
- **[operator-nfr.md §7.3]** — operator-control state machine; this spec owns the `starting → reconciling → ready` prefix, operator-nfr owns the rest.
- **[operator-nfr.md §7.5]** — `harmonik upgrade` contract; §PL-027 names it.
- **[operator-nfr.md §7.7]** — graceful-shutdown cross-subsystem ordering; §PL-011 names the daemon-level sequence.
- **[operator-nfr.md §7.8]** — restart RTO; §PL-009 defines the measurement endpoint (`daemon_ready` emission).
- **[reconciliation.md §9.1a]** — reconciliation-workflow idempotence; §PL-025 depends on it.
- **[reconciliation.md §9.2]** — category taxonomy; §PL-005 step 7 dispatches through it.
- **[reconciliation.md §9.2a]** — action-mapping; §PL-005 step 7 routes by it.
- **[reconciliation.md §9.3]** — detectors and Cat 0 pre-check; §PL-005 step 3 invokes it.
- **[beads-integration.md §10.8]** — intent log and idempotency-keyed writes; §PL-004 and §PL-006 reference it.
- **[beads-integration.md §10.9]** — Beads-CLI skill; §PL-015 references skill-routed commands.
- **[workspace-model.md §5.1]** — worktree leases; §PL-006 unlocks stale ones.

### 9.2 Reverse dependencies

> INFORMATIVE: Reverse dependencies are computed on demand from all specs' `depends-on` lists. At draft time, specs known to depend on this spec include `handler-contract.md` (launch + socket model per §4.10, §4.12), `workspace-model.md` (leases assume a single-daemon leasing authority per §PL-INV-001), `operator-nfr.md` (§7.3 builds on this spec's status-prefix; §7.8 measures from this spec's `daemon_ready` event), `reconciliation.md` (§9.2 assumes this spec's orphan-sweep invariant), and `beads-integration.md` (§10.8 intent log is keyed against a single-writer daemon).

### 9.3 Co-references

- **[core-scope.md §5 Orchestrator loop]** — this spec consumes the daemon-as-sole-driver framing declared there; no reverse dependency (core-scope is a foundation document, not a spec).
- **[core-scope.md §6 Subsystem organization]** — this spec consumes the `internal/daemon` composition-root declaration; no reverse dependency.
- **[docs/foundation/components.md §8]** — bootstrap-era source for this spec's normative content, migrated on finalize.

## 10. Conformance

### 10.1 Conformance profiles

**Core MVH.** An implementation conforms to Core MVH when it passes PL-001 through PL-028 and satisfies all three invariants (PL-INV-001, -002, -003). The `harmonik upgrade` contract (PL-027) conformance is delegated to [operator-nfr.md §7.5] and is not required for Core MVH until operator-nfr.md §7.5 finalizes.

**Post-MVH.** Orchestrator-agent session integration (PL-019) is OPTIONAL in MVH. An implementation MAY conform to Core MVH without ever spawning an orchestrator-agent.

### 10.2 Test-surface obligations

> INFORMATIVE: `specs/testing.md` does not yet exist. Test obligations are named in prose here and MUST migrate to test-layer citations within one revision cycle of testing.md landing. See OQ-PL-001.

For each requirement, the implementation MUST satisfy at least one test covering the behavior:

- **PL-001, PL-002, PL-INV-001** — a twin-driven test that attempts to start a second daemon against the same project and asserts it exits with the pidfile-contention exit code.
- **PL-005, PL-006, PL-007, PL-INV-003** — a scenario test that seeds tmux sessions, stale worktree locks, re-parented subprocesses, and stale intent files, then starts the daemon and asserts the orphan-sweep event payload matches expected counts before any reconciliation detector runs.
- **PL-009, PL-010** — scenario tests covering (a) `ready` transition only when criteria are met and (b) `degraded` persistence until Cat 0 clears.
- **PL-011, PL-012** — scenario tests for graceful drain (asserting in-flight runs reach a checkpoint before suspend) and immediate abort (asserting subprocess kill + next-startup recovery).
- **PL-018, PL-INV-002** — a build-time lint (e.g., `go-arch-lint` per [core-scope.md §6]) asserting `internal/daemon` imports no LLM SDK, plus a unit test that inspects the binary's import graph.
- **PL-020** — `go-arch-lint` declaration that `internal/daemon` is the only subsystem-crossing importer.
- **PL-021, PL-022, PL-023** — lint rule: `internal/adapter/ntm` imports only the allowed ntm surface (process/tmux); importing ntm pipeline or SwarmPlan packages is a build failure.
- **PL-024, PL-025, PL-026** — chaos-style scenario tests: SIGKILL the daemon mid-reconciliation; assert the next startup re-runs §PL-005 deterministically and produces identical classifications.

### 10.3 Excluded conformance claims

- `harmonik upgrade` contract conformance — owned by [operator-nfr.md §7.5]; this spec makes no conformance claim over upgrade semantics.
- Silent-hang detection rule conformance — owned by [handler-contract.md §4.6]; this spec names the obligation but does not test the detection rule directly.
- Reconciliation classification correctness — owned by [reconciliation.md §10]; this spec tests only the orphan-sweep precondition.
- Restart RTO numeric target — owned by [operator-nfr.md §7.8]; this spec defines only the measurement endpoint.
- Multi-daemon commands (`harmonik list`, machine-level budget coordination) — owned by [operator-nfr.md §7.10].

## 11. Open questions

#### OQ-PL-001 — Migrate test-surface obligations to testing.md citations

Question: When `specs/testing.md` lands, §10.2's prose test obligations must migrate to citations of the form `[testing.md §<layer>]`.
Owner: foundation-author
Blocks: none (Core MVH tests can be authored against the prose obligations in the interim).
Default-if-unresolved: Tests follow the prose obligations; migrate within one revision cycle of testing.md finalizing.

#### OQ-PL-002 — Bounded timeouts for orphan-sweep sub-steps

Question: What are the normative upper bounds for the tmux-kill wait (§PL-006 "≤2 seconds"), the SIGTERM→SIGKILL interval for re-parented subprocesses (§PL-006 "bounded"), and the Cat 0 retry cadence (§PL-010 "configurable")?
Owner: foundation-author (coordinated with operator-nfr for consistency with drain timeout §7.7)
Blocks: none (MVH uses suggested values: 2s tmux wait, 5s SIGTERM→SIGKILL, 10s Cat 0 retry).
Default-if-unresolved: Suggested defaults above; tune per operator feedback.

#### OQ-PL-003 — `.harmonik/` directory auto-creation

Question: If a project has a git repo but no `.harmonik/` directory, does `harmonik daemon` auto-create it, or does it fail and require `harmonik init`?
Owner: foundation-author
Blocks: PL-001 (the "project" definition depends on `.harmonik/` existence).
Default-if-unresolved: Require `harmonik init` (explicit opt-in); daemon fails with a specific exit code if `.harmonik/` is absent.

## 12. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-04-23 | 0.1.0 | foundation-author | Initial draft migrated from [docs/foundation/components.md §8] per spec-template 1.1. Incorporates round-2 amendments: §8.2 step 1a orphan sweep, §8.3 upgrade-contract obligation, §8.5 silent-hang obligation, §8.6 daemon-vs-orchestrator-agent distinction, §8.8 crash semantics. |
