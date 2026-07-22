# 02 — Components (affected spec areas)

> Decomposed by spec file (spec-jig convention: `{component}` = a spec area). Three existing files
> are affected; no new spec file is required. Each entry lists the change, concrete requirements
> (what the spec must describe after the change), and dependencies. Requirements are "what," not
> "how the text reads" (that is Change Design).

Current ID high-water marks (probed): WG-054, HC-071; EM uses §-numbered clauses (dot binding at
§7.5; review-loop at §4.3.EM-015d; validator at §7.5.3.EM-057). New IDs continue from there.

---

## Component A — `specs/workflow-graph.md` (WG): declared per-node I/O + verdict typing + load checks

**Change:** give a node a way to declare the named values it reads (inputs) and the named values it
produces (outputs), with per-node read visibility; type the reviewer's verdict output as a closed
enum; carry the per-node model/effort/tool override surface; and add the load-time checks these
enable. This is the graph-language layer of the single-renderer design.

**Requirements — the spec must describe:**
- A1. **Declared node inputs.** An agentic node declares the named run-variables it may read. A node
  reads only its declared inputs plus a small fixed safe default set. Absent a declaration, a node
  gets an implicit default set by role (back-compat — every existing `.dot` still loads).
- A2. **Declared node outputs.** A node may declare named values it produces as it runs (the reviewer
  produces `verdict` + `notes`). An output is bound to a downstream node that declares it as an input
  (the producer→consumer pair on an edge).
- A3. **Per-node visibility rule.** Variables are global to the run, but a node sees only what it
  declares. The implementer node does not declare — cannot see — the review variable.
- A4. **Typed verdict enum.** The reviewer's verdict output is a closed enum
  (`APPROVE | REQUEST_CHANGES | BLOCK`). Types otherwise stay minimal (strings for task/rubric/notes);
  no general type lattice, no JSON-in-DOT ports.
- A5. **Per-node model/effort/tool override addressed by node id.** The override surface (carried in
  the bead as structured config — serialization decided in Design) names a node id and overrides that
  node's model/effort/tool defaults. Override clauses that name a non-existent node are a load error.
  A node may be marked locked so escalation skips it. This is task config, NOT version pinning.
- A6. **Load-time checks (fail before spending tokens):** every declared required input is bound (by
  an edge/producer or a default) or is optional; every edge condition branches only on a verdict value
  the reviewer can emit (verdict-enum ↔ edge-condition compatibility); every back-edge carries a
  traversal cap; every override clause names a real node. Unconditional-fallback invariant (WG-011),
  five-step cascade, closed node-type enum, and review-floor (WG-050) are unchanged.

**Dependencies:** foundational — A defines the vocabulary that EM's renderer and feedback contracts
consume. No dependency on B or C. Depends on nothing new.

---

## Component B — `specs/execution-model.md` (EM): single renderer + feedback-on-edge + model seal/replay

**Change:** define the single brief-renderer contract; make reviewer→implementer feedback a value on
the back-edge (reconciling the EM §7.5 prohibition that made dot-mode feedback undefined); flip the
model-resolution ladder so a per-bead escalation beats the graph default; and define the
per-(node, iteration) concrete-selection seal plus the replay-reads-the-seal rule.

**Requirements — the spec must describe:**
- B1. **Single brief renderer.** One contract assembles any node's brief from exactly {its declared
  inputs} + {its prompt} + {its role} + {run goal}. It replaces the two role-specific builders. Role
  framing (worktree discipline, reviewer read-only constraint, verdict-emission instruction) is
  selected by role, not by which builder ran. The implementer's brief cannot contain the review
  checklist because the checklist is not a declared input — the leak is unexpressible.
- B2. **Reviewer brief carries task context.** The reviewer's brief must still include what change it
  is judging (bead id/title/body reference + diff base/head SHAs), alongside the rubric — the reviewer
  must not be under-contexted about the task the diff is meant to satisfy.
- B3. **Feedback as a produced value on the back-edge.** On REQUEST_CHANGES the reviewer's
  `verdict` + `notes` flow to the implementer's declared feedback input and are rendered by the same
  renderer, identically across all tools. This replaces the review-loop-only `reviewer-feedback.iter-N.md`
  file. Reconcile EM §7.5: the current "a dot run MUST NOT produce reviewer-feedback/review-target"
  clause is replaced by the value-on-edge channel. (Note: `dot_cascade.go` already writes the file in
  dot mode — hkwixms — so code and spec currently disagree; this reconciles them.)
- B4. **Iteration-1 vs resume.** On iteration 1 the feedback input is unbound → its section is
  omitted. On a bounce-back it is bound → rendered. This is what makes a deterministic round-trip
  forceable once the leak is gone.
