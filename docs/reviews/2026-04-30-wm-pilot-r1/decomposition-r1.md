# WM Pilot r1 — Decomposition-Quality Review

`reviewer: decomposition-quality (§3.2 method)` — ran 2026-04-30 against `wm-pilot.md` v0.1.0 / `wm-pilot-data.yaml` v0.1.0 / `specs/workspace-model.md` v0.4.2 / `discipline.md` v0.9.

Pilot is the seventh in the corpus and the **first authored entirely under discipline v0.9**. Pre-load expectation per pilot author: §2.11(c.2) consumer→taxonomy direction should be right from the start (no HC-style retrofit). Sample sized 14 beads weighted toward complex per the prompt: **all 4 sensors** (WM-INV-001/002/003/005) + **all 5 schemas** (Workspace, LeaseLockFile, WorkspaceState, InterruptState, SessionMetadataSidecar) + **the umbrella + 1 sentinel-consumer rep** (`wm-error.taxonomy`, `wm-013a`) + 4 §4 reqs touching the F8b/git-state-machine/no-op-accept families (WM-016, WM-013b, WM-022, WM-036).

---

## Per-bead findings

### `wm-error.taxonomy` (§8 umbrella) — Q5 schema-completeness probe + edge-direction probe

**Q5: complete?** YES. The 12 typed sentinel classes all present, each with workspace-transition consequence + downstream routing matching spec §8 verbatim.

**§2.11(c.2) edge direction probe.** Every edge **into** the taxonomy bead (loaded as `<req> → wm-error.taxonomy`) runs consumer→owner per v0.9. **YAML edge count: 11**, not 13 as pilot doc §6 claims.

**Finding D-WM-1 [MAJOR · local lane]:** **Pilot doc §6 vs YAML edge-count discrepancy on consumer→taxonomy edges.** Pilot doc §6 asserts "13 consumer→taxonomy edges fire" and enumerates them. YAML loader-data has only **11** rows of `from: <consumer>, to: wm-error.taxonomy`. Missing from YAML: `wm-002 → wm-error.taxonomy` (RunIdReuseForbidden / WorkspaceAlreadyExists path-uniqueness consumer per pilot §6) and `wm-003 → wm-error.taxonomy` (WorktreeCreationFailed sentinel per pilot §6).
- Spec text check: WM-002 body declares the path convention but does not inline-cite any sentinel; WM-003 body declares `git worktree add -b` but does not cite WorktreeCreationFailed. Strict §3.1 step 1 + §2.11(c.2) reads "every §4 requirement whose body cites a sentinel name" — both reqs do NOT cite sentinels, so the YAML's 11 edges (excluding 002/003) is the spec-faithful read.
- Either the pilot doc §6 enumeration is wrong (13→11 patch) OR the YAML is missing 2 edges that should fire from the §7.2 `create_workspace` pseudocode-level ERROR returns. **Recommend pilot doc §6 is patched to "11" and the wm-002/wm-003 entries removed from the prose list.** The YAML is the authoritative loader-data and is internally consistent.
- Lane: `local` — application of §2.11(c.2) is sound; the prose enumeration drift is a doc-vs-data-bookkeeping bug.

### `wm-002` — Q1 description-spec match

**Match.** Description faithful: canonical path, local-clone-only, operator-config per CP-037, run_id regex.

### `wm-013a` (lease-lock atomic write) — Q1 description-spec match

**Match.** All four JSON fields present (run_id/pid/created_at/ttl_sec); atomic write + fsync gate before `workspace_leased`; OQ-WM-005 BLOCKING-CROSS-SPEC noted; `LeaseLockHeldByOrphan` sentinel-consumer edge fires.

### `wm-013b` (per-terminal-path release gates) — Q3 multi-step probe + F8b verification

**F8b application sound.** 4 paths (merged / failed-run / post-escalation / verdict-driven) are switch-branches inside `discard_workspace`/`complete_merge` sharing `release_lease_lock + write_workspace_local_marker` epilogue (per §7.2 pseudocode lines 893–921). Spec §7.2 makes the shared epilogue concrete; F8b correctly applied. **No split.**

