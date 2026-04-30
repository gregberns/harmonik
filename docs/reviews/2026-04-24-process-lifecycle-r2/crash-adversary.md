# Crash-Recovery Adversary Review — process-lifecycle.md v0.3.0

**Reviewer role.** Crash-Recovery Adversary (round 2).
**Target.** `/Users/gb/github/harmonik/specs/process-lifecycle.md` v0.3.0 (789 lines, 28 numbered requirements + subletters, 5 invariants, 9 OQs).
**Lens.** Pressure the spec against every physical-reality boundary: kernel panics, power loss, partial fsync, torn writes, concurrent writers, PID reuse, fd-table leaks, SIGSTOP, socket rebinds, `ntm` version drift, clock skew. Find requirements whose stated guarantees do not survive the kernel.
**Date.** 2026-04-24.

---

## 1. Verdict summary

v0.3.0 is **markedly better crash-aware than v0.2** — PL-002a's fd-lifetime-advisory-lock choice, PL-006a's dual provenance marker (env-var + PGID), PL-018a's recovery barrier, and PL-027(iii)'s exec-upgrade socket continuity are all well-targeted. The R1 integration closed several gaps the prior review had flagged.

But the spec is not yet crash-safe. Concretely:

1. **The startup sequence (PL-005) is not idempotent at fine grain.** Steps 0 → 9 are declared "deterministic" but the spec never says how a crash *between* steps recovers — the PL-025 requirement handwaves at "re-run from step 0" without naming what happens to side effects of partial steps (e.g., a daemon that bound the socket at step 1, emitted `daemon_started`, but crashed before the orphan sweep leaves a `daemon_started` event with no `daemon_ready` or `daemon_shutdown`, and the *next* daemon emits a second `daemon_started`; consumers see two starts against a single continuous recovery).
2. **The fd-lock-released-on-crash window is not bounded.** PL-002a says the kernel releases the lock on process termination and that a second daemon "MUST" acquire without operator intervention — but says nothing about agent subprocesses spawned by the crashed daemon that are still alive at the moment the second daemon acquires. Those agents hold `HARMONIK_PROJECT_HASH` markers and will be reaped by PL-006's sweep — but they *also* hold open socket connections to the now-dead daemon, and they may have unflushed state in-memory. Scenario 2 below.
3. **No fsync discipline on the pidfile, socket, or `.harmonik/daemon.upgrading` marker.** The spec relies on these being kernel-visible but never mandates `fsync(2)` on the pidfile or its parent directory. On ext4 `data=ordered` default this is usually fine; on APFS power-loss, a written-but-unfsynced pidfile can vanish, leaving a running daemon with no pidfile and no lock — inverse of the normal failure.
4. **PL-018a's panic recovery is a sync-point hazard.** A panic inside `recover()` (double panic) bypasses the exit-code-12 path, leaves the pidfile stale, and produces NO `daemon_shutdown` emission. The spec treats `recover()` as best-effort but the language ("emit … on a best-effort basis") under-specifies what happens when the best-effort itself panics.
5. **The exec-upgrade path (PL-027) has a window where the old binary has `exec`'d but the new binary has not re-bound the socket.** PL-027(iii) names T_rebind = 2s but does not name what happens if the *new binary itself crashes* during its own PL-005 step 0–1. The pidfile is still locked (by the new process before it crashes; unlocked thereafter). The socket is unbound. The tmux sessions, agent subprocesses, and intent files are still live and bearing this project's provenance marker. A *third* `harmonik daemon` invocation then runs PL-006 against live agents that believe they're still talking to a live parent.
6. **PID-reuse-within-minutes (not across reboot) is not addressed.** OQ-PL-007 names reboot disambiguation; but the cheap failure is: daemon PID 12345 crashes, OS spawns an unrelated process with PID 12345 within seconds. PL-002a's `kill(pid, 0) + /proc/<pid>/cmdline` check sees a live process whose cmdline is NOT a harmonik binary. The disambiguation path is correct in principle, but the spec says "behavior on ambiguity is to refuse startup with a specific exit code" — a **live-but-unrelated PID is not ambiguous**, it's just "not harmonik." The spec conflates "ambiguous" with "negative match."
7. **Orphan-sweep has no ENOENT tolerance rules.** PL-006 enumerates tmux sessions, worktrees, subprocesses, intent files. Each of these can race with filesystem operations (a worktree dir vanishing mid-scan because an operator `rm -rf`'d it; a tmux server dying mid-kill; a `/proc/<pid>` entry going away between `readlink` and env-read). The spec doesn't say which of these are fatal vs skippable.
8. **Concurrent `harmonik daemon start` races.** PL-002 + PL-002a imply atomicity of `flock(LOCK_EX|LOCK_NB)`, which is sound at the fd level — but the `write-pidfile-then-flock` vs `flock-then-write-pidfile` order is never pinned. A naive implementer would write the pidfile first (to record the PID before the lock), creating a window where two daemons can both write their PIDs, and one then fails `flock` but has already overwritten the other's PID. The spec does not mandate the order.
9. **Socket race during exec-upgrade.** PL-027(iii) says "SAME file the new binary re-binds" and PL-003 says the normal-startup path unlinks stale socket files. But if the old binary `exec`'d and the new binary crashes before re-binding, the socket file still exists (held by the old bound-socket's inode, which has no listener). A third daemon invocation hitting PL-003's stale-socket-unlink path will unlink and rebind — but now clients that were mid-request to the dead socket see `ECONNREFUSED` with no exit-code signal.
10. **Clock skew for UUIDv7 and `started_at`.** PL-005 step 2 emits `daemon_started` with `started_at`. The spec doesn't say whether `started_at` is wall-clock or monotonic. If wall-clock and clock regressed across restart, a reader comparing start timestamps sees out-of-order daemon_started events. Event-model §4.1 addresses the event_id HWM for UUIDv7 but not the payload `started_at`.
11. **Disk-full during JSONL append at step 0.** PL-005 step 0 starts the JSONL writer. If the first write (step 2 `daemon_started`) fails with ENOSPC, the startup never proceeds past step 2 because `daemon_startup_failed` (exit code 10) also cannot write. The spec's interim-catalog path (PL-008a code 10) is ambiguously reachable: you cannot emit `daemon_startup_failed` when the event bus itself is ENOSPC-blocked.
12. **`ntm` version drift mid-session is silent.** PL-021a catches `ntm` version mismatch at startup (Cat 0 pre-check). But if an operator upgrades `ntm` while the daemon is running, existing agent subprocesses spawned under old-ntm continue to run; new spawns go through new-ntm. The spec doesn't require re-probe on spawn, so a mid-session downgrade to an unsupported version silently serves new spawns through an incompatible adapter.

