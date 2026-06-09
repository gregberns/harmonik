# Postmortem: The Concurrent-Dispatch Wedge (hk-37giq / tapCh competing-consumer race)

**Incident:** 2026-06-08 → 2026-06-09 (~7 hours of active diagnosis)
**Fix:** commit `53ead2aa` (bead `hk-37giq`, "tap fan-out")
**Backstop:** commit `7641f648` (bead `hk-jgxqc`, launch-suppression ceiling — time-bounds the spin only)
**Agents involved:** `captain` (orchestrator), `named-queues` (daemon lane owner), `controlpoints` (drove the fix), `flywheel`, `codexcrew`

> This is the motivating incident for the `validation-net` kerf work. A latent, concurrency-only daemon bug
> survived ~2 weeks because **no scenario / high-level test exercised concurrent real-bead dispatch.**

---

## 1. TL;DR

Dispatching 2+ real beads concurrently through the harmonik daemon stalled them at launch: each run hit
`launch_stall_detected → run_stale` and never advanced `implement → merge`, even after the implementer had
already committed. The root cause was a **competing-consumer race on a single per-run event channel (`tapCh`)**
at `internal/daemon/dot_cascade.go:549`: one channel was received by two exclusive consumers — `waitAgentReady`
and the `pasteInjectQuitOnCommit` watchdog — and a Go channel receive is exclusive, not broadcast, so under
concurrency `waitAgentReady`'s hot drain goroutine starved the watchdog of every heartbeat. The fix (`53ead2aa`)
turns the per-run tap into a true **fan-out** where each consumer gets its own buffered subscription. Validated
by a 3-concurrent real-bead smoke: **1** launch-suppression line vs. the **217** observed while wedged,
`active_run_count` reached 3, and all three runs recovered from `run_stale` and spawned.

---

## 2. Timeline (absolute UTC, from the comms bus)

The night was **two stacked sagas**. The first (the "no-spawn / spawn-stall" saga) was a real but *different*
family of bugs; fixing it revealed the tapCh wedge underneath.

- **06-08 ~22:33–23:48Z** — first real-bead concurrent dispatch (captain T2 `hk-w6y70`) **wedges at `launch_stall`
  with no implementer spawn**. Disk-full re-attribution disproven (healthy 28Gi disk, green main).
- **06-09 00:02Z** — `hk-4l7zs` spawn-semaphore slot-leak theory disproven: a fresh daemon wedges on its *first*
  dispatch; **zero `spawn_cap_blocked` events ever fired**.
- **01:46–02:03Z** — spawn-saga root cause found: `EnsureWorktreeTrust` (`claudetrust_wm040b.go:118`) blocking on
  an unbounded `LOCK_EX` flock over a bloated `~/.claude.json` (8.6MB, 36.6k leaked trust keys). Pruned 9.1→1.9MB;
  durable fixes `hk-bfvby` (`5282816c`) + `hk-r1rup` (`ec30b225`) + `hk-oihnf` (`7cc0f0bc`). Spawn saga declared
  resolved at `-c6`.
- **06:43–07:36Z** — the wedge **returns under real concurrent beads**, but now runs *recover past launch and
  commit* (T2/T7 → `8515c729`, `b3481dbe`) then wedge **post-commit** in `pasteinject.go` — "217× launch-heartbeat
  -timeout suppressed".
- **07:44–07:47Z** — WORKING BASELINE pinned `--max-concurrent 1` (serial). Wedge declared concurrency-only and
  **LATENT, not a regression**. An independent verifier **refutes** the suppression-ceiling hypothesis.
- **08:21–09:03Z** — live-smoke of `7641f648` (ceiling fix) **still wedges** (`hk-o50hy` → `run_stale`, last event
  `node_dispatch_requested`, 627s, `goroutine_count=21`). The ceiling targets a branch that fires 0×.
- **10:31–10:48Z** — a **12-agent fan-out + 2 adversarial verifiers that OVERRULED a wrong synthesis** RECONCILES:
  no regression; **three cleanly-separated latent bugs**, the real wedge being the `tapCh` competing-consumer race.
