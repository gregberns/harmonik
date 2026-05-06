# Parked-state cleanup — 2026-05-05

**Driver.** User withdrew the "loaded beads must not auto-start; parked state + readiness workflow approval" rule. Verbatim: "If there are instructions you read as 'I can't do anything until the person does something' then those need to be changed or they were written in a different context and need to be ignored. The objective now is to get all of the tasks into a state where we can start a new agent and it can just start churning hard through all the tasks."

**Scope.** Hunt down every reference to the rule across CLAUDE.md, specs/, docs/, and the live Beads DB; remove the bead-lifecycle gating instances; preserve queue-level operator safety (operator-stop / pause / upgrade per `bootstrap.md §4`).

## 1. Search hits — classified

Search command:

    grep -rn "parked\|readiness workflow\|load-but-don.t-start\|awaiting-review\|operator-approve\|approval gate" CLAUDE.md specs/ docs/

Total raw hits: 45 lines across 26 files. Classification:

### KEEP (queue-level safety / unrelated semantics) — 24 hits

| File | Line(s) | Why KEEP |
|---|---|---|
| `specs/operator-nfr.md` | 73, 957 | The `in_flight(run)` predicate excludes "parked lifecycle position" (bead-state, not run-state). Rule withdrawal makes parked-position empty, but the predicate's semantic is harmless either way. KEEP — but flag for v0.4.x patch (see §3 below). |
| `docs/foundation/core-scope.md` | 306 | "Branch protection with approval gate" — solo-dev branch-protection setting; not bead-lifecycle. KEEP. |
| `docs/foundation/core-scope.md` | 316 | "ephemeral `agent/<codename>` branches for parked-across-sessions work" — kerf session-park branch, not bead state. KEEP. |
| `docs/foundation/project-level/agent-configuration.md` | 74 | Same — session-park branch. KEEP. |
| `docs/foundation/project-level/build-practices.md` | 46 | Same — session-park branch. KEEP. |
| `docs/foundation/problem-space.md` | 13, 80, 230, 297, 339 | "23 parked open architectural questions" — questions parked for design resolution, not bead state. KEEP. |
| `docs/subsystems/orchestrator-core.md` | 34, 62 | "approval gates" (policy concept), "parked detail" (architectural-question parking). KEEP. |
| `docs/subsystems/policy-engine.md` | 15 | "approval gates" — policy engine concept (post-MVH per `core-scope.md` §10). KEEP. |
| `docs/subsystems/verifier-layer.md` | 19 | "parked as open detail" — architectural-question parking. KEEP. |
| `docs/subsystems/agent-runner.md` | 68 | "parked detail" — architectural-question parking. KEEP. |
| `docs/subsystems/INDEX.md` | 28 | "node types parked" — design-detail parking. KEEP. |
| `docs/decompose-to-tasks/meta-pilot-data.yaml` | 32 | "parked as forward-deferred edges" — pilot-edge state, not bead-lifecycle. KEEP. |
| `docs/reviews/2026-04-27-ar-pilot-r1/decomposition-r1.md` | 27 | Historical review artifact ("readiness workflow would mis-classify"). Reviews are immutable. KEEP. |
| `docs/reviews/2026-04-24-operator-nfr-r2/skeptic.md` | 31, 302 | Historical review artifact (suggests routing parked-state cite to BI). Reviews are immutable. KEEP. |

### EDIT — 3 hits

| File | Line(s) | Edit |
|---|---|---|
| `TASKS.md` | 68 | Removed "Loaded bead set → readiness workflow" entry; replaced with note that the gate was withdrawn. |
| `docs/decompose-to-tasks/pilot-review-protocol.md` | 160 | Replaced "every bead enters `draft`; readiness workflow promotes later" with a clarification that the gate was withdrawn 2026-05-05; loaded beads flip to `open` at load time so agents can dispatch directly. |
| `docs/foundation/phase-1-readiness-gap-analysis.md` | 14, 17, 48-53, 211, 229-230 | Body preserved (historical context); §Z addendum appended that voids §A3, voids the candidate `p1-readiness-*` beads, and voids entry-gate item 6. |

