# Evidence

## Repository state

The review inspected Harmonik at `43ad681c4` on
`work/alpha-integration-merge`. The branch is 88 commits ahead of its upstream.
Pre-existing modified and untracked files were preserved and excluded as evidence.

The analysis suite was built from `codebase-organism` revision `5e16c72` with the
uncommitted structural fixes described in its `HANDOFF.md`. Before use:

```text
make lint          PASS, 0 issues
make test          PASS, Go tests and 21 Python tests
make determinism   PASS, every tool output reproducible
```

The binaries and raw output live in the temporary directory
`/tmp/harmonik-review-20260822.ReocCf`; it is not a durable artifact.

## Commands

```sh
typegraph -repo /Users/gb/github/harmonik -out /tmp/review/harmonik ./...
detect -repo /Users/gb/github/harmonik -pkg ./... -out /tmp/review/findings.json
mq -graph /tmp/review/harmonik_symbols.json
hotspots -repo /Users/gb/github/harmonik -graph /tmp/review/harmonik_symbols.json -top 40
coref -graph /tmp/review/harmonik_symbols.json -out /tmp/review/coref.json
funcseam -repo /Users/gb/github/harmonik -pkg ./internal/queue -list
funcseam -repo /Users/gb/github/harmonik -pkg ./internal/daemon -list
go test ./internal/queue ./internal/queuewiring ./internal/dispatch ./internal/orchestrator
```

All four focused packages passed. The queue package completed in 4.492 seconds;
queuewiring in 4.591, dispatch in 1.376, and orchestrator in 0.706.

## Detector comparison

| Class | 2026-08-10 | 2026-08-22 | Change |
|---|---:|---:|---:|
| A1 | 1,342 | 1,477 | +135 |
| A2 | 652 | 670 | +18 |
| A4 | 13 | 13 | 0 |
| B1 | 150 | 153 | +3 |
| B2 | 160 | 158 | -2 |
| B4 | 81 | 94 | +13 |
| **Total** | **2,398** | **2,565** | **+167** |

Only 54 A1 findings are same-package, unambiguous candidates—the same count as the
last review. There are 782 ambiguous findings and 1,273 findings whose declared
constant is not proven to be in the same package. A2 contains 5,038 sites; 565
findings span multiple files and the largest spans 73 files.

These counts are candidate pressure, not an implementation queue.

## Graph and modularity

| Measure | 2026-08-10 | 2026-08-22 |
|---|---:|---:|
| declarations | 9,256 | 10,154 |
| edges | 22,522 | 25,541 |
| STATE edges | 35 | 49 |
| mutable nodes | 17 | 19 |
| current MQ | — | 0.723 |
| best measured MQ | — | 0.808 |
| MQ gap | 11% | 10% |
| package LOC Gini | 0.77 | 0.77 |
| top-five LOC share | 56% | 56% |

The hill climb moved 1,709 of 10,154 declarations. The small MQ improvement does not
offset unchanged concentration. Current declaration-span LOC remains dominated by
`internal/daemon` (33,751), `cmd/harmonik` (24,616), `internal/core` (15,570), and
`internal/queue` (7,302).

Direct filesystem measurement is the authority for package size:

| Revision/date | daemon production lines | files |
|---|---:|---:|
| program start, 2026-07-27 | 44,808 | 94 |
| preceding review, 2026-08-10 | 45,020 | 100 |
| current, 2026-08-22 | 49,370 | 110 |

There are also 95,252 daemon test lines. Do not confuse declaration-span LOC from
the graph with whole-file LOC.

## Live function pressure

| Function | LOC | complexity |
|---|---:|---:|
| `runWorkLoop` | 1,432 | 158 |
| `driveDotWorkflow` | 1,033 | 94 |
| `beadRunOne` | 959 | 77 |
| `runAgentLaunch` | 835 | 69 |
| `dispatchDotAgenticNode` | 688 | 69 |
| `pasteInject` | 581 | 85 |
| stale watcher | 427 | 64 |
| orphan sweep | 371 | 42 |

Since the preceding review, `runWorkLoop` grew by 81 lines and `runAgentLaunch` by
68. Queue pressure remains concentrated in `Validate` (444/81), `AppendItems`
(172/25), `writeReplacement` (126/16), and `HandleQueueSubmit` (121/20).

The hotspot history output still names deleted `reviewloop.go`; historical hotspot
rows are not current-source proof.

## Source-verified facts

- `internal/orchestrator.SelectNextQueue` is a pure snapshot-based selector and the
  daemon wrapper delegates to it. C22 is complete, contrary to the new plan text.
- `internal/dispatch/result.go` explicitly records that
  `dispatchstore.Store.Create` has no production caller. Search found calls in tests,
  not the live producer path.
- Boot replay refuses unresolved intents and several replay actions remain
  deliberately unimplemented. Activation is unsafe until a separate design closes
  that state space.
- `internal/runloop.RunEnv` still has 29 fields and `SharedHandles` has 18. C26 is
  not complete.
- `internal/daemon/runregistry.go` remains in the daemon package. The current
  decomposition plan reports a compiler-proved scratch extraction, but no production
  move has landed.

## Limits

Static analysis establishes structure, not semantic ownership. Commit messages and
plans were used to locate work, then current source and tests were used to classify
it. This review did not run the 294-second complete daemon suite or claim coverage
from focused package tests. The untracked decomposition plan contains additional
scratch measurements that were checked for consistency but not all independently
reproduced.
