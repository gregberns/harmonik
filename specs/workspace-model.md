# Workspace Model

```yaml
---
title: Workspace Model
spec-id: workspace-model
requirement-prefix: WM
status: reviewed
spec-shape: requirements-first
spec-category: runtime-subsystem
version: 0.4.5
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-05-13
depends-on:
  - architecture
  - execution-model
  - handler-contract
  - control-points
  - reconciliation
  - operator-nfr
  - process-lifecycle
  - beads-integration
---
```

> NOTE (v0.4.0): `event-model` is intentionally NOT in `depends-on` despite heavy citation; the WM ↔ EV relationship is a consume-produce pair resolved directionally per the EM/EV precedent. WM emits events whose payload shapes EV owns (per EV-025); `event-model` appears in §9.3 co-references. Adding it to `depends-on` would create a cycle since EV's own `depends-on` lists `workspace-model`.
>
> NOTE (v0.4.0 ID FREEZE): As of v0.4.0 status transition to `reviewed`, all WM-NNN requirement IDs, WM-INV-NNN invariant IDs, and OQ-WM-NNN open-question IDs are FROZEN at this revision. Retired IDs (WM-017, WM-INV-004) remain retired and MUST NOT be reused. Future revisions MUST NOT renumber; additions take the next free ID. This freeze applies corpus-wide to peer cites.

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
- Session-log directory layout and the `harmonik.meta.json` metadata sidecar (S06 side of the pipeline; the S04 emission side is [handler-contract.md §4.2 HC-010]).
- Failed-run worktree persistence — no auto-cleanup beyond the startup orphan sweep.
- Re-run vs intra-run-loop distinction at the workspace layer.
- Interrupt-state representation as an orthogonal field on the workspace record.

### 2.2 Out of scope

- Handler session-log emission (writing the log file) — owned by [handler-contract.md §4.2 HC-010]; S04 emits. The `session_log_location` progress-stream message contract lives there.
- CASS ingestion of session-log directories — owned by the memory-layer spec (S08 subsystem, deferred); this spec only declares the on-disk layout S08 reads.
- Beads CLI adapter, bead-status writes — owned by [beads-integration.md §4.2]. This spec only carries `bead_id` as an opaque correlation field.
- Reconciliation verdicts that trigger workspace actions (`reopen-bead`, `reset-to-checkpoint`, etc.) — owned by [reconciliation/spec.md §4.5 RC-020] and [reconciliation/schemas.md §6.1 Verdict]. This spec names WHAT workspace action each verdict triggers; reconciliation owns the verdict vocabulary, including any future additions.
- Orchestrator loop that issues workspace create/lease requests — owned by [execution-model.md §4.3] and the S01 subsystem spec.
- Event type names AND payload shapes — owned by [event-model.md §8.5]. This spec declares emission obligations (WHEN each event fires); event-model declares the canonical event-type names (WHAT they are called) and payload field schemas (WHAT they carry) per EV-025.
- `LaunchSpec` composition — owned by [handler-contract.md §6.1]. WM contributes the `workspace_path` field value; HC owns the full record schema and budget rules.
- Cross-subsystem schema-version ceiling — owned by [operator-nfr.md §4.5 ON-018]. §6.4 of this spec observes the N-1 contract; it does not set it.
- Operator control (pause / stop / upgrade) state machine and events — owned by [operator-nfr.md §4.3] and [event-model.md §8.7]. This spec names WHICH workspace states are affected and declares the orthogonal `interrupt_state` projection; operator-nfr owns the operator-control vocabulary itself.
- Adze environment provisioning — not in MVH; MVH workspaces are plain `git worktree add` (see [docs/foundation/core-scope.md §3]).
- Git-Large-File-Storage (LFS)-backed blobs, git submodules, and bare-repository backing layouts — all out-of-scope for MVH per OQ-WM-018. MVH assumes a plain single-repo local clone with regular blobs. LFS smudge/clean filters may execute at `git worktree add` time under operator configuration; harmonik does not currently coordinate with the LFS endpoint and a repository with mandatory LFS smudge may experience unexpected checkout latency that harmonik reports as a slow `created` state (see §10.3 performance note). Submodules within a task worktree are likewise uncoordinated — harmonik neither initializes nor updates submodules as part of lease acquisition. Bare repositories are not supported as the backing repo; WM-002's `repository` field MUST reference a working-tree clone, not a bare `.git` directory. Post-MVH support is tracked in OQ-WM-018.

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
- **original implementer** — the handler-class lineage (not the process-identity) recorded by the workspace record's `implementer_handler_ref` field and identified mechanically at merge-pending entry; see §4.6.WM-022 for the identification rule. MVH's merge-conflict resolver re-dispatches a fresh session of that handler class.
- **session** — a single handler subprocess launch against a workspace, keyed by a per-launch `session_id`. A workspace hosts many sessions sequentially over its run's lifetime; each session has its own directory and metadata sidecar (§4.7). The `session_id` is minted by the workspace manager at session start; its shape and assignment rule are in [handler-contract.md §4.2].
- **lease lock** — a structured lock file at the canonical path of §6.2 whose presence represents the lease and whose contents identify the owning run and daemon generation. See §4.3.WM-013a.
- **worktree-lock disjoint** — harmonik never issues `git worktree lock` on its own worktrees. The lease-lock file (§4.3.WM-013a) is a HARMONIK-LAYER mechanism and is disjoint from git's own `git worktree lock` feature, which is an OPERATOR-LAYER mechanism harmonik respects but does not use. An operator-issued `git worktree lock` on a harmonik-managed worktree causes `git worktree prune` (§4.8.WM-033) to skip the locked entry on the next sweep, which is safe: harmonik does not depend on pruning locked entries. See §4.8.WM-033 and I5 of the v0.4.0 revision history.
- **scratch merge-worktree** — a short-lived worktree at `<repo>/.harmonik/worktrees/merge-<merge_id>/` created by the merge node per §4.5.WM-019a option (b) to execute the squash-merge without contending with operator activity in the main worktree. Not leased, no lease-lock, not subject to §4.3 lifecycle.
- **orphan evidence types** — filesystem conditions detected by §4.3.WM-013c / §4.1.WM-003a and routed to reconciliation Cat 3. The defined types are `bare-worktree-no-lease` (registered worktree, no lease-lock, no sessions) and `sidecar-without-lease` (registered worktree, sidecar present, no lease-lock).

## 4. Normative requirements

### 4.a Subsystem envelope

#### WM-ENV-001 — Envelope declaration

Envelope for the workspace-manager subsystem (S06) per [architecture.md §4.0 AR-053].

(a) Events produced:
  - `workspace_created` — emission rule §4.4.WM-015; payload schema in [event-model.md §8.5.1].
  - `workspace_leased` — emission rule §4.4.WM-016; payload schema in [event-model.md §8.5.2].
  - `workspace_merge_status` — emission rule §4.5.WM-021 (paired-phase single event with `status ∈ {pending, merged}` per [event-model.md §8.9(h)]); payload schema in [event-model.md §8.5.3].
  - `workspace_discarded` — emission rule §4.8.WM-032; payload schema in [event-model.md §8.5.4].
  - `merge_conflict_escalation` — emission rule §4.6.WM-023; payload schema in [event-model.md §8.5.6].

(b) Events consumed:
  - `session_log_location` — announces handler-side observability of the session-log directory that WM pre-created per §4.7. Emission by handler watcher per [handler-contract.md §4.2 HC-010]; payload schema in [event-model.md §8.3.7].
  - `operator_pausing`, `operator_paused`, `operator_stopping`, `operator_stopped`, `operator_resuming` — drive §4.10 `interrupt_state` transitions per [event-model.md §8.7] and [operator-nfr.md §4.3].
  - `reconciliation_verdict_executed` — the workspace-manager side of verdict execution per [reconciliation/schemas.md §6.2 Verdict-execution table] routes on §4.9.WM-036.
  - `workspace_interrupted` — WM observes but does not emit this event; the emitter is the reconciliation detector per [event-model.md §8.5.5] (see OQ-WM-006 for unresolved emitter-identity with operator-driven interrupt paths).

