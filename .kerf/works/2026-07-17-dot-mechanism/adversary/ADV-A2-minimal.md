# ADV — Adversarial review of A2 (Minimal / Pragmatic I/O)

> Target: `brainstorm/A2-minimal-pragmatic-io.md`. Ammunition: `brainstorm/A1-maximal-typed-io.md`
> + the five research findings + code seams verified against `/Users/gb/github/harmonik` HEAD.
> Posture: attack first, steelman second, verdict last.

---

## FATAL FLAWS

### F1. The value channel A2 anoints as "the ONLY one" physically cannot carry a real rubric or task.
A2 §1.1 / §7 make template params **the ONLY value-injection mechanism**, and §2 routes the
implementer's TASK and the reviewer's RUBRIC through `--param`. But template-param values are
hard-rejected at ingest for **any control character, newline included**:

- `internal/core/templateparams.go:91-98` — `for _, r := range v { if unicode.IsControl(r) { reject } }`.
- The doc-comment (`:69-71`) is explicit: *"a newline is a shell command separator"* — the ban is a
  deliberate injection defense, not an oversight, and it is a shared chokepoint (queue RPC + the
  substitution path). Values also cap at **8192 bytes** (`MaxTemplateParamValueBytes`).

Consequence: **a multi-line rubric or a multi-paragraph task cannot be passed as a param at all.**
Every realistic review rubric is multi-criterion / multi-line (see the actual `review-target.md`
Coverage-Check + Spec-Field-Name-Check structure, research 01 §2). A2's own worked example §6.2
hides this by using a shell line-continuation `\` so `RUBRIC` collapses to one physical line — the
example is *constructed around the limitation*. To ship A2 you must either (a) cram the whole rubric
onto one ≤8 KB line (unreadable, and still capped), or (b) relax `ValidateTemplateParams` to permit
newlines — which re-opens the exact `tool_command` shell-injection vector WG-046 closed. A2 nowhere
acknowledges this; it is a load-bearing contradiction between "params are the only channel" and the
existing untrusted-param hardening. **This alone sinks the design as written.**

Note A1 sidesteps it: A1 §2.1 assembles the reviewer brief from a typed `run.inputs.review_rubric`
**string input** (a distinct channel), not the newline-forbidden param splice. A2 explicitly
forecloses that alternative by declaring params "the ONLY value channel."

### F2. The one "type" A2 keeps (enum) validates values that never route; the value that routes has no type.
A2 §1.2 justifies enum-only typing thus: *"enum is the only 'type' worth having because it lets
graph-load verify the value against the edge conditions that branch on it (a reviewer verdict is
`enum(APPROVE,REQUEST_CHANGES,BLOCK)`)."* This is incoherent against the actual dataflow:

- The reviewer **verdict is a RUNTIME output** produced by the reviewer via `write-review-verdict` →
  `review.json` → `outcome.preferred_label` (research 01 §2-3). It is **never a launch `--param`.**
- A2's enum feature validates **launch-supplied params** ("a supplied value outside the set is a
  load error", §1.2). Launch params are the *static inputs* (TASK, RUBRIC) — and **nothing routes on
  them.** The flagship `VERDICT_ENUM:enum(APPROVE,REQUEST_CHANGES,BLOCK)` example is a value you would
  never write `--param VERDICT_ENUM=APPROVE` for; it is not a launch input.

So the enum surface either (a) checks params that never influence an edge = catches nothing real, or
(b) is secretly reaching for **edge-condition-RHS-vs-producer-type checking** — which requires typing
the reviewer's *output*, i.e. exactly the output-port typing A2 rejects as "speculative generality"
(§1.3, §5). A2 cannot have the routing-safety benefit it advertises without building the very thing it
refuses to build. The entire "smallest type surface that catches real errors" claim rests on this,
and it does not hold. A1 §4.3 gets this right: it types the reviewer's `verdict` **output** and checks
the conditional edge against the producer's declared enum — the check A2 gestures at but cannot perform.

### F3. The leak reappears through the `goal` broadcast channel — the fix is not "structural."
A2 §2.2 claims the split is *"structural: two attributes on two nodes, filled from two params… the
rubric never reaches the implementer."* But params are **global and substituted verbatim into every
attribute** (WG-046; research 01 §6), and `goal` is a graph-level string **threaded into EVERY agentic
node's ExtraContext** (research 01 §7, `workloop.go:4070-4081`). The moment an author writes the
natural `goal="Complete __TASK__ satisfying __RUBRIC__"` — or references `__RUBRIC__` in the
implementer node's `role=` — the rubric is re-broadcast into the implementer brief. A2's split holds
only under author discipline never to mention the reviewer's param in any shared attribute. That is a
convention, not a structural guarantee. A1's brief-assembles-only-from-declared-input-ports model
makes the leak *unexpressible*; A2 makes it *avoidable-if-careful* — a materially weaker P1 closure,
and P1 is the whole reason this work exists.

---

## SERIOUS CONCERNS

### S1. "Symmetric one-field plumbing" for Delta A/B is false across the handler matrix (P7).
A2 §2.1 sells the reviewer-prompt fix as *"the same one-field plumbing already done on the implementer
side (`claudelaunchspec.go:308-311`)."* In reality the implementer body-swap exists in **two**
divergent sites, and they already disagree:

- claude: `claudelaunchspec.go:308-311` swaps body→prompt **and guards `phase != Reviewer`**.
- pi/codex: `harnessregistry.go:207` `taskBody := rc.nodePrompt` with **no reviewer-phase guard**.

The reviewer brief is a **third** site (`buildReviewTargetContent` rendering `p.BeadBody`,
`agenttask_chb028.go:577`), and the pi/codex reviewer delivery differs again. So Delta A/B is a
cross-harness change touching ≥3 code paths with existing drift — precisely the P7 hazard the frame
calls out (c074: a "codex adapter" scenario that silently ran on the claude handler). A2 counts this
as ~one line; it is not, and "un-defer WG-040" understates the blast radius.

### S2. Params-as-task-text is an expressiveness regression versus the bead body.
A2 §2.3 says P4 "falls out for free." But the bead body can hold arbitrary multi-line text of any
size; the param channel cannot (F1: no newlines, 8 KB cap). A2 trades a working-if-leaky channel (bead
body) for a **more constrained** one for the task text itself. Large or structured task descriptions
regress.

### S3. Global-untyped params wall the operator's stated near-future within one or two graphs.
The operator wants params that "flow on the edges" and DOT "powerful." A2 keeps params **global** and
defers edge-scoped dataflow "until a second producer/consumer pair appears" (§1.3). That pair is not
hypothetical — it is the obvious next step:

- **planner → implementer → reviewer** (amolstrongdm's Coding/Validator/Debugger/Planner split,
  research 04 §183/302): a plan produced by the planner must reach *only* the implementer. A2 has **no
  channel** for a node-produced value bound to a specific successor except the untyped global `context`
  bag or the implementer-only hardcoded feedback file. A structured plan handoff has no home.
- **a structured RUBRIC object** (per-criterion weights, a file-list to touch): A2's string-only params
  can carry it only as opaque prose the agent must re-parse — no validation, the WG-046 stringly path.
- **fan-in / multiple reviewers voting**: no channel at all.

Each of these forces a from-scratch edge-dataflow addition later — the work A2 defers is the work the
operator most plausibly asks for next. "Let it earn its way in" is defensible only if the second pair
is far off; the role-split research suggests it is near.

### S4. Flat-markdown feedback loses the machine-readable fields the frame wants to route on.
A2 §3.3 argues a typed feedback envelope "buys nothing." But `reviewer-feedback.iter-N.md` is prose
+ a flags list; A1's `feedback.v1` carries `failure_class` (enum) and `flags: string[]` as
**structured, routable** fields. The frame explicitly wants routing on structured verdict data
(research 03 §128 names the `kind=verdict` payload as the additive path for exactly this). With A2 the
only structured surface an edge can branch on remains `outcome.preferred_label` (the coarse
APPROVE/RC/BLOCK) plus the optional `context.last_verdict` mirror — you cannot route on
`failure_class == 'coverage'` vs `'style'` because that data is buried in free-text notes. For a graph
that wants "coverage failures retry, style failures escalate," A2 has no answer.

### S5. Reviewer loses bead context under Delta B.
A2 §2.2: with reviewer `prompt="__RUBRIC__"`, "the bead body is no longer rendered into either brief."
But `review-target.md` today also renders the bead id/title/**body** (research 01 §2, `:610-616`) so
the reviewer knows *what change* it is judging. If the RUBRIC param is the sole body, the reviewer is
under-contexted about the task the diff is meant to satisfy. A2 assumes the rubric self-describes the
task; often it does not.

---

## WHAT A2 GETS RIGHT (that A1 does not)

1. **Speed to value on a RED cell.** The pi-dot cell is broken *now*. A2 is a schema bump + ~4 deltas
   on existing writers; A1 admits (§8.1) it is "the most-work option… a genuine subsystem" (typed
   store + `ref<KIND>` artifact store + edge binder + assembler rewrite + every harness adapter). For
   a leak that needs closing this cycle, A2 ships in a fraction of the surface.
2. **Back-compat is nearly free.** A2 deletes a prohibition and un-defers a reserved behavior; existing
   graphs are essentially untouched. A1's compat rests on an elaborate "implicit default port schema by
   role" layer that itself needs golden tests (A1 §8.6) and subtly re-assembles every existing brief.
3. **The feedback reuse is genuinely cheap and correct on the read side.** Both resume paths
   (`pasteInjectImplementerResume`, `implementerResumeSeedPrompt`) already read the same file (research
   01 §4). Lifting EM §7.5's dot-mode prohibition to let the existing `WriteReviewerFeedback` fire on a
   dot back-edge is a small, low-risk change that collapses harness divergence — whereas A1 *rebuilds*
   feedback as a typed port through every adapter, adding migration-time divergence risk, not removing it.
4. **Right about scale.** harmonik runs one dominant graph shape. A1's `ref<KIND>` store, edge binder,
   and JSON-in-DOT typing (A1 §8.5 concedes the `\"…\"` escaping is "genuinely unpleasant") are priced
   for graph *diversity* that does not exist today. YAGNI is a legitimate value; A2 honors it.
