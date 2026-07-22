# 04-Design / substrate — the generic record→replay seam (`internal/substrate`)

> **Pass 4 (Change Design), substrate component.** How to build the extraction. Elaborates within
> the pinned contracts in `00-decisions.md` (D1–D5, D9-fileorder, D13) — it MUST NOT alter any pin;
> where a pin fixes a signature this doc restates it verbatim and designs the *semantics, edge
> cases, and migration* around it. Grounded in `03-research/substrate/findings.md` and
> `03-research/session-keeper/findings.md` (clock sites). All repo paths absolute from
> `/Users/gb/github/harmonik/`. Requirement IDs stay `SB-*` here; they become `RS-*` at spec-draft
> (D5). Not spec text — this is the build plan the spec-drafter (pass 5) and implementer follow.

---

## 0. Design thesis in one paragraph

`internal/substrate` is a **stdlib-only leaf** holding four generic primitives — the `EventSource`/
`Effector`/`Run` seam, the two test doubles, the `Twin[E]`+`ReplayCodec[E]`+`FaultConfig` replay
engine, and the `ClockPort`+`Ticker` determinism port with a real and a fake impl. Every codex type
stays where it is; codex re-instantiates the seam through **type aliases + const re-exports + one
codec + one wrapper Twin**, which is what makes `internal/codextest` compile with zero edits (D1;
findings §2.1). `internal/apptap` stays a **separate** package (findings §5). The abstraction is
proven generic by hosting the codex vertical unchanged and the keeper vertical (SK) with no codex
vocabulary in the seam.

---

## 1. Package layout — `internal/substrate`

A **single flat package** `substrate`, one concept per file. Stdlib imports only:
`context`, `sync`, `io`, `bufio`, `bytes`, `time` (findings §5: leaf, `$gostd` suffices; MUST NOT
import `internal/core` — event/action types are type parameters, nothing here touches `core`).

| File | Contents |
|---|---|
| `seam.go` | `EventSource[E]`, `Effector[A]` interfaces; `Run[E, A]` free function (the driver loop). |
| `doubles.go` | `FakeEffector[A]` (mutex + `[]A`, `Execute`/`Actions`/`Reset`); `SyntheticSource[E]` + `NewSyntheticSource[E]` (fixed-slice source, buffered-then-closed, honors pre-cancelled ctx). |
| `replay.go` | `FaultMode` + 5 consts; `FaultConfig`; `ReplayCodec[E]` interface; `Twin[E]` + `NewTwin[E]` + `Events`; the internal `replay` loop + a `WithBufferSize` option. |
| `clock.go` | `ClockPort` + `Ticker` interfaces; `SystemClock` (real, delegates to `time`); its `systemTicker`. |
| `fakeclock.go` | `FakeClock` (virtual time) + `fakeTicker`; `Advance`, `BlockUntil` test drivers. |
| `doc.go` | Package doc: the seam contract in prose + the "not to be confused with the process-spawn / cognition / transport substrates" pointer (D5 reduces SB-R14 to this one line). |

Test files (`*_test.go`, `package substrate_test`) exercise each primitive with a throwaway
`intEvent`/`intAction` instantiation so the generics themselves are covered independent of any
vertical — this is the substrate's *own* L0.

### 1.1 The pinned surface (restated verbatim from D1/D2/D4 — do not re-derive)

```go
package substrate

// ── seam.go ──
type EventSource[E any] interface{ Events(ctx context.Context) <-chan E }
type Effector[A any]    interface{ Execute(ctx context.Context, a A) error }

// Run is a FREE FUNCTION, not a method: Go forbids generic methods, and Step
// carries vertical semantics, not substrate semantics (findings §2, R5).
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

// ── doubles.go ──
type FakeEffector[A any] struct {
    mu      sync.Mutex
    actions []A
}
func (f *FakeEffector[A]) Execute(_ context.Context, a A) error // append under mu
func (f *FakeEffector[A]) Actions() []A                          // returns a copy
func (f *FakeEffector[A]) Reset()                                // actions = nil

type SyntheticSource[E any] struct{ events []E }
func NewSyntheticSource[E any](events []E) *SyntheticSource[E]
func (s *SyntheticSource[E]) Events(ctx context.Context) <-chan E // buffered, pre-filled, closed;
                                                                  // returns closed chan if ctx already cancelled
```

`Run`'s only behavioral note beyond the loop: it returns `nil` on source exhaustion (channel close)
and the **first** effector error otherwise — identical to codex `reactor.go:282-292`. It never
closes the source's channel (the source owns that, via `defer close`). Cancellation is delivered
through `ctx` into `src.Events`, whose goroutine must stop producing — the loop drains no further
because the source closes its channel on `ctx.Done()`.

### 1.2 `internal/apptap` stays SEPARATE — confirmed (findings §5)

