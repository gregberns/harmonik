# Code-health audit — independent pass after P2 extraction

**Date:** 2026-07-24  
**Scope:** audit and prioritization only; no remediation or production-code changes  
**Relationship to prior work:** follows, but independently re-measures, `plans/2026-07-21-p2-extraction/`

Execution planning produced after the audit:

- [`INTEGRATED-EXECUTION-PLAN.md`](INTEGRATED-EXECUTION-PLAN.md) — integration with the active P2 LIFT,
  priority lanes, dependency DAG, staffing, gates, and takeover procedure;
- [`WORKTREE-RUNBOOK.md`](WORKTREE-RUNBOOK.md) — isolated worktree, lease, builder, review, merge, cleanup,
  and stale-agent recovery protocol.

## Objective

Identify the parts of Harmonik most likely to keep producing correctness failures, slow changes, and
hard-to-diagnose regressions after the P2 extraction campaign. The audit emphasizes:

1. concurrency, mutexes, goroutine lifecycle, and mutable shared state;
2. code whose structure makes meaningful tests impractical;
3. complex functions, files, and packages that can be detected by deterministic local checks;
4. lint suppressions, exclusions, and missing enforcement;
5. low or misleading coverage, especially at multi-system integration seams.

This document records prioritized findings and future remediation directions, but does not authorize or
contain implementation work.

## Audit rules

- Cite files and symbols, not line numbers alone.
- Distinguish measured facts from reviewer judgment.
- Verify production wiring; a well-tested helper with an untested composition root is still a gap.
- Do not count generated code the same as hand-maintained code.
- Do not treat raw line coverage as proof of behavior.
- Reconcile findings against completed P2 work so the plan does not prescribe already-landed extraction.
- Prefer enforceable local gates (`make check-fast`, `make check-short`, lint, unit/scenario tests); do not
  propose GitHub Actions.
- Group related symptoms under the smallest credible ownership boundary rather than inflating the count.

## Prioritization model

Each hotspot receives five 0–5 subscores:

| Dimension | What raises the score |
|---|---|
| **Failure impact (I)** | data loss, wrong process killed, stranded run, deadlock, silent wedge, cross-project harm |
| **Likelihood / exposure (L)** | production frequency, concurrency, mutable ownership, historical recurrence |
| **Structural risk (S)** | complexity, coupling, state-space size, unclear lifecycle, broad change surface |
| **Detection deficit (D)** | weak tests, mocks that bypass wiring, low coverage, nondeterministic-only failures |
| **Change pressure (C)** | active feature area, frequent edits, known future extension point |

Base score:

`priority = 6I + 5L + 4S + 4D + 2C` (maximum 105).

Then classify:

- **P0 audit hotspot (85–105):** existential correctness or safety risk; remediation planning should start
  before broad feature work in that subsystem.
- **P1 audit hotspot (65–84):** high recurring-cost area; staff as a named workstream.
- **P2 audit hotspot (45–64):** bounded but material debt; suitable for parallel improvement batches.
- **P3 audit hotspot (<45):** hygiene or preventive enforcement; batch after higher-risk structural work.

Confidence is recorded separately (`high`, `medium`, `low`) so weak evidence cannot gain authority merely
from a high severity estimate.

## Evidence collected

### Repository-wide quantitative baseline

- package/file/non-test LOC distribution;
- largest functions and measured cyclomatic/cognitive complexity;
- mutex/RWMutex/atomic/channel/goroutine sites and ownership clusters;
- globals and package-level mutable registries/caches;
- `//nolint` inventory and `.golangci.yml` exclusions;
- package and function coverage, including packages with production code but no tests;
- race-enabled test viability and failures;
- test-type inventory: unit, integration, scenario, crash-recovery, property;
- recent churn for hotspots, used only as a prioritization multiplier rather than a quality judgment.

### Independent review lenses

1. concurrency and shared-state ownership;
2. deterministic complexity and lint enforcement;
3. coverage and testability;
4. package architecture and composition roots;
5. process execution, cancellation, and cleanup;
6. queue/run state machines and persistence boundaries;
7. config, filesystem, git, and remote side effects;
8. adversarial synthesis review for omissions and false positives.

## Findings

The reconciled findings below include:

- ownership boundary and production symbols;
- measured evidence;
- concrete failure mode or maintenance cost;
- current test/detection status;
- score, confidence, and recommended sequencing;
- whether it is residual P2 work, a newly identified hotspot, or a preventive gate.

### Initial quantitative signal (not yet the final ranking)

Measured on the 2026-07-24 branch state:

