# Analysis Lens: Form vs Content

Generated: 2026-04-23

## What this lens looks for

Whether the *shape* of a planning discussion — turn length distribution, rhythm, use of structured artifacts, how agent turns close — predicts alignment speed independently of topic. The user's explicit hypothesis: "the form of the discussion might have a significant impact."

Measured features per session:
- Turn length distribution (human and agent separately, in rough character bands)
- Human:agent character ratio
- Turn rhythm (evenly distributed or bursty)
- Agent-turn closing pattern (question / option-list / declarative)
- Structured artifacts in human turns (bullets, code, tables)
- Whether form shifted mid-session and whether that helped or hurt
- Total human writing cost proxied as estimated total human-turn character count

## Methodology

All 10 sessions read in full (or as far as the token limit allowed for large files). Turn counts, opening and closing forms, and representative turn lengths were measured by direct observation. Character counts are estimates from visible text, not precise counts — within-order-of-magnitude for comparison purposes. Sessions are labeled by their short ID prefix.

"Alignment speed" is approximated as: (a) how many turns elapsed before agent output was accepted without correction and (b) whether misalignments surfaced early or late.

This lens deliberately avoids re-reporting decision-delegation, misaligned-assumption, or context-switch findings — those belong to other lenses. Focus here is on structural features only.

## Findings

### Per-session structural features table

| Session (short ID) | ht / at | Est. avg human turn (chars) | Est. avg agent turn (chars) | H:A ratio | Human turn rhythm | Agent close pattern | Human uses structure? | Form type |
|---|---|---|---|---|---|---|---|---|
| **79a42399** (secure-dev) | 38 / 28 | ~200 | ~800 | 1:4 | Very bursty early, short-volley mid | Mix of question + option-list | Rarely (lists appear in H2) | Mixed: proposal-response early → short-volley operational |
| **38415843** (kerf) | 31 / 22 | ~100 | ~600 | 1:6 | Evenly distributed, mostly short | Declarative + trailing question | No (terse confirmations) | Short-volley throughout |
| **c6d1bd16** (secure-dev) | 25 / 17 | ~350 | ~1200 | 1:3.5 | Bursty (H2 is a multi-paragraph redirect) | Option-list + question | Yes (H2: numbered list of phases + correction) | Mixed: one big redirect turn, then short-volley |
| **3bf5774c** (harmonik) | 21 / 19 | ~350 | ~700 | 1:2 | Evenly distributed | Question at end of each turn | Some (H1: structured handoff doc; H2: prose) | Short-volley planning dialog |
| **f588ff0c** (harmonik) | 20 / 18 | ~400 | ~1100 | 1:2.75 | Bursty (H6 is the large "go for it" directive) | Long autonomous after H6 | Somewhat (H5 uses bullet responses) | Short-volley → autonomous dispatch shift |
| **2a50e0fc** (machine-setup) | 19 / 15 | ~600 | ~1800 | 1:3 | Very bursty (H1 is a ~3000-char dump; rest are terse) | Declarative (task-based) | Yes (H1: numbered priority list) | Single large directive → operational short-volley |
| **d1704aa0** (secure-dev) | 17 / 5 | ~150 | ~5000 | 1:33 | Near-all human turns are single lines after H1 | Declarative (no questions) | H1 only (numbered priority list) | Single large directive → nearly-autonomous |
| **13493c8d** (harmonik, context-dump) | 5 / 4 | ~2500 | ~3500 | 1:1.4 | Completely bursty (2 large turns before agent runs) | No questions; declarative summaries | Yes (H2: dense enumerated multi-topic brief) | Context-dump; briefing-then-run |
| **729dad16** (kerf, session-recovery) | 14 / 8 | ~120 | ~700 | 1:5.8 | Short-volley throughout | Mix: question and declarative | No | Session-recovery → short operational dialog |
| **00eb9fc9** (harmonik, short-recent) | 5 / 5 | ~600 | ~900 | 1:1.5 | Bursty (H2 is a very long answer, H3 redirects) | Question at end of each turn | Partly (H2: inline numbered answers to 5 questions) | Structured Q&A → research setup |

---

### Finding 1: Short-volley sessions with structured agent option-lists correlated with faster alignment

Sessions **38415843** (kerf) and **3bf5774c** (harmonik) are the clearest short-volley examples with consistent agent turn structure. Both featured:
- Agent turns ending with 2–4 numbered options or explicit questions (not open-ended "let me know what you think")
- Human confirmations averaging under 150 chars
- No large correction turns — alignment was incremental

