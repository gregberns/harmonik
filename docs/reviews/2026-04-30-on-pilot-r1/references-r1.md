# ON Pilot r1 — Reference Reviewer Findings

`reviewer: reference / r1` — drafted 2026-04-30 against `specs/operator-nfr.md` v0.4.1 (995 lines), `docs/decompose-to-tasks/on-pilot-data.yaml` (1653 lines), `docs/decompose-to-tasks/on-pilot.md` v0.1.0, and discipline v0.9 (§2.7, §3.1, §3.2, §2.11(c.2), §2.11(d.2), §3.1.3 wide-fanout). Self-contained.

ON's `depends-on`: `[architecture, event-model, execution-model, handler-contract, control-points, process-lifecycle, reconciliation, beads-integration]`. AR / EM / EV / HC / CP / PL / BI mnem-maps loaded; RC forward-deferred. WM intentionally excluded.

Yaml emits 271 resolved cross-spec / intra-ON edges + 13 forward-deferred RC edges = 284 total, by kind: cross-ar=8, cross-em=7, cross-ev=37 (ev-events), cross-hc=8, cross-cp=12, cross-pl=18, cross-bi=10, intra-ON ~80, sensor / test-infra ~62.

## 1. Verdict

**MOSTLY CLEAN with two MAJOR-class findings, three MINOR findings, and one sharpened correction to F-pilot-ON-6.** No invented hard-cycle edges; no depends-on violations; no bidirectional intra-cite cycles. 19 consumer→`on-error.taxonomy` direction confirmed correct per §2.11(c.2). 37 ON→EV-row edges confirmed correct per §2.11(d.2). Multi-step ON-027 split (8 step beads) sequenced correctly with no step→umbrella explicit edges (per F11). F-pilot-ON-6's WM-as-informational classification holds for the substantive cites, but the count and the routing of cite #1 are inaccurate.

## 2. Findings

### 2.1 MAJOR — Missed cross-spec edge: `on-025 → ev-events.skills-provisioned`. [local]

**Where.** ON-025 body (spec line 343–344) inline-cites `[event-model.md §8.3]` for the `skills_provisioned` event ("handler MUST emit a `skills_provisioned` event (per [event-model.md §8.3]) listing only the skills actually installed"). The pilot's per-requirement table row for `on-025` (pilot.md §4.7) lists `ev-events.skills-provisioned` in its `blocks edges` column. **The yaml `edges:` section at lines 1559–1591 does NOT emit `{from: on-025, to: ev-events.skills-provisioned}`.** ON-ENV-001 enumerates events produced/consumed but `skills_provisioned` is not in the produced list (correct — it is HC-emitted, ON-25 obligates the consequence) and not in the consumed list (omitted there too).

**Why MAJOR.** Per §2.11(d.2), every ON normative cite to an EV §8.x event row fires a `<on-NNN> → <ev-events.<name>>` edge. `ev-events.skills-provisioned` is a real EV map row (`hk-hqwn.59.28`). The pilot table promises this edge; the yaml omits it; load will not reflect the WHEN-rule.

**Lane.** `local` — the discipline rule is unambiguous; the omission is a transcription error from pilot table → yaml.

**Severity.** MAJOR (missed edge, real dep, target loadable).

**Patch.** Add: `- {from: on-025, to: ev-events.skills-provisioned}` to the EV cross-spec edges block, alongside on-046/on-048/on-049 → budget events.

### 2.2 MAJOR — Missed forward-deferred edge: `on-028 → forward:rc-NNN`. [local]

**Where.** ON-028 body (spec line 363–364) inline-cites `[reconciliation/spec.md §4.2]` ("In-flight run state is recoverable on next startup via checkpoint + reconciliation per [reconciliation/spec.md §4.2]"). The pilot's table row for `on-028` (pilot.md §4.7) lists `forward:rc-NNN` in its `blocks edges` column with note "in-flight run state recoverability via reconciliation post-`stop --immediate`." Pilot §3.2 narrative also enumerates `on-028 → forward:rc-NNN` in its RC forward-deferral list. **The yaml `edges:` section at lines 1631–1645 does NOT emit a forward edge from `on-028`.**

