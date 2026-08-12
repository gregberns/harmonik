# Testing Strategy

> Concrete Go practice for the 5-layer methodology in `docs/methodology/TESTING.md`. Scoped to solo-dev, agent-coded. Go 1.25 toolchain (per `quality-checks.md`).
>
> **The library list is held by a linter, not by taste.** `tools/forbid-import` fails `make full` on any test import outside the allowlist, so nothing reaches the tree by drifting in. To add a library, edit the allowlist and this doc in the same commit and say in the commit body what the new library does that the current list cannot. That edit is visible and a reviewer reads it — which is the whole mechanism. The list exists so there is one way to do each thing, not because the choices are beyond discussion.

## Decisions

1. **Test runner:** stdlib `testing`, always. No Ginkgo, no testify/suite.
2. **Assertions:** `github.com/stretchr/testify/require` only. `require` (not `assert`) so a failed precondition stops the test. Banned: testify/suite, testify/mock.
3. **Property testing:** `pgregory.net/rapid`. Chosen over `testing/quick` (too thin) and `gopter` (abandoned). Used where an invariant is easier to state than a table of cases.
4. **Mocking:** hand-written fakes in `internal/<pkg>/faketest/`. No `gomock`, no `mockery`. Rationale: hand-written fakes are ~20 lines and an agent can read them; generated mocks hide behavior.
5. **Golden files:** `gotest.tools/v3/golden`. Updated with `go test ./... -update`.
6. **Subprocess orchestration in tests:** stdlib `os/exec` wrapped by a thin `internal/proctest` helper. No external test-harness libraries.
7. **Coverage tool:** `go test -cover` + `go tool cover`. Thresholds are enforced by `scripts/coverage-gate.sh`, which runs from `make coverage-gates` — **not from CI**, which runs `make full`. The ratchet is a trend measure and does not block a merge.
8. **Race detector:** `-race` on the scenario and integration tiers, and on the nightly lane (`make test-race-nightly`). `make full` runs the unit tier without it, so the nightly lane is the only place a data race in unit-tested code surfaces. Treat a flake that appears only under load as a reason to run that lane early rather than to retry.
9. **Build tags:** `//go:build integration` / `scenario` / `crash` / `nightly`. Default `go test ./...` runs unit + property only. CI explicitly selects tiers.
10. **Naming:** `TestXxx` for unit, `TestIntegration_Xxx` for integration, `TestScenario_Xxx` for scenario, `TestCrash_Xxx` for crash-recovery, `TestProp_Xxx` for rapid properties.

## The 5 layers in Go practice

### 1. Unit — `internal/<pkg>/*_test.go`
Tests one package; no fs beyond `t.TempDir()`, no subprocess, no network. Library: stdlib + `require`. Example: `edge/cascade_test.go` asserts `Cascade(state, candidates)` picks the declared edge for a fixed input table.

### 2. Integration — `internal/<subsystem>/integration_test.go` with `//go:build integration`
One subsystem, real filesystem/git/SQLite allowed via `t.TempDir()`. External services stubbed by in-process fakes (e.g., `br` CLI replaced by a `brfake` binary built once in `TestMain`). Library: stdlib + `require` + `gotest.tools/v3/fs` for tree assertions. Example: `workspace/integration_test.go` creates a real worktree, leases it, merges it back, asserts the final ref graph.

### 3. Scenario — `test/scenario/*_test.go` with `//go:build scenario`
End-to-end via S07 harness. Harness entry point: `scenariotest.Run(t, "testdata/scenarios/golden-build.yaml")`. The harness: compiles `harmonik-daemon` and `claude-twin` once per `TestMain`, writes DOT workflow + twin script to a temp dir, launches daemon, asserts on JSONL events + final git state. Scenario YAML shape (locked here): `workflow: path/to.dot`, `twins: {builder: path/to/script.yaml}`, `asserts: {events: [...], git_head_trailers: {...}, files: {...}}`.

### 4. Crash-recovery — `test/crash/*_test.go` with `//go:build crash`
Built on the scenario harness + an `Interrupt(at: EventName)` primitive. See dedicated section below.

### 5. Property — `internal/<pkg>/*_prop_test.go`
Rapid generators for: edge-selection determinism, DOT cycle-detection, checkpoint-trailer round-trip, JSONL event schema round-trip, reconciliation category classifier (input: partial git + beads state; invariant: classifier is total and stable). Default: 100 iterations per push; `HARMONIK_NIGHTLY=1` bumps to 10 000 with a random seed.

