# Analysis — Testing Strategy Uplift

**Work:** testing-strategy-uplift  
**Pass:** analyze  
**Date:** 2026-05-20

---

## What Exists: Inventory

### 1. Testing docs (authoritative)

`docs/foundation/project-level/testing.md` is the normative testing strategy doc. It names:
- 5-layer methodology: unit, integration, scenario, crash, property
- Toolchain: stdlib testing + testify/require only; `pgregory.net/rapid` for property; `gotest.tools/v3/{golden,fs,icmd}` for golden/fs; `faultpoint` for crash injection
- Build tags: `integration`, `scenario`, `crash`, `nightly`
- Naming conventions: `TestXxx` / `TestIntegration_Xxx` / `TestScenario_Xxx` / `TestCrash_Xxx` / `TestProp_Xxx`
- Coverage targets: core ≥95%, internal ≥90%, utility ≥85%, overall floor ≥90% with ≤0.3pp regression guard
- Handler-divergence testing: recorded-fixture (Tier A), twin-driven scenario (Tier B), real-agent nightly (Tier C, deferred)
- Fuzz seed corpora for 4 boundary parsers (DOT, YAML policy, JSONL event, commit trailer)

This doc is well-written but describes intended state, not actual state.

`docs/foundation/project-level/quality-checks.md` documents the three-tier Makefile gauntlet; same gap (well-documented, partially implemented).

### 2. Build infrastructure

**Makefile** — all tiers present as targets:
- `make test` — `go test ./...` (quick smoke)
- `make check` (Tier 2) — full golangci-lint + `-race` + tidy diff + coverage gate + govulncheck
- `make check-full` (Tier 3) — Tier 2 + `-tags=integration` + `-tags=scenario` + `-tags=crash`
- `make test-e2e-real-claude` / `make test-e2e-real-claude-reviewloop` — real claude smoke tests
- Coverage gate invoked as `scripts/coverage-gate.sh` inside the `check` recipe

**scripts/coverage-gate.sh** — EXISTS and is fully implemented (bash 4+, associative arrays, per-package parsing of `go tool cover -func` output). Three gates: (1) internal/core ≥95%, (2) all other internal ≥90%, (3) ≤0.3pp regression vs baseline. Excludes `internal/testhelpers` from thresholds. Vacuously passes when baseline is empty or no packages exist.

**coverage.baseline** — EXISTS as a comment-only stub. No package entries yet. The regression gate is vacuously satisfied.

**lefthook.yml** — pre-commit runs `make check-fast` + `make agent-review`; pre-push runs `make check`; commit-msg runs `scripts/validate-commit-msg.sh`. All hooks are wired. `make agent-review` stubs gracefully when skill absent.

### 3. Linting (.golangci.yml)

Active linters: errcheck, govet, staticcheck, ineffassign, unused, errorlint, nilerr, copyloopvar, testifylint, errchkjson, exhaustive, bodyclose, rowserrcheck, sqlclosecheck, contextcheck, noctx, containedctx, fatcontext, gocritic, revive, misspell, unparam, unconvert, prealloc, nakedret, nolintlint, gosec, forbidigo, depguard.

Active depguard rules:
- `beads-direct-access-ban` — enforced globally (all `.go` except `internal/brcli/`, `internal/testhelpers/`)
- `llm-sdk-ban` — enforced globally
- `core` — files: `**/internal/core/**`; deny: any `internal/` import
- `queue` — files: `**/internal/queue/**`; allow: gostd + `internal/core`
- `handler-brcli-ban` — files: `**/internal/handler/**`, `**/internal/handlercontract/**`
- `lifecycle-tmux` — files: `**/internal/lifecycle/tmux/**`

**Commented-out depguard stubs for packages that now exist:**
- `eventbus` (package `internal/eventbus` exists) — stub commented out
- `orchestrator` (not yet in `internal/` proper, but there is `internal/scenario`, `internal/workspace` etc.) — various stubs commented
- `workspace` — stub commented
- `handler-impls` — stub commented
- `adapter-br`, `adapter-ntm` — stubs commented
- `daemon` — stub commented
- `cmd` — stub commented

### 4. Test distribution by package

| Package | Prod files | Test files | Notes |
|---------|-----------|-----------|-------|
| internal/core | 179 | 175 | High test density; unit tests |
| internal/daemon | 30 | 73 | High test density; some scenario |
| internal/workspace | 25 | 57 | High test density |
| internal/handlercontract | 32 | 34 | Good coverage |
| internal/lifecycle | 31 | 70 | Good coverage |
| internal/scenario | 20 | 28 | Scenario harness scaffold |
| internal/handler | 11 | 15 | Unit tests |
| internal/eventbus | 3 | 4 | Unit tests |
| internal/operatornfr | 8 | 29 | Many invariant tests |
| internal/queue | 16 | 10 | Lower test density |
| internal/brcli | 20 | 32 | Good coverage |
| internal/specaudit | 1 | 127 | Spec audit has heavy tests |
| internal/testhelpers | 6 | 3 | Test infra itself |

Total: 388 prod files, 666 test files across the whole `internal/` tree.

### 5. Property tests

**Zero property tests exist.** No `TestProp_*` functions found anywhere in the codebase. `pgregory.net/rapid` is named in testing.md as the chosen library but is NOT in `go.mod` (module only has: `github.com/google/uuid`, `gopkg.in/yaml.v3`, `github.com/expr-lang/expr`). Testify is referenced in golangci-lint config (`testifylint` linter) but also not in `go.mod` — this means either tests don't use it or the linter is catching future imports.