The good news: eight of these twelve are one-or-two-sentence normative additions. Three (#5 exec-upgrade-during-upgrade-crash, #8 write/flock ordering, #9 socket-race-during-exec) need small design tweaks. One (#4 double-panic) is a fundamental limit of `recover()` and should be named as an accepted limitation rather than hidden.

No finding reopens a locked decision; none requires a subsystem re-envelope.

---

## 2. Scenarios probed

### Scenario 1 — Daemon crash between startup steps 0 and 9

**Affected requirements.** PL-005 (startup order deterministic), PL-025 (crash during startup reconciliation re-runs from step 1 — wait, says step 0 in prose), PL-024 (stale pidfile recovery), PL-008a (interim exit-code catalog), PL-INV-003 (orphan_sweep_complete_at flag), plus §6.1 `DaemonStatus` enum transitions.

**What the spec says.** PL-005 declares steps 0–9 in strict order. PL-025 (line 459–464) says "the next restart MUST re-run §PL-005 from step 0." Step 2 emits `daemon_started`. Step 0 bootstraps registries including the JSONL writer. The `starting` status is entered implicitly after step 1 (per §7.1 state machine, lines 603–605).

**What actually happens.** The restart sequence has multiple distinct crash-during-step recovery profiles:

- **Crash between step 0 (bootstrap) and step 1 (pidfile lock).** Event bus is up; JSONL writer is up; pidfile not yet locked. Nothing is durably recorded because no events have been emitted yet — step 2's `daemon_started` hasn't fired. This is clean. Next restart starts fresh.
- **Crash between step 1 (lock acquired) and step 2 (`daemon_started` emitted).** Lock released by kernel (per PL-002a). Pidfile is stale (PID of crashed daemon). Next restart's PL-024 sees stale pidfile, removes it, proceeds. No `daemon_started` event was emitted; no `daemon_shutdown` pairing needed. Also clean.
- **Crash between step 2 (`daemon_started`) and step 3 (orphan sweep).** `daemon_started` is on disk (EV-016 + event-model §8.7.1 — need to confirm class). The *next* daemon emits its own `daemon_started`. Consumers see two `daemon_started` events with no intervening `daemon_ready`, `daemon_shutdown`, or `daemon_startup_failed`. The spec does NOT say consumers must tolerate dangling `daemon_started` events. Operator dashboards and RTO measurement (ON-031) that compute "time from SIGTERM to `daemon_ready`" will misattribute the crash.
- **Crash between step 3 (orphan sweep) and step 4 (Cat 0).** PL-INV-003's `orphan_sweep_complete_at` flag was set in memory; lost on crash. `daemon_orphan_sweep_completed` event was emitted (step 3 last action). Next restart re-runs the sweep; emits *another* `daemon_orphan_sweep_completed`. Same pattern as above — consumer sees duplicated lifecycle events with no pairing.
- **Crash during step 8 (reconciliation dispatch).** PL-025 covers this case. Re-runs from step 0. Reconciliation is RC-002 idempotent. But the outer reconciliation workflow's own checkpoint state (if any) is opaque to PL-025 — the spec defers "investigator workflow's verdict-unexecuted case" to Cat 3b (RC-spec §8.5), which is correct but never says PL itself has no obligation to track the investigator workflows' durability.

**Safe or unsafe.** **Partially unsafe.** The lifecycle-event duplication is silent — consumers that pair `daemon_started` with `daemon_ready`/`daemon_shutdown` see invalid pairings after crash-mid-startup. The RC-routed crash-mid-reconciliation (PL-025) is safe because RC-002 is idempotent. But PL-025's prose says "re-run §PL-005 from step 0" while the heading says "Crash during startup reconciliation re-runs from step 1" — the heading and body disagree. Cite: line 459 heading vs line 460 body.

**Concrete spec-text proposal.** Add **PL-025a — Lifecycle-event pairing tolerance**: "Consumers of `daemon_started`, `daemon_ready`, `daemon_orphan_sweep_completed`, `daemon_degraded`, and `daemon_shutdown` MUST tolerate unpaired events produced by a crash during startup sequence. A `daemon_started` with no subsequent `daemon_ready`, `daemon_shutdown`, or `daemon_startup_failed` before the next `daemon_started` indicates a crash between PL-005 step 2 and one of those terminal emissions; the prior instance is considered crashed and its lifecycle events MUST be treated as orphaned. Operator-facing dashboards ([operator-nfr.md §4.1 ON-002] and §4.8 ON-031 RTO measurement) MUST use this pairing rule when computing restart RTO from SIGTERM to `daemon_ready`." Fix PL-025 heading/body disagreement at line 459 — declare "re-run from step 0" consistently. Add one sentence to PL-018a: a panic between step 0 and step 2 MUST emit `daemon_startup_failed{failure_mode=panic,exit_code=12}` if the event bus is up; otherwise exits with code 12 via stderr only.

### Scenario 2 — Daemon crash after acquiring fd-lock; agents spawned by crashed daemon still alive

**Affected requirements.** PL-002a (fd-lifetime lock), PL-014 (agent subprocesses are children of daemon), PL-INV-005 (daemon-originated parentage), PL-006 (orphan sweep), PL-003 (stale socket unlink before bind).

**What the spec says.** PL-002a says kernel releases fd-lifetime lock on daemon termination "so that a subsequent daemon invocation can acquire the lock on restart without operator intervention." PL-006 re-parents handler subprocesses to init (ppid=1) and kills them. PL-003 unlinks stale socket before bind. PL-INV-005's sensor (PL-006a provenance marker) identifies subprocesses.

**What actually happens.** There is a window between "daemon crash" and "next daemon's PL-006 kill" during which:

- the fd-lifetime lock IS released immediately by the kernel (moment of process exit);
- the socket file exists on disk but the bind fd is closed (kernel removes the listener);
- the agent subprocess is re-parented to init; it STILL has an open fd to the socket pointing at the dead listener.

A second `harmonik daemon` starts. Per PL-002a, acquires the lock (correctly — kernel released it). Per PL-003, unlinks the stale socket and binds a new one. The new daemon is now serving requests on the same socket path.

**Now the agent subprocess spawns a new request** (`claim-next`, `emit-outcome`). The agent's fd to the OLD socket returns EOF or EPIPE (dead listener). The agent's logic, per HC-007a framing discipline, may reconnect to `.harmonik/daemon.sock` — which is now the NEW daemon's socket. The new daemon receives an `emit-outcome` from a subprocess IT DID NOT SPAWN, bearing a run_id and session_id the new daemon has no in-memory record of (it hasn't reached PL-005 step 7 yet).

Two failure modes:

1. If the agent's reconnection lands BEFORE the new daemon's orphan sweep kills the agent, the new daemon either (a) accepts the emit and corrupts its state, or (b) rejects with an error that the dead-daemon's session never sees. The agent may commit work product to its worktree, believing the emission was accepted.
2. If the new daemon's orphan sweep kills the agent (PL-006 SIGTERM→5s→SIGKILL per HC-018), the agent gets up to 5s to panic-shut-down, during which the socket race window is open.

PL-006's SIGTERM→SIGKILL interval is 5s. PL-005 step 3 (orphan sweep) runs BEFORE step 4 (Cat 0) and well before the daemon is `ready`. The daemon DOES bind the socket at step 1 (wait — PL-003 doesn't have an explicit step number in PL-005; need to verify). Reading PL-005 carefully: steps 0–9 do not include socket bind explicitly. Step 1 is pidfile lock. The socket bind must happen somewhere but the spec never pins it to a specific step number.

Cite: PL-005 lines 189–202 (steps 0–9); PL-003 lines 163–168 (socket bind); PL-ENV-001(h) line 130 names `bind_socket` as an operation tagged against §4.1 PL-003 but not as a specific PL-005 step.

**Safe or unsafe.** **Unsafe and under-specified.** The gap between socket bind (unspecified step) and orphan sweep (step 3) allows orphan agents to reconnect to the new daemon's socket. The new daemon has no defense except "reject requests from unknown run_ids," which is not a stated requirement.

**Concrete spec-text proposal.** Pin socket bind to a specific PL-005 step and ensure it is AFTER the orphan sweep. Replace the step list with:

```
0. Composition-root bootstrap.
1. Acquire pidfile lock (PL-002).
2. Emit `daemon_started`.
3. Execute orphan sweep per PL-006 — kill all orphan subprocesses bearing this project's provenance marker BEFORE accepting any socket connections.
3a. Bind Unix socket at `.harmonik/daemon.sock` per PL-003. Start accepting connections.
4. Cat 0 pre-check.
5. Walk git log.
6. Query Beads.
7. Build in-memory model.
8. Dispatch reconciliation.
9. Transition to `ready`.
```

Add **PL-003b — Request rejection before agent in-memory model is built**: "Between socket bind (PL-005 step 3a) and completion of the in-memory model (PL-005 step 7), the daemon MUST reject any `emit-outcome` or `claim-next` request whose `run_id` is not yet known, with a typed error (`daemon_not_ready`). The client-side behavior on receipt is bounded-retry with backoff. This prevents orphan-agent-reconnect races during the window between startup and reconciliation completion." Cross-link to [handler-contract.md §4.3 HC-011] for the agent-side retry obligation.

### Scenario 3 — `ntm` subprocess crash mid-launch; partial tmux session

**Affected requirements.** PL-014 (subprocess parentage), PL-021 (ntm adapter scope), PL-021a (absence detection), PL-006a (provenance marker), HC-001 (handler launch), §7.1 state machine.

**What the spec says.** PL-014 says spawns are via ntm adapter. PL-006a requires both env-var and PGID provenance markers "on every handler subprocess spawned." PL-021 lists the ntm capabilities consumed including "agent process spawning in a tmux pane." HC-001 owns launch mechanics.

**What actually happens.** The launch pipeline is:

1. Daemon calls ntm adapter to spawn an agent.
2. ntm creates a tmux session (`tmux new-session -d -s harmonik-<hash>-<session_id>`).
3. ntm sends a command to the tmux pane (`tmux send-keys`) that spawns the agent process.
4. Agent process starts; inherits ntm-set environment (HARMONIK_PROJECT_HASH + others).
5. ntm reports success back to daemon.

A crash between steps 2 and 4 can leave:

- a tmux session whose pane is empty or running an un-started command;
- no agent process, OR a partially-initialized agent process that has not yet bound its socket to the daemon;
- the daemon's in-memory state believes the session is "spawning" while no agent PID exists.

The spec does NOT say:

