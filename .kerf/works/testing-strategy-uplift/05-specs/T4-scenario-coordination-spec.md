# Change Spec: T4 — Scenario Test Coordination + Checklist

**Component:** T4  
**Date:** 2026-05-20  
**Research:** 04-research/T4/findings.md

---

## Requirements (from 03-components.md)

1. 07-tasks.md lists hk-p3diy children as sibling dependencies, not duplicated beads.
2. Document in `docs/scenario-test-authoring-checklist.md` the invariant: every feature bead touching composition root MUST cite a scenario test bead.
3. Define twin extension pattern for new scenarios: --scenario flag, twin script location.
4. Existing 5 scenario tests in test/scenario/scenarios_test.go verified to pass.

---

## Research Summary

- The original hk-sc1..hk-sc5 placeholder IDs were never filed. The actual canonical beads are the hk-p3diy children: hk-x3s1p (SC-1), hk-92v9m (SC-2), hk-xfhva (SC-3), hk-wzygs (SC-4), hk-35mpj (SC-5), hk-nx5wu (SC-6), hk-04azt (SC-7), hk-d8u1y.
- 5 existing scenario tests in test/scenario/scenarios_test.go are real (not stubs).
- `harmonik-twin-claude` binary is in repo root as a Mach-O arm64 executable; source location unclear.
- T4 is blocked by hk-p3diy (epic must unblock before child beads can run).

---

## Approach

**Bead T4a: Verify existing 5 scenario tests pass**

Run `go test -race -tags=scenario ./test/scenario/...` and confirm all 5 tests (Fix1-3, Fix6, Fix10) pass. If any fail, file a bug bead — this is a prerequisite for the checklist doc being credible.

**Bead T4b: Create scenario-test-authoring-checklist.md**

File: `docs/scenario-test-authoring-checklist.md`

Contents:
1. **Invariant:** Every feature bead that modifies daemon.go's composition-root wiring (subscriber registration, handler launch path, substrate wiring, queue wiring) MUST include or cite a scenario-test bead in its "Done means..." criteria. This bead is reviewed at PR time; a bead close without a scenario-test bead cite is a protocol violation.

2. **Checklist for authoring a new scenario test:**
   - [ ] Identify the composition-root path being exercised
   - [ ] Select or extend a twin variant (see Twin Extension Pattern below)
   - [ ] Write `TestScenario_<Name>` in `test/scenario/scenarios_test.go`
   - [ ] Build tag: `//go:build scenario`
   - [ ] Add a comment: `// catches <bead-id> class: <brief description>`
   - [ ] Run `go test -race -tags=scenario ./test/scenario/...` — must pass
   - [ ] Reference test in the feature bead's "Done means..." entry

3. **Twin Extension Pattern:**
   The `harmonik-twin-claude` binary is the standard test twin. To add a new `--scenario X` variant:
   - Source is in `cmd/harmonik-twin-claude/` (or the binary at repo root was built from there — confirm before implementing)
   - Add a new case to the scenario dispatch switch in the twin's main.go
   - The variant should: emit `handler_capabilities` → emit events matching the scenario → optionally emit `agent_ready` or hang
   - Add `make build-twin-claude` target if not present (analogous to `make build-twin-generic`)
   - The scenario test must reference `make build-twin-claude` in its setup

4. **Existing twin variants:** from `test/twins/` directory: `hang/main.go`, `fail-immediately/main.go`. These are minimal test twins. Scenario-specific variants belong in the main twin binary, not as separate `test/twins/` programs.

**Bead T4c: Update 07-tasks.md with hk-p3diy children**

When 07-tasks.md is written (tasks pass), reference hk-p3diy and its children as sibling dependencies. This is a task-pass concern, not a code change.

---

## Files & Changes

- `docs/scenario-test-authoring-checklist.md` — new file (Bead T4b)
- No changes to test code in T4 (existing scenario tests are verified, not modified)

---

## Acceptance Criteria

1. `go test -race -tags=scenario ./test/scenario/...` exits 0 (5 existing tests pass).
2. `docs/scenario-test-authoring-checklist.md` exists with all 4 sections above.
3. The invariant (composition-root bead → must cite scenario test) is documentable in the checklist.
4. Twin extension pattern section names a specific source path for the harmonik-twin-claude main.go.

---

## Verification

```bash
go test -race -tags=scenario -v ./test/scenario/...  # must show 5 passing tests
ls docs/scenario-test-authoring-checklist.md  # must exist
```

---

## Dependencies

- T4 is conceptually blocked by hk-p3diy (the SC beads need to be dispatched before T4's full scenario coordination function is verifiable). T4b (checklist doc) can proceed independently.

---

## Bead Candidates

- T4a: `T4: verify 5 existing scenario tests pass under go test -tags=scenario` (type: task, chore)
- T4b: `T4: create docs/scenario-test-authoring-checklist.md` (type: docs)
