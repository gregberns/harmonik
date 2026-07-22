# 04 — Change design: `specs/workflow-graph.md` (WG)

> CHANGE-DESIGN pass. For each requirement A1–A6: the exact clause new/amended, the SHAPE of the new
> normative posture, and how it satisfies the requirement. One step before final normative text.
> Expands decisions **D-A1..D-A9** (`DECISIONS.md`). Every touch cites a `WG-id` + line against the
> current `specs/workflow-graph.md`. **High-water mark: WG-054** (L205) — new clauses continue at WG-055.

## ID assignments (final)

| new id | section | subject | decision |
|---|---|---|---|
| WG-055 | §4 (after WG-042, ~L230) | declared node **inputs** attribute | D-A1, D-A2 |
| WG-056 | §4 (paired with WG-055) | declared node **outputs** attribute | D-A1, D-A2 |
| WG-057 | §4 (semantics sub-clause) | per-node **visibility invariant** (structural) | D-A3 |
| WG-058 | §4 (after WG-056) | declared-output **type**, incl. verdict `enum(...)` | D-A4 |
| WG-059 | §4 (WG-042 family) | override **node-id addressing** + `model_locked` marker | D-A5, D-A6, D-A7 |
| WG-060 | §9 (after WG-030, ~L503) | declared-I/O **load checks** | D-A5, D-A8 |

Amendments: WG-040 (L161), WG-044 (L240), WG-014 (L332), WG-015 (L346)/WG-025 (L459), WG-028 (L487,
reference only), WG-031 reserved set (L514) + position rules (L516), WG-002 note bullet (L94), §16.1
vocab-diff (L725), changelog (L861). Cross-cutting: **declared inputs are a DISTINCT channel from
template params (WG-045, not `__TOKEN__` splices); declared node I/O is a SEPARATE namespace from
`context_keys` (WG-031a).** Both stated verbatim in WG-055.

---

## New clause shapes

### WG-055 — Declared node inputs (§4, new)
An `agentic` node MAY carry an optional `inputs` attribute: one readable DOT line, a comma-separated list
of **run-variable names** the node's brief MAY read (e.g. `inputs="task, feedback"`). Shape:
- Each name is an identifier (`^[a-z][a-z0-9_]*$`), naming a **run variable** — a value set at launch or
  **produced by a node** (WG-056). Distinct from WG-045 template params: an input carries arbitrary
  multi-line text (task, rubric, notes), so it is NOT a `__TOKEN__` splice (WG-045 rejects newlines /
  caps 8 KB — hazard i). Distinct from `context_keys` (WG-031a): that is the edge-condition-LHS registry;
  `inputs` is the brief-assembly channel. They MAY share the underlying store but are declared separately.
- **Visibility is structural** (WG-057): the assembler can reach ONLY the node's declared inputs plus the
  role-default set. There is no "and everything else is discouraged" — an undeclared variable is
  unreachable to that node's brief.
