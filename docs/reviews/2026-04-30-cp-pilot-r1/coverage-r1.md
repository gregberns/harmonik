# CP Pilot — Coverage Review (r1)

`reviewer: coverage` · `protocol: pilot-review-protocol.md §3.1 v0.2` · `inputs: specs/control-points.md v0.3.2 (1126 lines, status reviewed) · docs/decompose-to-tasks/cp-pilot.md v0.1.0 (304 lines) · docs/decompose-to-tasks/cp-pilot-data.yaml (1216 lines) · discipline.md v0.9`

## Summary line

**CLEAN.** Every numbered ID in CP §4 (55 active reqs), §5 (3 invariants), §6.1–§6.2 (24 schemas), and §8 (no taxonomy bead, routes to EM) is accounted for in the pilot. All pilot §1 / §9 counts and arithmetic check out. Pilot's spec-version reference matches `specs/control-points.md` v0.3.2 exactly. Zero BLOCKER, zero MAJOR, zero MINOR findings.

---

## 1. Enumeration of source-spec IDs

### 1.1 §4 normative requirements (active) — 55 total

CP-001, CP-002, CP-003, CP-004, CP-005, CP-006, CP-007, CP-008, CP-009, CP-010, CP-011, CP-012, CP-013, CP-014, CP-015, CP-016, CP-017, CP-018, CP-019, CP-020, CP-021, CP-022, CP-023, CP-024, CP-025, CP-026, CP-026a, CP-027, CP-028, CP-029, CP-030, CP-031, CP-032, CP-033, CP-034, CP-034b, CP-035, CP-036, CP-037, CP-038, CP-039, CP-040, CP-040a, CP-041, CP-042, CP-043, CP-044, CP-045, CP-046, CP-047, CP-048, CP-049, CP-050, CP-051, CP-052.

Headcount: 52 base IDs (CP-001..CP-052) + 3 letter-suffixed sub-requirements (CP-026a, CP-034b, CP-040a) = **55 active §4 reqs**.

### 1.2 §5 invariants — 3 active, 2 demoted

- **Active:** CP-INV-001 (registry single source of truth), CP-INV-002 (effects observable only via events), CP-INV-003 (cognition replay-safety).
- **Demoted:** CP-INV-004, CP-INV-005 — explicitly named in §5's NOTE block (lines 500–501) as failing the §5 invariant-vs-requirement selection test; consumers redirected to CP-020 (§4.4) and CP-026 (§4.5) respectively. Confirmed by direct read of `specs/control-points.md` lines 500–501.

### 1.3 §6 schemas — 24 records/interfaces/enums

§6.1 (top-level): ControlPoint, Kind, Evaluator (3); §6.1.1 Gate payload: GatePayload, GateSubtype, AttachPoint (3); §6.1.2 Hook payload: HookPayload, SideEffectKind (2); §6.1.3 Guard payload: GuardPayload (1); §6.1.4 Budget payload: BudgetPayload, ScopeTarget, BudgetResource, BudgetScope (4); §6.1.5: DelegationPath (1); §6.1.6 Verdict: GateVerdictRecord, HookVerdictRecord, CognitionMeta, SideEffect, IdempotencyClass, GateAction (6); §6.1.7: Registry interface (1); §6.2: Role, PermissionSchema, FreedomProfile (3). Total **24**.

§6.3 (policy YAML template), §6.4 (policy expression grammar — adopted from `expr-lang/expr`), §6.5 (co-owned event payloads — owned by EV), and §6.6 (schema evolution prose) are correctly NOT minted as schema beads per discipline §2.6 (no new RECORD/INTERFACE/ENUM constructs).

### 1.4 §8 errors — 0 first-class CP error beads

§8 explicitly routes ControlPoint failures onto EM's failure-class taxonomy ("Not applicable as a separate taxonomy; ControlPoint failures map onto the execution-model failure classes per [execution-model.md §8]"). No `cp-error.taxonomy` bead is mintable.

### 1.5 Retired §4 IDs — 0

Grep for `[retired]` in `specs/control-points.md` returns zero hits. No `[retired]` markers.

---

## 2. Coverage verification

### 2.1 §4 reqs (55 → 53 first-class beads)

