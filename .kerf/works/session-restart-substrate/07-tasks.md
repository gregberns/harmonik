# 07 — Implementation Tasks

> **Pass 7 (Tasks).** Breaks the drafted specs (`05-spec-drafts/` — `replay-substrate.md`/RS,
> `session-keeper.md`/SK, `event-model-amendment.md`/EV-046..050) into consumable implementation
> tasks with deliverables, spec citations, acceptance criteria, and a dependency graph.
> Implementation-agnostic, but annotated for THIS work's execution model: **off-daemon Claude Code
> sub-agents; worktree-isolated where file-disjoint; single-writer sequential where a package is
> shared; human-style review; merge to a branch** (Constraint 1, no daemon). Autonomous mode.

## Execution-model annotations
- **[DISJOINT]** — touches only new/own package files; safe to run in a parallel worktree agent.
- **[SINGLE-WRITER pkg]** — shares a package with other tasks; those tasks serialize (one writer at
  a time) or run as one agent.
- Every task's acceptance is verifiable **out-of-band** (`go build`/`go test`/`jq`/`stat`), never
  via the daemon pipeline (census Acceptance Oracle; measurement-design §6).

---

## Dependency graph (waves)

```
W0 spec-landing (T0.1 registry+specs, T0.2 reference-touches)      [independent of code; land with code]
        │
W1 foundation ── T1 internal/substrate [DISJOINT] ───┐
             └── T2 event catalog (core) [SINGLE-WRITER core] ─┬── T2b DecodePayloadStrict (core)
        │                                                       │
W2  ── T3 codex re-instantiation [DISJOINT] (needs T1) ─────────┤
    └─ T4 internal/replay harness [DISJOINT] (needs T2,T2b) ─────┘
        │
W3 keeper rebuild [SINGLE-WRITER internal/keeper] (needs T1, T2):
     T5 ClockPort migration → T6 five ports → T7 Step reactor+InCycle → T8 four events+model-done
        │
W4 measurement (needs T7,T8,T1,T4):
     T9 corpus+synthesizer → T10 L0–L3 tiers → {T11 differential, T12 fault matrix} → T13 metrics/oracle
        │
W5  T14 acceptance-oracle run (needs all) — N=10, fault 100%, out-of-band, coverage floor, no-regress
```

Parallelizable at once: **T1 ∥ T2(+T2b)** (W1); then **T3 ∥ T4** (W2) while W3 can start T5 as soon
as T1 lands. W3 is internally sequential (shared `internal/keeper`). W4 fans out after T8.

---

## W0 — Spec landing

### T0.1 — Reserve prefixes + land the three spec changes
- **What:** put the drafted specs into `specs/`.
- **Spec sections:** 05-changelog landing checklist; EV amendment apply-checklist.
- **Deliverables:** `specs/_registry.yaml` gains `RS: {spec-id: replay-substrate, …}` and
  `SK: {spec-id: session-keeper, …}`; `specs/replay-substrate.md` + `specs/session-keeper.md`
  created from the drafts; `specs/event-model.md` amended (EV-U5 §8.16–§8.19 reconciliation FIRST,
  then §8.20 cohort + EV-046..050; version 0.6.4→0.7.0).
