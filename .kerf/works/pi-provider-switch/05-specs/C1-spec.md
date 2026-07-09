# C1 — RunCtx tuple contract — Change Spec

**Component:** C1 (mechanical, foundational). Add the five sibling provider fields
to the published `handlercontract.RunCtx` alongside `Model`, additive, zero-value =
no-override.

## Requirements (from 03-components.md C1)

1. Five new string fields exist on `RunCtx`; each documents "empty ⇒ no override".
2. Zero-value of every new field leaves existing harness behavior unchanged (no
   consumer is forced to read them).
3. No new import or dependency is added to `internal/handlercontract` (it stays a
   leaf contract package).

## Research summary (from 04-research/C1)

- Precedent field to mirror: `RunCtx.Model string`
  (`internal/handlercontract/harness.go:141`), doc "resolved model alias; empty ⇒
  no flag (tool default)". `Effort string` (`harness.go:143`) is a second string
  precedent.
- `internal/handlercontract` is a true leaf — plain `string` fields add no import.
  The `Harness` interface (`LaunchSpec(RunCtx)`, `harness.go:173`) is unchanged;
  only the struct grows.
- Every consumer reads named fields; no reflection/range-over-fields serialization
  of `RunCtx` exists, so adding fields is purely additive. All construction sites
  use keyed struct literals → unset new fields default to `""` → keep compiling.

## Approach

Add exactly five `string` fields to the `RunCtx` struct in
`internal/handlercontract/harness.go`, placed immediately after `Model` (line 141)
and `Effort` (line 143) to keep the model/provider tuple visually grouped. **Field
names MUST match the `PiHarness` struct field spelling** (`piharness.go:52-79`:
`provider`, `apiKeyEnv`, `apiKeyFile`, `baseURL`, `api`) exported to Go-public form,
so the C4 override reads as a straight copy:

- `Provider string`
- `APIKeyEnv string`
- `APIKeyFile string`
- `BaseURL string`
- `API string`

Each carries a doc comment mirroring `Model`'s discipline: "empty ⇒ no override
(harness-global default)."

This is the only change in C1. No behavior, no consumer edits — downstream reads
land in C3 (`claudeRunCtx` projection at `harnessregistry.go:240-264`) and C4
(`PiHarness.LaunchSpec`).

## Files & changes

| File | Change |
|------|--------|
| `internal/handlercontract/harness.go` | Add `Provider`, `APIKeyEnv`, `APIKeyFile`, `BaseURL`, `API` (`string`) to the `RunCtx` struct (after line 143), each with an "empty ⇒ no override (harness-global default)" doc comment. No new import. |

Exact new block (insert after the `Effort` field at line 143):

```go
// Provider is the per-bead Pi provider override (pi-provider-switch).
// Empty ⇒ no override (harness-global default from PiHarness.provider).
Provider string

// APIKeyEnv is the per-bead Pi credential env-var-name override.
// Empty ⇒ no override (harness-global default). Travels coupled with Provider.
APIKeyEnv string

// APIKeyFile is the per-bead Pi credential file-path override.
// Empty ⇒ no override (harness-global default).
APIKeyFile string

// BaseURL is the per-bead Pi endpoint override for local OpenAI-compatible
// endpoints. Empty ⇒ no override. Part of the coupled wire-format triple
// {Provider, BaseURL, API}.
BaseURL string

// API is the per-bead Pi wire-format override (e.g. "openai-completions").
// Empty ⇒ no override. Part of the coupled wire-format triple.
API string
```

## Acceptance criteria

1. `go build ./...` compiles: `internal/handlercontract` and `internal/daemon` both
   build with the five new fields present.
2. A struct-shape assertion test confirms the five fields exist on `RunCtx` and are
   of type `string`.
3. `go vet ./internal/handlercontract` reports no new import; the package importer
   set is unchanged (leaf preserved).

## Verification

- `go build ./...` — no errors.
- New test (see below) passes.
- `go list -deps ./internal/handlercontract` shows no new dependency vs `main`.

## Tests to add

- **`internal/handlercontract/harness_runctx_tuple_test.go`** — `TestRunCtx_ProviderTupleFields_Exist`:
  construct a `RunCtx` with all five new fields set to sentinel strings via a keyed
  literal; assert each reads back. This is a compile-and-shape pin, not a behavior
  test (behavior proven at C4/C5). Follows the keyed-literal convention already used
  throughout `pilaunchspec_test.go`.

## Error handling / edge cases

None at this layer — pure additive struct growth. Zero-value = no-override is the
sole invariant; every downstream reader (C4 fallback, C6 default path) relies on it.

## Migration / backwards compatibility

Fully backward compatible. All existing `RunCtx` construction sites use keyed struct
literals (confirmed research Q3/Q4); new fields default to `""`. No positional
literal exists that would break.
