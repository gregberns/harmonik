# CQ-02 card review

- Reviewer: `/root/rl01_final_review`
- Profile: `sol_xhigh`
- Final verdict: `APPROVE`
- Reviewed: 2026-07-27

## Review history

The first pass returned `REQUEST_CHANGES` for three execution defects:
reviewer selection was incorrectly delegated to the worker, the evidence
validator accepted empty contract sections and pending reviews, and the lease
proof omitted uncommitted candidate changes.

## Approved result

The final card:

- pins the `queue-transaction-contract` kerf work and keeps the separate
  `durable-state-contract` work read-only;
- limits writes to that kerf work, `specs/queue-model.md`, and CQ-02 evidence;
- requires explicit topology, one mutation owner, transaction/result
  protocols, every CQ-00 crash cut, event/migration ordering, and
  non-overlapping implementation slices;
- records the kerf-local-storage finalization exception;
- requires coordinator-routed, independent durability and composition reviews;
- validates non-empty evidence, approved reviewers, and both pre-commit and
  post-commit lease scope.

The index record matches the card and `git diff --check` was clean. No files
were edited by the reviewer.

## Writer note — completion authority amendment (2026-07-27)

This historical review verdict is unchanged and is not reused as approval of
the amendment. The task-card writer subsequently replaced the proposed
event-log/effect-key/segmented-replay completion mechanism with a queue-owned
completion receipt:

- QM-053 durably creates a receipt keyed by `(queue_id, receipt_id)` and bound
  to the exact completed canonical digest before CAS-safe unlink;
- retained receipts permit same-name reuse, survive restart, and use bounded,
  crash-safe GC without gating admission;
- `queue_group_completed` is a one-attempt normal-path observation with no
  pending/ack state or restart replay, and EM-015f receives the corresponding
  narrow clarification;
- the card removes CQ-EFFECT, segmented event storage, ReplayCursorV2,
  `ScanAfter`/reader migration, and global effect-key consumer work while
  preserving general replace/archive intents and all non-event findings.

Fresh independent card and final-draft reviews are still required by the
amended CQ-02 card.

## Independent completion-authority amendment review (2026-07-27)

- Reviewer: `/root/cq02_round6_design_review`
- Scope: uncommitted CQ-02 card amendment and the appended writer note only
- Verdict: `REQUEST_CHANGES`

The architectural direction is correct. A queue-owned completion receipt
resolves the QM-003/QM-033 conflict without making observational JSONL
authoritative under EV-021/EV-022. The fourth-spec expansion is necessary:
EM-015f currently says unconditionally that the daemon `MUST emit`
`queue_group_completed`, so a narrow normal-path-versus-crash-delivery
clarification is required. The card otherwise bounds that expansion to
EM-015f, preserves the semantic last-item-terminal ordering, retains the
semantic-claim/execution-baseline and root-single-writer safeguards, and
removes the stale CQ-EFFECT, segmented-log, ReplayCursorV2, ScanAfter, and
repository-reader migration direction.

Four execution defects remain.

### F1 — receipt identity is not durably fixed before the first ambiguous cut

Decision 7 requires a daemon-minted `receipt_id` that is “created once for the
completion transaction,” but Decision 5's durable replace-intent contents do
not record that ID or the exact selected receipt path/bytes. The crash matrix
then requires recovery from completed canonical state with no receipt and from
receipt rename/parent-sync indeterminacy.

After a crash in either state, volatile memory cannot tell recovery which
UUIDv7 was the once-selected receipt ID. Minting another ID can leave two
receipts if the first rename survived an indeterminate parent sync; scanning
for any record under the queue-ID directory does not prove which record was
selected and is not specified. This violates the one-receipt acceptance and
reintroduces volatile restart identity.

Require one of these exact contracts before approval:

- make `receipt_id` a deterministic identity equal to or derived
  collision-free from the already-durable completion transaction ID; or
- durably bind the selected receipt ID, path, exact canonical receipt bytes,
  and digest in the completion replace intent before canonical installation,
  and retain that intent through receipt durability and canonical unlink.

The crash matrix and evidence validator must then prove that exact identity at
receipt create/rename/sync/restart cuts.

### F2 — the new nested receipt namespace has no complete durability matrix

The selected path
`.harmonik/queues/.completion-receipts/<queue_id>/<receipt_id>.json`
introduces two directory levels that may not exist. The required crash matrix
covers receipt file create/write/fsync/close/rename and one parent
open/sync/close sequence, but does not cover creation of
`.completion-receipts`, creation of `<queue_id>`, or durability of each new
directory entry in its own parent.

The card must require exact mkdir/already-exists classifiers and parent-sync
cuts for every newly created directory level, including cleanup of an empty
per-queue directory. Otherwise “receipt durable before canonical unlink” is
not executable on first completion, and GC can leave unclassified directory
state.

### F3 — making the final event optional strands a production completion waiter

`cmd/harmonik/run_via_daemon.go` `viaWatchGroupCompletion` blocks on
`queue_group_completed`/`queue_paused`; the fresh-submit success path has no
other completion source. Under the amended contract, an ordinary append
failure may leave the final event absent while the daemon and subscribe
connection remain healthy, so this command can consume heartbeats forever
after the queue has durably completed and unlinked.

