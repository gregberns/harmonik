---
id: run-verb-table
title: The command-line tool picks its verb with 55 sequential if-statements in one 774-line function
type: task
priority: 1
labels: [cli, run-machine, clear-the-ground]
depends_on: []
blocks: []
workstream: W2
batch: 3
---

## Problem

`cmd/harmonik/main.go` `run() int` is **774 lines** (lines 150–923, brace to brace), takes **zero
parameters**, and reads every input from package-level state: `os.Args`, `os.Getenv`, `os.Stdout`,
`os.Stderr`. Measured 2026-08-24 from the tree, not copied from the plan.

| Measure | Ceiling in `.golangci.yml` | `run` today |
|---|---|---|
| `cyclop` (cyclomatic) | 15 | **227** |
| `gocognit` (cognitive) | 20 | **262** |
| `funlen` | 100 lines / 60 statements | **774 lines** |

`PLAN.md` §W2.3 says 770 lines. It is 774. Use the measured figure. The cognitive figure comes from
`~/go/bin/gocognit -top 25 ./cmd/harmonik`; the cyclomatic figure comes from a `go/ast` walk, because
`golangci-lint` is not installed on this machine. That walk reproduces the two cyclomatic figures the
plan already records for `driveDotWorkflow` (94) and `beadRunOne` (77) exactly, so it is measuring
what `cyclop` measures.

**The shape is not the same as `runWorkLoop`'s.** This function is not a state machine. It is a verb
table written out longhand: about 55 consecutive blocks of the form
`if len(os.Args) >= 2 && os.Args[1] == "<verb>" { return run<Verb>Subcommand(os.Args[2:]) }`, one per
subcommand, followed by roughly 256 lines that parse the daemon's own flags, build a
`daemon.Config`, check the yank ledger and call `daemon.Start`. The CLI structure assessment reached
the same reading: wide and shallow, "exactly the shape a command-line dispatch layer is supposed to
have". So the fix is a table and a split, not a hunt for pure decisions.

**The reason it cannot be left alone is the lint gate, not the number.** `tools/lintreport/allow.txt`
carries one tolerated finding for it — digest `873880800dbd…`, linter `gocognit`, comment
`# cmd/harmonik/main.go:150 run`. Since `fix(lint): key tolerated findings by content identity`
landed, that key is `sha256(linter, finding text, enclosing symbol body)`. Both inputs move when you
touch the function: the body changes, and the finding text embeds the complexity number. So a partial
improvement produces a key the list does not hold, `scripts/lint-allow.sh` fails, and adding the new
key trips `scripts/lint-allow-ratchet.sh`. **There is no half-way landing that keeps `make fast`
green.** Plan for one commit that takes the function under the ceiling, or hold intermediate commits
green behind a `//nolint` you delete at the end.

## Scope

- `cmd/harmonik/main.go` — `run`, and the four small helpers below it
  (`inFlightDrainGoroutine`, `startSupervisorWatchdogIfEnabled`, `buildSupervisorWatchdogSpec`,
  `spawnCapFromEnv`).
- `cmd/harmonik/usage.go` — `unknownSubcommand`, `daemonStartRefusal`, `trailingPositional`,
  `harmonikUsage`. The verb table and the usage text are the same list written twice; that is the
  second caller the table buys.
- `tools/lintreport/allow.txt` — one line deleted, none added.

Two moves, in this order:

1. **Make the verb list data.** One entry per subcommand: the verb, and the function that runs it.
   `run` becomes a lookup and a call. `unknownSubcommand` and `harmonikUsage` read the same table
   rather than repeating it.
2. **Split the daemon-start path out.** Everything from the flag declarations to `daemon.Start`
   (`main.go` lines 668–923 today) is one job — parse the daemon's flags, resolve the project
   directory, build `daemon.Config`, refuse a yanked binary, start. It is not part of choosing a
   verb. Give it its own function in `package main`.

Six verbs do more than forward before they dispatch — `tmux-start`, `hook-relay`, `queue`, `worker`,
`keeper` and `start` (which sets `startDaemonRequested`). They are the entries that will not fit a
one-line table row. Decide once how a table entry carries pre-work, rather than leaving six special
cases beside the table.

## Done when

1. `run` is under all three ceilings: `cyclop` below 15, `gocognit` below 20, `funlen` below both
   100 lines and 60 statements. Quote all three in the commit body, measured with the suppression
   gone.
2. **The set of verbs exists as one named value**, and a table test asserts the two lists agree: every
   verb the table holds appears in the usage text, and every verb the usage text names has a table
   entry. Prove the test observes the production path — add a verb to the table only, and record that
   the test fails.
3. The daemon-start path is a separate named function that `run` calls, and it is the only place
   `daemon.Config` is built in `main.go`.
4. The `gocognit` line for `run` is **deleted** from `tools/lintreport/allow.txt` because the finding
   is gone. `git diff tools/lintreport/allow.txt` shows deletions and no additions. If you used a
   temporary `//nolint` to keep intermediate commits green, it is deleted too —
   `nolintlint` is configured with `allow-unused: false`, so an unused directive is itself a finding
   and will tell you.
5. Every existing test in `cmd/harmonik` passes **unmodified**, including
   `unknown_subcommand_test.go`, which pins that a flag-first argument list is not a verb and that
   `harmonik` with no verb does not start a daemon.
6. No change to any exit code. `exitUnknownSubcommand` is 2, a locked pidfile is 5, a yanked binary
   is 9. Say in the commit body how you checked.

## Limits

- **Do not widen `tools/lintreport/allow.txt`.** Not one added line, for any reason. If a
  half-finished state cannot pass the gate, that is the signal to finish the function, not to seed a
  key.
- **Do not delete a doc comment to move a metric.** `funlen` is configured `ignore-comments: true`,
  so comments count for nothing here and deleting one buys nothing. Several of the verb blocks carry
  the only written record of why a verb behaves the way it does; carry that text to the table entry.
  A previous implementer on this program lost a comment that carried the specification during a
  move.
- **Do not move anything out of `cmd/harmonik`.** The CLI structure assessment ruled `main.go`
  **no move** — the complexity is the problem, the placement is not. Extracting the logic that
  belongs in packages is `cli-extract-logic`, and its own limits already exclude `run`.
- **Do not extract a helper shared with `cmd/harmonik/run.go`.** `runBeadSubcommandIO` builds a
  second `daemon.Config` and calls `selectSubstrate` too, and `runbead-extract-decisions` is editing
  that file at the same time. The duplication is real and it is a follow-up, not this task. Touching
  it here is what turns two parallel lanes into a conflict.
- Do not change what any verb does. This task changes how the verb is chosen.
