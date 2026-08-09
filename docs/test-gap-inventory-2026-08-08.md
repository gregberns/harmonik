# Test-gap inventory — 2026-08-08

Output of a five-way parallel audit run during the testing campaign on
`work/bravo-reachability`. Each entry names a test to write, the property it
pins, and **the one-line mutation that must turn it red**. An entry with no
nameable mutation was dropped rather than written down, on purpose.

Defects — things that are broken now, not merely untested — are filed as beads
labelled `found-by:testing-campaign` and are not repeated here. This file is the
*test* backlog.

Read the mutation column as the acceptance test for the test. A test nobody can
make fail is not coverage, and this repo has already deleted 681 files that
were.

## Scope — read this before picking a section

**Added 2026-08-09, after a session spent three test files in the wrong
subsystem.** This inventory was written before the core set was applied to it,
so it ranks sections by cost of regression and says nothing about whether the
code is in scope at all.

The core set, from the delete-and-rewrite charter §3, is:

> config → event bus → queue → bead-ledger adapter → worktrees → harness
> registry + one substrate → work loop → merge

Named as outside it: comms, crew, captain, keeper, dashboard, live-state,
subscribe, the sentinel, and the socket listener. Against that line:

| Section | In the core set? |
|---|---|
| 1. Queue transaction and replacement protocol | Yes — queue |
| 2. Reservation, release and claim paths | Yes — queue + work loop |
| 3. Daemon lifecycle and the missing crash tier | **No** — daemon process supervision |
| 4. Operator CLI | **No** — the CLI is a client over the socket listener, and the keeper rows are keeper |
| 5. Multi-harness dispatch | Yes — harness registry + substrate |

Sections 3 and 4 are not wrong about the gaps they name. They are coverage for
subsystems the rewrite may not keep, so they cost more than they return until
the core is done. Work 1, 2 and 5.

## How to use it

Work top-down inside a section. The sections are ordered by how much a silent
regression would cost. Confirm each gap still exists before writing — the tree
moves, and a few of these will close on their own.

Two standing traps that apply to every entry:

- `go test -run <pattern>` exits 0 when the pattern matches nothing, which is
  indistinguishable from a pass. Confirm with `-v` that the test ran.
- Never pipe `go test` or `make` into `tail` or `grep`. A pipeline returns the
  last command's status. Redirect to a file and grep the file.

---

## 1. Queue transaction and replacement protocol

| Test | Property | Mutation that must turn it red |
|---|---|---|
| `TestTransact_FailedWriteLeavesInMemoryItemsUntouched` | A failed write leaves item statuses exactly as they were. An item marked dispatched in memory but never on disk is a bead the daemon believes it launched. | Make `cloneGroup` in `internal/queuewiring/store.go` return its argument unchanged. |
| `TestWriteReplacementLosingTheIntentInstallRaceRefusesTheName` | When another writer's intent appears while we install ours, refuse the name. Never rename our candidate over the canonical queue. | In `internal/queue/transaction.go` `durableNoReplace`, make the post-link `present` arm return installed. |
| `TestWriteReplacementUnreadableIntentPathRefusesTheName` | An unreadable intent path establishes nothing, so it must shut the queue rather than claim the mutation is provably absent. | Change that first `readOptional` error branch to `noReplaceNotInstalled`. |
| `TestWriteReplacementNotInstalledIntentRemovesItsCandidate` | A definitely-not-installed intent removes the candidate it wrote. Nothing ever comes back for it. | Replace `cleanupErr := ops.remove(candidatePath)` with `var cleanupErr error`. |
| `TestQueueOpDrain_PauseIsDurableBeforeQueuePausedIsEmitted` | When `queue_paused` reaches the bus the file already reads paused. Every current emission test takes the `ProjectDir == ""` memory-only branch, so none of them proves this. | In `internal/queuewiring/operatorevents.go` `transitionQueue`, always take the memory-only branch. |
| `TestPersistFaultCuts` | Each of the five durability steps fails cleanly and the canonical file never changes. **Needs a product change first** — `Persist` has no syscall seam, unlike its two siblings. Mirror `migrateFromLegacyOps`. | Move the rename above the temp fsync, or drop the temp removal on the rename-failure path. Neither is detectable today. |
| `TestTransact_IntentCleanupFailureQuarantinesWithoutRollingBackTheCommit` | A cleanup failure after a durable commit quarantines, still installs memory, and reports committed with a non-nil cleanup error. **Needs a seam** — the branch is unreachable as written. | Delete the quarantine assignment from the post-install branch of `Transact`. |
| `TestAdvanceGroup_SecondCompletionAfterRecoverDoesNotRecountEarlierSuccesses` | A group re-opened by recovery and completed again reports counts a consumer can sum. `countOutcomes` walks every entry with no watermark. **Decide first** whether the second payload is cumulative or a delta — the spec does not say. | Skip entries whose bead already appeared terminally earlier in the slice. |

