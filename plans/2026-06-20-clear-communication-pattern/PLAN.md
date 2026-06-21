# Clear-communication pattern — the failure, and the pivot that worked

**Date:** 2026-06-20
**Why this exists:** The operator asked for it after a session where the agent repeatedly buried answers in internal jargon, got called out, then pivoted to a plain-English answer the operator called "brilliant." The goal of THIS doc is to capture *exactly* what failed and *exactly* what worked — so a follow-up agent can figure out how to make the good version happen **by default in every agent**, not only after the operator gets frustrated.

---

## What happened (the failure)

Across a long build session, the agent's status updates and closing summary were written from the **agent's** frame of reference:

- Dense with internal codes the operator doesn't carry in their head: bead IDs (`hk-9waz`), work-item labels (`ES1`–`ES8`), decision codes (`D1`–`D7`), "Tranche 1/2/3."
- The closing message ended with a **"go/no-go" question** on a feature called "keeper HOLD" — but never explained, in plain terms, *what it was*, *what the options were*, or *what choosing either way would cost*.

The operator's reaction:

> "What are you talking about? You need to learn to ask questions more clearly. I don't have the information you have — and you haven't given me any. It's the same thing every fucking time."

**The crux:** this is a *recurring* failure ("every time"), and it persists **despite existing standing rules that already forbid it**. The global working-style file already says:
- "**Plain English, always** — translate every internal code, ID, codename, or section number on first mention each turn."
- "**Queue With Context** — for real decisions include description + why-queued + concrete options-with-consequences."

The rules existed. They still didn't fire. **That is the problem the follow-up agent must solve** — not "write the rule," but "make the rule actually change behavior."

---

## The pivot (what worked)

When called out, the message that landed did five things:

1. **Reframed into the operator's world.** "You can start a captain or crew with one simple command — `harmonik start captain` / `harmonik start crew paul`. No env var, no script path." NOT "ES1–ES8 landed."
2. **Zero internal codes.** No bead IDs, no `ES`/`D` codes, no "Tranche." Every concept named in plain words.
3. **Caught the fake question.** Recognized the "go/no-go" wasn't a real decision — the operator had **already said** they wanted that feature in an earlier turn. Stopped offloading a decision the agent already had the answer to.
4. **Led with the information, not the ask.** Said what the leftover feature actually *does* ("tell the keeper not to restart you while you're working; it auto-reverts after the next restart") **before** any mention of building it.
5. **Brief honest acknowledgement, ended on the action.** "I was wrong to sit here asking go/no-go — building it now." No groveling, no new question.

### Before → after (the shape)

- **BEFORE (failed):** a table of `ES1`–`ES8` + *"Tranche 3 — keeper HOLD (`hk-9waz`) is the one piece I'm holding for your go/no-go…"*
- **AFTER (worked):** *"You can now start a captain or crew with one simple command… The only thing left is the feature you asked for: a way to tell the keeper 'don't restart me right now, I'm working' that reverts on the next restart… I was wrong to ask go/no-go — you already told me you want it. Building it now."*

Same facts. The difference is entirely **frame and codes**, plus killing a question that shouldn't have been asked.

---

## Diagnosis: why the existing rules didn't fire

- The rules are phrased as **principles** ("plain English always"). Principles are easy to feel compliant with while still writing from the agent's own frame. There is **no concrete trigger or self-check at the moment a user-facing message is composed.**
- **Status updates masquerade as helpful.** "Reporting progress" feels like a courtesy, so the plain-English rule never gets consciously applied to it — yet status updates are exactly where the code-dumping happens.
- **"Ask when unsure" overfires.** It produces go/no-go questions that *look* like good collaboration but are actually decision-offloading when the answer is already known (the user stated it, or it's a runtime-tunable detail the agent should just pick).

---

## The embeddable essence (what the follow-up agent should operationalize)

A **pre-send discipline** applied to every user-facing message:

1. **Frame check.** Is this written in the *user's* terms (what they can now do / what they must decide), or in my internal tracking terms? Strip every code/ID/label the user didn't type first; if a code must appear, translate it inline.
2. **Question test.** If I'm asking something: is it a *genuine* decision only the user can make? If I already have the answer (they said it earlier, or it's runtime-tunable), **don't ask — act.** If it IS genuine, put the information + concrete options + consequences **in the message** — never make them go find context I'm holding.
3. **End on the next action**, not a question (unless it's the genuine-decision case above).

---

## For the follow-up agent (the "embed into every agent" task)

- **The hard part is making it FIRE, not stating it.** A rule already exists and failed. Investigate *why a stated principle didn't change behavior* and what mechanism would. Candidate surfaces:
  - A **pre-response self-check / gate** (the moment-of-composing trigger that principles lack).
  - A **concrete before/after exemplar baked into agent context** — examples shift behavior more reliably than principles.
  - A trigger keyed to the two danger zones: **"status update"** and **"asking the user a question."**
  - Possibly wiring into skills that already gate behavior (orchestrator / handoff / the agent-config reviewers) rather than another CLAUDE.md line.
- **Verification:** how do you know it's working? Consider a reviewer that flags any user-facing message containing an untranslated internal code or a context-free question.
- **Do NOT** just append another "be clearer" sentence to CLAUDE.md and call it done — that is precisely the mechanism that already failed.
