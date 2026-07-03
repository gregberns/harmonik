# Research — A. Tool/shell node (hk-l8rpd, KEYSTONE)

> Pass 3 (`research`) of `attractor-parity`. Component A per `02-components.md`. The tool/shell capability **rides the existing `non-agentic` node type** (per OQ-WG-007 / D17) — NO new node type. Adds `tool_command` + `timeout` node attrs, a real exec dispatch branch, and an exit-code → Outcome/failure_class mapping.

## Research questions

- RQ-A1. Where exactly does the non-agentic dispatch happen today, and what does it produce?
- RQ-A2. What does `workflow-graph.md` WG-002 require/permit on the `non-agentic` row, and what is the reserved-attr discipline (WG-031) for new dispatcher-consumed attrs?
- RQ-A3. How does the failure taxonomy (EM §8 / HC §4.5) want an exit code classified, given the "mechanism-tagged, no cognition" constraint?
- RQ-A4. What attr+semantics do the live kilroy pipelines actually require of a tool node?
- RQ-A5. Is the change a clean additive change, or does anything touch a load-bearing abstraction?

## Findings

### F-A1 — The non-agentic branch synthesizes SUCCESS and runs nothing (RQ-A1)

`internal/daemon/dot_cascade.go:198-203`, `case core.NodeTypeNonAgentic:` — the entire body is:

    outcome = core.Outcome{Status: core.OutcomeStatusSuccess}

There is no handler dispatch, no command execution, no exit-code path. The comment (lines 199-201) names this as the `noop` start/terminal case. The cascade then runs `workflow.DecideNextNode` on that synthesized SUCCESS and follows the single outbound edge. **This is the exact dispatch point the tool node must extend** with a real exec branch (branch within the case on whether `tool_command` is present; absent ⇒ keep the noop-SUCCESS behavior for start/terminal nodes).

Evidence the parser already retains the needed attrs: `internal/workflow/dot/parser.go:660-666` — any non-reserved node attribute lands in `node.UnknownAttrs[key]` with a permissive WG-031/032 warning. So `tool_command="..."` and `timeout="120"` are *already parsed today* into `node.UnknownAttrs`, exactly like `traversal_cap` is on edges (`parser.go:699-702`). The dispatch branch can read `node.UnknownAttrs["tool_command"]` with zero parser change — but see F-A2: to make them strict-position (dispatcher-consumed), they SHOULD get switch cases + WG-031 reserved-set entries.

### F-A2 — WG-002 `non-agentic` row + WG-031 reserved-set discipline (RQ-A2)

`specs/workflow-graph.md:86` — the `non-agentic` row today:

    | `non-agentic` | deterministic | `handler_ref` | `idempotency_class`, `axis_tags`, `budget_ref`, `hook_ref`, `guard_ref` | SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS | `handler_outcome` | [handler-contract.md §4.5] |

Required attr is `handler_ref`; legal statuses already include SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS — **so a tool node can emit FAIL with no schema change to the status enum.** The change is: add `tool_command` and `timeout` to the optional-attr column.

WG-031 reserved set (`workflow-graph.md:388`) is the closed list of names that are strict-position. New dispatcher-consumed attrs SHOULD be added to it so a misplaced `tool_command` (e.g. on an agentic node or an edge) is an *ingest error* rather than a silently-ignored permissive attr. Parser pattern to follow: the `schema_version`-on-node strict error (`parser.go:652-659`) and the `policy_ref` reserved-and-rejected case (`parser.go:644-651`) show how a reserved-position violation is emitted as a `ParseError`.

N-1 readability (WG-034, `workflow-graph.md:436`): adding an *optional* attr is an additive minor bump that "MUST NOT break readers at the prior minor version." No new node type, no new enum member, no new edge field ⇒ `schema_version` stays `1`-readable. Confirmed clean.

