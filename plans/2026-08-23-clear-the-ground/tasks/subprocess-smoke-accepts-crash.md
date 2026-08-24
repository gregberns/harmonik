---
id: subprocess-smoke-accepts-crash
title: The only test that boots the real binary calls a crashed run a pass
type: bug
priority: 0
labels: [testing, gate, dispatch, clear-the-ground]
depends_on: []
blocks: []
workstream: W0
batch: 3
---

## Problem

`make full` is the merge decision. Its main sweep is `go test -short ./...`, so every
`testing.Short()` guard is skipped, and one test in it boots the real `harmonik` binary as an
operating-system process. That test accepts a crashed implementer as a pass.

**Measured on this box, 2026-08-24.** One command, no harness in between:

```
go test -tags=subprocess -timeout 5m -count=1 -v ./cmd/harmonik -run TestSubprocessDaemonBootSmoke
```

```
    subprocess_boot_smoke_test.go:83: terminal event observed: {"type":"run_failed", …
      "summary":"dot: agentic node \"implement\" failed: node \"implement\" (implementer)
      agent_failed class=structural sub_reason=claude_crashed exit=1"}
--- PASS: TestSubprocessDaemonBootSmoke (22.17s)
```

The agent crashed, no commit landed, the bead never closed, the queue paused — and the test printed
PASS. `make full` runs this through `make test-subprocess`, and the recipe comment in the `full`
target presents it as the thing that stands in front of a regression in the boot path a real
operator takes.

**How it passes.** `subprocessSmokeWaitTerminal` in `cmd/harmonik/subprocess_boot_smoke_test.go`
reads the event log as text and returns true on the first line that contains `run_completed` OR
`run_failed` OR `bead_closed`. It never decodes the JSON, so it judges by substring: a `run_failed`
event whose summary quotes the words "run_completed" satisfies it as readily as a completed run. It
never reads `run_id` or `bead_id`, so a terminal event belonging to any other run satisfies it too.
`run_failed` is a failure and the function counts it as the answer.

**The pipeline this test drives really does reach the agent.** Hand-driven run of the same wiring,
2026-08-24, every event in order:

```
run_started(bead_id=sub-5qo) → node_dispatch_requested(start) → node_dispatch_decided
→ node_dispatch_requested(implement) → harness_selected → model_selected
→ handler_capabilities → session_log_location → skills_provisioned → launch_initiated
→ agent_heartbeat → agent_ready → implementer_phase_complete(exit_code=1, commit_landed=false)
→ run_failed(success=false) → queue_group_completed → queue_paused
```

So there is real coverage here — boot, socket, CLI submit, dispatch, worktree, agent launch — and
the test asserts none of it. It asserts that one of three words appeared somewhere in a file.

## What the bead gets wrong, and it matters for the fix

The bead reads the acceptance of `run_failed` as an oversight. It is not. The commit that added the
test (`1dbfddfe3`) states the choice: the generic twin speaks harmonik-native NDJSON, not the Codex
app-server wire protocol, so the driver handshake fails fast and the run reaches `run_failed`
without tmux, a real agent, or the network. A clean bead close was assigned to the container leg.

That changes the fix. **A completed run is not reachable from this test today.** Measured:

- The real binary has exactly one substrate injection point. `cmd/harmonik/main.go` defines
  `--default-harness` and `--codex-binary` and no other harness-binary flag, and
  `cmd/harmonik/substrate_select.go` `selectSubstrate` returns the tmux substrate for every value of
  `HARMONIK_SUBSTRATE` except `codexdriver`.
- `daemon.Config.HandlerBinary` and `HandlerArgs` — the seam the scenario tier uses to point the
  daemon at a twin — are set in-process only. No flag, no environment variable and no config key
  reaches them; `internal/daemon/bootworkloop.go` defaults `HandlerBinary` to `claude`.
- Nothing in the tree speaks the Codex app-server protocol as a process.
  `cmd/harmonik-twin-codex` mimics the `codex exec` command-line surface and its JSONL stdout.
  `cmd/harmonik-twin-generic` speaks harmonik NDJSON over a socket. `internal/codexdigitaltwin` is a
  library that replays a corpus in-process.

So "assert `run_completed`" cannot be satisfied without new product surface. **This task does not
add that surface.** It makes the test state the outcome it expects, refuse anything else, and say
plainly what it does not prove.

## Scope

Three files. One new, one rewritten, one comment change.

### 1. New: `cmd/harmonik/subprocess_smoke_judge_test.go` — no build tag

It carries **no** `//go:build` line. That is the point: it compiles into the ordinary
`./cmd/harmonik` test binary, so `make full`'s main sweep runs it even though the live smoke test
sits behind the `subprocess` tag.

