# Round 1 Implementer Review — process-lifecycle.md v0.2.0

## Verdict summary

The spec is partially implementable. The daemon's shape is legible: a single Go binary per project, a pidfile-locked lifetime, a Unix socket for in-process-tree communication, a deterministic eight-step startup sequence, an orphan sweep, three declared events, and a small status prefix (`starting → reconciling → ready | degraded`). An implementer can lay down `internal/daemon` with these contracts today. But a cluster of surfaces — pidfile-lock mechanics, signal handling, exit-code table, upgrade exec-replacement, subprocess supervision (launch/reap/restart policy), and the entire CLI entry-point surface of §4.10 — hand the implementer an obligation without the contract that satisfies it. The delegation posture is aggressive (8 distinct "owned by [other.md §N]" deferrals in §2.2 alone), and several of those deferral targets are currently **wrong cross-references**: PL v0.2.0 cites `[operator-nfr.md §7.1]`, `[reconciliation.md §9.2]`, `[event-model.md §3.2]`, `[event-model.md §3.4]`, `[beads-integration.md §10.8]`, `[beads-integration.md §10.9]`, `[workspace-model.md §5.1]`, `[execution-model.md §2.1]` — none of these section numbers resolve to the topics PL claims they do. ON-NNN requirements live in §4.1–§4.11, EV events live in §8.1–§8.7, RC detectors live in §4.3 and §8.1–§8.11, BI intent log lives in §4.10 and Beads-CLI skill in §4.9, WM orphan sweep lives in §4.8. The citation bug pervades the spec and blocks downstream consumers from following any delegation chain. Roughly half of the requirements I walked are implementable as written; the other half require either fixing the citation drift or adding missing normative content.

## Requirements I attempted to implement

### PL-001 — One daemon per project — IMPLEMENTABLE

```go
// internal/daemon/scope.go
type ProjectRoot struct {
    Path        string // absolute path to the project's git repo
    HarmonikDir string // <Path>/.harmonik
}

func resolveProject(cwd string) (*ProjectRoot, error) {
    // walk up from cwd to find a .harmonik/ directory inside a git worktree
    ...
}
```

PL-001 is tight: "project = git repo containing `.harmonik/` directory; one daemon per project." The invariant is straightforward to assert via the pidfile lock of PL-002. One open question: OQ-PL-003 (auto-create `.harmonik/`?) directly affects whether `resolveProject` returns an error or bootstraps. PL-001's "project" definition depends on the OQ-PL-003 resolution. The default-if-unresolved (`require harmonik init`) is sufficient to stub against, but PL-001 would be cleaner if it named the precondition.

### PL-002 — Pidfile at `.harmonik/daemon.pid` — PARTIALLY

```go
// internal/daemon/lock.go
type pidLock struct {
    path string
    fd   int
}

func acquirePidLock(path string) (*pidLock, error) {
    fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT, 0600)
    if err != nil { return nil, err }
    // Advisory lock: syscall.Flock(fd, LOCK_EX|LOCK_NB) on Linux/macOS
    if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
        // read PID, check if live, exit with code ??? ("specific error code")
    }
    // write PID...
}
```

Stuck points:

1. **"Advisory lock" is not specified.** `flock(2)` and `fcntl(F_SETLK)` have different semantics across fork/exec boundaries (fcntl locks are released when ANY fd to the file in the process closes — a well-known hazard). Linux-specific `OFD` locks (`F_OFD_SETLK`) are fd-lifetime, not process-lifetime, and work correctly across fork. macOS has no `OFD` locks. The spec says "advisory lock" without naming the primitive; the choice materially affects PL-024's stale-pidfile detection.
2. **"Specific error code" is named but not defined.** PL-002 says "exit with a specific error code directing the operator to query the running daemon (per [operator-nfr.md §7.1])" — but operator-nfr's §7.1 is the operator-control state machine, not the exit-code taxonomy. The exit-code taxonomy lives at operator-nfr.md §8, where code **5** ("pidfile-locked") is declared. PL-002's citation is wrong. An implementer following the cross-ref chain hits a dead end. Fix: change PL-002 to cite `[operator-nfr.md §8 code 5]`.
3. **Race between "find pidfile held by live process" and "stale pidfile with dead PID"** (the PL-024 case). PL-002 + PL-024 together describe the check but not the detection order: (a) try `flock` first, if it succeeds the pidfile was stale and we claim it; (b) try `flock` first, if it fails read the PID and check if live via `kill(pid, 0)` to disambiguate stale-with-crashed-owner vs. held-by-live. The spec does not pick; the implementer must.

### PL-003 — Socket at `.harmonik/daemon.sock` — IMPLEMENTABLE

```go
func bindSocket(path string) (net.Listener, error) {
    _ = syscall.Unlink(path) // "remove stale socket file on startup before binding"
    l, err := net.Listen("unix", path)
    if err != nil { return nil, err }
    _ = os.Chmod(path, 0600) // per HC-044 (handler-contract) — not stated by PL
    return l, nil
}
```

PL-003 specifies the path, the transport (Unix socket), and the stale-unlink. Mode `0600` is stated by handler-contract HC-044 but NOT by PL-003; the daemon cannot know to `chmod(0600)` from this spec alone. This is a content gap, not a delegation: PL-003 should declare the mode because the daemon creates the socket before any handler runs.

### PL-004 — Daemon owns per-project files under `.harmonik/` — PARTIALLY

```go
var allowedDaemonPaths = map[string]bool{
    ".harmonik/daemon.pid":        true,
    ".harmonik/daemon.sock":       true,
    ".harmonik/events/events.jsonl": true,  // ??? where from
    ".harmonik/events/dead-letters.jsonl": true,
    ".harmonik/transitions/":      true,
    ".harmonik/beads-intents/":    true,
    ".harmonik/event_id_hwm":      true,  // declared by event-model.md §4 EV-003a — NOT listed by PL-004
    // worktrees/ is owned by workspace-model, not daemon per se — see below
}
```

Stuck points:

1. **`event_id_hwm` is missing.** Event-model §4 declares `.harmonik/event_id_hwm` as a daemon-maintained file (the UUIDv7 high-water-mark). PL-004 does not enumerate it. An implementer reading PL-004 alone would violate the "MUST NOT read or write harmonik-owned state outside this surface" invariant on day one.
2. **Spill files are missing.** Event-model EV-011a declares per-consumer spill files at `.harmonik/events/spill-<consumer>.jsonl`. These are bus-layer files inside the daemon but PL-004's surface enumeration does not list them.
3. **Worktrees are elided.** PL-004 does not list `.harmonik/worktrees/<run_id>/` — the workspace root. This is arguably correct (workspace-model owns worktrees), but the pidfile at `.harmonik/worktrees/<run_id>/.lock` declared by HC-044a is daemon-written. The daemon-owned-file surface is leakier than PL-004 claims.
4. **Citation drift.** PL-004 cites `[event-model.md §3.4]` for event log layout — but event-model has no §3.4. The JSONL layout is declared across EV §4 and the co-owned event payload section is §6.5. `[beads-integration.md §10.8]` for intent log is wrong; BI-030 lives at §4.10. `[execution-model.md §2.1]` for transition records is wrong; the transition record is §6.1 of execution-model and §4.4 defines the checkpoint contract.

### PL-005 — Startup order is deterministic — PARTIALLY

