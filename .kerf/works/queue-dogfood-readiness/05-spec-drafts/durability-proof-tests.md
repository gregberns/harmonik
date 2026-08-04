# Queue readiness durability proof contract

## 1. Purpose

This contract defines the proof required before queue-dogfood readiness can
advance to an assessor gate. It covers reservation, release, failed-queue
recovery, and completion. It does not replace the owning subsystem specs.

## 2. Release-contract table

| Edge | Owner | Durable state observed | Production-path proof | Fault proof | Ratchet |
| --- | --- | --- | --- | --- | --- |
| Reserve item | QueueStore transaction | Canonical queue candidate with item status and run ID | Dispatch one item and read the canonical queue | Inject candidate-write failure and prove no item becomes dispatched | Source guard may name QueueStore only |
| Release committed DOT run | Run terminal spine and merge bridge | Git run branch, target branch, and bead terminal state | Stop after commit and prove merge before close | Inject synchronization or merge failure and prove reopen without close | Source guard may require the drain bridge call |
| Recover failed queue | QueueStore transaction | Canonical queue candidate with re-armed items, active group, and active queue | Call `hk queue recover` and inspect the durable queue before dispatch | Inject replacement failure and prove old bytes, memory, and quarantine remain | Source guard may require the named transaction operation |
| Complete queue | QueueStore transaction and receipt protocol | Completed canonical bytes and immutable completion receipt | Drive final item success and inspect the receipt | Inject a receipt or canonical write failure and prove no completion response | Source guard may require the receipt path |

## 3. Required proof properties

Each production-path test MUST exercise the real mutation owner. It MUST fail
when the durable write it claims to observe is removed. Each fault test MUST
inject failure at the persistence boundary. It MUST prove that the old durable
candidate remains the only authority. A source ratchet is optional. A ratchet
MUST NOT be the sole evidence for durability.

The release tests use one daemon suite at a time. The evidence records host
load, suite concurrency, command, source revision, and artifact path. A result
outside that load rule is machine-contention evidence until a controlled rerun
classifies it.

## 4. Ownership

Alpha owns daemon shutdown and daemon-side release tests. Bravo owns queue,
queue-wiring, CLI recovery, non-daemon proofs, and ratchets. Both lanes review
the release-contract table before either changes a shared boundary.

## 5. References

- `specs/queue-model.md` QM-001, QM-052b, QM-060, and QM-063.
- `specs/execution-model.md` EM-031b and EM-053a.
- `specs/run-state-machine.md` RSM-020 through RSM-022.
- `specs/operator-nfr.md` ON-027b and ON-032a.
