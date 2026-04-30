# Round 1 Critic Review — workspace-model.md v0.2.0

## Verdict summary

The draft is structurally sound on the big calls that matter for Gas Town alignment: lease-by-run (§4.3), canonical worktree path derivable from `run_id` (WM-002/WM-013), three-level branching with small-scope collapse (§4.2/WM-008), and the orthogonal interrupt-state field (§4.10). The load-bearing softness sits in four places: (a) the state machine in §7.1 contradicts the protocol pseudocode in §7.2 and the sidecar-ordering requirement WM-016 on what event drives `setup → ready`; (b) the "original implementer" identity — the load-bearing role for every merge-conflict path — is never defined mechanically, while core-scope §3 explicitly admits that the merge node MAY be non-agentic, creating a case the spec does not handle; (c) the re-run vs intra-run-loop classification (§4.9) is declared deterministic on the verdict enum but the enum is owned by reconciliation, meaning the "deterministic" predicate is only as solid as that external source, and the spec names no default for verdicts not yet enumerated; (d) lease lifetime has no mechanical end: WM-010 says "full lifetime" but no requirement names the transition that releases the lease on `merged` or `discarded` (it is implicit only, through state-machine terminals); (e) the interrupt-state orthogonality claim has a silent leak — the `workspace_interrupted` transition row in §7.1 applies to "any" state, which includes `merged` and `discarded`, both of which cannot meaningfully be interrupted.

Verdict: **proceed with revisions**. None of the findings require architectural re-work, but six need concrete text before advancing past draft. Scope discipline is mostly honest — the only scope leak is WM-015 hard-coding event names after §2.2 cedes payload shapes to event-model.md (this is the same failure mode execution-model r1 had with `sub_workflow_entered`). Reading order works in the requirements-first shape; §§1–5 cohere.

The three findings that would most change the spec if acted on:
1. **§7.1 state-machine ↔ §7.2 pseudocode ↔ WM-016 contradiction on when `ready` is entered** (blocking — implementer cannot write the state machine).
2. **Original implementer undefined; non-agentic merge node case unhandled** (blocking — first merge conflict on a mechanical merge node has no defined resolver).
3. **Lease lifetime has no release-transition requirement** (important — lease-by-run is an invariant with no termination rule).

---

## Challenges

### Challenge 1 — §7.1 state machine, §7.2 pseudocode, and WM-016 disagree on when `ready` is entered

- **Challenge** — the three normative surfaces that describe the `setup → ready → leased` transition give three incompatible rules for what event advances the state, and in which order the sidecar is written.

- **What the spec says** —
  - §7.1 row (line 458): `| setup | metadata sidecar written | sidecar file present per §4.7 | ready | — |` — says the sidecar is written BEFORE `ready`, advancing `setup → ready`.
  - §7.1 next row (line 459): `| ready | handler launch imminent | run_id held by orchestrator | leased | workspace_leased |` — says the transition `ready → leased` is triggered by "handler launch imminent," not by the sidecar write.
  - WM-016 (line 166–168): "The workspace manager MUST write the session-log directory and `harmonik.meta.json` sidecar per §4.7 BEFORE emitting `workspace_leased`. Emission order is: (a) worktree created, (b) branch created, (c) session-log directory and metadata sidecar written, (d) `workspace_leased` event emitted." — places the sidecar write between `created` and `leased`, with no `setup`/`ready` intermediate step.
  - §7.2 pseudocode (lines 473–495): `create_workspace` emits `workspace_created` and returns `state=ready` without writing any sidecar. A separate `stamp_session_metadata` function writes the sidecar AND emits `workspace_leased`.

- **Is the justification adequate?** — no. Three distinct sequences are declared:
  1. §7.1: `created → setup → (sidecar write) → ready → (handler imminent) → leased`
  2. WM-016: `(create worktree) → (create branch) → (write sidecar) → leased` (no `setup`, no `ready` gate on sidecar).
  3. §7.2: `created → ready (no sidecar) → (sidecar write + leased emission simultaneously)`.
  
  The practical consequence is worse than a documentation drift. The sidecar is per-SESSION (WM-025: `${workspace_path}/.harmonik/sessions/${session_id}/harmonik.meta.json`), not per-workspace. A workspace hosts many sessions (WM-010: "Multiple agents sequentially"). Which session's sidecar write advances the workspace from `setup` to `ready`? Only the first? If so, the state machine confuses a per-workspace lifecycle state with a per-session artifact.