```go
func (d *Daemon) Start(ctx context.Context) error {
    // 1. pidfile lock
    lock, err := acquirePidLock(d.paths.Pidfile)
    if err != nil { return err }
    defer lock.Release()

    d.setStatus(StatusStarting)
    emit(ctx, "daemon_started", ...)  // ??? PL-005 does not name this emission

    // 2. orphan sweep
    if err := d.sweepOrphans(ctx); err != nil { return err }

    // 3. Cat 0 pre-check (reconciliation.RC-012)
    if prereq := d.cat0Precheck(ctx); prereq.Failed() {
        d.setStatus(StatusDegraded)
        emit(ctx, "infrastructure_unavailable", prereq)
        return d.waitForPrereqs(ctx)
    }
    d.setStatus(StatusReconciling)

    // 4. git log walk
    commits, err := d.walkCheckpointCommits(ctx) // ??? across which branches?
    if err != nil { return err }

    // 5. Beads query
    beads, err := d.br.ListOpenAndInProgress(ctx) // per BI-004
    if err != nil { return err }

    // 6. build in-memory model
    model := buildRunModel(commits, beads)

    // 7. reconciliation dispatch
    for _, run := range model.InFlight {
        cat := d.reconciler.Classify(run) // RC-010/013
        d.reconciler.Dispatch(cat, run)   // RC-007/008 action-mapping
    }

    // 8. ready
    d.setStatus(StatusReady)
    emit(ctx, "daemon_ready", model.InvestigatorRunIDs)
    return nil
}
```

Stuck points:

1. **Step 4 "walk the git log" does not say which branches to walk.** Every branch? A registry of task branches? The execution-model spec has the same gap (noted in its implementer review, EM-031 active-run discovery). If the daemon tracks task branches in git by naming convention (`task/<run_id>`), the convention is declared in workspace-model.md §4.2, not referenced by PL-005. Implementer must guess.
2. **Step 5 "query Beads via `br`"** is under-specified at the command level. `br` has 30+ commands (per BI-003). Which command lists open + in_progress beads? BI-020 says "`br ready` (or its equivalent)." Is `ready` filtered to `open` only, or does it include `in_progress`? PL-005 step 5 needs a named command or a citation to BI-020 at minimum.
3. **`daemon_started` is not named in the startup sequence.** Event-model §8.7 declares `daemon_started` as daemon-core-emitted with payload `{started_at, pid, binary_commit_hash}`. PL's §6.2 event list includes only `daemon_ready`, `daemon_orphan_sweep_completed`, and `infrastructure_unavailable`. Nothing in PL tells the implementer WHEN `daemon_started` fires — plausibly between step 1 and step 2, but the spec does not say.
4. **Citation drift.** PL-005 step 4 cites `[execution-model.md §2.1]`. EM §2.1 is "In scope." The trailer format is §6.2 and the checkpoint contract is §4.4. Step 5 cites `[beads-integration.md §10.8]` — wrong; intent log is §4.10, open-bead read surface is §4.5.

### PL-006 — Orphan sweep precedes reconciliation — PARTIALLY

```go
func (d *Daemon) sweepOrphans(ctx context.Context) (*SweepReport, error) {
    report := &SweepReport{}

    // (a) tmux sessions matching `harmonik-<project-hash>-*`
    sessions, _ := tmux.ListSessions()
    for _, s := range sessions {
        if strings.HasPrefix(s.Name, "harmonik-"+d.projectHash+"-") {
            tmux.KillSession(s.Name)
            report.TmuxKilled++
        }
    }
    // "wait (bounded; ≤2 seconds) for underlying processes to exit"
    time.Sleep(2 * time.Second) // ??? is that the rule, or poll?

    // (b) worktree locks older than daemon-start-time
    for _, wt := range d.worktrees() { // ??? how is the list obtained?
        lockPath := filepath.Join(wt, ".harmonik/lease.lock")
        if info, err := os.Stat(lockPath); err == nil {
            if info.ModTime().Before(d.startTime) {
                os.Remove(lockPath)
                report.LocksCleared++
            }
        }
    }

    // (c) re-parented subprocesses (parent PID 1) matching handler paths
    // How do we enumerate processes and match binary paths cross-platform?
    // On Linux: /proc/<pid>/exe readlink. On macOS: proc_pidpath (libproc).
    // The spec says "matches a handler binary under the project's expected launch path"
    // — the set of expected launch paths lives in handler-contract HC-042 but is
    // configuration, not spec. The daemon must know this set at sweep time.

    // (d) stale intent files older than daemon start
    entries, _ := os.ReadDir(".harmonik/beads-intents/")
    for _, e := range entries {
        info, _ := e.Info()
        if info.ModTime().Before(d.startTime) {
            d.reconciler.InvokeCat3aDetector(e.Name()) // RC-013 per reconciliation
            report.StaleIntents++
        }
    }

    emit(ctx, "daemon_orphan_sweep_completed", report)
    return report, nil
}
```

Stuck points:

1. **Project hash naming.** PL-006 says tmux sessions match prefix `harmonik-<project-hash>-`. No requirement names the hash function, the input, or the length. Is it SHA-256 of the absolute path? A 16-char prefix? An operator-configurable ID? Implementer must invent. Recommend: add `PL-INV-00X — Project hash is the first 12 hex chars of SHA-256(abspath(project_root))` or similar. This is load-bearing because multi-project daemons on the same machine MUST disambiguate their tmux sessions.
2. **Tmux wait discipline.** OQ-PL-002 acknowledges the "≤2 seconds" is unbounded: is it a fixed sleep, or a poll-until-empty with a 2-second ceiling? The difference matters: a fixed 2-second sleep wastes RTO budget (operator-nfr §4.8 targets 30s p95); a poll-until-empty is correct but the poll cadence is unspecified.
3. **Worktree enumeration.** Step (b) requires iterating worktrees. The daemon does not carry a worktree registry (WM-013 says `workspace_id = "ws-" + run_id`, derivable from Beads/git, but the in-memory model is not built until step 6 of PL-005). The orphan sweep runs **before** step 6. So: how does the sweep know which worktrees to inspect? Filesystem scan of `.harmonik/worktrees/*/`? The spec does not say. Add: "enumerate by filesystem scan of `.harmonik/worktrees/*/`; this surface is the authoritative worktree index for sweep-time (per [workspace-model.md §4.1 WM-002] path convention)."
4. **Re-parented subprocess detection.** Step (c) is OS-dependent. On macOS, enumerating child processes of init (PID 1) plus reading their executable paths requires `libproc`; on Linux, `/proc/<pid>/exe`. The spec would benefit from naming the platform abstraction (e.g., "via `gopsutil` or `syscall`-layer equivalent") or adding OQ-PL-00X for the cross-platform contract.
5. **SIGTERM → SIGKILL interval.** The spec says "a bounded interval between them" (OQ-PL-002 default 5s). 5s is consistent with handler-contract HC-018 ("subprocess cleanup to complete within 5s"), but the consistency is implicit — an implementer could pick 1s without violating PL-006. Add a normative default or cite HC-018.
6. **Citation drift.** PL-006 cites `[workspace-model.md §5.1]` — WM §5 is Invariants (WM-INV-001 is the lease-by-run invariant); WM-033 lease lock lives at §4.8. `[reconciliation.md §9.3]` for Cat 3a is wrong — RC-013 is at §4.3, Cat 3a taxonomy is at §8.4a.

### PL-009 — Ready criteria — IMPLEMENTABLE

```go
func (d *Daemon) readyCriteriaMet(state *StartupState) bool {
    return state.OrphanSweepComplete &&
        state.Cat0Passed &&
        state.GitWalkComplete &&
        state.BeadsQueryComplete &&
        state.InMemoryModelBuilt &&
        state.SynchronousReconciliationComplete
}
```

Tight. PL-009 enumerates 5 preconditions, distinguishes synchronous reconciliation from in-flight investigator workflows, and names the measurement endpoint for operator-nfr's restart RTO. The `daemon_ready` event emission with `investigator_run_ids[]` is consistent with event-model §8.7.2. This is one of the cleanest requirements in the spec.

