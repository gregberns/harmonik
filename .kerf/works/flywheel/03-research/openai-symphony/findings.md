# Research — OpenAI Symphony

> Component: `openai-symphony`. Source: research sub-agent (sonnet) over github.com/openai/symphony, 2026-05-27. Weight: significant on the SPEC, modest on the Elixir prototype ("engineering preview").

## TL;DR
- Symphony is **spec-first**: an 80KB normative `SPEC.md` (the deliverable; README even says "tell your coding agent to build Symphony from the spec") + a prototype Elixir impl.
- Solves long-running agents via **session recycling, NOT context management**: each worker runs up to `max_turns` (default 20) on one live thread, then the orchestrator restarts a fresh session. Context deliberately discarded; **the durable store (Linear tracker + persistent filesystem workspace) is the memory.**
- **Zero LLM context-window management in the spec** — no summarization, no sliding window, no compaction. Assumes the context fills and the session dies; the daemon re-dispatches a fresh agent against the same workspace.

## The loop (SPEC §16.1-16.2)
A long-running daemon polling loop:
```
on_tick: reconcile_running_issues → validate_dispatch_config → fetch_candidate_issues
       → sort_for_dispatch (priority, then oldest createdAt)
       → for issue in sorted: if no_slots break; if should_dispatch: dispatch
       → schedule_tick(poll_interval_ms=30s default)
```
Single daemon = sole authority for dispatch/retry/state; one instance, no distributed coordination. **No LLM in the loop — the loop is plain code; LLMs are only the workers.** (Strong corroboration for flywheel's Architecture B.)

## "The store is the memory" (§14.3) — the central insight
Tracker (Linear) + filesystem workspace = durable memory; orchestrator in-memory state is ephemeral. *"Restart recovery means the service can resume by polling tracker state and reusing preserved workspaces. It does not mean retry timers, running sessions, or live worker state survive restart."* On restart: no timers restored, no sessions assumed recoverable; cleanup terminal workspaces, poll fresh, re-dispatch eligible. In-memory state (running map / claimed set / retry_attempts / completed / token totals) is throwaway; the workspace dir survives and is reused (§9.1 "workspaces reused across runs; successful runs don't auto-delete"). **Stateless agents over a durable store** — exactly the pattern.

## Context across sessions (§16.5, §7.1, §5.4)
Within a session: up to max_turns on the same `thread_id`; continuation turns send "only continuation guidance, not resend the original prompt already in thread history." Across boundaries: **no context bridging** — `stop_session` then `start_session(workspace=path)` = fresh context; only the re-rendered prompt template (with `issue`+`attempt` vars) is passed. **The `attempt` variable is the sole cross-session state signal** (null=first run, int=retry); the prompt author decides how to branch — Symphony provides the variable, not the strategy. **No summarization anywhere; the workspace IS the context.**

## Multi-agent + queue fullness
Implicit coordination via shared tracker state (no conductor/worker metaphor despite the name). Global `max_concurrent_agents` + **per-state slot caps** (`max_concurrent_agents_by_state`, e.g. cap "Merging" at 2 while "In Progress" runs 8) — flywheel's queue-pressure thread borrows this. `dispatch_issue` adds to running/claimed; loop dispatches until `no_available_slots`, then breaks (fills all slots every tick). No work-stealing/preemption; first-in-slot wins, sort gives highest-priority-oldest the next slot. Failure recovery: normal exit → 1s continuation retry; abnormal → exp backoff `min(10000·2^(attempt-1), cap=5min)`; stall detection kills + retries; reconciliation each tick re-fetches running-issue state (terminal → terminate+cleanup); config hot-reload via filesystem watch.

## Calibration + gap for harmonik
Spec = real production-grade design (pseudocode-precise §16); Elixir = "engineering preview," not hardened. **Gap Symphony doesn't solve for harmonik:** its "state digest" is the Linear ticket description (rich natural language) + the `attempt` integer; harmonik needs a **deterministic machine-readable digest** an agent parses to re-orient without reading the full workspace. Symphony delegates that entirely to the prompt author → **flywheel must specify the digest schema explicitly** (the context-management-no-compaction thread does).

## Mapping
| flywheel constraint | Symphony answer |
|---|---|
| No LLM compaction | same — not used, not considered |
| Fixed cache-stable prefix | re-rendered WORKFLOW.md prompt per session |
| Small deterministic digest | `attempt` var + workspace filesystem (under-specified for us) |
| Survive crash/recycle | restart = fresh poll + re-dispatch; workspace survives |
| Track in-flight work | running map + claimed set, rebuilt from tracker on restart |
| Prompt-cache integrity | not addressed (sessions just start fresh) |

## Source
github.com/openai/symphony (SPEC.md §5,§7,§9,§14,§16,§18); local note docs/concepts/symphony.md (accurate but undersells that context-mgmt is absent-by-design).
