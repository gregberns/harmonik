# Analysis Lens: Context-Switch / Autonomy Stretches

Generated: 2026-04-23

## What this lens looks for

How long the agent runs autonomously between human turns, and what governs that. Specifically: what precedes long autonomous stretches, what terminates them, whether sessions achieve the "many short human turns + long autonomous agent passes" pattern the user named as a goal, and what separates sessions that achieve it from those stuck in ping-pong.

## Methodology

For each session, agent-turn headers were extracted (duration and tool-call count from the header format `(Nm, K tool calls)`). "Autonomous Run" = marked `[AUTONOMOUS RUN]` (>5 min or >20 tool calls). "Short stretch" = <1 min or 0 tool calls. Session-level summaries derived from headers; content samples read for turn-context around long stretches.

Duration markers `~` (zero-length ping) and `<1s` counted as under-1-min. Session wall-clock spans include multi-day gaps where the user stepped away; these inflate per-session totals but do not affect per-turn measurements.

## Per-session stretch profiles

| Session | Turns (H / A) | Median agent duration | Max agent turn | # AUTONOMOUS RUN | # short (<1 min) | Notes |
|---|---|---|---|---|---|---|
| d1704aa0 (secure-dev) | 17 H / 5 A | ~20m | 42m, 165 tools | 2 of 5 | 2 | Dispatch-to-dispatch; very few dialog turns |
| c6d1bd16 (secure-dev) | 25 H / 17 A | ~2m | 19m, 93 tools | 7 of 17 | 4 | Highest density of autonomous runs in corpus |
| 79a42399 (secure-dev) | 38 H / 28 A | ~1m | 9m, 60 tools | 5 of 28 | 9 | Mixed; long stretches interleaved with ping-pong |
| 38415843 (kerf) | 31 H / 22 A | ~1m | 5m, 37 tools | 2 of 22 | 7 | Mostly dialog; two bounded autonomous bursts |
| f588ff0c (harmonik) | 20 H / 18 A | ~1m | 48m, 134 tools | 2 of 18 | 5 | One extreme outlier; overnight dispatch |
| 3bf5774c (harmonik) | 21 H / 19 A | ~45s | 18m, 24 tools | 1 of 19 | 9 | Mostly dialog; one late autonomous run |
| 2a50e0fc (machine-setup) | 19 H / 15 A | ~2m | 27m, 75 tools | 3 of 15 | 4 | Brief dialog, multiple large autonomous passes |
| **Variants** | | | | | | |
| 13493c8d (harmonik, context-dump) | 5 H / 4 A | ~10m | 19m, 99 tools | 1 of 4 | 1 | Archetype: dense brief + long run |
| 729dad16 (kerf, session-recovery) | 14 H / 8 A | ~30s | 2m, 15 tools | 0 of 8 | 5 | No long runs post-handoff; tool-navigation ping-pong |
| 00eb9fc9 (harmonik) | 5 H / 5 A | ~2m | 16m, 18 tools | 2 of 5 | 0 | Scoping dialog; two meaningful autonomous bursts |

## Findings

### 1. Long autonomous stretches are preceded by one of three dispatch types, not by long human messages

The strongest predictor of a long autonomous run is not the length of the human turn, but whether the human turn contained an **explicit go-directive** that pre-authorized a class of decisions.

Three observed dispatch types and their outcomes:

**Type A — Delegated-work directive with explicit exit criteria and no-question clause.** Examples:
- f588ff0c Human #6 (273 chars): "Ok, I'm going to bed. ... Don't stop to ask questions at this point - just go. There is so much work to be done - start getting it delegated out ... Go for it!" → Agent #5: 48m, 134 tools.
- c6d1bd16 Human #2 (455 chars): "Correction: to do any work, the change first needs to be defined in the spec... Before anything else, lets get that set of steps defined and formalized in a skill so it can be repeated" → Agent #2: 5m, 24 tools.
- d1704aa0 Human #1 (long structured handoff with task list): → Agent #1: 42m, 165 tools.

The no-question clause is load-bearing. Without it, the agent tends to surface one decision per sub-task it encounters.

