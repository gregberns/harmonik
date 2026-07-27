# Spec-draft review — `queue-transaction-contract`

## Verdict

**REQUEST_CHANGES**

The draft is structurally complete and carries most of the change design into a
cohesive full-file replacement, but several normative contradictions and
coverage gaps remain at the transaction boundary. They affect exactly-once
completion observability, restart classification, shutdown cleanup, and the
operator-cancel surface, so the draft is not ready to become the system spec.

## Review method

Fresh-context review of the exact substantive inputs authorized for this pass:

- `04-design/queue-model-design.md`
- `05-spec-drafts/queue-model.md`
- the current `specs/queue-model.md`
- `05-changelog.md`
- `07-tasks.md`
- `kerf show queue-transaction-contract` Pass 5 instructions and acceptance
  criteria

I mechanically compared the complete draft with the current spec, traced each
of the design's twelve target-state sections into the draft, and reviewed the
replacement/unlink/archive cuts, legacy-plus-intent order, lifecycle effect
batches, and downstream dependency graph as state machines rather than as
prose summaries. I also confirmed that every absolute spec-file target cited by
the draft exists. I did not read additional substantive project inputs, run
`kerf square`, or run build, test, lint, or UBS commands.

The coordinator-approved Kerf beta exception is accepted: the two validation
task records may retain `bead_id: null` with
`materialization: deferred_to_implementation_dispatch`. Their title, label,
invocation, behavior, and observable-terminal-condition fields otherwise
satisfy the Pass 5 validation-task shape.

## Findings requiring changes

### 1. The design's shutdown transaction is not normatively specified or assigned downstream

The change design's “Make shutdown cancellation and archive separate durable
steps” requires shutdown to process every sorted queue name through durable
`cancelled` state, durable archive intent, exact archive rename, `main` legacy
resolution, durable intent deletion, and only then owner clearing. It also
requires per-name reporting and safe continuation after a sibling failure.

The draft mentions shutdown in the `QueueStatus.cancelled` comment,
QM-054's drain entry, and QM-CONF-006, but QM-058 specifies an operator
cancellation command and then a generic cancellation sequence. It never says
that shutdown MUST invoke that sequence for each sorted name, when the
`paused-by-drain` queue becomes `cancelled`, or that shutdown reports every
per-name result and continues safe siblings. QM-068 alone does not bind
shutdown to the cancellation/archive protocol.

`07-tasks.md` likewise has a live/CLI cancellation card and tests operator
cancel, but no card or acceptance item owns the shutdown caller migration or
the shutdown outcome matrix.

Required correction:

1. Add an explicit shutdown clause to QM-058 that states the design's sorted
   per-name sequence, its relationship to QM-054 drain completion, the exact
   result matrix, and safe sibling continuation.
2. Assign that production caller to a named downstream card in `07-tasks.md`
   and add an observable shutdown-matrix acceptance case to a validation task
   (or add a separately tracked test task).

### 2. QM-051 and QM-053 both require the final `queue_group_completed`

QM-051 says that after the all-terminal candidate is durably installed, its
effect batch emits `queue_group_completed` first. That wording includes the
no-successor/final-group case and then sends that case to QM-053. QM-053 again
requires emitting the final `queue_group_completed` in step 2.

This contradicts QM-033's single logical completion landmark and can be
implemented as two identical final events on the normal path.

Required correction: make one requirement the sole emission owner. For
example, have QM-051 delegate the final/no-successor effect batch to QM-053,
while retaining QM-051's event order only for successor and failure cases; or
have QM-053 explicitly consume the already-emitted QM-051 event rather than
append another. QM-CONF-005 and the scenario task should say that normal
completion and restart recovery each leave exactly one matching final
landmark.

### 3. Generic post-commit event recovery was narrowed to the completion landmark

The change design requires any required event failure after durable install to
retain the candidate, refuse mutation, and on startup emit only the missing
logical event before releasing refusal. That applies to submit, append,
activation, deferral, pause, and advance batches, not only final completion.

QM-063 instead says startup emits “only the missing logical landmark.”
“Landmark” is defined by QM-033 and the glossary as the final
`queue_group_completed`. QM-CONF-005 repeats the same narrowed wording.
QM-002's phrase “missing logical event emission” does not define which
non-final events are recoverable, how a restart identifies the exact failed
batch, their order, or when refusal is released.

Required correction: restore the design's generic “missing logical event”
contract and provide deterministic recovery rules for every affected effect
batch. The spec must identify the durable evidence that distinguishes already
emitted from missing submit/append/group-start/deferred/pause events, preserve
their operation order, state that RPC responses are never replayed, allow Wake
to be repeated only after install, and state the refusal-release condition.
Expand QM-CONF-005 and downstream acceptance to exercise at least one
non-final event failure in addition to the final landmark.

### 4. `rejected` and `not_committed` overlap on validation

QM-001a defines `rejected` to include validation rejection before namespace
I/O. QM-001b's replacement cut table classifies “validation/marshal” as
`not_committed`. QM-CONF-001 then asks a fault-injected “validation” case to
assert an exact disposition, but the normative clauses give two answers.

