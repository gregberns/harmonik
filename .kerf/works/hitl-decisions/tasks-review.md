# hitl-decisions — Tasks Pass: Independent Review

**Reviewer role:** independent (did not author 07-tasks.md or any upstream artifact).
**Under review:** `07-tasks.md` (task decomposition K1–K6 + test-bead wiring).
**Cross-checked against:** `SPEC.md` (§1–§9, S1–S8, N1–N9), `06-integration.md`, `05-specs/hitl-decisions-spec.md`, and **live beads state** (`br show`).
**Date:** 2026-06-13.

---

## TOP-LINE VERDICT: **APPROVE**

The task list fully covers SPEC.md §1–§9 and acceptance criteria S1–S8; the dependency graph is a valid DAG with no cycles; the integration/cross-cutting concerns (N1 fsync in K1, N8 arm-then-check K2↔K4, N3 first-writer-wins across K2/K4/K5) are all assigned; the test beads (hk-rz4, hk-1vl) exist and are wired with the correct dependencies; and the reviewer-mandated S5 (restart) and S7 (reap/re-wait) scenario coverage is folded into K3 and K5 respectively. **The wired live-bead dependency graph matches 07-tasks.md §E byte-for-byte, and NO bead depends on the open epic hk-rom.** Findings below are all P3 (non-blocking polish).

---

## 1. COVERAGE TABLE

### 1.1 SPEC.md §1–§9 → covering task(s)

| Section | Topic | Covered by | Verified |
|---------|-------|-----------|----------|
| §1 | Event schemas (3 `decision_*`) | **K1** | ✅ type constants + 3 payloads w/ `Valid()` + registration + N1 fsync |
| §2 | CLI surface `harmonik decisions` | **K2** (raise/wait/withdraw) + **K4** (list/show/answer) | ✅ verb→op map split correctly; `wait` is client-side (K2) |
| §3 | Open-set projection | **K3** | ✅ pure fold; shared source of truth read by K2/K4/K5/K6 |
| §4 | Blocked-wait contract (N8) | **K2** (wait impl) + **K6** (keeper-alive via heartbeat) | ✅ |
| §5 | Lifecycle, orphan reaping, keeper seam | **K5** (emit) + **K4** (flag, read-pure) + **K6** (exempt live agent) | ✅ split read-visibility from emission correctly |
| §6 | Normative conditions N1–N9 | **K1/K2/K4/K5/K6** + all emit paths | ✅ per-N mapping in §D.3 is complete and correct |
| §7 | Files & changes (anchor table) | **K1–K6** (one task per row) | ✅ K7 row = deferred |
| §8 | Acceptance criteria S1–S8 | hk-rz4, hk-1vl, K3-scenario, K5-scenario | ✅ see §1.2 |
| §9 | Integration seams & risks | **K1** (R1/N1), **K2** (R2/blocked-wait), K7-deferred | ✅ §9 policy gate SIGNED — no open decision |

**No uncovered section.** Every §1–§9 maps to ≥1 task.

### 1.2 Acceptance criteria S1–S8 → covering test/task

| AC | Statement | Covered by | Verified |
|----|-----------|-----------|----------|
| **S1** | raise emits `decision_needed` + agent blocks cleanly | **hk-rz4** | ✅ gate bead |
| **S2** | `decisions list` shows all open decisions from ≥2 agents | **hk-1vl** | ✅ gate bead |
| **S3** | `answer` → originating agent wakes with `chosen_option` | **hk-rz4** | ✅ gate bead |
| **S4** | replay resolve → no double-apply / no second wake (N2) | **hk-rz4** | ✅ gate bead |
| **S5** | open set identical after restart; resolved/withdrawn drop out | **K3** folded `//go:build scenario` test | ✅ REQUIRED AC in K3 |
| **S6** | `decisions list` renders with no aggregator process | **hk-1vl** + K3 purity AC | ✅ |
| **S7** | (a) kill agent → K5 withdraws/leaves queue; (b) restart → re-wait | **K5** folded `//go:build scenario` test | ✅ REQUIRED AC in K5 |
| **S8** | answering same/bogus `decision_id` is a no-op (N3) | **hk-rz4** | ✅ gate bead |

**No uncovered criterion.** S5 and S7 — the two gaps the prior independent reviewer flagged in the gate beads — are closed by REQUIRED scenario-test deliverables folded into K3 (S5) and K5 (S7). This matches the SPEC.md §8.1 / 06-integration.md §5.2 recommendation exactly (fold-into-impl-task, not standalone beads).

---

## 2. DAG-VALIDITY CHECK

**Result: VALID DAG — no cycles, build order matches data dependencies.**

Plan-stated edges (07-tasks.md §C / §E):

```
K1 ──► K3 ──► {K2, K4, K5, K6}        (each reader deps {K1, K3})
hk-rz4 deps {K1, K2, K3, K4}
hk-1vl deps {K3, K4}
```

