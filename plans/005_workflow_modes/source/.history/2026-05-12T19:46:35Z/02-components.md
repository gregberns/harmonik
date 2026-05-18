# Workflow Modes — Decompose

Seven existing specs are affected; no new spec file is required. No conflicts with locked-in rules. No `workflow_mode` or equivalent concept exists in any current spec — this is novel naming, no prior-field collision.

Each section below names: affected sections, required spec additions/amendments (described as what the spec should state after the change, not how to write it), constraints from the existing spec, and dependencies on other spec changes.

---

## C1. Handler Contract — `specs/handler-contract.md`

**Affected sections:** §4.2 (LaunchSpec, HC-006), §4.1 (handler selection, HC-003), §4.3 (concurrency model, HC-011–HC-016a).

**Required content after change:**
- LaunchSpec gains an optional `workflow_mode ∈ {single, ralph, dot}` field. When absent the daemon resolves it per execution-model's precedence rule (task → project → daemon → `single`).
- LaunchSpec gains an optional `phase` field (or equivalent) distinguishing implementer from reviewer launches within a ralph cycle. The handler interface itself remains mode-agnostic — phase routing is the daemon's job, not the adapter's.
- HC-004's idempotency-key contract extends to `(run_id, node_id, phase, iteration)` for ralph mode, so each phase launch is independently idempotent.
- HC-003's "handler type is config-level" principle is preserved; mode selection is **dispatch-level**, not handler-selection-level. The spec should explicitly draw this distinction so future readers don't read mode as a handler-selector.

**Constraints:**
- Adapter surface (HC-013) does not expand. Watcher and adapter remain mode-agnostic.
- HC-003's config-level handler selection rule must remain intact.

**Dependencies:** Drives execution-model (C2) and event-model (C3). Should land before or alongside C2.

---

## C2. Execution Model — `specs/execution-model.md`

**Affected sections:** §4.3 (Run model, EM-012–EM-015c), §4.1 (core types, EM-001–EM-005a), §6.1 (Go type def for Run).