- **11:12–11:23Z** — fix `53ead2aa` lands; lane owner `named-queues` gives an **independent fresh-context APPROVE**.
- **11:19–12:02Z** — redeploy + 3-concurrent real-bead live-smoke: all three recovered from `run_stale` and
  spawned (`active_run_count=3`); **CONCURRENCY WEDGE FIXED**. Remaining failures are downstream (`commit_gate` /
  no-progress), not the wedge.

---

## 3. The real root cause — the tapCh competing-consumer race

At `internal/daemon/dot_cascade.go:549`, `newPerRunEventTap` produced **one** synthetic-event channel (`tapCh`)
consumed by **two** independent goroutines:

1. `newChanAgentEventSource → waitAgentReady` — the launch-readiness poller, with a drain goroutine on `<-tapCh`.
2. `pasteInjectQuitOnCommit` — the post-commit `/quit` watchdog (`internal/daemon/pasteinject.go`), watching for
   `firstHeartbeatSeen`.

**A Go channel receive is exclusive, not broadcast.** Each `agent_heartbeat` went to exactly one receiver. Serial
dispatch was harmless (the lone `waitAgentReady` goroutine goes dormant after `readyCancel()`). Under **2+
concurrent runs**, `waitAgentReady`'s goroutine stayed hot and consumed *every* heartbeat, so:

- The watchdog never observed `firstHeartbeatSeen` (`pasteinject.go:552/:624`).
- Its launch-verification branch (`:679`) saw the pane still had an active child → **reset `launchDeadline`
  without bound** (`:695`), emitting `"launch-heartbeat-timeout suppressed"` every window — **217×** observed.
- `sess.Wait` never unblocked → workflow never advanced `implement → merge`. Surfaced as
  `launch_stall_detected → run_stale`, holding the slot forever.

---

## 4. The six refuted hypotheses

| # | Hypothesis | Why it was wrong |
|---|---|---|
| 1 | `hk-4l7zs` spawn-semaphore slot-leak | A *fresh* daemon wedged on its **first** dispatch; **zero `spawn_cap_blocked`** events fired. |
| 2 | Disk-full / ENOSPC | Reproduced on healthy 28Gi disk, green main. |
| 3 | `hk-r1rup` unbounded `tmux new-window` / `hk-oihnf` new-window mutex | Real downstream fixes, but wedge persisted after both landed; no new-window timeout fired. |
| 4 | `hk-bfvby` trust-flock / `CLAUDE_CONFIG_HOME` isolation | Fixed the *spawn-stall* saga; wedge returned post-fix with runs recovering past launch + committing. |
| 5 | `hk-9vp51` session-nesting / merge serializer / reviewer-launch contention | The 12-agent fan-out OVERRULED it; `named-queues` itself called the file:line "MOOT". A revert would have dropped the `hk-9vp51` P0 without fixing anything. |
| 6 | `7641f648` launch-suppression-ceiling is the fix | The ceiling targets `pasteinject.go:658`, which fires 0× for these runs; live-smoke still wedged. Kept as defence-in-depth only. |

---

## 5. The fix (`53ead2aa`)

`perRunEventTap` no longer hands out a single shared channel. It holds a slice of subscriber channels
(`workloopeventsource.go:80`); `Subscribe()` returns a new independent cap-64 buffered channel; `fanOut()` writes
a **copy** of every envelope to **each** subscriber, **non-blocking, drop-if-full per channel**. The two competing
call sites (`workloop.go` single-mode, `dot_cascade.go:549` DOT-mode) now give the watchdog its **own**
`tap.Subscribe()` channel. Neither consumer can steal the other's events.

**Regression test** (`internal/daemon/workloopeventsource_hk37giq_test.go`):
`TestPerRunEventTap_FanOut_PassiveSubscriberNotStarvedByAggressiveDrain` — two subscribers, A drained by a hot
goroutine, asserts passive B still receives all 50 heartbeats. Fails on the old design (0/50), passes on fan-out
(50/50), clean under `-race`. **This is a unit-level guard on the tap primitive — NOT an end-to-end
concurrent-dispatch-reaches-merge scenario. The end-to-end gap remains open (see §6).**

**Validation:** 3-concurrent real-bead smoke → 1 suppression line vs 217; `active_run_count=3`; all three recovered
from `run_stale` and spawned; lane-owner independent APPROVE.

---

## 6. Why it stayed latent ~2 weeks — THE prevention lesson

