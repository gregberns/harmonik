# Decompose-to-Tasks Discipline

`discipline-version: 0.9` — drafted 2026-04-25 against the v0.4.x foundation corpus. v0.4 applied a 6-finding pass from the BI smoke-load against live `br` v0.1.45 (see [bi-smoke-load-findings.md](bi-smoke-load-findings.md)): added a mnemonic-vs-assigned-ID translation rule (§2.10), forbade step→umbrella explicit `blocks` edges (§2.2 F11), emphasised sensor↔impl one-way edges (§2.5), disambiguated bidirectional inline cites (§2.7), acknowledged Beads's default `P2` priority (§2.9), and fixed the corpus workspace as a single `.beads/` with prefix `hk` (new §2.12). v0.5 applied six further findings from AR's r1 review (see [docs/reviews/2026-04-27-ar-pilot-r1/synthesis.md](../reviews/2026-04-27-ar-pilot-r1/synthesis.md)): (1) §2.1 cross-reference exception (F-pilot-AR-1), (2) §2.6 primitive-shape schema category (F-pilot-AR-3), (3) §2.7 F13 default-resolution heuristic for slot-rule/content-rule pairs (F-pilot-AR-8), (4) §3.1 term-use edge rule (F-pilot-AR-2.5/2.6/2.7), (5) §3.1 no-edge list expansion (F-pilot-AR-AO1/AO2/AO3), (6) §2.5 §10.2 reviewer-persona-bundling sensor-edge clarification (F-pilot-AR-2.8). v0.6 applies four further findings — three documentation-tightening from the AR pilot's r2 review (see [docs/reviews/2026-04-27-ar-pilot-r2/synthesis-r2.md](../reviews/2026-04-27-ar-pilot-r2/synthesis-r2.md)) and one NEW MECHANICAL RULE surfaced when the v0.2.1 load attempt hit a 3-cycle: (1) §3.1 step 1 supporting-cite-vs-hard-dep clause (F-pilot-AR-10, NEW MECHANICAL RULE), (2) §3.1 step 5 invariant-as-target exemption (F-pilot-AR-r2-2), (3) §2.5 reviewer-persona-bundling tightening (F-pilot-AR-r2-3), (4) §2.5 three-sensor-edge-source enumeration (F-pilot-AR-r2-4). v0.7 applies a 10-finding pass from the EM pilot's r1 review (see [docs/reviews/2026-04-27-em-pilot-r1/synthesis.md](../reviews/2026-04-27-em-pilot-r1/synthesis.md)) plus two policy decisions (Option E §4.a envelope grandfathering, Option G VerdictPayload type-alias policy). One behavioral patch (F-em-r1-MAJ-1) extends §3.1 step 5 with an invariant-body term-use sub-clause and grows §2.5's §10.2 enumeration from three sources to four; the rest are documentation-tightening clauses landing in §2.2 (F8b behavioural-spec example), §2.5 (sensor-predecessor degeneracy + `gated-by-corpus-scale` tag), §2.6 (type-alias-resolves-to-single-MVH-variant; typed-alias clusters), §2.7 (F13 declaration-rule ↔ method-rule worked example), §2.11 (registry-row dual-ownership extension), §3.1 (rule-precedence between F-pilot-AR-10 and F-pilot-AR-r2-2; explicit §7 prose no-edge entry), §3.1.3 (forward-deferred wide-fanout tag policy), and §3.2 (§4.a envelope grandfather carve-out). v0.8 applies a 5-patch pass from the EV pilot's r1 review (see [docs/reviews/2026-04-28-ev-pilot-r1/synthesis.md](../reviews/2026-04-28-ev-pilot-r1/synthesis.md)): (1) §3.2 §4.a envelope grandfather carve-out extended to include EV as a same-day boundary case (F-pilot-EV-4); (2) §2.5 F10 sensor→sensor explicit-ID-cite clarification (F-refs-EV-6); (3) §2.11(c.1) §6.3-payload-co-located-with-§8-row clause (F-pilot-EV-1); (4) `post-mvh` transient-tag definition in §3.1 (F-pilot-EV-2); (5) §2.11(d.2) event-row dual-ownership cross-reference (F-pilot-EV-7). All five are documentation-tightening; no behavioural changes. v0.9 applies one class-lane finding from the HC pilot's r1 review (see [docs/reviews/2026-04-30-hc-pilot-r1/synthesis.md](../reviews/2026-04-30-hc-pilot-r1/synthesis.md)): a new §2.11(c.2) clause + anti-pattern note nailing down the edge direction for `<spec>-error.taxonomy` (vocabulary owner, NOT slot rule). The same anti-pattern surfaced 3× across pilots (EM r1 #15, EV r1 #15, HC r1 — caught at LOAD this time, after 5 Beads cycle rejections); the documentation-only patch makes the direction unambiguous so CP/WM/PL/ON/RC do not repeat it. See §6 revision history.

> Companion files: [bi-pilot.md](bi-pilot.md) v0.1.3 — the first instance of this discipline applied to `specs/beads-integration.md` (BI). [bi-smoke-load-findings.md](bi-smoke-load-findings.md) — first execution against live `br`, source of v0.4 deltas. Read the discipline first, the pilot second.

> Tooling: pilots from HC onward are loaded via `scripts/load-pilot.py` against a `<spec>-pilot-data.yaml` file (sibling of the pilot markdown). Schema and usage at [loader-tooling.md](loader-tooling.md). Earlier pilots (BI, AR, EM, EV) used hand-coded Python load scripts — already loaded in DB; YAML re-conversion is out of scope.

> **Convention.** Throughout this doc, identifiers like `bi-001`, `bi-030.s4`, `bi-schema.intent-log-entry` are *mnemonic plan-level names* used in prose and worked examples. Live Beads assigns its own IDs at create time in the form `<prefix>-<base36-suffix>` (top-level) and `<parent-id>.<n>` (children, when `--parent` is passed). See §2.10 for the mnem→assigned-ID translation rule and §2.12 for the corpus prefix.

## 1. Purpose

Define the rules by which a normative spec's `§4` requirements (and `§5` invariants, `§6` schemas, `§8` errors) become a concrete, Beads-loadable task set ("bead set"). The bead set is the Phase 1 implementation backlog: each bead names a unit of code-or-test work that one engineer or one agent can carry from start to a verifiable terminal state.

The discipline must produce repeatable output: a second author applying these rules to the same spec MUST produce a bead set that differs only in cosmetic detail (titles, prose), not in shape (count, edges, tags). The pilot's role is to expose where the rules are still ambiguous; ambiguities surface as Open Questions in §6.

What this discipline does NOT do:

- It does not write code. It produces work items; implementers do the work.
- It does not re-derive the spec. The spec is the only normative input; this discipline is mechanical.
- It does not add abstraction. One requirement → one or more tasks; never one requirement → one task → many sub-tasks → many sub-sub-tasks.

---

## 2. Decomposition rules

Each rule is a single declaration the author MUST follow, plus 2–3 worked examples drawn from BI.

### 2.1 Granularity rule — one requirement, one task (default)

**Declaration.** Every numeric or letter-suffixed requirement ID (`<prefix>-NNN`, `<prefix>-NNNa`) maps to **exactly one bead** by default. The bead's title is the requirement's heading; the bead's description cites the requirement ID and summarises what to build in 1-2 sentences.

This default holds regardless of whether the requirement names "shape" (a record, an interface) or "behavior" (a protocol step). The bead is the unit of work; whether it produces a struct definition, a function body, a config knob, or a test fixture is left to the implementer.

**Exceptions** (each governed by a sub-rule below):

- **Multi-step protocols (§2.2)** — one requirement → one umbrella bead + N step beads.
- **Coalescible cluster (§2.3)** — multiple closely-bound requirement IDs collapse into one bead.
- **Pure declaration (§2.4)** — a requirement that declares only a structural fact and is fully covered by a §6 schema bead becomes a `notes` line on the schema bead, not its own bead.
- **Pure cross-reference requirement (§2.1a)** — a requirement whose normative body is a verbatim "see §N.OTHER-NNN" pointer with no independent normative content becomes a notes line on the cited bead, not its own bead.

**Worked examples (BI):**

- BI-002 ("All Beads interactions route through the `br` CLI") — one bead. The work is wiring the adapter constructor + a lint check; one engineer-day at most.
- BI-013 ("Ready-work query") — one bead. Adapter method `Ready() ([]BeadID, error)` + parse `br ready --format json`.
- BI-024 ("Beads version is pinned per harmonik release") — one bead. The pinning is a manifest entry + a release-engineering check; one bead covers both.

### 2.1a Pure cross-reference exception

**Declaration.** When a requirement's body is exactly a pointer ("Role is orthogonal to agent type — see §4.7.AR-026"), it does not warrant its own bead. The pointer is a navigational aid, not work. Collapse to a `notes:` line on the cited bead.

**Triggering rule.** All three conditions must hold:

1. The body is one sentence (or two, the second being a "Cross-references the normative rule for X" style pointer).
2. The body inline-cites exactly ONE other requirement in the same spec.
3. No `Tags:` field declares mechanism/cognition with substantive implementation work distinct from the cited rule.

**Worked example (AR):** AR-035 ("Role is orthogonal to agent type. See §4.7.AR-026 for the normative rule; this subsection cross-references it for role-taxonomy locality.") collapses to a `notes:` line on `ar-026`. The AR-035 ID's `req:AR-035` tag remains on `ar-026` as a tag (so `req:AR-026, req:AR-035` co-exist on the same bead, mirroring the §2.3 coalesce treatment). (F-pilot-AR-1)

### 2.2 Multi-step protocol rule — umbrella bead plus per-step beads (guideline)

**Declaration.** This is a **guideline**, not a hard rule. When a requirement contains a numbered step list, the per-spec author decides whether each step warrants its own bead based on three signals — **split when ALL three hold**:

1. **≥3 steps** in the protocol.
2. **Independent testability** — each step is breakable on its own (e.g., a power-loss test that targets one fsync, an exit-code branch that classifies one error class).
3. **Umbrella loses meaning when stripped** — describing the protocol without enumerating the steps would lose enough detail that an implementer would have to re-derive them from the spec.

If any signal fails, the requirement stays as one bead. The author should **err toward fewer beads** — step beads are useful when the steps are independent work; they are noise when the steps are one cohesive function with checkpointed control flow.

When split: the umbrella bead is the protocol contract; step beads are `parent-child` of the umbrella; the umbrella cannot close until all steps close. Cross-spec consumers depend on the umbrella, not on individual steps.

**(F8a) Depth ceiling.** Step beads do not themselves split. A step that contains a sub-protocol (e.g., PL-005 step 0 mints UUID + writes file + registers subsystems) stays as one step bead; the sub-actions are description bullets, not sub-step beads. Maximum hierarchy: spec-parent → req-umbrella → step.

