# extqueue v0.1 — Spec-Draft Critical Review

Scope: 6 drafts under `05-spec-drafts/` reviewed against `01-problem-space.md`, `02-components.md`, the six `04-design/*.md` design docs, and the package-level `05-changelog.md`. Source specs at `/Users/gb/github/harmonik/specs/` were the baseline for drive-by-edit checking.

## Critical issues

**C1. `queue_validation_failed` is a phantom event.** queue-model.md treats `queue_validation_failed` as both an event-bus event and a JSON-RPC error payload, but event-model.md §8.10 registers exactly 6 events and `queue_validation_failed` is NOT among them. The package changelog (line 24) explicitly states the 6-event cohort is the closed set. Concretely:

- queue-model.md:281 — `MUST NOT … emit any event other than queue_validation_failed on failure`
- queue-model.md:288, 299, 311, 322, 333, 344, 370, 382 — eight `queue_validation_failed{...}` literal payload blocks in QM-020..QM-027
- queue-model.md:393 (QM-028) — `MUST emit a queue_validation_failed event per [event-model.md §8.10]` — this reference is unresolvable because §8.10 does not declare such an event
- queue-model.md:397 (QM-029) — defines the `QueueValidationReason` enum on this non-existent event
- queue-model.md:553 (QM-064) — refers to "the QM-028 `queue_validation_failed` event"

This must be resolved before integration. Two viable paths:

1. **Demote to JSON-RPC error only.** Rename `queue_validation_failed{…}` blocks to "JSON-RPC error payload" form, drop QM-028 entirely, and move the `QueueValidationReason` enum into queue-model.md's own JSON-RPC error space (the changelog already says error codes `-32010..-32019` are reserved). operator-nfr.md:1003 changelog already aligns with this reading ("new `queue_validation_failed` failure modes live in queue-model.md's JSON-RPC error space, not in ON §8 exit-code taxonomy"). This is the lower-disruption path.
2. **Add `queue_validation_failed` as a 7th event in event-model.md §8.10.** Requires a §8.10.7 row, a §6.3 payload schema, an EV-027 amendment in the changelog, and reworking the package changelog "6-event cohort" claim.

Recommendation: path 1. The changelog and operator-nfr both already read it as JSON-RPC-error-only; queue-model.md is the only file that contradicts.

**C2. `operator_command_failed` still enumerates `enqueue`.** event-model.md:240 (§8.7.16) lists `command` enum as `pause / stop / upgrade / attach / enqueue`. The `enqueue` operator command is retired per the package invariant; this enum value is now unreachable. event-model-design.md did not enumerate §8.7.16, so it was missed. This is a real contradiction with the cross-spec invariant (`enqueue` retired with no alias). Required edit: replace `enqueue` with `queue-submit / queue-append / queue-status / queue-dry-run` in event-model.md:240, and add a one-line note to the event-model §A.4 v0.5.0 changelog entry.

## Required changes

**R1. operator-nfr.md:139 (ON-001) still lists `enqueue`.** Flagged in `05-changelog.md` as a known residual. ON-001 is the structured-exit-code obligation and is explicitly in scope — every operator-invoked harmonik command. Leaving `enqueue` here contradicts ON-013a (already amended) and ON-050 (already amended). Edit: replace `enqueue` in the enumeration with `queue` (the new operator command per ON-041 step b at L… — uses the `queue` verb with subcommands). This is consistent with ON-041's `queue (with subcommands submit, status, append, dry-run)` formulation.

**R2. Dotted-form method names leak in 4 places.** Per cross-spec invariant: bare-kebab everywhere.

- queue-model.md:52 — `queue.resume` / `queue.remove` / `queue.clear` in §1.2 out-of-scope bullet → use `queue-resume` / `queue-remove` / `queue-clear` (A.3 at L592-594 already uses the correct kebab form).
- event-model.md:1075 — `queue.resume operation` in `queue_paused` payload doc → `queue-resume`.
- event-model.md:1327 — `queue.resume operation` in the §A.4 changelog entry → `queue-resume`.
- (queue-model.md:240, 241; execution-model.md:1086 — `queue.status` here is RECORD field access in pseudocode, NOT a method name; LEAVE AS-IS.)

