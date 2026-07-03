# Decomposition — Testing Strategy Uplift

**Work:** testing-strategy-uplift  
**Pass:** decompose  
**Date:** 2026-05-20

---

## Components (8 tracks)

Per the problem-space framing, this plan decomposes into 8 execution tracks. Each track is a coherent unit that can be implemented via one or more beads dispatched through `harmonik run`.

---

### T1 — Unit Test Gap Audit + Convention Enforcement

**One-line:** Identify packages with missing tests or convention violations; produce a bead-per-gap batch.

**Requirements:**
1. Run `go test -cover ./...` across all `internal/` packages and produce a per-package coverage report.
2. Identify packages below 85% coverage (the "utility" floor per testing.md); produce one bead per gap, labeled `testing-gap` + `codename:testing-strategy-uplift`.
3. Confirm naming conventions are followed: `TestXxx` for units, no `Test_` prefix (Go convention), no `TestSuite`-style structs.
4. Document in `docs/testing-friction-mining.md` (shared with T8) that the T1 audit methodology is the template for future sessions.

**Dependencies:** none (can start immediately once coverage.baseline is populated by T6)

---

### T2 — Property Tests (rapid invariants)

**One-line:** Introduce `pgregory.net/rapid` and write ≥3 `TestProp_*` invariants for load-bearing functions.

**Requirements:**
1. Add `pgregory.net/rapid` to `go.mod` + `go.sum` via `go get`. Single-concern commit per coverage.baseline policy.
2. Write `TestProp_EdgeSelectionDeterminism` in `internal/core/` — rapid generator over `(state, candidates)` pairs; invariant: `SelectEdge` is total and deterministic for any valid state.
3. Write `TestProp_JSONLEventRoundTrip` in `internal/eventbus/` — rapid generator over `Event` structs; invariant: `Marshal → Unmarshal` produces identical struct.
4. Write `TestProp_ReconciliationClassifierTotal` in `internal/daemon/` (or relevant package) — rapid generator over partial git + beads states; invariant: classifier returns one of the 6 defined categories for any input.
5. All `TestProp_*` functions compile and pass under `go test -race ./...` (default tags, no extra build tag needed per testing.md §"Property — internal/<pkg>/*_prop_test.go").
6. `HARMONIK_NIGHTLY=1` env var bumps iteration count to 10 000 per testing.md §5.

**Dependencies:** T6 (go.mod change should be coordinated; not a hard blocker)

---

### T3 — Integration Tests (composition-root wiring)

**One-line:** Add `//go:build integration` tests that exercise the composition-root wiring seams that unit tests cannot reach.

**Requirements:**
1. At minimum one integration test in `internal/daemon/` with `//go:build integration` that boots the daemon in a reduced mode (or calls composition-root wiring functions directly) and asserts subscription wiring: e.g., `HandlerPausePolicyGoroutine` is subscribed before `bus.Seal()`.
2. At minimum one integration test that asserts substrate wiring: when `deps.substrate != nil`, `implSpec.Substrate` and `revSpec.Substrate` are both non-nil after `buildSpecs()` (or equivalent).
3. Tests use `internal/testhelpers` for tmpdir and clock. No real claude subprocess.
4. `go test -race -tags=integration ./internal/daemon/...` exits 0.
5. A comment in each test file cites the bead it would have caught (e.g., `// catches hk-37zy8 class: policy goroutine wiring gap`).

**Dependencies:** none (can be authored independently of T2, T5, T6)

**Sibling cross-reference:** The scenario-gap audit beads hk-sc1…hk-sc5 cover the same wiring seams at the scenario level. T3 covers them at the integration level (faster, no twin subprocess). Both are needed: T3 catches composition bugs pre-scenario, T4 catches them end-to-end.

---

### T4 — Scenario Test Coordination + Checklist

**One-line:** Reference the 5 P0 scenario gaps (hk-sc1…hk-sc5 filed by sibling agent) and define the scenario authoring checklist so future scenario tests are complete.

**Requirements:**
1. `07-tasks.md` for this plan lists hk-sc1…hk-sc5 as sibling dependencies, not duplicated beads.
2. Document in `docs/scenario-test-authoring-checklist.md` (or a section of testing.md) the invariant: every new feature bead that touches the composition root MUST include or cite a scenario test bead in its "Done means..." criteria.
3. Define the twin extension pattern for new scenarios: what `harmonik-twin-generic` flags are needed, how to add a new `--scenario X` variant, where the twin script lives.
4. The existing 5 scenario tests in `test/scenario/scenarios_test.go` are verified to pass under `go test -race -tags=scenario ./test/scenario/...`.

**Dependencies:** sibling agent (hk-sc1…hk-sc5 bead IDs must be known); T3 is a prerequisite in spirit (integration tests first, scenario tests second)

---

### T5 — Lint Audit + depguard Rule Activation

**One-line:** Activate commented-out depguard stubs for packages that now exist; audit for any linter config drift.

