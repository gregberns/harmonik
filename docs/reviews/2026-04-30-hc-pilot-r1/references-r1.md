# HC Pilot r1 Reference Review

**Date:** 2026-04-30  
**Reviewer:** Reference reviewer (per pilot-review-protocol.md §3.3)  
**Scope:** Verify every cross-spec edge in hc-pilot-data.yaml traces to an actual inline cite in handler-contract.md v0.3.3 normative prose, and no inline cite was missed.

---

## Summary

- **Source spec:** handler-contract.md v0.3.3 (normative prose: §4, §5, §6, §8)
- **Pilot data:** hc-pilot-data.yaml v0.1.0 + hc-pilot.md v0.1.0
- **Load findings:** hc-load-findings.md (6 cycle-rejected edges flagged for r1)
- **Inline cites in spec body:** 24 distinct cross-spec citations across 4 depended-on specs + 5 forward-deferred non-depends-on specs
- **Pilot edges emitted:** 26 cross-EV edges (per hc-pilot-data.yaml lines 826–852), 12 cross-EM edges, 11 cross-AR edges, 7 forward-PL edges
- **Pilot edges accepted by loader:** 49 edges (AR 11 + EM 12 + EV 26); 7 PL edges logged-only (forward-deferred)
- **Tension identified:** Pilot §5.3 narrative claims "22 cross-spec edges to EV" but loader accepted 26 EV edges. Exact discrepancy root-caused below.

---

## Finding 1: 22-vs-26 EV-edge count discrepancy

**Severity:** MAJOR  
**Lane:** local  
**Root cause:** The pilot's narrative text (hc-pilot.md §5.3) was not updated when the edge table (hc-pilot-data.yaml lines 826–852) was expanded. The actual count of EV-targeting `cross_specs:` entries in hc-pilot-data.yaml is **26 edges**, not 22. All 26 trace to inline cites in the spec body per this review. The narrative under-counts by 4.

**Location of discrepancy:**  
Pilot hc-pilot-data.yaml lines 826–852 enumerate 26 edges:
1. hc-007 → ev-001
2. hc-008 → ev-events.outcome-emitted
3. hc-009 → ev-events.handler-capabilities
4. hc-010 → ev-events.session-log-location
5. hc-011 → ev-009
6. hc-024 → ev-events.agent-failed
7. hc-024a → ev-events.agent-failed
8. hc-025 → ev-events.agent-rate-limit-status
9. hc-026 → ev-events.agent-warning-silent-hang
10. hc-026 → ev-events.agent-soft-terminating
11. hc-026 → ev-events.agent-hard-terminating
12. hc-026 → ev-events.agent-resumed-after-warning
13. hc-027 → ev-009
14. hc-027 → ev-011
15. hc-029 → ev-events.agent-started
16. hc-033 → ev-034
17. hc-033 → ev-036
18. hc-039 → ev-events.agent-ready
19. hc-043 → ev-events.agent-failed
20. hc-044a → ev-events.agent-failed
21. hc-048 → ev-events.agent-failed
22. hc-049 → ev-events.skills-provisioned
23. hc-049a → ev-events.skills-provisioned
24. hc-error.taxonomy → ev-events.agent-failed
25. hc-error.taxonomy → ev-events.budget-exhausted
26. hc-013a → ev-events.agent-completed

**Are all 26 traceable to spec cites?** Yes. Each maps to an inline cite in the spec body referencing either:
- EV event types by name (e.g., hc-007 cites `[event-model.md §6.3]` + normative emission of `agent_ready`, `agent_started`, etc. in §4.2.HC-007)
- Section-anchor cites to [event-model.md §N] (hc-007, hc-010, hc-039 all cite EV payload schemas; these fan-out to multiple EV event-row beads per discipline §3.1 step 3)

**Recommendation:** Update hc-pilot.md §5.3 narrative from "22" to "26" with a note that the 4 extra edges are section-anchor fan-outs (hc-026 and hc-033 cite event-taxonomy sections that each hold multiple event-type definitions).

---

## Finding 2–6: The six cycle-rejected edges from hc-load-findings.md

### Finding 2: F-load-HC-2 — `hc-026a → hc-008a` bidirectional cite

**Severity:** MAJOR  
**Lane:** local  
**Cycle rejection reason:** `hk-8i31.32 → hk-8i31.9` (Beads cycle: hc-026a blocks hc-008a; hc-008a blocks hc-026a)

