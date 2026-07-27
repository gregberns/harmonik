# CQ-02I implementation review

## Verdict

**REQUEST_CHANGES**

Reviewed strictly as the claimed implementation range:

```text
83c2a689d..609308a53bf04f07408b4ee0d7c540abf76b3100
```

The range changes exactly the four leased implementation/test files. No
prohibited caller, receipt, status, event, or terminal-path file is present in
the range.

## Blocking findings

### F1 — the replace-intent bytes do not implement the exact QM-001 v1 schema

`internal/queue/transaction.go` `ReplaceIntentV1` serializes nested `prior` and
`candidate` objects and fields named `completion_receipt` and
`archive_handoff`. It also serializes both optional fields as `null` on an
ordinary replacement.

QM-001 and the approved CQ-02 evidence require the exact flat field set:

```text
schema_version
transaction_id
operation_kind
normalized_name
queue_id
canonical_basename
prior_state
candidate_sha256
candidate_temp_basename
wake_required
[completion_receipt_binding | archive_handoff_binding]
```

Thus generated records use the wrong field names, nesting, and ordinary field
presence. In addition, `ClassifyReplaceIntent` accepts an already-decoded
`ReplaceIntentV1`, so it has no raw-byte boundary at which it can reject
unknown fields, missing fields that collapse to Go zero values, or
non-canonical predecessor bytes. This cannot provide the required
corrupt/unsupported exact-schema refusal.

Required correction: implement the exact canonical flat v1 schema and make
restart classification consume and strictly decode the persisted predecessor
bytes before consulting bound filesystem facts. Add exact field-set,
unknown-field, missing-field, and non-canonical-byte tests for replace
intents, equivalent to the successor coverage.

### F2 — successful no-replace installation can be returned as `not_committed`

`durableNoReplace` installs the authoritative target with `link`, then returns
the error from removing its unique temp. If that removal fails, the target
entry already exists but has not yet been parent-synced. `writeReplacement`
maps every such error to `not_committed`, attempts to remove the selected
candidate, and returns. `installBoundArchiveIntent` likewise maps the state to
`not_committed`.

This violates the typed result and crash matrix. The target namespace mutation
occurred and parent durability is unresolved, so the result must be
`commit_indeterminate`, the exact evidence must be retained/reloaded, and the
name must remain refused. In the QueueStore path only
`commit_indeterminate` installs quarantine, so the current result permits a
same-generation retry to perform further namespace I/O against the surviving
intent.

The fault suite cuts link-before-install and link-error-after-install, but
never cuts temp unlink after a successful link. The archive suite has the same
gap.

Required correction: distinguish pre-install failure from post-install temp
cleanup failure, classify/reload the exact target and temp state, preserve the
selected candidate/predecessor evidence, and return/quarantine the
indeterminate state. Add both replace- and archive-intent fault cuts for
link-success plus temp-unlink failure and for its retry/restart state.

### F3 — conflicting existing intent bytes do not refuse the name

When `durableNoReplace` finds different existing target bytes it returns one
undifferentiated error. Both callers classify that as `not_committed`.
`QueueStore.Transact` quarantines only `commit_indeterminate`, so a different,
corrupt, or unsupported existing replace intent is preserved but does not
refuse the name before subsequent I/O. The archive test
`TestInstallBoundArchiveIntentConsumesExactBytes` explicitly expects
`not_committed` for a changed existing successor.

QM-001 and QM-007 require different/corrupt/unsupported existing records to be
preserved and to refuse the name. A collision is an identity-integrity state,
not proof that the requested namespace is safely retryable.

Required correction: expose a distinct exact-existing/conflicting-existing
classification from the no-replace primitive, map conflict to preserve and
refuse/quarantine, and test that a second transaction performs zero I/O while
the conflict remains.

### F4 — replace recovery promotes through a wrong selected temp

