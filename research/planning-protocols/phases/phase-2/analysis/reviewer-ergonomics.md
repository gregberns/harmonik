# Reviewer: Ergonomics

**Phase 2, Step 5 sub-agent artifact.** Reviewer frame: *ergonomics*. Author: ergonomics sub-agent, 2026-04-23. Input: [unified-protocol-catalog.md](unified-protocol-catalog.md) (87 candidates). Framework: [evaluation-framework.md](../evaluation-framework.md).

## 1. Frame

Ergonomics here means: *moment-to-moment ease-of-use for the human during planning*. A protocol with good ergonomics feels natural to execute, does not require the human to hold protocol-state in their head while thinking about the problem, and does not impose ceremonial moves that interrupt the flow of thought.

My scoring rules:

- **Protocol-state load.** If the human has to remember what phase they are in, which slot to fill, or which move is next, the protocol is taxing ergonomically. Good protocols make the structure the agent's problem.
- **Ceremonial friction.** Protocols that mandate formal moves (numbered restatement, scripted vocabulary, fixed 3-part trigger) per turn score worse than protocols that either (a) do this work on the agent side or (b) fire rarely and legibly.
- **Graceful degradation.** A protocol that still works when the human cuts a corner scores higher than one that breaks, lies, or produces filler when the human deviates.
- **Proportionality.** Overhead should scale with stakes. Protocols that impose the same ritual on a one-line amendment and a new subsystem design score worse on ergonomics than protocols that adapt.
- **Silent execution.** A protocol that silently does work for the human (auto-readback, auto-scribe, auto-park) is ergonomically superior to one that *asks* the human to do the same work.

Note on what I am *not* scoring: task-type adaptability, robustness-to-fatigue, and cognitive-load-in-the-technical-sense are other reviewers' turf. Where a protocol is ergonomically fine but fails on one of those, I say so briefly and move on. I am explicitly not deferring to observed patterns; many of the observed-corpus favorites score poorly on my frame.

Corpus-signal caveat (§4.5): the transcript-signal filter has not been executed. My rankings are analytical. Protocols flagged `[filter-dep]` below depend on corpus evidence the filter would provide; treat those as provisional.

---

## 2. Top 20 — best ergonomics

Ranked roughly. Rationale cites protocol IDs.

1. **`forced-choice-with-default`** — Converts the common case ("agent proposes; user ratifies") into a zero-writing event: silence = yes. Ergonomically near-optimal when defaults are right; the human opts *in* to cost only for the override case. Proportional by construction. Depends on the agent calibrating defaults, which is the agent's problem, not the human's.

2. **`commanders-intent`** — Three short sentences at session open (purpose, key tasks, end state) is very low human writing-cost, and once written it does not have to be touched again. The human does not hold protocol-state mid-session; the agent does. Composes cleanly with downstream protocols.

3. **`autonomy-scope-grant`** — A single paired-grant/bound sentence ("you can decide X, Y, Z; pause for W") front-loads a one-time cost that eliminates per-decision renegotiation overhead for the rest of the session. Classic "pay-once, benefit-many."

4. **`upfront-decision-partition`** — Observed form of the above. Same ergonomic logic; has the tradeoff that if the human guesses the wrong categories the correction work is deferred, but the deferred correction is still cheaper than per-decision interrupts.

5. **`scribe-sub-agent`** — Silent, parallel execution of a task the human would otherwise have to remember to do. Pure ergonomic win: a dedicated agent maintains session log; human does nothing.

6. **`agent-surfaced-parking`** — Agent tracks parked branches and surfaces them at appropriate moments. The human offloads "what did we park earlier?" entirely. The ergonomic risk (agent surfaces at wrong moment) is low-cost to ignore.

7. **`asynchronous-navigator`** — Agent emits skimmable checkpoints; human reads async. Directly addresses the ergonomic cost of synchronous review. Only exists as an option in agent settings and is a rare example of a genuinely agent-native ergonomic win.

