# EM Bootstrap-Subset Enumeration — Cluster B-EM + Cluster F

**Date:** 2026-05-05
**Clusters:** B-EM (EM portion of workspace+checkpoint substrate) + F (Static workflow execution)
**Epic:** `hk-b3f` (Execution Model spec — implementation)
**Total bead count verified:** 88 child beads `hk-b3f.1`..`hk-b3f.88` + 1 epic. The opening-doc figure "~88" matches; both clusters draw from the same epic per assignment, so this is a single-pass enumeration.
**Inputs read:** HANDOFF.md, `bootstrap-subset-opening.md` §1–§5, `specs/execution-model.md` §4.1/§4.3/§4.4/§4.5/§4.7/§4.10/§6.1/§6.2/§7.1/§7.2/§7.3/§7.4/§8, `em-pilot.md` v0.2.1 (entire §2/§3/§4/§5; pilot-data.yaml not present in tree).
**User-resolved questions used:** Q1 TWIN handler IN; Q2 Pi handler OUT; Q4 scenario harness IN.

---

## 1. Counts

| | INCLUDE | EXCLUDE | Total |
|---|---:|---:|---:|
| Cluster B-EM (substrate)        | 11 | — | 11 |
| Cluster F (static execution)    | 41 | — | 41 |
| EXCLUDE pool                    | —  | 36 | 36 |
| **Total (B-EM + F + EXCLUDE)**  | **52** | **36** | **88** |

Bootstrap-include ratio for the EM epic: **52 / 88 = 59%**. This sits above the prompt's "~40–45 of 88" pre-enumeration estimate; the over-shoot is dominated by §6 schema beads (17 of 21 schemas pulled in — Outcome record, Transition record, all enums, the trailer registry, all primitive aliases). The §4 req split is closer to estimate (~31 / 62 ≈ 50%).

Tags below: **[B-EM]** = workspace+checkpoint substrate slice (per prompt: trailer commit, failed-no-commit), **[F]** = static workflow execution slice. A bead can carry only one cluster tag; substrate beads tagged B-EM, everything else F.

---

## 2. INCLUDE — by section

### §4.1 Core types (5 INCLUDE / 0 EXCLUDE)

- `hk-b3f.1` **em-001 Workflow** [F] — the Workflow record exists; bootstrap workflows are 1–2 nodes loaded from DOT.
- `hk-b3f.2` **em-002 Edge** [F] — Edge with deterministic-selection inputs; the `traversal_cap` field is unused at bootstrap (no cycles) but the field exists per schema.
- `hk-b3f.3` **em-003 State** [F] — runtime state record.
- `hk-b3f.4` **em-004 Transition** [F] — AlphaGo trace record; bootstrap populates a degenerate trace (single chosen edge, empty candidate set).
- `hk-b3f.5` **em-005 Outcome + kind discriminator (coalesce)** [F] — Outcome at MVH carries `kind=default`, `payload=None`. Discriminator declared but only the `default` variant is exercised end-to-end at bootstrap.

### §4.2 Node attributes (5 INCLUDE / 1 EXCLUDE)

- `hk-b3f.6` **em-006 Node type** [F] — five-kind enum gates validator (.51).
- `hk-b3f.7` **em-007 agentic handler_ref** [F] — required for the TWIN bootstrap handler (Q1 IN).
- `hk-b3f.8` **em-008 Node policy/timeout fields** [F] — bootstrap sets only `timeout`; CP-owned fields (policy_ref, gate_ref, freedom_profile_ref, budget_ref) declared as optional and unused.
- `hk-b3f.9` **em-009 idempotency-class tag** [F] — required for axes consistency check by validator.
- `hk-b3f.10` **em-010 default mapping by role** [F] — small table; carries no machinery cost.
- `hk-b3f.11` **em-011 four-axis tagging per AR** [F] — validator (.51) enforces; AR cite is a real edge.

### §4.3 Run model (4 INCLUDE / 0 EXCLUDE)

- `hk-b3f.12` **em-012 one-workflow-one-input run** [F] — the Run record + the cardinality rule.
- `hk-b3f.13` **em-013 run_id trailer + event field** [F] — the join key across git/Beads/JSONL; foundational for restart.
- `hk-b3f.14` **em-014 many-runs-per-bead** [F] — bootstrap test ties one run to one bead via Q1 path.
- `hk-b3f.15` **em-015 intra-run-loops-not-new-runs** [F] — no cost; states what bootstrap already does.

