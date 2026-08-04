# Change Design review — Queue dogfood readiness

## Round 1 — APPROVED

The design records cover every affected area from `02-components.md`. The
queue record owns durable failed recovery. The execution and run-state records
own one terminal-recovery matrix. The lifecycle, operator, workspace, and
event records assign command, ordering, lease, and observation duties without
adding a second terminal path. The assessor and runbook records keep the first
canary limits out of the general queue model.

The selected design uses a dedicated readiness gate, an explicit durable
recovery record, a durable failed-recovery receipt, and Class-F recovery
events. No conflicting owner or untraced target state remains.
