---
id: crew-cleanup-skill
title: A skill that finds the accumulated junk, shows it, and asks before deleting
type: task
priority: 1
labels: [tooling, cleanup, clear-the-ground]
depends_on: []
blocks: []
workstream: W6
batch: 5
---

## Problem

Operator direction, 2026-08-23: several directories accumulate dead files and nobody owns clearing
them. A cleanup *system* would be a distraction to build; a skill that knows where to look and asks
before it deletes is enough.

Measured accumulation, 2026-08-23:

- `.harmonik/crew/missions/` — 29 mission files. **17 have neither a live crew in the registry nor a
  reference from any skill, manifest or doc.** Roughly 120 KB.
- `.harmonik/comms/cursors/` and `.harmonik/comms/cursors-live/` — contents unaudited.
- `.harmonik/cognition/` — contains generated relaunch scripts whose generator is Go code.
- `.harmonik/beads-owned/` — contents unaudited.
- Repo root — four `HANDOFF-*.md` files nothing references, including a 44 KB archive.
- `.harmonik/context/` — two raw `.diff`/`.patch` files parked in the directory a captain boot-reads.

## Scope

A new project-authored skill at `.claude/skills/crew-cleanup/`.

**This is project-authored, not shipped.** Do not add it to `cmd/harmonik/assets/skills/` — that
would make it a fourth thing to keep in sync, which is the problem `skill-copy-governance` exists to
reduce.

The skill: knows the sites above, reports what it finds at each with counts and ages, cross-checks
mission files against the live crew registry (`.harmonik/crew/*.json`) and against references
elsewhere, proposes a deletion set, and **deletes nothing without confirmation**.

## Done when

1. Running it on this repo lists every site with a count, a total size, and an age for the oldest
   item.
2. It proposes a deletion set with a reason per item — "no live crew and no reference" is a reason;
   "old" is not.
3. It asks before deleting, and answering no leaves the tree untouched.
4. It reports what it could not classify rather than assuming those files are safe to keep or safe to
   delete.

## Limits

- **One skill, one pass. Do not build a cleanup subsystem.** No daemon job, no scheduled reaper, no
  config surface. If it grows past a single skill file plus a helper, it has become the distraction
  the operator declined.
- Do not delete anything as part of writing it. The first real run is a separate, supervised act.
- Do not touch `.beads/`. The ledger has its own retention story and getting it wrong loses history.
