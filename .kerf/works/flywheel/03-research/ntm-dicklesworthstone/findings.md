# Research — NTM & the Dicklesworthstone corpus

> Component: `ntm-dicklesworthstone`. Source: research sub-agent (sonnet) over github.com/Dicklesworthstone, 2026-05-27.

## TL;DR
- **NTM** gives real primitives for process supervision, crash detection, session checkpoint/restore, and rate-limit recovery — directly useful for keeping a loop *process* alive across crashes — but it is **orthogonal to context-window management**; NTM sees tmux pane text, not API messages.
- **`claude_code_agent_farm`** is the closest existing reference implementation of an indefinite agent loop: monitors 20+ concurrent Claude Code agents, detects context exhaustion via Claude's own "Context left until auto-compact: N%" string, and recovers by **throwing the context away and restarting** — soft reset (`/clear` + re-inject prompt) or hard reset (`/exit` + relaunch with exponential backoff). **State carries across via an external task-queue file, NOT in-process memory.**
- His answer to "context filled" is **discard + restart fresh**, never compaction/sliding-window. The loop survives because *work state lives outside the agent*. The unsolved-by-him residue = the **stateful controller** (the orchestrator tracking in-flight runs, retries, batch outcomes) — that needs a state digest, which is exactly flywheel's hard part.

## 1. NTM process-lifecycle primitives (repo: github.com/Dicklesworthstone/ntm)
Commands: `ntm health`, `ntm spawn`, `ntm checkpoint`, `ntm resume`, `ntm activity --watch`, `ntm serve`.
- **Spawn + panes:** `ntm spawn <project> --cc=N ...` creates a named tmux session with labeled panes (one agent/pane). `ntm add` adds agents live. Harmonik already uses this to launch handler claude processes.
- **Death detection:** `ntm health` polls pane scrollback for alive/stuck/rate-limited *text patterns* + a heartbeat concept (silent too long → flagged). **Behavioral detection only — no OS-level pidfd/process heartbeat.**
- **Auto-respawn / rate-limit recovery:** knows rate-limit output signatures; rotates accounts via `caam` and re-injects work. (Respawn path is for rate limits more than crashes.)
- **Checkpoint/restore:** `ntm checkpoint save` snapshots tmux layout + pane scrollback + git HEAD + NTM queue state; `restore`/`resume` rebuild it. Pipeline steps track completion so `ntm pipeline resume` re-runs from first incomplete step. **This is the machine-restart crash-recovery path.**
- **Machine-readable fleet state:** `ntm serve` (REST/SSE/WS) + `ntm --robot-snapshot` (JSON of all pane states) — an external loop controller can poll fleet state without parsing scrollback.
- **Irrelevant to context mgmt:** NTM has zero knowledge of what's *inside* Claude's context. The `/clear`, threshold logic, prompt re-injection all live above NTM.

## 2. The reference loop: `claude_code_agent_farm` (file `claude_code_agent_farm.py`)
- **Loop structure:** one Python controller process owns a monitor thread polling every agent pane every `check_interval`s; `AgentMonitor.check_agent()` captures pane text → classifies `working|ready|idle|error`.
- **Context-exhaustion detection:** `detect_context_percentage()` (~L290) regex-matches Claude's native *"Context left until auto-compact: N%"*; threshold (default 20%) triggers recovery. Per-agent `last_context` tracked.
- **Two-tier recovery:**
  1. **Soft reset** (`clear_agent_context`, ~L2332): send `/clear`, wait ~2s, re-inject the *original prompt* with a fresh random seed; reset `last_context=100`. Same pane, no process restart. **No state hand-off — continuity is the external queue.**
  2. **Hard reset** (`start_agent(restart=True)`, ~L1645): `/exit`, wait for shell prompt, relaunch `cc`; exponential backoff `min(300, 10*2**restart_count)`s.
