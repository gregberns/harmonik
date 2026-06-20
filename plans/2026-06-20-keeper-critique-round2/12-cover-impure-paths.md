# 12 — Cover the keeper's impure side-effect paths (hk-zole)

**Goal:** add DETERMINISTIC coverage to the keeper side-effect surface the suite
previously mocked at the point it fails in production — the real `InjectText`
paste→750 ms-settle→Enter→2× retry sequence (was ~10.5 %; `sendEnter` 0 %) and
the respawn ACTUAL-spawn (`sh -c <RespawnCmd>`, previously stubbed `true`).
Refs: `15-reliability-baseline.md` §1/§3, `04-testability.md` §2.

## What changed

### Seam added (minimal, in the package's `…Fn` style)
`internal/keeper/injector.go`:
- New package-level var **`tmuxRunFn`** (defaults to `runTmuxCombined`), the one
  place the injector shells out to tmux: `tmuxRunFn(ctx, stdin, args...) →
  (combinedOutput, err)`. `InjectText` and `sendEnter` now call it instead of
  `exec.CommandContext` directly. This lets the FULL sequence run against a fake
  runner with no real tmux — mirroring `CyclerConfig.InjectFn` et al.
- `submitRetryDelay` changed `const → var` (now matches `submitSettle`, which was
  already a var "so tests can zero it"). Value unchanged (400 ms); still
  regression-guarded by the existing `TestInjectText_SettleConstants`.

No behavior change to production paths — `runTmuxCombined` reproduces the prior
`exec.CommandContext(...).CombinedOutput()` exactly (incl. stdin for load-buffer).

### Tests added
- `injector_sequence_hkzole_test.go` (pkg `keeper`): drives `InjectText`
  end-to-end against a recording fake runner. Asserts the exact tmux sequence
  (load-buffer w/ text on stdin → paste-buffer → first send-keys Enter →
  `submitRetries` retry Enters), that settle+retry waits actually elapse (timed),
  and the error/cancel arms: load-buffer error stops, paste-buffer error stops,
  first-Enter error is fatal (no retries), retry-Enter errors are best-effort
  (nil), cancel-during-settle returns `ctx.Err()` and never Enters. Plus
  `sendEnter` direct (success issues one Enter; tmux error wraps).
- `respawn_spawn_hkzole_test.go` (pkg `keeper_test`): runs a REAL harmless
  `RespawnCmd` (`printf RESPAWNED > sentinel`) on a TRUSTED (UUIDv4) `.sid` and
  proves the sentinel was written — i.e. `sh -c` actually executed, not a stubbed
  `true`. Pairs it with the untrusted-sid arm (absent/uuidv7/garbage) asserting
  the identity gate fail-closes (`ErrLiveRecoverIdentityUntrusted`, sentinel
  absent) for the same side-effecting command.

## Coverage delta (default `go test ./internal/keeper/... -cover`)

| Function | Before | After |
|---|---|---|
| `InjectText` | 10.5 % | **93.8 %** |
| `sendEnter` | 0.0 % | **100 %** |
| `NewLiveRecoverViaRespawn` (respawn action) | 100 %* | 100 % (now via a REAL side-effecting cmd, not `true`) |
| package `internal/keeper` total | 74.4 % baseline / 76.3 % branch | **77.6–78.4 %** |

\* the respawn action was already 100 % line-covered, but its only "ran" assertion
used `RespawnCmd:"true"` which proves nothing executed; this adds the sentinel
proof that `sh -c` really fired on a trusted sid.

## Green confirmation
- `go build ./...` — OK
- `go vet ./internal/keeper/...` — OK
- `go test ./internal/keeper/...` — PASS (8+ consecutive runs, incl. `-race`)
- tmux: `tmux ls | wc -l` = **9 before and after** — no leak. No live tmux/claude
  touched (fake runner only).
