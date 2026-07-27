# Receipt-architecture research review

## Round 2

- Reviewer: `/root/cq02_round6_design_review`
- Execution baseline:
  `b5a7cc6d4972e6e7ee4194ba259122a755963648`
- Scope:
  the approved CQ-02 card, `research-review-receipt-round-1.md`, its writer
  response, all six active research artifacts, `CQ-02.yaml`, and the
  superseded stop markers in `04-design/`, `05-changelog.md`,
  `05-spec-drafts/`, and `07-tasks.md`
- Verdict: `REQUEST_CHANGES`

## Finding

### R2-1 — active research still publishes the pre-serialization task spine

Round 1 required the exact safe landing order:

```text
CQ-02I -> CQ-01 -> CQ-RECEIPT -> CQ-RUN-WAIT -> JR-03
```

The regenerated `CQ-02.yaml` and the `07-tasks.md` stop marker now record that
order, including the reason that `CQ-01` must precede `CQ-RECEIPT` because
both edit `internal/queue/rpc.go`. However, these active research claims still
omit `CQ-01`:

- `01-problem-space.md` under **Review gates**;
- `02-components.md` C9 **Conformance and task DAG**;
- `03-research/queue-model/findings.md` C13 **Required spine**; and
- `03-research/event-model/findings.md` EV5 **Required landing order**.

Each currently states:

```text
CQ-02I -> CQ-RECEIPT -> CQ-RUN-WAIT -> JR-03
```

That is not merely abbreviated context: those sections define the required
task DAG and landing order, so they authorize `CQ-RECEIPT` before the
same-file `CQ-01` work has completed. It also contradicts the Round-1 writer
response’s statement that the slices and conflicts were rebuilt around the
five-node spine.

Update all four active research claims to include `CQ-01` and make the
`internal/queue/rpc.go` serialization reason explicit. Re-run the active
artifact consistency search before Round 3.

## Checks that now pass

The remaining Round-1 findings are resolved:

- `CQ-02.yaml` has the approved top-level evidence schema and passes the
  card’s structural/semantic checks through the intentionally pending final
  durability and composition reviews.
- Completion authority is receipt/queue owned; active evidence contains no
  event-history recovery, missing-event replay, event-delivery refusal, or
  stale event landmark.
- Exact-ID receipt fallback is deterministic and fail closed for zero,
  multiple, corrupt, unsupported, or identity-disagreeing candidates.
- The crash matrix covers receipt-root establishment, receipt installation,
  completed-canonical/receipt/event ordering, queue-ID plus digest CAS cleanup,
  ownership release, retention, and GC ambiguity.
- The evidence DAG correctly serializes `CQ-01`, `CQ-RECEIPT`, and
  `CQ-CALLER-CANCEL` work in `internal/queue/rpc.go`; it also gates `CQ-04`
  and `JR-03` on the required receipt, waiter, reservation, and activation
  predecessors.
- Fresh-submit and shared-append waiter ownership and terminal/error outcomes
  are carried by `CQ-RUN-WAIT`, including missing observations, non-final and
  final success, pause/failure, own-bead completeness, same-name reuse,
  receipt-index integrity, and status transport failure.
- The execution-model proof is explicitly limited to literal preamble
  preservation, version/date, EM-015f, and one qualifying revision row.
- Process-lifecycle capability and cancel-wire evidence covers complete
  startup/upgrade/downgrade/rollback refusal and daemon-unreachable exit 17
  with zero local writes.
- The three design files, changelog, queue-model draft, and task file are
  explicit stop markers and do not present their superseded contents as
  active design, draft, or dispatch authority.

No further receipt architecture or evidence-schema change is requested.

## Focused re-review

- Scope: R2-1 remediation, active spine consistency, and evidence path
  classification
- Verdict: `APPROVE`

All active DAG and landing-order claims now use:

```text
CQ-02I -> CQ-01 -> CQ-RECEIPT -> CQ-RUN-WAIT -> JR-03
```

`01-problem-space.md`, `02-components.md`, queue-model research, event-model
research, the queue-design stop marker, `07-tasks.md`, and `CQ-02.yaml` all
place `CQ-01` before `CQ-RECEIPT`. The active research also states the
reason: both slices edit `internal/queue/rpc.go`, so their work is explicitly
serialized and may not run concurrently.

A focused search found no obsolete four-node spine in active research,
design-stop, draft-stop, task-stop, or evidence artifacts. Occurrences in
prior review artifacts are historical findings, not active contract claims.

The evidence prepackage inventory exactly matches the current 21 in-scope
untracked paths. The Round-2 response and this Round-2 review are both present
in `porcelain_status`, `untracked_paths`, and `classified_paths`, with
`worker_kerf` ownership; the evidence file remains `worker_evidence`. The
observed and classified sets are equal, and the machine DAG still records
`CQ-02I -> CQ-01 -> CQ-RECEIPT`.

R2-1 is resolved. No new contradiction was found, so the earlier
`REQUEST_CHANGES` is superseded by this focused `APPROVE`.
