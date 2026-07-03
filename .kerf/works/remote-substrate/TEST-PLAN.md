The critic is verified correct. `dispatchDotToolNode` at `dot_cascade.go:1229-1244` runs `exec.CommandContext(execCtx, "/bin/sh", "-c", node.ToolCommand)` with `cmd.Dir = wtPath` directly on box A — no `CommandRunner`/SSHRunner involved. For a remote run `wtPath` is the worker's path, which doesn't exist on box A → chdir/no-such-file. The D2 refusal string (`workloop.go:2713`) and FR13 worker metadata (`workloop.go:2261-2267`, `3829-3831`) are confirmed verbatim. Writing the merged plan on Plan 3's skeleton with all 14 fixes applied.

# Remote-Substrate Phase 1 — Final Executable Test Plan

## Overview

Drive **real beads end-to-end on the separate-filesystem worker `gb-mbp`** (a MacBook Pro reached over plain `ssh gb-mbp`), ramping from a one-line text commit → concurrent worktrees → full DOT-mode pipelines → non-trivial multi-file code beads → the integration-branch→protected-main enterprise promote flow — surfacing and resolving the remaining gaps (foremost the **gating bug**, below) as we go.

**Translations glossary (plain English for every code used here):**
- **gb-mbp** = the spare MacBook Pro worker; a *real, separate filesystem*, reached via `ssh gb-mbp`. The only load-bearing test target.
- **Box A** / **gb-mac-mini** = the harmonik daemon host: merge authority, captain/crew, reviewer, authoritative repo `/Users/gb/github/harmonik`.
- **The gating bug (tracked as hk-230h, but RE-ANCHORED here)** = the DOT commit-gate / auto-status **shell node runs `/bin/sh -c` *locally on box A*** (`internal/daemon/dot_cascade.go:1229-1244`, `dispatchDotToolNode`) with `cmd.Dir = wtPath`. For a remote run, `wtPath` is the *worker's* worktree path, which **does not exist on box A** → the gate fails with **chdir / "no such file"**. It is NOT "SSHRunner drops `cmd.Env`": env is already inherited correctly locally (the hk-m5axg `os.Environ()` fix at :1218-1228). **The fix is to route the shell node through the per-run `CommandRunner` so the gate runs ON the worker**, like `fetchBaseOnWorker`/`pushRunBranchOnWorker` already do in `codesync_rs_b8.go`. This gates Phases 3–5.
- **DOT mode** = the production workflow graph (`standard-bead.dot`): implement → commit_gate (`go build ./... && go vet ./... && bash scripts/scenario-gate.sh`) → review (reviewer loop) → close, with auto-status. **single mode** = implement+commit only, no gate/reviewer.
- **D2 fail-closed** = the daemon **refuses to dispatch a remote run** if `ANTHROPIC_API_KEY` is in the spawn env, with the exact reason string `remote run: ANTHROPIC_API_KEY in spawn env (D2 fail-closed)` (`workloop.go:2713`); the spawn spec also re-emits empty `ANTHROPIC_API_KEY=` / `CLAUDE_CODE_OAUTH_TOKEN=` overrides (`claudehandler_chb006_024.go`). This is the *deterministic* billing guardrail.
- **FR13** = every run records `worker_name` + `worker_os` in `run_started`/`run_completed` (`workloop.go:2261-2267`, `3829-3831`); empty ⇒ ran locally.
- **NFR7** = with zero/disabled workers, execution is **byte-identical** to pre-feature local behavior.
- **`promote`** = the integration→main tool; **PR-mode** (`--pr`) opens a PR via `gh`, pushes nothing to the target; **push-mode** cherry-picks + build-gates + pushes, **refused with exit 5 against a protected branch**.
- **V1–V7** = the reusable "ran on the worker / no API burn / local fallback intact" assertion set, defined in Phase 0.

**Why `gb-mbp`, never `ssh localhost`:** localhost shares box A's filesystem (`cmd.Dir = <worker path>` exists locally), so it **masks the gating bug** — the exact false-confidence trap this whole effort exists to avoid. The localhost scenario tests (`scenario_remote_substrate_localhost*_test.go`) are necessary unit coverage but **not sufficient proof**. Every load-bearing assertion below runs on gb-mbp.

## Prerequisites

- Box A daemon under the **supervisor** (`hk-keeper.sh` / `harmonik supervise`). **Lifecycle is supervisor-only** for this entire plan — never mix `pkill` + raw background `harmonik --project … &` launches (risks a second daemon, pidfile collision exit 5, or a supervisor reviving a hand-killed daemon with stale flags).
- `gb-mbp` provisioned: clone at `/Users/gb/harmonik-worker/repo` tracking the same origin, `tmux` + `claude` + `gh` + `go` present, auth via `CLAUDE_CODE_OAUTH_TOKEN` in `~/.zshenv`, **no `ANTHROPIC_API_KEY`**.
- `.harmonik/workers.yaml` has `gb-mbp` with `enabled: false`, `max_slots: 1` (disabled pending the gating fix). **The running daemon caches `enabled`/config at boot — a `supervise restart` is mandatory after any edit**, or beads route to a half-wired worker.
- CWD pinned to `/Users/gb/github/harmonik` all session; use `git -C` / `ssh gb-mbp --` rather than `cd`.

---

## Phase 0 — Preconditions, harness, verification primitives

**Objective:** prove gb-mbp is correctly provisioned and is a *real separate FS*; define the assertions reused every phase; confirm daemon/binary current — before any bead runs.

