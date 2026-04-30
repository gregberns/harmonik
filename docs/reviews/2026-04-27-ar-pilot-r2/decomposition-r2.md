# AR Pilot Decomposition-Quality Review (r2) — 2026-04-27

Reviewer: decomposition-quality reviewer (per `pilot-review-protocol.md` v0.2 §3.2). Pilot under review: `docs/decompose-to-tasks/ar-pilot.md` v0.2.1 against `specs/architecture.md` v0.3.1, with discipline `docs/decompose-to-tasks/discipline.md` v0.5. Second pass after r1 surfaced findings that drove discipline v0.5 and the v0.2.0 / v0.2.1 pilot re-drafts.

**Headline.** All five r1 BLOCKERs / MAJORs verify CLEAN: AR-035 collapse is structurally sound; F13 slot-rule resolution applied correctly to the AR-013/052/053 trio; the `ar-011 → ar-029` cycle is gone in v0.2.1; the `ar-052 → ar-016` non-bidirectional half is emitted; `ar-inv-001` reviewer-persona bundling expanded with `ar-003`/`ar-042`. The 9 reported term-use edges all justify against actual term use in the spec body (no over-eager emissions). The `ar-schema.agent-type-identifier` retag is correct. Two MINOR findings remain (one term-use omission, one §10.2 persona-bundle hunt result) and one MAJOR-class finding on a recurring asymmetry between term-use and §10.2-bundling rules.

## 1. Sampled beads (15)

Weighted toward post-re-draft changes per the r2 task instructions:

- **All 9 reported term-use edges** (one bead each, citing-side): `ar-052`, `ar-004`, `ar-014`, `ar-017`, `ar-019`, `ar-028`, `ar-029`, `ar-030`, `ar-031`.
- **The AR-035 collapse target:** `ar-026` (verify notes-line + co-tag).
- **The F13 envelope-slot trio:** `ar-013`, `ar-052`, `ar-053` (re-checked from r1).
- **The §10.2 persona-bundle expansion target:** `ar-inv-001`.
- **The v0.2.1 fix:** `ar-011`.
- **Random regression-check first-class beads:** `ar-021` (cognition bead), `ar-039` (term-use survivor from r1 finding 2.6), `ar-042`, plus the schema bead `ar-schema.agent-type-identifier`.

(Total unique beads inspected: 15. Some beads serve dual duty as both r2-change verification and term-use justification.)

## 2. Per-sample findings

### 2.1 AR-035 collapse onto `ar-026` — VERIFY CLEAN

- **What r2 must verify:** AR-035 row absent from §2; `ar-026` row carries `req:AR-035` co-tag; notes-line names AR-035 and cites discipline §2.1a; no orphan references.
- **Verified.** Pilot row for `ar-026` (line 69) carries `req:AR-026, req:AR-035` in tags. Notes column reads "AR-035 (role-orthogonality cross-reference) collapsed here per discipline §2.1a — see source spec §4.8.AR-035." Pilot §2 contains no `ar-035` row (verified by walking the table from line 43 through line 91). Pilot §1 spec-parent description (lines 11, 26) explains the collapse: "50 active §4 reqs minus AR-035 collapsed to a `notes:` line on `ar-026` per discipline §2.1a." Pilot §7 tally arithmetic accounts for the drop (49 req beads instead of 50).
- **Source-spec audit.** AR-035 body (line 315): "Role is orthogonal to agent type. See §4.7.AR-026 for the normative rule; this subsection cross-references it for role-taxonomy locality." This is a textbook §2.1a triggering case (one sentence + one inline cite + no independent normative content). The collapse is correctly applied.
- **No-orphan check.** Searched the pilot for any other reference to `ar-035`: appears in §1 description (correct enumeration of the collapse), §8 F-pilot-AR-1 (correctly marked CLOSED v0.5), §9 revision history (correct provenance). No row, no edge, no foreign reference treats `ar-035` as a live bead.
- **Severity.** None. Clean.

### 2.2 F13 slot-rule resolution for AR-013/052/053 envelope-slot trio — VERIFY CLEAN