**Sample / bootstrap file:** `internal/core/beadid_prop_test.go` (landed via hk-m084e). Demonstrates:
- `rapid.Check` harness call shape.
- `rapid.StringN(min, max, -1)` generator for variable-length opaque strings.
- Two `TestProp_*` invariants: string round-trip and equality symmetry.
- Correct `*_prop_test.go` file naming (default tags; runs with plain `go test ./...`).

**Fuzz seed corpora.** `go test -fuzz=Fuzz<Parser>` on the four boundary parsers (DOT workflow, YAML policy, JSONL event, commit trailer). A committed seed corpus lives at `testdata/fuzz/<parser>/` beside the test file — tiny, hand-curated inputs (including prior crashers once found). Every push runs each fuzz target in seed-only mode (`go test -run=FuzzX/seed` or equivalent) so regressions on known inputs are caught cheaply. Continuous fuzzing (long-running `-fuzz` on a schedule / OSS-Fuzz integration) remains deferred — see Deferred / follow-up.

## Libraries and tools

In: `testing`, `testify/require`, `pgregory.net/rapid`, `gotest.tools/v3/{golden,fs,icmd}`, stdlib `os/exec`, stdlib `testing/fstest`.
Out: testify/suite, testify/mock/assert, Ginkgo, Gomega, gomock, mockery, gopter, dockertest (we do not need containers; Beads is embedded SQLite).

Rationale for the tight list: one way to do each thing. `tools/forbid-import` holds it — `make full` runs `go run ./tools/forbid-import ./...` and fails on an import outside the list. An agent cannot add a second assertion library by accident, because the allowlist edit is a separate visible change a reviewer sees.

## Fixture and testdata conventions

- **Location.** `testdata/` lives beside the test file that uses it. Lean toward leaving it there: a fixture next to its test is read and changed with the test. Promote one to `test/fixtures/` when a third package needs it, because at that point the copies drift apart faster than the shared file gets in the way.
- **Formats.** DOT for workflows, YAML for policies + twin scripts, JSONL for expected event streams, `.golden` for blob comparisons.
- **Refresh.** `go test ./... -update` regenerates golden + expected-JSONL files. Refresh commits must diff the fixture and justify the change in the commit message.
- **Recorded real-agent fixtures.** `testdata/fixtures/claude-code/<version>/*.jsonl` holds captured wire output. Capture tool: `scripts/record-claude-fixture.sh` (nightly job writes new captures; human reviews before merge).
- **Determinism seeds.** Any test that uses `rand` reads its seed from `testdata/seed` (committed). Nightly overrides with `HARMONIK_RAPID_SEED=auto`.

## Coverage numbers — a regression alarm, not a target

**These numbers were endorsed as targets on 2026-04-24 and downgraded to a diagnostic on 2026-07-27. Read them as an alarm level, never as a goal.** Percentage targets reward volume, and volume is what this repo got: 488k lines of test code against 216k of production, including 720 test files named after bead IDs (199,899 lines, 38% of all test code) that largely re-tested the same behavior. That figure is the re-measurement of 2026-07-30 in `plans/2026-07-27-delete-and-rewrite/CHARTER.md`, which corrects an earlier over-count of 885 files and 255,664 lines. Nothing here blocks a merge — `scripts/coverage-gate.sh` is the single authority for these numbers, it runs from `make coverage-gates`, and `make full` does not call it.

The question to answer is **which real behavior is unprotected**, never what percent a package reports. A package at 95% whose tests assert nothing has failed the check that matters. Writing a test to move a number, rather than to catch a bug you can name, is the smell — when you notice it, go find the behavior with no test and cover that instead.

| Layer / package class | Alarm level (line) | Error-path diagnostic |
|---|---|---|
| Core subsystem packages (`internal/orchestrator`, `workspace`, `eventbus`, `handler`, `reconciler`) | 95% | every error return exercised |
| Boundary parsers (DOT, YAML policy, JSONL event, commit trailer) | 95% | every error return + malformed-input fuzz corpus |
| Handler adapters (`claude-code`, `claude-twin`) | 90% real, 100% twin | see handler-divergence below |
| Utility / glue packages | 85% | — |
| Overall repo | 90% | 0.3pp regression cap, reported by `make coverage-gates` |

A line marked `// unreachable: <why>` and covered by an assert-panic test counts as covered. That keeps the alarm from pushing anyone toward a bogus assertion on a line that cannot fire in practice.

## Handler-divergence testing

