# CQ-EXPLORE-CANCEL — Explore cancellation and capability fault surfaces

## Dispatch metadata

- Group / priority: core queue / P1
- Execution profile: `pi_ralph` for fixed fixtures; escalate to `terra_high`
- Model / effort: local Nemotron via Pi / high; escalation
  `gpt-5.6-terra` / high
- Reviewer profile: `sol_xhigh`
- Depends on: `CQ-CALLER-CANCEL`, `CQ-04`
- Lease family: `queue_cancel_exploratory`
- Work type: exploratory test; no production implementation

## Objective

Exercise the operator-facing cancel RPC/CLI and capability refusal surfaces
against the approved fixed oracle. This is a new card because closed
`CQ-DEF-01` owns default-harness propagation and must not be repurposed.

## Exclusive scope and non-goals

Own only exploratory/scenario fixtures and reports for daemon-down exit 17,
linked archive parent-sync cuts, incapable downgrade, and audit append failure.
No production code, policy, persistence, shared spine, Beads, or spec edits.
Nemotron/Pi may enumerate the already-approved matrix; any semantic ambiguity
stops and escalates to Sol.

## Acceptance and rollback

Prove zero daemon-down writes, convergence across every prescribed archive
cut, incapable downgrade preserves/refuses, and audit failure does not alter
success. Record exact commands/artifacts. Rollback deletes only new fixtures
and the report. **COMMIT EXPLICITLY.**
