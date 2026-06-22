# Morning note — overnight 2026-06-22 (captain)

Plain-English status of the 4 overnight clusters. Codenames: C1=workflow-mode-config, C2=process-supervision, C3=multibox-routing, C4=release. "DOT" = the 3-reviewer Sonnet build pipeline. Bead IDs are `hk-…`.

## C1 — workflow_mode must live in config (YOUR #1) — ✅ BUILDING
**Status:** design done + 2 adversarial reviews → consensus → **dispatched and building** as bead `hk-y3o51` (queue `c1-workflow-mode`, triple-review DOT).

What it fixes: `queue submit --beads` was hardcoding single-reviewer (`review-loop`) and silently overriding the daemon's correct `dot` (triple-review) default — that's why the fleet ran for hours on the wrong process. The build does the **durable** version, not just the one-liner:
- Core: `internal/queue/cli/submit.go:48` stops stamping `review-loop` → daemon's `dot` default wins.
- Config: adds `daemon.workflow_mode: dot` to `.harmonik/config.yaml` (set once, persists across captains/restarts).
- Durability (reviewers flagged as blocking): `cmd/harmonik/init_cmd.go:393` re-hardcodes `review-loop` into **every new project** → changed to `dot`. And `run_started` events don't carry `workflow_mode`, so "wrong mode for hours" was undetectable → adds the field.
- Parity: `harmonik run` had the same silent default → also inherits config now.
- Tests rewritten to the new contract.

**No decision needed from you** — it's buildable-now and reversible (single commit). Reviewers agreed NOT to make the daemon fail-loud on unset mode (the shipped `dot` floor is the correct safe default; this is not the keeper no-hardcoded-thresholds case).

## C2 — process supervision + health checklist — ✅ consensus; **silent-death fix BUILDING**, rest pending 2 decisions
**Dispatched tonight:** `hk-sbitr` — the ctx-watchdog now auto-relaunches (daemon-native `every@5m` ensure, mirroring ops-monitor; idempotent launcher). This closes the exact failure that caused this session's crisis (watchdog died ~36h → 5 crews lost context enforcement). Building via DOT on queue `c2-watchdog-relaunch`. **Lands dormant** — activates on the next daemon restart (your morning rebuild), reversible by revert. Default-ON, no new hardcoded token threshold (mandate-safe). The watchdog FORCE-path change (hk-u5tgh) and the escalation ledger were deliberately left out of this bead (see below).

