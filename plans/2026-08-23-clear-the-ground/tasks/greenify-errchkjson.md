---
id: greenify-errchkjson
title: Two findings where the repair and the reveal are the same edit
type: task
priority: 0
labels: [lint, gate, clear-the-ground]
depends_on: []
blocks: [lint-rekey-exclusion-list]
workstream: W1
batch: 1
---

## Why this exists

`hk-lint-rekey-exclusion-list-t9ebz` needs a tree green under the OLD keying scheme — see
[`ratchet-scheme-migration.md`](ratchet-scheme-migration.md). Two of the fourteen findings blocking it
have a property none of the others share, and it is the reason they are not in
[`greenify-mechanical.md`](greenify-mechanical.md).

## The trap, stated so you do not discover it

Two `errcheck` findings on `json.Marshal`, both in `internal/daemon/bandwidthtuner_test.go`:

| # | symbol | finding |
|---|---|---|
| 5 | `TestBandwidthTunerBackstop_Pi_EventSkipsGlobalTuner` | `errcheck` on `json.Marshal` |
| 7 | `TestBandwidthTunerBackstop_NonPi_EventReachesGlobalTuner` | `errcheck` on `json.Marshal` |

**Behind each, on the same line, masked by `--uniq-by-line`:**

    errchkjson — unsafe type `core.RunID` found

**This task also owns TWO `gosec` G301 findings in those same two functions** — one in each. They
look mechanical and they are, but the allow list keys a tolerated finding by its **enclosing
declaration**, so whoever edits these functions re-fingerprints every tolerated row inside them.
**One task owns both functions whole, or the two tasks break each other.** That is why they are here
and not in [`greenify-mechanical.md`](greenify-mechanical.md).

**The obvious repair and the reveal are the SAME EDIT.** Write `_ = json.Marshal(...)` and you
silence `errcheck` — and `errchkjson` fires on `json.Marshal` of an unsafe type **regardless of
whether the error is discarded**. So the cheap fix swaps one untolerated finding for another, the
judge still fails, and tolerating the new one needs a row the ratchet refuses.

**There is no version of the discard that avoids this.** Do not go looking for one.

## The answer, so it is decided rather than discovered

**Deal with the unsafe type, not the discarded error.** `errchkjson` objects that `core.RunID` is a
type whose marshalling can fail, so the call needs to either stop marshalling an unsafe type or
handle the error it can return. Either resolves both findings at once; discarding resolves neither.

Check the error properly and the `errcheck` finding goes with it.

## Done when

1. **All six findings in these two functions are gone**: two `errcheck`, the two masked
   `errchkjson`, and the two `gosec` G301. Confirm the two functions are clear as a whole rather than
   checking the four you came for.
2. **`allow.txt` gained no row.**
3. **A `--uniq-by-line=false` run over the file shows nothing revealed and nothing left behind.**
   State the before and after counts. This is the task where that check earns its place.
4. The two tests still assert what they asserted. They cover Pi and non-Pi bandwidth-tuner routing;
   if a test stops exercising that, the repair went too far.

## Limits

- **Do not discard the error.** It is the one repair that provably does not work here, and it is the
  first thing anyone will reach for.
- **Do not add an allow-list row** for either finding or for anything you reveal.
- **Do not touch `--uniq-by-line`** — `hk-e0ybh` owns it and is itself blocked behind the migration.
- Do not fix findings outside these two functions. You own them whole; you own nothing else in the
  file.
