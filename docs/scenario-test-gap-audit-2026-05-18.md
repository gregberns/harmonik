# Scenario-test gap audit — 2026-05-18

Source: gap-audit sub-agent, dispatched during the v50 session. Five P0 workflows currently have zero scenario-test coverage. Each names the half-built-system pattern it would have caught at PR time. Filed as beads hk-sc1..hk-sc5 (titles below); IDs assigned at filing time.

## 1. HandlerPause policy goroutine wired end-to-end

When the daemon emits `budget_exhausted` or `agent_rate_limit_status`, `HandlerPausePolicyGoroutine` (subscribed in `daemon.go` pre-`bus.Seal()`) must trip the handler to `paused`, causing the dispatcher to skip subsequent queue items for that handler type.

**Catches:** hk-37zy8 — policy goroutine existed + unit-tested but was never `Subscribe()`d in production. A test that boots the full daemon, injects the event, and asserts `IsPaused() == true` would have caught this before merge.

**Twin support:** new `harmonik-twin-claude --scenario budget-exhausted` variant emits the event after `agent_ready`.

**Spec refs:** handler-pause.md §4, execution-model.md §4.6, scenario-harness.md §4.

## 2. Reviewer-phase tmux substrate wired

When `deps.substrate` is non-nil, `runReviewLoop` must assign `Substrate` to both `implSpec` and `revSpec` before `h.Launch`; `SpawnWindow` is called; `pasteInjectOnLaunch` succeeds.

**Catches:** hk-2hb2y — `implSpec.Substrate` / `revSpec.Substrate` were unwired; `SpawnWindow` never called; `lastHandle` empty; `pasteInjectOnLaunch` panicked "no window spawned yet."

**Twin support:** stub `handler.SubstrateSession` (counter spy) injected via `deps.substrate`. No real tmux.

**Spec refs:** process-lifecycle.md §4.7 PL-021b, handler-contract.md §4.

## 3. Reviewer-phase nil watcher handled gracefully

When reviewer is launched via tmux substrate (watcher nil because `subSess.Stdout()` is nil), `revWatcher.Done()` and kill-reap blocks must not panic; reviewer phase completes via context cancellation.

**Catches:** hk-yjduq — immediately after hk-2hb2y wired the substrate, first dogfood panicked because `revWatcher.Done()` was called unconditionally on nil. Race-detector test on this path would have caught it.

**Twin support:** same stub substrate as #2. Run under `-race`.

**Spec refs:** process-lifecycle.md §4.7, handler-contract.md §4.8.

## 4. Handler-fatal outcome → pause trip → queue items held (full work-loop path)

When the work loop dispatches a bead and the handler twin emits `agent_failed` (handler-fatal sub-class), dispatcher detects the pause trip and emits `queue_item_held_for_handler_pause` for the next pending bead instead of dispatching it.

**Catches:** hk-37zy8 (broader form) — even with subscribe fixed, the dispatcher-gate check in the work loop has never been exercised end-to-end. A silent regression in the gate check would let beads keep dispatching against a paused handler.

**Twin support:** new `--scenario handler-fatal` twin variant emits `agent_ready` then `agent_failed` with `is_handler_fatal: true`. Uses `ExportedRunWorkLoop` + stub two-bead ledger.

**Spec refs:** handler-pause.md §4 (gate locus).

## 5. Reviewer-phase agent_ready timeout → kill + reap → bead re-queued

When reviewer phase exceeds `agentReadyTimeout` without `agent_ready`, work loop must kill the reviewer process, reap within `agentReadyKillReapTimeout`, and re-queue the bead to `open` (not leak as `in_progress`).

**Catches:** hk-yjduq broader form — the kill-and-reap path after reviewer ready-timeout is load-bearing for bead liveness but has never been exercised; silent failure to requeue would strand beads in `in_progress` forever.

**Twin support:** new `--scenario reviewer-hang` twin variant (implementer completes normally; reviewer hangs indefinitely). Shortened `agentReadyTimeout` (100ms) in test.

**Spec refs:** execution-model.md §4.3 (re-queue contract), handler-contract.md §4.8.
