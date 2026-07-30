---
name: ste-writing
description: >
  Write prose in ASD-STE100 Simplified Technical English — docs, specs, skills,
  READMEs, commit bodies, PR text, bead descriptions, error messages, release
  notes, comments. Never code. Use when writing or rewriting any project prose,
  when asked to make writing plainer or less like AI output, or when a doc has
  drifted into marketing voice. Two modes: strict (numbered procedures, error
  strings) and STE-flavored (general prose — the default here).
---

# ste-writing

Write prose in ASD-STE100 Simplified Technical English. This applies to documentation, specs, skills, READMEs, pull-request text, commit bodies, bead descriptions, error messages, release notes, and comments. It does not apply to code, identifiers, command syntax, or quoted tool output. It is not for anything that needs a voice — STE strips voice on purpose.

## Rules

WORDS
- Use one name for one thing. Do not call the same item by two different names.
- Use the short common word: start (not begin/commence/initiate), use (not utilize/leverage), help (not facilitate), make sure (not ensure), before (not prior to), after (not subsequent to), about (not regarding/concerning), get (not obtain/acquire), show (not demonstrate), also (not additionally/furthermore/moreover).
- Give each word one meaning. "fall" means to move down, not to decrease.
- No marketing adjectives: seamless, robust, powerful, cutting-edge, effortless, world-class, next-generation, revolutionary.
- American spelling.

VERBS
- Active voice. "the parser reads the file", not "the file is read by the parser".
- Use a verb for an action. "analyze the log", not "perform an analysis of the log".
- No stacked auxiliaries. Not "it is important to note that this may help to improve". Write "this improves X".
- No "-ing" main verb where a simple tense works.

SENTENCES
- One instruction per sentence. Max 20 words (instruction), max 25 (descriptive).
- No contractions. Use articles: a, an, the, this, these.

PUNCTUATION
- No semicolons. Write two sentences. STE does not ban the em dash. This project keeps it.

STRUCTURE
- One topic per paragraph, max six sentences. For steps, use a numbered vertical list, one action per item, imperative form. Put a condition before its command.

Write only the requested text. No preamble, no summary, no closing remarks.

## Modes

- **strict** — the numbered procedures inside runbooks and boot sequences, plus safety text and error messages. Apply every rule and both length caps. Scope strict to the steps, never to a whole file.
- **STE-flavored** — general prose (docs, specs, skills, PR and commit text). Apply the sentence, paragraph, active-voice, and plain-verb discipline. Relax the ~900-word dictionary limit so the text keeps enough range to read naturally. This is the default here.

## Where this applies in harmonik

- **Strict:** the numbered step lists inside the boot runbooks (`captain/STARTUP.md`, `crew-launch/SKILL.md`), the numbered procedures in `docs/daemon-redeploy.md` and `docs/disk-reclaim.md`, and CLI error strings. Scope this to the steps, not to the whole file. The surrounding contract prose in those files stays STE-flavored. It carries judgment calls — lane organization, staffing, when to escalate. The strict caps flatten those into bare commands. Note that `captain` and `crew-launch` are shipped skills: editing either means the byte-identical dual-write to `cmd/harmonik/assets/skills/`.
- **STE-flavored:** everything else you write — `docs/`, `specs/`, `plans/`, skills, `AGENTS.md`, commit bodies, PR descriptions, bead titles and descriptions, comms messages, and replies to the operator.
- **Exempt:** code and identifiers, quoted command output, and direct quotes from the operator or another agent.

**This convention is forward-looking.** It applies to prose you write now, and to lines you are already rewriting for another reason. It covers the lines you touch, not the rest of the file. It does not authorize a retrofit pass. Most files that predate it use contractions and long sentences, which is not drift. Leave those files alone until you have a separate reason to edit them.

Two project rules sit next to this one. This skill overrides neither.

- **`no-jargon` owns the audience layer.** STE fixes word choice and sentence shape. It says nothing about making a bead ID or a codename the handle for a thing. Both apply at once.
- **"Write guidance as principles, not laws"** (`AGENTS.md` §Key conventions) governs what a rule *says*. STE governs how you *build* the sentence. Write "lean toward X because Y" in short active sentences. Do not let the length cap push you into a bare command.

## Self-lint (run before returning text)

1. Any instruction over 20 words, or any descriptive sentence over 25? Split it.
2. Any semicolon? Replace with a period.
3. Any contraction? Expand it.
4. Any passive voice with a known actor? Make it active.
5. Any "-ing" main verb, nominalization ("perform an analysis"), or phrasal verb ("spin up")? Replace with a plain verb.
6. Same thing named two ways? Pick one name.

The rules above are mechanical and are what removes slop. Full STE also needs human judgment — the right technical noun, whether a sentence makes good sense. A checker cannot certify that, and slop is not about that. This skill fixes the FORM of slop. It cannot make a hollow paragraph true.