One minor gap: PL-009 does NOT say the daemon must record `daemon_ready.ready_at` as the wall-clock time at emission. Event-model §8.7.2 declares the field; PL doesn't restate it. Implementer picks the right moment (on emission, not on criteria-met — the difference matters for RTO).

### PL-010 — Degraded state on Cat 0 infrastructure failure — IMPLEMENTABLE

```go
func (d *Daemon) handleDegraded(ctx context.Context, failure *PrereqFailure) {
    d.setStatus(StatusDegraded)
    emit(ctx, "infrastructure_unavailable", failure)
    t := time.NewTicker(d.cfg.Cat0RetryInterval) // OQ-PL-002 default 10s
    for range t.C {
        if res := d.cat0Precheck(ctx); res.Passed() {
            d.setStatus(StatusReconciling)
            return
        }
    }
}
```

Clean. The `harmonik status` report obligation is deferrable to operator-nfr (ON-002). OQ-PL-002 pins the retry cadence at "configurable, default 10s." The one concrete gap: the spec does not say whether the daemon exits degraded on SIGTERM or drains-while-degraded. Since degraded means no in-flight runs are classified, exit is trivially graceful (nothing to drain), but stating it avoids ambiguity.

### PL-011 — Graceful shutdown drains in-flight runs — PARTIALLY

```go
func (d *Daemon) gracefulStop(ctx context.Context) int {
    d.setStatus(StatusDraining)

    d.dispatcher.StopPullingNewBeads()          // step 1
    d.waitForRunsToCheckpoint(d.cfg.DrainTimeout) // step 2
    d.waitForAgentSubprocessTermination(...)    // step 3
    d.eventBus.Flush()                          // step 4 — fsync per EV §?
    d.workspace.ReleaseAllLeases()              // step 5 — per WM §4.x
    d.socketListener.Close()
    os.Remove(d.paths.Socket)
    d.pidLock.Release()                         // step 6
    os.Remove(d.paths.Pidfile)                  // implied?

    if d.drainEscalated { return 1 }
    return 0                                     // step 7
}
```

Stuck points:

1. **"In-flight runs proceed to their next checkpoint, then suspend."** The verb "suspend" is not defined. Does the run state transition to a `suspended` state? A run-state-machine state? Execution-model.md §7.1 does NOT have a `suspended` state — the states are `pending | running | failed | succeeded | canceled`. The spec equivocates: "suspend" might mean "the next `harmonik daemon` start re-enters the run via reconciliation" (which is the normal crash-semantics path). If so, it's not a new state, it's "stop executing; the next startup will re-detect." Clarify.
2. **Fsync location for event bus.** Step 4 says "flush the event bus (fsync per [event-model.md §3.4])." EV has no §3.4. EV §4 (requirements) and §6.5 (co-owned payloads) describe fsync-boundary semantics. The citation is broken.
3. **Lease release.** Step 5 cites `[workspace-model.md §5.1]`. WM §5 is invariants. Lease state machine is §4.4 / §7.1. Another broken citation.
4. **Drain-timeout escalation to forced termination.** PL-011 says "exit with code 1 if the drain timeout escalated to forced termination." What does "forced termination" look like? SIGTERM → wait T → SIGKILL? The spec says operator-nfr §7.7 owns the ordering, but PL must still pick a daemon-side mechanic. Add: "on drain-timeout expiry, the daemon MUST send SIGKILL to surviving agent subprocesses and proceed to step 4, still exit code 1."
5. **Pidfile removal.** PL-002 says the daemon "MUST hold an advisory lock on [the pidfile] for the duration of its lifetime." PL-011 says "release the pidfile lock" but does not say "remove the pidfile." On restart, PL-024's stale-pidfile detection assumes the file persists across crashes. On clean shutdown, should it be removed or left? Most daemons remove on clean shutdown and rely on PID-liveness check on dirty restart. Add an explicit rule.
6. **`daemon_shutdown` event is missing from PL.** Event-model §8.7.3 declares `daemon_shutdown` with `{shutdown_at, mode}`. PL-011 does not mention its emission. The implementer adds it where? Probably before fsync-flush (step 4) so the event lands in the log before the bus closes. The spec should say.

### PL-012 — Immediate shutdown aborts in-flight runs — IMPLEMENTABLE

Straightforward. "Skip steps 2–3; best-effort 4–7." The only ambiguity is whether `stop --immediate` emits `daemon_shutdown{mode=immediate}`. Presumed yes given EV §8.7.3, but PL should state.

SIGKILL cannot be intercepted, so steps 4–7 are skipped by force. PL-012 acknowledges this. Good honesty.

### PL-013 — Daemon does not exit on queue-empty — IMPLEMENTABLE

```go
func (d *Daemon) runLoop(ctx context.Context) {
    for {
        beads := d.br.Ready(ctx) // per BI-020
        if len(beads) == 0 {
            select {
            case <-time.After(d.cfg.QueueRequeryInterval): // "configurable cadence"
            case <-d.enqueueNotify:                         // in-process from enqueue CLI
            case <-ctx.Done():
                return
            }
            continue
        }
        d.dispatch(ctx, beads)
    }
}
```

Tight. The cadence is named as configurable. `harmonik enqueue` is the in-process notification path; the spec could state it explicitly (the in-memory channel delivers `enqueueNotify` only if the enqueue arrived via the socket from a `harmonik enqueue` CLI — external Beads mutations from outside harmonik require re-poll).

### PL-014 — Agent subprocesses are children of the daemon — IMPLEMENTABLE

```go
cmd := exec.CommandContext(ctx, binaryPath, args...)
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // new process group
// On Linux, HC-044 says SHOULD install PR_SET_PDEATHSIG(SIGTERM) — handler, not daemon
```

PL-014's structural content ("children of the daemon; OS re-parents orphans to init") is fine. What's missing (and handler-contract picks up): the child-process lifecycle — launch, watchdog, reap, restart policy. Handler-contract HC-011 owns the watcher goroutine. HC-018 owns cancellation timing. But the reap discipline (calling `cmd.Wait()` to collect exit status and avoid zombies) is not named anywhere. Implementer picks it up per Go convention, but a normative pointer would close the loop.

**Restart policy on subprocess crash is not declared.** PL-026 says "an agent-subprocess crash routes through handler contract §4.6 (error propagation)." Handler-contract §4.6 is the error taxonomy. The *restart* policy (retry? re-plan? fail the run?) is execution-model's `Routing of that event to retry / re-plan / terminal paths is owned by [execution-model.md §8]` (HC-024). PL is clean here: it delegates correctly. But PL-014's "child parentage is structural" sentence hides a bigger question — **no requirement in the PL spec says the daemon supervises its children**. No requirement says "the daemon calls `Wait()` on every spawned child." No requirement says "a child's exit triggers a watcher-goroutine teardown." These are handler-contract's HC-011 and HC-024. PL-014 should cite them explicitly (currently it doesn't), so an implementer wiring `internal/daemon` against the PL surface knows to hand the subprocess to the handler-contract watcher rather than managing it in the daemon loop.

### PL-016 — Agent-subprocess failure is observed by the daemon — IMPLEMENTABLE

Tight delegation: "The daemon-side watcher is S01-owned; the per-agent-type adapter (S04-owned) supplies the signal-interpretation layer." The `S01` and `S04` shorthand is not defined in the PL spec (S01 = Orchestrator Core per docs/foundation/components.md §1; S04 = Agent Runner). This is fine for cross-project vocabulary but PL's glossary should say so or `S01-owned` should be replaced with `owned by [handler-contract.md §4.3 HC-011]`. The current `S01`/`S04` shorthand breaks the locally-self-contained convention the template prefers.

