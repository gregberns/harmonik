# 04-Design — M2-2: the structured-protocol driver IS the Codex app-server driver

> Confirms the re-scope: the structured input path is the **already-built, proven,
> subscription-compatible Codex app-server driver** — NOT a claude structured driver.
> This design pass CONFIRMS what exists and pins the concrete residual wiring; it does
> not invent a new driver.
>
> **Supersession (flag to planner):** the sibling `driver-design.md` designs a
> `claudewire`/`claudereactor`/`claudedriver` cohort driving `claude --input-format
> stream-json --output-format stream-json -p` behind a `CLAUDE_LIVE=1` spike. That path
> is what the re-scope REJECTS (`-p`/API-key breaks subscription-first; stream-json
> carries no request ids → positional ack has a genuine false-ack risk, its own §3/§8
> abort criteria). This M2-2 design replaces it with the codex path, which is the design
> the AIS architecture was actually modeled on. `driver-design.md` should be marked
> superseded-by-this-file at finalize.

## 0. Why codex is the structured path (and claude is not)

| Axis | Codex app-server (THIS driver) | Claude structured (rejected) |
|---|---|---|
| Auth | codex subscription (same as codex CLI) — **subscription-safe** | needs `-p` + API key — breaks subscription-first |
| Wire | JSON-RPC 2.0 with **real request ids** → trivial, sound ack correlation | stream-json, **no ids** → positional ack, false-ack risk (D9 abort) |
| Codec | **frozen + corpus-round-trip proven** (`codexwire`, drift canary green) | provisional, spike-gated freeze |
| Reactor | **landed + tested** (`codexreactor` over `substrate.Run`) | to be built (`claudereactor`) |
| Replay | **landed** (`codexdigitaltwin.codexCodec` = `ReplayCodec[Event]`) | to be built (`claudetwin`) |
| Test taxonomy | **landed** L0–L3 + drift canary + L3 live E2E gate | to be built (`claudetest`) |

Claude stays on the tmux paste path (M2-3) by design. Codex is the structured path.

## 1. What ALREADY EXISTS vs what M2-2 must wire

### DONE (proven, LANDED)
- **Generic seam:** `substrate.Run[E,A]` (`internal/substrate/seam.go:27`), `EventSource[E]`/`Effector[A]` (`seam.go:7,13`), `Twin[E]`+`ReplayCodec[E]`+`FaultConfig` (`replay.go:59,82`), `ClockPort` (`clock.go`).
- **Concrete E/A:** `codexreactor.Event` (`reactor.go:66`), `codexreactor.Action` (`reactor.go:106`), aliases `EventSource = substrate.EventSource[Event]` (`reactor.go:145`), `Effector = substrate.Effector[Action]` (`reactor.go:130`).
- **Pure Step + Run:** `Reactor.Step` (`reactor.go:193`, invariants I1 one-turn-in-flight, I2 dedup-by-seq), `Reactor.Run` = one-line wrapper over `substrate.Run` (`reactor.go:297-299`).
- **Wire codec:** `codexwire` — strict JSON-RPC envelope, tolerant unknown method (`FrameKindRaw`), single-source `methodRegistry` (`codexwire.go:92-163`), Parse/Marshal round-trip gate.
- **ReplayCodec fit (P1 D2):** `codexdigitaltwin.codexCodec` implements `substrate.ReplayCodec[codexreactor.Event]` (`twin.go:85-124`): `DecodeLine` = `codexwire.Parse` + `frameToEvent` (`twin.go:94`); `ErrorEvent`/`DisconnectEvent` supplied (`twin.go:113,122`). **The codec is already the P1 D2 shape — nothing to build here.**
- **Test taxonomy:** `codextest` L0 wire, L1 corpus-contract, L2 reactor+bridge, L3 live E2E (`CODEX_LIVE=1`) + drift canary (`canary_hkoe86p_test.go`), all green vs a real captured corpus at `CODEX_LIVE=0`.
- **Twin-blind double:** `cmd/harmonik-twin-codex` (the swap target for the twin-blind acceptance).
- **Live driver, IN TEST FORM:** `l3_live_hkoe86p_test.go:59-198` spawns real `codex app-server`, `StdinPipe`/`StdoutPipe`, drives `initialize → initialized → thread/start → turn/start → turn/completed`. This is the exact production shape — but hand-rolled inline in a test, not extracted.

