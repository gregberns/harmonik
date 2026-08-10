# Implementation Tasks

## T1. Add cycle policy and environment

Build `CyclePolicy`, `CycleEnv`, library resolution, and resolved-command conversion.

Spec: `SPEC.md` Main types and Policy levels.

Deliverables: `internal/keeper/cycle_policy.go`, parity tests, and command-policy parity tests.

Acceptance: every field and sentinel matches the legacy paths.

Verification: run keeper package tests, the seam ratchet when present, and `make fast`.

Dependencies: none.

## T2. Add narrow port contracts and the seam ratchet

Define the exact source and effect interfaces. Add compatibility adapters and the tracked seam checker.

Spec: `SPEC.md` Required ports and Compatibility and removal.

Deliverables: port definitions, adapter contract tests, `internal/keeper/legacy_seams.txt`, and `scripts/keeper-seam-ratchet.sh`.

Acceptance: lazy-read guards, journal error rules, nonce scrubbing, and the ratchet pass.

Verification: run keeper package tests, `scripts/keeper-seam-ratchet.sh`, and `make fast`.

Dependencies: none. T1 and T2 can run in parallel.

## T3. Add validated cycle construction

Build `CycleDeps`, `CycleIDGenerator`, validation, and `NewCyclerWithDeps`.

Spec: `SPEC.md` Main types, Required ports, and Observation and action flow.

Deliverables: constructor code and missing-dependency tests.

Acceptance: typed nil and multi-missing tests pass. No effect runs before validation succeeds.

Verification: run keeper package tests, the seam ratchet, and `make fast`.

Dependencies: T1 and T2.

## T4. Migrate command production wiring

Resolve project configuration into policy, environment, and real dependencies. Start production through the validated constructor.

Spec: `SPEC.md` Policy levels and Scenario rules.

Deliverables: command wiring, parity tests, and startup error handling.

Acceptance: command policy matches legacy effective configuration. Keeper startup reports construction errors.

Verification: run keeper and command package tests, the seam ratchet, and `make fast`.

Dependencies: T3.

## T5. Add the production-policy scenario builder

Add recording narrow ports, fake time, named gate methods, and ordered effect records.

Spec: `SPEC.md` Scenario rules.

Deliverables: `internal/keepertest/scenario.go`, builder tests, migrated success and collision scenarios, and a tmux twin with resolved policy or named opt-outs.

Acceptance: every gate method has coverage. Raw policy cannot mutate after construction. The collision replay parks and retries. Integration-tag twin tests pass with no silent gate disable.

Verification: run keeper, keepertest, and integration-tag tests. Run the seam ratchet and `make fast`.

Dependencies: T3.

## T6. Add differential and mutation checks

Compare legacy and new construction for all named fixtures. Add detached-worktree origin and dependency mutations.

Spec: `SPEC.md` Acceptance and Compatibility and removal.

Deliverables: differential tests, mutation commands, and gate table tests.

Acceptance: old and new paths match. Each mutation makes its scenario fail.

Verification: run keeper, keepertest, command, and integration-tag tests. Run the seam ratchet and `make fast`.

Dependencies: T4 and T5.

## T7. Run the real command scenario

Task record: `hk-77khj`.

Add an automated test under `cmd/harmonik`. Run `harmonik keeper` through command-private production wiring in a temporary project. Use controlled pane and transcript boundaries from the scenario harness.

Acceptance: the test command passes. The cycle journal records complete and `events.jsonl` contains `session_keeper_cycle_completed`.

Dependencies: T4, T5, and T6.

## T8. Run the operator-facing exploration

Task record: `hk-7x7no`.

Add an automated subprocess test under `cmd/harmonik`. Start a valid isolated keeper through production wiring. Then run `harmonik keeper doctor --agent test-agent`.

Acceptance: doctor reports a live watcher. The keeper stops cleanly and leaves no process behind.

Dependencies: T4 and T6.

## T9. Migrate remaining callers and remove legacy seams

Migrate keeper tests and remaining production callers. Remove all function fields, broad adapters, the compatibility list, and its checker.

Spec: `SPEC.md` Compatibility and removal.

Acceptance: final seam search is empty. All keeper and integration tests pass.

Verification: run keeper, keepertest, command, and integration-tag tests. Run `make fast`. Confirm the final seam search is empty before removing the ratchet.

Dependencies: T7 and T8.

## T10. Final verification

Run `make full` and review the complete change.

Spec: all acceptance criteria.

Acceptance: full gate passes with no retry. A separate reviewer approves the indexed change.

Dependencies: T9.

## Dependency graph

```text
T1 ─┐
    ├─ T3 ─┬─ T4 ─┬─ T6 ─┬─ T7 ─┐
T2 ─┘      └─ T5 ─┘      └─ T8 ─┴─ T9 ─ T10
```