### EDIT-as-historical-narrative (no edit needed; informative only) — 4 hits

| File | Line(s) | Status |
|---|---|---|
| `docs/decompose-to-tasks/bi-pilot.md` | 264 | Pilot narrative §8 item 6 references the readiness workflow as the deferral mechanism for `gated-by-spec-edit`. Per task scope, pilots are not lifecycle-gate carriers; the narrative documents the THEN-current intent. NO EDIT. |
| `docs/decompose-to-tasks/ev-pilot.md` | 17 | Same shape — describes `post-mvh` tag's effect "operationally parked; readiness workflow filters". NO EDIT. |
| `docs/decompose-to-tasks/bi-smoke-load-findings.md` | 83 | "Operators tune priority in the readiness workflow" — historical findings doc. NO EDIT. |
| `docs/decompose-to-tasks/bi-smoke-load-findings.md` | 101 | "F2's promotion of `draft` over `harmonik:parked` is validated" — historical findings; the `draft` mechanism remains valid as the bead-create-time default before the loader flips to `open`. NO EDIT. |

### FLAG-FOR-VERSIONED-PATCH (do NOT silently edit) — 7 hits

These are in `reviewed` or in-versioning artifacts that carry revision-history obligations.

| File | Line(s) | Recommendation |
|---|---|---|
| `docs/decompose-to-tasks/discipline.md` v0.9 | 246, 256, 263, 267, 379, 437, 543 | The discipline carries the readiness-workflow framing as part of §2.8 / §2.9 / §2.11(c) / §2.12 / §2.13. v0.10 patch wave is in flight (per `STATUS.md`); rule-removal is a **v0.11 candidate**. Do NOT apply retroactively to v0.9 or fold into v0.10 silently. The v0.11 entry should: (a) rewrite §2.9 to drop the "readiness workflow promotes `draft → open`" language and replace with a loader-flips-to-open contract; (b) rewrite §2.8 / §2.11(c.1) / §2.11(d) "readiness workflow can defer" prose to "loader / filter excludes from dispatch set"; (c) revision-history row + version bump v0.10 → v0.11 (minor — it is a substantive rule change, not a typo). |
| `specs/operator-nfr.md` v0.4.0 (`reviewed`) | §3 glossary line 73 | The `in_flight(run)` predicate explicitly excludes "parked lifecycle position." Withdrawal of the rule does not break the predicate (it just makes the excluded set empty); but the prose is now stale. Recommend a **v0.4.x patch** that drops the "parked lifecycle position" sub-clause from the glossary entry. The Group A1 entry in §revision-history (line 957) is historical and STAYS as the record of why the prior fabricated `RunState.PARKED` was removed. Edit estimate: 2 lines in §3, no requirement-body changes. **Patch type: documentary clarification (v0.4.0 → v0.4.1, patch-level).** No new normative content; revision-history row required. |

### NONE FOUND

- `CLAUDE.md` — no parked-rule references. No edit needed.
- `STATUS.md` line 57 — describes discipline v0.1's "load `parked`, no priority" rule as historical fact. The discipline has since superseded that with `draft` status (v0.3 F2). Description is historically accurate; no edit warranted.
- `AGENT_INDEX.md` — no parked-rule references.

## 2. Edits applied

