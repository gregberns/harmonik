# 06 — Integration pass (cross-reference + contradiction check)

> INTEGRATION pass for kerf work `dot-hardening`. Checks the three drafts
> (`workflow-graph.md`, `execution-model.md`, `handler-contract.md`) against each other AND against the
> whole unchanged `specs/` corpus. Every claim cites `file:line`. Verdict at the end.

---

## (a) Cross-reference checks performed + results

**Anchor validity (draft insertion points exist and match):** all confirmed.
- WG new IDs WG-055..WG-060 continue above the WG-054 high-water (`specs/workflow-graph.md:205`). ✓
- EM anchors: EM-055 step 6 = `specs/execution-model.md:1581`; B↔E composition contract L1584–1590 (`:1590` `buildAgentTaskContent`); EM-056 clause 4 = `:1610`; EM-012b-NODE = `:295`; precedence block `:297–306`; EM-015d-RFD `:366`, EM-015d-RIA `:386`; §6.1 anchors all present. ✓
- HC anchors: HC-006a trailing paragraph (`specs/handler-contract.md:249`), HC-055a "Value opacity"/"Translation to argv" (`:910`/`:914`), HC-069/070/071 (`:143/163/182`), HC-035/036/038, HC-051/052 all present. ✓

**External handle resolution (referenced targets exist):**
- WG-003 open-set (`workflow-graph.md:99`), WG-013 dialect (`:304`), WG-014 LHS (`:332`), WG-031a (`:534`), WG-050 review-floor, CHB-028 all resolve. ✓
- EM-032 (`execution-model.md:637`), EM-055 (`:1570`), EM-056 §7.5.2 (`:1601`), EM-012b §4.3 (`:255`), §7.5.3 validator (`:1618`) resolve. ✓
- scenario-harness S07 exists and owns twin/harness mechanics (`scenario-harness.md`); HC-038 defers to it correctly. ✓
- agent-input AIS-001/003/011, process-lifecycle PL-021b/PL-021d all resolve (relevant to the C1 conflict below). ✓
- EM-069 / EM-069-MAN are NEW clauses landed by the EM draft; WG and HC point at them → resolve within the same `kerf finalize`. ✓

**Dangling / wrong cross-refs found in the drafts (3 — must fix in Tasks pass):**
- **CR-1.** WG draft cites `[execution-model.md §4.3 EM-056]` at `05-spec-drafts/workflow-graph.md:53` and `:83`. **EM-056 lives in §7.5.2** (`execution-model.md:1601`); §4.3 is EM-012/EM-015d. The intended target is the feedback back-edge channel → should be **§7.5.1 EM-069-FB** (or §7.5.2 EM-056). Wrong section label.
- **CR-2.** EM draft EM-012b-NODE rewrite cites the `model_locked` marker "per `[workflow-graph.md §4 WG-042]`" (`05-spec-drafts/execution-model.md:258`). **`model_locked` is defined in WG-059**, not WG-042 (WG-042 is per-node `model`/`effort`). Wrong clause.
- **CR-3.** EM draft EM-069-FB cites `verdict`/`notes` "typed per `[workflow-graph.md §4 WG-014]`" (`05-spec-drafts/execution-model.md:157`). **The verdict TYPE is WG-058 (§4)**; WG-014 is the §6 `preferred_label` LHS constraint. Wrong clause + section.

All other in-draft handles (EM-069, EM-069-MAN, EM-070, EM-071, WG-057, HC-072/073/074) resolve.

---

## (b) Contradictions with UNCHANGED specs + resolution

### Contradiction class 1 — model-precedence ladder flip leaves stale "tier-0 / highest-precedence" text (5 sites)

The EM draft REWRITES EM-012b-NODE so the per-node `model=`/`effort=` is a **soft default (tier 2)** BELOW per-bead escalation (tier 1) and per-run force (tier 0). Five UNCHANGED assertions still call it the **highest-precedence (tier-0)** input and directly contradict the flip:

1. `specs/workflow-graph.md:94` — WG-002 note bullet: "`model`/`effort` are the highest-precedence (tier-0) input…override the run-level default." The WG-002 draft amendment explicitly says "[remainder of the existing bullet unchanged]", so this stale sentence **survives the amendment**. Resolution: extend the WG-002 amendment to reword it to the soft-default framing.
2. `specs/workflow-graph.md:223` — WG-042 body: "overrides the run-level `(model, effort)` pair…**for that node's dispatch only**." False under the flip (escalation beats it). WG-042 body is NOT amended by the draft. Resolution: reconcile WG-042 body to "soft default; a per-bead escalation wins."
3. `specs/workflow-graph.md:236` — WG-042 porting note: "…direct `model="<alias>"` attribute (per §4 WG-042 and `[execution-model.md §4.3 EM-012b]` **tier 0**)." Resolution: drop "tier 0".
4. `specs/execution-model.md:1177` — §6.1 `Node.model` field note: "overrides the run-level ModelPreference.model for this node's dispatch." Stale under the flip. EM is a changed file but this line is **not** in the EM draft's amendment list. Resolution: add it to the EM Tasks-pass edit set.
5. `specs/examples/per-node-model-effort.md:30` — sidecar table: "Per-node `model`/`effort` is the **highest-precedence (tier-0)** input…override the run-level defaults." Resolution: reword; the whole sidecar's "override" framing needs the soft-default caveat.

(`specs/beads-integration.md:156` "highest-precedence input in the four-tier **workflow-mode** resolution chain" is about `workflow:<mode>`, NOT model/effort — **not** a conflict.)

### Contradiction class 2 — reviewer `prompt` "accepted-but-inert / silently ignored" posture is overturned (4 exemplar docs)

The WG-040 amendment makes the reviewer `prompt` the **live rubric source** (retiring "accepted-but-inert"). The normative clause `specs/workflow-graph.md:173` is handled by the draft, but four UNCHANGED exemplar/authoring docs still assert the old "ignored" posture:

6. `specs/examples/authoring-notes.md:144–145` — "The reviewer's brief is **always sourced from the review-target artifact**…the `prompt` value is **silently ignored**." Direct contradiction.
7. `specs/examples/README.md:413` — "reviewer-class nodes where it is **accepted-but-inert at v1** (pending `hk-sdnzj`)." Resolution: the reviewer-prompt bead is now realized by this work.
8. `specs/examples/sentry-triage-faithful.dot:16` — comment "prompt= on reviewer nodes…accepted-but-inert at v1."
9. `specs/examples/plan-to-shipped-faithful.dot:26` and `:97` — comments "prompt= on reviewer nodes accepted-but-inert at v1."

Resolution: update the exemplar comments + authoring notes to "reviewer `prompt` is the rubric source (WG-040 as amended)."

### Contradiction class 3 — C1 transport wording ("paste-inject") vs the landed AIS migration (draft-internal, high-value)

The drafts describe claude's resume-feedback delivery as **tmux paste-inject** referencing an on-disk file: HC-072 claude bullet + honesty caveat (`05-spec-drafts/handler-contract.md:36–39,53–56`), HC-074 honesty limit 1 (`:234–240`), EM-069-FB (`execution-model.md:159`), and the EM-015d carve-outs. But the **already-landed** M2 `agent-input-substrate` work made ALL daemon-run input delivery go through the AIS structured input port (`SubmitInput → Ack`) and **SUPERSEDED paste-inject for the daemon-run path**:
- `specs/execution-model.md:411` (EM-015d input-delivery note): review-loop resume "MIGRATES to AIS rather than being preserved."
- `specs/process-lifecycle.md:772` PL-021d "DEMOTED — superseded by AIS for daemon runs; retained for keeper + interactive nudge."
- `specs/agent-input.md:183` AIS-011 "tmux is observation-only on the structured-driver input path."

A dot-mode review loop is a daemon-run path, so its resume instruction must go through AIS, **not** paste-inject. Resolution: reword the drafts' claude transport as "an on-disk-file reference delivered via the AIS structured input port (`agent-input.md` AIS-001), **not** via `LaunchSpec.Args`." The **load-bearing point (file-reference vs argv) is unaffected** — the recorder taps `LaunchSpec.Args` and still cannot capture an AIS `SubmitInput`, so HC-074's honesty limit holds verbatim; only the mechanism noun changes. This touches the drafts (HC, EM) and adds `agent-input.md` as a cross-ref.

### Soft item (reference-covered, optional touch)

10. `specs/workspace-model.md:933–934` — WM entries describe `reviewer-feedback.iter-<N>.md` / `review-target.md` as review-loop artifacts "governed by `[execution-model.md EM-015d-RFD/RIA]`." Because EM-015d now carries the dot carve-out the drafts add, the reference stays valid and there is **no hard contradiction** (WM never says "dot MUST NOT write them"). Optional: add a one-line dot-mode-transport note to WM-933/934 for clarity.

### Non-conflicts confirmed (checked, clean)

