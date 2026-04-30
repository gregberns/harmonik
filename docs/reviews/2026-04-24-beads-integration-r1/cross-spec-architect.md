# Beads Integration — Round 1 Cross-Spec Architect Review

**Spec under review:** `/Users/gb/github/harmonik/specs/beads-integration.md` (v0.2.0, 2026-04-24, status `draft`)
**Reviewer role:** Cross-Spec Architect
**Scope:** BI's seat in the corpus dependency graph; correctness of every inter-spec citation; scope leaks versus owning specs; 3-store alignment with architecture; HC skill-injection seam; bead-ID propagation contract; reverse-drift map; bootstrap-citation closure; recommended graph edits.

## 1. Verdict

**Changes required (blocking on an R2).** BI's architectural seat is correct and load-bearing: it is the only spec that owns the external-dependency surface (the Beads SQLite fork, the `br` CLI, the terminal-transition write discipline, the idempotency contract, the Beads-CLI skill's existence as a required default), and the scope split against EM/EV/HC/CP/ON/RC is cleanly articulated in §2.2 and §10.3. The in-scope / out-of-scope boundary is disciplined — no detectable scope leak into event payload shape, workflow state semantics, the handler Go interface, the skill-injection mechanism, or operator-CLI surfaces.

What is **not** sound is the **inter-spec citation surface itself**. Every citation BI makes to a sibling spec uses an outdated section-numbering convention; almost none of them resolve in the current corpus:

- **[event-model.md §3.2]** is cited 5 times and means "event payload shapes" — EV §3 is Glossary. The payload shapes live in §6.3, the event taxonomy in §8 (e.g., `§8.1.1 run_started`). Five broken cites.
- **[event-model.md §3.4]** cited once for fsync durability — EV §3.4 does not exist; durability contract is EV §4.4 (`EV-016`).
- **[control-points.md §6.11]** cited 4 times as the skill-declaration surface — control-points has no §6.11 (its §6 ends at §6.6). Skill declaration is **§4.11** (CP-049/CP-050/CP-052). This is the single most impactful wrong cite because BI's §4.2 and §4.9 both route their mechanism through it; the wrong anchor does not resolve.
- **[control-points.md §6.5]** cited once for YAML policy and per-role skill exclusion — CP §6.5 is Co-owned event payloads, not policy YAML. Per-role default/exclusion lives at **§6.3 Policy YAML** (`skill_sets[]`) and **§4.6 Role permissions** (CP-028/CP-031).
- **[reconciliation.md §9.N]** cited 5 times for Cat 3 taxonomy, action-mapping, detectors, and verdict vocabulary — RC §9 is Cross-references (depends/reverse/co-refs). Category taxonomy is §8.4 (Cat 3) / §8.4a (Cat 3a) / §8.6 (Cat 3c), detectors at §4.3, action-mapping at §8.12, verdict vocabulary at §4.5. (Additionally: file path is `reconciliation/spec.md`, not `reconciliation.md` at the repo root.)
- **[process-lifecycle.md §8.2]** cited once for startup sequence — PL §8 is the error taxonomy; startup is §4.2 (`PL-005`, steps 0–9). Moreover the "steps 3–4" wording in BI-016 does not match PL-005's current steps (the reconciliation reads are steps 6–7).
- **[operator-nfr.md §7.4 / §7.5 / §7.8]** cited three times for queue-format, checkpoint-format N-1 compat, and throughput bounds — ON §7 is state machines + protocols, not obligations. Queue-format is §4.4 (ON-015/016/017), schema compat is §4.5 (ON-018/019), `§7.8` has no analog (there is no throughput-bounds section anywhere in ON).
- **[workspace-model.md §5.3 / §5.8]** cited twice — WM §5 is invariants. Session-log metadata is §4.7; branching is §4.2 and §4.5. WM v0.3 publishes an §A.4 migration map for exactly this drift and explicitly counts **5 inbound cites** in BI as pending migration.

None of these is a semantic design flaw — in every case the citation target exists under a different number. But the combined effect is that a reader cannot traverse from BI to any single sibling spec without hitting a broken anchor, and the corpus lint obligation in `docs/foundation/spec-template.md §Conformance checklist` fails across all 18 of them.

The second-order finding is **reverse drift**: sibling specs back-cite BI at legacy `[beads-integration.md §10.N]` / `§10.8a` / `§10.9` / `§10.7` anchors (see §8 of this review, 13 inbound sites identified). BI's §12 revision-history line for v0.2 records "one anchor migration of §1.5→§4.6 in the `br serve` clause"; that is an incomplete migration. BI needs to publish its own §A.4 reverse-drift map analogous to WM v0.3's, because the §10 → §4 migration is non-trivial (BI's internal layout renumbered §1 / §3 / §5 / §7 / §8 → §4.1 / §4.3 / §4.5 / §4.7 / §4.8 / §4.9 / §4.10).

Third-order: BI's §9.1 **depends-on list is a superset of the front-matter `depends-on`**. Front matter declares only `execution-model, event-model`, but §9.1 additionally cites handler-contract.md §4.11 and control-points.md §6.11 / §6.5 as normative depends. That is a structural error per AR-022: the front-matter depends-on is the authoritative dependency declaration. Either promote HC and CP into the front-matter (correct — BI truly does depend on both normatively) or demote the §9.1 entries to co-references (wrong — HC's skill-injection mechanism is load-bearing for BI-004 / BI-027 / BI-028). The WM v0.3 precedent is to include all normative peers in front-matter `depends-on`.

Fourth-order: BI's §9.1 and §6.4 both reference `bead_id?` as an optional field on `run_completed` and `run_failed` — but event-model §8.1.2 / §8.1.3 payloads **do not include `bead_id`** (only `run_started` §8.1.1 does). BI either needs to assert the field and publish a coordinated amendment against EV, or narrow §6.4's claim to the three EV events that actually carry `bead_id?` (`run_started` §8.1.1, `checkpoint_written` §8.1.7, `session_log_location` §8.3.7, `store_divergence_detected` §8.6.8, `divergence_inconclusive` §8.6.10). Currently BI's §4.6.BI-019 is a *universal* claim ("Every event emitted for a bead-bound run MUST carry the optional `bead_id` field on its payload") that EV's taxonomy does not support. This is a genuine design gap, not a citation typo — see §7 below.

