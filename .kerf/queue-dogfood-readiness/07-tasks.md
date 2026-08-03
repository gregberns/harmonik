# Implementation tasks — Queue dogfood readiness

## 1. Durable queue recovery — Bravo

Implement `QM-058` and `QM-059` in `internal/queue` and
`internal/queuewiring`. Route failed recovery, reservation undo, terminal
release, and adoption through one transaction owner. Preserve completed items,
clear retired run IDs, persist a receipt, and keep quarantine sticky.

**Accept:** a failed recovery survives restart, repeats without mutation, and a
failed write leaves disk and memory equal. **Validates:** br-4nu, br-yws.

## 2. Terminal-recovery record and drain — Alpha

Implement `EM-053a` and the RSM amendment in `internal/daemon`,
`internal/runexec`, and `internal/runloop`. Persist the post-commit record and
wire shutdown to the existing terminal spine.

**Accept:** every recovery matrix row selects one action and never dispatches,
merges, closes, or advances twice. **Depends on:** 1. **Validates:** br-0ri,
br-86i, br-pry, br-zv2.

## 3. Recovery command and observation — Bravo with Alpha review

Implement `PL-032`, `PL-033`, and `EV-051` in queue CLI/RPC, the daemon-owned
operator-event consumer, and event payloads. Keep drain resume unchanged.

**Accept:** the command returns accepted, no-op, or rejected only after durable
state change and emits the matching Class-F record. **Depends on:** 1, 2.
**Validates:** br-2dd, br-s3q, br-r52, br-zez.

## 4. Workspace recovery lease — Alpha

Implement `WM-041` in workspace lifecycle and startup adoption. Retain the
committed worktree and branch until terminal recovery selects an outcome.

**Accept:** restart adopts the recovery worktree before stale sweep and creates
neither a fresh worktree nor a duplicate terminal action. **Depends on:** 2.
**Validates:** br-9an, br-cm3.

## 5. Ordered drain and proof policy — Alpha

Implement `ON-052`. Replace the ambiguous global shutdown timeout behavior
with the specified terminal-recovery outcome and record controlled-load proof.

**Accept:** timeout records recovery state and keeps resources until their
owner releases them. **Depends on:** 2, 4. **Validates:** br-bst, br-3od.

## 6. Readiness handoff and scratch proof — Bravo

Implement schema version 3 and the controlled-readiness runbook procedure.
Update the assessor author, validator, parser, and scratch script together.

**Accept:** a readiness mission rejects incomplete data, pins the candidate,
rejects broad input, retains its evidence manifest, and cannot activate work
after PASS. **Depends on:** 3, 5. **Validates:** br-8ur, br-cvt, br-sob, br-y50.

## 7. Read-only stale-ledger triage — Bravo

Verify each stale graph finding against present source. Close only a proven
stale finding. File a current scoped defect for any remaining condition.

**Accept:** each closure has a source-backed explanation. **Depends on:** none.

## Dependency graph and parallel work

`1 → 2 → {3,4} → 5 → 6`. Task 7 runs in parallel with tasks 1 through 6.
Alpha owns daemon and workspace tasks. Bravo owns queue, CLI, schema/runbook,
and triage. Tasks 3 and 6 require the stated cross-owner review before merge.

The listed scenario and exploratory beads are validation tasks. They remain
open until their implementing task is complete and independently reviewed.