Both reviewers: AGREE-WITH-AMENDMENTS. Design is ~80% reuse of existing primitives (ops-monitor checks, `LiveKeeperPresent`, `SupervisorWatchdog`, schedule-ensure) — keeps an LLM **out** of the liveness path (the exact thing whose death caused this session's keeper-coverage crisis). Phase-1 resolves the crisis class: ctx-watchdog auto-relaunch (closes hk-sbitr — kills the silent-death), crew-keeper detection+escalation, `keeper liveness --json`.

**2 genuine morning decisions (everything else is buildable-now):**
1. **Governor ownership:** keep the Sonnet ctx-watchdog as the 300k force-cut, OR retire it and make the daemon the governor. (Retiring adds an operator-required, fail-loud token threshold — the one place the no-hardcoded-defaults mandate bites.)
2. **Crew-keeper auto-re-arm vs escalate-only:** auto-re-arming a *live, busy* crew risks disrupting in-flight work; safe default = escalate-only.

Reviewer-flagged fix before build: the `--to operator` escalation backstop targets a comms recipient that **doesn't exist yet** — needs a concrete channel (desktop notif / file drop / dedicated sink) before that rung is wired. Auto-decided per your brief: keeper + watchdog default-ON (you said "cheap to run / leave on").

## C3 — multibox routing + worker scaling — ✅ reviewed → **PARK gb-mbp tonight** (1 review finishing)
**Decision: do NOT point the daemon at gb-mbp unattended tonight.** Reviewer CORRECTED the design's reasoning (important): routing is NOT broken — the event bus has 116 `gb-mbp` events, so selection DID fire. The real blocker is that **remote runs FAIL FAST**: hk-h106 failed 28s in; the concurrency-proof bead hk-icdz failed 6× in a row (~90s each). The only clean remote completion is hk-620j (single bead, review-loop); the claimed "DOT GREEN" proof (hk-4lrj) actually went stale + claim-write-lost, so it was overstated. All 6 concurrency-proof beads remain OPEN.

So PARK is **more** strongly correct, for a corrected reason: the morning task is to **diagnose the remote-run failure cause** (grep `run_failed` payloads on the event bus), not to re-test whether selection engages. Enabling tonight is *survivable* (rollback ~2s: `--worker-enabled` off + pkill → supervisor revives; in-flight remote beads reset to open) but **not beneficial** — unattended it would thrash remote→fail→local. The router code itself (single decision point `SelectWorker()` at `workloop.go:2747`; global cap is box-agnostic so per-box accounting in `RunHandle` is the load-bearing scaling change) is buildable-now and a good next-step once remote runs are GREEN.

**No tonight action.** Morning: diagnose remote `run_failed`, get one clean remote DOT completion, then build the router + bump workers (start 2 on gb-mbp, watch session limits). Both reviewers confirm PARK. Router build gotchas to honor: the `workers.yaml version: 2` bump is a **breaking parser change** (`workers.go:278` rejects unknown versions → silently drops to local-only) — ship the loader bump atomically or nest routing under `version: 1`; per-box accounting (a box field on `RunHandle`) is needed for true run-on-both fairness; and router payoff is gated on R5 (why did remote selection no-op last time — actually it didn't, runs reached the box and failed). Architecture itself: AGREE.

## C4 — cut a known-good release — ✅ **local tag v0.2.0 CUT** (reversible); push needs your sign-off
Reviewer verified `79a3b0ce` is known-good: `go build ./...` clean, `go vet` clean, core suites (release/keeper/supervise/workflow/dot/scenario + full internal/daemon) green; contains the wixms DOT fix (1c84fd1f) + supervisor cry-wolf fix (f6b76f59); no mid-flight regression at HEAD; local tag fires no CI (push-only trigger, git hooks inert), fully reversible.

**Done tonight:** cut local annotated tag **`v0.2.0`** on `79a3b0ce` — NOT pushed.
**Version correction (reviewer):** the design said v0.1.1 (patch) — that's wrong. There are **1016 commits incl. many `feat()`** since v0.1.0 (native `harmonik start`, captain/crew launchers, keeper hold/release, worker-report telemetry, etc.). That's a **minor → v0.2.0**, which is what I tagged. Final call is yours.
**Needs your sign-off to ship:** GPG-signed `git push origin v0.2.0` → triggers the 16-binary GitHub publish. To re-version first: `git tag -d v0.2.0 && git tag -a <ver> 79a3b0ce -m "…"`. To undo entirely: `git tag -d v0.2.0`.
**Note:** `origin/main` has since advanced past 79a3b0ce (daemon kept merging crew work — e.g. paul's cache-reaper mutex fix) — the tag deliberately points at the *reviewed* SHA, not moving-main. If you'd rather release the newer head, it needs a fresh known-good pass.

---
## ⚙️ Daemon restarted onto wixms (~05:42Z) — significant infra action
**Why:** the live daemon binary was stale (mtime Jun-21 19:41, PRE-wixms). Stilgar root-caused the `hk-01gjv` "no-progress iter-2: HEAD did not advance" failure as the **iter-2 implementer-resume back-edge thrash** that wixms (`1c84fd1f`) + yrnui (`f6b76f59`) fix. Those fixes were on origin/main but NOT in the running daemon — so *every* DOT run that hit a review back-edge would thrash, putting C1 (your #1), C2, and stilgar's lane all at risk.
**What I did:** `go build ./...` clean → `go install ./cmd/harmonik` (new binary 22:40 local, has both fixes) → killed the daemon (`pkill -f "harmonik --project …"`, matched only daemon+wrapper, never supervisor/keepers) → supervisor `hk-keeper.sh` auto-relaunched it in ~10s (new pid 87219, `--max-concurrent 4`). Reversible/standard deploy; handoff pre-authorized it.
**Consequence (expected):** in-flight runs were reset and re-dispatched on the fixed daemon. Paul's long-running `hk-ldzp` (daemon-reliability, ~3h in) was collateral-killed → paul re-dispatched it once (known behavior, hk-r73qr). C1 immediately re-claimed a worker on the fixed daemon. Stilgar released its held heavy bead `hk-yv1ck` on the "UP on wixms" signal.
**Correction for you:** the rebuild did **NOT** silence the `supervisor-up` cry-wolf flag (the handoff assumed `f6b76f59` would). The supervisor IS alive (verified pid 55659); the flag's real fix is a different open bead (detail cites hk-pen9), not f6b76f59. Still benign — ignore the alert.

---
## Verified-benign tonight (no action needed)
- **`supervisor-down` IMMEDIATE alerts** = cry-wolf. Confirmed the supervisor (pid 55659, `hk-daemon-supervise` session) + daemon (pid 47502) alive. The ops-monitor check is buggy; its fix (f6b76f59) is in the code but not in the *running* daemon. Silences on your morning rebuild. Did NOT restart mid-build to silence it (no lull). Paul's crew has `dqlkz` (supervisor auto-restart) in flight — same hardening converging.

---
*Updated as reviews land. Live lanes (paul=daemon-reliability, stilgar=keeper-coverage, logmine=remote/log) kept draining throughout.*
