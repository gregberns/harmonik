# Evaluation-Criteria Refinement -- Sub-Analysis: Operationalization Audit

Generated: 2026-04-23
Phase 2, Step 1, sub-task 1 of the planning-protocols research track.

## Scope

This audit interrogates the provisional evaluation criteria in `research-statement.md` §2 and the user-proposed candidate additions in §7 Step 1. For each criterion it answers six standardized questions: operationalizability, signal source, proxies, within-/cross-session, ceiling/floor effects, and interpretation dependencies. It then summarizes the criteria set across cost tiers and trade-off/correlation structure, and concludes with concrete recommendations for which criteria to keep, replace, or demote.

The audit's lens is deliberately narrow: **what can be measured, from what source, at what cost, with what confounds.** It does not yet propose protocol designs.

Primary data source assumed throughout: the 10 extracted planning-dialog transcripts under `01-corpus/`, plus the corpus-wide character counts, turn counts, autonomous-run markers, gap durations, and tool-call counts already surfaced by the Phase 1 lens reports. Where external signals are required (implementation outcome, user self-report, cross-session comparison), that is explicitly flagged as an expense.

One operational convention used throughout: "transcript-only" = derivable from the JSONL or extracted markdown without asking the user anything. "User-report" = requires the user (the one named in this research) to answer a follow-up. "Outcome-linked" = requires joining the transcript to downstream implementation artifacts (commits, bug reports, spec revisions, time-to-merge) to interpret.

---

## Primary criterion 1: Human writing / clarification effort

The scarce-resource criterion. Phase 1 already operationalized this crudely via character counts.

**Operationalizability.** Yes, cheaply. Direct instrument: character count of all human-authored text turns in the session, excluding machine-generated turns (tool results, `<command-name>` tags, task notifications, paste detection wrappers). Formula: `sum(len(turn.text) for turn in session if turn.role == 'user' and not turn.is_machine_generated)`. Unit: characters (or words, or token-equivalents; character is the most faithful to "time at keyboard").

Cleaner variants of the same metric:
- Characters per substantive turn (excludes "yes" / "go" / approval noise from the denominator).
- Characters spent on *each* category from the writing-load lens (a-h). Category-weighted totals are more diagnostic than a raw total: a session can be high-total because of one context-dump opener (cheap per outcome) or because of seven redirection turns (expensive per outcome).
- Type-rate-adjusted time-at-keyboard (assume ~40 wpm baseline) rather than characters. Same signal, different units.

**Signal source.** Transcript-only. The existing `extract_dialog.py` already separates human turns from machine turns; adding per-category tagging is a modest extension (either hand-label as Phase 1 did, or auto-label with heuristics: turn-position, n-questions-in-preceding-agent-turn, presence of correction tokens like "no", "actually", "not that"). No user follow-up required.

**Proxies.** Direct measurement is available, so proxies are unnecessary. But useful *decompositions* exist: (i) raw characters, (ii) characters excluding opening framing, (iii) characters spent on correction (category b) specifically. Decomposition (iii) is a better criterion than the raw total, because "human writing to establish novel intent" is unavoidable while "human writing to correct agent misframing" is the thing a protocol should reduce.

**Within-session vs cross-session.** Within-session measurable. But *interpretation* requires cross-session comparison: 4,000 chars is low for a founding session, high for a refinement session. For protocol comparison, you need matched tasks (same planning problem handled under different protocols) or matched phases (founding vs refinement).

**Ceiling/floor effects.** Severe floor effects. A protocol can drive this metric to zero by letting the agent write an entire spec from a one-line dispatch (13493c8d at the extreme: 8,700 chars upfront then ~0 for the rest of the session -- but the 8,700 chars still existed). Worse, a *zero-character* session is achievable if the human provides no input at all, but then there is no intent to plan against. The metric by itself rewards silence, which is not the goal. It must be paired with at least one alignment metric to be useful.

**Interpretation dependencies.** What problem was being planned (founding vs refinement); whether prior sessions had pre-built context; whether the human added scope mid-session (scope-expansion is human-authored by necessity, not protocol failure); whether the session's final plan was correct. Without these, a low number can mean "protocol worked" or "human disengaged and agent made things up."

---

## Primary criterion 2: Targeting of agent questions

Whether agent questions hit architectural uncertainties vs waste turns on trivia.

**Operationalizability.** Partially. The atomic unit is an agent question. An agent question is detectable from transcripts (question mark, "?", enumerated choice list, "A or B", "which would you prefer", etc.) with reasonable precision/recall. Less trivial: classifying each question as *trivial* vs *architectural* vs *requirements-clarification* vs *status-check*. Phase 1's decision-delegation lens did this by hand for all 10 sessions; the categories were stable.

