# Queue transaction and durability contract — Integration

## Package boundary

This integration pass verifies planning/spec composition only. It does not
copy normative specs, change production code, mark implementation tasks
complete, commit, push, or run `kerf finalize`.

The coordinator packaging operation is a byte-for-byte copy:

| Draft | Normative target | Git blob | SHA-256 |
|---|---|---|---|
| `05-spec-drafts/queue-model.md` | `specs/queue-model.md` | `ea8c23cf2e2dcb057e133ccb264b2f2a106f6e0b` | `01a7313e892f204267448e72aa8e3fdf8fb8489aec54729358edcc6a2e9e59a3` |
| `05-spec-drafts/process-lifecycle.md` | `specs/process-lifecycle.md` | `814dd8f3b299f346f03b981ebb1317cccacfd7e3` | `beae6b8e2d621f25d1e957b09f79cae832796581be4693b17be956983b66b02f` |
| `05-spec-drafts/event-model.md` | `specs/event-model.md` | `a499c7debf722a1f0a73fe52d6126ca8a0718398` | `43d342fb338912e3e506e27319249d702bb01f648374dcabe85da0f61bf66932` |
| `05-spec-drafts/execution-model.md` | `specs/execution-model.md` | `4d897fe3ae4f39a695889beb7c0f0c36149b2221` | `36b01a1a54eb36ca579894d45a975fa6a8d8cb9b4c98f81d9fd1b40500d39c49` |

## Cross-spec resolution

- Queue-model owns named identity/lifecycle, QueueStore mutation and recovery,
  completion receipts, retention/GC, per-queue workers, and QM-067
  arbitration.
- Execution-model consumes complete immutable named-fleet snapshots, performs
  all-name duplicate checks, uses per-name submission, applies global plus
  per-queue capacity, and permits `br ready` only for an empty named set.
- Event-model observations remain diagnostic and non-authoritative. No event
  outbox, replay cursor, segmented JSONL, or recovery scan was introduced.
- Process-lifecycle owns startup recovery/capability negotiation and
  daemon-routed cancellation; daemon-unreachable cancellation writes nothing.
- The waiter queries exact queue identity/status/receipt authority and cannot
  be stranded by an absent final observation.
- The 21-slice implementation DAG serializes shared files and gives each
  critical production caller one owner. It is a plan, not completed work.

## Approved reviews

Durability:

- reviewer: `/root/cq02_round6_design_review`
- profile: `sol_xhigh`
- verdict: `APPROVE`
- artifact:
  `spec-draft-review-receipt-durability-round-1.md`
- Git blob: `c56109cadc2ad655e306bccd0bc8aa91483ea024`
- SHA-256:
  `1be5023e58927cdc87a6297f0799856f64492a23ac3f486db6e51dae4c5dba26`

Composition:

- reviewer: `/root/cq02_spec_draft_review`
- profile: `sol_xhigh`
- verdict: `APPROVE`
- artifact:
  `spec-draft-review-receipt-composition-round-5.md`
- Git blob: `8f7e8710a8628d6513ed7e561a06a47436a5c17d`
- SHA-256:
  `1f60ef063e08cd7b8e41e9e7ffd37a7d424ef6ec178983727199fbc7e1968bec`

The reviewers are independent and their artifacts are immutable inputs to
coordinator packaging.

## Coordinator-only next step

The root coordinator, while holding the single-writer packaging lease, may:

1. re-run the exact card and semantic validators;
2. copy each draft to its mapped normative target byte-for-byte;
3. verify each target with both `cmp` and the hashes in this file;
4. stage only the Kerf package, CQ-02 evidence, and four normative targets;
5. commit with the repository-required `Reviewed-By` and JSON
   `Review-Verdict` trailers;
6. run scoped UBS, `/check` or `make check-fast`, and the required milestone
   gate before push.

Do not run `kerf finalize` as part of this handoff. Do not infer that any
implementation task in `07-tasks.md` is complete.
