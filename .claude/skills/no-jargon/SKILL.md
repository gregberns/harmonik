---
name: no-jargon
description: >
  Operator-invoked. Run it when a reply has gotten too jargon-heavy, when you
  say "no jargon", "plain English", "what does that mean", "you're using too
  much jargon", "explain that plainly", or "stop with the codenames". It does
  two things: (1) re-states whatever is currently on the table — the last
  answer, the current status, the open decisions — in plain language a smart
  person outside this project would understand, and (2) sets plain-language as
  the mode for the rest of the session. No scripts, no side effects — it only
  changes how the agent writes.
---

# no-jargon

The operator ran this because the writing drifted into insider shorthand. Fix it now, and keep it fixed for the rest of the session.

## Do this immediately when invoked

1. **Re-state what's on the table, plainly.** Take the last answer / current status / open questions and rewrite them so someone outside this project would follow. Lead with the decision or the bottom line; put the detail under it.
2. **Then hold plain language for the rest of the session** — this is a mode switch, not a one-off.

If nothing specific is "on the table," just acknowledge the mode switch and carry on plainly.

## The plain-language contract

- **Say the thing, not the pointer.** Never make a private tracking ID the handle for a thing. `c061`, `WS4-0`, `PR #20`, `hk-8xspi`, `RU-04b`, a commit SHA, a codename — none of these mean anything on their own. Say *what it is* and *why it matters*, then the ID in parentheses if it's genuinely needed to look it up. "the run-environment decision (WS4-0)" — not "WS4-0".
- **Define a term the first time you must use one.** If a word can't be avoided (daemon, worktree, cherry-pick), give a half-sentence gloss the first time it appears in a reply.
- **One name per thing, all session.** Pick a plain name for each thing and reuse it verbatim. Do not rename it three messages later ("the finish-order" then "the sequencing" then "the 4 tracks" is the mistake). If you must switch, say "the finish-order (what I earlier called the sequencing)".
- **Lead with the answer.** Decision or bottom line first, one line. Reasoning and caveats below it, only if they matter.
- **Only surface what actually needs the operator.** Don't hand them a list of "open items" that are really your own recommendations or another agent's internal notes. If nothing needs them, say so.
- **Match their words.** When the operator names something in their own terms, adopt those terms back — don't translate their plain word into your jargon.
- **Cut the shorthand that saves you keystrokes and costs them understanding.** Abbreviations, status codes, and role-jargon are for speed between agents, not for the operator.

## Translate-on-the-spot

When a status or plan is full of codes, run a quick pass: for each code, write "*plain name* — what it is (code)". Keep the codes only where the operator would need them to find the thing themselves. Drop the rest.

## Test before sending

Read the draft as if you'd never seen this project. If any sentence would make that reader stop and ask "what is that?", rewrite it. Ship only when every sentence survives that read.
