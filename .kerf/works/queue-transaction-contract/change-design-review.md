# Change-design review

## Round 1

- Reviewer: `/root/cq02_research_review`
- Inputs:
  `04-design/queue-model-design.md`,
  `02-components.md`,
  `03-research/queue-model/findings.md`,
  `specs/queue-model.md`
- Verdict: `REQUEST_CHANGES`

### Finding D1 — design the full migration conflict matrix

The design needed literal actions for valid legacy + invalid canonical,
invalid legacy + valid canonical, and valid divergent copies.

Resolution:

- added a ten-row migration matrix;
- valid legacy + invalid canonical retains both, loads neither, and
  mutation-refuses;
- invalid legacy + valid canonical loads canonical and retains invalid legacy
  unchanged;
- valid divergent copies retain both and fail closed.

### Finding D2 — design each namespace reconciler separately

The design's generic indeterminate recovery could misclassify absence after
unlink or a partial archive rename.

Resolution:

- defined distinct replacement, unlink, and archive classifiers;
- required a parent sync before promotion;
- made expected unlink absence promotable;
- required canonical absent + exact intended archive for archive promotion;
- made archive both/neither/mismatch states quarantine;
- preserved post-sync close as durable plus cleanup diagnostic.

### Finding D3 — add operator cancellation and every archive caller

The design did not own live RPC cancellation or both inline archive regions.

Resolution:

- added `HandlerAdapter.HandleQueueCancel` cancelled persistence, archive,
  clear, audit, and RPC ordering;
- made `RunQueueCancel` an RPC-only client with fail-closed daemon absence;
- included bootstrap-time archive in CQ-CALLER-BOOTSTRAP;
- proposed CQ-CALLER-INLINE-EXIT for post-daemon paused-failure archive;
- proposed CQ-CALLER-CANCEL after CQ-01/CQ-02I to serialize `rpc.go`.

### Finding D4 — publish an exact acyclic implementation graph

The original graph simultaneously placed all callers before JR-03 and
maintenance after JR-03. It also omitted CQ-04's CQ-03/JR-01 evidence ordering
and activation/JR-03 serialization.

Resolution:

- added an explicit edge list with no cycle;
- preserved CQ-04 dependencies CQ-02I + CQ-03 and added JR-01;
- ordered CQ-03 → group activation → JR-03;
- ordered JR-03 → work-loop maintenance;
- ordered bootstrap + JR-03 → inline exit;
- ordered CQ-04 + JR-03 → JR-04.

## Author resolution status

All round-1 changes are applied. A focused round-2 confirmation has been
completed. This Kerf pass review does not count as either final CQ-02
independent review.

## Round 2

- Reviewer: `/root/cq02_research_review`
- Verdict: `REQUEST_CHANGES`
- Prior findings: D1, D3, and D4 resolved; D2 retained one archive-restart gap

### Finding D2.1 — archive recovery depends on volatile destination identity

The archive classifier named an “exact intended archive” but had no durable
way to reconstruct the timestamped destination after process death. Multiple
historical archives made newest-file selection unsafe.

Resolution:

- introduced the queue-specific stable archive-intent sidecar, durable before
  rename;
- specified its source digest/identity, archive kind, and exact destination
  basename fields;
- classified intent write/removal with the existing typed namespace results;
- made canonical absent + exact intent destination the sole archive promotion
  state;
- made exact source canonical + destination absent the sole not-committed
  state;
- made both, neither, invalid intent, mismatch, and third states quarantine;
- required durable intent deletion before clear or same-name resubmit;
- defined missing canonical + missing intent as the clean no-owner state only
  after a successful startup directory sync, with historical archives ignored.

## Author resolution status after round 2

D2.1 is applied. A focused round-3 confirmation was completed.

## Round 3

- Reviewer: `/root/cq02_research_review`
- Verdict: `REQUEST_CHANGES`
- Archive-intent protocol: approved

### Finding D2.2 — legacy `main` can contradict a clean archive result

The clean no-owner rule omitted legacy singleton evidence. A present main
intent must be resolved before migration/load, and invalid/divergent legacy
must not be erased by a canonical-absence shortcut.

Resolution:

- ordered startup as intent classification → main legacy matrix → canonical
  load → terminal/cross-store reconciliation;
- prohibited legacy-to-main creation while a main intent exists;
- after archive durability, durably removes only an equivalent legacy
  duplicate before intent cleanup;
- preserves invalid/divergent legacy with intent and mutation refusal;
- requires legacy absence or durable migration before main is clean;
- keeps historical archives nonblocking.

## Author resolution status after round 3

D2.2 is applied. A focused round-4 confirmation was completed.

## Round 4

- Reviewer: `/root/cq02_research_review`
- Verdict: `REQUEST_CHANGES`
- Legacy/intent ordering: approved except for one absence-durability cut

### Finding D2.3 — legacy absence needs its own parent sync

An absent `.harmonik/queue.json` under a durable main archive intent may be an
unlink that has not survived a crash. Intent removal before
`fsync(.harmonik)` could reopen the archived queue through legacy
resurrection.

Resolution:

- main-intent cleanup always syncs `.harmonik` before removing intent when
  legacy is absent;
