# 05 — Changelog: no-auto-dispatch spec impact

> Proposed revision-log rows to land in each amended spec on `kerf finalize`. PENDING operator D1.

## specs/execution-model.md — proposed §12 revision row

| Date | Version | Author | Change |
|------|---------|--------|--------|
| 2026-07-2x | 0.9.x | agent (no-auto-dispatch / hk-04q2j) | **Remove the `br ready` boot-time auto-pull fallback ENTIRELY (not default-off).** RETIRE EM-066 + EM-067 (§4.11) [branch A] — queue-only is no longer a default-with-opt-in but the daemon's ONLY dispatch topology; a bare boot dispatches zero runs; no `--auto-pull`. §7.4 pseudocode `queue IS None` fork collapsed to a single idle-wait arm. §10.1 Core MVH: `br ready` fallback opt-in language removed; EM-066/EM-067 dropped from the required set. §10.2: historical-topology `--auto-pull` fallback-dispatch test + auto-pull sealing test deleted; the quiet-daemon zero-runs test promoted to the sole boot obligation. Source: operator directive 2026-07-21 (DECISIONS.md); epic hk-04q2j. Terminal step of the auto-pull walk-down (v0.8.1 added the flag, v0.8.2 flipped default-off, this removes it). |

## specs/queue-model.md — proposed §12 revision row (cross-spec ripple)

| Date | Version | Author | Change |
|------|---------|--------|--------|
| 2026-07-2x | x.y.z | agent (no-auto-dispatch / hk-04q2j) | §8.5 QM-054 informative note: dropped the retired execution-model `br ready` fallback gate (EM-067) from the list of `operator_pause_status` co-consumers; the `active → paused-by-drain` queue transition is unchanged. |

## Requirement-ID ledger

- **RETIRED:** EM-066, EM-067 (branch A). Not reused.
- **REPURPOSED:** EM-066 body rewritten, EM-067 retired (branch B alternative).
- **ADDED:** none.
- **RENUMBERED:** none.
