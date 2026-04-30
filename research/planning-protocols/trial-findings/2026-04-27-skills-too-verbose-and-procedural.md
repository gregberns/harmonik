# Trial Finding 1 — Verbose, Schema-Heavy Skills Produce Verbose, Procedural Output

> **Date:** 2026-04-27 (observations span 2026-04-27 through 2026-04-28 UTC)
> **Trial:** `/session-handoff` + `/session-resume` skill trial (PP-TRIAL:v1)
> **Status:** First named trial finding. Triggered v2 skill iteration on 2026-04-28 ([`../skill-iterations/v2-2026-04-28/`](../skill-iterations/v2-2026-04-28/)). v2 deployed 2026-04-28 to `~/.claude/skills/`; v1 archived at [`../skill-iterations/v1-baseline/`](../skill-iterations/v1-baseline/). n=3 test fires on next real planning session.

## Summary

When the agent operates inside a long, schema-heavy protocol, its mid-session output mirrors the input: long structured responses, permission-seeking on trivial choices, internal vocabulary used unmodified in user-facing questions. Two distinct conversations triggered direct user pushback with the same core complaint within ~24 hours. One was running `/session-resume`; the other was running an unrelated multi-stage review protocol with no `/session-handoff` involvement at all. The skills are an amplifier, not the sole cause — the underlying pattern is broader than this trial.

The protocols-as-ideas (`commanders-intent`, `back-brief-plan-quality`, `autonomy-scope-grant`, `load-bearing-token-readback`, `closing-summary-ritual`) may still be sound. The protocols-as-templates as currently authored are counterproductive in the cases observed.

This document separates **what occurred** (Part 1 — observations) from **what causes it** (Part 2 — analysis) so the evidence can be re-evaluated independently of the diagnosis.

---

## Part 1 — What occurred

Three conversations are relevant. Conversation IDs and line numbers are cited so the events can be re-examined in raw form.

### Conversation A — basata `0327c7b3` (predecessor; the session that wrote `HANDOFF.md`)

Project: `/Users/gb/github/basata`. No `/session-handoff` skill was active during the substantive work; the skill ran only at session end to write the handoff document. Evidence the jargon problem was already present:

- **L17 (early user turn):** *"I dont know what 'C4' is - make sure you're converting the internal jargon to something I will understand. We may have gone through other parts past C3 - but I dont remember now."*
- **L211, L213, L215 (later in session):** the user typed numbered-list answers ("1-6 fine, 7 remove, 8 remove…") three times in succession, correcting typos. The agent had been asking questions in a numbered-list shape; the user's repeated edits suggest they were fitting their answer to the question's shape rather than the other way around.

The session ended with `/session-handoff` writing `/Users/gb/github/basata/HANDOFF.md`:

- 150 lines
- 24 numbered "Decisions Made"
- **50+ "Load-Bearing Tokens"** enumerated verbatim (every Java type, every endpoint, every CSS-relevant variable)
- 30+ "Out of Scope" items
- 3 "Open Questions"
- 5 "Decisions Parked" with `routine` / `watch` / `escalate` severity tags

### Conversation B — basata `426257cc` (under `/session-resume`)

Project: `/Users/gb/github/basata`. Session opened by the user invoking `/session-resume`.

**L3–L4.** `/session-resume` command and full skill prompt (~5KB) loaded into context. The skill prompt mandates a 4-step procedure (read handoff → back-brief → token readback → stop), a worked example with three contrast variants for paraphrase quality, 5 behavior rules, and 5 failure modes.

**L7–L15 (agent back-brief).** Agent reads `HANDOFF.md` and produces back-brief in the exact prescribed format:

> "**1. Intent (my words).** … **2. Plan for first action.** … **3. One uncertainty.** …"

The token readback at the end enumerates 60+ tokens verbatim:

> "Tokens: kerf, kerf new, kerf show, kerf status, kerf shelve, kerf resume, kerf square, kerf finalize, StoredDocument, ProcessingDocument, Document, WorkflowInstance, WorkflowStatus, canonicalStatus, AWAITING_REVIEW, FLAGGED, AWAITING_APPROVAL, FILED, REJECTED, currentStep, TEXT_EXTRACTING, CLASSIFYING, EXTRACTING, FAILED, reextractionStatus, NONE, IN_PROGRESS, FAILED, llm_call_audit, …"

**L18 (user turn).** Single short sentence: *"Define the steps we should take next."*