`grep "MUST NOT produce"` — the only dot-mode hit is `execution-model.md:1610` (the EM-056 clause the draft REPLACES); the event-model/RETRY hits are unrelated. `preferred_label` example hits (README, `.dot` files) use uppercase `APPROVE/REQUEST_CHANGES/BLOCK`, **consistent** with the new WG-058 verdict enum. `context_keys` sites (`workflow-graph.md:340,534`; `handler-contract.md:1110`) stay the edge-LHS registry — the drafts keep `inputs`/`outputs` a separate namespace (WG-055/WG-002 note), no collision. `buildAgentTaskContent`/`buildReviewTargetContent` appear only in changelog prose, not live normative clauses outside the drafts' replacement scope.

---

## (c) Terminology consistency

- **"precedence order" (EM) vs "alias-catalog lookup" (HC)** — kept distinct per P1. HC uses "alias-catalog lookup" throughout HC-073/HC-055a and references EM's ladder as "the precedence order (EM owns it)"; EM uses "precedence order" throughout EM-012b-NODE and references the catalog step as "alias→concrete." Neither uses the bare word "resolution" for the other's concept. ✓ (Minor: EM §6.1 seal note says "after alias→concrete resolution" — descriptive, acceptable; could align to "alias-catalog lookup" but not a drift.)
- **"declared inputs/outputs", "single renderer", "node_model_seal", "verdict enum"** — used identically across all three drafts. ✓
- **`task_context` is ONE key** — stated consistently in WG-055 (`:59`), EM-069-MAN (`:144`), and the manifest `source_keys` rule. ✓
- No terminology drift blocking finalize.

---

## (d) Changelog accuracy (`05-changelog.md`)

- WG `0.3.1 → 0.4.0`, EM `→ v0.10.0`: match the per-draft fragments. ✓
- The **two non-additive items** (REPLACE EM-056 clause 4; REWRITE EM-012b-NODE + precedence block) are correctly called out. ✓
- Per-clause new/amended lists for WG, EM, HC match the drafts. ✓
- **Gaps:**
  - **HC has no version number** — the consolidated table (`05-changelog.md:12`) and the HC fragment both say "modified / modified" with no bump. HC needs a real revision-history version at finalize.
  - The "Open reconciliations" list (`05-changelog.md:44–47`) omits the **class-1 ladder-flip staleness** (WG L94/223/236, EM L1177, per-node-model-effort.md L30) and the **class-2 reviewer-inert exemplar staleness** (authoring-notes, README, two `.dot` files). Add these as downstream touches.
  - It also omits the **C1 paste-inject↔AIS reconciliation** and the **3 cross-ref fixes (CR-1/2/3)**.

---

## (e) Coherence assessment

The three drafts are **internally coherent and mutually consistent** — the WG↔EM↔HC handles (EM-069, EM-069-MAN, EM-056, EM-012b, WG-057, HC-072/073/074) line up, the ownership split (WG names I/O; EM owns precedence order + renderer + seal; HC owns alias-catalog lookup + transport) is clean, and terminology is pinned. The gaps are (1) a small set of stale assertions in **unchanged** specs left behind by two non-additive changes (the ladder flip and the reviewer-prompt promotion), (2) one wording conflict with the **already-landed** AIS migration, and (3) three fixable in-draft cross-refs. None reopens a locked decision; all are mechanical follow-ups the Tasks pass must schedule.

**Downstream spec touches (feeds the Tasks pass):**
- `specs/workflow-graph.md` — WG-002 bullet L94, WG-042 body L223 + porting note L236 (ladder flip).
- `specs/execution-model.md` — §6.1 `Node.model` note L1177 (ladder flip); + apply CR-1/CR-3 fixes; + reword paste-inject→AIS (EM-069-FB, EM-015d carve-outs).
- `specs/handler-contract.md` — reword paste-inject→AIS in HC-072/HC-074; add HC version number.
- `specs/examples/per-node-model-effort.md` — L30 + "override" framing (ladder flip).
- `specs/examples/authoring-notes.md` — L144–145 (reviewer prompt now rubric source).
- `specs/examples/README.md` — L413 (retire "accepted-but-inert"; `hk-sdnzj` realized).
- `specs/examples/sentry-triage-faithful.dot` — L16 comment.
- `specs/examples/plan-to-shipped-faithful.dot` — L26, L97 comments.
- `specs/workspace-model.md` — WM-933/934 (soft/optional dot-mode note).
- In-draft cross-ref fixes: CR-1 (WG `§4.3 EM-056`→§7.5.1 EM-069-FB), CR-2 (EM `WG-042`→WG-059 for `model_locked`), CR-3 (EM `WG-014`→WG-058 for verdict/notes type).
