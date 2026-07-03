# Attractor + kilroy Parity — Decomposition

> Pass 2 (`decompose`) of the `attractor-parity` spec work. Maps the five in-scope capabilities from `01-problem-space.md` to spec/code components with their normative changes, dependencies, and sequencing. Grounded in the parity research (`/tmp/sdlc-corpus/_parity-research.md` §4 clean-add matrix) and the DOT specs.

## 1. Decomposition strategy

One **component per capability** — they map 1:1 to the in-scope beads plus E1, because the research already established each is an independent clean-add. The components cluster on three surfaces: the **DOT vocabulary** (`workflow-graph.md` WG-002 attr rows + WG-031 reserved set), the **handler dispatch** (`handler-contract.md` anchors + `execution-model.md` §7.5.4 table), and the **launch surface** (`dispatchDotAgenticNode` / `buildClaudeLaunchSpec` / EM-012b resolution chain).

The one cross-component coupling worth calling out up front: **(B) inline prompt and (C) non-committing mode both edit `dispatchDotAgenticNode`** and its launch-spec build. They must be sequenced (or co-designed) to avoid two concurrent edits to the same function. Everything else is independent.

| # | Component | Capability | Bead | Surface | Spec change? |
|---|---|---|---|---|---|
| A | Tool/shell node | shell `tool_command` exec + exit-code mapping | hk-l8rpd (KEYSTONE) | DOT vocab + handler dispatch | **YES** |
| B | Inline per-node prompt | `prompt` attr → brief | hk-sdnzj | DOT vocab + launch | **YES** (attr) + code |
| C | Non-committing agentic mode | relax HEAD-advance invariant | hk-69asi | DOT vocab + launch | small attr + code |
| D | Per-node model/effort | per-node model resolution layer | hk-q8nqr | DOT vocab + launch | minor attr; mostly code |
| E | Graph goal + template params | `goal` + `__PARAM__` substitution | E1 (new) | DOT vocab + launch | additive (permissive) + code |

## 2. Component specifications

### A — Tool/shell node (hk-l8rpd) — KEYSTONE

**Spec areas:** `workflow-graph.md` §4 WG-002 (catalog) + §10 WG-031 (reserved set); `handler-contract.md` (new shell-handler anchor + §4.5/§8 exit-code→sentinel mapping); `execution-model.md` §7.5.4 EM-058 (dispatch-table); resolves §13 OQ-WG-007 / D17.