It holds the judgment the live test now uses:

```go
type subprocessSmokeRun struct {
    RunID    string          // run_started whose payload.bead_id equals the submitted bead
    Nodes    map[string]bool // node_dispatch_requested payload.node_id, for RunID only
    Terminal string          // "", "run_completed" or "run_failed", for RunID only
    Success  bool
    Summary  string
}

func scanSubprocessSmokeEvents(jsonl []byte, beadID string) (subprocessSmokeRun, error)
```

Rules the scanner obeys, and there is nothing else to decide:

- Decode every non-empty line with `encoding/json`. Match nothing by substring.
- Only the **last** line may fail to decode, and only then is it skipped — the log is appended while
  the test polls, so a partial final line is normal. Any earlier line that fails to decode returns
  an error. A corrupt log must not read as "no terminal event yet".
- The first `run_started` whose `payload.bead_id` equals `beadID` sets `RunID`. Until `RunID` is
  known, no other event is recorded.
- `node_dispatch_requested` records `payload.node_id` only when `payload.run_id` equals `RunID`.
- `run_completed` and `run_failed` set `Terminal`, `Success` and `Summary` only when
  `payload.run_id` equals `RunID`.

Plus `TestScanSubprocessSmokeEvents`, a table test over four fixtures in
`cmd/harmonik/testdata/subprocess-smoke/`. It must not call `testing.Short()`.

| Fixture | What it holds | Required judgment |
|---|---|---|
| `crashed-run.events.jsonl` | The real log of a real run. Capture it with the commands below. | `Terminal == "run_failed"`, `Success == false`, `Nodes["implement"]` true |
| `completed-run.events.jsonl` | Hand-written: `run_started` (bead + run id), `node_dispatch_requested` (implement), `run_completed` (`success: true`) | `Terminal == "run_completed"`, `Success == true` |
| `summary-quotes-completed.events.jsonl` | A `run_failed` whose `summary` contains the literal text `expected run_completed` | `Terminal == "run_failed"`. The old substring scan called this a completed run |
| `other-run-terminal.events.jsonl` | `run_started` for our bead, then a `run_completed` carrying a **different** `run_id` | `Terminal == ""`. A terminal event for another run is not our answer |

Capture the first fixture from a real run — the live test's project directory is a temporary
directory that is removed at cleanup, so drive it by hand:

```
CGO_ENABLED=0 go build -o /tmp/hk-fx/harmonik ./cmd/harmonik
CGO_ENABLED=0 go build -o /tmp/hk-fx/generic-twin ./cmd/harmonik-twin-generic
# scratch project: git init + a bare origin + br init --prefix sub + br create … --labels model:o4-mini
HARMONIK_SUBSTRATE=codexdriver /tmp/hk-fx/harmonik start daemon --project "$P" \
    --default-harness codex --codex-binary /tmp/hk-fx/generic-twin &
/tmp/hk-fx/harmonik queue submit --project "$P" --beads "$BEAD"
# wait for run_failed, then copy "$P"/.harmonik/events/events.jsonl whole
```

Keep the captured file whole — it is about 27 lines. Record in the test's doc comment when it was
captured and from which commit.

### 2. Rewritten: `cmd/harmonik/subprocess_boot_smoke_test.go`

- Delete `subprocessSmokeWaitTerminal`. Poll with `scanSubprocessSmokeEvents` instead.
- Rename the test to `TestSubprocessDaemonBoot_SubmitReachesAgentThenFailsStructurally`.
- Assert, in this order, all against the run the test submitted:
  1. `Nodes["implement"]` becomes true. This is the claim that the real binary took a
     command-line submit and dispatched it to an agent node.
  2. A terminal event arrives for that same `RunID`.
  3. `Terminal == "run_failed"`, `Success == false`, and `Summary` contains `class=structural`.
  4. `Terminal == "run_completed"` **fails the test**, with a message that says the expected
     outcome is now wrong and must be re-decided rather than widened. A completed run means the
     wiring changed, and a negative test that quietly accepts a positive result proves nothing.
- Give the file a doc comment stating the boundary in plain words: what the test proves, and that
  it proves nothing about an agent doing work, because the twin cannot speak the protocol the
  driver speaks.
- Turn the three `t.Skip` guards (`go`, `br`, `git`) into `t.Fatalf`. `scripts/go-test-must-match.sh`
  says in its own header that it cannot see a `t.Skip`: an all-skipped package prints a plain `ok`.
  This is the merge decision's only real-binary leg, so a box that cannot run it must say so.

### 3. `Makefile`

