# Emergent Tooling & Safety Patterns — Capture Inventory

**Status:** Living capture — append freely, do NOT formalize or dispatch yet.
**Purpose:** Preserve ad-hoc tools, watchdogs, and safety/observability patterns that emerged organically while building harmonik. Aggregating over several days before deciding what (if anything) to promote to first-class features.

---

## DAEMON / RUN-LIFECYCLE SAFETY

### Escape Detector (`implementer_escaped_worktree`)
Fails a run when an implementer writes into main outside its assigned worktree. Known false-positive window: when main is transiently dirty from a sibling merge/escape or a scratch file (bug hk-77q8e). Triage rule: check if a sibling merged/escaped in the same ~1s window before blaming the named bead.

### Pasteinject Quit-on-Commit Watchdog (hk-trjef)
Sequence: quit → 30s grace → kill → noChange-subsumed check. Guards against implementers that commit but never exit, leaving the run slot permanently occupied.

### Post-Commit `/quit` Watchdog (hk-5s7tg)
Recovers when the implementer has committed but `sess.Wait` is stuck. Complements the pasteinject watchdog — different failure mode (hung session, not missing quit call).

### Merge Auto-Skip + Per-Bead Dispatch Guard (`bead_already_dispatched`)
Two-part protection:
1. **Merge auto-skip**: skips beads that would cause merge conflicts at dispatch time rather than failing mid-run.
2. **`bead_already_dispatched`**: blocks double-dispatch of the same bead across queues — prevents duplicate work when the same bead appears in multiple queue feeds.

### Reconciler
Re-adopts in-flight runs with their original run-IDs after a daemon restart. Prevents the daemon from treating a surviving run as a new dispatch (no double-dispatch, no orphaned run slots).

### Restart-Backoff / Rapid-Boot Guard
Delays daemon restart after rapid successive boots. Prevents thrash loops where a bad config causes repeated fast crashes.

---

## PROCESS / DAEMON SUPERVISION

### External Keeper Script (`/tmp/hk-keeper.sh`)
Auto-revives the daemon if it dies. Design characteristics: key-stripped (subscription-billed API key not embedded), `--no-auto-pull` flag. Runs outside the daemon so it survives daemon crashes.

### Supervisor-Owned `DaemonWatchdog` (hk-3w50w, commit b43f46c7)
In-daemon revival actor — complements the external keeper. The daemon watches itself and can trigger controlled restarts without relying on the external script.

---

## OBSERVABILITY / MONITORING

### `harmonik subscribe` + `events.jsonl` Tail
Typed event stream with server-side heartbeat. Primary agent-side channel for run and health monitoring. The JSONL tail is the reliable fallback when the live subscribe stream drops (e.g. on daemon restart — the stream exits with code 17 and does not reconnect; drain from JSONL by cursor instead).

### Agent-Armed Monitor Patterns
Monitors instantiated by the orchestrator/captain for:
- `run_completed` / `run_failed` / `run_stale` / `merge_conflict` events (keyed by `run_id`, not `bead_id`)
- Daemon-up heartbeat
- File tails for `AGENT_COMMS` and per-agent outboxes

### Disk-Watch + `go clean -cache` Pattern
Worktree and build cache disk pressure hits hard at ~5 GiB / 98% usage — OOM/disk crash threshold documented as hk-dgwf4. Pattern: watch df output, trigger `go clean -cache` proactively before hitting the wall. Note: proactive cache wipe risks removing shared build artifacts mid-build (TOCTOU hazard — see memory reference `cache-reaper-proactive-toctou`).

---

## COORDINATION / DISPATCH

### Pre-Screen Scouts
Before dispatching a bead, check whether the work has already landed. Verification protocol:
- Check `git log` refs AND inspect actual code presence.
- Verify against `main`, **not** `--all` branches — `--all` produces false "already landed" claims when the commit is only on a worktree branch that never merged.

### Per-Agent Outbox Comms + Tails
Predecessor: v0 used a shared `.harmonik/comms/*.md` file-outbox (RETIRED — do not write there). Current: v1 per-agent outbox pattern (being formalized as `agent-comms` hk-uxm0j). Capture both patterns here for historical context during formalization.

### Serialize Shared-File-Conflicting Bead Families
Some bead families all touch the same files (e.g. SDLC group hk-o52fm.* all touch `specs/examples/README.md`). These must be dispatched one-at-a-time rather than concurrently — concurrent dispatch guarantees a merge conflict on the loser. Detection: scan the bead's expected edit surface before dispatch.

---

## GATE / CI ENVIRONMENT

### Go Build Cache Wipe → Stdlib Import Failures
Gate runs against docs-only commits have failed twice (iters 1 and 2, hk-nlhys) with errors of the form `could not import os (open /Users/gb/Library/Caches/go-build/...no such file or directory)`. Different packages each time (`internal/specaudit`, `internal/workspace`, `internal/workflow/scenario`). All pass immediately with a warm cache. Pattern: the gate worker's build cache is being wiped between runs — likely proactive go-cache reaping (see disk-watch entry above) or a cold worker. Docs-only commits should not require a full go-cache warm-up; this is a gate infrastructure gap, not a code defect. Triage rule: if a gate fails with `could not import <stdlib-package>`, check for concurrent cache-reap activity before blaming the commit.

---

## PROPOSED / IN-FLIGHT (idea capture only — not dispatched)

### Session-Keeper (hk-ekap1)
Context-threshold handoff → clear → resume cycle. Signal sources under consideration:
- `statusLine` context indicator
- Stop-hook idle detection
- Pasteinject-watchdog timing
- `PreCompact` backstop

---

## Append Log

*Named-queue agents and flywheel agents: append your daemon-lane tools and internals below as you surface more patterns. Include bead references where relevant.*

| Date       | Agent/Source     | Pattern Added |
|------------|-----------------|---------------|
| 2026-07-05 | implementer-initial (hk-nlhys) | Initial capture from bead description |
| 2026-07-05 | implementer-resume (hk-nlhys iter-2) | Confirmed gate failure was cache miss (stdlib import errors); no code changes; re-commit to clear gate |
| 2026-07-05 | implementer-resume (hk-nlhys iter-3) | Added gate/CI environment section: go build cache wipe → stdlib import failures pattern |