- B5. **Model-resolution ladder flip.** A per-bead escalation (and a per-run force) sits ABOVE the
  node's `model=`/`effort=` default. The node default becomes soft (overridable), with a per-node
  lock for nodes that must stay cheap.
- B6. **Per-(node, iteration) concrete-selection seal.** At each agentic-node dispatch, after
  resolving alias->concrete for that node's effective tool, record the concrete `(tool, model, effort)`
  keyed by `(node_id, iteration)`. The back-edge re-dispatches the same node, so an iteration-blind
  key would overwrite iteration 1's record — the composite key is load-bearing.
- B7. **Replay reads the seal.** A replay/resume MUST read the sealed concrete for `(node_id,
  iteration)` and MUST NOT recompute from the live catalog/labels/config. This keeps a rerun
  byte-identical even if the model catalog was edited afterward — the confirmation mechanism the
  operator wants.

**Dependencies:** B1–B4 depend on A1–A4 (declared inputs/outputs, verdict enum). B5–B7 are the model
leg and depend on A5 (override surface) but are otherwise independent of the renderer.

---

## Component C — `specs/handler-contract.md` (HC): transport-only adapter + per-tool resolution + test seams

**Change:** define each tool's adapter as transport-only (it delivers the assembled brief, never
re-derives content); fix per-tool model resolution (the pi/codex silent-ignore) with fail-loud on an
unresolvable explicit escalation; and define the test seams that let an in-process harness force a
round-trip and assert the real per-handler argv.

**Requirements — the spec must describe:**
- C1. **Transport-only adapter.** The tool adapter receives one already-assembled brief and chooses
  only the delivery envelope (claude: brief file + pane kick; pi/codex: positional seed argv). It does
  not build role-specific content. This collapses the paste-inject-vs-resume-seed divergence into one
  representation with per-tool transport.
- C2. **Per-tool model-alias resolution.** A node's model alias resolves to a per-tool concrete via a
  catalog, plumbed to the value all tools read (`rc.model`), fixing the claude-only path that silently
  discards a pi/codex node model. An explicit tool-namespaced concrete on the wrong tool fails loud at
  load (author error). An explicit escalation (force / bead label) that cannot resolve for the target
  tool fails loud — NOT a silent downgrade. A default-band alias that misses the catalog degrades +
  warns (drift-safe). Catalog is operator-owned, hot-reloadable, keep-last-good on parse failure.
- C3. **Test seams (coverage contract).** The handler contract must expose seams sufficient for an
  in-process harness to: keep the REAL launch-spec builder, tee the built launch spec into a recorder
  (argv-faithfulness tap), swap only the executable to a handler-faithful twin, and run against real
  scratch git. The harness must be able to (a) force a deterministic REQUEST_CHANGES->resume->APPROVE
  round-trip via a role x iteration twin script, (b) assert per-handler argv/binary matches the
  declared tool (the c074 mis-route guard), and (c) assert the implementer's brief does NOT contain
  the reviewer's rubric key (the leak oracle). **Honesty caveat to spec plainly:** claude's resume
  feedback is delivered by tmux paste-inject, not argv, so the recorder cannot capture it — the claude
  row proves "daemon assembled/wrote the brief," and claude's delivery is proven by its existing unit
  tests + live, while pi/codex delivery is argv-faithful. The leak oracle must assert on a **typed
  input manifest the daemon emits** (role->source keys), not on rendered markdown substrings, so tests
  survive the renderer change.

**Dependencies:** C1 depends on B1 (single renderer produces the brief the adapter transports). C2 is
the model leg's tool-facing half (pairs with B5–B7). C3 depends on B1/B3 (there must be a single brief
+ a feedback value to assert on) and is what proves the whole change end-to-end.

---

## Goal -> component coverage (every goal maps to >=1 area)

| Problem-space goal | Covered by |
|---|---|
| 1. No leak, structurally | A1, A3, B1, C3 (oracle) |
| 2. One renderer, all roles/tools | B1, C1 |
| 3. Feedback as value on back-edge | A2, B3, B4 |
| 4. Per-node model/effort/tool in bead + logged + replayable | A5, B5, B6, B7, C2 |
| 5. Forceable round-trip + argv-faithfulness | A6 (forceable-by-construction), C3 |

## Cross-file dependency order

**A (WG vocabulary) -> B (EM renderer/feedback/model) -> C (HC transport/resolution/test seams).**
Within B, the renderer/feedback leg (B1–B4) and the model leg (B5–B7) are independent and can be
drafted in parallel. C3 (test harness) is drafted last because it asserts the behavior A+B define.

## Possible additional touch (confirm in Research)

- `specs/examples/standard-bead.dot` + its `.md` sidecar — if the canonical default graph gains
  declared I/O so it exercises the new path out of the box. Likely yes (so the default is the
  leak-proof path), but scoped as an exemplar update, not a fourth normative component.
