# per-node-model-effort.dot — documentation sidecar

Per [specs/workflow-graph.md §12 WG-037], each canonical example requires a
sidecar documenting which spec sections it exercises, its expected golden trace,
and any reserved context-key dependencies.

## Spec sections exercised

| Spec ref | What it exercises |
|---|---|
| WG-042 | `model=` and `effort=` are valid optional attrs on `agentic` nodes only; each is independent of the other |
| WG-042 | `effort` must be a member of `{low,medium,high,xhigh,max}`; out-of-enum is an ingest-time strict error |
| WG-042 | `model` must match `^[A-Za-z0-9._:/-]+$`, at most 128 chars; bad shape is an ingest-time strict error |
| WG-042 / WG-031 | `model`/`effort` on a `non-agentic`, `gate`, or `sub-workflow` node, or on an edge, is a reserved-out-of-position strict error |
| WG-043 / WG-031 | `class=` and `model_stylesheet=` are NOT reserved; they are accepted permissively (warning emitted, retained in `UnknownAttrs`, not dispatched on) |
| EM-012b-NODE | Per-node `model`/`effort` is the highest-precedence (tier-0) input to the model/effort resolution chain; they override the run-level defaults for the node that carries them only |
| WG-021..WG-023 | Distinct terminal node IDs (`close`, `close-needs-attention`) |
| WG-010..WG-011 | Edge cascade with conditional + unconditional fallback |
| WG-028 / EM-043 | `traversal_cap="3"` on the reviewer→implementer back-edge |

## What this example demonstrates

The workflow is a minimal review loop that adds per-node model/effort overrides:

- **`implementer`** carries both `model="claude-opus-4-8"` and `effort="high"`.
  The daemon resolves this node's launch using those values instead of the
  run-level `(model, effort)` pair, for this node's dispatch only.

- **`reviewer`** carries only `model="claude-haiku-4-5"` (no `effort=`).
  The daemon uses the reviewer's per-node model with the run-level effort.
  This demonstrates the independence of the two attributes per WG-042.

- `start`, `close`, and `close-needs-attention` carry neither `model=` nor
  `effort=` — they are `non-agentic` nodes where those attributes are forbidden
  (strict error per WG-042 / WG-031).

## Expected golden trace

The expected loader behaviour when this file is parsed via `harmonik graph validate`:

1. `dot.Parse` parses the file cleanly — no strict errors.
2. `dot.Validate` emits no `SeverityError` diagnostics.
3. `harmonik graph validate per-node-model-effort.dot` exits 0 and prints
   `per-node-model-effort.dot: valid`.
4. `harmonik graph validate --json per-node-model-effort.dot` exits 0 and
   prints `[]`.

The per-node model/effort values (`claude-opus-4-8`, `high`, `claude-haiku-4-5`)
are opaque aliases at parse time — harmonik validates their shape only, not
whether a real model with that name exists. Handler-side launch failure is the
authoritative compatibility check per WG-042 and EM-012b.

## Ingest-error reference

| Invalid attribute | Error type | Error message fragment |
|---|---|---|
| `effort="ultra"` on agentic node | Strict parse error | `effort "ultra" is not in {low,medium,high,xhigh,max}` |
| `model="bad value!"` on agentic node | Strict parse error | `model "bad value!" must match ^[A-Za-z0-9._:/-]+$` |
| `model="..."` on `non-agentic` node | Strict parse error | `reserved-out-of-position on type "non-agentic"` |
| `effort="..."` on `gate` node | Strict parse error | `reserved-out-of-position on type "gate"` |
| `class="hard"` on agentic node | Permissive warning | `unknown permissive attribute "class"` — retained in `UnknownAttrs` |
| `model_stylesheet="..."` (any node) | Permissive warning | `unknown permissive attribute "model_stylesheet"` — retained in `UnknownAttrs` |

## Context-key dependencies

None. This workflow does not declare `context_keys` and does not use
`context.<key>` in any edge condition.
