# R4 — Plain Language Review

Reviewing `session-handoff.md` and `session-resume.md` only for tone, formality, and jargon. The drafts are already in pretty good shape — most of the prose reads casual. A handful of phrases tighten up.

## session-handoff.md

- **"First line is `<!-- PP-TRIAL:v2 -->` (greppable)."** — Reads like a spec clause. Plainer: "Start the file with `<!-- PP-TRIAL:v2 -->` so it's easy to grep for."
- **"Only items with a real trigger. A parked item with no trigger is a TODO, not a handoff item."** — "Real trigger" / "handoff item" is starting to coin a category. Plainer: "Only include things that actually fire on something. If there's no trigger, it's just a TODO — leave it out."
- **"git log is authoritative for decisions; project instructions cover scope."** — "Authoritative" is the legalistic note in this file. Plainer: "git log already has the decisions, and the project instructions already cover scope."
- **"Cover only what applies"** followed by a bulleted menu — fine, but the bullets themselves use bold-label-period-sentence formatting (**Status.** **What we're doing.** etc.) which reads form-like. Could just be hyphens with a lead-in: "What's the status — green, blocked, or broken?" The current shape isn't broken, just stiff.
- **"Skip boilerplate. Skip sections with nothing real to say."** — Good. No change.

## session-resume.md

- **"Before doing any work, post a short message:"** — "Post a short message" is fine but a hair procedural. Plainer: "Before starting, say back briefly:"
- **"The blocking question, if there is one — phrased so a human reading quickly can answer without looking anything up."** — "Blocking question" is mild jargon and "phrased so a human…" is a long qualifier. Plainer: "Any question that's actually blocking you — ask it so the user can answer without digging."
- **"Anything stale or contradicting the repo."** — Fine but terse to the point of cryptic. Plainer: "Anything in the handoff that looks stale or doesn't match what's in the repo."
- **"get on with the work using normal judgment — don't ask permission for routine choices."** — Good, keep.
- **"the user is human, jargon walls them out."** — Nice line, keep.
- **"If the handoff is missing, say so and ask how to start. Don't invent one."** — Good, keep.

## Overall

Neither file is contract-y or anxious-making. The two recurring tics are (1) compressed noun phrases that read like defined terms ("handoff item," "blocking question," "real trigger") and (2) a couple of "authoritative"/"greppable"-style words that import a formal register. Loosening those is the whole fix.
