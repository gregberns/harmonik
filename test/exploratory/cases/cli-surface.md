# Cases — CLI surface under bad input

Format and rules: [`README.md`](README.md). `$SCRATCH` is the scratch project directory.

Swept 2026-08-09 against a daemon at `89dc52d5`; statuses re-verified 2026-08-10 against
`daf396b41` by reading the code at that head.

---

## LP-001 — the emergency stop reports success on a queue name that does not exist

Class: probe
Exercises: `queue pause`, operator emergency stop
Bead: `hk-queue-pause-succeeds-on-unknown-queue-nr18c`
Status: FIXED at `953627f59` (verified 2026-08-10; **re-verified live at `aedbd770`** — `pause`,
`resume` and `recover` all now exit 2 on an unknown queue name and say what did not happen)

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
Status: **FIXED at `5dd157cb9` (verified 2026-08-11)** — was OPEN at `daf396b41`

Preconditions: a directory that has NEVER been through `harmonik init`.

Steps — **four spellings, not one.** The original case recorded only the first. Alpha's fix
(`2b17e7111`) named two more, and a fourth falls out of the same defect:

    mkdir -p /tmp/h/never-inited
    out=$(harmonik --project /tmp/h/never-inited queue list 2>&1); rc=$?   # flag-first + real verb
    out=$(harmonik --project /tmp/h/never-inited 2>&1); rc=$?              # flags, no verb
    out=$(harmonik --project /tmp/h/never-inited status 2>&1); rc=$?       # no such subcommand
    (cd /tmp/h/never-inited && harmonik); rc=$?                            # no arguments at all
    ls -a /tmp/h/never-inited

Expect: refusal on all four. Exactly one command starts a daemon and none of these is it.

Failure signature: a daemon starts and a full `.harmonik/` tree appears in a directory that
was never initialised.

**Verified fixed 2026-08-11 at `5dd157cb9`**, on a binary built from a clean tree. All four
now exit 2 with an explicit refusal and leave the directory empty:

    harmonik: no subcommand given, only flags — this does not start a daemon
      "status" was ignored: a subcommand must come first, before any flags
      To start a daemon, name it: `harmonik start daemon [--project DIR] [flags]`

The message names the ignored word and points at the one spelling that does start a daemon,
which is what makes the refusal useful rather than merely correct.

Why it mattered: the guard existed and worked for `harmonik status` (bare, exit 2), but the
flag-first spelling bypassed it because the guard returned early on any argument beginning
with `-`. The root cause was more general than the guard — starting a daemon was simply what
`run()` did when nothing else claimed the arguments, so every argv that ran out of verbs fell
through to it. **The third spelling cost real time**: an operator poll-checking
`harmonik --project X status` during a redeploy started a SECOND daemon that contended with
the one being revived, and probably killed an early revive attempt on 2026-06-30.