- **What r2 must verify:** `ar-053 → {ar-013, ar-052}` survive; reverse cites `ar-013 → ar-053` and `ar-052 → ar-053` are NOT emitted; `ar-052 → ar-016` is emitted.
- **Verified — `ar-053`'s edges.** Pilot row for `ar-053` (line 44) lists `blocks` edges: `ar-052, ar-013`. Both targets present. Notes column explicitly cites discipline §2.7 F13: "Per discipline §2.7 F13 slot-rule heuristic: `ar-053 → {ar-013, ar-052}` are the normative deps (slot points at what fills it); the reverse cites `ar-013 → ar-053` and `ar-052 → ar-053` are informational plumbing — no edge emitted."
- **Verified — reverse cites suppressed.** `ar-013` row (line 56) lists `blocks` edges `ar-001, ar-005, ar-052`. No `ar-053` predecessor. ✓ correct (F13 suppression). `ar-052` row (line 43) lists `blocks` edge `ar-016`. No `ar-053` predecessor. ✓ correct.
- **Verified — non-bidirectional half emitted.** `ar-052` row carries `ar-016` as the sole predecessor, justified by AR-052's body inline-citing "§4.5.AR-016" (line 80 of architecture.md: "realized at MVH as a Go package inside the daemon binary per §4.5.AR-016"). ✓ correct.
- **Cycle check.** Pilot §5 cycle inspection (line 154) walks the trio explicitly: "AR-053 → {AR-052, AR-013}; reverse cites (AR-013 → AR-053 and AR-052 → AR-053) reclassified as informational per discipline §2.7 F13 slot-rule heuristic — no edges emitted. No cycle." Confirmed by independent walk: AR-053 → AR-013, AR-053 → AR-052, AR-052 → AR-016; AR-013 has no path back to AR-053 (AR-013 → AR-001/AR-005/AR-052; AR-052 → AR-016 only). DAG. ✓
- **Severity.** None. F13 application is mechanically correct on both pairs.

### 2.3 The 9 reported term-use edges — VERIFY 9/9 JUSTIFIED

Discipline §3.1 step 5 (term-use rule) — a defined-term use in a requirement's body generates a `blocks` edge to the defining bead. Each of the 9 v0.2.0-added edges audited against the source spec's body text:

#### 2.3.1 `ar-052 → ar-016` — JUSTIFIED
- AR-052 body (line 80): "A `runtime-subsystem` spec is realized at MVH as a Go package inside the daemon binary **per §4.5.AR-016**". This is technically more than term-use — it's an inline cite — but either reading justifies the edge. ✓

#### 2.3.2 `ar-004 → ar-013` — JUSTIFIED
- AR-004 body (line 112): "Every type a subsystem exports across **its envelope (§4.4)** — including types transitively referenced by any event payload a different subsystem consumes". The defined term "envelope" is owned by AR-013 (which titles "Subsystem envelope declaration" and lists the eight envelope elements). The §4.4 anchor resolves to the section AR-013 anchors. ✓

#### 2.3.3 `ar-014 → ar-005` — JUSTIFIED
- AR-014 body (line 178): "violate the **mechanism/cognition boundary** by performing cognition in framework code". The "mechanism/cognition" tag dichotomy is established by AR-005 ("Every normative requirement … MUST carry a `Tags:` line with exactly one of `mechanism` or `cognition`"). ✓

#### 2.3.4 `ar-017 → ar-013` — JUSTIFIED
- AR-017 body (line 198): "None of (a), (b), (c) is a **subsystem**; handlers implement the handler contract but **declare no envelope**". "Subsystem" and "envelope" are both AR-013-owned terms (subsystem = "a unit declaring an envelope per §4.4"). ✓

#### 2.3.5 `ar-019 → ar-013` — JUSTIFIED
- AR-019 body (line 210): "the **subsystem envelope's semantics** MUST remain unchanged: each Go package that **declares an envelope is a subsystem** regardless of which process binary hosts it". Two distinct uses of AR-013-defined terms. ✓

