# CR (cold code review) — baseline 599f80ab

- PIN: `599f80ab7f8fc7ef5169db8e37210b04aeb5ccb3` on `phase1-session-restart-substrate`
- New surface reviewed: `git diff a0591ba3..599f80ab -- internal/ scripts/scratch-daemon.sh` (26 commits, +4671/-141 across 51 files)
- Reviewer: CR leg (cold, did not build). `go build ./internal/...` = clean.

## Verdict contribution: CLEAN

No P0/P1. No runtime-reachable core-loop/dispatch defect. One latent P2 (in not-yet-production-wired codexdriver scaffolding) and two P3 notes. The baseline can PASS from CR's perspective.

---

## Findings

### P2 — F1: `ResidentSession.Close` defeats its own documented graceful FIFO drain
- **file:** `internal/codexdriver/resident.go:315-345` (Close) + `resident.go:246-256` (ensure)
- **defect:** `Close` sets `r.closed = true` BEFORE calling `r.queue.Close()`. `queue.Close()` drains buffered submissions through the drainer, which calls `r.SubmitInput → r.ensure`, and `ensure` returns `ErrResidentClosed` immediately because `r.closed` is now true. So every submission buffered at Close resolves with `ErrResidentClosed` instead of being delivered.
- **scenario:** caller `Enqueue`s N requests, then `Close()`. `inputqueue.go` Close doc ("Submissions already buffered at Close are still delivered to the port") and `resident.go` Close doc ("Drain buffered submissions (graceful FIFO) before tearing the child down") both promise delivery; all N instead get `ErrResidentClosed`.
- **why bounded:** callers waiting on the result channel receive a clear error (no hang, no goroutine leak, no panic), and `ResidentSession` has **no production caller yet** (`NewResidentSession` is referenced only in tests), so this is latent scaffolding, not a live core-loop defect. Rated P2, effectively latent. Fix: drain the queue BEFORE latching `r.closed`, or have the drainer bypass the closed-check during a graceful close.

### P3 — F2: `nonce_provenance.go` render leg is dead in production
- **file:** `internal/keeper/nonce_provenance.go:27` (`leaderDeferTextForHandoff`)
- **defect:** the T6 "provenance join via handoff KEEPER:<id> marker" render has NO production caller (only `nonce_provenance_hk0hk8n_test.go`). The live warn path (`maybeDeliverLeaderWarn`, `delivery_decision_0nlqs.go:155`) mints a FRESH id via `w.mintCycleID()` and calls `selectLeaderDeferText` directly, never the marker-derived render.
- **scenario:** not a runtime fault; the described marker→command→event join key is produced by the fresh-mint path, and the marker-read render simply is not wired. Latent/dead code; note for the wire-up follow-up.

