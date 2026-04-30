# EV pilot r1 — Reference review

`reviewer: reference-r1` · drafted 2026-04-28 against `docs/decompose-to-tasks/ev-pilot.md` v0.1.0, `specs/event-model.md` v0.3.3 (status `reviewed`), `docs/decompose-to-tasks/discipline.md` v0.7. Self-contained.

EV's `depends-on` is `[architecture, execution-model, handler-contract, workspace-model]`. HC and WM are not yet drafted; HC/WM-targeted edges are forward-deferred per F-pilot-EM-2 / Option B precedent. Targets in the universe `{ar-NNN, em-NNN}` materialize as live `blocks` edges.

Method per pilot-review-protocol §3.3:

1. Walked EV §3 (glossary), §4 (normative), §5 (invariants), §6 (schemas), §7 (protocols — no edges per discipline §3.1 v0.7), §8 (taxonomy), §9 (cross-references — §9.1 `depends-on` enumerated; §9.3 co-references no-edge).
2. Walked the pilot's §2 / §3 / §4 / §4a tables and §5 cross-spec edge summary.
3. Cross-checked emitted edges against inline cites.
4. Verified §4.a envelope rule (EV is post-AR-053 per discipline v0.7 §3.2 — does NOT receive carve-out).
5. Spot-checked intra-EV bidirectional resolutions and the `ev-inv-001 → em-inv-001` invariant-as-target edge.

---

## 1. Inline-cite enumeration (cross-spec) in EV §3 / §4 / §5 / §6 / §8

Bucketed by classification per discipline §3.1.

### 1.1 Normative-prose cites — eligible for edges

| Citing site | Cite | Bucket | Pilot disposition |
|---|---|---|---|
| §3 glossary `TransitionKind` | `[execution-model.md §4.10 EM-044]`, `[execution-model.md §6.1]` | Glossary anchors §6.1 type, used only by §8 row payloads | Pilot: not directly emitted; resolves through `ev-events.transition-event → em-schema.transition-kind` and `ev-events.transition-event → em-004` (Transition record). **No-edge from glossary itself per discipline §3.1 step 5 — definitional pointer.** |
| §3 glossary `FailureClass` | `[execution-model.md §8]` | Same | Resolved through `ev-events.run-failed → em-error.taxonomy`. |
| §3 glossary `ErrorCategory` | `[handler-contract.md §4.5]` | Same | Forward-deferred (HC); resolved through `forward:hc-error-category` on §8.3 / §8.6.5 / §8.8.2 row beads. |
| §4.1 EV-001 body | `[architecture.md §4.5]` | Section anchor (no req ID) | Pilot reclassifies as **supporting cite** per F-pilot-AR-10 — see §1.4 below. |
| §4.1 EV-004 body | `[architecture.md §4.4]` | Section anchor | Pilot emits `ev-004 → ar-013` per term-use rule. |
| §4.1 EV-005 body | `[workspace-model.md §4.7]` | Section anchor (forward-deferred) | Pilot emits `forward:wm-session-log`. |
| §4.3 EV-011 body | `[operator-nfr.md]` | Whole-spec ref (forward; ON not in `depends-on`) | Pilot logs as forward-cite finding (F-pilot-EV-3); reclassified as supporting in §5.6. |
| §4.5 EV-017 body | `[execution-model.md §4.7]` | Section anchor | Pilot emits `ev-017 → em-031` (term-use). |
| §4.5 EV-021 body | `[execution-model.md §4.7]` | Section anchor | Pilot emits `ev-021 → em-031`. |
| §4.5 EV-023 body | `[reconciliation/spec.md §4.3]` | Section anchor (forward-deferred; RC not in `depends-on`) | Pilot logs as forward-cite finding (F-pilot-EV-3). |
| §4.6 EV-027 body | `[architecture.md §4.6]` | Section anchor | Pilot emits `ev-027 → ar-019`. **See §3 below — this is wrong; the AR §4.6 amendment protocol is owned by AR-020 (proposal) / AR-021 (authority) / AR-023 (overlap), NOT AR-019 (post-MVH process geometry).** |
| §4.7 EV-029 body | `[operator-nfr.md §4.3]` | Section anchor (forward) | Pilot logs as forward-cite finding; reclassified as supporting in §5.6. |
| §4.10 EV-035 body | `[handler-contract.md §4.7]` | Section anchor (forward) | Pilot emits `forward:hc-redaction-registry`. |
| §5 EV-INV-001 body | `[execution-model.md §4.7 EM-INV-001]` | **Specific invariant ID cite** | Pilot emits `ev-inv-001 → em-inv-001` (invariant-as-target edge per §2.5 F10). See §6 below for whether this is allowed under v0.7. |
| §6.2 read-recovery rules | `[reconciliation/spec.md §8]` (Cat 6 escalation) | Section anchor (forward) | Pilot emits `forward:rc-cat-6` on `ev-schema.jsonl-line`. |
| §6.3 `run_failed` payload note | `execution-model.md §8`, `handler-contract.md §4.5` | Inline (in §6.3 prose) | Resolved through row-bead edges: `ev-events.run-failed → em-error.taxonomy` and `forward:hc-error-category`. |
| §6.3 `transition_event` payload note | `execution-model.md §6.1` | Section anchor | Resolved through `ev-events.transition-event → em-schema.transition-kind`. |
| §6.3 `agent_output_chunk` payload note | `[handler-contract.md §4.1]` | Section anchor (forward) | Resolved through `forward:hc-session-id` on §8.3 row beads. |
| §6.3 `divergence_kind` post-MVH note | `[beads-integration.md §4.10]`, OQ-BI-008 | Forward (not in `depends-on`) | Logged as forward-cite finding (F-pilot-EV-3). |
| §6.3 `daemon_ready` / `daemon_shutdown` note | `[operator-nfr.md §4.8 ON-033]` | **Specific req ID cite** (forward; ON not in `depends-on`) | Logged as forward-cite finding. |
| §6.3 `daemon_degraded.reason` note | `[reconciliation/spec.md §4.2 RC-012a]` (cat0_post_ready) | **Specific req ID cite** (forward; RC not in `depends-on`) | Logged as forward-cite finding. |
| §6.3 `daemon_degraded.reason` note | `[operator-nfr.md §4.9 ON-040]` (silent_hang_aggregate) | Specific req ID (forward) | Logged. |
| §6.4 breaking-change table prose | `[operator-nfr.md §4.3]`, `[operator-nfr.md §4.5]` | Section anchors (forward) | Logged. |
| §6.5 co-ownership map | Various (`[execution-model.md §6.5]`, `[control-points.md §6.5]`, `[handler-contract.md §4.1, §4.9, §4.11, §7.1]`, `[control-points.md §4.5]`, `[workspace-model.md §4.4, §4.5]`, `[reconciliation/spec.md §4.1, §4.3, §4.5]`, `[operator-nfr.md §6.5, §7.3]`, `[process-lifecycle.md §6.2, §8.6]`) | Section anchors | These are emission-rule pointers; they translate to row-level cross-spec edges on the §8 row beads (e.g., §8.1 → EM-015a/015b; §8.5 → WM-forward; §8.6 → RC-forward; §8.7 → PL/ON-forward). |
| §8.1.8 `outcome_emitted` row | `[execution-model.md §4.1 EM-005]` | **Specific req ID** | Pilot emits `ev-events.outcome-emitted → em-005` and `→ em-schema.outcome-status`. ✓ |
| §8.5.5 `workspace_interrupted` row | `[reconciliation/spec.md §8]` (RC detector) | Section anchor (forward) | Pilot emits `forward:rc-cat-6`. |
| §8.6.3 `reconciliation_verdict_emitted` row | `[reconciliation/spec.md §4.5]` | Section anchor (forward) | Pilot emits `forward:rc-verdict-vocab`. |
| §8.6.14 post-MVH note | `[operator-nfr.md §4.9 ON-035]` (forward) | Specific req ID | Logged as forward-cite finding; row carries `post-mvh` tag (F-pilot-EV-2). |
| §8.7.4 `daemon_startup_failed` row | `[operator-nfr.md §8]` | Section anchor (forward) | Pilot emits `forward:on-failure-modes`. |

