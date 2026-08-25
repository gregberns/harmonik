---
id: greenify-checkrun
title: Decompose StaleWatcher.checkRun so it clears all three complexity linters at once
type: task
priority: 0
labels: [lint, gate, complexity, clear-the-ground]
depends_on: []
blocks: [lint-rekey-exclusion-list]
workstream: W1
batch: 1
---

## Why this exists

`hk-lint-rekey-exclusion-list-t9ebz` cannot land until the tree is green under the OLD keying scheme
— see [`ratchet-scheme-migration.md`](ratchet-scheme-migration.md). One of the 14 findings blocking it
is a complexity finding on `StaleWatcher.checkRun`, and it is the only one of the fourteen that is a
genuine refactor. It gets its own file because the failure mode below has already been walked into
twice on this programme.

**One symbol: `StaleWatcher.checkRun` in `internal/daemon/stalewatch.go`.** Find it by name; the line
number moves.

## THREE LINTERS, ONE LINE, AND YOU MUST CLEAR ALL THREE TOGETHER

| linter | reported | ceiling |
|---|---|---|
| `gocognit` | 65 | 20 |
| `cyclop` | 64 | 15 |
| `funlen` | 152 statements | 60 |

**Only `gocognit` is visible in the report.** `--uniq-by-line` shows at most one finding per line, so
`cyclop` and `funlen` are masked behind it (`hk-e0ybh`). **Clear `gocognit` alone and the other two
surface as untolerated findings** — the judge then fails, tolerating them needs rows the ratchet
refuses, and you are back in the deadlock having done the refactor.

> **A partial decomposition that satisfies the metric you were shown is the expected failure here,
> not a hypothetical one.**

**This programme has been met one metric at a time twice already:**

- An implementer met "under complexity 30" on four functions by renaming each one, moving the body
  verbatim behind an inline `//nolint:funlen,gocognit,cyclop`, and leaving a pass-through wrapper
  under the old name. A body moved verbatim cannot change complexity.
- A second moved a 540-line body into a new name behind a 14-line shim. Reverted as `2083adcfa`.

**Name only the metric you measured and you will get exactly that metric.**

## Done when

1. **`gocognit`, `cyclop` AND `funlen` are all clear on `checkRun` and on every function you split out
   of it.** State all three measured numbers for every resulting function in the commit body. A
   number for one linter is not evidence about the others — they count different things, which is why
   64 and 65 are not the same finding.
2. **No new `nolint` directive for a complexity linter appears anywhere in the diff.** Greppable, and
   that is the point. A suppression is not a decomposition.
3. **The extracted functions carry real behaviour, not a renamed body.** A body moved verbatim cannot
   change complexity, so if a metric fell without the code changing shape, something is wrong.
4. **`allow.txt` gained no row.**
5. **A `--uniq-by-line=false` run shows nothing revealed** anywhere you edited. State before and
   after counts.
6. **The stale-watch behaviour is unchanged.** This is daemon code on the run-liveness path. The
   existing tests must pass untouched — if you find yourself editing a stale-watch test to make it
   agree with your refactor, stop: that is the refactor changing behaviour.

## Limits

- **Do not add an allow-list row**, and do not add a complexity `nolint`.
- **Do not change what `checkRun` does.** This is a decomposition, not a redesign. If you believe the
  logic is wrong, say so in a bead rather than fixing it here.
- **Do not edit doc comments inside the move.** Comments are re-examined separately, and spec-bearing
  comment has already been lost to a move on this programme once.
- **Take your own measurements.** Complexity figures quoted in an earlier run's verdict on this
  programme matched no command in this repository. Run the linter yourself.