- equivalent legacy uses unlink → `.harmonik` sync → intent unlink →
  `.harmonik/queues` sync → clear/admit;
- parent open/sync failure retains intent/refusal;
- post-success parent close failure is diagnostic;
- shutdown, operator cancellation, startup, and the generic archive
  classifier all name the same order.

## Author resolution status after round 4

D2.3 is applied. A focused round-5 confirmation was completed.

## Round 5

- Reviewer: `/root/cq02_research_review`
- Verdict: `APPROVE`

The reviewer confirmed all C1-C7 target states, including archive intent,
legacy composition, cancellation writers, and the acyclic implementation DAG.
Change design may advance to `spec-draft`.

## Round 6 — reopened after spec-draft review

- Reviewer: pending coordinator-routed independent reviewer
- Inputs:
  `01-problem-space.md`,
  `02-components.md`,
  all files under `03-research/`,
  all files under `04-design/`,
  `specs/queue-model.md`,
  `specs/process-lifecycle.md`,
  `specs/event-model.md`,
  `spec-draft-review.md`
- Verdict: `PENDING`

The first spec-draft review found that the approved Round-5 replacement
classifier relied on restart evidence that did not exist. The work was
formally returned to `change-design`; the earlier approval does not authorize
another spec draft.

Author amendments awaiting review:

- made stable per-name `.replace-intent` universal for every ordinary
  canonical replacement;
- recorded exact prior/candidate digests, transaction identity, selected temp,
  Wake requirement, and ordered logical-effect keys/types/payloads;
- required `PrepareQueueEffects` before intent persistence to apply the same
  deterministic EV-035 redaction/canonicalization boundary as normal emission,
  store only exact post-redaction canonical bytes/digests, and prevent
  digest-only collision matches;
- replaced unsafe preallocated envelope IDs with
  `<transaction_id>:<ordinal>` keys in `trace_context.trace_id`; normal
  monotonic event IDs are minted only at actual append;
- specified bounded from-zero exact-key scan, append/torn-tail repair,
  `FlushThroughQueueEffects`, intent create/delete, candidate replacement, and
  restart classifiers, including unsupported-capability/downgrade fail-closed;
- added the proposed `CQ-EFFECT-01` implementation owner for the event-bus
  preparation/exact-key machinery, with `CQ-02 -> CQ-EFFECT-01` and
  `CQ-EFFECT-01 + CQ-MIG-01 -> CQ-02I`; the later `07-tasks.md` pass must
  materialize that dependency before CQ-02I;
- prohibited RPC response persistence/replay;
- made QM-053 the sole final `queue_group_completed` emission owner and added
  non-final effect recovery;
- separated validation/stale `rejected` from marshal/pre-namespace
  `not_committed`;
- specified shutdown after PL-011 drain, sorted per-name continuation, and
  exact `cancelled` archives;
- specified explicit operator plan withdrawal as `failed` archive kind, not a
  Run/Bead terminal operation;
- expanded the Kerf draft targets to process-lifecycle for `queue-cancel`,
  startup/upgrade capability gates, and shutdown ordering;
- expanded the event-model draft target to one typed Class O
  `queue_cancelled_operator` after cleanup/release and before response;
- pinned QueueStatus name-over-ID-over-main selector compatibility and
  changelog/version trace obligations;
- reconciled the problem-space's stale one-spec/event-payload non-goals.

No normative spec has been copied. Return to `spec-draft` requires an
independent Round-6 approval.

### Independent Round-6 result

- Reviewer: `/root/cq02_round6_design_review`
- Verdict: `REQUEST_CHANGES`

The reopened design resolves the seven prior spec-draft findings at the
headline level: it adds restart-visible replacement identity, generic
non-final effect recovery, sole QM-053 final-event ownership, exact result
taxonomy, the PL cancel/startup/upgrade/shutdown surface, the typed Class O
operator audit, and selector/changelog traceability. Four remaining design
failures prevent approval.

#### R6-1 — cancellation loses its archive kind at the replace-to-archive handoff

Artifact: `04-design/queue-model-design.md` §9 “Shutdown after drain” steps
1–2, §10 “Explicit operator plan withdrawal” steps 2–3, and §1 “Paths and
protocol records.”

Both cancellation paths first complete the universal replacement protocol,
including durable deletion of `.replace-intent`, and only afterward create
`.archive-intent`. A crash in that gap leaves only a parseable canonical queue
with `status=cancelled`. The only durable discriminator,
`QueueReplaceIntent.operation_kind`, is gone, and the queue envelope does not
record whether the predecessor was graceful shutdown or explicit operator
withdrawal. Startup §11 therefore cannot choose the required
`cancelled-<queue_id>-<transaction_id>` versus
`failed-<queue_id>-<transaction_id>` archive, nor decide which
operator-cancel completion path applies. The prohibition on replace/archive
intent coexistence removes the obvious handoff state but supplies no atomic
alternative.

Required change: define a durable, crash-classifiable handoff from cancelled
replacement to the exact archive intent, including every create/rename/sync
cut, without a state in which cancellation origin and selected archive
basename are volatile.

