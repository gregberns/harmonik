# CQ-CALLER-INLINE-EXIT — Migrate post-daemon inline exit archive

## Dispatch metadata

- Group / priority: core queue / P0
- Execution profile: `terra_high`
- Model / effort: `gpt-5.6-terra` / `high`
- Reviewer profile: `sol_xhigh`
- Depends on: `CQ-CALLER-BOOTSTRAP`, `JR-03`
- Conflicts with: `CQ-CALLER-BOOTSTRAP` on `cmd/harmonik/run.go`
- Lease family: `queue_inline_exit`

## Objective and exact ownership

Migrate only `cmd/harmonik/run.go` `runBeadSubcommandIO`'s post-daemon
paused-by-failure archive/exit region and focused tests behind the adopted
QueueStore owner.

## Non-goals

No bootstrap/admission, queue persistence primitive, daemon cancellation,
terminal workloop, unrelated CLI output, or event durability changes.

## Proof and accepted intermediate

Failing-first tests prove the CLI never archives directly, a failed owner call
retains ownership/refusal and returns a truthful nonzero exit, and durable
archive precedes reported success. Mutation restores the local archive and
must fail. Accepted intermediate: inline run has no post-daemon direct queue
writer.

## Completion and rollback

CLI fault/composition/repeat tests, UBS/vet, check-fast, and Sol review pass.
Roll back only the post-daemon exit region/tests. **COMMIT EXPLICITLY.**
