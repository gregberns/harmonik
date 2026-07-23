# RT19c — clock-port the Working-phase watchdogs

**Status:** **READY TO PLAN** — descoped out of RT14, not yet recipe-grade. No operator decision
required; no new seam invented.
**Filed by:** RT14 (2026-07-22), which closed the dispatch path's last raw wall-clock site and
proved these were a separate concern.
**Depends on:** nothing hard. RT14 is landed, so `substrate.After` / `substrate.ClockPort` are
already threaded through `internal/daemon` and the conversion idiom is established at four sites.

---

## 1. Why this exists as its own slice

E5-dot-runloop.md §4 step 16 told RT14 to "delete the **eight** raw-time sites" across
`dot_gate.go`, `waitsocketgrace.go` and `postreadyhang.go`. That instruction **conflated two
different concerns**, and RT14 descoped seven of the eight after verifying the split:

- **One site was genuinely in RT14's blast radius** — `dot_gate.go`'s
  `time.After(runlaunch.KillReapTimeout)` inside the cognition-gate ready-timeout kill block. It sat
  *inside the dispatch segment*, converted for free as part of the segment binding, and RT14 phase B
  did it (and deleted the `--exclude='dot_gate.go'` carve-out in `scripts/runlaunch-freeze-gate.sh`
  that existed only for it).
- **The rest are Working-phase watchdogs.** They run *after* the segment has reached Working.
  `internal/daemon/dispatchsegment.go`'s header is explicit that the Working-phase completion wait
  (`waitWithSocketGrace` + the frozen-commit watchdog) stays with the sub-driver's imperative control
  flow **until the M5-adjacent full reactorization**. They are outside the RT8 segment boundary by
  design, not by omission.

Converting one copy of a watchdog without its siblings would **diverge three implementations of the
same pattern** and close nothing. Hence: one slice that takes all of them together.

RT14's `scripts/readywait-freeze-gate.sh` deliberately does **not** police these files. Adding them
to that gate is RT19c's closing move, not something to do earlier — it would make the gate red.

---

## 2. Measured inventory (2026-07-22, post-RT14)

`grep -cE 'time\.(After|Now|NewTimer|NewTicker|Tick|Sleep)\('` per file:

| File | Sites | Notes |
|---|---:|---|
| `internal/daemon/pasteinject.go` | **22** | the bulk; see per-function split below |
| `internal/daemon/dot_gate.go` | **6** | **all six inside `pasteInjectQuitOnGateFile`** (currently ~:811-868). The file's other wall-clock use left with RT14 phase B. |
| `internal/daemon/waitsocketgrace.go` | **1** | the stop-hook grace window |
| `internal/daemon/postreadyhang.go` | **1** | the post-agent_ready progress bound |
| **Total** | **30** | |

`pasteinject.go` per function:

| Function | Sites |
|---|---:|
| `pasteInjectQuitOnCommit` | 12 |
| `pasteInjectQuitOnReviewFile` | 7 |
| `splashDismissWait` | 1 |
| `sendSubmitEnterWithRetry` | 1 |
| `injectAndVerifySeed` | 1 |

> **Every line number in this document will be stale by the time you read it.** This has bitten three
> consecutive P2 slices. Locate every symbol with `grep -n`; re-derive the counts above before
> planning, and treat a mismatch as new information rather than an error.

---

## 3. The three sibling watchdogs

The reason this is one slice and not four: `pasteInjectQuitOnCommit` (pasteinject.go),
`pasteInjectQuitOnReviewFile` (pasteinject.go) and `pasteInjectQuitOnGateFile` (dot_gate.go) are the
**same watchdog shape** specialised per phase — poll for a completion signal (a commit, a review
file, a gate-verdict file), extend a budget on heartbeat evidence, then `/quit` and kill on a grace
timer. They share the `briefDelivered` gate, the no-change kill delay and the post-quit kill grace.

Convert one and the three drift. Convert all three and the pattern stays legible — and a future M5
reactorization has one shape to lift, not three.

---

## 4. What "clock-port" means here

The idiom is already established at four sites by RT8/RT14. Substitutions:

```
time.After(d)        -> substrate.After(clk, d)
time.NewTicker(d)    -> clk.NewTicker(d)          // .C() for the channel, .Stop() as before
time.Now()           -> clk.Now()
time.Sleep(d)        -> <-substrate.After(clk, d) // with a ctx.Done() select where a ctx exists
```

`clk` is `deps.clock` (`substrate.ClockPort`). The four existing consumers all carry the
`if deps.clock == nil { deps.clock = substrate.SystemClock{} }` backstop for struct-literal test deps
(`reviewloop.go`, `dot_cascade.go` ×2, `dot_gate.go`); any function RT19c gives a clock dependency
needs the same treatment, or its callers must be shown to always come through one that has it.

