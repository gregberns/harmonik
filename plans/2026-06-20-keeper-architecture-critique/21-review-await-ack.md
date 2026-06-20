# Independent review — agent-side ACK handshake (`harmonik keeper await-ack`, hk-uldg)

**Commit:** `e902114c` (branch `worktree-agent-ad1f09bd686408cd8`)
**Reviewer:** fresh-context independent agent, 2026-06-20
**Verdict: APPROVE**

Note: the implementer report `19-impl-await-ack.md` does not exist in the plan
dir; review was conducted against the design (`18-design-agent-side-ack.md`),
the commit diff, and the source as committed.

---

## What it does

New `internal/keeper/awaitack.go` + `AwaitAck(ctx, cfg, emitter)` core: polls an
injectable `PaneCapturer` every `--poll` for the exact token
`[KEEPER ACK <nonce>]`. Match → nil (exit 0). Timeout / no-pane / repeated
capture failure → emits durable `session_keeper_ack_timeout` and returns an
error wrapping `ErrAckTimeout` (CLI exit 3). CLI `runKeeperAwaitAck` wires
`ResolveTmuxTarget` + `NewFileEmitter`, dispatched from `main.go`. Additive
`core.EventTypeSessionKeeperAckTimeout` + `SessionKeeperAckTimeoutPayload`.

## Correctness of the match — PASS

- Matches the FULL bracket token `AckMatchToken(nonce)` = `[KEEPER ACK <nonce>]`
  via `strings.Contains`, NOT a bare nonce substring. A bare `rn-123` in a log
  line cannot false-match (closing `]` anchors it), and a nonce that is a prefix
  of another (`rn-12` vs `rn-123]`) does not match — the trailing `]` discriminates.
- `AckMatchToken` is the verified bracket prefix of `AckLine(nonce, kind)`
  (`injector.go`), independent of the `received <kind>` tail, so an await-ack for
  one kind still confirms the keeper if the nonce matches. Pinned by
  `TestAckMatchToken`.
- `strings.Contains` over the whole multi-line buffer handles a mid-line /
  mid-buffer match.
- Scrollback window `-S -200` (`awaitAckScrollback`) catches an ACK that scrolled
  off the visible pane between inject and first poll, without dragging full
  history. Adequate for a 15s/1s window.

## Tests are non-vacuous + GREEN — PASS

Ran in an isolated worktree at e902114c:
- `go test ./internal/keeper/ -run AwaitAck -v` → 7 cases PASS.
- `go test ./cmd/harmonik/ -run TestKeeperAwaitAck -v` → 5 sub-cases PASS
  (exit 2 positional/bogus-flag, exit 1 missing --agent/--nonce, exit 3
  no-pane-timeout).
- `go test ./internal/keeper/... ./cmd/harmonik/...` → all packages ok.
- `go build ./...` clean; `go vet` clean; `gofumpt -l` clean.

Non-vacuity confirmed by reading the assertions:
- **Wrong-nonce** (`TestAwaitAck_WrongNonce`): pane holds `[KEEPER ACK rn-OTHER]`,
  await waits `rn-555` → genuinely takes the timeout path (asserts
  `errors.Is(err, ErrAckTimeout)` AND exactly 1 event). Real discrimination, not
  a no-op.
- **Never-appears** (`TestAwaitAck_NeverAppears`): asserts EXACTLY ONE
  `session_keeper_ack_timeout` event, and unmarshals the payload to check
  agent/nonce/kind/reason=`ack_not_observed`/timeout_seconds=0.5. Strong.
- A fake clock (`fakeClock`) advances deterministically past the deadline, so the
  timeout fires without wall-clock dependence.

## Adversarial edge cases — all handled

1. **Capture errors every poll:** consecutive errors counted; at
   `captureErrorBudget` (5) returns ErrAckTimeout naming the capture failure
   (not a clean "ack_not_observed" masquerade). `TestAwaitAck_CapturerError`
   asserts exactly 5 attempts. A successful capture resets the counter.
2. **Agent name resolves to no pane:** fast-fail before the loop; emits event
   with reason `no_tmux_target`; ErrAckTimeout → exit 3. Tested both at the core
   (`TestAwaitAck_NoTmuxTarget`) and CLI (`no-pane-timeout`) levels.
3. **timeout < poll:** `wait` is clamped to `min(poll, time.Until(deadline))`,
   so the sleep never overshoots the deadline — at most one short poll then
   timeout.
4. **ACK in the same poll as timeout expiry (off-by-one):** match is checked
   BEFORE the deadline check within each iteration, so a real ACK present on the
   timeout-poll wins (returns nil). Correct safe bias — no false-negative.
5. **ctx cancel:** `sleepCtx` returns false on `ctx.Done()` → `return ctx.Err()`
   with NO `emitAckTimeout` call. `TestAwaitAck_ContextCancel` asserts 0 events.
6. **Exit codes:** 0 success / 1 arg-or-wd error / 2 flag misuse / 3 timeout
   (incl. no-pane and repeated-capture-failure). Documented in the
   `runKeeperAwaitAck` doc comment and the usage block. Sane and consistent with
   the existing keeper CLI (flag-only, positional→2).

## Honors operator-confirmed decisions — PASS

- Defaults: `DefaultAwaitAckTimeout = 15s`, `DefaultAwaitAckPoll = 1s`; used as
  CLI flag defaults AND as the zero-fill in `AwaitAck`.
- Binary does NOT call comms — only `emitAckTimeout` (durable event) + non-zero
  exit. The comms alert is explicitly the caller/skill's job (design §3); no
  hardcoded `--from <lane>` footgun. Verified: no `comms` reference in
  awaitack.go or runKeeperAwaitAck.
- Event name exact: `session_keeper_ack_timeout`. Exit 3 on timeout.

## Non-collision with hk-vpnp — PASS

New files only; zero edits to `cycle.go` / `watcher.go` / `restartnow.go` /
`injector.go`. Additive event type + payload + one CLI subcommand. Matches the
design's non-collision guarantee.

## Minor, non-blocking observations

- A ctx-cancel that lands DURING the real `capture()` (CommandContext kills the
  tmux child) surfaces as a capture error and counts toward the 5-budget rather
  than returning `ctx.Err()` immediately; the next `sleepCtx` then catches the
  cancel. Function still terminates promptly; not a correctness bug. The fake
  capturer can't exercise this path, so it's untested — acceptable.
- The CLI test logs a benign `FileEmitter: open events.jsonl` warning because the
  temp project dir has no `.harmonik/events/` tree; emit is best-effort and the
  return value (exit 3) is the authoritative signal, so the test is unaffected.

## tmux hygiene

9 sessions before the test run, 12 after — the 3 new sessions are live
`*-flywheel` sessions spawned by the running fleet at ~09:00, NOT leaked by the
review (all committed tests use fake capturers / resolve no live tmux). No live
tmux smoke was run. Review worktree at /tmp/review-hkuldg removed after testing.

---

**APPROVE.** Match logic is exact and cross-cycle-safe, tests are non-vacuous and
green, all adversarial edges (error budget, no-pane, timeout<poll, off-by-one,
ctx-cancel-no-event) are correctly handled, and every operator-confirmed decision
(15s/1s, no-comms, `session_keeper_ack_timeout`, exit 3) is honored. Safe to merge.