- **K3 → K1:** ✅ correct — the fold pattern-matches on K1 type constants and reads K1 payload fields; cannot exist before K1.
- **K2 → {K1, K3}:** ✅ correct — emit-ops need K1 constructors; the N8 arm-then-check re-projection + restart re-derive call into K3.
- **K4 → {K1, K3}:** ✅ correct — `answer` emits a K1 `decision_resolved`; `list`/`show` render K3; `answer` validates openness against K3.
- **K5 → {K1, K3}:** ✅ correct — emits a K1 `decision_withdrawn`; iterates the K3 open set.
- **K6 → {K1, K3}:** ✅ correct — the "is this agent legitimately blocked?" check is a K3 projection lookup.
- **hk-rz4 → {K1, K2, K3, K4}:** ✅ correct — end-to-end raise→block→answer→wake needs the events (K1), raise/wait CLI (K2), the projection the wait re-checks (K3), the answer CLI (K4).
- **hk-1vl → {K3, K4}:** ✅ correct — the list/answer CLI (K4) renders the projection (K3).

**Acyclicity:** every edge points strictly downward in build order; no back-edges; K7 is detached (deferred). `br dep cycles` reports **"✓ No dependency cycles detected."** Longest path = K1→K3→K4→hk-rz4 (4 hops). Build order `K1 → K3 → {K2,K4,K5,K6}` matches the spec's strict data dependency.

---

## 3. TASK SIZING

All six impl tasks are appropriately sized — each is one coherent unit reviewable in a single pass:

- **K1** (event contract) — 3 constants + 3 payloads + registration + fsync entries. Tightly scoped to `internal/core`. ✅
- **K3** (projection + S5 scenario) — one pure function + one scenario test. The scenario is a deliverable of the same component, not unrelated work. ✅
- **K2 / K4** (CLI surfaces) — split along the natural agent/operator seam, same file `decisions.go`, same upstream pair. Sized to one verb-group each. ✅
- **K5** (reaper + S7 scenario) — keeper-tick emitter + scenario test of the same component. ✅
- **K6** (keeper seam) — single exemption gate. Smallest task, but it is a distinct correctness concern (the live-agent-protection complement of K5) and the §4/Risk-R2 contract makes it load-bearing, so it is correctly NOT folded into K5. ✅

No task bundles unrelated work; none is trivially small enough to merge. **No sizing finding.**

---

## 4. INTEGRATION / CROSS-CUTTING TASKS

All three named cross-cutting contracts are explicitly assigned and correctly ordered:

- **N1 fsync (K1→all):** assigned to **K1** as a load-bearing deliverable (add all 3 type names to `fsyncBoundaryEventTypes`, `busimpl.go:115`) with a dedicated REQUIRED acceptance criterion (unit test asserts each of the 3 types is reported F-class — guards Risk R1). ✅ Lands *with* the types, not bolted on later.
- **N8 arm-then-check (K2↔K4):** assigned as a joint contract — K2 owns the arm-then-check ordering (arm subscribe FIRST → re-project K3 → return-if-terminal → else block); K4's `answer` may emit at any moment, making K2's ordering the only defense. Both 07-tasks.md §A-K2/K4 and §D call it out as a cross-component invariant; hk-rz4 exercises the race. ✅
- **N3 first-writer-wins (K2/K4/K5):** assigned to all three — K2 (apply first terminal once), K4 (`answer` no-op on unknown/terminal), K5 (`withdraw(orphaned)` no-op on already-resolved — the answer-vs-reap race). §D.3 maps it correctly. ✅

Ordering is sound: K1 (fsync) is first; the N8/N3 enforcers (K2/K4/K5) all sit below {K1, K3}. **No integration finding.**

---

## 5. TEST BEADS

- **Scenario-test bead hk-rz4** exists (live), type=task, label `codename:hitl-decisions` + `scenario-test`, deps **{K1=hk-33p, K2=hk-xz9, K3=hk-qed, K4=hk-kba}** — matches the plan. Covers S1, S3, S4, S8. ✅
- **Exploratory-test bead hk-1vl** exists (live), type=task, label `codename:hitl-decisions` + `exploratory-test`, deps **{K3=hk-qed, K4=hk-kba}** — matches the plan. Covers S2, S6. ✅
- **Reviewer-mandated S5 (restart):** folded into **K3** as a REQUIRED `//go:build scenario` deliverable + acceptance criterion (emit → restart daemon → open set unchanged → resolve → drops out). Acceptable per the project's "fold into owning impl task" convention. ✅
- **Reviewer-mandated S7 (reap/re-wait):** folded into **K5** as a REQUIRED `//go:build scenario` deliverable + acceptance criterion (S7a kill→keeper-withdraw→leaves-queue, Stale-not-reaped; S7b restart→re-wait→wake-or-clean-withdraw). Acceptable. ✅

Both folded scenarios correctly note the scenario-test authoring convention (worktree sub-agent + cherry-pick; daemon gate skips `//go:build scenario`; run independently). **No test-bead finding.**

---

## 6. LIVE-BEADS VERIFICATION

**Result: ALL-CORRECT.**

