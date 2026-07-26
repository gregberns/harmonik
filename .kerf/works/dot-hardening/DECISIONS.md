# Design decisions ledger — resolves the 25 research open questions

> Input to the Change-Design pass. Each open question from `03-research/*/findings.md` is resolved
> here (a decision) or marked **[OPERATOR]** and carried to `ISSUES.md` with a recommendation.
> Grounded in `MODEL.md` + the operator's locked decisions (`01-problem-space.md`). Decisions are
> mine to make within the decided design; genuinely product-level choices are deferred, not guessed.

## Cross-cutting vocabulary

- **Declared inputs/outputs are a DISTINCT channel from template params** (research hazard i:
  template params reject newlines + cap at 8 KB). Declared inputs name **run variables** that carry
  arbitrary multi-line text; they are NOT `__TOKEN__` splices. Template params (WG-045) are unchanged
  and keep their launch-time role for short trusted values.
- **Declared node I/O is a separate namespace from `context_keys`** (OQ-HC-8). `context_keys` stays
  the edge-condition-LHS registry (HC-062); node inputs/outputs are the brief-assembly + producer/
  consumer surface. They may share the underlying run-variable store but are declared separately;
  do not overload `context_keys`.

## Workflow-graph (Component A)

- **D-A1 syntax (OQ-WG-1):** new optional agentic-node attributes `inputs="a, b, ..."` and
  `outputs="name:type, ..."`, one readable DOT line, comma-separated variable names; `type` optional
  and only `enum(...)` is meaningful at v1. Absent `inputs` → role-default set (strict-additive
  back-compat; every existing `.dot` still loads).
- **D-A2 safe default input set (OQ-WG-2):** role-derived. Every node sees `bead.id` + `bead.title`
  (traceability, already in the artifact header) + `goal` (default-visible, see D-A3). Implementer
  default inputs = `{task}`; on resume also `{feedback}` when bound. Reviewer default inputs =
  `{task_context (bead id/title/body + diff base/head SHAs), rubric}`. These defaults reproduce
  today's behavior for un-migrated graphs.
- **D-A3 goal reconciliation (OQ-WG-3, hazard ii):** `goal` becomes a **default-visible declared
  input**, subject to the visibility rule (a node MAY exclude it). WG-044's "unconditional broadcast
  into every agentic brief" is amended: `goal` is a default-visible input, not an ambient thread that
  bypasses visibility. This closes the re-leak vector structurally (a `goal` embedding the rubric can
  no longer reach a node that does not declare/inherit it).
- **D-A4 verdict enum + WG-019 (OQ-WG-4, hazard iv):** the reviewer node declares
  `outputs="verdict:enum(APPROVE,REQUEST_CHANGES,BLOCK), notes:string"`. `verdict` is the **typed
  name for the value that still surfaces as `outcome.preferred_label`** — no separate routing field;
  WG-019 stands. WG-014's "arbitrary string" is refined: when the producing node declares a verdict
  enum, that value is enum-constrained. `notes` is the feedback-content output.
- **D-A5 producer back-pointer (OQ-WG-5):** the loader resolves the producer of a routed value as the
  **edge's from-node**. Check: for an edge from node N whose condition branches on
  `outcome.preferred_label == 'X'`, if N declares a verdict enum, `X` MUST be a member — else load
  error. Needs no new edge field. Untyped `preferred_label` edges stay permissive (back-compat).
- **D-A6 override serialization — [OPERATOR] → ISSUES #1.** Recommend a **fenced structured block in
  the bead body** (`harmonik` YAML block) carrying per-node `{tool, model, effort, locked}`, because
  the operator rejected the ad-hoc `implement@model=…` string and asked for "a proper serialization."
  A flat `model:<alias>` label stays the **run-wide** simple form. WG owns only the node-id addressing
  vocabulary + the "override names a real node" load check; the block's parse home is EM/bead-integration.