**Required content after change:**
- The Run record carries `workflow_mode ∈ {single, ralph, dot}`. Mode is resolved at claim time and immutable for the run's lifetime.
- The spec states the **mode-resolution precedence rule** explicitly: per-task setting (from the bead's metadata, per C5) → project-level config → daemon-level default (per C4) → built-in default `single`.
- Ralph mode is described as a hardcoded two-node sub-case of the general workflow-graph model: `implementer → reviewer → {APPROVE: close, REQUEST_CHANGES: implementer, BLOCK: close-needs-attention, iteration-cap: close-needs-attention}`. The spec names this graph but does not require a graph-walker implementation — that arrives with `dot`.
- The Run's `context` map (or equivalent) carries per-iteration state for ralph: `iteration_count`, `last_verdict`, and the implementer's `session_id` for resume.
- The Run keeps a **single `run_id`** across all ralph iterations. Each implementer or reviewer launch within that run gets its own `session_id`. Iterations are distinguished by `iteration_count` and `phase`, not by separate run IDs.

**Constraints:**
- EM-015's intra-run loop semantics remain valid; ralph fits within them as a special case.
- `run_id` stability is a hard rule (downstream tooling assumes one run = one ID).

**Dependencies:** Depends on C1 (LaunchSpec field). Drives C3 (event-model uses these fields), C5 (beads-integration must surface the per-task setting), C4 (daemon default flows up into this resolution).

---

## C3. Event Model — `specs/event-model.md`

**Affected sections:** §8.1 (run-lifecycle events), §8.3 (agent/handler lifecycle events), §6.3 (payload shapes).

**Required content after change:**
- Five new event types in §8.1:
  - `implementer_resumed` (class O). Payload includes `run_id`, `session_id`, `iteration_count`, prior verdict summary.
  - `reviewer_launched` (class O). Payload includes `run_id`, `session_id`, `iteration_count`.
  - `reviewer_verdict` (class O). Payload conforms to the existing `agent-reviewer` JSON schema v1 (`schema_version`, `verdict ∈ {APPROVE, REQUEST_CHANGES, BLOCK}`, `flags[]`, `notes`). Also includes `run_id`, `session_id`, `iteration_count`.
  - `iteration_cap_hit` (class O). Payload includes `run_id`, `iteration_count`, `cap_value`, `final_verdict`.
  - `ralph_cycle_complete` (class F). Payload includes `run_id`, `final_iteration_count`, `completion_reason ∈ {approved, cap_hit, blocked, error}`.
- All five events carry `run_id` per the event-model join-key contract.
- A note in the spec records the **alternative considered and rejected** (or to be decided in design pass): folding implementer/reviewer launch events into existing `outcome_emitted` with a `role` field. v1 prefers distinct event types for legibility; the role-field alternative is captured as a future refactor option.

**Constraints:**
- Per problem-space, one `run_id` umbrella for the entire ralph cycle. The event-model spec must reflect this (no multi-`run_id`-per-task semantics at MVH).
- `reviewer_verdict` payload schema must match the existing agent-reviewer schema verbatim — no parallel schema is introduced.

**Dependencies:** Depends on C2 (run_id stability rule) and C1 (LaunchSpec phase field, for `reviewer_launched`).

---

## C4. Process Lifecycle — `specs/process-lifecycle.md`

**Affected sections:** §4.a (subsystem envelope, PL-ENV-001) or §4.1 (whichever owns daemon config schema).

**Required content after change:**
- The daemon configuration schema includes a `workflow_mode` field with values `{single, ralph, dot}` and a default of `single`.
- The spec names this field as the **lowest tier** of the resolution precedence (above only the built-in fallback). It is read once at daemon startup (PL-005); it is immutable for the daemon's lifetime.
- The spec is explicit that this config does **not** override task-level settings — it only fills in when no higher-precedence setting exists.

**Constraints:**
- No mid-run mode change; not operator-controllable at runtime (locked in by C7).
- Bootstrap remains mode-agnostic; nothing about the daemon's lifecycle depends on the mode value at startup, only at dispatch time.

**Dependencies:** Drives C2's precedence rule. Independent of others.

---

## C5. Beads Integration — `specs/beads-integration.md`

**Affected sections:** §4.3 (beads-managed data, BI-005–BI-009), §4.6 (bead-ID propagation, BI-017–BI-020).

**Required content after change:**
- A bead MAY carry an optional `workflow_mode` setting (label, metadata field, or label-prefix — exact encoding decided in design pass C5-D). When present, this is the highest-precedence setting in the resolution chain.
- The Beads adapter (BI-004) reads this field on every ready-work query and surfaces it in the ready-work response payload.
- Per BI-004's write-discipline rule, agents MUST NOT modify the mode setting; only operators or the daemon (where workflow design intent so dictates) write it. The spec should restate this rule against the new field explicitly.
- BI-020's sidecar metadata MAY carry the resolved `workflow_mode` for audit/observability.

**Constraints:**
- Adapter writes must not silently mutate the mode.
- If Beads doesn't support arbitrary metadata, label-prefix encoding (`workflow:ralph`) is the fallback. Decided in design pass.

**Dependencies:** Drives C2 (highest-precedence input). Drives reader path of C4 (daemon reads, then resolves).

---

## C6. Workspace Model — `specs/workspace-model.md`

**Affected sections:** §4.3 (lease model, WM-010–WM-013e), WM-011 specifically.

**Required content after change:**
- WM-011's "one active agent at a time, agents run sequentially" rule already permits ralph's pattern (implementer paused → reviewer runs → reviewer exits → implementer resumes). The spec adds an example or note naming ralph-mode as a concrete instance.
- WM-013a's lease-lock semantics are preserved: a single lease covers the entire run lifetime, spanning all ralph iterations. Sessions come and go; the lease does not.
- WM-030's session-log directories accumulate per-session under `${workspace}/.harmonik/sessions/<session_id>/` — multiple session directories per workspace is already valid; ralph just exercises that path more.

**Constraints:**
- No new state in the workspace state machine.
- Lease ownership is per-run, not per-session — must be stated explicitly so future readers don't infer per-session leases from ralph mechanics.

**Dependencies:** Trivial clarification; can land last.

---

## C7. Operator NFR — `specs/operator-nfr.md`

**Affected sections:** §4.1 (exit-code taxonomy, ON-001–ON-004), §4.3 (operator-control semantics, ON-007–ON-013c).

**Required content after change:**
- ON-004's config-inventory obligation extends: operator-visible inventory must list the daemon's `workflow_mode` default and the precedence layers below/above it.
- Operator visibility into ralph cycles is via the existing JSONL event consumption path (`harmonik status`, `harmonik logs`, `jq`). No new command surface.
- Mode is **not** operator-controllable at runtime — explicitly named as a non-control surface so operators don't mistake it for a tunable.
- ON-002's exit-code taxonomy may gain a new category for `iteration-cap-exceeded` or fold this into an existing failure category. Decided in design pass C7-D; MVH default is "fold into existing `needs-attention` close path."

**Constraints:**
- Ralph iteration cap (3 at MVH) is **not** operator-tunable, per problem-space non-goal.
- Mode-change-mid-run is **not** an operator action.

**Dependencies:** Depends on C2 (run model), C3 (events for observability), C5 (beads label visible to operator queries).

---

## Goal-to-Component Coverage

Each problem-space success criterion is covered:

| Goal | Covered by |
|------|------------|
| 1. Daemon config supports `workflow_mode` field, default `single` | C4 |
| 2. Per-task override; resolution order | C2, C5 |
| 3. Ralph lifecycle defined in Handler Contract | C1, C2 |
| 4. New ralph-cycle events in Event Model | C3 |
| 5. Reviewer JSON contract documented; reuses agent-reviewer schema | C3 |
| 6. Smoke test: full ralph success path | (tasks pass, not a spec change) |
| 7. Smoke test: iteration cap path | (tasks pass, not a spec change) |
| 8. Adding `dot` later is one branch, not a refactor | C1, C2 (precedence rule already accommodates) |

---

## Dependency Order for Drafting

Spec drafts in this order to minimize re-work:

1. **C4** (daemon config) — independent.
2. **C5** (beads label) — independent.
3. **C2** (execution-model Run field + precedence rule + ralph sub-case) — depends on C4 + C5.
4. **C1** (LaunchSpec field + phase) — depends on C2.
5. **C3** (event types) — depends on C1 + C2.
6. **C6** (workspace clarification) — depends on nothing; trivial.
7. **C7** (operator visibility) — depends on C2 + C3 + C5.

---

## Open Decisions Carried Into Research / Design Passes

- **C5-D**: Beads encoding — label-prefix (`workflow:ralph`) vs metadata field vs bead-type prefix. Research pass should confirm what Beads CLI supports today and what `br` flags exist for the chosen encoding.
- **C3-D**: Distinct event types (`implementer_resumed`, `reviewer_launched`, ...) vs reusing `outcome_emitted` with a `role` field. Design pass weighs legibility vs schema bloat. v1 recommendation: distinct types.
- **C7-D**: Iteration-cap-exceeded as a distinct exit-code category, or fold into `needs-attention`. Design pass.
- **Mechanism**: How exactly does the daemon resume a Claude implementer session? `claude --resume <session-id>`? Some other flag? Research pass confirms by reading Claude Code CLI docs or experimenting. This is the single highest-risk piece of ralph mechanics.