**Setup (from box A):**
```bash
# 0.1 Reachability + provisioning + toolchain freshness (FIX #10/#12)
ssh -o ConnectTimeout=5 gb-mbp -- 'hostname && uname -s && tmux -V && claude --version && gh auth status && go version'
ssh gb-mbp -- 'git -C /Users/gb/harmonik-worker/repo fetch origin && git -C /Users/gb/harmonik-worker/repo remote -v'
go version   # box A — RECORD both; pin worker Go == box A Go (toolchain drift silently changes gate behavior)

# 0.2 Real separate FS — device-id check (FIX #4, the mandatory localhost-trap guard)
WDEV=$(ssh gb-mbp -- 'stat -f "%d" /Users/gb/harmonik-worker/repo')
ADEV=$(stat -f "%d" /Users/gb/github/harmonik)
[ "$WDEV" != "$ADEV" ] && echo "SEPARATE-FS-OK" || echo "FATAL: same device — NOT a real worker"

# 0.3 Subscription billing, fail-closed (NFR4/D2) — AND the ~/.zshenv non-interactive-ssh OAuth trap (FIX #4)
ssh gb-mbp -- 'test -n "$CLAUDE_CODE_OAUTH_TOKEN" && echo OAUTH-OK || echo FATAL-NO-OAUTH'   # empty here = dotfile trap
ssh gb-mbp -- 'test -z "$ANTHROPIC_API_KEY" && echo NO-APIKEY || echo FATAL-APIKEY-PRESENT'

# 0.4 SSH multiplexing for latency control under concurrency
ssh -O check gb-mbp 2>&1 || echo "add ControlMaster/ControlPersist to ~/.ssh/config for gb-mbp"
```
> **The single most likely silent prod failure:** if `$CLAUDE_CODE_OAUTH_TOKEN` is empty over *non-interactive* ssh, the worker's token is in `~/.zprofile`/`~/.zshrc` (login/interactive only), not `~/.zshenv` (sourced by non-interactive ssh). Fix the worker dotfiles **now**, or the daemon's spawns will be unauthenticated.

