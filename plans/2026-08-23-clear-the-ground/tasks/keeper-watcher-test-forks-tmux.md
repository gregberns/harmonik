---
id: keeper-watcher-test-forks-tmux
title: Keeper watcher tests fork real tmux subprocesses inside a 150ms budget, so the batch gate measures machine load
type: bug
priority: 1
labels: [keeper, test-debt, gate, clear-the-ground]
depends_on: []
blocks: []
workstream: W5
batch: 3
---

## Problem

The batch gate went red on 2026-08-24 with `internal/keeper` failing
`TestWatcher_InjectDeliveredAfterQuiescence`. **The branch was green. The gate was measuring how busy
the machine was.**

Measured cause. `TestWatcher_*` tests set `InjectFn` to a spy but leave `SelfHintInjectFn` unset.
`WatcherConfig` then defaults it to the real `InjectText`, which shells out to `tmux load-buffer` and
`tmux paste-buffer`. `TestWatcher_InjectDeliveredAfterQuiescence` uses `TmuxTarget: "fake-pane"`,
which never resolves, so the pending hint never clears and the watcher forks **two real subprocesses
on every 10 ms poll tick**. The
self-hint block runs before the quiescence check in the same tick, synchronously, and the whole test
has a 150 ms wall-clock budget. Under subprocess-spawn contention the forks eat the budget, the
watcher never reaches a quiesced tick, and the spy sees zero calls. The failing log line
`keeper: tmux paste-buffer: signal: killed` is the run context expiring mid-command.

Measured behaviour, 2026-08-24:

| condition | result |
|---|---|
| isolation, `-count=20` | 20 / 20 pass |
| raw CPU load, 12 spinners, `-race -tags=scenario -count=30` | 30 / 30 pass |
| compile load (`go build -a ./...` + `go vet -tags=scenario`) | **2 of 5 attempts FAIL** |

Raw CPU is not the trigger; subprocess-spawn contention is. **The second reproduction failed a
second test, not the same one** — `TestWatcher_RespawnFiredWhenGauseAbsentAndPaneIdle`. Spell it
`Gause`, not `Gauge`: the function name in `internal/keeper/watcher_test.go` carries a typo, and its
own doc comment one line above spells it `Gauge`. `go test -run` with the corrected spelling matches
no test and exits 0, which reads as "fixed".

**It is 4 tests of 16, not the whole family.** Measured 2026-08-24 in
`internal/keeper/watcher_test.go`: 16 `TestWatcher_*` functions, each setting `TmuxTarget` exactly
once. Twelve set it to `""`. `internal/keeper/watcher.go` gates the pending-hint injection on
`w.cfg.TmuxTarget != ""`, so those twelve cannot reach a subprocess at all. The four that can are
`TestWatcher_InjectDeliveredAfterQuiescence` (`"fake-pane"`) and the three respawn tests
`TestWatcher_RespawnFiredWhenGauseAbsentAndPaneIdle`, `TestWatcher_RespawnSkippedWhenPaneNotIdle` and
`TestWatcher_RespawnCooldownPreventsDoubleSpawn` (all `"dummy-pane"`).

The mechanism is unchanged by that narrowing. A test that leaves `SelfHintInjectFn` unset gets the
real `InjectText`, and any future test that also sets a non-empty `TmuxTarget` joins the four. The
defect is the default, not the count.

This is not a regression. `git log -- internal/keeper/` shows only comment-stripping, fixture
migration and de-timing work since the assertion's own fix commit, and 55 clean isolated runs show
the guarded behaviour is intact.

## Scope

`internal/keeper/watcher_test.go` — the four `TestWatcher_*` tests named above, which are the ones
that set a non-empty `TmuxTarget`. `internal/keeper/watcher.go` and `internal/keeper/injector.go` for
the defaulting behaviour that makes an unset `SelfHintInjectFn` reach a real subprocess.

The repair belongs at the seam, not in the four tests. Fix the default and the other twelve stay
correct for a reason instead of by accident.

## Done when

1. No test in `internal/keeper` can start a `tmux` subprocess. Prove it: a test-only guard that fails
   if the real inject path is reached during a unit test, or an injected fake at the seam. A comment
   asking future authors to remember is not enough — this defect is exactly a field somebody forgot.
2. `go test ./internal/keeper/ -race -tags=scenario -run TestWatcher -count=5` passes while
   `go build -a ./...` runs concurrently. That is the condition that reproduces it today.
3. The behaviour each test guards still fails when broken. Mutate the watcher's retry-on-crossing-tick
   path and watch `TestWatcher_InjectDeliveredAfterQuiescence` go red, so the repair is not a test that
   passes because it now checks nothing.
4. Any test whose real subject is the tmux command path is named as such and moved out of the unit
   tier, rather than being given a longer budget.

## Limits

- **Do not fix this by raising the 150 ms budget.** A longer budget hides the fork storm instead of
  removing it, and the failure returns on a busier box.
- **Do not fix it by marking the tests as skipped, short-only, or serial.** The tier already carries 13
  tests skipped for retired behaviour; adding more skips is how the signal was lost.
- Do not change what the watcher does. This is test debt. A production change here needs its own task.
- Do not widen `tools/lintreport/allow.txt`.
- Do not delete doc comments to satisfy any metric.

## Note for whoever schedules this

Separately worth deleting rather than skipping: 13 keeper cycler tests are skipped with
"keeper-checkpoint-handshake: timeout abort is retired". They test behaviour that no longer exists.
That is a different task; do not fold it in here.
