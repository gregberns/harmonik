# 03 — Research: `specs/workflow-graph.md` (WG), requirements A1–A6

> RESEARCH pass. Grounds the WG change in the CURRENT spec + code and pins exact insertion points.
> Does NOT design new text. All citations are `WG-id` / `file:line` against `/Users/gb/github/harmonik`.
> Scope: Component A of `02-components.md` (declared node I/O + verdict typing + load checks).

## 0. Current-state anchors (spec structure)

`specs/workflow-graph.md` — 865 lines, `status: draft`, front-matter `version: 0.1.0` (line 11 — stale;
changelog last bumped to 0.2.0 at line 865; the 02-specs findings' "v0.3.1" label is not reflected in the
front-matter). Section map and the exact clauses each requirement touches:

| clause | section / line | current content (one line) |
|---|---|---|
| WG-001 node types | §4, L71 | closed enum of four `{agentic, non-agentic, gate, sub-workflow}` (unchanged by A) |
| WG-002 node catalog table | §4, L79 | per-type required/optional attr table; where new node attrs land |
| §4 attr summary bullet | L94 | lists `prompt`/`non_committing`/`model`/`effort` as agentic-only inputs |
| WG-039 tool_command | §4, L145 | `non-agentic` tool node |
| WG-040 prompt | §4, L161 | inline `prompt` REPLACES bead body (implementer); inert on reviewer at v1 (L173) |
| WG-041 non_committing | §4, L179 | clean-exit-without-HEAD-advance |
| WG-042 model/effort | §4, L221 | per-node `model`/`effort`; `model` opaque, shape-only; `effort` closed enum; agentic-only |
| WG-043 class/model_stylesheet | §4, L232 | informative-only, deferred hk-1xzg3 |
| WG-044 goal | §4, L240 | graph-level free-form string threaded to EVERY agentic brief via ExtraContext |
| WG-045 template params | §4, L248 | `__TOKEN__` post-parse per-attr splice; UNTRUSTED; verbatim except `tool_command` |
| WG-046 substitution ordering | §4, L262 | parse→substitute→validate invariant |
| WG-009 edge fields | §5, L270 | locked EM-002 field set; NO new edge fields; no `model`/`effort` on edge |
| WG-010/011/012 cascade | §5, L276–302 | 5-step cascade + unconditional-fallback invariant |
| WG-013 edge dialect | §6, L304 | restricted equality; `==`/`!=`/`&&` only; no `<`/`>`/`||` |
| WG-014 LHS whitelist | §6, L332 | `outcome.{status,preferred_label,failure_class,kind}`, `context.<key>` |
| WG-015 RHS literals | §6, L346 | string / int / closed-enum member |
| WG-019 verdict via preferred_label | §7, L397 | verdict rides `outcome.preferred_label`; NO first-class `verdict` field |
| WG-024 reserved strictness | §9, L446 | required-attr / forbidden-attr / ref-resolution load checks |
| WG-025 edge-condition strictness | §9, L459 | dialect/LHS/RHS enforced at load |
| WG-027 well-formedness | §9, L471 | start/terminal/reachability checks |
| WG-028 cycle bounding | §9, L487 | every cyclic edge MUST carry an effective traversal cap |
| WG-031 reserved-attr set | §10, L507 | the closed reserved-name list + position rules (L514/L516) — any new attr name lands here |
| WG-031a context_keys | §10, L534 | graph-level comma-list; LHS NOT validated against list at v1 |
| WG-032 AST retention | §10, L544 | permissive attrs retained, dispatcher MUST NOT read |
| WG-047–052 standard-bead | §17, L761 | canonical default graph: 6 nodes / 10 edges / review-floor |
| WG-050 review-floor | §17, L825 | `close` has EXACTLY ONE inbound edge (`review→close` on APPROVE) — unchanged |
| §13 open questions incl OQ-WG-002 | L609 | OQ-WG-002 context-key type-pinning OPEN (L615) — directly adjacent to A1/A3 |
| §16.1 vocab-diff table | L723 | every new normative posture needs a row here (L734/L742/L744 are the model) |
| changelog | L865 | additive version-bump entry required |