**R3. `queue-model.md:240 transition table guard column uses RECORD-field syntax confusingly.** "queue.status == active" inside the §5.1 state-machine table appears similar to a method name. Clarify by renaming to `Queue.status` (capitalized) or `queue_status` (field reference style consistent with the §2 RECORDs). Minor but reduces invariant-grep false positives.

**R4. Add a `queue_validation_failed` reconciliation line to 05-changelog.md.** Once C1 is resolved, update the second "Known residuals" bullet (lines 101-102) to record the decision and the file diffs applied.

**R5. event-model.md §8.7.4 `failure_mode` enum gap.** Line 228 lists `queue-format-unsupported` (the Beads/overlay-schema case per ON-016) but the new daemon-startup failure for `.harmonik/queue.json` schema mismatch uses `queue-schema-incompatible` (per process-lifecycle.md:249, 932, 1063). This is a new `failure_mode` value not registered in event-model.md or operator-nfr.md §8. Two options: (a) add `queue-schema-incompatible` to the §8.7.4 row description and a corresponding §8 exit-code entry (probably exit code 14 per PL-005 step 8a), OR (b) reuse the existing `queue-format-unsupported` mode and rewrite process-lifecycle.md's three references. Either way, normative parity is required. Recommend (a) since the two failure_modes describe distinct startup paths (Beads vs. queue.json).

## Optional improvements

**O1. queue-model.md §A.2 cross-spec impact summary** lists "operator-nfr.md §4.6 ON-015" but ON-015 lives at §4.4 of operator-nfr.md per the original spec's heading structure (see queue-model-design.md:5 "operator-nfr.md ON-015 (line 300)"). Verify §4.4 vs §4.6 in the source and align.

**O2. queue-model.md §A.2 cross-spec impact summary** does not include `process-lifecycle.md §4.3` (PL-028 `hk queue` subcommand family) even though process-lifecycle-design lists it. Add a row for completeness.

**O3. queue-model.md §5.9 numbering anomaly.** Section ordering is §5.1, §5.2, §5.3, §5.4, §5.5, §5.6, §5.7, then jumps to §5.9 (line 271). Renumber §5.9 → §5.8.

**O4. event-model.md §8.10 ordering paragraph (L292)** uses prose "(g) `queue_appended` MAY interleave at any time on a stream group whose status is `pending` or `active`" — consistent with queue-model.md §7.4 (QM-043). No change needed; calling this out as a consistency confirmation.

**O5. operator-nfr.md L228 (ON-009a disambiguation note)** is well-written but contains the awkward sentence "the needs-attention set governs which beads an orchestrator MAY enqueue into the execution queue" — the verb `enqueue` here is descriptive English, not the retired command, but ambiguous to future readers. Consider "MAY submit to the execution queue" for clarity.

## Spot-check confirmations (no findings)

- **Design fidelity (3+ target-state directives per spec):** sampled queue-model design §2 (RECORDs), §3 (QM-001/002/003 persistence), §4 (QM-010/011/012 identity), §5 (transition table), §6 (validation rules), §7 (append), §8 (lifecycle), §9 (concurrency). All present in draft, faithfully translated, with §6.11 (QM-029a order of evaluation) added as a draft-pass elaboration consistent with design intent.
- **No drive-by edits:** spot-checked beads-integration.md §4.7 (subprocess timeouts at L430), §4.9 (concurrency at L453) against design doc — unchanged outside the BI-024a/BI-025c re-anchoring the design called out. Spot-checked process-lifecycle.md §4.5 / §4.6 (bind_socket, file-surface) — unchanged outside PL-004 file-surface and PL-027(iii) NON-REGRESSION NOTE the design called out.
- **Cross-reference validity (10 sampled):** queue-model.md → `[event-model.md §8.10]` ✓ (L279), `[operator-nfr.md §4.5 ON-018]` ✓ (L62 — ON-018 lives in §4.5 per spec source), `[execution-model.md §4.3, §7.1]` ✓, `[workspace-model.md §4.6 WM-026]` ✓; event-model.md → `[queue-model.md §4 QM-010..012]` ✓; process-lifecycle.md → `[queue-model.md §3 QM-002]` ✓; beads-integration.md → `[queue-model.md §6 QM-020..QM-022]` ✓; operator-nfr.md ON-018 → `queue execution plan ([queue-model.md §3]…)` ✓. The `[operator-nfr.md §4.1 ON-004]` references (not ON-026) are consistent throughout — the changelog-flagged ON-026 typo did not occur.
- **Method-naming consistency:** 4 dotted forms found (R2 above); all other references are bare-kebab.
- **6-event cohort:** event-model.md §8.10 declares exactly `queue_submitted`, `queue_group_started`, `queue_group_completed`, `queue_paused`, `queue_appended`, `queue_item_deferred_for_ledger_dep` (L283-288). queue-model.md does NOT reference the three dropped events (`queue_item_dispatched/completed/failed`); only `queue_validation_failed` is the contested 7th name (per C1).
- **Filename / structure:** all 6 drafts named `05-spec-drafts/<spec>.md` — match target paths.
- **Formatting / vocabulary:** RECORD/ENUM pseudo-Pascal style, MUST/MAY/SHOULD usage, heading levels all match the corpus convention.
- **Per-spec §A.4 / §12 changelog entries** present and accurate in all 6 drafts; consistent with 05-changelog.md.

## Verdict

**REQUEST_CHANGES.**

C1 (the phantom `queue_validation_failed` event) and C2 (`enqueue` left in `operator_command_failed` enum) are the only blockers — both are scoped, mechanical fixes once the direction is chosen. R1-R5 are required but each is a single-edit change. The bulk of the package is high-fidelity to the design docs and to the cross-spec invariants. Once C1 + C2 + R1-R5 land, this approves for integration.
