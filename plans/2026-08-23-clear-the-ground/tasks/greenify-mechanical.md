---
id: greenify-mechanical
title: Clear the eight mechanical findings blocking the scheme migration
type: task
priority: 0
labels: [lint, gate, clear-the-ground]
depends_on: []
blocks: [lint-rekey-exclusion-list]
workstream: W1
batch: 1
---

## Why this exists, and why it is P0

`hk-lint-rekey-exclusion-list-t9ebz` cannot land until the tree is **green under the OLD keying
scheme**. That precondition is explained in
[`ratchet-scheme-migration.md`](ratchet-scheme-migration.md) — the migration's soundness check
computes old-scheme digests, so any finding the judge refuses at the base blocks it by definition.

**14 findings are refused at the batch tip.** They are split across four task files by REPAIR RISK,
not by linter or file, because bundling them means the riskiest change gets a sweep review. **This
file owns the eight that are genuinely mechanical.** The other three files own the complexity
decomposition, the two poisoned rows, and three behaviour changes.

**These rows must be FIXED, not tolerated.** Adding an allow-list row is refused by the ratchet —
that is the deadlock this whole sequence exists to get around. Fixing a finding makes its row stale,
and stale rows are allowed.

## The six

> **Two rows that look mechanical are NOT here, and taking them would break another task.**
> `gosec` G301 in `TestBandwidthTunerBackstop_Pi_EventSkipsGlobalTuner` and in
> `TestBandwidthTunerBackstop_NonPi_EventReachesGlobalTuner` are trivial repairs — but they sit in
> the SAME TWO FUNCTIONS as the two poisoned rows owned by
> [`greenify-errchkjson.md`](greenify-errchkjson.md). The allow list keys a tolerated finding by its
> **enclosing declaration**, so editing either function re-fingerprints the other task's rows.
> **Those two G301 rows belong to that task, which owns both functions whole.** Do not touch
> `bandwidthtuner_test.go`.


All in `internal/daemon`. Line numbers are from the batch tip; **find the symbol, not the line.**

| # | linter | file | symbol | finding |
|---|---|---|---|---|
| 1 | `containedctx` | `dot_postexit_fixture_test.go` | `dotFixtureOpts` | struct contains a `context.Context` field |
| 3 | `unused` | `handlerpause_policy_37zy8.go` | `runEntry` (local type in `buildInFlightList`) | type unused |
| 4 | `errcheck` | `workloop_handlerpause_qxtbq_test.go` | `TestScenario_WorkLoop_HandlerFatalTripsGate` | `daemon.ExportedRunWorkLoop` return unchecked |
| 8 | `gosec` | `draindetect_test.go` | `TestGatherDrainFacts_WorktreePathsPopulated` | G301 |
| 10 | `gosec` | `draindetect_test.go` | `TestGenuineDrain_FailedArchiveFileIsStuck` | G306 |
| 14 | `errcheck` | `subscribe_test.go` | `TestSubscribeHub_HeartbeatActiveRunsFromRegistry` | `uuid.NewV7` return unchecked |

**Row 4 also clears a masked sibling.** `gosec` G104 "Errors unhandled" sits on the same line, hidden
by `--uniq-by-line`. It is the same defect stated twice, so one real error-handling fix clears both.
**Handle the error properly rather than discarding it** — a discard clears `errcheck` and leaves G104.

## The hazard that makes the other three files exist — read it before you touch a line

**`--uniq-by-line` defaults true, so the report shows at most ONE finding per source line and hides
the rest** (`hk-e0ybh`). Fixing a visible finding can REVEAL a hidden one on the same line. An
unmasked finding is untolerated, so the judge fails, and tolerating it needs a row the ratchet
refuses — **you would be back in the deadlock having done the work.**

**The six rows above were measured clear**, except row 4 whose sibling is the same defect. The two
rows where the repair and the reveal are the SAME EDIT live in
[`greenify-errchkjson.md`](greenify-errchkjson.md) and are not yours.

**Before you commit, re-run with `--uniq-by-line=false` and confirm you revealed nothing new.** The
measurement above was taken before your edit; your edit is what could change it.

## Done when

1. All six findings are gone from `make lint-allow`, and **row 4's masked G104 with them**.
2. **`allow.txt` gained no row.** Its six now-stale rows may be left for the migration to drop, or
   deleted here — say which you did. Adding a row fails the ratchet and is the thing this task exists
   to avoid.
3. **A `--uniq-by-line=false` run shows no finding you revealed.** State the before and after counts.
4. `make full` is no worse than it was. It is expected to remain red on the eight findings owned by
   the other three files until those land.

## Limits

- **Do not add an allow-list row.** Not for these, not for anything you reveal. If you reveal
  something you cannot fix, stop and report it rather than tolerating it.
- **Do not touch `.golangci.yml`**, and do not touch `--uniq-by-line` — `hk-e0ybh` owns that and is
  itself blocked.
- **Do not fix findings outside this list.** The other three files own theirs, and the thirteen
  `lint-burn-*` tasks own the rest of the tree. Widening scope here re-fingerprints rows those tasks
  are keyed to.
- **A discarded error is not a fixed error.** `_ =` clears `errcheck` and leaves the sibling that was
  hiding behind it. That is exactly how row 4 and the two poisoned rows behave.
