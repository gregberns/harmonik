# 07 — Tasks (tasks pass): codex-app-server Phase 1 (surface-before-build)

> codename:codex-app-server · parent epic hk-q3ovr · piter (Codex lane)
> Operator-RATIFIED direction 2026-07-11 (via admiral/captain). Supersedes the original
> "build client+sidecar then test" shape. Authority: `plans/2026-07-11-codex-app-server-replan/
> PHASE-1-tap-serializer-reactor.md` (gates, test taxonomy, token-budget). The spec-draft (05) and
> integration (06) passes are subsumed by that ratified plan doc — it is the normative reference.

## Principle (why this order)

Build the layers **closest to the wire first**, tested against **real captured frames**, before
anything downstream exists. The design's method names were summarized from a web fetch, not read off
source — so verify the contract by connecting, THEN record, THEN build. 95%+ of tests are zero-token
by construction (replay, not live). `make test` defaults `CODEX_LIVE=0`.

## Task List (each gate must pass before the next starts)

### T0 — Auth spike (OPTIONAL starter, throwaway)
- **What:** ~100-line stdio client: launch `codex app-server` → `initialize` → `thread/start` → one
  trivial turn → read to `turn/completed`.
- **Why:** settles (a) `~/.codex/` subscription auth carries into app-server (closes design OQ-2),
  (b) the **real** wire frames (closes design OQ-7). Byproduct = first trustworthy capture.
- **Deliverables:** throwaway client (deleted after) + first raw `.jsonl` capture.
- **Acceptance / exit:** auth confirmed + first raw frames captured, OR skipped if we drive via the
  tap directly. The one sanctioned live task in Phase-1 prep; token-capped.
- **Depends on:** none.

### T1 — Tap (capture layer)
- **What:** transparent stdio splice teeing both directions to raw `.jsonl` verbatim — never parsed
  for meaning, never drops a line. Any client drives it (incl. the OpenAI VS Code extension).
  Index/correlation is a **separate read-only pass** so a wire surprise corrupts the index, never the
  capture.
- **Deliverables:** the tap binary/lib + raw capture format.
- **Gate (acceptance):** a real client through the tap for one happy turn — child works E2E AND the
  concatenated captured `raw` bytes diff-match an untapped control run. Transparent + lossless proven.
- **Depends on:** none (T0 optional-feeds first fixtures).

### T2 — Serializer (wire-contract) — ★ FIRST CHECKPOINT
- **What:** split **trusted envelope** (JSON-RPC 2.0 framing, strict) from **untrusted payload** (each
  Codex method-params struct carries a preserve-and-count `Extra` for unknown fields). Method strings
  in ONE registry table (drift = one-line edit); unknown method → typed-raw, not a crash. Model only
  methods present in the corpus, optional-by-default; tighten to required only when the corpus proves
  a field always present.
- **Deliverables:** envelope+payload types, method registry, round-trip harness.
- **Gate (operator's checkpoint):** every captured real message round-trips (parse → re-serialize →
  semantic-equal), **zero unmodeled fields** (or explicitly waived w/ reason), **zero unknown
  methods.** A real message our types can't handle is a red test, not a runtime shrug. **This layer
  IS the wire-contract verification the design left open.**
- **Depends on:** T1 (needs the corpus).

### T3 — Reactor (stream-consumption)
- **What:** one seam `EventSource → Reactor → Effector`. Reactor consumes **typed** events, fed
  identically by live socket / replay fixture / synthetic scenario (indistinguishable). Every side
  effect is an `Action` through one interface; `FakeEffector` records them. The two hard invariants —
  one-turn-in-flight backpressure + dedup-by-seq — live here as **pure state**, so
  reconnect/backpressure are proven offline. Answers the design's open "where does the translation
  brain live": a thin Go state machine.
