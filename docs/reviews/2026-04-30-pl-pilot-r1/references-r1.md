# PL Pilot r1 — Reference Reviewer Report

`reviewer: reference (r1)` · 2026-04-30 · against `specs/process-lifecycle.md` v0.4.1 (891 lines), `docs/decompose-to-tasks/pl-pilot.md` v0.1.0, `pl-pilot-data.yaml` v0.1.0, `discipline.md` v0.9, `pilot-review-protocol.md` v0.2.

Method walks PL spec body top to bottom, records every cross-spec inline cite, walks the yaml's edge list, cross-checks per protocol §3.3. Cross-spec target validity verified against `/tmp/{ar,em,ev,hc,cp,wm,bi}-mnem-map.csv`.

## Summary

- **Cross-spec target ID validity:** ALL 7 active mnem-map cross-spec edges (ar, em, ev, hc, cp, wm, bi) resolve to existing mnemonics. No invented bead IDs.
- **Section-anchor wide-fanout tag policy:** ZERO `cite:wide-fanout` tags emitted across the pilot. Several already-loaded section-anchor cites (AR/EM/EV/HC/CP/WM) merit the tag.
- **§2.11(d.2) PL→EV-row direction audit:** 15 `pl-NNN → ev-events.<name>` edges fired, all in correct direction (consumer→owner). Count matches PL §6.2's 8 daemon-lifecycle events plus §4.5/§4.9 derived events (`agent_failed`, `operator_upgrading/-completed/-rejected`) and PL-005-consumed events (`reconciliation_category_assigned`, `reconciliation_verdict_executed`). No reciprocal `ev-events.X → pl-NNN` edges (correctly owned by EV pilot per `hk-ahvq.23` backfill plan).
- **§2.11(c.2) consumer→error.taxonomy direction:** N/A. PL has no local taxonomy bead per F-pilot-PL-1. Consumer→owner direction lands as `pl-NNN → forward:on-NNN` to ON's exit-code taxonomy; direction is correct (consumer-blocks-on-vocabulary-owner).
- **Bidirectional cite cycles:** None remaining in-corpus. Pilot §9 cycle-pre-check enumerates 6 reverse-cite candidates resolved one-way per F13 / F-pilot-AR-10 (PL-004↔PL-027, PL-005↔PL-009/PL-010, PL-011↔PL-011a, PL-012↔PL-011a, PL-019↔PL-028, PL-008a↔§4-citing reqs). Spot-checked; all OK.
- **Direction inversions:** None.
- **Total findings:** 1 BLOCKER candidate (pending F-pilot-PL-4 lane decision below); 2 MAJOR (missed invariant-body term-use edges); 2 MINOR (wide-fanout tag, hc-007 standin for HC §4.6).
- **F-pilot-PL-4 26 forward:on-* edges:** see §4 below — recommended **discipline-patch lane**, MEDIUM priority.

## 1. Missed edges

### F-refs-PL-1 — PL-INV-001 body explicit-ID cites to BI-030 and WM-013a do NOT emit edges. [MAJOR; class]

PL-INV-001 body: *"This invariant spans [operator-nfr.md §4.3] (...), [beads-integration.md §4.10 BI-030] (...), and [workspace-model.md §4.3 WM-013a] (...)."*

Per discipline v0.7 §2.5 source 4 (F-em-r1-MAJ-1 invariant-body term-use sub-clause): when an invariant's body uses a defined term whose definition is owned by an in-`depends-on` requirement (with or without an explicit inline cite), emit `<sensor> → <defining-bead>`. Both BI-030 and WM-013a are in PL's depends-on AND directly named with explicit ID in the invariant's body. Yaml only emits `pl-inv-001 → forward:on-NNN`; missing:

- `pl-inv-001 → bi-030` (intent log keyed against single-writer daemon)
- `pl-inv-001 → wm-013a` (worktree-lease leasing-authority assumption)

The pilot author may be reading "spans" as supporting prose under F-pilot-AR-10 supporting-cite test — but §2.5 source 4 explicitly says "with or without an explicit inline cite" and lists invariant-body term-use as a direct sensor predecessor source independent of the supporting-cite heuristic. F-em-r1-MAJ-1's worked example (`em-inv-005` body using `Harmonik-Bead-ID` from `em-schema.checkpoint-trailers`) is structurally identical: invariant-body explicit ID-cite to a defining bead in an in-depends-on spec → edge fires.

