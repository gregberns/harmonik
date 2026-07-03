# Pass 7 — Tasks

> Decomposition of phase-3-dot ("DOT" = workflow-graph runtime) into executable tasks. Inputs: pass-5 drafts C1 (workflow-graph spec, NEW), C2 (execution-model §7.5 + EM-007 amendment + §10.1 lift), C3 (handler-contract §4.2a/§5.6 + EM-005 v0.3.4), C4 (control-points §4.12/§4.13/§6.1.8 + CP-038a), C5 (canonical example + README), plus pass-6 integration review (1 BLOCKER, 6 SHOULD-FIX after CI-8 addendum, 6 NIT).
>
> Plain-English shorthand used below:
> - **C1 / workflow-graph.md** — the new normative spec defining the DOT artifact (nodes, edges, validation).
> - **C2 / EM §7.5** — execution-model amendments that wire the DOT loader into the daemon's workflow-mode toggle.
> - **C3 / HC §4.2a + EM-005** — handler Outcome envelope additions (`preferred_label`, `failure_class`, `gate_decision`, `context_updates`).
> - **C4 / CP §4.12-§4.13** — binding control-points to node types; `gate` nodes carry `gate_ref` + `handler_ref`; `GateDecisionPayload`.
> - **C5 / examples/review-loop.dot** — canonical pinned example (implementer → reviewer → gate → loop/close).
> - **Cascade** — the 5-step edge-selection: filter by outcome → evaluate edge conditions → cap-hit → no-match → unconditional fallback.
>
> Task IDs are stable; later beads MUST reference them verbatim.

## 1. Spec-transcription tasks

These tasks take the five pass-5 drafts (currently on the kerf bench at `~/.kerf/projects/gregberns-harmonik/phase-3-dot/05-spec-drafts/`) and apply them to the real `specs/` tree, folding in the pass-6 remediations. Each transcription task is a single commit per component.

### T-SPEC-C1 — Land `specs/workflow-graph.md` (NEW)

- **Source draft:** `05-spec-drafts/C1-workflow-graph.md` (594 lines).
- **Target file:** `specs/workflow-graph.md` (new).
- **Apply during transcription:**
  - **SHOULD-FIX (Contradiction 2):** Rename C1 §4 WG-006 attribute `workflow_ref` → `sub_workflow_ref`. Update C1 §10 WG-031 reserved-attribute set (line 378). Update C1 §4 WG-002 table row (line 87).
  - **SHOULD-FIX (CI-2):** Drop `policy_ref` from C1 §4 WG-002 optional-attribute lists on all four node types. Add `skills_ref` and `freedom_profile_ref` per CP-055. Keep `policy_ref` in §10 WG-031 reserved set with a note: "reserved-and-rejected name; see [control-points.md §4.12 CP-056]."
  - **SHOULD-FIX (CI-3):** Add a new WG-NNN under §10 (suggested WG-031a) declaring `context_keys` as a graph-level DOT attribute, comma-separated identifier list per HC-062. Close OQ-WG-002 (or narrow it to "type-pinning of declared context keys is still open at v1").
  - **NIT (CI-4):** Add a §3 glossary entry locking "workflow graph" as the canonical noun; "DOT" used only as a qualifier for the on-disk artifact format.
  - **NIT (CI-5):** Reconcile C1 WG-018 wording with C3 EDIT B / HC-058: handler-side `failure_class` is OPTIONAL on FAIL and absent otherwise; daemon-side post-classifier MUST populate it on FAIL. Two-sided contract phrased explicitly.
  - **NIT (CI-7):** Pick canonical name `start_node` (DOT attribute) / `start_node_id` (parsed record field). Reference in WG-027.
- **Acceptance:**
  - `specs/workflow-graph.md` exists with WG-001 … WG-038.
  - `grep -n "workflow_ref" specs/workflow-graph.md` returns nothing (only `sub_workflow_ref`).
  - C1's WG-031 reserved set includes `context_keys`.

### T-SPEC-C2 — Apply C2 to `specs/execution-model.md`

- **Source draft:** `05-spec-drafts/C2-execution-model-dot.md` (183 lines).
- **Target file:** `specs/execution-model.md` — three edit sites: (a) new §7.5 binding block; (b) in-place EM-007 amendment in §4.2; (c) in-place §10.1 conformance-lift edit.
- **Apply during transcription:**
  - **SHOULD-FIX (CI-8 — pass-6 addendum):** Edit EM §6.1 `ENUM NodeType` block (~line 953) to drop `control-point`. New value set: `{agentic, non-agentic, gate, sub-workflow}`. Per C1 §4 WG-001.
  - **SHOULD-FIX (Contradiction 4):** Drop the BI-005 `workflow_ref` reference from EM-055 step 1. Source the `.dot` artifact path from per-daemon configuration / fallback chain only at v1. File a follow-up bead for a future BI-NNN bead-schema extension (note inline).
  - **SHOULD-FIX (CI-1):** Rewrite all C1-by-named-anchor references in C2 to use WG-NNN IDs per the CI-1 mapping table (e.g., "§Schema" → "§10 WG-031, §11 WG-033"; "§Node Types" → "§4 WG-001/WG-002"; etc.).
  - **NIT — C2 citation imprecision:** Rewrite "[handler-contract.md §Outcome]" → "[handler-contract.md §4.2a HC-058]" or "[handler-contract.md §6.1 RECORD Outcome]". Rewrite "[control-points.md §Node-Type Binding]" → "[control-points.md §4.12 CP-053/CP-054]".