### 1.2 Normative-prose cites in §1, §2.1, §2.2

§1 Purpose and §2.1/§2.2 Scope contain inline cites. Per discipline §3.1 v0.7 explicit no-edge list, §1/§2 prose is not enumerated but discipline practice (per AR/EM pilots) treats §2.2 out-of-scope cites as scope-pointers, not load-bearing dependencies. The cites at:

- §1 line 26: `[execution-model.md §4.7]`, `[workspace-model.md §4.7]` — scope-pointer prose; resolved to EM-031 via the §4.5 normative cite (already covered).
- §2.2 lines 46/49: `[execution-model.md §4.7]`, `[reconciliation/spec.md §8, §4.4, §4.5]` — out-of-scope pointers.

**No additional edges fire from §1/§2.** Pilot correctly does not emit any.

### 1.3 §9.1 `depends-on` cites

§9.1 enumerates AR / EM / HC / WM section anchors (lines 935-946). Per discipline §2.7 + §3.1 v0.7, §9.1 itself does not generate edges; edges are derived from the body cites in §3/§4/§5/§6/§8 (which are the per-cite enumeration above). **No additional edges.** Pilot correctly does not double-count.

### 1.4 Supporting-cite reclassifications (per discipline §3.1 step 1, F-pilot-AR-10)

The pilot reclassifies three cites as supporting (no edge):

1. **EV-001 → `[architecture.md §4.5]`.** Operational test: removing the AR §4.5 cite leaves "envelope MUST carry `source_subsystem` (Go-package-identifier string)" as an independently testable string-shape claim. **Reclassification holds.** ✓
2. **EV-029 → `[operator-nfr.md §4.3]`.** Removing the cite leaves "Readers MUST accept N-1" testable (reader contract); migration scheduling is operational. **Holds + forward.** ✓
3. **EV-011 → `[operator-nfr.md]`.** Removing leaves "queue depth MUST be bounded (default 1024)" testable. **Holds + forward.** ✓

Reviewer concurs with all three reclassifications.

### 1.5 Glossary-as-no-edge (additional reviewer observation)

The §3 glossary cites for `TransitionKind`, `FailureClass`, `ErrorCategory` (lines 66-68) are definitional pointers, not normative cites. Per discipline §3.1 step 5 invariant-as-target precedence, glossary-anchor cites do not themselves emit edges; they are realized through the §6.3 / §8 cites that USE the term. Pilot correctly handles this. ✓

---

## 2. Pilot edge enumeration cross-check