`classifyReplaceIntent` returns `ReplacePromoteCanonical` as soon as canonical
bytes match `candidate_sha256`. Although it has already read the selected
candidate-temp path, that first branch ignores whether the temp is present and
whether its bytes are wrong.

QM-001 explicitly makes a wrong selected temp a preserve-and-refuse state. An
exact candidate canonical plus a present wrong selected temp is therefore
accepted when it must be quarantined. The current table tests wrong-temp only
with the prior canonical, so they do not exercise this ordering error.

Required correction: classify all canonical/temp combinations, refuse any
present wrong selected temp before promotion, and add exact-canonical plus
wrong-temp and exact-canonical plus unexpected-temp cases.

### F5 — the public transaction API does not enforce cancellation-handoff
invariants

`prepareReplacementWithIDs` independently validates the operation enum and
the optional archive plan. It permits `OperationCancellation` without an
archive handoff and permits an archive handoff on any other operation.
`QueueStore.Transact` also returns its no-op fast path before preparing the
handoff, so a request carrying cancellation/archive authority can report
`committed_durable` without creating the required predecessor at all.

QM-001/QM-007 require the cancelled-state replacement to durably bind the
preallocated successor facts before it becomes durable. The API shape must
make this misuse impossible, not rely on future callers to preserve the
coupling.

Required correction: enforce the cancellation-operation/archive-binding
relationship at preparation and reject archive-bearing no-ops (or route them
through the handoff preparation/install protocol). Add negative tests for
both mismatched directions and the no-op handoff case, all proving zero
namespace I/O.

## Verification

The following checks passed:

```text
go test ./internal/queue ./internal/queuewiring
go test -count=1 -race ./internal/queue ./internal/queuewiring
go vet ./internal/queue ./internal/queuewiring
gofmt -d <four changed files>
git diff --check 83c2a689d..609308a53
```

Scoped UBS completed under `/opt/homebrew/bin/bash` and exited nonzero. Its
reported criticals are ordinary enum/status/test comparisons misidentified as
secret comparisons. Its loop-capture warning is not a defect under this
module's Go 1.22+ loop semantics, and its lock warning points to the
intentional `LockForMutation` handle whose `Done` method releases the lock.
The shadow-workspace module warnings are also synthetic. None disposes or
supersedes the blocking durability findings above.

`make check-fast` could not start because this worktree lacks the expected
`.tools/gofumpt` binary:

```text
scripts/go-format.sh: line 41:
/Users/gb/github/harmonik-wt/cq-02i/.tools/gofumpt: No such file or directory
```

That environment/tooling failure is not the reason for this verdict.

## Round 2

- Fix commit: `25c5a11127a92bac8258132fd0a28a8ccbcaabcc`
- Reviewed fix range: `609308a53..25c5a1112`
- Verdict: **REQUEST_CHANGES**

The fix range remains within the same four implementation/test files. It
corrects the core durability failures from Round 1:

- ordinary predecessor bytes now use the flat ten-field QM-001 schema;
  `prior_state` is the scalar `"absent"` or an exact digest; optional bindings
  are omitted unless present;
- the ordinary public classifier strictly decodes raw JSON, rejects unknown,
  missing, trailing, and non-canonical bytes before filesystem access;
- no-replace installation has distinct installed, not-installed, refused, and
  indeterminate states;
- link success followed by temp-cleanup failure retains the intent and selected
  candidate, returns `commit_indeterminate`, and gives restart the exact
  retry-rename state;
- conflicting predecessor bytes are preserved, return indeterminate, quarantine
  the QueueStore name, and make the next transaction reject before namespace
  I/O;
- exact canonical state is promoted only when the selected temp is absent;
  exact or wrong unexpected temps refuse;
- operation/handoff mismatch and archive-bearing no-op requests reject before
  namespace I/O.

F2 and F3 are resolved. F1, F4 test completeness, and F5 remain incomplete at
the public misuse-resistant boundary.

### Round-2 F1 — linked predecessor recovery still bypasses raw exact decoding

