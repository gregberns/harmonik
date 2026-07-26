# Spec draft review

## Round 1 — REQUEST_CHANGES

The reviewer found two cross-spec blockers:

1. Execution and Handler still described same-run-workspace reviewer execution and retained target input,
   contradicting Workspace Model's mandatory disposable box-A reviewer projection.
2. Execution's `needs-attention` glossary keyed off raw non-APPROVE verdict and omitted new terminal paths.

## Resolution

- EM-015d-RIA now creates and uses the mandatory projection, treats its target/verdict as staging, and
  removes the target with phase cleanup after authoritative archive transfer.
- HC-006a now requires reviewer environment, working directory, and agent task in the projection.
- `needs-attention` is selected from normalized outcomes: cap, block, fix-up stall, and error; normalized
  flagless request-changes approval is excluded.

## Round 2

REQUEST_CHANGES. The reviewer found remaining unqualified schema/state text: Handler `workspace_path`
still meant only run worktree, and Execution still read/archived unqualified worktree-relative verdicts.

## Round 3

APPROVE after making `LaunchSpec.workspace_path` phase-specific and explicitly reading the reviewer
projection then atomically transferring to the run-workspace archive.
