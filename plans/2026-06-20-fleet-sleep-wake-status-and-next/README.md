# Fleet sleep/wake — status, critique, and recommended next step

**Date:** 2026-06-20
**Author:** main session (catch-up + critique pass for the operator)
**Epic:** `hk-rl4b` (Fleet sleep/wake: quiesce token-burning LLM loops when work drains)
**Related:** `docs/ideas/fleet-sleep-wake-research.md` (design), `specs/park-resume-protocol.md` (spec), `plans/2026-06-20-captain-economy/` (sibling economy campaign)

---

## TL;DR

The idea — **put the fleet to sleep so idle LLM sessions stop burning subscription tokens** — is **mostly already built and merged to main.** The hard, valuable parts work today. Two things are NOT what the project notes claim:

1. **The "deterministic daemon auto-sleep" is not deferred — it is LIVE and ungated.** The daemon already auto-parks every session the moment its drain oracle says DRAINED, on every boot, with no on/off switch. The notes say "policy layer deferred"; in fact only the *captain's* self-decision is deferred — the *daemon* already decides and parks autonomously.
2. **The full captain-autonomous policy layer should be declined, not just deferred.** Two independent reviews agree it is the lowest-value, highest-risk remaining piece and duplicates a decision the daemon already computes deterministically.

**Recommended next step:** *Harden and gate what's already shipped* (add an on/off flag + fix three concrete safety gaps), grab the free polling-frequency cuts, and adopt manual `harmonik sleep`/`wake` as an operator habit. Do **not** build the LLM captain-decides-to-sleep layer.

---

## 1. The problem (why this exists)

~6 long-lived `claude --remote-control` LLM sessions (a captain + crews + a Sonnet ctx-watchdog) stay alive 24/7. Deterministic loops — daemon heartbeats (60s), captain `/loop 12m` health tick, crew progress timers (10m), comms monitors — keep **waking** those sessions to re-read ~300K of context and do nothing.

**Evidence (token audit `hk-bsdr`, 2026-06-17):** two near-idle multi-day sessions cost **$326 = 18% of a 3-day $1,775 spend** — 944 turns over 56h ≈ **1 turn / 3.5 min** to do little. ~96% of idle spend is cache-read (context-size × turns × sessions). This is the operator-away-overnight/weekend case.

---

## 2. What is already built (merged on main, 2026-06-18)

| Module | Bead | File | Status |
|---|---|---|---|
| M0 genuine-drain oracle (queue/run/ready) | `hk-95uf` | `internal/daemon/draindetect.go` | CLOSED |
| M0b oracle (open-epic / ledger axis) | `hk-rai2` | `internal/daemon/draindetect_epic.go` | CLOSED |
| M1 daemon quiesce + wake trigger | `hk-jeby` | `internal/daemon/quiesce.go` | CLOSED |
| M2 session park/resume protocol | `hk-s8qi` | `specs/park-resume-protocol.md` + skills | CLOSED |
| M3 keeper sleep-gate | `hk-l3gs` | `internal/keeper/gates.go` (`SleepingCheckFn`) | CLOSED |
| M4 `harmonik sleep` / `wake` CLI | `hk-s5v3` | `cmd/harmonik/sleepwake.go` | CLOSED |
| supporting: daemon auto-reset on cancel | `hk-e3fy` | — | CLOSED |

