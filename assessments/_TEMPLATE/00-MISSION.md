# Mission

> Written BEFORE anything runs. This file is what you were asked to do, frozen at the start.
> If the scope changes mid-assessment, add a dated note at the bottom — do not edit what is above
> it, or the record stops showing what you actually set out to do.

**Started:** YYYY-MM-DD HH:MM (local)
**Assessor:** <which agent is holding the role>
**Gate:** merge | deploy
**Asked by:** operator | admiral

## What is being gated

| Lane / branch | Commit | What is in it |
|---|---|---|
| `<branch>` | `<hash or "resolve at step 1">` | |

**Merge base:** `<hash>`
**Conflicts:** `git merge-tree` result, and when it was measured.

If there is no single candidate commit — the branches have not been merged — say so here and say
where the merge result will be built. A gate with no named revision is not a gate.

## Scope

What is in bounds. What is explicitly out of bounds and will not hold this gate.

## Independence

The assessor never grades work it helped build. State the position plainly:

- **Clean** — the assessor built none of this. Say so.
- **Carve-out** — the assessor built part of it. Name exactly which commits, who reviews those
  instead, and record that the verdict will say so on its face.

A carve-out is a decision someone made. Name who made it and when. An unrecorded conflict of
interest is the failure this section exists to prevent.

## Known-red going in

What is already failing before this branch is considered, so a known failure is not read as news.
Name the issue id for each. Anything listed here is inherited debt and does not block on its own.

## Decided before start

Questions that were settled so the assessment could begin, and who settled them. This is the section
that stops the same three questions being re-derived every session.