OQ-WG-007 (`workflow-graph.md:497`) explicitly anticipates this: "If a `tool` node type is added in a future schema version (currently rejected by the closed-set enum of §4 WG-001), its handler contract is pending. Tracked as D17." The decompose decision is to **not** add a node type — so OQ-WG-007's "tool node type" framing is resolved in the *negative* (no new type) and replaced by "tool capability on the existing `non-agentic` type."

### F-A3 — Exit-code → failure_class must be mechanism-tagged (RQ-A3)

EM §8 (`execution-model.md:1456-1475`) and HC §4.4 (`handler-contract.md:379`, "Classifier MUST be deterministic from structured fields (exit code, event payload flags, typed adapter return). No cognition MAY participate"). Six-class enum: `transient`, `structural`, `deterministic`, `canceled`, `budget_exhausted`, `compilation_loop`.

The classifier's normative input is the **handler-returned `ErrX` sentinel** (`execution-model.md:119`, `:1475`; `handler-contract.md:354-361` — `ErrTransient`, `ErrStructural`, `ErrDeterministic`, `ErrCanceled`, `ErrBudget`), NOT fields of the Outcome. So the tool node's mapping is an **exit-code → sentinel → failure_class** chain. The clean v1 mapping:

| Observed | Sentinel | failure_class | Rationale |
|---|---|---|---|
| exit 0 | (none — SUCCESS) | n/a | command succeeded |
| exit non-zero | `ErrDeterministic` | `deterministic` | a deterministic shell command that exits non-zero exits non-zero again on identical inputs; MUST NOT retry (EM §8.3). Correct *floor* default. |
| timeout-kill | `ErrTransient` (lean) | `transient` | design decides; lean `transient` (a longer-running env may pass). Alternative `canceled`. |
| signal-kill (ctx cancel / SIGKILL) | `ErrCanceled` | `canceled` | matches EM §8.4. |

**Why `deterministic` (not `structural`) is the non-zero floor:** EM §8.2 `structural` means "retry only after an approach change — typically an edge routes to a re-planning node." A failing pytest / `make ci` IS the signal a re-plan node (the kilroy `fix_*` boxes) keys on. But the *classification* of a bare non-zero exit is "this command, run again unchanged, fails again" = `deterministic` (no-retry). The kilroy graphs route `outcome=fail` to `loop_guard` → `fix` on an unconditional fallback edge AND carry class-specific edges (`condition="outcome=fail && context.failure_class=deterministic"`). So `deterministic` is the floor that makes those class-specific edges fire; a `transient` classification would wrongly license a blind retry of a deterministically-failing command. **Decision: non-zero → `deterministic`.**

Note: a tool node never emits `compilation_loop` (daemon-observed at traversal-cap-hit, `execution-model.md:1469/:1475` — handler not consulted) and never emits `budget_exhausted` (pre-launch denial). So the tool node's classifier is a 3-way (or 4-way with timeout) map: SUCCESS / `deterministic` / `canceled` (+ optional `transient` on timeout).

### F-A4 — Concrete kilroy tool-node requirements (RQ-A4)

From `/Users/gb/github-qwick/qwick-ai/pipelines/{sentry-triage,sentry-bugfix}/pipeline.dot` (`parallelogram` shape = tool node):

