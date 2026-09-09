# Evidence

> Append-only, written as you go.

## The tree this was run from

| | |
|---|---|
| Checkout | `/Users/gb/github/harmonik-slice-b-b10` |
| Branch | `work/slice-b-b10` |
| Pinned revision | `da7cd0113215a583e9cf11fb7286b76bd3b5cfa6` |
| Working-tree state | MODIFIED — B10 harness added (`tools/dispatch/chaos_test.go`), dispatch slow-work knob added (`tools/dispatch/plugin.go`), assessment folder. These are the B10 deliverable itself, left uncommitted for the orchestrator to review + commit. The substrate under test (B1–B9) is at the pinned revision, unmodified. |

A note on pinning: this is the slice-tip acceptance gate, not a cross-lane merge audit, so the gate
is built ON the pinned tip. The B1–B9 code being graded is the pinned commit; the added files are
the gate that grades it. No source under `kernel/` changed.

## Runs

| # | When | Command | Exit | Log | Notes |
|---|---|---|---|---|---|
| 1 | 02:03 | `make segments` (baseline, pre-B10) | 0 | `scratchpad/baseline-segments.log` | substrate green before the gate was added |
| 2 | 02:05 | `make chaos` (baseline, pre-B10: echo VC-12 + B9 conformance) | 0 | `scratchpad/baseline-chaos.log` | echo gate + B9 four-type conformance green before B10 |
| 3 | 02:18 | `go test -tags chaos -run TestG1LoadBalanceFiveFive` | 0 | inline | G1: shares worker-1=5 worker-2=5, accepted=10, exactly-once |
| 4 | 02:19 | `go test -tags chaos -run TestG2WorkerKillRequeue` | 0 | inline | G2: worker-1 killed after 1 done, survivor worker-2 did 19, total 20 exactly-once |
| 5 | 02:21 | `go test -tags chaos -run TestG3ReloadUnderLoadStateful` | 0 | inline | G3: 500 jobs @180 msg/s, primary reload 64ms, worker reload 54ms, 0 loss/0 dup, mesh PID unchanged |
| 6 | 02:23 | `go test -tags chaos -run TestG4FourTypeConformance` | 0 | inline | G4: cross-node PUBSUB ok; B9 four-type conformance green |
| 7 | 02:24 | `make segments` (after the dispatch slow-work knob) | 0 | `scratchpad/segments-after.log` | kernel-vocabulary + tool-isolation + import-closure all ok |
| 8 | 02:26 | `make chaos` x3 (first attempt) | 0,0,0 | `scratchpad/chaos-run-{1,2,3}.log` | run 1 ran (33.9s); runs 2+3 **served from Go's test CACHE** (0.91s). A cached chaos gate is not re-validating — see Finding F1. |
| 9 | 02:40 | `GOFLAGS=-count=1 make chaos` x3 (genuine, uncached) | 0,0,0 | `scratchpad/genuine-chaos-{1,2,3}.log` | three genuine consecutive sweeps: 71.57s / 71.10s / 71.18s real, no FAIL. echo gate 8.7s, kernel conformance 28.5s, dispatch 32.7s — all uncached each run. |
| 10 | 02:52 | `go test -tags chaos -count=1 -v ./...` x3 in tools/dispatch (per-gate numbers) | 0,0,0 | `scratchpad/dispatch-v-{1,2,3}.log` | per-run detail — table below |
| 11 | 03:05 | Makefile fix: add `-count=1` to the `chaos` target; `make chaos` x2 | 0,0 | inline | second plain `make chaos` re-ran dispatch (32.9s, NOT cached) — the gate now re-validates on every invocation (F1 fixed) |
| 12 | 03:10 | `golangci-lint run --build-tags chaos` on tools/dispatch | n/a | inline | findings match the echo chaos file's accepted patterns; chaos-tagged files are excluded from the real `segments-lint` gate (no chaos tag), which is green. Not a gate failure — see F2. |

## Three-run numbers (evidence #10, genuine uncached)