**Requirements:**
1. For each package that now exists and has a commented-out depguard rule (`eventbus`, `orchestrator`, `workspace`, `handler-impls`, `daemon`, `cmd`, `adapter-br`, `adapter-ntm`): uncomment and activate the rule in `.golangci.yml`.
2. `golangci-lint run` exits 0 after all rules are activated (i.e., existing code does not violate the new rules — fix violations if any before activating).
3. `tools/forbid-import/` — if the stub is referenced in Makefile but not present, either implement it or remove the reference. Current Makefile gracefully skips it with `echo "forbid-import not yet present"` — acceptable to defer the tool itself, but the Makefile skip message should not be a permanent state. File a follow-up bead.
4. Document which linters are intentionally disabled and why (the `.golangci.yml` already has good comments; verify they are complete).

**Dependencies:** none (can run in parallel with T2/T3)

---

### T6 — Coverage Baseline Population

**One-line:** Run `go test -cover ./...` and populate `coverage.baseline` with actual per-package numbers.

**Requirements:**
1. Run `go test -coverprofile=/tmp/harmonik-cov.out -covermode=atomic ./...` and `go tool cover -func` to extract per-package coverage.
2. Populate `coverage.baseline` with one entry per `internal/` package that has ≥1 statement and ≥0% coverage. Format: `<pkg-path> <pct>`.
3. Commit is a single-concern commit (baseline only, no code) with `codename:testing-strategy-uplift` in the body.
4. After commit: `make check` runs `scripts/coverage-gate.sh` with the populated baseline and exits 0.
5. Verify that the gate correctly fails if a test is artificially removed to drop a package below its floor.

**Dependencies:** none (runs against current code state; should be first bead dispatched in this plan's task batch)

**Note:** `scripts/coverage-gate.sh` already exists and is fully implemented. T6 is just population of the baseline file.

---

### T7 — Cadence: `make test-all` + Lefthook + Session Checklist

**One-line:** Make "run all tests" a single command with a readable summary; confirm the pre-push hook gate is operational.

**Requirements:**
1. `make check-full` (Tier 3, already defined) serves as `make test-all` — either alias it or document it as the canonical "run all tests" target. Add `make test-all` as an alias if the name is clearer.
2. `make check-full` output includes a coverage-delta summary line — either extend `scripts/coverage-gate.sh` to emit `"coverage delta vs baseline: +X.Xpp"` or add a separate `scripts/coverage-summary.sh`.
3. Confirm `lefthook.yml` pre-push hook (`make check`, Tier 2) is operational: run `lefthook run pre-push` manually and verify it calls `make check`.
4. Add a per-session checklist entry to `HANDOFF.md` template (or document under `docs/session-protocol.md`): "Before session-end, run `make check` and confirm green."
5. All of the above is local-only. No GitHub Actions. The Makefile/CI parity invariant (same commands locally and in any future CI) is maintained.

**Dependencies:** T6 (coverage gate must be populated to produce meaningful delta)

---

### T8 — Friction-Mining Loop

**One-line:** Define the process by which runtime failures become `testing-gap` beads, closing the feedback loop from production bug to test coverage.

**Requirements:**
1. Create `docs/testing-friction-mining.md` with the following:
   - Pattern definition: "runtime failure that unit tests passed → missing integration/scenario/property test → testing-gap bead"
   - Template for a `testing-gap` bead: title format, labels required (`testing-gap`, `codename:testing-strategy-uplift` or successor), which tier the gap belongs to, what spec/behavior it covers
   - Examples: hk-37zy8 (policy goroutine wiring), hk-2hb2y (substrate wiring), hk-yjduq (nil watcher), hk-012af — each mapped to which T3/T4 test would have caught it
   - Cadence: after every session where a runtime bug landed that reviewer-APPROVED code did not catch, file ≥1 `testing-gap` bead before session-end
2. File at least one `testing-gap` bead using this template for the current set of open gaps (hk-sc1…hk-sc5 are already filed; this bead should cover the integration-tier equivalents from T3).
3. The `testing-gap` label is defined in the bead ledger (either via `br create` convention or noted in a label registry).

**Dependencies:** T3, T4 (to have concrete examples to document)

---

## Component Dependencies

```
T6 (coverage baseline)
  └─> T1 (unit gap audit — needs coverage data)
  └─> T7 (cadence — delta output needs baseline)

T3 (integration tests) ──────────────────────────────────> T8 (friction-mining)
T4 (scenario coordination — needs sibling bead IDs) ────> T8

T2 (property tests) — independent after go.mod add
T5 (lint audit) — independent
```

Suggested dispatch order (first batch):
1. T6 — populate baseline (unblocks T1, T7)
2. T5 — lint audit (independent, low risk)
3. T2 — property tests (independent, adds rapid to go.mod)

Second batch:
4. T3 — integration tests
5. T1 — unit gap audit (uses T6 data)

Third batch:
6. T4 — scenario coordination (waits for sibling bead IDs)
7. T7 — cadence (waits for T6)
8. T8 — friction-mining (waits for T3, T4)

---

## Interfaces Between Components

- T6 produces `coverage.baseline` entries → consumed by T1 (gap identification) and T7 (delta summary)
- T3 produces integration test files → T8 references them as concrete examples
- T4 resolves sibling bead IDs (hk-sc1…hk-sc5) → T8 uses them in the template
- T2 adds `pgregory.net/rapid` to `go.mod` → all future property-test beads inherit this dep
- T5 activates depguard rules → any code change must pass the new rules (gates all subsequent beads)
