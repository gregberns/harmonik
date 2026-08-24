<!-- TIER: 2 (operational state, days cadence)
     LOADED BY: captain @ STARTUP Step 0b; NOT loaded by crews or implementers
     OWNER: captain, updated at session end (before HANDOFF.md) or on any crew/epic change
     DO NOT PUT HERE: standing behavioral rules (→ orchestrator-rules skill);
                      this-session salvage / run-id play-by-play (→ HANDOFF.md tier-1);
                      durable phase/locked decisions (→ project.yaml tier-3) -->

# Tier-2 context: captain lane registry + medium-term tracker (days cadence)
# Captain reads on every boot (STARTUP.md Step 0b) BEFORE re-deriving lanes.
# Keep this SHORT — one current-truth block. Superseded history is DELETED, not archived here.
# Pre-freeze lane history: .harmonik/archive/2026-07-12-freeze-and-carve/ (not boot-read).

## CURRENT TRUTH 2026-08-24 — daemon UP and dispatching; work batches before it merges.

> Replaces the 2026-07-22 block, which was six weeks stale: it described an inline mode
> with the daemon queue OFF and named five crews (india, juliet, kilo, lima, mike) that are
> registered but long offline. Superseded text is deleted, not archived here — git holds it.
> Verified from `scripts/captain-boot-digest.sh`, `harmonik queue status`, and the live
> mission files, not carried forward from the previous block.

The daemon dispatches real beads. Work lands on `work/charlie-batch-1`, and only a batch
that passes the gate in `BATCH-GATE.md` merges into `work/alpha-integration-merge`.

**The two branch keys behave differently, and the difference points the dangerous way.**
`lands_on` is re-read per run — `internal/daemon/branching.go` `resolveBranchingFrom` calls
`branching.LoadCached`, which invalidates on the file's mtime — so an edit takes effect on
the very next run with no restart. `protect_branches` is boot-pinned: `daemon.Start` reads
it once through `bootconfig.Resolve`, so an edit to it IS inert until the daemon restarts.
Confirm the live values with the `daemon_config` event, which prints them.

### Lanes (live)
- **alpha** — plans the `plans/2026-08-23-clear-the-ground/` program. Writes and ranks the
  task files; does not implement. Its product is that charlie always has ready work.
- **charlie** — runs those beads through the queue and confirms each lands on the batch
  branch. Does not implement, and does not open a second queue.
- **bravo** — the queue, `internal/core`, and everything outside the daemon package, through
  the delete-and-rewrite program.
- **admiral, assessor** — registered; oversight.
- **india, juliet, kilo, lima, mike** — registered but offline for weeks. Reconcile or
  deregister them; do not read them as staffed lanes. Note that the one open epic,
  `hk-scaj0` (uniform sandbox across harnesses), is still assigned to lima — so it reads
  as staffed and is not. Reassign it or clear the assignee before anyone trusts that field.

### Carried-forward defects
Live defects are beads, not entries in this file. Read the ledger.

### Open operator decisions
- **The captain rethink** — `plans/2026-08-24-captain-rethink/` is cutting the captain's
  instruction corpus and reworking what the captain is responsible for. Step 3 of that plan
  needs the operator.
