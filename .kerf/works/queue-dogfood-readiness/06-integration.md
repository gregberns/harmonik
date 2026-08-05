# Integration Review

## Cross-Reference Checks Performed

The pass checked the queue recovery contract across `queue-model`,
`beads-integration`, `process-lifecycle`, `execution-model`,
`run-state-machine`, `operator-nfr`, and `event-model`.

The pass checked the readiness path across `scratch-daemon-runbook`,
`step9-core-loop-artifacts`, `lanes-handoffs`, and the two new operational
records. It checked the final-target mappings in `05-changelog.md`.

The pass also checked the current queue implementation for `Item` fields,
recovery behavior, and the reserved queue validation code. It checked the
event cohort source before asserting a count.

## Contradictions Found

1. QM-052b reset fields that the Item record did not declare. The record now
   declares `attempts`, `last_failure_reason`, and `review_loop_failures`.
   Recovery resets only attempts and last failure. It retains the prior run ID
   and review-loop count, as the current queue model does.
2. Recovery could re-arm a queue item whose Bead remained `in_progress`.
   QM-052b and BI-013f now require one read-only, all-items-open preflight.
   Failure leaves the queue unchanged and emits no recovery event or wake.
3. EM-053a called the normal close ladder while RSM-021 excluded a normal
   outcome. Both now define the drain result: close then `bead_closed` then
   `run_completed`, or reopen then `run_failed`, without `outcome_emitted`.
4. PL-011 treated all just-checkpointed runs as quiescent and allowed a normal
   timeout to force shutdown. It now waits for each committed DOT run's EM-053a
   result. A normal graceful timeout cannot kill or exit before that result.
5. The recovery error surface was incomplete and `-32019` was already used for
   `queue_name_invalid`. QM-029b now maps that existing validation code. QM-052b
   owns the separate typed recovery block `-32020..-32026`.
6. PL-003a named `queue-recover` but omitted its exact records. It now lists its
   request selectors and full success response.
7. The event draft asserted a stale fixed taxonomy count. It now requires an
   implementation to update the current ordinary-cohort count guard and treats
   the registry plus cohort tests as the source of the total.
8. The readiness target lacked inputs and retained artifact locations. The
   step-map now defines `make queue-dogfood-readiness`, its inputs, checks, and
   evidence directory. It also defines the read-only validator target.
9. The scratch procedure relied on operator intent. It now requires validator
   rejection for invalid evidence and assessor acceptance of the retained,
   passing validation record. Its canary is named as pre-fleet scratch work.
10. The event contract referred to a count guard that did not exist. EV-027 now
    requires `TestCrossBusEventTypeCohortCount` with a named `wantCount` and
    requires the adding work to create or update it. EV-050 and
    `queue_recovered` use the same named guard.
11. EM-031b treated a branch ahead of its dispatch head as enough restart
    evidence. That shape cannot recover the chosen merge target or remote
    worker endpoint. It now requires an immutable Git-backed release claim in
    the final pre-release transition checkpoint. Recovery reads the claim and
    current Bead state before it takes any release action. JSONL and a
    daemon-local registry cannot provide release facts. A missing or invalid
    claim preserves the branch and routes to reconciliation.

## Consistency Issues Found

`queue-recover` is consistently distinct from operator resume, drain release,
and handler resume. `queue_recovered` is the only recovery observation.

The release claim is the shared boundary between graceful drain and restart
reconstruction. T5a writes it. T6 consumes it. The claim stores the dispatch
head, resolved merge target, and optional remote endpoint in the immutable Git
checkpoint. This removes the prior unstated dependency on daemon-local state.

The scratch readiness canary is now explicitly pre-fleet. It does not name or
authorize the later fleet canary. The validator does not call Beads or the
fleet daemon.

The current `LANES.md` copy says this work is shelved at Decompose. The draft
target must update that stale status at finalization. This pass does not edit
the operator-owned source file.

## Cross-Reference Validity