8. **`shuttle-diplomacy-reviewer`** — Interposed reviewer absorbs tone-work, paraphrase checking, and question-batching. From the human's seat the session feels simpler: fewer, better turns arrive. The main ergonomic risk is opacity, which the protocol addresses via visible audit log.

9. **`pre-reply-self-review`** — All work happens on the agent side; human sees only cleaner replies. If it fires well, the human experiences better message quality with zero additional work. `[filter-dep]` on whether measurable quality gains exist, but ergonomically the protocol is free.

10. **`aporia-graceful-stop`** — A named, low-ceremony move to park an unresolved question. Cost to execute is one short agent turn; the human ratifies or overrides. Prevents unproductive spinning, which is a direct ergonomic win.

11. **`tactical-pause`** — Scripted trigger phrase that either party can call. Ergonomically, this is a *release valve*: rather than holding "we're not aligned, should I interrupt?" in mind, the user can fire the trigger cheaply. Composes with `sitrep-at-cadence`.

12. **`affirmations-competence`** (narrow form) — "You've already established X, so I'll skip re-deriving it" is a one-line recognition that directly shortcuts rework. Ergonomically it is the opposite of ceremonial: it saves human turns.

13. **`coding-opord-4slot`** — Four-slot artifact. Four is the upper bound of what a human can fill without mental paging; compresses SMEAC and engagement-letter to something small enough to be ergonomic yet structured enough to carry weight. Better ergonomics than `ipass-opener` (5 slots) or `sbar-opener` (reads fine but lacks scope slot coding needs).

14. **`single-text-procedure`** — Single persistent artifact. The human critiques rather than drafts. Critiquing is lower-effort than drafting. The artifact is always in a usable state, removing the "plan lives in dialog, must be extracted" cognitive cost.

