# C2 — Durable provenance for the stale-`in_progress` reset — Research findings

Component: give the bead-reset sweep a provenance signal that survives queue.json removal AND
intent-log drain, so genuinely-stuck `in_progress` beads (incident's "observed 6, reset 0") get reset.
Bead hk-9eury, analysis gap #2, goal G1. Anchors verified at `2e49a8df`.

## RQ1 — What is the EXISTING reset path and why did it reset 0 in the incident?

**Finding:** The reset path is built and WIRED in production, but its provenance signal is the local
intent-log, which the incident had drained.

- `internal/lifecycle/orphansweepbeads.go` — `SweepStaleInProgressBeads` is the reset engine. Wired in `daemon.go:681-700`: when `cfg.BrPath != ""`, `beadResetter = brAdapter` and `beadCat3cCloser = brAdapter`; passed to `RunOrphanSweep` (`daemon.go:743-751`). Result counts roll into `BeadInProgressReset` / `BeadCat3cClosed` (`orphansweep.go:304-305`).
- **Provenance signal today** (`orphansweepbeads.go:90-130`): a bead is "owned by this project" iff it has ANY intent file (any op type) in `.harmonik/beads-intents/*.json` (the `IntentProvenanceSet` from `ScanIntentLog`, `orphansweepbeads.go:294-317`) OR a positive `ProvenanceChecker` verdict. `ProvenanceChecker` is left **nil** in production (MVH: Beads v0.1.x audit `actor` field records git `user.name`, not project_hash — see the doc comment at `orphansweepbeads.go:100-115`). hk-sc3o4 already broadened the signal from `claim`-only to any-op-type.
- **Why 0 reset in the incident:** the daemon auto-pulled `br ready` into a WAVE queue; on completion `queue.json` is removed (QM-003) and BI-031 recovery drains claim-intent files. A bead whose claim-intent was cleared and whose queue.json is gone has NO intent file of any type and `ProvenanceChecker` is nil → conservative skip (`orphansweepbeads.go:103` "NOT owned... MUST NOT be touched"). The skip is correct-by-design (no blind cross-project reset) but leaves real stuck beads stranded. This is the **reachability gap**.

## RQ2 — What durable signal can establish provenance after queue+intent drain (C2-R1)?

**Finding:** No durable per-project run-ledger exists today (`grep RunLedger` → only a test name). Three candidate signals:

- **Option A — durable run-ledger file** `.harmonik/run-ledger.jsonl` (or `.harmonik/runs/<run_id>.json`): the daemon appends `{bead_id, run_id, claimed_at, project_hash}` at claim time (workloop `Register` site, `runregistry.go:94`), append-only, fsync'd, NOT removed on queue completion. Survives SIGKILL, queue.json removal, intent drain. The sweep reads it as an additional `IntentProvenanceSet`-equivalent owner set. **Pro:** purpose-built, exactly the missing durable handle; cheap append. **Con:** new on-disk artifact + lifecycle (needs its own bounded growth / compaction — a stale 6-month-old entry must not authorize a reset of an unrelated bead; window-bound or prune on successful close).
- **Option B — `ProvenanceChecker` via Beads audit-actor** once Beads writes `project_hash` into the audit `actor` field. **Pro:** zero new harmonik artifact; the seam (`orphansweepbeads.go:185-204`) already exists for it. **Con:** depends on a Beads release harmonik does not control (explicitly the post-MVH future per the doc comment) — cannot be the MVH fix.
- **Option C — git-trailer / merge-commit scan as negative-only** (`MergeCommitScanner`, already wired): tells you a bead WAS completed (Cat-3c close), not that an incomplete one is ours. Insufficient as a positive owner signal.

**Recommendation:** Option A (durable run-ledger) is the MVH-grade fix per C2-R1/C2-R4; keep `ProvenanceChecker` (Option B) as the future seam. The decomposition already names this ledger as the shared provenance source C1 and C3 also read.

## RQ3 — Reset exclusion ordering: does adding a ledger break the conservative invariant (C2-R3)?

**Finding:** No, if the ledger is added as an OWNER signal, not a skip-override. Exclusions apply in order (`orphansweepbeads.go` + analysis §A): (a) live run reattached / claim-intent / queue-dispatched; (b) close/reopen intent (Cat 3a); (c) merged commit (Cat 3c → close). Only if NONE apply AND ownership is established does `ResetBead` fire. The ledger augments the ownership test (the `IntentProvenanceSet`-style gate at the top), it does NOT remove any exclusion. A bead with no ledger entry, no intent, not queue-owned, `ProvenanceChecker` nil → still skipped (C2-R3 preserved). **Key correctness note:** the ledger entry must be PRUNED when a bead reaches a terminal state (closed/merged) so a stale entry cannot authorize resetting a bead that a DIFFERENT later run legitimately re-claimed — window-bound the ledger to the current daemon-generation or prune-on-close.

## RQ4 — Idempotency of the reset write (unchanged surface)

**Finding:** Unchanged. The reset write carries idempotency key `<project_hash>:<bead_id>:reset:<daemon_start_ns>` per BI-010d (`orphansweepbeads.go` doc comment) and is intent-logged per BI-030. Two restarts produce distinct keys. C2's ledger does not touch this; the reset count still rolls into the existing `BeadInProgressReset` field (C2-R6, no schema change).

## RQ5 — Reproduce-first (C2-R5): what test shape locks the fix?

**Finding:** Test infrastructure exists. `orphansweepbeads_test.go`, `bi010d_sweep_idempotency_test.go`, `provenance_pl006a_test.go` use fake adapters injected via the seam interfaces (`BeadResetter`, `ProvenanceChecker`, `InFlightBeadLedger`) with table-driven exclusion cases. C2-R5 adds a case: bead `in_progress`, claim-intent absent, `queue.json` absent, `ProvenanceChecker` nil — assert WITH the ledger entry present the bead IS reset, and WITHOUT it (pre-fix) it is skipped. `internal/testhelpers/crashharness.go` already models the SIGKILL/intent-drain crash shape.

## Risks / unknowns
- **R1 (ledger growth / stale-authorization):** the run-ledger must be bounded (prune-on-close or daemon-generation-scoped) or a months-old entry could authorize resetting a re-used bead id — flagged as a change-spec decision.
- **R2 (claim-time write atomicity):** the ledger append must happen at the same logical moment as the in_progress claim so a crash between claim and ledger-write does not produce an unprovenanced stuck bead. Recommend: append BEFORE the br claim (so worst case is a ledger entry with no claim → harmless extra owner signal), and fsync.
- **R3 (multi-daemon-generation):** `daemon_start_ns` already disambiguates reset keys; the ledger should record the generation so a current sweep does not over-trust a prior generation's entry — couples cleanly to C6's last-boot marker.

## No-blocker assertion
No blocker prevents a C2 change spec. One decision required (run-ledger lifecycle: prune-on-close vs generation-scoped) — recommend prune-on-terminal-state + generation tag. C2-R1's ledger is the prerequisite the dependency graph says C1/C3 consume; build it first.
