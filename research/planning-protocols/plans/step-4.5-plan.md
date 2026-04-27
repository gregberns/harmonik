# Step 4.5 Implementation Plan — Corpus-Signal Filter Harness

> Planning-protocols research track, Phase 2 Step 4.5. This document expands `evaluation-framework.md` §4 into an implementation-level specification. It is a plan, not a commitment to execute. Authorization to run Step 4.5 is a separate user decision.
>
> Produced 2026-04-24 by a planning sub-agent. Input to user review per `phase-2-findings.md` §9 open question #1.

## 1. Scope and position in the pipeline

Step 4.5 is the Filter stage of the Generate → Filter → Confirm pipeline (framework §2). Its single job: produce quantitative corpus-signal support for the 87-entry unified protocol catalog, so that Step 5 reviewer evaluation receives a filtered slate rather than an unfiltered one.

Inputs:
- All `~/.claude/projects/-Users-gb-github-*/*.jsonl` session files (the four project dirs enumerated in `phases/phase-1/corpus/INDEX.md`: harmonik, kerf, machine-setup, secure-dev; 195 sessions total).
- `phases/phase-2/analysis/unified-protocol-catalog.md` (87 candidate protocols in 8 groups).
- `evaluation-framework.md` §3 (pair-graph criteria), §4 (harness spec), §4.3 (seven natural experiments).

Outputs (all under `research/planning-protocols/03-filter/`, new directory — does not overwrite any existing artifact):
- `features/session-<id>.json` — per-session feature row (195 files).
- `aggregates/protocol-<kebab>.json` — per-protocol aggregate (one per tagged protocol, ~20-30 files).
- `natural-experiment-report.md` — human-readable per-NE effect sizes.
- `filter-ranking.md` — catalog entries re-sorted by corpus-signal support level, ready for Step 5.
- `harness/` — the scripts themselves.

