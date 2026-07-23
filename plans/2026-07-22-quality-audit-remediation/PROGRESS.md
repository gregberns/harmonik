# Quality-audit remediation — live progress and ownership

Last updated: 2026-07-22 10:00, post-P2 quality-suite enablement wave in progress.

This file is the coordination surface for quality work running alongside P2
extraction. P2's own `PROGRESS.md` remains authoritative for its files.

## Status

| Lane | Scope | Status | Ownership while active |
|---|---|---|---|
| Q1 | `internal/codexwire/**` | complete, independent review passed | released |
| Q2 | `internal/agentmanifest/**` + `cmd/harmonik/agent.go` call site | complete, independent review passed | released |
| Q3 | `internal/hookrelay/**`, `internal/sessiondata/**`, `internal/supervise/**` | complete, independent re-review passed | released |
| Q4 | formatting/typecheck/full-lint boundaries and `cmd/**` coverage-ratchet design | implementation complete; Makefile wiring deferred until P2 releases it | released |
| Q5 | make ZeroMQ research programs explicitly non-buildable | complete, independent review passed | released |
| Q6 | wire Git-aware formatter into both Makefile paths | complete, independent review passed | released |
| Q7 | honest `cmd/**` coverage ratchet implementation and measurement | implementation complete, independent re-review passed; baseline blocked by two active cmd test failures | released |
| Q8 | read-only whole-tree quality measurement | complete | released |
| Q9 | isolate quality commands from concurrent Go-cache invalidation | final failure-propagation fix in progress | Q9 agent |
| Q10 | remove depguard phantom noise and tighten measured harness fences | complete, independent review passed | released |
| Q11 | newly released daemon lint correctness/suppression cleanup | complete, independent re-review passed | released |

## Already completed before this wave

- Removed 76 provably unused `//nolint:gosec` directives across 44 files.
- Fixed lifecycle/supervise tmux and reconciliation-lock swallowed errors with tests.
- Added `make lint-full-count`; it fails explicitly when typechecking prevents an
  honest full count.
- Rejected and backed out an unconditional 90% `cmd/**` coverage floor because it
  would make the complete suite permanently red.

## Hard no-touch set

- `.golangci.yml`
- `internal/daemon/**`
- `internal/harness/**`
- `internal/transport/**`
- `internal/queuewiring/**`
- `internal/crewrun/**`
- `plans/2026-07-21-p2-extraction/**`

## Current suite blockers not owned by these lanes

- Untracked ZeroMQ experiment `.go` files break full-tree typechecking.
- Available disk is below the daemon test watermark, manufacturing timing failures.
- Active P2 boundary assertions may be transiently red while extraction slices are
  uncommitted.

## Return log

Append each lane's before/after counts, files, tests, and deferred findings here.

- Q1 baseline: 87 findings (`revive` 74, `errcheck` 7, six other).
- Q2 baseline: 129 findings (`errcheck` 57, `gosec` 62, ten other).
  Renderer `errcheck` debt requires returning writer failures; Q2 may edit the
  clean production call site `cmd/harmonik/agent.go` and package tests.
- Q3 prior combined baseline: 233 across hookrelay/sessiondata/supervise; the
  lane is producing per-package counts before reporting completion.
- Q1 complete: 87 to 0 findings. Edited only `internal/codexwire/codexwire.go`
  and its test. Package tests, scoped lint, diff-check, and UBS pass. Work includes
  exhaustive frame handling, checked writer/marshal errors, and decomposition of
  request/notification parsing; semantic review remains pending.
- Q4 complete: added `scripts/go-format.sh` plus an isolated test. The selector
  uses Git's tracked + non-ignored-untracked file set, so real new Go files stay
  gated while ignored managed worktrees no longer poison formatting. Makefile
  wiring is deferred because P2 is editing that file. The selector correctly
  exposes five non-ignored ZeroMQ experiment programs as the current typecheck
  blocker. Command coverage design is a strict package-complete baseline ratchet
  with no 90% absolute floor; measurement and ratification wait for a clean
  post-P2 tree.
