# CP pilot r1 — Reference reviewer report

`reviewer: references` · `pilot: cp-pilot.md v0.1.0` · `spec: control-points.md v0.3.2` · `discipline: v0.9` · `date: 2026-04-30`

## Method run

Walked `specs/control-points.md` body top to bottom, classifying every cross-spec inline cite (`[<other>.md §N]`, `<OTHER>-NNN`) as normative-prose vs informative-block per discipline §2.7. Walked `cp-pilot-data.yaml`'s edges and forward-deferred placeholders. Cross-checked normative-prose cites against pilot edges; cross-checked pilot edges against inline cites; verified target-spec membership in CP's `depends-on: [architecture, execution-model, event-model, handler-contract]`; spot-checked cross-spec mnemonics against `/tmp/{ar,em,ev,hc}-mnem-map.csv` (all referenced mnems resolve). Validated the 13 forward-deferred edges against the corpus state. Verified F-pilot-CP-1 direction (5 consumer→`em-error.taxonomy` edges + 1 schema) per discipline §2.11(c.2).

---

## Summary

**Total findings: 3** (0 BLOCKER, 1 MAJOR, 2 MINOR). The pilot's reference layer is in clean shape: all 277 active cross-spec edges trace to normative-prose cites; the depends-on universe is fully respected; the forward-deferral set of 13 is correctly bounded; the consumer-direction `em-error.taxonomy` edges (cp-015, cp-023, cp-027, cp-034b, cp-049 + cp-schema.budget-payload) match discipline v0.9 §2.11(c.2); no HC-style direction inversion. The three findings below are: one missed term-use edge in CP-008 (MAJOR), one missing `cite:wide-fanout` tag on CP-048 mirroring the F-pilot-CP-7 pattern that was correctly tagged on CP-013 (MINOR), and one questionable section-anchor resolution on CP-024's `[event-model.md §8.9]` cite (MINOR).

---

## Findings

### F-refs-CP-1 — CP-008 missing term-use edge to AR §4.8 role taxonomy [MAJOR]

**Source spec cite (line 131, normative-prose, CP-008 body):**
> `approval-gate` requires a named approver (human role or agent role per [architecture.md §4.8]); `quality-gate` requires a prior verification node's outcome to satisfy a policy expression.