Do **not** fold `apptap` into `substrate`. Three reasons, all from the inventory:
1. It is already 100% generic with **zero** code changes needed (`tap.go:21` declares itself
   protocol-agnostic) and **zero** importers anywhere — folding buys nothing mechanically.
2. It imports `os/exec` and spawns processes (`tap.go:96,108`). Keeping it out lets the substrate
   depguard rule stay **pure/exec-free** — worth having for a package whose whole point is
   deterministic in-memory replay.
3. `specs/replay-substrate.md` (SB-R11) can normatively own the `Tap` capture-tee contract while
   the code physically stays at `internal/apptap` — **spec scope and package boundary need not
   coincide.** SB-R11 states the tee owns no file format (persistence is a consumer-provided
   `io.Writer`, `tap.go:58-63`) and points at `internal/apptap` as its instantiation.

If physical co-location is ever wanted, the only acceptable shape is `internal/substrate/tap` as a
sub-package with its own file-scoped depguard entry (quarantining `os/exec`); folding `exec` into
the flat leaf is **off the table**.

### 1.3 depguard rules (SB-R13 direction enforcement)

**Leaf rule (substrate side) — REQUIRED.** Add to `.golangci.yml` `depguard.rules` (modeled on the
`schedule` leaf at `:162-168` and the keeper deny-list at `:124-136`):