- Q2 complete: 129 to 0 scoped findings. All 57 `errcheck` findings now have
  explicit semantics: markdown/toon renderers retain the first writer failure,
  stop subsequent writes, return it, and the CLI reports a failed render. A
  failing-writer regression test covers both renderers. The YAML parse error now
  wraps both `ErrInvalid` and the underlying parser error. All 62 `gosec`
  findings were test fixtures with unnecessarily broad literal permissions;
  fixture directories/files now use `0700`/`0600` rather than suppressions.
  Remaining correctness/style/complexity findings were fixed with test-helper
  cleanup and small renderer/check decompositions. `go test
  ./internal/agentmanifest`, focused `cmd/harmonik` tests, scoped uncapped lint,
  and diff-check pass. UBS's only critical reports are false-positive web-XSS
  taint claims against terminal stderr output in `cmd/harmonik/agent.go`; no
  Q2 findings are deferred. Semantic integration review remains pending.

- Q1 independent review passed across codexwire and its production consumers;
  no protocol or JSON round-trip regression found.
- Q2 independent review passed. All renderer call sites remain compatible,
  first writer errors and partial-output semantics are correct, CLI failure is
  observable for all formats, and permission changes are test-only.
- Q3 review found two composition-root issues now being fixed: the reaper's sole
  consumer falsely counted/emitted success after unexpected `KillSession`
  failures, and watchdog liveness incorrectly depended on local connection
  close success after a successful dial. Hookrelay changes otherwise passed.
- Q3 review fixes complete and independently re-reviewed. Unexpected tmux kill
  errors now return a partial result without a false success event; missing-session
  races remain idempotent. Successful watchdog dial is authoritative even if
  closing the local probe fails. Full supervise/hookrelay tests pass, including
  the supervise race run. Final scoped counts: hookrelay 120 to 91 and supervise
  91 to 41; remaining production debt is five complexity findings plus one
  non-security jitter finding, with the rest test-only.

## Integration state

- Q1/Q2 scoped lint is zero and independent reviews passed.
- Q3 targeted production correctness debt is cleared; remaining findings are
  explicitly classified above.
- Q4 script and isolated tests pass. Makefile wiring is deliberately deferred
  until P2 releases its concurrently edited Makefile.
- The formatter correctly remains red on the five non-ignored ZeroMQ experiment
  programs. Their owner must add build-ignore markers, relocate them, or formally
  ignore the directory; the quality gate must not hide them.
- No files are staged or committed because P2 is actively using the shared Git
  index for explicit-path slice commits. All quality ownership is recorded here.

## Post-P2 reassessment

- P2 Phase 3 and its punch list are complete: nine slices landed, six extracted
  leaf packages are daemon-free, and `internal/daemon` dropped 6,393 LOC.
- Disk is healthy again at roughly 22 GiB free, above the daemon's 10 GiB
  watermark. Serialized differential testing is meaningful again.
- A separate runmerge extraction is currently editing `.golangci.yml`, the
  Makefile freeze-gate sections, `internal/daemon/**`, and `internal/runmerge/**`.
  This quality wave avoids those areas except for isolated formatter recipe
  hunks in Makefile.
- The immediate suite blockers are the five non-ignored ZeroMQ research
  executables, missing formatter wiring, and absence of a ratified command
  coverage baseline ratchet.

- Q5 complete: all five preserved ZeroMQ experiment programs now carry modern
  `//go:build ignore` constraints. `go list ./...` and the Git-aware formatter
  both pass package discovery; research logic is unchanged apart from required
  import grouping.
- Q6 complete: both `make fmt` and `make fmt-check` now delegate to the tested
  Git-aware selector, while concurrent runmerge freeze-gate hunks remain intact.
  The isolated formatter test, real `make fmt-check`, dry runs, and diff-check pass.
- Q5/Q6 independent review passed with no correctness findings. A low-priority
  future test can explicitly inject formatter-tool failures; current `set -e`
  behavior is already fail-closed.
- Q7 implementation complete in standalone script/test/baseline files. It
  discovers every production command package, requires exact baseline membership,
  measures fresh atomic coverage, and fails at a regression of 0.3pp or more,
  without imposing an impossible absolute floor. Baseline ratification correctly
  refuses to proceed while two `cmd/harmonik` P2 boundary tests remain red.
