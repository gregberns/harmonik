# Anti-Anchored Fresh Draft — /session-handoff and /session-resume

Designed from the brief alone, without reading the existing skill files or the trial-finding diagnosis. Goal: short, plain-English, judgment-preserving.

---

## /session-handoff

```markdown
---
name: session-handoff
description: Write a short HANDOFF.md so the next session can pick up where this one left off. Use at session end or when context is filling up. Optional argument: a path other than ./HANDOFF.md.
---

Save your work, then write a handoff for the next session's agent. That agent has none of this conversation — they only have the file you write and the repo.

Before writing:
- Make sure work-in-progress is saved (commits, shelved planning works, scratch notes filed where they belong).
- Note anything that's mid-edit and not yet on disk.

Write the handoff to `./HANDOFF.md` (or the path passed as an argument). Keep it short — aim for under 40 lines. Cover, in plain English:

- What we were working on, in one or two sentences.
- Where it stands right now: what's done, what's in progress, what's the immediate next step.
- Anything the next agent needs to know that isn't obvious from the repo — a decision we just made, an option we ruled out, a thing the user said to remember.
- Files or docs they should open first.
- Open questions for the user, if any. Phrase them in plain English — if you'd normally use a project-internal name or code, translate it.

Skip boilerplate. Skip sections that have nothing to say. If something is already well-captured in a committed doc, point at the doc instead of restating it. A 20-line handoff that says the right things is better than a 100-line one that pads.

When done, tell the user the path and a one-line summary of what's in it.
```

---

## /session-resume

```markdown
---
name: session-resume
description: Read the HANDOFF.md the previous session left and continue the work. Reads ./HANDOFF.md by default, or a path passed as an argument. Use at the start of a session that's continuing prior work.
---

Read the handoff file (`./HANDOFF.md` unless a different path was passed) and any docs it points at. Then read the project's normal start-of-session files if the project has them (CLAUDE.md, AGENT_INDEX.md, STATUS.md, TASKS.md — only the ones that exist).

Before doing any work, post a short message to the user with:

- One or two sentences in your own words on what you understand the situation to be and what the next step is.
- Any open questions the previous agent flagged — phrased in plain English. If the handoff used a project-internal name, code, or section number, translate it so a human reading quickly can answer without lookup.
- Anything in the handoff that looks stale or contradicts what you see in the repo.

Then wait for the user to confirm or correct before proceeding. Once they've responded, get on with the work — use your normal judgment, don't ask permission for routine choices.

If the handoff file is missing, say so and ask the user how they'd like to start.
```

---

## Rationale (~150 words)

The two skills are deliberately thin. The handoff prompt names what to cover and immediately tells the agent to skip anything boilerplate — verbose prompts produce verbose output, so the prompt itself models the brevity it wants. There is no template, no required sections, no headings list; just a short list of things to cover when they apply, plus an explicit "skip what doesn't apply." The resume skill front-loads one specific behavior that protects against silent drift: paraphrase the situation back to the user in plain English before doing work, with project-internal vocabulary translated. That single back-brief turn catches the two failure modes that matter most — misread state and jargon walls — without installing permission-seeking on routine moves. After the back-brief, the agent is told to use its own judgment. Both skills accept an optional path argument so parallel work streams in one repo don't collide.