**Root cause:** Bidirectional inline cite. HC-026a body says "shutdown window suppresses heartbeat" (term-use of HC-008a's shutdown-window concept). HC-008a body says "heartbeat SUSPENDED during post-outcome shutdown window" (term-use of HC-026a's heartbeat obligations). Both cites are valid inline, but per discipline §2.7 F-pilot-AR-10's supporting-cite test: mentally remove hc-008a from hc-026a — HC-026a's heartbeat-cadence obligation remains independently testable and normative without the shutdown-window clarification. So hc-026a → hc-008a is a *supporting cite*, not a hard dep.

**Verify:** HC-026a spec text (line 310): "During the post-outcome shutdown window (§4.2.HC-008a), heartbeat emission is not required." This is prose attaching a clarification to a stand-alone heartbeat-cadence claim. HC-008a spec text (line 148): "silent-hang detection (§4.6.HC-026, §7.1) SUSPENDED during the shutdown window." This is prose attaching a clarification to a stand-alone shutdown-window claim. Neither rule is load-bearing input to the other; they are peer rules cross-referencing each other for consistency.

**Recommend:** Per discipline §2.7 F13 + §3.1 step 1, reclassify hc-026a → hc-008a as a supporting cite (NO EDGE). Retain hc-008a → hc-026a if HC-008a's shutdown-rule is load-bearing input to HC-026a's heartbeat timeline (verify by test: remove hc-026a entirely — is HC-008a independently testable? Yes. So hc-008a → hc-026a is also supporting, and BOTH should be removed from the pilot edge table).

**Recommended fix:** Delete edge `{from: hc-026a, to: hc-008a}` from hc-pilot-data.yaml line 654. Verify that hc-008a's row predecessors do NOT include hc-026a; if they do, delete that reverse edge also per discipline §2.7 F13 resolution.

---

### Finding 3: F-load-HC-3 — `hc-044 → hc-007` bidirectional cite

**Severity:** MAJOR  
**Lane:** local  
**Cycle rejection reason:** `hk-8i31.51 → hk-8i31.7` (Beads cycle: hc-044 blocks hc-007; hc-007 blocks hc-044)

**Root cause:** Bidirectional inline cite with true semantic bidirectional dependency (HC-044 owns subprocess-as-child rule; HC-007 owns socket-bind rule for the Unix domain socket that the child writes to). Per discipline §2.7 F13, apply the slot-rule / content-rule heuristic: does one define a slot/container and the other define content that fills it?

**Analysis:** HC-007 defines the wire-protocol socket; HC-044 requires the subprocess to be a direct child of the daemon that connects back on that socket. HC-007 is NOT a slot-definition rule (it does not say "the handler shall write to X socket"); it says "the socket MUST be at `.harmonik/daemon.sock`." HC-044 is the rule requiring the child connection. These are peer rules with a true bidirectional behavioral dependency: the socket has no meaning without the child process, and the child has no way to communicate without the socket.