**(F8b) Sub-case tiebreaker.** When a step contains lettered sub-cases (e.g., BI-031 step 4's 4a–4f BrError branches), apply the tiebreaker: if the sub-cases share a single function body / state machine, they are sub-bullets in the step bead's description; if they are independent code paths with independent failure modes, they are step beads. BI-031 step 4 → sub-bullets (shared reissue state machine).

**(F8b) Behavioural-spec worked example (F-em-r1-MIN-7 / F-pilot-EM-1).** The shared-function-body tiebreaker also fires on whole-requirement sequences in behavioural specs, not just on lettered sub-cases. Worked example: EM-016's three-step git atomic sequence — `git write-tree` → `git commit-tree` → `git update-ref`. All three §2.2 split signals fire (3 steps, each independently testable in principle, the umbrella loses meaning when stripped); but in any plausible Go implementation the three ops sit in one cohesive function body (`func checkpoint(...) { writeTree(); commitTree(); updateRef() }`) with no stable testable boundary between them. F8b applies → keep ONE bead with sub-bullets, do NOT mint step beads. Same shape applies to EM-038's six-category validator (one classifier function body, six branches). Both stay as single beads with sub-bullets.

**(F8c) Constraint-requirements adjacent to umbrellas.** When a separate requirement constrains an umbrella's steps (e.g., ON-027a is atomicity over ON-027's 8 steps; ON-029 is per-step timeouts), the constraint requirement gets ITS OWN bead with a `blocks` edge from the umbrella (the umbrella cannot close until its constraints are met). The constraint bead does NOT itself mint step beads — the steps already exist under the umbrella.

**(F11) Step→umbrella edges are implicit.** When a step bead is created with `--parent <umbrella-id>`, Beads materializes a `parent-child` dep edge that the cycle detector treats as a dependency. Step beads MUST NOT add an explicit `blocks` edge to their umbrella — `step blocks-on umbrella` plus the parent-child edge produces a cycle, and Beads rejects the second edge. Sequencing between step beads (e.g., `bi-030.s2 blocks-on bi-030.s1`) IS expressed via explicit `blocks` edges; only the step→umbrella relationship is implicit. (Origin: BI smoke-load 2026-04-27 — every `bi-NNN.s1 → bi-NNN` edge in pilot v0.1.2 was rejected by Beads as a cycle.)

**Worked examples (BI):**

- BI-031 ("Idempotent crash-recovery") — 5 steps, each independently testable. All three signals hold → split. 1 umbrella + 5 step beads. The 4a–4f BrError sub-cases inside step 4 are sub-bullets on `bi-031.s4`, not their own beads (they share the reissue state machine).
- BI-030 ("Pre-write intent log with fsync durability") — 5 create steps + 1 delete step. Each fsync is independently breakable on power-loss tests. All three signals hold → split. 1 umbrella + 6 step beads (the highest fan-out in the BI pilot).
- BI-024a ("`br --version` handshake") — 2 steps (regex check + comparison), shared state. Signal 1 fails → stays one bead.

### 2.3 Coalescible-cluster rule — multiple requirements → one bead

**Declaration.** A cluster of requirement IDs collapses into a single bead when ALL of the following hold:

1. The requirements share a single data shape or single code path (e.g., all describe the same enum, the same table, the same INTERFACE method).
2. The earliest requirement in the cluster is the "anchor"; the remaining requirements are clarifications, sub-cases, or table rows that the anchor's implementer cannot omit.
3. Splitting them produces beads whose descriptions reduce to "see the anchor bead."

When coalescing, the bead's ID is the anchor's (`bi-010` for the BI-010 + BI-010a + BI-010b cluster). The bead's description enumerates the sibling IDs covered ("Implements BI-010, BI-010a, BI-010b — write surface + status-mapping table + reconciliation-driven write carve-out").

**Worked examples (BI):**

- BI-010 + BI-010a + BI-010b → one bead (`bi-010`). All three describe the write surface: BI-010 declares the three ops, BI-010a tabulates run-event → coarse-status mapping, BI-010b carves out reconciliation-driven writes. An implementer building BI-010 cannot skip the table or the carve-out without producing a non-conformant adapter.
- BI-008 + BI-008a → one bead (`bi-008`). BI-008 declares ID stability; BI-008a clarifies scoping and opacity. They are inseparable.
- BI-025a + BI-025b + BI-025c + BI-025d + BI-025e → five beads, NOT one. They share the `br` invocation surface but address orthogonal concerns (exit-code classification vs JSON mode vs timeout vs stderr vs concurrency); each is independently testable and independently buggy. The cluster test ("BI-025 family") is a check-list, not a coalescing trigger.

### 2.4 Test-vs-implementation split — combine by default; split only when test predates impl

**Declaration.** A bead carries BOTH the implementation work AND the test obligation cited in §10.2 of the spec. The bead does not close until both land.

A test bead is created as a SEPARATE bead only when the test must exist before the implementation (e.g., a contract-test fixture, a crash-injection harness used by multiple impl beads). In that case the test bead `blocks` the impl beads.

**Rationale.** The spec already names the test obligation per requirement; treating implementation and test as separate beads doubles the bead count without adding planning value. The exception preserves the case where shared test infrastructure is the gating dependency.

**Worked examples (BI):**

- BI-013 ("Ready-work query") — one bead covering adapter method + the unit test that exercises `br ready` JSON parsing. Closes when both green.
- BI-029..BI-032 (idempotency family) — §10.2 cites "crash-injection tests kill the adapter between intent-log fsync and `br` call completion." That harness is shared infrastructure: extract it as a separate bead (`bi-test.crash-harness`) that `blocks` `bi-029`, `bi-030`, `bi-031`, `bi-032`.
- BI-INV-001 ("No intra-run writes to Beads") — the §5 invariant cites a corpus-wide reviewer-persona scan. Extract as a separate bead (`bi-inv-001`) per §2.5 (sensor rule); the scan is cross-cutting infrastructure, not impl-tied.

### 2.5 Sensor / invariant rule — one bead per `BI-INV-NNN`, never combined with §4 beads

**Declaration.** Every `<prefix>-INV-NNN` invariant maps to **exactly one bead**. The bead's work is implementing the invariant's `Sensor:` line — the cross-subsystem check that proves the invariant holds. Sensor beads MUST NOT be merged into the §4 requirement beads they constrain, because:

- Invariants span multiple subsystems by selection test ([spec-template.md] §5).
- Their sensors are typically cross-spec scenario tests, lints, or reviewer-persona scans — different test surface than the per-requirement contract tests.
- Treating an invariant as a sub-task of one requirement under-states which other requirements it constrains.

A sensor bead has `blocks` edges from EVERY §4 requirement bead the invariant constrains (the sensor cannot pass until the constrained requirements are implemented). Edge label note: `A blocks B` means A is the prerequisite; B is gated. Loaded as `br dep add <B> <A> -t blocks` (Beads convention: dependant first, prerequisite second). The discipline never uses `blockedBy` — that label does not exist in Beads's `DependencyType` enum (`blocks`, `parent-child`, `conditional-blocks`, `waits-for`, `related`, `discovered-from`, `replies-to`, `relates-to`, `duplicates`, `supersedes`, `caused-by`).

**Sensor↔impl edges are one-way.** Sensor beads `blocks-on` impl beads. Impl beads do NOT block-on their sensors — implementation is independent of verification. A pilot table that lists both directions (e.g., `bi-011 → bi-inv-001` *and* `bi-inv-001 → bi-011`) has a bug: the impl→sensor entry is wrong-direction and must be removed. (Origin: BI smoke-load 2026-04-27 — Beads correctly rejected the reverse edges as cycles.)

**Invariant beads as edge targets (F10).** Invariant beads MAY be the target of cross-spec edges when a downstream requirement EXPLICITLY cites the invariant by ID (e.g., `RC-INV-004`). By default, downstream consumers edge to the constrained §4 requirements, not to the invariant. Edging to the invariant is appropriate when the consumer's correctness depends on the invariant's sensor passing, not on any individual constrained requirement.

**(F-refs-EV-6, v0.8) Sensor→sensor explicit-ID-cite extension.** F10 applies symmetrically to sensor→sensor (invariant-bead-to-invariant-bead) edges. When an invariant body explicitly cites another invariant by `<prefix>-INV-NNN` ID, a sensor→sensor `blocks` edge fires from the citing invariant's sensor bead to the cited invariant's sensor bead. The cross-spec form requires the cited invariant's spec to be in the citing spec's `depends-on`. The §2.5 F12 sensor↔impl one-way rule does NOT apply to sensor↔sensor — both invariants are the same kind of bead (sensor-bead) and the citation direction is determined by the body cite, not by the impl/sensor distinction; the F-pilot-AR-r2-2 invariant-as-target exemption is impl→invariant-specific and does not fire for invariant→invariant. Worked example: `ev-inv-001 → em-inv-001` per EV-INV-001's body explicit ID cite to EM-INV-001 (`Git plus Beads is authoritative per [execution-model.md §4.7 EM-INV-001]`); EV is in EM's reciprocal direction and EM is in EV's `depends-on`, so the edge fires.

**§10.2 sensor-edge sources (F-pilot-AR-r2-4; extended by F-em-r1-MAJ-1 to a fourth source).** A spec's §10.2 conformance section names test-surface obligations that gate the invariant's sensor implementation. The sensor bead's `blocks-on` predecessors derive from FOUR distinct §10.2 patterns; all four patterns produce edges and the sensor bead's predecessor list is the union:

1. **Conformance-group prose cites.** When §10.2 prose names a contiguous range of §4 reqs by group label (e.g., "AR-009..AR-012 group", "AR-005..AR-007 (ZFC) group"), the sensor blocks-on EACH req in the cited range. Worked example: AR-INV-003's §10.2 group is "AR-009..AR-012" → `ar-inv-003` blocks-on `ar-009`, `ar-010`, `ar-011`, `ar-012`.

2. **Reviewer-persona group bundling (F-pilot-AR-2.8 v0.5; tightened by F-pilot-AR-r2-3).** When §10.2 names a reviewer persona that bundles a §5 invariant with one or more §4 requirements ("the conformance-auditor persona checks AR-003, AR-007, AR-042, and AR-INV-001"), the bundled invariant's sensor blocks-on each bundled §4 req. **The bundling trigger requires the specific `<prefix>-INV-NNN` ID to appear in the persona block's named-target list; category phrases like "cross-cutting-invariant violations" or "all invariant violations" do NOT trigger the bundling rule.** Worked example: AR-INV-001 is named explicitly in AR §10.2's conformance-auditor block alongside AR-003, AR-007, AR-042 → `ar-inv-001` blocks-on `ar-003`, `ar-007`, `ar-042` (in addition to other §10.2-text-derived predecessors). Counter-example: AR's `architect` persona checks "cross-cutting-invariant violations and AR-020/AR-021 amendment material-change determinations" — the category phrase "cross-cutting-invariant violations" is not a specific ID, so no bundling-induced edges fire from any AR invariant to AR-020/AR-021.

3. **Sensor-block body inline cites.** When the §5 invariant's own body or its `Sensor:` line names a specific §4 req inline ("§4.5.AR-017(a)"), the sensor blocks-on the cited req per §3.1 step 1's standard cite-derivation. Worked example: AR-INV-001's body anchors at "§4.5.AR-017(a)" → `ar-inv-001` blocks-on `ar-017` directly from the body cite.