### `wm-016` (`workspace_leased` 4-step ordering) — Q3 multi-step probe

**F8b application sound.** Steps (a)–(d) live in `launch_session` function body (per §7.2 pseudocode); shared cohesive control flow. Single bead with sub-bullets is correct. Description includes the "subsequent sessions do NOT re-emit" clarifier. ✓

### `wm-019a` (scratch merge-worktree 7-step lifecycle) — Q3 multi-step probe

**F8b application sound.** Spec §4.5 lifecycle (i)–(vii) is one cohesive merge-node function body. Single bead correct.

### `wm-013a` + `wm-026` (atomic-write 4-step) — Q3 multi-step probe

**F8b application sound.** Both share the `write_json_atomic_fsync` helper per §7.2 pseudocode. Single bead each.

### `wm-024` (5-bullet contract) — Q3 multi-step probe

**F8b application sound.** Single `redispatch_implementer_for_merge_conflict` function-body. Cap [1, 10] noted; description matches spec.

### `wm-013e` (gitignore write-or-fail) — Q3 multi-step probe

**F8b application sound.** Single function body at startup; `GitignoreWriteForbidden` sentinel routes to taxonomy edge. ✓

### `wm-022` (sidecar walk) — Q1 description-spec match

**Match.** Description correctly captures: reverse-chronological walk by `launched_at`, agentic-classes-only filter, NO trailer walk, retired `Harmonik-Actor-Role` reference, null fallback to WM-022a. Cognition-tag + axes (`llm-freedom-bounded`, `io-determinism-best-effort`, `replay-safety-unsafe`, `idempotency-non-idempotent`) all present. ✓

### `wm-022 + wm-022a` cluster — Q2 coalesce-rejection probe (F-pilot-WM-1a)

**Pilot author's three-AND test:** Test 1 fires (same identification path); Test 2 fires (anchor + clarification); **Test 3 fails** (WM-022a's "skip re-dispatch + emit `merge_conflict_escalation` directly" is its own testable escalation path distinct from WM-022's identification rule). Two-of-three insufficient → no coalesce. **Verified sound.** Same posture as F-em-r1-MIN-8 typed-alias cluster pattern. ✓

### `wm-037 + wm-037a` cluster — Q2 coalesce-rejection probe (F-pilot-WM-1b)

**Pilot author's three-AND test:** Test 1 fires (same enum/state machine); Test 2 fires (orthogonality + terminal bound); **Test 3 fails** (WM-037a's terminal-clearance + silent-reject of `InterruptOnTerminalWorkspace` is its own testable rule distinct from WM-037's enum + orthogonality declaration). Two-of-three insufficient → no coalesce. **Verified sound.** ✓

### `wm-036` (verdict 7-value classification + no-op-accept) — Q1 description-spec match

**Finding D-WM-2 [MAJOR · local lane]:** **Description omits a normative clause from the v0.4.2 `no-op-accept` row.** Spec WM-036's `no-op-accept` table row reads: "no workspace action; record the verdict; **clear any non-`none` `interrupt_state` per §4.10.WM-040 if the workspace was previously marked interrupted by reconciliation**; outer run continues...". Pilot bead description simplifies to "`no-op-accept` (v0.4.2 row) → no workspace action".
- The **interrupt-state-clearance side-effect** is normative per spec WM-036 + WM-040 cross-reference and would be missed by an implementer reading the bead description alone. (The interaction matters: if a Cat 6a reconciliation pass set `interrupt_state = daemon-crash-suspected` and the human investigator returns `no-op-accept`, WM-036 says the field must be cleared back to `none` per WM-040 — the bead-as-written does not say this.)
- Lane: `local` — the omission is a description-fidelity gap on this bead; v0.4.2 just landed and the bead description was not back-filled with the side-effect clause. The yaml's `wm-036 → forward:rc-NNN` covers verdict-routing dependency but NOT the WM-040 interaction.
- **Recommend** patch wm-036 description to add: "The `no-op-accept` row additionally clears any non-`none` `interrupt_state` back to `none` per WM-040 when reconciliation had previously marked the workspace interrupted (the v0.4.2 row codifies this resolution of OQ-RC-011)." Also add edge `wm-036 → wm-040` (term-use of WM-040 inside the no-op-accept row content).