**L19–L22 (agent response).** A 9-step procedural list. Steps 2, 4, 6, 7, and 9 marked **"Decision —"** with explicit user-decision prompts:

- "Decision — commit now or batch?" (commit timing)
- "Decision — advance kerf?" (status advance)
- "Decision — parallel or sequential?" (sub-agent dispatch shape)
- "Decision — change-spec format." (per-component vs single-file layout)
- "End-of-pass checkpoint." (future user OK)

**L23 (user pushback).**

> *"Damn - your so procedural now. Please use your best judegment, follow instructions when it will improve outcomes. … There shouldn't be anything defining whether you are 'allowed'/not allowed to run something in parallel or sequential - that is completely up to your judgement. Make sure to think about what is the best decision, then you can procceed.*
>
> *I don't know what 'change-spec' format is. That sure seems like you can think about and make a decision on.*
>
> *All of this seems like non-sense procedural bullshit. Why is this coming up? We are now trying the 'session-handoff' and 'session-resume' steps. Is there something occurring in those instructions that is causing you to ... I dunno be so ... why are you asking me to commit - its a trivial decision that absolutely doesnt matter. 'advance kerf' - you mean move on to the next step? Why wouldnt you just ask 'Are you ready to move on to the next step?' ??*
>
> *What is it thats making you ask in this way? Is there something in the instructions? Is it the framing? What do you think?*
>
> *I really need you to step back, look at what is happening, and THINK through why the decisions and language was used. Its signaling an issue with the session handoff/resume instructions or kerf or something."*

The user explicitly hypothesized the skills as cause.

**L25 (agent next response).** The agent self-diagnosed and named four mechanisms inside the skills. Verbatim text reproduced in Part 2.

### Conversation C — harmonik `a121e7f1` (NOT under `/session-resume`)

Project: `/Users/gb/github/harmonik`. The session was deep in implementation-discipline pilot review work — a multi-stage protocol unrelated to `/session-handoff` / `/session-resume`. The agent had spawned three parallel sub-agent reviewers (Coverage, Decomposition, Reference) on an event-model spec.

**L380–L403.** Sub-agents complete and return findings keyed as `F-pilot-EM-X`, `F-em-r1-X`, `em-inv-005`, `em-schema.checkpoint-trailers`, `AR-052`, `AR-053`, etc. The agent reads each review.

**L404 (agent synthesis to user).**

> *"EM r1 complete. Three reviewers: 0 BLOCKER, 4 MAJOR (3 class, 1 spec-edit), 10 MINOR (mostly class). The pilot's behavior is mostly defensible — most findings are documentation-tightening + 2 are spec edits requiring your input.*
>
> *## Findings requiring your decision (spec edits)*
>
> ***1. EM `depends-on` is too narrow vs actual citation graph (F-em-r1-7 / F-pilot-EM-2)***
>
> *EM `depends-on: [architecture]` but EM's normative §4/§5/§6 body has 50+ forward inline cites to event-model, handler-contract, control-points, reconciliation, workspace-model, beads-integration, operator-nfr, process-lifecycle. Pilot emits only 5 edges (all to AR); the bead set under-represents implementation-order constraints by ~10×.*
>
> *Three options: (A) Patch EM spec to expand `depends-on`. (B) Defer to corpus-scale reciprocal-pilot resolution. (C) Discipline carve-out.*
>
> ***2. EM is missing its §4.a Subsystem envelope per AR-053 (F-pilot-EM-3)***
>
> *EM is `runtime-subsystem` per AR-052; AR-053 mandates `§4.a Subsystem envelope` as the first §4 subsection. EM v0.3.3 starts at §4.1 — no §4.a present. …"*

**L428 (user pushback).**

> *"Going back to the decisions 'I need to make'. Why?*
>
> *I dont know what is in '§4/§5/§6 body'. I honestly have no idea what your talking about - and we've discussed that.*
>
> *What is it that you think requires me to answer that question?*
>
> *'EM is runtime-subsystem per AR-052; AR-053 mandates §4.a Subsystem envelope as the first §4 subsection. EM v0.3.3 starts at §4.1 — no §4.a present.'*
>
> *That sounds like insane legal bullshit.*
>
> *If that helps you to understand what needs to be done - cool - go for it. But if you think I should make a decision on something - you need to provide something in the english language that is understandable by the average - maybe slightly above average human."*

**Critical observation:** this pushback occurred without `/session-resume` being active in the session. The protocol the agent was operating inside (pilot review with its own taxonomy of finding lanes and cross-spec citations) produced the same kind of jargon-in-user-asks failure independently. Same shape, different protocol.

