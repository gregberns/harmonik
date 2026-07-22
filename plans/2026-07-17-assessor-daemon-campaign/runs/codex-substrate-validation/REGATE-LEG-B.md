# Codex-Substrate RE-GATE — Validation LEG B (live remote codex exec) — PIN `fff3d937`

**Verdict: RE-BLOCK.** Both prior-gate defects REPRODUCE on `fff3d937`. Proof 3 (PRIMARY,
review-node relay) is **STILL-STALLED**; Proof 2 (codex own-commit under danger-full-access)
is **RED** (edit lands only via daemon fallback, codex still runs `workspace-write`).

Pin: `fff3d937cebefc07ac74a58fbfaefbc72876ac66` (branch `phase1-session-restart-substrate` HEAD;
`git merge-base --is-ancestor` confirms a0619c1c + czb11 + fufel all IN pin). Fresh iso binary
`/tmp/h-assessor/legB-regate/bin/harmonik` → `harmonik dev (commit: fff3d937…)`. `daemon_started`
payload `binary_commit_hash=fff3d937…` confirms the RUNNING daemon is this build.

## Harness (fresh, isolated — NOT the prior codexB scaffold)
- **Box-A project** `/tmp/h-assessor/legB-regate-proj` — fresh git repo (base `7cb7db9` → config
  commit `844d6f4`), `harmonik init` on this pin, `workflow_mode: dot`, `target_branch: main`,
  `codex.stale_wal_max_bytes: 1048576`, `remote_control_prefix: legb`, beads prefix `lrp`. Root
  `workflow.dot` = **standard-bead.dot keeping the REAL pinned review node**
  (`harness="claude-code"`, `model="claude-opus-4-8"`, tier 3); commit_gate `test -f counter.py`
  (trivial — the point is to exercise implement+review, not the gate).
- **Bare origin** `/tmp/h-assessor/legB-regate-origin.git` (main → `844d6f4`), cloned by the worker
  over `ssh://100.120.22.74/…` (box-A Tailscale IP; reverse ssh worker→boxA probed GREEN).
- **Worker** gb-mbp `100.87.151.114` (Tailscale IP). codex `/Users/gb/.local/bin/codex`
  (**codex-cli 0.142.0** — note: NOT 0.144.5), `~/.claude.json` present (135701 B),
  `CLAUDE_CODE_OAUTH_TOKEN` set (in ambient `~/.zshenv` — load-bearing below).
- **Worker repo** `/Users/gb/harmonik-assessor-iso/repo` — provisioned FRESH (guarded nuke of the
  `*harmonik-assessor-iso` path, fresh clone of the bare origin, `checkout main` @ `844d6f4`). NOT
  prod `/Users/gb/harmonik-worker`.
- **box-A workers.yaml**: gb-mbp, transport ssh, `repo_path=/Users/gb/harmonik-assessor-iso/repo`,
  enabled — satisfies the isolation-boundary guard.
- **Daemon** booted DIRECTLY: `HARMONIK_SUBSTRATE=codexdriver HARMONIK_DISABLE_EAGER_REFILL=1
  harmonik --project /tmp/h-assessor/legB-regate-proj --max-concurrent 2` (pid 80466; env verified
  via `ps eww`). This subagent's OWN daemon — fully torn down (below). Dispatched bead **lrp-0cu**
  (labels `harness:codex, codex-substrate-validation`) via `queue submit`; queue `019f7bfd`,
  run **`019f7bfd-7ffb-7649-b8b9-b300dcc527fa`**.

All event queries: `jq` over `.type` in `/tmp/h-assessor/legB-regate-proj/.harmonik/events/events.jsonl`.