**Type B — Delegated sub-task with clear goal + constraints.** Examples:
- 79a42399 Human #4 (short: "A. Sounds good / B. git is fine / C. whatever you think is fine") → Agent #4: 2m, 26 tools. The human resolved three pending choices in one message; the agent could then run.
- c6d1bd16 Human #4 (2 lines: scope reduction + "Lets get started") → Agent #4: 8m, 33 tools.

Short message, but it *closed open decisions*. Message length did not predict stretch length — decision-closure did.

**Type C — Multi-thousand-character context brief (context-dump).** Example:
- 13493c8d Human #2 (very long multi-point list of research areas, tool references, external URLs): → Agent #1: 19m, 99 tools.
- 2a50e0fc Human #1 (long structured handoff listing 24 bugs with file/fix detail): → Agent #1: 26m, 51 tools.

Here message length *does* predict stretch length, but the mechanism is different: the brief pre-answers all anticipated agent questions, so the agent never needs to surface them. This is the context-dump protocol — spend the brief once, get a long uninterrupted run.

### 2. Short ping-pong stretches have a consistent cause: open decisions or tool-friction, not topic complexity

The shortest agent turns in the corpus fall into two categories:

**Decision-surface ping-pong.** Agent produces analysis and asks 2-4 questions; human answers; agent confirms and asks next set. This dominates 38415843 (kerf), 3bf5774c (harmonik), and the early turns of f588ff0c. Example from 38415843: Human #3 answers with "I'll leave that to your discretion" three times in a row — the agent was surfacing decisions the human had already delegated. The agent needed permission to decide, not an answer.

**Tool-navigation interruptions.** 729dad16 (session-recovery) is almost entirely this: agent encounters an unfamiliar tool (ntm), tries something, reports failure, human corrects. Agent #1 runs <1s (2 tools), Agent #2 runs 26s, Agent #3 runs 2m. No long runs in the entire session — the agent lacked the operational confidence to run autonomously.

