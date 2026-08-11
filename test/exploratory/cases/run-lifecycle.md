# Cases — live run lifecycle under failure

Format and rules: [`README.md`](README.md).

**This file holds the `protocol` cases, and they are the ones that earn their keep.** Every
expensive finding lane bravo has produced came from one of these, not from a probe. A protocol
is a way of looking, with a question in hand. It does not reduce to an exit code, and it is
supposed to be run by someone who is paying attention.

---

## LP-010 — protocol: ask every health surface the same question and compare the answers

Class: protocol
Exercises: the whole run lifecycle, and every surface that claims to report on it
Bead: found `hk-stop-hook-failure-wedges-run-dc5z6` and two others
Status: the specific defect is FIXED at `2361a2c0d` + `04351ed60`; **the protocol stays open
forever** — it is a method, not a case that closes

**The question:** every health surface says this run is fine. Is it?

Preconditions: a scratch daemon with one real bead dispatched to a real agent. Not a twin —
the point is to observe a system nobody scripted.

Method. At intervals through the run, and especially when nothing seems to be happening, ask
EVERY surface independently and write down all the answers side by side:

    harmonik queue list --project "$SCRATCH"        # workers, status
    harmonik queue status --project "$SCRATCH"      # the operator-facing summary
    harmonik subscribe --project "$SCRATCH" --json  # the event stream, structured
    tail .harmonik/events/events.jsonl              # what was actually written
    git -C <worktree> log --oneline                 # ground truth: did work land?
    ps / tmux ls                                    # is the agent process alive at all?

Then ask the question the surfaces cannot ask themselves: **when did a real event last
arrive?** Not a heartbeat — a real one. Heartbeats continue after the work stops.

Expect: the surfaces agree with each other AND with git.

Failure signature — the shape to recognise, which is more general than any one bug:

> The surfaces agree with each other and disagree with reality.

The instance that produced the bead: an implementer finished, its completion signal failed to
reach the daemon, and for 79 minutes there was no `agent_failed`, no `run_stale`, nothing in
the daemon log, `agent_heartbeat` arriving on cadence, and `queue list` showing
`status=active workers=1`. Last real event at 04:24:54Z. At 05:43:49Z a budget backstop fired
and the run moved on recording `commit_landed: false`.

Why it matters: three separate findings fell out of one sitting with this protocol, and all
three pointed the same way. **A run that is dead and a run that is quiet look identical on
every surface this system offers.** That is the finding; the individual bugs are instances.

Two traps this protocol has already caught in its own results:

- **The recovery can name the wrong cause.** The backstop that fired reported
  `implementer_budget_exceeded`, so a later reader sees an agent that ran out of budget rather
  than one whose completion signal could not be delivered. Check that the recovery names the
  cause, not just that a recovery happened. Mislabels like this cost the project twelve days
  once already, on the pi endpoint probe.
- **A bounded wedge is not the same as no wedge, and it is also not a P1.** This was first
  filed as an unbounded wedge and that was wrong — a backstop existed and it fired. Find the
  backstop before you set the severity.

---

## LP-011 — protocol: run the same bead on every harness and compare what the daemon believes

Class: protocol
Exercises: the completion contract across pi, codex and claude
Bead: found `hk-quit-instruction-not-portable-ms55w`, `hk-codex-success-retried-and-lost-rqxz3`
Status: both FIXED at `b49210d67` and `5450d1cfc`; protocol stays open

**The question:** the agent did the work and committed it. Does the daemon agree?

Method: dispatch one trivial, unambiguous bead — append a dated line to a file — to each
harness in turn. For each run, record separately:

1. Did the agent do the task? (`git log` on `run/<run_id>`, read the diff)
2. Did the agent exit cleanly? (exit code, terminal event)
3. What did the daemon record? (`run_completed` / `run_failed`, `sub_reason`, `commit_landed`)
4. Did the work land anywhere a human would find it?

Expect: (1) and (3) agree.

Failure signature: **(1) is yes and (3) is no.** Both instances found had this shape and each
had a different mechanism:

- **pi** did the task, committed it, then could not end its own session. It was told to run
  `/quit` — a Claude REPL command — and having only a shell, it ran `echo "/quit" | pbcopy`
  and copied the string to the clipboard. Killed on the budget at 158s, recorded as a crash.
  Commit `b55dacb76` real and stranded on `run/<id>`.
- **codex** did the task, committed it, and exited correctly with `commit_landed=true`,
  `exit_code=0`, a clean `terminate_complete` — and was retried anyway. The retry failed on a
  bad thread id and the retry's failure replaced the success. Commit `7482bfc0a` stranded the
  same way.

Why it matters: a harness that does the work correctly and is scored as a failure is the most
expensive defect class in this system. It burns the tokens, produces the commit, and throws it
away — and the record blames the agent, so the investigation starts in the wrong place.

**Always look at the commit, not the verdict.** Both of these read as harness failures in
every summary the daemon produced.

---

## LP-012 — protocol: break the environment the gate runs in, not the code it checks

Class: protocol
Exercises: commit gate failure classification
Bead: `hk-gate-env-failure-blamed-on-agent-g4w5q`
Status: FIXED at `c4d61aa4e` + `f92b9a5df`; protocol stays open

**The question:** when the gate cannot run, who does the daemon blame?

Method: stand up a scratch clone the way the documented assessor sequence does, dispatch any
trivial bead, and watch what the commit gate does. The interesting condition is a gate that
**cannot execute**, as distinct from a gate that executes and finds a problem.

Expect: a gate that could not run is classified as an environment failure and is never routed
back to the implementer to fix.

Failure signature: `gofumpt: No such file or directory`, exit 127 — command not found. The
gate could not run and therefore found nothing wrong. The daemon classified that as a
deterministic failure and resumed the implementer to fix it. The implementer then spent about
twenty minutes of Opus time installing five build tools and running the whole merge-decision
suite, for a one-line documentation change.

Root cause worth remembering: `.tools/` is gitignored and the scratch-daemon script had no
`make tools` step, so a fresh clone never had the toolchain.

Why it matters: **a gate that cannot run reports nothing, and nothing reads as approval or as
the agent's fault depending on which way the code leans.** This one contaminated a whole
three-harness result matrix before it was found — the live-verify leg stands its scratch up
through the same scripts, so every cell in that matrix was red for this reason.

Before concluding anything from a red matrix, check that the gate could execute at all.

---

## LP-013 — protocol: check the gate can pass BEFORE you read anything into a run failure

Class: protocol
Exercises: the whole dispatch loop's dependence on repo AND box health
Bead: `hk-scenario-tier-nondeterministic-xt1wa`
Status: OPEN at `14b632046` — the gate is red for a different reason on every run

**The question:** is this run red because of the run, because the tree is red, or because the
box is starved?

Method: before dispatching anything, run the commit gate by hand on the tree you are about to
test from. The standard workflow's `commit_gate` node is `make full`, fail-closed, and costs
roughly 22 minutes per pass.

    out=$(make full 2>&1); rc=$?; echo "rc=$rc"

Expect: exit 0 before you dispatch anything.

**Run it more than once.** Measured 2026-08-10, three passes on the same box within two hours,
two of them on the identical commit:

| Pass | Tree | Free disk | Failed |
|---|---|---|---|
| 1 | `9089bda08` | 9.8 GiB | `internal/keeper` — one heartbeat test |
| 2 | `14b632046` | 9.8 GiB | `internal/daemon` — three T6 tests; `internal/workers` — one poll test |
| 3 | `14b632046` | 12 GiB | **no test failures at all — 108/108 packages** |

**No test failure repeated, and none survived pass 3.** Every failing test passed 3/3 when
re-run by hand, and the whole `internal/keeper` package passed 3/3 on its own. Nothing was
wrong with any of that code. Passes 1 and 2 were below the daemon's 10 GiB dispatch floor and
pass 3 was above it — see LP-014, which is most of this story.