## Timeline (run 019f7bfd, UTC)
```
20:07:20  run_started            worker_name=gb-mbp  workflow_mode=dot  workspace under /Users/gb/harmonik-assessor-iso/repo
20:07:20  node_dispatch          start → implement
20:07:21  harness_selected       agent_type=codex  tier=1        ← real remote codex
20:07:23  codex rollout opens on worker: ~/.codex/sessions/2026/07/19/rollout-…-019f7bfd-915a-….jsonl
20:09:27  implementer_phase_complete  exit_code=0  duration=124.6s  commit_landed=FALSE
20:09:28  node_dispatch          commit_gate (test -f counter.py) SUCCESS → review
20:09:29  harness_selected       agent_type=claude-code           ← pinned review node
20:09:31  reviewer_launched
20:10:30  agent_ready_stall_detected  stall_seconds=188
20:13:01  agent_ready_timeout
20:13:01  run_failed             summary="dot: agentic node \"review\" failed: node \"review\" agent_ready_timeout"
                                 worktree_tip_sha=a627c0e (daemon fallback commit)
```
Bead **lrp-0cu remains OPEN** (`br show` → `[● P1 · OPEN]`, Notes = the agent_ready_timeout summary).
The daemon did NOT close it — the run failed at review.

## Proof 1 — Real remote codex exec — **GREEN**
- `run_started`: `worker_name="gb-mbp"`, `worker_os="darwin"`, `workflow_mode="dot"`,
  `workspace_path=/Users/gb/harmonik-assessor-iso/repo/.harmonik/worktrees/019f7bfd-…` (fresh iso
  repo, NOT prod). `harness_selected agent_type=codex tier=1`.
- Real codex rollout on the worker:
  `~/.codex/sessions/2026/07/19/rollout-2026-07-19T13-07-23-019f7bfd-915a-7e23-ae1c-ab64e6a02c51.jsonl`
  (145 KB, `codex_core` crate). `implementer_phase_complete` `exit_code=0` `duration=124.6s`.
  Unambiguously a real codex process on the remote substrate.

## Proof 2 — Commit lands a run/* branch via codex's OWN commit (hk-daegv fix) — **RED**
- **Run branch + edit DO exist:** `run/019f7bfd-7ffb-7649-b8b9-b300dcc527fa` on the worker,
  `counter.py` = `return 2` (the intended edit). So the change reaches the boundary.
