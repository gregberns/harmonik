# codex-app-server — Phase 1 plan: tap / serializer / reactor

> **Status:** operator-ratified direction, 2026-07-11. Supersedes the "build the client+sidecar then
> test" shape of the original `codex-app-server` design. This is the reference the kerf re-draft works
> from. Source: admiral 5-agent fan-out synthesis (capture layer, serializer layer, reactor layer,
> test taxonomy, adversarial critic).

## Why the replan

Agents fail against 3rd-party surfaces by winging it — building the whole mechanism live, assuming the
wire contract, then hacking endlessly and burning tokens re-running live each retry. The fix: build the
layers **closest to the wire first**, tested against **real captured data**, before anything downstream
exists. The naive "record first" has a trap — you can't record until you connect + authenticate, and the
design's method names were *summarized from a web fetch*, not read off source. So verify the contract by
connecting, THEN record, THEN build.

## Optional starter: auth spike (operator: "fine to start here")

~100-line throwaway stdio client: launch `codex app-server`, `initialize`, `thread/start`, one trivial
turn, read to `turn/completed`. Settles (1) subscription auth via `~/.codex/` carries into app-server,
and (2) the **real** wire frames. Byproduct = the first trustworthy capture. Delete after.

## Phase 1 — the three-layer build (THIS is what we build first; see how hard/long it is)

### 1. Tap (capture layer)
app-server = newline-delimited JSON over stdio → a line is a frame. A transparent splice tees both
directions to raw `.jsonl` verbatim (never parsed for meaning, never drops a line). Any client can drive
it (incl. the OpenAI VS Code extension) to harvest real scenarios cheaply. Indexing/correlation is a
**separate read-only pass** so a wire surprise corrupts the index, never the capture.
- **Gate:** run a real client through the tap for one happy turn; the child works end-to-end AND the
  concatenated captured `raw` bytes match the real exchange (diff vs an untapped control run). Tap
  proven transparent + lossless.

### 2. Serializer (wire-contract layer) — FIRST CHECKPOINT
Split **trusted envelope** (JSON-RPC 2.0 framing, strict) from **untrusted payload** (Codex method
params — each struct carries a preserve-and-count `Extra` for unknown fields). Method strings live in
ONE registry table (drift = one-line edit); unknown method → typed-raw, not a crash. Model payload
structs only for methods present in the corpus, optional-by-default; tighten to required only when the
corpus proves a field is always present.
- **Gate (operator's checkpoint):** every captured real message round-trips (parse → re-serialize →
  semantic-equal), **zero unmodeled fields** (or explicitly waived w/ reason), **zero unknown methods**.
  A real message our types can't handle is a red test, not a runtime shrug. This layer IS the
  wire-contract verification the design left open.

### 3. Reactor (stream-consumption layer)
One seam: `EventSource → Reactor → Effector`. Reactor consumes **typed** events (fed identically by live
socket, replayed fixture, or synthetic scenario — indistinguishable). Every side effect is an `Action`
through one interface; `FakeEffector` records them. Test shape is always "given this input stream, these
actions fired in this order" — zero live effects, sub-ms. The two hard invariants (one-turn-in-flight
backpressure, dedup-by-seq) live here as pure state → reconnect/backpressure proven offline with crafted
streams. Answers the design's open "where does the translation brain live": a thin Go state machine.
- **Gate:** a growing library of scenario `.jsonl` (happy, tool-call-mid-turn, error, cancel,
  reconnect-resume, token-pressure, out-of-order/dup) each asserts the reactor's action sequence.

### Supporting: digital twin
Fake app-server replaying captured `.jsonl` over the real transport, with fault injection
(`--drop-after`/`--stall`/`--truncate`/`--dup`) to exercise reconnect/backpressure with no live server.
Capture once, replay forever.

## Test taxonomy (95%+ zero-token by construction)
- **L0** unit — serializer round-trip/golden/malformed — never live.
- **L1** contract — behavior vs captured `.jsonl` via the twin — never live.
- **L2** integration — reactor + faked comms/queue/beads — never live.
- **L3** live — ≤4 scenarios, env-gated `CODEX_LIVE=1`, token-capped — ONLY the wire-contract canaries.
- **Rule:** a test may hit real Codex only to verify the wire contract itself; everything verifying OUR
  logic is a replay test. `make test` defaults `CODEX_LIVE=0`. Capture = deliberate, budget-capped,
  ledgered `make capture-fixtures`. A single drift canary re-checks live wire shape pre-deploy. This IS
  the `daemon-testbed` harness specialized to Codex; L3 happy-path = the PRE-DEPLOY E2E GATE artifact.

## Explicitly DEFERRED to a later phase (not Phase 1)
- The **persistent client + supervised sidecar + reconnect/backpressure/watchdog** (`hk-nzzos`, the
  design's "real cost") — built LAST, sized to what the live suite shows actually breaks. Server-side
  state makes a dropped client a cheap `thread/resume`, so resilience matters less than for Claude crews.
- The **"can Codex actually orchestrate" bet** (Spike B) — the load-bearing live proof — can run as its
  own throwaway spike; not required to build the tap/serializer/reactor, but it gates committing to the
  whole path. Flag to operator before Phase 2.

## Phase 1 success = the operator's ask
Build tap + serializer + reactor + twin + L0/L1/L2 tests against real captured frames. Report **how hard
it was and how long it took** — that's the signal for whether the whole app-server bet is worth Phase 2.
