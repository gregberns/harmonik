# Beads Integration — Change Design (C5)

Scope: per-task encoding of the `workflow_mode` setting as a `workflow:<mode>` label on the bead, plus the read-path and write-discipline rules surrounding it.

## 1. Current state

- `beads-integration.md §4.3 BI-005..BI-009` (lines ~104–145) names Beads as authoritative for bead content (`title`, `description`, `type`) and typed edges. `BI-007` (lines ~116–124) defines the harmonik write-subset of Beads's `Status` enum and the read-tolerance contract; labels are not normatively addressed.
- `§4.4 BI-010..BI-012` (lines ~149–204) restricts harmonik's Beads write surface to terminal status transitions (`claim` / `close` / `reopen`). No mechanism today permits a label-write from the daemon mid-run.
- `§4.5 BI-013..BI-016` (lines ~208–242; BI-014a is the orphan-sweep entry, unrelated to this change) defines the read surface (ready-work, dep-graph, bead-detail, reconciliation queries). The ready-work JSON return shape already includes labels (per research finding: `br show --format json` exposes `labels` array).
- `§4.6 BI-017..BI-020` (lines ~246–268) covers bead-ID propagation into run metadata, checkpoint trailers, event payloads, and session-log metadata.
- No current requirement defines or constrains the `workflow:<...>` label prefix.

## 2. Target state

Amend `§4.3` with a new requirement, provisionally `BI-009a — Workflow-mode label`:

(a) **Encoding.** A bead MAY carry an optional label of the form `workflow:<mode>` where `<mode> ∈ {single, review-loop, dot}`. The label's presence asserts a per-task workflow-mode override; its absence defers to lower-precedence tiers (project → daemon → built-in). (b) **Multi-label collision.** A bead carrying two or more `workflow:<...>` labels is malformed. The adapter MUST treat this as a hard read-error: emit a class-O `bead_label_conflict` observability event (cross-spec coordination request to event-model; see `event-model-design.md`), surface the bead to the dispatch loop with `workflow_mode = <unresolved>`, and the daemon MUST fall back to the next-lower precedence tier rather than dispatch under an ambiguous mode. (c) **Single-source authority.** The bead's `workflow:` label is the **highest-precedence** input in the four-tier resolution chain (task → project → daemon → built-in) owned by execution-model.

Amend `§4.4 BI-010` write-discipline restatement (provisional `BI-010c — Workflow-mode label write discipline`):

(d) **Agent prohibition.** Agents MUST NOT add, remove, or modify `workflow:<...>` labels via `br update` from inside a workflow run. The label is operator-or-creation-time only; reconciliation-or-daemon-written exception is permitted ONLY where a workflow's design intent explicitly so dictates (e.g., a self-modifying reconciliation routine), and any such write MUST route through the existing `§4.8 BI-025` adapter and carry the `§4.10` idempotency-key infrastructure. (e) The Beads-CLI skill (`§4.9 BI-027`) MUST document the prohibition for agents so it appears in the agent's launch context.

Amend `§4.5 BI-013` (ready-work query): the ready-work response payload MUST surface the bead's labels array including any `workflow:<...>` label so the daemon's claim path can extract and apply the mode. No new query; this is a read-path observation, not a writer change.

Amend `§4.5 BI-013` further (provisional `BI-013a — Needs-attention exclusion`): the Beads adapter's ready-work query MUST exclude beads carrying a `needs-attention` label, even when their status is `open`. Rationale anchored in operator-nfr's drain-policy design — see `operator-nfr-design.md §2(d)`.

Amend `§4.6 BI-020` (session-log metadata): the per-run sidecar (per `workspace-model.md §4.7 WM-026`) MAY carry the **resolved** `workflow_mode` (the tier that won, not just the bead label) for audit and reconciliation use. This is a workspace-model-owned writer concern; this spec only names that the resolved value is carried alongside `bead_id` in that sidecar.

## 3. Rationale

- Research finding `beads-encoding/findings.md` confirmed: Beads has no extensible metadata; the `workflow:<mode>` label prefix matches existing harmonik convention (`spec:`, `kind:`, `scope:`, `area:`) and requires no Beads schema change.
- Satisfies problem-space success criterion §2 (per-task override) and the locked decision "Beads encoding: `workflow:<mode>` label prefix."
- The agent-prohibition restatement against the new label closes a foreseeable confusion: agents already understand `BI-010` forbids status writes, but a fresh field invites a fresh class of mistake. Explicit naming pre-empts it.

## 4. Requirements traceability

| Req (02-components.md C5) | Target-state element |
|---|---|
| Optional `workflow:<mode>` label on the bead | §2 (a) |
| Highest-precedence in resolution chain | §2 (c) |
| Adapter surfaces label on ready-work query | §2 (BI-013 amendment) |
| Agents MUST NOT modify the mode | §2 (d), (e) |
| Sidecar carries resolved mode for audit | §2 (BI-020 amendment) |
| Multi-`workflow:` label is an error | §2 (b) |
| Ready-work query excludes `needs-attention`-labeled beads (cross-component from C7) | §2 (BI-013a amendment) |

## 5. Open decisions remaining for spec-draft pass

- **Allowed-mode enum source-of-truth.** Whether the enum lives in BI's spec (and is re-stated in EM) or in EM only with BI cross-referencing. Recommend EM as source-of-truth, BI cross-refs.
- **`bead_label_conflict` event ownership.** New class-O event in event-model §8.8 (observability) vs §8.6 (reconciliation lifecycle, since the daemon's fall-back behaviour is mildly reconciliation-shaped). Spec-draft chooses; see `event-model-design.md` open decisions.