- **Acceptance:**
  - EM §6.1 ENUM NodeType has four values.
  - EM §7.5 exists with EM-055 … EM-059 (or whatever final allocation lands; C2 assigns up to EM-061).
  - EM-007 carries the "handler_ref required on gate" amendment.
  - No occurrence of "BI-005" within 30 lines of "workflow_ref" anywhere in EM.
- **Conflicts:** T-SPEC-C3 also edits `specs/execution-model.md` (EM-005 v0.3.4). Serialize: T-SPEC-C2 first, then T-SPEC-C3 rebases.

### T-SPEC-C3 — Apply C3 to `specs/handler-contract.md` and `specs/execution-model.md`

- **Source draft:** `05-spec-drafts/C3-handler-contract-outcome.md` (300 lines).
- **Target files:** `specs/handler-contract.md` (new §4.2a + §5.6) AND `specs/execution-model.md` (EM-005 v0.3.3 → v0.3.4 bump + EM-005a enum extension + new EM-005b/c).
- **Apply during transcription:**
  - **NIT (CI-5):** Align HC-058 OPTIONAL-on-FAIL, C1 WG-018 MUST-on-FAIL-after-daemon-classifier, and EDIT B "present ONLY when FAIL" into one consistent two-sided contract (handler-side optional, daemon-side mandatory back-fill).
  - **NIT (CI-6):** Amend EM §6.1 `RECORD Outcome.payload` row at `execution-model.md:1086` to widen its type, OR amend the §6.1 INFORMATIVE note at `execution-model.md:1103` to clarify `VerdictPayload` as a union alias of `VerdictEvent | GateDecisionPayload`. Prefer the latter (preserves EM-005a's umbrella-alias design).
  - **CITATION CLEANUP:** Rewrite C3's "[workflow-graph.md §C1 design — context-key registry]" → "[workflow-graph.md §10 WG-031]" (now that C1 has landed).
- **Acceptance:**
  - HC §4.2a present with HC-058 … HC-062.
  - HC §5.6 present (context-update discipline).
  - EM-005 carries v0.3.4 schema-version bump rationale.
  - EM-005a enum mentions `gate_decision` kind.
  - EM-005b, EM-005c IDs present.
- **Dependencies:** Must merge AFTER T-SPEC-C2 (same file).

### T-SPEC-C4 — Apply C4 to `specs/control-points.md` and `specs/event-model.md`

- **Source draft:** `05-spec-drafts/C4-control-points-binding.md` (172 lines).
- **Target files:** `specs/control-points.md` (new §4.12, §4.13, §6.1.8, §4.7 CP-038a amendment, §6.5 event additions) AND `specs/event-model.md` §6.3 (payload schema for new events — verify §6.3 is the canonical payload-schema home; if not, follow the file's existing convention).
- **Apply during transcription:**
  - **NIT (C4 → C2 citation refinement):** Rewrite "[execution-model.md §7.5 EM-007 amendment]" → "[execution-model.md §4.2 EM-007 amendment + §7.5]".
  - **CITATION CLEANUP:** Rewrite "[handler-contract.md §Outcome / §4.x / §4.11]" → "[handler-contract.md §4.2a + §6.1 RECORD Outcome]" and "[handler-contract.md §4.11]" left as-is (it's a real section).
  - **NIT (C4 → C1 citation refinement):** "[workflow-graph.md §4.x Node-type catalog]" → "[workflow-graph.md §4 WG-001/WG-002]".
- **Acceptance:**
  - CP §4.12, §4.13, §6.1.8 sections present.
  - CP-053 … CP-058 + CP-038a IDs present.
  - `GateDecisionPayload` declared exactly once (in §6.1.8 CP-058).
  - `event-model.md` §6.3 carries the new payload schemas.
- **Conflicts:** No other transcription task touches `specs/control-points.md`. May run parallel with T-SPEC-C1.

### T-SPEC-C5 — Land `specs/examples/` directory + canonical example

- **Source drafts:** `05-spec-drafts/C5-review-loop.dot` (106 lines) + `05-spec-drafts/C5-examples-README.md` (59 lines).
- **Target files:** `specs/examples/review-loop.dot` (new) and `specs/examples/README.md` (new).
- **MUST RUN AFTER T-FIX-C5-BLOCK** — the BLOCKER patch makes C5 round-trip C1's validator. Without that, T-SPEC-C5 lands a non-conforming example.
- **Apply during transcription:**
  - The pre-patched C5 already (after T-FIX-C5-BLOCK) uses `type` (not `node_type`), uppercase verdicts, real C1 anchor IDs (no `WG-T03`), and `start_node="start"` graph-level attribute.
  - **CITATION CLEANUP (Contradiction 6):** Replace `§WG-T03` references → `§8 WG-021..WG-023`. Already folded into T-FIX-C5-BLOCK if that task takes the wider sweep.
  - **CITATION CLEANUP:** README's "[handler-contract.md §Handler Binding]" → "[handler-contract.md §4.1 HC-001]"; "[handler-contract.md §Outcome]" → "[handler-contract.md §4.2a + §6.1 RECORD Outcome]".
- **Acceptance:**
  - `specs/examples/review-loop.dot` parses through the C1 validator (T-IMPL-002 validator) with zero diagnostic output above WARN severity.
  - All node `type` values are in `{agentic, non-agentic, gate, sub-workflow}`.
  - All edge-condition verdict string literals are uppercase (`APPROVE` / `REQUEST_CHANGES` / `BLOCK`).
  - README's anchor list resolves against shipped specs (every cited `§X` or `WG-NNN` exists).
- **Dependencies:** T-FIX-C5-BLOCK (must precede); T-SPEC-C1 (validator targets the merged C1 spec); ideally T-IMPL-001 + T-IMPL-002 land in the same wave to validate the example mechanically.

## 2. Remediation tasks (pass-6 carry-in)

### T-FIX-C5-BLOCK — Patch C5 to round-trip C1 (BLOCKER — Contradiction 1)

- **Source / Target:** `05-spec-drafts/C5-review-loop.dot` + `05-spec-drafts/C5-examples-README.md` (kerf bench).
- **Edits:**
  1. Rename attribute `node_type` → `type` on all five nodes (`start`, `implementer`, `reviewer`, `close`, `close-needs-attention`).
  2. Re-categorize node types into C1's closed enum `{agentic, non-agentic, gate, sub-workflow}`:
     - `start` → `non-agentic` with inert/no-op handler binding, OR drop the explicit `start` node entirely and rely on `start_node` graph-level attribute pointing at `implementer`. Recommend the latter for minimality.
     - `close`, `close-needs-attention` → `non-agentic` with `handler_ref="noop"`.
  3. Add `start_node="start";` (or `start_node="implementer";` if `start` is dropped) to the graph-level attribute block at line 30-31.
  4. Update verdict-string literals on the three condition edges to uppercase: `'APPROVE'`, `'REQUEST_CHANGES'`, `'BLOCK'` (Contradiction 3).
  5. Update README line 21 to reflect the renamed attribute. Update README line 20 from `§WG-T03` to `§8 WG-021..WG-023` (Contradiction 6).
- **Acceptance:** The patched `C5-review-loop.dot` would round-trip C1 §9 WG-024/WG-001/WG-002 as written.
- **Dependencies:** None — this is wave 0.
- **Priority:** P0. Blocks T-SPEC-C5 and downstream test bead hk-isp3y.

### T-FIX-SHOULD-01 — C1 `workflow_ref` → `sub_workflow_ref` rename

- Folded into T-SPEC-C1. Keep as a standalone bullet so the changelog audit-trail is complete; closes when T-SPEC-C1 lands.

### T-FIX-SHOULD-02 — C5 verdict labels uppercase

- Folded into T-FIX-C5-BLOCK step 4.

### T-FIX-SHOULD-03 — C2 BI-005 / workflow_ref drop

- Folded into T-SPEC-C2.

### T-FIX-SHOULD-04 — C2 named-anchor → WG-NNN rewrites (CI-1)

- Folded into T-SPEC-C2.

### T-FIX-SHOULD-05 — C1 `policy_ref` deprecation note (CI-2)

- Folded into T-SPEC-C1.

### T-FIX-SHOULD-06 — `context_keys` declaration site promotion (CI-3)

- Folded into T-SPEC-C1 (new WG-031a) + T-SPEC-C3 (re-cite to WG-031a).

### T-FIX-SHOULD-07 — EM `ENUM NodeType` drop `control-point` (CI-8)

- Folded into T-SPEC-C2.

### T-FIX-NIT-01 — Terminology unification "workflow graph" (CI-4)

- Folded into T-SPEC-C1 §3 glossary.

### T-FIX-NIT-02 — `failure_class` three-shape alignment (CI-5)

- Folded into T-SPEC-C1 + T-SPEC-C3.

### T-FIX-NIT-03 — EM-005a `payload` type-widening (CI-6)

- Folded into T-SPEC-C3.

### T-FIX-NIT-04 — `start_node` / `start_node_id` naming (CI-7)

- Folded into T-SPEC-C1 + T-FIX-C5-BLOCK.

### T-FIX-NIT-05 — Citation imprecision sweep across C2/C3/C4/C5

- Folded into each respective T-SPEC-* task.

### T-FIX-NIT-06 — Confirm `event-model.md` §6.3 is payload-schema home

- Folded into T-SPEC-C4 acceptance.

> All remediation work is folded into spec-transcription tasks for cohesion. Listing them above with stable IDs preserves traceability for the pass-6 review record.

## 3. Implementation tasks (Go)

### T-IMPL-001 — DOT-file parser + AST

- **Spec traceability:** WG-031 (mixed strict/permissive), WG-032 (AST retention), WG-033 (schema_version), WG-009 (edge fields), WG-002 (node attribute catalog), WG-038 (path resolution conventions).
- **First impl file:** `internal/workflow/dot/parser.go` (new package). Optionally `internal/workflow/dot/ast.go` for the AST type.
- **Pre-impl audit (per pass-7 reviewer):** `internal/workflowvalidator/` already contains `dotparser.go` + `validator.go`. The implementer MUST audit those before writing new code — decide whether to (a) extend the existing package, (b) move it under `internal/workflow/dot/` and rename, or (c) write the new package alongside and migrate later. Document the choice in the commit message. Don't ship parallel parsers.
- **Acceptance:**
  - Parses a valid `.dot` artifact into a typed AST.
  - Preserves unknown permissive attributes per WG-032 (round-trip retention).
  - Surfaces parse errors with file:line.
  - Round-trips `specs/examples/review-loop.dot` (post-T-FIX-C5-BLOCK).
- **Dependencies:** T-SPEC-C1 (spec must exist as authority).
- **Granularity:** ~1-2 days. OK.

### T-IMPL-002 — Workflow-graph validator

- **Spec traceability:** WG-001 (closed node-type enum), WG-005/WG-006 (gate/sub-workflow attrs), WG-013–WG-016 (edge-condition dialect), WG-018 (failure_class top-level), WG-022/WG-023 (terminal nodes), WG-024 (reserved-attr strictness), WG-025 (edge-condition strictness), WG-026 (reference resolution), WG-027 (well-formedness — start_node, single entry, every non-terminal reachable), WG-028 (cycle bounding), WG-029 (sub-workflow acyclicity), WG-030 (axis-tag consistency), WG-031 + WG-031a (`context_keys`), WG-034 (N-1 readability), WG-035 (`version` vs `schema_version`).
- **First impl file:** `internal/workflow/dot/validator.go`.
- **Acceptance:**
  - Rejects unknown `type` values with `ErrDeterministic`.
  - Rejects `policy_ref` per CP-056 (cross-spec hook).
  - Rejects unreachable non-terminal nodes per WG-027.
  - Rejects edge-condition LHS outside WG-014 whitelist.
  - Rejects edge-condition operators outside `==` / `&&` per WG-013/WG-016.
  - Returns a structured diagnostic list (severity + file:line + WG-NNN code).
  - `specs/examples/review-loop.dot` validates clean (zero errors, zero warnings).
- **Dependencies:** T-IMPL-001, T-SPEC-C1, T-SPEC-C4 (CP-056 cross-ref).
- **Granularity:** ~2 days. Acceptable upper bound.

### T-IMPL-003 — Workflow-graph loader (filesystem → validated graph)

- **Spec traceability:** EM-055 (artifact resolution: per-daemon config / fallback only at v1 per pass-6 Contradiction 4 resolution), WG-026 (reference resolution), WG-038.
- **First impl file:** `internal/workflow/loader.go`.
- **Acceptance:**
  - Reads `.dot` artifact from a configured path; falls back through a documented chain (per T-SPEC-C2).
  - Calls parser + validator; returns a `*WorkflowGraph` or a typed error.
  - Cacheable across runs of the same workflow `name` + `version`.
- **Dependencies:** T-IMPL-001, T-IMPL-002.
- **Granularity:** ~half-day. OK.

### T-IMPL-004 — Daemon workflow-mode wiring (loader → workloop)

- **Spec traceability:** EM-055 (loader integration), EM-056/EM-057 (workflow_mode toggle plumbing — exact IDs per final EM allocation), EM-007 amendment (handler_ref on gate now required).
- **First impl file:** `internal/daemon/workflow_mode.go` (new) + edits to `internal/daemon/workloop.go`.
- **Acceptance:**
  - When `workflow_mode=dot`, daemon loads the configured `.dot` artifact at run-start.
  - On load failure, fails the run with `ErrDeterministic` and emits `run_failed` with `failure_class=workflow_load`.
  - `workflow_mode=builtin` path is unchanged (regression-safe).
- **Dependencies:** T-IMPL-003, T-SPEC-C2.
- **Granularity:** ~1 day. OK.

### T-IMPL-005 — Outcome envelope: `preferred_label`, `failure_class`, `gate_decision`, `context_updates`

- **Spec traceability:** HC-058 (Outcome surface table), HC-059 (daemon back-fill), HC-060/HC-061, EM-005 v0.3.4, EM-005a (kind enum + `gate_decision`), EM-005b/c.
- **First impl file:** `internal/handler/outcome.go`. JSON schema/codec at `internal/handler/outcome_codec.go`.
- **Acceptance:**
  - Outcome struct carries the four new fields with correct optionality per HC-058.
  - JSON round-trip preserves all fields.
  - Schema-version bump (`v0.3.4`) honored — older payloads with `v0.3.3` accepted with `gate_decision`/etc. zero-valued per N-1 readability (WG-034 / EM-005 schema-version policy).
- **Dependencies:** T-SPEC-C3.
- **Granularity:** ~1 day. OK.

### T-IMPL-006 — Daemon-side failure-class classifier (back-fill)

- **Spec traceability:** HC-059 (daemon back-fills when handler omits), WG-018 (post-classifier MUST populate on FAIL), EM §8 six-class taxonomy.
- **First impl file:** `internal/daemon/failure_class.go`.
- **Acceptance:**
  - Handlers emitting FAIL without `failure_class` get a back-filled value per EM §8 rules.
  - Handlers emitting FAIL with `failure_class` are honored.
  - Non-FAIL outcomes have `failure_class` cleared (post-T-FIX-NIT-02 wording).
  - Emits structured-log line on back-fill for observability.
- **Dependencies:** T-IMPL-005, T-SPEC-C3 (CI-5 alignment), T-SPEC-C1 (WG-018).
- **Granularity:** ~1 day. OK.

### T-IMPL-007 — Edge-condition evaluator

- **Spec traceability:** WG-013 (dialect), WG-014 (LHS whitelist), WG-015 (RHS literal types), WG-016 (dialect distinct from guards).
- **First impl file:** `internal/workflow/dot/edges.go`.
- **Acceptance:**
  - Evaluates `outcome.<field> == <literal>` and `&&`-conjunctions per WG-013.
  - Rejects out-of-whitelist LHS at evaluation time as `ErrDeterministic` (defense-in-depth — validator should have caught at load).
  - LHS includes `outcome.status`, `outcome.preferred_label`, `outcome.failure_class`, registered `context.<key>` per WG-014.
  - Returns `(matched bool, err error)`; deterministic on equal inputs.
- **Dependencies:** T-IMPL-005 (reads from Outcome), T-IMPL-002 (validator dialect).
- **Granularity:** ~1 day. OK.

### T-IMPL-008 — Cascade engine: outcome → next-node decision

- **Spec traceability:** WG-010 (5-step cascade), WG-011 (unconditional-edge fallback invariant), WG-012 (no-match-set fallback), WG-007 (legal outcome statuses per node type), C2 §7.5.2 dispatch table.
- **First impl file:** `internal/workflow/dispatcher.go`.
- **Acceptance:**
  - Implements the 5-step cascade: (1) filter edges by outcome.status, (2) evaluate edge-conditions, (3) cap-hit handling, (4) no-match → fallback, (5) unconditional edge.
  - Returns `next_node_id` or terminal-state indication.
  - Emits `node_dispatch_decided` event with `next_node_id` populated.
  - Cap-hit raises `completion_reason=cap_hit` per EM-015d-RFD wording.
- **Dependencies:** T-IMPL-002, T-IMPL-005, T-IMPL-007.
- **Granularity:** ~1.5-2 days. OK.

### T-IMPL-009 — Cascade context-updates plumbing

- **Spec traceability:** HC-062 (`context_keys` declaration), C3 EDIT D (context_updates surface), C3 §5.6 (context-update discipline), C2 §7.5.2 cascade.
- **First impl file:** `internal/workflow/context_updates.go`.
- **Acceptance:**
  - Reads `outcome.context_updates` (map of key → value).
  - Validates each key is in the workflow's declared `context_keys` (warn-and-drop unregistered keys per D8).
  - Threads merged context to the next node dispatch.
  - Emits `context_updated` event with the diff.
- **Dependencies:** T-IMPL-005, T-IMPL-002 (validator must register `context_keys`), T-SPEC-C3.
- **Granularity:** ~1 day. OK.

### T-IMPL-010 — Gate node dispatch + `GateDecisionPayload`

- **Spec traceability:** CP-053 (gate node = Gate-kind ControlPoint dispatch surface), CP-054 (`gate_ref` + `handler_ref`), CP-058 (`GateDecisionPayload`), §6.1.8 RECORD block, EM-005a `kind=gate_decision`.
- **First impl file:** `internal/handler/gate_dispatch.go`.
- **Acceptance:**
  - Gate-node handler invocation receives the bound Gate ControlPoint via `gate_ref`.
  - Handler returns Outcome with `kind=gate_decision` and `payload: GateDecisionPayload`.
  - Cascade reads `outcome.gate_decision` and routes accordingly.
  - Emits `gate_decision_recorded` event per CP §6.5.
- **Dependencies:** T-IMPL-005, T-IMPL-008, T-SPEC-C4.
- **Granularity:** ~1.5 days. OK.

### T-IMPL-011 — Sub-workflow node dispatch (nested run)

- **Spec traceability:** WG-006 (`sub-workflow` attrs: `sub_workflow_ref` + `workflow_version`), WG-029 (sub-workflow acyclicity), EM-007 amendment (no handler_ref on sub-workflow — daemon dispatches directly).
- **First impl file:** `internal/workflow/sub_workflow.go` + handler-runtime nested-run hook in `internal/handler/runtime.go`.
- **Acceptance:**
  - Sub-workflow node triggers a nested run with the resolved target graph.
  - Nested-run outcome surfaces to parent's cascade as a normal Outcome.
  - Acyclicity enforced (no graph G transitively invokes G).
  - Emits `sub_workflow_started` and `sub_workflow_completed` events.
- **Dependencies:** T-IMPL-003 (loader), T-IMPL-004 (workflow-mode), T-IMPL-008 (cascade for parent).
- **Granularity:** ~2 days. OK.

### T-IMPL-012 — `policy_ref` rejection at workflow-ingest

- **Spec traceability:** CP-056 (deprecated, MUST reject), C1 WG-002 (reserved name).
- **First impl file:** `internal/workflow/dot/validator.go` (extension of T-IMPL-002).
- **Acceptance:**
  - Any DOT attribute literally named `policy_ref` produces a diagnostic with code `CP-056` and `ErrDeterministic` rejection.
  - Diagnostic suggests `gate_ref` / `skills_ref` / `freedom_profile_ref` per CP-055.
- **Dependencies:** T-IMPL-002, T-SPEC-C4.
- **Granularity:** ~half-day. Could roll into T-IMPL-002, but kept distinct for clear cross-spec traceability to CP-056.

### T-IMPL-013 — CLI `harmonik run --workflow-mode dot --workflow-ref <path>`

- **Spec traceability:** EM-058 (or final allocated EM ID for the CLI surface in C2).
- **First impl file:** `cmd/harmonik/run.go` (extend with `--workflow-mode` and `--workflow-ref` flags).
- **Acceptance:**
  - `harmonik run --workflow-mode dot --workflow-ref ./review-loop.dot --beads hk-XXX` succeeds end-to-end on the canonical example.
  - `--workflow-mode` defaults to `builtin` (regression-safe).
  - Unknown `--workflow-mode` value rejected with usage error.
- **Dependencies:** T-IMPL-004.
- **Granularity:** ~half-day. OK.

### T-IMPL-014 — CLI `harmonik graph validate <path>` (operator-facing)

- **Spec traceability:** Operator-NFR §4.3 needs-attention surfacing; covers the W-3eip explore-bead scope (operator-facing graph validation CLI).
- **First impl file:** `cmd/harmonik/graph.go` (new subcommand).
- **Acceptance:**
  - Reads a `.dot` path, runs parser + validator, prints diagnostics to stdout, exits non-zero on any ErrDeterministic diagnostic.
  - `--json` flag emits the diagnostic list as structured JSON.
  - Documented in `cmd/harmonik/README.md` (or wherever CLI docs live).
- **Dependencies:** T-IMPL-001, T-IMPL-002.
- **Granularity:** ~half-day. OK.

### T-IMPL-015 — `specs/examples/review-loop.dot` as a real Go-test fixture

- **Spec traceability:** WG-036 (canonical examples live in `specs/examples/`), WG-037 (per-example doc sidecar), C5 README's claim that layer-1 static round-trip exists.
- **First impl file:** `internal/workflow/dot/examples_test.go`.
- **Acceptance:**
  - Test loads `specs/examples/review-loop.dot`, parses + validates, asserts zero ErrDeterministic.
  - Test runs in `go test ./internal/workflow/...` and is invoked by the default CI loop.
  - Sidecar README's anchor list is verified by a separate string-match test (best-effort lint).
- **Dependencies:** T-SPEC-C5, T-IMPL-001, T-IMPL-002.
- **Granularity:** ~half-day. OK.

## 4. Test tasks (pre-filed beads)

Each pre-filed bead is listed here as a test task with the impl tasks it validates. No new beads are filed by pass-7.

### T-TEST-hk-fiq55 — Scenario: C1 DOT round-trip

- **Validates:** T-IMPL-001 (parser), T-IMPL-002 (validator), T-IMPL-015 (fixture).
- **Dependencies:** T-IMPL-001, T-IMPL-002, T-SPEC-C1.

### T-TEST-hk-lphyf — Scenario: C2 `workflow_mode=dot` drives `review-loop.dot`

- **Validates:** T-IMPL-003, T-IMPL-004, T-IMPL-013.
- **Dependencies:** T-IMPL-004, T-IMPL-013.

### T-TEST-hk-aoz34 — Scenario: C3 `failure_class` cascade

- **Validates:** T-IMPL-005, T-IMPL-006, T-IMPL-008.
- **Dependencies:** T-IMPL-006, T-IMPL-008.

### T-TEST-hk-yfm05 — Scenario: C4 gate node `GateDecisionPayload`

- **Validates:** T-IMPL-010.
- **Dependencies:** T-IMPL-010, T-IMPL-008.

### T-TEST-hk-isp3y — Scenario: C5 full review-loop cascade incl. cap-hit

- **Validates:** T-IMPL-008 (cap-hit branch of cascade), T-IMPL-013 (CLI dispatch), entire integration.
- **Dependencies:** T-IMPL-010, T-IMPL-011, T-IMPL-013, T-IMPL-015.

### T-TEST-hk-4fvid — Explore: C2 CLI `harmonik run --workflow-mode dot`

- **Validates:** T-IMPL-013 (manual operator workflow + edge cases).
- **Dependencies:** T-IMPL-013.

### T-TEST-hk-6zvki — Explore: C3 `context_keys` warn-and-drop

- **Validates:** T-IMPL-009 (warn-and-drop branch).
- **Dependencies:** T-IMPL-009.

### T-TEST-hk-w3eip — Explore: C1 operator-facing graph validation CLI

- **Validates:** T-IMPL-014.
- **Dependencies:** T-IMPL-014.

### T-TEST-hk-zqr6f — Explore: C4 `skills_ref` vs `policy_ref` symmetry

- **Validates:** T-IMPL-012 (policy_ref rejection) and validator's positive path for `skills_ref` / `freedom_profile_ref`.
- **Dependencies:** T-IMPL-012, T-IMPL-002.

### T-TEST-hk-geype — Explore: C5 README spec-anchor mapping

- **Validates:** T-SPEC-C5 (anchor list resolves); ties to T-IMPL-015's sidecar-anchor lint.
- **Dependencies:** T-SPEC-C5, T-IMPL-015.

## 5. Dependency DAG

```
T-FIX-C5-BLOCK
        │
        ▼
T-SPEC-C1 ── T-SPEC-C4 ── (parallel)
   │              │
   ▼              ▼
T-SPEC-C2 ── (then) T-SPEC-C3            ← serialized: both edit execution-model.md
   │              │
   └──── T-SPEC-C5 ◄── needs T-FIX-C5-BLOCK + T-SPEC-C1
                  │
   ┌──────────────┼──────────────┬──────────────┐
   ▼              ▼              ▼              ▼
T-IMPL-001 ── T-IMPL-002 ── T-IMPL-003 ── T-IMPL-005
                  │              │              │
                  ▼              ▼              ▼
              T-IMPL-012   T-IMPL-004      T-IMPL-006
                                 │              │
                                 ▼              ▼
                            T-IMPL-007 ── T-IMPL-008
                                          /   │   \
                                         ▼    ▼    ▼
                                  T-IMPL-009 T-IMPL-010 T-IMPL-011
                                                │
                                          T-IMPL-013 ── T-IMPL-014
                                                │
                                          T-IMPL-015
                                                │
                                          (test beads consume)
```

No cycles. Every changelog requirement-ID is reachable by at least one implementing task — see §8 coverage matrix.

## 6. Parallelization plan (waves)

- **Wave 0 (serial, blocking):** T-FIX-C5-BLOCK.
- **Wave 1 (spec transcription, partial parallel):**
  - Parallel batch A: T-SPEC-C1, T-SPEC-C4 (different files).
  - Parallel batch B (after A): T-SPEC-C2, T-SPEC-C5 (different files; C5 depends on C1 from A).
  - Serial: T-SPEC-C3 must follow T-SPEC-C2 (both edit `specs/execution-model.md`).
  - **Cross-wave conflict:** T-SPEC-C2 and T-SPEC-C3 both edit `specs/execution-model.md`. Serialize T-SPEC-C2 → T-SPEC-C3.
- **Wave 2 (foundation Go, parallel):** T-IMPL-001, T-IMPL-005 (independent files). Then T-IMPL-002 after T-IMPL-001.
- **Wave 3 (parallel after Wave 2):** T-IMPL-003, T-IMPL-006, T-IMPL-007, T-IMPL-012.
- **Wave 4 (parallel after Wave 3):** T-IMPL-004, T-IMPL-008.
- **Wave 5 (parallel after Wave 4):** T-IMPL-009, T-IMPL-010, T-IMPL-011, T-IMPL-013, T-IMPL-014.
- **Wave 6 (fixture + test beads):** T-IMPL-015 → all T-TEST-* beads in parallel.
- **Wave 7 (cleanup):** Any NIT items not folded into spec-transcription tasks (none expected — all NITs are pre-folded).

## 7. Granularity check

| Task | Granularity verdict |
|---|---|
| T-FIX-C5-BLOCK | OK — concrete 10-line edit |
| T-SPEC-C1 | OK — single-file create + folded remediations |
| T-SPEC-C2 | OK — three edit sites in one file, all spelled out |
| T-SPEC-C3 | OK — two files, listed remediations |
| T-SPEC-C4 | OK — single file, possible event-model.md side-effect noted |
| T-SPEC-C5 | OK — single-directory create |
| T-IMPL-001 | OK |
| T-IMPL-002 | OK — within 2-day cap; tight if validator surface grows |
| T-IMPL-003 | OK |
| T-IMPL-004 | OK |
| T-IMPL-005 | OK |
| T-IMPL-006 | OK |
| T-IMPL-007 | OK |
| T-IMPL-008 | OK — 1.5-2d; if cap-hit handling grows, split into T-IMPL-008a (cascade core) + T-IMPL-008b (cap-hit + completion_reason emission) |
| T-IMPL-009 | OK |
| T-IMPL-010 | OK |
| T-IMPL-011 | OK |
| T-IMPL-012 | OK — could roll into T-IMPL-002 if reviewer prefers |
| T-IMPL-013 | OK |
| T-IMPL-014 | OK |
| T-IMPL-015 | OK |
| T-TEST-* | OK — each bead is already pre-scoped |

No task flagged as needing further decomposition before dispatch. T-IMPL-008 is the largest at ~2 days; split-point identified in advance if needed.

## 8. Coverage matrix (changelog requirement-IDs → implementing tasks)

| Requirement ID | Owning spec | Implementing task(s) |
|---|---|---|
| WG-001 (closed node-type enum) | workflow-graph.md | T-SPEC-C1, T-IMPL-002 |
| WG-002 (node attribute catalog) | workflow-graph.md | T-SPEC-C1, T-IMPL-002 |
| WG-003 (`agent_type` open set) | workflow-graph.md | T-SPEC-C1, T-IMPL-002 |
| WG-004 (non-agentic subtype open) | workflow-graph.md | T-SPEC-C1, T-IMPL-002 |
| WG-005 (gate attrs) | workflow-graph.md | T-SPEC-C1, T-IMPL-002, T-IMPL-010 |
| WG-006 (sub-workflow attrs incl. `sub_workflow_ref`) | workflow-graph.md | T-SPEC-C1, T-IMPL-002, T-IMPL-011 |
| WG-007 (legal outcome statuses per type) | workflow-graph.md | T-SPEC-C1, T-IMPL-008 |
| WG-008 (idempotency-class attribute) | workflow-graph.md | T-SPEC-C1, T-IMPL-002 |
| WG-009 (edge fields = EM-002 set) | workflow-graph.md | T-SPEC-C1, T-IMPL-001 |
| WG-010 (5-step cascade) | workflow-graph.md | T-SPEC-C1, T-IMPL-008 |
| WG-011 (unconditional-edge fallback) | workflow-graph.md | T-SPEC-C1, T-IMPL-008 |
| WG-012 (no-match-set fallback) | workflow-graph.md | T-SPEC-C1, T-IMPL-008 |
| WG-013 (dialect: `==` + `&&`) | workflow-graph.md | T-SPEC-C1, T-IMPL-007 |
| WG-014 (LHS whitelist) | workflow-graph.md | T-SPEC-C1, T-IMPL-007, T-IMPL-002 |
| WG-015 (RHS literal types) | workflow-graph.md | T-SPEC-C1, T-IMPL-007 |
| WG-016 (dialect distinct from guards) | workflow-graph.md | T-SPEC-C1, T-IMPL-007 |
| WG-017 (failure-class taxonomy) | workflow-graph.md | T-SPEC-C1, T-IMPL-006 |
| WG-018 (`failure_class` top-level on FAIL) | workflow-graph.md | T-SPEC-C1, T-IMPL-006 |
| WG-019 (`preferred_label` for verdict) | workflow-graph.md | T-SPEC-C1, T-IMPL-005 |
| WG-020 (no `retry_target` at v1) | workflow-graph.md | T-SPEC-C1, T-IMPL-002 |
| WG-021 (distinct terminal IDs) | workflow-graph.md | T-SPEC-C1, T-IMPL-002 |
| WG-022 (reserved terminal IDs) | workflow-graph.md | T-SPEC-C1, T-IMPL-002 |
| WG-023 (terminal-node detection) | workflow-graph.md | T-SPEC-C1, T-IMPL-008 |
| WG-024 (reserved-attribute strictness) | workflow-graph.md | T-SPEC-C1, T-IMPL-002 |
| WG-025 (edge-condition strictness) | workflow-graph.md | T-SPEC-C1, T-IMPL-002 |
| WG-026 (reference resolution) | workflow-graph.md | T-SPEC-C1, T-IMPL-003 |
| WG-027 (well-formedness incl. start_node) | workflow-graph.md | T-SPEC-C1, T-IMPL-002 |
| WG-028 (cycle bounding) | workflow-graph.md | T-SPEC-C1, T-IMPL-002 |
| WG-029 (sub-workflow acyclicity) | workflow-graph.md | T-SPEC-C1, T-IMPL-002, T-IMPL-011 |
| WG-030 (axis-tag consistency) | workflow-graph.md | T-SPEC-C1, T-IMPL-002 |
| WG-031 (mixed strict/permissive + `context_keys` reserved) | workflow-graph.md | T-SPEC-C1, T-IMPL-002 |
| WG-031a (`context_keys` declaration site — new at pass-6) | workflow-graph.md | T-SPEC-C1, T-IMPL-009 |
| WG-032 (AST retention of unknown permissives) | workflow-graph.md | T-SPEC-C1, T-IMPL-001 |
| WG-033 (schema_version graph-level) | workflow-graph.md | T-SPEC-C1, T-IMPL-001 |
| WG-034 (N-1 readability) | workflow-graph.md | T-SPEC-C1, T-IMPL-005 |
| WG-035 (`version` vs `schema_version`) | workflow-graph.md | T-SPEC-C1, T-IMPL-001 |
| WG-036 (canonical examples in `specs/examples/`) | workflow-graph.md | T-SPEC-C1, T-SPEC-C5, T-IMPL-015 |
| WG-037 (per-example doc sidecar) | workflow-graph.md | T-SPEC-C5 |
| WG-038 (project-local paths out-of-scope at v1) | workflow-graph.md | T-SPEC-C1, T-IMPL-003 |
| EM-005 v0.3.4 bump | execution-model.md | T-SPEC-C3, T-IMPL-005 |
| EM-005a enum extension (incl. `gate_decision`) | execution-model.md | T-SPEC-C3, T-IMPL-005, T-IMPL-010 |
| EM-005b | execution-model.md | T-SPEC-C3, T-IMPL-005 |
| EM-005c | execution-model.md | T-SPEC-C3, T-IMPL-006 |
| EM-007 amendment (`handler_ref` on gate) | execution-model.md | T-SPEC-C2, T-IMPL-004, T-IMPL-010 |
| EM-055 (workflow-graph loader integration) | execution-model.md | T-SPEC-C2, T-IMPL-003, T-IMPL-004 |
| EM-056 (workflow_mode toggle plumbing) | execution-model.md | T-SPEC-C2, T-IMPL-004 |
| EM-057 (dispatch decisions + events) | execution-model.md | T-SPEC-C2, T-IMPL-008 |
| EM-058 (CLI surface) | execution-model.md | T-SPEC-C2, T-IMPL-013 |
| EM-059 (cascade obligations) | execution-model.md | T-SPEC-C2, T-IMPL-008 |
| EM-060 (in-place EM-007 amendment bookkeeping) | execution-model.md | T-SPEC-C2 |
| EM-061 (in-place §10.1 lift bookkeeping) | execution-model.md | T-SPEC-C2 |
| HC-058 (Outcome surface table) | handler-contract.md | T-SPEC-C3, T-IMPL-005 |
| HC-059 (daemon back-fills `failure_class`) | handler-contract.md | T-SPEC-C3, T-IMPL-006 |
| HC-060 | handler-contract.md | T-SPEC-C3, T-IMPL-005 |
| HC-061 | handler-contract.md | T-SPEC-C3, T-IMPL-005 |
| HC-062 (`context_keys` declaration cite) | handler-contract.md | T-SPEC-C3, T-IMPL-009 |
| CP-053 (gate node = Gate-CP dispatch) | control-points.md | T-SPEC-C4, T-IMPL-010 |
| CP-054 (`gate_ref` + `handler_ref` on gate) | control-points.md | T-SPEC-C4, T-IMPL-010 |
| CP-055 (typed `*_ref` family) | control-points.md | T-SPEC-C4, T-IMPL-002, T-IMPL-012 |
| CP-056 (`policy_ref` deprecated + rejected) | control-points.md | T-SPEC-C4, T-IMPL-012 |
| CP-057 (`skills_ref` semantics) | control-points.md | T-SPEC-C4, T-IMPL-002 |
| CP-058 (`GateDecisionPayload`) | control-points.md | T-SPEC-C4, T-IMPL-010 |
| CP-038a (envelope-hash sub-amendment) | control-points.md | T-SPEC-C4 |

Coverage: 100% — every requirement-ID in the pass-5 changelog has at least one implementing task.

## 9. Pass-7 done-criteria check (jig)

- [x] Every spec draft has a transcription task with folded remediations.
- [x] Pass-6 BLOCKER is Wave 0, blocking C5 spec transcription.
- [x] All 6 SHOULD-FIX items (incl. CI-8 from pass-6 reviewer addendum) have explicit task entries.
- [x] All 6 NIT items have explicit task entries (folded into spec-transcription for cohesion).
- [x] All 10 pre-filed test beads listed as tasks with impl-task dependencies.
- [x] Every Go-level capability called out in the pass-7 brief has a T-IMPL-* task.
- [x] Dependency DAG laid out; no cycles.
- [x] Parallelization plan groups tasks into waves with cross-wave file-conflicts called out (EM file shared by T-SPEC-C2 and T-SPEC-C3 → serialized).
- [x] Granularity check: every task is half-day to ~2 days; T-IMPL-008 has a pre-identified split-point if it grows.
- [x] Coverage matrix demonstrates 100% requirement-ID coverage from `05-changelog.md`.

Pass-7 is ready to advance to pass-8 (Square) once these tasks are filed as beads (`br create` per T-* ID) and pinned to this kerf work via `kerf pin`.