### RESIDUAL (what M2-2 must build) — all wiring, no new abstractions
- **R1 — production `LiveSource` (`EventSource[Event]`):** extract the L3 stdout loop (`l3_live_hkoe86p_test.go:108-140`) into a reusable source: `bufio.Scanner(stdout, 1 MiB)` → `codexCodec.DecodeLine` → `Event` channel closed on EOF/ctx. **Reuses the SAME `codexCodec`** — `DecodeLine([]byte)` is transport-agnostic (corpus line or live stdout line are identical bytes), so replay and live share one codec (P1 D2 delivered). Only the byte source differs (corpus `io.Reader` vs `stdout` pipe).
- **R2 — production `Effector`:** an `Effector` (=`substrate.Effector[Action]`) mapping `Action → daemon bus/queue/beads`. Today only `codexreactor.FakeEffector` (`fake.go`) and the test-only `HarmonikBridgeSink` (`codextest/l2_integration_hkoe86p_test.go`) exist. Production map: `EmitOutput`→agent-output stream, `CompleteTurn`→`outcome_emitted`, `EmitError`→run failure, `NotifyTokenUsage`→bandwidth tuner (PI-073 shape), `NotifyStatus`→presence.
- **R3 — client-request writer / stdin owner (= M2-1 InputPort, codex instantiation):** the `initialize`/`thread/start`/`turn/start` submission path that HOLDS the subprocess stdin pipe and writes frames via `codexwire.Marshal`. The reactor is READ-ONLY (server→client). This writer is the actual agent INPUT and IS the codex instantiation of M2-1's typed input op + ack contract. **HARD-depends on M2-1.**
- **R4 — composition-root transport selection + twin-blind wiring** (see §4).
- **R5 — WAL-guard fold (rides M2-7):** graceful stdin-close shutdown removes the ungraceful-kill class the codex WAL-guard compensates for; app-server driver makes it adapt/redundant.

## 2. Concrete E/A (resolves bullet 1)

Already concrete — no new types:

```
E = codexreactor.Event   (reactor.go:66)  — flat, JSON-round-trippable; Seq + Type + payload
A = codexreactor.Action  (reactor.go:106) — flat, JSON-comparable; Type + payload
step  = Reactor.Step      (reactor.go:193) — pure (state,event)→(state,[]action)
loop  = substrate.Run[Event,Action](ctx, src, r.Step, eff)  (reactor.go:298 → seam.go:27)
```

Event vocab (server→client notifications): `Connected/Disconnected` (Seq=0, dedup-bypass), `TurnStarted/TurnCompleted`, `MessageDelta`, `ThreadStatus`, `TokenUsage`, `Error` (`reactor.go:38-56`).
Action vocab: `CompleteTurn`, `EmitOutput`, `EmitError`, `NotifyStatus`, `NotifyTokenUsage` (`reactor.go:84-100`).

The reactor covers the **read/observe** half in full. The **write/input** half (client requests) is NOT in the reactor — it is R3 (the InputPort), correlated to the read half by JSON-RPC request id.

## 3. ReplayCodec[E] (P1 D2) fit (resolves bullet 2) — DONE

`codexdigitaltwin.codexCodec` (`twin.go:85`) already IS `substrate.ReplayCodec[codexreactor.Event]`:
- `DecodeLine(line)` = `codexwire.Parse(line)` then, for `FrameKindServerNotification` only, `frameToEvent` (`twin.go:94-108`); client frames / server responses → skip (`emit=false`); parse failure → fatal `err` (twin emits `ErrorEvent`).
- `ErrorEvent(msg)` → `EventTypeError` carrying current seq (`twin.go:113`); `DisconnectEvent()` → `EventTypeDisconnected` Seq=0 (`twin.go:122`).
- Fault matrix (`FaultDropAfter/Stall/Truncate/Dup`) already exercises the reactor via `substrate.Twin` (`replay.go:191`).

**M2-2 change here = ZERO.** The one seam decision: the production `LiveSource` (R1) SHOULD call the same `codexCodec.DecodeLine` so live and replay decode identically (single decode path → replay fidelity is structural, not asserted). Recommend promoting `codexCodec` from unexported (`twin.go:85`) to an exported constructor reused by both `Twin` and `LiveSource`, OR co-locating `LiveSource` in `codexdigitaltwin` so it keeps package-private access. No behavior change either way.

## 4. Stdin ownership + composition-root injection (resolves bullet 3)