- **Acceptance:** registry lint passes (prefixes unique, present); the spec-template conformance
  checklist passes on both new specs; `grep '§8.20' specs/event-model.md` present; landing order
  EV → RS → SK (RS cites EV's ScanAfter/DecodePayloadStrict; SK depends-on both).
- **Depends on:** none. **[SINGLE-WRITER specs/]**

### T0.2 — Reciprocal reference-touch edits (R5-a/b/c)
- **What:** add the reciprocal pointers on the UNCHANGED target specs.
- **Spec sections:** 00b R5; 06-integration REQUIRED edits.
- **Deliverables:** `handler-contract.md` §9 points HC-035 → RS-001/006/007; `scenario-harness.md`
  §9 points SH-018/SH-INV-001 → RS-017; `operator-nfr.md` §4.13 ON-059 → SK-016.
- **Acceptance:** each pointer resolves to a real anchor; no new normative requirements added.
- **Depends on:** T0.1. **[SINGLE-WRITER specs/]**

---

## W1 — Foundation

### T1 — `internal/substrate` (the generic seam + ClockPort + replay engine)
- **What:** the generic record→replay package extracted from the codex stack.
- **Spec sections:** replay-substrate.md RS-001..023, RS-INV-001..004.
- **Deliverables (new package, stdlib-only leaf):** `seam.go` (`EventSource[E]`, `Effector[A]`,
  free func `Run[E,A]`); `doubles.go` (`FakeEffector[A]`, `SyntheticSource[E]`); `replay.go`
  (`ReplayCodec[E]`, `FaultMode`+consts, `FaultConfig`, `Twin[E]` with the four vertical-neutral
  faults + 1 MB scanner default); `clock.go` (`ClockPort`, `Ticker`, `SystemClock`); `fakeclock.go`
  (`FakeClock` manual-advance + waiter/ticker registries, first-tick-after-interval); `doc.go`
  (RS-023 3-way disambiguation); `.golangci.yml` depguard leaf rule (allow `$gostd`+self, deny
  codex*/keeper/daemon).
- **Acceptance:** `go build ./internal/substrate/...` green; unit tests: `FakeClock` advance +
  ticker firing; `Twin` over a tiny synthetic corpus, each fault mode → a terminal event never
  silence (RS-INV-003); `SyntheticSource`+`FakeEffector`+`Run` round-trip; depguard passes.
- **Depends on:** none. **[DISJOINT]**

### T2 — Event catalog: §8.16 reconciliation + §8.20 cohort
- **What:** register the 4 interior events + close the §8 numbering drift.
- **Spec sections:** EV-U5 (§8.16–§8.19), EV-U1/U1a (§8.20), EV-046, EV-050.
- **Deliverables (in `internal/core`):** `eventtype.go` (§8.20 EventType consts; fix §8.13→§8.16
  doc-comments); `keeperevents.go` (the 4 canonical payload structs from 00b R1+R2 verbatim +
  `Valid()` methods); `eventreg_hqwn59.go` (`registerKeeperInteriorEvents()`, 4 `mustRegister`
  schema v1; fix §-comment); `pertypecompat_hqwn38.go` (4 mandatory compat rows; fix §-comment; fix
  the `SetPayloadSchemaVersion`→`RegisterEventTypeAtVersion` doc-drift); roundtrip tests **plus `Valid()` property tests** for the 4 payloads; leave
  `allEventTypeCohort`/`wantCount` untouched (EV-050 carve-out) and amend the gjyks doc-comment.
  **The §-comment sweep is the FULL EV-U5 set** per the amendment apply-checklist — not only the
  keeper §8.13 fix but the alarm/remote/flywheel/HITL citation corrections the amendment enumerates.
- **Acceptance:** `go test ./internal/core/...` green incl. bidirectional pertypecompat + new
  roundtrip tests; EV-036 secret-prefix scan passes; `go vet` clean.
- **Depends on:** T0.1 recommended (code may precede spec merge). **[SINGLE-WRITER core]**

### T2b — `DecodePayloadStrict` additive registry variant
- **What:** strict-decode variant so the harness sees additive writer drift.
- **Spec sections:** EV-049.
- **Deliverables:** `internal/core/eventregistry.go` — `DecodePayloadStrict(e Event)
  (EventPayload,error)` (`DisallowUnknownFields`); drift-detection test.
- **Acceptance:** a payload with an extra field decodes via `DecodePayload`, errors via
  `DecodePayloadStrict`.
- **Depends on:** T2 (same package). **[SINGLE-WRITER core]**

---

## W2 — Consumers of the foundation

### T3 — Codex re-instantiation on the substrate seam
- **What:** move codex onto the generic seam via aliases; prove it stays green.
- **Spec sections:** RS-021, RS-003; substrate-design §2.
- **Deliverables:** `codexreactor/reactor.go` (2 aliases + 1-line `Run` wrapper); `codexreactor/fake.go`
  (alias re-exports); `codexdigitaltwin/twin.go` (a `codexCodec` implementing
  `substrate.ReplayCodec[codexreactor.Event]` fusing the 5 leak points, seq as codec state,
  Error/Disconnect synthetic events; wrapper `Twin`; `FaultConfig`/`FaultMode` const re-exports
  preserving `New`); optional codex-side depguard companion rule.
- **Acceptance (Goal-2 proof):** `go test ./internal/codextest/... ./internal/codexreactor/...
  ./internal/codexdigitaltwin/...` green with **ZERO diff to any `_test.go`** (`git diff --stat`
  shows no test-file changes); `make test-codex-l012` green.
- **Depends on:** T1. **[DISJOINT]**

### T4 — `internal/replay` invariant-checking harness
- **What:** the typed-decode replay checker for the SR invariants.
- **Spec sections:** EV-048, EV-047, D6; enables SK-R8.
- **Deliverables (new package):** `Checker`/`Violation`/`Report`/`Replay(path,since,strict,checkers)`;
  uses `ScanAfter` + `ValidateEnvelopeSchemaVersion` + `DispatchObservational`/`Synchronous` +
  `LookupPayloadCompatEntry` + `DecodePayloadStrict`; `GetCycleID`/`GetAgentName` mini-interfaces;
  SR3/SR4/SR6/SR7/SR9 `Checker` implementations keyed on `(agent_name, cycle_id)`; EventID-sort
  before checking; "registered-but-never-observed → report not fail".
- **Acceptance:** run `Replay` over a fixture log (clean cycle + SR4-violating cycle + unterminated
  cycle); the report flags exactly the injected violations + the unterminated cycle; unknown-type
  line skipped in observational mode, errors in strict mode.
- **Depends on:** T2, T2b. **[DISJOINT]**

---

## W3 — Keeper rebuild `[SINGLE-WRITER internal/keeper]` (T5→T6→T7→T8 serialize)

### T5 — ClockPort migration
- **What:** thread `substrate.ClockPort` through the cycle core.
- **Spec sections:** SK-008 (SK-R3); RS-015 consumer.
- **Deliverables:** replace the 34 `cycle.go` wall-clock sites, `injector.go` `sleepCtx`,
  `watcher.go` ticker; migrate `restartnow.go`/`awaitack.go` `cfg.Now` to `ClockPort`;
  `FileEmitter.TimestampWall` via the clock; production wires `SystemClock`.
- **Acceptance:** `go test ./internal/keeper/...` green; `TestInjectText_SettleConstants` green; a
  fake-clock unit test drives a cycle timeout deterministically.
- **Depends on:** T1.

### T6 — Five ports + RespawnPort
- **What:** promote the 22 `CyclerConfig` fn-fields to named ports.
- **Spec sections:** SK-001..007 (SK-R1); SK-002 (PL-021d).
- **Deliverables:** `PanePort`/`GaugePort`/`HandoffPort`/`EmitterPort`(=`keeper.Emitter`)/`ClockPort`/
  `RespawnPort`; the 7 gate-predicate reads → per-tick `GateSnapshot`; production adapters wire the
  real fns.
- **Acceptance:** `go test ./internal/keeper/...` green (the ~55-file suite + conformance);
  no behavior change.
- **Depends on:** T5.

### T7 — The pure `Step` reactor + shell + `InCycle` suppression
- **What:** the pure state machine + imperative shell reproducing the freeze.
- **Spec sections:** SK-009/010/011 (SK-R2), SK-016/017 (SK-R9).
- **Deliverables:** pure `Step(state,Event)→(state,[]Action)` (`Idle→AwaitingHandoff→AwaitModelDone→
  Clearing→Briefing→{Complete|Aborted}`, full table from session-keeper-design §3d) with
  timers-as-events; the 11-gate ladder as a pure predicate evaluated only in `Idle` with
  unconditional prelude side effects; the shell driving ports+ClockPort + converting timers; the
  `InCycle` suppression parking non-cycle processing off-`Idle`.
- **Acceptance:** `go test ./internal/keeper/...` green incl. reactive-harness/scenario suites;
  `thresholds_test.go` green (bands unchanged).
- **Depends on:** T5, T6.

### T8 — The four durable events + model-done signal
- **What:** emit the interior events + implement SR4's model-done.
- **Spec sections:** SK-012/013 (SK-R4), SK-014 (SK-R5/SR4), SK-018.
- **Deliverables:** emit the 4 `session_keeper_*` events at the named transitions with `cycle_id`
  (payloads from T2); the model-done signal (`.idle`-mtime ≥ t_nonce primary, `recentTranscriptTurn`
  backstop, `model_done_timeout`=60s fail-open → `degraded:true`); emit-failure logging (D9); SR4
  enforced (no `InjectClear` action before `ModelDone`).
- **Acceptance:** `go test ./internal/keeper/...` green; a unit test asserts no `InjectClear` before
  a `ModelDone` (SR4); the timeout path emits `model_done{degraded:true}` and proceeds (SR9).
- **Depends on:** T2, T7.

---

## W4 — Measurement `[DISJOINT new: internal/keepertwin, internal/keepertest, testdata/, scripts/]`

### T9 — Keeper replay corpus + StimulusSynthesizer
- **What:** build the input-event corpus from the frozen baseline.
- **Spec sections:** SK-R8/R10; measurement-design §1, §2.
- **Deliverables:** `scripts/extract-keeper-corpus.py` → 507 `(agent_name,cycle_id)` `.jsonl` +
  `.summary.json` + `manifest.json` (pinned 507/427/79/347/1, EventID-sorted) under
  `testdata/keeper-cycles/baseline-2026-07-13/`; the `StimulusSynthesizer` (summary → input-event
  schedule; build-time synthesis; old-corpus `ModelDone` after `NonceObserved`); `internal/keepertwin`
  `keeperCodec` (`ReplayCodec[keeper.Event]` deserializing synthesized input lines).
- **Acceptance:** extractor reproduces the anchors (`manifest.json` matches); the synthesizer
  decision table is a single reviewed table; a Twin round-trip smoke test.
- **Depends on:** T7, T8, T1.

### T10 — Keeper L0–L3 tiers + Makefile
- **What:** the test taxonomy for the keeper vertical.
- **Spec sections:** SK-020 (SK-R8), RS-017/018/019; measurement-design §3.
- **Deliverables:** `internal/keepertest` L0 (pure Step tables + property tests), L1 (507-cycle corpus
  → Twin → reactor → golden `summary.json`, decoded via `internal/replay`), L2 (Twin → reactor →
  fake-ports sink), L3 (`KEEPER_LIVE=1` one-cycle tmux smoke) + drift canary; `Makefile`
  `test-keeper-l012` / `test-keeper-live`.
- **Acceptance:** `make test-keeper-l012` green + zero-token; L3 skipped without `KEEPER_LIVE=1`;
  minimum-artifact list (RS-018) present.
- **Depends on:** T9, T4.

### T11 — Old-vs-new differential harness (transition scaffold)
- **What:** run OLD Cycler and NEW reactor over identical synthesized schedules.
- **Spec sections:** SK-R9; measurement-design §4; D13.
- **Deliverables:** per-cycle diff (terminal type/reason, clear_unconfirmed, interior order,
  terminated-within-bound) with the permitted-divergence allowlist (SR4 tightening + 4 interior
  events + the 1 unterminated cycle as a required FIX).
- **Acceptance:** differential green under the allowlist; the 1 unterminated cycle FIXED not matched;
  **the synthesizer table frozen against this green differential before the scaffold is deleted**
  (00b R6).
- **Depends on:** T10.

### T12 — Fault matrix (SR9)
- **What:** the 4-fault × 4-strata × EventN matrix.
- **Spec sections:** SK-015/SK-INV-005 (SK-R6); measurement-design §5.
- **Deliverables:** each cell asserts a terminal signal within a virtual-time deadline + wall-clock
  backstop, never silence.
- **Acceptance:** fault-matrix pass rate **100%**.
- **Depends on:** T10.

### T13 — Metrics recompute + out-of-band oracle + coverage floor
- **What:** the no-regress measurement + the off-daemon oracle.
- **Spec sections:** SK-019 (SK-R10); measurement-design §6.
- **Deliverables:** the 9 metric recompute commands as a checkable script (jq/grep + stat/file);
  a measured coverage floor on the new Step/reactor files (recorded); the N=10 runner.
- **Acceptance:** metrics match the frozen anchors (84.2% restart-completion; degraded 81.3% must
  not RISE — target DOWN); coverage floor measured & recorded; oracle zero-daemon.
- **Depends on:** T10, T12.

---

## W5 — Acceptance

### T14 — Run the full acceptance oracle + no-regress confirmation
- **What:** the census Acceptance Oracle for this vertical.
- **Spec sections:** measurement-design §5.
- **Deliverables:** evidence bundle — (1) N=10 consecutive green `go test` over keeper+keepertwin+
  keepertest; (2) fault matrix 100%; (3) out-of-band jq/stat checks; (4) coverage floor met;
  (5) codex L0–L2 still green (T3); (6) differential green before scaffold deletion.
- **Acceptance:** all six met and recorded; baseline characterized, not regressed; `git diff` shows
  codextest untouched.
- **Depends on:** all.

---

## Notes for the executor
- **Parallel worktrees:** T1, T3, T4, and the W4 new-package tasks are `[DISJOINT]` — safe in
  isolated worktree agents. T2/T2b share `internal/core`; W3 (T5–T8) shares `internal/keeper` —
  run each as a single-writer sequence (one agent, or strict serialization) to avoid merge conflicts.
- **Review gate:** each task's diff gets an independent review before merge (Constraint 1 /
  orchestrator-rules review gate) — no daemon `queue submit`, no pipeline self-close.
- **Spec-vs-code:** the spec is normative; if implementation reveals a spec defect, fix the spec
  (kerf) — do not silently diverge.
- **Deferred (NOT in this phase):** F-class keeper durability; relaxing `InCycle`; daemon ClockPort;
  the enforcement-linter levers (Track C, separate direct work).
