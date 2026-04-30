# AR Pilot Decomposition-Quality Review (r1) — 2026-04-27

Reviewer: decomposition-quality reviewer (per `pilot-review-protocol.md` v0.2 §3.2). Pilot under review: `docs/decompose-to-tasks/ar-pilot.md` v0.1.0 against `specs/architecture.md` v0.3.1, with discipline `docs/decompose-to-tasks/discipline.md` v0.4. Result: pilot is structurally sound at the granularity / coalesce / sensor / schema level — descriptions faithfully cite the spec, sensors are real verification mechanisms (not invariant restatements), the one cognition bead correctly quotes its delegation path. The substantive defects cluster in the intra-spec edge graph: at least one invented edge (BLOCKER), one almost certainly invented edge (MAJOR), one missing-edge plus undeclared bidirectional inline-cite pair (MAJOR), and several borderline edges that depend on whether "uses-a-defined-term" counts as an inline cite (MINOR / class-lane silence in the discipline).

## 1. Sampled beads (13)

Weighted toward the riskier classes per protocol §3.2 (AR has 0 coalesces and 0 multi-step splits, so the weighting tipped toward sensors, the cognition bead, the schema bead, and structurally-tight first-class beads).

- **All 4 sensors:** `ar-inv-001`, `ar-inv-003`, `ar-inv-007`, `ar-inv-008` (Q4).
- **The 1 schema:** `ar-schema.agent-type-identifier` (Q5).
- **The only cognition bead:** `ar-021` (Q1 + §2.8 delegation-path quote rule).
- **The only off-baseline-axis bead:** `ar-021` (covered above; its `axis:llm-freedom-bounded` + `axis:io-determinism-best-effort` are the only two axis tags in the entire pilot).
- **The pilot's self-flagged "pure cross-reference" bead:** `ar-035` (Q1 + granularity).
- **Tight-pair structural beads:** `ar-052`, `ar-053`, `ar-013` (envelope-slot trio).
- **Verification-naming trio:** `ar-029`, `ar-030`, `ar-031` (potential missing-coalesce smell + §3.1 edge derivation).
- **Random first-class beads:** `ar-014`, `ar-027`, `ar-039` (random selection across §4.4, §4.7, §4.9).

## 2. Per-sample findings

(Sampled beads with NO issue: `ar-inv-003`, `ar-inv-007`, `ar-inv-008`, `ar-schema.agent-type-identifier`, `ar-021`, `ar-027`, `ar-030`, `ar-014`, `ar-053`. They pass Q1/Q4/Q5 cleanly.)

### 2.1 `ar-029` — invented intra-spec edge to `ar-016`

- **Spec reqs covered:** AR-029.
- **Question flagged:** Q1 (description-vs-spec).
- **Concern.** Pilot row 21 lists `blocks` edges `ar-011, ar-016, ar-018`. AR-029's normative body (lines 277, `specs/architecture.md`) inline-cites two AR requirements: `§4.5.AR-018` ("No 'verifier' subsystem exists per §4.5.AR-018") and `§4.3.AR-011` (same sentence). It does NOT inline-cite AR-016 or §4.5.AR-016. The `ar-016` edge is **invented**: per discipline §2.7 forbidden rule ("inventing cross-spec edges that the spec does not cite"; same logic applies intra-spec per §3.1.1).
- **Severity.** BLOCKER (per protocol §3.2: "BLOCKER (description doesn't match spec — implementation would diverge)") — an extra `blocks` edge over-gates AR-029's implementation against the wrong predecessor; if AR-016 were ever blocked while AR-018 was clear, the readiness workflow would mis-classify AR-029 as not-ready.
- **Lane.** `local`. Discipline §3.1 step 1 is unambiguous ("List every cross-spec citation in the source requirement's body"). The pilot misapplied the rule by reading `§4.5.AR-018` as "anything in §4.5" rather than the specific AR-018 cite.
- **Suggested fix.** Drop `ar-016` from the `ar-029` blocks row. Final row: `ar-029` blocks on `ar-011, ar-018`.