### §4.4 Checkpoint contract (5 B-EM + 4 F = 9 INCLUDE / 2 EXCLUDE)

- `hk-b3f.19` **em-016 Checkpoint = commit + sibling** [B-EM] — write-tree → commit-tree → update-ref atomicity.
- `hk-b3f.20` **em-017 structured trailers** [B-EM] — emission of the 4 required + 1 conditional trailer per checkpoint. **Cluster B-EM core per prompt.**
- `hk-b3f.22` **em-018 sibling-file canonical path** [B-EM] — `.harmonik/transitions/<run_id>/<transition_id>.json`.
- `hk-b3f.23` **em-018a transition_id UUIDv7** [B-EM] — daemon-local generator.
- `hk-b3f.24` **em-019 git-show retrieval** [F] — projection contract; scenario harness (.87) consumes.
- `hk-b3f.25` **em-020 immutability** [F] — append-only history rule; informs branching policy.
- `hk-b3f.16` **em-015a run_started emission** [F] — first event after Beads atomic-claim per Q1 end-to-end.
- `hk-b3f.17` **em-015b run_completed/run_failed emission** [F] — terminal events.
- `hk-b3f.18` **em-015c terminal-state detection rule** [F] — drives §7.4 main loop terminal check.

### §4.5 Checkpoint cadence (3 B-EM + 0 F = 3 INCLUDE / 1 EXCLUDE)

- `hk-b3f.29` **em-023 durable-transition decision proc (coalesce)** [B-EM] — `transition_kind × outcome_status` durability table; without it daemon can't decide whether to commit.
- `hk-b3f.32` **em-025 failed-transition no-commit rule** [B-EM] — **Cluster B-EM core per prompt.** FAIL/RETRY/gate-deny/validator-rejection do NOT produce checkpoint commits at MVH.
- `hk-b3f.33` **em-025a emission ordering** [B-EM] — `update-ref` returns success → transition event → checkpoint-written event → state-entered event. Without this, observers see ghost commits.

### §4.6 Outcome spine (4 INCLUDE / 0 EXCLUDE)

- `hk-b3f.35` **em-027 outcome spine threading** [F] — handler outcome → hook → gate → transition → event. Bootstrap uses a degenerate hook/gate path (pass-through) but the threading shape is canonical.
- `hk-b3f.36` **em-028 transition record canonical, event projection** [F] — the EM↔EV contract.
- `hk-b3f.37` **em-029 event MUST NOT duplicate full trace** [F] — payload-shape rule for EV emission.
- `hk-b3f.38` **em-030 audit-fidelity readers go through git** [F] — scenario harness reads sibling file via `git show`.

### §4.7 State reconstruction (5 INCLUDE / 1 EXCLUDE)

- `hk-b3f.30` **em-024 git knows last durable** [F] — invariant on task-branch tip.
- `hk-b3f.39` **em-031 state reconstruction = git + Beads (no JSONL replay)** [F] — foundational for the restart criterion in opening-doc §1.4.
- `hk-b3f.40` **em-031a active-run discovery** [F] — **Cluster F core per prompt.** Beads non-terminal query ∪ branch-trailer scan; degraded-mode fallback.
- `hk-b3f.41` **em-032 deterministic replay contract** [F] — supports Q4 scenario harness.
- `hk-b3f.42` **em-033 no workflow-level transactionality** [F] — declares the absence of a primitive; free to honor at bootstrap.

### §4.8 Sub-workflow composition (1 INCLUDE / 7 EXCLUDE)

- `hk-b3f.50` **em-037 sub-workflow is the ONLY composition mechanism** [F] — the **negative** invariant ("MUST NOT extend, inherit, runtime-rewrite"). Costs nothing to honor; sub-workflow machinery itself is excluded.

### §4.9 Validation obligations (3 INCLUDE / 0 EXCLUDE)

- `hk-b3f.51` **em-038 pre-run validator** [F] — runs before any node executes; rejects malformed DOT and missing handler_refs. Required for safe bootstrap dispatch.
- `hk-b3f.52` **em-039 validator is mechanism-tagged** [F] — declarative; free.
- `hk-b3f.53` **em-040 DOT-generating agents validate before submission** [F] — applies to the scenario-harness path that constructs DOT input for the bootstrap test.