### Stdin ownership — the seam inversion
Three stdin regimes exist in the tree today:
1. tmux-paste (claude): substrate owns the **pty**; `SendInput`/`CloseStdin` are **no-ops** (`handler/substrate.go:137-142,171-175`), input arrives via `load-buffer`/`paste-buffer`.
2. codex CLI ProcessExit: stdin = **`/dev/null`** (`StdinDevNull`, `substrate.go:64-74`; `harnessregistry.go:318`) — codex reads its task from argv, never from stdin.
3. **app-server driver (NEW, this design):** the driver owns the child's **stdin pipe DIRECTLY** — a real `exec.Cmd.StdinPipe()` carrying JSON-RPC request frames (`l3_live:69`). Not a pty, not `/dev/null`. `CloseStdin` becomes MEANINGFUL: it is the graceful end-of-input / drain signal.

So the app-server driver is the FIRST consumer that needs the real (non-no-op) `SendInput`/`CloseStdin` semantics the current `SubstrateSession` explicitly disclaims (`substrate.go:96-99`). That is exactly what **M2-1 widens** (the typed input op + ack, retiring the `substrateSessionAdapter` no-ops). R3 plugs into M2-1's `InputPort`. Ack is clean because codex JSON-RPC carries request ids: a `turn/start` (id=N) is acked by the server's `turn/started`/`turn/completed` flowing back through the reactor — no positional guessing, no false-ack (the decisive advantage over the rejected claudewire path). The bounded-liveness analog (AIS-INV-001): `turn/start` submitted → `TurnCompleted` XOR `agent_input_stale` on the timer, never silence.

### Composition-root injection
Current selection (per agent type) at the composition root:
- `harnessRegistry` built in `newHarnessRegistry` (`harnessregistry.go:47`): claude → `buildClaudeLaunchSpec` (tmux paste), codex → `buildCodexRoutedLaunchSpec` (**codex CLI ProcessExit**, `harnessregistry.go:200-327`), pi.
- `routedLaunchSpecBuilder` resolves agent_type via `resolveHarness` (bead>queue>node>global) and dispatches (`harnessregistry.go:134-164`); the `substrate handler.Substrate` field (`workloop.go:491`) is the tmux-substrate hook.

The app-server driver does NOT fit either existing shape: it is a **long-lived, stdio-owning, reactor-driven session** (`substrate.Run` loop), not `CompletionProcessExit` (codex CLI) and not a fire-and-forget tmux `SpawnWindow`. Injection options:
- **(A) new agent_type `codex-app-server`** — cleanest selection story: `resolveHarness` already keys per agent type; register an app-server harness/driver alongside the CLI codex. Keeps the CLI codex path untouched. **Recommended.**
- (B) reuse agent_type `codex` with a transport sub-flag — muddier (two transports behind one type; resume/WAL/ProcessExit assumptions in `codexharness.go` diverge).
- (C) a `handler.Substrate` impl whose `SubstrateSession` owns the JSON-RPC connection — fits the seam field but strains `SpawnWindow` (the driver is a client loop, not a window spawn).

The driver most naturally lands on **M3-4's `runexec` reactor** (itself a `substrate.Run` state machine) as a per-agent-type driver seam, with the app-server LiveSource+Effector+InputPort injected at `workloop.go:491`/`harnessregistry.go:47`. Twin-blind acceptance = swap the real `codex app-server` subprocess for `cmd/harmonik-twin-codex` behind the same LiveSource, asserting the reactor/effector output is identical. Note the ordering dependency: full runexec landing is M3-4, but M2-2 does NOT require it — the driver can land against the current dispatch path and be migrated into `runexec` when M3-4 arrives (the reactor is the same `substrate.Run` shape either way).

## 5. Dependencies / blockers on `ready`
- **HARD: M2-1** — R3 (the client-request/stdin writer) IS M2-1's typed InputPort + ack contract instantiated for codex; the widened `SubstrateSession` input op + retired no-ops (`substrate.go:137-175`) are M2-1's deliverable. M2-2 cannot reach `ready` until M2-1 lands.
- Soft: M3-4 (`runexec`) is the preferred long-term home but NOT a blocker (driver lands against current dispatch, migrates later).
- Doc residual: mark `driver-design.md` superseded (§0).

## 6. Acceptance delta
Original card: "Codex driver swappable at the composition root; twin-blind; concrete E/A, ReplayCodec fit, stdin ownership." After this pass, E/A + ReplayCodec fit + reactor + replay + L0–L3 are **DONE/proven**; the acceptance narrows to the four wiring residuals R1 (LiveSource), R2 (production Effector), R3 (client-request/stdin writer = M2-1 codex instance), R4 (composition-root selection + twin-blind swap), with R5 (WAL-guard) riding M2-7.