The card inventories `viaSubmitOrAppend` from CQ-00 but does not assign or
prove the corresponding completion-wait migration. Add an explicit,
non-overlapping implementation slice and failing-first acceptance showing that
the daemon-backed run command terminates correctly when the final event is
absent. Its authoritative fallback must query a queue-owned status/receipt
surface by queue ID (including same-name reuse), not scan JSONL. Add the slice
to the DAG before any card that enables crash-optional final delivery.

### F4 — stale receipt “ack/dedupe” language and conditional payload shape are unresolved

Decision 10 says `CQ-RECEIPT` owns receipt `create/read/ack` operations and
receipt-ID payload “dedupe wiring.” Those terms contradict the same card's
immutable receipt, no persistent delivery acknowledgement, no replay, and no
new dedupe-substrate rules. They must be removed or replaced with the exact
CAS cleanup/observation operation actually intended.

Also, `queue_group_completed` occurs for non-final successful groups and for
`complete-with-failures`, neither of which creates a completion receipt. The
card says the event “gains” `completion_receipt_id` but never specifies its
wire rule. Require it to be optional and producer-present exactly for the
final `complete-success` observation governed by QM-053, and absent for every
non-final or failed group completion. Add this conditional rule to the
evidence schema/validator and compatibility proof.

Finally, because execution-model is now a fourth full-file draft, add a
machine-checkable scope proof that its semantic diff is confined to EM-015f
(plus required frontmatter/changelog bookkeeping). The current validator only
checks that an `allowed_delta` string contains “EM-015f”; it does not constrain
the actual draft.

No card text was edited by this reviewer.

## Writer disposition — independent amendment review findings (2026-07-27)

The independent review's `REQUEST_CHANGES` verdict above is preserved exactly;
this writer disposition does not replace it or assert approval.

- **F1 addressed.** Final `complete-success` now preallocates one receipt ID
  and durably binds its exact flat basename, schema version, canonical bytes,
  and exact-byte digest in the replace intent before completed canonical
  installation. The intent survives through receipt and unlink-directory
  durability, and restart must reuse it rather than mint, rebuild, or scan.
- **F2 addressed.** The receipt layout is flat under one
  startup-established `.completion-receipts` directory. The card classifies
  root mkdir/EEXIST verification, queues-parent fsync, receipt
  temp/fsync/rename/receipt-root fsync, and flat-file GC unlink/root-fsync
  cuts; no per-queue directory exists.
- **F3 addressed.** New non-overlapping `CQ-RUN-WAIT` owns
  `viaWatchGroupCompletion`. Fresh-submit waiting queries exact queue-ID live
  status or an authoritative retained receipt after setup and on heartbeat,
  never JSONL or a newer same-name queue. The gated spine is
  `CQ-02I → CQ-RECEIPT → CQ-RUN-WAIT → JR-03`, with failing-first
  absent-final-event and same-name-reuse coverage.
- **F4 addressed.** Receipt operation wording now names intent binding,
  create/read/CAS cleanup/retention/GC rather than delivery bookkeeping.
  `completion_receipt_id` is optional, present exactly on final
  `complete-success`, and absent otherwise. The evidence validator enforces
  that rule and a full-file machine comparison limits execution-model changes
  to frontmatter version/date, EM-015f, and one qualifying revision row.

Fresh independent review is still required.

## Round 2 independent completion-authority review (2026-07-27)

- Reviewer: `/root/cq02_round6_design_review`
- Scope: corrected uncommitted CQ-02 card and prior F1–F4 dispositions
- Verdict: `REQUEST_CHANGES`

The correction closes most of the prior review:

- **F1 resolved.** Final completion preallocates the receipt ID and durably
  binds the exact flat basename, schema version, canonical receipt bytes, and
  exact-byte digest in the replace intent before completed-canonical install.
  The intent remains through receipt and unlink-directory classification, and
  recovery is explicitly forbidden from minting, rebuilding, or scanning.
- **F2 resolved.** The flat receipt namespace removes the per-queue directory.
  Startup owns root creation/EEXIST validation plus queues-parent durability,
  and receipt installation/GC each classify their receipt-root namespace and
  parent-sync cuts before readiness or success.
- **F4 receipt/event semantics resolved.** The stale receipt ack/dedupe
  vocabulary is gone. `completion_receipt_id` is optional, present exactly on
  the final `complete-success` observation, and absent on non-final success
  and every failure observation. The evidence validator checks this literal
  rule.
- The architecture remains bounded: QM-owned facts decide completion,
  restart, cleanup, and same-name admission; EV-021/EV-022 remain unchanged;
  EM expansion remains limited to the necessary EM-015f clarification; no
  CQ-EFFECT, segmentation, cursor, or reader-migration work returned.

Two executable defects remain.

### R2-1 — F3 is only fixed for fresh submit; the append waiter can still hang

