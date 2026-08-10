# Evidence

## Commands

The tools were built from `/Users/gb/github/codebase-organism`.
The review used these command forms:

```sh
typegraph -repo /Users/gb/github/harmonik -out /tmp/review/harmonik ./...
detect -repo /Users/gb/github/harmonik -pkg ./... -out /tmp/review/findings.json
mq -graph /tmp/review/harmonik_symbols.json
hotspots -repo /Users/gb/github/harmonik -graph /tmp/review/harmonik_symbols.json -top 40
coref -graph /tmp/review/harmonik_symbols.json -out /tmp/review/coref.json
funcseam -repo /Users/gb/github/harmonik -pkg ./internal/queue -list
funcseam -repo /Users/gb/github/harmonik -pkg ./internal/daemon -list
```

The updated tool suite passed its twice-run byte comparison before this review.

## Detector census

| Class | Count |
| --- | ---: |
| A1 | 1,342 |
| A2 | 652 |
| A4 | 13 |
| B1 | 150 |
| B2 | 160 |
| B4 | 81 |
| Total | 2,398 |

The A1 set contains 737 ambiguous findings.
It contains 1,142 findings where the referencing package does not declare a candidate.
Only 54 findings are same-package and unambiguous.

The A2 set contains 4,896 source sites.
It contains 548 values that touch more than one file.
One value touches 68 files.
The updated records retain every site and can now produce correct file locks.
This fixes scheduling safety.
It does not prove that the repeated text has one meaning.

## Structure and history

The symbol graph contains 9,256 declarations and 22,522 ties.
The current package partition has mean MQ 0.719.
The best found partition has mean MQ 0.805.
The gap is 11 percent.

Package LOC Gini is 0.77.
The five largest packages hold 56 percent of the code.
The result still points to concentration before package reshuffling.

`internal/daemon/workloop.go` is the highest current hotspot.
`beadRunOne` is about 959 lines.
`runWorkLoop` is about 1,351 lines.
`driveDotWorkflow` is about 1,010 lines.
`runAgentLaunch` is about 767 lines.

The history report also names deleted files such as `internal/daemon/reviewloop.go`.
Those rows are historical evidence only.
They are not current implementation targets.

## Corrected STATE facts

The graph now contains 35 STATE edges and 17 mutable nodes.
Three STATE edges cross a package boundary.
They are writes from the root command package to `supervise` hook variables.

The former claim that cross-package STATE was zero was false by construction.
Any old ranking that used that zero as evidence must be rechecked.
This review does not infer a new STATE weight from three edges.

## Queue completion facts

`internal/queue/state.go` `newEvent` marshals payloads, creates UUIDv7 identity, and reads the system clock.
`AdvanceGroup` calls it.
`AppendItems` calls it.

`internal/daemon/scheduler.go` does not preserve those envelope fields.
It sends the event type and payload to the event bus.
The event bus creates another identity and envelope time.

The queue package therefore creates effects that are discarded.
The queue functions are not pure despite their value-shaped signatures.

The current final-completion path calls the old `CompleteAndUnlink` helper.
The current queue specification requires completed canonical bytes and a bound completion receipt before observation and cleanup.
The general transaction owner says that it has no completion-receipt behavior.
The larger completion rewrite therefore lacks its required durable input contract.

## Detector interpretation

The corrected A2 lock set removes one old blocker.
The semantic blocker remains.
Struct tags, CLI tokens, test-twin protocol text, and domain vocabulary still share the class.

The B1 and B2 detector both nominate `AppendItems` and the daemon completion path.
That supports direct inspection.
It does not define the extraction contract.

The B4 detector still reports wire records, state records, configuration values, and dependency bags together.
Field count alone does not justify a split.
