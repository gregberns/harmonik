# CP Pilot — Decomposition-Quality Review (r1)

`reviewer: decomposition-quality` · `date: 2026-04-30` · `pilot-version: 0.1.0` · `discipline-version: 0.9` · `spec-version: 0.3.2`

## 1. Sample selection

15 beads sampled per pilot-review-protocol §3.2:

- **Coalesce** (1, mandatory): `cp-004` (CP-004 + CP-005 → cp-004)
- **§2.1a collapse** (1, mandatory): `cp-031` (CP-052 collapsed onto cp-031)
- **Sensor / invariant** (3, all active): `cp-inv-001`, `cp-inv-002`, `cp-inv-003`
- **Schema beads** (3 — high-density §6.1 stress per F-pilot-CP-5): `cp-schema.control-point`, `cp-schema.hook-verdict-record`, `cp-schema.budget-payload`
- **First-class §4 reqs** (7 — weighted to §6.1 type-anchors and §8 routing-to-EM): `cp-007`, `cp-013`, `cp-015`, `cp-023`, `cp-027`, `cp-034b`, `cp-040a`, `cp-049`

(One over count: 8 first-class reqs sampled to cover all 5 consumer→`em-error.taxonomy` edges plus the §6.1 schema-density stress.)

## 2. Per-bead findings

### 2.1 Coalesce — `cp-004` (CP-004 + CP-005)

**Q1 description faithfulness.** Description enumerates the four Kinds and per-Kind contracts (trigger, evaluator I/O, outcome enum, boundary rule) and states they MUST match the §4.1 table. Spec body (CP-004): "MUST match the table in §4.1.CP-005. No ControlPoint may deviate from its Kind's row." Spec body (CP-005): the table itself, rows keyed by Kind. Description carries the table substance; "Mechanism OR cognition" / "Mechanism only" boundary classifications match the spec table verbatim. **Faithful.**

**Q2 coalesce soundness (§2.3 three-AND test).**

1. Single data shape / code path: YES — CP-005 IS the table CP-004 references; in any plausible Go implementation both checks live in one `validateKindRow(...)` body (the CP-004 normative claim and the CP-005 row data are inseparable inputs to the validator).
2. Anchor-and-clarifications: YES — CP-004 is the anchor ("per-Kind semantics MUST match the table"); CP-005 IS the clarification (the table contents).
3. Splitting reduces to "see anchor": YES — a `cp-005` bead alone would have description "the table referenced by CP-004"; a `cp-004` bead without CP-005 would be untestable.

**Sound coalesce.** `local` lane.

### 2.2 §2.1a collapse — `cp-031` (CP-052 → notes line)

**Q3 collapse soundness (§2.1a three-AND triggers).**

Spec CP-052 body verbatim: "The Beads-CLI skill per [docs/foundation/components.md §10.9] MUST be a default skill in every MVH-required role per §4.6.CP-031. Any agent requiring Beads queries or status updates depends on its presence; node-level declaration supplements the role default when additional skills are needed."

1. One sentence (or two, second being navigational): TWO sentences. First is the verbatim restatement of CP-031's normative claim; second is a one-line "supplements" clarifier. Borderline-pass on (a). The clarifier IS arguably a "see also" navigational pointer (it merely repeats CP-049's node-level option without re-asserting it).
2. Single in-spec inline cite: YES — explicit `§4.6.CP-031`. The `[docs/foundation/components.md §10.9]` cite is to a non-spec doc and is NOT counted.
3. No substantive impl distinct from cited rule: YES — whatever code makes CP-031 hold also satisfies CP-052 (no new mechanism is introduced).

**Sound collapse.** Same shape as AR-035 → ar-026 precedent. `local` lane.

**MINOR (cosmetic).** The collapse description on cp-031 reads cleanly; no concern. The pilot also surfaces CP-031's two non-spec cites (`docs/foundation/components.md §10.9` and forward:bi-NNN) consistently with F-pilot-CP-9. No finding.

### 2.3 Sensors (Q5)

