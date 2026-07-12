# Archive — pre-freeze-and-carve initiative/lane state (snapshot 2026-07-12)

These are the **admiral + captain initiative/lane trackers as they stood the moment
before the freeze-and-carve clean slate.** They are preserved here (git-tracked) so the
old work is not lost, but they are deliberately OUT of every boot-read path so the stale
5/6-lane history stops surfacing in handoffs going forward.

## What's here
- `admiral-initiatives.md` — the big-rocks registry (37 KB): the Pi / Remote / Codex-as-crew
  / Quality-enforcement / comms-test-harness priority order + the full dated audit-marker
  log through 2026-07-09 (v0.5.0 cut, teardown sweep, keeper-inject work, etc.).
- `captain-lanes.md` — the tier-2 lane tracker showing the 5-lane parallel fleet
  (kynes/hawat/piter/stilgar/yueh + leto) as CURRENT TRUTH.
- `lanes.json` — the machine-readable lane→epic index (6 active + 2 parked lanes).

## Why archived
The operator ordered a **freeze-and-carve** strategic pivot (direction-log 2026-07-12
~18:00Z) and then a clean-slate teardown: all worker/oversight crews stopped, run
worktrees reaped, 267 beads closed. The lane/initiative structure above no longer
reflects reality — the fleet is deliberately torn down and execution is frozen pending
ratification of `plans/2026-07-12-codebase-census/PLAN.md`.

## Where the live state is now
- Current direction: `.harmonik/context/direction-log.md` (the ~18:00Z pivot entry).
- Current plan: `plans/2026-07-12-codebase-census/PLAN.md` (+ `REPORT.md`).
- The rewritten, live trackers: `.harmonik/context/captain-lanes.md`,
  `.harmonik/context/lanes.json`, `.harmonik/crew/admiral-initiatives.md`.

Nothing in this folder is boot-read. Read it only to recover historical context.
