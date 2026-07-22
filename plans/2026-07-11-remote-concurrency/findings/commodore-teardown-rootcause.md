# Commodore teardown — root-cause verdict

Date: 2026-07-12
Scope: READ-ONLY research. Why the `commodore` planning session is torn down ~every 7 min for ~a day.

## TL;DR verdict

**(c) — Not fixed by the b25e9919 redeploy.** The four fixes in that redeploy (hk-62r8w, hk-p006e,
hk-1q7bt, hk-fpjxi) address token-waste, keeper arming, log floods, and branch reaping — **none of
them address the two beads that actually kill commodore**, both of which are still **OPEN**:

- **PRIMARY root cause — `hk-zeo5y` (OPEN, P1):** the daemon's `RunOrphanSweep` runs at every daemon
  boot and reaps any project-prefixed tmux session that is not on one of three hard-coded exemptions
  (captain sentinel / crew-registry record + `crew-<name>` session / flywheel sentinel). There is **no
  name allowlist for oversight roles** — commodore/admiral/watch have zero daemon references. Every
  supervisor-revive or daemon redeploy therefore reaps commodore. During a day of fleet-hardening the
  daemon was redeployed repeatedly (that IS the ~7-min cadence), so commodore was reaped over and over.
- **SECONDARY / aggravating — `hk-6629b` (OPEN, P1):** orphaned `comms recv/subscribe --follow` child
  procs survive their session's `/clear`, reparent to init (ppid=1), and hold daemon subscribe slots
  forever. Over successive /clear cycles they exhaust the **32-slot** subscriber cap, so new watchers
  (commodore's, and even the captain's own) fail to arm with `subscribe_capacity_exceeded`.

The known prior diagnosis (B13 comms-flap = the killer) is **only partially right**: that bug caused a
presence-event *storm* and token waste, not the process death. It is now fixed, but the death mechanism
was the orphan-sweep, which the redeploy never touched.

## Evidence

### 1. Does d1fbf715 fully close the exit-1-on-socket-reject path?

`git show d1fbf715` — the fix extends three client decode loops (`runCommsRecvFollowIO`,
`runCommsRecvWait`, `runSubscribeFollowIO`) with `Ok *bool` + `Error string` and, on `Ok != nil &&
!*Ok`, **exits code 1 instead of reconnecting** (cmd/harmonik/comms.go recv-follow path; cmd/harmonik/subscribe.go).

- This is generic: it catches **any** `SocketResponse` error, including `subscribe_capacity_exceeded` —
  the commit message names it explicitly. So it is not scoped to `ErrUnexpectedEOF`.
- BUT the fix's behavior is to **exit(1)**, not to reconnect-with-backoff. It stops the ~1s flap (token
  waste, the presence storm in hk-62r8w's iter-22 note: 3,723 presence events / 20% of the window). It
  does **not** keep the watcher alive on capacity rejection — the client still dies. If a `recv --follow`
  is ever the tmux foreground process, that exit(1) tears the session down cleanly with no teardown
  event. Per commodore's mission (below) the watch is armed inside a Monitor, so for commodore it mainly
  makes it **deaf**, not dead — but for the captain's own boot watcher it is a hard arm-failure.

Conclusion: capacity_exceeded IS in the "fatal" set — deliberately. It is treated as fatal-exit, not
reconnectable. That is the correct anti-flap behavior but the wrong availability behavior for a
long-lived planner.

### 2. subscribe_capacity_exceeded — the cap and the reject path

- Limit: `subscribeMaxConnectionsDefault = 32` — internal/daemon/subscribe.go:156.
- Reject path: `HandleSubscribe` compare-and-increment loop; when `cur >= maxConn` it writes
  `subscribe_capacity_exceeded` and returns — internal/daemon/subscribe.go:334-340.
- Slot-leak root cause + partial mitigation: the `subscribeWriteTimeout = 30s` write-deadline
  (subscribe.go:124-135, bead hk-qsz0p, commit 6d23fdcf9, LIVE) reaps a subscriber whose socket is held
  by a lingering descendant with no read-side EOF. This helps but does **not** cover the ppid=1
  orphaned-`--follow` accumulation described in hk-6629b (those keep reading heartbeats, so the write
  deadline never trips) — which is why hk-6629b remains open and proposes idle/dead-subscriber eviction
  and/or boot-time reaping of prior orphaned watchers for the same agent.

hk-6629b evidence quote: "5+ orphaned ppid=1 subscribers on the live socket incl. a 6-day-old prior-captain
`--to captain --follow` (pid 61270)." So yes — the fleet (~10 agents each holding a `--follow`, plus
leaked orphans) can exhaust the 32-slot pool, and commodore is rejected at join then exits.

### 3. Keeper auto-arm (hk-p006e) — does it now protect commodore?