### P3 — F3: claude:LOCAL still writes/probes the shared global ~/.claude.json that isolation was meant to bypass
- **file:** `internal/daemon/claudelaunchspec.go:302` (Step 3 EnsureWorktreeTrust, pre-existing) + `:315` (Step 3a' EnsureClaudeThemeVia) vs `:325-338` (Step 3a'' isolation)
- **defect:** once `CLAUDE_CONFIG_DIR` isolation (Step 3a'') is set for a local run, claude ignores `~/.claude.json`, yet Step 3 still upserts the shared-global trust entry and Step 3a' runs the theme seed (the code itself labels the theme seed a superseded no-op for local). The shared-global trust write uses a brand-new worktree key each launch → always the slow-path LOCK_EX RMW → re-incurs the contention the isolation was designed to escape.
- **scenario:** under `-c8` concurrent local launches, the redundant shared-global trust writes still contend on the sidecar flock; bounded by `defaultTrustLockTimeout` (15s, reopens bead) so not a wedge, and not a regression vs pre-isolation behavior. Cosmetic/efficiency; a follow-up can skip the shared-global writes when isolation is active.

---

## Positives confirmed (highest-risk items)

- **Dispatch wiring intact (workloop.go).** The 66-line delta is struct-field re-alignment plus the hk-5h759 codex isolation guard. `CodexRequireIsolationBoundary` is wired end-to-end: `daemon.Config` (daemon.go:579) → `workLoopDeps` (workloop.go:1193) → `beadRunOne` guard (workloop.go:3627), set from `codexRequireBoundary` at both cmd sites (run.go:714, main.go:1356). No dispatch logic altered for the tmux/local path.
- **No slot/lease leak, no lost-wakeup in the guard.** The guard sits at the established pre-launch refusal point (with the srt gate). On refuse it calls `failRun(...)` (reopens the bead) and `return`; the deferred `workerRegistry.ReleaseSlot()` (workloop.go fallback block) and `relLocalSlot`/`localInFlight` cleanup all run on the early return, so no slot/lease is stranded. Fail-CLOSED (refuse, never silent local fallback) matches the runner's `WorkerSnapshot` routing predicate.
- **codexdriver queue bounded and drains.** `BoundedInputQueue` (inputqueue.go) uses a fixed-cap buffered channel; `Enqueue` is non-blocking and returns `ErrQueueFull` at cap (no unbounded growth); a single drainer preserves the port's one-in-flight invariant; `resCh` is buffered(1) so the drainer never blocks resolving results; `Enqueue` holds `RLock` across the channel send while `Close` closes the channel under the write lock, so there is no send-on-closed panic; `Close` waits `drainDone` → no drainer goroutine leak.
- **ResidentSession revival is leak-free and race-free.** No two-live-children (`spawnLocked` replaces only a nil/dead `cur`; `ensure`/`revive` gate on `sessionDead`); `SubmitInput` retries exactly once on `ErrSessionClosed`; watchdog backoff is bounded [100ms,5s] and `backoffSleep`/`superviseLoop` wake on `closeCh`/`ctx.Done()` so Close/cancel exit promptly with no lingering goroutine; `resumeThreadID` is set before `start()` and read only on the readLoop (no data race). thread/resume reuses the thread/start correlation and result codec (codexwire.go alias) correctly.
- **scrub leaks nothing realistic (scrub.go).** `keyIsSensitive` catches separator-less auth concatenations (`authtoken`, `authkey`, `authheader`, `authcode`) via the compact-substring `"auth"` test, excluding only the author/authority family (all contain `"author"`); `authorization` is caught earlier by `unambiguousSecretWords`, and `token`/`apikey`/`password`/`secret` are all checked before the auth rule, so a real secret auth token is redacted regardless of the `author` exclusion. No realistic key leaks a tail; the only leak class (contains `auth` AND `author` but none of token/apikey/password/secret/authorization) is the benign author/authority family plus contrived non-keys.
- **keeper gating correct.** T8 (shell.go:323) re-samples operator-attached each wait tick and gates BOTH edges reaching `/clear` (holds the nonce-confirm and skips the freshness recovery while attached); the handoff-timeout still fires so the wait stays bounded and aborts warn-only rather than `/clear`ing over the operator. gauge-drop gating (step.go:503) suppresses the redundant defensive re-inject only when `ev.CF != nil && belowActThreshold` (context already dropped below act = `/clear` landed); a nil/unreadable CF or a still-high gauge re-injects (fail-defensive, preserves hk-vdqe2). `OperatorAttachedFn` is defaulted (cycle.go:414, watcher.go:774) so the new `TmuxTarget != "" && OperatorAttachedFn(...)` calls cannot nil-panic.
- **keeper delivery config surface honors partial config (projectconfig.go).** `leader_defer_text` / `crew_defer_text` are parsed (rawKeeperWarnMessages), carried verbatim into `KeeperConfig` (parseKeeperBlock:1782), and added to `keeperBlockAbsent` so a keeper block containing ONLY these keys is NOT treated as absent — partial config is not silently dropped. warn_messages live-reload (watcher.go `maybeReloadWarnMessages`) is mtime-gated, advances the cache even on a rejected parse (a persistently-bad edit re-parses at most once), applies only the four text fields (thresholds/bands/self_service stay startup-bound), and runs single-threaded on the Run goroutine.
- **restart-now nonce audit event (restartnow.go:398) is best-effort and correctly ordered** — emitted after the injected /clear+brief, never validates the nonce, nil Emitter (Ping) emits nothing, marshal/emit errors are logged not fatal.
