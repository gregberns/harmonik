# Change-spec delta — workflow-graph.md (auto_status v2)

**DRAFT for review. NOT applied to `specs/`. Do not commit.**
Codename: auto-status · Epic: hk-cq1 · Date: 2026-06-13.

## Anchor

Target the captain named "§4 (auto_status semantics)". The best real anchor is
**§4 Node type catalog → WG-041** (`specs/workflow-graph.md:179`), the sibling rule
that today *reserves-and-rejects* `auto_status`. The v2 delta REPLACES the reserved
block (the `**auto_status is reserved.**` paragraph at line 187) with a new
standalone rule **WG-053** and amends the WG-031 reserved-set position rules and the
§16.1 vocabulary-diff row at line 715.

> ⚠ The "§I.4" anchor the shipped v1 code cites ("WG-041 §I.4", `parser.go:706`,
> `node.go:103`) DOES NOT EXIST in the spec. §4 of workflow-graph.md has no
> sub-clause numbering. The v1 FAIL-axis slice (hk-oo4, 5c5b15ef) shipped CODE that
> accepts `auto_status="true"` but the spec was never updated — it still says
> reserved-and-rejected. This delta is also the v1 spec-catch-up. Flag for redline.

## Proposed rule WG-053 (new) — replaces the reserved block in WG-041

### WG-053 — `auto_status` deny-side outcome-derivation attribute on `agentic` nodes

An `agentic` node MAY carry an `auto_status` optional attribute. When set, the engine
performs a **deterministic, daemon-authoritative deny-side outcome gate** after the
implementer exits and before the node's `SUCCESS` outcome is finalized. The gate MAY
derive `FAIL` (with a `failure_class` per §7 WG-017); it MUST NOT derive `APPROVE`,
`BLOCK`, or any verdict, and it MUST NOT auto-confirm `SUCCESS` as authoritative — a
clean gate leaves the pre-existing `SUCCESS`/HEAD-advance derivation unchanged. The
derived outcomes are a strict subset of the §4 WG-007 legal `agentic` status set; no
new Outcome enum value is introduced.

**Field shape (v1) and forward-compat.** At v1 the only accepted value is the boolean
`auto_status="true"` (and the inert `"false"`, the default-absent equivalent). A
non-boolean value is a strict ingest error at v1. The boolean form is the
forward-compatible base of a future `auto_status="<policy-name>"` STRING form: a later
schema version MAY admit a named-policy value selecting a deny-side policy that can
also bounce the review loop to `REQUEST_CHANGES` (deferred — see WG-019 / §13). A
loader MUST therefore parse `auto_status` as a string-typed attribute whose v1 value
domain is `{"true","false"}`; widening the value domain to policy names is an additive
change and MUST NOT break a v1 reader (which rejects unknown values).

**Node-type scope.** `auto_status` is valid ONLY on **implementer-class** `agentic`
nodes (the same dispatch path as WG-041 `non_committing`). On a reviewer-class
`agentic` node, a `non-agentic` node, or a `gate` node it is a validation warning per
§10 WG-031 (those paths do not reach the post-implementer outcome-derivation point);
the value is retained in the AST and ignored.

**Orthogonality.** `auto_status` and `non_committing` (WG-041) are orthogonal axes:
`non_committing` controls commit-or-not for the `SUCCESS` path; `auto_status` controls
deny-side `FAIL` derivation from the work product. A node MAY carry both.

The derivation mechanism (which deterministic signals are inspected, where in the
dispatch path the gate runs, the AR-006 LLM-free guarantee) is owned by
[execution-model.md §7.5 EM-068]; the handler-supplied marker input is owned by
[handler-contract.md §4.2a HC-068]. WG-053 owns only the node-attribute surface.

D-mapping: D1 (deny-side only; never auto-APPROVE/BLOCK), D4 (FAIL-only in v1;
REQUEST_CHANGES-via-policy deferred), forward-compat (boolean→string field shape).

Tags: mechanism, normative

## Amend WG-031 (reserved-set position rules, line 488)

Add `auto_status` to the node-level (`agentic`-only) reserved names alongside
`prompt` / `non_committing` / `model` / `effort`:

> Position rules: … `prompt` / `non_committing` / `auto_status` / `model` / `effort`
> are node-level (`agentic` only); …

D-mapping: D1/D4 (positions the attribute as an accepted agentic-only name, not a
rejected one).

## Amend §16.1 vocabulary-diff row (line 715)

Replace the trailing "auto_status reserved/not-accepted" clause of the WG-041 row
with a new WG-053 row:

> | §4 WG-053 auto_status | New normative content (auto-status v2). Deny-side
> outcome-derivation attribute on implementer-class `agentic` nodes; derives
> `FAIL`+`failure_class` only (never APPROVE/BLOCK); boolean value shape
> forward-compatible with a future `auto_status="<policy-name>"` form. Supersedes the
> WG-041 reserved-and-rejected reservation. |

D-mapping: D1/D4/forward-compat (history marker for the reservation lift).