- **D-A7 locked marker (OQ-WG-7):** a node is marked keep-cheap via `model_locked` (in the same
  structured config as D-A6, or a node attribute — see ISSUES #1). A locked node ignores escalation
  from the override band.
- **D-A8 back-edge cap (OQ-WG-8):** no new clause — A6's "back-edge carries a traversal cap" is a
  reference to the existing WG-028 (every cyclic edge has an effective cap), not a duplicate.
- **D-A9 exemplar (OQ-WG-9):** yes — update `specs/examples/standard-bead.dot` + sidecar to carry
  declared I/O so the default graph is the leak-proof path. Scoped as an exemplar update in Spec-Draft,
  not a normative clause.

## Execution-model (Component B)

- **D-B1 role field + renderer (OQ-EM-1):** promote `role` to a typed §6.1 Node record field (today
  only threaded via ExtraContext). The single-renderer clause **generalizes/replaces** the B↔E
  brief-composition contract (EM 1584–1590) and the `buildAgentTaskContent`-naming sentence (1590):
  brief = {declared inputs} + {prompt} + {role} + {goal}, framing selected by role.
- **D-B2 reviewer body verbatim (OQ-EM-2, hazard H4):** the reviewer's default inputs include the
  bead body **verbatim** (as today, `agenttask_chb028.go:610–613`) + diff base/head SHAs + rubric.
  Do NOT reduce the reviewer to rubric-only.
- **D-B3 feedback = in-record value; file is transport (OQ-EM-3, hazard H3):** the canonical feedback
  is the reviewer's produced `verdict`+`notes` values, which the single renderer reads and renders
  into the implementer's resume brief. **EM-056 clause 4 (line 1610) is REPLACED** (not augmented) by
  this value-on-back-edge channel — resolving the code/spec contradiction (dot_cascade.go already
  writes the file, hk-wixms). The on-disk `reviewer-feedback.iter-N.md` file is **retained only as
  claude's transport detail** (claude delivers via paste-inject referencing a file, not argv).
- **D-B4 iteration counter (OQ-EM-4):** "iteration" = the review-loop `iteration_count` (§6.1 context,
  1225) — the same counter in the HC-004 launch idempotency tuple `(run_id,node_id,phase,
  iteration_count)`. Feedback input unbound at iteration 1 (section omitted), bound on bounce-back.
- **D-B5 ladder flip (OQ-EM-5a):** rewrite EM-012b-NODE (295) + the precedence block (297–306): node
  `model=`/`effort=` becomes a **soft default BELOW** the per-bead escalation / per-run force, with the
  `model_locked` opt-out. State the new order unambiguously (highest→lowest): per-run force →
  per-bead escalation → node attr (unless locked) → project config → env → compiled default.
- **D-B6 escalation scope (OQ-EM-5b):** **resolved this session** — per-node addressing by node id is
  primary; a bare/flat form = run-wide. (Was MODEL.md's one open decision; operator answered it.)
- **D-B7 force-vs-lock — [OPERATOR] → ISSUES #2.** Recommend: a per-run `--force-model` (operator,
  one-shot, top tier) **overrides** a `model_locked` node (explicit operator override); a per-bead
  escalation **label respects** the lock (the lock is the author saying "cheap even for hard beads").
- **D-B8 seal (OQ-EM-6, hazards H1/H2):** new sealed §6.1 Run-record field
  `node_model_seal: Map<(node_id, iteration_count), {tool, model, effort}>`. Written at the FIRST
  dispatch of each `(node_id, iteration_count)` after alias→concrete resolution for that node's
  **effective tool** (a dispatch-time fact). Keyed by the **composite** `(node_id, iteration_count)` —
  the back-edge re-dispatches the same node, so an iteration-blind key would overwrite iter-1 (H1).
- **D-B9 replay reads the seal (hazard H2):** amend EM-055 resume semantics (1592): on replay/resume
  the graph reparses as today, BUT dispatch MUST read the sealed concrete for `(node_id,
  iteration_count)` and MUST NOT recompute from the live catalog/labels/config. The catalog is an
  input to first resolution only; after that the run is frozen. This is the operator's replay/rerun
  confirmation ("did it use what I configured?").

## Handler-contract (Component C)

- **D-C1 transport-only (OQ-HC-1, hazard HC-013):** one new seam clause **HC-072** ("adapter receives
  ONE assembled brief, chooses only the delivery envelope") + amend HC-006a §249 to point at the EM
  single-renderer contract. Expressible within the fixed 4 callbacks — **no new adapter method** (D-C7).
  The `reviewer-feedback.iter-N.md` file survives as claude's transport detail (per D-B3).
- **D-C2 model-resolution split (OQ-HC-2):** **EM owns** the resolution ladder + seal (D-B5/B8/B9);
  **HC owns** "the resolved per-tool concrete reaches `rc.model` for EVERY tool" (delete the claude-only
  guard, `nodeModelForHarness` `dot_cascade.go:1182`) + the alias-catalog location
  (`.harmonik/config.yaml models.aliases`, hot-reloadable, keep-last-good on parse failure) + the
  fail-loud-on-resolution rule. Amend HC-055a + new HC-073.
- **D-C3 fail-loud scope (OQ-HC-3):** fail-loud is scoped to **resolution** — a catalog miss on an
  **explicit escalation** (force / bead label), or a tool-namespaced concrete on the wrong tool
  (author error). The concrete's **validity** stays handler-side (HC-055a value-opacity invariant
  preserved — harmonik still never checks a model string names a real model).
- **D-C4 degrade target + pi (OQ-HC-4):** a **default-band** alias that misses the catalog degrades to
  the run-level resolved concrete (+ warn). If that concrete is empty for a tool that requires it (pi
  hard-fails on empty), that is itself a fail-loud — never dispatch an empty-model pi run.
- **D-C5 coverage-contract ownership (OQ-HC-5):** HC owns the coverage **contract** (what must be
  assertable: forceable round-trip; per-handler argv/binary matches declared; leak oracle). The
  harness **mechanics** live in the scenario-harness spec (S07, per HC-038). The **typed input
  manifest is DAEMON-EMITTED** (a production byproduct of the real routing path) — its emitter is
  specified in EM (the renderer emits `role→source-keys`), so it is not test-only and cannot rot silently.
- **D-C6 leak-oracle sequencing (OQ-HC-6):** the leak-oracle clause lands now as **"known-RED until
  the renderer lands, GREEN after."** Tasks sequence it after the renderer + manifest.
- **D-C7 adapter-surface budget (OQ-HC-7):** confirmed — C1 + C3 are constraints on the existing
  `Launch`/callbacks; no new adapter method, no HC-013 foundation amendment.
- **D-C8 honesty limits (stated plainly in the spec, not decisions):** (1) claude resume feedback is
  tmux paste-inject, NOT argv — the recorder cannot capture it; the claude row proves "daemon
  assembled/wrote the brief" (its delivery is proven by existing unit tests + live), pi/codex rows are
  argv-faithful. (2) The leak oracle asserts on the daemon-emitted manifest, and only becomes a real
  oracle after per-role source keys exist (post-renderer). (3) Degradation is a three-way split
  (D-C3/C4), not "graceful degrade everywhere."

## Deferred to ISSUES.md (operator decisions / significant issues)

1. **Override serialization** (D-A6/A7): fenced structured `harmonik` block in the bead vs
   node-addressed labels. Recommend the block. Needs a shape confirmation.
2. **Force-vs-lock** (D-B7): does a per-run force override a `model_locked` node? Recommend yes;
   a per-bead label respects the lock.
3. **Ladder-flip migration** (hazard H5 / ADV-B S2): flipping silently changes any deployed bead
   carrying a bare `model:opus` label (was run-wide; under D-B5/B6 the bare form STAYS run-wide, so
   this is mostly mitigated — but confirm no deployed bead relies on a node `model=` winning over a
   label). Beads are OFF this phase, so blast radius is likely nil; flag for confirmation.
4. **Accepted limitation** (D-C8): claude's resume delivery is not argv-assertable in the harness —
   proven by unit tests + live, not by the round-trip recorder. Accept or invest in a paste-inject
   capture seam.

---

## Change-design review fixes (applied before Spec-Draft)

Review verdict: APPROVE-WITH-FIXES (2 CONFIRMED, 4 tightenings). `change-design-review.md`. Resolutions:

- **D-FIX-1 (D1, load-bearing) — task/rubric value-source; EM owns it.** The "leak is unexpressible"
  guarantee requires `task` and `rubric` to come from DISTINCT sources, not two renders of one bead body.
  Resolution:
  - **Reviewer `rubric` input source** = (1) the **reviewer node's `prompt`** — this is where today's
    generic reviewer rubric (the hardcoded coverage-check / spec-field-check strings in
    `buildReviewTargetContent` `agenttask_chb028.go:580–608`) MOVES: it becomes authored graph text the
    single renderer emits on the reviewer path (so it is NOT orphaned when the builder is deleted); PLUS
    (2) an OPTIONAL per-bead `rubric` field carried in the structured bead config (Issue #1's block). The
    rubric is NOT sourced from the bead task/body.
  - **Implementer `task` input source** = the bead's task text (the bead body, or a structured `task:`
    field when the bead uses the block). This source excludes the reviewer node prompt AND the bead rubric
    field.
  - **Invariant (state in EM + WG-057):** the implementer declares `task` (+`feedback` on resume) and does
    NOT declare `rubric`; the rubric sources bind ONLY to the reviewer. The cross-role leak (reviewer's
    rubric → implementer) is therefore structurally unexpressible.
  - **Boundary (state honestly):** the system does NOT prevent an author from embedding review criteria in
    the task prose itself — that is telling the implementer directly, not a cross-role leak, and is out of
    scope (putting the answer in the question). The guarantee is precisely: one role's separately-sourced
    instructions cannot reach another role.
  - **Back-compat (fixes the false reviewer-regression claim):** today's hardcoded reviewer rubric strings
    become the standard-bead reviewer node's `prompt`/role framing → the reviewer brief is preserved, not
    lost. Reviewer default input set = `{task_context (bead id/title/body verbatim + diff base/head SHAs),
    rubric (= reviewer node prompt + optional bead rubric field)}`.
  - **Owner:** EM (it deletes the builder, so it specifies the rubric origin + the task-is-rubric-free
    invariant). WG names the inputs; the structured bead `rubric`/`task` fields tie to Issue #1's block.

- **D-FIX-2 (D2) — manifest emitter gets its own EM clause.** New **EM-069-MAN**: the daemon MUST emit,
  as a production byproduct of the renderer, a typed input manifest per node dispatch —
  `{node_id, role, iteration_count, source_keys[]}` — where `source_keys` names each declared input the
  brief was assembled from. The C3 leak oracle asserts `rubric` ∉ the implementer's `source_keys`. Not a
  cross-file aside — a normative MUST with the shape.

- **P1 (terminology) — pin two words across the EM/HC seam:** EM says **"precedence order"** (tier/ladder +
  seal); HC says **"alias-catalog lookup"** (catalog resolve + fail-loud + endpoint). Never the bare word
  "resolution" for both.
- **P2 (manifest granularity):** `source_keys` are the top-level declared-input names (`task`, `feedback`,
  `rubric`, `task_context`, `goal`); `task_context` is ONE key (not four sub-keys). Pin this where EM-069-MAN
  is drafted.
- **P3 (effective-tool owner):** the node's effective tool is resolved by the existing handler-selection
  path (HC-003) at dispatch, BEFORE the seal (EM-070) is written. State the ordering so the seal-write
  timing is unambiguous.
- **P4 (verdict↔edge check scope):** WG-060 check 2 fires ONLY on edges whose from-node is the verdict
  producer (the cascade evaluates an edge condition against its from-node's output). A verdict routed
  through an intermediate gate is correctly NOT checked. One sentence in the clause so a drafter does not
  "strengthen" it into an unrunnable global check.
