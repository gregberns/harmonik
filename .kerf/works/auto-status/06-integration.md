# auto_status v2 — Integration Edit-Set (apply-ready)

> DRAFT deliverable. This file is the consolidated, deterministic edit-set for the
> auto_status v2 spec change. It will be applied as a single daemon bead. Each edit is
> an exact OLD→NEW block with enough surrounding context to apply unambiguously.
>
> LOCKED decisions encoded (do NOT re-litigate):
> - **D1** deny-side only (FAIL/bounce; never auto-APPROVE/BLOCK; LLM reviewer is sole APPROVE/BLOCK authority).
> - **D2** C1+C2 (keep C1 daemon work-product inspection; add C2 = post-run `.harmonik/auto_status.json`,
>   daemon-read + VALIDATED, mirroring `ReadReviewVerdict` / `.harmonik/review.json`; defer C3).
> - **D3** validated INPUT (C2 is a daemon-validated input, not an authoritative self-report;
>   daemon keeps classification authority; cross-ref EM-005c / HC-059).
> - **D4** FAIL-only v1 (FAIL + failure_class only; REQUEST_CHANGES-via-policy + reviewer-loop
>   re-entry DEFERRED).
> - **Forward-compat**: boolean `auto_status="true"` is forward-compatible with a future
>   `auto_status="<policy-name>"` string form.
>
> v1 ALREADY SHIPPED: bead hk-oo4, commit 5c5b15ef
> ("feat(auto-status): FAIL-axis slice — accept auto_status boolean + deterministic engine inspection").
> The spec was never updated; it still calls auto_status "reserved/rejected" in several places.
> This edit-set documents the shipped v1 AND adds the v2 C2 carrier, and closes EVERY dangling
> "reserved/rejected" reference so nothing contradicts the new WG-053.

---

## FILE 1 — specs/workflow-graph.md

### Edit 1.1 — NEW rule WG-053 (REPLACES WG-041's reserved-and-rejected block, ~line 187)
Addresses: core delta 1 (WG-053). D-mapping: D1, D2, D3, D4, forward-compat.

The replaced text is the single paragraph in WG-041 currently headed
**`auto_status` is reserved.** WG-041's other paragraphs (non-committing semantics,
the authoring rule, the reviewer/non-agentic/gate warning) are UNCHANGED.

**OLD** (workflow-graph.md, inside §4 WG-041, the paragraph immediately after the
"Authoring rule (normative)" paragraph):

```
**`auto_status` is reserved.** A `non_committing` node controls exactly one axis: commit-or-not. It does NOT derive status from a work product or an embedded marker. The attribute name `auto_status` is NOT accepted as a node attribute at v1 (it would mislead authors into expecting status-derivation that does not exist); `auto_status` is reserved for a future status-derivation feature. Pipelines ported from external `auto_status=true` semantics MUST use `non_committing="true"`.
```

**NEW**:

```
**`auto_status` is accepted (deny-side; see WG-053).** A `non_committing` node controls exactly one axis: commit-or-not — it derives no status from a work product or marker. A SEPARATE, orthogonal attribute, `auto_status` (WG-053 below), governs deny-side outcome-derivation on implementer-class `agentic` nodes: it lets the engine derive a non-`SUCCESS` (`FAIL` + `failure_class`) Outcome deterministically, with zero LLM calls. `auto_status` and `non_committing` are independent and MAY co-occur on the same node. Pipelines ported from external `auto_status=true` semantics now map directly to `auto_status="true"` (see WG-053); use `non_committing="true"` only when the intent is purely "succeed without committing."
```

Then append the following NEW rule **immediately after WG-041's closing
`Tags: mechanism, normative` line** (i.e., a new `### WG-053` block inserted before
`### WG-042`):