**Verified working today:**
- `harmonik sleep [--force]` and `harmonik wake [--agent X | --all]` exist in the built binary.
- The genuine-drain **oracle is well-tested** (14 tests covering the operator's known false-empty traps: `br ready` pagination, paused-by-failure queues, failed-archive scan, in-flight runs, open-epic ready-children, `kerf next` not sole oracle, fail-closed on UNSURE). This is the load-bearing safety interlock and it is the strongest part of the work.
- The daemon's `QuiesceArbiter` runs every boot: drain-check every 30s, auto-park on DRAINED, auto-wake on `queue submit` (via `WakeCh`), 4h max-sleep failsafe.

**Genuinely missing:** only the *captain's own* "I notice I'm drained, I'll sleep myself" behavior (the "POLICY layer"). Everything mechanical is shipped.

---

## 3. Critique — what the reviewers found

Two independent agent reviews (adversarial-correctness + simplicity/alternatives), plus a direct code check.

### 3a. The biggest finding: auto-sleep is LIVE and ungated (severity: high)
`internal/daemon/daemon.go:1785` calls `quiesceArbiter.Start(ctx)` unconditionally, with the drain oracle wired at `:1770`. `quiesce.go:282-295`: on `DrainStateDrained` it immediately `parkAllSessions` — **no enable flag, no operator opt-in, no policy gate.** Grep for `Enabled`/`AutoSleep` in `quiesce.go` returns nothing.

**Implication:** the system *already* sleeps itself deterministically. That's good (it's the cheap, bounded option a reviewer would have recommended building) — but it ships **with no off-switch** and depends entirely on the oracle never false-declaring DRAINED. If any drain axis has a hole, the fleet goes dark over real work with zero human in the loop.

### 3b. Wake-reliability / stuck-asleep gaps (severity: high, likelihood: medium)
- **C2 — captain wake pane target is a fragile hard-coded derivation** (`quiesce.go:304`: `harmonik-<hash>-captain:0.0`). Memory note records the live captain session is sometimes just `captain` → the nudge lands on a non-existent pane → stuck asleep until the 4h failsafe, which nudges the *same wrong pane* and also fails. The doc comment promises a `ResolveTmuxTarget` fallback that the code does not call.
- **C8 — daemon-restart orphans sleep markers.** If the daemon dies while sessions are parked, the in-memory `sleeping` map and the 4h failsafe die with it; on restart `NewQuiesceArbiter` starts with an empty map and never reconciles the on-disk `.sleeping.*` markers → keeper gates stay suppressed and those sessions can be stuck asleep indefinitely. No startup reconcile scans/clears the markers.

### 3c. The ctx-watchdog fights sleep (severity: high, likelihood: high if both run)
The Sonnet `ctx-watchdog` (`scripts/ctx-watchdog-launch.sh`) — the operator's live 300K governor — has **zero knowledge of `.sleeping.*` markers**. A parked crew with a stale `.ctx` gauge can read as DEAD → the watchdog force-restarts and nudges it awake, billing tokens for exactly what sleep was meant to stop, and un-sleeping it behind the daemon's back. The keeper *was* gated (M3); the ctx-watchdog was not.

### 3d. The captain-autonomous POLICY layer is the wrong thing to build (both reviewers agree)
- It needs a **5-check anti-false-sleep predicate** whose failure mode is the worst in the system (dark fleet over ready work) and operator-tuned bands.
- It puts a **probabilistic organ (LLM judgment) where a deterministic skeleton already has the answer** — the daemon's `GenuineDrain` + `WakeCh` *are* the drain decision. Asking the captain LLM to re-derive it inverts the project's own "deterministic skeleton, probabilistic organs" principle.
- Marginal savings over the already-live daemon auto-sleep ≈ "an LLM re-deciding what Go already decided."

### 3e. Cheaper root-cause attack the design under-weights: just poll less (free, zero risk)
The research doc's own §1 "Key finding" says the idle burn is **scheduled LLM re-invocations**, i.e. *frequencies*, not the existence of sessions:
- Captain `/loop 12m` → `30–45m` while idle (the single largest scheduled captain burn).
- Subscribe heartbeat 60s → 300–600s while idle, or drop the crew heartbeat subscribe entirely (clamp already allows 600s).
- Crew progress timer 10m → 20m while idle/draining.

This is a **config/skill-text change, no Go, no new primitive, no false-sleep risk**, and it cuts burn during the short 10–30 min idle gaps that are too brief for any sleep policy to bother with — exactly the gaps a present-but-distracted operator generates all day. Sleep handles the long contiguous away-window; slower polling handles the steady-state drizzle. They stack.

### 3f. Smaller oracle gaps (severity: medium, worth a live check)
- `complete-with-failures` queues that roll to `Completed` status may be invisible to the oracle (`draindetect.go:217` treats Completed/Cancelled as drained; failed-archive scan only catches on-disk `*.failed-*`).
- `IsSleeping` is fail-*open* on empty sessionID (`gates.go:155`) — combined with the known "sessionID flips dead on /clear" drift, a session that lost its SID is both un-wakeable-by-marker and un-gate-able.

