# RUN-LOG — Assessor Daemon Campaign

| Field | Value |
|---|---|
| PASS_ID | `exploratory-2d308836` |
| PASS_KIND | exploratory (PROVISIONAL — not admiral-gating; only a baseline pass produces the gating ASSESSMENT) |
| PIN_SHA | `2d3088363e9fea9ae82c78fa7816a7e148053516` |
| PIN_BRANCH | `phase1-session-restart-substrate` |
| SANDBOX | `/tmp/h-assessor/scratch-exploratory-2d308836` |
| BINARY | `<scratch>/.harmonik/bin/harmonik` (built FROM pinned clone; ldflags commitHash=PIN_SHA) |
| ENV | `TMPDIR=/tmp/h-assessor` `GOCACHE=/tmp/h-assessor/gocache` `CODEX_HOME=/tmp/h-assessor/codex` · `OPENAI_API_KEY` unset · `HK_PROJECT` unset |
| STARTED | 2026-07-18 (UTC; see blocks below) |
| EXECUTOR | assessor (harness claude) |
| CODEX | minimal — matrix rows pre-marked SKIPPED per operator "codex minimal" |

## Green-tree precondition (§0.D.4) — at PIN_SHA

| Check | CMD | Result | ASSERT |
|---|---|---|---|
| build | `go build ./...` | exit 0, no output | PASS |
| vet | `go vet ./...` | exit 0, 0 lines | PASS |
| test -race | `go test -race -count=1 ./...` | **RED** — see red-tree block below | **FAIL (precondition)** |

## Known deviations (flagged, not silently passed)