**Timing trap when re-running this.** If the defect ever returns, three of these four spellings
BLOCK — a daemon starts and does not exit — so a bare `out=$(...)` hangs the session rather than
failing. Guard each one, and note that macOS has no `timeout` (`rc=127`, and a 127 read as a
refusal is a false PASS — this happened while verifying the fix). Background the command, poll
`kill -0`, and kill it after ~15s.

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
Status: OPEN at `daf396b41`; **still OPEN, re-verified live at `aedbd770`** — `set-concurrency 99999`
exits 0 and prints `max_concurrent: 1 → 99999`. The lower bound IS enforced (`0` exits 2 with "n
must be an integer >= 1"), so the guard exists and only the upper end is missing.

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
Bead: `hk-promote-dryrun-validates-nothing-975nt`; repro defect `hk-q21jt`
Status: OPEN at `938b3c4cb` — **bug re-confirmed live; the STEPS below were wrong until 2026-08-11**

Steps — **both lines below were previously recorded with flags that do not exist.** SHAs are
POSITIONAL (`harmonik promote <sha>...`); there is no `--sha`. Run these against any repo with at
least one commit. `--dry-run` mutates nothing.

    SHA=$(git rev-parse HEAD)
    # a commit that does not exist, real target
    out=$(harmonik promote --dry-run deadbeefdeadbeefdeadbeefdeadbeefdeadbeef \
          --target main --project "$SCRATCH" 2>&1); echo "rc=$?"; echo "$out"
    # real commit, a target branch that does not exist
    out=$(harmonik promote --dry-run "$SHA" \
          --target refs/heads/branch-that-does-not-exist --project "$SCRATCH" 2>&1); echo "rc=$?"

Expect: each refuses, naming which input could not be resolved.

Failure signature: **both exit 0** and print a full plan, including the push line:

    harmonik promote (dry-run): would cherry-pick 938b3c4cb... onto
      "refs/heads/branch-that-does-not-exist" in a temp worktree
    harmonik promote (dry-run): would push: git push origin
      HEAD:refs/heads/branch-that-does-not-exist (with up to 3 non-ff retries)

**What the old steps did instead, and why this case is the library's own cautionary tale.** The
recorded repro was written from memory of the command surface rather than from a run, and it was
broken in two different ways, both of which read as a PASS:

- `--sha deadbeef...` exited 1 with `unknown flag "--sha"` — dead on flag parsing, never reaching
  the code under test.
- `--target refs/heads/nope` with no SHA exited 1 with `push-mode requires at least one SHA
  argument` — a refusal, but for the missing SHA, not the bad target.

The case expects a refusal, so **both non-zero exits read as correct behaviour and the case
reported this live bug as FIXED.** Found by alpha, filed as `hk-q21jt`, verified here. The bug was
never fixed; only the test for it was broken.

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

---

## LP-015 — a message to a recipient that does not exist is accepted and never delivered

Class: probe
Exercises: `comms send`, `comms who`, the presence registry
Bead: `hk-rtqmu`
Status: OPEN at `aedbd770` (found 2026-08-10)

Preconditions: a live scratch daemon. No agent named `nosuchlane` anywhere.

Steps:

    out=$(harmonik comms send --project "$S" --to nosuchlane --from alpha \
          --no-wake --topic status "epic complete" 2>&1); rc=$?
    echo "rc=$rc"; echo "$out"
    harmonik comms log --project "$S" | tail -2

Expect: a directed send to a name the daemon has never seen either fails, or returns a
delivery status the sender can act on.

Failure signature: `rc=0`, and the whole output is an event id:

    019fef4e-9fbd-7ed0-9184-646377e53d07

Drop `--no-wake` and one accidental signal appears on stderr — `can't find pane:
harmonik-<hash>-nosuchlane` — but `comms send --help` states that wake failures "do not affect
the exit code", so it is documented as not-a-signal, and `--no-wake` removes it. **Both spellings
exit 0.**

The message is not lost. It sits in `comms log` looking exactly like a delivered one:

    2026-08-11T05:32:15Z  alpha → nosuchlane  [status]  epic complete

**`comms who` cannot be used as the pre-flight check**, and this is the part worth remembering:

    harmonik comms recv --project "$S" --agent phantom-never-existed >/dev/null
    harmonik comms who --project "$S" | grep phantom
    # phantom-never-existed    last_seen 2026-08-11T05:32:15Z

`recv` emits a presence beat, so **typing a name once puts it in the registry**. A `--to` target
never appears there at all. So `who` answers "which names did a process recently use", not "which
agents exist", and the one name you wanted to validate is the one it will never show you. Entries
do expire (120s online, 10m stale cutoff, `internal/presence`), so ghosts do not accumulate — but
inside that window a name typed once reads as a live agent.

Why it matters: this is the channel the captain uses to mail epics to crews and crews use to report
completion. A misaddressed epic is a silent black hole — the captain sees success, the crew never
hears, and the only symptom is an epic that never completes. **That is indistinguishable from a
stalled crew, which is the thing everyone is already hunting.**

---

## LP-016 — `wake` says it nudged a session for any string you give it

Class: probe
Exercises: `harmonik wake`, the fleet-stall escape hatch
Bead: `hk-o3mz8`
Status: OPEN at `aedbd770` (found 2026-08-10)

Preconditions: a live scratch daemon.

Steps:

    for n in nosuchagent ../../etc "a b c" alpha; do
      out=$(harmonik wake --project "$S" --agent "$n" 2>&1); rc=$?
      printf '%-14s rc=%s | %s\n' "$n" "$rc" "$out"
    done

Expect: a name matching no session is refused, or the output states how many sessions matched.

Failure signature: every one of them exits 0 and claims success.

    nosuchagent    rc=0 | wake: nosuchagent nudged
    ../../etc      rc=0 | wake: ../../etc nudged
    a b c          rc=0 | wake: a b c nudged
    alpha          rc=0 | wake: alpha nudged

The last is a real session. Nothing in the output separates it from the other three. Only the
empty string is refused (rc=1).

The counter-argument, and why it does not hold: `wake --help` says "Sessions that are not currently
sleeping are silently skipped", which is reasonable for a REAL session that is already awake. It
does not cover a name matching no session at all, and "nudged" is an affirmative claim either way.
The documented meaning of exit 0 is "sessions nudged".

Why it matters: `wake` is the fleet-stall human escape hatch — its own help says so. It gets used
when the fleet is already wedged and the operator is deciding whether the wake path is broken or
the session is. "Nudged" when zero sessions matched sends that operator off to debug a session they
never woke. A count settles it: `0 sessions matched` versus `1 nudged`.

Smaller, same family: `queue cancel --queue <does-not-exist>` exits 0 with "no active queue found
(queue file absent)" and never echoes the name it was given.

**The fix shape already exists in this codebase** — `queue pause/resume/recover` had this exact
defect (LP-001) and now refuse:

    rc=2  daemon: operator-pause: no queue named "ghostqueue":
          `harmonik queue pause` changed nothing and no queue was paused

---

## LP-017 — held up: every daemon-requiring verb refuses correctly when the daemon is down

Class: probe
Exercises: the `17 = daemon not running` contract across the whole CLI
Bead: none — held up
Status: HELD UP at `aedbd770` (swept 2026-08-10)

Preconditions: a scratch daemon that has been brought DOWN. The point is the socket's absence.

Steps:

    bash scripts/scratch-daemon.sh down "$S"
    for v in "queue status" "queue list" "queue pause --queue main" \
             "queue set-concurrency 4" "comms send --to alpha --from bravo --no-wake hi" \
             "comms recv --agent alpha" "wake --all" "sleep"; do
      out=$(harmonik $v --project "$S" 2>&1); echo "$v -> rc=$?"
    done

Expect: exit 17 and a message naming the missing socket. Anything that hangs, or exits 0, or
starts a daemon of its own, is the finding.

Result: **all eight exit 17**, each naming the socket path it looked for:

    harmonik queue status: daemon not running (no socket at <project>/.harmonik/daemon.sock)

`comms who` correctly exits 0 — its help lists it as one of the two verbs needing no daemon — and
it degrades honestly rather than pretending, marking the registry entries `stale (last seen 3m
ago)` instead of reporting them online.

Why it matters: this is the failure every operator and every agent hits constantly, and it is the
one place a CLI is most tempted to be helpful by starting a daemon for you. Nothing here does. Note
the contrast that makes this worth recording: **the flag-first hole (LP-002) reaches daemon-start
through this same binary**, so "the daemon-down path is safe" is true of the verb-first spelling and
NOT of `harmonik --project DIR queue list`. Sweeping this surface is how you tell those apart.

Do not re-sweep this surface looking for silent-success defects. The two found in this pass
(LP-015, LP-016) are on the *live-daemon* path, not this one.