| Bead | Role | exists | type | prio | label codename:hitl-decisions | assignee | deps on hk-rom? |
|------|------|--------|------|------|-------------------------------|----------|-----------------|
| hk-33p | K1 | ✅ | task | P2 | ✅ | (none) | ❌ none |
| hk-qed | K3 | ✅ | task | P2 | ✅ | (none) | ❌ none |
| hk-xz9 | K2 | ✅ | task | P2 | ✅ | (none) | ❌ none |
| hk-kba | K4 | ✅ | task | P2 | ✅ | (none) | ❌ none |
| hk-061 | K5 | ✅ | task | P2 | ✅ | (none) | ❌ none |
| hk-50f | K6 | ✅ | task | P2 | ✅ | (none) | ❌ none |
| hk-rz4 | scenario | ✅ | task | P2 | ✅ (+scenario-test) | (none) | ❌ none |
| hk-1vl | exploratory | ✅ | task | P2 | ✅ (+exploratory-test) | (none) | ❌ none |

**Wired dependency edges (live) vs plan §E:**

| Bead | Live deps | Plan §E expects | Match |
|------|-----------|-----------------|-------|
| hk-qed (K3) | hk-33p | K1 | ✅ |
| hk-xz9 (K2) | hk-qed, hk-33p | K1, K3 | ✅ |
| hk-kba (K4) | hk-qed, hk-33p | K1, K3 | ✅ |
| hk-061 (K5) | hk-qed, hk-33p | K1, K3 | ✅ |
| hk-50f (K6) | hk-qed, hk-33p | K1, K3 | ✅ |
| hk-rz4 | hk-kba, hk-xz9, hk-qed, hk-33p | K1, K2, K3, K4 | ✅ |
| hk-1vl | hk-kba, hk-qed | K3, K4 | ✅ |

- `br dep cycles` → **"✓ No dependency cycles detected."**
- **No bead depends on the open epic hk-rom** (verified by enumerating each bead's `dependencies` array). The epic carries `assignee: paul`; the impl/test beads are correctly unassigned and attached to the work only via the `codename:hitl-decisions` label.

The live graph matches 07-tasks.md §E exactly.

---

## 7. FINDINGS (numbered, severity-tagged)

All findings are **P3 (non-blocking polish)**. None blocks finalize.

1. **[P3] §D.3 maps N4 to "K4/K5" but no live bead text mentions a daemon-written bead marker.** N4 (write discipline — any "blocked-on-human" bead marker is daemon-written only) is mapped in the coverage table, but the optional bead marker is a K7-side (deferred) concern and neither K4 nor K5's deliverables actually write one in v1. **Ref:** 07-tasks.md §D.3 row N4; SPEC.md §N4/D4. **Fix:** clarify in §D.3 that N4 is vacuously satisfied in harmonik v1 (no agent and no v1 component writes terminal bead state; the marker is K7/deferred). Cosmetic — does not affect any bead.

2. **[P3] K6's optional `.decision_waiting` gate is described as both a deliverable and "optional/belt-and-suspenders."** 07-tasks.md §A-K6 lists the `.decision_waiting` gate as a deliverable but then says the §4 heartbeat already covers keeper-aliveness, making it optional. **Ref:** 07-tasks.md §A-K6 deliverables vs acceptance criteria. **Fix:** the acceptance criteria correctly require only the heartbeat-based exemption (not the optional gate), so this is internally consistent — but to avoid an implementer over-building, restate the gate as "MAY add" in the deliverables line, matching the AC. Wording only.

3. **[P3] EV-025 event-model doc entries (K1 deliverable) have no acceptance criterion.** K1 lists "event-model doc — EV-025 entries" as a deliverable but the K1 acceptance criteria do not assert the doc entry exists. **Ref:** 07-tasks.md §A-K1 deliverables vs acceptance criteria. **Fix:** add a one-line AC ("EV-025 entries present in the event-model doc for all 3 types") so the doc deliverable is gated. Low impact — doc drift only.

4. **[P3] hk-1vl is typed `task` (with an `exploratory-test` label), not a distinct issue_type.** The kerf gate asks for "≥1 scenario-test and ≥1 exploratory-test bead." Both gate beads are `issue_type=task` and distinguished only by the `scenario-test` / `exploratory-test` label. **Ref:** live `br show hk-rz4 hk-1vl`. **Fix:** none required — beads_rust has no `scenario-test`/`exploratory-test` issue_type, so the label convention is the correct mechanism; noting for completeness. No action.

---

## 8. BOTTOM LINE

The tasks pass is complete and correct. Coverage is total (§1–§9, S1–S8, N1–N9 all assigned), the DAG is valid and matches the spec's build order, sizing is appropriate, the three cross-cutting contracts (N1/N8/N3) are assigned and ordered, the two test beads exist with correct deps, and the reviewer-flagged S5/S7 gaps are closed by folded scenario tests in K3/K5. The live bead graph matches the plan exactly and no bead depends on the open epic. **APPROVE.** Proceed to `kerf square` → `kerf finalize`.