**Verification primitives — the reusable proof set (FIX #6 applied: author-host DROPPED):**

| # | Proof | How |
|---|---|---|
| V1 | Worktree existed **on gb-mbp** during the run | `ssh gb-mbp -- 'ls -d /Users/gb/harmonik-worker/repo/.harmonik/worktrees/<run_id>'` succeeds *while running*; **and is ABSENT on box A** (`ls /Users/gb/github/harmonik/.harmonik/worktrees/<run_id>` fails) |
| V2 | Run metadata stamps the worker | `run_started`/`run_completed` carry `worker_name=gb-mbp`, `worker_os=darwin` (jq events.jsonl by run_id; `workloop.go:2261-2267`/`3829-3831`). Empty ⇒ ran locally ⇒ **test invalid** |
| V3 | Run branch pushed **from the worker** | `pushRunBranchOnWorker` ran (events) and `git -C /Users/gb/github/harmonik ls-remote origin run/<run_id>` exists; box A only *fetched* it pre-merge (`fetchRunBranchBoxA`, `codesync_rs_b8.go:80`) |
| V4 | Box A merge-only (no local spawn for this run) | events.jsonl shows NO local-substrate spawn for run_id; merge commit on the target bears `Harmonik-Bead-ID: <bead>` |
| V5 | **Content-level remote proof** (FIX #6 (d)) | the bead deliverable bakes in `hostname` output; assert the merged file contains `gb-mbp` — the only proof that the *agent* ran on the worker, not just that the worktree lived there |
| V6 | **Deterministic** subscription billing (FIX #1/#3/#7) | (a) D2 dispatch-time refusal works (Phase 6 negative test); (b) spawn spec carries empty `ANTHROPIC_API_KEY=` override; (c) `ssh gb-mbp -- 'launchctl getenv ANTHROPIC_API_KEY || echo unset'` = unset. Anthropic Console `$0` API-delta = **corroborating only**, not a hard gate |
| V7 | Local fallback byte-identical (NFR7) | `enabled:false` control run ⇒ V2 fields empty + worktree on box A + argv unchanged vs pre-feature |

**Daemon hygiene + monitor (every phase boundary):**
```bash
go install ./cmd/harmonik
harmonik supervise restart --project /Users/gb/github/harmonik --watch-restart   # re-reads workers.yaml
# "(no socket)" for ~30s–1m during restart-backoff is NORMAL — do NOT call it dead from one snapshot
harmonik queue status                                  # exit 17 = still booting; poll, never start a 2nd daemon
tmux has-session -t harmonik-<hash>-default 2>/dev/null || \
  tmux new-session -d -s harmonik-<hash>-default -c /Users/gb/github/harmonik   # FIX #12: assert -default present
harmonik subscribe --types run_started,run_completed,run_failed,run_stale,worker_unhealthy,worker_offline,reviewer_verdict,merge_conflict,heartbeat --heartbeat 60s --json
```

**Exit criterion:** 0.1–0.4 green; device-ids differ (real FS); worker Go pinned to box A's; OAUTH-OK + NO-APIKEY; daemon up on the fresh binary with `-default` present; subscribe armed.

---

## Phase 1 — Trivial real bead on gb-mbp (single, then a few)

**Objective:** ONE real bead — create-and-commit a text file — runs end-to-end on gb-mbp and merges to box A's `main`. Use **single mode** to deliberately bypass the DOT commit-gate the gating bug breaks, isolating "does remote processing work at all" from "does the gate work remotely."

**Setup:**
```bash
# Edit .harmonik/workers.yaml: enabled: true, max_slots: 1 — COMMIT before restart (FIX: never leave main tree dirty)
git -C /Users/gb/github/harmonik add .harmonik/workers.yaml
git -C /Users/gb/github/harmonik commit -m "test(remote): enable gb-mbp max_slots=1 (phase 1)"
harmonik supervise restart --project /Users/gb/github/harmonik --watch-restart
harmonik supervise status --json   # confirm worker loaded + B6 health-check passed (no worker_unhealthy)
```

**Beads** (real, trivial; deliverable bakes in `hostname` for V5):
```bash
br create --title "remote-smoke-1: write hostname into docs/remote-smoke/host-1.txt" \
  --type=task --priority=1 --label codename:remote-substrate-test \
  -d 'Run `hostname` and write its output as the single line of docs/remote-smoke/host-1.txt. Commit with Refs: <this bead>. No other changes.'
br show hk-<smoke1>                                                  # status open
git -C /Users/gb/github/harmonik log --all --grep "Refs: hk-<smoke1>" --oneline | wc -l   # expect 0
```
**Dispatch — single mode, and ASSERT it actually is single (FIX #15):**
```bash
harmonik queue submit --beads hk-<smoke1>          # capture queue_id + run_id
harmonik queue status --json | jq '.. | .workflow_mode? // empty'   # MUST show "single" — beadsToQueueDoc once stripped it (live regression class)
```

**Assertions:** run reaches `run_completed`; **V1** (worktree on gb-mbp during run, absent on box A), **V2** (`worker_name=gb-mbp`), **V3** (run branch from worker), **V4** (merge on `main` w/ trailer), **V5** (`git -C /Users/gb/github/harmonik show origin/main:docs/remote-smoke/host-1.txt` == `gb-mbp`), **V6** (no API burn). Bead auto-closed by daemon.

**1.2 — a few simple beads:** create 3 more trivial beads on **distinct** files (avoid same-file merge auto-skip), submit as a `stream` group, drain serially at `max_slots:1`. Assert all 4 on `main`, all V2-stamped `gb-mbp`, **one-at-a-time merge held** (`merge_conflict`=0).

**Expected failures + resolution:**
- `worker_unhealthy` at boot → B6 probe failed. `harmonik supervise logs --lines 200`; re-run Phase-0 checks over ssh. Classify **worker-provisioning** → fix on worker, re-enable.
- `run_stale` / no spawn at `launch_initiated` → spawn-semaphore (hk-4l7zs class) or ssh ControlMaster. `ssh gb-mbp -- 'tmux ls; pgrep -fl claude'`; classify provisioning vs daemon-code.
- `run_failed no_commit` in single mode → implementer didn't commit. `ssh gb-mbp -- git -C <wt> log --oneline -3`. Classify **bead-content** (vague prompt) → fix the **description** (not a trailing comment guard), re-dispatch ONCE (never >2× without an investigator).
- **DOT `chdir/no-such-file` appearing in single mode** → single-mode wasn't honored (the `workflow_mode` strip regression). STOP; fix the submit path before proceeding.

**Exit criterion:** ≥4 trivial beads processed on gb-mbp, all merged to `main`, V1–V6 green, zero local fallback, one-at-a-time merge held, single-mode verified in metadata.

---

## Phase 2 — Concurrent worktrees on gb-mbp (multi-slot)

**Objective:** prove multiple git worktrees run simultaneously on gb-mbp without collision, and box A's centralized one-at-a-time merge holds; also exercise the **conflict→auto-skip→fresh-queue recovery** path (FIX #14). Still single mode (DOT comes Phase 3).

**Setup — raise BOTH ceilings (FIX #17):**
```bash
# workers.yaml: max_slots: 4 — COMMIT, then restart so the boot-cached cap updates:
git -C /Users/gb/github/harmonik add .harmonik/workers.yaml && git -C /Users/gb/github/harmonik commit -m "test(remote): max_slots=4 (phase 2)"
harmonik queue set-concurrency 4                                     # global cap
harmonik supervise restart --project /Users/gb/github/harmonik --watch-restart
harmonik supervise status --json | jq '{global:.max_concurrent, worker_slots:.workers[]?.max_slots}'   # assert BOTH ≥4
```

**Beads:** 5 trivial, mutually-non-conflicting beads (distinct files under `docs/remote-conc/`, each baking `hostname`), submitted as a **`wave`** group (the unambiguous concurrency primitive):
```bash
harmonik queue dry-run /tmp/wave5.json && harmonik queue submit /tmp/wave5.json   # groups[0].kind="wave", 5 items
```

**Assertions:**
1. **Concurrency proven from the EVENT STREAM (FIX #9):** ≥2 `run_started{worker=gb-mbp}` with **overlapping run-id lifetimes** in events.jsonl (a second `run_started` before the first's `run_completed`). Corroborate — not prove — with `ssh gb-mbp -- 'pgrep -fc claude'` ≥2 and `ls .../worktrees/` showing ≥2 dirs. The `ls` poll alone is racy (false neg on fast runs, false pos on stale worktrees).
2. Each bead has its own `run/<run_id>` branch; no two share a worktree path.
3. **Merge serialization:** merge commits on `main` are strictly sequential (single parent chain, no interleaved merges) even though spawns overlapped — the merge mutex held. All 5 land; V2/V5/V6 green.
4. **Slot cap:** never >4 concurrent worktrees; a 6th submission waits for a free slot.
5. Worktrees GC'd on completion (`ls .../worktrees/` empties).

**2.1 — conflict→auto-skip→recovery (FIX #14):** submit two concurrent beads that touch the **same** file. Assert the second merge **auto-skips** (skip event fires), and the skipped bead is **re-dispatchable only via a FRESH queue** (re-adding the bead_id to the same stream stays ineligible per `streamEligible` dedup). Re-dispatch fresh after the first merges; assert it lands.

**Expected failures + resolution:**
- distinct-file beads still conflict → investigate base-SHA sync (DEC-B step 2/7), `codesync_rs_b8.go`. Classify daemon-code.
- spawn-semaphore wedge under concurrency → bounded-wait, trace by **run_id** (not bead_id). If a real wedge reproduces on a load-bearing concurrency claim → **major-issue fan-out** (10–15 parallel investigators on distinct angles: ControlMaster saturation vs slot-leak vs CPU saturation, + ≥2 adversarial verifiers; never declare root cause from one snapshot; check `ssh gb-mbp -- uptime` first — CPU saturation masquerades as daemon flakiness).

**Exit criterion:** ≥2 overlapping run-id lifetimes on gb-mbp (event-stream proof); all distinct-file beads merged serially; slot cap honored; conflict→auto-skip→fresh-queue recovery demonstrated; GC clean.

---

## Phase 3 — Fix the gating bug, then full DOT-mode on gb-mbp (GATING SEQUENCE)

> **Hard sequencing:** the gating bug MUST be fixed and validated on gb-mbp before any DOT-mode bead can pass. The commit_gate + auto-status shell nodes (`go build`/`go vet`/`scenario-gate.sh`) run via `dispatchDotToolNode`'s **local `/bin/sh -c` with `cmd.Dir = <worker path>`**, which chdir-fails on box A.

### Phase 3a — Resolve the gating bug (reproduce → fix → redeploy → re-validate)

**Reproduce first (no fix-and-pray):** switch the daemon to DOT mode, dispatch ONE trivial DOT bead to gb-mbp. **Capture the expected signature — `chdir` / "no such file", NOT empty-PATH / `go: command not found` (FIX #1):**
```bash
harmonik queue submit --beads hk-<trivial-dot>
RID=<run_id>
jq -c "select(.run_id==\"$RID\" and (.type|test(\"gate|status|run_failed\")))" /Users/gb/github/harmonik/.harmonik/events/events.jsonl
ssh gb-mbp -- "cat /Users/gb/harmonik-worker/repo/.harmonik/worktrees/$RID/.harmonik/commit-gate.log 2>/dev/null"   # gate log (dot_cascade.go:1261)
```

**Fix — dispatch an investigator + fixer, anchored to durable artifacts (orchestrator does NOT debug inline):**
> "Start at `internal/daemon/dot_cascade.go:1229` (`dispatchDotToolNode`). It runs `exec.CommandContext(ctx,\"/bin/sh\",\"-c\",node.ToolCommand)` with `cmd.Dir = wtPath` **locally on box A** — for a remote run `wtPath` is the worker's path, so it chdir-fails. The env is already correct (hk-m5axg `os.Environ()` at :1218). The fix is NOT env forwarding. Thread the **per-run `CommandRunner`** (the SSHRunner for a remote run; `LocalRunner` for local) into `dispatchDotToolNode` and run the shell node ON the run's substrate — `ssh worker -- /bin/sh -c '<cmd>'` with `cd <wtPath>` on the **remote** side — mirroring `fetchBaseOnWorker`/`pushRunBranchOnWorker` in `internal/daemon/codesync_rs_b8.go`. Keep `LocalRunner` byte-identical (NFR7). Find every call site of `dispatchDotToolNode` and thread the runner from the per-run substrate. Report root cause + fix in <200 words."

A second live gap is already recorded on the bead — **DOT HEAD-resolve `resolveWorktreeHEAD … chdir … no such file`** (same class: a DOT step running locally against the worker path). Have the investigator confirm whether the claimed HEAD-resolve fix is actually deployed on this binary; if `chdir` recurs at HEAD-resolve, it's the sibling of the gate bug — same fix pattern (`dot_cascade.go` HEAD-resolve + agentic-spawn routing through `perRunSubstrate`'s runner).

**Author the scenario test correctly (FIX #19):** the regression test is a `//go:build scenario` test that boots a **real remote daemon against gb-mbp (NOT localhost)**. It will time out on the daemon's 30-min commit budget if dispatched as a normal bead — **author it via a worktree sub-agent + fast local gate + cherry-pick** (the scenario-test-authoring convention; the daemon gate skips `//go:build scenario`). Add a fast unit test asserting the shell node's produced argv targets the run's runner and that `LocalRunner` is unchanged.

**Deploy cycle (the standing loop):**
```bash
gofumpt -l internal/ cmd/                          # gofumpt v0.7.0 — must be EMPTY output (version mismatch = gate red)
go build ./... && go vet ./...
go install ./cmd/harmonik
harmonik comms send --from captain --broadcast -- "Daemon restart for gating-bug fix; hold ~2m"
harmonik supervise restart --project /Users/gb/github/harmonik --watch-restart
tmux has-session -t harmonik-<hash>-default 2>/dev/null || tmux new-session -d -s harmonik-<hash>-default -c /Users/gb/github/harmonik
harmonik queue status
```
A separate **reviewer agent** (or fresh-context re-read) approves the fix before merge (review gate not optional).

**Re-validate:** re-run the trivial DOT bead → the commit-gate now passes **on gb-mbp** (gate log lives on the worker; `chdir` gone).

### Phase 3b — Full gated DOT (implement → commit_gate → review → close) on gb-mbp

**Beads:** 3 real beads exercising the whole graph: a small Go change that genuinely compiles + has a unit test (so `go build`/`go vet`/auto-status run meaningfully on the worker). `max_slots:1` first (clean signal), then 4.

**Assertions:**
1. **commit_gate ran ON gb-mbp:** the gate executed in the worker's worktree (gate log at `gb-mbp:.../<run_id>/.harmonik/commit-gate.log`); no `chdir`/empty-PATH symptom.
2. **Reviewer loop ran on BOX A (DEC-C) — proven by LOCATION (FIX #5):** `reviewer_launched`+`reviewer_verdict` fire; during the review window, the reviewer's worktree exists under **box A's** `.harmonik/worktrees/` AND is **absent** on gb-mbp (`ssh gb-mbp -- ls …`). APPROVE is the sole inbound edge to close (`standard-bead.dot` SOLE-inbound invariant); commit carries `Reviewed-By:`/`Review-Verdict:` trailers.
3. **auto-status** (`autostatusmarker.go`) ran its build on the worker and wrote the correct status class.
4. **Escalation: commit-gate cap-exhaustion → `close-needs-attention` (FIX #2).** Dispatch a bead whose implement step cannot satisfy the gate; assert the implement↔commit_gate loop runs its **3-iteration cap** then reaches the unconditional **`close-needs-attention`** fallback on the worker — NOT a wedge, NOT a silent re-loop.
5. **scenario-gate fail-open vs fail-closed branch (FIX #3).** Prove a *transient* infra error in `scenario-gate.sh` on the worker → `failure_class=transient` → self-loop retry (capped at 2), vs a genuine `go build`/`go vet` failure → `failure_class=deterministic` → implement loop. (Maps to `dispatchDotToolNode`'s exit→outcome table at :1208-1216.)
6. V2/V5/V6 across all iterations; final commit on `main`.

**Expected failures + resolution:**
- commit_gate still chdir-fails after the fix → fix incomplete (a call site of `dispatchDotToolNode` not threaded). Capture the exact remote command the daemon issued, run it by hand over `ssh gb-mbp`. Classify daemon-code; iterate.
- reviewer accidentally ran on the worker / mutated its worktree → DEC-C violated → file bug, daemon-code.
- reviewer-stall (committed, reviewer hung) → known **slow-recovery**, recovers at the reviewer's 30-min ceiling; do NOT escalate before launch+30min. If `verdict absent`, salvage via `git cherry-pick -x <worker sha>` + build/test + push.

**Exit criterion:** gating bug fixed + reviewer-approved + merged (with a gb-mbp scenario test); ≥3 full DOT beads pass on gb-mbp with commit_gate (on worker), reviewer (on box A, location-proven), auto-status all exercised; cap-exhaustion→close-needs-attention and transient/deterministic gate branches both demonstrated.

---

## Phase 4 — Full non-trivial multi-file code workflows on gb-mbp

**Objective:** exercise the whole pipeline on real, multi-file Go changes with genuine build/test gates running on the worker — the production workload and the offload's reason to exist.

**Setup:** DOT mode, `max_slots:2`, global cap ≥2.

**Beads:** 2–3 real beads from the backlog (`kerf next` / `br ready --limit 0`) or purpose-built, that: touch ≥2 files; compile + pass `go test` for the touched package (a genuine gate, not a no-op); are scoped to **distinct files/packages** so concurrent runs don't conflict (same-package test-helper redeclaration is a known collision class — namespace per bead or land serially); carry `codename:` so kerf attaches them. Submit as a stream group, append as slots free.

**Assertions:**
1. `go build`/`go vet`/affected-package tests + `scenario-gate.sh` ran **on gb-mbp** and reflect the real change — proven by the worker's gate log + a deliberately-failing variant looping back. **Offload visible:** `ssh gb-mbp -- uptime` rises while box A `uptime` stays low during the gate window.
2. Multiple non-trivial worktrees coexist on gb-mbp (Phase-2 overlap proof) each carrying a full DOT lifecycle (Phase-3 reviewer proof).
3. All land on `main`, serialized merge, V2/V5/V6 on every run.
4. A bead whose gate **legitimately fails** on the worker (e.g. dependency drift) surfaces as a clean `run_failed` with the gate output — not a wedge.

**Expected failures + resolution:**
- **worker dependency / toolchain drift** → gate fails on the worker; V2 names gb-mbp. Classify **worker-provisioning**: `ssh gb-mbp -- 'brew install <dep>'` or dispatch a "provision gb-mbp: <dep>" task; pin worker Go == box A Go (Phase 0); re-dispatch. This is *expected operational drift*, not a code defect.
- base-SHA staleness surfacing as a merge conflict → designed safety net (auto-skip + re-dispatch fresh).

**Exit criterion:** ≥2 real multi-file Go beads with genuine build/test gates run on gb-mbp under DOT, concurrently, land on `main`; one deliberately-failing gate proves clean failure + recovery; box A demonstrably offloaded.

---

## Phase 5 — Integration-branch enterprise workflow (accumulate → reviewed promote to protected main)

**Objective:** prove the enterprise pattern — a *series* of remote beads accumulate on `integration` (none touch protected `main`), then a reviewed `harmonik promote --pr` opens the `integration→main` PR; push-to-main is refused fail-closed.

**Setup (flags + `branching.yaml` — NOT `init`, which is fail-closed exit 1 for non-main targets until hk-m8vy2):**
```bash
git -C /Users/gb/github/harmonik checkout -b integration main && git -C /Users/gb/github/harmonik push -u origin integration
git -C /Users/gb/github/harmonik checkout main
cat > /Users/gb/github/harmonik/.harmonik/branching.yaml <<'YAML'
version: 1
defaults:
  lands_on: integration
  protect_branches:
    - main
YAML
git -C /Users/gb/github/harmonik add .harmonik/branching.yaml && git -C /Users/gb/github/harmonik commit -m "test(remote): land on integration, protect main"
go install ./cmd/harmonik
# Supervisor-managed restart with integration flags via keeper env (HK_TARGET_BRANCH=integration,
# HK_PROTECT_BRANCH=main, --forbid-default-main); NEVER pkill+background-launch (FIX #16):
harmonik supervise restart --project /Users/gb/github/harmonik --watch-restart
harmonik supervise status --json | jq '{target:.target_branch, protected:.protect_branches}'   # assert target=integration
```

**5.0 — PRE-GATE the branching config (FIX #4):** dispatch ONE throwaway bead to gb-mbp and assert its merge landed on **`integration`**, not `main`. **Fail the phase here** if it leaks to main — don't discover leakage after a 5-bead series.

**5.1 — the series:** 4–5 real beads (mix trivial + one multi-file Go change), all to gb-mbp in DOT mode, as one stream group, appending as it drains.

**Assertions (accumulation):**
1. Each merge lands on **`integration`**: `git -C /Users/gb/github/harmonik log --oneline origin/integration` shows all N trailers; `git log --oneline origin/main` shows **none**.
2. The daemon **never pushed `main`** (protect-branch deny-list fail-closed): no `main` push events for these runs.
3. All N V2-stamped `gb-mbp`; V5/V6 across the series.

**Promote (in a VERIFIED queue lull — FIX #11):**
```bash
harmonik queue status --json | jq 'select(.active>0 or .merging>0)'   # MUST be empty (0 active/merging) before promote
harmonik promote --pr --from integration --target main \
  --title "remote-substrate Phase 1 sprint" --body "N beads run on gb-mbp"
harmonik promote --target main <reviewed-sha>          # push-mode against protected main → MUST exit 5
```
**Assertions (promote):**
4. `promote --pr` opens a real `integration→main` PR via `gh` (`gh pr view`), pushes **nothing** to the target. (PR-mode is race-safe; push-mode is the only mode that races the daemon — hence the lull gate.)
5. `promote --target main <sha>` (push-mode) is **refused exit 5** — the enterprise guardrail proven.
6. (Optional positive push-mode on the *unprotected* target: `harmonik promote --target integration <reviewed-sha> --dry-run` previews cleanly.)
7. After human `gh pr merge`, all N commits are on `origin/main`; **`harmonik reconcile --target-branch integration`** closes any bead left `in_progress` whose work merged to **integration** (FIX #13 — reconcile against the target branch, not main, since accumulation landed on integration).

**Partial-series recovery (FIX #13):** if a mid-series bead fails or the daemon dies, `integration` holds a partial sprint — it's **resumable**: re-dispatch the failed bead onto `integration`, and only `promote --pr` once the intended set is whole. Don't promote a partial set.

**Expected failures + resolution:**
- a bead lands on `main` → branching config not active. Confirm `supervise status --json` resolved target=integration and `branching.yaml` loaded; classify config/deploy; restart. (5.0 catches this before the series.)
- `promote --pr` exit 1 → `gh` not on PATH / not authed → fix env, retry.
- `promote --target main <sha>` did NOT exit 5 → protection gate broken → P1 daemon-code bead, issue-resolution loop.

**Exit criterion:** the 5.0 pre-gate passed; N remote beads accumulated on `integration` (zero on `main`); daemon never pushed `main`; push-mode-to-main refused exit 5; `integration→main` PR opened via `promote --pr` in a verified lull; post-merge all on `main`; reconcile (against integration) clean.

---

## Phase 6 — Resilience: worker offline / partition, D2 dispatch-time refusal, NFR7 fallback

**Objective:** prove a worker dying mid-run recovers (never silently wedges) and is observable; prove the D2 billing guardrail refuses at dispatch; prove byte-identical local fallback.

**6.1 — two SEPARATE failure modes (FIX #8 — NEVER `pkill -f sshd`):**
- **Transport partition** (→ `worker_offline`): dispatch a longer bead; once `run_started{worker=gb-mbp}` fires and the worktree exists, `ssh gb-mbp -- 'sudo ifconfig en0 down'` (or `tailscale down`). Assert: ssh-failure/255 (`IsSSHConnectionFailure`) → typed `worker_offline{worker_name=gb-mbp}` (workloop.go:2168/2188/2747) → run reaches a **recoverable terminal** (`run_stale` → re-queue fresh or clean-fail), **never a wedge**. Restore connectivity, re-dispatch, bead completes.
- **Agent death, transport alive** (→ no_commit/run_stale): `ssh gb-mbp -- 'pkill -f claude'`. Assert a different path: implementer gone but ssh alive → `no_commit`/`run_stale`, not `worker_offline`. These exercise distinct code; test separately.

**6.2 — recovery hygiene:** on reconnect, the daemon reconciles before re-dispatch — did a commit land + push before the drop? If yes, salvage via fetch/merge (avoid the dup-work trap); if no, re-dispatch. Orphaned remote worktree GC'd on the next orphan-sweep (over ssh): `ssh gb-mbp -- git -C <repo> worktree list` shows no leak.

**6.3 — D2 dispatch-time refusal (FIX #1/#3 — the deterministic billing proof):** set `ANTHROPIC_API_KEY` in the **daemon's own env**, restart, dispatch a remote bead. Assert the dispatch is **refused with the exact reason string** `remote run: ANTHROPIC_API_KEY in spawn env (D2 fail-closed)` (workloop.go:2713) — NOT merely "health-check failed." Then unset, restart, recover.

**6.4 — unhealthy-at-boot:** point the worker repo_path at a missing dir (or remove `claude` from PATH), restart → boot health-check marks it **unhealthy + skip** (config retained), emits `worker_unhealthy`, daemon falls back to **LOCAL** (NFR7) rather than dispatching to a broken worker.

**6.5 — NFR7 byte-identical fallback (V7):** flip `enabled:false`, restart, dispatch one bead → runs **locally**, V2 fields empty, worktree on box A, argv byte-identical to pre-feature. Re-run this control **after every substrate/`dot_cascade.go` change** as the regression guard.

**Expected failures + resolution:** any of the above that wedges (no recovery, no event) → daemon-code bug; investigator anchored to `internal/daemon/workloop.go` (notifyWorkerOffline ~2168/2188/2747) + `internal/workers/offline.go` + the run_id's events. If a wedge survives ≥2 fixes → major-issue fan-out.

**Exit criterion:** transport-partition → `worker_offline` + recoverable terminal + GC clean; agent-death → no_commit/run_stale (distinct path); D2 refusal string asserted at dispatch; unhealthy-boot skipped (config retained) with local fallback; NFR7 control byte-identical.

---

## Cross-phase Issue-Resolution Loop (the standing procedure)

When anything fails, in any phase:

**1. Detect** — `harmonik subscribe --json` surfaces `run_failed`/`run_stale`/`worker_offline`/`worker_unhealthy`. Match by **event TYPE, not bead_id** (`run_completed`/`run_failed` are keyed by run_id only; grepping by bead_id silently drops them).

**2. Diagnose — durable artifacts ONLY (orchestrator never reads code inline):**
```bash
jq -c "select(.run_id==\"$RID\")" /Users/gb/github/harmonik/.harmonik/events/events.jsonl          # which events fired vs expected
jq -c "select(.run_id==\"$RID\" and .type==\"run_started\") | {worker_name,worker_os}" .../events.jsonl   # placed remote?
ssh gb-mbp -- "git -C /Users/gb/harmonik-worker/repo/.harmonik/worktrees/$RID log --oneline -3; git -C … status; cat .../$RID/.harmonik/commit-gate.log"
```
Signatures: `run_started` but no commit-detect = stuck before commit; commit present but no `reviewer_verdict` = reviewer stall; **`chdir`/no-such-file at a gate/HEAD-resolve node = the gating-bug class (shell node ran locally on box A)**.

**3. Classify:**
- **bead-content** (vague, over-scoped into a forbidden artifact, wrong files) → fix the **description** (excise the lure; a trailing comment guard won't hold), re-dispatch **fresh** (re-adding to the same stream stays ineligible — use a fresh queue). Never re-dispatch the same bead >2× without an investigator.
- **worker-provisioning** (missing dep, toolchain drift, OAuth dotfile, push auth) → fix on gb-mbp via ssh, re-check Phase-0 gates, re-enable. Expected operational drift, not a code defect.
- **daemon-code** (shell node runs locally, worktree ordering, env, offline handling, slot leak) → file a bead, dispatch an investigator anchored to file:line + events.jsonl (never tmux scrollback), then a fixer + reviewer gate.

**4. Fix + deploy cycle (coordinated, supervisor-only):**
```bash
harmonik comms send --from captain --broadcast -- "Daemon restart for <bead>; hold ~2m"
# land the fix on main (daemon queue; or for harmonik-self-fix exceptions, worktree sub-agent + cherry-pick)
gofumpt -l internal/ cmd/                  # gofumpt v0.7.0 — empty output required
go build ./... && go vet ./...
go install ./cmd/harmonik                  # stale binary = #1 "but I fixed that"
harmonik supervise restart --project /Users/gb/github/harmonik --watch-restart
tmux has-session -t harmonik-<hash>-default 2>/dev/null || tmux new-session -d -s harmonik-<hash>-default -c /Users/gb/github/harmonik
harmonik queue status                      # confirm up ("(no socket)" ~30s–1m is normal); deploy banked SHAs ONLY in a true lull (0 merging — out-of-band cherry-pick races daemon merges, non-ff)
```

**5. Re-validate** the *exact* failing bead/assertion **on gb-mbp** (never localhost); re-run the Phase-6.5 NFR7 control after any substrate/`dot_cascade.go` change; confirm no regression in earlier phases.

**6. Major/recurring blocker** (root cause refuted ≥2× or wedge survives ≥2 fixes) → **major-issue fan-out**: 10–15 parallel investigators on distinct angles + ≥2 adversarial verifiers that can overrule the synthesis; use `harmonik subscribe --json`/`jq`, never hand-grep by run_id; captain never debugs inline.

---

## Feature-level DEFINITION OF DONE

Remote-substrate Phase 1 is **done** when ALL hold, **each proven on the real separate-filesystem worker gb-mbp (never localhost):**

1. **Trivial → processed:** ≥4 trivial real beads run end-to-end on gb-mbp and merge to box A's target branch; V1–V6 green; single-mode verified in run metadata; one-at-a-time merge held (Phase 1).
2. **Concurrency:** ≥2 git worktrees ran simultaneously on gb-mbp — proven by **overlapping run-id lifetimes in the event stream** (ls/pgrep corroborating only); box A's merge is provably serial under concurrent spawns; BOTH `max_slots` and global `--max-concurrent` cap honored; same-file conflict → auto-skip → fresh-queue recovery demonstrated (Phase 2).
3. **Gating bug resolved:** the DOT commit_gate + auto-status shell nodes execute **on the worker** via the per-run `CommandRunner` (re-anchored to `dot_cascade.go:1229 dispatchDotToolNode`; the chdir-on-box-A bug, NOT an SSHRunner env bug); `LocalRunner` byte-identical; covered by a `//go:build scenario` test that runs against **gb-mbp**, reviewer-gate-approved before merge (Phase 3a).
4. **DOT production path:** ≥3 full DOT beads pass on gb-mbp with commit_gate (on worker, gate log present), reviewer loop (**location-proven on box A**), and auto-status all exercised; commit-gate **cap-exhaustion → close-needs-attention** and **scenario-gate transient-fail-open vs deterministic-fail-closed** branches both demonstrated (Phase 3b).
5. **Non-trivial workloads:** ≥2 real multi-file Go beads with genuine build/test gates run on the worker, land on the target branch, reviewer-APPROVE'd; one deliberately-failing gate proves clean failure + recovery; box A demonstrably offloaded (Phase 4).
6. **Enterprise integration flow:** the 5.0 pre-gate passed; a series of remote beads accumulated on `integration` (zero leaked to `main`); the daemon never pushed protected `main`; `promote --pr` opened a reviewable integration→main PR in a verified queue lull; push-mode-to-`main` refused **exit 5**; post-merge all on `main`; `reconcile --target-branch integration` clean; partial-series recovery defined (Phase 5).
7. **Resilience + NFR7:** transport-partition → `worker_offline` + recoverable terminal (no wedge) + orphan GC; agent-death → no_commit/run_stale (distinct path); unhealthy-boot worker skipped with config retained + local fallback; a disabled/offline worker falls back to **byte-identical** local execution (Phase 6).
8. **Billing safety — deterministic, not dashboard-dependent:** the D2 dispatch-time refusal string `remote run: ANTHROPIC_API_KEY in spawn env (D2 fail-closed)` (`workloop.go:2713`) is asserted; spawn spec carries the empty `ANTHROPIC_API_KEY=`/`CLAUDE_CODE_OAUTH_TOKEN=` overrides; worker env has OAuth + no API key; Console `$0` API-delta is corroborating only (V6).
9. **Provenance:** every load-bearing remote run is proven ON the worker via V1–V5 — worktree on gb-mbp & absent on box A, `worker_name=gb-mbp`/`worker_os=darwin` in metadata, `run/<id>` branch pushed by `pushRunBranchOnWorker`, box A merge-only, and a `hostname`-baked deliverable containing `gb-mbp` (**author/committer-host is NOT used as proof** — it's identity config, identical across hosts).
10. **No regressions:** the Phase-6.5 NFR7 control re-passes after every substrate/`dot_cascade.go` change; `internal/lifecycle/tmux` + `internal/workers` unit tests green; binary clean under gofumpt v0.7.0; lifecycle was supervisor-only throughout (no mixed pkill+background launches).

**Sequencing rule (load-bearing):** Phases 1–2 use **single mode** (verified in metadata) to validate spawn/commit/push/merge/concurrency *without* tripping the gating bug. **The gating bug MUST be fixed (Phase 3a) before any full-DOT run (Phase 3b onward).** Phases 5–6 follow DOT green. Re-run the NFR7 control after every substrate change.

**Key code anchors (absolute):**
- Gating bug (re-anchored): `/Users/gb/github/harmonik/internal/daemon/dot_cascade.go:1229-1264` (`dispatchDotToolNode` — local `/bin/sh -c`, `cmd.Dir = wtPath`); fix pattern: `/Users/gb/github/harmonik/internal/daemon/codesync_rs_b8.go` (`fetchBaseOnWorker`/`pushRunBranchOnWorker`/`fetchRunBranchBoxA:80`).
- D2 refusal: `/Users/gb/github/harmonik/internal/daemon/workloop.go:2713`; strip/override: `/Users/gb/github/harmonik/internal/handler/claudehandler_chb006_024.go`.
- FR13 worker metadata: `/Users/gb/github/harmonik/internal/daemon/workloop.go:2261-2267, 3829-3831`.
- worker_offline path: `/Users/gb/github/harmonik/internal/daemon/workloop.go:2168,2188,2747`; `/Users/gb/github/harmonik/internal/workers/offline.go`.
- SSHRunner (argv tunnel — *not* the gating bug): `/Users/gb/github/harmonik/internal/lifecycle/tmux/runner.go:92`.
- DOT graph: `/Users/gb/github/harmonik/internal/daemon/standard-bead.dot`; auto-status: `/Users/gb/github/harmonik/internal/workspace/autostatusmarker.go`.
- Worker config: `/Users/gb/github/harmonik/.harmonik/workers.yaml` (gb-mbp, `enabled:false` until the fix); Phase 5 creates `/Users/gb/github/harmonik/.harmonik/branching.yaml`.
- Lifecycle skill (promote exit-5 / supervise): `/Users/gb/github/harmonik/.claude/skills/harmonik-lifecycle/SKILL.md`; queue model (wave/stream): `/Users/gb/github/harmonik/specs/queue-model.md`.