(c) Types introduced (cross-subsystem):
  | Type | `Tags:` | `Axes:` (if non-baseline) |
  |---|---|---|
  | `Workspace` (§6.1) | mechanism | baseline |
  | `WorkspaceState` (§6.1 ENUM) | mechanism | baseline |
  | `InterruptState` (§6.1 ENUM) | mechanism | baseline |
  | `SessionMetadataSidecar` (§6.1) | mechanism | `io-determinism=deterministic; idempotency=non-idempotent` (on-disk artifact) |
  | `LeaseLockFile` (§6.1) | mechanism | `io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `WorkspaceRef` (consumed from [execution-model.md §6.1]) | mechanism | baseline |

(d) Handlers implemented: none. WM does not expose a handler; it is the orchestrator-side subsystem (S06) that pre-creates the worktree before S04 launches handlers.

(e) State owned:
  - `Workspace` record (§6.1) — keyed by `run_id`.
  - `LeaseLockFile` (§6.1) — per-workspace filesystem artifact; birth at `leased`, death at `merged`/`discarded` per §4.3.WM-013b.
  - Session-log directories and metadata sidecars under `${workspace_path}/.harmonik/sessions/` (§4.7).
  - `Run` record is consumed but NOT owned here ([execution-model.md §6.1]).

(f) Control points provided: none. The workspace-manager is a mechanism-tagged subsystem; its operations are not gate/hook/guard/budget points per [control-points.md §4.1].

(g) NFRs inherited / overridden:
  - Inherited: `ON-018` N-1 schema compatibility (§6.4 applies it to `Workspace` and `SessionMetadataSidecar`).
  - Inherited: `ON-027` graceful-shutdown ordering (workspaces in `leased`/`merge-pending` drain before daemon exit; WM participates in the drain as a file-system-consistency actor).
  - Overridden: none.

(h) Boundary classification per operation:
  | Operation | `Tags:` | Axes |
  |---|---|---|
  | `create_workspace` (§7.2) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `stamp_session_metadata` (§7.2) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `acquire_lease` (§4.3) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `release_lease` (§4.3) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `merge_back` (§4.5) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `detect_git_version` (§4.a WM-ENV-002) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `ensure_gitignore_hygiene` (§4.3.WM-013e) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `redispatch_implementer_for_merge_conflict` (§4.6) | cognition | `llm-freedom=bounded; io-determinism=best-effort; replay-safety=unsafe; idempotency=non-idempotent` |

(i) Environment pins:
  - Git minimum version 2.34 per WM-ENV-002 (detected at daemon startup; fail-fast if below).

Tags: mechanism

#### WM-ENV-002 — Minimum git version pin

The workspace manager requires git version 2.34 or newer. The pin derives from three mechanical dependencies: (i) `git merge --strategy=ort` is the default merge algorithm only from git 2.34 onward, and WM-019 explicitly specifies `--strategy=ort` to fix the merge-conflict semantics across sites; (ii) `git for-each-ref --format '%(trailers:key=X,valueonly=true)'` — used by corpus tooling to read checkpoint trailers on task branches — requires git 2.34's expanded `trailers` format token; (iii) `git worktree repair` (introduced in git 2.30 but stabilized in 2.34) is the supported recovery path for a daemon whose `.git/worktrees/` metadata has diverged from the filesystem after a reboot or restore.

The daemon MUST detect the installed git version at startup by parsing `git --version` and MUST refuse to start if the detected version is below 2.34, surfacing a typed `GitVersionTooOld` error (§8). Detection at startup (versus per-operation) is chosen so the failure is surfaced ONCE at daemon-start rather than at an arbitrary later checkout. Coordinated version-pin declaration across subsystems (analogous to PL-021's ntm-version pin) is tracked as OQ-WM-015 for bilateral alignment with [process-lifecycle.md §4.x]. WM v0.4.0's pin is authoritative for WM's own operations; downstream subsystems with narrower version needs MAY require a higher floor.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.1 Worktree primitive

#### WM-001 — Workspace type and required fields

A `Workspace` MUST carry `workspace_id` (stable across restarts), `run_id` (the run this workspace is leased to), `repository` (absolute path to the local clone of the backing repo — see WM-002 for the repo-root-locality rule), `parent_commit` (the commit the workspace was branched from), `branch_name` (per §4.2), `path` (absolute filesystem path to the worktree directory), `state` (per §4.4), `interrupt_state` (per §4.10), `metadata` (a closed map with exactly the keys `created_at` (RFC 3339) and `operator_fingerprint`; additional keys are forbidden at MVH), `implementer_handler_ref` (optional; the `handler_ref` of the agentic node whose commits are the most recent writes on the task branch at merge-pending entry per §4.6.WM-022; null when the task branch carries no agentic-node commits), `schema_version` (per §6.4; set by the workspace manager on create), and an optional `bead_id` (opaque correlation field per [beads-integration.md §4.6 BI-017, BI-018]).

Tags: mechanism

#### WM-002 — Worktree path convention

Every workspace MUST be created at the canonical path `<repo>/.harmonik/worktrees/<run_id>/`, where `<repo>` is the absolute path to the **local clone** of the backing repository and `<run_id>` is the run's stable identifier. The daemon operates on a local clone only; workspaces MUST NOT be materialized against a bare remote URL. The worktree root (`<repo>/.harmonik/worktrees/`) MAY be overridden by operator configuration; the per-run subdirectory `<run_id>/` is fixed. The worktree-root override precedence layer is the operator-policy config surface per [control-points.md §4.7 CP-037]; change takes effect at the next operator pause boundary per [operator-nfr.md §4.3].

`run_id` MUST match the filesystem-safe regex `[A-Za-z0-9-]+` (UUIDv7 satisfies this by construction); post-MVH ID-scheme extensions MUST preserve this invariant or declare an escape rule before adoption.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### WM-002a — Tmux window-name convention

Every agent process that the daemon launches under the tmux substrate (per [process-lifecycle.md §4.7 PL-021b]) MUST occupy a tmux window whose name is derived by the pure function:

    window_name(bead_id, phase, iteration_count) =
        bead_id                                  when phase = "single"
        bead_id + "/i" + dec(iteration_count)    when phase = "implementer"
        bead_id + "/r" + dec(iteration_count)    when phase = "reviewer"

where `bead_id` is the run's bound bead identifier ([beads-integration.md §4.6 BI-017]), `phase ∈ {single, implementer, reviewer}` is the workflow-graph node category at dispatch time, `iteration_count` is the 1-based review-loop turn (always 1 for `phase = single`), and `dec` is base-10 with no leading zeros.

The tmux session that contains these windows is named per [process-lifecycle.md §4.2 PL-006a] (`harmonik-<project_hash>`) and is referenced as `provenance.TmuxSessionName` at the substrate boundary — window names are scoped within that session and MUST NOT be assumed unique across sessions.

When the daemon runs in the PL-021b `$TMUX`-reuse mode (operator's session, `owns_session=false`), the window name MUST be prefixed with `hk-<hash6>-` where `<hash6>` is the first 6 hex chars of the project hash, yielding e.g. `hk-a1b2c3-<bead_id>` or `hk-a1b2c3-<bead_id>/i2`. The prefix preserves the sweep-sentinel invariant required by PL-021c.

**Replay determinism.** Given identical `(bead_id, phase, iteration_count, project_hash, owns_session)` inputs the function MUST produce a byte-identical window name across daemon restarts, host migrations, and replayed scenario runs. The substrate adapter MUST NOT inject wall-clock components, PIDs, run_ids, or random suffixes into the window name. Collision with an already-existing window of the same name inside the project's tmux session is a fail-fast `WindowNameCollision` error (mapped to PL-006 orphan-sweep coverage — a colliding window from a prior daemon instance is by construction an orphan and is reaped before any new spawn).

**Truncation rule.** Tmux imposes no hard window-name length cap, but operator readability and the 80-column status line do. If `len(window_name) > 64` bytes after construction, the adapter MUST truncate `bead_id` (NOT the suffix) by retaining the leading 56 bytes and appending an 8-byte lowercase-hex prefix of `SHA-256(bead_id)`, yielding a name of the form `<bead_id[:56]>~<hash[:8]><suffix>`. The `~` separator is unreserved in tmux window names and unambiguous with `/` from the iteration suffix.

Cross-references: [process-lifecycle.md §4.7 PL-021b] (pane creation primitive that consumes this name); [process-lifecycle.md §4.2 PL-006a] (project-hash session scoping); WM-002 (sibling worktree-path determinism rule whose discipline this clause mirrors); [beads-integration.md §4.6 BI-017] (`bead_id` provenance).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### WM-003 — Worktree creation uses `git worktree add -b`

The workspace manager MUST create a worktree and a fresh task branch atomically via `git worktree add -b <branch> <path> <parent_commit>` against the backing repository, where `<branch>` is the task branch from §4.2.WM-005, `<path>` is the canonical path from WM-002, and `<parent_commit>` is the explicit start-point commit SHA (the `parent_commit` field on the `Workspace` record per §6.1). The `-b` form is REQUIRED because the task branch does not yet exist at worktree-create time; `git worktree add <path> <branch>` (no `-b`) requires the branch to pre-exist and will fail with `fatal: invalid reference: <branch>`. An explicit `<parent_commit>` start-point is REQUIRED to pin the branch base deterministically; omitting it would default to HEAD, which races with operator activity in the main worktree. No provisioning layer (adze, devbox, container build) participates in MVH worktree creation; the worktree is a plain subfolder. Git minimum-version pin per WM-ENV-002.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### WM-003a — Discovery classifies partially-crash-ed worktree states

If daemon startup (or reconciliation) observes a directory under `<repo>/.harmonik/worktrees/<run_id>/` that `git worktree list --porcelain` reports as a registered worktree but that carries NO lease-lock file (`${workspace_path}/.harmonik/lease.lock` absent per §4.3.WM-013a) AND NO session-log directory (`${workspace_path}/.harmonik/sessions/` absent), it MUST be classified under the evidence type `bare-worktree-no-lease` and routed to reconciliation Cat 3 per [reconciliation/spec.md §8]. If the same worktree carries a sidecar (`.harmonik/sessions/<session_id>/harmonik.meta.json` present) but NO lease-lock, it MUST be classified under the evidence type `sidecar-without-lease` and routed to reconciliation Cat 3. Both cases arise from a SIGKILL / power loss between `git worktree add` and the lease-lock fsync gate of WM-016; neither the `leased` nor any post-`ready` event has been durably emitted, so the run's workflow state treats the workspace as non-existent while the filesystem retains an orphan. Cat 3 routing gives reconciliation the right to propose `reopen-bead` (discard the partial worktree, fresh run_id per WM-034) or `accept-close-with-note` with operator cleanup.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### WM-004 — Workspace ID is stable across restarts

The `workspace_id` MUST be generated by the deterministic construction `workspace_id = "ws-" + run_id` so that a daemon restart finds the same `workspace_id` for a given run without consulting a separate store. State reconstruction uses git + Beads per [execution-model.md §4.7]; the workspace ID MUST be derivable from those sources. `workspace_id` MUST be treated as opaque by event consumers — downstream subsystems MUST NOT parse the prefix or the embedded `run_id` from the string; the correlation field for joins is `run_id`, which is carried explicitly in every event payload per [event-model.md §8.5].

Tags: mechanism

### 4.2 Branch naming

#### WM-005 — Task branch naming convention

Every run's task branch MUST be named `run/<run_id>`. The branch is created off the current integration branch (§4.2.WM-006) at worktree-create time. Checkpoint commits per [execution-model.md §4.5 EM-023] land on this branch only.

Tags: mechanism

#### WM-005a — Sub-workflow expansion does not create additional task branches

Sub-workflow expansion per [execution-model.md §4.8 EM-034] does NOT create additional task branches or workspaces. All checkpoint commits produced by an expanded sub-workflow's nodes MUST land on the parent run's task branch per [execution-model.md §4.5 EM-023] and [execution-model.md §4.8 EM-035]. The workspace is leased by the parent run; nested execution occupies the same worktree. A sub-workflow that attempts to materialize its own workspace is a foundation-amendment-requiring change to this rule.

Tags: mechanism

#### WM-006 — Integration branch naming convention

The default integration branch MUST be named `harmonik/integration`. When a run has a parent-bead context visible to the dependency-graph query per [beads-integration.md §4.5 BI-014], its task branch MUST target a derived branch named `harmonik/integration/<parent_bead_id_refsafe>`, where `<parent_bead_id_refsafe>` is the bead ID transformed to satisfy git's ref-name constraints per WM-006a. The exact transformation template is operator-configurable per OQ-WM-002; the default is verbatim bead-ID substitution. When a run has no parent-bead context, its merge target is determined by WM-008.

Tags: mechanism

#### WM-006a — Ref-safe bead-ID substitution

When substituting `<parent_bead_id>` into a git ref name (WM-006's integration-branch template), the workspace manager MUST delegate ref-name validation to `git check-ref-format(1)` rather than attempting an independent character-class enumeration. Concretely: after constructing the proposed branch name (`harmonik/integration/<parent_bead_id>` with `<parent_bead_id>` substituted verbatim), the workspace manager MUST invoke `git check-ref-format refs/heads/<proposed>`; a zero exit code means the name is accepted verbatim, and a non-zero exit code means a canonical fallback transformation MUST be applied and re-validated.

The canonical fallback is: (i) hex-encode every byte NOT in `[a-zA-Z0-9/_-]` as `%HH` (uppercase); (ii) collapse every run of `/` longer than one into a single `/`; (iii) reject and fail-fast if the resulting name would be the bare `@` character, is empty, is a single `.`, or still fails a second `git check-ref-format` invocation. The hex-encode rule covers the edge cases prose enumeration misses: `@{`, `//`, a sole `@`, leading and trailing `.`, a per-component trailing `.lock`, ASCII control characters, and case collisions on case-insensitive filesystems (which `git check-ref-format` itself does not guard against but which the hex escape of component-leading `.` / trailing `.lock` neutralizes in practice).

`git check-ref-format` is the single source of truth for ref-name validity at each check; the workspace manager MUST NOT cache or encode git's accepted set independently. A bead ID whose transformed form would be empty, would collide case-insensitively with an existing branch on the same repo, or would still fail validation after fallback MUST be rejected at workspace-create time with a fail-fast `RefNameInvalid` error (see §8). The operator-policy surface for the integration-branch template itself remains OQ-WM-002; the validation mechanism is frozen at this revision.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### WM-007 — Three-level branching model

The system MUST use a three-level branching model: (a) node commits land on the task branch per WM-005 and [execution-model.md §4.5 EM-023]; (b) the task branch squash-merges onto the integration branch at run-terminal-success per §4.5; (c) the integration branch merges to main under developer or operator policy. Harmonik's contract ends at "integration branch holds one commit per task." Merge style from integration to main is NOT dictated by this spec, and `workspace_merge_status` (§4.5) fires ONLY for the task-branch → integration-branch merge — it does NOT fire on an external main-merge performed by developer tooling.

Tags: mechanism

#### WM-008 — Small-scope collapse — operator-policy gated merge target for parentless runs

When a run has no parent-bead relationship in Beads (per [beads-integration.md §4.5 BI-014]), the task branch's merge target MUST be determined by operator policy per [operator-nfr.md §4.3] with two allowed values: `integration` (squash-merge to `harmonik/integration`, the default) or `main` (squash-merge directly to main — the small-scope-collapse shape). Absent an operator override, the default MUST be `integration` (consistent with WM-006). The decision is deterministic on the configured policy value; no cognition participates. The policy knob's precedence layer and reload semantics are tracked in OQ-WM-002.

Tags: mechanism

#### WM-009 — Branch naming is stable across a harmonik version

Task-branch and integration-branch naming conventions MUST be stable across a harmonik minor version per the compat contract declared in [operator-nfr.md §4.5 ON-018]. A breaking change to branch naming requires a migration release.

Tags: mechanism

### 4.3 Lease model

#### WM-010 — Lease is held by the run, not by individual agents

A workspace MUST be leased by exactly one `Run` for the run's full lifetime, defined as the interval that starts on the emission of `workspace_leased` (§4.4.WM-016) and ends on the emission of a terminal workspace event (`workspace_merge_status` with `status=merged`, or `workspace_discarded`) per §4.3.WM-013b. Multiple agents within the run (planner, researcher, builder, reviewer, merge agent) MUST occupy the same worktree sequentially across their nodes. An agent MUST NOT hold exclusive ownership of the worktree for the duration of its agent-level session; the centralized-controller principle ([architecture.md §4.9]) requires the run — not the agent — to be the lease holder.

Tags: mechanism

#### WM-011 — One active agent at a time inside a workspace

At any instant, AT MOST ONE agent process MAY be actively writing to the worktree. Agents run sequentially as the run traverses its workflow graph; parallel nodes within one run MUST NOT share a worktree. Parallel nodes across different runs occupy separate worktrees per WM-002. Enforcement is delegated to the orchestrator (S01): the orchestrator MUST NOT dispatch a second agent into a workspace already holding a live handler subprocess. The workspace manager's storage-level contribution is the lease-lock file (§4.3.WM-013a), whose content identifies the owning run but does not by itself arbitrate per-agent concurrency.

> INFORMATIVE: `review-loop` mode (per [execution-model.md §4.3]) is a concrete instance of this rule. The implementer agent and the reviewer agent are sequential occupants of the same worktree: the implementer agent runs to a checkpoint or exits; only then does the reviewer agent launch; the reviewer exits before the implementer (or a fresh implementer session for the next iteration) resumes. The implementer and reviewer never run concurrently against the same worktree. No new workspace primitive is introduced for `review-loop`; the rule of one-active-agent-at-a-time already accommodates the iteration cycle.

Tags: mechanism

#### WM-012 — One run per bead at a time

At any instant, AT MOST ONE run MAY be in flight for a given bead. A second run for the same bead is permitted ONLY after the first has reached a terminal state (completed, failed, or canceled) per [execution-model.md §4.3]. Re-claim semantics are defined in §4.9.

Tags: mechanism

#### WM-013 — Workspace ID is discoverable from run_id without a separate index

Given a `run_id`, the workspace manager MUST be able to resolve the workspace record (path, branch, state) by deterministic construction per WM-002 and WM-004 plus a filesystem check per WM-013c. No separate run-to-workspace index MAY be required as the authoritative lookup path.

Tags: mechanism

#### WM-013a — Lease-lock file canonical path, content, and birth

The lease on a workspace is represented by a lease-lock file at the canonical path declared in §6.2. The lock file's existence represents the lease; its absence represents a released workspace. The file's content MUST be a JSON object with the fields:

- `run_id` (UUID, required) — the owning run.
- `pid` (integer, required) — the daemon process ID that wrote the lock.
- `created_at` (RFC 3339, required) — wall-clock time the lock was written.
- `ttl_sec` (integer, required) — advisory lifetime; informative for the orphan sweep, does not enforce auto-expiry.

The workspace manager MUST write the lease-lock file atomically (write-to-temp + rename) and MUST fsync the file before emitting `workspace_leased` (§4.4.WM-016). Lock-file birth timing: immediately preceding the `workspace_leased` emission; the emission ordering of WM-016 applies unchanged (worktree → branch → sessions dir + sidecar → lease lock → `workspace_leased`). On every `workspace_created` emission, the workspace manager MUST NOT yet have written a lease-lock file — the lock is tied to lease acquisition, not to workspace existence.

**One lease per run lifetime — including across multi-session modes.** In `review-loop` mode (per [execution-model.md §4.3]) — and any other workflow mode that launches multiple sessions sequentially within a single run — exactly ONE lease MUST cover the entire run. The lease is acquired at `workspace_leased` (per WM-016) and released only at the terminal workspace transition (`workspace_merge_status` with `status=merged` or `workspace_discarded`) per WM-013b. Multiple sessions (e.g., implementer launches one per iteration plus reviewer launches one per iteration, up to the iteration cap of 3) all occur under the same lease. The lease-lock file is NOT re-acquired or released per session; only the per-session sidecars (per §4.7.WM-026) are written per launch.

> NOTE: The canonical lock path in this spec is `${workspace_path}/.harmonik/lease.lock`. [handler-contract.md §4.10 HC-044a] currently names `.harmonik/worktrees/<run_id>/.lock` and [process-lifecycle.md §4.2 PL-006] names `.harmonik/lease.lock`. The three specs disagree on filename; OQ-WM-005 tracks the coordinated resolution. Until HC and PL align, implementers MUST treat this spec's filename as authoritative for WM's writer side, and MUST NOT assume HC-044a's fail-fast path shares a filename with the WM-owned lock.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### WM-013b — Lease release on terminal transitions

The workspace manager MUST release the lease (remove the lease-lock file) on every terminal workspace transition: entering `merged` (§7.1) or `discarded` (§7.1). Release is gated on the durability of the workspace's FINAL state-transition event per the per-terminal-path rules below; the worktree directory and branch persist on disk per §4.8.WM-031, but the lock file does NOT. Operator-initiated purge workflows (post-MVH per §2.2) MAY additionally remove a preserved worktree directory after lease release; MVH has no such workflow. Release itself is idempotent: a second release call against an already-released workspace MUST succeed without error.

**Release gate per terminal path.** The release gate is the fsync boundary of the terminal event, which differs by path:

- **Merged path.** Release MUST occur AFTER `workspace_merge_status` with `status=merged` has been emitted and flushed to the durable events journal per [event-model.md §4.4 EV-015]. The event is class F (fsynced) per EV's classification, so a successful EV-015 flush means the event is durably recorded.
- **Failed-run path.** Release MUST occur AFTER the `run_failed` event (EV §8.1.3, class F per EV) has been emitted and flushed. `run_failed` is the durable terminal marker for a failed run; the companion `workspace_discarded` (class O per EV, not fsynced) announces the workspace-level consequence but is NOT the release gate because an O-class event MAY be lost on power loss. Tying the release to `run_failed` restores a durable pairing between "run is terminally failed" and "lease is released."
- **Post-escalation path.** When the workspace enters `discarded` following `merge_conflict_escalation` (WM-023) without a matching `run_failed` (the run may remain in `conflict-resolving` awaiting operator intervention), release MUST occur AFTER a workspace-local durability marker is written: append a single line `{"event":"lease_released","run_id":"<run_id>","workspace_id":"<workspace_id>","reason":"post_escalation","released_at":"<rfc3339>"}` to `${workspace_path}/.harmonik/events/workspace-<workspace_id>.jsonl` and fsync that file. The marker is a workspace-scoped durability signal consumed by reconciliation's Cat 3 detector to distinguish "released" from "dropped on crash." The workspace-local events file is owned by this spec (see §6.2 path table) and is NOT the global JSONL journal of EV.
- **Operator-forced discard (reconciliation verdict-driven).** When discard arises from a reconciliation verdict (`reopen-bead` followed by worktree abandonment per §4.9.WM-034), release MUST occur AFTER `reconciliation_verdict_executed` has been emitted and flushed per EV and AFTER the workspace-local `lease_released` marker described above is written and fsynced.

Across all terminal paths, the workspace-local `lease_released` JSONL marker MUST be written before the lease-lock file is removed; if the workspace manager crashes after writing the marker but before unlink, startup reconciliation observes a present lock + marker combination and completes the release by unlinking the lock (idempotent replay).

**OQ-WM-013.** EV classifies `workspace_discarded` as class O (ordinary, not fsynced). A cleaner shape would be for EV to reclassify `workspace_discarded` from O to F as a durable terminal marker, removing WM's need for the workspace-local JSONL fallback. The reclassification is tracked as OQ-WM-013 for EV's next revision; WM does NOT block on it — the workspace-local marker resolves the durability gap locally.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### WM-013c — Lease discovery mechanism on startup

The workspace manager's startup path MUST discover live workspaces by (a) enumerating subdirectories of `<repo>/.harmonik/worktrees/` matching the `<run_id>` regex of WM-002; (b) for each, calling `git worktree list --porcelain` against `<repo>` and confirming the directory is a registered worktree; (c) reading the lease-lock file per WM-013a (if present) to recover the `run_id`, `pid`, and `created_at`; (d) stat-ing `${path}/.harmonik/sessions/` to detect whether any session was ever started. A directory failing (b) but present on disk is an orphan worktree subject to [process-lifecycle.md §4.2 PL-006]. A directory passing (b) with a live lease-lock file whose recorded `pid` is NOT the current daemon is subject to the orphan-sweep rule of §4.8.WM-033 (stale → sweep; live non-orphan → fail-fast coordination with HC-044a; see OQ-WM-005).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### WM-013d — Released-workspace re-use is forbidden

A released workspace's canonical path (WM-002) MUST NOT be re-leased by a subsequent run. New runs receive new canonical paths via new `run_id`s per WM-034. The prior run's worktree directory and branch MAY persist on disk per WM-031; re-use of the path for a different `run_id` is forbidden — the canonical-path invariant (§5.WM-INV-005) would be violated.

Tags: mechanism

#### WM-013e — `.gitignore` hygiene for harmonik control-plane paths

The workspace manager MUST ensure that the backing repository's root `.gitignore` excludes the harmonik control-plane paths so that checkpoint commits on a task branch do NOT inadvertently include daemon-local state. Required ignore entries (patterns relative to repo root; order preserved):

```
.harmonik/lease.lock
.harmonik/sessions/
.harmonik/worktrees/
.harmonik/events/
.harmonik/review.json
.harmonik/review.iter-*.json
.harmonik/review-target.md
.harmonik/reviewer-feedback.iter-*.md
.harmonik/agent-task.md
.harmonik/agent-task.tmp-*
.claude/settings.json
```

The `.claude/settings.json` entry covers the bridge-materialized settings file introduced by §4.7a.WM-040a; the materialization step MUST add this line to the worktree's `.gitignore` if not already present, in the same atomic-write transaction as the settings.json write.

The `.harmonik/sessions/` inclusion here is SPECIFICALLY scoped to `<repo>/.harmonik/sessions/` at the repo root (which should not exist — sessions live INSIDE worktrees) and is a defense-in-depth against session-log leakage from a stray operator operation. Per-worktree `.harmonik/sessions/` directories are NOT excluded by the entry above because the `.gitignore` interpretation is rooted at the main worktree; checkpoint commits from inside a task worktree see their own `.harmonik/sessions/` as in-tree and are free to include them per WM-030's preserve-in-merged-branch contract.

The `.harmonik/events/` entry covers the workspace-local durability JSONL file introduced by WM-013b.

**Daemon-startup write-or-fail posture.** At daemon startup the workspace manager MUST check the root `.gitignore` for the entries above. If any are missing, the daemon MUST add them AND stage + commit the `.gitignore` change on a dedicated branch (`harmonik/gitignore-init`) before creating any worktree. If the daemon lacks write permission on `.gitignore`, startup MUST fail with a typed `GitignoreWriteForbidden` error per §8 and surface the failure to the operator — silent continuation with a misconfigured ignore file would risk leaking daemon state into user commits. The operator-side override (skip the write and accept the risk) is tracked in OQ-WM-014 pending operator-nfr guidance.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.4 Lifecycle states and events

#### WM-014 — Workspace state machine

A `Workspace` MUST traverse the lifecycle states `created` → `ready` → `leased` → (optionally `merge-pending` → optionally `conflict-resolving`) → (`merged` | `discarded`). The lifecycle state is orthogonal to the interrupt-state field (§4.10); the orthogonality applies only to in-flight states, per §4.10.WM-037a. See §7.1 for the transition table.

> NOTE: The `setup` state from WM v0.2 has been retired in v0.3.0. Its material is subsumed into `created → ready`; the retired enum value MUST NOT be reintroduced. See §12 revision history.

> NORMATIVE: `review-loop` mode introduces NO new workspace lifecycle state. The existing `leased → merge-pending → merged` (or `... → discarded`) progression accommodates the entire iterate-review-iterate cycle. Per-iteration state (current iteration index, cumulative reviewer verdicts) lives in the Run record's `context` per [execution-model.md §4.3], NOT in the workspace state machine.

Tags: mechanism

#### WM-015 — Workspace lifecycle event emission obligations

The workspace manager MUST emit the following events at the state transitions declared in §7.1. Event type names AND payload schemas are authoritative in [event-model.md §8.5] per EV-025; this spec is normative ONLY for WHEN each event fires:

- `workspace_created` — emit on entry to `created` (§7.1 initial transition). Schema: [event-model.md §8.5.1].
- `workspace_leased` — emit on entry to `leased` AFTER the emission-ordering gates of WM-016. Schema: [event-model.md §8.5.2].
- `workspace_merge_status` — single paired-phase event per [event-model.md §8.9(h)]. Emit ONCE with `status=pending` on entry to `merge-pending` and ONCE with `status=merged` on entry to `merged`. Schema: [event-model.md §8.5.3].
- `workspace_discarded` — emit on entry to `discarded`. Schema: [event-model.md §8.5.4].
- `merge_conflict_escalation` — emit when the implementer-resolution path is exhausted per §4.6.WM-023. Schema: [event-model.md §8.5.6].

The `workspace_interrupted` event (EV §8.5.5) is emitted by the reconciliation detector, NOT by the workspace manager; see §4.10.WM-039 for WM's observation obligation and OQ-WM-006 for the unresolved emitter-identity for operator-driven interrupts.

This spec MUST NOT declare event type names (they live in EV per EV-025) or payload field lists (they live in EV). Prior-version inline payload enumerations (WM-017 payload list, WM-023 payload list, WM-039 payload list) are retired per §12; the wire format is read from EV alone.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### WM-016 — workspace_leased is emitted after metadata sidecar and lease-lock write

The workspace manager MUST complete the following in order BEFORE emitting `workspace_leased`: (a) `git worktree add` has succeeded (§4.1.WM-003); (b) the task branch (§4.2.WM-005) exists; (c) the session-log directory for the first session and its `harmonik.meta.json` sidecar per §4.7 are written and fsynced; (d) the lease-lock file per §4.3.WM-013a is written and fsynced. Only after (a)–(d) complete does `workspace_leased` emit. The workspace's lifecycle state MUST transition to `leased` BEFORE the event is emitted so that consumers observing the event see a consistent record.

> NOTE: The per-workspace `workspace_leased` emission is tied to the FIRST session's sidecar write. Subsequent sessions (WM-025) write their own sidecars per WM-026; the workspace's lifecycle state does NOT re-transition and `workspace_leased` does NOT re-emit on subsequent session launches.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### WM-017 — Retired

WM-017 is retired in v0.3.0. Its prior content (payload-field declarations for `workspace_merged`, `workspace_discarded`, `workspace_interrupted`) violated EV-025 by enumerating payload fields that are EV's to own. Correlation-field obligations are now discoverable from [event-model.md §8.5]. Do NOT reuse the ID.

Tags: mechanism

### 4.5 Merge back to integration

#### WM-018 — Merge-back is performed by a node in the same worktree

The merge-back operation (merging the task branch onto the integration branch) MUST be performed by a workflow node that executes INSIDE the run's already-leased worktree. The merge is NOT a new workspace; it is another node in the same run consuming the same lease per WM-010. A design that creates a new workspace for the merge step is forbidden.

Tags: mechanism

#### WM-018a — Merge-back node dispatch contract

Every workflow that produces a merge-back MUST declare the merge step as a workflow node of one of two shapes: (a) a **non-agentic merge node** dispatched directly by the orchestrator — the orchestrator executes `git merge --squash` + commit per WM-019, with no handler subprocess; (b) an **agentic merge node** dispatched through [handler-contract.md §4.1] whose handler executes the same git operations plus any pre-merge validation per its LaunchSpec. The choice is a workflow-author concern, not dictated here. In both shapes, the merge node's dispatch occurs INSIDE the existing lease per WM-018; the node's `Outcome` (per [execution-model.md §6.1]) carries the terminal merge outcome and drives the §7.1 transition from `merge-pending` to `merged` or to `conflict-resolving`. Conflict detection is mechanical: a non-zero exit from `git merge --squash` or the presence of conflict markers in `git status --porcelain` output MUST be treated as conflict entry per WM-020.

Tags: mechanism

#### WM-019 — Task branch squash-merges onto integration branch with one commit per task

The merge-back operation MUST produce exactly one commit on the integration branch per completed task. The merge MUST be implemented as `git merge --squash --strategy=ort <task_branch>` followed by `git commit` on the integration branch (or an equivalent porcelain-level sequence that preserves identical tree-and-trailer semantics). The `--strategy=ort` pin is REQUIRED to fix the merge algorithm across git versions; `ort` is the default for `git merge` since git 2.34 (see WM-ENV-002 minimum version pin) and explicit specification prevents an older or differently-configured site from silently falling back to `recursive` with different conflict resolution semantics.

**Commit message synthesis.** The integration-branch commit message is synthesized by the merge node (the non-agentic orchestrator path or the agentic merge-node handler per §4.5.WM-018a). The message body MUST include a summary of the task-branch's checkpoint commit set — at minimum the first-line subject of each checkpoint, concatenated in commit order — so the squashed commit remains human-readable as a condensed task narrative. The message MUST carry trailers: `Harmonik-Run-ID: <run_id>` and, when the run is bead-tied, `Harmonik-Bead-ID: <bead_id>` per [execution-model.md §4.4 EM-017]. The trailer set is NOT copied verbatim from the final checkpoint; it is re-synthesized from the run's invariant identifiers (which are equivalent in practice, since every checkpoint on a single task branch shares these two trailers per WM-INV-003).

**Author and committer identity.** The squashed commit's `author` MUST be set to the LaunchSpec identity of the merge-node's implementer (when §4.5.WM-018a uses the agentic variant, author = `implementer_handler_ref.identifier` string; see [handler-contract.md §6.1] for the identifier shape), OR to the daemon identity when §4.5.WM-018a uses the non-agentic orchestrator-dispatched variant. The squashed commit's `committer` MUST be the daemon identity in both variants: `Harmonik Daemon <no-reply@harmonik.local>` or the operator-configured override per [operator-nfr.md §4.3]. The author/committer split makes the provenance of the content (author) distinct from the provenance of the merge operation itself (committer).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### WM-019a — Merge executes outside the main worktree

The merge operation (§4.5.WM-019) MUST NOT execute `git checkout` against the integration branch in a worktree concurrently used by operator activity. Two mechanisms are accepted:

(a) **`--ignore-other-worktrees` flag.** The merge node MAY run `git checkout --ignore-other-worktrees <integration_branch>` against the merge-staging area. This flag tells git to proceed even if another worktree has the branch checked out; harmonik's lease model (WM-010) guarantees that no other harmonik worktree holds the integration branch, but operators running their own main worktree may.

(b) **Scratch merge-worktree (PREFERRED).** The merge node creates a dedicated short-lived worktree at `<repo>/.harmonik/worktrees/merge-<merge_id>/` (where `<merge_id>` is a UUIDv7 minted for the merge operation), executes the merge inside that worktree, and removes the worktree after the merge commit is created. Lifecycle: (i) `git worktree add -b merge-<merge_id> <merge_path> <integration_tip>`; (ii) `git merge --squash --strategy=ort <task_branch>`; (iii) resolve conflicts per WM-024 if present; (iv) `git commit` with trailers per WM-019; (v) `git push <integration_tip>` to the integration branch (or equivalent ref-update); (vi) `git worktree remove --force <merge_path>` and `git worktree prune`; (vii) delete the transient `merge-<merge_id>` branch. The scratch-worktree path is owned by WM (see §6.2) and is NOT leased — it has no lease-lock file and does not participate in the §4.3 lease state machine. The scratch worktree's `merge_id` is distinct from any `run_id`; scratch worktrees are never classified as orphan workspaces by WM-003a.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### WM-020 — Squash-merge is non-fast-forward by construction

A squash-merge (§4.5.WM-019) is non-fast-forward by its nature: `git merge --squash` produces a single new commit on the integration branch whose tree equals the merged result but whose parent is the integration-branch tip (not the task-branch tip). The integration-branch tip therefore ADVANCES to a new commit containing the squashed content; the task-branch tip is UNCHANGED and is NOT an ancestor of the new integration-branch tip. The phrasing "not fast-forward-only" used in prior WM drafts is retired — under squash a fast-forward is not representable, so qualifying the merge as "not ff-only" is vacuous.

Conflict markers in the index are real and MUST be resolved per §4.6 before the merge commit is created. Detection of conflicts is mechanical per §4.5.WM-018a (non-zero exit from `git merge --squash`, or conflict markers visible in `git status --porcelain`).

Tags: mechanism

#### WM-021 — Merge outcome emits workspace_merge_status with status=merged

On successful merge, the workspace manager MUST emit `workspace_merge_status` with `status=merged` per [event-model.md §8.5.3]; the payload schema (including `merge_commit_hash`, `source_branch`, `target_branch`) is declared there. The workspace state MUST transition to `merged` per §7.1. On entry to `merge-pending` (prior to merge execution), the workspace manager MUST emit `workspace_merge_status` with `status=pending`; this is the same event type, consistent with the paired-phase single-event rule of [event-model.md §8.9(h)]. Two separate `workspace_merge_pending` and `workspace_merged` event types are NOT registered; references to those names in WM v0.2 are retired per §12.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### 4.6 Conflict resolution

#### WM-022 — Original-implementer identification at merge-pending entry

The "original implementer" for a given merge-back MUST be identified mechanically from the workspace record's `implementer_handler_ref` field (§6.1). That field MUST be set by the workspace manager at merge-pending entry (§7.1) to the `handler_ref` (per [handler-contract.md §6.1]) derived from the most-recent agentic session sidecar recorded under the workspace's session-log root.

**Identification mechanism (sidecar walk).** The workspace manager MUST enumerate `${workspace_path}/.harmonik/sessions/*/harmonik.meta.json` — the metadata sidecars written by WM-026 for every session launched against the workspace — and MUST order them by the sidecar's `launched_at` field (RFC 3339; per WM-026) in reverse chronological order. For each sidecar in that order, WM MUST inspect the `agent_type` field (the conformance-class identifier per [architecture.md §4.7 AR-024] and [handler-contract.md §6.1]); the FIRST sidecar whose `agent_type` belongs to the set of agentic handler classes (as registered under the handler-contract conformance-class taxonomy — see [handler-contract.md §6.1]; mechanical / generator / merge-node classes are non-agentic) supplies `implementer_handler_ref` and the LaunchSpec template for any subsequent re-dispatch. The sidecar is the authoritative source because WM-026 guarantees the sidecar is written and fsynced before the handler launches, so its presence on disk is a durable prerequisite for any commit that may have landed.

**No git-trailer walk.** This identification rule does NOT walk git commit trailers. Trailers emitted under [execution-model.md §4.4 EM-017] carry `Harmonik-Run-ID`, `Harmonik-State-ID`, `Harmonik-Transition-ID`, `Harmonik-Schema-Version`, and (conditionally) `Harmonik-Bead-ID` — none of which identify the emitting agent's conformance class. Earlier WM drafts cited a `Harmonik-Actor-Role` trailer; no such trailer exists in EM-017 and the reference is retired at this revision. Enriching the EM trailer set to carry `Harmonik-Agent-Type` (to allow a trailer-only walk without sidecar I/O) is tracked as a cross-spec enhancement in OQ-WM-012.

**If no agentic sidecar exists.** If the sidecar walk finds no session whose `agent_type` is agentic (the task branch carries only mechanical/generated commits, or the workspace was leased but no agentic handler has yet run), `implementer_handler_ref` MUST be set to null. §4.6.WM-022a then governs the escalation path.

When a merge conflict is detected (§4.5.WM-020), the resolution agent MUST be a fresh handler session launched from `implementer_handler_ref` per WM-024. There is no dedicated "merge-agent" role in MVH.

> NOTE on "original": The phrase preserved in the glossary refers to HANDLER-CLASS LINEAGE, not to PROCESS-IDENTITY preservation. Agent sessions are ephemeral; by the time a merge happens the prior implementer's process has terminated. Re-dispatch launches a fresh session of the recorded handler class.

Tags: cognition
Axes: llm-freedom=bounded; io-determinism=best-effort; replay-safety=unsafe; idempotency=non-idempotent

#### WM-022a — All-mechanical task branches escalate directly

If `implementer_handler_ref` is null at merge-pending entry (the task branch contains only non-agentic commits — mechanical refactors, generated-code landings, or merge-node commits with no agentic ancestry), the workspace manager MUST skip WM-024 re-dispatch and emit `merge_conflict_escalation` directly per WM-023 on conflict detection. The system MUST NOT silently remap the implementer role to an unrelated handler class. Human resolution is the MVH fallback for all-mechanical conflict paths.

Tags: mechanism

#### WM-023 — Unresolvable conflicts escalate via merge_conflict_escalation

If the re-dispatched implementer per WM-024 cannot resolve the merge conflicts within its handler-contract budget per [handler-contract.md §6.1], or returns any terminal outcome other than a successful merge commit, the workspace manager MUST emit `merge_conflict_escalation` per [event-model.md §8.5.6] — EV owns the payload schema including `workspace_id`, `run_id`, `conflict_paths[]`, `escalated_at`. The run MUST transition to `conflict-resolving` per §7.1 on initial conflict detection (not again on escalation — the transition is single-entry); escalation marks the resolution path as exhausted and the workspace transitions to `discarded` per §7.1 after the escalation event is emitted.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### WM-024 — Conflict-resolver dispatch is mechanism-tagged; resolution itself is cognition-tagged

The mechanism of dispatching the implementer agent back into the worktree to resolve a conflict is deterministic (mechanism-tagged, this spec owns it). The agent's reasoning during resolution is delegated to the implementer's handler per [handler-contract.md §4.1] (cognition-tagged; owned there). This spec names the mechanical delegation path:

- **Role.** The handler class resolved from `workspace.implementer_handler_ref` per WM-022.
- **Model-class.** The `agent_type` recorded on the session metadata sidecar that produced the latest agentic commit (§6.1 SessionMetadataSidecar.agent_type).
- **Input shape.** A fresh `LaunchSpec` per [handler-contract.md §6.1] with `workspace_path` pointing at the existing worktree, a `required_skills` list that includes the conflict-resolution skill (skill name and provisioning path tracked in OQ-WM-007 pending a handler-contract skill registration), and an input payload of (task branch tip, integration branch tip, conflict-markered paths, the run's transition history as recoverable from git + JSONL per [execution-model.md §4.7]).
- **Budget.** The re-dispatch budget is a fresh budget issued per the handler-contract LaunchSpec default; the prior implementer session's unused budget is NOT automatically inherited.
- **Re-dispatch attempt cap.** The workspace manager MUST cap conflict-resolution re-dispatch attempts at a DEFAULT of THREE (3) attempts per merge-pending cycle. Each attempt spawns a fresh LaunchSpec per the bullets above; each attempt's terminal outcome (success or non-success) is recorded in the workspace-local events JSONL of §4.3.WM-013b. After three non-successful attempts, §4.6.WM-022a / WM-023 MUST route the verdict to `escalate-to-human` per [reconciliation/spec.md §4.5 RC-020] — further automated re-dispatch would burn budget without a reasoning-path-change. The cap is operator-configurable per [operator-nfr.md §4.3] with a lower bound of 1 (no re-dispatch; escalate on first conflict) and an upper bound of 10 (beyond which escalation is heuristically better per the §A.3 rationale). Operator overrides outside [1, 10] MUST be rejected at daemon startup.

If the recorded handler class has been retired between the initial commit time and merge-time re-dispatch (post-MVH concern), the workspace manager MUST treat this as an unresolvable-conflict path and route to WM-023 escalation without attempting a silent handler-class remap.

Tags: mechanism

### 4.7 Session-log directory and metadata sidecar

#### WM-025 — Session-log directory layout

For every agent session launched against a workspace, a session-log directory MUST exist at `${workspace_path}/.harmonik/sessions/${session_id}/`. The directory is the join point for handler-written session logs (S04) and CASS-read metadata (S08). The workspace manager OWNS directory creation; handlers OWN log-file contents; the handler's `session_log_location` progress-stream emission per [handler-contract.md §4.2 HC-010] announces this pre-existing path to the watcher.

`session_id` is a per-session UUID minted by the workspace manager at session start; the generation and uniqueness rule is in [handler-contract.md §4.2]. Session-log directories for distinct sessions within a workspace never collide because `session_id` is unique per launch.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### WM-026 — Metadata sidecar file and required fields

The workspace manager MUST write a metadata sidecar file at `${workspace_path}/.harmonik/sessions/${session_id}/harmonik.meta.json` before the handler launches. The file MUST carry `run_id`, `node_id`, `agent_type` (the conformance-class identifier per [architecture.md §4.7 AR-024]), `workflow_id`, `launched_at` (RFC 3339 timestamp), `schema_version` (set by the workspace manager per §6.4), and `bead_id` (present iff the run is bead-tied per [execution-model.md §4.3 EM-014]). The metadata sidecar is the authoritative join key for CASS indexing AND the authoritative source for agent-type at merge-time implementer identification (§4.6.WM-022). For runs inside expanded sub-workflows (per [execution-model.md §4.8 EM-034a]), the sidecar's `node_id` MUST carry the namespaced form `<parent_node_id>/<sub_node_id>` so the sidecar locates the expanded node uniquely.

**Atomicity discipline.** The sidecar MUST be written with the same atomic discipline as the lease-lock (§4.3.WM-013a): (i) write the JSON content to a sibling temp file (e.g., `harmonik.meta.json.tmp-<pid>`); (ii) fsync the temp file; (iii) `rename(2)` the temp file to the canonical `harmonik.meta.json` name (POSIX rename is atomic); (iv) fsync the parent directory `${workspace_path}/.harmonik/sessions/${session_id}/` to durably record the rename. Step (iv) is REQUIRED — a power loss after step (iii) without a parent-directory fsync can lose the rename on some filesystems. The ordering gate of WM-016 (the four-step sequence ending at `workspace_leased`) requires both the sidecar AND the lease-lock to be durable on disk before the event emits; the parent-directory fsync MUST complete before `workspace_leased` for the first session's sidecar.

An interrupted sidecar write (process death between steps (i) and (iii)) leaves a `.tmp-<pid>` file and no canonical `harmonik.meta.json`. The startup sweep MUST tolerate orphan `.tmp-<pid>` files by removing them; a missing `harmonik.meta.json` with a present lease-lock is classified per §4.3.WM-013c. A missing lease-lock with a present sidecar is the `sidecar-without-lease` evidence type of WM-003a.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### WM-027 — Metadata stamping precedes workspace_leased for the first session

The workspace manager MUST write the first session's metadata sidecar BEFORE emitting `workspace_leased` per WM-016. This ordering ensures that any consumer of `workspace_leased` observing the handler launch can join metadata without racing the sidecar write. For subsequent sessions within a workspace (WM-010 allows many sessions per workspace), the sidecar write precedes handler launch but does NOT re-emit `workspace_leased`; it proceeds as a stand-alone sidecar-write operation covered by WM-026.

Tags: mechanism

#### WM-027a — Reviewer verdict artifact path

For workflows that include a reviewer agent (notably `review-loop` mode per [execution-model.md §4.3]), the reviewer agent MUST write its verdict to the canonical path `${workspace_path}/.harmonik/review.json` inside the worktree. The file's content MUST conform to the `agent-reviewer` skill's JSON verdict schema v1 (carrying `schema_version`, `verdict ∈ {APPROVE, REQUEST_CHANGES, BLOCK}`, `flags[]`, and `notes`); the schema is owned by the agent-reviewer skill surface per [handler-contract.md §4.11] and the event-model verdict-routing entry. Lifecycle:

(a) The reviewer subprocess writes the file as part of its session output. The reviewer MUST overwrite any existing `.harmonik/review.json` from an earlier iteration; archival of the prior file is the daemon's responsibility per (c) below, NOT the reviewer's.

(b) The daemon (or the orchestrator-core dispatch loop) MUST read `${workspace_path}/.harmonik/review.json` after the reviewer's `agent_completed` event (per [handler-contract.md §4.3]) and before deciding the next phase of the run (continue, terminate, escalate). The verdict-routing logic itself is owned by [execution-model.md §4.3] and [handler-contract.md §4.1]; this requirement names only the on-disk bus between reviewer and daemon.

(c) On entry to a subsequent iteration's reviewer phase (iteration N+1, where iteration 1 is the first reviewer launch and the iteration cap of 3 is hardcoded for v1 per [execution-model.md §4.3]), the daemon MUST archive the prior `.harmonik/review.json` by renaming it to `${workspace_path}/.harmonik/review.iter-<N>.json` (where `<N>` is the just-completed iteration ordinal) BEFORE launching the next reviewer session. The rename MUST use the atomic temp+rename+fsync(parent_dir) discipline of WM-026. Archived per-iteration verdicts persist in the worktree for the lifetime of the workspace.

(d) `.harmonik/review.json` and `.harmonik/review.iter-*.json` MUST be excluded from checkpoint commits via the WM-013e `.gitignore` hygiene set. The reviewer's verdict is workflow-control state, not work product; it MUST NOT pollute the squash-merge commit per WM-019.

(e) Absence of `.harmonik/review.json` after a reviewer's `agent_completed` event is a malformed-reviewer-outcome condition. The daemon MUST treat the run's phase as inconclusive and route per the failure-handling rules of [handler-contract.md §4.6]; this spec does NOT define the resulting workspace transition.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### WM-028 — Bead-ID propagates into session metadata when present

When the run is tied to a bead, the metadata sidecar's `bead_id` field MUST carry the same `bead_id` value the checkpoint trailer carries per [execution-model.md §4.4 EM-017] and [beads-integration.md §4.6 BI-017, BI-018] — the trailer name (`Harmonik-Bead-ID`) and schema are owned by execution-model and beads-integration respectively; this spec asserts only the VALUE correlation. The `bead_id` field MUST be absent (or explicit null) when the run has no bead tie. CASS uses this metadata to join session logs to the Beads task ledger.

Tags: mechanism

#### WM-029 — Session-log directory is consumed read-only by S08

The session-log directory (including the metadata sidecar and handler-written logs) MUST be treated as read-only from the memory-layer subsystem's perspective. S08 indexes contents into CASS without mutating any file under `${workspace_path}/.harmonik/sessions/`. MVH does not declare a mechanical sensor for this obligation; the read-only contract is enforced by reviewer discipline until a memory-layer spec (S08, deferred) names an operator-auditable permission or audit hook. See OQ-WM-003.

Tags: mechanism

#### WM-030 — Post-merge session-log retention default

On successful merge (`workspace_merge_status` with `status=merged`), the workspace manager MUST preserve the sessions directory inside the merged branch (i.e., the session logs remain in the integration-branch commit tree) by default. An operator-configured alternative MAY move the directory to a post-merge archive path post-MVH; the default for MVH is preserve-in-merged-branch for audit retention.

The MVH default requires that project `.gitignore` MUST NOT exclude `.harmonik/sessions/`; a gitignored sessions directory silently breaks the preserve-in-merged-branch contract. Violations are an operator-observable misconfiguration per [operator-nfr.md §4.9]. This spec asserts the preservation contract; enforcement of the .gitignore hygiene is an operator-nfr concern.

> INFORMATIVE: `review-loop` mode exercises the multiple-sessions-per-workspace path more aggressively than `single` mode. A 3-iteration `review-loop` run (the iteration cap per [execution-model.md §4.3]) produces up to seven session directories per workspace (one initial implementer session plus, per iteration, one implementer and one reviewer launch). The post-merge retention defaults of this requirement apply unchanged; no new retention rule is needed for `review-loop`.

Tags: mechanism

> INFORMATIVE: `.claude/settings.json` materialized per §4.7a follows the same lifetime as the session-log directory: it persists across the worktree's lifetime, is preserved on successful merge per WM-030 (but not committed by way of WM-013e gitignore hygiene), and persists on terminal failure per WM-031.

### 4.7a Claude-code settings.json materialization

#### WM-040a — `.claude/settings.json` materialization for claude-code workspaces

For every workspace that will host a `claude-code` agent session (determined by [execution-model.md §4.3] node `agent_type`), the workspace manager MUST materialize a file at `${workspace_path}/.claude/settings.json` between WM-003 (worktree creation) and WM-016 (workspace_leased emission). The file's content is owned by [claude-hook-bridge.md §4.1].

The write MUST follow the atomic-write discipline of WM-026: temp file + fsync + rename + fsync(parent_dir). The parent-directory fsync MUST complete BEFORE `workspace_leased` emits.

If a `.claude/settings.json` file already exists in the worktree at materialization time (inherited from the cloned repo's state), the workspace manager MUST attempt a merge per [claude-hook-bridge.md §4.1 CHB-004]: the bridge-required hook entries are APPENDED to the existing event-type arrays. On malformed-JSON or merge-incompatible existing content, the workspace manager MUST OVERWRITE and log a warning line to the session log noting the displacement. No new bus event is emitted at MVH (the bridge introduces zero new event types per [claude-hook-bridge.md §4]); post-MVH operators MAY route this through an existing observability surface.

For workspaces that will NOT host a claude-code agent session, this requirement is a no-op.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### 4.7b Worktree auto-trust pre-seed

#### WM-040b — Pre-seed `~/.claude.json` trust before Claude exec

**Purpose.** Claude Code shows an interactive "Trust this directory?" dialog when launched in a directory it has not seen before. A daemon-spawned tmux pane has no human at the keyboard; the dialog blocks indefinitely and [handler-contract.md §4.9 HC-056] fires.

**Mechanism.** Claude Code reads per-project trust state from `~/.claude.json` under a top-level `"projects"` map keyed by absolute directory path. Setting `hasTrustDialogAccepted: true` on the worktree path entry suppresses the dialog. This is the mechanism Claude Code uses after the operator clicks "Yes" in an interactive session; harmonik pre-seeds it before Claude starts.

**Obligation.** For every workspace that will host a `claude-code` agent session, the daemon MUST call `EnsureWorktreeTrust(worktreePath)` (or equivalent) AFTER WM-003 (worktree creation) and WM-040a (settings.json materialization) and BEFORE exec'ing Claude via the tmux substrate (`SubstrateSpawn`). The ordering is:

1. WM-003 — `git worktree add`
2. WM-040a — materialize `.claude/settings.json`
3. **WM-040b** — pre-seed `~/.claude.json` trust entry ← this requirement
4. CHB-028 — materialize `.harmonik/agent-task.md`
5. `SubstrateSpawn` — exec Claude in tmux pane

**Atomicity.** The write MUST use an atomic temp-file + rename discipline (same WM-026 pattern) to guard against concurrent daemon instances writing to the same `~/.claude.json`. The temp file MUST be a sibling of `~/.claude.json` (e.g., `~/.claude.json.tmp-<pid>`).

**Idempotency.** If the entry is already present and `hasTrustDialogAccepted` is already `true`, no write is performed. This covers the re-attach path (daemon restart mid-session).

**Preservation.** The write MUST NOT remove or modify any other key in `~/.claude.json` or any other entry in its `projects` map. The merge is additive.

**Failure.** If the write fails, the daemon MUST NOT exec Claude. The error is surfaced as `ErrStructural` with `sub_reason: trust_seed_failed`; `agent_failed` is emitted. An un-seeded launch blocks silently rather than failing fast.

**Rejected alternatives.** `--dangerously-skip-permissions` and `--permission-mode` are forbidden by [handler-contract.md §4.9 HC-055] and [claude-hook-bridge.md §4.2 CHB-007]. The `.claude/settings.local.json` file in the worktree does not carry trust state that suppresses the startup dialog.

**Scope.** This is the user-operator machine running the daemon, not the worktree itself. The `~/.claude.json` entry persists after the run; cleanup is acceptable but not required at MVH (orphaned entries are cosmetically inert).

Cross-refs: [claude-hook-bridge.md §4.12 CHB-029] (authoritative requirement; WM-040b is the workspace-model side of the same contract). Code: `internal/workspace/claudetrust_wm040b.go`.

Tags: mechanism, security-relevant
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.8 Failed-run worktree persistence

#### WM-031 — Failed-run worktrees persist until operator cleanup

A worktree whose run reached a terminal failure state (`failed` or `canceled` per [execution-model.md §7.1]) MUST persist on disk with its branch intact. The workspace manager MUST NOT auto-delete failed-run worktrees beyond the startup orphan sweep (§4.8.WM-033). Failed-run worktrees are retained for audit and post-mortem. Lease-lock files are still released per §4.3.WM-013b; the worktree directory and branch remain, but the lock file does not.

MVH imposes no retention window on the failed-run worktrees. Operators facing disk pressure MUST handle it via operator-initiated cleanup workflows per [operator-nfr.md §4.9]; an automatic retention-window default is tracked in OQ-WM-008.

Tags: mechanism

#### WM-032 — Failed-run workspace state is discarded; interrupt state composes orthogonally

On terminal failure, the workspace state MUST transition to `discarded` per §7.1. A workspace's `interrupt_state` (§4.10) MAY be non-`none` at the moment of terminal failure (e.g., an operator-stopped run reaches `discarded`); per §4.10.WM-037a, the terminal transition MUST clear any non-`none` `interrupt_state` back to `none` — terminal states are absorbing and do not carry interrupt signals. The branch MUST NOT be deleted; the branch reference in the run's bead history per [reconciliation/schemas.md §6.2] preserves auditability.

Tags: mechanism

#### WM-033 — Startup orphan sweep removes stale lease-lock files only

On daemon startup, the orphan sweep (per [process-lifecycle.md §4.2 PL-006]) MUST remove stale lease-lock files from worktrees whose owning daemon is no longer running. The sweep MUST NOT delete worktree directories or branches. Full worktree cleanup is an operator-initiated workflow, not a startup action.

**Staleness detection — content-first, mtime tiebreaker.** Staleness MUST be determined PRIMARILY from the lease-lock file's JSON content (WM-013a): the recorded `pid` and `created_at` combined with the liveness-probe discipline of [handler-contract.md §4.10 HC-044a] (recorded PID not live OR recorded PID recycled to a non-harmonik-daemon argv) establish staleness. Filesystem `mtime` MAY be used ONLY as a tie-breaker when the content-based probe is ambiguous (e.g., PID is live but argv identifies a different harmonik-daemon generation than the current one). A lock whose content parses cleanly and whose recorded PID is live and identifies a harmonik-daemon of the CURRENT generation is NOT stale regardless of mtime; a lock whose recorded PID is dead is stale regardless of mtime. The prior v0.3.0 dependence on PL-006's mtime-only rule is narrowed: mtime is a tiebreaker, not the primary signal.

**Post-sweep `git worktree prune`.** After removing stale lease-lock files AND after WM's own orphan-directory detection (WM-003a routes to reconciliation Cat 3; routing does NOT itself remove the git metadata), the sweep MUST invoke `git worktree prune` against `<repo>` to drop stale `<repo>/.git/worktrees/<name>/` metadata entries for any worktree directory the sweep has deemed gone or never existed on disk. `git worktree prune` is safe: it skips entries for which `git worktree lock` was externally issued, and harmonik itself never issues `git worktree lock` (see I5 below and the glossary entry for **worktree-lock disjoint**). An operator-issued `git worktree lock` on a harmonik-managed worktree is respected by the prune pass and does not block harmonik lifecycle progress; the operator is responsible for unlocking before the next sweep if they intend harmonik to manage the entry.

**CROSS-SPEC COORDINATION (elevated in v0.4.0).** WM, HC, and PL currently disagree on the lock file's filename and content. WM declares the canonical lock at `${workspace_path}/.harmonik/lease.lock` with structured JSON content per WM-013a. HC-044a names `.harmonik/worktrees/<run_id>/.lock` (equivalent to `${workspace_path}/.lock`) with bare-PID content. PL-006 cites `.harmonik/lease.lock` with mtime-based staleness. WM v0.4.0's canonical path AND structured JSON content format are AUTHORITATIVE for the corpus; OQ-WM-005 is elevated from DEFERRED to **BLOCKING-CROSS-SPEC** — HC and PL MUST align to WM's canonical path and JSON content format by their next revision cycle. The WM-side sweep matches WM's path; implementations encountering an HC-style or PL-style lock file on disk from a prior generation MUST treat it as a migration artifact and handle per the OQ-WM-005 resolution default. WM does NOT write a second legacy-compatible lock file.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.9 Re-run rule

#### WM-034 — reopen-bead verdict triggers fresh worktree and fresh branch and mints a fresh run_id

When the investigator agent issues a `reopen-bead` verdict per [reconciliation/spec.md §4.5 RC-020] and [reconciliation/schemas.md §6.1 Verdict], the subsequent claim of the reopened bead MUST produce a new run with a FRESH `run_id` distinct from every prior run_id ever dispatched against the bead. The fresh run_id becomes the lease key for a FRESH worktree at the canonical path `<repo>/.harmonik/worktrees/<new_run_id>/` and a FRESH task branch named `run/<new_run_id>` per §4.2.WM-005. The prior run's `run_id`, worktree, and branch persist on disk per §4.8.WM-031 (their lease-lock files have been released per §4.3.WM-013b). This rule closes the tension between path canonicality (§4.1.WM-002) and failed-run persistence (§4.8.WM-031): distinct runs land at distinct paths because their `run_id`s differ.

The minting of the fresh `run_id` is the execution-model's responsibility (the new run passes through the normal run-create path per [execution-model.md §4.3]); this spec asserts only that the workspace manager MUST observe a new `run_id` on entry and MUST reject any attempt to reuse a prior `run_id` at workspace-create time.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### WM-035 — Intra-run rollback verdicts keep the same worktree

Intra-run rollback verdicts — `resume-here`, `resume-with-context`, `reset-to-checkpoint` per [reconciliation/spec.md §4.5 RC-020] and [reconciliation/schemas.md §6.1 Verdict] — MUST keep the same worktree and the same task branch. The run's `run_id` is unchanged; the state reverts to the named checkpoint via git operations inside the existing worktree per [execution-model.md §4.10 EM-044] — the `rollback_to_state_id` field on the transition record (EM-044) is the mechanical driver of the rollback.

Tags: mechanism

#### WM-036 — Re-run vs intra-run classification is deterministic on the verdict enum

The decision between "fresh worktree" (§4.9.WM-034) and "keep worktree" (§4.9.WM-035) MUST be deterministic on the verdict enum value declared in [reconciliation/schemas.md §6.1 Verdict]. No cognition participates in the classification. The authoritative classification against the seven-value verdict enum (per RC-020 as amended in RC v0.3.0) is:

| Verdict | Workspace disposition | Handled by |
|---|---|---|
| `reopen-bead` | fresh worktree + fresh task branch + fresh run_id | §4.9.WM-034 |
| `resume-here` | keep worktree + keep task branch | §4.9.WM-035 |
| `resume-with-context` | keep worktree + keep task branch | §4.9.WM-035 |
| `reset-to-checkpoint` | keep worktree + keep task branch (state reverts via EM-044) | §4.9.WM-035 |
| `accept-close-with-note` | no re-run attempted; workspace remains in its current terminal state | (terminal) |
| `no-op-accept` | no workspace action; record the verdict; clear any non-`none` `interrupt_state` per §4.10.WM-040 if the workspace was previously marked interrupted by reconciliation; outer run continues in its current state per [reconciliation/spec.md §8.12] and [reconciliation/schemas.md §6.2] | (no-op) |
| `escalate-to-human` | no re-run attempted; workspace remains in its current terminal state; operator escalation per [operator-nfr.md §4.3] | (terminal) |

Prior WM v0.2 text naming `abandon` as a verdict is retired per §12; `abandon` is NOT in the reconciliation verdict enum. Any verdict value not in the table above is a malformed verdict per [reconciliation/spec.md §4.5 RC-023] and MUST NOT be routed by this spec — the classifier MUST return a classification-error to the caller and emit no workspace action. Additions to the verdict enum require a reconciliation amendment; the associated workspace-disposition mapping MUST be declared in the reconciliation spec (the disposition attribute is a reconciliation-owned concern; see OQ-WM-009 for the proposed bilateral attribute `worktree_disposition`). The `no-op-accept` row resolves OQ-RC-011 (WM v0.4.1 lacked the seventh-verdict mapping after RC v0.3.0 added it).

Tags: mechanism

### 4.10 Interrupt-state representation

#### WM-037 — interrupt_state is orthogonal to lifecycle state for in-flight states

The workspace record MUST carry an `interrupt_state` field orthogonal to the lifecycle state of §4.4 FOR THE IN-FLIGHT STATES of §7.1: `created`, `ready`, `leased`, `merge-pending`, `conflict-resolving`. Values: `none` (default; no interruption), `operator-paused` (operator pause interrupted the lease), `operator-stopped-graceful` (graceful stop in progress), `operator-stopped-immediate` (immediate stop; handler subprocess may have been killed), `daemon-crash-suspected` (pre-reconciliation placeholder for a run whose lease was not cleanly released). The lifecycle state (e.g., `leased`) and the interrupt-state (e.g., `operator-paused`) compose independently within the in-flight set.

The `interrupt_state` enum values are locally owned by this spec, but they map onto the operator-control vocabulary and reconciliation categories owned elsewhere: `operator-paused` / `operator-stopped-graceful` / `operator-stopped-immediate` correspond to operator-control states per [operator-nfr.md §4.3 ON-011]; `daemon-crash-suspected` corresponds to the Cat 6 detector carve-out per [reconciliation/spec.md §8.11]. This spec does NOT redefine those vocabularies; it projects them onto a workspace-local field for dispatch convenience.

Tags: mechanism

#### WM-037a — Terminal lifecycle states carry no interrupt signal

The terminal lifecycle states `merged` and `discarded` MUST carry `interrupt_state = none`. A transition into a terminal state MUST clear any non-`none` interrupt state as part of the transition. Any interrupt signal received against a workspace already in a terminal state MUST be rejected silently by the workspace manager and logged as an operator-observability event per [operator-nfr.md §4.9]; the workspace state MUST NOT transition and `workspace_interrupted` (emitted by reconciliation per §4.10.WM-039) MUST NOT fire against a terminal workspace. This rule bounds the orthogonality claim of §4.10.WM-037 to the in-flight state set and eliminates the "interrupt a merged workspace" semantic anomaly.

Tags: mechanism

#### WM-038 — interrupt_state is set by operator and reconciliation pathways; WM is sole writer

Transitions of `interrupt_state` are driven by (a) the operator control subsystem per [operator-nfr.md §4.3 ON-013] emitting operator-control events that the workspace manager observes and applies; (b) reconciliation per [reconciliation/spec.md §4.5] emitting verdicts that the workspace manager applies. The workspace manager MUST be the SOLE writer of the `interrupt_state` field on the Workspace record. The workspace manager MUST NOT mutate `interrupt_state` except in response to (a) an observed operator-control event or (b) an observed reconciliation verdict execution. No other subsystem may mutate the field; cross-subsystem requests to change interrupt_state MUST route through the event bus.

Tags: mechanism

#### WM-038a — Workspace-local marker for operator- and verdict-driven interrupt_state transitions

When the workspace manager mutates `interrupt_state` in response to an operator-control event (pause, stop, upgrade) OR a reconciliation verdict (crash-recovery disposition), the system currently has NO durable wire-event signaling the mutation on the bus: [event-model.md §8.5.5] names the reconciliation detector as the sole emitter of `workspace_interrupted` and scopes the event to Cat 6 crash detection. Operator-driven and verdict-driven transitions therefore risk being invisible to downstream consumers (reconciliation, observability) until the next full reconciliation pass discovers the mutated record.

To close this gap locally (without waiting for EV's emitter broadening in OQ-WM-016), the workspace manager MUST on every `interrupt_state` mutation (`none → non-none` OR `non-none → non-none` transition) append a single workspace-scoped JSONL line to `${workspace_path}/.harmonik/events/workspace-<workspace_id>.jsonl` and fsync that file. The marker shape is:

```
{"event":"interrupt_state_changed",
 "workspace_id":"<id>","run_id":"<id>",
 "prior_interrupt_state":"<enum>","new_interrupt_state":"<enum>",
 "cause":"<operator-event-type | verdict-kind>",
 "cause_event_id":"<optional event_id from bus>",
 "changed_at":"<rfc3339>"}
```

The workspace-local marker is the authoritative record of the transition for reconciliation's consumer pass (consumed on each reconciliation sweep per [reconciliation/spec.md §4.3]). Additionally, WM MAY emit an async bus event as a bus-level hint for in-process observers, but the marker is the durability signal. Promotion of WM to a valid emitter of `workspace_interrupted` (for operator + verdict paths) OR introduction of a new `workspace_interrupt_state_changed` event type is the subject of OQ-WM-016 (coordination with EV); WM v0.4.0 ships the local marker unconditionally and defers the bus-event decision.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### WM-039 — Workspace manager ensures interrupt_state transitions are observable; workspace_interrupted is emitted by reconciliation

On any transition of `interrupt_state` from `none` to a non-`none` value, the workspace manager MUST update the record atomically with the causing input (operator-control event or reconciliation verdict) such that the new state is durable and observable by the reconciliation detector. The `workspace_interrupted` event per [event-model.md §8.5.5] is emitted by the reconciliation detector, NOT by the workspace manager; payload schema (`workspace_id`, `run_id`, `detected_at`, `category`) is EV-owned. The workspace manager MUST NOT emit `workspace_interrupted`.

This rule reflects a split of emission authority: the workspace manager owns the interrupt_state FIELD (mutation per WM-038); the reconciliation detector owns the wire EVENT (emission per EV §8.5.5). The prior WM v0.2 text declaring the workspace manager as emitter of `workspace_interrupted` and enumerating payload fields (`prior lifecycle state`, `new interrupt_state`) is retired per §12 — payload field lists are EV-owned per EV-025. OQ-WM-006 tracks the unresolved coordination with EV: EV currently lists a single emitter (reconciliation detector) and a payload shape focused on Cat 6 crash detection; operator-driven interrupts may need a second emitter row or an enriched payload. Until OQ-WM-006 resolves, this spec cedes to EV.

Tags: mechanism

#### WM-040 — interrupt_state reset requires reconciliation or operator resume

Clearing `interrupt_state` back to `none` MUST be driven by either (a) an `operator_resuming` event per [event-model.md §8.7] and [operator-nfr.md §4.3 ON-013] for operator-initiated interrupts, or (b) a reconciliation verdict per [reconciliation/spec.md §4.5] for daemon-crash or lost-lease interrupts. The workspace manager MUST NOT silently clear the field. Sensor for this rule: any mutation of `interrupt_state` to `none` not preceded by a matching causing event is a violation, detected by the reconciliation detector's transition-audit pass per [reconciliation/spec.md §4.3 RC-010].

Tags: mechanism

## 5. Invariants

> NOTE (v0.3.0): Invariants in this spec are cross-subsystem claims: each constrains the behavior of two or more subsystems and each names a sensor per [architecture.md §4.9 AR-042]. Three v0.2 entries (WM-INV-001, WM-INV-002, WM-INV-005) were §4 requirements promoted verbatim and were reworded in v0.3 to satisfy the template §5 selection test. WM-INV-004 was a duplicate of §4.6.WM-022 and is retired; its content lives at WM-022.

#### WM-INV-001 — Lease-by-run is honored corpus-wide

Every subsystem observing a workspace MUST treat the workspace's `run_id` as the exclusive lease holder for the run's full lifetime (per WM-010 definition). No subsystem MAY mechanism a per-agent lease acquisition: agents within a run sequentially occupy the same worktree under one lease. This invariant constrains the orchestrator (S01; dispatch), the agent runner (S04; handler-launch preconditions), the workspace manager (S06; storage), reconciliation (lost-lease detection), and the memory layer (S08; session-metadata consumption). It is the workspace-layer realization of the centralized-controller principle per [architecture.md §4.9 AR-INV-007].

Sensor: reconciliation's startup lost-lease detector per [reconciliation/spec.md §8.11 Cat 6] correlates each live lease-lock file (§4.3.WM-013a) against Beads' record of the owning `run_id` and against the daemon's live-run registry; any workspace with a lease-lock whose `run_id` does not appear as an in-flight run (or whose lease-lock is missing while the run is in-flight) is a violation evidence route.

Tags: mechanism

#### WM-INV-002 — One in-flight run per bead

For every bead, every subsystem routing on run activity MUST assume at-most-one in-flight run. Multi-run-per-bead is allowed across time only after the prior run has reached a terminal state per [execution-model.md §4.3 EM-014]. Reconciliation's post-crash detectors MUST reject observations of multiple in-flight runs for one bead as inconsistency evidence per [reconciliation/spec.md §8.11 Cat 6a]. Beads-integration's terminal-write rule (BI-010) and the workspace-manager's WM-012 both rely on this.

Sensor: the Cat 6a detector (bead `in_progress` with two+ task branches each advertising `Harmonik-Run-ID` without `Harmonik-Verdict-Executed`) per [reconciliation/schemas.md §6.3].

Tags: mechanism

#### WM-INV-003 — Checkpoint commits obey git append-only semantics on the task branch

Any git observer of a run's task branch MUST reject commits whose new tip is not a fast-forward descendant of the prior tip (i.e., no amend, rebase, filter-branch, force-push, `git replace` rewrite, or history-editing operation may rewrite history on a task branch that has emitted `workspace_leased`). Git's native commit semantics are the contract; this spec adds no transactional layer. The rule constrains the handler-contract (S04 handlers writing checkpoint commits per [handler-contract.md §4.1]), the workspace manager (merge-back in §4.5), and execution-model's checkpoint-cadence rule per [execution-model.md §4.5 EM-023].

Sensor (two-part):

- **Part A: branch-tip monotonicity.** [execution-model.md §4.5 EM-024a] branch-tip monotonicity check — the daemon persists the last-observed task-branch-tip SHA per in-flight run and routes any non-fast-forward discrepancy to reconciliation Cat 3 per [reconciliation/spec.md §8.4]. This part catches amend/rebase/force-push.
- **Part B: history-editing audit.** EM-024a alone does NOT catch `git filter-branch` / `git replace` / rewrites that preserve the tip SHA reachability but change ancestor content; a filter-branch that rewrote history and then cherry-picked the same tip-identity would pass Part A but violate the invariant. WM-INV-003 names a conformance obligation: a testing-layer invariant-auditor tool (scope and implementation tracked in OQ-WM-017 until `testing.md` lands) MUST walk `git reflog` entries for every live task branch and MUST reject any reflog entry whose operation is `filter-branch`, `replace`, or a non-fast-forward `update` on a run still in the in-flight state set. The auditor is a testable obligation enforced at conformance-test time, not at runtime (runtime dependence on reflog would be fragile: `git reflog expire` MAY prune entries older than a retention window).

**WM does NOT rely on reflog for recovery.** The daemon's state-reconstruction path (per [execution-model.md §4.7]) uses branch tips + JSONL, not reflog; the reflog dependency is confined to the invariant-auditor test tool. This makes WM robust against operator-configured aggressive `gc.reflogExpire`.

Tags: mechanism

#### WM-INV-004 — Retired

WM-INV-004 is retired in v0.3.0. Its prior content (merge-conflict resolver is the original implementer) is a single-subsystem rule now fully carried by §4.6.WM-022 (mechanical identification via `implementer_handler_ref`) and §4.6.WM-024 (re-dispatch mechanics). Per the template §5 selection test, a rule that fits in one subsystem's §4 without cross-subsystem reach is not an invariant. Do NOT reuse the ID.

Tags: mechanism

#### WM-INV-005 — Canonical-path discovery without a registry

Any subsystem reconstructing workspace state from the filesystem MUST derive the workspace location from `run_id` alone per §4.1.WM-002 without consulting a separate registry, index, or SQLite table. No registry-backed lookup MAY be declared authoritative for workspace path resolution. This invariant constrains the workspace manager (WM-013, WM-013c), reconciliation's startup pass (which walks the filesystem per [reconciliation/spec.md §4.3]), and the orchestrator's run-resume logic at restart.

Sensor: WM-013c filesystem discovery by construction — any workspace whose path does not match `<repo>/.harmonik/worktrees/<run_id>/` for some recognized `run_id` is a violation, detected by the startup enumeration pass and routed to reconciliation Cat 3c (inverse premature-close) or Cat 6a (integrity violation) per [reconciliation/schemas.md §6.3].

Tags: mechanism

## 6. Schemas and data shapes

### 6.1 Record schemas

```
RECORD Workspace:
    workspace_id              : String              -- stable identifier; deterministic from run_id (WM-004)
    run_id                    : UUID                -- the run this workspace is leased to
    repository                : String              -- absolute path to the LOCAL clone of the backing repo (WM-001, WM-002)
    parent_commit             : CommitSHA           -- commit SHA this worktree was branched from
    branch_name               : String              -- task branch name per §4.2 (e.g., "run/<run_id>")
    path                      : String              -- absolute filesystem path to the worktree
    state                     : WorkspaceState      -- lifecycle state per §4.4
    interrupt_state           : InterruptState      -- orthogonal interrupt field per §4.10
    bead_id                   : BeadID | None       -- correlation field; present iff run is bead-tied
    implementer_handler_ref   : HandlerRef | None   -- set at merge-pending per WM-022; null iff task branch has no agentic commits
    metadata                  : Map<String, String> -- closed map: keys "created_at" (RFC 3339), "operator_fingerprint"
    schema_version            : Integer             -- N-1 readable per §6.4; set by S06 on create
```

```
RECORD LeaseLockFile:                              -- per-workspace file at §6.2 canonical path; JSON body (WM-013a)
    run_id         : UUID                          -- owning run
    pid            : Integer                       -- daemon PID that wrote the lock
    created_at     : Timestamp                     -- RFC 3339 wall clock
    ttl_sec        : Integer                       -- advisory lifetime; does not enforce auto-expiry
```

```
ENUM WorkspaceState:
    created
    ready
    leased
    merge-pending
    conflict-resolving
    merged
    discarded
```

> NOTE (v0.3.0): `setup` retired; see §12 for migration impact.

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
    node_id        : String                  -- namespaced <parent_node_id>/<sub_node_id> inside expanded sub-workflows (WM-026)
    agent_type     : String                  -- agent-type conformance class per [architecture.md §4.7 AR-024]
    workflow_id    : UUID
    bead_id        : BeadID | None           -- present iff run is bead-tied
    launched_at    : Timestamp               -- RFC 3339 wall clock
    schema_version : Integer                 -- N-1 readable per §6.4; set by S06
```

Type aliases: `CommitSHA`, `BeadID`, and `HandlerRef` are defined in the owning specs — `CommitSHA` is a 40-hex-char git object name ([execution-model.md §6.1]); `BeadID` is the bead identifier type per [beads-integration.md §6.1]; `HandlerRef` is defined per [handler-contract.md §6.1]. `UUID` is UUIDv7 per [event-model.md §4.1 EV-002].

### 6.2 Canonical on-disk paths

| Path | Owner | Purpose |
|---|---|---|
| `<repo>/.harmonik/worktrees/` | S06 | Worktree root (operator-configurable prefix per WM-002). |
| `<repo>/.harmonik/worktrees/<run_id>/` | S06 | Per-run worktree directory. |
| `${workspace_path}/.harmonik/sessions/` | S06 | Session-log root within a worktree. |
| `${workspace_path}/.harmonik/sessions/<session_id>/` | S06 creates; S04 writes logs | Per-session directory. |
| `${workspace_path}/.harmonik/sessions/<session_id>/harmonik.meta.json` | S06 | Metadata sidecar per §4.7. |
| `${workspace_path}/.harmonik/sessions/<session_id>/session.log` | S04 | Handler-written session log (format per-handler). |
| `${workspace_path}/.harmonik/lease.lock` | S06 | Lease-lock file; content per WM-013a; birth at `leased`, death at `merged` / `discarded` per WM-013b; swept if stale per WM-033. *Cross-spec coordination: HC-044a names a different path; see OQ-WM-005 (elevated to BLOCKING-CROSS-SPEC; WM's path is authoritative).* |
| `${workspace_path}/.harmonik/events/workspace-<workspace_id>.jsonl` | S06 | Workspace-local durability JSONL file; append-only; carries `lease_released` markers (§4.3.WM-013b) and `interrupt_state_changed` markers (§4.10.WM-038a). Consumed by reconciliation on each sweep. |
| `${workspace_path}/.harmonik/sessions/<session_id>/harmonik.meta.json.tmp-<pid>` | S06 (transient) | Transient temp file during atomic sidecar write per WM-026; orphans are removed by the startup sweep. |
| `<repo>/.harmonik/worktrees/merge-<merge_id>/` | S06 (transient) | Scratch merge-worktree per §4.5.WM-019a option (b); unleased; lifecycle is create-use-remove within one merge operation. |
| `${workspace_path}/.harmonik/review.json` | reviewer agent writes; S06 archives per §4.7.WM-027a | Reviewer verdict for the current iteration of a `review-loop` run; conforms to the `agent-reviewer` JSON verdict schema v1; excluded from checkpoint commits via WM-013e. |
| `${workspace_path}/.harmonik/review.iter-<N>.json` | S06 (per-iteration archive) | Archived reviewer verdict for prior iteration `<N>` (1-indexed; iteration cap = 3 per [execution-model.md §4.3]); written by the daemon's atomic rename of the prior `.harmonik/review.json` before the next reviewer launch per §4.7.WM-027a; excluded from checkpoint commits via WM-013e. |
| `${workspace_path}/.harmonik/reviewer-feedback.iter-<N>.md` | S06 (per-iteration write) | Reviewer-feedback delivery file written by the daemon before launching `implementer-resume` for iteration `N+1`; contains verdict, flags, full notes, and diff_summary from the iteration-`N` reviewer pass; atomic write per WM-026; excluded from checkpoint commits via WM-013e; persists post-run; governed by [execution-model.md §4.3 EM-015d-RFD]. |
| `${workspace_path}/.harmonik/review-target.md` | S06 (per-reviewer-launch write) | Reviewer input artifact written by the daemon atomically (WM-026) before each reviewer pane is spawned; contains bead id+title+body, base+head SHAs, prior-verdict summaries (iteration ≥ 2), and optional reviewer-tier hints; overwritten fresh per reviewer launch; excluded from checkpoint commits via WM-013e; persists post-run; governed by [execution-model.md §4.3 EM-015d-RIA]. Tag: WM-RIA-001. |
| `${workspace_path}/.harmonik/agent-task.md` | S06 writes; claude-code agent reads | Per-launch task artifact per [claude-hook-bridge.md §4.11 CHB-028]. Daemon materializes atomically (WM-026 discipline) before each Claude exec; reserved name — MUST NOT be overwritten by agents or operators. Excluded from checkpoint commits via WM-013e. |
| `${workspace_path}/.harmonik/agent-task.tmp-<pid>` | S06 (transient) | Transient temp file during atomic task-artifact write per CHB-028; orphans removed by startup sweep alongside other `.tmp-<pid>` files. |
| (`<integration branch>`) | S06 (target of merge) | Integration branch is a git branch, NOT a worktree. No separate on-disk directory is created by this spec; the task-branch squash-merge lands on this branch's tip per §4.5.WM-019. |

### 6.4 Schema evolution

`Workspace` and `SessionMetadataSidecar` records carry a `schema_version` integer. The compatibility contract is N-1 readable per [operator-nfr.md §4.5 ON-018]: a reader MUST accept the immediately prior schema version. Additive changes (new optional field) are non-breaking and bump the version; renaming or removing fields is breaking and requires a migration release. The `schema_version` value is set by the workspace manager at record creation; consumers that mutate the record (currently: only the workspace manager per WM-038 for `interrupt_state`) MUST preserve the existing `schema_version`.

### 6.5 Co-owned event payloads

This section consolidates the event-emission obligations of §4 (previously §6.3 in v0.2; renumbered to match the template §6.5 slot for co-owned event payloads).

Event type names AND payload schemas are authoritative in [event-model.md §8.5] per EV-025. This spec is normative only for emission-when (WHEN each event fires); the WHAT (name, payload fields) is read from EV.

| Event (EV anchor) | Emission-when (this spec) | Emitter |
|---|---|---|
| `workspace_created` ([EV §8.5.1]) | Transition to `created` in §7.1 (after `git worktree add` succeeds per WM-003). | workspace-manager (S06) |
| `workspace_leased` ([EV §8.5.2]) | Transition to `leased` in §7.1 (after the four-step ordering gate of WM-016). | workspace-manager (S06) |
| `workspace_merge_status` ([EV §8.5.3]) — `status=pending` | Transition into `merge-pending` in §7.1. | workspace-manager (S06) |
| `workspace_merge_status` ([EV §8.5.3]) — `status=merged` | Transition into `merged` in §7.1 on successful merge per WM-021. | workspace-manager (S06) |
| `workspace_discarded` ([EV §8.5.4]) | Transition into `discarded` in §7.1 (failed-run failure per WM-032, or post-escalation terminal per WM-023). | workspace-manager (S06) |
| `workspace_interrupted` ([EV §8.5.5]) | NOT emitted by WM. Observed by WM when the reconciliation detector emits per [event-model.md §8.5.5]; the workspace manager mutates `interrupt_state` per WM-038. | reconciliation detector (per [reconciliation/spec.md §4.3]) |
| `merge_conflict_escalation` ([EV §8.5.6]) | Escalation path of WM-023 (implementer re-dispatch exhausted, or all-mechanical branch per WM-022a). | workspace-manager (S06) |
| `session_log_location` ([EV §8.3.7]) | NOT emitted by WM. Emitted by the watcher translating the handler's progress-stream message per [handler-contract.md §4.2 HC-010]; WM's contribution is the preexistence of the session-log directory per §4.7.WM-025. | handler watcher (per [handler-contract.md §4.2 HC-010]) |

**Workspace-local durability markers (not bus events).** In addition to the bus events above, WM writes two classes of workspace-scoped markers to `${workspace_path}/.harmonik/events/workspace-<workspace_id>.jsonl`:

- `lease_released` per §4.3.WM-013b (written on every terminal release; reconciliation consumes as the durable release signal distinct from any class-O bus event).
- `interrupt_state_changed` per §4.10.WM-038a (written on every `interrupt_state` mutation; reconciliation consumes as the durable local signal for operator/verdict-driven transitions that EV's `workspace_interrupted` currently does not cover — see OQ-WM-016).

These markers are NOT bus events; they are workspace-local, consumed by reconciliation's sweep pass (per [reconciliation/spec.md §4.3]).

Per EV-025, payload fields for every row above are read from EV. Prior WM v0.2 inline enumeration of payload fields (at WM-015, WM-017, WM-023, WM-039) is retired per §12.

## 7. Protocols and state machines

### 7.1 Workspace lifecycle state machine

States: `created`, `ready`, `leased`, `merge-pending`, `conflict-resolving`, `merged`, `discarded`. Terminal states: `merged`, `discarded`. In-flight states: everything else. Per §4.10.WM-037a, `interrupt_state ≠ none` is valid only in in-flight states.

| From | Event | Guard | To | Emits |
|---|---|---|---|---|
| (initial) | orchestrator issues create | run_id unique, path free per WM-002 | `created` | `workspace_created` |
| `created` | `git worktree add` succeeds AND sessions_dir exists | WM-003 succeeded; `${path}/.harmonik/sessions/` mkdir succeeded | `ready` | — |
| `ready` | first session's sidecar + lease-lock file written and fsynced | sidecar per WM-026; lease-lock per WM-013a | `leased` | `workspace_leased` |
| `leased` | merge node dispatched per WM-018a | task branch ready to merge per WM-018 | `merge-pending` | `workspace_merge_status (status=pending)` |
| `merge-pending` | merge succeeds | `git merge --squash` + commit succeeded, no conflict markers | `merged` | `workspace_merge_status (status=merged)` |
| `merge-pending` | merge conflicts detected | WM-020 conflict detection rule | `conflict-resolving` | — |
| `conflict-resolving` | implementer resolves | resolution commit exists on task branch and merge re-attempt succeeds | `merge-pending` | — |
| `conflict-resolving` | implementer re-dispatch exhausted OR all-mechanical per WM-022a | WM-023 trigger conditions | `discarded` | `merge_conflict_escalation`, `workspace_discarded` |
| `leased` | run reaches terminal failure | run state = `failed` or `canceled` per [execution-model.md §7.1] | `discarded` | `workspace_discarded` |
| in-flight (`created`, `ready`, `leased`, `merge-pending`, `conflict-resolving`) | operator pause/stop OR lost-lease detection applied per WM-038 | causing event observed | (same lifecycle state, `interrupt_state ≠ none`) | (WM observes only; `workspace_interrupted` is emitted by reconciliation per WM-039) |
| in-flight | `interrupt_state` cleared per WM-040 | `operator_resuming` OR reconciliation verdict observed | (same lifecycle state, `interrupt_state = none`) | — |
| in-flight → terminal (`merged` or `discarded`) | transition per rows above | any in-flight state with `interrupt_state` possibly non-`none` | terminal (`interrupt_state` MUST be reset to `none` per WM-037a) | (as per row) |

> NORMATIVE: The `interrupt_state` field (§4.10) composes orthogonally with the lifecycle state ONLY within the in-flight state set. Terminal states MUST carry `interrupt_state = none` per WM-037a. An interrupt signal received against a terminal workspace MUST be rejected silently per WM-037a; no transition, no `workspace_interrupted` emission.

### 7.2 Worktree-create-and-stamp sequence (protocol pseudocode)

```
FUNCTION create_workspace(run_id, parent_commit, bead_id | None):
    # §7.1 initial → created → ready transitions.
    path = worktree_root() + "/" + run_id             # WM-002
    IF reuse_of_prior_run_id(run_id): RETURN ERROR(RunIdReuseForbidden)  # WM-034
    IF path already exists: RETURN ERROR(WorkspaceAlreadyExists)         # §8 taxonomy
    branch = "run/" + run_id                          # WM-005
    # WM-003: the -b form creates the branch atomically with the worktree.
    # start_point is the explicit parent_commit, not HEAD.
    git.cmd("worktree", "add", "-b", branch, path, parent_commit)
    sessions_dir = path + "/.harmonik/sessions"
    mkdir(sessions_dir)
    events_dir = path + "/.harmonik/events"            # WM-013b, WM-038a
    mkdir(events_dir)
    workspace = Workspace(
        workspace_id = "ws-" + run_id,                # WM-004
        run_id       = run_id,
        repository   = local_repo_path(),             # WM-001, WM-002
        parent_commit = parent_commit,
        branch_name  = branch,
        path         = path,
        state        = "created",
        interrupt_state = "none",
        bead_id      = bead_id,
        implementer_handler_ref = None,
        metadata     = { "created_at": now_rfc3339(), "operator_fingerprint": operator_fingerprint() },
        schema_version = CURRENT_SCHEMA_VERSION)      # §6.4
    emit_event("workspace_created", workspace)        # WM-015 (name & payload per EV §8.5.1)
    workspace.state = "ready"                         # §7.1 created → ready (no emission)
    RETURN workspace

FUNCTION launch_session(workspace, session_id, node_id, agent_type, workflow_id):
    # Called once per session. Writes sidecar per WM-026.
    # On the FIRST session only, also writes the lease-lock and emits workspace_leased (WM-016, WM-027).
    session_dir = workspace.path + "/.harmonik/sessions/" + session_id
    mkdir(session_dir)
    sidecar = SessionMetadataSidecar(
        run_id       = workspace.run_id,
        node_id      = node_id,
        agent_type   = agent_type,
        workflow_id  = workflow_id,
        bead_id      = workspace.bead_id,
        launched_at  = now_rfc3339(),
        schema_version = CURRENT_SCHEMA_VERSION)
    # WM-026 atomic discipline: temp file + fsync + rename + parent-dir fsync.
    write_json_atomic_fsync(session_dir, "harmonik.meta.json", sidecar)  # WM-026
    # write_json_atomic_fsync:
    #   1. write JSON to ${session_dir}/harmonik.meta.json.tmp-${pid}
    #   2. fsync the temp file
    #   3. rename(temp, "${session_dir}/harmonik.meta.json")
    #   4. fsync(session_dir)  [parent-dir fsync - REQUIRED]

    IF workspace.state == "ready":
        # First session. WM-016 ordering: write lease-lock then transition then emit.
        # WM-013a atomic discipline matches WM-026: temp+fsync+rename+parent-fsync.
        write_lease_lock_atomic_fsync(workspace.path + "/.harmonik",
            "lease.lock",
            { "run_id": workspace.run_id,
              "pid": current_pid(),
              "created_at": now_rfc3339(),
              "ttl_sec": DEFAULT_LEASE_TTL_SEC })
        workspace.state = "leased"                      # state transitions BEFORE emission
        emit_event("workspace_leased", workspace)       # WM-015 (EV §8.5.2)
    # Subsequent sessions: no state transition, no emission (WM-027 note).

FUNCTION enter_merge_pending(workspace):
    # WM-022: identify the implementer_handler_ref before emitting.
    workspace.implementer_handler_ref = resolve_latest_agentic_handler_ref(workspace.path, workspace.branch_name)
    workspace.state = "merge-pending"
    emit_event("workspace_merge_status", workspace, status="pending")  # WM-015, WM-021 (EV §8.5.3)

FUNCTION complete_merge(workspace, merge_commit_hash, target_branch):
    workspace.state = "merged"
    emit_event_fsync("workspace_merge_status", workspace, status="merged",
               source_branch=workspace.branch_name,
               target_branch=target_branch,
               merge_commit_hash=merge_commit_hash)   # WM-021 (EV §8.5.3), class F
    # WM-013b release gate: F-class event is now durable; safe to unlink lock.
    write_workspace_local_marker(workspace, "lease_released", reason="merged")
    release_lease_lock(workspace.path + "/.harmonik/lease.lock")       # WM-013b
    # interrupt_state cleared to "none" per WM-037a on terminal entry (set by caller).

FUNCTION discard_workspace(workspace, reason):
    workspace.state = "discarded"
    # interrupt_state cleared to "none" per WM-037a on terminal entry.
    workspace.interrupt_state = "none"
    # WM-013b: the release gate depends on the reason.
    IF reason == "run_failed":
        # Wait for run_failed (EV class F) to be durably flushed.
        await_event_fsynced("run_failed", workspace.run_id)  # EV §8.1.3
    ELIF reason == "post_escalation":
        # merge_conflict_escalation already emitted; workspace-local marker is the gate.
        pass
    ELIF reason == "verdict_driven":
        await_event_fsynced("reconciliation_verdict_executed", workspace.run_id)
    emit_event("workspace_discarded", workspace, reason=reason)         # WM-015 (EV §8.5.4), class O
    # workspace_discarded is class O (ordinary, not fsynced) per EV; we do NOT gate on it.
    # OQ-WM-013 tracks a future EV reclassification to F.
    write_workspace_local_marker(workspace, "lease_released", reason=reason)  # WM-013b durable marker
    release_lease_lock(workspace.path + "/.harmonik/lease.lock")        # WM-013b

FUNCTION write_workspace_local_marker(workspace, event_name, **fields):
    # WM-013b, WM-038a workspace-local durability JSONL.
    path = workspace.path + "/.harmonik/events/workspace-" + workspace.workspace_id + ".jsonl"
    line = json_encode({
        "event": event_name,
        "workspace_id": workspace.workspace_id,
        "run_id": workspace.run_id,
        **fields,
        "recorded_at": now_rfc3339()
    })
    append_line_fsync(path, line)

FUNCTION set_interrupt_state(workspace, new_value, cause, cause_event_id | None):
    # WM-038 sole-writer rule; WM-038a local marker.
    prior = workspace.interrupt_state
    workspace.interrupt_state = new_value
    write_workspace_local_marker(workspace, "interrupt_state_changed",
        prior_interrupt_state=prior,
        new_interrupt_state=new_value,
        cause=cause,
        cause_event_id=cause_event_id)
```

Every branch point above corresponds to a normative requirement: worktree-add (§4.1.WM-003), path convention (§4.1.WM-002), branch naming (§4.2.WM-005), metadata sidecar write (§4.7.WM-026), lease emission ordering (§4.4.WM-016), lease-lock birth (§4.3.WM-013a), lease-lock release (§4.3.WM-013b), implementer identification (§4.6.WM-022), fresh run_id rule (§4.9.WM-034), terminal `interrupt_state` clearance (§4.10.WM-037a).

## 8. Error and failure taxonomy

This subsystem mutates filesystem and git state; failure classes MUST be named so their downstream routing (reconciliation detection, operator-observability) is well-defined.

| Class | Triggered when | Workspace transition | Downstream routing |
|---|---|---|---|
| `WorkspaceAlreadyExists` | `create_workspace` observes an existing directory at the canonical path | (none) | orchestrator reports run-create failure; reconciliation Cat 3c may fire on reboot if a prior run's terminal transition did not complete. |
| `RunIdReuseForbidden` | `create_workspace` observes that `run_id` has been dispatched before (violates WM-034) | (none) | orchestrator reports run-create failure; this is a caller-error class. |
| `WorktreeCreationFailed` | `git worktree add` returns non-zero (bad parent_commit, concurrent git lock, disk full) | (remains in initial) | orchestrator routes to run-create failure; operator-observability per [operator-nfr.md §4.1 ON-001]. |
| `LeaseLockHeldByOrphan` | `create_workspace` / `launch_session` observes a live lease-lock file belonging to a prior daemon generation per WM-013c / HC-044a | (stays out of `leased`) | Launch fail-fast per [handler-contract.md §4.10 HC-044a]; reconciliation Cat 6a investigator if correlation reveals a lost lease. |
| `SidecarWriteFailed` | metadata sidecar write fails on I/O (disk full, permissions, concurrent file conflict) | (stays out of `leased`) | run reported failed; operator-observability. |
| `MergeConflictUnresolvable` | implementer re-dispatch or all-mechanical-branch per WM-022a produces no successful merge | `conflict-resolving` → `discarded` | `merge_conflict_escalation` emission per WM-023. |
| `InterruptOnTerminalWorkspace` | operator-control or reconciliation signal targets a `merged` / `discarded` workspace | (no transition per WM-037a) | silent reject; operator-observability log entry only. |
| `RefNameInvalid` | integration-branch template substitution (WM-006a) produces a name that fails `git check-ref-format` after canonical fallback, or the transformed name is empty / collides case-insensitively | (remains in initial) | orchestrator reports run-create failure; operator-observability. |
| `BareWorktreeNoLease` | discovery (WM-003a) finds a registered worktree directory with no lease-lock and no sessions | (none; directory is orphan) | reconciliation Cat 3 via evidence type `bare-worktree-no-lease`. |
| `SidecarWithoutLease` | discovery (WM-003a) finds a registered worktree with sidecars but no lease-lock | (none; directory is orphan) | reconciliation Cat 3 via evidence type `sidecar-without-lease`. |
| `GitignoreWriteForbidden` | WM-013e detects `.gitignore` is missing required entries AND the daemon lacks write permission | (startup-fail) | daemon refuses to start; operator must fix `.gitignore` permissions. |
| `GitVersionTooOld` | WM-ENV-002 detects installed `git` below 2.34 at daemon startup | (startup-fail) | daemon refuses to start; operator must upgrade git. |

Detection of each class is the daemon's responsibility; class identity flows to the event bus via the appropriate lifecycle event's payload (event-model owns the schema), or to the operator-observability pipeline per [operator-nfr.md §4.9].

## 9. Cross-references

### 9.1 Depends on

- **[architecture.md §4.1]** — four-axis classification; workspace operations are mechanism-tagged by default, conflict-resolution is cognition-tagged.
- **[architecture.md §4.7 AR-024]** — `agent_type` conformance-class identifier consumed by §4.7.WM-026 SessionMetadataSidecar.
- **[architecture.md §4.9 AR-INV-007]** — centralized-controller invariant; lease-by-run invariant WM-INV-001 is the workspace-layer realization.
- **[architecture.md §4.9 AR-042]** — every invariant names a sensor; §5 invariants conform.
- **[architecture.md §4.0 AR-052, AR-053]** — spec-category front-matter and §4.a envelope obligation; this spec declares `runtime-subsystem` and carries WM-ENV-001.
- **[execution-model.md §4.3]** — `Run` record; `run_id` is the workspace lease key.
- **[execution-model.md §4.4 EM-017]** — checkpoint trailer schema (`Harmonik-Run-ID`, `Harmonik-State-ID`, `Harmonik-Transition-ID`, `Harmonik-Schema-Version`, optional `Harmonik-Bead-ID`) consumed by §4.5.WM-019 merge-preservation. **Note:** §4.6.WM-022 implementer identification does NOT read trailers; it walks session sidecars. A prior WM draft cited a `Harmonik-Actor-Role` trailer; no such trailer exists in EM-017. See OQ-WM-012 for the proposed trailer enrichment.
- **[execution-model.md §4.5 EM-023, EM-023a, EM-024a]** — checkpoint cadence, durability decision procedure, branch-tip monotonicity check (sensor for WM-INV-003).
- **[execution-model.md §4.7]** — state reconstruction from git + Beads; WM-004 relies on this to derive `workspace_id` on restart.
- **[execution-model.md §4.8 EM-034, EM-034a, EM-035]** — sub-workflow expansion pins the single-worktree rule for WM-005a; namespaced `node_id` for WM-026 sidecar inside expanded sub-workflows.
- **[execution-model.md §4.10 EM-044]** — `rollback_to_state_id` mechanics consumed by §4.9.WM-035 intra-run rollback verdicts.
- **[handler-contract.md §4.1]** — Handler interface consumed by §4.5.WM-018a (agentic merge-node variant) and §4.6.WM-024 (implementer re-dispatch).
- **[handler-contract.md §4.2 HC-010]** — `session_log_location` progress-stream message announces the pre-created directory (§4.7).
- **[handler-contract.md §4.10 HC-044a]** — fail-fast Launch if workspace held by prior-generation orphan; co-operates with WM-033 lock sweep. Disagrees on filename; see OQ-WM-005.
- **[handler-contract.md §4.11]** — skill provisioning; §4.6.WM-024 implementer re-dispatch includes a conflict-resolution skill in `required_skills` (skill name pending OQ-WM-007).
- **[handler-contract.md §6.1]** — `LaunchSpec` record consumed by §4.5.WM-018a and §4.6.WM-024; `workspace_path` is the WM-contributed field.
- **[control-points.md §4.7 CP-037]** — config-loading precedence; operator-configurable worktree-root (WM-002) reloaded per this precedence.
- **[reconciliation/spec.md §4.3]** — detector framework; sensor for WM-INV-001 and WM-INV-005.
- **[reconciliation/spec.md §4.5 RC-020]** — verdict vocabulary consumed by §4.9.WM-034 / WM-035 / WM-036 re-run rule.
- **[reconciliation/spec.md §8.11 Cat 6]** — lost-lease / integrity-violation detector; sensor for WM-INV-001.
- **[reconciliation/schemas.md §6.1 Verdict]** — authoritative verdict enum (six values).
- **[reconciliation/schemas.md §6.2 Verdict-execution table]** — workspace-side action per verdict; co-reference for §4.9.WM-036.
- **[operator-nfr.md §4.3]** — operator-control state machine; drives `interrupt_state` per §4.10.WM-038, WM-040.
- **[operator-nfr.md §4.5 ON-018]** — N-1 schema-compatibility contract applied at §6.4.
- **[operator-nfr.md §4.9]** — observability envelope; WM-033 cross-spec coordination notes and WM-031 retention-window OQ route through here.
- **[process-lifecycle.md §4.2 PL-006]** — startup orphan sweep mechanics consumed by §4.8.WM-033.
- **[beads-integration.md §4.5 BI-014]** — parent-child edge query; drives §4.2.WM-006 / WM-008 integration-branch selection.
- **[beads-integration.md §4.6 BI-017, BI-018]** — `bead_id` propagation into run metadata and checkpoint trailers; consumed by §4.1.WM-001 and §4.7.WM-028.

### 9.2 Reverse dependencies

> INFORMATIVE: Reverse dependencies are computed on demand from the foundation corpus and populated at finalize. Known reverse consumers as of 2026-04-24 (drawn from the corpus walk):
>
> - **handler-contract.md** — LaunchSpec.workspace_path (HC-006 / §6.1), HC-010 session-log pipeline, HC-044a orphan-held workspace fail-fast, HC-013a rotation turn-boundary interaction with worktree side-effects, §9.3 co-reference.
> - **event-model.md** — §1 Purpose, EV-005 internals carve-out, §6.5 emission-when map for workspace events.
> - **beads-integration.md** — BI-010 merge-completion ordering, BI-014 dependency-graph query producing WM-006 inputs, BI-020 session log metadata, §9.3.
> - **process-lifecycle.md** — §2.2 scope, PL-006 worktree locks, §4.4 graceful shutdown drain, PL-INV-001 co-reference.
> - **operator-nfr.md** — ON-015 overlay schema, ON-024 sandbox boundary, ON-027 drain ordering, ON-INV-003 sinks, §7 references.
> - **reconciliation/spec.md** — §1 Purpose, RC-015 investigator inputs, RC-028 reopen-bead, RC-029 intra-run rollback, §8.4 Cat 3c detection, §9.3; **note: these specs currently cite WM at legacy `§5.x` anchors that do not exist in WM v0.2+. See §A.4 for the migration map.**

### 9.3 Co-references (read-only consumption)

- **[execution-model.md §6.1 Run]** — this spec's §4.1.WM-001 consumes the `Run` shape; EM-side consumes `WorkspaceRef` (bidirectional consume-produce resolved directionally).
- **[event-model.md §8.5]** — workspace lifecycle event payload shapes (directional-split resolution with EV per EM/EV precedent); payload ownership cedes to EV per EV-025.
- **[event-model.md §8.3.7]** — `session_log_location` payload shape.
- **[event-model.md §8.7]** — operator-control event payloads consumed by §4.10.WM-038.
- **[event-model.md §8.9(h)]** — paired-phase single-event rule; drives the `workspace_merge_status` shape in §6.5.
- **[event-model.md §4.6 EV-025]** — each event type has exactly one owning spec for payload shape; WM cedes payloads to EV throughout §4.4 and §6.5.
- **[handler-contract.md §4.2 HC-010]** — `session_log_location` emission-when from the handler-watcher side.
- **[handler-contract.md §4.11]** — skill provisioning precedes handler launch; WM-016's emission ordering depends.
- **[reconciliation/spec.md §4.5 RC-020]** — verdict enum consumed by §4.9 re-run rule.
- **[reconciliation/schemas.md §6.2]** — verdict-execution table names the WM-side action for each verdict.
- **[operator-nfr.md §4.3 ON-011, ON-013]** — operator-control state machine and event emissions consumed by WM-038, WM-040.
- **[core-scope.md §3]** — MVH scope (plain `git worktree add`, no adze provisioning).

> NOTE: Legacy `[docs/foundation/components.md §N]` citations from WM v0.2 have been migrated to the owning-spec form per the cross-spec-architect's table. The four legacy sites retained in this spec are (a) the `core-scope.md §3` citations (no owning spec migration target) and (b) any post-MVH cleanup-workflow placeholder tracked in OQ-WM-010. No `[docs/foundation/components.md §N]` citations remain in the normative body of v0.3.0.

## 10. Conformance

### 10.1 Conformance profiles

**Core MVH.** An implementation conforming to Core MVH MUST pass every requirement in WM-001 through WM-040 (including `WM-003a`, `WM-005a`, `WM-006a`, `WM-013a`, `WM-013b`, `WM-013c`, `WM-013d`, `WM-013e`, `WM-018a`, `WM-019a`, `WM-022a`, `WM-037a`, `WM-038a`) AND the subsystem envelope WM-ENV-001 AND WM-ENV-002 AND every invariant WM-INV-001 through WM-INV-005 (excluding the retired WM-INV-004 and excluding the retired WM-017). Retired IDs (WM-017, WM-INV-004) MUST NOT be implemented — retired means "no-op, do not reintroduce." IDs are FROZEN at v0.4.0 per the front-matter ID freeze NOTE; future revisions MUST NOT renumber. No other requirement is deferred at MVH.

**Post-MVH extensions.** Adze environment provisioning (currently out of scope per §2.2), operator-configured post-merge archive paths (currently the non-default branch of §4.7.WM-030), failed-run retention-window defaults (OQ-WM-008), and automated dedicated-merge-agent dispatch (currently forbidden in MVH per §4.6.WM-022 / WM-024) are additive extensions.

### 10.2 Test-surface obligations

During bootstrap (before `testing.md` exists) test obligations are named in prose. Each requirement's test obligation:

- **WM-001 — WM-004, WM-003a (worktree primitive).** Filesystem-scenario tests: `git worktree add -b` runs produce the canonical path + fresh branch atomically; `workspace_id` is derivable from `run_id` after daemon restart; `run_id` filesystem-safety regex enforced; `workspace_id` prefix treated as opaque by consumers (negative test); crash scenarios produce the `bare-worktree-no-lease` and `sidecar-without-lease` evidence types per WM-003a and route to reconciliation Cat 3.
- **WM-005 — WM-009, WM-005a, WM-006a (branch naming).** Branch-naming unit tests: task-branch and integration-branch templates produce expected names; parent-bead-derived integration branch survives `git check-ref-format` validation; pathological bead-ID inputs (`@{`, `//`, sole `@`, leading `.`, trailing `.lock`, control chars) exercise the canonical hex-encode fallback and re-validate; small-scope-collapse is operator-policy-gated (default `integration`, override `main`); sub-workflow expansion does not create additional task branches.
- **WM-010 — WM-013e (lease model).** Multi-agent-sequential scenario tests: three agents run in sequence in one worktree; lease-lock file is written exactly once per lease with atomic temp+rename+fsync+parent-dir-fsync; lock JSON content matches WM-013a; lock is released on terminal transitions (WM-013b) with lock absence AND workspace-local `lease_released` marker as release proofs; per-terminal-path release gates exercised (merged, run_failed, post_escalation, verdict_driven); filesystem discovery matches canonical path (WM-013c); canonical-path re-use is rejected (WM-013d); `.gitignore` hygiene enforced at startup with `GitignoreWriteForbidden` surfaced if daemon lacks write permission (WM-013e).
- **WM-014 — WM-016 (lifecycle + emission ordering).** Event-emission unit tests: every state transition in §7.1 emits the expected event with EV-compatible payload fields; `workspace_leased` emitted only after worktree + branch + first-session sidecar (atomic write) + lease-lock fsync + parent-dir fsync; `workspace_merge_status` paired-phase single-event test verifies both `status=pending` and `status=merged` emissions.
- **WM-018 — WM-021, WM-018a, WM-019a (merge-back).** Merge-back scenario tests: one-commit-per-task on integration branch; `--strategy=ort` pinning verified; synthesized commit message carries `Harmonik-Run-ID` + conditional `Harmonik-Bead-ID` trailers with author = LaunchSpec identity (agentic variant) or daemon identity (non-agentic variant) and committer = daemon identity; scratch merge-worktree lifecycle (create-use-remove) validated via WM-019a option (b); conflict detection from non-zero `git merge --squash` exit plus porcelain status works.
- **WM-022 — WM-024, WM-022a (conflict resolution).** Cognition-tagged integration tests with twin implementer: synthetic merge conflict triggers implementer re-dispatch using `implementer_handler_ref` derived from sidecar walk (NOT from trailer walk; confirm absence of `Harmonik-Actor-Role` dependency); three-attempt default cap exercised with escalation on cap-reach; operator-configured cap in [1, 10] tested and out-of-range values rejected at startup; unresolvable case emits `merge_conflict_escalation`; all-mechanical task branch skips re-dispatch and escalates directly per WM-022a; handler-class retirement case routes to escalation (WM-024 terminal clause).
- **WM-025 — WM-030 (session-log).** Filesystem tests: metadata sidecar exists before `workspace_leased` for the first session; sidecar atomic write (temp+rename+parent-dir-fsync) verified; `.tmp-<pid>` orphan cleanup by startup sweep; subsequent session sidecars do not re-emit `workspace_leased`; namespaced `node_id` tested inside an expanded sub-workflow; `.gitignore` excluding `.harmonik/sessions/` is detected as a misconfiguration; CASS (S08) reads sidecar + log; post-merge retention default verified.
- **WM-031 — WM-033 (failed-run persistence).** Daemon-restart scenario tests: failed-run worktrees persist across restart; lease-lock file released on terminal failure per WM-013b with marker written before unlink; orphan sweep uses content-first staleness (mtime tiebreaker only) per WM-033; `git worktree prune` invoked after sweep; operator-issued `git worktree lock` respected (prune skips locked entries).
- **WM-034 — WM-036 (re-run rule).** Verdict-driven scenario tests: `reopen-bead` creates fresh worktree at `run/<new_run_id>` with a freshly minted `run_id`; reuse of a prior `run_id` is rejected; `reset-to-checkpoint` / `resume-here` / `resume-with-context` keep worktree; `accept-close-with-note` and `escalate-to-human` produce no re-run; unknown-verdict input produces a classification error.
- **WM-037 — WM-040, WM-037a, WM-038a (interrupt-state).** Operator-pause and crash-recovery scenario tests: `interrupt_state` composes with lifecycle state in the in-flight set; `workspace_interrupted` emission comes from the reconciliation detector only (crash path); operator- and verdict-driven transitions write workspace-local `interrupt_state_changed` markers per WM-038a and are consumed by reconciliation on next sweep; interrupt signals against terminal workspaces are silently rejected per WM-037a; clearing requires operator resume or reconciliation verdict; non-matching clearance attempts are detected by reconciliation's transition-audit pass.
- **WM-ENV-001, WM-ENV-002 (envelope).** Envelope audit test: every entry in (a)–(i) is present or explicitly "none"; cross-spec citations resolve; events produced/consumed match §6.5 and §4. Git-version detection at startup tested with synthetic old-git (refuses start with `GitVersionTooOld`) and current-git (starts cleanly).
- **WM-INV-003 (invariant auditor).** Part A enforced by EM-024a branch-tip monotonicity runtime check. Part B scoped as a future test-tool obligation (OQ-WM-017); prose-level test: synthetic `git filter-branch` on a task branch during a live run MUST be detected by the invariant-auditor tool once `testing.md` lands.

Migration to `[testing.md §<layer>]` cross-references occurs within one revision cycle once testing.md lands; this obligation is tracked in OQ-WM-001.

### 10.3 Excluded conformance claims

- This spec does NOT grant conformance over: handler session-log FORMAT (per-handler; owned by [handler-contract.md §4.1]); CASS indexing (owned by the deferred memory-layer spec); event type names and payload shapes (owned by [event-model.md §8.5]); operator-CLI surface for `harmonik workspace` subcommands (deferred).
- This spec does NOT guarantee performance or throughput bounds on `git worktree add`; those are operator-observable per [operator-nfr.md §4.9]. A slow `git worktree add` produces a long `created` state during which reconciliation's startup detector (per [reconciliation/spec.md §4.3]) may transiently observe a workspace not yet in `ready`; this is an acceptable race because `workspace_created` emission precedes the detector pass. Repositories using git-LFS MAY experience unusually long `git worktree add` latency under mandatory smudge filters; this is not within WM's control surface (per §2.2 and OQ-WM-018).

## 11. Open questions

#### OQ-WM-001 — Migrate test-obligation prose to testing.md references

Question: §10.2 currently names test obligations in prose. The template §10.2 expects cross-references to `[testing.md §<layer>]` once testing.md lands.
Owner: foundation-author
Blocks: none (MVH prose obligations are in place)
Default-if-unresolved: Keep prose obligations; migrate within one revision cycle after testing.md is finalized.

#### OQ-WM-002 — Operator configurability of integration-branch template

Question: §4.2.WM-006 declares a default integration-branch name derived from parent-bead ID (`harmonik/integration/<parent_bead_id>`) with the template operator-configurable. The precedence layer for the template (per [control-points.md §4.7 CP-037]) and its change-takes-effect semantics are not yet fully specified — including ref-safe bead-ID substitution per WM-006a. §4.2.WM-008's operator-policy-gated small-scope-collapse merge-target knob needs the same precedence layer.
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

#### OQ-WM-005 — Lock-file canonical path alignment across WM / HC / PL [BLOCKING-CROSS-SPEC]

**Status at v0.4.0: elevated from DEFERRED to BLOCKING-CROSS-SPEC.** This is a blocker for HC and PL (not for WM); WM's canonical path AND JSON content format are authoritative for the corpus. HC and PL MUST align by their next revision cycle.

Question: WM-013a declares `${workspace_path}/.harmonik/lease.lock` with a structured JSON content schema; [handler-contract.md §4.10 HC-044a] names `.harmonik/worktrees/<run_id>/.lock` (equivalent to `${workspace_path}/.lock`) with PID-plus-liveness-probe content; [process-lifecycle.md §4.2 PL-006] cites `.harmonik/lease.lock` and uses mtime for staleness.
Owner: foundation-author; bilateral coordination with handler-contract and process-lifecycle authors. HC and PL authors carry the alignment burden for the next cycle.
Blocks: HC-044a's fail-fast path, PL-006's sweep, cross-subsystem lock adoption. Does NOT block WM conformance — WM ships its canonical path at v0.4.0.
Default-if-unresolved: WM retains `${workspace_path}/.harmonik/lease.lock` with the JSON content of §4.3.WM-013a. HC and PL adopt WM's path and content format in their next revision. Implementations encountering a legacy-format lock file on disk from a prior generation MUST treat it as a migration artifact and ignore it (or sweep it as stale after applying content-first staleness detection per WM-033). WM does NOT write a second compat-path lock file.

#### OQ-WM-006 — workspace_interrupted emitter identity and payload coverage for operator-driven interrupts

Question: [event-model.md §8.5.5] names the reconciliation detector as the sole emitter of `workspace_interrupted` and declares a payload oriented to Cat 6 crash-detection (`workspace_id`, `run_id`, `detected_at`, `category`). Operator-driven interrupts (pause, stop) transition `interrupt_state` via WM-038 but are not Cat 6; either (a) EV's row is narrow and operator-driven interrupts should route through a different event (perhaps `operator_pausing` / `operator_stopping` alone), or (b) EV's payload and emitter list should be widened to cover operator paths.
Owner: foundation-author; bilateral coordination with event-model author.
Blocks: none (WM currently cedes to EV and does NOT emit `workspace_interrupted`; the record-level `interrupt_state` field is independent).
Default-if-unresolved: WM does not emit `workspace_interrupted` for any path. Operator-driven interrupts surface via the operator-control events of [event-model.md §8.7]; the workspace manager updates `interrupt_state` silently per WM-038 without a dedicated wire event, and the reconciliation detector emits `workspace_interrupted` only for crash paths. Revisit in EV round-2 or in an operator-nfr integration cycle.

#### OQ-WM-007 — Conflict-resolution skill name and provisioning

Question: §4.6.WM-024 requires the implementer re-dispatch LaunchSpec to include a conflict-resolution skill in `required_skills`, but no such skill name is registered in [handler-contract.md §4.11]. The skill's name, on-disk shape, and provisioning path are unspecified.
Owner: foundation-author; bilateral coordination with handler-contract.
Blocks: implementer re-dispatch implementation (WM-024) without a fallback prompt-only dispatch.
Default-if-unresolved: re-dispatch proceeds without a special conflict-resolution skill; the handler relies on the LaunchSpec input payload's conflict markers + transition history to resolve. This degrades the cognitive surface but does not block MVH conformance. Revisit when handler-contract declares its skill-registry shape (currently OQ-HC-007).

#### OQ-WM-008 — Failed-run retention window defaults

Question: §4.8.WM-031 preserves failed-run worktrees on disk without a retention window. A daemon running weeks against a repo with many failed runs can exhaust disk. §A.3 rationale cites operator-initiated cleanup workflows, but those are post-MVH.
Owner: foundation-author; bilateral coordination with operator-nfr.
Blocks: none for MVH correctness; blocks operator experience at scale.
Default-if-unresolved: MVH preserves failed-run worktrees indefinitely; disk-pressure mitigation is operator-initiated (manual `git worktree remove` followed by branch deletion). Revisit post-MVH with a configurable retention window (e.g., N runs, M days, size cap, GC cadence) in operator-nfr.

#### OQ-WM-009 — worktree_disposition bilateral attribute on reconciliation Verdict

Question: §4.9.WM-036 maps the six-value verdict enum to three workspace dispositions (`fresh`, `keep`, `none`) — but this mapping is owned by WM, not reconciliation. If reconciliation adds a new verdict post-MVH, the mapping must be updated in WM, which is a bilateral edit. A cleaner shape would be for [reconciliation/schemas.md §6.1 Verdict] to declare a `worktree_disposition` attribute on each variant, and for §4.9.WM-036 to route on that attribute mechanically.
Owner: foundation-author; bilateral coordination with reconciliation author.
Blocks: none for MVH (the six-value enum is stable); blocks graceful extensibility.
Default-if-unresolved: Keep the mapping table in §4.9.WM-036 for MVH; any future verdict addition requires a WM amendment. Adopt the attribute form in reconciliation's next revision cycle if verdict additions materialize.

#### OQ-WM-010 — Post-MVH cleanup workflow home

Question: §4.8.WM-031 cedes retention and cleanup to operator-initiated workflows. No current spec owns the cleanup-workflow surface. The legacy components.md §5.6 bootstrap placeholder for cleanup is retained temporarily; a proper owner is needed.
Owner: foundation-author.
Blocks: none for MVH.
Default-if-unresolved: Cleanup workflow definition lives post-MVH in either operator-nfr (as an operator-control extension) or in a new workflows library spec. Bootstrap citation remains at `[components.md §5.6]` until a home is named.

#### OQ-WM-011 — Citation migrations pending downstream reviews

Question: v0.3.0 migrated all WM body `[docs/foundation/components.md §N]` citations to owning-spec `§4.x` form. However, reverse inbound citations (other specs citing WM at legacy `§5.x` anchors) persist across five reviewed and four drafted peers (48 broken inbound cites per the round-1 cross-spec-architect review). The §A.4 migration map is published here for downstream use; migration in each spec's next revision cycle is required.
Owner: corpus-wide; per-spec owner applies the migration during their next integration.
Blocks: lint cleanliness of the corpus.
Default-if-unresolved: The legacy `§5.x` citations resolve by applying §A.4's mapping table. Reviewers SHOULD flag non-migrated inbound citations during each spec's next revision.

#### OQ-WM-012 — Enrich EM trailer set with `Harmonik-Agent-Type`

Question: §4.6.WM-022 uses a sidecar-file walk to identify the most-recent agentic session; this requires filesystem I/O and cannot operate on a bare git repository (e.g., an analysis tool running against a push-only clone). A cleaner shape would be for [execution-model.md §4.4 EM-017] to add `Harmonik-Agent-Type: <agent_type>` to the standard trailer set so that a `git log --format '%(trailers)'` walk suffices for implementer identification, without touching `.harmonik/sessions/`. This is a foundation-amendment request for EV/EM coordination, not a blocker.
Owner: foundation-author; bilateral with execution-model author.
Blocks: none (WM v0.4.0 resolves via sidecar walk).
Default-if-unresolved: Sidecar walk remains the authoritative mechanism. If EM adds the trailer later, WM MAY prefer the trailer walk as the faster path and treat the sidecar walk as the tiebreaker.

#### OQ-WM-013 — EV reclassify `workspace_discarded` from O to F

Question: EV classifies `workspace_discarded` as class O (ordinary, not fsynced). WM-013b's release gate for the failed-run and post-escalation paths therefore cannot depend on `workspace_discarded` alone; WM routes around this by gating on `run_failed` (class F) or by writing a workspace-local JSONL marker. Reclassifying `workspace_discarded` to class F would simplify WM-013b by making the release gate uniformly "`workspace_discarded` fsynced," removing the run_failed coupling and the workspace-local marker.
Owner: foundation-author; bilateral with event-model author.
Blocks: none (WM v0.4.0 resolves locally).
Default-if-unresolved: WM retains the per-terminal-path release-gate rules of WM-013b; the workspace-local marker serves as the durable signal. Revisit in EV's next revision cycle.

#### OQ-WM-014 — `.gitignore` write-vs-fail operator posture

Question: §4.3.WM-013e requires the daemon to ensure `.gitignore` covers harmonik control-plane paths and to fail-fast with `GitignoreWriteForbidden` if it lacks write permission. Operators with strict repository-hygiene policies may object to the daemon writing to `.gitignore` at all. The alternative posture — skip the write, surface a warning, and let the operator add the entries manually — needs an operator-nfr policy knob.
Owner: foundation-author; bilateral with operator-nfr.
Blocks: none for MVH (fail-fast is the safe default).
Default-if-unresolved: Fail-fast at daemon startup when `.gitignore` is missing entries and the daemon lacks write permission. Operators MAY pre-populate `.gitignore` with the required entries to bypass the daemon write.

#### OQ-WM-015 — Cross-subsystem git-version pin coordination with PL

Question: §4.a WM-ENV-002 pins git ≥ 2.34 for WM's operations (`--strategy=ort`, trailer format, `worktree repair`). [process-lifecycle.md] may pin a different minimum (e.g., for its own git interaction or for ntm-version coordination per PL-021). A unified corpus-wide pin avoids the "WM-compatible git but PL-incompatible" trap.
Owner: foundation-author; bilateral with process-lifecycle.
Blocks: none for MVH.
Default-if-unresolved: WM ships 2.34 as its pin. Any stricter pin from PL raises the effective corpus floor; any looser pin from PL is overridden by WM's floor when the workspace-manager runs. Surface a unified pin in operator-nfr's environment-requirements table when both specs are reviewed.

#### OQ-WM-016 — WM as valid emitter for operator-/verdict-driven interrupt wire events

Question: §4.10.WM-038a ships a workspace-local JSONL marker for operator- and verdict-driven `interrupt_state` mutations because [event-model.md §8.5.5] currently names the reconciliation detector as sole emitter of `workspace_interrupted` with a Cat-6 crash-oriented payload. The local marker resolves the durability gap but leaves the bus silent for operator paths. Two resolution shapes: (a) EV promotes WM to a valid emitter of `workspace_interrupted` and widens the payload to cover operator/verdict causes; (b) EV introduces a new event type `workspace_interrupt_state_changed` owned by WM with a payload `{prior_interrupt_state, new_interrupt_state, cause}`.
Owner: foundation-author; bilateral with event-model author.
Blocks: none for MVH (local marker suffices).
Default-if-unresolved: WM writes only the local marker; bus consumers receive signal on next reconciliation sweep. Revisit in EV's next revision cycle; WM has a slight preference for (b) because it separates Cat-6 crash detection from operator-control transitions.

#### OQ-WM-017 — Invariant-auditor tool scope and owner

Question: §5.WM-INV-003 Part B names a conformance-time invariant-auditor tool that walks `git reflog` for history-editing operations on task branches. The tool has no home; it cannot live in runtime WM (reflog may be pruned) and cannot live in a missing `testing.md`. Once `testing.md` exists the tool's home is there; until then, the obligation is a named-but-unscoped requirement.
Owner: foundation-author; coordinate with whoever authors `testing.md`.
Blocks: WM-INV-003 Part B automated enforcement; Part A is in place via EM-024a.
Default-if-unresolved: Part A is the enforced sensor; Part B's rule remains a conformance obligation enforced by reviewer discipline during test-author cycles.

#### OQ-WM-018 — Post-MVH LFS, submodules, and bare-repo support

Question: §2.2 declares LFS-backed blobs, git submodules, and bare-repository backing layouts out-of-scope for MVH. Post-MVH, each has implementation implications: LFS smudge/clean may elongate `created` state; submodule init requires a cross-subsystem provisioning step (possibly via adze); bare-repo support requires rethinking the `repository` field's meaning and the `git worktree add` path.
Owner: foundation-author.
Blocks: none for MVH; scopes post-MVH planning.
Default-if-unresolved: Out-of-scope at MVH. Post-MVH support is an additive extension with its own spec surface or amendments here.

## 12. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-04-23 | 0.1.0 | foundation-author | Initial draft. Requirements-first shape; 40 requirements, 5 invariants, 4 open questions. Bootstrap citations into docs/foundation/components.md for specs not yet finalized. |
| 2026-04-24 | 0.2.0 | foundation-author | Corpus-wide cleanup pass (no semantic changes). Migrated legacy architecture.md citation anchors to the §4.N map per the v0.2 NOTE (the citations had been carried as `docs/foundation/components.md §1.N` bootstrap references to content now owned by the reviewed architecture.md): §1.1→architecture.md §4.1 (×1 in §9 Depends on), §1.8→architecture.md §4.9 (×3 in §4.3 lease-model lead-in, §5 WM-INV-001, §9 Depends on) plus §A.3 Rationale footer (×1). Completed AR-MIG-001 `handler_type` → `agent_type` rename at §4.7.WM-026 (metadata-sidecar field list), §6.1 SessionMetadataSidecar RECORD, and §7.2 stamp_session_metadata protocol pseudocode. No requirement IDs, invariants, or schemas were touched. |
| 2026-04-24 | 0.4.0 | foundation-author | Round-2 reviewer integration (skeptic + crash-adversary + git-expert). **Status transition: `draft` → `reviewed`**; all WM-NNN, WM-INV-NNN, and OQ-WM-NNN IDs FROZEN permanently at this revision. **BLOCKING fixes (B1–B10):** **B1** — retired fabricated `Harmonik-Actor-Role` trailer reference in WM-022 (trailer does not exist in [execution-model.md §4.4 EM-017] trailer set); rewrote WM-022 to walk `${workspace_path}/.harmonik/sessions/*/harmonik.meta.json` in reverse-chronological order by `launched_at`, selecting the first agentic `agent_type`; sidecar is authoritative; filed OQ-WM-012 tracking a future EM trailer enrichment (`Harmonik-Agent-Type`) as cross-spec enhancement. **B2** — lock-path triangle resolved in WM's favor: affirmed WM-013a's JSON-content schema + fsync discipline; amended WM-033 to prefer JSON content (pid + created_at + liveness probe) over filesystem mtime for staleness (mtime demoted to tiebreaker); OQ-WM-005 elevated from DEFERRED to **BLOCKING-CROSS-SPEC** — HC and PL MUST align to WM's canonical path + content format in their next revision. WM does NOT write a compat-path lock file. **B3** — added WM-003a classifying partially-crashed worktree states: `bare-worktree-no-lease` (registered worktree, no lock, no sessions) and `sidecar-without-lease` (registered worktree, sidecar present, no lock) both route to reconciliation Cat 3 per [reconciliation/spec.md §8]; closes gap where SIGKILL between `git worktree add` and lease-lock fsync left an unclassifiable orphan. **B4** — amended WM-026 to require the sidecar be written with the same temp+rename+fsync atomic discipline as WM-013a's lease-lock, plus parent-directory fsync after rename (required for durability on filesystems that do not fsync directory entries as part of file fsync). **B5** — rewrote WM-013b's release gate to be per-terminal-path: merged path gates on `workspace_merge_status` fsync (EV class F), failed-run path gates on `run_failed` fsync (EV class F, not the O-class `workspace_discarded`), post-escalation path gates on a workspace-local JSONL marker written to `${workspace_path}/.harmonik/events/workspace-<workspace_id>.jsonl`, verdict-driven path gates on `reconciliation_verdict_executed` + local marker. Introduces workspace-scoped durability file (owned by WM; gitignored per WM-013e). Filed OQ-WM-013 tracking a future EV reclassification of `workspace_discarded` from O to F. **B6** — fixed broken `git worktree add` command in WM-003 and §7.2 pseudocode: changed from `git worktree add <path> <branch>` (which fails if `<branch>` does not exist) to `git worktree add -b <branch> <path> <parent_commit>` (atomically creates the branch with an explicit start-point). **B7** — replaced WM-006a's prose character-class enumeration with a delegation to `git check-ref-format(1)`: after constructing the proposed ref, WM invokes `git check-ref-format refs/heads/<proposed>`; on failure, applies a canonical fallback (hex-encode non-`[a-zA-Z0-9/_-]` bytes, collapse `//`, reject sole `@` / empty / single `.`) and re-validates. Closes skeptic's enumeration-incomplete concern (prior prose missed `@{`, `//`, sole `@`, leading/trailing `.`, per-component `.lock`, case collisions, control chars). **B8** — pinned squash-merge semantics in WM-019/WM-020: `git merge --squash --strategy=ort <task_branch>` (ort is default in git ≥ 2.34 but explicit to prevent drift); commit message synthesizes summary of checkpoint-commit subjects and carries `Harmonik-Run-ID` + conditional `Harmonik-Bead-ID` trailers; author = LaunchSpec identity (agentic variant) or daemon identity (non-agentic variant), committer = daemon identity; rewrote WM-020's "not fast-forward-only" phrasing as "squash is non-ff by construction" (the integration tip advances to a new commit containing squashed content; task-branch tip unchanged). **B9** — added WM-013e requiring daemon startup to ensure `.gitignore` covers `.harmonik/lease.lock`, `.harmonik/sessions/`, `.harmonik/worktrees/`, `.harmonik/events/`; fail-fast with `GitignoreWriteForbidden` if the daemon lacks write permission; operator posture (write-vs-fail override) tracked in OQ-WM-014. **B10** — added WM-ENV-002 pinning minimum git version 2.34 (for `--strategy=ort` default, `%(trailers:key=X,valueonly=true)`, `worktree repair`); detected at daemon startup with `GitVersionTooOld` fail-fast; cross-subsystem version coordination with PL tracked as OQ-WM-015. **IMPORTANT integrations (I1–I11):** **I1** — added WM-038a requiring workspace-local JSONL marker for every `interrupt_state` mutation (operator-driven, verdict-driven) written to `${workspace_path}/.harmonik/events/workspace-<workspace_id>.jsonl`; resolves EV-WM emitter-identity gap locally without blocking on EV; bus-event promotion (WM as `workspace_interrupted` emitter, OR new `workspace_interrupt_state_changed` event type) tracked as OQ-WM-016. **I2** — rewrote WM-INV-003 sensor as two-part: Part A (EM-024a branch-tip monotonicity, catches amend/rebase/force-push) and Part B (invariant-auditor tool walks `git reflog` for `filter-branch` / `replace` / non-ff updates on live task branches); Part B scoped as a conformance-time test obligation until `testing.md` lands (tracked as OQ-WM-017); stated explicitly that WM does NOT rely on `git reflog` for state recovery (reflog dependence confined to the auditor tool). **I3** — added WM-019a specifying merge execution happens either with `--ignore-other-worktrees` OR in a dedicated scratch merge-worktree at `<repo>/.harmonik/worktrees/merge-<merge_id>/` (preferred); scratch worktree is not leased, not classified as orphan, removed after merge success. **I4** — amended WM-033 to invoke `git worktree prune` after WM's own cleanup of canonical paths (drops `.git/worktrees/<name>/` metadata for removed paths); noted that operator-issued `git worktree lock` is respected (prune skips locked entries; harmonik never issues the lock itself). **I5** — added glossary entry `worktree-lock disjoint`: harmonik's lease-lock (harmonik-layer) is disjoint from git's `git worktree lock` (operator-layer); harmonik never issues the latter; operator locking is respected by the §4.8.WM-033 sweep. Added glossary entries for `scratch merge-worktree` and `orphan evidence types`. **I6** — no git-trailer walk needed in WM-022 after B1 (sidecar walk replaces `--first-parent` trailer walk). **I7** — added explicit re-dispatch cap to WM-024: default 3 conflict-resolution attempts, with operator override in range [1, 10]; after cap is reached, WM-023 escalates to `escalate-to-human` per [reconciliation/spec.md §4.5 RC-020]. Added §A.3 rationale on "why most-recent agentic" (recency-approximates-responsibility) and "why budget=3" (heuristic based on marginal success rate). **I8** — declared LFS, submodules, bare-repo out-of-scope for MVH in §2.2 with explicit rationale; post-MVH support tracked as OQ-WM-018. **I9** — stated WM does NOT rely on `git reflog` for recovery (reflog is auditor-tool-only per Part B of WM-INV-003). **I10** — §A.3 rationale updates: "most-recent agentic wins" justification; honest documentation of OQ-WM-005 resolution path (WM-authoritative); justification for conflict-budget default of 3. **I11** — split-or-not decision: spec grew from 999 to ~1260 lines with integration; kept single-file per template §Multi-file split guidance (threshold ~1200 lines considered; splitting would cascade ~60+ cross-references, not worth the coupling cost). Single-file at v0.4.0 is sustainable; revisit if v0.5.0+ pushes past 1400 lines. **New requirements (net):** WM-003a, WM-013e, WM-019a, WM-038a, WM-ENV-002 (5 new). **New OQs:** OQ-WM-012 (EM trailer enrichment), OQ-WM-013 (EV reclassify workspace_discarded to F), OQ-WM-014 (gitignore write-vs-fail operator posture), OQ-WM-015 (cross-subsystem git-version pin with PL), OQ-WM-016 (WM as emitter for interrupt_state wire events with EV), OQ-WM-017 (invariant-auditor tool home pending testing.md), OQ-WM-018 (post-MVH LFS / submodules / bare-repo). **Elevated:** OQ-WM-005 from DEFERRED to BLOCKING-CROSS-SPEC (HC and PL must align to WM by their next revision). **New error classes in §8:** `RefNameInvalid`, `BareWorktreeNoLease`, `SidecarWithoutLease`, `GitignoreWriteForbidden`, `GitVersionTooOld`. **Schema touches:** §6.2 path table gained rows for workspace-local events JSONL, sidecar tempfile, scratch merge-worktree directory; §6.5 event table gained note on workspace-local durability markers (not bus events). **§7.2 pseudocode** updated throughout: `-b` flag on `worktree add`, atomic write-json-fsync helper (temp+rename+parent-dir fsync), per-terminal-path release gates, `write_workspace_local_marker` helper, `set_interrupt_state` helper. **IDs preserved throughout; no renumbering; retired IDs (WM-017, WM-INV-004) remain retired and MUST NOT be reused at any future revision.** **Deferred cross-spec items** left to next integration cycles of owning specs: EV's paired-phase emitter identity and O-vs-F classification (OQ-WM-013, OQ-WM-016); HC + PL lock-path alignment (OQ-WM-005 BLOCKING-CROSS-SPEC); EM trailer set enrichment (OQ-WM-012); operator-nfr gitignore posture (OQ-WM-014); PL git-version pin coordination (OQ-WM-015); testing.md invariant-auditor home (OQ-WM-017). |
| 2026-04-24 | 0.3.0 | foundation-author | Round-1 reviewer integration (implementer + cross-spec-architect + critic). Status remains `draft` (R2 will be the transition to `reviewed`). **Front matter:** added `spec-category: runtime-subsystem` per AR-052; expanded `depends-on` from `[execution-model]` to `[architecture, execution-model, handler-contract, control-points, reconciliation, operator-nfr, process-lifecycle, beads-integration]` per the cross-spec-architect's dependency-graph analysis; `event-model` stays in §9.3 co-references per the EM/EV cycle-avoidance precedent (WM ↔ EV is a consume-produce pair resolved directionally). **New §4.a Subsystem envelope with WM-ENV-001** declaring (a)–(h) envelope elements per AR-053. **State-machine coherence fix (critic C1, C9, implementer §7.2 trace).** Retired the `setup` lifecycle state (never reachable in prior §7.2 pseudocode, contradicted by WM-016 ordering); collapsed into `created → ready → leased`; rewrote §7.1 table and §7.2 pseudocode to agree with WM-016's four-step emission gate (worktree → branch → first-session sidecar → lease-lock → `workspace_leased`). **Lease-lifetime mechanics (critic C3, implementer SP-2, SP-5).** New WM-013a (lease-lock canonical path, content JSON schema `{run_id, pid, created_at, ttl_sec}`, fsync discipline, birth at `leased`); WM-013b (release on entering `merged`/`discarded`); WM-013c (filesystem discovery mechanism); WM-013d (released-path re-use forbidden). **Event-ownership alignment with event-model (implementer SP-1, architect §3.4/§3.5, critic C6).** Rewrote WM-015 and §6.5 (renamed from v0.2 §6.3 per template slot) to cede all event type names and payload shapes to EV per EV-025. `workspace_merge_pending` + `workspace_merged` references replaced by the single paired-phase `workspace_merge_status` with `status ∈ {pending, merged}` per [event-model.md §8.9(h)]; WM-021 rewrites accordingly. `workspace_interrupted` emitter identity ceded to reconciliation detector per EV §8.5.5 — WM-039 now names WM as FIELD owner (not EVENT emitter), with cross-spec mismatch for operator-driven interrupts flagged in new OQ-WM-006. Retired WM-017 (payload-field declarations violated EV-025); ID NOT reusable. **Original-implementer mechanical definition (critic C2, implementer SP-7).** Added `implementer_handler_ref` field to Workspace record (§6.1); rewrote WM-022 to identify the implementer mechanically by walking task-branch trailers in reverse from merge-pending tip; added WM-022a (all-mechanical task branch escalates directly); tightened WM-024 to name `handler_ref`, LaunchSpec construction, budget policy, and handler-class-retirement fallback. **Merge-back node dispatch (implementer SP-6).** New WM-018a declaring the merge node's positive shape (non-agentic direct dispatch OR agentic node via handler-contract); conflict detection rule made mechanical (exit code + porcelain). **Run-id minting on reopen-bead (implementer SP-3).** WM-034 amended to state that a reopen-bead verdict MUST produce a new run_id; added an explicit anti-reuse rule (WM-013d). **Verdict-enum classification (implementer SP-4, critic C4).** WM-036 rewritten against the authoritative six-value [reconciliation/schemas.md §6.1 Verdict]; removed non-existent `abandon`; added `accept-close-with-note`; malformed-verdict handling cites RC-023. Bilateral-attribute proposal tracked as OQ-WM-009. **Interrupt-orthogonality constraint (critic C5).** New WM-037a pins terminal states to `interrupt_state = none`; §7.1 "any" row narrowed to the in-flight state set; §4.10.WM-038 strengthens WM as SOLE writer. **WM-008 MUST/MAY contradiction (critic C8).** Rewritten as operator-policy-gated (default `integration`); WM-006 / WM-008 now agree. **§5 invariant rewrites (implementer SP-9, critic invariant audit).** Three v0.2 entries (WM-INV-001, WM-INV-002, WM-INV-005) reworded as cross-subsystem claims with named AR-042 sensors; WM-INV-003 gets EM-024a monotonicity-check sensor; WM-INV-004 retired as a duplicate of §4.6.WM-022 (ID NOT reusable). **Schema additions.** `LeaseLockFile` RECORD; `implementer_handler_ref` field on `Workspace`; typed aliases (`CommitSHA`, `BeadID`, `HandlerRef`) cited from owning specs; `metadata` map fixed as closed shape (keys `created_at`, `operator_fingerprint`); integration-branch row added to §6.2 path table with explicit "branch, not worktree" note. **New §8 Error and failure taxonomy** (seven classes; optional per template, load-bearing for filesystem+git mutations per implementer §8 audit). **Citation migrations (architect §2).** 32 sites migrated from `[docs/foundation/components.md §N]` bootstrap form to owning-spec form (event-model §8.5 / §8.3.7 / §8.7 / §8.9(h); handler-contract §4.1 / §4.2 HC-010 / §4.10 HC-044a / §4.11 / §6.1; reconciliation §4.3 / §4.5 RC-020 / §8.11, reconciliation/schemas.md §6.1 / §6.2; operator-nfr §4.3 / §4.5 / §4.9; process-lifecycle §4.2 PL-006; beads-integration §4.5 BI-014 / §4.6 BI-017, BI-018; control-points §4.7 CP-037; architecture §4.0 / §4.7). Fixed one broken current-form cite at line 109 (§4.4.EM-023 → §4.5 EM-023). Remaining legitimate bootstrap: core-scope.md §3 (no owning spec target); components.md §5.6 cleanup-workflow (OQ-WM-010). **Reverse-drift §A.4 migration map** added: publishes canonical `§5.x legacy → §4.x current` anchor mapping for downstream specs (EM, EV, HC, BI, ON, PL, RC, AR) whose citations to WM at `§5.x` do not resolve in v0.2+. **New OQs:** OQ-WM-005 (lock-path alignment WM/HC/PL), OQ-WM-006 (workspace_interrupted emitter identity with EV), OQ-WM-007 (conflict-resolution skill name with HC), OQ-WM-008 (failed-run retention-window defaults with ON), OQ-WM-009 (bilateral `worktree_disposition` attribute with RC), OQ-WM-010 (post-MVH cleanup-workflow home), OQ-WM-011 (corpus-wide inbound citation migration tracking). **Requirements added (net):** WM-005a, WM-006a, WM-013a–d (4), WM-018a, WM-022a, WM-037a (9 new). **Retired:** WM-017 (payload-shape violation), WM-INV-004 (duplicate). **IDs preserved throughout; no WM-NNN renumbering.** Deferred to cross-spec cycles: EV's paired-phase / emitter-identity alignment for workspace_interrupted; HC/PL lock-path coordination (these remain cross-spec OQs). |
| 2026-04-24 | 0.4.1 | foundation-author | Corpus citation-drift cleanup pass 2: migrated legacy §N.N cross-spec anchors to current template §N.N form per the central remap table; 21 citations fixed, all of them `[reconciliation.md §N]` → `[reconciliation/spec.md §N]` path fixes (reconciliation is a multi-file spec; cites must use the explicit file path). Sites: §2.2 scope, §4.7 WM-024 re-dispatch cap, §4.9 WM-034/WM-036 verdict consumption, §4.10 WM-038 interrupt-state drivers, §4.10 WM-038a clearing rule + sensor, §4.10 interrupt-marker consumer, §5 WM-INV-001 sensor, §5 WM-INV-002 multi-run rejection, §5 WM-INV-003 branch-tip monotonicity, §5 WM-INV-005 workspace path reconstruction, §6.2 path table consumer annotations, §6.5 event marker consumer, §9.3 four cross-refs (RC §4.3, §4.5 RC-020, §8.11, §4.5), §10.3 conformance exclusion. No requirement IDs, invariants, or schemas touched. |
| 2026-05-13 | 0.4.4 | bridge-integration | **WM-002a added (hk-gql20.2).** New clause directly after WM-002 specifying the deterministic tmux window-name convention for agent processes launched under the PL-021b direct-tmux substrate. Pure function of `(bead_id, phase, iteration_count, project_hash, owns_session)`; single-mode → bare `bead_id`; review-loop → `bead_id/i<n>` or `bead_id/r<n>`; $TMUX-reuse mode prefixes `hk-<hash6>-` as the sweep sentinel for PL-021c. Truncation rule preserves suffix when name > 64 bytes via `<bead_id[:56]>~<hash[:8]><suffix>`. Cross-refs PL-021b, PL-006a, WM-002, BI-017. No existing WM IDs renumbered. Status remains `reviewed`. |
| 2026-04-25 | 0.4.2 | foundation-author | OQ-RC-011 resolution: extended §4.9.WM-036's verdict-disposition table from six rows to seven by adding the `no-op-accept` row introduced into the reconciliation Verdict enum at RC v0.3.0 (per [reconciliation/schemas.md §6.1 Verdict] and [reconciliation/schemas.md §6.2 Verdict-execution table]); the new row codifies the no-workspace-action disposition (record the verdict; clear any non-`none` `interrupt_state` per §4.10.WM-040 if reconciliation had previously marked the workspace interrupted; outer run continues per [reconciliation/spec.md §8.12]) consistent with RC's mechanical semantics that `no-op-accept` performs no mechanical action beyond `reconciliation_verdict_executed` emission and the verdict-executed commit. Updated WM-036 lead-in prose from "six-value verdict enum" to "seven-value verdict enum (per RC-020 as amended in RC v0.3.0)" and appended an OQ-RC-011 resolution citation to the trailing paragraph. No new WM IDs, no invariant changes, no schema changes; ID FREEZE preserved. Status remains `reviewed`. |
| 2026-05-12 | 0.4.3 | foundation-author | Add §4.7a Claude-code settings.json materialization (WM-040a, gap-filler after existing WM-040 to avoid collision with WM-038 / WM-039 / WM-040 interrupt-state requirements) covering atomic-write discipline, merge-with-existing semantics, and gitignore-hygiene extension. Overwrite-on-malformed logs to the session log; no new bus event is introduced (zero-new-event-types invariant of [claude-hook-bridge.md]). Companion to [claude-hook-bridge.md] new spec. No prior requirement IDs renumbered. Status remains `reviewed`. |
| 2026-05-13 | 0.4.5 | agent (hk-fdyip) | **WM-040b added: worktree auto-trust pre-seed (§4.7b).** For every claude-code workspace, the daemon MUST pre-seed `~/.claude.json` projects[worktreePath].hasTrustDialogAccepted=true before exec'ing Claude, so the interactive trust dialog is suppressed in daemon-spawned tmux panes. Ordering: after WM-003 + WM-040a, before SubstrateSpawn. Atomic temp+rename write; idempotent; preserves all other ~/.claude.json content. Failure → ErrStructural / trust_seed_failed / agent_failed (no exec on failure). Rejected alternatives documented: --permission-mode and --dangerously-skip-permissions are deny-listed in HC-055 / CHB-007; .claude/settings.local.json does not carry startup trust state. Companion: claude-hook-bridge.md §4.12 CHB-029. Code: internal/workspace/claudetrust_wm040b.go. No prior requirement IDs renumbered. Status remains `reviewed`. |

## A. Appendices

### A.3 Rationale

**Why lease-by-run and not lease-by-agent.** Agent sessions are ephemeral (minutes to hours); runs span the full arc of work on a bead. Leasing by agent would require re-acquisition handshakes every time a new agent takes over in the same worktree, and would introduce state-transfer overhead (committed files in an uncommitted-to-branch state, lease-handover events). Leasing by run lets the worktree be a passive surface that agents step into and out of; the run is the authority on when the lease ends. See [architecture.md §4.9 AR-INV-007] for the centralized-controller framing.

**Why the original implementer resolves merge conflicts.** A merge conflict requires the context of (a) the implementer's plan, (b) the implementer's understanding of the competing change, and (c) ongoing intent. A separate merge-agent would need to rebuild this context from session logs and commit messages, which is both expensive and drift-prone. The implementer's handler class already has tuned prompts and role context for the workflow. Re-dispatch launches a fresh session of that handler class (see WM-022 "original" = handler-class lineage, not process identity); escalation to human is reserved for the case where the re-dispatched implementer cannot resolve within its budget — the failure mode is already a reasoning failure; adding another reasoning layer between it and human review would be cruft. Resolved for MVH in [core-scope.md §3]. The MVH fallback for all-mechanical task branches (WM-022a) acknowledges that no implementer handler exists to re-dispatch, and routes directly to escalation.

**Why "most-recent agentic" wins for implementer identification.** The sidecar walk in §4.6.WM-022 selects the most recent agentic session's `agent_type` as `implementer_handler_ref`. The rule is a recency-approximates-responsibility heuristic: the last agent to place commits on the task branch is the one whose plan most-directly shaped the current merge-time state. Alternatives considered and rejected: (a) the FIRST agentic session (owner-of-origin) — drifts from current responsibility after later hand-offs; (b) the MAJORITY agentic session by commit count — sensitive to formatting or cleanup bursts that should not reassign responsibility; (c) a merge-specific responsibility field set explicitly by each session — adds a cross-session contract surface that no existing requirement carries. "Most recent" is the simplest rule that matches the intuition behind "whichever agent last touched the code should resolve its conflicts."

**Why a re-dispatch budget of 3 by default.** WM-024's three-attempt cap is heuristic: one attempt verifies the implementer's initial approach; a second attempt lets the implementer incorporate the feedback of the first failure (either a different path or a narrower scope); a third attempt is the final retry after observing two distinct failure modes. Beyond three, empirical experience in iterated-debugging workflows suggests the marginal success rate drops sharply — the LLM is more likely to loop on the same local optimum than to find a novel path. Three is therefore the point at which escalation to human review has better expected value than another re-dispatch. Operators tuning the cap upward (to 5–10) should observe the conflict-resolution JSONL markers for loop-detection and correspondingly widen escalation thresholds.

**Why the worktree path is canonical and not registry-driven.** A separate workspace registry (e.g., a SQLite table mapping run_id → path) would duplicate state that is already implicit in the filesystem. Daemon restart must rediscover workspaces anyway; a registry-backed path adds a source of drift without adding information. The canonical-path convention (WM-002) makes the workspace ID derivable from the run ID, which in turn is already recoverable from git per [execution-model.md §4.7]. One source of truth, no registry.

**Why interrupt-state is orthogonal to lifecycle state — in-flight only.** Interrupt events can occur at any IN-FLIGHT lifecycle state (a workspace in `ready` can be interrupted by operator stop just as one in `leased` can). Encoding interrupts as lifecycle states would multiply the state space (`leased` × `operator-paused`, `merge-pending` × `daemon-crash-suspected`, etc.). An orthogonal field keeps the lifecycle graph small and delegates interrupt-aware handling to the subsystems that own interrupts (operator-control, reconciliation). Terminal states are absorbing — interrupts received against `merged` or `discarded` are semantically incoherent ("pause a workspace that is already complete"), so WM-037a bounds the orthogonality to in-flight states and treats terminal-state interrupts as silent rejects. The lifecycle state answers "where is this workspace in its work"; `interrupt_state` answers "has something disrupted that work."

**Why sub-workflow expansion shares the parent's workspace.** [execution-model.md §4.8 EM-034] pins the parent `run_id` as the sole run identifier; sub-workflow nodes run inside the parent run's lease. Spawning a child workspace for a sub-workflow would re-introduce the child-run multiplicity EM-034 forbids and would violate the "one commit per task" integration-branch contract — checkpoints from nested nodes would lose their single-lineage shape. Keeping the lease parent-bound is structurally necessary, not a convenience. WM-005a names this explicitly.

**Why WM cedes event wire-format ownership to EV throughout.** Event-model.md's EV-025 rule declares that each event type has exactly one owning spec for payload shape. Prior versions of this spec enumerated payload fields inline — a scope leak that would require coordinated edits across two specs whenever a field was added or renamed. The v0.3.0 rewrite cedes all event type names and payload field lists to EV, leaving this spec normative only for WHEN events fire. The remaining scope anomaly — EV names the reconciliation detector as sole emitter of `workspace_interrupted` while operator-driven interrupts also transition `interrupt_state` — is tracked in OQ-WM-006 rather than silently resolved with an inline payload declaration.

**Why failed-run worktrees persist.** Post-mortem analysis, audit, and the improvement loop all need access to the state of the workspace at the moment of failure. Auto-deletion would destroy evidence. Disk pressure is a known operator concern and is handled by operator-initiated cleanup workflows rather than by the workspace manager's automatic behavior; MVH errs on the side of preservation. Post-MVH MAY introduce configurable retention windows; see OQ-WM-008.

### A.4 Reverse-drift migration map — §5.x legacy → §4.x current

This table is published to help downstream specs migrate their inbound citations. Five reviewed and four drafted peers currently cite WM at legacy `§5.x` anchors that do not exist in WM v0.2+ (the numbering derives from components.md's §Component 5 subsections; WM §5 is now Invariants). Each peer spec's next revision cycle SHOULD apply this mapping. The migration is tracked corpus-wide in OQ-WM-011.

**Migration posture (v0.4.0).** Downstream specs SHOULD migrate inbound citations to WM during their own next revision cycle. No hard MUST-by-when date is assigned at this revision; corpus-wide coordinated migration is a separate task post batch-2 review. Individual reviewers for each downstream spec's next cycle SHOULD flag unmigrated WM cites as a revision blocker for that specific spec (not for WM). WM v0.4.0's ID freeze (see front-matter NOTE) guarantees that migrating downstream specs need not re-migrate again as WM evolves.

| Legacy `§5.x` anchor (components.md §Component 5) | Current WM v0.3 anchor | Content |
|---|---|---|
| `§5.1 Lease rule` | `§4.3` (entire subsection) — OR `§4.1 WM-001` for the Workspace record itself | Lease-by-run rule and Workspace record shape |
| `§5.2 State machine` | `§4.4` plus `§7.1` state-machine table | Lifecycle states and transitions |
| `§5.3 Session-log aggregation` | `§4.7` | Session-log directory + metadata sidecar (S06 side) |
| `§5.3a Session-log pipeline` | `§4.7` (S06 side only); co-citation with `[handler-contract.md §4.2 HC-010]` for the S04 emission side | Three-spec pipeline co-ownership |
| `§5.4 Merge semantics` | `§4.5` | Merge-back to integration |
| `§5.5 Operator-control interaction` | `§4.10` | Interrupt-state projection |
| `§5.6 Cleanup` | `§4.8` (failed-run persistence); full retention policy is post-MVH per OQ-WM-008 / OQ-WM-010 | Retention / cleanup |
| `§5.7 Non-git artifacts` | `§2.2` out-of-scope bullet | Not covered normatively here |
| `§5.8 Branching model` | `§4.2` | Branch naming and three-level model |
| `§5.9 Re-run rule` | `§4.9` | Re-run vs intra-run classification |

Known inbound citation counts requiring migration (per round-1 cross-spec-architect audit, 2026-04-24): execution-model.md (12), reconciliation/spec.md (8), operator-nfr.md (6), process-lifecycle.md (5), handler-contract.md (5), beads-integration.md (5), event-model.md (4), reconciliation/schemas.md (2), architecture.md (1). Total ~48 inbound cites across 9 spec files. The `control-points.md` citations already use the current §4.2 form.
