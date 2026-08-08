# Findings

> Written as findings appear. Every confirmed defect is filed as an issue AND recorded here. The
> issue is the durable ledger; this table is what the verdict reasons over.

Beads drift and are not reliably maintained. That is why the verdict never reduces to a count of
them, and why this file records the assessor's own judgement of each finding rather than deferring
to whatever the ledger says later.

## Confirmed

| # | Finding | Issue | Severity | Branch-introduced? | Disposition | Evidence |
|---|---|---|---|---|---|---|
| 1 | | | P0..P3 | yes / inherited | blocking / assigned / passive | row N in `01-EVIDENCE.md` |

**Branch-introduced vs inherited is the column that decides whether this gate is held.** A defect
this branch introduced is a blocker. A defect it merely inherited goes to the operator as a separate
health finding and does not hold the branch. Getting this wrong in either direction is expensive:
holding a good branch for old debt, or shipping a new regression because it looked familiar.

File each with the found-by label and this gate's scope label. Leave every finding unassigned.
**Never close, claim or reopen anything** — the daemon owns terminal transitions, and an assessor
that closes its own findings has graded its own work.

## Investigated and dismissed

Things that looked like defects and were not. Worth as much as the confirmed list — it stops the
next assessment re-deriving the same dead end.

| Looked like | What it actually was | How that was established |
|---|---|---|
| | | |

## Claimed-done, reconciled

For every item the mission claims complete, what confirms it. This is a first-class duty, not a
formality: a claim with no corresponding commit, diff or test result is a finding in itself.

| Claim | Confirmed by | Holds? |
|---|---|---|
| | | |