The claude-code and claude-twin handlers will drift. The strategy (three tiers, concretely):

**Tier A — Recorded-fixture tests (CI-mandatory, every push).** For each handler method that parses wire output (`parseOutputChunk`, `detectRateLimit`, `detectReady`, `parseExitCategory`), a table test feeds real captured bytes from `testdata/fixtures/claude-code/<version>/` and asserts the parsed `Session` event matches a golden struct. These tests do NOT launch the subprocess; they exercise parsing only. Coverage target: 100% of known wire-format variants at the pinned claude-code version.

**Tier B — Twin-driven scenario tests (CI-mandatory, every push).** Every scenario in `test/scenario/` runs against `claude-twin`. This exercises the daemon ↔ handler wiring end-to-end without tokens. Divergence from the twin's *contract* (not real claude-code) is caught here.

**Tier C — Budget-capped real-agent smoke (nightly, not per-commit).** `test/realagent/*_test.go` with `//go:build realagent`. 5–10 scenarios run against actual claude-code with a hard `$HARMONIK_REAL_AGENT_BUDGET_USD=5` cap enforced by a wrapper. The wrapper writes the same captured JSONL into `testdata/fixtures/claude-code/<version>/pending/`. A nightly job diffs `pending/` vs committed fixtures; a non-empty diff opens a PR titled `fixture-refresh: claude-code <version>`. This is the drift detector.

**Twin conformance (deferred, named here so it is not forgotten).** Same scenarios run real-agent + twin in the same CI job; event streams diffed with a tolerance spec. Deferred; gap acknowledged in `docs/methodology/TESTING.md` §Twin conformance.

## Crash-recovery testing approach

Fault injection is implemented by a `faultpoint` package that is **always compiled in** — no `//go:build crash` build tag on the package itself. Production is safe because no env-var arms any site: `faultpoint.Check(site)` is a ~1ns hot-path that looks up the site name in a map that is empty in production, then returns. There is no second build configuration.

**Arming at test time.** The test harness sets `HARMONIK_FAULTPOINT_ARM=<site>:<action>` before launching the daemon subprocess. Example: `HARMONIK_FAULTPOINT_ARM=mid-commit-write:SIGKILL` causes the next hit of `mid-commit-write` to `syscall.Kill(os.Getpid(), SIGKILL)`. Multiple sites may be armed with a comma-separated list. Unknown sites log a warning and are ignored (helps detect stale test configs).

Named sites (starter set, each one a line in the daemon): `before-checkpoint-commit`, `mid-checkpoint-commit` (between `git write-tree` and `git commit-tree`), `after-commit-before-beads-write`, `mid-jsonl-fsync`, `after-merge-before-bead-close`, `during-workspace-cleanup`, `mid-reconciliation-verdict-commit`.

Test shape: scenario harness launches daemon subprocess with `HARMONIK_FAULTPOINT_ARM` set, runs scenario until fault fires, restarts daemon (without the env-var), asserts the reconciliation classifier reaches the expected category and the final git state is well-defined. `os/exec.Cmd.Process.Kill()` is used for external SIGKILL where the fault has to come from outside the process. JSONL tail corruption is simulated by a `corruptJSONLTail(path, bytes)` helper that truncates mid-record between runs.

**Trade-off.** Runtime check cost is negligible (nanoseconds per site; map lookup on a single-entry-or-empty map) and eliminates the two-build-config complexity of a `//go:build crash` split. The prior build-tag approach traded operational simplicity for a zero-cost-in-production guarantee that was never materially valuable on a cold hot path.

Fast subset (3 sites, ~1 min) runs every push under the crash test tier (now tag `//go:build crash` on the *tests* only, not the `faultpoint` package). Full site set runs nightly.

## CI gates

**User-endorsed invariant (2026-04-24):** every check listed here is ALSO executable locally via the `make full` target; CI and local run IDENTICAL commands. Agent-declared-done requires `make full` to pass locally (see `quality-checks.md §Two gate targets` and `agent-configuration.md`). No CI-only logic is permitted. If CI fails after a local pass, it is environment drift — a bug in setup, not a CI-specific behavior.

Work is accepted when `make full` passes. Where the work then lands is owned by `build-practices.md §"Branch model — land on the integration branch"`. `make full` covers (each item is a command equally runnable locally):

