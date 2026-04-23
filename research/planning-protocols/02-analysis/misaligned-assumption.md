# Analysis Lens: Misaligned Assumption

Generated: 2026-04-23

## What this lens looks for

Moments where the agent operated on an assumption that did not match the human's intent, causing work to proceed in the wrong direction or requiring correction. The goal is to identify: what telegraphed the misalignment, how many turns elapsed before the human noticed, what the correction cost was, and whether earlier detection was possible.

This lens excludes decision-delegation (when agent asked vs. decided) and writing-load (where human wrote too much) except where those directly overlap with a misalignment event.

---

## Methodology

Read all 10 corpus sessions cover-to-cover. For each human turn that contains pushback, correction, a "wait" moment, or clarifying context the human felt compelled to add, trace backward to find the agent assumption that caused it. Classify each instance by domain. Assess detectability: what signal was available earlier that the agent missed or the human didn't pre-empt?

Sessions read: 38415843 (kerf), c6d1bd16 (secure-dev), 79a42399 (secure-dev), 3bf5774c (harmonik), f588ff0c (harmonik), 2a50e0fc (machine-setup), d1704aa0 (secure-dev), 13493c8d (harmonik context-dump), 729dad16 (kerf session-recovery), 00eb9fc9 (harmonik short-recent).

---

## Findings

### Incident Table

| Session | Human turn | Type | Misalignment | Correction cost | Signal used |
|---|---|---|---|---|---|
| f588ff0c | H#4/H#5 | Scope/vocabulary | Agent listed file names (`architecture.md`, `node-types.md`) as if labels told the human what content was proposed. Human: "I don't know what you're talking about." | 1 correction turn; agent rewrote proposal describing content, not file labels. ~200 chars human wrote. | Human compared agent's output (a table of file labels) against what they actually needed to evaluate. Caught at next turn. |
| 3bf5774c | H#3 | Vocabulary/authority framing | Agent said "the mechanism is already locked" re: checkpoint pattern. Human pushed back: "we are still investigating — that seemed like a good decision at the time." | 1 turn; agent acknowledged and dropped "locked" framing going forward. ~150 chars. | Human caught immediately from the phrase "locked." Fast catch — a single word signaled the wrong posture. |
| 3bf5774c | H#4 | Structure/algorithm | Agent described in-flight state reconstruction using JSONL: "replays JSONL from last checkpoint per in-flight run to reconstruct position." Human: "WAIT — you just said JSONL wasn't used." | 1 turn, but required 3 sub-questions to fully untangle the JSONL-vs-git contradiction. ~350 chars human wrote. | Human noticed because they had just been told JSONL was observational-only, not state-deriving. Consistency check across turns. |
| 3bf5774c | H#4 | Assumptions about domain model | Agent described "SQLite queue" as "not a truth store." Human challenged: "If its not truth — what is it? Why isn't it just in memory?" Agent had applied a caching framing to Beads that did not match the human's mental model of Beads as authoritative for task content. | 1 turn; agent corrected ("I was sloppy") and gave a precise three-store model. ~200 chars. | Human caught via a definitional question: "what is it?" The misalignment was conceptual, not technical. Fast once the question was formed. |
| 3bf5774c | H#5 | False abstraction | Agent said memory was updated but human noted: "You updated a memory — but don't we want to be updating the spec?" Agent had been routing decisions to memory rather than spec, misunderstanding which store was authoritative for design decisions. | 1 turn correction; agent agreed and deferred the spec-write to a later batch. Small cost. | Human used the literal output ("Updating memory now") as the signal. The phrase itself exposed the wrong routing choice. |
| 3bf5774c | H#2 (agent #1 response) | Scope/boundary | Agent proposed starting with L3 (composition) and suggested skipping L1/L2. Human later confirmed L1/L2 but the agent had proposed a narrowed-scope conversation path that could have missed important alignment on foundational layers. | Pre-empted: human confirmed L1/L2 in their reply before agent could skip them. | Human caught by reading the agent's proposed "proposed flow for this session" — a structure they could evaluate and redirect. Low cost because the agent surfaced the plan before acting on it. |
| f588ff0c | H#2 (human turn, agent #2) | Scope/boundary | Agent proposed `ROADMAP.md` and a five-stage structure before the human mentioned `kerf`. Agent was about to invent a process document to replace a tool that already existed. Human interrupted: "run the 'kerf' command real quick." No actual mismatch yet — but near-miss. | 0 turns lost; human redirected before agent wrote anything. Very fast. | Human had tool knowledge agent lacked. Agent's proposal text (building a five-stage process document) was enough signal to trigger the redirect. |
| c6d1bd16 | H#5 | Authority/process | Agent completed phases 1-2 (spec + review) and asked "Before marking the spec as approved, would you like to review…?" Human's response revealed that the agent had been asking permission at each phase gate, which contradicted the human's stated preference for autonomous execution. Human updated the skill: "when tasked with a job, do not ask permission to complete steps that were assigned." | 1 turn + skill rewrite. This was a recurring assumption (permission-seeking was baked into the workflow skill). | Human noticed because the agent asked *again* at the same kind of gate where it had asked before. Pattern recognition across multiple gates within one session. |
| 38415843 | H#15, H#21, H#22 | Assumptions about tooling state | Recurring: agent incorrectly assessed ntm worker pane status via stale `tmux capture-pane` output. Reported workers "stalling on write permission" or "not running" when they were actually running fine. Human corrected three times: "You are not seeing what I'm seeing. They all seemed to complete fine." | Each correction: ~1-2 turns, ~50-100 chars. Total across 3 incidents: ~4 turns, ~250 chars, plus process-debugging time at H#22. | Human was watching the pane directly. Agent was relying on a capture method that returns stale snapshots. The human's correction signal was consistently "you are not seeing what I'm seeing" — a reliable early flag. Lingering misalignment: the agent never diagnosed the root cause until H#22 prompted investigation ("theres gotta be a better way"). |
| 2a50e0fc | H#18 | Assumptions about environment | Agent provided a rebuild command assuming Go was installed (`go build -o ~/.local/bin/adze ./cmd/adze/`). Human: "go is not installed yet." Agent had forgotten that Go gets installed by adze itself — chicken-and-egg. | 1 turn; agent acknowledged and fixed bootstrap sequence. ~30 chars. | Human's environment feedback. Fast catch but required a real-world attempt. Not detectable from the planning dialog alone. |
| 2a50e0fc | H#16 | Assumptions about codebase | Agent built the step config pipeline but never wired `stepConfigs` into the Runner. Bug not caught in planning or implementation; surfaced only during actual `adze apply`. Agent independently diagnosed via code reading. | Not a planning misalignment — implementation gap. Included as counterpoint: no amount of planning dialog would have surfaced this; it required running the code. |

