# Workspace Model

```yaml
---
title: Workspace Model
spec-id: workspace-model
requirement-prefix: WM
status: draft
spec-shape: requirements-first
version: 0.1.0
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-04-23
depends-on:
  - execution-model
---
```

## 1. Purpose

This spec defines the workspace primitive — a per-run git worktree, its branching convention, its lifecycle, and the session-log directory and metadata sidecar it owns. It names the lease-by-run rule (worktrees are leased by a `Run`, not by individual agents), the three-level branching model (node commits on a task branch, task branch squash-merges to an integration branch, integration merges to main), the merge-conflict resolver (original implementer), and the session-log path + metadata sidecar that the workspace manager (S06) pre-creates before the handler (S04) attaches.

The spec is separate from [execution-model.md] because the worktree lifecycle, branching, and merge semantics are large enough to warrant their own surface and because multiple other subsystems (S01 orchestrator, S04 agent runner, S06 workspace manager, S08 memory layer, reconciliation) cite the workspace types and events directly.

## 2. Scope

### 2.1 In scope

- The `Workspace` type: worktree path convention, branch naming, lease state machine.
- Lease-by-run rule: one lease per run, multiple agents sequentially occupy one worktree.
- Workspace lifecycle events the system emits and consumes.
- Three-level branching: node commits on task branch, task branch on integration branch, integration on main.
- Merge semantics back to the integration branch, including the conflict-resolution role.
- Session-log directory layout and the `harmonik.meta.json` metadata sidecar (S06 side of the pipeline declared in [docs/foundation/components.md §5.3a]).
- Failed-run worktree persistence — no auto-cleanup beyond the startup orphan sweep.
- Re-run vs intra-run-loop distinction at the workspace layer.
- Interrupt-state representation as an orthogonal field on the workspace record.

### 2.2 Out of scope

- Handler session-log emission (writing the log file) — owned by [docs/foundation/components.md §4] handler-contract; S04 emits. The `session_log_location` event emission contract is there.
- CASS ingestion of session-log directories — owned by the memory-layer spec (S08 subsystem); this spec only declares the on-disk layout S08 reads.
- Beads CLI adapter, bead-status writes — owned by [docs/foundation/components.md §10] beads-integration. This spec only carries `bead_id` as an opaque correlation field.
- Reconciliation verdicts that trigger workspace actions (`reopen-bead`, `reset-to-checkpoint`) — owned by [docs/foundation/components.md §9] reconciliation. This spec names WHAT workspace action each verdict triggers; reconciliation owns the verdict vocabulary.
- Orchestrator loop that issues workspace create/lease requests — owned by [execution-model.md §4.3] and the S01 subsystem spec.
- Event payload shapes — owned by [docs/foundation/components.md §3.2] event-model. This spec declares emission obligations (WHEN); event-model declares payloads (WHAT).
- Operator control (pause / stop / upgrade) interaction — owned by [docs/foundation/components.md §7] operator-nfr. This spec names WHICH workspace states are affected.
- Adze environment provisioning — not in MVH; MVH workspaces are plain `git worktree add` (see [docs/foundation/core-scope.md §3]).

## 3. Glossary

- **workspace** — a leased git worktree with a stable `workspace_id`, a path on disk, a branch, and a lifecycle state. (see §4.1)
- **worktree** — the filesystem directory backing a workspace, created by `git worktree add` under the configured worktree root. (see §4.1)
- **worktree root** — the parent directory under which per-run worktrees are created; default `<repo>/.harmonik/worktrees/`. (see §4.1)
- **task branch** — per-run git branch on which that run's checkpoint commits accumulate; default name `run/<run_id>`. (see §4.2)
- **integration branch** — the per-parent-bead branch onto which task branches squash-merge; default name `harmonik/integration` or derived from the parent-bead ID. (see §4.2)
- **lease** — the exclusive binding of one `Run` to one workspace for that run's full lifetime. (see §4.3)
- **session-log directory** — the per-session directory under `${workspace_path}/.harmonik/sessions/${session_id}/` where handlers write logs and S06 stamps metadata. (see §4.7)
- **metadata sidecar** — `harmonik.meta.json` written by S06 into the session-log directory as the authoritative join key for CASS indexing. (see §4.7)
- **interrupt-state** — an orthogonal field on the workspace record that captures whether the workspace was interrupted by operator action or a crash, independent of the lifecycle state. (see §4.10)
- **original implementer** — the agent that performed the work leading to a merge; for MVH, the merge-conflict resolver. (see §4.6)

## 4. Normative requirements

### 4.1 Worktree primitive

#### WM-001 — Workspace type and required fields

A `Workspace` MUST carry `workspace_id` (stable across restarts), `run_id` (the run this workspace is leased to), `repository` (path or URL of the backing repo), `parent_commit` (the commit the workspace was branched from), `branch_name` (per §4.2), `path` (absolute filesystem path to the worktree directory), `state` (per §4.4), `interrupt_state` (per §4.10), `metadata` (creation timestamp, operator fingerprint), and an optional `bead_id` (opaque correlation field per [docs/foundation/components.md §10.3]).

