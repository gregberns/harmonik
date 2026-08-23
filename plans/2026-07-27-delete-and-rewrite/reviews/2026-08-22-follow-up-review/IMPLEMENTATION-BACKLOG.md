# Charlie implementation backlog

Each task defends one claim. Use one implementation commit per task or stop gate,
run focused tests with `-count=1`, and deliberately mutate every new claim test to
prove it can fail. Do not edit this review to record implementation status; add a
dated reconciliation note.

## P0 — decisions and safety rails

### N01. Make the lint ratchet rename-aware

**Problem:** Exact file paths make behavior-preserving moves fail against 248
grandfathered daemon file/linter pairs.

**Scope:** After the operator chooses policy, teach the lint report comparison to
recognize a Git rename and transfer only the existing `(linter, finding)` debt for
that file. New findings must still fail.

**Acceptance:** Fixtures prove pure rename passes, rename plus new linter fails,
changed finding fails, copy-with-old-file-retained fails, and ordinary additions
still fail. Run the real lint gate on one scratch move.

**Limits:** Do not add allow-list entries or globally suppress changed-line lint.

### N02. Add an explicit dispatch-activation guard

**Problem:** A future implementer can treat the inert C21 producer as unfinished
wiring and wedge startup.

**Scope:** Add a tracked prohibition adjacent to the dispatch producer seam and a
test or build-time guard proving production code does not call intent creation.

**Acceptance:** The guard fails under a deliberate production `Store.Create` call.
Current dispatch and daemon-focused tests pass.

**Limits:** Do not delete the subsystem, wire the producer, or broaden replay actions.

### N03. Install the decomposition scoreboard

**Problem:** The daemon grew 9.7 percent between reviews without a visible alarm.

**Scope:** Add a reproducible report for production daemon lines, daemon test time,
white-box shims, and daemon lint pairs. Record the baseline 49,370 / 294s / 140 / 248.

**Acceptance:** One command emits all four values in stable machine-readable form.
CI verifies schema and nonnegative counts. Landing notes must explain production-line
increases.

**Limits:** Do not make timing variance a hard pass/fail threshold yet.

### N04. Decide and fence eager refill

**Problem:** The live daemon filler can compete with queue-specific planning and
does not rank by bead priority.

**Scope:** Implement the operator’s choice: explicitly disable it for managed-crew
mode or bind it to the selected queue and documented ranking policy.

**Acceptance:** Integration tests prove two queues cannot refill each other and the
disable switch prevents candidate fetching.

**Limits:** Do not redesign the scheduler or introduce a second planning engine.

## P1 — extraction foundation

### N05. Move the run registry to `internal/runregistry`

**Problem:** The daemon-owned registry is the remaining dependency for five seams.

**Scope:** Move `runregistry.go`, export `SnapshotWithKeys`, add an aborted-state
setter, migrate its production and test consumers, and add a deny-back-edge rule.

**Acceptance:** `go list -deps ./internal/runregistry` contains no
`internal/daemon`; focused registry and daemon tests pass; build, vet, lint, and
freeze gate pass. Re-measure the scoreboard.

**Limits:** No lifecycle redesign and no direct export of the `aborted` field.

### N06. Extract harness selection

**Problem:** 1,218 lines of harness/model/profile selection have no measured outbound
daemon dependency but still occupy the collision domain.

**Scope:** Move the measured file set to `internal/harnesspick`, export only symbols
named by the compiler, move tests, delete obsolete shims, and forbid daemon imports.

**Acceptance:** Package and consumer tests pass; dependency query has no daemon;
behavioral fixtures remain byte-identical; scoreboard decreases.

**Limits:** Do not change selection policy or registry schemas.

### N07. Extract spend meters

**Problem:** The spend subsystem is a leaf after N05 but remains daemon-owned.

**Scope:** Move the two meter implementations and their tests to `internal/spend`.

**Acceptance:** Exact budget and rollover tables pass; no daemon import; shims used
only by moved tests are deleted; scoreboard decreases.

**Limits:** Do not change accounting units or budget policy.

### N08. Extract handler pause

**Problem:** 2,056 lines of pause state and handlers form a coherent seam whose
remaining registry dependencies disappear after N05.

**Scope:** Move the six measured files and tests to `internal/pause`; expose only the
two compiler-required symbols and install a dependency fence.

**Acceptance:** Pause/resume/idempotence/race tests pass with `-count=1`; no daemon
import; removed shims and scoreboard deltas are recorded.

**Limits:** Do not merge operator pause or change event schemas.

## P1 — ownership and pure spine

### N09. Give run goroutines one supervisor

**Problem:** Goroutine lifetime, cancellation, and terminal observation are owned by
multiple call sites.

**Scope:** Complete C23: one supervisor owns launch, cancellation, join, and exactly
one terminal result for each admitted run.