Priority ordering for R2: (a) mechanical citation fixes for the 18 broken sibling anchors; (b) front-matter `depends-on` completion (add HC, CP, WM, PL, ON, RC, AR); (c) BI-019 vs EV §8 reconciliation (the bead_id field inventory); (d) §A.4 reverse-drift map publication; (e) the `components.md §10.x` bootstrap citations in CP and RC (not BI's direct concern but caught during audit).

## 2. Dependency graph

### 2.1 Front-matter `depends-on`

Current (BI v0.2.0 front-matter, lines 14–17):

```
depends-on:
  - execution-model
  - event-model
```

### 2.2 §9.1 declared depends

Current (BI §9.1, lines 412–419) cites **five** normative dependencies:

| # | Anchor cited by BI | Resolution status |
|---|---|---|
| D1 | `[execution-model.md §4.3]` — run-vs-bead + bead_id on Run | Resolves (EM §4.3 is Run model; EM-014 declares the field) |
| D2 | `[execution-model.md §6.2]` — checkpoint commit trailer format | Resolves (EM §6.2 declares `Harmonik-Bead-ID` as conditional trailer) |
| D3 | `[execution-model.md §4.4]` — git checkpoint trail | Resolves (EM §4.4 is Checkpoint contract; EM-016/EM-017) |
| D4 | `[event-model.md §3.2]` — event taxonomy | **Broken** — EV §3 is Glossary; target is EV §8 (row) and §6.3 (shape) |
| D5 | `[event-model.md §3.4]` — fsync durability | **Broken** — EV §3.4 is Glossary subsection; target is EV §4.4 (`EV-016` / `EV-016a` / `EV-017`) |
| D6 | `[handler-contract.md §4.11]` — skill-injection mechanism | Resolves (HC §4.11 is Skill injection; HC-046 through HC-050) |
| D7 | `[control-points.md §6.11]` — skill-declaration surface | **Broken** — CP has no §6.11; target is CP §4.11 (`CP-049`–`CP-052`) |
| D8 | `[control-points.md §6.5]` — YAML policy for per-role exclusion | **Broken** — CP §6.5 is Co-owned event payloads; target is **§4.6 CP-028/CP-031** (role default skills) and/or **§6.3 Policy YAML** (the `skill_sets[]` block) |

**Gap between front-matter and §9.1:** HC and CP are cited as normative in §9.1 but missing from front-matter `depends-on`. Per `docs/foundation/spec-template.md` v1.1, the front-matter list is the authoritative dependency declaration (consumed by corpus lint under AR-022). PL v0.3.0 and WM v0.3.0 both carry expanded front-matter `depends-on` arrays covering every normatively-cited peer; BI v0.2.0 does not.

### 2.3 §9.3 co-references

Current (BI §9.3, lines 427–436) lists eight co-reference entries. Status:

| # | Anchor | Resolution |
|---|---|---|
| C1 | `[workspace-model.md §5.3 Session-log metadata]` | **Broken** — WM §5 is invariants; target is §4.7 (session-log directory + metadata sidecar) |
| C2 | `[workspace-model.md §5.8 Branching model]` | **Broken** — target is WM §4.2 (branch naming) and §4.5 (merge back) |
| C3 | `[reconciliation.md §9.2 Category taxonomy]` | **Broken** — RC §9.2 is reverse-deps; target is **[reconciliation/spec.md §8.4]** (Cat 3) |
| C4 | `[reconciliation.md §9.2a Action-mapping layer]` | **Broken** — no §9.2a in RC; target is **[reconciliation/spec.md §8.12]** (Action-mapping layer) |
| C5 | `[reconciliation.md §9.3 Detection rules]` | **Broken** — RC §9.3 is co-references; target is **[reconciliation/spec.md §4.3]** (Detectors, RC-013/RC-014) |
| C6 | `[reconciliation.md §9.5 Verdict vocabulary]` | **Broken** — RC §9.5 does not exist; target is **[reconciliation/spec.md §4.5]** (RC-020/RC-021/RC-025) |
| C7 | `[process-lifecycle.md §8.2 Startup sequence]` | **Broken** — PL §8 is errors; target is §4.2 (PL-005 startup sequence) |
| C8 | `[operator-nfr.md §7.4 Queue-format contract]` | **Broken** — ON §7.4 does not exist; target is §4.4 (ON-015/016/017) |
| C9 | `[operator-nfr.md §7.5 Checkpoint-format stability]` | **Broken** — target is §4.5 (ON-018/019) |
| C10 | `[docs/foundation/problem-space.md §Locked decisions]` | Bootstrap OK per BI's own permission clause (line 436) |

### 2.4 Cycle risks

BI's load-bearing citations trace:

- BI → EM (EM-012 Run record, EM-014 bead_id field, EM §6.2 trailers, EM §4.4 checkpoint trail). EM already depends on BI in the reverse direction (`[beads-integration.md §4.6 BI-018]` at EM-013, `§4.3 BI-005/BI-008/BI-009` at EM-014/EM-015a, etc. — see §8 below). **EM ↔ BI is a mutual-dependency pair.** This is not unusual (both specs declare halves of the same contract: EM owns Run, BI owns bead_id's write path) but it MUST be broken directionally in the depends-on graph. Precedent from EM/EV: one spec declares `depends-on` and the other moves the cite to §9.3 co-references. BI currently puts EM in depends-on (correct); EM puts BI in §9.3 (correct). Cycle avoided.
- BI → HC (HC §4.11 skill injection). HC does not currently cite BI; no cycle.
- BI → CP (CP §4.11 skill declaration, §4.6 role skills). CP cites BI at §4.6.CP-031 and §4.11.CP-052 under the `docs/foundation/components.md §10.9` bootstrap (see §9 below). No cycle.
- BI → WM (co-ref session-log, branching). WM cites BI at WM §6.1 (`bead_id` correlation) and §4.8 (reconciliation routing). No cycle — WM has BI in its front-matter `depends-on` as of v0.3.0 (per WM §12).
- BI → PL (co-ref startup). PL cites BI at PL-005 step 6 and PL-006 stale-intent. No cycle.
- BI → RC (co-ref Cat 3 / action-mapping / detectors / verdict). RC heavily cites BI (13 sites per §8 below). No cycle; RC has BI in §9.3 co-references.
- BI → ON (co-ref queue-format, schema-compat). ON cites BI at ON-015 and ON-017. No cycle.
- BI → AR (co-ref amendment protocol). AR does not cite BI; no cycle.

**No cycles in the current graph.** The dependency shape is correct; what needs fixing is the anchors, not the directionality.

## 3. Citation correctness — every inter-spec cite walked

All 47 inter-spec citations in BI body (excluding the bootstrap permissions in §9.3 and revision history) listed with resolution status.

### 3.1 Cites to execution-model

| BI location | Anchor | Target | Resolves? |
|---|---|---|---|
| §2.2 out-of-scope | `[execution-model.md §4.3]` run metadata | EM §4.3 Run model | Yes |
| §2.2 out-of-scope | `[execution-model.md §6.2]` trailer format | EM §6.2 Checkpoint commit trailer format | Yes |
| §4.4.BI-011 | `[execution-model.md §4.4]` checkpoint trail | EM §4.4 Checkpoint contract | Yes |
| §4.6.BI-017 | `[execution-model.md §6.1 Run]` | EM §6.1 Typed ID aliases and record schemas | Yes (`Run` record is declared in §6.1) |
| §4.6.BI-018 | `[execution-model.md §6.2]` trailer format | EM §6.2 | Yes |
| §9.1 D1 | `[execution-model.md §4.3]` | EM §4.3 | Yes |
| §9.1 D2 | `[execution-model.md §6.2]` | EM §6.2 | Yes |
| §9.1 D3 | `[execution-model.md §4.4]` | EM §4.4 | Yes |

EM citations are all clean. EM v0.3.0 has already migrated its own cites of BI from `§10.N` to `§4.N` form (see EM §12 v0.3.0 changelog, line 1065), so the EM ↔ BI pair is the only pair in the corpus with both sides migrated.

### 3.2 Cites to event-model

| BI location | Anchor | Target | Resolves? |
|---|---|---|---|
| §2.2 out-of-scope | `[event-model.md §3.2]` event payload shapes | EV §3 is Glossary; payload shapes in §6.3; taxonomy in §8 | **No** |
| §4.4.BI-011 | `[event-model.md §3.2]` JSONL event log | Same issue | **No** |
| §4.6.BI-019 | `[event-model.md §3.2]` `bead_id` field | Same issue | **No** |
| §4.10.BI-030 | `[event-model.md §3.4]` fsync durability | EV §3.4 is Glossary; target is §4.4 (EV-016 / EV-016a / EV-017) | **No** |
| §6.4 header | `[event-model.md §3.2]` | Same §3.2 issue | **No** |
| §6.4 closing | `[event-model.md §3.2]` is normative for shape | Same issue | **No** |
| §9.1 D4 | `[event-model.md §3.2]` | Same issue | **No** |
| §9.1 D5 | `[event-model.md §3.4]` | Same issue | **No** |

Total: **8 broken EV cites.** Every one targets "§3.2" or "§3.4" which are sub-entries of EV's Glossary. The two legitimate targets:

- **Event taxonomy rows** live at EV §8.N (§8.1 Run lifecycle, §8.2 Control-point, §8.3 Agent/handler, §8.6 Reconciliation, §8.7 Operator-control). BI should cite per-event rows like `[event-model.md §8.1.1]` (`run_started`) or, when it means "the taxonomy", `[event-model.md §8]`.
- **Per-type payload schemas** live at EV §6.3.
- **Fsync durability contract** is EV §4.4 `EV-016` (per-event fsync; F/O/L class), `EV-016a` (per-event fsync; no multi-event atomicity), `EV-017` (event loss between fsyncs acceptable).

Where BI says "emission WHEN" it should cite §8 row; where it says "payload shape" it should cite §6.3 or §8 row; where it says "fsync durability" it should cite §4.4 `EV-016` explicitly.

### 3.3 Cites to handler-contract

| BI location | Anchor | Target | Resolves? |
|---|---|---|---|
| §2.2 out-of-scope | `[handler-contract.md §4.11]` skill-injection | HC §4.11 Skill injection (HC-046..HC-050) | Yes |
| §3 glossary "Beads-CLI skill" | `[handler-contract.md §4.11]` | HC §4.11 | Yes |
| §4.2.BI-004 | `[handler-contract.md §4.11]` | HC §4.11 | Yes |
| §4.9.BI-028 | `[handler-contract.md §4.11]` | HC §4.11 | Yes |
| §9.1 D6 | `[handler-contract.md §4.11]` | HC §4.11 | Yes |
| §10.3 | `[handler-contract.md §4.11]` | HC §4.11 | Yes |

HC citations are all clean.

### 3.4 Cites to control-points

| BI location | Anchor | Target | Resolves? |
|---|---|---|---|
| §2.2 out-of-scope | `[control-points.md §6.11 Skill declaration surface]` | Target is **CP §4.11** (Skill declaration); no §6.11 exists | **No** |
| §4.2.BI-004 | `[control-points.md §6.11]` | **No** |
| §4.9.BI-027 | `[control-points.md §6.11]` | **No** |
| §4.9.BI-028 | `[control-points.md §6.5]` YAML per-role exclusion | CP §6.5 is Co-owned event payloads; target is **§4.6 CP-028/CP-031** and/or **§6.3 Policy YAML** (skill_sets) | **No** |
| §9.1 D7 | `[control-points.md §6.11]` | **No** |
| §9.1 D8 | `[control-points.md §6.5]` | **No** |

Total: **6 broken CP cites.** The `§6.11` anchor is the most damaging because it is the only cite to CP's skill-declaration surface and BI's §4.9 hinges on it; at review time a reader following the cite finds nothing.

The correct canonical cite is `[control-points.md §4.11]` (skill declaration) — optionally paired with `[control-points.md §4.6 CP-031]` (role-default skill set obligation) for the "Beads-CLI skill is a default" claim.

Note: HC §4.12 HC-050 itself miscites CP at `[control-points.md §6.11]` (HC-050 line 490). This is an inbound error to CP that BI is mirroring; reporting separately to HC's R2 reviewer.

### 3.5 Cites to workspace-model

| BI location | Anchor | Target | Resolves? |
|---|---|---|---|
| §4.4.BI-010 | `[workspace-model.md §5.8]` merge to target | WM §5 is invariants; target is §4.5 (WM-020..WM-022) merge-back | **No** |
| §4.5.BI-014 | `[workspace-model.md §5.8]` branching | Target is §4.2 (branch naming, three-level) and §4.5 | **No** |
| §4.6.BI-020 | `[workspace-model.md §5.3]` session-log metadata | WM §5 invariants; target is §4.7 (session-log directory) | **No** |
| §9.3 C1 | `[workspace-model.md §5.3]` | Same issue | **No** |
| §9.3 C2 | `[workspace-model.md §5.8]` | Same issue | **No** |

Total: **5 broken WM cites.** All resolvable through WM v0.3's §A.4 migration map:

- `§5.3 Session-log aggregation` → **WM §4.7**
- `§5.4 Merge semantics` → **WM §4.5**
- `§5.8 Branching model` → **WM §4.2**
- `§5.9 Re-run rule` → **WM §4.9** (BI does not currently cite 5.9 but should, for BI-INV-002 stability)

WM §A.4 explicitly counts 5 BI inbound cites — audit confirms exactly 5 broken sites. No hidden WM cites; no new ones to add.

### 3.6 Cites to reconciliation

Note: file path convention. Reconciliation lives at `specs/reconciliation/spec.md` (not `specs/reconciliation.md`). Every BI cite reads `reconciliation.md` — for internal repo paths this is a **10th mechanical issue** layered on top of the anchor drift. EM v0.3, WM v0.3, and PL v0.3 all migrated inbound RC cites to `reconciliation/spec.md` form.

| BI location | Anchor | Target | Resolves? |
|---|---|---|---|
| §2.2 out-of-scope | `[reconciliation.md §9.3]` Cat 3a detector | RC §9.3 is co-references; target is §4.3 (RC-013) and §8.4a (Cat 3a) | **No** (path + anchor) |
| §4.4.BI-010 | `[reconciliation.md §9.5]` `reopen-bead` verdict | RC §9.5 does not exist; target is **§4.5 RC-020 / RC-028** | **No** |
| §4.7.BI-022 | `[reconciliation.md §9.2]` Cat 3 classification | Target is §8.4 (Cat 3 generic) | **No** |
| §4.7.BI-022 | `[reconciliation.md §9.2a]` Cat 3c auto-resolver | Target is §8.6 (Cat 3c) and §8.12 (action-mapping) | **No** |
| §4.7.BI-023 | `[reconciliation.md §9.3a]` JSONL divergence-evidence | RC §9.3a does not exist; closest target is §4.3 detectors or §8.6.8 `store_divergence_detected` in EV | **No** |
| §4.10.BI-032 | `[reconciliation.md §9.3]` Cat 3a detector | Same as BI-030 | **No** |
| §4.10.BI-032 | `[reconciliation.md §9.2a]` classification + auto-resolver | Target is §8.12 | **No** |
| §5.BI-INV-003 | `[reconciliation.md §9.2]` Cat 3 + auto-resolver | Target is §8.4 | **No** |
| §9.3 C3 | `[reconciliation.md §9.2]` Category taxonomy | §8.4 | **No** |
| §9.3 C4 | `[reconciliation.md §9.2a]` Action-mapping layer | §8.12 | **No** |
| §9.3 C5 | `[reconciliation.md §9.3]` Detection rules | §4.3 | **No** |
| §9.3 C6 | `[reconciliation.md §9.5]` Verdict vocabulary | §4.5 | **No** |
| §10.3 | `[reconciliation.md]` (unqualified) | would need §4.3 + §8 | Partial (no anchor) |

Total: **12 broken RC cites** (all sharing the double problem of path `reconciliation.md` → `reconciliation/spec.md` AND `§9.N` → `§4.N` / `§8.N`).

Mapping table for the R2 fix pass:
- `§9.2 Category taxonomy` → `§8.4` (generic Cat 3), `§8.4a` (Cat 3a), `§8.5` (Cat 3b), `§8.6` (Cat 3c)
- `§9.2a Action-mapping` → `§8.12`
- `§9.3 Detection rules` → `§4.3` (RC-012/RC-013/RC-014)
- `§9.3a` does not exist in any form — closest substance is §4.3 RC-013 plus EV §8.6.8 `store_divergence_detected`
- `§9.5 Verdict vocabulary` → `§4.5` (RC-020 / RC-022 / RC-025 / RC-028)

### 3.7 Cites to process-lifecycle

| BI location | Anchor | Target | Resolves? |
|---|---|---|---|
| §4.1.BI-001 RATIONALE | `[process-lifecycle.md §8.1]` single-machine per-project daemon | PL §8 is errors; target is §4.1 (Per-project daemon scope, PL-001) | **No** |
| §4.5.BI-016 | `[process-lifecycle.md §8.2 steps 3–4]` startup | Target is §4.2 PL-005 steps 6–7 (Beads read for reconciliation input) | **No** (anchor + step numbers) |
| §9.3 C7 | `[process-lifecycle.md §8.2]` Startup sequence | §4.2 PL-005 | **No** |

Total: **3 broken PL cites.** Note the inline "steps 3–4" text in BI-016 is semantically wrong — PL-005 currently runs Beads queries at steps 6–7 (step 4 is Cat 0 pre-check, step 5 is git-log walk). This is a content-drift bug, not just an anchor issue; BI was written against a pre-R1 PL draft that had a different step sequence.

### 3.8 Cites to operator-nfr

| BI location | Anchor | Target | Resolves? |
|---|---|---|---|
| §6.1 IntentLogEntry schema_version | `[operator-nfr.md §7.5]` N-1 readable | ON §7.5 does not exist; target is §4.5 (ON-018) | **No** |
| §6.3 schema evolution | `[operator-nfr.md §7.5]` N-1 | Same | **No** |
| §9.3 operator-nfr queue | `[operator-nfr.md §7.4 Queue-format contract]` | ON §7.4 does not exist; target is §4.4 (ON-015/016/017) | **No** |
| §9.3 operator-nfr schema | `[operator-nfr.md §7.5 Checkpoint-format stability]` | Target is §4.5 (ON-018/019) | **No** |
| §10.3 | `[operator-nfr.md §7.8]` performance/throughput bounds | §7.8 does not exist; no performance-bounds section exists anywhere in ON | **No** — genuine gap |

Total: **5 broken ON cites**, one of which is not just a section-number migration but a missing section entirely. ON has no §7.8 analog for throughput bounds; the closest surfaces are §4.8 (RTO) and §4.11 (Resource budgets), neither of which bounds `br` invocation throughput. BI's §10.3 "does NOT guarantee any performance or throughput bounds on `br` invocation; those are operator-observable in [operator-nfr.md §7.8]" is a claim with no target. Recommend either (a) drop the cite and state "no performance bound is declared at MVH in any spec", or (b) file an OQ against ON to add a `br`-throughput observable.

### 3.9 Cites to architecture

| BI location | Anchor | Target | Resolves? |
|---|---|---|---|
| §4.1.BI-003 | `[architecture.md §4.6]` amendment protocol | AR §4.6 (AR-020..AR-023) | Yes |

This is BI's only architecture cite; it was migrated in the v0.2 cleanup pass per the §12 revision note. Clean.

### 3.10 Bootstrap citation leftovers

Audited for `[docs/foundation/components.md §N]` and `[docs/foundation/problem-space.md §N]` citations.

| BI location | Anchor | Disposition |
|---|---|---|
| §1 Purpose | `[docs/foundation/problem-space.md §Locked decisions]` | Permitted per template's bootstrap rule (no migration target) |
| §9.3 final | `[docs/foundation/problem-space.md §Locked decisions]` | Same |

No `[docs/foundation/components.md §N]` anchors remain in BI body; only the §12 revision-history line references `components.md §10` as provenance, which is appropriate for a history log.

### 3.11 Total citation error count

| Target spec | Broken anchors |
|---|---|
| event-model | 8 |
| control-points | 6 |
| reconciliation (path + anchor) | 12 (+ path form) |
| workspace-model | 5 |
| process-lifecycle | 3 |
| operator-nfr | 5 (1 missing-target, 4 anchor-shift) |
| handler-contract | 0 |
| architecture | 0 |
| **Total** | **39 broken or wrong-path inter-spec citations** |

39 broken cites in a 508-line spec makes BI the heaviest-drift consumer among the 10 foundation specs. For comparison, WM's R1 cross-spec-architect audit counted 48 across the entire corpus.

## 4. Scope leaks

Audited BI against its §2.1 in-scope / §2.2 out-of-scope declaration plus the ownership sibling list (EM, EV, HC, CP, ON, RC, WM, PL).

### 4.1 Event payload shape leaks

- **§6.4 "Co-owned event payloads"** names 5 EV events and states "This spec is normative for WHEN bead_id appears on a payload; [event-model.md §3.2] is normative for the shape of each event." **Correct ownership split** — BI owns the conditional-field presence rule, EV owns the shape. **No leak.**
- **§4.6.BI-019** repeats the same split correctly: "Every event emitted for a bead-bound run MUST carry the optional `bead_id` field on its payload per [event-model.md §3.2]." **No leak** — but see §7 below for the BI-019 universality problem against what EV actually declares.

### 4.2 Handler interface / protocol leaks

- **§4.2.BI-004** declares "agents MUST invoke `br` through the Beads-CLI skill delivered via the handler-contract skill-injection mechanism per [handler-contract.md §4.11]." **Correct** — BI owns the mandate that the skill is the only access path; HC owns the injection mechanism. **No leak.**
- **§4.9.BI-027 / BI-028** declare the skill's *existence* and *universality* as defaults. The skill's *resolution path*, *provisioning*, *runtime shape*, and *event emission* are all correctly left to HC §4.11. **No leak.**

### 4.3 Workflow state / run semantics leaks

- **§4.6.BI-017** declares the `bead_id` field on the `Run` record builds on "[execution-model.md §6.1 Run]" — EM owns the field's definition, BI owns "WHEN the field is populated". **Correct** per §9.1 D1. **No leak.**
- **§4.6.BI-018** declares the checkpoint trailer's presence rule, citing EM §6.2 for the trailer format. **Correct.** **No leak.**

### 4.4 Reconciliation-category leaks

- **§4.7.BI-022 (store authority)** correctly cites `[reconciliation.md §9.2]` for Cat 3 classification ownership and explicitly says "Beads status is corrected (via the §4.4 write surface, routed through the §4.10 adapter) only after the investigator's verdict lands or after a Cat 3c auto-resolver fires per [reconciliation.md §9.2a]." BI owns the adapter path; RC owns the classification. **No leak.**
- **§4.10.BI-032** correctly declares BI owns the intent-log shape and durability while "[reconciliation.md §9.2a] owns the classification and auto-resolver." **No leak.**

### 4.5 Operator-surface leaks

- **§10.3** correctly defers `br`-wrapper operator CLI surfaces to ON. **No leak.**

### 4.6 Minor observation (not a leak)

- **§4.9.BI-027** declares the Beads-CLI skill "MUST document: `br` command surface, output formats, idiomatic `jq` pipelines, and the harmonik write discipline." This is a **declaration of skill-package contents**, which is arguably HC's or CP's territory. In practice this is a documentation obligation on a specific skill package named by BI, not a change to the injection mechanism; the sibling-spec equivalent of CP-052 ("Beads-CLI skill is the motivating default"). **Not a leak**, but it would be cleaner to cite `docs/components/external/beads.md` (already done) and leave the skill-contents prose there.

**Verdict: no structural scope leaks.** The in-scope / out-of-scope boundary is disciplined.

## 5. Store-authority rules vs 3-store model

Architecture v0.3 does **not** declare "3-store model" as a named concept in any normative §4 requirement — the only textual occurrence of "three-store discipline (git, Beads, JSONL)" is in §A.3 rationale (architecture.md:641) as a justification for not admitting "feature" as a fourth artifact. The 3-store model is therefore a corpus-level concept anchored informatively in architecture's rationale plus EM §4.7 state reconstruction.

BI §4.7 declares three store-authority rules:

- **BI-021** — Beads authoritative for bead content + coarse status. Scope: within Beads's owned domain.
- **BI-022** — Git authoritative for completion. Scope: Beads-vs-git disagreement on `closed` status, routes to Cat 3.
- **BI-023** — JSONL is observational only. Scope: JSONL cannot override Beads or git.

Cross-check against the corpus:

- **EM §4.7 EM-031** ("State reconstruction from git + Beads"): declares git + Beads as authoritative reconstruction source; JSONL is acknowledged as observational. **Aligned with BI-023.**
- **EM line 569** ("Every subsystem that observes this class of divergence MUST route it through [reconciliation.md §8.4 Cat 3] and [beads-integration.md §4.7 BI-022]. No subsystem may silently prefer Beads or JSONL over git."): **Aligned with BI-022 and BI-023.** Note EM v0.3.0 updated this to cite the correct BI §4.7 anchor.
- **AR §A.3 rationale** ("three-store discipline (git, Beads, JSONL)"): informative; confirms BI's three-store framing is the corpus vocabulary.
- **RC §8.4 Cat 3**: routes generic store disagreement; consumes BI-022's authority rule. Aligned.
- **RC §8.6 Cat 3c**: "Inverse premature-close (terminal-transition-without-Beads-write)" — the auto-resolver BI-022 references. Aligned, though BI cites it at the wrong anchor (`[reconciliation.md §9.2a]`).
- **EV §4.7 / §A.3**: Event stream is "observational fanout"; does not claim authority. Aligned with BI-023.

**Alignment is tight and correct.** Three nits:

1. **"3-store" is a corpus-level concept without a canonical citation target.** BI's §4.7 section header "Store-authority rules" asserts the three-store shape without citing a definitional anchor. An inline informative reference to `[architecture.md §A.3]` or `[execution-model.md §4.7]` would ground the rules' premise.
2. **BI-022's Cat 3 cite** should be RC §8.4 (Cat 3 generic) with an explicit cross to §8.6 (Cat 3c) — currently BI cites the broken `§9.2` and `§9.2a` anchors.
3. **BI-INV-003** is a near-duplicate of BI-022 (both assert git wins on completion disagreement). Per template §5 selection test (v0.3 retires duplicates of §4 requirements as invariants), BI-INV-003 should be evaluated: does it add cross-corpus quantification beyond BI-022's single-subsystem rule? Arguably yes — it asserts the rule for **every** harmonik artifact that binds to a bead, not just BI's own write path. Keep, but add an inline sensor per AR-042 (currently BI-INV-003 names "resolution MUST route through a reconciliation workflow or Cat 3 auto-resolver" — this is both the rule and the sensor; acceptable).

## 6. `br` CLI vs HC skill-injection — seam correctness

This is the architectural hinge of BI. The question is whether BI correctly delegates the skill-injection MECHANISM to HC §4.11 while owning the skill-REQUIREMENT.

### 6.1 BI's claims

- **BI-004** (§4.2): Daemon invokes `br` directly; agents invoke `br` only via the Beads-CLI skill, delivered per HC §4.11.
- **BI-027** (§4.9): Beads-CLI skill is the only agent-facing access path; documents the skill's contents; cross-refs `docs/components/external/beads.md`.
- **BI-028** (§4.9): Every agent in a harmonik run has the skill by default, per HC §4.11, with per-role exclusion recorded in YAML policy per "[control-points.md §6.5]" (should be §6.3 / §4.6).

### 6.2 HC §4.11 claims (for comparison)

- **HC-046**: Handler MUST provision every skill named in `LaunchSpec.required_skills[]`.
- **HC-047**: Skill resolution is mechanism-tagged.
- **HC-048**: Fail-launch on unresolvable required skill.
- **HC-050**: Handler consumes `LaunchSpec.required_skills[]` and `skill_search_paths[]` only; does NOT read DOT node attributes or YAML policy directly. Workflow-load-time resolution is owned by EM §4.9 and "[control-points.md §6.11]" (a broken cite within HC, mirroring BI's error).

### 6.3 CP §4.11 claims (for the full seam)

- **CP-049**: Nodes MAY declare `required_skills` as DOT attribute or `policy_ref`.
- **CP-050**: Effective skill set = union of node-level `required_skills` + role `default_skills`.
- **CP-052**: "Beads-CLI skill is the motivating default" (§4.11).
- **CP-031** (§4.6): "Default skills include the Beads-CLI skill".

### 6.4 Seam assessment

The three-spec seam is coherent:

- BI says **the skill must exist, must be the only path, must be a default for MVH roles**.
- CP says **how the skill is declared and merged** (node + role default → effective set).
- HC says **how the declared skill is resolved and provisioned** (LaunchSpec → disk search → fail-launch on miss).

No overlap, no gap. BI-028's cross-reference should land at **CP §4.11 CP-052** (declaring Beads-CLI as the motivating default) and **CP §4.6 CP-031** (the default-skills obligation). The current cite `[control-points.md §6.5]` is wrong (§6.5 is event payloads).

For the per-role exclusion surface BI-028 mentions ("unless a role-specific permission set explicitly excludes it"): CP §6.3 Policy YAML shape defines `default_skills: [<skill-name>, ...]` per role; explicit exclusion is achieved by a policy that omits `beads-cli` from a given role's default_skills, or (stronger) by an explicit "explicit-exclusion" mechanism that CP does NOT currently define. CP-031 says the Beads-CLI skill MUST be in every MVH-required role's default_skills; BI-028's "unless explicitly excluded" carve-out is **stronger than CP-031's current normative** (CP-031 admits no exclusion). This is a **cross-spec tension**:

- If BI-028's exclusion carve-out is authoritative, CP-031 must be weakened to permit it.
- If CP-031's "MUST include" is authoritative, BI-028's "unless explicitly excluded" is a design error.

Recommend: resolve in favor of CP-031 (every MVH role gets the skill; no exclusion), and rewrite BI-028 to "Every agent in a harmonik run MUST have the Beads-CLI skill available in its launch context per CP §4.6 CP-031 and CP §4.11 CP-052; exclusion is not admitted at MVH." The exclusion clause currently in BI-028 is unnecessary for the MVH envelope (no non-Beads workflows exist) and conflicts with CP's tighter obligation.

## 7. Bead-ID propagation audit

The `bead_id` threading is the cross-spec contract most dependent on BI's authority. Required threading:

| Surface | Owning spec's declaration | BI's corresponding declaration |
|---|---|---|
| `Run.bead_id` field | EM §6.1 Run record (`bead_id : BeadID \| None`) and EM §4.3 EM-014 (many-runs-per-bead) | BI-017 "Run metadata records `bead_id`" (§4.6) |
| Checkpoint trailer `Harmonik-Bead-ID` | EM §6.2 (conditional trailer) and EM §4.4 EM-017 | BI-018 "Checkpoint commits for bead-bound runs carry the bead-ID trailer" (§4.6) |
| Event payload `bead_id?` | EV §8.1.1 `run_started`; EV §8.1.7 `checkpoint_written`; EV §8.3.7 `session_log_location`; EV §8.6.8 `store_divergence_detected`; EV §8.6.10 `divergence_inconclusive` | BI-019 "Every event emitted for a bead-bound run MUST carry the optional `bead_id` field" (§4.6) |
| Session log metadata | WM §4.7 WM-025 (session-log directory), WM §6.1 Workspace record (optional `bead_id`) | BI-020 "Session logs for bead-bound runs carry `bead_id` metadata" (§4.6) |

### 7.1 Contract alignment

- **Run record**: EM owns the field presence (EM-014 declares `bead_id : BeadID | None`); BI owns the propagation rule (BI-017 asserts it MUST be populated when the run is bead-bound). **Correct split.**
- **Trailer**: EM §6.2 declares the trailer format (`Harmonik-Bead-ID | String | Conditional | Present iff the run is bead-tied (§4.3.EM-014)`); BI-018 asserts WHEN the trailer appears (conditional on bead-bound run). **Correct split.**
- **Event payloads**: **Mismatch.** EV declares `bead_id?` on **exactly five events**: `run_started` (§8.1.1), `checkpoint_written` (§8.1.7), `session_log_location` (§8.3.7), `store_divergence_detected` (§8.6.8), `divergence_inconclusive` (§8.6.10). BI §6.4 names **five events** but a different set: `run_started`, `run_completed` (§8.1.2 — no bead_id in EV payload), `run_failed` (§8.1.3 — no bead_id), `checkpoint_written`, `store_divergence_detected`. BI-019 is a **universal** claim ("Every event emitted for a bead-bound run"), which is strictly stronger than what EV's field inventory supports.

Three ways to resolve the mismatch:

1. **Widen EV**: add `bead_id?` to `run_completed`, `run_failed`, and any other bead-bound-run event. Requires an EV amendment coordinated with BI's R2 revision. Reviewer recommendation: **yes, do this** — BI's universality is the right model because the alternative (reconciliation has to join via `run_id` → `bead_id` through EM) duplicates a lookup everywhere. Widening is additive (optional field) and therefore N-1 compatible.
2. **Narrow BI-019**: restrict to the 5 EV events that currently carry `bead_id?`; drop the universality claim. This keeps BI aligned with EV today but weakens the propagation contract.
3. **Split the surface**: BI-019 declares the universality for a named subset (e.g., "run-lifecycle events and checkpoint/workspace/session-log events"); EV widens to cover that subset. Hybrid.

Recommended R2 action: Option 1 — widen EV §8.1.2 and §8.1.3 to carry `bead_id?`, fix BI §6.4 to enumerate all EV events that carry the field (add `session_log_location`, `divergence_inconclusive`; remove `run_completed` / `run_failed` from the mismatch list only if EV refuses to widen).

- **Session-log metadata**: WM §4.7 WM-025 does not currently declare an explicit `bead_id` field in the session-log metadata (`harmonik.meta.json` sidecar); WM §6.1 places the optional `bead_id` on the Workspace record. BI-020 asserts the session log's `harmonik.meta.json` carries `bead_id` metadata. This is a **co-owned contract** BI declares from one side and WM must honor. Check WM: the sidecar's `harmonik.meta.json` schema is WM-owned (§4.7 and §6.1); WM v0.3 does not explicitly declare a `bead_id` field on the sidecar. Either WM needs to add the field (simple) or BI-020 is asserting content WM does not produce.

Recommended: file OQ or amendment against WM to declare `bead_id` in the `harmonik.meta.json` sidecar schema, so that BI-020's claim has a backing declaration. Currently it is a one-sided assertion.

### 7.2 BI-INV-002 (bead-ID stability) cross-corpus coverage

BI-INV-002 asserts stability of the `bead_id` across **every** harmonik artifact that binds to a bead (run metadata, checkpoint trailers, event payloads, session-log metadata). That is a cross-corpus quantifier and correctly promoted to an invariant per AR-042. Sensor named inline: "Every harmonik artifact that binds to a bead MUST use the same ID across the entire bead lifetime. Harmonik MUST NOT mint harmonik-local alternate identifiers for the same bead." Sensor is a review-time corpus scan for "harmonik-local bead identifier" patterns. Acceptable but weak; consider strengthening with a cross-spec lint that checks every `bead_id` field declaration across the four surfaces binds to the same `BeadID` type alias from EM §6.1.

## 8. Reverse drift — inbound BI citations

BI is back-cited at `[beads-integration.md §N.N]` across the corpus. The pre-R1 convention was the legacy `§10.N` form from `docs/foundation/components.md §Component 10`. Since BI's own v0.2 numbering is §1 Purpose / §2 Scope / §3 Glossary / §4 Normative with §4.1–§4.10 requirement sub-sections, the `§10.N` anchor no longer resolves.

### 8.1 Inbound-cite audit

| Citing spec | Anchor | Target in BI v0.2.0 |
|---|---|---|
| control-points.md:295 (CP-031) | `[docs/foundation/components.md §10.9]` — Beads-CLI skill declaration | BI §4.9 (BI-027 / BI-028) |
| control-points.md:477 (CP-052) | `[docs/foundation/components.md §10.9]` | BI §4.9 |
| control-points.md:1031 (§9.3) | `[docs/foundation/components.md §10.9]` | BI §4.9 |
| reconciliation/spec.md:47 (§2.2) | `[beads-integration.md §10.8a]` — intent-log + adapter idempotency | BI §4.10 (BI-029 / BI-030 / BI-031) |
| reconciliation/spec.md:133 (Cat 3a detect) | `[beads-integration.md §10.8a]` | BI §4.10 BI-030 (intent log shape) |
| reconciliation/spec.md:135 (Cat 3a resolve) | `[beads-integration.md §10.8a]` | BI §4.10 BI-031 (audit-log recovery) |
| reconciliation/spec.md:159 (Cat 3c default) | `[beads-integration.md §10.8a]` | BI §4.10 |
| reconciliation/spec.md:236 (table) | `[beads-integration.md §10.8a]` | BI §4.10 |
| reconciliation/spec.md:331 (Cat 0 rule) | `[beads-integration.md §10.8]` — version-pin | BI §4.8 BI-024 |
| reconciliation/spec.md:362 | `[beads-integration.md §10.7]` — store-authority | BI §4.7 BI-021/BI-022 |
| reconciliation/spec.md:486 (RC-025) | `[beads-integration.md §10.8a]` | BI §4.10 |
| reconciliation/spec.md:672 (§9.3) | `[beads-integration.md §10.7]` | BI §4.7 |
| reconciliation/spec.md:673 (§9.3) | `[beads-integration.md §10.8a]` | BI §4.10 |
| reconciliation/spec.md:714 (§10.3) | `[beads-integration.md §10.8a]` | BI §4.10 |
| reconciliation/schemas.md:142 | `[beads-integration.md §10.8a]` | BI §4.10 |
| reconciliation/schemas.md:158 | `[beads-integration.md §10.8a]` | BI §4.10 |
| process-lifecycle.md:789 (revision history) | lists `[beads-integration.md §10.8] / §10.9 → §4.10 / §4.9` as DONE for PL's own outbound | Sibling evidence of the migration map |

PL v0.3.0 has already migrated its BI outbound cites (§12 line 789). EM v0.3.0 has migrated. WM v0.3.0 has migrated (per WM §12). The unresolved debt is in CP (3 sites at §10.9, all bootstrap-cited) and RC (13 sites at §10.7 / §10.8 / §10.8a, all legacy-anchor).

### 8.2 Recommended §A.4 migration map (publish as WM v0.3 did)

Template: add a new §A.4 "Reverse-drift migration map — §10.x legacy → §4.x current" to BI at R2, with the table:

| Legacy `§10.N` anchor (components.md §10) | Current BI v0.2 anchor | Content |
|---|---|---|
| `§10.1 Selection (SQLite fork)` | `§4.1` (BI-001) | Beads SQLite fork adoption |
| `§10.2 Access model (`br` CLI)` | `§4.2` (BI-002, BI-003, BI-004) | CLI-only access; no MCP |
| `§10.3 Beads-managed data` | `§4.3` (BI-005..BI-009) | Bead content, edges, status, ID stability, atomic claim |
| `§10.4 Harmonik write surface` | `§4.4` (BI-010, BI-011, BI-012) | Terminal transitions only |
| `§10.5 Read surface` | `§4.5` (BI-013..BI-016) | Ready, dependency, detail, reconciliation queries |
| `§10.6 Bead-ID propagation` | `§4.6` (BI-017..BI-020) | Run metadata, trailer, event, session-log |
| `§10.7 Store-authority rules` | `§4.7` (BI-021, BI-022, BI-023) | Beads/git/JSONL authority |
| `§10.8 Version-pin + adapter layer` | `§4.8` (BI-024, BI-025, BI-026) | Pin per release; adapter-absorbs-breakage |
| `§10.8a Adapter idempotency` | `§4.10` (BI-029..BI-032) | Intent log + audit-log recovery |
| `§10.9 Beads-CLI skill` | `§4.9` (BI-027, BI-028) | Default skill for MVH roles |

Add an inbound-count note: "Known inbound citation counts requiring migration (per R1 cross-spec-architect audit, 2026-04-24): reconciliation/spec.md (11), reconciliation/schemas.md (2), control-points.md (3). Total 16 inbound cites across 3 spec files. EM, WM, PL have already migrated."

## 9. Bootstrap citations

Audited BI for `[docs/foundation/components.md §N]` leftovers.

- **BI body**: zero `components.md §N` active citations.
- **BI §1 Purpose** line 24: `[docs/foundation/problem-space.md §Locked decisions]` — permitted per template bootstrap rule.
- **BI §9.3** line 436: same problem-space reference — permitted.
- **BI §12 revision history** line 494: "Initial draft from components.md §10" — provenance log, not a normative citation; acceptable.

**BI is clean on the bootstrap-citation closure criterion.** This is the one area where BI's v0.2 cleanup pass succeeded — the `components.md §10` bootstrap has been removed. The only `components.md` cite that remains is the one CP v0.2 still carries at CP-031 / CP-052 / §9.3 (CP bootstrap debt, not BI's).

## 10. Recommended graph edits

Priority-ordered for R2. Grouped by change class.

### 10.1 Blocking (must fix before R2 completes)

**R1. Front-matter `depends-on` expansion.** Add `architecture, handler-contract, control-points, workspace-model, process-lifecycle, operator-nfr, reconciliation` to BI front-matter. All seven are currently normatively cited in §9.1 or body requirements. Precedent: WM v0.3.0 front-matter carries the same 7-peer expansion. Without this, AR-022 `foundation-version` compatibility cannot be verified and the BI envelope does not match the cross-spec citation surface.

**R2. Mechanical anchor fix: 39 broken cites.** Full table from §3 above. The minimal R2 pass is a non-semantic edit batch:

- 8 × `[event-model.md §3.2]` → `[event-model.md §8.1.1]` / `§8.1.7` / `§8.3.7` / `§8.6.8` / `§6.3` (per-site target) or `§8` (when referring to the taxonomy)
- 1 × `[event-model.md §3.4]` → `[event-model.md §4.4 EV-016]` (fsync durability)
- 6 × `[control-points.md §6.11]` → `[control-points.md §4.11]`
- 2 × `[control-points.md §6.5]` → `[control-points.md §6.3]` (YAML) + `[control-points.md §4.6 CP-031]` (role-default obligation)
- 12 × `[reconciliation.md §9.N]` → `[reconciliation/spec.md §8.4]` / `§8.4a` / `§8.6` / `§8.12` / `§4.3` / `§4.5` (per-site target) — including path change
- 5 × `[workspace-model.md §5.N]` → `[workspace-model.md §4.N]` per WM §A.4 map
- 3 × `[process-lifecycle.md §8.N]` → `[process-lifecycle.md §4.N]`; also correct BI-016's inline "steps 3–4" to "steps 6–7"
- 5 × `[operator-nfr.md §7.N]` → `[operator-nfr.md §4.N]`; drop or OQ-ify the §7.8 throughput-bounds cite

**R3. BI-028 vs CP-031 reconciliation.** BI-028 currently admits per-role exclusion of the Beads-CLI skill; CP-031 admits no exclusion at MVH. Resolve in CP's direction: rewrite BI-028 as "Every agent in a harmonik run MUST have the Beads-CLI skill in its launch context per [control-points.md §4.6 CP-031] and [control-points.md §4.11 CP-052]; per-role exclusion is not admitted at MVH and would require a foundation amendment per [architecture.md §4.6]." Drop the "unusual policy decision" carve-out.

**R4. BI-019 / §6.4 event inventory completion.** BI §6.4 names 5 events; EV §8 declares `bead_id?` on 5 events but a partially different set. Resolve by:
- File coordinated EV amendment to add `bead_id?` to `run_completed` (§8.1.2) and `run_failed` (§8.1.3); OR
- Narrow BI §6.4 to the 5 EV events that currently carry the field: `run_started`, `checkpoint_written`, `session_log_location`, `store_divergence_detected`, `divergence_inconclusive`. Drop `run_completed` and `run_failed` from BI §6.4 or track as OQ.
- Regardless of outcome, rewrite BI-019 from the universal-claim form ("Every event emitted for a bead-bound run") to the enumerated-surface form naming the events by EV §8.N row reference.

**R5. BI-020 vs WM sidecar schema.** File OQ or coordinated amendment against WM to declare `bead_id` on `harmonik.meta.json`. Currently BI-020 asserts content WM-025 / WM §6.1 does not produce. Alternatively, demote BI-020 to "Session logs for bead-bound runs carry `bead_id` via the Workspace record per [workspace-model.md §6.1]" and drop the metadata-sidecar wording.

### 10.2 Strongly recommended (should fix at R2)

**R6. Publish §A.4 reverse-drift migration map.** Table in §8.2 above. 16 inbound cites identified across RC + CP. Publication is the signal to downstream specs that the map is authoritative.

**R7. Three-store informative anchor.** Add to BI §4.7 header: "The store-authority rules below are BI's instantiation of the corpus-level three-store discipline informatively grounded in [architecture.md §A.3 Rationale] and [execution-model.md §4.7]."

**R8. Ownership citation in §4.10.BI-032.** Split the current single cite `[reconciliation.md §9.3]` into two: (a) `[reconciliation/spec.md §4.3 RC-013]` for the Cat 3a detection rule body, (b) `[reconciliation/spec.md §8.4a]` for the Cat 3a category definition.

**R9. AR-INV-001 sensor alignment.** BI's `Tags:` lines carry `mechanism` throughout (correct — BI is pure mechanism). No `cognition`-tagged requirements to check. BI does not violate AR-INV-001.

**R10. Spec-category declaration.** BI front-matter is missing `spec-category` per AR-052. Per AR-052 examples, BI is `foundation-cross-cutting` (like ON, RC, AR itself). Add the field.

**R11. Spec-template envelope exemption confirmation.** AR-053 exempts foundation-cross-cutting specs from §4.a envelope declaration. BI as foundation-cross-cutting does not need a §4.a. Confirm via inline note in §4 or at §10.1 that BI is exempt per AR-052/AR-053. This answers OQ-AR-007 (mechanical spec-category test) in BI's favor.

### 10.3 Nice-to-have (defer if R2 is tight)

**R12. Extract a "surface-ownership" table into §2.3.** WM v0.3 uses a tabular format that pairs every cross-spec surface BI touches with the owning spec and the direction (BI owns / BI reads / BI co-owns). This would make the scope discipline machine-checkable at R3 and help reviewers cross-audit.

**R13. Explicit re-declaration of BI-INV-003 sensor.** The sensor wording in BI-INV-003 ("Resolution MUST route through a reconciliation workflow or Cat 3 auto-resolver") is also the rule. Separate them: "Rule: git wins on completion disagreement. Sensor: reviewer-enforced scan for any auto-reconciliation-into-git's-direction code path without a Cat 3 dispatch."

**R14. Disambiguate BI-INV-002 vs BI-008.** BI-008 says "A bead's ID MUST be stable from creation to tombstone" (single-subsystem rule about Beads); BI-INV-002 says "A bead's ID is stable across harmonik's lifetime" (cross-corpus quantifier over harmonik artifacts). Per template §5 retention rule, the invariant is retained when the quantifier differs. Acceptable, but the relationship would read better if BI-INV-002 explicitly cites "extending BI-008's Beads-side stability across harmonik's binding surfaces."

**R15. §10.3 throughput-bound claim.** BI §10.3 cites `[operator-nfr.md §7.8]` for "performance or throughput bounds on `br` invocation". ON has no such section. Either file OQ-ON against ON to add a `br`-throughput observable (reconciliation-relevant because timeouts drive Cat 0), or rewrite BI §10.3 to "This spec does NOT declare any performance or throughput bound on `br` invocation; no foundation spec does at MVH."

### 10.4 Cross-spec amendments (out of BI's direct edit, flagged)

The following issues surfaced during BI's audit are problems in other specs; BI does not need to edit them:

- **HC-050 line 490**: cites `[control-points.md §6.11]` which does not exist; should be `[control-points.md §4.11]`. Report to HC R2.
- **EM line 569**: correct cite of BI at `[beads-integration.md §4.7 BI-022]`. Already migrated; no action.
- **CP-031 / CP-052 / CP §9.3**: cite `[docs/foundation/components.md §10.9]` as bootstrap. When BI's R2 lands with §A.4 published, CP can migrate to `[beads-integration.md §4.9 BI-027]`. Flag for CP R2.
- **RC-025 line 486, RC-013 lines 133–159, RC §9.3 lines 672–673, schemas.md**: 13 inbound cites at BI `§10.7 / §10.8 / §10.8a`. Flag for RC R2; §A.4 map in BI (R6 above) unblocks the migration.
- **EV §8.1.2 and §8.1.3**: add `bead_id?` to `run_completed` and `run_failed` payloads per R4 above (if the universality model is accepted).
- **WM-025 / WM §6.1**: add `bead_id` to `harmonik.meta.json` sidecar schema per R5 above.

---

## Appendix A: Citation-resolution table (for the R2 edit batch)

Paste-ready mapping for the R2 mechanical fix. Format: `current` → `correct`.

| Current cite | Correct cite | BI site(s) |
|---|---|---|
| `[event-model.md §3.2]` (event taxonomy) | `[event-model.md §8]` | §2.2 |
| `[event-model.md §3.2]` (event payload shape) | `[event-model.md §6.3]` or per-event `§8.N.M` | §4.4 BI-011; §4.6 BI-019; §6.4 header; §6.4 closing; §9.1 |
| `[event-model.md §3.4]` (fsync durability) | `[event-model.md §4.4 EV-016]` | §4.10 BI-030; §9.1 |
| `[control-points.md §6.11]` (skill declaration) | `[control-points.md §4.11]` | §2.2; §4.2 BI-004; §4.9 BI-027; §4.9 BI-028; §9.1 |
| `[control-points.md §6.5]` (role exclusion policy) | `[control-points.md §4.6 CP-031]` + `[control-points.md §6.3]` | §4.9 BI-028; §9.1 |
| `[reconciliation.md §9.2]` (Cat 3 taxonomy) | `[reconciliation/spec.md §8.4]` | §4.7 BI-022; §5 BI-INV-003; §9.3 |
| `[reconciliation.md §9.2a]` (Cat 3a/3c auto-resolver) | `[reconciliation/spec.md §8.12]` + `§8.4a` / `§8.6` | §4.7 BI-022; §4.10 BI-032; §9.3 |
| `[reconciliation.md §9.3]` (Cat 3a detector) | `[reconciliation/spec.md §4.3]` + `§8.4a` | §2.2; §4.10 BI-032; §9.3 |
| `[reconciliation.md §9.3a]` (JSONL divergence-evidence) | No direct target; closest is `[reconciliation/spec.md §4.3 RC-013]` + `[event-model.md §8.6.8]` | §4.7 BI-023 |
| `[reconciliation.md §9.5]` (verdict vocabulary) | `[reconciliation/spec.md §4.5]` | §4.4 BI-010; §9.3 |
| `[workspace-model.md §5.3]` (session-log metadata) | `[workspace-model.md §4.7]` | §4.6 BI-020; §9.3 |
| `[workspace-model.md §5.8]` (branching, merge target) | `[workspace-model.md §4.2]` + `[workspace-model.md §4.5]` | §4.4 BI-010; §4.5 BI-014; §9.3 |
| `[process-lifecycle.md §8.1]` (single-machine per-project) | `[process-lifecycle.md §4.1 PL-001]` | §4.1 BI-001 RATIONALE |
| `[process-lifecycle.md §8.2]` (startup sequence) | `[process-lifecycle.md §4.2 PL-005]`; fix "steps 3–4" to "steps 6–7" | §4.5 BI-016; §9.3 |
| `[operator-nfr.md §7.4]` (queue-format) | `[operator-nfr.md §4.4 ON-015]` | §9.3 |
| `[operator-nfr.md §7.5]` (schema compat N-1) | `[operator-nfr.md §4.5 ON-018]` | §6.1 IntentLogEntry; §6.3; §9.3 |
| `[operator-nfr.md §7.8]` (throughput bounds) | No target; drop or OQ-ify | §10.3 |
| `reconciliation.md` (filename) | `reconciliation/spec.md` | all RC cites |

## Appendix B: Scorecard

| Criterion | State |
|---|---|
| Verdict | Changes required (blocking R2) |
| Dependency graph — cycle risk | None |
| Front-matter `depends-on` completeness | Incomplete (2 of 8 peers listed) |
| §9.1 depends anchors | 5 of 8 broken |
| §9.3 co-ref anchors | 9 of 10 broken |
| Body inter-spec anchors (total) | 39 broken |
| Scope leak detection | No leaks |
| 3-store alignment | Aligned (nits only) |
| `br` CLI / HC §4.11 seam | Correct, but cite the right CP section and resolve BI-028 / CP-031 tension |
| Bead-ID propagation | 4-surface contract; 1 mismatch with EV §8, 1 mismatch with WM sidecar schema |
| Reverse-drift map published | No (should publish at R2) |
| Bootstrap citations (components.md) | Clean (only history log) |
| `spec-category` front-matter | Missing; should be `foundation-cross-cutting` |
| §4.a envelope | Exempt (foundation-cross-cutting) |
| Cognition-tagged requirements | Zero — BI is pure mechanism (clean on AR-INV-001) |
| Total broken or wrong-path cites | 39 in body + 16 inbound reverse-drift sites = 55 coordinated sites needing R2 edits across BI + CP + RC |

The spec's substance is sound and the scope discipline is clean — BI's R2 is primarily a citation-correctness pass plus three substantive tightenings (BI-028 / CP-031, BI-019 / EV §8, BI-020 / WM sidecar).
