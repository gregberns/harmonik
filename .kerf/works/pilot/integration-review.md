# Integration Review Findings — `pilot`

Reviewer pass over `06-integration.md` + all four `05-spec-drafts/*` + the unchanged system-spec corpus. Single round (the finalize critical-review must-fix items were the review inputs).

## Verdict: APPROVE — advance to `tasks`

## Criteria check (per kerf integration gate)

| Criterion | Status | Evidence |
|---|---|---|
| `06-integration.md` lists all cross-reference checks performed | MET | Part 2 enumerates every outbound link per draft, with VALID/FIXED disposition. |
| Contradictions found are documented with resolution | MET | Part 3: six contradiction candidates checked (operator-pause vs handler-pause; loop-top vs EM-067; ON-INV-006 vs ON-056; wave vs stream; submit-as-start; CL-051 vs EM-052/053). All resolved; none outstanding. |
| Consistency issues found with resolution | MET | Part 4 (terminology) + the CL-071 `§9 → §8.1 QM-050` cross-ref fix in Part 2. |
| Integration examined ALL system specs, not just modified ones | MET | Verified targets in unchanged specs: `reconciliation/spec.md` §4.4, `process-lifecycle.md` §4.1/§4.9, `event-model.md` §8.7.6/§6.3, `handler-pause.md` §6 HP-025. |
| Cross-references valid in both directions (nothing orphaned, nothing dangling) | MET | One drifted link found and fixed (CL-071 `§9`). All others resolve. Producer→consumer chain bidirectional. |
| Terminology consistent across corpus | MET | Part 4: `operator_pause_status`, "single source of pause truth", stream/wave, `harmonik supervise pause/resume`, task-vs-run all consistent. |
| Changelog matches actual drafts | MET | Part 5: row-by-row check; EM-067 changelog entry updated this pass to match the reframed spec text. |

## Must-fix items from the finalize critical review

1. **EM-067 redundancy/coherence gap** — RESOLVED via resolution (b): loop-top `should_pause_between_runs()` (= ON-008 operator-control pause hook) is the PRIMARY gate covering all paths; EM-067 reframed as the single-source-of-pause-truth binding + a defense-in-depth re-assert. Spec text (§4.11 EM-067), §7.4 pseudocode comments, §10.2 test obligation, and both changelogs updated. See `06-integration.md` Part 1.
2. **Complete `06-integration.md`** — DONE: the TODO skeleton is replaced with a full integration record that also captures the three design-notes deferred items (CL-051→reconciliation §4.4 routing; EM-062..065 line-range re-verification; ON-013 `drain_summary?` EV coordination).

## Edits applied this pass

- `05-spec-drafts/execution-model.md`: EM-067 rewrite; §7.4 pseudocode comment update; §10.2 test reframe; v0.8.1 changelog row update.
- `05-spec-drafts/cognition-loop.md`: CL-071 cross-ref fix `[queue-model.md §9]` → `[queue-model.md §8.1 QM-050]`.
- `05-changelog.md`: EM-067 entry updated to reframed semantics.
- `06-integration.md`: full population (was TODO skeleton).

## Out-of-bundle coordination items (tracked, non-blocking)

- EV `event-model.md §8.7.6` extension to carry `drain_summary?` (pre-existing ON-013 request; owned operator-nfr → event-model).
- EV event additions already requested by `reconciliation/spec.md` (unchanged by this bundle).