### 2.2 `ar-035` — invented intra-spec edge to `ar-032`

- **Spec reqs covered:** AR-035.
- **Question flagged:** Q1.
- **Concern.** Pilot row 27 lists `blocks` edges `ar-026, ar-032`. AR-035's normative body (line 315) is one sentence: "Role is orthogonal to agent type. See §4.7.AR-026 for the normative rule; this subsection cross-references it for role-taxonomy locality." The single inline cite is `§4.7.AR-026`. AR-035 is located inside §4.8 (the section AR-032 anchors) but does NOT inline-cite AR-032; the role-taxonomy locality is asserted, not depended on. The `ar-032` edge is **invented** by location-context inference, not by inline cite.
- **Severity.** BLOCKER — same reasoning as 2.1; an over-gating edge.
- **Lane.** `local`. The discipline rule is clear; pilot inferred a location-based edge.
- **Suggested fix.** Drop `ar-032` from the `ar-035` blocks row. (Note: this finding is independent of the pilot's own `F-pilot-AR-1` flag in §8 — even if AR-035 collapses to a notes line on `ar-026` per a future discipline patch, the spurious edge is wrong as written.)

### 2.3 `ar-013` ↔ `ar-053` — undeclared bidirectional inline-cite pair

- **Spec reqs covered:** AR-013, AR-053.
- **Question flagged:** Q1 (description-vs-spec edge derivation).
- **Concern.** Per discipline §2.7 F13 ("Bidirectional inline cites are a smell. If A inline-cites B AND B inline-cites A, the resulting edge graph would have a cycle… surface to the discipline author for resolution"). AR-053 inline-cites AR-013 (line 86: "declare the eight envelope elements of §4.4.AR-013"). AR-013 inline-cites AR-053 (line 172: "declare its envelope in the §4.a slot reserved by §4.0.AR-053"). The pilot emits one direction only (`ar-053 → ar-013` via row 4) and silently drops the reverse (`ar-013 → ar-053`). The pilot's row for `ar-013` has `ar-001, ar-005, ar-052` as predecessors but not `ar-053`.
- **Severity.** MAJOR — the bidirectional cite is not surfaced as a smell-flag in §8 of the pilot. Per F13, bidirectional cites MUST be surfaced for resolution; one of the two cites should be reclassified (likely AR-013's "reserved by §4.0.AR-053" is informational because AR-053 is itself the slot rule, while AR-053's "eight envelope elements of AR-013" is the load-bearing dep). Silent drop loses provenance.
- **Lane.** `local` for the pilot (rule exists; pilot didn't apply it). However the *specific* surfaced question (which of two cites is informational) is `class`-adjacent — the discipline's F13 only says "surface to the discipline author"; it gives no guidance on which side typically reclassifies for slot-vs-content rule pairs. **Tagging primarily `local` per protocol's bias rule, with a `class` co-tag for the resolution-direction silence.** Bias-toward-class per §4.1: any class tag routes to the discipline lane, so this finding belongs there.
- **Suggested fix.** Pilot §8 gains an item (`F-pilot-AR-8`) flagging the AR-013↔AR-053 bidirectional cite and proposing the resolution: AR-013's "§4.0.AR-053" cite is reclassified as informational (slot-rule plumbing AR-053 owns), the AR-053→AR-013 cite stays normative. Discipline §2.7 may benefit from an example of the slot-rule pattern.

### 2.4 `ar-052` — missing intra-spec edges to `ar-016` and `ar-053`

- **Spec reqs covered:** AR-052.
- **Question flagged:** Q1 (edge derivation).
- **Concern.** Pilot row 1 lists `blocks` edges `(none)` for `ar-052`. AR-052's normative body (line 80) inline-cites two AR requirements: `§4.5.AR-016` ("realized at MVH as a Go package inside the daemon binary per §4.5.AR-016") and `AR-053` ("MUST declare an envelope per AR-053"). Neither edge is emitted. Per discipline §3.1 step 2 ("For each citation that names a SPECIFIC requirement ID… emit a `blocks` edge"), both edges are required.
- **Severity.** MAJOR — missing inline-cite-derived edges. Implementation order would be wrong: AR-052's category-declaration rule depends on AR-016 (Go-package realization) and AR-053 (slot rule) being defined.
- **Lane.** `local`. Discipline rule is clear; pilot omitted both edges.
- **Suggested fix.** Add `ar-052` blocks on `ar-016, ar-053`. Note this interacts with finding 2.3 — `ar-052 → ar-053` is fine on its own (no reverse cite from AR-053 to AR-052), but the AR-052 row's "first requirement under §4.0 (placed before AR-001 numerically out-of-order; it gates AR-013 / AR-053)" prose implies AR-053 depends on AR-052, which is consistent with the AR-053→AR-052 edge already in row 4. The added AR-052→AR-053 edge would create a fresh cycle with AR-053→AR-052. **Resolution:** AR-052's "MUST declare an envelope per AR-053" is the cite that needs reclassification (informational — AR-052 names the obligation; AR-053 is the obligation). After reclassification, only `ar-052 → ar-016` survives as an emitted edge; `ar-053 → ar-052` stays. This exposes a **second** bidirectional-cite smell (AR-052↔AR-053) parallel to 2.3.
- **Class-lane co-tag.** The fact that two of three structurally-tight envelope-slot beads (AR-013, AR-052, AR-053) form mutually-citing pairs that the pilot silently chose one direction for is `class`-relevant: the discipline's F13 fires twice in this one pilot, and the silent-resolution pattern says the discipline's "surface to author" guidance is being ignored in practice. Discipline-lane finding: F13 needs a pre-emit lint, not just an author-surface obligation, OR the discipline gains a pattern entry for "slot-rule vs content-rule pairs" with a default reclassification.

### 2.5 `ar-006`, `ar-007` — soft-cite edges to `ar-005`

- **Spec reqs covered:** AR-006, AR-007 (paired finding).
- **Question flagged:** Q1.
- **Concern.** Pilot rows for `ar-006` and `ar-007` both list `ar-005` as a `blocks` predecessor. Neither AR-006's nor AR-007's normative body inline-cites AR-005 by ID; they USE the defined terms `mechanism`-tagged and `cognition`-tagged that AR-005 establishes. Per discipline §3.1 step 1 (citations are inline cites of `[<other-spec-id>.md §N]` or `<OTHER-PREFIX>-NNN`), use-of-a-term is not a citation. The edges are defensible as conceptual dependencies but not justified by the discipline's mechanical rule.
- **Severity.** MINOR (defensible direction; the bias is toward over-gating, which is safe for readiness ordering but creates a pattern the discipline does not authorize).
- **Lane.** `class`. The discipline is silent on intra-spec "uses-a-defined-term" edges. AR has many such cases (AR-006/007 use AR-005's tags; AR-031 uses `verification-result` from AR-030; AR-038 references "centralized-controller invariant" without inline-citing AR-INV-007). Multiple specs will hit this. Per protocol §4.1 probe 3 (silence): a gap in coverage is a class finding.
- **Suggested fix.** Discipline §3.1 / §2.7 grows a clause: "Inline use of a term whose definition is owned by another requirement in the same spec (`mechanism`-tagged, `cognition`-tagged, named schemas, named invariants) generates a `blocks` edge to the defining requirement bead." OR explicitly excludes term-use from edge generation. Either choice is fine; the silence is the bug. Pilot's current behaviour (emit-the-edge) is the safer default.

### 2.6 `ar-039` — possibly invented edge to `ar-036`

- **Spec reqs covered:** AR-039.
- **Question flagged:** Q1.
- **Concern.** Pilot row 39 lists `blocks` edge `ar-036`. AR-039's body (line 341) is two sentences: "A merge agent (when the workflow uses a distinct merge node) MUST operate in the same worktree the implementer used; the worktree is leased by the workflow run, not by any individual agent. See [docs/foundation/components.md §5]." It does not inline-cite AR-036; it uses the term "merge agent" / "merge node" that AR-036 defines. Same shape as finding 2.5 — a conceptual / term-use edge that the discipline does not formally authorize.
- **Severity.** MINOR.
- **Lane.** `class` (same root cause as 2.5 — discipline is silent on term-use edges).
- **Suggested fix.** Folded into the discipline-lane fix in 2.5.

### 2.7 `ar-031` — borderline edge inventory

- **Spec reqs covered:** AR-031.
- **Question flagged:** Q1.
- **Concern.** Pilot row 23 lists `blocks` edge `ar-030`. AR-031's body (line 289) does not inline-cite an AR requirement; it uses the term `verification-result` (owned by AR-030) and lists the three hyphenated forms (each owned by AR-029, AR-030, AR-031). The pilot picks `ar-030` as the predecessor. Per the literal §3.1 rule, no edge is generated; per term-use, two edges (`ar-029, ar-030`) would be. The pilot is inconsistent with itself: row 23 has just `ar-030`, but row 21 (`ar-029`) generated edges from a similar one-sentence cite list, and row 22 (`ar-030`) edges to `ar-029`. Suggests the pilot author ran out of patience by AR-031.
- **Severity.** MINOR.
- **Lane.** `class` (same root cause as 2.5 / 2.6).
- **Suggested fix.** Same as 2.5.

### 2.8 `ar-inv-001` — possibly missing edge to `ar-003`

- **Spec reqs covered:** AR-INV-001.
- **Question flagged:** Q4 (sensor edge inventory).
- **Concern.** Pilot row 1 of §3 lists `blocks` predecessors `ar-005, ar-006, ar-007, ar-016, ar-017`. The §10.2 "AR-005 — AR-007 (ZFC)" group is the natural mechanical answer, but AR-INV-001's body explicitly anchors at `§4.5.AR-017(a)` (agent-handler subprocess), and the spec's §10.2 reviewer-persona block (line 504) explicitly bundles AR-003 with AR-INV-001 under the conformance-auditor: "the **conformance-auditor** persona checks AR-003 daemon/handler boundary, AR-007 delegation-path completeness, AR-042 invariant-sensor grounding, and AR-INV-001 corpus-search heuristics." AR-003 is the daemon/handler-boundary profile rule that AR-INV-001 enforces. The pilot does not emit `ar-inv-001 → ar-003`.
- **Severity.** MINOR — defensible to omit (AR-003 is a §4.1 axis rule, not the cog/mech tag rule the invariant directly sensors), but the §10.2 persona-bundling reads as a soft inline-cite that AR-INV-001's sensor de facto verifies.
- **Lane.** Borderline. `local` if the discipline rule is "edges from §10.2 group only." `class` if §10.2 reviewer-persona-bundling counts as an inline cite for sensor edge derivation. Tagging `class` per the bias rule.
- **Suggested fix.** Discipline §2.5 grows a clause clarifying whether reviewer-persona group bundling in §10.2 counts as a sensor-edge source. If yes, add `ar-003` to the `ar-inv-001` predecessors.

## 3. Missing-coalesce flags

None at BLOCKER or MAJOR severity. Three candidate clusters were considered:

- **AR-029 + AR-030 + AR-031 (verification naming).** Share the canonical-hyphenated-form theme. Each is independently testable, addresses a different surface (enum membership / outcome shape / gate-evaluator), and the descriptions for each cite different mechanisms. Same shape as discipline §2.3's BI-025a..e example (orthogonal concerns kept separate). Correctly NOT coalesced.
- **AR-047 + AR-048 + AR-049 (artifact definitions).** Three short definition stubs. They share the three-artifact taxonomy, but coalescing would lose the per-artifact testability and obscure the AR-050 / AR-051 / AR-INV-008 references which name each by ID. Correctly NOT coalesced.
- **AR-052 + AR-053 (envelope-slot pair).** Considered as a coalesce candidate (front-matter rule + section-slot rule could share a "spec category enforcement" bead). They address different surfaces (front matter vs §4.a section), each is independently testable. Per discipline §2.3 they are not coalescible. Correctly NOT coalesced.

## 4. Over-split flags

None. AR has zero multi-step splits (zero step beads). The pilot's §1 is explicit: "0 multi-step protocol requirements in §4 — no §4 requirement contains a numbered step list." Verified by direct §4 walk; no AR requirement has a numbered step list. Vacuously satisfies §2.2.

## 5. Pure-declaration exception (§2.1) check

Discipline §2.1's third exception says a requirement covered by a §6 schema bead becomes a notes line on the schema bead, not its own bead. AR has only one §6 schema declaration (the agent-type identifier regex in §6.1) and two requirements that depend on that shape: AR-025 (declares the regex) and AR-027 (declares the byte-equal cross-subsystem reference points). The pilot mints `ar-025`, `ar-027`, AND `ar-schema.agent-type-identifier` as three separate beads, with `ar-025` blocking on `ar-schema.agent-type-identifier`. **AR-025 is a candidate for the pure-declaration exception** — its body declares the regex shape and is fully covered by the schema bead's contents. The pilot conservatively keeps AR-025 as a separate bead. This is consistent with discipline §2.6 ("schema beads are extracted from §4 requirement beads even when a single §4 requirement appears to 'own' the schema") which favours separation; the §2.1 third exception is the narrower path. The two rules slightly tension here. **No finding** — the pilot's conservative call is defensible; flagging only as observation. (Pilot §8 F-pilot-AR-3 already raises a related class concern about the schema-typology gap for primitive shapes; that's the right place for this tension.)

## 6. Cognition-bead delegation-path quote (§2.8) check

The pilot has exactly one cognition-tagged bead, `ar-021`. Per discipline §2.8 cognition rule and tag-mapping table: the bead description MUST quote the delegation path (role + model class + input shape). The pilot's description for `ar-021` quotes:

- **Role:** "Reviewer (architect persona per build-practices.md)" — present.
- **Model class:** "agentic (LLM-backed reviewer subagent invoked through handler-contract)" — present.
- **Input shape:** "amendment proposal + diff of proposed change against prior foundation baseline" — present.
- **Output:** "structured `material | non-material` verdict with written rationale" — present (bonus; not required by §2.8 but adds traceability).

All three required elements present and verbatim from the spec body. **No finding.** The pilot's own §8 F-pilot-AR-4 raises the description-length question (~300 chars); that is `class`-lane and already flagged by the pilot. No additional concern.

## 7. Axis-tag mechanical correctness (§2.8) check

`ar-021` is the only bead with axis tags. Spec line 227 declares `Axes: llm-freedom=bounded; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent`. Per discipline §2.8 "axis tags are emitted only when off-baseline":

- `llm-freedom=bounded` (off-baseline `none`) → `axis:llm-freedom-bounded` ✓ emitted.
- `io-determinism=best-effort` (off-baseline `deterministic`) → `axis:io-determinism-best-effort` ✓ emitted.
- `replay-safety=safe` (baseline) → no tag ✓ correct (no tag emitted).
- `idempotency=idempotent` (baseline) → no tag ✓ correct.

Mechanical correctness: 4/4. **No finding.**

## 8. Retired-ID logic check

AR has 7 retired IDs total (3 §4: AR-008, AR-037, AR-046; 4 §5: AR-INV-002/004/005/006). Per discipline §2.11(b), retired IDs do NOT mint beads; spec-parent description enumerates them. The pilot:

- Lists all 7 in the spec-parent description (§1, line 26). ✓
- Does not mint beads for any of the 7. ✓
- AR-007's description includes "EXAMPLE block (anti-pattern list from retired AR-008) is non-edge-generating per discipline §3.1" — correctly identifies that the retired AR-008's content moved into AR-007's EXAMPLE block but generates no bead obligation. ✓
- AR-INV-007's row notes "Promoted from retired AR-037 at v0.3.0" — correct provenance. ✓
- AR-INV-008's row notes "Promoted from retired AR-046 at v0.3.0" — correct provenance. ✓

No bead description references retired-IDs as live work. **No finding.**

## 9. Findings list (consolidated)

| # | Finding | Severity | Lane | Bead(s) |
|---|---|---|---|---|
| 2.1 | Invented intra-spec edge `ar-029 → ar-016` (no inline cite in AR-029 body). | BLOCKER | local | `ar-029` |
| 2.2 | Invented intra-spec edge `ar-035 → ar-032` (AR-035 inline-cites only AR-026). | BLOCKER | local | `ar-035` |
| 2.3 | AR-013 ↔ AR-053 bidirectional inline-cite pair silently emitted as one-direction edge; F13 not surfaced. | MAJOR | local + class co-tag | `ar-013`, `ar-053` |
| 2.4 | Missing intra-spec edges from `ar-052` to `ar-016` and `ar-053` (both inline-cited in AR-052 body); also exposes a second bidirectional-cite smell AR-052 ↔ AR-053. | MAJOR | local + class co-tag | `ar-052` |
| 2.5 | Term-use edges (`ar-006 → ar-005`, `ar-007 → ar-005`) not authorized by discipline §3.1's literal mechanical rule; emitted defensibly but discipline is silent. | MINOR | class | `ar-006`, `ar-007` |
| 2.6 | Term-use edge `ar-039 → ar-036` — same root cause as 2.5. | MINOR | class | `ar-039` |
| 2.7 | Inconsistent term-use edge inventory across `ar-029` / `ar-030` / `ar-031` — same root cause as 2.5. | MINOR | class | `ar-031` |
| 2.8 | Possibly missing sensor edge `ar-inv-001 → ar-003` based on §10.2 reviewer-persona bundling — discipline silent on whether persona-bundling counts as inline cite. | MINOR | class | `ar-inv-001` |

Counts: **2 BLOCKER, 2 MAJOR, 4 MINOR. Class-tagged: 6** (2 of which are the class co-tag on findings primarily local; per protocol §4.1, any class tag routes to the discipline lane unless overruled in synthesis).

## 10. Severity-level summary

- **BLOCKER (2):** 2.1, 2.2 — both invented intra-spec edges. Pilot-patch lane (local). Must be fixed before load.
- **MAJOR (2):** 2.3, 2.4 — both involve AR-052 / AR-053 / AR-013 envelope-slot trio's bidirectional cites. Each has a `local` primary tag (pilot didn't apply F13's surface-for-resolution rule) plus a `class` co-tag (the F13 rule lacks resolution guidance for slot-rule vs content-rule pairs, which AR has TWO instances of). Per protocol §4.1's class-bias rule, both findings route to the discipline lane.
- **MINOR (4):** 2.5, 2.6, 2.7, 2.8 — all class-lane silence on whether term-use, persona-bundling, or other "soft cite" patterns generate edges. The pilot's behaviour (emit-the-edge for term-use; skip-the-edge for persona-bundling) is internally inconsistent, suggesting the discipline patch should pick one direction explicitly. These can batch into the next discipline-patch pass per protocol §4.2.

## 11. Most-important-finding paragraph

The most important finding is the AR-052 / AR-053 / AR-013 envelope-slot trio (findings 2.3 and 2.4 together). Two separate bidirectional inline-cite pairs (AR-013 ↔ AR-053 and AR-052 ↔ AR-053) appear in adjacent §4.0 / §4.4 requirements; the pilot silently picked one direction for each pair without flagging the smell, and missed two intra-spec inline-cite-derived edges from AR-052. Per discipline §2.7 F13, bidirectional cites MUST be surfaced for resolution before load — not silently collapsed. The fact that ONE pilot section produces TWO of these cases suggests the pattern (slot-rule cites content-rule, content-rule cites slot-rule) is a recurring corpus shape that the discipline's F13 — which says only "surface to the discipline author" — should grow a default-resolution heuristic for. The remaining two BLOCKERs (`ar-029 → ar-016`, `ar-035 → ar-032`) are local pilot drafting errors, fixable by editing the two rows; they do not propagate. The MINORs cluster around a single discipline silence (term-use as inline-cite) that is worth resolving before drafting EM and the other behavioural specs, where the term-use frequency will be much higher than AR's.
