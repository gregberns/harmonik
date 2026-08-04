# Queue dogfood readiness spec-draft changelog

| Draft target | Status | Change | Design source |
| --- | --- | --- | --- |
| `assessor-handoff-schema.md` | reviewed, unchanged | Schema version 2 carries the gate owner and report path. Canary facts stay in mission prose. | `assessor-handoff-schema-design.md` |
| `beads-integration.md` | modified | Adds the retained read-first readiness snapshot and scoped remaining-finding rule. | `beads-integration-design.md` |
| `beads-ledger-events.md` → `docs/queue-readiness-ledger-events.md` | new operational record | Defines the snapshot and event-evidence record. | `beads-ledger-events-design.md` |
| `durability-proof-tests.md` → `docs/queue-readiness-durability-proofs.md` | new operational record | Defines the release-contract table and required durable/fault proofs. | `durability-proof-tests-design.md` |
| `event-model.md` | modified | Adds class-O `queue_recovered` with payload, ordering, and replay rules. | `event-model-design.md` |
| `execution-model.md` | modified | Defines shutdown drain and the immutable Git-backed release claim used to reconstruct unfinished DOT release. | `execution-model-design.md` |
| `handler-pause.md` | modified | States the separation of handler resume and queue recovery. | `handler-pause-design.md` |
| `lanes-handoffs.md` → `plans/2026-07-27-delete-and-rewrite/LANES.md` | modified | Complete plan copy plus ownership, shared-boundary review, work order, and handoff content. | `lanes-handoffs-design.md` |
| `operator-nfr.md` | modified | Defines committed DOT drain completion, safe normal-watchdog behavior, and controlled-load evidence. | `operator-nfr-design.md` |
| `process-lifecycle.md` | modified | Adds the direct `queue-recover` RPC and CLI contract. | `process-lifecycle-design.md` |
| `queue-model.md` | modified | Defines the durable per-queue failed-item recovery transaction and response records. | `queue-model-design.md` |
| `run-state-machine.md` | modified | Replaces the conflicting no-sync shutdown clause with synchronized close-or-reopen behavior. | `run-state-machine-design.md` |
| `scratch-daemon-runbook.md` | modified | Adds the local, non-Pi, one-item readiness procedure and evidence retention. | `scratch-daemon-runbook-design.md` |
| `step9-core-loop-artifacts.md` → `plans/2026-07-27-delete-and-rewrite/DECOMPOSITION-MAP.md` | modified | Complete plan copy plus the local readiness target and retained Step 9/core-loop evidence. | `step9-core-loop-artifacts-design.md` |

`readiness-evidence-design.md` is the integration design for the scratch,
Step 9, durability, ledger, and lane records. It introduces no separate target.

The four operational records are not new normative specs. The two new records
have the declared `docs/` paths above. The lane and Step 9 draft filenames are
Kerf component names. Each contains the complete target file named above.
