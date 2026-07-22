# Remote worker concurrency — investigation plan

> **Status:** OPEN / investigation not yet started. This doc captures *the basics of what's going
> on* so a proper look can start. **This is a blocker on running more volume** (see "Why it matters").
> Created 2026-07-11 (admiral, operator-directed).

## Why it matters (the operator's goal)

The operator wants to sustain **~9 concurrent runs** — roughly **3 Pi (DGX/ornith provider) + 4 Claude
+ 2 codex** — which the local box cannot hold. That volume *requires* the remote worker substrate
(`gb-mbp`, a spare MacBook Pro over Tailscale SSH) to carry most of the load. Remote is live and
routing runs today, **but it is only proven clean at ~3 concurrent slots and fails at 6.** Until that
is fixed, the volume goal is not reachable. **Remote concurrency is the gating constraint on scale.**

## What's going on today (the basics)

- **`gb-mbp` is live and selected.** `.harmonik/workers.yaml`: `enabled: true`, `max_slots: 6`, and the
  live daemon is actively routing real runs to it right now (Pi/codex/revamp beads landed on it today).
- **Proven clean at low concurrency, historically.** A real bead ran → reviewed → merged over SSH on
  2026-06-21 (~3 slots). **Then it broke for a week or two and "totally didn't work"** (operator) —
  that regression history is itself a data point the investigation should reconstruct, not skip.
- **Fails at high concurrency now.** At ~6 concurrent, runs execute the implement + review agents on
  the remote box (reviewer even reaches APPROVE) but then fail — the named symptom is a reviewer
  **`agent_ready_timeout`** under load, tracked as **hk-5z1f0** (fix merged `fd92adfc`, NOT deployed,
  NOT proven). A second, distinct tail failure is a daemon **non-fast-forward merge retry gap**
  (hk-sfy7f) and an **empty-HEAD worktree race** on an unpushed base ref (hk-zno2t).
- The three formal "validate remote at load" beads (`hk-rs-validate-remote-898a`, `hk-tagp`,
  `hk-icdz`) **were never banked green.**

## Open questions the operator has explicitly flagged (do NOT accept the current framing)

These are challenges to the *existing explanation*, raised by the operator. The investigation must
treat them as live hypotheses, not settled:

1. **"Cold start" is a suspect explanation.** The current story blames reviewer spawn on Claude
   *cold-start* contention. But **it can't be a cold start if we just ran on that box** — the machine,
   the harness, the caches are already warm. "Cold start" keeps getting repeated and does not add up.
   → *What is the `agent_ready_timeout` actually waiting on?* Pane/tmux spawn? An SSH round-trip? A
   lock/semaphore? A model API handshake? Instrument the real wait, don't assume "cold start."

2. **The concurrency ceiling smells architectural, not incidental.** Node handles high concurrency;
   Go handles it at least as well — many systems do **100k+ requests/min** trivially. A system that
   **falls over at 6 concurrent runs** is not hitting a resource wall, it's hitting a *design* limit
   (a serialized section, a single-worker lock, a per-op blocking SSH call, a global mutex, an
   unbounded-then-starved goroutine pattern, one shared tmux server, etc.).
   → **This likely warrants a fundamental look at how the remote-dispatch / worker path is built**,
   not just bumping a timeout. The `hk-5z1f0` timeout raise (90→150s + a spawn semaphore) may be a
   band-aid over a structural bottleneck. Find the actual serialization point.

## What the investigation should produce (next steps — not yet done)

- Reconstruct the "worked June → broke for weeks → works-but-flaky now" timeline (what changed?).
- Instrument and *name the real thing* the reviewer spawn blocks on under load (kill the "cold start"
  hand-wave with a measured wait breakdown).
- Map the remote-dispatch path end to end and identify every serialized / single-threaded / blocking
  section (SSH calls, tmux/pane spawn, worktree create, lock acquisition, merge-back).
- Decide: is the fix incremental (deploy hk-5z1f0 + land hk-zno2t/hk-sfy7f + canary) or does the
  worker/remote-execution architecture need a redesign for real concurrency? Recommend with evidence.
- Define the acceptance bar: a **sustained 6-slot remote canary banked green**, then a path to 6→10.

## Immediate (incremental) unblock path — tracked separately

Independent of the architectural question, the known incremental path to a first 6-slot proof (gated
on the operator freeing disk so the daemon stops bouncing):
1. Deploy the reviewer-timeout fix **hk-5z1f0** via the normal GATE-0 redeploy.
2. Run a sustained 6-slot remote canary; close the validation beads.
3. Land the empty-HEAD worktree-race durable fix **hk-zno2t** + the merge-retry gap **hk-sfy7f**.

## Pointers
- `.harmonik/workers.yaml` — worker registry / slots.
- Beads: hk-5z1f0 (reviewer timeout), hk-zno2t (empty-HEAD race), hk-sfy7f (merge-retry gap),
  hk-rs-validate-remote-898a / hk-tagp / hk-icdz (unbanked validation), hk-rs-phase1-qfn1
  (remote-substrate Phase 1 epic, open), hk-ydlyh (multi-remote N>1, future).
- `.harmonik/context/captain-lanes.md` — remote lane (owner: hawat), now reconciled to gb-mbp-LIVE.
