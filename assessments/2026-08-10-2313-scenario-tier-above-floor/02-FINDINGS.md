# Findings

> A row per confirmed defect, written as it is confirmed. The column no ledger has a field for is
> INTRODUCED vs INHERITED — that is the one that decides whether a gate is held.

No gate is being held here (this session is not a gate), so the last column records provenance for
the next reader rather than a hold decision.

## Confirmed

| # | Finding | Bead | Severity | Introduced or inherited |
|---|---|---|---|---|
| 1 | A codex scenario test sets a daemon-level `single` workflow default that the daemon deliberately refuses, so the run walks `commit_gate` (`make full`) inside a non-Go-module temp dir and dies on the traversal cap. Fails 4 of 4, including both isolation runs. | `hk-5ji8t` (new) | P1 | INHERITED — the test predates this branch |
| 2 | The same test's diagnostic names the wrong cause: it prints "the codex shim likely rejected a leaked `--model`" when the payload says `traversal cap hit at node "commit_gate"`. No `--model` leak occurs. | `hk-5ji8t` | part of 1 | INHERITED |
| 3 | The same test prints `PASS beadID=... gotTerminal=true` from an unconditional `t.Logf` after `t.Errorf` has already failed it. | `hk-5ji8t` | part of 1 | INHERITED |
| 4 | The scenario tier is red for the same reason `make core` is: wall-clock deadlines on a box that runs two or three lanes at once. Partially disjoint failure sets, 9 vs 6 across two runs, intersection 3. | comment on `hk-scenario-tier-nondeterministic-xt1wa` + `hk-core-gate-nondeterministic-m9vlf` | P1 / P0 existing | INHERITED |

Findings 2 and 3 are recorded as separate rows because each cost investigation time on its own,
but they live on one bead — they are one test's defects.

## Corrections to this lane's own prior record

**These are not defects in the product. They are places where this corpus was wrong, and the
corpus is this role's own output.**

| What was recorded | What is true | Where |
|---|---|---|
| `make test-scenario` "sits behind `lint-allow`" and could not be reached | `test-scenario` is its own target; only `build-all` gates it. `make full` merely sequences lint first. The tier needed one command, not a lint fix. | `LP-013`, corrected at `84ad44e20`; also stated on `hk-scenario-tier-nondeterministic-xt1wa` |
| The tier's instability is a disk story (`LP-014`) | Disk is one instance of a general class. Above the floor with zero real pauses, the tier is still red — from wall-clock deadlines under load. | `LP-018` (new), and the bead comment |

## Investigated and dismissed — recorded so nobody re-derives them

| Hypothesis | Why it was dropped |
|---|---|
| **A test `pkill`s sibling tests' processes.** `t2_scenarios_test.go` calls `pkill -SIGKILL -f` mid-tier, which would explain disjoint failure sets far better than load. | Refuted by reading the call site. The pattern is a per-test marker built from `t.Name()` and the pid, explicitly *not* the shared twin name, with a comment citing the bead where that was already fixed. Scoped correctly. |
| **`TestScenario_CommitGateCapTerminates_hki8g59` is a live infinite-loop regression** — it says so in its own failure message. | Its budget is a 60s wall clock its own comment calls "the safety net"; its trace shows two completed implement passes against a cap of 3; and it PASSED in run 2. Slow box, working cap. |
| **The daemon paused dispatch on disk 306 times.** | All 306 come from fixtures injecting synthetic values (`available=0MiB`, `available=4398046511104MiB`). Real pauses: 0 in both runs. Free space never below 22.1 GiB against a 10.24 GiB watermark. |
| **Three packages failed to build** (`FAIL pkg [build failed]`, `FAIL pkg [setup failed]`, `FAIL scenariopkg.test/scenariopkg [build failed]`). | Fixture strings printed *inside* passing tests (`TestClassifyScenarioGateError_CompileFail`). All three tests pass. A `grep '^FAIL'` overstates the damage. |
| **The daemon ignores a configured workflow mode** — the product bug implied by finding 1. | The refusal is deliberate and commented in `resolveWorkflow`: a daemon-level default naming `single` is not one of the two audited compatibility inputs. The product is right. Filing this against the daemon would have been the fourth instance of the failure mode the contract's §"did I measure the daemon, or my copy of it?" exists to prevent. |

## Not this lane's, and not held against it

`hk-pw1wv` — the receipt-GC lint allow-list failure that keeps `make full` red. Confirmed still
live: no commit on either branch touches `completion_gc.go`, `completion_gc_test.go` or
`tools/lintreport/allow.txt`. It is alpha's. It did **not** block the scenario tier, which is the
correction above.
