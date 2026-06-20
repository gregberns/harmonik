# Keeper Critique — Concurrency & Timing Lens

**Critic angle:** The operator's core finding — *"unreliability is TIMING, not executability."* The /clear paste *works*; it lands *unreliably*. This report finds every timing/race hazard in the restart cycle.

**Verdict:** The cycle is **fire-and-forget paste against an unobserved REPL, fenced only by wall-clock sleeps and best-effort polls.** There is no end-to-end ACK that any of `/session-handoff`, `/clear`, or `/session-resume` actually landed. Every "fix" in the git history is another guessed sleep or another retry — the architecture has no closed feedback loop, so timing failures are structural, not incidental.

---

## 1. The restart-now cycle: ordering & timing assumptions

The happy path (`runCycle`, cycle.go:706, and `runOnDemandCycleTail`, cycle.go:1240) is:

```
truncate handoff → Escape → paste /session-handoff → pollForNonce →
append identity → setenv → paste /clear → waitForNewSessionID →
write .managed → paste /session-resume → done
```

Each `paste` is `InjectText` (injector.go:67): `tmux load-buffer → paste-buffer → sleep(submitSettle=750ms) → send-keys Enter → 2× retry Enter @400ms`.

### Hazard A — only ONE of three injections is confirmed (HIGH)
- `/session-handoff` IS confirmed — by `pollForNonce` (cycle.go:1363) waiting for the nonce comment in the handoff file. Good.
- **`/clear` is NOT confirmed as *executed*.** `waitForNewSessionID` (cycle.go:1385) polls the gauge for a *changed* session_id, but the gauge is written by `keeper-statusline.sh` on UI repaint and **skips the write on NA/absent pct right after /clear** (heartbeat.go:17-22 documents exactly this). So the new SID frequently does NOT appear within `ClearSettle` (3s). The code treats this as **non-fatal** (cycle.go:838 `emitClearUnconfirmed`, then proceeds to `/session-resume` anyway).
- **`/session-resume` is NOT confirmed at all.** It is pasted and the journal is marked `complete` (cycle.go:863) regardless of whether the keystrokes landed. This is the textbook "looked right at the time" failure: the journal says `complete`, the pane may show nothing.

So **2 of the 3 destructive steps are fire-and-forget.** The operator's "works but unreliable" is precisely this: the paste mechanism is *correct*, but nothing observes whether it took.

### Hazard B — `ClearSettle` (3s) is a guess that loses the post-/clear race (HIGH)
After `/clear`, a fresh Claude session must boot and a repaint must occur before the gauge carries a new SID. 3s is far too short for a cold resume; under CPU load it is hopeless. The code is *designed* to fail this poll ("Absent a new session_id is non-fatal", cycle.go:836) — meaning the common case is the unconfirmed case. Then `SetManagedSessionFn(..., "")` (cycle.go:847) **clears the .managed binding**, and the watcher must re-latch on a later tick. This is the documented "keeper goes DEAD after the 1st /clear; must re-resolve from gauge" failure signature, mechanized.

### Hazard C — `/session-resume` pasted before the new session can accept input (HIGH)
`/clear` resets the REPL and starts a new Claude process. `/session-resume` is pasted after at most `ClearSettle` (3s) of polling, OR **immediately** if `waitForNewSessionID` returns "" via ctx-timeout. There is **no gate that the post-/clear REPL is ready to accept a paste.** If the new session is still booting, the `/session-resume` paste lands in a not-yet-ready input handler and is dropped — the canonical "timing not executability" failure. `submitSettle` (750ms, injector.go:41) defends only the paste→Enter sub-race *within one* injection; it does nothing for the *across-injection* /clear→resume race.

### Hazard D — sleeps are wall-clock guesses that break under load (MED-HIGH)
`submitSettle=750ms`, `submitRetryDelay=400ms`, `ClearSettle=3s`, `onDemandSettle=3s`, `PollInterval=200ms`, watcher tick `5s`, `crispIdleTolerance=10s`. All are absolute durations. Per the operator's own memory (`reference_cpu_saturation_masquerades_as_daemon_flakiness`), CPU saturation makes every one of these misfire: a saturated host pushes paste-ingestion and session-boot well past these windows, so the settle elapses *before* the REPL has ingested anything. There is no adaptive backoff and no "is the pane done rendering?" check.

---

## 2. Is there any ACK that each step landed?

