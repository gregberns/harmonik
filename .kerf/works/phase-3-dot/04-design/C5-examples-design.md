# C5 Design — `specs/examples/` + canonical `.dot` files (Pass-4, phase-3-dot)

> Pass-4 (change-design) for component **C5** of `phase-3-dot`. Scope: directory layout, README requirements, the canonical `.dot` file(s) to ship at v1, and the testing approach. **This is a design doc, not spec text.** The normative `.dot` file content lands in pass-5 (spec-draft); the illustrative sketch in §4.3 is descriptive only.

## 1. Current state

- `specs/examples/` does NOT exist in the harmonik repo.
- No canonical workflow graphs are published; the review-loop is hardcoded in daemon Go code (per EM-015d/EM-015e) with no DOT counterpart.
- Implementers and reviewers have no in-repo artifact to point at when discussing "what a harmonik workflow looks like."
- Gap **G4** ("no example `.dot` files in repo") is fully open; gap **G6** ("DOT schema versioning + repo convention") is partially open — convention will be validated by C5 placing the first example.

This means a Phase-3 spec landed without C5 is unanchored: C1's node-type catalog, C2's dispatch contract, and C3's Outcome surface have no worked example to demonstrate they compose. Pass-3 research (`03-research/examples/findings.md` §§6, 9) treats this as the load-bearing motivation for C5.

## 2. Target state

When C5 lands, the repo carries:

```
specs/examples/
├── README.md
└── review-loop.dot
```

That is the full v1 surface. Three positions explained in §3:

- **Flat directory.** No per-workflow subdir; `expected-trace.golden.jsonl` (if any) lives alongside, or under `internal/workflow/testdata/` per existing Go test-data convention.
- **One canonical example at v1: `review-loop.dot`.** `bead-process.dot` is deferred to a post-Phase-3 follow-up bead per the pass-3 research recommendation.
- **Mandatory `README.md`** mapping the `.dot` file to its spec anchors (which gap it absorbs; which C1/C2/C3 section normatively constrains each attribute and edge it uses).

Path convention (per D11): canonical examples live at `specs/examples/`. Project-local user workflows (`.harmonik/workflows/`) are explicitly out of scope at v1 — C1 §Schema Versioning will declare `specs/examples/*.dot` as the only canonical path harmonik ships.

### 2.1 Directory layout — committed shape

- **Path:** `specs/examples/` (new directory at repo root, under the existing `specs/` tree).
- **Files at v1:**
  - `specs/examples/README.md` — anchor doc.
  - `specs/examples/review-loop.dot` — canonical example.
- **Future-additions slot:** new examples land as siblings (`specs/examples/<name>.dot` + entry in `README.md`). The directory is flat until it exceeds five examples, at which point C5-followup decides whether to introduce subdirs. This threshold is not encoded normatively; it's a guideline.

### 2.2 README.md requirements

The README is normative for the C5 artifact — workflows that ship under `specs/examples/` MUST appear in it. Required sections:

1. **Purpose.** One paragraph: "Canonical workflow graphs that harmonik's spec uses as worked examples. Each `.dot` file in this directory MUST round-trip through C2's validator and MUST execute cleanly under the C2 dispatch driver against C3's Outcome contract."
2. **Per-example entry.** For each `.dot` file, a subsection containing:
   - **Filename and one-line purpose.**
   - **Schema version.** Must match the file's graph-level `schema_version` attribute (cross-checked at test time).
   - **Spec anchors.** Bulleted list mapping each attribute / node-type / edge-condition LHS the file uses to the C1/C2/C3 section that normatively defines it. Example bullet: "Uses `outcome.preferred_label` as an edge-condition LHS — per D4 row 3 / C1 §Edge Conditions."
   - **Gap coverage.** Which pass-1 G-gaps this example helps close (G4, G6 partial, etc.).
   - **Test surface.** Pointer to the static round-trip test and the scenario-harness test that exercise this file.
3. **Schema-version pinning.** A sentence declaring "All examples under this directory pin to `schema_version=1` at v1; mixed-version fixtures live under test-data directories, not here." Closes D10 by example.
4. **Authoring discipline.** Three rules: (a) every example must have inline DOT comments on each non-terminal node naming its role; (b) every example must have at least one scenario-harness test asserting the terminal-node path; (c) every example must appear in this README.