---

## 4. Recommendation (ranked)

The expensive overnight burn ($326/3-day) is **already solved** the moment the operator runs two commands or the daemon auto-sleeps. The remaining work is *making the already-live auto-sleep safe and controllable*, not adding new policy.

1. **[Do first — free, no code] Polling-frequency cuts (§3e).** Widen captain `/loop`, subscribe heartbeat, and crew idle timers in the skill assets. Highest savings-per-effort; zero risk; stacks with everything. Remember the embedded-asset re-sync (`cp` to `cmd/harmonik/assets/skills/...`).
2. **[Do — already shipped, adopt it] Operator habit: `harmonik sleep --force` on leaving / `harmonik wake` on return.** `--force` bypasses the drain gate, so it's a complete solution for the deliberate away-window. Document it as the standing wind-down step. Optionally add a daily night-sleep / 9am-wake row via the existing `harmonik schedule` (no code; self-heals on any `submit` via `WakeCh`).
3. **[Build — small, this is the real next epic] Gate + harden the live daemon auto-sleep.** Concretely:
   - Add a `sleep_enabled` flag (config or daemon flag) so auto-park can be turned off — it currently cannot. Default off until the wake path is trusted.
   - Fix C2: resolve the captain/crew wake pane via `ResolveTmuxTarget` (with a liveness probe) instead of the hard-coded `…-captain:0.0`.
   - Fix C8: reconcile orphaned `.sleeping.*` markers on daemon startup (and re-seed the failsafe for already-parked sessions).
   - Fix C3c: teach the ctx-watchdog (and any restart authority) to skip sessions with a live `.sleeping.*` marker.
   - Live-verify §3f oracle edges (complete-with-failures, lost-SID fail-open).
4. **[Decline] The captain-autonomous LLM sleep-decision policy layer.** Convert epic `hk-rl4b`'s deferred policy item to *declined* unless a measurable gap survives steps 1–3. The deterministic daemon already makes this decision; an LLM should not re-make it.

**One-line answer to "what next":** ship the free polling cuts + the operator habit now; spend code only on a small *gate-and-harden* epic for the auto-sleep that's already live; do **not** build the captain-decides-to-sleep layer.

---

## 5. Before trusting auto-sleep — three live tests

1. **Does the daemon auto-park right now?** Let the fleet drain with the daemon up; watch for `.harmonik/.sleeping.*` markers. (Per §3a it should — confirm, because if the oracle has a hole this fires with no human in the loop.)
2. **Captain wake round-trip on the REAL session name** (§3b/C2): park the captain, fire an `epic_completed` / `queue submit`, confirm the Enter nudge lands in the live captain pane.
3. **ctx-watchdog vs. a parked crew** (§3c): park a crew, let the 30-min watchdog tick, confirm it does NOT force-restart the sleeping crew.

---

## Appendix — evidence map

- Auto-park unconditional: `internal/daemon/daemon.go:1770` (SetDrain) + `:1785` (Start); `internal/daemon/quiesce.go:282-295` (DRAINED → parkAllSessions), no enable flag.
- Wake pane target: `internal/daemon/quiesce.go:304` (captain), `:313` (crew); 4h failsafe `:260-276`.
- Oracle: `internal/daemon/draindetect.go` (+`_epic.go`), 14 tests in `draindetect_test.go`.
- Keeper gate: `internal/keeper/gates.go:155` (`IsSleeping`, fail-open).
- CLI: `cmd/harmonik/sleepwake.go` (`--force` bypasses gate).
- ctx-watchdog (sleep-unaware): `scripts/ctx-watchdog-launch.sh`, `.harmonik/cognition/ctx-watchdog-prompt.txt`.
- Design/spec: `docs/ideas/fleet-sleep-wake-research.md`, `specs/park-resume-protocol.md`.
- Epic + history: `hk-rl4b` (OPEN; full M0–M4 trail in its comments).
