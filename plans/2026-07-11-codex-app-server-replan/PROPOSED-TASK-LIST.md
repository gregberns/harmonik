# codex-app-server — re-scoped Phase 1 task list (SURFACE-BEFORE-BUILD)

> Drafted by captain from `PHASE-1-tap-serializer-reactor.md` per operator decision 2026-07-11.
> **Nothing is built until the operator OKs this list.** Owner lane: **piter** (Codex lane).
> Sequenced by the plan's gates — each task's gate must pass before the next starts.

## T0 — Auth spike (OPTIONAL starter, throwaway)
~100-line stdio client: launch `codex app-server` → `initialize` → `thread/start` → one trivial turn →
read to `turn/completed`. Settles (a) `~/.codex/` subscription auth carries into app-server, (b) the
**real** wire frames. Byproduct = first trustworthy capture. **Delete after.**
- Exit: auth confirmed + first raw frames captured, OR spike skipped if we drive via the tap directly.

## T1 — Tap (capture layer)
Transparent stdio splice that tees both directions to raw `.jsonl` verbatim (never parsed for meaning,
never drops a line). Any client can drive it. Index/correlation is a **separate read-only pass**.
- **Gate:** real client through the tap for one happy turn — child works E2E AND concatenated captured
  `raw` bytes diff-match an untapped control run. Transparent + lossless proven.

## T2 — Serializer (wire-contract) — ★ FIRST CHECKPOINT
Split **trusted envelope** (JSON-RPC 2.0 framing, strict) from **untrusted payload** (each struct carries
preserve-and-count `Extra`). Method strings in ONE registry table; unknown method → typed-raw, not crash.
Model only methods present in the corpus, optional-by-default; tighten to required only when the corpus
proves a field always present.
- **Gate (operator's checkpoint):** every captured real message round-trips (parse → re-serialize →
  semantic-equal), **zero unmodeled fields** (or explicitly waived w/ reason), **zero unknown methods.**

## T3 — Reactor (stream-consumption)
One seam: `EventSource → Reactor → Effector`. Reactor consumes **typed** events (live socket / replay
fixture / synthetic — indistinguishable). Every side effect is an `Action` through one interface;
`FakeEffector` records them. Invariants — one-turn-in-flight backpressure + dedup-by-seq — live here as
pure state.
- **Gate:** growing scenario `.jsonl` library (happy, tool-call-mid-turn, error, cancel, reconnect-resume,
  token-pressure, out-of-order/dup) each asserts the reactor's action sequence.

## T4 — Digital twin (supporting)
Fake app-server replaying captured `.jsonl` over the real transport, with fault injection
(`--drop-after` / `--stall` / `--truncate` / `--dup`). Capture once, replay forever.

## T5 — Test taxonomy wiring
- **L0** unit — serializer round-trip / golden / malformed — never live.
- **L1** contract — behavior vs captured `.jsonl` via the twin — never live.
- **L2** integration — reactor + faked comms/queue/beads — never live.
- **L3** live — ≤4 scenarios, env-gated `CODEX_LIVE=1`, token-capped — wire-contract canaries only.
- `make test` defaults `CODEX_LIVE=0`; `make capture-fixtures` deliberate + budget-capped + ledgered.

## Phase-1 exit = operator's ask
tap + serializer + reactor + twin + L0/L1/L2 green against real frames. **Report how hard + how long** —
that is the signal for whether the whole app-server bet is worth Phase 2.

## Explicitly DEFERRED (NOT Phase 1)
- **hk-nzzos** — persistent client + supervised sidecar + reconnect/backpressure/watchdog ("the real
  cost"). Built LAST, sized to what the live suite shows actually breaks.
- **Spike B** — "can Codex actually orchestrate" load-bearing live proof. Own throwaway spike; gates
  committing to the whole path. **Flag to operator before Phase 2.**
