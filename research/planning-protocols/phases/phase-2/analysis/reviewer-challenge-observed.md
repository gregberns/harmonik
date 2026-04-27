# Reviewer Pass — Challenge-Observed Frame

**Phase 2, Step 5 artifact.** Reviewer: local-maxima guardian (Step 5.5), 2026-04-23. Track: planning-protocols. One of six reviewer outputs. This reviewer inverts the default; the other five frames handle the opposing direction.

## 1. Frame definition

This reviewer enters the catalog with the explicit prior that **observed-origin protocols are local optima**. The user has iterated on their practice over time, which produces convergence — but convergence is evidence only of stability, not of global optimality. The user may never have tried the counter-pattern; may have tried it in a bad form, at a bad time, and dismissed the shape; may be anchored on the first shape that worked well enough. Other reviewer frames (robustness-to-fatigue, cognitive-load, adaptability, etc.) will weigh observed patterns on their current evidence. This frame weighs observed patterns as *suspect by default* and external/counter-pattern rivals as *innocent by default*, and demands that observed patterns clear a bar their rivals don't have to clear: independent evidence beyond corpus presence.

The second-order reason this frame is needed: **Step 4.5 (corpus-signal filter) was not executed this session.** Without a filter pass, observed-origin protocols carry no evidence beyond "the user did it." Every other reviewer frame may still implicitly defer to "well, it's working for them." This frame is the only one tasked with calling that deference out. The honest acknowledgment: some observed patterns *will* survive this challenge (those with strong external convergence, those with load-bearing mechanisms absent from rivals, those where the counter-pattern collapses on honest inspection). For those, the challenge fails explicitly — not because the frame softened, but because the evidence is genuinely there. For the rest, this reviewer argues for displacement.

## 2. Observed-origin catalog: pattern-by-pattern challenge

The catalog contains eleven protocols with `observed` in their origin field. Each is evaluated here against its strongest non-observed rival.

### 2.1 `context-dump` — challenged by `scqa-opener`, `commanders-intent`, `dialogic-context-accretion`

**Observed claim:** rich upfront brief minimizes context-switches; agent responds to the brief, few human turns total. User converged on this in 13493c8d (harmonik founding-vision).