Candidate formulas:
- `trivial_question_rate = trivial_questions / total_questions`. Lower is better.
- `avg_architectural_questions_per_session` (absolute). Higher is better -- absence of architectural questions is a failure mode too.
- `wasted-question count` = questions where the human's response was "whatever / I don't care / your discretion / fine". Directly detectable from transcripts via a small lexicon. This is the most empirically grounded variant; Phase 1 counted 6-8 of these across the primary corpus.

**Signal source.** Transcript-only for *wasted-question count* (human response reveals the question was wasted). Transcript-only-with-classifier-judgment for *trivial-question rate* (requires a classifier -- either hand-labeled or LLM-judged -- to decide if a question was trivial ex ante). The LLM-judge approach is cheap to run and produces consistent labels across a corpus; it does introduce judge-model bias.

**Proxies.**
- *Wasted-question count* is the strongest transcript-only proxy. Confound: a human might answer "whatever" to a question that was actually architectural but they were fatigued. Mitigation: cross-check against post-session outcome (did that decision later need revision?).
- *Question-density over session length.* High question density early can mean good probing; high question density late can mean the agent never converged. Confound: density depends on dialog form, not just question quality.
- *Ratio of human-preempted-decisions to agent-asked-decisions.* If the human frequently preempts ("here's what I want: X"), either the human is over-directing (bad protocol match) or the agent is under-asking the critical stuff (bad question targeting). Hard to disambiguate without context.

**Within-session vs cross-session.** Within-session measurable on the atomic question. Trends (improving question targeting across a session) require session-internal time-series. Cross-session comparison is needed to say "protocol X targets questions better than protocol Y."

**Ceiling/floor effects.** A protocol that forbids all agent questions scores perfectly on *trivial-question rate* (zero of zero) but catastrophically on *architectural questions per session* (zero again) -- false positive if only the first metric is used. A protocol that asks only architectural questions but asks them redundantly scores well here while failing "correction-cycle count" (same ground re-covered).

**Interpretation dependencies.** What the agent "should have asked" requires knowing what the human actually needed. A question that looks trivial might have been the right probe for a fuzzy-intent session. Interpretation needs session-goal context.

---

## Primary criterion 3: Correction-cycle count

How often the human had to redirect the agent.

**Operationalizability.** Yes, with care. A "correction" is a human turn whose purpose is to redirect or contest a prior agent turn. Phase 1's writing-load lens already separates category (b) redirections from (f) decision responses. Detection heuristics: correction-tokens at turn start ("no", "actually", "wait", "that's not", "I don't think"), negation of prior agent framing, explicit "go back" / "restart" / "let's try again" directives. Not perfect -- some corrections are gentle and lexically indistinguishable from normal dialog.

Formula candidates:
- `corrections_per_session` (absolute count).
- `corrections_per_10_turns` (normalized).
- `correction_character_share` (chars in category-b turns / total substantive chars).
- `correction_depth` -- whether a single correction spawned follow-up corrections (same thread re-emerging vs one-and-done).

**Signal source.** Transcript-only for counting, with either hand-labeling or an LLM-judge. Category-b was hand-labeled in Phase 1; the categorization is replicable. Semi-automated classification at 80%+ agreement with hand labels is feasible.

**Proxies.** Direct measurement is available. Useful decompositions:
- *Framing correction* (agent asked/asserted the wrong way) vs *content correction* (answer was wrong). Phase 1 flagged framing corrections as more expensive; they deserve separate counting.
- *Premature-commitment correction* (agent said "locked" when design is open). Small, detectable pattern.
- *Same-thread repeated correction* (agent needs to be told the same thing twice). Highest-cost subclass.

**Within-session vs cross-session.** Within-session measurable. Protocol comparison needs cross-session matched-task design.

**Ceiling/floor effects.** A protocol that minimizes correction count by preventing the agent from taking initiative at all (pure read-back) scores zero on corrections but provides no autonomy benefit. Context-dump (13493c8d) scored near-zero by eliminating the correction *loop* -- but the same errors instead propagated silently into implementation, an outcome this metric cannot see. So: low corrections are good only conditional on the alignment *outcome* being good.

**Interpretation dependencies.** Whether the session converged on a plan the human actually wanted; whether "zero corrections" means "no errors" or "no opportunity to correct." The in-session/post-session decoupling is exactly why this criterion must be paired with a post-implementation outcome signal to be trustworthy.

---

## Primary criterion 4: Agent autonomy on trivia

Whether the agent decides file names, library choices, structural details without asking.

**Operationalizability.** Moderate. The atomic unit is a decision moment. Detecting decisions in transcripts is harder than detecting questions because many decisions are implicit in agent output (naming, file layout, style choice embedded in proposals). Phase 1's decision-delegation lens used careful hand analysis; automated detection is imperfect but workable if constrained to *explicit* decisions (choices the agent surfaces, choices the agent announces).

