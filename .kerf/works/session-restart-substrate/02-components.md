# 02 — Components (Affected Spec Areas)

> **Pass 2 of the spec jig (Decompose).** Autonomous mode (signoffs waived, 2026-07-13).
> Grounded in `01-problem-space.md`, the six dossiers in
> `plans/2026-07-13-code-revamp/research/`, and a four-agent survey of the existing spec
> tree (findings archived in the session scratchpad `decompose-survey/`). Requirements below
> state **what must be true after the change**, not how the spec text should read (that is
> Change Design, pass 4).

## Summary of the decomposition

The work touches **five** spec areas: **two NEW** normative specs, **one substantive UPDATE**,
and **two light cross-reference touches** — plus one **blocking data-reconciliation
dependency** inside the UPDATE.

| Spec | Kind | Prefix | Role |
|------|------|--------|------|
| `specs/substrate.md` | **NEW** | **SB** (free) | The generic record→replay seam contract + the ClockPort testability port + the L0–L3 taxonomy/measurement policy |
| `specs/session-keeper.md` | **NEW** | **SK** (free) | The session-restart vertical: 5 ports, the pure `Step`→reactor, the 4 durable interior events' ordering, and SR3/SR4/SR6/SR7/SR9 as normative testable invariants |
| `specs/event-model.md` | **UPDATE** | EV | Register the 4 durable keeper interior events, fix zero-`run_id` joinability, adopt the dead typed-decode path for replay invariant-checking, promote `ScanAfter` to a declared read surface |
| `specs/handler-contract.md` | **REFERENCE** | HC | Annotate the HC-035 in-process-fake carve-out to point at SB (the gap SB fills); confirm codex twin-parity unaffected |
| `specs/scenario-harness.md` | **REFERENCE** | SH | Cross-reference SB; confirm SB's L0–L2 zero-token tiers satisfy the SH-018 / SH-INV-001 no-test-branch discipline |
| `specs/operator-nfr.md` | **REFERENCE** | ON | Cross-reference ON-059 (the only pre-existing normative keeper cycle fragment) from SK |
| `specs/process-lifecycle.md` | **REFERENCE** | PL | SK's PanePort must cite PL-021d (tmux write discipline) and stay consistent with the PL-021b process-spawn seam without rebuilding the daemon side |

**A broad naming collision — "substrate" is overloaded 3–4 ways in the spec tree.** The new
record→replay package is `internal/substrate` (named so in Goal 1, PLAN §3a, and the handoff — a
settled *code* vocabulary, not reopened here). But "substrate" already carries **at least three
distinct normative meanings**:
1. **Process-spawn seam** — the tmux/subprocess boundary: `internal/handler.Substrate`, PL-021b
   "Substrate seam", HC-056, PI-012a, CI-004.
2. **Session substrate** — cognition-loop CL-015 / CL-024 "substrate teardown" (flywheel
   fresh-start session recycle).
3. **Transport/production substrate** — credential-isolation §2.2 "LLM transport substrate"
   (raw Messages API vs pi-agent-core) and pi-harness PI-069 "production substrate = paid".

Naming a bare `specs/substrate.md` into that namespace under-addresses the collision. This
decompose therefore records **two live options for Change Design, with the rename as the leading
one**: (a) **rename the spec to `specs/replay-substrate.md`, prefix RS** (both free) — cleanest,
decouples from all three prior senses; or (b) keep `specs/substrate.md` (SB) **only if** SB-R14
disambiguates from *all three* senses by name. The `internal/substrate` package name is unchanged
either way; this is a spec-file/prefix naming decision. **pi-harness (PI-012a/PI-069) and
credential-isolation (CI-004/§2.2) were considered and are NOT edited** — SB-R14 (or the RS
rename) owns disambiguation centrally; those specs keep their own "substrate" meanings intact.

---

## New Specs

### `specs/substrate.md` — prefix **SB** (the generic record→replay seam)