Tags: mechanism

#### WM-002 — Worktree path convention

Every workspace MUST be created at the canonical path `<repo>/.harmonik/worktrees/<run_id>/`, where `<repo>` is the repository's root directory and `<run_id>` is the run's stable identifier. The worktree root (`<repo>/.harmonik/worktrees/`) MAY be overridden by operator configuration; the per-run subdirectory `<run_id>/` is fixed.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### WM-003 — Worktree creation uses git worktree add

The workspace manager MUST create a worktree via `git worktree add <path> <branch>` against the backing repository, where `<path>` is the path from WM-002 and `<branch>` is the task branch from §4.2. No provisioning layer (adze, devbox, container build) participates in MVH worktree creation; the worktree is a plain subfolder.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### WM-004 — Workspace ID is stable across restarts

The `workspace_id` MUST be generated deterministically from the `run_id` (recommended: `workspace_id = "ws-" + run_id`) so that a daemon restart finds the same `workspace_id` for a given run without consulting a separate store. State reconstruction uses git + Beads per [execution-model.md §4.7]; the workspace ID MUST be derivable from those sources.

Tags: mechanism

### 4.2 Branch naming

#### WM-005 — Task branch naming convention

Every run's task branch MUST be named `run/<run_id>`. The branch is created off the current integration branch (§4.2.WM-006) at worktree-create time. Checkpoint commits per [execution-model.md §4.4] land on this branch only.

Tags: mechanism

#### WM-006 — Integration branch naming convention

The integration branch for a parent-bead scope MUST be named `harmonik/integration` by default, OR derived from the parent bead's ID when the parent-child edge (per [docs/foundation/components.md §10.3]) is present. The exact template for parent-derived integration branches is operator-configurable; the default template is `harmonik/integration/<parent_bead_id>`. A run without a parent-bead context MUST target the fixed `harmonik/integration` branch.

Tags: mechanism

#### WM-007 — Three-level branching model

The system MUST use a three-level branching model: (a) node commits land on the task branch per WM-005 and [execution-model.md §4.4.EM-023]; (b) the task branch squash-merges onto the integration branch at run-terminal-success per §4.5; (c) the integration branch merges to main under developer or operator policy. Harmonik's contract ends at "integration branch holds one commit per task." Merge style from integration to main is NOT dictated by this spec.

Tags: mechanism

#### WM-008 — Small-scope collapse

When a run has no parent-bead relationship in Beads (per [docs/foundation/components.md §10.3]), the task branch MAY squash-merge directly to main, skipping the integration branch. The decision is deterministic on the Beads parent-child edge: present → task branch targets integration; absent → task branch MAY target main.

Tags: mechanism

#### WM-009 — Branch naming is stable across a harmonik version

Task-branch and integration-branch naming conventions MUST be stable across a harmonik minor version per the compat contract declared in [docs/foundation/components.md §7.4]. A breaking change to branch naming requires a migration release.

Tags: mechanism

### 4.3 Lease model

#### WM-010 — Lease is held by the run, not by individual agents

A workspace MUST be leased by exactly one `Run` for the run's full lifetime. Multiple agents within the run (planner, researcher, builder, reviewer, merge agent) MUST occupy the same worktree sequentially across their nodes. An agent MUST NOT hold exclusive ownership of the worktree for the duration of its agent-level session; the centralized-controller principle ([docs/foundation/components.md §1.8]) requires the run — not the agent — to be the lease holder.

Tags: mechanism

#### WM-011 — One active agent at a time inside a workspace

At any instant, AT MOST ONE agent process MAY be actively writing to the worktree. Agents run sequentially as the run traverses its workflow graph; parallel nodes within one run MUST NOT share a worktree. Parallel nodes across different runs occupy separate worktrees per WM-002.

Tags: mechanism

#### WM-012 — One run per bead at a time

At any instant, AT MOST ONE run MAY be in flight for a given bead. A second run for the same bead is permitted ONLY after the first has reached a terminal state (completed, failed, or canceled) per [execution-model.md §4.3]. Re-claim semantics are defined in §4.9.

Tags: mechanism

#### WM-013 — Workspace ID is discoverable from run_id without a separate index

Given a `run_id`, the workspace manager MUST be able to resolve the workspace record (path, branch, state) by deterministic construction per WM-002 and WM-004 plus a filesystem check. No separate run-to-workspace index MAY be required as the authoritative lookup path.

Tags: mechanism

### 4.4 Lifecycle states and events

#### WM-014 — Workspace state machine

A `Workspace` MUST traverse the states `created` → `setup` → `ready` → `leased` → (optionally `merge-pending` → `conflict-resolving`) → (`merged` | `discarded`). The lifecycle state is orthogonal to the interrupt-state field (§4.10). See §7.1 for the transition table.

Tags: mechanism

#### WM-015 — Workspace lifecycle events