---

### Pattern Analysis

**Lingering misalignment — 38415843 pane-capture problem**

The most significant lingering case. The agent's `tmux capture-pane` method gave stale data, and the agent never questioned its own tool choice. The misalignment lasted three correction cycles (H#15, H#21, H#22) over ~40 minutes before the human explicitly asked the agent to investigate. What finally surfaced it: the human explicitly named it as a recurring pattern ("you've had issues almost every time") at H#22. Earlier detection was available: the first "you are not seeing what I'm seeing" (H#15) should have triggered the agent to re-examine its status method, not just re-read the same stale output. The signal was present but the agent treated each incident as a one-off rather than a systemic tool issue.

**Fast catches — vocabulary and posture signals**

The fastest catches (3bf5774c H#3, H#5) were from single-word or single-phrase signals: "locked" (wrong framing for design stage), "Updating memory now" (wrong routing decision). Both human catches required minimal reading and the correction was immediate. The pattern: **abstract posture claims are more readable — and catchable — than buried structural decisions**. When the agent says what it's doing rather than just doing it, misalignment in intent surfaces on the same turn.

**Preempted misalignments — structure surfaced before action**

f588ff0c H#2 and 3bf5774c agent#1: both cases where the agent described its proposed plan or conversation structure before executing. The human could redirect without any work having been done. The f588ff0c case (kerf redirect) was zero-cost. The 3bf5774c case (L3 skip proposal) was caught because the agent listed its proposed session flow explicitly. **Pre-action plan disclosure is the strongest preemption mechanism observed in this corpus.**

**Human fuzziness contributing to misalignment**

c6d1bd16 H#2 contains a misalignment with partial human contribution: the human said "Lets figure out what we want to do. Do not start building." but the immediately prior statement was "Lets do both" and "Lets build out a comprehensive plan." The agent started building the spec immediately (not code — but still building). The human later said "Before anything else, let's get that set of steps defined." The instruction set was internally inconsistent; the agent correctly parsed "do not start building code" but then built the skill/workflow. This was not wrong given the instructions — the human had mid-stream refined their intent. This is the clearest case where the human's own fuzziness (not the agent's assumption) was the root cause.

**False precision — agent over-specifies and human pays attention cost**

In 3bf5774c, the agent listed 9 specific alignment points in H#4's response and asked "Do those 9 points feel right?" The human confirmed most with one correction. But the format imposed an overhead: the human had to evaluate 9 numbered points rather than 1 or 2 load-bearing questions. This is not a misalignment per se, but it is a pattern where the agent's false precision on a summary (treating all 9 as equally weighty) cost human attention. The actual new information was in points 3-5.

---

## Candidate planning protocols this lens suggests

**P1 — Plan disclosure before action.** Before executing a multi-step plan or a significant research/writing pass, the agent states the plan in one paragraph and asks for a redirect (not approval). Human reads 2-3 sentences and either stays quiet (proceed) or redirects with minimal writing. This was the strongest preemption mechanism observed. It is distinct from the current "ask permission at each phase gate" anti-pattern in c6d1bd16 — it is disclosure of plan, not request for approval.

**P2 — Single-word posture markers.** Agent signals its interpretation of the conversation's authority mode: "treating this as design-stage candidate" vs "treating this as locked decision." Cheap to write, cheap to scan. The 3bf5774c "locked" catch shows that one wrong word surfaces immediately if the human is scanning for it. A simple convention ("candidate:" or "treating as locked:") would give the human a fast-scan signal without reading full paragraphs.

**P3 — Stale-tool self-quarantine.** When an agent receives "you are not seeing what I'm seeing" once, it should immediately mark its current status-reading method as suspect and try an alternative before reporting status again. The 38415843 lingering case shows the agent re-applying a known-bad tool three times. The protocol: if a status method produces output that contradicts human observation, do not re-use the same method.

**P4 — Weighted question batching.** When the agent has multiple questions or alignment points, it flags the 1-2 genuinely load-bearing ones and handles the rest autonomously. The corpus shows the human's biggest frustration is trivia deferral, not load-bearing decisions. The agent can state "I'm deciding X myself; the call that matters is Y."

**P5 — Store-routing declaration.** When updating project state (a design decision, a resolved question), the agent states where it is writing: "Writing to spec / Writing to memory / Deferring to TASKS." The 3bf5774c H#5 incident shows this routing is invisible by default, but the human cares about it. Making it explicit is cheap.

---

## Open questions

- Does plan-disclosure-before-action slow sessions where the agent is on the right track? There is no evidence in this corpus of a case where disclosure would have wasted time — all detected preemptions were valuable. But the sample is small.
- Can the stale-tool pattern be addressed structurally (e.g., agent instruction: "if a status tool returns output the human contradicts, quarantine and try alternative") or does it require the human to build this into agent configuration?
- The human-fuzziness case (c6d1bd16) is important but this lens cannot prescribe a protocol for it — the ambiguity was in the human's message, not the agent's assumption. A separate lens (writing-load, form-vs-content) may have more to say about how to elicit tighter human intent earlier.
- The 3bf5774c JSONL contradiction (H#4) was caught within-session because the human had good recall of what was said 2 turns prior. Across sessions (session-recovery context), this kind of internal contradiction would be invisible. Is there a within-session consistency check protocol that could catch these before the human notices?

---

## Notes on variants

**13493c8d (context-dump).** With 5 very long human messages, there are almost no mid-course corrections visible in the transcript — the agent simply executed. The absence of misalignment incidents is not evidence of good alignment; it's evidence that the human was not present to catch drift. The context-dump protocol trades correction opportunity for execution throughput. The alignment risk is all in the up-front brief.

**729dad16 (session-recovery).** The session immediately surfaced a tooling-state question ("why are there 12 named agents?") that the agent diagnosed cleanly. Recovery context provided enough grounding that no design-level assumption misalignments occurred in the short session. The handoff format (structured recovery message) appears to function as a pre-emption mechanism — it constrains the assumption space before the agent acts.

**00eb9fc9 (short-recent).** This session is itself a meta-instance: it is the research framing session from which this corpus was drawn. Two relevant misalignment moments: (1) the agent framed the corpus as "15 sessions, not 195" which the human correctly pushed back on as a mis-characterization — the real dialog corpus is larger because planning is a phase within sessions, not a property of whole sessions. Agent corrected immediately. (2) The agent produced a 5-question batch at the start; human asked for one topic at a time. Both corrected within 1-2 turns but reinforce the pattern that the agent's default toward batching conflicts with the human's serial attention preference.
