# Round-2 Critique — Automatic Context-Reset Cycle (`MaybeRun`/`runCycle`/`RunForPrecompact`)

**Lens:** architecture/reliability of the AUTOMATIC cycle.
**Verdict up front:** the automatic cycle is **still open-loop on the two destructive injections** (`/clear` and `/session-resume`). The `await-ack` primitive can and should be wired in. This is the single highest-value remaining reliability fix. Severity: HIGH.

---

## 1. The automatic cycle is STILL fire-and-forget — cited

Trace of `runCycle` (`internal/keeper/cycle.go:752-940`). The cycle has exactly ONE confirmed step and TWO unconfirmed destructive steps:

| Step | Action | Confirmed? | Lines |
|---|---|---|---|
| 2b | inject `/session-handoff <path> … nonce` | **YES** — `pollForNonce` reads the handoff file back for the nonce | `799-813`, `1171-1189` |
| 4 | inject `/clear` | **NO** — error ignored; success inferred | `882-885` |
| 5 | wait for new session_id (best-effort) | partial / non-gate | `890-897` |
| 6 | inject `/session-resume <path>` | **NO** — error ignored; success inferred | `911-914` |
| 7 | mark journal `complete`, emit `cycle_complete` | unconditional | `919-923` |

The handoff step IS closed-loop (this is the SAFETY win — `/clear` is gated behind a positive nonce read-back, `cycle.go:751` "/clear is ONLY issued after the handoff nonce is positively confirmed"). But the **keystrokes that actually reset the session are never confirmed to land**:

- **`/clear` (line 884):** `_ = c.cfg.InjectFn(ctx, c.cfg.TmuxTarget, "/clear")` — the return value is discarded (`//nolint:errcheck`). The journal is then set to `Phase = "cleared"` (`886-888`) regardless of whether the paste landed.
- **`/session-resume` (line 913):** identical pattern — return discarded, `Phase = "resumed"` set unconditionally (`915-917`).
- **Success is marked at `cycle.go:919-923`** (`Phase = "complete"` + `emitCycleComplete`) with **zero read-back of the pane**. The only post-`/clear` signal consulted is `waitForNewSessionID` (`890-894`, `1193-1214`), and even that is explicitly **"best-effort, NOT a hard gate"** (line 890): when it returns `""` the cycle emits `clear_unconfirmed` (`895-897`) **and proceeds to inject `/session-resume` and mark complete anyway**.

So `cycle_complete` is emitted on the strength of "we wrote bytes to a tmux pipe," not "the session was reset." If the bracketed-paste was swallowed (pane busy, submit race, REPL not at prompt — the exact failure class round-1 report 04 flagged as 0%/10% covered), the keeper believes it succeeded, the journal reads `complete`, and the anti-loop suppression latches (`lastFiredSID = cf.SessionID`, `927`) — silently leaving an un-cleared, overflowing session unmonitored. **This is the open-loop hole round-1 named, and it is unchanged.**

`waitForNewSessionID` is NOT a substitute for ACK read-back: it polls the *gauge file* (`ReadGaugeFn`, `1205`), which is written by the Claude statusline hook, not by the `/clear` itself. It confirms "a new session eventually appeared," not "MY `/clear` keystroke caused it," and it's non-fatal so it gates nothing.

---

## 2. Wiring `await-ack` into the automatic cycle — concrete and worth it

The primitive already exists and is shaped to do exactly this. `AwaitAck` (`awaitack.go:94-172`) polls the pane via `PaneCapturer` for the exact token `AckMatchToken(nonce)` = `[KEEPER ACK <nonce>]` (`180-182`), returns `nil` when seen, or emits `session_keeper_ack_timeout` and returns `ErrAckTimeout` on timeout. The keeper→pane half is `AckLine(nonce, kind)` = `[KEEPER ACK <nonce>] received <kind>` (`injector.go:37-38`). `restart-now` already closes its loop by injecting `AckLine` **first** (`restartnow.go:130`), before `/clear`.

**What wiring it into `runCycle` changes concretely:**

1. The automatic cycle must **inject an ACK directive of its own**. Today `runCycle` injects `/clear` and `/session-resume` but never an `AckLine`, so there is no token for `AwaitAck` to find. The clean insertion is right before Step 4 `/clear` (mirroring restart-now's ordering): generate an ACK nonce (reuse `cycleID` — it's already unique and timestamp-prefixed), inject `AckLine(cycleID, "cycle")`, then call `AwaitAck` (or inline the same capture-poll) against `c.cfg.TmuxTarget`.
2. **Gate `/clear` on the ACK** (or at minimum gate `cycle_complete`): if `AwaitAck` times out, the pane is unresponsive/wrong/dead → abort the cycle the same way the handoff-timeout path does (`815-863`) instead of marking `complete`. This converts "inferred success" into "confirmed the agent's REPL is live and accepting our keystrokes" before the destructive `/clear`.
3. **Confirm the resume too:** optionally inject a second ACK after `/session-resume` and `AwaitAck` it, so `Phase = "complete"` means the resumed session actually came back, not just that we pasted text into a possibly-dead pane.

This requires a new injectable seam (a `PaneCapturer`/`AwaitAckFn` field on `CyclerConfig`) so the cycle stays unit-testable without tmux — consistent with the existing 27 `…Fn` injection points. Note the brief's claim that awaitack adds "ZERO code to cycle.go" (`awaitack.go:34`) is a *non-collision* design note for the primitive landing; it does NOT mean cycle.go must never call it. Wiring is the explicit follow-up (hk-uldg, "primitive not integrated").