**`cp-inv-001`** — Spec body: "Every ControlPoint observable by the daemon MUST be resolvable through the §4.9 registry." Sensor description: "invariant test asserts registry-lookup uniqueness via cross-subsystem inspection (no shadow store)." This is a real verification mechanism (cross-subsystem inspection check that no subsystem-local cache exists), not a restatement. §10.2 group CP-043..CP-046 confirmed in spec §10.2 prose. Predecessors: cp-043, cp-044, cp-045, cp-046, cp-test.registration-fixture. All §10.2 source-1 confirmed. **Faithful sensor.**

**`cp-inv-002`** — Spec body: "Every ControlPoint effect that crosses a subsystem boundary MUST be observable through one of the typed events declared in §4.10.CP-048." Sensor description: "cross-subsystem tests verifying no ControlPoint effect crosses a boundary by any path other than typed event." This is the §10.2 prose ("Cross-subsystem tests verifying no ControlPoint effect crosses a boundary by any path other than a typed event") restated; verified against §10.2 group CP-047..CP-048. Predecessors cp-047, cp-048. **Faithful sensor.**

**`cp-inv-003`** — Spec body: replay-must-consume-persisted-verdict-on-hash-match; replay-must-Cat-6-on-mismatch; never silently re-invoke. Sensor description names FOUR concrete tests: envelope-hash determinism; hash-match-replay-reads-persisted; hash-mismatch-Cat-6-escalation; Cat-6 stale-verdict re-invocation. These are genuine verification mechanisms. §10.2 group CP-039..CP-042 confirmed. Predecessors cp-039, cp-040, cp-040a, cp-041, cp-042, cp-test.replay-safety-harness, forward:rc-NNN. **Faithful sensor.**

**No sensor findings.**

### 2.4 Schemas (Q6)

**`cp-schema.control-point`** — Description: "9-field RECORD: name; kind; trigger; evaluator; outcome_action; payload (KindPayload); axes (AxisTags per AR §4.1); mode_tag; schema_version (N-1 per CP-038)." Spec §6.1 RECORD ControlPoint declares exactly these 9 fields. **Field/name list complete and correct.**

**`cp-schema.hook-verdict-record`** — **MINOR (cosmetic field-count mismatch).** Description: "7-field RECORD: hook_name; invocation_id; side_effect; failed; reason; cognition_meta; input_envelope_hash; produced_at." That is 8 names listed, not 7. Spec §6.1.6 declares HookVerdictRecord with 8 fields (the listed names). The names are correct and complete; the count prefix is off-by-one. Implementation will not diverge — the field names drive the impl, not the prose count. `local` lane. Cosmetic; can be patched with the next pilot revision (`7-field` → `8-field`).

**`cp-schema.budget-payload`** — Description: "5-field RECORD: resource; scope; limit (positive int); warning_threshold (default 0.8 per CP-022); scope_target." Spec §6.1.4 RECORD BudgetPayload declares exactly these 5. Edge to `em-error.taxonomy` (consumer direction) is correct per §2.11(c.2). **Faithful.**

### 2.5 First-class §4 req beads

**`cp-007`** — Description states attach points (4 enum values), declaration-order short-circuit on first non-allow verdict, registration-failure on missing attach. Spec body matches. The "EM §4.10 cite is supporting per F-pilot-CP-4" notes-line correctly applies §3.1 supporting-cite rule. **Faithful.**

**`cp-013`** — Description enumerates the 8 MVH-baseline on_* trigger names; `on_*` prefix distinguishing Hook-namespace; subsystem-envelope extension path; unrecognized trigger fails registration. Matches spec body verbatim on the 8-name list. **Faithful.** F-pilot-CP-7 reasoning (cite:wide-fanout still fires because cite text uses `§8` form) is correctly applied.

**`cp-015`** — Description: typed failure descriptor; per-Hook MUST NOT halt unless `halt_on_failure = true`; timeout/resource → ErrTransient; schema-violation/registration → ErrDeterministic; wrapping done by S05 dispatcher per HC §4.5. Spec body matches. Edge `cp-015 → hc-error.taxonomy + hc-020` direction-correct per §2.11(c.2). **Faithful.**

**`cp-023`** — Description: dispatch-time check (pre-exhaustion); pending exceeding remaining → emit `budget_exhausted` + DENY (handler NOT launched); failure class `budget_exhausted` per EM §8.5. Spec body matches. Edge `cp-023 → em-error.taxonomy` direction-correct. **Faithful.**

