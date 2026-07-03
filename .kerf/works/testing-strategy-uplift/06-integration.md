# Integration Plan — Testing Strategy Uplift

**Work:** testing-strategy-uplift  
**Pass:** integration  
**Date:** 2026-05-20

---

## Component Connection Map

```
T6 (coverage baseline)
  └─> T1 (unit gap audit — needs populated baseline; produces testing-gap child beads)
  └─> T7 (cadence — coverage delta line needs populated baseline)

T3 (integration tests)
  └─> T8 (friction-mining — needs T3 test examples to document)

T4 (scenario coordination)
  └─> T8 (friction-mining — needs scenario bead IDs from hk-p3diy children)

T2 (property tests) — independent after go.mod add; unblocks T2b, T2c
T5 (lint/depguard) — independent; produces follow-up beads for eventbus + forbid-import

Parallel-safe first batch: T6 (after hk-b5bc0), T5a, T2a
Second batch: T3a, T3b, T1-audit (after T6 lands)
Third batch: T4b, T7, T8 (after T3 + T4 are planned)
hk-p3diy children: tracked independently, not this plan's dispatch
```

---

## Integration Order

1. **T6** (unblocks T1, T7): populate coverage.baseline. Depends on hk-b5bc0 and queue/cli build fix.
2. **T5a** (independent): activate workspace/daemon/cmd depguard rules. File T5b (eventbus arch) + T5c (forbid-import).
3. **T2a** (independent): add rapid + first TestProp_*. File T2b, T2c as follow-up beads.
4. **T3a, T3b** (after T3 research): integration tests in internal/daemon/. Deps: none (can run after T6 context is established but not strictly blocked).
5. **T1-audit** (after T6): produce per-package gap table + testing-gap child beads.
6. **T4b** (independent): create docs/scenario-test-authoring-checklist.md. T4a (verify existing scenarios) can run immediately.
7. **T7** (after T6): make test-all alias + delta line + AGENTS.md session checklist.
8. **T8** (after T3 planned): friction-mining doc + file integration-tier testing-gap beads.

---

## Shared State and Resources

- **`coverage.baseline`**: shared by T6 (writes), T1 (reads), T7 (reads). Must be committed before T1 and T7 run.
- **`go.mod` / `go.sum`**: T2 adds `pgregory.net/rapid`. All subsequent beads that build tests will include this dep.
- **`.golangci.yml`**: T5 modifies. Any bead that adds code to workspace/daemon/cmd must comply with the new rules after T5 lands.
- **`docs/testing-friction-mining.md`**: T8 creates it; T1 audit methodology should link to it.

---

## Cross-Cutting Concerns

### Initialization / commit ordering
- T6 must land before T1 and T7 (their inputs depend on a populated baseline).
- T5a must land before any bead that adds new imports to workspace/daemon/cmd (the new rules would catch violations immediately).
- T2a must land before T2b and T2c (they depend on rapid being in go.mod).

### Shared error: pre-existing test failures
- hk-b5bc0 (preexisting fails) and the queue/cli build error are prerequisites for a clean T6 run.
- Until those land, `make check` exits non-zero regardless of T6.
- This is tracked in T6 spec as a declared dependency.

### No new abstractions
- T1-T8 are all additions to the test/docs layer. No changes to production code in any spec.
- T3 uses existing `TestOnly*` hooks in daemon.Config — no new production seams needed.
- T2 adds rapid to go.mod — this is the only dep addition.

### Race detector consistency
- All new tests (T2, T3) must pass under `go test -race`.
- T3 integration tests use `//go:build integration` tag consistently.
- T2 property tests have no build tag (run under default `go test -race`).

---

## Cross-Component Inconsistencies Found and Resolved

1. **T2 spec uses `require.NotNil`** — testify not in go.mod. T3 spec also references `require.*`. Resolution: both specs note that `t.Fatal()` is the stdlib fallback until testify is added by a separate bead.

2. **T4 references hk-sc1..hk-sc5** — obsolete placeholder IDs. Resolution: T4 spec replaces all references with hk-p3diy children. `03-components.md` still has the old IDs — the tasks pass (07-tasks.md) will carry the corrected references.

3. **T8 beads are "integration-tier equivalents"** — distinct from hk-p3diy scenario beads. Clarification: hk-p3diy children are scenario-tier; T8's filed beads (hk-lz485 and related) are integration-tier. Both are needed per T3 requirement §5 ("T3 catches composition bugs pre-scenario, T4 catches them end-to-end").

---

## Integration Testing Strategy

This plan's own integration is tested via:
1. T6: `scripts/coverage-gate.sh` runs as part of `make check` after baseline population.
2. T3: `go test -race -tags=integration ./internal/daemon/...` covers composition-root wiring.
3. T5a: `golangci-lint run ./...` after .golangci.yml changes.
4. T2: `go test -race ./internal/core/... ./internal/eventbus/... ./internal/brcli/...` with TestProp_* filter.

No new CI infrastructure required. All verification via Makefile targets.