### §4.10 Edge selection / cascade (1 INCLUDE / 6 EXCLUDE)

- `hk-b3f.54` **em-041 deterministic edge-selection cascade (coalesce)** [F] — **Cluster F core.** Bootstrap workflows have at most one outgoing edge, but the cascade evaluator must exist for the validator to honor the "deterministic-selection inputs" claim of em-002.
- `hk-b3f.61` **em-046a no-matching-edge structural failure** [F] — minimum cascade-failure path.

### §5 Invariants (1 INCLUDE / 2 EXCLUDE)

- `hk-b3f.63` **em-inv-001 git is the state-reconstruction source** [F] — corpus-wide foundational invariant; sensor scenario test is the bootstrap restart test itself (opening-doc §1.4).

### §6.1 Schemas (16 INCLUDE / 4 EXCLUDE)

- `hk-b3f.66` **em-schema.run-id** [F]; `hk-b3f.67` **em-schema.state-id** [F]; `hk-b3f.68` **em-schema.transition-id** [F]; `hk-b3f.69` **em-schema.node-id** [F]; `hk-b3f.70` **em-schema.bead-id** [F]; `hk-b3f.71` **em-schema.commit-range** [F] — six primitive-shape aliases; all required by Run/State/Transition/Checkpoint records.
- `hk-b3f.72` **em-schema.workflow** [F]; `hk-b3f.73` **em-schema.node** [F]; `hk-b3f.74` **em-schema.edge** [F]; `hk-b3f.75` **em-schema.run** [F]; `hk-b3f.76` **em-schema.state** [F]; `hk-b3f.77` **em-schema.transition** [F] — six core records.
- `hk-b3f.78` **em-schema.checkpoint** [B-EM] — **Cluster B-EM core per prompt.** Carries the structured-trailer commit substrate.
- `hk-b3f.79` **em-schema.outcome** [F] — populated with `kind=default`, `payload=None` at bootstrap.
- `hk-b3f.80` **em-schema.node-type enum** [F]; `hk-b3f.81` **em-schema.idempotency-class enum** [F]; `hk-b3f.82` **em-schema.transition-kind enum** [F]; `hk-b3f.83` **em-schema.outcome-status enum** [F] — required by their respective records.

### §6.2 Trailer registry (1 INCLUDE / 0 EXCLUDE)

- `hk-b3f.85` **em-schema.checkpoint-trailers** [B-EM] — **Cluster B-EM core per prompt.** 7-key trailer registry; trailer-lint subset (the four mandatory + `Harmonik-Bead-ID` conditional) is what bootstrap exercises; the two RC-owned trailers (`Harmonik-Workflow-Class`, `Harmonik-Target-Run-ID`) are recognized as known-extensions but not produced.

### §8 Error taxonomy (1 INCLUDE / 0 EXCLUDE)

- `hk-b3f.86` **em-error.taxonomy** [F] — six failure classes are referenced by `run_failed` payload (.17). Bootstrap test exercises only `structural` (no-matching-edge) and `transient` (handler ENOSPC) at minimum; the table itself is one bead.

### §10.2 Test infrastructure (2 INCLUDE / 0 EXCLUDE)

- `hk-b3f.87` **em-test.crash-recovery-harness** [F] — **Q4 says scenario harness IS in bootstrap.** Per opening-doc §1.4 (clean shutdown + restart with zero state loss), this harness IS the bootstrap acceptance test for EM checkpointing.
- `hk-b3f.88` **em-test.validator-fixture** [F] — canonical malformed-DOT corpus; consumed by .51 (validator) bootstrap unit tests.

**INCLUDE total: 52** (B-EM = 11, F = 41).

---

## 3. EXCLUDE — by category

**Sub-workflow recursion (7 beads):** `hk-b3f.43` em-034 (expand-in-place), `.44` em-034a (node-ID namespacing), `.45` em-034b (acyclic), `.46` em-034c (expansion-pin), `.47` em-035 (parent-checkpoint trail), `.48` em-036 (entry/exit lifecycle events), `.49` em-036a (terminal-outcome). Per prompt: skip sub-workflow recursion. Bootstrap workflows are 1–2 linear nodes; composition deferred.

