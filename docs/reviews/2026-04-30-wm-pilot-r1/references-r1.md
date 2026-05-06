# WM Pilot r1 — Reference Reviewer Findings

`pilot-version: 0.1.0` · spec: `specs/workspace-model.md` v0.4.2 (1244 lines) · pilot data: `docs/decompose-to-tasks/wm-pilot-data.yaml` (1250 lines) · narrative: `docs/decompose-to-tasks/wm-pilot.md` (274 lines) · discipline: v0.9 · review-protocol: §3.3.

Method per §3.3: walked WM body §4 / §5 / §6 / §7 cross-spec inline cites top-to-bottom; walked yaml `edges:` block; cross-checked. Validated forward-deferred Option B placeholders against WM `depends-on: [architecture, execution-model, handler-contract, control-points, reconciliation, operator-nfr, process-lifecycle, beads-integration]` (8 entries — corpus high-water mark). Spot-checked F-pilot-EV-3 informational handling. Spot-checked direction-sanity per §2.11(c.2).

## Headline

**CLEAN with caveats.** WM v0.4.2 was authored entirely under v0.9 (post §2.11(c.2) anti-pattern note); the inverted-edge class does not manifest. No BLOCKER findings. No invented edges, no bidirectional cycles, no direction inversions, no depends-on violations. Several MINOR findings about edge omissions where pilot author exercised supporting-cite reclassification per F-pilot-WM-5 — judgment calls reviewable but not load-blocking.

## Severity tally

- **BLOCKER:** 0
- **MAJOR:** 0
- **MINOR:** 5
- **NOTE:** 2

## Critical questions resolved

### Q1 — Are PL forward edges in fact forward-deferred (depends-on violation hypothesis)?

**Resolved: forward edges are valid Option B placeholders. NO depends-on violation.** WM front-matter line 22 lists `process-lifecycle` in `depends-on`. The reviewer prompt's hypothesis "PL is NOT in WM's depends-on" is incorrect — PL IS in depends-on. The 3 `forward:pl-NNN` edges (`wm-013c`, `wm-033`, `wm-env-002`) are correctly forward-deferred per F-pilot-EM-2 / Option B precedent — target IS in depends-on, target's pilot just hasn't loaded. Same posture as RC and ON forward-deferred edges.

### Q2 — Are BI forward edges in fact forward-deferred (depends-on violation hypothesis)?

**Resolved: forward edges are valid Option B placeholders. NO depends-on violation.** WM front-matter line 23 lists `beads-integration` in `depends-on`. The reviewer prompt's hypothesis "BI is NOT in WM's depends-on" is incorrect — BI IS in depends-on. The 4 `forward:bi-NNN` edges (`wm-001`, `wm-006`, `wm-028`, `wm-inv-002`) are correctly forward-deferred.

### Q3 — Direction-sanity on §2.11(c.2) consumer→`wm-error.taxonomy` edges

**Resolved: all emitted edges run consumer → owner. No inversions.** WM is the first pilot drafted entirely under v0.9; pre-load cycle posture is clean per F-pilot-WM-4. 11 emitted edges all run `<wm-req> → wm-error.taxonomy` (consumer → owner). See MINOR-1 below for a count-discrepancy note.

### Q4 — F-pilot-EV-3 informational handling spot-check

**Spot-checked 5 EV cites in WM body; all correctly omitted from edges.** Sites verified:
- **WM-015** cites `[event-model.md §8.5]`, `[event-model.md §8.5.1..5]` — informational ✓ (no edge)
- **WM-021** cites `[event-model.md §8.5.3]` + `[event-model.md §8.9(h)]` — informational ✓
- **WM-023** cites `[event-model.md §8.5.6]` — informational ✓
- **WM-038a** cites `[event-model.md §8.5.5]` — informational ✓
- **WM-040** cites `[event-model.md §8.7]` — informational ✓