Formula candidates:
- `autonomous_trivia_rate = decisions_decided_autonomously_on_trivial_topics / total_decisions_on_trivial_topics`. Higher is better. Requires both classifying decisions and classifying topics-as-trivial.
- `over-ask count` = questions where the agent asked about a trivial topic (naming, split, layout). Phase 1 counted 6-8 of these; directly analogous to wasted-question count but refined to trivial-topic questions.
- `autonomy-grant latency` = turn number at which the human had to grant blanket autonomy ("whatever you think"). Session-opener grants (as in 79a42399) = latency 1. Late grants (as in 38415843) = latency 3+. Lower is better.

**Signal source.** Transcript-only with classifier support. The decision-delegation lens showed that *what is trivial* is stable across sessions for this user (file layout, naming, split-vs-single always trivial; architecture framing, behavior semantics never trivial; tool selection context-dependent). A small hard-coded domain taxonomy would get most of the way there.

**Proxies.**
- *Over-ask count* (described above). Strong.
- *Human-used-autonomy-grant-language count* (frequency of "whatever", "your discretion", "don't care" in the human turns). Indirect: high count suggests the agent is asking things the human doesn't want to decide. Confound: some humans use this language conversationally.
- *Agent self-labeling of question types* -- if the protocol asks the agent to pre-tag questions, the audit becomes trivial. Currently absent; would be a protocol feature not a measurement artifact.

**Within-session vs cross-session.** Within-session measurable per-decision. The autonomy-grant-latency metric is most diagnostic at the session level; comparing across sessions compares protocols.

**Ceiling/floor effects.** A protocol that lets the agent decide everything (even architectural decisions) scores perfectly but fails decision-delegation quality. A protocol that forbids agent autonomy altogether scores zero on trivia-autonomy and dumps all decisions on the human. The criterion needs to be bounded: "high autonomy on truly-trivial decisions, low autonomy on truly-architectural decisions." The two-axis structure is necessary; a single number is misleading.

**Interpretation dependencies.** What is "trivial" is user-specific. For this user the Phase 1 taxonomy is explicit; for a different user the taxonomy might differ. Cross-user transfer requires re-classifying.

---

## Primary criterion 5: Human context-switch load

Whether the protocol lets the human do other work during agent runs.

**Operationalizability.** Partially. Transcript-derivable signal: wall-clock gaps between human turns (the `gap` field already surfaced in context-switch lens). A gap of 30 minutes with no user turn in between means the user had 30 minutes free. A gap of 30 seconds means constant attention.

Formula candidates:
- `median_user_gap_minutes` across a session.
- `long_gap_count` = number of inter-turn gaps exceeding some threshold (10 min? 30 min?) -- proxies for "periods when the user could focus elsewhere."
- `p95_user_gap_minutes` -- the longest uninterrupted stretch of human freedom.
- `short_ping_pong_count` = number of agent-turn-then-human-turn sequences where both turns are <1 min. Counter-metric: high ping-pong is attention-corrosive.

**Signal source.** Transcript-only. Gap times are already extracted in the context-switch lens.

**Proxies.** Gap-based metrics are the direct signal, not proxies. But gaps have a serious confound: **gaps don't distinguish "user doing other work" from "user asleep" from "user stuck and re-reading."** Phase 1 observed overnight dispatches (f588ff0c) producing a 48-minute autonomous run; the gap is real but the user's context-switch load during that gap was zero (they were asleep). An awake user getting a 48-minute free block is a very different outcome from a sleeping user getting the same block, but the transcript cannot distinguish them.

Secondary proxies:
- *Agent-turn tool-call count* (many tool calls = long agent work = free human time).
- *Ratio of autonomous-run agent turns to total agent turns*.
- *Presence of dispatch-directive language in preceding human turn* (signals deliberate context-switch intent).

**Within-session vs cross-session.** Within-session measurable. Interpretation requires knowing the user's actual availability state (sleep, meetings, other work); transcripts don't reveal this.

**Ceiling/floor effects.** A protocol can game this metric by producing one 8-hour autonomous run (context-dump at limit). The gap is huge but the user had no alignment check during those 8 hours -- any error compounds. Also: a protocol with zero human turns scores infinity on gaps but is not a planning protocol. Must be paired with alignment metrics.

**Interpretation dependencies.** User's actual activity state during gaps; whether the gap was deliberate (dispatch, overnight) or accidental (user got distracted, forgot); whether the agent produced useful work during the gap or was blocked.

---

## Secondary criterion 1: Downstream implementation quality

Must-not-regress signal. Phase 1 flagged implementation quality as already strong.

