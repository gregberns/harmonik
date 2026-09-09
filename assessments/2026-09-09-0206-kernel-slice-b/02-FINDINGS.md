# Findings

> Written as findings appear.

## Confirmed

| # | Finding | Issue | Severity | Branch-introduced? | Disposition | Evidence |
|---|---|---|---|---|---|---|
| F1 | `make chaos` served a CACHED pass on the 2nd and 3rd run in a session (0.91 s, "cached"). Go's test cache keys on source, and a chaos sweep that changes no source replays the cached result — so a "three consecutive runs" check would execute once and replay twice. A fault-injection gate that does not re-run the injection is not a gate. | none filed (fixed in this change) | P2 | gate-hygiene, pre-existing in the `chaos` target (affected the K9 echo gate too) | **fixed here** — added `-count=1` to the `chaos` Makefile target; verified plain `make chaos` now re-runs uncached (evidence #11) | row 8 + row 11 in `01-EVIDENCE.md` |
| F2 | The chaos-tagged test files are not linted by the real gate: `make segments-lint` runs `golangci-lint run` with no `--build-tags chaos`, so `chaos_test.go` (in both tools/echo and tools/dispatch) is excluded from the default build and never linted. Linting the dispatch harness with the chaos tag surfaces the same errcheck/gosec/noctx patterns the shipped echo chaos file already uses. | none | P3 | inherited (the echo chaos file set the precedent) | **passive** — the new harness matches the accepted echo-chaos style; not a gate failure. Noted so a future tightening of the lint gate covers both chaos files together. | row 12 in `01-EVIDENCE.md` |

No substrate defect was found. B1–B9 held under every fault the gate injects: uneven load, a worker
`kill -9` mid-job, and a stateful-plugin reload (primary and worker) mid-stream at > 100 msg/s.
Zero jobs were lost or duplicated in any run.

## Investigated and dismissed

| Looked like | What it actually was | How that was established |
|---|---|---|
| A killed worker might livelock the mesh: its node's dispatcher keeps leasing from the group queue and nacking (the dead member is never detached on `kill -9`, only on reload), so round-robin could keep assigning to the dead member. | It terminates cleanly. Each job assigned to the dead member is delivered (Unavailable) → nacked → re-assigned; the survivor drains the queue. G2 completed all 20 jobs exactly-once in ~9 s across three runs. | G2 ran to a clean exactly-once terminal state every run (evidence #10); no hang, no duplicate. |
| Stateful reload (journal replay on `Start`) might blow the 150 ms reload budget the echo stateless gate uses. | Replay is fast: the primary re-forwards only accepted-minus-forwarded, and the reads are SQLite-local. Measured 54–67 ms across six reloads. | G3 reload-latency subtest, three runs (evidence #10). |
| Primary reload while workers hold leases might requeue-storm. | The leases are transport-held on the primary's KERNEL, untouched by the primary PLUGIN process dying. No storm; zero dup. | G3 no-duplication subtest held every run. |

## Claimed-done, reconciled

| Claim | Confirmed by | Holds? |
|---|---|---|
| G1 — 10 jobs → 5+5 exactly-once, set-equal to accepted | `TestG1LoadBalanceFiveFive`, 3 runs, all 5/5 | yes |
| G2 — worker killed mid-job → every job done exactly once, in-flight job on survivor | `TestG2WorkerKillRequeue`, 3 runs, 19+1=20 exactly-once | yes |
| G3 — reload primary + worker mid-stream → 0 loss, 0 dup, kernel PID unchanged, within budget | `TestG3ReloadUnderLoadStateful`, 3 runs, 0/0, PID stable, 54–67 ms | yes |
| G4 — four-type conformance, VC-13 full width | `TestG4FourTypeConformance` (cross-node PUBSUB) + B9 conformance subprocess, 3 runs | yes |
| Echo VC-12 gate stays green in the same sweep | `make chaos` ran tools/echo uncached (8.7 s) green each of 3 sweeps | yes |
| `make segments` stays green (harness behind the chaos tag) | evidence #7 | yes |
| Whole gate holds three consecutive runs, one command | `GOFLAGS=-count=1 make chaos` x3 all exit 0 (evidence #9); the `-count=1` Makefile fix makes plain `make chaos` do the same | yes |
