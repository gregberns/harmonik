# Phase 2 → Kerf Integration DRAFT

> **DRAFT** for user review. Produced per Phase 2 Step 7. Not committed as final. This draft proposes concrete kerf integration points for the Layer 1 foundation stack and Layer 6 safe swaps from `phase-2-findings.md`. User domain knowledge on kerf's internal structure is needed to turn this draft into a kerf work; do not act on it without user sign-off.
>
> Last updated: 2026-04-23

## 1. What this draft does

Takes the planning-protocols Phase 2 recommendations and maps them onto kerf's existing structure (jigs, passes, reviewer sub-agents, beads, session artifacts). Scope is deliberately narrow: only the high-confidence Layer 1 foundation + Layer 6 safe swaps from `phase-2-findings.md`. Experimental layers (counter-pattern stack, A/B candidates) are not integrated here — they need confirmation first.

What it does NOT do:
- Not a kerf spec. It is a set of proposals to turn into a kerf work if the user agrees.
- Not comprehensive. Many recommended protocols (Layer 2 task-shape openers, Layer 3 mid-session stack) need design work on how they'd live in kerf's pass structure. That design is out of this draft's scope.
- Not corpus-validated. Phase 2 Step 4.5 (corpus filter) was deferred; recommendations here are analytical.

## 2. Current kerf structure (as read from docs)

From `/Users/gb/github/harmonik/docs/components/internal/kerf.md`:
- **Jigs** (process templates): plan, spec, bug, implementation, spike, retrofit.
- **Passes** (per-jig stages): problem-space → decompose → research → design → spec-draft → integration → tasks.
- **Reviewer sub-agents at each major pass** (the current kerf-parallel-reviewer).
- **Artifacts per pass** with entry/exit criteria.
- **Session persistence** (SESSION.md; shelve/resume).
- **Finalization to git** as publish gate.

The recommendations below target the *reviewer sub-agent step* and the *pass-level artifact structure* primarily. Deeper changes to pass ordering are not proposed.

## 3. Proposed integrations (Layer 1 foundation stack)

### 3.1 `commanders-intent` as a mandatory opening artifact

**Where it lives in kerf:** pre-`problem-space` or as the first artifact of `problem-space`. One-paragraph Commander's Intent block with three slots: **Purpose**, **Key tasks**, **End state**.

**Current kerf analog:** problem-space pass produces a problem-statement artifact. The Commander's Intent slot is strictly additive — it's a 3-sentence structural prefix to the existing problem-space work, not a replacement.

**Square check:** finalize gate rejects works whose problem-space does not contain all three slots filled.

**User-decision points:**
- Whether Commander's Intent lives in problem-space or in a new pre-pass.
- Whether the three-slot form is load-bearing or whether "a statement of intent" (free form) is sufficient. Reviewer evidence strongly supports the structured three-slot form.

### 3.2 `autonomy-scope-grant` as a pass-opener clause

**Where it lives:** first turn of every kerf pass (spec-draft, design, integration, tasks). An explicit machine-readable block, not free-form: `decide-autonomously: [list]; ask-first: [list]`.