### Skill-file scale (for reference)

| Artifact | Lines | Notable |
|---|---|---|
| `~/.claude/skills/session-handoff/SKILL.md` | 104 | Mandatory schema with 8 sections; 7 behavior rules; 5 failure modes |
| `~/.claude/skills/session-resume/SKILL.md` | 102 | 4-step procedure; worked example with three contrast variants; 5 behavior rules; 5 failure modes |
| `/Users/gb/github/basata/HANDOFF.md` (produced by skill) | 150 | 24 Decisions Made; 50+ verbatim tokens; 30+ Out-of-Scope items; 5 Decisions Parked |
| `/Users/gb/github/harmonik/research/planning-protocols/HANDOFF.md` (produced by skill) | 85 | Smaller subject matter; same structural shape |

Each skill file is roughly an order of magnitude longer than the user's plain-English description of what they expected the skills to do: *"save all your stuff, note what needs to be done in HANDOFF"* / *"on the resume, just read the file plus related files and do these other procedures."*

---

## Part 2 — Analysis

The agent in basata `426257cc`, when called out at L23, produced its own diagnosis at L25. That diagnosis is treated here as primary evidence (the agent had direct introspective access to which parts of the skill prompt were shaping its output). It is reproduced first, then extended with cross-conversation observations.

### A. From the agent's L25 self-diagnosis (basata `426257cc`)

Verbatim reproduction of the four named mechanisms:

> ***Biggest offender: the Autonomy Scope section of /session-handoff.*** *It partitions decisions into "decide autonomously" / "ask first" lists, then closes with `If unsure, default to ask.` That last line converts the whole system from judgment to permission-seeking — any decision that doesn't perfectly match a bullet becomes an ask. It also encodes contingent observations as policy: the prior session noted "the user has been the gate on every pass advance" (a description) and I read it as a binding rule (a policy).*
>
> ***Second: the back-brief checkpoint in /session-resume*** *primes a confirm-then-stop posture for turn 1. That's correct for the back-brief itself, but it bleeds into turn 2 — when you said "define next steps" I produced more checkpoints instead of just doing the work.*
>
> ***Third: the Open Questions section of the handoff template.*** *It invited the prior-me to record every minor branch as a numbered question with no annotation about importance. So "should we commit now or batch?" sits alongside real questions with the same visual weight, and I treated all three as binding asks for this session.*
>
> ***Fourth, more subtle: "load-bearing tokens" / "drift detection"*** *installs an anxious-precision mindset for turn 1. Mirror exactly, don't substitute. That care is right for vocabulary, but the posture leaks operationally — every decision starts feeling like it needs the same care.*
>
> *The handoff/resume protocol is genuinely useful for catching big misalignments at turn 1 (vocabulary drift, fundamental intent mismatch). The bug is that the ceremony around it treats trivial operational moves with equal gravity. Kerf-advancing, commit timing, and format choice aren't decisions — they're just doing the work.*

