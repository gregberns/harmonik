# CQ-02 full-draft durability review — Round 1

## Verdict

**REQUEST_CHANGES**

The completion receipt and release-marker design is now conservative and
crash-safe, and the execution-model draft is mechanically limited to the
authorized EM-015f clarification. The four full-file drafts are nevertheless
not ready to become normative: the queue/process drafts omit executable parts
of the ordinary replace/archive protocol and retain several active singleton
and wire contracts that contradict the approved design.

## Review scope and checks

I reviewed all four full-file drafts against the current four specs, the
approved research and change design (including both focused receipt-review
rounds), `05-changelog.md`, `07-tasks.md`, `CQ-02.yaml`, the approved CQ-02
card, and the current queue persistence/RPC types and callers.

The following checks pass:

- all four source-to-draft full-file diffs are readable and bounded to the
  planned surfaces;
- the execution-model literal preamble, frontmatter normalization, EM-015f
  replacement, and single qualifying revision row pass the card's semantic
  scope algorithm: no execution-model text outside EM-015f changed;
- `CQ-02.yaml` parses, has the exact 23-key top-level schema, 69 crash rows,
  19 implementation slices, the exact replace-intent and receipt paths, and
  the required release-marker, trusted-UTC, and migration cuts;
- the execution baseline/card diff and current worker lease classification
  pass;
- `git diff --check` passes.

`make specaudit-lint` remains red on pre-existing canonical-corpus findings
(including existing process-lifecycle tag reach and unrelated event-consumer
citations); it does not inspect these unfinalized draft paths and supplied no
CQ-02-specific approval signal.

## Required changes

### D1 — The normative ordinary replace/archive protocol is incomplete

The approved card makes
`.harmonik/queues/<normalized-name>.replace-intent` mandatory restart-visible
evidence for every ordinary replacement. The queue draft's QM-001 lists the
intent fields, but never specifies that path. More importantly, it stops the
ordinary success path after canonical parent sync and memory installation. It
does not require removal and queue-parent sync of the resolved ordinary
replace intent, nor classify failure/ambiguity of that unlink. Under QM-027
and QM-053's own rule that an unresolved intent owns/refuses the name, a
conforming implementation could leave every successful ordinary mutation
permanently refused.

The restart text is also too compressed to close the cuts required by the
approved design. It does not normatively state the exact decisions for:

- prior canonical plus exact selected candidate temp;
- exact candidate canonical;
- exact prior/absence with an abandoned selected temp;
- conflicting/corrupt identity or digest;
- replace-intent removal before and after queue-parent fsync; or
- definite rename failure versus rename/root-sync ambiguity.

QM-007 has the same problem for the successor archive intent. It names an ID,
bytes, and digest, but defines neither its deterministic path/canonical schema
nor the full predecessor-unlink and successor-unlink parent-sync classifiers.
The evidence matrix contains these decisions, but the normative draft does
not. The corrupt-source variant implied by `archive_kind: corrupt` and the
typed corrupt cancellation audit is also not connected to an exact
source-preserving archive path; QM-007 is phrased only as a handoff from a
successfully installed cancelled canonical.

Required correction: carry the approved universal protocol into QM-001 and
QM-007 as an executable state machine. Name the exact intent paths and
no-replace identity rules; specify selected-temp retry/cleanup, predecessor
handoff, ordinary/final/archive intent cleanup, every parent-sync ambiguity,
and the corrupt-source archive classifier. A resolved ordinary intent must be
durably absent before its refusal is released, while an unresolved or
mismatched intent must continue to refuse the name.

### D2 — Active singleton and overwrite contracts contradict the new topology

The process-lifecycle draft's PL-004 still normatively declares
`.harmonik/queue.json` to be the live queue-manager WM-026 write target, read
at startup and unlinked on completion. PL-013 still says the in-memory queue is
loaded from that singleton. The PL-028 queue-submit command still says a
successful submit persists that singleton, and the dependency text still
describes WM-026 as the singleton queue-write discipline. These are active
requirements and interfaces, not historical revision rows.

The queue draft retains the same contradiction in `QueueStatus.cancelled` and
the glossary: cancellation allegedly leaves `queue.json` on disk so a later
run can overwrite it. That directly conflicts with QM-007's linked
cancel/archive transaction, durable intent cleanup, ownership release, and
same-name admission rule. It also permits the overwrite behavior this work is
intended to eliminate.

Required correction: reconcile every active occurrence. PL-004 must inventory
canonical named queues, replace/archive intents, receipt root/files, and
release markers, with `.harmonik/queue.json` identified only as migration
input. PL-013, PL-028, and the dependency text must point to named
QueueStore-owned recovery/persistence. The cancelled status and glossary must
describe durable archive/intent cleanup and release, never overwrite-on-next-
run. Historical revision rows may remain historical.

### D3 — The normative queue wire schemas omit approved, already-shipped fields