#### R6-2 — the restart classifier's prior and candidate cases are not disjoint

Artifact: `04-design/queue-model-design.md` §4
`QueueReplaceIntent.{prior_sha256,candidate_sha256}` and §5
“Replace-intent crash/restart classifier.”

The record permits a present prior and candidate to have identical exact
serialized bytes/digests. In that state the canonical file simultaneously
matches the §5 candidate branch (install/Wake/complete keyed effects) and the
prior branch (require no keyed effect and remove the intent as not committed).
A crash before candidate rename and a crash after an identical-byte rename
are observationally indistinguishable, so branch ordering would either emit
effects for an uncommitted operation or suppress effects for a committed one.
Volatile generation cannot resolve it.

Required change: make the classifier cases mechanically disjoint—for example,
reject/collapse identical-byte replacements before intent creation, or add a
durable phase fact—and add the equal-prior/candidate fixture to the restart
matrix.

#### R6-3 — “writer-exclusive” does not exclude the current second JSONL writer

Artifacts: `04-design/event-model-design.md` §1–§2
`EnsureQueueEffects`/torn-tail repair; current `specs/event-model.md` EV-047;
`04-design/queue-model-design.md` §12 `CQ-EFFECT-01`.

EV-047 explicitly records that the daemon `JSONLWriter` and standalone keeper
`FileEmitter`s append to the same `events.jsonl` from different processes.
The design's JSONL-writer-exclusive operation is only an in-process
serialization boundary. A keeper append can occur after the queue recovery
captures EOF but before it truncates an incomplete tail; truncation can erase
that unrelated complete append, or concurrent bytes can turn the tail into a
newline-terminated malformed record and permanently strand the queue intent.
Thus the promised torn-tail repair and “unrelated interleaving cannot hide a
key” proof do not hold for the normative writer topology.

Required change: define an exclusion/quiescence mechanism shared by every
appender for scan/repair/append/FlushThrough, or another repair protocol that
cannot truncate concurrent writes. Expand `CQ-EFFECT-01` ownership and
conformance to the affected non-daemon writer(s) and exercise real
cross-process interleaving.

#### R6-4 — the DAG has no owner for implementing the new typed cancel event

Artifacts: `04-design/event-model-design.md` §3 and §7;
`04-design/queue-model-design.md` §12; `02-components.md` C7.

The event design requires a new typed, XOR-validating
`queue_cancelled_operator` event and normal registration/compatibility
coverage. Section 12 assigns `CQ-EFFECT-01` only the EventBus,
`busimpl`/`jsonlwriter`, and `TraceContext` mechanism, while `CANCEL` owns only
queue RPC/CLI files. No slice owns the event type constant, typed payload and
`Valid` logic, constructor registration, per-type compatibility row,
taxonomy/durability coverage, or their tests. Those are distinct current
`internal/core` semantic writers, so the later task graph cannot implement the
event without an unclaimed edit or an overlapping lease.

Required change: assign the complete typed-event implementation to
`CQ-EFFECT-01` (or an explicit predecessor) with exact files/symbols, leaving
`CANCEL` to remove the raw append and invoke the registered event after its
durable predecessor.

Because R6-1 leaves a real crash cut unclassifiable, R6-2 leaves the ordinary
replacement classifier non-total, R6-3 can destroy unrelated event evidence,
and R6-4 leaves the DAG non-dispatchable, Round 6 is
`REQUEST_CHANGES`.

### Author resolution of Round-6 findings

The Round-6 finding text above is preserved unchanged. The author applied:

- **R6-1 resolved:** cancellation now selects `archive_origin`,
  `archive_kind`, source/candidate digest, and exact archive basename before
  replace-intent persistence. After cancelled-state/effect durability, the
  replace intent remains while its exact linked archive intent is
  create/write/sync/close/rename/parent-synced. Only then is replace intent
  deletion allowed. Replace-only, exact linked-pair, and archive-only are
  deterministic handoff states; every mismatched pair quarantines. The design
  enumerates every successor-intent and predecessor-deletion cut. An
  originless cancelled canonical is never guessed.
- **R6-2 resolved:** exact present prior/candidate byte equality collapses
  before candidate-temp or intent creation with no Wake/effect. A valid
  replace intent requires unequal digests; an externally observed
  equal-digest intent is invalid/quarantined. Conformance includes the
  collapsed no-intent fixture and fabricated-invalid-intent fixture.
- **R6-3 resolved:** EV-051 now defines persistent
  `.harmonik/events/events.lock` capability `event-log-lock/v1`. Every
  sanctioned primary-log appender holds a shared flock; queue
  scan/repair/append/FlushThrough drains/takes the daemon-local gate then
  holds the cross-process exclusive flock. Keeper `FileEmitter`, handler,
  sentinel, and the legacy cancel raw path are assigned to the shared helper.
  PL startup must quiesce/restart incompatible live standalone writers or
  fail closed without truncation. CQ-EFFECT-01 includes source allowlisting
  and real subprocess keeper/daemon interleaving proof.
- **R6-4 resolved:** CQ-EFFECT-01 now owns the full
  `queue_cancelled_operator` constant, XOR-validating payload, registry
  constructor, compatibility row, exhaustive event cohort/count, Class O
  classification, and tests. CANCEL only invokes the registered event after
  its queue predecessor and removes the raw append.