**No — not on main.** The only positive confirmation in the committed code is `pollForNonce` for the handoff step. `/clear` and `/session-resume` have none.

**Crucially:** an **ACK protocol IS designed but UNMERGED.** `docs/keeper-restart-now-ack-protocol.md` exists *only* in an uncommitted worktree (`.claude/worktrees/agent-acb20218b63a573e6/`). It specifies exactly the missing piece: a `[KEEPER ACK <nonce>]` line injected into the pane, a **synchronous** restart-now that "fails loudly" (non-zero exit), and **removal of the `.restart-now` marker path** — explicitly calling the marker "the silent-no-op bug … the marker was written under the caller's CWD while the watcher polled a different project dir, so the request was never seen."

The committed `cmd/harmonik/keeper_cmd.go:449` still does `WriteRestartNowMarker` + watcher poll (the *old, silent* path). **The fix for the operator's #1 complaint is written down but not in the running binary.** This is the single highest-leverage finding: spec/code drift between the ACK redesign and the deployed marker-poll path.

---

## 3. Races

### Race 1 — restart-now marker write vs watcher CWD (HIGH, already diagnosed in the unmerged doc)
`restart-now` writes `.restart-now` under `projectFlag` resolved from the *caller's* args; the watcher polls *its own* `ProjectDir`. If they differ (captain invoking from a worktree; default-project mismatch), the marker is written where the watcher never looks → silent no-op. The ack-doc names this as the root cause. Still live on main.

### Race 2 — handoff truncate vs agent still writing (MED)
`runCycle` truncates the handoff (cycle.go:736) *then* pastes `/session-handoff`. If the agent was mid-write of a *previous* handoff when the cycle fires, the truncate races the agent's write. `pollForNonce` partly covers this (it waits for the *new* nonce), but a partial write containing a *stale* nonce from a crashed prior cycle is the exact DEFECT-2 the truncate is meant to prevent — and it only works if the truncate strictly precedes the agent's first write byte, which is unsynchronized.

### Race 3 — `MaybeRun` then `RunForPrecompact`/`RunOnDemand` in the SAME tick can double-fire (MED-HIGH)
In `watcher.go:753-781` a single tick calls, in order: `Cycler.MaybeRun`, then (if marker) `RunForPrecompact`, then (if marker) `RunOnDemand`. These are **not mutually exclusive.** If context is above threshold AND a `.precompact` marker exists AND a `.restart-now` marker exists, all three can run in one tick. `MaybeRun` could complete a full handoff→clear→resume, and then `RunForPrecompact`/`RunOnDemand` runs again against a session that was just cleared. The anti-loop `lastFiredSID` set by the first call (cycle.go:870) *partially* guards this within the tick (subsequent calls hit Gate 4/anti-loop), but `RunForPrecompact`'s anti-loop check (cycle.go:1036) reads the same field that `MaybeRun` just set to the **pre-clear** SID — and the precompact path can still construct a minimal CtxFile (cycle.go:1066) and run. The ordering is load-bearing and fragile; it depends on field side-effects flowing between three methods on one struct within one tick.

### Race 4 — keeper restart vs daemon respawn (MED)
`maybeRespawn` (watcher.go:958) runs `sh -c RespawnCmd` when the pane is idle; `maybeLivePaneRecover` (watcher.go:1039) runs `LiveRecoverFn` (also a respawn) when the pane is alive. They share the stale-gauge branches and are separated only by `IsPaneIdle` vs `IsPaneAlive` — both query `#{pane_current_command}` at *different instants*. A pane that flips shell↔claude exactly between the two `tmux display-message` calls (e.g. mid-/clear, when the pane briefly shows a shell) can satisfy *neither* or be misclassified, either respawning a live agent or skipping a dead one. The "mutually exclusive" claim (watcher.go:1013) holds only for a single atomic query, but they are two separate queries.

### Race 5 — heartbeat write vs statusline write vs cycle's gauge read (LOW-MED)
`maybeHeartbeat` (heartbeat.go:178) and `keeper-statusline.sh` both write `.ctx`; the heartbeat uses atomic tmp+rename (good), the statusline's atomicity is unverified here. `waitForNewSessionID` reads the gauge concurrently. The heartbeat carries forward `last.SessionID` and re-derives tokens — if it fires *during* the ClearSettle window it can re-stamp the **old** SID onto a fresh-mtime gauge, actively *defeating* `waitForNewSessionID`'s "SID changed?" test and making the unconfirmed-clear case *more* likely.