| Path | Description |
|---|---|
| `/Users/gb/github/harmonik/TASKS.md` | Removed "Loaded bead set → readiness workflow" entry; replaced with a note explaining the gate was withdrawn 2026-05-05 and that operator queue-pause / stop controls remain operational. |
| `/Users/gb/github/harmonik/docs/decompose-to-tasks/pilot-review-protocol.md` | Rewrote §5 last bullet to clarify that `br ready` clean-on-load remains the structural check; the readiness-workflow promotion language is replaced with a loader-flips-to-open contract; the structural check (no cycles, no missing prerequisites) is the surviving criterion. |
| `/Users/gb/github/harmonik/docs/foundation/phase-1-readiness-gap-analysis.md` | Appended §Z addendum: §Z.1 (CI scope reduced — no GitHub Actions, agent-reviewer-every-commit runs locally), §Z.2 (parked rule withdrawn — §A3 voided, candidate beads `p1-readiness-*` voided, entry-gate item 6 voided), §Z.3 (twin-binary gap addressed by parallel beads pass). Revision-history row added (v0.1 → v0.2). Body §§A–E unchanged. |

## 3. Spec-patch follow-ups (not applied)

| Spec | Version | Section | Edit | Patch-bump |
|---|---|---|---|---|
| `specs/operator-nfr.md` | 0.4.0 → 0.4.1 | §3 glossary `in_flight(run)` entry; consider whether `parked` cross-reference to `[beads-integration.md ...]` is still meaningful or should be dropped entirely | Drop the "parked lifecycle position (loaded bead not yet dispatched) is bead-state-only" sub-clause from line 73; the predicate `run.state ∉ {completed, failed, canceled}` stands on its own. Add revision-history row. | patch (no normative behavior change) |
| `docs/decompose-to-tasks/discipline.md` | 0.9 → 0.11 (v0.10 already queued) | §2.8 (F6 OQ-resolution carve-out); §2.9 ("readiness workflow promotes `draft → open`"); §2.11(c.1) ("readiness workflow can defer"); §2.11(d.2 / EV-2 transient-tag effect); §2.12 (.gitignore prose mentioning "when readiness workflow lands"); §2.13 backfill prose | Replace "readiness workflow defers" / "readiness workflow promotes" framing with "loader / dispatcher filters" framing. The `gated-by-spec-edit`, `post-mvh`, `gated-by-corpus-scale` transient tags continue to MARK beads-to-defer; the EFFECT is now expressed as filter-at-dispatch-time, not as workflow gate. Add revision-history row capturing F-cleanup-2026-05-05 finding. | minor (substantive rule semantics, but no decomposition output changes) |

## 4. Beads DB cleanup — Phase 3

`br list --status parked` (and `br list -a --status parked` for closed-included) returned **zero rows**.

The discipline (v0.3 F2) replaced the synthetic `harmonik:parked` tag with Beads's native `draft` status; no parked beads were ever created. **Zero parked beads found; no flips required.**

For completeness:

    $ br list --status parked
    (no output — zero parked beads)
    $ br list -a --status parked
    (no output — zero parked beads, including closed)

This validates the §A3 BLOCKER reframe in the gap-analysis addendum: the rule withdrawal has no live-data cleanup cost.

## 5. Readiness-gap doc addendum

**Landed at** `/Users/gb/github/harmonik/docs/foundation/phase-1-readiness-gap-analysis.md` (§Z, lines 287+; revision-history v0.2 row added).

Captures three user clarifications:

1. **Z.1 CI clarification.** No GitHub Actions / remote pipeline; agent-reviewer-every-commit runs locally. Voids the `p1-build-ci-workflow` candidate bead. Build/test scaffolding (Makefile, golangci, lefthook, agent-reviewer skill) remains.
2. **Z.2 Parked-state rule withdrawn.** §A3 voided; candidate beads `p1-readiness-workflow-definition` / `p1-readiness-parked-state` / `p1-readiness-gate-bead` voided; entry-gate item 6 voided. Operator queue-level controls (stop, pause, upgrade per `bootstrap.md §4`) explicitly preserved. Transient tags (`gated-by-spec-edit`, `post-mvh`, `gated-by-corpus-scale`) explicitly preserved with reframed effect (loader/filter, not workflow gate).
3. **Z.3 Twin-binary gap.** §B1 acknowledged as parallel-pass scope; addendum sets expectation that §B1 closes via that pass, not via this analysis.