### PL-018 — Daemon is a deterministic Go binary with no LLM logic — IMPLEMENTABLE

Enforceable via `go-arch-lint` (named by PL §10.2). The `internal/daemon` package MUST NOT import any LLM SDK. The conformance test is concrete: parse the binary's import graph, assert no `github.com/anthropics/*` or `github.com/openai/*` appear in the daemon's transitive closure. This is implementable and testable today.

### PL-020 — Composition root is `internal/daemon` — IMPLEMENTABLE

`go-arch-lint` rule per PL §10.2. Architecture §4.4 owns the subsystem-envelope rule. PL-020's delegation is clean. Implementer can write the `go-arch-lint` config today.

### PL-021 / PL-022 / PL-023 — ntm adapter scope — IMPLEMENTABLE

These are boundary requirements (anti-requirements, really — they name what the adapter MUST NOT import). Implementable as `go-arch-lint` deny-list rules:

```yaml
- name: ntm-adapter-scope
  from: internal/adapter/ntm
  allowed: [ntm/process, ntm/tmux, ntm/lifecycle, ntm/account]
  denied: [ntm/pipeline, ntm/swarmplan, ntm/checkpoint, ntm/agentmail]
```

The requirements are declarative enough to stub.

### PL-024 — Daemon crash leaves stale pidfile — IMPLEMENTABLE (with caveat from PL-002 stuck point)

```go
func detectStalePidfile(path string) (stale bool, pid int) {
    data, err := os.ReadFile(path)
    if err != nil { return false, 0 }
    pid, _ = strconv.Atoi(string(bytes.TrimSpace(data)))
    if pid == 0 { return true, 0 }
    if err := syscall.Kill(pid, 0); err != nil {
        // ESRCH -> dead; EPERM -> alive (permission denied but exists)
        if errors.Is(err, syscall.ESRCH) { return true, pid }
    }
    return false, pid // live
}
```

The logic is clean. The gap is the same as PL-002 stuck point (1) — the advisory-lock primitive determines whether the lock is automatically released on process death (flock/OFD: yes; fcntl: racy on fd-close semantics). PL should pick.

**PID reuse hazard.** `kill(pid, 0)` returning "alive" after a reboot could match an unrelated process that happened to recycle the PID. Handler-contract HC-044a acknowledges this ("PID recycled to a non-handler process identifiable by argv check"). PL-024 does not acknowledge it. Add: "daemon MAY corroborate PID liveness via argv check (e.g., `/proc/<pid>/cmdline`) to distinguish recycled PIDs from live daemons of the same project."

### PL-025 — Crash during startup reconciliation re-runs from step 1 — IMPLEMENTABLE

Delegates correctly to reconciliation §9.1a (actually §4.1 / §5 idempotence invariant). The implementability hinges on reconciliation's idempotence being true, which is a reconciliation-spec obligation, not PL's. Cross-reference drift: `[reconciliation.md §9.1a]` should be `[reconciliation/spec.md §4.1 RC-001]` or similar — §9 in reconciliation is cross-references.

### PL-026 — Agent-subprocess crash routes through handler contract — IMPLEMENTABLE

Clean. Delegates detection and routing to handler-contract §4.6 and reconciliation §9.2 (should be §4.3 / §8). Cross-ref drift again but the normative content is fine.

### PL-027 — Upgrade contract obligation — PARTIALLY (BLOCKED on operator-nfr)

This is a pure delegation requirement. PL-027 is an obligation-shaped requirement with no daemon-side behavior beyond "be consistent with whatever operator-nfr §7.5 produces." operator-nfr's §4.6 (ON-020) is itself an obligation-shaped requirement. Result: **the upgrade contract exists as a doubly-obligation-shaped promise with no concrete content**. An implementer cannot stub `harmonik daemon upgrade` today.

Concretely, the startup sequence's interaction with upgrade is undefined:

- Does `exec`-replacement re-run the orphan sweep? (Probably no — the new binary inherits the PID but the pidfile is still valid, and no subprocesses have been re-parented during exec. But PL does not say.)
- Does the new binary re-read the Beads audit log from scratch, or does it inherit in-memory state from the old binary (which it cannot — exec wipes address space)?
- Is the socket re-bound (PL-003 says "remove a stale socket file on startup before binding" — but during upgrade, the socket file is the LIVE daemon's, and removing it would break in-flight client connections)?
- What's the exec-replacement mechanic on macOS vs Linux (setsid differences, socket-fd-passing before exec)?

These are operator-nfr §4.6 (ON-020) obligations, not PL's. But PL-027 inherits the gap: PL-005 must behave consistently with the upgrade contract, and the upgrade contract has no content. Implementer cannot stub.

### PL-028 — Daemon command surface — PARTIALLY

```go
// cmd/harmonik/main.go
var rootCmd = &cobra.Command{Use: "harmonik"}
var daemonCmd = &cobra.Command{Use: "daemon"}    // PL-028
var attachCmd = &cobra.Command{Use: "attach"}    // PL-028
var runnerCmd = &cobra.Command{Use: "runner"}    // PL-028
var enqueueCmd = &cobra.Command{Use: "enqueue"}  // PL-028
var statusCmd = &cobra.Command{Use: "status"}    // PL-028
var pauseCmd = &cobra.Command{Use: "pause"}      // PL-028; semantics in ON §4.3
var stopCmd = &cobra.Command{Use: "stop"}        // PL-028; --graceful/--immediate
var upgradeCmd = &cobra.Command{Use: "upgrade"}  // PL-028; semantics in ON §4.6
// ... plus harmonik list per ON-041, harmonik claim-next / emit-outcome per §4.5
```

Stuck points:

1. **No flag surface.** PL-028 names commands but not flags. `harmonik stop` takes `--graceful` and `--immediate` per PL-011 and PL-012. `harmonik attach` takes `--socket` per ON-041. `harmonik daemon` takes what? No flag is declared in PL. Operator-nfr §4.10 ON-041 obligates multi-daemon flags on ALL daemon-communicating commands. PL-028's list overlaps but the flag contract is owned nowhere normative yet.
2. **Socket-communication protocol for CLI → daemon.** "CLI clients communicate with the daemon exclusively through this socket" (PL-003, PL-015). But the wire format on the socket for CLI commands is unspecified. Handler-contract §4.2 specifies NDJSON for handler ↔ daemon. Is the CLI ↔ daemon channel also NDJSON? JSON-RPC? Protobuf? Plain HTTP over Unix socket? No requirement says. Implementer invents.
3. **`harmonik runner` — what does "optionally spawns an orchestrator-agent session" mean mechanically?** PL-028 says "optionally spawns an orchestrator-agent session" — but PL-019 declares orchestrator-agent is "a separate Claude Code session" that interacts via the CLI. Does `harmonik runner` shell out to `claude` (or the CLI of whichever model class)? What's the invocation shape? Implementer cannot stub this today.
4. **`harmonik claim-next` and `harmonik emit-outcome`** (named in PL-015) do not appear in PL-028's list but are named as commands agents call via the socket. Are they agent-facing subcommands of `harmonik`? Their LaunchSpec (per HC-006) does not include them, so they're not obligations on the handler; they're socket-operations the Beads-CLI skill (BI-027) exposes. But PL-028 should either list them or name the skill as the access path and say "out of the direct CLI-surface obligation."
5. **OQ-PL-003 (auto-create `.harmonik/`) blocks PL-028: `harmonik daemon` behavior on a bare git repo is undefined.**

## Stuck points and stronger alternatives

For every partial/blocked finding above, here is concrete spec text the draft should adopt.

### PL-002 stuck point: lock primitive not named

**Add:** `PL-002a — Lock primitive. The pidfile lock MUST be acquired via fd-lifetime advisory lock: \`flock(LOCK_EX|LOCK_NB)\` on macOS; \`fcntl(F_OFD_SETLK)\` or \`flock(LOCK_EX|LOCK_NB)\` on Linux. The lock MUST be released automatically by the kernel on daemon-process termination (clean or crash), which is why fd-lifetime is mandatory — fcntl's process-lifetime semantics are forbidden because a child-process close of any shared fd would drop the lock.`

### PL-002 stuck point: exit-code citation drift

**Fix:** change the citation in PL-002 from `[operator-nfr.md §7.1]` to `[operator-nfr.md §8 code 5 "pidfile-locked"]`. Same fix needed in PL-010 (`§7.1` → `§4.1 ON-002`) and PL-011 (`§7.7` → `§4.7 ON-027`).

### PL-003 stuck point: socket mode unspecified

**Add:** `PL-003 clause: After bind, the daemon MUST \`chmod(0600)\` the socket file. The socket MUST be owned by the daemon's effective uid. Socket authenticity is filesystem-permission-based per [handler-contract.md §4.10 HC-044].`

### PL-004 stuck point: file surface incomplete

**Amend PL-004 to list:** `.harmonik/event_id_hwm` (per event-model EV-003a), `.harmonik/events/spill-<consumer>.jsonl` (per event-model EV-011a), and the daemon-written pidfile at `.harmonik/worktrees/<run_id>/.lock` (per handler-contract HC-044a — this is daemon-written even though its path is workspace-owned).

### PL-005 step 4: branch discovery rule

**Add a new clause to PL-005:** `The git-log walk MUST scan commits reachable from every task branch matching the naming convention \`task/*\` declared in [workspace-model.md §4.2 WM-005]. Detection is filesystem-based: \`git for-each-ref refs/heads/task/\`. No separate run-registry is maintained; branch naming is the authoritative in-flight-run index.`

### PL-005 step 5: Beads command naming

**Amend PL-005 step 5:** `Query Beads via \`br list --status open,in_progress --format json\` (or equivalent per [beads-integration.md §4.5 BI-020]). The \`br\` invocation MUST carry a request timeout; on timeout (T = 5s per [reconciliation.md §8.1]), the pre-check classifies as Cat 0 per §PL-010.`

### PL-006 stuck point: project hash

**Add:** `PL-006a — Project hash. The project hash referenced by §PL-006 MUST be the first 12 hexadecimal characters of \`SHA-256(abspath(project_root))\`, computed once at daemon start and stable across restarts. This disambiguates tmux sessions across multiple concurrent per-project daemons per §PL-001.`

### PL-006 stuck point: worktree enumeration

**Amend PL-006 bullet (b):** `The worktree enumeration MUST be a filesystem scan of \`.harmonik/worktrees/*/\` (path convention per [workspace-model.md §4.1 WM-002]). No in-memory registry is required at sweep time; the sweep runs before §PL-005 step 6.`

### PL-006 stuck point: SIGTERM→SIGKILL interval

**Amend PL-006 bullet (c):** `SIGTERM followed by SIGKILL after a 5-second bounded interval, consistent with [handler-contract.md §4.4 HC-018] cleanup bound.`

### PL-008 stuck point: doubly-obligation failure-mode catalog

**Adopt as normative:** PL-008 currently depends on operator-nfr.md §4.1 ON-003 to produce a catalog that does not yet exist. Until ON-003 is satisfied, an implementer has no concrete exit-code list. Add a daemon-side stub: `PL-008a — Interim catalog. Until [operator-nfr.md §4.1 ON-003] finalizes, the daemon MUST at minimum detect and distinguish these startup prerequisite failures with the following exit codes: 5 pidfile-locked, 6 socket-bind-failed, 7 git-bad-state, 8 beads-unavailable, 9 filesystem-unwritable, 10 disk-full (consistent with [operator-nfr.md §8]).`

### PL-011 stuck point: "suspend" verb and daemon_shutdown emission

**Amend PL-011 step 2:** `In-flight runs proceed to their next durable checkpoint per [execution-model.md §4.4 EM-017], then the daemon STOPS advancing them. No new run-state transition is introduced; the next startup's reconciliation per §PL-024–§PL-026 classifies these runs as Cat 1 or Cat 5 per [reconciliation/spec.md §4.3].`

**Add PL-011a:** `Shutdown event. The daemon MUST emit \`daemon_shutdown\` (per [event-model.md §8.7.3]) before the event bus flush (step 4). The event payload \`mode\` is \`graceful\` for §PL-011 and \`immediate\` for §PL-012.`

**Amend PL-011 step 6:** `Release the pidfile lock AND remove the pidfile on clean shutdown. PL-024's stale-pidfile detection applies only on crash, where the pidfile is left in place.`

### PL-014 stuck point: subprocess supervision delegation

**Amend PL-014:** `Subprocess supervision is owned by [handler-contract.md §4.3 HC-011] (per-session watcher goroutine) and [handler-contract.md §4.6 HC-024] (crash detection and routing). The daemon's PL-014 obligation is scoped to the OS-level parentage relationship; process-level supervision is handler-contract's concern.`

### PL-027 stuck point: upgrade's startup-sequence interaction

**Add PL-027a:** `Upgrade does not re-run the orphan sweep. An \`exec\`-replacement per [operator-nfr.md §4.6 ON-020(e)] preserves the daemon's PID. The new binary's startup sequence (§PL-005) MUST detect its own PID's pidfile (the lock having been transferred to the new process via exec), skip orphan sweep (no orphans exist — in-flight agents and worktrees remain managed by the same PID), and proceed to Cat 0 pre-check (§PL-005 step 3) directly. The socket file MUST NOT be unlinked (in-flight CLI connections survive via SO_REUSEADDR + SCM_RIGHTS fd-passing, or through the client-retry window declared by [operator-nfr.md §4.6 ON-020(e)]).`

### PL-028 stuck point: CLI ↔ daemon wire format

**Add PL-028a:** `CLI wire protocol. The daemon's Unix socket MUST carry a JSON-RPC 2.0 request/response stream framed as NDJSON (per [handler-contract.md §4.2 HC-007a]). CLI clients MUST issue one request per command and close the connection on response. The JSON-RPC method set is the command inventory of §PL-028 plus the agent-facing \`claim-next\`, \`emit-outcome\`, and Beads-CLI proxy methods of §PL-015.`

## Startup sequence concreteness

Can an implementer trace every step of §PL-005 (and §PL-006) to a defined endpoint? Walk-through:

- **Step 1 (pidfile lock):** Endpoint named (`.harmonik/daemon.pid`), primitive unnamed (see PL-002 stuck point). **Mostly traceable; primitive ambiguous.**
- **Step 2 (orphan sweep → §PL-006):** Four sub-steps (tmux, worktree locks, re-parented subprocesses, stale intent files). Each sub-step has a named action; sub-step (a) is blocked on project-hash definition, (b) on worktree enumeration, (c) on cross-platform process listing. **Partially traceable.**
- **Step 3 (Cat 0 pre-check):** Named endpoint (`RC-012` in reconciliation §4.3 / §8.1). The pre-check enumerates prerequisites but PL does not invoke them; it just says "Cat 0 pre-check per [reconciliation.md §9.3]" (broken citation; should be §4.3). **Traceable with citation fix.**
- **Step 4 (git log walk):** No named endpoint for branch set. See PL-005 stuck point (1). **Not traceable as written.**
- **Step 5 (Beads query):** No named `br` command. See PL-005 stuck point (2). **Not traceable as written.**
- **Step 6 (build in-memory model):** No declared model shape. The daemon must hold `Map<RunID, RunState>` presumably; nothing in PL declares this. Execution-model §6.1 has the `Run` record, but PL does not cite. **Not traceable.**
- **Step 7 (reconciliation dispatch):** Named endpoints (`[reconciliation.md §9.2a]` action-mapping, `§9.3]` detectors — both citation-broken; should be `§4.2` and `§4.3`). Even with citation fix, the DISPATCH mechanic (synchronous vs async; how the daemon holds references to in-flight investigator workflows for PL-009's `investigator_run_ids[]` field) is not declared. **Partially traceable.**
- **Step 8 (ready transition):** Clean endpoint (§PL-009 + `daemon_ready` event emission). **Traceable.**

Reconciliation dispatch coordination with WM orphan sweep (WM-033) and BI intent-log probe (BI-030) is named-by-delegation, but the actual sequence between PL-006 bullet (d) (stale intent referral to Cat 3a) and PL-005 step 7 (reconciliation dispatch) is implicit. Does the Cat 3a handling happen during step 2 or step 7? PL-006 bullet (d) says "each stale entry triggers a Cat 3a detector invocation per [reconciliation.md §9.3]" — invocation during sweep time, before Cat 0 pre-check. But reconciliation §4.3 says detectors run AFTER Cat 0. There's a contradiction: PL-006 invokes a Cat 3a detector before Cat 0 passes; reconciliation §4.3 says detectors are gated on Cat 0 first. Resolve.

Readiness signal is clean: PL-009 enumerates 5 criteria; `daemon_ready` is the event. Event-model §8.7.2 confirms the payload. This part of the spec is the strongest.

## Shutdown semantics

The spec covers normal and SIGTERM (both PL-011), plus SIGKILL (PL-012) and crash (PL-024). Coverage audit:

- **Normal (`harmonik stop --graceful`):** PL-011 — covered with gaps (see stuck points).
- **SIGTERM:** PL-011 treats SIGTERM as equivalent to `--graceful`. Clean.
- **SIGINT (Ctrl-C):** Not mentioned. Is SIGINT equivalent to SIGTERM (daemonic convention) or is it ignored (since the daemon is headless)? For `harmonik runner` (which is interactive), SIGINT is the natural interrupt. Add SIGINT handling.
- **SIGKILL:** PL-012 acknowledges it cannot be intercepted. Recovery is orphan sweep + reconciliation on next start. Clean.
- **Panic:** Not explicitly covered. A Go panic in the daemon's main goroutine terminates the process (non-zero exit). What about panics in handler-contract-watcher goroutines? Those are HC-011a's responsibility (recover and emit `agent_failed`). What about panics in PL's own goroutines (dispatcher, reconciler, supervisor)? PL does not say. Add a normative: `The daemon MUST install a top-level recover() barrier in main(). Unrecovered panics terminate the daemon with exit code "panic" (assign via operator-nfr §8 taxonomy) and leave the pidfile stale; recovery follows PL-024.`
- **OS crash / power loss:** PL-024 covers "unexpected daemon termination (panic, SIGKILL, OS crash)." OK.

Exit-code discipline: PL-011 step 7 names codes 0 (clean) and 1 (drain-timeout-escalated). Operator-nfr §8 has a 12-code taxonomy that PL does not cross-reference beyond the indirect citations. Gap: PL never enumerates the full exit-code surface the daemon emits, leaving implementer to cross-check operator-nfr §8 (which itself was authored before PL was drafted; the mapping is one-directional and likely drifted).

Final JSONL fsync discipline: PL-011 step 4 says "fsync per [event-model.md §3.4]." EV §3.4 does not exist. The actual fsync rule is in EV §4 (fsync-boundary class declared in event envelope §3, specific requirements spread across §4). Implementer cannot find the contract from the citation.

Lock file release: PL-011 step 5 says "release worktree leases (per [workspace-model.md §5.1])." WM §5 is invariants. Lease release is WM §4.4 / §7.1. Implementer cannot find the contract.

## Crash semantics

PL-024 through PL-026 form the crash story. Audit:

- **Daemon crash (detected at next startup):** PL-024 — detect stale pidfile, remove, proceed with startup. Clean.
- **Daemon crash during startup reconciliation:** PL-025 — next restart re-runs PL-005 from step 1. Relies on reconciliation idempotence (delegated). Clean with citation fix.
- **Agent subprocess crash:** PL-026 — routes through handler-contract §4.6. Clean.
- **Multiple concurrent daemons (PID reuse after reboot, two `harmonik daemon` invocations racing):** Covered by PL-002's advisory lock + PL-024's PID-liveness check. With caveat: PID reuse hazard not fully addressed (see PL-024 finding).

Pidfile / lock-file strategy coherence check:

- **`.harmonik/daemon.pid`** — daemon-owned, PL-002. Fd-lifetime advisory lock recommended (PL-002 stuck point).
- **`.harmonik/worktrees/<run_id>/.lock`** — declared by handler-contract HC-044a as a pidfile written at subprocess spawn. PL-004 should list this file in the daemon surface (it is daemon-written even though its path is inside a workspace-owned directory); PL-004 currently doesn't.
- **`.harmonik/worktrees/<run_id>/.harmonik/lease.lock`** (or equivalent) — declared by WM-033 as the lease lock. Different from the HC-044a pidfile. WM owns creation and release; PL-006's orphan sweep removes stale ones.

Three distinct lock files in `.harmonik/`: (a) daemon.pid, (b) worktree subprocess pidfile (HC-044a), (c) worktree lease lock (WM-033). PL-006 bullet (b) removes (c); PL-004 does not enumerate (b). This is the **HC-044a lock-path drift** mentioned in the review prompt: HC-044a puts the subprocess pidfile at `.harmonik/worktrees/<run_id>/.lock`, while WM-033 puts the lease lock at `.harmonik/lease.lock` or `${workspace_path}/.harmonik/lease.lock`. The two paths are similar enough to confuse an implementer. PL-004 should distinguish them.

## Agent-subprocess management

§PL-014 through §PL-017 declare the agent-subprocess relationship. Audit against the HC concurrency model (§4.3):

- **Launch:** PL-014 says "spawn as children of the daemon (via the ntm adapter or equivalent)." HC-001 defines `Handler.Launch`. PL's PL-014 and HC's HC-001 are consistent but the PL-014 text does not cite `[handler-contract.md §4.1 HC-001]`. Fix.
- **Watchdog:** PL-016 says "observed by the daemon's watcher goroutine per [handler-contract.md §4.3]." Cleaner. HC-011 declares one watcher per session. PL's delegation is correct.
- **Reap:** Not explicitly declared. Who calls `cmd.Wait()` on the subprocess to collect exit status? HC-008a says "the watcher MUST send SIGKILL and emit `agent_failed`" on shutdown-window expiry, implying the watcher holds the `*exec.Cmd` reference. PL should confirm: `PL-016 clause: The watcher owns the \`*exec.Cmd\` reference for the subprocess and is the exclusive caller of \`cmd.Wait()\`.`
- **Restart policy:** PL-026 delegates to handler-contract §4.6 (error routing) and execution-model §8 (retry/re-plan). Clean delegation. No explicit in-PL restart mechanic.
- **Silent-hang detection:** PL-017 names the obligation (detection rule owned by HC-026/§7.1). Clean.
- **Subprocess rate-limiting:** No PL requirement, but `agent_rate_limited` events flow through the watcher (HC-025). PL-017 could explicitly reference this to close the loop on "agent-subprocess lifecycle events."

**Coordination with HC §4.3 concurrency model:** HC-011 says one watcher goroutine per session. HC-012 says the adapter is not per-session. PL-016 says "the daemon-side watcher is S01-owned; the per-agent-type adapter (S04-owned) supplies the signal-interpretation layer" — consistent. But PL's framing as "S01-owned" and "S04-owned" uses the components.md subsystem codes (S01 = Orchestrator Core, S04 = Agent Runner). These codes are declared in `[docs/foundation/components.md §1]` but not in the PL glossary. New readers (implementers coming to PL first) have no way to resolve S01/S04 without hunting across documents. Either (a) add S01/S04 to the glossary, or (b) replace with `[handler-contract.md §4.3 HC-011]` citations.

**Concurrency ceiling:** Nothing in PL declares a maximum number of concurrent agent subprocesses. operator-nfr §4.10 ON-041(c) declares a machine-level ceiling obligation (multi-daemon) but per-daemon concurrency is not specified. Implementer picks an unbounded default, which will cause problems under contention. Add: `PL-014a — Per-daemon concurrency ceiling. The daemon MUST maintain a configurable concurrency ceiling on simultaneously-running agent subprocesses. Default: unbounded (subject to OS limits); operator-configurable. Cross-daemon coordination is owned by [operator-nfr.md §4.10 ON-041].`

## Cross-spec reach-ins

A reach-in is a requirement that silently consumes a contract from another spec without citing it. Findings:

1. **PL-003 socket mode `0600`** — consumed from handler-contract HC-044 ("Socket authenticity is filesystem-permission-based for MVH; daemon socket MUST be mode 0600"). PL-003 does not state this; it's implicitly imported.
2. **PL-004 file surface** — does not enumerate `.harmonik/event_id_hwm` (EV-003a) or `.harmonik/events/spill-<consumer>.jsonl` (EV-011a). Silent reach-in via EV.
3. **PL-004 transition-record directory** — cites `[execution-model.md §2.1]` (broken; actual content is EM §4.4 checkpoint-contract + §6.2 trailer format). Citation drift; the reach-in is declared but to the wrong section.
4. **PL-004 intent log** — cites `[beads-integration.md §10.8]` (broken; actual content is BI §4.10). Same.
5. **PL-005 step 4 git walk** — silently assumes `Harmonik-Run-ID` trailer (from EM §6.2) without citing it as a schema dependency. The cited `[execution-model.md §2.1]` is the scope statement, not the trailer schema.
6. **PL-005 step 6 in-memory model** — silently assumes `Run` record shape (EM §6.1). No citation.
7. **PL-006 bullet (b) "equivalent per [workspace-model.md §5.1]"** — WM §5 is invariants; actual lease-lock rule is WM §4.8 WM-033. Citation drift.
8. **PL-009 restart RTO measurement endpoint** — cites operator-nfr §7.8; actual content is ON §4.8. Drift.
9. **PL-010 `harmonik status`** — cites operator-nfr §7.1; actual content is ON §4.1. Drift.
10. **PL-011 drain timeout operator-configurable** — cites operator-nfr §7.7; actual content is ON §4.7. Drift.
11. **PL-014 "per locked decision 4"** — names a project-level decision but does not cite the decisions document. Implementer looking for the decision list has to hunt.
12. **PL-015 Beads-CLI skill** — cites `[beads-integration.md §10.9]`; actual content is BI §4.9. Drift.
13. **PL-016 "handler-contract §4.3"** — cites correctly! This is one of the few citations that resolves.
14. **PL-INV-002 centralized-controller** — cites architecture.md §4.9; confirmed correct.
15. **PL-020 subsystem-envelope rule** — cites architecture.md §4.4; confirmed correct.
16. **Events declared by PL's §6.2** — only 3 events (`daemon_ready`, `daemon_orphan_sweep_completed`, `infrastructure_unavailable`). Event-model §8.7 declares 7 daemon-core-emitted events (`daemon_started`, `daemon_ready`, `daemon_shutdown`, `daemon_startup_failed`, `daemon_degraded`, `daemon_orphan_sweep_completed`, `infrastructure_unavailable`). PL does not name WHEN `daemon_started`, `daemon_shutdown`, `daemon_startup_failed`, or `daemon_degraded` are emitted. Silent reach-in: event-model assumes PL owns the emission timing (per EV §9 cross-ref `[process-lifecycle.md §6.2, §8.2]`) but PL doesn't reciprocate the obligation.

**Aggregate citation-drift assessment:** the v0.2.0 cleanup pass (per the revision history) migrated architecture.md anchors but left every other spec's citations stale at the bootstrap-era section numbers (`§10.8`, `§9.3`, `§7.1`, `§5.1`, `§2.1`, `§3.2`, `§3.4`). PL v0.2.0 cannot be consumed end-to-end by an implementer: the citation chain is broken on first hop in ~12 places. The cleanup pass needs to run against all dependencies, not just architecture.md.

## Command surface (§4.10)

§4.10 (specifically PL-028) lists eight entry points: `harmonik daemon`, `harmonik attach`, `harmonik runner`, `harmonik enqueue`, `harmonik status`, `harmonik pause`, `harmonik stop`, `harmonik upgrade`. Plus `harmonik claim-next` and `harmonik emit-outcome` mentioned in PL-015. Implementability by command:

| Command | Implementable? | Gap |
|---|---|---|
| `harmonik daemon` | PARTIALLY | no flag contract; OQ-PL-003 (`.harmonik/` auto-create) open |
| `harmonik attach` | PARTIALLY | "observability TUI over the socket" — TUI framework / payload not specified |
| `harmonik runner` | BLOCKED | "optionally spawns an orchestrator-agent session" — orchestrator-agent invocation shape undefined |
| `harmonik enqueue` | PARTIALLY | wire protocol undefined (see PL-028 stuck point); enqueue payload unspecified |
| `harmonik status` | PARTIALLY | report format delegated to ON §4.1 (ON-002); no concrete shape here |
| `harmonik pause` | BLOCKED | semantics entirely owned by ON §4.3; pause ↔ reconciling interaction in ON-013 but PL doesn't explicitly consume |
| `harmonik stop` | IMPLEMENTABLE | PL-011/PL-012 cover `--graceful` / `--immediate` reasonably |
| `harmonik upgrade` | BLOCKED | contract entirely owned by ON §4.6 (ON-020), which is itself an obligation |
| `harmonik claim-next` | PARTIALLY | named but not in PL-028 list; payload / outcome wire format undefined |
| `harmonik emit-outcome` | PARTIALLY | same |

A Go engineer stubbing the CLI surface today can write the `cobra.Command` skeletons for all 10, but 6 of the 10 are stubs without a concrete request/response shape. The daemon's socket protocol is the central missing piece — PL-003 names the socket but nothing names the wire format carried over it. Handler-contract §4.2 HC-007a specifies NDJSON for handler ↔ daemon. Does the CLI use the same framing? If so, PL-028 should say so. If not, what does the CLI use? This is a foundational gap.

## Implementability score

Of 29 normative requirements (PL-001 through PL-028 plus three invariants PL-INV-001/002/003) walked or sampled:

| Verdict | Count | Percent |
|---|---|---|
| IMPLEMENTABLE | 11 | 38% |
| PARTIALLY | 13 | 45% |
| BLOCKED | 5 | 17% |

The 38% IMPLEMENTABLE includes PL-001 (scope), PL-009 (ready criteria), PL-012 (immediate shutdown), PL-013 (queue-empty), PL-018 (no-LLM invariant), PL-020 (composition root), PL-021/022/023 (ntm boundary), PL-024 (stale pidfile), PL-025 (crash during startup), PL-026 (agent crash routing), and PL-INV-002 / PL-INV-003.

The 45% PARTIALLY includes PL-002 (pidfile lock — primitive unnamed), PL-003 (socket — mode unstated), PL-004 (file surface — incomplete), PL-005 (startup sequence — 3 of 8 steps under-specified), PL-006 (orphan sweep — 4 sub-stuck-points), PL-008 (failure-mode catalog — doubly-obligation), PL-010 (degraded state — shutdown-while-degraded ambiguous), PL-011 (graceful shutdown — "suspend" undefined, `daemon_shutdown` missing), PL-014 (subprocess parentage — supervision delegation implicit), PL-016 (watcher obligation — partially delegated), PL-017 (silent-hang — clean delegation, content gap on cross-reference), PL-019 (orchestrator-agent invocation shape), PL-028 (command surface — wire format, orchestrator-agent invocation, 6 of 10 stubs).

The 17% BLOCKED includes PL-027 (upgrade contract — doubly-obligation), and 4 of PL-028's subcommands (runner, pause, upgrade, attach-TUI).

**Net implementability:** a Go engineer could produce a daemon skeleton today with pidfile lock, socket bind, status FSM (`starting | reconciling | ready | degraded | draining | stopped`), three event emissions, and the orphan-sweep scaffold. They would block on: the lock primitive, the project-hash definition, the branch-discovery rule, the Beads command inventory, the socket wire format, the orchestrator-agent invocation shape, the full file-surface (event_id_hwm, spill files), the daemon_shutdown emission point, the full exit-code table, and the entire upgrade contract. These are not editorial gaps; they are load-bearing daemon mechanics. The citation-drift problem compounds: an implementer trying to resolve any of these gaps by following the PL cross-references hits broken hops in most cases.

## Findings summary (for the author's next revision)

### Blocking

1. **Citation drift across the entire corpus.** PL v0.2.0 cites `[operator-nfr.md §7.X]` (should be §4.X), `[event-model.md §3.X]` (should be §4.X / §6.5 / §8.X), `[beads-integration.md §10.X]` (should be §4.X), `[reconciliation.md §9.X]` (should be §4.X / §8.X), `[execution-model.md §2.1]` (should be §4.4 / §6.2), `[workspace-model.md §5.X]` (should be §4.X). The v0.2.0 cleanup pass (per revision history) migrated architecture.md anchors only; every other spec's citations are broken. This must be fixed corpus-wide before PL v0.2 can be consumed.

2. **Event emission gaps.** PL §6.2 declares 3 events; event-model §8.7 declares 7 daemon-core-emitted events. PL must either (a) declare emission timing for `daemon_started`, `daemon_shutdown`, `daemon_startup_failed`, `daemon_degraded`, or (b) cite event-model §8.7 as the owner of those emission points (but EV cross-refs back to PL for timing — circular). One of the two specs has to own each emission; today neither does.

3. **Socket wire format.** §4.10 PL-028 names the command surface but no requirement defines the socket wire format (JSON-RPC? NDJSON? binary?). This blocks every CLI → daemon command.

4. **Project hash unspecified.** §4.2 PL-006 bullet (a) uses "project hash" without defining it. Load-bearing for multi-daemon tmux disambiguation (PL-001).

5. **Ordering contradiction between PL-006 bullet (d) and Cat 0 gate.** PL-006 invokes Cat 3a detector during orphan sweep (step 2); reconciliation §4.3 RC-012 says detectors are gated on Cat 0 passing (step 3). Resolve.

### Partial (concrete fix available)

6. **Pidfile lock primitive.** §4.1 PL-002 needs to name fd-lifetime advisory lock (`flock`/OFD) and reject process-lifetime fcntl.
7. **Socket mode.** §4.1 PL-003 should state mode `0600` (consistent with HC-044).
8. **Daemon-owned file surface completeness.** §4.1 PL-004 should enumerate `event_id_hwm`, spill files, worktree subprocess pidfiles.
9. **Branch discovery rule for git log walk.** §4.2 PL-005 step 4 should cite WM-005 `task/*` naming convention.
10. **Beads command naming.** §4.2 PL-005 step 5 should cite BI-020 `br ready` (or equivalent).
11. **Worktree enumeration at sweep time.** §4.2 PL-006 bullet (b) should cite WM-002 path convention.
12. **SIGTERM → SIGKILL interval.** §4.2 PL-006 bullet (c) should cite HC-018 5s bound.
13. **"Suspend" verb in shutdown.** §4.4 PL-011 step 2 — define or replace.
14. **`daemon_shutdown` emission.** §4.4 PL-011 — declare emission point.
15. **Pidfile removal on clean exit.** §4.4 PL-011 step 6 — explicit rule.
16. **Panic recovery barrier in main().** §4.8 — add requirement for top-level `recover()`.
17. **Subprocess reap / `Wait()` caller.** §4.5 PL-016 — name watcher as the `Wait()` caller.
18. **Per-daemon concurrency ceiling.** §4.5 — add configurable ceiling requirement.
19. **S01 / S04 shorthand.** Glossary — resolve or replace with citations.
20. **SIGINT handling.** §4.4 — declare SIGINT as SIGTERM-equivalent or clarify.

### Editorial

21. **PL-INV-002 "spans architecture.md §4.1 (four-axis classification assigns llm-freedom=none to the daemon as a whole)"** — architecture.md §4.1 is AR-001 through AR-008 (four-axis + mechanism/cognition + triple); the "assigns llm-freedom=none to the daemon as a whole" claim is not in AR; it's implied by PL-018. This is an editorial overreach. Rewrite: "spans [architecture.md §4.1] (four-axis classification test) and [architecture.md §4.9] (centralized-controller)."
22. **§10.2 test obligations** are prose-only pending testing.md; OQ-PL-001 flags the migration. Acceptable at MVH.

## Affirmations

1. **The daemon-status state machine prefix (§7.1)** is clean: five transitions, each with emit. The table-form presentation matches template §7.1. The operator-control continuation is delegated correctly to operator-nfr §7.3.

2. **The daemon vs orchestrator-agent distinction (§4.6)** is crisp. PL-018's "daemon MUST NOT call any LLM, MUST NOT import any LLM SDK, and MUST NOT embed any cognition-bearing component" is an enforceable invariant testable by `go-arch-lint` plus binary import-graph inspection. PL-019's orchestrator-agent-as-separate-process framing closes a potential ambiguity (can the daemon "secretly" embed cognition via a subprocess? no — cognition lives in handler-launched agents or in an orchestrator-agent session).

3. **The ntm adapter scope requirements (§4.7)** are boundary-shaped in a testable way: §PL-022 enumerates the four ntm surfaces that MUST NOT be imported (Pipeline, SwarmPlan, checkpoint/recovery, Agent Mail). An `go-arch-lint` rule can express this directly.

4. **PL-009's ready criteria** name every precondition and name the measurement endpoint for operator-nfr's RTO. This is exemplary: the spec pins a wall-clock metric to a specific event emission.

5. **The orphan-sweep-before-reconciliation invariant (PL-INV-003)** is load-bearing and correctly identified as system-spanning. This is what an invariant should look like per template §5.

6. **The composition-root declaration (§4.6 PL-020)** is a clean seam: `internal/daemon` is the only package that imports across subsystems, enforced by `go-arch-lint`. Every other subsystem is reached via its published interface. This is one of the cleanest architectural pins in the corpus.

7. **The §2.2 scope list** is rigorous: 9 items, each naming what owns the excluded content. This is the template §2.2 rule applied honestly. The downside — see citation-drift finding — is that every "owned by [X §N]" is only as good as the section number; the scope list's delegation is correct but the anchors need corpus-wide repair.

8. **The `harmonik upgrade` obligation (§4.9 PL-027)** is honest about its own vacuity: it is an obligation-shaped requirement pointing at operator-nfr §7.5 (actual: §4.6 ON-020) which is itself obligation-shaped. The spec admits the daemon-side obligation is "startup and shutdown sequences be consistent with whatever the operator-nfr spec-draft produces" — that is a correctly-scoped delegation, not a hidden content gap. An implementer reading this knows to wait for operator-nfr to land rather than invent.
