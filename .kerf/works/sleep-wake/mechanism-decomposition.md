# Sleep/Wake Mechanism Decomposition (hk-rl4b)

Greenlit shape (do NOT redesign): daemon-arbitrated sleep + daemon-triggered wake,
HYBRID with a `harmonik sleep`/`wake` CLI manual override. The daemon (deterministic Go,
zero subscription cost) is the single always-awake wake-listener.

POLICY layer is DEFERRED (see §POLICY). These are the MECHANISM beads.

---

## Bead M0 — Genuine-drain oracle (FIRST, policy-independent)
See `genuine-drain-oracle.md` + the dedicated bead spec. Owns the correctness floor for
SLEEP. No risk directly, but enables risk-2/3/4 by deciding WHEN/WHO. Ship first.

## Bead M1 — Daemon quiesce-mode + wake-trigger
**One paragraph:** Add a daemon-side `QuiesceArbiter` that, on a poll tick, calls the
genuine-drain oracle (M0); on DRAINED it transitions matching sessions to SLEPT (writes a
durable per-session `.harmonik/keeper/<agent>.sleeping.<session_id>` marker and signals
the session to park — M2), and registers per-session WAKE TRIGGERS. The daemon WATCHES for
new work (queue submit/append wake via the existing `QueueStore.WakeCh()`; a new ready
crew-bead; an operator comms message; an `epic_completed` event) and, on a trigger, NUDGES
the target session's tmux pane via the daemon's stored `cachedPaneTarget`
(`internal/daemon/tmuxsubstrate.go:1527` `SendEnterToLastPane`/`WriteToPane`,
`internal/keeper/tmuxresolve.go:33` `ResolveTmuxTarget` for sessions not in the per-run
substrate) to re-arm + resume, then clears the `.sleeping` marker. Owns **Risk 2 (wake
reliability)** and **Risk 3 (wake-targeting)**: the trigger→session routing table lives
here (a crew-bead wakes THAT crew only; an operator message wakes the captain). Includes the
fleet failsafe: a `max-sleep-duration` auto-wake re-nudge so a missed trigger or a stuck
oracle bounds a stall to one window.

## Bead M2 — Session-side park/resume of wake-loops
**One paragraph:** Give each LLM session (captain/crew) a SLEEP protocol it runs when the
daemon signals SLEPT: detach `harmonik subscribe` (the ~600s heartbeat,
`internal/daemon/subscribe.go:456`), stop `harmonik comms recv --follow` (presence refresh
+ ~60s heartbeat, `subscribe.go:357`), and pause its status-post / soak / `/loop` timers —
so the session takes ZERO turns (zero cache-read burn). On WAKE (a tmux pane nudge from M1)
the session re-arms exactly those loops and resumes its operating loop. The park/resume is
driven by the session reading its own `.sleeping.<session_id>` marker (set by M1) — this is
the same correlation key the keeper uses (`ReadManagedSessionID`,
`internal/keeper/keeper.go:122`). Owns **Risk 1 (keeper-vs-sleep)** jointly with M3: while
the `.sleeping` marker is present the keeper's WARN/ACT pane-injection is suppressed (gate
at `internal/keeper/watcher.go:780`, before `inject()`) WITHOUT touching any threshold —
the keeper state machine (`warnArmed`/`warnFired`) stays intact, only the tmux delivery is
gated. Captain-sleep interlock (**Risk 4**): the session's park list MUST keep one cheap
path open for completion / `epic_completed` re-tasking — implemented by M1 registering an
`epic_completed`-wake trigger for the captain (the daemon, not the session, listens).

## Bead M3 — Keeper sleep-gate (suppress own injection while slept)
**One paragraph:** Add a `WatcherConfig.SleepingCheckFn func(projectDir, agent, sessionID)
bool` (mirrors the existing `HoldingDispatchFn`/`CrispIdleFn` plumbing) that reads the
`.sleeping.<session_id>` marker, and gate the WARN inject at `watcher.go:780` and the cycler
handoff at `cycle.go:621` (co-located with the existing `HoldingDispatch` gate) so a slept
session is NOT woken by its OWN keeper. Reuses the `.dispatching` marker pattern
(`internal/keeper/gates.go:17`) but with a DISTINCT `.sleeping` marker (sleep ≠ dispatch).
DEFAULTS-PIN HARD: this bead gates keeper BEHAVIOR only; it MUST NOT read or change any
`internal/keeper/thresholds.go` value (warn 80%/270k, act 90%/300k, force +40k, window
200k). Owns **Risk 1 (keeper-vs-sleep)**: makes the keeper the cooperating party, never the
fighter. NOTE: while slept the gauge keeps reading but no action fires; if the session is
slept and genuinely overflowing, M1's max-sleep failsafe wakes it and THEN the keeper acts
normally — sleep never strands a session past its act threshold indefinitely.

## Bead M4 — `harmonik sleep` / `harmonik wake` CLI (manual override)
**One paragraph:** Add `harmonik sleep [--agent <name>|--all]` and `harmonik wake [--agent
<name>|--all]` as operator/captain MANUAL overrides over the automatic path: `sleep` writes
the `.sleeping` marker(s) and signals park (bypassing the oracle — operator intent is
authoritative); `wake` clears the marker(s) and nudges the pane(s). These are thin RPC
verbs into the daemon's QuiesceArbiter (M1). Manual `wake` is also the human escape hatch if
the auto-wake failsafe ever misfires. Owns no risk directly but provides the operator
escape valve for Risk 2 (a stuck wake) and the override for Risk 4 (force-wake the captain).

---

## POLICY — DEFERRED / OUT OF SCOPE (held for operator knob preferences)

The CAPTAIN SLEEP-PROTOCOL / POLICY layer is explicitly NOT in this mechanism set:
- sleep-grace aggressiveness (how long drained before sleeping),
- wake-trigger default set + sensitivity,
- any band / threshold for sleep,
- per-role policy (captain vs crew sleep eligibility).

These touch operator-tunable behavior and the hk-xjlq "no band-retune" sensitivity, so they
are held for operator knob preferences. The mechanism beads above ship with CONSERVATIVE,
non-tunable hardcoded defaults (e.g. immediate-on-DRAINED sleep with a long max-sleep
failsafe) sufficient to validate the mechanism; the policy layer replaces those with
operator-chosen knobs in a later pass.

DEFAULTS-PIN applies across ALL beads: NONE of M0–M4 may alter any keeper warn/act/force/
window threshold value. Sleep gates BEHAVIOR; it never moves the keeper bands.