**Operationalizability.** Difficult. "Quality" has no single scalar. Candidate instruments:
- *Bug count per implementation* joined to a planning session (requires git/issue-tracker linkage).
- *Revert or revision count* on commits implementing a plan.
- *Spec-to-implementation drift* -- how much the implementation diverged from the spec (textual diff heuristics; unreliable).
- *Code-review comments* on PRs implementing the plan (not collected here).

**Signal source.** Outcome-linked, expensive. Requires joining transcripts to downstream implementation artifacts. For this user's solo practice, "PRs" and "code review comments" mostly don't exist; commit messages + manual inspection are the best available signals.

**Proxies.**
- *Spec-revision count after implementation started.* If the spec kept changing, the plan was inadequate. Detectable if specs live in git.
- *Time-to-stable-implementation* (commits between first-implementation-commit and last-related-commit). Confound: longer time can mean higher quality (polish) or worse (bugs).
- *Reopened-decision count* -- if the planning session resolved a decision and a later session re-opened it, the plan was incomplete. Cross-session measurable if sessions are indexed by topic.

**Within-session vs cross-session.** Cross-session inherently -- requires linking the planning session to later implementation and even-later maintenance.

**Ceiling/floor effects.** Hard to game because it's outcome-based. But measurement noise is high: solo development has no external reviewers to catch quality issues.

**Interpretation dependencies.** What "quality" means for this user's projects; whether the task was simple (so any plan would have worked) or genuinely hard; whether downstream changes reflect plan failure or discovered new requirements.

---

## Secondary criterion 2: Spec completeness at implementation-start

Must-not-regress signal.

**Operationalizability.** Partial. If the plan produces a discrete spec artifact, completeness can be assessed by checklist: does it cover X, Y, Z? The user's existing spec rubric (problem-space, decomposition, research, design, spec-draft, integration, tasks passes in kerf) provides an implicit checklist. But completeness judgment is inherently qualitative.

Formula candidates:
- *Spec-section-coverage* = fraction of expected sections present (if template is known).
- *Word count of spec* (weak; long specs are not automatically complete).
- *Presence of explicit decisions on known-hard axes* (e.g., "state source of truth", "process lifecycle", "reconciliation"). Cross-referenceable with a locked-decision list.

**Signal source.** Outcome-linked, moderate expense. The spec artifact exists in the repo; joining transcripts to the spec they produced is feasible via branch names or commit timestamps.

**Proxies.**
- *Post-implementation questions that weren't in the plan.* If implementation surfaces "but what about X?" and X wasn't in the plan, the plan was incomplete. Detectable by searching later sessions / beads / commits for decision-language against the plan's topics.
- *Count of TODO / FIXME / "decide later" markers in the spec.* Easy to grep.
- *Implementer-time-to-first-blocker.* If the implementer (possibly an agent) gets stuck within minutes, the plan was thin.

**Within-session vs cross-session.** Cross-session. Requires post-planning implementation to reveal gaps.

**Ceiling/floor effects.** A protocol that produces exhaustive specs trivially wins -- but the protocol's cost (human writing, wall-clock) will balloon. Completeness traded against the primary criteria.

**Interpretation dependencies.** How detailed a spec needs to be is task-dependent. A trivial task gets a terse spec and that's fine.

---

## Secondary criterion 3: Total wall-clock time

Must-not-regress, but cheap.

**Operationalizability.** Trivially yes. First-turn timestamp to last-turn timestamp of the planning session. If multi-day, either include the whole span (reflects calendar time) or sum active periods by detecting gaps over a threshold (reflects actual work time).

**Signal source.** Transcript-only. Timestamps are present in JSONL.

**Proxies.** Direct measurement is cheap, proxies unneeded. Decompositions useful:
- *Active wall-clock* = sum of inter-turn gaps under some threshold (e.g., 30 min).
- *Calendar wall-clock* = first turn to last turn including gaps.
- *Human-active time* = sum of human-turn durations estimated from character-count and type-rate, plus some post-agent-turn reading time.

**Within-session vs cross-session.** Within-session measurable.

**Ceiling/floor effects.** A protocol can minimize wall-clock by skipping steps -- producing a fast, bad plan. Must be paired with quality/completeness.

**Interpretation dependencies.** Task size; whether the session was intentionally paused for overnight work.

---

## Candidate addition 1: Quality of mental-model transfer

Whether the agent's internal model of what the human wants matches what the human actually wants.

**Operationalizability.** Poor in principle. The human's true intent is not directly observable from transcripts; it has to be reconstructed from what the human says, which is a noisy rendering. Mental-model transfer *quality* therefore has no direct instrument.

