# RT14 — convert the last two open-coded ready-waits to `dispatchSegment`, retire `waitAgentReady`

**Status:** **READY** — no operator decision required, no new seam invented.
**Depends on:** RT13 (merge carve-out → `internal/runmerge`) **committed and green**, and E4c
(`buildWorkerRegistry` → `internal/workers/bootwire.go`) landed. Neither is a *semantic* dependency —
RT14 touches none of their symbols — but both edit `internal/daemon/workloop.go`, which RT14 rewrites
270 lines of, so they must not be in flight concurrently (§6).
Explicitly **NOT** gated on E1a/E1b/E1c or E4 (E5 §7.3 says so, and it is verified: the converted
regions call no `reversetunnel.go` symbol and no harness-package symbol that has not already moved).

**Size:** ~430 LOC net across **9 files** — ~272 lines restructured in `workloop.go`, ~91 in
`dot_gate.go`, ~158 deleted outright (`agentready.go` −90, `workloopeventsource.go` −46,
`export_test.go` −22), 1 test file deleted (264 LOC), 1 test file re-pointed, 1 new freeze-gate script,
1 Makefile edit.

**Risk:** **MEDIUM-HIGH.** Every line is inside `beadRunOne` — the single-mode dispatch path that every
non-DOT run traverses — in the tree's hottest file (`workloop.go`, **331 commits/90d**). The mitigating
facts are that the target seam is already load-bearing at three other sites, and the conversion is a
line-for-line transposition of `reviewloop.go:579-759` with the site's own bodies pasted in verbatim.

---

## 0. Corrections to E5 §4 steps 14–18 — read this before anything else

E5-dot-runloop.md's RT14 section is **wrong in three places**. Each was re-verified against the
post-RT13 tree on 2026-07-22; the corrected facts govern.

| E5 §4 claim | Reality (verified) | Consequence |
|---|---|---|
| step 14/15: sites are `workloop.go:4897` and `dot_gate.go:474` | `workloop.go:**4905**` and `dot_gate.go:**476**` (`grep -n 'waitAgentReady(' internal/daemon/*.go` excluding tests → exactly 3 hits: the two call sites + the declaration at `agentready.go:154`) | line anchors below are post-RT13; use the grep anchors, not the numbers, because E4c shifts them |
| step 17: **"`rm internal/daemon/agentready.go`"**, and §1 "its only remaining reference is the `export_test.go:1569` shim" | **FALSE, and unsafe.** `agentready.go` declares **six** symbols. Four are live in production *after* RT14: `defaultAgentReadyTimeout` (read at `workloop.go:6595` inside `emitAgentReadyTimeout`), `defaultRemoteAgentReadyTimeout`, `effectiveAgentReadyTimeout` (**4** production call sites — every `dispatchSegment` cfg: `reviewloop.go:587`, `reviewloop.go:1368`, `dot_cascade.go:1772`, plus the two RT14 creates), and `ErrAgentReadyTimeout` (`reviewloop.go:730`, `dot_cascade.go:1866`, plus the two RT14 creates, plus ~10 test files). The shim is at `export_test.go:**1421-1442**`, not `:1569`. | **The file is NOT deleted.** Only `waitAgentReady` (:137-207) and `agentEventSource` (:118-135) are deleted — 90 of 207 lines. The remaining 117 lines stay. §7 records why a wrong deletion here would have been unrecoverable. |
| step 16: "delete the **eight** raw-time sites — `dot_gate.go` `:484 :751 :758 :767 :773 :786`, `waitsocketgrace.go`, `postreadyhang.go`" | Only **one** of those eight is in RT14's blast radius. `dot_gate.go:486` (`time.After(agentReadyKillReapTimeout)`) is inside the ready-timeout kill block and converts for free as `clockAfter(deps.clock, …)` inside the `killReady` hook. The other five dot_gate sites (`:753 :760 :761 :769 :775 :788`) live in **`pasteInjectQuitOnGateFile`** — a Working-phase quit watchdog, not part of the dispatch segment; its two siblings in `pasteinject.go` carry **22** further raw-time sites, so converting only this copy closes nothing and diverges the three watchdogs. `waitsocketgrace.go:113` and `postreadyhang.go:59` are the Working-phase completion wait, which `dispatchsegment.go:17-20` **explicitly places outside the RT8 segment boundary** until the M5 full reactorization. | **Step 16 is descoped to its one in-segment site.** The remaining seven are re-filed as a separate future slice (**RT19c — clock-port the Working-phase watchdogs**, ~29 sites across `pasteinject.go`/`dot_gate.go`/`waitsocketgrace.go`/`postreadyhang.go`). Attempting them inside RT14 would be a logic change to code the segment does not own. |

Two E5 claims that **held up** and are relied on below: `waitAgentReady` really has exactly two
production callers; and `beadRunOne` (workloop.go 3167–5400) is verified free of raw wall-clock
(`awk 'NR>=3167 && NR<=5400' … | grep -cE 'time\.(After|Now|NewTimer|NewTicker|Tick|Sleep)\('` → `0`),
so RT14 does not have to fix any timer inside it.

---

## 1. What changes

| File | Lines | What | Why |
|---|---|---|---|
| `internal/daemon/workloop.go` | 4685–4956 (272 lines) | Replace the open-coded `Launch` + 200 lines of post-launch wiring + `waitAgentReady` with one `&dispatchSegment{…}` whose hooks hold the existing bodies **verbatim** | The single-mode path is the last dispatch site still doing its own wall-clock ready wait; it cannot be FakeClock-driven, and it is the reason `waitAgentReady` still exists |
| `internal/daemon/workloop.go` | 4600 (`var sess handler.Session`) | Add `var watcher *handlercontract.Watcher` / `var launchErr error` beside it | The launch closure assigns all three; `sess` is already predeclared for `agentEndCb` (PI-014), so this just extends the same idiom |
| `internal/daemon/dot_gate.go` | 406–496 (91 lines) | Same conversion, using `dot_cascade.go:1764-1888` as the template | The cognition gate is the second and last `waitAgentReady` caller |
| `internal/daemon/dot_gate.go` | import block (:24-43) | **Delete the `"errors"` import**; **add `"github.com/gregberns/harmonik/internal/runexec"`** | `errors.Is` at :479 is the file's **only** `errors.` use (`grep -n 'errors\.' internal/daemon/dot_gate.go` → :73 comment, :479 code). Removing the ready block orphans the import. `runexec` is needed for `DispatchConfig` / `DispatchFailed`. `workloop.go` already imports `runexec` and keeps `errors` (many other uses) |
| `internal/daemon/agentready.go` | 118–135, 137–207 | **Delete** `agentEventSource` and `waitAgentReady`; rewrite the file header (:3-30) which describes only the deleted function | Zero production callers after the two conversions. Deleting the last `time.After` in the file (`:194`) takes agentready.go to 0 raw-clock sites |
| `internal/daemon/agentready.go` | 1–117 | **KEEP.** `defaultAgentReadyTimeout`, `defaultRemoteAgentReadyTimeout`, `effectiveAgentReadyTimeout`, `ErrAgentReadyTimeout` are all live | See §0 |
| `internal/daemon/workloopeventsource.go` | 185–230 | **Delete** `chanAgentEventSource`, `newChanAgentEventSource`, its `Events` method; scrub the `waitAgentReady` prose at :3-23/:49-66/:90/:105-107/:139/:203 | Its only two constructors were the two `waitAgentReady` call sites. `perRunEventTap` (1–183) **stays** — it is what every `dispatchSegment` consumes as `tap`/`tapCh` |
| `internal/daemon/export_test.go` | 1421–1442 | **Delete** `AgentEventSourceExported` and `ExportedWaitAgentReady` | The shims for the two deleted symbols |
| `internal/daemon/agentready_hkgql2018_test.go` | whole file (264) | **Delete** | Four unit tests of `waitAgentReady` only. Coverage transfer argued in §7.3 — do not delete until that argument is checked |
| `internal/daemon/twinparity_timing_property_test.go` | 135–178, 189 | Re-point stage 1 of `observeAnomalies` off `daemon.ExportedWaitAgentReady` | The only other caller. §4 phase C gives the exact replacement |
| `scripts/readywait-freeze-gate.sh` | new (~70) | Ratchet: no re-declaration of the retired symbols, no new raw wall-clock in the six run-path files that are clean today | `_plan.md` §3.2 — an extracted concern is a closed door, hard CI failure |
| `Makefile` | `check-fast` (:503-509) and `check-short` (:531-537) | Add `scripts/readywait-freeze-gate.sh` after `runmerge-freeze-gate.sh` | Same wiring as the six existing gates |

