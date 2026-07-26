# Research — C1 Daemon dispatch loop

> **Provenance.** Code anchors are from the epic bead tree `hk-04q2j.2` (a real code survey by the
> operator/captain during epic decomposition). The planning agent independently **verified** the two
> load-bearing anchors below by read-only inspection of `internal/daemon/workloop.go` on branch
> `phase1-session-restart-substrate` (2026-07-21). No fresh full-file spelunking beyond those
> verifications; unverified anchors are marked (bead-sourced).

## Verified anchors (read directly, 2026-07-21)

- **The `noAutoPull` gate + fallback structure exists as described.** At `workloop.go` ~2723 the
  `if deps.noAutoPull { workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); continue }`
  branch is present, immediately followed by the operator-pause gate for "the br-ready path
  (hk-ry8q1)", then `// No queue active — fall back to br-ready poll.` and
  `readyRecords, err := deps.brAdapter.Ready(ctx)`. This confirms the deletion target: the
  `noAutoPull==true` behavior (sleep-on-submitWakeC + continue) is exactly what must become the
  UNCONDITIONAL behavior of the `queue IS None` branch. The design is a "promote the noAutoPull
  branch to the only branch, delete everything after it" edit — mechanically clean.
- **The `noAutoPull` field exists** at `workloop.go` ~712-717 with godoc: "disables the br-ready
  fallback poll path so the work loop only dispatches items that arrive via the queue surface.
  Sourced from Config.NoAutoPull … Bead ref: hk-exd7m." Confirms the field + Config linkage and the
  godoc that C1 must fix.

## Bead-sourced anchors (from hk-04q2j.2, not independently re-verified)

- Fallback dispatch block spans ~2725-2833: `br ready` poll + pick-first + dispatch +
  `readyPathAttempts` map + operator-pause/handler-pause `br ready` copies.
- `noAutoPull` assignment at ~1183; godoc to fix at ~79-84.
- PRESERVE: queue-pull path ~2119-2723; sentinel-governor `Ready()` reads at `workloop.go:1919, 1970`
  (observe-only — readiness as a movement/opportunity signal, no dispatch).

## Key finding — the deletion is a *collapse*, not a rewrite

Because the `noAutoPull==true` arm already implements the desired end-state (idle + sleep on
`submitWakeC`), C1 is: (a) make that arm unconditional, (b) delete the fall-through `br ready`
machinery below it, (c) delete the field + assignment + godoc. Low risk in the queue-pull path,
which is upstream of the `queueItemIndex < 0` fork and untouched.

## Risk — the two Ready() consumers must be disambiguated

`deps.brAdapter.Ready` is called both as the (deleted) dispatch source AND as the (preserved)
sentinel observe-only reads at 1919/1970. A naive "delete all Ready() callers" would break the
governor. The deletion MUST be surgical to the `queueItemIndex < 0` dispatch fork only.