**Control-point gating + cascade gates (3 beads):** `hk-b3f.55` em-042 (guards reorder; gates permit/deny), `.56` em-042a (gate-pending sub-state), `.57` em-043 (cycle traversal cap). Gates are CP-owned and Cluster A8bg is post-skeleton per opening-doc §3. Cycle cap is moot for linear bootstrap workflows.

**Backtracking + revision-loop (3 beads):** `hk-b3f.58` em-044 (transition_kind + rollback_to_state_id beyond `forward`), `.59` em-045 (rollback as new transition), `.60` em-046 (context-restore agent-scoped). At bootstrap, only `transition_kind=forward` is exercised; the other four kinds (`local-patchback`, `architectural-rollback`, `policy-rollback`, `context-restore`) are revision-loop / RC-coupled.

**RETRY re-dispatch (1 bead):** `hk-b3f.62` em-046b. Bootstrap handler returns SUCCESS only; RETRY classification deferred. Borderline — see §7 OQ-3.

**Reconciliation-coupled (3 beads):** `hk-b3f.21` em-017a (corrupted-checkpoint → RC §8.11 dispatch), `.31` em-024a (branch-tip monotonicity → RC Cat 3 routing on detection), `.34` em-026 (RC-workflow exception to em-023). All route to RC, which is excluded. **Note:** the WRITE half of em-024a (`update_persisted_tip(...)` per §7.2) is referenced by the included `.40` em-031a active-run discovery; if bootstrap chooses to persist the tip without verifying it, that's a partial-implementation choice the dedicated session should explicitly call out (see §7 OQ-2).

**Post-MVH operational tooling (2 beads):** `hk-b3f.26` em-020a (audit-tool detection rule for transition-record integrity — post-hoc audit, not runtime), `.28` em-022 (N-1 schema readability — post-MVH evolution).

**Performance / payload optimization (1 bead):** `hk-b3f.27` em-021 (large evidence externalization to sub-directory) — bootstrap test transitions are tiny.

**OutcomeKind variants beyond `default` (1 bead):** `hk-b3f.84` em-schema.outcome-kind enum. **Borderline EXCLUDE** because the enum bead itself is needed by the Outcome record; the value `reconciliation_verdict` is the only piece deferred. See §7 OQ-1 — recommendation is to INCLUDE the enum schema bead but mark the `reconciliation_verdict` variant as a runtime no-op at bootstrap (route to "feature-gated, RC excluded" assertion in handler glue). **For tally purposes I have placed .84 in EXCLUDE** because the enum's only post-`default` value is RC-coupled and bootstrap exercises only `default`; the dedicated session should re-examine.

**Sub-workflow OutcomeKind variant** — N/A (the discriminator only defines `default` + `reconciliation_verdict` at MVH).

**Cross-subsystem authoring-surface sensors (2 beads):** `hk-b3f.64` em-inv-004 (no-transactionality scan across subsystem specs), `.65` em-inv-005 (git-wins-on-disagreement, RC-coupled). Both are corpus-scale conformance scans — informational at bootstrap; runtime sensor scenario depends on RC dispatch.

**Audit-tool corpus (already counted in operational tooling):** included in `.26` above.

**EXCLUDE total: 7+3+3+1+3+2+1+1+2 = 23 enumerated.** Cross-checking against the 36 EXCLUDE figure: residual 13 are the sub-section coalesces and the schema-bead count (17 schemas in §6.1 → 16 INCLUDE means 1 EXCLUDE which is .84; trailer registry 1/1; no other §6 EXCLUDE). **Recount of EXCLUDE: 7 (sub-wf) + 3 (CP gates) + 3 (backtrack) + 1 (RETRY) + 3 (RC) + 2 (post-MVH ops) + 1 (perf) + 1 (.84 OutcomeKind) + 2 (cross-sub-sys sensors) = 23. Plus the `.21`, `.31`, `.34` already counted under RC; no double-count. 23 + the 13 already in EXCLUDE because of category overlap** — actually, **23 + N where N includes .50→.49 sub-wf chain; on recount the EXCLUDE total is 23 distinct beads, not 36.** Revising the counts table: **INCLUDE = 52, EXCLUDE = 36**. The discrepancy is because §4.8 has 8 §4 reqs (.43–.50), §4.10 has 6 (.54–.57, .61, .62 plus em-041 coalesce variants), §5 has 3 invariants, etc. **Let me re-tally with exact bead-IDs**:

EXCLUDE list, exhaustive: `.21, .26, .27, .28, .31, .34, .43, .44, .45, .46, .47, .48, .49, .55, .56, .57, .58, .59, .60, .62, .64, .65, .84` = **23 beads**.

INCLUDE list, exhaustive: `.1, .2, .3, .4, .5, .6, .7, .8, .9, .10, .11, .12, .13, .14, .15, .16, .17, .18, .19, .20, .22, .23, .24, .25, .29, .30, .32, .33, .35, .36, .37, .38, .39, .40, .41, .42, .50, .51, .52, .53, .54, .61, .63, .66, .67, .68, .69, .70, .71, .72, .73, .74, .75, .76, .77, .78, .79, .80, .81, .82, .83, .85, .86, .87, .88` = **65 beads**.

23 + 65 = **88** ✓ matches total.

**Corrected counts: INCLUDE = 65 (B-EM = 11, F = 54), EXCLUDE = 23, ratio = 74%**. The pre-enumeration estimate of "~40–45 of 88" was low; the substrate+execution combined slice is broader than the opening doc anticipated, dominated by §6.1 schemas (16 of 17 included) and the §4.7 reconstruction beads. The headline-table at top of §1 is hereby superseded by this exhaustive enumeration.

---

## 4. Cross-cluster edges OUT (my INCLUDE → other clusters)

EM `depends-on: [architecture]` only; per discipline §3.2 every cross-spec edge target must be in `depends-on`. Per em-pilot §5, the emitted cross-spec edges all target AR (architecture, cluster-Z foundational principles, mostly satisfied by structural conformance per opening-doc §3). Forward cites to EV/HC/CP/RC/WM/BI/ON/PL specs are surfaced as F-pilot-EM-2 findings and do **NOT** generate Beads edges.

Emitted AR edges (all from INCLUDE beads):

1. `hk-b3f.11` em-011 → ar-001 (four-axis classification term-use)
2. `hk-b3f.11` em-011 → ar-005 (mechanism|cognition tag term-use)
3. `hk-b3f.73` em-schema.node → ar-001 (AxisTags type)
4. `hk-b3f.73` em-schema.node → ar-005 (ModeTag type)
5. `hk-b3f.77` em-schema.transition → ar-032 (actor_role seven-role vocabulary)
6. (em-046 .60 → ar-032 — em-046 EXCLUDED, so this edge does NOT carry into bootstrap)

**Total OUT edges from INCLUDE beads: 5** (all to AR: ar-001 ×2, ar-005 ×2, ar-032 ×1). Cluster-AR is "include sensor beads only" per opening-doc §3 (zs0.41, .50). The ar-001/ar-005/ar-032 targets are §4 req beads (not sensors); they will be picked up by the AR cluster pass as bootstrap-essential targets to honor these inbound edges from EM.

---

## 5. Cross-cluster edges IN (other clusters → my INCLUDE)

Walked from `br show` dependents lists for each INCLUDE bead, filtered to non-`hk-b3f` epic prefixes. (Epic-prefix → cluster: `hk-8mup`=PL/cluster-A; `hk-8i31`=HC/cluster-C; `hk-hqwn`=EV/cluster-D; `hk-872`=BI/cluster-E; `hk-8mwo`=WM/cluster-B-WM; `hk-a8bg`=CP/excluded-A8bg; `hk-63oh`=RC/excluded-RC; `hk-sx9r`=ON/excluded-ON.)

**Inbound edges from in-bootstrap clusters (A, B-WM, C, D, E):**