- 85 production packages under `internal/**` and `cmd/**`;
- 213,530 non-test Go LOC;
- largest ownership boundaries by non-test LOC: `internal/daemon` 50,962,
  `cmd/harmonik` 32,106, `internal/core` 31,884, `internal/lifecycle` 9,366,
  `internal/keeper` 8,195, `internal/workspace` 7,760, and `internal/queue` 6,540;
- 463 functions (including tests) score above 20 under `gocognit`;
- the top production cognitive-complexity scores are `daemon.runWorkLoop` 888,
  `daemon.beadRunOne` 396, `daemon.runReviewLoop` 331, `main.run` 256,
  `daemon.pasteInjectQuitOnCommit` 215, `daemon.driveDotWorkflow` 195,
  `main.runHarnessWithSigs` 190, `daemon.dispatchDotAgenticNode` 185,
  `keeper.(*Watcher).Run` 179, and `main.runCommsRecvFollowIO` 175;
- the highest 90-day edit counts are concentrated in the same runtime files:
  `internal/daemon/workloop.go` 356 commits, `internal/daemon/daemon.go` 174,
  `cmd/harmonik/main.go` 127, `internal/daemon/dot_cascade.go` 116,
  `internal/daemon/reviewloop.go` 108, `internal/keeper/watcher.go` 78, and
  `internal/daemon/tmuxsubstrate.go` 67;
- `internal/daemon/workloop.go` remains 6,664 non-test LOC after the completed extraction slices.

These figures establish concentration and change pressure; they do not by themselves establish a defect.
Manual production-path review and test evidence determine the final score.

## Prioritized hotspot register

Scores incorporate the completed adversarial pass. The rank is by failure risk and detection deficit,
not by raw LOC.

### 1. Run orchestration graph and state machines — 105/105, P0, high confidence

**Ownership boundary:** `internal/daemon/workloop.go` `workLoopDeps`, `runWorkLoop`, `beadRunOne`,
`RunEnv`, `SharedHandles`, `RunPorts`; `internal/daemon/reviewloop.go` `runReviewLoop`;
`internal/daemon/dot_cascade.go` `driveDotWorkflow`, `dispatchDotAgenticNode`;
`internal/daemon/bootworkloop.go` and `cmd/harmonik/run.go` composition.

**Score:** I5 L5 S5 D5 C5.

Evidence:

- `workLoopDeps` is an 86-field mutable service locator assembled in stages by `newWorkLoopDeps` and
  `bootState.injectWorkLoopDeps`; validity is temporal rather than compiler-enforced.
- The extracted run bundles remain broad and partially valid: `RunEnv` has 30 fields,
  `SharedHandles` 17, and `RunPorts` 8. `runPorts()` deliberately returns required fields nil and a later
  assembly step fills them.
- The main state machines remain extreme: cyclomatic complexity 266 / ~1,667 lines for `runWorkLoop`,
  217 / ~2,288 for `beadRunOne`, 136 / ~1,582 for `runReviewLoop`, and 91–99 / ~883–1,001 for the DOT
  driver functions.
- Composition is split across CLI config resolution, `daemon.Start`, `bootState`, boot helpers, and late
  dependency injection. There is no single immutable, inspectable production graph.
- Scenario tests commonly use `StartForTesting`, an injected empty-commit worktree factory, an injected
  merge mutex, a phase-aware twin, and recovery skip flags. Those are useful tests, but they do not prove
  the actual production graph.
- Daemon statement coverage is about 58.9%; P2 runtime proof remains explicitly deferred while the daemon
  is down.

Failure modes: omitted/overwritten wiring, tests exercising a different graph from production, invalid
bundle combinations, stranded runs, wrong terminal transition, and changes whose blast radius cannot be
locally reasoned about.

Relationship to P2: **residual and partially owned by the planned RT20+ lift.** Do not create a competing
extraction plan. The follow-on must validate that the lift eliminates partial-valid bundles and narrows
state ownership rather than merely moving the same locator and giant functions to another package.

### 2. Process-tree ownership and cancellation — 99/105, P0, high confidence

**Ownership boundary:** `internal/daemon/dot_cascade.go` `dispatchDotToolNode`;
`internal/lifecycle/tmux/runner.go` `SSHRunner.Command`; `internal/handler/session.go` `Kill`, `runWait`,
`SendInput`; process launch and teardown paths in daemon, lifecycle, workspace, and harness packages.

**Score:** I5 L5 S4 D5 C4.

Evidence:

- Remote DOT tool cancellation kills the local SSH client but leaves the worker-side `/bin/sh -lc` and
  build/test process tree running. The production code explicitly records this observed pile-up class.
