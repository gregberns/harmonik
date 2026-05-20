# Test/Production Divergence Audit — 2026-05-20

Source: investigation under user directive 2026-05-20. Read-only audit.

## Summary

6 HIGH-severity + 2 MEDIUM-severity patterns where production code contains test-only seams, nil-guards, or fallback logic that masks production bugs.

## HIGH Severity

1. **AdapterRegistry nil-guard (workloop.go:1157-1162)** — `waitAgentReady` silently skipped when `deps.adapterRegistry == nil`. Documented precondition not enforced. Hides the hk-012af class of bug. — Tracked: `hk-d8u1y`.

2. **synthHookStore (export_test.go:207-216, 415-446)** — test-only stub that immediately returns `(nil, nil)` from `WaitForOutcome`, bypassing the 3-second `stopHookGrace` window. Production calls `hookStore.WaitForOutcome` without distinguishing real vs synthetic. Timeout testing is unreliable.

3. **synthLaunchSpecBuilder (export_test.go:218-224, 401-409)** — skips MaterializeClaudeSettings, MintClaudeSessionID, CheckSettingsLocalJSON. Bridge logic CHB-001..005 never exercised in tests.

4. **synthWorktreeFactory (export_test.go:226-233, 374-380)** — uses `os.MkdirTemp` instead of real `git worktree add`. Workspace integration (task-branch landing, commit tracking, cleanup) untested. Tradeoff documented in source (avoids git subprocess overhead).

5. **perRunSubstrate type-assertion fallback (tmuxsubstrate.go:304-316)** — when `sub` is not `*tmuxSubstrate`, `newPerRunSubstrate` returns nil and caller silently falls back to shared substrate. Test doubles bypass per-run pane isolation. Just-added in `a92090b`.

6. **TestOnlyBusObserver + TestOnlyBrAdapterFactory (daemon.go:285-307, 316-320, 537-539)** — explicit `TestOnly*` fields on `Config` that branch composition behavior at the root. Production has two code paths.

## MEDIUM Severity

7. **deadLetterSink nil-guard (busimpl.go:334-335, 344-345, 449-450, 457-458)** — observer panics + consumer errors silently dropped when nil. Tests rarely wire it.

8. **jsonlWriter nil-guard (busimpl.go:283-289, 400-406)** — entire EV-016 JSONL append path skipped when nil. Persistence bugs invisible.

## Architectural Root Cause

`export_test.go` exposes daemon's internal dependency graph (`WorkLoopDepsParams` ~200 lines), allowing tests to wire different dependencies than production. Production code does NOT enforce preconditions — it checks nil and changes behavior. The SH-INV-001 spec audit forbids test-mode branches but uses if-flag detection; the field-nil pattern dodges it.

## Recommendations

- HIGH-001: Remove synthHookStore, synthLaunchSpecBuilder, synthWorktreeFactory from `WorkLoopDepsParams`. Force tests to wire production dependencies.
- HIGH-002: Enforce adapterRegistry non-nil precondition in `newWorkLoopDeps` (hk-d8u1y scope).
- HIGH-003: Remove TestOnlyBusObserver + TestOnlyBrAdapterFactory from daemon.Config; replace with explicit fixture factories.
- HIGH-004: Audit production code for nil-checks on Substrate / Adapter / Registry; require precondition assertions instead.
- MEDIUM-001: Assertions (not just comments) for intentional nil-guards.
- MEDIUM-002: Ensure bridge, worktree, hook-relay timeout logic is exercised with real implementations.

Full investigation: agent a915362366110ca34 output.
