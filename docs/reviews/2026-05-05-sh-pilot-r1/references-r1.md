# SH Pilot r1 — Reference Reviewer Output

`reviewer: reference` · `date: 2026-05-05` · `pilot: docs/decompose-to-tasks/sh-pilot.md` v0.1.0 · `spec: specs/scenario-harness.md` v0.2.0 · `discipline: docs/decompose-to-tasks/discipline.md` v0.9 · `protocol: docs/decompose-to-tasks/pilot-review-protocol.md` v0.2 §3.3.

## 1. Method recap

Per pilot-review-protocol §3.3:

1. Walked SH source spec body top-to-bottom; for every cross-spec inline cite (`[<spec>.md §N]` or `<PREFIX>-NNN`) recorded citing context, target identifier, and prose location (normative §4 / §5 / §6 / §8 vs. informative §9.x / §10.x / §11 / §12 / §A / `> RATIONALE` / `> INFORMATIVE` / scope §2).
2. Walked pilot's §2 / §3 / §4 / §6 tables and `sh-pilot-data.yaml` `edges:` block; collected every cross-spec `blocks` edge.
3. Cross-checked: per-cite verified pilot has the corresponding edge (or has a documented no-edge analysis); per-edge verified the source spec has a corresponding inline cite in normative prose.
4. Verified zero-forward-deferred claim by resolving every cross-spec target mnemonic against the corresponding `mnem-maps/<spec>-mnem-map.csv`.
5. Verified depends-on coverage in both directions (every cited spec ⊆ depends-on; every depends-on entry actually used).
6. Verified `cite:wide-fanout` tagging on section-anchor cites.
7. Surfaced bidirectional-cite cycles for resolution.

---

## 2. Cite enumeration (source spec → pilot edge)

Cite-counting convention: I count distinct CITES, not distinct EDGES. A cite that resolves to N targets via wide-fanout produces 1 cite-count but N edges. Per the mission brief headline (`22 to HC, 13 to EM, 10 to WM, 9 to EV, 5 to AR, 3 to PL, 1 to ON`), those are all SH→other cite counts INCLUDING informative-prose cites — i.e., they include §9.x / §10.x / §2.x mentions. The supporting-cite-vs-hard-dep test (§3.1 step 5) reduces them to the 40 hard-dep edges actually emitted. A bare cite-count is therefore upper-bound for edge-count.

### 2.1 Cross-spec cites in normative-prose (§4 / §5 / §6.1 / §6.2 / §6.3 schema-evolution / §8 prose)

The list is exhaustive over the spec body lines I inspected:

**To architecture (AR):**
1. SH-INV-005 body (line 432): `[architecture.md §4.6]` — foundation amendment for parser change. → pilot edge `sh-inv-005 → ar-020`. ✓
2. §8 taxonomy intro (line 635): `[architecture.md §4.6]` — adding new failure class. → pilot edge `sh-error.taxonomy → ar-020`. ✓

**To handler-contract (HC):**
3. SH-008 body (line 134): `[handler-contract.md §4.1.HC-003]` (handler-config). → pilot edge `sh-008 → hc-003`. ✓
4. SH-008 body (line 134): `[handler-contract.md §5.HC-INV-002]` (twins indistinguishable). → pilot edge `sh-008 → hc-inv-002` (F10 invariant-as-target — explicit ID cite by SH-008 satisfies F10). ✓
5. SH-009 body (line 141): `[handler-contract.md §4.10.HC-042]` (repo-relative path). → pilot edge `sh-009 → hc-042`. ✓
6. SH-009 body (line 141): `[handler-contract.md §4.10.HC-043]` (commit-hash check). → pilot edge `sh-009 → hc-043`. ✓
7. SH-009 body (line 141): `[handler-contract.md §4.10.HC-045]` (launch-rule). → pilot edge `sh-009 → hc-045`. ✓
8. SH-011 body (line 155): `[handler-contract.md §4.8]` (HC-035..HC-040 parity surface — section anchor). → pilot edges `sh-011 → hc-035` and `sh-011 → hc-036`. **PARTIAL** — see Finding F-ref-SH-1 below.
9. SH-015 body (line 184): `[handler-contract.md §4.4.HC-018]` (cancellation bound). → pilot edge `sh-015 → hc-018`. ✓
10. SH-019 body (line 227): `[handler-contract.md §4.6.HC-024]` (`agent_failed`). → pilot edge `sh-019 → hc-024`. ✓
11. SH-026 body (line 288): `[handler-contract.md §4.4.HC-018]` (HC-018 again, cancellation bound). → pilot edge `sh-026 → hc-018`. ✓
12. SH-027 body (line 299): `[handler-contract.md §4.6.HC-026a]` (heartbeat carve-out). → pilot edge `sh-027 → hc-026a`. ✓
13. SH-INV-001 sensor-block exclusion (line 406): `HC-035 in-process fakes carve-out` — informative parenthetical excluding unit-test packages from grep. *Supporting cite per F-pilot-AR-10* — removing leaves SH-INV-001's grep enumeration testable. Pilot does NOT emit `sh-inv-001 → hc-035`. Defensible.
14. SH-INV-003 body (line 419): `[handler-contract.md §4.8.HC-035]` (twin contract). → pilot edge `sh-inv-003 → hc-035`. ✓
15. SH-INV-003 body (line 419): `HC-043 commit-hash check` (named explicitly inline). → pilot edge `sh-inv-003 → hc-043`. ✓
16. `sh-schema.fixture-setup` body (line 470, §6.1 RECORD): `[handler-contract.md §6.1 LaunchSpec]` — type-cite. *NOT in pilot.* **MAJOR finding F-ref-SH-2 below.**
17. `sh-schema.agent-override` body (line 462, §6.1): `subject to SH-009 + HC-043 hash check` — HC-043 named in schema body. Per discipline §3.1 step 5 term-use this should fire `sh-schema.agent-override → hc-043`. Pilot does NOT emit. *MINOR* — see F-ref-SH-3 below.

