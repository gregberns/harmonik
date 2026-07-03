# C6 — Boot-level daemon restart backoff — Research findings

Component: throttle rapid sequential daemon boots with a crash-aware exponential backoff backed by a
durable last-boot marker, so a crash-and-re-pull loop cannot produce 10 boots/day.
Bead hk-7t9g1, analysis gap #7, goal G5. Anchors verified at `2e49a8df`.

## RQ1 — Does ANY boot-level backoff or last-boot marker exist today?

**Finding — NO. Fully greenfield at the boot level.** `grep -rln 'last.boot|daemon_boot_throttled|boot.backoff'` over `internal/` and `specs/` returns ZERO matches. `daemon.Start` (`internal/daemon/daemon.go:381`) emits `daemon_started` (`:629-650`, §8.7.1) and proceeds immediately — no last-boot timestamp is read or written, no boot-time delay gate. The pidfile lock (PL-002) prevents CONCURRENT daemons but not RAPID SEQUENTIAL re-boots after each crash (each boot is a fresh OS process that acquires the auto-released lock cleanly). This is precisely the incident multiplier: 10 separate `harmonik --project` invocations across a day, the last 5 within ~14 minutes, each with no memory of the prior boot.

## RQ2 — Is there a reusable backoff algorithm in-repo (C6-R2, R4)?

**Finding:** Yes — the SUPERVISOR has a proven exponential-backoff-with-jitter + crash-loop detector to model C6 on (but it is in-PROCESS, not boot-level):
- `internal/supervise/supervisor.go` — `backoffWithJitter(backoff, jitter)` (`:307,460`, ±jitter/2 uniform), exponential `base 1s → cap 60s, ×2`, `MaxRestarts=5` (`:35-44`), dual crash-loop detection: absolute cap (`:279-283`, exceed N restarts → error) AND sliding-window (`:286-296`, N restarts all within a window → crash-loop). C6 reuses this ENVELOPE shape (base/cap/window, finite defaults, operator-configurable per C6-R4) but applies it at boot rather than per-child-restart. The supervisor's in-memory `restartTimes []time.Time` becomes a DURABLE file for C6 (the supervisor can keep state in memory because one process supervises many restarts; C6 spans many processes so state MUST be on disk).

## RQ3 — Durable last-boot marker design (C6-R1, R6)

**Finding:** No marker exists; C6 creates one. Contract (per decomposition interface table): `.harmonik/daemon.last-boot` carrying `{started_at, exit_disposition, recent_boot_count}`, under the PL-005 file surface. Requirements:
- **C6-R1 durable + crash-safe** — file-based (not in-memory), written at `daemon.Start` (start timestamp) and updated at shutdown (exit disposition). Must survive SIGKILL — so the START write must happen early in `daemon.Start` and the recent-boot-count must be incrementable from the file alone even if the prior boot never wrote its exit disposition (a missing exit-disposition = treat as abnormal/crash, the safe default).
- **C6-R6 self-healing / window-bound** — a stale marker from long ago MUST NOT throttle a legitimate boot. The recent-boot-count is computed over a bounded window (e.g. boots in the last N minutes); entries older than the window are ignored. Recommend an append-only ring or a small list of recent boot timestamps so the window is computable; prune on read.