The approved-card execution baseline is dynamically recorded as
`6a7d1e77cf1fa22b9d1d8cbb030942e4abecfb06`; semantic claim base remains
`4f750c11ab30afd9371405d5d591ae91e153c5c6`, with
`81b09ac7d26af311964013b197996fbbebc710ac` retained as an ancestor/original
lease baseline.

## Round 7 — focused Round-6 correction review

- Reviewer: pending same independent reviewer as Round 6
- Scope: only R6-1 through R6-4 corrections and cross-artifact consistency
- Verdict: `PENDING`

No normative spec, `07-tasks.md`, evidence, production code, or Kerf status
was advanced by these corrections.

### Independent Round-7 result

- Reviewer: `/root/cq02_round6_design_review`
- Verdict: `REQUEST_CHANGES`

R6-1, R6-2, and R6-4 are resolved. The cancellation handoff now records the
exact origin/kind/source/destination before replace-intent persistence, retains
that predecessor until the exact successor is parent-synced, classifies
replace-only/linked-pair/archive-only states and all named handoff cuts, rejects
mismatches and originless cancelled state, and gives raw-corrupt operator
withdrawal its own origin-bearing archive intent. Exact prior/candidate byte
equality now collapses before temp/intent/effects, different bytes with the
same digest reject, fabricated equal-digest intents quarantine, and the
required fixtures are named. `CQ-EFFECT-01` now owns the complete typed
`queue_cancelled_operator` constant/payload/XOR/registry/compatibility/cohort/
Class-O implementation and tests; the later cancel caller only invokes it and
removes the transitional raw helper.

#### R7-1 — mixed-version quiescence still excludes a known standalone writer

Artifacts: `04-design/event-model-design.md` §1 “Add EV-051 queue logical-
effect completion” (sanctioned-writer inventory and capability paragraph);
`04-design/process-lifecycle-design.md` §3 “PL-005 step 8a queue startup
subphase” and §5 “PL-027 upgrade/rollback capability gate”; current
`cmd/harmonik/handler.go` `runHandlerResume` / `emitHandlerResumedEvent`.

The event design correctly inventories handler resume as a sanctioned direct
primary-log appender and assigns its new implementation to the shared-lock
helper. But its capability gate narrows startup/exec quiescence to registered
standalone keeper/FileEmitter processes, and PL repeats the narrow
keeper/FileEmitter failure rule. `harmonik handler resume` is itself a
daemon-independent standalone command: it updates handler state and directly
opens/appends `events.jsonl`, with no keeper registration or daemon socket.
Therefore a still-running invocation from the incompatible old binary is not
covered by the stated proof.

The remaining failure is the same destructive R6-3 cut: an old handler process
can pass the lifecycle gate, ignore `events.lock`, and append after the new
daemon captures EOF while queue recovery holds its advisory exclusive flock.
Tail repair can then truncate that unrelated append, or the concurrent bytes
can make the tail newline-terminated malformed and strand the queue intent.
Updating the target handler source does not quiesce an already-running old
handler, and “every registered standalone writer” cannot prove absence of this
explicitly unregistered writer.

Required change: make mixed-version capability establishment cover every
sanctioned standalone primary-log appender, including in-flight handler-resume
commands (and any equivalent direct helper), or remove their ability to append
outside a daemon-mediated/exclusively governable path. State the mechanically
checkable quiescence rule in event-model and PL, fail closed before scan/repair
when it cannot be proved, and extend the real subprocess proof to an
incompatible non-keeper direct appender. The ordinary capable-binary shared
flock, local-gate → flock order, non-recursive exclusive token, failure-no-
repair behavior, and `CQ-EFFECT-01` source ownership are otherwise coherent.

Because the known handler process still invalidates the cross-process
exclusion proof required by R6-3, Round 7 remains `REQUEST_CHANGES`.

### Author resolution of Round-7 finding

The independent Round-7 finding text above is preserved unchanged. The author
applied:

- **R7-1 resolved:** the design no longer treats a capability marker,
  registration, or inspection of currently live keepers as proof that an old
  copied appender cannot be invoked later. Automatic queue-effect recovery
  that observes a torn primary-log tail now returns typed
  `offline_repair_required`, changes no log byte, appends no effect, retains
  the queue intent, and prevents ready convergence.
- Physical truncation is moved to explicit
  `harmonik events repair --offline`. The command requires daemon/project
  exclusion and an exclusive `writer-epoch.lock`, opens and records the
  primary-log inode/mode, removes all inode write bits and fsyncs that metadata
  before capturing EOF, and performs two stable OS process/open-fd censuses.
  Any writable fd, incompatible process, ambiguity, or unavailable census
  restores the exact mode and truncates nothing.
- The pre-open recovery fd is usable only after the sealed-mode and absence
  proofs. An old process that opened the inode before sealing is caught by the
  fd census; a future old `handler resume` invocation cannot open the
  write-sealed inode even if it ignores both lock files. Repair removes only
  the incomplete final suffix, fsyncs, restores the exact prior mode and
  metadata durability, and never appends queue effects. A later capable daemon
  performs the exact-key rescan/append.