- **Scope:** The reusable, vertical-agnostic record→replay spine extracted from the green codex
  app-server stack (dossier 03 §8), plus the shared `ClockPort` determinism port and the L0–L3
  test-taxonomy/measurement policy. This is the "quality mechanism" of Goals 1, 2, 5(harness),
  and 6 stated as normative contract.
- **Requirements** (what must be true after the change):
  - **SB-R1** — Defines the `EventSource` / `Effector` seam and the `Run` driver-loop contract
    (`for ev := range src.Events(ctx) { for _, a := range Step(ev) { eff.Execute(ctx,a) } }`) as
    the normative composition boundary: any source × any effector composes; the reactor never
    knows whether the effect is real, a recorder, or a bridge sink.
  - **SB-R2** — States the genericity contract of the seam over event/action types. The seam MUST
    be typed, not stringly. (Go-generics vs `any`-boundary is the mechanism — **decision deferred
    to Change Design**, operator leans generics; SB states the *requirement* — "one method each,
    typed, no codex leakage" — not the mechanism.)
  - **SB-R3** — Defines the two test doubles as normative primitives: a recorder effector
    (`FakeEffector`-shaped, captures the action log for assertions) and a fixed-slice source
    (`SyntheticSource`-shaped). Both are the "swap any layer for a testable fake" guarantee.
  - **SB-R4** — Defines the replay-`Twin` contract: a captured corpus (`io.Reader` of append-only
    NDJSON) presented **as** an `EventSource`, parameterised by a pluggable `decode([]byte)→Frame`
    step and a `frame→Event` mapper — so the replay engine is generic and each vertical supplies
    only its codec + mapping table.
  - **SB-R5** — Defines the fault-injection model (`FaultConfig` with drop-after / stall /
    truncate / dup, indexed by event position) as a normative property of the Twin: transport
    faults meaningful for any event stream; each fault MUST yield a terminal signal, never silence.
  - **SB-R6** — Defines the **L0–L3 test taxonomy + measurement policy** as normative: L0 unit
    (pure), L1 contract (corpus decode/round-trip/no-unknown/golden-action replay), L2 integration
    (corpus→twin→reactor→bridge-sink, faked side effects), all zero-token; L3 the single
    env-gated live tier + a drift canary. The policy MUST require ≥ the codex "~95% zero-token"
    property and a Makefile wiring convention (env var + corpus-path resolution).
  - **SB-R7** — States the **codex stack as the first instantiation / reference implementation**:
    codex MUST be re-expressed on this seam and stay green (Goal 2), which is itself the proof the
    extraction is generic and not a codex-shaped copy.
  - **SB-R8** — States that the session-restart vertical is the **second instantiation** and the
    validation of the abstraction: if the seam cannot host it without codex-specific leakage, the
    extraction boundary is wrong (Goal 3 / PLAN §5.6).
  - **SB-R9** — Defines the **`ClockPort` contract** (`Now`, `Since`, `NewTicker`, `Sleep`) as the
    shared determinism port required by any vertical wanting deterministic replay of timeouts/poll
    races. SB is the home because the port is reused across verticals (keeper now, daemon later —
    a forward-compat note only; the daemon is out of scope). **Home decision (SB vs SK) deferred
    to Change Design** with SB recommended.
  - **SB-R10** — States the **measurement standard**: correctness is proven by replay-regression
    over a recorded corpus + fault-injection pass-rate vs a frozen baseline, **not** live A/B
    (Goal 6). Names the acceptance shape (replay-regression, fault terminal-signal, N clean runs +
    an out-of-band check).
  - **SB-R11** — States the **capture-tee** (`Tap`-shaped, protocol-agnostic stdio splice) as the
    generic capture primitive, with the explicit note that it owns no file format (persistence is a
    consumer-provided `io.Writer`).
  - **SB-R12** — States the **two-layer decode discipline** as a reusable pattern requirement
    (strict outer envelope, tolerant inner payload with unmodeled-field preserve-and-count,
    unknown → typed-raw not crash) — the pattern is normative; the concrete framing is the
    vertical's.
  - **SB-R13** — States the port idiom is **Go-native / consumer-owned narrow interfaces** (the
    `internal/queue` pattern — pure handlers returning `(newState, events, error)`); NO
    `Result`/`Option`/`Either` monad containers (Constraint 3).
  - **SB-R14** *(naming)* — Under option (b) (keep `specs/substrate.md`), this spec MUST
    explicitly disambiguate its "substrate" (the record→replay seam) from **all three** prior
    normative senses: the process-spawn seam (PL-021b / HC-056 / `internal/handler.Substrate` /
    PI-012a / CI-004), the cognition session substrate (CL-015 / CL-024 "substrate teardown"), and
    the transport/production substrate (credential-isolation §2.2 / pi-harness PI-069). Under
    option (a) (the recommended `specs/replay-substrate.md` / RS rename), SB-R14 is satisfied by
    the name itself and reduces to a single "not to be confused with…" pointer.
- **Cross-references:** event-model (EV-021 / §6.2 JSONL substrate, `ScanAfter`), scenario-harness
  (SH-018 / SH-INV-001 no-test-branch discipline; cadence mapping), handler-contract (HC-035
  in-process-fake carve-out — SB fills that gap).
- **Dependencies:** prefix reservation in `_registry.yaml` (SB) must land in the same commit
  (registry lint rule). Independent of SK and EV for authoring; the EV `ScanAfter`/typed-decode
  changes are what SB's replay-harness clauses *consume*, so SB's invariant-checking clauses
  (SB-R4/R6) are **soft-dependent** on the EV update landing (see Dependency Map).

### `specs/session-keeper.md` — prefix **SK** (the session-restart vertical)

- **Scope:** The normative contract for the keeper session-restart cycle rebuilt behind the SB
  seam — the ports, the pure state machine, the durable interior events, and the load-bearing
  ordering/liveness invariants. **This spec does not exist today** and there is **no normative home
  for SR3/SR4/SR6/SR7/SR9 or the 7-step cycle** anywhere in `specs/` (grep-verified); the only
  adjacent normative fragment is `operator-nfr.md §4.13 ON-059` (the restart-now gate ladder +
  thresholds). So this is genuinely NEW.
- **Requirements:**
  - **SK-R1** — Defines the **five keeper ports** as named consumer-owned interfaces, promoting
    the existing `CyclerConfig` function-fields (dossier 02 §6): `PanePort` (tmux inject/capture),
    `GaugePort` (token/session_id read), `HandoffPort` (handoff nonce poll + mtime freshness),
    `EmitterPort` (durable bus), and `ClockPort` (time — required by reference from SB-R9). The
    `EmitterPort` MUST remain structurally identical to the handler-contract `EventEmitter`
    (dossier 06) so no divergent bus port is introduced.
  - **SK-R2** — Requires the cycle to be expressed as a **pure `Step(event)→[]action` reactor**
    (the SB seam, second instantiation): the 11-gate ladder (step 1) becomes explicit states; the
    terminal outcomes (`cycle_complete` / `cycle_aborted` / `clear_unconfirmed`) become terminal
    transitions.
  - **SK-R3** — Requires **`ClockPort` to replace the 32 direct wall-clock sites** in `cycle.go`
    and the wall-clock sleeps in `injector.go` (dossier 02 §3), so the auto-cycle becomes
    fake-clock-drivable like `restartnow.go`/`awaitack.go` already are.
  - **SK-R4** — Defines the **four durable interior events** —
    `session_keeper_handoff_written`, `session_keeper_model_done`, `session_keeper_clear_sent`,
    `session_keeper_new_session_up` (naming per EV-U1a) — each carrying a real cycle id, and
    requires them durable on the bus (today they are journal-only + overwritten, dossier 02 §2).
    Their *registration/type-catalog* lives in EV (see the EV update); SK owns their **ordering
    semantics**.
  - **SK-R5** — Encodes **SR4** ("`/clear` NEVER before model-done") as a normative, testable
    ordering invariant — introducing the currently-missing model-done signal
    (`session_keeper_model_done`) as its precondition. *This is the headline: today SR4 has no interior
    implementation (dossier 02 §1); the spec makes it real.*
  - **SK-R6** — Encodes **SR9** (bounded liveness): every cycle reaches a terminal event within a
    bounded window or emits a `restart_failed`-class event; never silence (the resume-hang class).
  - **SK-R7** — Encodes **SR3** (handoff-write-done before `/clear`), **SR6** (brief only after
    new-session confirmed), **SR7** (no overlapping restarts) as normative testable statements.
  - **SK-R8** — Requires these invariants to be **verified by property tests over the recorded
    corpus + the four SB fault modes** (Goal 5), using the SB L0–L2 tiers.
  - **SK-R9** — States the **behavior-parity constraint**: the gate ladder, thresholds, and cycle
    semantics are preserved (re-expressed behind ports + state machine, not rewritten), except
    where a property test deliberately tightens the currently-missing SR4 invariant (Constraints
    2 & 4).
  - **SK-R10** — States the **measurement anchor**: the rebuilt reactor, replayed over the ~476
    recorded restart cycles, MUST characterize and not regress the frozen baseline
    (restart-completion 84% = 427/507; `clear_unconfirmed` 347) — per SB-R10.
  - **SK-R11** *(consistency)* — The `PanePort` inject contract MUST cite PL-021d (tmux
    load-buffer+paste-buffer write discipline) and stay consistent with the PL-021b process-spawn
    seam; the keeper-only `capture-pane` read (which PL-021b §5 forbids the daemon) is owned here.
- **Cross-references:** substrate (SB seam + ClockPort + taxonomy), event-model (the 4 event
  types + `run_id` joinability), operator-nfr (ON-059 restart-now ladder), process-lifecycle
  (PL-021d / PL-021b).
- **Dependencies:** prefix reservation (SK) in `_registry.yaml` (same commit). **Depends on SB**
  for the seam/ClockPort/taxonomy it instantiates, and **on the EV update** for the event
  type-catalog + `run_id` fix it references. Authorable in parallel with SB; must not finalize
  before SB's seam contract and EV's event registration are settled.

---

## Affected Existing Specs

### `specs/event-model.md` (prefix EV) — substantive UPDATE

- **Change summary:** Register the 4 durable keeper interior events, fix the zero-`run_id`
  joinability defect, adopt the dead typed-decode registry path for replay invariant-checking, and
  promote the offline-scan primitive to a declared read surface.
- **Requirements:**
  - **EV-U1** — The catalog MUST register the four new durable interior keeper event types as a
    numbered cohort that satisfies every §8.9 registration criterion, with durability and
    append-only/UUIDv7-ordering obligations stated. The cohort MUST occupy a **fresh, un-colliding
    section number** (next free is §8.16 — see EV-U5; do NOT reuse §8.13, which is Epic-completion).
  - **EV-U1a** *(naming convention — decision)* — The four events MUST follow the existing
    `session_keeper_*` catalog convention (all 18 registered keeper types use it), i.e.
    `session_keeper_handoff_written`, `session_keeper_model_done`, `session_keeper_clear_sent`,
    `session_keeper_new_session_up` — **not** the `keeper_*` shorthand used in the problem-space /
    PLAN prose. Recommendation: adopt `session_keeper_*` for catalog consistency; SK references
    them by that registered name. (Recorded as a decision, not silently renamed.)
  - **EV-U2** *(joinability fix)* — Keeper interior events MUST be joinable by a real correlation
    id. The recommended shape (dossier 04 §7; findings) is a REQUIRED payload-level `cycle_id`
    (precedent: the §8.6 `reconciliation_run_id`; the code already carries `CycleID`), plus a
    normative prohibition on a zero-valued envelope `run_id` for these events (§6.1 currently
    permits UUID-or-None with no zero prohibition).
  - **EV-U3** *(adopt typed-decode)* — The spec MUST classify a **replay invariant-checking
    harness** as a sanctioned observational reader bound to the typed-decode registry
    (`DecodePayload` / `ValidateEnvelopeSchemaVersion` / the `pertypecompat` N-1 table — today
    zero production readers, dossier 04 §6 / EV-032/033/028/029). This turns the dead read path
    into the replay-decode/assert layer instead of deleting it (operator leans adopt; **confirm in
    Change Design**).
  - **EV-U4** — `ScanAfter` / `Filter` (the append-only offline-replay primitive) MUST be promoted
    from its incidental EV-038 mention to a **declared read surface** that the SB replay harness
    consumes.
  - **EV-U5** *(blocking sub-dependency — a real §8-number collision, not a phantom)* — The
    survey found **18 existing `session_keeper_*` event types registered in code**
    (`internal/core/eventreg_hqwn59.go:478-489`) whose comments cite **§8.13** — but in
    `event-model.md` **§8.13 is already the Epic-completion cohort** (`epic_completed`); the code
    has *squatted* a number the spec assigns elsewhere, and these 18 types are absent from the EV
    catalog entirely. The reconciliation MUST (i) register the keeper cohort under a **fresh,
    un-colliding section** (next free is §8.16), and (ii) correct the code-comment §8.13 citation.
    This is a prerequisite *within* the EV change so the 4 new events land in a consistent catalog,
    not on top of the collision. (May be carved as a separate tracked bead if it balloons, but it
    blocks EV-U1.)
- **Dependencies:** the EV-U5 reconciliation is a **prerequisite** within this change (clean
  catalog before adding). SB-R4/R6 and SK-R4 **consume** EV-U1/U3/U4. EV can be authored in
  parallel but the event-registration portion should land before SK finalizes.

### `specs/handler-contract.md` (prefix HC) — light REFERENCE touch

- **Change summary:** Annotate the **HC-035 in-process-fake carve-out** (which explicitly
  disclaims governing in-process fakes) to point at SB as the spec that now governs that surface;
  confirm codex twin-parity (HC) is unaffected by the extraction.
- **Requirements:** After the change, the in-process-fake surface that HC-035 disclaims is
  governed by SB, and the two are mutually discoverable; HC's wire-protocol/twin-parity content is
  unchanged. No new HC requirements beyond that post-state.
- **Dependencies:** SB must exist to be referenced.

### `specs/scenario-harness.md` (prefix SH) — light REFERENCE touch

- **Change summary:** Cross-reference SB; confirm SB's zero-token L0–L2 tiers satisfy the SH-018 /
  SH-INV-001 **no-test-branch** discipline and map onto SH's cadence taxonomy
  (smoke/regression/nightly).
- **Requirements:** SB's taxonomy MUST be shown consistent with (not contradictory to) SH's
  cadence + no-test-branch rules; add a mutual cross-reference. SH is a subprocess-level E2E rig
  and does NOT house the record→replay seam (survey-confirmed) — so this is a pointer, not a
  merge.
- **Dependencies:** SB must exist.

### `specs/operator-nfr.md` (prefix ON) — light REFERENCE touch

- **Change summary:** Cross-reference **ON-059 §4.13** (the restart-now gate ladder + thresholds —
  the only pre-existing normative keeper cycle fragment) from SK, so the two do not drift.
- **Requirements:** After the change, SK and ON-059 are mutually discoverable (SK is the fuller
  keeper contract; ON-059 the restart-now gate ladder), with no behavioral change to ON.
- **Dependencies:** SK must exist.

### `specs/process-lifecycle.md` (prefix PL) — light REFERENCE touch (consistency only)

- **Change summary:** No PL text change required beyond ensuring SK's PanePort **cites** PL-021d
  (tmux write discipline) and is consistent with PL-021b (process-spawn seam). Recorded here so
  the consistency obligation is explicit and not missed.
- **Requirements:** SK's PanePort inject path MUST NOT diverge from PL-021d; the keeper capture
  read stays keeper-scoped (PL-021b §5 forbids the daemon that read). PL itself is not rewritten.
- **Dependencies:** none (constraint on SK, not an edit to PL).

---

## Dependency Map

Ordering of spec changes (→ = "must be settled before"):

```
1. _registry.yaml prefix reservations  (SB, SK)      ── land with each new spec's commit
        │
        ├──►  SB  (substrate seam + ClockPort + L0–L3/measurement)      [authorable now]
        │
        └──►  EV update
                 ├─ EV-U5  reconcile 18 undocumented session_keeper_* types   [prereq, internal to EV]
                 └─ EV-U1..U4  add 4 events, run_id fix, adopt typed-decode, declare ScanAfter
                          │
   SB (seam/clock/taxonomy) ──┐
   EV (events/run_id/decode) ─┴──►  SK  (ports, reactor, SR3/4/6/7/9)   [finalize last of the three]
                                         │
                                         └──►  HC / SH / ON / PL cross-reference touches
                                                (pointers; land after SB & SK exist)
```

- **Parallelizable:** SB and the EV update can be drafted concurrently; SK drafting can start in
  parallel but **must not finalize** until SB's seam/ClockPort contract and EV's event-registration
  are stable (it references both).
- **Blocking:** EV-U5 (drift reconciliation) is a prerequisite *within* the EV change. The
  registry-prefix reservations are hard prerequisites (lint) for SB and SK.
- **Two decisions deferred to Change Design (pass 4):** (a) seam genericity — Go generics vs
  `any` boundary (SB-R2; operator leans generics); (b) adopt vs delete the typed-decode path
  (EV-U3; operator leans adopt). Also to confirm in Change Design: ClockPort home (SB vs SK; SB
  recommended); and the **spec naming** — `specs/replay-substrate.md` (RS, leading/recommended) vs
  `specs/substrate.md` (SB) with a three-way SB-R14 disambiguation. The `internal/substrate`
  package name is unaffected by that choice.

---

## Goal → Area Traceability

Every goal in `01-problem-space.md` maps to at least one spec area; no spec area is listed without
a justifying goal.

| Goal (01-problem-space §Goals) | Satisfying spec area(s) |
|---|---|
| **1** — generic `internal/substrate` package exists (record→replay spine) | **SB** (SB-R1..R6, R11, R12) |
| **2** — codex re-instantiated on substrate, stays green | **SB** (SB-R7, reference-impl requirement) + existing `internal/codextest` (no new spec) |
| **3** — session-restart rebuilt behind the seam (ClockPort, ports, `Step` reactor) | **SK** (SK-R1, R2, R3) + **SB** (SB-R9 ClockPort, SB-R8 second-instantiation) |
| **4** — restart interior durable on the bus (4 events, real cycle id) | **EV** (EV-U1, U2) + **SK** (SK-R4 ordering) |
| **5** — load-bearing invariants as property tests (SR3/4/6/7/9) | **SK** (SK-R5, R6, R7, R8) + **SB** (SB-R6 taxonomy/harness that runs them) |
| **6** — measurement is replay-vs-baseline, not live A/B | **SB** (SB-R10 measurement standard) + **SK** (SK-R10 baseline anchor) |

Constraints coverage: Constraint 1 (no daemon) is an execution constraint, not a spec area.
Constraint 2/4 (keep regression net / behavior parity) → SK-R9. Constraint 3 (Go-native, no
monads) → SB-R13. Constraint 5 (append-only UUIDv7, joinable by real id) → EV-U2, EV-U4.
Constraint 6 (codex stays green) → SB-R7.