### `wm-inv-001` (lease-by-run sensor) — Q4 sensor-mechanism + cross-spec edge probe

**Q4 mechanism.** Sensor names a real verification mechanism (correlate live lease-lock against Beads owning run_id + daemon live-run registry per RC §8.11 Cat 6). ✓ Real, not just restatement.

**Finding D-WM-3 [MAJOR · class lane]:** **Missing sensor→sensor edge `wm-inv-001 → ar-inv-007` per discipline §2.5 F-refs-EV-6 v0.8 extension.** Spec WM-INV-001 body explicitly cites AR-INV-007 by ID: *"It is the workspace-layer realization of the centralized-controller principle per `[architecture.md §4.9 AR-INV-007]`"*. Per §2.5 F-refs-EV-6, sensor→sensor `blocks` edges fire when an invariant body explicitly cites another invariant by `<prefix>-INV-NNN` ID. AR is in WM's `depends-on`. Pilot doc note ("AR-INV-007 term-use is sensor-only per F-pilot-AR-r2-2 invariant-as-target exemption; no edge") **mis-applies the exemption**: F-pilot-AR-r2-2 exempts `impl→invariant` edges (preserving §2.5 F12 sensor↔impl one-way), NOT `sensor→sensor` edges. The applicable rule for sensor↔sensor is F-refs-EV-6, which fires on explicit ID cite without an invariant-as-target carve-out.
- Discipline-author rule precedence: F-refs-EV-6 explicitly says *"the F-pilot-AR-r2-2 invariant-as-target exemption is impl→invariant-specific and does not fire for invariant→invariant"* (discipline.md line 132). Pilot's reasoning is reversed.
- **Recommend** add edge `wm-inv-001 → ar-inv-007` to the YAML cross-spec edges block (AR mnem-map already loaded). Update pilot §4 note for `wm-inv-001` to cite F-refs-EV-6 (not F-pilot-AR-r2-2) and emit the edge.
- Lane: `class` because **two specs at sensor-author time apparently misread the F-pilot-AR-r2-2 exemption as covering sensor→sensor** (this pilot does it; HC may be at risk too — recommend retrospective check). The discipline rule is sound but the cross-rule precedence between F-pilot-AR-r2-2 and F-refs-EV-6 is easy to invert. F-em-r1-MAJ-4 (rule precedence: invariant-as-target exemption beats supporting-cite test) covers a different rule pair; an analogous F-refs-EV-6-vs-F-pilot-AR-r2-2 precedence note in §2.5 would prevent the same misread on RC's pilot.

### `wm-inv-002` (one-in-flight-run-per-bead sensor) — Q4 sensor + four-source probe

**Q4 mechanism.** Sensor names a real Cat 6a detector. ✓