- **Stronger alternative** — collapse `setup` into `created` and make `ready` a transient state emitted only when the FIRST session's sidecar has landed. Concrete proposal:
  - Retire `setup` (it carries no observable work the spec owns) OR rename it to `worktree-ready` with the guard `git worktree add succeeded AND sessions_dir exists` (what §7.2's `create_workspace` actually produces).
  - Make `ready → leased` conditional on "first session's sidecar written" to match WM-016's ordering: the per-WORKSPACE state advances `ready → leased` on the first handler launch; subsequent sessions write their own sidecars without re-transitioning workspace state.
  - Rewrite §7.1 rows:
    ```
    | (initial) | orchestrator issues create | run_id unique, path free | created | workspace_created |
    | created | git worktree add + sessions_dir mkdir | WM-003 succeeded | ready | — |
    | ready | first session's sidecar written | sidecar file present per WM-026 | leased | workspace_leased |
    ```
  - Rewrite §7.2 to write the first sidecar INSIDE `create_workspace` (so the sequence matches WM-016), and add a separate `launch_next_session` function for subsequent agents that stamps their sidecars without re-transitioning workspace state.

- **How load-bearing** — blocking. An implementer cannot write the workspace manager's state machine without resolving this. Every downstream consumer of `workspace_leased` (S04 handler-contract, S08 memory-layer) races on emission ordering; the contradiction is directly observable at the wire.

- **Additional finding** — the session-vs-workspace grain problem. The workspace is a PER-RUN concept, surviving many agents. The sidecar is PER-SESSION, one per agent launch. Conflating them in the state machine (Challenge 9's `setup` state getting exited on "sidecar written") means the workspace's lifecycle state is being advanced by a per-session event, which is a category error. A cleaner separation: the WORKSPACE lifecycle is `created → ready → leased → merge-pending → (merged | discarded | conflict-resolving)`, driven by run-level events; the SESSION lifecycle is a separate per-agent state tracked by handler-contract. The spec currently mixes the two grains in §7.1 without marking the seam.

### Challenge 2 — "Original implementer" is named as a load-bearing role but never mechanically defined; the non-agentic merge case is unhandled

- **Challenge** — WM-022, WM-024, and WM-INV-004 all route on the predicate "ORIGINAL IMPLEMENTER," but no requirement defines how this agent is identified at conflict-resolution time, nor what happens when the merge node itself is non-agentic (core-scope §3 admits this explicitly).

- **What the spec says** —
  - §3 glossary (line 61): "**original implementer** — the agent that performed the work leading to a merge; for MVH, the merge-conflict resolver. (see §4.6)"
  - WM-022 (line 211): "the resolution agent MUST be the ORIGINAL IMPLEMENTER — the agent whose work produced the divergent commits on the task branch."
  - WM-024 (line 227): "role = ORIGINAL IMPLEMENTER, model-class = the handler class that launched the implementer originally, input shape = (task branch, integration branch, conflict markers) plus the run's transition history."
  - core-scope §3 (line 188): "A `merge` node (agentic or mechanical) integrates the run's branch to the integration branch."
  - execution-model §4 node type enum permits `non-agentic` nodes.

- **Is the justification adequate?** — no. Several cases are unaddressed:
  1. **Which agent, when there are many.** A run has multiple agentic nodes (planner, researcher, builder, reviewer). Each produced "divergent commits on the task branch." The spec offers no rule to pick ONE as the original implementer. Candidates: (a) the last agentic node before the merge node; (b) the agent whose commits touched the conflicting hunks; (c) a reserved "implementer" node-role attribute on the workflow.
  2. **Non-agentic merge, no preceding agentic implementer.** If a workflow's only writes come from non-agentic nodes (mechanical refactors, generated-code commits), there IS no "original implementer" agent. WM-022 is undefined on this input.
  3. **Implementer unavailable.** If the implementer's handler class has been retired (post-MVH: model deprecated) or rate-limited past its budget, WM-023 escalates to human — but WM-022 says the implementer MUST resolve. "MUST" with no graceful degradation is a liveness bug at the spec level.
  4. **Implementer already terminated.** Agent sessions are ephemeral (per §A.3 rationale line 599). By the time a merge happens, the implementer's process is gone. "Re-dispatch" (WM-024) means launching a NEW session of the same handler class with reconstructed input. That is not the same as "the ORIGINAL implementer" — it is a fresh agent with the same role. The spec conflates agent-identity with handler-class identity.

- **Stronger alternative** — replace the prose concept with a record field on the run/workflow. Concrete proposal:
  - Add WM-001a: "Every agentic node that produces checkpoint commits MUST be recorded on the run's transition trail. The `implementer_handler_ref` field on the `Workspace` record (new field, added to §6.1) carries the `handler_ref` of the agentic node whose commits are the most recent writes on the task branch at merge-pending entry."
  - Add WM-022a: "If `implementer_handler_ref` is null (the task branch contains only non-agentic commits), the system MUST emit `merge_conflict_escalation` directly on conflict detection, skipping WM-022 re-dispatch. Human resolution is the fallback for all-mechanical conflict paths in MVH."
  - Replace the prose "ORIGINAL IMPLEMENTER" in WM-022/WM-024/WM-INV-004 with "the handler class identified by `workspace.implementer_handler_ref`".
  - Add WM-022b: "Re-dispatch of the implementer launches a FRESH session of the recorded handler class. The phrase 'original implementer' is normative for the handler-class lineage, not for process-identity preservation. Session-level continuity across merge-conflict resolution is NOT guaranteed."
  - Add OQ-WM-005: "Implementer handler-class retirement between initial commits and merge-time re-dispatch. Default-if-unresolved: treat as `merge_conflict_escalation` directly (no silent handler-class remapping)."

- **How load-bearing** — blocking. The first merge conflict where the workflow has multiple agentic nodes, OR the merge node is mechanical, OR the implementer handler is unavailable, produces undefined behavior at MVH.

### Challenge 3 — Lease lifetime has no end-of-life mechanical rule; WM-010 says "full lifetime" but no requirement names the releasing transition

- **Challenge** — WM-010 declares leases are held "for the run's full lifetime" but no requirement names the transitions that RELEASE the lease on run terminal, nor what state the `Workspace` record takes after release.

- **What the spec says** —
  - WM-010 (line 127–129): "A workspace MUST be leased by exactly one `Run` for the run's full lifetime."
  - WM-INV-001 (line 341–343): "Every workspace MUST be leased by exactly one `Run` for that run's full lifetime."
  - §7.1 (lines 461, 465): `merge-pending → merged` and `leased → discarded` are the only terminal transitions, emitting `workspace_merged` or `workspace_discarded`. Neither row names "lease released."
  - WM-031 (line 273): "A worktree whose run reached a terminal failure state MUST persist on disk with its branch intact." — preservation is named; release is not.
  - WM-033 (line 285): the orphan sweep removes stale LOCK files — implying a lock-file mechanism exists, but no requirement creates or releases it.

- **Is the justification adequate?** — no. Several questions are unaddressed:
  1. **When does the lease end?** On `workspace_merged`? On `workspace_discarded`? On `run_completed` (which is event-model's event, not this spec's)? The spec is silent.
  2. **What happens to the `Workspace` record after release?** Does it persist with `state=merged` forever? Can a NEW run re-lease a merged workspace? WM-012 ("one run per bead at a time") forbids concurrent but is silent on sequential re-lease of the same worktree path.
  3. **What is the mechanical representation of the lease?** WM-033 mentions a `lease.lock` file (table row line 432), but no requirement declares its format, writer, or release semantics. The lock file shows up only in the orphan-sweep discussion as the thing that gets swept — it has no birth requirement.
  4. **Failed-run lease.** A failed run's workspace persists (WM-031). Is the lease released or held? If held, WM-012 prevents a re-run on the same bead from getting a workspace (since WM-034 says "fresh worktree at new path," but the OLD worktree's lease blocks something, unclear what).

- **Stronger alternative** — add three requirements:
  - WM-013a: "The lease on a workspace is held by the `lease.lock` file at `${workspace_path}/.harmonik/lease.lock`, written by S06 at `workspace_created` emission, containing the owning `run_id` and the owning daemon's fingerprint. The file's existence represents the lease; its absence represents a released workspace."
  - WM-013b: "The workspace manager MUST release the lease (delete the lock file) on every terminal workspace transition: `merged`, `discarded`. Release MUST occur AFTER the terminal event is emitted. A failed run's lease is released; the worktree directory and branch persist per WM-031, but the lease file does NOT."
  - WM-013c: "A released workspace MAY be re-leased by a subsequent run only if the new run targets a NEW `run_id` and therefore a NEW canonical path per WM-002. Path-level re-use (same worktree serving a new run) is forbidden."
  - Rewrite WM-033 to reference WM-013a for the lock file's format, so the orphan sweep has a target to match against.

- **How load-bearing** — important. Reconciliation's "lost lease" detection (cited by WM-038 line 322) depends on a lease-file model the spec does not declare. The first daemon-crash scenario has no mechanical rule for whether the crashed run's workspace is still "leased" on recovery.

### Challenge 4 — Re-run vs intra-run classification is declared deterministic on an enum owned elsewhere; no default rule for unlisted verdicts

- **Challenge** — WM-036 asserts that re-run vs intra-run-loop is "deterministic on the verdict enum value" and lists the four mappings. Since the enum is owned by reconciliation.md §9.5, any verdict added post-MVH has no pre-declared mapping. WM-036's last clause ("other verdicts (abandon, escalate) → no re-run attempted") is prose, not a rule that extends.

- **What the spec says** —
  - WM-036 (line 306–308): "The classification table is: `reopen-bead` → fresh worktree; `resume-here` | `resume-with-context` | `reset-to-checkpoint` → keep worktree; other verdicts (abandon, escalate) → no re-run attempted."
  - WM-034/WM-035 normatively bind specific verdict names to specific worktree behavior.
  - §9.3 names reconciliation.md §9.5 as the verdict vocabulary owner (co-reference, read-only).

- **Is the justification adequate?** — partial. The specification works for the four-verdict enum of today, but:
  1. The "other verdicts → no re-run" clause is not a generalizable rule — an implementer encountering a new verdict (e.g., a hypothetical `retry-with-different-model`) has to come back and edit this spec.
  2. The workspace manager cannot validate verdicts it does not know about. If reconciliation adds a verdict, the workspace manager silently treats it as "no re-run" (per the "other verdicts" catch-all), which may be the wrong default for some additions.
  3. "Deterministic" is done a disservice by the spec owning only HALF the table. The rule should be: reconciliation owns the enum AND the fresh-vs-keep attribute for each variant; this spec consumes the mapping. Otherwise every reconciliation verdict change triggers an edit here.

- **Stronger alternative** — invert the ownership. Concrete proposal:
  - Rewrite WM-036 as: "Each verdict in the reconciliation enum per [reconciliation.md §9.5] MUST carry a `worktree_disposition ∈ {fresh, keep, none}` attribute owned by that spec. The workspace manager MUST route on that attribute mechanically: `fresh` → WM-034; `keep` → WM-035; `none` → no re-run. The four MVH verdicts carry: `reopen-bead` → fresh; `resume-here`, `resume-with-context`, `reset-to-checkpoint` → keep; `abandon`, `escalate` → none. The attribute MUST be defined in reconciliation.md §9.5; this spec consumes it."
  - If reconciliation.md has not yet declared the attribute, add OQ-WM-005 to coordinate the schema change.
  - Retain WM-034 and WM-035 as the mechanical rules for `fresh` and `keep`, but make them route on the attribute rather than on verdict enum values.

- **How load-bearing** — important. The enum coupling is exactly the kind of bilateral obligation that template §9.3's Co-references subsection exists to clean up. The current shape forces a bilateral edit every time reconciliation extends its verdict vocabulary.

### Challenge 5 — Interrupt-state is declared orthogonal to lifecycle state, but §7.1 permits `workspace_interrupted` on any state — including `merged` and `discarded`

- **Challenge** — the orthogonality claim (§4.10, §A.3 rationale) holds for the active lifecycle states (`created`/`setup`/`ready`/`leased`/`merge-pending`/`conflict-resolving`) but breaks at terminal states (`merged`/`discarded`), where interruption is semantically incoherent. §7.1's "any" row admits this as a live transition.

- **What the spec says** —
  - §7.1 last row (line 466): `| any | operator pause/stop OR lost-lease detection | see §4.10.WM-038 | (same state, interrupt_state != none) | workspace_interrupted |`.
  - WM-037 (line 314): "The workspace record MUST carry an `interrupt_state` field orthogonal to the lifecycle state of §4.4. ... The lifecycle state (e.g., `leased`) and the interrupt-state (e.g., `operator-paused`) compose independently."
  - §A.3 rationale (lines 604–605): "Interrupt events can occur at any lifecycle state (a workspace in `setup` can be interrupted by operator stop just as one in `leased` can). Encoding interrupts as lifecycle states would multiply the state space..."
  - WM-040 (line 333–335): "Clearing `interrupt_state` back to `none` MUST be driven by either (a) an `operator_resuming` event... or (b) a reconciliation verdict..."

- **Is the justification adequate?** — no. The orthogonality defense is correct for IN-FLIGHT lifecycle states but is silent on TERMINAL states:
  1. A `merged` workspace is done. Operator pause has no effect; there is nothing to pause. But §7.1 says `workspace_interrupted` fires on "any" state.
  2. A `discarded` workspace is also done. Same logic.
  3. If an interrupt fires on a merged workspace, WM-040 requires operator resume or reconciliation to clear — but resuming a `merged` workspace is meaningless; it's already complete.
  4. The orthogonality claim is load-bearing because the `interrupt_state × lifecycle_state` product space is what defense-in-depth reconciliation detectors route on. Permitting nonsensical pairs is strictly worse than shrinking the orthogonality claim to where it holds.

- **Stronger alternative** — constrain the orthogonality to in-flight states explicitly. Concrete proposal:
  - Add WM-037a: "The `interrupt_state` field composes orthogonally with the lifecycle states `created`, `setup`, `ready`, `leased`, `merge-pending`, `conflict-resolving`. Terminal lifecycle states (`merged`, `discarded`) MUST carry `interrupt_state = none`; a transition into a terminal state MUST clear any non-`none` interrupt state. Interrupts that fire on a workspace already in a terminal lifecycle state MUST be rejected silently and logged as operator-observability events per [docs/foundation/components.md §7.8]."
  - Rewrite §7.1's last row: `| leased | created | setup | ready | merge-pending | conflict-resolving | operator pause/stop OR lost-lease detection | see §4.10.WM-038 | (same state, interrupt_state != none) | workspace_interrupted |` — explicitly enumerating the six in-flight states.
  - Add to §A.3 rationale: "The orthogonality claim applies to in-flight states only. Terminal states are absorbing; interrupt signals received at terminal are archived as operator-observability events, not as state transitions."

- **How load-bearing** — important. The ambiguity surfaces the first time reconciliation encounters a crashed run whose workspace had already merged before the crash — it will emit `workspace_interrupted` against a `merged` workspace per the current rule, and WM-040's "clearing" requirement has no well-defined meaning.

### Challenge 6 — WM-015 hard-codes seven event names; §2.2 ceded event taxonomy to event-model.md

- **Challenge** — WM-015's event list (`workspace_created`, `workspace_leased`, `workspace_merge_pending`, `workspace_merged`, `workspace_discarded`, `workspace_interrupted`, `merge_conflict_escalation`) and the payload fields (`workspace_id`, `run_id`, branch name) are taxonomy declarations — exactly what §2.2 declared out of scope.

- **What the spec says** —
  - §2.2 (line 46): "Event payload shapes — owned by [docs/foundation/components.md §3.2] event-model. This spec declares emission obligations (WHEN); event-model declares payloads (WHAT)."
  - WM-015 (line 161): "The workspace manager MUST emit the following events at the state transitions of §7.1: `workspace_created`, `workspace_leased`, ..., `merge_conflict_escalation`. Each event payload MUST carry `workspace_id`, `run_id`, and the associated branch name."
  - §6.3 (lines 438–444): repeats the event names and emission rules.

- **Is the justification adequate?** — no. This is the same failure mode execution-model r1 had with `sub_workflow_entered`/`sub_workflow_exited` (critic.md Challenge 3). The payload-field declaration ("MUST carry `workspace_id`, `run_id`, and the associated branch name") in WM-015 is payload schema. Event names ARE taxonomy. §2.2's ownership split — "WHEN here, WHAT in event-model" — is contradicted by declaring both here.

- **Stronger alternative** — strip names and fields to citations. Concrete proposal:
  - Rewrite WM-015 as: "The workspace manager MUST emit the lifecycle events declared in [event-model.md §3.2] at the state transitions of §7.1. The event-model.md registry names each event and its payload schema; this spec is normative for the WHEN each event fires, via the emission rules in §7.1 and §6.3."
  - Move the event-name list and correlation-field requirement to event-model.md's registry. If event-model.md has not yet registered these events, add OQ-WM-005 (or WM-006, renumber) to migrate at event-model finalization.
  - Retain §6.3 as a WHEN-only table that cites event-model for the payload: `| Event (see event-model.md §3.2) | Emission trigger | Workspace state after |`.

- **How load-bearing** — important. If event-model lands with `workspace_create`/`workspace_lease` (present tense, no participle) or with `workspace_id` renamed to `workspace_key`, this spec has to change too. The scope leak is avoidable and precisely the kind of cross-spec coupling §2.2's out-of-scope list is meant to prevent.

### Challenge 7 — Sub-workflow branching contract is unnamed; EM-034's namespaced nodes have no corresponding workspace rule

- **Challenge** — execution-model §4.8 establishes that sub-workflows expand in place at runtime, with the PARENT run's `run_id` being the sole run identifier. This spec's three-level branching model (§4.2) names "task branch = `run/<run_id>`" as per-run, but says nothing about sub-workflow behavior. A sub-workflow expansion runs inside a parent run — so all nested-node checkpoints land on the parent's task branch. That is actually correct by EM-035 — but this spec does not cross-reference it, and a reader could assume sub-workflows get their own task branches.

- **What the spec says** —
  - WM-005 (line 97): "Every run's task branch MUST be named `run/<run_id>`."
  - §4.2 never mentions sub-workflows.
  - §9.1 (line 507): cites `[execution-model.md §4.10]` for backtracking but NOT `§4.8` for sub-workflow expansion.
  - WM-018 (line 183): "The merge-back operation... MUST be performed by a workflow node that executes INSIDE the run's already-leased worktree." — consistent with sub-workflows running inside the parent worktree.

- **Is the justification adequate?** — partial. The branching contract is CORRECT by inheritance from EM-034's "parent `run_id` is the sole run identifier," but the reader of this spec alone would not know that. The question "does a sub-workflow get its own task branch?" is implicit-no; an explicit rule closes the door on misimplementation.

- **Stronger alternative** — add one requirement and one cross-reference:
  - WM-005a: "Sub-workflow expansion per [execution-model.md §4.8.EM-034] does NOT create additional task branches or workspaces. All checkpoint commits produced by an expanded sub-workflow's nodes land on the parent run's task branch per [execution-model.md §4.5.EM-023, §4.8.EM-035]. The workspace is leased by the parent run; nested execution occupies the same worktree."
  - Add `[execution-model.md §4.8]` to §9.1 Depends on.
  - Add a note in §A.3 rationale: "Sub-workflow expansion is a single-run, single-worktree mechanism by design. A sub-workflow that spawned its own workspace would re-introduce the child-run multiplicity EM-034 forbids."

- **How load-bearing** — minor. EM-034 and EM-035 are load-bearing in execution-model; this spec's inheritance is consistent. But making it explicit here prevents a cross-spec-drift hazard if either spec is edited in isolation.

### Challenge 8 — Small-scope collapse (WM-008) is gated on the Beads parent-child edge, but Beads is owned by another spec; the deterministic predicate has a silent null case

- **Challenge** — WM-008 says "present → task branch targets integration; absent → task branch MAY target main." The `MAY` is a soft choice. Combined with WM-006's "A run without a parent-bead context MUST target the fixed `harmonik/integration` branch," the two requirements disagree on the default for a no-parent run.

- **What the spec says** —
  - WM-006 (line 103): "A run without a parent-bead context MUST target the fixed `harmonik/integration` branch."
  - WM-008 (line 115): "When a run has no parent-bead relationship in Beads..., the task branch MAY squash-merge directly to main, skipping the integration branch. The decision is deterministic on the Beads parent-child edge: present → task branch targets integration; absent → task branch MAY target main."

- **Is the justification adequate?** — no. A run with no parent bead has two mutually exclusive normative rules:
  - Per WM-006: MUST target `harmonik/integration`.
  - Per WM-008: MAY target main directly.
  
  Which applies? The "deterministic" framing of WM-008 is undermined by the `MAY`. If the decision is deterministic, it should be `MUST`. If it is operator-configurable, it should say so and name the configuration knob.

- **Stronger alternative** — pick one, and make it operator-policy-gated. Concrete proposal:
  - Rewrite WM-008 as: "When a run has no parent-bead relationship in Beads, the task branch's merge target MUST be determined by operator policy per [operator-nfr.md §<TBD>] with two allowed values: `integration` (squash-merge to `harmonik/integration`, the default) or `main` (squash-merge directly to main, the small-scope-collapse shape). Without an operator override, `integration` is the default per WM-006."
  - Rewrite WM-006 as: "An integration branch MUST exist named `harmonik/integration` by default. When a run has a parent-bead context, its task branch MUST target the derived branch `harmonik/integration/<parent_bead_id>`. When a run has no parent-bead context, its target is operator-policy-driven per WM-008."
  - Add OQ-WM-005 (or renumber) to identify the operator-nfr knob name and default.

- **How load-bearing** — important. The first no-parent-bead run will exercise this path; the contradictory MUST/MAY produces two legal implementations that disagree on where main ends up getting commits.

### Challenge 9 — `setup` lifecycle state has no observable work and appears in two documents with no matching cause

- **Challenge** — the `setup` state in the state-machine enum (§6.1, §7.1) is unreachable in the protocol pseudocode (§7.2) and has no requirement that describes the work it represents.

- **What the spec says** —
  - §6.1 `ENUM WorkspaceState` (line 392–399) lists `setup` as the second lifecycle state.
  - §7.1 rows (lines 457–458) describe `created → setup → ready` with `setup` entered on "git worktree add succeeds" and exited on "metadata sidecar written."
  - §7.2 `create_workspace` pseudocode (lines 473–482) goes `path check → git.worktree_add → mkdir sessions_dir → return state=ready`. Never visits `setup`.
  - No requirement in §4 declares what occurs in the `setup` state, nor what distinguishes it from `created` or `ready`.

- **Is the justification adequate?** — no. `setup` is a placeholder state that does no observable work. If the intent is "between `git worktree add` success and sidecar write," then the pseudocode skips it. If the intent is "between workspace record creation and `git worktree add`," then §7.1's guards are wrong. Either way, a lifecycle state with no owning requirement is dead weight.

- **Stronger alternative** — retire `setup` from the enum and the state machine. Concrete proposal:
  - Drop `setup` from `ENUM WorkspaceState` in §6.1.
  - Replace §7.1's first two rows with a single row: `| (initial) | orchestrator issues create | run_id unique, path free | created | workspace_created |`.
  - Add a new row: `| created | sessions_dir + first sidecar written | WM-026 succeeded | ready | workspace_leased |` (matching WM-016's ordering from Challenge 1).
  - Removes one state, removes one contradiction, simplifies the product space for `interrupt_state × lifecycle_state` by 17%.

- **How load-bearing** — minor on its own, but compounds with Challenge 1. Retiring `setup` is the simplest path to resolving the §7.1/§7.2/WM-016 disagreement.

---

## Scope leaks

Requirements that violate §2.2's out-of-scope declarations:

1. **WM-015 event names and payload fields.** §2.2 cedes event schemas to event-model. WM-015 declares seven event names and a three-field minimum-payload contract. See Challenge 6. This is the largest scope leak in the spec.

2. **WM-017 payload shape for `workspace_merged`, `workspace_discarded`, `workspace_interrupted`.** Line 175 names specific payload fields: "merged commit hash and the surviving branch name" for `workspace_merged`; "discarded branch name" for `workspace_discarded`; "last-known durable state" for `workspace_interrupted`. All three are payload shape, owned by event-model per §2.2. The emission ordering belongs here; the payload field list belongs in event-model.

3. **WM-023 payload fields for `merge_conflict_escalation`.** Line 220 names payload carriage: "`workspace_id`, `run_id`, `branch_name`, and the conflict summary." Same failure mode.

4. **WM-027 and WM-016 both declare emission ordering.** Both are WHEN requirements (correctly scoped), but the fact that WM-027 exists as a standalone requirement whose only content is "per WM-016" suggests WM-016 already covered it. The redundancy is harmless but suggests reviewer-suppressed content got duplicated.

5. **§6.3 table.** Lines 438–444 repeat the event names from WM-015 with slightly different emission-rule prose. The emission rules belong here; the names belong in event-model.

6. **WM-028 trailer value correlation.** Line 254: "the metadata sidecar's `bead_id` field MUST carry the same value as the `Harmonik-Bead-ID` trailer on checkpoint commits per [execution-model.md §4.4.EM-017]." The "same value" rule is in scope — it is a workspace-layer correlation requirement. But the TRAILER NAME (`Harmonik-Bead-ID`) is execution-model's to own; this spec could cite EM-017 and say "the same bead-ID value as the checkpoint trailer" without naming the trailer string. Minor, but same pattern.

**Stronger shape for all six**: replace inline names/fields with citation form. Example for WM-017: "The `workspace_merged`, `workspace_discarded`, and `workspace_interrupted` events MUST carry the correlation fields declared for each in [event-model.md §3.2]. This spec is normative for emission timing; see §7.1 and §6.3."

---

## First-plausible-answer findings

Requirements where the author picked the first answer without naming the tradeoff:

1. **WM-030 default: preserve-in-merged-branch.** (Line 265–266.) The session-log directory lives under the workspace's `.harmonik/sessions/` path, which means squash-merging the task branch to integration sweeps those files INTO the merged commit tree on integration. The tradeoff: audit retention (preserve) vs. integration-branch cleanliness (exclude session logs from the merged diff, which pollutes every code review with session-log noise). The "default is preserve-in-merged-branch for audit retention" is plausible, but the opposite default (gitignore under `.harmonik/sessions/`, archive out-of-tree) is equally defensible and is the more common industry pattern. The spec names the default but not the tradeoff. OQ-WM-003 gestures at archival config but doesn't acknowledge the integration-branch-cleanliness cost of the chosen default.

2. **WM-022 "original implementer resolves."** (Line 211.) The tradeoff — rebuild context in a separate merge agent vs. re-dispatch implementer vs. escalate — is acknowledged in §A.3 rationale, but the alternative "dedicated merge agent with shared context" is dismissed on cost grounds without considering that a merge-specific agent COULD have narrower, more-focused prompts that are easier to iterate on. The first-plausible-answer trap here is picking the implementer-re-dispatch without asking whether the session-logs are rich enough for a fresh merge agent to succeed cheaply. Not blocking; a named tradeoff would help post-MVH iteration.

3. **WM-031 failed-run persistence.** (Line 273.) "MUST persist on disk with its branch intact" — no disk-pressure backstop. The tradeoff is audit-preservation vs. disk-pressure-bounded-operations. §A.3 rationale cites operator-initiated cleanup workflows — but those are post-MVH (per line 534 "automated merge-agent dispatch... are additive extensions"). For MVH, a daemon running weeks against a repo with hundreds of failed runs will fill the disk. The first-plausible-answer is "preserve everything." The alternative — "preserve for N runs or M days, configurable" — is what operator experience will actually want. Add a default retention window or cite why unbounded growth is MVH-acceptable.

4. **WM-004 `workspace_id = "ws-" + run_id`.** (Line 87–89.) The recommendation is plausible but the prefix `ws-` is never motivated — why not `workspace:<run_id>`, why not bare `<run_id>` (since WM-013 says it's discoverable from `run_id` alone), why not a hashed form that's stable but not trivially run-id-reveal. The first-plausible-answer sits on a cosmetic choice; the actual decision is whether `workspace_id` is a SEPARATE identity surface at all. If it is always derivable from `run_id`, consider removing the field entirely and having consumers use `run_id` for both.

5. **WM-038 reconciliation + operator-control co-ownership of `interrupt_state`.** (Line 320–323.) Two distinct subsystems can mutate the field. The tradeoff: single-writer (simpler, lower-contention) vs. multi-writer (more-flexible, race-prone). The spec names the co-ownership without asking whether one subsystem should be the sole writer and the other should request transitions via events. Gas Town-style centralization would suggest: operator-control emits an event, reconciliation emits an event, and the workspace manager is the single writer reacting to both. Current shape is closer to shared-mutable-field.

6. **WM-007 integration-to-main merge style "NOT dictated by this spec."** (Line 109.) The tradeoff — named here but not resolved — is: harmonik either owns the full pipeline to main (prescriptive, enforces policy) or stops at the integration branch (permissive, delegates to developer judgment). The spec picks the permissive answer without naming what happens if the developer's main-merge workflow diverges from harmonik's model (e.g., force-push to main, squash-merge to main with different trailer handling). The first-plausible-answer is "we stop at integration"; the alternative "we own the full pipeline with developer-opt-out" is equally defensible and gives better replay guarantees.

7. **WM-037 five interrupt_state values with no routing distinction.** (Line 314–316.) The split between `operator-stopped-graceful` and `operator-stopped-immediate` does no work in the current spec (see Definitional gap #8). The first-plausible-answer is "capture what the operator command said"; the alternative "collapse to one operator-stopped and let the event carry the graceful/immediate flag" reduces state-machine breadth without losing information. Either pick a routing rule that distinguishes the two, or collapse.

---

## Invariant audit

Applying the template §5 selection test ("an invariant is a system-wide property that constrains multiple subsystems' requirements"):

- **WM-INV-001 — Lease-by-run.** (Line 341.) Rule: "Every workspace MUST be leased by exactly one Run for that run's full lifetime." This is WM-010 verbatim (line 127). WM-010 lives in §4.3 (lease model, single subsystem of this spec). Does it span subsystems? It is consumed by execution-model (run lifetime) and reconciliation (lost-lease detection), so arguably YES — but the rule itself is authored here and doesn't cite the cross-subsystem obligations. **FAILS** the selection test as currently worded. Rescue: rewrite as "Every subsystem observing a workspace MUST treat the workspace's run_id as the exclusive lease holder for the run's full lifetime; no subsystem MAY mechanism a per-agent lease acquisition" — then it spans subsystems. OR delete the §5 copy and keep WM-010.

- **WM-INV-002 — One run per bead at a time.** (Line 347.) Rule: WM-012 verbatim (line 139). WM-012 is in §4.3. Same failure mode. Consumed by execution-model (EM-014 bead lifecycle) and beads-integration. Rescue: rewrite as "For every bead, every subsystem routing on run activity MUST assume at-most-one in-flight run; reconciliation's post-crash detectors MUST reject observations of multiple in-flight runs as inconsistency evidence." OR delete the §5 copy.

- **WM-INV-003 — Checkpoint commits obey git append-only semantics on the task branch.** (Line 353.) Rule: "No operation in this spec MAY rewrite committed history (amend, rebase, filter-branch) on a task branch that has emitted `workspace_leased`." This is a genuine cross-subsystem invariant because handler-contract (S04) ALSO writes commits, and execution-model owns the checkpoint cadence. The rule constrains at least three subsystems' authoring surfaces. **HOLDS**, but the sensor is soft — "any operation in this spec" is a self-reference. A better sensor would be: "Any git observer of the run's task branch MUST reject commits whose parent is not the prior tip of the branch (amend, rebase violate this invariant)." The named sensor is the git ref-log or a git-hook; naming it would satisfy AR-042.

- **WM-INV-004 — Merge-conflict resolver is the original implementer.** (Line 359.) Rule: "For every merge conflict produced by §4.5 merge-back, the resolution agent MUST be the original implementer per §4.6.WM-022." This is WM-022 verbatim with the failure-mode restated. WM-022 lives in §4.6 (single subsystem of this spec, conflict resolution). Does it span subsystems? It constrains handler-contract (which handler class resolves), reconciliation (when escalating), AND this spec. Arguably yes, but the wording is internal. **FAILS** as currently worded. Rescue: rewrite as "Any subsystem dispatching a merge-conflict resolution MUST NOT dispatch any agent other than the handler class recorded as the implementer; spec-authored alternative resolvers (e.g., a reviewer agent invoked at conflict-time) are forbidden." Then it constrains other subsystems' dispatching authority.

- **WM-INV-005 — Worktree path is canonical and derivable from run_id.** (Line 365.) Rule: "Every live workspace MUST be located at `<repo>/.harmonik/worktrees/<run_id>/` per WM-002." This is WM-002 verbatim plus "only the worktree-root prefix operator-configurable." WM-002 lives in §4.1. Does it span subsystems? Reconciliation's daemon-restart path depends on this; S04's worktree attachment depends on this. Arguably spans — but like the others, it reads as a §4 requirement promoted. **FAILS** as currently worded. Rescue: rewrite as "Any subsystem reconstructing workspace state from the filesystem MUST derive the workspace location from `run_id` without consulting a separate index; no registry-backed lookup MAY be the authoritative path." Then it constrains reconciliation and the orchestrator's discovery.

**Sensors.** None of the five invariants name a sensor (the thing that observes violation). Per architecture.md AR-042, an invariant needs a detector. Candidates to add:
- WM-INV-001: sensor = lease-lock file presence + owning-run_id correlation with live runs in Beads.
- WM-INV-002: sensor = query "count active runs per bead_id" against Beads.
- WM-INV-003: sensor = git ref-log showing no `update-ref -d` or `update-ref <old> <new>` rewriting on task branches.
- WM-INV-004: sensor = merge-conflict-resolution events whose handler_ref matches the recorded implementer_handler_ref.
- WM-INV-005: sensor = filesystem walk under `<repo>/.harmonik/worktrees/` matches `run_id` directory pattern with no out-of-pattern directories.

**Net recommendation**: three of five invariants (INV-001, INV-002, INV-005) are §4 requirements promoted; rewrite as cross-subsystem claims or retire. INV-003 and INV-004 are marginally cross-subsystem and are salvageable with the rewrites above. Add sensor clauses per AR-042 to all survivors.

---

## Definitional gaps

Terms used heavily in normative body but not rigorously defined:

1. **"Full lifetime"** (WM-010, WM-INV-001). Is the lease held from `workspace_created` to `workspace_merged`/`workspace_discarded`? Or from first `workspace_leased` to those terminal events? Or from `run_started` (an execution-model event) to `run_completed`/`run_failed`? The three framings give three different correctness conditions for lease-release races. The spec does not pick.

2. **"Original implementer"** — see Challenge 2. Glossary entry is circular: "the agent that performed the work leading to a merge" — which agent is THAT when multiple agentic nodes produced commits?

3. **"Terminal failure state"** (WM-031) — the spec lists `failed` and `canceled` but cites execution-model §7.1 for the definition. Fine. But WM-032's `discarded` vs `interrupt_state` split is conditional on "clean failure with preserved branch" vs "interrupted mid-run" — these two are not mutually exclusive. A run interrupted mid-execution can ALSO leave a preserved branch. Which takes precedence on state transition? Unclear.

4. **"Small-scope collapse"** (WM-008) — is a scope "small" because it has no parent bead, or because it has one-level depth? The spec uses "no parent-bead relationship" as the mechanical predicate, but the term "small-scope" implies more than absent parentage. A better name: "parentless run" — and drop the evocative "small-scope" language that implies a semantic judgment.

5. **"Sessions"** (WM-025, WM-026). WM-025 says "every agent session launched against a workspace" gets its own directory. A `session_id` is the per-session key. But `session_id` is never defined in §3 Glossary, never assigned a UUID shape in §6.1, never cited to handler-contract. An implementer does not know whether session_id is per-handler-launch, per-node-execution, or per-agent-turn.

6. **"Worktree-root prefix"** (WM-002, WM-INV-005). Is operator-configurable. But which operator-config precedence layer? YAML? CLI flag? Environment variable? None named; OQ-WM-002 gestures at a similar problem for integration-branch templates but doesn't cover worktree-root.

7. **"Merge conflict budget"** (WM-023). "within its budget or declares an unresolvable-conflict outcome" — budget is a resource quantity; its shape (wall time, LLM tokens, attempts, all three) and its default value are unstated. Handler-contract presumably owns the budget concept, but this spec should cite.

8. **"Interrupt_state"** values (WM-037). Five values enumerated, but `operator-stopped-graceful` vs `operator-stopped-immediate` is the ONLY place in the spec that distinguishes graceful from immediate stop. No requirement routes on the distinction — so why carry two values instead of one? Either add a routing rule (e.g., "graceful allows the current node to complete, immediate kills mid-node") or collapse to a single `operator-stopped` value.

---

## Cross-reference hygiene

- **WM-024 cites `[docs/foundation/components.md §4]`** (line 227) — this is the bootstrap form for handler-contract. handler-contract is already reviewed (r2 directory exists); the cite should migrate to `[handler-contract.md §<N>]`. Template cross-reference convention requires migration within one revision cycle of the target spec's finalization.

- **WM-028 cites `[execution-model.md §4.3.EM-014]`** (line 241, 254) — this is the correct form. Good.

- **§9.1 Depends on** lists execution-model only. The spec's body cites architecture.md (§4.1 and §4.9) in WM-010, WM-INV-001, and in the prose. Add architecture.md to depends-on.

- **§9.1 Depends on** does NOT list handler-contract, but WM-024 normatively cites handler-contract's delegation path. Either add handler-contract to depends-on (the right answer: cognition-tagged dispatching delegates to handler-contract), or demote WM-024's handler-contract cite to co-reference in §9.3 if the dependency is truly read-only.

- **§9.1 Depends on** lists `docs/foundation/components.md` for five different sections (§3.2, §4, §7.3, §7.5, §8.2, §9, §10.3) — this is the bootstrap citation form. Post-foundation, these will need migration to their per-component specs. Acceptable for R1 draft; should be tracked as an OQ for finalization.

- **§9.3 Co-references** lists three items. Missing: `[reconciliation.md §9.5]` cited at WM-034, WM-035, WM-040. The co-reference is a consumption relationship for the verdict enum (Challenge 4). Add.

- **Inter-spec cite form uses anchors that don't match template discipline.** The template §Cross-reference convention forbids "§4.2(b)"-style letter suffixes. The spec uses `§4.4.EM-016` (compound section+requirement ID) several times — this is the recommended `<prefix>-NNN (§N.N)` form reversed. Pick one. Preference: requirement ID first, section hint in parens: `EM-016 (§4.4)`. Current form is a minor readability snag.

- **Missing cite: EM-034c / EM-036a.** Execution-model v0.3 adds sub-workflow expansion pin (EM-034c) and terminal-outcome rule (EM-036a). WM's §4.5 merge-back does not cite either, but if a run includes sub-workflows, the merge trailer semantics (WM-019) inherit from EM-036a. Verify and cross-reference.

---

## MUST/SHOULD discipline

Items where keyword choice is wrong or permissive language hides a real requirement:

1. **WM-008 `MAY squash-merge directly to main`** — see Challenge 8. Either MUST (small-scope collapse is the rule for parentless runs) or MAY with operator policy gating, but not this bare MAY.

2. **WM-023 "within its budget or declares an unresolvable-conflict outcome"** — two OR'd conditions, neither defined. Replace with a positive rule: "If the implementer returns any outcome other than a successful merge commit within its handler-contract budget per [handler-contract.md §<N>], the workspace manager MUST emit `merge_conflict_escalation`."

3. **WM-030 "MAY move the directory to a post-merge archive path"** — the MAY hides a post-MVH feature. Collapse to: "For MVH, the session-log directory MUST be preserved in the merged branch. Post-MVH archive-path configuration is out of scope per §2.2." Aligns with the Core MVH profile in §10.1.

4. **WM-033 "MUST remove stale worktree LOCK files"** — the `LOCK` here is under `.harmonik/lease.lock` per the §6.2 table (line 432) — but the lock file itself is never declared by a MUST requirement (see Challenge 3). This MUST to remove a file the spec never declares exists is a dangling reference.

5. **WM-038 "workspace manager owns the field's storage; it does NOT mutate the field except as directed"** — the "does NOT mutate" is an implicit MUST NOT. Make it explicit: "The workspace manager MUST NOT mutate `interrupt_state` except as directed by operator-control per [...] or by reconciliation per [...]."

6. **WM-015 "Each event payload MUST carry `workspace_id`, `run_id`, and the associated branch name"** — payload contract scope leak (see Challenge 6). The MUST is the wrong keyword AND the wrong spec for this rule.

---

## Hidden assumptions

Things the spec assumes without stating:

1. **The worktree-root directory is writable by the daemon's process.** `git worktree add` requires write access. If the repo is on a read-only mount (post-MVH CI use), WM-003 silently fails. No preflight check.

2. **The `run_id` is filesystem-safe.** WM-002 uses `<run_id>` as a directory name. A `run_id` UUID is filesystem-safe, but if post-MVH introduces alternate ID schemes (e.g., monotonic integers, hashed strings), characters like `/` or `:` break the convention. Should be pinned: "run_id MUST match the regex `[A-Za-z0-9-]+` for filesystem safety."

3. **Only one daemon operates on the repo at a time.** The lease-lock model (Challenge 3) implicitly assumes a single-daemon-per-repo world. Post-MVH multi-daemon scenarios (e.g., one per project under a shared monorepo) would need lock-file ownership correlation. Not MVH-blocking; should be an OQ.

4. **`git worktree add` is safe against a worktree already existing at the target path.** WM-003's pseudocode says "IF path already exists: RETURN ERROR." But git itself returns an error on collision; the spec's error is redundant with git's. The real hidden assumption: the startup orphan sweep (WM-033) does not clear a non-stale worktree's lock, so a daemon restart with live runs does NOT re-run `git worktree add` at a path that already has a worktree. Needs explicit "pre-flight check: if path exists with matching run_id, adopt; else error."

5. **`harmonik.meta.json` never conflicts with another tool's sidecar.** The filename is chosen; no requirement says "this file is harmonik-namespaced and MUST NOT be deleted by external tools." Post-MVH integrations that scan `.harmonik/` may race.

6. **Session-log directory's `session_id` is known at sidecar-write time.** WM-026 requires the sidecar BEFORE handler launch. But `session_id` is a per-session identifier — where does it come from before the session exists? S06 presumably generates it. The generation rule is unstated.

7. **Branch names fit within git's ref-name constraints.** `run/<run_id>` with a UUID works; `harmonik/integration/<parent_bead_id>` depends on bead_id format. Bead IDs in Dicklesworthstone/beads_rust are typed strings — could they contain characters invalid in git refs? Not checked.

8. **`workspace_merged` on an integration-branch merge to main is NOT this spec's concern.** WM-007 says "Merge style from integration to main is NOT dictated by this spec." Fine — but WM-021's `workspace_merged` event fires on integration-branch-merges. If main-merges are separate, what event (if any) fires? The spec is silent; the reader may assume `workspace_merged` fires twice (task-to-integration AND integration-to-main), which is probably wrong.

9. **The session-log directory path is stable across handler terminations.** WM-025's `${workspace_path}/.harmonik/sessions/${session_id}/` assumes the directory persists through handler crashes, signals, and retries. No requirement declares post-crash ownership — if a handler crashes mid-write, is the session directory still "owned" by that failed session, or may a restart overwrite it? Reconciliation will need this rule.

10. **The `workspace_id` is treated as opaque by event consumers.** WM-004's derivation `workspace_id = "ws-" + run_id` means `workspace_id` reveals the `run_id` by string inspection. If any downstream consumer (UI, operator tooling, monitoring) treats `workspace_id` as opaque and then starts parsing it, the derivation rule is silently a public contract. The spec should either say "workspace_id MAY be parsed for run_id" or "workspace_id MUST be treated as opaque."

11. **Branch deletion is never discussed.** WM-031 says failed-run branches persist. Post-merge, does the task branch (`run/<run_id>`) get deleted? Git worktrees' branches are typically kept after `git worktree remove`, but this spec never removes worktrees — so the branches pile up. With thousands of runs, `git branch --list` becomes unusable. Minor, but a known post-MVH disk-and-refs-pressure path.

12. **Concurrent sidecar writes.** If a run has two agentic nodes launch in rapid succession (conceptually serial but implementation-raced), both sidecar writes hit `${workspace_path}/.harmonik/sessions/${session_id_A|B}/harmonik.meta.json`. The session_ids differ, so no file collision — but the implicit assumption is "one sidecar write completes before another begins." No requirement enforces this.

---

## Informative prose doing normative work

Content in `> INFORMATIVE:` or plain-prose blocks that actually carries load-bearing rules:

1. **§7.1 INFORMATIVE block (line 468).** "The `interrupt_state` field (§4.10) composes with the lifecycle state; it does NOT replace it. A `leased` workspace interrupted by operator pause is `leased + operator-paused`, not a new lifecycle state." This sentence IS the orthogonality contract — it's the rule consumers route on. Informative framing is wrong. Promote to a requirement (WM-037a) and delete the INFORMATIVE block.

2. **§A.3 "Why interrupt-state is orthogonal to lifecycle state" (lines 604–605).** The argument contains the load-bearing rule "Interrupt events can occur at any lifecycle state" — which Challenge 5 shows is too broad. Rationale prose doing normative work at the same time the §4 requirement leaves the rule incomplete is a reliable soft-spot pattern.

3. **§2.2 "Event payload shapes — owned by event-model.md" (line 46).** Correctly out-of-scope, but WM-015/WM-017/WM-023 violate it (Challenge 6 + Scope leaks). The §2.2 prose declares a boundary the §4 requirements do not respect.

4. **§10.3 "This spec does NOT guarantee performance or throughput bounds on `git worktree add`" (line 556).** Reasonable exclusion, but the spec silently assumes `git worktree add` is fast enough that the `created → ready → leased` transition is not a latency concern. A slow worktree-add (e.g., large repo, network FS) produces a long `created` state during which reconciliation might observe and misdiagnose. The exclusion protects the spec from the performance bound but does not close the reconciliation-race it opens.

---

## Affirmations

Five decisions that hold up under pressure:

1. **Lease-by-run, not lease-by-agent (§4.3, §A.3).** The rationale (lines 599) is rigorous and directly ties to the centralized-controller principle. Gas Town pattern compatibility is preserved without adopting Agent Mail's file-reservation system. The "multiple agents sequentially" framing is the right abstraction — workspaces are passive surfaces, runs are active lease-holders.

2. **Canonical worktree path, no registry (WM-002, WM-013, WM-INV-005, §A.3).** "One source of truth, no registry" is the right call for restart-safety. The daemon can rediscover state by filesystem walk + git + Beads, no extra SQL table to drift.

3. **Three-level branching with small-scope collapse (§4.2, WM-007, WM-008).** Separates harmonik's contract ("one commit per task on integration") from developer policy ("how integration reaches main"). The small-scope collapse for parentless runs is the right shape, even if WM-006/WM-008 disagree on the default (Challenge 8).

4. **Merge-back is a workflow node, not a special workspace (WM-018).** "A design that creates a new workspace for the merge step is forbidden" closes the door on a whole category of accidental complexity. The merge step inherits the lease.

5. **Failed-run worktrees persist (WM-031, §A.3).** The bias toward preservation for audit is correct for MVH, even though disk-pressure bounds need a post-MVH answer (first-plausible-answer finding #3).

6. **§2.2 out-of-scope discipline is mostly honest.** Six of seven exclusions are correctly ceded to their owning specs (handler emission, CASS ingestion, beads writes, reconciliation verdicts, orchestrator loop, operator-control). Only the event-payload exclusion leaks into §4 (Challenge 6). That is better hygiene than most R1 drafts achieve.

7. **Re-run vs intra-run-loop distinction as a mechanical routing rule (§4.9).** Even with Challenge 4's enum-coupling issue, the foundational decision — "the workspace layer does not cognite about re-run vs loop; it reads the reconciliation verdict and routes mechanically" — is right. It keeps cognition in reconciliation where it belongs and mechanism in workspace-model where it belongs. The table shape (WM-036) is the right primitive, even if ownership of the attribute column needs to flip.

8. **Session-log directory + sidecar as the CASS join key (§4.7).** The decision to have S06 (mechanism) stamp the sidecar and S04 (per-handler) write the log contents, with S08 consuming read-only, is a clean three-way separation. The per-session grain for the sidecar (WM-026 uses `session_id`) correctly allows multiple agents' logs to coexist without stomping each other.

---

## Schema audit

Reviewing §6.1 records for completeness, consistency, and match-to-requirements:

1. **`Workspace` record missing `implementer_handler_ref`.** (Line 376–388.) Challenge 2's load-bearing finding: the record needs a field to identify the implementer at merge-pending time. Without it, WM-022's "original implementer" predicate has no machine-readable source.

2. **`Workspace.metadata` as opaque map.** "creation timestamp, operator fingerprint" are the two example keys, but the map is typed `Map<String, String>`. Is this extensible? Are there reserved keys? A schema for the schema would help: either fix the fields as first-class (`created_at`, `operator_fingerprint`) or declare the map schema in a sub-section.

3. **`Workspace.bead_id: String | None`.** (Line 385.) `String` is odd for a typed ID field — execution-model §6.1 declares a `BeadID` type alias. Should be `BeadID | None` for consistency with the cross-spec typed-ID discipline.

4. **`Workspace.parent_commit: String`.** Same comment; could be a `CommitSHA` alias. Less load-bearing than bead_id because commit SHAs are stable string forms, but the typing discipline should be consistent.

5. **`SessionMetadataSidecar.node_id: String`.** (Line 415.) Execution-model §4.8.EM-034a defines `node_id` as namespaced under sub-workflow expansion (`<parent_node_id>/<sub_node_id>`). The sidecar's `node_id` field should carry the namespaced form for runs inside expanded sub-workflows. No requirement says so; Challenge 7's cross-reference gap applies here too.

6. **Missing `WorkspaceLeaseRecord` or equivalent.** The lease itself — per Challenge 3 — has no record schema. Either add a `LeaseFile` RECORD with fields `owning_run_id`, `daemon_fingerprint`, `acquired_at`, or declare the lease as "the existence of `${workspace_path}/.harmonik/lease.lock` containing the run_id."

7. **`ENUM WorkspaceState` contains `setup`** — Challenge 9's retire-candidate.

8. **`ENUM InterruptState` — five values, no routing distinction.** See Definitional gap #8 and first-plausible-answer #7.

9. **§6.2 "Canonical on-disk paths" table missing the integration branch location.** The table shows worktree root, per-run worktree, sessions root, per-session dir, sidecar file, session log, and lease lock — but NOT the integration branch's backing (there isn't one — integration is a branch, not a worktree). A note row would head off the "where does the integration branch live on disk?" question: `| integration branch | n/a (branch, not worktree) | Targeted by task-branch squash-merge per §4.5. No separate worktree is created. |`.

10. **`§6.3 Lifecycle event emission rules`** duplicates WM-015's rule list. Either §6.3 is the normative home and WM-015 cites it, or WM-015 is normative and §6.3 is informative summary — not both. Templates §6.5 says "emitting spec is normative for the when; event-model is normative for the shape" — which reads to me as WM-015 normative and §6.3 redundant. Retire §6.3's redundant content.

11. **`CommitRange` from execution-model not carried.** `Workspace` has `parent_commit` but not `current_tip_commit` or a commit range. Some consumers (reconciliation, improvement loop) will want to know "what commits on the task branch belong to this run" — which is EM-003's CommitRange. Should this spec carry a derived `commit_range: CommitRange` field, or is it always queried live from git?

---

## Recommendation

**Proceed to R2 with specific revisions.** The six challenges above are all addressable by text edits; none require re-architecting the subsystem. The three blocking items:

1. **Challenge 1 (state-machine contradiction)** — must be resolved before the spec can be implemented. Propose collapsing `setup` into `created` and making `ready → leased` the sidecar-write gate, matching WM-016's ordering.

2. **Challenge 2 (original implementer undefined)** — add a record field `implementer_handler_ref` on the workspace, and a direct-to-escalation rule for all-mechanical-merge cases.

3. **Challenge 3 (lease lifetime has no release rule)** — add WM-013a/b/c to name the lock file's birth, death, and re-use constraints.

The four important items (Challenges 4, 5, 6, 8) are revision-cycle-appropriate: rewording rather than new content.

The §5 invariants and §2.2 scope leaks are the reviewer-grade homework that R2 should finish: either rewrite invariants as cross-subsystem claims or retire them; either cite event-model for names/payloads or explicitly move those declarations (Challenge 6).

Estimated R2 delta: ~8–12 requirement edits (3 new, 5 rewrites, 2–4 retired to §4 collapse), 2 schema additions (`implementer_handler_ref`, `lease.lock` record), 1 new invariant on terminal-state orthogonality, 4 new OQs for the co-spec coordinations named above.

No architecture-layer decision needs revisiting. The spec's foundations (lease-by-run, canonical path, three-level branching, orthogonal interrupt-state, failed-run persistence) are all sound; the softness is in seams between normative statements and in scope hygiene, which is normal R1 terrain.

---

## Appendix — cross-spec coordinations R2 needs

Coordinations the R2 author will need to confirm or negotiate with sibling specs:

1. **event-model.md §3.2** — register seven workspace events with payload schemas (see Challenge 6). If event-model has already registered these, verify names match (`workspace_created`, not `workspace_create`, etc.) and payload fields align with WM-015/WM-017/WM-023.

2. **reconciliation.md §9.5** — add `worktree_disposition ∈ {fresh, keep, none}` attribute to each verdict enum entry (see Challenge 4). The current four MVH verdicts need their attributes pinned; the bilateral edit protocol determines which spec owns the attribute.

3. **handler-contract.md** — confirm the `handler_ref` type for the `implementer_handler_ref` field (see Challenge 2). Add handler-contract to §9.1 Depends on.

4. **operator-nfr.md** — confirm the operator-policy knob name for the small-scope-collapse default (see Challenge 8) and for the worktree-root prefix override (WM-002 hidden assumption #6).

5. **execution-model.md §4.8** — add to §9.1 Depends on; confirm sub-workflow expansion inherits the parent's workspace cleanly (see Challenge 7).

6. **beads-integration.md (when drafted)** — confirm `bead_id` type and its string representation (is it git-ref-safe?) for WM-006's `harmonik/integration/<parent_bead_id>` template (see Hidden assumptions #7).

7. **process-lifecycle.md §8.2** — confirm the startup orphan sweep criterion for "stale worktree locks" matches WM-033's declared criterion (see Challenge 3); if the lease lock format is declared here, align.

8. **memory-layer.md (S08, not yet drafted)** — confirm the read-only consumption contract from WM-029 is compatible with the spec's indexing model when it lands.

Each of these is an R2 coordination item, not an architectural blocker. The workspace-model spec can advance past draft with the revisions named in this review, pending final cross-spec cite cleanup once each target spec is reviewed.
