# TASK-ID — Short outcome

## Dispatch metadata

- Group / priority:
- Execution profile:
- Reviewer profile:
- Depends on:
- Work type:

## Objective

One observable outcome. State what is deliberately excluded.

## Evidence to verify first

- Source/spec symbols to inspect.
- Live claims that must be re-derived.
- Existing tests or historical fixes that are evidence, not authority.

## Exclusive lease

Allowed production files, tests, and shared files. Anything else requires a
coordinator-approved task split.

## Required work

1. Failing-first characterization or evidence capture.
2. Smallest production or planning change.
3. Focused and composition-path proof.
4. Return evidence.

## Acceptance

- Observable pass/fail statements.
- Production call-site coverage.
- No-regression behavior.

## Verification

Exact targeted, race/fault/scenario, lint, UBS, and post-commit gates. Mark
environmental proof deferred rather than calling the task green without it.

## Escalate when

The precise boundary at which the worker stops instead of expanding scope.

## Return

Use the directory return contract. **COMMIT EXPLICITLY** for implementation
tasks.

