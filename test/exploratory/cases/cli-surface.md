# Cases — CLI surface under bad input

Format and rules: [`README.md`](README.md). `$SCRATCH` is the scratch project directory.

Swept 2026-08-09 against a daemon at `89dc52d5`; statuses re-verified 2026-08-10 against
`daf396b41` by reading the code at that head.

---

## LP-001 — the emergency stop reports success on a queue name that does not exist

Class: probe
Exercises: `queue pause`, operator emergency stop
Bead: `hk-queue-pause-succeeds-on-unknown-queue-nr18c`
Status: FIXED at `953627f59` (verified 2026-08-10)

Preconditions: live daemon, a queue named `main`, one bead dispatched and running.

Steps:

    out=$(harmonik queue pause --queue mian --project "$SCRATCH" 2>&1); rc=$?
    echo "rc=$rc"; echo "$out"
    harmonik queue list --project "$SCRATCH"

Expect: non-zero exit, a message naming the unknown queue, and `main` still dispatching
because nothing was paused.

Failure signature: prints `paused: mian`, exits 0, `main` keeps dispatching.

Why it matters: pause is the emergency stop. A typo that reports success means the operator
believes the fleet is stopped while it is still spending tokens. `queue recover` refused the
same bad name correctly, so the daemon always had the information to refuse — which is the
tell that this was an omission and not a design limit.

---

## LP-002 — a leading flag turns off the mistyped-command guard and starts a daemon

Class: probe
Exercises: top-level argument parsing, `unknownSubcommand`
Bead: `hk-cli-flag-first-starts-daemon-gjhiy` (operator-owned; carries an operator direction)
Status: OPEN at `daf396b41` — deliberately, see the bead

Preconditions: a directory that has NEVER been through `harmonik init`.

Steps:

    mkdir -p /tmp/h/never-inited
    out=$(harmonik --project /tmp/h/never-inited queue list 2>&1); rc=$?
    echo "rc=$rc"; echo "$out"
    ls -a /tmp/h/never-inited

Expect: refusal. Exactly one command starts a daemon, and this is not it.

Failure signature: a daemon starts and a full `.harmonik/` tree appears in a directory that
was never initialised.

Why it matters: the guard exists and works — `harmonik status` (bare) is refused with exit 2.
The flag-first spelling bypasses it because the guard returns early on any argument beginning
with `-`. **A reader who trusts `docs/daemon-redeploy.md` gets this exactly backwards**: that
doc warns against the bare spelling, which is now safe, and says nothing about the flag-first
spelling, which is not.

---

## LP-003 — `queue status` reports a lull while a bead is dispatched

Class: probe
Exercises: `queue status`, the captain shutdown gate
Bead: `hk-queue-status-blind-shutdown-gate-9dco0`
Status: FIXED at `e750b138f` (verified 2026-08-10)

Preconditions: live daemon with a bead actively dispatched.

Steps:

    harmonik queue status --project "$SCRATCH"
    harmonik queue list --project "$SCRATCH"     # cross-check: shows status=active workers=1

Expect: `queue status` reports the active queue and the running work.

Failure signature: `(no queue active)` while `queue list` shows `status=active workers=1`.

Why it matters: this is not only a wrong display. `SHUTDOWN.md` reads an `active_runs` field
off this payload, and that field does not exist here — it exists on the subscribe heartbeat.
**The gate reads the right field from the wrong command, so it can never fail**, and the
shutdown check passes over a live run.

---

## LP-004 — a mistyped event filter is accepted and then delivers nothing forever

Class: probe
Exercises: `subscribe --types`
Bead: `hk-subscribe-accepts-unknown-type-rd07b`
Status: FIXED at `1cc8f5d7e` (verified 2026-08-10)

Steps:

    harmonik subscribe --types run_complete --project "$SCRATCH"   # note: no trailing 'd'

Expect: refusal naming the unknown type, ideally with the near miss suggested.

Failure signature: the stream opens, heartbeats arrive on cadence, and no matching event is
ever delivered. Indistinguishable from a quiet system.

