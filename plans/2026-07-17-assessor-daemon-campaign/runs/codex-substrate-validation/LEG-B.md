# Codex-Substrate Pre-Deploy Gate — Validation LEG B (live remote codex exec)

Pin: `9db85569` (HEAD == origin/main, contains hk-fufel PR#33). Iso binary
`/tmp/h-assessor/codexB-iso/bin/harmonik` → `commit: 9db85569…`.

Harness (the "trivial-project" iso, set up on top of the codexB-iso scaffold):
- **Box-A project** `/tmp/h-assessor/codexB-iso` — a 3-file git project (HEAD `9ae3bd6`)
  whose **root `workflow.dot`** is standard-bead.dot with the commit_gate trivialised to
  `tool_command="test -f counter.py"` while keeping the REAL pinned review node
  (`harness="claude-code"`, `model="claude-opus-4-8"`, tier 3). `<projectDir>/workflow.dot`
  is preferred over the embedded graph (`standardgraph.go`), so this is the graph that ran.
- **Worker** gb-mbp `100.87.151.114` (Tailscale). Probed GREEN: codex `/Users/gb/.local/bin/codex`
  present (**codex-cli 0.142.0**, not 0.144.5 — non-blocking, no version binding), `~/.claude.json`
  present (onboarded), `CLAUDE_CODE_OAUTH_TOKEN` set. Reverse ssh worker→boxA (`100.120.22.74`) works.
- **Worker repo** `/Users/gb/harmonik-assessor-iso/repo` — provisioned FRESH (guarded nuke of the
  stale path + `git clone ssh://100.120.22.74/tmp/h-assessor/codexB-iso-origin.git`, checkout `9ae3bd6`;
  origin carries the base so `git fetch origin <base>` resolves — no empty-HEAD). NOT prod `harmonik-worker`.
- **workers.yaml** (box-A iso): gb-mbp, transport ssh, `repo_path=/Users/gb/harmonik-assessor-iso/repo`,
  enabled — satisfies the isolation-boundary guard (workloop.go:3621).
- Config gap fixed pre-run: `codex.stale_wal_max_bytes: 1048576` (without it the implement node hard-fails
  at `buildCodexRoutedLaunchSpec` — observed as the 18:55:19 run_failed before the key was set).

**Provenance note:** the daemon that executed the run (pid 53081) is parented to the **assessor
session** (`96709 … --remote-control hk-assessor`), started 11:54:08, NOT a daemon this subagent
booted. It dispatched bead **ci-5jj** (labels `harness:codex, codex-substrate-validation`) against the
freshly-provisioned worker repo. Evidence below is harvested from that live run
(**run_id `019f7bbc-e3fc-78ae-902f-17ea54a0ac73`**). A second bead `ci-h5n` was created by this subagent
but never dispatched (the queue paused on group_failure); it exercises the identical path.

All event queries are `jq` over `.type` in `/tmp/h-assessor/codexB-iso/.harmonik/events/events.jsonl`.

## Timeline (run 019f7bbc, UTC)
```
18:56:46  node_dispatch_requested  start → implement
18:56:48  codex rollout opens on worker: ~/.codex/sessions/2026/07/19/rollout-…-019f7bbc-f211-….jsonl
18:58:28  implementer_phase_complete  exit_code=0  duration=99.3s  commit_landed=FALSE
18:58:28  node_dispatch_requested  commit_gate  (trivial `test -f counter.py`) → SUCCESS
18:58:28  node_dispatch_requested  review
18:58:29  harness_selected  agent_type=claude-code  tier=3      ← pinned review harness
18:58:30  reviewer_launched
19:00:08  agent_ready_stall_detected  stall_seconds=201
19:02:00  run_failed  summary="dot: agentic node \"review\" failed: node \"review\" agent_ready_timeout"
```

## Proof 1 — Real remote codex exec — **GREEN**
- `implementer_phase_complete` (run 019f7bbc): `exit_code=0`, `duration_seconds=99.3`,
  `stderr_tail_head` = `… ERROR codex_core::tools::router: error=exec_command failed for
  /bin/zsh -lc 'rm -rf __pycache__ && git status --short': CreateProce…` — `codex_core::tools::router`
  is codex's own Rust crate: unambiguously a real codex process.
- Real codex rollout session on the worker:
  `/Users/gb/.codex/sessions/2026/07/19/rollout-2026-07-19T11-56-48-019f7bbc-f211-7772-9680-bc7e7a902bbf.jsonl`
  (92 KB), timestamp matches the run. Not a stub.

## Proof 2 — Fresh worker repo + commit lands — **SPLIT: fresh-repo GREEN, commit-land RED**
- **Fresh worker repo — GREEN.** `run_started.workspace_path =
  /Users/gb/harmonik-assessor-iso/repo/.harmonik/worktrees/019f7bbc-…` — worktree created in the FRESH
  assessor-iso repo (NOT prod `/Users/gb/harmonik-worker`). Worktree-create SUCCEEDED — the prior run's
  hk-iaj1w empty-HEAD race did NOT recur (fresh clone with base present).
- **Commit lands — RED.** `commit_landed=false`; on the worker `git show HEAD:counter.py` still
  `return 1`, no `run/*` branch. **Root cause (from the codex rollout log):** codex was launched with
  `sandbox_mode: workspace-write`, `Approval policy … never`, writable roots = the worktree + `/private/tmp`.
  Codex MADE the edit ("the code change is done") but the **git commit was denied by the seatbelt sandbox**:
  ```
  Operation not permitted
  error: counter.py: failed to insert into database
  error: unable to index file 'counter.py'
  fatal: updating files failed
  exec_command failed for `/bin/zsh -lc '…'`: CreateProcess { … Operation not permitted
  ```
  Net: **codex exits 0 with the edit made but NO commit** — a silent no-op. The isolation-guard's premise
  ("danger-full-access would run unsandboxed") is NOT what actually ran; the crew ran `workspace-write`,
  which is too restrictive to write `.git/`. **Material deploy-risk defect.**

## Proof 3 — Routing — **GREEN**
- Typed `run_started` events (both attempts): `worker_name="gb-mbp"`, `worker_os="darwin"`,
  `workflow_mode="dot"`. Workspace path under `/Users/gb/harmonik-assessor-iso` confirms the ssh worker
  (not silent-local). Routed to the configured worker.

## Proof 4 — hk-g5wkt review node (RELAY vs STALL) — **STALL**
- Pinned review node: `harness_selected` `agent_type=claude-code` `tier=3` → `reviewer_launched`
  (18:58:30) → **`agent_ready_stall_detected` `stall_seconds=201`** (19:00:08) →
  **`run_failed` "node \"review\" agent_ready_timeout"** (19:02:00).
- The review agent's `~/.claude/projects/…worktrees-019f7bbc…/019f7bbe-….jsonl` session file was
  **never written** (absent) — claude-code launched but never produced a turn, i.e. never reached
  `agent_ready`. No orphaned review process remained (the `claude-remote` tmux session on the worker is an
  unrelated pre-existing interactive session from Jul 18 23:05, not the review agent).
- **gb-mbp IS onboarded** (`~/.claude.json` + OAUTH present) yet the pinned claude-code review node STILL
  STALLED to `agent_ready_timeout`. Onboarding alone did NOT make it relay — contrast the expectation
  that an onboarded worker might relay. Precise root cause (onboarding-suppression not propagated to the
  remote exec vs. sandbox vs. seed-paste) could not be isolated because the worktree + session were cleaned
  on run_failed; the OBSERVABLE is a hard stall of the review node on the remote worker. **Material deploy-risk.**

## Proposed defects (for the assessor to file — none filed by this subagent)
1. **P1/P0 — codex crew commits are blocked by `workspace-write` sandbox.** Remote codex implement node
   runs `sandbox_mode=workspace-write`+approval=never; `git commit` is denied ("Operation not permitted",
   "unable to index file", "fatal: updating files failed"). Codex exits 0 with the edit made but no commit
   → silent no-op. Fix: dispatch the crew with the sandbox mode that permits `.git` writes (the guard's
   "danger-full-access" intent) or grant `.git` as a writable root.
2. **P1 — pinned claude-code review node stalls on a remote (even onboarded) worker (hk-g5wkt confirmed).**
   `agent_ready_timeout` on gb-mbp despite onboarding. Blocks every DOT bead from reaching `close` on the
   codex substrate (review is the sole inbound edge to close).
3. **Info/known — codex driver hard-fails without `codex.stale_wal_max_bytes`.** Not a defect (missing
   config); noted because it silently blocks the implement node until set.

## Teardown — PARTIAL, with a deliberate hold (flagged for the assessor)
- The executing daemon **pid 53081 is the assessor session's own process** (parent `96709
  --remote-control hk-assessor`), and `/Users/gb/harmonik-assessor-iso` is that live daemon's worker repo.
  Killing a parent-owned daemon / removing its active worker repo is destructive and outside this
  subagent's authority, so **NOT torn down** — left for the assessor to reconcile.
- This subagent created only: bead `ci-h5n` (unrun, harmless — NOT terminal-transitioned per bead-lifecycle
  rules) and empty scratch `/tmp/h-assessor/codexB-run`.
- **Safety bounds VERIFIED intact:** prod `/Users/gb/harmonik-worker/repo` present (HEAD `0553d4b`);
  box-A prod `.harmonik/workers.yaml` sha `ef91bf42` UNCHANGED; live fleet supervise **21849** + preempt
  watcher **38536** both alive; the live fleet session was never touched.

## Verdict
Proof1 GREEN · Proof2 fresh-repo GREEN / commit-land RED · Proof3 GREEN · Proof4 STALL.
Two material deploy-risk defects surfaced (codex sandbox blocks commit; pinned review node stalls on
remote onboarded worker). **The prod codex-daemon reboot should NOT be greenlit on Leg B** — the codex
substrate cannot currently land a commit AND cannot pass the review node on the remote worker.

---

## ADDENDUM — independent 2nd-agent reproduction (CONFIRMS verdict; one refinement)
A second leg-B agent ran the SAME iso concurrently (this agent booted daemon pid 53081 @11:54:08 and
dispatched ci-5jj; the writeup above harvested this agent's first run 019f7bbc). Independent confirmation:

- **ROUTING / remote codex exec / STALL — all reproduced.** run_started worker_name=gb-mbp, harness_selected
  agent_type=codex tier 1; review STALLED again on a SECOND run `019f7bc5-c141-…`
  (reviewer_launched 19:08:16 → agent_ready_stall_detected stall_seconds=189 → daemon reaped before terminal;
  run 019f7bbc reached the full agent_ready_timeout HC-056). Two-for-two review stalls. Verdict UNCHANGED.

- **REFINEMENT to "commit-land RED / zero landed work":** on run 019f7bc5 the worktree was snapshotted ON
  gb-mbp AT implement-completion (before the run_failed cleanup). Codex's OWN commit was blocked
  (`commit_landed=false`, seatbelt sandbox), BUT the **daemon FALLBACK committed** codex's applied patch:
  worktree HEAD `123b2ae` = `feat(codex): codex turn output (auto-committed by daemon fallback)` on base
  9ae3bd6, and `git diff` on the worker shows `counter.py: return 1 → return 2`. So the change DOES reach the
  boundary via the daemon fallback — it is not a pure zero-landed-work no-op. This does NOT change BLOCK:
  the review node still stalls (sole inbound edge to close), so the bead never completes; and relying on a
  daemon fallback because codex's own git commit is sandbox-denied is itself fragile.

- **Duplicate beads (for assessor dedup):** this agent independently filed **hk-36xy5** (review stall,
  == hk-qxvc2) and **hk-wwyse** (codex remote commit failure, == hk-daegv; note hk-wwyse attributes the cause
  to codex exec_command spawn — the canonical root cause is the seatbelt `workspace-write` sandbox denying
  `.git` writes, per hk-daegv). Recommend consolidating onto hk-qxvc2 / hk-daegv.

- **Teardown RE-VERIFIED clean (post-12:11 activity resolved):** this agent's extra runs after the 12:11
  VERDICT re-created `/Users/gb/harmonik-assessor-iso` and used daemon 53081; all now torn down —
  `pgrep -fl codexB-iso` empty, guard process killed, `/Users/gb/harmonik-assessor-iso` removed & verified
  gone, no leftover `harmonik-assessor-iso` procs on gb-mbp. Prod `/Users/gb/harmonik-worker/repo` present
  and untouched; box-A prod `.harmonik/workers.yaml` git-clean/UNCHANGED; live fleet a3dc45482890 intact
  (6 sessions), supervise pid 21849 + preempt watcher pid 38536 alive.
