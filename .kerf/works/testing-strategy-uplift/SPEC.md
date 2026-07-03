# SPEC: Testing Strategy Uplift

**Work:** testing-strategy-uplift  
**Status:** integration complete (tasks pass next)  
**Date:** 2026-05-20

---

## What This Work Does

Closes the gap between harmonik's documented testing strategy (`docs/foundation/project-level/testing.md`) and its actual implementation. 8 tracks, each producing concrete beads for `harmonik run` dispatch.

The work does NOT produce new product features. It produces:
- A live coverage gate (was vacuous)
- Integration tests that catch composition-root wiring bugs (the hk-37zy8 class)
- Property tests (the first `pgregory.net/rapid` usage in the project)
- Activated depguard rules for existing packages
- A per-session checklist and friction-mining loop
- Scenario test authoring conventions

---

## The 8 Tracks + Their Beads

### T6 — Coverage Baseline Population (dispatch first)

**Goal:** Populate `coverage.baseline` with real per-package numbers so the regression gate is calibrated.

**Current state:** coverage.baseline is a comment-only stub; gate vacuously passes.  
**Target state:** 9+ package entries; `make check` fails on absolute floor gates (intentional) and catches regressions.

**Bead:** T6 — populate coverage.baseline  
**Depends on:** hk-b5bc0 (fix preexisting fails), queue/cli build fix  
**Files changed:** `coverage.baseline` only

**Done means:**
- coverage.baseline has ≥9 entries in format `internal/<pkg>  <number>`
- `scripts/coverage-gate.sh` emits floor failures (expected)
- Regression gate produces no findings (baseline = current)

---

### T5 — Lint / depguard Rule Activation

**Goal:** Activate the depguard rules for packages that now exist.

**Current state:** workspace, daemon, cmd rules are commented out. eventbus rule is commented out AND would fail if activated (imports handlercontract).  
**Target state:** workspace, daemon, cmd rules active and green. eventbus architecture question filed as separate bead.

**Beads:** T5a (activate 3 rules), T5b (file eventbus arch bead), T5c (file forbid-import bead)  
**Files changed:** `.golangci.yml`

**Done means:**
- `golangci-lint run` exits 0 after changes
- workspace/daemon/cmd depguard rules are active (uncommented)
- eventbus architecture bead filed

---

### T2 — Property Tests (rapid invariants)

**Goal:** Add the first `TestProp_*` functions using `pgregory.net/rapid`.

**Current state:** Zero property tests; rapid not in go.mod.  
**Target state:** 3 TestProp_* functions; rapid in go.mod; HARMONIK_NIGHTLY=1 bumps iterations.

**Beads:** T2a (rapid + BeadID round-trip), T2b (EventMarshal round-trip), T2c (Reconciliation classifier total)  
**Files changed:** go.mod, go.sum, internal/testhelpers/prophelpers.go, 3 new prop test files

**Done means:**
- `go test -race ./internal/core/... ./internal/eventbus/... ./internal/brcli/... -run TestProp_` exits 0
- `HARMONIK_NIGHTLY=1 go test ...` runs 10,000 iterations

---

### T3 — Integration Tests (composition-root wiring)

**Goal:** Add the first `//go:build integration` tests that catch composition-root wiring bugs.

**Current state:** test/integration/ has only an empty stub.  
**Target state:** ≥2 integration tests in internal/daemon/ using TestOnlyBusObserver and Substrate spy.

**Beads:** T3a (HandlerPausePolicyGoroutine subscription), T3b (Substrate wiring spy)  
**Files changed:** internal/daemon/composition_integration_test.go (new)

**Done means:**
- `go test -race -tags=integration ./internal/daemon/...` exits 0
- Removing Subscribe call from daemon.go causes T3a to fail
- Removing Substrate assignment causes T3b to fail

---

### T1 — Unit Test Gap Audit

**Goal:** Map every internal package that is below its coverage floor; produce one testing-gap bead per gap.

**Current state:** No structured audit; every package below floor.  
**Target state:** ≥9 testing-gap beads filed with per-package gap data; methodology documented.

**Bead:** T1-audit (produces child beads, no code change)  
**Depends on:** T6

**Done means:**
- ≥9 testing-gap beads filed labeled `testing-gap, codename:testing-strategy-uplift`
- docs/testing-audit-2026-05-20.md exists with per-package gap table

---

### T4 — Scenario Test Coordination

**Goal:** Document the scenario authoring checklist; verify existing tests pass; resolve hk-sc1..hk-sc5 → hk-p3diy children.

**Current state:** No checklist doc; hk-sc1..hk-sc5 are orphan placeholder IDs.  
**Target state:** docs/scenario-test-authoring-checklist.md exists; 5 existing tests verified green.

**Beads:** T4a (verify existing scenarios pass), T4b (checklist doc)  
**Files changed:** docs/scenario-test-authoring-checklist.md (new)

**Done means:**
- `go test -race -tags=scenario ./test/scenario/...` exits 0
- Checklist doc exists with authoring invariant + twin extension pattern

---

### T7 — Cadence: make test-all + delta line + session checklist

**Goal:** One-command "run all tests"; coverage delta output; session-end checklist in AGENTS.md.

**Current state:** check-full exists but is not aliased; gate emits no delta line; no session checklist.  
**Target state:** `make test-all` works; delta line in gate output; AGENTS.md has session checklist.

**Bead:** T7 (Makefile + script + AGENTS.md)  
**Depends on:** T6  
**Files changed:** Makefile, scripts/coverage-gate.sh, AGENTS.md

---

### T8 — Friction-Mining Loop

**Goal:** Define the pattern for converting runtime failures into testing-gap beads.

**Current state:** No formal friction-mining process; ad-hoc bug-to-bead flow.  
**Target state:** docs/testing-friction-mining.md with template + worked examples; ≥2 testing-gap beads filed.

**Bead:** T8 (doc + bead filing)  
**Depends on:** T3 (examples)  
**Files changed:** docs/testing-friction-mining.md (new)

---

## Dispatch Order

**Batch 1 (parallel, no deps on each other):**
- T6 bead (after hk-b5bc0 merges)
- T5a bead
- T2a bead

**Batch 2 (after Batch 1 merges):**
- T3a bead
- T3b bead  
- T1-audit bead (after T6)
- T2b bead
- T2c bead

**Batch 3:**
- T4a bead (verify scenarios — can run at any point)
- T4b bead
- T7 bead (after T6)
- T8 bead (after T3 is planned)

---

## What This Work Intentionally Excludes

- Fuzz tests (FuzzXxx): deferred per testing.md; no seed corpora
- Crash tests (//go:build crash): only the stub exists; filling it is a follow-up
- Real-claude nightly tests (Tier C per testing.md): deferred
- testify/require in go.mod: no bead in this plan explicitly adds it (it arrives when the first unit tests need it)
- hk-p3diy children (SC-1..SC-7): tracked separately, not this plan's dispatch

---

## Key Files for Implementers

- `docs/foundation/project-level/testing.md` — normative testing strategy (the "why")
- `scripts/coverage-gate.sh` — the gate (already implemented)
- `internal/daemon/daemon.go` — TestOnlyBusObserver and TestOnly* fields
- `.golangci.yml` — depguard rules (commented stubs at line ~154)
- `coverage.baseline` — currently a stub
- `test/scenario/scenarios_test.go` — 5 real scenario tests
- `test/scenario/harness_test.go` — scenario test harness