Instruments one could attempt:
- *Agent paraphrase accuracy.* At session milestones, the agent restates its understanding; the human confirms or corrects. A high-quality protocol produces paraphrases the human accepts. Detectable from transcripts: look for agent turns containing "my understanding is" / "to confirm" / "let me restate" and measure subsequent human correction rate.
- *Novel-context test.* Give the agent a new situation after planning and see if its decisions align with what the human would have decided. Requires post-session user-report; expensive.

**Signal source.** Mixed. Paraphrase-accuracy is transcript-only (cheap). Novel-context is user-report (expensive). The deepest-signal versions are outcome-linked (does implementation reveal model mismatch).

**Proxies.**
- *Correction-depth* (criterion 3 subclass: did one correction spawn more). Deep correction chains signal mental-model mismatch vs simple content errors.
- *Post-commit revision count* for decisions made during the session.
- *"No, what I actually meant..." count* -- lexically detectable framing corrections.

**Within-session vs cross-session.** Both. Within-session paraphrase-accuracy is a local signal. Mental-model transfer that lasts requires cross-session check (does the agent, in a new session on the same topic, still act consistent with the prior model).

**Ceiling/floor effects.** A protocol that makes the agent echo back everything trivially inflates paraphrase counts without improving real transfer. A protocol where the agent never paraphrases scores zero on the in-session instrument but may have deep transfer (absence of evidence).

**Interpretation dependencies.** Heavy. The human's own intent may be fuzzy (see candidate addition 7). Partial mental-model transfer against fuzzy intent is sometimes the correct outcome.

**Net assessment.** This is the criterion Phase 1 findings hint at most (misaligned-assumption lens) but is the hardest to measure cleanly. The best transcript-only version is *framing-correction count* -- a direct subclass of criterion 3.

---

## Candidate addition 2: Probability of post-implementation regret

Does the plan produce what the user actually wanted, or is there a "why did we build it this way?" moment later?

**Operationalizability.** Low from transcripts alone. Regret is a post-hoc judgment, emitted (if at all) days or weeks later.

Instruments:
- *Spec rewrite within N weeks* of original planning. Detectable from git.
- *Re-planning session on same topic.* Cross-session detectable if sessions are topic-tagged.
- *User self-report questionnaire* post-implementation. Expensive and against the user's time-scarcity principle.

**Signal source.** Outcome-linked, usually expensive. Cheapest automated version is "was this spec revised substantively within 30 days of first implementation commit?"

**Proxies.**
- *Frequency of "we should have..." / "I wish we had..." / "this doesn't actually..." tokens in later sessions.* Rough lexical proxy. Might actually work for this user because their voice is fairly consistent.
- *Beads items created to revise prior plans.* If the task ledger shows "revise plan X", that's regret-evidence.
- *Commit messages containing "actually" / "revert" / "second try" patterns.*

**Within-session vs cross-session.** Cross-session inherently.

**Ceiling/floor effects.** A protocol that produces regret-proof plans by producing only very generic plans ("build a thing that does the thing") will score well on regret-absence but fail spec-completeness. Must be paired.

**Interpretation dependencies.** Regret might reflect the plan or reflect changed requirements. Hard to separate.

**Net assessment.** Important criterion conceptually; measurable only via outcome joins, at modest expense. The spec-revision proxy is the most tractable.

---

## Candidate addition 3: Robustness to user state

Does the protocol still work when the human is tired, distracted, novice-in-domain, or drained?

**Operationalizability.** Low from transcripts alone. User state is not directly observable.

Instruments:
- *Natural-experiment discovery.* Find sessions that ran at unusual times (late night, early morning) and compare outcomes against same-user daytime sessions. Timestamps available.
- *User self-tag sessions* with state at start. Against the time-scarcity principle.
- *Lexical-state inference.* Short turns, typos, terse affirmations might proxy for fatigue. Very weak signal.

**Signal source.** Transcript-only with heavy inference (weak) or user-report (expensive).

**Proxies.**
- *Session timestamp* (late-night sessions proxy for tired). Already available; usable for natural experiments.
- *Human-turn character density late in session vs early* -- fatigue effects within a session might reveal. Detectable.
- *Dispatch-style turn count* (e.g., "I'm going to bed" language). Detectable in f588ff0c.

**Within-session vs cross-session.** Both. Fatigue effects within-session are fine-grained; protocol robustness across user-state requires cross-session comparison stratified by state.

**Ceiling/floor effects.** A protocol that works equally badly across all states (always brittle) will trivially score "robust." Needs absolute performance floor first.

**Interpretation dependencies.** Requires knowing the user's state, which is (mostly) unobservable.

**Net assessment.** Useful conceptually, measurable only via natural experiments on session timestamps. Best treated as a *qualitative overlay* rather than a quantitative scoring criterion -- "does this protocol require heavy cognitive engagement from the user? if yes, it will fail under fatigue."