**`~` (zero-duration) turns** appear when the agent is waiting on a task-notification from a sub-agent (f588ff0c Agents #6, #9, #10) or has nothing to say pending more information. These are not planning turns; they are scheduling artifacts.

### 3. The "desired pattern" (short human turns → long autonomous passes) does exist in the corpus and has two distinct forms

**Form 1: Batched-clarification + go-directive.** Sessions 38415843, 3bf5774c, and early f588ff0c: 4-6 short turns (1-3 min each) of back-and-forth clarification, then one human turn that explicitly closes decisions and dispatches. Example from 38415843: Human turns #1–4 are 1-3 min gaps and short messages; Human #5 says "Lets start by thinking deeply about that" → leads to cascading autonomous work once structure is agreed. Human #16 says "Agent mail is the right path. Our objective is to hone our process right now... Make sure to get the process updated in your configuration" → Agent #13: 5m, 37 tools [AUTONOMOUS RUN].

This form requires a conscious "dispatch turn" — a human message that signals mode-switch from dialog to execution.

**Form 2: Context-loaded one-shot.** Sessions d1704aa0 and 13493c8d: one large human message containing the full context bundle, and the agent runs immediately without further input. The human does their writing up front; there is no iterative clarification phase at all.

00eb9fc9 is a clean example of the desired pattern emerging within a research-scoping dialog: Human #3 gives a 6-sentence scope-narrowing message → Agent #3: 16m, 18 tools. The message contains both permission ("use sub agents", "throw many different ideas at them") and scope ("get the research infrastructure created... formally define the process").

### 4. Overnight/go-to-bed dispatches reliably produce the longest autonomous stretches

f588ff0c Agent #5 (48m, 134 tools) is the single longest autonomous stretch in the primary corpus. It was preceded by the user's "I'm going to bed" message with an explicit no-question instruction. Agent #8 in the same session (11m, 18 tools) was also preceded by a go-away-and-work dispatch.

The pattern is reliable: when the human signals non-availability, the agent is forced to make decisions autonomously. The tradeoff is that misaligned assumptions accumulate and may require correction later.

### 5. Context-ladder moments are present but fragile

A "context-ladder" sequence — human loads context → agent confirms understanding → human dispatches → agent runs long — appears in several sessions but does not always reach the long run:

- 3bf5774c: Human #1 loads a 1400-character structured handoff → Agent #1 confirms understanding (1m, 15 tools) → Human opens dialog about a specific topic. The ladder never triggers a long autonomous run; each topic becomes a new dialog branch.
- f588ff0c: Human #1 loads handoff → Agent #1 confirms (49s, 4 tools) → Human #2–5 are short clarification turns → Human #6 dispatches → Agent #5 runs 48m. The ladder required 4 additional clarification turns before the dispatch fired.

The fragility comes from the agent's tendency to surface options rather than commit. Even after a rich context load, the agent defaults to presenting choices unless explicitly told to decide.

## Candidate planning protocols this lens suggests

**Protocol A: Decision-batch-then-dispatch.** Run 3-6 short dialog turns to close open decisions. Then send a single "now go" message that names what to do and explicitly delegates any remaining sub-decisions. The "go" message needs: a clear goal, permission to decide implementation-level details autonomously, and either a no-question clause or a reference to what the agent should do if it hits a blocking decision. Predicted effect: converts 20 turns of ping-pong into 5 clarification turns + 1 dispatch + 1 long autonomous run.

**Protocol B: Structured-brief one-shot.** Write one long, structured message that covers: goal, constraints, context pointers (file paths, external references), explicit decision rights ("I don't care about X, decide it"), and exit criteria. Skip the iterative phase entirely. Higher up-front human writing cost; maximal autonomous stretch. Best for sessions where the human already has a clear picture and just needs the agent to execute.

**Protocol C: Explicit decision-delegation boundary.** Before the first substantive agent turn, establish in the opening message which decisions the agent should make itself vs escalate. "Architectural and UX-critical → ask me. Implementation details and structure → decide." This prevents the decision-surface ping-pong that makes short stretches short. Evidence: 79a42399 Human #1 explicitly names this pattern ("If you have questions, ask them. If there are trivial details - solve them. If there are critical decisions, ask.") — this session has one of the higher autonomous-run counts in the primary corpus.

## Open questions

1. **Do longer autonomous runs produce more misaligned assumptions?** This lens cannot answer without cross-referencing the misaligned-assumption lens. The overnight f588ff0c dispatch is the best test case.

2. **What makes 729dad16 (session-recovery) produce zero long autonomous runs?** The agent appears to lack operational confidence with an unfamiliar tool environment (ntm), not a lack of decision rights. Is tool-familiarity a prerequisite for long autonomous stretches, independent of protocol?

3. **Why does the context-ladder pattern not reliably trigger long runs in 3bf5774c?** Even with a rich handoff, each sub-topic becomes its own dialog branch. Is this a jig structure issue (kerf's one-topic-at-a-time design) or agent behavior?

4. **Is the no-question clause necessary or sufficient?** All observed overnight dispatches include it. Is it causal, or does the "I'm leaving" signal already carry enough permission?

## Notes on variants

**13493c8d (context-dump):** Achieved the desired pattern cleanly. 5 human turns total, one substantial autonomous run (19m, 99 tools) on the first agent turn. The mechanism: Human #2 is a 2000+ character multi-point brief that pre-answers anticipated questions. This is the cleanest example of Protocol B in the corpus. The tradeoff is visible: the session produced a broad research structure but left many open questions that required follow-up sessions.

**729dad16 (session-recovery):** Zero autonomous runs. The recovery handoff was minimal (1 line: "Reread AGENTS.md and continue") and immediately ran into tool-navigation friction. The session never established enough operational clarity for the agent to run independently. This is a negative case: recovery handoffs that don't include decision-rights + operational context produce ping-pong regardless of the underlying work being well-defined.

**00eb9fc9 (harmonik, this research session):** Shows the desired pattern emerging within a focused research-scoping dialog. Two autonomous runs (16m and 7m) preceded by scope-closing human messages. The 16m run was preceded by a 6-sentence message that combined scope-closing, permission-granting ("use sub agents", "this is incredibly important - do not hesitate to churn hard"), and concrete task definition ("Get a new folder research created").
