# Adversarial Review — Brainstorm B (Model + Effort Control)

Target: `.kerf/works/2026-07-17-dot-mechanism/brainstorm/B-model-effort-control.md`
Reviewer stance: attack for where it breaks. Code-verified against `dot_cascade.go` and
`modelpreference.go` at HEAD.

---

## FATAL FLAWS

### F1 — The replay seal has a hole: the resolver B sketches never reads the seal, and per-node keying collides across iterations.

B's central correctness claim (§3.2, "the single most important correctness addition") is: seal
the fully-resolved concrete `(model, effort)` per node so replay is byte-identical even if the
catalog/labels/config changed. Three concrete holes:

1. **The proposed code path re-resolves; it does not consult a seal.** `resolveNodeModel(nodeAttr,
   effHarness, cat)` (§2.3) is a pure function of the *live* catalog `cat`. It is called at every
   agentic-node dispatch. Nothing in the sketch reads a sealed plan or short-circuits on replay.
   So the sketch and the determinism claim directly contradict each other: the actual dispatch
   path recomputes from whatever the catalog says *now*. For the seal to bind, dispatch must read
   the sealed concrete FIRST and skip `resolveNodeModel` on any replay/resume — an integration
   point B never specifies. Prior design (03-prior-design §1 "re-parse on restart, no serialized
   parse-tree trust") means resume RE-PARSES the graph and re-enters dispatch fresh; without an
   explicit seal-read branch, a resumed run re-resolves against the current catalog.

2. **"Per-node plan" keyed by `node_id` collides with the back-edge.** A REQUEST_CHANGES loop
   re-dispatches the SAME implementer node at iteration 2, 3 (`dot_cascade.go` phase selection,
   1288-1303). B alternately calls the seal a "per-node plan" (keyed by node_id) and a per-dispatch
   `model_selected` event (§3.2 says "extend its payload with node_id + resolution_tier"). If the
   seal is keyed by `node_id` only, iteration 2's re-resolve OVERWRITES iteration 1's concrete;
   replaying iteration 1 then binds iteration 2's model — not byte-identical. The seal MUST be keyed
   `(node_id, iteration)` and replayed by that composite key. B never states this, and the
   `escalation_ladder` sugar (§3.3) makes per-iteration divergence *intended*, which makes the
   collision easy to miss.

3. **The claim-time seal of OVERRIDE inputs is illusory.** `ResolveModelPreference` runs once at
   claim (`workloop.go:3274`) and — verified at `modelpreference.go:220-223` — returns the alias
   STRING (`strings.TrimPrefix(..., "model:")`), never a concrete. It CANNOT resolve `strong` to a
   concrete at claim, because a run may contain mixed-harness nodes and `strong.claude ≠ strong.pi`;
   the concrete only exists once you know the node's effective harness, which is a dispatch-time
   fact. So "seal `ForceModel`/label into `ModelPreference` at claim" (§3.2 bullet 1) seals only an
   opaque alias string with zero determinism value — the ENTIRE determinism guarantee rests on the
   per-dispatch concrete seal from hole #1/#2, which is exactly the part left underspecified.

**The race, concretely:** daemon restarts mid-run (or a run spans a `harmonik queue submit` of a
second bead) while the operator edits the hot-reloadable catalog. Nodes already completed replay
their old concrete from the event log; nodes re-dispatched after restart re-resolve the NEW
concrete. One logical run now mixes two catalog versions across its nodes. That is precisely the
non-determinism the seal was introduced to kill, and the hot-reload (§6.1, "unlike the
restart-required config today") is what opens the window that the old immutable-graph-text model
never had.

**Fix direction:** resolve alias→concrete exactly once per (node_id, iteration) at first dispatch,
write it to the Run record, and make BOTH the live back-edge re-entry AND the post-restart resume
read that record before ever calling `resolveNodeModel`. The catalog is an input to first
resolution only; after that the run is frozen. Say this explicitly and show the seal-read branch in
the code shape, or the guarantee is prose only.

### F2 — Silent degrade on an EXPLICIT override runs a too-weak model and wastes the run.

The degradation matrix (§6.2) applies "warn, don't fail" uniformly. For a DEFAULT (node attr,
config) that is correct and matches the no-external-version-binding rule. For an explicit OVERRIDE
it is actively harmful:

- Operator labels a hard bead `model:strong` (or `--force-model strong`). The catalog is missing
  `strong.pi` for the harness that runs. B degrades to "the run-level model" (§2.2) — which for a
  pi run is the DEFAULT weak model. The operator's entire intent was "not the default." The system
  silently runs the WEAK model, burns the full run on a task it was told is too hard for the weak
  model, and emits only a `model_alias_undefined` event nobody reads in real time. The operator
  then blames the model for failing, never learning the escalation silently no-op'd.
- This is *inconsistent with B's own good principle*: a namespaced-concrete on the wrong harness
  fails LOUD because it is author-intent that can't be honored (§2.2, §6.2 row 3). An explicit
  alias override that can't be honored is ALSO intent that can't be honored — but B degrades it
  silently. Intent-strength should govern uniformly: an explicit force/escalation that cannot
  resolve for the target harness should fail loud (or block the dispatch), NOT quietly weaken.

**Fix direction:** two-track degradation. Default-band aliases that miss the catalog degrade + warn
(drift-safe). Override-band aliases (force / escalation label) that miss the catalog for the target
harness FAIL the dispatch with a precise diagnostic — the operator asked for something specific and
must know it wasn't delivered.

---

## SERIOUS CONCERNS

### S1 — `model_locked` is the wrong default direction AND under-specified against `--force-model`.

B makes the node `model=` a SOFT default (escalation overrides it), opt-out via
`model_locked="true"`. The steelman for the flip is real (§ "gets right"), but the DEFAULT
direction is arguable and B picks the riskier one:

- A graph author who writes `model=sonnet` (or `fast`) on a cheap triage/format/implementer node
  has a legitimate COST-CONTROL pin. Under B, a bare `model:strong` label (role-scoped to
  implementer) silently escalates that exact node if it is implementer-class — and a cheap
  *implementer* node is a common shape. The failure mode is a SILENT cost overrun (a run that
  quietly spent Opus money), whereas under-escalation is LOUD (operator sees it didn't use Opus and
  re-labels). Defaulting to silent-escalate optimizes the loud case at the expense of the silent
  one.
- `model_locked` is per-node and boolean, so every cost-sensitive node needs its own
  `model_locked="true"` — exactly the per-node repetition B elsewhere invokes the stylesheet to
  eliminate. There is no graph-level "these nodes are pinned" and no "locked against escalation but
  still honor an operator force."
- **Unspecified interaction:** does tier-0 `--force-model` override a `model_locked` node? Force is
  "above everything" (§1.1) but lock makes "escalation skip it" (§1.2) — is a force an escalation?
  If force respects the lock, the operator cannot force (surprising for a tier-0 override). If
  force ignores the lock, the lock is not a lock. Either answer needs to be stated; the ambiguity
  is a real gap in a precedence spec whose whole job is to be unambiguous.

### S2 — Role-scoped label semantics silently change existing `model:` behavior and reference an unvalidated open set.

- **Silent semantic change to a landed feature.** Today (verified `modelpreference.go:212-223`) a
  bare `model:opus` label sets the RUN-LEVEL model — every node without its own `model=` gets it,
  including reviewer nodes. Under B, bare `model:opus` = implementer-CLASS only (§1.2). Existing
  beads carrying `model:opus` that expect run-wide effect will, post-change, escalate only the
  implementer and leave reviewers on the default. B's §7 cost list flags grepping `.dot` files but
  NOT the change to existing bead-LABEL scope. That is a behavior regression for deployed labels.
- **`@role` targets an OPEN set with no validation.** `agent_type` is an open set (02-specs WG-003;
  unknown values surface only as structural failure at dispatch). So `@implementer` is a magic
  string matched against an unvalidated taxonomy: `@impl` (typo) or `@auditor` (a graph whose
  reviewer node uses a custom `agent_type`) silently matches nothing and no-ops. Another silent
  operator-intent failure, and there is no schema surface to catch it because the taxonomy it
  references is deliberately open.
- **Coarse targeting / bead-vs-node question.** `@role` cannot target one of two implementer nodes
  in a fan-out graph (`@implementer` hits both; there is no `@#id` for labels). And a bead is
  cross-workflow — `model:opus@reviewer` is meaningless in a graph with no reviewer node. Coupling
  a bead label to a specific graph's role structure is a layering smell; at minimum the "unmatched
  role → silent no-op" needs to become "unmatched role → surfaced warning event."

### S3 — Hot-reload catalog parse-failure is a fleet-wide footgun.

§6.1 makes the catalog mtime-invalidated and hot-reloadable. B never specifies what happens when the
reload reads a half-written or malformed catalog (an operator saving a `config.yaml` mid-edit).
Row "Alias catalog absent entirely" (§6.2) falls back to bare-literal — so a parse failure that
collapses to "no catalog" would make EVERY alias across EVERY running node degrade at once. A single
fat-fingered catalog edit silently downgrades the whole fleet's escalations. Hot-reload needs:
keep-last-good on parse failure (never adopt a broken catalog), and this interacts with F1 — a
reload must never retroactively change an already-sealed run.

---

## SCOPE CREEP — CUT

### C1 — `model_stylesheet` + `class` (§4) does not belong in this work.

- The operator's stated wall is exactly one thing: "escalate a hard bead past the node default."
  That is §1–§3. The stylesheet solves a DIFFERENT problem — author-side repetition in multi-node
  graphs "this project is heading toward" (§4.3). "Heading toward" is the textbook maximal-design
  tell; it adds a new mini-parser (selector grammar, `#id > .class > agent_type > *` specificity,
  a new `class` node attr, selector-vs-graph validation) for zero contribution to the operator's
  actual pain.
- It resurrects a DELIBERATELY DEFERRED item: WG-043 already parked `model_stylesheet`/`class` as
  INFORMATIVE-only at v1, "a clean future amendment," deferred to **hk-1xzg3** (02-specs §3,
  03-prior-design §2). Bundling a deferred sugar into an escalation-bug fix violates
  "don't add abstraction the user hasn't asked for."
- The `sonnet-triple-review` repetition it cites is real but trivial (repeat `model=` on 6 nodes,
  or a per-agent-type config default). A stylesheet engine is disproportionate.
- **Recommend: CUT from this work; re-file against hk-1xzg3.** If per-role effort is wanted now
  (§5), get it from an `effort:<lvl>@role` label + `--force-effort` + per-node `effort=` — none of
  which needs the stylesheet. §5's dependency on the stylesheet is the tail wagging the dog.

### C2 — `escalation_ladder` (§3.3) — CUT / defer.

Another speculative feature ("the operator will likely want"). It compounds F1 (per-iteration
divergence must be sealed by `(node_id, iteration)`; the ladder makes that divergence intentional
and thus easier to get wrong). B already marks it optional — make that explicit: not in this work.

---

## WHAT IT GETS RIGHT (steelman)

- **The core diagnosis is correct and precisely located — verified in code.**
  `nodeModelForHarness` (`dot_cascade.go:1182-1187`) returns `node.Model` for the claude family, and
  line 1351 layers that over `resolvedModel`, which `ResolveModelPreference` already folded the
  claim-time bead label into (`modelpreference.go:220-223`). So a `.dot` `model=sonnet` literally
  overwrites a per-bead `model:opus`. The operator's wall is real, and B pins it to the exact line.
  Flipping so task-specific escalation beats stage-typical default is the right instinct — task
  specificity SHOULD beat file position.
- **The cross-harness bug is real and the fix shape is right.** `nodeModelForHarness` DISCARDS the
  pin for pi/codex (the `effHarness == core.AgentTypeClaudeCode` guard at line 1183): per-node model
  is silently ignored for two whole harness families. Harness-portable aliases resolved per-harness
  through a catalog is the correct remedy, and the plumbing observation is accurate — pi and codex
  already consume `rc.model`, so the change is genuinely small (delete the claude-only guard, feed
  the resolved value into `rc.model` for all paths).
- **The author-error-vs-version-drift split is a good principle.** Namespaced-concrete-on-wrong-
  harness fails loud (author mistake); alias drift degrades (version drift). B just fails to apply
  it to the override case (F2) — but the principle itself is sound and worth keeping.
- **B correctly identifies that aliases + a mutable catalog + labels BREAK the old replay
  assumption.** It gets the PROBLEM right (§3.2 opening) even though the proposed seal has the holes
  in F1. Raising sealing as load-bearing is the right call; it just needs to be finished.

---

## VERDICT (per component)

| Component | Verdict | Why |
|---|---|---|
| §1 ladder FLIP (node attr below override band) | **ADOPT** the direction | Correct, code-verified fix to the operator's exact wall |
| §1.2 node = SOFT default + `model_locked` | **REVISE** | Default direction arguable (silent cost overrun); lock is per-node repetition; lock-vs-force interaction unspecified (S1) |
| §2 cross-harness aliases + per-harness resolve | **ADOPT** | This is the real bug; small, correct plumbing fix |
| §3.1 `--force-model` / `--force-effort` flags | **ADOPT** | The missing per-run override surface |
| §3.2 replay seal | **REVISE (blocking)** | Key by (node_id, iteration); dispatch + resume MUST read the seal before re-resolving; define catalog-parse-fail; claim-time alias seal is illusory (F1) |
| §3.3 `escalation_ladder` | **CUT / defer** | Speculative; compounds the seal hole (C2) |
| §6 degradation matrix | **REVISE** | Split override (fail loud) vs default (degrade); keep-last-good on catalog parse failure (F2, S3) |
| §1.2 role-scoped labels `@role` | **REVISE** | Silent scope change to existing `model:`; unvalidated open-set target; unmatched→silent no-op (S2) |
| §4 `model_stylesheet` + `class` | **CUT** | Deferred (hk-1xzg3); scope creep; solves a non-stated problem (C1) |
| §5 effort per-role | **ADOPT the label/flag path, DROP the stylesheet dependency** | `effort:<lvl>@role` + `--force-effort` + node `effort=` suffice |

**Bottom line:** the two genuine bug fixes (ladder flip §1–§2, per-run override §3.1) are correct,
small, and answer the operator — adopt them. But the replay seal that everything else leans on is
prose, not mechanism (F1), and the uniform silent-degrade turns an explicit escalation into a
silently-weakened wasted run (F2). Fix those two before landing. Cut the stylesheet and the
escalation ladder — they are deferred/speculative surface that dilutes a sharp fix.
