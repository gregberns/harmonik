# Assessments

One folder per assessment. The assessor creates it before it runs anything, and writes to it as it
goes.

This is the assessor's durable record, in the same spirit as `plans/`. The instructions the assessor
follows live in [`roles/assessor/`](../roles/assessor/); its output lives here. Those are kept
apart on purpose — instructions are edited rarely and read every time, records are written once and
read later.

## Why this exists

A verdict is a conclusion, and a conclusion with no record behind it cannot be checked. Assessments
kept failing in the same ways: a gate result nobody could reproduce, a "green" run whose commit
nobody wrote down, a finding that turned out to be inherited debt after the branch was already held.
The fix is not a better verdict. It is writing down the evidence while it is still in hand.

Two rules make the record worth keeping:

- **Write as you go, not at the end.** A record assembled from memory after the verdict is a
  retelling, and it will agree with the verdict because the same mind produced both.
- **Name the commit for every result.** A result that cannot name the revision it came from is not
  evidence about anything.

## Folder naming

    assessments/YYYY-MM-DD-HHMM-<slug>/

`YYYY-MM-DD-HHMM` is when the assessment STARTED, local time, 24-hour. The slug is a few words about
what was gated — `alpha-bravo-merge`, `codex-first-deploy`. The timestamp sorts the folders; the
slug is what a human recognises six months later.

The folder listing is the index. There is no separate index file to update, because an index that
must be updated by hand is an index that goes stale.

## What goes in it

Copy [`_TEMPLATE/`](_TEMPLATE/) and fill it in. Four files, in the order they get written:

| File | Written | Holds |
|---|---|---|
| `00-MISSION.md` | before anything runs | what was asked, what is being gated, decided scope and carve-outs |
| `01-EVIDENCE.md` | throughout | every command that produced a result, its revision, its exit code, where its log is |
| `02-FINDINGS.md` | as findings appear | one row per confirmed defect, with its issue id, severity and disposition |
| `03-VERDICT.md` | last | the reasoned PASS or BLOCK, naming every commit graded |

Add more if a gate needs it. A `legs/` subfolder is the usual one, holding a report per leg when
sub-agents ran them:

    legs/live-verify.md · legs/break-testing.md · legs/code-review.md · legs/merge-gate.md

Raw output — full `make full` logs, matrix JSON — goes in `logs/` inside the assessment folder if it
is small, or stays where it was written with an absolute path recorded in `01-EVIDENCE.md` if it is
large. Do not paste a 50,000-line log into a markdown file.

## The record outlives the verdict

An assessment folder is never edited to agree with what was learned later. If a verdict turns out to
be wrong, that is a new entry in `03-VERDICT.md` under a dated heading, with the original left
intact. The value of the record is that it says what was believed at the time and on what evidence.
Rewriting it to be correct in hindsight destroys the only thing it was for.
