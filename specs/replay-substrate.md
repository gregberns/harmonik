# Record→Replay Substrate

```yaml
---
title: Record→Replay Substrate
spec-id: replay-substrate
requirement-prefix: RS  # NOTE: prefix RS MUST be reserved in specs/_registry.yaml in the same
                        # commit that lands this spec (registry lint rule). Both RS and the
                        # spec-id `replay-substrate` are free as of 2026-07-13.
status: draft
spec-shape: requirements-first
spec-category: runtime-subsystem
version: 0.1.0
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-07-13
depends-on:
  - event-model
---
```

## 1. Purpose

This spec defines the generic, vertical-agnostic record→replay seam extracted into the Go package `internal/substrate`: the typed `EventSource[E]` / `Effector[A]` composition boundary and its `Run` driver loop, the two test doubles, the `Twin[E]` + `ReplayCodec[E]` + `FaultConfig` replay-and-fault engine, the `ClockPort` + `Ticker` determinism port, the two-layer decode discipline, and the L0–L3 test-taxonomy and measurement policy that a vertical instantiates. It is normative for any subsystem that records an event/action stream and replays it deterministically for testing.

The seam is a separate spec because its substance is a reusable quality mechanism — a boundary plus a determinism kit — that no single vertical owns. The codex app-server stack is its first instantiation and its correctness anchor; the session-keeper vertical is its second. The name `replay-substrate` deliberately distinguishes this seam from three unrelated "substrate" meanings already normative in the spec tree (see RS-023).

## 2. Scope

### 2.1 In scope

- The generic seam: `EventSource[E]`, `Effector[A]`, and the `Run` driver loop as the normative composition boundary (RS-001, RS-002).
- The genericity contract: the seam is typed over event/action types, with no vertical vocabulary leaking into it (RS-003).
- The port idiom and package-boundary discipline: Go-native consumer-owned narrow interfaces, no monad containers, stdlib-only leaf (RS-004, RS-005).
- The two test doubles: a recorder effector and a fixed-slice synthetic source (RS-006, RS-007).
- The replay engine: `ReplayCodec[E]` + `Twin[E]`, the `DecodeLine` skip-vs-fatal split, the two synthetic-event constructors, the 1 MB scanner default, and the channel delivery shape (RS-008 … RS-011).
- The four vertical-neutral fault modes and the terminal-signal-never-silence property (RS-012, RS-INV-003, §8).
- The two-layer decode discipline as a reusable pattern (RS-014).
- The `ClockPort` + `Ticker` determinism port (RS-015).
- The capture-tee primitive (RS-016).
- The L0–L3 test taxonomy, the per-vertical minimum-artifact list, the Makefile-target-pair + env-gate convention, and the replay-vs-baseline measurement standard (RS-017 … RS-020).
- The instantiation obligations: codex as reference (stays green), session-keeper as the second instantiation (RS-021, RS-022).

### 2.2 Out of scope

- Any concrete vertical's `Event`/`Action` types, its codec, its corpus, its bridge sink, and its reactor `Step` — these carry vertical semantics and stay in the vertical's package. The codex codec (`frameToEvent`, `codexwire`) and the keeper reactor are examples; the seam sees only type parameters.
- The process-spawn "Substrate seam" — `internal/handler.Substrate` / the PL-021b Substrate seam / PI-012a / CI-004. That is a different, unrelated normative meaning of the word "substrate" (see RS-023).
- The cognition session substrate — the CL-015 / CL-024 "substrate teardown" (flywheel fresh-start session recycle). Unrelated meaning (see RS-023).
- The transport/production substrate — [credential-isolation.md §2.2] "LLM transport substrate" and pi-harness PI-069 "production substrate = paid". Unrelated meaning (see RS-023).
- Event registration, payload schemas, and the `run_id`/`cycle_id` joinability fix for keeper interior events — owned by [event-model.md §8].
- The session-keeper cycle contract (ports, reactor states, SR3/SR4/SR6/SR7/SR9, the frozen baseline anchors) — owned by the forthcoming session-keeper spec (prefix SK, not yet landed).

> INFORMATIVE: The out-of-scope process-spawn/cognition/transport "substrate" senses are the reason this spec is named `replay-substrate`, not `substrate`. The Go package remains `internal/substrate`; the spec-file/prefix naming is a separate decision from the package vocabulary.

## 3. Glossary

