# Workspace and artifact-location findings

## Questions

1. Where do artifacts actually live for a remote implementer and local reviewer?
2. Does that conform to the current same-worktree contract?
3. What explicit location model describes the system?
4. How are branch and feedback transferred?
5. What cleanup ownership is missing?

## Evidence

- WM-010/011 require implementer and reviewer to occupy the same leased run worktree sequentially.
- WM-027a and EM-015d place review target, verdict, archive and feedback under the run workspace.
- Current code fetches a remote implementer branch directly to box A, creates a detached box-A reviewer
  scratch worktree, and runs the reviewer locally.
- Review target, live verdict and budget sentinel exist only in that reviewer projection.
- Local verdicts are copied/archived to the authoritative run worktree; remote copy/archive is skipped.
- Remote `REQUEST_CHANGES` feedback is not transferred to the worker, so the intended loop cannot complete.
- Reviewer projection cleanup is deferred to loop exit; verdict watchdogs and tap subscriptions have no
  explicit cancel/join.

## Actual location model

1. **Run workspace** — authoritative task branch and work product, local or on a named worker.
2. **Reviewer projection** — phase-owned, box-A-local detached checkout from a verified transferred SHA.
3. **Run control archive** — the authoritative run-workspace copy of durable review artifacts. The
   class-F verdict event is authoritative for routing and audit chronology, but does not replace the
   Workspace Model's durable artifact obligation in this work.

Every operation must name its location. Nullable runner or concrete-runner inference is insufficient.

## Conflicts

- Same-worktree prose conflicts with isolated reviewer projections.
- Persistent review-target/verdict promises require transfer into the run control archive before scratch
  cleanup.
- Remote feedback and archive behavior violate EM/WM requirements.
- Reviewer projection names are outside canonical run-worktree discovery and lack crash reclamation.

## Required amendments

- Permit and define a phase-owned reviewer projection without a second run lease.
- Define projection identity by run, iteration, source ref/SHA and location.
- Assign target/live verdict/budget to the projection and feedback to the authoritative run workspace.
- Preserve the existing durable-file contract: transfer verdict/archive and feedback into the
  authoritative run workspace. The class-F event remains the routing/audit fact, not a file replacement.
- Require explicit branch transfer and location-aware atomic feedback delivery before implementer resume.
- Require observer/session quiescence before projection removal.

## Required tests

- Local/remote artifact-location matrix.
- Remote `REQUEST_CHANGES → feedback transfer → commit → fetch → APPROVE` round trip.
- SHA/ref transfer integrity and stale-input rejection.
- Artifact persistence policy after cleanup.
- Watcher cancellation/join before projection removal.
- Crash reclamation of orphan reviewer projections.
- Active proof that reviewer I/O never uses the worker runner.
