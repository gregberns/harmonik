# Keeper Reliability — Execution Worklog

**Started:** 2026-06-20 (after the 13-agent critique). Operator directive: *make BOTH reset cycles work reliably; merge `89852bb3` if it's good code, then base move-forward off that; document everything; delegate.*

## Operator's reframes (binding)
- The "restart loses intent" premise is **moot** — `/clear` discards the transcript too, so both L (in-place clear) and D (kill+respawn) depend on the handoff capturing intent. Not a differentiator. **L is marginally preferred** (same pane, same pid).
- Goal: **both cycles reliable**, with a **bidirectional ACK handshake** giving verifiable liveness (operator's design = README §4 = what `89852bb3` partially implements).
- `.sid` is written immediately by the SessionStart hook; the bug is the code not *trusting* it (re-derives identity from the gauge). Fix = single authoritative identity source.

## State at start
- `89852bb3` NOT merged (HEAD `1ccc2b90`); on branch `worktree-agent-acb20218b63a573e6`. −539 net lines, prior overnight reviewer APPROVE + green tests + live smoke.
- **Daemon RUNNING** (pid 66377, `--max-concurrent 4`) → merge only in a queue lull.
- Open decision flagged by README: `89852bb3` pins launch scripts to 200k/215k abs, *sidesteps* the pct-vs-band conflict instead of grafting the killed agent's **clamp-to-tighten-only** `derivePctThreshold`. Decide at review.

## Plan
1. [x] **Fresh independent review of `89852bb3`** → **APPROVE** (`14-review-89852bb3.md`). Tests green, deletions safe, conflict-free, no must-fix. pct-pin acceptable (clamp = follow-up); `.sid` already authoritative via ReadCtxFile overlay (hk-8prq); manual path only — automatic ACT-loop untouched.
2. [in progress] **Reliability baseline** of both cycles (`15-reliability-baseline.md`). — agent
3. [x] **Merged** `89852bb3` → cherry-picked as **`4128d760`** on main (lull confirmed: all queues paused, 0 workers). `go build`/`go test ./internal/keeper/...` green on merged main; tmux count normal (9, no leak). `go install` done; **pushed** `1ccc2b90..4128d760`.
   - ⚠️ Running daemon (pid 66377) + live keeper watchers still hold the OLD binary — need restart to pick up restart-now changes (deferred to the controlled live-smoke step in the runbook).
   - ⚠️ Operator caveat from review: neither old nor new script delivers a **300k cap** — if that's the intent, separate change needed.
4. [ ] **Harden** (move-forward, based on merged state):
   - (a) **Automatic ACT-loop (Bug 3)** — the worst live pain, NOT fixed by `89852bb3`: ACT-when-idle loops + truncates handoff to 0. Close the loop on `MaybeRun`/`runCycle`.
   - (b) **Agent-side ACK timer/auto-investigate** — the missing half of the handshake (keeper→pane side is in `89852bb3`).
   - (c) **Single authoritative identity** — collapse the watcher's fallback rederivation/latch heuristics.
   - (d) **Test the impure paste path** (`InjectText`/`sendEnter`); stop certifying the no-op as success.
   - (e) **File uncaptured bugs** A/B/3/D + test-coverage gaps as beads.

## Log
- 2026-06-20: critique complete (`EXEC_SUMMARY.md`); execution phase opened. Dispatched review + reliability agents.
- 2026-06-20: review APPROVE; **merged `89852bb3` → `4128d760`**, built, installed, pushed. Awaiting reliability baseline before dispatching the hardening wave.
- 2026-06-20: reliability baseline in (`15-…md`): deterministic suite GREEN 74.4%; all 3 cycles testable-but-untested-where-they-fail; 2 integration tests fail on stale 270k/300k band; integration suite leaks 2 flywheel tmux sessions single-shot.
- 2026-06-20: filed beads **hk-vpnp** (Bug3 ACT-loop, P1), **hk-uldg** (agent-side ACK timer, P1), **hk-zole** (impure-path coverage, P2), **hk-0ouc** (integration tmux leak, P2), **hk-j4m6** (band-drift tests, P3). hk-5da7 still open (manual-path piece landed via 4128d760).
- 2026-06-20: dispatched hardening wave — hk-vpnp (worktree, reproduce+fix), hk-j4m6 (worktree, test fix), hk-uldg (DESIGN only, operator-gated). Held hk-zole + hk-0ouc for next wave (avoid same-package collisions). Awaiting branches to review+merge.
- 2026-06-20: **merged band-drift fix `95135107`** (hk-j4m6) — verified green on main, tmux clean. (not yet pushed; batching with Bug3.)
- 2026-06-20: ACK agent-side DESIGN ready (`18-…md`, hk-uldg) → recommends `harmonik keeper await-ack` subcommand. Operator-defaults chosen: captain-watches-crews / 15s-1s / comms-in-skill-not-binary / event `session_keeper_ack_timeout`. Dispatched IMPL in worktree (review-gated before merge; operator may redirect).
- 2026-06-20: Bug3 (hk-vpnp) FIXED → branch `worktree-agent-a9acc91670372a2f7` commit **`e2109a2f`** (reproduce-first, 2 failing→passing tests; fixes unconditional handoff-truncation + post-abort re-fire). In review (`20-…md`).
- 2026-06-20: await-ack (hk-uldg) IMPLEMENTED → branch `worktree-agent-ad1f09bd686408cd8` commit **`e902114c`** (new subcommand + injectable PaneCapturer + `session_keeper_ack_timeout` event; 8 deterministic tests). In review (`21-…md`).
- 2026-06-20: **hk-0ouc CONFIRMED reproducible** — integration test runs leaked 4 `*-flywheel` sessions (08:56–08:57); killed, back to baseline 9. Integration tests must not be run without kill-after cleanup until hk-0ouc is fixed. Raises hk-0ouc to the test-infra blocker.
- 2026-06-20: two reviews in flight (Bug3 `e2109a2f`, await-ack `e902114c`); will merge green ones + push band-drift+Bug3+await-ack together.
- 2026-06-20: **Bug3 review APPROVE** (`20-…md`) — non-vacuous (revert→fail with exact signatures), no leak in truncation guard, no ACT-path deadlock, flake not introduced. **Merged `3f88fae5`** (daemon healthy, lull held, suite green). Not yet pushed (batching with await-ack).
- 2026-06-20: **await-ack review APPROVE** (`21-…md`). **Merged `b43638ca`**. Build/vet/test green; `go install` done.
- 2026-06-20: **PUSHED** `4128d760..b43638ca` (band-drift + Bug3 + await-ack). Cleaned 5 more flywheel leaks → baseline 9.
- 2026-06-20: closed **hk-vpnp**, **hk-j4m6**. Reverted hk-uldg/hk-5da7 to open (partial).

## Shipped this session (on main + pushed)
- `4128d760` — keeper restart-now direct/synchronous + flag-only + ACK injection (the stranded `89852bb3` cure).
- `95135107` — band-drift integration tests → pinned 200k/215k/240k.
- `3f88fae5` — Bug 3 fix: automatic ACT-loop + handoff-truncation (reproduce-first, reviewed).
- `b43638ca` — `harmonik keeper await-ack` (agent-side ACK handshake primitive, reviewed).

## Fleet restart — DONE (2026-06-20, on new binary)
- **await-ack live-smoke:** validated on the real binary — emits `session_keeper_ack_timeout` with correct payload + exit 3 on abort; match logic unit-proven + uses shared proven `ResolveTmuxTarget`. No clobber (used a throwaway /tmp shell session, not a real claude). tmux 9→9.
- **Daemon restarted:** `pkill` → `hk-keeper.sh` supervisor (pid 22133) auto-revived it on the new binary (new pid 32217, tmux `hk-a3dc45482890-keeper`). Socket up, queues intact. (`hk-keeper.sh` IS the daemon supervisor — the earlier "no supervisor" read was wrong.)
- **Captain keeper refreshed:** killed old pid 74156 (old binary + old wide-band `--warn-pct 30/--act-pct 35`); relaunched as pid 33207 on the new binary with corrected `--warn-abs-tokens 200000 --act-abs-tokens 215000`, env-unset, same respawn-cmd. **Doctor: all-green.** Bound to live captain `1274a140` (managed==gauge; NO drift — the `fe5efd0e` in the process cmdline was just the original launch arg). Captain at 6.7% ctx so no immediate cycle risk. Also ran `harmonik init --refresh-captain-tools` (fixed the STALE ~/.claude copy).
- **Captain + 5 crews:** untouched, still running (they are watched, not restarted). Crews have no keeper watchers to refresh (known: gauge-not-wired-for-crews). tmux baseline 9 throughout, no flywheel leaks.

## Round 2 (2026-06-20) — current-state critique + remaining beads
- 7 critics on the post-fix tree → `plans/2026-06-20-keeper-critique-round2/` (synthesis `EXEC_SUMMARY.md`, findings W1–W7 + D1). 2 adversarial verifiers in flight.
- **Wave-A beads merged:** `c61d228f` hk-nbft (ALL keeper subcommands flag-only — fixes the recurring positional footgun; doctor/enable were the holdouts, now exit 2; table-driven regression test); `795f57aa` hk-zole (real InjectText 10.5%→93.8% / sendEnter 0%→100% via `tmuxRunFn` seam); `909c25b0` hk-uldg (wire await-ack into the MANUAL restart flow — `keeper-restart-verified.sh` wrapper + captain/crew/keeper skills). hk-0ouc (tmux leak) still running.
- **Top round-2 findings (wave-B, pending verifier sign-off):** W1 per-agent keeper has NO supervisor → dies & stays dead (fleet risk; explains today's hand-relaunch); W2 automatic cycle STILL fire-and-forget (wire await-ack into runCycle); W3 liveness alarm nobody consumes; W4 crews have no keeper watcher (gauge works, just spawn it); W5 `--project` not Abs-normalized (bug-2a root cause persists) + set-dispatching fail-open; W6 precompact race; W7 pct flags/`default:` case. D1 identity-collapse DEFERRED (blocked on W4).

## Remaining / next wave
- **OPERATOR DECISION — 300k cap (hk-5da7):** mechanism fixed (pct now honored, restart-now no longer a no-op); but scripts pin 200k/215k. If a 300k *cap* is intended, that's a separate ctx-watchdog change.
- **hk-uldg** — wire `await-ack` into captain/crew skills (captain watches crews; restart wrapper watches self). Skill-side; design `18-…md` decision 1.
- **hk-zole** — cover the impure paste/respawn paths (the deeper testability fix).
- **hk-0ouc** — integration-suite flywheel leak teardown (test-infra blocker; reproducible).
- **Larger follow-up (from critique):** single authoritative identity — collapse the watcher fallback rederivation/latch heuristics.