Evidence: 38415843 H3 ("commands.md — lets leave as one file for now / I'll leave that to your discretion...") is 4 lines of terse confirmations to 4 agent questions. The agent's numbered questions in A2 pulled out those confirmations efficiently. By contrast, 3bf5774c H3 was a multi-paragraph recalibration ("Again — 'the mechanism is already locked' — we are still investigating...") triggered by the agent using definitive framing. That single phrasing choice cost ~42 minutes and a correction turn.

### Finding 2: The first 2–3 human turns' form strongly predicts the whole session's form — with one common failure mode

In every session, the opening 2–3 turns established a form that persisted unless the human explicitly redirected. The failure mode: agent adopted a long-analysis-then-seek-approval shape when the human opener was a broad, open-ended brief (e.g., 79a42399 H1: "I have no idea if/how it works..."). This produced long agent turns that the human could not absorb without significant reading load.

The sessions that escaped this pattern had one of two interventions:
- **Human forced a process checkpoint**: c6d1bd16 H2 ("Before anything else, lets get that set of steps defined and formalized in a skill") — this redirected a drift toward premature action into a structured planning gate.
- **Agent self-structured**: 38415843 A1 ended with exactly 3 numbered questions, pulling a short-volley shape from a moderately open opener.

Prediction accuracy: In sessions where A1 used a numbered question list as its close, all three (38415843, c6d1bd16, 3bf5774c) stayed in short-volley or got there within 3 turns. Where A1 used a declarative summary with no pull, drift toward monologue occurred (d1704aa0, 2a50e0fc after H1).

### Finding 3: Agent-turn closing pattern is a stronger predictor of subsequent human-turn length than human-turn length itself

Across all sessions: when the agent ended a turn with a **numbered question list** (2–4 specific questions), the following human turn was typically under 200 chars and directly addressed each item. When the agent ended with **"want me to proceed?"** or **"let me know what you think"**, the following human turn was often either very long (they answered more than asked) or very short and permissive ("yes, go ahead") — both worse outcomes for planning fidelity.

Evidence:
- 38415843 A2 ends with 4 numbered questions → H3 answers each in 4 lines (clean, parseable, low human writing cost).
- 38415843 A4 ends with "Thoughts? Things I'm uncertain about" + 2 bullet optionals → H4 answers in 3 lines, dismisses both optionals.
- f588ff0c A3 ends with "Two questions before I write it: 1... 2..." → H3 answers both in 5 words each ("kerf / spec jig" and "I don't have a strong lean"). This is the most efficient exchange in the corpus.
- 3bf5774c A2 ends with 3 numbered "One sharper framing question" → H2 was 400+ chars with a conceptual reframe of the whole discussion, suggesting the framing question landed but opened scope rather than closing it.

### Finding 4: Form shifts mid-session — the "big directive" shift — correlate with autonomy stretches but carry alignment risk

Three sessions show a clear "short-volley phase → large directive → autonomous run" structure:
- **f588ff0c**: H6 ("Ok, I'm going to bed... go for it!") — ~400 chars; agent A5 ran for 48 minutes with 134 tool calls.
- **2a50e0fc**: H1 itself is the large directive; Agent #1 ran 26 minutes autonomously.
- **d1704aa0**: H1 is the directive; Agent #1 ran 42 minutes.

In f588ff0c, the autonomous run produced solid artifacts because the short-volley phase (H1–H5) had established alignment. The run was a controlled delegation, not a blind one.

In 2a50e0fc, the directive was precise enough (numbered priority list, spec references) that the autonomous run mostly succeeded — but H13 introduced a runtime error that required a correction cycle, suggesting the directive didn't fully anticipate real-world interaction.

In d1704aa0, the single directive preceded by zero planning dialog led to 17 human turns where 15 were just 1–2 lines of "check mail again" / "they completed." Human steering overhead was near-zero but also alignment checking was near-zero.

**Pattern**: the big-directive shift works best when it follows adequate short-volley calibration. Without prior calibration, it delegates unknown unknowns.

### Finding 5: Structured artifacts in human turns only help when they match the agent's question granularity

Sessions where the human used structured responses (numbered lists, bullets) produced faster alignment only when the structure matched what the agent asked. The failure case was 13493c8d H2: a dense multi-topic brief (~3441 chars with 6 enumerated topics, inline questions, sub-goals) that asked the agent to handle everything in one pass. The agent's A1 response was a long summary declaration with no discriminating questions. No alignment loop happened because the agent didn't expose any model to correct.