A single green pass here does not mean the gate is green, and a single red pass names nothing.
**Read the disk before you read the failures.**

Pass 3 then failed at the step AFTER the tests — `lint-allow`, on six file/linter pairs in newly
added code (`hk-pw1wv`). That failure is deterministic and it is the honest state of the branch:
the tests are clean and the lint gate is not.

**Correction, 2026-08-11 at `84ad44e20`: the sentence that used to end this paragraph was wrong.**
It said `make test-scenario` "sits behind `lint-allow`" and so could not be reached. It does not.
`test-scenario` is its own `.PHONY` target and its only prerequisite is `build-all`. Only `make
full` orders `lint-allow` ahead of it, so a red lint aborts `full` before the tier — but nothing
stops you running the tier directly. **The tier was never blocked. It was unreached by the one
command anybody ran**, and it cost this bead four days of waiting on a lint fix it never needed.

The general lesson is the one already in this case, applied to a target instead of a gate: **read
the `Makefile` before you write down what depends on what.** A step failing before another step
in one recipe is not a dependency.

Two earlier blockers recorded in this case are now closed and are kept only so the next reader
does not re-derive them:

1. The reachability gate exiting 2 on retired `internal/keeper` names — fixed in `2c7bd3258`.
2. The `evaltasks/eval-bugfix-rate-limiter` fixture, which holds a bug on purpose — **this one
   was never real.** `make full` runs its tests with `-short` and the fixture skips itself under
   `testing.Short()`. The original measurement used a bare `go test ./...`, which is not the
   gate. Refuted on `hk-4sp1z` by direct measurement. The general lesson is worth more than the
   case: **measure the gate by running the gate**, not by running something that resembles it.

Why it matters: with the gate red, every dispatched run fails at the gate for a reason that
has nothing to do with the agent's work, gets routed back to the implementer, and burns ~22
minutes plus Opus tokens per pass. **Any conclusion drawn about the daemon from such a run is
worthless.**

---

## LP-014 — the daemon stops dispatching below 10 GiB free and says so only in its own log

Class: protocol
Exercises: the disk watermark guard vs. every test that waits for a bead to close
Bead: `hk-scenario-tier-nondeterministic-xt1wa`
Status: OPEN at `14b632046` — the guard works as designed; what is missing is that nothing
tells the reader of a failing test why it failed

Preconditions: a box below `diskLowWatermarkDefault` (10 GiB, `internal/daemon/workloop.go`).
Reaching that state needs no effort at all — see below.

**The question:** this test timed out waiting for a bead to close. Was the bead never
dispatched, or did it fail?

Steps:

    df -h /System/Volumes/Data          # BEFORE you read anything into a timeout
    go test -run TestT6_1MBBeadBody ./internal/daemon/

Expect: on a box above the watermark, the bead closes in a few seconds.

Failure signature. The test fails on its own 60-second deadline with a message about the work
not completing, and the reason sits several lines above it in the daemon's own output where
nothing draws attention to it:

    daemon: disk-check: available=10097MiB watermark=10240MiB path=/... — dispatch paused
    t6_scale_shape_test.go:464: T6-2: all_closed=false elapsed=60.06s
    t6_scale_shape_test.go:466: T6-2 FAIL: 1MB bead not closed within 60s

Measured 2026-08-10 at `14b632046`. Three `internal/daemon` tests failed this way inside
`make full`, each burning its full 61-second deadline. Free space was 9.8 GiB. Deleting two
Go build caches belonging to checkouts that no longer existed returned 1.9 GiB, and the same
three tests then passed **together in 23 seconds**. The code was never involved.

Note the third line of the signature: `available` was still DROPPING across the three tests —
10097, then 9903, then 9796 MiB — because the test run itself was consuming the disk it needed.

Why it matters: the guard is correct and the pause is the right behaviour, but a paused
dispatch is indistinguishable from a broken daemon at the place anyone actually looks. It
manufactures fake failures and fake hangs fleet-wide, and the event log makes them look like
real regressions — `docs/disk-reclaim.md` says exactly this in its opening and it was still
missed here for most of a day. Any test that waits for a bead to close is testing the disk.