The workspace manager MUST emit the following events at the state transitions of §7.1: `workspace_created`, `workspace_leased`, `workspace_merge_pending`, `workspace_merged`, `workspace_discarded`, `workspace_interrupted`, `merge_conflict_escalation`. Each event payload MUST carry `workspace_id`, `run_id`, and the associated branch name. Payload schemas are declared in [docs/foundation/components.md §3.2] event-model; this spec is normative for WHEN each event fires.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### WM-016 — workspace_leased is emitted after metadata sidecar is written

The workspace manager MUST write the session-log directory and `harmonik.meta.json` sidecar per §4.7 BEFORE emitting `workspace_leased`. Emission order is: (a) worktree created, (b) branch created, (c) session-log directory and metadata sidecar written, (d) `workspace_leased` event emitted. S04's subsequent `agent_started` event confirms the handler is attached.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### WM-017 — Workspace events carry branch + commit correlators

`workspace_merged` MUST carry the merged commit hash and the surviving branch name. `workspace_discarded` MUST carry the discarded branch name. `workspace_interrupted` MUST carry the last-known durable state (the `Harmonik-State-ID` trailer of the tip commit per [execution-model.md §4.4.EM-017]).

Tags: mechanism

### 4.5 Merge back to integration

#### WM-018 — Merge-back is performed by a node in the same worktree

The merge-back operation (merging the task branch onto the integration branch) MUST be performed by a workflow node that executes INSIDE the run's already-leased worktree. The merge is NOT a new workspace; it is another node in the same run consuming the same lease per WM-010. A design that creates a new workspace for the merge step is forbidden.

Tags: mechanism

#### WM-019 — Task branch squash-merges onto integration branch with one commit per task

The merge-back operation MUST produce exactly one commit on the integration branch per completed task. The merge MAY be implemented as `git merge --squash` followed by a commit, or as an equivalent mechanism that preserves the one-commit-per-task contract. The integration-branch commit message MUST preserve the `Harmonik-Run-ID` and `Harmonik-Bead-ID` (when present) trailers per [execution-model.md §4.4.EM-017].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### WM-020 — Merge is not fast-forward-only

The merge MUST support real (non-fast-forward) merges, including merges that encounter conflicts. Conflict markers in the index are real and MUST be resolved per §4.6 before the merge commit is created.

Tags: mechanism

#### WM-021 — Merge outcome emits workspace_merged

On successful merge, the workspace manager MUST emit `workspace_merged` with the merged commit hash and surviving branch reference per WM-017. The workspace state MUST transition to `merged` per §7.1.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### 4.6 Conflict resolution

#### WM-022 — Original implementer resolves merge conflicts for MVH

When a merge-back operation produces conflicts, the resolution agent MUST be the ORIGINAL IMPLEMENTER — the agent whose work produced the divergent commits on the task branch. There is no dedicated "merge-agent" role in MVH; the implementer resumes in the same worktree and resolves conflicts in its own branch.

> RATIONALE: The implementer has the context (prompts, plan, intent) required to resolve conflicts correctly. A separate merge-agent would need to re-derive that context. See [docs/foundation/core-scope.md §3] for the resolution of the prior open question.

Tags: cognition
Axes: llm-freedom=bounded; io-determinism=best-effort; replay-safety=unsafe; idempotency=non-idempotent

#### WM-023 — Unresolvable conflicts escalate to human via merge_conflict_escalation

If the original implementer cannot resolve the merge conflicts within its budget or declares an unresolvable-conflict outcome, the workspace manager MUST emit `merge_conflict_escalation` carrying `workspace_id`, `run_id`, `branch_name`, and the conflict summary. The run MUST transition to `conflict-resolving` per §7.1 and await external resolution. Human review is the last-resort escalation path for MVH.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### WM-024 — Conflict-resolver dispatch is mechanism-tagged; resolution itself is cognition-tagged

The mechanism of dispatching the implementer agent back into the worktree to resolve a conflict is deterministic (mechanism-tagged, this spec owns it). The agent's reasoning during resolution is delegated to the implementer's handler per [docs/foundation/components.md §4] handler-contract (cognition-tagged; owned there). This spec names the delegation path: role = ORIGINAL IMPLEMENTER, model-class = the handler class that launched the implementer originally, input shape = (task branch, integration branch, conflict markers) plus the run's transition history.

Tags: mechanism

### 4.7 Session-log directory and metadata sidecar

#### WM-025 — Session-log directory layout

For every agent session launched against a workspace, a session-log directory MUST exist at `${workspace_path}/.harmonik/sessions/${session_id}/`. The directory is the join point for handler-written session logs (S04) and CASS-read metadata (S08). The workspace manager OWNS directory creation; handlers OWN log-file contents.

Tags: mechanism

#### WM-026 — Metadata sidecar file and required fields

The workspace manager MUST write a metadata sidecar file at `${workspace_path}/.harmonik/sessions/${session_id}/harmonik.meta.json` before the handler launches. The file MUST carry `run_id`, `node_id`, `handler_type`, `workflow_id`, `launched_at` (RFC 3339 timestamp), and `bead_id` (present iff the run is bead-tied per [execution-model.md §4.3.EM-014]). The metadata sidecar is the authoritative join key for CASS indexing.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### WM-027 — Metadata stamping precedes workspace_leased