**Lane: class.** The discipline rule is unambiguous; the pilot author missed it for two cross-spec invariant predecessors. Same surface as F-em-r1-MAJ-1's origin; reviewers across pilots may need a sharper "invariant-body 'spans X'" worked example added to §2.5 source 4 to head off the pattern in RC/ON.

### F-refs-PL-2 — PL-INV-003 body explicit cite to RC-INV-005 not in forward log specifically. [MINOR; local]

PL-INV-003 body: *"detectors scope on runs ([reconciliation/spec.md RC-INV-005] detectors filter by `run_id`, never `bead_id`)"*. Per F-refs-EV-6 sensor→sensor extension (v0.8): when an invariant body explicitly cites another invariant by `<prefix>-INV-NNN` ID, edge fires `pl-inv-003 → rc-inv-005`. RC pilot is NOT yet loaded; the edge should appear in the pilot's forward-deferred log as `pl-inv-003 → forward:rc-inv-005` (specific, not the generic `forward:rc-NNN`).

Yaml has `pl-inv-003 → forward:rc-NNN` (generic). Per the F-pilot-PL-5 narrative the author calls it "informational" and emits the generic forward — but F-refs-EV-6 §2.5 sensor↔sensor explicit-ID-cite extension makes this NOT informational: the §2.5 F12 sensor↔impl one-way rule does NOT apply between two sensor beads, so the edge fires.

**Lane: local.** F-refs-EV-6 worked-example precedent (`ev-inv-001 → em-inv-001`) is direct. Tighten the placeholder to `forward:rc-inv-005` so RC's reciprocal pilot resolves the specific target.

### F-refs-PL-3 — `cite:wide-fanout` tag missing on already-fired section-anchor edges. [MINOR; class]

Per discipline §3.1 step 3 + §3.1.3 forward-deferred wide-fanout tag policy: when a citing bead emits an edge derived from a section-anchor cite (no specific req ID), the citing bead is tagged `cite:wide-fanout`. F-em-r1-MIN-6 (v0.7) clarifies the tag fires at edge-fire time, not deferral time. For ALREADY-LOADED target specs the tag should fire NOW.

Examples in PL where the tag is missing:
- `pl-005 → cp-001` derived from `[control-points.md §4.1]` section-anchor (yaml comment line 1133 explicitly says "closest anchor is cp-001" — the wide-fanout collapse pilot author chose a single-bead approximation for a section-anchor cite without tagging).
- `pl-014 → hc-001` derived in part from `[handler-contract.md §4.1 HC-001]` (specific, OK), but the broader §4.1 "Handler interface" cite is wide.
- `pl-020 → ar-016` derived from `[architecture.md §4.5 AR-016]` (specific) — OK.
- `pl-005 → ev-002` derived from `[event-model.md §4.1]` (section anchor) — should tag wide-fanout.

The yaml emits zero `cite:wide-fanout` tags total. At minimum, `pl-005` (multiple section-anchor cross-spec cites in step 0/5/6/7) and `pl-014` deserve the tag. Note the same pilot's narrative §3.1 says the universe is correct; this finding is purely tag-application.

**Lane: class.** Recurring; HC pilot r1 surfaced an analogous tag-omission pattern. Discipline could clarify that the tag is mandatory at edge-fire time when the cite is unambiguously a section-anchor (no specific ID) AND the target spec is loaded — currently §3.1.3 phrasing ("tag fires at edge-fire time") is silent on omission consequences.

### F-refs-PL-4 — `pl-017` uses `hc-007` as standin for HC §4.6 silent-hang detection. [MINOR; local — author already disclosed]

The yaml comment (line 1124) acknowledges: "silent-hang detection rule lives in HC §4.6 (closest mapped anchor is hc-007 for §4.2..§4.10 coalesce; backfill to a more specific row when HC adds one)". This is a known gap — HC's pilot collapsed §4.2..§4.10 such that no specific HC §4.6 anchor exists. Standin `hc-007` is the consumer's best available target; the pilot deserves the wide-fanout tag and a forward-cite-log line for HC's next pilot to materialize a specific row when HC §4.6 silent-hang gets its own bead. Not a discipline gap.

