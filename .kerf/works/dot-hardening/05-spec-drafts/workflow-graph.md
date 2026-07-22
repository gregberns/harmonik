# 05 — Spec draft: `specs/workflow-graph.md` (WG)

> SPEC-DRAFT pass. This is the ACTUAL NORMATIVE TEXT that becomes part of `specs/workflow-graph.md` at
> `kerf finalize`. Each clause below is written to be pasted verbatim into the target spec at the insertion
> point named in its heading. Voice, clause-numbering, MUST/SHOULD/MAY register, cross-ref form, and the
> trailing `Tags:` line match the existing spec. New clauses continue at **WG-055** (current high-water mark
> WG-054, §4 L205). Grounded in `DECISIONS.md` (D-A1..D-A9, D-FIX-1/D-FIX-2, P1–P4) and
> `04-design/workflow-graph-design.md`.
>
> **New clauses:** WG-055, WG-056, WG-057, WG-058, WG-059, WG-060 (6).
> **Amended clauses:** WG-040, WG-044, WG-014, WG-019, WG-015, WG-025, WG-002, WG-031, §16.1 vocab-diff,
> changelog (10 sites).

---

## 1. New clauses (insert into §4 and §9)

### WG-055 — Declared node inputs (insert in §4, after WG-042 / before WG-043, ~L230)

> Tags: mechanism, normative

### WG-055 — Declared `inputs` attribute on `agentic` nodes

An `agentic` node MAY carry an optional `inputs` attribute: one readable DOT line whose value is a
comma-separated list of **run-variable names** the node's brief MAY read (e.g. `inputs="task, feedback"`).
Each name is an identifier matching `^[a-z][a-z0-9_]*$`, naming a **run variable** — a value bound at
launch or **produced by a node** (§4 WG-056). The `inputs` list is the node's brief-assembly channel: the
single renderer ([execution-model.md §7.5] renderer contract) assembles the node's task brief from exactly
the values named here (plus the role-default set below), and from nothing else (the visibility invariant of
§4 WG-057).

- **Distinct from template params (§4 WG-045).** A declared input carries arbitrary multi-line text (a task
  body, a review rubric, reviewer notes); it is NOT a `__TOKEN__` splice. WG-045 params reject newline /
  control characters and cap at 8192 bytes (a shell-injection defense), so they cannot carry a multi-line
  rubric. `inputs` is its own vocabulary and does NOT route text through the WG-045 template-param surface.
- **Distinct namespace from `context_keys` (§10 WG-031a).** `context_keys` is the edge-condition-LHS
  registry (the `context.<key>` whitelist of §6 WG-014); `inputs` is the brief-assembly channel. The two
  MAY share the underlying run-variable store but are declared separately; a loader MUST NOT overload one
  for the other.
- **Value source is owned by [execution-model.md §7.5].** WG names the inputs; the origin of each named
  value — which text a `task` or a `rubric` input resolves to — is normative in the execution model
  (the single renderer). In particular, `task` and `rubric` are DISTINCT declared inputs sourced from
  DISTINCT origins (the implementer's `task` from the bead task text; the reviewer's `rubric` from the
  reviewer node's `prompt` plus an optional per-bead rubric field — see §4 WG-040 and [execution-model.md
  §7.5]). The implementer node does NOT declare `rubric`; the cross-role leak (a reviewer's rubric reaching
  an implementer) is therefore structurally unexpressible at the graph layer (§4 WG-057).

**Role-default input sets (absent `inputs` → back-compat, strictly additive).** When a node carries no
`inputs` attribute, the loader binds the role-default set for the node's resolved class, reproducing today's
brief so that every existing `.dot` loads and dispatches unchanged:

- **implementer-class:** `{task}`. On a resume iteration where a `feedback` variable is bound (a bounce-back
  from a reviewer, per [execution-model.md §4.3 EM-056]), the set additionally includes `{feedback}`.
  `feedback` is unbound at iteration 1 and its brief section is omitted (§4 WG-060 check 1; the iteration
  binding is [execution-model.md §7.5]).
- **reviewer-class:** `{task_context, rubric}`, where `task_context` = the bead id, title, and body
  **verbatim** plus the diff base/head SHAs (the reviewer body stays verbatim, per [execution-model.md
  §7.5]), and `rubric` = the reviewer node's `prompt` plus any optional per-bead rubric field. `task_context`
  is ONE declared input (not four sub-keys).
- **every node additionally sees:** the bead ID and bead `Title` (traceability — already retained in the
  per-dispatch artifact header per §4 WG-040), and `goal` (a default-visible declared input per §4 WG-044 /
  §4 WG-057; a node MAY exclude it).

