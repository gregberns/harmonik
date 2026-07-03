# Problem Space — Testing Strategy Uplift

**Work:** testing-strategy-uplift  
**Project:** gregberns-harmonik  
**Jig:** plan  
**Status:** problem-space → ready for analyze  
**Date:** 2026-05-20

---

## Summary

Harmonik is at the Phase 1 → Phase 2 threshold (operational smoke GREEN as of 2026-05-14). Multiple bugs have landed that were unit-tested, reviewer-APPROVED, and still broken at runtime:

- **hk-37zy8** — `HandlerPausePolicyGoroutine` existed and was unit-tested but was never `Subscribe()`d in the composition root. No scenario test caught the wiring gap.
- **hk-yjduq** — nil watcher panic in reviewer phase, triggered immediately after hk-2hb2y's fix. Race-detector scenario test would have caught this.
- **hk-2hb2y** — `implSpec.Substrate` / `revSpec.Substrate` left unset; `SpawnWindow` never called; `pasteInjectOnLaunch` panicked at runtime.
- **hk-012af** — similar class: unit-tested, APPROVED, broken in composition.

The pattern: **unit tests cover the unit, but don't cover wiring**. Approval-gated commits that look right in isolation fail at integration because there is no automated gate that exercises the full stack. The project needs a layered, regularly-run test strategy that catches composition-root wiring gaps before they land in main.

The strategy doc at `docs/foundation/project-level/testing.md` already describes a 5-layer methodology (unit, integration, scenario, crash, property), names the toolchain (rapid for property, gotest.tools for golden/fs, faultpoint for crash injection), and sets concrete coverage targets (95%/90%/85% by package class). What the doc describes is largely **not yet built** — coverage.baseline is empty, property tests are absent, most scenario tests are stubs, the crash harness is a stub, and the `scripts/coverage-gate.sh` mentioned in the Makefile does not yet exist. The Makefile references these gaps with `#hk-pvcs.5`, `#hk-pvcs.7` comments.

This plan defines the work required to take the testing strategy from documented-intention to operational-reality across 8 tracks, and establishes how testing gaps are detected and converted into beads on an ongoing basis.

---

## Goals

1. **Make `make check` actually enforce coverage** — the coverage gate (`scripts/coverage-gate.sh`) exists in Makefile but the script does not. Ship it with a populated `coverage.baseline`.
2. **Property tests for candidate invariants** — introduce `pgregory.net/rapid` (already chosen in testing.md), write properties for edge-selection determinism, JSONL round-trip, DOT cycle-detection, reconciliation classifier stability.
3. **Integration tests for the daemon-substrate-twin contract surface** — every wiring seam that has burned us (composition root, substrate wiring, handler launch path) gets a `//go:build integration` test.
4. **Scenario tests for the 5 P0 gaps audited 2026-05-18** — these are already named in `docs/scenario-test-gap-audit-2026-05-18.md` (hk-sc1…hk-sc5) and being filed by a sibling agent; this plan coordinates cross-references without duplicating per-scenario bead work.
5. **Lint audit** — identify which golangci-lint rules are currently disabled, stale, or commented-out stub entries in `.golangci.yml`, and produce a remediation plan.
6. **Cadence** — establish a local-only (no GitHub Actions) test-all cadence that fits the `harmonik run` dispatch loop: Makefile target + lefthook hooks + per-session-end checklist.
7. **Friction-mining loop** — define how testing gaps are detected (runtime failure → bead pattern) and turned into systematic bead candidates, not just one-off fixups.

---

## Non-Goals

- **GitHub Actions / remote CI** — explicitly out of scope. User has stated local-only. The Makefile/CI parity invariant (everything runnable via `make check-full`) is the enforcement surface.
- **Nightly / real-agent smoke tests** — `test/realagent/` tier described in testing.md is deferred; this plan does not schedule it.
- **New testing libraries** — the sanctioned library list in `docs/foundation/project-level/testing.md` is locked. No new dependencies unless the plan explicitly argues for one and the user approves.
- **Twin conformance diffing** — deferred per testing.md §"Twin conformance (post-MVH, named here so it is not forgotten)." This plan may create the bead but does not implement it.
- **Per-scenario beads (hk-sc1…hk-sc5)** — those are filed by a sibling agent. This plan cross-references them as dependencies but does not re-create them.

---

## Constraints