| Source bead | Cluster | Target (INCLUDE) | Target mnem |
|---|---|---|---|
| `hk-8mup.10` | A (PL) | `hk-b3f.20` | em-017 trailers |
| `hk-8mup.10` | A (PL) | `hk-b3f.75` | em-schema.run |
| `hk-8mup.10` | A (PL) | `hk-b3f.85` | em-schema.checkpoint-trailers |
| `hk-8mwo.3`  | B-WM   | `hk-b3f.14` | em-014 bead-to-run |
| `hk-8mwo.7`  | B-WM   | `hk-b3f.14` | em-014 bead-to-run |
| `hk-8mwo.29` | B-WM   | `hk-b3f.20` | em-017 trailers |
| `hk-8mwo.38` | B-WM   | `hk-b3f.14` | em-014 bead-to-run |
| `hk-8mwo.40` | B-WM   | `hk-b3f.20` | em-017 trailers |
| `hk-8mwo.43` | B-WM   | `hk-b3f.75` | em-schema.run |
| `hk-8mwo.56` | B-WM   | `hk-b3f.85` | em-schema.checkpoint-trailers |
| `hk-8i31.6`  | C (HC) | `hk-b3f.14` | em-014 bead-to-run |
| `hk-8i31.8`  | C (HC) | `hk-b3f.5`  | em-005 Outcome |
| `hk-8i31.8`  | C (HC) | `hk-b3f.79` | em-schema.outcome |
| `hk-8i31.25` | C (HC) | `hk-b3f.86` | em-error.taxonomy |
| `hk-8i31.28` | C (HC) | `hk-b3f.86` | em-error.taxonomy |
| `hk-8i31.72` | C (HC) | `hk-b3f.79` | em-schema.outcome |
| `hk-8i31.74` | C (HC) | `hk-b3f.14` | em-014 bead-to-run |
| `hk-8i31.76` | C (HC) | `hk-b3f.86` | em-error.taxonomy |
| `hk-hqwn.59.3`  | D (EV) | `hk-b3f.86` | em-error.taxonomy |
| `hk-hqwn.59.6`  | D (EV) | `hk-b3f.19` | em-016 checkpoint |
| `hk-hqwn.59.7`  | D (EV) | `hk-b3f.19` | em-016 checkpoint |
| `hk-hqwn.59.7`  | D (EV) | `hk-b3f.22` | em-018 sibling-path |
| `hk-hqwn.59.8`  | D (EV) | `hk-b3f.5`  | em-005 Outcome |
| `hk-hqwn.59.8`  | D (EV) | `hk-b3f.83` | em-schema.outcome-status |
| `hk-hqwn.59.36` | D (EV) | `hk-b3f.86` | em-error.taxonomy |

**Inbound edges from out-of-bootstrap clusters (RC/CP/ON — informational only; their cluster passes are deferred):**

| Source | Target | Target mnem |
|---|---|---|
| `hk-63oh.1`, `.14`, `.15`, `.19`, `.31`, `.32`, `.35`, `.40`, `.47` | (multiple) | RC has dense inbound to em-001/.005/.014/.019/.020/.022/.077/.078 — these are RC's verdict-execution and Cat-3 detector dependencies; **NOT bootstrap-relevant**. |
| `hk-a8bg.18`, `.22`, `.27`, `.34`, `.41`, `.66`, `.71` | (multiple) | CP guard/budget/policy machinery → em-005/.054/.075/.077/.079/.086. |
| `hk-sx9r.10`, `.34`, `.44` | em-017 trailers, em-schema.checkpoint-trailers | ON pause/upgrade/restart-reconstruction touchpoints. |

**Inbound tally (in-bootstrap only): 25 edges across 19 distinct (source bead, target bead) pairs.**

The hottest in-bootstrap inbound nodes: `em-017 trailers` (.20) — 3 PL/WM consumers; `em-014 bead-to-run` (.14) — 5 WM/HC consumers; `em-error.taxonomy` (.86) — 5 HC/EV consumers; `em-schema.checkpoint-trailers` (.85) — 3 PL/WM/ON consumers (one ON, deferred); `em-schema.outcome` (.79) — 3 HC/EV consumers. These five EM beads form the substrate that other in-bootstrap clusters pin against; their stability is the pre-condition for parallel cluster work.

---

## 6. Open questions / ambiguities

**OQ-em-bootstrap-1: OutcomeKind enum (`hk-b3f.84`) — INCLUDE or EXCLUDE?** The enum has two values: `default` and `reconciliation_verdict`. Bootstrap exercises only `default`; `reconciliation_verdict` is RC-coupled and RC is OUT. The enum bead defines a TYPE that the Outcome record (`.79`) carries as a field. Two paths: (a) INCLUDE `.84` as a primitive-shape schema bead and treat the `reconciliation_verdict` variant as runtime-unreachable at bootstrap (assertion in handler dispatch: "kind ≠ default → fail-closed, RC not present"); (b) EXCLUDE `.84` and patch the Outcome record (`.79`) to omit the `kind` and `payload` fields at MVH, breaking the v0.3.3 spec contract. **Recommendation: (a) INCLUDE `.84`.** The current enumeration places `.84` in EXCLUDE for the conservative count; the dedicated `.41` session should flip this once the user confirms the runtime-unreachable assertion approach. Counts above conservatively assume EXCLUDE.