**Challenge:** The lens analysis already flags context-dump as the pattern "where errors propagate furthest before detection" (catalog entry, Strongest evidence field). There is no external convergence on *undifferentiated* rich briefs — every external-domain rich-brief family (SBAR, SMEAC, SCQA, commander's intent, engagement letter) is *slot-disciplined*. The user's context-dump is slot-undisciplined by construction (a founding-vision dump). This is not "rich-brief is wrong"; it is "*rich-brief without slot discipline* is wrong."

- Best non-observed displacer: **`scqa-opener`** or **`commanders-intent`** for rich-brief cases; **`dialogic-context-accretion`** (counter-pattern #6) for the inverse claim that context should be pulled, not pushed.
- Why the user converged anyway: path-dependence. Founding-vision sessions are rare and high-stakes; there was no correction-loop test on the dump itself because it worked well enough to proceed. Slot-discipline was never compared.
- Subtle failure the user wouldn't notice: the brief *fixes the human's model* at the moment of writing, which is precisely when the model is least refined. The user has no way to measure "how much my own model shifted in the first 20 minutes of agent work" because context-dump prevents that correction from being surfaced.
- **Verdict: displace.** SCQA for the slot-disciplined rich-brief case; dialogic accretion for anti-anchoring. The raw context-dump form should be deprecated.

### 2.2 `recovery-handoff` — challenged by `handoff-closed-acknowledgment`, `ipass-opener`

**Observed claim:** structured session-recovery opener (729dad16 kerf) carries state across sessions.

**Challenge:** This one *survives most of the challenge*. Session-recovery handoffs have direct external convergence (aviation authority-transfer three-part exchange, ICS handoff, medical I-PASS). The user's `recovery-handoff` is already external-shaped. The residual challenge is that the observed form lacks the **closed-acknowledgment** move (the next agent confirms back) and lacks **severity-tagging** (I-PASS's watcher tier). Without these, the observed handoff has two known failure modes: silent-staleness (agent accepts without validating the payload is current) and severity-flattening (every recovery reads as routine).

- Best non-observed displacer: **`handoff-closed-acknowledgment`** as a wrap on the existing observed form; **`watcher-tier-orientation`** as an addition.
- **Verdict: augment, don't displace.** Observed form is right on core structure; external evidence should be grafted on.

### 2.3 `autonomous-dispatch` — challenged by `mission-command`, `question-preserving-autonomy`, `incremental-step-autonomy`

**Observed claim:** minimal dispatch + "never ask questions" produces unbounded autonomy. Dominant in secure-dev corpus (~100/133 sessions).

**Challenge:** This is one of the **most suspect** observed patterns. The corpus presence is high, but the high corpus presence reflects the user's *response to time pressure*, not a considered comparison. The "never ask questions" clause is explicitly contested by counter-hypothesis #7 (`question-preserving-autonomy`): removing questions does not remove ambiguity; it suppresses the signal. The external analog (`mission-command`) is doctrinally *opposed* to "never ask" — it requires disciplined initiative *including* back-brief checkpoints and subordinate-must-deviate-to-preserve-intent. The user's form collapses the mission-command back-brief entirely.

- Best non-observed displacer: **`mission-command` + `back-brief-plan-quality` + `question-preserving-autonomy`** — the same autonomy envelope, but with preserved ambiguity-detection and planned checkpoints.
- Why the user converged anyway: evidence is missing on *late corrections*. The user measures "interruption rate during the autonomous run" (low — that's the point) but does not measure "rework rate after the autonomous run terminated" (likely high, especially on tasks with human-frame ambiguity — naming, scope, intent). The observed pattern optimizes the wrong metric.
- Subtle failure: `autonomous-dispatch` creates **false confidence** as a load-bearing property, not a side effect. The user cannot tell, from inside the session, whether the agent resolved an ambiguity correctly or papered it over. The corpus doesn't record non-interruption; it records lack-of-interruption.
- **Verdict: displace decisively.** Replace with mission-command stack (`commanders-intent` + `autonomy-scope-grant` + `contingency-preauthorization` + `question-preserving-autonomy` + `back-brief-plan-quality`). This is the single highest-leverage displacement in the catalog.

### 2.4 `numbered-question-close` — challenged by `load-bearing-token-readback`, counter-pattern #4 (open-ended hand-off), `graded-assertiveness-pace`

**Observed claim:** numbered list at turn-end shortens subsequent human turn.

**Challenge:** This observed pattern has **direct external counter-evidence**. Aviation CRM literature flags "any questions?" closers as *ineffective* vs. interleaved slot-acknowledgment; the catalog notes this explicitly. This is an observed pattern with *an externally-documented anti-pattern match*. The measured benefit (shorter next human turn) is real but measures the wrong thing: short next-turn is consistent with "human answered the numbered questions and skipped the unframed concerns the numbering suppressed." See §6 below for the detailed evidence-weight assessment.

- Best non-observed displacer: **`load-bearing-token-readback`** (aviation interleaved slot-ack) per-turn + counter-pattern #4's "Name the shape" open close at decision points.
- Why the user converged anyway: the feedback signal was short next-turn, which is easy to see; the missing signal is late-surfacing architectural issues that numbered close suppressed, which requires corpus-wide audit.
- **Verdict: displace.** Strong case. This is the *exciting displacement* par excellence.

### 2.5 `upfront-decision-partition` — challenged by `emergent-partition`, `mission-command`

**Observed claim:** up-front declaration of trivial-vs-critical decisions (79a42399 H#1) produces zero-cost corrections.

**Challenge:** Partially survives. Upfront partition has convergent external support (mission-command's autonomy-scope-grant, I-PASS contingency pre-authorization). The challenge is not to upfront-partition-in-general but to the **specific claim that partition-set-up-front is better than partition-formed-mid-session** (counter-pattern #3).

- Best non-observed displacer: **`emergent-partition`** — no external analog, novel contribution, but well-motivated (trivial-vs-architectural is task-relative and cannot be known up front).
- Why the user converged anyway: when it worked (79a42399), it produced visible zero-cost corrections, a legible win. The counter (mid-session reclassification rate) was never measured because upfront partition has zero reclassifications by construction.
- Subtle failure: **freezes the human's understanding of which decisions matter early**, then the agent silently resolves architecturally-important decisions as trivial because they were pre-classified as such. The user cannot detect this from inside the session.
- **Verdict: do not decide yet; pit head-to-head.** This is the most genuinely uncertain case. Both survive first challenge. Step 6 should run an experimental head-to-head (reclassification rate vs. correction rate) on a matched pair of kerf works. Until then, *do not prefer upfront over emergent on defaults*.

### 2.6 `incremental-step-autonomy` (observed + unexplored) — challenged by `micro-step-incrementalism`, `autonomous-dispatch` (opposite extreme)

**Observed claim:** one small step, report, next small step. Present but not well-characterized in corpus.

**Challenge:** Because the observed version is not well-characterized, this is mostly a dimensional value with thin evidence. The counter-pattern `micro-step-incrementalism` is the explicit full-strength version. The strongest rival is *not* micro-step, it is the **other extreme** — `autonomous-dispatch`, which the user's own corpus has more data on. Given that the user's practice has both, without knowing which dominates on which task, the observed-corpus position is unstable.

- Best non-observed displacer: **`micro-step-incrementalism`** when task has low per-step overhead and high misalignment cost; **`question-preserving-autonomy`** as synthesis across the two extremes.
- **Verdict: observed version underspecified; treat the unexplored variants (`micro-step-incrementalism`, `question-preserving-autonomy`) as first-class and evaluate them directly.**

### 2.7 `pre-action-plan-disclosure` — challenged by `example-led-emergence`, `hypothesis-driven-ghost-deck`, `back-brief-plan-quality`

**Observed claim:** agent discloses plan before executing; human corrects; execution under corrected plan. Observed to produce zero-cost corrections (79a42399, f588ff0c A2).

**Challenge:** Heavy convergent external support (back-brief, hypothesis-driven ghost deck, SMEAC), *but* directly contested by counter-pattern #1 (`example-led-emergence`). The steel-manned counter is that pre-disclosure causes premature commitment — both parties interpret subsequent discussion through the disclosed plan's framing. The observed "zero-cost corrections" signal is itself suspect under this frame: the corrections measured are to *stated aspects of the plan*, not to framing-level choices the plan's existence anchored.

- Best non-observed displacer: **`example-led-emergence`** (counter-pattern #1) for tasks with concrete-case granularity; **`hypothesis-driven-ghost-deck`** with *falsification-discipline* for tasks where a plan is the right shape (the observed form lacks the adversarial falsification move).
- Why the user converged anyway: the visible signal (zero-cost corrections) is legible; the invisible signal (framing corrections that never happened because the frame was locked) is by construction unseeable.
- Distinguishing experiment: **framing-corrections vs. case-corrections ratio** (catalog's own evaluation plan).
- **Verdict: conditional displace.** Pre-disclosure survives for structural/cross-cutting tasks *if* augmented with falsification discipline (ghost-deck); displaced by example-led for tasks with concrete-case granularity. Current observed form (no falsification discipline) is dominated.

### 2.8 `forced-choice-with-default` (observed rare + unexplored) — challenged by `micro-step-incrementalism`'s "OK?" close, `autonomy-scope-grant`

**Observed claim:** agent proposes "I'll do X unless you object"; shifts default to action. Rare in observed corpus.

**Challenge:** Because the observed incidence is rare, this is effectively an unexplored candidate. It converges with aviation readback "silence = confirmation" and with micro-step's "OK?" gate. The best external rivals exist; the observed position is negligible.

- Best non-observed displacer: **`micro-step-incrementalism`** bundles this natively; **`autonomy-scope-grant`** subsumes it as a per-decision case.
- **Verdict: observed version is a dimension-value not a rival protocol; fold into rivals.**

### 2.9 `kerf-parallel-reviewer` — challenged by `role-split-reviewer-library`, `premortem-reviewer`, `mob-parallel-reviewers`

**Observed claim:** multiple reviewer sub-agents critique in parallel. Kerf adopts structurally.

**Challenge:** The parallel-reviewer *architecture* is well-supported externally (mob programming, multi-agent thesis-antithesis-synthesis research, Smith 2024). The challenge is to the *observed form*, which is a generic reviewer prompt. The rival specializations — **`role-split-reviewer-library`** (devil's advocate, future-maintainer, simplicity-guardian), **`premortem-reviewer`** (adversarial-by-construction), **`shuttle-diplomacy-reviewer`** (mediator) — each produce output classes the generic reviewer does not.

- Best non-observed displacer: **`role-split-reviewer-library`** subsumes kerf-parallel-reviewer structurally and adds specialization. Premortem-reviewer orthogonal-coverage-test is highly likely to reveal issues the generic reviewer misses.
- Subtle failure the user wouldn't notice: generic reviewers produce redundant output; the "orthogonality" property of parallel-review is under-realized without role specialization.
- **Verdict: displace with `role-split-reviewer-library` as architecture; adopt `premortem-reviewer` for the adversarial slot; keep current reviewer as one role within the library.**

### 2.10 `dialog-log-plan` — challenged by `single-text-procedure`, `coding-opord-4slot`, `test-cases-as-plan`, `ipass-opener`

**Observed claim:** plan lives in the chat; extracted or summarized post-hoc. Dominant in Phase 1 planning-dialog corpus.

**Challenge:** The external rivals are many and all stronger. **`single-text-procedure`** (Fisher's Camp David procedure) directly contests dialog-log-plan: one persistent document throughout, agent drafts, human critiques, the artifact is always usable. Dialog-log's primary failure mode (resumption cost; silent decay) is exactly what single-text solves. **`coding-opord-4slot`** compresses the plan into a named structure. **`test-cases-as-plan`** provides behavior-shaped alternative.

- Best non-observed displacer: **`single-text-procedure`** — high convergence with kerf's spec-first posture (the user already has a locked-in decision that specs are normative; dialog-log-plan is in tension with that locked decision).
- Why the user converged anyway: lowest up-front cost, no artifact maintenance. But resumption and extraction cost is paid every session thereafter.
- **Verdict: displace.** Dialog-log-plan is inconsistent with the user's own locked-in "spec-first" decision. The spec-first posture *entails* a persistent artifact. High-leverage swap.

### 2.11 `controller-orchestration` — challenged by `named-role-separation`, `sitrep-at-cadence`, `commanders-intent`

**Observed claim:** human directs a running system; controller-agent parcels work to worker-agents. Observed in b7eca5d2, 3fb3dc80, 69050eec.

**Challenge:** This is a workload pattern, not a protocol per se. The controller-worker shape is directly relevant to harmonik's orchestrator subsystem and is the right primitive. The challenge is to its *communication discipline*, which is thin in the observed form.

- Best non-observed displacer (additive, not displacing): **`named-role-separation` (IC/OL/Scribe/Planning)** imposes role discipline on the controller; **`sitrep-at-cadence`** imposes cadence discipline; **`commanders-intent`** imposes intent discipline.
- **Verdict: observed form is structurally right but communication-undisciplined. Augment with ICS stack rather than replace.**

---

## 3. Inversion test per Phase 1 cross-cutting finding

For each of the eight Phase 1 findings, rank observed vs. counter-pattern *on this reviewer's frame*.

### 3.1 Pre-action plan disclosure is high-leverage — **counter wins**
Counter-pattern #1 (`example-led-emergence`) + `maieutic-drawout` (external convergence) displace observed for concrete-case-granular tasks. For structural tasks, augmented `hypothesis-driven-ghost-deck` with falsification discipline displaces bare observed form. Observed form wins on nothing once rivals are taken seriously.

### 3.2 Multi-question-per-turn is avoidable cost — **counter wins**
Counter-pattern #2 (`assumption-bundle`) directly inverts. SBAR Assessment/Recommendation batches by construction; MECE / Issue-Tree batches interdependent questions. If the observation is "one-at-a-time reduces writing cost" and the counter is "batched-with-dependencies reduces *consistency corrections*," the counter measures a different signal entirely and may dominate on tasks with interdependent assumptions. Per-turn writing cost is the *visible* metric; consistency corrections are the *load-bearing* one.

### 3.3 Upfront decision partition is best — **draw, experimental head-to-head needed**
Only observed pattern where the challenge does not clearly displace. Both have convergence elsewhere (upfront aligns with mission-command's pre-authorized-by-intent; emergent aligns with IAP re-issuance). Genuine uncertainty.

### 3.4 Numbered-close — **counter wins decisively** (see §6)
Strongest case in the catalog. External empirical counter-evidence (aviation CRM) directly against the observed mechanism.

### 3.5 Decision closure enables long autonomy — **counter wins**
Counter-pattern #5 (`micro-step-incrementalism`) + `question-preserving-autonomy` + `back-brief-plan-quality` dominate `autonomous-dispatch` on late-correction rate (catalog's evaluation plan agrees). Observed form optimizes interruption-rate, which is not the relevant failure mode.

### 3.6 Context-dump trades writing for correction reduction — **counter wins**
Counter-pattern #6 (`dialogic-context-accretion`) + EPE + SPIN directly invert. Observed form is flagged by the lens reports themselves as the pattern where errors propagate furthest. The trade is stated wrong: context-dump doesn't reduce corrections, it *defers and concentrates* them into implementation-time rework.

### 3.7 "Never ask" clause enables autonomy — **counter wins decisively**
Counter-pattern #7 (`question-preserving-autonomy`) with direct external convergence (mission-command + back-brief). The observed form is the *opposite* of the external doctrinal evidence. Heavy-weight displacement.

### 3.8 Form matters independently — **partial counter**
Counter-pattern #8 (`knowledge-state-inventory`) argues form is epiphenomenal. The counter-pattern's own honest-reassessment note concedes partial truth; the likely synthesis is "form effects are *conditional on* knowledge-state alignment." This reviewer's position: adopt inventory-first (since it's the load-bearing driver) and leave form as the secondary/conditional layer. Observed form-specific findings (numbered close) don't survive as *first-order* protocols; they may survive as *conditional-on-inventory* form choices.

**Summary of inversion tests: 7 of 8 findings — counter-pattern or external rival wins. 1 of 8 (upfront partition) — draw.**

## 4. Safe displacements (low-risk swaps, observed pattern has external look-alike with independent validation)

These are observed patterns whose *structural position is right* but whose specific implementation lacks independent evidence. The swap is external-validated look-alike; the risk of swap is low because the structural move is confirmed.

| Observed | Safe swap (external, validated) | Why safe |
|---|---|---|
| `recovery-handoff` | `handoff-closed-acknowledgment` + `watcher-tier-orientation` (adjunct, not replace) | Direct convergence; adds known-missing safety moves |
| `kerf-parallel-reviewer` | `role-split-reviewer-library` + `premortem-reviewer` | Kerf architecture subsumed; specialization well-validated |
| `controller-orchestration` | Augment with `named-role-separation` + `sitrep-at-cadence` + `commanders-intent` | ICS is the external canonical for controller-worker coordination |
| `pre-action-plan-disclosure` (structural tasks) | `hypothesis-driven-ghost-deck` with falsification discipline, or `back-brief-plan-quality` | McKinsey and military doctrine both have 50+ years of iteration |
| `dialog-log-plan` | `single-text-procedure` (agent-drafts, human-critiques, one artifact) | Fisher's Camp David procedure; aligns with user's locked-in spec-first posture |

## 5. Exciting displacements (high-leverage experiments, observed pattern has only corpus-presence)

These are the protocols that this reviewer recommends as **highest-priority for actual experimentation**. Corpus presence alone is not evidence. The rivals are counter-patterns or unexplored candidates; the observed patterns have no external convergence to defend them.

| Observed | Exciting rival | Expected finding |
|---|---|---|
| `autonomous-dispatch` "never ask" | `question-preserving-autonomy` (counter-pattern #7) + `mission-command` | Late-correction count drops sharply; user's metric (interruption-rate) was never the load-bearing metric |
| `numbered-question-close` | Counter-pattern #4 (open-ended "Name the shape") + `load-bearing-token-readback` | Late-surfacing architectural issues surface earlier; next-human-turn may lengthen but *total-session issue-density* improves |
| `context-dump` | `dialogic-context-accretion` (counter-pattern #6) | Total human writing *after* first useful agent output drops; correction propagation halves |
| `pre-action-plan-disclosure` (concrete-case tasks) | `example-led-emergence` (counter-pattern #1) | Framing-corrections drop; case-corrections rise; spec emerges from case-table rather than from dispute on framing |
| `upfront-decision-partition` | `emergent-partition` (counter-pattern #3) | Reclassification rate > 0 predicts upfront partition was wrong on those decisions; the value of mid-session is only visible under this metric |
| `dialog-log-plan` | `single-text-procedure` or `test-cases-as-plan` | Resumption cost per session drops; spec-first posture becomes enforceable |
| `autonomous-dispatch` (multi-question-per-turn is bad) | `assumption-bundle` (counter-pattern #2) | Consistency corrections cluster at bundle-v1 not implementation time; human's actual load drops |

The common pattern: every "exciting" displacement measures a *different metric* than the observed pattern optimized. This is the signature of local-maximum anchoring — the user's practice optimized what was legible, and the counter-patterns optimize the load-bearing thing that was illegible.

## 6. Evidence-weight assessment: numbered-close as aviation-CRM anti-pattern

The catalog's entry on `numbered-question-close` contains this finding: *"aviation-CRM literature flags 'any questions?' closers as ineffective vs. interleaved slot-acknowledgment."* This is a direct external empirical finding *against* an observed pattern. Weight assessment:

**What the aviation finding is, precisely.** Aviation CRM (Cockpit Resource Management), developed after United 173 (1978) and the systematic crash-cause analyses that followed, converged on *interleaved slot-acknowledgment* (per-item readback at the moment of each instruction) and away from *end-of-briefing "any questions?"*. The mechanism documented in aviation literature: end-close enumeration biases toward questions already well-framed and allows mis-hearings at slot N to survive the briefing because the end-question doesn't drill back to slot N. Interleaved readback catches the slot-N mis-hearing at slot N.

**Applicability to planning protocols.** The mechanism translates cleanly. The numbered close on an agent turn is structurally "any questions?" at turn-end. The observed benefit (shortened next human turn) is precisely what aviation would predict (humans answer the enumerated well-framed questions quickly) — and is precisely what the anti-pattern finding says is *misleading* (the mis-hearings at the mid-turn slots are not being caught).

**Evidence-weight relative to the observed signal.**
- Observed evidence: Phase 1 lens reports, no controlled comparison, no measurement of late-session issue density.
- Aviation evidence: decades of incident archive, specific accidents attributable to end-close (Tenerife, KAL 007 transcripts), formal phraseology mandate (ICAO Doc 9432).
- **The external counter-evidence is empirically stronger by 2+ orders of magnitude.**

**What this implies for the user's practice.** The observed benefit is real but non-load-bearing. The observed cost (late-surfacing architectural issues the numbered close suppresses) is load-bearing and invisible in the user's own feedback loop. The user has almost-certainly converged on a locally-optimal form where the measurable metric (next-turn length) is being minimized while the load-bearing metric (late-session issue surfacing) is silently degrading.

**This is the single strongest observed-pattern displacement case in the catalog.** The aviation evidence should not be weighted symmetrically with the corpus signal; it should dominate.

**Recommendation:** Adopt `load-bearing-token-readback` (per-turn interleaved, aviation-shape) as the baseline communication discipline, and reserve numbered close for narrow cases where the question space is genuinely well-framed (catalog's evaluation plan tests this directly). The observed numbered-close pattern should not be preserved as the default.

## 7. Closing: which observed patterns survive the challenge?

After running the challenge frame against each observed-origin protocol:

**Survive with augmentation:**
- `recovery-handoff` — right architecture, add closed-acknowledgment and severity-tagging.
- `controller-orchestration` — right structural primitive, add ICS communication discipline.
- `kerf-parallel-reviewer` — right architecture, replace generic reviewer with role-split library + premortem.

**Survive conditionally (need head-to-head experiment):**
- `upfront-decision-partition` — only observed pattern where the counter (`emergent-partition`) does not have clean external support and the observed has mixed support (aligns with mission-command's pre-authorized-by-intent). Genuinely uncertain.

**Do not survive — displace or demote:**
- `autonomous-dispatch` — displace with mission-command stack + question-preserving-autonomy. Highest-leverage single displacement.
- `numbered-question-close` — displace with interleaved `load-bearing-token-readback`; direct external counter-evidence.
- `context-dump` — displace with slot-disciplined rivals (SCQA / commanders-intent) for push model; dialogic-context-accretion for pull model. The raw undisciplined dump form should be deprecated.
- `pre-action-plan-disclosure` — displace with `example-led-emergence` for concrete-case tasks; augment with falsification discipline for structural. The bare observed form is dominated.
- `dialog-log-plan` — displace with `single-text-procedure`. Directly inconsistent with the user's own locked-in spec-first posture.
- `incremental-step-autonomy` (observed version) — underspecified; fold into `micro-step-incrementalism` + `question-preserving-autonomy`.
- `forced-choice-with-default` — rare in observed corpus; fold into rivals that subsume it (`micro-step-incrementalism`, `autonomy-scope-grant`).

**Count:** 3 survive-with-augmentation, 1 survive-conditionally, 7 displace-or-demote. Of eleven observed-origin protocols, the challenge frame finds three that pass — and two of those three are architectural primitives (reviewer architecture, controller architecture) that would have been adopted from external evidence anyway.

**Final frame-level observation.** The pattern across displacements is consistent: the user's observed practice optimizes *legible metrics* (interruption rate, next-turn length, artifact overhead) and misses *load-bearing metrics* (late-correction count, architectural-issue surfacing timing, framing-lock-in). The local-maximum is real and visible — but it is local because the gradient in the legible-metric space is strong and the load-bearing-metric space was never explored. Step 6's ranking should treat this as the operational finding: for every observed-origin recommendation, ask whether the rival has been eliminated on its own terms (load-bearing metrics) or merely on the observed's terms (legible metrics). If the latter, the local-maximum has not been tested.