**Lane: local.** Author flagged in yaml comment; reviewer concurs.

## 2. Invented edges

None observed. All cross-spec edges target valid mnemonics (verified against `/tmp/{ar,em,ev,hc,cp,wm,bi}-mnem-map.csv`).

## 3. Depends-on violations

### F-refs-PL-5 — PL-004 supporting cites to BI-030 / WM-013a (not emitted; correct under F-pilot-AR-10). [no-finding]

PL-004 body enumerates `.harmonik/...` paths each with "(per [BI-030])", "(per [WM-013a])", "(per [event-model.md §6.2])" supporting cites. Per F-pilot-AR-10 supporting-cite test the surrounding enumeration ("daemon's per-project file surface includes …") is independently testable; "per X" is informational. No edge correctly emitted.

### F-refs-PL-6 — 26 forward:on-* edges; ON not in PL's `depends-on`. [BLOCKER candidate; class — see §4 below]

This is the load-bearing finding of the pilot. Yaml's 26 `forward:on-*` edges are §3.2 violations as the discipline reads today. Full disposition + lane recommendation below.

## 4. F-pilot-PL-4 — Class-lane disposition + lane recommendation

**Recommendation: discipline-patch lane, MEDIUM priority. Drop the 26 `forward:on-*` edges from the yaml in pilot-patch lane simultaneously; convert the inventory to F-pilot-EV-3-style informational tracking until the discipline patch lands.**

### 4.1 Probes (per pilot-review-protocol §4.1)

**(1) Generality.** YES. Other specs reaching pilot stage will hit the same shape:
- RC pilot: ON's depends-on includes RC; the cycle-break-NOTE pattern means RC will likely cite ON-class obligations symmetrically without listing ON in depends-on.
- HC and CP already cite ON without listing it in depends-on (HC §4.6 silent-hang detection and CP §4.x policy/budget — both surface ON-named obligations under §2/§9 named-obligation pattern).
- ON pilot itself (when drafted) will have to settle whether named-obligation cites *to PL* (which IS in ON's depends-on) emit hard-dep edges symmetrically.
The general shape is "spec X cites spec Y normatively under the §2/§9 named-obligation pattern; Y is intentionally absent from X's depends-on to break a cycle; some of those cites are hard deps." This shape will recur in ≥3 of the remaining pilots (RC, ON-as-emitter-of-RC-cites, and any post-MVH spec that adds named-obligation cites against existing pre-MVH specs).

**(2) Rule-vs-application.** **The pilot followed the discipline correctly and produced bad output.**
- Discipline §3.2 says: "every cross-spec edge `<bi-bead>` → `<other-bead>` MUST cite a spec whose `spec-id` appears in BI's `depends-on`. An edge to a spec NOT in `depends-on` is a bug — either the edge is invented, or `depends-on` is incomplete and the spec needs a patch."
- The pilot correctly identified hard-dep cites under F-pilot-AR-10 ("removing X changes citing rule's behavior") to ~17–26 ON IDs in PL's body.
- The pilot then chose to emit them as forward-deferreds **flagged as §3.2 violations** rather than (a) silently invent the edges, (b) silently drop them, or (c) demand `depends-on` be patched (which would re-create the cycle).
- The discipline today provides NO mechanism for "hard-dep cite to a spec excluded from depends-on by an explicit cycle-break". The author's choice (flag-and-emit) preserves the audit trail.

This is the textbook §4.1 probe-2 "yes": rule is silent on the exact case, applying the rule mechanically produces output the rule itself flags as a violation.

**(3) Silence.** YES. The discipline is mute on cycle-break-NOTE-induced exclusions. §3.2's worked example for the BI ↔ EM mutual dependency works because the cycle splits at the requirement level (BI's `bi-017` blocks-on `em-014`; `em-014` does NOT block on any BI bead). PL ↔ ON does NOT split that cleanly: PL's `daemon_ready` event emission TIME is owned by PL but REFERENCED by ON-031/ON-033 (RTO measurement endpoint); PL's startup-failure-mode catalog OBLIGATION is owned by ON-003 but CONSUMED by PL-008. The cycle break in §9.3 NOTE is at the front-matter level, not the requirement level. The discipline's existing language never anticipated this.

