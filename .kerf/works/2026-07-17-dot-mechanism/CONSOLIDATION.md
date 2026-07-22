# DOT mechanism — consolidation & decision brief

> **Status:** synthesis of the 2026-07-17 exploration (5 research + 4 brainstorm + 4 adversary streams).
> Nothing here is locked. This is the decision-ready readout for the operator to walk through.
> Source material: `research/*/findings.md`, `brainstorm/{A1,A2,B,C}-*.md`, `adversary/ADV-*.md`.

---

## 0. The frame (what we set out to do)

Make DOT — the implement→review→feedback workflow engine that is the heart of harmonik — **powerful and
easily controllable**, and **robustly testable**. Four threads, three from the operator + one structural:

1. **Model control** — a bead should be able to say "this is hard, run me on Opus" without editing the `.dot`.
2. **Typed parameters** — supply per-bead instructions (task, review rubric) as inputs that flow to the right
   role, with strict types (FP discipline). Question posed: global params vs params that flow on edges?
3. **Feedback routing** — the reviewer→implementer re-implement signal (today a file) done better.
4. **Robust testing** — an integration layer that can drive the whole cascade deterministically.

Constraints were explicitly OFF: no prior comment or rejection is binding.

---

## 1. Ground truth — how DOT works today (code-verified)

**Models.** Resolution is a 4-tier walk (`ResolveModelPreference`, `internal/daemon/modelpreference.go`):
bead label `model:<alias>` → project `config.yaml` → env var → hardcoded Go default (`claude-code = sonnet/medium`,
`modelpreference.go:156-159`). A bead **can** override its model today — but only via a text label, and:

- **The operator's exact wall is real and has a line number.** `dot_cascade.go:1351` layers the node's `model=`
  attribute *on top of* the run preference, so a `.dot` node saying `model=sonnet` **overrides** a bead labeled
  `model:opus`. The node default wins over the per-bead escalation — backwards from what you want.
- **Node `model=` is silently ignored for pi/codex.** `nodeModelForHarness` (`dot_cascade.go:1182-1187`) honors a
  node model pin only for the claude family; pi/codex node pins are discarded with no error.

**Params.** `__UPPER_SNAKE__` template params live on the queue item (`Item.TemplateParams`), spliced into the
`.dot` text post-parse (WG-045/046). They are **untyped strings, global** (an edge-scoped param is a hard error),
and — critically — `internal/core/templateparams.go:91` **rejects any control character including newlines** and
caps values at 8 KB (a shell-injection defense). So template params **cannot carry a multi-line review rubric.**

**Per-role input + the leak (P1/P5).** The bead *body* carries both the task and the `## Review` rubric, and the
daemon renders the **whole body** into BOTH role briefs — implementer (`agent-task.md`) and reviewer
(`review-target.md`) — via the same code, for **all** harnesses (not just pi/codex). So the implementer sees the
reviewer's rubric and one-shots the task; a review round-trip can't be forced. The reviewer's own node `prompt`
is **inert at v1** and its brief comes from the body, not its node prompt — per-role routing is not wired.

**Feedback.** DOT mode has **no spec'd feedback channel** — `EM §7.5` says a `dot` run MUST NOT produce
`reviewer-feedback.iter-N.md`; that machinery is `review-loop`-mode only. Delivery is also **harness-divergent**:
claude uses tmux **paste-injection** + a rewritten `agent-task.md`; pi/codex use a separate **resume seed prompt**
(`agentseedprompt.go`, the `8dbe5a17` fix) — two mechanisms kept in sync by hand.

**Verdict routing.** Equality-only edges over `outcome.preferred_label` (`== 'APPROVE'`, etc.); the REQUEST_CHANGES
back-edge carries `traversal_cap="3"`. No `<`/`>`/`||`.

**Testing.** The in-process cascade driver **already exists** (`daemon.ExportedDriveDotWorkflow`, ~30
`dot_*_test.go`) running against real ephemeral scratch git, with a real `LaunchSpecBuilder` DI seam
(`dot_cascade.go:1402`). The gap: tests swap the whole executable for `/bin/sh`, so the daemon's **real per-handler
argv is never built or asserted**, and no test forces a deterministic multi-iteration round-trip.

---

## 2. The central decision — the input/feedback model

We ran two poles and had each attacked by an independent adversary. **Both adversaries converged on the same
answer: neither pole as-specified — a middle design.**

### The two poles

- **A1 (maximal):** every node is a typed function with named input/output ports, a closed type lattice,
  edge-scoped dataflow, an artifact `ref<KIND>` store, typed feedback objects. The leak becomes structurally
  impossible; all four problems collapse into one model.