- Fix e710147e moves `createCrewManagedMarker` to step 3.5, **before** `SpawnCrewSession`, so the
  in-session keeper's `IsManaged` check no longer loses the race. LIVE.
- This means a commodore launched via `harmonik crew start commodore` now gets a keeper armed, and the
  keeper will drive handoff→/clear→/session-resume on context fill (context-fill watcher only).
- BUT the keeper restarts on **context fill**, not on **process death**. A keeper does not defend against
  the orphan-sweep killing the tmux session — when the session is reaped there is no live pane for the
  keeper to act on, and the keeper itself dies with the pane. Keeper auto-arm is necessary but not
  sufficient for commodore survival. hk-zeo5y explicitly relates to hk-dtpoo ("keepers auto-supervised/
  revived", still OPEN).

### 4. Orphan-sweep invocation cadence

`RunOrphanSweep` is invoked at internal/daemon/daemon.go:1394 in the daemon startup path (boot-only, no
in-memory tracking) — matching hk-zeo5y's trace (orphansweep.go:731; exemptions at :550 captain sentinel,
:616/:690 crew registry + `crew-<name>` live fallback, flywheel sentinel). The interim mitigation
scripts/agent-session-watchdog.sh checks every 90s (CHECK_INTERVAL=90, line 22) and relaunches via
`harmonik crew start` (line 64) — but it is UNTRACKED, NOT SUPERVISED, and "died overnight (last start
23:57Z)" per hk-zeo5y, so when the daemon rebooted and reaped commodore, nothing brought it back.

## Why the ~7-minute cadence

The orphan sweep only fires at daemon boot. A ~7-min teardown cadence means the daemon was being
revived/redeployed roughly that often — consistent with a day of active fleet-hardening landing multiple
fixes (hk-62r8w, hk-p006e, hk-1q7bt, hk-fpjxi, hk-sfy7f, gofumpt) each triggering a redeploy/revive. Each
daemon boot reaped commodore; the 90s watchdog respawned it (whack-a-mole) until the watchdog itself died.

## Recommendation — the reliable-planner path

Do NOT expect a clean relaunch on b25e9919 to survive: the reaper (hk-zeo5y) is still open, so the next
daemon redeploy/revive reaps commodore again.

Priority order:

1. **Durable protection (hk-zeo5y, fix first).** Give commodore a first-class protected launch that is
   non-optional. Best option: a backstop **name-allowlist in the orphan-sweep** for recognized oversight
   roles (commodore/admiral/watch), gated on a live pane pid — so ANY launch path is protected, not just
   the exact crew-start RPC. Plus fold the watchdog into the supervisor (option B) so revival is durable
   rather than a detached nohup shell. Until this lands, if you must run commodore, launch it via
   `harmonik crew start commodore --mission .harmonik/crew/missions/commodore.md` (the registry+session
   path is the ONLY currently-protected route) AND keep a supervised watchdog running.

2. **Stop the slot leak (hk-6629b).** Add daemon-side idle/dead-subscriber eviction (heartbeat-timeout:
   no reads for N cycles → evict) and/or have crew/captain launch reap prior orphaned `--follow` watchers
   for the same agent on boot. This clears the `subscribe_capacity_exceeded` that blinds new watchers.
   Interim: manually kill ppid=1 `harmonik comms recv/subscribe --follow` orphans before relaunch.

3. **Make the subscribe client resilient to capacity (optional hardening).** For long-lived oversight
   watchers, `subscribe_capacity_exceeded` should be **reconnectable-with-backoff** (capped exponential),
   not a bare exit(1) — the current d1fbf715 behavior correctly kills the flap but also gives up
   permanently. A capacity rejection is transient (a slot frees when another subscriber leaves), unlike a
   malformed-request rejection. Distinguish the two: retry capacity with backoff, keep exit(1) for
   permanent protocol errors. Raising the 32 cap is a weaker fix (leak refills any cap).

4. **Verify.** After (1)+(2), relaunch commodore clean, force a daemon revive/redeploy, and confirm it
   survives ≥30 min across at least one daemon reboot with its comms watch still armed (no
   `subscribe_capacity_exceeded`).

## Bead status snapshot

- hk-62r8w CLOSED (comms flap → exit-1; token-waste fixed, not the killer).
- hk-p006e CLOSED (keeper auto-arm; context-fill only, not death).
- hk-1q7bt CLOSED (no_gauge flood; unrelated to teardown).
- hk-fpjxi / b25e9919 (branch reaper; unrelated to teardown).
- **hk-zeo5y OPEN P1 — PRIMARY root cause (orphan-sweep reaps commodore).**
- **hk-6629b OPEN P1 — SECONDARY (slot leak → subscribe_capacity_exceeded).**
- hk-dtpoo OPEN (keepers auto-supervised/revived) — related durability gap.
</content>
</invoke>
