# 04-Design — driver (C2 structured-protocol driver; C7 WAL-guard homing)

> Elaborates D3/D4/D10 within `00-decisions.md`. Code facts from
> `03-research/driver/findings.md`. The generics spine instantiated here is LANDED:
> `internal/substrate/seam.go:7-36` (EventSource/Effector/Run), `replay.go` (Twin/
> ReplayCodec/FaultConfig), `clock.go:10-18` (ClockPort). Reactor idiom:
> `internal/keeper/step.go` + `internal/codexreactor/reactor.go`.

## 1. Package layout (D3)

| Package | Role | Template |
|---|---|---|
| `internal/claudewire` | JSONL frame codec + type registry | `internal/codexwire` (registry `codexwire.go:92-163`; unknown→Raw `:16-18`) |
| `internal/claudereactor` | Event/Action/`Step`; substrate aliases; `Run` wrapper | `codexreactor/reactor.go` (aliases `:130,:145`; Run `:297-299`) |
| `internal/claudedriver` | shell: spawn, pipes, tee, Submit correlation, timers, shutdown; implements `handler.Substrate`+session | keeper `shell.go` (effector switch `:35-128`) — but live loop routes through `substrate.Run` |
| `internal/claudetwin` | `claudeCodec` = `substrate.ReplayCodec[claudereactor.Event]`; frameToEvent | `codexdigitaltwin/twin.go:94-230` |
| `internal/claudetest` | L0–L3 + canary | `internal/codextest` |

depguard: `claudewire`/`claudereactor`/`claudetwin` = stdlib+substrate leaf rules (mirror the
substrate leaf rule, P1 T1); `claudedriver` may import handler + apptap + substrate +
claudewire/claudereactor, NEVER `internal/lifecycle/tmux` (grep+depguard gate = SC3 teeth).

## 2. The wire protocol (spike-gated — D3)

**PROVISIONAL frame model — every row below is confirmed-or-corrected by the T0 spike before
`claudewire` freezes** (no in-tree evidence exists for `--input-format stream-json`; driver
findings §5).

Input frames (driver→claude stdin, one JSON object per line):
- `user_message` — `{"type":"user","message":{"role":"user","content":[{"type":"text","text":<Body>}]}}`
  — carries InputSeed / InputResumeBrief bodies.
- control/shutdown — spike determines: control frame vs stdin close (fallback: stdin close =
  graceful end-of-input).

Output frames (claude stdout → driver):
- `system/init` — session id, model, tools (handshake-complete anchor).
- `assistant` / `stream_event` (with `--include-partial-messages`) — turn output; the FIRST
  such frame after a submitted `user_message` is the **ack anchor** (D2).
- `result` — turn terminal (cost/duration; success/error subtype) → `TurnCompleted`.
- unknown → `FrameKindRaw` preserved (codex idiom), NEVER fatal on the live path.

Codec rules copied from codexwire: strict envelope decode, tolerant unknown type, Marshal/
Parse round-trip gate (L0), single-source registry so adding a frame type = one entry.

## 3. Submit correlation — how the ack becomes real (D2)

stream-json carries no request ids (unlike codex JSON-RPC), so correlation is **positional
within a session**: the driver serializes inputs per session (I1-analog: ONE uncorrelated
input in flight; `Submit` calls queue) and attributes the first turn-opening output frame
after write-flush to the in-flight `MsgID`. This is exactly the invariant the reactor
enforces (state `AwaitingAck` refuses a second uncorrelated write — `Step` returns a
`Reject` action mapped to a queued retry in the shell). The spike MUST verify positional
attribution is sound (no unsolicited turn-opening frames interleave; if they can, the spike
finds the discriminator — e.g. echoed user content or turn ids — and the codec pins it).
False-ack is a D9 abort criterion precisely because this is the design's sharpest assumption.

## 4. The reactor (D4)

States: `Spawning → Handshaking → Ready → AwaitingAck → InTurn → {Draining | Exited}`.

Events (flat, JSON-round-trippable; `At` clock-stamped by shell): `ProcStarted`, `InitSeen`,
`SubmitRequested{MsgID,Kind}` (shell-injected when a caller parks on Submit), `FrameSeen{...}`
(post-codec), `AckAnchorSeen{MsgID}`, `TurnCompleted`, `TimerFired{kind}` (`ack_timeout`,
`handshake_timeout`, `drain_timeout`), `StdoutClosed`, `ProcExited{code}`.

