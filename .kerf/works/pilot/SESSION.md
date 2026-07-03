# Session Notes

## Session 2 (2026-05-31) — must-fix application + Integration + Tasks → Ready

Picked up `pilot` (Pi-driven dispatch & control plane) at the Integration pass (passes 1-5 completed in session 1; the spec drafts were already authored). This session: applied the two finalize critical-review must-fix items to the spec drafts, completed the Integration pass (`06-integration.md`), completed the Tasks pass (`07-tasks.md`) with bead reconciliation, and advanced to `ready`.

### Critical-review must-fixes applied (both)

1. **EM-067 redundancy/coherence gap (the one substantive flaw) — RESOLVED via resolution (b).** The §7.4 main-loop keeps a loop-top gate `IF should_pause_between_runs(): wait_for_resume(); CONTINUE` ABOVE the `queue IS None` branch; EM-067 added a SECOND operator-pause gate INSIDE that branch. Verified `should_pause_between_runs()` is annotated `[operator-nfr.md §4.3] pause-between-runs` (`specs/execution-model.md:1391`), and §4.3.ON-008 (`operator-nfr.md:211`, between-task drain gate) IS the operator-control pause hook — so once the ON-056/057 producer lands, the loop-top check `CONTINUE`s before control reaches the `queue IS None` branch, making EM-067's inline gate unreachable on the operator-pause path. **Resolution:** reframed EM-067 — the loop-top ON-008 check is the PRIMARY gate covering all paths (incl. fallback); EM-067's load-bearing content is the BINDING of the fallback path to the single source of pause truth (ON-056/057 `operator_pause_status`, the same signal driving QM-054), guaranteeing both branches honor ONE pause concept; the inline branch gate is retained as a belt-and-suspenders re-assert (vacuous in a conforming impl, non-vacuous only if `should_pause_between_runs()` was scoped narrower than the full ON-008 state). Edits: EM-067 spec text (§4.11), §7.4 pseudocode comments, §10.2 test obligation (reframed to observable-outcome + single-source-of-truth test), execution-model v0.8.1 changelog row, and `05-changelog.md` EM-067 entry. ON-057 (operator-nfr draft 307) and QM-054 note (queue-model draft 710) were already consistent with the reframing — no edit needed.

2. **Complete `06-integration.md` (was the TODO skeleton) — DONE.** Full integration record written (Parts 1-6 + verdict): must-fix resolutions, cross-reference checks per draft, contradictions checked (six candidates, all resolved), terminology consistency, changelog verification, final assessment. Also captured the three design-notes deferred-to-Integration items.

### Design-notes deferred-to-Integration items (all three resolved)

- **(i) CL-051 → `[reconciliation/spec.md §4.4]` routing.** Verified §4.4 (`reconciliation/spec.md:452`) is the Investigator-agent contract = the investigator-required-category (Cat 2/Cat 3) path. A trailer-present-but-no-event divergence is a store divergence routing to an investigator, so §4.4 is the correct anchor. Recorded the terminology bridge: "Tier-2 reconciliation" is harness-internal language for the investigator-required-category path; not a literal reconciliation-spec term. No edit needed.
- **(ii) EM-062..065 line-range re-verification.** Cited by section+ID (§4.13/§4.14), unchanged by the pilot edits (EM-066/067 inserted into §4.11, shifting absolute LINE numbers ~23 down but not the section numbers or IDs). All cross-refs remain valid. No edit needed.
- **(iii) ON-013 `drain_summary?` EV coordination.** A pre-existing ON-013 cross-spec coordination request (operator-nfr → event-model §8.7.6); the pilot bundle consumes it (ON-057 conformance (d)), adds no new EV obligation. Recorded as COORD-1 (tracked, non-blocking).

### One cross-reference drift found and fixed

- **CL-071 `[queue-model.md §9]` → `[queue-model.md §8.1 QM-050]`.** §9 in the queue-model draft is "Concurrency" (QM-060+), the WRONG target; the submit-as-start confirmation lives at §8.1 QM-050. Fixed in the cognition-loop draft.

### Integration pass

- `06-integration.md` — full record: scope recap, Part 1 (must-fix + deferred-item resolutions), Part 2 (per-draft cross-reference checks, all VALID after the one fix), Part 3 (six contradiction candidates, all resolved — incl. operator-pause vs handler-pause orthogonality, ON-INV-006 vs ON-056), Part 4 (terminology consistency), Part 5 (changelog verification), Part 6 (final assessment: APPROVED).
- `integration-review.md` — criteria table; APPROVE; verdict advance to `tasks`.

### Tasks pass

- `07-tasks.md` — six impl tasks (IT-1..IT-6) + six test tasks (TT-1..TT-6); spec traceability, acyclic DAG, Wave A/B/C parallelization plan, changelog coverage table (all 8 rows; queue-model A4 annotation-only row satisfied transitively).
- **Bead reconciliation** (`br list --label codename:pilot` → 11 beads): IT-2→hk-ry8q1 (P0), IT-3→hk-3ix6o (P1), IT-4→hk-dg42b (P2), IT-5→hk-ytj2r (P2), IT-6→hk-5bw7a (P3); TT-1→hk-h5lv2, TT-2→hk-ynjnf, TT-3→hk-95a2r, TT-4→hk-rnlxh, TT-5→hk-iht2w, TT-6→hk-va7z2; IT-1→hk-ry8q1 (EM-067 gate half) + GAP-1 (EM-066 flag half).
- **GAP-1 flagged (no bead created):** the `--no-auto-pull`/`--queue-only` flag + EM-066 quiet-branch wiring (`workloop.go` + CLI flag + topology default) has no `codename:pilot` bead. hk-ry8q1 covers the pause verb + producer + EM-067 br-ready GATE, but not the EM-066 FLAG. The pre-existing daemon-idle bead **hk-exd7m** (which EM-066 escalates) is NOT labeled `codename:pilot`. Recommendation: label hk-exd7m `codename:pilot` OR widen hk-ry8q1. Per the no-create-beads constraint, only flagged.
- **COORD-1 (tracked, non-blocking, no pilot bead):** EV §8.7.6 `drain_summary?` extension; IT-2 consumes it.
- `tasks-review.md` — criteria table; APPROVE; DAG acyclic; both test beads per area gate the impl beads.

### Status: ready

`kerf square` passes on status + dependencies; all pass-output artifacts present (SESSION.md added this pass to satisfy the 19/19 file check).

### Key facts verified against the live tree this session

- `specs/execution-model.md:1391` — loop-top `should_pause_between_runs() -- [operator-nfr.md §4.3] pause-between-runs` (the EXISTING primary gate).
- `specs/operator-nfr.md:211` (ON-008 between-task invariant), `:239` (ON-011 operator-control state machine) — confirm `should_pause_between_runs()` IS the operator-control pause hook (resolution-b basis).
- `specs/reconciliation/spec.md:452` — §4.4 Investigator-agent contract (CL-051 Tier-2 routing target).
- Draft `execution-model.md`: EM-062/063 in §4.13, EM-064/065 in §4.14 (section+ID cross-refs stable despite line shifts).
- Draft `operator-nfr.md` ON-013 (`:259` drain_summary EV request), ON-056 (`:292`), ON-057 (`:303`, single-source-of-truth; `:307` names the fallback path as a consumer, consistent with the reframing).
- Draft `queue-model.md`: §8.1 QM-050 (submit-as-start, the corrected CL-071 anchor), §8.3a QM-052a (handler-pause orthogonality), §8.5 QM-054 (`:710` single-source-of-truth note), §7.1 QM-040 (stream-only append target).