**(4) Reviewer self-classification.** Pilot self-flags `class` lane in F-pilot-PL-4 narrative. Per §4.1 probe 4, any `class` from any reviewer routes the finding to discipline lane unless explicitly overruled in synthesis. I do not overrule; I concur.

### 4.2 What the discipline patch should likely say

Three plausible shapes (synthesis can choose):

**(a) Named-obligation hard-dep carve-out.** Add a clause to §3.2: "When a spec X cites spec Y under the §2/§9 named-obligation pattern AND Y is excluded from X's depends-on by an explicit cycle-break NOTE in §9.3, the §3.2 depends-on validation rule is relaxed for those edges; they are emitted as forward-deferreds tagged `cycle-break-named-obligation` with `cite:type=hard-dep` per the F-pilot-AR-10 supporting-cite test. The reciprocal pilot (Y) materializes the predecessor when its own pilot loads. This is functionally identical to the §2.11(d.2) co-owned event payload pattern where the EV-row's WHEN-rule cites the consumer's emission-rule across a similar exclusion." Worked example: PL §9.3 NOTE excludes ON; PL-009 cites ON-031 normatively; edge `pl-009 → forward:on-031` emits with `cycle-break-named-obligation` tag; ON pilot's reciprocal pilot resolves the `forward:` placeholder.

**(b) Convert to F-pilot-EV-3-style informational tracking.** Treat the 26 cites as informational (forward log only, no edges); rely on the §10 conformance test surface to catch consumer divergence at integration time. Cleaner but loses the hard-dep distinction the F-pilot-AR-10 test surfaces.

**(c) Re-add ON to depends-on and split the cycle at the requirement level.** Reject. The PL ↔ ON cycle is structural (ON's operator-control state machine builds on PL's status prefix; PL's startup catalog comes from ON-003); splitting the requirement level would require reorganizing both specs.

**My recommendation: (a)** with the caveat that synthesis may prefer (b) for simplicity. (a) preserves the F-pilot-AR-10 hard-dep distinction and gives reciprocal pilots a clean handle; (b) trades audit fidelity for one fewer tag.

### 4.3 Pilot-patch lane action (regardless of which (a)/(b) lands)

Drop the 26 `forward:on-*` edges from `pl-pilot-data.yaml` v0.1.0 → v0.1.1 simultaneously with the discipline patch landing; re-emit under the new discipline rule (with the new tag if (a), or as informational forward-log entries if (b)). Per §4.2 of the protocol the affected pilot is re-drafted against the new discipline; do not patch in place.

The pilot is otherwise loadable today; the 26 edges are CURRENTLY logged as `forward:*` placeholders that the loader does NOT load into Beads, so no `br dep cycles` rejection threat. The pilot can sit as v0.1.0-pending until the discipline patch lands.

## 5. Bidirectional cite cycles

None remaining. Pilot §9 enumerates 6 reverse-cite candidates resolved one-way per F-pilot-AR-10 supporting-cite test:
- PL-004 ↔ PL-027 — file-surface owner vs upgrade-marker writer; reverse no-edge (PL-027 is consumer).
- PL-005 ↔ PL-009 / PL-010 — startup-step trigger vs ready/degraded predicate; reverse no-edge (predicate owners).
- PL-011 ↔ PL-011a — drain step 5 vs daemon_shutdown emission rule; reverse no-edge (PL-011a owns rule).
- PL-012 ↔ PL-011a — interceptable-immediate emits event; reverse no-edge.
- PL-019 ↔ PL-028 — orchestrator-agent shape vs runner spawn entry; reverse no-edge.
- PL-008a ↔ §4 citing reqs — taxonomy consumer vs sentinel-citing reqs; reverse no-edge per §2.11(c.2).

Spot-checked all six against spec body. Author's resolution holds in each case.

## 6. Direction inversions

None observed. Cross-spec direction audit:

- **PL→AR (7 edges).** All consumer→owner. `pl-env-001 → ar-052/053` (envelope shape consumer); `pl-018/019/020a/inv-002 → ar-inv-007` (centralized-controller invariant cited explicitly by ID — invariant-as-target exemption applies for impl→invariant cites BUT for `pl-inv-002 → ar-inv-007` invariant→invariant per F-refs-EV-6 the edge fires; for impl→invariant the F-pilot-AR-r2-2 exemption fires, so `pl-018 → ar-inv-007` should NOT fire). **Wait:** `pl-018 → ar-inv-007` IS in the yaml (line 1078), `pl-019 → ar-inv-007` (line 1079), `pl-020a → ar-inv-007` (line 1081). These three are impl-bead → invariant-bead edges. Per F-pilot-AR-r2-2 invariant-as-target exemption, the term-use rule does NOT fire when defining req is `<prefix>-INV-NNN`. **POTENTIAL DIRECTION INVERSION** — see F-refs-PL-7 below.

### F-refs-PL-7 — `pl-018 → ar-inv-007`, `pl-019 → ar-inv-007`, `pl-020a → ar-inv-007` may violate F-pilot-AR-r2-2. [MAJOR; class candidate]

The discipline §3.1 step 5 says: "When the defining requirement is a `<prefix>-INV-NNN` invariant, the term-use rule does NOT fire." Worked example in §3.1: `ar-038 → ar-inv-007` was rejected because AR-038 IS itself an AR-INV-007 sensor predecessor (creates 2-cycle).

PL-018 / PL-019 / PL-020a all explicitly cite AR-INV-007 by ID in their bodies. AR-INV-007 (centralized-controller invariant) has a sensor (`ar-inv-007` bead in AR pilot) whose predecessors per AR pilot's §10.2 INCLUDE `pl-018` (and possibly more PL beads, depending on AR's persona-bundling). The AR pilot's `ar-inv-007 → pl-018` direction IS the sensor-blocks-on-impl direction; the yaml's reverse `pl-018 → ar-inv-007` direction creates a 2-cycle exactly per F-pilot-AR-r2-2's worked example.

