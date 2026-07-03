# Spec Draft — A. Tool/shell node (component `tool-node`, hk-l8rpd, KEYSTONE)

> Pass 5 (`spec-draft`) of `attractor-parity`. Grounded in `04-design/tool-node-design.md`.
> This draft is **component-scoped** (not target-spec-file-scoped): the tool-node, inline-prompt, and non-committing components all amend `workflow-graph.md` and `handler-contract.md`, so each component's draft states the precise per-section normative text it contributes; the integration pass (06) merges the three components' edits into the full updated target files. Each amendment below names the exact target spec file, section, and the verbatim normative text to splice in.
>
> All changes are ADDITIVE: minor `schema_version` bump (stays `1`-readable per WG-034), no new node type, no new enum member, no new edge field, Outcome envelope and edge cascade unchanged, v69 review-loop path unaffected.

---

## Amendment A1 — `workflow-graph.md` §4 WG-002: amend the `non-agentic` row

In `specs/workflow-graph.md` §4 WG-002 (the node-type catalog table), the `non-agentic` row's **optional attrs** column gains `tool_command` and `timeout`. The amended row reads:

    | `non-agentic` | deterministic | `handler_ref` | `tool_command`, `timeout`, `idempotency_class`, `axis_tags`, `budget_ref`, `hook_ref`, `guard_ref` | SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS | `handler_outcome` | [handler-contract.md §4.5] |

No other row changes. The `required attrs` column on this row is unchanged (`handler_ref` remains required — see Amendment A4).

## Amendment A2 — `workflow-graph.md` §4: new requirement WG-039 (`tool_command` / `timeout` attr semantics)

Insert a new normative requirement after WG-008 in §4 of `specs/workflow-graph.md`. (ID `WG-039` is **provisional and COLLIDES** with components D/E, which also claimed `WG-039`/`WG-040`/`WG-041` for `model`/`effort`/`goal`/substitution. The integration pass MUST assign a final non-colliding sequential WG ID and update every reference to it — including the HC-063 and EM-057/EM-058 amendments below — plus §14 cross-references.)

> ### WG-039 — Tool-command attributes on `non-agentic` nodes
>
> A `non-agentic` node MAY carry a `tool_command` (string) optional attribute. When `tool_command` is present, the node is a **tool node**: at dispatch time the run executes `tool_command` as a shell command in the run's worktree per the built-in `shell` handler of [handler-contract.md §4.x HC-063], and the command's exit state is mapped to an Outcome per [handler-contract.md §4.x HC-063] / §7 WG-017.
>
> A `non-agentic` node MAY carry a `timeout` (integer, seconds) optional attribute. `timeout` is the wall-clock kill bound for the command; when absent, the loader applies a default of `300` seconds. `timeout` is only meaningful on a node that also carries `tool_command`; a `timeout` attribute on a `non-agentic` node without `tool_command` is retained in the AST and ignored.
>
> A `non-agentic` node WITHOUT `tool_command` is unchanged from prior behavior: it carries no tool semantics and its handler dispatch is governed by the §4 WG-002 `non-agentic` row and [handler-contract.md §4.1].
>
> A tool node MUST carry `handler_ref="shell"` (the built-in shell handler of [handler-contract.md §4.x HC-063]). The `handler_ref="shell"` requirement satisfies the §4 WG-002 `non-agentic` row's required-`handler_ref` obligation and [execution-model.md §7.5.3 EM-057] item 7. A `tool_command` present on a node whose `handler_ref` is not `shell` is a validation warning at v1 (the loader emits the §10 WG-031 warning event and retains the node); it is reserved to become a strict error at the next schema major bump.
>
> **Trust boundary (normative).** `tool_command` is a literal shell string supplied by the `.dot` author. The `.dot` author is a trusted operator. At v1 the value is passed verbatim to `/bin/sh -c`; there is NO sandboxing, NO argument escaping, and NO allow-list of permitted commands. A workflow that admits an untrusted `.dot` author admits arbitrary command execution in the run's worktree under the daemon's privileges. Operators MUST treat `.dot` artifacts as trusted code, equivalent to a checked-in shell script.
>
> Tags: mechanism, normative