---

## Candidate addition 4: Trust accumulation / erosion across repeated protocol use

Does repeated use build or erode the human's willingness to delegate?

**Operationalizability.** Low in principle, weakly from transcripts.

Instruments:
- *Autonomy-grant language trend over time.* Does the user grant broader autonomy in later sessions than earlier? Detectable across the corpus timeline.
- *Scope of agent autonomous runs over time.* Does the user sanction longer/more-tool-heavy runs in later sessions? Already in the context-switch lens data.
- *Human-preempted decisions over time.* If the human preempts more as time goes on, trust is eroding.

**Signal source.** Transcript-only across the session corpus, if the corpus is longitudinal. (The 10-session corpus spans ~2 months, enough for some trend signal.)

**Proxies.**
- *Autonomy-grant latency* (criterion 4 variant) as a function of session timestamp.
- *Human-turn count per session* over time (declining count can signal growing trust or disengagement -- confounded).
- *Corrections-per-session trend* over time.

**Within-session vs cross-session.** Cross-session inherently.

**Ceiling/floor effects.** A protocol with an initial trust-honeymoon will show erosion no matter what; a protocol that starts low and stays low trivially has "stable trust" (bad interpretation). Needs baseline.

**Interpretation dependencies.** Trust moves on long timescales; short-corpus measurements are noisy.

**Net assessment.** Probably the hardest to measure well. Good as a qualitative framing; unreliable as a quantitative criterion from 10 sessions.

---

## Candidate addition 5: Downstream maintainability signals

Does the plan produce work that's easy to modify later?

**Operationalizability.** Low without implementation artifacts. Same signal-source challenges as secondary criterion 1 (downstream quality).

Instruments:
- *Churn rate on files produced from the plan* over some later window.
- *Time-to-change for a new requirement affecting the planned system.*
- *Rework count on core structures* from the plan.

**Signal source.** Outcome-linked, expensive. Needs git joins.

**Proxies.**
- *Test coverage of produced code* (if tests are in the repo).
- *Module boundary stability* (module interface diff over time).

**Within-session vs cross-session.** Cross-session.

**Ceiling/floor effects.** A protocol that produces over-engineered plans will score well on short-term maintainability and badly on cost. A protocol that produces minimal plans will score badly on maintainability but well on cost. Trade-offs.

**Interpretation dependencies.** Maintainability is hard to attribute to the plan vs. the implementer vs. the domain.

**Net assessment.** Meaningful but too far from planning-protocol variation to be a primary criterion. Best kept as a long-run sanity check, not a session-scored criterion.

---

## Candidate addition 6: Transferability

Does the protocol work across task types (founding, refinement, bug, investigation) or is it task-specific?

**Operationalizability.** Partial. Requires protocol-typed and task-typed sessions. Task type is inferable from the session (founding = new project, refinement = existing spec, bug = failure-report opener, investigation = research). The Phase 1 corpus already has some type variation (founding = 13493c8d; refinement = 38415843, f588ff0c; etc.).

Instruments:
- *Per-protocol, per-task-type performance* on primary criteria (1, 3). Requires matched task-type coverage per protocol.
- *Variance of primary-criterion scores* across task types for a given protocol. Low variance = high transferability.

**Signal source.** Transcript-only, but requires many sessions to fill the protocol x task-type matrix. The current 10-session corpus is too sparse.

**Proxies.**
- Extrapolation from 2-3 task-type comparisons if available.
- External-source analogies (incident command transfers across incident types; SBAR transfers across handoff types).

**Within-session vs cross-session.** Cross-session inherently.

**Ceiling/floor effects.** A protocol that scores equally mediocre across all task types will trivially look "transferable." Needs absolute floor.

**Interpretation dependencies.** Whether different task types *should* use different protocols. The research hypothesis is that they should. If true, transferability of a single protocol is a weaker desideratum than knowing the *map* of when-to-use-what.

**Net assessment.** Valuable framing but not a single-number criterion. Best treated as a requirement-on-the-recommendations-map rather than a per-protocol score.

---

## Candidate addition 7: Stability to human fuzziness

Does the protocol still work when the human's own intent is fuzzy?

**Operationalizability.** Moderate with careful instrumentation. Fuzziness is a property of early human turns: low-commitment language, uncertainty markers, exploratory framing. Detectable lexically ("I'm not sure", "maybe", "either way", "I haven't decided", open-ended framings).

Instruments:
- *Fuzziness-index of opening turns* (lexical indicators in turns 1-3).
- *Per-fuzziness-bucket performance on primary criteria.* Do protocols degrade at high fuzziness?
- *Fuzziness reduction over session* -- does the protocol help the human's intent firm up, or does it force premature commitment?