**Acceptance:** Race-enabled tests cover normal exit, cancellation, launch failure,
double terminal signals, and shutdown. No orphaned goroutine remains.

**Limits:** Do not extend the run state machine into planning in this task.

### N10. Narrow run inputs by phase

**Problem:** `RunEnv` has 29 fields and `SharedHandles` 18, hiding ownership and
making tests construct unrelated dependencies.

**Scope:** Complete C26 by defining consumer-owned values/interfaces for admission,
planning, provisioning, execution, and observation. Migrate one phase per commit.

**Acceptance:** Each phase accepts only fields its implementation reads; compile-time
interface checks and focused tests pass; aggregate field counts decrease.

**Limits:** Do not create a renamed mega-struct or a service locator.

### N11. Name work-loop state and extract one pure decision

**Problem:** `runWorkLoop` is a 1,432-line state machine encoded in 18 live locals.

**Scope:** Introduce a private loop-state value without behavior change, then extract
one depth-three decision into `internal/orchestrator` using supplied facts and typed
results.

**Acceptance:** Existing loop tests pass; table tests cover the pure decision; a
mutation fails the table; `runWorkLoop` LOC decreases and does not gain parameters.

**Limits:** Do not move the work loop or combine multiple policies in one task.

### N12. Add a minimal core composition root

**Problem:** Construction is distributed through a wide config and boot state,
allowing core components to depend on daemon implementation details.

**Scope:** Complete C27 for only the charter core path. Construct typed ports at one
root and add an import fence that prevents core packages importing daemon.

**Acceptance:** One vertical config→event bus→queue→ledger→worktree→harness→substrate
→work-loop test passes; a forbidden import fixture fails.

**Limits:** Do not migrate socket, dashboard, or optional admission providers.

## P2 — later structural slices

### N13. Externalize optional admission providers

**Problem:** Optional providers widen the scheduler’s mandatory dependency surface.

**Scope:** Complete C28 by making providers explicit optional ports composed outside
the scheduler.

**Acceptance:** Absent, one-provider, and failing-provider tables are total and
deterministic; scheduler core compiles without concrete provider imports.

**Limits:** Preserve admission ordering and failure policy.

### N14. Extract graph-cascade helpers

**Problem:** A 2,098-line leaf remains in daemon and carries the one extraction with
material test-time payoff.

**Scope:** Move the measured helper file and tests after N05; expose the compiler-
measured boundary and add a no-daemon import fence.

**Acceptance:** Graph fixtures and full affected tests pass; the extracted package
does not import daemon; scoreboard and gate-time deltas are recorded.

**Limits:** Do not move `driveDotWorkflow` or change DOT semantics.

### N15. Prepare, then execute the control-plane cut

**Problem:** Socket, subscription, comms, dashboard, and live-state code form an
8,370-line legal cut only when moved together.

**Scope:** First produce a compiler-verified move manifest and resolve
`ConcurrencyController` plus `dashboardGateEvalInterval`; only then move all 30 files
and their already-external tests.

**Acceptance:** Scratch move builds before production edit; final package has no
daemon back-edge; socket protocol bytes and fan-out tests remain unchanged; lint and
scoreboard gates pass.

**Limits:** Do not attempt the older 24-file cut, add it to `CORE_PKGS`, or fix crew
routing incidentally.

## P3 — bounded quality work

### N16. Land safe automated lint fixes

**Problem:** A measured set of 25 findings is safely tool-fixable, while running the
repository config with `--fix` is unsafe.

**Scope:** Use only `--no-config --default=none` with the approved linter list, format,
remove only now-empty allow entries, and review the diff.

**Acceptance:** Build, vet, format, full lint judge, and affected tests pass; exact
before/after findings are recorded.

**Limits:** No `prealloc`, `forbidigo`, broad gocritic, or repo-config autofix.

### N17. Complete the four bounded error/literal tasks

**Problem:** C29–C32 remain useful compiler-edge cleanup but have lower leverage.

**Scope:** Four separate commits: same-package literals; workflow-loader typed parse
errors; tmux typed not-found error; remaining owned CLI/socket error-text sites.

**Acceptance:** Each commit has exact typed or byte-preservation tests and deliberate
mutation proof. Re-run detector counts after all four.

**Limits:** Do not replace wire vocabulary, test-twin tokens, or cross-package text
without an ownership decision.

### N18. Engineer the cheap gate wins

**Problem:** Every lane pays avoidable serial checks and cold compile cost.

**Scope:** Parallelize independent freeze scripts, warm a lane-local Go cache at
worktree creation, delete that cache with the worktree, then separately prototype
affected-package selection using `HK_GATE_BASE_SHA`.

**Acceptance:** Result equivalence tests compare serial and parallel failures; cache
ownership and cleanup are proven; timing samples and fallback behavior are recorded.

**Limits:** Never skip daemon tests solely because a heuristic says they are
unaffected; land affected-package selection behind an observation period.