- The local path uses a process group, negative-PGID kill, and `WaitDelay`; local tests therefore cannot
  establish remote cleanup.
- Handler sessions can safely signal only the immediate child PID. Grandchildren may survive until an
  orphan sweep. A canceled blocked `SendInput` also leaves its writer goroutine alive until later close.
- No required real-SSH cancellation test proves the remote process tree is gone.

Failure modes: resource exhaustion, expensive gates continuing after a canceled run, inherited descriptors
keeping waits alive, provider/process leaks, and repeated dispatch colliding with abandoned work.

### 3. Event bus concurrency and durability — 95/105, P0, high confidence

**Ownership boundary:** `internal/eventbus/busimpl.go` `busImpl.Emit`, `EmitWithRunID`,
`EmitAgentMessage`, `EmitAgentPresence`, `EmitTyped`, `Drain`, and `DrainRun`.

**Score:** I5 L5 S4 D4 C4.

Evidence:

- The 1,650-LOC implementation creates a goroutine for every async/observer delivery; comments acknowledge
  the missing bounded worker pool.
- Five public emission implementations repeat related dispatch/counter bookkeeping across global and
  per-run drain domains.
- Regression tests document prior WaitGroup and early-drain failures.
- Statement coverage is roughly 58–64%, depending on the profile. One environment-sensitive concurrency
  latency run exceeded its P99 budget during this audit; that is a stress signal, not proof of drain
  correctness failure.

Failure modes: unbounded goroutine growth, drain returning early or never returning, publish/close races,
dead-letter loss, and durability/replay disagreement under I/O failure.

### 4. Supervisor concurrent state — 92/105, P0, high confidence

**Ownership boundary:** `internal/supervise/supervisor.go` `Supervisor.Stop`, `Run`,
`runHealthProbe`, and the assume-running timer.

**Score:** I5 L4 S4 D5 C3.

Evidence:

- `Stop` claims concurrent safety but uses a select/default followed by raw `close(stopCh)`; two concurrent
  callers can both pass the select and the second close panics.
- Timer, health probe, and main loop independently perform atomic load-copy-store updates. An older snapshot
  can overwrite a newer PID/status/exit/restart field.
- Existing tests call `Stop` from only one goroutine and do not model competing state writers.

Failure modes: supervisor crash during shutdown and stale or incorrect daemon health/lifecycle state.
This is smaller than the run loop, but a concrete contract violation keeps it in P0.

### 5. Durable state atomicity and corruption visibility — 92/105, P0, high confidence

**Ownership boundary:** `internal/schedule/store.go` `SuspendAllForSleep`, `RestoreFromSleep`, and
`writeSuspendedSet`; `internal/lifecycle/branchtip_em024a.go` `WritePersistedTip` and
`CheckBranchTipMonotonicity`; `internal/replay/replay.go` `Replay`; eventbus scanning;
`internal/sessiondata/sessiondata.go` `Append` and `ReadAll`.

**Score:** I5 L4 S4 D5 C3.

Evidence:

- Schedule sleep first durably disables jobs, releases its flock, then separately writes the set to
  restore. A crash between the two leaves jobs disabled with no reconstructable pre-sleep enabled set.
  The sidecar uses plain truncate/write, no atomic rename/fsync, and no shared transaction lock; no
  suspend/restore test was found.
- The branch-tip rewind sensor claims atomic persistence but writes the canonical tip with
  `os.WriteFile`. A torn/empty file is treated as a first observation, so the rewind detector fails open
  precisely after crash or disk-full corruption.
- Replay consumes an event-bus scan that logs and skips malformed envelope lines. Its `Malformed` report
  therefore cannot count those missing transitions and may report a clean replay over a corrupt stream.
- Session metrics append without sync/process locking and reads silently skip every malformed line, including
  mid-file corruption.

Failure modes: wake cannot restore operator intent, rewind protection is defeated, replay proves invariants
over missing transitions, and operator metrics silently undercount. These are small compared with the
daemon giants but are direct durable-invariant violations.

### 6. Codex resident-session ownership — 84/105, P1, high confidence

**Ownership boundary:** `internal/codexdriver/session.go` `codexSession` lifecycle;
`internal/codexdriver/inputqueue.go` `BoundedInputQueue.Close`;
`internal/codexdriver/resident.go` `ResidentSession.Close`.

**Score:** I4 L4 S5 D3 C4.

Evidence:

- One 1,173-LOC session object owns five mutex domains, two `Once` guards, four lifecycle channels,
  mutable correlation/waiter/timer maps, and three long-lived goroutines.
- Package coverage is relatively strong (~85.7%), but statement reach does not prove lock order or lifecycle
  invariants.
