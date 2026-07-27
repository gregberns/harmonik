# Durability Round-4 response

The post-scope-expansion durability review's `R4-1` finding is accepted.

Execution-model §9.3 now cites queue lifecycle as `[queue-model.md §8]`.
QM-062 is cited only in the §9 dependency row, together with QM-060, QM-066,
and QM-067, where it owns global/per-queue capacity composition.

The correction stays inside the already-authorized §9.3 queue-dependency
region. No durability, receipt, retention, cancellation, event, Run, or
non-queue contract changed.

No normative `specs/` file, production code, task index, Beads ledger, commit,
or reviewer artifact was changed. Ready for focused durability final
re-review after the complete validation set passes.
