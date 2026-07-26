# PI-Q2A — Define effective-Pi queue admission

## Dispatch metadata

- Group / priority: Pi lifecycle / P0
- Execution profile: `sol_xhigh`
- Reviewer profile: `sol_xhigh`
- Depends on: `CQ-DEF-01`, `CQ-00`
- Work type: admission contract/spec design

## Objective

Define how queue admission resolves effective harness from bead labels, queue
default, and global default; require effective Pi work on a named non-main queue
with an explicitly supplied worker cap. Decide cap provenance/migration and
invalid selector behavior.

## Exclusive lease

Kerf/spec/task evidence only. No production code.

## Acceptance

Submit and append rules, label precedence, explicit-cap provenance, error codes,
legacy queue handling, and required narrow ledger label port are normative and
reviewed.

## Verification

Spec validation and Sol cross-group review.

## Escalate when

Surface policy choices about legacy queues or invalid harness values.

## Return

Return finalized contract. **COMMIT EXPLICITLY.**