**Current kerf analog:** none. This is a net addition. The observed `upfront-decision-partition` pattern (from 79a42399 H#1) is the informal ancestor; the structured form is the refinement.

**Square check:** each pass's first turn must contain the autonomy block. Works without it fail the pass's square check.

**User-decision points:**
- Whether this should be a pass-level artifact or a session-level opener.
- Per-jig taxonomies: what counts as "trivial" differs between plan jig and bug jig. The user's intuition about these taxonomies is load-bearing.

### 3.3 `back-brief-plan-quality` at plan-draft exit

**Where it lives:** new square check at exit of the `spec-draft` pass. Before advancing to `integration`, the agent produces a Back-Brief artifact: a structured restatement of *its own plan against the Commander's Intent*, plus identified risks and "what I'd do differently if I were the user reading this."

**Current kerf analog:** kerf already has reviewer sub-agents at pass boundaries. Back-Brief is distinct from review because it's *the agent's self-assessment*, not an external critique. Both should coexist.

**Square check:** spec-draft pass does not exit without a Back-Brief artifact.

**User-decision points:**
- Whether Back-Brief is an artifact or a pass-internal step.
- The minimum content required to pass the square check (too loose → ritual-only; too tight → goodharting target).

### 3.4 `alternatives-considered-section` as a mandatory spec section

**Where it lives:** part of the spec template. Final spec artifact must include a named section listing alternatives considered and the rejection rationale for each.

**Current kerf analog:** no mandatory-section enforcement currently; specs-in-specs/ are unconstrained structurally.

**Square check:** `kerf finalize` rejects specs missing the alternatives section.

**User-decision points:**
- Whether to enforce section name ("Alternatives considered" vs "Rejected approaches") or section content.
- Minimum alternatives count (0 = hollow ritual; 2 = meaningful).

### 3.5 `role-split-reviewer-library` replacing generic kerf reviewer

**Where it lives:** replace the current kerf-parallel-reviewer with a library of named reviewer roles. Minimum set: devil's-advocate, maintainer, simplifier, pre-mortem. Each role has a canonical prompt that instructs the sub-agent to score the plan on its single frame.

**Current kerf analog:** kerf already dispatches parallel reviewer sub-agents at pass boundaries. The integration is a change to the reviewer catalog, not to the dispatch mechanism.

**Square check:** no new check. The library is used, not enforced.

**User-decision points:**
- Which reviewer roles to seed the library with (minimum set is a bet).
- Whether reviewer choice is per-jig, per-pass, or per-work.
- Pre-mortem reviewer in particular is a Phase 2 recommended addition; Framing C evidence is strong.

## 4. Proposed integrations (Layer 6 safe swaps)

### 4.1 `load-bearing-token-readback` as a mid-session agent discipline

**Not a kerf structural change** — a CLAUDE.md instruction. Add to the agent-instructions section: agent mid-pass turns should briefly restate the load-bearing domain tokens from the human turn (not prose; just the tokens) to surface vocabulary drift at turn 1.

**Why not structural:** this is a per-turn behavior, below kerf's artifact-level granularity. The agent's session-level instructions carry it.

**User-decision points:**
- Exact instruction wording. The aviation-derived form is "mirror the terms I just used, once, then proceed."
- Whether this conflicts with kerf's existing turn-shape discipline.

### 4.2 Replace implicit `autonomous-dispatch` with `question-preserving-autonomy`

**Where it lives in kerf:** implementation-jig and spike-jig (where autonomous execution is the default). Current kerf dispatches workers with a "work on this; don't ask" tone. The swap: workers maintain a visible questions queue in their bead's session artifact; questions accumulate and are surfaced at bead-exit, not suppressed.

**Current kerf analog:** SESSION.md already tracks per-agent state. Adding a `questions-deferred:` section to bead SESSION.md is a small structural change.

**Square check:** bead-exit checks that if `questions-deferred:` has entries, they are flagged for human review rather than silently closed.

**User-decision points:**
- Whether this applies to all worker beads or only specific jigs.
- How to handle large queues (if a worker accumulates 20 questions, the bead's complexity may be mis-scoped).

### 4.3 Replace `dialog-log-plan` with `single-text-procedure`

**Where it lives:** the `spec-draft` pass already produces a single spec document (single-text) rather than leaving the plan in chat (dialog-log). This swap is *already mostly done* by kerf's spec-first posture. The residual change is to make it explicit in the pass instructions that the agent may not "decide things in chat" without updating the spec artifact.

**Current kerf analog:** spec-draft pass; the swap is reinforcement of existing behavior, not a new mechanism.

**User-decision points:**
- Whether this belongs in CLAUDE.md, in the jig's pass-prompt, or both.

### 4.4 Replace bare `context-dump` with `dialogic-context-accretion`

**Where it lives:** `problem-space` pass. Currently nothing in kerf enforces a shape for how the agent and human build context together. The swap: instruct the agent to surface "context I wish I had" questions incrementally as the problem-space work reveals gaps, rather than asking for a single upfront brain-dump.

**Current kerf analog:** the problem-space pass prompt is the locus; this is a change to that prompt rather than a new mechanism.

**User-decision points:**
- Whether founding-vision sessions (where the user has long context to impart) should override this in favor of a context-dump shape.
- Whether the "context I wish I had" format is the right form or whether other phrasings work.

## 5. Integration scope summary

| Recommendation | Kerf change class | Breaking? | Square-check impact |
|---|---|---|---|
| Commander's Intent slot | new artifact structure in problem-space | Additive | New check at finalize |
| Autonomy-scope-grant block | new pass-opener artifact | Additive | New check per pass |
| Back-Brief artifact | new artifact at spec-draft exit | Additive | New check at spec-draft exit |
| Alternatives-considered section | new required section in final spec | Additive | New check at finalize |
| Role-split reviewer library | change to reviewer catalog | Additive | None |
| Load-bearing-token-readback | CLAUDE.md instruction | No structure | None |
| Question-preserving-autonomy | SESSION.md section change | Minor structural | Check on bead-exit |
| Single-text-procedure | pass-prompt reinforcement | None | None |
| Dialogic-context-accretion | pass-prompt change | None | None |

No integration proposed here breaks existing kerf works or introduces a new jig. Most are additive artifacts with corresponding square checks; a minority are prompt / instruction changes.

## 6. What's NOT proposed in this draft

- **Layer 2 task-shape-specific openers** — need per-jig design work (SCQA for new-subsystem, SBAR for bug, SPIN for tool-selection). Worth a follow-up kerf work.
- **Layer 3 mid-session stack beyond load-bearing-token-readback** — `sitrep-at-cadence`, `agent-surfaced-parking`, `directional-clean-repetition`, `summary-as-transition` all need locus-in-kerf decisions.
- **Layer 4 user-state adapters** — kerf doesn't currently track user state; this is a separate design question.
- **Layer 7 experiments** — A/B candidates are not kerf structural changes; they are experimental protocols to apply on specific works.
- **Counter-pattern stack** — the entire counter-commitment stack (`example-led-emergence` + `dialogic-context-accretion` + `micro-step-incrementalism` + `emergent-partition`) is an alternative protocol system that would need its own kerf jig or mode. Out of this draft.

## 7. Implementation sequencing (if user approves)

A natural ordering for landing these, by difficulty and payoff:

1. **First** (CLAUDE.md-only, near-zero cost): `load-bearing-token-readback`, `single-text-procedure` reinforcement.
2. **Second** (additive artifact + instruction, low cost): `alternatives-considered-section`, `autonomy-scope-grant` block.
3. **Third** (structural pass change, medium cost): `commanders-intent` slot in problem-space, `back-brief-plan-quality` at spec-draft exit.
4. **Fourth** (dispatch-layer change, medium cost): `question-preserving-autonomy` in bead SESSION.md.
5. **Fifth** (reviewer catalog): `role-split-reviewer-library` with initial role seeds.

Each stage is independently valuable; later stages don't require earlier ones to have shipped.

## 8. Open questions for user

1. Does kerf's pass structure naturally accommodate the above, or are there kerf-internal constraints I'm missing? (e.g., does the existing square-check infrastructure handle optional new artifacts gracefully?)
2. Should the Commander's Intent slot replace or supplement the existing problem-space work?
3. Role-split reviewer library: what roles would the user seed it with beyond the recommended four (devil's advocate, maintainer, simplifier, pre-mortem)?
4. Whether to stand this up as a single kerf work or multiple small works.
5. Whether the user wants any of the deferred Layers (2, 3, 4, 7, counter-pattern stack) turned into follow-up kerf works, and in what order.
6. Whether the Phase 2 findings suggest any kerf capability that doesn't currently exist (my read: user-state-awareness is the main gap, but it's out of this draft's scope).

## 9. What this draft is not

- Not a commitment. Everything proposed here is subject to user veto or rework.
- Not exhaustive. See §6 for what's excluded.
- Not corpus-validated. Step 4.5 deferred; this is analytical predictions.
- Not final. The phase-2-kerf-integration work needs user review before being turned into a kerf work, and the proposal may look different once the user has weighed in on the open questions in §8.