The workspace manager MUST write the metadata sidecar BEFORE emitting `workspace_leased` per WM-016. This ordering ensures that any consumer of `workspace_leased` observing the handler launch can join metadata without racing the sidecar write.

Tags: mechanism

#### WM-028 — Bead-ID propagates into session metadata when present

When the run is tied to a bead, the metadata sidecar's `bead_id` field MUST carry the same value as the `Harmonik-Bead-ID` trailer on checkpoint commits per [execution-model.md §4.4.EM-017]. The `bead_id` field MUST be absent (or explicit null) when the run has no bead tie. CASS uses this metadata to join session logs to the Beads task ledger.

Tags: mechanism

#### WM-029 — Session-log directory is consumed read-only by S08

The session-log directory (including the metadata sidecar and handler-written logs) MUST be read-only from the memory-layer subsystem's perspective. S08 indexes contents into CASS without mutating any file under `${workspace_path}/.harmonik/sessions/`.

Tags: mechanism

#### WM-030 — Post-merge session-log retention default

On `workspace_merged`, the workspace manager MUST preserve the sessions directory inside the merged branch (i.e., the session logs remain in the integration-branch commit tree) by default. An operator-configured alternative MAY move the directory to a post-merge archive path; the default is preserve-in-merged-branch for audit retention.

Tags: mechanism

### 4.8 Failed-run worktree persistence

#### WM-031 — Failed-run worktrees persist until operator cleanup

A worktree whose run reached a terminal failure state (`failed` or `canceled` per [execution-model.md §7.1]) MUST persist on disk with its branch intact. The workspace manager MUST NOT auto-delete failed-run worktrees beyond the startup orphan sweep (§4.8.WM-033). Failed-run worktrees are retained for audit and post-mortem.

Tags: mechanism

#### WM-032 — Failed-run workspace state is discarded or interrupted, not merged

On terminal failure, the workspace state MUST transition to `discarded` (clean failure with preserved branch) or MUST set `interrupt_state` per §4.10 (interrupted mid-run). The branch MUST NOT be deleted; the branch reference in the run's bead history per [docs/foundation/components.md §9.5] preserves auditability.

Tags: mechanism

#### WM-033 — Startup orphan sweep removes stale worktree locks only

On daemon startup, the orphan sweep (per [docs/foundation/components.md §8.2 step 1a]) MUST remove stale worktree LOCK files (`.harmonik/lease.lock` or equivalent) from worktrees whose owning daemon is no longer running. The sweep MUST NOT delete worktree directories or branches. Full worktree cleanup is an operator-initiated workflow, not a startup action.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.9 Re-run rule

#### WM-034 — reopen-bead verdict triggers fresh worktree and fresh branch

When the investigator agent issues a `reopen-bead` verdict per [docs/foundation/components.md §9.5], the subsequent run for the same bead MUST receive a FRESH worktree at a new `<repo>/.harmonik/worktrees/<new_run_id>/` path and a FRESH task branch named `run/<new_run_id>` per §4.2.WM-005. The prior run's worktree and branch remain on disk per WM-031.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### WM-035 — Intra-run rollback verdicts keep the same worktree

Intra-run rollback verdicts — `resume-here`, `resume-with-context`, `reset-to-checkpoint` per [docs/foundation/components.md §9.5] — MUST keep the same worktree and the same task branch. The run's `run_id` is unchanged; the state reverts to the named checkpoint via git operations inside the existing worktree per [execution-model.md §4.10.EM-044].

Tags: mechanism

#### WM-036 — Re-run vs intra-run classification is deterministic on the verdict enum

The decision between "fresh worktree" (§4.9.WM-034) and "keep worktree" (§4.9.WM-035) MUST be deterministic on the verdict enum value. No cognition participates in the classification. The classification table is: `reopen-bead` → fresh worktree; `resume-here` | `resume-with-context` | `reset-to-checkpoint` → keep worktree; other verdicts (abandon, escalate) → no re-run attempted.

Tags: mechanism

### 4.10 Interrupt-state representation

#### WM-037 — interrupt_state is orthogonal to lifecycle state

The workspace record MUST carry an `interrupt_state` field orthogonal to the lifecycle state of §4.4. Values: `none` (default; no interruption), `operator-paused` (an operator pause interrupted the lease), `operator-stopped-graceful` (graceful stop in progress), `operator-stopped-immediate` (immediate stop; handler subprocess may have been killed), `daemon-crash-suspected` (pre-reconciliation placeholder for a run whose lease was not cleanly released). The lifecycle state (e.g., `leased`) and the interrupt-state (e.g., `operator-paused`) compose independently.

Tags: mechanism

#### WM-038 — interrupt_state is set by operator and reconciliation pathways

Transitions of `interrupt_state` are driven by (a) the operator control subsystem per [docs/foundation/components.md §7.3] on pause / stop commands; (b) reconciliation per [docs/foundation/components.md §9] on detecting a lost lease. The workspace manager owns the field's storage; it does NOT mutate the field except as directed by operator-control or reconciliation.