- Q7 independent review found and fixed atomic-baseline replacement and missing
  failure-path coverage. Same-directory rename, cleanup, numeric/range validation,
  and the expanded failure/boundary suite now pass re-review.
- Q8 measurement: `go list ./...`, real `make fmt-check`, `go build ./...`, and
  isolated-cache `go vet ./...` pass. Full uncapped lint now reaches analyzers:
  6,311 findings and zero typecheck failures. An ordinary shared-cache run can
  still fail spuriously when concurrent agents invalidate Go cache facts, so Q9
  owns cache isolation for the measurement path.
- Q9 complete: `lint-full-count` now runs under a tested disposable private
  `GOCACHE`. A real run completes with 6,310 findings and no typecheck/cache-fact
  failure, while preserving exact exit status and cleaning after success/failure.
  Broader public check targets should eventually wrap one recursive internal
  target per suite, not each Go command separately, to avoid repeated cold builds.
- Corrected the malformed supervisor `nolint:gocritic` syntax. One unknown
  `goerr113` directive remains in the actively edited daemon tree and is deferred.
- Runmerge extraction has now landed (`18ca67c7`), releasing `.golangci.yml` and
  the named daemon lint targets. Q10/Q11 are addressing the audit's deferred
  depguard noise, two daemon `nilerr` sites, complexity invisibility, and the
  unknown `goerr113` directive without entering the hard refactors.
- Q10 complete and reviewed: queue/core phantom depguard findings are gone,
  measured harness fences are tighter, four stale source suppressions were
  removed, and all extracted/runmerge closures remain daemon-free. The four
  real core YAML/Expr boundary breaches remain visible.
- Q11 complete and re-reviewed: both daemon `nilerr` sites now preserve intended
  semantics, DOT remote transport failures reach the production caller instead
  of masquerading as no-verdict retries, complexity debt is visible again, and
  the unknown `goerr113` directive is gone.
- Separate blocker discovered by Q11 review: the current
  `conformance_m4c7_test.go` AST security guard is boolean-semantics bypassable
  despite passing its own test. It belongs to unrelated concurrent work and was
  not edited in Q11.
- Q9's concurrency review added parallel-runner and signal hardening. A real
  timeout then exposed one final target bug: `lint-full-count` could let `jq`
  mask a nonzero linter exit. A regression-backed fix is in progress.
- Q9 final hardening and independent review are complete. Parallel lint runs,
  nonzero linter-status preservation, signal forwarding/reaping, and private
  cache cleanup are all regression-tested. The four standalone quality-script
  suites pass together (`go-format`, isolated cache, lint concurrency, command
  coverage gate).
- Q12 complete and structurally reviewed: the D2 remote-API-key refusal is now a
  typed production helper immediately before the unique launch, after all final
  environment mutations. The AST conformance test rejects decoys, weakened
  predicates, shadowing, missing returns, reordered/duplicate/nested launches,
  and late environment mutation. Focused runtime validation was re-attempted
  after the worker move restored package discovery; exact result is pending.
- The two-line `cmd/harmonik/substrate_select.go` boundary fix passed independent
  semantic review and focused boundary/router/spawn-seam tests. Codex-driver now
  reports and enforces the same fail-closed isolation requirement; tmux and
  healthy SSH routing are unchanged.
- Latest P2 check-in: runmerge is landed, while worker-registry bootwire extraction
  remains uncommitted in `internal/workers/**`. `go list ./...` succeeds, but real
  `make fmt-check` briefly failed on the moved owned file
  `internal/workers/bootwire_hkqmyis_test.go`; its owner subsequently formatted
  it and `make fmt-check` now passes. Quality work did not edit that active slice.
- Fresh-cache `go vet ./...`, package discovery, formatting, and compile-only
  daemon/CLI checks pass. While the worker slice was active, its full package test
  showed one load-sensitive `TestRunReportLoop_FastWhenInFlight` cadence failure;
  see the later post-landing recheck below.