**How the box gets there, which is the part worth internalising.** Nothing leaks. The Go caches
just grow with the work: `~/Library/Caches/go-build` at 20 GiB and
`~/Library/Caches/harmonik-lane-gocache` at 19 GiB on 2026-08-10 — 39 GiB of regenerable cache
against 9.8 GiB free. The shared one had doubled in 13 days. A box running two or three lanes
crosses the floor in about a week from a clean start, so this is a recurring condition, not an
incident.

**What to do about it, in order.** `docs/disk-reclaim.md` §0 owns this and its order is
load-bearing: per-checkout caches first, shared caches LAST and only when nothing is compiling.
Clearing the shared `go-build` mid-build has produced builds that reported success without
rebuilding — a wrong answer that looks like a right one, on a box where several agents are
deciding whether to merge. Deleting `~/Library/Caches/harmonik-lane-gocache/<dir>` for a
checkout that no longer exists is always safe and costs nothing that still exists a rebuild.

---

## LP-018 — protocol: is this suite red because the code is broken, or because the box is busy?

Class: protocol
Exercises: any parallel test tier with wall-clock deadlines — `make full`, `make core`,
`make test-scenario`
Bead: `hk-scenario-tier-nondeterministic-xt1wa`, `hk-core-gate-nondeterministic-m9vlf`
Status: OPEN at `84ad44e20` — the tier is red for both reasons at once, and they separate cleanly

**The question:** a tier named N failing tests. How many of those are defects?

`LP-013` asks whether the gate can pass at all and `LP-014` asks whether the box has disk. This
case is the step after both come back clean and the suite is *still* red. It exists because the
honest answer on 2026-08-11 was "9 failures, 1 defect", and nothing in the log distinguishes them.

Preconditions: above the disk floor (`LP-014`), or you are measuring the wrong thing.

### Steps

**1. Read the exit code from inside the log. Never from the runner.**

    (make test-scenario 2>&1; echo "EXIT_LINE rc=$?") > run1.log 2>&1
    grep EXIT_LINE run1.log

A pipeline returns its LAST command's status, so `make ... | tee ... | tail` reports `tail`'s zero
whatever the tier did. Measured: the harness reported run 1 as "completed (exit code 0)" while the
log's own line was `EXIT_LINE rc=2`. `hk-core-gate-nondeterministic-m9vlf` records a session that
made this exact mistake and recorded a red gate as green.

**2. Confirm the disk guard did not fire — and filter the fixtures, or you will get a false yes.**

    grep -c "dispatch paused" run1.log                                        # 306
    grep -o "disk-check:[^\"]*" run1.log | grep -viE "TestDiskLow|TestAdmissionOrder" | wc -l   # 0

The tier contains tests that inject synthetic disk readings on purpose — `available=0MiB`,
`available=4398046511104MiB`. **An unfiltered grep says the daemon paused 306 times on a box with
23 GiB free.** Only the second number means anything.

**3. Run the tier TWICE and intersect the failure sets.** One run names nothing; that is the whole
content of both beads above.

    run 1 → 9 failures
    run 2 → 6 failures
    intersection → 3

**Re-check free disk BETWEEN the runs, not once at the start.** This step used to say "above the
disk floor" as a precondition and leave it there, which is wrong, because **the runs themselves are
what eat the disk.** Measured 2026-08-11 in one assessor session: the box went from 23144 MiB free
to 9868 MiB across two scenario tiers under `-race`, two `make fast` runs, two lint runs and one
`make full` — about 13 GiB in four hours. The final `make full` then failed three `TestT6_*` tests
on a real `dispatch paused`, below the watermark, and those three were nearly filed as defects.

So later runs in a session are systematically more starved than earlier ones, and **a naive
intersection reads that as "the tests got flakier as I went"**. `LP-014` frames the floor as a
condition a busy box drifts into over about a week; a session doing repeat gate runs gets there in
an afternoon, and it is the measurer who put it there.

