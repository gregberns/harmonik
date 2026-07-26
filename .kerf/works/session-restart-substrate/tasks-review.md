# Tasks-Pass Review (Pass 7) — session-restart-substrate

> Independent review of `07-tasks.md` against the Pass-5 spec drafts (RS / SK / EV amendment),
> the `05-changelog.md` landing checklist, and the Pass-4 design pins (`00-decisions.md`,
> `00b-review-resolutions.md`, the four `*-design.md`, `06-integration.md`). Reviewer role only;
> `07-tasks.md` NOT edited.

## Verdict: **Approved**

The breakdown is complete, correct, and executable. Every load-bearing spec change maps to at
least one task; the three named sanity checks (codex-stays-green, R6 synthesizer-freeze,
SR4-no-clear-before-model-done) are all explicitly captured; the dependency graph is a valid DAG
with correct ordering; acceptance criteria are verifiable out-of-band. The findings below are
non-blocking refinements to two under-enumerated deliverable lists — none leaves a load-bearing
requirement without an implementing task.

---

## Per-criterion assessment

### 1. Task completeness of shape (what / spec-refs / deliverables / acceptance / deps) — PASS
Every task carries all five fields. Spec citations are file+ID precise (e.g. T1→RS-001..023/INV;
T4→EV-048/047/D6; T8→SK-012/013/014/018). Acceptance is concrete and command-shaped throughout.
Dependencies are explicit per task and reconciled with the wave graph.

### 2. Spec-change coverage — PASS (see checklist). No load-bearing requirement is orphaned.
Two deliverable lists under-enumerate (T2 comment-sweep, T2 Valid()-prop tests) — see findings
1 and 3; both are in-scope-by-reference, not missing coverage.

### 3. No gold-plating — PASS
Every task traces to a spec requirement or a design pin. T0.2 = 00b R5 / 06 REQUIRED edits;
T11 differential = D13 transition scaffold; T3 codex re-instantiation = RS-021. The "optional
codex-side depguard companion rule" in T3 correctly mirrors OQ-RS-002's recommended-not-required
default. Nothing exceeds the spec surface.

### 4. Dependency graph is a valid DAG, ordering correct — PASS
Topological check: T0.1→T0.2; T1(∅); T2(T0.1)→T2b(T2); T3(T1); T4(T2,T2b); T5(T1)→T6→T7→T8(+T2);
T9(T7,T8,T1)→T10(T9,T4)→{T11,T12}→T13(T10,T12)→T14(all). No cycles.
- T8 correctly after T2 (payloads) + T7 (reactor states). ✓
- Codex re-instantiation (T3) correctly after the substrate package (T1). ✓
- EV→RS→SK order honored: T0.1 lands the three specs in that order in-task; the *code* consumers
  of EV-047/048/049 (the `internal/replay` harness) are in T4, which correctly depends on T2/T2b.
  T1 (substrate leaf) rightly needs nothing from T2 — substrate does not import core; only the
  harness does. Landing-order coupling is respected at both the spec and code level. ✓

### 5. Parallelization plan — PASS (one label imprecision, finding 4)
- [DISJOINT] T1 (internal/substrate), T3 (codexreactor/codexdigitaltwin — codex pkgs), T4
  (internal/replay). Genuinely file-disjoint across packages; T3/T4 both in W2 touch different
  package trees. ✓
- [SINGLE-WRITER core] T2→T2b correct (both write internal/core, serialized). ✓
- [SINGLE-WRITER internal/keeper] T5→T6→T7→T8 correct (all write internal/keeper). T8 additionally
  *imports* core payloads (dependency, not a write conflict) — fine. ✓
- W4 tasks write new packages disjoint from keeper/core. Caveat in finding 4.

### 6. Acceptance verifiable out-of-band — PASS
All acceptances resolve via `go build` / `go test` / `git diff --stat` / `make` / `jq` / `stat`;
none depends on the daemon pipeline. T14 is explicitly zero-daemon. T10 zero-token + KEEPER_LIVE
gate is checkable without a model. Matches census Acceptance-Oracle / measurement §6.

---

## Named sanity checks