## Amendment A3 — `workflow-graph.md` §10 WG-031: add `tool_command` and `timeout` to the reserved set

In `specs/workflow-graph.md` §10 WG-031, the "Strict positions — reserved attribute name used outside its declared position" bullet's reserved-set list gains `tool_command` and `timeout`. The amended reserved-set sentence reads (additions in the natural insertion point alongside the other node-attr names):

> The reserved set at v1.0 is: `type`, `agent_type`, `handler_ref`, `gate_ref`, `sub_workflow_ref`, `workflow_version`, `input_mapping`, `idempotency_class`, `axis_tags`, `tool_command`, `timeout`, `policy_ref` (reserved-and-rejected name; see [control-points.md §4.12 CP-056]), `hook_ref`, `guard_ref`, `budget_ref`, `skills_ref`, `freedom_profile_ref`, `schema_version`, `version`, `condition`, `preferred_label`, `weight`, `ordering_key`, `start_node`, `terminal_node_ids`, `context_keys` (graph-level per [handler-contract.md §5.6 HC-062]; see WG-031a).

Effect: a loader accepts `tool_command` / `timeout` on a node and rejects either name appearing on an edge (or any non-node position) as a reserved-attribute-out-of-position strict error per WG-031.

## Amendment A4 — `workflow-graph.md` §9 WG-024: tool-node attribute validation

In `specs/workflow-graph.md` §9 WG-024 (reserved-attribute strictness), append a bullet to the checked-at-parse-time list:

> - On a `non-agentic` node carrying `tool_command`, `handler_ref` MUST equal `shell`. A `tool_command` on a node whose `handler_ref` resolves to any other handler is a warning at v1 per §10 WG-031 (not a strict error); the constraint is reserved to become strict at the next schema major bump. `timeout`, when present, MUST be a non-negative integer; a non-integer or negative `timeout` is a strict error.

## Amendment A5 — `handler-contract.md` §4.x: new requirement HC-063 (built-in `shell` handler)

Insert a new normative requirement at the end of §4.1 of `specs/handler-contract.md` (after HC-004), or as a clearly-anchored §4.1a. (ID `HC-063` is the next free HC ID; HC-062 is the highest assigned.)