**Do not try to explain the difference in counts by box load, and do not sample load the lazy way.**
The first version of this case did both and was wrong. It read the opening samples of run 2, called
it "the quieter box", and presented 9-vs-6 as a load correlation. Sampling every 20s for the whole
run showed run 2 ranged from **3.19 to 34.24**, peaking well above anything measured during run 1 —
because **the tier generates its own load**, so a "quiet box" does not survive first contact with
it. The count difference is unexplained and does not need explaining: the intersection is what the
next step consumes.

**4. Run each survivor of the intersection ALONE, and time it.**

    go test -race -tags=scenario -count=1 -run '^<TestName>$' ./internal/daemon/

Expect: a defect fails alone. A starved test passes alone, **and the margin is the evidence** —

    TestWorkLoop_HC056Timeout_ReopenAndRepickup         3.878s alone, vs a 20s deadline it blew
    TestWorkLoop_ClaimSemaphore_BoundsClaimConcurrency 13.695s alone, vs 60s

A 5x-plus headroom that vanishes under parallelism is starvation, not a race you got lucky on.

**5. For anything still failing alone, read the failure PAYLOAD, not the test's message.** See the
failure signature below — this is where the one real defect was, and where two hours nearly went
to a bug that does not exist.

### Failure signature

Starvation looks like this, and all nine failures in run 1 had this shape: an elapsed time sitting
at or just above the test's own `WithTimeout` value, and a message that says *timed out waiting
for* something. **Not one was a failed logical assertion.** Deadlines in this tier are 3s, 15s,
20s, 30s and 60s — grep `WithTimeout` in the failing file and compare it to the elapsed time.

**The isolation margin in step 4 is the evidence, not the box load.** A test that needs 3.878s and
is given 20s is not failing because of a race you got lucky on; it is failing because it never got
scheduled. That reasoning holds regardless of what `uptime` said, which is the point — load is
hard to attribute on a shared box and the margin is not.

Two shapes that look worse than they are:

- `CloseBead call count = 0; want ≥ 1` reads like a logic failure. It is what the assertion prints
  *after* its wait context expired. Check for a `timed out` line above it.
- `the traversal cap did NOT bound the implement↔commit_gate loop (infinite-loop regression)`
  (`TestScenario_CommitGateCapTerminates_hki8g59`) reads like a live regression. Its budget is a
  60s clock its own comment calls "the safety net", its trace showed two completed passes against
  a cap of 3, and it passed in run 2. Slow box, working cap.

One shape that is worse than it looks — **the test's stated cause was not the real one**:

    codex_adapter_lifecycle_hkvfmn9_test.go:546:
      run_failed present — the codex shim likely rejected a leaked --model

The real payload said something else entirely:

    "summary":"dot: traversal cap hit at node \"commit_gate\" (traversal cap reached)"

No `--model` leak occurred. The run walked a `commit_gate` node that runs `make full` inside a
three-file temp dir that is not a Go module. Cause: the test sets a daemon-level
`WorkflowModeSingle` default that `resolveWorkflow` deliberately refuses ("a stale daemon default
may still name single ... it cannot select no_review"), so the run fell through to dot mode. **The
daemon is right and the test is stale** — `hk-5ji8t`. Always read the event payload before you
believe the assertion's guess at why it fired.

And the same test prints `PASS beadID=... gotTerminal=true` from an unconditional `t.Logf` after
`t.Errorf` has already failed it. A reader tailing the log sees PASS on a red test.

### Why it matters

A tier that names a different set every run gets read as "flaky, ignore it", and the one real
defect inside it ships. It also runs the other way: nine starved tests get filed as nine bugs, and
a week goes into code that was never broken. Both have happened here. The intersect-then-isolate
step costs two runs and separates them.

**And the environment cause is not exotic — it is the normal state of this box.** These lanes run
two or three agents at once, so the contended run IS the condition under test. A suite of
wall-clock deadlines cannot be trusted on it, which is the finding, not a caveat about the
measurement.
