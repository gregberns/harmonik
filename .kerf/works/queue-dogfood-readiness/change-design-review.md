# Change Design Review — Queue dogfood readiness

## Round 1 — REQUEST_CHANGES

The review required five separate operational design records. It also required:

- reservation release and undo transaction rules;
- transaction completion before recovery event and wake;
- selector order and assessor boundary details;
- scoped records for confirmed live findings;
- shutdown-window proof; and
- explicit assessor gate-owner and mission-body facts.

All findings were corrected.

## Round 2 — APPROVE

The reviewer confirmed that all nine normative spec areas and five operational
areas have a design. Queue transaction, recovery event, and wake order agree
across the designs. Selector order, assessor boundary, scoped findings,
shutdown proof, and gate ownership are explicit. The designs remain within the
queue-dogfood readiness scope.

## Verdict

APPROVE — advance to Spec Draft.

---

## Folded in from lane bravo's parallel pass (2026-08-03)

Lane bravo ran the same pass on branch `work/queue-dogfood-readiness` before any
lane contract named that branch. The two passes reached the same shape. Bravo's
text is kept below because it names evidence, measurements and review records
this document does not. The plan of record stays T1..T12 plus T5a in
`07-tasks.md`. Where the two disagree on behaviour, the section above wins.

### Change Design review — Queue dogfood readiness

#### Round 1 — APPROVED

The design records cover every affected area from `02-components.md`. The
queue record owns durable failed recovery. The execution and run-state records
own one terminal-recovery matrix. The lifecycle, operator, workspace, and
event records assign command, ordering, lease, and observation duties without
adding a second terminal path. The assessor and runbook records keep the first
canary limits out of the general queue model.

The selected design uses a dedicated readiness gate, an explicit durable
recovery record, a durable failed-recovery receipt, and Class-F recovery
events. No conflicting owner or untraced target state remains.
