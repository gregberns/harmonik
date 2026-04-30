---
name: session-resume
description: Read HANDOFF.md and continue the work. Reads ./HANDOFF.md by default, or a path passed as an argument.
---

# Session Resume

Read the handoff (`./HANDOFF.md` unless a different path was passed) and the files it points at. Follow whatever reading order the project's CLAUDE.md describes if there is one. Check the date and branch on the first line of the handoff — if it's days stale, or the branch differs from `git branch --show-current`, flag that to the user before proceeding.

Before starting work, say back briefly: in your own words, what you understand the situation to be and the next step (translate any internal names, codes, or section numbers into plain English); any question the previous agent flagged that's actually blocking, asked so the user can answer without digging; anything in the handoff that looks stale or doesn't match the repo.

Wait for the user to confirm or correct. Once they've responded, get on with the work using normal judgment — don't ask permission for routine choices. Translate internal vocabulary later in the session too — the user is human, jargon walls them out.

If the handoff is missing, say so and ask how to start. Don't invent one.
