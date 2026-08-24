---
id: runbead-extract-decisions
title: harmonik run parses its flags, decides four things and starts a daemon in one 540-line function
type: task
priority: 1
labels: [cli, run-machine, clear-the-ground]
depends_on: []
blocks: []
workstream: W2
batch: 3
---

## Problem

`cmd/harmonik/run.go` `runBeadSubcommandIO` is **540 lines** (lines 45–584, brace to brace). Its
signature is:

```go
func runBeadSubcommandIO(subArgs []string, stdout io.Writer) int
```

Two parameters — and that is the trap. The narrow signature hides that the function reads `os.Args`
indirectly, `os.Getenv("TMUX")`, `os.Stderr`, the file system, the pidfile and a persisted queue,
and then starts a daemon. Measured 2026-08-24 from the tree:

| Measure | Ceiling in `.golangci.yml` | `runBeadSubcommandIO` today |
|---|---|---|
| `cyclop` (cyclomatic) | 15 | **124** |
| `gocognit` (cognitive) | 20 | **138** |
| `funlen` | 100 lines / 60 statements | **540 lines** |

`PLAN.md` §W2.3 says 540 lines and that agrees. The cognitive figure is from
`~/go/bin/gocognit -top 25 ./cmd/harmonik`; the cyclomatic figure is from a `go/ast` walk, because
`golangci-lint` is not installed on this machine. That walk reproduces the plan's recorded
cyclomatic figures for `driveDotWorkflow` (94) and `beadRunOne` (77) exactly.

**Unlike `run() int`, this function does contain real decisions**, and they are already interleaved
with the effects that act on them:

- A hand-rolled argument parser: `for i := 0; i < len(subArgs); i++` at line 62 runs to line 192 and
  fills sixteen locals. It prints to `os.Stderr` and returns an exit code from inside the loop.
- Which beads to run: the `switch` at line 194 over `--beads` against positional arguments,
  including the refusal to mix them.
- Which workflow to use: the `switch` at line 240 over `--workflow-mode`, which also refuses the
  retired review-loop mode and requires `--workflow-ref` for `dot`.
- Whether to turn the notification stream on by itself: line 271,
  `!notifyStreamSet && !noNotifyStream && (len(beadIDs) > 1 || maxConcurrent > 1)`.
- What to do about a queue that is already on disk: lines 440–466. Given the existing queue's status
  and the pidfile lock state, the answer is one of three — archive it and go on, go on, or refuse.
  Today the decision, the archiving and five `Fprintf` calls are one block.

**The pattern to copy is in the same file.** `classifyRunExit` (line 591) is a pure function over
`(*queue.Queue, bool)` returning a `runExitDecision`, and `run_exit_code_test.go` table-tests it. It
is what each of the five above should look like.

**The lint gate forces this to land whole.** `tools/lintreport/allow.txt` carries one tolerated
finding for this function — digest `fc317a056009…`, linter `gocognit`, comment
`# cmd/harmonik/run.go:45 runBeadSubcommandIO`. Since the tolerated findings were re-keyed by content
identity, that key is `sha256(linter, finding text, enclosing symbol body)`. Editing the body changes
the key, the gate then sees a finding it does not hold, and adding the new key trips
`scripts/lint-allow-ratchet.sh`. So there is no half-way landing that keeps `make fast` green: either
finish the function in one commit, or hold intermediate commits green behind a `//nolint` you delete
at the end.

## Scope

- `cmd/harmonik/run.go` — `runBeadSubcommandIO`, and `classifyRunExit` as the shape to match.
- `cmd/harmonik/run_exit_code_test.go` and `cmd/harmonik/run_stream_default_test.go` — the existing
  tests over this file's decisions. `run_stream_default_test.go` already covers the auto-enable rule;
  do not duplicate it, extend it to call the extracted function.
- `cmd/harmonik/help_test.go` — calls `runBeadSubcommandIO([]string{"--help"}, &buf)` and pins exit
  code 0. It must pass unmodified.
- `tools/lintreport/allow.txt` — one line deleted, none added.

Work one decision at a time: lift a decision to a pure function over values, delegate to it, cover it
with a table test, then move to the next. The argument parser goes first, because every other
decision reads what it produced. Give it a named options type and return it, rather than filling
sixteen locals.

## Done when

1. `runBeadSubcommandIO` is under all three ceilings: `cyclop` below 15, `gocognit` below 20,
   `funlen` below both 100 lines and 60 statements. Quote all three in the commit body, measured with
   any suppression gone.
2. **Argument parsing returns a value.** A named options type carries the parsed flags; the parser is
   a function that takes `[]string` and returns that type plus an error, writes nothing to
   `os.Stderr`, and returns no exit code. A table test covers it, including at least: `--beads` with
   a positional bead, an unknown `--workflow-mode`, `--workflow-mode dot` with no `--workflow-ref`,
   and the retired review-loop mode.
3. **The stale-queue answer is a value, not a code path.** A pure function takes the existing queue's
   status and the pidfile lock state and returns which of the three answers applies. A table test
   covers all three, including the `PausedByFailure`, `Cancelled` and dead-daemon cases. Archiving
   and printing stay at the call site.
4. Each of the other three decisions named in Problem is a function that takes values and returns
   values, and each has a table test. For each one, record in the commit body one deliberate mutation
   of the production call path that makes its test fail. A test that passes whether or not the
   production code calls the decision has proved nothing.
5. The `gocognit` line for `runBeadSubcommandIO` is **deleted** from `tools/lintreport/allow.txt`
   because the finding is gone. `git diff tools/lintreport/allow.txt` shows deletions and no
   additions. Any temporary `//nolint` is deleted too — `nolintlint` runs with `allow-unused: false`,
   so it will tell you if you forget.
6. Every existing test in `cmd/harmonik` passes unmodified except `run_stream_default_test.go`, which
   may be re-pointed at the extracted function. No change to any message on `os.Stderr` or to any
   exit code.

## Limits

- **Do not widen `tools/lintreport/allow.txt`.** Not one added line.
- **Do not delete a doc comment to move a metric.** `funlen` runs with `ignore-comments: true`, so
  comments count for nothing here. The parser locals carry the only written record of what several
  flags are for (`--beads`, `--context`, `--notify-stream`, `--param`, `--forbid-default-main`);
  those comments move to the options type's fields. A previous implementer on this program lost a
  comment that carried the specification during a move.
- **Do not move anything into another package.** `cli-extract-logic` owns the move of
  `classifyRunExit` and the queue-archive logic into `internal/runlaunch`, and this task is listed as
  blocking it on purpose: extract the decision here where it is, then let that task move a clean
  function. Extract-then-move is reviewable; move-then-extract is two rewrites of the same code.
- **Do not touch `run() int` in `cmd/harmonik/main.go`.** That is `run-verb-table`, running in
  parallel, and it is the other half of the same package.
- **Do not extract a helper shared with `main.go`.** Both files build a `daemon.Config` and both call
  `selectSubstrate`. The duplication is real and it is a follow-up. Collapsing it here is what turns
  two parallel lanes into a conflict.
- Do not change what `harmonik run` does. Same beads dispatched, same queue written, same output.