**Signal source.** Transcript-only. Fuzziness indicators are lexically detectable.

**Proxies.**
- *Use of hedging language* ("maybe", "I think", "I'm not sure").
- *Question-asking by the human* (human asking agent for options when they don't know what they want).
- *Scope-expansion turn count* (fuzzy-intent sessions expand scope as the human's intent firms up).

**Within-session vs cross-session.** Within-session. Cross-session comparison is needed to compare protocols at matched fuzziness levels.

**Ceiling/floor effects.** A protocol that collapses fuzziness by forcing the human to commit early scores well on "fuzziness at end" but may produce regret later. Needs pairing with regret/quality signals.

**Interpretation dependencies.** Fuzzy sessions may be *appropriate* (exploratory) -- reducing fuzziness prematurely is a failure mode, not a success. The interpretation depends on whether the session was meant to explore.

**Net assessment.** Probably the most tractable candidate addition. The lexical fuzziness signal is cheap and the session stratification is straightforward. Worth elevating to a measured criterion.

---

## Summary tables

### Cost of measurement

| Tier | Criterion | Instrument | Expense |
|---|---|---|---|
| **Cheap (transcript-only, scriptable)** | Human writing effort | Character count + category-tag | $ |
| | Wasted-question count | Lexicon on human responses ("whatever", "your discretion") | $ |
| | Over-ask count | Lexicon + topic classifier (trivia taxonomy) | $ |
| | Correction-cycle count | Lexicon on human turn openers + hand/LLM classifier | $ |
| | Context-switch gap stats | Inter-turn timestamps | $ |
| | Wall-clock time | First-last timestamp | $ |
| | Fuzziness-index of opening | Lexical hedging detector | $ |
| | Framing-correction count | Pattern on redirection + framing tokens | $ |
| | Question count / targeting rate | Agent-turn question detector + classifier | $$ |
| | Paraphrase-accuracy rate | Paraphrase detector on agent turns, correction detector on follow-up | $$ |
| **Expensive (cross-session or outcome-joined)** | Spec-revision rate (regret proxy) | Git log joins | $$ |
| | Trust trend (autonomy-grant latency over time) | Timeline of per-session autonomy-grant events | $$ |
| | Transferability | Protocol x task-type matrix; needs many sessions | $$$ |
| | Downstream implementation quality | Bug tracker / git churn joins | $$$ |
| | Spec completeness | Checklist against locked-decision list + implementer-time-to-blocker | $$$ |
| | Maintainability | File-churn / rework analysis over months | $$$ |
| **Qualitative-only (unmeasurable cleanly)** | Mental-model transfer quality (true) | Can only proxy via paraphrase/correction-depth | -- |
| | Post-implementation regret (true) | Without self-report, regret is invisible; proxies are weak | -- |
| | Robustness to user state | State is unobservable from transcripts | -- |

### Likely trade-offs

Pairs where optimizing one tends to pessimize the other, based on Phase 1 evidence and structural reasoning:

| Pair | Mechanism |
|---|---|
| Human writing effort (cheap) vs Correction-cycle count (cheap) | Context-dump (13493c8d) -- sink cost upfront, zero corrections; dialog -- cheap per-turn, many corrections. |
| Context-switch gap vs Correction-cycle count | Long autonomous runs create gaps but give the agent more space to drift before a correction. |
| Agent autonomy on trivia vs Correction-cycle count | High trivia-autonomy means the agent sometimes misclassifies architectural-as-trivial and the human catches it in a correction. |
| Targeting of agent questions vs Human writing effort | Sharper question targeting means fewer wasted questions but may mean longer, more architectural questions that produce longer responses. |
| Spec completeness vs Wall-clock time | Exhaustive specs take longer. |
| Stability to fuzziness vs Correction-cycle count | Protocols that resist premature commitment allow fuzziness to persist, which can manifest as more within-session corrections. |
| Autonomy (trivia + stretches) vs Mental-model transfer quality | Long autonomous runs give the agent less opportunity to check its model against the human; errors compound. |

### Likely correlations

Pairs where measuring one is a reasonable proxy for the other:

| Pair | Mechanism |
|---|---|
| Human writing effort & Correction characters | Much of writing effort *is* correction writing in long sessions. |
| Wasted-question count & Over-ask count | Same phenomenon from two sides (agent asks trivia / human waves off). |
| Framing-correction count & Mental-model transfer failures | Framing corrections *are* evidence of model mismatch. |
| Context-switch gap & Dispatch-directive count | Long gaps are preceded by dispatch language; count-one, get-other. |
| Spec-revision rate & Post-implementation regret | Spec rewrites are operational evidence of regret. |
| Fuzziness-index & Scope-expansion count | Fuzzy-intent sessions expand scope more; same underlying phenomenon. |
| Corrections-per-session trend & Trust accumulation | Declining corrections over time = growing mutual model = growing trust. |

### Pairs that must be paired (not-either-alone)

These metrics are each individually gameable; they must be reported as a pair to mean anything:

- Human writing effort **AND** an alignment metric (correction count or outcome signal). Silence is cheap but useless.
- Correction-cycle count **AND** an outcome signal. Zero-correction sessions can be zero-dialogue sessions that ship wrong plans.
- Agent autonomy on trivia **AND** mental-model transfer quality. Autonomy without accuracy is drift.
- Context-switch gap **AND** alignment metric. Long gaps without alignment checks are error-compounding.
- Wall-clock time **AND** spec completeness. Fast-and-bad isn't a win.

---

## Recommendations

### Keep (cheap, direct, Phase-2-usable)

1. **Human writing effort**, decomposed by category (a-h from writing-load lens). The raw total is noisy but the decomposition (especially correction-character-share and wasted-question writing) is directly diagnostic.
2. **Correction-cycle count**, split into framing-corrections, content-corrections, and same-thread repeated corrections. Phase 1 already showed these are separable and have different costs.
3. **Over-ask count / Wasted-question count** as the operational form of "targeting of agent questions" and "agent autonomy on trivia." Both criteria collapse onto variants of this measurement; the raw criteria names are less useful than this metric.
4. **Context-switch gap statistics** (median gap, long-gap count, short-ping-pong count) as the operational form of "human context-switch load." Keep the ping-pong counter as the corrosive-pattern indicator.
5. **Wall-clock time** as a cheap guardrail; do not make it primary.

### Replace with proxies

6. **Targeting of agent questions** → *Wasted-question count* + *Architectural-question count*. Two numbers are more diagnostic than a single "targeting score."
7. **Agent autonomy on trivia** → *Over-ask count* + *Autonomy-grant-latency*. Two numbers again; one for over-asking, one for how fast the human felt the need to grant blanket autonomy.
8. **Quality of mental-model transfer** → *Framing-correction count* + *Paraphrase-accuracy rate* (transcript-only instruments). The "true" criterion is unmeasurable; these are the best transcript proxies.
9. **Post-implementation regret** → *Spec-revision-within-30-days rate* joined via git. Cheapest real-world instrument of the concept.

### Elevate (from candidate additions)

10. **Stability to human fuzziness** → measured via opening-turn fuzziness-index and per-fuzziness-bucket primary-criterion performance. Cheap, transcript-only, and the concept maps cleanly onto the protocol-choice question.
11. **Transferability**, but as a *recommendation-map requirement* rather than a per-protocol score: Phase 2 delivers a map of when-to-use-what; transferability becomes "does the map cover the observed task types." Not a per-protocol metric.

### Demote

12. **Downstream implementation quality** → keep as a must-not-regress sanity check, not a session-scored criterion. Measurement is too expensive and too noisy for ranking protocols.
13. **Spec completeness at implementation-start** → same as above. Use implementer-time-to-first-blocker and post-implementation-spec-revision as the *only* tracked signals, and only for outlier detection.
14. **Trust accumulation** → demote to qualitative overlay. Longitudinal trust measurement on 10 sessions is under-powered; treat any findings as illustrative, not conclusive.
15. **Robustness to user state** → demote to qualitative overlay with a natural-experiment component using session timestamps. Do not try to score it per-protocol.
16. **Downstream maintainability** → demote. Too far from planning-protocol variation; dominated by implementation-quality confounds.

### Pairing requirements

When reporting a protocol's performance, *always* pair:
- writing effort + correction rate
- autonomy + mental-model transfer (or its proxy)
- context-switch gap + alignment metric
- wall-clock + completeness proxy

Single-number protocol scores are misleading on every criterion in this set.

### Candidate formal evaluation framework

The transcript-only instruments (1-5, 6-11 above) can be combined into an **evaluation harness** that runs over any planning session and emits a fixed panel of numbers: `[writing_chars_by_category, correction_count_by_subtype, overask_count, wasted_question_count, gap_stats, ping_pong_count, wall_clock, fuzziness_index, paraphrase_accuracy, architectural_question_count]`. The harness is scriptable in Python over the existing `extract_dialog.py` output. Building it is within the "scripts in service of extraction/analysis" allowance and would make cross-session protocol comparison reproducible. This framework itself may be a Phase 2 deliverable on par with protocol recommendations.

### Escalation flag

None of the audits in this document surface an issue that calls for pausing Phase 2 before Step 2. The provisional criteria set is broadly measurable; the refinements above are replacements-by-proxy and decompositions, not fundamental rejections. Phase 2 may proceed to Step 2 (external-source pass).
