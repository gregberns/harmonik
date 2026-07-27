# CQ-02 cross-spec composition and implementation-DAG review — Round 1

## Verdict

**REQUEST_CHANGES**

The receipt-backed completion spine is compositionally sound: queue state and
the final receipt remain authoritative, group observations are single
normal-path attempts, append failure is diagnostic, restart never synthesizes
or replays those observations, and the waiter plus startup-recovery gates
precede live receipt-producing terminal wiring. The four drafts and executable
task plan are nevertheless not ready to finalize. Normative wire records remain
incomplete, the failure-group transition contradicts the approved one-commit
design, and literal production status/startup callers are not assigned exact
implementation ownership.

## Scope and verification

I independently reviewed the four full-file drafts, `05-changelog.md`,
`07-tasks.md`, `CQ-02.yaml`, the approved CQ-02 card and designs, and the
literal production call sites for submit, append, status, cancel, startup
recovery, `viaWatchGroupCompletion`, queue-event payload registration, and the
workloop/inline/bootstrap/caller mutation slices.

The following checks pass:

- the execution-model semantic-scope proof permits only frontmatter
  version/date, EM-015f, and one qualifying revision row;
- the evidence file has the exact 23-key schema, 69 crash rows, and 19
  implementation slices; its declared dependency graph is acyclic;
- the declared spine
  `CQ-02I -> CQ-01 -> CQ-RECEIPT -> CQ-RUN-WAIT`, with
  `CQ-RECEIPT + JR-01 -> CQ-04` and both `CQ-RUN-WAIT` and `CQ-04` gating
  `JR-03`, is safe;
- same-file ordering is declared for `internal/queue/rpc.go`,
  `internal/daemon/workloop.go`, and `cmd/harmonik/run.go`;
- QM-053, PL startup, EV payload presence, and EM-015f agree that the
  intent-bound receipt is authoritative and
  `completion_receipt_id` is present exactly for final successful completion;
- optional `queue_group_completed`, `queue_group_started`, and `queue_paused`
  attempts cannot roll back committed queue state, block cleanup/admission,
  hang the daemon-backed waiter, or be replayed after restart;
- `queue_cancelled_operator` remains Class O, post-release, non-replayed, and
  excluded from shutdown;
- no implementation slice claims `ScanAfter`, `internal/replay`,
  ReplayCursor, segmented JSONL, or another stale global event migration;
- the changelog describes four modified drafts and does not claim a global
  event-log change; and
- `git diff --check` passes for the reviewed artifacts.

The card's structural evidence validator can pass while the ownership defects
below remain because it checks that each evidence slice has nonempty fields,
not that every literal production symbol and wire field is named.

## Required changes

### C1 — Normative queue wire records still omit approved production fields

Queue-model §2.10 defines `QueueSubmitRequest` with only `groups` and
`schema_version`, while production `internal/queue/types.go`
`QueueSubmitRequest` also carries the approved retained `name` and `workers`
fields. The same normative section omits production
`QueueAppendRequest.Name`. The approved card explicitly requires those fields
to be accurate in the drafts and changelog.

The drafts also expose `queue-cancel` without a normative request/response
record for the current name/queue-ID/force surface. PL-003a assigns queue
payload schemas to queue-model while PL-028c owns only cancellation transport
and daemon-only behavior, so neither draft supplies a complete field-level
cancel contract. PL-028's status command text also fails to inventory the
retained name selector consistently.

Required correction: make the owning spec define the exact retained submit,
append, status, and cancel records, including defaults, name-versus-ID
precedence, force behavior, and response identity. Preserve other shipped
fields rather than silently narrowing the existing wire. Make PL-003a/PL-028c
cite those records and make `05-changelog.md` enumerate the concrete retained
fields and method.

### C2 — The new receipt-backed QueueStatus surface has unowned client code

The queue draft adds `QueueStatusRequest.watched_group_index` and receipt-backed
response fields. Production currently has neither:

- `internal/queue/types.go` `QueueStatusRequest` contains only `Name` and
  `QueueID`;
- `internal/queue/cli/status.go` `RunQueueStatus` constructs only those two
  selectors; and