| Gate | Run 1 | Run 2 | Run 3 |
|---|---|---|---|
| G1 load-balance shares | worker-1=5 worker-2=5 | 5 / 5 | 5 / 5 |
| G2 victim done-before-kill / survivor / total exactly-once | 1 / 19 / 20 | 1 / 19 / 20 | 1 / 19 / 20 |
| G3 load rate | 178 msg/s | 179 msg/s | 180 msg/s |
| G3 primary reload latency | 66.7 ms | 60.7 ms | 63.3 ms |
| G3 worker reload latency | 62.9 ms | 56.8 ms | 54.5 ms |
| G3 loss / dup (of 500) | 0 / 0 | 0 / 0 | 0 / 0 |
| G3 mesh PID stable + child swapped | yes (2 of 3 children changed) | yes | yes |
| G4 cross-node PUBSUB + B9 conformance | pass | pass | pass |
| echo VC-12 gate (same sweep) | pass | pass | pass |

All six G3 reload latencies (54–67 ms) sit well under the 150 ms budget.

## Live legs — what actually went through the process

| | |
|---|---|
| Work driven through the loop | real `harmonikd mesh` subprocess (N=3: primary + worker-1 + worker-2), driven over gRPC (Publish to `dispatch.submit`) and admin HTTP (`/plugin/reload`); jobs submitted, killed, reloaded, journals read back |
| Reached a terminal state? | every gate: accepted journal settles at the submitted count; done-union settles equal to accepted |
| Did the work actually land? | verified in the kernel journals (`accepted` on the primary, `done` on each worker), read over the live KernelService `JournalRead` RPC |
| Cases re-run from `test/exploratory/cases/` | echo VC-12 (KV-001..004) and B9 conformance run in the same `make chaos` sweep; all held |
| New cases written this gate | `test/exploratory/cases/kernel-slice-b-chaos.md` (SB-001..SB-004) |

### Observations

| Question asked | What each surface said | What the journals actually showed |
|---|---|---|
| G1: did both workers take an equal share of 10 jobs? | test: `worker-1=5 worker-2=5` | the two `done` journals held 5 each; union set-equal to the primary's 10-job `accepted` set |
| G2: is a killed worker's in-flight job lost? | test: survivor completed 19 of 20, victim frozen at 1 | union of `done` journals == `accepted` (20), each job exactly once; the job leased to worker-1 at kill time appears in worker-2's `done` |
| G3: does a stateful reload mid-stream lose/duplicate? | test: 0 lost, 0 dup at 500 jobs | `accepted` held 500; done-union set-equal to it; mesh PID 31147 unchanged, plugin children swapped |
| G3: is the kernel restarted by a reload? | test: mesh PID unchanged, children changed | one OS process (all kernels) kept PID 31147; 2 of 3 plugin child PIDs changed (the reloaded primary + worker) |

## Conditions

| | Before | After |
|---|---|---|
| Box load | timing-sensitive gates (G3 reload latency) were run with no other heavy job on the box; lint deferred until after the three timed sweeps | — |
| Disk | ample (> 10 GiB free; no dispatch pause observed) | — |
| Sub-agents active in this tree | none | none |

## NOT RUN

- `make full` — out of scope for this gate: B10 is the chaos-tier slice gate (`make chaos`), not the
  unit/merge gate. `make segments` (the scoped build+vet+test+lint+boundary gate) WAS run and is green.
- Re-measuring the reload budget for the larger dispatch binary — deliberately reused the echo gate's
  recalibrated 150 ms ceiling (task plan Part 4, in-task-resolvable). Measured latencies (54–64 ms)
  sit well under it, so a re-measure was not needed.

## Reproduce it

    cd /Users/gb/github/harmonik-slice-b-b10
    make chaos              # the whole slice gate: echo VC-12 + B9 conformance + G1..G4
    # or one dispatch gate on its own:
    cd tools/dispatch && go test -tags chaos -run TestG2WorkerKillRequeue -v -count=1 ./.