- Queue close can wait forever for a `SubmitInput` invoked with a caller-owned non-canceling context.
- `ResidentSession.Close` marks the resident closed before draining buffered items through it, contradicting
  the documented drain-through-current-child behavior.

Failure modes: lost/misattributed acknowledgements, write-after-close, waiter/timer/goroutine leak,
finalization deadlock, and indefinite shutdown.

### 7. Tmux substrate capability and shared state — 81/105, P1, high confidence

**Ownership boundary:** `internal/daemon/tmuxsubstrate.go` `tmuxSubstrate`, `perRunSubstrate`,
`resizableSemaphore`, `SpawnWindow`, `spawnWindowVia`, and `tmuxSubstrateSession.Wait`.

**Score:** I4 L3 S5 D4 C3.

Evidence:

- 3,023 LOC with multiple lock domains, spawn semaphores, maps, resize/timer callbacks, and per-run mutable
  target/session/adapter state. `spawnWindowVia` has explicit complexity suppressions.
- A large family of optional local interfaces and type assertions makes behavior depend on hidden
  capabilities of concrete wrappers; wrapping can silently erase a feature.
- The first caller of `Wait` supplies the context for the sole background poller; its short deadline can
  determine the shared outcome seen by later callers.
- Historical comments tie shared target state to a seven-hour stall.

Failure modes: capacity wedges, incorrect remote/local feature selection, context-dependent shared outcome,
and mutable state leaking across runs.

### 8. Worktree/git cleanup and destructive probes — 70/105, P1, high confidence

**Ownership boundary:** `internal/workspace/createworktree.go` `createWorktreeWithRetry` and
`cleanupPartialWorktreeState`; `internal/workspace/mergedispatch_wm018a.go`
`DetectSquashMergeConflict`; `cmd/harmonik/promote_cmd.go` cleanup; daemon worktree reclaim/sweep paths.

**Score:** I4 L2 S4 D4 C2.

Evidence:

- Several teardown paths reuse the already-canceled operational context; remote cleanup then fails
  immediately and leaves directories, branches, or git worktree registrations.
- The squash-conflict probe runs a real `git merge --squash` followed by unconditional
  `git reset --hard HEAD`, with no deadline, internal cleanliness precondition, or ownership lock.
  Production search found zero production callers, only tests. It is therefore a latent hazardous API,
  not evidence of currently exposed runtime data loss.
- Disk reclaim and Claude worktree sweep can fall back from failed git removal to raw directory deletion
  while ownership evidence may be stale.
- Workspace has many tests and ~82% coverage, but zero-covered functions cluster at remote/reviewer/git
  composition seams.

Failure modes: stale registrations, retry collision cascades, loss of uncommitted work, and remote/local
cleanup divergence.

### 9. Lifecycle recovery and orphan reconciliation — 81/105, P1, high confidence

**Ownership boundary:** `internal/lifecycle/orphansweep.go`, daemon `RunOrphanSweep`, process provenance,
Beads/queue/worktree reconciliation, and restart recovery.

**Score:** I5 L3 S4 D4 C2.

Evidence:

- The package coordinates OS processes, tmux, Beads, queue state, and worktrees across partial failure.
- Current short coverage is about 77.6%, with production adapters such as `KillTmuxSession`,
  `SetsidDaemon`, and `ReadProcessCmdlineArgs` uncovered.
- Tests lean heavily on fake adapters; the highest-risk crash sequences and PID-reuse/cross-system
  disagreement are not comprehensively exercised.
- The existing backlog already contains concrete process-provenance and orphan-sweep defects. This audit
  treats those as evidence for one ownership problem, not as isolated bugs.

Failure modes: wrong process killed or spared, live work reclaimed, stale work left forever, and
state stores disagreeing after a crash.

### 10. Keeper operational state machine — 82/105, P1, high confidence

**Ownership boundary:** `internal/keeper/watcher.go` `Watcher.Run`, step/cycle/inject/ack/respawn paths,
tmux/comms ports, and process-global test seams.

**Score:** I4 L4 S4 D4 C3.

Evidence:

- `Watcher.Run` is ~549 lines with cyclomatic complexity 71 and cognitive complexity 179.
- The special keeper coverage gate covers only a subset of files, not the watcher/inject/ack/respawn
  composition that performs operational actions.
- A short test run during the audit failed its heartbeat derive-miss budget despite reporting high package
  coverage, indicating non-hermetic/shared-state or timing sensitivity.
- Important tmux/mac integration behavior is tagged outside the standard short gate; multiple action and
  adapter functions remain zero-covered.

Failure modes: missed restart warning, incorrect context action, duplicated or mistimed injection, and
tests whose result depends on shared machine state.