- **Heartbeat:** `tmux_send()` writes a timestamp to `.heartbeats/agent{N}.heartbeat` on every send; `_check_heartbeat_age()` flags >120s stale → `error`. **Filesystem heartbeats survive the controller itself restarting.**
- **State carry-across = external queue:** agents re-receive the *same* initial prompt; the durable state is a shared `problems.txt` where done items are marked. An agent losing its context loses nothing because it was never the authority — the file is. **Design insight: external durable task state + stateless agents = indefinite loop.**
- **Mass reset escape hatch:** tmux `bind-key Ctrl+R` broadcasts `/clear` to all panes (human-in-loop).

## 3. Multi-agent coordination patterns
- **File-lock registry:** agents write `/coordination/active_work_registry.json` + per-agent `.lock`; check conflicts before claiming. Pure filesystem convention, no daemon.
- **Agent Mail** (`mcp_agent_mail`, `mcp_agent_mail_rust`): formal inbox/outbox + file reservations; NTM integrates it. (Note: harmonik already *rejected* Agent-Mail file-reservations in favor of worktrees+merges — see memory; cite as the path-not-taken.)
- **External queue as HOL-free distribution:** single `problems.txt`, agents grab *random* chunks (per-instance seed) to avoid head-of-line blocking. No central dispatcher; self-scheduling; survives any restart.
- **`frankenterm`** (github.com/Dicklesworthstone/frankenterm): more advanced — Bayesian change-point detection on pane output for rate-limit/state transitions; event stream with per-pane `rule_id`; `tx` engine with prepare/commit/compensate. **Key advance: `ft robot send --wait-for <pattern>` is a *condition-based* send (polls until the agent confirms) — strictly superior to the agent-farm's sleep-based "inject then guess".** Adopt the wait-for idea for "inject digest, wait until agent ready."

## 4. Documented anti-patterns / failure modes (hard-won, worth lifting)
- **settings.json corruption under concurrent launch** → file lock (`_acquire_claude_lock`, 5s timeout) + tar.gz backup w/ 10-copy rotation.
- **Shell-stabilization race** after `/exit` before relaunch → `_wait_for_shell_prompt()` (30s) or silent failures/hangs.
- **Context % is a LAGGING indicator** — only updates when Claude emits output; a stuck agent shows stale numbers → heartbeat compensates (silent 120s = hung regardless of last %).
- **Stale-open claims:** claim registry has no TTL → dead claimant leaves orphan locks needing human cleanup. The farm *avoids* this by random sampling (no long-term ownership). → flywheel claim/lease mechanism MUST have a TTL/lease (feeds the consistency thread).
- **`pkill claude` is catastrophic** — kills all working agents; code comment "NEVER EVER kill all claude-code processes!" Granularity must be per-pane.
- **Post-compaction state loss** (`post_compact_reminder` repo): after compaction Claude forgets AGENTS.md/conventions/task state; his fix = hook injects a re-read reminder. For our no-compaction design the equivalent is: a fresh-context agent must receive a deterministic digest before acting; **the hook is the *delivery* mechanism, not the solution.**
- **Idle-timeout tuning:** `calculate_adaptive_timeout` = 3× median work-cycle, bounded [30s,600s]; too short → spurious restarts during long test runs.

## 5. Take vs. build (for flywheel)
- **Take/adapt:** NTM spawn+pane labeling & checkpoint/restore; filesystem heartbeats; soft `/clear`+reinject and hard exit+relaunch+backoff; external-queue-as-durable-state; frankenterm `--wait-for` condition-based send; lease TTLs (lesson from their absence).
- **Build from scratch (no reference exists in this corpus):** the **prompt-cache-stable fixed prefix injection**, the **deterministic state digest construction** for the *stateful controller*, and **context-budget estimation without sliding windows**.
- **Crucial reframe:** Emanuel sidesteps the digest by making *workers* stateless against an external queue — valid, and harmonik's queue/beads already ARE that durable authority for *worker* state. But the **orchestrator/controller is stateful** (in-flight concurrent runs, batch outcomes, retry counts, decisions); restarting it requires reading that state from a file = the digest problem flywheel must solve.