Why it matters: `--types` is in the canonical monitor pattern that every orchestrator is told
to use, so a single typo produces a monitor that looks healthy and watches nothing. This
compounds LP-003 and LP-010 — all three make a broken run look fine.

---

## LP-005 — `set-concurrency` accepts any value and applies it to a live daemon

Class: probe
Exercises: `queue set-concurrency`
Bead: `hk-set-concurrency-unbounded-ad79i`
Status: OPEN at `daf396b41`

Steps:

    out=$(harmonik queue set-concurrency 999999 --project "$SCRATCH" 2>&1); rc=$?
    echo "rc=$rc"; echo "$out"

Expect: refusal above a sane ceiling, naming the ceiling.

Failure signature: applied to the live daemon and reported back as safe. A lower bound is
enforced (`n >= 1`); there is no upper bound.

Why it matters: once in production this is a fleet outage by typo, with no confirmation step
between the keystroke and the effect.

---

## LP-006 — `promote --dry-run` prints the same plan for things that do not exist

Class: probe
Exercises: `promote --dry-run`
Bead: `hk-promote-dryrun-validates-nothing-975nt`
Status: OPEN at `daf396b41`

Steps:

    harmonik promote --dry-run --sha deadbeefdeadbeefdeadbeefdeadbeefdeadbeef --project "$SCRATCH"
    harmonik promote --dry-run --target refs/heads/branch-that-does-not-exist --project "$SCRATCH"

Expect: each refuses, naming which input could not be resolved.

Failure signature: an identical plan printed for both, exit 0.

Why it matters: `promote` is how work reaches the target branch, so this is the release path's
own safety check — and it cannot fail. A dry run that always succeeds trains the operator to
skip it.

---

## LP-007 — two commands tell the operator to run a command that does not exist

Class: probe
Exercises: `confirm-verdict`, `veto-verdict` help and error text
Bead: `hk-phantom-status-command-nlbtz`
Status: FIXED at `2c5b5b03b` (verified 2026-08-10)

Steps:

    harmonik confirm-verdict --help
    harmonik veto-verdict --help
    harmonik status                       # the thing they told you to run

Failure signature: both point the operator at `harmonik status`, which does not exist.

Why it matters: the phantom command compounds LP-002 — the flag-first spelling of the
non-existent command starts a daemon. Following the help text is what triggers the trap.

**Related, found while re-checking this one:** `hk-verdict-override-unwired-aqjxo` — no run
can ever be parked awaiting a verdict, because the executor never calls `Await`. Both verdict
commands can therefore only ever exit 16. Worth a case here once that is decided.

---

## LP-008 — `usage` counts other projects and reports zero for the runs that happened

Class: probe
Exercises: `harmonik usage --project`
Bead: `hk-usage-misattributes-cost-yymyu`
Status: OPEN at `daf396b41`

Steps:

    harmonik usage --project "$SCRATCH"

Expect: only this project's sessions, with real costs for runs that actually happened.

Failure signature: counts other projects' sessions — including sessions that ended before this
project existed — and reports `$0` for the runs that did happen. `Productive 0.0%` is an
artifact of both errors, not a measurement.

Why it matters: the staffing rule turns on this number. A measurement that is wrong in both
directions at once cannot be corrected by anyone reading it.

---

## LP-009 — held up: the workflow-graph validator

Class: probe
Exercises: `harmonik graph validate`
Bead: none — held up
Status: HELD UP at `89dc52d5`

Steps: feed the validator malformed input of increasing severity, ending with binary noise.

    harmonik graph validate /dev/null
    head -c 200000 /dev/urandom > /tmp/noise.dot && harmonik graph validate /tmp/noise.dot

Expect and observed: every malformed input rejected loudly and correctly, including 200 KB of
`/dev/urandom`. Config parsing named the file, the line, and the remedy on every malformed
shape.

Why it matters here: recorded so the next session does not spend a day re-proving it. Also
`handler status`, `crew list`, `release ledger`, `confirm-verdict` and `veto-verdict` all
refuse bad input with clear messages and correct exit codes, and `subscribe --since-event-id`
and `--heartbeat` validate properly.
