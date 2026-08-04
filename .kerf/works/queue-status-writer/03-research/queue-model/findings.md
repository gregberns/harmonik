# Queue model findings

## Questions

1. Which transitions and durable-write rules constrain the API?
2. Which direct writers are in this lane?
3. Which status initializers are not live transitions?
4. What transaction path already has the required failure behavior?

## Findings

- `queue-model.md` §§2, 5, and 8 define queue, group, and item transitions.
  QM-030 gates group completion on terminal items. QM-051 and QM-052 couple
  group results with successor activation or queue pause.
- QM-001, QM-060, and QM-063 require a detached candidate, durable replace,
  then memory install. Observation happens only after durable commit.
- There are 16 non-daemon direct assignments: seven in `internal/queue`, seven
  in `internal/lifecycle/startup_pl005_qm002.go`, and two in
  `internal/queuewiring/operatorevents.go`. The 14 daemon assignments stay out
  of this slice.
- New queue and append candidates use status literals before publication. They
  are initial-state formation, not live transitions. The direct-assignment
  ratchet must report them separately.
- `queuewiring.QueueStore.Transact` is the working durable boundary. It clones,
  persists, and installs only on `OutcomeCommittedDurable`. Failed writes leave
  the installed queue unchanged and can quarantine that name.

## Patterns to follow

- Use narrow operation types. Do not expose a generic status setter.
- Keep event emission outside `Transact` and after a durable result.
- Preserve operation-specific data such as run IDs, attempts, reasons,
  completion time, receipt, and archive behavior.

## Risks

- Live-pointer mutation followed by `Persist` exposes a successful memory state
  after a failed write.
- A generic operation can bypass terminal-item gates or coupled group and queue
  mutations.

