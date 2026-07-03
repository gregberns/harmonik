# T2 — Property Tests (rapid invariants): Research Findings

**Track:** T2 — Property Tests  
**Date:** 2026-05-20  
**Status:** complete

---

## Research Questions

1. Is `pgregory.net/rapid` in `go.mod`? What is the current v1.x API?
2. What are the 3 most valuable invariants to encode first?
3. Where do `TestProp_*` functions live per testing.md conventions?
4. How does `HARMONIK_NIGHTLY=1` env var change iteration count?
5. Does `rapid` require any special test runner setup?

---

## Findings

### Q1: rapid in go.mod

`pgregory.net/rapid` is NOT in `go.mod`. The only non-stdlib deps are:
  github.com/google/uuid, gopkg.in/yaml.v3, github.com/expr-lang/expr

Adding rapid requires: `go get pgregory.net/rapid` → updates go.mod + go.sum.

Current pgregory.net/rapid version: v1.1.0 (as of 2025). API is stable.

Key rapid v1.x API surface needed:
```go
import "pgregory.net/rapid"

func TestProp_Xxx(t *testing.T) {
    rapid.Check(t, func(t *rapid.T) {
        // generators: rapid.Int(), rapid.SliceOf(rapid.String()), etc.
        // assertions: if invariant fails, call t.Fatalf(...)
    })
}
```

HARMONIK_NIGHTLY=1 → check `os.Getenv("HARMONIK_NIGHTLY")` in TestMain or in each prop test, call `t.(*rapid.T).Repeat(10000)` or use `rapid.Settings{Checks: 10000}`.

### Q2: Most valuable invariants to encode first

Based on the problem-space (property tests = catch non-obvious invariants in core + eventbus):

**Invariant 1: TestProp_BeadIDRoundTrip** (internal/core)
  - BeadID = opaque string with parse/format cycle
  - `rapid.StringMatching(...)` → `BeadID.Parse` → `.String()` == original
  - Value: catches any future formatting change that breaks round-trip

**Invariant 2: TestProp_EventMarshalRoundTrip** (internal/eventbus)
  - rapid generator over event type + payload fields
  - `json.Marshal → json.Unmarshal` produces identical struct
  - Value: catches JSON tag drift, missing fields, type mismatches

**Invariant 3: TestProp_ReconciliationCategoryTotal** (internal/brcli)
  - The 6-category classifier (`classifyreconciliation_bi031b.go`) must return a valid category for ANY input
  - rapid generator over partial bead state fields
  - Value: catches "category not matched → zero value returned" bugs

Alternative: `TestProp_EdgeSelectionDeterminism` as specified in `03-components.md` — requires understanding the `SelectEdge` function. Quick search: `SelectEdge` not found in internal/core. The spec references an edge-selection function that may not exist yet or has a different name. Need to verify target function before committing to this invariant.

**Revised recommendation:** T2 bead implements TestProp_BeadIDRoundTrip + TestProp_EventMarshalRoundTrip + TestProp_ReconciliationCategoryTotal. These all have clear, existing target functions. The `SelectEdge` invariant in `03-components.md` should be revised or deferred to when the function exists.

### Q3: Test file placement per testing.md conventions

testing.md §"Property — internal/<pkg>/*_prop_test.go":
  - File: `internal/core/beadid_prop_test.go`
  - File: `internal/eventbus/event_prop_test.go`
  - File: `internal/brcli/reconciliation_prop_test.go`
  - Build tag: NONE required (rapid tests run under default `go test`)
  - Nightly tag: optionally `//go:build nightly` for slow variants; not required for basic prop tests

### Q4: HARMONIK_NIGHTLY=1 iteration count

testing.md §5: "HARMONIK_NIGHTLY=1 bumps iteration count to 10,000". This is a project convention, not a rapid built-in. Implementation:

```go
func propChecks() int {
    if os.Getenv("HARMONIK_NIGHTLY") == "1" { return 10000 }
    return 100 // rapid default
}

rapid.Check(t, func(t *rapid.T) { ... }, rapid.Settings{Checks: propChecks()})
```

### Q5: rapid test runner setup

None required beyond `go get pgregory.net/rapid`. rapid is a pure Go library with no external deps. Works under `-race`. Integrates with stdlib `testing.T`.

---

## Options and Tradeoffs

**go.mod commit strategy:**

Option A: `go get pgregory.net/rapid` as standalone commit (T2 scope, not bundled)
- Pros: atomic; reviewable in isolation
- Verdict: RECOMMENDED per `03-components.md` note ("bundle with first property test for atomicity")
- Correction to component doc: actually, bundling is cleaner — one commit adds dep + first test

Option B: Separate go.mod-only commit before any TestProp_* functions
- Cons: adds a commit with no tests, harder to review
- Verdict: not recommended

---

## Risks and Unknowns

1. `SelectEdge` function name from `03-components.md` not found in `internal/core/` — the T2 spec for that invariant needs to be revised to target an actual function
2. `testify/require` is not in go.mod — T2 bead should NOT add testify (per convention, rapid's own t.Fatalf is sufficient for property test assertions; testify is for unit tests that haven't been written yet)
3. The `HARMONIK_NIGHTLY=1` implementation needs a helper function to avoid repetition across all prop test files — candidate for `internal/testhelpers/`