1. **Go 1.25 toolchain** — testing/synctest is available; use it for goroutine timing tests in the daemon.
2. **No in-process mocks** — hand-written fakes in `internal/<pkg>/faketest/`. No gomock/mockery.
3. **Race detector mandatory** — every test tier runs under `-race` in the Makefile.
4. **Build tags isolate tiers** — `integration`, `scenario`, `crash`, `nightly`. `go test ./...` (no tags) runs unit + property only. Already documented in testing.md; this plan enforces it is implemented.
5. **coverage.baseline is a protected file** — edits require a kerf codename citation in the commit body. Initial population is a single-concern commit under this codename.
6. **Lefthook hooks are installed, not shell scripts** — the hook manager is `lefthook.yml`, not `.git/hooks/`. `make tools` installs lefthook; `lefthook install` arms it.
7. **Local-only** — no GitHub Actions, no remote jobs. The `make check-full` target is the CI-equivalent surface.
8. **Bead-label convention** — beads produced by this plan carry `codename:testing-strategy-uplift` label per CLAUDE.md §"Bead label convention".

---

## Success Criteria (Done means...)

Observable, agent-verifiable conditions — NOT "the beads shipped":

1. **`make check` passes with coverage gate enforced.** `scripts/coverage-gate.sh` exists, is executable, parses `go test -cover` output, and fails the build if any package class in `coverage.baseline` drops below its recorded floor. Verified by running `make check` from a clean checkout and observing it exits 0.

2. **`coverage.baseline` is populated.** At least one entry per top-level subsystem package (`internal/daemon`, `internal/handler`, `internal/core`, `internal/eventbus`, `internal/queue`, `internal/workspace`, `internal/lifecycle`, `internal/orchestrator`). Verified by `grep -c . coverage.baseline` returning ≥ 8.

3. **Property tests exist for ≥3 invariants.** At least 3 `TestProp_*` functions tagged with `pgregory.net/rapid` exist across `internal/`. Verified by `grep -r "TestProp_" internal/ | wc -l` returning ≥ 3. Running `go test ./... -run=TestProp_` completes without failure under `-race`.

4. **Integration tests cover the wiring-gap class.** At least one `//go:build integration` test in `internal/daemon/` that exercises composition-root wiring (subscription wiring, substrate assignment). Verified by `go test -race -tags=integration ./internal/daemon/...` exiting 0 and the test output naming at least one wiring assertion.

5. **The scenario gap audit beads (hk-sc1…hk-sc5) are referenced.** The tasks artifact (07-tasks.md) for this plan lists hk-sc1…hk-sc5 as sibling dependencies, not duplicated beads. Verified by reading 07-tasks.md.

6. **Lint audit complete and applied.** `.golangci.yml` has no commented-out `# Add when...` stubs for packages that now exist (depguard rules for `eventbus`, `policy`, `handler-contract`, `workspace`, `orchestrator`). Verified by `golangci-lint run` exiting 0 with the updated config.

7. **Cadence is operational.** `make test-all` (or `make check-full`) runs all tiers (unit, property, integration, scenario, crash) in sequence and produces a single summary line with coverage delta vs baseline. `lefthook.yml` has a `pre-push` entry that runs `make check` (not just `check-fast`). Verified by running `make check-full` and observing a coverage delta line in the output.

8. **Friction-mining loop is documented.** A doc or section in this plan's artifact set defines the pattern: runtime failure → bead class label (`testing-gap`) → bead creation template → kerf cross-reference. Verified by the existence of `docs/testing-friction-mining.md` (or equivalent section in an existing doc) and at least one existing bead carrying `testing-gap` label.

---

## Context: What Exists Now

