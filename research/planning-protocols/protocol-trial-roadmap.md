# Planning-Protocol Trial Roadmap

> Ordered list of what to investigate after the initial `/session-handoff` + `/session-resume` skill trial. Captures parking-lot items from the 2026-04-24 → 2026-04-27 walkthrough of `phase-2-findings.md`.
>
> Last updated: 2026-04-27.

## Active now

**Trial of `/session-handoff` + `/session-resume` skills.** User-level skills at `~/.claude/skills/session-{handoff,resume}/SKILL.md`. Together they embed five Layer 1/5/6 protocols:
- `commanders-intent` (Purpose / Key Tasks / End State, in handoff)
- `autonomy-scope-grant` (decide-vs-ask block, in handoff)
- `back-brief-plan-quality` (paraphrase + plan + uncertainty, in resume)
- `load-bearing-token-readback` (verbatim mirror, in resume)
- `closing-summary-ritual` + `out-of-scope-section` (in handoff)

Trial flag: `<!-- PP-TRIAL:v1 -->` at top of every produced `HANDOFF.md`. Greppable.

Path convention: skills accept an optional path argument (default `./HANDOFF.md`). For multi-track repos, pair handoff and resume on the same explicit path — e.g., `/session-handoff research/planning-protocols/HANDOFF.md` paired with `/session-resume research/planning-protocols/HANDOFF.md`. The root `./HANDOFF.md` is reserved for the harmonik-main session; this track uses an explicit path.

User-stated trial goals: time-to-alignment, assumption-surfacing. Adopt-and-notice testing model — no quantitative harness; look for signal across a few real working sessions.

---

## Trial calibration items to watch

Surfaced when the skills were authored (2026-04-27). The active trial is the natural way to test these.

- **Severity-tag boundary between `watch` and `escalate`** is murky. If `escalate` becomes either always-empty or always-overused across the first few real handoffs, the rule needs tightening.
- **Autonomy-scope is categorical, not boundary-aware.** Real cases like "rename a function called from 15 modules" may fall in either bucket; the resume agent has to flag the boundary case explicitly. If trial sessions consistently silence boundary cases, the autonomy-scope template needs structural change.
- **Handoff write cost.** Reviewer estimated 15-20 minutes for first handoff, 8 minutes once familiar. If real-use feels heavier than that, the structure is too elaborate.
- **Worked-example fix in resume's paraphrase rule** (added 2026-04-27 reviewer pass) — does it actually produce consistent paraphrasing across sessions, or do further iterations of the rule emerge?
- **Handoff location.** Whether `./HANDOFF.md` (or per-path argument) is the right convention, or whether multi-project work wants `~/.claude/handoffs/<project>-<timestamp>.md` for retention and per-project separation.

Concrete questions to keep in mind during each trial session:

- Did the resume's back-brief surface a jargon misalignment in real time, or did the user nod through it?
- Did the load-bearing-token readback mirror back anything that looked subtly off?
- Was the auto-selected "one uncertainty" the most useful thing to surface, or did a more important one go unstated?
- Did producing the closing handoff feel natural or forced? Where did the structure resist the actual session shape?
- Which of the 5 embedded protocols pulled visible weight; which felt like ceremony?

After 3-5 sessions, take stock — adopt or shed. Decision feeds whether to layer in `autonomy-scope-grant`-as-standalone, `alternatives-considered-section`, `role-split-reviewer-library`, or any mid-session disciplines.

---

## Next, layered in if the trial shows signal (low cost)

### 1. Complete the Layer 1 foundation stack

Layer 1 was ranked as a 5-protocol composing stack. The active trial covers 2 of 5. Remaining:

- **`autonomy-scope-grant` as a standalone discipline** (not just a section in HANDOFF.md). Even when not resuming from handoff, every fresh planning session should open with an explicit "decide X / ask about Y" block. Could be a CLAUDE.md addition or a small standalone skill (`/session-open` or extension to `/session-resume` to handle the no-handoff path).
- **`alternatives-considered-section` as a spec-template discipline.** Mandatory section in any spec produced. Records rejected approaches and why. Currently absent from observed corpus; convergent winner across reviewer frames. Lowest-cost addition possible (zero session-time impact) but only fires when actually writing a spec.
- **`role-split-reviewer-library` as a separate skill.** Named reviewer roles (devil's advocate / maintainer / simplifier / pre-mortem) instead of generic "review this." Collides with the existing `/review` Claude Code skill name; will need a different name (`/plan-review`, `/multi-review`, `/critique`). Medium cost — needs role prompts written carefully so the four perspectives produce orthogonal critique, not redundant.

**Order of layering:** `autonomy-scope-grant` first (cheapest, immediate reuse of existing skill machinery), then `alternatives-considered-section` (zero session-time cost), then `role-split-reviewer-library` (needs design work).

### 2. Mid-session disciplines via CLAUDE.md

Currently the per-turn protocols are not active — only session-start and session-end. The two highest-value mid-session disciplines:

- **`load-bearing-token-readback` per-turn.** Agent mirrors domain tokens from each user turn before proceeding, not just at session resume. Catches drift continuously, not just at handoff boundaries.
- **`back-brief-plan-quality` at plan-draft moments.** Before agent commits to writing a spec or starting major work mid-session, it produces a back-brief — paraphrase of the active intent, plan for the next move, one uncertainty. Distinct from the resume-time back-brief because it fires whenever the work shifts phase.

Both belong in CLAUDE.md (project-level or user-level), which makes them apply silently every session. **Heavier intervention than skills** because (a) they affect every session whether you want them or not, (b) they're harder to attribute signal to since they're always-on. Recommended only after the skill trial shows that these protocols do useful work.

### 3. Augment observed `recovery-handoff` further

The current `/session-handoff` already adds `closed-acknowledgment` (implicit in resume's back-brief) and `severity-tagging` (routine/watch/escalate on parked items) per the research's "augment, don't displace" verdict. Possible further augmentation:
- **Watcher-tier auto-elevation** — items tagged `watch` on multiple consecutive handoffs auto-promote to `escalate`. Requires handoff history awareness; currently each handoff overwrites.
- **Handoff history retention** — retain prior handoffs in `~/.claude/handoffs/<project>/<timestamp>.md` for retrospective. Different file location than the current `./HANDOFF.md`. Trade-off between simplicity and audit value.

---

## Validation work (parked; not active)

### 4. Step 4.5 corpus-signal filter harness

**Plan on disk** at [`plans/step-4.5-plan.md`](plans/step-4.5-plan.md). Two reviews on disk: [`plans/step-4.5-plan.review-1-coherence.md`](plans/step-4.5-plan.review-1-coherence.md) and [`plans/step-4.5-plan.review-2-risk.md`](plans/step-4.5-plan.review-2-risk.md).

Top issues to address before authorizing implementation:
- **False-positive inflation in correction-incident detection** (C1/M1 classifier) — load-bearing for Step 5 ranking; FP guards may not generalize from 10 hand-labeled sessions to all 195. Coherence reviewer flagged as weakest link.
- **NE-6 phase confound** — numbered-close vs open-close correlate with session phase, which also correlates with human turn length. Need post-hoc phase stratification spec before running, or NE-6 effect direction could reverse spuriously. Risk reviewer flagged as primary push-back.

If revisited:
- Trimmed scope (NE-6 + NE-2 + NE-7, ~12 hours) is the recommended starting point.
- Skip the H-complexity classifiers (correction-incident subtyping, writing-load category tagger) on first pass.
- Add the phase-stratification spec to the plan before implementation.

**Decision criterion for revisit:** if the skill trial produces ambiguous signal that quantitative analysis could resolve, OR if the user wants to validate the numbered-close anti-pattern claim concretely (Layer 7 experiment #1 territory), then resume Step 4.5.

### 5. Layer 7 A/B candidates

Five A/B experiments specified in `phase-2-findings.md` §4.7. None authorized to run. In rough leverage order:

1. `numbered-question-close` A/B — the highest confirmation-value-per-hour test in the catalog. 5-8 matched pairs. Aviation-CRM evidence external; this validates within-user.
2. `example-led-emergence` vs `pre-action-plan-disclosure` on a founding-vision session. Tests the highest-rivals-converge hidden gem.
3. `emergent-partition` vs `upfront-decision-partition` on a kerf work. Resolves the only "draw" outcome from the challenge-observed reviewer.
4. `assumption-bundle` swap-in on a context-dump-default task.
5. `question-preserving-autonomy` matched-pair with `autonomous-dispatch` on a fatigue-state task.

A/B trials require multi-week user commitment; better deferred until skills trial produces a clear question worth answering with hard evidence.

---

## Stretch trials (hidden gems, lower confidence than Layer 1)

Worth one-off trials on a single appropriate work each, not stacked into the skills:

- **`example-led-emergence`** — for founding-vision sessions, replace context-dump or pre-action-plan-disclosure. Build the spec from concrete cases first; abstract later. Highest-leverage hidden gem (high on all three rival framings).
- **`assumption-bundle`** — agent produces a dependency graph of assumptions; user edits with cascading effects. For tasks where misaligned assumption is the dominant pain point.
- **`emergent-partition`** — instead of declaring "trivial vs critical" upfront, partition emerges as work reveals which decisions are actually load-bearing. Counter-pattern to current `upfront-decision-partition` practice. Genuinely novel; no external analog.
- **`question-preserving-autonomy`** — for fatigue-state work where current default is `autonomous-dispatch` "never ask." Agent runs autonomously but maintains visible questions queue with rework-cost annotations.
- **`dialogic-context-accretion`** — agent pulls context as narrow work reveals gaps, instead of receiving a single upfront brain-dump. For tasks where the current default is `context-dump`.

Each is one trial on one fitting work. Notice and adjust.

---

## Structural / longer-horizon

### 6. Kerf integration draft → actual kerf work

`phase-2-kerf-integration-draft.md` proposes 5 structural additions + 4 instruction-level swaps to kerf. §8 names six open questions for the user. Currently in DRAFT status; not committed.

Decision criterion: revisit when (a) skill-level trial has shown which protocols are worth structuralizing, (b) user is starting a kerf work where these would actually apply.

### 7. Catalog gaps from `phase-2-findings.md` §9

- **Behaviors-first plan expression** (research-statement §8 flagged item; Phase 2 didn't fully explore). Worth a focused follow-up — Step 2-style external pass into TDD, BDD, design-by-contract, specification-by-example.
- **Dependency-aware decomposition** for scope-decomposition / roadmapping task shape. Catalog has no dependency-aware primary protocol. Targeted Step 2 pass into project-management literature (Gantt, PERT, critical chain) if scope-decomposition becomes a felt limitation.
- **Research-scoping question-quality** distinct from hypothesis-quality. Catalog gap; targeted follow-up.

These are research follow-ups, not adoption decisions. Defer until evidence accumulates that one of them matters.

---

## What is explicitly not being investigated

- **Multi-user generalization.** All findings are single-user (this user). Out of scope.
- **Reopening locked Phase 1 / Phase 2 methodology decisions.** Pair-graph framework, multi-framing requirement, transcript-only constraint on Step 4.5 are all locked.
- **Rewriting prior Phase 2 artifacts.** `phase-2-findings.md`, `evaluation-framework.md`, `phase-2-kerf-integration-draft.md` are append-only. New findings go in new files.

---

## Summary table

| Item | Status | When to revisit |
|---|---|---|
| `/session-handoff` + `/session-resume` skills | **Active trial** | After 3-5 real sessions of use |
| Complete Layer 1 stack (3 remaining protocols) | Parked | If trial signal positive |
| Mid-session disciplines (CLAUDE.md) | Parked | After Layer 1 completion shows compose-value |
| Step 4.5 corpus harness | Parked with plan + reviews on disk | If quantitative signal is needed |
| Layer 7 A/B (numbered-close first) | Parked | If a specific hypothesis becomes worth confirming |
| Hidden-gem stretch trials | Parked | One-offs on fitting works as opportunity arises |
| Kerf integration draft → kerf work | Parked (DRAFT) | When skills trial settles which protocols matter |
| Catalog gaps (behaviors-first, dependency-aware, etc.) | Parked | When the gap becomes a felt limitation in real work |