### 11. Queue persistence and restart behavior — 82/105, P1, high confidence

**Ownership boundary:** queue `Persist`, `MigrateFromLegacy`, `CompleteAndUnlink`,
`CancelQueueOnShutdown`; RPC `HandlerAdapter`; direct persistence callers across workloop, run ports,
spend meter, lifecycle recovery, and crew start.

**Score:** I4 L4 S4 D4 C3.

Evidence:

- Individual queue writes use atomic rename, but there is no persistence-level generation/CAS/flock.
  Full-snapshot concurrent renames are last-writer-wins; correctness depends on all writers sharing one
  in-memory `QueueStore` mutex.
- RPC append uses clone→persist→install and has a lost-update regression test, but many direct `Persist`
  callers make the invariant composition-wide and convention-based. A legacy unlocked handler fallback
  remains.
- Legacy migration deletes the valid old queue merely because the destination exists, without first proving
  the destination parses. A zero-length/corrupt destination can therefore cause loss of the only good copy.
- Completion/cancellation helpers mutate caller-visible memory before durable success, unlike the corrected
  append transaction pattern.
- Queue mutation/restart property tests and crash-point tests are sparse; open retry/wake defects corroborate
  the ownership risk.

Failure modes: lost updates, memory ahead of disk, deletion of the only valid queue, stale fields regressing,
and duplicate/unbounded dispatch after restart.

### 12. CLI and socket composition — 70/105, P1, high confidence

**Ownership boundary:** `cmd/harmonik/main.go` `run`; `cmd/harmonik/run.go` `runBeadSubcommandIO`;
`cmd/harmonik/harness.go` `runHarnessWithSigs`; comms/subscribe/supervise commands;
daemon socket constructor ladder and `bootState`.

**Score:** I3 L4 S3 D3 C4.

Evidence:

- CLI routing and command execution functions range from cyclomatic complexity 56 to 216 and hundreds to
  ~1,306 lines.
- Socket construction uses telescoping handler APIs with roughly 7–12 parameters and permits many partial
  graphs.
- `cmd/harmonik` is ~58.3% covered after the recent improvement; supervise is ~47.9% and twin-pi 0%.
- Helper tests with injected I/O do not prove flag/env/default resolution selects the intended production
  graph.

The flat `main.run` switch is less cognitively coupled than its raw cyclomatic number suggests; prioritize
composition validation and cohesive command functions ahead of cosmetic router splitting.

### 13. Mutable package globals and test seams — 69/105, P1, high confidence

**Ownership boundary:** mutable function/slice/map globals in supervise, transport, scenario, release,
daemon timing/probe hooks, CLI hooks, and related test overrides.

**Score:** I3 L3 S3 D4 C4.

Evidence:

- Function variables and exported mutable collections are routinely replaced by tests without
  synchronization.
- The most concerning cases sit in concurrently active timing/probe code such as paste injection and stale
  watching, where snapshot/restore can race with live goroutines or parallel tests.
- `gochecknoglobals` is not enabled as a general gate.

This should be a zero-growth policy with narrow immutable/ldflag/sentinel exceptions, not a mechanical ban
on all package values.

### 14. Remote worker telemetry and capacity policy — 67/105, P1, medium confidence

**Ownership boundary:** `internal/workers/telemetry.go` `parseWorkerReport`, report polling, breach
detection, and registry state.

**Score:** I3 L3 S3 D4 C3.

Partial/untrusted remote reports and time/network degradation drive worker availability and routing.
This was missed by the complexity-first scans because individual functions are moderate. It should receive
fault-injection tests for stale, partial, contradictory, and reordered reports before being promoted above
its current position; current evidence does not justify a large rewrite.

### Parallel P2 candidates requiring per-cluster scoring

Candidate clusters:

- `internal/workflow/dot/parser.go` `buildNode` and `tokenize`;
- `internal/queue/validation.go` sub-validators;
- command-specific parsing/output functions in comms, dashboard/state, init, and sync-assets;
- intentional duplicate protocol adapters and validators, after conformance ownership is decided.

These have measurable complexity but substantially lower concurrency and side-effect risk. They are not
finalized as one synthetic hotspot: score each cluster separately when converting the audit into work.
They are good file-disjoint lanes after the P0/P1 ownership plans are specified.

## Coverage and testability register

The repository has high test-file counts in several risky packages, but the key deficit is production-path
fidelity:

| Boundary | Current signal | Missing proof |
|---|---|---|
| daemon run graph | ~58.9%; 498 test files / 102 production files | compiled binary happy path; real graph; crash at each durable transition |
| eventbus | ~58–64%; concurrency regressions exist | race/stress/leak, I/O failure, replay and drain invariants |
| lifecycle | ~77.6%; fake-adapter rich | process/tmux/Beads/git partial-failure and PID-reuse cases |
| keeper | high reported coverage but audit run failed | hermetic watcher→tmux/comms action path |
| workspace | ~82%; many tests | real remote/reviewer worktree and cleanup under cancellation |
| CLI | harmonik ~58.3%; supervise ~47.9%; twin-pi 0% | flag/env/default→real composition wiring |
| queue | ~66%; validator-heavy | socket/store/restart concurrency and crash recovery |
| codexdriver | ~85.7% | lifecycle model, lock order, stuck-port shutdown, leak bounds |

There are roughly 479 `t.Skip` or short/E2E guard occurrences across `internal/**`, `cmd/**`, and tests.
The final baseline must classify these by reason; a raw count is not itself actionable.

The general `scripts/coverage-gate.sh` is not a live merge constraint: it runs under bare `make check`,
which project instructions describe as known-red, and is absent from `make check-short`. Current baselines
are far below its nominal 90%/95% floors. The recent cmd coverage ratchet is real and useful, but is
regression-only.

## Deterministic guardrail candidates

Candidates will be accepted only when the repository baseline and false-positive behavior are known.
Likely categories include complexity, function length, nesting, duplicate-code detection, suppression
hygiene, package dependency boundaries, coverage non-regression, race testing, and spawn-site lifecycle
registration.

### Recommended local enforcement ladder

**Immediate, baseline-preserving gates**

1. Add a production complexity/span baseline keyed by package + symbol. Fail a new function above cyclomatic
   15, cognitive 20, or 100 lines, and fail any legacy hotspot whose metric increases.
2. Prevent files already above 1,500 behavioral LOC from growing; warn for new behavioral files above 800
   and fail above 1,200. Exempt exact declarative catalog files, not entire packages.
3. Ratchet production `//nolint` counts globally and per file. Complexity suppressions need a named debt
   owner/expiry; no new free-form escape hatch.
4. Turn coverage into a live per-package non-regression gate in `check-short`, starting from honest current
   baselines. Report zero-covered changed functions separately.
5. Add a production mutable-global baseline and reject new mutable function/map/slice globals outside a
   narrow allowlist.
6. Forbid bare `exec.Command` and `exec.CommandContext(context.Background(), ...)` in production except an
   exact documented allowlist. Cleanup operations must declare their own bounded teardown context.

**Report-first, then ratchet**

7. Pilot `nestif`, `maintidx`, and `dupl` on production code. Use `dupl` around 150 tokens for reporting and
   consider 200 for a gate; intentional adapter/catalog symmetry needs explicit treatment.
8. Report functions with more than 7 parameters (fail new functions above 10) and structs with more than
   two mutex/atomic domains or three lifecycle channels.
9. Re-enable `gocritic` `hugeParam` and `rangeValCopy` in report mode, replacing the current global disable
   with exact exclusions if measurements justify them.
10. Run report-only complexity on scenario/specaudit/test harnesses even if production ceilings remain
    excluded; an unmaintainable oracle is still a risk.

**Semantic architecture and test gates**

11. Add a production-graph test that assembles through the real CLI/daemon composition and asserts required
    capabilities exactly once; compare harness and adapter agent-type registration.
12. Baseline and reject growth in run-bundle fields, optional substrate capability assertions, and socket
    constructor parameters.
13. Maintain a process-spawn register: every `Start` site declares Wait ownership, process-tree cancellation,
    and restart/orphan semantics.
14. Add named manifests for composition-root, real-binary, remote-process-tree, crash-point, property, race,
    and unexpected-skip coverage. These are separate from line coverage.

Do not use raw mutex counts, raw file size, or cyclomatic complexity alone as hard correctness gates. They
are triage signals; the ratchets above combine them with ownership and baseline behavior.

## Staged remediation roadmap (future scope)

This is sequencing guidance only; implementation is explicitly outside this audit.

### Stage A — proven safety defects and honest gates

- Concurrent supervisor stop/state update.
- Remote process-tree cancellation and bounded cleanup contexts.
- Eventbus fan-out/drain ownership.
- Live coverage and complexity/suppression ratchets using current honest baselines.

These are serialized by ownership boundary but can run in parallel with each other.

### Stage B — runtime ownership plans

- Complete RT20+ with explicit exit criteria for eliminating partial-valid run bundles and late service
  location.