`ClassifyReplaceIntent` now correctly accepts raw bytes and calls the private
strict decoder. The linked recovery surfaces do not:

- `ClassifyLinkedHandoff` accepts `*ReplaceIntentV1`;
- `InstallBoundArchiveIntent` accepts `ReplaceIntentV1`; and
- the strict `decodeReplaceIntent` helper is unexported and no public raw-byte
  linked operation returns the validated predecessor.

Consequently a recovery caller outside package `queue` can use ordinary
`json.Unmarshal`, silently discard unknown fields or normalize non-canonical
predecessor bytes, and pass the resulting struct to linked classification or
successor installation. The exact raw predecessor is the authority for the
bound successor. Validating it only in the separate ordinary classifier does
not make the linked API safe by construction, and that classifier does not
return the validated binding for the next linked step.

Required correction: make linked classification and bound-successor
installation consume raw predecessor bytes and call the same strict canonical
decoder, or expose an opaque validated predecessor value that can only be
constructed by that decoder. Add unknown-field, missing-field, explicit-null,
and non-canonical predecessor tests at both linked public entry points, proving
rejection before namespace I/O.

### Round-2 F4 — the recovery test table is not the complete canonical/temp
matrix

The branch ordering now correctly handles the Round-1 bug, including exact
canonical plus exact/wrong unexpected temp. The promised complete matrix is
still not executable:

- all current rows use a present prior canonical;
- no row covers `prior_state = "absent"` with canonical absent and selected
  temp absent/exact/wrong; and
- third canonical plus exact/wrong selected temp is not covered.

These are distinct restart states for create and third-state recovery. The
switch currently appears to produce the intended actions, but CQ-02I requires
a deterministic fault/restart oracle rather than leaving those combinations
to inspection.

Required correction: table-drive the Cartesian canonical states
`absent/prior/candidate/third` against temp states `absent/exact/wrong`, marking
inapplicable prior combinations explicitly, and assert the action plus
evidence preservation for every supported state.

### Round-2 F5 — cancellation coupling does not validate cancelled candidate
state

`validateArchiveOperationCoupling` enforces only:

```text
OperationCancellation iff ArchiveHandoff != nil
```

It never examines the decoded candidate status. The test helper
`transactionLinkedFixture` demonstrates the hole: it changes the fixture
operation to cancellation and supplies a handoff, but leaves the candidate
status as `paused-by-drain`; preparation accepts it and builds a cancellation
predecessor/successor pair. Conversely, a non-cancellation operation may write
a `StatusCancelled` candidate without the mandatory handoff.

QM-001/QM-007 require the cancelled-state canonical replacement itself to bind
the archive successor before durability. Matching an operation label to a
pointer is insufficient if candidate bytes can contradict both.

Required correction: after decoding candidate bytes, require cancellation
operation, cancelled candidate status, and archive binding as one invariant;
reject either mismatch before namespace I/O. Add negative direct and
QueueStore tests for cancellation with a non-cancelled candidate and a
cancelled candidate under a non-cancellation operation.

## Round-2 verification

The following checks passed at the exact fix commit:

```text
go test -count=1 ./internal/queue ./internal/queuewiring
go test -count=1 -race ./internal/queue ./internal/queuewiring
go vet ./internal/queue ./internal/queuewiring
gofmt -d <four changed files>
git diff --check 609308a53..25c5a1112
```

No implementation file was edited during this review. Kerf/integration state
was not advanced.

## Round 3

- Fix commit: `71af3bcbcbd96582f648b9998bc8c851c3337c37`
- Reviewed fix range: `25c5a1112..71af3bcbc`
- Verdict: **APPROVE**

The range changes only the three CQ-02I leased implementation/test files:

```text
internal/queue/transaction.go
internal/queue/transaction_fault_test.go
internal/queuewiring/store_transaction_test.go
```

All remaining Round-2 findings are resolved.

### Round-2 F1 — resolved

