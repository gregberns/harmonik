# Dossier: The codex app-server substrate template

**Purpose.** The codex app-server work (Phase 1, hk-893ct/tg5mo/5co9a/swc8p/oe86p) is a
built, green instance of the pattern the revamp wants to generalize into
`internal/substrate`: capture a raw event stream → layer decoding → swap any layer for a
testable fake, with a digital-twin replay and an L0–L3 test taxonomy. This documents what
IS, with exact `file:line` references and verbatim type/interface definitions, and cleanly
separates the generic seam from the codex-specific instantiation. It does not recommend.

All paths are relative to the repo root `/Users/gb/github/harmonik/`.

---

## 0. The five packages at a glance

| Layer | Package | File | Bead | One-line role |
|-------|---------|------|------|---------------|
| Raw capture | `internal/apptap` | `tap.go` | T1 hk-893ct | Transparent stdio splice; tees both directions to capture writers verbatim |
| Wire decode | `internal/codexwire` | `codexwire.go` (45 KB) | T2 hk-tg5mo | JSON-RPC 2.0 envelope + typed payload; `Parse`/`Marshal`, round-trip |
| Reactor | `internal/codexreactor` | `reactor.go`, `fake.go` | T3 hk-5co9a | Pure state machine `EventSource → Reactor.Step → []Action → Effector` |
| Digital twin | `internal/codexdigitaltwin` | `twin.go` | T4 hk-swc8p | Replays captured JSONL corpus as an `EventSource`; injects faults |
| Test taxonomy | `internal/codextest` | `l0..l3_*_test.go`, `canary_*_test.go` | T5 hk-oe86p | L0/L1/L2 zero-token + L3 live gate + drift canary |

Corpus fixture: `testdata/codex-app-server/corpus/raw-session-01.jsonl` (6.6 KB, one real
captured session). Captured via `make capture-fixtures` (`Makefile:128-133`, budget-capped,
`CODEX_LIVE=1`).

---

## 1. The layered architecture and its seams

The data-flow, drawn from the package docs (`codexdigitaltwin/twin.go:20-30`,
`codexreactor/reactor.go:22`):

```
   real codex app-server (stdio, newline-delimited JSON-RPC 2.0)
        │
        ▼  [apptap.Tap]  transparent tee, verbatim bytes
   raw .jsonl bytes  ──────────────►  InCapture / OutCapture writers  → corpus file
        │
        ▼  [codexwire.Parse]  (T2)  pure decode, no IO
   codexwire.Frame  (Kind + typed Params/Result)
        │
        ▼  [frameToEvent]  (in twin.go, T4)  frame → typed reactor event
   codexreactor.Event  (flat, JSON-round-trippable)
        │
        ▼  [codexreactor.Reactor.Step]  (T3)  pure state machine
   codexreactor.Action  (flat)
        │
        ▼  [codexreactor.Effector.Execute]
   real effect (comms/queue/beads)  OR  FakeEffector (records)  OR  HarmonikBridgeSink (L2)
```

The three named seams — the interfaces that let each layer be swapped for a fake — are all
defined in `internal/codexreactor/reactor.go`:

### Seam 1 — `EventSource` (the "what feeds the reactor" boundary)
`internal/codexreactor/reactor.go:133-137`:
```go
type EventSource interface {
	// Events returns a channel delivering events until ctx is cancelled or the
	// source is exhausted. The channel is closed when delivery is complete.
	Events(ctx context.Context) <-chan Event
}
```
Three implementations are anticipated (`reactor.go:128-137` doc): live source (wraps a
codexwire stream), replay source (the Twin), and `SyntheticSource` (a fixed slice, in
`fake.go`). The Twin is the concrete replay implementation — `codexdigitaltwin/twin.go:98`
`func (t *Twin) Events(ctx context.Context) <-chan codexreactor.Event`.

### Seam 2 — `Effector` (the "what the reactor's decisions drive" boundary)
`internal/codexreactor/reactor.go:120-122`:
```go
type Effector interface {
	Execute(ctx context.Context, a Action) error
}
```
The real effector wires the harmonik event bus + codex stdio (doc `reactor.go:117-119`);
`FakeEffector` records actions for assertions; `HarmonikBridgeSink` (L2 test) records what
the real comms/queue/beads bridge would emit.