**Observation:** the absence of rapid from `go.mod` means property tests are entirely unstarted — adding them requires `go get pgregory.net/rapid` AND writing `TestProp_*` functions.

### 6. Build-tag test tiers

- `//go:build integration` — only in `test/integration/integration_stub.go` (empty stub). No real integration tests yet.
- `//go:build scenario` — in `test/scenario/` (harness + `scenarios_test.go` which has 5 real scenario tests: Fix1–Fix3, Fix6, Fix10 from the twin-parity audit). And in `internal/daemon/t2_scenarios_test.go` (likely).
- `//go:build crash` — only in `test/crash/crash_stub.go` (empty stub). No real crash tests yet.

**Key finding:** `test/scenario/scenarios_test.go` is NOT a stub — it has 5 real tests referencing specific beads (hk-lj1p9.3, hk-tjl40, hk-smuku, hk-o5eww, hk-cmybm). The scenario tier is partially real, not entirely stubbed.

### 7. Scenario gap audit

`docs/scenario-test-gap-audit-2026-05-18.md` documents 5 P0 gaps:
1. HandlerPause policy goroutine wired end-to-end (caught hk-37zy8 class)
2. Reviewer-phase tmux substrate wired (caught hk-2hb2y)
3. Reviewer-phase nil watcher handled gracefully (caught hk-yjduq)
4. Handler-fatal outcome → pause trip → queue items held (hk-37zy8 broader)
5. Reviewer-phase agent_ready timeout → kill + reap → bead re-queued (hk-yjduq broader)

These have been (or are being) filed as sibling beads by a concurrent agent. This plan cross-references them without duplicating.

### 8. Fuzz infrastructure

No fuzz tests found (`FuzzXxx` functions). The seed corpora dirs (`testdata/fuzz/`) do not appear to exist yet. This is a deferred gap per testing.md.

### 9. Twin infrastructure

`twins/generic-twin` (from `cmd/harmonik-twin-generic/`) is the test twin. `make build-twin-generic` builds it. `test/twins/` has `hang/main.go` and `fail-immediately/main.go` — minimal test twins for specific behaviors. The scenario test harness in `test/scenario/harness_test.go` references these.

---

## Gap Inventory (What's Absent)

| Gap | Severity | Affected Track |
|-----|----------|----------------|
| Property tests (TestProp_*) — zero exist; rapid not in go.mod | High | T2 |
| Integration tests with `//go:build integration` — only stub | High | T3 |
| Crash tests with `//go:build crash` — only stub | High | T4/crash |
| coverage.baseline — empty (vacuous gate) | High | T6 |
| Depguard stubs for existing packages (eventbus, workspace, etc.) | Medium | T5 |
| Fuzz seed corpora and FuzzXxx tests — none | Medium | T2/fuzz |
| `tools/forbid-import/` — referenced in Makefile, not present | Low | T5 |
| `testdata/fixtures/claude-code/` — recorded wire fixtures | Low | T4 |
| Friction-mining process (testing-gap bead class) | Medium | T8 |

---

## Conventions to Follow (for beads produced by this plan)

1. **Build tags:** `//go:build integration|scenario|crash|nightly` — one tag per tier file. Never mix tags.
2. **Naming:** `TestProp_EdgeSelectionDeterminism` not `TestProperty_...`; `TestIntegration_CompositionRootWiring` not `TestInt_...`
3. **Fakes:** `internal/<pkg>/faketest/` — hand-written, no generated mocks.
4. **No new test libraries** unless explicitly approved in testing.md.
5. **Race detector:** `go test -race` on all tiers. The `-race` flag appears explicitly in every Makefile target.
6. **testify/require only** — not testify/assert, not testify/suite.
7. **rapid import:** `pgregory.net/rapid` — add to go.mod with `go get` in the bead implementing T2.
8. **coverage.baseline edits:** single-concern commit, kerf codename in body, cite `codename:testing-strategy-uplift`.

---

## Constraints Discovered

1. `scripts/coverage-gate.sh` already exists and is complete — the problem-space note that it was missing was incorrect. The script is the gate; what's missing is a populated `coverage.baseline` and packages with nonzero coverage data.
2. `testifylint` is configured in golangci-lint but `testify` is not in `go.mod` — this is not a bug. golangci-lint checks if testify is used; if it's not imported, the linter is a no-op. When beads add testify, it will be lint-enforced immediately.
3. The `make check` target already calls `scripts/coverage-gate.sh` conditionally (`if [ -x scripts/coverage-gate.sh ]`). Since the script now exists, `make check` will run the gate. With an empty `coverage.baseline` and no coverage profile yet, the gate vacuously passes.
4. `rapid` is not in `go.mod` — any bead adding property tests must run `go get pgregory.net/rapid` and commit `go.mod` + `go.sum` as part of its scope.

---

## Recent git activity in testing areas

- `hk-6x7dw` (EV-036 regression guard) — added `internal/core/ev036_global_registry_hk6x7dw_test.go`, merged 2026-05-20
- `hk-ppt32` (drain queue to cancelled on SIGINT/timeout) — daemon work-loop changes with tests
- Prior session: scenario tests for 5 twin-parity fixes landed in `test/scenario/scenarios_test.go`

The codebase is actively receiving tests via `harmonik run` dispatch. The gap is systemic (property, integration, crash) not total absence.
