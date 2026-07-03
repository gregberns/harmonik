# Change Design — A. Tool/shell node (hk-l8rpd, KEYSTONE)

> Pass 4 (`change-design`) of `attractor-parity`. Normative design for the tool/shell capability on the existing `non-agentic` node type. Grounded in `03-research/tool-node/findings.md`. Resolves OQ-WG-007 (negative: no new node type) and OQ-4 (exit-code granularity).

## 1. Design decision (summary)

The tool/shell capability is added as **two optional attrs on the existing `non-agentic` node type** — NO new node type, NO schema major bump. A `non-agentic` node carrying `tool_command` is dispatched by a built-in **`shell` handler** that executes the command in the run's worktree with the node's `timeout`, and maps the command's exit state to an Outcome via a mechanism-tagged classifier. A `non-agentic` node WITHOUT `tool_command` keeps today's noop-SUCCESS behavior (start/terminal nodes).

## 2. Attr names + WG-002 rows

Add to `specs/workflow-graph.md` §4 WG-002, the `non-agentic` row's **optional attrs** column:

| attr | type | meaning |
|---|---|---|
| `tool_command` | string | the shell command to execute in the run's worktree. When present, the node is a tool node. |
| `timeout` | duration (seconds, integer) | wall-clock kill bound for the command. Default 300s when absent. |

Amended `non-agentic` row (optional-attr column gains `tool_command`, `timeout`):

    | `non-agentic` | deterministic | `handler_ref` | `tool_command`, `timeout`, `idempotency_class`, `axis_tags`, `budget_ref`, `hook_ref`, `guard_ref` | SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS | `handler_outcome` | [handler-contract.md §4.5 + §4.x shell-handler anchor] |

`handler_ref` resolution (resolves R-A1): a tool node MUST carry `handler_ref="shell"` (the built-in shell handler). This keeps EM-057 item 7's "non-agentic MUST carry handler_ref" invariant intact and matches EM-058's "invoke the handler referenced by handler_ref" dispatch action. EM-057 item 7 (`execution-model.md:1419`) gets a clause: the `shell` handler_ref resolves to the built-in shell handler and consumes the node's `tool_command`/`timeout`. (`tool_command` present but `handler_ref != "shell"` ⇒ design leaves to integration: either a validation error or a documented "tool_command requires handler_ref=shell" rule. Lean: validation warning at v1, error at v2.)

## 3. WG-031 reserved-set addition

Add `tool_command` and `timeout` to the WG-031 reserved set (`workflow-graph.md:388`). Parser gains switch cases in `buildNode` (`parser.go:599-668`) mirroring `policy_ref`/`schema_version` placement discipline: `tool_command`/`timeout` accepted on a node, rejected (reserved-out-of-position strict error) on an edge. Add typed fields `Node.ToolCommand string` and `Node.Timeout string` to `internal/workflow/dot/ast.go` (next to `HandlerRef`).

## 4. Exit-code → Outcome / failure_class mapping (resolves OQ-4)

Mechanism-tagged classifier (no cognition; HC §4.4 `handler-contract.md:379`). Input is the command's exit state; output is a sentinel → `failure_class`:

| Exit state | Outcome.status | Sentinel | Outcome.failure_class | Notes |
|---|---|---|---|---|
| exit 0 | SUCCESS | (none) | (absent) | command succeeded |
| exit non-zero (1..255) | FAIL | `ErrDeterministic` | `deterministic` | **v1 floor.** A deterministic command exiting non-zero exits non-zero again on identical inputs (EM §8.3, MUST NOT retry). Makes kilroy's `condition="outcome=fail && context.failure_class=deterministic"` edges fire. |
| timeout-kill (exceeded `timeout`) | FAIL | `ErrTransient` | `transient` | resolves R-A3: lean `transient` (a longer/cleaner env may pass). The kilroy `loop_guard` circuit-breakers bound the retry, so a `transient` timeout cannot spin. |
| signal-kill (ctx cancel / operator stop / SIGKILL) | FAIL | `ErrCanceled` | `canceled` | EM §8.4. |

**OQ-4 resolution:** `deterministic` floor at v1; author-declared transient exit codes (a `transient_exit_codes` attr) is a **v2 follow-up** (file a follow-up bead). v1 is the bare 0/non-zero → SUCCESS/`deterministic` map plus the timeout/cancel special cases. No `partial_success` from a tool node at v1 (kilroy emits `partial_success` from agentic `box` nodes writing a JSON marker, not from `parallelogram` tool nodes — confirmed in both pipelines).

The classifier is daemon-side (the engine back-fills `failure_class` on FAIL per HC-020 / EM-005c); the tool node does not need to carry a handler-emitted hint. `compilation_loop` and `budget_exhausted` are never tool-node-emitted (F-A3).