Hard constraints carried through:
- Transcript-only. No LLM calls inside classifiers. Regex, lexicon, and small fixed taxonomies only.
- No user questions.
- No modification of prior artifacts. Every output lands under `03-filter/`.
- No new sub-agents (per this task's constraint; future Step 4.5 runs may fan out per NE).

---

## 2. Classifier inventory

The harness is a pipeline of small classifiers over extracted turn text. Each classifier is spec'd below with its lexicon or rule set. Complexity ratings: **T** = trivial (≤ 20 LOC regex or lookup); **M** = medium (needs taxonomy design + unit tests); **H** = hard (needs careful false-positive guarding).

### 2.1 Session-type classifier (T — already specified)

Reuses `phases/phase-1/session-type-discriminator.md` verbatim. Output: one of {`autonomous-dispatch`, `controller-orchestration`, `session-recovery-handoff`, `planning-dialog`, `context-dump`, `scratch`}. Implementation already sketched in that reference. Sessions tagged `scratch` or `controller-orchestration` are still ingested but most pair-graph metrics are omitted as non-meaningful (e.g., M1 framing corrections in a controller session).

### 2.2 Opener-shape detector (M)

Examines turn 1 only. Decision tree:

```
if message matches /^# Session Recovery Context/i OR starts with "# Session Recovery":
    opener_shape = "recovery-handoff"
elif message matches /^You are the controller agent/i:
    opener_shape = "controller-orchestration"
elif message contains /never ask (the human )?questions|do not ask the human/i
     AND contains /study specs|\.scratch\/fix_plan\.md/i:
    opener_shape = "never-ask-template"
elif len(message) > 1500 AND no agent turn follows for >= 15 min:
    opener_shape = "context-dump"
elif message contains (autonomy_partition_lexicon):
    opener_shape = "autonomy-partition"
elif message matches (smeac_headers_regex | sbar_headers_regex | scqa_headers_regex):
    opener_shape = "structured-external-template"
else:
    opener_shape = "ad-hoc"
```

`autonomy_partition_lexicon` (regex-joined): `"trivial.*(decide|solve) yourself"`, `"critical.*ask"`, `"if.*trivial.*solve"`, `"you decide.*ask.*important"`, `"your discretion.*but.*check"`. Calibrated from 79a42399 H#1.

### 2.3 Hedging-language lexicon (T)

Per-turn binary flag plus density score (matches per 100 words). Lexicon:
- **Uncertainty:** `maybe`, `probably`, `possibly`, `might`, `could be`, `i think`, `i guess`, `i suppose`, `perhaps`, `seems like`, `sort of`, `kind of`, `roughly`, `approximately`, `somewhere around`.
- **Non-commitment:** `i'm not sure`, `not sure yet`, `haven't decided`, `either way`, `both work`, `no strong preference`, `either is fine`, `don't have a strong lean`, `don't have strong preferences`.
- **Fuzziness:** `fuzzy`, `vague`, `unclear to me`, `don't fully understand`, `no idea`, `not quite`.
- **Exploratory:** `exploring`, `still thinking`, `haven't figured out`, `we'll see`, `open to`, `curious if`.

F1 (opening-turn fuzziness-index per framework §3) = density in turns 1–3. F2 (fuzziness-resolution latency) = turn index at which rolling-3-turn density falls below 50% of F1 and stays there for ≥ 3 turns.

### 2.4 Pushback lexicon → correction-incident detector (M, with H guarding)

An incident is a human turn whose first 200 characters match the pushback lexicon AND whose parent agent turn made a factual, framing, or procedural claim (heuristic: agent turn was non-trivial, ≥ 300 chars). Lexicon tiers:

- **Tier 1 (strong signal, turn-start only):** `^wait[,.]`, `^no[,.]`, `^actually[,.]`, `^hold on`, `^hmm[,.]`, `^stop`.
- **Tier 2 (mid-turn contradiction verbs):** `that's not`, `that's wrong`, `you're asking`, `you just said`, `i said X but you`, `that contradicts`, `that's not what i`.
- **Tier 3 (mental-model reassertion):** `i don't think`, `i meant`, `what i meant was`, `let me clarify`, `to clarify`, `re-`, `again`, `as i said`.

False-positive guards:
- Discard when Tier-1 token is followed within 10 chars by `problem`, `worries`, `issues`, `rush` (handles benign "no problem", "no worries").
- Discard "actually" when it's mid-sentence and the surrounding clause is affirmative (regex: not preceded by sentence-end punctuation).
- Discard "hmm" when followed by an agreement token (`yes`, `sure`, `right`).
- Require the human turn to be ≥ 40 chars total (drops "no" reply-tokens to yes/no questions).

Subtyping into C1 components (framework §3 Pair P1):
- **Framing correction (M1):** incident cites the agent's *way* of characterizing something. Lexical markers: `the framing`, `the way you`, `you're treating`, `you're asking.*by`, `rather than`, `not a cache`, `not a truth`, `locked` (when agent used "locked" and human pushes back). Matches include `3bf5774c` H#3 "locked" catch, `3bf5774c` H#4 Beads-as-cache, `f588ff0c` H#4/5 label-listing.
- **Content correction:** incident disputes a fact, number, or procedural step. Markers: `that file doesn't`, `that's not installed`, `the command is`, `it's actually X`.
- **Repeated correction (same-thread):** any C1 incident whose topic n-grams (3-grams from the agent's preceding turn) overlap ≥ 40% with a prior C1 incident within the same session. Stores `(session_id, topic_fingerprint, incident_count)`.

### 2.5 Autonomy-grant lexicon (T) → A2 latency

Blanket-grant phrases, matched anywhere in human turn content:
- `whatever you think`, `whatever is fine`, `your discretion`, `you decide`, `i don't care`, `no strong preference`, `no preference`, `leave it to you`, `leave that to your discretion`, `pick whatever`, `either one is fine`, `i'll leave that to you`.

A2 = turn index of first match. If no match, A2 = ∞ (sentinel; reported as "not granted"). Session opener (H#1) with autonomy-partition shape counts as A2 = 1 even when explicit phrases aren't present (because the partition IS the blanket grant).

### 2.6 Wasted-question detector (M)

Marks an `(agent_turn, human_turn)` pair where the agent asked ≥ 1 question and the human's answer is ≥ 60% autonomy-grant tokens (by char count). Joins with trivial-topic classifier (below) to compute A1.

### 2.7 Trivial-topic taxonomy (M)

Fixed classifier over the content of an agent question (the question text, not the whole turn). Buckets:
- **Trivial (over-ask candidates):** file-naming, file-split vs single-file, directory-organization, tool-selection-among-standard-tools, small-commit-strategy, formatting, import-order. Keyword sets: `{name the file, filename, call it, folder structure, directory, one file or, split across, commit message, should i commit, squash}`.
- **Architectural:** interface boundaries, data-model decisions, responsibility allocation, error-handling posture, API shape. Keywords: `{interface, boundary, responsibility, who owns, authoritative, source of truth, contract, data model, error handling}`.
- **Requirements-clarification:** user-intent, acceptance criteria, out-of-scope. Keywords: `{do you want, what should happen when, should we include, out of scope, is.*required}`.
- **Unclassified:** everything else (default).

A1 = count of agent questions tagged `trivial` in the session. (Framework P2.)

### 2.8 Numbered-question-close detector (T — highest leverage)

Per agent turn, examine last 500 chars of agent text. Classify close-shape:
- **Numbered:** matches `/(^|\n)\s*\d+[.)]\s+.+\?/m` at least twice in the trailing region.
- **Open-ended:** contains `let me know`, `what do you think`, `thoughts?`, `shall i proceed`, `want me to`, `does that work`, with no numbered-question block.
- **Declarative:** ends in a statement, no "?" in trailing 200 chars.
- **Single-question:** one "?" in trailing region, not in a numbered list.
- **Mixed:** both numbered-questions AND open-ended trailing. (Noted; not common.)

This is the single most load-bearing classifier — it's the basis of NE-6, the highest-n natural experiment. Per-turn data point count: thousands. Output per agent turn: `close_type`, `next_human_turn_char_count`, `next_human_turn_hedging_density`.

### 2.9 Writing-load category tagger (a–h) (H)

Categories from `phases/phase-1/analysis/writing-load.md` (a) framing, (b) correction, (c) clarification, (d) approval, (e) scope-expansion, (f) decision-response, (g) administrative, (h) other. Rule sketch:

```
if turn_index == 1 and len > 300: (a)
elif matches(administrative_markers): (g)  # <local-command-stdout>, <bash-stdout>, <command-name>
elif matches(pushback_lexicon) or is_correction_incident: (b)
elif matches(clarification_markers: "can you explain", "what do you mean", "clarify"): (c)
elif matches(approval_markers: "sounds good", "looks right", "yes", "go ahead", "lgtm") and len < 100: (d)
elif parent_agent_turn_had_question and not (b): (f)
elif len > 200 and scope_expansion_markers: (e)
else: (h)
```

Scope-expansion markers: `also`, `one more thing`, `actually let's`, `let's also`, `i want to add`, `new requirement`. Administrative markers match the Claude Code command-capture artifacts (per `session-type-discriminator.md` caveat 2).

W1 output: `{a: chars, b: chars, ..., h: chars}`. Each category gets its own weight in downstream ranking (framework §3 Pair P1).

### 2.10 Inter-turn gap features (T) → G1/C2/T1

For each human→agent or agent→human transition:
- `gap_seconds` (timestamp diff; `extract_dialog.py` already parses ISO timestamps).
- `is_long_gap` = gap > 600 (10-min threshold from framework §3).
- `is_ping_pong` = gap < 60.
- `mid_gap_checkpoint` = agent turn within 5 min of a long gap ending, containing an explicit decision-surface token (`before you go`, `checkpoint`, `open question`, `i'll wait for`, numbered-question close with ≥ 3 items).

G1 reported as `(median, p95, long_count, ping_pong_count)`. T1 active = sum of gaps ≤ threshold; calendar = first-to-last timestamp.

### 2.11 Autonomous-stretch detector (T)

Piggybacks on `extract_dialog.py`'s existing `[AUTONOMOUS RUN]` marker (agent turn > 5 min OR > 20 tool calls). Per-session: count, longest, total autonomous minutes.

### 2.12 Template-dispatch leakage detector (T) — for NE-7

For sessions with `opener_shape == "never-ask-template"`: count human text turns after H#1. Each one is a leakage event. Classify the triggering agent turn (the one that surfaced the issue) via the trivial-topic taxonomy + a new `"architectural-uncertainty"` bucket (agent explicitly states it cannot proceed without a decision). Output per-session leakage profile.

---

## 3. Data flow

```
 ~/.claude/projects/-Users-gb-github-*/*.jsonl
                  │
                  ▼
     ┌──────────────────────────┐
     │  extractor.py            │  extends extract_dialog.py;
     │  (session walker)        │  emits raw turn-stream per session
     └──────────────────────────┘
                  │
                  ▼
     ┌──────────────────────────┐
     │  feature_extractor.py    │  runs §2 classifiers; emits
     │                          │  03-filter/features/<id>.json
     └──────────────────────────┘
                  │
                  ▼
     ┌──────────────────────────┐
     │  protocol_tagger.py      │  joins per-session feature rows to
     │                          │  protocol identity (§4);
     │                          │  emits 03-filter/aggregates/<kebab>.json
     └──────────────────────────┘
                  │
                  ▼
     ┌──────────────────────────┐
     │  natural_experiment.py   │  runs 7 NE tests from §4.3;
     │                          │  emits 03-filter/natural-experiment-report.md
     └──────────────────────────┘
                  │
                  ▼
     ┌──────────────────────────┐
     │  filter_ranker.py        │  joins NE results back to catalog entries;
     │                          │  emits 03-filter/filter-ranking.md
     └──────────────────────────┘
```

`extract_dialog.py` role: its filter logic (the `is_human_text` / `is_main_assistant` predicates, the `content_text` / `content_tools` helpers, the flush/buffer mechanics for collapsing consecutive assistant events into agent turns) is already correct and reusable. The extension: replace its markdown-emitter with a JSON-emitter that preserves per-turn timestamps, per-turn raw text, and tool-activity counters — a `turn_stream` that feeds `feature_extractor.py`. The existing CLI stays intact; the harness imports the extractor as a library function.

---

## 4. Output schemas

### 4.1 Per-session row (`03-filter/features/<session-id>.json`)

```json
{
  "session_id": "f588ff0c-699f-460c-a9d8-d0909cb8937d",
  "project": "-Users-gb-github-harmonik",
  "session_type": "planning-dialog",
  "opener_shape": "ad-hoc",
  "ht": 20,
  "at": 18,
  "first_ts": "2026-04-15T22:03:11Z",
  "last_ts":  "2026-04-16T01:47:02Z",

  "W1_by_category": {"a": 980, "b": 1650, "c": 0, "d": 80, "e": 130, "f": 1200, "g": 0, "h": 0},
  "C1_by_subtype":  {"framing": 3, "content": 1, "repeated": 1},
  "A1": 2,
  "A2": null,
  "M1": 3,

  "G1": {"median_s": 74, "p95_s": 840, "long_count": 4, "ping_pong_count": 6},
  "C2": 1,
  "T1": {"active_min": 146, "calendar_min": 224},
  "S1": {"todo_count": 2, "decide_later_count": 1, "fixme_count": 0},

  "F1": 0.017,
  "F2": 7,

  "numbered_close_turns": 11,
  "open_close_turns":     4,
  "declarative_close_turns": 2,
  "single_q_close_turns": 1,

  "autonomous_stretches": [{"turn": 5, "minutes": 48, "tools": 134}],
  "template_leakage": null
}
```

### 4.2 Per-agent-turn row (for NE-6, highest-n test)

Emitted as a single NDJSON file `03-filter/agent-turns.ndjson` (~5–10k rows across 195 sessions):

```json
{"session_id": "...", "agent_turn_idx": 3, "close_type": "numbered", "n_questions": 3,
 "next_human_turn_chars": 42, "next_human_turn_hedging_density": 0.0,
 "next_human_turn_category": "f", "parent_session_type": "planning-dialog"}
```

### 4.3 Per-protocol aggregate (`03-filter/aggregates/<kebab>.json`)

```json
{
  "protocol": "numbered-question-close",
  "origin_tags": ["observed"],
  "support_level": "strong",
  "tagged_sessions": 11,
  "untagged_counterfactual_sessions": 42,
  "primary_effect": {
    "metric": "next_human_turn_chars",
    "treatment_median": 85,
    "control_median": 210,
    "mann_whitney_p": 0.003,
    "n_treatment": 247,
    "n_control": 158
  },
  "notes": "NE-6 per-agent-turn test; see natural-experiment-report.md §6"
}
```

`support_level` ∈ {`strong`, `moderate`, `weak`, `null`, `not-testable`}. `not-testable` means the protocol is absent from the corpus (most `external:*`-origin entries).

### 4.4 Natural-experiment report structure

Markdown, one section per NE-1 … NE-7. Each section:
- **Design.** What the treatment and control are, in one paragraph.
- **n.** Treatment-side and control-side counts.
- **Metric.** Which feature is the outcome.
- **Result.** Effect size, direction, test statistic, p-value.
- **Confidence band.** Bootstrap 95% CI where applicable.
- **Caveats.** Confounders specific to this NE.
- **Catalog impact.** Which protocol entries in `unified-protocol-catalog.md` change `support_level` as a result.

### 4.5 Filter ranking (`03-filter/filter-ranking.md`)

The 87 catalog entries re-grouped into `strong` / `moderate` / `weak` / `null` / `not-testable` buckets, each entry annotated with `[filter-dep]` resolved to `[filter:<level>]`. This is the artifact Step 5 consumes.

---

## 5. Natural-experiment coverage — first-pass scope

Seven NEs specified in framework §4.3. Not all are equal cost. Ordering for first pass:

| NE | Test | Classifier deps | First pass? | Rationale |
|---|---|---|---|---|
| **NE-6** | numbered-close vs open-close | 2.8 only | **Yes** | Highest-n (~5–10k agent turns); lowest classifier cost; the A/B target in §8.3. Zero tagging work. |
| **NE-2** | secure-dev never-ask vs dialog | 2.2 opener | **Yes** | Large n on treatment side (~100 sessions); 2.2 alone tags them; leakage detection (NE-7) shares the same tagged set. |
| **NE-7** | never-ask leakage | 2.2 + 2.12 | **Yes** | Shares input with NE-2. Produces the convergent-evidence signal on question-class load-bearing-ness (framework §4.3 NE-7 rationale). |
| **NE-1** | autonomy-partition opener | 2.2 + 2.5 + 2.6 + 2.7 | **Yes** | Only one treatment session (79a42399), but the test is a case-study with supporting per-session metrics. Low cost given 2.2 is already built. |
| **NE-4** | f588ff0c within-session shift | 2.10 + 2.11 | **Yes** | Single-session test; treat it as a case study, not a statistical comparison. Output is a narrative block, not a p-value. |
| **NE-5** | 13493c8d vs 3bf5774c matched | 2.2 + requires git-join for "artifact stability" | Partial | Transcript-side (W1, C1) is trivial. "Artifact stability" requires git blame on produced files — belongs to R1 outcome-join, not Step 4.5 strict scope. Report the transcript-side only. |
| **NE-3** | pre/post kerf reviewer adoption | all above + reviewer-pattern tagger | Defer | Needs a classifier to detect when a session *used* the reviewer pattern (vs not), which requires agent-output-shape analysis beyond §2. Tag as "follow-up" in filter-ranking.md. |

**First pass produces NE-6, NE-2, NE-7, NE-1, NE-4** with a partial NE-5. That covers the highest-leverage tests with the smallest classifier stack. NE-3 and the outcome-join half of NE-5 feed R1/R2 retrospective work (framework §3 "outcome-side sanity check") and should be specified now but run later.

---

## 6. Protocol-identity tagging

The 87 catalog entries tag into three identity-classes:

**Template-identifiable** (deterministic from opener regex). The whole session carries the protocol.
- `autonomous-dispatch` (never-ask template) → §2.2 `never-ask-template`.
- `recovery-handoff` → §2.2 `recovery-handoff`.
- `context-dump` → §2.2 `context-dump`.
- `controller-orchestration` → §2.2 `controller-orchestration` (tagged but excluded from most metrics).
- `upfront-decision-partition` (autonomy-partition opener) → §2.2 `autonomy-partition`.
- Most `external:*` opener protocols (SBAR, SMEAC, SCQA, I-PASS, commander's intent) → §2.2 `structured-external-template` further sub-classified by header-regex. Corpus presence of these is expected to be zero; the tagger emits `support_level: not-testable`.

**Behavioral-signature** (detectable per-turn or per-session by classifier ensemble).
- `numbered-question-close` → every agent turn classified by §2.8; the protocol "holds" for a session if ≥ 60% of agent turns close numbered.
- `one-question-turn` (writing-load P1, not in unified catalog by that name but observed) → agent-turn question-count ≤ 1 for ≥ 70% of turns.
- `pre-action-plan-disclosure` → agent turn includes "i'm going to" or "plan: …" structured header followed by execution, before executing. Regex + heuristic.
- `load-bearing-token-readback` / `read-back-comprehension` → human or agent echoes a load-bearing noun-phrase from the immediately-prior turn within a short window. N-gram overlap metric with threshold.
- `fixed-token-status-vocabulary` (proposed/locked/deferred) → count of these specific tokens used consistently across turns.
- `alternatives-considered-section` → produced-artifact signal (requires joining to git-committed files; out of Step 4.5 scope).

**Absent-from-corpus / untestable.** The ~50 `external:*` entries that do not correspond to an observed pattern. Tag `support_level: not-testable`. They remain in Step 5 as analytical candidates; Step 4.5 simply cannot comment on them.

The tagger walks each catalog entry, runs its identifying predicate against all 195 sessions, writes session-id lists into the per-protocol aggregate file. An entry gets `support_level: strong` if n ≥ 10, `moderate` if 3 ≤ n < 10, `weak` if n < 3, `null` if n == 0 but the predicate is implementable, `not-testable` if no transcript-only predicate is possible.

---

## 7. Failure modes and guards

**False-positive correction detection.** The highest risk — C1/M1 count is a load-bearing output. Mitigations:
- Tier-1 tokens must be turn-start (char 0–20); turn-mid matches ignored.
- Benign-phrase blocklist ("no problem", "no worries", "wait a sec", "hmm yes").
- Require correction incident to reference a substantive prior agent claim (agent turn ≥ 300 chars or contains a concrete technical noun).
- Validation: hand-label correction incidents in the 10 primary-corpus sessions (already extracted in `phases/phase-1/corpus/`). Correction-set ground truth exists in `phases/phase-1/analysis/misaligned-assumption.md` §Incident Table. Compare detector output; require precision ≥ 0.85 on the hand-labeled set before running corpus-wide.

**Session-boundary ambiguity.** Claude Code sometimes splits what is logically one planning session into two JSONL files (fresh session after compaction). Guard: if two sessions on the same project have `last_ts` and `first_ts` within 2 minutes AND the second opens with `# Session Recovery Context` OR a near-duplicate of the first's tail, tag as `continuation`. Report continuation pairs in a separate list; do not count both independently when computing per-session metrics.

**Sidechain filtering.** `session-type-discriminator.md` caveat 3: "Zero sidechain events appeared in top-10 dialog-dense sessions. Sub-agents dispatched via the Task tool may not always surface as isSidechain:true." Verification step: pick a session known to use `Task` heavily (kerf parallel-reviewer sessions) and confirm the extraction drops the sidechain sub-agent content correctly. If sub-agent output bleeds into the main-thread assistant stream, add a secondary filter on assistant-turn content markers (`<sub-agent>`, `[Task:`).

**Very large sessions.** `f588ff0c-699f-460c-a9d8-d0909cb8937d` at 3.2 MB / 621 messages is the stress case. The extractor streams line-by-line; memory should be fine. But the feature-extractor's correction-incident detection is O(n²) in the naive topic-fingerprint join. Guard: cap same-thread-correction search window to 30 preceding human turns. Report any session whose feature extraction exceeds 60 s wall time — those become case-study candidates, not statistical data points.

**Claude Code command-capture artifacts.** `<local-command-stdout>`, `<bash-stdout>`, `<command-name>` tags inside user content look like human writing but are command-capture injection. Per `session-type-discriminator.md` caveat 2, strip these before running any classifier; count them into category (g) only.

**Multi-message structured directives inflate ht** (caveat 1). In b7eca5d2 the controller directive split across 5+ consecutive user events gave ht=59. Guard: collapse adjacent human events with no intervening assistant event into a single logical turn. This is a classifier-input preprocessing step, not a new classifier.

**Project-name assumption.** The prompt says `~/.claude/projects/-Users-gb-github-*/`. The actual project directories per `phases/phase-1/corpus/INDEX.md` are harmonik, kerf, machine-setup, secure-dev (plus the filtered 8 ntm-worktree dirs under secure-dev, and a `Developer-secure-dev` variant mentioned once in the discriminator provenance). Enumerate explicitly; do not glob blindly, to avoid pulling in unrelated projects the user has added since.

---

## 8. Feasibility

| Component | Complexity | Est. hours |
|---|---|---|
| JSON extractor (refactor of `extract_dialog.py`) | T | 1.5 |
| Session-type classifier | T (already spec'd) | 0.5 |
| Opener-shape detector | M | 2 |
| Hedging-language lexicon + F1/F2 | T | 1 |
| Pushback / correction-incident + subtyping (2.4) | H (FP guards, hand-validation) | 4 |
| Autonomy-grant + wasted-question | T | 1 |
| Trivial-topic taxonomy | M | 2 |
| Numbered-close detector + NE-6 per-turn emission | T | 1.5 |
| Writing-load category tagger (a–h) | H | 3 |
| Inter-turn gap features | T | 0.5 |
| Template-dispatch leakage | T | 0.5 |
| Per-session feature row + JSON schema | T | 1 |
| Protocol-identity tagger (all 87 entries) | M | 3 |
| Natural-experiment driver (NE-1, -2, -4, -5-partial, -6, -7) | M | 3 |
| Report generation (`natural-experiment-report.md`, `filter-ranking.md`) | T | 2 |
| Hand-validation pass against primary-corpus labels | H | 3 |
| Bug-fix / FP-tuning iteration budget | — | 3 |
| **Total** | | **~32 hours** |

Framework §4 estimated 1–2 days. That was optimistic for the full harness; 32 hours ≈ 4 focused days is more realistic. If the scope is cut to just **NE-6 + NE-2 + NE-7** (skipping the writing-load category tagger, M1 subtyping, F1/F2, and C2), the harness collapses to ~12 hours — a credible 1.5-day build. The user should authorize either the trimmed or the full build explicitly.

Recommendation: build the trimmed version first (NE-6 is the primary target and the input to the recommended A/B in framework §8.3). Ship `03-filter/` as v1. Come back for W1/C1/M1/F1/F2 if the NE-6 result is interesting enough to justify the extra classifier work.

---

## 9. Follow-up work this harness unlocks

- **R1 outcome-join (framework §3).** Once `03-filter/features/<id>.json` exists, spec-revision-within-30-days can be computed by joining session timestamps to kerf's git log on the spec/ directory. That produces the first Framing-C quantitative signal and would retire several `[trap-candidate]` tags in `phase-2-findings.md` §3.
- **R2 implementer-time-to-first-blocker.** Same join, different edge: earliest commit after a planning session referencing a file the plan discussed.
- **Practitioner-diagnostic digest (framework §6).** Every signal in the §6.1 table is computable from the per-session JSON. A weekly digest script is ~100 LOC on top of the harness.
- **Simulation sweep calibration (framework §7).** The roleplay-user persona validation step ("check simulated responses fall within real-response distribution") needs the real distributions — hedging density, turn-length histograms, pushback-rate per opener-shape — exactly what §4.1 and §4.2 outputs produce.
- **A/B outcome metrics (framework §8).** The NE-6 A/B pre-registration names `next_human_turn_chars` as the primary metric. The harness computes it; the A/B just needs a new-session tagger and identical measurement. Near-zero incremental code.
- **Catalog maintenance.** As new protocols are added to the unified catalog, `protocol_tagger.py` gains a new predicate; everything else is reusable.

---

## 10. What this plan deliberately defers

- The R1/R2 outcome-joins themselves. They require git-history access and a repo-surface-pattern decision (which kerf directories count as "spec revisions"). Belongs to a separate sub-plan.
- Simulation harness construction (framework §7).
- A/B execution (framework §8).
- NE-3 reviewer-pattern detection. Requires a classifier that reads agent-output-shape within a session to detect when a reviewer sub-agent was invoked vs not.
- Per-user-state stratification (late-night vs daytime sessions). Framework §3 demoted this to qualitative overlay; the harness could emit it cheaply as a stratification axis on top of existing outputs.

---

## 11. Decision the user is being asked to make

Three discrete authorizations:
1. **Scope.** Trimmed harness (NE-6 + NE-2 + NE-7, ~12 h) or full harness (all NEs except NE-3, ~32 h)?
2. **Location.** Confirm `research/planning-protocols/03-filter/` as the output directory. (Append-only; does not touch `phases/phase-1/corpus/`, `phases/phase-2/analysis/`, or `evaluation-framework.md`.)
3. **Validation bar.** The correction-incident detector must hit precision ≥ 0.85 on the hand-labeled 10-session primary corpus before being run across all 195. If it doesn't, does the user want the harness to proceed anyway with the detector disabled (C1/M1 left uncomputed) or to pause for lexicon iteration?

The plan does not run Step 4.5. Phase-2-findings.md §9 open question #1 remains the gating user decision.

---

### Critical Files for Implementation

- `research/planning-protocols/scripts/extract_dialog.py`
- `research/planning-protocols/phases/phase-1/session-type-discriminator.md`
- `research/planning-protocols/evaluation-framework.md`
- `research/planning-protocols/phases/phase-2/analysis/unified-protocol-catalog.md`
- `research/planning-protocols/phases/phase-2/analysis/evaluation-criteria-refinement.sub-empirical-design.md`
