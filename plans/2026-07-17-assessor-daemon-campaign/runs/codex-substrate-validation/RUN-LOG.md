# RUN-LOG — assessor codex-substrate validation (pre-deploy gate)
- TASK: operator-directed, relayed by admiral (2026-07-19 18:17/18:31). Independently validate codex substrate end-to-end before prod codex-daemon reboot. Prod reboot GATED on this PASS.
- PIN: build iso binary from 9db85569 (== origin/main, contains hk-fufel PR#33). Clean clones present: pinsrc/mg/lt-9db85569.
- yankee hk-g0ror.4 Scenario-B = prior evidence ONLY, not a substitute.

## Prerequisite probes (assessor, direct)
- codex CLI 0.144.5 @ /opt/homebrew/bin/codex — PRESENT (local).
- gb-mbp @ 100.87.151.114 (Tailscale): ping OK 8ms; SSH_OK hostname=gb-mbp; codex @ /Users/gb/.local/bin/codex PRESENT; ~/.claude.json PRESENT (claude-ONBOARDED); CLAUDE_CODE_OAUTH_TOKEN set.
  -> CRUX: gb-mbp IS onboarded, so remote claude REVIEW node MAY relay (vs yankee fresh-worker Scenario-B stall). Independent hk-g5wkt check is genuinely runnable.
- Guard: workloop.go:3621 refuses if no enabled ssh worker (reason contains 'isolation-boundary'). Self-contained test TestCodexIsolationGuard_HK5H759.
- BOOT gotcha: scratch-daemon.sh can't inject HARMONIK_SUBSTRATE (tmux env). Run binary directly w/ env. substrate!=harness: bead MUST carry harness:codex.

## Legs (two parallel isolated subagents)
- A (self-contained): Leg1 LOCAL codex e2e (TestScenario_RemoteSubstrate_Localhost_E2E, codex over ssh-localhost, commit lands) + Leg2 fail-closed (unit TestCodexIsolationGuard_HK5H759 + real-dispatch refusal on iso codexdriver daemon w/ no worker).
- B (external, gb-mbp): Leg3 REMOTE DOT crew — codex execs on gb-mbp, worktree+commit land in a FRESH worker repo (NOT prod /Users/gb/harmonik-worker/repo), assert run_started.worker_name==gb-mbp via events.jsonl typed events; OBSERVE claude review node relay-vs-stall (hk-g5wkt). Report truth.