For analytical use the four offenders separate cleanly into five distinct mechanisms (cause #1 above bundles two):

1. **`If unsure, default to ask` converts judgment to permission-seeking.** The autonomy-scope section partitions decisions into authorized vs not-authorized; anything the prior-session author didn't explicitly authorize lands in "ask." The default-to-ask line is the trapdoor.

2. **Descriptive notes get encoded as policy.** A handoff observation like "the user has gated every pass advance" is descriptive in origin but reads as prescriptive when the next agent ingests it. The handoff template offers no slot for distinguishing description from prescription.

3. **The back-brief checkpoint posture bleeds past turn 1.** "Stop, paraphrase, confirm" is right for the back-brief itself but persists into the second and third turns. Every action accumulates checkpoints when none are needed.

4. **Open Questions section has no severity weighting.** Format choice sits at the same visual weight as architectural reversal. The agent treats both as binding user-decision points and re-asks them.

5. **Load-bearing-token-readback installs an anxious-precision mindset.** "Mirror tokens exactly, don't substitute" is right for vocabulary; but the careful-and-precise mindset leaks operationally to non-vocabulary decisions.

### B. Additional causes from cross-conversation analysis

6. **Output style matches input style.** A 100-line prescriptive skill prompt sits in the agent's context throughout the early conversation. Its tone — *"Behavior rules: 1. Source of truth is the conversation, not assumptions…", "Failure modes to avoid: …"* — shapes the agent's own output for several turns. Long, structured, careful prompts produce long, structured, careful responses. This is implicit, not by rule.

7. **No outward-translation discipline.** The token-readback step mirrors *internal* vocabulary at session start: the agent learns these tokens are sacred, mirror them exactly. The skill never says "translate them when asking the user a plain question." So when the agent has a structured internal world (kerf passes, spec subsystem identifiers, pilot-review finding codes), its user-facing questions use the internal vocabulary unmodified. (`§4/§5/§6 body`, `em-inv-005`, `change-spec format`, `AR-053` all appeared this way.)

8. **The handoff is framed as a "contract."** Skill behavior rule 1: *"Source of truth is the conversation, not assumptions. … Reasoning lives in the conversation; this file is the contract."* The contract framing nudges toward formal, deferential, precise behavior. Contracts get negotiated and signed; brain-dumps get scribbled and tossed. The user's plain-English description of what they wanted was closer to the latter.

### C. Second-order effect — schema invites filling

Eight required sections motivate the agent to populate eight required sections, even when half of them are noise. Concrete evidence:

- **The basata HANDOFF.md lists 50+ load-bearing tokens.** The genuinely load-bearing ones are perhaps 10 (`kerf`, `StoredDocument`, `ProcessingDocument`, `Document`, `WorkflowInstance`, `make test`, `ProcessingStepChanged`, `DocumentStateChanged`, `llm_call_audit`, `V1__init.sql`). The remaining 40 (every CSS-friendly status name, every dashboard stat field, every pass-name) are filler — present because the section exists and demands content.

- **The same HANDOFF lists 30+ "Out of Scope" items**, most of which were never proposed during the session. The section's existence converted "things we explicitly cut" into "things to enumerate in case anyone wonders."

- **The basata HANDOFF lists 24 numbered "Decisions Made,"** including line items like "Two commits made this session: `51f6ecb` and `e1dc2f4`." A commit hash is a `git log` lookup, not a decision. The Decisions Made section absorbed it because it had to absorb something.

### D. The harmonik conversation extends the finding

The harmonik failure occurred without `/session-resume` involvement. The agent was inside a different structured protocol (multi-stage pilot review with its own internal taxonomy of finding lanes, finding codes, and cross-spec citations). The same jargon-in-user-asks failure occurred. This means:

> The deeper pattern: whenever the agent operates inside a structured internal world, it forgets to translate when talking to the human. Long structured protocols with internal vocabulary produce user-facing asks in that internal vocabulary.

The `/session-resume` skill is one instance of this pattern. The pilot-review protocol is another. Kerf passes (with their own per-pass jig vocabulary) are likely a third. The general result is bigger than the trial.

---

## Part 3 — Implications

### For the active trial

The trial roadmap (`protocol-trial-roadmap.md`) listed *"which felt like ceremony?"* as a question to watch. The signal from this finding: **most of the schema-form scaffolding, when implemented as a mandatory template.**

The protocols-as-ideas may still be sound: `commanders-intent`, `back-brief-plan-quality`, `autonomy-scope-grant`, `load-bearing-token-readback`, `closing-summary-ritual`. The protocols-as-templates as currently authored are counterproductive in two of two real-session uses observed.

Sample size caveat: this is two sessions, not the 3–5 the methodology asked for before structural changes. But the user pushback in both cases was strong and specific, and the agent's introspective self-diagnosis was sharp. The signal is unusually clean for an n=2.

### Provisional alternative shape (recorded as direction; not authorized to implement)

Encode the same protocol intent as the agent's standing **disposition** rather than a mandatory schema. Examples:

- Replace `Autonomy Scope: Decide / Ask first / If unsure, default to ask` with a one-liner: *"act on judgment; ask only when an action is hard to reverse or clearly user-visible."*
- Replace `Load-Bearing Tokens: verbatim list` with *"if the next session uses these terms, mirror them; don't paraphrase domain vocabulary"* — and add an outward-translation rule: *"when asking the user a question, use plain English even when the internal work uses jargon."*
- Replace `Decisions Parked with severity tags` with *"flag what's still open and important; trivial branches don't need to be questions."*
- Drop `Out of Scope` as a required section unless the prior session actually had scope arguments.
- Drop the worked example (3-variant paraphrase contrast) from `/session-resume` — the agent doesn't need an example to paraphrase; carrying the example in context advertises "this task is delicate, be careful," which is the wrong signal.

Whether this disposition-form preserves the benefit while shedding the cost is itself a trial question.

### Higher-order implication for the planning-protocols research

Phase 2 evaluated protocols against multi-framing reviewers and flagged **trap candidates** (high on the provisional framework, low on Framing C / regret-adjusted outcome). This trial signal points at a different class of trap, currently absent from the evaluation framework: **protocols that read well as ideas may behave badly when implemented as detailed templates.** The mechanism is implicit: long structured prompts produce long structured outputs, and that procedural style undermines the goal (reduce user-attention cost; preserve plain communication).

This may warrant adding a new dimension to `evaluation-framework.md`: **implementation-form risk.** Specifically: does the protocol, when implemented as agent prompting, induce an output style that mismatches its purpose? The current framework evaluates the protocol idea; it doesn't evaluate whether the implementation form preserves the idea or distorts it.

Recording this for possible inclusion in a Phase 2 addendum. Not modifying the existing `evaluation-framework.md` per the append-only convention.

---

## Part 4 — Open questions / followups

(Not action items for this session. Recorded so a future session can address them.)

1. **Does load-bearing-token-readback amplify mid-session terminology use?** User-flagged hypothesis: mirroring internal tokens at session start may anchor the agent on those tokens for the rest of the session, reducing translation effort outward to the user. Verifiable by comparing token-counts in mid-session output across PP-TRIAL:v1 sessions vs non-trial sessions on similar work. Could be a Step 4.5–style transcript-only measurement if Step 4.5 ever runs.

2. **Priority weighting on Open Questions / parked items, without prescribing a level scheme.** Earlier (Opus 4.6) the agent used P1/P2 levels naturally; current behavior (Opus 4.7) uses major/minor or other ad-hoc levels that don't consistently surface only high-priority items. The user does not want a prescribed scheme. Open: how to encode "raise high-priority only" as an agent disposition without naming the levels?

3. **Skill-vs-CLAUDE.md placement for mid-session disciplines.** The trial roadmap parks `load-bearing-token-readback per-turn` and `back-brief-plan-quality at plan-draft moments` as CLAUDE.md candidates. Given this finding (verbose protocols backfire), should mid-session disciplines be even shorter — one-line dispositions in CLAUDE.md — or skipped entirely until the disposition-form vs schema-form question is settled?

4. **Generalization to other long protocols.** Pilot-review (harmonik), kerf passes, and `/session-handoff`+`/session-resume` (basata) all show the same pattern. Is this a general result about agent prompting (long structured prompts induce procedural output regardless of subject), or specific to "structured planning protocols"? If general, it has implications well beyond this research track.

5. **Adopt-and-notice n=2 vs accumulate-further.** This finding came from two real-session pushbacks within ~24 hours. Methodology asked for 3–5 sessions before adopt/shed structural decisions. The pushback strength here is high; the cost of continuing to use the current skills is also high (they actively damage the alignment they were meant to improve). Open: does the rule mean "don't act on n=2," or "n=2 is enough when the signal is this clean"?

6. **Why was the agent's L25 self-diagnosis so sharp?** It named four mechanisms accurately within one turn. Either the agent has decent introspective access here, or the user's pushback message was specific enough to do most of the diagnostic work. If the former, the same self-diagnosis prompt ("step back, look at what is happening, THINK through why the decisions and language was used") may be a usable disposition itself.

---

## Source material

- Conversation A (predecessor): `~/.claude/projects/-Users-gb-github-basata/0327c7b3-978a-4c08-b19c-45aad29b9237.jsonl`. User jargon flag at L17; numbered-list-close pattern at L211/L213/L215.
- Conversation B (`/session-resume` failure): `~/.claude/projects/-Users-gb-github-basata/426257cc-5b63-42d0-b0d5-a0f8916cac55.jsonl`. Skill load at L3–L4; agent back-brief at L7–L15; user pushback at L23; agent self-diagnosis at L25.
- Conversation C (independent failure, no `/session-resume`): `~/.claude/projects/-Users-gb-github-harmonik/a121e7f1-520b-450b-b1e1-6aeb33e4849b.jsonl`. Sub-agent reviews complete at L380–L403; agent synthesis at L404; user pushback at L428.
- Skill files: `~/.claude/skills/session-handoff/SKILL.md` (104 lines); `~/.claude/skills/session-resume/SKILL.md` (102 lines).
- HANDOFF documents produced by the skill: `/Users/gb/github/basata/HANDOFF.md` (150 lines); `/Users/gb/github/harmonik/research/planning-protocols/HANDOFF.md` (85 lines).