- **(a) Codex-stays-green, zero test-file diff — CAPTURED.** T3 acceptance: "ZERO diff to any
  `_test.go` (`git diff --stat` shows no test-file changes)" + `make test-codex-l012` green;
  re-asserted in T14 (5) "git diff shows codextest untouched." Matches RS-021.
- **(b) Synthesizer table frozen against a green differential before scaffold deletion (00b R6) —
  CAPTURED.** T11 acceptance verbatim: "the synthesizer table frozen against this green
  differential before the scaffold is deleted (00b R6)"; re-asserted in T14 (6).
- **(c) SR4 no `InjectClear` before `ModelDone` — CAPTURED.** T8 acceptance: "a unit test asserts
  no `InjectClear` before a `ModelDone` (SR4)"; the fail-open timeout path (SR9) also asserted.

---

## Spec-coverage checklist

**RS (replay-substrate) — all covered**
- RS-001..005 seam/Run/typed/port-idiom/stdlib-leaf+depguard → T1 ✓
- RS-006/007 doubles → T1 ✓ | RS-008/009/010/011 codec+Twin/skip-fatal/1MB/channel → T1 ✓
- RS-012 four faults → T1 ✓ | RS-014 two-layer decode → spec T0.1 + enforced by T2b (strict-outer)
  & T1 DecodeLine (tolerant-inner) ✓
- RS-015 ClockPort/Ticker/FakeClock → T1 (clock.go/fakeclock.go) ✓
- RS-016 capture-tee/apptap → **correctly requires no code task**: apptap is 100% generic,
  zero-change, and stays a separate package (substrate-design §1.2); the contract lands as spec
  text (T0.1). Not a gap.
- RS-017 taxonomy → T10 (keeper realizes) ✓ | RS-018 min-artifact list → T10 ✓ | RS-019
  Makefile/env-gate → T10 ✓ | RS-020 replay-vs-baseline → T13 ✓
- RS-021 codex reference green → T3 ✓ | RS-022 keeper 2nd instantiation → T5–T10 ✓ | RS-023
  disambiguation → T1 doc.go + spec ✓
- RS-INV-001 compose / 002 DecodeLine determinism → T1 ✓ | INV-003 fault→terminal → T1 + T12 ✓ |
  INV-004 EventID-sort → T4 + T9 ✓

**SK (session-keeper) — all covered**
- SK-001..007 five ports + RespawnPort/PanePort-PL021d/GateSnapshot/journal/Emitter/ClockPort → T6
  (with T5 threading ClockPort) ✓
- SK-008 34-site ClockPort migration → T5 ✓
- SK-009 pure Step / 010 timers-as-events / 011 11-gate pure predicate + unconditional prelude → T7 ✓
- SK-012 four events at transitions w/ cycle_id → T8 ✓ | SK-013 emit-failure logging → T8 (D9) ✓
- SK-014 no-clear-before-model-done + idle/transcript/timeout sources → T8 ✓
- SK-015 bounded liveness → T7 (structural) + T8 (timeout) + T12 (matrix) ✓
- SK-016 bands/thresholds/gate-order preserved → T7 ✓ | SK-017 InCycle suppression → T7 ✓
- SK-018 old-corpus ModelDone synthesis → T9 ✓ | SK-019 baseline not regressed → T13 ✓
- SK-020 property tests over corpus+faults + out-of-band oracle → T10/T12/T13 ✓
- SK-INV-001..005 (SR3/SR4/SR6/SR7/SR9) → T4 checkers + T10 property tests + T7 structural + T12 ✓