- Command coverage baseline ratification remains correctly blocked. The ordinary
  full `cmd/harmonik` suite passed, but atomic coverage deterministically exposes
  `TestHarnessSH033_DoubleSIGINTHardExit`: its preloaded two-signal setup races
  with its own graceful handler and emits a SuiteResult (10/10 focused failures).
  The coverage gate refused to update the baseline. A deterministic SH-033 test
  repair is now isolated to `cmd/harmonik/harness_sh033_test.go`.
- Command coverage baseline is now ratified and enforced. The SH-033 test uses
  real SIGINT delivery with readiness and graceful-write barriers, so the second
  signal provably exercises the hard-exit path under ordinary, race, and atomic
  coverage instrumentation. The complete fresh measurement wrote exact entries
  for all eight production `cmd/**` packages; an immediate second fresh run held
  every baseline (the 0.2pp cmd/harmonik sampling variation remained below the
  specified 0.3pp regression threshold). Independent review approved the signal
  semantics and requested only failure-path subprocess cleanup hygiene, now being
  addressed before final handoff.
- P2 worker bootwire extraction landed as `646748f3`. A fresh isolated
  `go test ./internal/workers -count=1` now passes, so the earlier cadence failure
  is not presently reproducible and the ownership collision is released.
- SH-033 failure-path cleanup is complete: a single waiter owns `cmd.Wait`, and
  every marker/signal/timeout failure kills and synchronously reaps the child.
  Ordinary x10, atomic coverage x10, and race x5 all pass; formatting and diff
  checks are clean.
- The current complete uncapped lint measurement succeeds through real analyzers
  with **6,261 findings**, down from the earlier 6,310 measurement, and without
  typecheck/cache-fact failure. This is an honest debt count, not a green lint
  claim; scoped cleanup can now be prioritized against a reliable runner.

### Q13 — parallel released-package lint wave

- Baseline: 6,261 full-tree findings. Ownership is directory-exclusive and avoids
  every already-dirty package: `internal/brcli/**`, `internal/eventbus/**`, and
  `internal/scenario/**` are independent lanes. Each lane prioritizes production
  correctness/error handling, then safe test mechanics such as Go 1.22 loop-copy
  cleanup; complexity rewrites and blanket suppressions are out of scope.
- Each lane must run package tests, scoped uncapped lint before/after, formatting,
  diff hygiene, and record any finding deliberately left behind. Root owns final
  integration, full-count remeasurement, and review.
- `internal/brcli/**` complete: **101 → 0**. Production cleanup now preserves
  intent-log cleanup/close failures, surfaces directory-fsync close errors, and
  retains `BrError` identity through wrapping; safe test loop/suppression debt is
  cleared. Package tests, zero-finding scoped lint, diff hygiene, and UBS pass.
- Root `internal/digest/**` lane: **66 → 29** so far. Production exhaustive-switch,
  shadowing, stale suppressions, close-error propagation, preallocation, package
  comment, and safe signature findings are cleared; fixture permissions are now
  private. Remaining production findings are deliberately larger complexity or
  public API naming concerns; remaining mechanical test findings are being triaged.
- The released worker is continuing with exclusive `internal/handler/**` ownership
  while eventbus and scenario lanes remain active.
- `internal/eventbus/**` complete: **75 → 16**. All 48 error-check findings are
  cleared; production dead-letter failures are observable, read/OS close errors
  propagate correctly, and safe test mechanics are fixed. Full tests and focused
  race tests pass. Residuals are 11 complexity findings, two intentional panic
  tests, and three test-only variable-path security heuristics.
- `internal/scenario/**` integration-corrected result: **111 → 52**. The first
  worker report incorrectly claimed zero and briefly referenced a helper across
  incompatible test packages; root validation caught the compile failure. The
  helper is now package-correct, tests and diff checks pass, and the authoritative
  JSON residual is recorded (mostly gosec/revive plus two nilerr findings).
- `internal/release/**` complete: **21 → 1** after correcting an initially
  non-authoritative measurement. Temp cleanup/close/rename failures are preserved,
  test errors are checked, and package tests pass. The sole residual is the public
  API rename `release.ReleaseEntry`, deliberately deferred.
- `internal/handler/**` independently confirms **0** scoped findings and passing
  tests; the earlier directory-prefix inventory grouped handlercontract debt into
  the apparent handler total.