**Not touched, deliberately:** `pasteinject.go`, `waitsocketgrace.go`, `postreadyhang.go`,
`dot_gate.go:736-797` (`pasteInjectQuitOnGateFile`) — see §0 row 3.

---

## 2. The seam it exits behind

**`dispatchSegment`, declared at `internal/daemon/dispatchsegment.go:73`**, driven by
`(*runShell).RunDispatch` at `internal/daemon/runshell.go:376`, over the pure reactor
`runexec.Dispatch` at `internal/runexec/dispatch.go:104`.

**No new seam is invented.** The seam is not merely pre-existing — it is already the *only* dispatch
mechanism at three of the five agent-launch sites in the daemon:

| Existing consumer | Site | Shape |
|---|---|---|
| review-loop implementer | `reviewloop.go:579-760` | full hook set incl. `deliver` with paste-inject + quit-on-commit |
| review-loop reviewer | `reviewloop.go:1361` | `emitReadyTimeout: nil` (the reviewer phase never emitted one) |
| DOT agentic node | `dot_cascade.go:1764-1888` | `SkipReadyHandshake` + the post-run `dotDeliver(ctx)` fall-through |

### Does the seam need widening? **No.** Verified hook-by-hook against both sites.

`dispatchSegment` exposes seven site-owned closures (`dispatchsegment.go:95-106`) plus three config
scalars. Every behaviour the two remaining sites need maps onto one of them:

| What the site does today | Hook it lands in | Precedent |
|---|---|---|
| `runH.Launch(ctx, spec)` → `sess, watcher, launchErr` | `launch` (returns `watcher.Done()` or nil) | `reviewloop.go:600-609` |
| `errors.Is(launchErr, ErrSpawnCapTimeout)` → `emitSpawnCapBlocked`; `ErrTmuxNewWindowTimeout` → `emitTmuxNewWindowTimeout` (workloop only) | `onLaunchFailed` | `reviewloop.go:610-627` |
| held-back `launch_initiated`, `SetMachine`, presence-online, `SetAgentReadyCallback`, `RunHeartbeatLoop` | `onLaunched` | `dot_cascade.go:1815-1862` |
| paste-inject + `pasteInjectQuitOnCommit` / `pasteInjectQuitOnGateFile` | `deliver` | `reviewloop.go:664-725` |
| ready-timeout `Kill` + bounded watcher reap + `Wait` + `CloseHookSession` | `killReady` | `dot_cascade.go:1864-1878` |
| `emitAgentReadyTimeout(…)` | `emitReadyTimeout` | `dot_cascade.go:1879-1881` |
| ctx-cancel idempotent kill | `killAbort` | `dot_cascade.go:1882-1886` |
| `effectiveAgentReadyTimeout(local, remote, isRemote)` | `cfg.ReadyTimeout` | `dot_cascade.go:1772` |
| `agentReadyKillReapTimeout` | `cfg.ReadyKillReap` | `dot_cascade.go:1774` |
| ProcessExit harness skips the handshake | `cfg.SkipReadyHandshake` | `reviewloop.go:583` |
| "no adapter → skip ready-wait but still brief" | `adapter: nil` (segment feeds a synthetic `EvAgentReady`, `dispatchsegment.go:167-172`) | `reviewloop.go:576` |

Ordering is preserved by the machine, not by luck: `dispatchReadyTimeoutEdge`
(`internal/runexec/dispatch.go:255-263`) emits `[ActKillAgent, ActArmTimer, ActEmit(agent_ready_timeout)]`
**in that order**, and `killAgent` runs the site's kill+reap **synchronously**
(`dispatchsegment.go:224-235`), so `agent_ready_timeout` is still emitted after the reap completes — the
pre-RT8 posture at both sites. `RunDispatch` runs on the caller's goroutine and drives effectors inline
(`runshell.go:376-388`), so any outer variable a hook assigns is visible once `seg.run(ctx)` returns.

**Conclusion: the seam is complete. There is no prep step and nothing to escalate.**

---

## 3. Coupling to break

### 3a. Outbound (converted regions → daemon symbols they keep calling)

RT14 moves no code out of `internal/daemon`, so no coupling is *broken* — the point is the coupling it
**stops creating**. Every symbol below stays where it is and keeps its current visibility.

| Symbol | Declared | Used by the converted regions | After RT14 |
|---|---|---|---|
| `dispatchSegment`, `dispatchSegmentInputAckWindow` | `dispatchsegment.go:73`, `:68` | both new sites | 5 consumers (was 3) |
| `effectiveAgentReadyTimeout` | `agentready.go:106` | both new `cfg.ReadyTimeout` | 4 production sites (was 4 — 2 segments + 2 open-coded; unchanged count, uniform shape) |
| `ErrAgentReadyTimeout` | `agentready.go:116` | both new `killReady` stderr lines | still live; **do not delete** |
| `agentReadyKillReapTimeout` | `workloop.go:155` | both new `cfg.ReadyKillReap` + `killReady` | unchanged (an E5 §3a "third band" back-edge; RT19b's problem, not RT14's) |
| `clockAfter` | `workloop.go:1225` | both new `killReady` reap selects | unchanged; replaces `dot_gate.go:486`'s raw `time.After` |
| `emitAgentReadyTimeout` | `workloop.go:6593` | both new `emitReadyTimeout` | unchanged |
| `emitSpawnCapBlocked`, `emitTmuxNewWindowTimeout`, `substrateSpawnStats`, `ErrSpawnCapTimeout`, `ErrTmuxNewWindowTimeout` | `workloop.go:8xxx` / `tmuxsubstrate.go` | workloop's `onLaunchFailed` only | unchanged. **Do NOT add these to `dot_gate`'s `onLaunchFailed`** — the gate has never emitted them, and adding them is a logic change, not a move |
| `perRunEventTap` (`newPerRunEventTap`, `Subscribe`, `EmitWithRunID`) | `workloopeventsource.go:67-183` | both sites' `tap`/`tapCh` | unchanged — this is why `workloopeventsource.go` shrinks but survives |