### Seam 3 — the pure `Reactor` itself
`internal/codexreactor/reactor.go:168-176`:
```go
type Reactor struct {
	state State
}
func New() *Reactor { return &Reactor{} }
func (r *Reactor) State() State { return r.state }
```
Its whole surface is `Step(ev Event) []Action` (`reactor.go:185`, pure — "no goroutines, no
I/O, no allocations beyond the returned slice") and the driver `Run(ctx, src EventSource, eff
Effector) error` (`reactor.go:282-292`), which is a 10-line loop:
```go
func (r *Reactor) Run(ctx context.Context, src EventSource, eff Effector) error {
	for ev := range src.Events(ctx) {
		actions := r.Step(ev)
		for _, a := range actions {
			if err := eff.Execute(ctx, a); err != nil {
				return err
			}
		}
	}
	return nil
}
```
This `Run(src, eff)` triple is the heart of the swappable pattern: any `EventSource` × any
`Effector` composes.

---

## 2. apptap — what it captures and how

**What.** Every byte in both directions of a child process's stdio, verbatim — "no parsing,
no framing, no drops" (`tap.go:1-24`). Target child is `codex app-server` (a JSON-RPC 2.0
process) but the doc is explicit that "Tap is protocol-agnostic — any client or child drives
it" (`tap.go:20-21`).

**How.** The `Tap` struct (`tap.go:48-75`) holds `Binary`, `Args`, and four IO fields:
`InCapture io.Writer` (`tap.go:58`) receives caller→child bytes, `OutCapture io.Writer`
(`tap.go:63`) receives child→caller bytes; `Stdin`/`Stdout`/`Stderr` default to the OS
streams. Capture is done with `io.MultiWriter` — a passive tee, not a proxy buffer.

**Capture entry point** — `tap.go:82` `func (t *Tap) Run() error`. The tee wiring
(`tap.go:112-135`):
```go
// inDst: bytes from caller stdin go to child stdin and (optionally) InCapture.
inDst := io.Writer(childIn)
if t.InCapture != nil {
	inDst = io.MultiWriter(childIn, t.InCapture)
}
// outDst: bytes from child stdout go to caller stdout and (optionally) OutCapture.
outDst := io.Writer(stdout)
if t.OutCapture != nil {
	outDst = io.MultiWriter(stdout, t.OutCapture)
}
```

**Persistence format & location.** The tap itself writes to whatever `io.Writer` you give it
— it does NOT own a file format. Persistence to `testdata/codex-app-server/corpus/*.jsonl`
happens by pointing `OutCapture` at a file. The corpus is newline-delimited JSON-RPC lines
(one frame per line; head of `raw-session-01.jsonl` is `initialize` request → result →
`configWarning` notification …). NB: the checked-in corpus was captured by the T0 spike /
L3 harness, not by a wired `apptap` consumer — `grep` for non-test `apptap` consumers in
`cmd/` and `internal/` returns nothing; `apptap` is currently exercised only by
`tap_test.go` (`InCapture`/`OutCapture` → `bytes.Buffer`, `tap_test.go:44-54`). So the
capture library exists and is green, but is not yet spliced into a production capture path.

---

## 3. codexwire — the decode layer

**Input.** One newline-delimited JSON-RPC 2.0 line (`[]byte`). **Output.** A `Frame`
(`codexwire.go:49-69`) with a discriminated `Kind` (one of five, `codexwire.go:37-45`:
`ClientRequest`, `ClientNotification`, `ServerResponse`, `ServerNotification`, `Raw`) and
exactly one non-nil typed payload (`Params any` / `Result any` / raw `Error`).

**Is decode pure?** Yes — no IO. Imports are only `encoding/json` and `fmt`
(`codexwire.go:29-32`). `Parse` operates on a `[]byte` argument and returns `(Frame, error)`.

**Core decode signature** — `codexwire.go:183`:
```go
func Parse(line []byte) (Frame, error) {
```
Discrimination logic (`codexwire.go:210-249`) is a switch on presence of id/method/result;
an unknown method is NOT an error — it yields `FrameKindRaw` with bytes preserved verbatim
(`codexwire.go:218-227`). The inverse is `func Marshal(f Frame) ([]byte, error)`
(`codexwire.go:309`); the round-trip guarantee (`Parse→Marshal` semantically equal) is the
T2 gate.

**Two-layer design** (`codexwire.go:4-18`): (1) trusted JSON-RPC envelope, parsed strictly;
(2) untrusted per-method payload structs, each carrying an `Extra map[string]json.RawMessage`
that preserves-and-counts unmodeled fields (`ExtraCount`, `codexwire.go:460`). Method strings
live in ONE registry table `methodRegistry` (`codexwire.go:92-163`) mapping method →
`{Dir, MakeParams, MakeResult}` (`methodEntry`, `codexwire.go:81-85`). Adding a method =
one registry entry + a Params/Result type pair. This registry is the single codex-specific
drift point.

---

## 4. codexreactor + fake.go — state machine and swap

**State machine.** `State` (`reactor.go:143-157`): `ThreadID`, `TurnID`, `InFlight`,
`LastSeq`. Two invariants enforced in `Step` (`reactor.go:185-277`):
- **I1 one-turn-in-flight** — tracks `InFlight`; `Disconnected`/`Error` mid-turn terminate
  and emit `ActionTypeEmitError` (`reactor.go:205-215`, `265-272`).
- **I2 dedup-by-seq** — events with `Seq ≤ LastSeq` are dropped; `Seq=0` bypasses dedup for
  connection-lifecycle events (`reactor.go:186-192`).

`Step` is a switch over 8 `EventType`s (`reactor.go:34-52`) producing 0..1 `Action`s from 5
`ActionType`s (`reactor.go:80-96`). `Event` (`reactor.go:62-73`) and `Action`
(`reactor.go:102-112`) are deliberately **flat structs** ("all fields optional… enables JSON
round-trip for scenario files without type-switching") — this is what makes scenario `.jsonl`
diffing trivial.

**What makes the effect swappable.** The `Effector` interface (§1 seam 2). The real path and
the fake path both satisfy `Execute(ctx, Action) error`; the reactor never knows which it has.

**FakeEffector shape** — `fake.go:13-40`:
```go
type FakeEffector struct {
	mu      sync.Mutex
	actions []Action
}
func (f *FakeEffector) Execute(_ context.Context, a Action) error {
	f.mu.Lock()
	f.actions = append(f.actions, a)
	f.mu.Unlock()
	return nil
}
func (f *FakeEffector) Actions() []Action { /* returns copy */ }
func (f *FakeEffector) Reset()            { /* clears log */ }
```
Mirror on the source side: `SyntheticSource` (`fake.go:47-68`) — a fixed `[]Event` delivered
on a pre-filled, immediately-closed channel. So both seam ends have a test double in the same
`fake.go`.

---

## 5. codexdigitaltwin — how replay works

**Input.** A captured JSONL corpus as an `io.Reader` plus a `FaultConfig`
(`twin.go:88` `func New(corpus io.Reader, fault FaultConfig) *Twin`). **Output.** It *is* a
`codexreactor.EventSource` (`twin.go:92-105` implements `Events`).

**Replay driver** — `twin.go:107-195` `func (t *Twin) replay(ctx, ch)`. The loop
(`twin.go:123-194`): scan lines → `codexwire.Parse(line)` → skip non-`ServerNotification`
frames (`twin.go:144-146`) → `frameToEvent(frame, &seq)` (`twin.go:148`) → apply fault at the
configured event index → send on the channel. Parse error → emit an `Error` event and stop
(treated as fatal transport failure, `twin.go:133-140`).

`frameToEvent` (`twin.go:202-277`) is the codex-specific translation table: 5 cases
(`turn/started`, `turn/completed`, `item/agentMessage/delta`, `thread/status/changed`,
`thread/tokenUsage/updated`) each type-asserting a `codexwire.*Params` struct and building a
flat `codexreactor.Event`. Non-matching notifications return `(zero, false)` and are skipped.

**What it asserts.** The twin asserts nothing itself — it is a source. Assertions live in the
tests that drive `Reactor.Run(ctx, twin, effector)` and compare `eff.Actions()` against a
golden sequence (see §6, L1/L2). "Capture once, replay forever" (PHASE-1 plan line 58).

---

## 6. codextest — the L0–L3 taxonomy

Package doc `l0_wire_hkoe86p_test.go:1-18` defines the four tiers. Each level's file and what
it tests:

| Level | File | Tests | Live? Tokens? |
|-------|------|-------|---------------|
| **L0 unit** | `l0_wire_hkoe86p_test.go` | codexwire serializer in isolation: golden `Parse` for each frame kind (`:42`, `:66`, `:84`), `Parse→Marshal` round-trip (`:109`), malformed-input errors (`:162`), unknown-method → Raw (`:190`) | Never live. Zero tokens. |
| **L1 contract** | `l1_contract_hkoe86p_test.go` | Every corpus line: zero `FrameKindRaw` (`:43`), zero unmodeled Extra fields (`:85`), round-trips (`:128`); and twin→reactor produces the exact golden action sequence (`:181`) | Never live. Zero tokens (reads corpus file). |
| **L2 integration** | `l2_integration_hkoe86p_test.go` | Full chain `corpus → twin → reactor → HarmonikBridgeSink` (faked comms/queue/beads, `:36-150`); happy path (`:160`) + mid-turn disconnect via fault (`:242`) | Never live. Zero tokens. |
| **L3 live** | `l3_live_hkoe86p_test.go` | Real `codex app-server` subprocess: full handshake → assert `turn/completed` arrives (`:59`); userAgent/protocol-version drift canary (`:204`) | Live. Non-zero, token-capped. |
| Drift canary | `canary_hkoe86p_test.go` | Pre-deploy gate: corpus parses with zero Raw frames, non-empty, valid JSON (`:34`) | Not live (reads corpus). Zero tokens. |

**The "~95% zero-token" claim.** Substantiated: L0/L1/L2 + canary spend zero tokens (pure
functions + file replay); only L3's ≤4 scenarios hit the real server, gated behind
`CODEX_LIVE=1` and skipped by default (`l3_live_hkoe86p_test.go:30-35`). Design doc
`.kerf/works/codex-app-server/04-design/reactor-and-taxonomy-deep-design.md:255-258`:
"L0–L2 are 100% of the logic coverage and are zero-token by construction… verifying OUR logic
is a replay test." The rule (PHASE-1 plan line 65): "a test may hit real Codex only to verify
the wire contract itself."

**How the levels are wired.** `make test-codex-l012` (`Makefile:111-113`) runs L0/L1/L2 +
canary with `CODEX_LIVE=0`. `make test-codex-live` (`Makefile:119-121`) runs `-run TestL3_`
with `CODEX_LIVE=1`. The corpus path is resolved by `runtime.Caller` + relative join
(`l1_contract_hkoe86p_test.go:30-36`, `l0_wire..._test.go:33-36`), so tests are hermetic to
the checked-in fixture. L1 is the pivot: it is where the four packages compose
(`twin := codexdigitaltwin.New(f, FaultConfig{})`, `eff := &codexreactor.FakeEffector{}`,
`r.Run(ctx, twin, eff)` — `l1_contract...:196-201`).

---

## 7. Fault injection

**Where.** In the twin (`codexdigitaltwin/twin.go`), not the tests — the test just selects a
mode. Four modes, `FaultMode` (`twin.go:47-68`): `FaultDropAfter`, `FaultStall`,
`FaultTruncate`, `FaultDup`, parameterised by `FaultConfig{Mode, EventN}` (1-based event
index, `twin.go:72-75`).

**How.** Applied inside the replay loop when the emitted-event count reaches `EventN`
(`twin.go:157-189`):
```go
if t.fault.Mode != FaultNone && evIdx == t.fault.EventN {
	switch t.fault.Mode {
	case FaultDropAfter:
		if !send(ev) { return }
		disc := codexreactor.Event{Seq: 0, Type: codexreactor.EventTypeDisconnected}
		send(disc)
		return
	case FaultStall:
		<-ctx.Done()
		return
	case FaultTruncate:
		errEv := codexreactor.Event{Seq: seq, Type: codexreactor.EventTypeError, Message: "twin: truncated frame"}
		send(errEv)
		return
	case FaultDup:
		if !send(ev) { return }
		if !send(ev) { return }   // reactor I2 dedup must drop the copy
		continue
	}
}
```

**Example use** — L2 disconnect test (`l2_integration_hkoe86p_test.go:251-253`):
```go
// FaultDropAfter ev2 (turn/started) — disconnect while turn in-flight.
fault := codexdigitaltwin.FaultConfig{Mode: codexdigitaltwin.FaultDropAfter, EventN: 2}
twin := codexdigitaltwin.New(f, fault)
```
It then asserts an error event fired and no queue completion (`:261-267`) — exercising
reactor invariant I1 offline.

---

## 8. THE KEY QUESTION — generic seam vs codex-specific instantiation

The pattern's reusable spine is: **capture tee → pure decode → frame→event mapper → pure
state-machine driven by (EventSource, Effector) → replay twin with fault injection →
L0..L3 taxonomy**. What is a codex-specific *instantiation* is: the JSON-RPC framing, the
method registry, the payload structs, the concrete Event/Action vocabulary, and the harmonik
bridge sink.

### GENERIC seam candidates (would move to `internal/substrate`)

| Concept | Current location (`file:line`) | Why generic |
|---------|-------------------------------|-------------|
| Transparent stdio capture tee | `internal/apptap/tap.go` — whole `Tap` type `:48-75`, `Run` `:82` | Already protocol-agnostic by its own doc (`tap.go:20-21`); `io.Writer` capture sinks, no codex knowledge. Moves nearly verbatim. |
| `EventSource` interface | `codexreactor/reactor.go:133-137` | Pure `Events(ctx) <-chan T`. The channel-source contract is vertical-agnostic (would need a generic event type; see below). |
| `Effector` interface | `codexreactor/reactor.go:120-122` | `Execute(ctx, Action) error` — pure side-effect sink; the swap boundary. Generic modulo the `Action` type. |
| Reactor `Run` driver loop | `codexreactor/reactor.go:282-292` | The `for ev := range src.Events; Step; Execute` loop is identical for any vertical. Only `Step` is specific. |
| `FakeEffector` (record/replay double) | `codexreactor/fake.go:13-40` | Generic recorder over any `Action`; the "swap for a testable fake" primitive. |
| `SyntheticSource` (fixed-slice source) | `codexreactor/fake.go:47-68` | Generic `EventSource` over any event slice. |
| Twin replay engine skeleton | `codexdigitaltwin/twin.go:88-195` (`New`, `Events`, `replay` loop) | The scan-line → decode → map → fault → send pipeline is generic; only `codexwire.Parse` + `frameToEvent` are pluggable steps. |
| Fault-injection model | `codexdigitaltwin/twin.go:47-75` (`FaultMode`, `FaultConfig`) + apply block `:157-189` | Drop/stall/truncate/dup are transport faults meaningful for ANY event stream. |
| Corpus-driven decode/round-trip/no-unknown contract (L1 shape) | `codextest/l1_contract_hkoe86p_test.go:43-215` + drift canary `canary_hkoe86p_test.go:34` | The *shape* ("every captured frame parses, is known, round-trips; replay yields golden actions") is a reusable test harness template. |
| L0–L3 taxonomy + `CODEX_LIVE`-style env gate + `make` wiring | `Makefile:107-133`, `l3_live...:30-35` | The "95% zero-token, one env-gated live gate + drift canary" policy is vertical-independent. Env var name and corpus path are the only specifics. |
| Two-layer decode discipline (trusted envelope / untrusted payload + `Extra` preserve-count) | `codexwire.go:4-18`, `ExtraCount` `:460`, `parseExtra` `:426` | The *pattern* (strict outer frame, tolerant inner payload with unmodeled-field counting, unknown → typed-raw not crash) is reusable; the concrete JSON-RPC shape is not. |

### CODEX-SPECIFIC instantiation (stays out of `internal/substrate`)

| Concept | Location (`file:line`) | Why specific |
|---------|-----------------------|--------------|
| JSON-RPC 2.0 framing + 5 `FrameKind`s | `codexwire.go:37-45`, `Parse` discrimination `:210-249` | JSON-RPC-specific; another vertical may be protobuf/SSE/etc. |
| `methodRegistry` (16 codex methods) | `codexwire.go:92-163` | The exact codex method strings + directions. |
| All payload structs (`InitializeParams`, `TurnStartedParams`, `ThreadTokenUsageUpdatedParams`, …) | `codexwire.go:471`–end | Codex protocol schema. |
| `Parse`/`Marshal` bodies | `codexwire.go:183`, `:309` | Encode JSON-RPC specifics; only the *interface* (`decode([]byte)→Frame`) is generic. |
| `EventType` / `ActionType` vocabularies | `codexreactor/reactor.go:34-52`, `:80-96` | `turn_started`, `message_delta`, `complete_turn`, … are codex-turn concepts. |
| `Event` / `Action` field sets | `codexreactor/reactor.go:62-73`, `:102-112` | `ThreadID/TurnID/ItemID/Delta/TotalTokens/ContextWindow` — codex-turn fields. |
| `State` + invariants I1/I2 | `codexreactor/reactor.go:143-157`; `Step` `:185-277` | One-turn-in-flight and dedup-by-seq are codex-app-server semantics. |
| `frameToEvent` translation table | `codexdigitaltwin/twin.go:202-277` | Maps codex methods → codex events; the *step* is generic, the *table* is specific. |
| `HarmonikBridgeSink` | `codextest/l2_integration_hkoe86p_test.go:36-150` | Codex→harmonik comms/queue/beads mapping. |
| Corpus fixture + `frameToEvent` "ServerNotification only" filter | `twin.go:144-146`; `testdata/codex-app-server/corpus/raw-session-01.jsonl` | Codex captured data + codex directionality rules. |
| L3 handshake sequence (`initialize`→`initialized`→`thread/start`→`turn/start`) | `l3_live...:96-197` | Codex app-server protocol handshake. |

### The extraction boundary in one sentence
`internal/substrate` would own: the capture tee (`Tap`), the `EventSource`/`Effector`
interfaces, the `Run` loop, the two test doubles (`FakeEffector`, `SyntheticSource`), the
twin replay+fault engine (parameterised by a pluggable `decode([]byte)→Frame` and a
`frame→Event` mapper), and the L0–L3 taxonomy/Makefile policy. Each vertical (codex, and the
next one) would supply: a wire codec (`codexwire`-shaped), its own `Event`/`Action` types +
`Step` state machine, its `frame→Event` table, its bridge sink, and its corpus.

**Note on generics friction.** The current `EventSource`/`Effector`/`Reactor` are hard-typed
to `codexreactor.Event`/`Action` (not type parameters). Making the seam truly generic
requires either Go generics (`EventSource[E]`, `Effector[A]`, `Run[E,A]`) or an
`any`/interface-typed event boundary. The interfaces are small (one method each), so the
mechanical cost is low, but this is the one place the extraction is not copy-paste.

---

## Source index
- `internal/apptap/tap.go`, `internal/apptap/tap_test.go`
- `internal/codexwire/codexwire.go` (45 KB; registry `:92-163`, `Parse` `:183`, `Marshal` `:309`)
- `internal/codexreactor/reactor.go`, `internal/codexreactor/fake.go`
- `internal/codexdigitaltwin/twin.go`
- `internal/codextest/{l0_wire,l1_contract,l2_integration,l3_live,canary}_hkoe86p_test.go`
- `testdata/codex-app-server/corpus/raw-session-01.jsonl`
- `Makefile:103-133`
- `plans/2026-07-11-codex-app-server-replan/PHASE-1-tap-serializer-reactor.md`
- `.kerf/works/codex-app-server/04-design/reactor-and-taxonomy-deep-design.md`