## 5. Dispatch-branch design (where in dot_cascade.go)

In `internal/daemon/dot_cascade.go`, the `case core.NodeTypeNonAgentic:` (currently lines 198-203):

    case core.NodeTypeNonAgentic:
        if node.ToolCommand == "" {
            // unchanged: noop start/terminal node
            outcome = core.Outcome{Status: core.OutcomeStatusSuccess}
        } else {
            outcome = dispatchDotToolNode(ctx, deps, runID, wtPath, node)  // NEW
        }

`dispatchDotToolNode` (new function, sibling of `dispatchDotAgenticNode`):
- Resolves timeout (`node.Timeout` parsed to a duration; default 300s).
- Runs `exec.CommandContext(timeoutCtx, "/bin/sh", "-c", node.ToolCommand)` with `cmd.Dir = wtPath` and the daemon's handler env (`deps.handlerEnv`).
- Emits the existing observability events (it sits inside the same per-visit loop, so `node_dispatch_requested` already fired at `dot_cascade.go:193`; add tool-specific events only if event-model requires — likely reuse `agent_output_chunk`-class or a new `tool_command_completed`; flag for integration, NOT a blocker).
- Maps exit state per §4 and returns the Outcome (never an error — a non-zero exit is a FAIL Outcome the cascade routes on, NOT a `nodeErr` that reopens the run; this is the critical difference from the agentic path which returns `nodeErr` on failure).

**Does NOT route through `dispatchDotAgenticNode` / `handler.Launch`** (F-A5 #2): the tool node has no NDJSON stream, `agent_ready`, or claude session. In-process `os/exec` is the implementation; EM-058's "invoke the handler" is satisfied at the spec layer by the built-in shell handler. Flag for integration (R-A2): the shell-handler anchor needs a "built-in deterministic handlers MAY be in-process (no subprocess/socket)" note, since the handler-contract is written assuming a subprocess.

## 6. handler-contract anchor for the shell handler

New anchor in `handler-contract.md` §4 (e.g. §4.x HC-0NN — "Built-in shell handler"). Normative content:
- Invocation: `/bin/sh -c <tool_command>` in the run's worktree (`workspace_path`), with the daemon's handler env, killed at `timeout` (default 300s).
- Boundary-classification tags: `io-determinism = non-deterministic` (shell side effects), `replay-safety = unsafe` (re-running `gh issue create` double-posts) — author declares via `axis_tags` per EM-011; the shell handler's default tags are documented here.
- Emits: a `kind = default` Outcome per EM-005a (§4.1). No `payload`. `failure_class` per the §4 mapping (daemon-classified; the handler does not emit a hint).
- In-process exception: this built-in handler MAY execute in-process (no subprocess, no socket, no `agent_ready`, no progress stream, no silent-hang detection). The §4.2 wire-protocol obligations do NOT apply to it. (R-A2 note.)

## 7. EM-058 dispatch-table note

`execution-model.md:1436` (the `non-agentic` row) gains a sub-note: when the node carries `tool_command` and `handler_ref="shell"`, the built-in shell handler executes the command in the worktree and applies the exit-code → Outcome mapping of [handler-contract.md §4.x] / [workflow-graph.md §4 WG-002]. The dispatch action ("invoke the handler referenced by handler_ref") is unchanged; the sub-note clarifies the shell handler's behavior.

## 8. Backwards compatibility

- **Additive.** New optional attrs (`tool_command`, `timeout`); no existing attr changes name/type/position.
- **Minor schema bump; N-1 readable (WG-034).** No new node type, no new enum member, no new edge field. `schema_version` stays `1`-readable. A `non-agentic` node without `tool_command` behaves exactly as today.
- **Outcome envelope untouched** (EM-005/EM-005a): the tool node emits a `default`-kind Outcome; `failure_class` is the existing additive field (EM-005c).
- **review-loop (v69) unaffected:** the tool node lives entirely in the `dot`-mode non-agentic branch; review-loop mode never reaches it.

## 9. Open items flagged for the integration pass

- I-A1 (R-A1): pin `handler_ref="shell"` requirement + amend EM-057 item 7.
- I-A2 (R-A2): "built-in handlers MAY be in-process" note in the shell-handler anchor; reconcile with HC §4.2 subprocess framing.
- I-A3: tool-node observability events (reuse vs new `tool_command_completed`) — coordinate with event-model.md.
- I-A4: `tool_command` present + `handler_ref != "shell"` — warn at v1, error at v2 (validation rule).
- I-A5 (v2 follow-up): author-declared `transient_exit_codes` attr — file a bead.