Cross-cutting closure note added: §A and §B gaps are being addressed by parallel beads-filing agents off the §"Recommended new tracking beads" list.

## 6. Surprises / observations

1. **No live "parked" beads.** Despite the rule's prominence in project memory and gap analysis, the discipline (v0.3, 2026-04-26) had already replaced the synthetic `harmonik:parked` tag with `draft` status; the readiness workflow was never built and no bead was ever in a parked-equivalent state. The withdrawal is therefore documentation cleanup, not data cleanup.

2. **The `in_flight(run)` glossary entry in `operator-nfr.md` (reviewed v0.4.0) is the most deeply-coupled spec reference.** It cites a "parked lifecycle position" as a bead-state-only thing excluded from the run predicate. Withdrawal makes the excluded set empty; the predicate is structurally inert under the change. Worth a v0.4.x patch but NOT a re-review trigger.

3. **The `gated-by-spec-edit` / `post-mvh` / `gated-by-corpus-scale` transient tags are still useful.** The user's reframe targets bead-lifecycle approval (the workflow promoting `draft → ready`), not deferral semantics. Beads tagged `gated-by-spec-edit` (e.g., the BI-014a / OQ-BI-010 / PL-006 worked example) are still NOT independently dispatchable until the spec edit lands. The reframe shifts the EFFECT from "readiness workflow filters" to "loader / dispatcher filters" — same outcome, different mechanism. Discipline v0.11 needs to capture this nuance carefully so the tags don't lose their intended effect.

4. **`docs/decompose-to-tasks/discipline.md` v0.9 has SEVEN distinct loci where the parked/readiness language appears.** This is more than I expected; v0.11 will be a substantive minor-version edit, not a one-line fix. Locus list: §2.8 (F6 carve-out), §2.9 (`draft` status declaration + procedure + priority), §2.11(c.1) (post-mvh transient tag effect), §2.12 (`.gitignore` framing), §2.13 (backfill workflow note), plus revision-history row at v0.3 (which is historical and stays). The v0.10 patch wave is currently active (per `STATUS.md`); coordinating the v0.11 edit with that wave is a separate planning task.

5. **`docs/decompose-to-tasks/bi-smoke-load-findings.md` line 101 is interesting.** It says "F2's promotion of `draft` over `harmonik:parked` is validated" — meaning the discipline's switch from a synthetic tag to Beads's native `draft` status was confirmed working at smoke-load time. With the rule withdrawn, the `draft → open` flip happens AT LOAD TIME (or via loader option) rather than via a separate workflow run. The mechanism that gets a bead from "freshly created" to "agent-dispatchable" is now a loader concern, not an orchestrator-workflow concern.

6. **Memory state — confirmed already updated by orchestrator agent.** Per the cleanup mission's preamble, `project_harmonik_task_ingestion.md` was already amended and `feedback_agent_flow_priority.md` was already added. No memory-file edits required from this pass.

7. **No edits to CLAUDE.md required.** CLAUDE.md does not carry the parked-state rule directly; the rule lived in project memory and propagated to docs/specs/tasks. The §"Don't" list at lines 53-58 is unaffected.

## 7. Summary

| Phase | Result |
|---|---|
| 1 — survey | 45 raw hits; classification: 24 KEEP, 3 EDIT applied, 4 EDIT-as-historical-narrative no-edit, 7 FLAG-FOR-VERSIONED-PATCH, 7 NONE-FOUND in expected locations |
| 2 — assess and edit | 3 files edited: `TASKS.md`, `pilot-review-protocol.md`, `phase-1-readiness-gap-analysis.md` (addendum) |
| 3 — Beads DB cleanup | 0 parked beads found; no flips required |
| 4 — readiness gap addendum | Landed at `docs/foundation/phase-1-readiness-gap-analysis.md §Z`, v0.2 revision-history row added |
| 5 — output report | This document |

**Operator queue-level controls (`bootstrap.md §4`) explicitly PRESERVED.** The reframe targets bead-lifecycle approval, not runtime safety.
