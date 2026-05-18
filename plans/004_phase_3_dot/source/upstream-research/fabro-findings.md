# Fabro investigation (2026-05-18)

Source: https://github.com/fabro-sh/fabro (Rust, MIT, 786 stars). Active project. Docs at docs.fabro.sh.

## What fabro is

Open-source deterministic Rust orchestrator that runs AI coding agents through Graphviz DOT workflow graphs. Branching, loops, parallelism, human gates. Single binary, no external DB, unified event stream (queryable via DuckDB), git checkpointing per stage, cloud sandboxes (Daytona), CSS-like model stylesheets for per-node model routing. Same architectural posture as harmonik's North Star: deterministic skeleton, probabilistic organs.

## Data-flow model (vs. Attractor)

Different and weaker than Attractor's `Outcome{...}` struct.
- **Outcome** is a simple 4-value enum: `succeeded | failed | partially_succeeded | skipped`. No context_updates, no notes, no structured payload on the outcome itself.
- **Routing directive** is a separate JSON object agents emit, containing `suggested_next_ids` (ordered) + `preferred_next_label`.
- **Context propagation** via `fidelity` (compact/full) on edges/graph + file-based sharing + `parallel_results.json` from merge nodes. No structured context_updates diff.

Attractor's outcome model is more cohesive: one struct carries status + routing hint + typed context diff. Fabro's is more ad hoc. Harmonik should stay with Attractor's shape.

## Terminal nodes

Reserved-ID + shape convention: terminal nodes have ID `exit`/`Exit`/`end`/`End` and `shape=Msquare`. Start is `Mdiamond` with reserved ID. Fabro requires exactly one Start + one Exit. (Harmonik should relax to allow multiple exits with distinct IDs.)

## Schema versioning

None documented. No version attribute on the digraph, no schema-version field anywhere. Workflows versioned via git only. Real gap.

## Failure handling (vs. Attractor + Kilroy)

Richer than Attractor here:
- 6 failure classes: `transient_infra`, `deterministic`, `budget_exhausted`, `compilation_loop`, `canceled`, `structural`.
- Auto-classification from SDK error types / HTTP status / message patterns.
- 3 retry layers: LLM-call (max 3, exp backoff), turn-level (stream-drop), node-level (`retry_policy`: standard|aggressive|linear|patient or explicit max_retries).
- Routing on failure: retry first, then `condition="outcome=failed"` edges, then `retry_target` attribute, then run fails.

## Adopt-verbatim list

1. **Failure-class enum** — `transient_infra | deterministic | budget_exhausted | compilation_loop | canceled | structural`.
2. **`retry_target` and `fallback_retry_target` node attributes** — dedicated edge-bypass for retry routing.
3. **`goal_gate=true` node attribute** — explicit marker for "this is the verification gate."
4. **`max_visits` per-node** — loop bound separate from `max_retries`.
5. **Edge `condition` expression grammar** — keep equality-only (per Kilroy §10.2); fabro's richer grammar is overkill for now.
6. **Reserved-ID + shape terminal convention** (`Mdiamond` start, `Msquare` exit) — explicit beats inferential.
7. **`fidelity` knob on edges** (`compact` vs `full`) — explicit control of how much prior context flows forward.
8. **Graph-level `goal=` attribute** — workflow purpose stated in the DOT itself.

## Avoid list

1. Outcome as bare enum without structured payload (stay with Attractor's shape).
2. No schema version on the graph (require `schema_version=` from day one).
3. Hard "exactly one Exit" requirement (allow multiple terminals with distinct IDs).
4. Edge-label keyboard-accelerator stripping (mixes presentation into routing semantics).
5. TOML sidecar (`workflow.toml`) next to `workflow.fabro` (prefer single-file).
6. Automatic failure classification from error-message string-matching (brittle).

## Open questions

- Concrete schema of "routing directive JSON" — docs reference fields but don't show exact shape.
- Context-bag structure — typed accumulator or opaque text + files? Appears to be the latter.
- `parallel_results.json` shape — referenced but not specified.
- Interaction between `outcome=partially_succeeded` and edge selection.

## Source URLs

- https://github.com/fabro-sh/fabro
- https://docs.fabro.sh/reference/dot-language.md
- https://docs.fabro.sh/execution/outcomes.md
- https://docs.fabro.sh/execution/failures.md
- https://docs.fabro.sh/workflows/transitions.md
- https://docs.fabro.sh/workflows/stages-and-nodes.md
- `.fabro/workflows/implement-plan/workflow.fabro` (example, fetched in source)