#### 2.3.6 `ar-028 → ar-016` — JUSTIFIED
- AR-028 body (line 271): "Adding a new agent type MUST be an exercise of the **subsystem envelope procedure (§4.4, §4.5)**." The §4.5 anchor is the section AR-016 anchors. The pilot row notes column corroborates this: "AR-028's body inline-cites the subsystem envelope procedure as '(§4.4, §4.5)'". ✓

#### 2.3.7 `ar-029 → ar-032` — JUSTIFIED
- AR-029 body (line 277): "An `agentic` node with `actor_role=**Reviewer**` is verification-capable (cognition-tagged delegation to a reviewer-agent)". "Reviewer" is one of the seven canonical roles defined by AR-032. ✓

#### 2.3.8 `ar-030 → ar-005` — JUSTIFIED
- AR-030 body (line 283): "**Mechanism-tagged** verification nodes MUST populate a structured `evidence` field …; **cognition-tagged** verification nodes MUST populate a `notes` field". Both AR-005-defined classifiers used substantively. ✓

#### 2.3.9 `ar-031 → ar-029` — JUSTIFIED
- AR-031 body (line 289): "The three hyphenated forms `**verification-node**`, `verification-result`, `quality-gate` are distinct". `verification-node` is AR-029-owned (AR-029 establishes "The term `verification-node` in this spec and elsewhere refers to any node so configured"). ✓

**Tally: 9/9 reported term-use edges are justified by actual term use in the spec body. No over-eager emissions.**

### 2.4 Hunt for missed term-use edges — 1 MINOR finding

A walk over every active req's body looking for defined-term uses that should generate edges per §3.1 step 5:

- **AR-006 → AR-005** (already emitted in v0.1.0; preserved through v0.2.1). Body uses "mechanism-tagged evaluation point". Verified. ✓
- **AR-007 → AR-005** (already emitted). Body uses "cognition-tagged evaluation point". Verified. ✓
- **AR-039 → AR-036** (already emitted in v0.1.0). Body uses "merge agent" and "merge node" defined by AR-036. The pilot's `ar-039` row (line 80) lists `ar-036` as the sole predecessor; this matches the discipline §3.1 step 5 worked example verbatim ("AR-039 uses 'merge agent' / 'merge node' defined in AR-036 → `ar-039` blocks-on `ar-036`"). ✓
- **AR-026 → AR-032** — **MISSING.** AR-026 body (line 257): "Role is a function assignment (**Planner, Builder, Reviewer**, etc.); agent type is a process assignment". Three of the seven canonical AR-032 roles are explicitly enumerated. The pilot's `ar-026` row (line 69) lists `blocks` edges `ar-024, ar-032`. ✓ **Wait — this IS emitted.** AR-032 IS in the predecessors. Mis-flagged on first read; on closer inspection the edge is present. Re-classified: **no finding** (the edge was already there in v0.1.0 and survives, justified by both the inline `(§4.8)` cite and the role-name term use).
- **AR-038 → AR-INV-007** — **CANDIDATE for MISSING.** AR-038 body (line 335): "The **centralized-controller invariant** is the explicit inverse of the Gas Town polecats/mayors decentralized-orchestration pattern." "Centralized-controller invariant" is the title of AR-INV-007 (line 435). Per discipline §3.1 step 5 ("named invariants"), this should generate `ar-038 → ar-inv-007`. The pilot's `ar-038` row (line 79) lists `blocks` edges `(none)`. **F-pilot-AR-r2-1 (MINOR, class).** However — this is partially mitigated by the §2.5 F10 clause ("Invariant beads as edge targets") which says downstream consumers edge to the constrained §4 reqs by default; AR-038 is itself one of the AR-INV-007-constrained reqs (the sensor's own predecessors), so an edge from AR-038 → AR-INV-007 would be a reverse of the sensor↔impl one-way rule. Per §2.5: "Sensor beads `blocks-on` impl beads. Impl beads do NOT block-on their sensors". So the missing edge is correctly suppressed by the existing one-way rule, not by silence. **Re-classified: no finding** (the §2.5 one-way rule supersedes the §3.1 step 5 term-use rule for invariant-as-target cases, and this is internally consistent though not explicitly stated in either rule).
- **AR-040 → AR-INV-007** — same reasoning as AR-038 above. AR-040 body (line 347): "The **centralized-controller principle** carries a real cost". AR-040 is itself an `ar-inv-007` impl predecessor (verified at pilot §3 row 3, line 107). Same one-way-rule suppression applies. **No finding.**
- **AR-038 → AR-040 / AR-040 → AR-038** — neither uses each other's distinctive defined terms; they share the "centralized-controller" surface but no individual term ownership. No edge expected. ✓
- **AR-040 → AR-020** (already emitted). Body cites "such a pivot is a foundation amendment per §4.6" — explicit inline cite to the section AR-020 anchors. ✓
- **AR-031 → AR-030** (already emitted). Body uses "verification-result". Verified.
- **AR-024 → AR-025?** — AR-024 body does not use the regex shape; it lists handler-contract conformance elements. AR-025 is the regex shape. No term-use edge expected. ✓
- **AR-027 → AR-025** (already emitted). Body inline-cites "§4.7.AR-025". Verified. ✓
- **AR-033 → AR-032** (already emitted). Body lists Planner/Builder/Reviewer/etc. — uses the AR-032-defined seven-role vocabulary. Pilot row `ar-033` (line 76) lists `ar-032`. ✓
- **AR-034 → AR-032** (already emitted). Body refers to "each role" — uses AR-032 vocabulary. ✓
- **AR-036 → AR-032** (already emitted). Body: "Merge responsibility MUST NOT be a top-level role". Uses AR-032's "role" vocabulary. ✓
- **AR-021 → AR-032** (already emitted). Body uses "role = `Reviewer` per §4.8" — explicit cite + term-use. ✓
- **AR-021 → AR-007?** — AR-021 IS itself a cognition-tagged req that follows the AR-007 delegation-path-shape obligation. It does not USE the term "delegation path" as a defined term it depends on; it INSTANCES the obligation. Borderline. Discipline §3.1 step 5 limits term-use edges to terms whose definition is owned by another single requirement; "delegation path" is the shape AR-007 names but AR-021 is not citing it as a term-meaning, it's executing the obligation. **No finding** — borderline at most, and the pilot consistently doesn't emit "I-implement-this-rule" edges (which would be sensor↔impl one-way reversals).
- **AR-INV-001 body → AR-007** — already emitted in r1; still present in v0.2.1 (sensor edges include `ar-007`). ✓
- **AR-INV-001 body → AR-017** — already emitted. AR-INV-001 body explicitly cites "§4.5.AR-017(a)". ✓
- **AR-INV-007 body → AR-040 / AR-041 / AR-042 / AR-043 / AR-044 / AR-045** — already emitted. Sensor's §10.2 group cite "AR-038..AR-045 group" expands to all of these; pilot row 3 of §3 (line 107) lists `ar-038, ar-039, ar-040, ar-041, ar-042, ar-043, ar-044, ar-045`. ✓
- **AR-INV-008 body → AR-047/048/049/050/051** — already emitted. ✓

**Walk result: only one tentative miss surfaced (`ar-038 → ar-inv-007` / `ar-040 → ar-inv-007`), and it correctly resolves to "no edge" via the §2.5 sensor↔impl one-way rule. No actionable missed term-use edges.**

**However, surfacing this `class` co-finding for the discipline author:** the interaction between §3.1 step 5 (term-use generates a `blocks` edge to the defining bead) and §2.5 F12 (sensor beads block-on impls, impls do NOT block-on sensors) is not made explicit anywhere in v0.5. The implementer hit it in v0.2.0 by intuition. **F-pilot-AR-r2-2 (MINOR, class).** Discipline patch: §3.1 step 5 grows a sub-clause "When the defining requirement is a `<prefix>-INV-NNN` invariant, the term-use rule does NOT fire — invariant beads are sensor beads, and per §2.5 F12 impl beads do not block-on sensors." Not load-blocking; the pilot's behaviour is correct.

### 2.5 `ar-inv-001` sensor edges — §10.2 reviewer-persona bundling — 1 MINOR finding