- Root `internal/digest/**` lane final safe-wave result: **66 → 21**. Package tests
  and diff checks pass. Remaining production debt is limited to two complexity
  findings, one function-length finding, and the public `DigestJSON` API stutter;
  the rest is test-only error/security cleanup.
- Q13 integrated validation: combined tests for brcli, eventbus, scenario, release,
  and digest pass; real formatter and diff hygiene pass. The first full count was
  correctly rejected during a concurrent partial edit in `internal/supervise`
  (undefined helper/unused import); once that external edit completed, `go build
  ./...` passed and the authoritative isolated count completed at **6,013**.
  This wave therefore removes **248 net full-tree findings** from the 6,261
  baseline despite concurrent unrelated edits elsewhere in the shared tree.

### Q14 — second parallel breadth wave

- Authoritative starting full-tree count: 6,013. Exclusive lanes are
  `internal/structuredlog/**`, `internal/hooksystem/**`, and
  `internal/sentinel/**`; every worker was given the exact uncapped JSON command
  after Q13 exposed how a non-authoritative invocation can falsely report zero.
- Root measured `internal/t5probe/**` separately at 45 findings, all test-only
  unchecked errors in one probe file. It is queued as a follow-on mechanical lane
  rather than overlapping the three active workers.
- `internal/structuredlog/**` complete: **44 → 0**. Production logging now passes
  the caller context to `Enabled`; checked cleanup/assertions and context-aware
  test logging clear the remainder. Package tests and root integration pass.
- `internal/hooksystem/**` complete: **47 → 8**. All 30 unchecked errors and six
  nil-error test defects are cleared; hook-failure emission remains observer-safe
  while verdict mismatch emission failures propagate. Residuals are one public
  constructor panic, two commented test blocks, and five helper signatures.
- `internal/sentinel/**` complete safe wave: **54 → 32**. Production append-close
  errors, private state permissions, stale suppressions, and a duplicated governor
  branch are fixed. Residuals are dominated by complexity/exhaustive switches and
  test-only security/context findings. Root combined tests/format/diff pass.
- Follow-on lanes are active for `internal/t5probe/**`, `internal/workers/**`, and
  `internal/codextest/**` with exclusive ownership and the exact JSON contract.
- Follow-on results: `t5probe` **45 → 0**, `workers` **37 → 20**, `codextest`
  **28 → 22**, `watch` **17 → 6**, `cognition` **11 → 0**, and `presence`
  **14 → 3**. Safe unchecked-error, context, permission, stale-suppression, and
  mechanical findings are cleared; residuals are explicitly complexity, public
  naming/panic contracts, test-only bounded-path heuristics, or live-harness work.
- Q14 integrated validation passes across all nine edited packages, real
  `fmt-check`, diff hygiene, and `go build ./...`. The authoritative isolated
  full-tree count is now **5,791**, a further **222 net reduction** from 6,013.
  Across Q13+Q14, the tree moved **6,261 → 5,791 (470 net findings removed)**
  while keeping typecheck failure fail-closed and recording intentional residuals.

### Q15 — continued non-daemon breadth

- `internal/daemon/**` is explicitly excluded per operator direction. Exclusive
  lanes: queue **208 → 206**, workflow **155 → 152**, transport **18 → 15**,
  schedule **10 → 4**, and eval CLI **22 → 17**. The small numerical reductions
  represent production error-identity, direct-push/fetch multi-error, safe network
  context, mutation assertion, and parser-state fixes; large API/complexity/writer
  contracts remain visible rather than suppressed.
- Root `internal/harness/**`: **110 → 88**. Safe Go 1.22 loop copies, Getwd error
  propagation, private model/WAL fixture permissions, stale suppression, and small
  static style findings are cleared. Claude/Pi suites and focused Codex billing,
  JSONL, and WAL tests pass. The full Codex package still has the known ambient
  operator-config failure in `TestCodexHarness_LaunchSpec_CustomBinary` and is not
  misreported as green.
- `internal/handlercontract/**` remains at 119 after two delegated assessments
  declined broad edits; it is recorded for a future explicitly mechanical sweep.