- New keeper, handler-resume, sentinel, transitional cancel, and daemon paths
  still route through the shared `events.lock` helper during normal operation.
  `CQ-EFFECT-01` now owns the offline command, writer-epoch/inode-seal/census/
  restoration mechanisms, direct-appender conformance gate, and incompatible
  non-keeper subprocess proofs.

## Round 8 — focused R7 correction review

- Reviewer: pending same independent reviewer as Rounds 6–7
- Scope: only R7-1 correction and cross-artifact consistency
- Verdict: `PENDING`

The reviewer must verify that:

- no automatic path truncates a torn primary log;
- the offline inode seal mechanically prevents a future incompatible writer
  from opening the log, while the double fd/process census detects an
  incompatible writer that opened it before the seal;
- the proof includes a non-keeper old `handler resume` subprocess, fails
  closed on census uncertainty, and preserves bytes on every refusal/failure;
- exact mode restoration and file/parent metadata fsync cuts are specified;
- current handler-resume and every other sanctioned appender route through the
  shared helper; and
- the complete mechanism and tests have one non-overlapping task owner.

No normative spec, `07-tasks.md`, evidence, production code, Kerf status, or
later phase is advanced pending this review and coordinator clearance of the
broader scope question.

### Supersession note for R6-3/R7-1 author resolutions and Round 8

The reviewer findings remain preserved as historical evidence. The later
R6-3 and R7-1 author resolutions, and the pending Round-8 mission derived from
them, are superseded by the coordinator-approved task-card amendment at
execution baseline
`1347e9605e3dfa6ea3a27f9bcf173e4ee4b4da47`.

R6-1 cancellation handoff, R6-2 equal-byte classification, and R6-4 typed
event ownership remain active. The superseded portion is only the attempted
exact-physical-record guarantee through scan/lock/truncate/offline repair.
Round 8 received no independent verdict and is closed as `SUPERSEDED`, not
approved.

### Author resolution under the amended logical-effect contract

- The replace intent is the acknowledgement authority. It records ordered
  stable effect keys/content and a durable contiguous
  `acked_through_ordinal`.
- Recovery emits only the unacknowledged suffix. Every attempt uses ordinary
  F-class append and a new normal event ID; only returned durable append
  success permits the queue owner to durably acknowledge that ordinal.
- A crash after append/fsync but before acknowledgement may create another
  matching physical attempt. EV-018 producer identity and EV-014b durable
  consumer/projection dedupe make that one logical effect. Same-key
  type/payload/digest mismatch quarantines.
- Raw observation retains physical count/order. Logical projection exposes
  one effect in transaction ordinal order across distinct IDs and survives
  projection restart. Synchronous durable mutation requires a sink that is
  itself durably idempotent by effect key.
- Torn active-segment bytes are never scanned for completion, truncated, or
  repaired. Rollover preserves the segment, records last-valid-newline and
  rollover-EOF framing offsets, and selects one directory-durable new active
  segment.
- A writer holding the pre-rename inode may complete afterward. Raw replay
  includes completed records across retired and active segments using the
  framing offset; logical replay dedupes/orders the combined set. No prior
  segment is assumed immediately byte-frozen.
- CQ-EFFECT-01 owns the optional effect-key envelope, prepared F append,
  durable logical consumer/projection behavior, collision quarantine,
  synchronous-mutator restriction, append-only segment rollover/replay, and
  the already-retained typed operator audit. CQ-02I owns durable intent prefix
  acknowledgement/classification.
- CQ-02 now explicitly excludes full-log effect lookup, exact physical
  uniqueness, truncation, offline repair CLI, cross-process append lock, and
  writer census.

## Round 9 — amended logical-effect change-design review

- Reviewer: pending independent reviewer
- Scope: the amended logical-exactly-once/physical-at-least-once design and
  cross-artifact consistency; historical findings are read-only
- Execution baseline:
  `1347e9605e3dfa6ea3a27f9bcf173e4ee4b4da47`
- Semantic claim base:
  `4f750c11ab30afd9371405d5d591ae91e153c5c6`
- Verdict: `PENDING`

The reviewer must verify:

- the intent, not JSONL, is the sole acknowledgement authority;
- F append precedes durable contiguous-prefix acknowledgement at every cut;
- append-before-ack physical duplicates use the same key/content and distinct
  normal event IDs, while logical consumers apply once;
- EV-018/EV-014b collision, projection-restart, raw/logical count/order, and
  synchronous durable-mutation restrictions are complete;
- response replay and the unkeyed Class O operator audit are excluded;
- torn bytes remain unmodified and segment create/rename/metadata/parent-sync
  cuts select one active segment or fail closed;
- an old-writer subprocess holding the pre-rename inode can append a completed
  record after rollover, and raw/logical replay includes completed records
  across quarantined plus active segments using the durable framing offset;
- no active artifact retains a full-log lookup, truncation, repair CLI,
  cross-process lock, or writer-census dependency;
- cancellation handoff, equal-byte collapse, and typed-event ownership remain
  intact; and
