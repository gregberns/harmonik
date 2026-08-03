# Tasks Review — Declared Substrate Capability Contract

## Round 1

BLOCK. The capability-record task overlapped consumer migration. The run and
run-session tasks overlapped the same per-run code. The boot task could bypass
the Step 12 serialization. The scenario task did not name a real selection
fixture for base-only and provider substrates.

## Round 2

APPROVE. T1 is record-only. T2 excludes both shared watcher calls. T3 owns the
active per-run boundary and T4 follows it with atomic run-session removal. Any
parallel T2 and T3 work must first prove symbol separation. The scenario task
uses a test-only construction seam and adds no production CLI selector. The
two required validation tasks are explicit dependencies.
