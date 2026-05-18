# Workflow Modes — Integration

Integration-pass audit across the seven drafted specs in `05-spec-drafts/` against each other and against the non-modified system specs (`specs/architecture.md`, `specs/control-points.md`, `specs/scenario-harness.md`, `specs/reconciliation/*.md`).

---

## 1. Cross-reference audit

| Cite (from → to) | Target anchor | Exists? | Notes |
|---|---|---|---|
| operator-nfr → event-model §8.1a events | §8.1a.1–8.1a.6 (six review-loop events) | YES | All six events present in EV draft §8.1a table (lines 107–112). |
| operator-nfr ON-009a → beads-integration §4.3 `needs-attention` label | BI-009a in §4.3 (label encoding) | YES | BI §4.3 owns the `workflow:<mode>` label; `needs-attention` label semantics surfaced in BI-013a (§4.5) — ON-009a's `[beads-integration.md §4.3]` cite is a label-locus cite (acceptable as it points to "Beads-managed data"); ON-009a also separately cites `§4.5` for the ready-work-exclusion rule. ✓ |
| operator-nfr ON-009a → handler-contract §4.2 HC-006 (BLOCK verdict emission) | HC-006 is LaunchSpec record, not reviewer-phase emission | WEAK | HC-006 names the `phase` field but does not describe BLOCK emission per se. The reviewer verdict is actually conveyed via `outcome_emitted` + `.harmonik/review.json` per WM-027a + event-model §8.1a.3. **DEFERRED** (tasks-pass bead: re-target ON-009a's HC cite to WM-027a + event-model §8.1a.3). |
| execution-model EM-015d/e → event-model events | §8.1 (six occurrences) | FIXED inline | EM cited `§8.1` for the six review-loop events; correct anchor is `§8.1a`. **RESOLVED** by Edit: lines 246, 259, 261 retargeted to `§8.1a`. |
| execution-model changelog → event-model §8.8 for `bead_label_conflict` | §8.8.6 | FIXED inline | Changelog cited `§8.8`; correct is `§8.8.6`. **RESOLVED** by Edit. |
| beads-integration BI-009a → event-model §8.8.6 (`bead_label_conflict`) | §8.8.6 in EV draft | YES | Row 8.8.6 present in EV §8.8 taxonomy table (line 256). |
| workspace-model WM-027a → beads-integration BI-013a (needs-attention exclusion) | BI-013a in BI §4.5 | YES — but WM-027a does not cite BI-013a directly; it cites `[handler-contract.md §4.6]` for malformed-verdict failure handling. The needs-attention close path is owned by execution-model EM-015e and is cited from EV §8.1a.3. No asymmetry blocking. |
| handler-contract → workspace-model §6.2 (`review.json` path) | §6.2 table row | YES | WM §6.2 path table includes `${workspace_path}/.harmonik/review.json` and `…/review.iter-<N>.json` (lines 792–793). HC line 131 cites `[workspace-model.md §4.7]` (WM-027a) — also correct. ✓ |
| process-lifecycle PL-004a → execution-model EM-012a (precedence rule) | EM-012a in EM §4.3 | YES | PL-004a cites `[execution-model.md §4.3]` (the §4.3 Run-model section that holds EM-012/012a/015d/015e). ✓ |
| beads-integration BI-013a → operator-nfr §4.3 (drain semantics) | ON-009a in §4.3 | YES | BI-013a cites `[operator-nfr.md §4.3]`; ON-009a is in §4.3. ✓ |
| event-model §8.1a.3 schema → execution-model EM-015e (needs-attention route on malformed verdict) | EM-015e | YES | EV line 122 cites `[execution-model.md §4.3 EM-015e]`. ✓ |
| event-model §3 glossary → execution-model §6.1 (`WorkflowMode` enum) | §6.1 Run RECORD | YES | EM §6.1 records `WorkflowMode` enum entry (confirmed via EM changelog line 1165). ✓ |

---

## 2. Conflicts with non-modified specs

**architecture.md** — No conflict. The three-mode framing is additive (a Run-record field plus dispatch-path branching); no architectural invariant (centralized controller AR-INV-001, mechanism/cognition AR-INV-005, three-artifact separation AR-019) is touched. `dot`-deferral is consistent with the post-MVH extension posture (§10 conformance / §5 invariants admit post-MVH features via amendment per AR-022). No `dot`-mode normative work is being shipped at MVH.

**control-points.md** — No conflict. ON-008's amendment admits **intra-run iteration boundaries** as pause checkpoints only when `workflow_mode = review-loop`. CP-040..CP-045 (policy/verdict persistence) and CP-037 (precedence) are untouched. The amendment narrows the between-task invariant for one mode without inventing a new control-point Kind; the carve-out is at the operator-control layer (ON), not at the ControlPoint layer (CP). No CP requirement is invalidated.

**scenario-harness.md** — No conflict. Scenario-harness does not reference `workflow_mode`, review-loop events, or `needs-attention` (zero matches). Harness assertions remain framed around `run_started` / `run_completed` / `run_failed` (whose payloads gain an OPTIONAL `workflow_mode` field per EV §8.1 rule — backward compatible). New §8.1a events will need harness fixtures eventually, but no existing harness assumption is violated.

**reconciliation/spec.md + schemas.md** — No conflict, but **a gap**: reconciliation specs contain **zero references to `needs-attention`** (label or status). The new corpus assumes the operator-drain queue is *outside* reconciliation's scope (review-loop terminations close the bead normally; the label is then a pure dispatch-filter via BI-013a). This is internally consistent — needs-attention beads are dispatchable when the label is removed; reconciliation does not need a category for them. **No conflict, but flag for tasks pass:** add a one-line clarifying note to reconciliation that `needs-attention`-labeled closed beads are NOT a reconciliation surface (they are a dispatch-filter surface).

---

## 3. Terminology consistency

- **`workflow_mode`, `single`, `review-loop`, `dot`** — used consistently across all seven drafts; enum domain `{single, review-loop, dot}` appears verbatim in EM §3 glossary, EM-012, BI-009a, PL-004a, HC-006, WM-027a (via EM cite), ON-004a, EV §8.1 rule.
- **`ralph` residue** — ZERO occurrences in drafts or existing specs. Clean.
- **`claude_session_id` vs `session_id`** — disambiguated explicitly in EM §3 glossary, EV §3 glossary, HC-006 field set, and EV §8.1a payloads (which carry BOTH). 23 `claude_session_id` references across the seven drafts, all on review-loop surfaces. ✓
- **`needs-attention` label vs status** — drafts uniformly say "label" (Beads label), never "status." EM §3 glossary calls it "the close-path marker applied to the bead." BI-013a, ON-009a, EM-015e all use "label." ✓
- **Event names** — `implementer_resumed`, `reviewer_launched`, `reviewer_verdict`, `iteration_cap_hit`, `no_progress_detected`, `review_loop_cycle_complete`, `bead_label_conflict` — spelled identically in changelog, EV §8.1a table, EV §6.3 schemas, EM-015d/e prose, ON references. ✓

---

## 4. Bidirectional references

- **HC ⇄ WM** for `.harmonik/review.json`: HC §4.2 (line 131) → WM §4.7 ✓; WM §6.2 row mentions reviewer-agent write but does not back-cite HC. **Minor asymmetry, not blocking** — WM-027a's (a) clause says "the reviewer subprocess writes the file" without naming the handler-contract surface. Tolerable; the verdict-file is a workspace artifact, not a handler-protocol artifact per se.
- **EM ⇄ ON** for `needs-attention` close path: EM-015e → ON §4.3 ✓; ON-009a → BI §4.3 + §4.5 (chooses to cite Beads rather than back-cite EM). Acceptable: ON owns "operator-drain discipline," EM owns "when a run enters the queue."
- **BI ⇄ EM** for `workflow:<mode>` label: BI-009a → EM §4.3 ✓; EM-012a → BI §4.3 ✓. Symmetric.
- **EV ⇄ EM**: EV §3 glossary cross-refs EM §6.1 (`WorkflowMode`); EM-015d/e cite EV §8.1a after the inline fixes. Symmetric.

---

## 5. Changelog accuracy spot-checks (5 entries)

1. **PL changelog** — "New requirement PL-004a (Default workflow mode) in §4.1." → Verified: PL-004a present at line 214 in §4.1 ✓.
2. **EM changelog** — "EM-012 amended: adds `workflow_mode` … reserves four `context` keys: `iteration_count`, `last_verdict`, `claude_session_id`, `last_diff_hash`." → Verified in EM glossary + §6.1 ✓.
3. **EV changelog** — "All run-lifecycle events carry optional `workflow_mode` field." → Verified: EV §8.1 payload-field rule at line 95 ✓.
4. **WM changelog** — "WM-013e `.gitignore` set extended to include `.harmonik/review.json` and `.harmonik/review.iter-*.json`." → Verified at line 327 ✓.
5. **ON changelog** — "ON-008 amended … review-loop additionally admits intra-run iteration boundaries." → Verified in ON-008 second paragraph (lines 211–212) ✓.

No changelog drift detected.

---

## 6. Registry impact (`specs/_registry.yaml`)

**No changes required.** The registry tracks **requirement-prefixes only** (AR / EM / EV / HC / WM / CP / ON / PL / RC / BI / SH). All new requirement IDs in the seven drafts (`PL-004a`, `BI-009a`, `BI-010c`, `BI-013a`, `EM-012a`, `EM-015d`, `EM-015e`, `HC-003a`, `ON-004a`, `ON-009a`, `ON-013d`, `ON-035a`, `WM-027a`) reuse existing reserved prefixes. New event types and new sections (§8.1a, §8.8.6) are not registry concerns. The seven specs all retain their existing prefixes — no spec was renamed or split.

---

## 7. Overall assessment

**Coherence verdict: COHERENT.** The seven drafts form an internally consistent extension of the existing corpus. Cross-references are dense and (with the two inline fixes applied this pass) accurate. Terminology is uniform; no `ralph` residue, no `session_id` / `claude_session_id` conflation, no `needs-attention` label/status confusion.

**Blocking issues: none.**

**Resolved inline (2):**
- EM §4.3 EM-015d/e cited `[event-model.md §8.1]` for six review-loop events; retargeted to `§8.1a` (three Edit ops, lines 246, 259, 261).
- EM changelog cited `[event-model.md §8.8]` for `bead_label_conflict`; retargeted to `§8.8.6` (one Edit op).

**Follow-ups deferred to tasks pass (2):**
- **Bead T-INT-1.** ON-009a's `[handler-contract.md §4.2 HC-006]` citation for "reviewer phase BLOCK emission" mis-targets HC-006 (which is the LaunchSpec record). Re-target to `[workspace-model.md §4.7 WM-027a]` + `[event-model.md §8.1a.3]` (the actual locus of reviewer-verdict surfacing).
- **Bead T-INT-2.** Reconciliation spec gap: add a one-line clarifying note that `needs-attention`-labeled closed beads are NOT a reconciliation surface (they are a BI-013a dispatch-filter surface). One-line addition to `reconciliation/spec.md` Cat 6 enumeration or scope-out section.

**Non-blocking observations (informational, no bead):**
- WM-027a (a) does not name the handler-contract surface for the reviewer-write; tolerable per workspace-vs-handler boundary.
- Reviewer-verdict bus event (`reviewer_verdict`) is class F and gated on file-validation per EV line 122 — strong, no concern.

Ready to proceed to the tasks pass.
