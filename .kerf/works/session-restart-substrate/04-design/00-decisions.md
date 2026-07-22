# 04-Design / 00 — Cross-cutting decisions (the pinned contracts)

> **Pass 4 (Change Design).** Top-model synthesis of the four Research findings
> (`03-research/{substrate,session-keeper,events,measurement}/findings.md`) against the
> Decompose requirements (`02-components.md`). This doc SETTLES every cross-component decision
> and pins the exact shared contracts; the four per-component design docs elaborate within these
> pins and MUST NOT contradict them. Autonomous mode (signoffs waived, 2026-07-13).
>
> Decisions marked **[OPERATOR-LEAN CONFIRMED]** were pre-leaned by the operator and are
> confirmed by code-grounded evidence here; the rest are admiral judgment calls the research
> forced into the open. None reverses a locked decision or is destructive, so per the waiver all
> proceed without a gate.

---

## D1 — Genericization mechanism: **Go generics** `[OPERATOR-LEAN CONFIRMED]`

**Decision:** Extract the seam as Go generics (`go 1.25`, full generics — go.mod:3), NOT an
`any`-typed boundary.

**Pinned surface** (substrate package):

```go
package substrate

type EventSource[E any] interface{ Events(ctx context.Context) <-chan E }
type Effector[A any]    interface{ Execute(ctx context.Context, a A) error }

// Run is a FREE FUNCTION, not a method — Go forbids generic methods, and the
// vertical's Step must not force Reactor itself to be generic (Step carries
// vertical semantics, not substrate semantics).
func Run[E, A any](ctx context.Context, src EventSource[E], step func(E) []A, eff Effector[A]) error

type FakeEffector[A any] struct{ /* mu; actions []A */ }
func (f *FakeEffector[A]) Execute(ctx context.Context, a A) error
func (f *FakeEffector[A]) Actions() []A
func (f *FakeEffector[A]) Reset()

type SyntheticSource[E any] struct{ /* events []E */ }
func NewSyntheticSource[E any](events []E) *SyntheticSource[E]
func (s *SyntheticSource[E]) Events(ctx context.Context) <-chan E
```

**Why (evidence, substrate findings §2):** (1) channel invariance — `<-chan codexreactor.Event`
does NOT satisfy `<-chan any`, so option (b) forces boxed sources or per-wiring pump goroutines
and breaks every `reflect.DeepEqual([]Action, ...)` golden (codextest would NOT stay green);
(2) generics make SB-R2's "typed, no leakage" a compile-time property; (3) codex re-instantiates
via **type aliases** so `internal/codextest` compiles with **zero changes** — the cheapest proof
of Goal 2. The one real cost is `Run` moving from method to free function + a one-line codex
wrapper.

**Codex re-instantiation is alias-based (load-bearing, substrate findings §2.1 / R7):**
```go
// codexreactor
type EventSource = substrate.EventSource[Event]     // ALIAS (=), not defined type
type Effector    = substrate.Effector[Action]
func (r *Reactor) Run(ctx, src EventSource, eff Effector) error { return substrate.Run(ctx, src, r.Step, eff) }
// fake.go shrinks to: type FakeEffector = substrate.FakeEffector[Action]; type SyntheticSource = substrate.SyntheticSource[Event]
// codexdigitaltwin: FaultConfig/FaultMode const re-exports + a codexCodec + a wrapper Twin
```
`Step` stays codex-typed and unchanged; I1/I2 never enter substrate. Implementers MUST use
aliases (not defined types) or composite-literal / DeepEqual sites break — a review-checklist item.

---

## D2 — The replay engine takes a fused **`ReplayCodec[E]`**, not a `decode`+`map` pair

**Decision:** The generic `Twin[E]` is parameterized by ONE stateful interface, not the raw
two-function sketch from the brief.