The README is auto-validated at test time only loosely (presence of file, presence of per-example subsection). Tight machine-readability isn't worth the complexity at v1.

### 2.3 The canonical example: `review-loop.dot`

Ships the EM-015d/EM-015e hardcoded review-loop expressed as a DOT graph. This is the single highest-value v1 example because:

- It is the highest-leverage **dogfood migration target.** The hardcoded Go path in `internal/daemon/workloop.go` becomes the post-Phase-3 reference migration; without `review-loop.dot` the migration has no target artifact.
- It is already **fully normatively pinned** by EM-015d/EM-015e — terminal-node set, routing inputs, reserved context keys, iteration cap. No invention required at C5; the example transcribes locked spec into DOT.
- It exercises **every load-bearing C1/C2/C3 surface**: cyclic edges (with bound), `outcome.preferred_label` routing (D-verdict-surfacing), context-key routing (`context.iteration_count`, `context.last_verdict` per D4 row 5), the unconditional-edge fallback invariant (D-edge-cascade-invariant), the `agentic` node-type with session-resume semantics.
- It is the **smallest sufficient example** that demonstrates the cascade's non-trivial steps (b)+(a) together. A simpler "two-node linear" example would not exercise the cascade meaningfully.

Concretely, `review-loop.dot` will contain (pass-5 owns final prose; this is the design shape):

- **Graph-level attributes:** `schema_version="1"`, `workflow_id="review-loop-example"`, plus a `terminal_node_ids` declaration listing the two terminals.
- **Nodes (5 total):** `start` (entry), `implementer` (`agentic`, with `handler_ref="claude-implementer"`, session-resume semantics inherited from C3), `reviewer` (`agentic`, with `handler_ref="claude-reviewer"`, fresh-session per iteration), `close` (terminal, APPROVE path), `close-needs-attention` (terminal, BLOCK / cap-hit / no-progress path).
- **Edges (6 total):**
  - `start -> implementer` (unconditional).
  - `implementer -> reviewer` (unconditional).
  - `reviewer -> close` with `condition="outcome.preferred_label == 'approved'"`.
  - `reviewer -> implementer` with `condition="outcome.preferred_label == 'changes_requested'"` (the retry-cycle edge). NOTE: no inline `iteration_count` bound — the D5 v1 dialect is equality + `&&` only (no `<`/`>`); cap-hit is handled by the unconditional fallback edge below + daemon-enforced EM-015e iteration-cap.
  - `reviewer -> close-needs-attention` with `condition="outcome.preferred_label == 'blocked'"`.
  - `reviewer -> close-needs-attention` (UNCONDITIONAL — the fallback edge that catches cap-hit and no-progress paths via the D-edge-cascade-invariant fallback rule).

The unconditional final edge is the example's **fallback-scenario hook** per D-edge-cascade-invariant — when the daemon's EM-015e iteration-cap fires (iteration_count == 3 with verdict `changes_requested`), the retry conditional no longer routes (per daemon enforcement), no other conditional matches, and the cascade falls back to the unconditional `close-needs-attention` edge. This satisfies D-edge-cascade-invariant's requirement that C5 ship a fallback-scenario example. The cap-hit is enforced daemon-side (EM-015e), NOT by an inline `<` condition (out of D5 v1 dialect).

### 2.4 The deferred example: `bead-process.dot`

**Not shipped at v1.** Reasons (research §§3, 8, 9):

- Depends on a **tool-node handler contract** (D17) that is not yet specced — the `br claim` / `br close` / `git worktree` / `git merge` steps would be `tool`-type nodes whose Outcome→status mapping is unwritten.
- Depends on a **merge-node primitive** with ff-only enforcement — not yet specced as a node type.
- Depends on a **reconciliation-node** binding (reconciliation-as-workflow) that lives in a separate subsystem.
- The most natural shape composes review-loop as a sub-workflow, which would require either (a) duplicating review-loop structure inline (b ugly), (b) referencing review-loop.dot as a sub-workflow (blocked: EM-034 explicitly excludes review-loop from sub-workflow expansion at MVH), or (c) inlining a single-shot review node (degrades the example to dishonesty about what the orchestrator actually does).