- **seam** — the `EventSource[E]` / `Effector[A]` composition boundary plus its `Run` driver loop; the point at which any source composes with any effector without either knowing the other's concrete nature. (see §4.1)
- **EventSource** — the generic `Events(ctx) <-chan E` producer interface; a live source, a synthetic slice, or a replay `Twin` all satisfy it. (see §4.1, §6.1)
- **Effector** — the generic `Execute(ctx, a A) error` consumer interface; a real side-effect sink, an in-memory recorder, or a bridge sink all satisfy it. (see §4.1, §6.1)
- **reactor Step** — the vertical-supplied pure function `func(E) []A` that the `Run` loop threads between a source and an effector. It carries vertical semantics and is not part of the seam. (see §4.1)
- **ReplayCodec** — the single stateful interface a vertical supplies to replay its corpus: it decodes one corpus line into an event (or a skip, or a fatal error) and supplies the two synthetic terminal-event constructors the fault injector needs. (see §4.3, §6.3)
- **Twin** — the generic replay engine `Twin[E]` that presents a captured corpus `io.Reader` as an `EventSource[E]`, applying the configured fault mode. (see §4.3, §6.3)
- **fault mode** — one of the four vertical-neutral transport faults (`FaultDropAfter`, `FaultStall`, `FaultTruncate`, `FaultDup`), or `FaultNone`, applied by the `Twin` at a 1-based event index. (see §4.4, §8)
- **ClockPort** — the determinism port (`Now`, `Since`, `NewTicker`, `Sleep`) through which a vertical reads time, so a fake clock can replay timeouts and poll races in virtual time. (see §4.6, §6.4)
- **L0–L3 tier** — the four-level test taxonomy a vertical instantiates: L0 unit (pure), L1 contract (corpus), L2 integration (corpus + faults + bridge sink), all zero-token; L3 the single env-gated live tier. (see §4.8)
- **vertical / instantiation** — a concrete subsystem that instantiates the seam with its own event/action types, codec, corpus, and tiers (codex, session-keeper). (see §4.9)

## 4. Normative requirements

### 4.1 The seam, the driver loop, and the port idiom

#### RS-001 — The composition seam

The substrate MUST define the record→replay composition boundary as exactly two generic interfaces: an `EventSource[E]` with the single method `Events(ctx) <-chan E`, and an `Effector[A]` with the single method `Execute(ctx, a A) error`. Any `EventSource[E]` MUST compose with any `Effector[A]` and any reactor `Step` of type `func(E) []A` without either the source or the effector knowing whether its counterpart is a live implementation, a recorder, or a bridge sink. (Signatures in §6.1.)

Tags: mechanism

#### RS-002 — `Run` is the normative driver loop and a free function

The substrate MUST expose the driver loop as a free function `Run[E, A any](ctx, src EventSource[E], step func(E) []A, eff Effector[A]) error`, NOT as a method, because Go forbids generic methods and the reactor `Step` carries vertical semantics that MUST NOT force the driver to be a method on a vertical type. The loop MUST range over `src.Events(ctx)`, apply `step` to each event, and call `eff.Execute(ctx, a)` for each returned action in order. `Run` MUST return `nil` on source exhaustion (channel close) and MUST return the first effector error otherwise, without executing further actions. `Run` MUST NOT close the source's channel — the source owns closure. A vertical MAY wrap `Run` in a one-line method for call-site ergonomics; the wrapper is instantiation detail and MUST NOT be treated as the normative seam.

Tags: mechanism

#### RS-003 — The seam is typed, not stringly

The seam MUST be parameterized over the vertical's event and action types via Go generics; it MUST NOT be an `any`-typed or string-keyed boundary. No vertical type, constant, or vocabulary (event-type names, sequence/dedup fields, transport enums) MUST appear in the substrate package. The typed boundary MUST make "no vertical leakage" a compile-time property.

Tags: mechanism

> RATIONALE: Channel invariance means `<-chan VerticalEvent` does not satisfy `<-chan any`; an `any` boundary would force boxed sources or per-wiring pump goroutines and break every `reflect.DeepEqual([]Action, …)` golden. Generics make the boundary free of adapters.

#### RS-004 — Port idiom: consumer-owned narrow interfaces, no monads

Every interface the substrate defines MUST be a 1–3-method, consumer-owned, narrow interface satisfied structurally (the `internal/queue` house idiom). The substrate MUST NOT introduce `Result`, `Option`, `Either`, or any other monad-style container type in its surface; errors are returned as Go-native `error` values and optional results as multiple return values (Constraint 3).

Tags: mechanism

#### RS-005 — Substrate is a stdlib-only leaf; dependency direction