**Pinned surface:**
```go
type FaultMode int
const ( FaultNone FaultMode = iota; FaultDropAfter; FaultStall; FaultTruncate; FaultDup )
type FaultConfig struct { Mode FaultMode; EventN int } // EventN 1-based

// ReplayCodec is everything a vertical supplies to replay its corpus. It fuses the
// four codex-specific replay steps (decode, error-policy, filter, map) into DecodeLine
// and supplies the two synthetic-event constructors the fault injector needs.
// Implementations MAY be stateful (own their seq/sequence counter).
type ReplayCodec[E any] interface {
    // DecodeLine decodes one corpus line.
    //   emit=false, err=nil  → skip this line (not reactor-relevant).
    //   err!=nil             → FATAL transport failure: twin emits ErrorEvent(err) and closes.
    DecodeLine(line []byte) (ev E, emit bool, err error)
    ErrorEvent(msg string) E   // vertical's transport-error terminal event (decode err, FaultTruncate)
    DisconnectEvent() E        // vertical's connection-lost event (FaultDropAfter)
}

type Twin[E any] struct{ /* corpus io.Reader; fault FaultConfig; codec ReplayCodec[E]; bufCap int */ }
func NewTwin[E any](corpus io.Reader, fault FaultConfig, codec ReplayCodec[E]) *Twin[E]
func (t *Twin[E]) Events(ctx context.Context) <-chan E
```

**Why (substrate findings §1.5, §2.3):** the codex replay loop leaks codex at FIVE points
(twin.go:132/135-139/144/148/164-177); a plain `decode`+`map` covers only two, forces a spurious
second type parameter `F`, and still misses the two synthetic-event constructors. One stateful
codec covers all five with one type parameter. **Seq disappears from the substrate surface**
entirely (it becomes codec-internal state) — this is what keeps codex's dedup/seq vocabulary out
of the generic seam (R2).

---

## D3 — Fault semantics stated **generically**, not in codex vocabulary

**Decision:** The spec (SB-R5) defines the four faults in vertical-neutral terms:
- `FaultDropAfter` = "deliver event N, then deliver the vertical's **connection-lost** event
  (`codec.DisconnectEvent()`) and end the stream" — NOT "inject Disconnected".
- `FaultTruncate` = "replace event N with the vertical's **transport-error** event
  (`codec.ErrorEvent`)" — NOT "inject Error".
- `FaultDup` = "deliver the same event value **twice**" (an idempotence probe) — same-Seq is the
  codex-instantiation consequence, not the substrate definition.
- `FaultStall` = "block after event N until ctx cancels" (already generic).

**Why (substrate findings R1/R2):** copying twin.go's codex-named definitions verbatim would make
the "generic" substrate silently assume every vertical has Disconnected/Error/Seq — the exact
leakage the extraction exists to prevent. A vertical lacking a natural disconnect concept (keeper)
supplies its `restart_failed`-class terminal event as `DisconnectEvent()`.

