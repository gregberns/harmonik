# External-Form Comparison: Medical, Military, Incident-Command Handoffs

What is the FORM and ECONOMY of a handoff in three established domains? Source files: `phases/phase-2/analysis/external-sources/{medical-handoffs,military-briefings,incident-command}.md`.

## Per-domain extraction

### Medical (I-PASS, SBAR)

- **Length / time:** SBAR is "one or two sentences" per slot, used for "urgent, short" calls. Full I-PASS handoffs run "5-15 minutes per patient" verbally; the written sheet is a single page.
- **Mandatory:** the four/five named slots (Situation, Background, Assessment, Recommendation; or I, P, A, S, S). I-PASS makes **synthesis-by-receiver** mandatory — "the handoff is considered incomplete until the receiver has restated."
- **Optional:** narrative elaboration around any slot; CUS-style escalation is opportunistic.
- **Underlying constraint:** shift change is timed; receiver's working memory caps absorption — "verbal-only handoffs retain 0–26%" after five cycles. Brevity is forced by decay, not by taste.
- **Explicit anti-verbosity rule:** the I-PASS *Illness severity* slot is reduced to a one-tag flag (stable / watcher / unstable) precisely "under cognitive load, the receiver may not process the full Patient summary."

### Military (Commander's Intent, SMEAC, FRAGO, back-brief)

- **Length / time:** Commander's intent is **"2–3 sentences"**. SMEAC OPORDs run pages but small-unit verbal orders are short; FRAGOs are minimal-delta — "only the *changed* paragraphs."
- **Mandatory:** purpose, end state, key tasks (intent); the five SMEAC paragraphs for an OPORD; the back-brief turn.
- **Optional:** rehearsal tier; whether to issue a WARNO before OPORD.
- **Underlying constraint:** "the subordinate must memorize the order because they will not have it in the fight." Plan must "survive contact" — survive being lost or invalidated.
- **Explicit anti-verbosity rule:** doctrinally-named failure mode of **"Too long / lengthy intent"** — "lengthy and/or vague intent statements... make it difficult for a company commander to focus on what is really important." Over-specification is a *named failure*, not a stylistic preference.

### Incident command (ICS, sitrep, IAP)

- **Length / time:** Commander's intent is **"one-to-three sentences"**. Sitreps are "structured (short-to-medium)." IAPs are pre-printed forms (ICS 202–206), not narratives. Handoff line is one sentence: *"You're now the incident commander, okay?"*
- **Mandatory:** the structured slots on the form; closed acknowledgment for role transfer.
- **Optional:** which forms get filled at small-team scale ("commander holds all positions not delegated"); rehearsal/role splits collapse on solo.
- **Underlying constraint:** time pressure plus radio bandwidth plus actor turnover. Cadence-driven, not trigger-driven: "even if nothing changed, you still produce a sitrep saying so."
- **Explicit anti-verbosity rule:** the **"two echelons down"** rule — intent must be understandable by someone two organizational levels removed. Forbids both vague-only-the-author-understands intent *and* tactics-disguised-as-intent.

## Comparison

| Dimension | Medical | Military | Incident command |
|---|---|---|---|
| Headline length | One-tag severity + 1–2 sentence slots | 2–3 sentences (intent) | 1–3 sentences (intent); 1-line handoff |
| Full artifact | 1 page sheet, 5–15 min verbal | Pages (OPORD), seconds (FRAGO) | Pre-printed form, ~1 page |
| Forcing function | Receiver memory decay | Plan survival under contact | Operational-period cadence + radio bandwidth |
| Named anti-verbosity rule | "Severity tag survives even when patient summary doesn't" | "Lengthy intent" is a named failure | "Two echelons down" rule |
| Mandatory closed loop | Synthesis-by-receiver (I-PASS) | Back-brief / confirmation brief | "Okay?" + firm acknowledgment |

## Common pattern

All three:

- **Compress the *why* into a few sentences** at the top, separate from the *how*. Purpose / end state / situation tag.
- **Use named slots, not free prose.** Slots exist because dropping each produces a documented error class.
- **Separate intent (stable) from plan (changeable).** Mission ≠ Execution; Assessment ≠ Recommendation; Objectives ≠ Tactics.
- **Require closed-loop acknowledgment.** Receiver restates *something* — comprehension (read-back), plan (back-brief), or ownership ("okay?").
- **Treat brevity as a mechanism, not a style.** Each domain has a doctrinally-named failure mode for over-specification.

What none of them do:

- **None list "load-bearing tokens" verbatim.** The receiver re-encodes in their own words; verbatim repetition is identified as a failure mode (medical teach-back's "patient said the words back but didn't understand"; military's "ceremonial back-brief" that parrots).
- **None partition decisions into "decide vs ask first" lists.** Authority is granted by *bound* (intent / objectives / end state). Concrete decisions stay implicit — anything inside the bound is pre-authorized; anything that would breach the bound triggers escalation. Mission command names "do whatever you think is best" as **abdication, not delegation**, but the cure is a tighter intent, not a longer permitted/forbidden list.
- **None produce "out of scope" sections.** Scope is defined positively (end state, key tasks, objectives). FRAGO carries only what changed; unchanged material is *inherited*, never re-listed. The closest analog — a military OPORD's *task organization* paragraph — names what is in, not what is out.

## Application

A 150-line handoff with 50+ verbatim tokens fails on every common-pattern axis: it expands instead of compressing the *why*; it lists tokens instead of forcing receiver re-encoding; it enumerates decisions instead of granting bounded autonomy; and it produces an out-of-scope inventory where these domains rely on positive scope plus inheritance. The shape lesson is severity-tag + 2–3-sentence intent + receiver paraphrase, not a longer checklist.