- New active lanes: core, keeper, and a bounded workspace mechanical subset
  (Go 1.22 loop copies plus private test-fixture permissions). Workspace's exact
  baseline is 486, not the stale 487 prefix estimate.
- Core completed in two passes: **338 → 109**, including 219 obsolete Go 1.22
  loop copies plus production HWM/error-identity fixes. Root caught and repaired
  stricter `gofumpt` whitespace left by the mechanical removals; core tests pass.
- Keeper completed: **140 → 88** after production close/error propagation and 55
  private test-fixture permission fixes. Codexreactor completed **13 → 4**.
- Workspace remains **486** because the delegated bounded worker declined the
  mechanical sweep without editing; no reduction is claimed.
- Q15 integrated non-daemon validation passes: package tests, `fmt-check`, diff
  hygiene, and `go build ./...`. Authoritative full-tree lint is now **5,459**,
  down **332** from Q14's 5,791 and **802 net** from the 6,261 starting point.

### Q16 — command twin breadth (daemon excluded)

- Claude twin **133 → 125**, generic twin **55 → 53**, and Codex twin **30 → 26**.
  Changes cover Go 1.22 loop copies, context-bound git commands, socket-close and
  version-writer propagation; package tests pass. Residuals are predominantly
  test writer/error mechanics and complexity.
- Root session twin **9 → 3**: best-effort hooks now have five-second command
  contexts, stale suppression and unsafe test assertion/style findings are gone.
  The three residuals are controlled JSON marshal/path heuristics; package tests
  and exact scoped lint validation pass.
- Q16 integration passes and the full-tree count is **5,439**.

### Q17 — small-package sweep (daemon excluded)

- `internal/testhelpers` **19 → 6**, `internal/usage` **8 → 4**,
  `internal/apptap` **7 → 3**, and root `internal/branching` **7 → 2**.
  Production close/stat/discovery errors now propagate, context-aware commands and
  private fixtures are in place, and safe loop/style/signature debt is cleared.
  Package tests and exact scoped lint validation pass; residuals are complexity,
  bounded path heuristics, or test helper/API concerns.
- Q17 integration passes at **5,417** full-tree findings.

### Q18 — tiny-package closure (daemon excluded)

- `internal/crewrun` **5 → 0** and `internal/run` **5 → 0**. Queuewiring's
  three mechanical findings are cleared (**4 → 1**, leaving complexity only).
  Dashboard's stale suppression is removed (**2 → 1**, leaving its public API
  naming concern); goalstate's sole bounded path heuristic remains.
- Package tests, format, diff, build, and isolated lint count pass. Full-tree
  findings are now **5,400**, or **861 net removed** from the 6,261 baseline.
- Follow-on tiny lanes: `internal/specaudit` **5 → 0**,
  `internal/scratchpad/**` **4 → 0**, and `internal/codexdigitaltwin` **2 → 1**
  (complexity only). Their scoped tests/lint/format checks pass.
- A new global integration/count attempt is temporarily blocked by concurrent
  `internal/daemon/workloop.go` work referencing missing `artifactAgentType` and
  `beadAlreadySubsumedInMain`. Per operator direction, this stream does not edit
  daemon. The last authoritative full-tree count remains **5,400** until that
  external partial edit compiles; scoped reductions above are not folded into a
  speculative global number.

### Q19 — current 3,974 baseline and path toward ~1,000

- Shared daemon work settled long enough for build, formatter, and an authoritative
  isolated count: **3,974** findings. Distribution is daemon 1,633; main CLI
  1,406; all other packages 935. Therefore the operator's earlier daemon exclusion
  creates a mathematical floor of 1,633; reaching ~1,000 eventually requires
  permission to enter daemon after its active work settles.
- Root core **109 → 93**, digest **18 → 8**, sessiondata **22 → 16**. Workspace
  scoped lint is **126 → 121**, but its first full validation exposed a regression
  in the delegated cleanup: seven advisory-lock `defer Close` calls had been
  deleted while removing suppressions, leaking locks and causing long trust-test
  timeouts. Root restored all production/test lock releases; focused hk-qx065,
  hk-bfvby, and concurrent-write tests now pass.