**`cp-027`** — Description: reconciliation wall-clock outer-bound; registers as Budget `wall_clock_seconds`/`per_run`/`scope_target=<reconciliation_run_id>`; inner Budgets MUST NOT extend beyond outer; conflict → DENIED with `budget_exhausted`. Spec body matches. F-pilot-CP-9 (no-edge for `docs/foundation/components.md §9.4a`) correctly applied; forward:rc-NNN logged. Edge `cp-027 → em-error.taxonomy` direction-correct. **Faithful.**

**`cp-034b`** — Description carries 4 normative pieces: (i) primary AST-step ceiling, (ii) secondary wall-clock soft-cap, (iii) `ErrDeterministic` abort + durability pair (event reaches JSONL BEFORE wrapper returns), (iv) crash-between-abort-and-durability replay rule. Plus the `bound_fired` discriminator's normative content (§4.7.CP-034b spec body) is implicit but carried by the forward:ev-events.policy-expression-exceeded-cost edge.

**Q4 multi-step soundness (§2.2) — POSITIVE confirmation.** Pilot's claim ("only 2 steps; signal 1 fails → single bead with sub-bullets") is correct. Spec body lists "Primary bound" + "Secondary bound" — exactly 2 steps; §2.2 signal 1 (≥3 steps) FAILS; pilot correctly does NOT split. **Single bead is sound.** `local` lane (mechanical application of §2.2).

Edge `cp-034b → hc-error.taxonomy + hc-020` direction-correct.

**MINOR (description completeness — cosmetic).** Description omits the `bound_fired ∈ {ast_steps, wall_clock}` discriminator field that the spec calls out as a load-bearing event-payload requirement. Discriminator carries: "Operators diagnosing cost-ceiling crossings depend on this discriminator; re-adding it post-MVH is a breaking event-payload change." The pilot description names the abort, the durability pair, the replay rule, but not the discriminator field — a future implementer reading only the bead might miss it. The forward:ev-events.policy-expression-exceeded-cost edge points at the EV row that will own the payload schema, so impl will recover the requirement; non-blocking but worth a one-clause add. `local` lane.

**`cp-040a`** — Description carries the 5-input envelope (expression_text / prompt_template / skill_packages / context_subset / policy_meta) with sub-bullets, the AST-walk vs whole-context fallback declaration, the SHA-256-over-canonical-JSON algorithm. Matches spec body §4.8 CP-040a verbatim on all 5 inputs.

**Q4 multi-step soundness — POSITIVE confirmation.** §2.2 signal 1 (≥3 steps) FIRES (5 inputs). F8b shared-function-body tiebreaker correctly applied: in any plausible Go impl the 5 inputs are computed-canonicalized-hashed in one cohesive `func computeInputEnvelopeHash(...)` body. **Single bead with sub-bullets is sound.** Same posture as EM-016 git atomic sequence. `local` lane (correct mechanical F8b application).

**`cp-049`** — Description: node `required_skills` declaration as comma-list OR YAML `policy_ref`; ingest-time syntactic-validity vs launch-time package-resolution covering-partition split; ErrDeterministic on syntactic violation. Spec body matches. Edge `cp-049 → hc-error.taxonomy` direction-correct. **Faithful.**

## 3. Direction sanity check (CP-specific)

Per the prompt's specific check: verify the 5 consumer→`em-error.taxonomy` edges are direction-correct per discipline §2.11(c.2) v0.9 anti-pattern.

Yaml-confirmed edge list (`cp-pilot-data.yaml` lines 1115–1126 + 1169–1181):

- `cp-015 → hc-error.taxonomy` ✓
- `cp-023 → em-error.taxonomy` ✓
- `cp-027 → em-error.taxonomy` ✓
- `cp-034b → hc-error.taxonomy` ✓
- `cp-049 → hc-error.taxonomy` ✓
- `cp-schema.budget-payload → em-error.taxonomy` ✓ (schema-cite consumer)

