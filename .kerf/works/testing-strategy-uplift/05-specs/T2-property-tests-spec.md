# Change Spec: T2 — Property Tests (rapid invariants)

**Component:** T2  
**Date:** 2026-05-20  
**Research:** 04-research/T2/findings.md

---

## Requirements (from 03-components.md)

1. Add `pgregory.net/rapid` to go.mod+go.sum.
2. Write TestProp_EdgeSelectionDeterminism in internal/core/ (rapid over state+candidates; SelectEdge total+deterministic).
3. Write TestProp_JSONLEventRoundTrip in internal/eventbus/ (Marshal → Unmarshal round-trip).
4. Write TestProp_ReconciliationClassifierTotal in internal/daemon/ (classifier returns valid category for any input).
5. All TestProp_* compile and pass under go test -race.
6. HARMONIK_NIGHTLY=1 bumps iteration count to 10,000.

---

## Research Summary

- `pgregory.net/rapid` not in go.mod; requires `go get`.
- `SelectEdge` function name not found in `internal/core/` — requirement 2 needs to be revised to target an actual function.
- Revised invariant targets: BeadID round-trip (core), EventMarshal round-trip (eventbus), ReconciliationClassifier total (brcli).
- HARMONIK_NIGHTLY=1 is a project convention; implement via a `propChecks()` helper.
- No build tag required for basic property tests (unlike integration/scenario/crash tiers).

---

## Approach

**Decision on SelectEdge:** The function does not exist. Replace requirement 2 with `TestProp_BeadIDRoundTrip` — BeadID.Parse(s).String() == s for any valid BeadID string. This is a concrete, existing invariant in `internal/core/beadid.go`.

**Three property tests (one bead each or bundled):**

**TestProp_BeadIDRoundTrip** (`internal/core/beadid_prop_test.go`):
```go
rapid.Check(t, func(t *rapid.T) {
    id := core.BeadID(rapid.StringMatching(`hk-[a-z0-9]{5}`).Draw(t, "id"))
    got, err := core.ParseBeadID(string(id))
    if err != nil { return } // invalid input, skip
    require.Equal(t, id, got) // round-trip
}, rapid.Settings{Checks: propChecks()})
```
(Adjust to match actual BeadID API from beadid.go.)

**TestProp_JSONLEventRoundTrip** (`internal/eventbus/event_prop_test.go`):
- rapid.Make over core.Event fields (Type, Payload as string map)
- json.Marshal → json.Unmarshal → assert equality
- Catches: JSON tag drift, missing fields

**TestProp_ReconciliationCategoryTotal** (`internal/brcli/reconciliation_prop_test.go`):
- rapid generators over partial bead state (status, gitHead, beadsHead)
- assert ClassifyReconciliation returns one of the 6 defined category values
- Catches: unhandled state combinations falling through to zero value

**HARMONIK_NIGHTLY=1 helper:**

Add to `internal/testhelpers/prophelpers.go`:
```go
func PropChecks() int {
    if os.Getenv("HARMONIK_NIGHTLY") == "1" { return 10000 }
    return 100
}
```
Import in each prop test file.

**go.mod commit strategy:**
Single bead: add `pgregory.net/rapid` + implement first TestProp_* in the same commit. Subsequent prop tests can be separate beads.

---

## Files & Changes

- `go.mod`, `go.sum` — add `pgregory.net/rapid` (from `go get pgregory.net/rapid`)
- `internal/testhelpers/prophelpers.go` — new file: `PropChecks()` helper
- `internal/core/beadid_prop_test.go` — new file: TestProp_BeadIDRoundTrip
- `internal/eventbus/event_prop_test.go` — new file: TestProp_JSONLEventRoundTrip
- `internal/brcli/reconciliation_prop_test.go` — new file: TestProp_ReconciliationCategoryTotal

Note: No build tag needed for basic property tests. If nightly-only variants are added later, use `//go:build nightly`.

---

## Acceptance Criteria

1. `go test -race ./internal/core/... ./internal/eventbus/... ./internal/brcli/...` exits 0 with all 3 TestProp_* functions running.
2. `HARMONIK_NIGHTLY=1 go test -race ./internal/core/...` runs 10,000 iterations for TestProp_BeadIDRoundTrip (verify via test count output).
3. `pgregory.net/rapid` appears in `go.mod`.
4. `testhelpers.PropChecks()` is importable from all property test files.
5. A deliberately broken BeadID round-trip (manually introduced in a test) causes rapid to report a counterexample.

---

## Verification

```bash
go test -race -v ./internal/core/... -run TestProp_  # must pass, show rapid iterations
go test -race -v ./internal/eventbus/... -run TestProp_  # must pass
go test -race -v ./internal/brcli/... -run TestProp_  # must pass
HARMONIK_NIGHTLY=1 go test -v ./internal/core/... -run TestProp_BeadIDRoundTrip  # must run 10000 iterations
```

---

## Error Handling

- Invalid BeadID strings: the prop test should use `rapid.StringMatching` to constrain to valid inputs, or handle `ParseBeadID` errors gracefully (skip vs fail).
- Reconciliation classifier: generators should cover the boundary cases (nil gitHead, empty status) explicitly.

---

## Bead Candidates

- T2a: `T2: add pgregory.net/rapid + TestProp_BeadIDRoundTrip` (type: task, labels: test-infra, codename:testing-strategy-uplift)
- T2b: `T2: TestProp_JSONLEventRoundTrip in internal/eventbus` (type: task)
- T2c: `T2: TestProp_ReconciliationCategoryTotal in internal/brcli` (type: task)
