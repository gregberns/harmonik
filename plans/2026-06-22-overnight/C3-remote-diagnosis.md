# C3 — Remote-run failure diagnosis (gb-mbp)

**Author:** logmine (crew, hk-mhmaw charter) · **Date:** 2026-06-22 ~07:00Z · **Mode:** READ-ONLY (event-bus only; worker NOT enabled, routing NOT re-tested, per captain scope).
**Question (captain assign 06:47Z):** operator parked pointing the daemon at gb-mbp because remote runs fail fast (hk-h106 failed 28s in, hk-icdz failed 6×). Routing is NOT broken (116 gb-mbp selection events). Find the common run_failed signature + root cause.

## TL;DR — the root-cause signature, and the twist
**Dominant signature: `implementer agent_ready_timeout at iteration 1`** (the remote claude launches but never signals `agent_ready` within the ~90s deadline). One lone variant: `fetchRunBranchBoxA … couldn't find remote ref` (hk-h106).

**The twist: every parked failure PREDATES the fix that targets its exact signature.** No remote run has been attempted on a daemon carrying the complete remote-substrate fix stack. The "fail-fast" the operator parked on is a **STALE signature** — this is the merge-base / stale-base trap, not a confirmed live bug. The honest call is **re-validate on current main**, not "still broken."

## The failure signatures (from run_failed payloads)
| Bead | Time (UTC) | Lifetime | run_failed summary |
|---|---|---|---|
| hk-icdz ×~6, hk-3zij, hk-tzfw, hk-xbpm | 06-20 22:11Z → 06-21 01:42Z | ~90–93s | `implementer agent_ready_timeout at iteration 1` / `dot: agentic node "implement" … agent_ready_timeout` |
| hk-icdz (1×) | 06-21 02:03Z | 7s | `no_commit_during_implementer: HEAD did not advance … exit=0` |
| hk-h106 | 06-21 03:47Z | 28s | `fetch run branch origin→box-A before reviewer: codesync: fetchRunBranchBoxA … exit status 128 — git: couldn't find remote ref run/<rid>` |

27 remote-signature `agent_ready_timeout` failures total in the bus, **first 05-26, last 06-21 03:47Z. Zero after 06-21 04:45Z.**

## Why agent_ready_timeout (event-sequence evidence)
A remote run's bus trace (hk-icdz 22:10Z) is:
```
run_started → harness_selected → handler_capabilities → skills_provisioned
            → launch_initiated → agent_heartbeat → [~90s SILENCE] → agent_ready_timeout → run_failed
```
The daemon DOES initiate the launch (`launch_initiated` + a heartbeat fire), then nothing for ~90s. So claude is dispatched to the worker but **its `agent_ready` hook never relays back to box-A** before the deadline. This is the documented **SSHRunner `#{pane_id}` comment-truncation** failure mode: unquoted tmux argv → the worker login shell reads `-F #{pane_id}` as a `#` comment → `tmux new-window` is truncated → no claude window opens → agent_ready_timeout. (Memory: "remote agent never launches … no claude window … agent_ready_timeout".)

## The smoking gun: failures straddle their own fixes
Fix commit times (UTC; real deploy is the **next daemon restart after** each):
| Commit (UTC) | Fix | Targets signature |
|---|---|---|
| 06-20 21:20Z | `3c5f8121` reverse tunnel uses TCP loopback so worker hook can relay agent_ready (hk-ege6) | agent_ready_timeout |
| 06-21 00:08Z | `9c461383` thread substrate-spawn runner + worker hook socket into review-loop/DOT (hk-fxy9/538l) | agent_ready_timeout |
| **06-21 01:58Z** | **`61ea9883` shell-quote SSHRunner remote argv so tmux `#{pane_id}` survives** (hk-fxy9/538l) | **agent_ready_timeout (the dominant one)** |
| **06-21 04:45Z** | **`8b6bcece` box-A fetches run branch DIRECTLY from worker over SSH** (hk-7bwx) | **hk-h106's couldn't-find-remote-ref** |
| 06-22 03:47Z | `082fddc4` retry fetchRunBranchBoxA on transient ref-not-found (hk-zsn7) | hk-h106's fetch race |

Cross-referenced against the failure timeline:
- The **agent_ready_timeout cluster ends 06-21 01:42Z** — i.e. *before* `61ea9883` (01:58Z commit, deployed only at a later restart). Those runs ran on builds **without the SSHRunner-quote fix**, the exact root cause of their signature.
- **hk-h106's fetch-ref failure (03:47Z)** is *before* `8b6bcece` (04:45Z). It tried to fetch the run branch from `origin` (where the worker never pushes) → "couldn't find remote ref" — precisely what `8b6bcece` (+`082fddc4` retry) rewrites to a direct-from-worker SSH fetch.
- **Remote failures after the full-fix commit floor (04:45Z 06-21): 0.** The operator parked at ~03:47Z, before a daemon carrying the full stack ever existed.

## Root cause (answer to the captain)
The parked decision rests on **pre-fix failures**. Two real bugs caused them — both already fixed:
1. **SSHRunner unquoted `#{pane_id}` → no claude window → `agent_ready_timeout`** → fixed by `61ea9883` (+ the `9c461383`/`3c5f8121` relay substrate threads).
2. **box-A fetched the run branch from `origin` not the worker → "couldn't find remote ref"** → fixed by `8b6bcece` (+ `082fddc4` retry).

The `no_commit … exit=0` (7s) is a downstream symptom of the same broken launch path (agent process exits immediately without a working seed).

## Recommendation
**Do NOT treat remote-substrate as broken on the evidence the park was based on — it is stale.** The correct next step (when the operator chooses to un-park) is a single clean re-validation of `hk-rs-validate-remote-898a` (or hk-icdz) on a daemon built from **current main** (carrying `61ea9883` + `8b6bcece` + `082fddc4`). The bus alone cannot prove current main *succeeds* remotely — only that all 27 parked failures predate the fixes for their signatures. This is the stale-base trap (cf. the prior `484ce09f` re-fix-of-an-already-landed-bug incident).

> **Caveat:** deploy-time is inferred from commit-time + "next restart"; the running daemon during the failures may have lagged the commits further — which only *strengthens* the stale conclusion. No claim is made that current main works remotely; that needs the operator's one re-validation run.