- CQ-EFFECT-01 and CQ-02I have complete, non-overlapping ownership.

No normative spec, `07-tasks.md`, evidence, production code, Kerf status, or
later phase advances before this independent verdict and coordinator
clearance.

### Worker-spawned Round-9 advisory review

- Reviewer: `/root/cq02_contract/cq02_round9_design_review`
- Authority: advisory self-check only; worker-selected reviewer cannot satisfy
  the coordinator-routed independent gate
- Verdict: `REQUEST_CHANGES`

The advisory reviewer found four active inconsistencies:

1. queue research C5 still allowed unlink when a final event record was
   physically present rather than when the final intent ordinal was durably
   acknowledged;
2. its risk inventory still permitted an event-log completion lookup;
3. its C4 summary placed durable intent before candidate-temp durability; and
4. event design omitted exact provisional-rollover-record to permanent-
   metadata rename/open/sync/close cuts.

### Author resolution of advisory findings

- C5 now requires the final ordinal in the replace intent's durable
  acknowledged prefix before unlink.
- Recovery now explicitly never inspects event history for delivery progress.
- C4 orders marshal/preparation → candidate temp create/write/fsync/close →
  durable intent → canonical rename/parent sync → install.
- The rollover matrix now classifies metadata rename failure, exact
  provisional/permanent presence, parent open/sync failure, and
  post-directory-sync close diagnostics.
- Replay additionally uses per-segment identity/offset cursors and rechecks
  retired EOFs, so a late completed old-inode append remains discoverable
  even after an earlier replay reached that segment's prior EOF.

Round 9 remains `PENDING`. The coordinator must route the independent reviewer;
this advisory verdict and correction do not advance the gate.

### Coordinator-independent Round-9 result

- Reviewer: `/root/cq02_round6_design_review`
- Authority: coordinator-routed independent change-design gate
- Verdict: `REQUEST_CHANGES`

The amended design resolves the seven prior spec-draft findings at design
level. Shutdown has a sorted post-drain owner; QM-053 is the sole final-effect
owner; non-final effects use the same recoverable outbox; rejection and
pre-intent failure are disjoint; restart has exact durable prior/candidate and
acknowledgement evidence; cancel has complete queue/PL/event surfaces; and
selector/changelog obligations are explicit. The intent is now the sole
acknowledgement authority, preparation persists only one-time post-redaction
bytes/digest, F append precedes a durable contiguous-prefix acknowledgement,
final unlink requires the final acknowledgement, append-before-ack may repeat
physical records, logical consumers dedupe/collision-check stable keys, the
operator audit/response are not replayed, and the old scan/truncate/repair/
lock/census design is excluded.

Three rollover/replay defects still prevent approval.

#### R9-1 — the rollover-EOF framing rule loses an append that completes across R

Artifacts: `04-design/event-model-design.md` §4 “Append-only torn-segment
rollover”; `03-research/event-model/findings.md` “Torn-tail safety”;
`02-components.md` EV2.

The design records L as the last valid newline and R as the observed rollover
EOF, then tells retired-segment replay to ignore the preserved interval L..R
and treat R as a fresh framing boundary. That handles a later whole append
whose first byte is at R, but not the explicitly allowed old-descriptor append
that was already in progress when R was captured. If its prefix occupies
L..R and the writer later appends the remainder plus newline after R, the
completed physical record is L..new-newline. The prescribed replay discards
its prefix and parses only the suffix beginning at R, classifying it malformed
and losing that completed unrelated event from both raw and logical replay.
The claim that every completed late old-inode record remains replayable is
therefore false for a required concurrency cut.

Required change: define non-destructive retired framing that can recognize a
record completed across R—for example, re-evaluate the candidate line from L
through the first later newline whenever the retired EOF grows, accepting it
when the complete envelope is valid and otherwise quarantining that line
before resuming later framing. Specify how the cursor revisits that candidate
without double delivery and add a subprocess cut where an old descriptor
writes the prefix before R and completes the same record after active-path
replacement.

#### R9-2 — initial rollover-record and retired-link durability cuts remain underclassified

Artifact: `04-design/event-model-design.md` §4 steps 2–6 and its
“Rollover recovery” table.

The advisory correction fully classifies provisional-record to permanent-
metadata promotion, but not initial creation of the provisional rollover
record. Step 3 compresses create/write/fsync/close/rename/parent-sync into
“durably create,” while the first table row classifies every
rollover-record failure before durability as `not_committed`. After the
provisional record has been renamed into its selected namespace but parent
open/sync fails, that result is indeterminate, not definitely uncommitted;
restart may observe either absence or the exact record. The table supplies no
exact absent/present/third-content reread rule for this cut. Likewise, after
the retired hard link is created, the table says only “sync if needed” and
does not classify link-parent open failure, sync failure, or close-after-
successful-sync, despite those cuts deciding whether the link is the durable
old-inode protection before active replacement.

Required change: enumerate provisional-record create/write/file-sync/close/
rename/parent-open/parent-sync/parent-close and retired-link create/
parent-open/parent-sync/parent-close cuts. A post-rename/link pre-parent-sync
failure must be indeterminate and resolved from exact path/content/inode
identity; successful parent sync must remain durable despite later close
failure. State which exact temp/record/link artifacts are retained or cleaned
in every branch.

