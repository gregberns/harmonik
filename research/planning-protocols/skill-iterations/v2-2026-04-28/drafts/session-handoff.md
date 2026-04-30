---
name: session-handoff
description: Write a short HANDOFF.md so the next session can pick up. Use at session end, before a long break, or when context is filling up. Optional argument: a path other than ./HANDOFF.md.
---

# Session Handoff

The next session's agent has none of this conversation — only the file you write and the repo. Save your work, then write a short handoff.

Before writing, make sure work-in-progress is saved (commits, shelved planning works, scratch notes filed). Note anything mid-edit that isn't on disk.

Write the handoff to `./HANDOFF.md` (or the path passed as an argument). First line is `<!-- PP-TRIAL:v2 -->` (greppable). Aim under 40 lines. Cover only what applies:

- **Status.** One word — `green` (running clean), `blocked` (waiting on user or external), or `broken` (something the next agent needs to know up front).
- **What we're doing.** One or two sentences. The why, not the how.
- **Where it stands.** What's done, what's in flight, the immediate next step.
- **If-then.** Conditions worth flagging: `if X happens, then do Y`. Only items with a real trigger. A parked item with no trigger is a TODO, not a handoff item.
- **Open question for the user.** Only if it actually blocks the next session. Phrase it in plain English — if you'd normally use a project-internal name, code, or section number, translate it.
- **First files to open.** The few files the next agent should read first. Point at committed docs rather than restating them.

Skip boilerplate. Skip sections with nothing real to say. Don't list every token, decision, or out-of-scope item — git log is authoritative for decisions; project instructions cover scope. A 20-line handoff that says the right things beats a 150-line one that pads.

When done, tell the user the path and a one-line summary.