- **What r2 must verify:** Edges include `ar-003`, `ar-007`, `ar-042` per §10.2 conformance-auditor block. Walk other §10.2 persona blocks for any other AR-INV-NNN sensor edges that should have been added.
- **Verified — `ar-inv-001`.** Pilot row (line 105) lists `blocks` predecessors: `ar-003, ar-005, ar-006, ar-007, ar-016, ar-017, ar-042`. The conformance-auditor bundle (AR-003, AR-007, AR-042, AR-INV-001) per §10.2 line 504 is fully covered: `ar-003` ✓, `ar-007` ✓, `ar-042` ✓. Notes column corroborates: "v0.2.0: added `ar-003`, `ar-042` per discipline §2.5 reviewer-persona-bundling clause (new in v0.5)". ✓
- **Hunt — other §10.2 persona blocks for AR-INV-NNN bundling.** AR §10.2 informative block (line 504) names four reviewer personas:
  - **architect** — checks "cross-cutting-invariant violations and AR-020/AR-021 amendment material-change determinations". The "cross-cutting-invariant violations" surface is generic — does NOT name a specific `<prefix>-INV-NNN` invariant. The two named §4 reqs are AR-020 and AR-021. **No invariant-bundling here.** No edges to add.
  - **conformance-auditor** — already analysed above, fully expanded.
  - **critic** — checks "AR-014/AR-015 subsystem-obligation violations and AR-017 out-of-process-actor enumeration closure". Three §4 reqs named (AR-014, AR-015, AR-017) but **no AR-INV-NNN named.** No invariant-bundling here. No edges to add.
  - **scope-steward** — checks "AR-026, AR-033, AR-034 role/agent-type orthogonality". Three §4 reqs named, **no invariant.** No edges to add.