**Pilot edge:** none (cp-008's blocks edges are `cp-006`, `cp-schema.gate-subtype` only).

**Why this is a hard term-use, not a supporting cite:** the cite anchors the `role` term — "human role or agent role" — to AR §4.8's role taxonomy (the single owner of role names per AR-039..AR-042). Per discipline §3.1 step 5 (term-use cite rule), an inline use of a defined term whose definition is owned by another single requirement set in the same or in-`depends-on` spec generates a `blocks` edge. Operational F-pilot-AR-10 test: removing the cite leaves "approval-gate requires a named approver (human role or agent role)" — but `human role` and `agent role` are AR-§4.8-defined classifications; without AR §4.8 the rule cannot validate which roles are legal approvers. Hard dep.

**Symmetry with peer beads:** cp-017, cp-028, cp-029, cp-030, cp-039 all term-use AR §4.8 role names and emit `→ ar-039, ar-040, ar-041, ar-042`. cp-008 should follow the same pattern.

**Severity:** MAJOR — pilot omits a real dependency that is materially identical to the cp-017/028/029/030/039 cluster the pilot already emits.

**Lane:** `local` — discipline §3.1 step 5 is unambiguous; pilot misapplied the term-use rule to one §4 req. F-pilot-AR-10 would not trigger here (the role term is the rule's load-bearing input).

**Patch:** add `cp-008 → ar-039, ar-040, ar-041, ar-042` to `cp-pilot-data.yaml`'s cross-spec AR edge block (between current cp-007/cp-009 entries). Bump pilot version 0.1.0 → 0.1.1.

---

### F-refs-CP-2 — CP-048 missing `cite:wide-fanout` tag [MINOR]

**Source spec cite (line 443, normative-prose, CP-048 body):**
> Every ControlPoint effect ... MUST emit a typed event per [event-model.md §8]: `gate_allowed`, `gate_denied`, `gate_escalated`, `hook_fired`, `hook_failed`, `guard_reordered`, `guard_failed`, `budget_accrual`, `budget_warning`, `budget_exhausted`.

**Pilot edge fanout:** cp-048 fans out to 10 specific `ev-events.*` rows (correct tighter fanout per F-pilot-CP-7 prose-enumeration logic).

**Pilot tag:** cp-048 carries `tag:mechanism` and `req:CP-048` (and the implicit `spec:control-points`); it does NOT carry `cite:wide-fanout`.

**Why this is a tag miss:** the cite text uses `[event-model.md §8]` (section anchor without specific row), identical in shape to CP-013's `[event-model.md §8]` cite which the pilot did tag `cite:wide-fanout` (yaml line: `cite:wide-fanout` extra_label on cp-013). Per discipline §3.1 step 3, section-anchor cites carry the `cite:wide-fanout` tag at edge-fire time, even when the body's prose enumeration permits a tighter fanout (per F-pilot-CP-7's note: "`cite:wide-fanout` tag still fires per §3.1 step 3 because the cite text uses §8 (section) form not specific row names"). CP-048 is the same shape and should carry the same tag.

**Severity:** MINOR — operational triage tag; does not affect edge correctness.

**Lane:** `local` — pilot inconsistency between cp-013 and cp-048 application of the same rule.

**Patch:** add `cite:wide-fanout` to cp-048's `extra_labels` in `cp-pilot-data.yaml`. Same revision as F-refs-CP-1.

---

### F-refs-CP-3 — CP-024's `[event-model.md §8.9]` cite resolution to ev-005 is imprecise [MINOR]

**Source spec cite (line 240, normative-prose, CP-024 body):**
> Per-chunk granularity is explicitly retained for MVH per [event-model.md §8.9]; a future log-level filter MAY suppress chunk events at consumer boundaries without changing the emission contract.

**Pilot edge:** `cp-024 → ev-005` ("lifecycle-boundary signals, not agent internals"). The pilot attaches no `cite:wide-fanout` tag for this section cite.

**Why imprecise:** EV §8.9 is "Acceptance criteria for candidate event types" — meta-prose listing the (a)–(h) criteria a §8 row must satisfy, not a numbered req. There is no `EV-NNN` housed at §8.9; the closest reqs are EV-005 (lifecycle-boundary signals — acceptance criterion (b)) and arguably EV-008 (partial-order)/EV-009 (subscription declared at registration). The pilot's choice of `ev-005` picks up the closest semantically related req but leaves the cite under-mapped. Per discipline §3.1 step 3, the pilot SHOULD either (a) resolve the section anchor to a specific row OR (b) tag `cite:wide-fanout` if the section houses multiple §4 reqs.

**Severity:** MINOR — the chosen edge is defensible (EV-005 is the lifecycle-boundary anchor cited by §8.9 criterion (b)); the issue is missing wide-fanout tag for traceability.

**Lane:** `class` — a one-line `class`-lane note: when a citing body's `§N` cite resolves to a meta-prose acceptance-criteria block (no specific req), the pilot SHOULD apply `cite:wide-fanout` and document the closest-req chosen. Could be folded into F-pilot-CP-7's discipline-patch candidate. No standalone discipline patch warranted.

**Patch:** add `cite:wide-fanout` to cp-024's `extra_labels`. Optionally add an additional edge `cp-024 → ev-008` for partial-order context (EV-008 is the under-cited target if §8.9 is being treated as fanout). Same revision as F-refs-CP-1/2.

---

## Direction-sanity (CP-specific) — VERIFIED CLEAN

Per discipline §2.11(c.2) (HC v0.1.2 lockstep release), `<spec>-error.taxonomy` is a vocabulary OWNER; consumers `block-on` the taxonomy bead, never the inverse.

CP has no `cp-error.taxonomy` bead (CP §8 routes to EM). The 5 consumer→`em-error.taxonomy` edges + 1 schema cite the pilot emits are:

| citing bead | target | direction | verdict |
|---|---|---|---|
| cp-015 | em-error.taxonomy (and hc-error.taxonomy) | consumer→owner | ✓ correct |
| cp-023 | em-error.taxonomy | consumer→owner | ✓ correct |
| cp-027 | em-error.taxonomy | consumer→owner | ✓ correct |
| cp-034b | em-error.taxonomy (and hc-error.taxonomy) | consumer→owner | ✓ correct |
| cp-049 | em-error.taxonomy (via hc-error.taxonomy) — pilot routes through hc-error.taxonomy + hc-022 sub-sentinel | consumer→owner | ✓ correct |
| cp-schema.budget-payload | em-error.taxonomy | type-cite via consumer schema | ✓ correct (mirrors §2.11(c.2) for type-cites) |

All edges run consumer→owner. **No HC-style direction inversion.** F-pilot-CP-1 self-classification (RESOLVED by §2.11(c.2) by analogy) is correct.

Spot-checked the v0.9 anti-pattern: the pilot's `from`/`to` tuples for em-error.taxonomy show no entries with em-error.taxonomy on the `from` side (which would invert ownership). Pilot is clean on this axis.

---

## Forward-deferred edge validation — VERIFIED CLEAN

Pilot claims **13 forward-deferred edges** in the breakdown 1 wm + 4 on + 4 rc + 1 bi + 3 ev-missing-row. Each was independently validated:

| # | from | target | spec status | verdict |
|---|---|---|---|---|
| 1 | cp-040 | forward:wm-NNN | WM not yet drafted (workspace-model) | ✓ correctly forward-deferred |
| 2 | cp-013 | forward:on-NNN | ON not yet drafted (operator-nfr) | ✓ correctly forward-deferred |
| 3 | cp-037 | forward:on-NNN | ON not drafted | ✓ |
| 4 | cp-038 | forward:on-NNN | ON not drafted | ✓ |
| 5 | cp-047 | forward:on-NNN | ON not drafted; reverse-cited | ✓ |
| 6 | cp-027 | forward:rc-NNN | RC not yet drafted | ✓ |
| 7 | cp-040a | forward:rc-NNN | RC not drafted | ✓ |
| 8 | cp-041 | forward:rc-NNN | RC not drafted | ✓ |
| 9 | cp-inv-003 | forward:rc-NNN | RC not drafted | ✓ |
| 10 | cp-031 | forward:bi-NNN | BI's §4.9 finalization is the migration target for `[docs/foundation/components.md §10.9]` | ✓ |
| 11 | cp-034b | forward:ev-events.policy-expression-exceeded-cost | confirmed absent from `/tmp/ev-mnem-map.csv` | ✓ EV r2 spec gap (tracked under hk-ahvq.45) |
| 12 | cp-041 | forward:ev-events.verdict-envelope-mismatch | confirmed absent from `/tmp/ev-mnem-map.csv` | ✓ EV r2 spec gap |
| 13 | cp-043 | forward:ev-events.control-points-registration-started | confirmed absent from `/tmp/ev-mnem-map.csv` (ev-events.control-points-registered IS present at hk-hqwn.59.20, sibling event correctly resolves to a real edge — pilot did not over-defer the present sibling) | ✓ EV r2 spec gap |

The 3 EV missing-row deferrals correctly distinguish the absent rows from the present `ev-events.control-points-registered` (which the pilot correctly resolves to a real edge in cp-048's fanout). F-pilot-CP-3 is well-structured: Option (a) carry-forward is consistent with F-pilot-EM-2 / Option B precedent. The class-lane recommendation in the pilot's narrative — "EV completeness precondition for emitting-spec pilots so future pilots (WM, RC, ON) detect the gap proactively" — is reasonable but is a discipline-doc note, not a corpus-blocking finding.

---

## Bidirectional-cite cycle scan — VERIFIED CLEAN

Walked CP's normative-prose inline cites against the four `depends-on` specs' inline cites back to CP:

- **CP ↔ AR.** AR has no normative-prose cites to CP requirement IDs (AR cites to control-points appear only in `> INFORMATIVE:`/scope blocks, e.g., AR scope §2.2 line 48 and AR §6.5 line 618 — both in informative locations). CP cites AR §4.1/§4.2/§4.4/§4.6/§4.8/§4.10. Direction: CP→AR only. No cycle.
- **CP ↔ EM.** EM normative-prose contains multiple cites to control-points (EM-007 §4.1 line 81 cites `[control-points.md §6.3]`; EM-008 line 148 cites `[control-points.md §6.3]`; EM-042 §4.10 line 515 cites `[control-points.md §6.4]` and `[control-points.md §6.2]`; §6 schema annotations EM-line 608/621/633/664 cite control-points types). CP cites EM §4.1/§4.2/§4.3/§4.9/§4.10/§8/§8.5/§8.4. **Bidirectional cite shape exists** between CP-018/019 (Guard) ↔ EM-042 (cascade owner). Resolution per §2.7 F13 slot-rule heuristic: EM-042 is the slot rule (declares the cascade); cp-018/019 fill the slot with reorder semantics → cp-018/019 → em-042 is the normative dep, em-042 → cp-018/019 reclassified to no-edge (informational "see also"). Spot-checked EM pilot for em-042 → cp-* edges: none found. Slot-rule resolution consistently applied. No cycle.
- **CP ↔ EV.** EV §9 / §10 normative-cite to CP appears at EV-line 858/860/954 — all in `### 9.` co-references or §10 conformance prose (no-edge per §3.1 / §2.7 third class). CP cites EV §4.3/§4.4/§6.3/§8/§8.2/§8.4/§8.9. Direction: CP→EV only. No cycle.
- **CP ↔ HC.** HC has cites to control-points at HC-line 53 (informative scope), HC-line 503 (§4.11 normative — refers to "the workflow-load-time resolution per `[execution-model.md §4.9]` and `[control-points.md §4.11]`"), HC-line 606/610/611 (§6 schema annotations). The HC §4.11 cite is a covering-partition cross-reference where HC owns launch-time resolution and CP owns declaration-time; per discipline F13 declaration-rule ↔ method-rule: declaration-rule (CP-049) → method-rule (HC-046..050). The CP pilot emits cp-049 → hc-046/048/050; HC pilot's reciprocal direction was already resolved at HC r1 / r2. No cycle.

---

## Depends-on validation — VERIFIED CLEAN

Active cross-spec edge target prefixes: `{ar, em, ev, hc}`. CP front-matter `depends-on: [architecture, execution-model, event-model, handler-contract]`. All four target specs are in `depends-on`. **No depends-on violation.**

Forward-deferred target prefixes (`{wm, on, rc, bi}`) are intentionally outside `depends-on` per F-pilot-EM-2 / Option B carry-forward precedent — these become real edges only when the reciprocal pilot lands and a `depends-on` patch may follow.

---

## Coverage cross-check — VERIFIED CLEAN (modulo F-refs-CP-1)

Walked every normative-prose inline cite in §4/§5/§6/§8 (excluding `> INFORMATIVE:`/`> NOTE:`/`> EXAMPLE:` blocks, §7 prose per F-em-r1-MIN-3, §9.1 depends-on / §9.3 co-references / §10 / §11 / §12 / §A per discipline §3.1). Result: **49 of 50 normative-prose cites have a corresponding pilot edge.** The one missed: F-refs-CP-1 (CP-008 → AR §4.8 role taxonomy term-use).

The pilot correctly applies the F-pilot-AR-10 supporting-cite test to CP-007 / CP-010 / CP-021 / CP-033 / CP-038's bare "per [execution-model.md §4.10]" / "per `_registry.yaml`" attachments (F-pilot-CP-4) — these are non-edge by the operational test. Spot-checked: the pilot's reasoning matches; not flagged.

The pilot correctly applies the no-edge list to: §8 routing prose (CP §8 routes to EM, no separate cp-error.taxonomy), §10.3 conformance carve-outs, §11 OQs, §12 revision history, §A appendices, all `> INFORMATIVE:` / `> NOTE:` / `> EXAMPLE:` blocks, and `docs/foundation/components.md` cites (per F-pilot-AR-AO3 / F-pilot-CP-9). Three §6.4.1 inlined-key-paths cites (lines 834 `[operator-nfr.md §4.3]`, 837 `[handler-contract.md §6.1]`) are field-annotation / illustrative descriptions — defensibly non-edge per F-pilot-AR-AO2 illustrative-example posture; not flagged.

**No invented edges:** every pilot cross-spec edge traces to a normative-prose inline cite. The 277 active edges are clean.

---

## Severity / lane summary

| ID | severity | lane | one-line action |
|---|---|---|---|
| F-refs-CP-1 | MAJOR | local | add cp-008 → ar-039/040/041/042; pilot v0.1.0 → v0.1.1 |
| F-refs-CP-2 | MINOR | local | add `cite:wide-fanout` extra_label to cp-048 |
| F-refs-CP-3 | MINOR | class (low priority; could fold into F-pilot-CP-7) | add `cite:wide-fanout` to cp-024; optionally edge to ev-008 |

No BLOCKER findings. Pilot loads with the 3 patches applied (F-refs-CP-1 adds 4 cross-AR edges; F-refs-CP-2 / F-refs-CP-3 are tag-only). Edge count adjusts: 277 + 4 = 281 cross-spec active edges; intra-spec unchanged; forward-deferred unchanged at 13.
