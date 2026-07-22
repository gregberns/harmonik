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