- Specify Codex session lifecycle/lock order and bounded close semantics.
- Specify tmux substrate capability and Wait ownership.
- Specify lifecycle/worktree/orphan recovery invariants and crash matrix.
- Specify keeper watcher/action boundary and hermetic adapter strategy.

Architecture work in this stage should be spec/kerf-first. Avoid introducing a generic DI container: that
would formalize the current service-locator problem.

### Stage C — production-path test net

- Real compiled-binary local happy path.
- Real SSH cancellation/process-tree cleanup.
- Durable-transition crash matrix for queue, merge, Beads, git, and worktree state.
- Race/stress/leak suites for eventbus, supervisor, codexdriver, tmux substrate, lifecycle, queue, and keeper.
- Tagged-suite and unexpected-skip manifest.

### Stage D — parallel complexity drains

Run file-disjoint batches for queue validation, DOT parsing/validation, cohesive CLI commands, stale/subscribe
helpers, and other P2-ranked pure-logic clusters. Each batch lowers a ratcheted baseline; it does not merely
move code or add another suppression.

### Parallelization map

| Lane | Serialization constraint | Can run alongside |
|---|---|---|
| run graph / RT20+ | single owner for workloop, run bundles, boot wiring, DOT/review loop | supervisor, eventbus, durable-state, keeper |
| process ownership | single contract across handler, tmux substrate, SSH runner; coordinate with run graph at call sites | supervisor, eventbus, schedule/replay |
| eventbus | single owner for emit/drain/durability invariants | all except consumers whose tests require API changes |
| supervisor | isolated package and focused contract defects | every other lane |
| durable state | split by schedule, branch-tip, replay/sessiondata, and queue after one shared durable-write contract is settled | run graph, eventbus, keeper |
| lifecycle/workspace | serialize destructive recovery ownership decisions; parallelize read-only probes and test matrices | supervisor, eventbus, CLI |
| keeper | single owner for watcher/cycle temporal model; tmux integration tests coordinate with substrate lane | eventbus, queue, pure-logic batches |
| pure-logic drains | file-disjoint queue validator, DOT parser, and cohesive CLI commands | all structural lanes |

## Existing lint/exclusion register

| Exclusion or suppression class | Audit disposition |
|---|---|
| complexity excluded for all tests, scenario, specaudit | keep production exclusion; add report-only harness signal |
| `cmd/**` structured-logger `fmt.Print*` exclusion | keep; printing is the CLI contract |
| test-only unchecked `Close` exclusion | re-measure; retain only if signal remains overwhelmingly teardown noise |
| whole `tools/**` `forbidigo`/`noctx` exclusion | narrow after inventory; synchronous tools can still hang |
| whole `internal/testhelpers/**` `forbidigo` exclusion | retain only for assertion panic; do not mask unrelated forbidden calls |
| global `gocritic` `hugeParam`/`rangeValCopy` disable | replace with report-first measurement and narrow exceptions |
| seven production complexity suppressions | convert to explicit zero-growth debt allowlist |
| inline `gosec` suppressions (dominant population) | classify fixture/path cases; consolidate justified patterns so exceptional cases are visible |
| delta-only legacy complexity enforcement | replace/augment with symbol baseline ratchet |

## Reproducible baseline appendix

Environment used:

- Go `go1.26.1 darwin/arm64`;
- repository `.tools/golangci-lint` `2.3.0`;
- `gocognit` `github.com/uudashr/gocognit v1.2.1`;
- `gocyclo` `github.com/fzipp/gocyclo v0.6.0` in the independent complexity pass;
- git churn window: 90 days ending 2026-07-24.

Representative commands:

```bash
# Production size by top-level ownership boundary.
find internal cmd -name '*.go' ! -name '*_test.go' -print0 |
  xargs -0 wc -l |
  awk '$2 != "total" {
    split($2, a, "/"); owner = a[1] "/" a[2]; loc[owner] += $1; files[owner]++
  } END {
    for (owner in loc) printf "%8d %4d %s\n", loc[owner], files[owner], owner
  }' |
  sort -nr

# Cognitive-complexity inventory (v1.2.1; include tests, matching the published 463 count).
go run github.com/uudashr/gocognit/cmd/gocognit@v1.2.1 \
  -test -over 20 internal cmd

# Cyclomatic-complexity inventory (v0.6.0; production only).
go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 \
  -ignore '_test\.go$' -over 10 internal cmd

# Package test-presence inventory.
go list -f '{{.ImportPath}} {{len .GoFiles}} {{len .TestGoFiles}} {{len .XTestGoFiles}}' \
  ./internal/... ./cmd/...

# Concurrency primitive candidate inventory. Counts are triage signals only.
rg -n --glob '*.go' \
  'sync\.(RW)?Mutex|atomic\.|\bgo\s+func|\bgo\s+[A-Za-z_]|make\(chan|chan\s' \
  internal cmd

# Ninety-day commit-touch and line-churn concentration. A commit produces at most
# one numstat row per path, so n[path] is the number of commits touching the path.
git log --since='90 days ago' --numstat --format='' -- '*.go' |
  awk 'NF == 3 && $1 ~ /^[0-9]+$/ && $2 ~ /^[0-9]+$/ {
    n[$3]++; added[$3] += $1; deleted[$3] += $2
  } END {
    for (path in n) printf "%5d %7d %7d %s\n",
      n[path], added[path], deleted[path], path
  }' |
  sort -nr

# Live suppressions and config exclusions.
rg -n --glob '*.go' '^\s*//nolint(?::|\b)' internal cmd
rg -n 'exclusions:|path:|path-except:|disabled-checks:|funlen:|cyclop:|gocognit:' \
  .golangci.yml
```