15. **`fixed-token-status-vocabulary`** — Small token set (*proposed*/*locked*/*deferred*/*rejected*) is ergonomic because once learned, the human can correct in fewer characters than prose. After a short learning curve the protocol *reduces* per-turn writing cost. Risk: ergonomically great only if the token list matches what the user actually means; composition with a bad list is brittle.

16. **`dialogic-context-accretion`** (counter-pattern #6) — Shifts context-writing from a large upfront brief to many small, evidence-justified requests. Each individual human turn is small and narrowly framed. Ergonomically this reads well: the human never has to do the "write everything I might eventually need" dump.

17. **`example-led-emergence`** (counter-pattern #1) — The human responds to concrete cases ("should this case produce X or Y?"), which is lower ergonomic load than judging abstract plans. "Which output is right?" is the kind of question a senior user can answer in one line.

18. **`handoff-closed-acknowledgment`** — Small explicit ritual at exactly the moments (session-resume, sub-agent spawn, multi-day pickup) where ergonomic failure modes are real. Low base cost, targeted firing.

19. **`recovery-handoff`** — Structured resume payload. The human writes it once; subsequent sessions read it and start in the right place, so the human does *not* re-orient each time. Payload-maintenance cost is the ergonomic risk and the main reason it is not higher.

20. **`so-what-test`** — Pure agent-side discipline. Human experiences shorter, tighter replies with no additional work. `[filter-dep]` on effect magnitude, but the ergonomic math is clear: any quality improvement is free from the human side.

---

## 3. Bottom 10 — worst ergonomics

1. **`rfc-full-form`** — Nine mandatory sections (summary / motivation / guide-level / reference-level / drawbacks / rationale-and-alternatives / prior-art / unresolved-questions / future-possibilities) is exactly the protocol-state-in-your-head failure. High ceremony, brittle to deviation, punishes small changes equally with large. Ergonomically the worst of the "heavy artifact" group.

2. **`pep-style-rfc`** — RFC + hard gates is strictly more ergonomically costly than `rfc-full-form` because the gate *forces* completion before advance. Graceful degradation is zero by design. This is ergonomically hostile even before asking whether the substance is good.

3. **`ipass-opener`** — Five-slot opener with mandatory receiver synthesis. Each session starts with the human producing five pieces of structure *and* the agent's synthesis creates a read-back gate the human must review. The evidence for outcome value is strong in medicine; the ergonomic cost in a solo planning session is high.

4. **`smeac-order`** — Five-paragraph OPORD is a full-ceremony opener. A&L and C&S paragraphs don't map cleanly to coding and produce filler. The `coding-opord-4slot` compression is the ergonomic fix; full SMEAC stays bottom-ten.

5. **`teach-back-loop`** — Asks the *senior human* to restate an agent's proposal in their own words and repeat until it lands. Ergonomically: the interaction inverts the usual direction (agent repeats human) with an insulting-feeling load on someone who understands the domain better than the agent. Even well-targeted use costs more than alternatives.

6. **`load-bearing-token-readback`** — Every turn prepended with a token echo. Per-turn ceremony on many turns. Individually small; aggregated across a session it is expensive and interruptive. The corpus should be checked `[filter-dep]` for whether any particular turn's readback ever catches something, because the ergonomic cost is per-turn and the value is sporadic.

7. **`readiness-ruler`** — 0-10 importance-and-confidence rating plus two follow-ups at decision points. The *numerical* framing alone reads clinical to a senior dev; the two-axis shape is apparatus-heavy. Ergonomically the protocol asks a senior user to perform an artifact that is designed for much less-expert respondents.

8. **`strong-style-pairing`** (human-as-navigator direction) — "For an idea to go from your head into the computer, it must go through someone else's hands" — forces the human to articulate every step of what they want. This is the opposite of ergonomic: it maximizes human writing effort per unit of work. (The *agent-as-navigator* inversion of this protocol is interesting for other frames but falls outside my scoring.)

9. **`classical-driver-navigator`** — Continuous navigator attention is the exact attention-cost the research track is trying to reduce. Ergonomically incompatible with solo-senior-with-agent model.

10. **`nvc-four-slot`** — Four slots (observation / implication / need / request) per message. Even the protocol's own description says "use implicitly, explicitly only on high-stakes contributions" — which is a signal that explicit use is ergonomically too heavy. Worth naming because the implicit form is fine but the explicit form is routinely the trap.

---

## 4. Surprising scores — divergences from the naive observed-pattern view

Things that matter more than my raw ranking suggests; or less.

**Overvalued by naive observation:**

- **`numbered-question-close`** (observed, treated as a strong pattern by the user). Ergonomically it is *fine*, not great. It imposes a small but real structural demand on the human: answer per-number or feel like they are dodging. It also suppresses unframed concerns — a subtle ergonomic cost because it makes the human feel they must answer what was asked rather than what they actually want to raise. The corpus evidence that it shortens the *next* human turn is a proxy for compliance, not for ease-of-use. Aviation CRM literature flagging "any questions?" closers as *less effective than interleaved slot-acknowledgment* is the disconfirming signal. I would rank it mid-pack, not top-5, on ergonomics. `[filter-dep]` on whether late-session-issue-density rises under numbered-close.

- **`kerf-parallel-reviewer`** (observed, established kerf practice). The reviewer sub-agent model is good, but the *default* posture of running multiple reviewers creates work on the human side: synthesizing disagreements, arbitrating between reviewer outputs, overriding silently-or-with-rationale. Ergonomically there is a hidden writing load here that the observed adoption obscures. The ergonomic fix is `articulate-override-rule` *only on disagreement*, not on every override.

- **`context-dump`** (observed, extreme). Appears to save many turns, but the ergonomic cost of producing a 5000-char brief is large, unevenly distributed (it has to happen at the *worst* moment, when the human has the least model of the problem), and brittle to model-shift mid-session. Low on ergonomics despite its visible presence in the corpus.

**Undervalued by naive observation:**

- **`forced-choice-with-default`** (observed as *rare*). Rarity in corpus does not mean rarely useful; it may mean the user has not yet reached for it. Ergonomically this is a top-tier move; its under-adoption is a local-maxima signal, not a quality signal.

- **`agent-surfaced-parking`** (unexplored). Absent from the corpus but ergonomically excellent. Its absence is exactly the local-maxima anchoring risk the research statement flagged.

- **`dialogic-context-accretion`** (counter-pattern #6). Reads ergonomically well; under-adopted in corpus because the observed pattern is the opposite (rich-brief upfront). The counter-pattern's ergonomic profile is quietly strong.

- **`example-led-emergence`** (counter-pattern #1). Senior users are better at judging concrete cases than at evaluating abstract plans in a single pass. The corpus does not exhibit this pattern, but ergonomically it is competitive with or better than `pre-action-plan-disclosure`.

- **`scribe-sub-agent`** (external). The ergonomic arithmetic is obvious once stated and it is effectively never executed by the user. Another local-maxima indicator.

**Undervalued by my frame but worth naming so I am not double-counting:**

- **`iap-cadence-artifact`** and **`sitrep-at-cadence`** — these are ergonomically medium-cost per firing, but their *cadence* (firing on schedule, not on trigger) means the human does not have to notice when to do them. That's actually an ergonomic asset, not a liability, that my per-turn-view understates.

---

## 5. Composition conflicts

Protocols that stack gracefully, and protocols that do not.

**Graceful stacks:**

- `commanders-intent` + `back-brief-plan-quality` + `autonomy-scope-grant` — each fires once or rarely; each pays off many-times. Ergonomically compatible.
- `forced-choice-with-default` + `agent-surfaced-parking` + `scribe-sub-agent` — three independent agent-side silent operations. No protocol-state handoff between them.
- `single-text-procedure` + `so-what-test` — the so-what test runs on the single draft and produces a tighter version; user work unchanged.
- `aporia-graceful-stop` + `agent-surfaced-parking` — aporia parks, surfacing brings back. Composes cleanly; each reduces human tracking load.
- `tactical-pause` + `sitrep-at-cadence` — cadenced updates give the human a natural moment to fire a pause rather than having to invent one.

**Bad stacks (ergonomically corrosive):**

- **`load-bearing-token-readback` + `numbered-question-close`** — both produce per-turn structure on the agent side and per-turn response demand on the human side. Together they double turn ceremony without adding proportional signal.
- **`read-back-comprehension` + `teach-back-loop`** — read-back in one direction plus teach-back in the other on the same handoff gives two restatement gates on one transition. Either alone is defensible; both is ergonomic overkill.
- **`ipass-opener` + `confirmation-brief` + `back-brief-plan-quality`** — three handoff-phase artifacts covering near-identical ground at three successive moments. Pick one.
- **`pep-style-rfc` + `hard-gate-missing-section` + `alternatives-considered-section`** — three layers of enforcement on the same artifact. The ergonomic cost compounds; gate-to-gate friction dominates.
- **`micro-step-incrementalism` + `load-bearing-token-readback`** — the first already breaks work into 2-5-minute steps; adding per-turn readback on top means most of each step is ceremony. The two protocols implicitly choose opposing turn-densities and cannot both be active.
- **`commanders-intent` + `rfc-full-form`** — the former is "specify less procedure, trust intent"; the latter is "specify everything in named sections." They are philosophically and ergonomically at odds; a session committed to both will gravitate to the heavier.
- **`non-directive-stance` + `numbered-question-close`** — non-directive says "don't advocate, don't propose"; numbered-close is an advocate-and-batch move. Using both means the agent is alternately withholding and pushing; the human experiences that as mode-confusion.

**Subtle composition issue:**

- **`kerf-parallel-reviewer` + `articulate-override-rule`** — individually fine. Composed, the user now has to review N reviewer outputs *and* articulate reasons for each override. Ergonomically, parallel reviewers work best with *silent* override of low-signal comments and articulated override only on flagged-substantive comments. The composed default (articulate-always) is too heavy.

---

## 6. Counterfactual inversion

What if core Phase 1 findings were inverted? Would my rankings shift?

**Finding: Numbered-question-close shortens subsequent human turns.**
*Counterfactual:* if multi-question-per-turn were actually *good* for the human (because it lets the human answer fast and move on), my ranking would shift. I would move `numbered-question-close` up (from mid-pack to top-10), and the bottom-10 position of several structured multi-slot protocols (`ipass-opener`, `smeac-order`) would *partially* recover — the ergonomic cost of multi-slot structure would then be offset by faster human response. But even under the inversion, `rfc-full-form` and `pep-style-rfc` stay bottom-ten because their cost is not per-turn but per-artifact, which the counterfactual does not address. **Partial rank shift; not structural.**

**Finding: Pre-action plan disclosure produces zero-cost corrections.**
*Counterfactual:* if pre-disclosure *caused premature commitment* (the counter-pattern #1 steelman), my rankings of `example-led-emergence` and `dialogic-context-accretion` would move *up* (they are already mid-high) and `pre-action-plan-disclosure` would drop from where I have it implicitly (decent-ergonomics; the counter-pattern is explicitly a frame-lock risk). More interestingly, `hypothesis-driven-ghost-deck` would drop sharply because it is pre-action-disclosure with stronger framing commitment. My ranking of the example-led / dialogic-accretion cluster survives the inversion; my implicit-positive view of pre-disclosure does not. **Genuine shift for a subset of rankings.**

**Finding: Rich-brief context-dump openers produce higher error-propagation.**
*Counterfactual:* if rich-brief were actually *ergonomically optimal* (one-shot cost, no per-turn overhead), my ranking of `context-dump` would rise, and `commanders-intent` / `coding-opord-4slot` / `scqa-opener` would become roughly indistinguishable in ergonomics (all one-shot, different levels of structure). But even under the counterfactual, `context-dump` loses to compressed-openers because the *size* of the one-shot cost still matters ergonomically — 5000 chars is worse than 500 chars even with the same amortization. **Minor shift; `commanders-intent` stays top-5 because compactness is itself an ergonomic virtue independent of the upfront-vs-distributed question.**

**Finding: Parallel reviewers produce orthogonal issue classes worth the synthesis cost.**
*Counterfactual:* if parallel reviewers mostly *duplicated* each other, the ergonomic cost of synthesis would overwhelm the catch benefit. `kerf-parallel-reviewer` drops; `premortem-reviewer` as a *single* adversarial reviewer rises (single reviewer avoids synthesis cost); `role-split-reviewer-library` becomes strictly worse than `premortem-reviewer`. **Rankings shift meaningfully in this region; my current neutrality on parallel-reviewers is precisely because the underlying finding is contested.**

---

## 7. What the ergonomic frame recommends

If I were picking a small bundle for a solo senior dev planning with a coding agent, ergonomics says: open with `commanders-intent` + `autonomy-scope-grant` (one-time, small). Let the agent run under `forced-choice-with-default` as the per-decision move. Offload bookkeeping to `scribe-sub-agent` and `agent-surfaced-parking`. When the agent needs to reason about output, run `so-what-test` and `pre-reply-self-review` as silent agent-side disciplines. When things get stuck, use `aporia-graceful-stop` or `tactical-pause`. Keep the plan in a `single-text-procedure` artifact. Avoid the heavy-artifact group (`rfc-full-form`, `pep-style-rfc`, `smeac-order`) unless the stakes warrant the ceremony. On counter-pattern candidates, ergonomics specifically endorses `dialogic-context-accretion` and `example-led-emergence` over their observed-pattern rivals, recognizing that observed-pattern dominance reflects path-dependence, not superiority on this frame.

Ergonomics is one frame of several. A recommendation optimal on ergonomics may be wrong on regret-adjusted outcome or on plan durability. The filter stage (§4.5) has not been run; several of my rankings depend on it. But the frame's shape is clear: silent agent-side work beats ceremonial human-side work, front-loading one-time costs beats paying per-turn, and compact structure beats full-artifact structure except where the stakes justify the ceremony.