**The hazard:** most of these functions do **not** currently take `deps`. `pasteInjectQuitOnCommit`,
`pasteInjectQuitOnReviewFile` and `pasteInjectQuitOnGateFile` take an explicit argument list.
Threading a clock in is a **signature change** — which is exactly why this could not ride inside
RT14, where `_plan.md` §5.1 forbids logic and signature deltas inside an extraction. RT19c is not an
extraction, so it may change signatures; it must still not change behaviour.

Decide up front and record it: add a `clk substrate.ClockPort` parameter, or pass the `deps`/ports
bundle. **Check whether RT15 (`RunEnv`/`SharedHandles`) lands first** — if it does, the ports bundle
already exists and threading it is nearly free. Sequencing RT19c after RT15 is probably the cheaper
order; confirm before starting.

---

## 5. Why it is worth doing

Not LOC. The payoff is the same as RT14's:

- **These watchdogs are the daemon's longest waits** — `gateFileTimeout` is 10 minutes, the reviewer
  budget runs to a 60-minute ceiling. Every test that wants to exercise a timeout branch either waits
  real wall-clock time or cannot reach it at all. On a ClockPort a FakeClock drives them instantly.
- **`pasteInjectQuitOnCommit` is implicated in the concurrent-dispatch wedge history** (hk-37giq,
  hk-jgxqc, hk-trjef, hk-7srrd — see the comments in `workloop.go`'s deliver hook). A wedge class
  whose timing cannot be driven deterministically is a wedge class that gets debugged in production.
- It closes the last raw-wall-clock region in `internal/daemon`'s run path, which lets
  `readywait-freeze-gate.sh` widen from six pinned files to the whole run path.

---

## 6. Suggested shape

Three commits, each green, mirroring RT14's phasing:

1. **RT19c-1 — the two small ones.** `waitsocketgrace.go` (1) + `postreadyhang.go` (1). Both are
   small, self-contained, and already reached from call sites that have `deps`. Proves the threading
   decision from §4 on the cheapest possible surface.
2. **RT19c-2 — the three sibling watchdogs.** `pasteInjectQuitOnCommit`,
   `pasteInjectQuitOnReviewFile`, `pasteInjectQuitOnGateFile` (25 sites). The signature change lands
   here. All three together — that is the whole point of the slice.
3. **RT19c-3 — the stragglers + the ratchet.** `splashDismissWait`, `sendSubmitEnterWithRetry`,
   `injectAndVerifySeed` (3 sites), then **add the four files to
   `scripts/readywait-freeze-gate.sh` check (2)** and delete the "NOT policed here (deliberately)"
   paragraph in its header. That paragraph names RT19c by name; deleting it is the slice's
   completion signal.

---

## 7. Verification gate

Same as RT14's (`RT14-dispatchsegment-conversion.md` §5), plus:

- `go test ./internal/daemon/ -run 'PasteInject|QuitOnCommit|QuitOnReview|QuitOnGate|SocketGrace|PostReadyHang' -count=1`
- **A new test that could not exist before:** drive one watchdog timeout branch on a FakeClock and
  assert it fires without real elapsed time. If RT19c lands without at least one such test it has
  paid the cost of the conversion and collected none of the benefit.
- `scripts/readywait-freeze-gate.sh` must still print OK at every commit, and after RT19c-3 must
  **fail** if a raw `time.After` is reintroduced into any of the four files. Prove it in both
  directions, as RT14 did.
- The two known-red daemon tests (`TestThroughput_TenBeadsAtMaxFour`,
  `TestM4C7_D2Chokepoint_IsHarnessAgnostic`) are pre-existing — differential green, not zero.

---

## 8. Risks

1. **Signature churn reaches test files.** The three watchdogs are called from `workloop.go`,
   `reviewloop.go`, `dot_cascade.go` and `dot_gate.go` and are exercised by a number of daemon tests.
   Threading a clock is mechanical but wide. Landing after RT15 shrinks this considerably.
2. **`time.Now()` inside a polling loop is a deadline computation, not a wait.** `pasteInjectQuitOnGateFile`
   does `deadline := time.Now().Add(gateFileTimeout)` then compares `time.Now().After(deadline)` each
   tick. Both must move together or the loop compares a fake clock against a real deadline and never
   terminates. Convert per *loop*, not per *call site*.
3. **These are the least test-covered paths in the daemon**, and `dot_gate.go`'s copy has **no live
   production traffic at all** (the default `workflow.dot` uses a tool-command `commit_gate`, so no
   run exercises the cognition gate). A regression there is latent until someone authors a
   cognition-gate graph. Weight review accordingly.
4. **Do not "fix" behaviour while converting.** The three watchdogs have accumulated
   individually-tuned budgets and grace windows (hk-cmybm, hk-930o3, hk-trjef, hk-7srrd, hk-jgxqc,
   hk-sj6a, hk-e7n76). They look inconsistent because they *are* inconsistent, deliberately. Preserve
   each one exactly; a unification pass is a separate, reviewable change.