Required correction: distinguish request/candidate validation from any
internal persistence precondition, or remove validation from the replacement
cut row. State one unambiguous disposition for each cut and use the same
terminology in QM-CONF-001. Ordinary validation and stale-generation rejection
should remain `rejected` with no namespace I/O.

### 5. Restart cannot execute the specified replacement classifier from the persisted evidence

QM-001b says replacement recovery distinguishes “intended candidate
canonical” from “exact prior canonical” and that process death runs the same
classifier on restart. But only archive operations have a durable intent
record. Queue generations are explicitly process-local and unpersisted, and
canonical replacement leaves no durable candidate digest, prior digest, or
operation identity.

After death between canonical rename and parent sync, restart can observe one
valid canonical file, but for an update its embedded `queue_id` is normally
the same in both the prior and intended snapshots. The restart therefore
cannot know whether the visible bytes are the candidate to promote or the
prior state to classify `not_committed`, nor which post-commit effect batch is
owed. The scenario task's “exact-byte readback” assertion assumes comparison
material that the normative data model does not retain.

Required correction: define restart-visible comparison evidence for ordinary
replacement (for example, a bounded transaction record), or replace the
candidate-versus-prior classifier with a safe classifier based solely on
specified observable state and define the consequent effect recovery. If that
requires a new sidecar or target-state decision, revise the change design
before redrafting rather than silently adding an unbacked format.

### 6. The operator-cancel boundary is not tied to a valid, exact cross-spec surface

Section 2.10 calls `queue-submit`, `queue-append`, `queue-status`, and
`queue-dry-run` the four v0.1 queue methods. QM-058 nevertheless requires a
live operator cancellation over “the daemon control transport,” but gives no
exact process-lifecycle requirement or existing method/payload reference.
`07-tasks.md` assumes `harmonik queue cancel --queue ...`. A normative
cross-subsystem command cannot depend on an unnamed transport contract.

The draft also weakens two design details: it changes the specifically named
best-effort `queue_cancelled_operator` audit emission into an unnamed “audit
diagnostic,” and it never maps operator cancellation to one of
`QueueArchiveIntent.archive_kind ∈ {cancelled, failed, corrupt}` or to a
normative destination policy.

Required correction:

1. Cite the exact existing process-lifecycle cancellation method and payload.
   If none exists, add the necessary target spec draft rather than claiming no
   cross-spec normative change.
2. Preserve the design's named audit emission and its after-release,
   before-response order, or explicitly revise the design.
3. Define the operator-cancel archive kind and basename rule.

### 7. New wire text and changelog metadata need precise traceability

`QueueStatusRequest` is new relative to the current spec, but its selector is
ambiguous: `name` both defaults to `main` when omitted and must be absent for
`queue_id` to act as the selector. Unlike `QueueAppendRequest`, it gives no
precedence rule when both fields are populated. The new request and the added
name/worker selectors are not called out in `05-changelog.md`, even though
they are normative wire changes.

There are also two changelog accuracy errors:

- the current spec frontmatter says `version: 0.1.4`, while
  `05-changelog.md` reports a literal `0.1.5 → 0.1.6` change; the existing
  embedded changelog does contain v0.1.5, so this pre-existing metadata drift
  should be documented rather than silently hidden;
- the new embedded v0.1.6 entry cites “§3.2c,” but the requirement is headed
  QM-002c under §3.2.

Required correction: define status-selector precedence (and mismatch behavior),
trace all retained wire additions to the change design and changelog, and make
the version/section references accurately describe the source and target
files.

## Criteria already satisfied

- There is exactly one draft for the one target spec, and its filename matches
  `specs/queue-model.md`.
- The draft is a complete replacement file, not a patch.
- Existing data records, validation rules, group/item state machines,
  named-queue scheduling, handler-pause text, and historical changelog content
  are substantially preserved. I found no accidental wholesale section
  removal.
- The canonical/legacy path split, immutable snapshots, per-name generation
  guard, sole mutation owner, four-way filesystem disposition vocabulary,
  archive intent, legacy validity matrix, owner-retention matrix,
  persist/install-before-effects rule, per-name isolation, and conformance
  sections are all present and mostly follow the change design.
- Archive-intent-before-legacy ordering for `main`, including
  `fsync(.harmonik)` when legacy is already absent, is represented.
- Submit constructs group zero active in the single installed candidate, and
  the success response follows install and ordered effects.
- Advance groups the terminal item, group terminal, successor/pause/completion
  state into one candidate.
- The external changelog accounts for the sole target draft and names the
  motivating change design.
- The implementation dependency graph in `07-tasks.md` matches the design's
  requested graph, and the four proposed caller cards plus three existing-card
  amendments are present.
- Both required validation-task records are present with the approved deferred
  materialization exception.

## Final decision

**REQUEST_CHANGES**

Resolve findings 1–6 before another review round. Finding 7 should be corrected
in the same round because Pass 5 finalizes the full normative file and its
changelog, not only the new transaction prose.
