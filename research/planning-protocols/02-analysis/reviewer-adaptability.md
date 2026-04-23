# Reviewer: Adaptability-to-Task-Type Lens

**Phase 2, Step 5 reviewer artifact.** Author: adaptability-reviewer sub-agent, 2026-04-23. Track: planning-protocols.

This is one of the Step 5 reviewer sub-agent outputs. Frame: **adaptability across planning-task shapes**. Other Step 5 reviewers hold different frames (ergonomics, counter-pattern steel-manning, failure-mode coverage). This document does not rank on peak performance at any single shape; it ranks on *breadth of acceptable performance* across the task-shape landscape.

## 1. Frame definition

**Adaptability**, as used here, is the property of a protocol that determines how well it works across the range of planning-task shapes the user actually encounters. A protocol that is excellent for new-subsystem design but useless for bug investigation has *narrow* adaptability. A protocol that works acceptably (not necessarily best-in-class) across all eight identified shapes has *wide* adaptability. Adaptability is not the same as quality: a narrow protocol can be higher-quality at its target shape than any wide protocol. The two lenses are orthogonal, and both matter.

Adaptability is further subdivided into two sub-properties in this review:

- **Cross-shape breadth** — does the protocol work on ≥ 5 of the 8 shapes at Good-or-better?
- **Mid-session flex** — does the protocol continue to function as the task shape shifts mid-session? (Phase 1 empirical-design sub-analysis noted that sessions starting as scope-decomposition often become refactor-planning or subsystem-design; a mode-locked protocol that was correctly chosen at open can degrade mid-session.)

