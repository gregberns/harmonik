# Research — C3 Test substrate (shim + migration)

> **Provenance.** Bead-sourced from `hk-04q2j.1` (shim) and `hk-04q2j.4` (migration). Test-file names
> are as enumerated in those beads; not re-verified against the tree by the planning agent.
> **NOTE:** crew `juliet` is actively IMPLEMENTING Step 1 (`hk-04q2j.1`) right now — the planning
> agent deliberately did not touch `internal/daemon` test files.

## The legacy-test pattern (why a shim is needed first)

Many `workloop`/scenario tests never submit a queue; they stub a ledger whose `Ready()` returns >=1
bead and rely on the `br ready` fallback to drive a dispatch. Once C1 deletes the fallback, those
tests would dispatch nothing and fail. **Discriminator for a legacy test:** stub ledger `Ready()`
returns >=1 bead AND the deps params carry NO `QueueStore`. Enumerated examples: `workloop_test.go`,
`workloop_allunclaimable_hktmhak_test.go`, `workloop_claim_*_test.go`,
`workloop_showbead_retry_hkfvpz5_test.go`, `daemon_test.go`.

## Step 1 shim (hk-04q2j.1, lands FIRST) — `internal/daemon/export_test.go`

The test export helper synthesizes a single-item queue from the `BrAdapter` when no `QueueStore` is
present. This keeps every legacy `Ready()`-driven test green THROUGH the deletion, by routing the
bead the test expected via a synthetic queue instead of the fallback. Landing this before C1 is what
keeps the suite from ever going red.

> **Live status (2026-07-21):** `hk-04q2j.1` has a `dot` note recording an implement-node failure
> ("exited without advancing HEAD past 5160326b…"). juliet has it dispatched on `juliet-q`. The
> shim is in progress, not yet landed — this is the gating item for the whole epic.

## Step 4 migration (hk-04q2j.4, AFTER C1) — feature tests that PIN the removed fallback

- DELETE `noautopull_em066_em067_test.go` fires-cases — keep only the zero-runs invariant, drop the
  now-meaningless auto-pull param.
- DELETE `workloop_bounded_retry_hk6pspu_test.go`, `brready_priority_scenario_hktul2a_test.go`.
- REWRITE `boot_redispatch_gate_bk33_test.go` to drive `spawnSubstrateReadyCh` via a *submitted
  queue* — the boot-redispatch gate still exists on the queue path; only its driver changes.
- DELETE the `br ready`-path cases in `workloop_operatorpause_ry8q1_test.go` and
  `workloop_handlerpause_kac8g_test.go`; KEEP the queue-path pause cases (those still assert real
  behavior).

## Finding — two-phase migration is mandatory

Step 1 (shim, keep-green) and Step 4 (delete pinned tests, post-deletion) bracket the C1 deletion.
The shim lets the bulk of the suite survive untouched; Step 4 only removes tests whose entire reason
for existing was the fallback. This is why the bead `blocks` order is 1 -> 2 -> 4.
