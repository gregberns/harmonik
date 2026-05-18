# Workspace Model — Change Design (C6)

Scope: clarify (do not change) that the existing lease model already accommodates review-loop's sequential implementer/reviewer pattern. Add explicit naming of the `.harmonik/review.json` artifact path inside the worktree. No new state in the workspace state machine.

## 1. Current state

- `workspace-model.md §4.3 WM-010..WM-013e` (lines ~243–332) defines the lease model. **`WM-010`** (line ~243) — "Lease is held by the run, not by individual agents." Quote: "Multiple agents within the run (planner, researcher, builder, reviewer, merge agent) MUST occupy the same worktree sequentially across their nodes."
- **`WM-011`** (line ~249) — "One active agent at a time inside a workspace." Quote: "Agents run sequentially as the run traverses its workflow graph; parallel nodes within one run MUST NOT share a worktree."
- **`WM-013a`** (line ~267) — lease-lock file canonical path and content: `${workspace_path}/.harmonik/lease.lock` with `{run_id, pid, created_at, ttl_sec}`. Lock is tied to lease acquisition, NOT to workspace existence; one lease per run.
- `§4.4 WM-014..WM-016` (lines ~336–369) defines the workspace state machine (`created` → `ready` → `leased` → ... → `merged` | `discarded`). Lifecycle states are run-scoped.
- `§4.7 WM-025..WM-030` (line ~475+) defines the session-log directory layout `${workspace}/.harmonik/sessions/<session_id>/` and the metadata sidecar (`WM-026`). Multiple session directories per workspace is already valid.

## 2. Target state

### (a) Restate that review-loop fits within `WM-010` and `WM-011`

Amend `WM-011` (or add a non-normative example block under `WM-010..WM-011`): cite `review-loop` mode as a concrete instance of the sequential-agents-in-one-workspace rule. The implementer agent and the reviewer agent are sequential occupants of the same worktree: implementer exits or is paused → reviewer launches → reviewer exits → implementer resumes. Never concurrent. This naming closes a foreseeable read-confusion ("does review-loop need a new workspace primitive?" — no).

### (b) Restate that the lease spans the entire review-loop cycle

Amend `WM-013a` (or add a clarifying sentence in `WM-010`): in `review-loop` mode, **one lease covers the entire run**. The lease is acquired at `workspace_leased` (per `WM-016`) and released at the terminal workspace transition (`workspace_merge_status status=merged` or `workspace_discarded`) per `WM-013b`. Sessions come and go — multiple implementer launches (one per iteration) plus multiple reviewer launches all occur under the same lease — but the lease-lock file is NOT re-acquired or released per session. State this explicitly so a future reader does not infer per-session leases from the mechanics.

### (c) New artifact path: `.harmonik/review.json` inside the worktree

Add a new requirement, provisionally `WM-027a — Reviewer verdict artifact path`. The reviewer agent writes its verdict to the canonical path `${workspace_path}/.harmonik/review.json` inside the worktree. The file conforms to the existing `agent-reviewer` skill's JSON verdict schema v1 (per event-model design). Lifecycle:

- The reviewer writes the file as part of its agent session output.
- The daemon (or the orchestrator-core dispatch loop) reads the file after the reviewer's `agent_completed` event but before deciding the next phase.
- After the daemon emits `reviewer_verdict` (per event-model `§8.1.a3`), the file persists in the worktree for that iteration. On a subsequent iteration, `.harmonik/review.json` is overwritten per iteration; the prior iteration's verdict is archived to `.harmonik/review.iter-<N>.json` immediately before the next reviewer launch. The daemon writes the archive; the reviewer subprocess only writes the current `review.json`.
- The file is included in the workspace `.gitignore` hygiene set per `WM-013e` (cross-spec coordination request: add `.harmonik/review.json` to the standard ignore entries) so it does NOT enter checkpoint commits.

### (d) Session-log directory layout — observation only

Add an informational note under `§4.7 WM-030`: review-loop exercises the multiple-sessions-per-workspace path more aggressively than `single` mode — a 3-iteration cycle produces up to seven session directories per workspace (3 implementer + 3 reviewer + 1 initial). `WM-030`'s post-merge retention defaults apply unchanged. No new state.

### (e) No new state in the workspace state machine

Restate explicitly (in `WM-014` or `§5 invariants`): review-loop introduces no new workspace lifecycle state. The existing `leased → merge-pending → merged` (or `... → discarded`) progression accommodates the entire cycle. Per-iteration state lives in the Run record's `context` (per execution-model `EM-012` amendment), NOT in the workspace state machine.

## 3. Rationale

- The existing workspace primitives already accommodate review-loop's pattern; this design pass is largely about pre-empting future reader confusion.
- Locked decision: implementer and reviewer share the same worktree sequentially, never concurrently. One lease covers the whole run. `WM-010..WM-011` already say this; the design pass amplifies the naming.
- The `.harmonik/review.json` artifact path is the load-bearing new addition — it is the bus between the reviewer agent and the daemon's verdict-routing logic. Naming it in workspace-model (rather than handler-contract or event-model) is consistent with `workspace-model.md §4.7`'s ownership of in-worktree control-plane paths.
- `.gitignore` hygiene closure prevents the verdict file from polluting checkpoint commits.

## 4. Requirements traceability

| Req (02-components.md C6) | Target-state element |
|---|---|
| `WM-011`'s "one active agent at a time" already permits review-loop's pattern; add a naming example | §2 (a) |
| `WM-013a` lease-lock semantics preserved; one lease per run lifetime | §2 (b) |
| `WM-030` session-log directories accumulate per-session; multi-session per worktree valid | §2 (d) |
| Lease ownership is per-run, not per-session (explicit) | §2 (b) |
| No new state in workspace state machine | §2 (e) |
| New artifact path `.harmonik/review.json` named | §2 (c) |

## 5. Open decisions remaining for spec-draft pass

- **Inclusion in `WM-013e` ignore set.** Cross-spec coordination request: add `.harmonik/review.json` and `.harmonik/review.iter-*.json` to the daemon-startup write-or-fail ignore-entry list. Spec-draft confirms.
