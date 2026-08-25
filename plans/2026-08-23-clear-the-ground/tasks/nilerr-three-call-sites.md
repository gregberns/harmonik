---
id: nilerr-three-call-sites
title: Three call sites return nil on a non-nil error — decide what each should do
type: task
priority: 0
labels: [daemon, correctness, clear-the-ground]
depends_on: []
blocks: [lint-rekey-exclusion-list]
workstream: W1
batch: 1
---

## This is not a lint sweep, and it must not be reviewed as one

The other three greenify tasks clear lint findings. **This one changes what daemon code does when a
call fails**, and it is separated from them for exactly that reason. Bundled with an `errcheck`
sweep, a behaviour change ships with a sweep review.

## READ THIS FIRST — the pressure is not an argument

These three findings are blocking `hk-lint-rekey-exclusion-list-t9ebz`, which is blocking a merge
that has been held for days. That is true, and it is **not an argument about what these call sites
should do on error.**

**It will feel like one.** The smallest edit that clears a `nilerr` row is not necessarily the right
error handling — it is just the shortest. Each of the three needs a named decision about what that
call site should do when the call fails, and "the merge was waiting" is not that decision.

**If the answer for any of the three is "this genuinely should return nil and the linter is wrong
here", the honest repair is a `nolint` with a stated reason — not a behaviour change.** That is a
legitimate outcome and not a cop-out. Better a documented suppression than a daemon error path
altered to clear a lint row.

## The three

`nilerr` reports: the error is not nil, and the function returns nil anyway. All in `internal/daemon`.
**Find them by symbol; the line numbers move — which is itself part of the story below.**

| # | file | symbol | note |
|---|---|---|---|
| 2 | `stalewatch_wkzlc_test.go` | `TestStaleWatch_LastEventTypeTracked` | test code |
| 9 | `stalewatch_wkzlc_test.go` | `staleFixtureNewBus` | test fixture |
| 13 | `bandwidthtuner.go` | `bandwidthTunerBackstop.handle` | **production daemon code — treat differently from the two above** |

**Row 13 is the one that matters.** Rows 2 and 9 are test code, where swallowing an error hides a
test failure rather than a production fault. Row 13 is a live handler on the bandwidth-tuner path.

## Why these are blocking, which is NOT why they are worth fixing

**Their findings never changed.** They re-keyed because a line was inserted ABOVE the declaration:
`nilerr`'s message embeds an absolute line number — ``error is not nil (line 195) but it returns
nil`` — so the number is inside the digest (`hk-k11fp`). Rows 9 and 13 were confirmed as
byte-identical declarations whose key moved only because of the line shift; row 2 is assumed to be
the same shape and was not individually verified.

**So the reason they block is a defect in the keying, not a change in the code.** The repair that
would make this a non-event — normalising line references out of message text — lives in `t9ebz`,
which these three are blocking. That circularity is why they must be resolved here rather than waited
out.

**Do not let that make the fix feel arbitrary.** Returning nil on a non-nil error is usually a real
bug. Judge each on its merits.

## Done when

1. **Each of the three has a NAMED decision recorded in the commit body**: what the call site now does
   on error, or why it correctly returns nil. Three decisions, stated separately. One sentence each
   is enough; silence is not.
2. **Row 13 is reviewed as a behaviour change**, not as a lint fix. If its error now propagates, say
   what the caller does with it and what changes for a running daemon.
3. **All three findings are gone** from `make lint-allow` — by repair or by a `nolint` carrying a
   stated reason.
4. **`allow.txt` gained no row.** Adding one is refused by the ratchet.
5. **A `--uniq-by-line=false` run shows nothing revealed.** These three were measured as carrying no
   masked sibling; confirm your edit did not create one.
6. **The stale-watch and bandwidth-tuner tests still pass untouched.** If a test must change to
   accommodate the new error path, that is the behaviour change showing itself — say so explicitly
   rather than adjusting the test quietly.

## Limits

- **Do not make the smallest edit that clears the row.** That is the failure mode this file exists to
  prevent.
- **Do not bundle these with the other greenify tasks**, and do not let a reviewer treat them as a
  sweep.
- **A bare `nolint` is not acceptable; a `nolint` with a stated reason is.** The reason is the
  deliverable.
- **Do not touch `--uniq-by-line`** or `.golangci.yml`.