Filed as a candidate follow-up bead `phase3-bead-process-example`, gated on (D17 land) AND (merge-node spec land) AND (sub-workflow review-loop interplay decided). Pass-5 spec-draft does NOT include `bead-process.dot`. The C5 README explicitly notes "future examples include `bead-process.dot` once the prerequisites land."

## 3. Rationale (the position-taking)

### 3.1 D11 — In-repo path: `specs/examples/` for canonical; `.harmonik/workflows/` out of scope at v1

**Position: adopt the research lean.** Canonical examples live at `specs/examples/`. Project-local user workflows under `.harmonik/workflows/` are deferred to a later spec amendment.

Rationale: harmonik does not yet have a "user authors workflows" surface. The dispatch driver (C2) is the only consumer of `.dot` files at v1, and the only `.dot` files harmonik ships are the canonical examples. Specifying a project-local search path before any user can author a workflow is premature; it forces C1 to define discovery semantics for files that don't exist. When user-authored workflows become a real capability, a follow-up amendment to C1 §Schema Versioning + Repo Convention adds `.harmonik/workflows/`. At v1, one path, one purpose.

### 3.2 Ship `review-loop.dot` alone; defer `bead-process.dot`

**Position: adopt the research recommendation.** Research §9 is unambiguous: bead-process.dot depends on at least three primitives that aren't specced (tool-node handler contract, merge-node, sub-workflow composition for review-loop). Authoring it now pins shapes before their contracts exist — exactly the failure mode the kerf process is designed to prevent.

Counter-position considered: ship a degraded `bead-process.dot` (single-shot review, no tool-node, no merge primitive) so the directory has two examples. Rejected — a degraded example is worse than no example. It would teach implementers the wrong shape and would have to be replaced wholesale once the prerequisites land, breaking the "examples are normative" discipline declared in §2.2.

Counter-position considered: defer C5 entirely until bead-process.dot is shippable. Rejected — review-loop.dot is shippable now and closes G4 in full for the highest-leverage migration target (the EM-015d hardcoded Go path). Holding C5 to wait for the second example wastes the closeable gap.

### 3.3 Directory shape: flat for v1

**Position: adopt the research lean.** Flat layout — `specs/examples/<name>.dot` + `README.md`. No per-workflow subdirectories.

Rationale: with one example at v1 (and at most one or two more in the immediately foreseeable future), per-workflow subdirs add noise without value. The Go convention for test data (`internal/.../testdata/`) handles golden-trace artifacts elsewhere, so the sibling-trace-file argument for subdirs is weak. If the directory grows past five examples or examples start carrying substantial sibling artifacts, a follow-up bead can introduce subdirs additively.

### 3.4 README.md: mandatory, with required structure

**Position: README is required and structured.** Per §2.2 above.

Rationale: examples without a README are easy to misread. A workflow-graph reader who doesn't know that `outcome.preferred_label` is whitelisted-by-D4-row-3 will guess at the language by reading the file — and may guess wrong. The README's per-example spec-anchor list pre-empts that. It also enforces a discipline: every attribute the example uses must trace to a normative spec section. If pass-5 tries to author an example that uses an unspecified attribute, the README requirement makes the omission visible during review.

The README is mandatory, but its enforcement is loose (presence-of-section, not machine-validated content). Tight enforcement (parse the README, cross-check attribute names) is post-v1 polish.

### 3.5 Testing approach: two-layer

**Position: adopt the research lean.** Two test layers, both required for every example shipped.

- **Layer 1 — Static round-trip.** A Go unit test (likely under `internal/workflow/` once that package exists per C2) parses every `.dot` in `specs/examples/` and runs it through the C2 validator. CI fails on any parse or validation error. This catches schema drift between spec and example.
- **Layer 2 — Scenario harness.** A scenario-harness test (per `specs/scenario-harness.md` — already established as the integration-test surface) loads the example, drives mock handler responses, and asserts the terminal node reached plus the emitted event sequence against a golden trace. This catches dispatcher bugs, cascade ordering bugs, and terminal-state-classifier bugs.

