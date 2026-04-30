---
name: session-resume
description: Read HANDOFF.md and continue the work. Reads ./HANDOFF.md by default, or a path passed as an argument.
---

# Session Resume

Read the handoff file (`./HANDOFF.md` unless a different path was passed) and any files it points at. Then read whichever of the project's standard start-of-session files exist — `CLAUDE.md`, `AGENT_INDEX.md`, `STATUS.md`, `TASKS.md` — for grounding.

Before doing any work, post a short message:

- One or two sentences in your own words on what you understand the situation to be and the next step. Plain English. If the handoff uses internal names, codes, or section numbers, translate them.
- The blocking question, if there is one — phrased so a human reading quickly can answer without looking anything up.
- Anything stale or contradicting the repo.

Then wait for the user to confirm or correct. Once they've responded, get on with the work using normal judgment — don't ask permission for routine choices. Translate any internal vocabulary when asking the user a question later in the session, too — the user is human, jargon walls them out.

If the handoff is missing, say so and ask how to start. Don't invent one.