4. **Invariant-body inline term-use (F-em-r1-MAJ-1).** When the `<prefix>-INV-NNN` invariant's body uses a defined term whose definition is owned by an in-spec or in-`depends-on` requirement, schema, or trailer-registry row — with or without an explicit inline cite — the sensor bead blocks-on the defining bead per §3.1 step 5's invariant-body term-use sub-clause. Sources (1)–(3) cover WHICH §4 reqs the sensor blocks-on transitively via §10.2; this fourth source covers WHICH schemas / impl reqs the sensor blocks-on directly via the invariant body's own term-uses. Worked example: `em-inv-005` body uses `Harmonik-Bead-ID` (owned by `em-schema.checkpoint-trailers`) and the bead-tied-runs concept (owned by `em-014`); both fire as direct predecessors. The reverse (impl req → invariant) does NOT fire — F12 sensor↔impl one-way and the F-pilot-AR-r2-2 invariant-as-target exemption preserve that direction.

The four sources compose: a sensor's predecessor list is the union of all §10.2 group cites, all named-ID persona bundles, all sensor-body inline cites, and all invariant-body term-uses. (Origin: AR pilot r1 surfaced reviewer-persona bundling [F-pilot-AR-2.8]; r2 surfaced the three-source disambiguation [F-pilot-AR-r2-4] and the named-ID-only trigger tightening [F-pilot-AR-r2-3]; EM pilot r1 surfaced the fourth source [F-em-r1-MAJ-1] when `em-inv-005`'s body terms had no §10.2 group/persona/sensor-block coverage but did term-use schemas and impl reqs.)

**Sensor-predecessor degeneracy + `gated-by-corpus-scale` tag (F-em-r1-MAJ-2).** When all four §10.2 sources above return empty AND the invariant's body has at least one cross-spec inline cite to a target outside the spec's `depends-on` (forward-deferred, awaiting reciprocal-pilot edge materialization), the sensor bead carries the transient tag `gated-by-corpus-scale`. The tag is the analog of F6's `gated-by-spec-edit`: informational, visible to `br ready` callers via `br list` filtering, but does NOT itself affect the bead's `draft` status (same posture as `gated-by-spec-edit`). The tag is dropped at edge-fire time when (a) a `depends-on` patch lands and the now-in-corpus target is resolved per sources (1)–(4), or (b) the reciprocal-direction pilot's edge materializes the predecessor. Document the tag's emission alongside `cite:wide-fanout` in §3.1.

**Worked examples (BI):**

- BI-INV-001 → `bi-inv-001`. The sensor is three checks (corpus reviewer scan, contract test on adapter call sites, cross-spec scenario test). Blocked by every BI-010* and BI-012 bead.
- BI-INV-002 → `bi-inv-002`. Cross-spec ID-equality check. Blocked by `bi-017`, `bi-018`, `bi-019`, `bi-020`, plus the cross-spec EM/EV/WM beads that own the carrying surfaces.
- BI-INV-004 → `bi-inv-004`. Cat 3a divergence-detection sensor; blocked by `bi-029`, `bi-030`, `bi-031`, `bi-032` and a cross-spec edge to the RC-014 bead.

### 2.6 Schema / data-shape rule — one bead per record or enum, one per error taxonomy

**Declaration.** Every `RECORD`, `INTERFACE`, `ENUM`, or **constrained primitive type** defined in §6 becomes its own bead. Every `BrError`-style error taxonomy in §8 becomes its own bead. Schema beads are extracted from §4 requirement beads even when a single §4 requirement appears to "own" the schema, because:

- Schemas are consumed by multiple §4 requirements; the schema is the shared dependency.
- Schema evolution rules (§6.3 / §6.4) apply to the schema, not to any one requirement.
- Cross-spec consumers cite the schema (`[beads-integration.md §6.1 IntentLogEntry]`); the cite is a dependency edge to the schema bead.

**Constrained primitive types (F-pilot-AR-3).** A §6 declaration that constrains a built-in primitive (string, integer, etc.) by regex, format, length, range, or enum-of-string-literals is a schema even though it is not formally a RECORD/INTERFACE/ENUM. Naming follows the same `<spec>-schema.<type-kebab>` convention. Tag: `kind:primitive-shape` (alongside `kind:schema`/`kind:interface`/`kind:enum`).

**Type-alias-resolves-to-single-MVH-variant (F-em-r1-MAJ-3 / Option G).** A discriminated-union or named type alias declared INSIDE an INFORMATIVE block (`> INFORMATIVE:` fence) or inside a §A appendix that resolves to a SINGLE owning-spec variant at MVH does NOT trigger the schema-bead rule. The MVH variant's bead (in the owning spec) carries the term; the alias is documentation. No `<spec>-schema.<alias-kebab>` bead is minted; minting one would add a thin redundant layer pointing back at the owning-spec variant. Worked example: EM §6.1's INFORMATIVE block declares `VerdictPayload`, which at MVH resolves to RC's single `VerdictEvent` variant — no `em-schema.verdict-payload` bead; the consumer edge goes to RC's owning bead per the §3.1 type-cite rule. Future pilots evaluate analogous aliases (CP Policy/Gate union, HC error-sentinel union) under this same rule. When the alias resolves to MULTIPLE owning-spec variants at MVH (a real discriminated union), the §2.6 RECORD/INTERFACE/ENUM rules apply normally and a schema bead is minted.

**Typed-alias clusters without anchor structure (F-em-r1-MIN-8 / F-pilot-EM-5).** A cluster of thin typed-ID aliases (e.g., the EM `RunID` / `StateID` / `TransitionID` / `NodeID` / `BeadID` UUID-or-String wrappers) is N peer rules without anchor structure. Apply §2.3's three-AND coalesce test: test 1 (single shape/path) fires — they all wrap the same underlying primitive; test 2 (anchor-and-clarifications) FAILS — there is no anchor of which the others are clarifications, sub-cases, or table rows; test 3 (split reduces to "see anchor") fires. Two-of-three is insufficient; coalesce does NOT fire. Each typed alias stays as its own primitive-shape schema bead. Document explicitly so EV/RC pilots do not re-litigate the cluster.

**Worked examples (BI):**