The static layer is a precondition for the scenario layer (a file that doesn't parse can't be dispatched). Both are required because they catch disjoint defect classes: validator catches structural errors; harness catches runtime-routing errors.

Golden traces (`expected-trace.golden.jsonl`) live with the scenario-harness test, not in `specs/examples/`. The `specs/examples/` directory contains only specification artifacts (the `.dot` file + the README); test fixtures are a runtime concern that belong adjacent to the test code. This separation is a small but real win for readability — implementers reading `specs/examples/` see specs, not test infrastructure.

For `review-loop.dot` specifically, the scenario tests must cover:

1. APPROVE-on-iteration-1 → `close`. (Conditional edge match, step (a).)
2. REQUEST_CHANGES → REQUEST_CHANGES → APPROVE-on-iteration-3 → `close`. (Loop edge; iteration cap enforced by daemon EM-015e, not by inline edge condition.)
3. BLOCK-on-iteration-1 → `close-needs-attention`. (Conditional edge match for blocked verdict.)
4. **Cap-hit fallback:** REQUEST_CHANGES three times → no conditional matches at iteration 3 → fallback to unconditional edge → `close-needs-attention`. (This is the D-edge-cascade-invariant fallback test.)
5. No-progress: REQUEST_CHANGES with bit-identical diff hash → context-driven early exit → `close-needs-attention`. (Exercises the EM-015e no-progress detector composing with the cascade.)

Scenario (4) is the load-bearing fallback test required by D-edge-cascade-invariant. Without it, the example does not demonstrate the invariant.

## 4. Requirements traceability

Mapping back to `02-components.md §C5` requirements and pass-1 success criteria:

| 02-components.md §C5 requirement | Where addressed |
|---|---|
| Path: `specs/examples/review-loop.dot`, `specs/examples/bead-process.dot` (minimum) | §2.1 — adjusted to **review-loop.dot alone**; bead-process.dot deferred per research recommendation. Documented in §3.2. |
| Kind: new artifacts, normative as "round-trips through validator" | §3.5 — two-layer testing makes this concrete. |
| Purpose: anchor the spec with concrete artifacts; double as worked test cases for C2 | §2.3 — review-loop.dot exercises C1+C2+C3 surfaces; §3.5 layer-2 makes the worked-test-case explicit. |
| Source gaps absorbed: G4 (no examples) full | §2.3 — review-loop.dot is the canonical example. |
| Source gaps absorbed: G6 (repo convention) partial | §3.1 — D11 position commits the canonical path. |
| Depends on: C1 (schema), C2 (dispatch), C3 (handlers) | §5 below — explicit cross-references. |
| Open Q: third example for failure-class routing | §2.3 + §3.5 — the cap-hit scenario in review-loop.dot **is** the failure-class routing exercise (cap-hit → fallback edge → close-needs-attention); a separate third example is not needed at v1. |
| Open Q: inline comments vs. sibling README | §2.2 + §3.4 — **both**: inline one-line comments per node (research lean) AND mandatory README. |

| Pass-1 success criterion (§6) | C5 coverage |
|---|---|
| #3 `specs/examples/review-loop.dot` + `bead-process.dot` exist (G4) | review-loop.dot only; bead-process.dot deferred with rationale (§3.2). G4 is closed-enough for Phase-3 by the single high-leverage example. |
| #6 No locked decision reopened | Confirmed — every position in §3 either adopts a landed D-decision or extends one additively. |

## 5. Cross-references to other component designs