A protocol can be wide-breadth and mode-locked (works everywhere, but once you start with it you're committed), or narrow-breadth and flex-capable (only works for one shape but adapts within it). Both are distinguished in the ranking below.

## 2. Task-shape definitions (evidence-anchored)

Shapes are drawn from `evaluation-criteria-refinement.sub-empirical-design.md` §9.1 (the eight canonical planning-problem shapes). Corpus evidence is thin for most shapes (Phase 1 corpus was biased toward new-subsystem design and spec refinement — see counterfactual §6 below).

| # | Shape | One-line | Corpus signal | Dominant cognitive mode |
|---|-------|----------|---------------|-------------------------|
| 1 | **New-subsystem design** | Open-ended architectural decisions from near-blank-slate | Dominant (13493c8d, 79a42399, secure-dev founding) | Divergent → convergent on unknown decision space |
| 2 | **Refactor-across-modules** | Constrained by existing code; interface/contract design under constraint | Secondary (scattered) | Constraint-anchored exploration |
| 3 | **Bug investigation** | Diagnostic, hypothesis-refinement, iterative evidence-gathering | Minimal in planning corpus (mostly non-planning) | Abductive narrowing |
| 4 | **Spec refinement** | Sharpening existing draft, catching ambiguity | Dominant (kerf passes) | Local critique |
| 5 | **Scope decomposition / roadmapping** | Milestone ordering, dependency analysis, MVP identification | Secondary | Tree/graph decomposition |
| 6 | **Integration planning** | Coupling surface identification between existing systems | Minimal | Interface/surface enumeration |
| 7 | **Tool-selection / adoption** | External-artifact evaluation and fit analysis | Minimal (beads-evaluation session, partially) | Comparative evaluation |
| 8 | **Research scoping** | Investigable-question framing before the research happens | Rare but observable (this track is itself shape 8) | Question-quality evaluation |

Shapes differ sharply on four dimensions that matter for protocol fit:
- **Outcome shape** (single-artifact vs. tree-decomposition vs. evidence-catalog)
- **Initial-uncertainty distribution** (known-unknowns vs. unknown-unknowns)
- **Whether the answer is already latent in the user's head** (spec refinement: often yes; bug investigation: no)
- **Convergence vs. divergence phase dominance** (new-subsystem: both; spec refinement: convergence-only; research scoping: divergence-only)

## 3. Shape × Protocol fit matrix

Top 30 protocols selected from the 87-entry catalog on cross-origin coverage (observed + external + counter-pattern represented) and plausible adaptability range (excluding protocols that are stance-modifiers rather than protocols per se, e.g., `gordon-roadblock-filter`, `non-directive-stance`, which compose rather than stand alone).

Legend: **E** = Excellent, **G** = Good, **N** = Neutral, **B** = Bad, **—** = Not applicable.

| Protocol | New-subsys | Refactor | Bug | Spec-refine | Scope-decomp | Integration | Tool-select | Research-scope |
|---|---|---|---|---|---|---|---|---|
| `commanders-intent` | E | G | N | G | G | G | G | G |
| `sbar-opener` | G | E | G | G | N | G | G | N |
| `ipass-opener` | N | G | E | G | N | G | N | N |
| `scqa-opener` | G | G | N | E | G | N | G | G |
| `engagement-letter-opener` | E | G | N | G | E | G | E | G |
| `context-dump` | E | N | B | N | N | N | G | N |
| `recovery-handoff` | G | G | G | G | G | G | G | G |
| `autonomous-dispatch` | N | G | N | E | B | N | B | B |
| `mett-tc-sweep` | G | E | G | N | G | E | G | G |
| `upfront-decision-partition` | G | E | N | G | G | G | N | N |
| `emergent-partition` | E | G | G | N | G | G | G | E |
| `pre-action-plan-disclosure` | G | G | G | G | G | G | G | G |
| `back-brief-plan-quality` | E | G | G | G | G | G | G | G |
| `read-back-comprehension` | G | G | G | E | G | G | G | G |
| `five-whys-bounded` | N | G | E | G | N | G | N | G |
| `spin-sequence` | G | G | E | G | G | E | E | E |
| `elenctic-probe` | G | G | G | E | N | G | N | G |
| `maieutic-drawout` | G | B | G | B | G | N | N | E |
| `reduced-dialectic` | E | G | G | E | G | G | E | G |
| `issue-tree-diagnostic` | G | G | E | N | E | G | N | E |
| `issue-tree-solution` | G | E | N | N | E | G | G | G |
| `mece-decomposition` | G | G | N | G | E | G | G | G |
| `alternatives-considered-section` | E | E | N | E | G | G | E | G |
| `kerf-parallel-reviewer` | G | G | G | G | G | G | G | G |
| `role-split-reviewer-library` | E | G | G | E | G | E | G | G |
| `premortem-reviewer` | E | E | G | E | G | E | G | N |
| `micro-step-incrementalism` | N | E | G | N | B | G | N | B |
| `example-led-emergence` | E | G | E | G | N | G | B | G |
| `assumption-bundle` | E | G | G | G | G | E | G | G |
| `hypothesis-driven-ghost-deck` | G | G | E | G | G | G | E | E |

### Matrix reading notes

- Entries are the reviewer's forward prediction of fit, not corpus-confirmed scores. The catalog's per-protocol "predicted trade-offs" field was the primary input; cross-checked against task-shape cognitive-mode descriptions (§2).
- An **E** means: if you only had this one protocol and this one shape, you would be doing well. A **G** means: acceptable, no major pathology. An **N** means: applies but adds no unique value. A **B** means: the protocol's mechanism actively conflicts with the shape's cognitive mode.
- Rows dense with **G/E** are wide-adaptable; rows with **B** entries are mode-locked (good at some shapes, actively-bad at others).

## 4. Top 10 most-widely-adaptable

Ranked by count of **E/G** entries in the matrix (with ties broken by mid-session flex). These are protocols the user can deploy without knowing in advance which shape the session will turn into.

1. **`pre-action-plan-disclosure`** (8/8). Pre-disclosure then corrected plan works on every shape; corrections have shape-shaped content but the disclosure mechanism is shape-neutral. Mid-session flex: high (any new plan-fragment triggers it).
2. **`recovery-handoff`** (8/8). Structured resumption carries state regardless of what kind of work the resumption is. Mid-session flex: n/a (opener-only).
3. **`back-brief-plan-quality`** (8/8). Forced re-encoding against intent works across any shape that *has* an intent. Mid-session flex: high (re-run per sub-ask).
4. **`kerf-parallel-reviewer`** (8/8). Parallel critique is shape-neutral if role prompts are adapted; prompts are cheap. Mid-session flex: high (re-dispatch per plan-revision).
5. **`commanders-intent`** (7/8, one N). Intent-first structure works whenever there *is* a coherent principal ask; weakest on bug investigation where the intent is "find out" rather than "build".
6. **`reduced-dialectic`** (8/8, all G-or-better). Position/counter/synthesis is a generic pattern; the content of the counter adapts to the shape.
7. **`assumption-bundle`** (7 G or E, 1 G). The dependency-graph structure carries across shapes because every shape has interacting assumptions.
8. **`emergent-partition`** (7 G or E, 1 N). Decision-classification emerges, so the category set adapts to whatever decisions actually appear. Mid-session flex: high by construction.
9. **`alternatives-considered-section`** (6 E, 2 G-or-N). Mandatory alternatives is shape-generic but weakest on bug investigation where alternatives may not be semantically meaningful.
10. **`role-split-reviewer-library`** (6 E, 2 G). Distinct reviewer roles can be paired to the shape; the meta-structure is shape-invariant.

### Why these dominate

Three properties recur: (a) the protocol operates at the meta-level (on *any* plan artifact), (b) it triggers on a re-usable linguistic shape (a disclosure, a restatement, a critique), (c) its per-shape content is filled in by the other party. Protocols with this "form supplies structure, content is domain-free" property are inherently wide-adaptable.

## 5. Top 10 most-shape-specific (high at one shape, poor elsewhere)

These are high-peak protocols that should be deployed *only* when the task shape matches. Mis-deployment is actively harmful.

1. **`context-dump`** — Peak: new-subsystem founding. Poor: bug investigation (no structured dialogic-refinement loop), spec refinement (bloats minor revisions), scope decomposition (dump format can't express dependencies).
2. **`autonomous-dispatch`** — Peak: spec refinement where a good spec already exists. Poor: research scoping (no question to pre-answer), tool selection (requires comparative evaluation the dispatch can't carry), scope decomposition (no running spec to advance).
3. **`maieutic-drawout`** — Peak: research scoping (drawing out latent questions). Poor: refactor (the agent has *more* context than the human), spec refinement (content-filled already; draw-out adds nothing).
4. **`micro-step-incrementalism`** — Peak: refactor-across-modules (step-sized well to file/function diffs). Poor: research scoping (no action steps), scope decomposition (decomposition is upstream of steps), new-subsystem (loses cross-step architectural coherence).
5. **`five-whys-bounded`** — Peak: bug investigation (causal-antecedent is the task). Poor: new-subsystem (no past-causes to trace), tool selection (comparison, not root-cause).
6. **`example-led-emergence`** — Peak: new-subsystem and bug investigation (examples concretize fuzzy space). Poor: tool selection (artifact comparison is not example-led), scope decomposition (globally-coherent ordering resists example-by-example).
7. **`issue-tree-diagnostic`** — Peak: bug investigation and scope decomposition. Poor: spec refinement (tree overkill for local critique).
8. **`ipass-opener`** — Peak: bug investigation (severity + action-list + contingencies fit diagnostic work). Poor: new-subsystem (no illness-tag analog), research scoping (no actions-as-planned to hand off).
9. **`sbar-opener`** — Peak: refactor-across-modules (fits interface-constraint framing cleanly). Poor: research scoping (Assessment/Recommendation slots presume known answer).
10. **`spin-sequence`** — High across many shapes, but sales-pitch origin vocabulary limits acceptance, and *Problem* slot forces framing-as-deficit inappropriate for research scoping. Included here as shape-specific because fit varies sharply by whether the task *has a problem*.

**Note on SPIN:** Marginally adaptable; could plausibly appear on the wide-adaptable list instead if the Need-payoff framing is relaxed. Ranked here because the four-slot sequence does have task-shape contingency.

## 6. Per-shape recommendations

For each shape, 2–3 best-fit protocols from the catalog. "Gap" flag where the catalog does not contain a strong fit.

### Shape 1 — New-subsystem design
- **Primary:** `commanders-intent` (establishes the purpose/end-state that architectural decisions must serve).
- **Secondary:** `reduced-dialectic` or `alternatives-considered-section` (forces the option space to be enumerated, since new-subsystem is where first-solution anchoring is most dangerous).
- **Adaptive:** `example-led-emergence` in early exploratory phase, then shift to `pre-action-plan-disclosure` once framing stabilizes.

### Shape 2 — Refactor-across-modules
- **Primary:** `sbar-opener` or `mett-tc-sweep` (structured inventory of existing-code constraint).
- **Secondary:** `micro-step-incrementalism` (this is the shape it was designed for).
- **Tertiary:** `upfront-decision-partition` (interface-vs-implementation split is the natural autonomy boundary).

### Shape 3 — Bug investigation
- **Primary:** `five-whys-bounded` or `issue-tree-diagnostic` (diagnosis is the task).
- **Secondary:** `ipass-opener` (severity/contingencies are the right shape).
- **Tertiary:** `hypothesis-driven-ghost-deck` (bug hypotheses are naturally falsifiable).

### Shape 4 — Spec refinement
- **Primary:** `elenctic-probe` (consequence-probing is the load-bearing move when text exists).
- **Secondary:** `alternatives-considered-section` enforced as a review gate, or `read-back-comprehension` to catch drift between draft and restatement.
- **Note:** `autonomous-dispatch` is the user's empirically preferred shape for some spec-refinement flavors, but it degrades when the spec has latent ambiguity.

### Shape 5 — Scope decomposition / roadmapping
- **Primary:** `mece-decomposition` or `issue-tree-solution` (decomposition is the task).
- **Secondary:** `engagement-letter-opener` (the out-of-scope slot is the most useful move for decomposition work).
- **Gap signal:** No protocol in the catalog is designed specifically for *dependency-aware* decomposition (DAG / prerequisite ordering). Closest is `assumption-bundle`'s dependency-graph shape, but that's for assumptions, not tasks. **This is a genuine catalog gap** — Step 6 should flag.

### Shape 6 — Integration planning
- **Primary:** `mett-tc-sweep` (the Opposing-forces + Terrain slots cover coupling surface).
- **Secondary:** `assumption-bundle` (integration is typified by assumption-coupling across system boundaries).
- **Tertiary:** `premortem-reviewer` (integration failures are highly imagined-failure-tractable).

### Shape 7 — Tool-selection / adoption
- **Primary:** `engagement-letter-opener` (acceptance criteria per deliverable = fit criteria per candidate).
- **Secondary:** `reduced-dialectic` (position/counter is the natural shape for pro-vs-con evaluation).
- **Tertiary:** `spin-sequence` (Implication questions surface downstream cost of adoption mistakes).
- **Note:** Tool-selection is the shape where the catalog's *consulting* origin-cluster is most directly applicable; military and medical protocols fit poorly.

### Shape 8 — Research scoping
- **Primary:** `maieutic-drawout` (the task *is* drawing out questions that aren't yet articulated).
- **Secondary:** `hypothesis-driven-ghost-deck` (stake-in-ground on provisional answer to focus investigation).
- **Tertiary:** `emergent-partition` or `issue-tree-diagnostic` (emergent structure is natural for research questions).
- **Note:** This track (planning-protocols itself) is shape 8, and the user has already been using a de-facto `hypothesis-driven-ghost-deck` stance.

## 7. Counterfactual: if corpus distribution were different

Phase 1 corpus is biased toward shapes 1 (new-subsystem) and 4 (spec refinement). Protocols that survived Phase 1's observed-pattern lens are likely optimized for those shapes. This counterfactual asks: **if the user's task mix were redistributed, would the rankings shift?**

### Counterfactual A: user spends 50% of time on bug investigation (shape 3)

**Rising:** `five-whys-bounded`, `ipass-opener`, `hypothesis-driven-ghost-deck`, `issue-tree-diagnostic` all move up. **Falling:** `context-dump`, `autonomous-dispatch`, `commanders-intent` (intent is given, not derived) all lose prominence.

**Key change:** `example-led-emergence` (counter-pattern #1) would move into the top-5 wide-adaptable, because bugs are naturally example-led (the bug report IS the first example). The catalog currently underweights this because the observed corpus is thin on bug work.

### Counterfactual B: user spends 40% of time on tool selection and integration

**Rising:** `engagement-letter-opener`, `spin-sequence`, `premortem-reviewer`, `mett-tc-sweep`. The consulting-origin cluster is load-bearing; the military and medical clusters are secondary.

**Falling:** `micro-step-incrementalism` (no micro-steps until after tool is chosen), `autonomous-dispatch` (not dispatchable; requires comparative judgment).

**Key change:** `reduced-dialectic` emerges as even stronger because tool selection IS position/counter/synthesis on each candidate. The current catalog treats `reduced-dialectic` as one option among many; in a tool-selection-heavy corpus it would be the dominant recommendation.

### Counterfactual C: user spends 30% of time on scope decomposition and research scoping

**Rising:** `issue-tree-diagnostic`, `issue-tree-solution`, `mece-decomposition`, `maieutic-drawout`, `emergent-partition`. The tree-structured and emergent-structure protocols cluster here.

**Falling:** All the comprehension-check protocols (`read-back`, `back-brief`, `teach-back`) which assume a coherent plan exists to be checked. In decomposition and scoping, the plan IS being built, so there is nothing to read back *to*.

**Key change:** The catalog has a gap for *dependency-aware scope decomposition* (noted in §6 shape 5) that a different corpus distribution would make far more visible. The absence of this protocol in the catalog is a Phase 1 observational-bias artifact.

### Summary of counterfactual analysis

The top-10 wide-adaptable list in §4 is *moderately* robust to distribution shifts — about 6 of 10 would stay on the list under any reasonable redistribution. But the top-10 shape-specific list in §5 is highly sensitive: shape-specificity changes which shapes dominate, so the "peak shape" for each protocol is stable but the shape-list of high-value deployments is not. **Step 6 should not present task-shape recommendations as context-free; the observed-distribution is thin evidence.**

## 8. Seed for Step 6: task-type-aware recommendation map

The Step 6 deliverable (ranked recommendations) should take the shape of a *map* — given a session's detected or declared task shape, retrieve a recommended opener + mid-session protocol + review protocol. This reviewer's contribution to that map is the shape × protocol fit matrix (§3) plus the per-shape recommendations (§6) as seed rows.

Suggested map structure for Step 6:

```
if task-shape detected:
  opener ← per-shape-row-primary
  mid-session ← wide-adaptable-top-5 ∩ mid-session-flex-high
  review ← premortem-reviewer OR role-split-reviewer-library (shape-adapted prompts)
  fallback (shape-misidentified at open) ← wide-adaptable-top-5
else:
  use wide-adaptable-only bundle
```

Two specific Step 6 asks this reviewer would put forward:

1. **Declare mid-session shape-shift as a first-class concern.** The Step 6 map should not assume shape is stable across a session. At least one recommendation slot should be for a shape-shift-detection protocol (closest candidate: `summary-as-transition` applied with explicit "did shape change?" probe). No current protocol handles this well.
2. **Flag the catalog gaps.** Shape 5 (scope decomposition / roadmapping) has no dependency-aware primary option; shape 8 (research scoping) has no protocol designed specifically for *question-quality* evaluation (distinct from hypothesis quality). Both gaps should be carried into any follow-on Phase (or into the research-statement §5 unexplored-region catalog).

## 9. Discipline notes (for Step 6 reviewer synthesis)

- **Observed corpus is biased.** The user's own pattern of use over-represents shapes 1 and 4. Top-ranked wide-adaptable protocols derived from observed convergences (e.g., `pre-action-plan-disclosure`) should be cross-checked against the counterfactuals in §7.
- **"Best at its shape" ≠ "should be recommended."** A protocol that's best-in-class for shape 3 but wrecks shape 1 is a *shape-specific* recommendation, not a default.
- **This reviewer did not run the Step 4.5 corpus-signal filter** (it was not executed this session). The matrix reflects mechanistic predictions + task-shape reasoning, not corpus-signal-filtered evidence. Step 6 must treat matrix entries as *hypotheses*, not measurements.
- **A final-recommendation identical to observed patterns is ruled out under research-statement §7 discipline.** Where the wide-adaptable top-10 overlaps with observed patterns, the recommendation must cite a considered-and-rejected counter-pattern. `pre-action-plan-disclosure` should be weighed against `example-led-emergence`; `context-dump` against `dialogic-context-accretion`; `autonomous-dispatch` against `question-preserving-autonomy`.