All 55 §4 IDs accounted for in the pilot:

- **53 first-class req beads** in pilot §2 / yaml `kind: req`: cp-001, cp-002, cp-003, cp-004 (carries `req: [CP-004, CP-005]` per §2.3 coalesce), cp-006..cp-051, cp-026a, cp-034b, cp-040a, cp-031 (carries `req: [CP-031, CP-052]` per §2.1a collapse).
- **CP-005** is named in cp-004's coalesce comment (`# coalesce: CP-004 declarative + CP-005 normative table`) and the pilot row title "Per-Kind semantics fixed by §4.1 table (coalesce: CP-004 + CP-005)". Coalesce sound: CP-005 IS the table CP-004 references; one cohesive `func validateKindRow(...)` body in any plausible implementation; neither is independently testable without the other.
- **CP-052** is named in cp-031's collapse notes line and the pilot row title "Default skills include Beads-CLI skill (notes: CP-052)". Collapse sound per §2.1a: one-sentence body, single in-spec cite to CP-031, no substantive impl distinct from CP-031.

No missed reqs. No phantom reqs (every `req:CP-NNN` tag in the pilot maps to a real spec ID).

### 2.2 §5 invariants (3 → 3 sensor beads)

- CP-INV-001 → cp-inv-001 (`kind: invariant`) ✓
- CP-INV-002 → cp-inv-002 (`kind: invariant`) ✓
- CP-INV-003 → cp-inv-003 (`kind: invariant`) ✓

Demoted CP-INV-004 / CP-INV-005 correctly produce no bead per discipline §2.5 ("only ACTIVE invariants produce sensors"). Pilot §1 and §4 explicitly call out the demotion with reference to spec §5 NOTE — accurate.

### 2.3 §6 schemas (24 → 24 schema beads)

All 24 schema constructs mapped 1:1 to `cp-schema.*` beads:

| Spec construct | Pilot bead |
|---|---|
| ControlPoint (RECORD) | cp-schema.control-point |
| Kind (ENUM) | cp-schema.kind |
| Evaluator (RECORD) | cp-schema.evaluator |
| GatePayload (RECORD) | cp-schema.gate-payload |
| GateSubtype (ENUM) | cp-schema.gate-subtype |
| AttachPoint (ENUM) | cp-schema.attach-point |
| HookPayload (RECORD) | cp-schema.hook-payload |
| SideEffectKind (ENUM) | cp-schema.side-effect-kind |
| GuardPayload (RECORD) | cp-schema.guard-payload |
| BudgetPayload (RECORD) | cp-schema.budget-payload |
| ScopeTarget (constrained primitive) | cp-schema.scope-target |
| BudgetResource (ENUM) | cp-schema.budget-resource |
| BudgetScope (ENUM) | cp-schema.budget-scope |
| DelegationPath (RECORD) | cp-schema.delegation-path |
| GateVerdictRecord (RECORD) | cp-schema.gate-verdict-record |
| HookVerdictRecord (RECORD) | cp-schema.hook-verdict-record |
| CognitionMeta (RECORD) | cp-schema.cognition-meta |
| SideEffect (RECORD) | cp-schema.side-effect |
| IdempotencyClass (ENUM) | cp-schema.idempotency-class |
| GateAction (ENUM) | cp-schema.gate-action |
| Registry (INTERFACE) | cp-schema.registry |
| Role (RECORD) | cp-schema.role |
| PermissionSchema (RECORD) | cp-schema.permission-schema |
| FreedomProfile (RECORD) | cp-schema.freedom-profile |

24/24 covered. No phantom schemas in the pilot.

### 2.4 §8 errors (0 CP taxonomy beads, routing documented)

Pilot §6 ("Error-taxonomy treatment") and yaml top-comment F-pilot-CP-1 both document the §8 routing decision: CP §8 is a routing prose section, no `cp-error.taxonomy` bead minted, consumers (cp-015, cp-023, cp-027, cp-034b, cp-049 + the schema cp-schema.budget-payload) fire `blocks` edges to `em-error.taxonomy` and/or `hc-error.taxonomy`. Routing is explicitly documented per F-pilot-CP-1 (resolved by discipline v0.9 §2.11(c.2) by analogy). Coverage clean.