**Also pinned (substrate findings R3, R4):** `DecodeLine`'s skip-vs-fatal split is normative (the
keeper corpus legitimately skips non-cycle lines; a parse failure must be fatal, never a silent
skip). `Twin` defaults its scanner buffer to **1 MB** (not codex's 64 KB assumption) with an
option, else the first oversized keeper bus line truncates the replay invisibly.

---

## D4 — ClockPort lives in **substrate**; keeper requires it by reference

**Decision:** `ClockPort` (+ `Ticker`) is defined in `internal/substrate` (SB-R9 home = substrate),
reused by keeper now and available to the daemon later (forward-compat note only; daemon is out of
scope).

**Pinned surface** (session-keeper findings §2a):
```go
type ClockPort interface {
    Now() time.Time
    Since(t time.Time) time.Duration
    NewTicker(d time.Duration) Ticker
    // Sleep waits d or until ctx cancels; reports whether the full d elapsed.
    // (injector.go's sleepCtx shape — a bare Sleep(d) can't honor cancellation,
    // which InjectText relies on.)
    Sleep(ctx context.Context, d time.Duration) bool
}
type Ticker interface { C() <-chan time.Time; Stop() }
```
Real impl delegates to `time`; the fake advances virtual time. `Since` is kept (sugar over `Now`)
because 13 cycle.go sites read as interval gates and a fake wants one advance-point. The two
existing `cfg.Now` seams (`restartnow.go:69`, `awaitack.go:96`) migrate to `Clock ClockPort`.
`FileEmitter`'s `TimestampWall` stamp (watcher.go:68) also routes through ClockPort for
deterministic replay envelopes.

---

## D5 — Spec-file naming: **`specs/replay-substrate.md`, prefix RS**

**Decision:** Name the new seam spec `specs/replay-substrate.md` with requirement-prefix **RS**
(both free), NOT `specs/substrate.md`. The Go package stays `internal/substrate` (Goal 1 vocabulary,
unaffected by the spec-file name).

**Why (Decompose review + surveys):** "substrate" already carries THREE normative meanings —
process-spawn seam (PL-021b/HC-056/`handler.Substrate`/PI-012a/CI-004), cognition session substrate
(CL-015/CL-024), transport/production substrate (CI §2.2/PI-069). A bare `substrate.md` into that
namespace is a collision magnet; the rename decouples cleanly and reduces SB-R14 to a single
"not to be confused with…" pointer. This was the Decompose review's leading option; the research
gives no reason to prefer the disambiguation-clause fallback. (Requirement IDs stay `SB-*` in this
design for continuity with 02-components; they become `RS-*` at spec-draft — noted in the changelog.)

---

## D6 — Adopt the typed-decode registry path `[OPERATOR-LEAN CONFIRMED]`

**Decision:** ADOPT `DecodePayload` / `ValidateEnvelopeSchemaVersion` / `DispatchObservational` /
`DispatchSynchronous` / `pertypecompat` as the replay harness's decode+assert layer. Do NOT delete.

**Why (events findings §3):** all four symbols have **zero production readers** (grep-proven) but
are **alive in 9 test files**, the registry is exhaustively populated (169 `mustRegister`), and two
guard tests keep it synchronized with the taxonomy. A replay harness is *exactly* the EV-033
"observational consumer" the API was specced for. Adopting gives typed decode + unknown-type skip
policy + schema-version assertion + N-1 window reporting for the cost of a thin `Replay` loop; the
harness becomes the registry's **first production reader** (closing the loop future payload edits
need). One additive gap: `DecodePayload` is non-strict (`json.Unmarshal`, no `DisallowUnknownFields`)
so it can't see additive writer drift — **in scope**: add a `DecodePayloadStrict` variant on the
registry (small, additive).

**Pinned harness home:** a NEW leaf package `internal/replay` (not in keeper, not in core) with:
```go
type Checker interface { Types() []core.EventType; Check(ev core.Event, p core.EventPayload, s *CycleState) []Violation }
type Violation struct { EventID core.EventID; CycleID string; Rule, Detail string }
type Report struct { Events, Skipped, Malformed int; SchemaMismatches []core.EventID; Violations []Violation }
func Replay(path string, since core.EventID, strict bool, checkers []Checker) Report  // consumes eventbus.ScanAfter
```

---

## D7 — Joinability fix: **required payload-level `cycle_id`**, NOT envelope `run_id` `[OPERATOR-LEAN CONFIRMED]`

**Decision:** Fix the zero-run_id defect with a REQUIRED, `Valid()`-checked payload field
`cycle_id` (json `cycle_id`, no omitempty) on the cycle-interior events. Leave the 14
`core.RunID{}` envelope arguments unchanged (absent envelope run_id is correct for
non-run-scoped events).