### 2.1 To AR — claimed 7 emitted edges

Per pilot §5.1:

| Pilot edge | Source cite (verified) | Verdict |
|---|---|---|
| `ev-004 → ar-013` | EV-004 body cites `[architecture.md §4.4]` (line 251); AR §4.4 anchors AR-013 / AR-014. Term-use: "subsystem identifiers declared in each subsystem's envelope". Single-owner: AR-013 declares the envelope shape that the identifiers fill. | **OK.** Edge correctly emitted via §3.1 step 5 term-use (single-owner cross-spec). |
| `ev-027 → ar-019` | EV-027 body cites `[architecture.md §4.6]` (line 462); AR §4.6 anchors **AR-020 (amendment-proposal procedure), AR-021 (amendment authority), AR-022 (foundation versioning), AR-023 (parallel-amendment serialization)**. AR-019 is in §4.5 ("Post-MVH process geometry is out of foundation scope") — NOT in §4.6 at all. | **MAJOR — phantom anchor.** AR-019 is **not the foundation amendment protocol**. The cite "via the foundation amendment protocol ([architecture.md §4.6])" pins to AR-020 (the proposal procedure) per term-use rule. The pilot's §5.1 row 4 says "AR §4.6 anchors AR-019 (amendment-protocol rule)" — this is an incorrect mapping. The correct edge is **`ev-027 → ar-020`** (single-owner of "amendment-proposal procedure"). Lane: `local`. Severity: MAJOR (would point at the wrong target bead at edge-fire time). |
| `ev-031 → ar-001` | EV-031 body cites four-axis tags `(llm-freedom, io-determinism, replay-safety, idempotency)`; AR-001 owns the four-axis classification rule. | **OK.** Term-use, single-owner. ✓ |
| `ev-031 → ar-005` | EV-031 body cites `mechanism \| cognition` tag "per SC-10"; pilot's F-pilot-EV-5 reclassifies to AR-005 (current canonical owner). | **OK on the term-use mapping** — AR-005 owns "Every evaluation point is tagged mechanism or cognition". The "per SC-10" anchor is stale; F-pilot-EV-5 correctly logs the spec-edit TODO. ✓ |
| `ev-034a → ar-013` | EV-034a body grounds in EV-004's AR §4.4 cite; AR-013 declares envelope-element registration. | **OK.** Term-use, single-owner. ✓ |
| `ev-events.agent-started → ar-027` | §8.3.2 row payload field `agent_type` (per AR-MIG-001 rename); AR-027 owns "Cross-subsystem agent-type reference points" (the four surfaces that name `agent_type` byte-for-byte). | **OK.** Term-use, single-owner. ✓ |
| `ev-events.session-log-location → ar-027` | §8.3.7 row payload field `agent_type`. | **OK.** Same pattern. ✓ |

**Missing AR edge candidate (potential MAJOR):** EV-001 cites `[architecture.md §4.5]` (line 215 — `source_subsystem (an opaque Go-package-identifier string per [architecture.md §4.5])`) and the pilot reclassifies as supporting per F-pilot-AR-10. Reviewer concurs the reclassification holds (see §1.4 above). **No additional edge.**

### 2.2 To EM — claimed 21 emitted edges

Per pilot §5.2 (3 from §4.5 replay reqs + 18 from §8 row beads):

**§4.5 replay reqs:**

| Pilot edge | Source cite (verified) | Verdict |
|---|---|---|
| `ev-017 → em-031` | EV-017 body line 378: "git plus Beads is authoritative for state per [execution-model.md §4.7]"; EM-031 owns "State reconstruction uses git plus Beads only" (line 377). | **OK.** Term-use, single-owner. ✓ |
| `ev-021 → em-031` | EV-021 body line 411: same anchor + same term-use ("Authoritative state-reconstruction source is git plus Beads"). | **OK.** ✓ |
| `ev-022 → em-031` | EV-022 body lines 416-419: "daemon's startup state-reconstruction path MUST walk git plus query Beads; it MUST NOT read JSONL". The cite to EM §4.7 is implicit (mirror of EV-017/021). | **OK.** ✓ |

**§8 row beads — sampled and verified:**

