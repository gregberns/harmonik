# Verdict

> Written last. A reasoned judgement, not a tally.

## PASS — Slice B holds.

The competing-consumers slice does what it was built to do, against a live `harmonikd mesh` process,
across three genuine consecutive runs. Work load-balances evenly (G1: 5+5 every run). A worker
`kill -9`ed mid-job loses nothing — its in-flight, leased-but-unjournaled job is requeued to a
surviving peer and completed there (G2: 19+1 = 20 exactly-once, every run). A stateful-plugin reload
mid-stream at > 100 msg/s — the primary and, separately, a worker, each swapped to a byte-distinct
binary — loses nothing and duplicates nothing (G3: 0/0 of 500, every run), the daemon that hosts
every kernel keeps its PID while the plugin children are swapped, and each reload completes in
54–67 ms, well under the 150 ms budget. All four channel types conform (G4), and the echo VC-12 gate
the slice definition names stays green in the same sweep.

**Zero jobs were lost or duplicated in any run.** The hard-stop condition never fired. No defect in
B1–B9 surfaced under load.

The standing gate is re-runnable by one command (`make chaos`) and, after the F1 fix, actually
re-executes the fault injection on every invocation rather than replaying a cached pass.

## Commits graded

| What | Commit |
|---|---|
| Branch under audit | `da7cd0113215a583e9cf11fb7286b76bd3b5cfa6` |
| Merge result built in a scratch clone | n/a — slice-tip acceptance gate, built on the pinned tip in place |

The B1–B9 substrate being graded is the pinned commit, unmodified. The B10 gate (the new
`tools/dispatch/chaos_test.go`, the dispatch slow-work knob in `tools/dispatch/plugin.go`, the
Makefile `-count=1` fix, the exploratory-cases file, this assessment) is the deliverable that grades
it and is left uncommitted for the orchestrator to review and commit.

## Not graded

- The kernel module's own chaos tests (echo VC-12, B9 four-type conformance) are re-run here, not
  re-reviewed — they are B9/K9 deliverables. G4 runs B9's cases and they pass; that is the extent of
  the grading of them.
- Slice C work (cross-machine transport, replicated LOOKUP, real probe loop, dynamic worker join),
  KV/backpressure/VC-11, and crash-loop relaunch are out of bounds by the task plan (Part 4) and were
  not exercised.

## Independence (carve-out, restated on the face of the verdict)

This session authored the B10 harness and ran it. The substrate under test (B1–B9) was built by other
sessions and is unmodified. The guard against a harness that hides a real loss is the adversarial
shape of the assertions (set-equality by job id, any loss/dup a hard stop naming exact ids), the three
consecutive genuine runs, and the echo VC-12 cross-check in the same sweep. The orchestrator reviews
and commits; this session did not self-approve a commit.

## What the four gate cases found

| Gate | Result | Weight |
|---|---|---|
| G1 load-balance | PASS — 5/5 exactly-once, three runs | load-bearing (the competing-consumers claim) |
| G2 worker-kill requeue (C5) | PASS — in-flight job requeued to survivor, exactly-once, three runs | load-bearing (the dead-worker guarantee) |
| G3 stateful reload under load (VC-12) | PASS — 0 loss / 0 dup, PID stable, within budget, three runs | load-bearing (the reload regression the slice names) |
| G4 four-type conformance (VC-13) | PASS — cross-node PUBSUB + B9's four cases, three runs | coverage (full channel width) |

## Residual risk

- **The gate proves the in-process mesh, not a networked one.** memmesh is a direct-call double, not
  the Slice-C cross-machine transport (Q-1, still open). Loss modes that only appear over a real
  socket — partitions, reordering, partial writes — are out of reach here by construction. The task
  plan states this split; Slice C owns it.
- **G3 kills are clean swaps via the admin reload, G2 is a `kill -9`.** A power-loss / fsync-tearing
  fault on the SQLite journal is not exercised; the `accepted` append uses `sync=true`, but a torn
  write under OS crash is a separate experiment (Slice-C-adjacent, disk-wipe).
- **Determinism of the 5+5 split rests on both workers attaching before the first job.** The harness
  gates submission on all nodes logging "node listening", which is after each worker's subscribe
  returns. If a future change makes the marker fire before the group membership is live, G1 could
  flake; the membership-before-marker ordering is the thing to preserve.
- **F1 was a latent hole in the chaos gate itself** (cached re-runs). Fixed here. Worth a glance at
  any other gate that claims "N consecutive runs" over unchanged source.

## For the operator

Slice B holds. Slice C may start. One thing to note, not decide: the `-count=1` fix to the `chaos`
target (F1) changes the shipped K9 echo gate's behaviour too — it now re-runs instead of caching.
That is the intended behaviour for a fault-injection gate, but it is a shared-Makefile change beyond
the B10 files, so it is called out rather than slipped in.
