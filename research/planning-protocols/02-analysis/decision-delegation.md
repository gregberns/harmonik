# Analysis Lens: Decision Delegation

Generated: 2026-04-23

## What this lens looks for

This lens tracks every instance in the corpus where a decision was made: who made it (agent alone, agent asked human, human preempted), what type of decision it was (trivial implementation detail, architectural/scope, requirements clarification), and whether the call to ask or self-decide was the right one. The primary signal for quality is the human's response — bored "fine", engaged deliberation, or correction.

## Methodology

Read all 10 sessions in the corpus (7 primary, 3 variants), cataloging decision moments turn by turn. For each: who held the decision, what domain it was in (naming, file layout, data structure, tool selection, interface contract, behavior boundary, requirements), what preceded it (did the agent give reasoning? did prior human text grant autonomy?), and how the human responded. Tallied autonomy grants (explicit "you decide" language) separately from implicit grants (behavioral, from context). Sessions were read in full; the 38-turn 79a42399 and 31-turn 38415843 received the most detailed passes.

---

## Findings

### 1. The agent over-asks about implementation structure, under-asks about behavior contracts

The clearest pattern across the corpus: agents repeatedly asked the human to choose among structurally equivalent options (file layout, naming conventions, split vs single files) while making consequential behavior decisions autonomously.

**Over-asking examples:**

- **38415843 agent #2**: Presents a 15-file spec layout, then asks "Is the spec file list right? Too many? Too few? Wrong boundaries?" — three questions simultaneously, all about file packaging that the human had explicitly said to optimize for coding agents, not humans.
- **38415843 agent #2**: "One `commands.md` or per-command — your preference?" — structurally equivalent choices; the human responds "one file is fine for now - can break up later" (38415843 human #3), a flat "I don't care."
- **38415843 agent #3** (after human #3): Immediately proposes `_plan.md` naming and asks about it. Human's actual response: "Do we want to make it `_plan.md`? I have no strong preferences." This turn reveals the pattern from both sides — the agent asked, the human confirmed no preference.
- **f588ff0c agent #3**: Asks two questions simultaneously: which jig (plan vs spec), and what scope (one work vs multiple). Human responds with a clear preference on spec-first but defers on scope: "I don't have a strong lean at this point… whatever you think is best" (f588ff0c human #3).

**Under-asking examples (agent decided autonomously, human had to correct):**

- **f588ff0c agent #1**: Proposes a 5-stage roadmap (`ROADMAP.md`) as a new document. The human redirects: "run the 'kerf' command real quick. We can use that" (f588ff0c human #2). The agent had elaborated a proposal for a process-level artifact without realizing an existing tool already covered it. Wrong autonomous call.
- **f588ff0c agent #3**: After human confirms spec-first, agent kicks off `kerf init` and a foundation work, then asks two questions about what belongs in "foundation." Human #4 pushes back hard: "you're asking me what should be in foundation and you're telling me what should be cut. Do you see the gap... what are you proposing is in foundation??? I don't know what you're talking about." The agent had been using file names (labels) as if they communicated content. Wrong autonomous framing.
- **3bf5774c agent #1**: Proposes jumping straight to Layer 3 (composition vocabulary) and skipping L1/L2 confirmation. Human #2 actually confirmed L1 but pushed back on the agent's framing of locked decisions: "we are still investigating what this system looks like. That seemed like a good decision at the time." Agent had characterized decisions as "already locked" when they were design candidates. Wrong call — the human wanted to treat everything as revisable.
- **3bf5774c agent #3**: Describes three "stores" and refers to Beads as "Not a truth store; rebuildable from git + JSONL." Human #4 immediately pushes back: "Its not? When a bunch of tasks are ingested into the system - the event log is the source of truth??" The agent had autonomously made an architectural framing call (Beads = cache) that the human contested. This required a full correction turn (agent #4) and then multiple alignment-check turns.

### 2. Explicit autonomy grants appear reactively, rarely proactively

Scanning all human turns for "you decide", "whatever you think", "I don't care", "use your discretion":

