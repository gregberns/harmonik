# Captain & Crew — Tasks-Pass Review (independent gate)

> Fresh-eyes review of `07-tasks.md` (T1–T15) against `SPEC.md`,
> `06-integration.md`, and `05-specs/{c1,c2,c3,c4}-spec.md`. Reviewer was NOT the
> author of any prior pass.

## verdict: MINOR_GAPS

Decomposition is high quality: every component, every acceptance criterion, every
integration gap, and both mandatory test beads are accounted for, with a valid DAG
and sane bead hygiene. Two MINOR findings (one factual overstatement in the §D
parallel-independence claim; one ordering subtlety on T11) keep it from CLEAN.
Neither blocks readying the beads — both are doc-clarity fixes the implementer can
honor as-is from the per-task instructions. `ready_for_ready: true`.

---

## spec_coverage: COMPLETE

Every SPEC.md section maps to ≥1 task (cross-checked against §E coverage tables and
re-derived independently):

| SPEC.md item | Covered by | Verified |
|---|---|---|
| §C1 (event + status edge + at-most-once guard + boot seed) | T1, T2, T3, T4 | yes |
| §C2 (crew cmd + registry + launch-spec + handler + keeper-attach + stop) | T5, T6, T7, T8, T9, T10 | yes |
| §C3 (handoff schema + crew-launch skill) | T11, T12 | yes |
| §C4 (captain skill) | T13 | yes |
| §Integration build order (C1∥C2 → C3 → C4 → E2E) | §D DAG | yes |
| 06 §4 Gap 1 (`--assignee` mirror attribution) | T12 (write-on-every-adopt) + T13 (read `br show --assignee`) | yes |
| 06 §4 Gap 2 (no crew-offline event — OUT) | T13 (comms-who TTL heuristic, surface-only) | yes (correctly carried as OUT) |
| 06 §4 Gap 3 (operator identity — dual-surface) | T13 (status line + `--to operator` + `comms log` fallback) | yes |
| 06 §5 cross-cutting (exit 17 uniform; shared journal; additive) | T7/T8 (exit 17), T2/T3 (ScanAfter), T15 (exit-17 check) | yes |
| C1 §8 / event-model.md §8 additive row (locked-default) | T2 (implementer SHOULD-add, flag only if §8 frozen) | yes |
| Locked-default: nested-epic single-level roll-up | T2 (helper handles direct parent only) | yes |
| Locked-default: at-least-once-on-crash guard | T2/T3 (in-memory guard + boot seed; §F notes it) | yes |
| Locked-default: two `br show` per close | T2 (helper does both reads; §F notes it) | yes |
| Needs-operator: handoff-schema doc home | T11 (blocked-pending-operator on WHERE; §F) | yes |

No orphan SPEC section.

## ac_coverage: COMPLETE — no orphan AC

Independently walked all 22 component ACs to a task whose **acceptance-criteria
block** names it (not merely the §E matrix):

- **C1 AC-1** → T2 (impl) + T4 (scenario). **AC-2** → T2 (guard) + T4 (race sub-test).
  **AC-3** → T2 + T4. **AC-4** → T2. **AC-5** → T3 + T4. **AC-6** → T1. **AC-7** → T1
  (+ regression in T2/T9). All seven present in the named task AC blocks. ✓
- **C2 AC-1** → T7 + T10 (keeper-input live branch). **AC-2** → T7/T8 + T14.
  **AC-3** → T7 + T9. **AC-4** → T5 + T9. **AC-5** → T6 + T9. All five present. ✓
- **C3 AC-1** → T12 + T14. **AC-2** → T12 + T14. **AC-3** → T12 + T14. **AC-4** →
  T12 + T14. All four present. ✓
- **C4 AC-1** → T13 + T14. **AC-2** → T13 + T14. **AC-3** → T13 + T14. **AC-4** →
  T13 + T14 (transcript check). **AC-5** → T13 + T15. **AC-6** → T13 + T14. All six
  present. ✓

Every success criterion #1–#6 also traces (§E success-criteria table is correct).

## dag: ACYCLIC + valid topological order

Adjacency list re-read edge-by-edge. Every edge points from a
lower/prerequisite task to a strictly later one:

- C1: T1→T2; T2→T3; T2→T4; T3→T4. (chain + diamond at T4) — forward-only.
- C2: T5→T7; T6→T7; T5→T8; T7→T8; T5→T9; T6→T9; T7→T9; T7→T10; T8→T10. — forward-only.
- C3: T2→T11; T7→T11; T8→T11; T11→T12. — forward-only.
- C4: T11→T13; T12→T13; T2→T13; T3→T13; T7→T13; T8→T13. — forward-only.
- Tests: T1..T13→T14; T8→T15; T13→T15. — forward-only.