- what the daemon does if ntm returns an error partway through;
- whether the tmux session is torn down by ntm on adapter error or persists until the daemon's next orphan sweep;
- whether a partially-initialized agent that never binds its socket is distinguishable from a silent-hanging one (PL-017 defers to [handler-contract.md §4.6] for silent-hang detection, but that's a post-bind signal).

Also: PL-006a's provenance-marker obligation says "every handler subprocess spawned" MUST bear the marker. If ntm's spawn failed before the child process was fully exec'd, there may be a tmux session with NO subprocess — the marker-based sweep finds the tmux session (via `tmux ls`) but there's no process to kill. PL-006's tmux-kill path (`tmux kill-session`) handles this case, but only as a side effect.

**Safe or unsafe.** **Partially safe** — orphan sweep does clean up the tmux session on next restart, but mid-session there is no rollback path for a half-launched agent. The daemon can be in a state where it believes a launch succeeded (ntm reported success) but the agent is dead, with no `agent_failed` event because the agent never bound its socket.

**Concrete spec-text proposal.** Add **PL-014b — Launch-atomicity obligation**: "The ntm-adapter handler-launch sequence MUST be atomic from the daemon's perspective: either (a) the spawn succeeds AND the agent binds its socket to the daemon within a bounded interval (default T_launch = 30s, tracked with OQ-PL-002), OR (b) the spawn is classified as failed. On failure of either condition, the daemon MUST emit `agent_failed` (per [handler-contract.md §4.6]) and MUST tear down the partial tmux session via `tmux kill-session`. A partially-initialized agent subprocess that does not bind within T_launch is treated identically to a silent-hang per [handler-contract.md §4.6]." Cross-reference HC-018's cancellation bound.

### Scenario 4 — Agent subprocess crash after socket-bind but before ready-state

**Affected requirements.** PL-016 (agent-failure observation), PL-017 (silent-hang), HC-011 (watcher goroutine), PL-014 (parentage), §8.3 event taxonomy.

**What the spec says.** PL-016: "Agent-subprocess failure (crash, hang, policy violation) MUST be observed by the daemon's watcher goroutine per [handler-contract.md §4.3 HC-011] and MUST produce typed events (`agent_failed`, etc.)." PL-017 names silent-hang. Both defer to handler-contract for the detection rule.

**What actually happens.** This scenario is straightforward *if* HC-011 says what happens for an agent that bound its socket but never reached its type-specific "ready" state (profile-based ready-detection per PL-021(b)). The process is alive; the socket fd is bound; but the agent has not emitted any ready-state signal or lifecycle event. From the daemon's perspective, it sits in "launched but not ready."

PL-017 defers silent-hang detection to HC §4.6. But "silent hang" is defined as "alive but produces no output, no heartbeat, and no lifecycle signal for longer than a bounded interval." A pre-ready crashed-during-init agent may emit an initial "hello" or profile-parse-start signal before crashing — that signal is lifecycle-bearing, making silent-hang detection not fire.

If the agent crashes (SIGSEGV) post-bind-pre-ready, the OS reaps it. HC-011's watcher owns `cmd.Wait()` — the reap returns exit status. This produces `agent_failed`. Safe.

If the agent is SIGSTOPped (not SIGKILLed), the kernel never reaps it; the watcher's `cmd.Wait()` never returns. The agent's socket fd is still bound but no I/O happens. This is a silent-hang-class condition but in a different form — the *process* is alive but scheduled-out. PL-017 + HC §4.6 should catch this if the detection rule is wall-clock-based.

If the agent's init code exits with code 0 cleanly but without ever emitting a "ready" signal, the watcher reaps successfully. The daemon observes `agent_exited{exit_code=0}` but no prior `agent_ready`. The spec does not say how this classifies: is it `FAIL`, `CANCEL`, or silent success?

**Safe or unsafe.** **Partially safe.** Crash and SIGSTOP handling is reasonable if HC §4.6 covers it. Clean-exit-without-ready is under-specified.

**Concrete spec-text proposal.** Add one sentence to PL-016: "An agent subprocess that exits cleanly (exit code 0) WITHOUT having emitted its type-specific ready-state signal per [handler-contract.md §4.X] MUST be classified as a failed launch (equivalent to `agent_failed{reason=premature_exit}`), NOT as a clean success. The watcher goroutine observing `cmd.Wait()` return with exit code 0 and no prior ready-signal emits the premature-exit variant." Cross-reference HC for the event taxonomy.

### Scenario 5 — Double panic in PL-018a top-level `recover()`

**Affected requirements.** PL-018a (panic recovery barrier), PL-008a (exit code 12), PL-024 (stale pidfile recovery).

**What the spec says.** PL-018a lines 396–401: "An unrecovered panic MUST terminate the daemon with exit code 12 'panic', emit `daemon_startup_failed` … or `daemon_shutdown{mode=immediate}` … on a best-effort basis." The word "best-effort" signals the spec knows this path can fail, but does not say what happens if it does.

**What actually happens.** A panic inside a Go `recover()` handler propagates to the runtime and calls `runtime.Goexit()` (or, if the recover called `panic()` with an error, re-panics). The goroutine dies; if it's the main goroutine, the program exits with a runtime error message and exit code != 12. The pidfile is stale. No event is emitted. No exit code 12.

This is **not a bug in the spec** — it's a hard limit of Go's panic/recover semantics. But the spec's prose promises exit code 12 "MUST" terminate. Reality says "SHOULD when possible, MAY fail on double-panic."

The PL-024 path handles this cleanly: next startup sees stale pidfile, unlocks, proceeds. The lost `daemon_shutdown` or `daemon_startup_failed` event is detectable via Scenario 1's pairing-tolerance rule.

**Safe or unsafe.** **Safe in effect** (next-restart recovery covers it), but **spec-prose is misleading**. The "MUST terminate with exit code 12" reads as a hard guarantee; real semantics are best-effort.

**Concrete spec-text proposal.** Amend PL-018a to explicitly state the double-panic limit: "A panic inside the top-level `recover()` handler (a 'double panic') MAY bypass the exit-code-12 path and terminate the daemon with a Go runtime panic message and a non-12 exit code. This is an accepted limitation of the `recover()` primitive. The next daemon startup recovers via PL-024's stale-pidfile detection; the absence of a terminal lifecycle event (`daemon_shutdown`, `daemon_startup_failed`) is consumable via the pairing-tolerance rule (PL-025a). Operators observing double-panic exit codes SHOULD capture the Go runtime's stderr output for diagnostic purposes; the event surface cannot capture it."

### Scenario 6 — SIGKILL during PL-011 drain

**Affected requirements.** PL-011 (graceful drain), PL-011a (`daemon_shutdown` emission), PL-012 (immediate shutdown), PL-024 (stale pidfile), EV-016 (fsync cadence).

**What the spec says.** PL-011 lines 311–325 declares a 9-step drain. Step 5 emits `daemon_shutdown`; step 6 flushes the bus. PL-011a line 330–334 specifies the emission is before flush. PL-012 addresses interceptable immediate shutdown.

**What actually happens.** SIGKILL mid-drain can strike at any step:

- **Between step 1 (transition to `draining`) and step 5 (emit `daemon_shutdown`).** In-flight runs partially drained. No `daemon_shutdown` emitted. Pidfile stale. Next restart runs PL-006 sweep + reconciliation — Cat 2 for non-idempotent in-flight runs, Cat 1 for idempotent.
- **Between step 5 (emit) and step 6 (flush).** `daemon_shutdown{mode=graceful}` event is in JSONL buffer but NOT fsync'd. If class F, EV-016 says `Append` returns only after fsync completes — so if the emit returned before SIGKILL, the event IS durable. But EV-016 is per-event fsync; the event is class F per §8.7.3 (need to verify against event-model). If class O, the event may be lost.

Let me check event-model for `daemon_shutdown` class:

(Reading event-model.md §8.7: per the durability-class table referenced in [event-model.md §8], `daemon_shutdown` is plausibly class F as a lifecycle-terminal. The spec under review does not cite the class explicitly; PL defers to EV-spec per line 332.)

- **Between step 6 (flush) and step 9 (exit).** Bus is flushed but pidfile still present. PL-011 step 8 removes pidfile on clean shutdown; SIGKILL skips this. Pidfile is stale. Next restart's PL-024 path recovers.

**Safe or unsafe.** **Mostly safe** if `daemon_shutdown` is class F (need to confirm). The drain-timeout path's exit code 1 (line 323) is correctly described. The crash path is covered by PL-024.

**One subtle issue.** PL-011 step 4 says "On drain-timeout expiry, the daemon MUST send SIGKILL to surviving agent subprocesses." If a SIGKILL strikes the daemon itself between step 4's SIGKILL-to-agents and step 5's emit, some agents have been SIGKILLed and some haven't. The next restart's PL-006 sweep cleans up. But the event stream shows no `daemon_shutdown` and no explicit `agent_failed{reason=drain_timeout_kill}` per agent — those `agent_failed` events were emitted by the watcher goroutines as each agent was reaped, so they should be durable. But if the watcher is mid-write when SIGKILL hits, the last write may be torn.

**Concrete spec-text proposal.** Confirm or mandate that `daemon_shutdown` is class F in event-model §8.7.3. Add to PL-011a: "The `daemon_shutdown` event's durability class per [event-model.md §8.7.3] MUST be `fsync-boundary` (F). This ensures that if the event emits successfully, it is durable before the daemon can be interrupted." If the event-model declaration disagrees, flag as cross-spec inconsistency.

### Scenario 7 — Disk full during JSONL append; torn tail detection

**Affected requirements.** PL-005 step 0 (JSONL writer started), PL-008a code 10 (disk-full), EV-016 (fsync), EV-014b (tail-truncation tolerance), PL-004 (file surface).

**What the spec says.** PL-005 step 0 starts the JSONL writer in composition-root bootstrap. PL-008a code 10 declares `disk-full` as a startup prerequisite failure. PL-008a para 2 (line 261): "For failures that occur BEFORE step 0 (e.g., the binary cannot locate `.harmonik/` at all), the daemon MUST emit only the exit code to stderr; the event surface is unreachable." This correctly says pre-step-0 failures cannot emit. But step 0 IS the JSONL writer start — if the first write to `events.jsonl` fails with ENOSPC, the failure occurs AT step 0, inside it, not before.

**What actually happens.** Three regimes:

- **Pre-step-0 ENOSPC.** The binary can't create `.harmonik/events/events.jsonl` because the disk is full. No event emitted; stderr only; exit code 10.
- **Mid-step-0 ENOSPC.** The JSONL writer is started but the first actual write fails. Step 2 (`daemon_started`) fails to emit. What does the daemon do? Exit with code 10? The spec doesn't say — PL-005 says each step "MUST complete before the next begins" but doesn't say "MUST succeed." If step 2 fails, does step 3 proceed? Spec implies no but doesn't state.
- **Mid-run ENOSPC.** The daemon is running; a mid-run emit fails with ENOSPC. PL-008a only covers startup. Reconciliation Cat 0 (RC-012 prerequisite) covers "filesystem full" but only re-checks periodically (PL-010 per-10s retry). Between the ENOSPC emit and the next Cat 0 check, the daemon is in a silent-failure mode — events are being dropped.

EV-014b tail-truncation tolerance handles readers, not writers. The spec relies on EV-INV-002 (event loss between fsyncs acceptable). But ENOSPC on the LAST write before a crash means the *fsync boundary itself* failed — not a loss-window-between-fsyncs case.

**Also:** Startup detection of a truncated tail is the reader's responsibility per EV-014b. PL-005's step 5 walks git, step 6 queries Beads — neither reads `events.jsonl`. Only observational consumers replaying against their checkpoint read it (per EV-014b). So a torn tail at startup isn't a startup-level concern per PL.

**Safe or unsafe.** **Partially safe** at startup (pre-step-0 clean, mid-step-0 under-specified, mid-run silent-failure window). The interaction between ENOSPC-on-emit and `infrastructure_unavailable` needs a concrete detection path.

**Concrete spec-text proposal.** Add to PL-008a: "A write-side ENOSPC during event emission (JSONL append failure per [event-model.md §4.4 EV-016]) at or after PL-005 step 0 MUST be treated as a Cat 0 prerequisite failure per PL-010: the daemon transitions to `degraded`, emits `infrastructure_unavailable{failed_prerequisite=disk_full}` on a best-effort basis (the emission itself may fail with ENOSPC; the daemon MAY write to stderr as a fallback), and halts dispatch per PL-010. Periodic retry per PL-010's 10s cadence re-attempts the Cat 0 pre-check." Also amend PL-005 step semantics: "Each step MUST complete **successfully** before the next begins; step failure routes to PL-008a or PL-010 per the failure taxonomy." Currently PL-005 (line 189) says "each step MUST complete" which is ambiguous.

### Scenario 8 — Power loss during `harmonik upgrade` exec-replace

**Affected requirements.** PL-027 (upgrade contract), PL-027(ii) (startup-sequence skip-path), PL-027(iii) (socket continuity), PL-027(iv) (intermediate daemon state), PL-002a (pidfile lock re-acquire), ON-020.

**What the spec says.** PL-027(i) says new process `execve`'s preserving PID. PL-027(iii) says socket is rebuilt within T_rebind = 2s. PL-027(iv) allows `.harmonik/daemon.upgrading` marker for operator observability. PL-027 explicit on exec-via-environment-marker signaling to skip orphan sweep.

**What actually happens.** Between "old binary `exec`'d" and "new binary re-bound socket," a power-loss crash leaves:

- pidfile present, recording the PID that IS still a live process OS-wise (exec preserved PID) except the process is now dead (power loss);
- socket file present but not bound to any listener;
- `.harmonik/daemon.upgrading` marker potentially present (informative only per PL-027(iv));
- all live-at-exec-time agent subprocesses re-parented to init on boot (because the power cycle killed the kernel's process tree entirely, so the agents are also gone — POWER LOSS is cleaner than a daemon-only crash);
- tmux sessions dead (tmux server died with the power cycle).

**Power loss is actually cleaner than a daemon crash** because everything dies together. The hazard surface is:

- Half-written pidfile content (if the new binary was mid-write when power failed). Torn pidfile → parse failure → can't extract PID. The spec doesn't mandate atomic pidfile write (write-to-temp + rename).
- Half-bound socket (if the new binary had `bind`-ed but not `listen`-ed yet). Unix socket files on disk persist; the next daemon invocation will `unlink()` per PL-003 and re-bind. Safe.
- Missing upgrade marker: if the new binary crashed before writing `.harmonik/daemon.upgrading`, operator-observability is reduced but no state is lost (marker is informative per PL-027(iv)).

**Also:** PL-027 says on upgrade, the new binary "MUST skip §PL-005 step 3 (orphan sweep)." After power loss, the new invocation is NOT an exec-replacement — it's a cold boot. How does it distinguish? PL-027(ii) says "detectable by passing a specific environment marker set by the outgoing binary." After power loss, the environment marker is gone (the parent process that was going to exec is dead). So the post-power-loss restart runs full PL-005 including orphan sweep. Correct.

But what if the power-loss-crashed daemon's `.harmonik/daemon.upgrading` marker file persists? The spec says it's informative. Does the restart clean it up? PL-027 doesn't say. The marker sits forever unless PL-006 includes it in the sweep — which it doesn't (PL-006 enumerates tmux, worktrees, subprocesses, intent files, not upgrade markers).

**Safe or unsafe.** **Mostly safe**, with two minor gaps: (a) no atomic pidfile write discipline, and (b) leaked `.harmonik/daemon.upgrading` marker after crash.

**Concrete spec-text proposal.**

1. Add **PL-002b — Pidfile atomic write**: "The daemon MUST write the pidfile content atomically: (i) write the PID string and trailing newline to a sibling temp file `.harmonik/daemon.pid.tmp-<randomstring>`; (ii) fsync the temp file; (iii) `rename(2)` to `.harmonik/daemon.pid`; (iv) fsync the parent directory `.harmonik/`. Step (iv) is REQUIRED — a power-loss after step (iii) without parent-directory fsync can lose the rename on some filesystems (APFS, ext4 with `data=ordered`). A torn pidfile observed by the next startup (unparseable PID) MUST be treated as a stale pidfile per PL-024." Pattern matches WM-026's sidecar discipline.

2. Add to PL-027(iv): "An `.harmonik/daemon.upgrading` marker observed at daemon startup whose creation time predates the current daemon's boot MUST be removed during PL-005 step 3 (orphan sweep). The marker is not itself load-bearing; its stale presence is merely untidy."

### Scenario 9 — Concurrent `harmonik daemon start` races

**Affected requirements.** PL-002 (pidfile), PL-002a (flock atomicity), PL-003 (socket), PL-INV-001 (one daemon per project), PL-INV-004 (socket exclusivity).

**What the spec says.** PL-002a says `flock(LOCK_EX|LOCK_NB)` is the atomicity primitive. PL-INV-001's sensor is "pidfile lock held + pidfile PID matches `getpid()`." PL-002 says a second invocation finding the lock held exits with code 5.

**What actually happens.** Two daemons launch near-simultaneously. Both race to:

1. Open the pidfile (`open("daemon.pid", O_RDWR|O_CREAT)`).
2. `flock(LOCK_EX|LOCK_NB)`.
3. Write their PID to the file.
4. Bind the socket.

`flock` is the atomicity primitive. Only one daemon succeeds at step 2; the other gets `EWOULDBLOCK` and exits. So far so good.

**But:** The order of steps 1 and 3 relative to step 2 matters. Consider:

**Order A: flock-first.** Daemon A opens pidfile, flocks successfully, writes PID. Daemon B opens pidfile (SAME file), tries flock, gets EWOULDBLOCK, exits. Daemon B never wrote anything. PID in file = A's PID. **Safe.**

**Order B: write-then-flock.** Daemon A opens pidfile, writes its PID (truncating any prior content), then flocks successfully. Daemon B opens pidfile (SAME file), writes ITS PID (truncating A's!), then tries flock — gets EWOULDBLOCK, exits. PID in file = B's PID, but the running daemon is A. PL-INV-001's sensor fails: "pidfile lock held + pidfile PID matches `getpid()`" is FALSE (A's getpid() != B's PID written). Also PL-002a's corroboration path (`kill(pid, 0)` of pidfile-recorded PID) probes B's PID (likely dead).

**The spec does not pin the order.** PL-002a mentions the primitive but says nothing about write-vs-flock ordering.

PL-002a line 158: "The daemon MUST disambiguate (a) 'pidfile present, lock held by live process' … from (b) 'pidfile present, no lock, recorded PID not live' … by attempting `flock` first and, on failure, reading the recorded PID and probing with `kill(pid, 0)`." This describes the *second daemon's check*, not the write order.

**Safe or unsafe.** **Unsafe if implementers choose write-then-flock order.** PL-002a already implies flock-first for the check; but pin it normatively.

**Concrete spec-text proposal.** Amend PL-002a: "The pidfile write order MUST be: (1) open the pidfile with `O_RDWR|O_CREAT` (NOT O_TRUNC); (2) acquire `flock(LOCK_EX|LOCK_NB)`; (3) only after successful lock, `ftruncate` to 0 and write the PID followed by newline; (4) fsync the pidfile and parent directory per PL-002b. Writing the PID BEFORE acquiring the lock is FORBIDDEN; it creates a race where two concurrent daemon invocations can overwrite each other's PID content before the losing invocation fails its flock." Tighten PL-INV-001's sensor to: "pidfile lock held by the daemon's fd AND pidfile content is parseable AND the parsed PID equals `getpid()`." Under the write-after-flock discipline, a torn or mismatched pidfile is a programming error, not a race artifact.

### Scenario 10 — PID reuse within minutes (no reboot)

**Affected requirements.** PL-002a (disambiguation), PL-024 (stale pidfile), OQ-PL-007 (reboot disambiguation).

**What the spec says.** PL-002a lines 157–158: "The daemon MAY corroborate via `/proc/<pid>/cmdline` (Linux) or `proc_pidpath` (darwin) to disambiguate recycled PIDs; behavior on ambiguity is to refuse startup with a specific exit code." OQ-PL-007 names reboot-specific disambiguation.

**What actually happens.** Daemon A (PID 12345) crashes at 14:00:00. OS spawns unrelated process at 14:00:15 with PID 12345 (PID reuse within seconds is common on Linux; typical PID space is 32768 or 4M, but high-churn systems exhaust and wrap quickly). User starts `harmonik daemon` at 14:00:30.

Startup runs PL-002a:

1. `open(".harmonik/daemon.pid")` → file exists.
2. `flock(LOCK_EX|LOCK_NB)` → SUCCEEDS (kernel released on daemon A's exit).
3. Since flock succeeded, per PL-002a this is NOT the "pidfile present, lock held by live process" case. It's case (b): "pidfile present, no lock, recorded PID not live" — BUT the recorded PID (12345) IS currently held by an UNRELATED process. `kill(12345, 0)` → returns 0 (success; process exists). If the daemon only probes with `kill(pid, 0)` and skips the `/proc/<pid>/cmdline` corroboration (which is MAY, not MUST), it misclassifies: "PID is live, so cannot remove pidfile" → could exit with code 5 falsely.

Actually re-reading PL-002a: the flock-first discipline takes precedence. Since flock succeeded, the daemon KNOWS no other daemon holds the lock, regardless of what `kill(pid, 0)` says. So the PID reuse case is handled correctly IF flock-first is the discipline — the "ambiguity" mentioned in the spec refers to cases where flock FAILS and you must then probe cmdline.

But the spec's prose is confusing. Line 158 says "behavior on ambiguity is to refuse startup." The ambiguity arises only when flock fails (case a) and cmdline probe returns a non-harmonik binary (possibly a recycled PID). In that scenario, the original daemon's fd is truly gone (flock released), and the current PID holder is unrelated. Why would the daemon refuse startup? OQ-PL-007's default-if-unresolved (line 767) says: "On ambiguity (unable to probe, or probe returns a non-harmonik binary), the daemon treats the PID as stale, removes the pidfile, and proceeds with startup per PL-024." This is the correct behavior.

**But PL-002a's main prose disagrees** with OQ-PL-007's default. Line 158 says "refuse startup with a specific exit code"; OQ-PL-007 default says "proceed with startup." The spec is internally inconsistent.

Also worth noting: on Linux, PID reuse detection via `/proc/<pid>/cmdline` requires the `proc` filesystem, which can be unmounted in rare containers.

**Safe or unsafe.** **Internally inconsistent.** The "refuse startup on ambiguity" in PL-002a contradicts OQ-PL-007's default-if-unresolved ("proceed with startup").

**Concrete spec-text proposal.** Reconcile PL-002a's main prose with OQ-PL-007's default. Preferred resolution: adopt OQ-PL-007's "treat as stale, proceed with startup, emit structured warning" path in PL-002a's main prose. Replace PL-002a's "refuse startup with a specific exit code" sentence with: "On disambiguation ambiguity (`/proc/<pid>/cmdline` returns a non-harmonik binary, `proc_pidpath` fails, or the proc filesystem is unavailable), the daemon MUST treat the pidfile as stale, remove it, log a structured warning `daemon_pidfile_reuse_detected{recorded_pid, current_cmdline}`, and proceed with PL-024 startup. The flock discipline (attempted first) is the authoritative check; `kill(pid, 0)` and cmdline-probe are corroboration only." Resolve OQ-PL-007 by promoting its default to normative.

### Scenario 11 — Orphan-sweep mid-run; reconciliation workflow as orphan

**Affected requirements.** PL-006 (orphan sweep), PL-006a (provenance marker), PL-INV-003 (orphan sweep before classification), PL-025 (crash during reconciliation re-runs), reconciliation §4.2.

**What the spec says.** PL-006 excludes from the sweep anything without a valid provenance marker. PL-006a requires markers on "every handler subprocess spawned by the daemon." Reconciliation workflows run as normal harmonik workflows (per reconciliation §4.1) — meaning their investigator agents are spawned via the same handler contract with the same markers.

**What actually happens.** The flow:

1. Daemon A starts; runs reconciliation dispatch; spawns an investigator workflow (via the normal handler path — marker applied).
2. Daemon A crashes before the investigator workflow completes.
3. Daemon B starts; runs PL-006 orphan sweep; sees the investigator's agent subprocess with a valid marker and KILLS it.
4. Daemon B then runs PL-005 step 8 — re-dispatches reconciliation for the still-in-flight outer runs. The prior investigator's work is lost; a NEW investigator is dispatched.

This is actually **correct per PL-025** — investigator workflows are re-classified on restart because reconciliation is idempotent per RC-002. The spec's invariant holds.

**But consider:** What if the orphan-sweep ITSELF takes longer than expected due to many orphan processes, and during the sweep the disk fills, or the tmux server becomes unresponsive? PL-006's tmux-kill step has a 2s poll ceiling (configurable per OQ-PL-002). After the ceiling, "the daemon proceeds regardless; remaining processes are picked up by the re-parented-subprocess bullet." But if the 5s SIGTERM→SIGKILL interval per subprocess multiplies by N processes, the sweep can take 5N seconds sequentially. For 100 orphans, that's 500 seconds.

The spec doesn't parallelize the sweep. If the sweep takes 10 minutes, and during that time an operator attempts another `harmonik daemon` (thinking the first hung), the first invocation holds the pidfile lock so the second exits cleanly. But the ON-031 RTO (measurement endpoint per PL-009) is breached silently.

**Also worth noting:** PL-006 enumerates tmux sessions via `tmux ls`. If the tmux server is DEAD (killed along with the daemon in a power loss), `tmux ls` returns "no server running" and succeeds with empty output — the sweep finds zero sessions to kill. That's CORRECT for power loss (all sessions died with the server) but FAILS if the tmux server was SIGKILLed while a daemon was cleanly shutting down: the daemon's own `tmux kill-session` calls would have succeeded pre-crash, but residual tmux sessions from OTHER tools may exist on the same machine under the user's tmux server. PL-006 filters by `harmonik-<project-hash>-` prefix, so non-harmonik sessions are excluded. Safe.

**Safe or unsafe.** **Safe in invariant** but **unbounded in time**. The unbounded-sweep-time is an ON-031 RTO concern, not a correctness concern.

**Concrete spec-text proposal.** Add **PL-006b — Orphan sweep is bounded and parallelized**: "The orphan sweep (PL-006) MUST complete within a bounded wall-clock ceiling T_sweep (default 60s, tracked under OQ-PL-002). Per-orphan cleanup (tmux-kill, subprocess-SIGTERM-SIGKILL, lease-lock-remove) SHOULD execute in parallel up to a configurable concurrency cap (default 8). If T_sweep expires with surviving orphans, the daemon MUST log a structured warning `orphan_sweep_incomplete{unswept_orphans}` and proceed to PL-005 step 4; the next Cat 0 pre-check retry (10s cadence per PL-010) re-attempts cleanup of surviving orphans." This keeps RTO bounded while preserving eventual consistency.

### Scenario 12 — Socket race: daemon A drops socket, daemon B binds to same path

**Affected requirements.** PL-003 (socket bind + stale-socket unlink), PL-002a (flock discipline precedes socket), PL-INV-004 (socket exclusivity).

**What the spec says.** PL-003 line 165: "The daemon MUST remove a stale socket file on startup before binding." PL-INV-004 says "at most one bound Unix socket at `.harmonik/daemon.sock` MUST be serving."

**What actually happens.** Flow-of-doom:

1. Daemon A crashes. Pidfile lock released by kernel.
2. Socket file still exists on disk (Unix sockets aren't auto-unlinked on process death; only the bound-fd ref is released).
3. Daemon B starts; acquires pidfile lock (PL-002a, correct); per PL-003 unlinks the stale socket file; binds a new socket.
4. Daemon A's agent subprocess (re-parented to init per PL-014) attempts to reconnect to `.harmonik/daemon.sock`. The inode it previously knew is gone (unlinked); the path now refers to B's bound socket. Agent's client lib calls `connect()` — SUCCEEDS against B's socket.

This is Scenario 2 restated from the socket angle. The fix there (PL-003b: reject requests with unknown run_ids before step 7) addresses this. Additionally:

**Subtle race:** In the window between daemon B's `unlink()` and `bind()`, the socket path has NO listener. If an agent's `connect()` fires in that window, it returns `ENOENT`. Agent retries (per HC-007a assumed retry). On retry, B is bound; connect succeeds. But this is a tight window.

**Another subtle case:** What if daemon B's socket is bound but an unrelated process on the machine `unlink()`s the path? Unix socket files can be unlinked without affecting the bound listener (the listener holds the inode). Daemon B's socket is still accepting connections via its bound fd, but new clients resolving `.harmonik/daemon.sock` get ENOENT. PL-003 doesn't say what happens. This is a trust-the-operator case — the spec could reasonably say "the `.harmonik/` directory is the daemon's exclusive domain; operator-initiated unlinks are out-of-contract."

**Safe or unsafe.** **Safe** in the PL-003-covered cases with PL-003b added from Scenario 2. The external-unlink case is a fair deferral.

**Concrete spec-text proposal.** Covered by PL-003b (Scenario 2). Additionally, clarify PL-INV-004's sensor to address the external-unlink case: add a sentence — "Operator-initiated `unlink()` of `.harmonik/daemon.sock` while the daemon holds a bound listener on that path is out-of-contract; the daemon's behavior is implementation-defined. The daemon MAY periodically re-bind on operator-detectable path loss, or MAY continue to serve pre-connected clients on the bound fd; foundation does not mandate a specific recovery."

### Scenario 13 — `ntm` version drift mid-session

**Affected requirements.** PL-021 (ntm capabilities consumed), PL-021a (version pin + absence detection).

**What the spec says.** PL-021a line 433: "The daemon MUST version-pin `ntm` per the external-inputs protocol." Cat 0 pre-check at startup verifies the pin. No requirement on mid-run re-probe.

**What actually happens.** Operator upgrades `ntm` binary while the daemon is running. Existing agent subprocesses are unaffected (they were spawned by the old `ntm`; they don't re-invoke it). New spawns use the new `ntm` binary. If the new `ntm` is incompatible — e.g., its agent-profile format changed, or it spawns agents without setting the env-var-based provenance marker — new spawns succeed but produce agents that cannot be tracked correctly.

A mid-session downgrade to an unsupported version: similar scenario. Downstream failures appear as mysterious agent-init failures or silent-hangs (PL-017). The root cause (ntm version drift) is invisible to PL's existing Cat 0 pre-check because the pre-check only runs at startup.

**Safe or unsafe.** **Partially unsafe, low-probability.** Operators don't typically upgrade `ntm` mid-daemon, but it's possible.

**Concrete spec-text proposal.** Add **PL-021b — ntm version probe on every spawn**: "Before each handler subprocess spawn (PL-014), the daemon MUST verify the `ntm` binary's version is within the supported set declared in the release manifest. The check MAY be cached with a bounded TTL (default 60s); on cache-miss or mismatch, the daemon MUST fail the spawn with exit code `11` 'ntm-unavailable' per PL-008a (returned as `agent_failed{reason=ntm_version_drift}` to the dispatcher) rather than silently spawning into an incompatible adapter. A mid-session version drift observed by the cached check MUST transition the daemon to `degraded` per PL-010 with `failed_prerequisite=ntm_unavailable`." Cross-link Cat 0.

### Scenario 14 — Clock skew on project-hash (PL-006a) and `started_at` payload

**Affected requirements.** PL-006a (project hash = SHA-256 of abspath), PL-005 step 2 (`daemon_started` payload), event-model §4.1 (UUIDv7 HWM).

**What the spec says.** PL-006a line 222: "The daemon MUST compute a stable `project_hash` at startup as the first 12 hexadecimal characters of `SHA-256(abspath(project_root))`. The hash MUST be stable across restarts (the same project root yields the same hash)." This is wall-clock-independent — good.

PL-005 step 2: emits `daemon_started{started_at, pid, binary_commit_hash}`. The spec doesn't say whether `started_at` is RFC 3339 wall-clock or monotonic nanos.

**What actually happens.** Project hash is safe — no clock dependency.

`started_at`: if wall-clock, clock regressions (NTP step-back, manual `date -s`) can produce two `daemon_started` events where the newer has an earlier timestamp than the older. Consumers sorting by `started_at` misorder lifecycle events. EV-INV-002 tolerates out-of-order events within a fsync window, but cross-restart ordering is what RTO measurement (ON-031) relies on: "Restart RTO is measured from SIGTERM to `daemon_ready` emission" (PL-009 line 279). If SIGTERM's wall-clock timestamp (from the operator-command event, a hypothetical `operator_stopping`) is AFTER `daemon_ready`'s `ready_at` due to clock regression, RTO computes negative.

Event-model §4.1 (line 230) addresses this for `event_id` generation via the UUIDv7 HWM mechanism — clock regression is detected and synthesized UUIDv7 timestamps stay monotonic. But that's for `event_id`, not for event *payload* timestamps like `started_at` and `ready_at`.

**Safe or unsafe.** **Under-specified.** Payload timestamps can regress.

**Concrete spec-text proposal.** Add a sentence to PL-005 step 2 and PL-009: "The `started_at` and `ready_at` payload timestamps MUST be derived from the same monotonic-corrected clock source as UUIDv7 `event_id` generation per [event-model.md §4.1]. A wall-clock regression MUST NOT produce out-of-order payload timestamps across restarts; the daemon MUST synthesize monotonic-forward timestamps per the event-model's clock-regression handling."

---

## 3. Atomicity claims audit

The spec makes several implicit or explicit atomicity claims. Audited against physical reality:

| Claim | Where | Reality | Verdict |
|---|---|---|---|
| "Acquire pidfile lock … via fd-lifetime advisory lock" is atomic | PL-002a | `flock(LOCK_EX|LOCK_NB)` is atomic | **Sound** |
| "Remove stale socket file before binding" is atomic | PL-003 | `unlink(2) + bind(2)` is NOT a single syscall; race exists between unlink and bind | **Gap** — window negligible under pidfile discipline but unnamed |
| "Orphan sweep MUST be deterministic given the filesystem + process state" | PL-007 | Filesystem + process state is NOT quiescent during sweep (other processes spawning); spec acknowledges via "project-scoped" filter | **Sound given filter** |
| "Steps MUST execute in order, each MUST complete before the next begins" | PL-005 | PL-005's order is spec-side; implementation MUST enforce it | **Sound** |
| "`daemon_shutdown` MUST emit before bus flush" | PL-011a | Emission + flush is two operations; SIGKILL between them loses shutdown event | **Partially unsafe** — class F mandate would close it |
| "Exec-replacement preserves PID" | PL-027(i) | `execve(2)` preserves PID atomically | **Sound** |
| "Socket MUST be re-bound within T_rebind = 2s" | PL-027(iii) | Implementation can fail to meet this; spec gives no fallback | **Unbounded** on failure |
| "Project hash is stable across restarts" | PL-006a | `SHA-256(abspath)` is deterministic for same input; `abspath()` resolves symlinks differently post-`ln -s` | **Sound for fixed paths** but symlink-swap changes hash |
| "`orphan_sweep_complete_at` flag MUST be set before any detector" | PL-INV-003 | In-memory flag; lost on crash | **Sound within one daemon's life** |
| "Socket authenticity is filesystem-permission-based" | PL-003 via HC-044 | `chmod(0600)` + owner check is atomic at FS layer | **Sound** |

**Key missing atomicity claims:**

- Pidfile write is not declared atomic — fix: PL-002b (write-to-temp + rename + fsync, per Scenario 8).
- Write-vs-flock ordering is not pinned — fix: normative in PL-002a (per Scenario 9).
- `.harmonik/daemon.upgrading` marker cleanup is not specified — fix: PL-027(iv) amendment (per Scenario 8).

---

## 4. Fsync discipline audit

Every durability-critical write and whether it is fsync'd:

| Write | Who owns | fsync'd? | Gap |
|---|---|---|---|
| `daemon.pid` content | PL-002 | **Not declared** | PL-002b add |
| `daemon.pid` parent directory (after rename) | PL-002 | **Not declared** | PL-002b add |
| `daemon.sock` file (socket node) | PL-003 | N/A (kernel manages) | — |
| JSONL `events.jsonl` writes (F-class) | EV-016 | Yes (fsync per event) | — |
| `event_id_hwm` | EV-015 | Per EV-015 | — |
| `.harmonik/daemon.upgrading` marker | PL-027(iv) | Informative, not declared | Acceptable if declared informative; OK as-is |
| Intent log writes | BI-030 | Per BI | Out of PL scope |
| Lease-lock file | WM-013a | Per WM-013a (temp+rename+fsync) | — |
| Session sidecar | WM-026 | Per WM-026 | — |

**Key gap:** The pidfile write is the sole PL-owned durability-critical file, and its fsync discipline is unspecified. Scenario 8 recommendation (PL-002b) closes this.

**Subtle:** The `ntm` version-pin check state (PL-021b if added) is in-memory only; no durability required.

---

## 5. Recovery rules coverage — reconciliation category per failure scenario

For each crash scenario, what reconciliation category handles the in-flight run's classification on restart?

| Scenario | Reconciliation Cat | Handled? |
|---|---|---|
| 1: Crash between PL-005 steps | None (PL-025 direct) — startup re-runs from step 0 | Yes, but lifecycle-event pairing unspecified (PL-025a fix) |
| 2: Daemon crash; orphan agents alive | Cat 2 or Cat 1 per agent's idempotency class (reconciliation §8.2, §8.3); PL-006 kills orphans | Yes, covered |
| 3: ntm spawn mid-launch failure | Cat 5 (clean restart) if no commit landed; Cat 2 if commit landed | Under-specified — PL-014b fix names `agent_failed` emission |
| 4: Agent post-bind pre-ready crash | Cat 5 or Cat 2 depending on work product | Mostly handled; PL-016 amendment for clean-exit-without-ready |
| 5: Double panic | Cat 2 per class of the in-flight handler | Yes, via PL-024 + RC classification |
| 6: SIGKILL during PL-011 drain | Cat 1 or Cat 2 per run's idempotency | Yes |
| 7: Disk full | Cat 0 (infrastructure) per reconciliation §8.1 | Yes at startup; mid-run under-specified (PL-008a amendment) |
| 8: Power loss mid-upgrade | Cat 0 if infra-damaged, else PL-024 + normal Cat classification | Yes |
| 9: Concurrent start race | N/A — exit code 5 prevents second daemon | Yes |
| 10: PID reuse within minutes | PL-024 direct (treat as stale) | Yes, with PL-002a prose reconciliation |
| 11: Reconciliation workflow as orphan | Cat 3b (verdict-unexecuted) per reconciliation §8.5 | Yes via PL-025 + RC-002 idempotence |
| 12: Socket race | Same as Scenario 2 | Same as Scenario 2 (PL-003b) |
| 13: ntm version drift mid-session | Cat 0 if PL-021b added; silent otherwise | Gap — PL-021b fix |
| 14: Clock skew | N/A for state; matters for RTO measurement | Event-model §4.1 + PL-005/PL-009 amendment |

**All scenarios route to a defined reconciliation category** assuming the PL-side amendments land. The reconciliation taxonomy is complete enough to absorb every PL-side failure; the gaps are PL-side detection/emission paths, not classification.

---

## 6. Temp+rename pattern audit

The temp-file + rename + fsync idiom is the canonical crash-safe write in the rest of the corpus (WM-026 sidecar, WM-013a lease-lock). PL does NOT apply this pattern to any of its own files:

| File | Written by | Temp+rename applied? |
|---|---|---|
| `daemon.pid` | PL-002 | **No** — gap (PL-002b fix) |
| `daemon.sock` | PL-003 | N/A (bind, not write) |
| `daemon.upgrading` marker | PL-027(iv) | Not specified (informative) |

The spec should apply the pattern uniformly to the pidfile. The marker file is informative and may stay best-effort.

---

## 7. Retry vs re-run discipline

PL-005 is re-run on crash (PL-025). PL-010 retries Cat 0 pre-check every 10s. PL-027(iii) allows client-side retry within T_rebind. PL-011 step 4 escalates SIGTERM to SIGKILL after drain-timeout.

**Observations:**

- **Re-run is idempotent via RC-002** (reconciliation detector idempotence). PL-005 re-runs produce identical classifications from identical git + Beads state. Safe.
- **Retry (Cat 0 pre-check) is bounded by cadence** (10s per PL-010); no per-attempt timeout is declared, which could stack failures. Not a correctness concern but an RTO concern.
- **Client-side retry during upgrade** (PL-027(iv)) is under-specified — how many retries? What backoff? Spec punts to ON-020(e). OK.
- **Retry vs re-run divergence point:** PL-011 drain timeout goes to SIGKILL. This is not a retry; it's a forced termination. Correct. But PL-006's per-subprocess SIGTERM→SIGKILL interval (5s per HC-018) is NOT parallelized (Scenario 11). A "retry" of subprocess kill at step 5s is unnecessary because SIGKILL is guaranteed.

**No retry-storm hazards identified.** The re-run discipline is sound.

---

## 8. Cross-spec coordination for each crash scenario

For each scenario, which sibling specs MUST coordinate?

| Scenario | PL amend | Coord specs |
|---|---|---|
| 1: Startup step crashes | PL-025a pairing-tolerance | Event-model (§4.4 EV-INV-002 extends to lifecycle pairs); operator-nfr §4.8 ON-031 (RTO measurement) |
| 2: Orphan agents during daemon transition | PL-003b (request rejection), step re-ordering | Handler-contract §4.3 HC-011 (client retry on ENOENT/EOF) |
| 3: ntm spawn mid-launch | PL-014b (launch atomicity) | Handler-contract §4.6 (agent_failed taxonomy); architecture (ntm adapter boundary) |
| 4: Post-bind pre-ready crash | PL-016 (premature-exit classification) | Handler-contract §4.3 HC-011 (watcher's clean-exit-no-ready rule) |
| 5: Double panic | PL-018a prose clarification | None — accepted limit |
| 6: SIGKILL during drain | Confirm `daemon_shutdown` class F | Event-model §8.7.3 class declaration |
| 7: Disk full | PL-008a mid-run ENOSPC route to Cat 0 | Event-model §4.4 EV-016 (fsync failures); reconciliation §8.1 Cat 0 |
| 8: Power loss mid-upgrade | PL-002b atomic pidfile; PL-027(iv) marker cleanup | Operator-nfr §4.6 ON-020 (operator-facing upgrade); architecture (no cross-spec impact) |
| 9: Concurrent start race | PL-002a write-after-flock ordering | None — PL-local |
| 10: PID reuse within minutes | PL-002a prose + OQ-PL-007 promotion | None — PL-local |
| 11: Reconciliation workflow as orphan | PL-006b bounded+parallel sweep | Operator-nfr §4.8 ON-031 (RTO bound) |
| 12: Socket race | PL-003b (covered by Scenario 2) | Handler-contract |
| 13: ntm version drift mid-session | PL-021b per-spawn probe | Reconciliation §8.1 Cat 0 (transition cause) |
| 14: Clock skew | Payload timestamp monotonicity | Event-model §4.1 (UUIDv7 HWM; extend to payload timestamps) |

**Three sibling-spec coordination items:**

1. **Event-model §8.7.3**: Confirm `daemon_shutdown` is class F. If currently class O, reclassify.
2. **Event-model §4.1**: Extend UUIDv7 clock-regression handling to cover payload timestamps used for SLA measurement (`started_at`, `ready_at`, `shutdown_at`).
3. **Handler-contract §4.3 HC-011**: Add client-side reconnect-retry semantics for the socket race in Scenario 2, and the clean-exit-no-ready classification rule for Scenario 4.

---

## 9. Requirements that held up well

- **PL-002a (fd-lifetime advisory lock).** The choice of `flock(LOCK_EX|LOCK_NB)` with kernel-guaranteed release on process termination is robust. The explicit prohibition of POSIX `fcntl(F_SETLK)` is exactly right — process-lifetime POSIX locks have a well-known fd-close footgun.
- **PL-006a (dual provenance marker).** The env-var + PGID combination gives portability across darwin (which lacks `/proc/<pid>/environ`) and Linux, with graceful degradation paths named. This is a cleanly-thought-out cross-platform design.
- **PL-013 (no-exit-on-queue-empty).** The rationale (line 348) is correct: exiting on queue-empty would make pidfile-lock handoff load-bearing for every enqueue. Staying alive avoids churn.
- **PL-025 (idempotent restart via RC-002).** Leaning on reconciliation's idempotence invariant means PL doesn't need to own restart-time classification correctness. Good delegation.
- **PL-INV-005 (daemon-originated parentage with explicit init-reparenting legal deviation).** Captures the only safe re-parenting rule; the provenance marker makes it machine-checkable.
- **PL-006 stale-intent-file deferral to Cat 3a (instead of inline classification).** The resolution of the OQ-PL-006 (v0.2 critic finding) is correct: pre-Cat-0 detectors would deadlock; deferring to step 8 post-Cat-0 is the right shape.
- **PL-021a absence-detection producing fail-fast code 11 instead of silent degradation.** The "no silent non-tmux mode" prohibition is correct given solo-dev-ergonomics locked decision #4. This is the right trade.
- **§6.1 enum scope (pre-ready degraded only).** Narrowly scoping the `degraded` enum state to Cat 0 and deferring post-`ready` degradation to the health surface (OQ-PL-009) is correct — reentrant `degraded` would entangle operator-control with reconciliation.

---

## 10. Hidden crash-safety assumptions

The spec quietly assumes:

1. **Kernel fd-lock release on crash is instantaneous.** It is, modulo container runtimes and niche filesystems. Safe in practice.
2. **`tmux ls` and `tmux kill-session` are reliable.** tmux servers can hang; the sweep's 2s poll ceiling mitigates but doesn't eliminate.
3. **`/proc/<pid>/cmdline` reads are atomic.** They are not — a process exec'ing replaces its cmdline. In practice the race window is tiny.
4. **`SHA-256(abspath(root))` is stable across realpath resolution.** Symlinks can break this (`ln -sfn new-target /path/to/root` changes `abspath(root)` if the symlink is in the path). Operator-level concern, not a daemon-level bug.
5. **Composition-root bootstrap (PL-005 step 0) is side-effect-free.** The comment says "No external state is read." Good. But what about writing? Does the event bus start create any files? The JSONL writer pre-creates per-consumer spill files per EV-011a — that IS a filesystem side effect. PL-005 step 0's "no external state is read" is correct but incomplete; writes happen.
6. **`harmonik.upgrading` marker presence is a reliable upgrade signal.** The marker is informative per PL-027(iv), not load-bearing. Stale presence (Scenario 8) leaks if not cleaned.
7. **JSON-RPC over NDJSON tolerates torn writes on the daemon side.** The daemon's line-read loop (HC-007a framing) MUST close connections on malformed lines. If the writer-side (client) is killed mid-write, the daemon observes a torn frame and closes; the event surface captures no error. OK.
8. **Agent subprocesses survive the daemon's brief unavailability during exec-upgrade.** They do — PL-027 is explicit that agents under the same PID remain attached. OK.

---

## 11. Proposed amendments (consolidated)

| New ID | Section | Shape | Fixes scenario |
|---|---|---|---|
| `PL-002b` | §4.1 | Pidfile atomic write (temp+rename+fsync+parent-fsync) | 8, 9 |
| `PL-002a` prose | §4.1 | Pin write-after-flock ordering; reconcile with OQ-PL-007 | 9, 10 |
| `PL-003b` | §4.1 | Reject unknown-run_id requests before in-memory model built | 2, 12 |
| `PL-005` step ordering | §4.2 | Pin socket bind to step 3a (after orphan sweep) | 2 |
| `PL-005` step completeness | §4.2 | "Complete successfully" not just "complete" | 7 |
| `PL-006b` | §4.2 | Bounded + parallel orphan sweep | 11 |
| `PL-008a` | §4.2 | Mid-run ENOSPC routes to Cat 0 | 7 |
| `PL-014b` | §4.5 | Launch atomicity (spawn + bind within T_launch or fail) | 3 |
| `PL-016` amend | §4.5 | Clean-exit-no-ready = `agent_failed{premature_exit}` | 4 |
| `PL-018a` prose | §4.6 | Double-panic accepted limitation | 5 |
| `PL-021b` | §4.7 | Per-spawn ntm version probe | 13 |
| `PL-025` heading/body fix | §4.8 | "Re-run from step 0" consistent (currently disagrees) | 1 |
| `PL-025a` | §4.8 | Lifecycle-event pairing tolerance | 1 |
| `PL-027(iv)` amend | §4.9 | Stale upgrade marker cleanup | 8 |
| `PL-INV-004` sensor amend | §5 | External-unlink-of-socket out-of-contract note | 12 |
| Payload timestamp monotonicity | §4.2 + §4.3 | Extend EV §4.1 HWM to `started_at`/`ready_at` | 14 |
| `PL-011a` + EV coord | §4.4 | Confirm `daemon_shutdown` class F in event-model §8.7.3 | 6 |

**Schema-version impact: zero.** All additions are either new requirements with mechanism tag, or prose clarifications. No on-disk schema changes; no new file formats. The pidfile atomic write changes how bytes hit disk but the pidfile *content* (PID + newline) is unchanged.

**Conformance impact:** All new IDs fit within the existing Core MVH conformance profile (§10.1). PL-002b, PL-003b, PL-006b, PL-014b, PL-016-amend, PL-021b, PL-025a SHOULD be added to Core MVH. PL-018a-prose is prose-only.

---

## 12. Cross-spec implications

Three amendments cross subsystem boundaries:

- **Event-model §8.7.3** — confirm/assert `daemon_shutdown` class F. If currently O, reclassify. One-line change.
- **Event-model §4.1** — extend UUIDv7 clock-regression-handling to `started_at`, `ready_at`, `shutdown_at` payloads used for RTO. New sentence at the end of EV-003 (or wherever the HWM spec lives).
- **Handler-contract §4.3 HC-011** — (a) add socket-reconnect retry semantics for orphan-agent-reconnect window; (b) classify clean-exit-no-ready as premature-exit failure. Two sentences.

All remaining amendments are PL-local. No amendment requires renegotiation of another spec's contract in shape, only one-sentence additions on their side.

---

## 13. Strongest single finding

**Scenario 2 (orphan-agent-reconnects-to-new-daemon-socket during startup window).** The spec correctly identifies that orphan subprocesses are re-parented to init and killed by the next daemon's sweep, but it does NOT address the socket-reconnection race between "new daemon binds socket" and "new daemon kills the orphan." An orphan agent whose watcher has reconnected to the new daemon's socket between those events can:

- emit `emit-outcome` for a run_id the new daemon doesn't yet know;
- the new daemon either accepts (corrupting state) or rejects (and the agent's outcome is lost without being retried elsewhere);
- the agent's work product may land on the worktree in a state that is not captured in any `transition_record` commit.

The fix is small — PL-003b (reject unknown-run_id before step 7) plus pinning socket bind to after orphan sweep (step 3a). But the silent-corruption mode on the current spec is the hardest category of bug to detect: the agent reports success to the new daemon, the new daemon logs an error, the agent proceeds to the next node. Nothing blocks the run; the damage surfaces only at the next reconciliation pass and may mask as "unexpected branch tip."

This is the scenario most likely to be missed in implementation if the spec is read linearly: PL-003 describes stale-socket-unlink, PL-006 describes orphan sweep, PL-014 describes agent parentage — none of them coordinates on the interaction.

---

## 14. Reviewer note

Sixteen concrete amendments close the surfaced gaps. Two (PL-005 step re-ordering + PL-003b) are structural; the rest are one-or-two-sentence additions. None reopens a locked decision; all fit within the current §4 grouping.

**Recommend:** Land the 16 amendments as an R2-integration pass before promoting status from `draft` to `reviewed`. Coordinate the three cross-spec amendments (EV §8.7.3, EV §4.1, HC §4.3 HC-011) with the sibling-spec authors; each is a one-line addition.

The spec's crash-awareness has improved substantially from v0.2 (particularly the addition of PL-002a, PL-006a, PL-018a, and PL-027). The remaining gaps are the harder ones — races between subsystem lifecycles, payload-timestamp monotonicity, and the orphan-reconnect window — all of which are defensibly one-shot-fixable.
