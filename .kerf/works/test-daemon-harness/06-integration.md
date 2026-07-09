# Integration — test-daemon-harness

## How the remaining pieces compose with what's landed
The landed loop is `init -> build -> up -> batch -> feedback -> down` (+ `cycle`).
The remaining work wraps that loop in a durability shell without changing its
contracts:

- **C4 (verification)** consumes the SAME code path the harness ships (`batch
  --from-events` offline fold, `feedback` against a throwaway fleet). It adds no new
  runtime surface — only a golden corpus + a CI trigger + a recorded live run. It
  cannot regress the loop because it only reads it.
- **C5.1 (make target)** is a pure discoverability wrapper over
  `scratch-daemon-smoke.sh`. Zero coupling.
- **C5.2 (`gc`)** is a new subcommand that shares the existing guard functions
  (`guard_path`, `assert_not_supervised`) and the fleet-safety invariant. It touches
  only `<scratch>`; it composes with `down` (must be down before `gc --hard`).
- **C5.3 (runbook)** documents the above; the runbook already declares the script the
  source of truth, so the doc trails the code.

## Cross-cutting invariants preserved
1. **Never touch the fleet daemon** — `gc` extends the four guards; the smoke's fleet
   writes go only to `SCRATCH_FEEDBACK_FLEET_ROOT` throwaway repos.
2. **Daemon owns terminal transitions** — unchanged; `feedback` still creates OPEN,
   never `--assignee`/`in_progress`.
3. **Dedup key stability** — the golden corpus is precisely the regression guard for
   this invariant.
4. **Hermetic-by-default** — the new smoke phase and the CI gate add no secrets/network.

## Interaction with the parallel scripted-twin lane
This lane and the scripted-twin (digital-twin) lane are the two halves of Phase-2
quality testing and are independent by construction: the twin mocks agents against
the real daemon in-process; this harness stands up a real second daemon out-of-process
on a scratch clone. They share no files. Both feed issues back as beads. No integration
work is required between them for this plan.

## Sequencing
C4.1/C4.2, C5.1, C5.2 are mutually independent and dispatch in parallel. C4.3
(recorded live run) needs a real claude binary and can run any time. C5.3 (runbook)
lands last, after gc + the gate exist, to document them accurately.