> #### HC-063 — Built-in `shell` handler for tool nodes
>
> The `shell` handler is a built-in deterministic handler bound by the reserved `handler_ref="shell"` per [workflow-graph.md §4 WG-039]. It dispatches a `non-agentic` tool node by executing the node's `tool_command` and mapping the command's exit state to an `Outcome` per [execution-model.md §4.1 EM-005].
>
> **Invocation.** The `shell` handler executes `/bin/sh -c <tool_command>` with the working directory set to the run's `workspace_path` (the worktree per [workspace-model.md §4.1]) and the daemon's handler environment. The command is killed if it exceeds the node's `timeout` (default `300` seconds per [workflow-graph.md §4 WG-039]).
>
> **In-process exception (normative).** The `shell` handler is a built-in deterministic handler and MAY execute IN-PROCESS within the daemon: it has no agent subprocess, no NDJSON progress stream (§4.2 HC-007 / HC-007a), no `agent_ready` signal (§4.9 HC-039), no heartbeat (§4.6 HC-026a), and no silent-hang detection (§4.6 HC-026). The §4.2 wire-protocol obligations (HC-005 through HC-010), the §4.9 ready-state obligations, and the §4.6 silent-hang obligations DO NOT apply to the `shell` handler. The `timeout` kill-bound replaces silent-hang detection as the liveness guard. This is the sole built-in-handler exception at v1; all agent-dispatching handlers remain subprocess-and-socket-bound per §4.2 HC-007.
>
> **Outcome.** The `shell` handler emits an `Outcome` with `kind = default` per [execution-model.md §4.1 EM-005a]; no `payload`. The handler does NOT emit a `failure_class` hint; classification is daemon-side per §4.5 HC-020 and the exit-state mapping below (the daemon back-fills `failure_class` on FAIL per HC-059 / [execution-model.md §4.1 EM-005c]). The exit-state → Outcome mapping is:
>
> | Exit state | `status` | `failure_class` |
> |---|---|---|
> | exit 0 | `SUCCESS` | (absent) |
> | exit non-zero (1..255) | `FAIL` | `deterministic` |
> | timeout-kill (exceeded `timeout`) | `FAIL` | `transient` |
> | signal-kill (context cancel / operator stop / SIGKILL) | `FAIL` | `canceled` |
>
> A non-zero exit is a `FAIL` `Outcome` the cascade routes on per [execution-model.md §4.10 EM-041]; it is NOT a daemon-side error that reopens the run. Author-declared per-command transient exit codes are reserved for a future schema version and are NOT supported at v1 (every non-zero exit is `deterministic`). The `shell` handler never emits `structural`, `budget_exhausted`, `compilation_loop`, or `partial_success`.
>
> **Boundary-classification tags.** The `shell` handler's default four-axis tags per [execution-model.md §4.2 EM-011] are `io-determinism = non-deterministic` (shell commands have side effects) and `replay-safety = unsafe` (re-running a side-effecting command may double-apply). A tool node's author MAY declare tighter `axis_tags` per WG-039 when the specific command is known-idempotent.
>
> **Trust boundary.** Per [workflow-graph.md §4 WG-039], `tool_command` is a literal shell string from the trusted `.dot` author; the `shell` handler performs no sandboxing or escaping at v1.
>
> Tags: mechanism, normative

## Amendment A6 — `handler-contract.md` §4.2 HC-005: in-process reconciliation note

In `specs/handler-contract.md` §4.2 (the wire-protocol section preamble or HC-005), append a clarifying clause noting that the §4.2 obligations are subprocess-handler obligations and that the built-in `shell` handler of HC-063 is exempt:

> The wire-protocol obligations of this section (HC-005 through HC-010) apply to handlers that launch an agent subprocess. Built-in deterministic handlers MAY execute in-process and are exempt as declared by their own requirement; the only such handler at v1 is the `shell` handler of §4.1 HC-063, which has no subprocess, no socket, and no progress stream. No other handler is exempt from §4.2 at v1.

## Amendment A7 — `handler-contract.md` §4.2a HC-058: amend the `non-agentic` (tool-style) row

In `specs/handler-contract.md` §4.2a HC-058 (per-node-type Outcome emission obligations table), the `non-agentic` (tool-style) row is reconciled with the built-in `shell` handler. The existing row already permits `SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS` with `default`-only payload kind; append a note clause to the row's `failure_class` cell:

> For a tool node dispatched by the built-in `shell` handler of §4.1 HC-063, `failure_class` is daemon-classified from the command's exit state per HC-063 (`deterministic` on non-zero exit, `transient` on timeout, `canceled` on signal); the `shell` handler emits no hint. The `shell` handler emits only `SUCCESS` or `FAIL` (never `RETRY` or `PARTIAL_SUCCESS`) at v1.

## Amendment A8 — `execution-model.md` §7.5.4 EM-058: amend the `non-agentic` dispatch-table row

This is the **keystone reconciliation**. In `specs/execution-model.md` §7.5.4 EM-058 (the normative dispatch table for `dot` node types), the `non-agentic` row currently asserts dispatch "via the handler referenced by the node's `handler_ref` ... Same dispatch path as `agentic` at the spec layer". The current code (`internal/daemon/dot_cascade.go:198-203`) instead synthesizes a `SUCCESS` Outcome in-process for a `non-agentic` node with no handler. The amendment reconciles code and spec by splitting the row's behavior on the presence of `tool_command`. The amended `non-agentic` row reads:

    | `non-agentic` | When the node carries `tool_command` and `handler_ref="shell"`: the built-in `shell` handler of [handler-contract.md §4.1 HC-063] executes the command in the run's worktree and applies the exit-state → Outcome mapping of HC-063 / [workflow-graph.md §4 WG-039]. The `shell` handler MAY run in-process (no subprocess, no socket) per the HC-063 exception. When the node carries no `tool_command` (start / terminal / pass-through node), the engine synthesizes a `SUCCESS` Outcome without dispatching a handler. Otherwise (a non-agentic node bound to a non-`shell` handler), invoke the handler referenced by the node's `handler_ref` per [handler-contract.md §4.1]; handler-internal determinism is the handler's responsibility per the node's four-axis tags (§4.2.EM-011). | §4.1.EM-005 `Outcome` with `kind = default` per §4.1.EM-005a. |

