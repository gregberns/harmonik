# C2 — Durable provenance for the stale-`in_progress` reset — Change Spec

**Component:** Give the bead-reset sweep a provenance signal that survives `queue.json` removal AND intent-log drain, so genuinely-stuck `in_progress` beads (the incident's "observed 6, reset 0") actually get reset.
**Bead:** hk-9eury · **Goal:** G1 · **Analysis gap:** #2
**Spec home:** `specs/process-lifecycle.md` §4.5 PL-006 (sixth bullet provenance clause + new sub-clause **PL-006f**); `specs/process-lifecycle.md` §4.1 file-surface inventory (new ledger file).

---

## Requirements (carried forward from 03-components.md)

- **C2-R1** — The daemon MUST maintain a durable, project-scoped record of beads it has claimed-into-`in_progress` that persists across SIGKILL and outlives both the wave-queue's `queue.json` (removed on completion per QM-003) and the BI-030 claim-intent file (drained by BI-031 recovery).
- **C2-R2** — On boot, a bead in `br list --status in_progress` whose provenance is established by C2-R1's record (OR existing intent-log / queue-owned signals) and that satisfies NO reset-exclusion (a/b/c per PL-006) MUST be reset `in_progress → open` via `BeadResetter` with the `:reset:<daemon_start_ns>` idempotency key.
- **C2-R3** — A bead in `in_progress` whose provenance CANNOT be established as this project's MUST NOT be reset (conservative-skip invariant preserved; no blind cross-project reset).
- **C2-R4** — The `ProvenanceChecker` seam MUST remain the pluggable point for a future Beads audit-actor `project_hash` checker; the C2-R1 ledger is the MVH-grade provenance that does not depend on Beads exposing the audit actor.
- **C2-R5** — Reproduce-first: a test MUST reproduce the incident shape (bead `in_progress`, claim-intent drained, `queue.json` absent) and assert that WITH C2's ledger the bead is reset, and WITHOUT it the bead is skipped.
- **C2-R6** — The reset count MUST roll into `daemon_orphan_sweep_completed.bead_in_progress_reset` (existing field; no schema change).

## Research summary (from 04-research/C2)

The reset engine (`SweepStaleInProgressBeads`, `internal/lifecycle/orphansweepbeads.go`) is built and wired in production (`daemon.go:681-700`). Its ownership signal today is the intent-log (`IntentProvenanceSet` from `ScanIntentLog`, any-op-type after hk-sc3o4) OR a positive `ProvenanceChecker` — which is left **nil** in production because Beads v0.1.x records git `user.name` in the audit `actor` field, not `project_hash`. In the incident the daemon auto-pulled `br ready` into a WAVE queue; on completion `queue.json` was removed (QM-003) and BI-031 recovery drained the claim-intent files, leaving the 6 stuck beads with NO intent file and `ProvenanceChecker` nil → correct conservative skip. The fix is a durable per-project **run-ledger** (`.harmonik/run-ledger.jsonl`): the daemon appends `{bead_id, run_id, claimed_at, daemon_generation, project_hash}` at claim time, append-only and fsync'd, NOT removed on queue completion. The sweep reads it as an ADDITIONAL owner signal (augmenting, never overriding, the exclusion ordering). Critical correctness constraints from research: (a) the ledger entry MUST be pruned when its bead reaches a terminal state (closed/merged) or be daemon-generation-scoped, so a stale entry cannot authorize resetting a bead a DIFFERENT later run legitimately re-claimed; (b) the append MUST precede the `br` claim (worst case = a harmless extra owner signal with no claim) and fsync; (c) `daemon_generation` (couples to C6's last-boot marker) records which boot wrote the entry.

## Approach

Add a durable run-ledger as a new **owner signal** consumed by the existing reset path, and a normative sub-clause **PL-006f** governing it. Decisions recorded:

- **Decision 1 (ledger format):** append-only JSONL at `.harmonik/run-ledger.jsonl`, one row `{bead_id, run_id, claimed_at, daemon_generation, project_hash}` per claim. Rationale: append is crash-safe; JSONL matches the intent-log precedent (`.harmonik/beads-intents/`).
- **Decision 2 (lifecycle / stale-authorization, research R1):** **prune-on-terminal-state.** When a bead reaches a terminal Beads state (closed) OR a merge commit bearing its ID is observed (Cat-3c), its ledger rows are dropped. ADDITIONALLY each row carries `daemon_generation`; the sweep MUST NOT trust a row whose `project_hash` differs from the current daemon's, and treats a row from any generation as a valid owner signal ONLY for ownership (it never overrides exclusions). The combination (prune-on-close + generation tag) closes the "months-old entry authorizes resetting a re-used bead id" hazard: a re-claimed bead gets a FRESH row, and the exclusion ordering (live-run / pending-intent / merged-commit) still gates the actual reset.
- **Decision 3 (write ordering, research R2):** the ledger append happens BEFORE the `br` claim write at the workloop `Register` site (`runregistry.go:94`), with `fsync`. A crash between append and claim leaves a ledger row whose bead is NOT actually `in_progress` → it simply will not appear in `br list --status in_progress` and is harmless; the next prune drops it.
- **Decision 4 (seam):** the ledger is exposed to the sweep as a new owner-set input (a `LedgerProvenanceSet` analogous to `IntentProvenanceSet`), NOT as a new `ProvenanceChecker`. `ProvenanceChecker` stays nil-in-production and reserved for the future Beads audit-actor checker (C2-R4).