**Is this the single highest-value remaining reliability fix? YES.** Of the round-2 known-open list, this is the only one that addresses *silent success-fabrication on the destructive path* — every other open item (positional args, crew watchers, identity heuristics, test leaks) is either cosmetic, additive, or a refinement of an already-functioning path. The automatic cycle is the keeper's *primary* job (the manual restart-now is the operator-triggered exception), yet it is the one path whose reset keystrokes are NEVER verified. Closing it makes the keeper's core promise — "I reset the session before it overflowed" — checkable instead of assumed.

---

## 3. Does Bug 3 fully kill the ACT-loop? Residual spin/loss paths

Bug 3 (`3f88fae5`) added two real protections, both present and correct in current code:
- **`lastFireWasAbort`** (`cycle.go:507`, set at `825`, checked at `603-604` and `1084-1085`): the same-SID below-Warn escape hatch is taken ONLY after a COMPLETED cycle, not after an abort. This kills the "re-fire a fresh nonce every gauge dip on a never-cleared session" loop (Bug 3a).
- **`handoffHasStaleNonce`** (`446-458`) gating the truncate at `788-790`: a genuine no-nonce handoff is PRESERVED; only a prior-cycle nonce is truncated. This kills the "wipe operator intent to 0 lines" loop (Bug 3b).

**Residual paths still reachable (adversarial):**

1. **Open-loop `/clear` failure → repeating forced cycles (the bigger live risk).** This is NOT a tight loop but a *slow recurring* one, and Bug 3 does not touch it. If `/clear` never lands (pane swallowed the paste) but the gauge happens to mint a new session_id for an unrelated reason (or `waitForNewSessionID` times out → `SetManagedSessionFn("")` clears the binding, `904`), the watcher re-latches, context is still high, and on the next breach the forced-clear path (`aboveForceThreshold`, `697-722`) re-fires every `ForceRetryInterval` (120s). Each iteration emits `cycle_complete` (success!) while nothing was cleared. Wiring await-ack (§2) is what actually closes this — Bug 3 only addressed the *abort* re-arm, not the *fake-complete* re-arm.

2. **Handoff-confirmed but `/clear` swallowed → anti-loop wedges a hot session.** After a fake-complete, `lastFiredSID = cf.SessionID` and `seenLowPctAfterLastFire = false` (`927-928`). If the session_id never changes (because `/clear` never ran) and context stays above Warn, Gate 6 same-SID suppression (`697-701`) holds — UNLESS above the force threshold, in which case it re-fires per §1. So the session is either silently suppressed-while-overflowing OR slow-looping. Both are "lose the reset," neither is the Bug-3 tight loop, both are open-loop symptoms.

3. **`pollForNonce` against a partial/echoed nonce.** `pollForNonce` (`1184`) matches `strings.Contains(content, nonce)`. The inject directive itself contains the nonce literal (`800-802`); if the agent's editor or a tool echoes the directive text INTO the handoff file without actually performing the handoff, the nonce appears and the cycle proceeds to `/clear` a stale-content handoff. Low-probability, but it is another "confirm by substring, not by semantic" gap of the same family.

So: Bug 3 fully resolves the *tight* ACT-when-idle loop it targeted, but the **open-loop destructive injections leave a slow recurring fake-complete cycle reachable**, which await-ack would close.

---

## 4. Is `RunForPrecompact` consistent with `runCycle` now?

`RunForPrecompact` (`1036-1134`) **converges into the SAME `runCycle`** (`1133`), so once gates pass, the open-loop `/clear`+`/session-resume` behavior is identical — both share the §1 defect, and both would equally benefit from the await-ack wiring (fix once in `runCycle`, both inherit it). Good: no divergence in the destructive core.

The gate sets are intentionally different (precompact skips CrispIdle + act_pct, per `1008-1013`) and that's by design. But two consistency notes:

- **The load-bearing invariant is still comment-only.** `RunForPrecompact`'s boot-grace (`1057-1059`) and anti-loop (`1075-1110`) depend on `currentSessionIDSince` / `lastFiredSID` / `seenLowPctAfterLastFire` being populated by `MaybeRun` running FIRST on the same tick — enforced ONLY by "watcher.go:568 before :580" in a comment (`1058-1059`, `1022-1023`). If a future edit reorders the watcher calls, precompact silently reads stale grace/anti-loop state. This is the "invariant enforced only by call-site ordering" smell round-1 flagged; still present.

- **Anti-loop side-effects are HAND-COPIED, not shared.** The re-arm observation (`1076-1078`) and the abort-gated escape hatch (`1084-1089`) are verbatim duplicates of `MaybeRun`'s `586-588` / `603-608`. Two copies of the same subtle `!lastFireWasAbort` logic must be kept in lockstep by hand — exactly the "each edge case is a new branch in N places" accretion pattern. If await-ack is wired into `runCycle`, keep it in the shared tail (where both entry points already converge) so this duplication is NOT extended into the new ACK logic.

---

## Bottom line

The automatic cycle confirms the HANDOFF (closed-loop) but fires `/clear` and `/session-resume` open-loop and marks `cycle_complete` without reading the pane back (`cycle.go:884`, `913`, `919-923`). The `await-ack` primitive (`awaitack.go`) is purpose-built to close exactly this gap the way restart-now already does (`restartnow.go:130`); wiring requires the cycle to inject its own `AckLine(cycleID,"cycle")` before `/clear` and `AwaitAck` it, plus a new injectable capturer seam. Bug 3 killed the tight abort-loop but a **slow fake-complete recurring cycle remains reachable** through the same open-loop injections. `RunForPrecompact` shares `runCycle`, so one fix covers both — but put the ACK logic in the shared tail to avoid extending the existing hand-copied gate duplication.