| Session | Turn | Exact text | What preceded |
|---------|------|-----------|---------------|
| 38415843 | human #3 | "I'll leave that to your discretion" (×2), "I don't have strong preferences" | Agent asked 4 questions simultaneously |
| 38415843 | human #3 | "one file is fine for now" | Agent asked file-split question |
| f588ff0c | human #3 | "whatever you think is best" | Agent asked about scope of first kerf work |
| 79a42399 | human #1 | "If you have questions, ask them. If there are trivial details - solve them. If there are critical decisions, ask." | Session opener |
| c6d1bd16 | human #4 | "A: Sounds good / B: git is fine for now / C: Whatever you think is fine for now, we can iterate on that later" | Agent presented A/B/C on 3 questions |
| 3bf5774c | human #2 | "I don't care too much what we discuss next — unless I lead us a particular direction, just pick" | Agent asked which thread to start |

**Pattern: every explicit grant was reactive.** The human granted autonomy *after* the agent asked a question the human considered trivial. None of the sessions show a proactive autonomy framework at session open (except 79a42399 human #1, which is the only opener in the corpus to explicitly partition decisions: "trivial → solve them; critical → ask"). That opener was also the session with the most effective delegation behavior.

**Asymmetry between the two kerf sessions:** 38415843 is the session with the most autonomy grants per turn; it is also the session where the agent had already built up a strong structural proposal and was asking which packaging to use. The human's grants were essentially "I don't care about packaging." The c6d1bd16 session (different project, same human) shows the same behavior when agent presents A/B/C options. In both cases the human did the work of classifying the question as trivial via their response, rather than the agent classifying it proactively.

### 3. Decision type correlates strongly with delegation quality

| Domain | Agent typically: | Quality |
|--------|----------------|---------|
| File naming / split decisions | Over-asks | Poor — human consistently "I don't care" |
| Library or tool selection | Asks, then acts on "whatever" | Acceptable — agent often has the right pick anyway |
| Review persona composition | Decides alone | Good — never challenged |
| Commit strategy / timing | Decides alone (proceeds to commit) | Good — human approves |
| Architecture framing / "is X a cache?" | Decides alone | Often poor — human corrects when wrong |
| Behavior semantics ("what does this state mean?") | Decides alone | Often poor — see 3bf5774c Beads-as-cache example |
| Scope of work / what to tackle next | Asks | Good when human engaged; but human sometimes defers |
| Process structure ("how do we approach this?") | Proposes, waits | Generally good |

The domain with the worst delegation mismatches is **architecture framing and behavior semantics** — consequential decisions the agent makes silently and the human discovers later during explanation. These are exactly the decisions the human should be in on. The domain with the most over-asking is **file and structural packaging** — naming, split vs single, directory organization — where the human has consistently zero preference.

### 4. "A or B" turns where the human clearly had no preference — wasted turns

Quantifying in the primary sessions:

- **38415843 agent #2 → human #3**: 4 questions, 3 of which received "I don't care / your discretion / leave it to you." At minimum 3 wasted sub-questions.
- **79a42399 agent #3 → human #4**: A/B/C questions on 3 separate dimensions. Human response: "A: Sounds good / B: git is fine / C: Whatever you think." Full "whatever" on C.
- **f588ff0c agent #3 → human #3**: Two questions; one ("which scope?") gets "whatever you think is best."
- **3bf5774c agent #2**: Asks whether to confirm L1/L2 first or jump to L3. Human defers: "just pick what the next thing we should discuss."

Conservatively: **6-8 "wasted" A-or-B question-pairs** across the primary sessions where the human had no preference. Each costs one human turn. In a 20-turn session, that's 30-40% of turns consumed by questions the agent could have answered by defaulting.

### 5. Signal that precedes over-asking: lack of an explicit autonomy framework

The session with the best delegation behavior (79a42399) has this in human #1: "If you have questions, ask them. If there are trivial details — solve them. If there are critical decisions, ask." This partitioning instruction is the only case in the corpus where the human proactively set a decision rule.

All other sessions lack this. The agent defaults to asking whenever uncertain, regardless of whether the decision is trivial. In 38415843 human #3 and c6d1bd16 human #4, the human eventually *patches* this in response to over-asking — but the patching costs a turn and the grant is often local ("fine for now") not a general rule.

**Corollary:** once the human has granted general autonomy on trivia (as in 79a42399), the agent does appear to internalize it for the remainder of that session. The pattern doesn't re-emerge. But it does re-emerge in subsequent sessions (different JSONL = no memory of the grant).

### 6. Batch-question problem amplifies perceived over-asking

Agents frequently batch 3-5 questions in a single turn, some critical and some trivial. The human must address the batch entirely or let the trivial ones hang. This inflates human writing load and makes it hard for the agent to distinguish which answers were authoritative vs throwaway.

**Worst examples:**
- 38415843 agent #2: 4-part question block
- f588ff0c agent #3: 2-part question with A/B/C sub-options under each
- 79a42399 agent #3: A/B/C block on 3 independent design dimensions

00eb9fc9 (the most recent session, where the human is actively trying to research this exact problem) explicitly names this as a pain point: "this is another pattern we want to try and limit — large question and answer batches... Taking one area at a time is often easier to handle" (00eb9fc9 human #2).

---

## Candidate planning protocols this lens suggests

- **Autonomy-partitioning opener.** The session opens with an explicit rule: "trivial implementation details (naming, file layout, tool selection) → decide yourself; architectural or behavior-contract decisions → ask me; requirements clarifications → always ask." Single sentence. Only 79a42399 has this; it's the best-delegating session. Hypothesis: include this in session templates or agent system prompts.

- **Decision-type self-labeling before asking.** Before asking a question, the agent states: "This is [trivial / architectural / requirements-clarification]. I'm asking because [reason]." If the agent labels its own question as trivial and still asks, that's a signal to the human to grant blanket autonomy on that domain. If it labels it architectural, the human knows to engage. This makes the implicit explicit. Not present in any session currently.

- **Deferred-decision batching.** Instead of asking questions mid-pass, the agent lists all open decisions at the end of the pass: "Here are decisions I made autonomously [list]; here are decisions I'm holding for you [list]." Human reviews the batch at their own pace rather than being interrupted. The batch/interrupt problem is named in 00eb9fc9 and seen structurally throughout.

- **Behavior-contract check before silent architecture framing.** When the agent is about to characterize a component's role (Beads is a cache; git is truth; X is authoritative for Y), it should surface the framing as a question, not assert it. This is the failure mode in 3bf5774c (Beads framing) and f588ff0c (foundation file labels). Low ask-cost; high correction-prevention value.

---

## Open questions

- Does the autonomy-partitioning opener work across sessions, or only within the session where it's stated? The evidence (79a42399) shows it works within session, but there's no cross-session test in the corpus.
- Can agents reliably classify their own questions as trivial vs architectural before asking? The corpus doesn't test this; it would require deliberate protocol intervention. If agents can't self-classify, the decision-type labeling protocol has no leverage.
- Is there a domain taxonomy stable enough to embed in a system prompt? The evidence suggests: file structure / naming / split decisions = almost always trivial. Architecture framing / behavior semantics = almost always ask. Tool selection = context-dependent. Testing this taxonomy across future sessions would strengthen or falsify it.
- The batch-question pattern shows up in high-ht sessions where the agent is processing rich context. Is the batch a byproduct of long autonomous runs (agent gathers many uncertainties and surfaces them together), or of a specific question-generating behavior? A context-switch lens question but relevant here.

---

## Notes on variants

**13493c8d (context-dump, 5 human turns):** Zero A-or-B questions from the agent. The agent is executing a brief, not co-designing. Decision delegation is implicit: every decision that isn't in the brief is agent-owned. This is the extreme of the autonomy-partitioning idea — the human pre-solves it by writing a comprehensive brief. No wasted decision-asking turns, but the "briefing cost" is very high (5294-char first turn). This variant trades human-upfront-writing cost for human-in-loop cost.

**729dad16 (session-recovery handoff, 14 turns):** Almost entirely tooling/environment debugging turns. The human is answering "what do you see?" questions rather than making design decisions. The only planning-adjacent turn is human #4's brief: "I don't remember how I installed either one... investigate and figure out the best path." Explicit delegation. No over-asking observed in the debugging context — the agent asks only to diagnose specific state uncertainty, which is appropriate. This variant's delegation profile is dominated by its non-planning nature.

**00eb9fc9 (recent short, 5 turns):** This session is meta — the human is explicitly trying to research planning protocols. The most interesting signal: in agent #2, the agent proposes "five load-bearing questions" to scope the research statement, then in the same turn immediately names "decision delegation" as an item the human flagged as a likely top-lever. The human response (human #2) confirms: "Often the agent wants me to decide everything, and there are many decisions it can be making — implementation details that in my opinion are trivial." This is the only session where the problem is named directly and becomes part of the discussion object. The human also demonstrates the corrective pattern: "I've done some experimentation with this — but haven't figured it out."
