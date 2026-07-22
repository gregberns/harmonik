# Codex-Substrate RE-GATE — Validation LEG A (self-contained)

Pin: `fff3d937` (HEAD == pin, `git rev-parse HEAD == fff3d937cebefc07ac74a58fbfaefbc72876ac66`;
branch `phase1-session-restart-substrate` HEAD; contains hk-daegv@a0619c1c sandbox fix +
hk-qxvc2@fff3d937 remote claude-config-dir fix).
Iso binary: `/tmp/h-assessor/legA-regate/bin/harmonik` → **`harmonik dev (commit: fff3d937…)`**
(FRESH build from a detached worktree at the pin, `go build -ldflags "-X main.commitHash=fff3d937…"`).
Iso namespace: `legA-regate` (distinct dir/socket/pidfile — no collision with any live daemon).
ENV: `TMPDIR=/tmp/h-assessor GOCACHE=/tmp/h-assessor/legA-regate/gocache`; host API keys unset for the dispatch leg.

## PROOF 1 — fail-closed isolation guard (unit) — GREEN
`go test ./internal/daemon/ -run TestCodexIsolationGuard_HK5H759 -v` → **PASS (6.07s)**, all 5 cases:
- `codex_crew_no_registry_REFUSED` — PASS (guard fired, `isolation-boundary` reason, reopening)
- `codex_crew_nonssh_worker_REFUSED` — PASS
- `codex_crew_disabled_worker_REFUSED` — PASS
- `flag_off_no_guard_baseline` — PASS (no-op; proceeded past routing-gate to CreateWorktree, guard absent)
- `codex_crew_with_boundary_ALLOWED` — PASS (guard allowed; proceeded to reverse-tunnel readiness gate,
  which then failed loud on the `.invalid` host — expected, proves guard let it through)

## PROOF 2 — fail-closed real-dispatch refusal — GREEN
Booted an iso codexdriver daemon (pid `76168`) on its own fresh project `/tmp/h-assessor/legA-regate/proj`
(`harmonik init --project … --no-supervise --prefix iso`, **no `.harmonik/workers.yaml`** → no boundary;
confirmed absent). Binary run **DIRECTLY** (not via scratch-daemon.sh) so `HARMONIK_SUBSTRATE` injects:
`env -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN -u OPENAI_API_KEY HARMONIK_SUBSTRATE=codexdriver`.
Created bead `iso-300` (label `harness:codex`), submitted via `harmonik queue submit --beads iso-300`
(queue_id `019f7bfb-6120-7a19-8998-21b866c5b6ee`).

Typed events (`jq` over `.type` in `.harmonik/events/events.jsonl`):
- **`run_started` count = 0** (`jq 'select(.type=="run_started")' | jq -s length` → 0)
- **`run_failed`** — full payload: `bead_id: iso-300`, `success: false`,
  `run_id: 019f7bfb-689d-7c8e-90d5-21a81fa6c734`,
  `summary: "codex isolation-boundary guard: refusing to launch a codex app-server crew with no enabled ssh worker boundary (danger-full-access would run unsandboxed on the daemon host) — enable an ssh-transport worker"`
  → reason string **contains `isolation-boundary`**. ✓
- `queue_paused` reason `group_failure`, fail_count 1 (queue halted, not retried into a spawn).
- **No local `codex` process spawned** (`pgrep -fl '[c]odex'` → none).

Guard fired at real dispatch on the daemon host, before any worktree/tunnel/codex-exec. Fail-closed confirmed end-to-end.

## PROOF 3 — local exec / ssh git-lifecycle (scenario) — GREEN
Localhost ssh key-auth available (`ssh -o BatchMode=yes -o ConnectTimeout=5 localhost true` → OK).
`go test -tags=scenario -run 'TestScenario_RemoteSubstrate_Localhost_E2E|…_NoWorker_RunStartedWorkerNameEmpty' ./internal/daemon/ -v`
with `HARMONIK_REQUIRE_REMOTE_E2E=1` (fail loud, don't skip) → **PASS (7.82s)**. Evidence:
- `TestScenario_RemoteSubstrate_Localhost_E2E` — **PASS (6.98s)**:
  worktree **created over real `ssh localhost`**; worker commit **synced over ssh and landed on box A main**
  — sha `1c9dbd53f04e9494be1d0b4118d9a523731ae589`; bead `hk-rs-b12-e2e-localhost` **closed=1 reopened=0**;
  **`run_started.worker_name="localhost"`** (non-empty — routed to the ssh worker, not silent-local).
- `TestScenario_RemoteSubstrate_NoWorker_RunStartedWorkerNameEmpty` — **PASS (3.97s)**:
  local run emitted **`run_started.worker_name=""`** (empty as required — negative guard holds).

**FIDELITY NOTE (not a defect):** Proof 3 proves the remote-substrate git lifecycle
(worktree-create-over-ssh + commit-land + one-at-a-time merge to box A) with fidelity, BUT the
"agent" is a **stub handler** (`/bin/sh`-style no-op); the run-branch commit is made by the test's
worktree factory, **not by a real `codex` subprocess**. No self-contained test in this pin exercises a
live codex process exec (that path needs a real remote worker + API creds). The faithful
codex-exec-into-boundary proof is **leg B's job** / an integration environment with a live ssh worker.

## Teardown — CONFIRMED CLEAN
- Killed (SIGTERM, order supervise→daemon): daemon `76168`, supervise `_shim` `79237` (internally spawned).
- `pgrep -fl legA-regate` → **none remain**; re-checked after 3s → **no revival**.
- Detached worktree `/tmp/h-assessor/legA-regate/src` removed (`git worktree remove --force`).
- Live fleet supervise **pid 21849 alive** (unchanged).
- Prod `/Users/gb/github/harmonik/.harmonik/workers.yaml`: sha `ef91bf42a92ede30674b230d530d8e99f73032c1` **UNCHANGED** (matches prior LEG-A baseline).

## Verdict
Proof 1 GREEN · Proof 2 GREEN · Proof 3 GREEN (fidelity-limit: stub handler, not real codex exec — leg B).
No defects filed (nothing broken). No merge, no fleet-state edits, no bead terminal-transitions performed.