### 3b. Inbound (daemon → new exports)

**Total symbols needing export: ZERO.** RT14 is entirely intra-package. It is the first P2 slice with a
*negative* export delta:

| Export | Direction | Note |
|---|---|---|
| `daemon.AgentEventSourceExported` (`export_test.go:1429`) | **DELETED** | consumed only by `agentready_hkgql2018_test.go` (deleted) and `twinparity_timing_property_test.go:135` (re-pointed) |
| `daemon.ExportedWaitAgentReady` (`export_test.go:1434`) | **DELETED** | same two consumers |
| `daemon.ExportedErrAgentReadyTimeout` (`export_test.go:1393`) | **KEPT** | still used by `twinparity_timing_property_test.go` and others; `ErrAgentReadyTimeout` survives |
| `daemon.ExportedDefaultAgentReadyTimeout` (`export_test.go:1410`) | **KEPT** | `defaultAgentReadyTimeout` survives |

`export_test.go` goes 2,895 → ~2,873 LOC.

### 3c. The deletion-safety proof (run this yourself before deleting anything)

The task that commissioned this plan asked for verification, not assertion. This is the check, and its
measured result on 2026-07-22 post-RT13:

```bash
cd /Users/gb/github/harmonik
for s in waitAgentReady agentEventSource chanAgentEventSource newChanAgentEventSource \
         ErrAgentReadyTimeout effectiveAgentReadyTimeout \
         defaultAgentReadyTimeout defaultRemoteAgentReadyTimeout; do
  echo "=== $s ==="
  grep -rn "\b$s\b" --include='*.go' . \
    | grep -v '^\./internal/daemon/agentready\.go:' \
    | grep -vE ':[0-9]+:[[:space:]]*//' \
    | grep -vE '"[^"]*'"$s"           # drop matches inside string literals
done
```

Measured verdict:

- `waitAgentReady` — 2 production call sites (`workloop.go:4905`, `dot_gate.go:476`); every other one of
  the ~150 hits across 60 files is **a comment or a message string**. 2 real test callers
  (`agentready_hkgql2018_test.go` ×4, `twinparity_timing_property_test.go:189`) via the export shim.
  **→ deletable once the 2 call sites convert and the 2 test files are handled.**
- `agentEventSource` / `chanAgentEventSource` / `newChanAgentEventSource` — sole constructors are the
  same 2 call sites; sole interface implementor is `chanAgentEventSource`. **→ deletable together.**
- `ErrAgentReadyTimeout` — live at `reviewloop.go:730`, `dot_cascade.go:1866` in production **today**,
  plus ~10 test files. **→ NOT deletable.**
- `effectiveAgentReadyTimeout` — live at `reviewloop.go:587`, `reviewloop.go:1368`, `dot_cascade.go:1772`.
  **→ NOT deletable.**