### What's present
- `docs/foundation/project-level/testing.md` — comprehensive 5-layer strategy, toolchain choices, coverage targets, fixture conventions. Well-written. Not enforced.
- `docs/foundation/project-level/quality-checks.md` — three-tier Makefile gauntlet (Tier 1/2/3), formatter, vet, golangci-lint v2 config, pre-commit hooks via lefthook.
- `.golangci.yml` — rich linter config, active depguard rules for `core`, `queue`, `handler-brcli-ban`, `llm-sdk-ban`, `beads-direct-access-ban`, `lifecycle-tmux`. Many depguard subsystem rules are commented-out stubs waiting for packages to land.
- `Makefile` — `make test`, `make check`, `make check-full` targets exist and reference `scripts/coverage-gate.sh` (missing) and `tools/forbid-import` (missing stub). `make test-e2e-real-claude` and `make test-e2e-real-claude-reviewloop` exist.
- `internal/testhelpers/` — assert.go, clock.go, crashharness.go, jsonlfixture.go, tempdir.go, testenv.go. Solid shared test infrastructure.
- `test/scenario/` — harness_test.go, scenarios_test.go, scenario_stub.go. Scenario infrastructure exists as a scaffold; stub.
- `test/integration/integration_stub.go` and `test/crash/crash_stub.go` — stubs only.
- `coverage.baseline` — exists, documented, but EMPTY. No entries yet.
- `internal/` has 666 test files vs 388 prod files — high test ratio, but concentrated in unit tests; wiring-level coverage is the gap.
- `lefthook.yml` — exists (referenced by Makefile `make tools`); not read yet; likely has pre-commit hooks.

### What's absent
- `scripts/coverage-gate.sh` — referenced in Makefile, does not exist.
- `tools/forbid-import/` — referenced in Makefile, does not exist.
- Property tests (`TestProp_*`, `pgregory.net/rapid`) — zero instances found.
- Populated `coverage.baseline` entries.
- Integration tests with `//go:build integration` for composition root / wiring seams.
- Depguard rules for subsystems that now exist: eventbus, orchestrator, workspace, handler-contract, lifecycle — rules are commented-out stubs in `.golangci.yml`.
- `docs/testing-friction-mining.md` (or equivalent) — no process for converting runtime failures to test-gap beads.

---

## Seven Tracks (Decomposition Preview)

This plan decomposes into 7 execution tracks (plus a meta track for friction-mining):

| Track | Short name | Key deliverable |
|-------|-----------|-----------------|
| T1 | Unit gaps + conventions | Identify packages with <85% coverage and missing naming conventions; bead candidates |
| T2 | Property tests | Add `rapid` to go.mod, write ≥3 `TestProp_*` invariants |
| T3 | Integration tests (wiring) | `//go:build integration` tests for composition-root seams |
| T4 | Scenario tests | Reference hk-sc1…hk-sc5; define scenario-test authoring checklist |
| T5 | Lint audit | Uncomment depguard stubs; audit disabled/missing rules |
| T6 | Coverage gate | Ship `scripts/coverage-gate.sh`; populate `coverage.baseline` |
| T7 | Cadence | `make test-all`/`make check-full` + lefthook pre-push hook + per-session checklist |
| T8 | Friction-mining | Define `testing-gap` bead class; write `docs/testing-friction-mining.md` |

---

## References

- `docs/foundation/project-level/testing.md` — 5-layer strategy (authoritative)
- `docs/foundation/project-level/quality-checks.md` — three-tier gauntlet
- `docs/scenario-test-gap-audit-2026-05-18.md` — 5 P0 scenario gaps (hk-sc1…hk-sc5)
- `plans/README.md` — "Done means..." discipline
- `.golangci.yml` — current linter config
- `Makefile` — test targets, missing script stubs
- `coverage.baseline` — empty, documented
- `internal/testhelpers/` — shared test infrastructure
- Sibling bead agent: daemon-spawn-path test coverage (cross-reference once bead IDs are filed)

---

## Open Questions

1. **`scripts/coverage-gate.sh` ownership** — should this be a simple shell script (`go test -cover ./... | awk ...`) or a Go tool under `tools/`? Prefer shell for composability with Makefile; Go tool for robustness. Current Makefile comment implies shell. Recommendation: shell script, same pattern as `scripts/record-claude-fixture.sh` described in testing.md.

2. **lefthook.yml pre-push gate** — is pre-push `make check` (Tier 2, 3-5 min) acceptable or should it be `make check-fast` (Tier 1, <15s) pre-push and `make check` only pre-merge? The testing.md doc says "Default pre-push + work-in-progress verification" for Tier 2 — take this as the answer.

3. **depguard stubs for now-existing packages** — packages `eventbus`, `lifecycle`, `workspace`, `handlercontract`, `orchestrator` all exist in the repo. Their depguard rules are commented-out stubs. Should activation be a single batch bead or one bead per package? Recommendation: one batch bead that activates all stubs in a single `.golangci.yml` edit with `make check` as verification.