Actions: `WriteFrame{MsgID,frame}`, `ResolveSubmit{MsgID, ok|stale}` (the shell completes the
parked caller), `ArmTimer`/`CancelTimer`, `EmitEvent{type,payload}` (durable events §6),
`CloseStdin`, `Kill`, `RecordTerminal{reason}`.

Invariants (mirroring codex I1/I2, `reactor.go:11-19`): **I1'** one uncorrelated input in
flight per session; **I2'** frame ordering is stdout order (no seq — single pipe, no dedup
need; stated explicitly rather than inherited). Step is TOTAL and pure — no io/clock/id-mint
(keeper `step.go:16-22` discipline verbatim). Timers-as-events per P1 D11.

**Ack-or-stale (IN-INV-001):** `AwaitingAck` arms `ack_timeout` (30s default, config);
`TimerFired(ack_timeout)` → `ResolveSubmit{stale}` + `EmitEvent{input_stale}` + transition
`Ready` (session usable for retry) — never silence, never a wedged park. `Handshaking` and
`Draining` have their own bounds (`handshake_timeout` 60s — replaces the split 60s/180s
ambiguity flagged in seam-contract findings §6; `drain_timeout` 20s → Kill).

## 5. The shell (`internal/claudedriver`)

- **Spawn:** `exec.CommandContext`; argv = `buildClaudeLaunchSpec` step-6b surface REUSED
  (`--session-id`/`--resume`, `--model`, `--effort`, HC-055b skip-permissions; CHB-007 guard)
  + `--input-format stream-json --output-format stream-json --include-partial-messages` +
  `-p` mode per spike outcome. Env = `handler.ClaudeEnvVars` unchanged (deny-list intact).
  Sanctioned by PL-021f.
- **Pipes + tee:** stdin via `apptap.CaptureWriter(stdinPipe, wireInFile)`; stdout via
  `apptap.CaptureReader(stdoutPipe, wireOutFile)` → `bufio.Scanner` (1 MiB buffer, RS-010
  parity) → codec → events channel. The scanner goroutine is the `EventSource[Event]`; the
  live loop is `substrate.Run(ctx, src, reactor.Step, shellEffector)` (deliberately the
  codexreactor shape, not keeper's hand-written drive — driver findings flag 5).
- **Submit API:** mints nothing; validates MsgID; enqueues `SubmitRequested`; parks on a
  per-MsgID channel the `ResolveSubmit` action completes. ClockPort supplies all timing; ZERO
  `time.Sleep`/`time.After` in the package (SC6 grep gate).
- **Graceful shutdown (D10a / IN-009):** `Shutdown(ctx)` = `CloseStdin` → await
  `ProcExited` within `drain_timeout` → else `Kill`. Replaces `/quit` sends for bead runs.
- **Session handle:** implements the full widened `SubstrateSession` (Kill/Wait/Outcome/PID
  delegate to the cmd; `Stdout()` returns nil — the bridge socket remains the event wire, and
  Attach uses the capture stream per HC-054 amendment).

## 6. Durable events (small additive cohort)

Three registered event types (EV registration task, mirroring P1 T2 shape):
`agent_input_submitted{msg_id,kind,run_id}`, `agent_input_acked{msg_id,latency_ms}`,
`agent_input_stale{msg_id,bound_ms}` — the joinable record the `internal/replay` IN-INV
checkers consume (harness-design §4) and the D9 metrics mine. Payloads carry `run_id`
(envelope-scoped — these ARE run-scoped, unlike P1's cycle events; no D7-style payload-key
debate needed). Emit-failure logged, not swallowed (P1 D9 hardening carried forward).

## 7. WAL-guard homing (C7 / D10)
As pinned: `codexwalguard.go` untouched; IN-009/IN-010 own the concern; the claude driver's
graceful shutdown is the structural fix for the ungraceful-kill class on the claude channel;
follow-up (deferred) ports codex to the same discipline.

## 8. Spike (T0') definition — blocking gate for wire freeze
Budget-capped (one minimal turn ×3 scenarios), env-gated `CLAUDE_LIVE=1`:
1. fresh session: init → submit → ack anchor → result; capture corpus.
2. resume (`--resume <uuid>`): brief submit → ack; verify transcript continuity.
3. stale probe: submit to a killed/hung child → confirm detectability (no false ack).
Outputs: `testdata/claude-agent/T0-findings.md` (codex T0 precedent) + first corpus files +
the confirmed/corrected §2 frame table + §3 correlation rule. Fallback decision recorded per
D3 if any scenario falsifies.