Append a sub-note to EM-058 below the table (after the existing "load-bearing for implementer epics" paragraph):

> **Non-agentic dispatch sub-note.** The `non-agentic` row admits three dispatch paths distinguished by node content: (1) a tool node (`tool_command` + `handler_ref="shell"`) runs the built-in `shell` handler per [handler-contract.md §4.1 HC-063], which MAY execute in-process; (2) a start / terminal / pass-through node (no `tool_command`) is synthesized to `SUCCESS` without a handler dispatch; (3) any other `non-agentic` node invokes its bound `handler_ref` via the handler registry exactly as `agentic` does. The "invoke the handler referenced by `handler_ref`" dispatch action of the prior EM-058 row is preserved for path (3); paths (1) and (2) are the spec-layer reconciliation of the in-process behavior at `internal/daemon/dot_cascade.go`.

## Amendment A9 — `execution-model.md` §7.5.3 EM-057 item 7: `shell` handler_ref clause

In `specs/execution-model.md` §7.5.3 EM-057 item 7 (required attributes by node type), the `non-agentic` sub-bullet currently reads: "`non-agentic` nodes MUST carry `handler_ref` resolving to a handler registered per [handler-contract.md §4.1]." Append a clause:

>    - `non-agentic` nodes MUST carry `handler_ref` resolving to a handler registered per [handler-contract.md §4.1]. (This obligation is the §4.2.EM-007 amendment per EM-060.) When the node is a tool node (carries `tool_command` per [workflow-graph.md §4 WG-039]), `handler_ref` MUST be `shell`, resolving to the built-in `shell` handler per [handler-contract.md §4.1 HC-063]; the validator MAY emit a warning rather than fail when `tool_command` is present with a non-`shell` `handler_ref` at v1 (reserved to become a validation failure at the next schema major bump).

The mandatory-`handler_ref` invariant of EM-057 item 7 is preserved: a tool node still carries `handler_ref` (pinned to `shell`).

---

## Cross-component reconciliation flagged for integration (06)

- **I-A-shared-table (with components B, C):** Components A, B, and C each amend `workflow-graph.md` §4 WG-002 and §10 WG-031 and `handler-contract.md`. The integration pass MUST merge: WG-002 `agentic` row gains `prompt` (B) + `non_committing` (C); WG-002 `non-agentic` row gains `tool_command` + `timeout` (A); WG-031 reserved set gains all four names. Produce ONE updated WG-002 table and ONE updated WG-031 reserved-set sentence.
- **I-A-newids:** WG-039 (A), the B and C new WG requirements, and HC-063 (A) must receive non-colliding IDs in the merged files; integration assigns the final sequential IDs and fixes §14 / cross-reference tables.
- **I-A-events (R-A2 / I-A3):** tool-node observability events (reuse `agent_output_chunk`-class vs a new `tool_command_completed`) coordinate with `event-model.md`. NOT a blocker for the normative text above; flagged.
- **I-A-validation (I-A4):** the warn-at-v1 / error-at-v2 rule for `tool_command` + non-`shell` `handler_ref` is stated in WG-024, WG-039, and EM-057 item 7; integration must keep the three statements consistent.
- **I-A-v2 (I-A5):** author-declared `transient_exit_codes` is a v2 follow-up bead, out of scope for this draft.