The substrate package MUST depend only on the Go standard library and itself; it MUST NOT import any vertical package (codex, keeper) or the daemon. Verticals instantiate the substrate; the substrate MUST NOT depend on any vertical. This direction SHOULD be machine-enforced by a depguard leaf rule for `internal/substrate` (allow `$gostd` + self; deny each vertical and the daemon).

Tags: mechanism

### 4.2 Test doubles

#### RS-006 — Recorder effector

The substrate MUST provide a generic recorder effector (`FakeEffector[A]`) whose `Execute` appends each action to an internal, mutex-guarded log, and which exposes `Actions()` (returning a copy of the recorded log) and `Reset()`. The recorder MUST be usable in place of any real `Effector[A]` so a test can assert the exact action sequence a source-plus-`Step` produced. The recorder MUST only accumulate in memory and MUST own no assertion logic — it is the graded artifact, not the grader (see RS-020).

Tags: mechanism

#### RS-007 — Fixed-slice synthetic source

The substrate MUST provide a generic fixed-slice source (`SyntheticSource[E]`, constructed via `NewSyntheticSource[E]([]E)`) whose `Events(ctx)` returns a buffered, pre-filled channel that is closed after the last element. It MUST honor a pre-cancelled context by returning a closed (empty) channel. It is the "swap any source for a deterministic fixture" primitive.

Tags: mechanism

### 4.3 The replay engine: codec + Twin

#### RS-008 — `ReplayCodec[E]` + `Twin[E]` replay contract

The substrate MUST define replay as a `Twin[E]` that takes a captured corpus (`io.Reader` of append-only NDJSON), a `FaultConfig`, and a single stateful `ReplayCodec[E]`, and presents the decoded stream **as** an `EventSource[E]` via `Events(ctx) <-chan E`. The vertical MUST supply only its `ReplayCodec[E]`; the replay loop, buffering, cancellation, and fault application MUST be generic. Any per-line sequence/dedup state MUST live inside the codec, never on the substrate surface. (Signatures in §6.3.)

Tags: mechanism

#### RS-009 — `DecodeLine` skip-vs-fatal split and the two synthetic-event constructors

`ReplayCodec[E].DecodeLine(line) (ev E, emit bool, err error)` MUST distinguish three outcomes and the `Twin` MUST honor them: `emit == false, err == nil` skips the line (not reactor-relevant, never a crash); `err != nil` is a FATAL transport failure, on which the `Twin` MUST emit `codec.ErrorEvent(err.Error())` and close the stream; `emit == true, err == nil` delivers `ev`. A parse failure MUST be treated as fatal, never silently skipped. The codec MUST also supply `ErrorEvent(msg) E` (the vertical's transport-error terminal event) and `DisconnectEvent() E` (the vertical's connection-lost event); the fault injector consumes these. A vertical lacking a natural disconnect concept MUST supply its terminal `restart_failed`-class event as `DisconnectEvent()`.

Tags: mechanism

#### RS-010 — Scanner buffer defaults to 1 MB

The `Twin`'s line scanner MUST default its buffer capacity to at least 1 MB, not a 64 KB assumption, so an oversized corpus line does not truncate the replay invisibly (a short read that closes the source early and masquerades as a short corpus). The substrate SHOULD provide a `WithBufferSize(n)` functional option for verticals that need a larger buffer.

Tags: mechanism

#### RS-011 — Channel-of-values delivery shape

The `Twin` and every `EventSource[E]` MUST deliver events as channel-of-values with close-on-exhaustion (`Events(ctx) <-chan E`), and MUST stop producing and close the channel when `ctx` is cancelled. This shape is accepted as a deliberate lock-in: the substrate MUST NOT expose a pull-based or error-returning iterator variant (`Next() (E, error)`) in this version. A pull-based live source (e.g. a poll loop) MUST adapt to the channel shape via a goroutine.

Tags: mechanism

### 4.4 The fault-injection model

#### RS-012 — The four vertical-neutral fault modes

The `Twin` MUST implement, selected by `FaultConfig{Mode, EventN}` with `EventN` 1-based over the post-skip event stream, exactly these modes, stated in vertical-neutral terms (the taxonomy detail is §8):

- `FaultDropAfter` — deliver events 1..N, then deliver the vertical's connection-lost event (`codec.DisconnectEvent()`) and end the stream.
- `FaultTruncate` — replace event N with the vertical's transport-error event (`codec.ErrorEvent(...)`) and end the stream.
- `FaultStall` — deliver events 1..N-1, then block before event N until `ctx` is cancelled.
- `FaultDup` — deliver event N, then deliver the identical event value a second time.
- `FaultNone` — faithful replay with no injection.