- **Absent `inputs` → the role-default set** (back-compat, strictly additive). Role-default sets:
  - **implementer-class:** `{task}`; on a resume iteration where a feedback variable is bound, also
    `{feedback}` (unbound at iteration 1 → omitted — see WG-060 check 1, and EM's iteration binding).
  - **reviewer-class:** `{task_context, rubric}`, where `task_context` = bead id/title/body + diff
    base/head SHAs (per D-B2 / EM; reviewer body stays verbatim).
  - **every node additionally sees** `bead.id` + `bead.title` (traceability, already in the artifact
    header per WG-040 L165) + `goal` (default-visible declared input per WG-057/D-A3).
- Valid ONLY on `agentic` nodes; on any other position it is a WG-031 reserved-attr-out-of-position error.

### WG-056 — Declared node outputs (§4, new)
An `agentic` node MAY carry an optional `outputs` attribute: `outputs="name:type, ..."` (`type` optional;
only `enum(...)` is meaningful at v1 — WG-058). An output names a value the node **produces as it runs**
that a downstream node consumes by declaring the same name as an input (the producer→consumer pair on an
edge; MODEL.md §3). The reviewer node declares `outputs="verdict:enum(APPROVE,REQUEST_CHANGES,BLOCK),
notes:string"`. Bulk work product (the diff) is NOT an output — it flows through the shared git worktree
(MODEL.md §"stays out of variables"); `outputs` carries only small instruction/control text. A dangling
output (declared, unconsumed) is a **warning**, not an error (permissive, WG-031/WG-032 posture). Valid
ONLY on `agentic` nodes.

### WG-057 — Per-node visibility invariant (§4, new; the semantics of WG-055)
A node's brief is assembled from EXACTLY its declared inputs (WG-055) — declared explicitly or via the
role-default set — and nothing else. All other run variables are **structurally invisible** to that node:
the single renderer (EM) is given only the resolved values of the node's declared-input set, so a value
the node does not declare **cannot be reached**, not merely "should not be referenced." This overturns the
WG-044 unconditional-`goal`-broadcast behavior (see amendment below) and makes the implementer↔reviewer
leak *unexpressible*: the rubric is a reviewer input the implementer does not declare, so no assembly path
places it in the implementer brief (MODEL.md §4; hazard ii). This is the structural leak fix; it is not a
convention.

### WG-058 — Declared-output type; verdict enum (§4, new)
A declared output (WG-056) MAY carry a type from the minimal set `string | number | bool | enum(...)`.
Only `enum(m1,m2,...)` carries load-time weight at v1 (reuses the WG-042 L226 `effort` closed-enum-at-load
pattern): the members are checked for shape at load, and drive the WG-060 check-2 edge-compatibility rule.
No general type lattice, no JSON-in-DOT (02-components A4). The reviewer's `verdict` output is
`enum(APPROVE,REQUEST_CHANGES,BLOCK)`. **`verdict` is the typed NAME for the value that still surfaces as
`outcome.preferred_label`** — no new routing field is introduced; WG-019 (L397) stands (see amendment).

### WG-059 — Override node-id addressing + `model_locked` (§4, WG-042 family; new)
Two graph-side halves of A5 (the bead **serialization** is deferred to the operator, D-A6 → ISSUES #1, and
is an EM/bead-integration concern — WG owns only the addressing vocabulary + one load check):
1. **Addressing.** A per-node model/effort/tool override **addresses a node by its node id** (node ids are
   the graph's stable handles). WG-060 check 4 makes an override that names a node absent from the node set
   a load error ("name a real node"). The bare/flat `model:<alias>` label stays **run-wide** (D-B6); the
   node-addressed form layers above it. WG does NOT define the bead block/label grammar (D-A6).
2. **Lock marker.** An `agentic` node MAY carry `model_locked="true"` (boolean, value domain
   `{"true","false"}`, non-boolean = ingest error). A locked node ignores per-bead escalation from the
   override band (the author saying "cheap even for hard beads"); the resolution ladder + force-vs-lock
   precedence are EM's (D-B5/D-B7). **This is task config, NOT version pinning** (stated in the clause text
   — memory `no-external-version-binding`; hazard). Valid ONLY on `agentic` nodes.

### WG-060 — Declared-I/O load checks (§9, new; after WG-030)
A loader MUST check at parse time and reject the graph (run MUST NOT start) on any violation:
1. **Input binding.** Every declared *required* input of a reachable node is bound — by a producer on an
   inbound path (WG-056) or the role-default set — or is marked optional. An unbound *optional* input is a
   warning, not an error (its brief section is omitted — e.g. `feedback` at iteration 1).
2. **Verdict↔edge compatibility.** For an edge from node N whose condition branches on
   `outcome.preferred_label == 'X'`: if N declares a verdict `enum(...)` output (WG-058), `X` MUST be an
   enum member — else load error (catches `APPROVE`/`APPROVED` typos before tokens are spent). The producer
   is resolved as the **edge's from-node** (D-A5) — no new edge field (WG-009 stays locked). Edges whose
   from-node declares **no** verdict enum stay permissive (back-compat).
3. **Back-edge cap — reference, not a new check.** A6's "every back-edge carries a traversal cap" is
   ALREADY WG-028 (L487: every cyclic edge carries an effective cap). WG-060 cites WG-028; it introduces no
   duplicate rule (D-A8).
4. **Override names a real node.** Every node-addressed override clause (WG-059) resolves to a node id in
   the node set; an override naming a non-existent node is a load error. (Where the override is *parsed* is
   EM/bead-integration; WG owns the name-a-real-node invariant.)
These extend the WG-024/025/028 battery and are strictly additive: WG-011 unconditional fallback, the
five-step cascade, the closed node-type enum (WG-001), and the WG-050 review-floor are untouched.

---

## Amendments to existing clauses

- **WG-040 (L161–176) — prompt is one renderer input.** Add a sentence: `prompt` is one of the node's
  declared/default brief inputs assembled by the single renderer (EM), alongside its declared `inputs`,
  `role`, and `goal` — not a parallel channel. The reviewer-class "accepted-but-inert / brief sourced from
  the review-target artifact" carve-out (L173) is reconciled with the reviewer role-default input set
  (`task_context` = that artifact's content; WG-055). No change to `prompt`-replaces-body for implementers.
- **WG-044 (L240–245) — goal becomes a default-visible declared input, not an unconditional broadcast
  (hazard ii).** Amend the "the daemon MUST surface it to *every* agentic brief" sentence: `goal` is a
  **default-visible declared input** subject to WG-057 — present in every node's role-default input set, but
  a node MAY exclude it. Delete the "unconditional ExtraContext thread" framing that let a
  `goal="…rubric…"` re-leak. Closes the re-leak vector structurally.
- **WG-014 (L332–343) — verdict-enum reconciliation (hazard iv).** Amend the `outcome.preferred_label`
  bullet (L337 "an arbitrary string"): `preferred_label` is an arbitrary string **except** when the
  producing node declares a verdict `enum(...)` output (WG-058), in which case its value is enum-constrained
  and edges branching on it are checked by WG-060 check 2. `verdict` is the typed name for that value; no
  first-class `verdict` routing field is added — **WG-019 (L397) stands unchanged** (add one cross-ref
  sentence to WG-019 pointing at WG-058 for the typed name).
- **WG-015 (L346) / WG-025 (L459) — RHS enum check.** Extend the RHS-membership check: when the edge's
  from-node declares a verdict enum, a `preferred_label` RHS literal MUST be an enum member. Untyped
  `preferred_label` edges keep today's permissive (shape-only) treatment (back-compat).
- **WG-028 (L487) — back-edge cap is a reference (D-A8).** No text change; WG-060 check 3 cites it. Note in
  §16.1 that A6's back-edge cap is satisfied by WG-028, not a new clause.
- **WG-031 reserved set (L514) + position rules (L516).** Add `inputs`, `outputs` (node-level, `agentic`
  only), `model_locked` (node-level, `agentic` only; value domain `{"true","false"}`, non-boolean = ingest
  error). Position rules: all three are `agentic`-only; a use outside that position is the WG-031 strict
  out-of-position error. `verdict`/`notes`/`task`/`rubric`/`feedback`/`task_context` are run-variable
  NAMES inside `inputs`/`outputs` values, NOT attribute names — they are not added to the reserved set
  (matching how `context_keys` list members are not reserved attrs).
- **WG-002 note bullet (L94).** Add `inputs`, `outputs`, `model_locked` to the list of `agentic`-only
  attributes; add the three to the `agentic` row's optional-attrs column (L85).
- **§16.1 vocab-diff (L725).** New rows: WG-055/056 declared node I/O (single-renderer layer; distinct from
  WG-045 params and WG-031a context_keys); WG-057 structural visibility invariant (overturns WG-044
  broadcast); WG-058 declared-output types + verdict enum (typed name for the WG-019 `preferred_label`
  value); WG-059 override node-id addressing + `model_locked` (serialization deferred, D-A6); WG-060
  declared-I/O load checks (back-edge cap = WG-028 reference).
- **Changelog (L861).** Additive minor bump (current 0.3.1 → **0.4.0**): "declared per-node I/O
  (WG-055/056) + structural visibility (WG-057) + declared-output types incl. verdict enum (WG-058) +
  override node-id addressing & `model_locked` (WG-059) + declared-I/O load checks (WG-060); WG-040/044
  reconciled to the single-renderer input model; WG-014/015/025 verdict-enum reconciliation (WG-019 stands,
  no new routing field); WG-031 reserved set + position rules add `inputs`/`outputs`/`model_locked`."

**Exemplar (D-A9, scoped as Spec-Draft update, not a normative clause):** `specs/examples/standard-bead.dot`
+ sidecar gain declared I/O so the default graph is the leak-proof path — `implement inputs="task, feedback"`
/ `review inputs="task_context, rubric" outputs="verdict:enum(...), notes:string"`. Exercises WG-055–060
out of the box. No change to WG-047–052 topology/edges.

---

## Per-requirement change design (A1–A6)

### A1 — Declared node inputs
- **(i)** NEW **WG-055** (§4) + reserved-set/WG-002/summary-bullet touches.
- **(ii)** The spec will require that an `agentic` node MAY declare `inputs` (one DOT line, comma-separated
  run-variable names) naming the values its brief may read; absent the attribute, the node gets its
  role-default set. Inputs are a distinct channel from template params and a separate namespace from
  `context_keys`.
- **(iii)** Existing graphs stay valid: `inputs` is optional; an undeclared node resolves to the role-default
  set that reproduces today's brief (implementer `{task}`, reviewer `{task_context, rubric}`, plus
  id/title/goal). Strictly additive — no existing `.dot` renumbered or rejected.
- **(iv)** Introduces WG-060 check 1 (required-input binding).
- **(v)** EM consumes WG-055 (the single renderer assembles from declared inputs — B1); the role-default
  sets pair with EM's iteration binding (feedback unbound at iter 1 — B4). No HC dependency.

### A2 — Declared node outputs
- **(i)** NEW **WG-056** (§4), paired with WG-055.
- **(ii)** The spec will require that a node MAY declare `outputs="name:type,..."` for values it produces; an
  output is consumed by a downstream node that declares the same name as an input (producer→consumer on an
  edge). The reviewer produces `verdict` + `notes`.
- **(iii)** Optional attribute; graphs without it behave as today (verdict still rides `preferred_label`,
  WG-019). Dangling output = warning, not error. Diff stays in git, not an output.
- **(iv)** Feeds WG-060 check 1 (a declared input is "bound" when a producer on an inbound path declares it).
- **(v)** EM B3 (feedback as a produced value on the back-edge, replacing `reviewer-feedback.iter-N.md`);
  the reviewer's `notes` is the feedback-content output. No HC dependency at WG layer.

### A3 — Per-node visibility rule
- **(i)** NEW **WG-057** (§4, semantics of WG-055) + AMEND **WG-044** (L242).
- **(ii)** The spec will require that a node's brief is assembled from exactly its declared/default inputs and
  that all other run variables are structurally unreachable to it; `goal` becomes a default-visible declared
  input subject to this rule, not an ambient broadcast.
- **(iii)** The role-default set makes an undeclared node see exactly what it sees today, so migration is
  invisible; only the leak vector is removed. `goal` still reaches every node by default.
- **(iv)** No standalone load check — the invariant is enforced at assembly (the renderer is handed only the
  declared-input values); WG-060 check 1 covers binding.
- **(v)** EM B1 (single renderer is the enforcement point — "the leak is unexpressible"); C3 leak-oracle
  asserts on the daemon-emitted role→source-keys manifest (D-C5). Cross-file: EM + HC.

### A4 — Typed verdict enum
- **(i)** NEW **WG-058** (§4) + AMEND **WG-014** (L337), **WG-015** (L346)/**WG-025** (L459); **WG-019**
  (L397) stands with a cross-ref only.
- **(ii)** The spec will require that a declared output MAY carry `enum(...)`; the reviewer's `verdict` is
  `enum(APPROVE,REQUEST_CHANGES,BLOCK)`, and that is the typed name for the value surfaced via
  `outcome.preferred_label` — no new routing field.
- **(iii)** Back-compat: verdict-enum checking fires ONLY when the producing node declares an enum; untyped
  `preferred_label` edges stay permissive, so existing graphs load unchanged. WG-019's "no first-class
  verdict field" is preserved.
- **(iv)** Introduces WG-060 check 2 (edge branches only on emittable verdicts; producer = edge from-node).
- **(v)** EM/HC reference the enum name for the renderer + manifest; no new EM/HC field required.

### A5 — Per-node model/effort/tool override addressed by node id
- **(i)** NEW **WG-059** (§4) + reserved-set add `model_locked`.
- **(ii)** The spec will require that a per-node override addresses a node by its node id and that a node MAY
  be marked `model_locked` to skip escalation; it will state this is task config, not version pinning. WG
  defines only the addressing vocabulary + the name-a-real-node load check; the bead **serialization** is
  deferred (D-A6 → ISSUES #1, EM/bead-integration).
- **(iii)** Additive: `model_locked` is optional; node-id addressing reuses existing node ids (WG-021); the
  bare `model:<alias>` label stays run-wide (D-B6), so deployed flat-label beads are unaffected.
- **(iv)** Introduces WG-060 check 4 (override names a real node).
- **(v)** EM owns the resolution ladder/seal/force-vs-lock (D-B5/B7/B8); HC owns per-tool concrete resolution
  (D-C2). WG is the graph-side addressing surface those consume. Cross-file: EM + HC.

### A6 — Load-time checks
- **(i)** NEW **WG-060** (§9), plus the WG-028 reference for the back-edge cap.
- **(ii)** The spec will require the loader to reject, before dispatch: an unbound required input; an edge
  branching on a verdict the producing node's enum cannot emit; and an override naming a non-existent node —
  and to rely on WG-028 for the back-edge cap.
- **(iii)** All four are additive rejections of newly-expressible errors; they never relax WG-011,
  the five-step cascade, WG-001, or WG-050 (02-components A6 guardrails). Graphs without declared I/O trip
  none of them.
- **(iv)** This clause *is* the load-check set (checks 1, 2, 4; check 3 = WG-028).
- **(v)** None new — the checks are graph-local. The forceable-round-trip payoff (goal 5) is realized once
  EM's renderer + C3's harness land on top.

---

## Under-specified points in DECISIONS.md for this component

1. **Dangling-output severity (WG-056).** D-A1/A2 spec the producer→consumer pair but not whether a
   declared-but-unconsumed output is a warning or error. I chose **warning** (consistent with the WG-031
   permissive posture and the "outputs may be dangling" open question in findings A2(d)); flag if the
   operator wants it strict.
2. **`model_locked` home (WG-059).** D-A7 leaves the lock marker as "structured config (D-A6) OR a node
   attribute." Since D-A6 defers the bead serialization to the operator but WG must own *something*
   graph-side, I placed `model_locked` as an `agentic`-node attribute (the graph-static, author-owned half).
   If the operator's chosen serialization (ISSUES #1) carries the lock instead, WG-059's marker becomes a
   pure cross-ref — confirm which side owns it.