**Do not write:** external-writer clobber during a transaction. The detectable
half is covered exhaustively by `TestClassifyReplaceIntentExactFacts` (24 rows),
and the in-flight half is permitted by QM-001 by design. Do not pin a clobber
the spec allows.

**House idiom to copy:** `internal/queue` and `internal/queuewiring` are clean of
sleep-driven tests. Faults are injected with a struct of function fields
(`namespaceOps`, `migrateFromLegacyOps`) wrapped around the real `os` call,
driven by a table with `t.Parallel()` and `t.TempDir()`. Non-events are asserted
with a non-blocking `default:`, never a sleep.

---

## 2. Reservation, release and claim paths

| Test | Property | Mutation that must turn it red |
|---|---|---|
| `TestReleaseReservation_RefusesAnItemInANonActiveGroupWithTheSameIndex` and `..._InADifferentGroupIndex` | The release identifies its item by all four of group status, group index, item index and bead id. **Two of `activeQueueItem`'s four guards can be deleted today with the whole suite green** — they cover for each other in every fixture. | Delete the `GroupStatusActive` guard, or the `GroupIndex` guard. Needs a fixture with decoys carrying the same bead at the same item index. |
| `TestReleaseFrom_ExhaustsTheRetryBudgetAndReportsContention` | After the budget is spent the release stops, reports contention, and leaves the item dispatched rather than reporting a false success. `releaseRetryBudget` appears in zero tests. **Needs a one-word seam:** `const` → `var`, so a test can set it to 1. | Change `return last` to return retry-later, or make the loop unbounded. |
| `TestReserveQueueItem_CrossQueueDuplicateDoesNotChargeAnAttempt` | Losing a cross-queue race does not consume the item's attempt budget — the item did not cause the failure. | Add `item.Attempts++` to `failQueueItem`'s mutate. |
| ~~`TestWorkLoop_DispatchHaltAfterTheReservationDoesNotLeaveTheItemDispatched`~~ **WRITTEN 2026-08-09, and this row was wrong** | Every exit between the reservation commit and the claim either releases the item or records it. This row said three early returns leave the item dispatched, and marked it red today. Driven against the tree, two of the three are safe: both reachable exits leave through `exitClean`, which calls `drainCancelledQueue` and archives the whole active queue. Those two are now pinned in `internal/daemon/workloop_reservationwindow_test.go`. Only the claim-TransitionID exit is a real defect — it returns an error directly, skips the drain, and strands the item; filed as a bug for the fixing lane. | Delete the `drainCancelledQueue` call from `exitClean` — both written tests go red with the stranded-item message. |

**Cannot be fixed by a test — decide instead:**
`TestWorkLoop_ClaimSemaphore_BoundsClaimConcurrency` asserts `peak > 4`, but the
only caller is a single-threaded loop, so peak is structurally 1. Raise the
semaphore to 1000 or delete it entirely and the test stays green. Either delete
the semaphore or rename the test to what it pins — that the loop does not
deadlock. Do not write a replacement concurrency-bound test; nothing can make it
red while the claim path is serial.

**Do not write:** concurrent two-queue reservation. Selection and stamping run
on one goroutine for the daemon's life, so no work-loop test can interleave
them, and every mutation a unit-level version would catch is already caught
serially. Recorded as a known accepted gap in the delete-and-rewrite open
defects list.

---

## 3. Daemon lifecycle and the missing crash tier

The `crash` build tag is declared in the Makefile and typechecked by
`make vet-tagged`, and **no file carries it**. The tier was named and never
built. Every crash-recovery test today simulates the crash by hand; nothing
SIGKILLs a live daemon mid-write and reboots it.

A concrete design exists that needs no product change beyond five one-line call
sites — see the bead. The short version: add an `internal/crashpoint` package
whose `At(name)` is a no-op under `!crash` and kills the process under `crash`,
gated by an env var naming the point. Put points at the three protocol-exact
states in `writeReplacement`, one between the bead claim and the dispatched
write, and one inside `AcquirePidfile` between truncate and write. Rebooting
needs no sleeps, because boot reconciliation completes before the socket bind —
so waiting for the socket proves reconciliation finished.

Supervisor revive needs no product change either: `harmonik supervise _shim`
runs the watchdog alone without tmux, and all four timing knobs are existing
config fields. Nothing currently exercises `harmonik supervise` against a real
process — every existing revive target is `sh -c touch` or `true`.