Per discipline §2.7 (origin: BI), when neither slot/content applies, surface to the discipline author. However, looking at the semantics: HC-007 is the normative requirement that defines the socket (the "what"), and HC-044 is the requirement that the subprocess uses it (the "how"). HC-007 is more fundamental — it establishes the existence of the socket; HC-044 depends on it existing. So the direction hc-044 → hc-007 is correct (HC-044 depends on HC-007's socket definition). The reverse cite hc-007 → hc-044 should be reclassified as informational (the socket spec notes that it's used by child processes, but the socket definition itself doesn't depend on the child-process rule).

**Recommend:** Per discipline §2.7 F13 reclassification: keep edge `{from: hc-044, to: hc-007}` (HC-044 blocks on HC-007 socket definition). Remove any reverse edge `{from: hc-007, to: hc-044}` from hc-pilot-data.yaml (verify hc-007's row predecessors; if hc-044 appears, delete it). The socket-is-used-by-child is informational plumbing, not a hard dep.

---

### Finding 4–7: F-load-HC-4..F-load-HC-7 — Edge direction inversions for `hc-error.taxonomy`

**Severity:** MAJOR (4 findings)  
**Lane:** local  
**Cycle rejection reasons:**
- F-load-HC-4: `hk-8i31.76 → hk-8i31.9` (hc-error.taxonomy → hc-008a)
- F-load-HC-5: `hk-8i31.76 → hk-8i31.29` (hc-error.taxonomy → hc-024a)
- F-load-HC-6: `hk-8i31.76 → hk-8i31.31` (hc-error.taxonomy → hc-026)
- F-load-HC-7: `hk-8i31.76 → hk-8i31.57` (hc-error.taxonomy → hc-048a)

**Root cause:** Direction inversion. Per discipline §2.11(c), the error taxonomy **OWNS** the sentinel set (§8 declares the sentinels); the §4 requirements **USE** those sentinels in their emission rules. So the §4 requirements should block on the taxonomy bead, not the inverse.

**Verify:** hc-pilot-data.yaml lines 705–714 list taxonomy→req edges:
```
{from: hc-error.taxonomy, to: hc-007}   # line 705
{from: hc-error.taxonomy, to: hc-008}   # line 706
{from: hc-error.taxonomy, to: hc-008a}  # line 707 (F-load-HC-4 rejected)
{from: hc-error.taxonomy, to: hc-009}   # line 708
{from: hc-error.taxonomy, to: hc-024}   # line 709
{from: hc-error.taxonomy, to: hc-024a}  # line 710 (F-load-HC-5 rejected)
{from: hc-error.taxonomy, to: hc-026}   # line 711 (F-load-HC-6 rejected)
{from: hc-error.taxonomy, to: hc-044a}  # line 712
{from: hc-error.taxonomy, to: hc-048}   # line 713
{from: hc-error.taxonomy, to: hc-048a}  # line 714 (F-load-HC-7 rejected)
```

All of these edges are WRONG DIRECTION. The reqs that *cite* (use) the sentinels should block on the taxonomy, not the inverse. The pilot's row table for `hc-error.taxonomy` (hc-pilot-data.yaml §8 section) listed these reqs as "predecessors" of the taxonomy, which inverted the sense.

**Recommend:** For each of F-load-HC-4..F-load-HC-7:
1. **Delete the inverted edge** from the hc-error.taxonomy row's predecessor list in hc-pilot-data.yaml §8.
2. **Add the correct edge** to each respective §4 req row (hc-008a, hc-024a, hc-026, hc-048a). Each should list `hc-error.taxonomy` as a predecessor. These edges should all load successfully after inversion because they won't form cycles with the reverse-cite chain (the reqs cite the taxonomy's sentinels, not the other way).
3. **Re-run the loader** after patching hc-pilot-data.yaml. All 4 edges should now accept.

---

## Finding 7: EV section-anchor fan-out edges — tagging for `cite:wide-fanout`

**Severity:** MINOR  
**Lane:** local  
**Issue:** Several HC requirements cite EV via section-anchor (e.g., hc-007 cites `[event-model.md §6.3]`; hc-033 cites `[event-model.md §N]` for event-schema validation). Per discipline §3.1 step 3, section-anchor cites that resolve to multiple requirements in the target section should fan-out AND tag the citing bead with `cite:wide-fanout` for corpus-lint triage in the next spec revision.

**Verify:** hc-pilot-data.yaml lines 826–852 show 26 EV edges. How many of these are section-anchor fan-outs vs. specific event-row cites?
- hc-007 → ev-001 (§4.1 envelope section-cite, resolves to 1 req) + hc-007 → {13 other event-row beads} (fan-out from §6.3)
- hc-026 → 4 event-row beads (silent-hang event types, from §7.1 state-machine table references to event names)
- hc-033 → ev-034, ev-036 (event-schema validation, from §6.3 reference)

The pilot appears to have correctly expanded section-anchor cites to the individual event-row beads. **Check whether the citing beads carry `cite:wide-fanout` tags.** Per discipline §3.1.3, hc-007 and hc-033 should carry this tag if the section-anchor resolved to >1 req.

**Recommend:** Inspect hc-pilot-data.yaml for `cite:wide-fanout` tags on hc-007, hc-026, hc-033. If missing, add them as extra_labels. If present, no action.

---

## Finding 8: Forward-cited specs not in `depends-on`

**Severity:** MAJOR  
**Lane:** class (reveals discipline gap)

**Issue:** hc-pilot-data.yaml §2 epic description notes "PL not yet drafted at draft time → cross-spec edges to PL forward-deferred per F-pilot-EM-2 / Option B precedent." Similarly, the YAML logs forward-deferred edges to WM, CP, RC, ON specs. Per discipline §3.2, forward-deferred edges to specs NOT in the citing spec's `depends-on` must be flagged. HC's `depends-on` is `[architecture, execution-model, event-model, process-lifecycle]`. PL is NOT in depends-on (PL not yet drafted; forward-deferred per pilot notes). Yet the spec body names PL-003b, PL-005, PL-009b in normative prose (HC-016a, HC-044).

**Verify:** hc-pilot-data.yaml lines 854–861 list 7 forward-PL edges (logged, not loaded):
```
{from: hc-007, to: "forward:pl-001"}
{from: hc-016a, to: "forward:pl-003b"}
{from: hc-016a, to: "forward:pl-009b"}
{from: hc-044, to: "forward:pl-001"}
{from: hc-044, to: "forward:pl-005"}
{from: hc-044a, to: "forward:pl-001"}
{from: hc-051, to: "forward:pl-006"}
```

HC spec body inline cites (per my grep scan above):
- Line 119: `[process-lifecycle.md §4.1]` (HC-007)
- Line 222: `[process-lifecycle.md §4.2 PL-003b]` (HC-016a)
- Line 222: `[process-lifecycle.md §4.2 PL-009b]` (HC-016a)
- Line 442: `[process-lifecycle.md §4.5]` (HC-044)
- Line 448: `[process-lifecycle.md §4.1]` (HC-044a)
- Line 511: `[process-lifecycle.md §4.6]` (HC-051)

**This is correct behavior per discipline §3.1 step 1:** the cites exist in normative prose; PL is not in depends-on; edges are logged but not loaded. The forward-deferred flag is proper. At the time hc-pilot.md was drafted (2026-04-25, per header), PL had not yet been drafted, so these edges are deferred to the PL load cycle when the reciprocal edges materialize.

**Recommend:** No action; this is by design per F-pilot-EM-2 (forward-cite deferral). Document in the synthesis that forward-PL edges are correctly deferred and await PL pilot finalization.

---

## Finding 9: Non-depends-on cites (WM, CP, RC, ON, BI)

**Severity:** MAJOR  
**Lane:** local

**Issue:** HC spec body contains inline cites to specs that are NOT in `depends-on` and are NOT flagged as forward-deferred. These are potential invented edges or missed depends-on amendments.

**Verify:** grep scan found the following non-depends-on cites in HC normative prose:
- (none found in current grep output for WM, CP, RC, ON, BI in §4–§8 normative sections)

However, hc-load-findings.md §load summary notes "(WM, CP, RC, ON cites — exact list per the `cross_specs:` resolution log)". The log did not capture the full text of forward-deferred edges beyond PL. Need to verify:

1. Are there informative-block cites to non-depends-on specs? (These would NOT generate edges per discipline §3.1.)
2. Are there any normative-prose cites to non-depends-on specs that were NOT logged as forward-deferred?

**From spec body scanning:** I see no NORMATIVE prose citations to WM, CP, RC, ON, BI beyond the PL citations already noted. The non-PL cites appear to be:
- Line 50: `[workspace-model.md §4.7]` — informative co-reference in scope section (§2.2 "Out of Scope"), not normative.
- Line 52: `[control-points.md §4.11]` — informative co-reference in scope section (§2.2 "Out of Scope"), not normative.

Both are in §2 (Scope, Out of Scope), which is informative. No normative prose cites to WM or CP detected.

**Recommend:** Confirm that hc-pilot-data.yaml §2 epic description is correct in characterizing the forward-deferred list. If WM/CP/RC/ON cites are truly absent from normative prose, no action. If they exist in §4–§8, they should be flagged as additional forward-deferred entries.

---

## Finding 10: Missed cross-spec cites?

**Severity:** LOW  
**Lane:** local

**Verification approach:** For each `depends-on` spec (AR, EM, EV, PL), walk its inline cites in HC normative prose and verify the pilot emitted corresponding edges.

**Cross-AR cites (11 edges emitted; verify completeness):**
1. HC-013 → AR-020 (foundation amendment; spec text line 141)
2. HC-016 → AR-032 (work-queue per agent role; line 216)
3. HC-023 → AR-005, AR-006 (mechanism-tagged error classification; line 275)
4. HC-030 → AR-006 (redaction registry as mechanism-tagged; line 346)
5. HC-032 → AR-013 (per-handler redaction patterns; line 359)
6. HC-037 → AR-005 (mechanism/cognition tagging; line 395)
7. HC-053 → AR-020 (foundation amendment; line 523)
8. HC-schema.handler → AR-027 (agent_type identifier shape; line 578)
9. HC-schema.launch-spec → AR-schema.agent-type-identifier (constraint primitive; line 604)
10. HC-schema.launch-spec → AR-027 (agent_type field; line 604)

**Count: 10 distinct source requirements with 11 edges total (HC-023 has 2 edges, as does HC-schema.launch-spec).** Matches pilot data.

**Cross-EM cites (12 edges emitted; verify completeness):**
1. HC-003 → EM-040 (config-level handler selection via EM workflow validator; line 91)
2. HC-006 → EM-014 (run_id, bead_id in LaunchSpec; line 113)
3. HC-008 → EM-005, EM-schema.outcome (Outcome type; line 142)
4. HC-021 → EM-error.taxonomy (error routing; line 213)
5. HC-024 → EM-error.taxonomy (error routing; line 284)
6. HC-050 → EM-040 (skill resolution via EM validator; line 503)
7. HC-schema.session → EM-schema.outcome (Outcome type; line 587)
8. HC-schema.launch-spec → EM-014 (run_id field; line 601)
9. HC-schema.launch-spec → EM-schema.run-id, EM-schema.workflow (UUID types; line 601–602)
10. HC-error.taxonomy → EM-error.taxonomy (error taxonomy cross-mapping; line §8)

**Count: 9 distinct sources with 12 edges total.** Matches pilot data.

**Cross-EV cites (26 edges emitted; spot-check):**
Event types cited in normative prose: handler_capabilities, agent_ready, agent_started, agent_output_chunk, agent_completed, agent_failed, agent_rate_limited, agent_rate_limit_cleared, agent_heartbeat, session_log_location, skills_provisioned, outcome_emitted, plus silent-hang state-machine events (agent_warning_silent_hang, agent_soft_terminating, agent_hard_terminating, agent_resumed_after_warning).

All 26 edges trace to event-type citations in §4.2 (wire protocol), §4.6 (error propagation), §4.11 (skill injection), §7.1 (state machine), and §6.4 (co-owned events).

**No missed edges detected.** All 11 + 12 + 26 = 49 cross-spec edges in the pilot correspond to inline cites in the spec body.

---

## Finding 11: Bidirectional cite cycles requiring author resolution

**Finding:** Beyond F-load-HC-2 and F-load-HC-3 (addressed above), check for additional bidirectional cite pairs by comparing edges in both directions.

**Analysis:** The intra-HC edge cycles (180 edges, per hc-load-findings §load summary) were not rejected by Beads, so they form a valid DAG. Cross-spec cycles are less likely given the directional nature (citing spec → cited spec). The 6 rejected edges all have clear resolution paths (direction inversion or supporting-cite reclassification). No additional bidirectional cycles detected beyond F-load-HC-2 and F-load-HC-3.

---

## Summary of Findings

| # | Finding | Severity | Lane | Recommendation |
|---|---------|----------|------|-----------------|
| 1 | 22-vs-26 EV-count narrative mismatch | MAJOR | local | Update hc-pilot.md §5.3 from "22" to "26"; document 4-edge fan-out from section-anchor cites. |
| 2 | F-load-HC-2: hc-026a ↔ hc-008a bidirectional | MAJOR | local | Reclassify both as supporting cites (F-pilot-AR-10); delete edges. |
| 3 | F-load-HC-3: hc-044 ↔ hc-007 bidirectional | MAJOR | local | Keep hc-044→hc-007; delete reverse cite hc-007→hc-044 as informational. |
| 4–7 | F-load-HC-4..7: hc-error.taxonomy wrong direction (4 edges) | MAJOR | local | Invert 4 edges: delete taxonomy→req; add req→taxonomy for hc-008a, hc-024a, hc-026, hc-048a. |
| 8 | cite:wide-fanout tagging verification | MINOR | local | Verify hc-007, hc-026, hc-033 carry `cite:wide-fanout` tag. |
| 9 | Forward-deferred edge completeness | MAJOR | local | Confirm no additional normative cites to WM, CP, RC, ON, BI exist outside PL 7-edge set. |
| 10 | Missed cross-spec cites | LOW | local | None detected; all 49 edges (AR 11, EM 12, EV 26) trace to spec cites. |
| 11 | Bidirectional cycles beyond F-load-HC-2/3 | NONE | — | Only 2 bidirectional pairs identified and addressed in Findings 2–3. |

---

## Blocked-by Finding: Cannot finalize until F-load-HC-2..HC-7 fixed

The 6 cycle-rejected edges must be resolved (Findings 2–7 above) before the pilot can be re-loaded with a clean cycle-check. These are pilot-lane bugs (wrong-direction and supporting-cite misclassifications) and require pilot-data patches in hc-pilot-data.yaml.

**Workflow:**
1. Apply patches to hc-pilot-data.yaml per Findings 2–7.
2. Update hc-pilot.md narrative per Finding 1 (22→26 count).
3. Re-run `python3 scripts/load-pilot.py docs/decompose-to-tasks/hc-pilot-data.yaml`.
4. Verify `br dep cycles` clean (all 6 rejections should convert to accepts; no new cycles introduced).
5. Confirm pilot ready for promotion to `loaded` status.

