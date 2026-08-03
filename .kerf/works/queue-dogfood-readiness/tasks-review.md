# Tasks Review

## Round 1

Two independent read-only reviews checked `07-tasks.md` against every target in
`05-changelog.md`, the drafted contracts, and the declared test beads.

The first review approved the recovery-code range and task graph. It confirmed
that `-32030..-32036` follows the handler reservation `-32020..-32029`.

The second review found one sequencing mismatch. T10 said final document
integration preceded scenario validation, but the initial graph allowed T11 in
parallel. The corrected plan has `T1–T9 → T10 → T11 → T12` in both task fields
and graph.

The final focused review approved the corrected dependency order. Both reviews
confirm that all sixteen scenario and exploratory test beads exist, remain
open, and are named in the plan.

## Post-review amendment

The approval above predates T5a. T5a writes the immutable Git release claim
that T6 needs for restart reconstruction. The updated graph is
`T5 → T5a → T6`; final integration waits for T5a with the other implementation
tasks. Re-review this shared core boundary before finalization.

## Prior verdict

APPROVE. This verdict applies to the pre-amendment task list. No reviewer
started a daemon or changed operational state.