Changed-contract links resolve to the drafted component files and their named
anchors. The integration pass found legacy missing-target links in copied
source material. They are outside this work's edits and remain a tracked
finalization concern: `docs/components/external/beads.md`, `docs/conc-a.md`,
`docs/conc-f.md`, `docs/regate-seq.md`, `specs/config-inventory.md`,
`specs/quality-checks.md`, and `specs/testing.md`.

The two new operational-record targets do not yet exist in the worktree. This
is expected before finalization. `05-changelog.md` names them explicitly.

## Changelog Verification

`05-changelog.md` maps every draft to its final target. It marks the ledger
event and durability-proof records as new operational documents. It maps the
full copied `LANES.md` and `DECOMPOSITION-MAP.md` drafts back to their tracked
plan files. No draft is omitted.

## Final Assessment

The queue-dogfood readiness contract is coherent after the corrections above.
It defines durable recovery, safe committed-DOT drain, Git-backed restart
inputs, retained local evidence, and independent assessment without
fleet-daemon operation. The release-claim amendment needs the next
cross-boundary review before finalization. No integration reviewer started a
daemon or changed operational state.

---

## Folded in from lane bravo's parallel pass (2026-08-03)

Lane bravo ran the same pass on branch `work/queue-dogfood-readiness` before any
lane contract named that branch. The two passes reached the same shape. Bravo's
text is kept below because it names evidence, measurements and review records
this document does not. The plan of record stays T1..T12 plus T5a in
`07-tasks.md`. Where the two disagree on behaviour, the section above wins.

### Integration — Queue dogfood readiness

#### Cross-reference checks

Checked the drafted identifiers against the source corpus and the affected
specification set. `QM-058`, `QM-059`, `EM-053a`, `PL-032`, `PL-033`, `ON-052`,
`WM-041`, and `EV-051` are new identifiers in the drafts. Existing links in
each complete draft are preserved from its source document. The affected-spec
names occur across the current corpus, so the amendment set does not orphan a
referenced specification.

> **Correction 2026-08-05.** Every identifier in the list above came from the
> second, superseded changelog table, and the finalize landed all of them
> against the tiebreak this work states. They are being withdrawn one file at a
> time. `ON-052` is retired in `specs/operator-nfr.md` and its number is not
> reusable. `PL-033` is retired in `specs/process-lifecycle.md`. `WM-041` is
> retired in `specs/workspace-model.md`, so the consistency-check sentence
> below that says "WM-041 applies only to the retained terminal ladder" no
> longer describes a live rule. `EV-051` is retired in `specs/event-model.md`,
> and no recovery event is registered in its place, so the sentence below that
> says "`queue_recovered` is the only recovery observation" describes no live
> event. Read this section as a record of what the losing lane drafted, not as a
> list of live requirements. Bead: hk-6lt60.

#### Consistency checks

- Queue-model owns failed recovery and its receipt.
- Execution-model owns the durable terminal-recovery record.
- Run-state-machine owns the in-process terminal spine.
- Operator NFR and process lifecycle own ordering and command behavior.
- Workspace-model owns retained recovery evidence and adoption.
- Event-model owns observation after persistence.
- The assessor schema and runbook own the restricted audit input.

The selected terminology is consistent: *terminal-recovery record* means the
durable post-commit record, *recovery receipt* means the durable failed-queue
result, and *readiness gate* means the separate assessor audit. Neither record
is an event-log substitute.

#### Resolved apparent contradictions

The prior `queue resume` behavior remains drain resume. PL-032 adds a distinct
failed-recovery surface. Existing reopen behavior remains a new run and
worktree. WM-041 applies only to the retained terminal ladder. The general
scratch harness remains broad; its new readiness procedure is narrow.

#### Changelog and corpus assessment

`05-changelog.md` accounts for all nine drafts. The source-spec list was
checked for every affected name and the draft changes introduce no removed
target. The full amendment set is coherent and ready for task decomposition.