#### R9-3 — the promised per-segment replay cursor has no implementable API or task owner

Artifacts: `04-design/event-model-design.md` §3–§4;
`04-design/queue-model-design.md` §12 `CQ-EFFECT-01`; current normative
`specs/event-model.md` EV-047 and current
`internal/eventbus/jsonlwriter.go` `ScanAfter`,
`internal/eventbus/eventbus.go` / `busimpl.go` `ReplayFrom`, and
`internal/replay/replay.go` `Replay`.

The design correctly says a single “retired means closed” watermark is
insufficient and requires a durable cursor per segment identity and byte
offset with retired EOF rechecks. But it defines neither that cursor record
nor a replay API carrying it. The current declared surfaces accept only one
`EventID` watermark. EV-047 also records independent per-process ID generators,
so a completed late old-inode event can have an ID below a consumer's already
persisted active-segment watermark. Merely teaching `ScanAfter` to enumerate
segments would still filter that event out forever.

`CQ-EFFECT-01` owns `jsonlwriter.go`, `busimpl.go`, and new
`jsonlsegments.go`, but does not name the cursor schema/API or the production
`internal/replay/replay.go` caller (nor disposition of other single-watermark
callers). Implementing the promised restart-safe raw/logical replay therefore
requires unclaimed interface and caller edits, while retaining the old API
cannot satisfy the late-write proof.

Required change: define the per-consumer/raw-replay segment-cursor schema,
durability/commit rule, and replacement/additive replay API; state how it
composes with or supersedes EV-047 `since EventID` semantics; and expand
`CQ-EFFECT-01` to the exact implementation/caller files and migration tests.
Conformance must checkpoint active and retired offsets, then append through an
old descriptor with an event ID below the active watermark and prove the next
incremental replay still returns it once physically and once logically.

Because R9-1 can lose a completed event, R9-2 leaves namespace mutations
misclassified, and R9-3 leaves the required late-write replay contract
non-dispatchable, Round 9 is `REQUEST_CHANGES`.

### Author resolution of coordinator-independent Round-9 findings

The independent finding text above is preserved unchanged. The author applied:

- **R9-1 resolved:** retired replay frames from durable
  last-valid-newline L. Rollover EOF R is evidence only, never a parse
  boundary. An unterminated candidate keeps its cursor at L and is
  re-evaluated when EOF grows. A valid envelope completed across R is
  delivered; a newline-terminated invalid line is quarantined as one exact
  interval and replay resumes after its newline, so it cannot hide later
  complete records. Conformance writes a prefix before R and suffix/newline
  after active replacement through the old descriptor.
- **R9-2 resolved:** the rollover table separately classifies new-segment
  temp, provisional-record temp create/write/fsync/close, record rename,
  parent open, parent sync, parent close, retired hard-link creation, link
  parent open, link parent sync, and link parent close. Every namespace
  success before parent sync is indeterminate and resolved by exact
  record/path/inode identity. Parent-sync success remains durable despite a
  close error. Evidence/temp/link retention and cleanup rules are explicit.
- **R9-3 resolved:** EV design now defines `ReplayCursorV2`, `SegmentID`,
  `SegmentCursor`, `ReplayRecord`, `ScanSegments`, `ReplayFromCursor`, and
  `MigrateEventIDCursor`, plus default durable cursor path and atomic
  generation replacement. Migration snapshots each segment EOF; after V2
  durability, bytes are filtered only by segment offset, never EventID.
  Therefore a late retired-inode record below the active EventID watermark is
  returned by the next replay. The direct production caller inventory is
  partitioned into file-disjoint `CQ-EFFECT-01`,
  `CQ-EFFECT-DAEMON-01`, and `CQ-EFFECT-READERS-01`, all predecessors of
  CQ-02I.

## Round 10 — focused Round-9 correction review

- Reviewer: pending same coordinator-routed independent reviewer as Round 9
- Scope: R9-1 through R9-3 corrections and cross-artifact consistency only
- Verdict: `PENDING`

The reviewer must verify:

- the evolving candidate always starts at L; a prefix-before-R/suffix-after-R
  valid record is delivered once per cursor progression, while an invalid
  newline-delimited fragment cannot hide later complete records;
- every provisional-record and hard-link file/namespace/parent durability cut
  is classified, with no post-namespace/pre-parent-sync result called
  `not_committed`;
- V2 cursor schema, durability API, EventID migration, and all direct
  production replay/projection callers have exact owners;
- after cursor migration and active-watermark advance, a late old-inode event
  with a lower EventID is returned by the next raw replay and applied once by
  the logical projection; and
- no previously approved cancellation/equality/typed-event/logical-delivery
  result regressed.

No normative spec, `07-tasks.md`, evidence, production code, Kerf status, or
later phase advances pending the Round-10 verdict.

### Coordinator-independent Round-10 result

- Reviewer: `/root/cq02_round6_design_review`
- Authority: coordinator-routed independent focused gate
- Verdict: `REQUEST_CHANGES`