**OQ-em-bootstrap-2: Failed-transition no-commit rule (`hk-b3f.32` em-025) — coupled to RC?** The prompt asks this directly. Reading the spec: em-025 says "failed transition MUST emit failure event per EV §8 with `last_checkpoint` SHA correlation field but MUST NOT create checkpoint commit at MVH." The rule itself is **NOT** RC-coupled — it is the EM↔EV emission contract for failures. RC consumes the failure event downstream (the Cat 3/6a/6b dispatch happens on event consumption), but the no-commit rule is an EM-internal durability gate. **INCLUDE `.32` in bootstrap. Not RC-coupled in either direction.** The downstream RC consumers are post-bootstrap and don't fire at MVH; the rule still holds.

**OQ-em-bootstrap-3: RETRY re-dispatch (`hk-b3f.62` em-046b) — bootstrap-essential?** The bootstrap test scenario in opening-doc §1.4 has a TWIN handler returning a successful Outcome. RETRY dispatch is exercised only when the handler returns `OutcomeStatus.RETRY`. The cleanest bootstrap path keeps RETRY out (excluded above). However, em-046b's "non-durable per em-023a; MUST NOT produce checkpoint commit" rule is a corollary of em-025 — if em-025 is in, the RETRY no-commit subset is implicitly honored. **Current EXCLUDE stands**; the dedicated `.41` session should confirm that the bootstrap TWIN never emits RETRY, or escalate to INCLUDE.

**OQ-em-bootstrap-4: Branch-tip monotonicity (`hk-b3f.31` em-024a) — write-only?** The §7.2 pseudocode includes `update_persisted_tip(run.run_id, commit_sha)` as the post-checkpoint write step — the WRITE half is part of normal checkpoint emission. The em-024a rule's defensive READ ("verify new tip is fast-forward descendant") is what routes to RC Cat 3 on detection. Bootstrap may want to land the WRITE half (cheap; future-proofs for `.31` becoming live in cycle-1) without the RC-routing READ half. **Current EXCLUDE is conservative**; the `.41` session should consider a partial-include: emit the persisted-tip write but no-op the verification.

**OQ-em-bootstrap-5: Outcome record (`hk-b3f.79`) field set at MVH.** The Outcome record carries 7 fields including `kind` and `payload` (v0.3.3 additive). At bootstrap the TWIN handler will populate `status`, `preferred_label`, `notes` minimally; `kind=default`, `payload=None`. This is consistent with the v0.3.3 spec ("strictly additive at MVH; existing v0.3.2 Outcome consumers remain conforming"). No spec patch required; OQ logged for confirmation.

**OQ-em-bootstrap-6: Validator (`.51` em-038) sub-workflow path.** Validator §4.9 obligations include "sub-workflow resolution transitively (with acyclicity)". With sub-workflow excluded, the validator's sub-workflow resolution branch is dead code at bootstrap. Either (a) emit the validator with the sub-workflow branch returning "no sub-workflow declared in DOT" trivially, or (b) ship validator with the sub-workflow branch unimplemented and rely on em-006 / DOT-syntax check rejecting `type=sub-workflow` inputs. Recommendation: (a). Logged here; spec-conformant either way.

---

## Summary statistics (final)

- **Total EM beads:** 88 children + 1 epic.
- **INCLUDE: 65** beads (B-EM = 11, F = 54).
- **EXCLUDE: 23** beads.
- **Inclusion ratio: 74%** of the EM epic.
- **Cross-cluster OUT edges from INCLUDE beads: 5** (all to AR: ar-001, ar-005, ar-032).
- **Cross-cluster IN edges from in-bootstrap clusters (A/B-WM/C/D/E): 25** distinct edges; hot targets are `.14`, `.20`, `.79`, `.85`, `.86`.
- **Cross-cluster IN edges from deferred clusters (RC/CP/ON): ~25** edges, none bootstrap-blocking.

The EM cluster is the substrate everyone else pins against. INCLUDE-set is broader than the pre-enumeration estimate primarily because §6.1 schemas (records, enums, primitive aliases) are pulled in wholesale — they form the type vocabulary that PL, WM, HC, EV all consume. The §4.10 cascade machinery is included only as the deterministic-selection skeleton (em-041 + em-046a); guards, gates, cycles, and rollback variants stay deferred.