**Why MAJOR.** RC §4.2 is a real cite; the cite is hard-dep (removing reconciliation breaks ON-028's recoverability claim). When the RC pilot loads and reciprocal-pilot resolution materializes RC anchor-IDs, this forward will be silently absent.

**Lane.** `local` — the cite is unambiguous; the omission is a yaml drafting gap. The pilot's §3.2 narrative claims "~12 RC forwards" (yaml has 13, table promises 14 with on-028 included). Routine tally drift, but the underlying edge is missing.

**Severity.** MAJOR.

**Patch.** Add: `- {from: on-028, to: "forward:rc-NNN"}` (RC §4.2 reconciliation classification on aborted runs) to the forward-deferred block.

### 2.3 MINOR — Two invented forward-deferred RC edges: `on-040 → forward:rc-014` and `on-045 → forward:rc-018`. [local]

**Where.**
- **ON-040** body (spec line 472–476) inline-cites `[handler-contract.md §4.6]`, `[event-model.md §8.3]`, `[handler-contract.md §7.1]`, and intra-spec `§4.9.ON-037` / `ON-029` / `ON-027`. **No `[reconciliation/spec.md` cite** in the body. Yaml emits `{from: on-040, to: "forward:rc-014"}` with comment "silent-hang routes through reconciliation."
- **ON-045** body (spec line 538–540) inline-cites `[control-points.md §4.5]` (×2) and intra-spec `§4.10.ON-042`. **No `[reconciliation/spec.md` cite** in the body. Yaml emits `{from: on-045, to: "forward:rc-018"}` with comment "reconciliation-workflow budget."

**Why MINOR.** Per discipline §3.1 step 1 + F-pilot-AR-10 supporting-cite test, an edge requires a load-bearing cite in normative prose. Both forwards rely on inferred cross-spec coupling rather than an inline cite. They are plausible content links (silent-hangs do go through RC; reconciliation-workflow budgets do live in RC §4.4) but the SOURCE CITES live in OTHER reqs (ON-009 already covers the silent-hang→RC routing via `forward:rc-014`; ON-047 + ON-048 already cover the RC-018 budget linkage). These two are non-load-bearing duplicates.

**Lane.** `local` — pilot author over-emitted; discipline unambiguous on supporting-cite-vs-hard-dep.

**Severity.** MINOR (inflates forward-deferred count by 2; corpus-lint will surface them as unsourced when RC loads).

**Patch.** Remove `on-040 → forward:rc-014` and `on-045 → forward:rc-018` from the forward-deferred block. Pilot §3.2 narrative count of "~12 RC forwards" then matches the corrected yaml exactly.

### 2.4 MINOR — `cite:wide-fanout` tag count inconsistent. [local]

**Where.** Pilot §3 narrative (and finding F-pilot-ON-9) says "Total: 4 cite:wide-fanout tags applied (on-008, on-013, on-027, on-038)." Yaml top-comment block at lines 491–510 echoes the same "4 tags" claim. Yaml `extra_labels` blocks ACTUALLY apply **6** `cite:wide-fanout` tags: `on-008` (line 637), `on-013` (line 677), `on-020` (line 753), `on-027` (line 821), `on-038` (line 1014), `on-041` (line 1039).

**Why MINOR.** The two extra tags (on-020, on-041) are arguably correct emissions. ON-020 cites `[process-lifecycle.md §4.9 PL-027(iii)]` (single anchor, not fanout) but ON-020 also cites `[handler-contract.md §4.10]` informationally (single anchor). The fanout claim on ON-020 / ON-041 is borderline. Either the narrative undercount is wrong, or the yaml over-tagged.

**Lane.** `local` — bookkeeping drift between narrative and yaml.

**Severity.** MINOR (informational tag; does not affect load).

**Patch.** Author-discretion: either narrative count → 6, or audit on-020 / on-041 fanout claims and demote the tag if not warranted.

### 2.5 MINOR — F-pilot-ON-6 WM enumeration is undercount + one mis-route. [local — sharpening, not a bug]

**Where.** Pilot §3.3 enumerates 6 WM cites in ON normative prose. **Direct grep against ON spec §4 / §5 yields 7 cites:**

1. **ON-ENV-001 (§4.a)** — spec line 113: "Workspace record per `[workspace-model.md §6.1]`" (Types-introduced declaration; declarative).
2. **ON-015 (§4.4)** — spec line 265: "session-log bead-ID metadata per `[workspace-model.md §4.7]`" (substantive — N-1 readability of the overlay extends to WM-owned metadata).
3. **ON-024 (§4.7)** — spec line 338: "leased workspace directory per `[workspace-model.md §4.3]`" (substantive — sandbox enforcement).
4. **ON-027 step 6** — spec line 357: "workspace manager unlocks leased workspaces ... per `[workspace-model.md §4.3]`" (substantive — drain step body).
5. **ON-030a** — spec line 393: "temp+rename+fsync+parent-fsync per `[workspace-model.md §4.7 WM-026]`" (substantive — atomic-write discipline).
6. **ON-053** — spec line 425: "atomicity discipline of `[workspace-model.md §4.7 WM-026]`" (substantive — atomic-write discipline).
7. **ON-INV-003 sensor** — spec line 605: "session log per `[workspace-model.md §4.7]`" (substantive — sink enumeration in joint-hold invariant).

The pilot's enumeration says cite #1 is "on-022 §4.7 — redaction-sink list cites session log per `[workspace-model.md §4.7]`." **ON-022 body (spec lines 322–326) contains NO inline `[workspace-model.md` cite.** The session-log redaction-sink cite the pilot is referring to lives in **ON-INV-003 sensor (a)** at spec line 605, which the pilot also lists separately as cite #6. So the pilot's cite #1 is mis-routed — it should be either ON-015 (§4.4 session-log bead-ID metadata) or fold into #6 (ON-INV-003).

Net the pilot **misses 2 cites** (ON-ENV-001 §6.1 Workspace-record; ON-015 §4.4 session-log) and double-counts ON-INV-003.

**F-pilot-ON-6 verdict (substantive task spot-check).** Are the WM cites genuinely informational or borderline / load-bearing?

| Cite | Substantive? | Verdict |
|---|---|---|
| ON-ENV-001 (Workspace record type) | Declarative — Types-introduced says "none. References existing types ..." | Informational. Type ref only. |
| ON-015 (session-log bead-ID metadata) | **Substantive** — "Both halves MUST be N-1 readable" extends to the WM-owned half | Borderline / load-bearing. Removing WM §4.7 metadata semantics would break ON-015's compat claim. |
| ON-024 (sandbox invariant) | **Substantive** — workspace leasing is the sandbox boundary | Load-bearing. Removing WM §4.3 lease semantics breaks the sandbox invariant. |
| ON-027 step 6 | **Substantive** — workspace unlock IS the drain step body | Load-bearing. Step 6 has no body without WM. |
| ON-030a (atomic-write WM-026) | **Substantive** — temp+rename+fsync+parent-fsync atomicity is WM-owned discipline | Load-bearing. Marker durability semantics are WM. |
| ON-053 (atomic-write WM-026) | **Substantive** — same atomicity discipline | Load-bearing. Forensic-file durability is WM. |
| ON-INV-003 sensor (session log path) | **Substantive** — joint-hold invariant enumerates WM as one of three sinks | Load-bearing. Removing WM-owned session log breaks the joint hold. |

**6 of 7 are load-bearing.** F-pilot-ON-6's pilot-author estimate ("3 of 6 substantive") is conservative; the actual ratio is 6/7. The class-finding's directional claim — **WM should join ON's `depends-on` in the next ON revision** — is strengthened, not weakened, by the corrected accounting.

The discipline classification (F-pilot-EV-3 informational baseline because WM is OUT of `depends-on`) is correct; the pilot's correct mechanical action (no `forward:wm-*` edges emitted; mnem-map deliberately omitted from `cross_specs`) is unchanged. The finding lane stays `class` (inverse-direction sibling of F-pilot-PL-4).

**Lane.** `local` for the count fix (this pilot's bookkeeping); the class-lane signal F-pilot-ON-6 itself is unchanged.

**Severity.** MINOR (the corrective accounting strengthens F-pilot-ON-6, does not invalidate it; no load impact).

**Patch.** Pilot §3.3 enumeration: replace 6 cites with 7 cites (add ON-ENV-001 §6.1 + ON-015 §4.7; remove the spurious ON-022 entry; keep ON-INV-003 entry once). Pilot §1 / spec-summary phrasing of "six WM cites" → "seven WM cites" everywhere it appears (front-matter narrative, F-pilot-ON-6 finding body). Class-lane finding text: amend "at minimum three of the six (ON-024 sandbox; ON-030a atomic-write; ON-053 atomic-write) are substantive" to "six of the seven cites are load-bearing" — strengthens the case for adding WM to depends-on without changing the discipline-lane classification.

## 3. Direction-sanity confirmations

### 3.1 19 consumer→`on-error.taxonomy` edges — direction CORRECT.

Yaml emits exactly 19 edges with target `on-error.taxonomy` (lines 1262–1408 across §4 and §5). Every emission is consumer-to-vocabulary-owner per §2.11(c.2):

`on-001`, `on-002`, `on-003`, `on-013`, `on-016`, `on-019`, `on-020`, `on-020a`, `on-027.s4`, `on-027.s5`, `on-027.s6`, `on-027.s7`, `on-027`, `on-029`, `on-031`, `on-053`, `on-041`, `on-048`, `on-inv-005` → `on-error.taxonomy`.

No edge runs the inverse direction. The taxonomy bead has no §4 req predecessors. F-pilot-HC-direction anti-pattern not present.

> Note: pilot doc §6 narrative says "16 intra-ON consumer edges"; yaml has 19. The 3-edge gap is the sensor edge (`on-inv-005`) plus the ON-027 step beads (`on-027.s4..s7`) which the narrative did not enumerate but are mechanically correct emissions. Bookkeeping drift, not a load issue.

### 3.2 37 ON → EV-row edges — direction CORRECT per §2.11(d.2).

Yaml emits 37 edges with target `ev-events.<name>` (lines 1513–1557). Every emission is consumer/emitter→event-row (ON's WHEN-rule cites EV's payload-shape; §2.11(d.2) direction `<on-NNN> → ev-events.<name>`).

Breakdown:
- **on-env-001 → ev-events.{8 produced + 6 consumed}** = 14 edges.
- **on-013 → ev-events.{8 operator-events}** = 8 emission edges.
- **on-005 → ev-events.operator-upgrade-rejected** = 1.
- **on-033 → ev-events.{daemon-ready, daemon-shutdown}** = 2.
- **on-022 → ev-events.redaction-failed** = 1.
- **on-040 → ev-events.agent-warning-silent-hang** = 1.
- **on-046 → ev-events.{warning, accrual, exhausted}** = 3.
- **on-048 → ev-events.{exhausted, dispatch-deferred}** = 2.
- **on-049 → ev-events.{warning, accrual, exhausted}** = 3.
- **on-010 → ev-events.reconciliation-category-assigned** = 1.
- **on-003 → ev-events.daemon-startup-failed** = 1.

No reverse-direction emissions. Per §2.11(d.2), EV's reciprocal direction is owned by EV's pilot — checked in /tmp/ev-mnem-map.csv to confirm every target row exists; all 12 distinct ev-events targets resolve.

### 3.3 ON-027 step bead sequencing — CORRECT per §2.2 F11.

8 step beads (`on-027.s1` … `on-027.s7` plus `on-027.s3a`) parented to umbrella via `--parent on-027` (parent-child auto-edge per F11). Inter-step `blocks` edges (s2→s1, s3→s2, s3a→s3, s4→s3a, s5→s4, s6→s5, s7→s6) emitted at yaml lines 1319–1325. **No explicit step→umbrella `blocks` edges** (would create cycle per F11). F8c constraints `on-027a` and `on-029` emit `blocks` edges to umbrella correctly.

### 3.4 Bidirectional cite cycles — NONE.

Yaml comment blocks at lines 1276–1281 (on-011 ↔ on-030a), 1330–1335 (on-027 ↔ on-008), 1350–1353 (on-031 ↔ on-032) explicitly remove reverse edges per F-pilot-AR-10 supporting-cite test + F13 slot-rule heuristic. Verified: each removed direction is `supporting`, the kept direction is `hard-dep`. ON-INV-001 / ON-INV-003 / ON-INV-005 / ON-INV-006 sensors emit only impl→sensor reverse-blocked direction; F12 sensor↔impl one-way preserved; F-pilot-AR-r2-2 invariant-as-target exemption applied (no impl→invariant cites).

`on-017 → on-015` correctly NOT emitted (ON-017 body cites BI §4.2 and external-inputs protocol; no inline cite to ON-015 by ID; topical adjacency does not warrant invented edge per §3.1). Documented in yaml comment block 1294–1296.

### 3.5 Forward-deferred edges — 13 emitted; 1 missed (ON-028); 2 invented (ON-040, ON-045).

After the §2.2 / §2.3 patches, the corrected forward-deferred set is **12 edges**:

`on-003 → forward:rc-012` · `on-009 → forward:rc-014` · `on-010 → forward:rc-002` · `on-014 → forward:rc-018` · `on-014 → forward:rc-025` · `on-027.s2 → forward:rc-002` · **`on-028 → forward:rc-NNN` (ADD)** · `on-030 → forward:rc-008` · `on-032 → forward:rc-018` · `on-047 → forward:rc-018` · `on-048 → forward:rc-018` · `on-inv-005 → forward:rc-008`.

Removed: `on-040 → forward:rc-014` (no source cite in ON-040 body) · `on-045 → forward:rc-018` (no source cite in ON-045 body).

Each kept forward target is RC-section-cited in ON normative prose:
- rc-002 ← RC §4.1 (ON-010), RC §4.2 (ON-027.s2 derived; OK)
- rc-008 ← RC §4.2 (ON-030, ON-INV-005)
- rc-012 ← RC §4.3 (ON-003)
- rc-014 ← RC §4.3 (ON-009), RC §4.2 (ON-028)
- rc-018 ← RC §4.4 (ON-014, ON-032, ON-047, ON-048)
- rc-025 ← RC §4.5 (ON-014)

All RC anchors map to real RC §4.x subsections per the RC spec's expected layout; reciprocal-pilot resolution will materialize concrete RC anchor-IDs.

### 3.6 ON-ENV-001 voluntary envelope — produced/consumed events match spec body.

ON §4.a Subsystem envelope (spec lines 91–134) declares 8 events produced and 4 events consumed. Yaml lines 1514–1527 emit edges to the same 8 produced (operator-pause-status, operator-resuming, operator-stopped, operator-upgrading, operator-upgrade-completed, operator-upgrade-rejected, operator-command-rejected, dispatch-deferred) and 4+2 consumed (daemon-ready, reconciliation-category-assigned, budget-warning, budget-exhausted, budget-accrual, agent-warning-silent-hang). Match exact (consumed adds `budget-accrual` per §4.11 attribution aggregation cite, which is normative, not envelope-decorative).

## 4. F-pilot-ON-5 ON-027 step bead spot-check — edges resolve correctly.

Reviewer's narrow remit per task: confirm the 8 step beads' edges are mechanically correct.

| Step | Internal edges | Cross-spec edges | Forward-deferred | Verdict |
|---|---|---|---|---|
| s1 | (parent) | `bi-007` (`br ready` query loop), `pl-013` (queue-empty parallel) | — | ✓ matches body |
| s2 | s2→s1 | `em-017` (checkpoint), `em-017` (also via on-027.s2 again — checked, single edge) | `forward:rc-002` (ordering) | ✓ |
| s3 | s3→s2 | `hc-011` (watcher), `hc-018` (SIGTERM→SIGKILL) | — | ✓ |
| s3a | s3a→s3 | `bi-029`, `bi-030`, (note: yaml omits bi-031 — see below) | — | ⚠ minor — body cites BI-031 status-check classification but yaml has only bi-029 + bi-030. BI-031 is a sentinel-class beat per the pilot table notes. **MINOR missed cite — bi-031 would resolve to `bi-error.taxonomy` consumer per §2.11(c.2) but ON's edge is already to bi-030/bi-029 which carry BI-031 in their bodies. Borderline.** |
| s4 | s4→s3a | `ev-015` (JSONL durable form per EV §4.4), `ev-events.agent-warning-silent-hang`, `on-error.taxonomy` (codes 11/21) | — | ✓ |
| s5 | s5→s4 | `on-error.taxonomy` (code 21) | — | ✓ (no cross-spec — memory subsystem owns its own) |
| s6 | s6→s5 | `on-error.taxonomy` (code 21) | — | ✓ (WM cite informational per F-pilot-ON-6) |
| s7 | s7→s6 | `on-error.taxonomy` (codes 11/0), `on-008` (drain-completion gates pausing→paused) | — | ✓ |

Step sequencing chain s1→s2→s3→s3a→s4→s5→s6→s7 is monotone-correct; no skip; no reverse. `on-027 → on-error.taxonomy` umbrella edge (code 11 fanout) is redundant with s4/s5/s6/s7's per-step edges but not incorrect; mild duplication.

**Step bead spot-check verdict.** Edges resolve correctly with one minor cite-omission (`bi-031` cite not directly emitted from `on-027.s3a`; BI-031 is the status-check rule, citable separately; pilot author chose to subsume it under the bi-029/bi-030 anchors). Not a finding for this review's lane; downstream BI consumer linkage is clean.

## 5. Severity / Lane summary

| # | Finding | Severity | Lane | Action |
|---|---|---|---|---|
| 2.1 | Missed: `on-025 → ev-events.skills-provisioned` | MAJOR | local | Pilot patch — add edge before load |
| 2.2 | Missed: `on-028 → forward:rc-NNN` | MAJOR | local | Pilot patch — add forward edge |
| 2.3 | Invented: `on-040 → forward:rc-014`, `on-045 → forward:rc-018` | MINOR | local | Pilot patch — remove 2 forwards |
| 2.4 | `cite:wide-fanout` count drift (4 narrative vs 6 yaml) | MINOR | local | Author-discretion |
| 2.5 | F-pilot-ON-6 enumeration: 6 cited → 7 actual; 1 mis-routed | MINOR | local | Pilot patch — fix enumeration; class-finding strengthens |

**No BLOCKERs.** **No depends-on violations.** **No bidirectional intra-cite cycles.** **No invented hard-dep edges.** **F-pilot-ON-6 informational classification holds; the corrected accounting strengthens (does not weaken) the class-lane signal that WM should join ON's `depends-on` in the next ON revision.**

The two MAJORs (2.1, 2.2) are mechanical pilot-patch fixes — the cites both exist in the spec body and the pilot table promises both edges; the yaml drafter simply missed them. After patches, this pilot is ready for r1 synthesis triage and load.

## 6. Cross-references

- Coverage reviewer output: `coverage-r1.md` (sibling file) — cross-check requirement-ID coverage and tally arithmetic.
- Decomposition-quality reviewer output (separate spawn) — cross-check F-pilot-ON-5 ON-027 split decision (F8b deviation from PL precedent).
- Pilot synthesis: `synthesis.md` (to be drafted) — combines findings, applies §4 triage probes, decides lanes.