**Finding D-WM-4 [MAJOR · local lane]:** **Missing sensor→schema edge per §2.5 source 4 (F-em-r1-MAJ-1 invariant-body term-use sub-clause).** Spec WM-INV-002 sensor body uses the term `Harmonik-Run-ID` (a checkpoint trailer) and `Harmonik-Verdict-Executed` (also a checkpoint trailer). Both are owned by EM (§4.4 EM-017 trailer registry / `em-schema.checkpoint-trailers`). EM is in WM's `depends-on`; EM pilot is loaded. Per §2.5 source 4, **the sensor bead must `block-on` the trailer-registry schema bead**. YAML has zero edges from `wm-inv-002` to any EM schema or req.
- Worked example in discipline.md F-em-r1-MAJ-1: `em-inv-005` body uses `Harmonik-Bead-ID` (owned by `em-schema.checkpoint-trailers`); both fire as direct sensor predecessors. WM-INV-002 is the same shape: invariant body term-uses two trailer keys.
- **Recommend** add edge `wm-inv-002 → em-schema.checkpoint-trailers` (or to `em-017` if that is the pilot's chosen anchor for the trailer registry).
- Lane: `local` — discipline §2.5 source 4 is unambiguous; the edge was simply missed by the pilot author. (Possible class-lane angle: the v0.7 source 4 was authored for §10.2-empty / cross-`depends-on`-cite invariants per F-em-r1-MAJ-2; WM-INV-002's body terms ARE in `depends-on`, so the four-source rule should have fired through the standard path. No discipline-patch needed.)

### `wm-inv-003` (checkpoint append-only sensor) — Q4 + cross-spec edge probe

**Q4 mechanism.** Two-part sensor (Part A EM-024a runtime + Part B reflog-walking auditor-tool) is a real verification mechanism. ✓

**Finding D-WM-5 [MAJOR · local lane]:** **Edge `wm-inv-003 → em-inv-001` fires without explicit ID cite and may not satisfy F-refs-EV-6.** YAML emits `wm-inv-003 → em-inv-001` with note *"em-inv-001 sibling-invariant cite (git is state-reconstruction source)"*. Spec WM-INV-003 body cites `[execution-model.md §4.5 EM-024a]`, `[execution-model.md §4.7]` (state reconstruction), and `[reconciliation/spec.md §8.4]` — but **does NOT explicitly cite `EM-INV-001`** by ID. F-refs-EV-6 requires "explicit `<prefix>-INV-NNN` ID" cite to fire a sensor→sensor edge.
- The cite is to a §4.7 *section anchor*, not to an invariant ID. Per §3.1 step 3, section-anchor cites become `cite:wide-fanout` edges to all reqs in the section, not edges to a specific invariant.
- Either (a) the pilot is correct that `em-inv-001` is the natural target and the discipline §2.5 F-refs-EV-6 should be widened to cover sibling-invariant section-anchor cites (class-lane patch), or (b) the edge is invented and should be removed (local-lane patch).
- Recommend **author-decides:** pilot §8 F-pilot-WM-7 already documents this term-use chain; if the author considers EM-INV-001 the de-facto anchor of "git is state-reconstruction source," then F-refs-EV-6 needs a clarifying clause about section-anchor sibling-invariant cites; if the term "git state-reconstruction source" is best routed via `em-014` or a §4.7 EM req bead instead, the edge should retarget there.
- Lane: `local` (defaults to local; can be re-tagged class if the author flags F-refs-EV-6 ambiguity).

### `wm-inv-005` (canonical-path-discovery sensor) — Q4 sensor probe

**Q4 mechanism.** Sensor body says *"WM-013c filesystem discovery by construction"* — this is partly tautological (the sensor IS WM-013c). Spec §5 acknowledges this *"any workspace whose path does not match `<repo>/.harmonik/worktrees/<run_id>/` for some recognized `run_id` is a violation, detected by the startup enumeration pass and routed to reconciliation Cat 3c (inverse premature-close) or Cat 6a (integrity violation)"*. The non-tautological piece is the violation-classification (Cat 3c vs Cat 6a). Pilot description carries this. **Acceptable** — borderline restatement but the routing (Cat 3c / 6a) is real.

Pilot note "sensor entirely intra-WM... RC routing is consequential, not edge-emitting" is consistent with §3.1 step 3's section-anchor-without-specific-req treatment. ✓

### `wm-schema.workspace` — Q5 field-list completeness

**Match.** All 12 fields present; type aliases (CommitSHA, BeadID, HandlerRef, UUID) cited from owning specs per §6.1. ✓

### `wm-schema.lease-lock-file` — Q5

**Match.** 4 fields complete. Cross-spec OQ-WM-005 BLOCKING-CROSS-SPEC noted. ✓

### `wm-schema.workspace-state` — Q5

**Match.** 7 values complete; retired `setup` flagged. ✓

### `wm-schema.interrupt-state` — Q5

**Match.** 5 values complete. ON-011 / RC §8.11 vocabulary mapping noted. ✓

### `wm-schema.session-metadata-sidecar` — Q5

**Match.** 7 fields complete. ✓

### Missing-coalesce smell scan

WM has these prefix-clusters:
- WM-013a..e (5 sub-letters) — each has a distinct mechanism (lock content vs release vs discovery vs anti-reuse vs gitignore). No coalesce justified.
- WM-018, 018a, 019, 019a, 020, 021 (merge-back family) — each a distinct mechanism.
- WM-037 + 037a (interrupt-state) — already considered + rejected per F-pilot-WM-1b.
- WM-038 + 038a (interrupt-state writer + marker) — distinct mechanisms (sole-writer rule vs JSONL marker write); no coalesce.

**No missing-coalesce smells detected.**

### Over-split smell scan

**No over-split smells.** WM has 0 multi-step splits and 0 step beads (per F-pilot-WM-3 F8b application across 7 candidate sequences). The 71-bead total is reasonable for 54 §4 reqs (1.31× multiplier, between HC's 0.95× and CP's 1.55×).

### §2.11(c) threshold check (F-pilot-WM-2 SHAPE-vs-COUNT)

**Pilot's reasoning sound.** WM has 12 sentinel classes (just above RC's "11 figure"). Per §2.11(c), WM is **BI-shape (flat typed-sentinel set)**, not RC-shape (multi-stage classification with §8.1–§8.12 sub-section structure). Single-bead `wm-error.taxonomy` is correct.

**Finding D-WM-6 [MINOR · class lane]:** Pilot author flags F-pilot-WM-2 as a class-lane discipline-patch candidate ("the discipline doc could clarify that the ~11 figure is descriptive-of-RC-not-prescriptive"). Concur — this would help RC's pilot draft (and any post-RC author who hits 9-12-class taxonomies). Low priority since the SHAPE test is mechanically applicable without doc clarification. Recommend bundling into the next §2.11(c) edit.

---

## Summary

| Severity | Count |
|---|---|
| BLOCKER | 0 |
| MAJOR | 5 (D-WM-1, D-WM-2, D-WM-3, D-WM-4, D-WM-5) |
| MINOR | 1 (D-WM-6) |

**Lane breakdown:** 4 `local` (D-WM-1, D-WM-2, D-WM-4, D-WM-5) · 2 `class` (D-WM-3, D-WM-6).

**Headline concerns.**
1. **D-WM-3 (class):** F-pilot-AR-r2-2 vs F-refs-EV-6 rule-precedence misread led to a missing `wm-inv-001 → ar-inv-007` sensor→sensor edge. Discipline-patch candidate (analogous to F-em-r1-MAJ-4 rule precedence note).
2. **D-WM-1 (local):** Pilot doc §6 claims 13 consumer→taxonomy edges; YAML has 11. Doc-vs-data drift on `wm-002` and `wm-003`. YAML appears spec-faithful; recommend doc patch.
3. **D-WM-2 (local):** WM-036 description omits the `no-op-accept` interrupt-state-clearance side-effect introduced in v0.4.2; this would silently miss the WM-040 interaction at impl time.
4. **D-WM-4 (local):** Missing sensor→schema edge `wm-inv-002 → em-schema.checkpoint-trailers` per §2.5 source 4 (F-em-r1-MAJ-1).
5. **D-WM-5 (local):** `wm-inv-003 → em-inv-001` fires on a §4.7 section-anchor cite, not an explicit `EM-INV-001` ID cite. Either retarget the edge or surface F-refs-EV-6 ambiguity.

The §2.11(c.2) consumer→taxonomy direction is correctly applied: 11 of 11 YAML edges run consumer→owner, no inverted edges, no LOAD-time cycles in the sentinel chain. **First pilot under v0.9 cleared the §2.11(c.2) anti-pattern.**

The 0-coalesce / 0-multi-step decisions (F-pilot-WM-1, F-pilot-WM-3) are sound on three-AND and F8b respectively.

Schema beads (5/5) all complete with full field/enum lists matching spec §6.1. Sensor beads name real verification mechanisms (3/4 strong; WM-INV-005 is borderline restatement but acceptable given the routing-classification content).

Recommend re-running the **decomposition reviewer only** after the 5 MAJOR findings are addressed; structural shape (counts, F8b/F12, schema completeness) is sound.
