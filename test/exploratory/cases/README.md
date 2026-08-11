# Live adversarial case library

Test cases the assessor runs against a **live daemon** to find what breaks when reality does
not match the happy path. One file per topic. Each case is written so that somebody who was
not there can run it and get the same answer.

Started 2026-08-10 from lane bravo, out of 17 findings whose only record was prose inside
beads. A finding that cannot be re-run is a story, not a test.

## What this is NOT — read this before adding a case here

Three test surfaces already exist and they are mature. Put the case where it belongs.

| Surface | Question it answers | Agents | Put a case there when |
|---|---|---|---|
| `scenarios/core-loop-proof/` (`make core-loop-lt`) | Does the happy path work end to end across harness x model? | real | the case is CONFORMANCE — the loop should work and you are proving it does |
| `scenarios/smoke/`, `scenarios/regression/` | Does the daemon emit the specified events for a scripted agent? | twins | the case is DETERMINISTIC and assertable on an event stream |
| Go tests under `internal/` | Does this unit behave? | none | the case does not need a daemon at all |

**This library is for the fourth question: what happens when the input is wrong, the
environment is broken, or a component goes away mid-run?** Live process, real daemon,
adversarial input. Nothing else covers it.

If a case here becomes stable and deterministic, promote it into `scenarios/regression/` and
leave a pointer behind. This library is a net, not a permanent home.

## Two classes of case, and the second one is the valuable one

**`probe`** — a command, an expected refusal, an exit code. Most cases are these. They are
cheap, they are scriptable, and they are how the CLI surface gets swept. Every one of them can
eventually run unattended.

**`protocol`** — an observation method, not an assertion. You drive a real run and watch
something with a stated question in hand. These cannot be reduced to an exit code, and they
are where the expensive bugs come from: the 79-minute silent stall
(`hk-stop-hook-failure-wedges-run-dc5z6`) was found by watching a live run and asking every
health surface what it thought was happening, then comparing the answers to reality. No
assertion suite would have found it, because every surface was reporting success.

**A library of only `probe` cases decays into a regression suite that finds nothing new.**
The `protocol` cases are what keep finding things. When you add cases, add both kinds.

## Case format

    ## LP-nnn — one line, what breaks, in plain words

    Class:       probe | protocol
    Exercises:   the component or contract under test
    Bead:        hk-... (or "none — held up")
    Status:      OPEN at <sha> | FIXED at <sha> (verified <date>) | HELD UP at <sha>

    Preconditions: what must be true before you start.

    Steps: exact commands, copy-pasteable.

    Expect: what a correct system does.

    Failure signature: what the defect looks like, precisely enough to recognise it
    in someone else's log.

    Why it matters: what shipping this costs. One or two sentences. If you cannot
    write this line, the case is not worth a slot.

`Status` carries a commit, always. A case with a bare "FIXED" is not re-checkable, and
findings in this project have repeatedly been re-fixed or re-reported because a status was
read without a commit attached to it.

**Record the cases that HELD UP too.** A swept surface that refused everything is worth
knowing about — it stops the next session spending a day there. Those carry
`Bead: none — held up`.

## Running these

The daemon must be an isolated scratch, never the fleet:

    bash scripts/scratch-daemon.sh init /tmp/h/<name> --rev <sha>
    bash scripts/scratch-daemon.sh build /tmp/h/<name>
    bash scripts/scratch-daemon.sh up /tmp/h/<name>
    # ... run cases ...
    bash scripts/scratch-daemon.sh down /tmp/h/<name>
    rm -rf /tmp/h/<name>

**Build BEFORE you seed**, or the binary stamps `vcs.modified=true` and every provenance
check on it is void.

Traps that have produced false results in this library's own history:

- **Read `$?` directly, never after a pipe.** `cmd | head; echo $?` reports `head`'s status.
  Use `out=$(cmd 2>&1); rc=$?`.
- **Trust git over the event stream** when the question is whether work landed.
- **Background runs lie about exit codes.** Read the logged exit line.
- **A red gate may be another lane's**, and the commit gate is `make full` — if the tree
  cannot pass it, every dispatched run goes red for a reason that has nothing to do with the
  case you are testing. Check the gate is green before you read anything into a run failure.

## Index

- [`cli-surface.md`](cli-surface.md) — bad input to the operator-facing commands.
- [`run-lifecycle.md`](run-lifecycle.md) — what a live run does when a component fails or
  goes away, including the observation protocols.