**No cycle.** The stated topological schedule
`T1,T5,T6 │ T2,T7 │ T3,T8,T9 │ T4,T10,T11 │ T12 │ T13 │ T14,T15` is a valid linear
extension (spot-checked: every predecessor of a task lands in an earlier band).

**C3 deps complete:** T11 depends on T2 (C1 event) + T7/T8 (C2 seed/CLI); T12 depends
on T11. ✓ **C4 dep completeness (the asked check):** T13 depends on **T11+T12 (C3
docs)** AND **T2/T3 (the C1 event it subscribes to)** AND **T7/T8 (the C2 surfaces it
calls)**. All three families present — matches the brief's required set. ✓

**T14 deps:** T1–T13 (full impl set incl. T4 + T10 transitively). ✓
**T15 deps:** T8 (CLI) + T13 (captain). ✓ — exactly as the brief specifies.

### C1 ∥ C2 independence check — ONE OVERSTATEMENT (finding F1)

§D claims "C1 ∥ C2 ... **no shared files**". That is **not strictly true**:

- **T3 (C1)** modifies `internal/daemon/daemon.go` (boot-scan seed in `daemon.Start`,
  thread seed/mutex into `newWorkLoopDeps`).
- **T7 (C2)** modifies `internal/daemon/daemon.go` (construct + register `CrewHandler`).
- **T2 (C1)** modifies `internal/daemon/workloop.go`; **T7** does not — so workloop is
  clean, but `daemon.go` is touched by both chains.

