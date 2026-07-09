# C1 — RunCtx tuple contract — Research findings

Mechanical component. Add five sibling provider fields to the published
`handlercontract.RunCtx` alongside `Model`, additive, zero-value = no-override.

## Research questions

1. What is the exact precedent field + doc-comment discipline to mirror?
2. Is `internal/handlercontract` a true leaf package (no new imports required)?
3. Which construction sites must keep compiling once the fields are added?
4. Does any consumer iterate/serialize `RunCtx` fields so adding fields is non-additive?

## Findings

**Q1 — precedent field.** `RunCtx.Model string` (handlercontract/harness.go:141),
doc "resolved model alias; empty ⇒ no flag (tool default)"; `Effort string`
(harness.go:143) is a second string precedent. Mirror the five as `Provider`,
`APIKeyEnv`, `APIKeyFile`, `BaseURL`, `API` — all `string`, each with an
"empty ⇒ no override (harness-global default)" doc comment. Field names should
match the `PiHarness` struct field spelling so the C4 override reads as a straight
copy (piharness.go:52-79: `provider`, `apiKeyEnv`, `apiKeyFile`, `baseURL`, `api`).

**Q2 — leaf package.** Plain `string` fields; no new import needed.
`internal/handlercontract` stays a leaf. The `Harness` interface
(`LaunchSpec(RunCtx)`, harness.go:173) is unchanged — only the struct grows.

**Q3 — construction sites.** The RunCtx literal for the pi/codex path is at
harnessregistry.go:240-264 (the ONLY place `claudeRunCtx` projects onto `RunCtx`
for pi; `Model: rc.model` at :258). Test constructors build RunCtx/piRunCtx
literals too (pilaunchspec_test.go; hk_pkugu/hk_lfrub via `ExportedClaudeRunCtx`).
All keyed struct literals → unset new fields default to `""` → keep compiling.

**Q4 — no non-additive consumer.** No reflection/range-over-fields serialization
of RunCtx found; every consumer reads named fields. Adding fields is safe.

## Pattern to mirror / test to pin
- Mirror `Model string` verbatim (five string fields; "empty ⇒ no override" doc).
- Test: struct-shape assertion (fields exist, are `string`) + both
  `internal/handlercontract` and `internal/daemon` compiling. No behavior test at
  this layer — behavior proven at C4/C5.

## Risks / open decisions
- None blocking. Only naming consistency (match PiHarness field spelling).