`inputs` is valid ONLY on `agentic` nodes. An `inputs` attribute on a `non-agentic`, `gate`, or
`sub-workflow` node, on an edge, or at the graph level is a reserved-attribute-out-of-position strict error
per §10 WG-031.

Tags: mechanism, normative

---

### WG-056 — Declared `outputs` attribute on `agentic` nodes (insert in §4, paired with WG-055)

An `agentic` node MAY carry an optional `outputs` attribute: `outputs="name:type, ..."`, a comma-separated
list of `name:type` pairs on one readable DOT line, where each `name` matches `^[a-z][a-z0-9_]*$` and `type`
is optional (§4 WG-058; only `enum(...)` carries load-time weight at v1). An output names a value the node
**produces as it runs**; a downstream node consumes it by declaring the same name in its `inputs` (§4
WG-055) — the producer→consumer pair rides an edge between the two nodes.

The **reviewer node** declares `outputs="verdict:enum(APPROVE,REQUEST_CHANGES,BLOCK), notes:string"`:
`verdict` is the reviewer's routing verdict (the typed name for the value that still surfaces as
`outcome.preferred_label`, §4 WG-058 / §7 WG-019), and `notes` is the reviewer's feedback content consumed
as the implementer's `feedback` input on a REQUEST_CHANGES bounce-back ([execution-model.md §4.3 EM-056]).

Bulk work product (the diff, generated files) is NOT a declared output: it flows through the shared git
worktree, not a run variable. `outputs` carries only small instruction/control text (a verdict, feedback
notes). A declared output that no downstream node consumes (a dangling output) is a **warning** per the §10
WG-031 permissive posture, retained in the AST and ignored — NOT an ingest error.

`outputs` is valid ONLY on `agentic` nodes. An `outputs` attribute on any other node type, on an edge, or at
the graph level is a reserved-attribute-out-of-position strict error per §10 WG-031.

Tags: mechanism, normative

---

### WG-057 — Per-node visibility invariant (insert in §4, after WG-056; the semantics of WG-055)

A node's brief is assembled from EXACTLY its declared inputs (§4 WG-055) — declared explicitly via the
`inputs` attribute or supplied by the role-default set — and from nothing else. All other run variables are
**structurally invisible** to that node: the single renderer ([execution-model.md §7.5]) is handed only the
resolved values of the node's declared-input set, so a value the node does not declare **cannot be reached
by the assembler**. This is a structural guarantee, not a convention: it is enforced at assembly, not by an
authoring rule that says "do not reference the value."

This invariant makes the implementer↔reviewer rubric leak **unexpressible** at the graph layer. The
reviewer's `rubric` is a reviewer-class input; the implementer declares `task` (and `feedback` on resume)
and does NOT declare `rubric` (§4 WG-055 / D-FIX-1), so no assembly path places the rubric in an implementer
brief. The rubric's value sources bind ONLY to the reviewer (the reviewer node's `prompt` plus the optional
per-bead rubric field — [execution-model.md §7.5]); the implementer's `task` is sourced from the bead task
text, which excludes both the reviewer node `prompt` and the bead rubric field.

**Boundary (normative, stated honestly).** This invariant guarantees precisely that *one role's
separately-sourced instructions cannot reach another role*. It does NOT prevent a workflow author from
embedding review criteria in the task prose itself — writing the answer into the question is telling the
implementer directly, not a cross-role leak, and is out of scope for this guarantee.

`goal` (§4 WG-044) is a default-visible declared input subject to this rule, not an ambient thread that
bypasses it: a `goal` value that embeds a rubric can reach only the nodes that declare or inherit `goal`,
and a node MAY exclude it. This overturns the WG-044 unconditional-broadcast behavior (see the WG-044
amendment); it closes the re-leak vector structurally.

Tags: mechanism, normative

---

### WG-058 — Declared-output type; verdict enum (insert in §4, after WG-057)

A declared output (§4 WG-056) MAY carry a type drawn from the minimal set
`string | number | bool | enum(...)`. Only `enum(m1, m2, ...)` carries load-time weight at v1: its members
are checked for shape at load (each member matches the enum-literal grammar of §6 WG-013), and it drives the
verdict↔edge-compatibility check of §9 WG-060 check 2. The other three types (`string`, `number`, `bool`)
are informative at v1 and carry no load-time check; an absent type is equivalent to `string`. This spec
introduces no general type lattice and no JSON-in-DOT; the enum form reuses the closed-enum-at-load pattern
already established for the `effort` attribute (§4 WG-042).