- Scenario, queue, brcli, eventbus, and lifecycle received additional safe scoped
  reductions, but global recount is currently blocked again by concurrent daemon
  clock-port signature work and active main-CLI version/captain changes. No edits
  from this stream enter either active area.

### Q3 — small dense packages

- 2026-07-22: active ownership claimed for `internal/hookrelay/**` and
  `internal/supervise/**`. Existing quality-stream edits in `internal/supervise`
  and the comment-only `internal/sessiondata/sessiondata.go` edit will be
  preserved. Baseline measurement and correctness/context triage are in progress.
- 2026-07-22: implementation complete. `internal/hookrelay` moved from 120 to
  91 scoped findings; all 16 production `errcheck` findings plus eight obsolete
  loop-variable copies and four safe static/style findings were cleared.
  `internal/supervise` moved from 91 to 41 scoped findings; all production
  `errcheck`, `errorlint`, `noctx`, `gocritic`, `prealloc`, `revive`, and
  `unparam` findings were cleared. Detached-process commands retain explicit,
  justified `noctx` exceptions because cancellation must not kill the revived
  process. Added focused tmux-list/kill error tests. Remaining production debt
  is five complexity findings and one non-security jitter `gosec` finding;
  remaining findings are test-only cleanup. Package tests pass.

### Wave committed — Q13 through Q18 drained to disk

- The entire wave's working-tree state has been reviewed and committed as **18
  commits** spanning roughly 25 packages: brcli, scenario, eventbus, sentinel,
  hooksystem, digest, workers, watch, structuredlog, release, core, keeper,
  harness, codexreactor, the four `cmd/harmonik-twin-*` packages, and a long tail
  of small packages (t5probe, codextest, cognition, presence). Every commit
  carries an independent agent-reviewer verdict trailer and passing package tests.
- The reviews were not rubber stamps; two real defects were caught before commit.
  In `internal/release/lastgood.go` the cleanup rewrite had dropped the
  `defer in.Close()` and replaced it on only some paths, so the source file handle
  leaked whenever creating the temp file failed — fixed in the same commit
  (`3a41676d`). In `internal/sentinel` the governor de-duplication left the
  low-threshold accessor with no callers, and the orphaned dead accessor was
  removed rather than left for the dead-code linter (`864991a2`).
- The Q18 "blocked on concurrent daemon work" note is **resolved**. The daemon
  tree referencing missing `artifactAgentType` / `beadAlreadySubsumedInMain`
  compiles again as of `efeeb047`, which moved both into
  `internal/harness/shared`. A fresh authoritative full-tree count is therefore
  unblocked; the last recorded number remains **5,400** until one is taken.

### Q20 — non-daemon breadth continuation

- `cmd/harmonik-twin-claude` moved from **125 → 4** scoped findings. The
  remaining four are production complexity/length findings.
  Unsafe JSON assertions, unchecked fixture operations, stale suppressions, and
  version-output error handling were corrected; focused package tests pass.
- `cmd/harmonik-twin-generic` moved from **53 → 44** in the first pass, with a
  second test-only unchecked-operation pass completing at **0**. `internal/workers`
  is down to eight intentional/structural findings. Queue's four stale
  directives are removed, with its remaining safe helper/comment cleanup in
  progress.
- Digest is **18 → 4**, now production complexity/API naming only. Sessiondata
  test-security findings are cleared, and sentinel's remaining test-controlled
  command/path findings are being drained file by file.
- Global recount is still gated by concurrent daemon and main-CLI edits. This
  stream continues to avoid both collision zones and uses isolated package
  lint/test runs to make reductions measurable meanwhile.

### Q21 — authoritative recount and remaining floor

- A stable-tree isolated full lint completed at **3,204**, down **770** from the
  prior 3,974 baseline. Current concentration: daemon 1,539; main CLI 1,088;
  every other package combined 577.
- The earlier daemon exclusion therefore still creates a hard floor above the
  requested ~1,000. Even eliminating every non-daemon finding would leave
  1,539. Work continues in clean main-CLI files and test-only workspace lanes
  while the P2 extraction agent owns daemon.