R9-1 and R9-2 are resolved. Retired parsing now remains at durable L while a
candidate is unterminated, re-evaluates L through the next newline when EOF
grows, accepts a valid envelope completed across R, and quarantines one
newline-bounded invalid interval before continuing. The rollover table now
classifies new-segment and provisional-record temps, provisional-record
rename/parent cuts, hard-link creation/parent cuts, active replacement, and
metadata promotion; namespace success before parent sync is indeterminate,
parent-sync success survives close failure, cleanup is evidence-preserving,
and the active path is never absent. The previously approved queue outbox,
cancellation handoff, equality collapse, typed audit, and logical-delivery
rules did not regress.

R9-3 remains incomplete in two material ways.

#### R10-1 — EventID-to-V2 migration has no crash-safe backlog/cursor commit rule

Artifacts: `04-design/event-model-design.md` §3.1 “Segment cursor and
EventID-watermark migration”; `03-research/event-model/findings.md`
“Consumer and observer semantics”; current normative
`specs/event-model.md` EV-047.

`MigrateEventIDCursor` snapshots each segment EOF, treats records at/below the
legacy EventID watermark as consumed, returns records above it as
`migration_backlog`, and returns one `SegmentCursor.next_offset` per segment.
The design does not state the candidate offsets or durability order relative
to backlog delivery. Because EV-047 permits independent per-process IDs,
“consumed by EventID” is not necessarily an offset prefix: one segment may
contain `ID > watermark` followed by `ID <= watermark`.

If the returned cursor advances to captured EOF and is made durable before
the backlog is accepted, a crash loses the above-watermark record forever.
If it stops before the first backlog record, later at/below-watermark records
are redelivered despite being described as seeded consumed. Either policy can
be made safe, but the current contract chooses neither and supplies no atomic
or resumable migration phase joining backlog acceptance, cursor generation,
and retirement of the legacy watermark.

The same schema identity is underspecified across rollover:
`SegmentID` includes `basename`, so the active inode's key changes when
`events.jsonl` becomes its selected retired basename. No rule transfers the
active cursor entry to that retired identity. Treating it as a new segment
replays the whole old physical segment; retaining only the active-name entry
cannot checkpoint late retired writes under the new key.

Required change: define a crash-recoverable migration state machine. State
the exact per-segment offsets returned, whether backlog is durably accepted
before the V2 cursor reaches captured EOF or is represented as durable pending
work/ranges, and the reread action at every backlog/cursor/legacy-retirement
cut. Also define the active-to-retired `SegmentID` rekey/alias rule using the
rollover metadata so existing offsets transfer without losing late growth or
replaying prior physical records. Add nonmonotonic-ID and crash-between-
backlog-and-cursor fixtures.

#### R10-2 — the three-slice “every production caller” inventory is not closed

Artifacts: `04-design/queue-model-design.md` §12 `CQ-EFFECT-*` ownership;
`03-research/queue-model/findings.md` C7; current production symbols below.

The listed direct `ScanAfter` files are covered, but production replay and
projection surfaces are broader than those calls:

- `cmd/harmonik/subscribe.go` `runSubscribeFollowIO` persists only
  `lastSeen event_id` and sends `since_event_id` on every reconnect. It is not
  in `CQ-EFFECT-READERS-01`, so the promised additive V2 subscribe token has no
  client owner and a late lower-ID retired event remains invisible.
- `internal/daemon/commscursor.go` `CursorStore` persists EventID strings, and
  `internal/daemon/bootsocket.go` constructs/wires those stores into
  `commsrecvhandler_nnwaa.go` and `SubscribeHub`. Neither file is in
  `CQ-EFFECT-DAEMON-01`; editing only the named handlers cannot replace their
  durable cursor schema/wiring.
- `internal/sessiondata/sessiondata.go` `buildRunEventData` and
  `cmd/harmonik/eval_cmd.go` `evalReadEvents` open only the active
  `events.jsonl` path. After rollover they silently omit every retired
  segment, yet neither projection is assigned to a reader slice.
- `cmd/harmonik/goalkeeper_cmd.go` `readOperatorComms` and
  `internal/goalstate/types.go` retain `last_event_id` as durable incremental
  state over `comms log`; no slice owns its one-time V2 migration and new
  checkpoint representation.

The dependency prose is also internally stale: after the DAG correctly makes
both caller slices predecessors, `04-design/queue-model-design.md` §12 and
`03-research/queue-model/findings.md` C7 still describe `CQ-02I` as only
“after CQ-EFFECT-01 and CQ-MIG-01.” That wording permits dispatch before the
two required migrations.

Required change: perform a closed inventory of direct primary-log opens,
wire-level replay clients, durable EventID stores, and projection wrappers—not
only literal `ScanAfter` calls. Assign every affected file/symbol to
file-disjoint effect slices, add the exact V2 wire/store migration
dependencies and accepted intermediate states, and make every active CQ-02I
dependency statement name all three effect slices plus CQ-MIG-01. Re-run the
late-lower-ID proof through the generic subscribe reconnect, comms cursor, and
at least one direct full-history projection.

Because migration can lose its backlog and multiple production readers remain
outside the supposedly complete migration DAG, Round 10 is
`REQUEST_CHANGES`.