| Test | Property | Mutation |
|---|---|---|
| `TestSupervisorRevival_PriorSessionWithoutShutdownIsTheOnlyTrigger` | Revival fires exactly when the second-to-last start block lacks a shutdown. Pure fold over a JSONL file — cheapest test on this page, and it does not exist. | Invert the `prior.hasShutdown` guard. |
| `TestComputeRestartBackoffDelay_...` and `TestApplyBootBackoff_...` | Backoff doubles from the first prior boot and clamps; boots outside the window do not count. All four symbols are untested; the only greps that hit them are comments. | `math.Pow(2, n-1)` → `float64(n)`; make the window filter always true. |
| `TestProbePidfileLock_ALiveRecordedPIDWithAnAcquirableLockIsAmbiguous` | PID reuse reports ambiguous, never stale — because the caller reads stale as "daemon dead" and would start a second daemon. | Return stale instead of ambiguous. |
| `TestOrphanSweep_ALiveCaptainSessionSurvivesEvenWhenItsPidfileIsStale` | A captain whose pane is live is spared even when its pidfile names a dead process. The live-pane fallback exists because the pid-only version reaped a live captain in production. `probeCaptainSentinel` has zero test hits. | Delete the live-pane branch. |

---

## 4. Operator CLI

Counting sub-verbs, roughly **35 verb/sub-verb pairs have no coverage**. The
cheapest large win in the repo. The daemon side of `comms` is well tested; the
client hop — the command an agent actually types — had zero tests, and that is
the one being closed first (see beads).

Highest value after `comms`, because these flip live daemon state:

| Test | Property | Mutation |
|---|---|---|
| `TestRunWorkerEnableDisable_OpNamePayloadAndExitCodes` | `worker disable` really disables. | Hardcode `enabled: true` — `worker disable` becomes a no-op that reports success, and remote dispatch keeps feeding a box you just took out of rotation. |
| `TestRunQueueSetConcurrency_...` | The op name and ceiling reach the daemon. The verb appears only in help text today. | Change the op string. |
| `TestKeeperAwaitAck_TimeoutExitsThreeNotOne` | A timed-out ack exits 3 and an argument error exits 1. That split is the verb's whole purpose. | Change `return 3` to `return 1`. |
| `TestKeeperHoldRelease_RoundTripAndExitCodes` | `release` actually clears the hold. Hold is the co-working override that suspends the restart cutoff — a silent no-op restarts the operator's session mid-conversation. | Swap `ReleaseHold` for `ClearDispatching` — the hold survives, the verb still exits 0 and still prints "hold cleared". |
| `TestKeeperEnableDoctorEntry_ResolveSettingsPathUnderHome` | The real operator path resolves under `$HOME`. All 31 existing enable/doctor tests inject the settings path directly, so this step never runs. | Point the join at `settings.json.bak` — enable writes the wrong file, doctor calls a healthy project broken, suite stays green. |

**Do not rewrite:** the positional-XOR-flags rule is fully covered by
`TestRunStart_Parser` (20 cases, both mixing errors, `--flag=value` counted as a
flag).

---

## 5. Multi-harness dispatch

Verified numbers, which correct an earlier estimate: `specs/examples/` holds
**29** graphs, not 22. **26 of 29 never reach a daemon** — 21 are driven only by
in-memory simulation and 5 are referenced by no test at all. **Zero shipped
graph selects a non-claude harness.**

Five separate lists of supported harnesses exist and already disagree; the
`--default-harness` help text omits pi.

| Test | Property | Mutation |
|---|---|---|
| `TestScenario_MixedHarness_ThreeItemsOneDaemon` | One daemon, three beads, three harnesses, each routed to its own binary and each completing. Nothing anywhere boots a daemon with more than one harness live. Leave `HandlerBinary` empty so the production resolution path is what gets exercised. | In `routedLaunchSpecBuilder`, hardcode the agent type to claude. |
| `TestScenario_PiTwin_SingleModeFullLifecycle` | A `harness:pi` bead reaches a pi binary with no test-only flag in the argv. Land this before the mixed test. | Delete the pi registration from `newHarnessRegistry`. |
| `TestRegistriesCoverTheSameAgentTypes` | The launch-side and watcher-side registries cover the same set. They are populated in different files by hand-written call sequences with nothing tying them; a harness registered in one and forgotten in the other launches and is never watched. | Delete the pi adapter registration from `bootsocket.go`. |
| `TestSpecsExamplesCorpusParsesAndValidates` | Every shipped graph parses and validates. Five are parsed by nothing. Costs nothing to run. | Delete a closing brace from any of them. |
| `TestScenario_DotNodeHarnessPin_RoutesCodexReviewer` | A DOT node's `harness=` attribute overrides the global default at tier 3. No shipped graph and no daemon test does this. | Delete the node-harness assignment in `dot_cascade_core.go`. |

**A trap to record:** `internal/daemon/harnessregistry_test.go` contains
`TestRoutedLaunchSpecBuilder_ClaudeParity`, which matches the Makefile's
`-run 'ClaudeParity'` pattern and has nothing to do with twin parity. Only the
package scoping keeps it out of the gate. Drop the scoping and
`make test-twin-parity-claude` starts passing on the wrong test.

---

## Provenance

Five parallel audits, each told to verify before asserting and to drop any gap
it could not name a mutation for. Their claims were spot-checked against the
tree, and several were corrected in the process — the graph count, the CLI verb
count, and the cause of the twin handshake failure were all wrong in the first
telling and are right here. Treat the rest the same way: confirm before you
build on it.