| Pilot edge | Source cite | Verdict |
|---|---|---|
| `ev-events.run-started → em-015a` | §6.5 line 857 ("Run-lifecycle events (§8.1): emission rules in [execution-model.md §6.5]"); EM-015a owns "run_started emission on dispatch" (line 192). | **OK.** §6.5 co-ownership pointer + EM-015a single-owner. ✓ |
| `ev-events.run-completed → em-015b` | EM-015b owns `run_completed`/`run_failed` emission (line 199). | **OK.** ✓ |
| `ev-events.run-failed → em-015b` | Same. | **OK.** ✓ |
| `ev-events.run-failed → em-error.taxonomy` | §6.3 `run_failed` payload note (line 680): "failure_class: <FailureClass> # coarse bucket per execution-model.md §8". EM §8 is the taxonomy. | **OK.** Term-use of FailureClass / EM §8. ✓ |
| `ev-events.state-entered → em-003` | Payload term-use of State / `state_id`. EM-003 owns `State` (line 91). | **OK.** ✓ |
| `ev-events.state-exited → em-003`, `→ em-004` | Payload uses State + Transition. EM-004 owns Transition (line 97). | **OK.** ✓ |
| `ev-events.transition-event → em-004`, `→ em-schema.transition-kind`, `→ em-016` | §3 glossary cites EM-044 + §6.1 for TransitionKind; §6.3 payload notes commit_hash. The TransitionKind enum is owned by EM §6.1 → the schema bead `em-schema.transition-kind`. EM-016 owns Checkpoint commit_hash semantics. | **OK on `→ em-schema.transition-kind`** (correct target — the enum is a §6.1 schema). **OK on `→ em-016`** (commit_hash term-use). **The `→ em-004` edge is also correct** (Transition record). The pilot's §3 glossary cite to EM-044 (TransitionKind enum) routes through the schema bead, which is the right target per discipline §3.1.4. ✓ |
| `ev-events.checkpoint-written → em-016`, `→ em-018` | §8.1.7 payload uses commit_hash (EM-016); transition_id implies sibling-file path semantics (EM-018). | **OK on `→ em-016`** (commit_hash). **The `→ em-018` edge is borderline** — EM-018 owns the sibling-file path; `checkpoint_written` payload doesn't directly cite that path, but the `transition_id` field cross-references EM-004's Transition record which is sibling-file-based. Reviewer judgment: edge is defensible under §3.1 step 5 term-use of transition_id; minor over-gating is preferred per F4. **OK.** ✓ |
| `ev-events.outcome-emitted → em-005`, `→ em-schema.outcome-status` | §8.1.8 row body explicitly cites `[execution-model.md §4.1 EM-005]` (line 85). | **OK on `→ em-005`** (specific req ID cite — most precise inline cite in EV's §8). **`→ em-schema.outcome-status`** is the OutcomeStatus enum schema which lives in EM §6.1; pilot row description says "Outcome + OutcomeStatus enum"; the term `outcome_status` (the field) is the §4.1 EM-005-owned status enum. **Note:** there is no separate `OutcomeStatus` enum in EM (EM-005 declares `status ∈ {SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS}` inline; EM §6.1 has `Outcome` RECORD and `OutcomeKind` ENUM but NOT `OutcomeStatus`). The schema bead target name `em-schema.outcome-status` may not exist in EM's bead set; the actual schema bead is `em-schema.outcome` (the Outcome RECORD). **MINOR — bead-target-name verification.** Lane: `local`. Severity: MINOR (target-bead-name mismatch; `em-schema.outcome` is the resolvable target). |
| `ev-events.sub-workflow-entered → em-034`, `→ em-036` | §8.1.9 row uses `parent_node_id`, `sub_workflow_name`. EM-034 owns sub-workflow expansion; EM-036 owns sub-workflow lifecycle events. | **OK.** ✓ |
| `ev-events.sub-workflow-exited → em-034`, `→ em-036`, `→ em-036a` | Plus EM-036a (sub-workflow terminal outcome). | **OK.** ✓ |
| `ev-events.node-dispatch-requested → em-006` | §8.1.11 row uses `node_id`. EM-006 owns Node. | **OK.** ✓ |
| `ev-events.budget-exhausted → em-error.taxonomy` | §8.4.3 payload uses `budget_exhausted` failure-class (per EM §8 FailureClass). | **OK.** Term-use of EM §8. ✓ |

**Pilot's edge count audit:** Pilot claims "21 emitted EM edges (3 + 18)". Counting unique edges from the verified table:
- §4.5: 3 (ev-017, ev-021, ev-022 → em-031). ✓
- §8.1.1: 1 (em-015a). §8.1.2: 1 (em-015b). §8.1.3: 2 (em-015b, em-error.taxonomy). §8.1.4: 1 (em-003). §8.1.5: 2 (em-003, em-004). §8.1.6: 3 (em-004, em-schema.transition-kind, em-016). §8.1.7: 2 (em-016, em-018). §8.1.8: 2 (em-005, em-schema.outcome-status). §8.1.9: 2 (em-034, em-036). §8.1.10: 3 (em-034, em-036, em-036a). §8.1.11: 1 (em-006). §8.4.3: 1 (em-error.taxonomy).
- Subtotal §8: 21 (counting `em-error.taxonomy` once for §8.1.3 + §8.4.3 — it's the same target bead, but reachable from two row beads = two edges; pilot's "18" appears to count distinct (citing-bead, target-bead) pairs).

Recount with unique pairs: 1+1+2+1+2+3+2+2+2+3+1+1 = **21 §8 row pair edges**. Plus 3 §4.5 edges = **24 total emitted EM edges**.

**MINOR — tally inconsistency.** Pilot §5.2 says "Total emitted cross-spec edges to EM (counting unique edges): 21 (3 from §4.5 replay reqs + 18 from §8.1 / §8.4 row beads)". The body of §5.2 enumerates 17 row entries (rows 4-16 in §5.2 list, plus rows 17-18 are intra-spec cross-listings); the actual emitted edge count from the §4a row table is 21. The "18" in §5.2 prose is undercounted. Lane: `local`. Severity: MINOR (cosmetic tally; doesn't affect edge correctness).

### 2.3 Forward-deferred edges (HC and WM)

**HC (~30 forward):** §8.3 (13 rows) × ~3 edges each (`forward:hc-handler-watcher`, `forward:hc-session-id`, per-row `forward:hc-<event-name>`), plus EV-035 → `forward:hc-redaction-registry`, plus 5-6 §8.6 / §8.8 / §8.1.3 rows that reference `error_category`. Spot-check: every §8.3 row carries `forward:hc-session-id` per pilot (line 196), and HC §4.1 owns session_id per §6.3 cross-ref. **All forward-deferred placeholders correctly logged in §5.3.** ✓

**WM (~6 forward):** EV-005 → `forward:wm-session-log` + 5 §8.5 rows. **OK.** ✓

### 2.4 Forward-cite findings (non-`depends-on` targets)

§5.5 logs ~32 occurrences across RC / BI / ON / PL / CP. Spot-check verified count per target:
- RC: ~10 ✓ (EV-023 §4.3, §6.2 Cat 6, §8.5.5, §8.6 family, §6.3 RC-012a, §9.3 entries).
- BI: ~3 ✓ (§6.3 OQ-BI-008, §8.6.14, §9.3).
- ON: ~7 ✓ (EV-011 default, EV-029 §4.3, §6.4 §4.5, §8.7.4 §8, §6.3 ON-033, §8.7.5 ON-040, §8.6.14 ON-035, §8.8.5 ON-022).
- PL: ~3 ✓ (§9.3 PL §6.2/§8.2, §8.7 family).
- CP: ~9 — **possible overcount.** EV-031 cites "per SC-10" (a stale anchor that pilot resolves to AR-005, NOT to CP). The pilot's "EV-031 cite to 'SC-10' (stale, → AR-005)" is correctly attributed to AR; CP's section count should drop the SC-10 entry. CP ~8.

**MINOR — forward-cite tally mis-attribution.** Pilot §5.5 lists CP's "SC-10" stale-cite under CP's count; it actually resolves to AR. Net effect: ~31 forward cites total, not ~32. Lane: `local`. Severity: MINOR.

The Option B carry-forward decision (do not patch `depends-on`) is correctly applied. ✓

---

## 3. Missed edges

### 3.1 BLOCKER / MAJOR

**MAJOR — `ev-027 → ar-019` should be `ev-027 → ar-020`.**
- **Where:** Pilot §2 row `ev-027`; pilot §5.1 row 4.
- **Source cite:** EV-027 body line 462, "via the foundation amendment protocol ([architecture.md §4.6])".
- **Issue:** AR §4.6 contains AR-020 (amendment-proposal procedure), AR-021 (amendment authority), AR-022 (foundation versioning), AR-023 (parallel-amendment serialization). AR-019 is in AR §4.5 ("Post-MVH process geometry is out of foundation scope"). The amendment protocol's load-bearing single-owner is **AR-020** (the procedure that EV-027 invokes when "MUST add the type to §8 via the foundation amendment protocol"). AR-019 is unrelated to amendments.
- **Discipline rule:** §3.1 step 5 term-use rule binds the cite to the load-bearing single-owner of the cited concept. The cited concept is "amendment protocol → propose/review/approve a foundation change", which is owned by AR-020. AR-019 is a post-MVH process-geometry carve-out that EV-027 does not invoke.
- **Correct edge:** `ev-027 → ar-020`.
- **Lane:** `local` — pilot misread the AR §4.6 anchor. Discipline rule (§3.1 step 5) is correct; pilot's application is wrong.
- **Severity:** MAJOR — at edge-fire time the load would either point at the wrong bead (`ar-019` for "post-MVH process geometry") or fail to resolve.

### 3.2 MINOR

**MINOR — `ev-events.outcome-emitted → em-schema.outcome-status` target name.**
- **Where:** Pilot §4a §8.1 table, row `ev-events.outcome-emitted`.
- **Issue:** EM does not declare an `OutcomeStatus` enum/schema. EM §6.1 has `Outcome` RECORD (which carries `status ∈ {SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS}` inline) and `OutcomeKind` ENUM. The pilot's notation `em-schema.outcome-status` does not match either. The actual schema bead is `em-schema.outcome` (the Outcome RECORD).
- **Lane:** `local`.
- **Severity:** MINOR — at edge-fire time, the mnem→assigned-ID lookup will fail to find `em-schema.outcome-status`; resolver should redirect to `em-schema.outcome`.
- **Recommended fix:** rename pilot's target identifier to `em-schema.outcome` in the §4a row description AND in §5.2 row 12.

**MINOR — pilot §5.2 prose count says "18 from §8" but actual count is 21.** Lane: `local`. Severity: MINOR (cosmetic).

**MINOR — pilot §5.5 CP forward-cite count includes the "SC-10" stale-cite that resolves to AR.** Net forward-cite tally is ~31, not ~32. Lane: `local`. Severity: MINOR.

---

## 4. Invented edges

**None confirmed.** All emitted live edges (`ev-004 → ar-013`, `ev-031 → ar-001`, `ev-031 → ar-005`, `ev-034a → ar-013`, two `→ ar-027` row-level, `ev-017/021/022 → em-031`, plus the 21 §8 EM-target row-level edges) trace to inline cites in EV §4 / §6 / §8 normative prose. The one questionable target — `ev-027 → ar-019` — is a misidentified target (MAJOR §3.1 above), not an invented edge per se: the cite exists in EV-027 body, but the target req ID was misread.

---

## 5. Depends-on validation (per discipline §3.2)

EV `depends-on: [architecture, execution-model, handler-contract, workspace-model]`. Cross-spec edge targets emitted by the pilot:

- Live edges: `{ar, em}` — both in `depends-on`. ✓
- Forward-deferred: `{hc, wm}` — both in `depends-on`. ✓
- Forward-cite findings (no edges fired): `{rc, bi, on, pl, cp}` — none in `depends-on`. **No edges emitted, so no violation.** ✓

**No depends-on violation.** ✓ The carry-forward Option B (don't patch `depends-on`) is consistent with EM precedent.

---

## 6. §4.a envelope rule (per discipline v0.7 §3.2)

**The pilot's F-pilot-EV-4 self-flag is correctly identified as a finding for the discipline author, not for the pilot to resolve.**

Discipline v0.7 §3.2 §4.a envelope grandfather carve-out: "The carve-out applies to the named pre-AR-053 set: EM, HC, CP, WM, PL, RC. AR itself is exempt by AR-052 (foundation-cross-cutting). Post-AR-053 specs (EV is in this category once it is drafted) DO require §4.a."

The discipline language explicitly names EV as "in this category once it is drafted" — that is, EV is **not in the grandfather set** and DOES require §4.a per the carve-out's plain text.

**Verified:** EV v0.3.3 has no §4.a section (verified by reading the full §4 ToC: §4.1 Envelope, §4.2 Clock, §4.3 Bus, §4.4 Durability, §4.5 Replay, §4.6 Producer/consumer, §4.7 Schema versioning, §4.8 Tagging, §4.9 Go representation, §4.10 Redaction). EV v0.3.3 also does not declare `spec-category: runtime-subsystem` in front matter (only `architecture.md` carries the field, with value `foundation-cross-cutting`).

**Two distinct reasons EV could be exempt:**
1. **AR-053-grandfather:** EV is NOT in the named set `{EM, HC, CP, WM, PL, RC}`, so this exemption does not apply per the plain text.
2. **`spec-category: foundation-cross-cutting`:** Per AR-052, foundation-cross-cutting specs are exempt from envelope declaration. EV does not declare a `spec-category`; absence is silent on whether EV is foundation-cross-cutting or runtime-subsystem.

**The pilot's argument** (F-pilot-EV-4) is "EV reached `reviewed` 2026-04-24 — the SAME DAY AR-053 landed" → ask for an extension of the carve-out to same-day cases. This is a discipline-author triage question.

**Reviewer's reading** of the discipline plain text: EV is post-AR-053 and DOES require §4.a per discipline §3.2. The same-day carve-out is NOT codified. Per pilot-review-protocol §3.3, this is surfaced as:

**MAJOR — EV missing §4.a Subsystem envelope.**
- **Where:** EV v0.3.3 §4 (ToC).
- **Discipline rule:** v0.7 §3.2 names the grandfather set explicitly as `{EM, HC, CP, WM, PL, RC}`; EV is NOT in that set; EV is named as "in this category [post-AR-053]" requiring §4.a.
- **Lane:** `class` — the carve-out language did not anticipate same-day cases. Three options exist (extend, require, codify same-day clarification); discipline author must choose.
- **Severity:** MAJOR — affects whether the pilot needs to mint an envelope bead AND whether the source spec needs an §4.a edit.
- **Pilot's choice** (carve-out compliant pending triage; no envelope bead minted) is reasonable as a pilot disposition but the finding belongs in the discipline lane.

(Note: the pilot's F-pilot-EV-4 is correctly tagged `class` in the pilot's §8 self-classification.)

---

## 7. Bidirectional cycle resolutions (pilot §5.7)

The pilot walks 17 candidate bidirectional pairs across the 137-bead set. Spot-check sample:

### 7.1 Spot-checked resolutions

**`ev-001 ↔ ev-034a` (#1).** Pilot resolves: emit `ev-034a → ev-001`; remove `ev-034a` from `ev-001` predecessor list. Verified: EV-001 declares envelope (incl. `source_subsystem` field); EV-034a declares startup-time registration of identifiers. Slot-rule heuristic: EV-001 declares the slot (envelope field), EV-034a fills it. Direction emit `ev-034a → ev-001` is the slot-rule direction. **However, the pilot says "Patch §2 row table: drop `ev-034a` from `ev-001`'s predecessor list"** — but inspecting the §2 row table line 48, EV-001's blocks edges include `ev-002`, `ev-034a`, `ar-001`. The pilot's patch removes `ev-034a` from `ev-001`'s predecessor list. **Cross-check:** EV-001's actual body cite to EV-034a is implicit (envelope declares fields, registration declares identifiers); F13 slot-rule is correct. **Patch is sound.** ✓

**`ev-014b ↔ ev-018` (#5).** Pilot resolves: both supporting per F-pilot-AR-10; remove `ev-018` from `ev-014b` predecessor list. Verified: EV-014b body cites EV-018; EV-018 body cites EV-014b ("Consumers MUST be coded against tail truncation (EV-014b)"). Both claims independently testable. Pilot's resolution correct. ✓

**`ev-035 ↔ ev-036` (#2).** Both supporting (no edge between them). Verified: EV-035 body says "compile-time check (EV-036) is the structural guardrail" (supplemental); EV-036 body says "complementing EV-035's runtime redactor" (supplemental). Both claims independently testable. **Pilot's resolution correct.** ✓

**`ev-events.taxonomy umbrella ↔ §4 reqs` (#15).** Pilot's resolution: umbrella → §4 reqs (slot-rule); remove `ev-events.taxonomy` from EV-016, EV-025, EV-026, EV-027, EV-031, EV-033, EV-036's predecessor lists. Verified per F13 slot-rule heuristic (umbrella is the canonical taxonomy slot; §4 reqs are content rules). The patch is necessary because the §2 row table lines 71, 81, 82, 83, 87, 89, 93 currently list `ev-events.taxonomy` in those reqs' blocks edges — which would create the umbrella-cycle. **Patch is sound.** ✓

### 7.2 Independent cycle scan

Reviewer pairwise-walked the §2 / §3 / §4 / §4a tables for additional A-cites-B-and-B-cites-A pairs not listed in §5.7. Sampled checks:

- **`ev-014a ↔ ev-014c`?** EV-014a cites EV-010/011/012 for dispatch; EV-014c is observer dispatch detail. EV-014c does not cite EV-014a in body. **No cycle.**
- **`ev-002a ↔ ev-002b ↔ ev-002c`?** EV-002a → no upstream cite. EV-002b cites EV-002a. EV-002c cites EV-002a + EV-016 + `daemon_degraded` event. EV-002a does not cite the children. **Linear chain, no cycle.**
- **`ev-008 ↔ ev-002a`?** EV-008 cites EV-002, EV-002a, `trace_context.parent_event_id` (§6.1), `hook_fired`. EV-002a strengthens EV-008 ("strengthens EV-008's partial-order contract"). Reverse cite is term-use only. EV-008 does not block on EV-002a being implemented; it observes EV-002a's contribution. **F13 slot-rule heuristic: EV-008 is the partial-order slot; EV-002a is the content rule.** Direction `ev-008 → ev-002a` is correct (slot-rule); reverse is informational. **Pilot's `ev-008` row blocks-on `ev-002a` correctly.** ✓ No cycle.
- **`ev-014d ↔ ev-014b ↔ ev-018`?** Triangle: EV-014d cites both as "load-bearing inputs to the replay contract"; EV-014b ↔ EV-018 already resolved as both-supporting (#5). EV-014d's posture (different — names them as load-bearing) emits `ev-014d → ev-014b` and `ev-014d → ev-018` per pilot. Neither of those reverses fires. **No cycle.** ✓

**No additional bidirectional cycles found in spot-checks.** Pilot's claim of "17 candidate cycles examined" is reasonable; the 11 F13 + 1 F-pilot-AR-10 + 5 no-cycle accounting holds. ✓

---

## 8. The `ev-inv-001 → em-inv-001` invariant-as-target edge

The pilot emits this edge per §2.5 F10 (invariant-as-edge-target rule).

**Discipline rule citations:**

1. **§2.5 F10 (v0.4):** "Invariant beads MAY be the target of cross-spec edges when a downstream requirement EXPLICITLY cites the invariant by ID (e.g., `RC-INV-004`). By default, downstream consumers edge to the constrained §4 requirements, not to the invariant. Edging to the invariant is appropriate when the consumer's correctness depends on the invariant's sensor passing, not on any individual constrained requirement."

2. **§3.1 step 5 (v0.7) F-pilot-AR-r2-2 Invariant-as-target exemption:** "When the defining requirement is a `<prefix>-INV-NNN` invariant, the term-use rule does NOT fire."

3. **§3.1 step 5 (v0.7) F-em-r1-MAJ-4 Rule precedence:** "When BOTH the supporting-cite test (F-pilot-AR-10) AND the invariant-as-target exemption (F-pilot-AR-r2-2) could plausibly apply to the same cite, the invariant-as-target exemption takes precedence."

**Reviewer reading:** F10 (v0.4 — invariant-as-edge-target permission) and F-pilot-AR-r2-2 (v0.6 — term-use does not fire when target is invariant) are NOT in conflict; they cover different code paths. F10 is the EXPLICIT-ID-CITE path (downstream cites the invariant by ID); F-pilot-AR-r2-2 is the IMPLICIT-TERM-USE path (downstream uses a term that happens to be the title of an invariant). They distinguish:

- **EV-INV-001 body cites `EM-INV-001` BY EXPLICIT ID** (line 541: "Git plus Beads is authoritative per [execution-model.md §4.7 EM-INV-001]"). This is the F10 path → edge permitted.
- The F-pilot-AR-r2-2 exemption blocks the impl→invariant direction (e.g., `ev-040 → ev-inv-007`-style reverse), not the invariant→invariant direction.

**The pilot's edge `ev-inv-001 → em-inv-001` is INVARIANT TO INVARIANT** (sensor-side to sensor-side). This is a fourth case the discipline does not explicitly address. Reading F10 and F-pilot-AR-r2-2 together:

- F10 permits invariant-as-target when CITED BY ID. ✓ (EV-INV-001 cites EM-INV-001 by ID.)
- F-pilot-AR-r2-2 forbids term-use → invariant edges to avoid §2.5 F12 sensor↔impl one-way reversal. The §2.5 F12 rule is about IMPL→SENSOR being forbidden; SENSOR→SENSOR is not addressed.
- F-em-r1-MAJ-4 is about precedence between F10 and F-pilot-AR-r2-2 when BOTH might apply. Here F10 cleanly applies (explicit ID cite); F-pilot-AR-r2-2 doesn't fire because EV-INV-001 is itself an invariant (not an impl req), so the "impl term-uses invariant" rule isn't being invoked.

**Verdict:** The edge `ev-inv-001 → em-inv-001` is permitted under F10 (explicit ID cite) and does NOT trigger F-pilot-AR-r2-2 (which is impl→invariant-specific). **Pilot's emission is correct.** ✓

**Discipline-class observation (MINOR class):** The discipline does not explicitly address invariant→invariant cross-spec edges. The pilot's emission is sound by F10 reading, but the discipline could grow a clarification:

- **Minor `class` finding:** §2.5 F10 should explicitly cover the SENSOR→SENSOR (invariant-bead-to-invariant-bead) case. EV's `ev-inv-001 → em-inv-001` is the first instance of this pattern in the corpus. The current rule covers REQ→INVARIANT (F10's "downstream requirement explicitly cites the invariant by ID"); the same logic extends to INVARIANT→INVARIANT but the rule text doesn't say so.
- **Lane:** `class`.
- **Severity:** MINOR (the pilot's reading is defensible; discipline language is silent rather than wrong).
- **Recommended discipline patch:** §2.5 F10 grow a sub-clause: "F10 applies symmetrically to invariant→invariant edges when the citing invariant's body explicitly cites the target invariant by ID. The §2.5 F12 sensor↔impl one-way rule does NOT apply to sensor↔sensor; both invariants are the same kind of bead (sensor-bead) and the citation direction is determined by the body cite, not by F12's impl/sensor distinction."

---

## 9. Summary of findings

### 9.1 BLOCKER

None.

### 9.2 MAJOR

| # | Finding | Lane | Section |
|---|---|---|---|
| 1 | `ev-027 → ar-019` should be `ev-027 → ar-020`. AR-019 is post-MVH process geometry (§4.5); AR-020 is the amendment-proposal procedure (§4.6) which is the term-use single-owner of EV-027's "foundation amendment protocol" cite. | `local` | §3.1 |
| 2 | EV missing §4.a Subsystem envelope. EV is post-AR-053 per discipline v0.7 §3.2 plain text and is NOT in the named grandfather set `{EM, HC, CP, WM, PL, RC}`. The same-day-as-AR-053 carve-out is not codified. F-pilot-EV-4 correctly self-flags this as a discipline-author triage question. | `class` | §6 |

### 9.3 MINOR

| # | Finding | Lane | Section |
|---|---|---|---|
| 3 | `ev-events.outcome-emitted → em-schema.outcome-status` — target bead name does not match EM's actual schema. EM has `Outcome` RECORD and `OutcomeKind` ENUM but no `OutcomeStatus` enum; the resolvable target is `em-schema.outcome`. | `local` | §3.2 |
| 4 | Pilot §5.2 says "18 from §8" but the actual EM §8 row-edge count is 21; the 3 + 18 = 21 prose tally is internally inconsistent (21 + 3 = 24, not 21). | `local` | §2.2 |
| 5 | Pilot §5.5 CP forward-cite count includes the EV-031 "SC-10" stale-cite that resolves to AR-005, not CP. Net forward-cite tally is ~31, not ~32. | `local` | §2.4 |
| 6 | §2.5 F10 invariant-as-edge-target rule does not explicitly cover SENSOR→SENSOR (invariant-to-invariant) edges. Pilot's `ev-inv-001 → em-inv-001` emission is defensible under F10's "explicit ID cite" reading but the discipline language is silent on the symmetric case. | `class` | §8 |

### 9.4 No depends-on violations

EV `depends-on: [architecture, execution-model, handler-contract, workspace-model]`; all live and forward-deferred edges target specs in this set. Forward-cite findings (RC, BI, ON, PL, CP — ~31 occurrences) are correctly logged with no edges emitted, consistent with EM precedent (Option B carry-forward).

### 9.5 No bidirectional cycles unresolved

Pilot's 17-candidate walk + reviewer's spot-check found no additional cycles. The umbrella-slot resolution (§5.7 #15) is necessary and correct.

### 9.6 The `ev-inv-001 → em-inv-001` edge

Permitted under §2.5 F10 (explicit ID cite); does not trigger §3.1 F-pilot-AR-r2-2 (which is impl→invariant-specific). MINOR class finding (#6 above) records the discipline silence on the symmetric invariant→invariant case for the discipline author's consideration; pilot's emission stands.

---

## 10. Reviewer's bottom line

The EV pilot's reference handling is structurally sound. The dominant features:

- 7 emitted AR edges (1 with wrong target req-ID — MAJOR `local` #1).
- 21 emitted EM edges (1 with wrong schema-bead-name — MINOR `local` #3; 1 prose-tally undercount — MINOR `local` #4).
- ~30 forward-deferred HC edges; ~6 forward-deferred WM edges (Option B precedent).
- ~31 forward-cite findings across non-`depends-on` specs (one tally mis-attribution — MINOR `local` #5).
- 1 invariant-as-target edge (`ev-inv-001 → em-inv-001`) correctly emitted under F10 (MINOR `class` #6 records discipline silence on symmetric case).
- §4.a envelope status is a class finding for discipline-author triage (MAJOR `class` #2).

After fixing #1 (rename `ar-019` → `ar-020` in pilot §2 row `ev-027` and pilot §5.1) and #3 (rename `em-schema.outcome-status` → `em-schema.outcome` in pilot §4a §8.1.8 and §5.2), the live edge set is correct. #2 routes to the discipline lane; #6 is a documentation tightening for §2.5 F10. #4 and #5 are cosmetic prose tally fixes.