The queue draft's `QueueSubmitRequest` contains only `groups` and
`schema_version`; it omits the approved retained `name` and `workers` fields
present in `internal/queue/types.go` `QueueSubmitRequest`. Its
`QueueAppendRequest` omits the retained `name` selector present in
`internal/queue/types.go` `QueueAppendRequest`. This contradicts the approved
process-lifecycle research/design and the card's wire-accuracy acceptance
condition.

The drafts also register `queue-cancel` while defining no normative
request/response record for the current name/ID/force selection surface.
PL-003a points queue payload ownership at queue-model §6, but queue-model
defines no cancel payload there; PL-028c gives only high-level routing. The
result is insufficient to decide name-versus-ID precedence, force behavior,
or the response identity at the durability boundary.

Required correction: define the exact retained submit and append request
fields and their precedence/defaults. Define the queue-cancel
request/response and name/ID/force selector semantics in its declared owning
spec, then make PL-003a/PL-028c cite that record. Reconcile the changelog's
generic “inventory” claim with the actual normative records.

### D4 — Failure-group transition still creates an unclassified two-commit cut

The approved queue design commits a group's terminal failure and
`Queue.status = paused-by-failure` together in one replacement candidate.
QM-052 instead requires one QM-001 commit for the group terminal status and a
second QM-001 commit for the queue pause.

A crash between those commits leaves an active queue whose active group is
already `complete-with-failures`. Neither QM-052 nor startup recovery assigns
that state a deterministic continuation, refusal, or event rule. It is also
absent from the evidence crash matrix because the approved design deliberately
eliminated the intermediate state.

Required correction: make the terminal group status and paused queue status
one immutable candidate/replace transaction, as the approved design states.
Only after that combined state is `committed_durable` may the uninterrupted
normal path attempt `queue_group_completed` and `queue_paused`.

## Confirmed clean areas

The receipt/release-marker portion itself needs no further architecture
change. The drafts bind the only receipt identity and exact bytes before
completed-canonical installation; make receipt/root durability precede CAS
unlink; use queue ID plus exact completed-byte digest; release the name only
after canonical and final-intent absence are durable; create the immutable
release marker only after actual ownership release; and use
`released_at + 720h` with trusted synchronized UTC and conservative refusal.
Missing, corrupt, mismatched, or unsupported markers cannot shorten
retention. Receipt-first, root-synced GC leaves a marker-only witness across
every deletion cut and never touches a newer same-name queue.

The drafts also preserve JSONL as observation only, persist no event-delivery
state in intents/receipts/markers, never mint a recovery receipt, keep the
final event diagnostic and non-replayed, capability-gate
upgrade/downgrade/rollback, fail daemon-down cancellation with exit 17 and
zero local writes, and introduce no unauthorized event-log or execution-model
architecture.

Re-review after D1–D4 are corrected in all affected full-file drafts and the
evidence/path proof is regenerated.

## Round 2 focused re-review

Verdict: **REQUEST_CHANGES**

The Round-1 response and corrected artifacts resolve D1, D2, and D4. The
ordinary replace and archive protocols now define exact intent paths and
schemas, no-replace creation, root synchronization, cleanup, and exhaustive
restart classification including corrupt-source handling. Active singleton
language has been removed in favor of named QueueStore ownership, with the
legacy singleton retained only as migration input. A failed group's terminal
state and `paused-by-failure` are now installed in one immutable replacement
transaction.

### R2-1 — Queue-cancel still breaks the shipped request wire

The corrected queue draft defines `QueueCancelRequest` with `name`,
`queue_id`, and `force`. The shipped request in `internal/queue/types.go`
`QueueCancelRequest`, and the daemon message constructed by
`internal/queue/cli/cancel.go` `tryDaemonQueueCancel`, use the JSON field
`queue`, not `name`. `CQ-CALLER-CANCEL` describes the change as additive, but
replacing `queue` with `name` is a breaking rename. An N-1 client sending
`queue` would not select the requested named queue under the normative record
and could fall through to the default queue.

Required correction: retain `queue` as the shipped selector and add
`queue_id`, or normatively accept both `queue` and `name` with deterministic
conflict and precedence rules. Reconcile the queue draft,
`CQ-CALLER-CANCEL`, the evidence wire-field inventory, and the changelog's
claim that shipped cancel fields are preserved.

All other Round-2 durability checks remain clean: the complete submit and
append records, receipt binding and release ordering, trusted-clock 720-hour
retention, conservative marker handling, receipt-first root-synced GC,
same-name protection, cancellation failure behavior, migration and
capability/downgrade boundaries, JSONL non-authority, and the
execution-model-only delta. The evidence YAML parses, its dependency graph is
acyclic, the execution-model authorized-section scan passes, and
`git diff --check` is clean.

## Final focused durability re-review

Verdict: **REQUEST_CHANGES**

Round-2 R2-1 is resolved. The literal shipped cancel JSON fields `queue` and
`force` are preserved in the queue and lifecycle drafts, research, designs,
implementation task, CQ-02 evidence, and changelog. Optional `queue_id` is
additive; either selector may stand alone, both must resolve to the same
identity, and neither-present is invalid rather than a silent `main` default.
The CQ-02 YAML parses and its cancel inventory matches that contract.

