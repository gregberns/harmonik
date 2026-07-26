# CQ-DEF-01 — Preserve a queue's default harness through dispatch

## Dispatch metadata

- Group / priority: core queue / P0
- Execution profile: `sol_high`, escalate to `sol_xhigh`
- Reviewer profile: `sol_xhigh`
- Depends on: none
- Work type: production wiring defect on the shared queue/run spine

## Objective

Make a persisted `Queue.DefaultHarness=pi` reach both quiet model resolution and
the launch-time harness resolver for an unlabeled queue item. Preserve tier-1
bead-label precedence and the current global-default behavior when the queue
field is absent.

This task owns only default-harness propagation. It does not change queue
admission policy, Pi concurrency policy, model/profile semantics, or the
reviewer harness rule.

`RunEnv.DefaultHarness` is the existing global tier-4 default and must retain
that meaning. Do not overwrite or repurpose it with the queue value. Carry the
persisted queue tier-2 value in a distinct immutable field (for example,
`QueueDefaultHarness`) so the resolver receives both tiers independently.

## Evidence to verify first

- Current `internal/daemon/workloop.go` `snapshotFleet`, `selectNextQueue`,
  `buildRunBundles`, and `beadRunOne` drop the queue field.
- `internal/orchestrator/select.go` `QueueSnapshot` and `Selection` do not carry
  it.
- `internal/daemon/runports.go` `runEnv` and `internal/runloop/ports.go`
  `RunEnv` do not carry it.
- `internal/daemon/harnessresolve.go` still describes tier 2 as a stub.
- Historical commits `5ab18fc4b` and `bdab1adb6` show an earlier fix, but are not
  ancestors and predate the selector/run-bundle extraction. They are reference
  material, not patches to cherry-pick.

## Exclusive lease

- `internal/orchestrator/select.go` and focused selector tests
- `internal/daemon/workloop.go`
- `internal/daemon/runports.go`
- `internal/runloop/ports.go`
- `internal/runloop/reviewerharness_hkiv748.go` and focused tests
- `internal/daemon/harnessresolve.go`
- exact default-harness production-path tests under `internal/daemon/`,
  `internal/orchestrator/`, or `internal/runloop/`

`internal/daemon/reviewloop.go`, `dot_cascade_core.go`, and `dot_gate.go` are
read/test targets, not writable by default. If the value must cross one of those
production call sites, stop for a coordinator lease amendment before editing.
No queue persistence or Pi launch-spec edits.

## Required work

1. Add `TestQueueDefaultHarnessProductionPath`: start with a queue carrying
   `DefaultHarness=pi`, select its unlabeled item, build the run environment,
   and observe Pi at the resolver/launch-builder boundary.
2. Carry the immutable queue value through queue snapshot → pure selection →
   daemon selection → goroutine capture → a distinct `RunEnv` queue-default
   field → `buildRunBundles`; preserve `RunEnv.DefaultHarness` as tier 4.
3. Pass the same value to `resolveHarnessAgentTypeQuiet`; quiet model selection
   and launch-time harness selection must agree.
4. Add `TestSelectQueueDefaultHarness` with precedence cases: valid tier-1
   label wins, empty queue default preserves the golden fallback, and a valid
   non-Pi queue default is not hard-coded away.
5. Add
   `TestQueueDefaultHarnessDoesNotOverrideGlobalReviewerDefault` and prove that
   a captured implementer queue default such as Pi neither overwrites the
   global tier-4 default nor makes an unsupported reviewer use Pi, including
   the DOT gate.
6. Update stale tier-2 comments without changing the four-tier contract.

## Acceptance

- An unlabeled item from a `default_harness: pi` queue selects Pi in the actual
  run-bundle production call path.
- `harness:codex` on that item still wins at tier 1.
- Empty/legacy queues produce byte-equivalent effective routing to the current
  default path.
- Quiet model resolution uses the same effective harness as launch resolution.
- The queue tier-2 field and global tier-4 `RunEnv.DefaultHarness` coexist; a
  queue default never becomes the global or reviewer default by field reuse.
- No constructor or test fixture silently leaves the new immutable field unset
  where a queue selection exists.

## Verification

- `go test ./internal/orchestrator -run TestSelectQueueDefaultHarness -count=1`
- `go test ./internal/runloop -count=1`
- `go test ./internal/daemon -run 'TestQueueDefaultHarnessProductionPath|TestQueueDefaultHarnessDoesNotOverrideGlobalReviewerDefault|TestReviewerHarness' -count=1`
- `go test -race ./internal/daemon -run TestQueueDefaultHarnessProductionPath -count=10`
- `go vet` on the changed packages; delta lint; `ubs` on every changed Go file.
- Independent reviewer must find the production call site and compare the
  historical fix with the current extracted path.
- Post-commit `make check-fast`; primary-daemon proof remains deferred.

## Escalate when

Stop if the fix requires changing queue admission, model precedence, reviewer
inheritance, DOT node defaults, or any runloop interface rather than adding an
immutable value field to the existing bundle.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