There is **no logical cycle** (no edge links the C1 and C2 chains), so the partial
order is still valid and they can be *developed* in parallel. But if dispatched as
two literally-concurrent daemon beads, the daemon-one-at-a-time merge will see a
**`daemon.go` merge contention** (different funcs/regions, so likely auto-mergeable,
but not the "no shared files" the doc promises). This is a doc-accuracy fix + a
dispatch-ordering note, not a structural defect. Recommend: land T1–T3 (or at least
T3's `daemon.go` edit) before T7's `daemon.go` edit, or accept the small merge risk
and note it. Both edits are additive and in distinct regions of `daemon.go`.

## sizing: appropriate — no split/merge required

- **T1 vs T2 split — JUSTIFIED.** T1 (`brShowEdge.Status` + `DependencyEdge.EndpointStatus`
  + parse, in `internal/brcli` + `internal/core`) is a self-contained, independently
  testable additive change that T2's completion-check *reads*. Landing it first
  de-risks the larger emit change and gives a clean root for the C1 chain. Keep split.
- **T7 size — ACCEPTABLE as one bead, with a caveat.** T7 bundles handler +
  `CrewHandler` interface + two socket op-cases + queue-ensure + keeper-attach +
  daemon.go wiring + stop path. That is large, but it is **one cohesive transaction**
  (`crew start` must do collision-check → mint+write registry → ensure queue → launch
  → keeper-attach atomically; splitting socket-wiring from the handler would leave a
  dead op-case, and splitting stop from start would orphan teardown). The launch-spec
  (T6) and registry (T5) are already carved out as the genuinely-separable units, so
  what remains in T7 is the irreducible handler core. The §6 manual-smoke is correctly
  pushed to T10. Keep as one bead; flag for the implementer that it is the largest
  bead in the slice and the §7-ordering (collision→mint→queue→launch→attach, rollback
  on fail) must be honored exactly.

## test_beads: BOTH PRESENT, deps correct

- **T14 (scenario, E2E captain+crew)** — present, `//go:build scenario`/manual-smoke,
  RUN-BY-HAND noted (daemon gate skips scenario; session/spawn must be live-smoked
  under the supervisor). Deps = **T1–T13** (full C1–C4 impl, incl. T4 + T10). ✓
- **T15 (exploratory, operator CLI surface)** — present, deps = **T8 (CLI) + T13
  (captain)**. ✓
- **Close-gate stated:** §C explicitly says "**Neither the plan nor any of its impl
  beads (T1–T13) may CLOSE until BOTH of these close**." Matches the kerf tasks-pass
  requirement. ✓

(Note: T15 carries `type=task` — beads has no "explore" type, so this is correct;
the "explore:" title prefix is the convention marker.)

## integration tasks: present + correctly ordered

- **C3 schema doc as the shared contract** → **T11**, ordered FIRST within C3
  (before T12 which references its field-contract). Correct per 06 §2 Step 2. ✓
- **Gap-1 attribution via `--assignee`** → split correctly across the writer (T12,
  crew mirrors on every adopt — called out as load-bearing) and the reader (T13,
  captain reads `br show --assignee`). Both downstream of T11. ✓
- **Gap-3 dual-surface operator convention** → T13. ✓
- All integration tasks (T11–T13) sit after their C1/C2 prereqs (T2/T7/T8) in the DAG.

## bead hygiene: clean

- **Label attachment, not epic-dep:** §A intro explicitly states every bead carries
  the `codename:captain` **label** and warns against `br dep` on the epic (cites
  `reference_beads_epic_dep_blocks_dispatch`). Every proposed bead line carries
  `label codename:captain`. ✓
- **Types sane:** T1–T10/T14/T15 = `task`; T11/T12/T13 (skill/doc artifacts) = `docs`.
  Correct (skills/schema docs are not code tasks). ✓
- **Priorities sane:** P1 on the spawn-critical-path + smokes (T7, T8, T10, T14);
  P2 on the rest. Reasonable; nothing P0 (no critical-path blocker), nothing absurd.
- **Handoff-schema doc-home open decision** → T11 carries it as **WHERE-not-WHETHER**,
  blocked-pending-operator, with a default path to proceed on if no answer, and an
  instruction to record the chosen path so C2/C4 reference the same one. Exactly the
  right framing. ✓
- **Proposals-only, no beads created** — §A intro states this; correct for a
  tasks-pass artifact awaiting the operator check-in. ✓

---

## findings

```json
[
  {
    "severity": "minor",
    "where": "07-tasks.md §D (DAG) + §A 'C1 ∥ C2 ... no shared files' claim",
    "issue": "Both T3 (C1) and T7 (C2) modify internal/daemon/daemon.go. The 'no shared files' independence claim is overstated; if dispatched as two literally-concurrent daemon beads they contend on daemon.go at merge. No logical cycle — the partial order is intact and parallel DEVELOPMENT is fine — but the merge is not conflict-free as promised.",
    "suggested_fix": "Soften the §D/§A wording to 'C1 ∥ C2 are logically independent; both add to distinct regions of internal/daemon/daemon.go, so order T3's boot-seed edit before (or merge-serialize with) T7's CrewHandler-wiring edit.' Optionally add a soft dispatch note: prefer landing the C1 chain's daemon.go touch first."
  },
  {
    "severity": "minor",
    "where": "07-tasks.md T11 dependencies (T2, T7, T8) + 06 §2 Step 2",
    "issue": "T11 (schema doc) is a pure CONTRACT artifact (field-contract table + path convention + example handoff). Its content does not actually depend on the C2 Go code existing — it depends only on the C1 event semantics being fixed (so the 'do-not-br-close' rule is correct). Gating T11 on T7+T8 (the full daemon handler + CLI) is stricter than necessary and needlessly serializes the doc behind all of C2. 06 §2 Step 2 justifies it as 'C3 requires C1+C2 landed' but that rationale applies to the crew-launch SKILL smoke (T12), not the byte-for-byte schema doc (T11).",
    "suggested_fix": "Consider relaxing T11's deps to just T2 (C1 event) so the schema doc — the contract C2 itself reads — can be authored in parallel with the C2 build, fixing the contract earlier. Keep T12's full C1+C2 deps (its smoke needs crew start). This is optional throughput optimization, not a correctness fix; if kept strict it is merely slower, not wrong."
  },
  {
    "severity": "nit",
    "where": "07-tasks.md T2 acceptance criteria / C1 §8 row",
    "issue": "T2 carries the optional specs/event-model.md §8 additive-row task as a SHOULD with 'flag operator only if §8 frozen'. This silently mixes a (possibly) normative specs/ edit into an impl bead. The push-autonomy memory permits specs/ edits without per-push confirmation, so this is fine, but the spec-text-check-in constraint is narrower — worth an explicit one-line note that the §8 row, if added, is the only specs/ touch in the whole slice.",
    "suggested_fix": "Add one line to T2: 'If the §8 row is added, it is the slice's only specs/ edit — additive taxonomy row, EV-029-safe; no behavior change.' Cosmetic."
  }
]
```

## ready_for_ready: true

The decomposition fully and correctly covers the spec, has a valid acyclic DAG with
complete dependencies, right-sized beads, both mandatory test beads with correct deps
and the close-gate, and clean label-based bead hygiene; the two MINOR findings are
doc-clarity/throughput refinements (a daemon.go shared-file caveat and an
over-tight T11 dependency) that do not block creating and dispatching the beads.
