# Workspace placement and reviewer-control artifacts design

## Current state

Workspace Model describes review artifacts relative to one run workspace. Production instead has a local
or worker-remote implementer workspace and a box-A-local detached reviewer projection. Target, live
verdict, and budget sentinel originate in the projection. Local verdict copy/archive works; remote archive
and feedback transfer do not.

## Target state

Amend WM-027a to define:

- **Run workspace:** authoritative lease and task branch, local or remote, containing work product and the
  durable review-control archive.
- **Reviewer projection:** short-lived, unleased, box-A-local detached checkout of the verified
  implementation SHA.

The projection is not a second run lease. Its identity is the tuple
`{run_id, iteration, source_ref, source_sha, location}` and its path/name is deterministically
reconstructable from that tuple.

Every artifact operation names its location; nullable runner/path inference is forbidden. Require:

1. explicit SHA transfer to box A and projection creation at that SHA;
2. target materialization and reviewer execution in the projection;
3. local read/validation of projection `review.json`;
4. atomic transfer to run-workspace `.harmonik/review.iter-<N>.json` and current alias when needed;
5. run-workspace feedback write before implementer resume/Retask;
6. budget-diagnostic transfer before observable outcome;
7. projection removal only after session, observers, validation, and transfers complete.

Register the projection for startup/crash reclamation. Reclamation validates the identity tuple and proves
there is no live phase owner before removal; it must not infer ownership from path shape alone.

Amend §6.2 to classify target/live verdict/budget sentinel as projection staging, per-iteration verdict and
feedback as authoritative run-workspace artifacts, and current verdict as an archive alias. Retain WM-026
atomic writes and WM-013e checkpoint exclusion.

Class-F events remain routing/audit facts and do not replace durable files.

Introduce explicit-location artifact operations during extraction. Local parity is the first gate; full
remote transfer is a staged corrective consumer of the same boundary.

## Rationale

This preserves box-A review while restoring one durable authority and preventing accidental SSH access to
local paths, local writes aimed at worker paths, lost remote archives, and feedback-free remote resume.

## Traceability

- Component: C3; WM-013e/026/027a and §6.2.
- Dependencies: EM-015d RIA/RFD and C4 teardown.
- Tests: local/remote verdict, remote feedback, transfer failure, budget diagnostic, deterministic
  identity/no-second-lease, cleanup ordering, and orphan projection reclamation.