Tags: mechanism

#### WM-039 — workspace_interrupted event fires on interrupt_state transitions to non-none

On any transition of `interrupt_state` from `none` to a non-`none` value, the workspace manager MUST emit `workspace_interrupted` carrying `workspace_id`, `run_id`, the prior lifecycle state, and the new `interrupt_state` value. The payload shape is declared in [docs/foundation/components.md §3.2].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### WM-040 — interrupt_state reset requires reconciliation or operator resume

Clearing `interrupt_state` back to `none` MUST be driven by either (a) an `operator_resuming` event per [docs/foundation/components.md §7.3] for operator-initiated interrupts, or (b) a reconciliation verdict per [docs/foundation/components.md §9.5] for daemon-crash or lost-lease interrupts. The workspace manager MUST NOT silently clear the field.

Tags: mechanism

## 5. Invariants

#### WM-INV-001 — Lease-by-run

Every workspace MUST be leased by exactly one `Run` for that run's full lifetime. No agent MAY hold a workspace lease independent of its run context. This invariant underpins §4.3 and is the workspace-layer realization of the centralized-controller principle per [docs/foundation/components.md §1.8].

Tags: mechanism

#### WM-INV-002 — One run per bead at a time

For every bead, at most one run MAY be in flight at any instant. Multi-run-per-bead is allowed ACROSS time (per [execution-model.md §4.3.EM-014]) only after the prior run has reached a terminal state.

Tags: mechanism

#### WM-INV-003 — Checkpoint commits obey git append-only semantics on the task branch

Checkpoint commits on the run's task branch MUST follow standard git append-only semantics. No operation in this spec MAY rewrite committed history (amend, rebase, filter-branch) on a task branch that has emitted `workspace_leased`. Git's native commit semantics are the contract; the workspace spec adds no transactional or distributed-commit layer on top.

Tags: mechanism

#### WM-INV-004 — Merge-conflict resolver is the original implementer

For every merge conflict produced by §4.5 merge-back, the resolution agent MUST be the original implementer per §4.6.WM-022. No alternate resolver role exists in MVH. Escalation is to human (via `merge_conflict_escalation`) only after the implementer reports unresolvable.

Tags: mechanism

#### WM-INV-005 — Worktree path is canonical and derivable from run_id

Every live workspace MUST be located at `<repo>/.harmonik/worktrees/<run_id>/` per §4.1.WM-002, with only the worktree-root prefix operator-configurable. This invariant is the precondition for daemon-restart workspace rediscovery per WM-013.

Tags: mechanism

## 6. Schemas and data shapes

### 6.1 Record schemas

```
RECORD Workspace:
    workspace_id      : String               -- stable identifier; deterministic from run_id (WM-004)
    run_id            : UUID                 -- the run this workspace is leased to
    repository        : String               -- absolute path or URL of the backing repo
    parent_commit     : String               -- commit SHA this worktree was branched from
    branch_name       : String               -- task branch name per §4.2 (e.g., "run/<run_id>")
    path              : String               -- absolute filesystem path to the worktree
    state             : WorkspaceState       -- lifecycle state per §4.4
    interrupt_state   : InterruptState       -- orthogonal interrupt field per §4.10
    bead_id           : String | None        -- correlation field; present iff run is bead-tied
    metadata          : Map<String, String>  -- creation timestamp, operator fingerprint
    schema_version    : Integer              -- N-1 readable per §6.4
```

```
ENUM WorkspaceState:
    created
    setup
    ready
    leased
    merge-pending
    conflict-resolving
    merged
    discarded
```

```
ENUM InterruptState:
    none
    operator-paused
    operator-stopped-graceful
    operator-stopped-immediate
    daemon-crash-suspected
```

```
RECORD SessionMetadataSidecar:
    run_id         : UUID
    node_id        : String
    handler_type   : String
    workflow_id    : UUID
    bead_id        : String | None       -- present iff run is bead-tied
    launched_at    : Timestamp           -- RFC 3339 wall clock
    schema_version : Integer
```

### 6.2 Canonical on-disk paths

| Path | Owner | Purpose |
|---|---|---|
| `<repo>/.harmonik/worktrees/` | S06 | Worktree root (operator-configurable prefix). |
| `<repo>/.harmonik/worktrees/<run_id>/` | S06 | Per-run worktree directory. |
| `${workspace_path}/.harmonik/sessions/` | S06 | Session-log root within a worktree. |
| `${workspace_path}/.harmonik/sessions/<session_id>/` | S06 creates; S04 writes logs | Per-session directory. |
| `${workspace_path}/.harmonik/sessions/<session_id>/harmonik.meta.json` | S06 | Metadata sidecar per §4.7. |
| `${workspace_path}/.harmonik/sessions/<session_id>/session.log` | S04 | Handler-written session log (format per-handler). |
| `${workspace_path}/.harmonik/lease.lock` | S06 | Lease lock file; removed by startup orphan sweep if stale (§4.8.WM-033). |

