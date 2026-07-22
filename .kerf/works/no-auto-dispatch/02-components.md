# 02 — Component decomposition: no-auto-dispatch

> Back-filled from the `hk-04q2j.1`–`.5` bead breakdown + `DECISIONS.md`. This is a **deletion**
> work, so "components" are the surfaces the deletion touches, mapped 1:1 to the operator's five
> sequenced steps. The bead tree IS the authoritative decomposition; this records it and adds the
> spec-first surface (C4) that the beads imply but do not name.

## Dependency shape (from the bead `blocks` edges)

```
C3-shim (Step 1) ──blocks──> C1-delete (Step 2) ──blocks──> C2-plumbing (Step 3) ──blocks──> C5-cleanup (Step 5)
                                     └──blocks──> C3-tests (Step 4)
C4-spec (spec-first) rides with C1/C2 (amend as the code lands)
```

Land order: **C3-shim → C1-delete → {C2-plumbing, C3-tests} → C5-cleanup**, with **C4-spec**
amended in lockstep with C1/C2.

## C1 — Daemon dispatch-loop deletion  [bead hk-04q2j.2, PRIMARY, P1]

The load-bearing change. In `internal/daemon/workloop.go`:
- Delete the body of the `if queueItemIndex < 0` branch: the `br ready` poll
  (`deps.brAdapter.Ready`), pick-first, dispatch, the `noAutoPull` gate (~2727–2737), the
  operator-pause and handler-pause `br ready` copies, and the `readyPathAttempts` map.
- Replace `queueItemIndex < 0` with **idle + continue** — sleep on `submitWakeC` exactly as the old
  `noAutoPull` branch already did (that branch becomes the ONLY behavior).
- Delete the `noAutoPull` field (struct ~712–717) and its assignment (~1183).
- Fix the godoc at ~79–84 that described the fallback.
- **PRESERVE:** the queue-pull path (~2119–2723) and the sentinel-governor `Ready()` reads at
  `workloop.go:1919, 1970` (observe-only, no dispatch).

## C2 — Config / flag / status plumbing removal  [bead hk-04q2j.3, P1]

Tear out the `NoAutoPull` surface that fed C1's deleted gate:
- `cmd/harmonik/main.go`: `autoPullFlag` (~996–998) + `NoAutoPull` assignment (~1353).
- `cmd/harmonik/usage.go`: flag help (~53–54, 59).
- `internal/lifecycle` supervise/shim.go (~261, 269): drop the `--no-auto-pull` arg.
- daemon `Config.NoAutoPull` (`daemon.go` ~379–395); `bootstate.go:374`.
- `internal/core/daemonevents_hqwn59.go`: status-payload `NoAutoPull` (~1156, 1184–1186).
- `scenario/orchdrive.go` (~216–219) + `daemon/scenariotest/concurrent_merge.go:246` setters.
- `brcli/ready.go:182` comment.
- **Open D2:** keep `--auto-pull`/`--no-auto-pull` as accepted-but-ignored no-ops vs delete.

## C3 — Test substrate: shim + migration  [beads hk-04q2j.1 (P2, FIRST) and hk-04q2j.4 (P2)]

Two parts around C1:
- **Shim (Step 1, lands FIRST):** `internal/daemon/export_test.go` — the test export helper
  synthesizes a single-item queue from `BrAdapter` when no `QueueStore` is present, keeping legacy
  `Ready()`-driven tests green *before* the deletion. Legacy tests are those whose stub ledger
  `Ready()` returns ≥1 bead AND that pass **no** QueueStore in the deps params (e.g.
  `workloop_test.go`, `workloop_allunclaimable_hktmhak_test.go`, `workloop_claim_*_test.go`,
  `workloop_showbead_retry_hkfvpz5_test.go`, `daemon_test.go`).
- **Migration (Step 4, after C1):** delete/rewrite feature tests that *pin the removed fallback* —
  delete `noautopull_em066_em067_test.go` fires-cases (keep the zero-runs invariant, drop the
  param), delete `workloop_bounded_retry_hk6pspu_test.go`, `brready_priority_scenario_hktul2a_test.go`;
  rewrite `boot_redispatch_gate_bk33_test.go` to drive `spawnSubstrateReadyCh` via a submitted queue
  (the gate survives); delete the `br ready`-path cases in `workloop_operatorpause_ry8q1_test.go` +
  `workloop_handlerpause_kac8g_test.go`, keep the queue-path cases.

## C4 — Spec surface (spec-first obligation, implied not bead-named)

Harmonik is spec-first: the spec is normative and code matches it. Today
`specs/execution-model.md` still *sanctions* the fallback:
- **§4.11 EM-066** — "queue-only is the default … `--auto-pull` is the opt-in to the historical
  `br ready` fallback."
- **§4.11 EM-067** — binds the fallback path's operator-pause behavior to the single pause-truth.
- **§7.4** run-main-loop pseudocode — the `queue IS None` two-way branch with the `br ready`
  fallback arm (~lines 1483–1488).
- **§10.1 Core MVH** — makes the `br ready` fallback "a conforming opt-in" under `--auto-pull`.
- **§10.2** — EM-066/EM-067 test obligations including the historical-topology fallback test.

Removing the code without amending these leaves a spec-vs-code contradiction (the very kind
EM-066/EM-067 were originally added to *resolve*). **Open D1** governs whether these clauses are
retired outright or kept as "removed at vX" historical records.

## C5 — Vestigial cleanup  [bead hk-04q2j.5, P3, OPTIONAL]

- `internal/daemon/restartbackoff.go` — existed only because each boot auto-pulled `br ready`;
  with auto-pull gone its rationale evaporates. Harmless (throttles nothing meaningful now).
  Deletion is optional cleanup, operator's call — flag as now-vestigial.
- The `--auto-pull`/`--no-auto-pull` flags (see C2 / Open D2).