The reviewer node's `verdict` output is `enum(APPROVE, REQUEST_CHANGES, BLOCK)`.

**`verdict` is the typed NAME for the value that still surfaces as `outcome.preferred_label`.** No new
first-class `verdict` routing field is introduced: the reviewer's verdict continues to ride the
`outcome.preferred_label` channel that drives the §5 WG-010 step-2 cascade match, and workflow authors
continue to route on `outcome.preferred_label == 'APPROVE'` etc. §7 WG-019 stands unchanged; `verdict` is
the declared output's typed name for that same value, and its enum members are what §9 WG-060 check 2
validates the routing edges against.

Tags: mechanism, normative

---

### WG-059 — Override node-id addressing and `model_locked` marker (insert in §4, WG-042 family)

This clause defines the two **graph-side** halves of per-node model/effort/tool override. The
**serialization** of the override carried in the bead is deferred to the operator (D-A6 → this work's
`ISSUES.md` #1) and is an [execution-model.md §4.3] / bead-integration concern; WG owns only the addressing
vocabulary and one load check.

1. **Node-id addressing.** A per-node model/effort/tool override **addresses a node by its node id** — node
   ids are the graph's stable handles (§8 WG-021). An override that names a node MUST resolve to a node id
   in the graph's node set; an override naming a non-existent node is an ingest error (§9 WG-060 check 4,
   "name a real node"). The bare / flat `model:<alias>` form remains **run-wide** (the run-level default of
   [execution-model.md §4.3 EM-012b]); the node-addressed form layers above it. This spec does NOT define
   the bead label/block grammar that carries the override — that grammar is deferred (see the note below).

   > NOTE (open, ISSUES #1): the concrete serialization of a node-addressed override in the bead — a fenced
   > structured `harmonik` block in the bead body vs. node-addressed labels — is an operator decision
   > (D-A6). The recommendation is a fenced structured block carrying per-node `{tool, model, effort,
   > locked}`. WG's normative surface here is the node-id **addressing** vocabulary and the name-a-real-node
   > load check (§9 WG-060 check 4) only; the block's parse home is [execution-model.md §4.3] /
   > bead-integration.

2. **`model_locked` marker.** An `agentic` node MAY carry an optional `model_locked` attribute (boolean;
   value domain `{"true","false"}`, a non-boolean value is an ingest error per §10 WG-031, the run MUST NOT
   start). A node with `model_locked="true"` ignores per-bead escalation from the override band — it is the
   workflow author declaring "keep this node cheap even for hard beads." The resolution ladder in which the
   lock sits, and the per-run force-vs-lock precedence, are normative in [execution-model.md §4.3 EM-012b]
   (D-B5) and are not restated here.

   **This is task configuration, NOT model-version pinning.** `model_locked` fixes *which resolution band
   wins for this node*, not a specific external model version; harmonik does not bind to external
   model/tool versions.

`model_locked` is valid ONLY on `agentic` nodes. A `model_locked` attribute on any other node type, on an
edge, or at the graph level is a reserved-attribute-out-of-position strict error per §10 WG-031.

> NOTE (open, ISSUES #1): if the operator's chosen override serialization (ISSUES #1) carries the lock in
> its structured block, the `model_locked` node attribute defined here becomes redundant with that block; on
> that resolution WG-059's marker collapses to a cross-reference. Pending that decision, WG-059 places
> `model_locked` as the graph-static, author-owned `agentic`-node attribute so the lock has a normative
> home.

Tags: mechanism, normative

---

### WG-060 — Declared-I/O load checks (insert in §9, after WG-030, ~L503)

A loader MUST perform the following checks at parse time and reject the graph on any violation (the run MUST
NOT start), extending the §9 WG-024 / WG-025 / WG-028 validation battery. All four checks are strictly
additive: they reject newly-expressible declared-I/O errors and never relax the §5 WG-011
unconditional-fallback invariant, the §5 WG-010 five-step cascade, the §4 WG-001 closed node-type enum, or
the §17 WG-050 review-floor.

1. **Declared-required-input binding.** Every declared *required* input of a node reachable from the
   `start_node` MUST be bound — by a producer on an inbound path (a node declaring that name in its
   `outputs`, §4 WG-056) or by the node's role-default set (§4 WG-055) — or the input MUST be optional. An
   unbound *optional* input is a **warning**, not an error: its brief section is omitted (e.g. `feedback` at
   iteration 1). An unbound *required* input is an ingest error.

2. **Verdict↔edge compatibility.** For an edge from node N whose `condition` branches on
   `outcome.preferred_label == 'X'`: if N declares a `verdict` output typed `enum(...)` (§4 WG-058), then
   `X` MUST be a member of that enum; otherwise the graph is rejected (this catches an `APPROVE` / `APPROVED`
   typo before any tokens are spent). The producing node is resolved as the **edge's from-node** (D-A5); no
   new edge field is introduced and §5 WG-009's locked edge-field set is untouched.

   **Scope (normative — do not strengthen this into a global check).** This check fires ONLY on edges whose
   from-node is the verdict producer — i.e. the cascade evaluates an edge condition against its from-node's
   Outcome, so the check binds the branch value to that from-node's declared enum. A verdict value that is
   routed through an intermediate gate node (an edge whose from-node is not the verdict producer) is
   correctly NOT checked here. Edges whose from-node declares NO verdict enum keep today's permissive
   (shape-only) treatment (§6 WG-015), so existing graphs load unchanged.

3. **Back-edge traversal cap — reference, not a new check.** The requirement that "every back-edge carries a
   traversal cap" is ALREADY enforced by §9 WG-028 (every cyclic edge MUST carry an effective traversal
   cap). WG-060 introduces no duplicate rule; it cites §9 WG-028 for the back-edge cap (D-A8).

4. **Override names a real node.** Every node-addressed override clause (§4 WG-059) MUST resolve to a node id
   present in the graph's node set; an override naming a non-existent node is an ingest error. (Where the
   override is *parsed* is an [execution-model.md §4.3] / bead-integration concern — ISSUES #1; WG owns the
   name-a-real-node invariant only.)