Decision 10 explicitly leaves the shared append path outside `CQ-RUN-WAIT`.
That path uses the same `viaWatchGroupCompletion` function and waits on
per-bead run events plus `queue_group_completed`/`queue_paused`. Under the
amended contract the group event is a one-attempt observation for every
group, not only a fresh-submit final group. A crash or append failure may omit
it. Missing per-bead run observations are already an accepted case in the
function—the current code falls back to group outcome only when the group
event arrives—so a healthy heartbeat stream can still keep an append caller
alive forever.

Expand `CQ-RUN-WAIT` to both branches. On an appended call, the queue-ID status
surface must inspect the exact watched group and provide its durable
group status/counts; a final receipt is the fallback when the whole queue has
already unlinked. Preserve current own-bead attribution when all watched run
outcomes were observed, and preserve the current group-outcome fallback when
some were not. Add failing-first cases for:

- non-final watched group completed with its group event absent while a later
  group remains live;
- final watched group completed with event absent and only its receipt
  remaining;
- paused-by-failure with `queue_group_completed` and/or `queue_paused` absent;
- same-name reuse before each fallback query.

The optional-delivery gate must cover this expanded slice before JR-03.

### R2-2 — the EM-only scope checker exempts the document preamble

The Ruby scope proof's frontmatter regex captures the document title and
opening code-fence text in `source_fm[:prefix]` / `draft_fm[:prefix]`, but the
normalizer replaces each complete match with the same placeholder without
first comparing those prefixes. A worker could therefore alter or remove
`# Execution Model` or the opening YAML fence and still pass the claimed
“EM-015f only” proof.

Compare the source and draft frontmatter prefixes literally before
normalization (and retain the existing comparison of all non-version/date
frontmatter fields). The proof must reject every change outside version/date,
EM-015f, and the one qualifying revision row, including the document
preamble.

No card text was edited by this reviewer.

## Writer disposition — Round 2 findings (2026-07-27)

The Round-2 `REQUEST_CHANGES` verdict and all earlier review history remain
unchanged. This note records writer actions only and is not a review verdict.

- **R2-1 addressed.** `CQ-RUN-WAIT` now covers every fresh-submit and
  shared-append `viaWatchGroupCompletion` path. Exact queue-ID live status
  supplies the watched group's durable status/counts/items; an exact-ID final
  receipt proves success through its final group index after unlink. The card
  specifies continuing and terminal outcomes, preserves own-bead attribution
  when all run observations arrived, uses group/receipt outcome when any are
  missing, isolates every query from same-name reuse, and requires
  non-final/final/failure missing-observation tests before `JR-03`.
- **R2-2 addressed.** The execution-model checker now compares
  `source_fm[:prefix]` and `draft_fm[:prefix]` literally before frontmatter
  normalization, so title, opening fence, and every other pre-frontmatter byte
  cannot drift.

Fresh independent review is still required.

## Final focused independent re-review (2026-07-27)

- Reviewer: `/root/cq02_round6_design_review`
- Scope: R2-1/R2-2 corrections plus architecture-scope regression scan
- Verdict: `APPROVE`

R2-1 is resolved. `CQ-RUN-WAIT` now owns every fresh-submit and shared-append
`viaWatchGroupCompletion` path. Each watcher queries the accepted queue ID
after setup and on every heartbeat, inspects the exact watched group, and
never falls through to a newer same-name queue or JSONL. The outcome contract
covers:

- pending/active continuation;
- non-final durable success while later groups remain live and the group
  observation is absent;
- final success after unlink with only the exact-ID receipt retained;
- complete-with-failures and paused/cancelled states with group and/or pause
  observations absent;
- complete and incomplete own-bead observation sets on shared append;
- missing/corrupt/unsupported/identity-inconsistent state, invalid receipt
  group range, and transport failure.

The existing own-bead attribution is preserved when all watched outcomes are
known; otherwise the durable watched-group/receipt result is the fallback.
The exact gated spine is
`CQ-02I → CQ-RECEIPT → CQ-RUN-WAIT → JR-03`, and no card may enable
crash-optional group-observation delivery before the expanded waiter lands.
The crash matrix, acceptance text, evidence validator, and final composition
review all enforce this boundary.

R2-2 is resolved. The execution-model scope proof now compares
`source_fm[:prefix]` and `draft_fm[:prefix]` literally before normalization,
then permits only frontmatter `version`/`last-updated`, EM-015f, and one
qualified revision-history row. Title, opening fence, and all other
pre-frontmatter bytes can no longer escape the proof.

The final regression scan found no restored CQ-EFFECT task, effect-key/outbox
delivery state, segmented event storage, ReplayCursorV2, ScanAfter caller
migration, or unrelated reader work. Queue-owned canonical/intent/receipt
facts remain the sole completion, cleanup, restart, and same-name-admission
authority; EV-021/EV-022 remain unchanged; the fourth-spec delta remains
necessary and tightly confined to EM-015f. Receipt identity, flat-namespace
durability, CAS cleanup, retention/GC, conditional
`completion_receipt_id`, lease/baseline safety, and review gates remain
internally consistent and executable.

No card text was edited by this reviewer.
