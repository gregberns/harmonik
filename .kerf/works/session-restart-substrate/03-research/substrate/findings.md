# 03-research / substrate — extraction map for `internal/substrate`

> **Pass 3 (Research), substrate component.** Factual, code-grounded extraction inventory for the
> generic record→replay seam pulled out of the green codex app-server stack. Grounds the
> Change-Design pass. All paths relative to repo root `/Users/gb/github/harmonik/`.
> Go toolchain: `go 1.25` (go.mod:3) — full generics available.
>
> **Blast-radius fact (verified by import grep):** nothing outside the codex family imports
> `codexreactor` / `codexdigitaltwin` / `codexwire` / `apptap`. Importers are exactly:
> `internal/codexdigitaltwin/twin.go`, the four `internal/codextest/*_test.go` tiers +
> canary, and each package's own `_test.go`. `internal/apptap` currently has **zero**
> importers anywhere. The extraction's compile-time blast radius is fully contained.

---

## 1. Exact extraction inventory

### 1.1 Tap — `internal/apptap/tap.go`

| Symbol | Location | Signature |
|---|---|---|
| `Tap` struct | tap.go:48-75 | `type Tap struct { Binary string; Args []string; InCapture io.Writer; OutCapture io.Writer; Stdin io.Reader; Stdout io.Writer; Stderr io.Writer }` |
| `(*Tap).Run` | tap.go:82-145 | `func (t *Tap) Run() error` |

**Change needed to be generic: none.** The package doc itself declares "Tap is
protocol-agnostic — any client or child drives it" (tap.go:21). It imports only `io`, `os`,
`os/exec` (tap.go:27-31). Captures are consumer-provided `io.Writer`s (tap.go:58-63) — it owns
no file format, exactly SB-R11's shape. Only the package doc comment (tap.go:1-3, "for the
codex app-server integration") mentions codex. Whether it *physically* moves is a placement
question (§5); no code change is required either way.

### 1.2 EventSource / Effector / Run driver — `internal/codexreactor/reactor.go`

| Symbol | Location | Signature |
|---|---|---|
| `EventSource` | reactor.go:133-137 | `type EventSource interface { Events(ctx context.Context) <-chan Event }` |
| `Effector` | reactor.go:120-122 | `type Effector interface { Execute(ctx context.Context, a Action) error }` |
| `(*Reactor).Run` | reactor.go:282-292 | `func (r *Reactor) Run(ctx context.Context, src EventSource, eff Effector) error` — `for ev := range src.Events(ctx) { for _, a := range r.Step(ev) { if err := eff.Execute(ctx, a); err != nil { return err } } }` |