Every public entrypoint that consumes a linked predecessor now accepts its raw
bytes:

- `ClassifyReplaceIntent`;
- `ClassifyLinkedHandoff`; and
- `InstallBoundArchiveIntent`.

All three route through the same private `decodeReplaceIntent`, which applies
strict JSON decoding, semantic `ReplaceIntentV1` validation, canonical
re-marshalling, and byte equality before any bound filesystem operation.
`ClassifyLinkedHandoff` performs no filesystem I/O itself, and
`InstallBoundArchiveIntent` completes decoding before `mkdirAll` or successor
installation.

`TestLinkedPublicEntrypointsStrictlyRejectRawPredecessor` exercises unknown,
missing, explicit-null, and non-canonical predecessor bytes at both linked
public entrypoints. Classification refuses each input; installation returns
`rejected` and proves the project path remains absent.

### Round-2 F4 — resolved

`TestClassifyReplaceIntentExactFacts` is now the complete applicable
`prior_state × canonical × selected-temp` table:

- prior states: present and absent;
- canonical states: absent, prior, candidate, and third; and
- selected-temp states: absent, exact, and wrong.

The three impossible `prior_state=absent` plus prior-canonical rows are
explicitly marked inapplicable. Every other cell asserts the exact recovery
action and error/refusal relationship. Each row rereads both canonical and
selected-temp paths and proves classification preserved their presence and
bytes.

### Round-2 F5 — resolved

`prepareReplacementWithIDs` decodes the candidate and then applies
`validateCancellationCoupling`. It requires the following three facts to occur
together:

```text
OperationCancellation
candidate.Status == QueueStatusCancelled
ArchiveHandoff != nil
```

Any differing truth value returns rejection before namespace I/O.
`TestArchiveOperationCouplingRejectsBeforeIO` covers the direct
`WriteReplacement` boundary, including cancellation with a non-cancelled
candidate and a cancelled candidate under a non-cancellation operation.
`TestQueueStoreRejectsCancellationStatusMismatchBeforeNamespaceIO` covers the
same two status mismatches through `QueueStore.Transact`, proving no generation
advance and an absent project path. The previously reviewed mismatch and
archive-bearing no-op cases remain covered.

## Round-3 mutation-evidence inspection

The added negative oracles are sensitive to the relevant semantic mutations:

- bypassing `decodeReplaceIntent`, weakening strict decoding, or removing the
  canonical byte comparison admits at least one of the four malformed raw
  predecessor cases at both linked entrypoints;
- changing the ordering or predicate of any
  `classifyReplaceIntent` canonical/temp branch breaks a concrete applicable
  matrix cell, while classifier-side mutation is caught by the post-call byte
  and presence checks; and
- dropping or weakening any side of the cancellation-operation,
  candidate-status, and archive-handoff equivalence breaks the direct and/or
  QueueStore negative cases.

The commit trailer records implementation verification including mutation
checks. The executable negative oracles above substantiate that claim for the
three Round-2 findings under review.

## Round-3 verification

The following checks passed at `71af3bcbc`:

```text
go test -count=1 ./internal/queue ./internal/queuewiring
go test -count=1 -race ./internal/queue ./internal/queuewiring
go vet ./internal/queue ./internal/queuewiring
go test -count=1 ./internal/queue -run \
  '^(TestLinkedPublicEntrypointsStrictlyRejectRawPredecessor|TestClassifyReplaceIntentExactFacts|TestArchiveOperationCouplingRejectsBeforeIO)$'
go test -count=1 ./internal/queuewiring -run \
  '^TestQueueStoreRejectsCancellationStatusMismatchBeforeNamespaceIO$'
gofmt -d internal/queue/transaction.go \
  internal/queue/transaction_fault_test.go \
  internal/queuewiring/store_transaction_test.go
git diff --check 25c5a1112..71af3bcbc
```

The reviewed diff remains within the CQ-02I lease. No code or integration
state was changed during Round 3; only this existing review artifact was
updated.