- **C1 design (in progress, `C1-workflow-graph-design.md`).** C5 consumes C1's node-type catalog (`agentic`, terminal-node mechanism), the edge-condition syntax (D4 LHS whitelist + D5 dialect), and the schema-version attribute. review-loop.dot is the worked-example proof that C1's surface is sufficient. If C1 lands a node-type rename or edge-condition grammar change, review-loop.dot must update — the static round-trip test makes the dependency mechanical.
- **C2 design (in progress, `C2-execution-model-dot-design.md`).** C5's scenario-harness tests are the load-bearing exercises for C2's dispatch driver. The cap-hit fallback scenario (§3.5 scenario 4) is the test that proves D-edge-cascade-invariant is wired correctly. If C2's driver doesn't fall back to unconditional edges, this scenario fails — making C5 the early-warning surface for that defect class.
- **C3 design (in progress, `C3-handler-contract-outcome-design.md`).** review-loop.dot's reviewer node depends on C3 specifying that handlers may set `outcome.preferred_label` to verdict strings. Per D-verdict-surfacing, this is `"approved" / "changes_requested" / "blocked"` (vocabulary deferred to pass-5). C5 does not invent verdict vocabulary; it consumes whatever C3 + D-verdict-surfacing decide.
- **C4 design (`control-point-node-type-design.md`, landed).** No direct C5 dependency — review-loop.dot does not use any ControlPoint binding (no `gate_ref`, no `freedom_profile_ref`, no `budget_ref`). The reviewer node is a plain `agentic` node, NOT a `gate` node, per D3 implications §"Implications for C5".

## 6. Open questions surfaced (not closed by C5)

These are surfaced for pass-5 or for follow-up beads, not pre-conditions for C5 to land:

1. **Canonical verdict-string vocabulary.** D-verdict-surfacing leans `{"approved", "changes_requested", "blocked"}` but defers final choice to pass-5. C5's review-loop.dot uses these strings illustratively; pass-5 spec-draft must align the example with the final pass-5 choice.
2. **Iteration-cap encoding.** EM-015e hardcodes cap=3 daemon-side. review-loop.dot does NOT encode the cap as an inline edge condition — the D5 v1 dialect supports only equality + `&&` (no `<`/`>`). The cap is enforced exclusively by (a) EM-015e on the daemon side and (b) the D-edge-cascade-invariant unconditional fallback edge that catches the cap-hit path. Whether C1 introduces a graph-level retry-cap attribute later (so the cap can be visible in the graph rather than implicit) is a pass-5+ question; review-loop.dot v1 relies on daemon enforcement alone.
3. **`expected-trace.golden.jsonl` file format.** The scenario harness's golden-trace format is owned by `specs/scenario-harness.md`, not by C5. C5 only declares that goldens exist per example; the format is upstream.
4. **`bead-process.dot` follow-up bead naming and ID.** Filed as candidate `phase3-bead-process-example`; the bead itself lands in pass-6 task decomposition along with the other Phase-3 epic beads.
5. **README machine-validation.** Is presence-of-section sufficient, or should CI parse the README and cross-check attribute lists against the `.dot` files? Lean (§3.4): presence-only at v1; tight validation is post-v1 polish.

## 7. Implications for pass-5 (spec-draft)

Pass-5 owns:

- Creating `specs/examples/README.md` with the structure specified in §2.2 (including the `review-loop.dot` per-example subsection).
- Authoring `specs/examples/review-loop.dot` per the shape in §2.3 (with inline node-role comments and the graph-level + node-level + edge-level attributes specified there). Final verdict-string vocabulary aligned with the pass-5 choice for D-verdict-surfacing.
- Writing the static round-trip Go test (likely `internal/workflow/examples_test.go` or equivalent) that loads every `.dot` in `specs/examples/` and runs it through the C2 validator.
- Writing the five scenario-harness tests enumerated in §3.5 with golden traces.
- Updating the C1 §Schema Versioning + Repo Convention prose to declare `specs/examples/` as the canonical path, with `.harmonik/workflows/` deferred.

Pass-5 does NOT create `bead-process.dot` or any associated test scaffolding. Filing the follow-up bead is a pass-6 task.

---

**Sources:** `01-problem-space.md` §3 (G4, G6), `02-components.md` §C5, `03-research/examples/findings.md`, `03-research/SUMMARY.md` (rows D11, D12, D13), `04-design/D-attractor-adoption.md`, `04-design/D-edge-cascade-invariant.md`, `04-design/D-verdict-surfacing.md`, `04-design/D4-edge-condition-lhs-whitelist.md`, `04-design/control-point-node-type-design.md` (D3), `specs/execution-model.md` §§EM-015d, EM-015e (EM-015d-RFD, EM-015d-RIA sub-clauses), §6.5 (review-loop events).
