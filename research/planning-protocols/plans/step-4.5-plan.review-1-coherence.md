# Review: Step 4.5 Implementation Plan — Corpus-Signal Filter Harness

**Reviewers frame:** Coherence (internal consistency, specification clarity) and completeness (does it address everything framework §4 specifies?). Not evaluating whether the framework design is correct (locked position).

**Reviewed:** step-4.5-plan.md against evaluation-framework.md §3 (pair-graph criteria) and §4 (harness spec).

**Date:** 2026-04-24

---

## 1. Coverage vs Framework §4

The plan addresses **all operationalized measurements** from evaluation-framework.md §4.1–4.3. Mapping:

| Framework §4 spec | Plan section | Status |
|---|---|---|
| W1 (human writing effort, category-decomposed) | 2.9 (writing-load category tagger a–h) | ✓ Covered |
| C1 (correction-cycle count, subtyped) | 2.4 (pushback/correction-incident detector + subtyping M1) | ✓ Covered |
| A1 (over-ask count, trivial-topic) | 2.6, 2.7 (wasted-question detector + trivial-topic taxonomy) | ✓ Covered |
| A2 (autonomy-grant latency) | 2.5 (autonomy-grant lexicon) | ✓ Covered |
| M1 (framing-correction subset of C1) | 2.4 subtyping into framing/content/repeated | ✓ Covered |
| G1 (inter-turn gap statistics) | 2.10 (inter-turn gap features) | ✓ Covered |
| C2 (mid-gap alignment-check count) | 2.10 (mid_gap_checkpoint detection) | ✓ Covered |
| T1 (wall-clock time) | 2.10 (active_min, calendar_min) | ✓ Covered |
| S1 (spec-completeness proxy) | 4.1 output schema includes S1 count fields | ✓ Covered (minimal) |
| F1/F2 (fuzziness stability) | 2.3 (hedging-language lexicon + F1/F2 latency) | ✓ Covered |

**Deferrals with reason (all explicit):**
- **R1/R2 outcome-joins:** explicitly deferred to post-Step-4.5 work (§10, rationale: requires git history + repo-surface-pattern decision). Framework §4 marks these as retrospective (§6.3); step-4.5-plan correctly bounds them out.
- **NE-3 (reviewer-pattern detection) and outcome-side of NE-5:** deferred; marked as "Partial" in §5 table with justification (NE-3 requires agent-output-shape analysis beyond transcript scope; NE-5 transcript-side is included, artifact-stability requires git blame).

**Coverage judgment:** Complete against locked constraints. No framework-specified criterion is dropped without explicit deferral and stated reason.

---

## 2. Implementability per classifier (§2.1–2.12)

### 2.1 Session-type classifier — **Ready to code**
- Delegates to existing `session-type-discriminator.md` verbatim.
- Output specification clear: one of 6 tags.
- Risk: minimal. Caveat 2 (Claude Code command-capture artifacts) documented; no implementation guidance yet.

### 2.2 Opener-shape detector — **Needs-work (medium complexity, moderate clarity gap)**
- Decision tree is spec'd (lines 42–59).
- **Clarity issue:** lexicon variables invoked but not fully defined inline. Example: "autonomy_partition_lexicon (regex-joined)" — the actual regex is given as a keyword list, but the merging strategy (escaped, grouped as OR, case-insensitive?) is implicit.
- **Missing:** examples from corpus. The plan cites "calibrated from 79a42399 H#1" but doesn't show the actual H#1 text or the matching logic in context. Harder for implementer to debug false-positives.
- **Guard against:** "structured-external-template" branching is regex-matched against SBAR/SMEAC/SCQA header patterns, but the header-regex definitions are not in the plan (referenced in 2.2 but not spelled out). Implementer must locate them elsewhere or invent them.

### 2.3 Hedging-language lexicon — **Ready to code**
- Lexicon fully enumerated in four categories (lines 65–69).
- Density scoring defined: "matches per 100 words" (line 71).
- F1/F2 computation clear: F1 = density in turns 1–3; F2 = turn index where density falls below 50% of F1 for ≥ 3 turns consecutively.
- Ready to implement.