The wedge **predated every revertable commit by ~2 weeks.** A rollback could not have fixed it. It hid because it
is **strictly concurrency-only AND requires real work:**

- **Single-bead** dispatch never triggered it (lone drain goroutine goes dormant).
- **Trivial doc / single-happy-path smokes** never triggered it (they commit/quit before the competing-consumer
  window matters) — they masked the bug **3×** across the session.
- It only surfaces with **2+ real concurrent beads** that run long enough for both consumers to be live.

**The coverage gap this whole exercise is about:** no scenario or high-level test ever exercised **concurrent
real-bead dispatch through the real spawn/heartbeat/watchdog wiring.** The entire suite validated single-bead or
trivial twin smokes. Even the existing N=2 concurrency scenario (`scenario_concurrent_multiqueue_hkumemp`) uses the
`single-happy-path` twin, which short-circuits the very heartbeat/launch interplay the bug lived in. The fix's
narrow unit test guards the tap primitive but **not** the end-to-end path — so the structural gap that allowed the
2-week hide is **still open.** Closing it is `validation-net`'s flagship deliverable.

---

## 7. Residual / follow-up issues (open beads)

- **`hk-goczd` (P2)** — launch-serialization slowdown: `newWindowMu` (`tmuxsubstrate.go:627`) serializes tmux
  new-window → N concurrent beads launch ~1-at-a-time, each emitting a ~57s `launch_stall_detected` then
  recovering. Concurrency *works* but launches slowly. Do NOT reflexively re-pin `-c1`.
- **`hk-pj4b6` (P1)** — commit_gate loop has no escape: no-commit re-entry + **discarded gate output**
  (`cmd.Run()` at `dot_cascade.go:792`) loops to the DOT traversal cap with nobody able to see why. Highest-value
  hardening ("it's why diagnosing tonight took 12 agents").
- **`hk-6ra3p` (P2)** — `TestReviewLoopBridge_CHB009` flaky under `-short`; can flake the gate.
- **commit_gate `-short` question — ANSWERED:** the gate DOES run `go test -short` (scenario-gate.sh affected-unit
  step), so the `hk-p258q` quarantine *is* in effect. `make check` (Tier 2) runs *without* `-short`. The captain-bead
  failures (`hk-zblnu`/`hk-yj2j6`) are therefore a *different* gate failure (hk-pj4b6 loop / flaky / vet), not the
  quarantine.
- **Restore the 21 quarantined real-daemon E2E tests** in a separate non-merge-blocking CI lane (bead not yet filed).
- **`hk-jgxqc`** can be closed as fixed-by-`hk-37giq` once the smoke fully merges.

---

## 8. Process lessons

1. **Refuting by reasoning instead of by a reproducing test cost the most time.** Six "definitive" root causes
   declared and refuted; the bug never had a unit-level reproducer until `53ead2aa` shipped one alongside the fix —
   backwards from "reproduce before fix."
2. **Trivial smokes gave false "validated" signals — 3×.** Concurrency fixes need **2+ real concurrent beads under
   load**, never a single-bead or doc smoke.
3. **Discarded gate output blinded diagnosis (`hk-pj4b6`).** `commit_gate`'s `cmd.Run()` swallowed stdout/stderr, so
   a separate bug (deterministic `internal/daemon` reds looping the gate to the cap) was invisible and silently
   blocked the captain plan all session — masquerading as part of the wedge.
4. **`run_stale ≠ dead`** was mis-read twice; a slow-but-progressing run also goes stale. Verify
   `implementer_phase_complete` before declaring a wedge.
5. **What finally worked: the 12-agent fan-out + 2 adversarial verifiers that could OVERRULE a wrong synthesis.**
   Matches the project's "major-issue fan-out protocol."
6. **Lane ownership + independent review-gate held** — the lane owner gave a separate fresh-context APPROVE before
   deploy, not a rubber stamp.

---

**Key references:** fan-out `internal/daemon/workloopeventsource.go:80,86,111,122,130`; shared-tap origin
`dot_cascade.go:549`; watchdog `pasteinject.go:366-398,552,624,679,695`; regression test
`workloopeventsource_hk37giq_test.go`; backstop `7641f648`; fix `53ead2aa`; validation signatures
(`active_run_count=3`, runs `019eac1c-9c33/-9e92/-a11f`) in `.harmonik/events/events.jsonl`.