5. **`params=` declaration + V3/V7 are cheap, real wins** independent of the type debate: catching a
   typo'd token and an unbounded back-edge at load are strict improvements over today's residual-token
   blame-the-wrong-thing failure.

Net: A2's **feedback delta (C/D)** and its **validation deltas (V3/V7)** are the strong core and should
survive into any synthesis. Its **value-channel thesis (params-only, enum-only)** is the weak core.

---

## VERDICT

**A2 is right about the destination (minimal, scale-matched, ship-now) and wrong about the vehicle.**
Two of its four pillars are sound and cheap (generalize the feedback file to dot mode; add
`params=`/token/back-edge load checks). The other two — "template params are the ONLY value channel"
and "enum is the only type worth having" — are **technically broken**, not merely under-powered:

- **F1**: the param channel cannot carry a real (multi-line) rubric or task; the design's central
  example only works because it is written around the newline ban.
- **F2**: enum-on-launch-params validates values that never route; the routing value (verdict) is a
  runtime output A2 refuses to type, so the advertised routing-safety check cannot exist.
- **F3**: the leak fix is convention (don't mention RUBRIC in `goal`/`role`), not structure.

**Recommendation: do NOT adopt A2 as-specified.** Keep A2's Delta C/D (dot-mode feedback reuse) and
V3/V7 (load checks) — they are the best, cheapest ideas in either brief. Replace A2's value model with
the *minimum* of A1 that F1/F2/F3 force: a per-node **declared input** channel (so the rubric enters as
a multi-line typed string, not a newline-banned param) and a typed **verdict output** the edge condition
is checked against. That is materially less than A1's full port/`map`/`ref<KIND>`/artifact-store
subsystem — no edge-dataflow binder, no artifact store, no JSON-in-DOT — but it is more than A2's
"un-defer two prompts + one param decl." The honest synthesis is a **middle design**: A2's deltas for
feedback and validation, plus a small typed node-input + verdict-output surface, minus A1's artifact and
edge-binding machinery. A2's own north-star (smallest thing that makes the graph un-leaky and per-role)
is not actually reached by A2, because F1/F3 leave it leaky and unable to carry the rubric.
