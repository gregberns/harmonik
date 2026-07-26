# Codex-Substrate Pre-Deploy Gate — Validation LEG A (self-contained)

Pin: `9db85569` (HEAD == pin, porcelain empty; contains hk-fufel PR#33, hk-czb11 PR#32).
Iso binary: `/tmp/h-assessor/codexA-iso/bin/harmonik` → `commit: 9db85569…` (built from pin clone).
ENV: `TMPDIR=/tmp/h-assessor GOCACHE=/tmp/h-assessor/gocache-codexA CODEX_HOME=/tmp/h-assessor/codex`, `OPENAI_API_KEY` unset.

## LEG 2a — fail-closed isolation guard (unit) — GREEN
`go test ./internal/daemon/ -run TestCodexIsolationGuard_HK5H759 -v` → **PASS (5.24s)**, all 5 cases:
- `codex_crew_no_registry_REFUSED` — PASS (guard fired, isolation-boundary reason)
- `codex_crew_nonssh_worker_REFUSED` — PASS
- `codex_crew_disabled_worker_REFUSED` — PASS
- `flag_off_no_guard_baseline` — PASS (no-op; proceeded to CreateWorktree, guard absent)
- `codex_crew_with_boundary_ALLOWED` — PASS (guard allowed; proceeded to reverse-tunnel readiness gate)

## LEG 2b — real-dispatch refusal — GREEN
Booted an iso codexdriver daemon (pid 41804) on its own project `/tmp/h-assessor/codexA-iso-proj`
(`harmonik init --no-supervise`, **no `.harmonik/workers.yaml`** → no boundary), env
`-u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN -u OPENAI_API_KEY HARMONIK_SUBSTRATE=codexdriver`.
Created bead `iso-ehg` (label `harness:codex`), submitted via `harmonik queue submit`.

Typed events (`jq` over `.harmonik/events/events.jsonl`, field `.type`):
- **`run_started` count = 0**
- **`run_failed`** payload `success:false`, bead `iso-ehg`, run `019f7baf-0afa…`,
  `summary: "codex isolation-boundary guard: refusing to launch a codex app-server crew with no enabled ssh worker boundary (danger-full-access would run unsandboxed on the daemon host) — enable an ssh-transport worker"`
  → reason string **contains `isolation-boundary`**. ✓
- No local codex CLI process spawned (pgrep: only harmonik binaries, no `codex` exec).
Guard fired at real dispatch, before any worktree/tunnel setup. Fail-closed confirmed end-to-end.

## LEG 1 — worktree-create + commit-land over ssh-localhost — GREEN (with fidelity note)
Localhost ssh key-auth available (`ssh -o BatchMode=yes localhost true` → OK).
`go test -tags=scenario -run TestScenario_RemoteSubstrate_Localhost_E2E ./internal/daemon/ -v`
(with `HARMONIK_REQUIRE_REMOTE_E2E=1` to fail loud rather than skip) → **PASS (6.21s)**.
Evidence:
- worktree **created over real `ssh localhost`** (SSHRunner worktree factory);
- worker commit **synced over ssh and landed on box A main** — sha `a9e0edc52e91…`, tip carries `Refs: hk-rs-b12-e2e-localhost`; origin/main == box A main (push reached origin);
- bead closed=1, reopened=0; `run_started.worker_name="localhost"` (routed to the ssh worker, not silent-local);
- negative guard `TestScenario_RemoteSubstrate_NoWorker_RunStartedWorkerNameEmpty` also PASS (local run → empty worker_name).

**FIDELITY NOTE (not a defect):** this scenario proves the remote-substrate git lifecycle
(worktree-create-over-ssh + commit-land + one-at-a-time merge to box A) with fidelity, BUT the
"agent" is a stub `/bin/sh -c "exit 0"` handler; the run-branch commit is made by the test's
worktree factory, not by a real `codex` subprocess. So LEG 1 proves worktree+commit-land over a
real ssh boundary, NOT a live codex exec. No self-contained test in the pin exercises a real codex
process exec (the codex driver path needs a real remote worker + API creds, out of scope for an
isolated self-contained leg). The faithful codex-exec-into-boundary proof belongs to leg B / an
integration environment with a live ssh worker.

## Teardown — CONFIRMED CLEAN
- Killed (order: supervise shim → daemon): supervise `43133`, daemon `41804`, leaked subscribe `42974`.
- `pgrep -fl codexA-iso` → none remain.
- Live fleet `a3dc45482890`: **6 sessions** (unchanged from baseline); supervise **pid 21849 alive**.
- Prod `/Users/gb/github/harmonik/.harmonik/workers.yaml`: sha `ef91bf42…` **UNCHANGED**.
- Leg B (`codexB-iso`, pids 41373/42651/43015) **untouched**.

## Verdict
Leg2a GREEN · Leg2b GREEN · Leg1 GREEN (fidelity-limit: stub handler, not real codex exec).
No defects filed (nothing broken).