- **But codex's OWN commit did NOT land — the DAEMON FALLBACK did.** `implementer_phase_complete
  commit_landed=FALSE`; worktree HEAD = **`a627c0e feat(codex): codex turn output (auto-committed
  by daemon fallback)`**. Codex's `stderr_tail_head`: `ERROR codex_core::tools::router:
  error=exec_command failed for /bin/zsh -lc 'rm -rf __pycache__ && git status --short':
  CreateProce…` — the identical seatbelt EPERM signature as the prior gate.
- **The hk-daegv `-c sandbox_mode="danger-full-access"` override did NOT take effect on the
  worker.** The codex rollout's own environment-context system prompt states:
  `` `sandbox_mode` is `workspace-write` `` and `Approval policy is currently never` — **zero**
  `danger-full-access` occurrences in the 145 KB rollout; `Operation not permitted` present. The
  worker's `~/.codex/config.toml` has no sandbox key, so codex's built-in default (`workspace-write`)
  is what actually ran.
- **Mechanism:** a0619c1c appends `-c sandbox_mode="danger-full-access"` to
  `codexdriver.Options.Args` (`cmd/harmonik/substrate_select.go:236`), and `app-server` is
  present ONLY there — so the app-server spawn does use that slice. The flag was therefore
  DELIVERED to `codex app-server` but **codex-cli 0.142.0 on the worker did not honor it for the
  exec seatbelt** (the commit only empirically verified acceptance on 0.144.5; "accepts the key"
  ≠ "applies it"). Net effect is unchanged from the prior gate: codex runs `workspace-write`, its
  own `git commit` / `/bin/zsh` exec is seatbelt-denied, and the edit lands only because the
  **daemon fallback** commits it. The specific Proof-2 claim ("codex's OWN commit now succeeds
  under danger-full-access") is REFUTED.

## Proof 3 — ★ PRIMARY — review reaches agent_ready AND bead CLOSES (hk-qxvc2/hk-g5wkt fix) — **RED / STILL-STALLED**
- **Review lifecycle:** `harness_selected agent_type=claude-code` (20:09:29) → `reviewer_launched`
  (20:09:31) → **`agent_ready_stall_detected stall_seconds=188`** (20:10:30) →
  **`agent_ready_timeout`** (20:13:01) → **`run_failed`** "node \"review\" agent_ready_timeout".
  **No `agent_ready` and no `reviewer_verdict` event ever fired.** The bead did NOT reach `close`
  and is OPEN. Same failure mode as the prior gate (201s / 189s there; ~210s here).
- **The fff3d937 fix DID partly execute but the fix is INCOMPLETE.** `PrepareIsolatedClaudeConfigDirVia`
  ran and seeded the isolated config dir ON THE WORKER —
  `<worktree>/.harmonik/claude-config/.claude.json` exists (135956 B, mtime 20:09, matching the
  review dispatch). So the seed half of the fix works.
- **ROOT CAUSE — `CLAUDE_CONFIG_DIR` never reaches the remote reviewer process.** `ps eww` on the
  live review claude (pid 98702, `claude --session-id 019f7bff-804d-… --model claude-opus-4-8
  --dangerously-skip-permissions`, launched in the worktree) shows its FULL env contains
  `CLAUDE_CODE_OAUTH_TOKEN` but **ZERO `CLAUDE_CONFIG_DIR`** (`grep -c` = 0). The reviewer is
  launched via `/usr/bin/login -f -pq -h 100.120.22.74 gb /bin/zsh -c cd '<worktree>' && exec
  'claude' …` — a login shell with **no env-var prefix**. `CLAUDE_CODE_OAUTH_TOKEN` is present only
  because it is set in the worker's AMBIENT login env (`~/.zshenv` → `zsh -lc` confirms
  `OAUTH_ambient=SET`, `CONFIG_ambient=` empty). The daemon's `LaunchSpec.Env` (which Step 5a of
  `buildClaudeLaunchSpec` populates with `CLAUDE_CONFIG_DIR=<worktree>/.harmonik/claude-config`)
  is **NOT injected into the remote claude process** on this ssh launch path — so claude falls back
  to the worker's SHARED `~/.claude.json`, re-wedges on the first-run onboarding/theme modal BEFORE
  SessionStart, never fires `agent_ready`, and times out.
- **Verdict statement:** **STILL-STALLED.** The fff3d937 fix seeds the isolated dir but does not
  deliver the `CLAUDE_CONFIG_DIR` env var to the remote reviewer, so its premise (claude reads the
  private config → clears the modal → reaches agent_ready) does not hold end-to-end. The fix did
  NOT hold — this is a re-BLOCK finding.

## Proof 4 — Fail-closed isolation guard still GREEN on `fff3d937` — **GREEN**
- `go test ./internal/daemon -run TestCodexIsolationGuard_HK5H759 -count=1 -v` on this pin →
  **PASS**, all 5 subcases: `codex_crew_no_registry_REFUSED`, `codex_crew_nonssh_worker_REFUSED`,
  `codex_crew_disabled_worker_REFUSED` each log the guard message *"refusing to launch a codex
  app-server crew with no enabled ssh worker boundary (danger-full-access would run unsandboxed on
  the daemon host)"*; `flag_off_no_guard_baseline` + `codex_crew_with_boundary_ALLOWED` pass. The
  fail-closed `isolation-boundary` refusal is intact on this build.

## Proposed defects (for the assessor to file — none filed by this subagent)
1. **P1 (remediation:blocking) — pinned claude-code review node STILL stalls to `agent_ready_timeout`
   on the remote worker; hk-qxvc2/hk-g5wkt NOT fixed by fff3d937.** The fix seeds the isolated
   `CLAUDE_CONFIG_DIR` on the worker but the env var is never delivered to the remote claude
   process (the ssh login-shell launch carries no env prefix; OAUTH survives only via ambient
   `~/.zshenv`). claude reads the shared `~/.claude.json` and re-wedges on the modal. Fix must
   propagate `LaunchSpec.Env` (specifically `CLAUDE_CONFIG_DIR`) into the remote claude exec —
   e.g. inline `env CLAUDE_CONFIG_DIR=… claude …` in the worker command, or an ssh
   `SendEnv`/wrapper. Reopen hk-qxvc2. **Blocks every DOT bead from reaching `close` on the codex
   substrate (review is the sole inbound edge to close).**
2. **P1/P0 (remediation:blocking) — codex's own commit STILL blocked; hk-daegv NOT fixed on the
   worker's codex-cli 0.142.0.** `-c sandbox_mode="danger-full-access"` is delivered to
   `codex app-server` but 0.142.0 does not apply it to the exec seatbelt (rollout reports
   `workspace-write`, `Operation not permitted`, commit_landed=false; edit lands only via the
   daemon fallback `a627c0e`). The commit's empirical check was on 0.144.5; the worker runs 0.142.0.
   Fix must make the effective sandbox danger-full-access on the deployed codex version (verify
   against the actually-installed codex, not just app-server key acceptance), or land codex's own
   commit some other seatbelt-safe way. Reopen hk-daegv. Note: per the no-external-version-binding
   principle, the remedy is to degrade/adapt to the installed codex, not to pin 0.144.5.
3. **Info/known — codex driver still requires `codex.stale_wal_max_bytes`** (set to 1048576 here;
   without it the implement node hard-fails at `buildCodexRoutedLaunchSpec`). Config gap, not a defect.

## Teardown — CONFIRMED CLEAN (this subagent's OWN daemon, fully torn down)
- Killed in order: supervisor watchdog → iso daemon (pid 80466) → subscriber (pid 81124) →
  `pkill -f legB-regate`. `pgrep -fl legB-regate` **EMPTY**; iso `daemon.sock` gone; no
  reverse-ssh tunnel / codexB procs. Worker leftover review-claude + orphan procs cleaned
  (`ssh … pkill -f harmonik-assessor-iso` → EMPTY).
- Removed remote repo `ssh 100.87.151.114 'rm -rf /Users/gb/harmonik-assessor-iso'` (path-guarded
  to `*harmonik-assessor-iso`); verified **GONE**, and prod `/Users/gb/harmonik-worker/repo`
  **STILL PRESENT**.
- **Prod safe:** box-A prod `.harmonik/workers.yaml` sha `ef91bf42…` **UNCHANGED** (git-clean);
  live fleet supervise **21849** alive. Live fleet never touched.
- This subagent filed NO beads and terminal-transitioned NO beads. Bead lrp-0cu left OPEN (its
  iso project + beads DB were on the `/tmp/h-assessor/legB-regate-proj` scratch path).

## Verdict
Proof1 GREEN · Proof2 RED (own-commit fails; edit lands only via daemon fallback) · Proof3 **RED /
STILL-STALLED** (PRIMARY) · Proof4 GREEN. **Both prior-gate blockers REPRODUCE on `fff3d937`.**
The claimed fixes execute partially (sandbox flag delivered but not honored by codex 0.142.0;
isolated claude-config seeded but `CLAUDE_CONFIG_DIR` never reaches the remote reviewer) but
NEITHER holds end-to-end on the real remote substrate. **The prod codex-daemon reboot should NOT
be greenlit on this re-gate.** Re-gate again only after (a) the remote reviewer actually receives
`CLAUDE_CONFIG_DIR` and reaches agent_ready→close, and (b) codex's effective sandbox is
danger-full-access on the DEPLOYED codex version so its own commit lands.
