# extqueue v0.1 — Integration pass

Date: 2026-05-14. Reviewer: integration sub-agent.

## §1 Scope of audit

Examined corpus:

- The 6 drafted specs at `/Users/gb/.kerf/projects/gregberns-harmonik/extqueue/05-spec-drafts/{queue-model,execution-model,beads-integration,process-lifecycle,event-model,operator-nfr}.md`.
- The package changelog at `/Users/gb/.kerf/projects/gregberns-harmonik/extqueue/05-changelog.md`.
- All non-amended foundation specs: `/Users/gb/github/harmonik/specs/{architecture,claude-hook-bridge,control-points,handler-contract,scenario-harness,workspace-model}.md` and `/Users/gb/github/harmonik/specs/reconciliation/{spec,schemas}.md`.

Coverage methodology: ten audit checklist items from the task framing, executed by repository-wide grep with negative-set filtering (exclude amended-spec hits, include only non-amended-spec hits), plus targeted Read on every referenced section anchor in §§2/4 below.

## §2 Cross-reference findings

### §2.1 Broken section anchor: workspace-model §4.6 vs §4.7 (LOAD-BEARING)

Three citations in the drafts point at `workspace-model.md §4.6 WM-026`, but WM-026 is located under **§4.7 Session-log directory and metadata sidecar** (workspace-model.md:546 / §4.7 heading; WM-026 declaration at workspace-model.md:557). §4.6 in workspace-model.md is "Conflict resolution" (workspace-model.md:500) and does not contain WM-026.

Affected draft locations:

- `05-spec-drafts/queue-model.md:190` — QM-001 cites "WM-026 atomic-write discipline per [/Users/gb/github/harmonik/specs/workspace-model.md §4.6 WM-026]".
- `05-spec-drafts/queue-model.md:586` — appendix §A.2 row cites "[/Users/gb/github/harmonik/specs/workspace-model.md] §4.6 WM-026".
- (changelog `05-changelog.md` quotes the same string at line 25 — corrects with the file fix.)

The two other queue-model citations that use the correct section (workspace-model.md §4.7) are at beads-integration draft (`05-spec-drafts/beads-integration.md:350, 502, 562`) and event-model draft (`05-spec-drafts/event-model.md` indirectly via §4.4 ref) — these are correct.

Verdict: WM-026 location anchor MUST be corrected. Mechanical edit; substantive content of the citation is correct.

### §2.2 workspace-model.md §4.4 anchor in queue-model.md QM-063

`05-spec-drafts/queue-model.md:545` cites "the WM event-emit-after-persist discipline per [/Users/gb/github/harmonik/specs/workspace-model.md §4.4]". workspace-model.md §4.4 is "Lifecycle states and events" (workspace-model.md:390) and does cover the emit-after-persist ordering gate at WM-016. The §4.4 anchor is correct; no fix needed.

### §2.3 reconciliation/spec.md BI-013a reference (DOCUMENTATION-ALIGN)

`reconciliation/spec.md:57` states: "a closed bead with a `needs-attention` label is a dispatch-filter signal for the BI-013a ingestion workflow." Under the extqueue draft (`beads-integration.md` draft line 266), BI-013a is no longer an adapter dispatch-time filter — it is a **submit-time validation rejection** mapped through QM-021. The reconciliation prose is now slightly off-register but not load-bearing for any normative behavior: it correctly identifies BI-013a as the right routing target. Recommendation: minor prose tweak in a follow-up; not blocking for v0.1.

### §2.4 Pre-existing miscitation: WM-007 in beads-integration.md:205

beads-integration.md:205 (pre-existing, NOT introduced by extqueue) cites "task branch merged per [workspace-model.md §4.5 WM-007]". WM-007 is the three-level branching model, located in workspace-model.md §4.2 (workspace-model.md:264). The correct anchor for "task branch merged" is WM-019 / WM-021 in §4.5 (workspace-model.md:448 / 493). Flagged for record; out-of-scope for extqueue (the row is not edited in the draft).

### §2.5 Forward references INTO drafted specs from non-amended specs

Repo-wide grep for `EM-049`, `EM-050`, `EM-051`, `BI-013b`, `BI-013c`, `PL-003a`, `ON-009a`, `QM-`, `queue_id`, `queue_group_index`, and the new §8.10 event names against the non-amended spec set returned **zero hits** outside the 6 amended files. No non-amended spec carries a stale anchor into a draft.

### §2.6 References INTO queue-model.md from non-amended specs

Currently zero. queue-model.md is a new spec; no non-amended spec yet cites it. Candidate landing sites considered:

- **workspace-model.md** — does not currently need a citation. Workspace lifecycle is invariant under queue-vs-no-queue; the queue does not extend `Workspace` records.
- **scenario-harness.md** — see §5 below; no v0.1 amendment needed.
- **architecture.md** — the "atomic queued work item" definition of *bead* at architecture.md:72/403 is colloquial, predates the queue subsystem, and remains accurate (Beads-side queue). No edit.
- **control-points.md** — no overlap; the queue does not introduce new control-point primitives.
- **handler-contract.md** — handlers are launched by node; `queue_id` is daemon-internal and does not enter the LaunchSpec surface. No edit.
- **reconciliation/spec.md** — see §2.7 below.

### §2.7 Reconciliation interaction (NO ACTION)

`.harmonik/queue.json` is daemon execution-plan state, peer to `.harmonik/daemon.state` and `.harmonik/daemon.upgrading`. Reconciliation's three-store model (git, Beads, JSONL — reconciliation/spec.md:64) does NOT enumerate daemon-private state files as reconciled stores. The queue's recovery model is daemon-restart-and-replay (QM-002), not reconciliation. Reconciliation correctly remains the in-flight-run divergence detector; queue state is correctly out-of-scope. No edit needed in reconciliation/spec.md or reconciliation/schemas.md.

## §3 Terminology consistency

### §3.1 "enqueue" usage in non-amended specs

Repo-wide grep on the non-amended-spec set returned **zero** hits for `enqueue` or `harmonik enqueue`. The retirement is clean.

### §3.2 "br ready" usage

Repo-wide grep on the non-amended-spec set returned **zero** hits for `br ready` outside the amended files. The orchestrator-facing-only repositioning is clean. (`reconciliation/spec.md:393` defines `RC-012a` "Post-`ready` Cat 0", which uses `ready` as the *daemon ready-state* term — unrelated to `br ready`.)

### §3.3 "queue" usage

Two distinct senses survive in the corpus and both are now disambiguated correctly:

- "execution queue" / "the queue" → queue-model.md (daemon's execution plan).
- "Beads queue" / "needs-attention queue" / "Beads is the catalog" → operator-nfr.md draft ON-009a (terminology note) + ON-015 rewrite.

Non-amended specs use "queue" only colloquially:

- `architecture.md:72, 403` — "an atomic queued work item in the Beads SQLite store" (Beads-side, accurate).
- No other normative use of "queue" in non-amended specs.

Verdict: consistent.

### §3.4 "dispatch loop" / "dispatcher"

Non-amended-spec usages refer to **hook dispatch loop** or **orchestrator-core dispatch loop** — both distinct concepts from the queue-driven dispatch loop now owned by execution-model §7.4:

- `control-points.md:50, 185, 438, 1069` — hook dispatch (S05); distinct subsystem.
- `workspace-model.md:580` — "the daemon (or the orchestrator-core dispatch loop)" for `.harmonik/review.json` consumption; the orchestrator-core dispatch loop IS the queue-consuming dispatch loop of execution-model §7.4, so this remains coherent.

Verdict: no ambiguity introduced.

### §3.5 Retired dropped events

Repo-wide grep returned **zero** hits for the six dropped event-name candidates (`queue_item_dispatched`, `queue_item_completed`, `queue_item_failed`, `queue_completed`, `queue_resumed`, `queue_validation_failed`) outside the amended files. The 6-event cohort is the only registered surface; the §8.10 selection is internally consistent. (Note: within event-model.md draft itself, `queue_validation_failed` survives as a JSON-RPC error type, not an event — the C1 reviewer fix from `05-changelog.md:102`.)

## §4 Event-list completeness

### §4.1 Dropped events — zero residue

See §3.5. The six events listed in event-model.md draft §8.10 (`queue_submitted`, `queue_group_started`, `queue_group_completed`, `queue_paused`, `queue_appended`, `queue_item_deferred_for_ledger_dep`) are the complete cohort and no non-amended spec references a dropped name.

### §4.2 EM-049 / EM-050 / EM-051 — no duplicate documentation

Repo-wide grep on `--max-concurrent`, `capacity gate`, `in-flight-run capacity`, `claim-write semaphore` against non-amended specs returned two informational hits:

- `claude-hook-bridge.md:445, 638` — `capacity gate` mentioned in CHB-028 rationale and revision history (the "Stop hook never fires → capacity gate jams" failure mode). Informational, not a normative duplicate.

Neither location independently *specifies* the gate; they reference behavior owned elsewhere. EM-049/EM-050/EM-051 anchor cleanly with no reconciliation needed.

### §4.3 §8.10 emission-ordering paragraph

Internally consistent with QM-050 / QM-051 / QM-052 ordering chains in queue-model.md §8.1–§8.3.

## §5 Recommendations

### §5.1 In-package edits (required)

**E1 — queue-model.md (3 anchor fixes).** Replace `§4.6 WM-026` with `§4.7 WM-026`:

- queue-model.md:190 — QM-001 body.
- queue-model.md:586 — §A.2 cross-spec-impact row.
- changelog 05-changelog.md:25 (string `WM-026 atomic-write discipline (temp + fsync + rename + fsync(parent_dir)) per QM-001`) — no section anchor here; no fix needed.

Mechanical sed-equivalent: `s|§4.6 WM-026|§4.7 WM-026|g` in queue-model.md.

### §5.2 In-package edits (optional polish)

**E2 — reconciliation/spec.md:57 prose tweak (DEFER).** Update "dispatch-filter signal for the BI-013a ingestion workflow" to "submit-time validation rejection signal for the BI-013a ingestion workflow per [beads-integration.md §4.5a BI-013b]". Not load-bearing — the BI-013a reference still resolves correctly to the same requirement-ID; semantics differ only in *where* the filtering happens. Defer to a follow-up housekeeping pass alongside other reconciliation/spec.md edits.

### §5.3 Out-of-package items (record only)

**R1 — beads-integration.md:205 pre-existing miscitation of WM-007.** Pre-extqueue defect; flag for a future pass. Out of scope for v0.1.

**R2 — scenario-harness.md.** 02-components.md §7 listed scenario-harness.md as a "small edit" candidate. Examination found no actual normative touchpoint requiring an amendment in v0.1: scenario-harness is harness-mechanics-only, does not reference `br ready`, `enqueue`, or any queue lifecycle event. Its existing dispatch-order assertion machinery (SH-017 `same orchestrator entry-point`) inherits the new dispatch-loop semantics for free. **No v0.1 edit required.** Document this finding in scenario-harness.md only if a queue-aware scenario lands in v0.2.

## §6 Resolution applied / deferred

| Item | Resolution |
|---|---|
| §2.1 — workspace-model §4.6 vs §4.7 anchor (3 locations) | **APPLY in-package.** Mechanical fix in queue-model.md:190 and queue-model.md:586. |
| §2.2 — workspace-model §4.4 in QM-063 | **No action.** Anchor correct. |
| §2.3 — reconciliation/spec.md:57 BI-013a prose | **Defer to follow-up.** Not load-bearing; semantics still resolve. |
| §2.4 — pre-existing WM-007 miscitation at beads-integration.md:205 | **Out of scope.** Pre-existing defect; flag for separate pass. |
| §2.5 — forward refs into drafts | **No action.** Zero stale anchors found. |
| §2.6 — refs into queue-model.md | **No action.** No load-bearing landing site requires queue-model.md citation in v0.1. |
| §2.7 — reconciliation interaction | **No action.** Queue.json is daemon-private state, correctly out of three-store reconciliation scope. |
| §3.1 — `enqueue` residue | **No action.** Clean. |
| §3.2 — `br ready` residue | **No action.** Clean. |
| §3.3 — "queue" disambiguation | **No action.** ON-009a note + ON-015 rewrite suffice. |
| §3.4 — "dispatch loop" overlap | **No action.** Hook-dispatch and orchestrator-core-dispatch usages remain distinct. |
| §3.5 / §4.1 — dropped event residue | **No action.** Zero hits. |
| §4.2 — EM-049/050/051 duplicate documentation | **No action.** CHB references are informational, not normative. |
| §5 — scenario-harness.md amendment | **No action in v0.1.** Document at v0.2 if a queue-aware scenario lands. |

Net edits required in package: **1 mechanical anchor-fix touching 2 lines in queue-model.md.**

## §7 Final coherence assessment

After applying the single mechanical fix in §5.1 E1, the corpus is internally consistent. The extqueue v0.1 amendments land cleanly across the 6 amended specs without dangling forward references, without retired-event-name residue, and without terminology conflicts in the 8 non-amended specs. The new queue-model.md is fully self-contained at v0.1 and does not yet require citation from any non-amended spec — its consumers (process-lifecycle, beads-integration, execution-model, event-model, operator-nfr) are the 5 amended specs, and the citation graph is bidirectionally closed within the amended set plus controlled outbound references to workspace-model (atomic-write discipline) and beads-integration (already amended). No latent issue surfaces under a future spec read; the only outstanding pre-existing defect (WM-007 anchor at beads-integration.md:205) is unrelated to extqueue and properly tracked for a separate cleanup pass. The package is ready to advance to the tasks pass after the E1 mechanical fix is applied.