- Update the `-run` filter in `test-subprocess` to the new test name.
- The `test-subprocess` header comment and the comment inside the `full` recipe both say the test
  "asserts a terminal event". Replace that with what it now asserts, and add this sentence, in these
  words, to both places:

  `no test in make full proves a bead can go from queue submit to a closed bead through the real binary`

  followed by the one-line reason: the real binary's only substrate injection is
  `--codex-binary` with `HARMONIK_SUBSTRATE=codexdriver`, and no binary in the tree speaks the
  Codex app-server protocol.

## Traps

- **The Makefile filter and the test name move together.** `scripts/go-test-must-match.sh` fails the
  target when `-run` matches nothing. That is the safety net working, not an obstacle to route
  around. Never edit that script.
- **`agent_ready` fires on the crashed run too.** The Codex adapter has no readiness handshake, so
  the infrastructure emits the event. It is not evidence the agent worked. Do not assert on it.
- **Do not point `--codex-binary` at `cmd/harmonik-twin-codex` to "fix" the crash.** That twin
  mimics `codex exec` and its JSONL stdout, not the app-server JSON-RPC the driver speaks. It
  crashes the same way, and the test would then claim a success it did not get.
- **The judge file must not carry the `subprocess` tag.** With the tag, its table test never runs in
  `make full`'s main sweep and the whole proof disappears from the merge decision.
- **`make lint-allow` is not a verdict on the tagged file.** `.golangci.yml` sets no `build-tags`
  key, so golangci-lint never builds `subprocess`-tagged files, and complexity linters are excluded
  for `_test.go` in any case. Put the proof in tests, not in a lint run.
- `gate-test-compile` does compile the tagged tests (`TAGGED_BUILD_TAGS` in the Makefile), so a
  compile error there fails `make fast`. Nothing in `make fast` runs them.

## Done when

Every item is checked by a command. Record the observed output of items 3 through 6 in the commit
body — a test that has never failed against the bug it was written for measures nothing.

1. `make test-subprocess` exits 0, and its output names
   `TestSubprocessDaemonBoot_SubmitReachesAgentThenFailsStructurally`. The old name appears nowhere:
   `grep -rn "TestSubprocessDaemonBootSmoke" . --include='*.go' --include=Makefile` returns nothing.
2. `go test -short -count=1 -run TestScanSubprocessSmokeEvents ./cmd/harmonik` prints `ok` and does
   **not** print `[no tests to run]`. This proves the judgment runs inside `make full`'s main sweep.
3. **The live assertion is connected to reality.** Change the expected terminal outcome in the live
   test from `run_failed` to `run_completed`, run `make test-subprocess`, and observe a FAILURE whose
   message names the outcome actually seen. Revert. Before this task the same mutation could not
   fail, because the test accepted either word.
4. **The substring defect is dead.** `go test -count=1 -run TestScanSubprocessSmokeEvents
   ./cmd/harmonik` passes with the `summary-quotes-completed` fixture in place, and the fixture's
   expected judgment is `run_failed`. Show the fixture line that contains the text `run_completed`
   and the assertion that refuses it.
5. **Correlation is real.** The `other-run-terminal` fixture yields `Terminal == ""`. Delete the
   `run_id` comparison in the scanner, re-run, observe the case FAIL, restore it. Record both runs.
6. **A missing tool fails instead of skipping.** Run `make test-subprocess` with `br` removed from
   `PATH` and observe a FAILURE, not `ok`. Before this task the same run printed `ok` and tested
   nothing.
7. No new `nolint` directive for a complexity linter (`funlen`, `cyclop`, `gocognit`) appears
   anywhere in the diff, and `tools/lintreport/allow.txt` gains no line:
   `git diff -- tools/lintreport/allow.txt` is empty, and `git diff | grep -n 'nolint'` shows only
   the `//nolint:gosec` directives that were already in the smoke-test file.
8. Both Makefile comments carry the sentence named in Scope §3, verbatim:
   `grep -c "no test in make full proves a bead can go from queue submit to a closed bead through the real binary" Makefile`
   returns `2`.
9. `make full` still reaches `test-subprocess` and passes it inside the 5-minute budget. One daemon
   boot measured 22 seconds on 2026-08-24; expect the same order.

## Limits

- **Do not add a way to make a successful run reachable.** No new flag, environment variable or
  config key on `start daemon`. No new twin binary. That gap is real, it is named in Scope §3, and
  the operator schedules it as its own task.
- **Do not widen the accepted outcome set back to "any terminal event"**, under any name, in any
  helper.
- Do not delete the live test, move it out of `make full`, or put it behind `-short`.
- Do not edit `BATCH-GATE.md` — it is the operator's file and it is not tracked.
- Do not edit `scripts/go-test-must-match.sh`.
- Do not touch the scenario tier. It has its own end-to-end claims and they are not in question here.