- **A2 (minimal):** un-defer the reviewer node's `prompt`, keep untyped template-params as the only value channel
  + a one-line `params=` declaration (enum as the only type), generalize the existing feedback file to dot mode.

### Why neither survives as-is

- **A1 types a dataflow that doesn't exist.** An LLM node's real output is a **git commit**, not a value. A1 itself
  concedes free-text can't be statically typed, so the apparatus reduces to the few enums A2 already types — at
  ~5x cost, maximum blast radius (deletes the load-bearing review-loop path + reroutes the buggy resume path
  through 4 new subsystems in one migration), and it makes `.dot` files *harder* to author (worse on "easy control").
  A1's own §6 leaves model resolution **unchanged** — so it doesn't even fix the operator's headline problem.
- **A2's value channel is technically broken.** Template params **can't carry a rubric** (newline rejection,
  §1 ground truth). Its **enum typing validates the wrong thing** — the verdict that routes edges is a *runtime
  reviewer output*, never a launch param. And its **leak fix is convention, not structure** — `goal` broadcasts
  into every node's context (`workloop.go:4070`), so a natural `goal="…rubric…"` re-leaks to the implementer.

### The recommended middle design (what both adversaries point to)

A small, **structural** per-role I/O surface — not a general type system, not stringly-typed params:

1. **Per-role brief = a named, role-scoped input, sourced structurally (not from the shared body, not from a
   broadcast `goal`, not from newline-hostile template params).** The implementer's task text and the reviewer's
   rubric are **distinct fields** that reach only their role. Concretely: give each role node its own input slot
   (reviewer brief ← reviewer node's own prompt/field; implementer brief ← task field), assembled by **one
   role-driven brief-assembler** (the one genuinely good idea extracted from A1 — collapse the two divergent
   builders into a single assembler). The leak becomes a **structural property of the assembler**, verifiable at
   load, not an author-discipline convention.
2. **Type the verdict OUTPUT, not the input params.** The routing-safety you actually want comes from a typed,
   enum'd reviewer verdict (`preferred_label ∈ {APPROVE, REQUEST_CHANGES, …}`) checked against the edge conditions
   at graph-load — so a `.dot` that branches on a label the reviewer can't emit fails before any tokens are spent.
3. **Structured feedback payload on the back-edge, one writer, cross-harness.** Keep A2's cheapest correct idea —
   generalize `reviewer-feedback.iter-N.md` to dot mode (delete the EM §7.5 prohibition; both harness resume paths
   already read the file, so this kills the divergence with ~zero per-harness work) — but carry a **structured
   payload** (verdict, failure_class, flags[], notes + a reference to the diff detail), not just flat prose.
4. **Cheap load-time validation** (A2's V3/V7, kept): required inputs present, enum in-set, back-edge has a
   `traversal_cap`, no dangling refs.
5. **Explicitly deferred** (A1, until a second real producer/consumer pair exists): the type lattice, the artifact
   `ref<KIND>` store, the edge dataflow binder, deletion of the review-loop Go path. These are priced for graph
   *diversity* (fan-in, many node roles, non-git bulk artifacts) that harmonik's single linear implement→review
   loop does not yet exercise. Design so they are an **extension, not a rewrite**, if that diversity ever arrives.

**On "global vs edge-scoped params" (the operator's open question):** the honest answer from the survey + the
adversaries is that harmonik's dominant graph has **no node→node value handoff** — the payload between implementer
and reviewer is the git commit, carried by the workspace, not a param. So edge-scoped param dataflow solves a
problem we don't have yet. Recommendation: **run-ambient globals** (repo/branch, goal, policy, max-rounds) + the
**role-scoped brief inputs** above; adopt true edge-scoped typed dataflow only if/when multi-node non-review graphs
become real. (This is a decision to confirm, not a fact — see §5.)

---

## 3. Model control — recommended

Adopt the two **real bug fixes**; revise the determinism mechanism; cut the sugar.

**ADOPT:**
- **Flip the ladder** so a per-bead escalation beats a node default. Make node `model=` a **soft default**
  (opt-out via `model_locked=true` for a node that must stay cheap). Fixes the `dot_cascade.go:1351` wall.
- **Fix the pi/codex silent-ignore.** Replace the claude-only `nodeModelForHarness` with a harness-portable alias
  resolver (`strong`/`fast` → per-harness concrete via a catalog, plumbed to `rc.model` which all harnesses read).
  An explicit cross-harness concrete on the wrong node fails **loud** at load.
- **`--force-model` / `--force-effort` at `queue submit`** — escalate a specific run without editing the `.dot`.

**REVISE (adversary-mandated, load-bearing):**
- **Seal correctly.** Resolve each node's alias to a **concrete `(model, effort)` and freeze it**, keyed by
  **`(node_id, iteration)`** — the REQUEST_CHANGES back-edge re-dispatches the same node, so an iteration-blind
  seal would overwrite iter-1's model. The catalog is mutable and hot-reloadable, so replay MUST read the seal,
  never recompute from the live catalog (else a daemon restart + a catalog edit binds one run to two model
  versions). This is the difference between prose and mechanism.
- **Fail-loud on the escalation band, degrade only on the default band.** An explicit `model:strong` on a bead the
  author flagged as hard, when `strong` can't resolve for that harness, must **fail loud** — not silently fall back
  to the weak default and burn the whole run. "Warn don't fail" is correct only for defaults.

**CUT (scope creep):** `model_stylesheet`/CSS selectors and any auto-escalation ladder — not justified at current
scale; revisit only with real graph diversity.

**Respect the project rule:** aliases are advisory, no hard version pins, graceful degradation on the default band.

---

## 4. Testing harness — recommended (scoped down)

The diagnosis and the seam are sound (**no false code claims — all verified**), but the original scope was oversold.

- **Adopt the seam:** keep the real `LaunchSpecBuilder`, tee the built `LaunchSpec` into a recorder, rewrite only
  `Binary` to a handler-faithful twin; keep **real scratch git** (cascade correctness *is* git semantics).
- **Scope it right:** only the **multi-iteration round-trip** test needs the full twin+git+`//go:build scenario`
  machinery (~1–2 tests). The handler-matrix / no-leak / reviewer-harness / routing checks need **no subprocess or
  git** — just upgrade the existing capture-and-abort stub to record the full built `LaunchSpec` at **unit speed.**
- **Two honesty caveats to design around:**
  - **claude resume-feedback is delivered by tmux paste-injection, not argv** — the recorder can't capture it, so
    the claude "① end-to-end" row proves only "daemon wrote the brief," bypassing the exact mechanism `8dbe5a17`
    fixed. pi/codex (argv seed) are genuinely faithful. Either add a paste-inject capture seam or state plainly
    that claude's resume path is proven differently (its 4 existing unit tests + live).
  - **The twin ignores the bead body by fiat**, so the round-trip test proves the **plumbing** (edges, resume,
    traversal_cap) — **not** that the P1 leak is fixed. The leak oracle is a *separate* test that asserts the
    reviewer's rubric is **absent** from the implementer's recorded brief (red today, green when §2.1 lands). Anti-
    rot: assert on a **typed input manifest the daemon emits** (role→source keys), not on markdown substrings.

---

## 5. Decisions for the operator to confirm

1. **Adopt the middle input model** (§2.3): structural per-role brief inputs + typed verdict output + structured
   feedback file, deferring A1's type-lattice/artifact-store. **Y/N?**
2. **Params: run-ambient globals + role-scoped brief inputs now; edge-scoped typed dataflow deferred** (§2 close).
   Confirm we do NOT need node→node value dataflow yet — or name a concrete near-future workflow that does.
3. **Model ladder flip to soft-default** (node `model=` yields to a per-bead escalation, opt-out via
   `model_locked`). Confirm the default direction (soft) is what you want, vs. node-pins-win-unless-forced.
4. **Escalation semantics:** bare `model:opus` = run-wide (today) vs. role-scoped `model:opus@implementer`
   (proposed). Which?
5. **Feedback payload:** flat markdown file (cheapest) vs. structured payload (verdict/flags/notes + diff ref).
   The structured version is recommended and cheap; confirm.
6. **Testing scope:** unit-speed capture for the matrix + one scenario round-trip; accept that claude's resume path
   is proven by unit tests + live rather than argv-asserted. **OK?**

## 6. Suggested sequencing (once decided)

1. **Per-role brief-assembler + leak-as-load-error** (§2.1) — unlocks everything, closes P1/P5, makes the round-trip
   forceable by construction, and lands the no-leak test oracle.
2. **Structured feedback generalized to dot mode** (§2.3) — closes the DOT feedback gap, unifies harnesses.
3. **Model ladder flip + pi/codex fix + correct seal** (§3) — the operator's headline control ask.
4. **Typed verdict output + load validation** (§2.2, §2.4).
5. **Scoped test harness** (§4) — capture-stub matrix first, then the one scenario round-trip.
6. `--force-model` run override; defer stylesheet, artifact store, edge dataflow.

All contract-level changes are **spec-first** (touch `specs/workflow-graph.md`, `specs/execution-model.md`
EM-015d, `specs/handler-contract.md`).
