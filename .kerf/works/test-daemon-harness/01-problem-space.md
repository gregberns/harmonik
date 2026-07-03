# Problem Space — test-daemon-harness

## Summary
Make the scratch-clone standalone test-daemon a **durable, reusable, on-demand capability**, not a one-off for the remote last-mile. The seed already landed: `scripts/scratch-daemon.sh` (init/build/up/status/down/cycle) stands up a fully-isolated second daemon — separate git clone, own socket + per-project-hash tmux namespace, no supervisor/auto-revive — that can be pkill+rebuilt in seconds without touching the fleet daemon. This work closes the loop: **spin up an isolated test daemon → run a BATCH of tests/beads against it → feed the surfaced issues back to the MAIN daemon as beads**, documented and invokable on demand for ANY validation batch (not just remote-worker bugs).

## Goals
- A re-runnable batch loop: submit N test beads/scenarios to the scratch daemon and collect a structured pass/fail result set, on a clone fully decoupled from the fleet.
- Issue feedback: turn scratch-run failures into beads on the MAIN repo (labelled + provenance-stamped) so findings flow back to the fleet's normal dispatch.
- Promote it to a documented STANDING capability: when/how to use it for any test batch, one-command on-demand invocation.
- Broad reusability: the test target is a parameter (a bead set, a scenario tag, a make target), not hardcoded to the remote scenario.

## Non-goals
- Not a replacement for the fleet daemon or the normal `queue submit` loop.
- Not multi-machine / N-worker scheduling (separate lane, explicitly bottom-priority).
- Not auto-revive / supervision of the scratch daemon — standalone-and-disposable is the point (bootstrap-trap avoidance).
- Not the live-remote e2e (hk-nepva stays parked until the pyramid lands).

## Constraints
- MUST never touch the fleet daemon: no kill, no socket/tmux/binary clobber, no fleet-repo `br init`. (The landed guards — argv-gated PID kill, confirmed-scratch-gated tmux teardown, scratch≠fleet refusal, supervised-project refusal — are load-bearing and must extend to any new subcommand.)
- Standalone start = bare `harmonik --project <scratch>` inside tmux (requires $TMUX), built from the scratch clone's own source. NOT `harmonik supervise`.
- Twin/scenario tests give the fast loop (~seconds, no LLM/API): pair with `go test -tags=scenario`.
- Issue-feedback writes go to the MAIN repo's beads DB via `br` — must be idempotent / de-duplicated so re-runs don't spam duplicate beads.

## Success criteria
- One command spins up a scratch daemon, runs a named test batch, and returns a structured pass/fail summary.
- A failing batch item produces (or updates) a bead on the MAIN repo with provenance (which batch, which scratch run, the failure signature), without duplicating on re-run.
- The harness is documented as a general on-demand capability with a worked example, and the safety guarantee (never touches the fleet) is stated and enforced.
- Demonstrated end-to-end on a trivial batch (smoke) + on the remote-substrate scenario batch.
