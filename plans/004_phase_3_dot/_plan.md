# Plan 004: phase-3-dot

## Objective
Define harmonik's DOT-based workflow-graph dialect (nodes, edges, outcomes, control points, failure classes, schema versioning) so DOT files become the source-of-truth for bead processes — the third milestone of the North Star.

## Status
active / research-phase (pass-4 design, ~6 of ~20 decisions landed; pass-5 spec drafts not started)

## Done means...

Phase-3 DOT is done when the workflow-graph dialect is specified, implemented, and verifiable end-to-end. NOT "the spec is written" or "the beads shipped." Concrete gate:

1. **All 14 open pass-4 design decisions resolved.** D7–D20 each have a written D-doc artifact under `plans/004_phase_3_dot/source/04-design/` with a clear decision and rationale. No pending decisions block spec drafting.
2. **Normative DOT-dialect spec present.** `specs/workflow-graph.md` (or the path confirmed by the integration pass) covers: node types, edge conditions, `Outcome` shape, `schema_version`, `failure_class` taxonomy, control-point node type, terminal-node differentiation, bead↔node binding, tool-node contract, observability hooks, sub-workflow mechanics, migration/N-1 rule. Reviewer APPROVE on the spec.
3. **Spec amendments filed.** All affected specs (execution-model, handler-contract, event-model, beads-integration, process-lifecycle) amended per §4.6 amendment protocol. Each amendment has a cross-reference back to the workflow-graph spec.
4. **At least one worked DOT example.** A `.dot` fixture in `specs/examples/` or `internal/workflow/scenario/` is validated against the normative spec. The fixture exercises at least one control-point node and one edge with a `failure_class` condition.
5. **Implementation epic filed.** Beads filed for at minimum: DOT parser, workflow runner wiring, scenario-test bead (end-to-end: harmonik dispatches a bead via a `.dot` workflow and the expected event trace appears in JSONL), and exploratory-test bead (operator invokes harmonik with a `.dot` workflow and observes outcome via CLI or JSONL). These scenario and exploratory beads are required by `plans/README.md` before the plan closes.

## What's done

- **Pass 1 — problem space** (`source/01-problem-space.md`): scope, constraints, success criteria. Reviewer APPROVE.
- **Pass 2 — decomposition** (`source/02-components.md`, `source/decompose-review.md`): component boundaries identified. Reviewer APPROVE.
- **Pass 3 — research** (`source/03-research/`): four research threads (workflow-graph, execution-model-dot, handler-contract-outcome, control-points-binding) + examples + `SUMMARY.md`. Reviewer APPROVE.
- **Pass 4 — design, partial — decisions LANDED:**
  - D1 — `failure_class` admitted as edge-condition LHS (`source/04-design/D1-edge-condition-lhs-admit-failure-class.md`)
  - D2 — failure-class placement on outcome payload (`source/04-design/D2-failure-class-placement.md`)
  - D3 — control-point as node-type (`source/04-design/control-point-node-type-design.md`)
  - D4 — edge-condition LHS whitelist (`source/04-design/D4-edge-condition-lhs-whitelist.md`)
  - D5 — edge-condition dialect (`source/04-design/D5-edge-condition-dialect.md`) — adopted verbatim from attractor-spec §10.2
  - D6 — verdict-surfacing direction (covered under `D-verdict-surfacing.md`)
- **Meta-decisions LANDED:**
  - D-attractor-adoption — adopt Attractor's `Outcome{status, preferred_label, context_updates, notes}` shape over fabro's bare enum
  - D-verdict-surfacing — agent verdict reaches the graph via outcome payload, not side-channel
  - D-edge-cascade-invariant — edge-resolution order is fixed and total (no ambiguous matches)
- **Upstream research captured** (`source/upstream-research/`):
  - `fabro-findings.md` — fabro DOT orchestrator comparison; adopt-verbatim and avoid lists
  - `kilroy-remaining-rows-audit.md` — per-row mapping of remaining D-rows to Kilroy/Attractor upstream coverage

## What's remaining

Pass-4 open decisions:

- **D7** — gate-decision payload kind (gate verdict vs ordinary outcome — distinct edge LHS?)
- **D8** — context_updates typing: registered key list vs free-form-with-prefix (Kilroy partial precedent)
- **D9** — unknown-attribute policy on nodes/edges (strict reject vs permissive ignore)
- **D10** — `schema_version` placement on the digraph (harmonik invention — no upstream)
- **D11** — repo layout for `.dot` files (where they live, naming) (harmonik invention)
- **D12** — terminal-node differentiation: single exit vs multiple typed terminals (approved/needs-attention/failed) — harmonik invention
- **D13** — review-loop convention (which node-type pattern represents review→revise cycles)
- **D14** — sub-workflow / call-graph mechanics
- **D15** — mechanism-tag Gate schema drift policy
- **D16** — bead↔node binding contract (how a node addresses the bead it acts on)
- **D17** — Tool-node contract (exit-code → outcome.status mapping)
- **D18** — observability / event-stream hooks per node
- **D19** — normative dispatch table — adopt Kilroy §2.8 verbatim (largely closed, still needs a written D-doc)
- **D20** — migration / N-1 compatibility rule

Then:

- **Pass 5 — spec drafts**: convert landed D-decisions into normative spec text under `specs/`.
- **Pass 6 — integration + tasks**: wire spec into existing subsystem docs, generate implementation beads.

## References

- specs: `specs/` (target for pass-5 output; no phase-3-dot spec yet)
- source: `plans/004_phase_3_dot/source/` (full kerf bench tree as of 2026-05-18)
- upstream research: `plans/004_phase_3_dot/source/upstream-research/{fabro-findings.md,kilroy-remaining-rows-audit.md}`
- recon: `.kerf/recon/kilroy-findings.md`, `.kerf/recon/attractor-findings.md`
- summary: `plans/004_phase_3_dot/source/03-research/SUMMARY.md`
- chat-context: phase-3-dot is the DOT-dialect-definition work. Passes 1-3 cleared with reviewer APPROVE; pass-4 has been running in chunks (pass-2/3/4 iterations of design review). Fabro (https://github.com/fabro-sh/fabro) was investigated in chat as a second upstream reference alongside Kilroy/Attractor — findings were never persisted and are captured here now to avoid loss.

## Next steps

1. Land D7-D12 next (the harmonik-invention rows where upstream gives no guidance — these block spec drafting).
2. Write the trivial D19 D-doc (adopt Kilroy dispatch table verbatim) to clear the closed-by-upstream row.
3. Once D7-D20 are landed, advance to pass-5 (spec drafts) — produce normative spec text under `specs/`.

## Open questions

- Whether terminal-node differentiation (D12) should be a `terminal_kind=` attribute or distinct shapes (multiple `Msquare` variants).
- Whether `schema_version` (D10) lives on the digraph statement or as a top-of-file pragma comment.
- Whether registered context-keys (D8) are declared in-DOT or in a sidecar schema file.