The spec text MUST state these modes in vertical-neutral vocabulary; the substrate MUST NOT name transport enums such as "Disconnected", "Error", or a "Seq" field. Same-identity or same-sequence duplication is a consequence of a vertical re-delivering its own event value under `FaultDup`, NOT part of the substrate definition.

Tags: mechanism

### 4.5 The two-layer decode discipline

#### RS-014 — Two-layer decode discipline (reusable pattern)

The substrate MUST state the two-layer decode discipline as a normative, reusable pattern that a vertical's codec follows, while keeping the concrete framing in the vertical:

- Layer 1 (strict outer): the transport/envelope frame MUST be parsed strictly; a malformed envelope is a fatal decode error (`DecodeLine` returns `err != nil` per RS-009).
- Layer 2 (tolerant inner): the per-message payload MUST be parsed tolerantly — unmodeled fields are preserved and counted, and an unknown message maps to a typed-raw value delivered as a skip (`emit == false`), never a crash.

The concrete framing machinery (a vertical's `Frame`/`FrameKind`/`Extra` types and its `parseExtra`) MUST stay in the vertical's package; the substrate owns only the pattern statement. The strict-outer half is what the [event-model.md §4.7 EV-049] `DecodePayloadStrict` variant enforces on the invariant-checker side; the tolerant-inner half is what the `DecodeLine` skip-vs-fatal contract embodies on the replay side.

Tags: mechanism

### 4.6 The determinism port

#### RS-015 — `ClockPort` + `Ticker` is the required determinism port

The substrate MUST define `ClockPort` with methods `Now() time.Time`, `Since(t) time.Duration`, `NewTicker(d) Ticker`, and `Sleep(ctx, d) bool`, and `Ticker` with `C() <-chan time.Time` and `Stop()`. `Sleep` MUST wait for `d` or until `ctx` is cancelled and MUST report via its `bool` return whether the full `d` elapsed (a bare `Sleep(d)` that cannot honor cancellation is non-conformant). The substrate MUST ship a real implementation that delegates to `time`, and a fake implementation that advances virtual time deterministically. A vertical that wants deterministic replay of timeouts and poll races MUST read time only through `ClockPort`, never via direct `time` calls. The fake ticker MUST reproduce `time.Ticker`'s first-tick-after-one-full-interval semantics so replayed poll quantization matches a live recording. `ClockPort` is homed in the substrate because it is reused across verticals (keeper now; the daemon is a forward-compat note only and is out of scope). (Signatures in §6.4.)

Tags: mechanism

### 4.7 The capture-tee primitive

#### RS-016 — Capture-tee owns no file format

The substrate MUST recognize a protocol-agnostic capture-tee primitive (`Tap`-shaped: a stdio splice that copies a stream through to its normal consumer while teeing a byte-identical copy to a caller-supplied `io.Writer`). The tee MUST own no file format and MUST NOT open or name any file — persistence is the consumer's `io.Writer`. This spec MAY govern the tee contract while the code physically resides at `internal/apptap`; spec scope and package boundary need not coincide. Folding a process-spawning or `os/exec`-importing tee into the stdlib-only leaf (RS-005) is non-conformant; if physical co-location is ever wanted, the only acceptable shape is a `internal/substrate/tap` sub-package with its own file-scoped depguard entry quarantining `os/exec`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=n/a; idempotency=idempotent

### 4.8 The L0–L3 taxonomy and measurement policy

#### RS-017 — The L0–L3 test taxonomy is normative

The substrate MUST define, and every vertical MUST realize, a four-tier test taxonomy: L0 unit (pure `Step` transitions and codec golden-decode + malformed-line tables via `SyntheticSource` + `FakeEffector`); L1 contract (whole-corpus decode with zero unknown types, plus golden action-sequence replay `Twin → reactor → FakeEffector → DeepEqual(want)`); L2 integration (corpus → `Twin` → reactor → vertical bridge sink happy path, plus at least one fault case per mode asserting a terminal signal); L3 live (the single env-gated tier exercising a real subprocess/pane). Tiers L0–L2 MUST be zero-token and MUST run with the live env-gate off, touching no model or subprocess — at least the codex "~95% zero-token" property. Only L3 MAY require the live gate. A vertical MUST NOT introduce a runtime test-branch to select doubles; substitution MUST be by which `EventSource`/`Effector` value is wired (consistent with the SH-018 / SH-INV-001 no-test-branch discipline).

Tags: mechanism

#### RS-018 — The per-vertical minimum-artifact list

Because nothing compiles the taxonomy, a vertical is conformant only if it supplies every artifact below; a vertical missing any one is non-conformant:

1. Four tier files (L0 unit, L1 contract, L2 integration, L3 live) in `internal/<v>test/`.
2. A drift canary test asserting the corpus is non-empty, valid, codec-parseable, has zero unknown/raw frames, and meets a minimum frame/cycle count.
3. A recorded corpus under `testdata/<vertical>/corpus/*.jsonl` plus a `CAPTURE-LOG.md` ledger recording that captures are deliberate and budget-capped.
4. A Makefile target pair: one target running L0–L2 with the live gate off, and one running only the live tier with the gate on.
5. Exactly one `<V>_LIVE` env-gate variable whose `"1"` value enables the live tier.

The substrate MUST provide the reusable code these tiers import (`FakeEffector[A]`, `SyntheticSource[E]`, `Twin[E]` + `FaultConfig`/`FaultMode`, `Run`, `ClockPort` + real/fake clocks) so a vertical writes zero test-double plumbing. The substrate MUST NOT extract a generic bridge sink; bridge sinks stay per-vertical and test-local (two dissimilar users would make a shared sink invented abstraction).

Tags: mechanism

#### RS-019 — Makefile-target-pair and env-gate convention

A vertical MUST wire its taxonomy through a Makefile target pair and a single `<V>_LIVE` env gate (RS-018 items 4–5), mirroring the codex `CODEX_LIVE` convention, so the zero-token tiers run by default and the live tier is opt-in. The corpus-path resolution idiom (caller-relative path to `testdata/<v>/corpus`) is part of the copied template, not substrate code.

Tags: mechanism

#### RS-020 — Measurement is replay-vs-baseline, not live A/B

Correctness for a vertical on this seam MUST be proven by replay-regression over a recorded corpus plus a fault-injection pass-rate versus a frozen baseline — NOT by live A/B. The acceptance shape MUST include: (1) N consecutive green replay-plus-fault runs (determinism, made repeatable by fake-clock virtual time + `SyntheticSource` fixed order + faithful `Twin` replay); (2) a fault matrix in which every fault mode yields a terminal signal, never silence (RS-INV-003); (3) an out-of-band check in which the harness does not grade itself — the `FakeEffector.Actions()` log and the replayed event stream are read by an independent tool (e.g. `jq`/`grep`/`stat`); and (4) a measured-and-stated coverage floor on the reactor files. The substrate provides the deterministic mechanisms; the corpus, the baseline numbers, and any old-vs-new differential are the vertical's.

Tags: mechanism

### 4.9 Instantiations

#### RS-021 — Codex is the reference instantiation and MUST stay green

The codex app-server stack MUST be the first instantiation of this seam and MUST re-express itself on it — re-instantiating the moved symbols via type aliases (`=`) and const re-exports so that `internal/codextest` compiles and passes with zero diff to any `_test.go` file. Keeping the codex tiers green (L0/L1/L2 with the live gate off) is the observable proof that the extraction is generic and not a codex-shaped copy. A re-instantiation that uses defined types instead of aliases, or that changes any codex test, is non-conformant.

Tags: mechanism

#### RS-022 — Session-keeper is the second instantiation and validates the boundary

The session-restart (session-keeper) vertical MUST be the second instantiation and is the validation of the abstraction: it MUST host on the seam (its own `Event`/`Action`/`Step`, a `ReplayCodec[keeper.Event]`, a per-vertical bridge sink, a corpus, the four tiers + canary + Makefile pair, and `ClockPort` for its clock sites) with no codex vocabulary entering the substrate. If the seam cannot host the keeper vertical without codex-specific leakage, the extraction boundary is wrong and MUST be corrected rather than special-cased.

Tags: mechanism

### 4.10 Naming disambiguation

#### RS-023 — Disambiguation from the three prior "substrate" senses

This spec's "substrate" MUST be read as the record→replay seam only. It MUST NOT be conflated with the three prior normative senses of "substrate" in the spec tree: (1) the process-spawn seam (`internal/handler.Substrate`, the PL-021b Substrate seam, PI-012a, CI-004); (2) the cognition session substrate (CL-015 / CL-024 "substrate teardown"); (3) the transport/production substrate ([credential-isolation.md §2.2] "LLM transport substrate", pi-harness PI-069). The `replay-substrate` spec-file name and the `RS` prefix satisfy this disambiguation by construction; those three specs keep their own "substrate" meanings intact and are not edited by this spec.

Tags: mechanism

## 5. Invariants

#### RS-INV-001 — Any source × any effector composes

For every `EventSource[E]`, every reactor `Step` of type `func(E) []A`, and every `Effector[A]`, `Run` MUST drive the composition identically regardless of whether the source is live, synthetic, or a replay `Twin`, and regardless of whether the effector is real, a recorder, or a bridge sink. Substitutability of any layer for a testable fake is the load-bearing property of the seam; no requirement in any vertical MUST break it.

Tags: mechanism

#### RS-INV-002 — `DecodeLine` determinism

For a given codec state and a given input line, `DecodeLine` MUST be a deterministic function into exactly one of the three outcomes {skip, fatal, emit}; it MUST NOT depend on wall-clock time, goroutine scheduling, or any nondeterministic source. Any per-line sequence counter is codec-internal state advanced deterministically per line.

Tags: mechanism

#### RS-INV-003 — Fault → terminal, never silence

Every fault mode MUST yield a terminal signal, never silence: `FaultDropAfter` and `FaultTruncate` terminate the stream with a synthetic terminal event; `FaultStall` terminates by consumer ctx-timeout (the consumer MUST time out, never hang); `FaultDup` terminates normally after the doubled delivery; `FaultNone` terminates on faithful exhaustion. A vertical replaying under any fault mode MUST NOT be able to observe an indefinitely silent stream.

Tags: mechanism

#### RS-INV-004 — Multi-appender corpus is EventID-sorted before replay

Where a corpus is assembled from multiple appenders whose per-process EventID generators mean on-disk file order does not equal global EventID order, the harness MUST sort the corpus by EventID after collection and before it becomes a `Twin`'s `io.Reader`. The `Twin` itself is a faithful line-reader and MUST NOT reorder; the ordering guarantee for cross-process replay determinism is the harness's obligation.

Tags: mechanism

## 6. Schemas and data shapes

All signatures below are the pinned substrate surface, restated verbatim from the change-design pins. Types in use are Go standard-library types plus the vertical's type parameters `E` (event) and `A` (action).

### 6.1 The seam (`seam.go`)

```go
type EventSource[E any] interface{ Events(ctx context.Context) <-chan E }
type Effector[A any]    interface{ Execute(ctx context.Context, a A) error }

// Run is a FREE FUNCTION, not a method — Go forbids generic methods, and the
// vertical's Step carries vertical semantics, not substrate semantics.
func Run[E, A any](ctx context.Context, src EventSource[E], step func(E) []A, eff Effector[A]) error
```

### 6.2 The test doubles (`doubles.go`)

```go
type FakeEffector[A any] struct{ /* mu sync.Mutex; actions []A */ }
func (f *FakeEffector[A]) Execute(ctx context.Context, a A) error // append under mu
func (f *FakeEffector[A]) Actions() []A                           // returns a copy
func (f *FakeEffector[A]) Reset()                                 // actions = nil

type SyntheticSource[E any] struct{ /* events []E */ }
func NewSyntheticSource[E any](events []E) *SyntheticSource[E]
func (s *SyntheticSource[E]) Events(ctx context.Context) <-chan E // buffered, pre-filled, closed;
                                                                  // returns closed chan if ctx already cancelled
```

### 6.3 The replay engine (`replay.go`)

```go
type FaultMode int
const ( FaultNone FaultMode = iota; FaultDropAfter; FaultStall; FaultTruncate; FaultDup )
type FaultConfig struct { Mode FaultMode; EventN int } // EventN 1-based

// ReplayCodec is everything a vertical supplies to replay its corpus. It fuses the
// vertical's decode, error-policy, filter, and map steps into DecodeLine and supplies
// the two synthetic-event constructors the fault injector needs. Implementations MAY
// be stateful (own their sequence counter).
type ReplayCodec[E any] interface {
    // DecodeLine decodes one corpus line.
    //   emit=false, err=nil  → skip this line (not reactor-relevant).
    //   err!=nil             → FATAL transport failure: twin emits ErrorEvent(err) and closes.
    //   emit=true,  err=nil  → deliver ev.
    DecodeLine(line []byte) (ev E, emit bool, err error)
    ErrorEvent(msg string) E   // vertical's transport-error terminal event (decode err, FaultTruncate)
    DisconnectEvent() E        // vertical's connection-lost event (FaultDropAfter)
}

type Twin[E any] struct{ /* corpus io.Reader; fault FaultConfig; codec ReplayCodec[E]; bufCap int */ }
func NewTwin[E any](corpus io.Reader, fault FaultConfig, codec ReplayCodec[E]) *Twin[E]
func (t *Twin[E]) Events(ctx context.Context) <-chan E
// WithBufferSize is a functional option on NewTwin; the default buffer is 1 MB (RS-010).
```

### 6.4 The determinism port (`clock.go`)

```go
type ClockPort interface {
    Now() time.Time
    Since(t time.Time) time.Duration
    NewTicker(d time.Duration) Ticker
    // Sleep waits d or until ctx cancels; reports whether the full d elapsed.
    Sleep(ctx context.Context, d time.Duration) bool
}
type Ticker interface { C() <-chan time.Time; Stop() }
```

The real `SystemClock` delegates to `time`; the `FakeClock` advances virtual time by an explicit `Advance(d)` driver (not part of `ClockPort`) and reproduces `time.Ticker` first-tick-after-interval semantics (RS-015).

### 6.5 Schema evolution

None of the surfaces above is a versioned wire format; they are in-process Go interfaces. There is no on-disk schema owned by this spec. The corpus is append-only NDJSON whose per-line shape is the vertical's codec's concern, not the substrate's.

## 7. Protocols and state machines

### 7.1 The `Run` driver loop

The seam's only protocol is the driver loop (normative force in RS-002):

```
FUNCTION Run(ctx, src, step, eff):
    FOR ev IN src.Events(ctx):          -- ranges until the source closes its channel
        FOR a IN step(ev):              -- vertical Step; pure, returns []A in order
            IF err := eff.Execute(ctx, a); err != nil:
                RETURN err              -- first effector error; no further actions
    RETURN nil                          -- source exhausted (channel closed)
```

Cancellation is delivered through `ctx` into `src.Events`, whose producing goroutine stops and closes its channel; the loop then drains no further. `Run` never closes the source channel (RS-002). The `Twin` fault modes (§8) act inside `src.Events`, so `Run` observes them only as ordinary events or an early channel close.

## 8. Error and failure taxonomy

This section enumerates the failure classes the substrate injects and routes on; the normative force lives in RS-009, RS-012, and RS-INV-003.

### 8.1 Decode outcomes (per corpus line)

- **skip** — `DecodeLine` returns `emit == false, err == nil`; the line is not reactor-relevant (e.g. an interleaved non-cycle bus line, or a frame the vertical filters). Not an error.
- **fatal** — `DecodeLine` returns `err != nil`; a parse/transport failure. The `Twin` emits `codec.ErrorEvent(err.Error())` and closes. A parse failure is never a silent skip.
- **emit** — `DecodeLine` returns `emit == true, err == nil`; the event is delivered.

### 8.2 Injected fault modes (per replay run)

| Mode | Definition (vertical-neutral) | Terminal signal |
|---|---|---|
| `FaultNone` | Faithful replay, no injection. | Channel close on exhaustion. |
| `FaultDropAfter` | Deliver events 1..N, then deliver `codec.DisconnectEvent()` and end. | Synthetic connection-lost event, then close. |
| `FaultTruncate` | Replace event N with `codec.ErrorEvent(...)` and end. | Synthetic transport-error event, then close. |
| `FaultStall` | Deliver events 1..N-1, then block before event N until `ctx` cancels. | Consumer ctx-timeout (bounded-liveness probe). |
| `FaultDup` | Deliver event N, then deliver the identical value a second time. | Normal close after the doubled delivery (idempotence probe). |

Every row terminates (RS-INV-003): no fault mode may produce indefinite silence. `EventN` is 1-based over the post-skip stream.

## 9. Cross-references

### 9.1 Depends on

- **[event-model.md §4.5 EV-021]** (observational replay-reader classification), **[event-model.md §4.6 EV-047]** (the `ScanAfter` offline-scan read surface declared normative), and **[event-model.md §4.7 EV-049]** (the `DecodePayloadStrict` variant) — a vertical's replay/invariant harness consumes these; this spec's measurement and two-layer-decode clauses (RS-014, RS-020) build on that declared read surface.

### 9.2 Reverse dependencies

> INFORMATIVE: Reverse dependencies are computed on demand by walking every spec's `depends-on` list; they are not stored here (see §0). The forthcoming session-keeper spec (SK) will depend on this spec for the seam, `ClockPort`, taxonomy, and measurement standard it instantiates.

### 9.3 Co-references (read-only consumption)

- **[scenario-harness.md] SH-018 / SH-INV-001** — this spec's zero-token L0–L2 tiers (RS-017) are consistent with, and do not contradict, the scenario-harness no-test-branch discipline and its cadence taxonomy; substitution is by wired value, not runtime branch. A mutual pointer is added at integration. No reverse dependency.
- **[handler-contract.md] HC-035** — HC-035 disclaims governing the in-process-fake surface; this spec now governs that surface (the seam + the two test doubles, RS-001/RS-006/RS-007). The two become mutually discoverable at integration. No reverse dependency.
- **session-keeper (prefix SK, not yet landed)** — the second instantiation (RS-022). The pointer becomes a concrete cross-reference once the SK spec lands.
- **[run-state-machine.md] (RSM)** — the daemon run vertical, third instantiation of the replay-substrate seam.

## 10. Conformance

### 10.1 Conformance profiles

- **Core substrate** — a conforming substrate package satisfies RS-001 … RS-016 and RS-INV-001 … RS-INV-004: the typed seam and free-function `Run`; the two test doubles; the `ReplayCodec`+`Twin` engine with the skip-vs-fatal split, the two synthetic-event constructors, the 1 MB buffer default, and the channel shape; the four vertical-neutral fault modes with the terminal-never-silence property; the two-layer decode statement; the `ClockPort` real+fake pair; the capture-tee ownership rule; and the stdlib-only leaf direction.
- **Instantiation obligations** — a conforming vertical satisfies RS-017 … RS-020 (taxonomy, minimum-artifact list, Makefile/env-gate wiring, replay-vs-baseline measurement). The codex reference satisfies RS-021; the session-keeper vertical satisfies RS-022.

### 10.2 Test-surface obligations

- The codex tiers (`internal/codextest`, L0/L1/L2 with the live gate off) are green with zero diff to any `_test.go` file — the observable proof of RS-021.
- The substrate package carries its own tier-independent tests exercising each primitive with a throwaway `intEvent`/`intAction` instantiation, so the generics are covered independent of any vertical.
- A vertical supplies the RS-018 minimum-artifact list, and its fault matrix demonstrates RS-INV-003 (every mode terminal).
- The seam is typed (RS-003) and stringly-typed or `any`-boundary implementations do not conform.

> INFORMATIVE: [testing.md] does not yet exist; these obligations are stated in prose and migrate to a spec reference within one revision cycle once a testing spec lands (see OQ-RS-001).

### 10.3 Excluded conformance claims

- This spec grants no conformance over any vertical's concrete `Event`/`Action`/codec/corpus, nor over the session-keeper cycle semantics or its baseline numbers (owned by SK).
- It grants no conformance over the event registration, payload schemas, or `run_id`/`cycle_id` joinability (owned by [event-model.md §8]).
- It grants no conformance over the process-spawn, cognition, or transport "substrate" senses (RS-023).

## 11. Open questions

#### OQ-RS-001 — Migrate test-surface obligations to a testing spec

Question: Once a `testing.md` spec exists, migrate the §10.2 prose obligations to spec references.
Owner: foundation-author
Blocks: none (bootstrap-period prose citation is permitted).
Default-if-unresolved: keep the prose obligations in §10.2.

#### OQ-RS-002 — Codex-side companion depguard rule

Question: Should a depguard rule be added on the codex vertical side (codex packages allow `$gostd` + substrate + `codexwire` + self; deny daemon and sibling verticals), pinning "verticals depend on substrate, never sideways"?
Owner: foundation-author
Blocks: none (RS-005 covers the substrate-leaf side; the companion rule is a recommendation, not required for the extraction to compile).
Default-if-unresolved: recommended-not-required; the substrate leaf rule alone ships.

#### OQ-RS-003 — Physical placement of the capture-tee

Question: Should the capture-tee code physically move under `internal/substrate/tap`, or stay at `internal/apptap`?
Owner: foundation-author
Blocks: none (RS-016 governs the contract regardless of physical location; the leaf must stay `os/exec`-free either way).
Default-if-unresolved: stays at `internal/apptap` (zero importers, already generic); a move requires a file-scoped depguard entry quarantining `os/exec`.

## 12. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-07-13 | 0.1.0 | foundation-author | Initial draft (codename: session-restart-substrate). The generic record→replay seam (`EventSource`/`Effector`/`Run`), `ReplayCodec[E]`+`Twin[E]`+`FaultConfig`, the two test doubles, `ClockPort`+`Ticker`, the two-layer decode discipline, the L0–L3 taxonomy + measurement policy, and the codex/session-keeper instantiation obligations. Requirement IDs carried over from design `SB-*` to `RS-*`. |
```