**Change needed:** parameterize over the event and action types (§2). The `Run` loop body
references only `src.Events`, `r.Step`, `eff.Execute` — no codex field access — so the driver
extracts verbatim once `Step` becomes an injected `func(E) []A`. **Constraint:** Go forbids
methods with their own type parameters, so the generic `Run[E, A]` cannot stay a method on a
non-generic `Reactor`; it becomes a free function (or `Reactor` itself goes generic — rejected
in §2 because `Step`'s transition table is codex semantics, not substrate).

### 1.3 What does NOT move from reactor.go (context for the seam boundary)

- `Event` struct (reactor.go:62-73) and the 8-value `EventType` vocab (reactor.go:32-52) —
  codex-typed instantiation.
- `Action` struct (reactor.go:102-112) and the 5-value `ActionType` vocab (reactor.go:78-96).
- `State` (reactor.go:143-157), `Reactor`/`New`/`State()` (reactor.go:168-176),
  `Step` (reactor.go:185-277) — the pure state machine carrying invariants I1
  (one-turn-in-flight, doc reactor.go:11-14, enforced at :206-214, :217-224, :226-231) and I2
  (dedup-by-seq, doc reactor.go:16-18, enforced at :186-192). These are the codex vertical.
  Substrate specifies the *shape* (`Step` pure, `(state, event) → (state', actions)`,
  reactor.go:162-165) — SB-R1 — not the table.

### 1.4 FakeEffector / SyntheticSource — `internal/codexreactor/fake.go`

| Symbol | Location | Signature |
|---|---|---|
| `FakeEffector` | fake.go:13-16 | `type FakeEffector struct { mu sync.Mutex; actions []Action }` |
| `(*FakeEffector).Execute` | fake.go:19-24 | `func (f *FakeEffector) Execute(_ context.Context, a Action) error` |
| `(*FakeEffector).Actions` | fake.go:27-33 | `func (f *FakeEffector) Actions() []Action` (returns copy) |
| `(*FakeEffector).Reset` | fake.go:36-40 | `func (f *FakeEffector) Reset()` |
| `SyntheticSource` | fake.go:47-49 | `type SyntheticSource struct { events []Event }` |
| `NewSyntheticSource` | fake.go:52-54 | `func NewSyntheticSource(events []Event) *SyntheticSource` |
| `(*SyntheticSource).Events` | fake.go:59-68 | `func (s *SyntheticSource) Events(ctx context.Context) <-chan Event` — pre-filled buffered channel, immediately closed; honors already-cancelled ctx (fake.go:61) |

**Change needed:** mechanical `[A any]` / `[E any]` parameterization. Neither type touches a
single codex field — they only store/copy/channel the values. These are the cleanest moves in
the whole extraction.

### 1.5 Twin — `internal/codexdigitaltwin/twin.go`

| Symbol | Location | Signature |
|---|---|---|
| `FaultMode` + 5 consts | twin.go:47-68 | `type FaultMode int` — `FaultNone`, `FaultDropAfter`, `FaultStall`, `FaultTruncate`, `FaultDup` |
| `FaultConfig` | twin.go:72-75 | `type FaultConfig struct { Mode FaultMode; EventN int }` (EventN 1-based, twin.go:70-71) |
| `Twin` | twin.go:81-84 | `type Twin struct { corpus io.Reader; fault FaultConfig }` |
| `New` | twin.go:88-90 | `func New(corpus io.Reader, fault FaultConfig) *Twin` |
| `(*Twin).Events` | twin.go:98-105 | `func (t *Twin) Events(ctx context.Context) <-chan codexreactor.Event` — goroutine + `defer close`, buffer 16 (twin.go:99) |
| `(*Twin).replay` | twin.go:107-195 | scanner loop; ctx-aware `send` closure (twin.go:108-115); fault dispatch at twin.go:157-189 |
| `frameToEvent` | twin.go:202-277 | `func frameToEvent(frame codexwire.Frame, seq *uint64) (codexreactor.Event, bool)` — NOTE: seq is `*uint64`, not `*int` |

**FaultMode/FaultConfig move unchanged** — they reference no codex type.

**The replay loop is 90% generic but leaks codex at exactly five points** (this is the load-bearing
finding for the codec design in §2):

1. **Decode:** `codexwire.Parse(line)` (twin.go:132).
2. **Parse-error policy:** on decode error, emit a synthetic **codex** `Event{Type: EventTypeError, Message: err.Error()}` and treat as fatal (`return`) (twin.go:133-139).
3. **Filter:** `frame.Kind != codexwire.FrameKindServerNotification → continue` (twin.go:142-146).
4. **Map:** `frameToEvent(frame, &seq)` with mapped/not-mapped skip (twin.go:148-152).
5. **Fault synthesis:** `FaultDropAfter` injects a codex `Event{Seq: 0, Type: EventTypeDisconnected}` (twin.go:164); `FaultTruncate` injects `Event{Seq: seq, Type: EventTypeError, Message: "twin: truncated frame"}` (twin.go:173-177).

Points 1-4 fuse into one vertical-supplied decode step; point 5 requires **two
vertical-supplied synthetic-event constructors** — a plain `decode`+`map` pair is NOT
sufficient to make the Twin generic (see §2.3 and Risk R1). Everything else — the scanner,
blank-line skip (twin.go:127-130), ctx checks, evIdx counting (twin.go:121,154), the fault
switch, `FaultStall`'s `<-ctx.Done()` (twin.go:168-171), `FaultDup`'s double-send
(twin.go:179-187) — is vertical-agnostic control flow and moves verbatim.

**Seq threading:** the `seq *uint64` counter (twin.go:120) is incremented inside `frameToEvent`
(twin.go:210,223,237,252,265) — i.e. seq assignment is already a property of the *mapper*, not
the loop. `FaultDup`'s "same Seq" guarantee (twin.go:16-17,179-187) falls out for free in any
design that re-sends the same `E` value. So the generic Twin needs **no seq parameter at all**
if the codec is allowed to be stateful (§2.3). Only `FaultTruncate`'s error event wants "the
current seq" (twin.go:175) — solvable by letting the stateful codec mint it.

### 1.6 The L0–L3 taxonomy + Makefile policy — `internal/codextest/` + Makefile:107-133

The taxonomy is **test files + a Makefile convention, not exported code**. Shape
(l0_wire_hkoe86p_test.go:4-18):

- **L0 unit** — `l0_wire_hkoe86p_test.go` (204 lines): golden frame parses (:42-98),
  Parse→Marshal semantic round-trip via `map[string]any` + `reflect.DeepEqual` (:109-156),
  malformed-input table (:162-186), unknown-method→`FrameKindRaw` (:190-204). Corpus path by
  `runtime.Caller(0)` + `../../testdata/codex-app-server/corpus` (:33-36).
- **L1 contract** — `l1_contract_hkoe86p_test.go` (239 lines): corpus path helper (:30-36);
  zero-unknown-frames + request/response id correlation (:43-81); zero-unmodeled-Extra
  (:85-124); whole-corpus round-trip (:128-173); **golden action-sequence replay**
  `twin → New() reactor → FakeEffector → reflect.DeepEqual` against a literal 5-action `want`
  slice (:181-215, wiring at :196-199).
- **L2 integration** — `l2_integration_hkoe86p_test.go` (268 lines): test-local
  `HarmonikBridgeSink` Effector faking comms/queue/beads in memory (:36-150); happy-path
  corpus→twin→reactor→sink with per-service assertions (:160-238); `FaultDropAfter EventN:2`
  disconnect-mid-turn → error event, no queue completion (:242-268).
- **L3 live** — `l3_live_hkoe86p_test.go` (271 lines): `skipUnlessLive` env gate
  `CODEX_LIVE != "1" → t.Skip` (:30-35); binary resolution `CODEX_BIN` else `exec.LookPath`
  (:38-48); real-subprocess handshake canary (:59-198); protocol-version canary (:204-271).
- **Drift canary** — `canary_hkoe86p_test.go:34-90`: corpus valid-JSON / parseable /
  zero-`FrameKindRaw` / ≥10-frames gates.

Makefile policy (exact lines): `test-codex-l012` target Makefile:112-113 (`go test -count=1`
over the four codex packages, CODEX_LIVE=0 default, deploy gate per :107-110);
`test-codex-live` Makefile:120-121 (`CODEX_LIVE=1 go test -timeout 180s -count=1 -run TestL3_`);
`capture-fixtures` Makefile:129-133 (deliberate, budget-capped, ledgered via
`testdata/codex-app-server/corpus/CAPTURE-LOG.md`).

**What extracts:** nothing verbatim — the taxonomy becomes a **normative template** (SB-R6):
tier definitions, the env-gate + `-run` prefix convention, the `runtime.Caller` corpus-path
idiom, the golden-action-sequence pattern, the drift-canary gate list, and the Makefile
target-pair-per-vertical convention. The only reusable *code* is `FakeEffector` /
`SyntheticSource` / `Twin` / `FaultConfig`, inventoried above.

---

## 2. Genericization mechanics — concrete

### 2.1 Option (a): Go generics

```go
// package substrate

// EventSource[E] is the provider of typed events (SB-R1).
type EventSource[E any] interface {
	Events(ctx context.Context) <-chan E
}

// Effector[A] executes actions produced by a reactor (SB-R1).
type Effector[A any] interface {
	Execute(ctx context.Context, a A) error
}

// Run drives the seam: source → step → effector. step is the vertical's pure
// state-machine transition (a bound method value like (*Reactor).Step).
func Run[E, A any](ctx context.Context, src EventSource[E], step func(E) []A, eff Effector[A]) error {
	for ev := range src.Events(ctx) {
		for _, a := range step(ev) {
			if err := eff.Execute(ctx, a); err != nil {
				return err
			}
		}
	}
	return nil
}

// FakeEffector[A] records every action (fake.go:13-40, parameterized).
type FakeEffector[A any] struct {
	mu      sync.Mutex
	actions []A
}
func (f *FakeEffector[A]) Execute(_ context.Context, a A) error { … }
func (f *FakeEffector[A]) Actions() []A                          { … }
func (f *FakeEffector[A]) Reset()                                { … }

// SyntheticSource[E] delivers a fixed slice (fake.go:47-68, parameterized).
type SyntheticSource[E any] struct{ events []E }
func NewSyntheticSource[E any](events []E) *SyntheticSource[E] { … }
func (s *SyntheticSource[E]) Events(ctx context.Context) <-chan E { … }

// ── replay twin ──

type FaultMode int                                    // moves verbatim (twin.go:47-68)
const (FaultNone FaultMode = iota; FaultDropAfter; FaultStall; FaultTruncate; FaultDup)
type FaultConfig struct { Mode FaultMode; EventN int } // moves verbatim (twin.go:72-75)

// ReplayCodec[E] is everything a vertical supplies to replay its corpus.
// It fuses the four codex-specific replay steps (decode, error policy, filter,
// map — twin.go:132-152) into DecodeLine, and supplies the two synthetic-event
// constructors the fault injector needs (twin.go:164, :173-177).
// Implementations MAY be stateful (own their seq counter).
type ReplayCodec[E any] interface {
	// DecodeLine decodes one corpus line. emit=false skips the line
	// (non-reactor-relevant frame). err != nil is a fatal transport failure:
	// the twin emits ErrorEvent(err) and closes the source.
	DecodeLine(line []byte) (ev E, emit bool, err error)
	// ErrorEvent synthesizes the vertical's transport-error terminal event
	// (used for decode errors and FaultTruncate).
	ErrorEvent(msg string) E
	// DisconnectEvent synthesizes the vertical's connection-lost event
	// (used for FaultDropAfter).
	DisconnectEvent() E
}

type Twin[E any] struct {
	corpus io.Reader
	fault  FaultConfig
	codec  ReplayCodec[E]
}
func NewTwin[E any](corpus io.Reader, fault FaultConfig, codec ReplayCodec[E]) *Twin[E]
func (t *Twin[E]) Events(ctx context.Context) <-chan E   // twin.go:98-105 shape
// replay loop = twin.go:107-195 with the five leak points swapped for codec calls
```

The prompt-sketch `decode func([]byte) (Frame, error)` + `map func(Frame, *int) (E, bool)`
two-function parameterization was evaluated and **rejected in favor of the fused
`DecodeLine`**, for three code-grounded reasons: (i) it forces a second type parameter `F`
(`Twin[F, E any]`) that substrate never inspects — `Frame` appears in the loop only as a
handoff between decode and map (twin.go:132→148); (ii) the kind-filter (twin.go:144) would
need a third plug (`filter func(F) bool`) or leak `FrameKindServerNotification` semantics;
(iii) it still doesn't cover the two synthetic-event constructors, which are needed
regardless (twin.go:135-139, :164, :175). One stateful codec interface covers all five leak
points with one type parameter. (Also note the sketch's `*int` is actually `*uint64` —
twin.go:202 — and becomes codec-internal state, disappearing from the substrate surface.)

**What codex code changes under (a) — the re-instantiation:**

- `internal/codexreactor/reactor.go`:
  - `Event`, `EventType`, `Action`, `ActionType`, `State`, `Reactor`, `New`, `Step` — **unchanged.
    `Step` stays codex-typed** (`func (r *Reactor) Step(ev Event) []Action`, reactor.go:185); the
    substrate never sees I1/I2.
  - Delete interface bodies; alias to the instantiation (legal since Go 1.18 — alias of an
    *instantiated* generic type; parameterized aliases not needed):
    ```go
    type EventSource = substrate.EventSource[Event]
    type Effector    = substrate.Effector[Action]
    ```
    Aliases (not defined types) mean every existing reference — including
    `codexdigitaltwin.Twin` satisfying `EventSource` structurally, and `HarmonikBridgeSink`
    implementing `Effector` (l2_integration_hkoe86p_test.go:101) — compiles untouched.
  - `Run` becomes a one-line wrapper preserving all three call sites
    (l1:199, l2:178, l2:256, plus reactor_test.go):
    ```go
    func (r *Reactor) Run(ctx context.Context, src EventSource, eff Effector) error {
        return substrate.Run(ctx, src, r.Step, eff)
    }
    ```
- `internal/codexreactor/fake.go`: **file shrinks to two aliases**
  (`type FakeEffector = substrate.FakeEffector[Action]`,
  `type SyntheticSource = substrate.SyntheticSource[Event]`) plus
  `func NewSyntheticSource(events []Event) *SyntheticSource { return substrate.NewSyntheticSource(events) }`.
  Composite literals like `&codexreactor.FakeEffector{}` (l1:197) keep compiling because
  aliases of instantiated types allow them.
- `internal/codexdigitaltwin/twin.go`: shrinks to (1) a `codexCodec` implementing
  `substrate.ReplayCodec[codexreactor.Event]` — `DecodeLine` = `codexwire.Parse` +
  `Kind != FrameKindServerNotification → emit=false` + the existing `frameToEvent` table with
  the seq counter moved into the codec struct; `ErrorEvent`/`DisconnectEvent` returning the two
  synthetic events from twin.go:137, :164; and (2) back-compat wrappers:
  ```go
  type Twin struct{ inner *substrate.Twin[codexreactor.Event] }
  func New(corpus io.Reader, fault FaultConfig) *Twin           // signature preserved via alias:
  type FaultConfig = substrate.FaultConfig                       // + const re-exports FaultNone…FaultDup
  func (t *Twin) Events(ctx context.Context) <-chan codexreactor.Event { return t.inner.Events(ctx) }
  ```
  (Const re-export: `const FaultDropAfter = substrate.FaultDropAfter` etc., so
  `codexdigitaltwin.FaultConfig{Mode: codexdigitaltwin.FaultDropAfter, EventN: 2}` at l2:252
  still compiles.)
- `internal/codextest/*`: **zero changes** — every symbol it touches keeps its name and type
  identity via aliases. This is the Goal-2 "codex stays green" proof.

**Friction points under (a), honestly stated:**
1. `Run` can't remain a true generic method — the wrapper is one line but the seam's canonical
   entry point becomes `substrate.Run`; docs must say so.
2. Channel invariance means the alias trick is load-bearing: a *defined* type
   (`type EventSource substrate.EventSource[Event]`) would also work structurally, but aliases
   are strictly safer for assignability in both directions. No `<-chan Event` vs `<-chan any`
   mismatch exists under generics — that problem only afflicts option (b).
3. `FaultMode` const re-export is mildly ugly (Go has no enum re-export sugar); 5 consts + 2
   aliases in codexdigitaltwin.
4. Generic-instantiation error messages are noisier in test failures; mitigated because all
   assertion-facing types (`[]Action` from `Actions()`) resolve to concrete codex types.

### 2.2 Option (b): `any`-typed boundary

```go
type EventSource interface{ Events(ctx context.Context) <-chan any }
type Effector interface{ Execute(ctx context.Context, a any) error }
func Run(ctx context.Context, src EventSource, step func(any) []any, eff Effector) error
type FakeEffector struct{ mu sync.Mutex; actions []any }   // Actions() []any
type SyntheticSource struct{ events []any }
type ReplayCodec interface {
	DecodeLine(line []byte) (ev any, emit bool, err error)
	ErrorEvent(msg string) any
	DisconnectEvent() any
}
type Twin struct{ corpus io.Reader; fault FaultConfig; codec ReplayCodec }
```

**What codex code changes under (b):** substantially more, because Go channels are invariant —
`<-chan codexreactor.Event` does not satisfy `<-chan any`:
- `codexdigitaltwin.Twin.Events` must return `<-chan any`, boxing every `Event`
  (breaks its current signature at twin.go:98 and its structural satisfaction of the old
  codex-typed interface), OR codexreactor keeps its own typed `EventSource` and every
  source needs a pumping adapter goroutine (`for ev := range typed { anyCh <- ev }`) that
  copies the whole stream — a new goroutine + channel per wiring.
- `codexreactor.Step(ev Event)` needs an adapter `func(ev any) []any` doing a type assertion
  per event and a per-slice re-box of `[]Action → []any`. The reactor loses the compile-time
  guarantee that it only receives its own event type; a foreign value becomes a runtime
  assertion-failure path that must be *tested*.
- `FakeEffector.Actions() []any` breaks the L1 golden comparison
  `reflect.DeepEqual(got, want)` where `want` is `[]codexreactor.Action`
  (l1_contract_hkoe86p_test.go:203-214) and every field access in the L2 sink assertions —
  codextest does NOT stay untouched; each assertion site needs asserts/conversions.
- Every second-vertical effector starts with `a := av.(keeper.Action)` boilerplate.

**Friction:** the "no codex leakage, typed not stringly" requirement (SB-R2) is exactly what
(b) surrenders — leakage becomes a runtime property instead of a compile error, and depguard
can no longer prove the seam is typed. The one advantage — no generic syntax — buys nothing
here because the instantiating aliases in (a) already hide generics from codex call sites.

### 2.3 Recommendation

**Go generics (option a), with the fused stateful `ReplayCodec[E]` instead of the raw
decode+map pair.** Reasoning: (1) codextest and all golden assertions compile unchanged —
the cheapest possible proof of Goal 2; (2) SB-R2's "typed, no leakage" becomes a compile-time
property; (3) channel invariance makes (b) actively invasive (stream-pump adapters or boxed
sources); (4) the codec fusion is forced by the five leak points in the replay loop
(twin.go:132-177) — any Change-Design that specs only `decode`+`map` will rediscover the two
synthetic-event constructors during implementation. The single genuine cost of (a) is `Run`
moving from method to free function + a one-line wrapper.

---

## 3. What stays codex-specific (the instantiation) — confirmed

| Item | Stays in | Evidence |
|---|---|---|
| codexwire framing (Frame, FrameKind, Parse, Marshal, Extra machinery) | `internal/codexwire` | Frame codexwire.go:49-69; Parse :183-252; Marshal :309-329; parseExtra/mergeExtra :426-456. Substrate sees only `[]byte → (E, bool, error)` via the codec. |
| Method registry (16 methods) + all Params/Result payload structs | `internal/codexwire` | methodRegistry codexwire.go:92-163; payload structs :471-1339 |
| `Event`/`EventType` field set (8 types) | `internal/codexreactor` | reactor.go:32-52, :62-73 |
| `Action`/`ActionType` field set (5 types) | `internal/codexreactor` | reactor.go:78-96, :102-112 |
| `State` + `Step` transition table + invariants I1/I2 | `internal/codexreactor` | I1 doc reactor.go:11-14, enforced :217-224 (supersede), :205-215 (disconnect terminates); I2 doc :16-18, enforced :186-192, LastSeq reset on Connected :196-203 |
| `frameToEvent` mapping table (5 methods → 5 event types) | `internal/codexdigitaltwin` (as the codec) | twin.go:202-277 |
| Server-notification-only filter | codec (`DecodeLine emit=false`) | twin.go:142-146 |
| `HarmonikBridgeSink` + its six record types | `internal/codextest` (test-local; a *template*, not extracted code) | l2_integration_hkoe86p_test.go:36-150 |
| Corpus fixture + capture ledger | `testdata/codex-app-server/corpus/raw-session-01.jsonl` | l1_contract_hkoe86p_test.go:30-36; Makefile:125-127 |
| L3 live harness (codex subprocess handshake) | `internal/codextest` | l3_live_hkoe86p_test.go:59-198 |
| Drift canary gate values (≥10 frames, zero FrameKindRaw) | `internal/codextest` | canary_hkoe86p_test.go:69-87 |

---

## 4. Test-double + taxonomy portability

### 4.1 What becomes generic code vs template

- **Generic code (imported):** `substrate.FakeEffector[A]`, `substrate.SyntheticSource[E]`,
  `substrate.Twin[E]` + `FaultConfig`/`FaultMode`, `substrate.Run`. A vertical writes zero
  test-double plumbing.
- **Template (copied per vertical, shape mandated by SB-R6):** the four tier files + canary +
  the Makefile target pair. Evidence this is template-not-code: every L0-L3 file is
  `package codextest_test` with hardcoded corpus IDs (l1:184-188), a hand-written golden
  action slice (l1:203-209), and a bespoke bridge sink (l2:36-150) — none exportable without
  inventing abstraction the codebase style forbids.

### 4.2 Second vertical (session-keeper): supplies vs inherits

**Inherits:** `EventSource`/`Effector`/`Run` seam; `FakeEffector[keeper.Action]`;
`SyntheticSource[keeper.Event]`; `Twin[keeper.Event]` + all four fault modes; the tier
definitions, env-gate convention, corpus-path idiom, golden-action pattern, canary gate list,
Makefile convention; ClockPort (SB-R9, lives in substrate).

**Supplies:**
1. `keeper.Event` / `keeper.Action` types + pure `Step` reactor (the 11-gate ladder as states,
   SK-R2) — the analog of reactor.go:185-277.
2. A `ReplayCodec[keeper.Event]`: `DecodeLine` = JSON-decode one durable-bus/journal NDJSON
   line + type-switch mapping table (the analog of `frameToEvent`; no Frame-kind filter — the
   keeper corpus has no request/response correlation, confirming Frame must not appear in the
   substrate surface); `ErrorEvent`/`DisconnectEvent` = the keeper's transport-lost /
   `restart_failed`-class synthetic events.
3. A recorded corpus: the ~476 restart cycles (SK-R10) sliced from `events.jsonl` +
   keeper journal, checked into `testdata/session-keeper/corpus/` with a CAPTURE-LOG.
4. A `KeeperBridgeSink` recording fake PanePort/GaugePort/HandoffPort/EmitterPort calls
   (the analog of HarmonikBridgeSink's fake comms/queue/beads).
5. Golden action sequences + the SR-invariant property checks.
6. An L3 live gate + env var (`KEEPER_LIVE=1`) + Makefile pair.

### 4.3 Keeper L0/L1/L2 tier sketch

- **L0 unit (pure, zero-token):** table-driven `keeper.Step` transition tests via
  `SyntheticSource[keeper.Event]` + `FakeEffector[keeper.Action]` (gate-ladder branches,
  terminal transitions `cycle_complete`/`cycle_aborted`/`clear_unconfirmed`); codec golden
  decode + malformed-line table (mirror of l0_wire :162-186); ClockPort fake-clock unit tests.
- **L1 contract (corpus, zero-token):** every corpus line decodes with zero unknown event
  types (mirror of TestL1_CorpusZeroUnknownFrames :43-81); replay one known-good cycle
  through `Twin[keeper.Event]` → reactor → `FakeEffector` and `reflect.DeepEqual` against the
  golden action sequence (mirror of :181-215); whole-corpus ordering sweeps for SR3
  (handoff-written before clear-sent) and SR4 (model-done before clear-sent) as pure
  postconditions over decoded event order; drift canary: corpus non-empty, valid JSON,
  zero unknown types, ≥N cycles.
- **L2 integration (corpus + faults, zero-token):** corpus → Twin → reactor →
  KeeperBridgeSink happy path (427-completion baseline characterization, SK-R10);
  `FaultDropAfter` mid-cycle → terminal `restart_failed`-class event, never silence (SR9 —
  mirror of TestL2_ReactorDisconnectWithBridgeSink :242-268); `FaultDup` on the clear-trigger
  event → exactly one `/clear` injected (SR7, dedup analog of I2); `FaultStall` + fake clock
  advance → bounded-liveness timeout fires (SR9); `FaultTruncate` → abort path, no orphaned
  in-flight cycle.
- **(L3 live, for completeness):** `KEEPER_LIVE=1`, one real tmux pane, one scripted
  handoff→clear→resume cycle; wire-canary assertions only. Makefile: `test-keeper-l012` /
  `test-keeper-live` mirroring Makefile:112/120.

---

## 5. Package / depguard placement

**Placement: `internal/substrate`, a leaf package** (name locked by Goal 1/PLAN §3a per
02-components.md; the spec-file naming collision is a separate spec-level decision there).
Imports needed: `context`, `sync`, `io`, `bufio`, `bytes` — stdlib only, `$gostd` suffices.
It must not import `internal/core` either (event types are type parameters; nothing in the
inventory touches `core`).

**Depguard rule** (.golangci.yml `depguard.rules`, :64-179 pattern; modeled on the `schedule`
leaf rule at :162-168 and the keeper rule's explicit deny list at :124-136):

```yaml
# substrate: generic record→replay seam (codename:session-restart-substrate).
# Leaf: stdlib + self-import only. MUST NOT import any vertical (codex*, keeper)
# or the daemon — verticals instantiate substrate, never the reverse.
substrate:
  files: ["**/internal/substrate/**"]
  allow:
    - "$gostd"
    - "github.com/gregberns/harmonik/internal/substrate"
  deny:
    - { pkg: "github.com/gregberns/harmonik/internal/codexreactor",     desc: "substrate MUST NOT import a vertical (codex)" }
    - { pkg: "github.com/gregberns/harmonik/internal/codexwire",        desc: "substrate MUST NOT import a vertical (codex)" }
    - { pkg: "github.com/gregberns/harmonik/internal/codexdigitaltwin", desc: "substrate MUST NOT import a vertical (codex)" }
    - { pkg: "github.com/gregberns/harmonik/internal/keeper",           desc: "substrate MUST NOT import a vertical (keeper)" }
    - { pkg: "github.com/gregberns/harmonik/internal/daemon",           desc: "substrate MUST NOT import daemon" }
```

(The allow-list already excludes everything non-stdlib; the deny entries add named errors,
matching house style — cf. crew :107-114, keeper :124-136.)

**No depguard rules exist today for any codex package or apptap** (grep of .golangci.yml:
zero hits for `codex`/`apptap`). Change Design should decide whether to add a companion rule
pinning the direction on the vertical side (codexreactor/codexdigitaltwin allow
`substrate` + `codexwire` + self; deny daemon) — cheap and consistent with the matrix idiom.

**apptap: keep it a separate package; do not fold it into `internal/substrate`.** Reasons:
(1) it is already 100% generic with zero code changes needed (tap.go:21) and zero importers,
so folding buys nothing mechanically; (2) it imports `os/exec` and spawns processes
(tap.go:96,108) — keeping it out lets the substrate depguard rule stay pure/exec-free, which
is worth having for a package whose whole point is deterministic replay; (3) `specs/substrate.md`
(SB-R11) can normatively own the Tap contract while the code stays at `internal/apptap` — spec
scope and package boundary need not coincide. Cost: one extra package. If Change Design
prefers physical co-location anyway, `internal/substrate/tap` as a sub-package with its own
depguard file-scope keeps the exec dependency quarantined; folding into the flat package is
the only option that should be off the table.

**Port idiom conformance (SB-R13):** the substrate interfaces follow the `internal/queue`
consumer-owned-narrow-interface pattern exactly — cf. `QueueSetter` (queue/rpc.go:56-59),
`EventEmitter` (rpc.go:66-68), `BeadLedger` (queue/validation.go:144-154),
`HandlerPauseChecker` (validation.go:170-178): one-to-two-method interfaces declared where
consumed, satisfied structurally, faked trivially in tests. `EventSource`/`Effector`
(one method each) and `ReplayCodec` (three methods, all forced by the replay loop's leak
points) fit the idiom; no Result/Option containers anywhere in the inventory.

---

## 6. Risks / unknowns for Change Design

- **R1 — Fault semantics can leak codex vocabulary.** `FaultDropAfter` is defined as "inject
  *Disconnected*" (twin.go:53-55) and `FaultTruncate` as "replace with an *Error* event"
  (twin.go:61-63) — both are codex event names. If the spec text (SB-R5) copies these
  definitions verbatim, the substrate contract quietly assumes every vertical has
  Disconnected/Error event types. Fix in spec language: define them as "inject the vertical's
  connection-lost event" / "the vertical's transport-error event", supplied via
  `ReplayCodec.DisconnectEvent()`/`ErrorEvent()`. A vertical lacking a natural disconnect
  concept (possible for keeper) must still supply *something* — the spec should say what
  (e.g. its `restart_failed`-class terminal event).
- **R2 — Seq is codex vocabulary, not substrate vocabulary.** I2's Seq-dedup, Seq=0-bypass
  (reactor.go:16-18,59-61) and the twin's seq threading (twin.go:120,202) must all stay on
  the codex side. The fused stateful codec removes seq from the substrate surface entirely;
  any design that keeps a `*uint64`/`*int` in the substrate mapper signature re-imports the
  codex dedup model into the generic seam. FaultDup's contract should be restated generically
  as "the same event value is delivered twice" (idempotence probe), with same-Seq as the
  codex-instantiation consequence.
- **R3 — Parse-error-is-fatal is a policy choice, not a law.** The twin treats one decode
  error as fatal transport failure (twin.go:139). The keeper corpus (bus JSONL slices) may
  reasonably want skip-and-continue for lines outside the cycle's event set. The proposed
  `DecodeLine` already distinguishes skip (`emit=false, err=nil`) from fatal (`err!=nil`) —
  Change Design must state this split explicitly or the keeper codec will be forced to
  swallow real corruption as skips.
- **R4 — Scanner line-length limit.** The generic twin inherits `bufio.Scanner` with the
  default 64 KB buffer, documented as "sufficient for the corpus (max line ~1 KB)"
  (twin.go:117-118) — a codex-corpus assumption. The L1 tests already need 1 MB buffers for
  the same corpus family (l1:53, canary:42). Substrate `Twin` should take/allow a buffer-size
  option or default to 1 MB, else the first oversized keeper bus line silently truncates the
  replay (`scanner.Scan()` returns false, source closes early, looks like a short corpus).
- **R5 — `Run`-as-method break.** Generic methods are illegal; if Change Design specs
  `Reactor.Run` as the seam's normative entry point, it contradicts the extraction. Spec the
  free function `substrate.Run[E,A]` (SB-R1's loop) and note the codex wrapper as
  instantiation detail.
- **R6 — Channel-shape lock-in.** `Events(ctx) <-chan E` (reactor.go:136) bakes in
  channel-of-values delivery with close-on-exhaustion. Fine for both current verticals, but
  it forecloses pull-based or error-returning sources (`Next() (E, error)`); the L3/live
  keeper source must deliver tmux-poll results through a goroutine+channel adapter. Accept
  and state it — don't let a mid-design "improvement" to an iterator API silently rewrite
  the seam the codex stack already proves.
- **R7 — Alias-based re-instantiation is load-bearing for Goal 2.** The "codextest unchanged"
  property in §2.1 depends on using type *aliases* (`=`), const re-exports, and preserved
  wrapper signatures in codexreactor/codexdigitaltwin. If an implementer uses defined types
  instead, structural satisfaction still mostly works but composite literals and
  `DeepEqual`-comparisons of `[]Action` may need edits — a review checklist item, not a
  design change.
- **R8 — Taxonomy is convention, and conventions rot.** Nothing compiles the L0-L3 template;
  a second vertical could skip the canary or the golden-action tier and still build. The only
  enforcement hooks available are the Makefile target-pair convention (Makefile:112-133) and
  spec review. SB-R6 should name the minimum artifact list per vertical (tier files, canary,
  corpus + CAPTURE-LOG, Makefile pair) so drift is at least checkable.
- **R9 — HarmonikBridgeSink temptation.** The bridge sink is test-local and codex-shaped
  (l2:36-150). Extracting a "generic bridge sink" would be invented abstraction with two
  users of different shape (comms/queue/beads vs pane/gauge/handoff/emitter) — flag it now so
  Change Design explicitly keeps sinks per-vertical.