### R3-1 — Active singleton semantics remain in the full-file drafts

The D2 regression scan still finds active, non-historical singleton behavior:

- `execution-model.md`'s glossary says the daemon dispatches exclusively from
  one active queue.
- EM-064 directs the orchestrator to a single active queue and says a new
  submit while any queue is active is rejected.
- The §7.4 main loop reads one `active_queue()` from
  `.harmonik/queue.json`.
- The execution-model dependency map still declares canonical persistence at
  `.harmonik/queue.json`.
- Queue-model QM-061 says QM-027 ensures at most one active queue exists,
  despite QM-027 itself now being scoped per normalized name.

These statements contradict named QueueStore ownership, canonical
`.harmonik/queues/<normalized-name>.json` persistence, and the queue-model's
own multiple-active-queue capacity rule in QM-062. The legacy singleton is
migration input only, so it cannot remain the dispatch authority.

Required correction: reconcile every active execution-model queue glossary,
EM-064, dispatch-loop, dependency, and conformance occurrence with named
QueueStore snapshots and deterministic multi-queue selection. Scope QM-061
explicitly per normalized name. Historical changelog rows may remain
historical.

D1, D3, and D4 remain clean: exact replace/archive paths, root-sync cleanup,
restart classifiers, and corrupt-source handling are intact; all wire records
including corrected cancel compatibility are complete; and group failure plus
queue pause remains one authoritative transaction. `git diff --check` is
clean.

## Post-scope-expansion final durability re-review

Verdict: **REQUEST_CHANGES**

The substantive R3 singleton blockers are resolved. QM-061 is explicitly
per-normalized-name; the execution-model glossary admits multiple active named
queues; EM-015f routes terminal effects to the queue and group identified by
the run event; EM-062 through EM-065 use complete QueueStore snapshots,
all-name duplicate screening, deterministic refill targeting, and per-name
submission; §6.5 is queue-identity scoped; and §7.4 selects from the complete
eligible named set, permits `br ready` only for an empty set, enforces both
global and per-queue capacity, and advances the QM-067 cursor. §10.1 restates
the same conformance contract.

### R4-1 — Execution-model §9.3 cites QM-062 under the wrong section

The corrected dependency map contains:

```text
[queue-model.md §8 QM-062] — queue lifecycle ... and eligible active-queue filtering
```

QM-062 is not in queue-model §8; it is the §9 two-level global/per-queue
capacity rule. Section §8 owns queue lifecycle but has no QM-062 requirement.
The following §9 dependency row lists QM-060, QM-066, and QM-067 while omitting
QM-062, so the map neither cites lifecycle nor capacity accurately.

Required correction: cite queue lifecycle as `[queue-model.md §8]`, and include
QM-062 in the §9 capacity/arbitration dependency row (or make an equivalent
accurate split). Keep the change inside the already-authorized §9.3
queue-dependency region and reconcile any generated evidence claiming the map
is exact.

The bounded execution-model semantic checker otherwise passes: every
authorized region changed, the literal preamble and non-version frontmatter
are preserved, exactly one qualifying history row was added, and normalized
full-file comparison finds no out-of-region drift. D1–D4, shipped
`queue`/`force` plus additive `queue_id` cancel compatibility, both/neither
selector behavior, receipt prebinding and create-before-unlink order,
release/720-hour trusted-clock retention, receipt-first GC, and JSONL
non-authority all remain clean. `git diff --check` is clean.

## Packaged final durability review — 82212d4ce

Verdict: **APPROVE**

R4-1 is resolved. Execution-model §9.3 now cites queue lifecycle accurately as
`[queue-model.md §8]` and cites the capacity/arbitration contract as
`[queue-model.md §9 QM-060, QM-062, QM-066, QM-067]`. The map therefore
locates QM-062 under its real §9 authority while preserving §8 lifecycle
ownership.

The final fallback composition is also exact. EM-066 and EM-067 replace only
their removed singleton `queue IS None` branch references with
`fleet.named_queues IS EMPTY`. The §10.2 pause fixture enables fallback with
`--auto-pull` set, and the adjacent nonempty-ineligible-fleet fixture proves
that existing but wholly ineligible named queues suppress `br ready` and
fallback dispatch.

The bounded full-file checker passes against execution baseline
`82212d4ce569c57298f37a66bb3752870d9896c5`: the literal preamble and
non-version frontmatter are preserved, every authorized region and exact
replacement is unique, exactly one qualifying history row was added, and
normalized comparison finds no out-of-scope execution-model drift.

Final regression checks are clean for D1–D4; literal shipped cancel
`queue`/`force` compatibility plus additive `queue_id`; both/neither selector
behavior; exact replace/archive paths, no-replace/root-sync and restart
classifiers; corrupt-source binding; one-transaction failure/pause; receipt
prebinding and durable creation before canonical unlink; release only after
durable absence; trusted-clock `released_at + 720h`; receipt-first GC; and
JSONL exclusion from recovery authority. `git diff --check` is clean.
