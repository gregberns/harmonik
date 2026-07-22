# Pass 3 research brief — required framing and deliverable

Captain directive, 2026-07-22. Pass 3 is APPROVED to proceed. Read this before writing or
completing any `findings.md`, and make sure the finished pass answers it.

## The question is not "which failure do we accept"

Pass 2 framed OD-1 as a forced either/or: the cheap option (B2, daemon-group PGID + start-time
conjunct) requires children to stay in the daemon's group, which is exactly what hk-n93gq must
undo so a group-directed kill reaches grandchildren.

**Do not accept that framing as settled. Test it.** The central question is:

> Is the conflict TRULY forced, or does **B1** (a per-child provenance record) deliver
> provenance AND kill-reach simultaneously — the child leads its own process group (satisfying
> hk-n93gq), while a marker file carries provenance (satisfying hk-o7x4w) — at a bounded cost?

If B1 delivers both, the decision collapses from *"which failure do we accept"* to *"is B1's
marker-file cost worth avoiding the tradeoff entirely"* — a far cleaner call.

**Therefore: quantify B1's real cost precisely.** Not "adds a file format." Concretely: write
cost on the spawn hot path, GC/staleness story, failure mode when the write fails (must fail
CLOSED), on-disk schema churn, and whether it subsumes rather than adds to the existing schemes
(see OD-7 — does it absorb HC-044a's per-run lock file?).

A parallel question is assigned to the B2 thread: whether a POSIX **session** + per-child
**group** split gives both properties. If either that or B1 works, the either/or dissolves.

## Required deliverable: the resolution matrix

The pass does not finish without this table. For **each** of B1 / B2 / B3, state which item it
FIXES and which it LEAVES OPEN:

| | B1 (registry) | B2 (PGID + start-time) | B3 (darwin out of scope) |
|---|---|---|---|
| hk-c6dt2 — br sweep reaps other projects' processes | | | |
| hk-n93gq — kill must reach grandchildren | | | |
| hk-o7x4w — darwin handler sweep is a no-op | | | |
| darwin post-crash sweep generally | | | |
| hk-5z4ww — watcher reaper (needs *session-generation* proof, not just project ownership) | | | |
| the liveness-and-ownership query (see problem space, "Motivating real-world evidence") | | | |

Cells must be FIXES / LEAVES OPEN / PARTIAL-with-one-line-why. No blanks.

## Why the bar is this high

This is **locked-decision class**. The captain will not lock it unilaterally — the evidence goes
to the admiral/operator together with our comparison. The deliverable is a **decidable package,
not a recommendation resting on a guess.** Anywhere the evidence does not settle a cell, say so
explicitly rather than filling it in plausibly.

## Standing constraints (unchanged)

- Fail **closed**: an unresolvable marker means "do not reap," never "reap anyway."
- hk-o7x4w stays UNCOMMITTED until this work resolves — full stop. Its own patch ARMS a
  fail-open relay-grandchild exclusion on a process-killing path (pass 2 drift finding), which is
  a second, independent reason to hold it beyond its BLOCK review verdict.
- The 8 drift items fix regardless of which design wins. Keep them separated from the 9
  amendments so drift cannot hide behind the amendment.