- `defaultAgentReadyTimeout` — live at `workloop.go:6595` (`emitAgentReadyTimeout`'s zero fallback) and
  in 3 test files. `defaultRemoteAgentReadyTimeout` — live in `agentreadyremote_hk96d7w_test.go` ×8.
  Both referenced in `daemon.go:167`/`:185` **as comments only**, which is exactly the trap that makes a
  naive `grep -c` say "delete the file". **→ NOT deletable.**

---

## 4. Step-by-step recipe

**Ground rules.** Operate from `/Users/gb/github/harmonik` with absolute paths; never `cd` into a
worktree. Stage by explicit pathspec — **never `git add -A`, `git commit -a`, `git reset --hard`, or
`git clean`** (PROGRESS.md §"We nearly collided at 08:00"). Change **no** emission string, **no**
stderr message text, **no** signature. Every hook body is the pre-RT14 code *character-for-character*,
re-indented only.

Three phases. **Each phase ends green** (`go build ./internal/... ./cmd/...` + `go vet ./internal/...`).
Phase A alone is a complete, releasable state if the merge window closes early (§6).

---

### Phase A — convert `workloop.go` (the hard one)

**A1.** Re-anchor. E4c will have shifted these; re-derive before editing:

```bash
grep -n 'sess, watcher, launchErr := runH.Launch' internal/daemon/workloop.go   # the Launch, ~:4691
grep -n 'readyErr := waitAgentReady'              internal/daemon/workloop.go   # the wait,   ~:4905
grep -n 'releaseSpawnSlot()'                      internal/daemon/workloop.go   # post-ready, ~:4964
grep -n 'socketOutcome, ei := waitWithSocketGrace' internal/daemon/workloop.go  # segment end,~:5044
```

**A2.** At the `var sess handler.Session` predeclaration (`:4600`), add:

```go
var watcher *handlercontract.Watcher
var launchErr error
var hbDone chan struct{}
```

**A3. Hoist the two resolutions above the segment.** Move `completionMode` (`:4864-4869`) and the
`adapter, adapterErr := deps.adapterRegistry.ForAgent(artifactAgentType(artifacts))` block
(`:4871-4875`) to just after `implementerLaunchedAt := deps.clock.Now()` (`:4690`), and set
`adapter = nil` in the error branch (mirroring `reviewloop.go:576`). **Keep the stderr string byte-identical.**

> **Sanctioned divergence, must be named in the commit body:** the "skipping ready-wait" stderr line now
> prints *before* Launch instead of after. It is stderr only — no bus event, no ordering the replay
> checkers observe. This is the same divergence `reviewloop.go` and `dot_cascade.go` already took.

**A4. Keep at the site, before `seg.run(ctx)`, in this exact order** (all currently at :4640-4690):
`RegisterHookSession` + its defer → `implLaunchInitiatedMsg := emitPreExecBeforeLaunch(…)` →
`tap, tapCh := newPerRunEventTap(…)` → `runH := handler.NewHandler(tap, …)` → the `agentSpawnSem`
acquire + `releaseSpawnSlot` + its deferred backstop → `bridge.start(ctx, workflowMode)` →
`implementerLaunchedAt` → (A3's hoisted resolutions).

**A5. Build the segment.** Skeleton — hook bodies are the verbatim originals:

```go
implSeg := &dispatchSegment{
    clock: deps.clock,
    runID: runID,
    cfg: runexec.DispatchConfig{
        SkipReadyHandshake: completionMode == handlercontract.CompletionProcessExit,
        IsResume:           false, // single-mode beadRunOne is always a fresh launch (pre-RT14 parity)
        MaxInputAttempts:   1,
        // hk-96d7w: remote dispatch (rbc != nil) gets the longer remote window.
        ReadyTimeout:  effectiveAgentReadyTimeout(deps.agentReadyTimeout, deps.remoteAgentReadyTimeout, rbc != nil),
        InputAck:      dispatchSegmentInputAckWindow,
        ReadyKillReap: agentReadyKillReapTimeout,
    },
    adapter:     adapter, // nil when adapterErr != nil → synthetic ready, brief still delivered
    probeResume: false,   // pre-RT14 parity: the single-mode path had no resume accommodation
    tap:         tap,
    tapCh:       tapCh,

    launch: func(lctx context.Context) (<-chan struct{}, error) {
        sess, watcher, launchErr = runH.Launch(lctx, spec)
        if launchErr != nil {
            return nil, launchErr
        }
        if watcher != nil {
            return watcher.Done(), nil
        }
        return nil, nil
    },

    // body = workloop.go:4693-4708 VERBATIM (stderr line + the two errors.Is emissions).
    // The `reason := …; failRun(reason, reason); return` stays at the SITE (A6).
    onLaunchFailed: func(lctx context.Context, lErr error) { /* … */ },

    // body, in this order, = :4718-4720, :4725-4727, :4777, :4814-4838, :4843-4846.
    // hbDone is assigned here (NOT `:=`); its `defer close(hbDone)` is registered at the site (A7).
    onLaunched: func(lctx context.Context) { /* … */ },

    // body = workloop.go:4974-5039 VERBATIM (the `var noChangeTimeoutCh` DECLARATION stays at
    // the site; the closure only assigns it). Keep the
    // `if completionMode != handlercontract.CompletionProcessExit` guard inside the closure
    // even though SkipReadyHandshake already makes it unreachable in that case — removing it
    // would be a logic change, and it is the belt to SkipReadyHandshake's braces.
    deliver: func(dctx context.Context) { /* … */ },

    // body = workloop.go:4910-4937 VERBATIM: the stderr line (readErr → ErrAgentReadyTimeout,
    // which is what it already was, per errors.Is), sess.Kill(ctx), the clockAfter watcher reap,
    // and the hk-4hso5 bounded `context.WithTimeout(context.Background(), agentReadyKillReapTimeout)`
    // sess.Wait. The workloop site does NOT CloseHookSession here — do not add it.
    killReady: func(kctx context.Context) { /* … */ },

    // body = workloop.go:4944 VERBATIM — including `context.Background()` (hk-4hso5, NOT ectx)
    // and `deps.agentReadyTimeout` (NOT the effective remote window). Both are pre-RT14
    // behaviour; preserving a latent inconsistency is what "pure move" means.
    emitReadyTimeout: func(ectx context.Context) { /* … */ },

    killAbort: func(context.Context) {
        if sess != nil {
            _ = sess.Kill(context.Background())
        }
    },
}
implDispatch := implSeg.run(ctx)
```

**A6.** Immediately after `implSeg.run(ctx)`:

```go
if launchErr != nil {
    reason := fmt.Sprintf("launch error: %v", launchErr)
    failRun(reason, reason)
    return
}
```

**A7. Register the four deferred cleanups, in the original textual order** — this preserves LIFO firing
order exactly, and it must happen **before** the ready-timeout check (this is what `reviewloop.go:773-778`
does *before* its check at `:780`, and `dot_cascade.go:1901-1905` before `:1907`):

1. the hk-j6wm7 Pi stderr-tail defer (`:4736-4755`), unchanged including its `runIsPi && piCaptureDir != ""` guard;
2. `defer func() { if !useIndepSession || ctx.Err() == nil { forceTeardownSession(sess) } }()` (`:4768-4772`);
3. `defer func() { emitImplPresence(context.Background(), deps.bus, beadID, …Offline, …Leave) }()` (`:4778-4780`);
4. `if hbDone != nil { hbDoneToClose := hbDone; defer close(hbDoneToClose) }` (`:4847`).

> This is the **single highest-risk line-ordering constraint in RT14.** Register these *after* the
> ready-timeout early return and the agent_ready_timeout path silently stops tearing down the session,
> stops emitting presence-offline, and leaks the heartbeat goroutine.

**A8.** The ready-timeout terminal, replacing `:4908-4949`:

```go
if implDispatch.Phase == runexec.DispatchFailed && implDispatch.Reason == "agent_ready_timeout" {
    // RT7 / RSM-031 row 1: the ready-timeout Dispatch terminal maps onto the Run reopen spine.
    failRun("agent_ready_timeout", "agent_ready_timeout")
    return
}
// Working / Exited / Aborted: fall through — the pre-RT14 posture for agent_ready-observed,
// watcher-exit-first (context.Canceled), and ctx-cancel.
```

**A9.** `releaseSpawnSlot()` (`:4964`) and its comment stay, immediately after A8. Then
`var noChangeTimeoutCh chan struct{}` — **declared at the site before the segment** (A5's `deliver`
assigns it), and the post-wait `select` that reads it is unchanged.

**A10.** `waitWithSocketGrace` (`:5044`) onward is untouched. `watcher` and `sess` are the closure-assigned
outer variables, so it compiles as-is.

**A11.** Gate: `go build ./internal/... ./cmd/...` && `go vet ./internal/...` &&
`go vet -tags=scenario ./internal/daemon/`.

---

### Phase B — convert `dot_gate.go`

**B1.** Anchors: `grep -n 'sess, watcher, launchErr := runH.Launch' internal/daemon/dot_gate.go` (~:417)
and `grep -n 'readyErr := waitAgentReady' internal/daemon/dot_gate.go` (~:476).

**B2.** Predeclare above the tap (`:406`): `var sess handler.Session`, `var watcher *handlercontract.Watcher`,
`var launchErr error`, `var gateHBDone chan struct{}`.

**B3.** Hoist the adapter resolution (`:458-461`) above the segment; set `adapter = nil` on error, keeping
the stderr string byte-identical. **There is no `completionMode` here** — the gate is claude-pinned by
hk-01vs0 (`:458` hardcodes `core.AgentTypeClaudeCode`), so `SkipReadyHandshake: false`. Do not "improve" this.

**B4.** Build the segment. Same shape as A5, with these **site-specific differences**:

- `cfg.ReadyTimeout: effectiveAgentReadyTimeout(deps.agentReadyTimeout, deps.remoteAgentReadyTimeout, runner != nil)` — `runner`, not `rbc`.
- `onLaunchFailed`: body = `:419-421` **only** (`CloseHookSession` under the `deps.hookStore != nil` guard).
  **No `errors.Is` emissions** — the gate never had them (this is why the `errors` import is dropped).
- `onLaunched`: body, in order, = `:427-429` (held-back `launch_initiated`), `:436-439` (heartbeat loop;
  assign `gateHBDone`, do **not** `defer close` here), `:450-455` (`SetAgentReadyCallback` — note it uses
  plain `tap.Emit`, **not** `EmitWithRunID`; do not "fix" that).
- `deliver`: body = `:499-502` (`pasteInjectCognitionGate` + `pasteInjectQuitOnGateFile`).
- `killReady`: body = `:480-492` with **one** substitution — `time.After(agentReadyKillReapTimeout)` →
  `clockAfter(deps.clock, agentReadyKillReapTimeout)` (this is E5 step 16's one in-scope site). Keep
  `_ = sess.Wait(kctx)` unbounded — the gate does **not** have workloop's hk-4hso5 bounded wait, and adding
  it would be a logic change.
- `emitReadyTimeout`: body = `:493` verbatim (uses `ectx`, i.e. the gate's `ctx` semantics).
- `killAbort`: idempotent `sess.Kill(context.Background())` guard, per template.

**B5.** After `gateDispatch := gateSeg.run(ctx)`:

```go
if launchErr != nil {
    return nil, fmt.Errorf("cognition gate %q: launch: %w", gateRef, launchErr)
}
defer forceTeardownSession(sess)                                  // was :448
if gateHBDone != nil { g := gateHBDone; defer close(g) }           // was :440
if gateDispatch.Phase == runexec.DispatchFailed && gateDispatch.Reason == "agent_ready_timeout" {
    return nil, fmt.Errorf("cognition gate %q: agent_ready_timeout", gateRef)
}
```

Defers before the terminal check, same rule as A7.

**B6.** Delete `"errors"` from the import block; add `"github.com/gregberns/harmonik/internal/runexec"`.
Keep `"time"` (`pasteInjectQuitOnGateFile` still uses it).

**B7.** Gate: `go build ./internal/... ./cmd/...` && `go vet ./internal/...` &&
`go vet -tags=scenario ./internal/daemon/` && `go test ./internal/daemon/ -run 'DotGate|Cognition|Gate' -count=1`.

---

### Phase C — retire the dead ready-wait

**C1.** Prove it dead:

```bash
grep -rn 'waitAgentReady(' --include='*.go' internal/ cmd/ | grep -v '_test\.go' | grep -v 'agentready\.go'
# → MUST be empty
grep -rn 'newChanAgentEventSource' --include='*.go' internal/ cmd/
# → MUST be empty
```

**C2.** Re-point `internal/daemon/twinparity_timing_property_test.go`. Stage 1 of `observeAnomalies`
(`:186-194`) currently drives the real `waitAgentReady` to decide whether `agent_ready_timeout` fires.
Replace it with the same decision expressed directly, keeping the emitter call untouched:

```go
// Stage 1 — agent_ready. The real detector is now the dispatch segment's ready
// pump; the timing DECISION it encodes is "did a ready envelope satisfying
// adapter.DetectReady arrive within the band". Express that directly so the
// property still observes the REAL emitter (ExportedEmitAgentReadyTimeout).
if draw.AgentReadyDelay > agentReadyBand {
    daemon.ExportedEmitAgentReadyTimeout(ctx, emitter, runID, "twin-sid", agentReadyBand)
    return anomalyKindsIn(emitter.EventTypes())
}
```

Then delete `timingPropSource` (`:135-158`) and, if `timingPropAdapter` (`:120-133`) becomes unused,
delete it too — `go vet` will say. **Do not delete `delayedEventCh` or stage 2**;
`ExportedWaitPostAgentReadyProgress` is untouched by RT14.

> Call this out at review as the one place RT14 **reduces** coverage: the property no longer exercises a
> real goroutine race at the agent_ready edge. That race was an artifact of `waitAgentReady`'s wall-clock
> `select`; the machine resolves the same edge deterministically on a single goroutine
> (`runexec/dispatch.go:236-240`), so there is no race left to sample. If the reviewer disagrees, the
> honest alternative is to build a segment-level fixture in `dispatchsegment_test.go` — **not** to keep
> `waitAgentReady` alive for one test.

**C3.** `git rm internal/daemon/agentready_hkgql2018_test.go`. Coverage transfer, to be stated in the
commit body (each verified to exist):

| Deleted case | Replacement |
|---|---|
| `DetectReady` true → nil | `runexec.TestDispatch_HappyPath` (`internal/runexec/dispatch_test.go:60`) + `TestDispatchSegment_ResumeProbe_RunIDStampedReadyDelivers` (`dispatchsegment_test.go:216`) |
| no events → `ErrAgentReadyTimeout` | `runexec.TestDispatch_ReadyTimeoutSR9Edge` (`dispatch_test.go:124`) + `TestDispatchSegment_DotResume_ReadyTimeoutEdge` (`dispatchsegment_test.go:190`) |
| ctx cancel → `ctx.Err()` | `runexec.TestDispatch_AbortFromAnyNonTerminal` (`dispatch_test.go:227`); the shell maps cancel onto `EvAborted` (`runshell.go:370-375`) |
| boundary race (ready wins at timeout) | **no replacement, by construction** — see C2's note |

**C4.** Delete `internal/daemon/export_test.go:1421-1442` (`AgentEventSourceExported`,
`ExportedWaitAgentReady`). Leave `ExportedErrAgentReadyTimeout` (:1390-1393) and
`ExportedDefaultAgentReadyTimeout` (:1406-1410).

**C5.** Delete `internal/daemon/agentready.go:118-207` (`agentEventSource` + `waitAgentReady`). Rewrite
the file header (:3-30) — it currently documents only the deleted function. Replacement header:

```go
// agentready.go — the HC-056 agent_ready policy scalars.
//
// The WAIT itself no longer lives here. RT8 moved every launch/ready/brief
// segment onto the runexec Dispatch machine (dispatchsegment.go), whose
// ClockPort-timed TimerAgentReady is the one ready bound; RT14 converted the
// last two open-coded callers (single-mode beadRunOne, the cognition gate) and
// deleted waitAgentReady + agentEventSource with them.
//
// What stays is what the machine is CONFIGURED with, plus the sentinel the
// sites still name in their kill-reap logs:
//   - defaultAgentReadyTimeout / defaultRemoteAgentReadyTimeout
//   - effectiveAgentReadyTimeout  → every dispatchSegment cfg.ReadyTimeout
//   - ErrAgentReadyTimeout        → the killReady stderr lines
//
// Spec ref: specs/handler-contract.md §4.9 HC-056.
// Beads: hk-gql20.18 (origin), hk-5z1f0 / hk-96d7w (windows), P2 E5 RT14 (retirement).
```

**C6.** Delete `internal/daemon/workloopeventsource.go:185-230` (`chanAgentEventSource`,
`newChanAgentEventSource`, `Events`). Scrub the `waitAgentReady` prose at :3-23, :49-66, :90, :105-107,
:139, :203 — replace "waitAgentReady" with "the dispatch segment's ready pump" and keep the hk-37giq
fan-out rationale intact (it is still load-bearing: the ready pump and the quit watchdog still need
independent subscriptions). **`perRunEventTap` stays.**

**C7.** Write `scripts/readywait-freeze-gate.sh`. Modelled on `scripts/runmerge-freeze-gate.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

# readywait-freeze-gate.sh — P2 unit E5 RT14 freeze tripwire
# (plans/2026-07-21-p2-extraction/RT14-dispatchsegment-conversion.md §4 C7; _plan.md §3.2).
#
# The open-coded agent_ready WAIT left internal/daemon in slice RT14. Every
# launch/ready/brief segment now runs on the runexec Dispatch machine via
# dispatchSegment (dispatchsegment.go) — a ClockPort-timed, FakeClock-drivable
# bound. depguard cannot express "do not re-hand-roll a wall-clock wait", so this
# grep ratchet closes that door.
#
# NOT policed here (deliberately): the Working-phase watchdogs
# (pasteinject.go, dot_gate.go's pasteInjectQuitOnGateFile, waitsocketgrace.go,
# postreadyhang.go) still use raw wall-clock. dispatchsegment.go:17-20 places
# them OUTSIDE the RT8 segment boundary; slice RT19c owns them. Adding them here
# would make this gate red on landing.
#
# Exit 0: clean. Exit 1: the door was reopened.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

HITS=0

# (1) No re-declaration of the retired ready-wait symbols anywhere in the daemon
#     (recursive — a sub-package is the obvious evasion).
for sym in waitAgentReady agentEventSource chanAgentEventSource newChanAgentEventSource; do
    MATCHES="$(grep -rn --include='*.go' -E "^[[:space:]]*(func|var|const|type)?[[:space:]]*${sym}\b[[:space:]]*(=|struct|interface|func|\()" internal/daemon 2>/dev/null || true)"
    if [ -n "$MATCHES" ]; then
        echo "readywait-freeze-gate: FORBIDDEN re-declaration of ${sym} in internal/daemon:" >&2
        printf '%s\n' "$MATCHES" >&2
        HITS=$((HITS + 1))
    fi
done

# (2) The six run-path files that are wall-clock CLEAN today stay clean. The
#     dispatch path must remain FakeClock-drivable end to end.
for f in internal/daemon/dispatchsegment.go internal/daemon/runshell.go \
         internal/daemon/runbridge.go internal/daemon/reviewloop.go \
         internal/daemon/dot_cascade.go internal/daemon/workloopeventsource.go \
         internal/daemon/agentready.go; do
    if [ ! -f "$f" ]; then
        echo "readywait-freeze-gate: pinned file $f is gone — re-derive this gate's file list" >&2
        HITS=$((HITS + 1))
        continue
    fi
    N="$(grep -cE 'time\.(After|Now|NewTimer|NewTicker|Tick|Sleep)\(' "$f" || true)"
    if [ "$N" -ne 0 ]; then
        echo "readywait-freeze-gate: FORBIDDEN raw wall-clock in $f ($N site(s)) — use substrate.ClockPort / clockAfter:" >&2
        grep -nE 'time\.(After|Now|NewTimer|NewTicker|Tick|Sleep)\(' "$f" >&2
        HITS=$((HITS + 1))
    fi
done

# (3) beadRunOne stays wall-clock clean. Anchored on its two boundary symbols so
#     the range survives the line drift that E4c/RT15 will cause.
START="$(grep -n '^func beadRunOne' internal/daemon/workloop.go | head -1 | cut -d: -f1)"
END="$(awk -v s="$START" 'NR>s && /^func /{print NR; exit}' internal/daemon/workloop.go)"
if [ -n "$START" ] && [ -n "$END" ]; then
    MATCHES="$(awk -v s="$START" -v e="$END" 'NR>=s && NR<=e' internal/daemon/workloop.go \
               | grep -nE 'time\.(After|Now|NewTimer|NewTicker|Tick|Sleep)\(' || true)"
    if [ -n "$MATCHES" ]; then
        echo "readywait-freeze-gate: FORBIDDEN raw wall-clock inside beadRunOne (offsets from line $START):" >&2
        printf '%s\n' "$MATCHES" >&2
        HITS=$((HITS + 1))
    fi
else
    echo "readywait-freeze-gate: could not locate beadRunOne in workloop.go — re-derive this gate" >&2
    HITS=$((HITS + 1))
fi

# (4) The seam must still exist. A gate whose target was renamed away silently
#     stops testing what it claims to test.
if ! grep -q '^type dispatchSegment struct' internal/daemon/dispatchsegment.go; then
    echo "readywait-freeze-gate: dispatchSegment is gone — re-derive this gate" >&2
    HITS=$((HITS + 1))
fi

if [ "$HITS" -ne 0 ]; then
    echo "readywait-freeze-gate: FAIL — the open-coded agent_ready wait was retired in P2 E5 RT14; bind onto dispatchSegment, do not re-hand-roll it" >&2
    exit 1
fi
echo "readywait-freeze-gate: OK — the ready wait stays on the Dispatch machine"
```

```bash
chmod +x scripts/readywait-freeze-gate.sh && scripts/readywait-freeze-gate.sh
```

**C8.** Wire it into `Makefile` — append `scripts/readywait-freeze-gate.sh` after
`scripts/runmerge-freeze-gate.sh` in **both** `check-fast` (~:509) and `check-short` (~:537), and add the
`.PHONY: readywait-freeze-gate` target block after `runmerge-freeze-gate` (~:316), matching the six
existing blocks' comment style.

**C9.** No `.golangci.yml` change. **RT14 creates no package**, so there is no depguard block to add —
the `daemon` rule already allows the whole `internal/` prefix, and nothing new is being fenced. Say this
explicitly in the commit body so a reviewer does not read the absence as an omission.

---

## 5. Verification gate

Serialize these. **Do not run them concurrently with an agent fan-out or a parallel build** — that is
what manufactured five of the six failures in `00-test-oracle-baseline.md`.

**Pre-flight — the traps that invalidate the whole run:**

```bash
df -h /                      # MUST show ≥10 GiB free. Below the daemon's 10240MiB watermark it
                             # prints "disk-check: … dispatch paused" and every dispatch/throughput
                             # test fails for reasons unrelated to RT14 (baseline Addendum §3 —
                             # this is what wrecked P2's Phase-5 differential run).
git status --porcelain -- internal/daemon cmd/harmonik | head
                             # Another agent's dirty files get COMPILED into your test binary
                             # (baseline Addendum §4). Capture the differential from a CLEAN
                             # DETACHED WORKTREE at your own HEAD, never the shared tree.
```

| # | Command | Pass criterion |
|---|---|---|
| 1 | `go build ./internal/... ./cmd/...` | exit 0. **Never `go build ./...`** — the repo root is red regardless of RT14 (`plans/2026-07-15-agent-substrate-v2/investigate/10-zeromq-experiments/*.go` import unvendored zmq4/mangos) |
| 2 | `go vet ./internal/...` | exit 0 |
| 3 | `go vet -tags=scenario ./internal/daemon/` | exit 0. **Load-bearing:** 28 daemon test files are behind `//go:build scenario` and are never compiled by plain `go test`. Two of them — `scenario_remote_substrate_t4_claude_test.go`, `scenario_remote_substrate_localhost_dot_test.go` — name `waitAgentReady` in prose and drive the gate/remote ready paths RT14 rewrites. Also run `go vet -tags=e2e_real_claude ./internal/daemon/` and `go vet -tags=integration ./internal/daemon/` |
| 4 | `go test ./internal/runexec/... ./internal/runexectest/... -count=10` | exit 0. The RT11 fault-matrix + N=10 relaunch oracle is **the** behaviour-preservation gate for a Dispatch-machine change. `-count=1` is not sufficient |
| 5 | `go test ./internal/replay/... -count=1` | exit 0. The RT10 run-keyed replay checkers are what catch a "pure move" that silently reordered or renamed an emission — the exact failure mode RT14 risks |
| 6 | `go test ./internal/daemon/ -run 'AgentReady\|DotGate\|Cognition\|Gate\|SpawnPath\|ProcessExit\|HC056\|DispatchSegment\|Hkf6g7\|Hk3qjwl\|Hkx3s1p\|Hk4hso5' -count=1` | exit 0. The targeted set — cheap (~70s) inner loop before the full suite |
| 7 | `go test ./internal/daemon/... ./internal/harness/... -count=1 -timeout 25m` | **Differential green, not zero.** `00-test-oracle-baseline.md`: the suite is already red at HEAD. Procedure below |
| 8 | `go test -tags specaudit ./internal/specaudit/... 2>&1 \| grep -E '^--- FAIL' \| sort -u` | **Differential** against the same command at your pre-slice commit. specaudit is a **second red oracle** (7 failures at HEAD) and it is **not** compiled by `go build`, `go test`, or `golangci-lint`. Two of its sensors scan source by hardcoded path — none currently pin `agentready.go`/`workloopeventsource.go`, but confirm rather than assume: `grep -rn 'agentready\|workloopeventsource\|waitAgentReady' internal/specaudit/` |
| 9 | `golangci-lint run` | exit 0. This is where the orphaned `"errors"` import in `dot_gate.go` (B6) surfaces if missed |
| 10 | `scripts/readywait-freeze-gate.sh` | prints `readywait-freeze-gate: OK` |
| 11 | `make check-fast` | exit 0 — proves C8's Makefile wiring, and that the other six gates still pass |
| 12 | `ubs $(git diff --name-only HEAD \| grep '\.go$')` | exit 0 |
| 13 | `agent-reviewer` verdict on the commit | non-BLOCK; trailers applied (`_plan.md` §5.5) |

**Differential procedure for #7 (identical scope is mandatory):**

```bash
cd /Users/gb/github/harmonik
git worktree add /tmp/rt14-before <PRE_SLICE_COMMIT>
( cd /tmp/rt14-before && go test ./internal/daemon/... ./internal/harness/... -count=1 -timeout 25m 2>&1 ) \
  | grep -E '^--- FAIL' | sort -u > /tmp/rt14-before-failures.txt
# ...land RT14, then from a clean detached worktree at the RT14 commit:
go test ./internal/daemon/... ./internal/harness/... -count=1 -timeout 25m 2>&1 \
  | grep -E '^--- FAIL' | sort -u > /tmp/rt14-after-failures.txt
comm -13 /tmp/rt14-before-failures.txt /tmp/rt14-after-failures.txt   # MUST be EMPTY
```

**Both runs must use the identical package scope.** PROGRESS.md §"Phase 5 status" records that a
72-package "after" compared against a 1-package "control" produced 123-vs-20 failures and proved
nothing — the extra temp-dir churn from the wider scope drives the very disk metric the daemon gates
dispatch on.

**Triage rules for #7's after-set:**
- A name on the known-flaky allowlist (`00-test-oracle-baseline.md` §"Known-flaky allowlist") →
  **re-run IN ISOLATION**. Isolated failure = real regression; isolated pass = load artifact.
- `TestMergeToMain_RealConflictWithBeadsLedger_Escalates` is the **inverse** — it fails in isolation and
  passes in the full suite. Confirm it by re-running **in the full suite**. Applying the wrong procedure
  inverts the answer.
- `agentready_hkgql2018_test.go`'s four tests will vanish from the after-set. That is a **shrinking**
  set, which the oracle permits — but state it in the close comment so it is not mistaken for a silent
  loss (§4 C3 is the coverage-transfer argument).

**Measurement for the close comment:**

```bash
find internal/daemon -type f -name '*.go' ! -name '*_test.go' | wc -l
find internal/daemon -type f -name '*.go' ! -name '*_test.go' -exec cat {} + | wc -l
wc -l internal/daemon/workloop.go internal/daemon/dot_gate.go internal/daemon/agentready.go \
      internal/daemon/workloopeventsource.go internal/daemon/export_test.go
```

Expected: non-test files **−0** (`agentready.go` survives), non-test LOC **≈ −160**, test files **−1**,
test LOC **≈ −290**. **RT14 is not a LOC-reduction slice** — it is a *seam-uniformity* slice: it takes
the number of agent-launch sites in the daemon that hand-roll their own ready wait from **2 to 0**, and
the number of raw wall-clock sites on the dispatch path from **1 to 0**. Publish that number, not a LOC
delta, and say so plainly so the stream's metric is not read as a regression.

**§5.4 runtime proof is DEFERRED — the daemon is DOWN.** Per E5 §7.11 every slice must *flag* this
rather than omit it. When the daemon comes up, the proof for RT14 is: one single-mode (non-DOT) bead
through impl→review→merge, and one DOT run whose graph contains a **cognition** gate (note: the default
`workflow.dot` uses a tool-command `commit_gate`, so the gate path needs a deliberate fixture —
`dot_gate.go:373-374` records that no live remote run exercises it today).

---

## 6. Merge-conflict exposure

| File | Commits/90d | Exposure |
|---|---|---|
| `internal/daemon/workloop.go` | **331** | ~3.7 commits/day. RT14 rewrites 272 contiguous lines of `beadRunOne`. **Any concurrent commit inside 4600–5050 conflicts.** |
| `internal/daemon/export_test.go` | **229** | ~2.5/day, but RT14's edit is a 22-line deletion at a stable offset — low collision probability |
| `internal/daemon/dot_gate.go` | 21 | ~0.23/day — negligible |
| `internal/daemon/agentready.go`, `workloopeventsource.go`, `dispatchsegment.go` | cold | negligible |

**Window: aim for ≤4 hours from first edit to commit; treat 8 hours as the hard ceiling.** At 3.7
commits/day on `workloop.go`, an 8-hour window carries roughly a coin-flip chance of one concurrent
commit landing in the file, and a much smaller chance of it landing in the region.

**Must NOT run concurrently with RT14** (all edit `workloop.go` or its region):
- **RT13** — must be *committed* first. It cut lines 6397–8011; RT14's region is above that, so there is
  no textual overlap, but do not stack two uncommitted rewrites of the same file.
- **E4c** (`buildWorkerRegistry` → `internal/workers/bootwire.go`) — its target is `workloop.go:1135`.
  No overlap with 4600–5050, but it shifts every line number in this plan. **Land E4c first**, then
  re-derive anchors with the greps in A1/B1.
- **RT15** (`RunEnv`/`SharedHandles` + `beadRunOne` re-signature) — it rewrites ~40 `deps.*` reads
  *inside* 3167–5397, i.e. straight through RT14's region, and touches 156 test files. **RT15 must come
  after RT14**, never beside it. RT14 makes RT15 cheaper: `deps.agentReadyTimeout`,
  `deps.remoteAgentReadyTimeout`, `deps.clock`, `deps.adapterRegistry`, `deps.harnessRegistry` are all
  read once, at the `cfg`/segment construction, instead of scattered through 200 lines of imperative
  ready-wait.
- **RT19b** (the eleven surviving back-edges, incl. `clockAfter`, `agentReadyKillReapTimeout`,
  `emitAgentReadyTimeout`, `artifactAgentType`) — RT14 *adds* two more call sites to four of them.
  RT19b must be re-derived after RT14 lands.
- Any E5 slice that edits `reviewloop.go` or `dot_cascade.go` — RT14 does not edit them, but its
  correctness argument is "these two sites now look exactly like those two", and a reviewer will diff
  against them.

**Phase A is a legitimate stopping point.** If the window is closing, commit Phase A alone: the tree is
green, `waitAgentReady` still exists with one caller, and Phase B+C become RT14b. Do **not** stop between
A5 and A7 — a segment whose cleanup defers are unregistered is exactly the half-wired intermediate
`_plan.md` §3 forbids.

---

## 7. Risks

1. **The cleanup-defer reordering (A7) is the one that can silently break production.** The four defers
   currently sit *before* the ready wait, so they fire on the `agent_ready_timeout` return. In segment
   form they can only be registered *after* `seg.run()`. Register them before the terminal check and LIFO
   order is preserved exactly; register them after and the timeout path stops tearing down the session,
   stops emitting presence-offline, and leaks the heartbeat goroutine — **and no test will catch it**,
   because the ready-timeout path is exercised only by `workloop_hc056_reopen_test.go`, which asserts on
   the reopen, not on teardown. *Mitigation:* A7 states the ordering as a hard constraint with the two
   in-tree precedents; the reviewer is told to check this line specifically; and the differential run
   includes the `-tags=scenario` remote-substrate files that drive the timeout path end-to-end.

2. **A wrong deletion here is unrecoverable in a way a wrong move is not.** E5 §4 step 17 instructed
   `rm internal/daemon/agentready.go`, and §1 asserted a single remaining reference. Both are false —
   four of the file's six symbols are live in production, and the `daemon.go` references that make the
   file *look* dead are **comments**, which is precisely how a `grep -c`-driven deletion goes wrong.
   *Mitigation:* §3c gives the exact comment-and-string-filtered grep and its measured verdict; §0 states
   the corrected scope; C1 makes emptiness a hard precondition. **Do not delete anything Phase C does not
   name.**

3. **Coverage genuinely shrinks by one property.** `twinparity_timing_property_test.go`'s stage-1
   boundary race disappears with `waitAgentReady`. The honest defence is that the race was an artifact of
   a wall-clock `select` on two channels, and the machine resolves the same edge deterministically on one
   goroutine — but it *is* a reduction and must be argued at review, not buried. *Mitigation:* C2 states
   it explicitly and names the alternative (a segment-level fixture) rather than the wrong answer
   (keeping dead production code alive for one test).

4. **The two hoists (A3, B3) move a stderr emission earlier.** `ForAgent(...): skipping ready-wait` now
   prints before Launch. No bus event, nothing the RT10 replay checkers observe, and both templates
   already took this divergence — but it is a real ordering delta and must be named in the commit body,
   or the pure-move review rejects the diff on discovery. *Mitigation:* A3's blockquote.

5. **`dot_gate.go` is the site with the least test coverage and the least production traffic.**
   `dot_gate.go:373-374` records that the default `workflow.dot` uses a tool-command `commit_gate`, so
   **no live run exercises the cognition gate today** — a regression here would be latent until someone
   authors a cognition-gate graph. *Mitigation:* verification step 6 runs the `DotGate|Cognition|Gate`
   filter; step 3's `-tags=scenario` vet compiles `dot_gate_reviewer_harness_hk01vs0_test.go`; and B4
   enumerates every site-specific difference from the `dot_cascade` template so the conversion is not
   done from memory.

6. **Four hooks capture outer variables assigned by a *different* hook.** `deliver` reads `sess`;
   `killReady` reads `sess` and `watcher`, both assigned by `launch`. This is safe only because
   `RunDispatch` drives every effector inline on the caller's goroutine (`runshell.go:376-388`) — it is
   **not** safe under any future change that moves effectors onto their own goroutine.
   *Mitigation:* the three existing consumers already depend on this identical property, so RT14 adds no
   new constraint; but the commit body should name it so a future reactorization knows what it is
   breaking.

7. **`-race` is where an ordering mistake shows up, and `check-short` runs it with `-p=1`.** The segment
   introduces two helper goroutines per dispatch (`readyPump`, `watchWatcherExit`) at a site that
   previously had one (`waitAgentReady`'s observer). All are released by `segCancel` on `run()` return
   (`dispatchsegment.go:111-112`). *Mitigation:* verification step 4's `-count=10` on
   `internal/runexectest` is the relaunch oracle for exactly this class; run `make check-short` before
   the close comment.

8. **RT14 does not shrink the daemon**, which makes it look like a wasted slice against P2's published
   LOC metric. *Mitigation:* §5's "Measurement" paragraph gives the correct metric (hand-rolled ready
   waits 2→0, dispatch-path wall-clock sites 1→0) and instructs it be published instead of a LOC delta.

---

## 8. Documents to update when RT14 lands

- `plans/2026-07-21-p2-extraction/E5-dot-runloop.md` — rewrite §4 steps 14–18 per §0 of this file; mark
  the `waitAgentReady` row in §1's table **RESOLVED (converted, not moved)**; correct §3a's
  "`time.After`/`time.Now`/`time.NewTimer` on the run path" row to record that the eight-site count
  conflated the segment with the Working-phase watchdogs, and file the remaining seven as **RT19c**.
- `plans/2026-07-21-p2-extraction/PROGRESS.md` — add RT14 to the slice table; note the metric is
  seam-uniformity, not LOC.
- `plans/2026-07-21-p2-extraction/README.md` — the "Shippable slices" table's `E5 RT14→RT19` row splits:
  **RT14 is landable without E1/E4**, unlike RT15+.
- `plans/2026-07-21-p2-extraction/00-test-oracle-baseline.md` — the four `agentready_hkgql2018` tests
  leave the corpus; record it so the next differential run does not read the shrinkage as a mystery.