### 6.3 Lifecycle event emission rules

Payload schemas are declared in [docs/foundation/components.md §3.2] event-model. This spec is normative for WHEN each event fires:

- `workspace_created` — emitted after `git worktree add` succeeds and the branch exists per §4.1.
- `workspace_leased` — emitted after the session-log directory and metadata sidecar are written per WM-016; indicates the workspace is ready for a handler launch.
- `workspace_merge_pending` — emitted on transition into `merge-pending` per §7.1.
- `workspace_merged` — emitted on successful merge per §4.5.WM-021; payload carries merged commit hash and surviving branch name.
- `workspace_discarded` — emitted on transition into `discarded` per §7.1.
- `workspace_interrupted` — emitted on `interrupt_state` transition from `none` per §4.10.WM-039.
- `merge_conflict_escalation` — emitted when merge conflicts are unresolvable by the implementer per §4.6.WM-023.

### 6.4 Schema evolution

`Workspace` and `SessionMetadataSidecar` records carry a `schema_version` integer. The compatibility contract is N-1 readable per [docs/foundation/components.md §7.5]: a reader MUST accept the immediately prior schema version. Additive changes (new optional field) are non-breaking and bump the version; renaming or removing fields is breaking and requires a migration release.

## 7. Protocols and state machines

### 7.1 Workspace lifecycle state machine

| From | Event | Guard | To | Emits |
|---|---|---|---|---|
| (initial) | orchestrator issues create | run_id unique, path free | `created` | `workspace_created` |
| `created` | git worktree add succeeds | branch checkout clean | `setup` | — |
| `setup` | metadata sidecar written | sidecar file present per §4.7 | `ready` | — |
| `ready` | handler launch imminent | run_id held by orchestrator | `leased` | `workspace_leased` |
| `leased` | run enters merge node | task branch ready to merge | `merge-pending` | `workspace_merge_pending` |
| `merge-pending` | merge succeeds | no conflicts OR conflicts resolved | `merged` | `workspace_merged` |
| `merge-pending` | merge conflicts detected | conflicts present | `conflict-resolving` | — |
| `conflict-resolving` | implementer resolves | resolution commit exists | `merge-pending` | — |
| `conflict-resolving` | implementer reports unresolvable | budget exhausted | `discarded` | `merge_conflict_escalation`, `workspace_discarded` |
| `leased` | run reaches terminal failure | run state = `failed` or `canceled` | `discarded` | `workspace_discarded` |
| any | operator pause/stop OR lost-lease detection | see §4.10.WM-038 | (same state, interrupt_state != none) | `workspace_interrupted` |

> INFORMATIVE: The `interrupt_state` field (§4.10) composes with the lifecycle state; it does NOT replace it. A `leased` workspace interrupted by operator pause is `leased + operator-paused`, not a new lifecycle state.

### 7.2 Worktree-create-and-stamp sequence (protocol pseudocode)

```
FUNCTION create_workspace(run_id, parent_commit, bead_id | None):
    path = worktree_root() + "/" + run_id
    branch = "run/" + run_id
    IF path already exists:
        RETURN ERROR(WorkspaceAlreadyExists)
    git.worktree_add(path, branch, start_point=parent_commit)
    emit_event("workspace_created", workspace_id, run_id, path, branch)
    sessions_dir = path + "/.harmonik/sessions"
    mkdir(sessions_dir)
    RETURN Workspace(workspace_id=derive(run_id), run_id, path, branch, state=ready)

FUNCTION stamp_session_metadata(workspace, session_id, node_id, handler_type, workflow_id):
    session_dir = workspace.path + "/.harmonik/sessions/" + session_id
    mkdir(session_dir)
    sidecar = SessionMetadataSidecar(
        run_id=workspace.run_id, node_id=node_id,
        handler_type=handler_type, workflow_id=workflow_id,
        bead_id=workspace.bead_id, launched_at=now(),
        schema_version=CURRENT_SCHEMA_VERSION)
    write_json(session_dir + "/harmonik.meta.json", sidecar)
    emit_event("workspace_leased", workspace.workspace_id, workspace.run_id,
               branch=workspace.branch_name)
    workspace.state = leased
```

Every branch point above corresponds to a normative requirement: worktree-add (§4.1.WM-003), path convention (§4.1.WM-002), branch naming (§4.2.WM-005), metadata sidecar write (§4.7.WM-026), lease emission ordering (§4.4.WM-016).

## 9. Cross-references

### 9.1 Depends on