**NEW** (insert after WG-041's `Tags: mechanism, normative`):

```
### WG-053 — `auto_status` deny-side outcome-derivation attribute on `agentic` nodes

An **implementer-class** `agentic` node MAY carry an optional `auto_status` attribute. When present and truthy on such a node, the engine runs a deterministic, **daemon-authoritative deny-side gate** over the node's post-run state and work product: the gate MAY derive a `FAIL` Outcome carrying a `failure_class` (per [execution-model.md §8] and §7 WG-018), and MAY do nothing (leaving the node's `SUCCESS` derivation untouched). The gate NEVER derives `APPROVE`, `BLOCK`, a reviewer verdict, `REQUEST_CHANGES`, `RETRY`, or `PARTIAL_SUCCESS`, and NEVER auto-confirms `SUCCESS` from a work product. The reviewer agent remains the sole authority for `APPROVE` / `BLOCK` verdicts ([execution-model.md §4.3 EM-015d]); `auto_status` is a deny-side input only. The full engine semantics are normative in [execution-model.md §7.5.6 EM-068]; the daemon-validated marker input it consumes is normative in [handler-contract.md §4.2a HC-068].

- **Legal-status subset.** The Outcome the gate MAY derive is a subset of the §4 WG-002 / §3 WG-007 legal statuses for an `agentic` node: `FAIL` only. The gate introduces NO new `outcome.status` enum value and NO new `failure_class` value — `failure_class` is drawn from the six values of [execution-model.md §8]. The derived `FAIL` routes via the same failure-class edges as any other `FAIL` (§7 WG-018, §5 WG-010); there is no auto_status-specific routing channel.
- **Value domain (v1).** At v1 the loader MUST accept exactly the value domain `{"true", "false"}` (DOT-attribute boolean form). A non-boolean `auto_status` value is an ingest error per §10 WG-031 (reserved-attribute value-domain violation): the graph is static, so the loader MUST reject it at load and the run MUST NOT start. `auto_status="false"` (or an absent attribute) leaves the node's outcome-derivation unchanged from prior behavior.
- **Forward-compatibility.** The boolean `auto_status="true"` form is forward-compatible with a future `auto_status="<policy-name>"` string form (selecting a named deny-side policy). A v1 loader MUST treat any value outside `{"true","false"}` as an ingest error rather than silently accepting an unknown policy name; the string-policy form is reserved for a future schema version, where `"true"` resolves to the built-in default deny-side policy.
- **Orthogonal to `non_committing` (§4 WG-041).** The two attributes govern disjoint axes — commit-or-not (`non_committing`) vs. deny-side outcome-derivation (`auto_status`) — and MAY co-occur on one node. Neither implies the other.
- **Position / class scope.** `auto_status` is a node-level attribute valid ONLY on implementer-class `agentic` nodes. An `auto_status` attribute on a reviewer-class `agentic` node, a `non-agentic` node, a `gate` node, a `sub-workflow` node, an edge, or at the graph level is a reserved-attribute-out-of-position validation warning at v1 per §10 WG-031 (the value is retained in the AST and ignored; those dispatch paths do not reach the implementer deny-side gate). The §10 WG-031 reserved-set and position-rule entries for `auto_status` are normative.

Tags: mechanism, normative
```

---

### Edit 1.2 — WG-031 reserved-attribute set + position rule (reviewer-finding 4)
Addresses: completeness fix 4. D-mapping: D1 (deny-side, implementer-class scope),
forward-compat (value-domain enforcement). Both the reserved-set line AND the
position-rule line are edited so "non-boolean = ingest error" and "warning on
non-implementer nodes" are enforceable as reserved-attribute-out-of-position.

**OLD** (workflow-graph.md §10 WG-031, the reserved-set bullet, ~line 486):

```
- A reserved attribute name used outside its declared position. The reserved set at v1.0 is: `type`, `agent_type`, `handler_ref`, `gate_ref`, `sub_workflow_ref`, `workflow_version`, `input_mapping`, `idempotency_class`, `axis_tags`, `tool_command`, `timeout`, `transient_exit_codes` (node-level, `non-agentic` tool nodes only; reserved-and-warning at v1 per §4 WG-039; see [handler-contract.md §4.1 HC-063]), `prompt`, `non_committing`, `model`, `effort`, `policy_ref` (reserved-and-rejected name; see [control-points.md §4.12 CP-056]), `hook_ref`, `guard_ref`, `budget_ref`, `skills_ref`, `freedom_profile_ref`, `schema_version`, `version`, `condition`, `preferred_label`, `weight`, `ordering_key`, `start_node`, `terminal_node_ids`, `context_keys` (graph-level per [handler-contract.md §5.6 HC-062]; see WG-031a), `goal` (graph-level per §4 WG-044).
```

**NEW**:

```
- A reserved attribute name used outside its declared position. The reserved set at v1.0 is: `type`, `agent_type`, `handler_ref`, `gate_ref`, `sub_workflow_ref`, `workflow_version`, `input_mapping`, `idempotency_class`, `axis_tags`, `tool_command`, `timeout`, `transient_exit_codes` (node-level, `non-agentic` tool nodes only; reserved-and-warning at v1 per §4 WG-039; see [handler-contract.md §4.1 HC-063]), `prompt`, `non_committing`, `auto_status` (node-level, implementer-class `agentic` nodes only; accepted deny-side at v1 per §4 WG-053; value domain `{"true","false"}`, non-boolean = ingest error), `model`, `effort`, `policy_ref` (reserved-and-rejected name; see [control-points.md §4.12 CP-056]), `hook_ref`, `guard_ref`, `budget_ref`, `skills_ref`, `freedom_profile_ref`, `schema_version`, `version`, `condition`, `preferred_label`, `weight`, `ordering_key`, `start_node`, `terminal_node_ids`, `context_keys` (graph-level per [handler-contract.md §5.6 HC-062]; see WG-031a), `goal` (graph-level per §4 WG-044).
```

**OLD** (workflow-graph.md §10 WG-031, the "Position rules" line, ~line 488):

```
  Position rules: `tool_command` / `timeout` / `transient_exit_codes` are node-level (`non-agentic` only); `prompt` / `non_committing` / `model` / `effort` are node-level (`agentic` only); `goal` is graph-level. A name used outside its declared position is the WG-031 strict error and the run MUST NOT start. `class` and `model_stylesheet` are NOT in the reserved set (permissive/informative per WG-043). The WG-045 template-param surface is a load-time text transform, not an attribute, and adds no reserved name.
```

**NEW**:

```
  Position rules: `tool_command` / `timeout` / `transient_exit_codes` are node-level (`non-agentic` only); `prompt` / `non_committing` / `model` / `effort` are node-level (`agentic` only); `auto_status` is node-level (implementer-class `agentic` only) and its value MUST be drawn from `{"true","false"}` (a non-boolean value is the WG-031 strict value-domain error per §4 WG-053, and the run MUST NOT start); an `auto_status` attribute on any non-implementer-`agentic` position (reviewer-class `agentic`, `non-agentic`, `gate`, `sub-workflow`, edge, or graph level) is a reserved-attribute-out-of-position validation warning (retained in the AST and ignored) per §4 WG-053; `goal` is graph-level. A name used outside its declared position is the WG-031 strict error and the run MUST NOT start. `class` and `model_stylesheet` are NOT in the reserved set (permissive/informative per WG-043). The WG-045 template-param surface is a load-time text transform, not an attribute, and adds no reserved name.
```

> NOTE on the warning-vs-error split: per WG-040/WG-041 precedent, a reserved
> node-level attribute appearing on the WRONG node *type* (e.g. `prompt` on a
> `gate` node) is a §10 WG-031 *warning* (retained + ignored), while a malformed
> *value* in the right position is a strict error. The two NEW WG-031 clauses keep
> that split: out-of-position `auto_status` → warning; non-boolean value on an
> implementer-class `agentic` node → ingest error. This matches WG-053's
> "warning on non-implementer nodes" + "non-boolean = ingest error."

---

### Edit 1.3 — §16.1 vocab-diff: rewrite WG-041 trailing clause + add WG-053 row (reviewer-finding 3)
Addresses: completeness fix 3. D-mapping: D1, D2.

**OLD** (workflow-graph.md §16.1 vocab-diff table, the WG-041 row, ~line 715):

```
| §4 WG-041 non_committing | New normative content per component C (attractor-parity); implementer-class clean exit ⇒ SUCCESS without HEAD-advance; auto_status reserved/not-accepted; pair-with-validating-tool-node authoring rule. |
```

**NEW** (replace the row, and add a WG-053 row immediately after it):

```
| §4 WG-041 non_committing | New normative content per component C (attractor-parity); implementer-class clean exit ⇒ SUCCESS without HEAD-advance; orthogonal to `auto_status` (deny-side derivation, §4 WG-053); pair-with-validating-tool-node authoring rule. |
| §4 WG-053 auto_status | New normative content (auto_status v2; v1 boolean shipped hk-oo4 / 5c5b15ef); implementer-class `agentic` deny-side outcome-derivation attribute — deterministic daemon-authoritative `FAIL`+`failure_class` gate (never APPROVE/BLOCK/verdict, never auto-SUCCESS); value domain `{"true","false"}`, non-boolean = ingest error; forward-compat to future `auto_status="<policy-name>"`; engine semantics in [execution-model.md §7.5.6 EM-068], validated marker input in [handler-contract.md §4.2a HC-068]. |
```

---

## FILE 2 — specs/handler-contract.md

### Edit 2.1 — NEW rule HC-068 (appended to §4.2a after HC-061, ~line 308)
Addresses: core delta 2 (HC-068). D-mapping: D2 (C2 marker mirrors review.json /
ReadReviewVerdict), D3 (validated INPUT, daemon retains authority), D4 (FAIL-only).

Insert immediately AFTER HC-061's closing `Tags: mechanism` line (line ~308),
as a new `#### HC-068` block within §4.2a, before §4.3.

**NEW**:

```
#### HC-068 — `.harmonik/auto_status.json` is a daemon-validated deny-side outcome-derivation INPUT

An implementer-class `agentic` node carrying `auto_status="true"` per [workflow-graph.md §4 WG-053] MAY leave a post-run marker file at `${workspace_path}/.harmonik/auto_status.json` inside the worktree. The marker is a **daemon-validated INPUT to the deny-side outcome-derivation gate** of [execution-model.md §7.5.6 EM-068], NOT an authoritative self-report: the daemon retains classification authority exactly as for any handler-emitted `failure_class` hint per [execution-model.md §4.1 EM-005c] and HC-059. The marker mirrors the on-disk reviewer-verdict bus (`.harmonik/review.json` per [workspace-model.md §6.2 / §4.7 WM-027a], read by the daemon's `ReadReviewVerdict` path): it is the worktree-local file the daemon reads to recover an agent-supplied deny-side signal.

**Optionality.** The marker is OPTIONAL. When absent, the deny-side gate runs C1 only (the daemon's own work-product inspection per [execution-model.md §7.5.6 EM-068]); the run is conforming. The marker's absence is NOT an error and MUST NOT be treated as a malformed outcome (contrast `.harmonik/review.json`'s absence, which IS malformed per WM-027a(e), because a reviewer is obligated to write one; an `auto_status` node has no write obligation).

**Read timing.** The daemon MUST read `${workspace_path}/.harmonik/auto_status.json` AFTER the node's `agent_completed` event (per §4.3) and BEFORE finalizing the node's Outcome (the deny-side gate of EM-068 runs in the same window the reviewer-verdict read of WM-027a(b) occupies for a `review-loop` reviewer). The marker is read at most once per node dispatch.

**Validation (parse-or-reject).** The daemon MUST validate the marker and MUST ignore (treat as absent) any marker that fails validation, emitting a `failure_class_disagreement`-class log per HC-059's discipline when an ignored marker carried a status the daemon overrode:
- **Parse.** A marker that is not well-formed JSON, or whose `schema_version` is not daemon-readable per the N-1 rule, MUST be ignored (gate falls back to C1-only).
- **`status` MUST be `"FAIL"`.** A marker whose `status` is anything other than the literal `"FAIL"` MUST be ignored. In particular `SUCCESS`, `APPROVE`, `BLOCK`, and `REQUEST_CHANGES` are NON-conforming `status` values for this marker and MUST NOT derive any Outcome: the marker is a DENY-SIDE input only (D1; the reviewer is the sole APPROVE/BLOCK authority per [execution-model.md §4.3 EM-015d]). A non-`FAIL` `status` is ignored, not an error.
- **`failure_class` MUST be one of the six.** When `status == "FAIL"`, the marker's `failure_class` MUST be one of the six values of [execution-model.md §8] (`transient`, `structural`, `deterministic`, `canceled`, `budget_exhausted`, `compilation_loop`). A marker omitting `failure_class`, or carrying an out-of-set value, leaves the daemon to back-fill from its own classification per HC-059. A marker-supplied `failure_class = compilation_loop` MUST be overridden to `structural` per HC-059 (compilation-loop is daemon-only; the handler lacks the per-node attempt history) and logged via `failure_class_disagreement`.

**Authority.** The marker-supplied `failure_class` is an overridable HINT exactly like a handler-emitted `Outcome.failure_class` per HC-059; the daemon's classification is AUTHORITATIVE on disagreement (D3, cross-ref [execution-model.md §4.1 EM-005c] / HC-059). The daemon MUST NOT treat the marker as the final Outcome; it feeds the EM-068 gate, which the daemon owns.

**On-disk lifecycle.** `${workspace_path}/.harmonik/auto_status.json` is daemon-local control-plane state, NOT work product: it MUST be excluded from checkpoint commits via the [workspace-model.md §4.3 WM-013e] `.gitignore` hygiene set and MUST NOT pollute the squash-merge commit per [workspace-model.md §4.5 WM-019] (mirrors `.harmonik/review.json` per WM-027a(d)). There is NO mid-loop per-iteration archival of the marker at v1 (contrast `.harmonik/review.iter-<N>.json` per WM-027a(c)); the C3 multi-iteration / reviewer-loop-re-entry surface is DEFERRED.

**v1 schema.** At v1 the marker is a JSON object with fields:
- `schema_version` (integer, REQUIRED) — daemon-set N-1 readable per [workspace-model.md §6.4].
- `status` (string, REQUIRED) — MUST be the literal `"FAIL"`; any other value ⇒ marker ignored.
- `failure_class` (string, REQUIRED when honored) — one of the six [execution-model.md §8] values.
- `notes` (string, OPTIONAL) — freeform human-readable rationale; the engine MUST NOT parse it (mirrors `Outcome.notes` per [execution-model.md §4.1 EM-005]).
- `signals` (object, OPTIONAL) — freeform agent-supplied evidence map; retained for audit, NOT routed as edge-LHS.

Tags: mechanism, normative
```

---

## FILE 3 — specs/execution-model.md

### Edit 3.1 — EM-027 additive sentence (Outcome spine, ~line 589)
Addresses: core delta 3 (EM-027 additive sentence). D-mapping: D1, D2, D3.

**OLD** (execution-model.md §4.6 EM-027 body, ~line 591):

```
The handler outcome produced per [handler-contract.md §4.1], the hook dispatch per [control-points.md §4.3], the gate evaluation per [control-points.md §4.2], the transition selection per §4.10, and the transition event per [event-model.md §8.1] are one integrated flow. Each segment MUST consume the immediately prior segment's typed output and produce the typed input of the next; no segment may bypass another.
```

**NEW** (append one sentence to the existing paragraph):

```
The handler outcome produced per [handler-contract.md §4.1], the hook dispatch per [control-points.md §4.3], the gate evaluation per [control-points.md §4.2], the transition selection per §4.10, and the transition event per [event-model.md §8.1] are one integrated flow. Each segment MUST consume the immediately prior segment's typed output and produce the typed input of the next; no segment may bypass another. For a `dot`-mode implementer-class `agentic` node carrying `auto_status="true"` per [workflow-graph.md §4 WG-053], a deterministic daemon-authoritative deny-side gate (§7.5.6 EM-068) MAY derive a `FAIL` + `failure_class` Outcome before this spine's transition-selection segment; the gate feeds the SAME typed `Outcome` into the cascade and never bypasses a downstream segment.
```

---

### Edit 3.2 — NEW rule EM-068 at §7.5.6 (after §7.5.5, ~line 1640/1659)
Addresses: core delta 3 (EM-068). D-mapping: D1, D2 (C1+C2), D3 (daemon authority),
D4 (FAIL-only, no reviewer-loop re-entry), AR-006 mechanism-tag (anchor EM-039 NOT EM-040).

Insert a new `### 7.5.6` section + `#### EM-068` block immediately AFTER §7.5.5
EM-059's closing `Tags: mechanism` line (line 1659) and BEFORE `## 8. Error and
failure taxonomy` (line 1661).

**NEW** (insert between EM-059's `Tags: mechanism` and `## 8.`):

```
### 7.5.6 — Deny-side outcome-derivation gate (`auto_status`)

#### EM-068 — `auto_status` deny-side outcome-derivation gate

For a `dot`-mode implementer-class `agentic` node carrying `auto_status="true"` per [workflow-graph.md §4 WG-053], the daemon MUST run a deterministic, daemon-authoritative **deny-side gate** in the §7.4 `dispatch_node` `dot` path. The gate runs AFTER the §7.5.4 EM-058 HEAD-advance check (the component-C non-committing derivation) determines the node would otherwise resolve `SUCCESS`, and BEFORE that `SUCCESS` Outcome is returned to the §4.10 EM-041 cascade. The gate MAY replace the would-be `SUCCESS` with a `FAIL` Outcome carrying a `failure_class`; it does nothing on a clean pass. The gate is **mechanism-tagged** (zero LLM calls; per [architecture.md §4.2] AR-006 and §4.9 EM-039 — the gate is a validator-class deterministic check, NOT the EM-040 submission-validation path): its result MUST be determinable from the daemon's own observation plus the validated marker input, with no semantic judgment.

The gate has two evidence channels, both deny-side and both deterministic:

- **C1 — daemon work-product inspection (shipped v1, hk-oo4 / 5c5b15ef).** The daemon inspects the node's post-run worktree state with a deterministic build/vet probe (e.g. `go build ./... && go vet ./...` for a Go work product). A non-clean probe derives `FAIL` with a `failure_class` computed deterministically by the daemon (a build/vet failure maps to `deterministic` per [execution-model.md §8]). C1 requires no agent cooperation and is always available when `auto_status="true"`.
- **C2 — validated `.harmonik/auto_status.json` marker (auto_status v2).** When the node leaves the OPTIONAL post-run marker per [handler-contract.md §4.2a HC-068], the daemon reads and VALIDATES it per HC-068 and, if it validates as `status == "FAIL"`, the marker contributes a deny-side `FAIL` + `failure_class`. The marker's `failure_class` is an overridable HINT exactly like a handler-emitted `Outcome.failure_class` per [handler-contract.md §4.2a HC-059]; the daemon's classification is AUTHORITATIVE on disagreement (cross-ref [execution-model.md §4.1 EM-005c]). When the marker is absent or fails validation, the gate runs C1 only.

**Deny-side only.** The gate MUST NOT derive `APPROVE`, `BLOCK`, a reviewer verdict, `REQUEST_CHANGES`, `RETRY`, or `PARTIAL_SUCCESS`, and MUST NOT auto-confirm `SUCCESS` from any work product or marker (a clean gate leaves the node's prior `SUCCESS` derivation untouched). The reviewer agent remains the SOLE authority for `APPROVE` / `BLOCK` verdicts per §4.3 EM-015d (sub-clause EM-015d) — `auto_status` is a deny-side input only, never a verdict source.

**No reviewer-loop re-entry.** A gate-derived `FAIL` is a terminal node Outcome for cascade purposes: it routes via the node's failure-class edges per [workflow-graph.md §7 WG-018] / §4.10 EM-041 exactly as any other `FAIL` would. The gate does NOT re-enter the `review-loop` cycle of §4.3 EM-015d, does NOT emit a `REQUEST_CHANGES` verdict, and reserves NO reviewer-loop coupling at v1 (D4 — the REQUEST_CHANGES-via-policy + reviewer-loop re-entry surface is DEFERRED). The derived `FAIL` carries no new `failure_class` value: it is drawn from the six of §8.

**Default unchanged.** A node without `auto_status` (or with `auto_status="false"`) runs no deny-side gate; its outcome-derivation is exactly the §7.5.4 EM-058 behavior. The gate is opt-in per node and additive over the §10.1 EM-061 conformance set.

Tags: mechanism
```

---

## FILE 4 — specs/examples/authoring-notes.md  (reviewer-finding 1)
Addresses: completeness fix 1. D-mapping: D1, D4, forward-compat.

### Edit 4.1 — §1.2 heading + body + WRONG/CORRECT code block (~lines 40-72)

**OLD** (authoring-notes.md, the whole §1.2 from its `### 1.2` heading through the
`The ingest error is actionable…` paragraph, lines 40-72):

```
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
```

**NEW**:

```
### 1.2 `auto_status` is accepted — deny-side `FAIL` gate (distinct from `non_committing`)

harmonik **accepts `auto_status` as an implementer-class `agentic` node attribute
at v1** (per WG-053). It is the *deny-side outcome-derivation* axis: with
`auto_status="true"`, the engine runs a deterministic, daemon-authoritative gate
that MAY derive a `FAIL` (plus a `failure_class`) for the node with zero LLM
calls. It NEVER derives `APPROVE`, `BLOCK`, a verdict, or `SUCCESS` from a work
product — deny-side only; the reviewer agent remains the sole APPROVE/BLOCK
authority.

`auto_status` is ORTHOGONAL to `non_committing` (§1.1): one governs deny-side
status-derivation, the other governs commit-or-not. They may co-occur on one node.
A pipeline ported from a system whose `auto_status=true` meant "derive a deny-side
failure from this node's result" maps directly to `auto_status="true"`; a pipeline
whose `auto_status=true` only meant "succeed without committing" should use
`non_committing="true"` instead.

```dot
// ACCEPTED — implementer-class agentic node with the deny-side gate enabled:
analyze [
    type="agentic",
    agent_type="implementer",
    handler_ref="claude-implementer",
    auto_status="true",           // ← accepted (deny-side FAIL gate, WG-053)
    ...
];

// ACCEPTED — auto_status and non_committing are orthogonal and may co-occur:
analyze [
    type="agentic",
    agent_type="implementer",
    handler_ref="claude-implementer",
    auto_status="true",           // deny-side FAIL gate
    non_committing="true",        // succeed-without-committing
    ...
];
```

**Value domain.** At v1 `auto_status` accepts exactly `{"true","false"}`. A
non-boolean value (e.g. a policy name) is an ingest error at v1 — the run will not
start. The boolean form is forward-compatible with a future
`auto_status="<policy-name>"` string form selecting a named deny-side policy.

**Optional marker.** An `auto_status="true"` node MAY leave a post-run
`.harmonik/auto_status.json` marker (`{"schema_version":…, "status":"FAIL",
"failure_class":…}`) to supply a deny-side signal; the daemon validates it and
ignores any non-`FAIL` status (it is a deny-side INPUT, not an authoritative
self-report). The marker is optional — when absent, the gate uses the daemon's
own work-product inspection (e.g. `go build`/`go vet`).
```

### Edit 4.2 — summary attribute table row (~line 207)
Addresses: completeness fix 1 (strict-error table row). D-mapping: D1, forward-compat.

**OLD** (authoring-notes.md, the `auto_status` row in the summary table, ~line 207):

```
| `auto_status` | any | **Strict error** — reserved-and-rejected; use `non_committing="true"` |
```

**NEW** (replace with two rows — accepted on implementer-class, value-domain enforced;
out-of-position elsewhere):

```
| `auto_status="true"` / `auto_status="false"` | `agentic` implementer-class | Accepted (deny-side `FAIL` gate per WG-053); orthogonal to `non_committing`. Value domain `{"true","false"}` only — a non-boolean value is an ingest error. |
| `auto_status` (out of position) | `agentic` reviewer-class, `non-agentic`, `gate`, `sub-workflow`, edge, graph | Warning emitted; retained in AST; ignored (reserved-attribute-out-of-position per WG-031/WG-053) |
```

---

## FILE 5 — specs/examples/README.md  (reviewer-finding 2)
Addresses: completeness fix 2. D-mapping: D1, orthogonality.

### Edit 5.1 — bullet at ~line 572

**OLD** (README.md, the `auto_status` bullet under "Authoring and porting notes", ~line 572):

```
- **`auto_status` is rejected** — use `non_committing="true"` instead (the ingest
  error is actionable and names the replacement).
```

**NEW**:

```
- **`auto_status` is accepted (deny-side `FAIL` gate)** — an implementer-class
  `agentic` node may carry `auto_status="true"` to enable a deterministic,
  daemon-authoritative deny-side outcome-derivation gate (FAIL + failure_class;
  never APPROVE/BLOCK/verdict/SUCCESS). It is ORTHOGONAL to `non_committing`
  (deny-side derivation vs. commit-or-not); the two may co-occur. Value domain is
  `{"true","false"}` at v1; forward-compatible with a future
  `auto_status="<policy-name>"` string form. See WG-053.
```

---

## FILE 6 — specs/workspace-model.md  (reviewer-finding 5)
Addresses: completeness fix 5. D-mapping: D2 (C2 artifact's canonical on-disk-bus home).

### Edit 6.1 — §6.2 canonical-paths table: new auto_status.json row (~line 922)
Insert a new row immediately AFTER the `.harmonik/review.json` row (line 922) and
before the `.harmonik/review.iter-<N>.json` row (line 923).

**OLD** (workspace-model.md §6.2, the review.json row, line 922 — shown for anchor; UNCHANGED):

```
| `${workspace_path}/.harmonik/review.json` | reviewer agent writes; S06 archives per §4.7.WM-027a | Reviewer verdict for the current iteration of a `review-loop` run; conforms to the `agent-reviewer` JSON verdict schema v1; excluded from checkpoint commits via WM-013e. |
```

**NEW** (insert the following row immediately after the review.json row):

```
| `${workspace_path}/.harmonik/auto_status.json` | implementer-class `agentic` agent writes (OPTIONAL); S06/daemon reads + validates per [handler-contract.md §4.2a HC-068] | Deny-side outcome-derivation marker for an `auto_status="true"` node ([workflow-graph.md §4 WG-053]); daemon-validated INPUT (`{schema_version, status:"FAIL", failure_class, notes?, signals?}`), NOT an authoritative self-report; daemon retains classification authority per HC-059 / [execution-model.md §4.1 EM-005c]; OPTIONAL (absent ⇒ daemon work-product inspection only); NO per-iteration archival at v1; excluded from checkpoint commits via WM-013e. Mirrors the `.harmonik/review.json` on-disk bus. |
```

### Edit 6.2 — §4.3 WM-013e `.gitignore` hygiene set (~lines 364-376)
Add `.harmonik/auto_status.json` to the required ignore entries, mirroring
`.harmonik/review.json`.

**OLD** (workspace-model.md §4.3 WM-013e, the fenced ignore-entries block, lines 365-376):

```
.harmonik/lease.lock
.harmonik/sessions/
.harmonik/worktrees/
.harmonik/events/
.harmonik/review.json
.harmonik/review.iter-*.json
.harmonik/review-target.md
.harmonik/reviewer-feedback.iter-*.md
.harmonik/agent-task.md
.harmonik/agent-task.tmp-*
```

**NEW** (insert `.harmonik/auto_status.json` immediately after the `.harmonik/review.iter-*.json` line):

```
.harmonik/lease.lock
.harmonik/sessions/
.harmonik/worktrees/
.harmonik/events/
.harmonik/review.json
.harmonik/review.iter-*.json
.harmonik/auto_status.json
.harmonik/review-target.md
.harmonik/reviewer-feedback.iter-*.md
.harmonik/agent-task.md
.harmonik/agent-task.tmp-*
```

> NOTE: confirm the exact other entries in the fenced block at apply-time; the
> ONLY change is the inserted `.harmonik/auto_status.json` line. (The block also
> contains `.claude/settings.json` per WM-040a, omitted from the OLD anchor above
> for brevity — do not remove it; only insert the new line after
> `.harmonik/review.iter-*.json`.)

### Edit 6.3 — §4.7 lifecycle clause near WM-027a (gitignore/lifecycle mirror)
Addresses: completeness fix 5 (§4.7 gitignore/lifecycle clause). D-mapping: D2.

Append a short INFORMATIVE clause immediately AFTER WM-027a's closing
`Axes: …` line (line 589), before WM-028, documenting the auto_status.json
lifecycle as a mirror of review.json (the normative obligation lives in HC-068
and WM-013e; this is the WM-side cross-reference).

**NEW** (insert after WM-027a's `Axes:` line, before `#### WM-028`):

```
> INFORMATIVE: `${workspace_path}/.harmonik/auto_status.json` (the deny-side
> outcome-derivation marker for an `auto_status="true"` node, per
> [workflow-graph.md §4 WG-053] and [handler-contract.md §4.2a HC-068]) follows the
> same on-disk-bus lifecycle as `.harmonik/review.json`: an implementer-class agent
> MAY write it; the daemon reads and validates it after the node's `agent_completed`
> event and before finalizing the node Outcome; it is daemon-local control-plane
> state excluded from checkpoint commits via WM-013e and MUST NOT pollute the
> squash-merge per WM-019. It differs in two ways: (1) it is OPTIONAL (absence is
> NOT a malformed outcome, unlike review.json per (e) above), and (2) it has NO
> per-iteration archival at v1 (there is no `auto_status.iter-<N>.json`; the
> multi-iteration / reviewer-loop-re-entry surface is DEFERRED). The marker is a
> validated INPUT, not an authoritative self-report — daemon classification remains
> authoritative per [handler-contract.md §4.2a HC-059].
```

---

## GREP-SWEEP RESULT (`grep -rn "auto_status" specs/` after the edit-set)

Every surviving `auto_status` reference is consistent with "accepted, deny-side FAIL
gate." Pre-edit references and their post-edit disposition:

| File:line (pre-edit) | Pre-edit text | Post-edit disposition |
|---|---|---|
| workflow-graph.md:187 | "`auto_status` is reserved … NOT accepted … reserved for a future feature … MUST use non_committing" | REWRITTEN (Edit 1.1) → "accepted (deny-side; see WG-053)" + NEW WG-053 rule |
| workflow-graph.md:486 (WG-031 reserved set) | reserved set lacked auto_status | EDITED (Edit 1.2) → auto_status added with value-domain note |
| workflow-graph.md:488 (WG-031 position rules) | position rules lacked auto_status | EDITED (Edit 1.2) → implementer-`agentic`-only position rule + value-domain error + out-of-position warning |
| workflow-graph.md:715 (§16.1 WG-041 row) | "auto_status reserved/not-accepted" | REWRITTEN (Edit 1.3) → orthogonal-to-auto_status + NEW WG-053 row |
| handler-contract.md:1440 (revision history, v0.5.2) | "following the auto_status pattern of WG-041" (describes the transient_exit_codes reservation modeled on the OLD WG-041 auto_status block) | **NOT EDITED — see note below.** This is a frozen revision-history entry referencing the *pattern* (reserved-and-warning prose style), not asserting auto_status is reserved. Revision-history rows are immutable audit records; editing them rewrites history. A NEW revision-history row for this edit-set (added at apply-time) supersedes it. |
| examples/authoring-notes.md:40,42,45,51,57,207 | "not accepted / reserved-and-rejected / REJECTED / strict-error row" | REWRITTEN (Edits 4.1, 4.2) → accepted deny-side gate + value-domain + orthogonality |
| examples/README.md:572 | "auto_status is rejected — use non_committing" | REWRITTEN (Edit 5.1) → accepted deny-side gate, orthogonal to non_committing |

**Net-new auto_status references introduced by this edit-set** (all consistent with
"accepted, deny-side FAIL gate"): workflow-graph.md WG-053 + WG-031 + §16.1 row;
handler-contract.md HC-068; execution-model.md EM-027 sentence + EM-068 (§7.5.6);
examples/authoring-notes.md §1.2 + table; examples/README.md bullet;
workspace-model.md §6.2 row + WM-013e line + §4.7 informative clause.

**ZERO surviving text calls auto_status "reserved" or "rejected" or steers authors to
non_committing as a substitute**, EXCEPT the single frozen revision-history line
handler-contract.md:1440, which is an immutable audit record describing a *past*
documentation pattern (not a live normative claim) and is intentionally left
untouched per the file-discipline rule that prior revision-history rows are immutable.

### Recommended NEW revision-history rows (add at apply-time)
- **workflow-graph.md** §revision history: "auto_status v2 — new WG-053 (deny-side outcome-derivation, implementer-class agentic); WG-041 reserved-block REPLACED with accepted/orthogonal framing; WG-031 reserved-set + position-rules add auto_status (value domain {true,false}, non-boolean = ingest error); §16.1 vocab-diff WG-041 row rewritten + WG-053 row added. Documents shipped v1 (hk-oo4 / 5c5b15ef) + adds C2 carrier. Refs: <bead>."
- **handler-contract.md** §revision history: "New HC-068 (§4.2a) — `.harmonik/auto_status.json` daemon-validated deny-side INPUT mirroring review.json/ReadReviewVerdict; status must be FAIL, failure_class ∈ the six, compilation_loop→structural per HC-059; daemon retains authority; gitignored; no mid-loop archival; C3 deferred. Refs: <bead>."
- **execution-model.md** §revision history: "New EM-068 (§7.5.6) deny-side outcome-derivation gate (C1 work-product inspection + C2 validated marker; deny-side only; daemon authority EM-005c/HC-059; no reviewer-loop re-entry; mechanism-tagged zero-LLM per EM-039/AR-006). EM-027 (§4.6) Outcome-spine additive sentence. Refs: <bead>."
- **workspace-model.md** §revision history: "§6.2 adds `.harmonik/auto_status.json` canonical-path row; WM-013e gitignore set adds the same; §4.7 adds informative auto_status.json lifecycle clause mirroring review.json. Refs: <bead>."