- `BeadRecord` + `CoarseStatus` + `DependencyEdge` + `EdgeKind` (§6.1) → 4 schema beads (`bi-schema.bead-record`, `bi-schema.coarse-status`, `bi-schema.dependency-edge`, `bi-schema.edge-kind`). `bi-005` blocks-on `bi-schema.bead-record`; `bi-007` blocks-on `bi-schema.coarse-status`; etc. (Loaded as `br dep add bi-005 bi-schema.bead-record -t blocks`.)
- `BrError` enum + `IntentLogEntry` + `TerminalOp` (§6.1a, §6.1) → 3 schema beads. `bi-025a` blocks-on `bi-schema.br-error`; `bi-029` and `bi-030` blocks-on `bi-schema.intent-log-entry`.
- §8 error-taxonomy table → one bead (`bi-error.taxonomy`) capturing the `BrError` → reconciliation-category routing table. Distinct from `bi-schema.br-error` (which is the enum); the taxonomy bead is the routing logic. Blocked by `bi-schema.br-error`; blocks the RC Cat 3a / Cat 0 detector beads.
- AR §6.1 declares `agent_type := ^[a-z][a-z0-9-]{1,62}$` plus four reserved identifiers — a constrained string primitive. → bead `ar-schema.agent-type-identifier`, tag `kind:primitive-shape`. Consumed by every spec that names an `agent_type` field (cross-spec edges from those specs' beads when finalized).

### 2.7 Cross-spec dependency-edge rule

**Declaration.** A bead's edges fall into three classes:

1. **Intra-spec dependencies** (`blocks`) — derived from explicit "see §N" cites within the spec body and from the §2.6 schema rule. Mechanical. Loaded as `br dep add <citing-bead> <cited-bead> -t blocks` (Beads convention: dependant first, prerequisite second; default `-t` is `blocks`).
2. **Cross-spec normative dependencies** (`blocks`) — derived from inline cites to specific requirement IDs in upstream specs. Example: BI-031's body cites `RC-014` → `bi-031` is gated by `rc-014`, loaded as `br dep add bi-031 rc-014 -t blocks`. The cited requirement must be IMPLEMENTED before the citing bead can close.
3. **Cross-spec co-references** — `§9.3 "Co-references"` lists sibling specs that READ this spec's declared surface. **No edge is emitted** from §9.3 entries directly. Co-references are spec metadata describing read relationships at the surface level; they do NOT generate bead-level coupling. If a co-reference matters at the bead level, the corresponding inline cite will produce the edge per (1) or (2).

**Forbidden:** inventing cross-spec edges that the spec does not cite. If BI does not mention an EM requirement inline, no edge crosses to EM, even if the implementer "knows" they're related.

**Bidirectional inline cites are a smell.** If A inline-cites B AND B inline-cites A, the resulting edge graph would have a cycle. When this happens, apply the **slot-rule / content-rule heuristic (F-pilot-AR-8)** before surfacing: if A defines a slot, section, envelope-element, or container rule and B defines the content that fills it, then A→B is the normative dep (the slot points at what fills it) and B→A is informational plumbing ("declare in the slot reserved by §X") — reclassify B→A to no-edge. The slot-rule/content-rule heuristic resolves bidirectional cite cycles mechanically without requiring author judgment.

When the heuristic does not apply (neither side is a slot rule), surface to the discipline author for resolution: usually one of the cites is informational (a `> RATIONALE:` block, a §9.3 co-reference, or a "see also" link) rather than a true dependency, and is reclassified per the no-edge rules above. Cycles are NOT acceptable in the bead set; one or both cites must be reclassified or removed before the bead set loads.

(Origin: BI smoke-load 2026-04-27 — `bi-004 ↔ bi-027` produced a pilot-level cycle; on inspection neither requirement actually inline-cites the other in the BI spec body. AR pilot r1 — AR-013↔AR-053 and AR-052↔AR-053 fired the slot-rule/content-rule pattern twice in the envelope-slot trio; AR-053 is the slot rule, AR-013 and AR-052 fill it; F13 + F-pilot-AR-8 heuristic reclassifies the AR-013→AR-053 and AR-052→AR-053 cites as informational.)

**`waits-for` collapsed to `blocks` at MVH (F4).** Live Beads supports both `blocks` (gating) and `waits-for` (informational, non-gating) edge kinds. The discipline previously distinguished them by a declaration-vs-behavior test (declaration-only deps → `waits-for`; behavioral deps → `blocks`). The pilot showed the test produces inconsistent results on real cites (BI surfaced 3 ambiguous cases). At MVH the discipline uses `blocks` for ALL inline-cite-derived edges — intra-spec, cross-spec, and schema/type cites alike. This is simpler and never wrong; the only cost is occasional over-gating where a downstream could in principle have started against a declaration. Beads's `waits-for` value is reserved for post-MVH adoption when there's operational evidence (dispatch-latency telemetry) that the decoupling materially shortens the critical path.

**Missing inline cite catcher (F5).** If a §9.3 entry's consumer beads cannot be tested without the producer being implementation-complete, the source spec has a missing inline cite. Surface this as a spec-edit task per §2.8 (`kind:spec-edit` bead with the carve-out from F6), not as an invented edge.

**Worked examples (BI):**

- BI-018 cites `[execution-model.md §6.2]` (the trailer format) → `bi-018` blocks-on `em-017`. The trailer format must be defined and validated; declaration alone is insufficient.
- BI-019 cites `[event-model.md §6.3]` (payload shape) → `bi-019` blocks-on `ev-schema.<payload-type>`. (At MVH this is `blocks`; the prior `waits-for` framing is dropped per F4.)
- BI-013 has no inline cross-spec cites; no cross-spec edges. PL's mention in BI's §9.3 produces no edge (per declaration above).

**F13 second worked example — declaration-rule ↔ retrieval-method pattern (F-em-r1-MIN-5).** F13's first worked example (cascade↔shape, em-002↔em-041 / AR-053↔AR-013) covers the slot-rule/content-rule shape. A second sub-pattern is declaration-rule ↔ method-rule: one rule declares a canonical sibling-file path (or other named slot) and a peer rule defines the retrieval method against that path. Worked example: EM-018 declares the canonical sibling-file path that an artifact lives at; EM-019 is the retrieval method against that path. Both inline-cite each other. The slot-rule heuristic resolves to the declaration-rule pointing at the method-rule (`em-018 → em-019`), consistent with "the slot points at what fills/uses it." The reverse cite (`em-019 → em-018`) is informational plumbing and reclassifies to no-edge. RC verdict-record↔verdict-execution and PL startup-sequence↔component-init will retest under the same heuristic.

### 2.8 Tag-mapping rule — spec `Tags:` and `Axes:` become bead tags

**Declaration.** Every bead carries the following tags derived mechanically from the source requirement (or, for schema/sensor beads, from the §4 requirements they support):

- `tag:mechanism` OR `tag:cognition` — copied verbatim from the spec's `Tags:` line. Single value.
- `axis:llm-freedom-<value>` — present iff the spec declared `Axes:` AND `llm-freedom != none`.
- `axis:io-determinism-<value>` — present iff the spec declared `Axes:` AND `io-determinism != deterministic`.
- `axis:replay-safety-<value>` — present iff the spec declared `Axes:` AND `replay-safety != safe`.
- `axis:idempotency-<value>` — present iff the spec declared `Axes:` AND `idempotency != idempotent`.
- `spec:<spec-id>` — always present; identifies the source spec.
- `req:<XX-NNN>` — one tag per requirement ID covered by the bead (multiple if §2.3 coalesced).

The "interesting axes" rule (axes are tagged only when off-baseline) mirrors the spec template's "the present `Axes:` line means this requirement is interesting" stance ([spec-template.md] §4.N+1).

**Tag-mapping table:**

| Spec construct | Bead representation |
|---|---|
| `Tags: mechanism` | bead tag `tag:mechanism` |
| `Tags: cognition` | bead tag `tag:cognition` (also: bead `description` MUST quote the delegation path the spec names — role + model-class + input shape — per [spec-template.md] §4.N+1) |
| `Axes:` line absent (baseline) | no axis tags emitted |
| `Axes: llm-freedom=bounded; …` | bead tag `axis:llm-freedom-bounded` |
| `Axes: io-determinism=best-effort; …` | bead tag `axis:io-determinism-best-effort` |
| `Axes: replay-safety=unsafe; …` | bead tag `axis:replay-safety-unsafe` |
| `Axes: idempotency=non-idempotent; …` | bead tag `axis:idempotency-non-idempotent` |
| `Axes: idempotency=recoverable-non-idempotent; …` | bead tag `axis:idempotency-recoverable-non-idempotent` |
| Requirement ID `BI-031` | bead tag `req:BI-031`, bead ID `bi-031` |
| Cluster (BI-010 + BI-010a + BI-010b) | bead ID `bi-010`; tags `req:BI-010`, `req:BI-010a`, `req:BI-010b` |
| Invariant `BI-INV-001` | bead ID `bi-inv-001`, tag `req:BI-INV-001`, plus `kind:invariant` |
| Schema `IntentLogEntry` (§6.1) | bead ID `bi-schema.intent-log-entry`, tag `kind:schema` |
| Error taxonomy (§8) | bead ID `bi-error.taxonomy`, tag `kind:taxonomy` |
| Open question `OQ-BI-NNN` | NOT a bead. Open questions are design decisions (produce spec edits, not code) and stay in spec §11 until resolved. |
| OQ that resolves to spec-edit AND impl work (F6) | At OQ-resolution time, mint TWO beads: a `kind:spec-edit` bead for the amendment + a `kind:impl` bead for the code work. The `kind:impl` bead `blocks` consumer beads. Until OQ resolution, consumer beads carry the transient tag `gated-by-spec-edit` so the readiness workflow can defer them. Worked example: BI-014a / OQ-BI-010 / PL-006 (orphan-sweep PL extension — see bi-pilot.md §8 item 6). |

**Worked examples (BI):**

- BI-009 — `Tags: mechanism`, `Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` (all baseline). Bead tags: `tag:mechanism`, `spec:beads-integration`, `req:BI-009`. No axis tags (all baseline).
- BI-022 — `Tags: mechanism`, `Axes: idempotency=non-idempotent` (one off-baseline). Bead tags: `tag:mechanism`, `axis:idempotency-non-idempotent`, `spec:beads-integration`, `req:BI-022`.
- BI-031 — `Tags: mechanism`, `Axes: idempotency=recoverable-non-idempotent`. Bead tags: `tag:mechanism`, `axis:idempotency-recoverable-non-idempotent`, `spec:beads-integration`, `req:BI-031`. Plus `protocol:multi-step` (per §2.2 marker).

### 2.9 Status / priority assignment — all beads load `draft`; accept Beads's default `P2`

**Declaration.** Every loaded bead enters the Beads store in coarse status `draft`. Beads's `Status.enum` (live Beads v0.1.45) has 8 values: `open, in_progress, blocked, deferred, draft, closed, tombstone, pinned`. `br ready` natively excludes `draft` (verify with `br ready --help` — `draft` is not in the "ready" set), so the discipline does NOT need a separate `harmonik:parked` discriminator tag. This satisfies the user's stored preference that "loaded beads must not auto-start (parked state + readiness workflow)" using Beads's native vocabulary.

**Procedure (mechanical).** The CLI exposes a subset of statuses at create time. `br create --status` accepts only `{open, deferred, in_progress, closed}` per `br create --help`; `draft` is NOT directly creatable via `--status`. Therefore the loading procedure for each bead is two calls:

1. `br create --type <issue-type> --title "<title>" ...` (creates with default `open`).
2. `br update <bead-id> --status draft` (immediately demotes to `draft`).

The readiness workflow promotes `draft → open` to make beads dispatchable. Until promoted, beads are invisible to `br ready` and cannot be claimed.

**Spec-version dependency on Beads (BI-007).** `specs/beads-integration.md` §6.1 currently declares a "5-value coarse-status enum" (`{open, in_progress, closed, deferred, tombstone}`). Live Beads v0.1.45 has 8 values, including `draft`, `blocked`, and `pinned`. The discipline uses live Beads's `draft` value as the parked-equivalent. The BI spec's 5-value claim needs a future amendment OR a Beads version pin to reconcile; tracked as a separate user-facing item, not changed by this discipline pass.

**Priority — accept Beads's default P2 at MVH.** `br create` assigns priority `P2` (medium) by default. The discipline does NOT pass `--priority` at create time; every loaded bead carries P2 unless an operator subsequently tunes it. Priority is an operator concern; assigning anything other than the default would be invented data the spec does not specify. Operators set priority during the readiness workflow. (Note: discipline v0.3 said "priority is unset at MVH" — that was inaccurate; Beads has no "unset" priority value, so accepting `P2` is the realised version of the same intent.)

### 2.10 Parent-child grouping — per-spec parent bead, requirements as children

**Declaration.** Each spec produces one **spec-parent bead** carrying the spec's identity, version, and a description that enumerates child counts. Every requirement, sensor, and schema bead derived from the spec is a `parent-child` child of the spec-parent bead.

**Concrete representation (F3).** The spec-parent bead is loaded as:

    br create --type epic --status draft --title "<spec-name> spec — implementation" \
              --description "Implements specs/<spec>.md v<X.Y.Z> (<N> reqs, <M> invariants, <P> schemas)."

`--type epic` selects the native `IssueType.epic` value from Beads's `IssueType.enum` (`{task, bug, feature, epic, chore, docs, question}`); this is distinct from the `br epic` SUBCOMMAND (which is for status reporting and is informative, not normative — see below).

The spec-parent's `--status` cannot be set at create time (`br create -s` accepts only `{open, deferred, in_progress, closed}`); per §2.9 the load is two calls — `br create` then `br update <id> --status draft`.

Children are minted with `--parent <spec-parent-id>`, which creates the `parent-child` dependency edge atomically at create time:

    br create --type task --parent <spec-parent-id> \
              --title "<requirement title>" --description "..."  # then: br update <id> --status draft

Step beads (per §2.2 multi-step protocol rule) are minted with `--parent <umbrella-id>` to nest under their umbrella requirement bead. Per §2.2 F11, step beads do NOT add an explicit `blocks` edge to the umbrella — the parent-child edge already encodes the dep.

**Plan-level (mnemonic) IDs vs Beads-assigned IDs.** `br create` does NOT accept an `--id` flag. Live Beads assigns identifiers in the form:

- **Top-level issue:** `<workspace-prefix>-<base36-suffix>`. Example from the BI smoke-load: `hk-85z` (epic), `hk-9ab` (a top-level task at create-without-parent — rare; most beads have a parent).
- **Child issue (with `--parent <pid>`):** `<pid>.<n>`. The `<n>` is sequential within the parent. Example: children of `hk-85z` get `hk-85z.1`, `hk-85z.2`, ..., and step beads under `hk-85z.37` get `hk-85z.37.1`, ..., `hk-85z.37.6`.

The pilot doc identifiers (`bi-001`, `bi-030.s4`, `bi-schema.intent-log-entry`) are *mnemonic plan-level names* used in the pilot prose and the discipline's worked examples. They are NOT what Beads creates. The load procedure MUST maintain a **mnemonic→assigned-ID map** at load time:

1. As each bead is created, capture the assigned ID returned by `br create --silent` and record it against the bead's mnemonic in a CSV/array.
2. When emitting `--parent <pid>` for a child, look up the parent's mnemonic in the map → assigned ID → pass that.
3. When emitting `br dep add <citing> <prerequisite> -t blocks` for a `blocks` edge, look up both mnemonics → assigned IDs.
4. The mnemonic survives only as the bead's `req:<XX-NNN>` label and (for non-req beads such as schemas/sensors) inside the bead's title or description. There is no bead "name" — Beads exposes only the assigned ID, the title, and the labels.

A working zsh implementation pattern (used by the BI smoke-load):

    EPIC=$(br create --silent --type epic --title "..." --labels "spec:<id>")
    echo "<spec-id>,$EPIC" > /tmp/mnem-map.csv
    create_child() {
      local mnem="$1" parent_mnem="$2" title="$3" desc="$4" labels="$5"
      local parent_id=$(awk -F, -v m="$parent_mnem" '$1==m {print $2}' /tmp/mnem-map.csv)
      local id=$(br create --silent --type task --parent "$parent_id" --title "$title" --description "$desc" --labels "$labels")
      echo "${mnem},${id}" >> /tmp/mnem-map.csv
      br update "$id" --status draft
    }

**Operator commands (informative, not required).** `br epic status <epic-id>` and `br epic close-eligible` are operator commands for monitoring spec rollout — they report on epic completion against children. The discipline mentions them so authors know they exist; they are NOT normatively required by the loading procedure.

**Why parent.** It gives the bead store a navigable spec→requirement hierarchy mirroring the spec corpus's structure. It supports queries like "show all BI children with status open" without scanning every bead.

**Why not deeper.** Per §2.1 / §2.2, sub-sub-task hierarchies (umbrella bead → step beads) are the only second-level grouping. Going deeper invites the local-maxima trap: deep hierarchies are easy to invent and impossible to verify against the spec.

**Worked examples (BI):**

- `bi` (spec-parent bead) — `br create --type epic --status draft --title "Beads Integration spec — implementation" --description "Implements specs/beads-integration.md v0.4.0 (43 reqs, 4 invariants, 7 schemas)."`
- `bi-001` … `bi-032` — `br create --type task --status draft --parent bi …`.
- `bi-031.s1` … `bi-031.s5` — `br create --type task --status draft --parent bi-031 …` (umbrella → steps).
- `bi-inv-001` … `bi-inv-004` — `br create --type task --status draft --parent bi …` (invariant beads carry tag `kind:invariant` per §2.5).
- `bi-schema.*`, `bi-error.taxonomy` — `br create --type task --status draft --parent bi …`.

### 2.11 Edge cases for sibling specs

The BI pilot is a single-file spec. The other 9 specs surface edge cases the discipline must address before scaling.

**(a) Multi-file specs.** Some specs have a primary `<spec>.md` plus supporting `<spec>/schemas.md` (e.g., reconciliation: `specs/reconciliation/spec.md` + `specs/reconciliation/schemas.md`). Schema beads from `schemas.md` use the same `<spec-id>-schema.<type>` naming; the bead title cites `[<spec-id>/schemas.md §N <Type>]` to preserve the source path. Bead-set membership is `<spec-id>` regardless of which file the source is in; all schema beads are `parent-child` of the same spec-parent bead.

**(b) Retired requirement IDs.** A `<prefix>-NNN [retired]` heading (e.g., `RC-INV-002 [retired]`) does NOT mint a bead. The spec-parent bead's description enumerates retired IDs for traceability ("Retired: RC-INV-002, RC-INV-003, RC-INV-005"). No tombstone bead at the bead-set level — the spec already documents the retirement, and adding a Beads tombstone would invent a record where none exists.

**(c) Large §8 taxonomies (one bead per category, not one per spec).** Specs whose §8 contains MULTIPLE classification entries (e.g., RC's 11 reconciliation categories at §8.1–§8.12) mint ONE bead per §8.x entry, NOT a single `<prefix>-error.taxonomy` bead. The umbrella `<prefix>-error.taxonomy` bead becomes the spec-parent's child; per-category beads are children of the taxonomy umbrella (a permitted second-level grouping under §2.10). Single-table taxonomies (BI's `BrError` → category routing table) keep the §2.6 single-bead form (`bi-error.taxonomy`).

**(c.1) §6.3-payload-co-located-with-§8-row clause (F-pilot-EV-1, v0.8).** When a spec's §6.3 (or analogous payload-schema section) provides per-event payload schemas co-located with the §8 row that declares the event — i.e., §8 = event-taxonomy AND §6 = envelope+selected-payload-YAML — the per-row bead description carries the payload schema; no separate `<prefix>-schema.<event-name>-payload` bead is minted. Minting a separate schema bead would be redundant with the §8 row bead (the row bead's work IS declaring the payload struct in the registry) and would duplicate the per-type registration surface. Worked example: EV's 78-row §8 event taxonomy plus §6.3's concrete YAML for ~14 selected types; all per-type payloads collapse to the 78 §8 row beads + 6 §6.1/§6.2 envelope schemas; no `ev-schema.<event-type>-payload` beads. CP and RC may exhibit similar patterns (CP §8 budget/policy taxonomies; RC §8 reconciliation categories already covered by (c)). The (c.1) clause is the §2.6 / §2.11(c) intersection: the §2.6 RECORD/ENUM rule normally mints a schema bead per declared shape, but when the §8 row is the canonical home for the row's payload, the §6.3 declaration is documentary and the row bead is the bead-set's single carrier.

**(c.2) `<spec>-error.taxonomy` edge direction — vocabulary owner, NOT slot rule (F-pilot-HC-direction, v0.9).** The `<spec>-error.taxonomy` bead (single-table form per §2.6, or umbrella per (c)) is a **schema-shaped vocabulary owner**: it OWNS the sentinel-class set declared in §6.1 and §8. Every §4 requirement whose body cites a sentinel name (`ErrTransient`, `ErrStructural`, `ErrDeterministic`, `ErrCanceled`, `ErrBudget`, sub-sentinels, or any spec-local equivalent) is a CONSUMER of that vocabulary. Per §2.6 schema-cite direction and §3.1 step 5 term-use rule, **consumers `block-on` the taxonomy bead**. The taxonomy bead does NOT block-on the §4 reqs that use its sentinels — that direction would invert ownership and produces cycles whenever a downstream `block-on` chain closes back to a sentinel-citing req. Edges run `<req> → <spec>-error.taxonomy`, never the inverse.

> ANTI-PATTERN. The §8 taxonomy is sometimes mistaken for a "slot rule" in the F13 sense (§2.7), tempting the pilot author to emit `<spec>-error.taxonomy → <req>` (taxonomy as predecessor of the §4 req that fills its category). This is wrong on two grounds: (i) F13 slot rules describe a STRUCTURAL carve-out (e.g., AR-053 envelope) where the slot's content is independently nameable beads; the taxonomy's "content" is sentinel values, not §4 reqs. (ii) Per §2.6, schema/vocabulary ownership flows consumer→owner, identical to a record-shape cite — `<req> blocks-on <spec>-error.taxonomy` mirrors `<req> blocks-on <spec>-schema.<record>`.
>
> Recurring history: this anti-pattern surfaced at EM r1 review #15 (caught at draft), EV r1 review #15 (caught at draft), and HC r1 (caught at LOAD via 5 Beads cycle rejections in the F-load-HC-2..7 set). The HC pilot's §5.7 narrative correctly identified the bug class but applied an inverted patch. Adding this clause to make the direction unambiguous.
>
> Worked example. BI's `bi-error.taxonomy` is the canonical correct shape: BI's §4 reqs that cite `ErrTransient` / `ErrStructural` etc. all have `bi-error.taxonomy` in their `blocks` predecessor list. The taxonomy bead's own predecessors are limited to schema beads (e.g., handler/session/adapter interfaces that consume the sentinel set as their return type). HC's correct edge shape: HC-008a, HC-024a, HC-026, HC-048a (and ~15 other sentinel-citing §4 reqs) `block-on hc-error.taxonomy`. EM, EV, and any future spec with a taxonomy bead follow identically.

**(d) Co-owned event payloads (template §6.5).** When a spec EMITS an event whose payload schema lives in EV's §6.3 (the co-owned-payload pattern from spec-template §6.5), the EMITTING spec's bead carries the emission rule + an edge to the EV schema bead. The EV pilot pass owns the schema bead. The emitter does NOT mint a duplicate schema bead. Cross-reference: discipline §3.1.4 (type/schema cite rule).

**(d.1) Registry-with-row-level-dual-ownership (F-em-r1-MIN-9 / F-pilot-EM-6).** Extends (d) to row-level dual ownership. When a registry bead (e.g., `em-schema.checkpoint-trailers`, a 7-key trailer registry) owns N keys/rows and M of those rows are owned-by-annotation by another spec via §6.2 informative-block annotation, the registry-OWNING spec mints the bead (one bead, not split). Dual-owned rows are annotated in the bead description: "Owning spec for row `<key>`: `<other-spec>`; payload semantics live in `<other-spec>` §N." The cross-spec edge from the dual-owned-row's CONSUMER back to the other spec's owning bead is emitted at edge-fire time per the consumer's term-use rule (§3.1 step 5) — NOT from the registry bead itself. Worked example: `em-schema.checkpoint-trailers` owns 7 trailer keys, two of which are RC-owned by §6.2 annotation; the registry bead is minted by EM, those two rows are annotated, and a downstream RC consumer that term-uses one of the dual-owned rows edges back to the RC bead that owns the row's payload semantics — not to `em-schema.checkpoint-trailers`.

**(d.2) Event-row dual-ownership cross-reference (F-pilot-EV-7, v0.8).** Event-row dual-ownership — a §8 event-taxonomy row whose payload semantics live in another spec's §6 — follows the same pattern as registry-row dual-ownership (d.1): the §8-owning spec mints the row bead; dual-owned rows are annotated in the bead description ("Co-owning emitter spec for this event's WHEN: `<spec>`; emission rule lives in `<spec>` §<sec>"); the cross-spec edge from the consumer fires per the consumer's term-use rule (§3.1 step 5), NOT from the row bead itself. Worked example: EV's §8.3 handler-event rows whose `outcome` payload lives in EM §6.1 (`em-schema.outcome`) — the EV row bead is minted with the dual-ownership annotation, and a downstream consumer that term-uses the outcome shape edges back to `em-schema.outcome`, not to the EV row bead. Same posture for §8.6 (RC-emitted) and §8.7 (PL/ON co-owned) row families: the EV row bead is minted by EV (single home for SHAPE per EV-025); the WHEN's owning spec receives consumer edges for term-uses of the WHEN-rule, per §3.1 step 5.

**(e) Intra-protocol lettered step inserts (e.g., PL-005 step 3a, ON-027 step 3a).** Step bead IDs preserve the lettered insert in the *mnemonic*: `pl-005.s3a`, `on-027.s3a`. They are `parent-child` of the umbrella per §2.10. The bead title carries the canonical step identifier ("Step 3a — socket-bind ordering"). At load time the assigned Beads ID does not preserve the lettered form (it's just the next sequential `<umbrella-id>.<n>`); the mnemonic is recorded in the mnem→ID map and the title alone carries the step number.

### 2.12 Workspace prefix at corpus scale — single `.beads/` with prefix `hk`

The full corpus loads into ONE `.beads/` workspace (single SQLite DB at `<repo>/.beads/beads.db`) with prefix `hk` (harmonik). All 10 spec-parent epics share this DB; their assigned IDs are `hk-<base36-suffix>`.

**Why one DB, not ten.**
- `br init --prefix <X>` accepts ONE prefix per workspace (verified in `br init --help`); per-spec prefixes are not supported.
- Cross-spec cycle detection (§3.3) requires the union of all 10 specs' bead sets to form a DAG. `br dep cycles` is per-DB; running it across 10 separate DBs would require building a multi-DB join tool — work the discipline does not justify when one DB is the simpler choice.
- Spec scope is preserved by the `spec:<spec-id>` label that every bead carries (per §2.8). Filtering by spec is `br list -l spec:<spec-id>`; Beads natively supports this.

**Init procedure.**

    cd <repo>
    br init --prefix hk          # creates .beads/beads.db with prefix hk
    # then per-spec loads append to the same DB

**`.gitignore`.** `.beads/` is gitignored at MVH (the SQLite DB is environment-specific and the JSONL export is regenerable by `br sync`). Post-MVH, the JSONL may become a tracked artifact; revisit when the readiness workflow lands.

(Origin: BI smoke-load 2026-04-27 — initial smoke load used `--prefix bi`; post-load decision to fix the corpus prefix as `hk`. See [bi-smoke-load-findings.md](bi-smoke-load-findings.md) §2.6.)

---

## 3. Cross-spec edge convention

The decompose-to-tasks pass produces a bead set that spans all 10 specs. Cross-spec edges are essential — the implementation order is constrained by them — and they MUST be derived mechanically from the source corpus, not invented.

### 3.1 Mechanical derivation rule

For each bead `<spec>-NNN`:

1. List every cross-spec citation in the source requirement's body. A citation is `[<other-spec-id>.md §N]` or `<OTHER-PREFIX>-NNN`.

   **(F-pilot-AR-10) Supporting cite vs hard dep.** Not every inline cite generates an edge. An inline cite "per X" (or equivalent — "as established by X", "see also X", "consistent with X") attached to a stand-alone normative assertion is a *supporting cite* — informational only, no edge emitted. An inline cite "uses X's shape", "MUST conform to X", "is the outcome of X", or otherwise referencing X's content/value/concept as a load-bearing input to the citing rule is a *hard dep* — emit a `blocks` edge.

   **Operational test.** Mentally remove the cited rule. Does the citing rule remain independently testable from its own surrounding text? If yes → supporting cite, no edge. If no → hard dep, edge. The distinction matters because supporting-cite cycles are common in normative corpora (peer rules cross-referencing each other for consistency); treating them as hard deps creates spurious n-cycles that the load-time `br dep cycles` check rejects.

   **Worked examples (AR).**
   - AR-011 body: "outputs conform to the verification-result shape (see §4.7.AR-030)". This is a *hard dep* — verification-result shape is the citing rule's load-bearing input. Edge `ar-011 → ar-030` emitted.
   - AR-030 body: "verification-result MUST be the outcome of running a `verification-node` (per AR-029)". *Hard dep* — verification-node is the load-bearing concept. Edge `ar-030 → ar-029` emitted.
   - AR-029 body: "No 'verifier' subsystem exists per §4.5.AR-018 and §4.3.AR-011" — appended to AR-029's stand-alone enum claim. Removing AR-018/AR-011 leaves AR-029's "no `verification-node` enum value" claim independently testable. *Supporting cites* — no edges emitted. (Without this rule, AR-011 → AR-030 → AR-029 → AR-011 forms a 3-cycle that the load-time check rejects, as observed when v0.2.1 attempted to load.)
   - BI's `> RATIONALE:` blocks already excluded (§3.1 no-edge list); the supporting-cite test is for cites in non-rationale prose that nonetheless are not load-bearing.
2. For each citation that names a SPECIFIC requirement ID (`EV-023a`, `EM-014`, `RC-014`), emit a `blocks` edge from the BI bead to the corresponding bead in the other spec's bead set.
3. For each citation that names ONLY a section anchor (`[event-model.md §6.3]`) without a requirement ID, derive the closest requirement ID by reading the spec at that section. If §6.3 is a single requirement (rare), emit the edge; if §6.3 is a section header covering multiple requirements, emit edges to each requirement bead in that section AND tag the citing bead with `cite:wide-fanout` so corpus-lint can flag the source citation for tightening in the next spec revision.

   **(F-em-r1-MIN-6) Forward-deferred wide-fanout tag policy.** The `cite:wide-fanout` tag fires when the EDGE fires, not at deferral time. Forward-deferred section-anchor cites — cites whose target lives in a not-yet-loaded sibling spec or a spec outside the current `depends-on` (per F-pilot-EM-2 / forward-cite tracking) — do NOT pre-emit `cite:wide-fanout` placeholders on the citing bead. Instead, the pilot's §5 forward-cite log enumerates which deferred edges WILL be wide-fanout when they materialize, and the citing bead acquires the tag at edge-creation time during reciprocal-pilot resolution. Rationale: pre-emitting placeholders requires the citing pilot to predict edge-future-shape (how many requirements the target section will host at re-draft); deferring the tag to edge-fire time keeps the rule local to the pilot that emits the edge. The same posture applies to the `gated-by-corpus-scale` tag (§2.5): the tag is applied by the sensor-owning pilot when it observes the four-source-empty + outside-`depends-on`-cite condition and is dropped at edge-fire time when reciprocal pilots materialize predecessors.

   **(F-pilot-EV-2, v0.8) `post-mvh` transient tag.** `post-mvh` is a transient tag for beads whose normative content is gated to post-MVH delivery (i.e., the spec acknowledges the requirement but defers implementation to a post-MVH milestone). Visible to `br ready` callers via `br list`-style label filtering, but does NOT itself affect the bead's `draft` status. Analog of `gated-by-spec-edit` (the F6 OQ-resolution-time tag from §2.8) and `gated-by-corpus-scale` (the v0.7 §2.5 sensor-predecessor-degeneracy tag): the bead is minted but operationally parked; the readiness workflow filters it out until the post-MVH milestone unlocks the dependency. Drop the tag when the post-MVH amendment lands and the bead's normative content becomes MVH-current. Worked example: EV §8.6.14 `bead_terminal_transition_recovered` (post-MVH per OQ-BI-008) — the §8 row bead is minted but carries `post-mvh`; no MVH conformance obligation attaches; the tag drops at OQ-BI-008 resolution time when the BI adapter implements the event.
4. For citations that name a schema or type by `[other-spec.md §N <TypeName>]`, emit a `blocks` edge to the schema bead `<other-spec>-schema.<type-name>`. (At MVH this is `blocks`, per §2.7's F4 collapse — the prior `waits-for` framing is reserved for post-MVH adoption.)
5. **Term-use edges (F-pilot-AR-2.5).** When a requirement's body uses (without explicit inline cite) a defined term whose definition is owned by another single requirement in the same spec — including `mechanism`-tagged or `cognition`-tagged classifiers from a definition rule, named schemas, named invariants, or named roles — emit a `blocks` edge from the using requirement's bead to the defining requirement's bead. The same rule applies cross-spec when the defined term has a single-owner spec. Rationale: over-gating is never wrong (per F4); under-gating risks readiness-workflow misordering when the defining rule is incomplete. Worked example: AR-006 / AR-007 use `mechanism`-tagged / `cognition`-tagged classifiers from AR-005 → `ar-006` and `ar-007` block-on `ar-005`. AR-039 uses "merge agent" / "merge node" defined in AR-036 → `ar-039` blocks-on `ar-036`.

   **(F-pilot-AR-r2-2) Invariant-as-target exemption.** When the defining requirement is a `<prefix>-INV-NNN` invariant, the term-use rule does NOT fire. Per §2.5 F12, sensor beads `blocks-on` impl beads but impls do NOT `blocks-on` sensors; emitting a term-use edge from an impl req to an invariant would reverse the one-way rule and produce a cycle. Worked example: AR-038 body uses "centralized-controller invariant" (the title of AR-INV-007). The term-use rule would produce `ar-038 → ar-inv-007`, but AR-038 is itself an AR-INV-007 sensor predecessor (`ar-inv-007 → ar-038` per §10.2 group cite); the resulting 2-cycle is rejected by §2.5 F12. The invariant-as-target exemption is the explicit codification of pilot-author intuition.

   **(F-em-r1-MAJ-1) Invariant-body term-use sub-clause.** When an `<prefix>-INV-NNN` invariant's BODY (the §5 invariant prose, with or without inline cite) uses a defined term whose definition is owned by an in-spec or in-`depends-on` requirement, schema, or trailer-registry row, emit a `blocks` edge FROM the invariant's sensor bead TO the defining bead. This is consistent with — and additive to — the §2.5 v0.7 four-source enumeration: §2.5 sources (1)–(3) cover WHICH §4 reqs the sensor blocks-on transitively via §10.2 conformance / persona / sensor-block prose; this sub-clause is §2.5 source (4), covering WHICH schemas / impl reqs the sensor blocks-on directly via the invariant body's own term-uses. Worked example: `em-inv-005` body uses `Harmonik-Bead-ID` (owned by `em-schema.checkpoint-trailers`) and the bead-tied-runs concept (owned by `em-014`); both fire as direct sensor predecessors. The reverse direction (impl req → invariant) does NOT fire — the F-pilot-AR-r2-2 invariant-as-target exemption preserves §2.5 F12 sensor↔impl one-way.

   **(F-em-r1-MAJ-4) Rule precedence: invariant-as-target exemption beats supporting-cite test.** When BOTH the supporting-cite test (F-pilot-AR-10 in step 1 above) AND the invariant-as-target exemption (F-pilot-AR-r2-2 immediately above) could plausibly apply to the same cite, the invariant-as-target exemption takes precedence. Rationale: the invariant-target exemption is structural (per §2.5 F12 sensor↔impl one-way and §2.5 F10 invariant-edge-target rules); the supporting-cite test is heuristic (per the operational-test framing). Under both rules the outcome is no-edge, so precedence is documentation-only — but stating it explicitly disambiguates rationale at re-draft time and prevents future-pilot litigation. Worked example: `em-040` cites `[architecture.md §4.9]` (centralized-controller); AR §4.9 houses AR-INV-007; the invariant-as-target exemption fires and the supporting-cite analysis is moot.

**No edge is emitted for:**

- §9.3 co-references (per §2.7 third class — co-references do not generate bead-level edges).
- Mentions in informative blocks (`> INFORMATIVE:`, `> RATIONALE:`).
- Mentions inside §A appendices.
- Mentions inside §11 open questions or §12 revision history.
- **§10 conformance / test-surface obligations prose** (F-pilot-AR-AO1) — descriptive of test surfaces, not declarative of spec-level dependencies.
- **§7 protocol / pseudocode / state-machine prose** (F-em-r1-MIN-3) — no edges fire from cross-spec cites in §7 sub-sections (.1, .2, .3, .4, etc.). Behavioural specs (PL, RC, EM §7.4 main-loop) embed cross-spec cites in pseudocode comments and state-machine table cells; these are illustrative, not normative, and follow the same no-edge posture as §10 / §11 / §12 / §A. Cross-spec normative dependencies in behavioural specs are emitted from the §4 requirement bodies that the §7 prose summarises, not from the prose itself.
- **`docs/components/internal/` doc cites** (F-pilot-AR-AO3) — internal-component docs are not specs under `specs/`; per the existing rule that only `[<other-spec-id>.md §N]` cites generate edges, internal-component-doc cites are non-edge by virtue of not being spec IDs. Enumerated explicitly here for clarity.
- **Self-cite illustrative examples** (F-pilot-AR-AO2) — when a requirement's body uses `[<own-spec-id>.md §N]` or `<OWN-PREFIX>-NNN` as an illustrative example of a `touches:`-shape entry or similar template, no edge is emitted (self-cites are not cross-spec, and illustrative-example cites are not dependency assertions).

### 3.2 Front-matter `depends-on` is the spec-level invariant; it does NOT auto-generate edges

The spec's `depends-on` list declares the universe of upstream specs whose requirements MAY be cited. It does NOT itself produce edges; edges are produced per §3.1 from the actual citations.

**Validation rule:** after the bead set is generated, every cross-spec edge `<bi-bead>` → `<other-bead>` MUST cite a spec whose `spec-id` appears in BI's `depends-on`. An edge to a spec NOT in `depends-on` is a bug — either the edge is invented, or `depends-on` is incomplete and the spec needs a patch.

**§4.a envelope grandfather carve-out (Option E, 2026-04-27; extended 2026-04-28 per F-pilot-EV-4 / Option D1).** AR-053 (foundation-protocol) introduced a `§4.a` envelope requirement that every spec name its consumed cross-spec contracts in a structured front-matter envelope. Specs that were `reviewed` BEFORE AR-053 landed — OR ON THE SAME DAY AR-053 LANDED (boundary-case inclusion) — are GRANDFATHERED until their next revision and are NOT required to retroactively add `§4.a` envelopes. The carve-out applies to the named set: `{EM, HC, CP, WM, PL, RC, EV}`. AR itself is exempt by AR-052 (foundation-cross-cutting). EV reviewed same-day as AR-053 (2026-04-24); the spec author's deliberate review decision is preserved by treating EV as a boundary-case inclusion. Post-EV-reviewed specs (none in current corpus) require §4.a per AR-053. The carve-out preserves the spec author's deliberate review decision while letting the rule apply to fresh specs going forward; retroactive enforcement is a separate conversation if/when the user wants it. Discipline-side mechanical consequence: when a grandfathered spec's pilot self-flags its missing `§4.a` envelope (e.g., F-pilot-EM-3, F-pilot-EV-4), document it as carve-out compliant; no spec edit; no pilot patch.

### 3.3 Cycle detection

After generating all 10 specs' bead sets, the union must form a DAG. Cycles indicate a real cross-spec contract problem (or, more commonly, a bidirectional dependency that should be split into two parts). The decompose-to-tasks pass MUST surface any cycle to the discipline author for resolution and refuse to emit the offending edges until resolved.

The BI ↔ EM mutual-dependency-by-direction pattern (`run.bead_id` is owned by EM; the bead-id propagation rules are owned by BI) is permitted because it splits across requirement IDs (EM owns the field; BI owns when it's populated). At the bead level, `bi-017` blocks-on `em-014` (the field exists), and `em-014` does NOT block on any BI bead. No cycle.

---

## 4. Tag mapping table (consolidated)

This is the single source of truth for spec construct → bead representation. The per-rule examples in §2 reference back here.

| Spec construct | Bead element | Notes |
|---|---|---|
| `<prefix>-NNN` heading | bead ID `<prefix-lower>-NNN` | Letter suffixes preserved: `BI-010a` → `bi-010a` (when not coalesced) |
| `<prefix>-INV-NNN` heading | bead ID `<prefix-lower>-inv-NNN` | Tag `kind:invariant` |
| Requirement title | bead title | Strip leading "—"; ≤80 chars; imperative if practical |
| Body prose (1–2 sentences) | bead description | Always cite the source requirement ID |
| `Tags: mechanism\|cognition` | bead tag `tag:<value>` | Mandatory; single value |
| `Axes: <axis>=<value>; …` (off-baseline only) | bead tag `axis:<axis>-<value>` | One tag per off-baseline axis |
| `Sensor:` line | sensor bead `<prefix-lower>-inv-NNN` (already covered by §2.5) | Not a separate tag |
| `RECORD <Type>` (§6) | bead ID `<prefix-lower>-schema.<type-kebab>` | Tag `kind:schema` |
| `INTERFACE <Type>` (§6) | bead ID `<prefix-lower>-schema.<type-kebab>` | Tag `kind:interface` |
| `ENUM <Type>` (§6) | bead ID `<prefix-lower>-schema.<type-kebab>` | Tag `kind:enum` |
| `<constrained primitive>` (§6, e.g., regex-validated string) | bead ID `<prefix-lower>-schema.<type-kebab>` | Tag `kind:primitive-shape` |
| §8 error taxonomy | bead ID `<prefix-lower>-error.taxonomy` | Tag `kind:taxonomy` |
| §10.2 test obligation | absorbed into the requirement bead per §2.4 | Test infra: separate bead with `blocks` edge |
| §11 open question `OQ-XX-NNN` | NOT a bead | Resolving an OQ produces a spec edit, not implementation code; OQs stay in spec §11 |
| Spec front matter `spec-id` | parent bead ID `<spec-id>` (e.g., `bi`) | Tag `kind:spec-parent` |
| Front-matter `version` | parent bead description includes `v<X.Y.Z>` | Updated when spec re-releases |
| Front-matter `depends-on` | NOT directly an edge; sets the universe | Edges derived per §3.1 |
| Cross-spec cite `EV-023a` | edge `<bi-bead>` `blocks`-on `ev-023a` | Mechanical; `br dep add bi-XYZ ev-023a -t blocks` |
| Cross-spec section cite `[ev.md §6.3]` | edge per §3.1.3 (may fan out) | If §N has one req → one edge; if many → many |
| Cross-spec type cite `[em.md §6.1 Outcome]` | edge `<bi-bead>` `blocks`-on `em-schema.outcome` | Mechanical (at MVH `blocks`, per F4) |
| Inline cite inside `> INFORMATIVE:` block | NO edge | Informative content does not induce edges |
| Cite inside §A appendix | NO edge | Appendix is non-normative |

The default coarse status for every loaded bead is Beads's native `draft` (§2.9). Priority defaults to Beads's `P2`; the discipline does not pass `--priority` at create time (per §2.9).

---

## 5. Cross-spec edge convention summary

Repeats §3 in checklist form for the implementer running this pass on a new spec.

For each `<prefix>-NNN` requirement bead:

1. Walk the requirement's body prose top-to-bottom.
2. For every cross-spec cite, classify it as:
   - **(a) Specific requirement ID** → emit `blocks` edge to that bead.
   - **(b) Section anchor only** → resolve to the requirement(s) under that section; emit `blocks` edges to each + tag the citing bead `cite:wide-fanout`.
   - **(c) Type / schema cite** → emit `blocks` edge to the schema bead. (At MVH all inline-cite edges are `blocks`; the `waits-for` distinction is reserved for post-MVH per §2.7 / F4.)
   - **(d) Inside informative / appendix / OQ / §9.3 co-references** → no edge.
3. Validate every emitted edge: the target spec MUST appear in the source spec's `depends-on`. Otherwise: bug.
4. After all 10 specs are processed, run a cycle check. Cycles → surface to the discipline author for resolution; do not emit the offending edges.

---

## 6. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-04-25 | 0.1 | foundation-author | Initial discipline draft from BI pilot. 10 rules across 3 dimensions (granularity, sensor/schema, cross-spec edges). 10 open questions. Companion: [bi-pilot.md](bi-pilot.md). |
| 2026-04-30 | 0.9 | foundation-author | HC pilot r1 class-lane finding. New §2.11(c.2) clause + anti-pattern note: `<spec>-error.taxonomy` is a vocabulary OWNER (schema-shaped), NOT a slot rule in the F13 sense. Edges run `<req> → <spec>-error.taxonomy`, never the inverse — consumers (any §4 req that cites a sentinel name) `block-on` the taxonomy bead, identical to a record-shape cite per §2.6 / §3.1 step 5. The same anti-pattern surfaced at EM r1 #15 (caught at draft), EV r1 #15 (caught at draft), and HC r1 — this time caught at LOAD (5 of 6 Beads cycle rejections in F-load-HC-2..7 traced to inverted edges in `hc-error.taxonomy`'s predecessor list). Documentation-only patch; no behavioural change. F-pilot-HC-direction. Worked example: BI's `bi-error.taxonomy` is the canonical correct shape (consumer reqs all `block-on` it; the taxonomy's own predecessors are limited to schema beads). HC pilot's data-level fix lives in `hc-pilot-data.yaml` v0.1.0 → v0.1.1 (separate from this doc patch). |
| 2026-04-28 | 0.8 | foundation-author | EV pilot r1 + 5 small patches. §3.2 §4.a envelope grandfather carve-out extended to include EV (same-day boundary, joining EM/HC/CP/WM/PL/RC). §2.5 F10 sensor→sensor explicit-ID-cite clarification (worked example: ev-inv-001 → em-inv-001). §2.11(c.1) §6.3-payload-co-located-with-§8-row clause (worked example: EV's 78-row §8). New `post-mvh` transient tag definition in §3.1 (worked example: EV §8.6.14). §2.11(d.2) event-row dual-ownership cross-reference. F-pilot-EV-4 (§4.a envelope), F-refs-EV-6 (sensor→sensor), F-pilot-EV-1 (§6.3 co-location), F-pilot-EV-2 (post-mvh tag), F-pilot-EV-7 (§2.11(d.2) xref). |
| 2026-04-25 | 0.2 | foundation-author | Collapsed 10 OQ-DTT-NNN open questions into rule clauses (decisions made by the discipline author per user direction). **Decisions made:** (DTT-001) OQs do NOT become beads — they're design decisions that produce spec edits, not implementation work. (DTT-002) Multi-step protocol rule reframed as a guideline: split when ≥3 steps + independent testability + umbrella loses meaning when stripped; per-spec author judges; err toward fewer beads. (DTT-003) Priority unset at MVH; operator concern. (DTT-004) Coalesced clusters keep all constituent `req:` tags for queryability. (DTT-005) §9.3 co-references generate no bead-level edges; inline cites generate edges (`blockedBy` for behavioral deps, `waits-for` for declaration-only deps like type/schema cites). (DTT-006) Section-anchor cites fan out to all reqs in the section AND tag the citing bead `cite:wide-fanout` for triage; recommend tightening in next spec revision. (DTT-007) Invariant beads are first-class edge targets. (DTT-008) Flat IDs (`bi-001`, `em-001`) — global namespace, prefix is the disambiguator. (DTT-009) Spec parent bead is pure aggregation; no own work. (DTT-010) Cognition delegation paths live in bead description text, no separate field. **Other:** §3.1 changed type/schema cites from `blockedBy` to `waits-for` per DTT-005 framing; §5 cross-spec edge convention summary updated to match. No structural reorganization; the §6 OQ section was removed and the prior §7 Revision history is now §6. |
| 2026-04-28 | 0.7 | foundation-author | EM pilot r1 + 3 policy decisions. Behavioral: §3.1 step 5 invariant-body term-use sub-clause + §2.5 fourth §10.2 source (changes em-inv-005 predecessors). Documentation-tightening: §2.5 sensor-predecessor degeneracy + gated-by-corpus-scale tag; §2.6 type-alias-resolves-to-single-MVH-variant guidance + typed-alias-cluster guidance; §2.7 F13 declaration-rule ↔ method-rule worked example; §2.11(d) registry-row dual-ownership extension; §3.1 invariant-as-target precedence over supporting-cite + §7 prose no-edge explicit entry; §3.1.3 forward-deferred wide-fanout tag policy; §2.2 F8b behavioural-spec worked example. Policy: §3.2 §4.a envelope grandfather carve-out for pre-AR-053 specs (EM/HC/CP/WM/PL/RC). F-em-r1-MAJ-1 through F-em-r1-MAJ-4, F-em-r1-MIN-3/5/6/7/8/9. |
| 2026-04-27 | 0.6 | foundation-author | Applied 4-finding pass: 3 documentation-tightening findings from AR pilot r2 review (synthesis at `docs/reviews/2026-04-27-ar-pilot-r2/synthesis-r2.md`) plus 1 new mechanical rule surfaced when the v0.2.1 load attempt hit a 3-cycle. **(F-pilot-AR-10, Major — NEW MECHANICAL RULE)** §3.1 step 1 grows a supporting-cite-vs-hard-dep clause: an inline cite "per X" attached to a stand-alone normative assertion that is independently testable is informational, not a hard dep. Operational test: if removing the cited rule leaves the citing rule independently testable, the cite is supporting. Worked example: AR-029's "per §4.5.AR-018 and §4.3.AR-011" cite attached to "No 'verifier' subsystem exists" was treated as a hard dep in v0.2.1 → produced 3-cycle `ar-011 → ar-030 → ar-029 → ar-011` rejected at load. v0.2.2 reclassified both as supporting cites. Without this rule, normative-corpus peer-rule cross-references will routinely produce spurious n-cycles. **(F-pilot-AR-r2-2, Minor class)** §3.1 step 5 grows an invariant-as-target exemption: term-use does NOT fire when defining req is `<prefix>-INV-NNN` (avoids reversing §2.5 F12 sensor↔impl one-way rule). **(F-pilot-AR-r2-3, Minor class)** §2.5 reviewer-persona-bundling trigger explicitly requires named `<prefix>-INV-NNN` ID; category phrases ("cross-cutting-invariant violations") do NOT trigger. **(F-pilot-AR-r2-4, Major class)** §2.5 reorganized: three §10.2 sensor-edge sources enumerated (conformance-group prose cites, reviewer-persona group bundling, sensor-block body inline cites). The three sources compose; sensor predecessor list is their union. |
| 2026-04-27 | 0.5 | foundation-author | Applied 6-finding pass from AR pilot r1 review (foundation-amendment is foundation-protocol; other findings are corpus-shape silences). See `docs/reviews/2026-04-27-ar-pilot-r1/synthesis.md` for full report. **(F-pilot-AR-1, Major)** §2.1 gains a fourth exception (§2.1a, "pure cross-reference requirement"): a req whose body is verbatim "see §N.OTHER-NNN" with no independent content collapses to a `notes:` line on the cited bead. Worked example: AR-035 → `notes:` on `ar-026`. **(F-pilot-AR-3, Major)** §2.6 gains a fourth schema category — constrained primitive types (regex/format/length-bounded primitives). Tag `kind:primitive-shape`. Worked example: AR §6.1's `agent_type` regex → `ar-schema.agent-type-identifier`. **(F-pilot-AR-8, Major)** §2.7's F13 paragraph grows a slot-rule/content-rule default-resolution heuristic: when A defines a slot and B fills it, A→B is normative and B→A is informational (reclassify B→A to no-edge). Worked example: AR-053 (slot rule) ↔ AR-013 / AR-052 (content) — `ar-053 → ar-013` and `ar-053 → ar-052` survive; reverse cites informational. **(F-pilot-AR-2.5/2.6/2.7, Minor class)** §3.1 gains a fifth derivation rule: inline use of a defined term whose definition is owned by another req in the same (or single-owner cross) spec generates a `blocks` edge to the defining bead. Decision rationale: over-gating is never wrong (mirrors F4 collapse). **(F-pilot-AR-AO1/AO2/AO3, Minor class)** §3.1's no-edge list grows three explicit entries: §10 conformance prose, `docs/components/internal/` doc cites, self-cite illustrative examples. Codifies pilot's correct intuitive behavior. **(F-pilot-AR-2.8, Minor class)** §2.5 grows a clarification: §10.2 reviewer-persona group bundling counts as a sensor-edge source when the persona explicitly names a §5 invariant alongside §4 reqs. Worked example: AR-INV-001 + AR-003/AR-007/AR-042 in AR §10.2 conformance-auditor block. **Other:** preamble updated; §4 tag-mapping table grows a `kind:primitive-shape` row. |
| 2026-04-27 | 0.4 | foundation-author | Applied 6-finding pass from BI smoke-load (first execution against live `br` v0.1.45). See [bi-smoke-load-findings.md](bi-smoke-load-findings.md) for full report. **(F11, Major)** §2.2 gains an explicit clause: step beads created with `--parent <umbrella-id>` MUST NOT add an explicit `blocks` edge to the umbrella — Beads's cycle detector treats parent-child as a dep, so step→umbrella `blocks` plus the implicit parent-child produces a cycle (Beads rejected all `bi-NNN.s1 → bi-NNN` edges in pilot v0.1.2). **(F12, Major — sensor↔impl one-way)** §2.5 gains an emphasis: sensor beads `blocks-on` impl beads, never the inverse. Pilot v0.1.2's `bi-011 → bi-inv-001` and `bi-022 → bi-inv-003` edges (impl→sensor) were wrong-direction; Beads correctly rejected them as cycles. **(F13, Major — bidirectional inline cites)** §2.7 gains a disambiguation paragraph: if A inline-cites B AND B inline-cites A, surface to the discipline author for resolution — usually one of the cites is informational (`> RATIONALE:`, §9.3 co-reference, "see also") and reclassifies as no-edge. Cycles are not acceptable; one or both must be reclassified or removed before load. (Origin: pilot v0.1.2 had `bi-004 ↔ bi-027` as bidirectional pilot edges; on inspection neither requirement actually inline-cites the other in BI.) **(F14, Major — mnem vs assigned IDs)** §2.10 gains a new "Plan-level (mnemonic) IDs vs Beads-assigned IDs" subsection. `br create` does NOT accept an `--id` flag; live Beads assigns IDs as `<prefix>-<base36-suffix>` (top-level) and `<parent-id>.<n>` (children). The pilot doc's identifiers (`bi-001`, `bi-030.s4`, `bi-schema.intent-log-entry`) are mnemonic plan-level names. The load procedure MUST maintain a mnem→assigned-ID map; `--parent` and `dep add` calls use assigned IDs; mnemonics survive only as the `req:<XX-NNN>` label and inside titles. Includes a working zsh implementation pattern. Doc preamble adds a convention note pointing readers at §2.10 and §2.12. **(F15, Minor — default priority)** §2.9 acknowledges that Beads assigns priority `P2` by default; the discipline accepts the default and does NOT pass `--priority` at create time. v0.3's "priority is unset at MVH" claim is corrected — Beads has no "unset" priority value. **(F16, Major — corpus workspace prefix)** New §2.12 fixes the corpus workspace as one `.beads/` with prefix `hk` (harmonik). Spec scope lives in the `spec:<spec-id>` label; all 10 spec-parent epics share the same DB so cross-spec cycle detection (§3.3) runs natively as `br dep cycles`. Per-spec workspaces rejected because cycle detection across 10 separate DBs would require multi-DB tooling the discipline does not justify. `.beads/` added to `.gitignore` at MVH. **Other:** preamble convention note added; §2.10 spec-parent representation block updated to reflect two-call create+update sequence. F1–F10 from v0.3 unchanged. The bi-pilot v0.1.3 patches (§7 tally, edge-removal patches, `bi-schema.harmonik-write-status` add) are tracked separately in the pilot's revision history. |
| 2026-04-26 | 0.3 | foundation-author | Applied 10-finding skeptical-review pass against v0.2 + bi-pilot v0.1.1. **(F1, BLOCKER)** Renamed the canonical edge label `blockedBy` → `blocks` corpus-wide (§2.5, §2.6, §2.7, §2.8, §3.1, §4, §5); `blockedBy` does not exist in Beads's `DependencyType.enum`. Added the loading convention "`A blocks B` means A is the prerequisite; loaded as `br dep add <B> <A> -t blocks` (dependant first, prerequisite second)." **(F2, BLOCKER)** Replaced the synthetic `harmonik:parked` tag with Beads's native `draft` status (live `Status.enum` has 8 values; `br ready` natively excludes `draft`). §2.9 documents the two-step load procedure (`br create` defaults to `open` → `br update --status draft`) since `br create --status` only accepts `{open, deferred, in_progress, closed}`. Notes the BI-007 5-value-vs-8-value spec/Beads inconsistency for separate user-facing reconciliation. **(F3, BLOCKER)** §2.10 specifies the spec-parent representation concretely: `br create --type epic --status draft …` (the native `IssueType.epic`, distinct from the `br epic` reporting subcommand); children minted with `--parent <spec-parent-id>` to materialize the `parent-child` dep at create time; step beads use `--parent <umbrella-id>`. **(F4, Major)** Collapsed `waits-for` vs `blocks` to `blocks` at MVH (§2.7, §3.1.4, §5 step 2c, §4 tag-mapping table); the declaration-vs-behavior test produced 3 ambiguous cases in BI; `waits-for` reserved for post-MVH adoption pending operational evidence. **(F5, Major)** Added the missing-inline-cite catcher to §2.7: if a §9.3 entry's consumers cannot be tested without producer impl, surface as a spec-edit task (per F6), not as an invented edge. **(F6, Major)** Added §2.8 carve-out for OQs that resolve to spec-edit-AND-impl: mint a `kind:spec-edit` bead + a `kind:impl` bead at OQ-resolution time; consumers carry transient `gated-by-spec-edit` until resolution. Worked example: BI-014a / OQ-BI-010 / PL-006. **(F7, Major)** New §2.11 covers sibling-spec edge cases — (a) multi-file specs (RC's `schemas.md`), (b) retired requirement IDs (no tombstone bead; spec-parent description enumerates), (c) large §8 taxonomies (one bead per §8.x category, NOT one per spec), (d) co-owned event payloads (emitter does NOT mint duplicate schema bead; EV pilot owns it), (e) intra-protocol lettered step inserts (PL-005 step 3a → `pl-005.s3a`). **(F8, Major)** Sharpened §2.2 with three sub-rules: (a) depth ceiling — step beads do not split (max hierarchy spec-parent → req-umbrella → step); (b) sub-case tiebreaker — shared state machine → sub-bullets; independent code paths → step beads (BI-031 step 4 → sub-bullets); (c) constraint-requirements adjacent to umbrellas (e.g., ON-027a atomicity over ON-027) get their own bead with a `blocks` edge from the umbrella, not their own steps. **(F10, Minor)** Appended invariant-as-edge-target guidance to §2.5: invariant beads MAY be edge targets when downstream cites the invariant by ID (e.g., `RC-INV-004`); by default downstream edges to the constrained §4 reqs. **F9 (Minor)** is applied in the companion bi-pilot.md v0.1.2 (pilot edge renames). |
