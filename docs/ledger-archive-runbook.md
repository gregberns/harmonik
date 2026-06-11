# Ledger-archive runbook (operator-grade) — shrink the 2083 closed beads

**Status:** READY TO EXECUTE — deferred to operator (requires full flywheel quiesce + ledger rewrite).
**Authored:** 2026-06-10 by captain, from read-only research agent a959012e (br v0.2.10).
**Why:** the beads ledger is 2137 issues / **2083 closed (97% dead weight)**. Every `br` write
snapshots the ~3.5 MB ledger and intermittently blows br's 10s WriteTimeout under IO contention →
`BrUnavailable persisted after 10 retries` → beads stranded in_progress/failed even though their
code landed on main. This is the recurring throughput bottleneck (hk-hdbls / hk-hypbi / hk-g8hv2).
**Captain's per-session mitigation (NOT a fix):** trim `.beads/.br_history` to 5 newest each
heartbeat — restores br close to sub-second within a session, but the 3.5 MB *ledger* snapshot is
the base cost, so close-failures still recur ~50% under concurrency. This runbook is the durable fix.

## Why this is operator-grade (not captain-autonomous)
- `br` has **NO** archive/compact/gc/vacuum/prune verb (v0.2.10). The only purge primitive is
  `br delete --hard`, which **removes the line entirely** → afterward `br show <closed-id>` 404s
  (exit 3), breaking anything that queries closed beads (kerf triage, `git log --grep "Refs:"`
  cross-checks, changelogs). So we do NOT hard-delete — we **split + rebuild** instead.
- The operation requires **stopping the daemon AND gurney** (full quiesce) — it halts all flywheel
  throughput — and rewrites the git-tracked `.beads/issues.jsonl` that the whole multi-agent system
  depends on. Corruption blast radius = the entire project's task state.

## Procedure (operator/captain, daemon + crews DOWN) — CORRECTED 2026-06-10
> ⚠️ Two corrections from live pre-validation (captain): (a) the JSONL is STALE relative to the DB
> (DB had 2083 closed, JSONL only 2065 — ~18 closes flushed to DB but not exported). You MUST
> `br sync --flush-only` FIRST or the rebuild reverts those closes. (b) `br sync --rebuild` is
> itself import-only-authoritative — do NOT also run `--import-only`.

0. **Quiesce:** confirm 0 runs in flight (`harmonik queue status` → no `dispatched` items; pause the
   queue first to stop new dispatch), then `pkill -f "harmonik --project"` and stop gurney's session
   (`tmux kill-session -t hk-crew-gurney`). Confirm no worktree is mid-run under `.harmonik/worktrees/`.
1. **Backup (rollback anchor):** `cp -r .beads .beads.bak.$(date +%s)`.
2. **Strand-sweep** (now contention-free): `br list --status=in_progress --json` → for each whose
   `Refs:` landed on main (`git log --grep`), `br close <id> --reason "landed <sha>; daemon close
   failed on br BrUnavailable"`. This makes them `closed` so they get archived out below.
3. **Flush DB → JSONL** so the JSONL is authoritative (captures the strand-sweep + the ~18 DB-only
   closes): `br sync --flush-only`. If the Stale-DB Guard trips, investigate before `--force`.
4. **Split closed out of the active JSONL** (run from `.beads/`):
   ```
   grep '"status":"closed"' issues.jsonl > issues.closed.jsonl     # archive (recoverable)
   grep -v '"status":"closed"' issues.jsonl > issues.active.jsonl
   # VERIFY: wc -l of both must sum to original; spot-check issues.active.jsonl keeps open/tombstone/deferred
   mv issues.active.jsonl issues.jsonl
   ```
5. **Rebuild the DB from the trimmed JSONL** (authoritative; drops absent rows, keeps tombstones):
   `br sync --rebuild`.
6. **Verify:** `br stats` total ≈ 70 (was 2137) · `br doctor health` = healthy · a timed `br close`
   (or `br show`) is sub-second · `br show <a-closed-id>` now 404s (expected — it's in the archive
   file). Then `git add .beads/issues.jsonl .beads/issues.closed.jsonl docs/ledger-archive-runbook.md
   && git commit` the new state.
7. **Restart:** daemon via supervisor `/tmp/hk-daemon-supervise.sh` (VERIFY it carries
   `--workflow-mode review-loop`); `harmonik queue resume --queue gurney-q`; restart + re-seed the
   gurney crew. Confirm the 3 pending beads (hk-9321v/hk-xizhl/hk-79x3v) resume and a test bead
   closes cleanly.

## Rollback
Restore the backup and rebuild:
```
rm -rf .beads && mv .beads.bak.<ts> .beads
br sync --rebuild
```
then restart the daemon.

## Single biggest risk
Mutating the ledger while the daemon or gurney hold the workspace write-lock — corrupts the DB or
strands in-flight beads. **Hard-quiesce is mandatory.** Do NOT run any step with a run in flight.

## Storage layout (for reference)
`.beads/beads.db` 20.5 MB (active SQLite, gitignored) · `.beads/issues.jsonl` 3.55 MB (git-tracked;
closed beads = 95% / 3.38 MB) · `.beads/.br_history/` rolling backups (captain trims to 5/heartbeat)
· `.beads/.br_recovery/` 33 MB DB snapshot · `issues.db` 10.8 MB (stale, gitignored).
JSONL + git history preserve every closed bead regardless of method.