Tags: mechanism, normative

---

## 2. Amendments to existing clauses (BEFORE → AFTER)

### WG-040 — Inline prompt attribute on `agentic` nodes (§4, L161)

**Amendment 1 — `prompt` is one renderer input (add to the composition paragraph).**

BEFORE (L171):

> `prompt` composes with a graph-level `goal` (§4 WG-044): `goal` is the run-level objective threaded via
> the run-level ExtraContext channel, while `prompt` is the node-level task body; they occupy distinct
> channels and do NOT double-inject (see [execution-model.md §7.5] launch-surface and the B↔E composition
> note).

AFTER:

> `prompt` is one of the node's declared/default brief inputs assembled by the single renderer
> ([execution-model.md §7.5]), alongside the node's declared `inputs` (§4 WG-055), its `role`, and the
> graph-level `goal` (§4 WG-044) — not a parallel injection channel. `goal` is the run-level objective and
> `prompt` is the node-level task body; the renderer composes them without double-injecting them (see
> [execution-model.md §7.5] launch-surface and the single-renderer composition contract). Every value the
> renderer places in a node's brief is a declared or default input of that node subject to the visibility
> invariant of §4 WG-057.

**Amendment 2 — reviewer-class `prompt` is no longer inert; it is the reviewer's rubric source (D-FIX-1).**

BEFORE (L173):

> **Reviewer-class scope (v1).** A `prompt` on a **reviewer-class** `agentic` node (resolved by the node's
> reviewer-class binding, e.g. `agent_type="reviewer"` / `handler_ref="claude-reviewer"`) is
> accepted-but-inert at v1: the reviewer's brief is sourced from the review-target artifact per
> [execution-model.md §4.3 EM-015d (sub-clause EM-015d-RIA)] and is NOT overridden by `prompt`. The loader
> retains the `prompt` attribute in the AST and emits no error; the value is ignored for reviewer-class
> dispatch. Reviewer-class `prompt` override is reserved for a future schema version.

AFTER:

> **Reviewer-class scope.** A `prompt` on a **reviewer-class** `agentic` node (resolved by the node's
> reviewer-class binding, e.g. `agent_type="reviewer"` / `handler_ref="claude-reviewer"`) is the reviewer's
> **rubric source**: it is one origin of the reviewer's `rubric` declared input (§4 WG-055), which the single
> renderer emits on the reviewer path. The generic reviewer rubric that earlier drafts hardcoded in the
> daemon's review-brief builder now lives as authored graph text on the reviewer node's `prompt` (the value
> origin is normative in [execution-model.md §7.5] — the deleted builder specifies the rubric origin there),
> plus an OPTIONAL per-bead rubric field (ISSUES #1). The reviewer's `rubric` is distinct from the
> implementer's `task`, and the implementer does NOT declare `rubric` (§4 WG-057). The reviewer's
> `task_context` declared input carries the review-target content (bead id/title/body verbatim + diff
> base/head SHAs — §4 WG-055; [execution-model.md §7.5]). The earlier "accepted-but-inert" carve-out is
> retired: the reviewer `prompt` is a live rubric input, not an ignored attribute.

Tags: mechanism, normative

---

### WG-044 — Graph-level `goal` attribute (§4, L240)

**Amendment — `goal` becomes a default-visible declared input, not an unconditional broadcast (hazard ii).**

BEFORE (L242):

> When `goal` is present, the daemon MUST surface it to `agentic`-node briefs (per [claude-hook-bridge.md §4
> CHB-028]) as the run-level objective, threaded through the run-level ExtraContext channel
> ([execution-model.md §7.5]); it composes with — and does NOT replace — any per-node `prompt` attribute (§4
> WG-040) and the bead-derived body.

AFTER:

> When `goal` is present, it is a **default-visible declared input** of every `agentic` node — present in
> each node's role-default input set (§4 WG-055) — subject to the per-node visibility invariant of §4 WG-057:
> a node MAY exclude `goal` from its `inputs`, and a node that does not declare or inherit `goal` cannot
> reach it. The single renderer ([execution-model.md §7.5]) assembles it into a node's brief as the run-level
> objective only for nodes whose declared/default input set includes it; it composes with — and does NOT
> replace — any per-node `prompt` attribute (§4 WG-040) and the node's `task` input. The earlier
> "unconditional ExtraContext thread surfaced to *every* agentic brief" framing is retired: `goal` is no
> longer an ambient broadcast, closing the re-leak vector whereby a `goal` embedding a rubric reached a node
> that should not see it (§4 WG-057).

Tags: mechanism, normative

---

### WG-014 — LHS whitelist (§6, L332)

**Amendment — `preferred_label` is enum-constrained when the producing node declares a verdict enum
(hazard iv).**

BEFORE (L337):

> - `outcome.preferred_label` — an arbitrary string declared by the just-completed node's Outcome per
>   [handler-contract.md §4.5].

AFTER:

> - `outcome.preferred_label` — a string declared by the just-completed node's Outcome per
>   [handler-contract.md §4.5]. It is an arbitrary string **except** when the producing node (the edge's
>   from-node) declares a `verdict` output typed `enum(...)` (§4 WG-058): in that case its value is
>   enum-constrained, and an edge branching on it is checked for enum membership by §9 WG-060 check 2. The
>   typed name for that value is `verdict` (§4 WG-058); no first-class `verdict` LHS is added — the value
>   continues to ride `outcome.preferred_label` (§7 WG-019).

Tags: mechanism, normative

---

### WG-015 — RHS literal types (§6, L346) and WG-025 — Edge-condition strictness (§9, L459)

**Amendment — extend the RHS enum-membership check to declared verdict enums.**

WG-015 BEFORE (L354):

> A loader MUST reject an edge whose RHS literal references an unknown enum member (strict policy per §10
> WG-025); status and failure-class enum membership is checked at parse time.

WG-015 AFTER:

> A loader MUST reject an edge whose RHS literal references an unknown enum member (strict policy per §10
> WG-025); status and failure-class enum membership is checked at parse time. Additionally, when the edge's
> from-node declares a `verdict` output typed `enum(...)` (§4 WG-058) and the edge branches on
> `outcome.preferred_label`, the RHS literal MUST be a member of that enum (§9 WG-060 check 2). An
> `outcome.preferred_label` RHS on an edge whose from-node declares NO verdict enum keeps today's permissive
> (shape-only) treatment.

WG-025 BEFORE (L461):

> A loader MUST parse each edge `condition` against the grammar of §6 WG-013 and reject the graph if any
> condition violates the dialect, the LHS whitelist (§6 WG-014), or the RHS literal types (§6 WG-015).
> Membership in a closed enum (status, kind, failure class) is checked at parse time; an unknown enum-member
> identifier on the RHS is a strict error.

WG-025 AFTER:

> A loader MUST parse each edge `condition` against the grammar of §6 WG-013 and reject the graph if any
> condition violates the dialect, the LHS whitelist (§6 WG-014), or the RHS literal types (§6 WG-015).
> Membership in a closed enum (status, kind, failure class) is checked at parse time; an unknown enum-member
> identifier on the RHS is a strict error. When the edge's from-node declares a `verdict` output typed
> `enum(...)` (§4 WG-058), a `preferred_label` RHS literal is additionally checked for membership in that
> enum per §9 WG-060 check 2; untyped `preferred_label` edges remain permissive (back-compat).

Tags: mechanism, normative

---

### WG-019 — Verdict surfacing is via `outcome.preferred_label` (§7, L397)

**Amendment — add one cross-ref sentence pointing at the typed name (WG-019 stands; reconcile, don't
contradict).**

BEFORE (L399):

> No first-class `verdict` field is introduced; the same `preferred_label` channel that drives the cascade's
> step-2 match (§5 WG-010) carries the verdict.

AFTER:

> No first-class `verdict` routing field is introduced; the same `preferred_label` channel that drives the
> cascade's step-2 match (§5 WG-010) carries the verdict. A reviewer node MAY declare a `verdict` **output**
> typed `enum(APPROVE, REQUEST_CHANGES, BLOCK)` (§4 WG-056 / §4 WG-058); `verdict` is the typed *name* for
> the value that still surfaces via `outcome.preferred_label` — it is a declared-output type used for
> load-time edge validation (§9 WG-060 check 2), NOT a second routing channel. This clause is unchanged by
> the verdict-enum work: routing remains on `outcome.preferred_label`.

Tags: mechanism, normative

---

### WG-002 — Node type catalog table (§4, L79)

**Amendment 1 — add the three new attributes to the `agentic` row's optional-attrs column (L85).**

BEFORE (`agentic` row, optional attrs cell):

> `prompt`, `non_committing`, `model`, `effort`, `idempotency_class`, `axis_tags`, `skills_ref`,
> `freedom_profile_ref`, `budget_ref`, `hook_ref`, `guard_ref`

AFTER:

> `prompt`, `non_committing`, `model`, `effort`, `inputs`, `outputs`, `model_locked`, `idempotency_class`,
> `axis_tags`, `skills_ref`, `freedom_profile_ref`, `budget_ref`, `hook_ref`, `guard_ref`

**Amendment 2 — extend the `agentic`-only attributes note bullet (L94).**

BEFORE (L94):

> - `prompt` (§4 WG-040), `non_committing` (§4 WG-041), `model`, and `effort` (§4 WG-042) are valid ONLY on
>   `agentic` nodes. …

AFTER (prepend to the existing bullet's first sentence):

> - `prompt` (§4 WG-040), `non_committing` (§4 WG-041), `model`, `effort` (§4 WG-042), `inputs` (§4 WG-055),
>   `outputs` (§4 WG-056), and `model_locked` (§4 WG-059) are valid ONLY on `agentic` nodes. `inputs` /
>   `outputs` name run variables the node reads / produces (a separate namespace from `context_keys`, §10
>   WG-031a, and a distinct channel from template params, §4 WG-045); `model_locked` is the escalation-lock
>   marker (task config, not version pinning). … [remainder of the existing bullet unchanged]

Tags: mechanism, normative

---

### WG-031 — Reserved-attribute set and position rules (§10, L514 / L516)

**Amendment 1 — add the three attribute names to the reserved set (L514).** Append to the reserved-set
enumeration, immediately after the `model`, `effort` entry:

> …, `inputs` (node-level, `agentic` only; comma-separated run-variable names per §4 WG-055), `outputs`
> (node-level, `agentic` only; `name:type` list per §4 WG-056 / §4 WG-058), `model_locked` (node-level,
> `agentic` only; value domain `{"true","false"}`, a non-boolean value is an ingest error per §4 WG-059), …

**Amendment 2 — extend the position rules (L516).** Add to the position-rule sentence:

> … `inputs`, `outputs`, and `model_locked` are node-level (`agentic` only); a use of any of the three
> outside an `agentic` node — on a `non-agentic`, `gate`, or `sub-workflow` node, on an edge, or at the
> graph level — is the WG-031 strict reserved-attribute-out-of-position error and the run MUST NOT start.
> `model_locked` additionally requires its value be drawn from `{"true","false"}` (a non-boolean value is
> the WG-031 strict value-domain error per §4 WG-059).

**Note (no reserved-set change for run-variable names).** The identifiers that appear *inside* an `inputs` or
`outputs` value — `task`, `feedback`, `rubric`, `task_context`, `verdict`, `notes`, `goal` — are
run-variable NAMES, not DOT attribute names. They are NOT added to the reserved set, matching the treatment
of `context_keys` list members (§10 WG-031a), which are likewise not reserved attribute names.

Tags: mechanism, normative

---

### §16.1 — Vocabulary diff against pre-existing specs (L725)

Add the following rows to the §16.1 table:

| Item introduced or pinned here | Source / status before this spec |
|---|---|
| §4 WG-055 declared node `inputs` | New normative content per kerf work `dot-hardening` (component A / D-A1, D-A2, D-FIX-1); per-node brief-assembly channel — a distinct namespace from `context_keys` (§10 WG-031a) and a distinct channel from template params (§4 WG-045); optional-with-role-default (implementer `{task}` +`feedback` on resume; reviewer `{task_context, rubric}`; every node also sees bead id/title + `goal`), so every existing `.dot` loads unchanged. |
| §4 WG-056 declared node `outputs` | New normative content per `dot-hardening` (D-A1, D-A2); producer→consumer run-variable declaration on an edge; reviewer produces `verdict` + `notes`; bulk work product stays in the git worktree, not an output; dangling output = warning. |
| §4 WG-057 per-node visibility invariant | New normative content per `dot-hardening` (D-A3, D-FIX-1); STRUCTURAL — the single renderer is handed only a node's declared-input values, so an undeclared value is unreachable; overturns the WG-044 unconditional-`goal`-broadcast behavior; makes the implementer↔reviewer rubric leak unexpressible at the graph layer. |
| §4 WG-058 declared-output type + verdict enum | New normative content per `dot-hardening` (D-A4); minimal type set `string|number|bool|enum(...)`, only `enum(...)` load-weighted (reuses the WG-042 `effort` closed-enum-at-load pattern); reviewer `verdict` = `enum(APPROVE,REQUEST_CHANGES,BLOCK)` — the typed NAME for the value that still rides `outcome.preferred_label` (§7 WG-019 stands, no new routing field). |
| §4 WG-059 override node-id addressing + `model_locked` | New normative content per `dot-hardening` (D-A5, D-A6, D-A7); per-node override addresses a node by node id (bare `model:<alias>` stays run-wide); `model_locked` = escalation-lock marker (task config, not version pinning); override *serialization* deferred to the operator (ISSUES #1); WG owns the addressing vocabulary + the name-a-real-node load check only. |
| §4 WG-060 declared-I/O load checks | New normative content per `dot-hardening` (D-A5, D-A8); loader rejects an unbound required input, a verdict↔edge mismatch (fires only on edges whose from-node is the verdict producer, per P4), and an override naming a non-existent node; the back-edge cap is a REFERENCE to §9 WG-028, not a duplicate rule. |
| §4 WG-040 reviewer-class `prompt` (revised) | Revised per `dot-hardening` (D-FIX-1): reviewer `prompt` is no longer "accepted-but-inert" — it is the reviewer's `rubric` source (the previously-hardcoded reviewer rubric moves to authored graph text); `prompt` is one declared/default renderer input, not a parallel injection channel. |
| §4 WG-044 `goal` (revised) | Revised per `dot-hardening` (D-A3): `goal` is a default-visible declared input subject to WG-057, no longer an unconditional broadcast to every agentic brief. |

Tags: (non-normative)

---

## 3. Exemplar update (D-A9 — scoped as a Spec-Draft artifact update, not a normative clause)

`specs/examples/standard-bead.dot` and its sidecar `specs/examples/standard-bead.md` (§17 WG-047–WG-052)
gain declared I/O so the canonical default graph is the leak-proof path out of the box:

- `implement` node gains `inputs="task, feedback"`.
- `review` node gains `inputs="task_context, rubric"` and
  `outputs="verdict:enum(APPROVE,REQUEST_CHANGES,BLOCK), notes:string"`.

This exercises WG-055–WG-060 in the default graph and makes the standard-bead reviewer's `verdict` output the
typed producer for edges 7/8/9 of WG-048 (the `review → close` / `review → implement` / `review →
close-needs-attention` branches), so WG-060 check 2 validates those `preferred_label` literals against the
enum. No change to the §17 topology (six nodes / ten edges / review-floor); WG-047–WG-052 and the golden test
`internal/workflow/scenario_standard_bead_hkp0kum_test.go` are unaffected in shape. The exemplar edit lands in
the Spec-Draft/exemplar pass, not as a new WG clause.

---

## Changelog fragment (WG)

> For the integrator: merge into `05-changelog.md`. Target file `specs/workflow-graph.md`, status
> **modified**. Additive minor bump (current top of the revision-history table is 0.3.1 → **0.4.0**). Add
> this row at the top of the §18 Revision history table.

| date | version | author | change |
|---|---|---|---|
| 2026-07-17 | 0.4.0 | kerf work `dot-hardening` | **Declared per-node I/O + structural visibility + verdict typing + override addressing.** New §4 WG-055 (declared node `inputs`; distinct namespace from `context_keys`, distinct channel from template params; optional-with-role-default so every existing `.dot` loads unchanged), WG-056 (declared node `outputs`; producer→consumer; reviewer produces `verdict`+`notes`; dangling output = warning), WG-057 (per-node visibility invariant — STRUCTURAL; the single renderer sees only a node's declared inputs, making the implementer↔reviewer rubric leak unexpressible; `task`/`rubric` are distinct inputs and the implementer does not declare `rubric`, per D-FIX-1), WG-058 (declared-output type `string\|number\|bool\|enum(...)`, only `enum(...)` load-weighted; reviewer `verdict=enum(APPROVE,REQUEST_CHANGES,BLOCK)` is the typed NAME for the value that still rides `outcome.preferred_label`), WG-059 (override node-id addressing + `model_locked` marker — task config not version pinning; override serialization deferred to operator, ISSUES #1), WG-060 (declared-I/O load checks: unbound-required-input; verdict↔edge compatibility fired ONLY on edges whose from-node is the verdict producer, per P4; back-edge cap = reference to WG-028; override-names-a-real-node). Amended WG-040 (`prompt` = one renderer input; reviewer-class `prompt` is now the reviewer's `rubric` source, retiring the "accepted-but-inert" carve-out, per D-FIX-1), WG-044 (`goal` = default-visible declared input subject to WG-057, no longer an unconditional broadcast), WG-014 + WG-015 + WG-025 (`preferred_label` is enum-constrained when the producing node declares a verdict enum; untyped edges stay permissive), WG-019 (cross-ref to the typed `verdict` name; routing still on `outcome.preferred_label` — no new routing field), WG-002 (`agentic` optional-attrs + note bullet add `inputs`/`outputs`/`model_locked`), WG-031 (reserved set + position rules add `inputs`/`outputs`/`model_locked`, `agentic`-only; `model_locked` value domain `{"true","false"}`). §16.1 vocab-diff gains rows for WG-055–WG-060 + revised WG-040/WG-044. Exemplar `specs/examples/standard-bead.dot` gains declared I/O (leak-proof default). No prior requirement IDs renumbered or retired; strictly additive over v0.3.1. Refs: kerf work `dot-hardening`. |

---

## Under-specified points carried from the design into this draft

1. **Front-matter `version` field is stale (bookkeeping, not in this work's scope).** The spec's front-matter
   `version:` field reads `0.1.0` (research findings §0, L9) while the §18 revision-history table has
   advanced to `0.3.1`. This changelog fragment follows the *table* (0.3.1 → 0.4.0). Whether to also correct
   the front-matter field is a pre-existing drift the design did not address; flag for the integrator, do not
   silently rewrite the front-matter here.

2. **`model_locked` home is contingent on ISSUES #1 (D-A7, design under-spec #2).** The design left the lock
   marker as "structured config (D-A6) OR a node attribute." This draft places `model_locked` as an
   `agentic`-node attribute (the graph-static, author-owned half) so it has a normative home now, and marks
   the contingency inline in WG-059 with a `> NOTE (open, ISSUES #1)`. If the operator's chosen override
   serialization carries the lock in its block, WG-059's marker collapses to a cross-ref — the draft says so,
   but the resolution is operator-deferred.

3. **Reviewer-`prompt` rubric-origin wording assumes the EM renderer clause (D-FIX-1).** The WG-040 amendment
   states the reviewer `prompt` is *a* rubric source and cross-refs [execution-model.md §7.5] for the value
   origin (the deleted daemon builder). WG deliberately does not re-specify the bead serialization of the
   optional per-bead rubric field (ISSUES #1) — this draft names the input and defers the field's parse home.
   If EM's renderer clause lands the rubric origin differently, the WG-040 cross-ref target must be updated at
   integration; the normative WG surface (naming `rubric` as a reviewer-only input) is stable regardless.

4. **`inputs`/`outputs` grammar edge cases the design did not enumerate.** The design fixed the identifier
   grammar (`^[a-z][a-z0-9_]*$`) and the one-line comma-separated shape, but did not state loader behavior for
   a duplicate name within one `inputs` list, or an `outputs` `name:type` pair with a malformed `type`. This
   draft treats a malformed `enum(...)` as a shape error at load (consistent with WG-058's "checked for shape
   at load") and leaves duplicate-name handling to the loader as a warning (WG-031 permissive posture); if the
   operator wants duplicate names to be a strict error, that is a one-line tightening to WG-055.