```yaml
# substrate: generic record→replay seam (codename:session-restart-substrate).
# Leaf: stdlib + self-import only. Verticals instantiate substrate, never the reverse.
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

**Companion rule (codex vertical side) — RECOMMENDED (D1 open-item, findings §5).** No depguard
rule exists today for any codex package or apptap (grep of `.golangci.yml`: zero hits for
`codex`/`apptap`). Pin the direction so a future edit can't make codex import the daemon or a
sibling vertical: `codexreactor` / `codexdigitaltwin` allow `$gostd` + `substrate` + `codexwire`
+ self, deny `daemon` and `keeper`. Cheap, consistent with the matrix idiom, and it makes the
"verticals depend on substrate, never sideways" invariant machine-checked rather than aspirational.
This is a recommendation the spec should record; it is not blocking for the extraction to compile.

**Port-idiom conformance (SB-R13):** every interface is a 1–3-method consumer-owned narrow
interface satisfied structurally, matching `internal/queue` house style (`QueueSetter`
`queue/rpc.go:56-59`, `EventEmitter` `rpc.go:66-68`). No `Result`/`Option`/`Either` containers
anywhere (Constraint 3).

---

## 2. The codex re-instantiation, file by file (D1, R7)

The whole extraction's Goal-2 proof is: **`internal/codextest/*` changes by zero lines.** That
holds iff codex re-exports every moved symbol under its *original name and type identity* using
**aliases (`=`)**, not defined types. Below is exactly what each codex file becomes.

### 2.1 `internal/codexreactor/reactor.go`

**Unchanged (the codex vertical, never enters substrate):** `Event`/`EventType` (`:32-52`,
`:62-73`), `Action`/`ActionType` (`:78-96`, `:102-112`), `State`/`Reactor`/`New`/`State()`
(`:143-176`), and `Step` (`:185-277`) with invariants I1 (one-turn-in-flight) and I2 (dedup-by-seq).
`Step` stays codex-typed: `func (r *Reactor) Step(ev Event) []Action`.

**Delete the interface bodies (`:120-122`, `:133-137`); replace with aliases to the instantiation:**

```go
import "github.com/gregberns/harmonik/internal/substrate"

// Aliases (=), NOT defined types. Every existing reference — codexdigitaltwin.Twin
// satisfying EventSource structurally, HarmonikBridgeSink implementing Effector
// (l2_integration_hkoe86p_test.go:101) — keeps compiling untouched.
type EventSource = substrate.EventSource[Event]
type Effector    = substrate.Effector[Action]
```

**`Run` becomes a one-line wrapper** (preserves all call sites `l1:199`, `l2:178`, `l2:256`,
`reactor_test.go`):

```go
func (r *Reactor) Run(ctx context.Context, src EventSource, eff Effector) error {
    return substrate.Run(ctx, src, r.Step, eff)
}
```

`r.Step` is a bound method value of type `func(Event) []Action`, exactly `Run`'s `step` parameter
after `E=Event, A=Action` instantiation. No adapter, no boxing.

### 2.2 `internal/codexreactor/fake.go` — shrinks to three lines

```go
type FakeEffector    = substrate.FakeEffector[Action]
type SyntheticSource = substrate.SyntheticSource[Event]
func NewSyntheticSource(events []Event) *SyntheticSource { return substrate.NewSyntheticSource(events) }
```

Composite literals like `&codexreactor.FakeEffector{}` (`l1:197`) keep compiling because an alias
of an instantiated generic type is a plain named struct type — literals, method calls, and
`reflect.DeepEqual([]Action, …)` all resolve to concrete codex types. The old
`FakeEffector`/`SyntheticSource` bodies (`fake.go:13-68`) are **deleted** (moved to
`substrate/doubles.go`).

### 2.3 `internal/codexdigitaltwin/twin.go` — codec + wrapper Twin

`FaultMode`/`FaultConfig`/`Twin`/`New`/`Events` move to substrate; codex keeps the *names* via
re-exports and gains one codec.

**(a) `codexCodec` — implements `substrate.ReplayCodec[codexreactor.Event]`.** This is where the
five codex leak points from the replay loop (findings §1.5: `twin.go:132/135-139/144/148/164-177`)
land, fused into three methods. The `seq` counter (`twin.go:120`, a `*uint64`) becomes codec-internal
state — it disappears from the substrate surface entirely (D2; R2):

```go
type codexCodec struct{ seq uint64 } // was the *uint64 threaded through frameToEvent (twin.go:120,202)

// DecodeLine fuses decode (twin.go:132) + filter (twin.go:144) + map (twin.go:148).
func (c *codexCodec) DecodeLine(line []byte) (codexreactor.Event, bool, error) {
    frame, err := codexwire.Parse(line)
    if err != nil {
        return codexreactor.Event{}, false, err // FATAL → twin emits ErrorEvent(err), closes (was twin.go:133-139)
    }
    if frame.Kind != codexwire.FrameKindServerNotification {
        return codexreactor.Event{}, false, nil // SKIP, not fatal (was twin.go:142-146)
    }
    ev, ok := frameToEvent(frame, &c.seq) // existing table, seq now codec-owned (twin.go:202-277)
    return ev, ok, nil                    // ok==false → skip (was twin.go:148-152)
}

// ErrorEvent = the codex transport-error event (was twin.go:137, :175).
func (c *codexCodec) ErrorEvent(msg string) codexreactor.Event {
    return codexreactor.Event{Seq: c.seq, Type: codexreactor.EventTypeError, Message: msg}
}

// DisconnectEvent = the codex connection-lost event (was twin.go:164).
func (c *codexCodec) DisconnectEvent() codexreactor.Event {
    return codexreactor.Event{Seq: 0, Type: codexreactor.EventTypeDisconnected}
}
```

Note `FaultTruncate`'s "current seq" want (`twin.go:175`) is satisfied because `ErrorEvent` reads
`c.seq`, which the last `frameToEvent` call advanced — the stateful codec makes this fall out for
free (findings §1.5 seq-threading paragraph). `FaultDup`'s same-Seq guarantee is automatic: the
Twin re-sends the identical `Event` value, so its `Seq` is unchanged (findings §1.5).

**(b) back-compat re-exports + wrapper Twin:**

```go
type FaultConfig = substrate.FaultConfig
type FaultMode   = substrate.FaultMode
const (
    FaultNone      = substrate.FaultNone
    FaultDropAfter = substrate.FaultDropAfter
    FaultStall     = substrate.FaultStall
    FaultTruncate  = substrate.FaultTruncate
    FaultDup       = substrate.FaultDup
)

type Twin struct{ inner *substrate.Twin[codexreactor.Event] }

func New(corpus io.Reader, fault FaultConfig) *Twin {
    return &Twin{inner: substrate.NewTwin(corpus, fault, &codexCodec{})}
}
func (t *Twin) Events(ctx context.Context) <-chan codexreactor.Event { return t.inner.Events(ctx) }
```

`New`'s signature is byte-identical to `twin.go:88-90`, so
`codexdigitaltwin.New(corpus, codexdigitaltwin.FaultConfig{Mode: codexdigitaltwin.FaultDropAfter, EventN: 2})`
at `l2:252` compiles untouched. `frameToEvent` (`twin.go:202-277`) and `codexwire` stay in codex —
substrate sees only `[]byte → (Event, bool, error)`.

### 2.4 codextest needs ZERO changes — the R7 implementer checklist

Every symbol codextest touches keeps its name and type identity. The extraction is correct iff **all**
of the following hold after the change (use aliases, not defined types — R7):

- [ ] `codexreactor.EventSource` / `.Effector` are `=` aliases (§2.1). A *defined* type would break
      structural satisfaction of `codexdigitaltwin.Twin` as an `EventSource` and `HarmonikBridgeSink`
      as an `Effector` (`l2:101`).
- [ ] `codexreactor.FakeEffector` / `.SyntheticSource` are `=` aliases (§2.2). Defined types break
      the composite literal `&codexreactor.FakeEffector{}` (`l1:197`) and
      `reflect.DeepEqual(got, want []codexreactor.Action)` (`l1:203-214`).
- [ ] `codexdigitaltwin.FaultConfig`/`FaultMode` are `=` aliases and `FaultNone…FaultDup` are const
      re-exports (§2.3). Otherwise `FaultConfig{Mode: FaultDropAfter, …}` (`l2:252`) fails.
- [ ] `codexdigitaltwin.New` keeps signature `func(io.Reader, FaultConfig) *Twin`; `.Events` keeps
      return `<-chan codexreactor.Event`.
- [ ] `codexreactor.Reactor.Run(ctx, EventSource, Effector) error` unchanged (§2.1).
- [ ] `go test ./internal/codextest/... -count=1` (L0/L1/L2, `CODEX_LIVE=0`) is green with **no diff
      to any `_test.go` file** — the observable proof of Goal 2. Run this as the gate on the codex
      commit (§7).

**Blast radius is contained (findings header, import grep):** nothing outside the codex family
imports `codexreactor`/`codexdigitaltwin`/`codexwire`/`apptap`. Importers are exactly
`codexdigitaltwin/twin.go`, the four `codextest` tiers + canary, and each package's own `_test.go`.
`apptap` has zero importers. So this whole re-instantiation touches three codex files + the new
`substrate` package and nothing else compiles differently.

---

## 3. `FakeClock` design (D4) — the load-bearing determinism piece

`ClockPort`+`Ticker` are pinned (D4). Two impls ship in substrate. The **real** one is trivial;
the **fake** one is where determinism is bought, and it serves **both** codex (which does not use it
today but may) and keeper (the actual consumer, whose 34 `cycle.go` clock sites + 2 poll loops all
route through it — session-keeper findings §2b).

### 3.1 Pinned interface (restated from D4)

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

`Since` is deliberately kept (not derived at call sites) because 13 `cycle.go` sites read as
interval gates and a fake wants **one advance-point**, not arithmetic scattered across the ladder
(D4; session-keeper findings §2a).

### 3.2 `SystemClock` — the real impl

```go
type SystemClock struct{}
func (SystemClock) Now() time.Time                     { return time.Now() }
func (SystemClock) Since(t time.Time) time.Duration    { return time.Since(t) }
func (SystemClock) NewTicker(d time.Duration) Ticker   { return &systemTicker{t: time.NewTicker(d)} }
func (SystemClock) Sleep(ctx context.Context, d time.Duration) bool {
    t := time.NewTimer(d)
    defer t.Stop()
    select {
    case <-ctx.Done(): return false // ctx cancelled first — the injector.go sleepCtx shape (:197-206)
    case <-t.C:        return true   // full d elapsed
    }
}
type systemTicker struct{ t *time.Ticker }
func (s *systemTicker) C() <-chan time.Time { return s.t.C }
func (s *systemTicker) Stop()               { s.t.Stop() }
```

`Sleep`'s `bool` return + `ctx` are exactly `injector.go:197-206`'s `sleepCtx` (D4; keeper findings
§2a) — a bare `Sleep(d)` cannot honor the cancellation `InjectText` relies on at `injector.go:148,161`.

### 3.3 `FakeClock` — virtual time, manual advance

**Model: manual advance with a waiter registry.** Virtual `now` is a field; nothing moves it except
a test/harness call to `Advance`. Sleepers and tickers register deadlines; `Advance` walks the
timeline and fires everything whose deadline falls in the interval it steps across. This gives
*precise interleaving control* (needed to reproduce the keeper's timeout races deterministically)
and lets the harness collapse hours of wall-clock timeouts into arithmetic — that is how **507
keeper cycles replay in milliseconds** (§3.5).

```go
type FakeClock struct {
    mu      sync.Mutex
    now     time.Time
    sleepers []*sleeper       // pending Sleep() calls
    tickers  []*fakeTicker    // live tickers
}
func NewFakeClock(start time.Time) *FakeClock { return &FakeClock{now: start} }

func (c *FakeClock) Now() time.Time                  { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *FakeClock) Since(t time.Time) time.Duration { return c.Now().Sub(t) }
```

**`Sleep` under manual advance.** A sleeper registers deadline `now+d` and blocks until either
`Advance` reaches that deadline (return `true`) or `ctx` cancels (return `false`):

```go
type sleeper struct { deadline time.Time; done chan struct{} }

func (c *FakeClock) Sleep(ctx context.Context, d time.Duration) bool {
    c.mu.Lock()
    if d <= 0 { c.mu.Unlock(); return true }
    s := &sleeper{deadline: c.now.Add(d), done: make(chan struct{})}
    c.sleepers = append(c.sleepers, s)
    c.mu.Unlock()
    select {
    case <-ctx.Done(): return false
    case <-s.done:     return true
    }
}
```

**`Advance` — the test/harness driver** (not part of `ClockPort`; a `*FakeClock` method the harness
holds directly). It moves virtual time forward, firing tickers at each interval boundary and waking
sleepers whose deadline is reached, **in timeline order**:

```go
func (c *FakeClock) Advance(d time.Duration) {
    c.mu.Lock(); defer c.mu.Unlock()
    target := c.now.Add(d)
    // Fire tickers and wake sleepers at each event instant up to target, in order.
    for {
        next, has := c.nextEventBefore(target) // earliest ticker-boundary or sleeper deadline in (now, target]
        if !has { break }
        c.now = next
        c.fireTickersAt(next)   // for each ticker whose nextFire == next: send `next` on C(), advance nextFire += interval
        c.wakeSleepersAt(next)  // for each sleeper whose deadline <= next: close(done), drop from list
    }
    c.now = target
}
```

- `wakeSleepersAt` closes `done` for every sleeper whose deadline `<= next`; the blocked `Sleep`
  returns `true`.
- `BlockUntil(n int)` (optional convenience) spins until `len(c.sleepers)+pending tickers == n`, so
  a test can deterministically wait for the reactor goroutine to register its sleep before advancing
  — avoids the "advance-before-arm" race.

### 3.4 Ticker semantics — first-tick-**after**-interval (the parity concern)

This is load-bearing (session-keeper findings §5, parity risk #4): Go's real `time.Ticker`
delivers its **first** tick only after one full interval, so `pollForNonce` (`cycle.go:1515-1528`,
200ms cadence) never sees a nonce in the first 200ms, and `10s/200ms = 50 polls exactly` at the
`ClearSettle` window edge. The fake MUST reproduce this or replayed poll-quantization diverges from
the corpus.

```go
type fakeTicker struct {
    ch       chan time.Time
    interval time.Duration
    nextFire time.Time // set to createTime + interval → first tick is AFTER one interval
    stopped  bool
}
func (c *FakeClock) NewTicker(d time.Duration) Ticker {
    c.mu.Lock(); defer c.mu.Unlock()
    t := &fakeTicker{ch: make(chan time.Time, 1), interval: d, nextFire: c.now.Add(d)}
    c.tickers = append(c.tickers, t)
    return t
}
func (t *fakeTicker) C() <-chan time.Time { return t.ch }
func (t *fakeTicker) Stop()               { /* mark stopped; drop from clock.tickers under mu */ }
```

- **First-tick-after-interval:** `nextFire = createTime + interval`, never `createTime`. A ticker
  created at virtual `t0` with interval `p` fires at `t0+p`, `t0+2p`, … — byte-identical to
  `time.Ticker`.
- **Coalescing parity:** `ch` is buffered cap-1; `fireTickersAt` does a non-blocking send
  (`select { case t.ch <- next: default: }`). If a prior tick is unconsumed, the new one is dropped
  — this mirrors real `time.Ticker` (which coalesces when the receiver is slow). Deterministic
  replay normally advances exactly one interval at a time, so one tick fires and is consumed before
  the next `Advance`; the coalescing rule only matters if a test jumps multiple intervals in one
  `Advance`, and then it matches production.
- **`Stop`** removes the ticker from the clock and stops future fires (does not drain `ch`, like
  real `time.Ticker`).

### 3.5 Driving 507 keeper cycles in milliseconds

The keeper reactor (D11) dissolves the two blocking poll loops (`pollForNonce`,
`waitForNewSessionIDWithBackstop`) into **armed-timer events**: the reactor emits `ArmTimer{kind,d}`
actions; the shell converts each into a `FakeClock`-scheduled `TimerFired{kind}` event. So in
deterministic replay **`Step` never sleeps and never reads the clock** (D11: every timestamp in
state comes from an event's Clock-stamped `at`) — the only clock interaction is the shell scheduling
timer fires. The harness therefore replays a cycle by:

1. building the cycle's stimulus schedule (D13: trace-driven from `summary.json`),
2. calling `FakeClock.Advance(cycle_span)` in the arithmetic time it takes to walk the waiter list —
   there is **no real waiting**, a multi-minute cycle timeout collapses to a few `Advance` calls,
3. observing that armed timers fire at the right virtual instants (first-tick-after-interval intact),
   so timeout races (SR9) are exercised deterministically rather than by real elapsed time.

Because the injector's real settle/retry sleeps (`injector.go:148,161`) sit behind a **faked**
PanePort in L0–L2 (KeeperBridgeSink), `FakeClock.Sleep` is rarely exercised in the fast tiers; where
a test does drive it directly (injector unit tests), the manual-advance model applies and
`BlockUntil` sequences the wake. `SystemClock` covers the L3 live tier where real timing matters.

---

## 4. The L0–L3 taxonomy as a normative template (SB-R6)

The taxonomy is **test files + a Makefile convention, not exported code** (findings §1.6). Substrate
provides the *reusable code* a vertical imports and *mandates the shape* of the per-vertical files a
vertical copies. This section is the normative template the spec (SB-R6) states and a second vertical
(SK) follows.

### 4.1 Generic CODE (imported) vs TEMPLATE (copied) — findings §4.1

| Provided as | Artifacts |
|---|---|
| **Generic code (imported from `substrate`)** | `FakeEffector[A]`, `SyntheticSource[E]`, `Twin[E]` + `FaultConfig`/`FaultMode`, `Run`, `ClockPort`+`SystemClock`+`FakeClock`. A vertical writes **zero** test-double plumbing. |
| **Template (copied per vertical, shape mandated)** | the four tier files + drift canary + the Makefile target pair. Evidence it is template-not-code: every L0–L3 file is `package <v>test_test` with hardcoded corpus IDs (`l1:184-188`), a hand-written golden action slice (`l1:203-209`), and a bespoke bridge sink (`l2:36-150`) — none exportable without inventing abstraction the codebase style forbids (findings §4.1, R9). |

**R9 — do NOT extract a generic bridge sink.** The codex `HarmonikBridgeSink` (comms/queue/beads,
`l2:36-150`) and the keeper `KeeperBridgeSink` (pane/gauge/handoff/emitter) have different shapes;
a "generic bridge sink" would be invented abstraction with two dissimilar users. Sinks stay
**per-vertical, test-local**. The spec must say so.

### 4.2 The minimum-artifact list a vertical MUST supply (SB-R6, closes R8)

Conventions rot because nothing *compiles* the taxonomy (R8) — the only enforcement hooks are the
Makefile target-pair and spec review. So SB-R6 names a checkable minimum-artifact list per vertical;
a vertical is non-conformant if any is absent:

1. **Four tier files** in `internal/<v>test/`:
   - **L0 unit** (pure, zero-token): table-driven `Step` transitions via `SyntheticSource[Event]` +
     `FakeEffector[Action]`; codec golden-decode + malformed-line table (mirror `l0_wire:162-186`).
   - **L1 contract** (corpus, zero-token): whole-corpus decode with zero unknown types (mirror
     `l1:43-81`); **golden action-sequence replay** `Twin → reactor → FakeEffector →
     reflect.DeepEqual(want)` (mirror `l1:181-215`); ordering sweeps as pure postconditions.
   - **L2 integration** (corpus + faults, zero-token): corpus→Twin→reactor→**bridge sink** happy
     path with per-service assertions (mirror `l2:160-238`); at least one fault case per mode
     asserting a terminal signal (mirror `l2:242-268`).
   - **L3 live** (env-gated): `skipUnless<V>Live` env gate → `t.Skip`; real-subprocess/real-pane
     canary; `-run TestL3_` prefix (mirror `l3:30-48`, `:59-198`).
2. **Drift canary** (`canary_*_test.go`): corpus non-empty, valid JSON, parseable by the codec,
   zero unknown/raw frames, **≥N frames/cycles** (mirror `canary:34-90`).
3. **Recorded corpus + CAPTURE-LOG**: `testdata/<vertical>/corpus/*.jsonl` plus a `CAPTURE-LOG.md`
   ledger (mirror `testdata/codex-app-server/corpus/CAPTURE-LOG.md`, `Makefile:129-133`) — captures
   are deliberate, budget-capped, and ledgered.
4. **Makefile target pair**: `test-<v>-l012` (`go test -count=1` over the vertical's packages,
   `<V>_LIVE=0` default, deploy-gated) and `test-<v>-live` (`<V>_LIVE=1 … -run TestL3_`), mirroring
   `Makefile:112-113` / `:120-121`.
5. **Env-gate var**: one `<V>_LIVE` variable, `"1"` enables the live tier (codex uses `CODEX_LIVE`;
   keeper uses `KEEPER_LIVE`).

**Corpus-path idiom (copied):** `runtime.Caller(0)` + relative `../../testdata/<v>/corpus` for
in-repo path resolution (`l1:30-36`, `l0_wire:33-36`). Not code substrate can own (it's per-vertical
paths), so it's part of the template.

**Zero-token property:** SB-R6 requires L0–L2 to be ≥ the codex "~95% zero-token" property — all
three tiers run with the live env-gate off and touch no model/subprocess. Only L3 is gated.

---

## 5. Fault model semantics (SB-R5, D3) — stated generically

The four faults are pinned as vertical-neutral (D3). This section gives the generic definitions the
spec (SB-R5) states and the `Twin` implements. **Codex vocabulary (`Disconnected`/`Error`/`Seq`) is
FORBIDDEN in the spec text** (R1/R2); it appears only as the codex-instantiation consequence.

### 5.1 The four faults (generic definitions — D3)

`FaultConfig{Mode, EventN}`, `EventN` **1-based** (`twin.go:70-71`). Let event N be the Nth event the
codec emits (post-skip):

- **`FaultDropAfter`** — deliver events 1..N, then deliver the vertical's **connection-lost** event
  (`codec.DisconnectEvent()`) and end the stream. *Not* "inject Disconnected."
- **`FaultTruncate`** — replace event N with the vertical's **transport-error** event
  (`codec.ErrorEvent(...)`), then end the stream. *Not* "inject Error." Models a malformed/truncated
  corpus line.
- **`FaultStall`** — deliver events 1..N-1, then **block before event N until `ctx` cancels** (already
  generic; `twin.go:168-171`). Probes bounded-liveness (SR9): the consumer must time out, never hang.
- **`FaultDup`** — deliver event N **twice** (the same event value delivered twice — an idempotence
  probe). Same-`Seq` is the *codex-instantiation consequence* of re-sending the identical `Event`,
  **not** the substrate definition (R2).
- **`FaultNone`** — faithful replay, no injection.

**Universal invariant (SB-R5):** every fault mode MUST yield a **terminal signal, never silence**.
`DropAfter`/`Truncate` terminate with a synthetic terminal event; `Stall` terminates by ctx-timeout
in the consumer; `Dup` terminates normally after the doubled delivery. A vertical lacking a natural
"disconnect" concept (keeper) supplies its `restart_failed`-class terminal event as
`DisconnectEvent()` and its transport-lost event as `ErrorEvent()` (D3).

### 5.2 The `ReplayCodec` skip-vs-fatal contract (SB-R5 / D3; findings R3)

Pinned in D2. The normative split (D3 "also pinned"): `DecodeLine(line) (ev E, emit bool, err error)`

- `emit == false, err == nil` → **skip** this line (not reactor-relevant; e.g. the keeper corpus
  legitimately interleaves non-cycle bus lines, and codex skips non-`ServerNotification` frames).
- `err != nil` → **fatal** transport failure: the Twin emits `codec.ErrorEvent(err.Error())` and
  closes the stream (mirrors `twin.go:133-139`).
- `emit == true, err == nil` → deliver `ev`.

This split is normative (D3, R3): a parse failure MUST be fatal, never a silent skip — else the
keeper codec would swallow real corruption as skips. The spec states it so a vertical codec can't
conflate the two.

### 5.3 The 1MB buffer default (SB-R5 / D3; findings R4)

The generic `Twin`'s scanner MUST default its `bufio.Scanner` buffer to **1 MB**, not codex's 64KB
assumption (`twin.go:117-118` documents "max line ~1 KB"). The keeper bus JSONL has lines the L1
tests already read with 1MB buffers (`l1:53`, `canary:42`); the first oversized line under 64KB
truncates the replay **invisibly** — `scanner.Scan()` returns false, the source closes early, and it
looks like a short corpus (R4). Provide a `WithBufferSize(n int)` functional option on `NewTwin` for
verticals that need larger; default 1 MB.

### 5.4 Channel-shape lock-in — accept and state (SB-R5 / findings R6)

`Events(ctx) <-chan E` bakes in **channel-of-values delivery with close-on-exhaustion**. This is
fine for both current verticals and is the shape the codex stack already proves, but it forecloses
pull-based / error-returning sources (`Next() (E, error)`). The L3/live keeper source must deliver
tmux-poll results through a goroutine+channel adapter to fit. **Decision: accept the lock-in and
state it in the spec** — do not let a mid-design "improvement" to an iterator API silently rewrite
the seam the codex stack already validates (R6). The `Twin` internals: a goroutine + `defer close`,
buffer 16 (`twin.go:98-105`), ctx-aware send closure (`twin.go:108-115`).

---

## 6. Measurement contract surface owned by substrate (SB-R10)

The full measurement design is `measurement-design.md` (D13). Substrate owns only the **generic
mechanisms** that measurement instantiates; it does not own the keeper baseline numbers or the
old-vs-new differential harness. Substrate's contribution to SB-R10:

- **`Twin[E]` + `FaultConfig`** are the replay-regression and fault-injection engine. Measurement
  runs correctness as **replay over a recorded corpus + fault-injection pass-rate vs a frozen
  baseline**, never live A/B (Goal 6). Substrate provides the deterministic replay; the corpus and
  baseline are the vertical's (D13: keeper corpus at `testdata/keeper-cycles/baseline-2026-07-13/`).
- **The fault matrix standard**: every fault mode yields a terminal signal, never silence (§5.1) —
  this is the property measurement asserts as "fault matrix 100% terminal-never-silence" (D13
  acceptance oracle #2). Substrate guarantees the *mechanism*; measurement asserts the *rate*.
- **The N-clean-run standard**: substrate's determinism (FakeClock virtual time + `SyntheticSource`
  fixed order + `Twin` faithful replay) is what makes "N=10 consecutive green replay+fault runs"
  (D13 oracle #1) *repeatable* — a flaky substrate primitive would make the oracle meaningless.
- **The out-of-band check**: substrate's role is that the harness **does not grade itself** — the
  `FakeEffector.Actions()` log and the replayed event stream are the graded artifacts, read
  out-of-band by `jq`/`grep`/`stat` (D13 oracle #3). Substrate keeps the recorder (`FakeEffector`)
  separate from the asserter, which is the structural precondition for an out-of-band oracle.
- **File-order determinism (D9-fileorder):** where a corpus is assembled from multiple appenders
  (daemon `JSONLWriter` + N keeper `FileEmitter`s) with per-process EventID generators, **file order
  ≠ global EventID order**. Substrate's replay presents lines in file order; the *harness sorts by
  EventID after collection* (D9 decision), cheap at this scale. Substrate does not itself sort — it
  is a faithful line-reader — but the spec must state that a multi-appender corpus is EventID-sorted
  before it becomes a `Twin` `io.Reader`. This is the one place substrate's "faithful replay" meets
  the cross-process ordering caveat.

Everything else in SB-R10 (the 507-cycle baseline anchors 427/507=84.2%, 347 `clear_unconfirmed`,
the old-vs-new differential, the coverage floor) is measurement/SK-owned (D13), not substrate.

---

## 7. Migration / sequencing + honest risks

### 7.1 Order of operations (codex green at every step)

Land the extraction as a sequence where the tree compiles and codextest is green after each step:

1. **Create `internal/substrate`** with `seam.go`, `doubles.go`, `replay.go`, `clock.go`,
   `fakeclock.go`, `doc.go` + the package's own `substrate_test.go` (int-typed generic coverage).
   Add the leaf depguard rule (§1.3). Nothing imports it yet → tree still green.
2. **Re-instantiate codex** in one commit touching exactly three files (`reactor.go`, `fake.go`,
   `twin.go`, §2). Gate: `go test ./internal/codextest/... -count=1` green with **zero** `_test.go`
   diff (the R7 checklist). Add the recommended codex-side companion depguard rule here.
3. **(SK work, separate beads)** keeper instantiates the seam: `keeper.Event`/`Action`/`Step`, a
   `ReplayCodec[keeper.Event]`, `KeeperBridgeSink`, the corpus, the four tier files + canary +
   Makefile pair — all *consuming* substrate, never modifying it. ClockPort migration of the 34
   `cycle.go` sites + `restartnow.go`/`awaitack.go` `Now` seams happens here (D4; keeper findings
   §2b/§2d).

Steps 1–2 are the substrate deliverable; step 3 is SK's, listed to show substrate is complete and
frozen before the second vertical lands (proving genericity by *use*, not by edit).

### 7.2 Honest friction list (state these in the spec, don't hide them)

- **`Run` is a free function, not a method (R5).** Generic methods are illegal in Go, so the seam's
  canonical entry point is `substrate.Run[E,A]`; the codex `Reactor.Run` becomes a one-line wrapper
  (§2.1). The spec must name `substrate.Run` as the normative loop, with the method wrapper as
  instantiation detail — a spec that pins `Reactor.Run` as the seam contradicts the extraction.
- **Alias-based re-instantiation is load-bearing (R7).** The "codextest unchanged" property depends
  on `=` aliases + const re-exports + preserved wrapper signatures. A defined type instead of an
  alias mostly works structurally but breaks composite literals and `[]Action` `DeepEqual`
  comparisons — this is a **review-checklist item** (§2.4), not a design change, but it is the single
  most likely implementer misstep.
- **Const re-export is mildly ugly.** Go has no enum re-export sugar; codexdigitaltwin restates 5
  consts + 2 aliases (§2.3). Accepted cost — it is what lets `FaultConfig{Mode: FaultDropAfter}`
  keep compiling at call sites.
- **1MB buffer is a silent-failure trap if forgotten (R4).** The default must be raised from the
  inherited 64KB or the first big keeper line truncates the replay invisibly. Called out as a Twin
  invariant (§5.3), not left to the vertical.
- **Channel-shape lock-in (R6).** Values-over-channel, close-on-exhaustion — accepted and stated
  (§5.4); no iterator API rewrite.
- **Fault vocabulary leakage (R1/R2).** The spec text must use "connection-lost / transport-error /
  delivered-twice," never "Disconnected/Error/Seq." The codec method names (`DisconnectEvent`,
  `ErrorEvent`) are the neutral surface; codex-named events live only in the codex codec impl.
- **Taxonomy is convention and conventions rot (R8).** Nothing compiles the L0–L3 template. The only
  hooks are the Makefile target-pair and spec review; SB-R6's minimum-artifact list (§4.2) makes
  drift at least *checkable*, not *prevented*.

---

## Traceability

| This design section | Satisfies (02-components) | Pins honored |
|---|---|---|
| §1 package layout + depguard | SB-R1, SB-R13 | D1, D5 (name), findings §5 |
| §2 codex re-instantiation | SB-R7 (reference impl, stays green) | D1, R7 |
| §3 FakeClock/ClockPort | SB-R9 | D4 |
| §4 L0–L3 taxonomy template | SB-R6 | findings §4.1, R8/R9 |
| §5 fault model | SB-R3, SB-R4, SB-R5 | D2, D3, R1–R6 |
| §6 measurement surface | SB-R10 | D13, D9-fileorder |
| §7 migration/risks | SB-R7, SB-R8 | D1, R4/R5/R6/R7/R8 |

**Deferred, not in substrate's scope:** apptap physical relocation (kept separate, §1.2); the codex
companion depguard rule is *recommended* not blocking (§1.3); F-class keeper durability, the coverage
floor value, and the old-vs-new differential are SK/measurement/EV-owned (00-decisions open items).