The exclusion ordering is UNCHANGED. The ledger only widens the ownership gate at the top of `SweepStaleInProgressBeads`; a bead with no ledger row, no intent, not queue-owned, `ProvenanceChecker` nil → still skipped (C2-R3). The reset write surface (idempotency key, BI-030 intent-log) is unchanged (C2-R6: count rolls into the existing `bead_in_progress_reset`).

### Spec text to add — PL-006f (process-lifecycle.md §4.5, after PL-006e)

> **PL-006f — Durable run-ledger for stale-`in_progress` provenance**
>
> The provenance signal of the §PL-006 stale-`in_progress` bullet (intent-log claim cross-reference OR the nil-in-production `ProvenanceChecker`) is insufficient after a wave-queue completes: `queue.json` is removed per [queue-model.md QM-003] and the BI-031 recovery drains claim-intent files, leaving a genuinely-stuck `in_progress` bead with NO surviving owner signal (the incident of 2026-05-30 observed 6 such beads and reset 0). To establish provenance that survives both drains, the daemon MUST maintain a durable, project-scoped run-ledger.
>
> **Ledger surface.** The ledger is an append-only JSONL file at `.harmonik/run-ledger.jsonl` (within the §PL-004 file surface). Each row records `{bead_id, run_id, claimed_at, daemon_generation, project_hash}`. The daemon MUST append a row at the moment it claims a bead into `in_progress` (the workloop run-registration site), and the append MUST occur BEFORE the `br` claim write and MUST be `fsync`'d. A crash between the ledger append and the `br` claim leaves a row whose bead is not actually `in_progress`; such a row is harmless (the bead does not appear in `br list --status in_progress`) and is pruned per the lifecycle rule below.
>
> **Ledger consumption (ownership).** During the §PL-006 stale-`in_progress` sweep, a bead is "owned by this project" if it has a ledger row whose `project_hash` equals the current daemon's `project_hash` (per PL-006a), OR it satisfies the pre-existing owner signals (an intent file of any op-type per [beads-integration.md BI-030], queue-owned, or a positive `ProvenanceChecker` verdict). The ledger is an ADDITIONAL owner signal; it MUST NOT override any of the reset-exclusion conditions (a) live-run-reattached, (b) pending close/reopen intent, (c) merged commit — these still gate whether an owned bead is actually reset. A bead with no ledger row, no intent, not queue-owned, and `ProvenanceChecker` nil/false MUST NOT be reset (the conservative-skip invariant; no blind cross-project reset).
>
> **Ledger lifecycle (stale-authorization guard).** The daemon MUST prune a bead's ledger rows when the bead reaches a terminal state — observed either as a `close` write completing for that bead, or a merge commit bearing `Harmonik-Bead-ID: <bead_id>` (the Cat-3c signal of PL-006c). Each row's `daemon_generation` (the boot-generation counter defined by §PL-005c's `boots.jsonl` / C6) records which daemon boot wrote it. The sweep MUST treat a row only as an OWNERSHIP signal; a re-claimed bead receives a fresh row, so a stale row from a prior generation cannot authorize resetting a bead that a later run legitimately re-claimed (the exclusion ordering remains the gate). The ledger MUST be bounded: rows for terminal beads are pruned on observation; the daemon MAY additionally prune rows older than a configurable window on each boot.
>
> **`ProvenanceChecker` reservation.** The `ProvenanceChecker` seam (PL-006b companion) remains the pluggable point for a future Beads audit-actor `project_hash` checker once Beads writes `project_hash` into the audit `actor` field. The run-ledger is the MVH-grade provenance that does not depend on that Beads capability; `ProvenanceChecker` stays nil in production.
>
> **Reset count.** A bead reset via this provenance path rolls into the existing `bead_in_progress_reset` field of `daemon_orphan_sweep_completed` (per [event-model.md §8.7.14]); no new payload field and no schema change is introduced by this clause.
>
> Tags: mechanism
> Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### Spec text to amend — PL-004 / PL-006

- **PL-004 file-surface inventory** — add `.harmonik/run-ledger.jsonl` (run-ledger, daemon-written append-only at claim time, PL-read at the §PL-006 sweep, pruned on terminal-state per PL-006f).
- **PL-006 sixth bullet** — add a parenthetical after the "OR by cross-referencing `claim` op entries in the daemon's own intent-log" clause: "OR by a matching row in the durable run-ledger per §PL-006f (the provenance signal that survives `queue.json` removal and intent-log drain)."

## Files & changes

| File | Change | Why |
|---|---|---|
| `specs/process-lifecycle.md` | Add PL-006f (above) after PL-006e; amend PL-006 sixth bullet + PL-004 inventory; changelog row. | Normative ledger contract. |
| `internal/daemon/runregistry.go` (~line 94 `Register`) | Append `{bead_id, run_id, claimed_at, daemon_generation, project_hash}` to `.harmonik/run-ledger.jsonl` BEFORE the claim, fsync'd. | Claim-time durable record. |
| `internal/lifecycle/orphansweepbeads.go` | Add a `LedgerProvenanceSet` owner-set input read from the ledger; widen the ownership gate (~line 90-130) to include it; keep exclusion ordering unchanged. | Consume the ledger as an owner signal. |
| `internal/lifecycle/runledger.go` (new) | Ledger reader/writer + prune-on-terminal-state + generation tagging, behind a seam (`RunLedger` interface) for test injection. | Ledger mechanics. |
| `internal/daemon/daemon.go` | Wire the `RunLedger` reader into `RunOrphanSweep` as an additional owner-set; prune terminal rows after the Cat-3c close path. | Production wiring. |
| (close/Cat-3c path) | On `close` / Cat-3c merge-commit observation, prune the bead's ledger rows. | Stale-authorization guard. |

## Acceptance criteria

- **AC1 (C2-R5 reproduce-first, the locking test):** Set up a bead `in_progress`, NO claim-intent file, NO `queue.json`, `ProvenanceChecker` nil, but a matching ledger row present → the sweep RESETS the bead (`in_progress → open`) and `bead_in_progress_reset == 1`. With the ledger row ABSENT (pre-fix shape) → the bead is SKIPPED (`bead_in_progress_reset == 0`). Both branches in one table-driven test.
- **AC2 (C2-R3 conservative skip):** A bead `in_progress` with a ledger row whose `project_hash` differs from the current daemon's, no intent, not queue-owned → NOT reset.
- **AC3 (exclusion ordering unchanged):** An owned bead (ledger row present) that ALSO has (a) a live re-attached run, (b) a pending close/reopen intent, or (c) a merge commit → NOT reset (excluded by a/b/c respectively); for (c) it is CLOSED via `BeadCat3cCloser`, not reset.
- **AC4 (write ordering, C2-R1):** A crash injected between the ledger append and the `br` claim leaves a ledger row but no `in_progress` bead; the next sweep does not reset anything spurious and the row is pruned.
- **AC5 (lifecycle / stale-authorization):** After a bead closes, its ledger rows are pruned; a later DIFFERENT bead reusing semantics is not falsely owned by a stale row.
- **AC6 (C2-R6):** No new payload field; `bead_in_progress_reset` carries the count; event schema unchanged.

## Verification

```bash
go test ./internal/lifecycle/... -run 'InProgress|OrphanSweepBeads|RunLedger|Provenance' -count=1
go test ./internal/daemon/...    -run 'RunLedger|OrphanSweep|Register'                    -count=1
```

Manual: reproduce the incident shape via `internal/testhelpers/crashharness.go` (SIGKILL after claim, drain intents, remove queue.json) with a ledger row present; boot; assert the bead returns to `open` and `bead_in_progress_reset == N`.

## Error handling & edge cases

- **Ledger append failure** (disk full) at claim time — the claim MUST NOT proceed without the durable record OR the daemon logs + falls back to the prior (intent-only) provenance; recommend: a failed fsync'd append logs a warning and the claim proceeds (the worst case is the prior behavior — a possibly-unprovenanced stuck bead — never a wrong reset).
- **Corrupt ledger row** — a malformed JSONL line is skipped with a structured-log warning (per ON-035), not fatal.
- **Generation mismatch** — a row from a prior `daemon_generation` is a valid ownership signal but the exclusion ordering still gates the reset; no over-trust.
- **Unbounded growth** — prune-on-terminal-state + optional window-prune-on-boot bounds the file.

## Migration / backwards compatibility

Additive: a fresh daemon with no ledger file behaves as today (intent-only provenance). The ledger file is created on first claim. No event schema change. N-1 daemons reading a project with a ledger ignore it (they use intent-only provenance) — no conflict.

## Test beads

- **Scenario:** the AC1 reproduce-first test IS the scenario test. CLI under test: `harmonik daemon` (boot orphan-sweep). Lifecycle state: bead `in_progress` with claim-intent drained + `queue.json` absent + ledger row present. Observable terminal condition: bead status `open` in Beads + `daemon_orphan_sweep_completed.bead_in_progress_reset == N` in `.harmonik/events/events.jsonl`.
- See the shared test-bead block in `C6-boot-backoff-spec.md` §Test beads for the filed bead IDs.