- `internal/queue/cli/client.go` `renderQueueStatusText` prints
  `(no queue active)` whenever `queue == nil`, which would misrender a valid
  receipt-backed completed response.

`CQ-RECEIPT` names response additions and server-side
`HandleQueueStatus`/`findQueueByID`, but does not own the request field or
either CLI symbol. `CQ-RUN-WAIT` owns only
`cmd/harmonik/run_via_daemon.go`. Therefore the literal status surface is not
end-to-end owned, and a conforming server can still have an incorrect shipped
client.

Required correction: assign the request and response type changes and the
operator CLI request/render/test changes to exact task leases. State whether
the operator CLI exposes `watched_group_index`; regardless, its renderer must
distinguish receipt-backed completion from no active queue. Serialize any
shared files and add failing-first JSON/text rendering plus request-wire
proofs, accepted intermediate state, rollback boundary, and conflict entry.

### C3 — QM-052 creates a forbidden two-commit failure state

The approved queue design says advance commits the current terminal group
together with either successor activation or the paused failure state.
EM-015f likewise refers to one authoritative group/queue commit before the
failure observations.

QM-052 instead requires:

1. persisting `complete-with-failures`; then
2. separately changing and persisting `Queue.status = paused-by-failure`.

A crash between those replacements exposes an active queue whose active group
is already terminal. No clause, crash-matrix row, startup classifier, or task
proof owns that state. This is not merely an event-order issue: it contradicts
the approved immutable-candidate boundary and can leave dispatch/recovery with
an undefined state.

Required correction: make terminal group failure plus queue pause one
QueueStore candidate and one classified replacement. Only after that combined
state is `committed_durable` may the normal path attempt
`queue_group_completed` and then `queue_paused`. Align QM-052, EM-015f
wording, evidence, and JR-03 proofs literally.

### C4 — The implementation plan is not exact enough to prove complete caller ownership

Two concrete ownership gaps remain:

- `internal/lifecycle/startup_pl005_qm002.go`
  `loadOneQueueAtStartup` invokes `reconcileDispatchedItems`,
  `reconcileThreeWay`, and `reconcileQueueTerminalState`. `CQ-04` names only
  the last of those helpers. The first two are current direct `queue.Persist`
  callers implementing QM-002a/QM-002b and must also migrate to the sole
  transaction owner.
- The QueueStatus request and CLI symbols in C2 have no task owner.

The plan also fails the card's “each slice names exact symbols, proof,
intermediate state, rollback, and conflicts” requirement for several declared
slices. `CQ-03` and `JR-04` are summaries rather than complete task records.
The six “existing file-disjoint caller amendments” are grouped prose: several
omit an exact lease, symbol list, individual failing-first proof, accepted
intermediate state, rollback boundary, and conflict declaration. The evidence
entries contain the required keys but use broad descriptions such as
“supported ... recovery,” “operator pause/resume queue mutations,” and
“reservation/claim ownership composition”; those are not exact symbol leases.

Required correction: expand every implementation slice into one
self-contained record in both `07-tasks.md` and `CQ-02.yaml`. At minimum name:

- all three startup reconciliation helpers and their direct persistence/event
  regions under `CQ-04`;
- the status request, server, operator CLI, and daemon-waiter symbols under
  explicitly serialized leases;
- the exact CQ-03 and JR-04 production symbols; and
- each eager, operator, budget, crew, review, bootstrap, workloop-maintenance,
  group-activation, terminal, and inline-exit mutation region separately.

Every record must include prerequisites, lease family, exact files/symbols,
failing-first proof, accepted intermediate state, rollback boundary, and
same-file/semantic conflict declaration. Regenerate the caller-to-task proof
from the literal production call sites; a nonempty `writes` string is not
sufficient.

## Composition conclusion

No new outbox, effect key, persistent delivery state, replay path, or global
event-reader migration is needed. The receipt/waiter/startup ordering should be
retained. Re-review after C1–C4 are corrected in the four drafts, changelog,
task plan, and evidence, with the wire and caller-to-task validators strengthened
to detect these omissions.