**EV (event-model amendment) — covered; two deliverable-list under-enumerations (findings 1,3)**
- §8.16–§8.19 reconciliation + code-comment fixes → spec §8.16–8.19 in T0.1; keeper §8.13→§8.16
  code-comment fix + phantom-API fix in T2. **Finding 1: T2's deliverable list names only the
  keeper §8.13→§8.16 fix and the SetPayloadSchemaVersion fix; the alarm/remote/flywheel/stall/HITL
  comment corrections (§2.3 / checklist step 9) are in-scope-by-reference ("close the §8 numbering
  drift", cites EV-U5 §8.16–§8.19) but not enumerated.**
- §8.20 four rows + payload structs → T2 (keeperevents.go/eventreg/pertypecompat) + spec T0.1 ✓
- EV-046 required payload cycle_id join → T2 (required CycleID + Valid()) + T4 (composite join) ✓
- EV-047 ScanAfter → T4 + spec ✓ | EV-048 typed-decode adopted → T4 ✓ | EV-049 DecodePayloadStrict
  → T2b ✓ | EV-050 cohort/count carve-out + gjyks doc-comment → T2 ✓
- internal/replay harness → T4 ✓ | EV-036 secret-prefix scan → T2 acceptance ✓
- Roundtrip tests → T2 ✓. **Finding 3: EV conformance §6 also requires Valid()-prop tests (mirror
  reconciliationevents prop test); T2 lists "roundtrip tests" but not the Valid()-prop tests
  explicitly.**

**Measurement / corpus / differential / fault-matrix / oracle — all covered**
- Corpus + StimulusSynthesizer + keeperCodec → T9 (anchors 507/427/79/347/1 pinned) ✓
- L0–L3 tiers + canary + Makefile → T10 ✓ | old-vs-new differential + allowlist → T11 ✓
- Fault matrix (4×4×EventN, 100%) → T12 ✓ | metrics recompute + coverage floor + out-of-band
  oracle → T13 ✓ | full acceptance-oracle run (N=10) → T14 ✓

---

## Findings (non-blocking recommendations)

1. **T2 code-comment sweep is under-enumerated.** T2 names only the keeper `§8.13→§8.16` fix and
   the `SetPayloadSchemaVersion→RegisterEventTypeAtVersion` fix. EV-U5 §2.3 / amendment checklist
   step 9 also require correcting the alarm (`§8.14→§8.17`), remote (`§8.16→§8.18`),
   flywheel/stall (`→§8.19`), and HITL (`§8.15→§8.14`) code-comment citations across
   `eventtype.go` / `eventreg_hqwn59.go` / `pertypecompat_hqwn38.go`. These are "drift-closure"
   (lower severity than the load-bearing keeper fix, which T2 does cover), but the catalog stays
   partially self-inconsistent if skipped. Recommend widening T2's deliverable line to "apply the
   full §2.3 citation sweep," or adding an explicit acceptance ("no `§8.1x` code-comment cites a
   spec section whose family it does not match").

2. **RS-014 two-layer-decode statement has no dedicated verification.** It is a spec-text pattern
   (T0.1) enforced code-side by T2b (strict-outer) and T1 (tolerant-inner skip/fatal). Coverage is
   real; noting only that no single task asserts the *pattern* end-to-end. Acceptable as-is.

3. **T2 does not name the Valid()-prop tests.** EV conformance §6 asks for prop tests mirroring
   `reconciliationevents_hqwn59_prop_test.go` (Valid() true on well-formed; false on empty
   `cycle_id`; false on `new_session_up` with `NewSessionID==PrevSessionID`; false on `clear_sent`
   with `Attempt==0`). T4 exercises Valid() in the harness, but the core-package prop tests should
   be an explicit T2 deliverable. Recommend adding them to T2's deliverables/acceptance.

4. **W4 `[DISJOINT]` label is wave-scoped, not task-scoped.** T10–T13 all add files to
   `internal/keepertest`. They are file-disjoint (distinct `_test.go` files) and safe to merge, but
   any shared edits (the Makefile from T10, common test helpers) must serialize. Recommend noting
   that within W4 the disjointness guarantee is "distinct new `_test.go` files; Makefile/helpers are
   single-writer (owned by T10)," so a parallel-worktree executor does not collide on shared files.

5. **Optional/deferred items correctly handled (no action).** The `SessionKeeperAckTimeoutPayload`
   cycle_id backfill (EV-046 note) is marked optional/deferrable and rightly absent; the codex-side
   depguard companion (OQ-RS-002) is listed "optional" in T3 matching its recommended-not-required
   default; the coverage-floor *value* (D13) is measured-and-recorded in T13, not pre-committed.