**Tradeoff (marker format):**
- **Option A — single JSON `{started_at, exit_disposition, recent_boot_count}`** (decomposition's shape). Pro: simple. Con: `recent_boot_count` must be recomputed against a window, which a single scalar can't express without a clock-anchor; needs `window_start` too.
- **Option B — append-only `.harmonik/boots.jsonl`** of `{started_at, exit_disposition}` rows; the window count is derived by scanning rows newer than `now - window`. Pro: window-bounding and self-healing fall out naturally; crash-safe append. Con: needs bounded growth (prune rows older than window on each boot).
- **Recommendation:** Option B (append-with-prune) — it directly supports the window-bounded self-healing C6-R6 demands and the crash-safe "missing exit disposition = abnormal" default; prune-on-boot keeps it small.

## RQ4 — Clean-vs-crash disposition (C6-R3): how is the prior exit classified?

**Finding:** The disposition must distinguish operator-intended restart (prior CLEAN shutdown) from crash-loop (prior ABNORMAL exit) — a clean prior shutdown MUST NOT be throttled (C6-R3). Signals available:
- **Clean shutdown** = the daemon's graceful-drain path (PL-011) completed and wrote `exit_disposition=clean` to the marker before exit. Exit codes 20 (`signal-terminated`) / 11 (`drain-timeout-escalated`) / 19 (`runtime-panic`) from the registry (`exitcode.go`) classify abnormal exits.
- **Abnormal / crash** = the marker's start row has NO matching clean-exit write (process died before writing disposition → SIGKILL/panic/OOM). The safe default: a start row with no exit row = abnormal. So C6 reads the prior boot's row; if its exit_disposition is absent or in {panic, signal-terminated-without-drain, drain-timeout} → abnormal → eligible for throttle; if `clean` → not throttled.

## RQ5 — Observability (C6-R5) and refuse-vs-delay choice

**Finding:** C6-R5 needs a `daemon_boot_throttled` event (delay + recent-boot-count) — a NEW event type (no precedent; `grep daemon_boot_throttled` → none) added to event-model §8.7.x, additive. The decomposition leaves refuse-vs-delay open (C6-R2: "delay before proceeding, OR refuse with a retry-after diagnostic"). **Recommendation:** DELAY (sleep then proceed) for early window positions, escalating to REFUSE (exit with a retry-after diagnostic + a new exit code, candidate 25 if C5 took 24) once the recent-boot-count exceeds a hard ceiling — mirroring the supervisor's delay-then-crash-loop-error shape (`supervisor.go:279-307`). A pure-delay design risks a process pile-up if the operator/wrapper keeps invoking; refuse-at-ceiling caps that. Change-spec decision.

## RQ6 — Interaction with C5 and the wrapper that re-launches

**Finding:** C6 throttles HOW OFTEN a crashed daemon re-boots; C5 prevents TWO running at once; they compose. Note the incident's re-launch driver was external (a wrapper/operator re-running `harmonik --project`), so C6's throttle must live INSIDE `daemon.Start` (the only point every boot passes through) — an external supervisor wrapper cannot be assumed. The throttle gate must run AFTER the pidfile-lock acquire (so a refused-concurrent boot exits 5 first) but BEFORE auto-pull/dispatch (so a throttled boot does not spawn). Place it early in `daemon.Start`, after pidfile-lock, before queue-load/auto-pull.

## Risks / unknowns
- **R1 (refuse-vs-delay + new exit code):** if C6 refuses at a ceiling it needs a new §8 exit code + `CommandExitCodeSet` registration (same constraint as C5) — coordinate code allocation (C5=24, C6=25).
- **R2 (operator-intended rapid restart):** an operator deliberately restarting after a clean stop must not be throttled (C6-R3) — the clean-disposition signal handles this, but a clean-stop that the daemon failed to record (e.g., killed during drain) would mis-classify as abnormal; the "missing disposition = abnormal" default errs toward throttling, which is the SAFE direction (prefers under-spending). Note the asymmetry.
- **R3 (clock source):** window computation needs a monotonic-ish wall clock; `internal/lifecycle/monotonic_darwin.go` exists for darwin — confirm the marker uses a comparable timestamp basis across boots (wall-clock UTC is fine for a minutes-scale window; monotonic does not survive reboot).

## No-blocker assertion
No blocker prevents a C6 change spec. C6 is the most greenfield component (no existing boot-level
machinery) but the algorithm (supervisor backoff envelope) and the lock/probe idioms are fully
precedented. Change-spec deliverables: (a) marker format (recommend append-with-prune `boots.jsonl`),
(b) refuse-vs-delay policy + any new exit code (coordinate with C5 at 24/25), (c) the
`daemon_boot_throttled` event schema.