- **Hunt — invariant-block-internal sensor predecessors.** Each AR-INV-NNN's body explicitly names which §10.2 group it ties to:
  - **AR-INV-001** body cites §10.2 conformance-auditor heuristic + AR-007 + AR-017(a). Pilot has `ar-003, ar-005, ar-006, ar-007, ar-016, ar-017, ar-042`. The AR-005/006/016 are from the AR-005..AR-007 ZFC-tagging group, AR-017 is anchored explicitly in the body. **AR-006** is a defensible add (AR-INV-001's body talks about "cognition-tagged evaluation" which AR-006/007 define; the §10.2 ZFC group bundles AR-005..AR-007). **AR-005** is similarly defensible. Both already present.
  - **AR-INV-003** body cites "§10.2 AR-009..AR-012 group". Pilot has `ar-009, ar-010, ar-011, ar-012`. ✓
  - **AR-INV-007** body cites "§10.2 AR-038..AR-045 group". Pilot has `ar-038..ar-045`. ✓
  - **AR-INV-008** body cites "§10.2 AR-047..AR-051 group". Pilot has `ar-047..ar-051`. ✓
- **Sub-finding hunt — does AR-INV-001 owe an edge to `ar-INV-003` or any other invariant?** No — AR-INV-001's sensor is independent. No.
- **Sub-finding hunt — does the architect persona's "cross-cutting-invariant violations" phrase warrant a sensor edge from EACH AR-INV-NNN to AR-020/AR-021?** Reading the §10.2 sentence carefully: "the **architect** persona checks cross-cutting-invariant violations **and** AR-020/AR-021 amendment material-change determinations." Two clauses joined by "and". Clause 1 is generic (cross-cutting-invariant violations as a class); clause 2 names specific reqs. The architect persona reviews invariant violations as a class but the §10.2 informative block doesn't name a specific `<prefix>-INV-NNN` to bundle. Per discipline §2.5 v0.5 ("when the source spec's §10.2 conformance section names a reviewer persona that bundles a §5 invariant with one or more §4 requirements"), the trigger is "names a §5 invariant" — and the architect block names AR-020 and AR-021 (both §4 reqs), not a specific `<prefix>-INV-NNN`. So no edge fires. ✓ **Note for the discipline author**: the architect-clause phrasing is on the borderline; if "cross-cutting-invariant violations" were interpreted as bundling all four AR-INV-NNN with AR-020/AR-021, the consequences would be load-bearing (each of `ar-inv-001`, `ar-inv-003`, `ar-inv-007`, `ar-inv-008` would gain edges to `ar-020` and `ar-021`). The pilot's reading (literal — must name a specific invariant by ID) is correct per discipline §2.5 v0.5; flagging only as a class-lane silence.
- **F-pilot-AR-r2-3 (MINOR, class).** Discipline §2.5's reviewer-persona-bundling clause v0.5 is implicit about whether the bundling trigger requires the invariant to be named by full ID (`AR-INV-001`) or whether a category phrase ("cross-cutting-invariant violations") suffices. Recommend §2.5 grow a sub-clause: "The bundling trigger requires the specific `<prefix>-INV-NNN` ID to appear in the persona block's named-target list; category phrases like 'cross-cutting-invariant violations' or 'all invariant violations' do NOT trigger the bundling rule." This pre-empts AR's v0.4-or-later revision adding such a phrase elsewhere, and also pre-empts other specs writing similar §10.2 prose. Not load-blocking; pilot is correct as written.

### 2.6 v0.2.1 fix — `ar-011` no longer blocks on `ar-029` — VERIFY CLEAN

- **What r2 must verify:** The `ar-011 → ar-029` edge is gone; `ar-011`'s blocks edges are now `ar-030, ar-032`.
- **Verified.** Pilot row for `ar-011` (line 54) lists `blocks` edges: `ar-030, ar-032`. The `ar-029` predecessor is absent. Notes column documents the v0.2.1 fix: "v0.2.1: dropped invented edge `ar-029` per F-pilot-AR-9 (AR-011's body inline-cites only `§4.7.AR-030`; the AR-029 reference was a v0.1.0 citation-projection error)."
- **Source-spec audit.** AR-011 body (line 158): cites `§4.7.AR-030` (inline cite to AR-030). Does NOT inline-cite AR-029. Verified by reading the body in full — the only AR-NNN reference is `§4.7.AR-030`. ✓
- **`ar-032` term-use justification.** AR-011 body uses "actor_role=`Reviewer`" — Reviewer is an AR-032-defined canonical role. ✓ Term-use edge correctly retained.
- **Reverse direction.** `ar-029` row (line 72) still lists `ar-011` as a predecessor, which is the legitimate one-way edge (AR-029's body inline-cites "§4.5.AR-018 and §4.3.AR-011"). ✓
- **Cycle check.** With v0.2.1's drop, AR-029 → AR-011 is one-way. No cycle. Pilot §5 line 156 walks this explicitly and confirms.
- **Severity.** None. Clean fix.

### 2.7 `ar-schema.agent-type-identifier` retag — VERIFY CLEAN

- **What r2 must verify:** Tagged `kind:primitive-shape`.
- **Verified.** Pilot row in §4 (line 122): tag column reads "mech, `kind:primitive-shape`". The `kind:schema` tag (the v0.1.0 form) is NOT present in v0.2.1; the retag is complete. Notes column corroborates: "v0.2.0: retagged from `kind:schema` to `kind:primitive-shape` per discipline §2.6 (v0.5) which added the constrained-primitive schema category."
- **Discipline §2.6 v0.5 audit.** Lines 138–145 of discipline.md establish the `kind:primitive-shape` tag for "constrained primitive types" and use AR's `agent_type` regex as the worked example. Pilot tag is consistent with discipline.
- **Tag-mapping table consistency.** Discipline §4 tag-mapping table line 382 explicitly lists "`<constrained primitive>` (§6, e.g., regex-validated string) | bead ID `<prefix-lower>-schema.<type-kebab>` | Tag `kind:primitive-shape`". Pilot uses the bead ID `ar-schema.agent-type-identifier` and tag `kind:primitive-shape`. ✓
- **Note: ambiguity around dual tags.** Discipline §2.6 v0.5 line 145 says "Tag: `kind:primitive-shape` (alongside `kind:schema`/`kind:interface`/`kind:enum`)" — the parenthetical "alongside" reads as "in the same family of `kind:` tags" not "in addition to `kind:schema`". The pilot interprets it as the former (single `kind:primitive-shape` tag, no `kind:schema` co-tag), which is the cleaner reading. Discipline §4 tag-mapping table corroborates with one row per tag, no co-tagging. ✓ **No finding** (interpretation is internally consistent and matches the §4 table).
- **Severity.** None. Clean.

### 2.8 Random first-class regression-check beads (3) — VERIFY CLEAN

#### 2.8.1 `ar-021` (cognition bead, off-baseline axes)
- Description (pilot line 64) quotes the full delegation path verbatim from the spec body — role, model class, input shape, output. ✓
- Tags: `cog, axis:llm-freedom-bounded, axis:io-determinism-best-effort, req:AR-021`. Matches spec line 226–227's `Tags: cognition` and `Axes: llm-freedom=bounded; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent`. Off-baseline axes correctly emitted (llm-freedom, io-determinism); baseline axes correctly suppressed (replay-safety, idempotency). ✓
- Blocks: `ar-020, ar-032`. AR-021 body inline-cites "§4.6" (AR-020's section) and uses "role = `Reviewer` per §4.8" (AR-032 + term-use). Both edges justified. ✓ No regression.

#### 2.8.2 `ar-039`
- Pilot row (line 80) lists `blocks` edge `ar-036` (sole predecessor). Description: "A merge agent (when workflow uses a distinct merge node) MUST operate in the same worktree the implementer used; the worktree is leased by the workflow run, not by any individual agent."
- Source-spec audit: AR-039 body (line 341) does not inline-cite AR-036; uses defined terms "merge agent" and "merge node" (both AR-036-owned). Edge justified by term-use rule. ✓ No regression. (This finding was MINOR class in r1; the discipline §3.1 step 5 v0.5 patch closes it.)

#### 2.8.3 `ar-042`
- Pilot row (line 83) lists `blocks` edge `ar-029`. Description correctly cites the meta-rule. AR-042 body (line 359): "The sensor MAY be a lint rule, a verification-node role-function (per AR-029), a review-agent scenario, or a conformance test cited by ID." Inline cite to AR-029. ✓ Edge justified.
- No other edges. AR-042 does not inline-cite AR-005 / AR-007 / etc.; the term-use is "sensor", "invariant" — neither has a single-owning AR req. ✓ No regression.

## 3. Missing-coalesce flags

None. R1 considered three candidate clusters (AR-029/030/031, AR-047/048/049, AR-052/053) and all three are correctly NOT coalesced. R2 finds no new candidates. The AR-035 collapse (now via §2.1a, not §2.3) is handled correctly.

## 4. Over-split flags

None. AR has zero step beads (declaration-heavy spec, no multi-step protocols). r1 verified this; r2 confirms.

## 5. Cross-bead consistency / asymmetry findings

### 5.1 Term-use rule (§3.1 step 5) vs §10.2 persona-bundling (§2.5 v0.5) — naming asymmetry

**F-pilot-AR-r2-4 (MAJOR, class — discipline-lane bias).** Both rules are mechanisms for emitting "soft cite" edges that aren't from explicit inline cites in normative prose:
- §3.1 step 5 fires on **term USE** in the body (e.g., AR-006 uses "mechanism-tagged" → edge to AR-005).
- §2.5 v0.5 reviewer-persona-bundling fires on **§10.2 persona-block grouping** (e.g., AR-INV-001 + AR-003/AR-007/AR-042 in conformance-auditor block).

The asymmetry: term-use rule v0.5 is broadly defined ("any defined term whose definition is owned by another single requirement") and operates from the citing req's body. The persona-bundling rule is narrowly defined (only §10.2 reviewer-persona blocks; only when bundling §5 invariants with §4 reqs). But there's a third mechanism the discipline does NOT name: **§10.2 conformance-group cites** that are NOT inside reviewer-persona blocks (e.g., the "AR-005..AR-007 (ZFC) group" at line 507, the "AR-009..AR-012 (required triple) group" at line 508). The pilot's `ar-inv-001` and `ar-inv-003` and `ar-inv-007` and `ar-inv-008` sensors all consume the relevant §10.2 group cites for their predecessors; this is correct behaviour but the rule isn't formally stated anywhere. The §2.5 sensor rule says "the §10.2 group" implicitly.

Recommend §2.5 grows a clear sub-clause distinguishing three §10.2 sensor-edge sources:
1. **Conformance-group prose cites** ("AR-009..AR-012 group") → sensor blocks-on each req in the cited range. (This is the dominant pattern; already implicitly applied.)
2. **Reviewer-persona group bundling** (v0.5 clause; conformance-auditor + AR-INV-001 + AR-003/007/042) → sensor blocks-on each bundled §4 req.
3. **Sensor-block body inline cites** (e.g., "§4.5.AR-017(a)") → sensor blocks-on the cited req.

Currently rules 1 and 3 are folded into "the §10.2 group" colloquially; rule 2 is the v0.5 addition. Disambiguation prevents confusion when the next pilot has §10.2 conformance prose without a reviewer-persona block.

This is `class` because it would affect every spec's sensor-edge derivation, not just AR.

## 6. Severity-level summary

- **BLOCKER (0):** None. All five r1 BLOCKERs / MAJORs are CLEAN in v0.2.1.
- **MAJOR (1):** F-pilot-AR-r2-4 (class) — §2.5 sensor-edge sources need formal disambiguation; recurring at corpus scale.
- **MINOR (2):**
  - F-pilot-AR-r2-2 (class) — interaction between §3.1 step 5 (term-use) and §2.5 F12 (sensor↔impl one-way) needs explicit clause to clarify that invariants-as-target are exempt from term-use.
  - F-pilot-AR-r2-3 (class) — §2.5 reviewer-persona-bundling trigger should explicitly require named `<prefix>-INV-NNN` ID, not category phrases.

**Total: 0 BLOCKER, 1 MAJOR, 2 MINOR. All 3 actionable findings are class-tagged (discipline-lane).**

## 7. Findings list (consolidated)

| # | Finding | Severity | Lane | Bead(s) / discipline §       |
|---|---|---|---|---|
| F-pilot-AR-r2-2 | §3.1 step 5 (term-use) and §2.5 F12 (sensor one-way) interaction is implicit; pilot got it right but discipline doesn't say it. | MINOR | class | discipline §3.1 step 5 / §2.5 F12 |
| F-pilot-AR-r2-3 | §2.5 reviewer-persona-bundling trigger phrasing v0.5 doesn't say whether category phrases ("cross-cutting-invariant violations") count. Pilot's literal-named-ID reading is correct. | MINOR | class | discipline §2.5 |
| F-pilot-AR-r2-4 | §10.2 sensor-edge sources need disambiguation: conformance-group cites vs reviewer-persona bundling vs sensor-block body cites. Three patterns currently folded together. | MAJOR | class | discipline §2.5 |

All three are class-lane (discipline-patch lane); none gates the v0.2.1 load. No pilot patches needed.

## 8. Delta from r1

**r1 had** 2 BLOCKER, 2 MAJOR, 4 MINOR (= 8 findings; 6 class-tagged, 2 strictly local).
**r2 has** 0 BLOCKER, 1 MAJOR, 2 MINOR (= 3 findings; all class-tagged).

The r1 BLOCKERs (2.1: invented `ar-029 → ar-016`; 2.2: invented `ar-035 → ar-032`) are both gone — 2.1 was fixed in v0.2.0's drop of `ar-016` from `ar-029` blocks; 2.2 is mooted by the AR-035 collapse to a notes-line. The r1 MAJORs (2.3: AR-013↔AR-053 bidirectional; 2.4: AR-052 missing edges + AR-052↔AR-053 bidirectional) are both fixed by discipline §2.7 F13 v0.5 + the v0.2.0 application: `ar-053 → {ar-013, ar-052}` survive normatively, the reverse cites are correctly suppressed as informational, and the non-bidirectional half `ar-052 → ar-016` is emitted. The r1 MINOR class findings (2.5/2.6/2.7: term-use; 2.8: §10.2 persona bundling) are closed by discipline §3.1 step 5 + §2.5 v0.5 clauses, both correctly applied across the 9 reported term-use edges and the `ar-inv-001` expansion. The new r2 findings are all `class`-lane and second-order: they refine the rules added in v0.5 to handle their own boundary cases (term-use on invariants, persona-bundling phrasing, §10.2 sensor-source disambiguation). None blocks load.