Coverage percentages in this plan come from fresh independent package profiles where available and the
ratified repository baselines otherwise. Differences of a few points between profiles are expected when
short/tagged/integration suites differ; the plan uses ranges where that distinction matters.

## Audit limitations

- The audit did not run the intentionally down primary daemon and did not claim live runtime proof.
- Several full-tree test/lint commands are known-red or were affected by concurrent branch activity; exact
  finding totals from those commands are treated as snapshots, not acceptance results.
- macOS-local tests cannot establish remote Linux/SSH process-tree cleanup.
- Static complexity, LOC, churn, mutex counts, and suppression counts are prioritization inputs, never
  standalone defect proof.
- The plan identifies remediation ownership and sequencing but deliberately does not design or implement
  the fixes.

## Deliverables

1. This plan with a scored hotspot register and staged remediation roadmap.
2. A machine-reproducible baseline appendix containing the commands and tool versions used.
3. A lint/exclusion register: keep, tighten, replace, or remove, with reasons.
4. A coverage/test-gap register that distinguishes unit gaps from missing production-path proofs.
5. A parallelization map separating high-risk serialized work from file-disjoint improvement batches.

## Done means

- all production Go packages were included in the quantitative inventory;
- repository-wide concurrency candidates were scanned and the highest-density/impact ownership clusters
  were manually reviewed; the audit does not claim an exhaustive proof of every primitive site;
- the worst measured complexity symbols were manually inspected;
- lint configuration exclusions, directive totals, dominant directive classes, and all explicit production
  complexity suppressions were accounted for; individual trusted-path `gosec` directives remain a future
  classification batch;
- coverage was measured at package level and the riskiest low-coverage symbols were identified;
- at least one independent audit pass covered every major runtime boundary;
- an adversarial review challenged the top-ranked findings and the scoring model;
- the plan names a finite, prioritized set of workstreams without changing production code.

## Work log

- **2026-07-24:** audit opened; current P2 progress and quality-audit documents reconciled; initial scoring
  model established; first independent review wave launched for concurrency/state, complexity/lint, and
  coverage/testability.
- **2026-07-24:** first quantitative baseline captured (package/file size, cognitive complexity, 90-day
  churn, suppression count, and test-presence inventory). The baseline exposed strong overlap among size,
  complexity, churn, and orchestration-critical code in the daemon run path.
- **2026-07-24:** independent concurrency audit found concrete supervisor stop/state defects, unbounded
  eventbus delivery, Codex close/lifecycle problems, tmux shared-state risk, and mutable-global seams.
- **2026-07-24:** process-lifecycle pass found remote process-tree leaks, cleanup using canceled contexts,
  immediate-child-only handler cancellation, destructive git probes, and ambiguous detached-process
  ownership.
- **2026-07-24:** complexity/lint pass measured the worst symbols, suppressions, exclusions, and duplicates;
  architecture pass identified temporal service-location, partial run bundles, split composition, and
  production/test graph divergence.
- **2026-07-24:** coverage/testability pass separated line coverage from production-path proof; durable-state
  pass found schedule, branch-tip, replay, sessiondata, and queue transaction/corruption gaps.
- **2026-07-24:** adversarial pass downgraded CLI-router/core-size/registry/suppression-count findings,
  promoted lifecycle/eventbus/queue/workspace/handler risks, and checked for omitted subsystems.
- **2026-07-24:** integrated the live `HANDOFF-alpha` LIFT plan with the audit findings. Added a serial
  LIFT spine, preemptive restart-P0 lane, independent safety lanes, two-builder cap, conflict matrix,
  takeover checklist, and isolated-worktree runbook while preserving the in-flight L0 worktree.