**Why (events findings §2):** (1) envelope `RunID` is a `uuid.UUID` newtype with `Valid()`
non-Nil enforcement; keeper cycle ids are non-UUID strings `cyc-<ts>-<seq>` — using the envelope
forces re-minting ids + migrating journal/nonce/78K historical events; (2) EM-013 `run_id` is the
*workflow-run* join key with live production folds (daemon reconcile, `eventbus.Filter`) — injecting
keeper cycles pollutes that keyspace; (3) exact precedent: reconciliation's
`ReconciliationRunID` (payload-level, `Valid()`-checked). The harness joins on `payload.cycle_id`
(also honoring major-issue-fanout's "never grep by run_id" rule). Optionally backfill `cycle_id`
onto `ack_timeout` (additive).

**Composite-key note (measurement findings §1):** `cycle_id` alone is not globally unique
(`newCycleIDGen` resets per process → 476 distinct), so the replay/measurement join key is the
composite **`(agent_name, cycle_id)` = exactly 507 cycles**. The new events all carry both fields.

---

## D8 — Event catalog: new cohort at **§8.20**, and reconcile the §8.13–8.19 drift

**Decision:** Register the four new `session_keeper_*` interior events as a new cohort. In the spec,
allocate real §8 sections to close the code/spec numbering drift: the keeper cohort is renumbered
out of the §8.13 collision and the new interior events land at **§8.20** (next free; §8.13–8.19 are
either spec-occupied by other cohorts or code-invented-but-unspecced).

**Concretely (events findings §1, §5):**
- Code squats §8.13 (keeper) / §8.14 (alarm) / §8.15 (hitl) which the spec assigns to
  epic_completed / hitl-decisions / bead-ledger; code-invented §8.16–8.19 (remote/flywheel/etc.)
  were never written into `specs/event-model.md` (headings end at §8.15).
- The EV amendment will: (a) add real spec sections for the keeper family and the code-invented
  cohorts through §8.19, then (b) allocate **§8.20** to the four interior events, and (c) correct
  the `eventreg_hqwn59.go` §8.13 doc-comment citation. This is EV-U5's blocking reconciliation —
  it MUST precede EV-U1 (the additive registration) so the four events land in a consistent catalog.
- Per event: `EventType` const + payload struct (`keeperevents.go`, with required `CycleID` +
  `Valid()`) + `mustRegister` (schema v1) + a mandatory `PayloadCompatEntry`
  (`CurrentVersion:1, PreviousVersion:0, CompatWindowHolds:true, AdditiveOnly:true`) +
  roundtrip test.

**Cohort-guard decision (events findings §4):** follow the existing keeper precedent — keeper
events stay EXCLUDED from `allEventTypeCohort` / the EV-027 count guard (all 18 existing keeper
types already are). Record this explicitly in the spec so `eventtype_coverage_gjyks_test.go`'s
stated "every constant must be in the cohort" contract is amended to name the keeper carve-out
(today that contract is silently false). The four new events are NOT EV-026-internal (they cross
the process boundary keeper→jsonl→harness), so they MUST be registered — which is also what makes
D6's typed decode work.

---

## D9 — Durability of the four new events

**Decision:** The four interior events are emitted through the existing keeper `Emitter` /
`FileEmitter` path, and the spec classifies them **O-class (observational), not F-class** for this
phase — with two required hardenings:
1. Emit failures MUST NOT be silently swallowed for these four (today every keeper emit is `_ =`);
   at minimum log-on-failure, because "durable event that silently fails to write" is a spec lie
   (events findings §5.1).
2. The spec explicitly states file-order authority for cross-process replay determinism (daemon
   `JSONLWriter` + N keeper `FileEmitter`s append to one file; per-process EventID generators mean
   file order ≠ global EventID order — the harness sorts by EventID after collection, or the spec
   declares "file order authoritative"; **decision: harness sorts by EventID**, cheap at this scale).

**Why not F-class/fsync now:** F-class would mean routing keeper emits through the daemon or adding
fsync to `FileEmitter` — larger blast radius, and the restart cycle's durability need is satisfied
by the journal's crash-recovery role (which is retained, D10). Revisit F-class if a later phase
routes keeper through the daemon. Stated as an explicit spec row, not left implicit.

---

## D10 — Ports: 5 named ports; journal + respawn placement

**Decision (session-keeper findings §1):** Promote the 22 `CyclerConfig` function-fields to five
consumer-owned narrow ports (the `internal/queue` idiom). Pinned:
- **PanePort** — `Inject / SendEscape / SetEnv / Capture / OperatorAttached` (Inject follows
  PL-021d; Capture is keeper-only, PL-021b §5 forbids the daemon it).
- **GaugePort** — the gate-input read surface + the one managed-session write-back. **Decision on
  width:** keep GaugePort as `ReadGauge` + `SetManagedSession` + `ClearPrecompactTrigger`, and fold
  the seven predicate reads (CrispIdle/HoldingDispatch/Sleeping/Held/RecentTurn/IsManaged) into a
  per-tick **`GateSnapshot`** the shell samples and puts on the `GaugeTick` event — so the pure
  `Step` never touches a port (cleaner functional core; session-keeper findings §1b alternative).
- **HandoffPort** — handoff file (path/read/mtime/truncate) + the cycle **journal** (write/read).
- **EmitterPort** — keep `keeper.Emitter` verbatim (already a strict subset of
  `handlercontract.EventEmitter`; zero adaptation — SK-R1 satisfied by subsetting).
- **ClockPort** — from substrate (D4).
- **`ForceRestart`** (kill+respawn) is a process-lifecycle effect, not a pane write — it stays a
  one-method `RespawnPort` (or config callback), NOT bloating PanePort.
- The **journal** is retained: its observability role is superseded by the 4 events, but its
  crash-recovery role (`RecoverFromCrash`) is not. Phase vocabulary stays byte-identical
  (`opened/handoff_injected/confirmed/cleared/resumed/complete/aborted`) — parity.

---

## D11 — The `Step` reactor: timers as events; reproduce-the-freeze first

**Decision (session-keeper findings §3, §5):**
- The cycle becomes a pure `Step(state, Event) → (state, []Action)` machine mirroring
  `codexreactor`, with ONE structural addition codex lacks: **timers as events**. The reactor emits
  `ArmTimer{kind,d}` / `CancelTimer{kind}` actions and consumes `TimerFired{kind}` events; the shell
  owns ClockPort and converts between them. This dissolves the two blocking poll loops
  (`pollForNonce`, `waitForNewSessionIDWithBackstop`) and the backstop deadline into explicit,
  replayable event interleavings — and is what makes timeout races deterministically testable.
- States: `Idle → AwaitingHandoff → AwaitModelDone(NEW) → Clearing → Briefing → {Complete|Aborted}`
  (full transition table in `session-keeper-design.md`). The 11-gate ladder stays a pure predicate
  evaluated per `GaugeTick` **only in `Idle`** (which is also SR7's structural guarantee).
- **BIGGEST PARITY RISK — decision: reproduce-the-freeze first.** Today `MaybeRun` runs
  synchronously inside the watcher tick, so during a cycle (up to ~7.5 min) NO warns/precompact/
  heartbeat fire. An unfrozen reactor would change observable event streams. The rebuild adds an
  explicit **`InCycle` suppression** in the shell (park non-cycle processing while off-`Idle`) to
  reproduce the freeze exactly; relaxing it is a *later, separately-measured* change, not part of
  this proof. This keeps the baseline comparison apples-to-apples.
- Gate-prelude side effects (re-arm observation, same-SID escape hatch, boot-grace tracking, Gate-5d
  hold write) run UNCONDITIONALLY before gating today — the Step design preserves that (a "clean"
  short-circuit would change observable state). All anti-loop hysteresis fields become reactor
  `State`; every timestamp in state comes from an event's Clock-stamped `at`, never a clock call
  inside `Step`.

---

## D12 — The model-done signal (SR4 — no code today)

**Decision (session-keeper findings §4):** Introduce `session_keeper_model_done` with a real
durable source and a fail-open bound:
- **Primary source:** the Stop-hook `.idle` marker transition. After `handoff_written` at
  `t_nonce`, the first `mtime(.idle) ≥ t_nonce` is `ModelDone` (the model reached an await-input
  boundary AFTER the turn that wrote the handoff). Strict compare against the nonce-observation
  instant — no `crispIdleTolerance` fudge (that 10s tolerance discounts passive `.ctx` repaints,
  irrelevant here).
- **Backstop source:** `recentTranscriptTurn(sid,"assistant")` with turn-ts ≥ `t_nonce` (heavier;
  for agents whose Stop hook isn't wired).
- **Liveness bound (SR9):** a new `model_done_timeout` (default ~60s, well under
  `ClearConfirmBackstop`=150s) after which the reactor proceeds to `Clearing` anyway, emitting
  `model_done{degraded:true}`. This preserves today's clear-immediately behavior as the degraded
  mode — the SR4 tightening is **fail-open**, so a lost Stop-hook write can never wedge the cycle.
- **Old-corpus replay:** synthesizes `ModelDone` immediately after `handoff_written` when replaying
  pre-rebuild recordings (keeping old-corpus action goldens identical); only NEW recordings assert
  the real ordering. This is the Constraint-4 / SK-R9 parity carve-out.

---

## D13 — Measurement: trace-driven Twin, old-vs-new differential, out-of-band oracle

**Decision (measurement findings):**
- A keeper replay corpus **must be built** (none exists). Extract 507 per-cycle streams from the
  frozen baseline into `testdata/keeper-cycles/baseline-2026-07-13/` (one `.jsonl` + a `summary.json`
  golden per cycle), keyed by composite `(agent_name, cycle_id)`.
- The keeper `Twin` is **trace-driven**: the baseline records keeper *outputs*, not inputs, so each
  cycle's `summary.json` outcome determines the stimulus schedule the Twin presents on the ports
  (nonce-never-appears ⇒ abort; delayed-SID-flip ⇒ clear_unconfirmed; prompt ⇒ clean). This
  generalizes the existing `cycle_reactive_harness_test.go` `reactiveSession` fake; virtual time
  (ClockPort) replays 507 cycles in ms.
- **Old-vs-new differential** (transition scaffold): run the old `Cycler` and the new `Step` reactor
  over the SAME 507 stimulus schedules; diff (terminal type, reason, clear_unconfirmed presence,
  interior order, terminated-within-bound). Permitted-divergence allowlist = only the SR4 tightening
  + the 4 new interior events. Catches hang/false-close/ordering. Deleted when old `runCycle` is
  deleted; the L1 golden-vs-baseline corpus test is the permanent net.
- **The frozen baseline anchors (must-not-regress):** restart-completion **427/507 = 84.2%**;
  degraded-completion (clear_unconfirmed/complete) **347/427 = 81.3%** (target: DOWN, via SR6);
  unterminated **1 → must be 0** (SR9); aborts all explicit-reasoned (79/79 `handoff_timeout`);
  terminal exclusivity 0 overlaps. The single most damning number the rebuild should improve is the
  81.3% degraded-completion rate.
- **Acceptance oracle (off-daemon, census PLAN):** (1) N=10 consecutive green replay+fault runs;
  (2) fault matrix 100% terminal-never-silence; (3) out-of-band check = `jq`/`grep` over the log +
  direct filesystem/stat reads (the keeper does not grade itself — the log file does); (4) a
  *measured & stated* coverage floor on the new Step/reactor files.

---

## Decision → requirement traceability

| Decision | Satisfies (02-components) | Component design doc |
|---|---|---|
| D1 generics | SB-R1, SB-R2, SB-R13 | substrate |
| D2 ReplayCodec | SB-R4 | substrate |
| D3 generic faults | SB-R5 | substrate |
| D4 ClockPort in substrate | SB-R9, SK-R3 | substrate + session-keeper |
| D5 RS spec name | SB-R14 (+ Decompose review) | substrate |
| D6 adopt typed-decode | EV-U3, SB-R6(harness) | events |
| D7 payload cycle_id | EV-U2 | events |
| D8 §8.20 + drift reconcile | EV-U1, EV-U1a, EV-U5 | events |
| D9 durability O-class | EV-U1 (durability), Constraint 5 | events |
| D10 5 ports | SK-R1 | session-keeper |
| D11 Step reactor + freeze | SK-R2, SK-R7, SK-R9 | session-keeper |
| D12 model-done | SK-R4, SK-R5 (SR4) | session-keeper |
| D13 measurement | SB-R10, SK-R8, SK-R10 | measurement |

Constraints held: no-daemon (execution), regression-net + parity (D10/D11/D13), Go-native no-monads
(D1/D6), append-only UUIDv7 joinable (D7/D9), codex stays green (D1 aliases).

## Open items explicitly deferred (NOT blocking this design)
- F-class durability for keeper events (revisit if a later phase routes keeper through the daemon) — D9.
- Relaxing the `InCycle` freeze (a later, separately-measured change) — D11.
- The coverage-floor *value* (measured and stated at implementation time) — D13.
- depguard companion rule on the codex vertical side (cheap, consistent; substrate findings §5) —
  fold into the substrate design as a recommendation.