- Twin results are now Claude **125 → 4**, generic **53 → 0**, Codex **26 → 0**;
  digest is **18 → 4**. Workspace test-controlled subprocess/path findings and
  lifecycle stale directives are being removed in bounded, non-overlapping
  files with focused tests.
- The subsequent bounded sweep completed at **3,020** authoritative findings,
  another **184 removed** in this wave and **954 removed** from the 3,974
  baseline. Workspace is **120 → 9** with its full package suite green; harness
  is **44 → 19**, lifecycle **34 → 22**, BRCLI **22 → 14**, and core **91 → 72**.
  Core plus all harness package tests pass, as do focused lifecycle/BRCLI checks.
- Final concentration is daemon **1,539**, main CLI **1,072**, and all remaining
  packages **409**. Reaching ~1,000 is impossible without entering daemon:
  daemon alone exceeds the target by 539.

### Q22 — main CLI safe-finding sweep and lint-cache audit

- A first full-tree report appeared to be **2,255**, but cache validation proved
  that number was not authoritative: golangci-lint replayed old findings after
  source changes, and combining `--output.text.path=/dev/null` with JSON output
  left the old JSON file untouched. That provisional count is withdrawn.
- With a genuinely fresh `GOLANGCI_LINT_CACHE`, broad `./...` and main-CLI runs
  currently exit zero before writing either requested report, while a bounded
  package run works and reports current findings. The monolithic quality command
  is therefore fail-open and cannot presently produce a trustworthy total.
  Cached output still says 3,020 and demonstrably points at lines that are now
  checked, so it must not be used as the current count.
- Main CLI fell from **1,072 → 315** through checked output/error propagation,
  context-bound subprocesses, focused security rationale, and test-helper
  cleanup in the last usable scoped report; additional safe findings were fixed
  after that checkpoint. The complete `cmd/harmonik/supervise` safe-finding sweep is done;
  its remaining findings are structural complexity only. Isolated CLI and
  supervise compile/tests pass, and the full repository builds.
- The last usable (now stale) concentration was daemon **1,539**, main CLI **315**, core **72**,
  scenario **33**, evaltasks **27**, queue **26**, lifecycle **22**, keeper and
  harness **19 each**. Its non-daemon subtotal was **716**; subsequent bounded
  package sweeps have removed more findings, but no false-precision replacement
  total is recorded until the fresh-cache full runner is fixed.
- The daemon exclusion remains the mathematical floor: even removing all 716
  editable findings would leave 1,539. Work therefore continues across core
  and the smaller packages while the P2 agent retains daemon ownership.

### Q23 — trustworthy runner and second breadth reduction

- Root cause of the apparently fail-open fresh-cache runs was operational:
  `with-isolated-gocache.sh` starts its child asynchronously, and the execution
  session yielded while several analyzer processes remained live. The redundant
  exact lint PIDs were terminated, then one direct fresh-cache run was monitored
  through real process completion.
- The first trustworthy fresh-cache report was **2,180**: daemon **1,539** and
  all non-daemon packages **641**. After clearing the remaining safe findings in
  the largest CLI files and another broad small-package wave, the second
  monitored report is **1,979**: daemon **1,539**, non-daemon **440**.
- This wave cleared safe findings from `init_cmd.go`,
  `keeper_enable_doctor_cmd.go`, `handler.go`, and `harness.go`; completed the
  supervise safe-finding sweep; and drained bounded findings across core,
  scenario, lifecycle, transport, sessiondata, runmerge, keeper, harness,
  testhelpers, codextest, sentinel, workflow, eval tasks, and BRCLI.
- The repository builds, focused affected-package tests pass, formatting and
  diff hygiene pass. Further explicit errchecks are being drained after the
  1,979 checkpoint. Remaining non-daemon debt is dominated by structural
  complexity and architectural naming/forbidigo findings rather than unchecked
  operations.
- A final monitored fresh-cache recount after that drain is **1,948**: daemon
  **1,539**, all non-daemon packages **409**. Non-daemon `errcheck` is now
  **zero**; all 544 remaining unchecked-error findings are in the excluded
  daemon tree. This is a verified **2,026-finding reduction** from the reliable
  3,974 baseline.