The efficient case: 00eb9fc9 H2, where the human used a numbered format to respond to the agent's numbered questions from A1 — same granularity, same order. The human explicitly flagged this interaction style as a cost they wanted to reduce: "large question and answer batches... taking one area at a time is often easier." That turn itself took ~48 minutes of human attention by their account.

## Candidate planning protocols this lens suggests

**P1 — Numbered-question close**: Agent ends every planning turn with 2–4 specific, independently-answerable numbered questions. Never open-ended "let me know" or "shall I proceed." Human can respond to each in 10–30 chars. Cuts human writing cost while preserving signal.

**P2 — First-turn framing contract**: At session start, agent and human establish the session shape explicitly: "for this session I will end each turn with 2–3 questions; you answer each directly, I update and ask the next set." Sets a short-volley contract before content diverges.

**P3 — Calibrated directive-shift**: Before issuing a "go for it" autonomous-run directive, human and agent jointly confirm: "here are the open unknowns I'm delegating; here's how I want them handled; here's when to surface a choice." Prevents the f588ff0c-style risk of delegating unknown misalignments.

**P4 — Structure-matching**: Human structure (bullets, numbered lists) should only be used in response to agent questions of equivalent granularity. Broad free-form human briefs with embedded structure (13493c8d pattern) skip the alignment loop and should be split into structured topics, not bundled.

## Open questions

1. **Does P1 (numbered-question close) break down for large conceptual topics?** 3bf5774c H2 responded to a focused question with a scope-expanding 400-char answer. This wasn't the agent's fault — the topic was genuinely branching. Are there topic types where numbered questions can't contain the human's answer?

2. **Is there a minimum short-volley phase length before a safe autonomous-run directive?** The corpus suggests 5–8 substantive human turns may be needed for the directive to be well-calibrated, but n=3 is too small to confirm.

3. **Does the H:A character ratio have a causal relationship with alignment quality, or is it a symptom?** The 1:1.4 ratio in 13493c8d (context-dump) and 1:2 in 3bf5774c (good short-volley) are very different, but both produced useful artifacts. The ratio may be a proxy for other variables.

4. **What is the right agent-turn length for a planning dialog turn?** The corpus contains agent turns from ~80 chars (38415843 A3: "Agreed on `_plan.md`. Good consistency.") to ~5000+ chars (d1704aa0 A1). No clear "sweet spot" is visible — length seems less important than closing structure.

## Notes on variants

**13493c8d (context-dump, harmonik) — the extreme form:**

This session is structurally the opposite of short-volley planning dialog. The human writes two massive turns (~5294 and ~3441 chars) front-loading everything. The agent's job is to do research and synthesize, not to co-develop understanding. Key observations:

- Zero alignment loop in the conventional sense — the agent never exposed its model to correction.
- The session produced 58 files and a complete structured knowledge base. Output quality appears high.
- Human writing cost was front-loaded and high (~8700 chars in 2 turns), but the payoff was a self-organizing knowledge base that didn't need correction cycles.
- **The form is appropriate for its purpose** — constructing a knowledge base from a brief is not the same as aligning on a plan. This pattern may be optimal for "here is everything I know; organize and synthesize it." It is not a planning dialog.
- **Critical limitation**: when the context-dump contains a mistaken assumption, there's no correction loop to surface it early. The error propagates into 58 files before a human can catch it.

**729dad16 (session-recovery, kerf):**

The session-recovery opening is a structured mechanical artifact ("# Session Recovery Context" header + single sentence of prior checkpoint). This is a *form protocol* in the precise sense the research is asking about: it establishes what the agent knows and where to resume without requiring the human to re-brief. The rest of the session reverted to operational short-volley (ntm troubleshooting). Form finding: a structured recovery header is high-leverage form — it compresses what would otherwise be a long re-context human turn into a machine-parseable artifact.

**00eb9fc9 (short-recent, harmonik):**

Most interesting for this lens because the session is *about* planning protocols. The human explicitly flagged that the large Q&A batch in H2 is a pattern they want to reduce. The session demonstrates the very cost it's trying to study: H2 is a ~600-char numbered response to 5 agent questions, which the human notes took ~48 minutes to produce. Agent A2 compressed the learnings in 4 lines and did not ask any further questions in that turn — a declarative synthesis. H3 then redirected with a meta-comment about form. The session shows both the problem (large batched Q&A) and the beginning of a solution (one-topic-at-a-time); it is the most direct empirical example of this lens's core hypothesis.