**Normative change:**
- WG-002: add `tool_command` (string, the shell command) and `timeout` (duration) to the **`non-agentic`** node's optional-attr list. NO new node type — rides the existing `non-agentic` category per OQ-WG-007 (so no schema major bump). Add both names to the WG-031 reserved set (the dispatcher consumes them, so they must be strict-position).
- New `handler-contract.md` anchor: the shell/tool handler — invocation (exec the command in the run's worktree with the timeout), boundary-classification tags (`io-determinism`, `replay-safety`), and what it emits (a `default`-kind Outcome).
- New normative **exit-code → `outcome.status` + `failure_class`** mapping table: `0 → SUCCESS`; non-zero → `FAIL` with `failure_class=deterministic` (v1 floor); timeout-kill → `FAIL` with `failure_class=transient` (or `canceled` — design decides); signal-kill → `canceled`. Must be mechanism-tagged (EM §8 / handler-contract §4.4), aligning with the six-class enum.
- EM-058 dispatch-table: the `non-agentic` row's dispatch action covers "invoke the handler referenced by `handler_ref`" — add a sub-note that the shell handler reads `tool_command`/`timeout` and applies the exit-code mapping.

**Dependencies / sequencing:** FIRST. ~Half of all kilroy nodes need it; the loop-counter circuit-breaker idiom (E3, covered by traversal_cap) is inert without a working tool node. Blocks meaningful end-to-end kilroy execution. No dependency on B–E.

**SPEC vs code:** SPEC change (WG-002 row + handler anchor + mapping table + EM-058 note). Code: the real exec branch in `dot_cascade.go` NodeTypeNonAgentic (today it synthesizes SUCCESS at `dot_cascade.go:198-203`) + the exit-code classifier.

### B — Inline per-node prompt (hk-sdnzj)

**Spec areas:** `workflow-graph.md` §4 WG-002 (add `prompt` to `agentic` optional attrs) + WG-031 reserved set; `handler-contract.md` §4.2 (how `prompt` threads into the brief).

**Normative change:**
- WG-002: add `prompt` (string) to the `agentic` node's optional-attr list; add to WG-031 reserved set.
- handler-contract: specify that when a node carries `prompt`, it becomes the node's brief — overriding (or coexisting with, design decides) the bead-derived `beadTitle`/`beadDescription`. The `agent-task.md` payload (CHB-028) is the threading point. No change to the Outcome contract.

**Dependencies / sequencing:** SECOND. Independent of A but pairs naturally with it to make a multi-box pipeline runnable (each box gets a distinct brief). **Shares `dispatchDotAgenticNode` / `buildClaudeLaunchSpec` with C** — sequence B before C (or co-design) to avoid concurrent edits.

**SPEC vs code:** SPEC (WG-002 attr + handler threading note). Code: thread `prompt` through `dispatchDotAgenticNode` → `buildClaudeLaunchSpec` → agent-task payload.

### C — Non-committing agentic mode (hk-69asi)

**Spec areas:** `workflow-graph.md` §4 WG-002 (add `non_committing` / `auto_status` to `agentic` optional attrs) + WG-031 reserved set; `handler-contract.md` §4.2a HC-058 (Outcome obligation, if a clarifying note is needed — SUCCESS-without-commit is already legal).

**Normative change:**
- WG-002: add a `non_committing` (or `auto_status`, design picks the spelling — see OQ-3) boolean optional attr to `agentic` nodes; add to WG-031 reserved set.
- Specify that an agentic node so marked returns SUCCESS by producing a work product (`.ai/*` working files / a valid Outcome) **without requiring HEAD to advance**. The over-strict `postHeadSHA == preHeadSHA → hard-fail` invariant (`dot_cascade.go:589`, "implementer didn't advance HEAD") is relaxed to a per-node mode. This is *removing* an over-strict invariant, not adding an abstraction — the Outcome contract (EM-005) already permits SUCCESS without a commit.

**Dependencies / sequencing:** THIRD. Depends on the same `dispatchDotAgenticNode` surface as B — sequence after B. Mostly code (relax the invariant) + a small attr addition.

**SPEC vs code:** small SPEC (one attr + a clarifying note that SUCCESS-without-commit is legal for this mode). Code: gate the HEAD-advance check on the attr.

### D — Per-node model/effort selection (hk-q8nqr)

**Spec areas:** `workflow-graph.md` §4 WG-002 (add `model` / `effort` / `class` to `agentic` optional attrs) + WG-031 reserved set; `execution-model.md` §4.3 EM-012b (add a per-node layer to the model-resolution chain).

**Normative change:**
- WG-002: add `model` and `effort` (and optionally `class`) optional attrs to `agentic` nodes; add to WG-031 reserved set.
- EM-012b: extend the existing model-resolution chain (which already produces `LaunchSpec.model_preference` per HC-006, consumed as `--model`/`--effort` argv per HC-055/HC-055a) with a **per-node layer** that takes precedence over the run-level default. This *reuses* the existing plumbing — no parallel mechanism. `resolvedModel`/`resolvedEffort` are already plumbed per-node into `dispatchDotAgenticNode` (currently constant).
- `class`-based stylesheet (kilroy's `model_stylesheet` + `class="hard"`) is INFORMATIVE at v1 unless research/design pins it normative (OQ-2). Also fold in the E4 failure_class vocabulary-alignment note (`transient_infra` vs `transient`; `outcome=success` shorthand) — a documented authoring convention or a compat shim (SC7).

**Dependencies / sequencing:** INDEPENDENT, parallelizable, can land any time. P3 (quality/cost tuning, not correctness).

**SPEC vs code:** minor SPEC (WG-002 attrs + EM-012b layer) if framed normatively; could be near-pure code if `class`/stylesheet is informative. Code: resolve the per-node model before launch in `dispatchDotAgenticNode`.

### E — Graph goal + template-param substitution (E1)

**Spec areas:** `workflow-graph.md` (new graph-level `goal` attr + a template-param substitution surface, §10/§11 area); the run-launch surface (loader / launch).

**Normative change:**
- Add a graph-level `goal` (string) attr. Under the permissive policy (WG-031) `goal` is already accepted as an unknown graph attr, but making it dispatcher-consumed means adding it to the reserved set + specifying its semantics.
- Specify a **template-param substitution** surface: `__PLACEHOLDER__` tokens (e.g. `__ISSUE_NUMBER__`, `__SENTRY_SHORT_ID__`, `__GITHUB_REPO__`) substituted from a param map supplied at launch. Define the substitution point (loader vs. launch — OQ-1, lean launch-time so the graph stays reusable) and the param-map source.

**Dependencies / sequencing:** INDEPENDENT, parallelizable. Touches the loader + launch surface only; additive.

**SPEC vs code:** additive SPEC (`goal` attr + substitution-surface definition). Code: a param-substitution pass at load/launch.

## 3. Sequencing summary

**Foundation chain (sequential, shared surface):**
1. **A — Tool/shell node** (KEYSTONE; unblocks ~half the pipeline; no deps).
2. **B — Inline prompt** (pairs with A; shares `dispatchDotAgenticNode` with C).
3. **C — Non-committing mode** (after B; same function surface).

**Independent (parallelizable, any time):**
4. **D — Per-node model/effort** (P3; reuses EM-012b chain).
5. **E — Goal + template params** (loader/launch only).

**Canonical example (after A+B+C):** a multi-node `specs/examples/` `.dot` exercising tool nodes + inline prompts + a non-committing node, with its WG-037 sidecar (SC8). Validates the vocabulary end-to-end.

## 4. What is NOT a component (out of scope, per §3 of problem-space)

- Parallel fan-out / join — the only load-bearing rework; deferred per EM-059 / architecture.md §4.6 / WG-001 closed enum. Not needed by any live kilroy pipeline.
- Gate evaluator (hk-karlz) — orthogonal; kilroy uses `diamond` edge-condition checks, not policy Gates.
- A new node type — the tool capability rides the existing `non-agentic` type (OQ-WG-007), so no schema major bump.

## 5. Cross-component notes for the research/design passes

- **All five attrs land additively under WG-031.** Each dispatcher-consumed attr (`tool_command`, `timeout`, `prompt`, `non_committing`/`auto_status`, `model`/`effort`, graph-level `goal`) must be added to the WG-031 **reserved set** so it is strict-position — an author who misplaces it gets an ingest error rather than a silently-ignored permissive attr. This is part of each component's spec change.
- **Schema stays minor.** No new node type, no new edge field, no new enum member ⇒ `schema_version` stays `1`, N-1 readable (WG-034). Confirm in the spec-draft pass.
- **The Outcome envelope is untouched.** Every component emits a `default`-kind Outcome (EM-005a). The tool node's failure_class is set via the exit-code classifier (daemon-side back-fill path, WG-018); the non-committing node emits SUCCESS directly. No component needs a new Outcome `kind` or `payload`.
- **Open questions OQ-1..OQ-4** (in `01-problem-space.md` §7) are the design-pass agenda: substitution point (A/E), stylesheet normativity (D), `non_committing` vs `auto_status` spelling (C), exit-code mapping granularity (A).