All five named consumer edges + the schema edge fire `<req> → <spec>-error.taxonomy`, consumer-blocks-on-vocabulary-owner. None invert. **CP got the direction right at draft time** — confirms the pilot author internalized the discipline v0.9 §2.11(c.2) clause and the F-pilot-CP-1 lens at draft authorship; no HC v0.1.0 → v0.1.1-style retrofit needed.

## 4. Missing-coalesce smell (§3 of method)

**Per-Kind §6.1 payloads (Gate / Hook / Guard / Budget — 4 schema beads).** Pilot kept these as 4 separate schemas. §2.3 three-AND test:

1. Single data shape / single code path: FAILS — each payload has different field counts and different domain semantics (Gate's attach-point + subtype + approver/verification refs vs Hook's trigger/filter/side-effect-kind vs Guard's lone applies-to-node vs Budget's resource/scope/limit/threshold/scope-target). They are PEER discriminator branches of `KindPayload`, not anchor-and-clarifications.
2. Anchor-and-clarifications: no anchor — no Kind's payload is a clarification of another's.

**Two-of-three insufficient (mirrors F-em-r1-MIN-8 typed-alias-cluster precedent).** Splitting into 4 schema beads is correct.

**§4.2 / §4.3 / §4.4 / §4.5 per-Kind §4 reqs (cp-006..cp-011 / cp-012..cp-017 / cp-018..cp-021 / cp-022..cp-027).** Pilot kept each as separate first-class beads. Each addresses orthogonal concerns within its Kind (firing trigger / attach point / subtype declaration / denial behavior / invocation owner / cognition tagging). Pattern matches the §2.3 BI-025a..BI-025e counter-example: shared invocation surface but orthogonal independently-testable concerns. Splitting is correct.

**No missing-coalesce findings.**

## 5. Over-split smell (§4 of method)

The prompt singled out two candidates:

- **CP-034b's 2-bound list.** Pilot did NOT split (§2.2 signal 1 fails on 2-step). Correct (see §2.5 above). No over-split.
- **CP-040a's 5-input envelope.** Pilot did NOT split (F8b shared-function-body tiebreaker). Correct (see §2.5 above). No over-split.

Neither bead was emitted as multi-step umbrella + step beads; the pilot's claim ("0 §2.2 multi-step splits") holds.

**Other near-candidates** worth scanning:

- **cp-013** (8 on_* names) is one bead with sub-bullets — correct (signal 2 fails; the trigger names share the registry-keyed dispatch path).
- **cp-016** at-least-once + idempotency_class branch is one bead — correct (the at-least-once floor and the idempotency-class declaration share the §4.3 Hook dispatch surface).
- **cp-035** (7 required policy YAML sections) is one bead — correct (validation-envelope of every Kind sits in one document-shape rule).

**No over-split findings.**

## 6. Findings summary

| Finding | Bead | Severity | Lane | Justification |
|---|---|---|---|---|
| Field count says "7-field" but lists 8 fields | `cp-schema.hook-verdict-record` | MINOR | local | Cosmetic count prefix; field-name list is correct and complete; impl will not diverge |
| `bound_fired` discriminator omitted from description | `cp-034b` | MINOR | local | Description carries primary impl rule; discriminator surfaces via forward:ev-events.* edge to EV row bead; non-blocking but worth a one-clause add |

**No BLOCKER or MAJOR findings.**

## 7. CLEAN-summary line

The CP pilot's decomposition is structurally sound: the 1 §2.3 coalesce passes the three-AND test; the 1 §2.1a collapse passes the three triggers; both candidate multi-step splits (CP-034b 2-bound, CP-040a 5-input) are correctly resolved to single beads (signal-1 fail and F8b shared-function-body tiebreaker respectively); 3 sensors carry real verification mechanisms not invariant restatements; 24 schema beads have correct field counts (1 cosmetic count-prefix typo) and complete declarations; all 5 consumer→`em-error.taxonomy`/`hc-error.taxonomy` edges run consumer-blocks-on-owner per discipline v0.9 §2.11(c.2). **2 MINOR findings, both `local`, both cosmetic. No BLOCKER. No MAJOR.**

CP-pilot ready to proceed to synthesis with the 2 MINORs filed against pilot v0.1.1 if the author elects to apply them.