### 2.4 Pushback lexicon & correction-incident detector — **Needs-work (highest-risk classifier, guards incomplete)**
- **Complexity rated H (high) correctly.** Three-tier lexicon enumerated (lines 77–79).
- **False-positive guards partially defined:** benign-phrase blocklist given (line 82–84); "actually" mid-sentence rule sketched (line 83); hmm + agreement rule stated (line 84).
- **Missing:** How to operationalize "surrounding clause is affirmative" for the "actually" guard? Pattern for "sentence-end punctuation"? The rule is intuitive but requires regex implementation choices.
- **Missing:** The requirement that human turn be ≥ 40 chars (line 85) is stated but the ratio of false-positives this guards against is not quantified. Will the hand-validation pass (§7, line 343) test this threshold?
- **Ground-truth dependencies:** Plan references `phases/phase-1/analysis/misaligned-assumption.md` §Incident Table (lines 88, 343) for hand-labeled correction sets. That file was read; it contains incident citations (3bf5774c H#3, 3bf5774c H#4, f588ff0c H#4/5) but the incident-table structure is not visible in the plan. Implementer must have direct access to that file to extract the ground-truth labels.
- **Validation spec clear:** "hand-label correction incidents in the 10 primary-corpus sessions… Compare detector output; require precision ≥ 0.85 on the hand-labeled set before running corpus-wide" (lines 343–344). This is executable.

**Verdict:** Lexicon is ready, guards are partially ready. The hand-validation step is the gate. Implementer should proceed but expect iteration during validation.

### 2.5 Autonomy-grant lexicon — **Ready to code**
- Explicit phrase list given (lines 94–95).
- A2 definition clear: first match index or ∞ if none.
- Special case: "Session opener (H#1) with autonomy-partition shape counts as A2 = 1 even when explicit phrases aren't present" (lines 96–97). Implementable as a fallback.

### 2.6 Wasted-question detector — **Ready to code**
- Detection rule clear: agent asked ≥ 1 question AND human response is ≥ 60% autonomy-grant tokens by char count.
- Joins with trivial-topic classifier (below) to compute A1.
- No guard details, but the rule is simple enough that false-positives are unlikely.

### 2.7 Trivial-topic taxonomy — **Ready to code**
- Four buckets defined with keyword sets for each (lines 106–108).
- Keywords are enumerated explicitly, not referenced.
- A1 computation clear: count of agent questions tagged trivial in the session.
- Open question: is keyword matching case-insensitive? Plan doesn't state, but implementer will choose reasonably.

### 2.8 Numbered-question-close detector — **Ready to code (highest leverage)**
- Regex pattern explicit: `/(^|\n)\s*\d+[.)]\s+.+\?/m` (line 116).
- Five close-shape categories enumerated (lines 116–120).
- "This is the single most load-bearing classifier" — noted; output per agent turn is specified.
- **Clarity note:** "last 500 chars of agent text" is given, but is this the agent text only, or the entire turn including tool output summary? Plan doesn't clarify, though context suggests agent text only.

### 2.9 Writing-load category tagger (a–h) — **Needs-work (high complexity, rule ambiguity)**
- Categories defined (line 126); skeleton rule set given (lines 128–137).
- **Missing rule details:** 
  - Category (a) framing: "turn_index == 1 and len > 300" — why 300? Is this length of the human turn or agent turn following? The rule checks `turn_index`, implying human turn index, but framing is typically a *agent* turn feature.
  - Categories are labeled for *human* turns (per Phase 1 lens), but rule line 134 references "parent_agent_turn_had_question" — is this correctly scoped?
  - The order of rule branches (lines 129–136) appears to have overlaps: a turn matching both administrative and pushback lexicons will match (g) before (b), giving misleading categorization.
- **Ground-truth dependency:** The plan defers to `phases/phase-1/analysis/writing-load.md` for category definitions but does not re-specify them inline. Implementer must have that file and must reconcile the skeleton rule with its definitions.
- **Validation:** No hand-validation step specified for W1. The plan specifies hand-validation only for C1/M1 (lines 343–344), not for W1/category-tagging. This is a gap.

**Verdict:** The structure is there, but rule-branch ordering and scope ambiguities need clarification before hand-validation. High risk of systematic mis-categorization.

### 2.10 Inter-turn gap features — **Ready to code**
- Features enumerated clearly (lines 144–149).
- G1 output format stated: `(median, p95, long_count, ping_pong_count)` (line 151).
- Thresholds given: 600s (10 min) for long gap, 60s for ping-pong (lines 147–148).
- "mid_gap_checkpoint" detection: token list given (line 149).
- Ready to implement.

### 2.11 Autonomous-stretch detector — **Ready to code**
- Reuses existing extract_dialog.py marker `[AUTONOMOUS RUN]` (agent turn > 5 min OR > 20 tool calls).
- Output: count, longest, total autonomous minutes.
- Straightforward; ready to code.

### 2.12 Template-dispatch leakage detector — **Ready to code**
- Scoped to sessions with `opener_shape == "never-ask-template"` only.
- Leakage = human text turns after H#1.
- Triggering agent turn classified via trivial-topic + new "architectural-uncertainty" bucket.
- "Architectural-uncertainty" is not fully defined: "agent explicitly states it cannot proceed without a decision." Implementer must infer what this lexicon should match. Examples would help.

---

## 3. Internal consistency

### 3.1 Output schemas vs. classifier outputs

**Per-session row (§4.1) vs. §2 outputs:**
- W1_by_category (§4.1 line 219): expects `{"a": chars, "b": chars, ..., "h": chars}`. Classifier 2.9 specifies this output. ✓
- C1_by_subtype (§4.1 line 220): expects `{"framing": count, "content": count, "repeated": count}`. Classifier 2.4 specifies subtyping into these three. ✓
- A1 (§4.1 line 221): integer. Classifier 2.6+2.7 computes this. ✓
- A2 (§4.1 line 222): null or integer. Classifier 2.5 specifies this. ✓
- M1 (§4.1 line 223): integer. Classifier 2.4 subtyping extracts framing count. ✓
- G1 (§4.1 line 225): JSON object with median_s, p95_s, long_count, ping_pong_count. Classifier 2.10 specifies these. ✓
- C2 (§4.1 line 226): integer. Classifier 2.10 specifies mid_gap_checkpoint detection. ✓
- T1 (§4.1 lines 227): object with active_min, calendar_min. Classifier 2.10 specifies this. ✓
- S1 (§4.1 line 228): object with todo_count, decide_later_count, fixme_count. Classifier §2 does not specify how to detect these. **Gap identified below.**
- F1/F2 (§4.1 lines 230–231): float and integer. Classifier 2.3 specifies both. ✓
- Numbered-close breakdown (§4.1 lines 233–236): four fields. Classifier 2.8 specifies close_type classification. ✓
- Autonomous stretches (§4.1 line 238): array of objects. Classifier 2.11 specifies this. ✓
- Template-leakage (§4.1 line 239): null or structure. Classifier 2.12 specifies this. ✓

**S1 (spec-completeness) is under-specified:** Framework §4 defines S1 as "one of: implementer-time-to-first-blocker (if implementation has started), checklist-coverage against locked-decision-list, or count of TODO / decide-later / FIXME markers." Plan's 4.1 output shows the third form only (counts of three marker types). The plan does not explain how the harness detects these markers. Are they token-matched in the produced human+agent turn text? Scraped from git? This is left unspecified; implementer must infer.

**Per-agent-turn row (§4.2):** Schema is complete for NE-6 use case. ✓

**Per-protocol aggregate (§4.3):** Schema is complete; "support_level" mapping is defined (§6). ✓

### 3.2 Do NE tests actually use the features from §2?

Framework §4.3 names seven NEs. Plan §5 specifies which are in first-pass scope:

| NE | Classifier dependencies | Plan coverage |
|---|---|---|
| NE-6 (numbered vs open close) | 2.8 only | Yes; "Highest-n (~5–10k agent turns); lowest classifier cost" (§5) |
| NE-2 (secure-dev never-ask vs dialog) | 2.2 opener | Yes; "Large n on treatment side (~100 sessions); 2.2 alone tags them" (§5) |
| NE-7 (template-dispatch leakage) | 2.2 + 2.12 | Yes; "Shares input with NE-2" (§5) |
| NE-1 (autonomy-partition opener) | 2.2 + 2.5 + 2.6 + 2.7 | Yes; "Only one treatment session (79a42399), but the test is a case-study" (§5) |
| NE-4 (f588ff0c within-session shift) | 2.10 + 2.11 | Yes; "Single-session test; treat it as a case study" (§5) |
| NE-5 (13493c8d vs 3bf5774c matched) | 2.2 + git-join | Partial; "Transcript-side (W1, C1) is trivial… belongs to R1 outcome-join, not Step 4.5 strict scope" (§5) |
| NE-3 (kerf reviewer adoption pre/post) | All above + reviewer-pattern tagger | Deferred; "Needs a classifier to detect when a session *used* the reviewer pattern" (§5) |

**Verdict:** NE dependencies are correctly matched to classifier outputs. First-pass scope is clearly justified. ✓

### 3.3 Broken references between sections

**Session-type discriminator (§2.1):** References `phases/phase-1/session-type-discriminator.md` verbatim. File exists and was read. ✓

**Extraction extension (§3):** Plan says "The harness imports the extractor as a library function" and describes extending extract_dialog.py to emit JSON with timestamps and tool-activity counters. Extract_dialog.py exists and has the right infrastructure (parse_ts, content_text, content_tools functions). Extractor library-ification is implementable. ✓

**Evaluation-criteria-refinement.sub-empirical-design.md (cited as NE source):** File exists and was read. Natural-experiment definitions in the plan match framework §4.3. ✓

**Unified-protocol-catalog.md (§6):** Plan references the 87-entry catalog and specifies how entries tag into three identity classes. File was not read, but the plan's protocol-identity tagging strategy (deterministic-opener, behavioral-signature, absent-from-corpus) is internally consistent. Implementer can apply it to any 87-entry input. ✓

**Writing-load.md (referenced for category a–h definitions):** File exists but plan does not re-inline the category definitions. Medium risk: implementer may misinterpret skeletal rules without access to that file.

**phases/phase-1/analysis/misaligned-assumption.md (ground-truth incident table):** Referenced for hand-validation (lines 343). File was not read to verify it has the incident-table structure the plan expects.

**Frame:** Plan's internal references are consistent and mostly resolvable. Category-definition and incident-table references require ground-truth file access.

---

## 4. Weakest link — concrete failure-mode prediction

**Single largest implementation risk:** **False-positive correction detection (C1/M1 metrics).**

**Why this is the weakest link:**

1. **Load-bearing, expensive downstream.** The pair-graph specifically requires C1/M1 for Step 5 ranking. If C1 count is systematically inflated (false positives), the rank ordering of candidate protocols will be corrupted. A candidate that scores well only because it produces false-positive corrections will be mis-ranked high.

2. **Validation is gated.** The plan correctly specifies hand-validation to precision ≥ 0.85 on the 10-session primary corpus (§7, lines 343–344) before corpus-wide run. However:
   - The plan does not specify how many incidents to label (if the primary corpus contains ~50 real incidents, precision ≥ 0.85 means ≤ ~9 false positives acceptable).
   - The lexicon has three tiers + multiple false-positive guards. If guards are over-tuned to fit the 10-session set, they may not generalize to the 195-session corpus (e.g., the "hmm yes" guard may be effective in one subset but over-block legitimate "hmm, [disagreement]" patterns elsewhere).
   - The plan does not specify what happens if precision < 0.85. Instruction is "check" (line 344); if it fails, decision-tree is unclear. User must explicitly choose to proceed with C1 disabled or to iterate. This is a user-decision gate, not an implementation blocker, but it is a risk.

3. **Rule complexity and false-positive surface.** Classifier 2.4 has multiple moving parts:
   - Tier-1 match must be turn-start (chars 0–20). But messages with leading whitespace or URLs at the very start may push the actual phrase outside this window.
   - The "actually mid-sentence" heuristic (line 83) requires identifying "surrounding clause is affirmative" — regex for clause boundaries is not specified.
   - The 40-char minimum (line 85) drops "no" single-word replies, but leaves "no, that's wrong" (18 chars) as a potential false negative.
   - Parent agent turn must be "substantive" (≥ 300 chars or contains a concrete technical noun). The technical-noun check is not operationalized — implementer must choose a heuristic (e.g., word list? presence of `<code>` markers?).

4. **Repeated-correction detection is O(n²).** The plan notes this (lines 349–350): "the feature-extractor's correction-incident detection is O(n²) in the naive topic-fingerprint join. Guard: cap same-thread-correction search window to 30 preceding human turns." The guard is specified, but the topic-fingerprint overlap metric (lines 90, "overlap ≥ 40%") is not. 40% is arbitrary; implementer must validate this threshold against the 10-session set.

5. **Confounding with session continuations.** The plan guards against session-boundary ambiguity (§7, lines 345–346): if two sessions are a continuation (within 2 minutes, matching context), they should be tagged and not counted independently. This is correct, but it adds a pre-processing step whose failure could inflate correction counts across session boundaries.

**Estimate of implementation cost impact:** The correction detector is rated H (hard) and allocated 4 hours (§8, line 367). Given the false-positive surface, expect this to be 5–7 hours once hand-validation is included and guards are tuned. If the hand-validation fails, add 2–3 hours of lexicon iteration. Total worst-case: 10 hours instead of 4, a 2.5× cost overrun on a small-but-load-bearing classifier.

**Mitigation is already in the plan:** hand-validation is specified (lines 343–344). The risk is real but bounded; the plan correctly identifies it.

---

## 5. Secondary observations

### Minor clarity gaps (non-blocking, addressed via implementer communication):

1. **Keyword-to-regex conversion (2.7, 2.9):** Keyword sets are given (e.g., lines 106–108), but the exact regex formation is left to implementer. This is fine for implementation, but a single canonical script should be used consistently across all classifiers.

2. **Timestamp handling for long gaps:** The plan notes (line 178) that `extract_dialog.py` already parses ISO timestamps and preserves per-turn timestamps. But what happens if a timestamp is missing or malformed? Plan does not specify fallback behavior for gap-calculation.

3. **S1 marker detection not operationalized.** To emit S1 counts (§4.1 line 228), the plan must detect TODO / decide-later / FIXME markers in the session text. Lexicon is not given. Likely candidates: `TODO`, `FIXME`, `DECIDE LATER`, `decide_later`, `decide later`, plus variations. Implementer should ask the user for the canonical marker set.

### In-scope but under-detailed:

4. **NE-5 partial implementation:** The plan correctly defers the "artifact stability" join to R1 outcome-work. But the plan should clarify whether the harness will emit enough data for R1 to plug in later. Specifically: does the harness save session timestamps and git-commit references? Yes (§4.1 includes first_ts, last_ts). ✓

---

## 6. Feasibility estimates — validation

Plan §8 estimates 32 hours total, or ~12 hours for trimmed version (NE-6 + NE-2 + NE-7 only). These estimates appear realistic given the classifier complexity. The largest cost driver is the correction-detector (4 hours) plus hand-validation (3 hours), followed by the writing-load category tagger (3 hours). Plan acknowledges both as H (hard).

One note: "Bug-fix / FP-tuning iteration budget" is 3 hours (line 379). Given the false-positive surface in the correction detector, this may be tight if multiple iteration cycles are needed post-hand-validation.

---

## Summary

**Coherence:** Internal consistency is strong. Output schemas match classifier specs; NE test dependencies are correct; references are resolvable.

**Completeness:** All framework §4 criteria are addressed, with explicit deferrals for outcome-joins and reviewer-pattern detection. Coverage is complete within the stated scope.

**Implementability:** 11 of 12 classifiers are ready-to-code or needs-work with clear paths forward. The correction-incident detector (2.4) and writing-load category tagger (2.9) are the highest-risk classifiers; both have partial ambiguities in rule definition and guard specification. Both are flagged with H (hard) rating and hand-validation steps. The plan correctly identifies these risks.

**Weakest link (one sentence):** False-positive inflation in correction-incident detection (C1/M1) is the single largest failure-mode risk because it is load-bearing for Step 5 ranking, has a complex false-positive guard surface, and depends on hand-validation thresholds that could fail to generalize to the full 195-session corpus.

---

## Recommendation for user authorization

The plan is coherent and **essentially complete**. It is ready to hand to an implementer. However:

1. **Before code starts:** Clarify with implementer:
   - Exact regex formation for keyword-set conversion (all classifiers).
   - NE-5 partial scope: confirm what transcript-side W1/C1 computation should save for later R1 git-join.
   - S1 marker canonical list (TODO, FIXME, DECIDE-LATER variants).

2. **During hand-validation (C1/M1 gate):** If precision < 0.85 on the 10-session set, explicitly authorize either (a) lexicon iteration or (b) proceeding with C1/M1 disabled. The plan should say so in the validation spec.

3. **Scope decision:** The plan recommends trimmed version first (NE-6 + NE-2 + NE-7, ~12 hours). This is prudent; ship v1, evaluate the NE-6 result, then decide whether to add W1/C1/M1/F1/F2. Plan already recommends this (lines 384–385). ✓

The plan does **not** authorize implementation; it proposes a scope and asks the user for authorization (§11). That framing is correct.
