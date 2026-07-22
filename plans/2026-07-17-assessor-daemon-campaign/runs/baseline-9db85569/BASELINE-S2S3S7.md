# Baseline verification — S3 / S2 / S7 (bounded quiet-window leg)

- **PASS_KIND:** baseline (bounded subset — S3, S2, S7 only; NOT a full S1–S7 campaign)
- **PIN_SHA:** `9db85569fdc11967e99ad1a51d495b8cd596bbb5` (== origin/main; PR#33 merge, `hk-fufel` remote-cwd-aware direct-exec spawn)
- **PIN_BRANCH:** origin/main
- **SCRATCH:** `/tmp/h-assessor/base-9db85569` (fresh detached clone; HEAD==pin; porcelain clean=0)
- **BINARY:** `harmonik dev (commit: 9db85569fdc11967e99ad1a51d495b8cd596bbb5)` — verified == pin before assertions
- **Live scratch daemon:** session `harmonik-c0edc34d4508-default`, sandbox socket `…/base-9db85569/.harmonik/daemon.sock`, pid 35482 (distinct project-hash from fleet `a3dc45482890`)
- **ENV:** `TMPDIR=/tmp/h-assessor GOCACHE=/tmp/h-assessor/gocache-base CODEX_HOME=/tmp/h-assessor/codex`, `OPENAI_API_KEY` unset
- **Method:** pinned-source Go test harnesses compiled + run hermetically from the scratch clone — the runbook's "closest faithful, deterministic variant." These exercise the real production paths (runWorkLoop + dot_cascade; sessionstore latch under `-race`; remote-kill routing) with hard falsifiable assertions. Never touch the fleet daemon or live tmux.

---

## S3 — DOT full-graph (core-loop-proof contract) — **PASS / GREEN**

- **CMD:** `go test -run '^TestDotMode_E2E_CascadeTransitions$' -v ./internal/daemon/` (in scratch clone)
- **Fixture:** multi-node DOT graph `specs/examples/review-loop.dot` seeded to `.harmonik/workflow.dot` (start → implementer → reviewer → APPROVE → close), driven by the real `driveDotWorkflow` cascade.
- **OBSERVED** (node-transition event stream): `run_started → node_dispatch_requested/decided ×4 distinct nodes`, with the **REVIEWER node firing** (`reviewer_launched`, `reviewer_verdict`), then `outcome_emitted → bead_closed → run_completed → queue_group_completed`; `closed=[hk-9dnak-dot-cascade-001] reopened=[]`.
- **ASSERT:** full graph executed, every node reached, edges in order, REVIEW node fired, bead closed on terminal — **NO single-mode collapse**. → **PASS** (7.21s).
- **Falsifiability:** DOT→single collapse would show one node pair and no `reviewer_verdict`; observed 4 node pairs + reviewer node. Tier-0 false-green guard satisfied.

## S2 — H13 lost-wakeup / dispatch-race — **PASS / GREEN**

- **CMD:** `go test -race -run '^TestSessionStore_AgentReadyLatch' -v ./internal/hook/`
- **OBSERVED:** `TestSessionStore_AgentReadyLatch_ReplaysOnLateCallback` PASS (agent_ready fired BEFORE `SetAgentReadyCallback` is replayed on late install — the exact lost-wakeup window); `TestSessionStore_AgentReadyLatch_ConcurrentFireAndInstall` PASS — **200 iterations** racing `Dispatch(agent_ready)` vs `SetAgentReadyCallback`, `t.Fatalf("agent_ready lost…")` on any drop. No `WARNING: DATA RACE`.
- **ASSERT:** `agent_ready` consumed exactly once per launch, zero lost wakeups across 200 race iterations under `-race`. → **PASS** (1.487s).
- **Falsifiability:** reverting the latch (`readyFired` replay) loses the wakeup in the concurrent window → `len(got)==0` → `t.Fatalf`. Exceeds the ≥20-iteration / one-hang-=-BLOCK bar (200 iters).

## S7 — H8 remote-Kill no-local-PID (PR#33-relevant) — **PASS / GREEN**

- **CMD:** `go test -run '^TestRemoteKill' -v ./internal/daemon/`
- **OBSERVED:** `TestRemoteKill_ForcefullyKillsWorkerPID_HKBTL1N` PASS — remote `Kill` routes `kill -TERM 424242` then `kill -KILL 424242` (the WORKER pane PID) **over the SSH runner**; graceful `SendKeysQuit` + authoritative `KillWindow` both fire; `s.pid` is NEVER local-`syscall.Kill`'d (code contract, tmuxsubstrate.go killRemoteProcessWithGrace). `TestRemoteKill_NilRunner_NoPanic_HKBTL1N` PASS (defensive).
- **ASSERT:** remote Kill signals no local PID; forceful termination is a remote shell `kill` over SSH against the worker's process table; daemon survives. → **PASS** (0.678s).
- **FIDELITY LIMIT:** no live remote worker available on this box → used the recording-runner/adapter unit variant, which directly proves the kill is *routed over the SSH runner* (not local syscall) — the tightest bounded proof. A live loopback-SSH capture of local-PID liveness across the Kill (`TestScenario_RemoteSubstrate_Localhost_*`, `-tags=scenario`) was not run in this bounded window.

---

## Verdict (bounded leg)

All three target items **GREEN**. **No findings filed** (no defect confirmed → no `br create`). Bounded stop-condition (a) met. This leg proves S2/S3/S7 on the pinned tree via deterministic pinned-source harnesses; it is NOT the full S1–S7 admiral-gating ASSESSMENT.