**Current high-water mark: WG-054** (L205, graph-level `no_progress_guard`). New clauses continue at **WG-055+**.
Note WG IDs are NOT sequential by section (WG-039–046, 053–054 all live in §4); a new ID is just the next integer.

---

## A1 — Declared node inputs

- **(a) Current state.** No "declared inputs" concept exists. A node's brief is assembled implicitly: bead
  body (or `prompt` verbatim, WG-040 L165) + graph-level `goal` broadcast to *every* agentic node (WG-044 L242)
  + node `role` (a Go `Node.Role` field surfaced into the brief per `ast.go:104-255`, but **not a normative WG
  clause** — no WG-id governs `role`). Reviewer brief is sourced separately from the review-target artifact
  (WG-040 L173, review-loop-mode only). Inputs are ambient, not declared; there is no per-node read scoping.
- **(b) Insertion point.** NEW clause **WG-055** in §4 (node attribute catalog, after WG-042 ~L230), plus a row
  in the WG-002 table (L79) and the §4 summary bullet (L94). The attribute name (e.g. `inputs="…"`) must be
  added to the WG-031 reserved set (L514) with an agentic-only position rule (L516).
- **(c) Maps onto existing primitives.** A declared-input list is a NEW named channel that *subsumes* today's
  ambient sources: the bead body / `prompt` become one declared input; `goal` becomes a declared (or default)
  input rather than an unconditional broadcast; the reviewer's rubric becomes a declared input the implementer
  does NOT list. It is NOT template params (see Hazard i) and NOT `context_keys` (that is an edge-LHS
  declaration, WG-031a, not a brief-assembly channel).
