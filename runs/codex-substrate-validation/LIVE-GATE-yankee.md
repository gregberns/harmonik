# LIVE-GATE yankee — forensic diagnosis

Run: `019f7c46-fdc7-79b0-9970-d7c8166e6a16` (bead yri-wq7)
Iso daemon binary: commit 44831898 (branch phase1-session-restart-substrate)
- Fix A = f907b702 (hk-qxvc2): RemoteExecArgv prepends `exec env CLAUDE_CONFIG_DIR=…`
- Fix B = 44831898 (hk-daegv): runtimeWorkspaceRoots=[worktreeCwd, <repo>/.git]

Failures:
- B: commit_landed:false (implementer_phase_complete, exit0, 83s, stderr_tail "Reading additional input from stdin")
- A: review agent_ready_timeout (185s stall)

worktree_tip_sha reported by run_failed = cfcd2057eff14c4438df37a1f102cefe38e68fed
Worker: gb@100.87.151.114, repo /Users/gb/harmonik-yankee-iso/repo

---

## Investigation log

## FINDINGS (run 019f7c46, binary=44831898, both fixes compiled)

### Fix B (hk-daegv) — ROOT CAUSE FOUND: writable_roots grant not forwarded to remote codex
Codex rollout `~/.codex/sessions/2026/07/19/rollout-2026-07-19T14-27-39-019f7c47-…jsonl` (worker) shows the
permissions the remote codex ACTUALLY ran with:
- `sandbox_mode = workspace-write`, `approval_policy = never`, `network_access = false`
- **writable roots = [`<worktree>/019f7c46-…`, `/private/tmp`]** — the `<repo>/.git` common-dir root that
  Fix B (`runtimeWorkspaceRoots=[worktreeCwd, <repo>/.git]`) is supposed to add is **ABSENT**.
- Codex DID attempt its own commit (`git commit -m …`) and hit **4× "Operation not permitted"** (can't write
  `.git`) → `commit_landed=false`.
- Conclusion: Fix B's `.git` writable-root grant does NOT reach the REMOTE codex sandbox permissions. Same
  class as Fix A's original bug (local-only, not forwarded over the ssh/app-server transport). The list codex
  got ([cwd, /private/tmp]) looks like codex's DEFAULTS, i.e. the custom runtimeWorkspaceRoots list was never
  applied on the remote path. (stderr "Reading additional input from stdin" is codex's headless prompt-drain,
  not the failure itself; the real failure is the seatbelt denial above.)

### Fix A (hk-qxvc2) — env delivered, but review still wedges PRE-TURN
- `CLAUDE_CONFIG_DIR` IS present in the remote review claude exec argv (Fix A env-forward works — confirmed
  live ps). Advance over re-gate #2.
- BUT the review claude wrote **NO session jsonl** (`~/.claude/projects/*019f7c46*` absent) → it never
  produced a turn → wedged BEFORE `agent_ready` (same observable as LEG-B Proof 4). This is a PRE-TURN wedge
  (trust/onboarding modal in the isolated config dir), NOT the "no task / context gone" post-start mode.
- Confound: NO `run/*` branch / no commit existed (Fix B failed) → there was also nothing to review. Cannot
  fully separate the two from this run.
- GAP: the isolated `.harmonik/claude-config/.claude.json` contents (trust state, hasCompletedOnboarding)
  are UNRECOVERABLE — the daemon auto-cleaned the worktree on run_failed (gone by ~21:35Z).

### Preservation status
- Worker worktree `019f7c46-…`: **already auto-cleaned by the daemon on run_failed** (not recoverable).
- SURVIVING evidence: worker codex rollout jsonl (above, persists); box-A iso events
  `/tmp/h-yankee/proj/.harmonik/events/events.jsonl`; iso daemon still up (pid 26316) + supervise shim.

## CORRECTION + DEEPER ROOT CAUSES (from the legacy live-gate sub-agent, completed 21:37Z)
A live-gate sub-agent spawned by the PRE-restart yankee session survived the /clear and completed with
richer evidence than the post-restart probes. It SUPERSEDES the two findings above where they differ.

### Fix A (hk-qxvc2) — CORRECTED: NOT a pre-turn modal. Protocol mis-route under codexdriver substrate.
- Prior "no session jsonl / pre-turn wedge" was a FALSE NEGATIVE — I checked `~/.claude/projects/` (default),
  but with isolation the transcript went to `<worktree>/.harmonik/claude-config/projects/`. Claude DID start.
- REAL cause: daemon booted with `HARMONIK_SUBSTRATE=codexdriver`. The pinned `harness="claude-code"` review
  node is driven with the **codex app-server JSON-RPC protocol**. Claude's first prompt was
  `{"id":1,"jsonrpc":"2.0","method":"initialize","params":{"clientInfo":{"name":"harmonik","version":"codexdriver"}…}}`.
  Claude isn't a JSON-RPC server → read it as chat → replied in prose ("I don't see a task … I'm booted and
  ready") → never got the review task → never emitted agent_ready → stall 185s → timeout.
- Corroborating: hook reverse-tunnel `tcp://127.0.0.1:59963` was NOT listening on the worker while the reviewer
  was alive (nc -z failed; no :59xxx LISTEN); no worker_tunnel_failed event = silent bridge_dial_failed. So a
  native SessionStart hook couldn't have called back either.
- So Fix A's env-delivery half IS proven fixed (CLAUDE_CONFIG_DIR present in live ps argv, count=1, was 0 in
  re-gate #2). The remaining wedge is a SUBSTRATE ROUTING bug: codexdriver speaks codex JSON-RPC to a claude
  review node, and the hook tunnel isn't stood up.

### Fix B (hk-daegv) — REFINED: fix targets app-server thread/start; run used `codex exec`.
- commit_landed=FALSE. Worktree HEAD = `cfcd2057 …(auto-committed by daemon fallback) Refs: yri-wq7` — daemon
  fallback, not codex. counter.py = return 2 (edit reached boundary).
- Codex ran `codex exec --sandbox workspace-write` (visible in ps). Rollout environment_context writable roots
  = worktree cwd + /tmp + tmpdir; `<worktree>/.git` listed READ-ONLY; `<repo>/.git` ABSENT. codex's own
  `git add && git commit` → exit 128, `Unable to create '…/.git/worktrees/019f7c46-…/index.lock': Operation not
  permitted`.
- PRECISE REASON fix inert: Fix B stamps `runtimeWorkspaceRoots=[worktreeCwd, <repo>/.git]` into app-server
  **thread/start & thread/resume** params — that path is NOT exercised when the implement node runs via
  **`codex exec`**. So the intended writable root never took effect.

### Teardown CONFLICT (surface to captain)
The legacy sub-agent TORE DOWN the iso harness (killed iso daemon 26316 + supervisor 27349; `rm -rf` worker
`/Users/gb/harmonik-yankee-iso`) as its final hygiene step — it predated and never saw the captain's 21:36Z
"DO NOT teardown / preserve as fanout evidence" order. Irreversible. BUT decisive evidence SURVIVES: box-A iso
`events.jsonl` (persists), worker codex rollout `rollout-…14-27-39-019f7c47-…jsonl` (persists, not under the
removed iso repo), and the review-claude JSON-RPC transcript finding (harvested pre-teardown, recorded above).
LOST: live iso daemon process + worker worktree/config-dir contents. Prod confirmed untouched (worker
/Users/gb/harmonik-worker present; box-A workers.yaml clean; live fleet daemon 24522 + supervisor 21849 alive).