- **[execution-model.md §4.3]** — `Run` record; `run_id` is the workspace lease key.
- **[execution-model.md §4.4]** — checkpoint commits land on the task branch defined in §4.2; trailer schema establishes `Harmonik-Run-ID` + `Harmonik-Bead-ID` contracts this spec propagates into merges (§4.5.WM-019) and metadata (§4.7.WM-028).
- **[execution-model.md §4.7]** — state reconstruction from git + Beads; WM-004 relies on this to derive `workspace_id` on restart.
- **[execution-model.md §4.10]** — backtracking representation; intra-run rollbacks (§4.9.WM-035) emit `rollback_to_state_id` transitions per EM-044.
- **[docs/foundation/components.md §1.1]** — four-axis classification; workspace operations are mechanism-tagged by default, conflict-resolution is cognition-tagged.
- **[docs/foundation/components.md §1.8]** — centralized-controller principle; lease-by-run invariant WM-INV-001 is the workspace-layer realization.
- **[docs/foundation/components.md §3.2]** — event-model taxonomy; lifecycle events (§6.3) have payload schemas there.
- **[docs/foundation/components.md §4]** — handler-contract; S04 writes session logs per §4.7; `workspace_leased` precedes handler launch per WM-016.
- **[docs/foundation/components.md §7.3]** — operator-nfr; operator pause/stop drives `interrupt_state` (§4.10).
- **[docs/foundation/components.md §7.5]** — compatibility contract; schema evolution (§6.4) follows N-1 readable.
- **[docs/foundation/components.md §8.2]** — process-lifecycle startup orphan sweep consumed by §4.8.WM-033.
- **[docs/foundation/components.md §9]** — reconciliation; verdict vocabulary drives §4.9 re-run rule.
- **[docs/foundation/components.md §10.3]** — beads-integration; parent-child edge drives §4.2.WM-006 integration-branch selection; `bead_id` correlation (§4.1.WM-001, §4.7.WM-028).

### 9.2 Reverse dependencies

> INFORMATIVE: Reverse dependencies are computed on demand from the foundation corpus. Populated at finalize.

### 9.3 Co-references (read-only consumption)

- **[execution-model.md §6.1 Run]** — this spec's §4.1.WM-001 consumes the `Run` shape defined there; no reverse dependency.
- **[docs/foundation/components.md §5.3a Session-log pipeline]** — S06 side of the three-subsystem pipeline is owned here (§4.7); S04 emission side is owned in handler-contract; S08 ingestion side is owned in memory-layer.
- **[docs/foundation/components.md §9.5 Verdict vocabulary]** — re-run rule (§4.9) consumes the verdict enum declared there.

## 10. Conformance

### 10.1 Conformance profiles

**Core MVH.** An implementation conforming to Core MVH MUST pass every requirement in WM-001 through WM-040 and every invariant WM-INV-001 through WM-INV-005. No requirement is deferred at MVH.

**Post-MVH extensions.** Adze environment provisioning (currently out of scope per §2.2), operator-configured post-merge archive paths (currently the non-default branch of §4.7.WM-030), and automated merge-agent dispatch (currently forbidden in MVH per §4.6.WM-022) are additive extensions.

### 10.2 Test-surface obligations

During bootstrap (before `testing.md` exists) test obligations are named in prose. Each requirement's test obligation:

- **WM-001 — WM-004 (worktree primitive).** Filesystem-scenario tests: `git worktree add` runs produce the canonical path; `workspace_id` is derivable from `run_id` after daemon restart.
- **WM-005 — WM-009 (branch naming).** Branch-naming unit tests: task-branch and integration-branch templates produce expected names; small-scope-collapse path tested with no-parent-bead scenario.
- **WM-010 — WM-013 (lease model).** Multi-agent-sequential scenario tests: three agents run in sequence in one worktree; concurrency check rejects a second active agent.
- **WM-014 — WM-017 (lifecycle events).** Event-emission unit tests: every state transition in §7.1 emits the expected event; `workspace_leased` payload contains metadata sidecar pointer.
- **WM-018 — WM-021 (merge-back).** Merge-back scenario tests: one-commit-per-task on integration branch; trailer propagation verified; non-fast-forward merges succeed.
- **WM-022 — WM-024 (conflict resolution).** Cognition-tagged integration tests with twin implementer: synthetic merge conflict triggers implementer re-dispatch; unresolvable case emits `merge_conflict_escalation`.
- **WM-025 — WM-030 (session-log).** Filesystem tests: metadata sidecar exists before `workspace_leased`; CASS (S08) reads sidecar + log; post-merge retention default verified.
- **WM-031 — WM-033 (failed-run persistence).** Daemon-restart scenario tests: failed-run worktrees persist across restart; orphan sweep removes only stale locks.
- **WM-034 — WM-036 (re-run rule).** Verdict-driven scenario tests: `reopen-bead` creates fresh worktree at `run/<new_run_id>`; `reset-to-checkpoint` keeps worktree.
- **WM-037 — WM-040 (interrupt-state).** Operator-pause and crash-recovery scenario tests: `interrupt_state` composes with lifecycle state; `workspace_interrupted` event fires; clearing requires operator resume or reconciliation verdict.

Migration to `[testing.md §<layer>]` cross-references occurs within one revision cycle once testing.md lands; this obligation is tracked in OQ-WM-001.

### 10.3 Excluded conformance claims

