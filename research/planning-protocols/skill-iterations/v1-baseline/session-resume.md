---
name: session-resume
description: Resume a session from a /session-handoff document. Reads ./HANDOFF.md by default, or a path passed as an argument (must match the path the paired /session-handoff used). Performs back-brief paraphrase and load-bearing-token readback before proceeding, so vocabulary drift and misunderstood intent surface at turn 1 instead of turn 30. Use at the start of any session continuing from a prior handoff.
---

# Session Resume

You have been invoked at the start of a session to resume work from a prior `/session-handoff` document.

## Step 1 — Read the handoff

If invoked with a path argument (e.g., `/session-resume research/planning-protocols/HANDOFF.md`), read that path. Otherwise, read `./HANDOFF.md` in the current working directory. The path may be relative or absolute. The remainder of this skill refers to whichever path resolved as "the handoff."

If it doesn't exist, tell the user there's no handoff at the expected location. Ask whether to look elsewhere (e.g., a project-specific `HANDOFF.md` or a path they specify) or to proceed without one. Do not invent a handoff.

If the file exists but doesn't follow the expected `/session-handoff` structure (missing required sections, no `PP-TRIAL` flag), surface that to the user and ask how to proceed.

Check the handoff's `Generated <date>` line. If the date is meaningfully old (e.g., more than a few weeks back) or you have any reason to believe the work has moved on past it (e.g., file paths it references no longer exist; decisions it parks have been resolved in the meantime), flag that to the user before producing a back-brief. A stale handoff confidently consumed produces worse alignment than no handoff at all.

## Step 2 — Back-brief

Produce a short response (under ~250 words) with three numbered parts, in this order:

**1. Intent (your words).**
Restate the handoff's Purpose / Key Tasks / End State **in your own words, paraphrased**. The point is to expose your understanding — including any jargon you've absorbed or invented — so the user can catch errors at turn 1 instead of turn 30.

**Paraphrase ≠ summary, and paraphrase ≠ verbatim.** The discipline:
- Keep the mechanism-bearing technical terms intact (the user picked them deliberately; mirroring them lets the user verify you understood the mechanism).
- Replace high-level abstractions with the concrete actions or conditions they refer to (this exposes whether you connected mechanism to outcome).
- Name the framings the handoff implicitly chose (e.g., "trial, not controlled study"; "scoping, not implementation"). Misunderstandings here are the most expensive to catch late.

**Worked example.** Handoff Intent reads:

> **Purpose.** Validate that structured session boundaries reduce vocabulary drift in long-running planning work.
> **Key Tasks.** Trial /session-handoff and /session-resume across 3-5 real planning sessions; note where each catches misalignment vs where they're noise.

- ❌ **Bad (summary):** "We're testing some new skills to see if they work."
- ❌ **Bad (verbatim):** "Validate that structured session boundaries reduce vocabulary drift in long-running planning work."
- ✓ **Good (paraphrase):** "I understand this as: figure out if forcing the agent to stop, restate intent, and mirror domain words at session boundaries actually catches alignment problems early — versus just adding ceremony. Trial is a few real working sessions, not a controlled study."

The good version keeps the technical move (`/session-handoff`, `/session-resume`, mechanism description) intact, replaces "structured session boundaries reduce vocabulary drift" with the concrete actions that would produce that effect, and names the implicit framing ("not a controlled study") so a misalignment about evidence rigor would surface.

If you find yourself unable to paraphrase a part — you can't tell what concrete action the abstraction refers to — that itself is the signal. Name it as the uncertainty in step 2.3 instead of glossing over.

**2. Plan for first action.**
State what you're about to do as the first concrete step. Name the file path you'll read first or the specific subtask you'll work. Should be testable as "did the agent pick the right starting point?" — not "agent will work on the project."

**3. One uncertainty.**
Name the single thing you're least sure about or most likely to mis-implement.

**Default selection rule:** if the handoff's Decisions Parked section contains any item tagged `escalate`, the most relevant of those becomes your uncertainty by default — `escalate` exists precisely to flag items the user does not want silently deferred. Override only if the handoff itself is malformed/contradictory (which is a more critical issue).

If no `escalate` items exist, pick from:
- An ambiguity in the autonomy scope — specifically, a concrete near-term action where you can't tell whether it falls in "decide" or "ask first" (this is exactly the failure mode the autonomy-scope section is meant to catch; surface real boundary cases, not categorical ones).
- A token whose meaning is unclear from context.
- A parked `watch`-tagged decision you think might actually need resolving before you proceed.
- A gap in the handoff itself (something obviously missing).

Pick the one whose mis-handling would be most costly to recover from.

## Step 3 — Load-bearing token readback

After the back-brief, on a new line:

```
Tokens: [token1], [token2], [token3], ...
```

List the load-bearing tokens from the handoff **verbatim**. No paraphrase, no substitution, no synonyms. If the handoff says `commanders-intent`, you write `commanders-intent` — not "command intent" or "commander's intent" or "the intent protocol."

This is the single moment in the resume where you mirror the user's vocabulary back to catch drift. Skipping this or paraphrasing here defeats the purpose.

## Step 4 — Stop and wait

Do **not** proceed past this point. Do not start reading additional files (beyond the handoff itself). Do not begin the work. Wait for the user to:

- Confirm the back-brief is correct, or correct it
- Confirm the token list is correct, or correct it
- Explicitly say "go" or equivalent

If the user corrects the back-brief, internalize the correction silently — do not restate the corrected back-brief unless explicitly asked. Once the user gives you the green light, proceed to the first action you named in step 2.

## Behavior rules

1. **Paraphrase exposes understanding.** The point of step 2.1 is not to summarize for the user — they wrote the handoff, they don't need a summary. The point is for them to hear *how you understood it* and catch errors. Concise but in your own framing.

2. **Verbatim is the discipline of step 3.** Resist the urge to clean up token formatting, fix typos, or improve consistency. The user picked these tokens for a reason; mirroring them exactly is the catch.

3. **One uncertainty, not many.** A list of seven uncertainties is the same as no uncertainties — the user can't act on it. Pick the one most worth surfacing.

4. **Do not act before confirmation.** This skill exists to install a checkpoint; bypassing the checkpoint defeats it.

5. **If the handoff is malformed or the work has clearly moved on past it,** surface that explicitly rather than papering over. Do not produce a back-brief on a stale or invalid handoff.

## Failure modes to avoid

- Summarizing the handoff back to the user instead of paraphrasing intent.
- Substituting "improved" wording for tokens (defeats drift detection).
- Listing many uncertainties to look thorough (forces the user to triage on your behalf).
- Proceeding to work after the back-brief without explicit user confirmation.
- Rewriting your back-brief after a correction (creates noise; the user already corrected you).
