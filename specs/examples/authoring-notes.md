# specs/examples/ — Authoring and Porting Notes

> **NON-NORMATIVE.** This file is a guidance document, not a spec section.
> Normative obligations are in `specs/workflow-graph.md`. This document
> consolidates the authoring rules and porting aliases scattered across WG-039
> through WG-043 into a single place workflow authors can consult.

---

## 1. `non_committing` — the canonical example sidecar (WG-041)

This section is the "canonical example sidecar" referenced by
`specs/workflow-graph.md §4 WG-041`.

### 1.1 What `non_committing` does

`non_committing="true"` on an implementer-class `agentic` node tells the engine
to accept a clean agent exit as `SUCCESS` even if the worktree HEAD did NOT
advance. Without this flag, a clean exit without a new commit is a node failure.

The flag controls exactly one axis: **commit-or-not**. It does NOT derive a
non-SUCCESS outcome from a work product or an embedded `{"status": ...}` marker.
The node always produces `Outcome{status = SUCCESS}` on a clean exit when
`non_committing="true"`.

### 1.2 `auto_status` is not accepted — use `non_committing` instead

harmonik **does NOT accept `auto_status` as a node attribute at v1**. The name
is reserved for a future status-derivation feature that does not yet exist.

If you are porting a pipeline from a system that uses `auto_status=true` to
mean "this node succeeds without committing", replace it with
`non_committing="true"`:

```dot
// WRONG — harmonik rejects this with a strict parse error:
//   node "analyze": attribute "auto_status" is reserved-and-rejected at v1
//   (WG-041 §I.4); use non_committing="true" instead
analyze [
    type="agentic",
    agent_type="implementer",
    handler_ref="claude-implementer",
    auto_status="true",           // ← REJECTED
    ...
];

// CORRECT — the harmonik v1 form:
analyze [
    type="agentic",
    agent_type="implementer",
    handler_ref="claude-implementer",
    non_committing="true",        // ← accepted
    ...
];
```

The ingest error is actionable: it names the offending attribute and tells the
author which attribute to use instead.

### 1.3 Authoring obligation — pair with a downstream validating tool node

A `non_committing` node produces no committed work product the engine can
validate. The engine cannot distinguish a good no-commit exit from a bad one;
it always yields `SUCCESS`. **The workflow author is responsible for adding a
downstream validating `non-agentic` tool node** (per WG-039) that inspects the
node's work product and exit-codes the routing decision.

Minimal pattern:

```dot
// The non_committing implementer writes a result file but does not commit.
analyze [
    type="agentic",
    agent_type="implementer",
    handler_ref="claude-implementer",
    non_committing="true",
    prompt="Analyze the codebase for X. Write findings to analysis.json. Do NOT commit."
];

// The downstream tool node reads analysis.json and routes on exit code.
// Exit 0 → SUCCESS (findings look good); exit 1 → FAIL (validation failed).
validate_analysis [
    type="non-agentic",
    handler_ref="shell",
    tool_command="test -s analysis.json && jq -e '.findings | length > 0' analysis.json",
    idempotency_class="idempotent",
    timeout="30",
    role="validate that analysis.json is non-empty and contains findings"
];

analyze -> validate_analysis;

validate_analysis -> next_step [
    condition="outcome.status == 'SUCCESS'"
];
validate_analysis -> "close-needs-attention" [
    condition="outcome.status == 'FAIL'"
];
// Unconditional fallback per D-edge-cascade-invariant:
validate_analysis -> "close-needs-attention";
```

The engine does not enforce this pairing. Skipping the validating tool node
means any `non_committing` node that runs and exits cleanly — even if it wrote
nothing, crashed silently, or produced garbage output — will route as `SUCCESS`.

---

## 2. Reviewer-node `prompt` — accepted but inert at v1 (WG-040)

A `prompt` attribute on a **reviewer-class** `agentic` node is parsed without
error and retained in the AST, but is **not used for dispatch** at v1.

The reviewer's brief is always sourced from the review-target artifact per the
EM-015d-RIA sub-clause; the `prompt` value is silently ignored. This is
intentional: reviewer-class `prompt` override is reserved for a future schema
version.

```dot
// The prompt= is accepted by the parser and retained in the AST.
// At v1, it has NO effect on the reviewer's brief.
reviewer [
    type="agentic",
    agent_type="reviewer",
    handler_ref="claude-reviewer",
    prompt="Focus on security issues only.",   // ← accepted, but INERT at v1
    role="security reviewer"
];
```

If you want to specialize a reviewer's brief today, use the `role` attribute:
the reviewer handler includes the node's `role` in the brief it sends the agent.
Per-node `prompt` override for reviewers is a clean future amendment.

---

## 3. `class` / `model_stylesheet` — informative only, use `model=` directly (WG-043)

Some upstream pipeline systems select per-node models via a graph-level CSS
`model_stylesheet` combined with per-node `class` attributes:

```dot
// Upstream form — NOT interpreted by harmonik:
graph_attributes [
    model_stylesheet=".hard { llm_model: claude-opus-4-8 } * { llm_model: claude-haiku-4-5 }"
];
expensive_node [
    class="hard",        // ← select the .hard model tier
    llm_provider="anthropic"
];
```

harmonik **does not interpret `class` or `model_stylesheet`** at v1. A loader
accepts both permissively (emits a warning, retains them in `UnknownAttrs`) but
the dispatcher never routes on them.

**To port such a pipeline to harmonik**, translate the stylesheet rule plus class
into a direct `model=` attribute on each node that needs a non-default model
(per WG-042):

```dot
// Harmonik form — direct per-node model override:
expensive_node [
    type="agentic",
    agent_type="implementer",
    handler_ref="claude-implementer",
    model="claude-opus-4-8",   // ← direct, per WG-042; no class= needed
    effort="high"
];
cheap_node [
    type="agentic",
    agent_type="reviewer",
    handler_ref="claude-reviewer",
    model="claude-haiku-4-5"   // ← direct, per WG-042
];
```

Drop `llm_provider` as well — the handler binding is fixed by `handler_ref` per
HC-003 and is not a per-node attribute in harmonik.

See `specs/examples/per-node-model-effort.dot` for the worked `.dot` example
and `specs/examples/per-node-model-effort.md` for the ingest-error reference
table.

---

## Summary of ingest-time attribute behavior

| Attribute | On node type | v1 behavior |
|---|---|---|
| `non_committing="true"` | `agentic` implementer-class | Accepted; relaxes HEAD-advance check for clean exits |
| `non_committing` (any) | `agentic` reviewer-class, `non-agentic`, `gate` | Warning emitted; retained in AST; ignored at dispatch |
| `auto_status` | any | **Strict error** — reserved-and-rejected; use `non_committing="true"` |
| `prompt` | `agentic` implementer-class | Accepted; REPLACES bead body for that node's dispatch |
| `prompt` | `agentic` reviewer-class | Accepted; **inert at v1** — reviewer brief is unchanged |
| `prompt` | `non-agentic`, `gate` | Warning emitted; retained in AST; ignored |
| `model` / `effort` | `agentic` | Accepted; override run-level model/effort for that node only |
| `model` / `effort` | `non-agentic`, `gate`, edge | **Strict error** — reserved-out-of-position |
| `class` | any | Permissive warning; retained in `UnknownAttrs`; never dispatched |
| `model_stylesheet` | any | Permissive warning; retained in `UnknownAttrs`; never dispatched |