- **(d) Open design questions.** Attribute syntax on one readable DOT line (MODEL.md §Types: "one readable line
  per node")? The "small fixed safe default set" (A1) — what is in it, and is it role-derived? Back-compat: an
  undeclared node gets an implicit default set by role (02-components A1) — spec the default-by-role mapping so
  every existing `.dot` still loads.
- **(e) Hazard.** Back-compat is load-bearing: WG changes are historically **strictly additive** (changelog L865
  "No prior requirement IDs renumbered"). A required-`inputs` attribute would break every existing graph — it
  MUST be optional-with-role-default.

## A2 — Declared node outputs

- **(a) Current state.** No node-output declaration. The reviewer's verdict is emitted out-of-band via
  `harmonik write-review-verdict` → `.harmonik/review.json`, read back by the daemon and surfaced ONLY as
  `outcome.preferred_label` (WG-019 L399; code `dot_cascade.go:954,990`). Feedback *content* (notes/flags) has
  no spec'd forward channel in dot mode (02-specs §6). `notes` reaches nothing today.
- **(b) Insertion point.** NEW clause **WG-056** in §4, paired with WG-055 (a producer node declares outputs;
  a consumer node declares one as an input). Reserved-set + WG-002-table + summary-bullet updates as A1.
- **(c) Maps onto existing primitives.** An output is the producer half of the A1 input on an edge — the
  producer→consumer pair (MODEL.md §3). It formalises what `preferred_label` does for `verdict` today and adds
  `notes` as a first-class produced value. Bulk code stays in the git worktree, NOT an output (MODEL.md §"stays
  out of variables"; problem-space non-goal "No artifact/bulk-value store").
- **(d) Open design questions.** Is an output bound to an edge, or to the run's global variable set with the
  edge only gating visibility? (Operator decision: variables are GLOBAL, graph controls visibility — 01
  L64-66.) Must every declared output be consumed, or may outputs be dangling (warn vs error)?
- **(e) Hazard.** The verdict output overlaps `outcome.preferred_label` (WG-019) — two surfaces for one value
  (see Hazard iv). Design must reconcile, not duplicate.

## A3 — Per-node visibility rule

- **(a) Current state.** No visibility scoping. `goal` is threaded into EVERY agentic brief unconditionally
  (WG-044 L242; code `workloop.go:4070-4081`). The reviewer's checklist leaks to the implementer today because
  both builders receive the whole bead body (01-problem-space L13-19). Nothing structurally prevents a node from
  seeing a value.
- **(b) Insertion point.** This is the *semantics* of WG-055/WG-056, not a separate attribute — expressed as a
  normative sub-clause of WG-055 ("a node reads ONLY its declared inputs plus the safe default set; all other
  run variables are invisible to its brief"). May warrant its own clause **WG-057** for the load-check-visible
  invariant. Also touches WG-044 (L242) — `goal`'s "surfaced to every agentic brief" wording must be
  reconciled with visibility (goal becomes a default-visible input, or itself declared).
- **(c) Maps onto existing primitives.** Directly overturns the WG-044 unconditional-broadcast behavior. This is
  the structural leak fix: "the leak becomes unexpressible" (MODEL.md §4).
- **(d) Open design questions.** Is `goal` in the safe default set (visible to all) or must it be declared? If
  default-visible, Hazard ii bites. What else is unconditionally safe (bead id/title for traceability — the
  WG-040 L165 header-retention rule already keeps title+id in the artifact header)?
- **(e) Hazard.** See Hazard ii — the visibility rule must be *structural* (assembly cannot reach an undeclared
  value), not merely "don't reference it," or a `goal` that embeds the rubric re-leaks.

## A4 — Typed verdict enum

- **(a) Current state.** `outcome.preferred_label` is "an **arbitrary string**" (WG-014 L337); the reviewer's
  verdict rides it with NO first-class `verdict` field (WG-019 L397-399). Edge RHS is checked only for shape /
  closed-enum membership *of the failure-class/status/kind enums* (WG-015 L346, WG-025 L459) — `preferred_label`
  RHS values (`'APPROVE'` etc.) are NOT enum-checked; an `'APPROVED'` typo passes load today. `effort` is the
  existing model for a node-level closed enum (WG-042 L226, ingest-strict).
- **(b) Insertion point.** NEW clause **WG-058** in §4 (a declared output MAY carry an `enum(...)` type; the
  reviewer's `verdict` output is `enum(APPROVE|REQUEST_CHANGES|BLOCK)`). Amends WG-014 (L337 — `preferred_label`
  is no longer purely "arbitrary" when a producer declares a verdict enum) and WG-015/WG-025 (add verdict-enum
  membership to the RHS check). Enables the A6 verdict↔edge compatibility check.
- **(c) Maps onto existing primitives.** Reuses the `effort` closed-enum-at-load pattern (WG-042 L226). Types
  otherwise stay minimal — `string|number|bool|enum(...)` (MODEL.md §Types); no general lattice, no JSON-in-DOT
  (02-components A4).
- **(d) Open design questions.** Where is the verdict enum declared — on the reviewer node's output decl, or a
  graph-level type? How does a load-time check know *which* node produces the value an edge branches on (edge
  `preferred_label` has no producer back-pointer today)?
- **(e) Hazard.** See Hazard iv — collision with WG-019's "no first-class verdict field."

## A5 — Per-node model/effort/tool override addressed by node id (carried in the bead)

- **(a) Current state.** Per-bead model config today = flat **labels** `model:<alias>` / `effort:<level>`
  (`modelpreference.go:166,212-232`; WG-042/EM-012b tier-1). Beads carry `Labels []string` + free-text
  `Description` only (`beadrecord.go:19-28`) — no structured-config field. Labels are **run-wide**, NOT
  node-addressed: there is no way today to say "use opus for the `implement` node only" from the bead. The `.dot`
  node `model=` (WG-042) is graph-static and, per the operator, currently *wins over* the bead label
  (`dot_cascade.go:1351`) — the priority the change flips (that flip is Component B/EM, not WG). Template params
  are on the queue item, not the bead (`types.go:181-185`), and are UNTRUSTED (WG-045) — not a bead-config
  channel.
- **(b) Insertion point.** WG owns only the *graph-side* half: NEW clause **WG-059** stating (1) an override
  clause names a real node id (load error if not — this is the A6 check), and (2) a node MAY be marked `locked`
  so escalation skips it. The *serialization of the override in the bead* is largely an EM/bead-integration
  concern, but WG must define the node-id addressing vocabulary the override binds to. Reserved-set touch only if
  a new node attribute (e.g. `model_locked`) is added (L514/L516).
- **(c) Maps onto existing primitives.** Node-addressing reuses node IDs (already the graph's stable handles,
  WG-021). The `locked` marker is a new agentic-node boolean attribute in the WG-042 family.
- **(d) Open design questions — serialization (DO NOT DECIDE; record options).** Operator REJECTED the throwaway
  `implement@model=…` string (01 L71). Beads offer exactly two structured surfaces today:
  - **Option 1 — structured labels.** e.g. `model:implement=opus`, `effort:review=high`, or a namespaced
    `node.implement.model:opus`. Fits the existing label-scan (`modelpreference.go:214`), stays in the bead,
    survives `br` round-trips. Cost: label grammar gets a node-address dimension; conflict rules (WG-031-style)
    needed.
  - **Option 2 — a fenced structured block in the bead `Description`.** e.g. a fenced `harmonik-config`
    YAML/TOML block parsed from `BeadRecord.Description` (`beadrecord.go:22`). Carries richer structure (per-node
    tool+model+effort+lock) without stretching label grammar. Cost: a new parse surface on free text; no existing
    precedent in code.
  - Note: the bare/simple form MAY still mean run-wide (01 L71) — the design must keep the flat `model:<alias>`
    label working as the run-wide default and layer node-addressing above it.
- **(e) Hazard.** "This is task config, NOT version pinning" (01 L73-75; memory `no-external-version-binding`) —
  the spec text must say so explicitly or a reviewer will flag it. An explicit escalation that cannot resolve
  must **fail loud, not silently downgrade** (01 L75) — but that resolution/fail-loud rule is HC/EM (C2), WG only
  carries the addressing + the name-a-real-node load check.

## A6 — Load-time checks

- **(a) Current state.** §9 already has a rich load-check battery: WG-024 (required/forbidden attrs, ref
  resolution), WG-025 (edge dialect/LHS/RHS), WG-027 (start/terminal/reachability), WG-028 (every cyclic edge has
  an effective traversal cap), WG-029 (sub-workflow acyclicity). NONE of them check declared-input binding,
  verdict↔edge compatibility, or override-names-real-node — those concepts do not exist yet.
- **(b) Insertion point.** NEW clause **WG-060** in §9 (§ validation rules, after WG-030 ~L503), enumerating the
  four A6 checks: (1) every declared *required* input is bound (by an edge/producer or a default) or is optional;
  (2) every edge condition that branches on a verdict branches only on a value the producing node's verdict enum
  can emit (verdict-enum ↔ edge-condition compatibility — depends on A4/WG-058); (3) every back-edge carries a
  traversal cap (this OVERLAPS WG-028 — reconcile, do not duplicate: WG-028 already requires a cap on every
  *cyclic* edge; A6's "back-edge" is a subset); (4) every override clause names a real node (A5/WG-059).
- **(c) Maps onto existing primitives.** Extends the WG-024/025/028 pattern (loader MUST reject at parse, run
  MUST NOT start). Check (2) is the payoff of typing the verdict (A4) — it makes an `APPROVE`/`APPROVED` typo a
  load error (MODEL.md §Types).
- **(d) Open design questions.** Is check (1) an error or a warning when an input is unbound but marked optional?
  Does check (2) fire only when the edge's producer declares a verdict enum (so untyped `preferred_label` edges
  stay permissive for back-compat)?
- **(e) Hazard.** Unchanged-invariant guardrails (02-components A6, 01 L100-102): WG-011 unconditional-fallback,
  the five-step cascade, the closed node-type enum (WG-001), and WG-050 review-floor MUST remain intact — the new
  checks are additive, never a relaxation.

---

## Key hazards (investigated, recorded)

**(i) Template params cannot carry a multi-line rubric — declared inputs MUST be a DISTINCT channel.**
`ValidateTemplateParams` rejects ANY `unicode.IsControl(r)` rune, which includes newline `\n`
(`internal/core/templateparams.go:91-98`); cap is `MaxTemplateParamValueBytes = 8192` (`templateparams.go:34`).
A real multi-line review rubric fits neither. WG-045 (L258) documents the newline/8KB rejection as a
shell-injection defense — a hard constraint, not incidental. Consequence for WG: the A1 declared-input channel
must be its OWN vocabulary, NOT a rebranding of the WG-045 template-param surface (which 01 L96-99 and MODEL.md
§Types both call out explicitly). Design must NOT route rubric/task/notes text through `__TOKEN__` params.

**(ii) The `goal` broadcast can re-leak the rubric — visibility must be STRUCTURAL.**
WG-044 (L242) threads `goal` into *every* agentic brief unconditionally; code confirms
(`workloop.go:4070-4081`). MODEL.md §"Why not the two extremes" flags exactly this: a convention-based leak fix
fails because "`goal`/`role` are broadcast to every node, so a natural `goal='…__RUBRIC__…'` re-leaks." So A3's
visibility rule cannot be "authors, please don't put the rubric in `goal`" — the assembler must be *unable* to
reach an undeclared value. This forces a reconciliation of WG-044's broadcast wording (make `goal` a declared /
default-visible input subject to the same visibility rule, not an ambient thread).

**(iii) A5 override serialization — beads carry only Labels + Description today.**
`BeadRecord` (`beadrecord.go:19-28`) has `Labels []string` and free-text `Description` — no structured field.
Current node-blind precedent: flat `model:<alias>` label (`modelpreference.go:214`). The two structured options
for node-addressed config are labels (`model:implement=opus`) or a fenced block in `Description` (see A5(d)).
**Not decided here** — recorded as options. The operator's only hard "no" is the `implement@model=…` string
(01 L71).

**(iv) A4 verdict enum vs WG-019 "verdict rides preferred_label, no first-class verdict field."**
WG-019 (L397-399) is explicit: "No first-class `verdict` field is introduced; the same `preferred_label` channel
… carries the verdict." A4 introduces a typed `verdict` *output*. These can coexist (the declared output can be
the typed name for the value that surfaces as `preferred_label`), but the design MUST reconcile the wording, or
WG-019 and WG-058 contradict. WG-014 (L337) calling `preferred_label` "an arbitrary string" is the specific
sentence in tension with a load-time verdict-enum check. Related: `outcome.preferred_label` is produced by the
reviewer out-of-band today (`review.json` → daemon read-back, `dot_cascade.go:1202`), so a load-time check that
an edge branches only on emittable verdicts needs the graph to know which node produces the verdict — a
back-pointer that does not exist in the edge model (WG-009 locked field set).

---

## Open questions for change-design

1. **Attribute syntax** for declared inputs/outputs on one readable DOT line (A1/A2) — new `inputs=`/`outputs=`
   attributes, and their grammar; must be optional-with-role-default for strict-additive back-compat.
2. **Safe default input set** (A1/A3) — its exact membership, whether `goal` and bead id/title are in it, and
   whether it is role-derived.
3. **`goal` reconciliation** (A3, Hazard ii) — does WG-044's unconditional broadcast become a default-visible
   declared input subject to the visibility rule?
4. **Verdict declaration site + WG-019 reconciliation** (A4, Hazard iv) — where the `enum(...)` verdict type is
   declared, and how it coexists with "no first-class verdict field" and WG-014's "arbitrary string."
5. **Producer back-pointer for the verdict↔edge check** (A6 check 2) — how the loader learns which node produces
   the value an edge branches on, given the WG-009 locked edge-field set.
6. **A5 override serialization** (A5(d), Hazard iii) — structured labels vs `Description` block; how the bare
   form stays run-wide; where the WG/EM/bead-integration boundary sits (WG owns node-id addressing + name-a-real-
   node load check only).
7. **`locked`-node marker** (A5) — new agentic-node attribute name + reserved-set/position-rule placement.
8. **Back-edge vs WG-028 overlap** (A6 check 3) — is "every back-edge carries a traversal cap" already covered by
   WG-028's every-cyclic-edge rule, or does it need a distinct clause?
9. **Exemplar touch** — does `specs/examples/standard-bead.dot` (WG-047–052) gain declared I/O so the default is
   the leak-proof path (02-components "Possible additional touch"; 01 L132)? Scoped as exemplar update, not a new
   normative clause.
10. **Bookkeeping** — new §16.1 vocab-diff rows (L723) and a changelog entry (L865) for WG-055…WG-060, plus the
    WG-031 reserved-set additions (L514/L516) for each new attribute name.