### 2.5 Retired markers (0)

No spec-side `[retired]` markers; pilot §1 says "Zero retired §4 IDs" — match.

---

## 3. Counts and arithmetic verification

### 3.1 Pilot §1 "Counts" section

| Claim | Source-spec actual | Match |
|---|---|---|
| 55 active §4 normative requirements | 55 (52 base + 3 letter-suffix) | ✓ |
| 1 §2.3 coalesce (CP-004 + CP-005 → cp-004) | CP-005 is the table CP-004 references; sound | ✓ |
| 1 §2.1a pure-cross-reference collapse (CP-052 → cp-031) | CP-052 is a one-sentence restatement with a single in-spec cite to CP-031 | ✓ |
| 53 first-class §4 req beads after coalesce + collapse | 55 − 1 (coalesce) − 1 (collapse) = 53 | ✓ |
| 0 §2.2 multi-step splits | Confirmed — CP-034b 2 bounds fails signal 1; CP-040a 5-input list resolves via F8b shared-function-body | ✓ |
| 3 active §5 invariants (CP-INV-001..003); CP-INV-004/005 demoted | §5 NOTE explicitly demotes 004/005; only 001/002/003 remain | ✓ |
| 24 §6.1 schema constructs | Hand-tallied to 24 in §1.3 above | ✓ |
| 14 §6.5 co-owned events | grep of §6.5 confirms 14 event names | ✓ |
| 0 `cp-error.taxonomy` beads | §8 routes to EM | ✓ |
| 5 test-infra beads | yaml lists 5 cp-test.* beads | ✓ |

### 3.2 Pilot §9 sanity tally arithmetic

Pilot §9 table totals:
- Spec parent epic: 1
- Requirement beads: 53
- Step beads: 0
- Sensor / invariant beads: 3
- Schema beads: 24
- Error-taxonomy beads: 0
- Test-infrastructure beads: 5

Sum: 1 + 53 + 0 + 3 + 24 + 0 + 5 = **86**. Pilot §9 prints "86" — match ✓.

The yaml top-comment claims "85 beads" (excluding the 1 spec-parent epic): 53 + 0 + 3 + 24 + 0 + 5 = **85** ✓.

Both forms internally consistent.

### 3.3 Pilot's spec-version reference

- Pilot line 3: "drafted 2026-04-30 against `specs/control-points.md` v0.3.2 (status `reviewed`, last-updated 2026-04-24)".
- `specs/control-points.md` front-matter line 10: `version: 0.3.2`. Line 13: `last-updated: 2026-04-24`. Line 8: `status: reviewed`.

All three reference fields match exactly. **No stale-version flag.**

---

## 4. Findings

| Finding | Severity | Lane | Justification |
|---|---|---|---|
| (none) | — | — | — |

**Total findings: 0.**

- Missed IDs: 0
- Phantom IDs: 0
- Tally inconsistencies: 0
- Stale-version flags: 0

---

## 5. Reviewer notes (informational, not findings)

- The CP §5 NOTE block (lines 500–501) explicitly demotes CP-INV-004 and CP-INV-005 with named §4 redirect targets (CP-020 and CP-026 respectively). The pilot author's claim that these are "demoted in spec §5 and not just missed" was verified by direct read — the demotion is normative spec text, not a pilot-side judgment call.
- The pilot demonstrates the §2.3 coalesce + §2.1a collapse + F8b shared-function-body tiebreaker patterns simultaneously without confusion. Sound application of discipline v0.9.
- The 24-schema density (corpus high-water mark per F-pilot-CP-5) is mechanically correct under §2.6's one-bead-per-schema rule. The pilot author's `class`-lane finding F-pilot-CP-5 surfaces this for post-corpus review without proposing a discipline patch — appropriate posture.
- Coverage scope of this review excludes decomposition-quality (§3.2 reviewer) and reference-edge correctness (§3.3 reviewer); those are companion outputs.

---

## 6. Verdict

**Coverage: CLEAN.** No BLOCKER / MAJOR / MINOR findings. Pilot is coverage-complete against `specs/control-points.md` v0.3.2 and may proceed to synthesis without coverage-side patches.
