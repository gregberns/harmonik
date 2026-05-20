# `harmonik run` post-success exit-path trace

Investigation of every place the `harmonik run --beads <id>` success path
could hang after the last bead reaches a terminal state. Companion to
HANDOFF.md line 131 (Phase-2 dogfood "Investigate stalled `harmonik run`
post-success").

## Success path trace

1. Last bead's goroutine (spawned at `workloop.go:871`) finishes its handler
   subprocess via `waitWithSocketGrace` (`workloop.go:1260`).
2. Outcome routes through `case term.Type == ProgressMsgTypeAgentCompleted`
   (`workloop.go:1292`) → `mergeRunBranchToMain` succeeds → `CloseBead`
   succeeds (`workloop.go:1306`) → `emitBeadClosed` (`workloop.go:1310`) →
   `emitDone(true, ...)` sets `*runSucceeded = true` (`workloop.go:917-920`).
3. `beadRunOne` returns; deferred `wtCleanup` runs (`workloop.go:979`),
   deferred `hookStore.CloseHookSession` (`workloop.go:1067`), deferred
   `close(hbDone)` stops heartbeat (`workloop.go:1137`).
4. The goroutine's wrapper calls `evaluateGroupAdvanceWithOutcome`
   (`workloop.go:880`).
5. Inside `evaluateGroupAdvanceWithOutcome`: item → completed, group →
   complete-success, queue → all-success → `queue.CompleteAndUnlink`
   (`workloop.go:1634`) persists `Status=completed` then unlinks
   `queue.json` (`persistence.go:196-208`).
6. `qs.ClearQueue()` (`workloop.go:1641`) → in-memory queue = nil.
7. `cancelOnQueueDrain()` (`workloop.go:1646`) cancels `runCtx` (set up at
   `run.go:357`); `cancelOnQueueExit()` also fires (`workloop.go:1651`)
   — same cancel func.
8. Per-bead goroutine `defer wg.Done()` + `defer runRegistry.Unregister`
   release (`workloop.go:872-873`).
9. Outer `runWorkLoop` poll loop next iteration: `<-ctx.Done()` selects
   (`workloop.go:476-480`) → `wg.Wait()` → return nil.
10. `daemon.Start` blocks on `<-loopDone` (`daemon.go:856`); now unblocks.
11. Deferred `jsonlWriter.Close()` (`daemon.go:457`) drains the writer
    goroutine via `<-w.done` (`jsonlwriter.go:291`).
12. Deferred `pidfile.Release()` (`daemon.go:384`) runs.
13. `daemon.Start` returns nil. `run.go:393` reads `qs.Queue()` → nil →
    `return 0` (`run.go:396`).
14. Deferred `cancelRun()` and `stop()` (signal.NotifyContext) run.
15. `runBeadSubcommand` returns; `main.go` calls `os.Exit(0)`.

## Hang risks identified

1. **HIGH — `cancelOnQueueExit` / `cancelOnQueueDrain` not fired if
   `CompleteAndUnlink` errors.** `workloop.go:1634-1638` falls through on
   error ("still clear in-memory state") but the comment is misleading:
   `lq.Done()` and `ClearQueue()` still run (lines 1639-1641), and the
   cancel-funcs still fire (lines 1645-1652). So this is actually safe.
   Re-classify LOW after re-read.

2. **HIGH — Socket listener goroutine drained but `go func() { <-socketDone
   }()` (`daemon.go:803`) is an orphan goroutine.** `daemon.Start` does not
   wait for it; on return the goroutine is left in the process. In practice
   it exits when `ctx.Done()` closes the listener (`socket.go:204`) and
   `RunSocketListener` returns, sending on `socketDone`. The drain goroutine
   then exits. But there is no synchronization — if `daemon.Start` returns
   before the listener observes ctx-cancel and closes, the process can
   exit before the drain runs. Acceptable for exit (no resource leak across
   process lifetime) but theoretical late-emit logs may be lost.

3. **MEDIUM — Per-connection socket handler goroutines (`socket.go:223`)
   are not waited on.** A late hook-relay envelope arriving as the daemon
   exits will hit `bus.Emit` on a bus that nobody is draining. `bus.Drain`
   is never called by `daemon.Start`. Since `Emit` is non-blocking
   (EV-002a), this does not hang, but events may be silently dropped.

4. **MEDIUM — `bus.Drain` is never called.** Asynchronous consumers (e.g.
   `HandlerPausePolicyGoroutine`, `QueueOperatorEventConsumer`,
   per-run taps) may still be processing the final
   `run_completed`/`bead_closed`/`queue_group_completed` events when
   `daemon.Start` returns. Without `bus.Drain(ctx)` the bus
   goroutines + worker pool may be torn down by process-exit mid-write.
   `daemon.go:565-567` emits `daemon_started` but there is no symmetric
   `daemon_stopped` (PL spec mentions it but it is not implemented at
   shutdown). Likely silent event-log truncation, not a hang.

5. **MEDIUM — `pasteInjectQuitOnCommit` goroutine (`workloop.go:1255`)
   is spawned but has its own polling deadline (`commitPollTimeout`,
   `pasteinject.go:125`).** If a /quit is never observed, this goroutine
   sits until the deadline OR until `ctx.Done()`. Since the outer ctx
   cancels on queue-drain, this is bounded. LOW in practice.

6. **MEDIUM — `RunHeartbeatLoop` goroutine
   (`claudehandler_chb006_024.go:524`).** Listens on both `ctx.Done()` and
   `done` channel. `defer close(hbDone)` (`workloop.go:1137`) fires when
   `beadRunOne` returns — before the success-path cancel — so this exits
   cleanly. SAFE.

7. **LOW — `agentReadyCallback` registered on hookStore
   (`workloop.go:1126`).** `defer CloseHookSession` (`workloop.go:1067`)
   tears down the session. Safe.

8. **LOW — `wg.Wait()` in `runWorkLoop` (`workloop.go:479`).** Waits for
   all in-flight bead goroutines. On the success path the goroutine has
   already returned by the time the outer loop re-checks ctx, so
   `wg.Wait` returns immediately. SAFE.

9. **HIGH — `qs.Queue() == nil` is the ONLY path that produces exit code
   0 in `run.go:393-397`.** If `ClearQueue` is skipped because
   `CompleteAndUnlink` returned an error (`workloop.go:1637` falls
   through), the queue is still cleared (line 1641 unconditionally calls
   `ClearQueue`). Verified safe — but the in-memory `Status` was set to
   `completed` by `CompleteAndUnlink` step 1 BEFORE the unlink failed, so
   even if `ClearQueue` were skipped, `qs.Queue().Status ==
   QueueStatusCompleted` would still drop into the "unexpected state"
   branch (`run.go:408-411`) returning exit code 2, not hanging. SAFE
   re. hangs; UX-bad on rare unlink failure.

10. **HIGH — On `CloseBead` failure in success path
    (`workloop.go:1306-1308` and `workloop.go:1333-1335`), `emitDone(false,
    "close-error: ...")` is called.** This sets `runSucceeded = false`
    → `evaluateGroupAdvanceWithOutcome` marks the item failed → queue →
    `paused-by-failure` → `cancelOnQueueExit` fires
    (`workloop.go:1666-1668`). Daemon exits, `run.go:398-407` archives the
    queue and returns exit code 1. **This is the prior hk-5dewt symptom:
    daemon stayed running because the close-fail retried instead of
    emitting failure.** With hk-5dewt fix (UnavailableRetryMax=10,
    UnavailableRetryCap=15s, WAL+history pre-flight) close should now
    succeed; if it still fails the new path correctly fires
    `cancelOnQueueExit` and the daemon exits. SAFE — this is the fix.

11. **MEDIUM — `bus.Emit` calls inside `evaluateGroupAdvanceWithOutcome`
    happen AFTER `lq.Done()` and AFTER the cancel-func fires
    (`workloop.go:1673-1679`).** If `cancelOnQueueDrain` cancels ctx
    before these Emits run, they may use a cancelled context. Since
    `bus.Emit` is async / non-blocking (EV-002a) this does not hang, but
    the final `queue_group_completed` event (the QM-033 durable landmark)
    may be dropped from the JSONL log. **This is a spec-compliance
    issue, not a hang risk.**

## Recommended verification

For each Phase-2 dogfood `harmonik run --beads <id>` invocation, capture:

- **Process exit code via `$?`** — confirm it is 0, not 1/2/124/137.
- **Wall-clock from `bead_closed` emit to process exit** — should be
  <500 ms. >2 s suggests bus-drain race or socket-listener lag.
- **`tail -n 20 .harmonik/events/events.jsonl`** — confirm the last event
  is `queue_group_completed` (QM-033 landmark). Absence confirms risk #11.
- **`ls .harmonik/queue.json`** — must be absent (CompleteAndUnlink ran).
  Present → CompleteAndUnlink failed or daemon exited before unlink.
- **`ls .harmonik/daemon.pid`** — must be absent (pidfile released).
  Present → defer chain did not run; process killed externally.
- **`pgrep -f harmonik` after exit** — must return empty. Any lingering
  PID confirms a hang risk above.
- **`grep -c bus.Emit .harmonik/events/events.jsonl`** vs goroutine count
  in a `runtime.NumGoroutine()` dump at exit — drift confirms risk #4
  (bus not drained on shutdown).
