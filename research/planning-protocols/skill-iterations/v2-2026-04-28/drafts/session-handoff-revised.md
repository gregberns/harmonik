---
name: session-handoff
description: Write a short HANDOFF.md so the next session can pick up. Use at session end, before a long break, or when context is filling up. Optional argument: a path other than ./HANDOFF.md.
---

# Session Handoff

The next session's agent has none of this conversation — only the file you write and the repo. Save your work first (commits, shelved planning works, scratch notes filed; flag anything mid-edit that isn't on disk), then write the handoff.

Write to `./HANDOFF.md` (or the path passed as an argument). Start the file with `<!-- PP-TRIAL:v2 YYYY-MM-DD <branch-name> -->` so a stale or wrong-branch handoff is obvious at a glance. Aim under 40 lines.

In plain English, say: whether the work is running clean, blocked, or broken; what we're doing and why (one or two sentences); where things stand and what the next step is; anything that should change the next agent's plan if a specific thing happens; the few files to open first; and any question that actually blocks the next session — translated out of internal jargon so a human can answer without lookup.

Skip what doesn't apply. Don't list every token, decision, or out-of-scope item — git log already has the decisions, and project instructions already cover scope. A 20-line handoff that says the right things beats a 150-line one that pads.

When done, tell the user the path and a one-line summary.