- **Attrs used:** `tool_command="<shell>"` and `timeout="<seconds>"` — the entire surface. Timeouts seen: 10, 30, 120, 1800.
- **Working-dir contract:** every command runs in the run's worktree (`mkdir -p .ai && ... > .ai/issue.json`, `make ci`, `git diff`, `gh issue view`). The exec branch MUST set `cwd = wtPath`.
- **Exit-code-as-signal idiom is pervasive:** `assess_confidence` does `[ "$CONFIDENCE" = "LOW" ] && exit 1 || exit 0` to route to `exit_skip` on the `condition="outcome=fail"` edge. `check_dedup_result` exits 1 on DUPLICATE. The loop-counter circuit breakers (`loop_guard_reproduce`, `loop_guard_fix`) do `echo $count > .ai/lp1; [ $count -gt 3 ] && exit 1`. **This is the load-bearing reason the tool node must map exit 0/non-zero → SUCCESS/FAIL faithfully** — the whole control flow of both pipelines is built on it.
- **Long-running:** `verify_fix` is `make ci` with `timeout="1800"` (30 min). The tool node is NOT an agent subprocess — it has no progress stream, no `agent_ready`, no silent-hang detector (those are agentic-path-only) — so a 30-min window is fine, but the design must NOT route the tool node through `dispatchDotAgenticNode`.
- **kilroy uses DOT shapes** (`shape=parallelogram`); **harmonik uses `type=non-agentic`.** A harmonik-native port re-expresses each `parallelogram` as `type="non-agentic", handler_ref="shell", tool_command="...", timeout="..."`. The shape→type translation is an authoring/porting concern, not a spec change (harmonik's parser keys on `type=`, `parser.go:601`).

### F-A5 — Clean-add verdict + the two wrinkles (RQ-A5)

Clean additive on the spec side (WG-002 optional-attr addition, WG-031 reserved-set addition, EM-058 sub-note, new HC shell-handler anchor). BUT two things are NOT a frictionless additive change — **flag for integration**:

1. **`handler_ref` requirement collides with the shell capability.** WG-002 + EM-057 item 7 (`execution-model.md:1419`) say a `non-agentic` node MUST carry `handler_ref` resolving to a *registered handler*. A tool node carries `tool_command` instead of a meaningful agent handler. Resolutions: (a) require `handler_ref="shell"` naming a built-in shell handler that reads `tool_command`/`timeout` — keeps WG-002's invariant intact and matches EM-058's "invoke the handler referenced by handler_ref" dispatch action; OR (b) make `tool_command` an *alternative* to `handler_ref`, relaxing EM-057 item 7. **Lean (a).** The design pins this and amends EM-057 item 7's prose to acknowledge the built-in shell handler.
2. **The exec branch does NOT go through the handler subprocess / wire-protocol path.** A tool node has no NDJSON stream, no `agent_ready`, no claude session. Cleanest implementation is an inline `os/exec` in the `NodeTypeNonAgentic` case in `dot_cascade.go` (analogous to the synthesized-SUCCESS noop there today), NOT a route through `dispatchDotAgenticNode` / `handler.Launch`. EM-058's "invoke the handler referenced by handler_ref" is a *spec-layer* action; the daemon can satisfy it with an in-process shell exec for the built-in `shell` handler. Flag for integration: the handler-contract is written assuming a subprocess + socket, so the shell-handler anchor needs a "built-in deterministic handlers MAY be in-process" note. This is the only place where the additive story has a seam.

## Patterns to follow

- Reserved-attr strict-error: `parser.go:644-659` (`policy_ref`, `schema_version`-on-node).
- Permissive-attr retention: `parser.go:660-666` + edge `traversal_cap` round-trip `parser.go:699-702`.
- Exit-code-as-routing-signal: kilroy `assess_confidence` / `check_dedup_result` / `loop_guard_*`.
- Mechanism-tagged classifier (no cognition): HC §4.4 `handler-contract.md:379`.

## Risks / conflicts

- **R-A1 (medium):** `handler_ref` requirement vs shell capability (F-A5 #1) — pick `handler_ref="shell"`; otherwise EM-057 item 7 needs a relaxation.
- **R-A2 (medium):** in-process exec vs the handler-contract's subprocess framing (F-A5 #2) — flag for integration; needs a "built-in deterministic handlers MAY be in-process" note in the shell-handler anchor.
- **R-A3 (low):** timeout classification (`transient` vs `canceled`) — OQ-4-adjacent; design decides (lean `transient` for timeout, `canceled` only for ctx-cancel/operator-stop).
- **R-A4 (low):** the HEAD-advance check (`dot_cascade.go:589`) is *agentic-path-only*, so a tool node's worktree side-effects (`.ai/*`, `git`, `gh`) are unaffected by it. No coupling with component C.