---

## 4. Multiple keepers

**Guarded — adequately.** `AcquireLock` (keeper.go:59) takes a per-agent `LOCK_EX|LOCK_NB` flock on `<agent>.lock`. A second keeper for the *same agent* gets `ErrLockHeld`. This is correct and is the one piece of robust concurrency control in the subsystem. Note it is **per-agent, not per-process**: two keepers for *different* agents coexist by design (correct), and nothing prevents a keeper and an out-of-band manual `restart-now` from both acting (the latter is a CLI marker-write, not a lock holder).

## 5. The fork-bomb (1500+ tmux sessions)

Per operator memory (`reference_keeper_smoke_forkbomb`), the leak came from an **ad-hoc looping restart-now smoke** (`hk-smoke-keeper`), not the committed test. The relevant question is whether the *production* loop is now bounded:

- The watcher loop itself is a single `time.Ticker` (watcher.go:591) — bounded cadence, no spawn-per-tick.
- The respawn paths are guarded by `RespawnCooldown` (90s) and `LiveRecoverCooldown` (5m). Bounded.
- The cycle anti-loop (`lastFiredSID` + `seenLowPctAfterLastFire`, cycle.go:469) plus `ForceRetryInterval` (120s) bound re-fires.

**But the bound is a tangle of interacting timers, not one debounce.** I count **nine** distinct anti-loop/grace/retry mechanisms in `MaybeRun` alone: boot-grace, MaxBootGraceTotal, lastFiredSID same-SID suppression, different-SID seenLowPct suppression, two force-retry exceptions, ForceRetryInterval, consecutiveHandoffTimeouts escalation, and the re-arm escape hatch. Each was added by a separate bead (hk-4f8, hk-ibb, hk-qoz, hk-hz9, hk-uxu, hk-0uu…). The fork-bomb is contained today, but the containment is **emergent from nine timers that must all agree** — a classic "any one off-by-one re-opens the loop" surface. The *real* fix (an external spawn-count ceiling / a single debounce) is absent.

## 6. Idempotency / double-fire

- `restart-now` / `precompact` markers are **consume-once**: cleared at entry (cycle.go:1110) / always-cleared (RunForPrecompact). Good.
- BUT a retried/duplicated **tick** does not double-fire only because of the `lastFiredSID` in-memory state. That state is **lost on keeper restart.** `RecoverFromCrash` (cycle.go:897) reads the journal: a journal at phase `cleared` will **re-inject /session-resume** on boot — correct — but a journal at `resumed`/`complete` is a no-op, and there is no protection against a keeper that crashes *after* `/clear` but *before* writing the `cleared` journal phase: it boots, sees phase `handoff_injected`, aborts (cycle.go:934), and the agent is left cleared-but-not-resumed with the keeper believing nothing happened. The journal phase write (cycle.go:829) and the actual `/clear` paste (cycle.go:827) are **not atomic** — the paste precedes the journal write, so a crash between them is the un-recoverable window.

---

## Severity-ranked summary

| # | Hazard | Severity |
|---|--------|----------|
| 2 | ACK protocol designed but UNMERGED; main still uses silent marker-poll restart-now | **HIGH (top)** |
| A | 2 of 3 destructive injections are fire-and-forget (no /clear or /resume ACK) | HIGH |
| B/C | ClearSettle=3s loses the post-/clear race; /session-resume pasted into a not-ready REPL | HIGH |
| 1 | restart-now marker written under caller CWD ≠ watcher ProjectDir → silent no-op | HIGH |
| D | All fences are wall-clock sleeps; CPU saturation makes them misfire | MED-HIGH |
| 3 | MaybeRun + precompact + restart-now can run in one tick; ordering side-effects load-bearing | MED-HIGH |
| 5 | Heartbeat can re-stamp old SID during ClearSettle, defeating new-SID detection | MED |
| 4 | respawn vs live-recover classified by two separate tmux queries → pane-flip misclassify | MED |
| 6 | /clear paste precedes journal write non-atomically → unrecoverable crash window | MED |

**One-line verdict:** The keeper's reset cycle is an open-loop paste machine — it confirms the handoff but *assumes* /clear and /session-resume land, fencing them with guessed wall-clock sleeps that the host's own CPU load defeats; the closed-loop ACK fix is written down but stranded in an unmerged worktree, which is exactly why it "works but is unreliable."