- **Deliverables:** EventSource/Reactor/Effector interfaces, FakeEffector, scenario library scaffold.
- **Gate:** growing scenario `.jsonl` (happy, tool-call-mid-turn, error, cancel, reconnect-resume,
  token-pressure, out-of-order/dup) each asserts the reactor's action sequence.
- **Depends on:** T2 (consumes typed events).

### T4 — Digital twin (supporting)
- **What:** fake app-server replaying captured `.jsonl` over the **real transport**, with fault
  injection (`--drop-after` / `--stall` / `--truncate` / `--dup`). Capture once, replay forever. The
  `daemon-testbed` harness specialized to Codex.
- **Deliverables:** twin binary + fault-injection flags.
- **Gate:** twin drives T3's reactor through every fault mode; injected faults produce the asserted
  reactor actions.
- **Depends on:** T1 (fixtures), T3 (consumer to drive).

### T5 — Test-taxonomy wiring
- **What:** wire the L0–L3 taxonomy and the make targets.
  - L0 unit — serializer round-trip / golden / malformed — never live.
  - L1 contract — behavior vs captured `.jsonl` via the twin — never live.
  - L2 integration — reactor + faked comms/queue/beads — never live.
  - L3 live — ≤4 scenarios, env-gated `CODEX_LIVE=1`, token-capped — wire-contract canaries only.
- **Deliverables:** `make test` (defaults `CODEX_LIVE=0`), `make capture-fixtures` (deliberate,
  budget-capped, ledgered), a single pre-deploy drift canary. L3 happy-path = PRE-DEPLOY E2E GATE.
- **Gate:** L0/L1/L2 green against real captured frames with `CODEX_LIVE=0`.
- **Depends on:** T2, T3, T4.

## Dependency Graph
```
T0 (optional, no dependents)
T1 → T2 → T3 → T4 → T5
              T3 ───────→ T4
        T2, T3, T4 ─────→ T5
```
Valid DAG. T1→T2→T3 is the critical path; T4 needs T1+T3; T5 needs T2+T3+T4.

## Parallelization Plan
Mostly serial by gate (each layer's gate gates the next). Opportunistic concurrency: once T1 lands,
fixture-harvesting (driving more scenarios through the tap) runs alongside T2/T3 build. T4's
fault-injection flags can be scaffolded in parallel with T3 once the twin's replay core exists.

## Phase-1 exit = the operator's ask
tap + serializer + reactor + twin + L0/L1/L2 green against real frames. **Report how hard it was and
how long it took** — the signal for whether the whole app-server bet is worth Phase 2.

## Explicitly DEFERRED (NOT Phase 1)
- **hk-nzzos** — persistent client + supervised sidecar + reconnect/backpressure/watchdog ("the real
  cost"). Built LAST, sized to what the live suite shows actually breaks.
- **hk-l63b9 / hk-lrf30 / hk-8efdl / hk-0ysh3 / hk-6z72r** — crew-start routing + launch-spec +
  boot-seed + session-id + keeper-branch (the *client-path* wiring). Wait on Phase-1 evidence.
  **hk-l63b9 stays PARKED.**
- **Spike B** — "can Codex actually orchestrate" load-bearing live proof. Own throwaway spike; gates
  committing to the whole path. **Flag to operator before Phase 2.**

## Bead mapping (created this pass)
NEW Phase-1 build beads, label `codename:codex-app-server`, under epic hk-q3ovr (parentage via
label/description — NOT a blocking dep on the open epic), chained per the DAG:
- **T0** hk-8wqsf (P2, optional, ready) · **T1** hk-893ct (P1, ready) · **T2** hk-tg5mo (P1, blocks-on T1)
- **T3** hk-5co9a (P1, blocks-on T2) · **T4** hk-swc8p (P2, blocks-on T1+T3) · **T5** hk-oe86p (P1, blocks-on T2+T3+T4)
Distinct from the deferred client-path beads (hk-l63b9/lrf30/8efdl/0ysh3/6z72r/nzzos).