**To execution-model (EM):**
18. SH-021 body (line 243): `[execution-model.md §4.1 EM-005]` Outcome record. → pilot edge `sh-021 → em-005`. ✓
19. SH-021 body (line 243): `Outcome.status enum` (term-use). → pilot edge `sh-021 → em-schema.outcome-status`. ✓
20. `sh-schema.outcome-expectation` body (line 504, §6.1): `[execution-model.md §4.1 EM-005] Outcome.status` (type-cite). → pilot edges `sh-schema.outcome-expectation → em-005` and `sh-schema.outcome-expectation → em-schema.outcome-status`. ✓
21. `sh-schema.scenario-file` body (line 445, §6.1): `workflow_id per [execution-model.md §4.1]`. *NOT in pilot.* **MAJOR finding F-ref-SH-4 below.**

**To event-model (EV):**
22. SH-020 body (line 236): `[event-model.md §4.5.EV-021]` observational replay. → pilot edge `sh-020 → ev-021`. ✓
23. SH-021 body (line 243): `[event-model.md §8.1.8]` run-level terminal-emission. → pilot edges `sh-021 → ev-events.run-completed` and `sh-021 → ev-events.run-failed`. **MISTARGET-LIKE** — see F-ref-SH-5 below (§8.1.8 is `outcome_emitted`, NOT run_completed/run_failed).
24. SH-024 body (line 273): `[event-model.md §6.2]` torn-tail rule. Pilot self-flagged in F-pilot-SH-5 as supporting cite, no edge. *Defensible per F-pilot-AR-10.*
25. SH-024 body (line 273): `EV-011a bus_overflow` (named explicitly). → pilot edges `sh-024 → ev-011a` and `sh-024 → ev-events.bus-overflow`. ✓ (Two edges from one cite per the row-bead-vs-rule-bead split — `ev-011a` is the §4.x rule, `ev-events.bus-overflow` is the §8 row bead.)
26. `sh-schema.event-expectation` body (line 489, §6.1): `EventType -- one of the §8 types per [event-model.md §8]`. → pilot edge `sh-schema.event-expectation → ev-001`. *Defensible* but see F-ref-SH-6 below for target-pin discussion.
27. `sh-schema.outcome-expectation` body (line 504): `[event-model.md §8.1.8]` (also cites the same row as #23). Pilot does NOT emit edge from `sh-schema.outcome-expectation` to any EV row bead — it edges only to em-005 + em-schema.outcome-status. **MAJOR/MINOR-class** — see F-ref-SH-7 below.

**To workspace-model (WM):**
28. SH-012 body (line 163): `[workspace-model.md §4.1.WM-001]` workspace primitive. → pilot edge `sh-012 → wm-001`. ✓
29. SH-012 body (line 163): `[workspace-model.md §4.2 branching model]` (section anchor). Pilot's prose claims "primary load-bearing single-owner is WM-001 (workspace primitive); branching is referenced contextually but not load-bearing." **MISCLASSIFIED** — WM-001 is in WM §4.1 not §4.2; the §4.2 cite resolves to WM-006..WM-009. Pilot does NOT emit edge to any §4.2 req. See F-ref-SH-8 (MINOR/class).
30. SH-013 body (line 170): `[workspace-model.md §4.3.WM-010]` lease-by-run. → pilot edge `sh-013 → wm-010`. ✓
31. SH-015 body (line 184): `[workspace-model.md §4.3.WM-013b]` lease release. → pilot edge `sh-015 → wm-013b`. ✓
32. SH-015a body (line 191): `[workspace-model.md §4.5.WM-019]` squash-merge. → pilot edge `sh-015a → wm-019`. ✓

**To process-lifecycle (PL):**
33. SH-014 body (line 177): no inline PL cite — wraps via SH-016a transitively. (No new edge.)
34. SH-015 body (line 184): `[process-lifecycle.md §4.2.PL-003a]` daemon stop RPC. → pilot edge `sh-015 → pl-003a`. ✓
35. SH-016a body (line 205): `[process-lifecycle.md §4.2.PL-005]` startup sequence. → pilot edge `sh-016a → pl-005`. ✓
36. SH-016a body (line 205): `[process-lifecycle.md §4.1.PL-001]` one-daemon-per-project. → pilot edge `sh-016a → pl-001`. ✓
37. SH-016a body (line 205): `PL-005 step 0` (supporting reference; SH-INV-001 sub-step). Already covered by edge #35.
38. SH-017 body (line 214): `[process-lifecycle.md §4.2.PL-005]` (same entry-point as production). → pilot edge `sh-017 → pl-005`. ✓
39. SH-019 body (line 227): `[process-lifecycle.md §4.4 PL-010]` degraded state. → pilot edge `sh-019 → pl-010`. ✓
40. SH-026 body (line 288): `[process-lifecycle.md §4.2.PL-003a]` daemon stop RPC for cancel. → pilot edge `sh-026 → pl-003a`. ✓
41. SH-031 body (line 342): `PL-005 step 8` reconciliation reference. → pilot edge `sh-031 → pl-005`. ✓
42. SH-INV-002 body (line 412): `[process-lifecycle.md §4.1.PL-006]` orphan sweep. → pilot edge `sh-inv-002 → pl-006`. ✓
43. SH-INV-002 body (line 412): `PL-006a HARMONIK_RUN_ID marker` (named explicitly). → pilot edge `sh-inv-002 → pl-006a`. ✓
44. SH-027 body (line 297): `daemon_instance_id per PL-005 step 0` — supporting parenthetical. *No edge per F-pilot-AR-10 supporting-cite test* — removing leaves SH-027's determinism contract testable. Pilot does NOT emit `sh-027 → pl-005`. Defensible.

**To operator-nfr (ON):**
45. SH-015 body (line 184): `[operator-nfr.md §4.7]` drain-timeout. → pilot edge `sh-015 → on-029`. ✓
46. SH-026 body (line 288): `[operator-nfr.md §4.7 ON-029]` drain-timeout escalation. → pilot edge `sh-026 → on-029`. ✓

### 2.2 Cross-spec cites in informative-prose (§9.1 / §9.3 / §10.1 / §10.2 / §11 / §12 / §2 / §A / `> RATIONALE` / `> INFORMATIVE`)

All cites in §9.1 (depends-on summary, lines 713-739), §9.3 (co-references, 747-751), §10.1 (conformance scenarios, 761-765), §10.2 (test obligations, 773-784), §11 (open questions, 803-884), §12 (revision history, 891), and §2 scope (40-64) are **no-edge per discipline §3.1 no-edge list**. Spot-checks:

- Line 765 `[architecture.md §4.6]` (§10.1 amendment): no-edge ✓
- Line 761 `[handler-contract.md §4.9.HC-039]` (§10.1 conformance): no-edge ✓
- Line 762 `[execution-model.md §4.4.EM-016]` (§10.1): no-edge ✓
- Line 762 `[workspace-model.md §4.5.WM-019]` (§10.1): no-edge from this cite (same target IS edged from SH-015a's normative cite #32; that's fine).
- Line 763 `[handler-contract.md §4.6.HC-024]` (§10.1): no-edge from this cite (same target IS edged from SH-019's normative cite #10).
- Line 763 `[handler-contract.md §5 HC-INV-006]` (§10.1): no-edge ✓
- Line 774 `HC-043 hash-mismatch injection` (§10.2): no-edge ✓
- Line 775 `PL-001 satisfied` (§10.2): no-edge ✓
- Line 776 `PL-010 mid-scenario` (§10.2): no-edge ✓
- Line 803 `[process-lifecycle.md §4.1.PL-001]` (§11 OQ-SH-002): no-edge ✓
- Line 846 `[handler-contract.md §4.8.HC-038]` (§11 OQ-SH-008): no-edge ✓
- Line 867 `[process-lifecycle.md §4.2.PL-005]` (§11 OQ-SH-011): no-edge ✓
- Line 891 cites in revision history: no-edge ✓
- Line 666 `[execution-model.md §8.2 structural]` (§8.2 EM-analog parenthetical): per F-pilot-AR-10 supporting-cite test, "Conceptually adjacent to" is supporting; no-edge. Pilot treats correctly.
- Line 679 `[execution-model.md §8.2 structural]` (§8.4 EM-analog): same — no-edge.
- Line 693 `[execution-model.md §8.4 canceled]` (§8.6 EM-analog): same — no-edge.
- Line 669 `[workspace-model.md §4.1.WM-003]` (§8.3 fixture-setup-failed Detection): supporting cite ("workspace creation failed per WM-003" — WM-003 names the workspace-creation failure mode; removing leaves "workspace creation failed" still meaningful). Pilot treats correctly as no-edge.

### 2.3 Per-spec cite-count vs. edge-count summary

| Target spec | Mission-brief headline cite-count | Normative-prose hard-dep edges (this review's count) | Pilot edges | Match |
|---|---|---|---|---|
| HC | 22 | 13 (cites #3–#15, #17, #16) — but #16 missed → 12 actual | 13 | Pilot count matches its own claim; one MAJOR-class missed edge below |
| EM | 13 | 4 (cites #18–#20) + 1 (cite #21 missed) → 5 expected | 4 | One MAJOR missed edge |
| WM | 10 | 5 (cites #28, #30, #31, #32 + maybe #29 if §4.2 wide-fanout fires) | 4 | One MINOR/class concern |
| EV | 9 | 6 (cites #22, #23×2, #25×2, #26) + 1 (cite #27 from sh-schema.outcome-expectation §8.1.8 cite missed) → 7 expected | 6 | One MAJOR/MINOR finding |
| AR | 5 | 2 (cites #1, #2); rest are §10.1/§9.1/§11 informative | 2 | ✓ |
| PL | 9 | 9 (cites #34, #35, #36, #38, #39, #40, #41, #42, #43); the rest are §10.x/§11 informative | 9 | ✓ |
| ON | 2 | 2 (cites #45, #46) | 2 | ✓ |
| **Total** | **70 (informative-incl)** | **40 expected** | **40 emitted** | **3 missed + 1 mistargeted + minor concerns** |

Per the mission brief headline counts, the supporting-cite-vs-hard-dep test (§3.1 step 5) cuts the 70 raw cites down to ~40 hard-dep edges; pilot emits 40 (matches the YAML row count of 40). Pilot's own header claim of "38 cross-spec edges" is **arithmetic-wrong** (2+13+4+6+4+9+2 = 40, not 38; the pilot's pilot.md §5 closing line and §8 tally both repeat the 38 figure — presumably forgot to add the last 2 ON edges). This is a MINOR coverage-reviewer concern (count mismatch), not a reference-reviewer concern strictly, but I surface it for the synthesis pass.

---

## 3. Findings

### F-ref-SH-1 — `sh-011` cite to `[handler-contract.md §4.8]` is a section anchor; pilot pins to single-owner without `cite:wide-fanout` tag

**Severity:** MINOR.
**Lane:** `class` (the discipline rule is silent on whether single-owner pinning replaces `cite:wide-fanout` tagging when section-anchor cite resolves to a sub-set of section reqs).

**Detail.** SH-011 body (line 155) cites `[handler-contract.md §4.8]` which covers HC-035..HC-040 (6 reqs). Pilot emits edges to `hc-035` + `hc-036` only and explicitly notes "HC-037..HC-040 are clarifications consumed transitively via SH-INV-003 sensor". Per discipline §3.1 step 3: "if §N is a section header covering multiple requirements, emit edges to each requirement bead in that section AND tag the citing bead with `cite:wide-fanout` so corpus-lint can flag the source citation for tightening". The pilot applies F-pilot-AR-10 supporting-cite test on a per-target basis (HC-037..HC-040 are deemed supporting cites). This is a defensible reading but the discipline rule does not codify single-owner pinning vs. wide-fanout fan-out as alternatives. The pilot also does NOT tag `sh-011` with `cite:wide-fanout`, even though the cite IS a section anchor.

**Class lane reasoning.** Two of the eight remaining specs (no, all are loaded — but the pilot pattern recurs) have similarly used "load-bearing single-owner" pinning (HC pilot, EV pilot per pilot's own §5 prose). Either the discipline should codify that single-owner pinning replaces wide-fanout tagging, OR the pilot should fan out to all 6 HC §4.8 reqs and apply `cite:wide-fanout`. Currently the discipline language ("emit edges to each AND tag") does not match observed pilot practice. This is a discipline-clarification gap.

**Recommended action.** Discipline-patch lane: codify whether pilot may resolve section-anchor cites to load-bearing single-owners per F-pilot-AR-10, and if so whether `cite:wide-fanout` still fires. (My read: it should still fire on the citing bead, since the cite IS section-anchor and the corpus-lint signal is about the spec text itself, not the pilot's resolution.)

### F-ref-SH-2 — Missed edge: `sh-schema.fixture-setup → hc-schema.launch-spec`

**Severity:** MAJOR.
**Lane:** `local` (the discipline §3.1 step 4 type-cite rule is unambiguous; pilot just missed the cite).

**Detail.** `sh-schema.fixture-setup`'s description (mirroring SH §6.1 line 470) reads: "`skill_search_paths` (List<String>|None — additional skill search paths injected into LaunchSpec per [handler-contract.md §6.1 LaunchSpec])". This is a textbook §3.1 step 4 type-cite: `[handler-contract.md §6.1 LaunchSpec]` resolves to `hc-schema.launch-spec` (which exists in `hc-mnem-map.csv` row 75: `hc-schema.launch-spec,hk-8i31.74,Define LaunchSpec record (§6.1)`). Pilot does NOT emit this edge.

The `skill_search_paths` field is NOT independently testable from the LaunchSpec shape — its semantics depend on what the LaunchSpec consumes. This is a hard dep, not a supporting cite.

**Recommended action.** Pilot-patch lane (BLOCKER-equivalent for v0.1.1 if author agrees): add edge `sh-schema.fixture-setup → hc-schema.launch-spec` to `sh-pilot-data.yaml` cross-spec HC edges. Bumps HC count from 13 → 14, total edges 40 → 41.

### F-ref-SH-3 — Missed edge candidate: `sh-schema.agent-override → hc-043`

**Severity:** MINOR.
**Lane:** `local`.

**Detail.** `sh-schema.agent-override`'s description (mirroring SH §6.1 line 462) reads: "`binary` (String — absolute path or twin-search-path-relative name; subject to SH-009 + HC-043 hash check)". The text names HC-043 explicitly inline. Per discipline §3.1 step 1 + step 5 term-use, when a schema bead's body explicitly names a cross-spec req ID, an edge fires. Pilot does NOT emit `sh-schema.agent-override → hc-043`.

Counter-argument (defensible): The HC-043 cite is in a parenthetical describing the constraint that resolves at SH-009 application time; the resolution edge `sh-009 → hc-043` already covers the dependency. The schema bead itself is just defining the field shape; HC-043 is a runtime check, not a structural shape constraint. Per F-pilot-AR-10 supporting-cite test: removing the HC-043 mention from `sh-schema.agent-override`'s description leaves the field shape (`binary: String`) testable. This is a supporting cite.

I lean toward "supporting" here — the schema is just a typed alias; the HC-043 binding is in SH-009's normative body. MINOR finding because reasonable reviewers could go either way. Pilot's omission is defensible; not a blocker.

**Recommended action.** Pilot-patch lane (MINOR; author discretion): no patch needed if author agrees with supporting-cite reading; otherwise add edge.

### F-ref-SH-4 — Missed edge: `sh-schema.scenario-file → em-schema.workflow` (or `em-001`)

**Severity:** MAJOR.
**Lane:** `local`.

**Detail.** `sh-schema.scenario-file`'s description (mirroring SH §6.1 line 445) reads: "`workflow_id : String|None -- workflow_id per [execution-model.md §4.1]; MUTUALLY EXCLUSIVE with workflow_path`". Per discipline §3.1 step 4 type-cite, a `[other-spec.md §N <TypeName>]` cite generates an edge. Here the cite is `[execution-model.md §4.1]` which is a section anchor (no specific type name appended). EM §4.1 covers EM-001..EM-005a (6 reqs); the load-bearing single-owner is `em-schema.workflow` (which owns the `workflow_id` field) or `em-001` (which declares the Workflow record).

Pilot does NOT emit any edge from `sh-schema.scenario-file` to EM. The `workflow_id` field's semantics are NOT independently testable from EM's Workflow definition — `workflow_id` is a field on EM-001's Workflow record. This is a hard dep.

The pilot also does not log this as a forward-deferred or `cite:wide-fanout` candidate. It's a clean omission.

**Recommended action.** Pilot-patch lane (MAJOR for v0.1.1): add edge `sh-schema.scenario-file → em-schema.workflow`. (Pinning to `em-schema.workflow` over `em-001` per the type-cite rule §3.1 step 4 is preferred.) Bumps EM count from 4 → 5, total edges 40 → 41 (or 42 with F-ref-SH-2). Document analysis in pilot's §7.

### F-ref-SH-5 — Mistargeted edge: SH-021 + sh-schema.outcome-expectation cite `[event-model.md §8.1.8]` resolves to `outcome_emitted`, not `run_completed`/`run_failed`

**Severity:** MINOR.
**Lane:** `local` (the spec's own text is internally inconsistent; the pilot's resolution follows the surrounding prose's words rather than the literal section anchor).

**Detail.** SH-021 body (line 243) reads: "`exit_code` — the run-level terminal-emission event payload (`run_completed{status=...}` or `run_failed{status=...}` per [event-model.md §8.1.8]) carries a declared `outcome_status` value matching..."

But EV §8.1.8 is the `outcome_emitted` event row (per `ev-mnem-map.csv` row 5: `ev-events.outcome-emitted,hk-hqwn.59.8,Event row: outcome_emitted (§8.1.8)`). The `run_completed` and `run_failed` rows are EV §8.1.2 and §8.1.3 respectively (`ev-events.run-completed,hk-hqwn.59.2` and `ev-events.run-failed,hk-hqwn.59.3`).

The pilot edges `sh-021 → ev-events.run-completed` and `sh-021 → ev-events.run-failed`. This follows the spec text's NAMED events but contradicts the literal section anchor. Likely the SH spec author meant `[event-model.md §8.1.2-§8.1.3]` (or `§8.1`) but wrote `§8.1.8` (the row that COMMENTS on `outcome_status` flow rather than declaring the run-level events).

The same cite recurs at `sh-schema.outcome-expectation` body (line 504): "`outcome_status` ... matches the run-level terminal-emission event payload's outcome_status per [event-model.md §8.1.8]". Pilot emits NO EV edge from `sh-schema.outcome-expectation` (only EM edges). See F-ref-SH-7.

**Recommended action.** Pilot-patch lane (MINOR; surface as spec-edit task per F5 missing-inline-cite catcher): the SH spec body's `§8.1.8` is mistargeted in source; pilot's interpretation is reasonable but the cite-vs-target mismatch should be fixed at spec level. Pilot's edges to `ev-events.run-completed` + `ev-events.run-failed` are defensible interim resolution; the spec edit should clarify which event carries the `outcome_status` field for the `exit_code` predicate.

### F-ref-SH-6 — `sh-schema.event-expectation` cite `[event-model.md §8]` resolves to envelope `ev-001` rather than EV row beads or `ev-schema.event`

**Severity:** MINOR.
**Lane:** `local`.

**Detail.** `sh-schema.event-expectation` body cites "`type` (EventType — one of the §8 types per [event-model.md §8])". Pilot edges to `ev-001` (the envelope rule that owns the EventType field). Defensible: the envelope IS the term-defining bead for EventType. But §8 is a 78-row taxonomy table; the `type` field's discriminator is the 78 event names. A purer pin would be `ev-schema.event` (the Event envelope record, mnem-map row 53). `ev-001` is the EV req that says "every event MUST carry the common envelope fields"; it's the envelope-rule, not the type-discriminator owner.

Either pin (`ev-001` or `ev-schema.event`) is defensible as load-bearing single-owner. Wide-fanout to all 78 §8 row beads would be over-edging. Pilot's choice of `ev-001` is fine; just slightly off the type-cite §3.1 step 4 rule (which says edge to `ev-schema.<type-name>`). MINOR/local.

**Recommended action.** Pilot-patch lane (MINOR; author discretion). Optionally swap target from `ev-001` to `ev-schema.event` for cleaner type-cite alignment.

### F-ref-SH-7 — `sh-schema.outcome-expectation` cites `[event-model.md §8.1.8]` but emits no EV edge

**Severity:** MINOR (overlaps with F-ref-SH-5).
**Lane:** `local`.

**Detail.** `sh-schema.outcome-expectation`'s description (mirroring SH §6.1 line 504) cites BOTH `[event-model.md §8.1.8]` AND `[execution-model.md §4.1 EM-005]`. Pilot edges to EM (em-005 + em-schema.outcome-status) but emits NO EV edge from `sh-schema.outcome-expectation`. SH-021 (which cites the same EV §8.1.8) DOES emit EV row-bead edges. The asymmetry is suspicious: either both should emit EV edges, or neither.

Per the schema bead's term-use rule (§3.1 step 5): the OutcomeExpectation owns the `outcome_status` field; that field's semantics are owned by EV §8.1.8 (the row where it's normatively declared in the event payload) AND by EM §4.1 EM-005 (the Outcome record's status field). The semantic value lives in EM (per F-em-r1-MAJ-3 type-alias-resolves-to-single-MVH-variant); the carrier is EV's payload field. Pilot's choice to edge only to EM is defensible under the type-alias rule.

**Recommended action.** Pilot-patch lane (MINOR; author discretion; bundles with F-ref-SH-5 spec-edit task). No patch required if F-ref-SH-5's spec edit clarifies the carrier vs. semantic-owner split.

### F-ref-SH-8 — SH-012 cite `[workspace-model.md §4.2 branching model]` misclassified as covered by `wm-001`

**Severity:** MINOR.
**Lane:** `class` (discipline silence on cross-section-anchor pinning).

**Detail.** SH-012 body (line 163) cites BOTH `[workspace-model.md §4.1.WM-001]` AND `[workspace-model.md §4.2 branching model]`. Pilot emits `sh-012 → wm-001` and notes "primary load-bearing single-owner is WM-001 (workspace primitive); branching is referenced contextually but not load-bearing". But WM-001 is in WM §4.1, NOT in WM §4.2. The §4.2 cite is a separate section-anchor cite resolving to WM-006..WM-009 (4 reqs in WM §4.2 "Branch naming"). Pilot's "load-bearing single-owner" pinning conflates WM §4.1 and WM §4.2 into a single resolution, which is incorrect.

If the §4.2 cite is hard-dep, pilot should fan out to WM-006..WM-009 + tag `cite:wide-fanout` per discipline §3.1 step 3. If supporting per F-pilot-AR-10, pilot should explicitly note no-edge for the §4.2 cite (not conflate it with §4.1's WM-001 pin).

**Class lane reasoning.** Multiple section-anchor cites in one requirement body, where each anchor resolves to a different section, is a recurrent pattern (HC, PL, EV all have this shape). The discipline does not codify whether each anchor needs independent resolution or whether the pilot may "merge" anchors into a single load-bearing pin. F-ref-SH-1 (HC §4.8 single-owner pinning) is the same pattern.

**Recommended action.** Discipline-patch lane (bundles with F-ref-SH-1): codify per-anchor independent resolution. Pilot-patch lane: SH-012's "branching model" cite resolution should be explicitly logged as supporting-cite-no-edge OR fan out to WM-006..WM-009 with `cite:wide-fanout`.

---

## 4. Zero-forward-deferred-claim verification

Pilot v0.1.0 explicitly claims: "**Zero forward-deferred edges** — a first for the corpus" (§1, line 20; §10 revision history, line 276; sh-pilot-data.yaml header lines 14-17).

**Verification method.** For each of the 40 cross-spec edges, look up the target mnemonic in the corresponding `mnem-maps/<spec>-mnem-map.csv` (per loader-tooling.md). Verified targets:

| Target mnem | Spec | Resolved? |
|---|---|---|
| ar-020 | architecture | ✓ (hk-zs0.21) |
| hc-003 | handler-contract | ✓ (hk-8i31.3) |
| hc-018 | hc | ✓ (hk-8i31.22) |
| hc-024 | hc | ✓ (hk-8i31.28) |
| hc-026a | hc | ✓ (hk-8i31.32) |
| hc-035 | hc | ✓ (hk-8i31.42) |
| hc-036 | hc | ✓ (hk-8i31.43) |
| hc-042 | hc | ✓ (hk-8i31.49) |
| hc-043 | hc | ✓ (hk-8i31.50) |
| hc-045 | hc | ✓ (hk-8i31.53) |
| hc-inv-002 | hc | ✓ (hk-8i31.65) |
| em-005 | execution-model | ✓ (hk-b3f.5) |
| em-schema.outcome-status | em | ✓ (hk-b3f.83) |
| ev-001 | event-model | ✓ (hk-hqwn.1) |
| ev-011a | ev | ✓ (hk-hqwn.15) |
| ev-021 | ev | ✓ (hk-hqwn.30) |
| ev-events.run-completed | ev | ✓ (hk-hqwn.59.2) |
| ev-events.run-failed | ev | ✓ (hk-hqwn.59.3) |
| ev-events.bus-overflow | ev | ✓ (hk-hqwn.59.77) |
| wm-001 | workspace-model | ✓ (hk-8mwo.3) |
| wm-010 | wm | ✓ (hk-8mwo.15) |
| wm-013b | wm | ✓ (hk-8mwo.20) |
| wm-019 | wm | ✓ (hk-8mwo.29) |
| pl-001 | process-lifecycle | ✓ (hk-8mup.2) |
| pl-003a | pl | ✓ (hk-8mup.7) |
| pl-005 | pl | ✓ (hk-8mup.10) |
| pl-006 | pl | ✓ (hk-8mup.11) |
| pl-006a | pl | ✓ (hk-8mup.12) |
| pl-010 | pl | ✓ (hk-8mup.19) |
| on-029 | operator-nfr | ✓ (hk-sx9r.43) |

**30/30 distinct cross-spec target mnemonics resolve.** All 40 edges resolve to in-corpus targets. **Zero-forward-deferred claim VERIFIED.** ✓

(Note: the missed-edge findings F-ref-SH-2 + F-ref-SH-4 would also resolve to in-corpus targets — `hc-schema.launch-spec` row 75 in hc-mnem-map.csv, `em-schema.workflow` row in em-mnem-map.csv. So even patching the missed edges would preserve the zero-forward-deferred property.)

---

## 5. Depends-on coverage verification

Front-matter `depends-on`: `[architecture, handler-contract, event-model, workspace-model, execution-model, operator-nfr, process-lifecycle]` (7 entries).

**Direction 1 — every cross-spec edge target's spec ⊆ depends-on:**
- AR (2 edges) ⊆ depends-on ✓
- HC (13 edges) ⊆ depends-on ✓
- EM (4 edges) ⊆ depends-on ✓
- EV (6 edges) ⊆ depends-on ✓
- WM (4 edges) ⊆ depends-on ✓
- PL (9 edges) ⊆ depends-on ✓
- ON (2 edges) ⊆ depends-on ✓

No edge target lives outside depends-on. **No depends-on violations.** ✓

**Direction 2 — every depends-on entry is actually used by at least one cross-spec edge:**
- architecture: 2 edges ✓
- handler-contract: 13 edges ✓
- event-model: 6 edges ✓
- workspace-model: 4 edges ✓
- execution-model: 4 edges ✓
- operator-nfr: 2 edges ✓
- process-lifecycle: 9 edges ✓

All 7 depends-on entries used. **No vestigial depends-on entries.** ✓

(F-ref-SH-2 and F-ref-SH-4 missed edges would not change either direction; they would just add 1 edge to HC and 1 to EM, both already in depends-on.)

---

## 6. Bidirectional-cite cycle check

SH is the 11th and most-downstream spec in the corpus; per the pilot's §1 framing it sits as a leaf component in the corpus DAG. None of the 7 upstream specs (AR, HC, EM, EV, WM, PL, ON) cite SH back. Verified spot-check: searched each of the 7 spec files for `scenario-harness.md` or `SH-` references — none found in normative prose. (`docs/subsystems/scenario-harness.md` is the seed, superseded by the spec; not a spec file.)

**No bidirectional cite cycles surface.** ✓

The `br dep cycles` check at load is the structural gate; cycle-freeness on the cross-spec front is expected to hold per the DAG-leaf property.

---

## 7. `cite:wide-fanout` tag policy verification

Per discipline §3.1 step 3 + §3.1.3: section-anchor cites that resolve to multiple req IDs MUST tag the citing bead `cite:wide-fanout`.

SH section-anchor cites in normative prose:
1. SH-011 cite `[handler-contract.md §4.8]` (covers HC-035..HC-040, 6 reqs). Pilot pins to single-owners HC-035 + HC-036; does NOT tag `cite:wide-fanout`. **F-ref-SH-1.**
2. SH-012 cite `[workspace-model.md §4.2 branching model]` (covers WM-006..WM-009). Pilot does not edge OR tag. **F-ref-SH-8.**
3. `sh-schema.fixture-setup` cite `[handler-contract.md §6.1 LaunchSpec]`. Type-cite — should resolve to `hc-schema.launch-spec`, NOT wide-fanout. **F-ref-SH-2.**
4. `sh-schema.scenario-file` cite `[execution-model.md §4.1]` (covers EM-001..EM-005a). Type-cite to `workflow_id` field — should resolve to `em-schema.workflow`. **F-ref-SH-4.**
5. SH-019 cite `[execution-model.md §8]` — pilot self-flagged in §5 prose as "supporting only, no edge"; defensible per F-pilot-AR-10. ✓
6. `sh-schema.event-expectation` cite `[event-model.md §8]` — pilot pins to `ev-001` (envelope owner). Defensible per single-owner pin; could also be `ev-schema.event`. **F-ref-SH-6** (MINOR).
7. SH-020 cite `[event-model.md §4.5.EV-021]` — pilot edges directly to specific req `ev-021`. Not section-anchor; no wide-fanout needed. ✓
8. SH-024 cite `[event-model.md §6.2]` — pilot self-flagged as supporting cite (F-pilot-SH-5); no edge, no tag. Defensible. ✓

Pilot's §5 prose explicitly states "The `cite:wide-fanout` tag is NOT applied to any SH bead". Per the discipline's letter, the §4.8, §4.2, and §8 cites SHOULD have produced `cite:wide-fanout` tags on their citing beads (sh-011, sh-012, sh-schema.event-expectation). Pilot's choice not to tag follows F-pilot-AR-10 single-owner-pin doctrine, but the discipline rule does not codify that this exempts the wide-fanout tag.

**This is a CLASS finding** — see F-ref-SH-1's class-lane discussion. The discipline silence on whether single-owner pinning relieves the `cite:wide-fanout` obligation is the gap.

---

## 8. Summary table — findings by severity and lane

| Finding | Severity | Lane | Brief |
|---|---|---|---|
| F-ref-SH-1 | MINOR | class | `sh-011` HC §4.8 section-anchor pinned single-owner without `cite:wide-fanout` tag |
| F-ref-SH-2 | MAJOR | local | Missed edge `sh-schema.fixture-setup → hc-schema.launch-spec` (§6.1 LaunchSpec type-cite) |
| F-ref-SH-3 | MINOR | local | Possible missed edge `sh-schema.agent-override → hc-043` (HC-043 named in schema body) |
| F-ref-SH-4 | MAJOR | local | Missed edge `sh-schema.scenario-file → em-schema.workflow` (`workflow_id per [execution-model.md §4.1]`) |
| F-ref-SH-5 | MINOR | local | SH-021 cite `[event-model.md §8.1.8]` mistargets `outcome_emitted` row (spec inconsistency; pilot's prose-following resolution is defensible) |
| F-ref-SH-6 | MINOR | local | `sh-schema.event-expectation` pins to `ev-001` over `ev-schema.event` for `[event-model.md §8]` cite |
| F-ref-SH-7 | MINOR | local | `sh-schema.outcome-expectation` cites `[event-model.md §8.1.8]` but emits no EV edge (asymmetric with sh-021) |
| F-ref-SH-8 | MINOR | class | `sh-012` `[workspace-model.md §4.2 branching model]` cite misclassified as covered by `wm-001` (cross-section anchor handling) |

**Counts:**
- BLOCKER: 0
- MAJOR: 2 (F-ref-SH-2, F-ref-SH-4) — both `local`
- MINOR: 6 — 4 `local` (F-ref-SH-3, F-ref-SH-5, F-ref-SH-6, F-ref-SH-7), 2 `class` (F-ref-SH-1, F-ref-SH-8)

**Overall cite-vs-edge match rate.**
- Hard-dep-edge expected count (this review's enumeration): 42 (40 emitted + F-ref-SH-2 missed + F-ref-SH-4 missed).
- Pilot emitted: 40.
- Match rate: 40 / 42 = **95.2%**.
- If supporting-cite reading applied to F-ref-SH-3 (no edge needed), and F-ref-SH-5/F-ref-SH-7 are spec-text errors not pilot errors: pure local-pilot-error count is **2 missed edges** (F-ref-SH-2 + F-ref-SH-4).

---

## 9. Verdict

**On the zero-forward-deferred claim:** VERIFIED. All 40 emitted cross-spec edges resolve to in-corpus targets; the additional missed edges F-ref-SH-2 and F-ref-SH-4 also resolve to in-corpus targets. SH is genuinely the first pilot in the corpus with zero forward-deferred edges. ✓

**On the pilot's structural reference health:** STRONG. Cite-vs-edge match rate 95.2% is among the highest of any pilot reviewed; depends-on coverage is clean in both directions; no bidirectional cite cycles; cycle-freeness expected at load.

**On the pilot's interpretive judgment:** MIXED. The pilot's "load-bearing single-owner" pinning doctrine (applied to HC §4.8, WM §4.2, EV §8) is defensible per F-pilot-AR-10 supporting-cite test on a per-target basis but not codified in the discipline. The two missed edges (F-ref-SH-2 LaunchSpec, F-ref-SH-4 workflow_id) are clean §3.1 step 4 type-cite oversights — both schema beads use type-cite shape (`per [other-spec.md §N <Type>]`) that the discipline rule unambiguously routes to `<other-spec>-schema.<type-name>`.

**On the discipline:** Two MINOR class findings (F-ref-SH-1 on single-owner pinning + `cite:wide-fanout`; F-ref-SH-8 on multi-anchor resolution) suggest the discipline could benefit from explicitly codifying single-owner pinning as a section-anchor resolution mechanism, with explicit `cite:wide-fanout` tag obligations preserved.

**Recommendation to synthesis pass:**
- Apply F-ref-SH-2 + F-ref-SH-4 as pilot-patch lane MAJOR findings; bump pilot to v0.1.1; pilot total edges 40 → 42 (HC: 13 → 14, EM: 4 → 5).
- Fix the arithmetic-tally bug (38 → 40 → 42 after patches) in pilot.md §5 closing line + §8 tally.
- Surface F-ref-SH-1 + F-ref-SH-8 as discipline-patch class findings to be batched with the next discipline release; document single-owner pinning vs. wide-fanout-tagging for section-anchor cites.
- F-ref-SH-3, F-ref-SH-5, F-ref-SH-6, F-ref-SH-7 are MINOR/local; author discretion.
- F-ref-SH-5 spec-edit (SH-021 / `sh-schema.outcome-expectation` cite `§8.1.8` → should be `§8.1.2/§8.1.3` or `§8.1`) is a downstream spec-fix task, not a pilot-fix.