- **DEV-1 (§4A.5 tmux server isolation):** the sanctioned substrate tool `scripts/scratch-daemon.sh`
  runs the scratch daemon on the **default** tmux server under a path-hash-unique session name
  (`harmonik-<projecthash>-default`), NOT the `tmux -L h-assessor` dedicated server §4A.5 mandates.
  Collision-safety (the actual risk §4A.5 guards) is independently provable: the session name is keyed
  off SHA-256(realpath(scratch)), so it cannot collide with the OFF fleet daemon nor the two running
  scratch daemons under `/private/tmp/h/{ws44-build,pi-verify}`. Full tmux-server isolation is a
  **campaign-harness gap** to harden (per PLAN operator decision 4: "harden into a scripted harness
  after the first run teaches the shape"). Recorded; does not block the provisional exploratory pass.
  The assessor did NOT edit fleet tooling to force `-L` (out-of-bounds fleet-state mutation).

---

## Blocks (append-only)

### [09:45 PDT] SANDBOX · §4A pre-flight + daemon up — HARD GATE
- CMD: `scenarios/preflight-4A.sh` → `PREFLIGHT_SUMMARY fails=0`; `scratch-daemon.sh up`.
- OBSERVED: all 9 §4A checks PASS (2 WARN: no workers.yaml → local dispatch; origin=`/Users/gb/github/harmonik` local path, NOT GitHub → flagged for G5). Daemon PID 6632, socket `$SBX/.harmonik/daemon.sock` present, boot log `harmonik daemon starting in /private/tmp/h-assessor/scratch-…` (reports **$SBX**, not real path). Boot log error/panic/fatal count = **0**. No ANTHROPIC/OPENAI key in daemon env (`env -u` strips them).
- ASSERT §4A.9 dry-boot: daemon reports $SBX project dir + binds sandbox socket → **PASS**.
- ASSERT scratch isolation (socket-scoped): `queue list --project $SBX` → "(no active queues)"; scratch `ops-monitor/latest.json` `queues:[]` → scratch daemon is **CLEAN, 0 queues**. PASS.
- ARTIFACT: `scenarios/preflight-4A.sh`, `/tmp/h-assessor/real-env-snapshot.txt`, `$SBX/.harmonik/scratch-daemon.log`.

### DEV-2 (evidence-hygiene) — `harmonik state` is CWD/HK_PROJECT-scoped, ignores --project/--socket
- `state_cmd.go:106-118` `resolveProjectDirForState()` reads `HK_PROJECT` else `os.Getwd()`/`findProjectRoot`; it does **not** honor `--project`/`--socket` (only `--json` per `state --help`). Run from the real repo root (as CWD discipline mandates), `harmonik state` connects to the **real** fleet daemon and prints its 46 queues — NOT the scratch's. This is the same read-leak class as §4A.7 (`usage`). **Consequence for the campaign:** `state` MUST NOT be used for scratch evidence; all scratch reads use socket/path-scoped queries (`queue list --project $SBX`, `subscribe --json`, scratch `events.jsonl`). Extends the §4A.7 known-un-isolable-read ack to include `state`. Severity Tier-2 (as-designed CWD scoping; misleading for multi-daemon workflows). Not gating.

### [09:53 PDT] GREEN-TREE PRECONDITION — **RED at PIN_SHA** (§0.D.4) — MAJOR
`go test -race -count=1 ./...` at the pin is NOT green. Failures triaged real-vs-confounded by
re-running the deterministic failers in **isolation** (no `-race`, quiet):

**F-1 (REAL, product code) — hooksystem `hook_fired` not emitted.** `internal/hooksystem`
CP042/CP017/CP016 all FAIL in isolation (0.65s, no -race):
- `dispatcher_cp042_test.go:491` CP-042.4: hook_fired not emitted for **replay** path; events=[agent_started]
- `dispatcher_cp017_test.go:249` CP-017.1: hook_fired not emitted for **cognition-hook success**; events=[agent_started]
- `dispatcher_cp016_test.go:264` CP-016: no hook_fired event when **idempotency_class omitted**
Common symptom: dispatcher emits only `agent_started`, never `hook_fired`. Hook-dispatch path is
core-loop-adjacent (risk floor ↑). CONFIRMED real (not load/-race). Evaluator still RUNS (CP017
callCount==1) — hooks fire but the `hook_fired` signal/side-effect emission is broken.
ATTRIBUTION: correlates with **`7bee7b89f`** `fix(hooksystem,hook): guard Gate entries…(W4)` (today
07:26) — the only recent `dispatcher.go` change; it added a `KindHook` filter at the fire path
(early-returns nil when no KindHook entries remain). Likely a fresh W4 regression on the wave the captain
is landing. Precise bisect deferred to baseline. → filed bead hk-okzy1 (annotated).

**F-2 (REAL, product code, codex path) — codexdriver dropped approval reply (RU-07).**
`internal/codexdriver` TestServerRequestAnswered FAILs in isolation (0.58s): "twin never received the
driver's approval reply — server request was dropped (RU-07)"; driver auto-declines execCommandApproval
id=999 → reply method-not-found. codex is "minimal" per operator, so weighted lower for THIS gate, but
it is a real tree-health fail. → filed bead.

**N-1 (NOT a product finding) — eval fixture.** `evaltasks/eval-bugfix-rate-limiter` TestLimiter FAIL +
DATA RACE at `limiter_test.go:52` is the **deliberately-buggy eval fixture** (the bug agents are meant to
fix), not product code. Excluded.

**INCONCLUSIVE → RESOLVED (mostly NOT real) — internal/daemon.** Original big `-race` run hit `panic:
test timed out after 10m0s` (601s) with many subtest FAILs under 4-daemon CPU contention. Re-ran the
*fast* failers in **isolation** (no -race, quiet box) to separate logic from load:
- `TestWorkLoop_ShutdownDrainsCommittedRun_hkdnrg` → **PASS** in isolation (load-flake, not real).
- `TestScenario_OperatorNFR_PauseWithRunInFlight` → **PASS** in isolation (load-flake, not real).
- `TestDaemonStart_SocketBindsBeforeRestartBackoffSleep` → FAIL, but the daemon's own log shows the cause:
  socket path `/tmp/h-assessor/TestDaemonStart_…1627097164/001/.harmonik/daemon.sock` is **104 bytes, at
  the macOS `sun_path` 104-byte limit** → bind fails EINVAL. **Environmental** (long TMPDIR + long test
  name), not a product bug. (Arguably the test should use a shorter tmpdir; minor test-hygiene note, not a
  gate finding. NB the daemon emits a *helpful* diagnostic about the limit — good behavior.)
So the `internal/daemon` cluster is **not a confirmed product-code red**; the timeouts were load, the fast
FAILs were flakes/env. The throughput/scenario timeouts (TestThroughput_TenBeadsAtMaxFour,
TestT6_10BeadSequentialDrain, TestScenario_ReviewLoop_ResumeReseed…) remain DEFERRED to a quiet-box
isolated re-run for the baseline, but on current evidence they read as load, not logic.
**Net: confirmed-real red tree = F-1 (P1) + F-2 (P2, codex path) only; F-3 watchdog (real, live-corroborated).**

**Assessment impact:** green-tree precondition is a stated gate input (§0.D.4). At least two real
product-code failure clusters exist at PIN_SHA, one on the core-loop-adjacent hook-dispatch path. The
exploratory verdict trends **BLOCK (provisional)** on precondition grounds. Mitigating context: tree is
MOVING (captain landing mediums); these may be in-flight. The authoritative call is the baseline pass at
the clean SHA. Reported to admiral.

### F-3 (REAL) — supervisor-watchdog auto-revive of a dead supervisor is unreliable
The standalone scratch daemon's in-daemon `supervisor-watchdog` found no supervisor, ran
`harmonik supervise restart --watch-restart --project <scratch>` 3× (attempts 1–3, 30s pidfile-wait each),
each FAILED to produce `cognition/supervisor.pid`, hit `max_revives=3`, logged `level=ERROR
"revival cap reached — giving up"`, and the **watchdog thread exited** (daemon 6632 survives, now
permanently unsupervised). Directly contrasted: `harmonik supervise start --project <scratch>` succeeds in
~2s (pidfile 27319, `supervise status: running`, session `harmonik-<hash>-flywheel`). So `supervise` is
not broken — the watchdog's `restart --watch-restart` revive path is. On the daemon-lifecycle risk floor.
Trigger scope: only when the daemon is launched WITHOUT a pre-existing supervisor (standalone); in the
normal fleet flow the supervisor launches the daemon so the watchdog never enters this path. → filed bead.
Evidence: `$SBX/.harmonik/scratch-daemon.log` lines (attempts 1–3 + cap-reached), `/tmp/h-assessor/supervise-manual.log`.

### [09:55 PDT] F-3 CORROBORATED ON LIVE FLEET + comms-down blocker — MAJOR
The real fleet daemon (PID 22202, `--project /Users/gb/github/harmonik`) is now **DEAD**; socket gone.
Its `daemon-run.log` ends with the **identical F-3 cascade**: supervisor-watchdog ran
`supervise restart --watch-restart` attempts 1–3 (08:33–08:35), each failed to write the supervisor
pidfile, `level=ERROR "revival cap reached — giving up" max_revives=3`, watchdog exited — then the daemon
itself went down (last activity 09:37:43). So **F-3 (hk-ky7ye) is NOT standalone-only** — it occurred on
the live fleet deployment and preceded the fleet daemon dying unsupervised. Elevates F-3 severity: real
blast radius, daemon-lifecycle floor → trending Tier-0 for the baseline.
- I did NOT cause this: the scratch substrate provably cannot target the fleet daemon (scratch-daemon.sh
  guard_path + argv-ownership); I never signalled 22202; it was alive at 09:47 (pgrep) and died on its own.
- **Blocker:** the comms bus is served by the (now-dead) fleet daemon → `harmonik comms send` fails
  ("daemon not running"); `comms who` shows admiral stale 6m, captain stale 3m. I **cannot deliver the
  gate verdict to the admiral over comms** until the fleet daemon/comms is restored. Reviving the live
  fleet daemon resumes real dispatch = an operator/fleet-owner action, outside the assessor's verify-only
  bounds → surfaced to the operator. Interim gate status to admiral (hk-okzy1/8m2j4/ky7ye) is queued,
  undelivered.

### CONTEXT-1 — real fleet daemon IS running (PLAN "main daemon OFF" premise is stale)
- `pgrep`: real daemon PID **22202** (`--project /Users/gb/github/harmonik`, started 08:31 via a claude shell) + 3 scratch daemons (ws44-build 5111, pi-verify 24060, assessor 6632). Live tmux: `harmonik-a3dc45482890-{captain,crew-admiral,crew-assessor}`. Isolation is unaffected — the scratch (`ac34931f8deb`) shares no handle with any of them (§4A.5 PASS). Noted because the PLAN §4A live-environment note assumed only 2 scratch daemons + OFF main.