1. `go vet ./...` clean; `staticcheck ./...` clean (via `golangci-lint`); `gofumpt -l` empty.
2. `go test -short ./...` (unit + property, default tags) passes in <3 min. `-race` on this tier moved to the nightly lane — see Decisions §8.
3. `go test -race -tags=integration ./...` passes in <5 min.
4. `go test -race -tags=scenario ./test/scenario/...` passes in <10 min.
5. `go test -tags=crash ./test/crash/...` (fast subset) passes in <2 min.
6. `tools/forbid-import` passes (holds the library allowlist).

Two things people expect to be gates here are not:

- **Coverage does not block.** `scripts/coverage-gate.sh` runs from `make coverage-gates` and reports a trend. See §"Coverage numbers".
- **A `t.Skip()` does not block either, and 233 of them are in the tree today.** No script rejects one, and no script ever has — an earlier version of this section named `scripts/no-skips.sh`, which does not exist. What does happen: `tools/testreport` lists every skipped test in its NOT RUN section on each `make fast` and `make full` run. Read that section, because a disabled test is an unproven claim wearing a green tick. A skip that is meant to stay should say in its reason string what has to become true before it runs again, and carry a bead. A skip added to turn a red gate green is the smell worth stopping on: it converts a failure into silence, and nothing downstream can tell the difference.

Nightly (not merge-blocking, but opens auto-PR on failure): full crash site set, full property suite with random seed (`HARMONIK_RAPID_SEED=auto`, 10k iters), fixture-refresh job (Tier C), `govulncheck` weekly deep scan.

## ⚑ Assumptions worth user's eye

1. **⚑ Library allowlist held by a linter.** A second mocking or assertion library cannot arrive without an edit to the allowlist, and that edit is a separate reviewable change. Tight, and it is the reason the list has not drifted.
2. **⚑ Hand-written fakes are the default; hybrid on the table later.** Start with hand-written fakes in `faketest/`. If interface count exceeds ~10 or fakes show clear rot (copy-paste drift, out-of-sync with the real interface), a hybrid (hand-written fakes + generated mocks for cleanly-typed, high-arity interfaces) becomes acceptable — reviewable as a one-shot decision, not a blocker.
3. **⚑ Budget cap for real-agent tests is $5/nightly.** Drawn from nowhere; user should confirm acceptable. Hard cap enforced by a wrapper, not a soft convention.
4. **⚑ Coverage percentages are a diagnostic, not a goal** — settled 2026-07-27, and §"Coverage numbers" carries the reasoning. The 95%-on-core figure was endorsed as a target on 2026-04-24 (it matched the user's Python practice and is aggressive by Go standards, where the median is 70-80%), and it produced volume rather than protection. Retained here as the record of the reversal. See `plans/2026-07-27-delete-and-rewrite/NEXT_STEPS.md` §2.
5. **⚑ Fault injection via runtime env-var, not compile-tagged dead code.** Simpler; runtime check cost is negligible (nanoseconds per site). Prior compile-tag approach was over-engineered for solo-dev. A single binary configuration is used in prod and test alike; `HARMONIK_FAULTPOINT_ARM` arms sites at test launch.
6. **⚑ `require` (not `assert`) everywhere.** A failed precondition stops the test. Trades readability of multi-assertion tests for fewer cascading failures. Agents get one failure at a time, which is easier to debug.

## Deferred / follow-up

- **Twin-conformance automated diff.** Tier C writes captures; automated real-vs-twin event-stream diff with tolerance spec is deferred.
- **Continuous fuzz infrastructure.** Seed corpora at `testdata/fuzz/<parser>/` are in scope (see §Property). Long-running continuous fuzzing (scheduled `-fuzz` jobs, OSS-Fuzz integration) is deferred.
- **AST-level anti-coverage-gaming analyzer.** Custom `go/analysis` pass verifying every `Test*` function reaches an assertion (`require.*`, `testify.*`, or an explicit `t.FailNow` / `t.Fatal` / `t.Error*`) on every return path. Prevents assertion-free table loops from pumping coverage without verifying behavior. Uses the same `go/analysis` vehicle as the four-axis-tag analyzer. Needed because any coverage number, target or alarm, reads the same for a test that asserts nothing. Until it ships, reviewer-agents check for the pattern by hand.
- **Benchmark suite.** No performance regression gate. `testing.B` benchmarks land with the RTO-sensitive paths (reconciliation startup, bead selection) when those specs are written.
- **Scenario generation from production failures.** S09 improvement loop may emit scenarios (see S07 open question); not yet.
- **Coverage per-file exemption mechanism.** Current gate is per-package-class; generated code exemptions deferred until the first generated-code file exists.
