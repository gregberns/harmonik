# R1 — Skeptic of the Fix (v2 drafts vs the 8 named causes)

Reviewing `session-handoff.md` (24 lines) and `session-resume.md` (18 lines).

## 1. `default to ask` → permission-seeking

Clean. Both drafts invert the disposition. `session-resume`: *"get on with the work using normal judgment — don't ask permission for routine choices."* `session-handoff`: *"Only items with a real trigger. A parked item with no trigger is a TODO, not a handoff item."* No residue.

## 2. Descriptive becomes policy

Mostly clean — the eight-section schema is gone, removing the slot where descriptive notes were getting frozen as rules. **Mild concern:** `session-handoff` line 17 still names a section called **"If-then"** with the gloss *"Conditions worth flagging: `if X happens, then do Y`."* That phrasing invites a future agent to read each `if/then` as a directive even when the prior author meant *"watch for this."*

CUT: drop the literal `if X happens, then do Y` template; rely on the surrounding sentence (*"Only items with a real trigger"*).

## 3. Back-brief checkpoint posture bleeds past turn 1

Mostly clean. `session-resume` line 16: *"Then wait for the user to confirm or correct. Once they've responded, get on with the work using normal judgment."* Good. **Residue:** the bullet *"The blocking question, if there is one"* still primes a stop-and-ask shape on turn 1.

REWORDING: collapse to *"flag anything blocking or stale"* — drops the question framing.

## 4. Schema-fill triggers (no severity weighting / sections invite filling)

Reduced but not gone. `session-handoff` still lists six bullets (Status / What we're doing / Where it stands / If-then / Open question / First files). Lines 12 (*"Cover only what applies"*) and 21 (*"Skip sections with nothing real to say"*) are explicit anti-fill instructions, which helps — but the named-bullet list itself is still a schema, and the v1 finding showed that *named slots get filled regardless of "skip if empty" disclaimers.*

CUT: collapse the six bullets into one or two lines of prose describing what a useful handoff contains, with no enumerated section names. The bullets are the schema; renaming them "optional" doesn't undo the pull.

## 5. Anchoring on internal vocabulary

Clean. No `load-bearing tokens` section, no readback ritual.

## 6. No outward-translation discipline

Clean and explicit. `session-handoff` line 18 and `session-resume` lines 13 and 16 all carry the translate-jargon instruction.

## 7. Output-style-matches-input-style

Largely fixed by the dramatic length cut (104 → 24, 102 → 18). **Residue:** `session-handoff` still uses bold-bullet prescriptive form (`**Status.**`, `**What we're doing.**`, `**Where it stands.**`). That is the same structural register the v1 finding called out — the agent mirrors it. Even at 24 lines, six bold-prefixed imperatives read as a schema.

REWORDING: drop the bold prefixes; write one paragraph of prose. *"A useful handoff names current status, what's in flight and the next step, anything that should trigger a course-change, a blocking question if one exists, and the few files to open first."*

## 8. Contract framing

Clean. No `contract` / `source of truth` / `behavior rules` / `failure modes` language.

---

## Bottom line

Causes 1, 5, 6, 8 are cleanly cut. Causes 2, 3, 4, 7 have **residue concentrated in `session-handoff`'s bulleted bold-prefixed list** — a smaller schema is still a schema. The single highest-leverage cut is collapsing those six bullets into prose; that simultaneously addresses 2, 3, 4, and 7. Adding any new clarifying sentence ("but only if relevant", "skip when empty") would be the failure repeating itself.
