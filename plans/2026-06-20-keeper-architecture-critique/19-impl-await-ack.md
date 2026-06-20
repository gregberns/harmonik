# Impl — agent-side ACK handshake `harmonik keeper await-ack` (hk-uldg)

Implements the design in `18-design-agent-side-ack.md`, option (a): a CLI
subcommand with an injectable pane-capturer so the timer/poll/match logic lives
in tested Go, not skill prose.

## What was built

- **`internal/keeper/awaitack.go`** — `AwaitAck(ctx, AwaitAckConfig, Emitter)`.
  Resolves nothing tmux-specific itself; takes an already-resolved `TmuxTarget`.
  Polls a `PaneCapturer` every `Poll` for the EXACT token
  `AckMatchToken(nonce)` = `[KEEPER ACK <nonce>]` (bracket token, not bare nonce
  → no cross-cycle false match). Match → nil (exit 0). Timeout → emits
  `session_keeper_ack_timeout` and returns `ErrAckTimeout`.
  - `PaneCapturer func(ctx, target) (string, error)` is the injectable seam;
    production default `CaptureTmuxPane` runs `tmux capture-pane -p -t <t> -S -200`
    (bounded scrollback tail catches a fast ACK). `Now` is injectable for a
    deterministic clock.
  - Empty target → fast `no_tmux_target` event + ErrAckTimeout (nothing to watch).
  - Bounded capture-error budget (5) so a flaky capturer fails as a real error,
    not a clean timeout. Context cancel → returns ctx.Err(), emits NO event.
  - Defaults `DefaultAwaitAckTimeout = 15s`, `DefaultAwaitAckPoll = 1s`
    (operator-confirmed). The binary does NOT send comms — caller owns that.
- **`internal/core/eventtype.go`** — new `EventTypeSessionKeeperAckTimeout =
  "session_keeper_ack_timeout"`.
- **`internal/core/keeperevents.go`** — `SessionKeeperAckTimeoutPayload`
  {agent_name, nonce, kind, timeout_seconds, tmux_target, reason}; reason ∈
  {`ack_not_observed`, `no_tmux_target`}.
- **`cmd/harmonik/keeper_cmd.go`** — `runKeeperAwaitAck`: flags
  `--agent --nonce [--kind restart|ping] [--timeout 15s] [--poll 1s] [--project]`.
  Exit codes 0 alive / 1 arg error / 2 flag misuse (flag-only) / 3 timeout.
  Plus `await-ack` lines in `keeperTopUsage`.
- **`cmd/harmonik/main.go`** — `case "await-ack"` dispatch.

**Untouched** (hk-vpnp collision-avoidance): `cycle.go`, `watcher.go`,
`restartnow.go`, `injector.go`. New files + 3 additive edits only.

## Tests (deterministic, no live tmux)

`internal/keeper/awaitack_test.go` — fake `PaneCapturer` + fake clock:
1. ACK present on 1st poll → nil, 0 events, exactly 1 capture.
2. ACK on 3rd poll → nil, ≥3 captures, 0 events.
3. ACK never appears → ErrAckTimeout, exactly 1 `session_keeper_ack_timeout`
   event with reason `ack_not_observed`, payload asserted.
4. Wrong nonce in pane (`rn-OTHER`) → still times out (no false match).
5. Capturer error every poll → trips error budget (5 attempts), timeout event.
6. Empty target → fast `no_tmux_target` event + ErrAckTimeout.
7. Context cancel → ctx.Err(), NO timeout event.
8. `AckMatchToken` shape pinned; substring of both restart/ping ACK lines.

`cmd/harmonik/keeper_await_ack_hkuldg_test.go` — CLI exit-code mapping:
positional→2, bogus flag→2, missing --agent→1, missing --nonce→1, no-pane
+tiny-timeout→3 (resolves no pane → fast path, touches no live tmux).

## Green

`go build ./...` ✓ · `go vet ./internal/... ./cmd/...` ✓ · `go test
./internal/keeper/... ./cmd/harmonik/...` ✓ · `gofumpt -l` clean. No live-tmux
smoke run; `tmux ls` = 12 before and after (unchanged).