The yaml comment (line 297 of yaml header narrative + pilot doc §8 F-pilot-PL-5) acknowledges F-pilot-AR-r2-2 BUT carves out an exception: "Per F-pilot-AR-r2-2 invariant-as-target exemption, term-uses of 'centralized-controller invariant' (AR-INV-007) from PL body do NOT emit edges; AR-INV-007 is sensor-only EXCEPT for explicit invariant→invariant ID-cites per F-refs-EV-6 — PL-INV-002's body explicitly cites AR-INV-007 by ID, so per D-WM-3 precedent the edge `pl-inv-002 → ar-inv-007` IS emitted. PL-018, PL-019, PL-020a similarly cite AR-INV-007 by ID and emit edges."

**The carve-out is incorrect for impl→invariant cites.** F-refs-EV-6 (v0.8) extends F10 to **sensor→sensor** explicit-ID-cite, not impl→invariant. F-pilot-AR-r2-2 is the impl→invariant rule and applies regardless of whether the invariant-cite is "explicit by ID" or implicit.

If `pl-018 → ar-inv-007`, `pl-019 → ar-inv-007`, `pl-020a → ar-inv-007` load AND AR's `ar-inv-007` blocks-on `pl-018` (via PL-INV-002's mention or AR's persona bundling), Beads will reject these as cycles at load time. **This is a LOAD-TIME blocker risk.** Must verify against AR's pilot:

- `pl-inv-002 → ar-inv-007` IS correct (F-refs-EV-6 sensor→sensor — both sides are invariant beads).
- `pl-018 → ar-inv-007`, `pl-019 → ar-inv-007`, `pl-020a → ar-inv-007` should NOT fire (F-pilot-AR-r2-2 impl→invariant exemption).

**Lane: class candidate IF the carve-out the pilot author invented ("PL-018, PL-019, PL-020a similarly cite AR-INV-007 by ID and emit edges") was generalised from a prior pilot's pattern. Otherwise local.** I read it as local — the pilot author misread F-refs-EV-6 (sensor→sensor only) as extending to impl→invariant. The discipline rule is unambiguous; the application is wrong.

**Severity: MAJOR (load-time cycle risk).** Recommend pilot patch v0.1.0 → v0.1.1: remove `pl-018 → ar-inv-007`, `pl-019 → ar-inv-007`, `pl-020a → ar-inv-007` (keep only `pl-inv-002 → ar-inv-007` per F-refs-EV-6).

Worth flagging to AR pilot's owner: verify `ar-inv-007`'s predecessor list does not include `pl-018` directly; if AR's persona bundling pulls PL-018 (it shouldn't — PL is a different spec from AR), the cycle is closed by the invalid PL→AR-INV-007 edges only.