- This spec does NOT grant conformance over: handler session-log FORMAT (per-handler; owned by [docs/foundation/components.md §4]); CASS indexing (owned by the memory-layer spec); event payload shapes (owned by event-model); operator-CLI surface for `harmonik workspace` subcommands (deferred).
- This spec does NOT guarantee performance or throughput bounds on `git worktree add`; those are operator-observable per [docs/foundation/components.md §7.8].

## 11. Open questions

#### OQ-WM-001 — Migrate test-obligation prose to testing.md references

Question: §10.2 currently names test obligations in prose. The template §10.2 expects cross-references to `[testing.md §<layer>]` once testing.md lands.
Owner: foundation-author
Blocks: none (MVH prose obligations are in place)
Default-if-unresolved: Keep prose obligations; migrate within one revision cycle after testing.md is finalized.

#### OQ-WM-002 — Operator configurability of integration-branch template

Question: §4.2.WM-006 declares a default integration-branch name derived from parent-bead ID (`harmonik/integration/<parent_bead_id>`) with the template operator-configurable. The precedence layer for the template (per [docs/foundation/components.md §6.8]) and its change-takes-effect semantics are not yet specified.
Owner: foundation-author
Blocks: WM-006 clarity for operators configuring multiple parallel integration branches
Default-if-unresolved: Template lives at the operator-policy precedence layer; change takes effect on next operator pause per the general config-reload rule.

#### OQ-WM-003 — Post-merge session-log archival policy

Question: §4.7.WM-030 pins preserve-in-merged-branch as the default; an operator-configured alternative moves the directory to a post-merge archive path. The archive path's retention policy (N runs, size cap, GC cadence) is not specified.
Owner: foundation-author
Blocks: none (MVH default is preserve-in-branch; archival is post-MVH config)
Default-if-unresolved: Preserve-in-branch is the only supported shape for MVH. Archive-path configuration is post-MVH.

#### OQ-WM-004 — interrupt_state granularity for mixed operator + crash paths

Question: §4.10.WM-037 enumerates five `interrupt_state` values. A workspace interrupted first by operator pause and then by a daemon crash while paused produces a layered interrupt history the single-value field does not capture.
Owner: foundation-author
Blocks: none (MVH: last-wins overwrite)
Default-if-unresolved: Last-wins overwrite; the prior interrupt is recoverable from the `workspace_interrupted` event history in JSONL but not from the workspace record itself. Revisit if the operator-observability spec demands richer structure.

## 12. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-04-23 | 0.1.0 | foundation-author | Initial draft. Requirements-first shape; 40 requirements, 5 invariants, 4 open questions. Bootstrap citations into docs/foundation/components.md for specs not yet finalized. |

## A. Appendices

### A.3 Rationale

**Why lease-by-run and not lease-by-agent.** Agent sessions are ephemeral (minutes to hours); runs span the full arc of work on a bead. Leasing by agent would require re-acquisition handshakes every time a new agent takes over in the same worktree, and would introduce state-transfer overhead (committed files in an uncommitted-to-branch state, lease-handover events). Leasing by run lets the worktree be a passive surface that agents step into and out of; the run is the authority on when the lease ends. See [docs/foundation/components.md §1.8] for the centralized-controller framing.

**Why the original implementer resolves merge conflicts.** A merge conflict requires the context of (a) the implementer's plan, (b) the implementer's understanding of the competing change, and (c) ongoing intent. A separate merge-agent would need to rebuild this context from session logs and commit messages, which is both expensive and drift-prone. The implementer already has the context in its running session. Escalation to human is reserved for the case where the implementer cannot resolve within its budget — the failure mode is already a reasoning failure; adding another reasoning layer between it and human review would be cruft. Resolved for MVH in [docs/foundation/core-scope.md §3].

**Why the worktree path is canonical and not registry-driven.** A separate workspace registry (e.g., a SQLite table mapping run_id → path) would duplicate state that is already implicit in the filesystem. Daemon restart must rediscover workspaces anyway; a registry-backed path adds a source of drift without adding information. The canonical-path convention (WM-002) makes the workspace ID derivable from the run ID, which in turn is already recoverable from git per [execution-model.md §4.7]. One source of truth, no registry.

**Why interrupt-state is orthogonal to lifecycle state.** Interrupt events can occur at any lifecycle state (a workspace in `setup` can be interrupted by operator stop just as one in `leased` can). Encoding interrupts as lifecycle states would multiply the state space (`leased` × `operator-paused`, `merge-pending` × `daemon-crash-suspected`, etc.). An orthogonal field keeps the lifecycle graph small and delegates interrupt-aware handling to the subsystems that own interrupts (operator-control, reconciliation). The lifecycle state answers "where is this workspace in its work"; `interrupt_state` answers "has something disrupted that work."

**Why failed-run worktrees persist.** Post-mortem analysis, audit, and the improvement loop all need access to the state of the workspace at the moment of failure. Auto-deletion would destroy evidence. Disk pressure is a known operator concern and is handled by operator-initiated cleanup workflows (per [docs/foundation/components.md §5.6]) rather than by the workspace manager's automatic behavior. MVH errs on the side of preservation; post-MVH MAY introduce configurable retention windows.