39 total EV cites in WM body; yaml emits zero EV edges (`grep -c "ev-" wm-pilot-data.yaml edges section = 0`). All correctly handled as F-pilot-EV-3 informational findings per the v0.4.0 NOTE. ✓

## Findings

### MINOR-1 — Consumer→taxonomy edge count: pilot narrative claims 13, yaml emits 11 [local lane]

The yaml top-comment (line 67-83) and pilot.md §6 narrative both list 13 consumer beads with cited error sentinels: `wm-002`, `wm-003`, `wm-003a`, `wm-006a`, `wm-013a`, `wm-013c`, `wm-013e`, `wm-016`, `wm-023`, `wm-026`, `wm-034`, `wm-037a`, `wm-env-002`. The yaml `edges:` block emits only 11 — `wm-002 → wm-error.taxonomy` and `wm-003 → wm-error.taxonomy` are absent.

**Pilot-author justification is implicit.** WM-002's body itself does not name any error sentinel inline; the `RunIdReuseForbidden` / `WorkspaceAlreadyExists` references appear in §7.2 pseudocode, not WM-002's §4 body. Per discipline §3.1 (normative-prose-only inline-cite walk), the WM-002 sentinel mention is in pseudocode and correctly omitted as an edge. Same for WM-003 (`WorktreeCreationFailed` is the §8 row name, not cited in WM-003's body).

**Issue.** The yaml top-comment description and pilot.md §6 narrative are inconsistent with the actual edge emission — they describe a 13-bead conceptual consumer set but only 11 are §3.1-walkable cites. **Suggestion: amend the comment / narrative to "11 consumer→taxonomy edges fire" and clarify the WM-002 / WM-003 conceptual-consumer status as documented-but-not-edge.**

Severity: **MINOR** (internal narrative inconsistency; does not affect load gate). Lane: `local` (mechanical fix to comment/narrative; no discipline patch needed).

### MINOR-2 — `wm-024 → hc-schema.handler` (HC §4.1) appears missed [local lane]

WM-024 body line 461: "agent's reasoning during resolution is delegated to the implementer's handler per **[handler-contract.md §4.1]**". HC §4.1 is the Handler interface (mapped to `hc-schema.handler` per HC mnem-map). Yaml emits `wm-024 → hc-schema.launch-spec` (LaunchSpec for re-dispatch) and `wm-024 → hc-046` (skill provisioning) but no edge to the Handler interface itself.

**Pilot-author justification is unstated.** Possible justifications: (a) WM-018a already emits `→ hc-schema.handler` for the agentic merge-node variant, so WM-024's HC §4.1 cite is redundant; (b) the cite is supporting per F-pilot-WM-5 — removing HC §4.1 leaves WM-024's mechanical delegation rule testable. Both are defensible.

**Issue.** Inconsistency vs WM-018a's treatment, where HC §4.1 yields `→ hc-schema.handler`. Suggest either emit the edge for parity, or document the F-pilot-WM-5 supporting-cite reclassification explicitly in F-pilot-WM-5 narrative for WM-024.

Severity: **MINOR** (omission is judgment-call; not load-blocking). Lane: `local`.

### MINOR-3 — `wm-031 → em-schema.run` (or equivalent) for EM §7.1 cite appears missed [local lane]

WM-031 body line 525 cites `[execution-model.md §7.1]` for "run reached a terminal failure state (`failed` or `canceled`)". §7.1 is EM's run state machine — owned anchor in EM. Yaml emits `wm-031 → wm-013b` and `wm-031 → forward:on-NNN` but no EM edge.

**EM mnem-map check:** EM has no top-level §7.1 mnem; `em-schema.run` (Run record §6.1) is the closest mapped anchor. Pilot may have judged this as a section-anchor cite without specific-row resolvability per §3.1.3 OR as a supporting cite per F-pilot-WM-5 (the run-failure terminal state set is defined in EM but WM-031 's persistence rule is independently testable given the state-set as input).

**Issue.** Same omission appears in §7.1 transition table line 813 ("run state = `failed` or `canceled` per [execution-model.md §7.1]"), which is referenced from multiple bead bodies. Suggest documenting the §3.1.3 wide-fanout treatment OR the F-pilot-WM-5 supporting-cite reclassification.

Severity: **MINOR**. Lane: `local`.

### MINOR-4 — `wm-036 → forward:on-NNN` for `escalate-to-human` operator-nfr cite appears missed [local lane]

WM-036 verdict-disposition table (line 579) row `escalate-to-human`: "operator escalation per **[operator-nfr.md §4.3]**". This is normative-table prose; ON §4.3 is in WM's depends-on. Yaml emits `wm-036 → forward:rc-NNN` for the verdict enum but no `forward:on-NNN` edge for the operator-escalation routing.

**Pilot-author justification is implicit and defensible.** Per F-pilot-WM-5 reclassification, the cite "operator escalation per [operator-nfr.md §4.3]" attached to the disposition "no re-run attempted; workspace remains in its current terminal state" is supporting — removing ON §4.3 leaves WM-036's no-re-run disposition independently testable. Same pattern as WM-031's ON §4.9 supporting-cite reclassification.

**Issue.** F-pilot-WM-5 narrative names WM-002 / WM-024 / WM-031 supporting-cite reclassifications but does NOT name WM-036. Suggest either emit the edge for completeness, or extend F-pilot-WM-5 to cover WM-036.

Severity: **MINOR**. Lane: `local`.

### MINOR-5 — `wm-schema.workspace → forward:bi-NNN` for `BeadID` type alias appears missed [local lane]

`wm-schema.workspace` carries `bead_id : BeadID | None`. §6.1 type-alias paragraph (line 749): "BeadID is the bead identifier type per **[beads-integration.md §6.1]**". Yaml emits `wm-schema.workspace → em-schema.commit-range` (CommitSHA) and `wm-schema.workspace → hc-schema.launch-spec` (HandlerRef) but no BI edge for BeadID.

**Inconsistency.** The CommitSHA / HandlerRef edges fire as type-alias citations to owning specs; BeadID identical pattern; UUID (also in same paragraph) cites EV but EV is intentionally excluded. BI is in `depends-on`, so a `forward:bi-NNN` edge would be the consistent treatment.

**Issue.** Same paragraph, same shape, three of four type-alias citations emit edges and one doesn't. The narrative does not mention an explicit reclassification for BeadID. Suggest emit `wm-schema.workspace → forward:bi-NNN` for consistency, OR document the §3.1 step-5 type-alias treatment uniformly (e.g., "schema-bead type aliases emit edges to owning-spec schemas; UUID/EV-002 excluded per consume-produce posture").

Severity: **MINOR**. Lane: `local`.

### NOTE-1 — Section-anchor cites without specific-row resolution

Two cites use bare section-anchor form without specific-row resolution per §3.1.3:
- WM-ENV-002 cites `[process-lifecycle.md §4.x]` (line 150) — explicit "x" in cite signals the row is unresolved at WM author-time. Yaml correctly emits `wm-env-002 → forward:pl-NNN` placeholder.
- §7.1 transition table line 813 cites `[execution-model.md §7.1]` — wide-anchor cite covering whole run state machine. Yaml does not emit an edge (see MINOR-3).

Both consistent with §3.1.3 wide-fanout posture (cite stays in body; downstream pilot resolves the specific row). Documented for completeness.

### NOTE-2 — `[reconciliation/spec.md §8]` (no §-specifier) at WM-003a

WM-003a cites `[reconciliation/spec.md §8]` (whole §8 — not a specific Cat). RC's §8 has 12 numbered subsections. Yaml emits no edge from WM-003a to RC (RC is forward-deferred), and the body says "routed to reconciliation Cat 3 per [reconciliation/spec.md §8]" — Cat 3 narrows to RC §8.4 (Cat 3) per RC's own structure. Pilot does not emit forward:rc-NNN from WM-003a — possibly because the cite reads as a wide-section cite per §3.1.3. Documented for the RC pilot to resolve as a Cat 3 specific-row backfill.

## Forward-deferred edge validation

| Target spec | Yaml top-comment claim | Edges in yaml `edges:` block | Reviewer count | Match? |
|---|---|---|---|---|
| `forward:rc-NNN` | 12 | 12 | 12 | ✓ |
| `forward:on-NNN` | 8 | 8 | 8 | ✓ |
| `forward:pl-NNN` | 3 | 3 | 3 | ✓ |
| `forward:bi-NNN` | 4 | 4 | 4 | ✓ |
| **Total** | **27** | **27** | **27** | **✓** |

All forward-deferred edges target specs in WM's `depends-on`. **No depends-on violations.** Pilot's pl=3 and bi=4 counts are correct, and both specs are confirmed in `depends-on` (the reviewer-prompt hypothesis to the contrary was based on a misreading; verified against front-matter lines 22–23).

The yaml top-comment shows `rc: 9` (line 90) but actual count is 12. Sub-MINOR drift between top-comment claim and actual. (The pilot.md §3.2 narrative correctly says 12; the yaml top-comment says 9 — internal inconsistency.)

## Active cross-spec edges (resolved against mnem-maps)

| Target | Edges | Citing beads | Notes |
|---|---|---|---|
| `ar-*` | 5 | `wm-env-001` (×2), `wm-022`, `wm-026`, `wm-schema.session-metadata-sidecar` | AR-024 + AR-052/053 envelope. AR-INV-007 correctly omitted per F-pilot-AR-r2-2 invariant-as-target exemption. ✓ |
| `em-*` | 13 | 11 distinct bead-pairs | EM-014/017/023/034/035/044 + em-schema.commit-range + em-inv-001 sibling. ✓ |
| `hc-*` | 11 | 8 distinct bead-pairs | hc-007 carries HC-044a (closest anchor — see in-yaml note); hc-010 / hc-046 / hc-schema.{handler,launch-spec,session-id}. ✓ |
| `cp-*` | 2 | `wm-008`, `wm-024` | CP-037 active for config-precedence; F-pilot-WM-5 correctly reclassifies WM-002's CP-037 as supporting. ✓ |
| **Total** | **31** | | Pilot.md narrative says ~29; yaml has 31. Drift within ±2 acceptable. |

## Bidirectional cycles

Walked all 233 edges for `(A,B) ∈ edges ∧ (B,A) ∈ edges`. Result: **none**. The F13 slot-rule one-way notes (lines 939–944, 962–969, 992–998, 1029–1033) successfully prevented inversions on the four candidate slot/content pairs (WM-006/006a, WM-013a/016, WM-016/027, WM-016/026).

## Direction inversions

None detected. The 11 `<req> → wm-error.taxonomy` edges are the only edges to a vocabulary-owner bead in this pilot, and all run in the consumer → owner direction per §2.11(c.2). The §2.5 sensor↔impl one-way rule is honored (sensor beads `wm-inv-NNN` block on impl beads, never reverse).

## Conclusion

**CLEAN with 5 MINOR findings + 2 NOTEs.** All MINOR findings are local-lane judgment calls about edge omissions where the pilot author either explicitly applied F-pilot-WM-5 supporting-cite reclassification or implicitly judged a cite as section-anchor / wide-fanout. None of them block load. Recommend the pilot author address MINOR-1 (count discrepancy) and MINOR-5 (BeadID inconsistency) for narrative-coherence; MINOR-2/3/4 are reviewable but defensible and could be left to F-pilot-WM-5 narrative extension.

No depends-on violations, no invented edges, no bidirectional cycles, no direction inversions, no missed BLOCKER edges. PL and BI forward-deferred placeholders are correct (target IS in depends-on; reviewer-prompt hypothesis to the contrary is mistaken).

---

**File path:** `/Users/gb/github/harmonik/docs/reviews/2026-04-30-wm-pilot-r1/references-r1.md`
**Reviewer:** reference reviewer (r1 pass)
**Date:** 2026-04-30
