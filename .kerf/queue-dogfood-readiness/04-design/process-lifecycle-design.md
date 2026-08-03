# Process lifecycle change design

## Current state

The queue CLI sends `operator-resume`, which resumes only drain-paused queues.
Shutdown treats post-commit work as an ordinary checkpoint.

## Target state

Specify a distinct failed-recovery RPC and CLI command. Define accepted queue
states, rejected and no-op results, receipt fields, and durable success before
response. Define committed-but-unmerged work as a drain state: drain its
existing ladder or persist recovery before exit. Define the controlled batch as
a separate assessor-gated lifecycle operation.

## Rationale

The command must not silently claim failed recovery when it only resumes drain.

## Requirements traceability

Addresses lifecycle command, drain, and assessor-boundary requirements.