## 7. §2.11(d.2) PL→EV-row direction audit

15 `pl-NNN → ev-events.<name>` edges; all correct direction (consumer→owner per §2.11(d.2)).

| pl-bead | ev-events target | direction-correctness |
|---|---|---|
| pl-005 | ev-events.daemon-started | ✓ consumer→owner |
| pl-005 | ev-events.reconciliation-category-assigned | ✓ |
| pl-005 | ev-events.reconciliation-verdict-executed | ✓ |
| pl-006 | ev-events.daemon-orphan-sweep-completed | ✓ |
| pl-009 | ev-events.daemon-ready | ✓ |
| pl-009a | ev-events.reconciliation-category-assigned | ✓ |
| pl-010 | ev-events.infrastructure-unavailable | ✓ |
| pl-010 | ev-events.daemon-degraded | ✓ |
| pl-011a | ev-events.daemon-shutdown | ✓ |
| pl-008a | ev-events.daemon-startup-failed | ✓ |
| pl-014a | ev-events.dispatch-deferred | ✓ |
| pl-027 | ev-events.operator-upgrading | ✓ |
| pl-027 | ev-events.operator-upgrade-completed | ✓ |
| pl-027 | ev-events.operator-upgrade-rejected | ✓ |
| pl-016 | ev-events.agent-failed | ✓ |

15 edges; matches PL §6.2's enumeration of 8 PL-emitted lifecycle events plus the 3 operator-upgrade events from PL-027(v) + the `agent_failed` event from PL-016 + 2 reconciliation events consumed at PL-005 (per §4.a (b)). Reciprocal `ev-events.X → pl-NNN` direction is owned by EV pilot per `hk-ahvq.23` backfill plan; correctly absent from PL pilot.

## 8. Counts summary

| Category | Count |
|---|---|
| Total findings | 7 (1 BLOCKER candidate, 3 MAJOR, 3 MINOR) |
| BLOCKER candidate | F-refs-PL-6 (= F-pilot-PL-4; lane rec: discipline-patch) |
| MAJOR | F-refs-PL-1 (PL-INV-001 missed BI-030 / WM-013a), F-refs-PL-7 (impl→AR-INV-007 direction inversion — load-time cycle risk) |
| MAJOR | (F-refs-PL-1 split into two missed targets but counted as one finding) |
| MINOR | F-refs-PL-2 (forward:rc-inv-005 specificity), F-refs-PL-3 (cite:wide-fanout tag), F-refs-PL-4 (hc-007 standin) |
| Cross-spec edges total in yaml | ~48 active + 39 forward-deferred (13 rc + 26 on) |
| Direction inversions blocking load | 3 (F-refs-PL-7) |
| Bidirectional cycles requiring author resolution | 0 |
| Invented edges | 0 |
| Depends-on violations | 26 (F-refs-PL-6, all flagged by author for class lane) |

## 9. Disposition

**Pilot is NOT clean. Two concrete pilot-patches before load-gate:**

1. **F-refs-PL-1 (MAJOR):** Add `pl-inv-001 → bi-030` and `pl-inv-001 → wm-013a` per §2.5 source 4.
2. **F-refs-PL-7 (MAJOR):** Remove `pl-018 → ar-inv-007`, `pl-019 → ar-inv-007`, `pl-020a → ar-inv-007` per F-pilot-AR-r2-2 invariant-as-target exemption (keep only `pl-inv-002 → ar-inv-007`).

**Three discipline-patch candidates for synthesis triage:**

3. **F-refs-PL-6 (BLOCKER candidate):** Decide §3.2 cycle-break-named-obligation lane. Recommend (a) — add a `cycle-break-named-obligation` clause + tag — over (b) — informational-only.
4. **F-refs-PL-3 (MINOR class):** Tighten §3.1.3 wide-fanout tag firing language to make tag-omission a load-time inspection point.
5. **F-refs-PL-2 (MINOR local):** Tighten the pilot's forward-deferred log to use specific IDs (`forward:rc-inv-005`) where the source cite is specific.

The pilot is otherwise structurally clean: cross-spec target IDs all valid, no invented edges, no bidirectional cycles surviving, §2.11(d.2) PL→EV direction audit clean.
