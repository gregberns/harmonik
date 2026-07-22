# SESSION — process-group-provenance

**Crew:** kilo · **Epic:** hk-220lv (keeper reliability) · **Queue:** kilo-q · **Reports to:** captain
**Last updated:** 2026-07-22, after the pass-5 APPROVE

## Where the work stands

**Status: `spec-draft` (pass 5). Drafts complete and REVIEWED — verdict APPROVE.** The reviewer
confirmed the normative text implements the approved design and that both pass-4 corrections landed.
Two minor items came with it: the A7 rationale note (now applied under PL-005) and the missing
reap-on-RESTART obligation (out of scope for these three files; filed as bead **hk-3eurz**).

**RESOLVED — nothing holds advancement now.** A measured finding after the APPROVE opened a second
non-coverage boundary (`FINDING-darwin-marker-unreadable-on-apple-binaries.md`): on darwin, `ps -E`
returns an empty environment for Apple-signed system binaries, so a `/bin/zsh` orphan is
marker-invisible and spared forever. Safe polarity, incomplete claim. The captain ruled at 12:23Z —
carry it into finalization, no separate review round — and **the row is applied**, in three places
so they cannot drift: the PL-021b §7 coverage table (fourth row), PL-006e(4) (measurement plus a
pointer to the row), and the revision history (two rows, not one).

**Next step is FINALIZE.** Then this work parks: the captain realigned kilo onto the P1
kernel-fabric plan (`plans/2026-07-21-platform-architecture`) per the operator's program.

Passes 1–4 complete. Pass 4 was reviewed once (REQUEST_CHANGES on two overstated claims, both fixed
and recorded in `change-design-review.md`) and advanced. Passes 6 (integration) and 7 (tasks) are not
started — `kerf square` reports NOT SQUARE for that reason, which is correct, alongside phantom
per-research-question filenames that are a defect in the check (see
`04-design/README-component-mapping.md`).

## What this work decides

How harmonik recognises its own leftover processes, so it can clean them up without killing
processes belonging to other projects or to the operator. The resolution is a single inherited
environment marker read portably on both platforms (`B4-strict`), with the weight of the change
moved onto **marker write coverage** rather than the read mechanism — because measurement showed the
long-lived process population carries no marker to read.

## Files that matter, in reading order

1. `04-design/process-lifecycle-design.md` — the main design. §0.2 measurements, §5 resolved
   decisions, §6 the coverage table with its one dated non-coverage row.
2. `04-design/FINDING-generation-nonce-mint-point.md` — found after the design was submitted; it
   invalidates the design's own proposed mint point for the generation nonce. Verdict-independent.
3. `change-design-review.md` — round-1 review findings and how each was applied.
4. `05-spec-drafts/{process-lifecycle,handler-contract,beads-integration}.md` — the drafted spec
   text, each a complete updated file.
5. `05-changelog.md` — what changed in each file and why, with bead traceability.

## The two things not to soften

1. **The non-coverage row in PL-021b §7.** A descendant that leaves its session while its root is
   alive is reached by nothing. It is dated and bounded, and it must survive into the final spec.
   Equally: it must not be drawn *wider* than that — the review caught the first draft claiming the
   whole `setsid` population was unreachable, which would have let the bead this work fixes be closed
   as unfixable.
2. **Marker-write and matcher-discipline ship together.** Either half alone is defective in a
   different direction: the matcher without the marker is a sweep that silently matches nothing; the
   marker without the matcher widens every reaper's candidate set with no new guard.

## Adjacent state

- `watcher-reap-scope` — committed `c718ca98` on `fix/watcher-reap-scope`, reviewed APPROVE, awaiting
  the captain's promote on CI-green. Nothing outstanding on it.
- `hk-pvrfx` — committed `79a96cd`, reviewed APPROVE, queued for the same batch.
- Beads owned by this work: hk-n93gq, hk-o7x4w, hk-g1qby, hk-c6dt2, hk-5z4ww. All held behind this
  work; none should be closed until the spec lands. hk-o7x4w in particular stays uncommitted — its own
  patch arms a fail-open exclusion on a kill path.

## Constraint on how this work gets reviewed

This crew session cannot spawn subagents, so every review round is captain-routed. Do not self-stamp
a verdict, and do not mark a non-trivial change trivial to get past the commit hook.
