# Mission

> Written BEFORE anything runs. This file is what you were asked to do, frozen at the start.
> If the scope changes mid-assessment, add a dated note at the bottom — do not edit what is above
> it, or the record stops showing what you actually set out to do.

**Started:** 2026-09-09 02:06 (local)
**Assessor:** B10 implementer/assessor session (kernel Slice B chaos gate)
**Gate:** merge (the Slice B acceptance gate — does the competing-consumers slice hold?)
**Asked by:** operator / kernel-track orchestrator

## What is being gated

| Lane / branch | Commit | What is in it |
|---|---|---|
| `work/slice-b-b10` | `da7cd0113215a583e9cf11fb7286b76bd3b5cfa6` | Slice A + B1–B9: PTP transport (lease/ack/nack + dead-worker requeue), memmesh, REQUEST_REPLY, LOOKUP, roster, the dispatch plugin (primary\|worker, job_id dedupe, journal-replay rehydration), `harmonikd mesh` composition, the B8 PTP delivery loop + 3-leg drain gate, the B9 four-type conformance harness. |

**Merge base:** not a merge gate in the cross-branch sense — this is the slice-tip acceptance gate. The branch carries B1–B9; B10 adds the standing gate that decides whether the slice holds.
**Conflicts:** none measured; the gate is built ON this tip, not merged across lanes.

## Scope

In bounds: the four gate cases against a live `harmonikd mesh` process —
- **G1** load-balance: N=3 mesh, 10 paced jobs → the two workers' `done` journals hold 5 and 5 with set-equality against the sent `job_id`s.
- **G2** worker-kill requeue (the C5 path): N=3, `kill -9` one worker mid-job (slow-work knob) → every job done exactly once, the dead worker's in-flight job lands among a survivor's completions.
- **G3** VC-12 re-run vs a STATEFUL reload: jobs ≥100 msg/s, reload the primary (byte-distinct binary) mid-stream and separately reload a worker mid-stream → zero loss, zero dup by `job_id` set-equality, every kernel PID unchanged, reload within the echo gate's recalibrated 150 ms budget.
- **G4** four-type conformance (VC-13 full width) — B9's conformance cases in the same sweep.
- The echo VC-12 gate stays green in the same `make chaos` sweep.

Out of bounds (does not hold this gate): Slice C work (cross-machine transport, replicated LOOKUP, real probe loop, dynamic worker join), KV/backpressure/VC-11, crash-loop relaunch policy. All deferred with triggers named in the task plan Part 4.

## Independence

**Carve-out.** This session is both the implementer of the B10 harness and the assessor that runs it.
The harness code (`tools/dispatch/chaos_test.go`, the dispatch slow-work knob) is authored here; the
substrate under test (B1–B9) was built by other sessions. The verdict says so on its face: the gate
assertions are adversarial (any lost/dup job is a HARD STOP), so a harness bug that hides a real loss
is the risk, and the three-consecutive-run requirement plus the echo-gate cross-check are the guard.
The orchestrator reviews and commits; this session does not self-approve a commit.

## Known-red going in

Nothing known-red expected. Baseline `make segments` and `make chaos` (echo VC-12 + B9 conformance)
are expected green on this tip before B10 is added. Recorded in `01-EVIDENCE.md`.

## Decided before start

- **E1 (topology):** N-node memmesh double, N=2 default, **N=3 for G1/G2** (primary + two workers).
  Settled by the operator 2026-09-08 (task plan Part 4). A non-consuming primary + C3's
  one-instance-per-daemon makes 10→5+5 need three kernels.
- **Reload budget:** reuse the echo gate's recalibrated 150 ms ceiling (operator-approved 2026-09-08),
  rather than re-measure for the larger dispatch binary. In-task-resolvable per Part 4.
- **Slow-work knob:** the dispatch plugin gets a worker delay env knob so G2's `kill -9` lands while a
  job is in flight — the legitimate mirror of the harnesstestplugin `deliver-delay` knob B8 used.
- **Distinct binary paths per worker node:** both workers run the same dispatch binary referenced by
  distinct file paths, so a `kill -9` can target one worker node by `pgrep -f <path>` without adding a
  launch flag the dispatch `main.go` does not parse.
