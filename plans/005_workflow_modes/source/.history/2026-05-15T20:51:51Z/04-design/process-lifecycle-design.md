# Process Lifecycle — Change Design (C4)

Scope: daemon-level default `workflow_mode` configuration. This is the lowest-precedence tier in the resolution chain (above only the hardcoded built-in `single`).

## 1. Current state

- `process-lifecycle.md §4.a PL-ENV-001` (lines ~77–144) enumerates the daemon's envelope: events produced, state owned, types introduced. No `workflow_mode` concept appears.
- `§4.1 PL-001..PL-004` (lines ~146–211) defines per-project daemon scope and the daemon-owned file surface under `.harmonik/`. No reference to mode defaults.
- `§4.2 PL-005` step 0 (lines ~215–219) describes the deterministic startup sequence — composition-root bootstrap reads no project-level workflow defaults today; the daemon proceeds mode-blind.
- `PL-018` (line ~444) restates that the daemon is a deterministic Go binary with no LLM logic — workflow-graph semantics are explicitly out of the daemon's responsibility in MVH.
- `PL-028` (line ~562) names the daemon command surface; no `workflow_mode` knob is exposed.

## 2. Target state

Amend `§4.1` (or `§4.2 PL-005` if the project-config-load step is treated as startup-sequence material) to add a new requirement, provisionally `PL-004a — Default workflow mode`:

(a) **Field.** The daemon's project config (the file or section the daemon already reads under `.harmonik/` during PL-005) gains an optional `workflow_mode` field with enum domain `{single, review-loop, dot}` and a built-in default of `single`. (b) **Load timing.** The field is read once during PL-005 composition-root bootstrap (or step 8a, alongside other startup markers); it is immutable for the daemon's lifetime. Mid-run mode change is FORBIDDEN. (c) **Precedence position.** The daemon-level value is the **second-lowest** tier in the four-tier resolution chain owned by execution-model (see `execution-model-design.md` §2); below it sits only the built-in fallback `single`. (d) **No override of higher tiers.** The daemon default MUST NOT override per-project (if a separate project-policy tier is added later) or per-task settings.

Amend `PL-ENV-001 (e) State owned` to list `workflow_mode_default` (in-memory enum, observable via `harmonik status`) as daemon-owned state.

Amend `PL-018` ("daemon is deterministic Go") with a clarifying sentence: setting a default workflow mode is a configuration affordance, not LLM logic; mode dispatch (graph walking, agent role selection) lives in handler-contract / execution-model, not in the daemon's lifecycle code.

No new events. No change to `PL-028` command surface — operators read the value via `harmonik status` (passive surface), not via a control command (per locked decision: mode is NOT operator-controllable at runtime).

## 3. Rationale

- Satisfies success criterion §1 of the problem space (daemon config supports `workflow_mode`, defaults to `single`).
- The daemon-level tier exists to make a fleet-wide policy choice (e.g., "all bare tasks in this project default to review-loop") without forcing every bead to carry a `workflow:` label.
- Bootstrapping mode at daemon startup, not per-claim, preserves PL-018's "no LLM logic in the daemon lifecycle" framing — the daemon stores an enum, nothing more.

## 4. Requirements traceability

| Req (02-components.md C4) | Target-state element |
|---|---|
| Config field `{single, review-loop, dot}`, default `single` | §2 (a) |
| Read once at startup; immutable | §2 (b) |
| Lowest-precedence tier above built-in fallback | §2 (c) |
| Bootstrap remains mode-agnostic | §2 (d) and PL-018 clarification |
| Not operator-controllable mid-run | §2 (b), no `PL-028` change |

## 5. Open decisions remaining for spec-draft pass

- **Co-location of the field.** Whether `workflow_mode_default` lives in an existing project-config file the daemon already loads (which one?) or warrants a new field under `.harmonik/daemon.config` (no such file exists today). Spec-draft picks the host file; this is a small placement decision, not a design decision.
- Renumbering: whether the new requirement is `PL-004a` or fits under `§4.2 PL-005` step 8a. Editorial.
